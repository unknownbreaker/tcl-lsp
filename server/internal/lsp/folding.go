package lsp

import "github.com/unknownbreaker/tcl-lsp/internal/tcl"

// buildFoldingRanges converts source-coordinate fold spans (the '{' and '}'
// offsets of each braced body) into LSP FoldingRanges keyed by line. src is the
// same source the folds were computed against (the .tcl text, or the .rvt text
// for templates), so its line table resolves both endpoints. Single-line bodies
// — where the open and close share a line — are dropped, since there is nothing
// to collapse. The editor folds startLine+1..endLine, leaving the opening line
// (e.g. `proc foo {} {`) visible.
func buildFoldingRanges(folds []tcl.FoldRange, src string) []FoldingRange {
	lines := newLineIndex(src)
	out := make([]FoldingRange, 0, len(folds))
	for _, f := range folds {
		startLine, _ := lines.lineChar(f.Open)
		endLine, _ := lines.lineChar(f.Close)
		if endLine > startLine {
			out = append(out, FoldingRange{StartLine: startLine, EndLine: endLine})
		}
	}
	return out
}
