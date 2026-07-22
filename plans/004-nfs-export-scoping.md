# Plan 004 — Scope the homelab NFS export down from rw-to-the-whole-tailnet

**Written against commit:** `d441448` — if the excerpt no longer matches, STOP and report drift.
**Finding:** #6 — NFS export is read-write to the entire `100.64.0.0/10` CGNAT range with `all_squash` (MED confidence).
**Priority:** 4 of 8. Independent. **Contains one `[user]` decision** — do not guess it.

## Why this matters

`scripts/homelab/nfs-export.sh` exports a curated `~/dev` root over NFSv3 to the whole
Tailscale CGNAT range, read-**write**, with `all_squash` mapping every client to uid 1000.
NFSv3 has no user authentication, so any device that joins the tailnet (or a compromised
one) can mount and **write** into `personal/ priceless/ cc/ central-planning/ brown/`.
Since scripts in those repos later execute locally, a write to code is a supply-chain path.
The stated use case (the Mac mounts `~/dev` to read source) does not obviously need `rw`.

## Current state (verified excerpt)

`scripts/homelab/nfs-export.sh:67` and `:76`:

```bash
TAILSCALE_CIDR="100.64.0.0/10"
...
EXPORT_OPTS="rw,sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt"
```

`:209` writes the export line:

```bash
        "$EXPORT_ROOT $TAILSCALE_CIDR($EXPORT_OPTS)")
```

## The decision that must come first (`[user]`)

**Does any machine WRITE to the mounted `~/dev` over NFS, or is it read-only consumption?**
searched: `docs/homelab-recovery.md`, `scripts/homelab/`, `README.md` mesh section, and
`rg -i "nfs|mount" docs/ scripts/homelab/` — the docs describe the Mac mounting `~/dev` for
reading but do not explicitly confirm whether any workflow writes back. This is an operator
decision about their own mesh usage; do not infer it from code.

Ask the maintainer exactly one question and wait:
> "Does anything write to `~/dev` over the NFS mount (e.g. edit-on-Mac-save-to-homelab), or
> is the mount read-only in practice? If unsure, we default to `ro` and you re-enable `rw`
> if something breaks."

- **Answer = read-only / unsure** → proceed with step 1a (`ro`).
- **Answer = writes are needed** → skip 1a, do step 1b (per-peer scoping, keep `rw`).

## Steps

1a. **(read-only path) Flip to `ro`.** Change `EXPORT_OPTS` at `:76`: `rw` → `ro`. Leave the
    rest (`sync,no_subtree_check,all_squash,anonuid=1000,anongid=1000,crossmnt`) unchanged.
    Verify: after the script writes `/etc/exports` (or in a dry-run — check whether the
    script has a `--dry-run`/`--print`; if so use it, otherwise inspect `write_if_changed`
    output), the exports line reads `... 100.64.0.0/10(ro,sync,...)`.

1b. **(writes-needed path) Scope to specific peers.** Replace the single
    `$TAILSCALE_CIDR($EXPORT_OPTS)` export with one clause per known mesh peer IP, keeping
    `rw` only for those. Get the peer IPs from the maintainer or from `tailscale status`
    (the three machines: Mac, homelab, Arch — the homelab is the server, so the clients are
    two). Build the export line as `"$EXPORT_ROOT $PEER1($EXPORT_OPTS) $PEER2($EXPORT_OPTS)"`.
    Prefer hardcoding resolved Tailscale IPs with a comment naming each machine, or resolve
    via `tailscale ip` at script run time if the script already shells to tailscale
    (grep first: `rg tailscale scripts/homelab/nfs-export.sh`).
    Verify: exports line lists the specific /32s, not the /10.

2. **Re-export safely (read-only verification only).** Do NOT run `exportfs -ra` or restart
   nfsd as part of this plan (that mutates the running system — out of bounds for the
   executor). Instead confirm the generated `/etc/exports` content is correct via the
   script's own dry-run/print path or by reading what `write_if_changed` would write.
   Leave applying it to the maintainer; say so in your report.

3. **Run the gate:** `scripts/check.sh` → exit 0 (shellcheck covers this file);
   `bash -n scripts/homelab/nfs-export.sh` → exit 0.

## Boundaries

- **In scope:** `scripts/homelab/nfs-export.sh` (the `EXPORT_OPTS` / export-line construction only).
- **Out of scope:** `nfs-export-bindmounts.sh`, the systemd units, firewall/`NFS_PORTS`, actually applying the export to the running system (maintainer action), the CIDR constant if the read-only path is chosen (leaving `/10` is fine when it's `ro`).

## Done criteria (machine-checkable)

- Read-only path: `rg 'EXPORT_OPTS=' scripts/homelab/nfs-export.sh` → contains `ro,` not `rw,`.
- Writes path: the constructed export line contains specific `100.x.y.z` addresses, not `100.64.0.0/10`, and `rg 'rw' ...` is scoped to those clauses only.
- `bash -n scripts/homelab/nfs-export.sh` → exit 0; `scripts/check.sh` → exit 0.
- Report explicitly states the export was NOT applied to the live system.

## Test plan

No harness. The syntax check + generated-content inspection are the tests. Record the
before/after `/etc/exports` line (from dry-run or code reading) in your report.

## Maintenance note

If a new mesh machine is added, the writes-needed path (1b) requires adding its IP here —
note that in a comment. The read-only path (1a) auto-covers new peers but only for reads,
which is the safer default.

## Escape hatch

- If the maintainer doesn't answer the `[user]` question, STOP after writing the question into your report — do NOT default to a change. This is the one plan where the safe default still alters behavior (read-only could break a silent write workflow), so it needs an explicit answer.
