package main

import (
	"fmt"
	"os"
	"time"
)

// PrintStats outputs a summary of dependency health statistics.
func PrintStats(cfg *Config, results []RepoStatus, nonGHModules []Module, stale []RepoStatus, deprecatedModules []Module) {
	total := len(results) + len(nonGHModules)
	if total == 0 {
		return
	}

	// Count categories
	var archived, active, notFound int
	var archivedDirect, archivedIndirect int
	for _, r := range results {
		switch {
		case r.NotFound:
			notFound++
		case r.IsArchived:
			archived++
			if r.Module.Direct {
				archivedDirect++
			} else {
				archivedIndirect++
			}
		default:
			active++
		}
	}

	_, _ = fmt.Fprintf(os.Stderr, "\nSUMMARY\n\n")
	_, _ = fmt.Fprintf(os.Stdout, "Total modules checked:     %d\n", total)
	_, _ = fmt.Fprintf(os.Stdout, "  GitHub modules:          %d\n", len(results))
	_, _ = fmt.Fprintf(os.Stdout, "  Non-GitHub modules:      %d\n", len(nonGHModules))

	if archived > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Archived:                  %d (%.1f%%)\n", archived, pct(archived, total))
		_, _ = fmt.Fprintf(os.Stdout, "  Direct:                  %d\n", archivedDirect)
		if cfg.DirectOnly {
			_, _ = fmt.Fprintf(os.Stdout, "  Indirect:                not evaluated (--direct-only)\n")
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "  Indirect:                %d\n", archivedIndirect)
		}
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "Archived:                  0\n")
	}

	if len(stale) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Stale:                     %d (%.1f%%)\n", len(stale), pct(len(stale), total))
	}

	if len(deprecatedModules) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Deprecated:                %d (%.1f%%)\n", len(deprecatedModules), pct(len(deprecatedModules), total))
	}

	_, _ = fmt.Fprintf(os.Stdout, "Active:                    %d (%.1f%%)\n", active, pct(active, total))

	if notFound > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Not found:                 %d\n", notFound)
	}

	// Age distribution of archived modules
	if archived > 0 {
		printAgeDistribution(results)
	}
}

// printAgeDistribution shows a histogram of archived module ages.
func printAgeDistribution(results []RepoStatus) {
	now := time.Now()
	buckets := []struct {
		label string
		max   time.Time // archived before this date falls in bucket
		count int
	}{
		{"< 1 year", now.AddDate(-1, 0, 0), 0},
		{"1-2 years", now.AddDate(-2, 0, 0), 0},
		{"2-5 years", now.AddDate(-5, 0, 0), 0},
		{"> 5 years", time.Time{}, 0},
	}

	for _, r := range results {
		if !r.IsArchived || r.ArchivedAt.IsZero() {
			continue
		}
		switch {
		case r.ArchivedAt.After(buckets[0].max):
			buckets[0].count++
		case r.ArchivedAt.After(buckets[1].max):
			buckets[1].count++
		case r.ArchivedAt.After(buckets[2].max):
			buckets[2].count++
		default:
			buckets[3].count++
		}
	}

	_, _ = fmt.Fprintf(os.Stdout, "\nArchive age distribution:\n")
	maxCount := 0
	for _, b := range buckets {
		if b.count > maxCount {
			maxCount = b.count
		}
	}
	for _, b := range buckets {
		bar := ""
		if maxCount > 0 {
			barLen := b.count * 20 / maxCount
			for range barLen {
				bar += "█"
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %-10s %-20s %d\n", b.label, bar, b.count)
	}
}

// pct returns the percentage of part relative to total.
func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
