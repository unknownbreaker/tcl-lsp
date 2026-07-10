package tcl

// This file is the single traversal that every per-file analysis shares. The
// four File* entry points (FileDefs, FileRefs, FileNamespaces, FileClasses) used
// to each Parse the source and walk it independently -- four full token-tree
// walks per file (and four per nested braced body). They all descend the *same*
// bodies via childBodies (bodies.go) and differ only in what they collect per
// command, so they are unified here: one parse, one walk, collectors toggled.
// FileAll runs all four at once for the workspace index; the individual File*
// functions enable a single collector for request-time callers.

// FileIndex bundles every per-file analysis the workspace index needs, produced
// by a single parse + traversal. Each field equals the result of the matching
// File* function.
type FileIndex struct {
	Defs       []Definition
	Refs       []ContextRef
	Namespaces map[string]*NamespaceInfo
	Classes    map[string][]string
}

// collectors holds the optional output sinks for a walkAll traversal. A nil sink
// (nil pointer / nil map) disables that collector.
type collectors struct {
	defs    *[]Definition             // proc/var/class/method/ivar definitions
	refs    *[]ContextRef             // contextual references
	ns      map[string]*NamespaceInfo // namespace declarations (non-nil to collect)
	classes map[string][]string       // class inherit edges (non-nil to collect)
}

// walkAll visits every command in cmds once, runs each enabled collector, and
// recurses into the command's script bodies via the shared childBodies so all
// collectors descend exactly the same bodies and cannot drift. base offsets a
// re-parsed braced body's defs/refs back to absolute source coordinates.
func walkAll(cmds []Command, base int, ns string, frame FrameKind, scope int, class string, c collectors) {
	for _, cmd := range cmds {
		w := cmd.Words

		if c.defs != nil {
			emitDefs(cmd, base, ns, frame, scope, class, c.defs)
			// A proc's parameters are local definitions; emit them alongside the
			// proc itself, as the former recurseDefBodies did before recursing.
			if isCmd(w, "proc") && len(w) >= 4 && w[len(w)-1].Kind == WordBraced {
				_, bodyBase := bracedInner(w[len(w)-1], base)
				emitProcParams(w[2], base, ns, bodyBase, class, c.defs)
			} else if _, args, body, ok := decoratedProcDef(w); ok {
				_, bodyBase := bracedInner(body, base)
				emitProcParams(args, base, ns, bodyBase, class, c.defs)
			}
		}

		if c.refs != nil {
			for _, r := range CommandRefs(cmd) {
				r.Start += base
				r.End += base
				*c.refs = append(*c.refs, ContextRef{Ref: r, Namespace: ns, Frame: frame, Scope: scope, Class: class})
			}
		}

		if c.ns != nil {
			recordNSDecl(cmd, ns, c.ns)
		}

		if c.classes != nil && frame == FrameClass && class != "" {
			recordInherit(cmd, ns, class, c.classes)
		}

		for _, b := range childBodies(cmd, base, ns, frame, scope, class) {
			walkAll(Parse(b.Inner), b.Base, b.NS, b.Frame, b.Scope, b.Class, c)
		}
	}
}

// FileAll parses src once and returns all four per-file analyses together. It is
// the workspace index's entry point (see index.IndexFile); request-time callers
// that need only one analysis use the individual File* functions.
func FileAll(src string) FileIndex {
	fi := FileIndex{
		Namespaces: map[string]*NamespaceInfo{},
		Classes:    map[string][]string{},
	}
	walkAll(Parse(src), 0, "::", FrameNamespace, 0, "", collectors{
		defs:    &fi.Defs,
		refs:    &fi.Refs,
		ns:      fi.Namespaces,
		classes: fi.Classes,
	})
	fi.Defs = promoteLinkedAssignments(fi.Defs)
	return fi
}
