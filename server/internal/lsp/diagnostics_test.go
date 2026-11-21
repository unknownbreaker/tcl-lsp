package lsp

import (
	"bytes"
	"encoding/json"
	"testing"
)

// published returns every textDocument/publishDiagnostics notification, in order.
func published(t *testing.T, ms []*Message) []PublishDiagnosticsParams {
	t.Helper()
	var out []PublishDiagnosticsParams
	for _, m := range ms {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatalf("unmarshal publishDiagnostics: %v", err)
		}
		if p.Diagnostics == nil {
			t.Fatalf("publishDiagnostics for %s has null diagnostics; want a list", p.URI)
		}
		out = append(out, p)
	}
	return out
}

func open(t *testing.T, in *bytes.Buffer, uri, text string) {
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{TextDocument: TextDocumentItem{URI: uri, Text: text}}))
}

func change(t *testing.T, in *bytes.Buffer, uri, text string) {
	in.Write(frame(t, "textDocument/didChange", nil, DidChangeParams{
		TextDocument:   TextDocumentIdentifier{URI: uri},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: text}}}))
}

func closeDoc(t *testing.T, in *bytes.Buffer, uri string) {
	in.Write(frame(t, "textDocument/didClose", nil, DidCloseParams{TextDocument: TextDocumentIdentifier{URI: uri}}))
}

func TestDiagnosticsPublishOnOpenChangeAndClearOnClose(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	open(t, &in, "file:///a.tcl", "proc p {} {\n    set x $x\n}\n")
	change(t, &in, "file:///a.tcl", "set y 1\n")
	change(t, &in, "file:///a.tcl", "set y 1\nset y ${y}\n")
	closeDoc(t, &in, "file:///a.tcl")
	in.Write(frame(t, "exit", nil, nil))

	pubs := published(t, runServer(t, in.Bytes()))
	if len(pubs) != 4 {
		t.Fatalf("got %d publishes, want 4 (open, 2 changes, close): %+v", len(pubs), pubs)
	}
	for _, p := range pubs {
		if p.URI != "file:///a.tcl" {
			t.Fatalf("publish for %s, want file:///a.tcl", p.URI)
		}
	}
	want := Diagnostic{
		Range:    Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 12}},
		Severity: DiagnosticSeverityWarning, Code: "self-assignment", Source: "tcl-lsp",
		Message: "assigns x its own value (self-assignment)",
	}
	if len(pubs[0].Diagnostics) != 1 || pubs[0].Diagnostics[0] != want {
		t.Errorf("on open: got %+v, want [%+v]", pubs[0].Diagnostics, want)
	}
	if len(pubs[1].Diagnostics) != 0 {
		t.Errorf("after a clean change: got %+v, want none", pubs[1].Diagnostics)
	}
	if d := pubs[2].Diagnostics; len(d) != 1 || d[0].Range.Start != (Position{Line: 1, Character: 0}) {
		t.Errorf("after the second change: got %+v, want one at 1:0", d)
	}
	if len(pubs[3].Diagnostics) != 0 {
		t.Errorf("on close: got %+v, want an empty list", pubs[3].Diagnostics)
	}
}

func TestDiagnosticsRVTInTemplateCoordinates(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	open(t, &in, "file:///page.rvt", "<html>\n<p><?= [set x $x] ?></p>\n")
	in.Write(frame(t, "exit", nil, nil))

	pubs := published(t, runServer(t, in.Bytes()))
	if len(pubs) != 1 || len(pubs[0].Diagnostics) != 1 {
		t.Fatalf("got %+v, want one publish with one diagnostic", pubs)
	}
	got := pubs[0].Diagnostics[0].Range
	want := Range{Start: Position{Line: 1, Character: 8}, End: Position{Line: 1, Character: 16}}
	if got != want {
		t.Errorf("range = %+v, want %+v (`set x $x` in the template)", got, want)
	}
}

func TestDiagnosticsMasterSwitchOff(t *testing.T) {
	off := false
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{Diagnostics: &off}}))
	open(t, &in, "file:///a.tcl", "set x $x\n")
	change(t, &in, "file:///a.tcl", "set x $x\nset y $y\n")
	closeDoc(t, &in, "file:///a.tcl")
	open(t, &in, "file:///b.tcl", "x")
	in.Write(frame(t, "textDocument/definition", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///b.tcl"}}))
	in.Write(frame(t, "exit", nil, nil))

	ms := runServer(t, in.Bytes())
	if pubs := published(t, ms); len(pubs) != 0 {
		t.Fatalf("diagnostics off: got %+v, want no publishes", pubs)
	}
	if responseByID(ms, "2") == nil {
		t.Fatal("diagnostics off: the server stopped answering requests")
	}
}

func TestDiagnosticsExplicitlyOn(t *testing.T) {
	on := true
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{Diagnostics: &on}}))
	open(t, &in, "file:///a.tcl", "set x $x\n")
	in.Write(frame(t, "exit", nil, nil))
	if pubs := published(t, runServer(t, in.Bytes())); len(pubs) != 1 || len(pubs[0].Diagnostics) != 1 {
		t.Fatalf("got %+v, want one diagnostic", pubs)
	}
}

// A builtin redefined anywhere in the workspace silences the Checks that assume
// it, in every document.
func TestDiagnosticsSilentWhenBuiltinRedefined(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	open(t, &in, "file:///lib.tcl", "namespace eval ::util { proc set {args} {} }\n")
	open(t, &in, "file:///a.tcl", "set x $x\n")
	in.Write(frame(t, "exit", nil, nil))
	for _, p := range published(t, runServer(t, in.Bytes())) {
		if len(p.Diagnostics) != 0 {
			t.Errorf("%s: got %+v, want none (set is redefined)", p.URI, p.Diagnostics)
		}
	}
}

// Only .tcl and .rvt documents get diagnostics.
func TestDiagnosticsOnlyForTclAndRvt(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	open(t, &in, "file:///notes.txt", "set x $x\n")
	closeDoc(t, &in, "file:///notes.txt")
	in.Write(frame(t, "exit", nil, nil))
	if pubs := published(t, runServer(t, in.Bytes())); len(pubs) != 0 {
		t.Fatalf("got %+v, want no publishes for a .txt document", pubs)
	}
}

// The option decodes from the JSON a client sends, absent meaning on.
func TestDiagnosticsOptionDecodes(t *testing.T) {
	for _, tc := range []struct {
		json string
		want *bool
	}{
		{`{}`, nil},
		{`{"diagnostics": false}`, new(bool)},
	} {
		var o InitializationOptions
		if err := json.Unmarshal([]byte(tc.json), &o); err != nil {
			t.Fatal(err)
		}
		if (o.Diagnostics == nil) != (tc.want == nil) || (o.Diagnostics != nil && *o.Diagnostics != *tc.want) {
			t.Errorf("%s: Diagnostics = %v, want %v", tc.json, o.Diagnostics, tc.want)
		}
	}
}
