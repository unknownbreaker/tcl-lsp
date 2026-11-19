package tcl

import "strings"

// Substitution order. Tcl substitutes a command's words left to right before the
// command runs: each $var is read and each [script] runs, in source order. A braced
// word is not substituted, except an expression word (an if or loop condition, an
// expr argument), whose $vars and [scripts] are substituted by expr itself when the
// expression is evaluated. Inside an expression a {braced} operand is a literal, a
// "quoted" one is substituted, and a script after &&, || or ? runs only on some
// paths. A single braced expression is parsed into that structure
// (exprtree.go), so every $var in it is reported. The flat scan here is the
// fallback, for several expr words or text the parse cannot read: there a $var
// after such a script is not reported, because which bindings reach it depends on
// which operands ran (`$c ? [set x 1] : $x` reads the value from before the
// script), so it stays silent. A command word's [script] ends where Tcl ends it
// (scriptclose.go), as the scanner first tries. An expression's ends at the depth
// count, and the flat scan stops at one whose `]` may sit in its quotes, braces or
// an array index (bracesHideBrackets). Either scan stops at a script whose end
// is unknown, since the rest of the text cannot be read, and reports
// flowDistrusted, which silences the uses after it. CommandRefs (refs) and the
// reaching analyzer (reaching_subst.go) both walk words through here, so they
// agree on what is substituted.

// exprFlow is how much of an expression may have been skipped by the point reached.
// Values are ordered; a later one implies the earlier ones.
type exprFlow uint8

const (
	flowAlways          exprFlow = iota // everything so far runs whenever the expression does
	flowConditional                     // a short-circuit operator came, so a later script may not run
	flowAfterCondScript                 // a script that may not have run came; later $vars stay silent
	flowDistrusted                      // a script whose end is unknown came; the scan stopped there
)

// scriptFunc receives the interior of one [script] at absolute offset innerBase.
// conditional is true when the script may not run although its command does.
type scriptFunc func(inner string, innerBase int, conditional bool)

// walkSubst calls onVar for each $var substituted in text and onScript for each
// [script] interior, in source order. base is the absolute offset of text's first
// byte. expr is true for the text of an expression. flow is how the same
// expression ran before text; the result is how it has run by text's end, or
// flowDistrusted when the scan stopped at a script it cannot read.
func walkSubst(text string, base int, expr bool, flow exprFlow, m *scriptEnds, onVar func(VarRef), onScript scriptFunc) exprFlow {
	inQuote := false
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			i += 2
		case expr && c == '"':
			inQuote = !inQuote
			i++
		case expr && !inQuote && c == '{':
			i = skipBraceSpan(text, i)
		case expr && !inQuote && (c == '?' || (c == '&' || c == '|') && i+1 < len(text) && text[i+1] == c):
			flow = max(flow, flowConditional)
			i++
		case c == '$':
			ref, next, ok := parseVarRef(text, i, base)
			if ok && flow < flowAfterCondScript {
				onVar(ref)
			}
			i = next
		case c == '[':
			end, ok := m.substEnd(text, i, expr)
			if !ok {
				return flowDistrusted
			}
			onScript(text[i+1:end-1], base+i+1, flow >= flowConditional)
			if flow == flowConditional {
				flow = flowAfterCondScript
			}
			i = end
		default:
			i++
		}
	}
	return flow
}

// skipBraceSpan returns the index just past a balanced {..} span starting at i.
// Backslash-aware; tolerant of unterminated input (returns len(text)).
func skipBraceSpan(text string, i int) int {
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

// exprWordStarts returns the start offsets of c's braced expression words.
func exprWordStarts(c Command) map[int]bool {
	starts := map[int]bool{}
	for _, e := range exprBodies(c.Words) {
		starts[e.Start] = true
	}
	return starts
}

// substEnd returns the offset just past the `]` that closes the [script] at i.
// A command word's script ends where Tcl ends it (scriptClose). An expression's
// keeps the depth count, distrusted when its braces or quotes may hide the `]`.
// ok is false when the end is unknown or distrusted.
func (m *scriptEnds) substEnd(text string, i int, expr bool) (int, bool) {
	if !expr {
		end, ok := m.scriptClose(text, i+1)
		return end + 1, ok
	}
	return trustedDepthSpan(text, i)
}

// walkWordSubst walks word idx of c as Tcl substitutes it, finding script ends
// through m (scriptends.go). A literal command name
// and a braced script or data word have nothing to substitute. exprWords is
// exprWordStarts(c). flow and the result are as for walkSubst; flow carries only
// through expression text, but any unbraced word may return flowDistrusted.
func walkWordSubst(c Command, exprWords map[int]bool, idx, base int, flow exprFlow, m *scriptEnds, onVar func(VarRef), onScript scriptFunc) exprFlow {
	w := c.Words[idx]
	if idx == 0 && isLiteralName(w) {
		return flow
	}
	if w.Kind != WordBraced {
		if walkSubst(w.Text, base+w.Start, false, flowAlways, m, onVar, onScript) == flowDistrusted {
			return flowDistrusted
		}
		return flow
	}
	if !exprWords[w.Start] {
		return flow
	}
	if t, ok := exprTreeOf(c, w, base); ok {
		t.visit(onVar, onScript, flow >= flowConditional)
		return flow
	}
	inner, innerBase := bracedInner(w, base)
	return walkSubst(inner, innerBase, true, flow, m, onVar, onScript)
}

// exprTreeOf parses the braced expression word w of c (see exprtree.go). It is
// false for an expression it cannot read, and for one of several expr words,
// which form one expression only once joined.
func exprTreeOf(c Command, w Word, base int) (*exprTern, bool) {
	if isCmd(c.Words, "expr") && len(c.Words) != 2 {
		return nil, false
	}
	inner, innerBase := bracedInner(w, base)
	return parseExprTree(inner, innerBase)
}

// carryFlow returns the flow that word w of c passes to c's next word. The words of
// a multi-word expr form one expression, so an operator in an unbraced word makes a
// later word's scripts conditional, and so may a substitution, whose value is
// unknown. Every other command's words are separate.
func carryFlow(c Command, w Word, flow exprFlow) exprFlow {
	if !isCmd(c.Words, "expr") {
		return flowAlways
	}
	if w.Kind != WordBraced && strings.ContainsAny(w.Text, "&|?$[") {
		return max(flow, flowConditional)
	}
	return flow
}
