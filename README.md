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
| `--show-ignored` | Show ignored modules and their current state |
| `--no-ignore` | Disable ignore lists (`.modrotignore` and `--ignore`) |
| `--stale[=THRESHOLD]` | Show dependencies not pushed in >THRESHOLD (default: `2y`, e.g. `1y6m`, `180d`) |

**Analysis:**

| Flag | Description |
|------|-------------|
| `--resolve` | Resolve vanity import paths to GitHub repos (e.g. `google.golang.org/grpc` → `github.com/grpc/grpc-go`) |
| `--deprecated` | Check for deprecated modules via the Go module proxy |
| `--duration[=DATE]` | Show how long dependencies have been archived (default: today) |
| `--freshness` | Show latest available version and how far behind each dependency is (LATEST + BEHIND columns) |
| `--age[=THRESHOLD]` | Show how old each version is (AGE column); with threshold, show OUTDATED section (e.g. `18m`, `1y6m`) |

**Display:**

| Flag | Description |
|------|-------------|
| `--all` | Show all modules, not just archived ones |
| `--tree` | Show ASCII dependency tree for archived modules (uses `go mod graph`) |
| `--files` | Show source files that import archived modules (requires `rg`) |
| `--sort ORDER` | Sort: `name` (default asc), `duration` (default desc), `pushed` (default desc); append `:asc` or `:desc` to override |
| `--time` | Include time in date output (2006-01-02 15:04:05 instead of 2006-01-02) |

**Execution:**

| Flag | Description |
|------|-------------|
| `--workers N` | Repos per GitHub GraphQL batch request (default 50) |
| `--go-version V` | Override the Go toolchain version from go.mod (e.g. `1.21.0`) |
| `--recursive` | Scan all go.mod files in the directory tree |
| `--no-color` | Disable colored output (also respects `NO_COLOR` env var) |
| `--color-threshold T1,..,TN` | Age thresholds for color levels, 2–4 values (default: `3m,1y,2y,5y`) |

**Info:**

| Flag | Description |
|------|-------------|
| `--version` | Print version information and exit |

### Exit codes

- `0` — no archived dependencies found
- `1` — archived dependencies detected (useful in CI)
- `2` — error (bad path, parse failure, API error)

## Examples

### Quick scan

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

Focus on what you directly control with `--direct-only`:

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

Add `--time` to include timestamps in date columns (2024-07-22 20:44:18 instead of 2024-07-22).

### Deep analysis

The `--resolve` flag resolves vanity import paths (`google.golang.org/grpc`, `k8s.io/api`, `gopkg.in/yaml.v3`, etc.) to their real GitHub repos. The `--deprecated` flag checks for `// Deprecated:` comments in go.mod files via the Go module proxy. The `--stale` flag finds dependencies not pushed in a long time, even if not archived.

```
$ modrot --resolve --deprecated --stale=1y
Resolved 50 non-GitHub modules to GitHub repos.
Found 2 deprecated modules.
Checking 265 GitHub modules...

ARCHIVED DEPENDENCIES (20 of 265 github.com modules)

MODULE                                     VERSION   DIRECT    ARCHIVED AT  LAST PUSHED
github.com/mitchellh/copystructure         v1.2.0    direct    2024-07-22   2021-05-05
gopkg.in/yaml.v2                           v2.4.0    indirect  2025-04-01   2025-04-01
...

DEPRECATED MODULES (2 modules)

MODULE                                                 VERSION  DIRECT    MESSAGE
github.com/Azure/azure-sdk-for-go/sdk/keyvault/azkeys  v0.10.0  indirect  use github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys instead
github.com/golang/protobuf                             v1.5.4   indirect  Use the "google.golang.org/protobuf" module instead.

STALE DEPENDENCIES (3 modules not pushed in >1y)
...
```

These flags are independent and combine freely. Stale detection is informational only — it does not affect the exit code. Use `--stale=1y6m` or `--stale=180d` to customize the threshold (default: 2y).

### Version freshness and age

Two complementary flags measure different aspects of dependency currency:

**`--freshness`** adds LATEST and BEHIND columns — how far behind the latest available release:

```
$ modrot --freshness
...
MODULE                          VERSION   DIRECT  ARCHIVED AT  LAST PUSHED  LATEST    BEHIND
github.com/mitchellh/copystructure  v1.2.0  direct  2024-07-22  2021-05-05  v1.2.0    -
github.com/foo/bar              v1.2.0    direct  2023-01-01   2021-03-15   v1.5.0    2y4m
```

**`--age`** adds an AGE column — how old the version you're running is (today minus publish date):

```
$ modrot --age
...
MODULE                          VERSION   DIRECT  ARCHIVED AT  LAST PUSHED  AGE
github.com/foo/bar              v1.2.0    direct  2023-01-01   2021-03-15   3y1m
```

With a threshold, `--age=THRESHOLD` adds an OUTDATED DEPENDENCIES section listing modules whose version was published more than THRESHOLD ago:

```
$ modrot --age=18m --direct-only
...
OUTDATED DEPENDENCIES (3 modules with version published >18m ago)

MODULE                            VERSION   AGE      DIRECT  PUBLISHED
go.uber.org/goleak                v1.3.0    2y5m     direct  2023-10-24
gopkg.in/jcmturner/goidentity.v3  v3.0.0    7y7m     direct  2018-08-27
layeh.com/radius                  v0.0.0    2y6m     direct  2023-09-22
```

Combine both for the full picture:

```
$ modrot --freshness --age=18m --direct-only
```

Both flags are informational only — they do not affect the exit code.

### Dependency paths and impact

`--tree` shows an ASCII tree of which direct dependencies transitively pull in archived modules. `--files` shows which source files import them, helping prioritize replacements. These combine naturally:

```
$ modrot --tree --files
github.com/Masterminds/sprig/v3@v3.2.3
  ├── github.com/mitchellh/copystructure@v1.2.0 [ARCHIVED 2024-07-22] (10 files)
  └── github.com/mitchellh/reflectwalk@v1.0.2 [ARCHIVED 2024-07-22] (1 file)
github.com/hashicorp/go-discover
  ├── github.com/Azure/go-autorest/autorest [ARCHIVED]
  ├── github.com/aws/aws-sdk-go [ARCHIVED]
  └── github.com/pkg/errors [ARCHIVED]
```

`--mermaid` generates [Mermaid](https://mermaid.js.org/) flowchart diagrams showing paths to archived or deprecated dependencies. Paste the output into any Mermaid-compatible renderer (GitHub, GitLab, Notion, etc.):

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

### Developer workflow

**Verify after adding dependencies** — run modrot after `go get` to catch archived or stale packages before they get committed:

```
$ modrot --direct-only --stale
$ modrot --resolve --deprecated             # Full picture including vanity imports
```

**Evaluate a package before adopting it** — point modrot at another project's go.mod to assess its dependency health:

```
$ modrot /path/to/candidate/go.mod --resolve --deprecated --stale
$ modrot --all /path/to/candidate/go.mod    # See every dependency's status
```

### CI/CD integration

modrot exits 1 when archived dependencies are found, making it a natural CI gate:

**GitHub Actions:**

```yaml
- name: Check for archived dependencies
  run: modrot --direct-only
```

**Markdown output for release notes:**

```bash
modrot --markdown --all --deprecated > dependency-report.md
```

**JSON scripting with jq:**

```bash
# List archived module paths
modrot --json | jq -r '.archived[].module'

# Count archived direct dependencies
modrot --json | jq '[.archived[] | select(.direct)] | length'
```

**Editor quickfix** — navigate directly to files importing archived modules:

```
$ modrot --quickfix
audit/hashstructure.go:14:github.com/mitchellh/copystructure
sdk/logical/request.go:14:github.com/mitchellh/copystructure
audit/hashstructure.go:15:github.com/mitchellh/reflectwalk
```

Use with vim: `vim -q <(modrot --quickfix)`

### Output formats

**JSON:**

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

Combine `--tree --json` for a structured tree, or add `--files` to include `source_files` arrays. With `--deprecated`, a separate `"deprecated"` array is included.

**Markdown:**

```
$ modrot --markdown
## ARCHIVED DEPENDENCIES

| Module | Version | Direct | Archived At | Last Pushed |
| --- | --- | --- | --- | --- |
| github.com/mitchellh/copystructure | v1.0.0 | direct | 2024-07-22 | 2021-05-05 |
```

Combines with `--tree`, `--files`, `--stale`, and `--all`.

**Sorting** — sort archived dependencies by field and direction. Append `:asc` or `:desc` to control order. Each field has a natural default:

| Value | Result | Default? |
|-------|--------|----------|
| `name` | A→Z | yes (asc) |
| `name:desc` | Z→A | |
| `duration` | Archived longest ago first | yes (desc) |
| `duration:asc` | Archived most recently first | |
| `pushed` | Pushed longest ago first | yes (desc) |
| `pushed:asc` | Pushed most recently first | |

```
$ modrot --sort=duration         # Archived longest ago → most recently (default desc)
$ modrot --sort=duration:asc     # Archived most recently → longest ago
$ modrot --sort=pushed           # Oldest push date → newest (default desc)
$ modrot --sort=pushed:asc       # Newest push date → oldest
```

**Color indicators** — in table output, dates are color-coded by age using a colorblind-safe palette with symbols for accessibility. Colors are auto-enabled when stdout is a terminal and can be disabled with `--no-color` or the `NO_COLOR` environment variable. Both ends are prominent to highlight new issues and long-standing risks.

With the default thresholds (`3m,1y,2y,5y`), 5 levels are shown:

| Age | Symbol | Color | Meaning |
|-----|--------|-------|---------|
| < 3 months | ★ | bold cyan | Just appeared — evaluate impact |
| 3 months – 1 year | ◇ | cyan | Emerging — plan migration |
| 1 – 2 years | ◆ | yellow | Established — known tech debt |
| 2 – 5 years | ▲ | magenta | Growing concern — security risk |
| > 5 years | ✖ | bold magenta underline | Long-standing — legacy burden |

Provide 2–4 comma-separated thresholds to customize the number of levels:

```
$ modrot --color-threshold=3m,1y,2y,5y   # 5 levels (default)
$ modrot --color-threshold=6m,1y,3y      # 4 levels
$ modrot --color-threshold=1y,3y         # 3 levels
$ modrot --no-color                      # Disable colors entirely
$ NO_COLOR=1 modrot                      # Also disables colors
```

Colors apply to archived and stale table output only (not JSON, markdown, mermaid, or quickfix).

### Filtering and ignoring

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

Use `--show-ignored` to see what's being ignored and whether those modules are still active or have been archived:

```
$ modrot --show-ignored
```

Use `--no-ignore` to temporarily disable all ignore lists and see the full unfiltered results:

```
$ modrot --no-ignore
```

Override the Go toolchain version used for `go mod graph` with `--go-version`:

```
$ modrot --tree --go-version 1.21.0
```

### Multi-module repos

`--recursive` discovers all `go.mod` files in a directory tree, queries GitHub once for all unique repos, and outputs per-module results:

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
```

Skips `vendor/`, `testdata/`, and hidden directories. Combines with all other flags:

```
$ modrot --recursive --json --deprecated --resolve /path/to/monorepo
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
