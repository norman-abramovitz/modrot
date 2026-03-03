# modrot

Detect archived GitHub dependencies in Go projects.

Parses your `go.mod`, queries the GitHub GraphQL API in batches, and reports which dependencies have been archived upstream.

## Install

### Homebrew

```bash
brew install norman-abramovitz/tap/modrot
```

### Go

```bash
go install github.com/norman-abramovitz/modrot@latest
```

### From source

```bash
git clone https://github.com/norman-abramovitz/modrot.git
cd modrot
go build -o modrot .
```

## Prerequisites

- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated — used to obtain your GitHub API token
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) — required only for `--files` flag

## Usage

```
modrot [flags] [path/to/go.mod | path/to/dir]
```

If no path is given, looks for `go.mod` in the current directory. You can also pass a directory path and the tool will look for `go.mod` inside it. Flags can appear before or after the path.

### Flags

**Output format:**

| Flag | Description |
|------|-------------|
| `--format FORMAT` | Output format: `table` (default), `json`, `markdown`, `mermaid`, `quickfix` |
| `--json` | Output as JSON (alias for `--format=json`) |
| `--markdown` | Output as GitHub-Flavored Markdown (alias for `--format=markdown`) |
| `--mermaid` | Output Mermaid flowchart diagram (alias for `--format=mermaid`) |
| `--quickfix` | Output `file:line:module` for editor quickfix (alias for `--format=quickfix`) |

**Filtering:**

| Flag | Description |
|------|-------------|
| `--direct-only` | Only check direct dependencies (skip indirect) |
| `--ignore-file PATH` | Path to ignore file (default: `.modrotignore` next to `go.mod`) |
| `--ignore MODULES` | Comma-separated list of module paths to ignore |
| `--stale[=THRESHOLD]` | Show dependencies not pushed in >THRESHOLD (default: `2y`, e.g. `1y6m`, `180d`) |

**Analysis:**

| Flag | Description |
|------|-------------|
| `--resolve` | Resolve vanity import paths to GitHub repos (e.g. `google.golang.org/grpc` → `github.com/grpc/grpc-go`) |
| `--deprecated` | Check for deprecated modules via the Go module proxy |
| `--duration[=DATE]` | Show how long dependencies have been archived (default: today) |

**Display:**

| Flag | Description |
|------|-------------|
| `--all` | Show all modules, not just archived ones |
| `--tree` | Show dependency tree for archived modules (uses `go mod graph`) |
| `--files` | Show source files that import archived modules (requires `rg`) |
| `--sort ORDER` | Sort archived modules: `name` (default), `duration`, `pushed` |
| `--time` | Include time in date output (2006-01-02 15:04:05 instead of 2006-01-02) |

**Execution:**

| Flag | Description |
|------|-------------|
| `--workers N` | Repos per GitHub GraphQL batch request (default 50) |
| `--go-version V` | Override the Go toolchain version from go.mod (e.g. `1.21.0`) |
| `--recursive` | Scan all go.mod files in the directory tree |

**Info:**

| Flag | Description |
|------|-------------|
| `--version` | Print version information and exit |

### Exit codes

- `0` — no archived dependencies found
- `1` — archived dependencies detected (useful in CI)
- `2` — error (bad path, parse failure, API error)

## Examples

### Default table output

```
$ modrot
Checking 234 GitHub modules...

ARCHIVED DEPENDENCIES (19 of 234 github.com modules)

MODULE                                     VERSION   DIRECT    ARCHIVED AT  LAST PUSHED
github.com/mitchellh/copystructure         v1.2.0    direct    2024-07-22   2021-05-05
github.com/mitchellh/mapstructure          v1.5.0    indirect  2024-07-22   2024-06-25
github.com/pkg/errors                      v0.9.1    indirect  2021-12-01   2021-11-02
...

Skipped 61 non-GitHub modules.
```

### Direct dependencies only

```
$ modrot --direct-only
Checking 83 GitHub modules...

ARCHIVED DEPENDENCIES (5 of 83 github.com modules)

MODULE                                     VERSION   DIRECT  ARCHIVED AT  LAST PUSHED
github.com/google/go-metrics-stackdriver   v0.2.0    direct  2024-12-03   2023-09-29
github.com/mitchellh/copystructure         v1.2.0    direct  2024-07-22   2021-05-05
github.com/mitchellh/go-testing-interface  v1.14.2   direct  2023-10-31   2021-08-21
github.com/mitchellh/pointerstructure      v1.2.1    direct  2024-07-22   2023-09-06
github.com/mitchellh/reflectwalk           v1.0.2    direct  2024-07-22   2022-04-21
```

### Dependency tree

Shows which direct dependencies transitively pull in archived modules:

```
$ modrot --tree
github.com/Masterminds/sprig/v3
  ├── github.com/mitchellh/copystructure [ARCHIVED]
  └── github.com/mitchellh/reflectwalk [ARCHIVED]
github.com/hashicorp/go-discover
  ├── github.com/Azure/go-autorest/autorest [ARCHIVED]
  ├── github.com/aws/aws-sdk-go [ARCHIVED]
  ├── github.com/denverdino/aliyungo [ARCHIVED]
  ├── github.com/nicolai86/scaleway-sdk [ARCHIVED]
  └── github.com/pkg/errors [ARCHIVED]
github.com/mitchellh/copystructure [ARCHIVED]
  └── github.com/mitchellh/reflectwalk [ARCHIVED]
```

### Source file scanning

Shows which source files import each archived module, helping prioritize replacements:

```
$ modrot --files
...
SOURCE FILES IMPORTING ARCHIVED MODULES

github.com/mitchellh/copystructure (10 files)
  audit/hashstructure.go:14
  sdk/logical/request.go:14
  vault/acl.go:21
  vault/mount_entry.go:14
  vault/policy.go:17
  ...

github.com/mitchellh/reflectwalk (1 file)
  audit/hashstructure.go:15

github.com/pkg/errors (0 files)
```

Combines with `--json` to add `source_files` arrays, or with `--tree` to append file counts:

```
$ modrot --files --tree
github.com/Masterminds/sprig/v3@v3.2.3
  ├── github.com/mitchellh/copystructure@v1.2.0 [ARCHIVED 2024-07-22] (10 files)
  └── github.com/mitchellh/reflectwalk@v1.0.2 [ARCHIVED 2024-07-22] (1 file)
```

### JSON output

```
$ modrot --json
{
  "archived": [
    {
      "module": "github.com/mitchellh/copystructure",
      "version": "v1.2.0",
      "direct": true,
      "owner": "mitchellh",
      "repo": "copystructure",
      "archived_at": "2024-07-22T20:44:18Z",
      "pushed_at": "2021-05-05T17:08:29Z"
    }
  ],
  "skipped_non_github": 61,
  "total_checked": 234
}
```

### JSON dependency tree

Combine `--tree --json` for a structured tree showing which direct dependencies pull in archived transitive deps:

```
$ modrot --tree --json
{
  "tree": [
    {
      "module": "github.com/Masterminds/sprig/v3",
      "version": "v3.2.3",
      "archived": false,
      "archived_dependencies": [
        {
          "module": "github.com/mitchellh/copystructure",
          "version": "v1.2.0",
          "archived_at": "2024-07-22T20:44:18Z",
          "pushed_at": "2021-05-05T17:08:29Z"
        },
        {
          "module": "github.com/mitchellh/reflectwalk",
          "version": "v1.0.2",
          "archived_at": "2024-07-22T20:48:05Z",
          "pushed_at": "2022-04-21T16:48:49Z"
        }
      ]
    },
    {
      "module": "github.com/mitchellh/copystructure",
      "version": "v1.2.0",
      "archived": true,
      "archived_at": "2024-07-22T20:44:18Z",
      "pushed_at": "2021-05-05T17:08:29Z",
      "archived_dependencies": []
    }
  ],
  "skipped_non_github": 61,
  "total_checked": 234
}
```

Add `--files` to include `source_files` arrays on archived entries.

### Recursive scanning

For multi-module repos, `--recursive` discovers all `go.mod` files in the directory tree, queries GitHub once for all unique repos, and outputs per-module results:

```
$ modrot --recursive --direct-only /path/to/project
Found 10 go.mod files, checking 90 unique GitHub repos...
=== api/go.mod — github.com/myorg/myapp/api/v2 ===

No archived dependencies found among 11 github.com modules.

=== go.mod — github.com/myorg/myapp ===

ARCHIVED DEPENDENCIES (5 of 83 github.com modules)

MODULE                                     VERSION   DIRECT  ARCHIVED AT  LAST PUSHED
github.com/mitchellh/copystructure         v1.2.0    direct  2024-07-22   2021-05-05
github.com/mitchellh/reflectwalk           v1.0.2    direct  2024-07-22   2022-04-21
...

=== sdk/go.mod — github.com/myorg/myapp/sdk/v2 ===

ARCHIVED DEPENDENCIES (3 of 34 github.com modules)
...
```

Skips `vendor/`, `testdata/`, and hidden directories. Combines with all other flags (`--json`, `--tree`, `--files`, etc.).

### Resolving vanity imports

By default, only `github.com/*` modules are checked. Many Go modules use vanity import paths (`google.golang.org/grpc`, `k8s.io/api`, `gopkg.in/yaml.v3`, `go.uber.org/zap`, etc.) that are actually hosted on GitHub. The `--resolve` flag resolves these to their real GitHub repos so they can be checked too:

```
$ modrot --resolve
Resolved 50 non-GitHub modules to GitHub repos.
Checking 265 GitHub modules...

ARCHIVED DEPENDENCIES (20 of 265 github.com modules)

MODULE                                     VERSION   DIRECT    ARCHIVED AT  LAST PUSHED
github.com/mitchellh/copystructure         v1.2.0    direct    2024-07-22   2021-05-05
gopkg.in/yaml.v2                           v2.4.0    indirect  2025-04-01   2025-04-01
...

Skipped 11 non-GitHub modules.
```

Without `--resolve`, the same project shows 215 GitHub modules checked and 61 skipped. With it, 50 of those 61 resolve to GitHub repos, leaving only 11 truly non-GitHub modules (mostly `golang.org/x/*` which lives on Google's own infrastructure).

Resolution uses two methods in sequence:
1. **Go module proxy** (`proxy.golang.org`) — fast, structured JSON with the repo's VCS URL
2. **HTML meta tags** — fallback for modules like `gopkg.in/*` where the proxy lacks origin info; parses `go-import` and `go-source` meta tags

Combines with all other flags. In `--recursive` mode, resolution is deduplicated across all go.mod files.

### Checking for deprecated modules

GitHub archival and Go module deprecation are independent signals. Many archived repos were never formally deprecated in their `go.mod`, and some modules like `github.com/golang/protobuf` are formally deprecated but not archived. The `--deprecated` flag checks for `// Deprecated:` comments in go.mod files via the Go module proxy:

```
$ modrot --deprecated
Found 2 deprecated modules.
Checking 234 GitHub modules...

ARCHIVED DEPENDENCIES (19 of 234 github.com modules)

MODULE                                     VERSION   DIRECT    ARCHIVED AT  LAST PUSHED
github.com/mitchellh/copystructure         v1.2.0    direct    2024-07-22   2021-05-05
...

DEPRECATED MODULES (2 modules)

MODULE                                                 VERSION  DIRECT    MESSAGE
github.com/Azure/azure-sdk-for-go/sdk/keyvault/azkeys  v0.10.0  indirect  use github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys instead
github.com/golang/protobuf                             v1.5.4   indirect  Use the "google.golang.org/protobuf" module instead.

Skipped 61 non-GitHub modules.
```

In JSON output, deprecated modules appear in a separate `"deprecated"` array with `deprecated_message` fields:

```
$ modrot --deprecated --json
{
  "archived": [ ... ],
  "deprecated": [
    {
      "module": "github.com/golang/protobuf",
      "version": "v1.5.4",
      "direct": false,
      "deprecated_message": "Use the \"google.golang.org/protobuf\" module instead."
    }
  ],
  "skipped_non_github": 61,
  "total_checked": 234
}
```

Deprecation checks all modules (not just GitHub ones), uses each module's exact version from go.mod, and respects `--direct-only`. In `--recursive` mode, proxy requests are deduplicated across go.mod files. In `--tree` mode, modules that are both archived and deprecated show `[DEPRECATED]` alongside `[ARCHIVED]`.

### Stale dependency detection

The `--stale` flag identifies dependencies that haven't been pushed to in a long time, even if they aren't archived:

```
$ modrot --stale
STALE DEPENDENCIES (3 modules not pushed in >2y)

MODULE                                     VERSION   DIRECT    LAST PUSHED  STALE
github.com/mitchellh/copystructure         v1.2.0    direct    2021-05-05   4y9m
github.com/mitchellh/reflectwalk           v1.0.2    direct    2022-04-21   3y10m
github.com/pkg/errors                      v0.9.1    indirect  2021-11-02   4y4m
```

Customize the threshold with a duration value:

```
$ modrot --stale=1y6m    # Stale if not pushed in 1 year 6 months
$ modrot --stale=180d    # Stale if not pushed in 180 days
```

Stale detection is informational only and does not affect the exit code.

### Markdown output

Generate GitHub-Flavored Markdown tables for embedding in reports or issues:

```
$ modrot --markdown
## ARCHIVED DEPENDENCIES

| Module | Version | Direct | Archived At | Last Pushed |
| --- | --- | --- | --- | --- |
| github.com/mitchellh/copystructure | v1.0.0 | direct | 2024-07-22 | 2021-05-05 |
```

Combines with `--tree`, `--files`, `--stale`, and `--all`.

### Mermaid diagrams

Generate [Mermaid](https://mermaid.js.org/) flowchart diagrams showing dependency paths to archived modules:

```
$ modrot --mermaid
graph TD
    root["mymodule"]
    n0["github.com/Masterminds/sprig/v3@v3.2.3"]
    n1["github.com/mitchellh/copystructure@v1.2.0"]:::archived
    n2["github.com/mitchellh/reflectwalk@v1.0.2"]:::archived
    root --> n0
    n0 --> n1
    n0 --> n2
    classDef archived fill:#f96,stroke:#333,stroke-width:2px
    classDef deprecated fill:#ff9,stroke:#333,stroke-width:2px
```

Only paths leading to archived or deprecated dependencies are included. Paste the output into any Mermaid-compatible renderer (GitHub, GitLab, Notion, etc.).

### Quickfix output

Generate editor-compatible quickfix output for navigating directly to files importing archived modules:

```
$ modrot --quickfix
audit/hashstructure.go:14:github.com/mitchellh/copystructure
sdk/logical/request.go:14:github.com/mitchellh/copystructure
audit/hashstructure.go:15:github.com/mitchellh/reflectwalk
```

Use with vim: `vim -q <(modrot --quickfix)`

### Ignoring dependencies

Create a `.modrotignore` file next to your `go.mod` to exclude specific modules:

```
# Modules we've evaluated and accepted
github.com/pkg/errors
github.com/mitchellh/mapstructure
```

Or use inline ignore:

```
$ modrot --ignore github.com/pkg/errors,github.com/mitchellh/mapstructure
```

Override the ignore file path with `--ignore-file`:

```
$ modrot --ignore-file path/to/ignorefile
```

### Sorting

Sort archived dependencies by name (default), archive duration, or last push date:

```
$ modrot --sort=duration    # Longest archived first
$ modrot --sort=pushed      # Least recently pushed first
```

## Development

Run `make` to see all available targets:

```
$ make
Usage:
  make <target>
  help                 Display this help message

Build
  build                Build the binary
  install              Install to GOPATH/bin

Testing
  test                 Run all tests
  coverage             Generate test coverage report
  coverage-html        Generate and open HTML coverage report

Code Quality
  fmt                  Format all Go source files
  vet                  Run go vet
  lint                 Run golangci-lint
  lint-fix             Run golangci-lint with auto-fix
  check                Run all code quality checks (fmt, vet, lint)

Dependencies
  tidy                 Tidy and verify go modules

Security
  govulncheck          Run vulnerability check on dependencies
  trivy                Run Trivy filesystem vulnerability scanner
  gosec                Run gosec security scanner
  gitleaks             Run gitleaks secret scanner
  security             Run all security scans

Verify
  verify               Run all checks before commit

Cleanup
  clean                Clean build artifacts
```

### Quick start

```bash
make build             # Build the binary
make test              # Run tests with race detection
make check             # Format, vet, and lint
make verify            # Run everything before committing
```

### Required tools

The following are required for code quality and security targets:

| Tool | Targets | Install |
|------|---------|---------|
| [golangci-lint](https://golangci-lint.run/) | `lint`, `lint-fix`, `check` | `brew install golangci-lint` |
| [trivy](https://aquasecurity.github.io/trivy/) | `trivy`, `security` | `brew install trivy` |
| [gitleaks](https://github.com/gitleaks/gitleaks) | `gitleaks`, `security` | `brew install gitleaks` |

`govulncheck` and `gosec` auto-install via `go install` if not found.

### Security scanning notes

gosec excludes G204 (subprocess launched with variable) and G304 (file inclusion via variable) by default, since these are expected for a CLI tool that invokes `rg` and reads user-specified file paths. To see all findings including these:

```bash
make gosec GOSEC_EXCLUDE=
```

### Build with version info

Matches what GoReleaser does for releases:

```bash
go build -ldflags "-X main.version=dev -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o modrot .
```

## Releasing

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions.

To create a release, tag and push:

```bash
git tag v1.2.3
git push origin v1.2.3
```

This triggers a GitHub Actions workflow that:

- Runs tests
- Builds cross-platform binaries (linux/darwin/windows/freebsd, amd64/arm64)
- Generates SHA-256 checksums
- Creates a GitHub release with changelog
- Updates the Homebrew formula in [norman-abramovitz/homebrew-tap](https://github.com/norman-abramovitz/homebrew-tap)

**Setup note:** The `HOMEBREW_TAP_TOKEN` repository secret must be set to a GitHub PAT with write access to the `homebrew-tap` repo, since `GITHUB_TOKEN` only has access to the current repository.

## How it works

1. Parses `go.mod` using `golang.org/x/mod/modfile`
2. Optionally resolves vanity import paths to GitHub repos via the Go module proxy and HTML meta tags (`--resolve`)
3. Optionally checks for deprecated modules via `proxy.golang.org/{module}/@v/{version}.mod` (`--deprecated`)
4. Extracts `owner/repo` from `github.com/*` module paths, deduplicating multi-path repos (e.g., `github.com/foo/bar/v2` and `github.com/foo/bar/sdk/v2`)
5. Batches repos into GitHub GraphQL queries (~50 per request) checking `isArchived`, `archivedAt`, and `pushedAt`
6. Non-GitHub modules that couldn't be resolved are skipped with a summary count

## Attribution

This project was built with the assistance of [Claude](https://claude.ai), an AI assistant by [Anthropic](https://www.anthropic.com).

## License

MIT
