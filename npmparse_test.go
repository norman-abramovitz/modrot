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

// The v1 breadth-first frontier must be keyed on (name, version), not on name
// alone. npm nests a second copy of a package under whichever parent needs a
// different version; collapsing the frontier by name drops that version and,
// worse, everything reachable only through it.
func TestParsePackageLockV1KeepsBothVersionsAndTheirSubtrees(t *testing.T) {
	const v1TwoParents = `{
  "lockfileVersion": 1,
  "dependencies": {
    "a": {
      "version": "1.0.0",
      "dependencies": { "circular-json": { "version": "0.5.9" } }
    },
    "b": {
      "version": "1.0.0",
      "dependencies": {
        "circular-json": {
          "version": "0.5.4",
          "dependencies": { "left-pad": { "version": "1.3.0" } }
        }
      }
    }
  }
}
`
	dir := t.TempDir()
	path := writeFile(t, dir, "package-lock.json", v1TwoParents)

	mods, err := parsePackageLock(path)
	if err != nil {
		t.Fatalf("parsePackageLock: %v", err)
	}
	got := map[string]bool{}
	for _, m := range mods {
		got[m.Path+"@"+m.Version] = true
	}
	for _, want := range []string{
		"circular-json@0.5.9",
		"circular-json@0.5.4",
		"left-pad@1.3.0",
	} {
		if !got[want] {
			t.Errorf("missing %s; got %v", want, got)
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

func TestParseNPMUnitWithLockfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", testPackageJSON)
	writeFile(t, dir, "package-lock.json", testPackageLockV3)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if res == nil {
		t.Fatal("got nil result")
	}
	if res.Name != "my-app" {
		t.Errorf("Name = %q, want my-app", res.Name)
	}
	if res.Primary != "package.json" {
		t.Errorf("Primary = %q, want package.json", res.Primary)
	}
	if res.Unlocked {
		t.Error("Unlocked = true, want false")
	}

	byPath := map[string]Module{}
	for _, m := range res.Modules {
		byPath[m.Path] = m
	}

	// Direct: resolved version from the lockfile, line from package.json.
	x := byPath["xterm"]
	if !x.Direct {
		t.Error("xterm: Direct = false, want true")
	}
	if x.Version != "5.3.0" {
		t.Errorf("xterm: Version = %q, want 5.3.0 (resolved)", x.Version)
	}
	if x.LineFile != "package.json" || x.Line != 5 {
		t.Errorf("xterm: anchor = %s:%d, want package.json:5", x.LineFile, x.Line)
	}

	// Indirect: lockfile-only package keeps its lockfile anchor.
	inf := byPath["inflight"]
	if inf.Direct {
		t.Error("inflight: Direct = true, want false")
	}
	if inf.LineFile != "package-lock.json" || inf.Line != 15 {
		t.Errorf("inflight: anchor = %s:%d, want package-lock.json:15", inf.LineFile, inf.Line)
	}

	// A direct dep the lockfile does not resolve has its range cleared: the
	// registry's per-version endpoint 404s on a range and the client caches
	// that as a definitive "no such package", so the dependency would vanish
	// silently. Clearing sends resolve to dist-tags.latest instead.
	if got := byPath["typescript"].Version; got != "" {
		t.Errorf("typescript: Version = %q, want \"\" (range cleared)", got)
	}
}

// A lockfile stale relative to package.json — present, parseable, but missing
// a declared dependency entirely — must clear that dependency's range rather
// than leave it to 404 against the per-version registry endpoint.
func TestParseNPMUnitStaleLockfileClearsRange(t *testing.T) {
	const pkg = `{
  "name": "my-app",
  "dependencies": {
    "xterm": "^5.3.0"
  }
}
`
	const lock = `{
  "name": "my-app",
  "lockfileVersion": 3,
  "packages": {}
}
`
	dir := t.TempDir()
	writeFile(t, dir, "package.json", pkg)
	writeFile(t, dir, "package-lock.json", lock)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if res == nil {
		t.Fatal("got nil result")
	}
	if res.Unlocked {
		t.Error("Unlocked = true, want false (a lockfile is present)")
	}
	if len(res.Modules) != 1 {
		t.Fatalf("got %d modules, want 1", len(res.Modules))
	}
	m := res.Modules[0]
	if m.Path != "xterm" {
		t.Fatalf("Path = %q, want xterm", m.Path)
	}
	if m.Version != "" {
		t.Errorf("xterm: Version = %q, want \"\" (unresolved range cleared)", m.Version)
	}
	if !m.Direct {
		t.Error("xterm: Direct = false, want true")
	}
}

// A package can be a direct dependency at one version and be pulled in
// transitively at another. Both are real: the second is what some other
// package actually resolved to, and deprecation is a per-version fact.
// Collapsing by name would lose it — the same mistake the lockfile parsers
// deliberately avoid.
func TestParseNPMUnitKeepsOtherVersionsOfADirectDep(t *testing.T) {
	const pkg = `{
  "name": "my-app",
  "dependencies": {
    "foo": "^1.0.0"
  }
}
`
	const lock = `{
  "lockfileVersion": 3,
  "packages": {
    "": { "name": "my-app" },
    "node_modules/foo": { "version": "1.0.0" },
    "node_modules/other/node_modules/foo": { "version": "2.0.0" }
  }
}
`
	dir := t.TempDir()
	writeFile(t, dir, "package.json", pkg)
	writeFile(t, dir, "package-lock.json", lock)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}

	var direct, transitive []Module
	for _, m := range res.Modules {
		if m.Path != "foo" {
			continue
		}
		if m.Direct {
			direct = append(direct, m)
		} else {
			transitive = append(transitive, m)
		}
	}

	if len(direct) != 1 {
		t.Fatalf("got %d direct foo entries, want 1", len(direct))
	}
	if direct[0].Version != "1.0.0" {
		t.Errorf("direct foo = %s, want 1.0.0 (the hoisted copy)", direct[0].Version)
	}
	if direct[0].LineFile != "package.json" {
		t.Errorf("direct foo anchored to %s, want package.json", direct[0].LineFile)
	}

	if len(transitive) != 1 {
		t.Fatalf("got %d transitive foo entries, want 1 (foo@2.0.0 must survive)", len(transitive))
	}
	if transitive[0].Version != "2.0.0" {
		t.Errorf("transitive foo = %s, want 2.0.0", transitive[0].Version)
	}
	if transitive[0].LineFile != "package-lock.json" {
		t.Errorf("transitive foo anchored to %s, want package-lock.json", transitive[0].LineFile)
	}
}

func TestParseNPMUnitUnlocked(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", testPackageJSON)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if !res.Unlocked {
		t.Error("Unlocked = false, want true")
	}
	if len(res.Files) != 1 || res.Files[0] != "package.json" {
		t.Errorf("Files = %v, want [package.json]", res.Files)
	}
	for _, m := range res.Modules {
		if m.LineFile != "package.json" {
			t.Errorf("%s: LineFile = %q, want package.json", m.Path, m.LineFile)
		}
	}
}

// Without a lockfile the declared constraint is a range, and the registry's
// per-version endpoint 404s on a range — which the client reads as "no such
// package", making every dependency silently vanish. Version must be cleared
// so the resolve phase asks for dist-tags.latest instead.
func TestParseNPMUnitUnlockedClearsRanges(t *testing.T) {
	const pkg = `{
  "name": "range-app",
  "dependencies": {
    "xterm": "^5.3.0",
    "pinned": "1.2.3"
  }
}
`
	dir := t.TempDir()
	writeFile(t, dir, "package.json", pkg)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if !res.Unlocked {
		t.Fatal("Unlocked = false, want true")
	}
	for _, m := range res.Modules {
		if m.Version != "" {
			t.Errorf("%s: Version = %q, want empty so the client uses dist-tags.latest", m.Path, m.Version)
		}
		// The anchor must survive; only the version is cleared.
		if m.LineFile != "package.json" || m.Line == 0 {
			t.Errorf("%s: anchor lost (%s:%d)", m.Path, m.LineFile, m.Line)
		}
		if !m.Direct {
			t.Errorf("%s: Direct = false, want true", m.Path)
		}
	}
}

func TestParseNPMUnitPrefersBunLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", testPackageJSON)
	writeFile(t, dir, "package-lock.json", testPackageLockV3)
	writeFile(t, dir, "bun.lock", testBunLock)

	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if len(res.Files) != 2 || res.Files[1] != "bun.lock" {
		t.Errorf("Files = %v, want [package.json bun.lock]", res.Files)
	}
	for _, m := range res.Modules {
		if m.LineFile == "package-lock.json" {
			t.Errorf("%s anchored to package-lock.json, want bun.lock ignored", m.Path)
		}
	}
}

func TestParseNPMUnitNoPackageJSON(t *testing.T) {
	dir := t.TempDir()
	res, err := parseNPMUnit(dir)
	if err != nil {
		t.Fatalf("parseNPMUnit: %v", err)
	}
	if res != nil {
		t.Errorf("got %+v, want nil for a directory with no package.json", res)
	}
}
