// See dispatch.go for the package-level Dispatcher contract. This file
// implements ClipboardDispatcher — see openspec/changes/wavetui-dispatch/
// tasks.md [2.3] and design.md § ClipboardDispatcher.
package dispatch

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// ClipboardDispatcher delivers promptText to the operator's clipboard
// rather than a tmux pane — see design.md § ClipboardDispatcher. Resolver
// (resolver.go) falls back to this Dispatcher only when TmuxDispatcher
// reports ErrNoDispatchTarget (zero candidates, or no $TMUX session at
// all); it is never the caller's first choice.
type ClipboardDispatcher struct {
	// ForceOSC52 overrides a false-negative OSC52 capability detection for
	// a terminal that supports the escape sequence but does not advertise
	// it — loaded from the per-project .wavetui.toml config by the caller
	// (cmd/wavetui/main.go, a later batch). Zero value (false) trusts
	// detection.
	ForceOSC52 bool

	// The fields below are the shell-out/IO/platform boundary, overridable
	// so tests never touch a real tty, the real $PATH, or a real terminal
	// — the same hermetic-testing rationale as tmuxRunner in tmux.go.
	goos     string
	openTTY  func() (io.WriteCloser, error)
	detect   func() bool
	lookPath func(string) (string, error)
	runPipe  func(ctx context.Context, name string, args []string, stdin []byte) error
}

// NewClipboardDispatcher constructs a ClipboardDispatcher backed by the
// real tty/terminfo/$PATH.
func NewClipboardDispatcher(forceOSC52 bool) *ClipboardDispatcher {
	return &ClipboardDispatcher{
		ForceOSC52: forceOSC52,
		goos:       runtime.GOOS,
		openTTY:    openDevTTY,
		detect:     detectOSC52Support,
		lookPath:   exec.LookPath,
		runPipe:    runPipeCommand,
	}
}

func openDevTTY() (io.WriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}

// detectOSC52Support is a best-effort, dependency-free feature check — no
// terminfo library dependency is added (config.go's "no third-party
// dependencies yet" constraint from wavetui-core applies equally here).
// $TERM_PROGRAM presence is treated as a fast positive signal (a terminal
// that identifies itself this way is a modern, actively-maintained
// emulator — plausibly OSC52-capable); otherwise the terminfo `Ms`
// capability (the standard name for the OSC52 "set clipboard" extension)
// is checked via `infocmp`, which is what design.md's "$TERM_PROGRAM/
// terminfo Ms capability presence" pairing names as the two signals. A
// terminal that supports OSC52 but trips neither check is a false
// negative here — exactly what ForceOSC52 exists to override.
func detectOSC52Support() bool {
	if os.Getenv("TERM_PROGRAM") != "" {
		return true
	}
	term := os.Getenv("TERM")
	if term == "" {
		return false
	}
	out, err := exec.Command("infocmp", "-1", term).Output()
	if err != nil {
		return false
	}
	return bytes.Contains(out, []byte("\tMs="))
}

// Dispatch implements Dispatcher. item is accepted only to satisfy the
// Dispatcher interface's fixed signature (design.md § Dispatcher
// interface) — ClipboardDispatcher never uses any item field in a shell
// invocation (promptText is the only payload, delivered via the OSC52
// escape sequence or piped to a clipboard binary's stdin, never via
// argv), so no dispatch-boundary ID validation applies here the way it
// does in TmuxDispatcher.
func (c *ClipboardDispatcher) Dispatch(ctx context.Context, _ store.Item, promptText string) error {
	if c.ForceOSC52 || c.detect() {
		if err := c.writeOSC52(promptText); err == nil {
			return nil
		}
		// OSC52 write failed (e.g. no controlling terminal, /dev/tty not
		// writable) — fall through to the pbcopy-equivalent path rather
		// than surfacing this specific failure, since a working fallback
		// still satisfies "deliver promptText to the clipboard."
	}
	return c.pbcopyFallback(ctx, promptText)
}

func (c *ClipboardDispatcher) writeOSC52(promptText string) error {
	tty, err := c.openTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	payload := fmt.Sprintf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(promptText)))
	_, err = tty.Write([]byte(payload))
	return err
}

// clipboardCandidate names a clipboard binary and the args to invoke it
// with promptText on stdin.
type clipboardCandidate struct {
	name string
	args []string
}

// fallbackOrder is this repo's own shell-alias fallback chain
// (home/dot_zsh/rc/linux.zsh's pbcopy alias, home/dot_zsh/rc/darwin.zsh's
// native-pbcopy note), re-implemented against real binaries via
// exec.LookPath — see design.md § ClipboardDispatcher's "gotcha this
// proposal must not repeat": a Go binary invoked directly never sees a
// shell alias, so the same fallback order is hard-coded here against real
// binary names instead. pbcopy is Darwin-only by construction (matching
// design.md's literal "pbcopy (Darwin only)") — on any other GOOS it is
// never even attempted, let alone looked up.
func (c *ClipboardDispatcher) fallbackOrder() []clipboardCandidate {
	var order []clipboardCandidate
	if c.goos == "darwin" {
		order = append(order, clipboardCandidate{"pbcopy", nil})
	}
	order = append(order,
		clipboardCandidate{"xclip", []string{"-selection", "clipboard"}},
		clipboardCandidate{"xsel", []string{"--clipboard", "--input"}},
		clipboardCandidate{"wl-copy", nil},
	)
	return order
}

// pbcopyFallback tries each candidate in fallbackOrder, using the first one
// exec.LookPath resolves. Surfaces failure (never silently no-ops) when
// none resolve, or when the resolved binary itself fails — design.md §
// ClipboardDispatcher: "surfacing failure rather than silently no-op'ing
// when none resolve."
func (c *ClipboardDispatcher) pbcopyFallback(ctx context.Context, promptText string) error {
	for _, cand := range c.fallbackOrder() {
		path, err := c.lookPath(cand.name)
		if err != nil {
			continue // not installed on this machine — try the next one
		}
		if err := c.runPipe(ctx, path, cand.args, []byte(promptText)); err != nil {
			return fmt.Errorf("clipboard fallback %s failed: %w", cand.name, err)
		}
		return nil
	}
	return errors.New("no clipboard mechanism available: OSC52 unsupported and none of pbcopy/xclip/xsel/wl-copy resolved on $PATH")
}

// runPipeCommand runs name(args...) with stdin piped in. It deliberately
// does NOT capture stderr via a Go-managed pipe (an io.Writer like
// bytes.Buffer, which an earlier version of this function used) — found via
// task [3.4]'s real-runtime verification against this exact fallback chain:
// every candidate in fallbackOrder that is actually reachable on a Linux
// box with no X11 (xclip/xsel — both, same X11-selection-ownership model)
// forks into the background to keep serving the clipboard selection after
// this process would otherwise exit (verified live against wl-copy on this
// machine: `wl-copy --help` documents this explicitly — "stay in the
// foreground instead of forking" is opt-in via -f). The daemonized
// grandchild inherits the pipe's write end; os/exec's own Wait/StderrPipe
// doc comment is explicit that Wait blocks until EVERY holder of a
// Go-managed pipe closes it — with a daemonizing grandchild holding it open
// indefinitely, Wait() (and this whole synchronous Dispatch call) hung
// forever on the SUCCESS path (reproduced live: a real wl-copy invocation
// that successfully set the clipboard left `go test` blocked >5 minutes).
// Routing stderr to os.DevNull instead (a real *os.File, dup2'd directly —
// no Go-side pipe/goroutine at all) removes the hang entirely; the
// trade-off is losing the trimmed stderr text in pbcopyFallback's wrapped
// error, an acceptable cost since the failure itself is still surfaced via
// a non-nil err (cmd.Run()'s own *exec.ExitError), never silently
// swallowed — design.md's "surfacing failure... when none resolve" is about
// not silently no-op'ing, not about diagnostic-text richness.
func runPipeCommand(ctx context.Context, name string, args []string, stdin []byte) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	if devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		defer devNull.Close()
		cmd.Stderr = devNull
	}
	return cmd.Run()
}
