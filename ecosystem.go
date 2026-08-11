package main

import (
	"fmt"
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

// buildManifestInfos parses every unit in the given directories, in ecosystem
// order within each directory. Units that fail to parse are reported and
// skipped rather than aborting the scan.
func buildManifestInfos(dirs []string, rootDir string) []manifestInfo {
	var units []manifestInfo
	for _, dir := range dirs {
		for _, eco := range discoverUnits(dir) {
			res, err := eco.Parse(dir)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: skipping %s in %s: %v\n",
					primaryManifest(eco), dir, err)
				continue
			}
			if res == nil {
				continue
			}
			manifestPath := filepath.Join(dir, res.Primary)
			rel, relErr := filepath.Rel(rootDir, manifestPath)
			if relErr != nil {
				rel = manifestPath
			}
			units = append(units, manifestInfo{
				eco:          eco,
				manifestPath: manifestPath,
				files:        res.Files,
				relPath:      rel,
				moduleName:   res.Name,
				unlocked:     res.Unlocked,
				allModules:   res.Modules,
			})
		}
	}
	return units
}

// depKey identifies a dependency for deduplication across manifests.
type depKey struct{ path, version string }

// enrichUnits runs the resolve and deprecation phases per ecosystem.
//
// Dependencies are deduplicated by path and version across every unit before
// any lookup, so a dependency shared by several manifests costs one network
// round trip rather than one per manifest. This is the behavior the
// per-ecosystem across-modules helpers used to provide; centralizing it here
// gives both ecosystems the same guarantee from one implementation.
//
// Resolve runs before deprecations: for an unlocked npm unit it fills in the
// version that the deprecation lookup then keys on.
func enrichUnits(units []manifestInfo, cfg *Config) {
	byEco := map[string][]int{}
	for i := range units {
		byEco[units[i].eco.Name] = append(byEco[units[i].eco.Name], i)
	}

	for _, eco := range ecosystems {
		idxs := byEco[eco.Name]
		if len(idxs) == 0 {
			continue
		}

		// One representative Module per distinct dependency, plus every
		// location it occurs at so results can be fanned back out.
		var unique []Module
		var keys []depKey
		locations := map[depKey][][2]int{} // key -> []{unit index, module index}
		for _, ui := range idxs {
			for mi := range units[ui].allModules {
				m := units[ui].allModules[mi]
				k := depKey{m.Path, m.Version}
				if _, seen := locations[k]; !seen {
					unique = append(unique, m)
					keys = append(keys, k)
				}
				locations[k] = append(locations[k], [2]int{ui, mi})
			}
		}
		if len(unique) == 0 {
			continue
		}

		if cfg.Resolve || eco.Name == "npm" {
			// npm always resolves: repository.url is its only route to a repo.
			if n := eco.Resolve(unique); n > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Resolved %d %s %s to GitHub repos.\n",
					n, eco.Name, pluralize(n, "dependency", "dependencies"))
			}
		}
		if cfg.Deprecated {
			if n := eco.Deprecations(unique); n > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Found %d deprecated %s %s.\n",
					n, eco.Name, pluralize(n, "package", "packages"))
			}
		}

		// Copy only the enriched fields. Line, LineFile, Direct, and
		// Ecosystem are per-location and must survive: the same dependency
		// can be direct in one manifest and transitive in another, at
		// different lines in different files.
		for i, m := range unique {
			for _, loc := range locations[keys[i]] {
				dst := &units[loc[0]].allModules[loc[1]]
				dst.Version = m.Version
				dst.Owner = m.Owner
				dst.Repo = m.Repo
				dst.Deprecated = m.Deprecated
				dst.SourceURL = m.SourceURL
			}
		}
	}
}
