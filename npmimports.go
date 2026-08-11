package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// npmSourceGlobs lists the file types worth scanning for npm imports, and the
// generated directories worth skipping.
var npmSourceGlobs = []string{
	"*.js", "*.jsx", "*.ts", "*.tsx", "*.mjs", "*.cjs", "*.vue", "*.svelte",
	"!node_modules/", "!dist/", "!build/", "!*.min.js",
}

// npmSpecRe extracts the imported specifier from a matched line.
var npmSpecRe = regexp.MustCompile(`(?:from|require\s*\(|import\s*\(|import)\s*['"]([^'"]+)['"]`)

// buildNPMImportPattern constructs a regex matching the import forms that can
// name a package: ES imports (including `import type`), bare side-effect
// imports, re-exports, CommonJS require, and dynamic import.
//
// `import\s*\(` must precede the bare `import` alternative: dynamic import has
// a parenthesis where the bare form has whitespace, so a leading bare `import`
// would match and then fail on the '(', missing every dynamic import. Matching requires the package name to be followed by a
// closing quote or a subpath separator, so a search for "foo" never matches
// "foo-bar".
func buildNPMImportPattern(pkgNames []string) string {
	escaped := make([]string, len(pkgNames))
	for i, p := range pkgNames {
		escaped[i] = regexp.QuoteMeta(p)
	}
	return `(?:from|require\s*\(|import\s*\(|import)\s*['"](?:` +
		strings.Join(escaped, "|") + `)(?:/[^'"]*)?['"]`
}

// ScanNPMImports uses rg (ripgrep) to find JavaScript and TypeScript source
// files that import any of the given packages. It returns a map from package
// name to file matches; packages with no imports are omitted.
func ScanNPMImports(projectDir string, pkgNames []string) (map[string][]FileMatch, error) {
	if len(pkgNames) == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, fmt.Errorf("rg (ripgrep) is required for --files; install from https://github.com/BurntSushi/ripgrep")
	}

	args := []string{"-n", "--no-heading"}
	for _, g := range npmSourceGlobs {
		args = append(args, "--glob", g)
	}
	args = append(args, "-e", buildNPMImportPattern(pkgNames), projectDir)

	out, err := exec.Command("rg", args...).Output() // #nosec G204
	if err != nil {
		// rg exits 1 when there are no matches, which is not an error here.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return map[string][]FileMatch{}, nil
		}
		return nil, fmt.Errorf("running rg: %w", err)
	}
	return parseNPMRgOutput(string(out), projectDir, pkgNames), nil
}

// parseNPMRgOutput turns ripgrep output into per-package file matches, mapping
// each specifier back to its package by longest-prefix match.
func parseNPMRgOutput(output, projectDir string, pkgNames []string) map[string][]FileMatch {
	results := make(map[string][]FileMatch)

	sorted := make([]string, len(pkgNames))
	copy(sorted, pkgNames)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })

	if !strings.HasSuffix(projectDir, "/") {
		projectDir += "/"
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		file, lineNum, content, ok := parseRgLine(scanner.Text())
		if !ok {
			continue
		}
		match := npmSpecRe.FindStringSubmatch(content)
		if match == nil {
			continue
		}
		spec := match[1]
		pkg := matchModule(spec, sorted)
		if pkg == "" {
			continue
		}
		results[pkg] = append(results[pkg], FileMatch{
			File:       strings.TrimPrefix(file, projectDir),
			Line:       lineNum,
			ImportPath: spec,
		})
	}

	for pkg := range results {
		sort.Slice(results[pkg], func(i, j int) bool {
			if results[pkg][i].File != results[pkg][j].File {
				return results[pkg][i].File < results[pkg][j].File
			}
			return results[pkg][i].Line < results[pkg][j].Line
		})
	}
	return results
}
