// Package expr parses the text Tcl's expr evaluates (an if or elseif condition,
// a while or for test, an expr argument) into a tree of its operators and
// operands, for the Checks. The parse is all or nothing: text it cannot read
// with certainty gives no tree, and a Check that gets none stays silent. So it
// rejects what Tcl rejects (a missing operand, an unbalanced paren, a `?` with
// no `:`), and also what Tcl 8.6 and 9.0 read differently or what it does not
// model: a `#` comment, a number with `_`, and the operators lt, gt, le and ge
// (all 9.0 only), an integer with a leading zero (octal in 8.6, decimal in 9.0),
// `${name}` with a `{` or `\` in the name, an unknown bareword, an unclosed or
// distrusted [script], and nesting deeper than maxDepth.
//
// Operator precedence, lowest first, checked against tclsh 8.6 and 9.0: `?:`
// (right to left), `||`, `&&`, `|`, `^`, `&`, `== != eq ne in ni`,
// `< > <= >=`, `<< >>`, `+ -`, `* / %`, `**` (right to left), then
// the unary `- + ~ !`, which bind tighter than `**` (`-2 ** 2` is 4).
package expr

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Kind is what a Node is.
type Kind int

const (
	// Literal is an operand with nothing to substitute: a number, a boolean
	// word (true, false, yes, no, on, off), a quoted string with no $ or [, or
	// a braced string.
	Literal Kind = iota + 1
	Var          // $name, ${name}, $name(index)
	Script       // [script]
	Quoted       // a quoted string holding a $var or [script]
	Call         // name(args): a math function, resolved when the expression runs
	Paren        // (x)
	Unary        // - + ~ ! x
	Binary       // x op y
	Ternary      // x ? y : z
)

// Node is one operand or operator of a parsed expression.
type Node struct {
	Kind Kind
	// Op is a Unary or Binary node's operator as written ("-", "==", "eq"),
	// "?" for a Ternary, and a Call's function name.
	Op         string
	Start, End int     // absolute byte range [Start,End) in the source
	Args       []*Node // a Unary's operand; a Binary's two; a Ternary's condition, then and else; a Call's arguments; a Paren's contents
}

// maxDepth bounds nesting, so hostile input cannot exhaust the stack.
const maxDepth = 256

// Parse parses text, which starts at absolute offset base, as one expression.
// ok is false when the parse is not certain; the tree is then nil.
func Parse(text string, base int) (n *Node, ok bool) {
	p := &parser{text: text, base: base, ok: true}
	n = p.ternary()
	p.space()
	if !p.ok || p.i != len(p.text) {
		return nil, false
	}
	return n, true
}

type parser struct {
	text  string
	base  int
	i     int
	depth int
	ok    bool
}

func (p *parser) fail() *Node {
	p.ok = false
	return nil
}

// enter counts one more level of nesting, and fails past maxDepth.
func (p *parser) enter() bool {
	p.depth++
	if p.depth > maxDepth {
		p.ok = false
	}
	return p.ok
}

func (p *parser) leave() { p.depth-- }

// space skips the whitespace expr skips, and a backslash-newline, which Tcl reads
// as a space even inside braces.
func (p *parser) space() {
	for p.i < len(p.text) {
		switch c := p.text[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			p.i++
		case c == '\\' && p.i+1 < len(p.text) && p.text[p.i+1] == '\n':
			p.i += 2
		default:
			return
		}
	}
}

func (p *parser) node(k Kind, op string, start int, args ...*Node) *Node {
	return &Node{Kind: k, Op: op, Start: p.base + start, End: p.base + p.i, Args: args}
}

func (p *parser) ternary() *Node {
	defer p.leave()
	if !p.enter() {
		return nil
	}
	cond := p.binary(0)
	if !p.ok {
		return nil
	}
	p.space()
	if p.op() != "?" {
		return cond
	}
	p.i++
	then := p.ternary()
	if !p.ok {
		return nil
	}
	p.space()
	if p.op() != ":" {
		return p.fail()
	}
	p.i++
	els := p.ternary()
	if !p.ok {
		return nil
	}
	return &Node{Kind: Ternary, Op: "?", Start: cond.Start, End: els.End, Args: []*Node{cond, then, els}}
}

// levels holds the binary operators by precedence, lowest first.
var levels = []map[string]bool{
	{"||": true},
	{"&&": true},
	{"|": true},
	{"^": true},
	{"&": true},
	{"==": true, "!=": true, "eq": true, "ne": true, "in": true, "ni": true},
	{"<": true, ">": true, "<=": true, ">=": true},
	{"<<": true, ">>": true},
	{"+": true, "-": true},
	{"*": true, "/": true, "%": true},
	{"**": true},
}

// powLevel is the level of `**`, the one binary operator that groups right to
// left.
var powLevel = len(levels) - 1

func (p *parser) binary(level int) *Node {
	if level == len(levels) {
		return p.unary()
	}
	left := p.binary(level + 1)
	for p.ok {
		p.space()
		op := p.op()
		if !levels[level][op] {
			return left
		}
		p.i += len(op)
		next := level + 1
		if level == powLevel {
			if !p.enter() {
				return nil
			}
			next = level
		}
		right := p.binary(next)
		if level == powLevel {
			p.leave()
		}
		if !p.ok {
			return nil
		}
		left = &Node{Kind: Binary, Op: op, Start: left.Start, End: right.End, Args: []*Node{left, right}}
		if level == powLevel {
			return left
		}
	}
	return nil
}

// symbolOps are the operators spelled with symbols, longest first where one is
// a prefix of another.
var symbolOps = []string{
	"**", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||",
	"*", "/", "%", "+", "-", "<", ">", "&", "^", "|", "?", ":",
}

// wordOps are the operators spelled as words. Tcl 9.0's lt, gt, le and ge are
// left out: 8.6 rejects them as barewords, so text using them is unparsed.
var wordOps = []string{"eq", "ne", "in", "ni"}

// op returns the binary or ternary operator at the cursor without reading it, or
// "" when there is none. A word operator followed by a name byte (`eqx`, `eq1`)
// is not read as one: Tcl's own reading of it is not modeled.
func (p *parser) op() string {
	rest := p.text[p.i:]
	for _, op := range symbolOps {
		if strings.HasPrefix(rest, op) {
			return op
		}
	}
	for _, op := range wordOps {
		if strings.HasPrefix(rest, op) && (len(rest) == len(op) || !isNameByte(rest[len(op)])) {
			return op
		}
	}
	return ""
}

func (p *parser) unary() *Node {
	defer p.leave()
	if !p.enter() {
		return nil
	}
	p.space()
	if p.i < len(p.text) {
		switch c := p.text[p.i]; c {
		case '-', '+', '~', '!':
			start := p.i
			p.i++
			x := p.unary()
			if !p.ok {
				return nil
			}
			return &Node{Kind: Unary, Op: string(c), Start: p.base + start, End: x.End, Args: []*Node{x}}
		}
	}
	return p.primary()
}

func (p *parser) primary() *Node {
	p.space()
	if p.i >= len(p.text) {
		return p.fail() // a missing operand
	}
	start := p.i
	switch c := p.text[p.i]; {
	case c == '(':
		p.i++
		x := p.ternary()
		if !p.ok {
			return nil
		}
		p.space()
		if p.i >= len(p.text) || p.text[p.i] != ')' {
			return p.fail()
		}
		p.i++
		return p.node(Paren, "", start, x)
	case c == '{':
		end, ok := braceEnd(p.text, p.i)
		if !ok {
			return p.fail()
		}
		p.i = end
		return p.node(Literal, "", start)
	case c == '"':
		return p.quoted()
	case c == '$':
		return p.variable()
	case c == '[':
		if !p.script() {
			return p.fail()
		}
		return p.node(Script, "", start)
	case isDigit(c) || c == '.':
		return p.number()
	case isNameByte(c):
		return p.word()
	}
	return p.fail()
}

// script reads the [script] at the cursor, and is false when its end is not
// certain.
func (p *parser) script() bool {
	end, ok := tcl.ExprScriptEnd(p.text, p.i)
	if !ok || end > len(p.text) || p.text[end-1] != ']' {
		return false
	}
	p.i = end
	return true
}

// quoted reads a quoted string, substituting as Tcl does up to its closing
// quote.
func (p *parser) quoted() *Node {
	start := p.i
	kind := Literal
	for p.i++; p.i < len(p.text); {
		switch p.text[p.i] {
		case '"':
			p.i++
			return p.node(kind, "", start)
		case '\\':
			p.i += 2
		case '$':
			if p.variable() == nil {
				return nil
			}
			kind = Quoted
		case '[':
			if !p.script() {
				return p.fail()
			}
			kind = Quoted
		default:
			p.i++
		}
	}
	return p.fail()
}

// variable reads the $var at the cursor: `$name`, `${name}`, or `$name(index)`,
// whose index is substituted up to the first `)`. A `$` Tcl would read as a
// literal (`$` alone, `$(k)` naming the array "") fails the parse.
func (p *parser) variable() *Node {
	start := p.i
	t := p.text
	i := p.i + 1
	if i < len(t) && t[i] == '{' {
		j := strings.IndexByte(t[i+1:], '}')
		// Tcl 8.6 ends the name at the first `}`, and 9.0 nests braces and
		// honors `\}` in it, so a `{` or `\` in the name is not certain.
		if j <= 0 || strings.ContainsAny(t[i+1:i+1+j], "{\\") {
			return p.fail()
		}
		p.i = i + 2 + j
		return p.node(Var, "", start)
	}
	j := nameEnd(t, i)
	if j == i {
		return p.fail()
	}
	p.i = j
	if j < len(t) && t[j] == '(' {
		for p.i++; ; {
			if p.i >= len(t) {
				return p.fail()
			}
			switch t[p.i] {
			case ')':
				p.i++
				return p.node(Var, "", start)
			case '\\':
				p.i += 2
			case '$':
				if p.variable() == nil {
					return nil
				}
			case '[':
				if !p.script() {
					return p.fail()
				}
			default:
				p.i++
			}
		}
	}
	return p.node(Var, "", start)
}

// number reads a numeric literal: decimal digits with an optional fraction and
// exponent, or 0x, 0o or 0b digits. A name byte or `.` right after it (`1_000`,
// `0d10`, `1eq`) fails the parse, since Tcl 8.6 and 9.0 read those differently,
// as does an integer of two or more digits that starts with 0 (`010` is 8 in
// 8.6 and 10 in 9.0). A fraction or exponent makes it decimal in both (`00.5`).
func (p *parser) number() *Node {
	start := p.i
	t := p.text
	i := p.i
	digits := func(ok func(byte) bool) int {
		n := 0
		for i < len(t) && ok(t[i]) {
			i++
			n++
		}
		return n
	}
	if t[i] == '0' && i+1 < len(t) && strings.IndexByte("xXoObB", t[i+1]) >= 0 {
		base := t[i+1] | 0x20
		i += 2
		ok := isHexDigit
		switch base {
		case 'o':
			ok = func(b byte) bool { return b >= '0' && b <= '7' }
		case 'b':
			ok = func(b byte) bool { return b == '0' || b == '1' }
		}
		if digits(ok) == 0 {
			return p.fail()
		}
	} else {
		n := digits(isDigit)
		integer := true
		if i < len(t) && t[i] == '.' {
			i++
			n += digits(isDigit)
			integer = false
		}
		if n == 0 {
			return p.fail()
		}
		if i < len(t) && (t[i] == 'e' || t[i] == 'E') {
			integer = false
			i++
			if i < len(t) && (t[i] == '+' || t[i] == '-') {
				i++
			}
			if digits(isDigit) == 0 {
				return p.fail()
			}
		}
		if integer && n > 1 && t[start] == '0' {
			return p.fail() // octal in 8.6, decimal in 9.0
		}
	}
	if i < len(t) && (isNameByte(t[i]) || t[i] == '.') {
		return p.fail()
	}
	p.i = i
	return p.node(Literal, "", start)
}

// booleans are the barewords both Tcl 8.6 and 9.0 read as boolean literals,
// compared without case. Tcl also takes unique prefixes (`t`, `fal`); those
// fail the parse.
var booleans = map[string]bool{"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true}

// word reads a bareword: a math function call when a `(` follows, a boolean
// literal, or else a parse failure.
func (p *parser) word() *Node {
	start := p.i
	for p.i < len(p.text) && isNameByte(p.text[p.i]) {
		p.i++
	}
	name := p.text[start:p.i]
	p.space()
	if p.i < len(p.text) && p.text[p.i] == '(' {
		return p.call(name, start)
	}
	if !booleans[strings.ToLower(name)] {
		return p.fail()
	}
	return &Node{Kind: Literal, Start: p.base + start, End: p.base + start + len(name)}
}

// call reads a function's arguments, from its `(` through its `)`.
func (p *parser) call(name string, start int) *Node {
	p.i++ // (
	var args []*Node
	p.space()
	if p.i < len(p.text) && p.text[p.i] == ')' {
		p.i++
		return p.node(Call, name, start) // no arguments: rand()
	}
	for {
		a := p.ternary()
		if !p.ok {
			return nil
		}
		args = append(args, a)
		p.space()
		if p.i >= len(p.text) {
			return p.fail()
		}
		switch p.text[p.i] {
		case ',':
			p.i++
		case ')':
			p.i++
			return p.node(Call, name, start, args...)
		default:
			return p.fail()
		}
	}
}

// braceEnd returns the offset just past the brace that closes the one at t[i],
// counting nesting and skipping backslash-escaped bytes, as Tcl does.
func braceEnd(t string, i int) (int, bool) {
	depth := 0
	for j := i; j < len(t); j++ {
		switch t[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return j + 1, true
			}
		}
	}
	return 0, false
}

// nameEnd returns the end of the variable name starting at s[i], as `$` reads
// it: name bytes, and runs of two or more colons between them.
func nameEnd(s string, i int) int {
	for i < len(s) {
		switch {
		case isNameByte(s[i]):
			i++
		case s[i] == ':' && i+1 < len(s) && s[i+1] == ':':
			for i < len(s) && s[i] == ':' {
				i++
			}
		default:
			return i
		}
	}
	return i
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isHexDigit(b byte) bool {
	return isDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func isNameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || isDigit(b)
}
