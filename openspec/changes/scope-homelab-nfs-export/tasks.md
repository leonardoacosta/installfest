---
stack: t3
---

<!-- owner: homelab-specialist — this repo's non-T3-stack convention, see rules/PATTERNS.md -->

# Implementation Tasks

## API Batch

- [ ] [1.1] [user] DECISION: does anything write to the mounted ~/dev over NFS (e.g. edit-on-Mac-save-to-homelab), or is the mount read-only in practice? If unsure, default to ro and re-enable rw if something breaks. searched: docs/homelab-recovery.md, scripts/homelab/, README.md mesh section, and `rg -i "nfs|mount" docs/ scripts/homelab/` — docs describe the Mac mounting ~/dev for reading but do not confirm any write workflow; this is an operator fact about their own mesh usage, not inferable from code. [type:config]
  - Option 1: Read-only (`ro`) — safe default; flip `EXPORT_OPTS` rw→ro at nfs-export.sh:76, leave everything else unchanged.
  - Option 2: Scope rw to specific peer IPs — replace the single CGNAT-wide export clause with one clause per known mesh peer (resolved via `tailscale status`/`tailscale ip`), keeping rw only for those.
  - Recommendation: Option 1 — the source plan's own escape hatch says default to `ro` when unsure and re-enable `rw` only if something breaks; NFSv3 has no user auth, so the safer posture should win absent a confirmed write workflow.
  - Evidence: `plans/004-nfs-export-scoping.md` — "Answer = read-only / unsure -> proceed with step 1a (`ro`)"; "If unsure, we default to `ro` and you re-enable `rw` if something breaks."
- [ ] [1.2] (read-only path, if Option 1 chosen) Flip EXPORT_OPTS rw→ro at nfs-export.sh:76, leave sync/no_subtree_check/all_squash/anonuid/anongid/crossmnt unchanged. [type:config]
  - depends on: 1.1
- [ ] [1.3] (peer-scoping path, if Option 2 chosen) Replace the single CIDR export clause with one clause per known mesh peer IP (resolve via tailscale status/tailscale ip), keeping rw only for those, one clause per machine with a naming comment. [type:config]
  - depends on: 1.1

## E2E Batch

- [ ] [2.1] Read the generated /etc/exports content via the script's own dry-run/print path (or by reading what write_if_changed would emit) — do NOT run exportfs -ra or restart nfsd. Record the before/after export line. [type:test]
- [ ] [2.2] bash -n scripts/homelab/nfs-export.sh && scripts/check.sh — both exit 0. Report explicitly states the export was NOT applied to the live system. [type:test]
