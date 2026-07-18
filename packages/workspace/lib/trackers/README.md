# tracker adapters

Per-tracker shell scripts that emit a normalized JSON array of "ready" work for
an org. Called by `packages/workspace/bin/ws-ready (mux ready)`, which reads each org's
`profile.toml` and dispatches to the right adapter.

## Adapters

| Adapter | Tracker | Reads from | Output schema |
| --- | --- | --- | --- |
| `beads-ready <org>` | beads | `bd ready --json` in each project dir for the org | bd issue shape + `project_code`, `project_path` annotations |
| `ado-ready <org>` | Azure DevOps work items | `az boards query --wiql @file` against the project GUID | Normalized item with `id`, `title`, `state`, `priority`, `work_item_type`, `assigned_to`, `url`, plus raw `fields` |
| `none-ready <org>` | (no tracker) | — | always `[]` |

There is intentionally **no enforced canonical schema** across adapters at this
wave (W1). Adapters emit their source-natural shape with annotation fields.
Normalization across trackers is a problem for whoever consumes both (W2 or
later), at which point the JSON contract can be hardened with real consumer
constraints — not pre-emptively.

## Configuration

Each org declares its tracker in `~/.config/workspace/<org>/profile.toml`:

```toml
# packages/workspace/profiles/<org>/profile.toml
tracker = "beads"        # or "ado", "none"

# Required only when tracker = "ado":
[tracker.ado]
org_url    = "https://dev.azure.com/bbins"
project_id = "<GUID>"    # see "ADO setup" below
```

The repo-side source lives in `packages/workspace/profiles/<org>/profile.toml`;
chezmoi symlinks each into `~/.config/workspace/<org>/profile.toml` via the
`home/dot_config/workspace/symlink_<org>.tmpl` templates.

## ADO setup (b-and-b only, one-time per machine)

The Azure DevOps adapter has known upstream parser bugs in
`azure-devops` extension 1.0.2 that the adapter works around:

1. **WIQL with `[Field.Name]` brackets is split on whitespace** by az's
   argparse. Workaround: pass WIQL via `--wiql @/path/to/file`.
2. **`--project "Name With Spaces"` is split on whitespace** by az's
   argparse. Workaround: use the project GUID instead of the display name.
3. **`--query "value[?name=='X Y']"` is split**. Workaround: filter
   client-side after `-o json`.

### Step 1: authenticate to ADO

The standard `az login` covers ARM (subscriptions, resource groups) but **not**
Azure DevOps. ADO needs its own credential. Pick one:

```bash
# Option A — interactive device-flow PAT (azure-devops extension stores it):
az devops login --organization https://dev.azure.com/bbins

# Option B — env-var PAT (good for headless agents):
export AZURE_DEVOPS_EXT_PAT='<your-personal-access-token>'
```

A PAT with **Work Items (Read)** scope is enough for `ado-ready`. Generate at
`https://dev.azure.com/bbins/_usersSettings/tokens`.

### Step 2: fetch the project GUID

```bash
az devops project show \
  --organization https://dev.azure.com/bbins \
  --project 'Wholesale Architecture' \
  --query id -o tsv
```

Drop the GUID into `packages/workspace/profiles/b-and-b/profile.toml` under
`[tracker.ado].project_id`, commit, run `chezmoi apply`.

## Failure modes (all adapters)

Adapters **never** fail the dispatcher hard. On any error they:
- Log a one-line diagnostic to **stderr** describing what went wrong.
- Emit `[]` on **stdout**.
- Exit 0.

Rationale: a single misconfigured org should not block `mux ready` for the rest
of the portfolio. The dispatcher and consumer decide whether `[]` counts as a
failure for their use case.

## Adding a new tracker

1. Create `packages/workspace/lib/trackers/<name>-ready` (executable).
2. Accept `<org>` as `$1`. Read any tracker-specific config from
   `~/.config/workspace/<org>/profile.toml` under `[tracker.<name>]`.
3. Emit a JSON array on stdout. Fail gracefully per the rule above.
4. Update `bin/ws-ready (mux ready)` to recognize `tracker = "<name>"` in `profile.toml`.
5. Add a row to the adapters table above.

There is no `Adapter` interface, base class, or shared library — adapters are
intentionally independent shell scripts. If three adapters develop a real
shared concern (HTTP retry, auth-cache, output normalization), that's the
right time to extract a `lib/trackers/common.sh`, not before.
