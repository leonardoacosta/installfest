# Plan 011: zsh + dot_local hygiene — dead vercel() ct routing, dead aliases, stale az comment, editor-wrapper dedupe, darwin chezmoiignore gap, zellij provisioning, three operator gates

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**:
> `git diff --stat 9399b92..HEAD -- home/dot_zsh/rc/ home/dot_local/bin/ home/.chezmoiignore scripts/install-arch.sh shared/cspell.json shared/vscode-user-settings.json home/dot_config/cspell/ docs/cloudpc-proxy-setup.md`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.
> Known-acceptable drift: plan 008 edits `scripts/install-arch.sh` line 3
> (header comment `install.sh` -> real entry point) — a diff touching ONLY
> that line is fine; anything touching the `packages=()` array is not.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (config/alias/comment edits, all reversible via git; the one structural change — editor-wrapper consolidation — has a deterministic runtime test)
- **Depends on**: soft ordering after `plans/008-*.md` (same file `scripts/install-arch.sh`, disjoint lines — see "Cross-plan coordination"); none otherwise
- **Category**: tech-debt
- **Planned at**: commit `9399b92`, 2026-07-14
- **Evidence**: `/improve:entropy` audit, adversarially verified bundle `/tmp/installfest-entropy-audit/plan-011.json` (ephemeral — this plan is self-contained)

## Why this matters

The zsh rc modules and `~/.local/bin` wrappers carry a cluster of dead surface
that actively misleads: a `vercel()` wrapper whose only non-default branch has
been unreachable since the repo relocation (so the wrong-account protection it
exists for silently never fires, and it references an env var —
`$VERCEL_TOKEN_PRICELESS_`, trailing underscore — defined nowhere), two aliases
pointing at binaries that do not exist (`nx` -> `nexus`) or were removed by the
OS vendor (`airport`, gutted in macOS 14.4), a stale header comment in the `az`
wrapper that directs the next maintainer to a shell function that was
deliberately deleted, three ~90%-identical 30-line editor wrappers that drift
independently, a one-directional platform gate in `.chezmoiignore` that ships
Linux-only trees (hyprland, 14 systemd user units) onto every Mac, and a live
hard dependency (`ws-claude` -> `zellij`) with zero install provisioning — a
homelab rebuild after a btrfs rollback silently breaks `ws-claude` until zellij
is hand-installed. Landing this removes every confirmed dead line, closes the
rebuild gap, and surfaces the three items that need an operator decision
(broken `*-proxied` trio, dark-shipping `vercel-trim`, consumer-less global
cspell dictionary) instead of leaving them to rot.

## Current state

All excerpts are fresh reads at commit `9399b92` (clean tree).

### A. `home/dot_zsh/rc/shared.zsh` — dead vercel() routing + unguarded cs alias

Lines 160-168 (the ONLY `VERCEL_TOKEN` use site in the repo; the case arm is
dead — `home/projects.toml:127-133` places project `ct` at
`dev/priceless/civalent`, and `~/dev/ct` does not exist on disk):

```zsh
# Vercel CLI — per-project token routing
vercel() {
  case "$PWD" in
    */dev/ct|*/dev/ct/*)
      command vercel --token "$VERCEL_TOKEN_PRICELESS_" "$@" ;;
    *)
      command vercel "$@" ;;
  esac
}
```

`$VERCEL_TOKEN_PRICELESS_` (trailing underscore) is defined nowhere: not in
`home/dot_zshenv.tmpl`, not in deployed `~/.zshenv`/`~/.zshenv.local`, no
keychain entry. The wrapper always takes the `*)` branch — it is a pure
pass-through today.

Line 51 (unguarded cs alias):

```zsh
alias cs="~/dev/ccswitch.sh --switch"
```

### B. `home/dot_zsh/rc/linux.zsh` — duplicate cs alias + dead nx alias

Lines 45-46 (redefines the identical target behind a guard that defends
nothing — shared.zsh:51 sources first per `home/dot_zshrc`, so when the script
is missing the unconditional alias already exists):

```zsh
# Project switching
[[ -f "$HOME/dev/ccswitch.sh" ]] && alias cs="$HOME/dev/ccswitch.sh --switch"
```

Lines 69-70 (no `nexus` binary exists: `command -v nexus` exits 1 on this
machine; `~/.local/bin` holds only `nexus-*` helpers; nothing in the repo
installs a bare `nexus`; this alias is the repo's only reference):

```zsh
# Nexus CLI shortcut
alias nx="nexus"
```

### C. `home/dot_zsh/rc/darwin.zsh` — airport alias, binary gutted by Apple

Lines 27-28 (Apple neutered the private-framework `airport` CLI to a
deprecation stub in macOS 14.4 and removed it in later releases; `wdutil` is
the replacement; no other reference in the repo):

```zsh
# Airport CLI for WiFi diagnostics
alias airport="/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport"
```

### D. `home/dot_local/bin/executable_az` — stale header comment

Lines 16-17:

```
# Project routing (in shared.zsh):
#   ~/dev/ct/* → personal identity (no proxy) — handled by shell function, never reaches this script
```

This contradicts `shared.zsh:97-105`, which records that the old `az()`
function was REMOVED as a documented arg-mangling foot-gun and that the
sanctioned path is explicit `az --as-personal`. The comment misdirects the
next maintainer to a function that no longer exists.

### E. `home/.chezmoiignore` — one-directional platform gating

Full current file (20 lines): the only platform block skips macOS-only trees
on Linux; there is NO reciprocal block skipping Linux-only trees on macOS.

```
{{ if ne .chezmoi.os "darwin" }}
# Skip macOS-only files on Linux (patterns match TARGET paths, not source)
.config/ghostty
.config/karabiner
Library
{{ end }}
```

Consequence: `home/dot_config/hypr/` (2 files: `bindings.conf`,
`hypridle.conf`) and `home/dot_config/systemd/user/` (14 unit files) deploy as
inert dead files onto every Mac (cross-machine `chezmoi apply` is active via
deploy hooks). The Code/Cursor note at lines 17-20 shows both-OS deploys are
otherwise a deliberate documented decision — hypr/systemd just never got the
same treatment.

### F. `scripts/install-arch.sh` — zellij provisioning gap

The `packages=()` array (lines 24-70) and `aur_packages=()` array (lines
92-97) contain no `zellij`, and `grep -i zellij platform/homebrew/Brewfile`
exits 1. Meanwhile `home/dot_config/zellij/config.kdl.tmpl` deploys on all
platforms (not in `.chezmoiignore`) and `packages/workspace/bin/ws-claude`
hard-depends on the zellij CLI (lines 33-34 `zellij list-sessions` /
`exec zellij attach`, line 57 `exec zellij -s ...`).
`docs/homelab-recovery.md:15` warns userspace pacman packages vanish on btrfs
rollback — a homelab rebuild silently breaks `ws-claude`. `zellij` is in the
official Arch `extra` repo (verified: `pacman -Si zellij` -> `Repository :
extra`, version 0.44.3-1 at planning time).

### G. `home/dot_local/bin/executable_{code,cursor,zed}` — 3-way copy-paste

Three 30-line `#!/usr/bin/env zsh` scripts, ~90% identical: delegate to the
real binary if one exists elsewhere on PATH, else ssh back to the client
machine and open remotely. `code` and `cursor` differ ONLY in the binary name;
`zed` differs additionally in the remote-open URL form (line 27:
`zed ssh://$REMOTE_ALIAS$target_path` vs
`$name --remote ssh-remote+$REMOTE_ALIAS '$target_path'`). Shared shape
(from `executable_code:11-15`):

```zsh
self="${0:A}"
for candidate in ${(f)"$(whence -ap code 2>/dev/null)"}; do
  [[ "$candidate" == "$self" ]] && continue
  exec "$candidate" "$@"
done
```

The repo already uses name-dispatched symlinks heavily
(`home/dot_local/bin/symlink_*.tmpl`, 15 existing, e.g.
`symlink_ws-claude.tmpl` containing a single absolute target path line).

### H. Operator-gate items (Phase 2 — report, do not execute)

**H1. `*-proxied` trio broken on BOTH platforms.**
`home/dot_local/bin/executable_edge-proxied` (and the `outlook`/`teams`
siblings, each 4 lines) exec proxychains4 against macOS app-bundle paths:

```sh
exec proxychains4 -f "$HOME/.config/proxychains/proxychains.conf" "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" "$@"
```

But: (a) on Linux — where these deploy too, and where proxychains-ng IS
installed (`scripts/install-arch.sh:66`) — the `/Applications` path does not
exist; (b) on macOS, proxychains4 is not in the Brewfile, and
`docs/cloudpc-proxy-setup.md` § 2a (line 49) establishes ProxyBridge as the
macOS mechanism; (c) `docs/cloudpc-proxy-setup.md` § 2b (lines 79-89) still
documents the trio as the LINUX wrapper mechanism. Zero non-doc callers. This
supersedes the narrower "collapse 3 identical wrappers" quick-win at
`docs/plans/README.md:71-72`.

**H2. `executable_vercel-trim` ships dark.** A 130-line token-trimming wrapper
around the vercel CLI (header projects "~367k tokens/60d" savings, eval
2026-05-16; forwarding call `subprocess.run(['vercel'] + fwd, ...)` at line
117) with ZERO routing to any consumer: `shared.zsh` `vercel()` calls
`command vercel` directly, no rtk/rtk-local rewrite rule exists, no cc
skill/command references it, zero shell-history invocations.

**H3. Global cspell dictionary has no traceable consumer.**
`home/dot_config/cspell/symlink_cspell.json.tmpl` deploys
`~/.config/cspell/cspell.json` -> `shared/cspell.json`, whose own description
(line 5) claims "Per-repo cspell.json files merge with this" — but the cspell
CLI does not auto-discover that path, and the deployed editor settings do not
import it (`shared/vscode-user-settings.json:146-147` sets only
`cSpell.diagnosticLevel` / `cSpell.allowCompoundWords`; no `cSpell.import` or
`cSpell.customDictionaries` anywhere). The claimed merge only happens if OTHER
fleet repos import it by absolute path — verifiable by grep across `~/dev`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Full repo gate | `bash scripts/check.sh` (or `npm run check`) | exit 0, all PASS lines |
| zsh syntax (single file) | `zsh -n <file>` | exit 0, no output |
| bash syntax (single file) | `bash -n <file>` | exit 0, no output |
| chezmoi template render | `chezmoi execute-template < home/.chezmoiignore` | exit 0, rendered text |
| Deploy | `chezmoi apply ~/.zsh ~/.local/bin` | exit 0 |
| Deploy drift check | `chezmoi diff ~/.zsh ~/.local/bin` | exit 0, empty output |
| Arch package exists | `pacman -Si zellij` | exit 0, `Repository : extra` |

Note: `scripts/check.sh` runs `zsh -n` over `home/dot_zsh/**/*.zsh` +
`home/dot_zshrc`, `bash -n` + shellcheck (severity=error) over its `SH_FILES`
set, and chezmoi template render checks. It does NOT cover
`home/dot_local/bin/executable_*` zsh scripts — Step 7 carries its own
explicit `zsh -n` for the new script.

## Scope

**In scope** (the only files you may modify):

Phase 1 (execute):
- `home/dot_zsh/rc/shared.zsh` (this plan is the single owner of this file across the concurrent plan set)
- `home/dot_zsh/rc/linux.zsh`
- `home/dot_zsh/rc/darwin.zsh`
- `home/dot_local/bin/executable_az` (header comment only, lines 16-17)
- `home/.chezmoiignore`
- `scripts/install-arch.sh` (`packages=()` array only)
- `home/dot_local/bin/executable_editor-open` (create)
- `home/dot_local/bin/symlink_code.tmpl`, `symlink_cursor.tmpl`, `symlink_zed.tmpl` (create)
- `home/dot_local/bin/executable_code`, `executable_cursor`, `executable_zed` (delete — replaced by the above)
- `plans/README.md` (status row only)

Phase 2 (report only — NO edits without operator answer):
- `home/dot_local/bin/executable_edge-proxied`, `executable_outlook-proxied`, `executable_teams-proxied`
- `docs/cloudpc-proxy-setup.md` § 2b
- `home/dot_local/bin/executable_vercel-trim`
- `shared/cspell.json`, `home/dot_config/cspell/symlink_cspell.json.tmpl`, `shared/vscode-user-settings.json`

**Out of scope** (do NOT touch, even though they look related):
- `packages/workspace/**` and any `wsenv`/`wk` surface — owned by plan 010.
- `home/run_onchange_*.sh.tmpl` scripts — owned by plan 012.
- `scripts/install-arch.sh` line 3 header comment — owned by plan 008.
- `apps/cc-tmux/**` — owned by plans 013/014.
- `home/dot_zsh/functions/onepassword.zsh` — its `VERCEL_TOKEN` mention (line 19) is a comment listing example key names, not a definition; leave it.
- `scripts/mac-open.sh` coexistence with ropen — recorded decision if-34u, settled.
- `platform/raycast-scripts/**` generated files — settled (generator-owned).
- `home/.chezmoiremove` — adding darwin-gated removal entries for already-deployed hypr/systemd files on the Mac is deliberately NOT done here (a mis-gated template would delete live systemd units on Linux); see Maintenance notes.
- The `code()`/`cursor()`/`zed()` home-guard FUNCTIONS in `shared.zsh:107-147` — they call `command code` etc. and are unaffected by the wrapper consolidation; do not edit them.
- `.claude/workflows/project-mgmt-audit.js` (evidence ENT-05, stale `tmux-nexus-creds` reference) — in the evidence bundle but NOT owned by this plan; see Maintenance notes.

## Git workflow

- installfest ad-hoc lane, current branch (`main`), ONE commit at the end of
  Phase 1, targeted adds only (list every file explicitly; never `git add .`).
- Message style `type(scope): subject`, e.g.
  `chore(hygiene): drop dead vercel/nx/airport surface, dedupe editor wrappers, gate linux trees on darwin, provision zellij`
- `.beads/issues.jsonl` is gitignored in THIS repo — do not force-add it. If
  `.beads/interactions.jsonl` shows modified, include it in the add.
- Do NOT push unless the operator instructed it.

## Steps

### Step 1: Delete the dead vercel() wrapper in shared.zsh

In `home/dot_zsh/rc/shared.zsh`, delete lines 160-168 entirely (the
`# Vercel CLI — per-project token routing` comment through the closing `}`).
Rationale for full deletion rather than arm-only: with the dead `*/dev/ct`
arm removed, the wrapper is a bare pass-through (`command vercel "$@"`) —
zero value, so it goes (default decision per the audit; the alternative is
recorded under "Decision record" below).

**Verify**:
- `zsh -n home/dot_zsh/rc/shared.zsh` -> exit 0
- `grep -rn "VERCEL_TOKEN_PRICELESS" home/ scripts/ platform/` -> no matches
- `grep -cn "vercel()" home/dot_zsh/rc/shared.zsh` -> `0`

**Decision record (do not execute — for the operator)**: the alternative was
repointing the case arm to `*/dev/priceless/civalent|*/dev/priceless/civalent/*`
and defining a correctly-named token env var (the trailing underscore in
`VERCEL_TOKEN_PRICELESS_` looks like a truncation bug). Rejected as default
because the branch has been dead since the repo relocation with no observed
breakage, and no token definition exists anywhere to wire in. If the operator
later wants per-project vercel token routing back, this function is also the
natural wiring point for `vercel-trim` (Phase 2, H2).

### Step 2: Consolidate the cs alias to one guarded definition

1. In `home/dot_zsh/rc/shared.zsh`, replace line 51:

   ```zsh
   alias cs="~/dev/ccswitch.sh --switch"
   ```

   with:

   ```zsh
   [[ -f "$HOME/dev/ccswitch.sh" ]] && alias cs="$HOME/dev/ccswitch.sh --switch"
   ```

2. In `home/dot_zsh/rc/linux.zsh`, delete lines 45-46 (the
   `# Project switching` comment and the guarded `alias cs=` line).

**Verify**:
- `zsh -n home/dot_zsh/rc/shared.zsh && zsh -n home/dot_zsh/rc/linux.zsh` -> exit 0
- `grep -rn 'alias cs=' home/dot_zsh/` -> exactly 1 match, in `rc/shared.zsh`

### Step 3: Delete the dead nx alias (with homelab probe)

Probe first (the audit could not reach the homelab; a `nexus` binary there is
the only thing that would save this alias):

1. Locally: `command -v nexus; echo $?` -> expected `1`.
2. Find the homelab ssh alias: `grep -i '^Host ' ~/.ssh/config` (expect an
   entry like `homelab` or `hl`). Then:
   `ssh -o BatchMode=yes -o ConnectTimeout=5 <homelab-alias> 'command -v nexus'; echo $?`
   - Expected: non-zero (not found) or connection failure.
   - If it PRINTS A PATH (binary exists on homelab): STOP condition — do not
     delete; report the path.
3. If the probe found nothing (or the homelab is unreachable — the deletion is
   a one-line, git-reversible alias, so unreachability does not block it; note
   it in the final report): in `home/dot_zsh/rc/linux.zsh`, delete lines 69-70
   (`# Nexus CLI shortcut` + `alias nx="nexus"`).

**Verify**:
- `zsh -n home/dot_zsh/rc/linux.zsh` -> exit 0
- `grep -rn 'alias nx=' home/` -> no matches

### Step 4: Delete the dead airport alias (with Mac probe)

1. Optional probe (same pattern; Mac ssh alias is typically `mac`):
   `ssh -o BatchMode=yes -o ConnectTimeout=5 mac 'ls /System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport'`
   - Expected: `No such file or directory` (removed post-14.4) — or, if the
     file exists, it is the 14.4 deprecation stub that only prints a warning
     pointing at `wdutil`; either way the alias is dead weight.
   - Only if the probe shows a FUNCTIONING airport binary (produces real WiFi
     scan output): STOP condition — report instead of deleting.
2. In `home/dot_zsh/rc/darwin.zsh`, delete lines 27-28
   (`# Airport CLI for WiFi diagnostics` + the `alias airport=` line).

**Verify**:
- `zsh -n home/dot_zsh/rc/darwin.zsh` -> exit 0
- `grep -rn 'airport' home/dot_zsh/` -> no matches

### Step 5: Fix the stale az header comment

In `home/dot_local/bin/executable_az`, replace lines 16-17:

```
# Project routing (in shared.zsh):
#   ~/dev/ct/* → personal identity (no proxy) — handled by shell function, never reaches this script
```

with:

```
# Civalent/personal routing: the old az() shell function (and its ~/dev/ct
# path routing) was removed as a documented arg-mangling foot-gun — see
# shared.zsh "Per-Project CLI Routing". Sanctioned path: explicit `az --as-personal`.
```

Change NOTHING else in this file (it is a live, load-bearing wrapper).

**Verify**:
- `bash -n home/dot_local/bin/executable_az` -> exit 0
- `grep -n 'dev/ct' home/dot_local/bin/executable_az` -> at most the new comment's own `~/dev/ct` mention; `grep -n 'never reaches this script' home/dot_local/bin/executable_az` -> no matches

### Step 6: Add the darwin-side platform block to .chezmoiignore

In `home/.chezmoiignore`, insert AFTER the existing `{{ end }}` at line 15
(keep the trailing Code/Cursor NOTE block last):

```
{{ if ne .chezmoi.os "linux" }}
# Skip Linux-only files on macOS (patterns match TARGET paths, not source)
.config/hypr
.config/systemd
{{ end }}
```

**Verify**:
- `chezmoi execute-template < home/.chezmoiignore` -> exit 0; on this Linux
  machine the output CONTAINS `.config/ghostty` and does NOT contain
  `.config/hypr` (the new block renders empty on linux — that is correct; the
  render proves template syntax).
- `chezmoi diff ~/.config/hypr ~/.config/systemd` -> exit 0, empty (no change
  on Linux).

### Step 7: Provision zellij in install-arch.sh

In `scripts/install-arch.sh`, inside the `packages=()` array (lines 24-70),
add one entry after `tmux` (line 39):

```bash
        tmux
        zellij            # terminal multiplexer — ws-claude hard-depends on it (packages/workspace/bin/ws-claude)
        neovim
```

Do NOT touch line 3 (plan 008 owns the header) or anything else in the file.

**Verify**:
- `bash -n scripts/install-arch.sh` -> exit 0
- `pacman -Si zellij >/dev/null; echo $?` -> `0` (package name valid in official repos)
- `grep -n 'zellij' scripts/install-arch.sh` -> exactly 1 match inside `packages=(`

### Step 8: Consolidate the editor wrappers into one name-dispatched script

8a. Create `home/dot_local/bin/executable_editor-open` with exactly this
content:

```zsh
#!/usr/bin/env zsh
# editor-open — shared remote-editor wrapper for code / cursor / zed.
# ~/.local/bin/{code,cursor,zed} are chezmoi-managed symlinks to this file;
# behavior dispatches on the invoked name (${0:t}).
#
# This dotfile is chezmoi-applied on every machine (Mac client AND the
# Linux/homelab remote target), so it must behave differently on each:
#   - On the machine with the real editor install (the client), delegate
#     straight to the real binary — this file must never shadow the native CLI.
#   - On a remote host with no local install, ssh back to the client and
#     trigger the editor's remote-open there (VS Code/Cursor: Remote-SSH
#     `--remote ssh-remote+<host>`; Zed: `zed ssh://<host>/<path>`) — the
#     editor auto-starts its remote server, no manual "Connect" step needed.

name="${0:t}"
case "$name" in
  code|cursor|zed) ;;
  *) echo "editor-open: invoked as unsupported name '$name' (expected code|cursor|zed)" >&2; exit 2 ;;
esac

# Skip-self MUST resolve symlinks on BOTH sides: $0 is the symlink
# (~/.local/bin/code) but whence lists it unresolved — comparing unresolved
# candidate against resolved self would exec the symlink again (infinite loop).
self="${0:A}"
for candidate in ${(f)"$(whence -ap "$name" 2>/dev/null)"}; do
  [[ "${candidate:A}" == "$self" ]] && continue
  exec "$candidate" "$@"
done

# No local install — trigger the client's real editor over SSH.
CLIENT_HOST="${EDITOR_REMOTE_CLIENT:-mac}"
REMOTE_ALIAS="${EDITOR_REMOTE_ALIAS:-$(hostname)}"

target_path="$PWD"
for a in "$@"; do
  [[ "$a" != -* ]] && { target_path="${a:A}"; break; }
done

if [[ "$name" == "zed" ]]; then
  ssh -o BatchMode=yes -o ConnectTimeout=5 "$CLIENT_HOST" \
    "zed ssh://$REMOTE_ALIAS$target_path"
else
  ssh -o BatchMode=yes -o ConnectTimeout=5 "$CLIENT_HOST" \
    "$name --remote ssh-remote+$REMOTE_ALIAS '$target_path'"
fi
rc=$?
[[ $rc -ne 0 ]] && echo "error: could not reach $CLIENT_HOST to auto-open $name" >&2
exit $rc
```

8b. Create the three symlink templates, each a single line (matches the
existing idiom of `home/dot_config/cspell/symlink_cspell.json.tmpl`):

- `home/dot_local/bin/symlink_code.tmpl`:
  `{{ .chezmoi.homeDir }}/.local/bin/editor-open`
- `home/dot_local/bin/symlink_cursor.tmpl`: same content
- `home/dot_local/bin/symlink_zed.tmpl`: same content

8c. Delete `home/dot_local/bin/executable_code`, `executable_cursor`,
`executable_zed` (`git rm` them — 3 files, replaced by 8a/8b; under the
5-file deletion threshold).

8d. Deploy and runtime-test:

```bash
zsh -n home/dot_local/bin/executable_editor-open   # exit 0
chezmoi apply ~/.local/bin                          # exit 0
readlink ~/.local/bin/code                          # -> /home/<user>/.local/bin/editor-open
readlink ~/.local/bin/zed                           # -> /home/<user>/.local/bin/editor-open
# Dispatch test with fake binaries (deterministic, no editor windows opened):
mkdir -p /tmp/plan011-fakebin
for n in code cursor zed; do printf '#!/bin/sh\necho FAKE-%s "$@"\n' "$n" > /tmp/plan011-fakebin/$n; chmod +x /tmp/plan011-fakebin/$n; done
PATH=/tmp/plan011-fakebin:$PATH ~/.local/bin/code --version    # -> FAKE-code --version
PATH=/tmp/plan011-fakebin:$PATH ~/.local/bin/cursor --version  # -> FAKE-cursor --version
PATH=/tmp/plan011-fakebin:$PATH ~/.local/bin/zed --version     # -> FAKE-zed --version
rm -rf /tmp/plan011-fakebin
```

Expected: each invocation prints exactly its `FAKE-<name> --version` line —
proving name dispatch works AND the skip-self loop exec'd the OTHER candidate
(no infinite loop). Do NOT test the ssh fallback branch by invoking a real
editor name without the fake PATH — on a machine where the client is
reachable it would open a real editor window on the Mac.

### Step 9: Full gate, deploy, single commit

```bash
bash scripts/check.sh          # exit 0, all PASS
chezmoi apply ~/.zsh ~/.local/bin
chezmoi diff ~/.zsh ~/.local/bin   # exit 0, empty
zsh -ic 'whence -w vercel'     # -> "vercel: command" (function gone from a live shell)
```

Then ONE commit with targeted adds:

```bash
git add home/dot_zsh/rc/shared.zsh home/dot_zsh/rc/linux.zsh home/dot_zsh/rc/darwin.zsh \
        home/dot_local/bin/executable_az home/.chezmoiignore scripts/install-arch.sh \
        home/dot_local/bin/executable_editor-open \
        home/dot_local/bin/symlink_code.tmpl home/dot_local/bin/symlink_cursor.tmpl home/dot_local/bin/symlink_zed.tmpl \
        plans/README.md
# the three executable_{code,cursor,zed} deletions are already staged by git rm in 8c
git commit -m "chore(hygiene): drop dead vercel/nx/airport surface, dedupe editor wrappers, gate linux trees on darwin, provision zellij"
```

**Verify**: `git status --short` -> clean (or only files outside this plan's
scope that were already dirty before you started — record them if so).

### Step 10: OPERATOR GATE — proxied trio (report, do not edit)

Report to the operator with these facts (from Current state H1) and the two
options; make no edit until answered:

- **Option A (recommended — delete)**: `git rm` the three
  `executable_{edge,outlook,teams}-proxied` files and rewrite
  `docs/cloudpc-proxy-setup.md` § 2b to drop the wrapper-script block (keep
  the "wrap any command manually" proxychains4 recipe, which still works on
  Linux). Rationale: zero non-doc callers; broken on Linux (macOS app paths),
  non-functional on macOS (proxychains4 not installed; ProxyBridge is the
  documented macOS canon, § 2a).
- **Option B (platform-dispatch)**: keep the trio, gate by `uname -s` — Darwin
  branch execs the app-bundle path (and proxychains4 must then be added to
  `platform/homebrew/Brewfile`), Linux branch execs the Linux binaries
  (`microsoft-edge`/`teams`/`outlook` equivalents — NOTE: the Linux binary
  names were never in these files' git history; the operator must supply the
  intended Linux launch commands). Update § 2b to match.
- The open question the operator must answer: which platform is this flow
  actually intended for today? (Commit `0499e02`, 2026-07-13, switched the
  paths to macOS while the docs still say Linux.)

### Step 11: OPERATOR GATE — vercel-trim (report, do not edit)

Report options for `home/dot_local/bin/executable_vercel-trim`:

- **Option A (wire)**: re-add a thin `vercel()` function to `shared.zsh`
  routing through the trimmer, e.g.
  `vercel() { command vercel-trim "$@" }` (vercel-trim invokes the real
  `vercel` via subprocess and forwards its exit code — line 117/122). This
  realizes the header's projected ~367k tokens/60d savings for LLM sessions;
  cost: ALL interactive vercel output goes through the trimmer.
- **Option B (delete)**: `git rm home/dot_local/bin/executable_vercel-trim` —
  it has shipped dark since 2026-05-16 (zero routing, zero shell-history
  invocations); git history preserves it.
- Note the interaction with Step 1's decision record: if the operator wants
  per-project token routing back AND trimming, both land in the same restored
  `vercel()` function.

### Step 12: OPERATOR GATE — global cspell dictionary (probe, then report)

1. Run the consumer probe (read-only):

   ```bash
   grep -rln --include='cspell.json' --include='cspell.config*' 'config/cspell' ~/dev 2>/dev/null | grep -v personal/installfest
   ```

2. Report the result plus options:
   - If the probe found importing repos: the dictionary is live — record the
     consumer list in the report; no change needed.
   - If zero consumers: **Option A (wire)** — add
     `"cSpell.import": ["~/.config/cspell/cspell.json"]` to
     `shared/vscode-user-settings.json` (near lines 146-147), making the
     claimed merge real for Code+Cursor on both OSes; **Option B (delete)** —
     `git rm shared/cspell.json home/dot_config/cspell/symlink_cspell.json.tmpl`
     (2 files, git history keeps them).

## Test plan

This repo has no unit-test harness for shell config; the verification pattern
to mimic is the one used by `plans/004-session-context-git-status.md`: the
`scripts/check.sh` gate plus pasted runtime evidence for each behavioral
claim. Concretely, this plan's runtime evidence set is:

- `bash scripts/check.sh` full-pass output (syntax + shellcheck + template render).
- The Step 8d fake-bin dispatch transcript (3 `FAKE-<name>` lines) — this is
  the regression test for the ONE change with runtime behavior (symlink
  dispatch + skip-self loop). Paste it in the final report.
- `zsh -ic 'whence -w vercel'` output proving the function is gone from a
  live shell.
- `chezmoi diff ~/.zsh ~/.local/bin` empty output post-apply.

(`cc-tmux self-test` is NOT part of this plan's gate — nothing here touches
`apps/cc-tmux`; that gate belongs to plan 014.)

## Done criteria

Machine-checkable. ALL must hold:

- [ ] `bash scripts/check.sh` exits 0
- [ ] `zsh -n home/dot_zsh/rc/shared.zsh home/dot_zsh/rc/linux.zsh home/dot_zsh/rc/darwin.zsh` exits 0 for each
- [ ] `zsh -n home/dot_local/bin/executable_editor-open` exits 0
- [ ] `grep -rn "VERCEL_TOKEN_PRICELESS" home/ scripts/ platform/` -> no matches
- [ ] `grep -rn 'alias nx=' home/` -> no matches; `grep -rn 'airport' home/dot_zsh/` -> no matches
- [ ] `grep -rn 'alias cs=' home/dot_zsh/` -> exactly 1 match (shared.zsh, guarded form)
- [ ] `ls home/dot_local/bin/executable_code home/dot_local/bin/executable_cursor home/dot_local/bin/executable_zed 2>&1` -> all three "No such file"
- [ ] `readlink ~/.local/bin/code ~/.local/bin/cursor ~/.local/bin/zed` -> each resolves to `.local/bin/editor-open`; fake-bin dispatch test prints the 3 `FAKE-<name>` lines
- [ ] `grep -c 'zellij' scripts/install-arch.sh` -> `1` (inside `packages=()`)
- [ ] `chezmoi execute-template < home/.chezmoiignore` exits 0
- [ ] `chezmoi diff ~/.zsh ~/.local/bin` -> exit 0, empty output (post-apply)
- [ ] `git status --short` shows nothing outside the in-scope list
- [ ] Exactly ONE Phase 1 commit exists; Steps 10-12 produced a written operator report and ZERO edits
- [ ] `plans/README.md` status row for 011 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The drift check shows changes to any in-scope file beyond plan 008's
  line-3 header edit to `scripts/install-arch.sh`, and the "Current state"
  excerpts no longer match.
- Step 3's homelab probe prints a real `nexus` binary path — the alias may
  need repointing, not deletion; that is the operator's call.
- Step 4's Mac probe shows a functioning airport binary producing real output
  (not the deprecation stub).
- The Step 8d dispatch test loops, hangs, or prints anything other than the
  `FAKE-<name>` line twice in a row after a fix attempt — the skip-self
  resolution is the known risk; do not ship a wrapper that can infinite-exec.
- `chezmoi apply` or `chezmoi diff` reports changes to targets OUTSIDE
  `~/.zsh`, `~/.local/bin` (another session may be mutating the shared tree —
  this repo has documented concurrent-session incidents).
- `bash scripts/check.sh` fails twice after a reasonable fix attempt.
- Anything requires editing a Phase 2 file (proxied trio, vercel-trim,
  cspell surfaces) before the operator has answered Steps 10-12.
- You are tempted to also fix `.claude/workflows/project-mgmt-audit.js` or any
  wsenv/run_onchange file — those belong to other plans; report instead.

## Maintenance notes

- **Mac-side cleanup after Step 6**: `.chezmoiignore` stops FUTURE deploys of
  `.config/hypr` + `.config/systemd` onto Macs, but chezmoi does not remove
  already-deployed files when they become ignored. Cleanup options for the
  operator: manual `rm -rf ~/.config/hypr ~/.config/systemd` on the Mac, or a
  `{{ if eq .chezmoi.os "darwin" }}`-gated block in `home/.chezmoiremove`
  (deliberately not done here — a mis-gated remove template would delete live
  systemd units on Linux; if added later, test the render on BOTH OSes first).
- **Brewfile zellij**: this plan provisions zellij for Arch only (the ws-claude
  dependency runs on the homelab's zellij server). If Mac-local zellij use
  emerges, add `brew "zellij"` to `platform/homebrew/Brewfile` then.
- **Plan 008 interaction**: if 008 lands after this plan, its line-3 edit to
  `scripts/install-arch.sh` merges cleanly (disjoint lines). If 008 was
  already applied, the drift check will show it — proceed.
- **Unassigned finding (not owned here)**: evidence ENT-05 —
  `.claude/workflows/project-mgmt-audit.js:93` names the retired
  `executable_tmux-nexus-creds` file (replaced by apps/cc-tmux); any run of
  that saved workflow audits a nonexistent seam file. S-effort; flag for the
  plan-index maintainer if no other plan claims it.
- **Reviewer scrutiny points**: (a) the `${candidate:A}` symlink resolution in
  editor-open — the original files compared unresolved candidate to resolved
  self, which only worked because they were real files; (b) the guarded cs
  alias now silently defines nothing when `~/dev/ccswitch.sh` is absent —
  intended behavior, but a changed failure mode vs the old unconditional
  alias; (c) that Step 10-12 produced reports, not edits.
- **Settled items — do not re-open**: mac-open/ropen coexistence (if-34u),
  raycast generated files, `scripts/utils.sh` fan-in, check.sh shellcheck
  burn-down list (mux-remote SC1071, gk-github-auth SC2148), and the cc-tmux
  wave-1 rejected findings table in `plans/README.md`.
