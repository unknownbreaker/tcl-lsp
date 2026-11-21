package check

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/expr"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const constantConditionID = "constant-condition"

// constantCondition flags an if or elseif condition, a while test or a for test
// whose value is fixed because it compares only literals: `if {1 == 1}`,
// `while {"a" eq "b"}`. A comparison counts only when both its operands are
// literals as written (a number, a boolean word, a string with nothing to
// substitute), so a value Tcl computes first, whose string form may hang on
// tcl_precision, never does. The condition may join such comparisons with
// `!`, `&&`, `||`, `?:` and parentheses, and nothing else: a $var, a [script]
// or a math function call (rand(), or a user's tcl::mathfunc) leaves it
// silent, as does any text the expression parser cannot read. Each operand of
// a joiner, and each `?:` arm, is a comparison or a bare 0 or 1, so a condition
// with no value (`"a" || 1 == 1` always errors) is not called constant.
//
// An if or elseif condition that is just `0` or `1` is the idiom that disables
// or forces a block, so it is a hint. `while 1` and `for {} 1 {}`, the idiomatic
// infinite loops, compare nothing and are never flagged; nor is any other bare
// literal or literal arithmetic.
func constantCondition(c tcl.Command, base int, out []Finding) []Finding {
	var buf [4]tcl.Word
	conds := tcl.AppendConditions(buf[:0], c.Words)
	if len(conds) == 0 {
		return out
	}
	head := c.Words[0].Text
	for _, w := range conds {
		text, ok := exprText(w)
		if !ok || strings.ContainsAny(text, "$[") {
			continue // a substitution: never constant, so skip the parse
		}
		n, ok := expr.Parse(text, 0)
		if !ok {
			continue
		}
		f := Finding{Start: base + w.Start, End: base + w.End, Assumes: []string{head}}
		switch lit := text[n.Start:n.End]; {
		case head == "if" && n.Kind == expr.Literal && isBit(lit):
			f.Severity = Hint
			f.Message = "condition is always false"
			if lit == "1" {
				f.Message = "condition is always true"
			}
		case comparesLiterals(text, n):
			f.Severity = Warning
			f.Message = "condition compares only literals, so its value never changes"
		default:
			continue
		}
		out = append(out, f)
	}
	return out
}

// exprText returns the text expr evaluates for a condition word: a braced word's
// interior, or the value of a bare or quoted word with nothing to substitute. ok
// is false for a word with a substitution or a backslash, whose value differs
// from its text.
func exprText(w tcl.Word) (text string, ok bool) {
	t := w.Text
	switch w.Kind {
	case tcl.WordBraced:
		if len(t) < 2 || t[len(t)-1] != '}' {
			return "", false
		}
		return t[1 : len(t)-1], true
	case tcl.WordBare:
		if strings.ContainsAny(t, "$[\\") {
			return "", false
		}
		return t, true
	case tcl.WordQuoted:
		if !quoteCloses(w) || strings.ContainsAny(t, "$[\\") {
			return "", false
		}
		return t[1 : len(t)-1], true
	}
	return "", false
}

// comparisons are the operators that compare their two operands.
var comparisons = map[string]bool{
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"eq": true, "ne": true, "in": true, "ni": true,
}

// comparesLiterals reports whether n, parsed from text at offset 0, holds at
// least one comparison and its value is fixed by comparisons of literals alone
// (constantCondition).
func comparesLiterals(text string, n *expr.Node) bool {
	fixed, compares := literalLogic(text, n, 0)
	return fixed && compares
}

// maxLogicDepth bounds literalLogic's recursion. The parser builds a chain of
// `&&` or `||` left-deep with no nesting limit, so a huge generated condition
// is silent instead of exhausting the stack.
const maxLogicDepth = 1000

// literalLogic reports whether n is built only from comparisons of two literals,
// the bare literals 0 and 1, and the logic operators over them, and whether it
// holds a comparison. depth counts the logic operators above n.
func literalLogic(text string, n *expr.Node, depth int) (fixed, compares bool) {
	if depth > maxLogicDepth {
		return false, false
	}
	n = unparen(n)
	switch {
	case n.Kind == expr.Literal:
		lit := text[n.Start:n.End]
		return isBit(lit), false
	case n.Kind == expr.Unary && n.Op == "!":
		return literalLogic(text, n.Args[0], depth+1)
	case n.Kind == expr.Binary && comparisons[n.Op]:
		l, r := unparen(n.Args[0]), unparen(n.Args[1])
		fixed = l.Kind == expr.Literal && r.Kind == expr.Literal
		if n.Op == "in" || n.Op == "ni" {
			fixed = fixed && plainList(text[r.Start:r.End])
		}
		return fixed, true
	case n.Kind == expr.Binary && (n.Op == "&&" || n.Op == "||"), n.Kind == expr.Ternary:
		for _, a := range n.Args {
			f, c := literalLogic(text, a, depth+1)
			if !f {
				return false, false
			}
			compares = compares || c
		}
		return true, compares
	}
	return false, false
}

// plainList reports whether lit, a literal as written, is a list with no `{`,
// `"` or `\` inside its delimiters, so it is certain to parse as a list: `{a b}`
// does, and `"\{a"` or `{{a}b}` makes in and ni raise an error.
func plainList(lit string) bool {
	if lit[0] == '{' || lit[0] == '"' {
		lit = lit[1 : len(lit)-1]
	}
	return !strings.ContainsAny(lit, "{\"\\")
}

// isBit reports whether lit, a literal as written, is a bare 0 or 1.
func isBit(lit string) bool { return lit == "0" || lit == "1" }

// unparen returns n without the parentheses around it.
func unparen(n *expr.Node) *expr.Node {
	for n.Kind == expr.Paren {
		n = n.Args[0]
	}
	return n
}
