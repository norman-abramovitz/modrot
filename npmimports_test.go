package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildNPMImportPattern(t *testing.T) {
	got := buildNPMImportPattern([]string{"xterm", "@babel/core"})
	want := `(?:from|require\s*\(|import\s*\(|import)\s*['"](?:xterm|@babel/core)(?:/[^'"]*)?['"]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestScanNPMImports(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()

	files := map[string]string{
		"src/terminal.ts": "" +
			"import { Terminal } from 'xterm';\n" +
			"import 'xterm/css/xterm.css';\n" +
			"const x = require(\"xterm\");\n" +
			"const y = await import('xterm');\n",
		"src/other.js": "" +
			"import foo from 'xterm-addon-fit';\n" + // must NOT match "xterm"
			"import bar from '@babel/core';\n",
		// Forms taken from real TypeScript sources: type-only import and
		// re-export both reach the specifier via `from`; dynamic import does
		// not, which is why the pattern needs its own `import(` alternative.
		"src/forms.ts": "" +
			"import type { Terminal } from '@babel/core';\n" +
			"export { thing } from '@babel/core';\n",
		"node_modules/pkg/index.js": "import 'xterm';\n", // excluded
		"dist/bundle.js":            "import 'xterm';\n", // excluded
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ScanNPMImports(dir, []string{"xterm", "@babel/core"})
	if err != nil {
		t.Fatalf("ScanNPMImports: %v", err)
	}

	xterm := got["xterm"]
	if len(xterm) != 4 {
		t.Fatalf("xterm: got %d matches, want 4: %+v", len(xterm), xterm)
	}
	for _, m := range xterm {
		if m.File != "src/terminal.ts" {
			t.Errorf("xterm matched in %s, want only src/terminal.ts", m.File)
		}
	}
	if lines := []int{xterm[0].Line, xterm[1].Line, xterm[2].Line, xterm[3].Line}; lines[0] != 1 || lines[3] != 4 {
		t.Errorf("xterm lines = %v, want 1..4", lines)
	}

	// One plain import in other.js, plus a type-only import and a re-export
	// in forms.ts. All three name the package and must all be reported.
	babel := got["@babel/core"]
	if len(babel) != 3 {
		t.Fatalf("@babel/core: got %d matches, want 3: %+v", len(babel), babel)
	}
	byFile := map[string]int{}
	for _, m := range babel {
		byFile[m.File]++
	}
	if byFile["src/other.js"] != 1 || byFile["src/forms.ts"] != 2 {
		t.Errorf("@babel/core per file = %v, want other.js:1 forms.ts:2", byFile)
	}
	if _, ok := got["xterm-addon-fit"]; ok {
		t.Error("xterm-addon-fit should not be reported; it was not requested")
	}
}

func TestScanNPMImportsNoPackages(t *testing.T) {
	got, err := ScanNPMImports(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ScanNPMImports: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for an empty package list", got)
	}
}
