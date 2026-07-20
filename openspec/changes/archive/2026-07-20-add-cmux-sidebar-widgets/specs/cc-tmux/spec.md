# cc-tmux Specification Delta

## ADDED Requirements

### Requirement: Hooks dual-write session state into a cmux-readable workspace field
The plugin's hook handlers SHALL dual-write every existing state transition (`SessionStart` ->
`idle`, prompt-submit -> `active`, `permission_prompt` notification -> `waiting` + wait-reason
`permission`, `Stop` -> `idle`) to the pane's cmux workspace via `cmux workspace-action
--description <encoded-state>`, in addition to the existing tmux pane-option write, when the pane
is running inside a cmux-managed workspace (`CMUX_WORKSPACE_ID` set). The encoding SHALL be a single shared
scheme (state token, optional wait-reason, epoch of last transition) defined once and consumed
identically by both this writer and the cmux custom-sidebar reader. When `CMUX_WORKSPACE_ID` is
unset (plain tmux, no cmux), this write SHALL be skipped entirely — no behavior change to the
existing tmux-only path.

#### Scenario: idle-to-active transition dual-writes under cmux
- Given: a tracked pane inside a cmux workspace (`CMUX_WORKSPACE_ID` set), currently `idle`
- When: the user submits a prompt
- Then: the pane's `@cc-state` tmux option becomes `active` AND `cmux workspace-action
  --description <encoded active state>` is called for that workspace

#### Scenario: permission-wait transition carries the wait reason
- Given: a tracked pane inside a cmux workspace, currently `active`
- When: Claude fires a `permission_prompt` notification
- Then: the cmux-side write encodes both the `waiting` state and the `permission` wait-reason,
  not state alone

#### Scenario: no cmux workspace means no cmux write
- Given: a tracked pane in a plain tmux session with no `CMUX_WORKSPACE_ID` set
- When: any hook-driven state transition fires
- Then: the existing tmux pane-option write happens exactly as before, and no `cmux
  workspace-action` call is made

#### Scenario: a failed cmux write does not break the existing tmux behavior
- Given: `cmux workspace-action` fails (cmux not running, socket error, timeout)
- When: a hook-driven state transition fires
- Then: the tmux pane-option write still succeeds and the failure is swallowed silently (same
  fail-open posture as the existing hook timeout/self-heal invariants)
