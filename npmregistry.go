package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// npm registry access. One request per unique package@version answers both of
// modrot's questions: the per-version document carries .deprecated and
// .repository together, and is small — about 1.7 KB, against 6.9 MB for a
// popular package's full packument.

// npmVersionInfo is the distilled per-version registry document.
type npmVersionInfo struct {
	Version    string
	Deprecated string
	RepoURL    string
}

// npmVersionDoc is the subset of the registry response modrot reads.
// Repository is polymorphic in the wild, so it stays raw until normalized.
type npmVersionDoc struct {
	Version    string          `json:"version"`
	Deprecated string          `json:"deprecated"`
	Repository json.RawMessage `json:"repository"`
}

// npmClient fetches and caches registry metadata.
type npmClient struct {
	client  *http.Client
	baseURL string

	mu    sync.Mutex
	cache map[string]*npmVersionInfo
}

// newNPMClient creates an npmClient with production defaults.
func newNPMClient() *npmClient {
	return &npmClient{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: "https://registry.npmjs.org",
		cache:   make(map[string]*npmVersionInfo),
	}
}

// slugRe matches a bare "owner/repo" repository shorthand.
var slugRe = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

// repositoryURL normalizes package.json's polymorphic "repository" field to a
// URL string. It accepts an object with a url, a plain URL string, npm's
// "github:owner/repo" shorthand, and a bare "owner/repo" slug.
func repositoryURL(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.URL != "" {
		return obj.URL
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(s, "github:"); ok {
		return "https://github.com/" + rest
	}
	if slugRe.MatchString(s) {
		return "https://github.com/" + s
	}
	return s
}

// fetch returns registry metadata for name@version. An empty version resolves
// through the registry's "latest" dist-tag.
//
// The second return value distinguishes the two ways of getting no data. A
// package or version the registry does not have returns (nil, true): that is
// a definitive answer, it is cached, and it simply yields no GitHub repo. A
// request that failed — network error, 429, 5xx — returns (nil, false): that
// is an absence of information, it is NOT cached so a later phase retries it,
// and the caller must tell the user their results are incomplete. Treating
// the two alike would let modrot report a clean scan while having silently
// skipped packages, which is the worst thing a rot detector can do.
func (c *npmClient) fetch(name, version string) (*npmVersionInfo, bool) {
	if version == "" {
		version = "latest"
	}
	key := name + "@" + version

	c.mu.Lock()
	if info, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return info, true
	}
	c.mu.Unlock()

	info, ok := c.get(name, version)
	if !ok {
		return nil, false // transient: leave uncached so it can be retried
	}

	c.mu.Lock()
	c.cache[key] = info
	c.mu.Unlock()
	return info, true
}

// get performs the uncached request. It reports ok=false only when the
// registry could not be consulted; a 404 is a definitive "no such package or
// version" and reports ok=true with nil info.
func (c *npmClient) get(name, version string) (*npmVersionInfo, bool) {
	url := fmt.Sprintf("%s/%s/%s", c.baseURL, name, version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, true // definitive: the registry has no such package/version
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	var doc npmVersionDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false
	}
	return &npmVersionInfo{
		Version:    doc.Version,
		Deprecated: doc.Deprecated,
		RepoURL:    repositoryURL(doc.Repository),
	}, true
}

// npmRegistry is the shared client for a run. Both the resolve and the
// deprecation phase read through it, so each package@version is fetched once.
var npmRegistry = newNPMClient()

// ResolveNPM populates Owner and Repo on npm modules from the registry's
// repository field, and fills in the resolved version for unlocked units.
// Returns the count resolved to a GitHub repo.
func ResolveNPM(modules []Module) int {
	return resolveNPMWithClient(modules, npmRegistry)
}

// CheckNPMDeprecations populates Deprecated on npm modules from the registry.
// Returns the count of deprecated packages found.
func CheckNPMDeprecations(modules []Module) int {
	return checkNPMDeprecationsWithClient(modules, npmRegistry)
}

// npmFetchAll fetches every distinct package@version in modules concurrently
// and applies apply to each module that has metadata. Returns the number of
// modules apply reported as changed, and the number of distinct packages the
// registry could not be consulted about.
func npmFetchAll(modules []Module, c *npmClient, apply func(m *Module, info *npmVersionInfo) bool) (int, int) {
	const maxWorkers = 20

	type key struct{ name, version string }
	locations := make(map[key][]int)
	for i := range modules {
		k := key{modules[i].Path, modules[i].Version}
		locations[k] = append(locations[k], i)
	}
	if len(locations) == 0 {
		return 0, 0
	}

	type result struct {
		k    key
		info *npmVersionInfo
		ok   bool
	}
	results := make(chan result, len(locations))
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for k := range locations {
		wg.Add(1)
		go func(k key) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			info, ok := c.fetch(k.name, k.version)
			results <- result{k: k, info: info, ok: ok}
		}(k)
	}

	wg.Wait()
	close(results)

	count, failed := 0, 0
	for res := range results {
		if !res.ok {
			failed++
			continue
		}
		if res.info == nil {
			continue // registry has no such package or version
		}
		for _, idx := range locations[res.k] {
			if apply(&modules[idx], res.info) {
				count++
			}
		}
	}
	return count, failed
}

// warnIncomplete tells the user how many packages could not be checked, so a
// clean-looking report is never mistaken for a complete one.
func warnIncomplete(failed int) {
	if failed == 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr,
		"Warning: could not reach the npm registry for %d %s; results are incomplete\n",
		failed, pluralize(failed, "package", "packages"))
}

// resolveNPMWithClient is the internal implementation, accepting a client so
// tests can point at a mock registry.
func resolveNPMWithClient(modules []Module, c *npmClient) int {
	resolved, failed := npmFetchAll(modules, c, func(m *Module, info *npmVersionInfo) bool {
		if m.Version == "" && info.Version != "" {
			m.Version = info.Version
			m.VersionInferred = true
		}
		m.SourceURL = info.RepoURL
		owner, repo := extractGitHubFromURL(info.RepoURL)
		if owner == "" {
			return false
		}
		m.Owner, m.Repo = owner, repo
		return true
	})
	warnIncomplete(failed)
	return resolved
}

// checkNPMDeprecationsWithClient is the internal implementation, accepting a
// client so tests can point at a mock registry.
func checkNPMDeprecationsWithClient(modules []Module, c *npmClient) int {
	found, failed := npmFetchAll(modules, c, func(m *Module, info *npmVersionInfo) bool {
		if info.Deprecated == "" {
			return false
		}
		m.Deprecated = info.Deprecated
		return true
	})
	// Report this phase's failures too, rather than assuming the resolve
	// phase already covered them. For an unlocked unit resolve fills in the
	// version, so this phase keys on a version resolve never fetched — its
	// failures are not a subset of resolve's. Warning twice about the same
	// package when the keys DO coincide is a far smaller harm than leaving
	// a package's deprecation status silently unchecked.
	warnIncomplete(failed)
	return found
}
