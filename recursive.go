package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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

// manifestInfo holds parsed data for a single manifest unit: one go.mod, or
// one package.json plus its lockfile.
type manifestInfo struct {
	eco           *Ecosystem
	manifestPath  string // path to the primary manifest
	files         []string
	relPath       string
	moduleName    string
	unlocked      bool
	allModules    []Module
	githubModules []Module
	nonGHModules  []Module
}

// sarifManifestDir returns the unit's directory relative to cwd, with forward
// slashes, for use as the base of SARIF artifact locations.
func sarifManifestDir(cwd, manifestPath string) string {
	dir := filepath.Dir(manifestPath)
	if rel, err := filepath.Rel(cwd, dir); err == nil {
		dir = rel
	}
	return filepath.ToSlash(dir)
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

// runRecursive scans a directory tree for manifest units (go.mod and/or
// package.json), queries GitHub once for all unique repos, and outputs
// per-unit results.
// Returns the exit code (0 = clean, 1 = archived found, 2 = error).
func runRecursive(rootDir string, cfg *Config) int {
	dirs, err := findUnitDirs(rootDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		return 2
	}
	if len(dirs) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No go.mod or package.json files found in %s\n", rootDir)
		return 2
	}

	modules, failed := buildManifestInfos(dirs, rootDir)
	if len(modules) == 0 {
		return reportNoUnits(rootDir, failed)
	}
	enrichUnits(modules, cfg)

	statusMap, err := checkUnitsOnGitHub(modules, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	if dispatchRecursiveOutput(modules, statusMap, cfg) {
		return 1
	}
	return 0
}

// checkUnitsOnGitHub filters every unit down to its GitHub modules,
// collects the globally unique set of repos across all units, and queries
// GitHub for their archive status once. It also runs the non-GitHub proxy
// and freshness enrichment passes, which key off the same per-unit
// githubModules/nonGHModules split. Returns owner/repo → RepoStatus.
func checkUnitsOnGitHub(units []manifestInfo, cfg *Config) (map[string]RepoStatus, error) {
	var allGitHub []Module
	globalSeen := make(map[string]bool)
	for i := range units {
		ghMods, nonGH := FilterGitHub(units[i].allModules, cfg.DirectOnly)
		units[i].githubModules = ghMods
		units[i].nonGHModules = nonGH

		for _, m := range ghMods {
			key := m.Owner + "/" + m.Repo
			if !globalSeen[key] {
				globalSeen[key] = true
				allGitHub = append(allGitHub, m)
			}
		}
	}

	// Enrich non-GitHub modules with proxy data
	enrichAcrossModules(units)

	// Enrich all modules with freshness data (skips already-enriched).
	// npm has no equivalent of the Go module proxy's freshness endpoints, so
	// npm units are excluded rather than passed through.
	if cfg.Freshness {
		var goUnits []manifestInfo
		for i := range units {
			if units[i].eco.Name == "go" {
				goUnits = append(goUnits, units[i])
			}
		}
		enrichFreshnessAcrossModules(goUnits)
	}

	if len(allGitHub) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "No GitHub modules found across %d %s.\n",
			len(units), pluralize(len(units), "manifest", "manifests"))
		return map[string]RepoStatus{}, nil
	}

	_, _ = fmt.Fprintf(os.Stderr, "Found %d %s, checking %d unique GitHub repos...\n",
		len(units), pluralize(len(units), "manifest", "manifests"), len(allGitHub))

	results, err := CheckRepos(allGitHub, cfg.Workers)
	if err != nil {
		return nil, err
	}

	statusMap := make(map[string]RepoStatus)
	for _, r := range results {
		statusMap[r.Module.Owner+"/"+r.Module.Repo] = r
	}
	return statusMap, nil
}

// dispatchRecursiveOutput renders every unit in the format selected by
// cfg.OutputFormat and reports whether any archived dependency was found.
func dispatchRecursiveOutput(units []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	switch cfg.OutputFormat {
	case "quickfix":
		return runRecursiveQuickfix(units, statusMap, cfg)
	case "json":
		return runRecursiveJSON(units, statusMap, cfg)
	case "sarif":
		return runRecursiveSARIF(units, statusMap, cfg)
	case "markdown":
		return runRecursiveMarkdown(units, statusMap, cfg)
	default:
		return runRecursiveText(units, statusMap, cfg)
	}
}

// runRecursiveQuickfix outputs quickfix-format lines across all modules.
func runRecursiveQuickfix(modules []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	hasAnyArchived := false

	for _, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
		if il.Len() > 0 {
			results, _ = il.FilterResults(results)
		}

		archivedPaths := getArchivedPaths(results)
		if len(archivedPaths) > 0 {
			hasAnyArchived = true
			fm, err := ScanImports(filepath.Dir(mi.manifestPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				continue
			}
			PrintFilesPlain(filepath.ToSlash(mi.relPath), results, fm)
		}
	}

	return hasAnyArchived
}

// runRecursiveJSON outputs recursive results as a single JSON document.
func runRecursiveJSON(modules []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	hasAnyArchived := false

	if cfg.Tree {
		out := RecursiveJSONTreeOutput{Modules: []RecursiveJSONTreeEntry{}}

		for _, mi := range modules {
			results := applyStatus(mi.githubModules, statusMap)

			// Apply ignore list
			il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
			if il.Len() > 0 {
				results, _ = il.FilterResults(results)
			}

			archivedPaths := getArchivedPaths(results)
			if len(archivedPaths) > 0 {
				hasAnyArchived = true
			}

			var fileMatches map[string][]FileMatch
			if cfg.Files && len(archivedPaths) > 0 {
				fm, err := ScanImports(filepath.Dir(mi.manifestPath), archivedPaths)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				} else {
					fileMatches = fm
				}
			}

			var graph map[string][]string
			if mi.eco.Graph != nil {
				g, err := mi.eco.Graph(filepath.Dir(mi.manifestPath), cfg.GoVersion)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph for %s: %v\n", mi.relPath, err)
					graph = map[string][]string{}
				} else {
					graph = g
				}
			} else {
				graph = map[string][]string{}
			}

			deprecatedModules := getDeprecatedModules(mi.allModules, cfg.DirectOnly, cfg.Deprecated)
			treeOut := buildTreeJSONOutput(cfg, results, graph, mi.allModules, fileMatches, mi.nonGHModules, deprecatedModules)
			out.Modules = append(out.Modules, RecursiveJSONTreeEntry{
				GoMod:          mi.relPath,
				ModulePath:     mi.moduleName,
				GoVersion:      cfg.GoToolchain,
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
			il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
			if il.Len() > 0 {
				results, _ = il.FilterResults(results)
			}

			archivedPaths := getArchivedPaths(results)
			if len(archivedPaths) > 0 {
				hasAnyArchived = true
			}

			var fileMatches map[string][]FileMatch
			if cfg.Files && len(archivedPaths) > 0 {
				fm, err := ScanImports(filepath.Dir(mi.manifestPath), archivedPaths)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports for %s: %v\n", mi.relPath, err)
				} else {
					fileMatches = fm
				}
			}

			deprecatedModules := getDeprecatedModules(mi.allModules, cfg.DirectOnly, cfg.Deprecated)
			stale := filterStale(cfg, results)
			jsonOut := buildJSONOutput(cfg, results, mi.nonGHModules, fileMatches, stale, deprecatedModules)
			out.Modules = append(out.Modules, RecursiveJSONEntry{
				GoMod:      mi.relPath,
				ModulePath: mi.moduleName,
				GoVersion:  cfg.GoToolchain,
				JSONOutput: jsonOut,
			})
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	}

	return hasAnyArchived
}

// runRecursiveSARIF outputs one SARIF document covering all manifest units,
// each finding anchored to its own manifest file within its unit.
func runRecursiveSARIF(modules []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	hasAnyArchived := false
	inputs := make([]SARIFInput, 0, len(modules))
	cwd, _ := os.Getwd()

	for _, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
		if il.Len() > 0 {
			results, _ = il.FilterResults(results)
		}

		if len(getArchivedPaths(results)) > 0 {
			hasAnyArchived = true
		}

		inputs = append(inputs, SARIFInput{
			ManifestDir: sarifManifestDir(cwd, mi.manifestPath),
			Results:     results,
			Deprecated:  getDeprecatedModules(mi.allModules, cfg.DirectOnly, cfg.Deprecated),
		})
	}

	PrintSARIF(inputs)
	return hasAnyArchived
}

// runRecursiveMarkdown outputs recursive results as Markdown with per-module headers.
func runRecursiveMarkdown(modules []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	hasAnyArchived := false

	for i, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		// Apply ignore list
		il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
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
		_, _ = fmt.Fprintf(os.Stdout, "# %s\n\n", unitHeader(mi, cfg))

		if len(mi.githubModules) == 0 {
			_, _ = fmt.Fprintf(os.Stdout, "No GitHub modules found.\n")
			continue
		}

		var fileMatches map[string][]FileMatch
		if cfg.Files && hasArchived {
			fm, err := ScanImports(filepath.Dir(mi.manifestPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports: %v\n", err)
			} else {
				fileMatches = fm
			}
		}

		deprecatedModules := getDeprecatedModules(mi.allModules, cfg.DirectOnly, cfg.Deprecated)
		stale := filterStale(cfg, results)

		if cfg.Tree && hasArchived && mi.eco.Graph != nil {
			graph, err := mi.eco.Graph(filepath.Dir(mi.manifestPath), cfg.GoVersion)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph: %v\n", err)
			} else {
				PrintMarkdownTree(cfg, results, graph, mi.allModules, fileMatches)
				if len(stale) > 0 {
					PrintMarkdownStale(cfg, stale)
				}
				if len(deprecatedModules) > 0 {
					PrintMarkdown(cfg, nil, nil, deprecatedModules)
				}
				if len(mi.nonGHModules) > 0 {
					PrintMarkdownSkipped(cfg, mi.nonGHModules)
				}
				continue
			}
		}

		PrintMarkdown(cfg, results, mi.nonGHModules, deprecatedModules)
		if fileMatches != nil {
			PrintMarkdownFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintMarkdownStale(cfg, stale)
		}
	}

	return hasAnyArchived
}

// runRecursiveText outputs recursive results as text with per-module headers.
func runRecursiveText(modules []manifestInfo, statusMap map[string]RepoStatus, cfg *Config) bool {
	hasAnyArchived := false

	for i, mi := range modules {
		results := applyStatus(mi.githubModules, statusMap)

		// Apply ignore list
		il := BuildIgnoreList(filepath.Dir(mi.manifestPath), cfg.IgnoreFile, cfg.IgnoreInline)
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
		_, _ = fmt.Fprintf(os.Stderr, "=== %s ===\n", unitHeader(mi, cfg))
		warnUnsupported(mi, cfg)

		if len(mi.githubModules) == 0 {
			_, _ = fmt.Fprintf(os.Stderr, "No GitHub modules found.\n")
			continue
		}

		var fileMatches map[string][]FileMatch
		if cfg.Files && hasArchived {
			fm, err := ScanImports(filepath.Dir(mi.manifestPath), archivedPaths)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not scan imports: %v\n", err)
			} else {
				fileMatches = fm
			}
		}

		deprecatedModules := getDeprecatedModules(mi.allModules, cfg.DirectOnly, cfg.Deprecated)
		stale := filterStale(cfg, results)

		if cfg.Tree && hasArchived && mi.eco.Graph != nil {
			graph, err := mi.eco.Graph(filepath.Dir(mi.manifestPath), cfg.GoVersion)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph: %v\n", err)
			} else {
				if cfg.OutputFormat == "mermaid" {
					PrintMermaid(cfg, results, graph, mi.allModules)
				} else {
					PrintTree(cfg, results, graph, mi.allModules, fileMatches)
					if len(stale) > 0 {
						PrintStaleTable(cfg, stale)
					}
					if len(deprecatedModules) > 0 {
						PrintDeprecatedTable(deprecatedModules)
					}
					if len(mi.nonGHModules) > 0 {
						PrintSkippedTable(cfg, mi.nonGHModules)
					}
				}
				continue
			}
		}

		PrintTable(cfg, results, mi.nonGHModules, deprecatedModules)
		if fileMatches != nil {
			PrintFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintStaleTable(cfg, stale)
		}
	}

	return hasAnyArchived
}
