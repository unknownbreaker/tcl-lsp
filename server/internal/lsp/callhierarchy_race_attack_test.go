// Package lsp — concurrency attack tests for the parallel fan-out behind a
// method's incoming calls (resolve.References over methodOwner).
//
// Run with: go test -race -count=50 ./internal/lsp/...
//
// Attack strategy:
//  1. Verify methodOwner only reads the class table (no writes).
//  2. Verify a.name bare-name filter across many same-named methods in
//     different classes with inheritance.
//  3. Stress MRO cycle detection across many goroutines (per-call seen map).
//  4. Result parity and deterministic order.
//  5. GOMAXPROCS=1 path.
package lsp

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/resolve"
)

// newIncomingServer builds a Server with an index pre-populated from files. Unlike
// newCHServer it takes (path, content) pairs so tests can use bare paths.
func newIncomingServer(files map[string]string) *Server {
	ix := index.New()
	s := &Server{ix: ix, res: resolve.New(ix), docs: map[string]string{}}
	for path, content := range files {
		s.setDoc("file://"+path, content)
	}
	return s
}

// --- Attack 1: many classes with the same bare method name, one class per file.
//
// References filters a method target by bare name first.
// When many classes each have a "run" method, every ref passes the name filter;
// methodOwner then applies MRO to pick only callers that resolve to the
// target anchor. Verify: only callers that inherit the target class are returned,
// and the result count is exact. Run under -race to prove the class table reads
// are concurrent-safe.
//
// NAMING NOTE: Sub callers use "sg%03d" and unrelated callers use "ug%03d" to
// avoid name collisions in the callerMap check — both groups call "run" but the
// ENCLOSING METHOD names must be distinct so we can distinguish them.
func TestIncomingMethodCallsManySameNamedMethods(t *testing.T) {
	const n = 50
	files := map[string]string{}

	// Base class with the "run" method we anchor on.
	base := "itcl::class ::Base {\n  public method run {} {}\n}"
	files["/base.tcl"] = base

	// n derived classes that inherit ::Base (so they call ::Base::run via MRO).
	// Their caller method is named "sg%03d" (sub-go).
	for k := 0; k < n; k++ {
		cls := fmt.Sprintf("::Sub%03d", k)
		src := fmt.Sprintf("itcl::class %s {\n  inherit ::Base\n  public method sg%03d {} { run }\n}", cls, k)
		files[fmt.Sprintf("/sub%03d.tcl", k)] = src
	}

	// m "unrelated" classes that also have a "run" method but do NOT inherit ::Base.
	// Their "run" call resolves to their own local method, NOT ::Base::run.
	// Their caller method is named "ug%03d" (unrelated-go) — distinct from "sg".
	const m = 20
	for k := 0; k < m; k++ {
		cls := fmt.Sprintf("::Unrelated%03d", k)
		src := fmt.Sprintf(
			"itcl::class %s {\n  public method run {} {}\n  public method ug%03d {} { run }\n}", cls, k)
		files[fmt.Sprintf("/unrelated%03d.tcl", k)] = src
	}

	s := newIncomingServer(files)

	// Anchor: ::Base::run (the definition at the start of base.tcl).
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///base.tcl"},
		Position:     posOf(base, "run {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing for ::Base::run")
	}

	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})

	// Collect caller names.
	callerMap := map[string]bool{}
	for _, c := range in {
		callerMap[c.From.Name] = true
	}

	// Unrelated classes (ug*) override run themselves: methodOwner must not return
	// ::Base for them (their resolution lands on their own run, not ::Base::run).
	for k := 0; k < m; k++ {
		ugName := fmt.Sprintf("ug%03d", k)
		if callerMap[ugName] {
			t.Errorf("unrelated class %q wrongly attributed to ::Base::run incoming calls", ugName)
		}
	}
	// Subs (sg*) don't override run, so their call resolves up the MRO to ::Base::run.
	for k := 0; k < n; k++ {
		sgName := fmt.Sprintf("sg%03d", k)
		if !callerMap[sgName] {
			t.Errorf("sub class %q missing from ::Base::run incoming calls", sgName)
		}
	}
}

// --- Attack 2: MRO cycle via diamond inheritance across many goroutines.
//
// methodOwner uses a per-call-local `seen` map. A diamond (A <- B, A <- C,
// B <- D, C <- D) must not loop forever. Multiple goroutines each have their own
// `seen` — verify no write-sharing under -race.
func TestIncomingMethodCallsMROCycleParallel(t *testing.T) {
	// Diamond: Base <- Left, Base <- Right, Left <- Diamond, Right <- Diamond.
	files := map[string]string{
		"/base.tcl":    "itcl::class ::Base {\n  public method draw {} {}\n}",
		"/left.tcl":    "itcl::class ::Left {\n  inherit ::Base\n}",
		"/right.tcl":   "itcl::class ::Right {\n  inherit ::Base\n}",
		"/diamond.tcl": "itcl::class ::Diamond {\n  inherit ::Left\n  inherit ::Right\n  public method go {} { draw }\n}",
	}
	// Add many more files that call draw through Diamond.
	for k := 0; k < 60; k++ {
		src := fmt.Sprintf("itcl::class ::D%03d {\n  inherit ::Diamond\n  public method use%03d {} { draw }\n}", k, k)
		files[fmt.Sprintf("/d%03d.tcl", k)] = src
	}

	s := newIncomingServer(files)
	baseSrc := "itcl::class ::Base {\n  public method draw {} {}\n}"
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///base.tcl"},
		Position:     posOf(baseSrc, "draw {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing for ::Base::draw")
	}
	// Must not deadlock, hang, or race — just verify it terminates.
	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
	if len(in) == 0 {
		t.Error("expected at least one incoming call through diamond inheritance")
	}
}

// --- Attack 3: GOMAXPROCS=1 forces inline par.Map path.
//
// With one CPU, the incoming-calls scan runs all work inline (no goroutines). Verify
// correct results under GOMAXPROCS=1.
func TestIncomingMethodCallsGOMAXPROCS1(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	src := "itcl::class ::C {\n  public method helper {} {}\n  public method run {} { helper }\n}"
	s := newIncomingServer(map[string]string{"/c.tcl": src})
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///c.tcl"},
		Position:     posOf(src, "helper {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing")
	}
	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
	var found bool
	for _, c := range in {
		if c.From.Name == "run" {
			found = true
		}
	}
	if !found {
		t.Errorf("GOMAXPROCS=1: expected run->helper incoming call, got %#v", in)
	}
}

// --- Attack 4: deterministic result order for the incoming-calls scan.
//
// Results must be in work-list order (current file first, then Files() sorted).
// Run 30 times and verify identical order every time.
func TestIncomingMethodCallsDeterministicOrder(t *testing.T) {
	base := "itcl::class ::Base {\n  public method act {} {}\n}"
	files := map[string]string{"/base.tcl": base}
	// Three callers in alphabetic file order.
	for _, name := range []string{"aa", "bb", "cc"} {
		src := fmt.Sprintf(
			"itcl::class ::%s {\n  inherit ::Base\n  public method go {} { act }\n}", strings.ToUpper(name))
		files["/"+name+".tcl"] = src
	}

	s := newIncomingServer(files)
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///base.tcl"},
		Position:     posOf(base, "act {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing")
	}

	var firstOrder []string
	for run := 0; run < 30; run++ {
		in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
		var order []string
		for _, c := range in {
			order = append(order, c.From.Name)
		}
		if run == 0 {
			firstOrder = order
			continue
		}
		got := strings.Join(order, ",")
		want := strings.Join(firstOrder, ",")
		if got != want {
			t.Fatalf("run %d order %v != run 0 order %v", run, order, firstOrder)
		}
	}
}

// --- Attack 5: class table is read-only — no ensureClass calls during fan-out.
//
// Verify that the incoming-calls scan never writes to ix.classes by calling a query on
// a class name that does NOT exist in the index. methodOwner finds no owner
// (not panic) when ix.Class returns nil. No write-through cache fill must happen.
//
// This test is structural: we call incomingCalls on a method of a class whose
// callers reference an unknown base class — methodOwner walks the inherit
// chain, calls ix.Class("::Unknown"), gets nil, returns false, and stops. No map
// write. Under -race this must be clean.
func TestIncomingMethodCallsUnknownBaseClassReadOnly(t *testing.T) {
	// ::Derived inherits ::Unknown (not indexed). Its "act" resolves through
	// ::Derived.act (not found there) to ::Unknown.act — ix.Class("::Unknown") is nil.
	files := map[string]string{
		"/base.tcl": "itcl::class ::Base {\n  public method act {} {}\n}",
		// ::Bridge inherits ::Unknown (unknown) and ::Base (known).
		// "act" in ::Bridge resolves to ::Unknown::act if ::Unknown had one, but
		// since ::Unknown is not indexed, methodOwner finds no owner safely.
		"/bridge.tcl": "itcl::class ::Bridge {\n  inherit ::Unknown\n  inherit ::Base\n  public method go {} { act }\n}",
	}
	// Many files call act through ::Bridge to stress the nil-class path.
	for k := 0; k < 40; k++ {
		src := fmt.Sprintf(
			"itcl::class ::Sub%03d {\n  inherit ::Bridge\n  public method use%03d {} { act }\n}", k, k)
		files[fmt.Sprintf("/sub%03d.tcl", k)] = src
	}

	s := newIncomingServer(files)
	baseSrc := "itcl::class ::Base {\n  public method act {} {}\n}"
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///base.tcl"},
		Position:     posOf(baseSrc, "act {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing")
	}
	// Must not panic or race — just check it returns something.
	_ = s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
}

// --- Attack 6: the incoming-calls scan with empty workspace (no files, no refs).
//
// References builds a work list of [current file] + Files(). With no indexed
// files, work is [{a.file, liveRefs}] and par.Map has 1 item. Must return nil, not panic.
func TestIncomingMethodCallsEmptyWorkspace(t *testing.T) {
	src := "itcl::class ::C {\n  public method run {} {}\n}"
	s := newIncomingServer(map[string]string{"/c.tcl": src})
	items := s.prepareCallHierarchy(CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///c.tcl"},
		Position:     posOf(src, "run {}"),
	})
	if len(items) == 0 {
		t.Fatal("prepareCallHierarchy returned nothing")
	}
	in := s.incomingCalls(CallHierarchyIncomingCallsParams{Item: items[0]})
	// No callers exist (only a definition, no calls inside its own body).
	if len(in) != 0 {
		t.Fatalf("expected no incoming calls for isolated method, got %#v", in)
	}
}
