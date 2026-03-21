package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// mermaidSafeID sanitizes a module path for use as a Mermaid node ID.
// Replaces characters that conflict with Mermaid syntax.
func mermaidSafeID(modulePath string) string {
	r := strings.NewReplacer(
		".", "_",
		"/", "_",
		"-", "_",
		"@", "_at_",
	)
	return r.Replace(modulePath)
}

// mermaidLabel returns a short display label for a module node.
func mermaidLabel(modulePath, version string) string {
	if version != "" {
		return modulePath + "@" + version
	}
	return modulePath
}

// PrintMermaid outputs a Mermaid flowchart diagram showing archived dependencies.
// Only paths leading to archived deps are shown (unrelated branches are pruned).
func PrintMermaid(cfg *Config, results []RepoStatus, graph map[string][]string, allModules []Module) {
	entries, ctx := buildTree(results, graph, allModules)

	_, _ = fmt.Fprintln(os.Stdout, "graph TD")

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "    root[\"No archived dependencies\"]")
		return
	}

	// Find root module name from graph
	var rootKey string
	for key := range graph {
		if !strings.Contains(key, "@") {
			rootKey = key
			break
		}
	}
	if rootKey == "" {
		rootKey = "root"
	}

	rootID := mermaidSafeID(rootKey)
	_, _ = fmt.Fprintf(os.Stdout, "    %s[\"%s\"]\n", rootID, rootKey)

	// Build version lookup for labels
	versionByPath := make(map[string]string)
	for _, m := range allModules {
		versionByPath[m.Path] = m.Version
	}

	// Track nodes we've already declared
	declared := map[string]bool{rootID: true}

	// Sort entries for stable output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].directPath < entries[j].directPath
	})

	nodeIndex := 0
	nodeIDMap := make(map[string]string) // module path → node ID

	getNodeID := func(modPath string) string {
		if id, ok := nodeIDMap[modPath]; ok {
			return id
		}
		id := fmt.Sprintf("r%d", nodeIndex)
		nodeIndex++
		nodeIDMap[modPath] = id
		return id
	}

	for _, e := range entries {
		directID := getNodeID(e.directPath)
		ver := ctx.versionByPath[e.directPath]
		label := mermaidLabel(e.directPath, ver)

		if !declared[directID] {
			if ctx.archivedPaths[e.directPath] {
				_, _ = fmt.Fprintf(os.Stdout, "    %s[\"%s\"]:::archived\n", directID, label)
			} else {
				_, _ = fmt.Fprintf(os.Stdout, "    %s[\"%s\"]\n", directID, label)
			}
			declared[directID] = true
		}

		// Link root → direct dep
		_, _ = fmt.Fprintf(os.Stdout, "    %s --> %s\n", rootID, directID)

		// Transitive archived deps
		seen := make(map[string]bool)
		for _, a := range e.archived {
			if seen[a] {
				continue
			}
			seen[a] = true

			aID := getNodeID(a)
			aVer := ctx.versionByPath[a]
			aLabel := mermaidLabel(a, aVer)

			if !declared[aID] {
				class := ":::archived"
				if ctx.deprecatedByPath[a] != "" {
					class = ":::deprecated"
				}
				_, _ = fmt.Fprintf(os.Stdout, "    %s[\"%s\"]%s\n", aID, aLabel, class)
				declared[aID] = true
			}

			// Link direct dep → archived transitive dep
			_, _ = fmt.Fprintf(os.Stdout, "    %s --> %s\n", directID, aID)
		}
	}

	// Class definitions
	_, _ = fmt.Fprintln(os.Stdout, "    classDef archived fill:#f96,stroke:#333,stroke-width:2px")
	_, _ = fmt.Fprintln(os.Stdout, "    classDef deprecated fill:#ff9,stroke:#333,stroke-width:2px")
}
