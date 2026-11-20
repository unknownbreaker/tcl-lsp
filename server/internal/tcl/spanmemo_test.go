package tcl

import (
	"math/rand"
	"strings"
	"testing"
)

// The span memo (scriptends.go) must answer exactly as the plain scans did. The
// ref* functions are those scans as they were before #95.

func refBracesHideBrackets(script string) bool {
	braces, brackets := 0, 0 // brackets counts only those inside braces or quotes
	quoted := false
	var indexes []int // per open array index: the brackets opened inside it
	wordStart, cmdStart := true, true
	for i := 0; i < len(script); i++ {
		c := script[i]
		atWord, atCmd := wordStart, cmdStart
		wordStart, cmdStart = false, false
		bare := braces == 0 && !quoted
		// In an index at its own level (bare or quoted, never in braces), only `\`,
		// `$`, `[`, `]` and `)` mean anything.
		inIndex := braces == 0 && len(indexes) > 0 && indexes[len(indexes)-1] == 0
		switch {
		case c == '\\':
			if i+1 < len(script) && script[i+1] == '\n' && bare {
				wordStart, cmdStart = true, atCmd
			}
			i++
		case inIndex && c != '$' && c != '[' && c != ']' && c != ')':
		case bare && (c == ';' || c == '\n'):
			wordStart, cmdStart = true, true
		case bare && isListSpace(c):
			wordStart, cmdStart = true, atCmd
		case bare && c == '$' && i+1 < len(script) && script[i+1] == '{':
			// `${name}` takes no index, and a bracket in the name is literal, so it
			// hides one from the span. Tcl 8.6 ends the name at the first `}`, but
			// 9.0 nests braces and honors `\}` in it, so a `{` or `\` is not trusted.
			end := strings.IndexByte(script[i+2:], '}')
			if end < 0 || strings.ContainsAny(script[i+2:i+2+end], "[]{\\") {
				return true
			}
			i += 2 + end
		case braces == 0 && c == '$' && arrayIndexAt(script, i+1) > 0:
			i = arrayIndexAt(script, i+1)
			indexes = append(indexes, 0)
		case bare && c == '[':
			if n := len(indexes); n > 0 {
				indexes[n-1]++
			}
			wordStart, cmdStart = true, true
		case bare && c == ']' && len(indexes) > 0:
			if indexes[len(indexes)-1] == 0 {
				return true
			}
			indexes[len(indexes)-1]--
		case inIndex && c == ')':
			indexes = indexes[:len(indexes)-1]
		case bare && c == '#' && atCmd:
			return true
		case c == '"' && braces == 0:
			if quoted {
				if brackets != 0 {
					return true
				}
				quoted = false
			} else if atWord {
				quoted = true
			}
		case bare && c == '{' && atWord && isExpandPrefix(script, i):
			i += 2 // {*}: the next byte starts the word
			wordStart = true
		case c == '{' && !quoted && (braces > 0 || atWord):
			braces++
		case c == '}' && !quoted && braces > 0: // a bare `}` is literal
			if braces--; braces == 0 && brackets != 0 {
				return true
			}
		case (braces > 0 || quoted) && c == '[':
			if quoted && len(indexes) > 0 {
				indexes[len(indexes)-1]++
			}
			brackets++
		case (braces > 0 || quoted) && c == ']':
			if quoted && inIndex {
				return true
			}
			if quoted && len(indexes) > 0 {
				indexes[len(indexes)-1]--
			}
			if brackets--; brackets < 0 {
				return true
			}
		}
	}
	return braces != 0 || quoted || len(indexes) != 0
}

func refTrustedDepthSpan(text string, i int) (int, bool) {
	end := skipBracketSpan(text, i)
	if end == i+1 || text[end-1] != ']' || refBracesHideBrackets(text[i+1:end-1]) {
		return end, false
	}
	return end, true
}

// refSkipBraceSpan returns the index just past a balanced {..} span starting at i.
// Backslash-aware; tolerant of unterminated input (returns len(text)).
func refSkipBraceSpan(text string, i int) int {
	depth := 0
	for i < len(text) {
		switch text[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return len(text)
}

// spanAlphabet favours the bytes the scans react to.
const spanAlphabet = "[[]]{{}}\"\"$$\\\\ \n;#()a(k):*x}{]["

func randSpanText(r *rand.Rand, n int) string {
	var b strings.Builder
	for range n {
		b.WriteByte(spanAlphabet[r.Intn(len(spanAlphabet))])
	}
	return b.String()
}

// checkSpanMemo compares every span answer in s, through m, with the plain scans.
func checkSpanMemo(t *testing.T, m *scriptEnds, s string) {
	t.Helper()
	for i := range len(s) {
		switch s[i] {
		case '[':
			we, wok := refTrustedDepthSpan(s, i)
			if e, ok := m.trustedDepthSpan(s, i); e != we || ok != wok {
				t.Fatalf("trustedDepthSpan(%q, %d) = %d, %v; want %d, %v", s, i, e, ok, we, wok)
			}
			if e, _ := m.depthSpan(s, i); e != skipBracketSpan(s, i) {
				t.Fatalf("depthSpan(%q, %d) = %d; want %d", s, i, e, skipBracketSpan(s, i))
			}
			if e, ok := m.closedTrustedSpan(s, i); ok && (!wok || e != we) {
				t.Fatalf("closedTrustedSpan(%q, %d) = %d, true; want %d, %v", s, i, e, we, wok)
			}
		case '{':
			if e, w := m.skipBraceSpan(s, i), refSkipBraceSpan(s, i); e != w {
				t.Fatalf("skipBraceSpan(%q, %d) = %d; want %d", s, i, e, w)
			}
		}
		if got, want := m.bracesHideBrackets(s[i:]), refBracesHideBrackets(s[i:]); got != want {
			t.Fatalf("bracesHideBrackets(%q) = %v; want %v", s[i:], got, want)
		}
	}
}

// TestSpanMemoMatchesPlainScans compares every span answer, through one memo per
// text and over its prefixes (which share its memory), with the plain scans.
func TestSpanMemoMatchesPlainScans(t *testing.T) {
	r := rand.New(rand.NewSource(95))
	for range 4000 {
		full := randSpanText(r, 1+r.Intn(40))
		m := &scriptEnds{}
		for _, cut := range r.Perm(len(full) + 1) {
			checkSpanMemo(t, m, full[:cut])
		}
	}
}

// TestSpanMemoSubstrings does the same over random substrings of one text,
// which share its memory and one memo, in random order.
func TestSpanMemoSubstrings(t *testing.T) {
	r := rand.New(rand.NewSource(1234))
	for range 6000 {
		full := randSpanText(r, 1+r.Intn(60))
		m := &scriptEnds{}
		for range 40 {
			a := r.Intn(len(full) + 1)
			b := a + r.Intn(len(full)-a+1)
			checkSpanMemo(t, m, full[a:b])
		}
	}
}

// TestSpanMemoHandWritten runs shapes the random alphabet reaches only rarely
// through the same comparison, over every prefix with one memo: a `}` inside a
// nested span that the plain scan reads as closing an outer brace group, and
// `${...}` names inside nested spans.
func TestSpanMemoHandWritten(t *testing.T) {
	for _, full := range []string{
		"list {[a}] [set x 1]",
		"list [list {[a}]}] [set x 1]",
		"expr {[a}]} ; set x 1",
		"list [list [list {[}]}]]] x",
		"list [set ${a}] ${b} [set x 1]",
		"list [list ${a[}] x]",
		"list [list ${a]}] x]",
		"list [list \"${a}\" [list ${b}]] ${c}(",
		"list [expr {${a} + [list ${b}]}] x",
	} {
		m := &scriptEnds{}
		for cut := len(full); cut >= 0; cut-- {
			checkSpanMemo(t, m, full[:cut])
		}
		m = &scriptEnds{}
		for cut := range len(full) + 1 {
			checkSpanMemo(t, m, full[:cut])
		}
	}
}
