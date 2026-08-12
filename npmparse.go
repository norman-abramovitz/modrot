package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skipAfterDelim consumes tokens until the composite value whose opening
// delimiter was just read is balanced. Call it immediately after reading an
// opening '[' or '{' that should be discarded rather than walked.
func skipAfterDelim(dec *json.Decoder) error {
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

// depLines walks a JSON document and, for each requested top-level section,
// returns that section object's keys mapped to their 1-based line numbers.
// Sections that are absent or are not objects are omitted from the result.
//
// li must have been built from the same bytes passed as src.
func depLines(src []byte, li *lineIndex, sections []string) (map[string]map[string]int, error) {
	want := make(map[string]bool, len(sections))
	for _, s := range sections {
		want[s] = true
	}
	out := make(map[string]map[string]int)

	dec := json.NewDecoder(bytes.NewReader(src))
	t, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("reading JSON: %w", err)
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected a JSON object at the top level")
	}

	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("reading key: %w", err)
		}
		key, _ := kt.(string)

		if !want[key] {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("skipping %q: %w", key, err)
			}
			continue
		}

		vt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", key, err)
		}
		d, isDelim := vt.(json.Delim)
		if !isDelim {
			continue // scalar or null: Token consumed the whole value
		}
		if d != '{' {
			// An array. Token consumed only the opening bracket, so the
			// rest must be walked to its match. Decode would read just the
			// next element and desync the decoder for every later section.
			if err := skipAfterDelim(dec); err != nil {
				return nil, fmt.Errorf("skipping %q: %w", key, err)
			}
			continue
		}

		entries := make(map[string]int)
		for dec.More() {
			nt, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("reading entry in %q: %w", key, err)
			}
			// InputOffset is the offset just past the key token, so
			// offset-1 is its closing quote — always on the key's own line.
			entries[nt.(string)] = li.Line(int(dec.InputOffset()) - 1)

			var v json.RawMessage
			if err := dec.Decode(&v); err != nil {
				return nil, fmt.Errorf("reading value in %q: %w", key, err)
			}
		}
		if _, err := dec.Token(); err != nil { // closing brace
			return nil, fmt.Errorf("closing %q: %w", key, err)
		}
		// A section key can legally appear more than once; encoding/json
		// merges such objects with the last value winning, so match that
		// rather than dropping the earlier occurrence's lines.
		if prev, ok := out[key]; ok {
			for k, v := range entries {
				prev[k] = v
			}
		} else {
			out[key] = entries
		}
	}
	return out, nil
}

// packageJSON is the subset of package.json modrot reads.
type packageJSON struct {
	Name                 string            `json:"name"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// parsePackageJSON reads package.json and returns the package name plus its
// declared dependencies, each anchored to its line in that file. Versions are
// the declared ranges, not resolved versions; a lockfile supplies those.
//
// dependencies and devDependencies are direct; peerDependencies and
// optionalDependencies are reported but not counted as direct.
// nonRegistryPrefixes are the dependency-specifier protocols that name
// something other than a package on the registry: a sibling directory, a
// workspace member, a tarball, a git checkout, or a package published under a
// different name. The registry has no entry to return for any of them, so
// asking about them produces a 404 that says nothing about the dependency's
// health.
var nonRegistryPrefixes = []string{
	"workspace:", "file:", "link:", "portal:", "patch:", "catalog:",
	"git+", "git:", "https:", "http:",
	"github:", "gitlab:", "bitbucket:",
	// An alias installs a DIFFERENT package under this key, so the key is not
	// the name to ask the registry about. Resolving the alias to its target
	// would require rewriting the module's identity, which would break the
	// package.json line anchor; treating it as non-registry preserves the
	// existing behaviour of not reporting on it, minus the false 404.
	"npm:",
}

// isRegistrySpec reports whether a package.json dependency constraint names a
// package the npm registry is expected to have. Only specifiers that pass are
// looked up, which is what makes a 404 on the rest meaningful.
func isRegistrySpec(spec string) bool {
	s := strings.TrimSpace(spec)
	if s == "" {
		return true // no constraint resolves through dist-tags.latest
	}
	for _, p := range nonRegistryPrefixes {
		if strings.HasPrefix(s, p) {
			return false
		}
	}
	// GitHub shorthand ("owner/repo", optionally "#ref"). A registry range
	// never contains a slash; a scoped NAME does, but this is the constraint,
	// not the name.
	return !strings.Contains(s, "/")
}

func parsePackageJSON(path string) (string, []Module, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from directory discovery
	if err != nil {
		return "", nil, fmt.Errorf("reading package.json: %w", err)
	}

	var pj packageJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return "", nil, fmt.Errorf("parsing package.json: %w", err)
	}

	li := newLineIndex(data)
	sections := []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"}
	lines, err := depLines(data, li, sections)
	if err != nil {
		return "", nil, fmt.Errorf("indexing package.json: %w", err)
	}

	var mods []Module
	add := func(section string, deps map[string]string, direct bool) {
		for name, constraint := range deps {
			mods = append(mods, Module{
				Path:        name,
				Version:     constraint,
				Direct:      direct,
				Line:        lines[section][name],
				LineFile:    "package.json",
				Ecosystem:   "npm",
				NonRegistry: !isRegistrySpec(constraint),
			})
		}
	}
	add("dependencies", pj.Dependencies, true)
	add("devDependencies", pj.DevDependencies, true)
	add("peerDependencies", pj.PeerDependencies, false)
	add("optionalDependencies", pj.OptionalDependencies, false)

	sort.Slice(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return pj.Name, mods, nil
}

// packageLock is the subset of package-lock.json modrot reads. Lockfile v2
// carries both maps; v3 carries only Packages; v1 only Dependencies.
type packageLock struct {
	Packages     map[string]packageLockEntry `json:"packages"`
	Dependencies map[string]packageLockDep   `json:"dependencies"`
}

type packageLockEntry struct {
	Version string `json:"version"`
	Link    bool   `json:"link"`
}

type packageLockDep struct {
	Version      string                    `json:"version"`
	Dependencies map[string]packageLockDep `json:"dependencies"`
}

// nodeModulesMarker separates install-depth segments in package-lock keys.
const nodeModulesMarker = "node_modules/"

// lockPackageName extracts the package name from a package-lock "packages"
// key. Keys are node_modules paths, and nested installs repeat the segment:
//
//	node_modules/xterm                     -> xterm
//	node_modules/@babel/core               -> @babel/core
//	node_modules/a/node_modules/nested     -> nested
//
// The empty key denotes the root project and yields "". Workspace entries
// (e.g. "packages/ui") contain no marker and also yield "", which correctly
// excludes local packages from registry lookups.
func lockPackageName(key string) string {
	idx := strings.LastIndex(key, nodeModulesMarker)
	if idx < 0 {
		return ""
	}
	return key[idx+len(nodeModulesMarker):]
}

// lockKeyDepth counts install-depth segments, so the hoisted top-level copy
// of a package sorts before any nested copy.
func lockKeyDepth(key string) int {
	return strings.Count(key, nodeModulesMarker)
}

// parsePackageLock reads package-lock.json (v1, v2, or v3) and returns the
// resolved package set. Every entry is reported as indirect; parseNPMUnit
// promotes the ones package.json declares. Nested v1 entries carry no line,
// which downstream renders as a file-level location.
func parsePackageLock(path string) ([]Module, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from directory discovery
	if err != nil {
		return nil, fmt.Errorf("reading package-lock.json: %w", err)
	}

	var pl packageLock
	if err := json.Unmarshal(data, &pl); err != nil {
		return nil, fmt.Errorf("parsing package-lock.json: %w", err)
	}

	li := newLineIndex(data)
	lines, err := depLines(data, li, []string{"packages", "dependencies"})
	if err != nil {
		return nil, fmt.Errorf("indexing package-lock.json: %w", err)
	}

	// Deduplicate on name AND version, matching parseBunLock. npm installs
	// several versions of one package at different depths — measured on a
	// real 1868-key lockfile, 196 names carry more than one version — and
	// deprecation is a per-version fact, so collapsing by name alone would
	// silently discard real findings.
	type nameVersion struct{ name, version string }
	var mods []Module
	seen := make(map[nameVersion]bool)
	addMod := func(name, version string, line int) {
		if name == "" {
			return
		}
		nv := nameVersion{name, version}
		if seen[nv] {
			return
		}
		seen[nv] = true
		mods = append(mods, Module{
			Path:      name,
			Version:   version,
			Direct:    false,
			Line:      line,
			LineFile:  "package-lock.json",
			Ecosystem: "npm",
		})
	}

	// Iteration order below is deliberate and load-bearing. npm installs the
	// same package at several depths, and every distinct version is reported
	// — only exact (name, version) duplicates collapse. Ranging over a Go map
	// would vary which copy a survivor takes its line from, and which version
	// lands first, making modrot's output and its SARIF results differ
	// between identical scans. Shallowest install first, so the entry
	// parseNPMUnit resolves a direct dependency to is the hoisted copy.
	if len(pl.Packages) > 0 {
		// v2 and v3: the flat "packages" map is authoritative.
		keys := make([]string, 0, len(pl.Packages))
		for key := range pl.Packages {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			di, dj := lockKeyDepth(keys[i]), lockKeyDepth(keys[j])
			if di != dj {
				return di < dj
			}
			return keys[i] < keys[j]
		})
		for _, key := range keys {
			entry := pl.Packages[key]
			if key == "" || entry.Link {
				continue
			}
			addMod(lockPackageName(key), entry.Version, lines["packages"][key])
		}
	} else {
		// v1: walk the nested "dependencies" tree breadth-first, so every
		// top-level entry is recorded before any nested one. Only top-level
		// entries have a cheaply recoverable line.
		//
		// The frontier is keyed on (name, version), not name alone: npm nests
		// a second copy of a package under the parent that needs a different
		// version, so collapsing the frontier by name would drop that version
		// and — worse — everything reachable only through it.
		level := map[nameVersion]packageLockDep{}
		for name, dep := range pl.Dependencies {
			level[nameVersion{name, dep.Version}] = dep
		}
		topLevel := true
		for len(level) > 0 {
			keys := make([]nameVersion, 0, len(level))
			for nv := range level {
				keys = append(keys, nv)
			}
			sort.Slice(keys, func(i, j int) bool {
				if keys[i].name != keys[j].name {
					return keys[i].name < keys[j].name
				}
				return keys[i].version < keys[j].version
			})

			next := map[nameVersion]packageLockDep{}
			for _, nv := range keys {
				dep := level[nv]
				line := 0
				if topLevel {
					line = lines["dependencies"][nv.name]
				}
				addMod(nv.name, dep.Version, line)
				for childName, child := range dep.Dependencies {
					childKey := nameVersion{childName, child.Version}
					if _, exists := next[childKey]; !exists {
						next[childKey] = child
					}
				}
			}
			level = next
			topLevel = false
		}
	}

	// Stable: ties on Path are real (one package at several versions), and
	// insertion order is shallowest-install-first, which parseNPMUnit relies on.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return mods, nil
}

// bunKeyDepth counts the dependency-chain segments in a bun.lock key.
// Transitive resolutions nest as "<parent>/<child>", and a scoped name
// contributes a single segment despite containing a slash:
//
//	negotiator                            -> 1
//	@angular-devkit/core                  -> 1
//	accepts/negotiator                    -> 2
//	angular-eslint/@angular-devkit/core   -> 2
func bunKeyDepth(key string) int {
	parts := strings.Split(key, "/")
	depth := 0
	for i := 0; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "@") && i+1 < len(parts) {
			i++ // scope and name are one segment
		}
		depth++
	}
	return depth
}

// splitNameVersion splits a bun.lock package specifier into name and version.
// Scoped names begin with @ and contain a second @ before the version, so the
// split is on the last @ rather than the first.
func splitNameVersion(spec string) (name, version string) {
	idx := strings.LastIndex(spec, "@")
	if idx <= 0 { // no @, or a leading @ that is the scope marker
		return spec, ""
	}
	return spec[:idx], spec[idx+1:]
}

// parseBunLock reads bun.lock and returns the resolved package set. The file
// is JSONC, so it is normalized first; StripJSONC preserves byte offsets,
// which keeps the line numbers accurate against the file on disk.
//
// Every entry is reported as indirect; parseNPMUnit promotes the direct ones.
func parseBunLock(path string) ([]Module, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from directory discovery
	if err != nil {
		return nil, fmt.Errorf("reading bun.lock: %w", err)
	}
	data := StripJSONC(raw)

	var bl struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &bl); err != nil {
		return nil, fmt.Errorf("parsing bun.lock: %w", err)
	}

	li := newLineIndex(data)
	lines, err := depLines(data, li, []string{"packages"})
	if err != nil {
		return nil, fmt.Errorf("indexing bun.lock: %w", err)
	}

	// Walk keys in a fixed order. bun.lock nests transitive resolutions as
	// "<parent>/<child>", so one package can appear under several keys —
	// measured on a real 1841-entry lockfile, 300 keys are exact duplicates.
	// Which key is kept decides which line the finding anchors to, and
	// ranging over a map would pick a different one on each run, making
	// SARIF startLine values flap between identical scans. Shallowest wins.
	keys := make([]string, 0, len(bl.Packages))
	for key := range bl.Packages {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		di, dj := bunKeyDepth(keys[i]), bunKeyDepth(keys[j])
		if di != dj {
			return di < dj
		}
		return keys[i] < keys[j]
	})

	// Deduplicate on name AND version, never on name alone. A real lockfile
	// legitimately resolves several versions of the same package (measured:
	// 218 such packages in that same file, e.g. @babel/code-frame at 8.0.0,
	// 7.29.0 and 7.29.7). Deprecation is a per-version fact, so collapsing
	// to one version per name would silently discard real findings.
	type nameVersion struct{ name, version string }
	seen := make(map[nameVersion]bool, len(keys))

	var mods []Module
	for _, key := range keys {
		var entry []json.RawMessage
		if err := json.Unmarshal(bl.Packages[key], &entry); err != nil || len(entry) == 0 {
			continue
		}
		var spec string
		if err := json.Unmarshal(entry[0], &spec); err != nil {
			continue
		}
		name, version := splitNameVersion(spec)
		if name == "" {
			name = key
		}
		nv := nameVersion{name, version}
		if seen[nv] {
			continue
		}
		seen[nv] = true
		mods = append(mods, Module{
			Path:      name,
			Version:   version,
			Direct:    false,
			Line:      lines["packages"][key],
			LineFile:  "bun.lock",
			Ecosystem: "npm",
		})
	}

	// Stable: ties on Path are real (one package at several versions), and
	// insertion order is shallowest-install-first, which parseNPMUnit relies on.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	return mods, nil
}

// ParseResult is one manifest unit's parsed contents. A unit is one primary
// manifest plus any companion files: go.mod alone, or package.json plus at
// most one lockfile.
type ParseResult struct {
	Name     string   // go.mod module path, or package.json "name"
	Primary  string   // base name of the primary manifest
	Files    []string // base names actually read, in read order
	Modules  []Module
	Unlocked bool // npm only: no lockfile, versions come from dist-tags.latest
}

// parseNPMUnit assembles one npm manifest unit from dir. It returns
// (nil, nil) when dir holds no package.json.
//
// package.json defines which packages are direct. A lockfile, when present,
// supplies resolved versions and contributes the transitive packages. Direct
// packages keep their package.json line so the user is sent to the file they
// would actually edit; transitive packages anchor to the lockfile, the only
// file that mentions them.
func parseNPMUnit(dir string) (*ParseResult, error) {
	pkgPath := filepath.Join(dir, "package.json")
	if _, err := os.Stat(pkgPath); err != nil {
		return nil, nil //nolint:nilnil // no package.json means no npm unit here
	}

	name, directMods, err := parsePackageJSON(pkgPath)
	if err != nil {
		return nil, err
	}

	res := &ParseResult{
		Name:    name,
		Primary: "package.json",
		Files:   []string{"package.json"},
	}

	lockName, lockMods := loadNPMLockfile(dir)
	if lockName == "" {
		// No lockfile, so package.json's constraint is a RANGE ("^5.3.0"),
		// not a version. The registry's per-version endpoint 404s on a range,
		// and the client reads a 404 as a definitive "no such version" — so
		// leaving the range here makes every dependency silently vanish and
		// produces a false all-clear. Clear it, so the resolve phase asks for
		// dist-tags.latest and writes the real resolved version back.
		res.Unlocked = true
		for i := range directMods {
			directMods[i].Version = ""
		}
		res.Modules = directMods
		return res, nil
	}
	res.Files = append(res.Files, lockName)

	// First occurrence wins. The lockfile parsers emit shallowest-install
	// first and sort stably, so the first entry for a name is the hoisted
	// copy — the version a direct dependency actually resolves to. Taking
	// the last would pick an arbitrary nested version instead.
	resolved := make(map[string]string, len(lockMods))
	for _, m := range lockMods {
		if _, ok := resolved[m.Path]; !ok {
			resolved[m.Path] = m.Version
		}
	}

	for i := range directMods {
		// A declared dependency the lockfile does not resolve still carries
		// its RANGE. The registry's per-version endpoint 404s on a range,
		// which the client reads as a definitive "no such package" and
		// caches — so the dependency vanishes with no warning. Clear it, so
		// resolve asks for dist-tags.latest exactly as the unlocked path does.
		if v, ok := resolved[directMods[i].Path]; ok && v != "" {
			directMods[i].Version = v
		} else {
			directMods[i].Version = ""
		}
	}

	// Skip only the exact version that was promoted to direct. Another
	// version of the same package, pulled in transitively by something else,
	// is a genuine entry in its own right — and since deprecation is a
	// per-version fact, dropping it by name would lose real findings, the
	// same collapse the lockfile parsers deliberately avoid.
	directVersion := make(map[string]string, len(directMods))
	for _, m := range directMods {
		directVersion[m.Path] = m.Version
	}

	mods := directMods
	for _, m := range lockMods {
		if v, ok := directVersion[m.Path]; ok && v == m.Version {
			continue // already present, anchored to package.json
		}
		mods = append(mods, m)
	}
	// Stable: ties on Path are real (one package at several versions), and
	// insertion order is shallowest-install-first, which parseNPMUnit relies on.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].Path < mods[j].Path })
	res.Modules = mods
	return res, nil
}

// loadNPMLockfile picks and parses the lockfile beside package.json. bun.lock
// wins when both are present, since a repo carrying both is normally a bun
// repo with a stale npm lockfile. A lockfile that fails to parse degrades to
// the unlocked path rather than failing the scan.
func loadNPMLockfile(dir string) (string, []Module) {
	bunPath := filepath.Join(dir, "bun.lock")
	npmPath := filepath.Join(dir, "package-lock.json")

	_, bunErr := os.Stat(bunPath)
	_, npmErr := os.Stat(npmPath)
	hasBun, hasNPM := bunErr == nil, npmErr == nil

	if hasBun && hasNPM {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: %s has both bun.lock and package-lock.json; using bun.lock\n", dir)
	}
	if !hasBun && !hasNPM {
		if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil {
			_, _ = fmt.Fprintf(os.Stderr,
				"Warning: %s has a binary bun.lockb, which modrot cannot read; run 'bun install --save-text-lockfile' to generate bun.lock\n", dir)
		}
		return "", nil
	}

	name, path := "bun.lock", bunPath
	parse := parseBunLock
	if !hasBun {
		name, path = "package-lock.json", npmPath
		parse = parsePackageLock
	}

	mods, err := parse(path)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v; falling back to package.json alone\n", name, err)
		return "", nil
	}
	return name, mods
}
