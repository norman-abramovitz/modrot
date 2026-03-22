package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// printMarkdownTable writes a GitHub-flavored Markdown table.
func printMarkdownTable(w io.Writer, headers []string, rows [][]string) {
	// Header row
	_, _ = fmt.Fprintf(w, "| %s |\n", strings.Join(headers, " | "))
	// Separator row
	seps := make([]string, len(headers))
	for i := range seps {
		seps[i] = "---"
	}
	_, _ = fmt.Fprintf(w, "| %s |\n", strings.Join(seps, " | "))
	// Data rows
	for _, row := range rows {
		_, _ = fmt.Fprintf(w, "| %s |\n", strings.Join(row, " | "))
	}
}

// PrintMarkdown outputs results in GitHub-flavored Markdown format.
func PrintMarkdown(cfg *Config, results []RepoStatus, nonGitHubModules []Module, deprecatedModules ...[]Module) {
	var archived, notFound, active []RepoStatus
	for _, r := range results {
		switch {
		case r.NotFound:
			notFound = append(notFound, r)
		case r.IsArchived:
			archived = append(archived, r)
		default:
			active = append(active, r)
		}
	}

	// Split and sort archived
	var archivedDirect, archivedIndirect []RepoStatus
	for _, r := range archived {
		if r.Module.Direct {
			archivedDirect = append(archivedDirect, r)
		} else {
			archivedIndirect = append(archivedIndirect, r)
		}
	}
	sortResults(cfg, archivedDirect)
	sortResults(cfg, archivedIndirect)

	totalChecked := len(results)

	if len(archived) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "## ARCHIVED DEPENDENCIES (%d of %d github.com modules)\n\n", len(archived), totalChecked)
		headers := archivedHeaders(cfg)
		buildRows := func(rs []RepoStatus) [][]string {
			var rows [][]string
			for _, r := range rs {
				rows = append(rows, archivedRow(cfg, r))
			}
			return rows
		}
		if len(archivedDirect) > 0 && len(archivedIndirect) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "### Direct (%d)\n\n", len(archivedDirect))
			printMarkdownTable(os.Stdout, headers, buildRows(archivedDirect))
			_, _ = fmt.Fprintf(os.Stdout, "\n### Indirect (%d)\n\n", len(archivedIndirect))
			printMarkdownTable(os.Stdout, headers, buildRows(archivedIndirect))
		} else {
			all := append(archivedDirect, archivedIndirect...)
			printMarkdownTable(os.Stdout, headers, buildRows(all))
		}
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "No archived dependencies found among %d github.com modules.\n", totalChecked)
	}

	if len(notFound) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "\n## NOT FOUND (%d modules)\n\n", len(notFound))
		for _, r := range notFound {
			_, _ = fmt.Fprintf(os.Stdout, "- %s — %s\n", r.Module.Path, r.Error)
		}
	}

	if cfg.ShowAll && len(active) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "\n## ACTIVE DEPENDENCIES (%d modules)\n\n", len(active))
		sort.Slice(active, func(i, j int) bool {
			return active[i].Module.Path < active[j].Module.Path
		})
		headers := []string{"Module", "Version", "Direct", "Last Pushed"}
		if cfg.Freshness {
			headers = append(headers, "Latest", "Behind")
		}
		var rows [][]string
		for _, r := range active {
			row := []string{r.Module.Path, r.Module.Version, directLabel(r.Module), fmtDate(cfg, r.PushedAt)}
			if cfg.Freshness {
				row = append(row, latestOrDash(r.Module), formatBehind(r.Module))
			}
			rows = append(rows, row)
		}
		printMarkdownTable(os.Stdout, headers, rows)
	}

	// Deprecated modules section
	if len(deprecatedModules) > 0 && len(deprecatedModules[0]) > 0 {
		printMarkdownDeprecated(deprecatedModules[0])
	}

	if len(nonGitHubModules) > 0 {
		PrintMarkdownSkipped(cfg, nonGitHubModules)
	}
}

// printMarkdownDeprecated outputs deprecated modules in Markdown format.
func printMarkdownDeprecated(deps []Module) {
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Path < deps[j].Path
	})
	_, _ = fmt.Fprintf(os.Stdout, "\n## DEPRECATED MODULES (%d %s)\n\n", len(deps), pluralize(len(deps), "module", "modules"))
	headers := []string{"Module", "Version", "Direct", "Message"}
	var rows [][]string
	for _, m := range deps {
		rows = append(rows, []string{m.Path, m.Version, directLabel(m), m.Deprecated})
	}
	printMarkdownTable(os.Stdout, headers, rows)
}

// PrintMarkdownSkipped outputs non-GitHub modules in Markdown format.
func PrintMarkdownSkipped(cfg *Config, modules []Module) {
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Path < modules[j].Path
	})
	_, _ = fmt.Fprintf(os.Stdout, "\n## NON-GITHUB MODULES (%d non-GitHub %s)\n\n",
		len(modules), pluralize(len(modules), "module", "modules"))
	headers := []string{"Module", "Version", "Latest"}
	if cfg.Freshness {
		headers = append(headers, "Behind")
	}
	headers = append(headers, "Direct", "Published", "Source")
	var rows [][]string
	for _, m := range modules {
		row := []string{m.Path, m.Version, latestOrDash(m)}
		if cfg.Freshness {
			row = append(row, formatBehind(m))
		}
		row = append(row, directLabel(m), fmtDate(cfg, m.VersionTime), m.SourceURL)
		rows = append(rows, row)
	}
	printMarkdownTable(os.Stdout, headers, rows)
}

// PrintMarkdownFiles outputs source file matches in Markdown format.
func PrintMarkdownFiles(results []RepoStatus, fileMatches map[string][]FileMatch) {
	var archivedPaths []string
	for _, r := range results {
		if r.IsArchived {
			archivedPaths = append(archivedPaths, r.Module.Path)
		}
	}
	sort.Strings(archivedPaths)

	_, _ = fmt.Fprintf(os.Stdout, "\n## SOURCE FILES IMPORTING ARCHIVED MODULES\n")

	for _, modPath := range archivedPaths {
		matches := fileMatches[modPath]
		uniqueFiles := make(map[string]bool)
		for _, m := range matches {
			uniqueFiles[m.File] = true
		}
		_, _ = fmt.Fprintf(os.Stdout, "\n### %s (%d %s)\n\n", modPath, len(uniqueFiles), pluralize(len(uniqueFiles), "file", "files"))
		for _, m := range matches {
			_, _ = fmt.Fprintf(os.Stdout, "- `%s:%d`\n", m.File, m.Line)
		}
	}
}

// PrintMarkdownStale outputs stale modules in Markdown format.
func PrintMarkdownStale(cfg *Config, stale []RepoStatus) {
	if len(stale) == 0 {
		return
	}
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].Module.Path < stale[j].Module.Path
	})
	_, _ = fmt.Fprintf(os.Stdout, "\n## STALE DEPENDENCIES (%d %s not pushed in >%s)\n\n",
		len(stale), pluralize(len(stale), "module", "modules"), formatThreshold(cfg))
	headers := staleHeaders(cfg)
	var rows [][]string
	for _, r := range stale {
		rows = append(rows, staleRow(cfg, r))
	}
	printMarkdownTable(os.Stdout, headers, rows)
}

// PrintMarkdownTree outputs the dependency tree in Markdown format.
func PrintMarkdownTree(cfg *Config, results []RepoStatus, graph map[string][]string, allModules []Module, fileMatches map[string][]FileMatch) {
	entries, ctx := buildTree(results, graph, allModules)

	if entries == nil {
		_, _ = fmt.Fprintf(os.Stdout, "No archived dependencies found.\n")
		return
	}

	_, _ = fmt.Fprintf(os.Stdout, "## DEPENDENCY TREE\n\n")

	for _, e := range entries {
		if ctx.archivedPaths[e.directPath] {
			if rs, ok := ctx.getStatus(e.directPath); ok {
				_, _ = fmt.Fprintf(os.Stdout, "- **%s** `[ARCHIVED %s]`", formatTreeLabel(e.directPath, ctx.versionByPath[e.directPath]), fmtDate(cfg, rs.ArchivedAt))
			} else {
				_, _ = fmt.Fprintf(os.Stdout, "- **%s** `[ARCHIVED]`", e.directPath)
			}
		} else {
			ver := ctx.versionByPath[e.directPath]
			if ver != "" {
				_, _ = fmt.Fprintf(os.Stdout, "- %s@%s", e.directPath, ver)
			} else {
				_, _ = fmt.Fprintf(os.Stdout, "- %s", e.directPath)
			}
		}
		_, _ = fmt.Fprintln(os.Stdout)

		seen := make(map[string]bool)
		for _, a := range e.archived {
			if seen[a] {
				continue
			}
			seen[a] = true
			if rs, ok := ctx.getStatus(a); ok {
				_, _ = fmt.Fprintf(os.Stdout, "  - **%s** `[ARCHIVED %s]`\n", formatTreeLabel(a, ctx.versionByPath[a]), fmtDate(cfg, rs.ArchivedAt))
			} else {
				_, _ = fmt.Fprintf(os.Stdout, "  - **%s** `[ARCHIVED]`\n", a)
			}
		}
	}
}

// formatTreeLabel returns "path@version" or just "path".
func formatTreeLabel(path, version string) string {
	if version != "" {
		return path + "@" + version
	}
	return path
}
