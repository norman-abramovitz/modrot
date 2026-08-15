package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Name      string   // "go" or "npm"
	Manifests []string // primary manifest first, then companions
	Parse     func(dir string) (*ParseResult, error)
	// Resolve and Deprecations each return (count, failed): what they found,
	// and how many dependencies their upstream could not be consulted about.
	// The failure count is what stops an outage reading as a clean scan.
	Resolve      func(modules []Module) (int, int)
	Deprecations func(modules []Module) (int, int)
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
	Resolve:      func(m []Module) (int, int) { return ResolveVanityImports(m, 20) },
	Deprecations: func(m []Module) (int, int) { return CheckDeprecations(m, 20) },
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
// skipped rather than aborting the scan, so one corrupt manifest cannot kill
// a whole monorepo scan.
//
// The second return value counts manifests that were found but could not be
// parsed. Callers need it to tell "there is nothing here" apart from "there
// is something here and it is broken" — reporting the former for the latter
// tells the user a file they are looking at does not exist.
func buildManifestInfos(dirs []string, rootDir string) ([]manifestInfo, int) {
	var units []manifestInfo
	failed := 0
	for _, dir := range dirs {
		for _, eco := range discoverUnits(dir) {
			res, err := eco.Parse(dir)
			if err != nil {
				failed++
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
	// WalkDir visits a directory before its entries, so units arrive
	// root-first; the pre-npm walk appended when it reached the go.mod FILE,
	// which yields path order. Sort to restore that order byte-for-byte and
	// to interleave npm units by path rather than by discovery accident —
	// unit order is user-visible in text, --json modules[] and --sarif
	// results[], where the first location anchors a code-scanning alert.
	sort.SliceStable(units, func(i, j int) bool { return units[i].manifestPath < units[j].manifestPath })
	return units, failed
}

// unitQualifier names what a unit is built with: the Go toolchain for a Go
// unit, the ecosystem (plus its lockfile state) for anything else. Stamping a
// Go toolchain onto an npm unit would be a lie in every output format, so the
// header and the JSON field share this one answer.
func unitQualifier(mi manifestInfo, cfg *Config) string {
	if mi.eco.Name == "go" {
		return cfg.GoToolchain
	}
	qualifier := mi.eco.Name
	if mi.unlocked {
		qualifier += ", unlocked"
	}
	return qualifier
}

// unitHeader renders the per-unit banner: which manifest, which project, and
// which toolchain or ecosystem it belongs to.
func unitHeader(mi manifestInfo, cfg *Config) string {
	return fmt.Sprintf("%s — %s (%s)", filepath.ToSlash(mi.relPath), mi.moduleName, unitQualifier(mi, cfg))
}

// warnUnsupported reports the flags this unit's ecosystem cannot honor, once
// per unit, so the user is never left wondering why a section is missing.
func warnUnsupported(mi manifestInfo, cfg *Config) {
	switch {
	case cfg.OutputFormat == "mermaid" && mi.eco.Graph == nil:
		// A Mermaid document is a graph and nothing else: with no graph
		// source there is no flat form to fall back to, so the unit is
		// omitted rather than printed as a table into the diagram.
		_, _ = fmt.Fprintf(os.Stderr,
			"Warning: --mermaid is not supported for %s (no dependency graph source); skipping %s\n",
			mi.eco.Name, mi.relPath)
	case cfg.Tree && mi.eco.Graph == nil:
		_, _ = fmt.Fprintf(os.Stderr,
			"Warning: --tree is not supported for %s (no dependency graph source); showing flat output for %s\n",
			mi.eco.Name, mi.relPath)
	}
	if (cfg.Freshness || cfg.Age.Enabled) && mi.eco.Name == "npm" {
		_, _ = fmt.Fprintf(os.Stderr,
			"Warning: --freshness and --age are not supported for npm (they require full registry packuments); skipping for %s\n",
			mi.relPath)
	}
	if mi.unlocked {
		_, _ = fmt.Fprintf(os.Stderr,
			"Note: %s has no lockfile; versions resolved to dist-tags.latest\n", mi.relPath)
	}
}

// reportNoUnits explains an empty scan. A manifest that exists but failed to
// parse must not be reported as a missing manifest.
func reportNoUnits(dir string, failed int) int {
	if failed > 0 {
		_, _ = fmt.Fprintf(os.Stderr,
			"Error: no manifest in %s could be parsed (%d %s failed)\n",
			dir, failed, pluralize(failed, "manifest", "manifests"))
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "No go.mod or package.json found in %s\n", dir)
	}
	return 2
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
			n, failed := eco.Resolve(unique)
			cfg.IncompleteLookups += failed
			if n > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Resolved %d %s %s to GitHub repos.\n",
					n, eco.Name, pluralize(n, "dependency", "dependencies"))
			}
		}
		if cfg.Deprecated {
			n, failed := eco.Deprecations(unique)
			cfg.IncompleteLookups += failed
			if n > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Found %d deprecated %s %s.\n",
					n, eco.Name, pluralize(n, "package", "packages"))
			}
		}

		for i, m := range unique {
			for _, loc := range locations[keys[i]] {
				applyEnriched(&units[loc[0]].allModules[loc[1]], m)
			}
		}
	}
}

// applyEnriched copies the fields an enrichment pass produces from a
// representative module onto one of its locations.
//
// Only enriched fields are copied. Line, LineFile, Direct and Ecosystem are
// per-location and must survive: the same dependency can be direct in one
// manifest and transitive in another, at different lines in different files.
//
// Every field an enrichment pass writes has to be listed here. A pass that
// writes a field this function does not copy will appear to work — the value
// is set on the representative — while nothing reaches output, which is how
// --age once came back blank for every GitHub-hosted module.
func applyEnriched(dst *Module, src Module) {
	dst.Version = src.Version
	dst.VersionInferred = src.VersionInferred
	dst.Owner = src.Owner
	dst.Repo = src.Repo
	dst.Deprecated = src.Deprecated
	dst.SourceURL = src.SourceURL
}
