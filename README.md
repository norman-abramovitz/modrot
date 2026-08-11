# modrot

Detect archived GitHub dependencies and deprecated packages in Go, npm, and Bun projects.

Parses your `go.mod` or `package.json`/lockfile, queries the GitHub GraphQL API in batches, and reports which dependencies have been archived upstream. A directory holding both `go.mod` and `package.json` is scanned as two units under one exit code — see [Go, npm, and Bun support](#go-npm-and-bun-support) below.

Archived dependencies no longer receive security patches, bug fixes, or compatibility updates. They can silently become liabilities — vulnerable to known exploits, incompatible with newer toolchains, or abandoned without a migration path. The sooner you know, the more options you have.

## Install

### Homebrew

```bash
brew trust norman-abramovitz/tap
brew install norman-abramovitz/tap/modrot
```

Homebrew 6.0+ requires third-party taps to be trusted before their code runs, so `brew trust` must come first. On older Homebrew the `brew trust` line is unnecessary (and unrecognized) — skip it and run `brew install` directly.

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

- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated — used to obtain your GitHub API token. After installing, run `gh auth login` to authenticate.
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) — required only for `--files` flag

npm and Bun scanning need no extra tool — modrot talks to `registry.npmjs.org` directly.

## Usage

```
modrot [flags] [path/to/go.mod | path/to/package.json | path/to/dir]
```

If no path is given, looks in the current directory for `go.mod` and/or `package.json`. You can also pass a directory path and the tool will look for manifests inside it. Flags can appear before or after the path.

### Flags

**Output format:**

| Flag | Description |
|------|-------------|
| `--format FORMAT` | Output format: `table` (default), `json`, `markdown`, `mermaid`, `quickfix`, `sarif` |
| `--json` | Output as JSON (alias for `--format=json`) |
| `--markdown` | Output as GitHub-Flavored Markdown (alias for `--format=markdown`) |
| `--mermaid` | Output Mermaid flowchart diagram (alias for `--format=mermaid`) — Go only, see below |
| `--quickfix` | Output `file:line:module` for editor quickfix (alias for `--format=quickfix`) |
| `--sarif` | Output SARIF 2.1.0 for GitHub code scanning (alias for `--format=sarif`) |

Five of the six formats — table, JSON, Markdown, quickfix, and SARIF — work for both Go and npm/Bun units. `--mermaid` is Go-only: it draws a dependency graph, and npm has no graph source wired up, so npm units are warned about and left out of the diagram.

**Filtering:**

| Flag | Description |
|------|-------------|
| `--direct-only` | Only check direct dependencies (skip indirect) |
| `--ignore-file PATH` | Path to ignore file (default: `.modrotignore` next to the unit's manifest) |
| `--ignore MODULES` | Comma-separated list of module paths to ignore |
| `--show-ignored` | Show ignored modules and their current state |
| `--no-ignore` | Disable ignore lists (`.modrotignore` and `--ignore`) |
| `--stale[=THRESHOLD]` | Show dependencies not pushed in >THRESHOLD (default: `2y`, e.g. `1y6m`, `180d`) — works for npm too, it reads GitHub `pushedAt`, not registry data |

**Analysis:**

| Flag | Description |
|------|-------------|
| `--resolve` | Resolve vanity import paths to GitHub repos (e.g. `google.golang.org/grpc` → `github.com/grpc/grpc-go`) — Go only; npm always resolves via each package's `repository` field |
| `--deprecated` | Check for deprecated modules — via the Go module proxy for Go, via the npm registry for npm/Bun |
| `--duration[=DATE]` | Show how long dependencies have been archived (default: today) |
| `--freshness` | Show latest available version and how far behind each dependency is (LATEST + BEHIND columns) — Go only, warns and skips npm units |
| `--age[=THRESHOLD]` | Show how old each version is (AGE column); with threshold, show OUTDATED section (e.g. `18m`, `1y6m`) — Go only, warns and skips npm units |

**Display:**

| Flag | Description |
|------|-------------|
| `--all` | Show all modules, not just archived ones |
| `--tree` | Show ASCII dependency tree for archived modules (uses `go mod graph`) — Go only, warns and falls back to flat output for npm units |
| `--files` | Show source files that import archived modules (requires `rg`) — scans `.go` for Go units and `.js/.jsx/.ts/.tsx/.mjs/.cjs/.vue/.svelte` for npm/Bun units |
| `--sort ORDER` | Sort: `name` (default asc), `duration` (default desc), `pushed` (default desc); append `:asc` or `:desc` to override |
| `--time` | Include time in date output (2006-01-02 15:04:05 instead of 2006-01-02) |

**Execution:**

| Flag | Description |
|------|-------------|
| `--workers N` | Repos per GitHub GraphQL batch request (default 50) |
| `--go-version V` | Override the Go toolchain version from go.mod (e.g. `1.21.0`) |
| `--recursive` | Scan all go.mod and package.json/lockfile units in the directory tree |
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

### Security audit

Archived dependencies no longer receive security patches. Combine flags for a comprehensive security-focused assessment:

```
$ modrot --resolve --deprecated --stale=6m --direct-only --freshness --age=1y
```

This checks direct dependencies for: archived repos (no patches), deprecated modules (known replacements exist), stale repos (no activity in 6 months), outdated versions (behind latest by time), and old versions (published over a year ago).

### Technical debt tracking

Run modrot periodically and save JSON snapshots to track dependency health over time:

```
$ modrot --resolve --deprecated --stale --freshness --json > reports/deps-$(date +%Y-%m-%d).json
```

Compare snapshots to see if debt is accumulating or being paid down. Use `jq` to extract trends:

```
$ jq '[(.modules // [.])[].archived[]?] | length' reports/deps-*.json
```

### Migration planning

When replacing an archived dependency, use `--tree --files` to understand the full impact:

```
$ modrot --tree --files --deprecated
```

`--tree` shows which direct dependencies transitively pull in the archived module. `--files` shows every source file that imports it. `--deprecated` shows the recommended replacement when available. Together they give you the scope of work before starting.

### Vendor evaluation

Before adopting a new library, check its dependency health:

```
$ modrot /path/to/candidate/go.mod --resolve --deprecated --stale --freshness --age=1y --all
```

Look for: archived direct dependencies (immediate risk), stale dependencies (may become archived), deprecated modules (migration debt you'd inherit), and old versions (maintainer may not be keeping up).

### CI/CD integration

modrot exits 1 when archived dependencies are found, making it a natural CI gate:

**GitHub Actions:**

```yaml
- name: Check for archived dependencies
  run: modrot --direct-only
```

**Testing strategies when incorporating tools that depend on external APIs:**

Tools like modrot require API access (GitHub) to produce full results. There are three approaches for CI integration:

1. **Structure validation (no API needed)** — verify the tool parses input correctly, produces valid output formats, and handles flags. Tests the tool without network access. Best for: unit tests, PR checks, fast feedback.

2. **Live API with skip control** — run the tool with real API access but gate with an environment variable (e.g. `MODROT_SKIP_CI=true`). Requires authentication in CI. Best for: scheduled full scans, release gates.

3. **Mock API responses** — inject test servers returning known responses. Tests the full pipeline without network dependency. Best for: integration tests that need deterministic results.

Most CI setups combine approach 1 (always runs, fast) with approach 2 (scheduled or on-demand, requires auth). Use environment variables to control execution, or `[skip ci]` in commit messages to bypass entire workflows.

**Markdown output for release notes:**

```bash
modrot --markdown --all --deprecated > dependency-report.md
```

**JSON scripting with jq:**

```bash
# `.modules // [.]` reads both JSON shapes: a scan that finds one manifest
# emits a flat document, one that finds several wraps them in `.modules`.

# List archived module paths
modrot --json | jq -r '(.modules // [.])[].archived[]?.module'

# Count archived direct dependencies
modrot --json | jq '[(.modules // [.])[].archived[]? | select(.direct)] | length'
```

**Editor quickfix** — navigate directly to each archived module's declaration line, then to the source files importing it. The declaration site is whichever manifest actually declares it: `go.mod` for a Go unit, `package.json` for a direct npm dependency, `package-lock.json` or `bun.lock` for a transitive one.

```
$ modrot --quickfix
go.mod:94:github.com/mitchellh/copystructure
audit/hashstructure.go:14:github.com/mitchellh/copystructure
sdk/logical/request.go:14:github.com/mitchellh/copystructure
go.mod:95:github.com/mitchellh/reflectwalk
audit/hashstructure.go:15:github.com/mitchellh/reflectwalk
```

Use with vim: `vim -q <(modrot --quickfix)`

Every path in the stream — manifest sites and source-file sites alike — is relative to the current working directory, the same base SARIF uses, so `vim -q` opens them from wherever modrot was invoked.

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

The document above is the **flat** shape, emitted when a scan finds exactly one manifest unit. When a scan finds more than one, results are **wrapped** per unit instead:

```
$ modrot --json
{
  "modules": [
    {
      "go_mod": "go.mod",
      "module_path": "github.com/myorg/myapp",
      "go_version": "go1.26.5",
      "archived": [ ... ],
      "non_github_count": 0,
      "total_checked": 0
    },
    {
      "go_mod": "package.json",
      "module_path": "myapp-tooling",
      "go_version": "npm, unlocked",
      "archived": [ ... ],
      "non_github_count": 0,
      "total_checked": 0
    }
  ]
}
```

The shape follows what the scan finds, not which flags were passed — so a Go repository with a root `package.json` (husky, commitlint, prettier) emits the wrapped shape without `--recursive`. Scripts that must handle either shape can normalize with `.modules // [.]`, as the jq examples above do.

**Markdown:**

```
$ modrot --markdown
## ARCHIVED DEPENDENCIES

| Module | Version | Direct | Archived At | Last Pushed |
| --- | --- | --- | --- | --- |
| github.com/mitchellh/copystructure | v1.0.0 | direct | 2024-07-22 | 2021-05-05 |
```

Combines with `--tree`, `--files`, `--stale`, and `--all`.

**SARIF** — upload findings to GitHub code scanning so archived and
deprecated dependencies appear in the repository's Security tab:

```
$ modrot --sarif --deprecated > modrot.sarif
```

```yaml
- run: modrot --sarif --deprecated > modrot.sarif || true
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: modrot.sarif
```

The `|| true` keeps the workflow running when modrot exits 1 on archived findings, so the upload step still runs. Each archived dependency is reported as a `warning` and each deprecated dependency as a `note`, anchored to the exact line that names it: the `require` line in `go.mod` for a Go unit, the `package.json` line for a direct npm dependency, the lockfile line (`package-lock.json` or `bun.lock`) for a transitive one. A dependency that appears in several manifests becomes one result with a location per site. SARIF paths are always relative to the current working directory, so run modrot from the repository root — e.g. `modrot --recursive --sarif . > modrot.sarif` — so the paths are repo-relative. Stale, age, and stats sections are not part of SARIF output.

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

Colors apply to archived and stale table output only (not JSON, markdown, mermaid, quickfix, or SARIF).

### Filtering and ignoring

Create a `.modrotignore` file next to your `go.mod` or `package.json` to exclude specific modules. Add an inline comment after `#` to document why each module is ignored — these reasons are shown by `--show-ignored`:

```
# Modules we've evaluated and accepted
github.com/pkg/errors              # Vendored replacement available
github.com/mitchellh/mapstructure  # Evaluated 2026-01: no security impact
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

### Go, npm, and Bun support

modrot scans Go, npm, and Bun dependencies in the same run. A directory holding a `go.mod` is a Go unit; a directory holding a `package.json` is an npm/Bun unit. A directory with both reports both, as two units under one exit code — a repo with only a `go.mod` is scanned exactly as before, unaffected by any of this.

**Manifests read:** `package.json` for the dependency list, plus a lockfile for exact versions: `package-lock.json` (npm, lockfile versions 1, 2, and 3) or `bun.lock` (Bun's JSONC-ish text lockfile). `yarn.lock` is not read. The older binary `bun.lockb` format is not read either — modrot warns and suggests running `bun install --save-text-lockfile` to produce a `bun.lock` it can parse.

If a package.json directory has both a `package-lock.json` and a `bun.lock`, modrot prefers `bun.lock` and warns that the npm lockfile was ignored, so a repo mid-migration between package managers gets a predictable, single-lockfile answer instead of a silently merged one.

With no lockfile at all, modrot falls back to resolving each dependency's version against the npm registry's `dist-tags.latest` — the header for that unit reads `(npm, unlocked)` and a note explains the fallback. This is inherently less precise than a lockfile: it reports what installing today would get you, not what's actually pinned.

Every distinct version of a package installed across your manifests is parsed and checked — npm's dependency resolution genuinely installs multiple versions of the same package side by side, and a deprecation notice is a fact about one specific version, not the package as a whole. The DEPRECATED table reports each of those versions separately. The ARCHIVED table does not: being archived is a fact about a repository, so several versions of one package collapse into a single row keyed on the GitHub repo they resolve to.

**What's Go-only in this release:** `--tree`, `--mermaid`, `--freshness`, and `--age` all warn and either fall back to flat output or skip the npm unit entirely — building a tree needs a dependency graph source, and npm has none wired up yet; freshness and age need the full npm packument, which isn't fetched. `--mermaid` is the strictest of the four: a Mermaid document is a graph and nothing else, so an npm unit is omitted from the diagram rather than rendered some other way. `--stale` does work for npm, since it reads GitHub's `pushedAt` on the resolved repo rather than anything registry-specific. `--direct-only`, `--sort`, `--stats`, `--files`, `.modrotignore`, and five of the six output formats (table, JSON, Markdown, quickfix, SARIF) work the same for both ecosystems.

If the npm registry can't be reached for some packages, modrot warns that results are incomplete rather than silently reporting a false all-clear.

A worked example against a small mixed repo — one Go module and one npm package with three deprecated dependencies, one of them (`circular-json`) also archived on GitHub:

```console
$ modrot --recursive --deprecated .
Resolved 2 npm dependencies to GitHub repos.
Found 3 deprecated npm packages.
Found 2 manifests, checking 4 unique GitHub repos...
=== go.mod — example.com/mixed (go1.26.5) ===

ARCHIVED DEPENDENCIES (1 of 2 github.com modules)

MODULE                       VERSION              DIRECT  ARCHIVED AT  LAST PUSHED
github.com/dgrijalva/jwt-go  v3.2.0+incompatible  direct  2022-05-21   2021-11-04

=== web/package.json — mixed-web (npm) ===

ARCHIVED DEPENDENCIES (1 of 2 github.com modules)

MODULE         VERSION  DIRECT  ARCHIVED AT  LAST PUSHED
circular-json  0.5.9    direct  2019-10-23   2019-05-27

DEPRECATED MODULES (3 modules)

MODULE         VERSION  DIRECT  MESSAGE
circular-json  0.5.9    direct  CircularJSON is in maintenance only, flatted is its successor.
left-pad       1.3.0    direct  use String.prototype.padStart()
xterm          5.3.0    direct  This package is now deprecated. Move to @xterm/xterm instead.

NON-GITHUB MODULES (1 non-GitHub module)

MODULE    VERSION  LATEST  DIRECT  PUBLISHED  SOURCE
left-pad  1.3.0            direct             git+ssh://git@github.com/stevemao/left-pad.git
```

Note the two `===` headers: one Go unit qualified by toolchain version, one npm unit qualified by `(npm)`. `left-pad` resolves to a repository, but its `git+ssh://` URL form is not parsed into an owner and repo pair, so it lands in NON-GITHUB MODULES — the same place an unresolvable Go module would appear. Its resolved source is still shown, so you can follow it up by hand.

### Multi-module repos

`--recursive` discovers all `go.mod` and `package.json`/lockfile units in a directory tree, queries GitHub once for all unique repos, and outputs per-unit results:

```
$ modrot --recursive --direct-only /path/to/project
Found 10 manifests, checking 90 unique GitHub repos...
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

Per-unit output is not exclusive to `--recursive` — any scan that finds more than one manifest unit uses it, including a single directory holding both a `go.mod` and a `package.json`. See [Output formats](#output-formats) for the two JSON shapes.

### Portfolio-wide scanning

To scan multiple independent repos, loop over them and aggregate JSON output:

```bash
for repo in ~/Projects/*/go.mod; do
  modrot --json --resolve --deprecated "$repo" 2>/dev/null
done | jq -s '[.[] | (.modules // [.])[].archived[]?] | group_by(.module) | map({module: .[0].module, count: length}) | sort_by(-.count)'
```

This identifies the most common archived dependencies across your portfolio. For repos that are monorepos, add `--recursive` to scan every manifest within each repo.

## Troubleshooting

**"failed to get GitHub token (is gh installed and authenticated?)"**
Install the [GitHub CLI](https://cli.github.com/) and run `gh auth login`.

**"Error: no manifest in ... could be parsed"**
Ensure the path points to a valid `go.mod` or `package.json`, or a directory containing one. A `Warning: skipping ...` line above names the file that failed and why.

**GitHub API rate limits**
modrot batches queries (default 50 repos per request) to minimize API calls. If you hit rate limits on very large projects, reduce the batch size with `--workers 20`.

**No archived dependencies found but you expected some**
Non-GitHub modules (e.g., `golang.org/x/*`, `k8s.io/*`) are listed separately as they cannot be checked for archive status via the GitHub API. Use `--resolve` to resolve vanity imports to their GitHub repos.

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

npm and Bun units follow the same shape: parse `package.json` plus `bun.lock` or `package-lock.json` (falling back to the registry's `dist-tags.latest` with no lockfile), resolve each dependency's GitHub repo from its `repository` field, query the npm registry for deprecation notices, then join the same GitHub archive check and reporting pipeline used for Go.

## Attribution

This project was built with the assistance of [Claude](https://claude.ai), an AI assistant by [Anthropic](https://www.anthropic.com).

## License

MIT
