package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseDeprecation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "inline deprecation",
			body: `module github.com/golang/protobuf // Deprecated: Use the "google.golang.org/protobuf" module instead.

go 1.17
`,
			want: `Use the "google.golang.org/protobuf" module instead.`,
		},
		{
			name: "comment above module directive",
			body: `// Deprecated: Use google.golang.org/protobuf instead.
module github.com/golang/protobuf

go 1.17
`,
			want: "Use google.golang.org/protobuf instead.",
		},
		{
			name: "no deprecation",
			body: `module github.com/foo/bar

go 1.21

require (
	github.com/baz/qux v1.0.0
)
`,
			want: "",
		},
		{
			name: "comment not immediately before module",
			body: `// Deprecated: old message

// some other comment
module github.com/foo/bar

go 1.21
`,
			want: "",
		},
		{
			name: "case sensitive - lowercase deprecated",
			body: `// deprecated: this should not match
module github.com/foo/bar
`,
			want: "",
		},
		{
			name: "deprecation with no message",
			body: `// Deprecated:
module github.com/foo/bar
`,
			want: "",
		},
		{
			name: "deprecation with leading whitespace in message",
			body: `// Deprecated:   Use something else.
module github.com/foo/bar
`,
			want: "Use something else.",
		},
		{
			name: "module directive with version suffix",
			body: `// Deprecated: Use v2 instead.
module github.com/foo/bar/v2

go 1.21
`,
			want: "Use v2 instead.",
		},
		{
			name: "inline deprecation with extra spaces",
			body: `module github.com/foo/bar   // Deprecated: Replaced by github.com/new/bar.

go 1.21
`,
			want: "Replaced by github.com/new/bar.",
		},
		{
			name: "empty go.mod",
			body: "",
			want: "",
		},
		{
			name: "only comments no module",
			body: `// Deprecated: something
// another comment
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDeprecation(tt.body)
			if got != tt.want {
				t.Errorf("parseDeprecation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchGoModDeprecation(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{
			name: "deprecated module",
			body: `// Deprecated: Use google.golang.org/protobuf instead.
module github.com/golang/protobuf

go 1.17
`,
			status: 200,
			want:   "Use google.golang.org/protobuf instead.",
		},
		{
			name: "not deprecated",
			body: `module github.com/foo/bar

go 1.21
`,
			status: 200,
			want:   "",
		},
		{
			name:   "proxy returns 404",
			status: 404,
			want:   "",
		},
		{
			name:   "proxy returns 410 (gone)",
			status: 410,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if tt.body != "" {
					_, _ = fmt.Fprint(w, tt.body)
				}
			}))
			defer srv.Close()

			r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
			got := r.fetchGoModDeprecation("github.com/golang/protobuf", "v1.5.4")
			if got != tt.want {
				t.Errorf("fetchGoModDeprecation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchGoModDeprecation_CorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = fmt.Fprint(w, "module github.com/foo/bar\n")
	}))
	defer srv.Close()

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	r.fetchGoModDeprecation("github.com/foo/bar", "v1.2.3")

	wantPath := "/github.com/foo/bar/@v/v1.2.3.mod"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
}

func TestCheckDeprecations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/golang/protobuf/@v/v1.5.4.mod":
			_, _ = fmt.Fprint(w,"// Deprecated: Use google.golang.org/protobuf instead.\nmodule github.com/golang/protobuf\n\ngo 1.17\n")
		case "/github.com/foo/bar/@v/v1.0.0.mod":
			_, _ = fmt.Fprint(w,"module github.com/foo/bar\n\ngo 1.21\n")
		case "/github.com/old/thing/@v/v0.5.0.mod":
			_, _ = fmt.Fprint(w,"module github.com/old/thing // Deprecated: Use github.com/new/thing.\n\ngo 1.20\n")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/golang/protobuf", Version: "v1.5.4"},
		{Path: "github.com/foo/bar", Version: "v1.0.0"},
		{Path: "github.com/old/thing", Version: "v0.5.0"},
		{Path: "github.com/missing/mod", Version: "v0.0.1"},
	}

	// Use internal resolver to control proxy URL.
	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}

	// Manually check each module (simulating CheckDeprecations logic).
	count := 0
	for i := range modules {
		msg := r.fetchGoModDeprecation(modules[i].Path, modules[i].Version)
		if msg != "" {
			modules[i].Deprecated = msg
			count++
		}
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if modules[0].Deprecated != "Use google.golang.org/protobuf instead." {
		t.Errorf("protobuf deprecated = %q", modules[0].Deprecated)
	}
	if modules[1].Deprecated != "" {
		t.Errorf("foo/bar should not be deprecated, got %q", modules[1].Deprecated)
	}
	if modules[2].Deprecated != "Use github.com/new/thing." {
		t.Errorf("old/thing deprecated = %q", modules[2].Deprecated)
	}
	if modules[3].Deprecated != "" {
		t.Errorf("missing/mod should not be deprecated, got %q", modules[3].Deprecated)
	}
}

func TestCheckDeprecations_WorkerPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/golang/protobuf/@v/v1.5.4.mod":
			_, _ = fmt.Fprint(w,"// Deprecated: Use google.golang.org/protobuf instead.\nmodule github.com/golang/protobuf\n\ngo 1.17\n")
		case "/github.com/foo/bar/@v/v1.0.0.mod":
			_, _ = fmt.Fprint(w,"module github.com/foo/bar\n\ngo 1.21\n")
		case "/github.com/old/thing/@v/v0.5.0.mod":
			_, _ = fmt.Fprint(w,"module github.com/old/thing // Deprecated: Use github.com/new/thing.\n\ngo 1.20\n")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/golang/protobuf", Version: "v1.5.4"},
		{Path: "github.com/foo/bar", Version: "v1.0.0"},
		{Path: "github.com/old/thing", Version: "v0.5.0"},
		{Path: "github.com/missing/mod", Version: "v0.0.1"},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	count := checkDeprecationsWithResolver(modules, 4, r)

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if modules[0].Deprecated != "Use google.golang.org/protobuf instead." {
		t.Errorf("protobuf deprecated = %q", modules[0].Deprecated)
	}
	if modules[1].Deprecated != "" {
		t.Errorf("foo/bar should not be deprecated, got %q", modules[1].Deprecated)
	}
	if modules[2].Deprecated != "Use github.com/new/thing." {
		t.Errorf("old/thing deprecated = %q", modules[2].Deprecated)
	}
	if modules[3].Deprecated != "" {
		t.Errorf("missing/mod should not be deprecated, got %q", modules[3].Deprecated)
	}
}

func TestCheckDeprecationsAcrossModules_WorkerPool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/golang/protobuf/@v/v1.5.4.mod":
			_, _ = fmt.Fprint(w,"// Deprecated: Use google.golang.org/protobuf instead.\nmodule github.com/golang/protobuf\n\ngo 1.17\n")
		case "/github.com/foo/bar/@v/v1.0.0.mod":
			_, _ = fmt.Fprint(w,"module github.com/foo/bar\n\ngo 1.21\n")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []moduleInfo{
		{
			allModules: []Module{
				{Path: "github.com/golang/protobuf", Version: "v1.5.4"},
				{Path: "github.com/foo/bar", Version: "v1.0.0"},
			},
		},
		{
			allModules: []Module{
				{Path: "github.com/golang/protobuf", Version: "v1.5.4"}, // duplicate
			},
		},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	count := checkDeprecationsAcrossModulesWithResolver(modules, r)

	if count != 1 {
		t.Errorf("count = %d, want 1 (protobuf deduplicated)", count)
	}
	// Both instances should be marked deprecated.
	if modules[0].allModules[0].Deprecated != "Use google.golang.org/protobuf instead." {
		t.Errorf("modules[0] protobuf deprecated = %q", modules[0].allModules[0].Deprecated)
	}
	if modules[1].allModules[0].Deprecated != "Use google.golang.org/protobuf instead." {
		t.Errorf("modules[1] protobuf deprecated = %q", modules[1].allModules[0].Deprecated)
	}
	if modules[0].allModules[1].Deprecated != "" {
		t.Errorf("foo/bar should not be deprecated, got %q", modules[0].allModules[1].Deprecated)
	}
}

func TestCheckDeprecationsAcrossModules_WorkerPool_Empty(t *testing.T) {
	modules := []moduleInfo{}

	r := &resolver{client: http.DefaultClient, proxyBaseURL: "http://unused"}
	count := checkDeprecationsAcrossModulesWithResolver(modules, r)

	if count != 0 {
		t.Errorf("count = %d, want 0 for empty modules", count)
	}
}
