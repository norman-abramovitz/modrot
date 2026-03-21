package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runConfig holds parsed flag values for runRecursive.
type runConfig struct {
	outputFormat   string // "table", "json", "markdown", "mermaid", "quickfix"
	showAll        bool
	directOnly     bool
	workers        int
	treeMode       bool
	filesMode      bool
	resolveMode    bool
	deprecatedMode bool
	freshnessMode  bool
	ageMode        bool
	goVersion      string
	goToolchain    string // e.g. "go1.23.4" from `go version`
	durationMode   bool
	durationDate   time.Time
	sortMode       string // e.g. "name", "pushed:desc"
	ignoreFile     string
	ignoreInline   string
}

// findGoModFiles walks the directory tree rooted at dir and returns
// paths to all go.mod files found. It skips vendor/, testdata/, and
// hidden directories (names starting with ".").
func findGoModFiles(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "testdata" || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

// applyStatus maps GitHub archive status from a global lookup onto
// a set of modules from a specific go.mod file.
func applyStatus(modules []Module, statusMap map[string]RepoStatus) []RepoStatus {
	results := make([]RepoStatus, len(modules))
	for i, m := range modules {
		rs := RepoStatus{Module: m}
		key := m.Owner + "/" + m.Repo
		if global, ok := statusMap[key]; ok {
			rs.IsArchived = global.IsArchived
			rs.ArchivedAt = global.ArchivedAt
			rs.PushedAt = global.PushedAt
			rs.NotFound = global.NotFound
			rs.Error = global.Error
		}
		results[i] = rs
	}
	return results
}

// getArchivedPaths returns module paths for archived results.
func getArchivedPaths(results []RepoStatus) []string {
	var paths []string
	for _, r := range results {
		if r.IsArchived {
			paths = append(paths, r.Module.Path)
		}
	}
	return paths
}

// moduleInfo holds parsed data for a single go.mod file.
type moduleInfo struct {
	gomodPath     string
	relPath       string
	moduleName    string
	allModules    []Module
	githubModules []Module
	nonGHModules  []Module
}

// getDeprecatedModules returns modules with non-empty Deprecated field,
// respecting the directOnly filter. Returns nil if deprecatedMode is false.
func getDeprecatedModules(allModules []Module, directOnly bool, deprecatedMode bool) []Module {
	if !deprecatedMode {
		return nil
	}
	var result []Module
	for _, m := range allModules {
		if m.Deprecated == "" {
			continue
		}
		if directOnly && !m.Direct {
			continue
		}
		result = append(result, m)
	}
	return result
}

// runRecursive scans a directory tree for go.mod files, queries GitHub
// once for all unique repos, and outputs per-module results.
// Returns the exit code (0 = clean, 1 = archived found, 2 = error).
func runRecursive(rootDir string, cfg runConfig) int {
	gomodPaths, err := findGoModFiles(rootDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		return 2
	}
	if len(gomodPaths) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No go.mod files found in %s\n", rootDir)
		return 2
	}

	// Phase 1: Parse all go.mod files
	var modules []moduleInfo
	for _, gp := range gomodPaths {
		allMods, err := ParseGoMod(gp)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", gp, err)
			continue
		}
		modName, _ := ModuleName(gp)
		rel, _ := filepath.Rel(rootDir, gp)
		modules = append(modules, moduleInfo{
			gomodPath:  gp,
			relPath:    rel,
			moduleName: modName,
			allModules: allMods,
		})
	}

	// Phase 2: Resolve vanity imports (before filtering)
	if cfg.resolveMode {
		resolved := resolveAcrossModules(modules)
		if resolved > 0 {
			_, _ = fmt.Fprintf(os.Stderr, "Resolved %d non-GitHub modules to GitHub repos.\n", resolved)
		}
	}

	// Phase 2.5: Check deprecations (before filtering)
	if cfg.deprecatedMode {
		count := checkDeprecationsAcrossModules(modules)
		if count > 0 {
			_, _ = fmt.Fprintf(os.Stderr, "Found %d deprecated %s.\n", count, pluralize(count, "module", "modules"))
		}
	}

	// Phase 3: Filter to GitHub modules and collect globally unique repos
	var allGitHub []Module
	globalSeen := make(map[string]bool)
	for i := range modules {
		ghMods, nonGH := FilterGitHub(modules[i].allModules, cfg.directOnly)
		modules[i].githubModules = ghMods
		modules[i].nonGHModules = nonGH

		for _, m := range ghMods {
			key := m.Owner + "/" + m.Repo
			if !globalSeen[key] {
				globalSeen[key] = true
				allGitHub = append(allGitHub, m)
			}
		}
	}

	// Phase 3.5: Enrich non-GitHub modules with proxy data
	enrichAcrossModules(modules)

	// Phase 3.6: Enrich all modules with freshness data (skips already-enriched)
	if cfg.freshnessMode {
		enrichFreshnessAcrossModules(modules)
	}

	if len(modules) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No valid go.mod files found.\n")
		return 2
	}

	if len(allGitHub) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No GitHub modules found across %d go.mod files.\n", len(modules))
		return 0
	}

	_, _ = fmt.Fprintf(os.Stderr, "Found %d go.mod files, checking %d unique GitHub repos...\n", len(modules), len(allGitHub))

	// Query GitHub once for all unique repos
	globalResults, err := CheckRepos(allGitHub, cfg.workers)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	// Build status map: owner/repo → RepoStatus
	statusMap := make(map[string]RepoStatus)
	for _, r := range globalResults {
		statusMap[r.Module.Owner+"/"+r.Module.Repo] = r
	}

	hasAnyArchived := false

	switch cfg.outputFormat {
	case "quickfix":
		hasAnyArchived = runRecursiveQuickfix(modules, statusMap, cfg)
	case "json":
		hasAnyArchived = runRecursiveJSON(modules, statusMap, cfg)
	case "markdown":
		hasAnyArchived = runRecursiveMarkdown(modules, statusMap, cfg)
	default:
		hasAnyArchived = runRecursiveText(modules, statusMap, cfg)
	}

	if hasAnyArchived {
		return 1
	}
	return 0
}

// runRecursiveQuickfix outputs quickfix-format lines across all modules.
func runRecursiveQuickfix(modules []moduleInfo, statusMap map[string]RepoStatus, cfg runConfig) bool {
	hasAnyArchived := false

	for _, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		il := BuildIgnoreList(filepath.Dir(mi.gomodPath), cfg.ignoreFile, cfg.ignoreInline)
		if il.Len() > 0 {
			results, _ = il.FilterResults(results)
		}

		archivedPaths := getArchivedPaths(results)
		if len(archivedPaths) > 0 {
			hasAnyArchived = true
			fm, err := ScanImports(filepath.Dir(mi.gomodPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				continue
			}
			PrintFilesPlain(results, fm)
		}
	}

	return hasAnyArchived
}

// runRecursiveJSON outputs recursive results as a single JSON document.
func runRecursiveJSON(modules []moduleInfo, statusMap map[string]RepoStatus, cfg runConfig) bool {
	hasAnyArchived := false

	if cfg.treeMode {
		out := RecursiveJSONTreeOutput{Modules: []RecursiveJSONTreeEntry{}}

		for _, mi := range modules {
			results := applyStatus(mi.githubModules, statusMap)

			// Apply ignore list
			il := BuildIgnoreList(filepath.Dir(mi.gomodPath), cfg.ignoreFile, cfg.ignoreInline)
			if il.Len() > 0 {
				results, _ = il.FilterResults(results)
			}

			archivedPaths := getArchivedPaths(results)
			if len(archivedPaths) > 0 {
				hasAnyArchived = true
			}

			var fileMatches map[string][]FileMatch
			if cfg.filesMode && len(archivedPaths) > 0 {
				fm, err := ScanImports(filepath.Dir(mi.gomodPath), archivedPaths)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				} else {
					fileMatches = fm
				}
			}

			graph, err := parseModGraph(filepath.Dir(mi.gomodPath), cfg.goVersion)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph for %s: %v\n", mi.relPath, err)
				graph = map[string][]string{}
			}

			deprecatedModules := getDeprecatedModules(mi.allModules, cfg.directOnly, cfg.deprecatedMode)
			treeOut := buildTreeJSONOutput(results, graph, mi.allModules, fileMatches, mi.nonGHModules, deprecatedModules)
			out.Modules = append(out.Modules, RecursiveJSONTreeEntry{
				GoMod:          mi.relPath,
				ModulePath:     mi.moduleName,
				GoVersion:      cfg.goToolchain,
				JSONTreeOutput: treeOut,
			})
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		out := RecursiveJSONOutput{Modules: []RecursiveJSONEntry{}}

		for _, mi := range modules {
			results := applyStatus(mi.githubModules, statusMap)

			// Apply ignore list
			il := BuildIgnoreList(filepath.Dir(mi.gomodPath), cfg.ignoreFile, cfg.ignoreInline)
			if il.Len() > 0 {
				results, _ = il.FilterResults(results)
			}

			archivedPaths := getArchivedPaths(results)
			if len(archivedPaths) > 0 {
				hasAnyArchived = true
			}

			var fileMatches map[string][]FileMatch
			if cfg.filesMode && len(archivedPaths) > 0 {
				fm, err := ScanImports(filepath.Dir(mi.gomodPath), archivedPaths)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				} else {
					fileMatches = fm
				}
			}

			deprecatedModules := getDeprecatedModules(mi.allModules, cfg.directOnly, cfg.deprecatedMode)
			stale := filterStale(results)
			jsonOut := buildJSONOutput(results, mi.nonGHModules, cfg.showAll, fileMatches, stale, deprecatedModules)
			out.Modules = append(out.Modules, RecursiveJSONEntry{
				GoMod:      mi.relPath,
				ModulePath: mi.moduleName,
				GoVersion:  cfg.goToolchain,
				JSONOutput: jsonOut,
			})
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	}

	return hasAnyArchived
}

// runRecursiveMarkdown outputs recursive results as Markdown with per-module headers.
func runRecursiveMarkdown(modules []moduleInfo, statusMap map[string]RepoStatus, cfg runConfig) bool {
	hasAnyArchived := false

	for i, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		// Apply ignore list
		il := BuildIgnoreList(filepath.Dir(mi.gomodPath), cfg.ignoreFile, cfg.ignoreInline)
		if il.Len() > 0 {
			var ignored []RepoStatus
			results, ignored = il.FilterResults(results)
			if len(ignored) > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Ignored %d %s.\n", len(ignored), pluralize(len(ignored), "module", "modules"))
			}
		}

		archivedPaths := getArchivedPaths(results)
		hasArchived := len(archivedPaths) > 0
		if hasArchived {
			hasAnyArchived = true
		}

		if i > 0 {
			_, _ = fmt.Fprintln(os.Stdout)
		}
		_, _ = fmt.Fprintf(os.Stdout, "# %s — %s (%s)\n\n", mi.relPath, mi.moduleName, cfg.goToolchain)

		if len(mi.githubModules) == 0 {
			_, _ = fmt.Fprintf(os.Stdout, "No GitHub modules found.\n")
			continue
		}

		var fileMatches map[string][]FileMatch
		if cfg.filesMode && hasArchived {
			fm, err := ScanImports(filepath.Dir(mi.gomodPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports: %v\n", err)
			} else {
				fileMatches = fm
			}
		}

		deprecatedModules := getDeprecatedModules(mi.allModules, cfg.directOnly, cfg.deprecatedMode)
		stale := filterStale(results)

		if cfg.treeMode && hasArchived {
			graph, err := parseModGraph(filepath.Dir(mi.gomodPath), cfg.goVersion)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph: %v\n", err)
			} else {
				PrintMarkdownTree(results, graph, mi.allModules, fileMatches)
				if len(stale) > 0 {
					PrintMarkdownStale(stale)
				}
				if len(deprecatedModules) > 0 {
					PrintMarkdown(nil, nil, false, deprecatedModules)
				}
				if len(mi.nonGHModules) > 0 {
					PrintMarkdownSkipped(mi.nonGHModules)
				}
				continue
			}
		}

		PrintMarkdown(results, mi.nonGHModules, cfg.showAll, deprecatedModules)
		if fileMatches != nil {
			PrintMarkdownFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintMarkdownStale(stale)
		}
	}

	return hasAnyArchived
}

// runRecursiveText outputs recursive results as text with per-module headers.
func runRecursiveText(modules []moduleInfo, statusMap map[string]RepoStatus, cfg runConfig) bool {
	hasAnyArchived := false

	for i, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		// Apply ignore list
		il := BuildIgnoreList(filepath.Dir(mi.gomodPath), cfg.ignoreFile, cfg.ignoreInline)
		if il.Len() > 0 {
			var ignored []RepoStatus
			results, ignored = il.FilterResults(results)
			if len(ignored) > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Ignored %d %s.\n", len(ignored), pluralize(len(ignored), "module", "modules"))
			}
		}

		archivedPaths := getArchivedPaths(results)
		hasArchived := len(archivedPaths) > 0
		if hasArchived {
			hasAnyArchived = true
		}

		if i > 0 {
			_, _ = fmt.Fprintln(os.Stderr)
		}
		_, _ = fmt.Fprintf(os.Stderr, "=== %s — %s (%s) ===\n", mi.relPath, mi.moduleName, cfg.goToolchain)

		if len(mi.githubModules) == 0 {
			_, _ = fmt.Fprintf(os.Stderr, "No GitHub modules found.\n")
			continue
		}

		var fileMatches map[string][]FileMatch
		if cfg.filesMode && hasArchived {
			fm, err := ScanImports(filepath.Dir(mi.gomodPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports: %v\n", err)
			} else {
				fileMatches = fm
			}
		}

		deprecatedModules := getDeprecatedModules(mi.allModules, cfg.directOnly, cfg.deprecatedMode)
		stale := filterStale(results)

		if cfg.treeMode && hasArchived {
			graph, err := parseModGraph(filepath.Dir(mi.gomodPath), cfg.goVersion)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph: %v\n", err)
			} else {
				if cfg.outputFormat == "mermaid" {
					PrintMermaid(results, graph, mi.allModules)
				} else {
					PrintTree(results, graph, mi.allModules, fileMatches)
					if len(stale) > 0 {
						PrintStaleTable(stale)
					}
					if len(deprecatedModules) > 0 {
						PrintDeprecatedTable(deprecatedModules)
					}
					if len(mi.nonGHModules) > 0 {
						PrintSkippedTable(mi.nonGHModules)
					}
				}
				continue
			}
		}

		PrintTable(results, mi.nonGHModules, cfg.showAll, deprecatedModules)
		if fileMatches != nil {
			PrintFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintStaleTable(stale)
		}
	}

	return hasAnyArchived
}
