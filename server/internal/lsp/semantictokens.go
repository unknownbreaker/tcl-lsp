package lsp

import (
	"sort"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Semantic token type indices. These MUST match semanticTokenTypes (the legend
// advertised in ServerCapabilities) by position -- the encoded data references a
// type by its index in the legend.
const (
	stNamespace = iota
	stClass
	stFunction
	stMethod
	stVariable
	stProperty
)

// semanticTokenTypes is the legend, in index order.
var semanticTokenTypes = []string{"namespace", "class", "function", "method", "variable", "property"}

// defTokenType maps a definition kind to a semantic token type. Only symbol-kinds
// we can color with certainty are returned.
func defTokenType(k tcl.DefKind) (int, bool) {
	switch k {
	case tcl.DefClass:
		return stClass, true
	case tcl.DefProc:
		return stFunction, true
	case tcl.DefMethod:
		return stMethod, true
	case tcl.DefNamespaceVar, tcl.DefLocal:
		return stVariable, true
	case tcl.DefIvar:
		return stProperty, true
	default:
		return 0, false
	}
}

// semanticTokens answers textDocument/semanticTokens/full. It colors only what it
// can be sure of -- definition names (by kind), $-variable uses, and command
// calls that RESOLVE to a user proc/method -- so builtins and dynamic constructs
// fall back to the editor's syntax highlighting rather than being mis-colored.
func (s *Server) semanticTokens(p SemanticTokensParams) SemanticTokens {
	path := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(path)

	type tok struct{ line, char, length, typ int }
	var toks []tok
	seen := map[int]bool{} // by start offset -- avoid overlapping/duplicate tokens
	add := func(start, end, typ int) {
		if start < 0 || end > len(src) || end <= start || seen[start] {
			return
		}
		sp := offsetToPosition(src, start)
		ep := offsetToPosition(src, end)
		if ep.Line != sp.Line || ep.Character <= sp.Character {
			return // identifiers are single-line; skip anything else
		}
		seen[start] = true
		toks = append(toks, tok{sp.Line, sp.Character, ep.Character - sp.Character, typ})
	}

	// Definition names, colored by kind.
	for _, d := range source.Defs(path, src) {
		if typ, ok := defTokenType(d.Kind); ok {
			add(d.NameStart, d.NameEnd, typ)
		}
	}
	// References: $vars -> variable; command calls resolving to a user proc/method
	// -> function/method (unresolved commands, i.e. builtins, stay uncolored).
	refs := source.Refs(path, src)
	for i := range refs {
		r := &refs[i]
		switch r.Ref.Kind {
		case tcl.RefVariable:
			add(r.Ref.Start, r.Ref.End, stVariable)
		case tcl.RefCommand:
			if kind, ok := s.res.CommandRefKind(r, path); ok {
				switch kind {
				case tcl.DefMethod:
					add(r.Ref.Start, r.Ref.End, stMethod)
				case tcl.DefProc:
					add(r.Ref.Start, r.Ref.End, stFunction)
				}
			}
		}
	}

	// Sort by position, then relative-encode into 5-tuples per the LSP spec.
	sort.Slice(toks, func(i, j int) bool {
		if toks[i].line != toks[j].line {
			return toks[i].line < toks[j].line
		}
		return toks[i].char < toks[j].char
	})
	data := make([]uint, 0, len(toks)*5)
	prevLine, prevChar := 0, 0
	for _, t := range toks {
		dLine := t.line - prevLine
		dChar := t.char
		if dLine == 0 {
			dChar = t.char - prevChar
		}
		data = append(data, uint(dLine), uint(dChar), uint(t.length), uint(t.typ), 0)
		prevLine, prevChar = t.line, t.char
	}
	return SemanticTokens{Data: data}
}
