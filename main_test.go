package main

import (
	"os"
	"testing"
	"time"
)

func TestReorderArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "flags already before positional",
			args: []string{"cmd", "--files", "--tree", "path/go.mod"},
			want: []string{"cmd", "--files", "--tree", "path/go.mod"},
		},
		{
			name: "flags after positional",
			args: []string{"cmd", "path/go.mod", "--files", "--tree"},
			want: []string{"cmd", "--files", "--tree", "path/go.mod"},
		},
		{
			name: "mixed flags and positional",
			args: []string{"cmd", "--json", "path/go.mod", "--files", "--tree"},
			want: []string{"cmd", "--json", "--files", "--tree", "path/go.mod"},
		},
		{
			name: "no flags",
			args: []string{"cmd", "path/go.mod"},
			want: []string{"cmd", "path/go.mod"},
		},
		{
			name: "no positional args",
			args: []string{"cmd", "--files", "--json"},
			want: []string{"cmd", "--files", "--json"},
		},
		{
			name: "no args at all",
			args: []string{"cmd"},
			want: []string{"cmd"},
		},
		{
			name: "value flag with separate arg",
			args: []string{"cmd", "path/go.mod", "--workers", "30"},
			want: []string{"cmd", "--workers", "30", "path/go.mod"},
		},
		{
			name: "value flag with equals syntax",
			args: []string{"cmd", "path/go.mod", "--workers=30"},
			want: []string{"cmd", "--workers=30", "path/go.mod"},
		},
		{
			name: "value flag with single dash",
			args: []string{"cmd", "path/go.mod", "-workers", "30"},
			want: []string{"cmd", "-workers", "30", "path/go.mod"},
		},
		{
			name: "value flag between boolean flags",
			args: []string{"cmd", "--json", "path/go.mod", "--workers", "25", "--files"},
			want: []string{"cmd", "--json", "--workers", "25", "--files", "path/go.mod"},
		},
		{
			name: "all flags after positional",
			args: []string{"cmd", "path/go.mod", "--json", "--files", "--tree", "--direct-only", "--all", "--time"},
			want: []string{"cmd", "--json", "--files", "--tree", "--direct-only", "--all", "--time", "path/go.mod"},
		},
		{
			name: "go-version value flag with separate arg",
			args: []string{"cmd", "path/go.mod", "--go-version", "1.21.0"},
			want: []string{"cmd", "--go-version", "1.21.0", "path/go.mod"},
		},
		{
			name: "go-version value flag with equals syntax",
			args: []string{"cmd", "path/go.mod", "--go-version=1.21.0"},
			want: []string{"cmd", "--go-version=1.21.0", "path/go.mod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := os.Args
			defer func() { os.Args = saved }()

			os.Args = tt.args
			reorderArgs()

			if len(os.Args) != len(tt.want) {
				t.Fatalf("got %d args %v, want %d args %v", len(os.Args), os.Args, len(tt.want), tt.want)
			}
			for i := range os.Args {
				if os.Args[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q (full: %v)", i, os.Args[i], tt.want[i], os.Args)
					break
				}
			}
		})
	}
}

func TestExtractDurationFlag_NoFlag(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--files", "path/go.mod"}
	durCfg := extractDurationFlag()

	if durCfg.Enabled {
		t.Error("expected Enabled=false when no --duration flag")
	}
	// Args should be unchanged
	if len(os.Args) != 3 {
		t.Errorf("expected 3 args, got %d: %v", len(os.Args), os.Args)
	}
}

func TestExtractDurationFlag_BareFlag(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--duration", "--files", "path/go.mod"}
	durCfg := extractDurationFlag()

	if !durCfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if durCfg.EndDate.IsZero() {
		t.Error("expected EndDate to be set to today")
	}
	// --duration should be removed from args
	for _, arg := range os.Args {
		if arg == "--duration" {
			t.Error("--duration should have been removed from os.Args")
		}
	}
	if len(os.Args) != 3 {
		t.Errorf("expected 3 args after removing --duration, got %d: %v", len(os.Args), os.Args)
	}
}

func TestExtractDurationFlag_WithDate(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--duration=2026-01-15", "--files"}
	durCfg := extractDurationFlag()

	if !durCfg.Enabled {
		t.Error("expected Enabled=true")
	}
	want := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if !durCfg.EndDate.Equal(want) {
		t.Errorf("EndDate = %v, want %v", durCfg.EndDate, want)
	}
	// --duration=DATE should be removed from args
	if len(os.Args) != 2 {
		t.Errorf("expected 2 args after removing --duration=DATE, got %d: %v", len(os.Args), os.Args)
	}
}

func TestExtractDurationFlag_SingleDash(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "-duration"}
	durCfg := extractDurationFlag()

	if !durCfg.Enabled {
		t.Error("expected Enabled=true with single dash")
	}
}

func TestExtractDurationFlag_SingleDashWithDate(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "-duration=2025-06-15"}
	durCfg := extractDurationFlag()

	if !durCfg.Enabled {
		t.Error("expected Enabled=true with single dash and date")
	}
	want := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	if !durCfg.EndDate.Equal(want) {
		t.Errorf("EndDate = %v, want %v", durCfg.EndDate, want)
	}
}

func TestExtractStaleFlag_NoFlag(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--files", "path/go.mod"}
	staleCfg := extractStaleFlag(DurationConfig{})

	if staleCfg.Enabled {
		t.Error("expected Enabled=false when no --stale flag")
	}
	if len(os.Args) != 3 {
		t.Errorf("expected 3 args, got %d: %v", len(os.Args), os.Args)
	}
}

func TestExtractStaleFlag_BareFlag(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--stale", "--files"}
	staleCfg := extractStaleFlag(DurationConfig{})

	if !staleCfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if staleCfg.Years != 2 {
		t.Errorf("expected default Years=2, got %d", staleCfg.Years)
	}
	for _, arg := range os.Args {
		if arg == "--stale" {
			t.Error("--stale should have been removed from os.Args")
		}
	}
}

func TestExtractStaleFlag_WithThreshold(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()

	os.Args = []string{"cmd", "--stale=1y6m"}
	staleCfg := extractStaleFlag(DurationConfig{})

	if !staleCfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if staleCfg.Years != 1 || staleCfg.Months != 6 || staleCfg.Days != 0 {
		t.Errorf("threshold = (%d, %d, %d), want (1, 6, 0)", staleCfg.Years, staleCfg.Months, staleCfg.Days)
	}
}
