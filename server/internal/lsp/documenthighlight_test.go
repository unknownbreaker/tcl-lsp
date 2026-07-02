package lsp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestServerAdvertisesDocumentHighlight(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "1")
	var res InitializeResult
	_ = json.Unmarshal(resp.Result, &res)
	if !res.Capabilities.DocumentHighlightProvider {
		t.Fatalf("document highlight capability not advertised: %#v", res.Capabilities)
	}
}

// A proc defined once and called twice highlights all three occurrences (the
// declaration plus both call sites), when the cursor is on any of them.
func TestServerDocumentHighlightProc(t *testing.T) {
	src := "proc foo {} {}\nfoo\nfoo\n"
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///m.tcl", Text: src}}))
	in.Write(frame(t, "textDocument/documentHighlight", 2, DocumentHighlightParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///m.tcl"},
		Position:     Position{Line: 1, Character: 0}, // on the first call
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")
	var hs []DocumentHighlight
	_ = json.Unmarshal(resp.Result, &hs)
	if len(hs) != 3 {
		t.Fatalf("document highlights = %#v, want 3 (def + 2 calls)", hs)
	}
}

// A proc-local variable highlights its binding and uses within the proc.
func TestServerDocumentHighlightLocalVar(t *testing.T) {
	src := "proc p {} {\n  set x 1\n  puts $x\n}\n"
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///m.tcl", Text: src}}))
	in.Write(frame(t, "textDocument/documentHighlight", 2, DocumentHighlightParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///m.tcl"},
		Position:     Position{Line: 2, Character: 8}, // on $x
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")
	var hs []DocumentHighlight
	_ = json.Unmarshal(resp.Result, &hs)
	if len(hs) != 2 {
		t.Fatalf("local var highlights = %#v, want 2 (set x + $x)", hs)
	}
}
