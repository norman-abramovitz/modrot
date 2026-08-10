package main

import "testing"

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"line comment", "{\n  // hi\n  \"a\": 1\n}", "{\n       \n  \"a\": 1\n}"},
		{"trailing comma object", `{"a": 1,}`, `{"a": 1 }`},
		{"trailing comma array", `[1, 2,]`, `[1, 2 ]`},
		{"comma inside string kept", `{"a": "x,"}`, `{"a": "x,"}`},
		{"slashes inside string kept", `{"a": "http://x"}`, `{"a": "http://x"}`},
		{"block comment", `{/* no */"a": 1}`, `{        "a": 1}`},
		{"escaped quote in string", `{"a": "x\"//y"}`, `{"a": "x\"//y"}`},
		{"no change", `{"a": 1}`, `{"a": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(StripJSONC([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("StripJSONC(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// The invariant that makes offset-based line anchoring work.
func TestStripJSONCPreservesLength(t *testing.T) {
	in := []byte("{\n  // c\n  /* b\n     b */\n  \"a\": [1,],\n}")
	out := StripJSONC(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: got %d, want %d", len(out), len(in))
	}
	inNL, outNL := 0, 0
	for i := range in {
		if in[i] == '\n' {
			inNL++
		}
		if out[i] == '\n' {
			outNL++
		}
	}
	if inNL != outNL {
		t.Errorf("newline count changed: got %d, want %d", outNL, inNL)
	}
}

func TestLineIndex(t *testing.T) {
	src := []byte("aa\nbbb\n\ncc")
	li := newLineIndex(src)
	tests := []struct {
		off  int
		want int
	}{
		{0, 1}, {1, 1}, {2, 1}, // "aa\n"
		{3, 2}, {5, 2}, {6, 2}, // "bbb\n"
		{7, 3},         // ""
		{8, 4}, {9, 4}, // "cc"
	}
	for _, tt := range tests {
		if got := li.Line(tt.off); got != tt.want {
			t.Errorf("Line(%d) = %d, want %d", tt.off, got, tt.want)
		}
	}
}
