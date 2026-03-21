package main

import "time"

// Config holds all program options parsed from command-line flags.
// Created once after flag parsing; passed by pointer to all functions.
type Config struct {
	// Output
	OutputFormat string // "table", "json", "markdown", "mermaid", "quickfix"
	DateFmt      string // "2006-01-02" or "2006-01-02 15:04:05"

	// Filtering
	DirectOnly   bool
	IgnoreFile   string
	IgnoreInline string
	ShowIgnored  bool
	NoIgnore     bool

	// Analysis
	Resolve    bool
	Deprecated bool
	Freshness  bool
	Duration   DurationConfig
	Stale      StaleConfig
	Age        AgeConfig

	// Display
	ShowAll     bool
	Tree        bool
	Files       bool
	Stats       bool
	SortMode    string // parsed: "name", "duration", "pushed"
	SortReverse bool

	// Color
	Color ColorConfig

	// Execution
	Workers     int
	GoVersion   string
	GoToolchain string
	Recursive   bool
}

// DurationConfig controls the --duration feature.
type DurationConfig struct {
	Enabled bool
	EndDate time.Time
}

// StaleConfig controls the --stale feature.
type StaleConfig struct {
	Enabled bool
	Years   int
	Months  int
	Days    int
}

// AgeConfig controls the --age feature.
type AgeConfig struct {
	Enabled bool
	Years   int
	Months  int
	Days    int
}

// ColorConfig holds the color/symbol feature state.
type ColorConfig struct {
	Enabled    bool
	Thresholds []ColorThreshold
}

// ColorThreshold holds a single parsed threshold (years, months, days).
type ColorThreshold struct {
	Y int
	M int
	D int
}

// NewDefaultConfig returns a Config with built-in defaults.
func NewDefaultConfig() *Config {
	return &Config{
		OutputFormat: "table",
		DateFmt:      "2006-01-02",
		SortMode:     "name",
		Workers:      50,
	}
}
