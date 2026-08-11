package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
)

// CheckDeprecations fetches go.mod files from the proxy for all modules
// and populates Module.Deprecated with the deprecation message if present.
// Returns count of deprecated modules found.
func CheckDeprecations(modules []Module, maxWorkers int) int {
	return checkDeprecationsWithResolver(modules, maxWorkers, newResolver())
}

// checkDeprecationsWithResolver is the internal implementation that accepts
// a resolver, allowing tests to inject mock HTTP servers.
func checkDeprecationsWithResolver(modules []Module, maxWorkers int, r *resolver) int {
	type result struct {
		idx     int
		message string
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

			msg := r.fetchGoModDeprecation(modules[idx].Path, modules[idx].Version)
			if msg != "" {
				results <- result{idx: idx, message: msg}
			}
		}(i)
	}

	wg.Wait()
	close(results)

	count := 0
	for res := range results {
		modules[res.idx].Deprecated = res.message
		count++
	}
	return count
}

// fetchGoModDeprecation fetches a module's go.mod from the proxy and
// extracts any "// Deprecated:" comment from the module directive.
func (r *resolver) fetchGoModDeprecation(modulePath, version string) string {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return ""
	}

	url := fmt.Sprintf("%s/%s/@v/%s.mod", r.proxyBaseURL, escaped, version)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ""
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	return parseDeprecation(string(body))
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
