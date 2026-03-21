package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStripVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/foo/bar@v1.2.3", "github.com/foo/bar"},
		{"github.com/foo/bar/v2@v2.0.0", "github.com/foo/bar/v2"},
		{"github.com/foo/bar@v0.0.0-20210821155943-2d9075ca8770", "github.com/foo/bar"},
		{"github.com/foo/bar", "github.com/foo/bar"}, // no version
		{"cel.dev/expr@v0.25.1", "cel.dev/expr"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripVersion(tt.input)
			if got != tt.want {
				t.Errorf("stripVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAllSeen(t *testing.T) {
	seen := map[string]bool{"a": true, "b": true}

	if !allSeen([]string{"a", "b"}, seen) {
		t.Error("expected true when all items seen")
	}
	if !allSeen([]string{}, seen) {
		t.Error("expected true for empty slice")
	}
	if allSeen([]string{"a", "c"}, seen) {
		t.Error("expected false when 'c' not seen")
	}
}

func TestFindArchivedTransitive(t *testing.T) {
	graph := map[string][]string{
		"root":                  {"github.com/a/b@v1.0.0", "github.com/c/d@v1.0.0"},
		"github.com/a/b@v1.0.0": {"github.com/x/y@v1.0.0"},
		"github.com/c/d@v1.0.0": {"github.com/x/y@v1.0.0", "github.com/e/f@v1.0.0"},
		"github.com/x/y@v1.0.0": {},
		"github.com/e/f@v1.0.0": {},
	}

	archivedPaths := map[string]bool{
		"github.com/x/y": true,
	}

	result := findArchivedTransitive("github.com/a/b@v1.0.0", graph, archivedPaths, make(map[string]bool))
	if len(result) != 1 || result[0] != "github.com/x/y" {
		t.Errorf("expected [github.com/x/y], got %v", result)
	}
}

func TestFindArchivedTransitive_Cycle(t *testing.T) {
	// Ensure cycles don't cause infinite loops
	graph := map[string][]string{
		"a@v1": {"b@v1"},
		"b@v1": {"a@v1"},
	}

	archivedPaths := map[string]bool{"b": true}

	result := findArchivedTransitive("a@v1", graph, archivedPaths, make(map[string]bool))
	if len(result) != 1 || result[0] != "b" {
		t.Errorf("expected [b], got %v", result)
	}
}

func TestFindArchivedTransitive_Deep(t *testing.T) {
	graph := map[string][]string{
		"a@v1": {"b@v1"},
		"b@v1": {"c@v1"},
		"c@v1": {"d@v1"},
		"d@v1": {},
	}

	archivedPaths := map[string]bool{"d": true}

	result := findArchivedTransitive("a@v1", graph, archivedPaths, make(map[string]bool))
	if len(result) != 1 || result[0] != "d" {
		t.Errorf("expected [d], got %v", result)
	}
}

func TestFindArchivedTransitive_NoArchived(t *testing.T) {
	graph := map[string][]string{
		"a@v1": {"b@v1"},
		"b@v1": {},
	}

	archivedPaths := map[string]bool{}

	result := findArchivedTransitive("a@v1", graph, archivedPaths, make(map[string]bool))
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestFmtDate(t *testing.T) {
	ts := time.Date(2024, 7, 22, 14, 30, 45, 0, time.UTC)

	// Default date-only format
	cfg := &Config{DateFmt: "2006-01-02"}
	if got := fmtDate(cfg, ts); got != "2024-07-22" {
		t.Errorf("date-only: got %q, want %q", got, "2024-07-22")
	}

	// With time
	cfg = &Config{DateFmt: "2006-01-02 15:04:05"}
	if got := fmtDate(cfg, ts); got != "2024-07-22 14:30:45" {
		t.Errorf("with time: got %q, want %q", got, "2024-07-22 14:30:45")
	}

	// Zero time
	if got := fmtDate(cfg, time.Time{}); got != "" {
		t.Errorf("zero time: got %q, want empty", got)
	}
}

func TestFormatArchivedLine_WithTime(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02 15:04:05"}

	rs := RepoStatus{
		ArchivedAt: time.Date(2024, 7, 22, 14, 30, 45, 0, time.UTC),
		PushedAt:   time.Date(2021, 5, 5, 9, 15, 0, 0, time.UTC),
	}

	got := formatArchivedLine(cfg, "github.com/foo/bar", "v1.0.0", rs)
	if !strings.Contains(got, "2024-07-22 14:30:45") {
		t.Errorf("expected time in archived date, got %q", got)
	}
	if !strings.Contains(got, "2021-05-05 09:15:00") {
		t.Errorf("expected time in pushed date, got %q", got)
	}
}

func TestFormatArchivedLine(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02"}

	rs := RepoStatus{
		ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
	}

	got := formatArchivedLine(cfg, "github.com/foo/bar", "v1.2.3", rs)
	want := "github.com/foo/bar@v1.2.3 [ARCHIVED 2024-07-22, last pushed 2021-05-05]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestFormatArchivedLine_NoVersion(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02"}

	rs := RepoStatus{
		ArchivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	got := formatArchivedLine(cfg, "github.com/foo/bar", "", rs)
	if !strings.Contains(got, "github.com/foo/bar [ARCHIVED") {
		t.Errorf("expected no @ when version empty, got %q", got)
	}
	if strings.Contains(got, "last pushed") {
		t.Errorf("should not show last pushed when zero, got %q", got)
	}
}

func TestFormatArchivedLine_NoDates(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02"}

	rs := RepoStatus{}

	got := formatArchivedLine(cfg, "github.com/foo/bar", "v1.0.0", rs)
	want := "github.com/foo/bar@v1.0.0 [ARCHIVED]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// captureStdout captures stdout output during fn execution.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// defaultTestConfig returns a Config with defaults suitable for most tests.
func defaultTestConfig() *Config {
	return &Config{
		DateFmt:  "2006-01-02",
		SortMode: "name",
	}
}

func TestPrintJSON_ArchivedOnly(t *testing.T) {
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
			PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	skippedModules := []Module{
		{Path: "golang.org/x/text", Version: "v0.14.0", Direct: true},
		{Path: "google.golang.org/protobuf", Version: "v1.33.0", Direct: false},
		{Path: "gopkg.in/yaml.v3", Version: "v3.0.1", Direct: true},
		{Path: "cel.dev/expr", Version: "v0.25.1", Direct: false},
		{Path: "golang.org/x/net", Version: "v0.24.0", Direct: true},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, skippedModules, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if len(out.Archived) != 1 {
		t.Errorf("expected 1 archived, got %d", len(out.Archived))
	}
	if out.Archived[0].Module != "github.com/foo/bar" {
		t.Errorf("archived module = %q", out.Archived[0].Module)
	}
	if out.Active != nil {
		t.Error("expected no active modules when showAll=false")
	}
	if out.NonGitHubCount != 5 {
		t.Errorf("non_github_count = %d, want 5", out.NonGitHubCount)
	}
	if len(out.NonGitHubModules) != 5 {
		t.Errorf("non_github_modules length = %d, want 5", len(out.NonGitHubModules))
	}
	if out.NonGitHubModules[0].Module != "golang.org/x/text" {
		t.Errorf("non_github_modules[0].module = %q, want %q", out.NonGitHubModules[0].Module, "golang.org/x/text")
	}
	if out.NonGitHubModules[0].Version != "v0.14.0" {
		t.Errorf("non_github_modules[0].version = %q, want %q", out.NonGitHubModules[0].Version, "v0.14.0")
	}
	if !out.NonGitHubModules[0].Direct {
		t.Error("non_github_modules[0].direct should be true")
	}
	if out.NonGitHubModules[0].Host != "golang.org" {
		t.Errorf("non_github_modules[0].host = %q, want %q", out.NonGitHubModules[0].Host, "golang.org")
	}
	if out.TotalChecked != 2 {
		t.Errorf("total = %d, want 2", out.TotalChecked)
	}
}

func TestPrintJSON_ShowAll(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ShowAll = true
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: false,
			PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(out.Active) != 1 {
		t.Errorf("expected 1 active module with showAll=true, got %d", len(out.Active))
	}
}

func TestPrintJSON_NotFound(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:   Module{Path: "github.com/gone/repo", Owner: "gone", Repo: "repo"},
			NotFound: true,
			Error:    "Could not resolve",
		},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(out.NotFound) != 1 {
		t.Errorf("expected 1 not_found, got %d", len(out.NotFound))
	}
	if out.NotFound[0].Error != "Could not resolve" {
		t.Errorf("error = %q", out.NotFound[0].Error)
	}
}

func TestPrintJSON_EmptyArchived(t *testing.T) {
	cfg := defaultTestConfig()
	output := captureStdout(t, func() {
		PrintJSON(cfg, nil, nil, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Archived should be empty array, not null
	if !strings.Contains(output, `"archived": []`) {
		t.Error("expected archived to be empty array, not null")
	}
}

func TestPrintTable_ContainsArchivedModule(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil)
	})

	if !strings.Contains(output, "github.com/foo/bar") {
		t.Error("table output should contain the module path")
	}
	if !strings.Contains(output, "2024-07-22") {
		t.Error("table output should contain archived date")
	}
	if !strings.Contains(output, "direct") {
		t.Error("table output should show 'direct'")
	}
}

func TestPrintTable_NoArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: false,
			PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	skippedModules := []Module{
		{Path: "golang.org/x/text", Version: "v0.14.0", Direct: true},
		{Path: "google.golang.org/grpc", Version: "v1.60.0", Direct: true},
		{Path: "gopkg.in/yaml.v3", Version: "v3.0.1", Direct: false},
	}

	// Should not print any table to stdout when no archived
	output := captureStdout(t, func() {
		PrintTable(cfg, results, skippedModules)
	})

	if strings.Contains(output, "github.com/foo/bar") {
		t.Error("should not show active modules when showAll=false")
	}
	// Skipped modules should appear in stdout table
	if !strings.Contains(output, "golang.org/x/text") {
		t.Error("should show skipped module golang.org/x/text")
	}
	if !strings.Contains(output, "google.golang.org/grpc") {
		t.Error("should show skipped module google.golang.org/grpc")
	}
}

func TestPrintTable_ShowAll(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ShowAll = true
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: false, Owner: "foo", Repo: "bar"},
			IsArchived: false,
			PushedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			Module:     Module{Path: "github.com/archived/repo", Version: "v0.5.0", Direct: true, Owner: "archived", Repo: "repo"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	skippedModules := []Module{
		{Path: "golang.org/x/text", Version: "v0.14.0", Direct: true},
		{Path: "google.golang.org/grpc", Version: "v1.60.0", Direct: false},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, skippedModules)
	})

	if !strings.Contains(output, "github.com/archived/repo") {
		t.Error("should show archived module")
	}
	if !strings.Contains(output, "github.com/foo/bar") {
		t.Error("should show active module when showAll=true")
	}
	if !strings.Contains(output, "indirect") {
		t.Error("should show 'indirect' for indirect dep")
	}
}

func TestPrintTable_NotFoundModule(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:   Module{Path: "github.com/gone/repo", Owner: "gone", Repo: "repo"},
			NotFound: true,
			Error:    "Could not resolve",
		},
	}

	// NotFound goes to stderr, stdout should be empty
	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil)
	})

	if strings.Contains(output, "github.com/gone/repo") {
		t.Error("not-found modules should go to stderr, not stdout")
	}
}

func TestPrintTree_BasicTree(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC),
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
		PrintTree(cfg, results, graph, allModules, nil)
	})

	if !strings.Contains(output, "github.com/a/b@v1.0.0") {
		t.Error("should show direct dep with version")
	}
	if !strings.Contains(output, "github.com/x/y@v0.1.0") {
		t.Error("should show archived transitive dep with version")
	}
	if !strings.Contains(output, "ARCHIVED 2024-03-15") {
		t.Error("should show archived date")
	}
	if !strings.Contains(output, "last pushed 2023-12-01") {
		t.Error("should show last pushed date")
	}
}

func TestPrintTree_DirectArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
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
		PrintTree(cfg, results, graph, allModules, nil)
	})

	if !strings.Contains(output, "github.com/a/b@v1.0.0 [ARCHIVED 2024-06-01, last pushed 2024-05-01]") {
		t.Errorf("should show direct dep as archived with version and dates, got:\n%s", output)
	}
}

func TestPrintTree_NoArchived(t *testing.T) {
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
		PrintTree(cfg, results, graph, allModules, nil)
	})

	if output != "" {
		t.Errorf("expected no stdout output when no archived deps, got %q", output)
	}
}

func TestPrintTree_EmptyGraph(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b", Direct: true},
	}

	// Empty graph — no root key found, fallback to flat list
	graph := map[string][]string{}

	output := captureStdout(t, func() {
		PrintTree(cfg, results, graph, allModules, nil)
	})

	if !strings.Contains(output, "github.com/a/b@v1.0.0 [ARCHIVED") {
		t.Errorf("fallback should still list archived deps with version, got:\n%s", output)
	}
}

func TestParseModGraphLines(t *testing.T) {
	input := `root github.com/foo/bar@v1.0.0
root github.com/baz/qux@v2.0.0
github.com/foo/bar@v1.0.0 github.com/x/y@v0.1.0
`
	graph := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(input), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			graph[parts[0]] = append(graph[parts[0]], parts[1])
		}
	}

	if len(graph["root"]) != 2 {
		t.Errorf("root should have 2 children, got %d", len(graph["root"]))
	}
	if len(graph["github.com/foo/bar@v1.0.0"]) != 1 {
		t.Errorf("foo/bar should have 1 child, got %d", len(graph["github.com/foo/bar@v1.0.0"]))
	}
}

func TestPrintFiles_BasicOutput(t *testing.T) {
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
			IsArchived: true,
		},
		{
			Module:     Module{Path: "github.com/baz/qux", Version: "v2.0.0", Owner: "baz", Repo: "qux"},
			IsArchived: true,
		},
	}

	fileMatches := map[string][]FileMatch{
		"github.com/foo/bar": {
			{File: "audit/hash.go", Line: 14, ImportPath: "github.com/foo/bar"},
			{File: "vault/policy.go", Line: 17, ImportPath: "github.com/foo/bar"},
		},
		"github.com/baz/qux": {
			{File: "cmd/main.go", Line: 5, ImportPath: "github.com/baz/qux"},
		},
	}

	output := captureStdout(t, func() {
		PrintFiles(results, fileMatches)
	})

	if !strings.Contains(output, "github.com/baz/qux (1 file)") {
		t.Errorf("should show singular 'file' for 1 match, got:\n%s", output)
	}
	if !strings.Contains(output, "github.com/foo/bar (2 files)") {
		t.Errorf("should show '2 files', got:\n%s", output)
	}
	if !strings.Contains(output, "audit/hash.go:14") {
		t.Errorf("should show file:line, got:\n%s", output)
	}
	if !strings.Contains(output, "vault/policy.go:17") {
		t.Errorf("should show file:line, got:\n%s", output)
	}
}

func TestPrintFiles_ZeroFiles(t *testing.T) {
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
			IsArchived: true,
		},
	}

	fileMatches := map[string][]FileMatch{}

	output := captureStdout(t, func() {
		PrintFiles(results, fileMatches)
	})

	if !strings.Contains(output, "github.com/foo/bar (0 files)") {
		t.Errorf("modules with no imports should show 0 files, got:\n%s", output)
	}
}

func TestPrintJSON_WithSourceFiles(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	fileMatches := map[string][]FileMatch{
		"github.com/foo/bar": {
			{File: "audit/hash.go", Line: 14, ImportPath: "github.com/foo/bar"},
		},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, fileMatches, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if len(out.Archived) != 1 {
		t.Fatalf("expected 1 archived, got %d", len(out.Archived))
	}
	if len(out.Archived[0].SourceFiles) != 1 {
		t.Fatalf("expected 1 source file, got %d", len(out.Archived[0].SourceFiles))
	}
	sf := out.Archived[0].SourceFiles[0]
	if sf.File != "audit/hash.go" {
		t.Errorf("source_files[0].file = %q, want %q", sf.File, "audit/hash.go")
	}
	if sf.Line != 14 {
		t.Errorf("source_files[0].line = %d, want 14", sf.Line)
	}
	if sf.Import != "github.com/foo/bar" {
		t.Errorf("source_files[0].import = %q, want %q", sf.Import, "github.com/foo/bar")
	}
}

func TestPrintJSON_NoSourceFilesWhenNil(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
		},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, nil)
	})

	if strings.Contains(output, "source_files") {
		t.Errorf("should not include source_files when fileMatches is nil, got:\n%s", output)
	}
}

func TestPrintTree_WithFileCount(t *testing.T) {
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

	fileMatches := map[string][]FileMatch{
		"github.com/a/b": {
			{File: "foo.go", Line: 5, ImportPath: "github.com/a/b"},
			{File: "bar.go", Line: 10, ImportPath: "github.com/a/b"},
		},
	}

	output := captureStdout(t, func() {
		PrintTree(cfg, results, graph, allModules, fileMatches)
	})

	if !strings.Contains(output, "(2 files)") {
		t.Errorf("tree output should show file count, got:\n%s", output)
	}
}

func TestBuildTree_BasicEntries(t *testing.T) {
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

	entries, ctx := buildTree(results, graph, allModules)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].directPath != "github.com/a/b" {
		t.Errorf("directPath = %q, want github.com/a/b", entries[0].directPath)
	}
	if len(entries[0].archived) != 1 || entries[0].archived[0] != "github.com/x/y" {
		t.Errorf("archived = %v, want [github.com/x/y]", entries[0].archived)
	}
	if !ctx.archivedPaths["github.com/x/y"] {
		t.Error("expected x/y in archivedPaths")
	}
}

func TestBuildTree_NoArchived(t *testing.T) {
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Owner: "a", Repo: "b"},
			IsArchived: false,
		},
	}
	allModules := []Module{
		{Path: "github.com/a/b", Owner: "a", Repo: "b"},
	}
	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {},
	}

	entries, _ := buildTree(results, graph, allModules)
	if entries != nil {
		t.Errorf("expected nil entries when no archived, got %v", entries)
	}
}

func TestBuildTree_EmptyGraph(t *testing.T) {
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
			IsArchived: true,
		},
	}
	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
	}

	entries, _ := buildTree(results, map[string][]string{}, allModules)
	if len(entries) != 1 {
		t.Fatalf("expected 1 fallback entry, got %d", len(entries))
	}
	if entries[0].directPath != "github.com/a/b" {
		t.Errorf("directPath = %q", entries[0].directPath)
	}
}

func TestPrintTreeJSON_Basic(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC),
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

	skippedModules := []Module{
		{Path: "golang.org/x/text", Version: "v0.14.0", Direct: true},
		{Path: "google.golang.org/protobuf", Version: "v1.33.0", Direct: false},
		{Path: "gopkg.in/yaml.v3", Version: "v3.0.1", Direct: true},
		{Path: "cel.dev/expr", Version: "v0.25.1", Direct: false},
		{Path: "golang.org/x/net", Version: "v0.24.0", Direct: true},
	}

	output := captureStdout(t, func() {
		PrintTreeJSON(cfg, results, graph, allModules, nil, skippedModules)
	})

	var out JSONTreeOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if out.NonGitHubCount != 5 {
		t.Errorf("non_github_count = %d, want 5", out.NonGitHubCount)
	}
	if len(out.NonGitHubModules) != 5 {
		t.Errorf("non_github_modules length = %d, want 5", len(out.NonGitHubModules))
	}
	if out.NonGitHubModules[0].Module != "golang.org/x/text" {
		t.Errorf("non_github_modules[0].module = %q, want %q", out.NonGitHubModules[0].Module, "golang.org/x/text")
	}
	if len(out.Tree) != 1 {
		t.Fatalf("expected 1 tree entry, got %d", len(out.Tree))
	}

	entry := out.Tree[0]
	if entry.Module != "github.com/a/b" {
		t.Errorf("module = %q", entry.Module)
	}
	if entry.Version != "v1.0.0" {
		t.Errorf("version = %q", entry.Version)
	}
	if entry.Archived {
		t.Error("direct dep should not be archived")
	}
	if len(entry.ArchivedDependencies) != 1 {
		t.Fatalf("expected 1 archived dep, got %d", len(entry.ArchivedDependencies))
	}

	dep := entry.ArchivedDependencies[0]
	if dep.Module != "github.com/x/y" {
		t.Errorf("dep module = %q", dep.Module)
	}
	if dep.ArchivedAt != "2024-03-15T00:00:00Z" {
		t.Errorf("dep archived_at = %q", dep.ArchivedAt)
	}
}

func TestPrintTreeJSON_DirectArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
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
		PrintTreeJSON(cfg, results, graph, allModules, nil, nil)
	})

	var out JSONTreeOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(out.Tree) != 1 {
		t.Fatalf("expected 1 tree entry, got %d", len(out.Tree))
	}
	if !out.Tree[0].Archived {
		t.Error("direct dep should be archived")
	}
	if out.Tree[0].ArchivedAt != "2024-06-01T00:00:00Z" {
		t.Errorf("archived_at = %q", out.Tree[0].ArchivedAt)
	}
}

func TestPrintTreeJSON_WithSourceFiles(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	allModules := []Module{
		{Path: "github.com/a/b", Version: "v1.0.0", Owner: "a", Repo: "b"},
		{Path: "github.com/x/y", Version: "v0.1.0", Owner: "x", Repo: "y"},
	}

	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {"github.com/x/y@v0.1.0"},
	}

	fileMatches := map[string][]FileMatch{
		"github.com/x/y": {
			{File: "foo.go", Line: 5, ImportPath: "github.com/x/y"},
		},
	}

	output := captureStdout(t, func() {
		PrintTreeJSON(cfg, results, graph, allModules, fileMatches, nil)
	})

	var out JSONTreeOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dep := out.Tree[0].ArchivedDependencies[0]
	if len(dep.SourceFiles) != 1 {
		t.Fatalf("expected 1 source file, got %d", len(dep.SourceFiles))
	}
	if dep.SourceFiles[0].File != "foo.go" {
		t.Errorf("source file = %q", dep.SourceFiles[0].File)
	}
}

func TestPrintTreeJSON_NoArchived(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/a/b", Owner: "a", Repo: "b"},
			IsArchived: false,
		},
	}
	allModules := []Module{
		{Path: "github.com/a/b", Owner: "a", Repo: "b"},
	}
	graph := map[string][]string{
		"mymodule":              {"github.com/a/b@v1.0.0"},
		"github.com/a/b@v1.0.0": {},
	}

	output := captureStdout(t, func() {
		PrintTreeJSON(cfg, results, graph, allModules, nil, nil)
	})

	var out JSONTreeOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if !strings.Contains(output, `"tree": []`) {
		t.Error("expected tree to be empty array, not null")
	}
}

func TestCalcDuration(t *testing.T) {
	tests := []struct {
		name       string
		archivedAt time.Time
		endDate    time.Time
		wantY      int
		wantM      int
		wantD      int
	}{
		{name: "same day", archivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), wantY: 0, wantM: 0, wantD: 1},
		{name: "one day apart", archivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), wantY: 0, wantM: 0, wantD: 2},
		{name: "one month", archivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC), wantY: 0, wantM: 1, wantD: 0},
		{name: "one year", archivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), wantY: 1, wantM: 0, wantD: 0},
		{name: "years months and days", archivedAt: time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC), wantY: 2, wantM: 4, wantD: 8},
		{name: "across year boundary", archivedAt: time.Date(2023, 11, 15, 0, 0, 0, 0, time.UTC), endDate: time.Date(2024, 2, 20, 0, 0, 0, 0, time.UTC), wantY: 0, wantM: 3, wantD: 6},
		{name: "time is ignored", archivedAt: time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC), endDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), wantY: 0, wantM: 0, wantD: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			y, m, d := calcDuration(tt.archivedAt, tt.endDate)
			if y != tt.wantY || m != tt.wantM || d != tt.wantD {
				t.Errorf("calcDuration() = (%d, %d, %d), want (%d, %d, %d)",
					y, m, d, tt.wantY, tt.wantM, tt.wantD)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cfg := &Config{Duration: DurationConfig{Enabled: true, EndDate: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)}, DateFmt: "2006-01-02"}

	tests := []struct {
		name       string
		archivedAt time.Time
		want       string
	}{
		{"years months days", time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC), "3y11m7d"},
		{"years and one day (inclusive)", time.Date(2024, 2, 21, 0, 0, 0, 0, time.UTC), "2y1d"},
		{"only days", time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), "2d"},
		{"one year and one day", time.Date(2025, 2, 21, 0, 0, 0, 0, time.UTC), "1y1d"},
		{"one month and one day", time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC), "1m1d"},
		{"same day", time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC), "1d"},
		{"zero time returns empty", time.Time{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(cfg, tt.archivedAt)
			if got != tt.want {
				t.Errorf("formatDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDurationShort(t *testing.T) {
	cfg := &Config{Duration: DurationConfig{Enabled: true, EndDate: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)}, DateFmt: "2006-01-02"}

	tests := []struct {
		name       string
		archivedAt time.Time
		want       string
	}{
		{"years months days", time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC), "3y11m7d"},
		{"years and one day", time.Date(2024, 2, 21, 0, 0, 0, 0, time.UTC), "2y1d"},
		{"only days", time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), "2d"},
		{"same day", time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC), "1d"},
		{"zero time returns empty", time.Time{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDurationShort(cfg, tt.archivedAt)
			if got != tt.want {
				t.Errorf("formatDurationShort() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuration_Disabled(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02"}
	got := formatDuration(cfg, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	if got != "" {
		t.Errorf("expected empty when disabled, got %q", got)
	}
}

func TestPrintTable_WithDuration(t *testing.T) {
	cfg := &Config{Duration: DurationConfig{Enabled: true, EndDate: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)}, DateFmt: "2006-01-02", SortMode: "name"}

	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil)
	})

	if !strings.Contains(output, "DURATION") {
		t.Error("table should contain DURATION header when enabled")
	}
	if !strings.Contains(output, "1y7m") {
		t.Errorf("table should contain duration value, got:\n%s", output)
	}
}

func TestPrintTable_NoDurationColumn(t *testing.T) {
	cfg := defaultTestConfig()

	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil)
	})

	if strings.Contains(output, "DURATION") {
		t.Error("table should NOT contain DURATION header when disabled")
	}
}

func TestPrintJSON_WithDuration(t *testing.T) {
	cfg := &Config{Duration: DurationConfig{Enabled: true, EndDate: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)}, DateFmt: "2006-01-02"}

	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if out.Archived[0].ArchivedDuration == "" {
		t.Error("expected archived_duration to be set")
	}
	if !strings.Contains(out.Archived[0].ArchivedDuration, "1y") {
		t.Errorf("duration = %q, expected to contain '1y'", out.Archived[0].ArchivedDuration)
	}
}

func TestFormatArchivedLine_WithDuration(t *testing.T) {
	cfg := &Config{Duration: DurationConfig{Enabled: true, EndDate: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)}, DateFmt: "2006-01-02"}

	rs := RepoStatus{
		ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
	}

	got := formatArchivedLine(cfg, "github.com/foo/bar", "v1.0.0", rs)
	if !strings.Contains(got, "1y7m") {
		t.Errorf("expected compact duration in archived line, got %q", got)
	}
	if !strings.Contains(got, "last pushed") {
		t.Errorf("expected last pushed still present, got %q", got)
	}
}

func TestPluralize(t *testing.T) {
	if got := pluralize(0, "file", "files"); got != "files" {
		t.Errorf("pluralize(0) = %q, want %q", got, "files")
	}
	if got := pluralize(1, "file", "files"); got != "file" {
		t.Errorf("pluralize(1) = %q, want %q", got, "file")
	}
	if got := pluralize(2, "file", "files"); got != "files" {
		t.Errorf("pluralize(2) = %q, want %q", got, "files")
	}
}

func TestPrintSkippedTable_Enriched(t *testing.T) {
	cfg := defaultTestConfig()
	modules := []Module{
		{Path: "golang.org/x/mod", Version: "v0.17.0", Direct: true, LatestVersion: "v0.22.0", VersionTime: time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC), SourceURL: "https://go.googlesource.com/mod"},
		{Path: "golang.org/x/text", Version: "v0.21.0", Direct: false, LatestVersion: "v0.21.0", VersionTime: time.Date(2023, 10, 11, 0, 0, 0, 0, time.UTC), SourceURL: "https://go.googlesource.com/text"},
		{Path: "gopkg.in/yaml.v3", Version: "v3.0.1", Direct: true},
	}

	output := captureStdout(t, func() {
		PrintSkippedTable(cfg, modules)
	})

	if !strings.Contains(output, "LATEST") {
		t.Error("table should contain LATEST header")
	}
	if !strings.Contains(output, "PUBLISHED") {
		t.Error("table should contain PUBLISHED header")
	}
	if !strings.Contains(output, "SOURCE") {
		t.Error("table should contain SOURCE header")
	}
	if !strings.Contains(output, "v0.22.0") {
		t.Error("table should show latest version v0.22.0")
	}
	if !strings.Contains(output, "2024-03-15") {
		t.Error("table should show published date")
	}
	if !strings.Contains(output, "https://go.googlesource.com/mod") {
		t.Error("table should show source URL")
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "golang.org/x/text") {
			if !strings.Contains(line, "-") {
				t.Errorf("same-version latest should show '-', got line: %s", line)
			}
			break
		}
	}
}

func TestPrintDeprecatedTable(t *testing.T) {
	modules := []Module{
		{Path: "github.com/old/thing", Version: "v0.5.0", Direct: true, Deprecated: "Use github.com/new/thing."},
		{Path: "github.com/ancient/lib", Version: "v1.0.0", Direct: false, Deprecated: "No longer maintained."},
	}

	output := captureStdout(t, func() {
		PrintDeprecatedTable(modules)
	})

	if !strings.Contains(output, "MODULE") {
		t.Error("table should contain MODULE header")
	}
	if !strings.Contains(output, "VERSION") {
		t.Error("table should contain VERSION header")
	}
	if !strings.Contains(output, "DIRECT") {
		t.Error("table should contain DIRECT header")
	}
	if !strings.Contains(output, "MESSAGE") {
		t.Error("table should contain MESSAGE header")
	}

	ancientIdx := strings.Index(output, "github.com/ancient/lib")
	oldIdx := strings.Index(output, "github.com/old/thing")
	if ancientIdx < 0 || oldIdx < 0 {
		t.Error("both modules should appear in output")
	}
	if ancientIdx > oldIdx {
		t.Error("modules should be sorted alphabetically")
	}

	if !strings.Contains(output, "direct") {
		t.Error("should show 'direct'")
	}
	if !strings.Contains(output, "indirect") {
		t.Error("should show 'indirect'")
	}
	if !strings.Contains(output, "Use github.com/new/thing.") {
		t.Error("should show deprecation message")
	}
	if !strings.Contains(output, "No longer maintained.") {
		t.Error("should show deprecation message")
	}
}

func TestPrintTable_WithDeprecatedSection(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
			PushedAt:   time.Date(2021, 5, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	deprecatedModules := []Module{
		{Path: "github.com/old/thing", Version: "v0.5.0", Direct: true, Deprecated: "Use github.com/new/thing."},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil, deprecatedModules)
	})

	if !strings.Contains(output, "github.com/foo/bar") {
		t.Error("should show archived module")
	}
	if !strings.Contains(output, "github.com/old/thing") {
		t.Error("should show deprecated module")
	}
	if !strings.Contains(output, "Use github.com/new/thing.") {
		t.Error("should show deprecation message")
	}
}

func TestBuildTreeJSONOutput_WithDeprecated(t *testing.T) {
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
	}

	deprecatedModules := []Module{
		{Path: "github.com/old/thing", Version: "v0.5.0", Direct: true, Owner: "old", Repo: "thing", Deprecated: "Use github.com/new/thing."},
	}

	out := buildTreeJSONOutput(cfg, results, graph, allModules, nil, nil, deprecatedModules)

	if len(out.Deprecated) != 1 {
		t.Fatalf("expected 1 deprecated entry, got %d", len(out.Deprecated))
	}
	if out.Deprecated[0].Module != "github.com/old/thing" {
		t.Errorf("deprecated module = %q", out.Deprecated[0].Module)
	}
	if out.Deprecated[0].DeprecatedMessage != "Use github.com/new/thing." {
		t.Errorf("deprecated message = %q", out.Deprecated[0].DeprecatedMessage)
	}
}

func TestPrintJSON_WithDeprecated(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	deprecatedModules := []Module{
		{Path: "github.com/old/thing", Version: "v0.5.0", Direct: true, Owner: "old", Repo: "thing", Deprecated: "Use github.com/new/thing."},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, nil, deprecatedModules)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if len(out.Deprecated) != 1 {
		t.Fatalf("expected 1 deprecated, got %d", len(out.Deprecated))
	}
	if out.Deprecated[0].Module != "github.com/old/thing" {
		t.Errorf("deprecated module = %q", out.Deprecated[0].Module)
	}
	if out.Deprecated[0].DeprecatedMessage != "Use github.com/new/thing." {
		t.Errorf("deprecated message = %q", out.Deprecated[0].DeprecatedMessage)
	}
}

func TestPrintJSON_NonGitHubModules(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{
			Module:     Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
			IsArchived: true,
			ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	nonGitHubModules := []Module{
		{Path: "golang.org/x/text", Version: "v0.14.0", Direct: true, LatestVersion: "v0.21.0", VersionTime: time.Date(2023, 10, 11, 17, 42, 28, 0, time.UTC), SourceURL: "https://go.googlesource.com/text"},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nonGitHubModules, nil, nil)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if out.NonGitHubCount != 1 {
		t.Errorf("non_github_count = %d, want 1", out.NonGitHubCount)
	}
	if len(out.NonGitHubModules) != 1 {
		t.Fatalf("non_github_modules length = %d, want 1", len(out.NonGitHubModules))
	}

	m := out.NonGitHubModules[0]
	if m.LatestVersion != "v0.21.0" {
		t.Errorf("latest_version = %q, want v0.21.0", m.LatestVersion)
	}
	if m.Published != "2023-10-11T17:42:28Z" {
		t.Errorf("published = %q", m.Published)
	}
	if m.Host != "golang.org" {
		t.Errorf("host = %q, want golang.org", m.Host)
	}
	if m.SourceURL != "https://go.googlesource.com/text" {
		t.Errorf("source_url = %q", m.SourceURL)
	}

	if !strings.Contains(output, `"non_github_count"`) {
		t.Error("JSON should use non_github_count field name")
	}
	if !strings.Contains(output, `"non_github_modules"`) {
		t.Error("JSON should use non_github_modules field name")
	}
}

func TestPrintFilesPlain(t *testing.T) {
	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"}, IsArchived: true},
		{Module: Module{Path: "github.com/baz/qux", Version: "v2.0.0", Owner: "baz", Repo: "qux"}, IsArchived: true},
	}

	fileMatches := map[string][]FileMatch{
		"github.com/foo/bar": {{File: "audit/hash.go", Line: 14, ImportPath: "github.com/foo/bar"}, {File: "vault/policy.go", Line: 17, ImportPath: "github.com/foo/bar"}},
		"github.com/baz/qux": {{File: "cmd/main.go", Line: 5, ImportPath: "github.com/baz/qux"}},
	}

	output := captureStdout(t, func() {
		PrintFilesPlain(results, fileMatches)
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), output)
	}
	if lines[0] != "cmd/main.go:5:github.com/baz/qux" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "audit/hash.go:14:github.com/foo/bar" {
		t.Errorf("line 1 = %q", lines[1])
	}
	if lines[2] != "vault/policy.go:17:github.com/foo/bar" {
		t.Errorf("line 2 = %q", lines[2])
	}
}

func TestParseThreshold(t *testing.T) {
	tests := []struct {
		input   string
		y, m, d int
		wantErr bool
	}{
		{"2y", 2, 0, 0, false},
		{"1y6m", 1, 6, 0, false},
		{"180d", 0, 0, 180, false},
		{"1y6m15d", 1, 6, 15, false},
		{"3m", 0, 3, 0, false},
		{"", 0, 0, 0, true},
		{"abc", 0, 0, 0, true},
		{"2x", 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			y, m, d, err := parseThreshold(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if y != tt.y || m != tt.m || d != tt.d {
				t.Errorf("parseThreshold(%q) = (%d, %d, %d), want (%d, %d, %d)", tt.input, y, m, d, tt.y, tt.m, tt.d)
			}
		})
	}
}

func TestExceedsThreshold(t *testing.T) {
	old := time.Now().AddDate(-3, 0, 0)
	if !exceedsThreshold(old, 2, 0, 0) {
		t.Error("3y old should exceed 2y threshold")
	}
	recent := time.Now().AddDate(-1, 0, 0)
	if exceedsThreshold(recent, 2, 0, 0) {
		t.Error("1y old should not exceed 2y threshold")
	}
	if exceedsThreshold(time.Time{}, 2, 0, 0) {
		t.Error("zero time should not exceed threshold")
	}
}

func TestFilterStale(t *testing.T) {
	cfg := &Config{Stale: StaleConfig{Enabled: true, Years: 2}, DateFmt: "2006-01-02"}

	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"}, IsArchived: false, PushedAt: time.Now().AddDate(-3, 0, 0)},
		{Module: Module{Path: "github.com/baz/qux", Version: "v2.0.0", Owner: "baz", Repo: "qux"}, IsArchived: false, PushedAt: time.Now().AddDate(0, -6, 0)},
		{Module: Module{Path: "github.com/old/archived", Version: "v0.1.0", Owner: "old", Repo: "archived"}, IsArchived: true, PushedAt: time.Now().AddDate(-5, 0, 0)},
	}

	stale := filterStale(cfg, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale, got %d", len(stale))
	}
	if stale[0].Module.Path != "github.com/foo/bar" {
		t.Errorf("stale[0] = %q, want github.com/foo/bar", stale[0].Module.Path)
	}
}

func TestFilterStale_Disabled(t *testing.T) {
	cfg := &Config{DateFmt: "2006-01-02"}

	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar"}, PushedAt: time.Now().AddDate(-5, 0, 0)},
	}
	stale := filterStale(cfg, results)
	if stale != nil {
		t.Error("expected nil when stale disabled")
	}
}

func TestPrintStaleTable(t *testing.T) {
	cfg := &Config{Stale: StaleConfig{Enabled: true, Years: 2}, DateFmt: "2006-01-02", SortMode: "name"}

	stale := []RepoStatus{
		{Module: Module{Path: "github.com/old/repo", Version: "v1.0.0", Direct: true}, PushedAt: time.Date(2022, 1, 15, 0, 0, 0, 0, time.UTC)},
	}

	output := captureStdout(t, func() {
		PrintStaleTable(cfg, stale)
	})

	if !strings.Contains(output, "github.com/old/repo") {
		t.Error("should contain stale module path")
	}
	if !strings.Contains(output, "2022-01-15") {
		t.Error("should contain pushed date")
	}
	if !strings.Contains(output, "direct") {
		t.Error("should show direct label")
	}
}

func TestPrintJSON_WithStale(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{Module: Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"}, IsArchived: true, ArchivedAt: time.Date(2024, 7, 22, 0, 0, 0, 0, time.UTC)},
	}

	stale := []RepoStatus{
		{Module: Module{Path: "github.com/old/repo", Version: "v1.0.0", Direct: true, Owner: "old", Repo: "repo"}, PushedAt: time.Date(2022, 1, 15, 0, 0, 0, 0, time.UTC)},
	}

	output := captureStdout(t, func() {
		PrintJSON(cfg, results, nil, nil, stale)
	})

	var out JSONOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if len(out.Stale) != 1 {
		t.Fatalf("expected 1 stale, got %d", len(out.Stale))
	}
	if out.Stale[0].Module != "github.com/old/repo" {
		t.Errorf("stale module = %q", out.Stale[0].Module)
	}
	if out.Stale[0].PushedAt != "2022-01-15T00:00:00Z" {
		t.Errorf("stale pushed_at = %q", out.Stale[0].PushedAt)
	}
}

func TestSortResults_ByName(t *testing.T) {
	cfg := &Config{SortMode: "name"}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/z/z"}},
		{Module: Module{Path: "github.com/a/a"}},
		{Module: Module{Path: "github.com/m/m"}},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/a/a" {
		t.Errorf("expected a/a first, got %s", results[0].Module.Path)
	}
	if results[2].Module.Path != "github.com/z/z" {
		t.Errorf("expected z/z last, got %s", results[2].Module.Path)
	}
}

func TestSortResults_ByDuration(t *testing.T) {
	cfg := &Config{SortMode: "duration"}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/new/repo"}, ArchivedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/old/repo"}, ArchivedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/mid/repo"}, ArchivedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/old/repo" {
		t.Errorf("expected old/repo first, got %s", results[0].Module.Path)
	}
	if results[2].Module.Path != "github.com/new/repo" {
		t.Errorf("expected new/repo last, got %s", results[2].Module.Path)
	}
}

func TestSortResults_ByPushed(t *testing.T) {
	cfg := &Config{SortMode: "pushed"}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/recent/repo"}, PushedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/old/repo"}, PushedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/old/repo" {
		t.Errorf("expected old/repo first, got %s", results[0].Module.Path)
	}
}

func TestParseSortFlag(t *testing.T) {
	tests := []struct {
		input       string
		wantMode    string
		wantReverse bool
	}{
		{"name", "name", false},
		{"duration", "duration", false},
		{"pushed", "pushed", false},
		{"name:asc", "name", false},
		{"name:desc", "name", true},
		{"pushed:desc", "pushed", false},
		{"pushed:asc", "pushed", true},
		{"duration:desc", "duration", false},
		{"duration:asc", "duration", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			mode, reverse := parseSortFlag(tt.input)
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
			if reverse != tt.wantReverse {
				t.Errorf("reverse = %v, want %v", reverse, tt.wantReverse)
			}
		})
	}
}

func TestSortResults_ByNameDesc(t *testing.T) {
	cfg := &Config{SortMode: "name", SortReverse: true}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/a/a"}},
		{Module: Module{Path: "github.com/m/m"}},
		{Module: Module{Path: "github.com/z/z"}},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/z/z" {
		t.Errorf("expected z/z first, got %s", results[0].Module.Path)
	}
	if results[2].Module.Path != "github.com/a/a" {
		t.Errorf("expected a/a last, got %s", results[2].Module.Path)
	}
}

func TestSortResults_ByDurationAsc(t *testing.T) {
	cfg := &Config{SortMode: "duration", SortReverse: true}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/old/repo"}, ArchivedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/new/repo"}, ArchivedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/mid/repo"}, ArchivedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/new/repo" {
		t.Errorf("expected new/repo first, got %s", results[0].Module.Path)
	}
	if results[2].Module.Path != "github.com/old/repo" {
		t.Errorf("expected old/repo last, got %s", results[2].Module.Path)
	}
}

func TestSortResults_ByPushedAsc(t *testing.T) {
	cfg := &Config{SortMode: "pushed", SortReverse: true}
	results := []RepoStatus{
		{Module: Module{Path: "github.com/old/repo"}, PushedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/recent/repo"}, PushedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	sortResults(cfg, results)

	if results[0].Module.Path != "github.com/recent/repo" {
		t.Errorf("expected recent/repo first, got %s", results[0].Module.Path)
	}
}

func TestPrintTable_Grouping(t *testing.T) {
	cfg := defaultTestConfig()
	results := []RepoStatus{
		{Module: Module{Path: "github.com/direct/one", Version: "v1.0.0", Direct: true, Owner: "direct", Repo: "one"}, IsArchived: true, ArchivedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/indirect/two", Version: "v2.0.0", Direct: false, Owner: "indirect", Repo: "two"}, IsArchived: true, ArchivedAt: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)},
	}

	output := captureStdout(t, func() {
		PrintTable(cfg, results, nil)
	})

	if !strings.Contains(output, "Direct (1)") {
		t.Errorf("should show Direct group header, got:\n%s", output)
	}
	if !strings.Contains(output, "Indirect (1)") {
		t.Errorf("should show Indirect group header, got:\n%s", output)
	}
}

func TestFormatThreshold(t *testing.T) {
	cfg := &Config{Stale: StaleConfig{Years: 2}}
	if got := formatThreshold(cfg); got != "2y" {
		t.Errorf("formatThreshold() = %q, want %q", got, "2y")
	}

	cfg = &Config{Stale: StaleConfig{Years: 1, Months: 6}}
	if got := formatThreshold(cfg); got != "1y6m" {
		t.Errorf("formatThreshold() = %q, want %q", got, "1y6m")
	}

	cfg = &Config{Stale: StaleConfig{Days: 180}}
	if got := formatThreshold(cfg); got != "180d" {
		t.Errorf("formatThreshold() = %q, want %q", got, "180d")
	}
}

func TestPrintIgnoredTable(t *testing.T) {
	cfg := defaultTestConfig()
	ignored := []RepoStatus{
		{Module: Module{Path: "github.com/pkg/errors", Version: "v0.9.1", Direct: false}, IsArchived: true, ArchivedAt: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC), PushedAt: time.Date(2021, 11, 2, 0, 0, 0, 0, time.UTC)},
		{Module: Module{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true}, IsArchived: false, PushedAt: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)},
	}

	output := captureStdout(t, func() {
		PrintIgnoredTable(cfg, ignored, nil)
	})

	if !strings.Contains(output, "github.com/pkg/errors") {
		t.Error("expected pkg/errors in output")
	}
	if !strings.Contains(output, "archived") {
		t.Error("expected 'archived' status in output")
	}
	if !strings.Contains(output, "active") {
		t.Error("expected 'active' status in output")
	}
	if !strings.Contains(output, "2021-12-01") {
		t.Error("expected archived date in output")
	}
}

func TestPrintIgnoredTable_WithReasons(t *testing.T) {
	cfg := defaultTestConfig()
	ignored := []RepoStatus{
		{Module: Module{Path: "github.com/pkg/errors", Version: "v0.9.1", Direct: false}, IsArchived: true, ArchivedAt: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC), PushedAt: time.Date(2021, 11, 2, 0, 0, 0, 0, time.UTC)},
	}

	il := NewIgnoreList()
	il.AddWithReason("github.com/pkg/errors", "Vendored replacement available")

	output := captureStdout(t, func() {
		PrintIgnoredTable(cfg, ignored, il)
	})

	if !strings.Contains(output, "REASON") {
		t.Error("expected REASON header")
	}
	if !strings.Contains(output, "Vendored replacement available") {
		t.Error("expected reason text in output")
	}
}

func TestPrintIgnoredTable_Empty(t *testing.T) {
	cfg := defaultTestConfig()
	output := captureStdout(t, func() {
		PrintIgnoredTable(cfg, nil, nil)
	})

	if output != "" {
		t.Errorf("expected no output for empty ignored list, got %q", output)
	}
}
