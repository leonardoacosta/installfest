// See dispatch.go for the validators under test here — validateDispatchTarget
// (bead/proposal id-space) and validateTmuxPaneID (tmux's own "%N" pane-ID
// id-space). tasks.md [4.1] names validateDispatchTarget's accept/reject
// cases explicitly; validateTmuxPaneID is covered alongside it since it is
// the sibling boundary validator declared in the same file for the same
// dispatch-boundary reason (see dispatch.go's idShapeRe doc comment).
package dispatch

import "testing"

func TestValidateDispatchTargetAccepts(t *testing.T) {
	for _, id := range []string{
		"if-p1ru",
		"ABC123",
		"a",
		"wavetui-dispatch",
		"if_dtfn",
	} {
		if err := validateDispatchTarget(id); err != nil {
			t.Errorf("validateDispatchTarget(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidateDispatchTargetRejects(t *testing.T) {
	for _, id := range []string{
		"",
		"has space",
		"if-p1ru; rm -rf /",
		"$(whoami)",
		"foo/bar",
		"foo\nbar",
		"%12",       // pane-ID shape, deliberately a different id-space — see idShapeRe doc comment
		"if-p1ru.9", // dotted child-bead shape (rules/TOOLING.md's fleet-wide BEADS_ID_RE allows ".") — idShapeRe here is deliberately narrower, no "." in its charclass
	} {
		if err := validateDispatchTarget(id); err == nil {
			t.Errorf("validateDispatchTarget(%q) = nil, want an error", id)
		}
	}
}

func TestValidateTmuxPaneIDAccepts(t *testing.T) {
	for _, id := range []string{"%0", "%12", "%999999"} {
		if err := validateTmuxPaneID(id); err != nil {
			t.Errorf("validateTmuxPaneID(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidateTmuxPaneIDRejects(t *testing.T) {
	for _, id := range []string{
		"",
		"12",        // missing leading %
		"%",         // no digits
		"%12a",      // trailing non-digit
		"% 12",      // embedded space
		"if-p1ru",   // bead-id shape, wrong id-space
		"%12;touch", // shell-metacharacter injection attempt
	} {
		if err := validateTmuxPaneID(id); err == nil {
			t.Errorf("validateTmuxPaneID(%q) = nil, want an error", id)
		}
	}
}
