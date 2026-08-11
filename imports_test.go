package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildImportPattern(t *testing.T) {
	paths := []string{
		"github.com/mitchellh/copystructure",
		"github.com/pkg/errors",
	}
	got := buildImportPattern(paths)
	want := `"(github\.com/mitchellh/copystructure|github\.com/pkg/errors)(/|")`
	if got != want {
		t.Errorf("buildImportPattern() =\n  %q\nwant:\n  %q", got, want)
	}
}

func TestBuildImportPattern_Single(t *testing.T) {
	got := buildImportPattern([]string{"github.com/foo/bar"})
	want := `"(github\.com/foo/bar)(/|")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseRgLine(t *testing.T) {
	tests := []struct {
		input   string
		file    string
		lineNum int
		content string
		ok      bool
	}{
		{
			input:   `/path/to/file.go:14:	"github.com/mitchellh/copystructure"`,
			file:    "/path/to/file.go",
			lineNum: 14,
			content: `	"github.com/mitchellh/copystructure"`,
			ok:      true,
		},
		{
			input:   `foo.go:1:import "github.com/foo/bar"`,
			file:    "foo.go",
			lineNum: 1,
			content: `import "github.com/foo/bar"`,
			ok:      true,
		},
		{
			input: "no-colons-here",
			ok:    false,
		},
		{
			input: "file:notanumber:content",
			ok:    false,
		},
		{
			input: "file:123",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			file, lineNum, content, ok := parseRgLine(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if file != tt.file {
				t.Errorf("file = %q, want %q", file, tt.file)
			}
			if lineNum != tt.lineNum {
				t.Errorf("lineNum = %d, want %d", lineNum, tt.lineNum)
			}
			if content != tt.content {
				t.Errorf("content = %q, want %q", content, tt.content)
			}
		})
	}
}

func TestMatchModule(t *testing.T) {
	// Must be sorted longest-first (as matchModule expects)
	modules := []string{
		"github.com/hashicorp/go-discover/provider/aws",
		"github.com/hashicorp/go-discover",
		"github.com/mitchellh/copystructure",
		"github.com/pkg/errors",
	}

	tests := []struct {
		importPath string
		want       string
	}{
		// Exact match
		{"github.com/mitchellh/copystructure", "github.com/mitchellh/copystructure"},
		// Subpackage match
		{"github.com/hashicorp/go-discover/provider/aws", "github.com/hashicorp/go-discover/provider/aws"},
		// Subpackage match, longer module wins
		{"github.com/hashicorp/go-discover/provider/aws/sub", "github.com/hashicorp/go-discover/provider/aws"},
		// Subpackage match, shorter module
		{"github.com/hashicorp/go-discover/provider/gce", "github.com/hashicorp/go-discover"},
		// No match
		{"github.com/unrelated/thing", ""},
		// Prefix but not at path boundary (should NOT match)
		{"github.com/pkg/errorsx", ""},
	}

	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			got := matchModule(tt.importPath, modules)
			if got != tt.want {
				t.Errorf("matchModule(%q) = %q, want %q", tt.importPath, got, tt.want)
			}
		})
	}
}

func TestParseRgOutput(t *testing.T) {
	modulePaths := []string{
		"github.com/mitchellh/copystructure",
		"github.com/mitchellh/reflectwalk",
	}

	rgOutput := `/proj/audit/hashstructure.go:14:	"github.com/mitchellh/copystructure"
/proj/vault/policy.go:17:	"github.com/mitchellh/copystructure"
/proj/audit/hashstructure.go:15:	"github.com/mitchellh/reflectwalk"
`

	got := parseRgOutput(rgOutput, "/proj", modulePaths)

	wantCopy := []FileMatch{
		{File: "audit/hashstructure.go", Line: 14, ImportPath: "github.com/mitchellh/copystructure"},
		{File: "vault/policy.go", Line: 17, ImportPath: "github.com/mitchellh/copystructure"},
	}
	wantWalk := []FileMatch{
		{File: "audit/hashstructure.go", Line: 15, ImportPath: "github.com/mitchellh/reflectwalk"},
	}

	if !reflect.DeepEqual(got["github.com/mitchellh/copystructure"], wantCopy) {
		t.Errorf("copystructure matches =\n  %+v\nwant:\n  %+v", got["github.com/mitchellh/copystructure"], wantCopy)
	}
	if !reflect.DeepEqual(got["github.com/mitchellh/reflectwalk"], wantWalk) {
		t.Errorf("reflectwalk matches =\n  %+v\nwant:\n  %+v", got["github.com/mitchellh/reflectwalk"], wantWalk)
	}
}

func TestParseRgOutput_Subpackage(t *testing.T) {
	modulePaths := []string{
		"github.com/hashicorp/go-discover",
	}

	rgOutput := `/proj/foo.go:5:	"github.com/hashicorp/go-discover/provider/aws"
`

	got := parseRgOutput(rgOutput, "/proj", modulePaths)

	want := []FileMatch{
		{File: "foo.go", Line: 5, ImportPath: "github.com/hashicorp/go-discover/provider/aws"},
	}

	if !reflect.DeepEqual(got["github.com/hashicorp/go-discover"], want) {
		t.Errorf("got %+v, want %+v", got["github.com/hashicorp/go-discover"], want)
	}
}

// TestParseRgOutput_MultipleImportsOnOneLine pins that every quoted path on a
// matched line is considered. Taking only the first one drops later imports,
// and drops the finding entirely when the first path is not searched for.
func TestParseRgOutput_MultipleImportsOnOneLine(t *testing.T) {
	modulePaths := []string{"github.com/foo/bar"}

	rgOutput := `/proj/two.go:3:import _ "github.com/other/thing"; import _ "github.com/foo/bar"
/proj/sub.go:4:import _ "github.com/foo/bar"; import _ "github.com/foo/bar/sub"
/proj/dup.go:5:import _ "github.com/foo/bar"; import _ "github.com/foo/bar"
`

	got := parseRgOutput(rgOutput, "/proj", modulePaths)

	want := []FileMatch{
		{File: "dup.go", Line: 5, ImportPath: "github.com/foo/bar"},
		{File: "sub.go", Line: 4, ImportPath: "github.com/foo/bar"},
		{File: "sub.go", Line: 4, ImportPath: "github.com/foo/bar/sub"},
		{File: "two.go", Line: 3, ImportPath: "github.com/foo/bar"},
	}
	if !reflect.DeepEqual(got["github.com/foo/bar"], want) {
		t.Errorf("matches =\n  %+v\nwant:\n  %+v", got["github.com/foo/bar"], want)
	}
}

func TestParseRgOutput_NoMatches(t *testing.T) {
	got := parseRgOutput("", "/proj", []string{"github.com/foo/bar"})
	if len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
}

func TestParseRgOutput_MalformedLines(t *testing.T) {
	modulePaths := []string{"github.com/foo/bar"}

	// Mix of valid lines, unparseable lines, and lines with no quoted import
	rgOutput := `/proj/good.go:5:	"github.com/foo/bar"
not-a-valid-line
/proj/noquote.go:10:	github.com/foo/bar
/proj/nomatch.go:3:	"github.com/unrelated/thing"
/proj/good2.go:7:	"github.com/foo/bar/sub"
`

	got := parseRgOutput(rgOutput, "/proj", modulePaths)
	matches := got["github.com/foo/bar"]

	if len(matches) != 2 {
		t.Fatalf("expected 2 valid matches, got %d: %+v", len(matches), matches)
	}
	if matches[0].File != "good.go" {
		t.Errorf("first match file = %q, want %q", matches[0].File, "good.go")
	}
	if matches[1].File != "good2.go" || matches[1].ImportPath != "github.com/foo/bar/sub" {
		t.Errorf("second match = %+v, want good2.go with subpackage import", matches[1])
	}
}

func TestParseRgOutput_ProjectDirWithTrailingSlash(t *testing.T) {
	modulePaths := []string{"github.com/foo/bar"}
	rgOutput := `/proj/src/main.go:1:	"github.com/foo/bar"
`
	got := parseRgOutput(rgOutput, "/proj/", modulePaths)
	matches := got["github.com/foo/bar"]
	if len(matches) != 1 || matches[0].File != "src/main.go" {
		t.Errorf("got %+v, want [{src/main.go 1 github.com/foo/bar}]", matches)
	}
}

func TestScanImports_EmptyPaths(t *testing.T) {
	got, err := ScanImports("/tmp", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestScanImports_Integration(t *testing.T) {
	// Skip if rg is not installed
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed, skipping integration test")
	}

	// Create a temp directory with Go files
	dir := t.TempDir()

	// File that imports an archived module
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"fmt"
	"github.com/mitchellh/copystructure"
)

func main() {
	fmt.Println(copystructure.Copy)
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// File that imports a subpackage of an archived module
	err = os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "sub", "helper.go"), []byte(`package sub

import "github.com/hashicorp/go-discover/provider/aws"

var _ = aws.New
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// File with no archived imports (should not appear)
	err = os.WriteFile(filepath.Join(dir, "clean.go"), []byte(`package main

import "fmt"

func clean() { fmt.Println("clean") }
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Vendor dir should be excluded
	err = os.MkdirAll(filepath.Join(dir, "vendor"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "vendor", "vendored.go"), []byte(`package vendor

import "github.com/mitchellh/copystructure"

var _ = copystructure.Copy
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	modulePaths := []string{
		"github.com/mitchellh/copystructure",
		"github.com/hashicorp/go-discover",
	}

	got, err := ScanImports(dir, modulePaths)
	if err != nil {
		t.Fatalf("ScanImports error: %v", err)
	}

	// copystructure: should find main.go only (not vendor/)
	copyMatches := got["github.com/mitchellh/copystructure"]
	if len(copyMatches) != 1 {
		t.Errorf("copystructure: expected 1 match, got %d: %+v", len(copyMatches), copyMatches)
	} else if copyMatches[0].File != "main.go" {
		t.Errorf("copystructure match file = %q, want %q", copyMatches[0].File, "main.go")
	}

	// go-discover: should find sub/helper.go via subpackage import
	discoverMatches := got["github.com/hashicorp/go-discover"]
	if len(discoverMatches) != 1 {
		t.Errorf("go-discover: expected 1 match, got %d: %+v", len(discoverMatches), discoverMatches)
	} else {
		if discoverMatches[0].File != "sub/helper.go" {
			t.Errorf("go-discover match file = %q, want %q", discoverMatches[0].File, "sub/helper.go")
		}
		if discoverMatches[0].ImportPath != "github.com/hashicorp/go-discover/provider/aws" {
			t.Errorf("go-discover import = %q, want subpackage path", discoverMatches[0].ImportPath)
		}
	}
}

func TestScanImports_NoMatches(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed, skipping integration test")
	}

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "fmt"

func main() { fmt.Println("hello") }
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ScanImports(dir, []string{"github.com/nonexistent/module"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
}

func TestParseRgOutput_SortedByFileThenLine(t *testing.T) {
	modulePaths := []string{"github.com/foo/bar"}

	rgOutput := `/proj/z.go:10:	"github.com/foo/bar"
/proj/a.go:20:	"github.com/foo/bar"
/proj/a.go:5:	"github.com/foo/bar"
`

	got := parseRgOutput(rgOutput, "/proj", modulePaths)
	matches := got["github.com/foo/bar"]

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	if matches[0].File != "a.go" || matches[0].Line != 5 {
		t.Errorf("first match should be a.go:5, got %s:%d", matches[0].File, matches[0].Line)
	}
	if matches[1].File != "a.go" || matches[1].Line != 20 {
		t.Errorf("second match should be a.go:20, got %s:%d", matches[1].File, matches[1].Line)
	}
	if matches[2].File != "z.go" || matches[2].Line != 10 {
		t.Errorf("third match should be z.go:10, got %s:%d", matches[2].File, matches[2].Line)
	}
}
