package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/mod/module"
)

// versionInfo represents the JSON response from proxy.golang.org/{module}/@v/{version}.info.
type versionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

// EnrichNonGitHub enriches non-GitHub modules in-place with data from the Go module proxy.
// For each module where Owner == "", it fetches:
//   - /@latest → LatestVersion and SourceURL
//   - /@v/{version}.info → VersionTime
func EnrichNonGitHub(modules []Module, maxWorkers int) {
	enrichNonGitHubWithResolver(modules, maxWorkers, newResolver())
}

// enrichNonGitHubWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func enrichNonGitHubWithResolver(modules []Module, maxWorkers int, r *resolver) {
	// Collect indices of non-GitHub modules.
	var indices []int
	for i := range modules {
		if modules[i].Owner == "" {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return
	}

	type result struct {
		idx           int
		latestVersion string
		sourceURL     string
		versionTime   time.Time
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

			m := modules[i]
			res := result{idx: i}
			res.latestVersion, res.sourceURL = r.fetchLatestInfo(m.Path)
			res.versionTime = r.fetchVersionInfo(m.Path, m.Version)
			results <- res
		}(idx)
	}

	wg.Wait()
	close(results)

	for res := range results {
		modules[res.idx].LatestVersion = res.latestVersion
		modules[res.idx].SourceURL = res.sourceURL
		modules[res.idx].VersionTime = res.versionTime
	}
}

// enrichAcrossModules enriches non-GitHub modules across multiple moduleInfo
// entries (for --recursive), deduplicating by module path+version.
func enrichAcrossModules(modules []moduleInfo) {
	enrichAcrossModulesWithResolver(modules, newResolver())
}

// enrichAcrossModulesWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func enrichAcrossModulesWithResolver(modules []moduleInfo, r *resolver) {
	type location struct {
		miIdx  int
		modIdx int
	}

	type modKey struct {
		path    string
		version string
	}

	keyLocations := make(map[modKey][]location)
	for i := range modules {
		for j := range modules[i].nonGHModules {
			m := &modules[i].nonGHModules[j]
			if m.Owner == "" {
				key := modKey{path: m.Path, version: m.Version}
				keyLocations[key] = append(keyLocations[key], location{miIdx: i, modIdx: j})
			}
		}
	}

	if len(keyLocations) == 0 {
		return
	}

	type enrichResult struct {
		key           modKey
		latestVersion string
		sourceURL     string
		versionTime   time.Time
	}
	results := make(chan enrichResult, len(keyLocations))

	const maxWorkers = 20
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for k := range keyLocations {
		wg.Add(1)
		go func(key modKey) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := enrichResult{key: key}
			res.latestVersion, res.sourceURL = r.fetchLatestInfo(key.path)
			res.versionTime = r.fetchVersionInfo(key.path, key.version)
			results <- res
		}(k)
	}

	wg.Wait()
	close(results)

	for res := range results {
		for _, loc := range keyLocations[res.key] {
			modules[loc.miIdx].nonGHModules[loc.modIdx].LatestVersion = res.latestVersion
			modules[loc.miIdx].nonGHModules[loc.modIdx].SourceURL = res.sourceURL
			modules[loc.miIdx].nonGHModules[loc.modIdx].VersionTime = res.versionTime
		}
	}
}

// fetchLatestInfo queries proxy.golang.org/{module}/@latest and returns the
// latest version and the VCS source URL from Origin.URL.
func (r *resolver) fetchLatestInfo(modulePath string) (latestVersion, sourceURL string) {
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

	latestVersion = info.Version
	if info.Origin != nil && info.Origin.URL != "" {
		sourceURL = info.Origin.URL
	}
	return latestVersion, sourceURL
}

// fetchVersionInfo queries proxy.golang.org/{module}/@v/{version}.info and
// returns the publish timestamp of that version.
func (r *resolver) fetchVersionInfo(modulePath, version string) time.Time {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return time.Time{}
	}

	url := fmt.Sprintf("%s/%s/@v/%s.info", r.proxyBaseURL, escaped, version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return time.Time{}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return time.Time{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return time.Time{}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Time{}
	}

	var info versionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return time.Time{}
	}

	return info.Time
}
