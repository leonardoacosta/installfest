---
order: 0722j
---

# Proposal: scope-homelab-nfs-export — scope the ~/dev NFS export down from rw-to-the-whole-tailnet

## Change ID
`scope-homelab-nfs-export`

## Summary
Scope `scripts/homelab/nfs-export.sh`'s NFSv3 export of the curated `~/dev` root down from
read-write across the entire Tailscale CGNAT range to either a read-only export (safe default)
or a read-write export scoped to specific known mesh peer IPs — whichever matches how the
mount is actually used, per an operator decision this proposal cannot infer from code.

## Why
NFSv3 has no user authentication, and `nfs-export.sh` currently exports the curated `~/dev`
root (`personal/`, `priceless/`, `cc/`, `central-planning/`, `brown/`) read-**write** to the
whole `100.64.0.0/10` Tailscale CGNAT range, with `all_squash` mapping every connecting client
to uid 1000 (`anonuid=1000,anongid=1000`). Any device that joins the tailnet — or a single
compromised one — can therefore mount and **write** into those repos. Since scripts in those
repos later execute locally (hooks, deploy scripts, cron/systemd units), a write to code over
this export is a supply-chain path. The stated use case — the Mac mounting `~/dev` to read
source (`scripts/mac-autofs-dev.sh`) — does not obviously require `rw`, but this proposal does
not assume that; it is the subject of the `[user]` decision task below.

## What Changes
- Depends on the `[1.1]` `[user]` decision recorded in `tasks.md`: **either**
  - flip `EXPORT_OPTS` at `scripts/homelab/nfs-export.sh:76` from `rw` to `ro` (safe default,
    still CIDR-wide — `100.64.0.0/10` — since a read-only export needs no per-peer scoping), **or**
  - replace the single `$EXPORT_ROOT $TAILSCALE_CIDR($EXPORT_OPTS)` export clause at `:209` with
    one clause per known mesh peer IP (resolved via `tailscale status`/`tailscale ip`), keeping
    `rw` scoped only to those named peers instead of the whole `/10`.
  Exactly one of these two paths executes — never both, and never neither.
- The change is generated-content only: `nfs-export.sh` is not run in a way that calls
  `exportfs -ra` or restarts `nfs-server.service` as part of this proposal. Applying the
  regenerated `/etc/exports` content to the live NFS server remains a manual maintainer step,
  stated explicitly in the report.

## Context
- touches: `scripts/homelab/nfs-export.sh`

## Testing
| Affected seam | Task |
|----------------|------|
| `EXPORT_OPTS` / export-line construction (`configure_exports`) | `[1.2]` or `[1.3]` (whichever path the `[1.1]` decision selects), verified in `[2.1]` |
| Generated `/etc/exports` content is correct without applying it | `[2.1]` — read the script's own dry-run/print path or `write_if_changed`'s would-be content; do NOT run `exportfs -ra` or restart `nfsd` |
| Script syntax + repo verification gate | `[2.2]` — `bash -n scripts/homelab/nfs-export.sh` and `scripts/check.sh` both exit 0 |

## Done Means
- The generated `/etc/exports` content for the curated `~/dev` export is either read-only to the
  CGNAT range, or read-write scoped to named peer IPs only — never both, never the current
  rw-to-`/10` state.
- The change was NOT applied to the live NFS server (no `exportfs -ra`, no `nfs-server.service`
  restart) — that remains a manual maintainer step, stated explicitly in the report.
- The `[user]` question about actual write usage of the `~/dev` mount was asked and its answer
  recorded before either path was implemented — this proposal does not default silently.
