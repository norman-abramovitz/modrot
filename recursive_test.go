package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindGoModFiles(t *testing.T) {
	// Create a temp directory tree with go.mod files
	root := t.TempDir()

	// helper to create directories and files
	writeFile := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Root go.mod
	writeFile(filepath.Join(root, "go.mod"), []byte("module example.com/root\n"))

	// Subdirectory with go.mod
	writeFile(filepath.Join(root, "api", "go.mod"), []byte("module example.com/root/api\n"))

	// Nested subdirectory with go.mod (extra nested dir for structure)
	if err := os.MkdirAll(filepath.Join(root, "sdk", "v2"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(filepath.Join(root, "sdk", "go.mod"), []byte("module example.com/root/sdk\n"))

	// vendor/ should be skipped
	writeFile(filepath.Join(root, "vendor", "lib", "go.mod"), []byte("module vendor/lib\n"))

	// testdata/ should be skipped
	writeFile(filepath.Join(root, "testdata", "go.mod"), []byte("module testdata/mod\n"))

	// Hidden directory should be skipped
	writeFile(filepath.Join(root, ".hidden", "go.mod"), []byte("module hidden/mod\n"))

	paths, err := findGoModFiles(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find exactly 3: root, api, sdk
	if len(paths) != 3 {
		t.Fatalf("expected 3 go.mod files, got %d: %v", len(paths), paths)
	}

	// Verify the found paths are the expected ones
	expected := map[string]bool{
		filepath.Join(root, "go.mod"):        true,
		filepath.Join(root, "api", "go.mod"): true,
		filepath.Join(root, "sdk", "go.mod"): true,
	}
	for _, p := range paths {
		if !expected[p] {
			t.Errorf("unexpected go.mod found: %s", p)
		}
	}
}

func TestFindGoModFiles_NoGoMod(t *testing.T) {
	root := t.TempDir()
	paths, err := findGoModFiles(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected 0 go.mod files, got %d", len(paths))
	}
}

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
