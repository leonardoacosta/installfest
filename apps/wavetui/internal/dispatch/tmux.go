// See dispatch.go for the package-level Dispatcher contract. This file
// implements TmuxDispatcher — see openspec/changes/wavetui-dispatch/
// tasks.md [2.1]/[2.2] and design.md § TmuxDispatcher primitive choice /
// § Target resolution / § Mid-turn safety.
package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// cc-tmux's own pane-option names — the same three names wavetui-sessions'
// TmuxSource (internal/sources/tmux.go) already reads for ccStateOpt, plus
// the project/branch options this file additionally needs for candidate
// scoring (see resolveSelf below). Declared independently here rather than
// imported from the sources package: dispatch is not itself a "source"
// (never publishes to the bus), and sources/tmux.go exports nothing for
// this purpose — design.md § TmuxDispatcher primitive choice explicitly
// frames this as "re-implements... via the same primitive" (the tmux CLI
// invocation shape), not a cross-package type import.
const (
	ccStateOpt   = "@cc-state"
	ccProjectOpt = "@cc-project"
	ccBranchOpt  = "@cc-branch"
)

// ErrNoDispatchTarget is returned by TmuxDispatcher.Dispatch when there is
// no tmux target to resolve at all — either `cc-tmux conductor list --json`
// returned an empty array, or the command itself failed (treated as "no
// $TMUX session at all") — see design.md § Target resolution point 3. This
// is the ONE error Resolver (resolver.go) treats as "fall back to
// ClipboardDispatcher"; every other error (an ambiguous tie, a refusal, or
// a genuine tmux failure once a target was actually found) propagates to
// the caller unchanged.
var ErrNoDispatchTarget = errors.New("no dispatch target: no linked session and no tmux candidates")

// conductorPane is one row of `cc-tmux conductor list --json` output — the
// shape is documented verbatim in apps/cc-tmux/skills/cc-dispatch/SKILL.md:
// "{id, session, window, state, project, branch, task, wait_reason,
// timestamp}". Task/WaitReason/Timestamp are declared for shape-fidelity
// (a later batch may want them for rendering) but are not read by any
// logic in this file — encoding/json's "unknown/unused fields ignored"
// tolerant-decode contract applies, same convention as sources/beads.go's
// beadRecord.
type conductorPane struct {
	ID         string  `json:"id"`
	Session    string  `json:"session"`
	Window     string  `json:"window"`
	State      string  `json:"state"`
	Project    string  `json:"project"`
	Branch     string  `json:"branch"`
	Task       string  `json:"task"`
	WaitReason string  `json:"wait_reason"`
	Timestamp  float64 `json:"timestamp"`
}

// Candidate is one scored tmux target considered during step 2 of design.md
// § Target resolution — the payload of an *AmbiguousTargetError, for a
// later batch's QueuePane to render as an inline "which pane?" prompt.
type Candidate struct {
	PaneID  string
	Session string
	Window  string
	Project string
	Branch  string
	State   string
	Score   int
}

// AmbiguousTargetError is returned when two or more scored candidates tie
// for the top score — design.md § Target resolution point 2: "A tie at the
// top score PROMPTS the operator... never a silent pick." A caller MUST
// NOT pick Candidates[0] automatically; it must surface the list.
type AmbiguousTargetError struct {
	Candidates []Candidate
}

func (e *AmbiguousTargetError) Error() string {
	return fmt.Sprintf("dispatch target is ambiguous: %d candidates tied for the top score", len(e.Candidates))
}

// selfContext is wavetui's OWN pane's tmux session/window identifiers plus
// its cc-tmux project/branch pane options — resolved once per resolveTarget
// call from $TMUX_PANE, used only to rank scored candidates (see
// candidateScore). Every field is "" when unavailable (wavetui running
// outside any tmux pane, or outside a cc-tmux-tagged one) and is treated as
// "no opinion on this dimension" by candidateScore, never a hard
// requirement — the same fail-open convention every cc-tmux-option read in
// this codebase already follows (see sources/tmux.go's ShowOption).
type selfContext struct {
	session, window, project, branch string
}

// Scoring weights for candidateScore.
//
// design.md § Target resolution names all four `conductor list --json`
// fields (project, branch, window, session) as the data used to score
// candidates ("same-window > same-session > other... matched against the
// item's project/branch") without fully specifying how the two dimensions
// compose — flagged and resolved here, documented in design.md's own
// addendum: locality (does the candidate share wavetui's own tmux
// window/session) decides the PRIMARY tier; project/branch affinity breaks
// ties WITHIN a tier. The gap between tiers (10) exceeds the largest
// possible affinity bonus (3 = scoreProjectMatch+scoreBranchMatch), so
// affinity can never promote an "other" candidate above a "same-session"
// one, or a "same-session" candidate above a "same-window" one — it only
// discriminates among otherwise-equal candidates.
const (
	scoreSameWindow   = 20
	scoreSameSession  = 10
	scoreOther        = 0
	scoreProjectMatch = 2
	scoreBranchMatch  = 1
)

// candidateScore ranks one candidate pane against wavetui's own tmux
// context. See the scoring-weights doc comment above for the tiering
// rationale.
func candidateScore(p conductorPane, self selfContext) int {
	score := scoreOther
	switch {
	case self.window != "" && p.Window == self.window:
		score = scoreSameWindow
	case self.session != "" && p.Session == self.session:
		score = scoreSameSession
	}
	if self.project != "" && p.Project == self.project {
		score += scoreProjectMatch
		if self.branch != "" && p.Branch == self.branch {
			score += scoreBranchMatch
		}
	}
	return score
}

// scoreCandidates scores every pane and returns the sole top-scoring one,
// or an *AmbiguousTargetError naming every candidate tied for the top score
// (2+ candidates) — design.md § Target resolution point 2. A single
// candidate is never ambiguous regardless of its score (there is nothing to
// tie against) — including the degenerate case where self is entirely
// unresolved (resolveSelf fail-open) and every pane scores 0: with exactly
// one pane, that is still an unambiguous, if low-confidence, pick. Two or
// more panes all scoring 0 under an unresolved self DOES tie, and correctly
// so — with no locality or affinity signal to distinguish them, prompting
// is the only safe behavior (design.md's own "never a silent pick"
// invariant), not a special case to work around.
func scoreCandidates(panes []conductorPane, self selfContext) (conductorPane, *AmbiguousTargetError) {
	type scored struct {
		pane  conductorPane
		score int
	}
	top := -1
	var best []scored
	for _, p := range panes {
		sc := candidateScore(p, self)
		switch {
		case sc > top:
			top = sc
			best = []scored{{p, sc}}
		case sc == top:
			best = append(best, scored{p, sc})
		}
	}
	if len(best) > 1 {
		cands := make([]Candidate, len(best))
		for i, b := range best {
			cands[i] = Candidate{
				PaneID:  b.pane.ID,
				Session: b.pane.Session,
				Window:  b.pane.Window,
				Project: b.pane.Project,
				Branch:  b.pane.Branch,
				State:   b.pane.State,
				Score:   b.score,
			}
		}
		return conductorPane{}, &AmbiguousTargetError{Candidates: cands}
	}
	return best[0].pane, nil
}

// tmuxRunner is the shell-out boundary TmuxDispatcher depends on, so tests
// can inject a stub instead of actually invoking tmux/cc-tmux — the
// identical hermetic-testing rationale as sources/tmux.go's tmuxCLI.
// execTmuxRunner (below) is the only implementation that ever touches
// os/exec.
type tmuxRunner interface {
	// ConductorList runs `cc-tmux conductor list --json` — pane enumeration,
	// reused as-is per design.md § TmuxDispatcher primitive choice.
	ConductorList(ctx context.Context) ([]byte, error)
	// ConductorSwitch runs `cc-tmux conductor dispatch --mode switch
	// --target <paneID>` — the no-paste "just look at this pane" action,
	// reused as-is per design.md § TmuxDispatcher primitive choice.
	ConductorSwitch(ctx context.Context, paneID string) error
	// DisplayMessage evaluates a tmux format string against paneID via
	// `tmux display-message -p -t <paneID> -F <format>` — used for
	// `#{pane_in_mode}` and for reading wavetui's own pane's session/window
	// identifiers. ok=false means the pane does not exist or the format
	// could not be evaluated (tmux exits non-zero) — fail-open, never a
	// hard error.
	DisplayMessage(ctx context.Context, paneID, format string) (value string, ok bool)
	// ShowOption reads a single pane option via `tmux show-options -p -v -t
	// <paneID> <option>` — the exact primitive wavetui-sessions' TmuxSource
	// already reads for @cc-state (design.md § TmuxDispatcher primitive
	// choice). ok=false means the option is unset or the pane does not
	// exist (tmux's own fail-open shape) — never a hard error.
	ShowOption(ctx context.Context, paneID, option string) (value string, ok bool)
	// LoadBuffer runs `tmux load-buffer -b <bufName> -`, feeding data via
	// stdin — prompt text never crosses as a command-line argument.
	LoadBuffer(ctx context.Context, bufName string, data []byte) error
	// PasteBuffer runs `tmux paste-buffer -b <bufName> -p -t <paneID>` —
	// `-p` requests bracketed-paste wrapping (design.md § TmuxDispatcher
	// primitive choice).
	PasteBuffer(ctx context.Context, bufName, paneID string) error
	// DeleteBuffer runs `tmux delete-buffer -b <bufName>` — best-effort
	// cleanup, called via defer regardless of PasteBuffer's outcome.
	DeleteBuffer(ctx context.Context, bufName string) error
	// SendKeysEnter runs `tmux send-keys -t <paneID> Enter` — ALWAYS a
	// separate call from PasteBuffer, never appended to a single command;
	// see sendPrompt below for the non-negotiable invariant this protects.
	SendKeysEnter(ctx context.Context, paneID string) error
}

type execTmuxRunner struct{}

func (execTmuxRunner) ConductorList(ctx context.Context) ([]byte, error) {
	return runOut(ctx, "cc-tmux", "conductor", "list", "--json")
}

func (execTmuxRunner) ConductorSwitch(ctx context.Context, paneID string) error {
	_, err := runOut(ctx, "cc-tmux", "conductor", "dispatch", "--mode", "switch", "--target", paneID)
	return err
}

func (execTmuxRunner) DisplayMessage(ctx context.Context, paneID, format string) (string, bool) {
	out, err := runOut(ctx, "tmux", "display-message", "-p", "-t", paneID, "-F", format)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (execTmuxRunner) ShowOption(ctx context.Context, paneID, option string) (string, bool) {
	out, err := runOut(ctx, "tmux", "show-options", "-p", "-v", "-t", paneID, option)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func (execTmuxRunner) LoadBuffer(ctx context.Context, bufName string, data []byte) error {
	cmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", bufName, "-")
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (execTmuxRunner) PasteBuffer(ctx context.Context, bufName, paneID string) error {
	_, err := runOut(ctx, "tmux", "paste-buffer", "-b", bufName, "-p", "-t", paneID)
	return err
}

func (execTmuxRunner) DeleteBuffer(ctx context.Context, bufName string) error {
	_, err := runOut(ctx, "tmux", "delete-buffer", "-b", bufName)
	return err
}

func (execTmuxRunner) SendKeysEnter(ctx context.Context, paneID string) error {
	_, err := runOut(ctx, "tmux", "send-keys", "-t", paneID, "Enter")
	return err
}

// runOut runs name(args...), returning stdout or a descriptive error
// (including trimmed stderr) on failure — the same shape as sources'
// runJSON, re-declared here rather than imported since dispatch and
// sources are independent packages with no shared-utility file (mirrors
// this codebase's "sources never touch each other directly" precedent,
// extended to this downstream package).
func runOut(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// TmuxDispatcher delivers promptText into a tmux pane via bracketed paste —
// see design.md § TmuxDispatcher primitive choice. It resolves its own
// target from item (linked-pane priority, else scored candidates) rather
// than accepting a pre-resolved pane in its Dispatch signature, since
// Dispatcher.Dispatch's signature is deliberately narrow (design.md §
// Dispatcher interface) and item already carries everything the resolution
// needs.
type TmuxDispatcher struct {
	runner tmuxRunner
	// selfPane returns $TMUX_PANE — the pane wavetui's own process is
	// running in, used by resolveSelf. Overridable for hermetic tests.
	selfPane func() string
}

// NewTmuxDispatcher constructs a TmuxDispatcher backed by the real
// tmux/cc-tmux binaries.
func NewTmuxDispatcher() *TmuxDispatcher {
	return &TmuxDispatcher{
		runner:   execTmuxRunner{},
		selfPane: func() string { return os.Getenv("TMUX_PANE") },
	}
}

// Dispatch implements Dispatcher. It resolves item's tmux target (the
// linked pane if one exists, else a scored `cc-tmux conductor list --json`
// candidate), validates the resolved pane ID's shape, checks the two
// non-negotiable refusal conditions (copy-mode, mid-turn-streaming), and
// delivers promptText via the bracketed-paste sequence. See design.md §
// TmuxDispatcher primitive choice / § Mid-turn safety / § Target
// resolution.
func (d *TmuxDispatcher) Dispatch(ctx context.Context, item store.Item, promptText string) error {
	pane, err := d.resolveTarget(ctx, item)
	if err != nil {
		return err
	}
	// validateTmuxPaneID, not validateDispatchTarget: pane is a tmux pane
	// ID ("%12"-shaped), a different id-space from item.ID's bead/proposal
	// slug shape — see dispatch.go's doc comment on the two validators for
	// why they are deliberately separate regexes.
	if err := validateTmuxPaneID(pane); err != nil {
		return err
	}
	if err := d.checkRefusals(ctx, item, pane); err != nil {
		return err
	}
	return d.sendPrompt(ctx, pane, promptText)
}

// resolveTarget implements design.md § Target resolution points 1-2:
// prefer item's linked pane when one exists (skip scoring entirely), else
// score `cc-tmux conductor list --json` candidates. Point 3 (zero
// candidates / no $TMUX session at all -> ClipboardDispatcher) is signaled
// by returning ErrNoDispatchTarget, which Resolver (resolver.go) is the
// only caller that interprets.
func (d *TmuxDispatcher) resolveTarget(ctx context.Context, item store.Item) (string, error) {
	if item.Session != nil && item.Session.PaneID != "" {
		return item.Session.PaneID, nil
	}

	raw, err := d.runner.ConductorList(ctx)
	if err != nil {
		// design.md § Target resolution point 3: a CLI error here is one
		// of the two named "no $TMUX session at all" signals.
		return "", ErrNoDispatchTarget
	}

	var panes []conductorPane
	if jsonErr := json.Unmarshal(raw, &panes); jsonErr != nil {
		// Malformed output is not one of design.md's two named cases
		// (empty array / CLI error), but the safe degrade is identical:
		// no usable candidate data, so fall back rather than guess at a
		// partially-parsed result.
		return "", ErrNoDispatchTarget
	}
	if len(panes) == 0 {
		return "", ErrNoDispatchTarget
	}

	self := d.resolveSelf(ctx)
	best, ambiguous := scoreCandidates(panes, self)
	if ambiguous != nil {
		return "", ambiguous
	}
	return best.ID, nil
}

// resolveSelf reads wavetui's OWN pane's tmux session/window identifiers
// and cc-tmux project/branch pane options (via $TMUX_PANE) for use by
// candidateScore. Never gates the zero-candidates/CLI-error check in
// resolveTarget, which reads solely from conductor list's own raw output
// per design.md's literal wording — this is ranking data only. Every field
// fails open to "" when wavetui is not running inside any tmux pane, or
// inside one cc-tmux has not tagged with @cc-project/@cc-branch.
func (d *TmuxDispatcher) resolveSelf(ctx context.Context) selfContext {
	pane := d.selfPane()
	if pane == "" {
		return selfContext{}
	}
	var sc selfContext
	sc.session, _ = d.runner.DisplayMessage(ctx, pane, "#{session_name}")
	// #{window_index} (a bare number, e.g. "5"), NOT #{window_id} (the
	// "@N" form) — verified live against this environment's own tmux
	// server during authoring: cc-tmux's own get_hop_panes format string
	// (apps/cc-tmux/src/cc_tmux/tmux.py) builds its "window" field from
	// #{window_index}, and `conductor list --json` confirmed emits bare
	// index values ("5", "4", ...) for real panes, never "@N". Using
	// #{window_id} here would have silently never matched any real
	// candidate's Window field — same-window scoring would have degraded
	// to always-"other" for every genuine same-window pane.
	sc.window, _ = d.runner.DisplayMessage(ctx, pane, "#{window_index}")
	sc.project, _ = d.runner.ShowOption(ctx, pane, ccProjectOpt)
	sc.branch, _ = d.runner.ShowOption(ctx, pane, ccBranchOpt)
	return sc
}

// checkRefusals implements design.md's two non-negotiable pre-paste checks.
//
// A pane in copy-mode would silently eat the paste, so it is refused
// rather than force-pasted through. A linked session that is actively
// streaming must not receive an interleaved prompt.
//
// "Actively streaming" has no dedicated store.SessionLink field (see
// design.md's Mid-turn safety addendum documenting this resolution) — the
// concrete, available signal is cc-tmux's own @cc-state pane option
// reading "active", which is exactly the busy/mid-turn signal cc-tmux's
// own send-prompt refusal (conductor.py's _send_prompt_refusal) already
// checks before typing into a pane. Zombie==false guards against refusing
// a session forever whose pane still happens to read "active" despite the
// session itself being stuck (wavetui-sessions' zombie detection already
// treats tmux-state and transcript-activity as two independent signals —
// this reuses that same independence). Fail-open when @cc-state has no
// data for this pane (not ok) — the same convention as every other
// cc-tmux-option read in this codebase.
func (d *TmuxDispatcher) checkRefusals(ctx context.Context, item store.Item, pane string) error {
	if inMode, ok := d.runner.DisplayMessage(ctx, pane, "#{pane_in_mode}"); ok && inMode == "1" {
		return ErrPaneInCopyMode
	}

	if item.Session != nil && !item.Session.Zombie {
		if state, ok := d.runner.ShowOption(ctx, pane, ccStateOpt); ok && state == "active" {
			return ErrSessionStreaming
		}
	}

	return nil
}

// sendPrompt delivers prompt into pane via bracketed paste: a fresh named
// buffer loaded via stdin, pasted with `-p` (bracketed-paste wrapping), and
// a SEPARATE send-keys Enter call — never a single `send-keys -l` carrying
// the full multi-line prompt. This is the exact gap design.md § TmuxDispatcher
// primitive choice cites in cc-tmux's own `_dispatch_send_prompt`
// (`send-keys -t <target> -l <prompt>` typing the prompt as literal
// keystrokes, including any embedded newline) and the reason this whole
// hybrid design exists — see also the package-level safety invariant.
func (d *TmuxDispatcher) sendPrompt(ctx context.Context, pane, prompt string) error {
	bufName := fmt.Sprintf("wavetui-dispatch-%d", time.Now().UnixNano())
	if err := d.runner.LoadBuffer(ctx, bufName, []byte(prompt)); err != nil {
		return err
	}
	defer d.runner.DeleteBuffer(ctx, bufName)
	if err := d.runner.PasteBuffer(ctx, bufName, pane); err != nil {
		return err
	}
	return d.runner.SendKeysEnter(ctx, pane)
}

// Switch moves tmux focus to paneID with no paste — reuses cc-tmux
// `conductor dispatch --mode switch` as-is (design.md § TmuxDispatcher
// primitive choice: "Reused as-is"). Deliberately not part of the
// Dispatcher interface: it is a UI-only "just look at this pane" action, a
// later batch's QueuePane concern, never invoked as part of an item's
// Dispatch call.
func (d *TmuxDispatcher) Switch(ctx context.Context, paneID string) error {
	if err := validateTmuxPaneID(paneID); err != nil {
		return err
	}
	return d.runner.ConductorSwitch(ctx, paneID)
}
