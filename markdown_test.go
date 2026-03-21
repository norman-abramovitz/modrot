package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrintMarkdownTable(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"Module", "Version", "Direct"}
	rows := [][]string{
		{"github.com/foo/bar", "v1.0.0", "direct"},
		{"github.com/baz/qux", "v2.0.0", "indirect"},
	}

	printMarkdownTable(&buf, headers, rows)
	output := buf.String()

	if !strings.Contains(output, "| Module | Version | Direct |") {
		t.Errorf("missing header row, got:\n%s", output)
	}
	if !strings.Contains(output, "| --- | --- | --- |") {
		t.Errorf("missing separator row, got:\n%s", output)
	}
	if !strings.Contains(output, "| github.com/foo/bar | v1.0.0 | direct |") {
		t.Errorf("missing data row, got:\n%s", output)
	}
}

func TestPrintMarkdown_ArchivedOnly(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false, Owner: "baz", Repo: "qux"},
			IsArchived: false,
		},
	}

	output := captureStdout(t, func() {
		PrintMarkdown(cfg, results, nil)
	})

	if !strings.Contains(output, "## ARCHIVED DEPENDENCIES") {
		t.Error("should have ARCHIVED DEPENDENCIES header")
	}
	if !strings.Contains(output, "github.com/foo/bar") {
		t.Error("should contain archived module")
	}
	if strings.Contains(output, "github.com/baz/qux") {
		t.Error("should not contain active module when showAll=false")
	}
}

func TestPrintMarkdown_WithAll(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ShowAll = true
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
		{
			Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false, Owner: "baz", Repo: "qux"},
			IsArchived: false,
			PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintMarkdown(cfg, results, nil)
	})

	if !strings.Contains(output, "## ACTIVE DEPENDENCIES") {
		t.Error("should have ACTIVE DEPENDENCIES section when showAll=true")
	}
	if !strings.Contains(output, "github.com/baz/qux") {
		t.Error("should contain active module when showAll=true")
	}
}

func TestPrintMarkdown_NoArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0", Direct: false, Owner: "baz", Repo: "qux"},
			IsArchived: false,
		},
	}

	output := captureStdout(t, func() {
		PrintMarkdown(cfg, results, nil)
	})

	if !strings.Contains(output, "No archived dependencies found") {
		t.Error("should say no archived found")
	}
}

func TestPrintMarkdownFiles(t *testing.T) {
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"},
			IsArchived: true,
		},
	}

	fileMatches := map[string][]FileMatch{
		"github.com/foo/bar": {
			{File: "audit/hash.go", Line: 14, ImportPath: "github.com/foo/bar"},
		},
	}

	output := captureStdout(t, func() {
		PrintMarkdownFiles(results, fileMatches)
	})

	if !strings.Contains(output, "## SOURCE FILES") {
		t.Error("should have SOURCE FILES header")
	}
	if !strings.Contains(output, "`audit/hash.go:14`") {
		t.Errorf("should contain file:line in backticks, got:\n%s", output)
	}
}

func TestPrintMarkdownStale(t *testing.T) {
	cfg := &Config{Stale: StaleConfig{Enabled: true, Years: 2}, DateFmt: "2006-01-02"}

	stale := []RepoStatus{
		{
			Module:   Module{Path: "github.com/old/repo", Version: "v1.0.0", Direct: true},
			PushedAt: time.Date(2022, 1, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintMarkdownStale(cfg, stale)
	})

	if !strings.Contains(output, "## STALE DEPENDENCIES") {
		t.Error("should have STALE DEPENDENCIES header")
	}
	if !strings.Contains(output, "github.com/old/repo") {
		t.Error("should contain stale module")
	}
}

func TestPrintMarkdownTree(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b", Direct: true},
		{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y", Direct: false},
	}

	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {"github.com/x/y@v0.1.0"},
		"github.com/x/y@v0.1.0": {},
	}

	output := captureStdout(t, func() {
		PrintMarkdownTree(cfg, results, graph, allModules, nil)
	})

	if !strings.Contains(output, "## DEPENDENCY TREE") {
		t.Error("should have DEPENDENCY TREE header")
	}
	if !strings.Contains(output, "github.com/a/b@v1.0.0") {
		t.Error("should contain direct dep")
	}
	if !strings.Contains(output, "[ARCHIVED") {
		t.Error("should mark archived deps")
	}
}
