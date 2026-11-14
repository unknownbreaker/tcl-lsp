package tcl

import "strings"

// VarRef is a $-substitution occurrence within a word, with an absolute byte
// range [Start,End) covering the reference token (e.g. "$x").
type VarRef struct {
	Name  string
	Start int
	End   int
	// Element is true when the reference names an array element. For $a(key)
	// Name and the range cover the base only; for ${a(key)} Name keeps the
	// subscript and the range covers the whole braced token.
	Element bool
}

// WordVarRefs returns the variable references substituted inside a single word.
// Braced words undergo no substitution and yield none. Bare and quoted words are
// scanned for $name forms. Interiors of [command substitution] spans are NOT
// descended here (handled later with the command walker).
func WordVarRefs(w Word) []VarRef {
	if w.Kind == WordBraced {
		return nil
	}
	return scanVarRefs(w.Text, w.Start)
}

func scanVarRefs(text string, base int) []VarRef {
	var refs []VarRef
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			i += 2 // escaped char is literal
		case c == '[':
			i = skipBracketSpan(text, i) // command-substitution interior deferred
		case c == '$':
			ref, next, ok := parseVarRef(text, i, base)
			if ok {
				refs = append(refs, ref)
			}
			i = next
		default:
			i++
		}
	}
	return refs
}

// skipBracketSpan returns the index just past a balanced [..] span starting at i.
// Backslash-aware; tolerant of unterminated input (returns len(text)).
func skipBracketSpan(text string, i int) int {
	depth := 0
	for i < len(text) {
		c := text[i]
		if c == '\\' && i+1 < len(text) {
			i += 2
			continue
		}
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return i
}

func parseVarRef(text string, dollar, base int) (VarRef, int, bool) {
	i := dollar + 1
	if i >= len(text) {
		return VarRef{}, dollar + 1, false
	}
	if text[i] == '{' {
		j := i + 1
		for j < len(text) && text[j] != '}' {
			j++
		}
		if j >= len(text) {
			return VarRef{}, len(text), false // unterminated ${ : tolerant
		}
		if j == i+1 {
			return VarRef{}, j + 1, false // empty ${} is not a valid reference
		}
		name := text[i+1 : j]
		return VarRef{Name: name, Start: base + dollar, End: base + j + 1, Element: isElementName(name)}, j + 1, true
	}
	// Bareword name: optional leading "::", then ::-joined [A-Za-z0-9_] segments.
	nameStart := i
	j := i
	if j+1 < len(text) && text[j] == ':' && text[j+1] == ':' {
		j += 2
	}
	if j >= len(text) || !isNameByte(text[j]) {
		return VarRef{}, dollar + 1, false
	}
	for j < len(text) && isNameByte(text[j]) {
		j++
	}
	for j+1 < len(text) && text[j] == ':' && text[j+1] == ':' {
		seg := j + 2
		for seg < len(text) && isNameByte(text[seg]) {
			seg++
		}
		trailing := seg == j+2
		j = seg
		if trailing {
			break // a trailing "::" still belongs to the name: $a:: reads `a::`
		}
	}
	element := j < len(text) && text[j] == '('
	return VarRef{Name: text[nameStart:j], Start: base + dollar, End: base + j, Element: element}, j, true
}

// isElementName reports whether Tcl reads a variable name as an array element:
// a non-empty base, a `(`, and a final `)`. `a(k)b` and `a(` are scalars.
func isElementName(name string) bool {
	return strings.IndexByte(name, '(') > 0 && strings.HasSuffix(name, ")")
}

func isNameByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
