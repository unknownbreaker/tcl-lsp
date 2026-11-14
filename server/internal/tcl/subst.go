package tcl

import "strings"

// Substitution order. Tcl substitutes a command's words left to right before the
// command runs: each $var is read and each [script] runs, in source order. A braced
// word is not substituted, except an expression word (an if or loop condition, an
// expr argument), whose $vars and [scripts] are substituted by expr itself when the
// expression is evaluated. Inside an expression a {braced} operand is a literal, a
// "quoted" one is substituted, and a script after &&, || or ? runs only on some
// paths. A $var after such a script is not reported: which bindings reach it
// depends on which operands ran (`$c ? [set x 1] : $x` reads the value from before
// the script), so it stays silent. CommandRefs (refs) and the reaching analyzer
// (reaching_subst.go) both walk words through here, so they agree on what is
// substituted.

// exprFlow is how much of an expression may have been skipped by the point reached.
// Values are ordered; a later one implies the earlier ones.
type exprFlow uint8

const (
	flowAlways          exprFlow = iota // everything so far runs whenever the expression does
	flowConditional                     // a short-circuit operator came, so a later script may not run
	flowAfterCondScript                 // a script that may not have run came; later $vars stay silent
)

// scriptFunc receives the interior of one [script] at absolute offset innerBase.
// conditional is true when the script may not run although its command does.
type scriptFunc func(inner string, innerBase int, conditional bool)

// walkSubst calls onVar for each $var substituted in text and onScript for each
// [script] interior, in source order. base is the absolute offset of text's first
// byte. expr is true for the text of an expression. flow is how the same
// expression ran before text; the result is how it has run by text's end.
func walkSubst(text string, base int, expr bool, flow exprFlow, onVar func(VarRef), onScript scriptFunc) exprFlow {
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
			end := skipBracketSpan(text, i) // index just past the matching ']'
			innerEnd := end
			// Strip the closing ']' if present; on unterminated input
			// skipBracketSpan returns len(text) and there is no ']' to remove.
			if end > i+1 && text[end-1] == ']' {
				innerEnd = end - 1
			}
			onScript(text[i+1:innerEnd], base+i+1, flow >= flowConditional)
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

// walkWordSubst walks word idx of c as Tcl substitutes it. A literal command name
// and a braced script or data word have nothing to substitute. exprWords is
// exprWordStarts(c). flow and the result are as for walkSubst; they carry only
// through expression text.
func walkWordSubst(c Command, exprWords map[int]bool, idx, base int, flow exprFlow, onVar func(VarRef), onScript scriptFunc) exprFlow {
	w := c.Words[idx]
	if idx == 0 && isLiteralName(w) {
		return flow
	}
	if w.Kind != WordBraced {
		walkSubst(w.Text, base+w.Start, false, flowAlways, onVar, onScript)
		return flow
	}
	if !exprWords[w.Start] {
		return flow
	}
	inner, innerBase := bracedInner(w, base)
	return walkSubst(inner, innerBase, true, flow, onVar, onScript)
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
