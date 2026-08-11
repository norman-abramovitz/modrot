package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testGoMod = `module example.com/app

go 1.21

require github.com/foo/bar v1.2.3
`

func TestDiscoverUnits(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"go only", []string{"go.mod"}, []string{"go"}},
		{"npm only", []string{"package.json"}, []string{"npm"}},
		{"both", []string{"go.mod", "package.json"}, []string{"go", "npm"}},
		{"neither", []string{"README.md"}, nil},
		{"lockfile without package.json", []string{"package-lock.json"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var got []string
			for _, eco := range discoverUnits(dir) {
				got = append(got, eco.Name)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFindUnitDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, name string) {
		d := filepath.Join(root, rel)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk(".", "go.mod")
	mk("web", "package.json")
	mk("node_modules/dep", "package.json") // must be skipped
	mk("vendor/x", "go.mod")               // must be skipped
	mk(".hidden", "package.json")          // must be skipped
	mk("testdata/x", "go.mod")             // must be skipped

	dirs, err := findUnitDirs(root)
	if err != nil {
		t.Fatalf("findUnitDirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2: %v", len(dirs), dirs)
	}
}

func TestParseGoUnit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := parseGoUnit(dir)
	if err != nil {
		t.Fatalf("parseGoUnit: %v", err)
	}
	if res.Name != "example.com/app" {
		t.Errorf("Name = %q, want example.com/app", res.Name)
	}
	if res.Primary != "go.mod" {
		t.Errorf("Primary = %q, want go.mod", res.Primary)
	}
	if res.Unlocked {
		t.Error("Unlocked = true, want false for Go")
	}
	if len(res.Modules) != 1 || res.Modules[0].Path != "github.com/foo/bar" {
		t.Errorf("Modules = %+v", res.Modules)
	}
}

// --tree support is expressed as data: npm has no graph source.
func TestNPMEcosystemHasNoGraph(t *testing.T) {
	if npmEcosystem.Graph != nil {
		t.Error("npmEcosystem.Graph should be nil")
	}
	if goEcosystem.Graph == nil {
		t.Error("goEcosystem.Graph should be set")
	}
}
