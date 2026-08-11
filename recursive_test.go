package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyStatus(t *testing.T) {
	statusMap := map[string]RepoStatus{
		"foo/bar": {
			IsArchived: true,
			ArchivedAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		"baz/qux": {
			IsArchived: false,
			PushedAt:   time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
		{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false, Owner: "baz", Repo: "qux"},
		{Path: "github.com/unknown/repo", Version: "v0.1.0", Direct: false, Owner: "unknown", Repo: "repo"},
	}

	results := applyStatus(modules, statusMap)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First module: archived
	if !results[0].IsArchived {
		t.Error("expected foo/bar to be archived")
	}
	if results[0].Module.Path != "github.com/foo/bar" {
		t.Errorf("expected module path github.com/foo/bar, got %s", results[0].Module.Path)
	}
	if results[0].ArchivedAt.IsZero() {
		t.Error("expected non-zero ArchivedAt for foo/bar")
	}

	// Second module: active
	if results[1].IsArchived {
		t.Error("expected baz/qux to be active")
	}
	if results[1].PushedAt.IsZero() {
		t.Error("expected non-zero PushedAt for baz/qux")
	}

	// Third module: not in status map
	if results[2].IsArchived {
		t.Error("expected unknown/repo to not be archived")
	}
	if !results[2].PushedAt.IsZero() {
		t.Error("expected zero PushedAt for unknown/repo")
	}
}

func TestGetArchivedPaths(t *testing.T) {
	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar"}, IsArchived: true},
		{Module: Module{Path: "github.com/baz/qux"}, IsArchived: false},
		{Module: Module{Path: "github.com/old/lib"}, IsArchived: true},
	}

	paths := getArchivedPaths(results)
	if len(paths) != 2 {
		t.Fatalf("expected 2 archived paths, got %d", len(paths))
	}
	if paths[0] != "github.com/foo/bar" {
		t.Errorf("expected github.com/foo/bar, got %s", paths[0])
	}
	if paths[1] != "github.com/old/lib" {
		t.Errorf("expected github.com/old/lib, got %s", paths[1])
	}
}

func TestGetArchivedPaths_None(t *testing.T) {
	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar"}, IsArchived: false},
	}
	paths := getArchivedPaths(results)
	if len(paths) != 0 {
		t.Fatalf("expected 0 archived paths, got %d", len(paths))
	}
}

func TestGetDeprecatedModules_Disabled(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Deprecated: "Use something else."},
	}
	result := getDeprecatedModules(modules, false, false)
	if result != nil {
		t.Errorf("expected nil when deprecatedMode=false, got %v", result)
	}
}

func TestGetDeprecatedModules_FilterDeprecated(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Deprecated: "Use something else."},
		{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: true},
		{Path: "github.com/old/lib", Version: "v0.5.0", Direct: false, Deprecated: "Moved to github.com/new/lib."},
	}

	result := getDeprecatedModules(modules, false, true)
	if len(result) != 2 {
		t.Fatalf("expected 2 deprecated modules, got %d", len(result))
	}
	if result[0].Path != "github.com/foo/bar" {
		t.Errorf("result[0].Path = %q, want github.com/foo/bar", result[0].Path)
	}
	if result[1].Path != "github.com/old/lib" {
		t.Errorf("result[1].Path = %q, want github.com/old/lib", result[1].Path)
	}
}

func TestGetDeprecatedModules_DirectOnly(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Deprecated: "Use something else."},
		{Path: "github.com/old/lib", Version: "v0.5.0", Direct: false, Deprecated: "Moved to github.com/new/lib."},
	}

	result := getDeprecatedModules(modules, true, true)
	if len(result) != 1 {
		t.Fatalf("expected 1 deprecated direct module, got %d", len(result))
	}
	if result[0].Path != "github.com/foo/bar" {
		t.Errorf("result[0].Path = %q, want github.com/foo/bar", result[0].Path)
	}
}

func TestGetDeprecatedModules_NoneDeprecated(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true},
		{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false},
	}

	result := getDeprecatedModules(modules, false, true)
	if len(result) != 0 {
		t.Errorf("expected 0 deprecated modules, got %d", len(result))
	}
}

// --no-ignore was honoured only on the single-unit path, so every multi-unit
// scan silently kept applying .modrotignore — dropping archived rows and
// turning exit 1 into exit 0. reportUnits routes any directory with two or
// more units here, so this hit plain non-recursive scans too.
func TestUnitIgnoreListHonoursNoIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".modrotignore"),
		[]byte("github.com/foo/bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mi := manifestInfo{manifestPath: filepath.Join(dir, "go.mod")}

	cfg := NewDefaultConfig()
	if got := unitIgnoreList(mi, cfg).Len(); got != 1 {
		t.Fatalf("default: ignore list Len = %d, want 1", got)
	}

	cfg.NoIgnore = true
	if got := unitIgnoreList(mi, cfg).Len(); got != 0 {
		t.Errorf("--no-ignore: ignore list Len = %d, want 0", got)
	}

	// An inline --ignore must be dropped by --no-ignore too.
	cfg.IgnoreInline = "github.com/baz/qux"
	if got := unitIgnoreList(mi, cfg).Len(); got != 0 {
		t.Errorf("--no-ignore with --ignore: Len = %d, want 0", got)
	}
}

// A multi-unit scan must survive --no-ignore end to end: the archived row is
// kept and the run still reports a finding.
func TestApplyUnitIgnoresHonoursNoIgnore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".modrotignore"),
		[]byte("github.com/foo/bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mi := manifestInfo{manifestPath: filepath.Join(dir, "go.mod")}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"}, IsArchived: true},
	}

	cfg := NewDefaultConfig()
	kept, ignored, _ := applyUnitIgnores(mi, results, cfg)
	if len(kept) != 0 || len(ignored) != 1 {
		t.Fatalf("default: kept %d, ignored %d; want 0, 1", len(kept), len(ignored))
	}

	cfg.NoIgnore = true
	kept, ignored, _ = applyUnitIgnores(mi, results, cfg)
	if len(kept) != 1 || len(ignored) != 0 {
		t.Errorf("--no-ignore: kept %d, ignored %d; want 1, 0", len(kept), len(ignored))
	}
	if len(getArchivedPaths(kept)) != 1 {
		t.Error("--no-ignore: archived row lost, exit code would drop to 0")
	}
}
