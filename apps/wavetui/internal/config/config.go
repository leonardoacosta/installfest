// Package config loads wavetui's per-project configuration and provides a
// generic atomic-write helper any wavetui package may reuse.
//
// The config file format is a minimal TOML subset (comments, blank lines,
// and flat `key = value` boolean assignments) hand-rolled against stdlib
// only, per this batch's "no third-party dependencies yet" constraint — see
// openspec/changes/wavetui-core/tasks.md [1.4]. It is a deliberate subset,
// not a full TOML parser: the config surface today is two booleans, and a
// real TOML library (e.g. BurntSushi/toml) can replace parseTOMLSubset
// later without changing Load's public signature or the Config shape.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the config file wavetui looks for in a project's root,
// resolved relative to the caller-supplied directory (see Load).
const FileName = ".wavetui.toml"

// Config is wavetui's per-project settings. The zero value is the default:
// both visibility flags off, and Flair all-off (see FlairConfig).
type Config struct {
	// ShowPlans surfaces items from a project's plans/ directory in the
	// queue. Default off.
	ShowPlans bool
	// ShowAdvisorPlans surfaces items from advisor-plans/. Default off.
	ShowAdvisorPlans bool
	// Flair holds wavetui-flair's opt-in animation settings. Zero value
	// (Enabled=false) means flair code never runs at all — see
	// openspec/changes/wavetui-flair/design.md § Config + calm-mode +
	// truecolor gating.
	Flair FlairConfig
	// ForceOSC52 overrides ClipboardDispatcher's own OSC52-capability
	// detection for a terminal that supports the escape sequence but does
	// not advertise it via $TERM_PROGRAM/terminfo. See
	// internal/dispatch/clipboard.go's ForceOSC52 field, whose doc comment
	// already named this config file as its intended source before this
	// field existed. Default false (trust detection).
	ForceOSC52 bool
	// HeadlessConcurrencyCap bounds how many headless `claude -p` child
	// processes wavetui-daemon's HeadlessDispatcher may run at once. See
	// openspec/changes/wavetui-daemon/design.md § Concurrency cap default.
	// This raw field round-trips whatever the config file literally sets
	// (including the zero value when unset or a hand-edited non-positive
	// number) — EffectiveHeadlessConcurrencyCap below is how a caller
	// resolves the value actually in effect (default 2 when this field is
	// unset or <= 0).
	HeadlessConcurrencyCap int
}

// EffectiveHeadlessConcurrencyCap returns the concurrency cap
// HeadlessDispatcher should actually use: HeadlessConcurrencyCap when
// positive, or the default of 2 when unset (zero) or set to a non-positive
// value. See openspec/changes/wavetui-daemon/design.md § Concurrency cap
// default for why 2 (not unbounded, not 1) is the shipped default.
func (c Config) EffectiveHeadlessConcurrencyCap() int {
	if c.HeadlessConcurrencyCap <= 0 {
		return 2
	}
	return c.HeadlessConcurrencyCap
}

// FlairConfig is wavetui-flair's additive settings block — see
// openspec/changes/wavetui-flair/design.md § Config + calm-mode + truecolor
// gating. Its shape is taken verbatim from that section: do not add fields
// here without a corresponding design.md update.
type FlairConfig struct {
	// Enabled gates whether wavetui-flair's Diff/animation machinery runs at
	// all. False (default) is the literal disabled-equals-identical path —
	// FlairManager.Diff is never called and the overlay compositor is never
	// invoked, not merely suppressed.
	Enabled bool
	// CalmMode (only meaningful when Enabled is true) routes every effect to
	// its static-glyph fallback instead of an animated one.
	CalmMode bool
}

// Load reads FileName from dir (typically the caller's cwd, i.e. a
// project's root). A missing config file is not an error — it is the
// expected case for any project that has not opted into wavetui config,
// and Load returns the zero-value (all-defaults-off) Config. A present but
// malformed line is skipped rather than treated as a fatal parse error,
// consistent with the tolerant-decoding convention used elsewhere in
// wavetui (see internal/blocker).
func Load(dir string) (Config, error) {
	path := filepath.Join(dir, FileName)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	defer f.Close()

	values, err := parseTOMLSubset(f)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ShowPlans:        values.bools["show_plans"],
		ShowAdvisorPlans: values.bools["show_advisor_plans"],
		Flair: FlairConfig{
			Enabled:  values.bools["flair_enabled"],
			CalmMode: values.bools["flair_calm_mode"],
		},
		ForceOSC52:             values.bools["force_osc52"],
		HeadlessConcurrencyCap: values.ints["headless_concurrency_cap"],
	}, nil
}

// parsedValues holds the two literal kinds this subset understands —
// booleans (the original config surface) and non-negative integers, added
// for HeadlessConcurrencyCap (wavetui-daemon's design.md § Concurrency cap
// default), the first int-typed setting this file has ever needed to parse.
type parsedValues struct {
	bools map[string]bool
	ints  map[string]int
}

// parseTOMLSubset reads `key = true|false` and `key = <integer>` lines,
// skipping blank lines and `#`-prefixed comments. Any line that does not
// match one of those two shapes is silently ignored (tolerant — an
// unrecognized or hand-edited-wrong line never breaks Load for the whole
// file).
func parseTOMLSubset(r *os.File) (parsedValues, error) {
	values := parsedValues{bools: make(map[string]bool), ints: make(map[string]int)}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch strings.ToLower(val) {
		case "true":
			values.bools[key] = true
			continue
		case "false":
			values.bools[key] = false
			continue
		}
		if n, err := strconv.Atoi(val); err == nil {
			values.ints[key] = n
			continue
		}
		// Not a boolean or integer literal this subset understands — ignore.
	}
	if err := scanner.Err(); err != nil {
		return parsedValues{}, err
	}
	return values, nil
}

// AtomicWriteFile writes data to path by writing a temp file in the same
// directory and renaming it into place, so any concurrent reader either
// sees the old complete content or the new complete content — never a
// partial write. It is deliberately independent of config loading — any
// wavetui package that needs to persist state safely may call this
// directly.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup: if we return before a successful rename, remove
	// the temp file. Once Rename succeeds, tmpName no longer exists under
	// its temp name, so this Remove becomes a harmless no-op.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
