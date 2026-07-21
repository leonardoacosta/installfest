// See dispatch.go for the package-level Dispatcher contract this file's
// Spawner interface deliberately does NOT extend — see openspec/changes/
// wavetui-decision-lanes/tasks.md [2.1]/[2.2] and design.md § Spawn gap /
// § Spawn prompt template.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// Spawner creates a NEW target (unlike Dispatcher, which always resolves to
// an existing one) and starts a claude session in it, delivering promptText
// as that session's opening prompt. Returns the new pane's ID so callers
// (internal/lanes) can track liveness via wavetui-sessions' TmuxSource
// without re-deriving pane identity. See design.md § Spawn gap for why this
// is a sibling interface in the SAME package rather than a method added to
// Dispatcher: a spawn action has no already-known store.Item to resolve
// against (Dispatcher.Dispatch's signature takes one for exactly that
// resolution), so Spawn's signature is deliberately item-free.
type Spawner interface {
	Spawn(ctx context.Context, promptText string) (paneID string, err error)
}

// ErrSpawnNoNewPane is returned when a spawn-task dispatch reported success
// but no new pane appeared in a `cc-tmux conductor list --json` before/after
// diff — see TmuxSpawner.Spawn's doc comment for why a diff, rather than
// parsing dispatch's own stdout, is how the new pane's ID is discovered at
// all, and why "spawn succeeded, but no new pane found" is treated as a hard
// error (fail-closed) rather than silently returning an empty paneID.
var ErrSpawnNoNewPane = errors.New("cc-tmux spawn-task reported success but no new pane was found")

// spawnRunner is the shell-out boundary TmuxSpawner depends on, so tests can
// inject a stub instead of actually invoking tmux/cc-tmux — the identical
// hermetic-testing rationale as tmux.go's tmuxRunner. Deliberately a
// separate, narrower interface from tmuxRunner rather than an added method
// on it: tmuxRunner already has a shipped test double (tmux_test.go's
// mockRunner, from wavetui-dispatch's own tasks.md [4.1]) that this batch
// must not touch or break by widening the interface's method set out from
// under it. execTmuxSpawnRunner (below) satisfies spawnRunner by reusing
// execTmuxRunner's ConductorList implementation and runOut helper — same
// exec/shelling boilerplate, not copy-pasted — while adding only the one new
// primitive (ConductorSpawnTask) neither tmuxRunner nor execTmuxRunner
// needed before this proposal.
type spawnRunner interface {
	// ConductorList runs `cc-tmux conductor list --json` — the SAME
	// primitive tmuxRunner.ConductorList already wraps (see tmux.go), reused
	// here (not reimplemented) as the before/after diff basis for
	// discovering a spawned pane's new ID.
	ConductorList(ctx context.Context) ([]byte, error)
	// ConductorSpawnTask runs the cc-tmux spawn-task dispatch primitive —
	// see execTmuxSpawnRunner's ConductorSpawnTask doc comment below for the
	// --prompt-file -> --prompt correction from design.md's sketch.
	ConductorSpawnTask(ctx context.Context, promptText string) error
}

// execTmuxSpawnRunner is the real spawnRunner, backed by the actual
// tmux/cc-tmux binaries. It embeds execTmuxRunner (tmux.go) so ConductorList
// is reused verbatim rather than re-implemented — the only method this type
// adds is ConductorSpawnTask.
type execTmuxSpawnRunner struct {
	execTmuxRunner
}

// ConductorSpawnTask shells `cc-tmux conductor dispatch --mode spawn-task
// --prompt <promptText>`.
//
// RESOLVED (API batch, tasks.md [2.1]): design.md § Spawn gap's sketch names
// a `--prompt-file <path>` flag, framed as avoiding "shell-arg string
// interpolation of promptText itself" the same way sendPrompt (tmux.go)
// avoids it for TmuxDispatcher's normal paste flow. Reading cc-tmux's own
// argparse wiring directly (apps/cc-tmux/src/cc_tmux/parser.py's
// p_conductor.add_argument block) confirms no `--prompt-file` flag exists on
// the real CLI — only `--prompt <string>`, the same flag `send-prompt` mode
// already uses. This is NOT the same hazard sendPrompt's doc comment
// describes: that concern is about a promptText containing newlines/shell
// metacharacters being interpolated into a SHELL command line (a string
// handed to /bin/sh -c). exec.CommandContext never invokes a shell — args
// are passed directly to the child process's argv, so a multi-line
// promptText as one argv element carries through byte-for-byte with no
// escaping/interpolation risk at all, the same way every other call in this
// file and tmux.go already passes paneID/format/option strings as argv
// elements. What conductor.py's own `_open_window` does internally once it
// receives that arg (`tmux send-keys -t <pane> -l <prompt>`, typing the
// prompt as literal keystrokes rather than a bracketed paste) is cc-tmux's
// implementation, not this call's — out of scope for this proposal to
// change, and orthogonal to whether the CLI invocation itself is safe.
func (execTmuxSpawnRunner) ConductorSpawnTask(ctx context.Context, promptText string) error {
	_, err := runOut(ctx, "cc-tmux", "conductor", "dispatch", "--mode", "spawn-task", "--prompt", promptText)
	return err
}

// TmuxSpawner creates a new claude pane via cc-tmux's spawn-task dispatch
// mode — see design.md § Spawn gap. Distinct from TmuxDispatcher: Spawn
// creates a target that does not exist yet and never resolves to an
// existing one, so it shares none of TmuxDispatcher's target-resolution or
// pre-paste refusal logic.
type TmuxSpawner struct {
	runner spawnRunner
}

// NewTmuxSpawner constructs a TmuxSpawner backed by the real tmux/cc-tmux
// binaries.
func NewTmuxSpawner() *TmuxSpawner {
	return &TmuxSpawner{runner: execTmuxSpawnRunner{}}
}

// Spawn implements Spawner.
//
// RESOLVED (API batch, tasks.md [2.1]): design.md § Spawn gap's sketch says
// to "parse `out` for the new pane ID" from the spawn-task dispatch call
// itself. Reading cc-tmux's own `_dispatch_spawn_task`/`_open_window`
// (apps/cc-tmux/src/cc_tmux/conductor.py) directly confirms neither writes
// the new pane's ID to stdout on success — only an exit code, with the pane
// ID appearing solely in an stderr message on a SEEDING failure (a degraded
// case, not the success path). There is nothing in dispatch's own output to
// parse. What design.md's sketch DOES already name as reused, not
// reinvented — "cc-tmux's `conductor list --json` shape... is the same
// shape wavetui-dispatch's Resolver already parses... for confirming the
// new pane registered" — is the mechanism this implements literally: take a
// `conductor list --json` snapshot before the spawn call, one after, and the
// set difference IS "confirming the new pane registered." This is safe
// because `_open_window` blocks synchronously until the new pane is
// (readiness-polled and, on timeout, best-effort) seeded before returning,
// so by the time ConductorSpawnTask's exec call returns, the pane already
// exists in tmux's own state for the immediately-following ConductorList
// call to see.
//
// Deliberately fail-closed (returns an error), unlike TmuxDispatcher's
// target-resolution fail-open convention: a wrong or empty paneID returned
// here would become ground truth for internal/lanes' liveness tracking
// (design.md § Lane liveness), so a "before" list that cannot be read, or an
// ambiguous (not exactly 1) diff result, is reported rather than guessed at.
func (s *TmuxSpawner) Spawn(ctx context.Context, promptText string) (string, error) {
	before, err := s.paneIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("spawn: listing panes before dispatch: %w", err)
	}

	if err := s.runner.ConductorSpawnTask(ctx, promptText); err != nil {
		return "", fmt.Errorf("cc-tmux conductor dispatch --mode spawn-task: %w", err)
	}

	after, err := s.paneIDs(ctx)
	if err != nil {
		return "", fmt.Errorf("spawn: listing panes after dispatch: %w", err)
	}

	newIDs := diffNewPaneIDs(before, after)
	switch len(newIDs) {
	case 1:
		return newIDs[0], nil
	case 0:
		return "", ErrSpawnNoNewPane
	default:
		return "", fmt.Errorf("spawn: dispatch produced %d new panes, expected exactly 1: %v", len(newIDs), newIDs)
	}
}

// paneIDs runs `cc-tmux conductor list --json` and returns just the pane IDs
// — reuses conductorPane (tmux.go), the same type/JSON shape
// TmuxDispatcher.resolveTarget already parses, per design.md's explicit
// "reused, not reinvented" instruction.
func (s *TmuxSpawner) paneIDs(ctx context.Context) (map[string]bool, error) {
	raw, err := s.runner.ConductorList(ctx)
	if err != nil {
		return nil, err
	}
	var panes []conductorPane
	if err := json.Unmarshal(raw, &panes); err != nil {
		return nil, fmt.Errorf("parsing conductor list --json output: %w", err)
	}
	ids := make(map[string]bool, len(panes))
	for _, p := range panes {
		ids[p.ID] = true
	}
	return ids, nil
}

// diffNewPaneIDs returns the IDs present in after but not in before, sorted
// for deterministic output (a stable order matters for the "expected
// exactly 1" error message above to be reproducible in tests).
func diffNewPaneIDs(before, after map[string]bool) []string {
	var newIDs []string
	for id := range after {
		if !before[id] {
			newIDs = append(newIDs, id)
		}
	}
	sortStrings(newIDs)
	return newIDs
}

// sortStrings is a tiny insertion sort — avoids pulling in "sort" for a
// handful of pane IDs per call; kept local since this is the only sort this
// file needs.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// RenderSpawnPrompt renders the spawn-target's opening prompt for item — see
// design.md § Spawn prompt template (the "capture-back contract"). Pure
// render function: no side effects, no shell, no I/O. This is the exact
// prompt text a spawned Claude Code session reads and acts on, not an
// internal code comment — every instruction below is written for that
// session's benefit, not this codebase's.
//
// RESOLVED (API batch, tasks.md [2.2]): design.md's template cites
// `{item.SourcePath}` for the openspec-backed persistence instruction, but
// store.Item carries no SourcePath field (confirmed: internal/store/
// store.go's Item struct has no such field, and internal/sources/openspec.go
// never populates one). For a KindProposal item, item.ID IS the proposal's
// slug (see openspec.go's parseOneProposal: `item.ID = slug`, with the file
// itself always at `openspec/changes/<slug>/proposal.md`), so the path is
// fully derivable from item.ID + item.Kind without a new store field —
// proposalPath (below) does exactly that derivation.
func RenderSpawnPrompt(item store.Item) string {
	var blockerType, blockerReason, blockerRef string
	if item.Blocker != nil {
		blockerType = item.Blocker.Type
		blockerReason = item.Blocker.Reason
		blockerRef = item.Blocker.Ref
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are resolving a blocker on %s %s: %q.\n", item.Kind, item.ID, item.Title)
	fmt.Fprintf(&b, "Blocker: %s - %s\n", blockerType, blockerReason)
	fmt.Fprintf(&b, "Reference: %s\n\n", blockerRef)
	b.WriteString("Ask Leo whatever clarifying questions you need to resolve this blocker. Before you exit, you\n")
	b.WriteString("MUST persist the resolution somewhere this system can observe:\n")
	fmt.Fprintf(&b, "- For a bead-backed item: `bd comment %s --body \"<resolution>\"` or `bd update %s\n", item.ID, item.ID)
	b.WriteString("  --notes \"<updated note with the blocker replaced or removed>\"`.\n")
	if item.Kind == store.KindProposal {
		fmt.Fprintf(&b, "- For an openspec-backed item: edit `%s`'s `## Context` section, replacing the\n", proposalPath(item.ID))
	} else {
		b.WriteString("- For an openspec-backed item: edit `openspec/changes/<slug>/proposal.md`'s `## Context`\n")
		b.WriteString("  section, replacing the\n")
	}
	b.WriteString("  `blocked: ...` line with the resolution (or removing it if fully resolved).\n\n")
	b.WriteString("Do not exit without writing one of the above. A silent answer with nothing persisted leaves the\n")
	b.WriteString("blocker showing as unresolved indefinitely -- there is no other completion signal.\n\n")
	fmt.Fprintf(&b, "Reference: /apply %s\n", item.ID)
	return b.String()
}

// proposalPath derives an openspec proposal's on-disk path from its slug
// (item.ID) — see RenderSpawnPrompt's doc comment for why this replaces
// design.md's cited (non-existent) item.SourcePath field. Relative to the
// repo root, mirroring internal/sources/openspec.go's own
// filepath.Join(root, "openspec", "changes", slug, "proposal.md")
// construction — correct for the spawned pane's cwd since cc-tmux's
// spawn-task mode falls back to "the current pane's directory" when no
// explicit --target is given (see execTmuxSpawnRunner.ConductorSpawnTask:
// this call never passes --target), and wavetui itself always runs from the
// repo root.
func proposalPath(slug string) string {
	return filepath.Join("openspec", "changes", slug, "proposal.md")
}

var _ Spawner = (*TmuxSpawner)(nil)
