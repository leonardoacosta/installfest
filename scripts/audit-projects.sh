#!/usr/bin/env bash
# audit-projects.sh — one-command drift audit for the project-management layer.
#
# Cross-checks the registry (home/projects.toml) against: the filesystem, the
# generated Raycast launchers, the workspace package + deployed profile
# symlinks, the ssh mesh, the peer machine's tier dirs, and the systemd user
# schedulers this repo deploys. Runs on either machine — this machine's tier is
# derived from `uname -s` (Linux -> remote, Darwin -> local).
#
# Detector only: reports drift, never mutates. Like check.sh it uses
# `set -uo pipefail` WITHOUT `-e` so every section runs and reports.
#
# AUDIT_SKIP_NET=1 skips the ssh/tailscale sections (5-6). Sections whose tool
# is absent are skipped with a warning, matching check.sh's idiom.
#
# Usage: scripts/audit-projects.sh   -> exit 0 all pass, 1 any FAIL.

set -uo pipefail

# --- log helpers (reuse repo's scripts/utils.sh) ---------------------------
if [ -f "scripts/utils.sh" ]; then
    # shellcheck source=scripts/utils.sh
    . "scripts/utils.sh"
elif command -v git >/dev/null 2>&1 && [ -f "$(git rev-parse --show-toplevel 2>/dev/null)/scripts/utils.sh" ]; then
    # shellcheck disable=SC1091
    . "$(git rev-parse --show-toplevel)/scripts/utils.sh"
else
    info()    { printf '==> %s\n' "$1"; }
    success() { printf '==> %s\n' "$1"; }
    error()   { printf '==> %s\n' "$1"; }
    warning() { printf '==> %s\n' "$1"; }
fi

# Resolve repo root so file globs are stable regardless of CWD.
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT" || { error "cannot cd to repo root"; exit 1; }
# Re-source utils now that we're at root (first attempt used a relative path
# that only resolves when already at root; this makes colors work from anywhere).
[ -f "scripts/utils.sh" ] && . "scripts/utils.sh"

# --- registry (single parse, cached in $TMP_DUMP for every later section) ---
# shellcheck source=scripts/lib/registry.sh
. "scripts/lib/registry.sh"
REG="$(registry_path)" || exit 1
PY="$(registry_python)" || exit 1

# One line per project: code|category|path|tier1,tier2
# Registry validation (unique codes, enums, required fields) happens here too:
# any violation prints to stderr and the dump exits 1.
registry_dump() {
  "$PY" - "$REG" <<'PYEOF'
import sys, tomllib
with open(sys.argv[1], "rb") as f:
    data = tomllib.load(f)
ok = True
seen = set()
CATS = {"b-and-b", "priceless", "personal", "cc"}
TIERS = {"remote", "local", "cloudpc"}
for p in data["projects"]:
    missing = [k for k in ("code", "name", "category", "path", "tiers") if k not in p]
    if missing:
        print(f"registry: {p.get('code', '?')}: missing {missing}", file=sys.stderr); ok = False
        continue
    if p["code"] in seen:
        print(f"registry: duplicate code {p['code']}", file=sys.stderr); ok = False
    seen.add(p["code"])
    if p["category"] not in CATS:
        print(f"registry: {p['code']}: bad category {p['category']}", file=sys.stderr); ok = False
    bad = set(p["tiers"]) - TIERS
    if bad:
        print(f"registry: {p['code']}: bad tiers {sorted(bad)}", file=sys.stderr); ok = False
    print(f"{p['code']}|{p['category']}|{p['path']}|{','.join(p['tiers'])}")
sys.exit(0 if ok else 1)
PYEOF
}

FAIL=0
TMP_DUMP="$(mktemp)"; TMP_ERR="$(mktemp)"; TMP_BASES="$(mktemp)"
trap 'rm -f "$TMP_DUMP" "$TMP_ERR" "$TMP_BASES"' EXIT

# Peer reachability latch — section_mesh sets it, section_remote_fs reads it.
PEER_HOST=""
PEER_REACHABLE=0

# has_tier TIER CSV — true if CSV (comma-joined tiers) contains TIER.
has_tier() { case ",$2," in *",$1,"*) return 0;; *) return 1;; esac; }

# machine_tier — this machine's registry tier from uname.
machine_tier() {
    case "$(uname -s)" in
        Linux)  echo remote;;
        Darwin) echo local;;
        *)      echo unknown;;
    esac
}

# --- Section 1: registry-parse ----------------------------------------------
section_registry() {
    if registry_dump >"$TMP_DUMP" 2>"$TMP_ERR"; then
        awk -F'|' 'NF{n=$3; sub(/.*\//,"",n); print n}' "$TMP_DUMP" >"$TMP_BASES"
        success "PASS: registry-parse ($(wc -l <"$TMP_DUMP" | tr -d ' ') projects)"
    else
        error "FAIL: registry-parse"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
        # Still emit basenames from any partial dump so later sections degrade.
        awk -F'|' 'NF{n=$3; sub(/.*\//,"",n); print n}' "$TMP_DUMP" >"$TMP_BASES"
    fi
}

# --- Section 2: local-fs (this machine's registered tier dirs + orphan scan) -
section_local_fs() {
    local tier bad=0 code cat path tiers d base
    tier=$(machine_tier)
    if [ "$tier" = unknown ]; then warning "skip: unknown OS (local-fs)"; return 0; fi
    # Registered dirs for THIS machine's tier must exist.
    while IFS='|' read -r code cat path tiers; do
        [ -n "$code" ] || continue
        has_tier "$tier" "$tiers" || continue
        [ -d "$HOME/$path" ] || { error "  missing dir for $code ($tier): ~/$path"; bad=1; }
    done <"$TMP_DUMP"
    # Orphan scan: git repos under ~/dev not backed by a registry path.
    # WARN (not FAIL) — legit unregistered repos exist.
    for d in "$HOME"/dev/*/; do
        [ -d "$d/.git" ] || continue
        base=$(basename "$d")
        grep -qxF "$base" "$TMP_BASES" || warning "  unregistered repo: ~/dev/$base"
    done
    if [ "$bad" -eq 0 ]; then success "PASS: local-fs ($tier dirs present)"
    else error "FAIL: local-fs"; FAIL=1; fi
}

# --- Section 3: raycast-sync -------------------------------------------------
# Orphans FAIL (generator never prunes, so they persist); missing scripts only
# WARN (chezmoi apply regenerates them). Asymmetry is intentional.
section_raycast() {
    local bad=0 pair tier dir f code tiers
    for pair in "local:platform/raycast-scripts/local" "cloudpc:platform/raycast-scripts/cloudpc"; do
        tier="${pair%%:*}"; dir="${pair#*:}"
        [ -d "$dir" ] || { warning "  no raycast dir: $dir"; continue; }
        # Orphans (FAIL): a <code>.sh whose code is not a registry code with
        # this tier. root.sh / open-project.sh are intentional infra.
        for f in "$dir"/*.sh; do
            [ -f "$f" ] || continue
            code=$(basename "$f" .sh)
            case "$code" in root|open-project) continue;; esac
            if ! awk -F'|' -v c="$code" -v t="$tier" \
                 '$1==c{n=split($4,a,","); for(i=1;i<=n;i++) if(a[i]==t) ok=1} END{exit !ok}' \
                 "$TMP_DUMP"; then
                error "  orphan raycast script ($tier): $code"; bad=1
            fi
        done
        # Missing (WARN): registry code with this tier but no script.
        while IFS='|' read -r code _ _ tiers; do
            [ -n "$code" ] || continue
            has_tier "$tier" "$tiers" || continue
            [ -f "$dir/$code.sh" ] || warning "  missing raycast script ($tier): $code"
        done <"$TMP_DUMP"
    done
    if [ "$bad" -eq 0 ]; then success "PASS: raycast-sync"
    else error "FAIL: raycast-sync"; FAIL=1; fi
}

# --- Section 4: workspace (profiles + deployed symlinks + consumer parity) ---
section_workspace() {
    local bad=0 org delta
    for org in b-and-b priceless personal; do
        [ -f "packages/workspace/profiles/$org/profile.toml" ] \
            || { error "  missing profile: packages/workspace/profiles/$org/profile.toml"; bad=1; }
    done
    # Deployed org symlinks — skip whole check if the parent isn't chezmoi-applied.
    if [ -d "$HOME/.config/workspace" ]; then
        for org in b-and-b priceless personal; do
            if [ ! -e "$HOME/.config/workspace/$org" ] \
               || ! readlink -e "$HOME/.config/workspace/$org" >/dev/null 2>&1; then
                error "  broken workspace symlink: ~/.config/workspace/$org"; bad=1
            fi
        done
    else
        warning "  skip: ~/.config/workspace not deployed (workspace symlinks)"
    fi
    # Consumer parity: wsenv --list codes (col 1) must equal registry code set.
    if [ -x "packages/workspace/bin/wsenv" ]; then
        delta=$(comm -3 \
            <(packages/workspace/bin/wsenv --list 2>/dev/null | awk '{print $1}' | sort -u) \
            <(awk -F'|' 'NF{print $1}' "$TMP_DUMP" | sort -u))
        if [ -n "$delta" ]; then
            error "  wsenv/registry code mismatch (left=wsenv-only, right=registry-only):"
            printf '%s\n' "$delta" | sed 's/^/      /'; bad=1
        fi
    else
        warning "  skip: wsenv not executable (workspace consumer)"
    fi
    if [ "$bad" -eq 0 ]; then success "PASS: workspace"
    else error "FAIL: workspace"; FAIL=1; fi
}

# --- Section 5: mesh (tailscale up + peer reachability) ----------------------
section_mesh() {
    if [ -n "${AUDIT_SKIP_NET:-}" ] || ! command -v tailscale >/dev/null 2>&1; then
        warning "  skip: net disabled or tailscale absent (mesh)"; return 0
    fi
    local bad=0 tier fail_host warn_host
    tailscale status >/dev/null 2>&1 || { error "  tailscale not up"; bad=1; }
    tier=$(machine_tier)
    case "$tier" in
        remote) fail_host=mac;     warn_host=cloudpc; PEER_HOST=mac;;
        local)  fail_host=homelab; warn_host=cloudpc; PEER_HOST=homelab;;
        *)      warning "  skip: unknown OS (mesh)"; return 0;;
    esac
    # cloudpc is WARN-only (frequently powered off); the tier peer is FAIL.
    if ssh -o BatchMode=yes -o ConnectTimeout=5 "$fail_host" true 2>/dev/null; then
        PEER_REACHABLE=1
    else
        error "  peer unreachable: $fail_host"; bad=1; PEER_REACHABLE=0
    fi
    ssh -o BatchMode=yes -o ConnectTimeout=5 "$warn_host" true 2>/dev/null \
        || warning "  peer unreachable (non-fatal): $warn_host"
    if [ "$bad" -eq 0 ]; then success "PASS: mesh ($tier)"
    else error "FAIL: mesh"; FAIL=1; fi
}

# --- Section 6: remote-fs (peer's registered tier dirs, one batched ssh) -----
section_remote_fs() {
    if [ -n "${AUDIT_SKIP_NET:-}" ] || ! command -v tailscale >/dev/null 2>&1; then
        warning "  skip: net disabled or tailscale absent (remote-fs)"; return 0
    fi
    local tier peer peer_tier paths missing
    tier=$(machine_tier)
    case "$tier" in
        remote) peer=mac;     peer_tier=local;;
        local)  peer=homelab; peer_tier=remote;;
        *)      warning "  skip: unknown OS (remote-fs)"; return 0;;
    esac
    if [ "$PEER_REACHABLE" -ne 1 ] || [ "$PEER_HOST" != "$peer" ]; then
        warning "  skip: peer $peer unreachable (remote-fs)"; return 0
    fi
    # cloudpc is out of scope here (Windows path semantics differ).
    paths=$(awk -F'|' -v t="$peer_tier" \
        '{m=split($4,a,","); for(i=1;i<=m;i++) if(a[i]==t) print $3}' "$TMP_DUMP")
    [ -n "$paths" ] || { warning "  no $peer_tier paths to check (remote-fs)"; return 0; }
    missing=$(printf '%s\n' "$paths" \
        | ssh -o BatchMode=yes -o ConnectTimeout=5 "$peer" \
            'while IFS= read -r p; do [ -d "$HOME/$p" ] || echo "$p"; done' 2>/dev/null)
    if [ -n "$missing" ]; then
        error "  peer $peer missing tier dirs:"; printf '%s\n' "$missing" | sed 's/^/      /'
        error "FAIL: remote-fs"; FAIL=1
    else
        success "PASS: remote-fs ($peer:$peer_tier)"
    fi
}

# --- Section 7: schedulers (Linux systemd user units this repo deploys) ------
section_schedulers() {
    case "$(uname -s)" in
        Linux)  : ;;
        Darwin) warning "  skip: launchctl audit deferred (schedulers)"; return 0;;
        *)      warning "  skip: unsupported OS (schedulers)"; return 0;;
    esac
    if ! command -v systemctl >/dev/null 2>&1; then
        warning "  skip: systemctl absent (schedulers)"; return 0
    fi
    local bad=0 u
    # Enablement unit per scheduler: mesh-heartbeat is timer-activated, so the
    # TIMER carries enablement and the .service is static by design (matches
    # run_onchange_after_install-user-schedulers.sh.tmpl, which enables
    # nexus-listener.service + mesh-heartbeat.timer).
    for u in nexus-listener.service mesh-heartbeat.timer; do
        [ "$(systemctl --user is-enabled "$u" 2>/dev/null)" = enabled ] \
            || { error "  not enabled: $u"; bad=1; }
    done
    # Health: the worker .service units must not be in a failed state.
    for u in nexus-listener.service mesh-heartbeat.service; do
        [ "$(systemctl --user is-failed "$u" 2>/dev/null)" = failed ] \
            && { error "  unit failed: $u"; bad=1; }
    done
    if [ "$bad" -eq 0 ]; then success "PASS: schedulers"
    else error "FAIL: schedulers"; FAIL=1; fi
}

info "audit-projects.sh — auditing project-management layer (root: $ROOT)"
section_registry
section_local_fs
section_raycast
section_workspace
section_mesh
section_remote_fs
section_schedulers

if [ "$FAIL" -eq 0 ]; then success "AUDIT CLEAN"; else error "AUDIT FOUND DRIFT"; fi
exit "$FAIL"
