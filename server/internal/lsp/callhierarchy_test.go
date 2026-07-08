package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/resolve"
)

func newCHServer(docs map[string]string) *Server {
	ix := index.New()
	s := &Server{ix: ix, res: resolve.New(ix), docs: map[string]string{}}
	for uri, text := range docs {
		s.setDoc(uri, text)
	}
	return s
}

// posOf returns the LSP position of the first byte of substr in src.
func posOf(src, substr string) Position {
	off := strings.Index(src, substr)
	l, c := LSPPosition(src, off)
	return Position{Line: l, Character: c}
}

func TestCallHierarchy(t *testing.T) {
	lib := "proc helper {x} { return $x }\nproc render {} {\n  helper 1\n  helper 2\n}"
	caller := "proc main {} {\n  render\n}"
	page := "<? render ?>"
	s := newCHServer(map[string]string{
		"file:///lib.tcl":    lib,
		"file:///caller.tcl": caller,
		"file:///page.rvt":   page,
	})

	// prepare on the `render` definition.
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///lib.tcl"},
		Position:     posOf(lib, "render {}"),
	})
	if len(items) != 1 {
		t.Fatalf("prepare returned %d items, want 1: %#v", len(items), items)
	}
	item := items[0]
	if item.Name != "render" || item.Kind != SymKindFunction {
		t.Fatalf("prepared item = %+v, want render/Function", item)
	}

	// incomingCalls: render is called by main (a proc) and by the page (file-level).
	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: item})
	callers := map[string]int{}
	for _, c := range in {
		callers[c.From.Name] = len(c.FromRanges)
	}
	if callers["main"] != 1 {
		t.Errorf("incoming: expected main to call render once, got %v", callers)
	}
	if callers["page.rvt"] != 1 {
		t.Errorf("incoming: expected page.rvt (file-level) caller, got %v", callers)
	}

	// outgoingCalls: render calls helper (twice).
	out := s.outgoingCalls(CallHierarchyOutgoingCallsParams{Item: item})
	callees := map[string]int{}
	for _, c := range out {
		callees[c.To.Name] = len(c.FromRanges)
	}
	if callees["helper"] != 2 {
		t.Errorf("outgoing: expected render to call helper twice, got %v", callees)
	}
}

// TestCallHierarchyOutgoingCrossFileCoordinates pins the LSP coordinate
// contract for a CROSS-FILE outgoing call: To carries the CALLEE's file and the
// callee-name position in THAT file's coordinates, while FromRanges are the
// call-site positions in the CALLER's file. The fixture places the callee at a
// line the caller's file doesn't even have, so any mispairing (e.g. a client
// applying FromRanges in To's file — Neovim's deprecated built-in quickfix
// handler does exactly this) is unambiguous. Same-file callees mask that bug,
// which is why it must be pinned cross-file.
func TestCallHierarchyOutgoingCrossFileCoordinates(t *testing.T) {
	callee := "# pad\n# pad\n# pad\n# pad\n# pad\nproc far_helper {} {}\n" // def at line 5
	caller := "proc caller {} {\n  far_helper\n}\n"                       // call at line 1
	s := newCHServer(map[string]string{
		"file:///callee.tcl": callee,
		"file:///caller.tcl": caller,
	})

	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///caller.tcl"},
		Position:     posOf(caller, "caller"),
	})
	if len(items) != 1 {
		t.Fatalf("prepare returned %d items, want 1", len(items))
	}
	out := s.outgoingCalls(CallHierarchyOutgoingCallsParams{Item: items[0]})
	if len(out) != 1 {
		t.Fatalf("outgoing returned %d calls, want 1: %#v", len(out), out)
	}
	c := out[0]

	// To: the callee's definition, in the callee file's coordinates.
	if c.To.URI != "file:///callee.tcl" {
		t.Errorf("To.URI = %q, want callee.tcl", c.To.URI)
	}
	if want := posOf(callee, "far_helper"); c.To.SelectionRange.Start != want {
		t.Errorf("To.SelectionRange.Start = %+v, want %+v (callee-file coords)", c.To.SelectionRange.Start, want)
	}

	// FromRanges: the call site, in the CALLER file's coordinates.
	if len(c.FromRanges) != 1 {
		t.Fatalf("FromRanges = %#v, want exactly 1", c.FromRanges)
	}
	if want := posOf(caller, "far_helper"); c.FromRanges[0].Start != want {
		t.Errorf("FromRanges[0].Start = %+v, want %+v (caller-file coords)", c.FromRanges[0].Start, want)
	}

	// The mispairing detector: the two coordinate systems must actually differ
	// in this fixture, or the assertions above prove nothing.
	if c.FromRanges[0].Start.Line == c.To.SelectionRange.Start.Line {
		t.Fatalf("fixture degenerate: caller call-site line == callee def line")
	}
}

// TestCallHierarchyItcl: method-to-method edges via the Itcl Tier-2 resolution —
// a bare intra-class call inside one method resolves to a sibling method.
func TestCallHierarchyItcl(t *testing.T) {
	src := "::itcl::class ::C {\n" +
		"  public method helper {} {}\n" +
		"  public method run {} { helper }\n" +
		"}"
	s := newCHServer(map[string]string{"file:///c.tcl": src})

	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///c.tcl"},
		Position:     posOf(src, "helper {}"),
	})
	if len(items) != 1 || items[0].Kind != SymKindMethod || items[0].Name != "helper" {
		t.Fatalf("prepare on method = %#v, want helper/Method", items)
	}

	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
	var fromRun bool
	for _, c := range in {
		if c.From.Name == "run" && c.From.Kind == SymKindMethod {
			fromRun = true
		}
	}
	if !fromRun {
		t.Errorf("incoming on method helper: expected caller method run, got %#v", in)
	}

	// outgoing on run -> helper
	runItems := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///c.tcl"},
		Position:     posOf(src, "run {}"),
	})
	out := s.outgoingCalls(CallHierarchyOutgoingCallsParams{Item: runItems[0]})
	var toHelper bool
	for _, c := range out {
		if c.To.Name == "helper" {
			toHelper = true
		}
	}
	if !toHelper {
		t.Errorf("outgoing on method run: expected callee helper, got %#v", out)
	}
}

// TestCallHierarchyNestedOutgoing: a call inside a proc nested in the anchor's
// body belongs to the nested proc, not the anchor — outgoing must agree with
// incoming's innermost-enclosing attribution.
func TestCallHierarchyNestedOutgoing(t *testing.T) {
	src := "proc target {} {}\nproc outer {} {\n  proc inner {} {\n    target\n  }\n}"
	s := newCHServer(map[string]string{"file:///n.tcl": src})

	outer := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///n.tcl"},
		Position:     posOf(src, "outer {}"),
	})
	for _, c := range s.outgoingCalls(CallHierarchyOutgoingCallsParams{Item: outer[0]}) {
		if c.To.Name == "target" {
			t.Errorf("outgoing(outer) wrongly includes target (called by nested inner)")
		}
	}

	inner := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///n.tcl"},
		Position:     posOf(src, "inner {}"),
	})
	var hasTarget bool
	for _, c := range s.outgoingCalls(CallHierarchyOutgoingCallsParams{Item: inner[0]}) {
		if c.To.Name == "target" {
			hasTarget = true
		}
	}
	if !hasTarget {
		t.Errorf("outgoing(inner) should include target")
	}
}

func TestServerAdvertisesCallHierarchy(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "1")
	var res InitializeResult
	_ = json.Unmarshal(resp.Result, &res)
	if !res.Capabilities.CallHierarchyProvider {
		t.Fatalf("callHierarchyProvider not advertised: %#v", res.Capabilities)
	}
}
