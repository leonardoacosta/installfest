package blocker

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name       string
		line       string
		wantOK     bool
		wantType   string
		wantReason string
		wantRef    string
	}{
		{
			name:       "full grammar with ref",
			line:       "blocked: dependency - waiting on foo (see if-1234)",
			wantOK:     true,
			wantType:   "dependency",
			wantReason: "waiting on foo",
			wantRef:    "if-1234",
		},
		{
			name:       "no ref suffix",
			line:       "blocked: external - vendor outage",
			wantOK:     true,
			wantType:   "external",
			wantReason: "vendor outage",
			wantRef:    "",
		},
		{
			name:       "case-insensitive prefix, type/reason case-preserved",
			line:       "BLOCKED: Review - Ping Bob directly",
			wantOK:     true,
			wantType:   "Review",
			wantReason: "Ping Bob directly",
			wantRef:    "",
		},
		{
			name:       "mixed-case prefix",
			line:       "Blocked: decision - pick an approach",
			wantOK:     true,
			wantType:   "decision",
			wantReason: "pick an approach",
			wantRef:    "",
		},
		{
			name:       "leading list bullet stripped",
			line:       "- blocked: decision - pick approach (see docs/design.md)",
			wantOK:     true,
			wantType:   "decision",
			wantReason: "pick approach",
			wantRef:    "docs/design.md",
		},
		{
			name:       "leading asterisk bullet stripped",
			line:       "* blocked: dependency - needs if-9999 (see if-9999)",
			wantOK:     true,
			wantType:   "dependency",
			wantReason: "needs if-9999",
			wantRef:    "if-9999",
		},
		{
			name:       "unknown type still accepted (forward-compat)",
			line:       "blocked: whatever - a future type nobody wrote yet",
			wantOK:     true,
			wantType:   "whatever",
			wantReason: "a future type nobody wrote yet",
			wantRef:    "",
		},
		{
			name:       "leading/trailing whitespace tolerated",
			line:       "   blocked: review - final check   ",
			wantOK:     true,
			wantType:   "review",
			wantReason: "final check",
			wantRef:    "",
		},
		{
			name:   "missing colon degrades silently",
			line:   "blocked dependency - waiting on foo",
			wantOK: false,
		},
		{
			name:   "missing dash separator degrades silently",
			line:   "blocked: external vendor outage",
			wantOK: false,
		},
		{
			name:   "empty line degrades silently",
			line:   "",
			wantOK: false,
		},
		{
			name:   "unrelated text degrades silently",
			line:   "This item is waiting on a review, no grammar match here.",
			wantOK: false,
		},
		{
			name:   "blocked prefix with no content at all",
			line:   "blocked:",
			wantOK: false,
		},
		{
			name:   "whitespace-only line degrades silently",
			line:   "   \t  ",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note, ok := Parse(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v (note=%+v)", tc.line, ok, tc.wantOK, note)
			}
			if !tc.wantOK {
				// Malformed/missing lines must degrade silently: zero
				// value, no error, no panic (already implied by reaching
				// here without a panic).
				return
			}
			if note.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", note.Type, tc.wantType)
			}
			if note.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", note.Reason, tc.wantReason)
			}
			if note.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", note.Ref, tc.wantRef)
			}
		})
	}
}

func TestParseNeverPanics(t *testing.T) {
	inputs := []string{
		"", " ", "blocked:", "blocked:-", "blocked: - (see )", "(see foo)",
		"blocked: a - b (see", "blocked: a - b (see c", strings.Repeat("x", 5000),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q) panicked: %v", in, r)
				}
			}()
			Parse(in)
		}()
	}
}
