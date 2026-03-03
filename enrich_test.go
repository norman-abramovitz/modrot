package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchLatestInfo(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		status        int
		wantVersion   string
		wantSourceURL string
	}{
		{
			name:          "full response with origin",
			response:      `{"Version":"v0.22.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/mod"}}`,
			status:        200,
			wantVersion:   "v0.22.0",
			wantSourceURL: "https://go.googlesource.com/mod",
		},
		{
			name:          "github origin",
			response:      `{"Version":"v1.70.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`,
			status:        200,
			wantVersion:   "v1.70.0",
			wantSourceURL: "https://github.com/grpc/grpc-go",
		},
		{
			name:        "no origin",
			response:    `{"Version":"v1.0.0"}`,
			status:      200,
			wantVersion: "v1.0.0",
		},
		{
			name:   "not found",
			status: 404,
		},
		{
			name:        "origin with empty URL",
			response:    `{"Version":"v1.0.0","Origin":{"VCS":"git","URL":""}}`,
			status:      200,
			wantVersion: "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if tt.response != "" {
					_, _ = fmt.Fprint(w, tt.response)
				}
			}))
			defer srv.Close()

			r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
			version, sourceURL := r.fetchLatestInfo("golang.org/x/mod")
			if version != tt.wantVersion {
				t.Errorf("version = %q, want %q", version, tt.wantVersion)
			}
			if sourceURL != tt.wantSourceURL {
				t.Errorf("sourceURL = %q, want %q", sourceURL, tt.wantSourceURL)
			}
		})
	}
}

func TestFetchLatestInfo_CorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = fmt.Fprint(w, `{"Version":"v0.22.0"}`)
	}))
	defer srv.Close()

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	r.fetchLatestInfo("golang.org/x/mod")

	wantPath := "/golang.org/x/mod/@latest"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
}

func TestFetchVersionInfo(t *testing.T) {
	tests := []struct {
		name     string
		response string
		status   int
		wantTime time.Time
	}{
		{
			name:     "valid response",
			response: `{"Version":"v0.17.0","Time":"2024-03-15T10:30:00Z"}`,
			status:   200,
			wantTime: time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:   "not found",
			status: 404,
		},
		{
			name:     "invalid JSON",
			response: `{invalid`,
			status:   200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if tt.response != "" {
					_, _ = fmt.Fprint(w, tt.response)
				}
			}))
			defer srv.Close()

			r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
			got := r.fetchVersionInfo("golang.org/x/mod", "v0.17.0")
			if !got.Equal(tt.wantTime) {
				t.Errorf("time = %v, want %v", got, tt.wantTime)
			}
		})
	}
}

func TestFetchVersionInfo_CorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = fmt.Fprint(w, `{"Version":"v0.17.0","Time":"2024-03-15T10:30:00Z"}`)
	}))
	defer srv.Close()

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	r.fetchVersionInfo("golang.org/x/mod", "v0.17.0")

	wantPath := "/golang.org/x/mod/@v/v0.17.0.info"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
}

func TestEnrichNonGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/golang.org/x/mod/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.22.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/mod"}}`)
		case "/golang.org/x/mod/@v/v0.17.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v0.17.0","Time":"2024-03-15T10:30:00Z"}`)
		case "/golang.org/x/text/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.21.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text"}}`)
		case "/golang.org/x/text/@v/v0.14.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v0.14.0","Time":"2023-10-11T17:42:28Z"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
		{Path: "golang.org/x/mod", Version: "v0.17.0"},
		{Path: "golang.org/x/text", Version: "v0.14.0"},
	}

	// Use internal resolver to control proxy URL.
	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}

	// Manually enrich non-GitHub modules (simulating EnrichNonGitHub logic).
	for i := range modules {
		if modules[i].Owner != "" {
			continue
		}
		modules[i].LatestVersion, modules[i].SourceURL = r.fetchLatestInfo(modules[i].Path)
		modules[i].VersionTime = r.fetchVersionInfo(modules[i].Path, modules[i].Version)
	}

	// GitHub module should be untouched
	if modules[0].LatestVersion != "" || modules[0].SourceURL != "" {
		t.Errorf("GitHub module should not be enriched")
	}

	// golang.org/x/mod
	if modules[1].LatestVersion != "v0.22.0" {
		t.Errorf("mod latest = %q, want v0.22.0", modules[1].LatestVersion)
	}
	if modules[1].SourceURL != "https://go.googlesource.com/mod" {
		t.Errorf("mod source = %q", modules[1].SourceURL)
	}
	wantTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	if !modules[1].VersionTime.Equal(wantTime) {
		t.Errorf("mod time = %v, want %v", modules[1].VersionTime, wantTime)
	}

	// golang.org/x/text
	if modules[2].LatestVersion != "v0.21.0" {
		t.Errorf("text latest = %q, want v0.21.0", modules[2].LatestVersion)
	}
	if modules[2].SourceURL != "https://go.googlesource.com/text" {
		t.Errorf("text source = %q", modules[2].SourceURL)
	}
}

func TestEnrichNonGitHub_ProxyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "golang.org/x/mod", Version: "v0.17.0"},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}

	// Should not panic, should leave fields empty
	modules[0].LatestVersion, modules[0].SourceURL = r.fetchLatestInfo(modules[0].Path)
	modules[0].VersionTime = r.fetchVersionInfo(modules[0].Path, modules[0].Version)

	if modules[0].LatestVersion != "" {
		t.Errorf("expected empty latest on error, got %q", modules[0].LatestVersion)
	}
	if modules[0].SourceURL != "" {
		t.Errorf("expected empty source on error, got %q", modules[0].SourceURL)
	}
	if !modules[0].VersionTime.IsZero() {
		t.Errorf("expected zero time on error, got %v", modules[0].VersionTime)
	}
}

func TestEnrichNonGitHub_WorkerPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/golang.org/x/mod/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.22.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/mod"}}`)
		case "/golang.org/x/mod/@v/v0.17.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v0.17.0","Time":"2024-03-15T10:30:00Z"}`)
		case "/golang.org/x/text/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.21.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text"}}`)
		case "/golang.org/x/text/@v/v0.14.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v0.14.0","Time":"2023-10-11T17:42:28Z"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
		{Path: "golang.org/x/mod", Version: "v0.17.0"},
		{Path: "golang.org/x/text", Version: "v0.14.0"},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	enrichNonGitHubWithResolver(modules, 4, r)

	// GitHub module should be untouched.
	if modules[0].LatestVersion != "" || modules[0].SourceURL != "" {
		t.Errorf("GitHub module should not be enriched")
	}
	// golang.org/x/mod
	if modules[1].LatestVersion != "v0.22.0" {
		t.Errorf("mod latest = %q, want v0.22.0", modules[1].LatestVersion)
	}
	if modules[1].SourceURL != "https://go.googlesource.com/mod" {
		t.Errorf("mod source = %q", modules[1].SourceURL)
	}
	wantTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	if !modules[1].VersionTime.Equal(wantTime) {
		t.Errorf("mod time = %v, want %v", modules[1].VersionTime, wantTime)
	}
	// golang.org/x/text
	if modules[2].LatestVersion != "v0.21.0" {
		t.Errorf("text latest = %q, want v0.21.0", modules[2].LatestVersion)
	}
}

func TestEnrichNonGitHub_WorkerPool_AllGitHub(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
	}

	r := &resolver{client: http.DefaultClient, proxyBaseURL: "http://unused"}
	enrichNonGitHubWithResolver(modules, 4, r)

	if modules[0].LatestVersion != "" {
		t.Errorf("GitHub module should not be enriched, got %q", modules[0].LatestVersion)
	}
}

func TestEnrichAcrossModules_WorkerPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/golang.org/x/mod/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.22.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/mod"}}`)
		case "/golang.org/x/mod/@v/v0.17.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v0.17.0","Time":"2024-03-15T10:30:00Z"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []moduleInfo{
		{
			nonGHModules: []Module{
				{Path: "golang.org/x/mod", Version: "v0.17.0"},
			},
		},
		{
			nonGHModules: []Module{
				{Path: "golang.org/x/mod", Version: "v0.17.0"}, // duplicate
			},
		},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	enrichAcrossModulesWithResolver(modules, r)

	// Both instances should be enriched.
	if modules[0].nonGHModules[0].LatestVersion != "v0.22.0" {
		t.Errorf("modules[0] mod latest = %q, want v0.22.0", modules[0].nonGHModules[0].LatestVersion)
	}
	if modules[1].nonGHModules[0].LatestVersion != "v0.22.0" {
		t.Errorf("modules[1] mod latest = %q, want v0.22.0", modules[1].nonGHModules[0].LatestVersion)
	}
	wantTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	if !modules[0].nonGHModules[0].VersionTime.Equal(wantTime) {
		t.Errorf("modules[0] mod time = %v, want %v", modules[0].nonGHModules[0].VersionTime, wantTime)
	}
}

func TestEnrichAcrossModules_WorkerPool_Empty(t *testing.T) {
	modules := []moduleInfo{
		{
			nonGHModules: []Module{},
		},
	}

	r := &resolver{client: http.DefaultClient, proxyBaseURL: "http://unused"}
	enrichAcrossModulesWithResolver(modules, r)
	// Should not panic or modify anything.
}

func TestHostDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"golang.org/x/text", "golang.org"},
		{"google.golang.org/grpc", "google.golang.org"},
		{"gopkg.in/yaml.v3", "gopkg.in"},
		{"cel.dev/expr", "cel.dev"},
		{"github.com/foo/bar", "github.com"},
		{"k8s.io/api", "k8s.io"},
		{"singleword", "singleword"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hostDomain(tt.input)
			if got != tt.want {
				t.Errorf("hostDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
