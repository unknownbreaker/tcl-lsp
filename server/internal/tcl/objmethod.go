package tcl

// ObjMethodAt detects the "$var method" command shape at the given byte offset.
//
// It reports whether offset falls on the SECOND word of a command whose FIRST
// word is a lone variable substitution — i.e. the word consists solely of a
// single "$name" token with no surrounding text. The same shape nested inside
// a "[command substitution]" span is also detected.
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
		// Recurse into [command substitution] spans within each word.
		for _, w := range c.Words {
			if ro, mn, found := objMethodInSubsts(w, base, ns, frame, scope, class, offset, ends); found {
				return ro, mn, true
			}
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

// objMethodInSubsts recurses into [command substitution] spans within word w,
// looking for the "$var method" shape at offset. Returns on first match.
func objMethodInSubsts(w Word, cmdBase int, ns string, frame FrameKind, scope int, class string, offset int, ends *scriptEnds) (int, string, bool) {
	if w.Kind == WordBraced {
		return 0, "", false // braces suppress substitution
	}
	text := w.Text
	// w.Start is relative to the script fragment this word was parsed from;
	// cmdBase is that fragment's absolute offset. Both are needed so the inner
	// command's offsets land in absolute source coordinates (the bug that hid
	// `[$obj method]` calls inside method bodies, where cmdBase != 0).
	base := cmdBase + w.Start
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			i += 2
		case c == '[':
			end, ok := ends.substEnd(text, i, false) // where Tcl ends the script
			if !ok {
				return 0, "", false // the rest of the word cannot be read
			}
			inner := text[i+1 : end-1]
			innerBase := base + i + 1
			if ro, mn, found := objMethodInCmds(parseWith(inner, ends), innerBase, ns, frame, scope, class, offset, ends); found {
				return ro, mn, true
			}
			i = end
		default:
			i++
		}
	}
	return 0, "", false
}
