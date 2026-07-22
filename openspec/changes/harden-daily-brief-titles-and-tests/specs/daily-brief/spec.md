# daily-brief Specification

## ADDED Requirements

### Requirement: External titles are stripped of control characters before rendering
Every title or location string that originates from an external, untrusted source (mx-gateway
triage items, calendar events — `src/sources/mx.ts`'s `TriageCore.title`/`CalendarEvent.title`
and `location`) SHALL have its C0 (`\x00-\x1F`) and C1 (`\x7F-\x9F`) control characters stripped
before it reaches either render path (`view --plain`'s `plainRender.ts` or the ink render). The
strip MUST happen at a single shared choke point — not duplicated per render site — so a new
render surface consuming the same shaped data cannot forget to sanitize.

#### Scenario: a title containing an ANSI escape sequence renders clean in --plain mode
- **WHEN** a radar item, meeting, or open-item title contains an ANSI escape sequence (e.g.
  `"x\x1b[2Jy"`, a terminal clear-screen sequence)
- **THEN** `daily-brief view --plain`'s rendered output contains the title's visible characters
  with the escape sequence removed, and the terminal is not manipulated (no cursor move, no
  screen clear) by the rendered output

#### Scenario: stripControlChars unit cases
- **WHEN** `stripControlChars` is called with an empty string, a plain ASCII string, a string
  containing an ANSI escape (`\x1b[31m`), a string containing a C1 control character
  (`\x80`-`\x9F` range), and a string containing a tab (`\t`)
- **THEN** the empty and plain strings are returned unchanged, and the ANSI/C1/tab cases each
  return the string with every C0/C1 control character removed and all other characters
  preserved
