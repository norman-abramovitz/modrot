package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary builds the modrot binary for integration tests.
// Returns the path to the built binary.
func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "modrot")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build binary: %v", err)
	}
	return binary
}

// runModrot runs the modrot binary with the given args and returns stdout, stderr, and exit code.
func runModrot(t *testing.T, binary string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestIntegration_Help(t *testing.T) {
	binary := buildBinary(t)

	// "modrot help" should show usage
	_, stderr, code := runModrot(t, binary, "help")
	if code != 0 {
		t.Errorf("modrot help: exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "Usage: modrot") {
		t.Error("modrot help: expected usage text in stderr")
	}
}

func TestIntegration_HelpFlag(t *testing.T) {
	binary := buildBinary(t)

	// "modrot --help" should show usage (Go flag package writes to stderr and exits 0... actually exits 2)
	_, stderr, _ := runModrot(t, binary, "--help")
	if !strings.Contains(stderr, "Usage: modrot") {
		t.Error("modrot --help: expected usage text")
	}
}

func TestIntegration_Version(t *testing.T) {
	binary := buildBinary(t)

	stdout, _, code := runModrot(t, binary, "--version")
	if code != 0 {
		t.Errorf("modrot --version: exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "modrot") {
		t.Errorf("modrot --version: expected 'modrot' in output, got %q", stdout)
	}
}

func TestIntegration_InvalidPath(t *testing.T) {
	binary := buildBinary(t)

	_, stderr, code := runModrot(t, binary, "/nonexistent/path/go.mod")
	if code != 2 {
		t.Errorf("invalid path: exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Error") {
		t.Error("invalid path: expected error message in stderr")
	}
}

func TestIntegration_NoGitHubDeps(t *testing.T) {
	binary := buildBinary(t)

	fixture := filepath.Join("testdata", "fixtures", "no-github-deps", "go.mod")
	_, stderr, code := runModrot(t, binary, fixture)
	if code != 0 {
		t.Errorf("no-github-deps: exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "No GitHub modules found") {
		t.Errorf("no-github-deps: expected 'No GitHub modules found', got stderr: %q", stderr)
	}
}

func TestIntegration_JSONOutput(t *testing.T) {
	binary := buildBinary(t)

	fixture := filepath.Join("testdata", "fixtures", "no-github-deps", "go.mod")
	stdout, _, code := runModrot(t, binary, "--json", fixture)
	if code != 0 {
		t.Errorf("json output: exit code = %d, want 0", code)
	}
	// JSON output should be valid (starts with { or is empty object)
	trimmed := strings.TrimSpace(stdout)
	if trimmed != "" && !strings.HasPrefix(trimmed, "{") {
		t.Errorf("json output: expected JSON object, got %q", trimmed[:50])
	}
}

func TestIntegration_MarkdownOutput(t *testing.T) {
	binary := buildBinary(t)

	fixture := filepath.Join("testdata", "fixtures", "no-github-deps", "go.mod")
	stdout, _, code := runModrot(t, binary, "--markdown", fixture)
	if code != 0 {
		t.Errorf("markdown output: exit code = %d, want 0", code)
	}
	// Markdown output may be empty for no-github-deps, that's fine
	_ = stdout
}

func TestMixedRepoDiscovery(t *testing.T) {
	root := filepath.Join("testdata", "fixtures", "mixed-repo")

	dirs, err := findUnitDirs(root)
	if err != nil {
		t.Fatalf("findUnitDirs: %v", err)
	}
	units, _ := buildManifestInfos(dirs, root)
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}

	names := map[string]bool{}
	for _, u := range units {
		names[u.eco.Name] = true
		if len(u.allModules) == 0 {
			t.Errorf("%s unit parsed no modules", u.eco.Name)
		}
		for _, m := range u.allModules {
			if m.Ecosystem != u.eco.Name {
				t.Errorf("%s: Ecosystem = %q, want %q", m.Path, m.Ecosystem, u.eco.Name)
			}
			if m.LineFile == "" {
				t.Errorf("%s: LineFile is empty", m.Path)
			}
		}
	}
	if !names["go"] || !names["npm"] {
		t.Errorf("discovered %v, want both go and npm", names)
	}
}
