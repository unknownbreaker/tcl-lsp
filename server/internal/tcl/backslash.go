package tcl

import (
	"strings"
	"unicode/utf8"
)

// decodeBackslashes applies Tcl's backslash substitution to s, the text of a bare
// word or list element with no `$` or `[`, and returns the value Tcl sees. at[i] is
// the offset in s where the escape or byte that produced value byte i starts. The
// rules follow tclsh 8.6 and 9.0, which agree here: \x reads at most 2 hex digits,
// \u at most 4, \U at most 8; an octal escape reads a third digit only while its value is
// below 040; a letter with no hex digit after it, and any other escaped character,
// stands for itself. ok is false where the value is not certain here: a
// backslash-newline (it separates words), a trailing backslash, a surrogate, and a
// \U value past U+FFFF, which 8.6 writes as U+FFFD and 9.0 as the character itself.
func decodeBackslashes(s string) (value string, at []int, ok bool) {
	var b strings.Builder
	emit := func(str string, from int) {
		b.WriteString(str)
		for range len(str) {
			at = append(at, from)
		}
	}
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			emit(s[i:i+1], i)
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", nil, false
		}
		start, c := i, s[i+1]
		i += 2
		switch c {
		case 'a':
			emit("\a", start)
		case 'b':
			emit("\b", start)
		case 'f':
			emit("\f", start)
		case 'n':
			emit("\n", start)
		case 'r':
			emit("\r", start)
		case 't':
			emit("\t", start)
		case 'v':
			emit("\v", start)
		case '\n', '\r':
			return "", nil, false
		case 'x', 'u', 'U':
			limit := 8
			switch c {
			case 'x':
				limit = 2
			case 'u':
				limit = 4
			}
			v, n := 0, 0
			for n < limit && i < len(s) && hexVal(s[i]) >= 0 {
				v = v<<4 | hexVal(s[i])
				i++
				n++
			}
			switch {
			case n == 0:
				emit(string(c), start)
			case c == 'x':
				emit(string(rune(v)), start)
			case v > 0xFFFF || (v >= 0xD800 && v <= 0xDFFF):
				return "", nil, false
			default:
				emit(string(rune(v)), start)
			}
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := int(c - '0')
			if i < len(s) && isOctal(s[i]) {
				v = v<<3 | int(s[i]-'0')
				i++
				if v < 040 && i < len(s) && isOctal(s[i]) {
					v = v<<3 | int(s[i]-'0')
					i++
				}
			}
			emit(string(rune(v)), start)
		default:
			// Any other character, a multibyte one included, stands for itself.
			_, size := utf8.DecodeRuneInString(s[i-1:])
			emit(s[i-1:i-1+size], start)
			i += size - 1
		}
	}
	return b.String(), at, true
}

// DecodeBackslashes returns the value Tcl gives s, the text of a bare word or a
// quoted word's inside with no `$` or `[`, and false where that value is not
// certain (decodeBackslashes).
func DecodeBackslashes(s string) (string, bool) {
	v, _, ok := decodeBackslashes(s)
	return v, ok
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// plainVarName returns the variable name a bare, unsubstituted word names, with its
// backslash escapes decoded: `a\x62` names `ab`.
func plainVarName(w Word) (string, bool) {
	if !isPlainName(w) {
		return "", false
	}
	if strings.IndexByte(w.Text, '\\') < 0 {
		return w.Text, true
	}
	name, _, ok := decodeBackslashes(w.Text)
	return name, ok && name != ""
}

// escapedArrayBaseName is arrayBaseName for a bare word with a backslash in it. The
// base's range ends where the escape or byte that produced its `(` starts.
func escapedArrayBaseName(w Word) (name string, start, end int, ok bool) {
	if !isPlainName(w) {
		return "", 0, 0, false
	}
	value, at, ok := decodeBackslashes(w.Text)
	if !ok || value == "" {
		return "", 0, 0, false
	}
	p := strings.IndexByte(value, '(')
	if p < 0 {
		return value, w.Start, w.End, true
	}
	if !isElementName(value) {
		return "", 0, 0, false
	}
	return value[:p], w.Start, w.Start + at[p], true
}

// emitEscapedListName emits the binding of a bare var-list word with a backslash,
// if it has one, and reports whether the word was such a word.
func emitEscapedListName(w Word, base int, ns string, scope int, class string, out *[]Definition) bool {
	p, ok, handled := escapedListName(w, base)
	if ok {
		*out = append(*out, localDef(p, ns, scope, class))
	}
	return handled
}

// escapedListName handles a bare variable-list word with a backslash in it, such as
// a foreach var list or proc args. Tcl decodes it twice, once as a command word and
// once as a list (`a\\x62` binds `ab`). It binds only when the first decoding leaves
// a single list element: no whitespace, brace or quote. handled is true for every
// such word, and ok says whether p is a binding.
func escapedListName(w Word, base int) (p paramName, ok, handled bool) {
	if w.Kind == WordBraced || strings.IndexByte(w.Text, '\\') < 0 {
		return paramName{}, false, false
	}
	elem, ok := plainVarName(w)
	if !ok || strings.ContainsAny(elem, " \t\n\r\v\f{}\"") {
		return paramName{}, false, true
	}
	name, _, ok := decodeBackslashes(elem)
	if !ok || name == "" {
		return paramName{}, false, true
	}
	return paramName{Name: name, Start: base + w.Start, End: base + w.End}, true, true
}

// variableDeclName returns the name a `variable NAME ?value?` command declares.
func variableDeclName(w []Word) (string, bool) {
	if !isCmd(w, "variable") || len(w) < 2 {
		return "", false
	}
	return plainVarName(w[1])
}

// decodedEqual reports whether two variable words name one variable once escapes
// are decoded, as `a\x62` and `ab` do.
func decodedEqual(a, b Word) bool {
	x, xok := plainVarName(a)
	y, yok := plainVarName(b)
	return xok && yok && x == y
}
