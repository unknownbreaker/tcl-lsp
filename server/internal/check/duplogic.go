package check

import (
	"strconv"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/expr"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const duplicateLogicID = "duplicate-logic"

// duplicateLogic flags logic that repeats itself, so the repeat changes
// nothing:
//
//   - an operand of a chain of `||` or `&&` that repeats an earlier operand of
//     the same chain (`$a == 1 || $a == 1`, `$x || $y || $x`), in an if or
//     elseif condition, a while or for test or `expr {…}`. An earlier copy
//     decided the chain already if it could.
//   - an elseif condition that repeats an earlier condition of the same if. The
//     earlier one was false, so the repeat is false too and its body never runs.
//   - an if with an else whose bodies are all the same braced text: the
//     conditions pick which branch runs, and every branch does the same.
//
// A repeat must be written like the earlier copy, up to spaces and parentheses,
// and hold a $var: a literal repeat is the constant-condition Check's. Nothing
// from the earlier copy to the repeat may call a command or a math function (a
// [script], rand(), a user's tcl::mathfunc): a call may return something new
// each time, or change a variable the repeat reads. Bodies count only braced,
// since Tcl substitutes a quoted or bare body once per word.
func duplicateLogic(c tcl.Command, base int, out []Finding) []Finding {
	var buf [4]tcl.Word
	conds, head := appendExprWords(buf[:0], c)
	if len(conds) == 0 {
		return out
	}
	// An if with an elseif compares its conditions by key (conditionKey); a
	// condition left unread keeps the key "", as one that may differ between
	// evaluations.
	compare := head == "if" && len(conds) > 1
	var keyBuf [4]string
	keys := keyBuf[:0]
	for _, w := range conds {
		keys = append(keys, "")
		text, ok := exprText(w)
		if !ok {
			continue
		}
		chain := strings.Contains(text, "$") && (strings.Contains(text, "||") || strings.Contains(text, "&&"))
		if !chain && !compare {
			continue // nothing to repeat, so skip the parse
		}
		off := exprOffset(w, base)
		n, ok := expr.Parse(text, off)
		if !ok {
			continue
		}
		if chain {
			out = appendRepeatedOperands(out, n, text, off, head)
		}
		if compare {
			keys[len(keys)-1] = conditionKey(n, text, off)
		}
	}
	if compare {
		out = appendRepeatedConditions(out, conds, keys, base)
	}
	if head == "if" {
		out = appendSameBodies(out, c, len(conds), base)
	}
	return out
}

// appendRepeatedConditions appends a finding for each elseif condition whose
// key repeats an earlier condition's with only pure conditions between them.
func appendRepeatedConditions(out []Finding, conds []tcl.Word, keys []string, base int) []Finding {
	for j := 1; j < len(keys); j++ {
		if keys[j] == "" || keys[j] == pureNoVar {
			continue
		}
		for i := j - 1; i >= 0 && keys[i] != ""; i-- {
			if keys[i] == keys[j] {
				w := conds[j]
				out = append(out, Finding{
					Severity: Warning,
					Start:    base + w.Start,
					End:      base + w.End,
					Message:  "repeats an earlier condition, which was false, so this branch never runs",
					Assumes:  []string{"if"},
				})
				break
			}
		}
	}
	return out
}

// appendSameBodies appends a finding on the else body of if command c, which
// has nconds conditions, when every one of its bodies is the same braced text.
func appendSameBodies(out []Finding, c tcl.Command, nconds int, base int) []Finding {
	var buf [4]tcl.Word
	bodies := tcl.AppendIfBodies(buf[:0], c.Words)
	if len(bodies) != nconds+1 {
		return out // no else body
	}
	for _, b := range bodies {
		if b.Kind != tcl.WordBraced || len(b.Text) < 2 || b.Text[len(b.Text)-1] != '}' || b.Text != bodies[0].Text {
			return out
		}
	}
	msg := "the else body repeats the if body, so the condition changes nothing"
	if nconds > 1 {
		msg = "every branch runs the same body, so the conditions change nothing"
	}
	last := bodies[len(bodies)-1]
	return append(out, Finding{
		Severity: Warning,
		Start:    base + last.Start,
		End:      base + last.End,
		Message:  msg,
		Assumes:  []string{"if"},
	})
}

// appendRepeatedOperands appends a finding for each operand of a chain of `||`
// or `&&` in n, parsed from text at absolute offset off, that repeats an earlier
// operand of its chain with only pure operands between them. The walk keeps its
// own stack, as a chain nests with no limit; a node's chainOp is the logic
// operator of the chain it is an operand of, if any, so each chain is read once,
// from its top.
func appendRepeatedOperands(out []Finding, n *expr.Node, text string, off int, head string) []Finding {
	type item struct {
		n       *expr.Node
		chainOp string
	}
	stack := []item{{n: n}}
	var operands []*expr.Node
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := it.n
		op := ""
		switch {
		case n.Kind == expr.Paren:
			op = it.chainOp
		case n.Kind == expr.Binary && (n.Op == "||" || n.Op == "&&"):
			op = n.Op
			if it.chainOp != n.Op {
				operands = chainOperands(operands[:0], n)
				out = appendRepeats(out, operands, n.Op, text, off, head)
			}
		}
		for i := len(n.Args) - 1; i >= 0; i-- {
			stack = append(stack, item{n.Args[i], op})
		}
	}
	return out
}

// chainOperands appends to dst the operands, left to right and without their
// parentheses, of the chain of n.Op that n tops: `$a || ($b || $c)` has three.
func chainOperands(dst []*expr.Node, n *expr.Node) []*expr.Node {
	stack := []*expr.Node{n}
	for len(stack) > 0 {
		m := unparen(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if m.Kind == expr.Binary && m.Op == n.Op {
			stack = append(stack, m.Args[1], m.Args[0])
			continue
		}
		dst = append(dst, m)
	}
	return dst
}

// appendRepeats appends a finding for each of a chain's operands that repeats
// an earlier one with only pure operands from that one to it. The nearest
// earlier copy is the one compared, so an impure operand before it does not
// matter.
func appendRepeats(out []Finding, operands []*expr.Node, op, text string, off int, head string) []Finding {
	var seen map[string]int // key → index of its last pure copy
	lastImpure := -1
	for j, o := range operands {
		k := conditionKey(o, text, off)
		if k == "" {
			lastImpure = j
			continue
		}
		if k == pureNoVar {
			continue
		}
		if i, ok := seen[k]; ok && i > lastImpure {
			out = append(out, Finding{
				Severity: Warning,
				Start:    o.Start,
				End:      o.End,
				Message:  "repeats an earlier operand of " + op + ", so it changes nothing",
				Assumes:  []string{head},
			})
		}
		if seen == nil {
			seen = map[string]int{}
		}
		seen[k] = j
	}
	return out
}

// pureNoVar is conditionKey's answer for a pure expression with no variable.
const pureNoVar = "\x00"

// maxKeyDepth bounds conditionKey's recursion, as a chain of one arithmetic
// operator nests with no limit. A deeper expression gets no key.
const maxKeyDepth = 1000

// conditionKey returns a key that two expressions share exactly when they are
// written alike up to spaces and parentheses, so they compute the same value
// from the same variables. It is "" when n is not pure: it holds a [script], a
// math function call or text the key cannot read for certain; and pureNoVar
// when n is pure but reads no variable. Leaves are keyed by their text, length
// first, so no two trees share a key.
func conditionKey(n *expr.Node, text string, off int) string {
	var b strings.Builder
	hasVar := false
	if !writeKey(&b, n, text, off, 0, &hasVar) {
		return ""
	}
	if !hasVar {
		return pureNoVar
	}
	return b.String()
}

// writeKey writes n's key to b (conditionKey), and reports whether n is pure.
func writeKey(b *strings.Builder, n *expr.Node, text string, off, depth int, hasVar *bool) bool {
	if depth > maxKeyDepth {
		return false
	}
	n = unparen(n)
	switch n.Kind {
	case expr.Script, expr.Call:
		return false
	case expr.Literal, expr.Var, expr.Quoted:
		leaf := text[n.Start-off : n.End-off]
		if n.Kind != expr.Literal {
			if strings.Contains(leaf, "[") {
				return false // a [script] in a string or an array index
			}
			*hasVar = true
		}
		b.WriteByte(byte('0' + n.Kind))
		b.WriteString(strconv.Itoa(len(leaf)))
		b.WriteByte(':')
		b.WriteString(leaf)
		return true
	}
	b.WriteByte(byte('0' + n.Kind))
	b.WriteString(n.Op)
	b.WriteByte('(')
	for _, a := range n.Args {
		if !writeKey(b, a, text, off, depth+1, hasVar) {
			return false
		}
		b.WriteByte(',')
	}
	b.WriteByte(')')
	return true
}
