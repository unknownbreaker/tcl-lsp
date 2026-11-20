package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// #100 through the handler: goto-definition on the method word of `$d field`
// inside an `if` condition answers with the Itcl method's location.
func TestServerDefinitionObjMethodInIfCondition(t *testing.T) {
	lib := "itcl::class ::STDisplay {\n  method field {name} {}\n}"
	use := "proc f {} {\n  set d [::STDisplay #auto]\n  if {[$d field isbn]} {}\n}"
	lines := strings.Split(use, "\n")
	col := strings.Index(lines[2], "field isbn")
	if col < 0 {
		t.Fatal("mark `field isbn` not on line 2")
	}

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///disp.tcl", Text: lib}}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///use.tcl", Text: use}}))
	in.Write(frame(t, "textDocument/definition", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///use.tcl"},
		Position:     Position{Line: 2, Character: col}}))
	in.Write(frame(t, "exit", nil, nil))

	resp := responseByID(runServer(t, in.Bytes()), "2")
	if resp == nil {
		t.Fatal("no definition response")
	}
	var locs []Location
	if err := json.Unmarshal(resp.Result, &locs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(locs) != 1 || locs[0].URI != "file:///disp.tcl" {
		t.Fatalf("definition = %#v", locs)
	}
	wantCol := strings.Index("  method field {name} {}", "field")
	if locs[0].Range.Start != (Position{Line: 1, Character: wantCol}) {
		t.Fatalf("range = %#v, want method name at line 1 col %d", locs[0].Range, wantCol)
	}
}
