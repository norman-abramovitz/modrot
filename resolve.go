package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
)

// resolver holds HTTP client and configurable URLs for resolving vanity imports.
type resolver struct {
	client       *http.Client
	proxyBaseURL string // "https://proxy.golang.org" in production
}

// proxyInfo represents the JSON response from proxy.golang.org/{module}/@latest.
type proxyInfo struct {
	Version string `json:"Version"`
	Origin  *struct {
		VCS string `json:"VCS"`
		URL string `json:"URL"`
	} `json:"Origin"`
}

// metaRe matches <meta ...> tags in HTML.
var metaRe = regexp.MustCompile(`(?i)<meta\s+([^>]*)>`)

// attrRe extracts name="..." and content="..." from a meta tag's attributes.
var attrRe = regexp.MustCompile(`(?i)(name|content)\s*=\s*"([^"]*)"`)

// newResolver creates a resolver with production defaults.
func newResolver() *resolver {
	return &resolver{
		client:       &http.Client{Timeout: 10 * time.Second},
		proxyBaseURL: "https://proxy.golang.org",
	}
}

// ResolveVanityImports resolves non-GitHub modules to GitHub repos.
// It updates Owner/Repo in-place on each Module. Returns the count resolved.
func ResolveVanityImports(modules []Module, maxWorkers int) int {
	return resolveVanityImportsWithResolver(modules, maxWorkers, newResolver())
}

// resolveVanityImportsWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func resolveVanityImportsWithResolver(modules []Module, maxWorkers int, r *resolver) int {
	// Collect indices of non-GitHub modules.
	var indices []int
	for i := range modules {
		if modules[i].Owner == "" {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return 0
	}

	// Bounded worker pool.
	type result struct {
		idx         int
		owner, repo string
	}
	results := make(chan result, len(indices))

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, idx := range indices {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			owner, repo := r.resolveOne(modules[i].Path)
			if owner != "" {
				results <- result{idx: i, owner: owner, repo: repo}
			}
		}(idx)
	}

	wg.Wait()
	close(results)

	resolved := 0
	for res := range results {
		modules[res.idx].Owner = res.owner
		modules[res.idx].Repo = res.repo
		resolved++
	}
	return resolved
}

// resolveOne tries the Go module proxy first, then falls back to meta tags.
func (r *resolver) resolveOne(modulePath string) (owner, repo string) {
	owner, repo = r.resolveViaProxy(modulePath)
	if owner != "" {
		return owner, repo
	}
	return r.resolveViaMeta(modulePath)
}

// resolveViaProxy queries proxy.golang.org/{module}/@latest for Origin.URL.
func (r *resolver) resolveViaProxy(modulePath string) (owner, repo string) {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return "", ""
	}

	url := fmt.Sprintf("%s/%s/@latest", r.proxyBaseURL, escaped)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", ""
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return "", ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ""
	}

	var info proxyInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", ""
	}

	if info.Origin != nil && info.Origin.URL != "" {
		return extractGitHubFromURL(info.Origin.URL)
	}
	return "", ""
}

// resolveViaMeta fetches the module's vanity import page (?go-get=1)
// and parses go-import/go-source meta tags for GitHub URLs.
func (r *resolver) resolveViaMeta(modulePath string) (owner, repo string) {
	url := "https://" + modulePath + "?go-get=1"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", ""
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return "", ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ""
	}

	goImport, goSource := parseMetaTags(string(body))

	// Try go-import first: content is "prefix vcs repo-url"
	if goImport != "" {
		parts := strings.Fields(goImport)
		if len(parts) >= 3 {
			if o, r := extractGitHubFromURL(parts[2]); o != "" {
				return o, r
			}
		}
	}

	// Fall back to go-source: content is "prefix home dir-tpl file-tpl"
	if goSource != "" {
		parts := strings.Fields(goSource)
		for _, part := range parts {
			if o, r := extractGitHubFromURL(part); o != "" {
				return o, r
			}
		}
	}

	return "", ""
}

// extractGitHubFromURL parses a URL for github.com/owner/repo.
// Handles https://github.com/owner/repo, .git suffix, no scheme, etc.
func extractGitHubFromURL(rawURL string) (owner, repo string) {
	if rawURL == "" {
		return "", ""
	}

	// Normalize: strip scheme
	s := rawURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}

	// Must start with github.com/
	if !strings.HasPrefix(s, "github.com/") {
		return "", ""
	}

	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimRight(s, "/")

	parts := strings.SplitN(s, "/", 3) // owner, repo, [rest]
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// parseMetaTags extracts go-import and go-source content values from HTML.
func parseMetaTags(body string) (goImport, goSource string) {
	for _, match := range metaRe.FindAllStringSubmatch(body, -1) {
		attrs := match[1]
		pairs := attrRe.FindAllStringSubmatch(attrs, -1)
		var name, content string
		for _, p := range pairs {
			switch strings.ToLower(p[1]) {
			case "name":
				name = p[2]
			case "content":
				content = p[2]
			}
		}
		switch name {
		case "go-import":
			if goImport == "" {
				goImport = content
			}
		case "go-source":
			if goSource == "" {
				goSource = content
			}
		}
	}
	return goImport, goSource
}

// resolveAcrossModules resolves non-GitHub modules across multiple
// moduleInfo entries (for --recursive), deduplicating by module path.
// It updates Owner/Repo in-place on each Module. Returns the total count resolved.
func resolveAcrossModules(modules []moduleInfo) int {
	return resolveAcrossModulesWithResolver(modules, newResolver())
}

// resolveAcrossModulesWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func resolveAcrossModulesWithResolver(modules []moduleInfo, r *resolver) int {
	// Collect unique non-GitHub module paths and their locations.
	type location struct {
		miIdx  int // index into modules slice
		modIdx int // index into modules[miIdx].allModules
	}

	pathLocations := make(map[string][]location)
	for i := range modules {
		for j := range modules[i].allModules {
			m := &modules[i].allModules[j]
			if m.Owner == "" {
				pathLocations[m.Path] = append(pathLocations[m.Path], location{miIdx: i, modIdx: j})
			}
		}
	}

	if len(pathLocations) == 0 {
		return 0
	}

	// Build list of unique paths to resolve.
	uniquePaths := make([]string, 0, len(pathLocations))
	for p := range pathLocations {
		uniquePaths = append(uniquePaths, p)
	}

	// Resolve concurrently with bounded workers.
	type result struct {
		path        string
		owner, repo string
	}
	results := make(chan result, len(uniquePaths))

	const maxWorkers = 20
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, path := range uniquePaths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			owner, repo := r.resolveOne(p)
			if owner != "" {
				results <- result{path: p, owner: owner, repo: repo}
			}
		}(path)
	}

	wg.Wait()
	close(results)

	resolved := 0
	for res := range results {
		for _, loc := range pathLocations[res.path] {
			modules[loc.miIdx].allModules[loc.modIdx].Owner = res.owner
			modules[loc.miIdx].allModules[loc.modIdx].Repo = res.repo
		}
		resolved++
	}
	return resolved
}
