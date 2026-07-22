# homelab-nfs Specification

## Purpose
This capability owns the homelab's NFS export posture for the curated `~/dev` root
(`scripts/homelab/nfs-export.sh`) shared over the Tailscale mesh — what client scope and
read/write permissions the export grants, and the guarantee that generating a new export
configuration never itself mutates the live NFS server. NFSv3 carries no user authentication,
so the export's CIDR/peer scope and `rw`/`ro` posture are the entire access-control boundary
for this share.

## ADDED Requirements

### Requirement: the ~/dev NFS export is scoped to actual mesh usage, not the whole CGNAT range
`scripts/homelab/nfs-export.sh` SHALL NOT export the curated `~/dev` root (`$EXPORT_ROOT`)
read-write to the entire `100.64.0.0/10` Tailscale CGNAT range. The export MUST take exactly one
of two forms, selected by the operator's answer to whether anything writes to the mount over
NFS: a read-only export (`EXPORT_OPTS` containing `ro`, still scoped to the CGNAT range since a
read-only export needs no per-peer narrowing), or a read-write export whose client scope is
narrowed from the CGNAT range to specific named mesh peer IPs (one export clause per peer,
`rw` retained only for those clauses). All other `EXPORT_OPTS` values
(`sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt`) SHALL remain unchanged
by either path.

#### Scenario: read-only path selected
- Given: the operator confirms the `~/dev` mount is read-only in practice (or is unsure)
- When: `scripts/homelab/nfs-export.sh`'s `configure_exports` generates `/etc/exports` content
- Then: the generated export line reads `$EXPORT_ROOT 100.64.0.0/10(ro,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt)`
  — `rw` does not appear anywhere in the generated content

#### Scenario: peer-scoped read-write path selected
- Given: the operator confirms a specific machine writes to the mounted `~/dev` over NFS
- When: `scripts/homelab/nfs-export.sh`'s `configure_exports` generates `/etc/exports` content
- Then: the generated content contains one export clause per named mesh peer IP (e.g.
  `$EXPORT_ROOT 100.x.y.z(rw,sync,...)`), the bare `100.64.0.0/10` CIDR clause with `rw` no
  longer appears, and each peer clause is comment-labeled with the machine it corresponds to

### Requirement: NFS export changes are generated, never auto-applied to the running server
Generating or changing the `/etc/exports` content for this capability SHALL NOT itself invoke
`exportfs -ra` or restart/reload `nfs-server.service` as part of this change. Verification of the
generated content SHALL happen via the script's own dry-run/print path or by inspecting what
`write_if_changed` would write, never by applying the change to the live system. Applying a
generated export to the running NFS server remains an explicit, separate maintainer action.

#### Scenario: generated content is verified without live mutation
- Given: `scripts/homelab/nfs-export.sh` has been changed to produce a new `EXPORT_OPTS` or
  export-clause set
- When: the change is verified
- Then: the verification reads the would-be `/etc/exports` content (dry-run/print path or
  `write_if_changed` inspection) and reports it, without running `exportfs -ra` or restarting
  `nfs-server.service`, and the report states explicitly that the live server was not touched
