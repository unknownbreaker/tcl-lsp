package tcl

import "strings"

// Expression structure. expr evaluates `a ? b : c`, `||` and `&&` lazily: an
// operand after && runs only when every earlier operand of its chain ran and was
// true, one after || only when every earlier one ran and was false, and a ternary
// arm only when its condition ran (checked against tclsh 8.6 and 9.0). Everything
// else (arithmetic, comparisons, function arguments) runs left to right. So a
// braced expression is parsed into that control structure, and a use inside it
// sees exactly the operands that ran before it. The parse is all or nothing: text
// it cannot read (an unbalanced paren, a `?` with no `:`) returns false, and
// callers fall back to walkSubst's flat scan.

// exprItem is one substitution in an operand, in evaluation order: a $var, a
// [script], or a parenthesized group (a function call's arguments or a subexpression).
type exprItem struct {
	ref       VarRef
	isVar     bool
	inner     string // a script's interior
	innerBase int
	isScript  bool
	group     []*exprTern // a group's comma-separated expressions
	isGroup   bool
}

// exprSeq is an operand with no lazy operator at its level. pure is true when the
// operand is a single parenthesized expression and nothing else, so its truth is
// that expression's.
type exprSeq struct {
	items []exprItem
	pure  bool
}

// exprTern is `cond ? then : els`, or just cond when then is nil. cond is a list
// of || operands, each a list of && operands.
type exprTern struct {
	cond      [][]exprSeq
	then, els *exprTern
}

type exprParser struct {
	ends *scriptEnds // spans found once (scriptends.go)
	text string
	base int
	i    int
	ok   bool
}

// parseExprTree parses an expression's text, at absolute offset base, finding
// spans through ends.
func parseExprTree(text string, base int, ends *scriptEnds) (*exprTern, bool) {
	p := &exprParser{ends: ends, text: text, base: base, ok: true}
	t := p.tern()
	p.space()
	if !p.ok || p.i < len(p.text) {
		return nil, false
	}
	return t, true
}

func (p *exprParser) space() {
	for p.i < len(p.text) && isListSpace(p.text[p.i]) {
		p.i++
	}
}

// at reports whether the lazy operator or delimiter op starts at the cursor.
func (p *exprParser) at(op string) bool {
	p.space()
	if len(p.text)-p.i < len(op) || p.text[p.i:p.i+len(op)] != op {
		return false
	}
	// `::` belongs to a qualified function name, not a ternary.
	if op == ":" && p.i+1 < len(p.text) && p.text[p.i+1] == ':' {
		return false
	}
	return true
}

func (p *exprParser) tern() *exprTern {
	t := &exprTern{cond: p.or()}
	if p.ok && p.at("?") {
		p.i++
		t.then = p.tern()
		if !p.ok || !p.at(":") {
			p.ok = false
			return t
		}
		p.i++
		t.els = p.tern()
	}
	return t
}

func (p *exprParser) or() [][]exprSeq {
	ops := [][]exprSeq{p.and()}
	for p.ok && p.at("||") {
		p.i += 2
		ops = append(ops, p.and())
	}
	return ops
}

func (p *exprParser) and() []exprSeq {
	ops := []exprSeq{p.seq()}
	for p.ok && p.at("&&") {
		p.i += 2
		ops = append(ops, p.seq())
	}
	return ops
}

// seq reads one operand, up to a lazy operator or a delimiter of the enclosing
// level. An empty operand is a parse failure.
func (p *exprParser) seq() exprSeq {
	var s exprSeq
	start := p.i
	text := false // non-space text other than a group
	for p.ok && p.i < len(p.text) {
		c := p.text[p.i]
		switch {
		case isListSpace(c):
			p.i++
			continue
		case c == ')' || c == ',' || c == '?':
			return p.endSeq(s, start, text)
		case c == ':' && !(p.i+1 < len(p.text) && p.text[p.i+1] == ':'):
			return p.endSeq(s, start, text)
		case (c == '&' || c == '|') && p.i+1 < len(p.text) && p.text[p.i+1] == c:
			return p.endSeq(s, start, text)
		case c == ':':
			p.i += 2
			text = true
		case c == '(':
			p.i++
			s.items = append(s.items, exprItem{isGroup: true, group: p.group()})
			continue
		case c == '{':
			p.i = p.ends.skipBraceSpan(p.text, p.i)
			if p.text[p.i-1] != '}' {
				p.ok = false
			}
			text = true
		case c == '"':
			p.i++
			s.items = p.subst(s.items, '"')
			text = true
		case c == '$' || c == '[':
			s.items = p.substOne(s.items)
			text = true
		case c == '#':
			// Tcl 9.0 reads a comment to end of line here, and 8.6 an error. Neither
			// is modeled: the flat scan answers instead.
			p.ok = false
		case c == '\\' && p.i+1 < len(p.text):
			p.i += 2
			text = true
		default:
			p.i++
			text = true
		}
	}
	return p.endSeq(s, start, text)
}

func (p *exprParser) endSeq(s exprSeq, start int, text bool) exprSeq {
	if p.i == start || len(s.items) == 0 && !text {
		p.ok = false
	}
	s.pure = !text && len(s.items) == 1 && s.items[0].isGroup && len(s.items[0].group) == 1
	return s
}

// group reads a parenthesized list after its `(`, through the `)`.
func (p *exprParser) group() []*exprTern {
	var out []*exprTern
	if p.at(")") {
		p.i++
		return out // a call with no arguments: rand()
	}
	for p.ok {
		out = append(out, p.tern())
		switch {
		case !p.ok:
		case p.at(","):
			p.i++
			continue
		case p.at(")"):
			p.i++
			return out
		default:
			p.ok = false
		}
	}
	return out
}

// subst reads substitutions up to the unescaped byte end (a quote's `"`, an
// index's `)`), and past it. Unterminated text fails the parse.
func (p *exprParser) subst(items []exprItem, end byte) []exprItem {
	for p.ok && p.i < len(p.text) {
		c := p.text[p.i]
		switch {
		case c == end:
			p.i++
			return items
		case c == '\\' && p.i+1 < len(p.text):
			p.i += 2
		case c == '$' || c == '[':
			items = p.substOne(items)
		default:
			p.i++
		}
	}
	p.ok = false
	return items
}

// substOne reads the $var or [script] at the cursor. An array element's index is
// substituted before the variable is read, so its items come first; the index ends
// at the first `)`, as Tcl's does.
func (p *exprParser) substOne(items []exprItem) []exprItem {
	if p.text[p.i] == '[' {
		end, ok := p.ends.trustedDepthSpan(p.text, p.i)
		if !ok {
			p.ok = false
			return items
		}
		items = append(items, exprItem{isScript: true, inner: p.text[p.i+1 : end-1], innerBase: p.base + p.i + 1})
		p.i = end
		return items
	}
	ref, next, ok := parseVarRef(p.text, p.i, p.base)
	if !ok {
		p.i = next
		return items
	}
	p.i = next
	if p.text[ref.Start-p.base+1] != '{' && p.i < len(p.text) && p.text[p.i] == '(' {
		p.i++
		items = p.subst(items, ')')
	}
	return append(items, exprItem{isVar: true, ref: ref})
}

// visit calls onVar and onScript for every substitution in t, in source order.
// conditional is true for a script that may not run when the expression does.
func (t *exprTern) visit(onVar func(VarRef), onScript scriptFunc, conditional bool) {
	for i, and := range t.cond {
		for j, s := range and {
			s.visit(onVar, onScript, conditional || i > 0 || j > 0)
		}
	}
	if t.then != nil {
		t.then.visit(onVar, onScript, true)
		t.els.visit(onVar, onScript, true)
	}
}

func (s exprSeq) visit(onVar func(VarRef), onScript scriptFunc, conditional bool) {
	for _, it := range s.items {
		switch {
		case it.isVar:
			onVar(it.ref)
		case it.isScript:
			onScript(it.inner, it.innerBase, conditional)
		case it.isGroup:
			for _, g := range it.group {
				g.visit(onVar, onScript, conditional)
			}
		}
	}
}

// bracesHideBrackets reports whether a script's span may have ended at the wrong
// `]`: skipBracketSpan knows neither braces nor quotes, so a span whose braces or
// quotes do not balance, or whose braced or quoted text holds unbalanced brackets
// (`[string map {] x} $s]`, `[string length "]"; set x 1]`), is not trusted. As in
// Tcl, only a `"` that starts a word opens a quote (`a"b` is literal), and a
// backslash-newline or a nested `[` starts a word. A `#` where a command starts
// begins a comment, which may hide a `]`, so it is not trusted either. A nested
// quote inside a quoted bracket also counts as unbalanced, which only costs silence.
// A `$name(` index, bare or in a quoted word, runs to its `)` and hides a `]` the
// same way (`[list $a(]); set x 1]`, and in Tcl 8.6 `[list "$a(k " ] )"]`), so a
// `]` at the index's own level, or an index that never closes, is not trusted; a
// `[...]` nested in the index is a command, and quotes, braces and spaces in it
// are literal. A bare `{` opens braces only where a word starts (`a{b}` is
// literal); `${name}` is a variable name.
func bracesHideBrackets(script string) bool {
	var m *scriptEnds
	return m.bracesHideBrackets(script)
}

// bracesHideBrackets is bracesHideBrackets, jumping over each [script] nested at
// the top level that closes and is trusted (scriptends.go). Reading such a span
// byte by byte starts and ends in the same state (bare, no index open, no word
// or command starting after its `]`) and returns nothing on the way, so the
// answer is the same.
func (m *scriptEnds) bracesHideBrackets(script string) bool {
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
		case bare && c == '[' && len(indexes) == 0:
			if end, ok := m.closedTrustedSpan(script, i); ok {
				i = end - 1
				continue
			}
			wordStart, cmdStart = true, true
		case bare && c == '[':
			indexes[len(indexes)-1]++
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
		case braces > 0 && c == '[' && m.jumpBraced(script, &i, &braces):
			// Inside braces only braces and brackets count, and a span's brackets
			// balance, so only its braces matter: a `}` in it that closes the
			// braces it sits in does so with a bracket still open.
			if braces <= 0 {
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

// jumpBraced moves *i to the `]` of the [script] span at *i when the span closes
// in script, and adds the span's net braces to *braces, or sets *braces to 0
// when a `}` in the span closes the braces it sits in. It is false, changing
// nothing, when the span does not close.
func (m *scriptEnds) jumpBraced(script string, i, braces *int) bool {
	e, end, closed := m.depthScan(script, *i)
	if !closed {
		return false
	}
	if *braces+e.braceLow <= 0 {
		*braces = 0
	} else {
		*braces += e.braceNet
	}
	*i = end - 1
	return true
}

// arrayIndexAt returns the offset of the `(` that opens an array index when a
// variable name (nameEnd; possibly empty, as `$(k)` reads the array named "")
// starts at i and is followed by one, and 0 otherwise.
func arrayIndexAt(s string, i int) int {
	j := nameEnd(s, i)
	if j < len(s) && s[j] == '(' {
		return j
	}
	return 0
}
