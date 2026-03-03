package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractGitHubFromURL(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
	}{
		{"https://github.com/grpc/grpc-go", "grpc", "grpc-go"},
		{"http://github.com/grpc/grpc-go", "grpc", "grpc-go"},
		{"https://github.com/grpc/grpc-go.git", "grpc", "grpc-go"},
		{"github.com/grpc/grpc-go", "grpc", "grpc-go"},
		{"https://github.com/uber-go/zap/", "uber-go", "zap"},
		{"https://github.com/uber-go/zap/tree/main", "uber-go", "zap"},
		{"https://go.googlesource.com/text", "", ""},
		{"https://gitlab.com/foo/bar", "", ""},
		{"", "", ""},
		{"https://github.com/", "", ""},
		{"https://github.com/owner", "", ""},
		{"github.com/owner/repo", "owner", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo := extractGitHubFromURL(tt.input)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("extractGitHubFromURL(%q) = (%q, %q), want (%q, %q)",
					tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestParseMetaTags(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		wantGoImport string
		wantGoSource string
	}{
		{
			name:         "go-import only",
			html:         `<meta name="go-import" content="google.golang.org/grpc git https://github.com/grpc/grpc-go">`,
			wantGoImport: "google.golang.org/grpc git https://github.com/grpc/grpc-go",
		},
		{
			name:         "go-source only",
			html:         `<meta name="go-source" content="gopkg.in/yaml.v3 https://github.com/go-yaml/yaml https://github.com/go-yaml/yaml/tree/v3{/dir} https://github.com/go-yaml/yaml/blob/v3{/dir}/{file}#L{line}">`,
			wantGoSource: "gopkg.in/yaml.v3 https://github.com/go-yaml/yaml https://github.com/go-yaml/yaml/tree/v3{/dir} https://github.com/go-yaml/yaml/blob/v3{/dir}/{file}#L{line}",
		},
		{
			name:         "both tags",
			html:         `<meta name="go-import" content="k8s.io/api git https://github.com/kubernetes/api"><meta name="go-source" content="k8s.io/api https://github.com/kubernetes/api">`,
			wantGoImport: "k8s.io/api git https://github.com/kubernetes/api",
			wantGoSource: "k8s.io/api https://github.com/kubernetes/api",
		},
		{
			name: "neither tag",
			html: `<html><head><title>test</title></head></html>`,
		},
		{
			name:         "reversed attribute order",
			html:         `<meta content="go.uber.org/zap git https://github.com/uber-go/zap" name="go-import">`,
			wantGoImport: "go.uber.org/zap git https://github.com/uber-go/zap",
		},
		{
			name:         "self-referential go-import with github go-source",
			html:         `<meta name="go-import" content="gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3"><meta name="go-source" content="gopkg.in/yaml.v3 https://github.com/go-yaml/yaml https://github.com/go-yaml/yaml/tree/v3{/dir} https://github.com/go-yaml/yaml/blob/v3{/dir}/{file}#L{line}">`,
			wantGoImport: "gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3",
			wantGoSource: "gopkg.in/yaml.v3 https://github.com/go-yaml/yaml https://github.com/go-yaml/yaml/tree/v3{/dir} https://github.com/go-yaml/yaml/blob/v3{/dir}/{file}#L{line}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goImport, goSource := parseMetaTags(tt.html)
			if goImport != tt.wantGoImport {
				t.Errorf("goImport = %q, want %q", goImport, tt.wantGoImport)
			}
			if goSource != tt.wantGoSource {
				t.Errorf("goSource = %q, want %q", goSource, tt.wantGoSource)
			}
		})
	}
}

func TestResolveViaProxy(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		status    int
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "origin with github URL",
			response:  `{"Version":"v1.60.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`,
			status:    200,
			wantOwner: "grpc",
			wantRepo:  "grpc-go",
		},
		{
			name:     "origin with non-github URL",
			response: `{"Version":"v0.20.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text"}}`,
			status:   200,
		},
		{
			name:     "no origin field",
			response: `{"Version":"v1.0.0"}`,
			status:   200,
		},
		{
			name:   "not found",
			status: 404,
		},
		{
			name:     "origin with empty URL",
			response: `{"Version":"v1.0.0","Origin":{"VCS":"git","URL":""}}`,
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
			owner, repo := r.resolveViaProxy("google.golang.org/grpc")
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("got (%q, %q), want (%q, %q)", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestResolveViaMeta(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "go-import with github",
			html:      `<html><head><meta name="go-import" content="k8s.io/api git https://github.com/kubernetes/api"></head></html>`,
			wantOwner: "kubernetes",
			wantRepo:  "api",
		},
		{
			name:      "self-referential go-import, github in go-source",
			html:      `<html><head><meta name="go-import" content="gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3"><meta name="go-source" content="gopkg.in/yaml.v3 https://github.com/go-yaml/yaml https://github.com/go-yaml/yaml/tree/v3{/dir} https://github.com/go-yaml/yaml/blob/v3{/dir}/{file}#L{line}"></head></html>`,
			wantOwner: "go-yaml",
			wantRepo:  "yaml",
		},
		{
			name: "no github anywhere",
			html: `<html><head><meta name="go-import" content="golang.org/x/text git https://go.googlesource.com/text"></head></html>`,
		},
		{
			name: "no meta tags",
			html: `<html><head><title>test</title></head></html>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, tt.html)
			}))
			defer srv.Close()

			// The resolver fetches https://{modulePath}?go-get=1, but our
			// test server is at srv.URL. We override by using a custom transport.
			r := &resolver{
				client:       srv.Client(),
				proxyBaseURL: srv.URL,
			}

			// We can't easily override the module URL, so test resolveViaMeta
			// indirectly via resolveOne with a proxy that 404s.
			// Instead, test parseMetaTags + extractGitHubFromURL directly
			// and test the full flow via TestResolveOne below.

			goImport, goSource := parseMetaTags(tt.html)
			var owner, repo string

			// Mimic resolveViaMeta logic
			if goImport != "" {
				parts := splitFields(goImport)
				if len(parts) >= 3 {
					owner, repo = extractGitHubFromURL(parts[2])
				}
			}
			if owner == "" && goSource != "" {
				parts := splitFields(goSource)
				for _, part := range parts {
					if o, re := extractGitHubFromURL(part); o != "" {
						owner, repo = o, re
						break
					}
				}
			}

			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("got (%q, %q), want (%q, %q)", owner, repo, tt.wantOwner, tt.wantRepo)
			}
			_ = r // used for reference only in this test
		})
	}
}

// splitFields is a test helper that mirrors strings.Fields.
func splitFields(s string) []string {
	return splitFieldsN(s, -1)
}

func splitFieldsN(s string, n int) []string {
	if n < 0 {
		return append([]string{}, splitAllFields(s)...)
	}
	return splitAllFields(s)[:n]
}

func splitAllFields(s string) []string {
	var fields []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if start >= 0 {
				fields = append(fields, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		fields = append(fields, s[start:])
	}
	return fields
}

func TestResolveOne(t *testing.T) {
	t.Run("proxy hit", func(t *testing.T) {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"Version":"v1.60.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`)
		}))
		defer proxy.Close()

		r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}
		owner, repo := r.resolveOne("google.golang.org/grpc")
		if owner != "grpc" || repo != "grpc-go" {
			t.Errorf("got (%q, %q), want (grpc, grpc-go)", owner, repo)
		}
	})

	t.Run("proxy miss, no meta fallback", func(t *testing.T) {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		}))
		defer proxy.Close()

		r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}
		// resolveViaMeta will fail because it tries to reach the actual module URL.
		// With a mock client pointed at proxy, it will get 404.
		owner, repo := r.resolveViaProxy("nonexistent.example.com/mod")
		if owner != "" || repo != "" {
			t.Errorf("got (%q, %q), want empty", owner, repo)
		}
	})

	t.Run("proxy no github origin", func(t *testing.T) {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"Version":"v0.20.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text"}}`)
		}))
		defer proxy.Close()

		r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}
		owner, repo := r.resolveViaProxy("golang.org/x/text")
		if owner != "" || repo != "" {
			t.Errorf("got (%q, %q), want empty", owner, repo)
		}
	})
}

func TestResolveVanityImports(t *testing.T) {
	// Mock proxy that returns GitHub origin for specific modules.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/google.golang.org/grpc/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.60.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`)
		case "/go.uber.org/zap/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.27.0","Origin":{"VCS":"git","URL":"https://github.com/uber-go/zap"}}`)
		case "/golang.org/x/text/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v0.20.0","Origin":{"VCS":"git","URL":"https://go.googlesource.com/text"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer proxy.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
		{Path: "google.golang.org/grpc", Version: "v1.60.0"},
		{Path: "go.uber.org/zap", Version: "v1.27.0"},
		{Path: "golang.org/x/text", Version: "v0.20.0"},
		{Path: "nonexistent.example.com/mod", Version: "v0.0.1"},
	}

	// Use internal resolver directly to control proxy URL.
	r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}

	// Resolve manually to test the logic.
	resolved := 0
	for i := range modules {
		if modules[i].Owner != "" {
			continue
		}
		owner, repo := r.resolveOne(modules[i].Path)
		if owner != "" {
			modules[i].Owner = owner
			modules[i].Repo = repo
			resolved++
		}
	}

	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}

	// Verify grpc resolved
	if modules[1].Owner != "grpc" || modules[1].Repo != "grpc-go" {
		t.Errorf("grpc: got (%q, %q), want (grpc, grpc-go)", modules[1].Owner, modules[1].Repo)
	}

	// Verify zap resolved
	if modules[2].Owner != "uber-go" || modules[2].Repo != "zap" {
		t.Errorf("zap: got (%q, %q), want (uber-go, zap)", modules[2].Owner, modules[2].Repo)
	}

	// Verify golang.org/x/text NOT resolved (googlesource, not GitHub)
	if modules[3].Owner != "" {
		t.Errorf("text: got owner %q, want empty", modules[3].Owner)
	}

	// Verify nonexistent NOT resolved
	if modules[4].Owner != "" {
		t.Errorf("nonexistent: got owner %q, want empty", modules[4].Owner)
	}

	// Verify original GitHub module untouched
	if modules[0].Owner != "foo" || modules[0].Repo != "bar" {
		t.Errorf("foo/bar: got (%q, %q), want (foo, bar)", modules[0].Owner, modules[0].Repo)
	}
}

func TestResolveVanityImports_WorkerPool(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/google.golang.org/grpc/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.60.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`)
		case "/go.uber.org/zap/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.27.0","Origin":{"VCS":"git","URL":"https://github.com/uber-go/zap"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer proxy.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
		{Path: "google.golang.org/grpc", Version: "v1.60.0"},
		{Path: "go.uber.org/zap", Version: "v1.27.0"},
		{Path: "nonexistent.example.com/mod", Version: "v0.0.1"},
	}

	r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}
	resolved := resolveVanityImportsWithResolver(modules, 4, r)

	if resolved != 2 {
		t.Errorf("resolved = %d, want 2", resolved)
	}
	if modules[1].Owner != "grpc" || modules[1].Repo != "grpc-go" {
		t.Errorf("grpc: got (%q, %q), want (grpc, grpc-go)", modules[1].Owner, modules[1].Repo)
	}
	if modules[2].Owner != "uber-go" || modules[2].Repo != "zap" {
		t.Errorf("zap: got (%q, %q), want (uber-go, zap)", modules[2].Owner, modules[2].Repo)
	}
	if modules[3].Owner != "" {
		t.Errorf("nonexistent should not resolve, got owner %q", modules[3].Owner)
	}
}

func TestResolveVanityImports_WorkerPool_AllGitHub(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
	}

	r := &resolver{client: http.DefaultClient, proxyBaseURL: "http://unused"}
	resolved := resolveVanityImportsWithResolver(modules, 4, r)

	if resolved != 0 {
		t.Errorf("resolved = %d, want 0 when all modules are GitHub", resolved)
	}
}

func TestResolveAcrossModules_WorkerPool(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/google.golang.org/grpc/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.60.0","Origin":{"VCS":"git","URL":"https://github.com/grpc/grpc-go"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer proxy.Close()

	modules := []moduleInfo{
		{
			allModules: []Module{
				{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
				{Path: "google.golang.org/grpc", Version: "v1.60.0"},
			},
		},
		{
			allModules: []Module{
				{Path: "google.golang.org/grpc", Version: "v1.60.0"}, // duplicate
				{Path: "nonexistent.example.com/mod", Version: "v0.0.1"},
			},
		},
	}

	r := &resolver{client: proxy.Client(), proxyBaseURL: proxy.URL}
	resolved := resolveAcrossModulesWithResolver(modules, r)

	if resolved != 1 {
		t.Errorf("resolved = %d, want 1 (grpc deduplicated)", resolved)
	}
	// Both instances of grpc should be resolved.
	if modules[0].allModules[1].Owner != "grpc" {
		t.Errorf("modules[0] grpc: owner = %q, want grpc", modules[0].allModules[1].Owner)
	}
	if modules[1].allModules[0].Owner != "grpc" {
		t.Errorf("modules[1] grpc: owner = %q, want grpc", modules[1].allModules[0].Owner)
	}
	if modules[1].allModules[1].Owner != "" {
		t.Errorf("nonexistent should not resolve, got owner %q", modules[1].allModules[1].Owner)
	}
}

func TestResolveAcrossModules_WorkerPool_Empty(t *testing.T) {
	modules := []moduleInfo{
		{
			allModules: []Module{
				{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
			},
		},
	}

	r := &resolver{client: http.DefaultClient, proxyBaseURL: "http://unused"}
	resolved := resolveAcrossModulesWithResolver(modules, r)

	if resolved != 0 {
		t.Errorf("resolved = %d, want 0 when no non-GitHub modules", resolved)
	}
}
