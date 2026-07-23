# Docs-sweep false positives

Finding shapes `scripts/bin/docs-sweep` flags on this repo that are NOT real defects.
Read before vetting a `/improve:docs` run; apply with **verify-before-suppress** semantics —
a candidate is only suppressed after a grep/Read confirms it is the same shape recorded
here, never on a filename match alone.

## `dangling_ref` on a path a tombstone ledger cites *because* it was deleted

**Shape:** a receipts-tier ledger (a retired-directory README, a disposition table, a
changelog row) cites a path in a cell whose adjacent disposition reads `resolved (deleted)`,
`removed`, `retired`, or equivalent. The path is *supposed* to be absent — the row exists to
record its removal. `docs-sweep`'s `check_cited_paths` sees only the path token, not the
disposition beside it.

**Confirmed instance:** `docs/plans/README.md` — the disposition table row
`| dead scripts/cmux-debug.sh | resolved (deleted) |`. `scripts/cmux-debug.sh` is correctly
absent; deleting the row would destroy the record of why.

**How to tell it apart from a real dangling ref:** read the line, not just the path. A real
dangling ref asserts the path is live ("see `X`", "run `X`", "defined in `X`"). This shape
asserts the opposite. If the sentence around the path would still be true with the file
missing, it is this false positive.

**Do not "fix" by:** deleting the row, or repointing the path at a git-history URL. Both
degrade a receipts-tier record that is trustworthy precisely because it is unedited
(operational-docs canon § Receipts vs State).
