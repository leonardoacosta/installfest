#!/usr/bin/env bash
# beads-jsonl-merge-driver.sh — git merge driver for .beads/issues.jsonl.
#
# `bd merge` does not exist as a bd subcommand (bd 1.0.3/1.0.4) — the default
# `bd hooks install` wiring points merge.beads.driver at it anyway, so any
# real conflict on issues.jsonl makes git invoke a nonexistent command and
# fail outright instead of resolving it. See rules/BEADS.md § JSONL
# Git-Merge Conflicts for the full incident record (first found in `tc`
# 2026-07-08; this is cc's own instance of the identical gap, surfaced
# 2026-07-11 during an /apply merge-back when a concurrent session's commit
# also touched .beads/issues.jsonl).
#
# Does NOT attempt a content merge. issues.jsonl is a generated EXPORT of
# the real Dolt database, not hand-edited source — two independently
# exported snapshots are each a full point-in-time dump; line-merging them
# with a generic 3-way text merge can produce structurally invalid JSONL
# (interleaved/duplicate/truncated lines). The correct reconciliation
# already exists at the Dolt layer (bd dolt push/pull against the shared
# remote) — this driver is a passthrough to that authoritative source: pull
# the latest Dolt state, then regenerate a correct export.
#
# git merge driver contract: invoked as `driver %O %A %B`, must write the
# merged result to %A (the second positional arg here) and exit 0 on
# success, non-zero to leave the file as a real conflict.
set -euo pipefail

ANCESTOR="$1"
CURRENT="$2"   # %A — this is where the merged result must land
OTHER="$3"

if ! bd dolt pull 2>&1; then
  echo "beads-jsonl-merge-driver: bd dolt pull failed — leaving as a real conflict for manual resolution" >&2
  exit 1
fi

bd export -o "$CURRENT"
