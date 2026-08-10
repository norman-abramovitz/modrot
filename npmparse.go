package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

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
		if d, ok := vt.(json.Delim); !ok || d != '{' {
			// Not an object — consume whatever it is and move on.
			if _, ok := vt.(json.Delim); ok {
				var skip json.RawMessage
				_ = dec.Decode(&skip)
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
		out[key] = entries
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
				Path:      name,
				Version:   constraint,
				Direct:    direct,
				Line:      lines[section][name],
				LineFile:  "package.json",
				Ecosystem: "npm",
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
