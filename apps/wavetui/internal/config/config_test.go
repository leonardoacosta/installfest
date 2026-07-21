package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on missing file returned error: %v", err)
	}
	if cfg.ShowPlans || cfg.ShowAdvisorPlans {
		t.Fatalf("want all-defaults-off Config on missing file, got %+v", cfg)
	}
	if cfg.Flair.Enabled || cfg.Flair.CalmMode {
		t.Fatalf("want all-defaults-off Flair on missing file, got %+v", cfg.Flair)
	}
}

func TestLoadParsesFlairBooleans(t *testing.T) {
	dir := t.TempDir()
	content := "flair_enabled = true\nflair_calm_mode = true\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.Flair.Enabled {
		t.Error("want Flair.Enabled=true")
	}
	if !cfg.Flair.CalmMode {
		t.Error("want Flair.CalmMode=true")
	}
	// Additive: existing fields must be unaffected by the new keys.
	if cfg.ShowPlans || cfg.ShowAdvisorPlans {
		t.Fatalf("want ShowPlans/ShowAdvisorPlans still default-off, got %+v", cfg)
	}
}

func TestLoadParsesForceOSC52(t *testing.T) {
	dir := t.TempDir()
	content := "force_osc52 = true\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.ForceOSC52 {
		t.Error("want ForceOSC52=true")
	}
	// Additive: existing fields must be unaffected by the new key.
	if cfg.ShowPlans || cfg.ShowAdvisorPlans || cfg.Flair.Enabled {
		t.Fatalf("want other fields still default-off, got %+v", cfg)
	}
}

func TestLoadParsesBooleans(t *testing.T) {
	dir := t.TempDir()
	content := "# a comment\n\nshow_plans = true\nshow_advisor_plans = false\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.ShowPlans {
		t.Error("want ShowPlans=true")
	}
	if cfg.ShowAdvisorPlans {
		t.Error("want ShowAdvisorPlans=false")
	}
}

func TestLoadToleratesMalformedLines(t *testing.T) {
	dir := t.TempDir()
	content := "this is not a valid line\nshow_plans = true\n===\nshow_advisor_plans = maybe\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("setup write failed: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error on malformed lines, want tolerant no-op skip: %v", err)
	}
	if !cfg.ShowPlans {
		t.Error("want ShowPlans=true (valid line among malformed ones still parses)")
	}
	if cfg.ShowAdvisorPlans {
		t.Error("want ShowAdvisorPlans=false (non-boolean value 'maybe' ignored, defaults to false)")
	}
}

func TestAtomicWriteFileWritesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.txt")

	if err := AtomicWriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "v1" {
		t.Fatalf("want %q, got %q (err=%v)", "v1", got, err)
	}

	if err := AtomicWriteFile(path, []byte("v2"), 0o644); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "v2" {
		t.Fatalf("want %q, got %q (err=%v)", "v2", got, err)
	}

	// No leftover temp files should remain in dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 entry (state.txt) in dir, got %d: %v", len(entries), entries)
	}
}

func TestAtomicWriteFileNotCoupledToConfig(t *testing.T) {
	// AtomicWriteFile must work for arbitrary content/paths unrelated to
	// config loading, proving it is a general-purpose helper, not
	// config-specific glue.
	dir := t.TempDir()
	path := filepath.Join(dir, "arbitrary.json")
	if err := AtomicWriteFile(path, []byte(`{"k":"v"}`), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile failed for non-config content: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != `{"k":"v"}` {
		t.Fatalf("want written JSON content back, got %q (err=%v)", got, err)
	}
}
