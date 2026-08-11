package main

import (
	"os"
	"path/filepath"
	"strings"
)

// An Ecosystem supplies the per-language half of a scan: how to find and read
// a manifest, how to map a dependency to a GitHub repo, how to learn that a
// release is deprecated, and how to find the source files that use it.
// Everything downstream — RepoStatus, the ignore list, and every output
// format — is shared.
//
// A nil capability means the ecosystem does not support it. Graph is nil for
// npm, which is what makes --tree report itself unsupported rather than
// needing a branch at the call site.
type Ecosystem struct {
	Name         string   // "go" or "npm"
	Manifests    []string // primary manifest first, then companions
	Parse        func(dir string) (*ParseResult, error)
	Resolve      func(modules []Module) int
	Deprecations func(modules []Module) int
	ScanImports  func(dir string, names []string) (map[string][]FileMatch, error)
	Graph        func(dir, goVersionOverride string) (map[string][]string, error)
}

// parseGoUnit reads the go.mod in dir into the shared ParseResult shape.
func parseGoUnit(dir string) (*ParseResult, error) {
	path := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(path); err != nil {
		return nil, nil //nolint:nilnil // no go.mod means no Go unit here
	}
	mods, err := ParseGoMod(path)
	if err != nil {
		return nil, err
	}
	name, _ := ModuleName(path)
	return &ParseResult{
		Name:    name,
		Primary: "go.mod",
		Files:   []string{"go.mod"},
		Modules: mods,
	}, nil
}

var goEcosystem = &Ecosystem{
	Name:         "go",
	Manifests:    []string{"go.mod"},
	Parse:        parseGoUnit,
	Resolve:      func(m []Module) int { return ResolveVanityImports(m, 20) },
	Deprecations: func(m []Module) int { return CheckDeprecations(m, 20) },
	ScanImports:  ScanImports,
	Graph:        parseModGraph,
}

var npmEcosystem = &Ecosystem{
	Name:         "npm",
	Manifests:    []string{"package.json", "package-lock.json", "bun.lock"},
	Parse:        parseNPMUnit,
	Resolve:      ResolveNPM,
	Deprecations: CheckNPMDeprecations,
	ScanImports:  ScanNPMImports,
	Graph:        nil, // no dependency graph source; --tree reports unsupported
}

// ecosystems lists every supported ecosystem, in reporting order.
var ecosystems = []*Ecosystem{goEcosystem, npmEcosystem}

// primaryManifest returns the manifest whose presence marks a unit.
func primaryManifest(eco *Ecosystem) string {
	return eco.Manifests[0]
}

// discoverUnits returns the ecosystems whose primary manifest exists in dir,
// in the order declared by ecosystems. A lockfile without its package.json is
// not a unit.
func discoverUnits(dir string) []*Ecosystem {
	var found []*Ecosystem
	for _, eco := range ecosystems {
		if _, err := os.Stat(filepath.Join(dir, primaryManifest(eco))); err == nil {
			found = append(found, eco)
		}
	}
	return found
}

// skipDir reports whether a directory should be excluded from a recursive
// walk: dependency caches, vendored code, test fixtures, and dotfiles.
func skipDir(name string) bool {
	switch name {
	case "vendor", "testdata", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}

// findUnitDirs walks the tree rooted at root and returns every directory
// holding at least one primary manifest.
func findUnitDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if len(discoverUnits(path)) > 0 {
			dirs = append(dirs, path)
		}
		return nil
	})
	return dirs, err
}
