# cmux Sidebars Specification

## Purpose
Custom cmux sidebar and browser-panel surfaces for the installfest workspace-launcher pipeline —
a left custom-sidebar widget surfacing live Claude Code session state per workspace, and two
right-side browser panels (commit graph, usage dashboard).

## ADDED Requirements

### Requirement: Left sidebar shows live git state for the focused workspace
The left custom sidebar (`home/dot_config/cmux/sidebars/claude-sessions.swift`) SHALL render, per
workspace row, the workspace's `branch` and `dirty` fields as already exposed natively by cmux's
`workspaces` live-data binding — no additional plumbing, no smuggled field.

#### Scenario: clean branch renders without a dirty indicator
- Given: a workspace whose live `branch` is `main` and `dirty` is `false`
- When: the sidebar renders that row
- Then: it shows `main` with no dirty marker

#### Scenario: dirty branch renders with a dirty indicator
- Given: a workspace whose live `branch` is `feature-x` and `dirty` is `true`
- When: the sidebar renders that row
- Then: it shows `feature-x` with a distinct dirty marker (e.g. a dot or asterisk)

### Requirement: Left sidebar shows project/folder name and Claude Code session name
Each row SHALL show the workspace's directory basename, truncated to 5 characters, followed by
the workspace's `title` (the Claude Code session name) when present. When `title` is absent, the
row SHALL show the truncated folder name alone.

#### Scenario: title present shows folder + session name
- Given: a workspace whose `directory` basename is `installfest` and `title` is `cmux:evolve`
- When: the sidebar renders that row
- Then: it shows `insta… cmux:evolve` (folder truncated to 5 chars, then the session title)

#### Scenario: no title falls back to folder name alone
- Given: a workspace whose `title` is empty
- When: the sidebar renders that row
- Then: it shows only the truncated folder name

### Requirement: Left sidebar shows Claude Code session state via a smuggled workspace field
The sidebar SHALL parse the workspace's `description` field for the shared state encoding cc-tmux
writes (see `cc-tmux` capability's dual-write requirement) and render a state indicator with three
visual modes: solid when `idle`, pulsating opacity when `active` (alternating by `clock`
wall-clock parity, mirroring cc-tmux's own tick-based pulse), and pulsing red when `waiting` with
wait-reason `permission`. Because cmux's sidebar interpreter has no image-loading support today,
the indicator SHALL use an SF Symbol or filled shape as a stand-in for the Claude mark, not the
literal brand SVG.

#### Scenario: idle state renders solid
- Given: a workspace whose `description` decodes to state `idle`
- When: the sidebar renders that row
- Then: the state indicator renders at full, static opacity

#### Scenario: active state pulses
- Given: a workspace whose `description` decodes to state `active`
- When: the sidebar is captured at two `clock` values of opposite wall-clock-second parity
- Then: the indicator's opacity alternates between two values across those captures

#### Scenario: waiting-for-permission pulses red
- Given: a workspace whose `description` decodes to state `waiting` with wait-reason `permission`
- When: the sidebar renders that row
- Then: the indicator renders in a red color and pulses (opacity alternates) the same way the
  active state does, visually distinct from the active-state color

#### Scenario: unparseable or absent description renders no state indicator
- Given: a workspace whose `description` is empty or does not match the encoding scheme
- When: the sidebar renders that row
- Then: no state indicator is shown for that row (no crash, no fallback icon)

### Requirement: Left sidebar shows openspec and beads status via the same smuggled mechanism
The sidebar SHALL render an openspec-status segment and a beads-status segment per workspace,
sourced from the same `description`-field encoding scheme as the Claude state indicator (a
periodic external writer, out of scope for this spec's UI requirement, populates these fields).

#### Scenario: openspec and beads segments render side by side
- Given: a workspace whose decoded fields include an openspec summary and a beads summary
- When: the sidebar renders that row
- Then: both segments render on the same row, separated by a visual divider

### Requirement: Left sidebar shows a compact usage-meter footer
The sidebar SHALL render a compact 5-hour/7-day usage footer at the bottom of the panel (not
per-workspace — this is account-global data), sourced from the same smuggled-field mechanism as
the Claude-state and openspec/beads segments, reusing cc-tmux's existing color thresholds (CYAN
below 50%, YELLOW 50-80%, RED above 80%).

#### Scenario: footer shows both windows with threshold coloring
- Given: the smuggled usage data decodes to 5H 68% and 7D 47%
- When: the sidebar renders its footer
- Then: it shows both percentages, the 5H figure colored per the >=50%/<80% (YELLOW) threshold
  and the 7D figure colored per the <50% (CYAN) threshold

#### Scenario: missing usage data renders no footer
- Given: no smuggled usage data is present or it fails to decode
- When: the sidebar renders
- Then: the footer is omitted entirely (no placeholder, no error text)

### Requirement: Right sidebar renders a gitkraken-style commit graph, SSH-aware
A browser panel SHALL render a visual commit graph for the focused workspace's git repository
(opened via `cmux browser open` from a script co-located with the workspace's CWD), generated from
`git log --graph --all --format=...` and rendered as HTML in the panel. The generating script
SHALL run wherever the CWD actually lives — for an SSH-backed workspace, that is the remote host,
with the rendered HTML reaching the Mac's browser panel over the same channel `cmux diff` already
uses for remote-invoked content.

#### Scenario: local workspace renders its own commit graph
- Given: a local (non-SSH) workspace whose CWD is a git repository
- When: the git-tree panel is opened for that workspace
- Then: it renders a commit graph reflecting that repository's actual history

#### Scenario: SSH-backed workspace renders the remote repository's graph
- Given: an SSH-backed workspace (`cmux ssh` to a remote host) whose CWD is a git repository on
  that remote host
- When: the git-tree panel is opened for that workspace
- Then: it renders a commit graph reflecting the REMOTE repository's history, not any local
  repository of the same name

#### Scenario: non-git directory shows no graph
- Given: a workspace whose CWD has no `.git` at its root
- When: the git-tree panel is opened
- Then: it renders an empty/placeholder state, not an error

### Requirement: Right sidebar renders a full usage-meter dashboard
A browser panel SHALL render a full multi-account 5H/7D usage dashboard (per-account progress
bars, reset countdowns, summary header row) by querying `http://localhost:7400/credentials`
directly from the panel's own JavaScript, reusing the color-threshold and percentage-formatting
logic ported from `cc-tmux`'s `usage.py`.

#### Scenario: dashboard renders per-account progress bars
- Given: nexus-agent's `/credentials` endpoint returns at least one account with resolvable
  5H/7D utilization
- When: the usage dashboard panel is opened
- Then: it renders a progress bar per utilization window per account, colored per the same
  CYAN/YELLOW/RED thresholds as the compact footer

#### Scenario: unreachable nexus-agent degrades to an empty state
- Given: nexus-agent is unreachable from the Mac
- When: the usage dashboard panel is opened
- Then: it renders an empty/unavailable state, not a JavaScript error surfaced to the user
