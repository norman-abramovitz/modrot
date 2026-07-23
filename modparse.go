package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

// Module represents a parsed go.mod dependency.
type Module struct {
	Path          string // full module path, e.g. "github.com/foo/bar/v2"
	Version       string
	Direct        bool
	Line          int       // 1-based require line in the source go.mod (0 if unknown)
	Owner         string    // GitHub owner (empty if non-GitHub)
	Repo          string    // GitHub repo name (empty if non-GitHub)
	Deprecated    string    // deprecation message from go.mod, empty if not deprecated
	LatestVersion string    // latest version from proxy (empty if unavailable)
	VersionTime   time.Time // publish time of current version from proxy
	LatestTime    time.Time // publish time of latest version from proxy
	SourceURL     string    // VCS URL from proxy Origin.URL
}

// ParseGoMod reads and parses a go.mod file, returning all required modules.
func ParseGoMod(path string) ([]Module, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	var modules []Module
	for _, req := range f.Require {
		m := Module{
			Path:    req.Mod.Path,
			Version: req.Mod.Version,
			Direct:  !req.Indirect,
		}
		if req.Syntax != nil {
			m.Line = req.Syntax.Start.Line
		}
		m.Owner, m.Repo = extractGitHub(req.Mod.Path)
		modules = append(modules, m)
	}
	return modules, nil
}

// extractGitHub extracts the GitHub owner and repo from a module path.
// Returns ("", "") for non-GitHub modules.
// Handles paths like:
//   - github.com/foo/bar           → (foo, bar)
//   - github.com/foo/bar/v2        → (foo, bar)
//   - github.com/foo/bar/sdk/v2    → (foo, bar)
func extractGitHub(path string) (owner, repo string) {
	if !strings.HasPrefix(path, "github.com/") {
		return "", ""
	}
	parts := strings.SplitN(path, "/", 4) // ["github.com", owner, repo, ...]
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

// ModuleName reads the module path (the "module" directive) from a go.mod file.
func ModuleName(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", err
	}
	if f.Module == nil {
		return "", fmt.Errorf("no module directive in %s", path)
	}
	return f.Module.Mod.Path, nil
}

// GoModInfo reads the module path and go directive version from a go.mod file.
func GoModInfo(path string) (moduleName, goVersion string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", "", err
	}
	if f.Module != nil {
		moduleName = f.Module.Mod.Path
	}
	if f.Go != nil {
		goVersion = f.Go.Version
	}
	return moduleName, goVersion, nil
}

// FilterGitHub separates modules into GitHub and non-GitHub.
// GitHub modules are deduplicated by owner/repo.
func FilterGitHub(modules []Module, directOnly bool) (github []Module, nonGitHub []Module) {
	seen := make(map[string]bool)
	for _, m := range modules {
		if directOnly && !m.Direct {
			continue
		}
		if m.Owner == "" {
			nonGitHub = append(nonGitHub, m)
			continue
		}
		key := m.Owner + "/" + m.Repo
		if seen[key] {
			continue
		}
		seen[key] = true
		github = append(github, m)
	}
	return github, nonGitHub
}
