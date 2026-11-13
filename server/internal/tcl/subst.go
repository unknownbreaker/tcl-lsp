package tcl

// Substitution order. Tcl substitutes a command's words left to right before the
// command runs: each $var is read and each [script] runs, in source order. A braced
// word is not substituted, except an expression word (an if or loop condition, an
// expr argument), whose [scripts] run when the expression is evaluated. Inside an
// expression, bare $vars are not reported as references, a {braced} operand is a
// literal, and a script after &&, || or ? runs only on some paths. CommandRefs (refs)
// and the reaching analyzer (reaching_subst.go) both walk words through here, so
// they agree on what is substituted.

// scriptFunc receives the interior of one [script] at absolute offset innerBase.
// conditional is true when the script may not run although its command does.
type scriptFunc func(inner string, innerBase int, conditional bool)

// walkSubst calls onVar for each $var substituted in text and onScript for each
// [script] interior, in source order. base is the absolute offset of text's first
// byte. expr is true for the text of an expression. conditional is whether a
// short-circuit operator came before text in the same expression; the result is
// whether one has come by its end.
func walkSubst(text string, base int, expr, conditional bool, onVar func(VarRef), onScript scriptFunc) bool {
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
			// Strip the closing ']' if present; on unterminated input
			// skipBracketSpan returns len(text) and there is no ']' to remove.
			if end > i+1 && text[end-1] == ']' {
				innerEnd = end - 1
			}
			onScript(text[i+1:innerEnd], base+i+1, conditional)
			i = end
		default:
			i++
		}
	}
	return conditional
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
// exprWordStarts(c). conditional and the result are as for walkSubst; they carry
// only through expression text.
func walkWordSubst(c Command, exprWords map[int]bool, idx, base int, conditional bool, onVar func(VarRef), onScript scriptFunc) bool {
	w := c.Words[idx]
	if idx == 0 && isLiteralName(w) {
		return conditional
	}
	if w.Kind != WordBraced {
		walkSubst(w.Text, base+w.Start, false, false, onVar, onScript)
		return conditional
	}
	if !exprWords[w.Start] {
		return conditional
	}
	inner, innerBase := bracedInner(w, base)
	return walkSubst(inner, innerBase, true, conditional, onVar, onScript)
}
