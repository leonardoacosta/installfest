# cmux workspace-action API — description field (task 1.3 findings)

> Standalone note per task `[1.3]` in `openspec/changes/add-cmux-sidebar-widgets/`. Task `[1.2]`
> (shared smuggled-field encoding scheme, `docs/cmux-sidebar-encoding.md`) had not landed yet when
> this was written — fold this into that doc when it exists; do not duplicate the encoding scheme
> itself here, this is CLI/API surface only.

## CLI: setting a workspace's `description` (Python writer side)

Confirmed live via `ssh mac 'cmux workspace-action --help'`:

```
Usage: cmux workspace-action --action <name> [flags]

Actions:
  pin | unpin
  rename | clear-name
  set-description | clear-description
  move-up | move-down | move-top
  close-others | close-above | close-below
  mark-read | mark-unread
  set-color | clear-color

Flags:
  --action <name>              Action name (required if not positional)
  --workspace <id|ref|index>   Target workspace (default: current/$CMUX_WORKSPACE_ID)
  --description <text>         Description for set-description
```

The action name is `set-description` (not `description`, `set-desc`, or a bare `--description`
flag alone — the flag only takes effect paired with `--action set-description`). Real invocation
shape for the cc-tmux Python writer:

```bash
cmux workspace-action --action set-description --description "<encoded state string>"
```

`--workspace` defaults to `$CMUX_WORKSPACE_ID` (current workspace), which matches task `[2.1]`'s
plan to gate the dual-write on that env var being set — no explicit `--workspace` needed for the
common case of a hook running inside the workspace it's updating.

Cross-checked against the raw `cli-contract.md` resource
(`curl -fsSL https://raw.githubusercontent.com/manaflow-ai/cmux/main/docs/cli-contract.md`), which
documents the identical action list under "Workspace and tab action names":

```
| `workspace-action` | `pin`, `unpin`, `rename`, `clear-name`, `set-description`,
  `clear-description`, `move-up`, `move-down`, `move-top`, `close-others`, `close-above`,
  `close-below`, `mark-read`, `mark-unread`, `set-color`, `clear-color` |
```

Live CLI help and the documented contract agree — `set-description` is the confirmed action name
for task `[2.1]`/`[2.4]`'s dual-write call.

## Sidebar-side `cmux(...)` call surface — read-only for this spec, confirmed

Fetched `custom-sidebars.md` and `swiftui-interpreter-surface.md` from the same repo
(`raw.githubusercontent.com/manaflow-ai/cmux/main/docs/`) to check for a sidebar-invokable
description-write equivalent.

The sidebar's `cmux("<method>", param: value)` call form (used inside `Button(action:)` /
`.onTapGesture`) is a **fixed, narrow action surface**, documented in `custom-sidebars.md` as:

- `workspace.select` (`workspace_id`)
- `surface.focus` (`surface_id`)
- `workspace.reorder` (`workspace_id` + `index`, via the `Reorderable` wrapper's `move:` param)

`swiftui-interpreter-surface.md` (the interpreter's own capability-matrix doc) confirms
`ButtonAction` is currently "a frozen `[ActionCommand]` of `cmux`/`log`/`openURL`" — i.e. the
action executor exists only for these host-opaque dispatch calls, and no mutable-state/binding
engine exists yet for anything richer. Neither doc lists a `workspace.set-description` (or any
`workspace.setDescription`/`workspace.description.*`) sidebar method anywhere in the enumerated
surface or the "Not yet supported" section.

**Conclusion for this spec**: there is no documented sidebar-side write method for `description`.
The assumption in task `[1.3]`'s brief is correct — the sidebar `.swift` file is a pure **reader**
of the smuggled `description` field (parsed out for the state icon / status rows per tasks
`[3.2]`/`[3.3]`); the only writer is the Python/cc-tmux side via the CLI's
`cmux workspace-action --action set-description --description "..."`, as tasks `[2.1]`/`[2.4]`
already plan. No design change needed on the sidebar side to support writing.
