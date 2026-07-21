// See clipboard.go for the ClipboardDispatcher contract under test here —
// tasks.md [4.2]: the OSC52 path, exec.LookPath fallback order under a
// faked $PATH, and the surfaced (not swallowed) failure when nothing
// resolves.
package dispatch

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// fakeTTY is a hermetic stand-in for the real /dev/tty openTTY resolves to
// in production — captures every byte written so a test can assert the
// exact OSC52 escape payload.
type fakeTTY struct {
	buf    bytes.Buffer
	closed bool
}

func (f *fakeTTY) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *fakeTTY) Close() error                { f.closed = true; return nil }

func TestClipboardDispatchOSC52Path(t *testing.T) {
	fw := &fakeTTY{}
	c := &ClipboardDispatcher{
		ForceOSC52: true,
		goos:       "linux",
		openTTY:    func() (io.WriteCloser, error) { return fw, nil },
		detect:     func() bool { return false }, // ForceOSC52 must override this
		lookPath: func(name string) (string, error) {
			t.Fatalf("lookPath(%q) should never be called when OSC52 succeeds", name)
			return "", nil
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error {
			t.Fatalf("runPipe(%q) should never be called when OSC52 succeeds", name)
			return nil
		},
	}

	if err := c.Dispatch(context.Background(), store.Item{}, "hello world"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	want := fmt.Sprintf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte("hello world")))
	if fw.buf.String() != want {
		t.Fatalf("OSC52 payload mismatch:\n got  %q\n want %q", fw.buf.String(), want)
	}
	if !fw.closed {
		t.Fatal("want the tty writer closed after the OSC52 write")
	}
}

func TestClipboardDispatchOSC52DetectGatesWhenNotForced(t *testing.T) {
	detectCalled := false
	fw := &fakeTTY{}
	c := &ClipboardDispatcher{
		ForceOSC52: false,
		goos:       "linux",
		openTTY:    func() (io.WriteCloser, error) { return fw, nil },
		detect:     func() bool { detectCalled = true; return true },
		lookPath: func(name string) (string, error) {
			t.Fatalf("lookPath(%q) should never be called when detect() says OSC52 is supported and the write succeeds", name)
			return "", nil
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error { return nil },
	}

	if err := c.Dispatch(context.Background(), store.Item{}, "hi"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if !detectCalled {
		t.Fatal("want detect() consulted when ForceOSC52 is false")
	}
	if fw.buf.Len() == 0 {
		t.Fatal("want an OSC52 write to have happened")
	}
}

func TestClipboardDispatchOSC52FailureFallsThroughToPbcopyFallback(t *testing.T) {
	var ranPipe []string
	c := &ClipboardDispatcher{
		ForceOSC52: true,
		goos:       "linux",
		openTTY:    func() (io.WriteCloser, error) { return nil, errors.New("no controlling terminal") },
		detect:     func() bool { return true },
		lookPath: func(name string) (string, error) {
			if name == "xclip" {
				return "/usr/bin/xclip", nil
			}
			return "", errors.New("not found")
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error {
			ranPipe = append(ranPipe, name)
			return nil
		},
	}

	if err := c.Dispatch(context.Background(), store.Item{}, "hi"); err != nil {
		t.Fatalf("want a successful fallback after OSC52 write failure, got %v", err)
	}
	if len(ranPipe) != 1 || ranPipe[0] != "/usr/bin/xclip" {
		t.Fatalf("want exactly one fallback invocation of xclip, got %v", ranPipe)
	}
}

// TestClipboardDispatchFallbackOrderNeverAttemptsPbcopyBinary is tasks.md
// [4.2]'s named case: under a faked $PATH resolving only "xclip", the
// fallback must never actually invoke the literal "pbcopy" binary name —
// goos is set to "darwin" specifically so pbcopy IS a candidate in
// fallbackOrder (proving it was skipped because it failed to resolve, not
// because this GOOS excludes it outright — see the sibling
// TestClipboardDispatchFallbackOrderExcludesPbcopyOnNonDarwin below for
// that separate case).
func TestClipboardDispatchFallbackOrderNeverAttemptsPbcopyBinary(t *testing.T) {
	var lookedUp []string
	var ranPipe []struct {
		name string
		args []string
	}
	c := &ClipboardDispatcher{
		ForceOSC52: false,
		goos:       "darwin",
		detect:     func() bool { return false },
		lookPath: func(name string) (string, error) {
			lookedUp = append(lookedUp, name)
			if name == "xclip" {
				return "/usr/bin/xclip", nil
			}
			return "", errors.New("not found on this fixture $PATH")
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error {
			ranPipe = append(ranPipe, struct {
				name string
				args []string
			}{name, args})
			return nil
		},
	}

	if err := c.Dispatch(context.Background(), store.Item{}, "prompt text"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}

	// pbcopy WAS a candidate (goos=="darwin") and its resolution was
	// attempted (LookPath called with "pbcopy")...
	if !contains(lookedUp, "pbcopy") {
		t.Fatalf("want pbcopy's resolution attempted as a darwin candidate, lookups: %v", lookedUp)
	}
	// ...but since this fixture's $PATH resolves only xclip, pbcopy must
	// never actually be RUN.
	for _, r := range ranPipe {
		if r.name == "pbcopy" {
			t.Fatalf("pbcopy must never actually be invoked when it fails to resolve, got runPipe calls: %v", ranPipe)
		}
	}
	if len(ranPipe) != 1 || ranPipe[0].name != "/usr/bin/xclip" {
		t.Fatalf("want exactly one successful fallback invocation, of xclip's resolved path, got %v", ranPipe)
	}
	if got, want := ranPipe[0].args, []string{"-selection", "clipboard"}; !equalStrings(got, want) {
		t.Fatalf("want xclip invoked with %v, got %v", want, got)
	}
}

func TestClipboardDispatchFallbackOrderExcludesPbcopyOnNonDarwin(t *testing.T) {
	var lookedUp []string
	c := &ClipboardDispatcher{
		goos:   "linux",
		detect: func() bool { return false },
		lookPath: func(name string) (string, error) {
			lookedUp = append(lookedUp, name)
			if name == "xclip" {
				return "/usr/bin/xclip", nil
			}
			return "", errors.New("not found")
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error { return nil },
	}

	if err := c.Dispatch(context.Background(), store.Item{}, "prompt"); err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if contains(lookedUp, "pbcopy") {
		t.Fatalf("pbcopy must never even be looked up on a non-darwin GOOS, lookups: %v", lookedUp)
	}
}

func TestClipboardDispatchFailureSurfacedNotSwallowedWhenNothingResolves(t *testing.T) {
	c := &ClipboardDispatcher{
		goos:   "linux",
		detect: func() bool { return false },
		lookPath: func(name string) (string, error) {
			return "", errors.New("not found")
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error {
			t.Fatal("runPipe should never be called when nothing resolved")
			return nil
		},
	}

	err := c.Dispatch(context.Background(), store.Item{}, "prompt")
	if err == nil {
		t.Fatal("want a surfaced error when no clipboard mechanism resolves, got nil (silent no-op)")
	}
	if !strings.Contains(err.Error(), "no clipboard mechanism available") {
		t.Fatalf("want a descriptive surfaced error, got: %v", err)
	}
}

func TestClipboardDispatchFailureSurfacedWhenResolvedBinaryFails(t *testing.T) {
	c := &ClipboardDispatcher{
		goos:   "linux",
		detect: func() bool { return false },
		lookPath: func(name string) (string, error) {
			if name == "xclip" {
				return "/usr/bin/xclip", nil
			}
			return "", errors.New("not found")
		},
		runPipe: func(ctx context.Context, name string, args []string, stdin []byte) error {
			return errors.New("exit status 1")
		},
	}

	err := c.Dispatch(context.Background(), store.Item{}, "prompt")
	if err == nil {
		t.Fatal("want the resolved-but-failing binary's error surfaced, got nil")
	}
	if !strings.Contains(err.Error(), "xclip") {
		t.Fatalf("want the error to name which fallback failed, got: %v", err)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
