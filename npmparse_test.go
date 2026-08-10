package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const testPackageJSON = `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "xterm": "^5.3.0",
    "@babel/core": "^7.0.0"
  },
  "devDependencies": {
    "typescript": "^5.0.0"
  },
  "peerDependencies": {
    "react": "^18.0.0"
  }
}
`

func TestParsePackageJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "package.json", testPackageJSON)

	name, mods, err := parsePackageJSON(path)
	if err != nil {
		t.Fatalf("parsePackageJSON: %v", err)
	}
	if name != "my-app" {
		t.Errorf("name = %q, want %q", name, "my-app")
	}

	byPath := map[string]Module{}
	for _, m := range mods {
		byPath[m.Path] = m
	}

	tests := []struct {
		path    string
		version string
		direct  bool
		line    int
	}{
		{"xterm", "^5.3.0", true, 5},
		{"@babel/core", "^7.0.0", true, 6},
		{"typescript", "^5.0.0", true, 9},
		{"react", "^18.0.0", false, 12},
	}
	for _, tt := range tests {
		m, ok := byPath[tt.path]
		if !ok {
			t.Errorf("%s: missing", tt.path)
			continue
		}
		if m.Version != tt.version {
			t.Errorf("%s: Version = %q, want %q", tt.path, m.Version, tt.version)
		}
		if m.Direct != tt.direct {
			t.Errorf("%s: Direct = %v, want %v", tt.path, m.Direct, tt.direct)
		}
		if m.Line != tt.line {
			t.Errorf("%s: Line = %d, want %d", tt.path, m.Line, tt.line)
		}
		if m.LineFile != "package.json" {
			t.Errorf("%s: LineFile = %q, want package.json", tt.path, m.LineFile)
		}
		if m.Ecosystem != "npm" {
			t.Errorf("%s: Ecosystem = %q, want npm", tt.path, m.Ecosystem)
		}
	}
	if len(mods) != 4 {
		t.Errorf("got %d modules, want 4", len(mods))
	}
}

func TestParsePackageJSONSortedByPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "package.json", testPackageJSON)
	_, mods, err := parsePackageJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(mods); i++ {
		if mods[i-1].Path > mods[i].Path {
			t.Fatalf("not sorted: %q before %q", mods[i-1].Path, mods[i].Path)
		}
	}
}
