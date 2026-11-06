package lsp

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
)

const largeFileLineCount = 30000

// largeArrayFileSource generates a file shaped like the one that exposed the
// per-call position rescan: tens of thousands of namespace-array assignments, each
// a definition whose positions the server must compute.
func largeArrayFileSource() string {
	return strings.Repeat("set ::a(k) {v}\n", largeFileLineCount)
}

// BenchmarkDocumentSymbolLargeFile does what the textDocument/documentSymbol handler
// does for a large file, so its timing is that request's cost.
func BenchmarkDocumentSymbolLargeFile(b *testing.B) {
	src := largeArrayFileSource()
	for i := 0; i < b.N; i++ {
		symbols := buildDocumentSymbols(source.Defs("big.tcl", src), src, false)
		if len(symbols) == 0 {
			b.Fatal("no document symbols for the generated file")
		}
	}
}
