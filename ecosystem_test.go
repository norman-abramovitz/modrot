package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testGoMod = `module example.com/app

go 1.21

require github.com/foo/bar v1.2.3
`

func TestDiscoverUnits(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"go only", []string{"go.mod"}, []string{"go"}},
		{"npm only", []string{"package.json"}, []string{"npm"}},
		{"both", []string{"go.mod", "package.json"}, []string{"go", "npm"}},
		{"neither", []string{"README.md"}, nil},
		{"lockfile without package.json", []string{"package-lock.json"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var got []string
			for _, eco := range discoverUnits(dir) {
				got = append(got, eco.Name)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFindUnitDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, name string) {
		d := filepath.Join(root, rel)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk(".", "go.mod")
	mk("web", "package.json")
	mk("node_modules/dep", "package.json") // must be skipped
	mk("vendor/x", "go.mod")               // must be skipped
	mk(".hidden", "package.json")          // must be skipped
	mk("testdata/x", "go.mod")             // must be skipped

	dirs, err := findUnitDirs(root)
	if err != nil {
		t.Fatalf("findUnitDirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2: %v", len(dirs), dirs)
	}
}

func TestParseGoUnit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := parseGoUnit(dir)
	if err != nil {
		t.Fatalf("parseGoUnit: %v", err)
	}
	if res.Name != "example.com/app" {
		t.Errorf("Name = %q, want example.com/app", res.Name)
	}
	if res.Primary != "go.mod" {
		t.Errorf("Primary = %q, want go.mod", res.Primary)
	}
	if res.Unlocked {
		t.Error("Unlocked = true, want false for Go")
	}
	if len(res.Modules) != 1 || res.Modules[0].Path != "github.com/foo/bar" {
		t.Errorf("Modules = %+v", res.Modules)
	}
}

// --tree support is expressed as data: npm has no graph source.
func TestNPMEcosystemHasNoGraph(t *testing.T) {
	if npmEcosystem.Graph != nil {
		t.Error("npmEcosystem.Graph should be nil")
	}
	if goEcosystem.Graph == nil {
		t.Error("goEcosystem.Graph should be set")
	}
}

func TestBuildManifestInfos(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(testGoMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(testPackageJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	units, failed := buildManifestInfos([]string{root}, root)
	if failed != 0 {
		t.Fatalf("got %d parse failures, want 0", failed)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	if units[0].eco.Name != "go" || units[1].eco.Name != "npm" {
		t.Fatalf("order = %s, %s; want go, npm", units[0].eco.Name, units[1].eco.Name)
	}
	if units[0].moduleName != "example.com/app" {
		t.Errorf("go unit name = %q", units[0].moduleName)
	}
	if units[1].moduleName != "my-app" {
		t.Errorf("npm unit name = %q", units[1].moduleName)
	}
	if !units[1].unlocked {
		t.Error("npm unit should be unlocked (no lockfile written)")
	}
	if units[1].relPath != "package.json" {
		t.Errorf("npm relPath = %q, want package.json", units[1].relPath)
	}
}

// Unit order is user-visible: it drives text output, --json modules[] and
// --sarif results[], where the first location anchors a code-scanning alert.
// WalkDir hands back a directory before its entries, so discovery order puts
// the root first; buildManifestInfos must sort back to path order.
func TestBuildManifestInfosSortsByPath(t *testing.T) {
	root := t.TempDir()
	rels := []string{".", "api", "api/auth", "zlib"}
	// Feed the dirs in WalkDir order (root before its entries), which is the
	// order that used to leak into the output.
	dirs := make([]string, 0, len(rels))
	for _, rel := range rels {
		d := filepath.Join(root, rel)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte(testGoMod), 0o600); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
	}

	units, failed := buildManifestInfos(dirs, root)
	if failed != 0 {
		t.Fatalf("got %d parse failures, want 0", failed)
	}
	want := []string{"api/auth/go.mod", "api/go.mod", "go.mod", "zlib/go.mod"}
	if len(units) != len(want) {
		t.Fatalf("got %d units, want %d", len(units), len(want))
	}
	for i, w := range want {
		if got := filepath.ToSlash(units[i].relPath); got != w {
			t.Errorf("units[%d] = %q, want %q", i, got, w)
		}
	}
}

// A manifest that exists but cannot be parsed must not be reported as a
// missing manifest — that sends the user looking for the wrong problem.
func TestBuildManifestInfosCountsParseFailures(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module \x00bad\nrequire (\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	units, failed := buildManifestInfos([]string{dir}, dir)
	if len(units) != 0 {
		t.Errorf("got %d units, want 0", len(units))
	}
	if failed != 1 {
		t.Errorf("got %d parse failures, want 1", failed)
	}
}

// enrichUnits must deduplicate across manifests: a dependency shared by three
// go.mod files costs one lookup, not three. This is the guarantee that
// resolveAcrossModules and checkDeprecationsAcrossModules used to provide.
func TestEnrichUnitsDeduplicatesAcrossUnits(t *testing.T) {
	shared := Module{Path: "example.com/shared", Version: "v1.0.0", Ecosystem: "go", LineFile: "go.mod"}

	var calls int
	fake := &Ecosystem{
		Name:      "go",
		Manifests: []string{"go.mod"},
		Resolve: func(mods []Module) int {
			calls++
			for i := range mods {
				mods[i].Owner, mods[i].Repo = "acme", "shared"
			}
			return len(mods)
		},
		Deprecations: func(mods []Module) int { return 0 },
	}
	orig := ecosystems
	ecosystems = []*Ecosystem{fake}
	t.Cleanup(func() { ecosystems = orig })

	// Same dependency in three units, direct in one and indirect in the others,
	// at different lines.
	a := shared
	a.Direct, a.Line = true, 5
	b := shared
	b.Direct, b.Line = false, 11
	c := shared
	c.Direct, c.Line = false, 3

	units := []manifestInfo{
		{eco: fake, allModules: []Module{a}},
		{eco: fake, allModules: []Module{b}},
		{eco: fake, allModules: []Module{c}},
	}

	cfg := NewDefaultConfig()
	cfg.Resolve = true
	enrichUnits(units, cfg)

	if calls != 1 {
		t.Errorf("Resolve called %d times, want 1", calls)
	}
	// One lookup, but every location enriched.
	for i, u := range units {
		if u.allModules[0].Owner != "acme" || u.allModules[0].Repo != "shared" {
			t.Errorf("unit %d not enriched: %+v", i, u.allModules[0])
		}
	}
	// Per-location fields must survive the fan-out.
	if !units[0].allModules[0].Direct {
		t.Error("unit 0 lost Direct=true")
	}
	if units[1].allModules[0].Direct {
		t.Error("unit 1 gained Direct=true")
	}
	wantLines := []int{5, 11, 3}
	for i, want := range wantLines {
		if got := units[i].allModules[0].Line; got != want {
			t.Errorf("unit %d Line = %d, want %d", i, got, want)
		}
	}
}

func TestUnitHeader(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.GoToolchain = "go1.24.5"

	tests := []struct {
		name string
		mi   manifestInfo
		want string
	}{
		{
			"go",
			manifestInfo{eco: goEcosystem, relPath: "go.mod", moduleName: "github.com/foo/bar"},
			"go.mod — github.com/foo/bar (go1.24.5)",
		},
		{
			"npm locked",
			manifestInfo{eco: npmEcosystem, relPath: "package.json", moduleName: "my-app"},
			"package.json — my-app (npm)",
		},
		{
			"npm unlocked",
			manifestInfo{eco: npmEcosystem, relPath: "web/package.json", moduleName: "my-app", unlocked: true},
			"web/package.json — my-app (npm, unlocked)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unitHeader(tt.mi, cfg); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// The recursive JSON entry's go_version field must say the same thing the
// text header says. Stamping the Go toolchain onto an npm unit reports a
// toolchain that had nothing to do with resolving those dependencies.
func TestUnitQualifier(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.GoToolchain = "go1.24.5"

	tests := []struct {
		name string
		mi   manifestInfo
		want string
	}{
		{"go", manifestInfo{eco: goEcosystem}, "go1.24.5"},
		{"npm locked", manifestInfo{eco: npmEcosystem}, "npm"},
		{"npm unlocked", manifestInfo{eco: npmEcosystem, unlocked: true}, "npm, unlocked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unitQualifier(tt.mi, cfg); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// enrichUnits fans results back out by copying an enumerated list of fields,
// which silently drops any field added later — the same defect that once left
// --age blank for every GitHub-hosted module. This pins the list: a new
// enrichment field must be added here, or it never reaches output.
func TestApplyEnrichedCarriesEveryEnrichedField(t *testing.T) {
	src := Module{
		Path:            "xterm",
		Version:         "5.3.0",
		VersionInferred: true,
		Owner:           "xtermjs",
		Repo:            "xterm.js",
		Deprecated:      "moved to @xterm/xterm",
		SourceURL:       "https://github.com/xtermjs/xterm.js",
		LatestVersion:   "5.5.0",
	}
	// Per-location fields must NOT be overwritten: the same dependency can be
	// direct in one manifest and transitive in another, at different lines.
	dst := Module{
		Path:      "xterm",
		Direct:    true,
		Line:      12,
		LineFile:  "package.json",
		Ecosystem: "npm",
	}

	applyEnriched(&dst, src)

	if !dst.VersionInferred {
		t.Error("VersionInferred did not survive the copy")
	}
	if dst.Version != "5.3.0" || dst.Owner != "xtermjs" || dst.Repo != "xterm.js" {
		t.Errorf("version/owner/repo lost: %+v", dst)
	}
	if dst.Deprecated == "" || dst.SourceURL == "" {
		t.Errorf("deprecated/source_url lost: %+v", dst)
	}
	if dst.Direct != true || dst.Line != 12 || dst.LineFile != "package.json" {
		t.Errorf("per-location fields were overwritten: %+v", dst)
	}
}
