package tcl

import "strings"

// Where a command substitution ends. Tcl reads a [script]'s interior as a script:
// a `]` closes it only where a word or command could go on, so one inside braces
// or quotes that open a word (after a `{*}` too), in a comment, or in a `$name(`
// index does not. The depth count in skipBracketSpan ignores all of that. The
// scanner and the command-word substitution walk use scriptClose; only where it
// fails does the scanner fall back to a trusted depth-count span, and the walk
// then stays silent about what the script binds.

// scriptClose returns the offset of the `]` that closes the command substitution
// whose interior starts at i, read as Tcl 8.6 and 9.0 read it. ok is false when
// none closes it (Tcl reports "missing close-bracket") or the two versions may
// disagree: a `${name}` holding `{` or `\`, which 9.0 nests and 8.6 does not.
// An index holding `"` is read as 8.6 reads it; 9.0 rejects it.
func scriptClose(s string, i int) (end int, ok bool) {
	var m *scriptEnds
	return m.scriptClose(s, i)
}

// scriptClose is scriptClose, remembering each end it finds in m (scriptends.go).
func (m *scriptEnds) scriptClose(s string, i int) (end int, ok bool) {
	if end, found, hit := m.lookup(s, i); hit {
		return end, found
	}
	end, ok = m.scanScriptClose(s, i)
	m.store(s, i, end, ok)
	return end, ok
}

func (m *scriptEnds) scanScriptClose(s string, i int) (end int, ok bool) {
	atWord, atCmd := true, true
	for i < len(s) {
		c := s[i]
		wasWord, wasCmd := atWord, atCmd
		atWord, atCmd = false, false
		switch {
		case c == '\\':
			if i+1 < len(s) && s[i+1] == '\n' {
				atWord, atCmd = true, wasCmd
			}
			i += 2
			continue
		case c == ';' || c == '\n':
			atWord, atCmd = true, true
		case isListSpace(c):
			atWord, atCmd = true, wasCmd
		case c == '#' && wasCmd:
			i = commentEnd(s, i)
			atWord, atCmd = true, true
			continue
		case c == ']':
			return i, true
		case c == '[':
			j, ok := m.scriptClose(s, i+1)
			if !ok {
				return 0, false
			}
			i = j + 1
			continue
		case c == '{' && wasWord && isExpandPrefix(s, i):
			i += 3 // {*}: the next byte starts the word
			atWord = true
			continue
		case c == '{' && wasWord:
			j, ok := braceClose(s, i)
			if !ok {
				return 0, false
			}
			i = j + 1
			continue
		case c == '"' && wasWord:
			j, ok := m.quoteClose(s, i+1)
			if !ok {
				return 0, false
			}
			i = j + 1
			continue
		case c == '$':
			j, ok := m.varEnd(s, i)
			if !ok {
				return 0, false
			}
			i = j
			continue
		}
		i++
	}
	return 0, false
}

// isExpandPrefix reports whether the `{` at i, at a word start, opens a `{*}`
// expansion prefix: Tcl reads `{*}` so only when a byte that can start a word
// follows. Before a space, separator, `]` or the end, it is the literal word `*`.
func isExpandPrefix(s string, i int) bool {
	if !strings.HasPrefix(s[i:], "{*}") || i+3 >= len(s) {
		return false
	}
	switch c := s[i+3]; {
	case isListSpace(c), c == ';', c == ']':
		return false
	case c == '\\':
		return i+4 >= len(s) || s[i+4] != '\n'
	}
	return true
}

// commentEnd returns the offset of the newline that ends the comment at i, or
// len(s). A backslash-newline continues it.
func commentEnd(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		if s[i] == '\\' {
			i++
		}
		i++
	}
	return min(i, len(s))
}

// braceClose returns the offset of the `}` that closes the brace group at i.
// A backslash quotes the next byte, so `\{` and `\}` do not count.
func braceClose(s string, i int) (int, bool) {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// quoteClose returns the offset of the `"` that closes a quoted word whose text
// starts at i. Its [scripts] and `$name(` indexes may hold a `"` of their own.
func (m *scriptEnds) quoteClose(s string, i int) (int, bool) {
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
		case '"':
			return i, true
		case '[':
			j, ok := m.scriptClose(s, i+1)
			if !ok {
				return 0, false
			}
			i = j + 1
		case '$':
			j, ok := m.varEnd(s, i)
			if !ok {
				return 0, false
			}
			i = j
		default:
			i++
		}
	}
	return 0, false
}

// varEnd returns the offset just past the variable substitution at the `$` at i,
// or i+1 when the `$` is literal. A `$name(` index runs to its `)`: only `\`, `$`
// and `[` mean anything in it, so it hides a `]` or `"`.
func (m *scriptEnds) varEnd(s string, i int) (int, bool) {
	j := i + 1
	if j < len(s) && s[j] == '{' {
		for k := j + 1; k < len(s); k++ {
			switch s[k] {
			case '}':
				return k + 1, true
			case '{', '\\':
				return 0, false
			}
		}
		return 0, false
	}
	j = nameEnd(s, j)
	if j >= len(s) || s[j] != '(' {
		return max(j, i+1), true
	}
	for j++; j < len(s); {
		switch s[j] {
		case '\\':
			j += 2
		case ')':
			return j + 1, true
		case '[':
			k, ok := m.scriptClose(s, j+1)
			if !ok {
				return 0, false
			}
			j = k + 1
		case '$':
			k, ok := m.varEnd(s, j)
			if !ok {
				return 0, false
			}
			j = k
		default:
			j++
		}
	}
	return 0, false
}

// nameEnd returns the offset just past the variable name that starts at i:
// letters, digits, `_` and runs of two or more `:`. A lone `:` ends it, so
// `$a:(` is `$a` and the text `:(`.
func nameEnd(s string, i int) int {
	for i < len(s) {
		if isNameByte(s[i]) {
			i++
		} else if s[i] == ':' && i+1 < len(s) && s[i+1] == ':' {
			for i < len(s) && s[i] == ':' {
				i++
			}
		} else {
			break
		}
	}
	return i
}
