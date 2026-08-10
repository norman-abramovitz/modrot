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

// depLines is the shared primitive for all three manifest parsers, so its
// decoder-state discipline is load-bearing: a desync silently loses every
// section after the offending one.
func TestDepLinesSurvivesNonObjectSections(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"array section", `{"dependencies":["a","b"], "devDependencies":{"typescript":"^5.0.0"}}`},
		{"array of objects", `{"dependencies":[{"x":1},{"y":2}], "devDependencies":{"typescript":"^5.0.0"}}`},
		{"nested arrays", `{"dependencies":[[1,[2]],3], "devDependencies":{"typescript":"^5.0.0"}}`},
		{"null section", `{"dependencies":null, "devDependencies":{"typescript":"^5.0.0"}}`},
		{"scalar section", `{"dependencies":42, "devDependencies":{"typescript":"^5.0.0"}}`},
		{"string section", `{"dependencies":"nope", "devDependencies":{"typescript":"^5.0.0"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			got, err := depLines(src, newLineIndex(src), []string{"dependencies", "devDependencies"})
			if err != nil {
				t.Fatalf("depLines: %v", err)
			}
			if _, ok := got["devDependencies"]["typescript"]; !ok {
				t.Errorf("devDependencies lost after a non-object section: %v", got)
			}
		})
	}
}

// encoding/json merges duplicate top-level keys, so both dependencies are
// real and each needs its own line rather than one silently getting 0.
func TestDepLinesMergesDuplicateSections(t *testing.T) {
	src := []byte("{\n  \"dependencies\": {\"a\":\"1.0.0\"},\n  \"dependencies\": {\"b\":\"2.0.0\"}\n}")
	got, err := depLines(src, newLineIndex(src), []string{"dependencies"})
	if err != nil {
		t.Fatalf("depLines: %v", err)
	}
	if got["dependencies"]["a"] != 2 {
		t.Errorf("a line = %d, want 2", got["dependencies"]["a"])
	}
	if got["dependencies"]["b"] != 3 {
		t.Errorf("b line = %d, want 3", got["dependencies"]["b"])
	}
}

func TestDepLinesMalformed(t *testing.T) {
	tests := []struct{ name, src string }{
		{"truncated", `{"dependencies":{"a":`},
		{"top level array", `["a"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			if _, err := depLines(src, newLineIndex(src), []string{"dependencies"}); err == nil {
				t.Error("want an error, got nil")
			}
		})
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

const testPackageLockV3 = `{
  "name": "my-app",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "my-app"
    },
    "node_modules/xterm": {
      "version": "5.3.0",
      "resolved": "https://registry.npmjs.org/xterm/-/xterm-5.3.0.tgz"
    },
    "node_modules/@babel/core": {
      "version": "7.24.0"
    },
    "node_modules/inflight": {
      "version": "1.0.6"
    },
    "node_modules/a/node_modules/nested": {
      "version": "2.0.0"
    },
    "node_modules/linked": {
      "resolved": "../local",
      "link": true
    }
  }
}
`

func TestParsePackageLockV3(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", testPackageLockV3)

	mods, err := parsePackageLock(path)
	if err != nil {
		t.Fatalf("parsePackageLock: %v", err)
	}

	byPath := map[string]Module{}
	for _, m := range mods {
		byPath[m.Path] = m
	}

	tests := []struct {
		path    string
		version string
		line    int
	}{
		{"xterm", "5.3.0", 8},
		{"@babel/core", "7.24.0", 12},
		{"inflight", "1.0.6", 15},
		{"nested", "2.0.0", 18},
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
		if m.Line != tt.line {
			t.Errorf("%s: Line = %d, want %d", tt.path, m.Line, tt.line)
		}
		if m.LineFile != "package-lock.json" {
			t.Errorf("%s: LineFile = %q, want package-lock.json", tt.path, m.LineFile)
		}
		if m.Direct {
			t.Errorf("%s: Direct = true, want false", tt.path)
		}
	}
	// The root entry "" and the link entry must both be skipped.
	if _, ok := byPath["linked"]; ok {
		t.Error("link:true entry should be skipped")
	}
	if len(mods) != 4 {
		t.Errorf("got %d modules, want 4: %v", len(mods), byPath)
	}
}

// npm installs the same package at several depths with different versions.
// Only one is reported, and which one must not depend on map iteration order,
// or identical scans produce different output and SARIF results flap.
func TestParsePackageLockDeterministic(t *testing.T) {
	const multiDepth = `{
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "root" },
    "node_modules/lodash": { "version": "4.17.21" },
    "node_modules/a/node_modules/lodash": { "version": "3.10.1" },
    "node_modules/b/node_modules/lodash": { "version": "2.4.2" }
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", multiDepth)

	// The hoisted top-level install must win, every run.
	for i := 0; i < 25; i++ {
		mods, err := parsePackageLock(path)
		if err != nil {
			t.Fatalf("parsePackageLock: %v", err)
		}
		if len(mods) != 1 {
			t.Fatalf("run %d: got %d modules, want 1", i, len(mods))
		}
		if mods[0].Version != "4.17.21" {
			t.Fatalf("run %d: lodash = %s, want 4.17.21 (the hoisted copy)", i, mods[0].Version)
		}
	}
}

// With no top-level copy, the choice must still be stable across runs.
func TestParsePackageLockDeterministicNestedOnly(t *testing.T) {
	const nestedOnly = `{
  "lockfileVersion": 3,
  "packages": {
    "node_modules/b/node_modules/lodash": { "version": "2.4.2" },
    "node_modules/a/node_modules/lodash": { "version": "3.10.1" }
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", nestedOnly)

	first, err := parsePackageLock(path)
	if err != nil {
		t.Fatalf("parsePackageLock: %v", err)
	}
	for i := 0; i < 25; i++ {
		mods, err := parsePackageLock(path)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if len(mods) != len(first) || mods[0].Version != first[0].Version {
			t.Fatalf("run %d: got %s, first run got %s", i, mods[0].Version, first[0].Version)
		}
	}
}

// The v1 nested walk must be breadth-first: the top-level copy wins over one
// nested under a dependency, regardless of map order.
func TestParsePackageLockV1Deterministic(t *testing.T) {
	const v1Dup = `{
  "lockfileVersion": 1,
  "dependencies": {
    "lodash": { "version": "4.17.21" },
    "wrapper": {
      "version": "1.0.0",
      "dependencies": { "lodash": { "version": "3.10.1" } }
    }
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", v1Dup)

	for i := 0; i < 25; i++ {
		mods, err := parsePackageLock(path)
		if err != nil {
			t.Fatalf("parsePackageLock: %v", err)
		}
		byPath := map[string]Module{}
		for _, m := range mods {
			byPath[m.Path] = m
		}
		if byPath["lodash"].Version != "4.17.21" {
			t.Fatalf("run %d: lodash = %s, want 4.17.21 (top level)", i, byPath["lodash"].Version)
		}
	}
}

const testPackageLockV1 = `{
  "name": "old-app",
  "lockfileVersion": 1,
  "dependencies": {
    "xterm": {
      "version": "5.3.0"
    },
    "wrapper": {
      "version": "1.0.0",
      "dependencies": {
        "inflight": {
          "version": "1.0.6"
        }
      }
    }
  }
}
`

func TestParsePackageLockV1(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", testPackageLockV1)

	mods, err := parsePackageLock(path)
	if err != nil {
		t.Fatalf("parsePackageLock: %v", err)
	}
	byPath := map[string]Module{}
	for _, m := range mods {
		byPath[m.Path] = m
	}
	if got := byPath["xterm"].Version; got != "5.3.0" {
		t.Errorf("xterm version = %q, want 5.3.0", got)
	}
	if got := byPath["xterm"].Line; got != 5 {
		t.Errorf("xterm line = %d, want 5", got)
	}
	if got := byPath["inflight"].Version; got != "1.0.6" {
		t.Errorf("inflight version = %q, want 1.0.6", got)
	}
	// Nested v1 entries are file-level: no line is recoverable cheaply.
	if got := byPath["inflight"].Line; got != 0 {
		t.Errorf("inflight line = %d, want 0", got)
	}
	if len(mods) != 3 {
		t.Errorf("got %d modules, want 3", len(mods))
	}
}
