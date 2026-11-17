package tcl

// isListSpace reports whether c separates Tcl list elements. Only ASCII
// whitespace does, in tclsh 8.6 and 9.0: a U+00A0 inside an element is literal.
func isListSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

// skipEscape returns the offset just past the backslash escape at s[i]. Only an
// escape's whitespace matters for splitting: a backslash-newline also takes the
// spaces and tabs after it, and any other escape is skipped as the backslash and
// one byte (a multi-byte character's other bytes are never list syntax).
func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	if s[i+1] != '\n' {
		return i + 2
	}
	i += 2
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// listElements splits s, a list's string value, into its elements as Tcl's list
// parser does. Each element is a Word with its raw text (braces and quotes kept)
// and offsets into s, and its Kind names its delimiter. In a list, `;`, `#`, `$` and
// `[` are ordinary characters. ok is false where Tcl rejects the list: an unmatched
// brace or quote, or a closing brace or quote not followed by whitespace.
func listElements(s string) (elems []Word, ok bool) {
	i := 0
	for {
		for i < len(s) && isListSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return elems, true
		}
		start := i
		kind := WordBare
		switch s[i] {
		case '{':
			kind = WordBraced
			depth := 1
			for i++; depth > 0; {
				if i >= len(s) {
					return nil, false
				}
				switch s[i] {
				case '\\':
					i = skipEscape(s, i)
					continue
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
		case '"':
			kind = WordQuoted
			for i++; ; {
				if i >= len(s) {
					return nil, false
				}
				if s[i] == '\\' {
					i = skipEscape(s, i)
					continue
				}
				i++
				if s[i-1] == '"' {
					break
				}
			}
		default:
			for i < len(s) && !isListSpace(s[i]) {
				if s[i] == '\\' {
					i = skipEscape(s, i)
					continue
				}
				i++
			}
		}
		if kind != WordBare && i < len(s) && !isListSpace(s[i]) {
			return nil, false
		}
		elems = append(elems, Word{Kind: kind, Text: s[start:i], Start: start, End: i})
	}
}

// bracedValue returns the value of a braced word's interior: Tcl replaces each
// backslash-newline and the spaces and tabs after it with one space, and keeps
// every other byte. The result here keeps the interior's length, padding with
// spaces, so its offsets still index the source.
func bracedValue(inner string) string {
	b := []byte(inner)
	for i := 0; i < len(b); {
		if b[i] != '\\' {
			i++
			continue
		}
		end := skipEscape(inner, i)
		if i+1 < len(b) && b[i+1] == '\n' {
			for j := i; j < end; j++ {
				b[j] = ' '
			}
		}
		i = end
	}
	return string(b)
}
