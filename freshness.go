package main

import (
	"fmt"
	"strings"
	"time"
)

// formatBehind returns a compact duration string representing the time
// between the current version's publish date and the latest version's
// publish date (e.g. "2y4m"). Returns "-" when current == latest version,
// and "" when data is unavailable.
func formatBehind(m Module) string {
	if m.LatestVersion == "" {
		return ""
	}
	if m.LatestVersion == m.Version {
		return "-"
	}
	if m.VersionTime.IsZero() || m.LatestTime.IsZero() {
		return ""
	}
	return compactDuration(m.VersionTime, m.LatestTime)
}

// formatAge returns a compact duration string representing the time
// between the module's version publish date and today (e.g. "3y1m").
// Returns "" when version time is unavailable.
func formatAge(cfg *Config, m Module) string {
	if m.VersionTime.IsZero() {
		return ""
	}
	return compactDuration(m.VersionTime, cfg.Now)
}

// exceedsAgeThreshold returns true if the module's version publish date
// is older than the age threshold relative to now. Returns false if
// no threshold is set (all zeros) or if version time is unavailable.
func exceedsAgeThreshold(cfg *Config, m Module) bool {
	if cfg.Age.Years == 0 && cfg.Age.Months == 0 && cfg.Age.Days == 0 {
		return true // no threshold → show all
	}
	if m.VersionTime.IsZero() {
		return false
	}
	return exceedsThreshold(m.VersionTime, cfg.Age.Years, cfg.Age.Months, cfg.Age.Days, cfg.Now)
}

// formatAgeThreshold formats the threshold as a compact string for display.
func formatAgeThreshold(cfg *Config) string {
	var parts []string
	if cfg.Age.Years > 0 {
		parts = append(parts, fmt.Sprintf("%dy", cfg.Age.Years))
	}
	if cfg.Age.Months > 0 {
		parts = append(parts, fmt.Sprintf("%dm", cfg.Age.Months))
	}
	if cfg.Age.Days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", cfg.Age.Days))
	}
	return strings.Join(parts, "")
}

// compactDuration computes a compact duration string between two times.
func compactDuration(from, to time.Time) string {
	if to.Before(from) || to.Equal(from) {
		return "-"
	}
	y, m, d := calcDurationBetween(from, to)
	var parts []string
	if y > 0 {
		parts = append(parts, fmt.Sprintf("%dy", y))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	if d > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dd", d))
	}
	return strings.Join(parts, "")
}

// calcDurationBetween computes calendar duration between two dates,
// similar to calcDuration but without the +1 day inclusiveness.
func calcDurationBetween(from, to time.Time) (years, months, days int) {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)

	years = t.Year() - f.Year()
	months = int(t.Month()) - int(f.Month())
	days = t.Day() - f.Day()

	if days < 0 {
		months--
		days += time.Date(t.Year(), t.Month(), 0, 0, 0, 0, 0, time.UTC).Day()
	}
	if months < 0 {
		years--
		months += 12
	}
	return years, months, days
}
