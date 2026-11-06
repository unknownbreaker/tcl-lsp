package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/resolve"
)

// The variable is indexed first, so a lookup that ignores sides sees it first and
// the call loses its function color.
func TestSemanticTokenForCallIsNotVariable(t *testing.T) {
	const uri = "file:///m.tcl"
	src := "set ::a::f 1\nproc ::a::f {} {}\n::a::f\n"
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: src}}))
	in.Write(frame(t, "textDocument/semanticTokens/full", 2, SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: uri}}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")

	var tokens SemanticTokens
	if err := json.Unmarshal(resp.Result, &tokens); err != nil {
		t.Fatal(err)
	}
	const callLine = 2
	for _, token := range decodeSemanticTokens(tokens.Data) {
		if token.line == callLine && token.char == 0 {
			if token.typ != stFunction {
				t.Fatalf("call token type = %d, want stFunction (%d)", token.typ, stFunction)
			}
			return
		}
	}
	t.Fatalf("no token on the call at line %d; tokens = %#v", callLine, decodeSemanticTokens(tokens.Data))
}

// `$test` resolves to the same name as the proc, but reading a variable is not a
// call. The real call keeps the test from passing on an empty answer.
func TestIncomingCallsExcludeVariableUses(t *testing.T) {
	const uri = "file:///m.tcl"
	src := "proc test {} {}\nset test 1\nproc caller {} {\n  test\n}\nputs $test\n"
	procName := Range{Start: posOf(src, "test"), End: posOf(src, " {} {}\nset")}
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: uri, Text: src}}))
	in.Write(frame(t, "callHierarchy/incomingCalls", 2, CallHierarchyIncomingCallsParams{
		Item: CallHierarchyItem{Name: "test", Kind: SymKindFunction, URI: uri, Range: procName, SelectionRange: procName},
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")

	var calls []CallHierarchyIncomingCall
	if err := json.Unmarshal(resp.Result, &calls); err != nil {
		t.Fatal(err)
	}
	variableUse := posOf(src, "$test")
	var realCallFound bool
	for _, call := range calls {
		for _, fromRange := range call.FromRanges {
			if fromRange.Start.Line == variableUse.Line {
				t.Errorf("incoming calls list the variable use: from %q at %+v", call.From.Name, fromRange)
			}
		}
		if call.From.Name == "caller" && len(call.FromRanges) == 1 {
			realCallFound = true
		}
	}
	if !realCallFound {
		t.Errorf("incoming calls = %#v, want the call from caller", calls)
	}
}

// ---- Adversarial additions -------------------------------------------------

type sideDoc struct{ uri, text string }

// orderedServer opens the documents in the order given, unlike newCHServer, whose
// map iteration randomizes which side of a collision Index.Lookup returns first.
func orderedServer(docs ...sideDoc) *Server {
	ix := index.New()
	s := &Server{ix: ix, res: resolve.New(ix), docs: map[string]string{}}
	for _, doc := range docs {
		s.setDoc(doc.uri, doc.text)
	}
	return s
}

func locationKeys(locs []Location) []string {
	out := make([]string, 0, len(locs))
	for _, loc := range locs {
		out = append(out, fmt.Sprintf("%s:%d:%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character))
	}
	sort.Strings(out)
	return out
}

func wantKeys(entries ...string) []string {
	sort.Strings(entries)
	return entries
}

func key(uri string, pos Position) string {
	return fmt.Sprintf("%s:%d:%d", uri, pos.Line, pos.Character)
}

// Find-references through the protocol layer: includeDeclaration prepends
// Declarations, which must be the declaration of the SAME side as the references.
// The variable is indexed first so an unfiltered Lookup would lead with it.
func TestReferencesIncludeDeclarationHonorsSideAcrossFiles(t *testing.T) {
	const libURI, useURI = "file:///lib.tcl", "file:///use.tcl"
	lib := "set test 1\nproc test {} {}\n"
	use := "test\nputs $test\n"
	s := orderedServer(sideDoc{libURI, lib}, sideDoc{useURI, use})

	procName := posOf(lib, "test {}")
	setName := posOf(lib, "test 1")
	call := Position{Line: 0, Character: 0}
	varUse := posOf(use, "$test")
	varUseCursor := Position{Line: varUse.Line, Character: varUse.Character + 1}

	procSide := wantKeys(key(libURI, procName), key(useURI, call))
	variableSide := wantKeys(key(libURI, setName), key(useURI, varUse))
	cases := []struct {
		label       string
		uri         string
		cursor      Position
		declaration bool
		want        []string
	}{
		{"call", useURI, call, true, procSide},
		{"proc name", libURI, procName, true, procSide},
		{"$use", useURI, varUseCursor, true, variableSide},
		{"set name", libURI, setName, true, variableSide},
		{"call, no declaration", useURI, call, false, wantKeys(key(useURI, call))},
		{"$use, no declaration", useURI, varUseCursor, false, wantKeys(key(useURI, varUse))},
	}
	for _, c := range cases {
		got := locationKeys(s.handleReferences(ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: c.uri}, Position: c.cursor},
			Context: ReferenceContext{IncludeDeclaration: c.declaration}}))
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("references from %s = %v, want %v", c.label, got, c.want)
		}
	}
}

// A name defined only on the other side has no declaration to prepend.
func TestReferencesIncludeDeclarationNeverListsOtherSideDeclaration(t *testing.T) {
	const uri = "file:///m.tcl"
	cases := []struct {
		label, src string
		cursor     func(src string) Position
		want       func(src string) []string
	}{
		{"proc only, from $use", "proc test {} {}\ntest\nputs $test\n",
			func(src string) Position {
				p := posOf(src, "$test")
				return Position{Line: p.Line, Character: p.Character + 1}
			},
			func(src string) []string { return wantKeys(key(uri, posOf(src, "$test"))) }},
		{"variable only, from the call", "set test 1\ntest\nputs $test\n",
			func(src string) Position { return posOf(src, "test\nputs") },
			func(src string) []string { return wantKeys(key(uri, posOf(src, "test\nputs"))) }},
	}
	for _, c := range cases {
		s := orderedServer(sideDoc{uri, c.src})
		got := locationKeys(s.handleReferences(ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: c.cursor(c.src)},
			Context: ReferenceContext{IncludeDeclaration: true}}))
		if want := c.want(c.src); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: references = %v, want %v", c.label, got, want)
		}
	}
}

func tokenTypeAt(s *Server, uri string, line, char int) (int, bool) {
	tokens := s.semanticTokens(SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	for _, token := range decodeSemanticTokens(tokens.Data) {
		if token.line == line && token.char == char {
			return token.typ, true
		}
	}
	return 0, false
}

// Cross-file: the call's file never mentions the variable, and index order puts it
// ahead of (or behind) the proc.
func TestSemanticTokenForCallIgnoresVariableInAnotherFile(t *testing.T) {
	variable := sideDoc{"file:///var.tcl", "set ::a::f 1\n"}
	proc := sideDoc{"file:///proc.tcl", "proc ::a::f {} {}\n"}
	use := sideDoc{"file:///use.tcl", "::a::f\n"}
	orders := map[string][]sideDoc{"var,proc,use": {variable, proc, use}, "proc,var,use": {proc, variable, use}}

	for label, docs := range orders {
		s := orderedServer(docs...)
		if typ, ok := tokenTypeAt(s, use.uri, 0, 0); !ok || typ != stFunction {
			t.Errorf("%s: call token = (%d, %v), want stFunction (%d)", label, typ, ok, stFunction)
		}
	}
}

// The call falls through the namespace's same-named variable to the global proc.
func TestSemanticTokenForCallFallsThroughShadowingVariable(t *testing.T) {
	const uri = "file:///m.tcl"
	src := "proc debug {} {}\nnamespace eval ::n {\n  variable debug 1\n  debug\n}\n"
	s := orderedServer(sideDoc{uri, src})

	if typ, ok := tokenTypeAt(s, uri, 3, 2); !ok || typ != stFunction {
		t.Fatalf("call token = (%d, %v), want stFunction (%d)", typ, ok, stFunction)
	}
}

// With a proc and a variable of one name, only the proc is a call-hierarchy anchor:
// the cursor on `$test` or on `set test` must not prepare an item.
func TestPrepareCallHierarchyIgnoresVariableSideOfCollision(t *testing.T) {
	const uri = "file:///m.tcl"
	src := "set test 1\nproc test {} {}\nputs $test\n"
	s := orderedServer(sideDoc{uri, src})

	varUse := posOf(src, "$test")
	cursors := map[string]Position{
		"$test":    {Line: varUse.Line, Character: varUse.Character + 1},
		"set test": posOf(src, "test 1"),
	}
	for label, cursor := range cursors {
		items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: cursor})
		if len(items) != 0 {
			t.Errorf("prepare on %s = %#v, want no item", label, items)
		}
	}
}

const shadowedCallerSource = "proc debug {} {}\nnamespace eval ::n {\n  variable debug 1\n  proc caller {} {\n    debug\n  }\n}\n"

// `debug` inside ::n resolves past the namespace variable to ::debug, so the proc
// has exactly one incoming call.
func TestIncomingCallsSeeCallPastShadowingVariable(t *testing.T) {
	const uri = "file:///m.tcl"
	s := orderedServer(sideDoc{uri, shadowedCallerSource})
	selection := Range{Start: posOf(shadowedCallerSource, "debug {}")}

	calls := s.incomingCalls(CallHierarchyIncomingCallsParams{
		Item: CallHierarchyItem{Name: "debug", Kind: SymKindFunction, URI: uri, Range: selection, SelectionRange: selection}})
	if len(calls) != 1 || calls[0].From.Name != "caller" || len(calls[0].FromRanges) != 1 {
		t.Fatalf("incoming calls = %#v, want one call from caller", calls)
	}
}

func TestOutgoingCallsSeeCallPastShadowingVariable(t *testing.T) {
	const uri = "file:///m.tcl"
	s := orderedServer(sideDoc{uri, shadowedCallerSource})
	selection := Range{Start: posOf(shadowedCallerSource, "caller {}")}

	calls := s.outgoingCalls(CallHierarchyOutgoingCallsParams{
		Item: CallHierarchyItem{Name: "caller", Kind: SymKindFunction, URI: uri, Range: selection, SelectionRange: selection}})
	if len(calls) != 1 || calls[0].To.Name != "debug" || len(calls[0].FromRanges) != 1 {
		t.Fatalf("outgoing calls = %#v, want one call to debug", calls)
	}
}
