package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
)

// CheckDeprecations fetches go.mod files from the proxy for all modules
// and populates Module.Deprecated with the deprecation message if present.
// Returns the count of deprecated modules found, and the count the proxy could
// not be consulted about.
func CheckDeprecations(modules []Module, maxWorkers int) (int, int) {
	return checkDeprecationsWithResolver(modules, maxWorkers, newResolver())
}

// checkDeprecationsWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func checkDeprecationsWithResolver(modules []Module, maxWorkers int, r *resolver) (int, int) {
	type result struct {
		idx     int
		message string
		ok      bool
	}
	results := make(chan result, len(modules))

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i := range modules {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			msg, ok := r.fetchGoModDeprecation(modules[idx].Path, modules[idx].Version)
			if !ok || msg != "" {
				results <- result{idx: idx, message: msg, ok: ok}
			}
		}(i)
	}

	wg.Wait()
	close(results)

	count, failed := 0, 0
	for res := range results {
		if !res.ok {
			failed++
			continue
		}
		modules[res.idx].Deprecated = res.message
		count++
	}
	warnIncompleteProxy(failed)
	return count, failed
}

// warnIncompleteProxy reports modules the Go module proxy could not be
// consulted about, mirroring warnIncomplete on the npm side.
func warnIncompleteProxy(failed int) {
	if failed == 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr,
		"Warning: could not reach the Go module proxy for %d %s; results are incomplete\n",
		failed, pluralize(failed, "module", "modules"))
}

// fetchGoModDeprecation fetches a module's go.mod from the proxy and
// extracts any "// Deprecated:" comment from the module directive.
// The second return value is false only when the proxy could not be consulted
// — a network error, a 429, a 5xx. A 404 or a 410 is a definitive "the proxy
// has no go.mod for this version" and reports true with an empty message, as
// does a module that simply carries no deprecation comment. Conflating the two
// is what let a proxy outage report a clean scan.
func (r *resolver) fetchGoModDeprecation(modulePath, version string) (string, bool) {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return "", true // a path this malformed is not a transient condition
	}

	url := fmt.Sprintf("%s/%s/@v/%s.mod", r.proxyBaseURL, escaped, version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", false
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return "", true // definitive: the proxy has no such module version
	case resp.StatusCode != http.StatusOK:
		return "", false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	return parseDeprecation(string(body)), true
}

// parseDeprecation extracts the deprecation message from a go.mod file body.
// Returns "" if no deprecation comment is found.
//
// The Go spec says the deprecation comment must contain "// Deprecated:"
// (case-sensitive, with colon). It can appear:
//  1. As a comment on the line immediately before the module directive
//  2. As an inline comment on the module directive line
func parseDeprecation(goModBody string) string {
	scanner := bufio.NewScanner(strings.NewReader(goModBody))
	var prevComment string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Track comment lines that might be deprecation comments.
		if strings.HasPrefix(line, "//") {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			if strings.HasPrefix(comment, "Deprecated:") {
				prevComment = strings.TrimSpace(strings.TrimPrefix(comment, "Deprecated:"))
			} else {
				prevComment = ""
			}
			continue
		}

		// Check if this is the module directive line.
		if strings.HasPrefix(line, "module ") || line == "module" {
			// Check for inline deprecation comment.
			if idx := strings.Index(line, "// Deprecated:"); idx >= 0 {
				msg := strings.TrimSpace(line[idx+len("// Deprecated:"):])
				return msg
			}

			// Check if the previous line was a deprecation comment.
			if prevComment != "" {
				return prevComment
			}
			return ""
		}

		// Reset previous comment tracker for non-comment, non-module lines.
		prevComment = ""
	}

	return ""
}
