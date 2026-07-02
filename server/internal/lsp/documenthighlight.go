package lsp

// documentHighlight returns the occurrences of the symbol under the cursor within
// the current document, for the editor to highlight (e.g. on cursor-hold). It is
// a thin, current-file-only wrapper over the resolver -- no workspace scan -- so
// it stays cheap at cursor-move frequency. Occurrences are deduplicated by range.
func (s *Server) documentHighlight(p DocumentHighlightParams) []DocumentHighlight {
	path := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(path)
	off := ByteOffset(src, p.Position.Line, p.Position.Character)

	seen := map[[2]int]bool{}
	var out []DocumentHighlight
	for _, l := range s.res.FileHighlights(path, src, off) {
		if l.File != path {
			continue // FileHighlights is in-file already; guard defensively
		}
		key := [2]int{l.NameStart, l.NameEnd}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, DocumentHighlight{Range: Range{
			Start: offsetToPosition(src, l.NameStart),
			End:   offsetToPosition(src, l.NameEnd),
		}})
	}
	return out
}
