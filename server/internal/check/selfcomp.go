package check

import (
	"fmt"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/expr"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const selfComparisonID = "self-comparison"

// selfComparison flags a comparison whose two operands are the same
// substitution, so its result is fixed: `$x eq $x` and `$x ne $x` in an if or
// elseif condition, a while or for test or `expr {…}`, `$x < $x` and `$x > $x`,
// and the commands `string equal $x $x` and `string compare $x $x`.
//
// The operands must be written alike, hold a $var and no [script] (a script may
// give a new value per call): `$x`, `$a($i)`, `"a$x"`. `==`, `!=`, `<=` and `>=`
// are left alone, since a NaN is not equal to itself (`set x NaN` makes
// `$x != $x` true in Tcl 8.6 and 9.0), and so are in and ni, whose result hangs
// on the value. eq and ne compare strings, and < and > are false for every
// value, NaN too. The command forms count only with no options, so the two
// words after the subcommand are the strings (tclsh 8.6 and 9.0).
func selfComparison(c tcl.Command, base int, out []Finding) []Finding {
	out = selfCompareCommand(c, base, out)
	var buf [4]tcl.Word
	conds := tcl.AppendConditions(buf[:0], c.Words)
	head := c.Words[0].Text
	if len(conds) == 0 {
		w := c.Words
		if len(w) != 2 || builtinHead(c) != "expr" || w[1].Kind != tcl.WordBraced {
			return out
		}
		conds, head = append(conds, w[1]), "expr"
	}
	for _, w := range conds {
		text, ok := exprText(w)
		if !ok || !strings.Contains(text, "$") || !mayCompare(text) {
			continue // nothing to compare with itself, so skip the parse
		}
		off := base + w.Start
		if w.Kind != tcl.WordBare {
			off++ // past the opening brace or quote
		}
		n, ok := expr.Parse(text, off)
		if !ok {
			continue
		}
		out = appendSelfComparisons(out, n, text, off, head)
	}
	return out
}

// selfResult is the fixed result of comparing a value with itself, by operator.
// Operators left out may vary (selfComparison).
var selfResult = map[string]string{"eq": "true", "ne": "false", "<": "false", ">": "false"}

// appendSelfComparisons appends a finding for each comparison in n, parsed from
// text at absolute offset off, whose operands are the same substitution. The walk
// keeps its own stack: a chain of `&&` or `||` nests with no limit.
func appendSelfComparisons(out []Finding, n *expr.Node, text string, off int, head string) []Finding {
	stack := []*expr.Node{n}
	for len(stack) > 0 {
		n = stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := len(n.Args) - 1; i >= 0; i-- {
			stack = append(stack, n.Args[i]) // so the leftmost is reported first
		}
		res, ok := selfResult[n.Op]
		if n.Kind != expr.Binary || !ok {
			continue
		}
		l, r := unparen(n.Args[0]), unparen(n.Args[1])
		lt, rt := text[l.Start-off:l.End-off], text[r.Start-off:r.End-off]
		if !sameSubst(l.Kind == expr.Var || l.Kind == expr.Quoted, lt, rt) {
			continue
		}
		out = append(out, Finding{
			Severity: Warning,
			Start:    n.Start,
			End:      n.End,
			Message:  fmt.Sprintf("compares %s with itself, so it is always %s", operandName(lt), res),
			Assumes:  []string{head},
		})
	}
	return out
}

// selfCompareResult is the fixed result of `string equal` and `string compare`
// on one value.
var selfCompareResult = map[string]string{"equal": "1", "compare": "0"}

// selfCompareCommand appends a finding when c is `string equal A A` or
// `string compare A A`, with A the same substitution twice.
func selfCompareCommand(c tcl.Command, base int, out []Finding) []Finding {
	w := c.Words
	if len(w) != 4 || builtinHead(c) != "string" || w[1].Kind != tcl.WordBare {
		return out
	}
	sub := w[1].Text
	res, ok := selfCompareResult[sub]
	if !ok || w[1].End >= w[2].Start || w[2].End >= w[3].Start {
		return out
	}
	if !sameSubst(substWord(w[2]) && wordAsTcl(w[2].Text), w[2].Text, w[3].Text) {
		return out
	}
	start, end := commandSpan(c, base)
	return append(out, Finding{
		Severity: Warning,
		Start:    start,
		End:      end,
		Message:  fmt.Sprintf("compares %s with itself, so it always returns %s", operandName(w[2].Text), res),
		Assumes:  []string{"string", sub},
	})
}

// substWord reports whether w is a bare word or a closed quoted word, the kinds
// whose $vars Tcl substitutes.
func substWord(w tcl.Word) bool {
	switch w.Kind {
	case tcl.WordBare:
		return true
	case tcl.WordQuoted:
		return len(w.Text) >= 2 && w.Text[len(w.Text)-1] == '"'
	}
	return false
}

// sameSubst reports whether two operands, a and b as written, certainly have
// the same value although it is not known: kindOK says the first is a kind Tcl
// substitutes, and both are the same text holding a $ and no [script].
func sameSubst(kindOK bool, a, b string) bool {
	return kindOK && a == b && strings.Contains(a, "$") && !strings.Contains(a, "[")
}

// mayCompare reports whether expression text may hold an operator selfResult
// knows outside any [script]. A script's own comparisons are its command's, met
// when the visitor reaches it; skipping them keeps nested `[expr {…}]` from being
// parsed again at every level. Each operator's places are found, and the bracket
// depth before each counted, with the strings package's vectorized search, so
// the scan stays cheap beside the parse it saves. A `[` or `]` escaped or in
// braces is counted too: a wrong depth only costs a parse, or a finding missed.
func mayCompare(text string) bool {
	for _, op := range [...]string{"eq", "ne", "<", ">"} {
		depth, from := 0, 0
		for {
			k := strings.Index(text[from:], op)
			if k < 0 {
				break
			}
			seg := text[from : from+k]
			depth += strings.Count(seg, "[") - strings.Count(seg, "]")
			if depth <= 0 {
				return true
			}
			from += k + len(op)
		}
	}
	return false
}

// wordAsTcl reports whether a command-form operand ends where Tcl ends it. Tcl
// reads `$name(` on to the `)` that closes the index, across spaces, newlines,
// semicolons and quotes, so a word with a `(` left open runs into the next one
// (`string equal $y(a $y(a<newline>) b) c` compares two other strings); and a
// backslash-newline splits a word. A word with a backslash, or a `(` its own
// text does not close, is not read. Counting every `(`, not only an index's,
// asks for at least the closes Tcl needs.
func wordAsTcl(text string) bool {
	if strings.IndexByte(text, '\\') >= 0 {
		return false
	}
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth == 0
}

// maxOperandName is the longest operand a message names.
const maxOperandName = 40

// operandName is how a message names an operand: as written, when it is short
// and has no control character, so the finding stays one short line; otherwise
// "a value".
func operandName(text string) string {
	if len(text) > maxOperandName {
		return "a value"
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] == 0x7f {
			return "a value"
		}
	}
	return text
}
