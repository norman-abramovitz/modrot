package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildGraphQLQuery(t *testing.T) {
	modules := []Module{
		{Owner: "foo", Repo: "bar"},
		{Owner: "baz", Repo: "qux"},
	}

	query := buildGraphQLQuery(modules)

	if !strings.Contains(query, `r0: repository(owner: "foo", name: "bar")`) {
		t.Error("query missing r0 alias")
	}
	if !strings.Contains(query, `r1: repository(owner: "baz", name: "qux")`) {
		t.Error("query missing r1 alias")
	}
	if !strings.Contains(query, "isArchived") {
		t.Error("query missing isArchived field")
	}
	if !strings.Contains(query, "archivedAt") {
		t.Error("query missing archivedAt field")
	}
	if !strings.Contains(query, "pushedAt") {
		t.Error("query missing pushedAt field")
	}
}

func TestBuildGraphQLQuery_Empty(t *testing.T) {
	query := buildGraphQLQuery(nil)
	if query != "{\n}\n" {
		t.Errorf("expected empty query block, got %q", query)
	}
}

func TestBuildGraphQLQuery_SpecialCharacters(t *testing.T) {
	modules := []Module{
		{Owner: "Azure", Repo: "go-autorest"},
	}
	query := buildGraphQLQuery(modules)
	if !strings.Contains(query, `owner: "Azure"`) {
		t.Error("query should properly quote owner with capital letter")
	}
}

func TestParseGraphQLResponse_Archived(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"},
	}

	resp := gqlResponse{
		Data: map[string]*repoData{
			"r0": {
				IsArchived: true,
				ArchivedAt: "2024-07-22T20:44:18Z",
				PushedAt:   "2021-05-05T17:08:29Z",
			},
		},
	}

	results := parseGraphQLResponse(resp, modules)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if !r.IsArchived {
		t.Error("expected IsArchived=true")
	}
	if r.NotFound {
		t.Error("expected NotFound=false")
	}
	if r.ArchivedAt.IsZero() {
		t.Error("expected ArchivedAt to be set")
	}
	expectedArchived := time.Date(2024, 7, 22, 20, 44, 18, 0, time.UTC)
	if !r.ArchivedAt.Equal(expectedArchived) {
		t.Errorf("ArchivedAt = %v, want %v", r.ArchivedAt, expectedArchived)
	}
	expectedPushed := time.Date(2021, 5, 5, 17, 8, 29, 0, time.UTC)
	if !r.PushedAt.Equal(expectedPushed) {
		t.Errorf("PushedAt = %v, want %v", r.PushedAt, expectedPushed)
	}
}

func TestParseGraphQLResponse_NotArchived(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"},
	}

	resp := gqlResponse{
		Data: map[string]*repoData{
			"r0": {
				IsArchived: false,
				PushedAt:   "2025-01-15T10:00:00Z",
			},
		},
	}

	results := parseGraphQLResponse(resp, modules)
	r := results[0]
	if r.IsArchived {
		t.Error("expected IsArchived=false")
	}
	if r.ArchivedAt.IsZero() == false {
		t.Error("expected ArchivedAt to be zero for non-archived repo")
	}
}

func TestParseGraphQLResponse_NotFound(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"},
		{Path: "github.com/baz/qux", Owner: "baz", Repo: "qux"},
	}

	resp := gqlResponse{
		Data: map[string]*repoData{
			"r0": nil, // null in JSON
			"r1": {IsArchived: false, PushedAt: "2025-01-01T00:00:00Z"},
		},
		Errors: []struct {
			Message string   `json:"message"`
			Path    []string `json:"path"`
		}{
			{Message: "Could not resolve to a Repository", Path: []string{"r0"}},
		},
	}

	results := parseGraphQLResponse(resp, modules)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].NotFound {
		t.Error("results[0] should be NotFound")
	}
	if results[0].Error != "Could not resolve to a Repository" {
		t.Errorf("results[0].Error = %q, want error message", results[0].Error)
	}

	if results[1].NotFound {
		t.Error("results[1] should not be NotFound")
	}
}

func TestParseGraphQLResponse_MissingFromData(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"},
	}

	// Data map is empty — no alias present at all
	resp := gqlResponse{
		Data: map[string]*repoData{},
	}

	results := parseGraphQLResponse(resp, modules)
	if !results[0].NotFound {
		t.Error("expected NotFound when alias missing from data")
	}
	if results[0].Error != "repository not found" {
		t.Errorf("expected default error message, got %q", results[0].Error)
	}
}

func TestParseGraphQLResponse_MultipleBatch(t *testing.T) {
	modules := make([]Module, 3)
	for i := range modules {
		modules[i] = Module{
			Path:  "github.com/test/repo" + string(rune('a'+i)),
			Owner: "test",
			Repo:  "repo" + string(rune('a'+i)),
		}
	}

	resp := gqlResponse{
		Data: map[string]*repoData{
			"r0": {IsArchived: true, ArchivedAt: "2024-01-01T00:00:00Z", PushedAt: "2023-12-01T00:00:00Z"},
			"r1": {IsArchived: false, PushedAt: "2025-02-01T00:00:00Z"},
			"r2": {IsArchived: true, ArchivedAt: "2023-06-15T00:00:00Z", PushedAt: "2023-06-01T00:00:00Z"},
		},
	}

	results := parseGraphQLResponse(resp, modules)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if !results[0].IsArchived {
		t.Error("results[0] should be archived")
	}
	if results[1].IsArchived {
		t.Error("results[1] should not be archived")
	}
	if !results[2].IsArchived {
		t.Error("results[2] should be archived")
	}
}

func TestParseGraphQLResponse_PreservesModuleInfo(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.2.3", Direct: true, Owner: "foo", Repo: "bar"},
	}

	resp := gqlResponse{
		Data: map[string]*repoData{
			"r0": {IsArchived: false, PushedAt: "2025-01-01T00:00:00Z"},
		},
	}

	results := parseGraphQLResponse(resp, modules)
	r := results[0]
	if r.Module.Path != "github.com/foo/bar" {
		t.Errorf("Module.Path = %q", r.Module.Path)
	}
	if r.Module.Version != "v1.2.3" {
		t.Errorf("Module.Version = %q", r.Module.Version)
	}
	if !r.Module.Direct {
		t.Error("Module.Direct should be true")
	}
}

func TestGQLResponseUnmarshal(t *testing.T) {
	raw := `{
		"data": {
			"r0": {"isArchived": true, "archivedAt": "2024-07-22T20:44:18Z", "pushedAt": "2021-05-05T17:08:29Z"},
			"r1": null
		},
		"errors": [
			{"message": "Could not resolve to a Repository", "path": ["r1"]}
		]
	}`

	var resp gqlResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if resp.Data["r0"] == nil {
		t.Fatal("r0 should not be nil")
	}
	if !resp.Data["r0"].IsArchived {
		t.Error("r0 should be archived")
	}
	if resp.Data["r1"] != nil {
		t.Error("r1 should be nil")
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Path[0] != "r1" {
		t.Errorf("error path = %v", resp.Errors[0].Path)
	}
}

// --- httptest-based tests for queryBatch and checkReposWithClient ---

func TestQueryBatch_Archived(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %q", r.Header.Get("Content-Type"))
		}
		_, _ = fmt.Fprint(w, `{
			"data": {
				"r0": {"isArchived": true, "archivedAt": "2024-07-22T20:44:18Z", "pushedAt": "2021-05-05T17:08:29Z"}
			}
		}`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := []Module{{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"}}

	results, err := gc.queryBatch("test-token", modules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].IsArchived {
		t.Error("expected IsArchived=true")
	}
	expected := time.Date(2024, 7, 22, 20, 44, 18, 0, time.UTC)
	if !results[0].ArchivedAt.Equal(expected) {
		t.Errorf("ArchivedAt = %v, want %v", results[0].ArchivedAt, expected)
	}
}

func TestQueryBatch_NonArchived(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{
			"data": {
				"r0": {"isArchived": false, "pushedAt": "2025-03-01T10:00:00Z"}
			}
		}`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := []Module{{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"}}

	results, err := gc.queryBatch("test-token", modules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].IsArchived {
		t.Error("expected IsArchived=false")
	}
}

func TestQueryBatch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{
			"data": {"r0": null},
			"errors": [{"message": "Could not resolve to a Repository", "path": ["r0"]}]
		}`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := []Module{{Path: "github.com/gone/repo", Owner: "gone", Repo: "repo"}}

	results, err := gc.queryBatch("test-token", modules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].NotFound {
		t.Error("expected NotFound=true")
	}
	if results[0].Error != "Could not resolve to a Repository" {
		t.Errorf("Error = %q", results[0].Error)
	}
}

func TestQueryBatch_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"message": "internal error"}`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := []Module{{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"}}

	_, err := gc.queryBatch("test-token", modules)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code 500, got: %v", err)
	}
}

func TestQueryBatch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{not valid json`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := []Module{{Path: "github.com/foo/bar", Owner: "foo", Repo: "bar"}}

	_, err := gc.queryBatch("test-token", modules)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parsing response") {
		t.Errorf("error should mention parsing, got: %v", err)
	}
}

func TestCheckReposWithClient_Batching(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		// Return empty data — modules will be NotFound, but that's fine for batching test
		_, _ = fmt.Fprint(w, `{"data": {}}`)
	}))
	defer srv.Close()

	gc := &ghClient{client: srv.Client(), graphqlURL: srv.URL}
	modules := make([]Module, 5)
	for i := range modules {
		modules[i] = Module{
			Path:  fmt.Sprintf("github.com/test/repo%d", i),
			Owner: "test",
			Repo:  fmt.Sprintf("repo%d", i),
		}
	}

	results, err := checkReposWithClient(modules, 2, "test-token", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
	if got := requestCount.Load(); got != 3 {
		t.Errorf("expected 3 batch requests (2+2+1), got %d", got)
	}
}

func TestCheckReposWithClient_Empty(t *testing.T) {
	gc := &ghClient{client: http.DefaultClient, graphqlURL: "http://unused"}
	results, err := checkReposWithClient(nil, 50, "test-token", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty input, got %v", results)
	}
}
