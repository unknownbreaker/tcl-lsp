package lsp

import (
	"path/filepath"
	"strconv"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Call hierarchy reuses the existing resolution: a callable is anchored by its
// definition (prepare), incoming calls are find-references grouped by the
// enclosing caller, and outgoing calls are the command-position references inside
// the callable's own body resolved to their definitions. The one new primitive is
// enclosingDef (offset -> the proc/method whose body contains it), derived from
// each Definition's FullStart/FullEnd span.

// anchor identifies a call-hierarchy target: a proc or method definition site.
type anchor struct {
	file      string
	name      string // fully-qualified
	kind      tcl.DefKind
	nameStart int
	nameEnd   int
}

// isCallable reports whether a definition kind participates in the call hierarchy.
func isCallable(k tcl.DefKind) bool {
	return k == tcl.DefProc || k == tcl.DefMethod
}

// resolveAnchor finds the call-hierarchy anchor at offset: the callable whose
// name token is at offset (cursor on a declaration), else the callable the call
// site at offset resolves to (cursor on an invocation).
func (s *Server) resolveAnchor(file, src string, offset int) (anchor, bool) {
	for _, d := range source.Defs(file, src) {
		if isCallable(d.Kind) && offset >= d.NameStart && offset < d.NameEnd {
			return anchor{file, d.Name, d.Kind, d.NameStart, d.NameEnd}, true
		}
	}
	for _, l := range s.res.Definition(file, src, offset) {
		if isCallable(l.Kind) {
			return anchor{l.File, l.Name, l.Kind, l.NameStart, l.NameEnd}, true
		}
	}
	return anchor{}, false
}

// anchorFromItem re-derives the anchor from an item the client sends back; the
// item's SelectionRange start is its definition's name token.
func (s *Server) anchorFromItem(item CallHierarchyItem) (anchor, bool) {
	file := uriToPath(item.URI)
	src := s.sourceOf(file)
	off := ByteOffset(src, item.SelectionRange.Start.Line, item.SelectionRange.Start.Character)
	return s.resolveAnchor(file, src, off)
}

// enclosingDef returns the innermost proc/method definition whose full extent
// (FullStart..FullEnd) contains offset. Innermost = the latest-starting container,
// so a proc/method nested inside another wins over its parent.
func enclosingDef(defs []tcl.Definition, offset int) (tcl.Definition, bool) {
	var best tcl.Definition
	found := false
	for _, d := range defs {
		if !isCallable(d.Kind) || d.FullStart > offset || offset >= d.FullEnd {
			continue
		}
		if !found || d.FullStart > best.FullStart {
			best, found = d, true
		}
	}
	return best, found
}

// callItem builds a CallHierarchyItem for a callable definition. lines indexes the
// definition file's source (for offset -> position conversion).
func callItem(file, name string, kind tcl.DefKind, nameStart, nameEnd int, lines *lineIndex) CallHierarchyItem {
	sk, _ := symbolKind(kind)
	r := Range{Start: lines.position(nameStart), End: lines.position(nameEnd)}
	return CallHierarchyItem{
		Name:           shortName(name),
		Detail:         name,
		Kind:           sk,
		URI:            pathToURI(file),
		Range:          r,
		SelectionRange: r,
	}
}

// fileItem represents a top-level / page-level call site that is not inside any
// proc or method (common in .rvt pages, whose code runs at the ::request frame).
func fileItem(file string) CallHierarchyItem {
	zero := Range{}
	return CallHierarchyItem{
		Name:           filepath.Base(file),
		Kind:           SymKindFile,
		URI:            pathToURI(file),
		Range:          zero,
		SelectionRange: zero,
	}
}

func (s *Server) prepareCallHierarchy(p CallHierarchyPrepareParams) []CallHierarchyItem {
	file := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(file)
	off := ByteOffset(src, p.Position.Line, p.Position.Character)
	a, ok := s.resolveAnchor(file, src, off)
	if !ok {
		return nil
	}
	return []CallHierarchyItem{callItem(a.file, a.name, a.kind, a.nameStart, a.nameEnd, newLineIndex(s.sourceOf(a.file)))}
}

func (s *Server) incomingCalls(p CallHierarchyIncomingCallsParams) []CallHierarchyIncomingCall {
	a, ok := s.anchorFromItem(p.Item)
	if !ok {
		return nil
	}
	// Call sites reaching the anchor: find-references from its definition. A
	// method's are the bare intra-class calls that resolve to it through the
	// calling class's inheritance chain; `$obj method` calls are not covered.
	sites := s.res.References(a.file, s.sourceOf(a.file), a.nameStart)

	type group struct {
		from   CallHierarchyItem
		ranges []Range
	}
	groups := map[string]*group{}
	var order []string
	indexes := lineIndexes{}

	for _, l := range sites {
		csrc := s.sourceOf(l.File)
		lines := indexes.forFile(l.File, s.sourceOf)
		var from CallHierarchyItem
		var key string
		if d, found := enclosingDef(source.Defs(l.File, csrc), l.NameStart); found {
			from = callItem(l.File, d.Name, d.Kind, d.NameStart, d.NameEnd, lines)
			key = l.File + "#" + strconv.Itoa(d.NameStart)
		} else {
			from = fileItem(l.File)
			key = "file:" + l.File
		}
		g := groups[key]
		if g == nil {
			g = &group{from: from}
			groups[key] = g
			order = append(order, key)
		}
		g.ranges = append(g.ranges, Range{Start: lines.position(l.NameStart), End: lines.position(l.NameEnd)})
	}

	out := make([]CallHierarchyIncomingCall, 0, len(order))
	for _, k := range order {
		g := groups[k]
		out = append(out, CallHierarchyIncomingCall{From: g.from, FromRanges: g.ranges})
	}
	return out
}

func (s *Server) outgoingCalls(p CallHierarchyOutgoingCallsParams) []CallHierarchyOutgoingCall {
	a, ok := s.anchorFromItem(p.Item)
	if !ok {
		return nil
	}
	src := s.sourceOf(a.file)
	defs := source.Defs(a.file, src)

	// Find the anchor's body span among the file's definitions.
	var span tcl.Definition
	for _, d := range defs {
		if d.Kind == a.kind && d.NameStart == a.nameStart && d.Name == a.name {
			span = d
			break
		}
	}
	if span.FullEnd == 0 {
		return nil
	}

	type group struct {
		to     CallHierarchyItem
		ranges []Range
	}
	groups := map[string]*group{}
	var order []string
	indexes := lineIndexes{}
	anchorLines := indexes.forFile(a.file, s.sourceOf)

	for _, r := range source.Refs(a.file, src) {
		if r.Ref.Kind != tcl.RefCommand || r.Ref.Start < span.FullStart || r.Ref.Start >= span.FullEnd {
			continue
		}
		// Attribute the call to its INNERMOST enclosing def and keep it only when
		// that is the anchor itself — calls inside a proc/method nested in the
		// anchor's body belong to the nested callable, not the anchor (this keeps
		// outgoing symmetric with incoming's enclosingDef grouping).
		if d, found := enclosingDef(defs, r.Ref.Start); !found || d.NameStart != a.nameStart || d.Kind != a.kind {
			continue
		}
		for _, l := range s.res.Definition(a.file, src, r.Ref.Start) {
			if !isCallable(l.Kind) {
				continue
			}
			key := l.File + "#" + strconv.Itoa(l.NameStart)
			g := groups[key]
			if g == nil {
				g = &group{to: callItem(l.File, l.Name, l.Kind, l.NameStart, l.NameEnd, indexes.forFile(l.File, s.sourceOf))}
				groups[key] = g
				order = append(order, key)
			}
			g.ranges = append(g.ranges, Range{Start: anchorLines.position(r.Ref.Start), End: anchorLines.position(r.Ref.End)})
		}
	}

	out := make([]CallHierarchyOutgoingCall, 0, len(order))
	for _, k := range order {
		g := groups[k]
		out = append(out, CallHierarchyOutgoingCall{To: g.to, FromRanges: g.ranges})
	}
	return out
}
