package tcl

import "strings"

// FrameKind is the kind of scope a reference appears in. Variable resolution
// differs by frame: inside a proc body a bare variable is local-only, at
// namespace-eval top level it is the namespace's own variable, and in the body of
// an unknown command nothing can be said about it.
type FrameKind int

const (
	FrameNamespace FrameKind = iota // namespace-eval top level (incl. global ::)
	FrameProc                       // inside a proc body
	FrameClass                      // an itcl::class body (member-declaration scope)
	FrameOpaque                     // an unknown command's body: the frame it runs in is not known
)

// ContextRef is a reference together with the namespace and frame at its site.
type ContextRef struct {
	Ref       Reference
	Namespace string // e.g. "::" or "::app"
	Frame     FrameKind
	Scope     int
	Class     string // fully-qualified itcl class name, or "" when not inside a class
}

// FileRefs parses src and returns every reference with its namespace and frame
// context, recursing into namespace eval and proc bodies.
func FileRefs(src string) []ContextRef {
	var out []ContextRef
	walkAll(Parse(src), 0, "::", FrameNamespace, 0, "", collectors{refs: &out})
	return out
}

// isCmd reports whether the command's literal head equals name.
// Note: a user command whose literal first word is "proc"/"namespace" would be
// treated as a scope-introducer (an accepted limitation of lightweight parsing).
func isCmd(words []Word, name string) bool {
	return len(words) > 0 && words[0].Kind == WordBare && words[0].Text == name
}

// qualifyNamespace resolves a namespace name argument against the current
// namespace: a leading "::" is absolute, otherwise it is relative to current.
func qualifyNamespace(name, current string) string {
	if strings.HasPrefix(name, "::") {
		return name
	}
	if current == "::" {
		return "::" + name
	}
	return current + "::" + name
}

// bracedInner returns the interior of a braced word and the absolute offset of
// that interior's first byte (base + word.Start + 1).
func bracedInner(w Word, base int) (string, int) {
	t := w.Text
	if len(t) >= 2 && t[0] == '{' && t[len(t)-1] == '}' {
		return t[1 : len(t)-1], base + w.Start + 1
	}
	// A tolerant scan can still emit a lone "{" for an unterminated brace (a file
	// mid-edit). Point Base just past it, keeping the invariant "Base is the first
	// interior byte" so offset math (Base-1 == the '{') holds for every caller.
	return "", base + w.Start + 1
}
