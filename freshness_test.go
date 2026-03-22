package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFormatBehind(t *testing.T) {
	tests := []struct {
		name string
		m    Module
		want string
	}{
		{
			name: "same version",
			m:    Module{Version: "v1.0.0", LatestVersion: "v1.0.0"},
			want: "-",
		},
		{
			name: "no latest version",
			m:    Module{Version: "v1.0.0"},
			want: "",
		},
		{
			name: "behind by 2 years",
			m: Module{
				Version:       "v1.0.0",
				LatestVersion: "v2.0.0",
				VersionTime:   time.Date(2022, 1, 15, 0, 0, 0, 0, time.UTC),
				LatestTime:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
			want: "2y",
		},
		{
			name: "behind by 2 years 4 months",
			m: Module{
				Version:       "v1.0.0",
				LatestVersion: "v2.0.0",
				VersionTime:   time.Date(2022, 1, 15, 0, 0, 0, 0, time.UTC),
				LatestTime:    time.Date(2024, 5, 15, 0, 0, 0, 0, time.UTC),
			},
			want: "2y4m",
		},
		{
			name: "behind by days only",
			m: Module{
				Version:       "v1.0.0",
				LatestVersion: "v1.1.0",
				VersionTime:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				LatestTime:    time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
			},
			want: "14d",
		},
		{
			name: "missing version time",
			m: Module{
				Version:       "v1.0.0",
				LatestVersion: "v2.0.0",
				LatestTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			want: "",
		},
		{
			name: "missing latest time",
			m: Module{
				Version:       "v1.0.0",
				LatestVersion: "v2.0.0",
				VersionTime:   time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBehind(tt.m)
			if got != tt.want {
				t.Errorf("formatBehind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompactDuration(t *testing.T) {
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want string
	}{
		{
			name: "same date",
			from: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "-",
		},
		{
			name: "to before from",
			from: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "-",
		},
		{
			name: "one year",
			from: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "1y",
		},
		{
			name: "mixed",
			from: time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC),
			to:   time.Date(2024, 7, 20, 0, 0, 0, 0, time.UTC),
			want: "2y4m5d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactDuration(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("compactDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCalcDurationBetween(t *testing.T) {
	from := time.Date(2022, 3, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 7, 20, 0, 0, 0, 0, time.UTC)
	y, m, d := calcDurationBetween(from, to)
	if y != 2 || m != 4 || d != 5 {
		t.Errorf("calcDurationBetween() = %d, %d, %d; want 2, 4, 5", y, m, d)
	}
}

func TestEnrichFreshness(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/foo/bar/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.5.0","Time":"2024-06-01T00:00:00Z"}`)
		case "/github.com/foo/bar/@v/v1.0.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v1.0.0","Time":"2023-01-15T00:00:00Z"}`)
		case "/github.com/baz/qux/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v2.0.0","Time":"2024-09-01T00:00:00Z"}`)
		case "/github.com/baz/qux/@v/v2.0.0.info":
			_, _ = fmt.Fprint(w, `{"Version":"v2.0.0","Time":"2024-09-01T00:00:00Z"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
		{Path: "github.com/baz/qux", Version: "v2.0.0", Owner: "baz", Repo: "qux"},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	enrichFreshnessWithResolver(modules, 4, r)

	// foo/bar: behind
	if modules[0].LatestVersion != "v1.5.0" {
		t.Errorf("foo/bar latest = %q, want v1.5.0", modules[0].LatestVersion)
	}
	if modules[0].LatestTime.IsZero() {
		t.Error("foo/bar LatestTime should be set")
	}
	if modules[0].VersionTime.IsZero() {
		t.Error("foo/bar VersionTime should be set (version != latest)")
	}

	// baz/qux: current == latest, no version info fetch needed
	if modules[1].LatestVersion != "v2.0.0" {
		t.Errorf("baz/qux latest = %q, want v2.0.0", modules[1].LatestVersion)
	}
	// version == latest → we skip fetchVersionInfo, so VersionTime stays zero
	if !modules[1].VersionTime.IsZero() {
		t.Error("baz/qux VersionTime should be zero (current == latest)")
	}
}

func TestEnrichFreshnessSkipsEnriched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	modules := []Module{
		{
			Path:          "golang.org/x/mod",
			Version:       "v0.17.0",
			LatestVersion: "v0.22.0", // already enriched
		},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	enrichFreshnessWithResolver(modules, 4, r)

	// Should not have made any requests
	if modules[0].LatestVersion != "v0.22.0" {
		t.Errorf("should not have changed LatestVersion, got %q", modules[0].LatestVersion)
	}
}

func TestEnrichFreshnessShortCircuit(t *testing.T) {
	var versionInfoCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/foo/bar/@latest":
			_, _ = fmt.Fprint(w, `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`)
		default:
			versionInfoCalled = true
			_, _ = fmt.Fprint(w, `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`)
		}
	}))
	defer srv.Close()

	modules := []Module{
		{Path: "github.com/foo/bar", Version: "v1.0.0", Owner: "foo", Repo: "bar"},
	}

	r := &resolver{client: srv.Client(), proxyBaseURL: srv.URL}
	enrichFreshnessWithResolver(modules, 4, r)

	if versionInfoCalled {
		t.Error("should not fetch version info when current == latest")
	}
	if modules[0].LatestVersion != "v1.0.0" {
		t.Errorf("latest = %q, want v1.0.0", modules[0].LatestVersion)
	}
}

func TestExceedsAgeThreshold(t *testing.T) {
	now := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	// No threshold set → always true
	cfg := &Config{Age: AgeConfig{Enabled: true}, Now: now}
	m := Module{VersionTime: now.AddDate(0, -1, 0)}
	if !exceedsAgeThreshold(cfg, m) {
		t.Error("no threshold should return true")
	}

	// 18m threshold, version 2 years old → exceeds
	cfg = &Config{Age: AgeConfig{Enabled: true, Months: 18}, Now: now}
	m = Module{VersionTime: now.AddDate(-2, 0, 0)}
	if !exceedsAgeThreshold(cfg, m) {
		t.Error("2y old version should exceed 18m threshold")
	}

	// 18m threshold, version 6 months old → does not exceed
	m = Module{VersionTime: now.AddDate(0, -6, 0)}
	if exceedsAgeThreshold(cfg, m) {
		t.Error("6m old version should not exceed 18m threshold")
	}

	// Zero version time → false
	m = Module{}
	if exceedsAgeThreshold(cfg, m) {
		t.Error("zero version time should return false")
	}
}

func TestFormatAgeThreshold(t *testing.T) {
	cfg := &Config{Age: AgeConfig{Years: 1, Months: 6}}
	if got := formatAgeThreshold(cfg); got != "1y6m" {
		t.Errorf("formatAgeThreshold() = %q, want %q", got, "1y6m")
	}

	cfg = &Config{Age: AgeConfig{Months: 18}}
	if got := formatAgeThreshold(cfg); got != "18m" {
		t.Errorf("formatAgeThreshold() = %q, want %q", got, "18m")
	}

	cfg = &Config{Age: AgeConfig{}}
	if got := formatAgeThreshold(cfg); got != "" {
		t.Errorf("formatAgeThreshold() = %q, want empty", got)
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	cfg := &Config{Now: now}
	m := Module{VersionTime: now.AddDate(-2, -3, -5)}
	got := formatAge(cfg, m)
	if got == "" || got == "-" {
		t.Errorf("formatAge() should return a duration, got %q", got)
	}

	// Zero time → empty
	m = Module{}
	if got := formatAge(cfg, m); got != "" {
		t.Errorf("formatAge(zero) = %q, want empty", got)
	}
}
