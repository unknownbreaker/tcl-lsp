package lsp

import (
	"sort"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// selectionRanges answers textDocument/selectionRange: for each cursor position,
// the nested hierarchy the editor uses for expand-selection. The hierarchy is
// purely structural -- the identifier under the cursor, then each enclosing
// braced body (the same spans that drive folding), then the whole document -- so
// it is never wrong; at worst a level is coarser than an editor with a full
// grammar would offer.
func (s *Server) selectionRanges(p SelectionRangeParams) []*SelectionRange {
	path := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(path)
	folds := source.Folds(path, src)

	out := make([]*SelectionRange, 0, len(p.Positions))
	for _, pos := range p.Positions {
		off := ByteOffset(src, pos.Line, pos.Character)
		out = append(out, buildSelectionRange(src, folds, off))
	}
	return out
}

// buildSelectionRange returns the innermost SelectionRange at off, its Parent
// chain growing outward to the whole document. Levels: the identifier token at
// off, every braced body containing off, and the document. Non-nesting or
// duplicate levels are dropped so each range strictly contains its child.
func buildSelectionRange(src string, folds []tcl.FoldRange, off int) *SelectionRange {
	type span struct{ start, end int }
	var spans []span

	// Identifier token under the cursor (finest level), when off is on a name.
	if ws, we := identifierSpanAt(src, off); we > ws {
		spans = append(spans, span{ws, we})
	}
	// Each enclosing braced body: the whole `{...}` from its open to close brace.
	for _, f := range folds {
		if f.Open <= off && off <= f.Close && f.Close < len(src) {
			spans = append(spans, span{f.Open, f.Close + 1})
		}
	}
	// The whole document is always a valid outermost level.
	spans = append(spans, span{0, len(src)})

	// Innermost (smallest) first.
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].end-spans[i].start < spans[j].end-spans[j].start
	})

	// Keep a strictly-nesting chain: drop duplicates and any span that does not
	// contain the current innermost.
	var chain []span
	for _, sp := range spans {
		if len(chain) > 0 {
			last := chain[len(chain)-1]
			if sp.start == last.start && sp.end == last.end {
				continue
			}
			if sp.start > last.start || sp.end < last.end {
				continue
			}
		}
		chain = append(chain, sp)
	}

	// Build from outermost (Parent nil) inward; return the innermost.
	var node *SelectionRange
	for i := len(chain) - 1; i >= 0; i-- {
		node = &SelectionRange{
			Range: Range{
				Start: offsetToPosition(src, chain[i].start),
				End:   offsetToPosition(src, chain[i].end),
			},
			Parent: node,
		}
	}
	return node
}

// identifierSpanAt returns the byte range of the TCL name token surrounding off
// (name bytes plus the ':' of :: separators), or (off, off) when off is not on a
// name.
func identifierSpanAt(src string, off int) (int, int) {
	if off < 0 || off > len(src) {
		return 0, 0
	}
	isPart := func(i int) bool {
		if i < 0 || i >= len(src) {
			return false
		}
		return isNamePart(src[i])
	}
	start, end := off, off
	for start > 0 && isPart(start-1) {
		start--
	}
	for end < len(src) && isPart(end) {
		end++
	}
	return start, end
}

// isNamePart reports whether b can appear in a (possibly qualified) TCL name.
func isNamePart(b byte) bool {
	return b == '_' || b == ':' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
