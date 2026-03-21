package main

import (
	"testing"
	"time"
)

func TestParseColorThreshold(t *testing.T) {
	tests := []struct {
		input   string
		wantN   int
		wantErr bool
	}{
		{"1y,3y", 2, false},
		{"6m,1y,3y", 3, false},
		{"3m,1y,2y,5y", 4, false},
		{"180d,1y6m,3y,5y", 4, false},
		{"1y", 0, true},             // too few
		{"1y,2y,3y,4y,5y", 0, true}, // too many
		{"bad,3y", 0, true},         // invalid value
		{"1y,bad", 0, true},         // invalid value
		{"", 0, true},               // empty
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			thresholds, err := parseColorThreshold(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseColorThreshold(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && len(thresholds) != tt.wantN {
				t.Errorf("got %d thresholds, want %d", len(thresholds), tt.wantN)
			}
		})
	}
}

func TestClassifyAge_FourThresholds(t *testing.T) {
	cfg := &Config{
		Color: ColorConfig{
			Enabled: true,
			Thresholds: []ColorThreshold{
				{0, 3, 0}, // 3m
				{1, 0, 0}, // 1y
				{2, 0, 0}, // 2y
				{5, 0, 0}, // 5y
			},
		},
	}

	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want int
	}{
		{"zero time", time.Time{}, -1},
		{"1 month ago (newest)", now.AddDate(0, -1, 0), 0},
		{"6 months ago (recent)", now.AddDate(0, -6, 0), 1},
		{"18 months ago (moderate)", now.AddDate(-1, -6, 0), 2},
		{"3 years ago (old)", now.AddDate(-3, 0, 0), 3},
		{"6 years ago (critical)", now.AddDate(-6, 0, 0), 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAge(cfg, tt.t)
			if got != tt.want {
				t.Errorf("classifyAge() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClassifyAge_TwoThresholds(t *testing.T) {
	cfg := &Config{
		Color: ColorConfig{
			Enabled: true,
			Thresholds: []ColorThreshold{
				{1, 0, 0}, // 1y
				{3, 0, 0}, // 3y
			},
		},
	}

	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want int
	}{
		{"6 months ago (newest)", now.AddDate(0, -6, 0), 0},
		{"2 years ago (middle)", now.AddDate(-2, 0, 0), 1},
		{"4 years ago (oldest)", now.AddDate(-4, 0, 0), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAge(cfg, tt.t)
			if got != tt.want {
				t.Errorf("classifyAge() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClassifyAge_ThreeThresholds(t *testing.T) {
	cfg := &Config{
		Color: ColorConfig{
			Enabled: true,
			Thresholds: []ColorThreshold{
				{0, 6, 0}, // 6m
				{1, 0, 0}, // 1y
				{3, 0, 0}, // 3y
			},
		},
	}

	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want int
	}{
		{"3 months ago (newest)", now.AddDate(0, -3, 0), 0},
		{"9 months ago", now.AddDate(0, -9, 0), 1},
		{"2 years ago", now.AddDate(-2, 0, 0), 2},
		{"4 years ago (oldest)", now.AddDate(-4, 0, 0), 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAge(cfg, tt.t)
			if got != tt.want {
				t.Errorf("classifyAge() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSelectStyle(t *testing.T) {
	// 5 levels (4 thresholds): should map 0→0, 1→1, 2→2, 3→3, 4→4
	for i := 0; i < 5; i++ {
		color, symbol := selectStyle(i, 5)
		if color != levelStyles[i].color || symbol != levelStyles[i].symbol {
			t.Errorf("selectStyle(%d, 5) = (%q, %q), want (%q, %q)",
				i, color, symbol, levelStyles[i].color, levelStyles[i].symbol)
		}
	}

	// 3 levels (2 thresholds): should map 0→0, 1→2, 2→4
	tests3 := []struct {
		level   int
		wantIdx int
	}{
		{0, 0},
		{1, 2},
		{2, 4},
	}
	for _, tt := range tests3 {
		color, symbol := selectStyle(tt.level, 3)
		if color != levelStyles[tt.wantIdx].color || symbol != levelStyles[tt.wantIdx].symbol {
			t.Errorf("selectStyle(%d, 3) mapped to wrong style", tt.level)
		}
	}

	// 4 levels (3 thresholds): should map 0→0, 1→1, 2→2, 3→4
	tests4 := []struct {
		level   int
		wantIdx int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 4},
	}
	for _, tt := range tests4 {
		color, symbol := selectStyle(tt.level, 4)
		if color != levelStyles[tt.wantIdx].color || symbol != levelStyles[tt.wantIdx].symbol {
			t.Errorf("selectStyle(%d, 4) mapped to wrong style, want idx %d", tt.level, tt.wantIdx)
		}
	}
}

func TestColorize_Disabled(t *testing.T) {
	cfg := &Config{Color: ColorConfig{Enabled: false}}
	input := "2020-01-01"
	got := colorize(cfg, input, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if got != input {
		t.Errorf("colorize with disabled = %q, want %q", got, input)
	}
}

func TestColorize_Enabled(t *testing.T) {
	cfg := &Config{
		Color: ColorConfig{
			Enabled: true,
			Thresholds: []ColorThreshold{
				{0, 3, 0},
				{1, 0, 0},
				{2, 0, 0},
				{5, 0, 0},
			},
		},
	}

	now := time.Now()

	// Recent — should be decorated
	recent := now.AddDate(0, -1, 0)
	got := colorize(cfg, "2025-01-01", recent)
	if got == "2025-01-01" {
		t.Errorf("newest level should be decorated")
	}

	// Critical — should be decorated differently
	old := now.AddDate(-6, 0, 0)
	got = colorize(cfg, "2020-01-01", old)
	if got == "2020-01-01" {
		t.Errorf("critical level should be decorated")
	}

	// Zero time — no decoration
	got = colorize(cfg, "", time.Time{})
	if got != "" {
		t.Errorf("zero time should not be decorated, got %q", got)
	}
}

func TestInitColor_NoColor(t *testing.T) {
	cfg := NewDefaultConfig()
	err := initColor(cfg, true, "")
	if err != nil {
		t.Fatalf("initColor error: %v", err)
	}
	if cfg.Color.Enabled {
		t.Error("expected color disabled with noColor=true")
	}
}

func TestInitColor_InvalidThreshold(t *testing.T) {
	_, err := parseColorThreshold("bad")
	if err == nil {
		t.Error("expected error for invalid threshold")
	}
}
