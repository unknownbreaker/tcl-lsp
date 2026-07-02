package lsp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestServerAdvertisesSelectionRange(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "1")
	var res InitializeResult
	_ = json.Unmarshal(resp.Result, &res)
	if !res.Capabilities.SelectionRangeProvider {
		t.Fatalf("selection range capability not advertised: %#v", res.Capabilities)
	}
}

// contains reports whether outer fully contains inner.
func rangeContains(outer, inner Range) bool {
	return posLE(outer.Start, inner.Start) && posLE(inner.End, outer.End)
}

// The hierarchy at a cursor inside a nested body grows strictly outward:
// identifier -> if body -> proc body -> whole document, each containing the
// cursor and its child.
func TestServerSelectionRangeNesting(t *testing.T) {
	src := "proc p {} {\n  if {1} {\n    puts hello\n  }\n}\n"
	cursor := Position{Line: 2, Character: 10} // inside "hello"

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///m.tcl", Text: src}}))
	in.Write(frame(t, "textDocument/selectionRange", 2, SelectionRangeParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///m.tcl"},
		Positions:    []Position{cursor},
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")

	var ranges []*SelectionRange
	_ = json.Unmarshal(resp.Result, &ranges)
	if len(ranges) != 1 || ranges[0] == nil {
		t.Fatalf("selection ranges = %#v, want one non-nil", ranges)
	}

	// Innermost must contain the cursor.
	inner := ranges[0]
	if !rangeContains(inner.Range, Range{Start: cursor, End: cursor}) {
		t.Fatalf("innermost range %#v does not contain cursor %#v", inner.Range, cursor)
	}

	// Walk outward: each parent strictly contains its child; the outermost starts
	// at the document origin.
	levels := 1
	cur := inner
	for cur.Parent != nil {
		if !rangeContains(cur.Parent.Range, cur.Range) {
			t.Fatalf("parent %#v does not contain child %#v", cur.Parent.Range, cur.Range)
		}
		if cur.Range == cur.Parent.Range {
			t.Fatalf("parent equals child (not strictly larger): %#v", cur.Range)
		}
		cur = cur.Parent
		levels++
	}
	if levels < 3 {
		t.Fatalf("expected >=3 nesting levels (identifier/body/.../file), got %d", levels)
	}
	if cur.Range.Start != (Position{0, 0}) {
		t.Fatalf("outermost range should start at document origin, got %#v", cur.Range.Start)
	}
}
