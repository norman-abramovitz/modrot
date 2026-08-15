# Changelog

All notable changes to modrot are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, breaking changes may land in a minor release; they
are always called out under **BREAKING** below.

## [Unreleased]

## [0.10.0] - 2026-08-14

### Added

- npm and Bun support. modrot now discovers `package.json` alongside `go.mod`
  and reports archived and deprecated JavaScript dependencies with the same
  flags, output formats and exit codes as Go.
- Lockfile parsing for `package-lock.json` v1, v2 and v3, and for `bun.lock`
  (JSONC, including comments and trailing commas).
- Deprecation detection for npm packages via the npm registry, reported in the
  same section as Go module deprecations.
- Archival detection for npm packages, by reading each package's `repository`
  field and routing the result through the existing GitHub batching — an npm
  scan costs no additional GitHub requests per package.
- JavaScript and TypeScript import scanning for `--files` and `--quickfix`,
  covering static `import`, dynamic `import()`, `require()` and re-exports.
- Findings anchor to the manifest line that declares them: `package.json` for a
  direct dependency, the lockfile for a transitive one.
- A warning when a flag does not apply to an ecosystem in the scan, rather than
  silently producing nothing. `--tree`, `--mermaid` and `--freshness` remain
  Go-only.
- A warning when a manifest is found but cannot be parsed, distinct from the
  message for no manifest at all.
- A mark on any version modrot resolved itself. A dependency the lockfile does
  not pin is reported at the registry's `dist-tags.latest`, which is pinned
  nowhere in the repo; it now renders as `~5.3.0` in tables and Markdown, sets
  `"version_inferred": true` in JSON, and carries an explanatory clause in
  SARIF.
- A warning naming the packages the npm registry has no entry for. Dependencies
  declared with a specifier the registry cannot answer for — `workspace:`,
  `file:`, `link:`, `portal:`, `patch:`, `catalog:`, git and URL specifiers,
  GitHub shorthand, and `npm:` aliases — are classified at parse time and never
  looked up, so the remaining 404s mean something: a dependency that should be
  published and is not. The exit code is unchanged.

### Changed

- **BREAKING** — `--json` emits the wrapped document `{"modules": [...]}`
  whenever a scan finds more than one manifest unit, where it previously emitted
  the flat document for any scan without `--recursive`. A Go repository with a
  root `package.json` — husky, commitlint, prettier, semantic-release — is now
  two units, so the shape changes with no change to the command line. Existing
  `jq` pipelines that read `.archived` directly fail with
  `Cannot iterate over null` and exit 5.

  Read both shapes with a `.modules // [.]` prefix:

  ```bash
  modrot --json | jq -r '(.modules // [.])[].archived[]?.module'
  ```

- **BREAKING** — the per-unit `--json` keys are renamed to ecosystem-neutral
  names: `go_mod` becomes `manifest`, `go_version` becomes `toolchain`. Both
  were named when Go was the only ecosystem, and held `"package.json"` and
  `"npm, unlocked"` for npm units. Bundled into the shape change above so
  consumers migrate once.
- Manifest units are reported in path order, so unit order is stable across
  runs in text, in `--json` `modules[]`, and in `--sarif` `results[]`, where the
  first location anchors a code-scanning alert.
- `--json` no longer stamps a Go toolchain version onto npm units.

### Fixed

- The Go import scanner reported only the first import on a line, so a source
  line importing two archived modules produced one finding instead of two.
  Present since `--files` was introduced.
- `--age` never reached GitHub-hosted modules, and `--freshness` left LATEST and
  BEHIND blank for the same modules. Two causes: the GitHub filter copied module
  values before the freshness pass wrote the publish time, and the freshness
  lookup was skipped whenever the current version was already the latest —
  zeroing the field for exactly the dormant dependencies `--age` exists to find.
  Fixed with no additional requests.
- `--quickfix` and `--sarif` disagreed on which directory their paths were
  relative to. Quickfix paths are now anchored to the working directory, and
  every path in one output stream shares a single base.
- Enrichment results were fanned back out to each dependency's locations by
  copying an enumerated list of fields, so any field added later was set on the
  representative and then silently discarded before reaching output. This is the
  same defect that left `--age` blank for GitHub-hosted modules; the copy is now
  a single function with a test that pins the list.

## [0.9.0] - 2026-07-23

### Added

- SARIF 2.1.0 output via `--sarif` and `--format sarif`, for upload to GitHub
  code scanning. Covers archived and deprecated findings, and works in
  `--recursive` mode.
- Duplicate findings for the same rule and module merge into a single result
  with multiple locations, rather than one result per location.

### Changed

- Recursive SARIF paths are anchored relative to the working directory.
- An empty SARIF document is emitted when no findings are possible, so a clean
  scan uploads a valid run.

### Fixed

- Documented the Homebrew tap trust requirement for installation.

## [0.8.0] - 2026-06-12

### Changed

- Upgraded to Go 1.26 and `x/mod` v0.37.0.
- Upgraded CI to Node 24.

## [0.7.0] - 2026-04-09

### Added

- `--freshness` flag, showing the latest available version and how far behind
  each dependency is, with an optional threshold.
- `--age` flag, split out from `--freshness`, for reporting version age.
- `--stats` flag, showing summary statistics with an age distribution.
- Fixture-based pipeline tests, so CI runs the full pipeline without API access.
- An injectable GitHub client and deterministic time, making tests repeatable.
- CI workflow on push and pull request, and the `MODROT_SKIP_CI` environment
  variable.
- Integration tests and CI testing strategy documentation.
- Use-case documentation and ignore reasons.

### Changed

- Program state unified into a single `Config` struct.
- Reduced complexity in `main()` and in the output and markdown builders, via
  shared column builders.
- Improved first-time user experience: troubleshooting guidance, motivation, and
  help detection.
- Homebrew releases publish as a cask.

## [0.6.0] - 2026-03-10

### Added

- `--show-ignored` and `--no-ignore` flags.
- Flexible color threshold levels, accepting 2 to 4 values.
- Sort direction suffixes `:asc` and `:desc`.
- Colorblind-safe age indicators for dates.

## [0.5.1] - 2026-03-03

### Changed

- README examples restructured into use-case groups, and improved CLI help text.

## [0.5.0] - 2026-03-03

### Changed

- Makefile is self-documenting, with coverage, lint, security and verify targets.
- All errcheck and staticcheck lint issues resolved across production and test
  code.

## [0.4.0] - 2026-03-02

### Added

- Markdown, Mermaid and quickfix output formats.
- Stale detection via `--stale`.
- Ignore lists, through `.modrotignore` and `--ignore`.
- Sorting via `--sort`.

## [0.3.0] - 2026-02-21

### Added

- Module header, `--duration` flag, and Go toolchain display.
- Non-GitHub modules are enriched with Go module proxy data.

### Changed

- The skipped-modules section is now titled NON-GITHUB MODULES.
- Test coverage raised from 52.8% to 70% via a worker pool refactor.

## [0.2.0] - 2026-02-21

First tagged release, as `modrot`.

### Added

- Archived dependency detection for `go.mod` via the GitHub GraphQL API.
- `--deprecated` flag, checking module deprecation through the Go module proxy.
- `--resolve` flag, resolving vanity import paths to GitHub repositories.
- `--recursive` flag, scanning every `go.mod` in a directory tree.
- `--files` flag, showing source files that import archived modules.
- `--tree` and combined `--tree --json` output.
- `--time`, `--go-version` and `--version` flags.
- Directory paths accepted as an argument, not just a `go.mod` file.
- Non-GitHub module details shown instead of only a count.
- GitHub Actions release workflow.
- MIT license.

### Changed

- Renamed from `go-mod-archived` to `modrot`, and the module path moved to
  `github.com/norman-abramovitz/modrot`.

### Fixed

- Flags were ignored when placed after the `go.mod` path.
- `--deprecated` findings were missing from `--tree --json` output.

[Unreleased]: https://github.com/norman-abramovitz/modrot/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/norman-abramovitz/modrot/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/norman-abramovitz/modrot/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/norman-abramovitz/modrot/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/norman-abramovitz/modrot/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/norman-abramovitz/modrot/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/norman-abramovitz/modrot/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/norman-abramovitz/modrot/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/norman-abramovitz/modrot/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/norman-abramovitz/modrot/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/norman-abramovitz/modrot/releases/tag/v0.2.0
