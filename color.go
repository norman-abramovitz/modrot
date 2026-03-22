package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ANSI color codes — colorblind-safe palette.
const (
	colorReset = "\033[0m"

	colorBoldCyan      = "\033[1;36m"   // prominent: new
	colorCyan          = "\033[36m"     // progression
	colorYellow        = "\033[33m"     // middle
	colorMagenta       = "\033[35m"     // progression
	colorBoldMagentaUL = "\033[1;4;35m" // prominent: critical
)

// Color/symbol pairs ordered from newest to oldest.
// Prominent at both ends; progression in the middle.
var levelStyles = []struct {
	color  string
	symbol string
}{
	{colorBoldCyan, "★"},      // newest: just appeared
	{colorCyan, "◇"},          // recent: emerging
	{colorYellow, "◆"},        // moderate: established
	{colorMagenta, "▲"},       // old: growing concern
	{colorBoldMagentaUL, "✖"}, // critical: long-standing
}

// isTerminal returns true if stdout is a terminal (character device).
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// initColor sets up color support based on terminal detection and environment.
// Called after flag parsing with the user's threshold string (may be empty for defaults).
func initColor(cfg *Config, noColor bool, threshold string) error {
	// Disabled by flag or NO_COLOR env var
	if noColor || os.Getenv("NO_COLOR") != "" {
		cfg.Color.Enabled = false
		return nil
	}

	// Auto-detect: only enable for terminals
	if !isTerminal() {
		cfg.Color.Enabled = false
		return nil
	}

	cfg.Color.Enabled = true

	// Default: 3m,1y,2y,5y (non-linear, front-loaded for new issues)
	if threshold == "" {
		threshold = "3m,1y,2y,5y"
	}

	thresholds, err := parseColorThreshold(threshold)
	if err != nil {
		return err
	}
	cfg.Color.Thresholds = thresholds
	return nil
}

// parseColorThreshold parses a comma-separated threshold string with 2–4 values.
// Each part uses the same format as --stale (e.g. "1y", "1y6m", "180d").
//
//	2 values → 3 levels (new, middle, critical)
//	3 values → 4 levels (new, recent, old, critical)
//	4 values → 5 levels (new, recent, moderate, old, critical)
func parseColorThreshold(s string) ([]ColorThreshold, error) {
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, fmt.Errorf("invalid color threshold %q (expected 2–4 values e.g. 1y,3y or 3m,1y,2y,5y)", s)
	}

	thresholds := make([]ColorThreshold, len(parts))
	for i, p := range parts {
		y, m, d, err := parseThreshold(p)
		if err != nil {
			return nil, fmt.Errorf("invalid threshold %q: %w", p, err)
		}
		thresholds[i] = ColorThreshold{y, m, d}
	}

	return thresholds, nil
}

// classifyAge returns the level index (0 = newest) for a timestamp.
// Returns -1 if the timestamp is below all thresholds (no decoration).
// The number of possible levels is len(thresholds) + 1.
func classifyAge(cfg *Config, t time.Time) int {
	if t.IsZero() {
		return -1
	}
	// Check from oldest threshold to newest, return the highest matching level.
	for i := len(cfg.Color.Thresholds) - 1; i >= 0; i-- {
		th := cfg.Color.Thresholds[i]
		if exceedsThreshold(t, th.Y, th.M, th.D, cfg.Now) {
			return i + 1 // levels are 0-based: 0=newest, N=oldest
		}
	}
	return 0 // below first threshold = newest level
}

// selectStyle picks the color and symbol for a given level index,
// mapping N+1 levels onto the 5-entry style palette.
// Both ends are always prominent; middle levels are distributed evenly.
func selectStyle(level, totalLevels int) (string, string) {
	if totalLevels <= 1 {
		return levelStyles[0].color, levelStyles[0].symbol
	}
	// Map level (0..totalLevels-1) onto style index (0..4)
	idx := level * (len(levelStyles) - 1) / (totalLevels - 1)
	return levelStyles[idx].color, levelStyles[idx].symbol
}

// colorize wraps a string with color and symbol based on the age of a timestamp.
// Returns the string unchanged if color is disabled.
func colorize(cfg *Config, s string, t time.Time) string {
	if !cfg.Color.Enabled {
		return s
	}
	level := classifyAge(cfg, t)
	if level < 0 {
		return s
	}
	totalLevels := len(cfg.Color.Thresholds) + 1
	color, symbol := selectStyle(level, totalLevels)
	return fmt.Sprintf("%s%s %s%s", color, symbol, s, colorReset)
}
