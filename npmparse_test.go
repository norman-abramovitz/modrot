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

// Several versions of one package legitimately coexist in a lockfile
// (measured: 196 such names in a real 1868-key file). Deprecation is a
// per-version fact, so every distinct version must survive; only exact
// (name, version) duplicates collapse.
func TestParsePackageLockKeepsDistinctVersions(t *testing.T) {
	const multiVersion = `{
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "root" },
    "node_modules/esbuild": { "version": "0.27.7" },
    "node_modules/a/node_modules/esbuild": { "version": "0.25.9" },
    "node_modules/b/node_modules/esbuild": { "version": "0.25.9" },
    "node_modules/c/node_modules/esbuild": { "version": "0.27.3" }
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", multiVersion)

	mods, err := parsePackageLock(path)
	if err != nil {
		t.Fatalf("parsePackageLock: %v", err)
	}
	versions := map[string]bool{}
	for _, m := range mods {
		if m.Path != "esbuild" {
			t.Errorf("unexpected module %q", m.Path)
			continue
		}
		if versions[m.Version] {
			t.Errorf("duplicate entry for esbuild@%s", m.Version)
		}
		versions[m.Version] = true
	}
	// 0.25.9 appears twice and must collapse; three distinct versions remain.
	want := []string{"0.27.7", "0.25.9", "0.27.3"}
	if len(versions) != len(want) {
		t.Fatalf("got %d distinct versions %v, want %d", len(versions), versions, len(want))
	}
	for _, v := range want {
		if !versions[v] {
			t.Errorf("lost esbuild@%s", v)
		}
	}
	// The hoisted copy must sort first, so parseNPMUnit resolves direct
	// dependencies to it.
	if mods[0].Version != "0.27.7" {
		t.Errorf("first entry = %s, want the hoisted 0.27.7", mods[0].Version)
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

	// All three versions survive (deprecation is per-version), but the
	// hoisted copy must sort first every run so parseNPMUnit resolves
	// direct dependencies to it.
	for i := 0; i < 25; i++ {
		mods, err := parsePackageLock(path)
		if err != nil {
			t.Fatalf("parsePackageLock: %v", err)
		}
		if len(mods) != 3 {
			t.Fatalf("run %d: got %d modules, want 3 distinct versions", i, len(mods))
		}
		if mods[0].Version != "4.17.21" {
			t.Fatalf("run %d: first entry = %s, want the hoisted 4.17.21", i, mods[0].Version)
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
		// Both versions survive; the top-level one must come first, since
		// parseNPMUnit takes the first occurrence as the resolved version.
		var firstLodash string
		for _, m := range mods {
			if m.Path == "lodash" {
				firstLodash = m.Version
				break
			}
		}
		if firstLodash != "4.17.21" {
			t.Fatalf("run %d: first lodash = %s, want 4.17.21 (top level)", i, firstLodash)
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

// Real bun.lock shape: JSONC with trailing commas, packages entries are
// arrays whose first element is "<name>@<version>".
const testBunLock = `{
  "lockfileVersion": 1,
  "workspaces": {
    "": {
      "devDependencies": {
        "@playwright/test": "^1.58.2",
      },
    },
  },
  "packages": {
    "@playwright/test": ["@playwright/test@1.58.2", "", {}, "sha512-aaa=="],

    "playwright": ["playwright@1.58.2", "", {}, "sha512-bbb=="],

    "fsevents": ["fsevents@2.3.2", "", { "os": "darwin" }, "sha512-ccc=="],
  }
}
`

func TestParseBunLock(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bun.lock", testBunLock)

	mods, err := parseBunLock(path)
	if err != nil {
		t.Fatalf("parseBunLock: %v", err)
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
		{"@playwright/test", "1.58.2", 11},
		{"playwright", "1.58.2", 13},
		{"fsevents", "2.3.2", 15},
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
		if m.LineFile != "bun.lock" {
			t.Errorf("%s: LineFile = %q, want bun.lock", tt.path, m.LineFile)
		}
		if m.Ecosystem != "npm" {
			t.Errorf("%s: Ecosystem = %q, want npm", tt.path, m.Ecosystem)
		}
	}
	if len(mods) != 3 {
		t.Errorf("got %d modules, want 3", len(mods))
	}
}

func TestBunKeyDepth(t *testing.T) {
	tests := []struct {
		key  string
		want int
	}{
		{"negotiator", 1},
		{"@angular-devkit/core", 1},
		{"accepts/negotiator", 2},
		{"angular-eslint/@angular-devkit/core", 2},
		{"a/b/c", 3},
	}
	for _, tt := range tests {
		if got := bunKeyDepth(tt.key); got != tt.want {
			t.Errorf("bunKeyDepth(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

// bun.lock nests transitive resolutions, so the same package appears under
// several keys. Exact duplicates collapse; genuinely different versions of
// one package must all survive, because deprecation is per-version.
func TestParseBunLockDeduplicatesByNameAndVersion(t *testing.T) {
	const nested = `{
  "lockfileVersion": 1,
  "packages": {
    "@babel/code-frame": ["@babel/code-frame@7.29.7", "", {}, "sha512-a=="],
    "accepts/negotiator": ["negotiator@0.6.3", "", {}, "sha512-b=="],
    "negotiator": ["negotiator@0.6.3", "", {}, "sha512-b=="],
    "wrapper/@babel/code-frame": ["@babel/code-frame@8.0.0", "", {}, "sha512-c=="],
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "bun.lock", nested)

	mods, err := parseBunLock(path)
	if err != nil {
		t.Fatalf("parseBunLock: %v", err)
	}

	// negotiator@0.6.3 appears twice and must collapse to one entry,
	// anchored to the shallower "negotiator" key.
	var negotiators []Module
	var codeFrames []Module
	for _, m := range mods {
		switch m.Path {
		case "negotiator":
			negotiators = append(negotiators, m)
		case "@babel/code-frame":
			codeFrames = append(codeFrames, m)
		}
	}
	if len(negotiators) != 1 {
		t.Errorf("negotiator: got %d entries, want 1 (exact duplicate)", len(negotiators))
	} else if negotiators[0].Line != 6 {
		t.Errorf("negotiator anchored to line %d, want 6 (the shallower key)", negotiators[0].Line)
	}
	// Two real versions of one package must both survive.
	if len(codeFrames) != 2 {
		t.Errorf("@babel/code-frame: got %d entries, want 2 distinct versions", len(codeFrames))
	}
}

// The retained entry's line must not depend on map iteration order.
func TestParseBunLockDeterministicLines(t *testing.T) {
	const nested = `{
  "lockfileVersion": 1,
  "packages": {
    "accepts/negotiator": ["negotiator@0.6.3", "", {}, "sha512-b=="],
    "negotiator": ["negotiator@0.6.3", "", {}, "sha512-b=="],
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "bun.lock", nested)

	for i := 0; i < 25; i++ {
		mods, err := parseBunLock(path)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if len(mods) != 1 {
			t.Fatalf("run %d: got %d modules, want 1", i, len(mods))
		}
		if mods[0].Line != 5 {
			t.Fatalf("run %d: line = %d, want 5 (the shallower key)", i, mods[0].Line)
		}
	}
}

func TestSplitNameVersion(t *testing.T) {
	tests := []struct {
		in      string
		name    string
		version string
	}{
		{"xterm@5.3.0", "xterm", "5.3.0"},
		{"@babel/core@7.24.0", "@babel/core", "7.24.0"},
		{"@scope/pkg@1.0.0-beta.1", "@scope/pkg", "1.0.0-beta.1"},
		{"noversion", "noversion", ""},
	}
	for _, tt := range tests {
		name, version := splitNameVersion(tt.in)
		if name != tt.name || version != tt.version {
			t.Errorf("splitNameVersion(%q) = (%q, %q), want (%q, %q)",
				tt.in, name, version, tt.name, tt.version)
		}
	}
}
