// Package blocker parses the wavetui blocker-note grammar, formalized in
// openspec/changes/wavetui-core/design.md § Blocker-note grammar:
//
//	blocked: <type> - <reason> (see <ref>)
//
// The convention lives in whichever text field the source already has
// (bead notes, or a proposal.md "## Context" bullet) — there is no new
// file or frontmatter field. Parse NEVER errors or panics on a
// malformed/missing line: absence of a match is the expected, silent
// non-error case (no badge is rendered), consistent with "tolerant
// decoding everywhere" elsewhere in wavetui.
package blocker

import (
	"regexp"
	"strings"
)

// noteRe is the grammar regex from design.md § Blocker-note grammar,
// verbatim, with the (?i) flag added for the "case-insensitive on the
// blocked: prefix only" rule — Go's regexp captures the original
// substring regardless of the (?i) match mode, so <type>/<reason> stay
// case-preserved in the returned Note exactly as design.md specifies.
var noteRe = regexp.MustCompile(`(?i)^blocked:\s*([\w-]+)\s*-\s*(.+?)(?:\s*\(see\s+([^)]+)\))?$`)

// Note is the parsed form of a blocker-note line.
type Note struct {
	// Type is one of decision, dependency, external, review — or any other
	// token; unknown types are accepted and render with a generic badge
	// (forward-compat: a future type needs no parser change).
	Type string
	// Reason is required free text.
	Reason string
	// Ref is the optional "(see <ref>)" suffix content, or "" if absent.
	Ref string
}

// Parse attempts to parse line as a blocker note. ok is false whenever line
// does not match the grammar — including an empty line, a line with no
// "blocked:" prefix, or a line missing the required " - " reason
// separator. Parse never returns an error and never panics; a non-matching
// line is simply not a blocker note.
//
// A single leading list-bullet marker ("- ", "* ", or "+ ") is stripped
// before matching, since design.md's proposal.md placement is "a plain
// bullet" under the "## Context" section.
func Parse(line string) (Note, bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = stripBulletPrefix(trimmed)

	m := noteRe.FindStringSubmatch(trimmed)
	if m == nil {
		return Note{}, false
	}

	return Note{
		Type:   m[1],
		Reason: strings.TrimSpace(m[2]),
		Ref:    m[3],
	}, true
}

// stripBulletPrefix removes exactly one leading "- ", "* ", or "+ " marker,
// if present. It intentionally does not strip an unmarked line, and does
// not touch anything after the first non-marker character, so it can never
// eat into a genuine "blocked: ..." line that happens to start with one of
// these characters without being a list item.
func stripBulletPrefix(s string) string {
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(s, marker) {
			return strings.TrimSpace(s[len(marker):])
		}
	}
	return s
}
