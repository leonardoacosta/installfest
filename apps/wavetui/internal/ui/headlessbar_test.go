// Tests for HeadlessBar — tasks.md [2.1] (wavetui-headless-admission): the
// admission-toggle keybinding and its View() indicator. The resume-on-"r"
// path (tasks.md [3.1], wavetui-daemon) has no dedicated test file of its
// own prior to this one, so newTestHeadlessBar below is shared scaffolding
// for both.
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/leonardoacosta/installfest/apps/wavetui/internal/bus"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/daemon"
	"github.com/leonardoacosta/installfest/apps/wavetui/internal/store"
)

// newTestHeadlessBar builds a HeadlessBar wired to a real daemon.Controller
// (Controller.ctrl is a concrete type, not an interface — see the ctrl
// field's own doc comment — so there is no fake to substitute). A real
// bus.New() satisfies daemon.EventBus trivially. This is safe to call
// Dispatch-adjacent methods against: ToggleAdmission only flips a guarded
// bool and never touches the dispatcher's runner, so no real `claude -p`
// subprocess is ever spawned by these tests.
func newTestHeadlessBar() (*HeadlessBar, *daemon.Controller) {
	d := daemon.NewHeadlessDispatcher(2, bus.New())
	ctrl := daemon.NewController(d)
	return NewHeadlessBar(ctrl), ctrl
}

func TestHeadlessBarAdmissionDisabledByDefault(t *testing.T) {
	h, ctrl := newTestHeadlessBar()

	if ctrl.AdmissionEnabled() {
		t.Fatal("admission must default to false — admission is opt-in, never on by default")
	}
	if got := h.View(); got != "" {
		t.Fatalf("View() with no toggle pressed and no queue state = %q, want empty string", got)
	}
}

func TestHeadlessBarTogglesAdmissionOnKeypress(t *testing.T) {
	h, ctrl := newTestHeadlessBar()

	h.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})

	if !ctrl.AdmissionEnabled() {
		t.Fatal("admission must be true after one admissionToggleKey press")
	}
	view := h.View()
	if !strings.Contains(view, "HEADLESS ADMISSION ON") {
		t.Fatalf("View() after enabling = %q, want it to contain %q", view, "HEADLESS ADMISSION ON")
	}
	if !strings.Contains(view, "press "+admissionToggleKey+" to disable") {
		t.Fatalf("View() after enabling = %q, want the disable hint for key %q", view, admissionToggleKey)
	}
}

func TestHeadlessBarTogglesAdmissionOffOnSecondKeypress(t *testing.T) {
	h, ctrl := newTestHeadlessBar()

	h.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})
	h.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})

	if ctrl.AdmissionEnabled() {
		t.Fatal("admission must be false again after a second admissionToggleKey press")
	}
	if got := h.View(); got != "" {
		t.Fatalf("View() after disabling (no other pending state) = %q, want empty string", got)
	}
}

func TestHeadlessBarAdmissionIndicatorShowsInFlightAndCap(t *testing.T) {
	h, _ := newTestHeadlessBar()
	h.HandleKey(tea.KeyPressMsg{Text: admissionToggleKey})

	h.Update(store.Snapshot{
		HeadlessQueue: &store.HeadlessQueueState{ActiveCount: 1, ConcurrencyCap: 2},
	})

	view := h.View()
	if !strings.Contains(view, "in-flight 1/2") {
		t.Fatalf("View() with a queue snapshot = %q, want it to contain %q", view, "in-flight 1/2")
	}
}

func TestHeadlessBarUnrelatedKeyDoesNotToggleAdmission(t *testing.T) {
	h, ctrl := newTestHeadlessBar()

	h.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	h.HandleKey(tea.KeyPressMsg{Text: "x"})

	if ctrl.AdmissionEnabled() {
		t.Fatal("admission must stay false — only admissionToggleKey may flip it")
	}
}

func TestHeadlessBarResumeStillWorksAlongsideAdmissionToggle(t *testing.T) {
	h, ctrl := newTestHeadlessBar()
	h.Update(store.Snapshot{
		HeadlessQueue: &store.HeadlessQueueState{Paused: true, ActiveCount: 0, ConcurrencyCap: 2},
	})

	h.HandleKey(tea.KeyPressMsg{Text: "r"})

	if h.lastAction != "resumed headless dispatch" {
		t.Fatalf("lastAction after r-press while paused = %q, want %q", h.lastAction, "resumed headless dispatch")
	}
	if ctrl.AdmissionEnabled() {
		t.Fatal("resume ('r') must never toggle admission")
	}
}
