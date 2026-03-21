package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewIgnoreList(t *testing.T) {
	il := NewIgnoreList()
	if il.Len() != 0 {
		t.Errorf("new ignore list should be empty, got %d", il.Len())
	}
}

func TestIgnoreList_Add(t *testing.T) {
	il := NewIgnoreList()
	il.Add("github.com/foo/bar", "github.com/baz/qux")
	if il.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", il.Len())
	}
	if !il.IsIgnored("github.com/foo/bar") {
		t.Error("expected foo/bar to be ignored")
	}
	if !il.IsIgnored("github.com/baz/qux") {
		t.Error("expected baz/qux to be ignored")
	}
	if il.IsIgnored("github.com/other/repo") {
		t.Error("expected other/repo to NOT be ignored")
	}
}

func TestIgnoreList_Add_SkipsEmpty(t *testing.T) {
	il := NewIgnoreList()
	il.Add("", "  ", "github.com/foo/bar")
	if il.Len() != 1 {
		t.Errorf("expected 1 entry (empty/whitespace skipped), got %d", il.Len())
	}
}

func TestIgnoreList_FilterResults(t *testing.T) {
	il := NewIgnoreList()
	il.Add("github.com/foo/bar")

	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0"},
			IsArchived: true,
		},
		{
			Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0"},
			IsArchived: true,
		},
		{
			Module:     Module{Path: "github.com/other/repo", Version: "v3.0.0"},
			IsArchived: false,
		},
	}

	filtered, ignored := il.FilterResults(results)
	if len(filtered) != 2 {
		t.Errorf("expected 2 filtered, got %d", len(filtered))
	}
	if len(ignored) != 1 {
		t.Errorf("expected 1 ignored, got %d", len(ignored))
	}
	if ignored[0].Module.Path != "github.com/foo/bar" {
		t.Errorf("expected foo/bar to be ignored, got %s", ignored[0].Module.Path)
	}
}

func TestLoadIgnoreFile_MissingFile(t *testing.T) {
	il, err := LoadIgnoreFile("/nonexistent/.modrotignore")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if il.Len() != 0 {
		t.Errorf("expected empty list for missing file, got %d", il.Len())
	}
}

func TestLoadIgnoreFile_ValidFile(t *testing.T) {
	dir := t.TempDir()
	ignoreFile := filepath.Join(dir, ".modrotignore")
	content := `# This is a comment
github.com/foo/bar

github.com/baz/qux
# Another comment
github.com/old/thing
`
	if err := os.WriteFile(ignoreFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	il, err := LoadIgnoreFile(ignoreFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if il.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", il.Len())
	}
	if !il.IsIgnored("github.com/foo/bar") {
		t.Error("expected foo/bar to be ignored")
	}
	if !il.IsIgnored("github.com/baz/qux") {
		t.Error("expected baz/qux to be ignored")
	}
	if !il.IsIgnored("github.com/old/thing") {
		t.Error("expected old/thing to be ignored")
	}
}

func TestParseIgnoreList(t *testing.T) {
	il := ParseIgnoreList("github.com/foo/bar,github.com/baz/qux")
	if il.Len() != 2 {
		t.Errorf("expected 2, got %d", il.Len())
	}
	if !il.IsIgnored("github.com/foo/bar") {
		t.Error("expected foo/bar")
	}
	if !il.IsIgnored("github.com/baz/qux") {
		t.Error("expected baz/qux")
	}
}

func TestParseIgnoreList_Empty(t *testing.T) {
	il := ParseIgnoreList("")
	if il.Len() != 0 {
		t.Errorf("expected 0, got %d", il.Len())
	}
}

func TestBuildIgnoreList(t *testing.T) {
	dir := t.TempDir()
	ignoreFile := filepath.Join(dir, ".modrotignore")
	if err := os.WriteFile(ignoreFile, []byte("github.com/from/file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	il := BuildIgnoreList(dir, "", "github.com/from/inline")
	if il.Len() != 2 {
		t.Errorf("expected 2 (file + inline), got %d", il.Len())
	}
	if !il.IsIgnored("github.com/from/file") {
		t.Error("expected from/file")
	}
	if !il.IsIgnored("github.com/from/inline") {
		t.Error("expected from/inline")
	}
}

func TestIgnoreList_IntegrationWithStaleAndSort(t *testing.T) {
	// Verify that ignored modules don't appear in stale results either
	cfg := &Config{Stale: StaleConfig{Enabled: true, Years: 1}, DateFmt: "2006-01-02"}

	results := []RepoStatus{
		{
			Module:   Module{Path: "github.com/ignored/repo", Version: "v1.0.0"},
			PushedAt: time.Now().AddDate(-3, 0, 0),
		},
		{
			Module:   Module{Path: "github.com/kept/repo", Version: "v1.0.0"},
			PushedAt: time.Now().AddDate(-3, 0, 0),
		},
	}

	il := NewIgnoreList()
	il.Add("github.com/ignored/repo")
	filtered, _ := il.FilterResults(results)

	stale := filterStale(cfg, filtered)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale after filtering, got %d", len(stale))
	}
	if stale[0].Module.Path != "github.com/kept/repo" {
		t.Errorf("expected kept/repo, got %s", stale[0].Module.Path)
	}
}
