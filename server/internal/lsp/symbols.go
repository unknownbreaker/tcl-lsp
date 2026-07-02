package lsp

import (
	"runtime"
	"sort"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/par"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// symbolKind maps a tcl.DefKind to an LSP SymbolKind.
// Returns (kind, true) for symbol-worthy definitions, (0, false) for locals and links.
func symbolKind(k tcl.DefKind) (SymbolKind, bool) {
	switch k {
	case tcl.DefProc:
		return SymKindFunction, true
	case tcl.DefMethod:
		return SymKindMethod, true
	case tcl.DefIvar:
		return SymKindField, true
	case tcl.DefClass:
		return SymKindClass, true
	case tcl.DefNamespaceVar:
		return SymKindVariable, true
	default:
		return 0, false
	}
}

// leafSymbol builds a DocumentSymbol from a single definition, computing the
// name SelectionRange and the full-extent Range (with containment fallback).
func leafSymbol(d tcl.Definition, src string) DocumentSymbol {
	selStart := offsetToPosition(src, d.NameStart)
	selEnd := offsetToPosition(src, d.NameEnd)
	selRange := Range{Start: selStart, End: selEnd}

	var fullRange Range
	if d.FullStart == 0 && d.FullEnd == 0 {
		fullRange = selRange
	} else {
		fullStart := offsetToPosition(src, d.FullStart)
		fullEnd := offsetToPosition(src, d.FullEnd)
		if posContains(fullStart, fullEnd, selStart, selEnd) {
			fullRange = Range{Start: fullStart, End: fullEnd}
		} else {
			fullRange = selRange
		}
	}

	kind, _ := symbolKind(d.Kind)
	return DocumentSymbol{
		Name:           d.Name,
		Kind:           kind,
		Range:          fullRange,
		SelectionRange: selRange,
	}
}

// buildDocumentSymbols converts a slice of tcl.Definition values into a
// hierarchical DocumentSymbol tree:
//   - DefClass nodes carry their DefMethod/DefIvar defs as Children.
//   - Procs, namespace vars, and class nodes are grouped under synthesized
//     Namespace nodes (one per distinct non-global namespace), nested by path.
//   - Global (::) symbols and top-level namespace nodes sit at the root.
//
// When hoistRequest is true, a root-level ::request namespace node is elided
// and its children are promoted to the document root (used for .rvt pages).
func buildDocumentSymbols(defs []tcl.Definition, src string, hoistRequest bool) []DocumentSymbol {

	// --- Step 1: build class nodes (with method/ivar children) ---

	// classSyms maps FQ class name -> DocumentSymbol (kind Class) with children,
	// built up-front so Step 2 can splice each class into the source-ordered
	// member stream by name.
	classSyms := map[string]*DocumentSymbol{}

	for _, d := range defs {
		if d.Kind != tcl.DefClass {
			continue
		}
		sym := leafSymbol(d, src)
		classSyms[d.Name] = &sym
	}

	// Attach method and ivar children to their class nodes.
	for _, d := range defs {
		if d.Kind != tcl.DefMethod && d.Kind != tcl.DefIvar {
			continue
		}
		parent, ok := classSyms[d.Class]
		if !ok {
			continue
		}
		child := leafSymbol(d, src)
		parent.Children = append(parent.Children, child)
	}

	// --- Step 2: group top-level symbols by namespace, IN SOURCE ORDER ---
	//
	// defs is in ascending source position (the shared walk visits commands in
	// order), so one ordered pass makes each namespace's members appear in the
	// order they occur in the file -- classes, procs, and vars interleaved by
	// position, instead of all classes hoisted ahead of the procs.

	// nsChildren maps namespace FQ -> slice of DocumentSymbol children.
	nsChildren := map[string][]DocumentSymbol{}
	var nsOrder []string // tracks first-seen order of distinct namespaces

	addToNS := func(ns string, sym DocumentSymbol) {
		if _, exists := nsChildren[ns]; !exists {
			nsOrder = append(nsOrder, ns)
		}
		nsChildren[ns] = append(nsChildren[ns], sym)
	}

	emittedClass := map[string]bool{} // a class redefined in-file must appear once
	for _, d := range defs {
		switch d.Kind {
		case tcl.DefClass:
			if emittedClass[d.Name] {
				continue
			}
			emittedClass[d.Name] = true
			sym := *classSyms[d.Name]
			if d.Namespace != "::" {
				sym.Name = shortName(sym.Name) // the namespace node supplies qualification
			}
			addToNS(d.Namespace, sym)
		case tcl.DefProc, tcl.DefNamespaceVar:
			if d.Class != "" {
				continue // inside a class body (itcl inner proc); not a top-level symbol
			}
			kind, ok := symbolKind(d.Kind)
			if !ok {
				continue
			}
			sym := leafSymbol(d, src)
			sym.Kind = kind
			// Members display their simple name; the containing namespace node (or
			// the document root) supplies the qualification.
			sym.Name = shortName(sym.Name)
			addToNS(d.Namespace, sym)
		}
	}

	// --- Step 3: build namespace tree (nested by path) ---

	// nsNode maps namespace FQ -> *DocumentSymbol (kind Namespace).
	nsNode := map[string]*DocumentSymbol{}
	var rootNSOrder []string // top-level namespace nodes (parent == "::")
	var allNSOrder []string  // every namespace node ever created, including synthesized intermediates

	// Build a namespace node for a given FQ namespace, creating intermediate
	// ancestors as needed. Returns the node.
	var ensureNSNode func(ns string) *DocumentSymbol
	ensureNSNode = func(ns string) *DocumentSymbol {
		if node, ok := nsNode[ns]; ok {
			return node
		}
		node := &DocumentSymbol{
			Name: ns,
			Kind: SymKindNamespace,
		}
		nsNode[ns] = node
		allNSOrder = append(allNSOrder, ns)

		parent := parentNamespace(ns)
		if parent == "::" {
			rootNSOrder = append(rootNSOrder, ns)
		} else {
			ensureNSNode(parent)
		}
		return node
	}

	// Attach direct children to namespace nodes.
	for _, ns := range nsOrder {
		if ns == "::" {
			continue // global symbols go to root directly
		}
		node := ensureNSNode(ns)
		for _, child := range nsChildren[ns] {
			node.Children = append(node.Children, child)
		}
	}

	// --- Step 4: assemble root ---

	var root []DocumentSymbol

	// Global symbols (Namespace == "::") go straight to root.
	for _, sym := range nsChildren["::"] {
		root = append(root, sym)
	}

	// Top-level namespace nodes (parent == "::").
	for _, fq := range rootNSOrder {
		node := nsNode[fq]
		// Re-attach updated child namespace nodes (intermediate nodes may have
		// received children after initial insertion).
		node.Children = rebuildNSChildren(fq, nsNode, nsChildren, allNSOrder)
		node.Range = spanChildren(node.Children)
		node.SelectionRange = node.Range
		root = append(root, *node)
	}

	// Interleave root-level global symbols and top-level namespace nodes by source
	// position, matching the per-namespace ordering above.
	sortBySourcePosition(root)

	// --- Step 5: hoist ::request children (for .rvt pages) ---
	//
	// When hoistRequest is true, a root-level ::request namespace node (produced
	// because .rvt pages are stitched into `namespace eval ::request { ... }`) is
	// elided and its children are promoted to the document root in its place.
	if hoistRequest {
		for i, node := range root {
			if node.Name == "::request" && node.Kind == SymKindNamespace {
				// Splice out the ::request node; insert its children at its position.
				hoisted := make([]DocumentSymbol, 0, len(root)-1+len(node.Children))
				hoisted = append(hoisted, root[:i]...)
				hoisted = append(hoisted, node.Children...)
				hoisted = append(hoisted, root[i+1:]...)
				root = hoisted
				break
			}
		}
	}

	return root
}

// shortName returns the simple (display) name from a fully-qualified name:
// the segment after the last "::". If no "::" is present the name is
// returned unchanged. Examples: "::app::helper" -> "helper", "helper" -> "helper".
func shortName(fq string) string {
	idx := strings.LastIndex(fq, "::")
	if idx < 0 {
		return fq
	}
	return fq[idx+2:]
}

// parentNamespace returns the parent namespace of a FQ namespace.
// "::a::b" -> "::a", "::a" -> "::", "::" -> "::".
func parentNamespace(ns string) string {
	if ns == "::" {
		return "::"
	}
	// Strip trailing "::" if present.
	trimmed := strings.TrimSuffix(ns, "::")
	idx := strings.LastIndex(trimmed, "::")
	if idx < 0 {
		return "::"
	}
	parent := trimmed[:idx]
	if parent == "" {
		return "::"
	}
	return parent
}

// spanChildren computes the Range that spans all children's Ranges.
// If there are no children, returns a zero Range.
func spanChildren(children []DocumentSymbol) Range {
	if len(children) == 0 {
		return Range{}
	}
	minStart := children[0].Range.Start
	maxEnd := children[0].Range.End
	for _, c := range children[1:] {
		if posLess(c.Range.Start, minStart) {
			minStart = c.Range.Start
		}
		if posLess(maxEnd, c.Range.End) {
			maxEnd = c.Range.End
		}
	}
	return Range{Start: minStart, End: maxEnd}
}

// posLess reports whether position a is strictly before position b.
func posLess(a, b Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character < b.Character)
}

// rebuildNSChildren builds the full Children slice for a namespace node: its
// direct symbol children and its child namespace nodes, interleaved by source
// position so the outline reads top-to-bottom (a nested `namespace eval` between
// two procs sorts between them, not after both).
//
// allNSOrder must contain every namespace node that was created (including
// synthesized intermediates with no direct symbols), so that chains like
// ::a -> ::a::b -> ::a::b::c are fully linked even when intermediate levels
// received no direct symbols of their own.
func rebuildNSChildren(fq string, nsNode map[string]*DocumentSymbol, nsChildren map[string][]DocumentSymbol, allNSOrder []string) []DocumentSymbol {
	var children []DocumentSymbol
	// Direct symbol children (procs/vars/classes) of this namespace.
	children = append(children, nsChildren[fq]...)
	// Child namespace nodes — iterate ALL created nodes, not just those that
	// received direct symbols, so synthesized intermediate levels are included.
	for _, childFQ := range allNSOrder {
		if childFQ == "::" {
			continue
		}
		if parentNamespace(childFQ) == fq {
			childNode := nsNode[childFQ]
			childNode.Children = rebuildNSChildren(childFQ, nsNode, nsChildren, allNSOrder)
			childNode.Range = spanChildren(childNode.Children)
			childNode.SelectionRange = childNode.Range
			children = append(children, *childNode)
		}
	}
	// Interleave members and sub-namespaces by position. A namespace node's Range
	// starts at its earliest descendant (spanChildren), which is inside its braces
	// and so orders correctly against sibling members. Stable, so equal-position
	// members keep their existing source order.
	sortBySourcePosition(children)
	return children
}

// sortBySourcePosition orders symbols by their Range start, stably.
func sortBySourcePosition(syms []DocumentSymbol) {
	sort.SliceStable(syms, func(i, j int) bool {
		return posLess(syms[i].Range.Start, syms[j].Range.Start)
	})
}

// offsetToPosition converts a byte offset in src to an LSP Position.
func offsetToPosition(src string, offset int) Position {
	line, char := LSPPosition(src, offset)
	return Position{Line: line, Character: char}
}

// posContains reports whether the range [outerStart, outerEnd] contains [innerStart, innerEnd].
// All four positions are inclusive on both ends in LSP line/character space.
func posContains(outerStart, outerEnd, innerStart, innerEnd Position) bool {
	startOK := outerStart.Line < innerStart.Line ||
		(outerStart.Line == innerStart.Line && outerStart.Character <= innerStart.Character)
	endOK := outerEnd.Line > innerEnd.Line ||
		(outerEnd.Line == innerEnd.Line && outerEnd.Character >= innerEnd.Character)
	return startOK && endOK
}

// buildWorkspaceSymbols filters entries by case-insensitive substring match on
// Name against query (empty query keeps all), then converts each to a
// SymbolInformation using sourceOf to compute byte-offset positions.
//
// The filter+convert is done in parallel over chunks of entries: sourceOf and
// offsetToPosition only READ (the live-doc map and the index source), and this
// runs while the dispatch loop is blocked on the request, so no shared state is
// mutated concurrently. Results are concatenated in chunk order, so the output
// order is identical to a sequential scan.
func buildWorkspaceSymbols(entries []index.SymbolEntry, query string, sourceOf func(string) string) []SymbolInformation {
	lq := strings.ToLower(query)
	convert := func(e index.SymbolEntry) (SymbolInformation, bool) {
		if lq != "" && !strings.Contains(strings.ToLower(e.Name), lq) {
			return SymbolInformation{}, false
		}
		kind, ok := symbolKind(e.Kind)
		if !ok {
			return SymbolInformation{}, false
		}
		src := sourceOf(e.File)
		return SymbolInformation{
			Name: e.Name,
			Kind: kind,
			Location: Location{
				URI: pathToURI(e.File),
				Range: Range{
					Start: offsetToPosition(src, e.NameStart),
					End:   offsetToPosition(src, e.NameEnd),
				},
			},
			ContainerName: e.Container,
		}, true
	}

	parts := par.Map(par.Chunk(entries, runtime.GOMAXPROCS(0)), func(chunk []index.SymbolEntry) []SymbolInformation {
		var out []SymbolInformation
		for _, e := range chunk {
			if si, ok := convert(e); ok {
				out = append(out, si)
			}
		}
		return out
	})

	var out []SymbolInformation
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
