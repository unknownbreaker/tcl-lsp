package tcl

// FoldRange is a foldable braced script body, as a byte range in the source it
// was parsed from: Open is the offset of the opening '{', Close the offset of the
// matching '}'.
type FoldRange struct {
	Open  int
	Close int
}

// FileFolds parses src and returns a FoldRange for every braced script body it
// contains — proc/method/constructor/destructor bodies, namespace eval and
// itcl::class blocks, external itcl::body, and control-flow bodies (if/for/
// foreach/while/catch/try/switch and custom-command script bodies) — nested at any
// depth. It reuses the shared childBodies classifier, so it folds exactly what
// the def/ref walkers descend (script bodies, not parameter lists, expressions,
// or data braces), plus a switch's arm list, which holds bodies without being one.
// Single-line bodies are included; callers that render folds drop ranges that do
// not span at least two lines.
func FileFolds(src string) []FoldRange {
	var out []FoldRange
	var walk func(cmds []Command, base int, ns string, frame FrameKind, scope int, class string)
	walk = func(cmds []Command, base int, ns string, frame FrameKind, scope int, class string) {
		for _, c := range cmds {
			if list, ok := switchArmList(c.Words); ok {
				inner, innerBase := bracedInner(list, base)
				out = append(out, FoldRange{Open: innerBase - 1, Close: innerBase + len(inner)})
			}
			// Thread the real scope context so method bodies (which childBodies only
			// yields at FrameClass) are folded too.
			for _, b := range childBodies(c, base, ns, frame, scope, class) {
				out = append(out, FoldRange{Open: b.Base - 1, Close: b.Base + len(b.Inner)})
				walk(Parse(b.Inner), b.Base, b.NS, b.Frame, b.Scope, b.Class)
			}
		}
	}
	walk(Parse(src), 0, "::", FrameNamespace, 0, "")
	return out
}
