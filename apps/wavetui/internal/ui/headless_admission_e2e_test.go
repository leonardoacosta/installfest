// TestHeadlessAdmissionEndToEnd is the E2E verification for tasks.md [3.1]
// (wavetui-headless-admission). It exercises the REAL wiring across both
// the daemon and ui packages — a real daemon.Controller wrapping a real
// daemon.HeadlessDispatcher, driven through a real ui.HeadlessBar.HandleKey
// keypress — with nothing faked or mocked at the Controller/HeadlessBar
// boundary. This lives in package ui (not package daemon) because ui
// imports daemon (HeadlessBar holds a *daemon.Controller) and Go forbids an
// internal daemon test file from importing ui, which imports daemon back —
// a genuine import cycle (confirmed empirically before writing this file).
//
// The only thing standing in for a real subprocess is `claude` itself: this
// test shims PATH so HeadlessDispatcher's OWN, UNMODIFIED, exported
// constructor (NewHeadlessDispatcher(cap, bus) — zero options, the default
// execHeadlessRunner backed by real os/exec) resolves "claude" to a small
// fake shell script instead of a real Claude Code invocation — the same
// "cheap, bounded real OS process standing in for `claude -p`" technique
// internal/daemon/e2e_process_lifecycle_test.go already established for
// tasks.md [4.1], just reached via PATH substitution instead of swapping
// the unexported `runner` field directly, since that field is inaccessible
// from outside package daemon (and headless_dispatcher.go is explicitly
// "None modified" per this proposal's own Impact table — proposal.md).
package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/daemon"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// installFakeClaude writes a fake `claude` executable into a fresh temp
// dir, prepends that dir to PATH (restored via t.Cleanup), and returns the
// path to the log file the fake script appends one line to per invocation:
// "PID=<pid> PROMPT=<promptText>". This is what lets the test recover, from
// OUTSIDE the daemon package, both WHICH items were actually dispatched (and
// in what order) and the real OS PID of each — the same "read real evidence,
// don't assume it" posture e2e_process_lifecycle_test.go's own `alive`
// helper takes.
//
// Any prompt containing "mid" sleeps briefly (0.3s) then exits on its own —
// standing in for a headless child that finishes naturally, so the test can
// prove a freed concurrency slot does NOT get admitted into once admission
// has been toggled off. Every other prompt sleeps long enough (10s) to
// still be running for the rest of the test — standing in for an
// already-dispatched child that must be left alone by a later toggle.
func installFakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "dispatch-log.txt")

	script := "#!/bin/sh\n" +
		"echo \"PID=$$ PROMPT=$2\" >> \"" + logPath + "\"\n" +
		"case \"$2\" in\n" +
		"  *mid*) sleep 0.3 ;;\n" +
		"  *) sleep 10 ;;\n" +
		"esac\n"
	scriptPath := filepath.Join(dir, "claude")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script: %v", err)
	}

	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("setenv PATH: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	return logPath
}

// e2eLogLine is one parsed "PID=<pid> PROMPT=<promptText>" line.
type e2eLogLine struct {
	pid    int
	prompt string
}

func readE2ELogLines(t *testing.T, path string) []e2eLogLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read dispatch log %s: %v", path, err)
	}
	var out []e2eLogLine
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " PROMPT=", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed dispatch log line %q", line)
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(parts[0], "PID="))
		if err != nil {
			t.Fatalf("bad PID in dispatch log line %q: %v", line, err)
		}
		out = append(out, e2eLogLine{pid: pid, prompt: parts[1]})
	}
	return out
}

// e2eWaitFor polls cond until true or timeout, failing the test otherwise —
// mirrors internal/daemon/headless_dispatcher_test.go's own waitFor, which
// is unexported and unreachable from this package.
func e2eWaitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

// e2eProcAlive reports whether pid is currently a live OS process, via
// `ps -p` — real evidence, matching e2e_process_lifecycle_test.go's `alive`.
func e2eProcAlive(pid int) bool {
	return exec.Command("ps", "-p", strconv.Itoa(pid)).Run() == nil
}

// TestHeadlessAdmissionEndToEnd covers all three behaviors named in
// tasks.md [3.1] in one real, cross-package flow:
//
//  1. order: two eligible items with different FanOutScore are dispatched
//     highest-first.
//  2. cap-stop: with a concurrency cap of 2 and three eligible items, the
//     third is never attempted this snapshot.
//  3. toggle-off: after disabling admission via the real HeadlessBar
//     keybinding, a subsequent Snapshot — even one where a slot has
//     genuinely freed up and the previously cap-refused item is still
//     eligible — admits nothing, and the still-running, already-dispatched
//     child is left alone (never killed, never re-dispatched).
func TestHeadlessAdmissionEndToEnd(t *testing.T) {
	logPath := installFakeClaude(t)

	// Real wiring throughout: real HeadlessDispatcher (default
	// execHeadlessRunner, zero options — headless_dispatcher.go is
	// unmodified), real Controller, real HeadlessBar.
	d := daemon.NewHeadlessDispatcher(2, bus.New())
	ctrl := daemon.NewController(d)
	bar := NewHeadlessBar(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // kills the still-running "high" fake child at test end

	if ctrl.AdmissionEnabled() {
		t.Fatal("admission must default to disabled")
	}

	// Real keypress: this is the exact call Root makes when "a" is pressed
	// while HeadlessBar has focus (see HandleKey).
	bar.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})
	if !ctrl.AdmissionEnabled() {
		t.Fatal("admission must be enabled after the toggle keypress")
	}

	snap1 := store.Snapshot{
		Items: []store.Item{
			{ID: "blocked", FanOutScore: 100, Blocker: &store.BlockerNote{Type: "manual", Reason: "waiting"}},
			{ID: "claimed", FanOutScore: 99, Session: &store.SessionLink{SessionID: "already-linked"}},
			{ID: "high", FanOutScore: 10},
			{ID: "mid", FanOutScore: 5},
			{ID: "low", FanOutScore: 1},
		},
	}
	ctrl.OnSnapshot(ctx, snap1)

	// --- Behavior 1 + 2: order and cap-stop ---
	var lines []e2eLogLine
	e2eWaitFor(t, 3*time.Second, func() bool {
		lines = readE2ELogLines(t, logPath)
		return len(lines) >= 2
	})
	// Give a generous settle window: if a latent bug admitted a 3rd item
	// (the cap-stop violation this test exists to catch), it would show up
	// here rather than being missed by stopping the read the instant 2
	// lines appear.
	time.Sleep(200 * time.Millisecond)
	lines = readE2ELogLines(t, logPath)

	if len(lines) != 2 {
		t.Fatalf("dispatch log after first snapshot = %+v, want exactly 2 lines (cap=2, third eligible item must not be attempted)", lines)
	}
	for _, l := range lines {
		if strings.Contains(l.prompt, "blocked") || strings.Contains(l.prompt, "claimed") {
			t.Fatalf("blocked/claimed item leaked into dispatch log: %+v", lines)
		}
	}

	// Order is asserted via PID allocation order, NOT log-write order: the
	// two forked children's own echo statements race the OS scheduler for
	// CPU time (confirmed empirically — log-write order flips between runs
	// even though Dispatch() is called for 'high' strictly before 'mid' on
	// a single goroutine). PID allocation, in contrast, happens
	// synchronously inside cmd.Start()'s fork+exec, in the SAME order
	// Dispatch() was called — Dispatch(high) fully returns (real PID
	// assigned) before Dispatch(mid) is even invoked — so a lower PID for
	// 'high' is a race-free proof that it was admitted first.
	var highLine, midLine *e2eLogLine
	for i := range lines {
		switch lines[i].prompt {
		case "/apply high":
			highLine = &lines[i]
		case "/apply mid":
			midLine = &lines[i]
		}
	}
	if highLine == nil || midLine == nil {
		t.Fatalf("dispatch log after first snapshot = %+v, want one '/apply high' line and one '/apply mid' line", lines)
	}
	if highLine.pid >= midLine.pid {
		t.Fatalf("PID order high=%d mid=%d — want high's PID lower than mid's (proof 'high', FanOutScore 10, was admitted/forked before 'mid', FanOutScore 5)", highLine.pid, midLine.pid)
	}
	t.Logf("EVIDENCE: dispatch order + cap-stop — %+v (blocked/claimed excluded; PID order high=%d < mid=%d proves FanOutScore-descending admission order; 'low' never attempted this snapshot)", lines, highLine.pid, midLine.pid)

	highPID := highLine.pid
	midPID := midLine.pid
	if !e2eProcAlive(highPID) {
		t.Fatalf("real child for 'high' (pid=%d) not alive right after dispatch", highPID)
	}
	t.Logf("EVIDENCE: real OS process for 'high' pid=%d confirmed alive via ps -p", highPID)

	// Let 'mid' exit on its own (its script sleeps 0.3s then returns),
	// freeing its concurrency slot — the precondition that makes the
	// toggle-off assertion below meaningful rather than a tautology: if
	// admission were still enabled, THIS freed slot is exactly what would
	// let the previously cap-refused 'low' item through on the next
	// snapshot.
	e2eWaitFor(t, 2*time.Second, func() bool { return !e2eProcAlive(midPID) })
	// Small grace window for HeadlessDispatcher's own awaitExit goroutine
	// (cmd.Wait() returning -> semaphore release -> HeadlessExitEvent) to
	// actually finish releasing the slot after the OS process itself has
	// already exited.
	time.Sleep(150 * time.Millisecond)
	t.Logf("EVIDENCE: real OS process for 'mid' pid=%d has exited naturally, freeing its concurrency slot", midPID)

	// --- Behavior 3: toggle off ---
	bar.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})
	if ctrl.AdmissionEnabled() {
		t.Fatal("admission must be disabled after the second toggle keypress")
	}

	// The toggle is a bare bool flip (Controller.ToggleAdmission) with no
	// path to the dispatcher's running children — confirm 'high' is
	// completely unaffected by it, immediately.
	if !e2eProcAlive(highPID) {
		t.Fatalf("'high' (pid=%d) died as a side effect of toggling admission off — already-dispatched items must be untouched", highPID)
	}

	snap2 := store.Snapshot{
		Items: []store.Item{
			{ID: "high", FanOutScore: 10},
			{ID: "low", FanOutScore: 1},
		},
	}
	ctrl.OnSnapshot(ctx, snap2)

	// Settle window generous enough that a real (buggy) admission attempt
	// for 'low' would have shown up in the log by now.
	time.Sleep(500 * time.Millisecond)
	linesAfterToggleOff := readE2ELogLines(t, logPath)
	if len(linesAfterToggleOff) != 2 {
		t.Fatalf("dispatch log after toggling admission off = %+v, want still exactly 2 lines — 'low' must NOT be admitted into the now-free slot while admission is disabled", linesAfterToggleOff)
	}
	t.Logf("EVIDENCE: toggling admission off stopped further admission on the next snapshot even though a slot had genuinely freed up (dispatch log unchanged: %+v)", linesAfterToggleOff)

	if !e2eProcAlive(highPID) {
		t.Fatalf("'high' (pid=%d) died after the second OnSnapshot call — already-dispatched items must remain untouched", highPID)
	}
	t.Logf("EVIDENCE: real OS process for 'high' pid=%d still alive and untouched after admission was toggled off and a further snapshot was processed", highPID)
}
