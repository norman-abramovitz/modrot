package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FileMatch represents a source file that imports an archived module.
type FileMatch struct {
	File       string // relative path from project root
	Line       int    // line number of the import
	ImportPath string // full import path found in source
}

// ScanImports uses rg (ripgrep) to find Go source files that import any of
// the given module paths. It returns a map from module path to the list of
// file matches. Modules with no imports in the project are omitted from the map.
func ScanImports(projectDir string, modulePaths []string) (map[string][]FileMatch, error) {
	if len(modulePaths) == 0 {
		return nil, nil
	}

	// Check that rg is available
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, fmt.Errorf("rg (ripgrep) is required for --files; install from https://github.com/BurntSushi/ripgrep")
	}

	pattern := buildImportPattern(modulePaths)

	// Run rg in one pass over all .go files, excluding vendor/
	cmd := exec.Command("rg", "-n", "--no-heading",
		"--glob", "*.go",
		"--glob", "!vendor/",
		"-e", pattern,
		projectDir,
	)
	out, err := cmd.Output()
	if err != nil {
		// rg exits 1 when no matches found — that's fine
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return map[string][]FileMatch{}, nil
		}
		return nil, fmt.Errorf("running rg: %w", err)
	}

	return parseRgOutput(string(out), projectDir, modulePaths), nil
}

// buildImportPattern constructs a regex that matches import lines containing
// any of the given module paths. It matches both exact imports and subpackage
// imports (e.g., "github.com/foo/bar" and "github.com/foo/bar/sub").
func buildImportPattern(modulePaths []string) string {
	// Escape dots in module paths for regex
	escaped := make([]string, len(modulePaths))
	for i, p := range modulePaths {
		escaped[i] = regexp.QuoteMeta(p)
	}
	// Match the module path followed by either a quote (exact) or slash (subpackage)
	return `"(` + strings.Join(escaped, "|") + `)(/|")`
}

// parseRgOutput parses ripgrep output lines (file:line:content) and maps
// each import back to its archived module using longest-prefix matching.
func parseRgOutput(output, projectDir string, modulePaths []string) map[string][]FileMatch {
	results := make(map[string][]FileMatch)

	// Sort module paths longest-first for longest-prefix matching
	sorted := make([]string, len(modulePaths))
	copy(sorted, modulePaths)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})

	// Compile a regex to extract the import path from a Go import line
	importRe := regexp.MustCompile(`"([^"]+)"`)

	// Normalize projectDir to end with /
	if !strings.HasSuffix(projectDir, "/") {
		projectDir += "/"
	}

	// One physical line can carry more than one import. `import _ "a"; import
	// _ "b"` is legal Go, and a factored import block can pair a path with a
	// quoted comment. Taking only the first quoted path drops the rest
	// silently — and if that first path is not one we were asked about, a real
	// match on the line vanishes with no signal at all.
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		// Parse "file:line:content"
		file, lineNum, content, ok := parseRgLine(line)
		if !ok {
			continue
		}

		// Make file path relative to project dir
		relFile := strings.TrimPrefix(file, projectDir)

		for _, match := range importRe.FindAllStringSubmatch(content, -1) {
			importPath := match[1]

			// Find the module this import belongs to (longest-prefix match)
			modulePath := matchModule(importPath, sorted)
			if modulePath == "" {
				continue
			}

			// Distinct subpath imports on one line are distinct findings; the
			// same import path twice on one line is not.
			key := modulePath + "\x00" + relFile + "\x00" + strconv.Itoa(lineNum) + "\x00" + importPath
			if seen[key] {
				continue
			}
			seen[key] = true

			results[modulePath] = append(results[modulePath], FileMatch{
				File:       relFile,
				Line:       lineNum,
				ImportPath: importPath,
			})
		}
	}

	// Sort matches within each module by file then line
	for mod := range results {
		sort.Slice(results[mod], func(i, j int) bool {
			if results[mod][i].File != results[mod][j].File {
				return results[mod][i].File < results[mod][j].File
			}
			return results[mod][i].Line < results[mod][j].Line
		})
	}

	return results
}

// parseRgLine splits an rg output line into file, line number, and content.
// Returns ok=false if the line can't be parsed.
func parseRgLine(line string) (file string, lineNum int, content string, ok bool) {
	// Format: "file:line:content"
	// File paths may contain colons (e.g., on Windows), but we're on Unix
	// and line numbers are always numeric, so split on first two colons.
	first := strings.IndexByte(line, ':')
	if first < 0 {
		return "", 0, "", false
	}
	rest := line[first+1:]
	second := strings.IndexByte(rest, ':')
	if second < 0 {
		return "", 0, "", false
	}

	file = line[:first]
	n, err := strconv.Atoi(rest[:second])
	if err != nil {
		return "", 0, "", false
	}
	return file, n, rest[second+1:], true
}

// matchModule finds which module path the given import belongs to using
// longest-prefix matching. modulePaths must be sorted longest-first.
func matchModule(importPath string, modulePaths []string) string {
	for _, mod := range modulePaths {
		if importPath == mod || strings.HasPrefix(importPath, mod+"/") {
			return mod
		}
	}
	return ""
}
