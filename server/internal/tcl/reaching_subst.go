package tcl

// Substitution order for the reaching analyzer. Tcl substitutes a command's words
// left to right before the command runs, and a [command substitution] is a script
// that runs in this frame at that point. So a use inside the bracket sees the
// bindings made before it in the same bracket, and the bracket's bindings reach the
// rest of the command and the code after it. A braced expression word (an if or loop
// condition, an expr argument) runs its brackets only when the expression is
// evaluated, and its bare $vars are not references (see CommandRefs).

// scriptFunc receives the interior of one [script]. conditional is true when the
// script may not run although its command does: it follows &&, || or ? in an
// expression, which evaluate their right side only on some paths.
type scriptFunc func(inner string, innerBase int, conditional bool)

// walkSubst calls onVar for each $var substituted in text and onScript for each
// [script] interior, in source order. base is the absolute offset of text's first
// byte. expr is true for the text of a braced expression: its $vars are not reported,
// and a script after &&, || or ? is conditional. The scan does not tell a quoted
// string from an operator, so a "?" in a string makes later scripts conditional too,
// which only widens an answer.
func walkSubst(text string, base int, expr bool, onVar func(VarRef), onScript scriptFunc) {
	conditional := false
	i := 0
	for i < len(text) {
		switch c := text[i]; {
		case c == '\\' && i+1 < len(text):
			i += 2
		case expr && (c == '?' || (c == '&' || c == '|') && i+1 < len(text) && text[i+1] == c):
			conditional = true
			i++
		case c == '$' && !expr:
			ref, next, ok := parseVarRef(text, i, base)
			if ok {
				onVar(ref)
			}
			i = next
		case c == '[':
			end := skipBracketSpan(text, i) // index just past the matching ']'
			innerEnd := end
			if end > i+1 && text[end-1] == ']' {
				innerEnd = end - 1
			}
			onScript(text[i+1:innerEnd], base+i+1, conditional)
			i = end
		default:
			i++
		}
	}
}

// walkWordSubst walks one word of c as Tcl substitutes it: a literal command name
// and a braced script or data word have nothing to substitute, and a braced
// expression word runs only its brackets.
func walkWordSubst(c Command, idx, base int, onVar func(VarRef), onScript scriptFunc) {
	w := c.Words[idx]
	if idx == 0 && isLiteralName(w) {
		return
	}
	if w.Kind == WordBraced {
		if !isExprWord(c, w) {
			return
		}
		inner, innerBase := bracedInner(w, base)
		walkSubst(inner, innerBase, true, onVar, onScript)
		return
	}
	walkSubst(w.Text, base+w.Start, false, onVar, onScript)
}

// isExprWord reports whether w is one of c's braced expression words.
func isExprWord(c Command, w Word) bool {
	for _, e := range exprBodies(c.Words) {
		if e.Start == w.Start {
			return true
		}
	}
	return false
}

// substitute threads in through the substitutions of c's words, left to right,
// recording the use at useOff when it is one of them. Words that deferred accepts
// are evaluated later by the caller (a loop test, an if condition). The
// variable-name arguments of info exists, unset and array are read last, by the
// command itself.
func (a *analyzer) substitute(c Command, base int, in reachSet, deferred func(Word) bool) reachSet {
	cur := in
	for idx, w := range c.Words {
		if deferred(w) {
			continue
		}
		cur = a.substituteWord(c, idx, base, cur)
	}
	for _, r := range nameArgRefs(c) {
		a.recordUseAt(r.Name, r.Start+base, r.End+base, cur)
	}
	return cur
}

// substituteWord threads in through the substitutions of word idx of c.
func (a *analyzer) substituteWord(c Command, idx, base int, in reachSet) reachSet {
	cur := in
	walkWordSubst(c, idx, base,
		func(r VarRef) { a.recordUseAt(r.Name, r.Start, r.End, cur) },
		func(inner string, innerBase int, conditional bool) {
			out, _ := a.seq(Parse(inner), innerBase, cur)
			if conditional {
				out = joinAll([]reachSet{cur, out})
			}
			cur = out
		})
	return cur
}

// wordIndex returns the index of w in c's words, or -1.
func wordIndex(c Command, w Word) int {
	for i, x := range c.Words {
		if x.Start == w.Start {
			return i
		}
	}
	return -1
}

// recordUseAt records the reaching set for name when the use at useOff lies in the
// reference [start, end).
func (a *analyzer) recordUseAt(name string, start, end int, in reachSet) {
	if a.useOff >= start && a.useOff < end {
		a.recordAnswer(name, in[name])
	}
}

// collectSubstBindings unions the bindings made in every [script] substituted in
// c's words, including expression words, which may be evaluated.
func (a *analyzer) collectSubstBindings(c Command, base int, into reachSet, follow func(Command) bool) {
	for idx := range c.Words {
		walkWordSubst(c, idx, base, func(VarRef) {}, func(inner string, innerBase int, _ bool) {
			a.collectBindings(Parse(inner), innerBase, into, follow)
		})
	}
}
