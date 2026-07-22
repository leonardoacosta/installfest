---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [x] [1.1] [user] DECISION: does anything write to the mounted ~/dev over NFS (e.g. edit-on-Mac-save-to-homelab), or is the mount read-only in practice? If unsure, default to ro and re-enable rw if something breaks. searched: docs/homelab-recovery.md, scripts/homelab/, README.md mesh section, and `rg -i "nfs|mount" docs/ scripts/homelab/` — docs describe the Mac mounting ~/dev for reading but do not confirm any write workflow; this is an operator fact about their own mesh usage, not inferable from code. [type:config]
  - Option 1: Read-only (`ro`) — safe default; flip `EXPORT_OPTS` rw→ro at nfs-export.sh:76, leave everything else unchanged.
  - Option 2: Scope rw to specific peer IPs — replace the single CGNAT-wide export clause with one clause per known mesh peer (resolved via `tailscale status`/`tailscale ip`), keeping rw only for those.
  - Recommendation: Option 1 — the source plan's own escape hatch says default to `ro` when unsure and re-enable `rw` only if something breaks; NFSv3 has no user auth, so the safer posture should win absent a confirmed write workflow.
  - Evidence: `plans/004-nfs-export-scoping.md` — "Answer = read-only / unsure -> proceed with step 1a (`ro`)"; "If unsure, we default to `ro` and you re-enable `rw` if something breaks."
  - **Resolved**: Option 2 — "Scope rw to specific peer IPs" (operator override of the recommendation; the Mac genuinely writes over this mount for editing files, so `ro` was not viable). Recorded in `decisions.jsonl` (`by: leo`, `ts: 2026-07-22T21:04:25Z`).
- [ ] [1.2] (read-only path, if Option 1 chosen) Flip EXPORT_OPTS rw→ro at nfs-export.sh:76, leave sync/no_subtree_check/all_squash/anonuid/anongid/crossmnt unchanged. [type:config]
  - depends on: 1.1
  - **Not the chosen path** — Option 2 was selected instead; left unimplemented.
- [x] [1.3] (peer-scoping path, if Option 2 chosen) Replace the single CIDR export clause with one clause per known mesh peer IP (resolve via tailscale status/tailscale ip), keeping rw only for those, one clause per machine with a naming comment. [type:config]
  - depends on: 1.1
  - Implemented in `scripts/homelab/nfs-export.sh`: added an `NFS_PEERS` array (`ip|comment` pairs) and rewrote `configure_exports()` to emit one `$EXPORT_ROOT $ip($EXPORT_OPTS)` clause per peer instead of the single `$TAILSCALE_CIDR` clause. Resolved peers via live `tailscale status` (not `ssh-mesh/README.md` alone — its documented Mac IP `100.91.88.16` is **stale**; live is `100.82.80.88`, hostname `macbook`). Only the Mac is included (`scripts/mac-autofs-dev.sh` is the sole NFS client script, macOS-only) — `homelab` itself is the server (not its own NFS client) and `cpc`/CloudPC (Windows bastion) has no NFS client wiring, so both are excluded from `NFS_PEERS`.

## E2E Batch

- [x] [2.1] Read the generated /etc/exports content via the script's own dry-run/print path (or by reading what write_if_changed would emit) — do NOT run exportfs -ra or restart nfsd. Record the before/after export line. [type:test]
  - No dry-run flag exists in the script (`main()` always runs the full pipeline including `exportfs -ra` on change) — verified by code-reading only, script was never invoked.
  - **Before** (live `/etc/exports`, still current as of this report):
    ```
    # Managed by scripts/homelab/nfs-export.sh — do not hand-edit.
    # NFS export of a curated ~/dev root, Tailscale mesh clients only.
    /srv/nfs-dev-export 100.64.0.0/10(rw,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt)
    ```
  - **After** (hand-traced through `configure_exports()`'s new `NFS_PEERS` loop — not applied):
    ```
    # Managed by scripts/homelab/nfs-export.sh — do not hand-edit.
    # NFS export of a curated ~/dev root, rw scoped to named mesh peers only (see NFS_PEERS).
    # mac (leo's MacBook — scripts/mac-autofs-dev.sh client)
    /srv/nfs-dev-export 100.82.80.88(rw,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt)
    ```
- [x] [2.2] bash -n scripts/homelab/nfs-export.sh && scripts/check.sh — both exit 0. Report explicitly states the export was NOT applied to the live system. [type:test]
  - `bash -n scripts/homelab/nfs-export.sh` — exit 0.
  - `scripts/check.sh` — `ALL CHECKS PASSED`, exit 0.
  - **The export was NOT applied to the live system.** Confirmed: `cat /etc/exports` still shows the pre-change CIDR-wide content (see [2.1] "Before"); `systemctl show nfs-server --property=ActiveEnterTimestamp` shows `Wed 2026-07-22 09:02:57 CDT`, unchanged since before this task started (no restart occurred). `exportfs -ra` and any `systemctl restart nfs-server` were never run — only `scripts/homelab/nfs-export.sh` (the source file) was edited.
