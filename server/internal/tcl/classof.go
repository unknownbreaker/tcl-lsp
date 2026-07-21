package tcl

import "strings"

// ClassOf returns the set of fully-qualified Itcl class names the variable used
// at receiverUseOff may hold at that point in the source, by inspecting its
// reaching definitions for a local `set v [ClassName ...]` instantiation.
//
// Returns nil when the receiver has no statically-traceable class (e.g. it is a
// proc parameter, assigned from an opaque expression, or not a variable in a proc
// or namespace scope).
func ClassOf(src string, receiverUseOff int) []string {
	defs, ok := ReachingAt(src, receiverUseOff)
	if !ok || len(defs) == 0 {
		return nil
	}

	// Get the enclosing scope (proc or namespace body) so we can walk its commands.
	inner, innerBase, _, _, _, _, found := enclosingProcOrScope(Parse(src), 0, "::", FrameNamespace, 0, receiverUseOff)
	if !found {
		return nil
	}
	cmds := Parse(inner)

	// Build a map from (NameStart, NameEnd) -> true for quick lookup.
	// We want to find the command that performed each reaching binding.
	type nameRange struct{ s, e int }
	wanted := make(map[nameRange]bool, len(defs))
	for _, d := range defs {
		wanted[nameRange{d.NameStart, d.NameEnd}] = true
	}

	var classes []string
	seen := map[string]bool{}

	// walkCmds recursively visits every command in a sequence (and all nested
	// child-body commands) to find `set NAME VALUE` commands whose name token
	// matches one of the wanted reaching-def ranges.  This mirrors the recursion
	// pattern used by collectBindings in reaching.go so that bindings inside
	// if/foreach/while bodies are discovered at any nesting depth.
	var walkCmds func(seq []Command, base int)
	walkCmds = func(seq []Command, base int) {
		for _, c := range seq {
			if isCmd(c.Words, "set") && len(c.Words) >= 3 {
				nameWord := c.Words[1]
				absStart := base + nameWord.Start
				absEnd := base + nameWord.End
				if wanted[nameRange{absStart, absEnd}] {
					cls := extractInstantiationClass(c.Words[2])
					if cls != "" && !seen[cls] {
						seen[cls] = true
						classes = append(classes, cls)
					}
				}
			}
			// Recurse into child script bodies (if/foreach/while/etc.).
			for _, b := range childBodies(c, base, "::", FrameProc, base, "") {
				walkCmds(Parse(b.Inner), b.Base)
			}
		}
	}
	walkCmds(cmds, innerBase)

	if len(classes) == 0 {
		return nil
	}
	return classes
}

// extractInstantiationClass returns the class name from a value word that looks
// like a command substitution instantiation: `[ClassName ...]` where ClassName
// is a qualified name (contains "::" or starts with "::"). Returns "" otherwise.
func extractInstantiationClass(w Word) string {
	// The value word for `set v [::Cls #auto]` is a WordBare whose text
	// starts with '[' (the bracket span).
	var text string
	switch w.Kind {
	case WordBare, WordQuoted:
		text = w.Text
	default:
		return ""
	}

	// Strip leading/trailing quotes for WordQuoted.
	if w.Kind == WordQuoted && len(text) >= 2 {
		text = text[1 : len(text)-1]
	}

	// Find the first '[' that opens a command substitution.
	i := 0
	for i < len(text) {
		if text[i] == '[' {
			break
		}
		if text[i] == '$' {
			// skip var ref, not a command substitution start
			_, next, _ := parseVarRef(text, i, 0)
			i = next
			continue
		}
		i++
	}
	if i >= len(text) {
		return ""
	}
	// text[i] == '['; extract interior up to the matching ']'.
	end := skipBracketSpan(text, i) // index just past ']'
	innerEnd := end
	if end > i+1 && text[end-1] == ']' {
		innerEnd = end - 1
	}
	inner := text[i+1 : innerEnd]

	// Parse the inner command and take its head word.
	innerCmds := Parse(inner)
	if len(innerCmds) == 0 {
		return ""
	}
	head := innerCmds[0].Words
	if len(head) == 0 {
		return ""
	}
	headWord := head[0]
	if headWord.Kind != WordBare {
		return ""
	}
	name := headWord.Text
	// Accept only qualified names (contain "::").
	if !strings.Contains(name, "::") {
		return ""
	}
	// Normalise: ensure leading "::".
	if !strings.HasPrefix(name, "::") {
		name = "::" + name
	}
	return name
}
