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
	"strings"
)

// FileName is the config file wavetui looks for in a project's root,
// resolved relative to the caller-supplied directory (see Load).
const FileName = ".wavetui.toml"

// Config is wavetui's per-project settings. The zero value is the default:
// both visibility flags off.
type Config struct {
	// ShowPlans surfaces items from a project's plans/ directory in the
	// queue. Default off.
	ShowPlans bool
	// ShowAdvisorPlans surfaces items from advisor-plans/. Default off.
	ShowAdvisorPlans bool
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
		ShowPlans:        values["show_plans"],
		ShowAdvisorPlans: values["show_advisor_plans"],
	}, nil
}

// parseTOMLSubset reads `key = true|false` lines, skipping blank lines and
// `#`-prefixed comments. Any line that does not match that shape is
// silently ignored (tolerant — an unrecognized or hand-edited-wrong line
// never breaks Load for the whole file).
func parseTOMLSubset(r *os.File) (map[string]bool, error) {
	values := make(map[string]bool)
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
		val = strings.ToLower(strings.TrimSpace(val))
		switch val {
		case "true":
			values[key] = true
		case "false":
			values[key] = false
		default:
			// Not a boolean literal this subset understands — ignore.
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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
