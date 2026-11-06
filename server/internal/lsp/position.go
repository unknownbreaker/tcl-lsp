package lsp

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

// ByteOffset converts an LSP (line, character) position — where character counts
// UTF-16 code units — to a byte offset in src. Positions past the end of a line
// clamp to the line end; lines past the end clamp to len(src).
func ByteOffset(src string, line, character int) int {
	off := 0
	for curLine := 0; off < len(src) && curLine < line; off++ {
		if src[off] == '\n' {
			curLine++
		}
	}
	u16 := 0
	for off < len(src) && src[off] != '\n' && u16 < character {
		r, size := utf8.DecodeRuneInString(src[off:])
		u16 += utf16.RuneLen(r)
		off += size
	}
	return off
}

// LSPPosition converts a byte offset in src to an LSP (line, character) position,
// where character counts UTF-16 code units. Offsets past len(src) clamp to it.
// A mid-rune offset is treated as pointing just past that rune; pass only rune-boundary offsets.
// It scans all of src on every call, so anything converting more than one offset uses a lineIndex.
func LSPPosition(src string, offset int) (line, character int) {
	return newLineIndex(src).lineChar(offset)
}

// lineIndex converts byte offsets in one source to LSP positions without rescanning
// the source for each lookup. It is read-only once built, so workers may share it.
type lineIndex struct {
	src        string
	lineStarts []int // byte offset where each line begins, ascending; lineStarts[0] is 0
}

// newLineIndex records where every line of src begins.
func newLineIndex(src string) *lineIndex {
	lineStarts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	return &lineIndex{src: src, lineStarts: lineStarts}
}

// lineChar has the contract of LSPPosition. A negative offset returns (0, 0).
func (ix *lineIndex) lineChar(offset int) (line, character int) {
	if offset > len(ix.src) {
		offset = len(ix.src)
	}
	line = sort.SearchInts(ix.lineStarts, offset+1) - 1
	if line < 0 {
		return 0, 0
	}
	for i := ix.lineStarts[line]; i < offset; {
		r, size := utf8.DecodeRuneInString(ix.src[i:])
		character += utf16.RuneLen(r)
		i += size
	}
	return line, character
}

// position is lineChar as a Position.
func (ix *lineIndex) position(offset int) Position {
	line, character := ix.lineChar(offset)
	return Position{Line: line, Character: character}
}

// lineIndexes holds one lineIndex per file for the length of a single request. It
// must never be stored on the Server: a document can change between requests.
type lineIndexes map[string]*lineIndex

// forFile returns the file's index, building it from sourceOf on the first ask. Only
// a first ask writes to the map, so concurrent readers must have every file filled in.
func (c lineIndexes) forFile(file string, sourceOf func(string) string) *lineIndex {
	if lines, ok := c[file]; ok {
		return lines
	}
	lines := newLineIndex(sourceOf(file))
	c[file] = lines
	return lines
}
