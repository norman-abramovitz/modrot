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
func PrintMarkdown(results []RepoStatus, nonGitHubModules []Module, showAll bool, deprecatedModules ...[]Module) {
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
	sortResults(archivedDirect)
	sortResults(archivedIndirect)

	totalChecked := len(results)

	if len(archived) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "## ARCHIVED DEPENDENCIES (%d of %d github.com modules)\n\n", len(archived), totalChecked)

		var headers []string
		if durationEnabled && freshnessEnabled {
			headers = []string{"Module", "Version", "Direct", "Archived At", "Duration", "Last Pushed", "Latest", "Behind"}
		} else if durationEnabled {
			headers = []string{"Module", "Version", "Direct", "Archived At", "Duration", "Last Pushed"}
		} else if freshnessEnabled {
			headers = []string{"Module", "Version", "Direct", "Archived At", "Last Pushed", "Latest", "Behind"}
		} else {
			headers = []string{"Module", "Version", "Direct", "Archived At", "Last Pushed"}
		}

		buildRows := func(results []RepoStatus) [][]string {
			var rows [][]string
			for _, r := range results {
				direct := "indirect"
				if r.Module.Direct {
					direct = "direct"
				}
				latest := r.Module.LatestVersion
				if latest != "" && latest == r.Module.Version {
					latest = "-"
				}
				behind := formatBehind(r.Module)
				if durationEnabled && freshnessEnabled {
					rows = append(rows, []string{
						r.Module.Path, r.Module.Version, direct,
						fmtDate(r.ArchivedAt), formatDuration(r.ArchivedAt), fmtDate(r.PushedAt), latest, behind,
					})
				} else if durationEnabled {
					rows = append(rows, []string{
						r.Module.Path, r.Module.Version, direct,
						fmtDate(r.ArchivedAt), formatDuration(r.ArchivedAt), fmtDate(r.PushedAt),
					})
				} else if freshnessEnabled {
					rows = append(rows, []string{
						r.Module.Path, r.Module.Version, direct,
						fmtDate(r.ArchivedAt), fmtDate(r.PushedAt), latest, behind,
					})
				} else {
					rows = append(rows, []string{
						r.Module.Path, r.Module.Version, direct,
						fmtDate(r.ArchivedAt), fmtDate(r.PushedAt),
					})
				}
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

	if showAll && len(active) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "\n## ACTIVE DEPENDENCIES (%d modules)\n\n", len(active))
		sort.Slice(active, func(i, j int) bool {
			return active[i].Module.Path < active[j].Module.Path
		})
		var headers []string
		if freshnessEnabled {
			headers = []string{"Module", "Version", "Direct", "Last Pushed", "Latest", "Behind"}
		} else {
			headers = []string{"Module", "Version", "Direct", "Last Pushed"}
		}
		var rows [][]string
		for _, r := range active {
			direct := "indirect"
			if r.Module.Direct {
				direct = "direct"
			}
			if freshnessEnabled {
				latest := r.Module.LatestVersion
				if latest != "" && latest == r.Module.Version {
					latest = "-"
				}
				behind := formatBehind(r.Module)
				rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt), latest, behind})
			} else {
				rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt)})
			}
		}
		printMarkdownTable(os.Stdout, headers, rows)
	}

	// Deprecated modules section
	if len(deprecatedModules) > 0 && len(deprecatedModules[0]) > 0 {
		deps := deprecatedModules[0]
		sort.Slice(deps, func(i, j int) bool {
			return deps[i].Path < deps[j].Path
		})
		_, _ = fmt.Fprintf(os.Stdout, "\n## DEPRECATED MODULES (%d %s)\n\n", len(deps), pluralize(len(deps), "module", "modules"))
		headers := []string{"Module", "Version", "Direct", "Message"}
		var rows [][]string
		for _, m := range deps {
			direct := "indirect"
			if m.Direct {
				direct = "direct"
			}
			rows = append(rows, []string{m.Path, m.Version, direct, m.Deprecated})
		}
		printMarkdownTable(os.Stdout, headers, rows)
	}

	if len(nonGitHubModules) > 0 {
		PrintMarkdownSkipped(nonGitHubModules)
	}
}

// PrintMarkdownSkipped outputs non-GitHub modules in Markdown format.
func PrintMarkdownSkipped(modules []Module) {
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Path < modules[j].Path
	})
	_, _ = fmt.Fprintf(os.Stdout, "\n## NON-GITHUB MODULES (%d non-GitHub %s)\n\n",
		len(modules), pluralize(len(modules), "module", "modules"))
	var headers []string
	if freshnessEnabled {
		headers = []string{"Module", "Version", "Latest", "Behind", "Direct", "Published", "Source"}
	} else {
		headers = []string{"Module", "Version", "Latest", "Direct", "Published", "Source"}
	}
	var rows [][]string
	for _, m := range modules {
		direct := "indirect"
		if m.Direct {
			direct = "direct"
		}
		latest := m.LatestVersion
		if latest != "" && latest == m.Version {
			latest = "-"
		}
		if freshnessEnabled {
			behind := formatBehind(m)
			rows = append(rows, []string{m.Path, m.Version, latest, behind, direct, fmtDate(m.VersionTime), m.SourceURL})
		} else {
			rows = append(rows, []string{m.Path, m.Version, latest, direct, fmtDate(m.VersionTime), m.SourceURL})
		}
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
func PrintMarkdownStale(stale []RepoStatus) {
	if len(stale) == 0 {
		return
	}
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].Module.Path < stale[j].Module.Path
	})
	_, _ = fmt.Fprintf(os.Stdout, "\n## STALE DEPENDENCIES (%d %s not pushed in >%s)\n\n",
		len(stale), pluralize(len(stale), "module", "modules"), formatThreshold())

	var headers []string
	if durationEnabled && freshnessEnabled {
		headers = []string{"Module", "Version", "Direct", "Last Pushed", "Inactive", "Latest", "Behind"}
	} else if durationEnabled {
		headers = []string{"Module", "Version", "Direct", "Last Pushed", "Inactive"}
	} else if freshnessEnabled {
		headers = []string{"Module", "Version", "Direct", "Last Pushed", "Latest", "Behind"}
	} else {
		headers = []string{"Module", "Version", "Direct", "Last Pushed"}
	}
	var rows [][]string
	for _, r := range stale {
		direct := "indirect"
		if r.Module.Direct {
			direct = "direct"
		}
		latest := r.Module.LatestVersion
		if latest != "" && latest == r.Module.Version {
			latest = "-"
		}
		behind := formatBehind(r.Module)
		if durationEnabled && freshnessEnabled {
			rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt), formatDurationShort(r.PushedAt), latest, behind})
		} else if durationEnabled {
			rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt), formatDurationShort(r.PushedAt)})
		} else if freshnessEnabled {
			rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt), latest, behind})
		} else {
			rows = append(rows, []string{r.Module.Path, r.Module.Version, direct, fmtDate(r.PushedAt)})
		}
	}
	printMarkdownTable(os.Stdout, headers, rows)
}

// PrintMarkdownTree outputs the dependency tree in Markdown format.
func PrintMarkdownTree(results []RepoStatus, graph map[string][]string, allModules []Module, fileMatches map[string][]FileMatch) {
	entries, ctx := buildTree(results, graph, allModules)

	if entries == nil {
		_, _ = fmt.Fprintf(os.Stdout, "No archived dependencies found.\n")
		return
	}

	_, _ = fmt.Fprintf(os.Stdout, "## DEPENDENCY TREE\n\n")

	for _, e := range entries {
		if ctx.archivedPaths[e.directPath] {
			if rs, ok := ctx.getStatus(e.directPath); ok {
				_, _ = fmt.Fprintf(os.Stdout, "- **%s** `[ARCHIVED %s]`", formatTreeLabel(e.directPath, ctx.versionByPath[e.directPath]), fmtDate(rs.ArchivedAt))
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
				_, _ = fmt.Fprintf(os.Stdout, "  - **%s** `[ARCHIVED %s]`\n", formatTreeLabel(a, ctx.versionByPath[a]), fmtDate(rs.ArchivedAt))
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
