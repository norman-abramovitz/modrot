package main

import (
	"bufio"
	"os"
	"strings"
)

// IgnoreList holds module paths that should be excluded from results.
type IgnoreList struct {
	paths map[string]bool
}

// NewIgnoreList creates an empty IgnoreList.
func NewIgnoreList() *IgnoreList {
	return &IgnoreList{paths: make(map[string]bool)}
}

// Add adds one or more module paths to the ignore list.
func (il *IgnoreList) Add(paths ...string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p != "" {
			il.paths[p] = true
		}
	}
}

// IsIgnored returns true if the module path is in the ignore list.
func (il *IgnoreList) IsIgnored(modulePath string) bool {
	return il.paths[modulePath]
}

// Len returns the number of ignored paths.
func (il *IgnoreList) Len() int {
	return len(il.paths)
}

// FilterResults splits results into filtered (kept) and ignored.
func (il *IgnoreList) FilterResults(results []RepoStatus) (filtered, ignored []RepoStatus) {
	for _, r := range results {
		if il.IsIgnored(r.Module.Path) {
			ignored = append(ignored, r)
		} else {
			filtered = append(filtered, r)
		}
	}
	return filtered, ignored
}

// LoadIgnoreFile reads a .modrotignore file and returns an IgnoreList.
// Returns an empty list (not an error) if the file doesn't exist.
// Format: one module path per line, # comments, blank lines skipped.
func LoadIgnoreFile(path string) (*IgnoreList, error) {
	il := NewIgnoreList()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return il, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		il.Add(line)
	}
	return il, scanner.Err()
}

// ParseIgnoreList parses a comma-separated string of module paths.
func ParseIgnoreList(commaSeparated string) *IgnoreList {
	il := NewIgnoreList()
	if commaSeparated == "" {
		return il
	}
	for _, p := range strings.Split(commaSeparated, ",") {
		il.Add(p)
	}
	return il
}

// BuildIgnoreList builds an IgnoreList from the ignore file next to gomodPath
// and inline ignores. If ignoreFile is empty, uses .modrotignore next to gomodPath.
func BuildIgnoreList(gomodDir, ignoreFile, ignoreInline string) *IgnoreList {
	ignoreList := NewIgnoreList()
	filePath := ignoreFile
	if filePath == "" {
		filePath = gomodDir + "/.modrotignore"
	}
	if il, err := LoadIgnoreFile(filePath); err == nil {
		for p := range il.paths {
			ignoreList.Add(p)
		}
	}
	if ignoreInline != "" {
		inline := ParseIgnoreList(ignoreInline)
		for p := range inline.paths {
			ignoreList.Add(p)
		}
	}
	return ignoreList
}
