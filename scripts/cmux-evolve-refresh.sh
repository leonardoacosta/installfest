#!/usr/bin/env bash
# cmux-evolve-refresh.sh — Phase 1 data producer for `.claude/commands/cmux/evolve.md`.
#
# Polls cmux's (manaflow-ai/cmux) GitHub releases Atom feed
# (https://github.com/manaflow-ai/cmux/releases.atom — stable, unauthenticated,
# no API rate limit), diffs entries against the persisted cursor in
# .claude/cmux-evolve/state/last-checked.json, and prints ONE JSON object to
# stdout: {"changed", "latest_tag", "new_releases", "error"}.
#
# Contract (scripts-as-data-producers convention, see scripts/check.sh /
# scripts/audit-projects.sh): ALWAYS exits 0, even on total failure (network
# down, malformed feed, no python3) — a nonzero exit here would abort the
# calling command's ```! ``` preprocessor render. Every failure path prints
# the JSON error shape instead of a shell error.
#
# Deliberate filtering — ALLOWLIST, not a blocklist: manaflow-ai/cmux is a
# monorepo whose releases.atom interleaves entries from multiple distinct
# products by update time — the main `cmux` app (bare `vX.Y.Z` tags, e.g.
# v0.64.19), a separate companion tool `cmux-tui` (`cmux-tui-vX.Y.Z` tags),
# a `mux-sdk-*` release train, one-off landmark tags, and a perpetual
# "nightly" entry whose <id>/tag never changes even though its content
# updates on every nightly build. installfest's pipeline (cmux-workspaces.sh,
# cmux-bridge.py, the Rust HTTP relay) drives the MAIN cmux app only — feeding
# it a cmux-tui/mux-sdk/nightly release would have Phase 4's research agents
# evaluating an unrelated product's changelog. Rather than name every current
# and future sub-project prefix in a blocklist, only tags matching a bare
# semver pattern (^v[0-9]+\.[0-9]+\.[0-9]+, e.g. v0.64.19) are accepted —
# this naturally excludes "nightly", "cmux-tui-*", "mux-sdk-*", and any other
# prefixed sub-project tag this monorepo adds later, with no name-list to
# maintain.
#
# Executed-only (never sourced) — bare strict mode is safe here. Deliberately
# NOT `set -e` (matching check.sh/audit-projects.sh): every failure must be
# caught and converted to the JSON error shape, not abort the script.
set -uo pipefail

FEED_URL="https://github.com/manaflow-ai/cmux/releases.atom"
CURL_MAX_TIME="${CMUX_EVOLVE_CURL_MAX_TIME:-10}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
STATE_DIR="$ROOT/.claude/cmux-evolve/state"
STATE_FILE="$STATE_DIR/last-checked.json"

# Accept --json for CLI parity with the command's ```! ``` invocation; JSON is
# the only output mode this script has, so the flag is a no-op. Any other arg
# is ignored rather than treated as a hard failure (never abort the render
# over an arg-parse quibble).
for _arg in "$@"; do
  case "$_arg" in
    --json) : ;;
    *) : ;;
  esac
done

# Minimal JSON string escaping (backslash, double-quote, newline) — used only
# on the pure-bash error path below, which must not depend on python3 being
# present (that's exactly one of the failure modes it reports).
json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/}"
  printf '%s' "$s"
}

emit_error() {
  local msg; msg="$(json_escape "$1")"
  printf '{"changed": false, "latest_tag": null, "new_releases": [], "error": "%s"}\n' "$msg"
  exit 0
}

mkdir -p "$STATE_DIR" 2>/dev/null || emit_error "cannot create state dir: $STATE_DIR"

if [ ! -f "$STATE_FILE" ]; then
  printf '{"last_tag": null, "last_checked": null}' > "$STATE_FILE" 2>/dev/null \
    || emit_error "cannot seed state file: $STATE_FILE"
fi

# python3 selection: this script only needs the stdlib xml.etree.ElementTree
# module, which ships with every python3 build (including macOS's bundled
# /usr/bin/python3 = 3.9) — unlike registry.sh's registry_python, it does NOT
# need tomllib, so that helper's 3.11+ probe would impose an unnecessary
# version floor for no benefit here. A plain `command -v python3` is the
# right-sized tool for this job.
PY="$(command -v python3 || true)"
[ -n "$PY" ] || emit_error "python3 not found on PATH"

FEED_TMP="$(mktemp 2>/dev/null)" || emit_error "mktemp failed"
trap 'rm -f "$FEED_TMP"' EXIT

if ! curl -sf --max-time "$CURL_MAX_TIME" "$FEED_URL" -o "$FEED_TMP" 2>/dev/null; then
  emit_error "failed to fetch $FEED_URL (curl error or timeout)"
fi

STATE_TMP="${STATE_FILE}.tmp"

# Bash-function-pipes-into-python3-heredoc pattern (matches cmux-workspaces.sh's
# load_projects()): inputs cross via env vars, not argv or stdin.
OUTPUT="$(FEED_FILE="$FEED_TMP" STATE_FILE="$STATE_FILE" STATE_TMP="$STATE_TMP" "$PY" <<'PYEOF'
import datetime
import json
import os
import re
import sys
from xml.etree import ElementTree as ET

NS = {"atom": "http://www.w3.org/2005/Atom"}
TAG_RE = re.compile(r"<[^>]+>")
# Allowlist: only the main cmux app's bare-semver release tags (v0.64.19).
# Excludes "nightly", "cmux-tui-vX.Y.Z", "mux-sdk-vX.Y.Z", and any other
# sub-project prefix — see script header for why this is an allowlist.
MAIN_APP_TAG_RE = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+")


def strip_html(text):
    if not text:
        return ""
    plain = TAG_RE.sub(" ", text)
    plain = re.sub(r"\s+", " ", plain).strip()
    return plain[:300]


def emit_error(msg):
    print(json.dumps({"changed": False, "latest_tag": None, "new_releases": [], "error": msg}))
    sys.exit(0)


def main():
    feed_file = os.environ["FEED_FILE"]
    state_file = os.environ["STATE_FILE"]
    state_tmp = os.environ["STATE_TMP"]

    tree = ET.parse(feed_file)
    root = tree.getroot()

    entries = []
    for entry in root.findall("atom:entry", NS):
        link_el = entry.find("atom:link", NS)
        tag = None
        if link_el is not None:
            href = link_el.get("href", "")
            tag = href.rstrip("/").rsplit("/", 1)[-1] or None
        if not tag:
            id_el = entry.find("atom:id", NS)
            if id_el is not None and id_el.text:
                tag = id_el.text.rsplit("/", 1)[-1]
        if not tag or not MAIN_APP_TAG_RE.match(tag):
            # Allowlist, not a blocklist — only bare-semver main-app tags
            # (v0.64.19) pass. Excludes "nightly", "cmux-tui-*", "mux-sdk-*",
            # and any other sub-project prefix this monorepo adds. See header.
            continue

        title_el = entry.find("atom:title", NS)
        updated_el = entry.find("atom:updated", NS)
        content_el = entry.find("atom:content", NS)
        if content_el is None:
            content_el = entry.find("atom:summary", NS)

        entries.append({
            "tag": tag,
            "title": title_el.text if title_el is not None and title_el.text else "",
            "published": updated_el.text if updated_el is not None and updated_el.text else "",
            "notes_excerpt": strip_html(content_el.text if content_el is not None else ""),
        })

    try:
        with open(state_file) as f:
            state = json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        state = {}
    last_tag = state.get("last_tag")

    if not entries:
        latest_tag = last_tag
        changed = False
        new_releases = []
    else:
        latest_tag = entries[0]["tag"]
        if last_tag is None:
            changed = True
            new_releases = entries
        elif latest_tag == last_tag:
            changed = False
            new_releases = []
        else:
            changed = True
            new_releases = []
            for e in entries:
                if e["tag"] == last_tag:
                    break
                new_releases.append(e)

    # Persist the cursor after every successful fetch, changed or not, so
    # last_checked stays fresh — atomic write via .tmp + os.replace.
    new_state = {
        "last_tag": latest_tag,
        "last_checked": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }
    with open(state_tmp, "w") as f:
        json.dump(new_state, f, indent=2)
    os.replace(state_tmp, state_file)

    print(json.dumps({
        "changed": changed,
        "latest_tag": latest_tag,
        "new_releases": new_releases,
        "error": None,
    }))


try:
    main()
except Exception as exc:
    emit_error(f"{type(exc).__name__}: {exc}")
PYEOF
)"
PY_EXIT=$?

if [ "$PY_EXIT" -ne 0 ] || [ -z "$OUTPUT" ]; then
  emit_error "python3 parser failed (exit $PY_EXIT)"
fi

printf '%s\n' "$OUTPUT"
exit 0
