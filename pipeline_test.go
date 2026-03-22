package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadFixtureResults reads a github_response.json fixture file and returns []RepoStatus.
func loadFixtureResults(t *testing.T, path string) []RepoStatus {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var results []RepoStatus
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	return results
}

// fixtureNow is the fixed reference time for all pipeline tests.
var fixtureNow = time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

func TestPipeline_MixedArchived(t *testing.T) {
	// Parse real go.mod
	gomodPath := filepath.Join("testdata", "fixtures", "mixed-archived", "go.mod")
	allModules, err := ParseGoMod(gomodPath)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}

	// Filter to GitHub modules
	githubModules, nonGitHubModules := FilterGitHub(allModules, false)
	if len(githubModules) == 0 {
		t.Fatal("expected GitHub modules from fixture")
	}
	if len(nonGitHubModules) == 0 {
		t.Fatal("expected non-GitHub modules from fixture")
	}

	// Load fixture results (skip API)
	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "mixed-archived", "github_response.json"))

	// Filter to archived only
	var archived []RepoStatus
	for _, r := range results {
		if r.IsArchived {
			archived = append(archived, r)
		}
	}
	if len(archived) != 2 {
		t.Fatalf("expected 2 archived, got %d", len(archived))
	}

	// Test table output
	cfg := defaultTestConfig()
	cfg.Now = fixtureNow
	output := captureStdout(t, func() {
		PrintTable(cfg, archived, nonGitHubModules)
	})
	if !strings.Contains(output, "github.com/pkg/errors") {
		t.Error("table output should contain pkg/errors")
	}
	if !strings.Contains(output, "github.com/mitchellh/mapstructure") {
		t.Error("table output should contain mitchellh/mapstructure")
	}

	// Test JSON output
	jsonOutput := captureStdout(t, func() {
		PrintJSON(cfg, archived, nonGitHubModules, nil, nil)
	})
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &jsonData); err != nil {
		t.Fatalf("JSON output should be valid JSON: %v", err)
	}
	archivedArr, ok := jsonData["archived"].([]interface{})
	if !ok {
		t.Fatal("JSON should have 'archived' array")
	}
	if len(archivedArr) != 2 {
		t.Errorf("JSON archived count = %d, want 2", len(archivedArr))
	}

	// Test Markdown output
	mdOutput := captureStdout(t, func() {
		PrintMarkdown(cfg, archived, nonGitHubModules)
	})
	if !strings.Contains(mdOutput, "| github.com/pkg/errors") {
		t.Error("markdown should contain pkg/errors in table row")
	}
}

func TestPipeline_AllClean(t *testing.T) {
	gomodPath := filepath.Join("testdata", "fixtures", "all-clean", "go.mod")
	allModules, err := ParseGoMod(gomodPath)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}

	githubModules, _ := FilterGitHub(allModules, false)
	if len(githubModules) != 3 {
		t.Fatalf("expected 3 GitHub modules, got %d", len(githubModules))
	}

	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "all-clean", "github_response.json"))

	// No archived
	var archived []RepoStatus
	for _, r := range results {
		if r.IsArchived {
			archived = append(archived, r)
		}
	}
	if len(archived) != 0 {
		t.Errorf("expected 0 archived, got %d", len(archived))
	}

	// JSON output should have empty archived array
	cfg := defaultTestConfig()
	cfg.Now = fixtureNow
	jsonOutput := captureStdout(t, func() {
		PrintJSON(cfg, archived, nil, nil, nil)
	})
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &jsonData); err != nil {
		t.Fatalf("JSON output should be valid JSON: %v", err)
	}
	archivedArr, ok := jsonData["archived"].([]interface{})
	if !ok || len(archivedArr) != 0 {
		t.Error("JSON archived should be empty array")
	}
}

func TestPipeline_DirectOnly(t *testing.T) {
	gomodPath := filepath.Join("testdata", "fixtures", "mixed-archived", "go.mod")
	allModules, err := ParseGoMod(gomodPath)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}

	// Filter direct only
	directModules, _ := FilterGitHub(allModules, true)
	for _, m := range directModules {
		if !m.Direct {
			t.Errorf("FilterGitHub(directOnly=true) included indirect module: %s", m.Path)
		}
	}

	// Filter all
	allGitHub, _ := FilterGitHub(allModules, false)
	if len(directModules) >= len(allGitHub) {
		t.Errorf("direct-only (%d) should be fewer than all (%d)", len(directModules), len(allGitHub))
	}
}

func TestPipeline_WithIgnore(t *testing.T) {
	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "mixed-archived", "github_response.json"))

	var archived []RepoStatus
	for _, r := range results {
		if r.IsArchived {
			archived = append(archived, r)
		}
	}

	// Ignore one of the archived modules
	il := NewIgnoreList()
	il.Add("github.com/pkg/errors")
	filtered, ignored := il.FilterResults(archived)

	if len(filtered) != 1 {
		t.Errorf("expected 1 filtered result, got %d", len(filtered))
	}
	if filtered[0].Module.Path != "github.com/mitchellh/mapstructure" {
		t.Errorf("filtered[0] = %q, want mitchellh/mapstructure", filtered[0].Module.Path)
	}
	if len(ignored) != 1 {
		t.Errorf("expected 1 ignored result, got %d", len(ignored))
	}
	if ignored[0].Module.Path != "github.com/pkg/errors" {
		t.Errorf("ignored[0] = %q, want pkg/errors", ignored[0].Module.Path)
	}
}

func TestPipeline_StaleDetection(t *testing.T) {
	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "stale-deps", "github_response.json"))

	cfg := defaultTestConfig()
	cfg.Now = fixtureNow
	cfg.Stale = StaleConfig{Enabled: true, Years: 2}

	stale := filterStale(cfg, results)

	// With Now=2026-03-21 and threshold=2y:
	// testify pushed 2025-02-15 → not stale
	// old/library pushed 2022-06-15 → stale (3.7y old)
	// ancient/tool pushed 2020-01-10 → stale (6.2y old)
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale, got %d", len(stale))
	}

	paths := make(map[string]bool)
	for _, s := range stale {
		paths[s.Module.Path] = true
	}
	if !paths["github.com/old/library"] {
		t.Error("old/library should be stale")
	}
	if !paths["github.com/ancient/tool"] {
		t.Error("ancient/tool should be stale")
	}
}

func TestPipeline_SortModes(t *testing.T) {
	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "mixed-archived", "github_response.json"))

	var archived []RepoStatus
	for _, r := range results {
		if r.IsArchived {
			archived = append(archived, r)
		}
	}

	// Sort by name (asc, default)
	cfg := defaultTestConfig()
	cfg.Now = fixtureNow
	cfg.SortMode = "name"
	output := captureStdout(t, func() {
		PrintTable(cfg, archived, nil)
	})
	mapIdx := strings.Index(output, "mitchellh/mapstructure")
	pkgIdx := strings.Index(output, "pkg/errors")
	if mapIdx < 0 || pkgIdx < 0 {
		t.Fatal("both archived modules should appear in output")
	}
	if mapIdx > pkgIdx {
		t.Error("sort by name: mitchellh should come before pkg")
	}

	// Sort by name descending
	cfg.SortMode = "name"
	cfg.SortReverse = true
	output = captureStdout(t, func() {
		PrintTable(cfg, archived, nil)
	})
	mapIdx = strings.Index(output, "mitchellh/mapstructure")
	pkgIdx = strings.Index(output, "pkg/errors")
	if mapIdx < pkgIdx {
		t.Error("sort by name:desc: pkg should come before mitchellh")
	}
}

func TestPipeline_DeprecatedAndArchived(t *testing.T) {
	gomodPath := filepath.Join("testdata", "fixtures", "deprecated-and-archived", "go.mod")
	allModules, err := ParseGoMod(gomodPath)
	if err != nil {
		t.Fatalf("ParseGoMod: %v", err)
	}

	githubModules, _ := FilterGitHub(allModules, false)
	if len(githubModules) != 3 {
		t.Fatalf("expected 3 GitHub modules, got %d", len(githubModules))
	}

	results := loadFixtureResults(t, filepath.Join("testdata", "fixtures", "deprecated-and-archived", "github_response.json"))

	var archived []RepoStatus
	var deprecated []Module
	for _, r := range results {
		if r.IsArchived {
			archived = append(archived, r)
		}
		if r.Module.Deprecated != "" {
			deprecated = append(deprecated, r.Module)
		}
	}

	if len(archived) != 1 {
		t.Errorf("expected 1 archived, got %d", len(archived))
	}
	if len(deprecated) != 1 {
		t.Errorf("expected 1 deprecated, got %d", len(deprecated))
	}
	if deprecated[0].Path != "github.com/golang/protobuf" {
		t.Errorf("deprecated module = %q, want golang/protobuf", deprecated[0].Path)
	}

	// JSON output should have both sections
	cfg := defaultTestConfig()
	cfg.Now = fixtureNow
	jsonOutput := captureStdout(t, func() {
		PrintJSON(cfg, archived, nil, nil, nil, deprecated)
	})
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &jsonData); err != nil {
		t.Fatalf("JSON output should be valid JSON: %v", err)
	}
	if _, ok := jsonData["archived"]; !ok {
		t.Error("JSON should have 'archived' key")
	}
	if _, ok := jsonData["deprecated"]; !ok {
		t.Error("JSON should have 'deprecated' key")
	}
}
