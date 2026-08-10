package main

// JSONC support for bun.lock, which uses trailing commas and permits comments.
// encoding/json rejects both. StripJSONC blanks them out in place so byte
// offsets — and therefore line numbers — remain valid against the original file.

// StripJSONC returns a copy of src with comments and trailing commas replaced
// by spaces. The result is always the same length as src and has newlines in
// the same positions, so an offset into the result addresses the same line in
// the original file.
func StripJSONC(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)

	// Pass 1: blank out comments, skipping string literals.
	inString := false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			if c == '\\' {
				i++ // skip the escaped byte
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			// Consume both opener bytes before scanning for the closer.
			// Starting the scan on the opening '/' would let the opener's
			// own '*' pair with a following '/' and close the comment three
			// bytes early, e.g. in "/*/ ... */".
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i < len(out) {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					break
				}
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
		}
	}

	// Pass 2: blank out commas that are followed only by whitespace before a
	// closing brace or bracket.
	inString = false
	lastComma := -1
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			lastComma = -1
		case ',':
			lastComma = i
		case '}', ']':
			if lastComma >= 0 {
				out[lastComma] = ' '
			}
			lastComma = -1
		case ' ', '\t', '\n', '\r':
			// whitespace does not clear the pending comma
		default:
			lastComma = -1
		}
	}
	return out
}

// lineIndex maps byte offsets to 1-based line numbers.
type lineIndex struct {
	starts []int // byte offset of the first character of each line
}

// newLineIndex builds a line index for src.
func newLineIndex(src []byte) *lineIndex {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{starts: starts}
}

// Line returns the 1-based line containing the byte at off. Offsets past the
// end of the input report the last line.
func (li *lineIndex) Line(off int) int {
	if off < 0 {
		return 1
	}
	lo, hi := 0, len(li.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if li.starts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo + 1
}
