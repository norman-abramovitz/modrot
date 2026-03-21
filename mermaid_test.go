package main

import (
	"strings"
	"testing"
	"time"
)

func TestMermaidSafeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/foo/bar", "github_com_foo_bar"},
		{"github.com/foo/bar-baz", "github_com_foo_bar_baz"},
		{"github.com/foo/bar@v1.0.0", "github_com_foo_bar_at_v1_0_0"},
		{"mymodule", "mymodule"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mermaidSafeID(tt.input)
			if got != tt.want {
				t.Errorf("mermaidSafeID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMermaidLabel(t *testing.T) {
	if got := mermaidLabel("github.com/foo/bar", "v1.0.0"); got != "github.com/foo/bar@v1.0.0" {
		t.Errorf("got %q", got)
	}
	if got := mermaidLabel("github.com/foo/bar", ""); got != "github.com/foo/bar" {
		t.Errorf("got %q", got)
	}
}

func TestPrintMermaid_BasicTree(t *testing.T) {
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
		PrintMermaid(cfg, results, graph, allModules)
	})

	if !strings.Contains(output, "graph TD") {
		t.Error("should start with graph TD")
	}
	if !strings.Contains(output, "mymodule") {
		t.Error("should contain root module")
	}
	if !strings.Contains(output, ":::archived") {
		t.Error("should have archived class")
	}
	if !strings.Contains(output, "classDef archived") {
		t.Error("should have classDef for archived")
	}
	if !strings.Contains(output, "-->") {
		t.Error("should have edges")
	}
}

func TestPrintMermaid_DirectArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b", Direct: true},
	}

	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {},
	}

	output := captureStdout(t, func() {
		PrintMermaid(cfg, results, graph, allModules)
	})

	if !strings.Contains(output, ":::archived") {
		t.Error("direct archived dep should have archived class")
	}
}

func TestPrintMermaid_NoArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Owner: "a", Repo: "b"},
			IsArchived: false,
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Owner: "a", Repo: "b", Direct: true},
	}

	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {},
	}

	output := captureStdout(t, func() {
		PrintMermaid(cfg, results, graph, allModules)
	})

	if !strings.Contains(output, "graph TD") {
		t.Error("should still output graph TD")
	}
	if !strings.Contains(output, "No archived dependencies") {
		t.Error("should show no archived message")
	}
}

func TestPrintMermaid_DeprecatedClass(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y", Deprecated: "Use something else"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b", Direct: true},
		{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y", Direct: false, Deprecated: "Use something else"},
	}

	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {"github.com/x/y@v0.1.0"},
	}

	output := captureStdout(t, func() {
		PrintMermaid(cfg, results, graph, allModules)
	})

	if !strings.Contains(output, ":::deprecated") {
		t.Error("deprecated module should have deprecated class")
	}
	if !strings.Contains(output, "classDef deprecated") {
		t.Error("should have classDef for deprecated")
	}
}
