# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

modrot is a Go CLI tool that detects archived GitHub dependencies in Go projects. It parses `go.mod` files, queries the GitHub GraphQL API in batches, and reports which dependencies are archived, deprecated, or inactive.

## Build and Test Commands

Use `make` targets instead of raw `go` commands. Run `make` with no arguments to see all available targets.

```bash
make build                        # Build the binary
make test                         # Run all tests (with -race)
make coverage                     # Run tests with coverage report
go test -run TestFunctionName     # Run a single test
go test -v ./...                  # Verbose test output
```

## Code Quality and Security

```bash
make check                        # Run fmt + vet + lint
make lint                         # Run golangci-lint
make lint-fix                     # Run golangci-lint with auto-fix
make security                     # Run all security scans (govulncheck, trivy, gosec, gitleaks)
make verify                       # Run everything before commit (tidy + check + test + security)
```

## Lint Policy

All `fmt.Fprint`/`fmt.Fprintf`/`fmt.Fprintln` return values are explicitly discarded with `_, _ =`. All `defer resp.Body.Close()` and `defer f.Close()` use `defer func() { _ = resp.Body.Close() }()` to satisfy errcheck. Do not introduce unchecked return values — `make lint` must pass with 0 issues.

gosec excludes G204 (subprocess with variable) and G304 (file path variable) by default via the `GOSEC_EXCLUDE` Makefile variable, since these are expected for a CLI tool that invokes `rg` and reads user-specified files.

## Architecture

All code lives in the root package (`package main`), with no subdirectories. The codebase is organized into focused files:

- **main.go** — CLI entry point, flag parsing, orchestration. Uses `reorderArgs()` to allow flags after positional arguments (Go's `flag` package normally stops at the first non-flag arg).
- **modparse.go** — Parses go.mod files using `golang.org/x/mod/modfile`. Extracts GitHub owner/repo from module paths, handles versioned paths (`/v2`, `/v3`), and deduplicates modules by owner/repo.
- **github.go** — Queries GitHub GraphQL API in batches (default 50 repos/request). Checks `isArchived`, `archivedAt`, `pushedAt`. Auth via `gh auth token`.
- **resolve.go** — Resolves vanity imports (e.g., `google.golang.org/grpc`) to GitHub repos. Two-stage: tries Go module proxy first, falls back to HTML meta tag parsing.
- **deprecated.go** — Fetches go.mod from `proxy.golang.org` to find `// Deprecated:` comments on the module directive.
- **imports.go** — Uses ripgrep (`rg`) to find source files importing archived modules. Builds regex patterns with longest-prefix matching.
- **output.go** — Table (tabwriter), JSON, dependency tree, file listings, stale detection, quickfix output, and sorting. Largest file.
- **markdown.go** — GitHub-Flavored Markdown output format: tables, tree, files, stale sections.
- **mermaid.go** — Mermaid flowchart diagram generation from dependency graph. Shows paths to archived/deprecated deps with CSS classes.
- **ignore.go** — `.modrotignore` file parsing and `--ignore` inline filtering. `IgnoreList` type with `LoadIgnoreFile()`, `ParseIgnoreList()`, `BuildIgnoreList()`.
- **recursive.go** — Multi-module scanning: walks directory trees finding go.mod files, deduplicates repos globally across modules. Uses `runConfig` struct with `outputFormat` string to dispatch to format-specific output functions.

### Key Data Types

`Module` (modparse.go) holds parsed dependency info including GitHub owner/repo. `RepoStatus` (modparse.go) wraps a Module with archive status from GitHub. These two types flow through the entire pipeline.

### Exit Codes

- **0** — No archived dependencies found
- **1** — Archived dependencies found (useful for CI)
- **2** — Error

### External Tool Dependencies

- **gh** (GitHub CLI) — Required for GitHub API authentication
- **rg** (ripgrep) — Required only for the `--files` flag
- **go mod graph** — Used by the `--tree` flag

### Single External Go Dependency

The only non-stdlib dependency is `golang.org/x/mod` for go.mod parsing.
