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
	// MaybeCommands names the commands a proc or class defined in an
	// `itcl::body` with a fallback class key may create (fallbackNestedNames).
	MaybeCommands []string
}

// collectors holds the optional output sinks for a walkAll traversal. A nil sink
// (nil pointer / nil map) disables that collector.
type collectors struct {
	defs    *[]Definition             // proc/var/class/method/ivar definitions
	refs    *[]ContextRef             // contextual references
	ns      map[string]*NamespaceInfo // namespace declarations (non-nil to collect)
	classes map[string][]string       // class inherit edges (non-nil to collect)
	maybe   *[]string                 // commands a fallback body may define
	// ends serves the whole walk, so script ends found while parsing a body are
	// not found again for its references (scriptends.go). nil remembers nothing.
	ends *scriptEnds
}

// walkFile walks the whole of src with the collectors in c, giving the walk one
// memo of script ends (scriptends.go). It is the entry point of every File*.
func walkFile(src string, c collectors) {
	c.ends = &scriptEnds{}
	walkAll(parseWith(src, c.ends), 0, "::", FrameNamespace, 0, "", false, false, c)
}

// walkAll visits every command in cmds once, runs each enabled collector, and
// recurses into the command's script bodies via the shared childBodies so all
// collectors descend exactly the same bodies and cannot drift. base offsets a
// re-parsed braced body's defs/refs back to absolute source coordinates.
// wrapped says the commands are inside a custom command's body in this proc frame
// (see bodyScope.Wrapped); bodies that keep the frame inherit it, and so does the
// body of an unqualified proc a wrapper defines in a class-namespace frame. classNS says the
// commands are looked up from class's namespace (see bodyScope.InClassNS).
func walkAll(cmds []Command, base int, ns string, frame FrameKind, scope int, class string, wrapped, inClassNS bool, c collectors) {
	var refs []Reference
	for _, cmd := range cmds {
		w := cmd.Words
		bodies := childBodies(cmd, base, ns, frame, scope, class)

		if c.defs != nil {
			emitDefs(cmd, base, ns, frame, scope, class, wrapped, inClassNS, c.defs)
			// A proc's parameters are local definitions; emit them alongside the
			// proc itself, as the former recurseDefBodies did before recursing.
			if isCmd(w, "proc") && len(w) >= 4 && w[len(w)-1].Kind == WordBraced {
				_, bodyBase := bracedInner(w[len(w)-1], base)
				emitProcParams(w[2], base, ns, bodyBase, class, c.defs)
			} else if _, args, body, ok := decoratedProcDef(w); ok {
				_, bodyBase := bracedInner(body, base)
				emitProcParams(args, base, ns, bodyBase, class, c.defs)
			} else if args, ok := methodArgs(w, frame); ok && len(bodies) > 0 {
				// Parameters belong to the main body, which is listed first.
				emitProcParams(args, base, ns, bodies[0].Base, bodies[0].Class, c.defs)
			}
		}

		if c.maybe != nil {
			*c.maybe = append(*c.maybe, fallbackNestedNames(cmd, frame, class, wrapped, inClassNS)...)
		}

		if c.refs != nil {
			refs = refs[:0]
			commandRefs(cmd, base, c.ends, &refs)
			for _, r := range refs {
				*c.refs = append(*c.refs, ContextRef{Ref: r, Namespace: ns, Frame: frame, Scope: scope, Class: class, Wrapped: wrapped, InClassNS: inClassNS})
			}
		}

		if c.ns != nil {
			recordNSDecl(cmd, ns, c.ns)
		}

		if c.classes != nil && frame == FrameClass && class != "" {
			recordInherit(cmd, class, c.classes)
		}

		// An itcl::class body's class is the name its definition takes. When that
		// is not certain (a wrapper's body in a method), its body is wrapped and
		// keyed by unknownClassKey, so no name lookup or `inherit` reaches it.
		if isItclClassCmd(w) && len(bodies) > 0 {
			name, ok := nestedDefName(w[1].Text, ns, frame, class, wrapped, inClassNS)
			for i := range bodies {
				if bodies[i].Frame != FrameClass {
					continue
				}
				if ok {
					bodies[i].Class = name
				} else {
					bodies[i].Class = unknownClassKey(bodies[i].Class, bodies[i].Base)
					bodies[i].Wrapped = true
				}
			}
		}

		// An eval the analysis does not read joins its words into one script, so a
		// binding in one of its braced words may be argument text (`eval list {set x
		// 1}` runs `list set x 1`). Reaching never enters the word (#140), so the
		// index binds no local of this scope there either; a proc's own scope in the
		// word keeps its locals. Namespace variables keep today's wrapper ruling.
		joinedFrom := -1
		if c.defs != nil && isCmd(w, "eval") && unreadEval(cmd, frame) {
			joinedFrom = len(*c.defs)
		}
		for _, b := range bodies {
			// A body keeping the frame inherits wrapped. So does an unqualified proc
			// defined in a wrapper's body in a class-namespace frame: Tcl creates it
			// wherever the wrapper runs it, so its body's namespace is not known
			// either. A wrapped class body (only an itcl::class a wrapper defines in
			// a method) passes it to every member body: its namespace is unknown.
			inherit := wrapped && ((b.Frame == frame && b.Scope == scope) ||
				(inClassNS && b.Frame == FrameProc && b.InClassNS == classNSInherit) ||
				frame == FrameClass)
			bodyInClassNS := inClassNS
			if b.InClassNS != classNSInherit {
				bodyInClassNS = b.InClassNS == classNSOn
			}
			walkAll(parseWith(b.Inner, c.ends), b.Base, b.NS, b.Frame, b.Scope, b.Class, b.Wrapped || inherit, bodyInClassNS, c)
		}
		if joinedFrom >= 0 {
			*c.defs = append((*c.defs)[:joinedFrom], dropLocals((*c.defs)[joinedFrom:], scope)...)
		}
	}
}

// dropLocals returns defs without the local bindings (DefLocal, DefGlobalLink)
// made in scope.
func dropLocals(defs []Definition, scope int) []Definition {
	var kept []Definition
	for _, d := range defs {
		if (d.Kind == DefLocal || d.Kind == DefGlobalLink) && d.Scope == scope {
			continue
		}
		kept = append(kept, d)
	}
	return kept
}

// FileAll parses src once and returns all four per-file analyses together. It is
// the workspace index's entry point (see index.IndexFile); request-time callers
// that need only one analysis use the individual File* functions.
func FileAll(src string) FileIndex {
	fi := FileIndex{
		Namespaces: map[string]*NamespaceInfo{},
		Classes:    map[string][]string{},
	}
	walkFile(src, collectors{
		defs:    &fi.Defs,
		refs:    &fi.Refs,
		ns:      fi.Namespaces,
		classes: fi.Classes,
		maybe:   &fi.MaybeCommands,
	})
	fi.Defs = promoteLinkedAssignments(fi.Defs)
	return fi
}
