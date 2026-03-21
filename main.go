package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	// Extract --duration, --stale, and --freshness before reorderArgs and flag.Parse,
	// since they support optional values which Go's flag package cannot handle.
	extractDurationFlag()
	extractStaleFlag()
	extractAgeFlag()

	// Reorder args so flags can appear after the positional argument.
	// Go's flag package stops parsing at the first non-flag argument.
	reorderArgs()

	// Output format flags
	formatFlag := flag.String("format", "table", "Output format: table, json, markdown, mermaid, quickfix")
	jsonFlag := flag.Bool("json", false, "Output as JSON (alias for --format=json)")
	markdownFlag := flag.Bool("markdown", false, "Output as GitHub-flavored Markdown (alias for --format=markdown)")
	mermaidFlag := flag.Bool("mermaid", false, "Output Mermaid flowchart diagram (alias for --format=mermaid)")
	quickfixFlag := flag.Bool("quickfix", false, "Output file:line:module for editor quickfix (alias for --format=quickfix)")

	// Filtering flags
	directOnly := flag.Bool("direct-only", false, "Only check direct dependencies")
	ignoreFileFlag := flag.String("ignore-file", "", "Path to ignore file (default: .modrotignore next to go.mod)")
	ignoreFlag := flag.String("ignore", "", "Comma-separated list of module paths to ignore")
	showIgnoredFlag := flag.Bool("show-ignored", false, "Show ignored modules and their current state")
	noIgnoreFlag := flag.Bool("no-ignore", false, "Disable ignore lists (.modrotignore and --ignore)")

	// Analysis flags
	resolveFlag := flag.Bool("resolve", false, "Resolve vanity import paths (e.g. google.golang.org/grpc) to GitHub repos")
	deprecatedFlag := flag.Bool("deprecated", false, "Check for deprecated modules via the Go module proxy")
	freshnessFlag := flag.Bool("freshness", false, "Show latest available version and how far behind each dependency is")

	// Display flags
	allFlag := flag.Bool("all", false, "Show all modules, not just archived ones")
	treeFlag := flag.Bool("tree", false, "Show ASCII dependency tree for archived modules (uses go mod graph)")
	filesFlag := flag.Bool("files", false, "Show source files that import archived modules")
	sortFlag := flag.String("sort", "name", "Sort: name[:asc|desc], duration[:asc|desc], pushed[:asc|desc]; name defaults asc, duration/pushed default desc")
	timeFlag := flag.Bool("time", false, "Include time in date output (2006-01-02 15:04:05 instead of 2006-01-02)")

	// Execution flags
	workers := flag.Int("workers", 50, "Number of repos per GitHub GraphQL batch request")
	goVersionFlag := flag.String("go-version", "", "Override the Go toolchain version from go.mod (e.g. 1.21.0)")
	recursiveFlag := flag.Bool("recursive", false, "Scan all go.mod files in the directory tree")
	noColorFlag := flag.Bool("no-color", false, "Disable colored output (also respects NO_COLOR env var)")
	colorThresholdFlag := flag.String("color-threshold", "", "Age thresholds for color: 2–4 values (default: 3m,1y,2y,5y)")

	// Info flags
	versionFlag := flag.Bool("version", false, "Print version information and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: modrot [flags] [path/to/go.mod | path/to/dir]

Detect archived GitHub dependencies in a Go project.

With no flags, checks go.mod in the current directory and prints archived
dependencies as a table. Exits 1 if any are found (useful for CI).
Flags can appear before or after the path argument.

Output format:
  --format string       Output format: table, json, markdown, mermaid, quickfix (default "table")
  --json                Output as JSON (alias for --format=json)
  --markdown            Output as GitHub-flavored Markdown (alias for --format=markdown)
  --mermaid             Output Mermaid flowchart diagram (alias for --format=mermaid)
  --quickfix            Output file:line:module for editor quickfix (alias for --format=quickfix)

Filtering:
  --direct-only         Only check direct dependencies (useful for CI)
  --ignore-file string  Path to ignore file (default: .modrotignore next to go.mod)
  --ignore string       Comma-separated list of module paths to ignore
  --show-ignored        Show ignored modules and their current state
  --no-ignore           Disable ignore lists (.modrotignore and --ignore)
  --stale[=THRESHOLD]   Show dependencies not pushed in >THRESHOLD (default: 2y, e.g. 1y6m, 180d)

Analysis:
  --resolve             Resolve vanity import paths to GitHub repos (recommended)
  --deprecated          Check for deprecated modules via the Go module proxy
  --freshness           Show latest available version and how far behind each dependency is
  --age[=THRESHOLD]     Show how old each dependency's version is (today minus publish date)
                          With threshold, show OUTDATED section (e.g. --age=18m, --age=1y6m)
  --duration[=DATE]     Show how long dependencies have been archived (default: today)

Display:
  --all                 Show all modules, not just archived ones
  --tree                Show ASCII dependency tree for archived modules (uses go mod graph)
  --files               Show source files that import archived modules (requires rg)
  --sort string         Sort: name[:asc|desc], duration[:asc|desc], pushed[:asc|desc]
                          name defaults to asc (A-Z), duration and pushed default to desc (oldest first)
  --time                Include time in date output

Execution:
  --workers int         Number of repos per GitHub GraphQL batch request (default 50)
  --go-version string   Override the Go toolchain version from go.mod
  --recursive           Scan all go.mod files in the directory tree (monorepos)
  --no-color            Disable colored output (also respects NO_COLOR env var)
  --color-threshold     Age thresholds: 2–4 comma-separated values (default: 3m,1y,2y,5y)
                          2 values → 3 levels, 3 → 4 levels, 4 → 5 levels
                          Symbols: ★ new  ◇ recent  ◆ moderate  ▲ old  ✖ critical

Info:
  --version             Print version information and exit

Examples:
  modrot                                     Check current directory
  modrot /path/to/go.mod                     Check a specific go.mod
  modrot --direct-only                       Direct dependencies only
  modrot --direct-only --stale               Verify no archived or stale deps
  modrot --resolve --deprecated --stale      Full dependency health check
  modrot --freshness --all                   Show version freshness for all deps
  modrot --age=18m --direct-only             Find deps older than 18 months
  modrot /path/to/pkg/go.mod --all --stale   Evaluate a package before adopting
  modrot --tree --files                      ASCII dependency tree and affected files
  modrot --markdown --all --deprecated       Markdown for release notes
  modrot --json | jq '.archived[].module'    Scripting with JSON output
  modrot --recursive /path/to/monorepo       Scan all go.mod files in a tree
`)
	}
	flag.Parse()

	// Detect common help patterns passed as positional arguments
	if flag.NArg() > 0 {
		arg := flag.Arg(0)
		if arg == "help" || arg == "-h" {
			flag.Usage()
			os.Exit(0)
		}
	}

	if *versionFlag {
		printVersion()
		os.Exit(0)
	}

	// Resolve output format: aliases override --format default
	outputFormat := *formatFlag
	switch {
	case *jsonFlag:
		outputFormat = "json"
	case *markdownFlag:
		outputFormat = "markdown"
	case *mermaidFlag:
		outputFormat = "mermaid"
	case *quickfixFlag:
		outputFormat = "quickfix"
	}

	// Quickfix implies --files
	if outputFormat == "quickfix" {
		*filesFlag = true
	}

	// Mermaid implies --tree
	if outputFormat == "mermaid" {
		*treeFlag = true
	}

	// Set freshness mode from flag
	if *freshnessFlag {
		freshnessEnabled = true
	}

	// Set date format
	if *timeFlag {
		dateFmt = "2006-01-02 15:04:05"
	}

	// Set sort mode and direction (supports "field" or "field:asc" / "field:desc")
	parseSortFlag(*sortFlag)

	// Initialize color support (auto-detects terminal, respects NO_COLOR)
	// Disable color for non-table formats (JSON, markdown, mermaid, quickfix)
	noColor := *noColorFlag || outputFormat != "table"
	if err := initColor(noColor, *colorThresholdFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	// Determine input path
	inputPath := "."
	if flag.NArg() > 0 {
		inputPath = flag.Arg(0)
	}
	inputPath, err := filepath.Abs(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	// Recursive mode: scan directory tree for all go.mod files
	if *recursiveFlag {
		rootDir := inputPath
		if info, statErr := os.Stat(rootDir); statErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", statErr)
			os.Exit(2)
		} else if !info.IsDir() {
			rootDir = filepath.Dir(rootDir)
		}
		os.Exit(runRecursive(rootDir, runConfig{
			outputFormat:   outputFormat,
			showAll:        *allFlag,
			directOnly:     *directOnly,
			workers:        *workers,
			treeMode:       *treeFlag,
			filesMode:      *filesFlag,
			resolveMode:    *resolveFlag,
			deprecatedMode: *deprecatedFlag,
			freshnessMode:  freshnessEnabled,
			ageMode:        ageEnabled,
			goVersion:      *goVersionFlag,
			goToolchain:    goToolchainVersion(),
			durationMode:   durationEnabled,
			durationDate:   durationEndDate,
			sortMode:       *sortFlag,
			ignoreFile:     *ignoreFileFlag,
			ignoreInline:   *ignoreFlag,
		}))
	}

	// Single-module mode
	gomodPath := inputPath
	if info, err := os.Stat(gomodPath); err == nil && info.IsDir() {
		gomodPath = filepath.Join(gomodPath, "go.mod")
	}

	// Parse go.mod
	allModules, err := ParseGoMod(gomodPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	// Print module header (same format as recursive mode)
	modName, _ := ModuleName(gomodPath)
	cwd, _ := os.Getwd()
	relPath, relErr := filepath.Rel(cwd, gomodPath)
	if relErr != nil {
		relPath = gomodPath
	}
	fmt.Fprintf(os.Stderr, "=== %s — %s (%s) ===\n", relPath, modName, goToolchainVersion())

	// Resolve vanity imports to GitHub repos
	if *resolveFlag {
		resolved := ResolveVanityImports(allModules, 20)
		if resolved > 0 {
			fmt.Fprintf(os.Stderr, "Resolved %d non-GitHub modules to GitHub repos.\n", resolved)
		}
	}

	// Check for deprecated modules via proxy
	if *deprecatedFlag {
		count := CheckDeprecations(allModules, 20)
		if count > 0 {
			fmt.Fprintf(os.Stderr, "Found %d deprecated %s.\n", count, pluralize(count, "module", "modules"))
		}
	}

	// Filter to GitHub modules and deduplicate
	githubModules, nonGitHubModules := FilterGitHub(allModules, *directOnly)

	// Enrich non-GitHub modules with proxy data
	if len(nonGitHubModules) > 0 {
		EnrichNonGitHub(nonGitHubModules, 20)
	}

	// Enrich all modules with version data (skips already-enriched)
	if freshnessEnabled || ageEnabled {
		EnrichFreshness(allModules, 20)
	}

	if len(githubModules) == 0 {
		fmt.Fprintf(os.Stderr, "No GitHub modules found in %s\n", gomodPath)
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "Checking %d GitHub modules...\n", len(githubModules))

	// Query GitHub
	results, err := CheckRepos(githubModules, *workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	// Apply ignore list (unless --no-ignore is set)
	var ignoredResults []RepoStatus
	if !*noIgnoreFlag {
		ignoreList := NewIgnoreList()
		ignoreFilePath := *ignoreFileFlag
		if ignoreFilePath == "" {
			ignoreFilePath = filepath.Join(filepath.Dir(gomodPath), ".modrotignore")
		}
		if il, err := LoadIgnoreFile(ignoreFilePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read ignore file: %v\n", err)
		} else {
			for p := range il.paths {
				ignoreList.Add(p)
			}
		}
		if *ignoreFlag != "" {
			inline := ParseIgnoreList(*ignoreFlag)
			for p := range inline.paths {
				ignoreList.Add(p)
			}
		}
		if ignoreList.Len() > 0 {
			results, ignoredResults = ignoreList.FilterResults(results)
			if len(ignoredResults) > 0 && !*showIgnoredFlag {
				fmt.Fprintf(os.Stderr, "Ignored %d %s.\n", len(ignoredResults), pluralize(len(ignoredResults), "module", "modules"))
			}
		}
	}

	// Check if any archived
	hasArchived := false
	var archivedModulePaths []string
	for _, r := range results {
		if r.IsArchived {
			hasArchived = true
			archivedModulePaths = append(archivedModulePaths, r.Module.Path)
		}
	}

	// Scan source files for imports of archived modules
	var fileMatches map[string][]FileMatch
	if *filesFlag && hasArchived {
		fm, err := ScanImports(filepath.Dir(gomodPath), archivedModulePaths)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning imports: %v\n", err)
			os.Exit(2)
		}
		fileMatches = fm
	}

	// Collect deprecated modules for output
	var deprecatedModules []Module
	if *deprecatedFlag {
		for _, m := range allModules {
			if m.Deprecated != "" {
				if *directOnly && !m.Direct {
					continue
				}
				deprecatedModules = append(deprecatedModules, m)
			}
		}
	}

	// Filter stale modules (non-archived repos with old push dates)
	stale := filterStale(results)

	// Handle --tree mode
	if *treeFlag && hasArchived {
		graph, err := parseModGraph(filepath.Dir(gomodPath), *goVersionFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not run go mod graph: %v\n", err)
		} else {
			switch outputFormat {
			case "mermaid":
				PrintMermaid(results, graph, allModules)
			case "json":
				PrintTreeJSON(results, graph, allModules, fileMatches, nonGitHubModules, deprecatedModules)
			case "markdown":
				PrintMarkdownTree(results, graph, allModules, fileMatches)
				if len(stale) > 0 {
					PrintMarkdownStale(stale)
				}
				if len(deprecatedModules) > 0 {
					PrintMarkdown(nil, nil, false, deprecatedModules)
				}
				if len(nonGitHubModules) > 0 {
					PrintMarkdownSkipped(nonGitHubModules)
				}
			default:
				PrintTree(results, graph, allModules, fileMatches)
				if len(stale) > 0 {
					PrintStaleTable(stale)
				}
				if len(deprecatedModules) > 0 {
					PrintDeprecatedTable(deprecatedModules)
				}
				if len(nonGitHubModules) > 0 {
					PrintSkippedTable(nonGitHubModules)
				}
				if ageEnabled {
					PrintOutdatedTable(results, nonGitHubModules)
				}
				if *showIgnoredFlag {
					PrintIgnoredTable(ignoredResults)
				}
			}
			if hasArchived {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	// Output
	switch outputFormat {
	case "quickfix":
		if fileMatches != nil {
			PrintFilesPlain(results, fileMatches)
		}
	case "json":
		PrintJSON(results, nonGitHubModules, *allFlag, fileMatches, stale, deprecatedModules)
	case "markdown":
		PrintMarkdown(results, nonGitHubModules, *allFlag, deprecatedModules)
		if fileMatches != nil {
			PrintMarkdownFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintMarkdownStale(stale)
		}
	default:
		PrintTable(results, nonGitHubModules, *allFlag, deprecatedModules)
		if fileMatches != nil {
			PrintFiles(results, fileMatches)
		}
		if len(stale) > 0 {
			PrintStaleTable(stale)
		}
	}

	if ageEnabled {
		PrintOutdatedTable(results, nonGitHubModules)
	}

	if *showIgnoredFlag {
		PrintIgnoredTable(ignoredResults)
	}

	if hasArchived {
		os.Exit(1)
	}
}

// valueFlagNames lists flags that take a value argument (not boolean).
var valueFlagNames = map[string]bool{
	"-workers": true, "--workers": true,
	"-go-version": true, "--go-version": true,
	"-sort": true, "--sort": true,
	"-ignore-file": true, "--ignore-file": true,
	"-ignore": true, "--ignore": true,
	"-format": true, "--format": true,
	"-color-threshold": true, "--color-threshold": true,
}

// reorderArgs moves flags after positional arguments to before them,
// so Go's flag package can parse them. For example:
//
//	modrot path/to/go.mod --files --tree
//
// becomes:
//
//	modrot --files --tree path/to/go.mod
func reorderArgs() {
	var flags, positional []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			// If this flag takes a value and it's not using = syntax, consume the next arg too.
			if valueFlagNames[arg] && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}
	reordered := make([]string, 0, 1+len(flags)+len(positional))
	reordered = append(reordered, os.Args[0])
	reordered = append(reordered, flags...)
	reordered = append(reordered, positional...)
	os.Args = reordered
}

// goToolchainVersion returns the Go toolchain version string (e.g. "go1.23.4")
// by running `go version` and extracting the version token.
func goToolchainVersion() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "go (unknown)"
	}
	// Output format: "go version go1.23.4 darwin/arm64"
	fields := strings.Fields(string(out))
	for _, f := range fields {
		if strings.HasPrefix(f, "go1") || strings.HasPrefix(f, "go0") {
			return f
		}
	}
	return strings.TrimSpace(string(out))
}

// extractDurationFlag scans os.Args for --duration or -duration, which
// supports an optional date value (--duration or --duration=2026-01-01).
// Go's flag package doesn't handle optional-value flags, so we extract
// this flag before flag.Parse() and remove it from os.Args.
func extractDurationFlag() {
	var filtered []string
	for _, arg := range os.Args {
		switch {
		case arg == "--duration" || arg == "-duration":
			durationEnabled = true
			durationEndDate = time.Now()
		case strings.HasPrefix(arg, "--duration=") || strings.HasPrefix(arg, "-duration="):
			durationEnabled = true
			dateStr := arg[strings.Index(arg, "=")+1:]
			t, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid duration date %q (expected YYYY-MM-DD)\n", dateStr)
				os.Exit(2)
			}
			durationEndDate = t
		default:
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
}

// extractStaleFlag scans os.Args for --stale or -stale, which supports
// an optional threshold value (--stale or --stale=1y6m). Default threshold
// is 2y. Also auto-enables durationEnabled when --stale is used.
func extractStaleFlag() {
	var filtered []string
	for _, arg := range os.Args {
		switch {
		case arg == "--stale" || arg == "-stale":
			staleEnabled = true
			staleYears = 2
			durationEnabled = true
			if durationEndDate.IsZero() {
				durationEndDate = time.Now()
			}
		case strings.HasPrefix(arg, "--stale=") || strings.HasPrefix(arg, "-stale="):
			staleEnabled = true
			threshStr := arg[strings.Index(arg, "=")+1:]
			y, m, d, err := parseThreshold(threshStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid stale threshold %q (expected e.g. 2y, 1y6m, 180d)\n", threshStr)
				os.Exit(2)
			}
			staleYears = y
			staleMonths = m
			staleDays = d
			durationEnabled = true
			if durationEndDate.IsZero() {
				durationEndDate = time.Now()
			}
		default:
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
}

// extractAgeFlag scans os.Args for --age or -age, which supports
// an optional threshold value (--age or --age=18m). When a threshold
// is given, only dependencies with a version publish date older than the threshold
// are shown in the OUTDATED section.
func extractAgeFlag() {
	var filtered []string
	for _, arg := range os.Args {
		switch {
		case arg == "--age" || arg == "-age":
			ageEnabled = true
		case strings.HasPrefix(arg, "--age=") || strings.HasPrefix(arg, "-age="):
			ageEnabled = true
			threshStr := arg[strings.Index(arg, "=")+1:]
			y, m, d, err := parseThreshold(threshStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid age threshold %q (expected e.g. 1y6m, 18m, 180d)\n", threshStr)
				os.Exit(2)
			}
			ageYears = y
			ageMonths = m
			ageDays = d
		default:
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
}

// parseModGraph runs `go mod graph` in the given directory and returns
// a map of parent → []child (both as "module@version" strings).
// If goVersion is non-empty, GOTOOLCHAIN is set to force that Go version.
func parseModGraph(dir string, goVersion string) (map[string][]string, error) {
	cmd := exec.Command("go", "mod", "graph")
	cmd.Dir = dir
	if goVersion != "" {
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go"+goVersion)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	graph := make(map[string][]string)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		parent, child := parts[0], parts[1]
		graph[parent] = append(graph[parent], child)
	}
	return graph, scanner.Err()
}
