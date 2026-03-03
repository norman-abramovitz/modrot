package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGitHub(t *testing.T) {
	tests := []struct {
		path      string
		wantOwner string
		wantRepo  string
	}{
		{"github.com/foo/bar", "foo", "bar"},
		{"github.com/foo/bar/v2", "foo", "bar"},
		{"github.com/foo/bar/sdk/v2", "foo", "bar"},
		{"github.com/mitchellh/mapstructure", "mitchellh", "mapstructure"},
		{"github.com/Azure/go-autorest/autorest", "Azure", "go-autorest"},
		{"golang.org/x/mod", "", ""},
		{"google.golang.org/grpc", "", ""},
		{"gopkg.in/yaml.v3", "", ""},
		{"cel.dev/expr", "", ""},
		{"github.com/foo", "", ""},        // too few parts
		{"github.com/", "", ""},           // trailing slash only
		{"notgithub.com/foo/bar", "", ""}, // wrong host
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			owner, repo := extractGitHub(tt.path)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("extractGitHub(%q) = (%q, %q), want (%q, %q)",
					tt.path, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestFilterGitHub(t *testing.T) {
	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Direct: true, Owner: "foo", Repo: "bar"},
		{Path: "github.com/foo/bar/v2", Version: "v2.0.0", Direct: false, Owner: "foo", Repo: "bar"},
		{Path: "github.com/baz/qux", Version: "v0.1.0", Direct: false, Owner: "baz", Repo: "qux"},
		{Path: "golang.org/x/mod", Version: "v0.17.0", Direct: true},
		{Path: "google.golang.org/grpc", Version: "v1.60.0", Direct: true},
		{Path: "github.com/abc/def", Version: "v1.0.0", Direct: true, Owner: "abc", Repo: "def"},
	}

	t.Run("all modules", func(t *testing.T) {
		gh, nonGH := FilterGitHub(modules, false)
		// foo/bar should be deduplicated (v1 and v2 share same owner/repo)
		if len(gh) != 3 {
			t.Errorf("expected 3 GitHub modules, got %d", len(gh))
		}
		if len(nonGH) != 2 {
			t.Errorf("expected 2 non-GitHub modules, got %d", len(nonGH))
		}
		// Verify the skipped modules are the expected ones
		if nonGH[0].Path != "golang.org/x/mod" {
			t.Errorf("nonGH[0].Path = %q, want %q", nonGH[0].Path, "golang.org/x/mod")
		}
		if nonGH[1].Path != "google.golang.org/grpc" {
			t.Errorf("nonGH[1].Path = %q, want %q", nonGH[1].Path, "google.golang.org/grpc")
		}
	})

	t.Run("direct only", func(t *testing.T) {
		gh, nonGH := FilterGitHub(modules, true)
		// direct: foo/bar (v1), abc/def — baz/qux is indirect
		if len(gh) != 2 {
			t.Errorf("expected 2 direct GitHub modules, got %d", len(gh))
		}
		// golang.org/x/mod and google.golang.org/grpc are direct non-GH
		if len(nonGH) != 2 {
			t.Errorf("expected 2 non-GitHub modules, got %d", len(nonGH))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		gh, nonGH := FilterGitHub(nil, false)
		if len(gh) != 0 || len(nonGH) != 0 {
			t.Errorf("expected empty results, got %d GitHub, %d non-GitHub", len(gh), len(nonGH))
		}
	})
}

func TestParseGoMod(t *testing.T) {
	gomod := `module example.com/myapp

go 1.21

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0 // indirect
	golang.org/x/text v0.14.0
)
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	modules, err := ParseGoMod(path)
	if err != nil {
		t.Fatalf("ParseGoMod() error: %v", err)
	}

	if len(modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(modules))
	}

	// Check first module
	if modules[0].Path != "github.com/foo/bar" {
		t.Errorf("modules[0].Path = %q, want %q", modules[0].Path, "github.com/foo/bar")
	}
	if modules[0].Version != "v1.2.3" {
		t.Errorf("modules[0].Version = %q, want %q", modules[0].Version, "v1.2.3")
	}
	if !modules[0].Direct {
		t.Error("modules[0] should be direct")
	}
	if modules[0].Owner != "foo" || modules[0].Repo != "bar" {
		t.Errorf("modules[0] owner/repo = %q/%q, want foo/bar", modules[0].Owner, modules[0].Repo)
	}

	// Check indirect module
	if modules[1].Direct {
		t.Error("modules[1] should be indirect")
	}

	// Check non-GitHub module
	if modules[2].Owner != "" || modules[2].Repo != "" {
		t.Error("modules[2] should have empty owner/repo")
	}
}

func TestParseGoMod_FileNotFound(t *testing.T) {
	_, err := ParseGoMod("/nonexistent/go.mod")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseGoMod_InvalidSyntax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte("this is not valid go.mod"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseGoMod(path)
	if err == nil {
		t.Error("expected error for invalid go.mod")
	}
}

func TestModuleName(t *testing.T) {
	gomod := `module example.com/myapp

go 1.21

require github.com/foo/bar v1.0.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	name, err := ModuleName(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "example.com/myapp" {
		t.Errorf("expected example.com/myapp, got %s", name)
	}
}

func TestModuleName_FileNotFound(t *testing.T) {
	_, err := ModuleName("/nonexistent/go.mod")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGoModInfo(t *testing.T) {
	gomod := `module example.com/myapp

go 1.23.4

require github.com/foo/bar v1.0.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	name, goVer, err := GoModInfo(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "example.com/myapp" {
		t.Errorf("moduleName = %q, want %q", name, "example.com/myapp")
	}
	if goVer != "1.23.4" {
		t.Errorf("goVersion = %q, want %q", goVer, "1.23.4")
	}
}

func TestGoModInfo_NoGoDirective(t *testing.T) {
	gomod := `module example.com/myapp

require github.com/foo/bar v1.0.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	name, goVer, err := GoModInfo(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "example.com/myapp" {
		t.Errorf("moduleName = %q, want %q", name, "example.com/myapp")
	}
	if goVer != "" {
		t.Errorf("goVersion = %q, want empty", goVer)
	}
}

func TestGoModInfo_FileNotFound(t *testing.T) {
	_, _, err := GoModInfo("/nonexistent/go.mod")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestModuleName_MissingModuleDirective(t *testing.T) {
	// go.mod with no module directive — just a go directive and requires
	gomod := `go 1.21

require github.com/foo/bar v1.0.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ModuleName(path)
	if err == nil {
		t.Error("expected error for go.mod missing module directive")
	}
}

func TestGoModInfo_MissingModuleDirective(t *testing.T) {
	// go.mod with go directive but no module directive
	gomod := `go 1.21

require github.com/foo/bar v1.0.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	name, goVer, err := GoModInfo(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Module should be empty when directive is missing.
	if name != "" {
		t.Errorf("moduleName = %q, want empty", name)
	}
	if goVer != "1.21" {
		t.Errorf("goVersion = %q, want 1.21", goVer)
	}
}

func TestFilterGitHub_DeduplicatesMultiPathRepos(t *testing.T) {
	modules := []Module{
		{Path: "github.com/openbao/openbao/api/v2", Version: "v2.0.0", Direct: true, Owner: "openbao", Repo: "openbao"},
		{Path: "github.com/openbao/openbao/sdk/v2", Version: "v2.0.0", Direct: true, Owner: "openbao", Repo: "openbao"},
		{Path: "github.com/openbao/openbao/api/auth/approle/v2", Version: "v2.0.0", Direct: true, Owner: "openbao", Repo: "openbao"},
	}

	gh, _ := FilterGitHub(modules, false)
	if len(gh) != 1 {
		t.Errorf("expected 1 deduplicated module, got %d", len(gh))
	}
	if gh[0].Path != "github.com/openbao/openbao/api/v2" {
		t.Errorf("expected first occurrence to be kept, got %q", gh[0].Path)
	}
}
