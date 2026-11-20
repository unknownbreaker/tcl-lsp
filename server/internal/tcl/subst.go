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
// flowDistrusted, which silences the uses after it. expr substitutes its unbraced
// words a second time, so one that may hold a bracket once substituted (a
// backslash, a $var or a [script]) reports flowResubst: its own uses were read
// on the first pass, and the uses after it stay silent. CommandRefs (refs) and the
// reaching analyzer (reaching_subst.go) both walk words through here, so they
// agree on what is substituted.

// exprFlow is how much of an expression may have been skipped by the point reached.
// Values are ordered; a later one implies the earlier ones. flowResubst comes only
// from a whole word, never from inside expression text: it silences the $vars
// after it, as flowAfterCondScript does, and the reaching analyzer distrusts the
// uses after its command (distrustWord).
type exprFlow uint8

const (
	flowAlways          exprFlow = iota // everything so far runs whenever the expression does
	flowConditional                     // a short-circuit operator came, so a later script may not run
	flowAfterCondScript                 // a script that may not have run came; later $vars stay silent
	flowResubst                         // an expr word was read, but expr may form and run a script from it
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
func walkSubst(text string, base int, expr bool, flow exprFlow, ends *scriptEnds, onVar func(VarRef), onScript scriptFunc) exprFlow {
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
			i = ends.skipBraceSpan(text, i)
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
			end, ok := ends.substEnd(text, i, expr)
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
	return m.trustedDepthSpan(text, i)
}

// walkWordSubst walks word idx of c as Tcl substitutes it, finding script ends
// through ends (scriptends.go). A literal command name
// and a braced script or data word have nothing to substitute. exprWords is
// exprWordStarts(c). flow and the result are as for walkSubst; flow carries only
// through expression text, but any unbraced word may return flowDistrusted, and
// an unbraced expr word that may form a script once expr substitutes it again
// returns flowResubst.
func walkWordSubst(c Command, exprWords map[int]bool, idx, base int, flow exprFlow, ends *scriptEnds, onVar func(VarRef), onScript scriptFunc) exprFlow {
	w := c.Words[idx]
	if idx == 0 && isLiteralName(w) {
		return flow
	}
	if w.Kind != WordBraced {
		if walkSubst(w.Text, base+w.Start, false, flowAlways, ends, onVar, onScript) == flowDistrusted {
			return flowDistrusted
		}
		if idx > 0 && (mayRunExpr(c) || mayBeCondition(c, idx)) && resubstMayFormScript(w.Text) {
			return flowResubst
		}
		return flow
	}
	if !exprWords[w.Start] {
		// An expanded braced word may spell a condition (`if {*}{{[set x 1]} {}}`),
		// which expr substitutes when it evaluates it.
		if idx > 1 && expands(c.Words, idx-1) && mayBeCondition(c, idx) && resubstMayFormScript(w.Text) {
			return flowResubst
		}
		return flow
	}
	if joinedExpr(c) {
		// The braced word means what the joined text means: `expr "{" {$x} "}"`
		// is a literal, and `expr {"} {[set x 1]} {"}` runs the script. So it
		// yields no use, and a script that may form in the join, and may bind,
		// distrusts what follows. A bracket in the word may form one; one from
		// another word (`expr "\[" {set x 1]}`, `expr $b {set x 1} $c`) is
		// distrusted at that word (resubstMayFormScript).
		if strings.ContainsAny(w.Text, "[]") {
			return flowDistrusted
		}
		// An expanded word is split as a list, which decodes its backslashes
		// (`expr {*}{\x5bset x 1\x5d}`), so it may spell a bracket too.
		if idx > 1 && expands(c.Words, idx-1) && strings.ContainsRune(w.Text, '\\') {
			return flowResubst
		}
		return flow
	}
	if t, ok := exprTreeOf(c, w, base, ends); ok {
		t.visit(onVar, onScript, flow >= flowConditional)
		return flow
	}
	inner, innerBase := bracedInner(w, base)
	return walkSubst(inner, innerBase, true, flow, ends, onVar, onScript)
}

// exprTreeOf parses the braced expression word w of c (see exprtree.go). It is
// false for an expression it cannot read, and for one of several expr words,
// which form one expression only once joined. Spans are found through ends.
func exprTreeOf(c Command, w Word, base int, ends *scriptEnds) (*exprTern, bool) {
	if joinedExpr(c) {
		return nil, false
	}
	inner, innerBase := bracedInner(w, base)
	return parseExprTree(inner, innerBase, ends)
}

// joinedExpr reports whether c is an expr of several argument words, which Tcl
// joins with spaces into one expression. Its braced words are expression text only
// once joined, with a meaning their neighbours decide, so none is read on its own.
func joinedExpr(c Command) bool {
	return isCmd(c.Words, "expr") && len(c.Words) != 2
}

// resubstMayFormScript reports whether an unbraced expr word may hold a bracket
// once substituted, which expr substitutes again and runs as a script: a
// backslash may escape or spell one (`\[`, `\x5b`), and a $var or [script] may
// supply one in its value. Nothing in the source says a value holds no bracket,
// so `expr $a + $b` counts too (#107).
func resubstMayFormScript(text string) bool {
	return strings.ContainsAny(text, "\\$[")
}

// mayRunExpr reports whether c is an expr, whose unbraced words are substituted
// twice: `expr` or `::expr`, also when expanded (`{*}expr`). A dynamic head
// (`{*}$cmd`) may run anything, so like any dynamic call it is out of reach.
func mayRunExpr(c Command) bool {
	w := c.Words
	if expands(w, 0) {
		w = w[1:]
	}
	return isCmd(w, "expr") || isCmd(w, "::expr")
}

// mayBeCondition reports whether word idx of c may be the condition of an if,
// elseif, while or for, which expr evaluates, substituting an unbraced one a
// second time (#110). The head may be qualified (`::if`) or expanded
// (`{*}while`). When an argument is expanded, no word's role is known, so every
// argument may be a condition, an expanded braced one too (walkWordSubst).
func mayBeCondition(c Command, idx int) bool {
	w, h := c.Words, 0
	if expands(w, 0) {
		h = 1
	}
	if idx <= h || h >= len(w) || w[h].Kind != WordBare {
		return false
	}
	name := strings.TrimPrefix(w[h].Text, "::")
	if name != "if" && name != "while" && name != "for" {
		return false
	}
	if hasExpansion(w[h:]) {
		return true
	}
	switch name {
	case "while":
		return idx == h+1
	case "for":
		return idx == h+2
	}
	// Tcl reads the keyword by its value, so `"elseif"` and `{elseif}` count, and
	// so does a word whose value is unknown.
	kw, ok := literalValue(w[idx-1])
	return idx == h+1 || !ok || kw == "elseif"
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
