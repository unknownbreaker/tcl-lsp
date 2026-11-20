package tcl

// ObjMethodAt detects the "$var method" command shape at the given byte offset.
//
// It reports whether offset falls on the SECOND word of a command whose FIRST
// word is a lone variable substitution — i.e. the word consists solely of a
// single "$name" token with no surrounding text. The same shape nested inside
// a "[command substitution]" span is also detected, including one in a braced
// expression (`if {[$o m]} ...`, `expr {1+[$o m]}`).
//
// On success it returns:
//   - receiverOff: the byte offset of the variable NAME (the byte after '$' in
//     the first word, matching the convention used by ClassOf and the variable
//     reference scanner)
//   - methodName:  the literal text of the second word (the method being called)
//   - ok:          true
//
// Returns (0, "", false) when offset does not sit on the second word of such a
// command.
func ObjMethodAt(src string, offset int) (receiverOff int, methodName string, ok bool) {
	ends := &scriptEnds{}
	return objMethodInCmds(parseWith(src, ends), 0, "::", FrameNamespace, 0, "", offset, ends)
}

// objMethodInCmds walks a sequence of commands (with absolute base offset) and
// their nested script bodies and [command substitution] spans looking for the
// "$var method" shape at the given offset. The scope context (ns/frame/scope/
// class) is threaded so childBodies descends into method bodies (a method body
// is only entered when frame == FrameClass) — otherwise a `$obj method` call
// inside a method body, the dominant real-world form, is never detected.
func objMethodInCmds(cmds []Command, base int, ns string, frame FrameKind, scope int, class string, offset int, ends *scriptEnds) (int, string, bool) {
	for _, c := range cmds {
		// Check the shape at this command level.
		if ro, mn, found := objMethodAtCmd(c, base, offset); found {
			return ro, mn, true
		}
		// Recurse into braced script bodies (proc/method bodies, if/foreach/etc.).
		for _, b := range childBodies(c, base, ns, frame, scope, class) {
			if ro, mn, found := objMethodInCmds(parseWith(b.Inner, ends), b.Base, b.NS, b.Frame, b.Scope, b.Class, offset, ends); found {
				return ro, mn, true
			}
		}
		// Recurse into the [scripts] Tcl substitutes in each word, including those
		// in braced expression words (if/while/for conditions, expr arguments).
		if ro, mn, found := objMethodInSubsts(c, base, ns, frame, scope, class, offset, ends); found {
			return ro, mn, true
		}
	}
	return 0, "", false
}

// objMethodAtCmd checks whether offset falls on word[1] of command c (in which
// c has at least two words, word[0] is a lone "$var", and word[1] is a plain
// bareword). base is the absolute offset of the script c lives in.
func objMethodAtCmd(c Command, base int, offset int) (receiverOff int, methodName string, ok bool) {
	w := c.Words
	if len(w) < 2 {
		return 0, "", false
	}
	head := w[0]
	method := w[1]

	// The method word must contain the cursor.
	absMethodStart := base + method.Start
	absMethodEnd := base + method.End
	if offset < absMethodStart || offset >= absMethodEnd {
		return 0, "", false
	}

	// The method word must be a plain bareword (no $ or [ in it).
	if method.Kind != WordBare || !isPlainName(method) {
		return 0, "", false
	}

	// The head word must be a lone "$var" substitution: a WordBare whose text
	// is exactly "$name" — starts with '$' and the rest is a valid unqualified
	// or qualified variable name, with nothing else.
	if head.Kind != WordBare {
		return 0, "", false
	}
	headText := head.Text
	if len(headText) < 2 || headText[0] != '$' {
		return 0, "", false
	}
	// Parse the var ref starting at index 0 of headText. The ref must consume
	// the entire word text (no residual characters after the name).
	ref, next, parsed := parseVarRef(headText, 0, 0)
	if !parsed || next != len(headText) {
		return 0, "", false
	}
	_ = ref

	// Receiver offset: byte after '$' in the absolute source text.
	absReceiverOff := base + head.Start + 1 // +1 to skip '$'

	return absReceiverOff, method.Text, true
}

// objMethodInSubsts looks for the "$var method" shape at offset in the [scripts]
// that Tcl substitutes in c's words, walked as references and reaching walk them
// (walkWordSubst): a command word's script ends where Tcl ends it, and an
// expression's at the depth count. A script whose end is unknown or distrusted
// stops the walk of its word, so the text after it stays silent.
func objMethodInSubsts(c Command, base int, ns string, frame FrameKind, scope int, class string, offset int, ends *scriptEnds) (int, string, bool) {
	var ro int
	var mn string
	found := false
	exprWords := exprWordStarts(c)
	for idx := range c.Words {
		walkWordSubst(c, exprWords, idx, base, flowAlways, ends, func(VarRef) {}, func(inner string, innerBase int, _ bool) {
			if !found && offset >= innerBase && offset <= innerBase+len(inner) {
				ro, mn, found = objMethodInCmds(parseWith(inner, ends), innerBase, ns, frame, scope, class, offset, ends)
			}
		})
		if found {
			return ro, mn, true
		}
	}
	return 0, "", false
}
