package tcl

// Attack tests for the substRefs fix (refs.go): after the fix, substRefs now
// calls childBodies on every command it finds inside a [substitution], to
// descend braced script bodies that walkAll would never reach.
//
// Attack vectors:
//   1. Double-counting: a ref that CommandRefs AND childBodies both walk.
//   2. Wrong byte offsets: refs must slice back to the exact name in the source.
//   3. Termination / stack depth for deeply nested substitutions.
//   4. Parity: the same call found at top-level must produce the same count when
//      wrapped in a [substitution].
//   5. Frame/namespace propagation through the new recursion path.
//   6. Interaction with for/while/if/try/dict-for/foreach/lmap inside [...].
//   7. No new spurious refs in ordinary (non-substitution) code.

import (
	"strings"
	"testing"
)

// countRefs returns how many ContextRefs in refs have Kind==RefCommand and
// Name==name. Used throughout to check exact multiplicity.
func countCommandRefs(refs []ContextRef, name string) int {
	n := 0
	for _, r := range refs {
		if r.Ref.Kind == RefCommand && r.Ref.Name == name {
			n++
		}
	}
	return n
}

func countVarRefs(refs []ContextRef, name string) int {
	n := 0
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == name {
			n++
		}
	}
	return n
}

// findCommandRef returns the first ContextRef for a command with the given name,
// or nil if not found.
func findCommandRef(refs []ContextRef, name string) *ContextRef {
	for i := range refs {
		if refs[i].Ref.Kind == RefCommand && refs[i].Ref.Name == name {
			return &refs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ATTACK 1: double-counting guard for the for command's init body inside [...]
// ---------------------------------------------------------------------------

// for {set i 0} {$i < 10} {incr i} {work} inside a substitution.
// childBodies returns indices 1 (init), 3 (step), 4 (body) as script bodies.
// CommandRefs returns word 2 (test) via exprBracketRefs.
// NONE of those bodies are also processed by CommandRefs (braced, not expr).
// So "work", "incr", and "set" (in the init body) each appear exactly once.
func TestSubstScriptBody_ForInsideSubst_NoDuplicate(t *testing.T) {
	src := `set r [for {set i 0} {$i < 10} {incr i} {work}]`
	refs := FileRefs(src)

	// "work" is in the for body -- should appear exactly once.
	if n := countCommandRefs(refs, "work"); n != 1 {
		t.Errorf("want 1 ref to 'work', got %d; refs=%+v", n, refs)
	}
	// "incr" is in the for step body -- exactly once.
	if n := countCommandRefs(refs, "incr"); n != 1 {
		t.Errorf("want 1 ref to 'incr', got %d; refs=%+v", n, refs)
	}
	// "set" in the init body -- exactly once (the outer "set r [...]" is one more).
	// Total "set" refs: outer set + inner set in for init = 2.
	if n := countCommandRefs(refs, "set"); n != 2 {
		t.Errorf("want 2 refs to 'set' (outer + for-init), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 2: double-counting guard for if-inside-substitution
// An [if {...} {body}] inside a larger expression.
// The condition is in exprWord (CommandRefs handles it), the body is in
// childBodies. Anything in the condition [subst] must appear exactly once.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_IfInsideSubst_NoDuplicate(t *testing.T) {
	// check is in the condition (exprBracketRefs path), body is in the body (childBodies).
	src := `set x [if {[check $v]} {myproc} else {altproc}]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "check"); n != 1 {
		t.Errorf("want 1 ref to 'check' (in if condition), got %d; refs=%+v", n, refs)
	}
	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (in if then-body), got %d; refs=%+v", n, refs)
	}
	if n := countCommandRefs(refs, "altproc"); n != 1 {
		t.Errorf("want 1 ref to 'altproc' (in if else-body), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 3: double-counting guard for while-inside-substitution
// while {[cond]} {body}: condition is exprWord, body is childBodies.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_WhileInsideSubst_NoDuplicate(t *testing.T) {
	src := `set x [while {[check]} {work}]`
	refs := FileRefs(src)

	// "check" is in the while test (expr) -- exactly once via exprBracketRefs.
	if n := countCommandRefs(refs, "check"); n != 1 {
		t.Errorf("want 1 ref to 'check' (while condition), got %d; refs=%+v", n, refs)
	}
	// "work" is in the while body (script) -- exactly once via childBodies.
	if n := countCommandRefs(refs, "work"); n != 1 {
		t.Errorf("want 1 ref to 'work' (while body), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 4: exact byte offset for a call inside [catch {proc_name} err]
// The fix must produce an offset that slices back to the exact name.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_Offset_CatchBody(t *testing.T) {
	//          0         1         2
	//          0123456789012345678901234567
	src := `set x [catch {myproc} err]`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found; refs=%+v", refs)
	}
	slice := src[ref.Ref.Start:ref.Ref.End]
	if slice != "myproc" {
		t.Errorf("offset slice = %q, want %q; Start=%d End=%d",
			slice, "myproc", ref.Ref.Start, ref.Ref.End)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 5: exact byte offsets at a deep nesting level
// [catch {[catch {myproc} err]} outer] — myproc is two levels deep.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_Offset_DoubleCatch(t *testing.T) {
	src := `set x [catch {[catch {myproc} err]} outer]`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found; refs=%+v", refs)
	}
	slice := src[ref.Ref.Start:ref.Ref.End]
	if slice != "myproc" {
		t.Errorf("offset slice = %q, want %q; Start=%d End=%d",
			slice, "myproc", ref.Ref.Start, ref.Ref.End)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 6: termination / no stack overflow for deeply nested substitutions
// Each level strictly shrinks the input, so recursion must terminate.
// Test with 30 levels of [catch {…}] nesting.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_DeepNesting_Terminates(t *testing.T) {
	const depth = 30
	// Build: set x [catch {[catch {[catch ... {leaf} ...]}]}]
	inner := "leaf"
	for i := 0; i < depth; i++ {
		inner = "[catch {" + inner + "} err]"
	}
	src := "set x " + inner

	// Must not panic or time out; just verify leaf is found.
	refs := FileRefs(src)
	if countCommandRefs(refs, "leaf") == 0 {
		t.Errorf("leaf not found after %d levels of nesting; total refs=%d", depth, len(refs))
	}
}

// ---------------------------------------------------------------------------
// ATTACK 7: parity — same ref count whether the call is in a top-level body
// or an identical body wrapped in a [substitution].
// The fix claim: call in [catch {myproc}] == call in top-level catch {myproc}.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_Parity_CatchTopLevelVsSubst(t *testing.T) {
	// Top-level catch body (walkAll path).
	topLevel := `catch {myproc} err`
	nTop := countCommandRefs(FileRefs(topLevel), "myproc")

	// Inside a substitution (substRefs path, the fixed code).
	inSubst := `set x [catch {myproc} err]`
	nSubst := countCommandRefs(FileRefs(inSubst), "myproc")

	if nTop != 1 {
		t.Errorf("top-level catch: want 1 ref to myproc, got %d", nTop)
	}
	if nSubst != 1 {
		t.Errorf("subst catch: want 1 ref to myproc, got %d", nSubst)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 8: no spurious refs injected into ordinary (non-substitution) code
// A plain proc body with a catch block should not now have refs counted twice.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_NoRegression_PlainProcBody(t *testing.T) {
	src := `proc p {} {
    catch {myproc} err
    myproc
}`
	refs := FileRefs(src)
	// "myproc" should appear exactly twice: once in catch body, once bare.
	if n := countCommandRefs(refs, "myproc"); n != 2 {
		t.Errorf("want 2 refs to myproc in proc body, got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 9: no spurious refs from ordinary control-flow at top level
// foreach/lmap/dict-for bodies that were already found by walkAll must not
// now get double-counted because substRefs also descends them.
// (substRefs is only called from within [...] spans; top-level commands are
// handled by walkAll, not substRefs, so no double-count should occur.)
// ---------------------------------------------------------------------------

func TestSubstScriptBody_NoRegression_ControlFlowTopLevel(t *testing.T) {
	src := `foreach x {1 2 3} { myproc }
lmap y {a b} { myproc }
dict for {k v} $d { myproc }
while {$go} { myproc }
if {$c} { myproc }
for {set i 0} {$i < 3} {incr i} { myproc }`
	refs := FileRefs(src)
	// Each control-flow body contributes exactly 1 ref to myproc.
	if n := countCommandRefs(refs, "myproc"); n != 6 {
		t.Errorf("want 6 myproc refs (one per control-flow body), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 10: try/on/trap/finally inside [...]
// The try bodies must be found now that substRefs descends childBodies.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_TryInsideSubst(t *testing.T) {
	src := `set x [try {myproc} on error {e o} {recover} finally {cleanup}]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (try body), got %d; refs=%+v", n, refs)
	}
	if n := countCommandRefs(refs, "recover"); n != 1 {
		t.Errorf("want 1 ref to 'recover' (on-error body), got %d; refs=%+v", n, refs)
	}
	if n := countCommandRefs(refs, "cleanup"); n != 1 {
		t.Errorf("want 1 ref to 'cleanup' (finally body), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 11: namespace eval inside [...] — does the namespace body get walked?
// (This tests whether childBodies' proc/namespace cases are hit inside substRefs.)
// ---------------------------------------------------------------------------

func TestSubstScriptBody_NamespaceEvalInsideSubst(t *testing.T) {
	src := `set x [namespace eval ::ns { myproc }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (namespace eval body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 12: proc inside [...] — proc body descended by substRefs/childBodies.
// [proc p {} {myproc}] — body contains myproc; should be found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ProcInsideSubst(t *testing.T) {
	src := `set x [proc p {} {myproc}]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (proc body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 13: custom command with trailing braced body inside [...]
// scriptBodies' default rule: last braced arg is a script.
// [with_lock { myproc }] should find myproc.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_CustomCommandInsideSubst(t *testing.T) {
	src := `set x [with_lock { myproc }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (custom-command body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 14: proc inside [catch {proc p {} {myproc}}] — proc body two levels
// deep. Tests that childBodies recursion inside substRefs itself recurses.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ProcBodyInsideCatchInsideSubst(t *testing.T) {
	src := `set x [catch {proc p {} {myproc}} err]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (proc body in catch in subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 15: offset verification for the user's original repro
// The myproc reference must slice back correctly in the full complex string.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_UserRepro_OffsetCorrect(t *testing.T) {
	src := `if {![catch {set v "[lindex [myproc $a 0 0 $b] 0]"} err]} {
  puts done
}`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found; refs=%+v", refs)
	}
	slice := src[ref.Ref.Start:ref.Ref.End]
	if slice != "myproc" {
		t.Errorf("offset slice = %q, want %q; Start=%d End=%d",
			slice, "myproc", ref.Ref.Start, ref.Ref.End)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 16: double-counting when the SAME call appears both in a condition
// [subst] AND in a braced body of a command inside [...]
// e.g. [if {[myproc]} {myproc}] — myproc appears TWICE, once in condition
// (exprBracketRefs path), once in body (childBodies path). Expect count = 2.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_SameNameInConditionAndBody(t *testing.T) {
	src := `set x [if {[myproc]} {myproc}]`
	refs := FileRefs(src)

	// Two distinct occurrences: condition substitution + body.
	if n := countCommandRefs(refs, "myproc"); n != 2 {
		t.Errorf("want 2 refs to 'myproc' (condition subst + body), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 17: foreach/lmap inside [...] — body is found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ForeachInsideSubst(t *testing.T) {
	src := `set x [foreach item $list { myproc $item }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (foreach body inside subst), got %d; refs=%+v", n, refs)
	}
}

func TestSubstScriptBody_LmapInsideSubst(t *testing.T) {
	src := `set result [lmap item $list { myproc $item }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (lmap body inside subst), got %d; refs=%+v", n, refs)
	}
}

func TestSubstScriptBody_DictForInsideSubst(t *testing.T) {
	src := `set result [dict for {k v} $d { myproc $k $v }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (dict for body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 18: expr {[myproc]} inside a [subst] — scriptBodies returns nil for
// an expr head (exprHead); exprBodies handles it. The call
// inside expr's bracketed substitution must be found exactly once.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ExprInsideSubst_NoDuplicate(t *testing.T) {
	src := `set x [expr {[myproc] + 1}]`
	refs := FileRefs(src)

	// exprBracketRefs handles {[myproc] + 1}, finding myproc via substRefs.
	// childBodies for expr returns nil (exprHead). So exactly once.
	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (expr bracket inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 19: large flat body inside [catch {...}] — performance smoke test.
// 1000 distinct calls in a single catch body should all be found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_LargeBody_AllFound(t *testing.T) {
	const n = 1000
	var sb strings.Builder
	sb.WriteString("set x [catch {\n")
	for i := 0; i < n; i++ {
		sb.WriteString("    proc_")
		sb.WriteString(strings.Repeat("x", 1)) // just to make unique names unnecessary; test count
		sb.WriteString("\n")
	}
	sb.WriteString("} err]")
	src := sb.String()
	refs := FileRefs(src)
	// All 1000 "proc_x" calls should be found. Name is the same, so count = n.
	if count := countCommandRefs(refs, "proc_x"); count != n {
		t.Errorf("want %d refs to 'proc_x' in large catch body, got %d", n, count)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 20: namespace eval + catch combination inside [...] verifies
// namespace context propagation through the substRefs recursion path.
// The call inside the catch body inside the namespace eval inside [...]
// should still be found. (The fix hardcodes "::" / FrameNamespace in the
// childBodies call within substRefs; verify it doesn't break finding refs.)
// ---------------------------------------------------------------------------

func TestSubstScriptBody_NamespaceEvalCatchNested(t *testing.T) {
	src := `set x [namespace eval ::foo { catch {myproc} err }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (catch in namespace eval in subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 21: the namespace context of refs found via the new substRefs path.
// A call in [catch {myproc}] inside a namespace eval body should be tagged
// with the enclosing namespace (::app), NOT :: from the hardcoded substRefs
// childBodies call. This verifies the enclosing context tagging is correct.
// Note: substRefs returns []Reference (not []ContextRef); the ContextRef
// tagging happens at the walkAll call site (which has ::app). So the ref
// found inside the [subst] inside the namespace eval body should inherit ::app.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_NamespaceContext_Encloses(t *testing.T) {
	src := `namespace eval ::app {
    set x [catch {myproc} err]
}`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found; refs=%+v", refs)
	}
	// The [catch {myproc} err] appears inside ::app's namespace eval body.
	// substRefs is called from CommandRefs inside walkAll, which tags the ref with
	// the enclosing context. But wait — this is via substRefs inside CommandRefs
	// inside walkAll, so the tag should be ::app.
	//
	// HOWEVER: the fix adds a SECOND recursion path (childBodies inside substRefs).
	// The catch body "myproc" is found via substRefs->childBodies->substRefs.
	// Those refs are returned as []Reference and tagged by the walkAll caller
	// with its context (::app). So myproc.Namespace should be ::app.
	if ref.Namespace != "::app" {
		t.Errorf("myproc namespace = %q, want ::app; ref=%+v", ref.Namespace, ref)
	}
	if ref.Frame != FrameNamespace {
		t.Errorf("myproc frame = %d, want FrameNamespace; ref=%+v", ref.Frame, ref)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 22: proc body context inside [...]
// A call in [catch {myproc}] inside a proc body should be tagged FrameProc.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ProcContext_Encloses(t *testing.T) {
	src := `proc p {} {
    set x [catch {myproc} err]
}`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found in proc body; refs=%+v", refs)
	}
	if ref.Frame != FrameProc {
		t.Errorf("myproc frame = %d, want FrameProc; ref=%+v", ref.Frame, ref)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 23: catch inside an if-body inside a proc, inside a namespace eval
// Verifies correct namespace propagation through multiple levels.
// namespace eval ::foo { proc p {} { if {$c} { set x [catch {myproc} err] } } }
// myproc should be found, tagged with ::foo and FrameProc.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_DeepContext_NamespaceAndProc(t *testing.T) {
	src := `namespace eval ::foo {
    proc p {} {
        if {$c} {
            set x [catch {myproc} err]
        }
    }
}`
	refs := FileRefs(src)

	ref := findCommandRef(refs, "myproc")
	if ref == nil {
		t.Fatalf("myproc not found; refs=%+v", refs)
	}
	if ref.Namespace != "::foo" {
		t.Errorf("myproc namespace = %q, want ::foo", ref.Namespace)
	}
	if ref.Frame != FrameProc {
		t.Errorf("myproc frame = %d, want FrameProc", ref.Frame)
	}
	// Offset must slice back correctly too.
	slice := src[ref.Ref.Start:ref.Ref.End]
	if slice != "myproc" {
		t.Errorf("offset slice = %q, want myproc", slice)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 24: variable refs inside a braced catch body inside [...]
// $var inside [catch {set x $myvar} err] must be found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_VarRefInsideCatchInsideSubst(t *testing.T) {
	src := `set r [catch {set x $myvar} err]`
	refs := FileRefs(src)

	if n := countVarRefs(refs, "myvar"); n != 1 {
		t.Errorf("want 1 ref to $myvar (catch body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 25: the for command's test body is an EXPR (handled by exprBracketRefs
// in CommandRefs), NOT a script. A bare name inside {$i < 10} must NOT be a
// command ref. Verify the expr-body exclusion still holds inside [for ...].
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ForTestIsExprNotScript(t *testing.T) {
	// "total" appears as a bareword operand inside the for test expr {total < 10}.
	// It must NOT be reported as a command ref — braced exprs don't invoke bare names.
	src := `set x [for {set i 0} {total < 10} {incr i} {}]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "total"); n != 0 {
		t.Errorf("bare operand 'total' in for test expr must not be a command ref, got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 26: itcl class body inside [...] — itcl::class in a substitution.
// The class body should be descended and method calls found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ItclClassInsideSubst(t *testing.T) {
	// Unusual but syntactically parseable.
	src := `set x [itcl::class MyClass { method m {} { myproc } }]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "myproc"); n != 1 {
		t.Errorf("want 1 ref to 'myproc' (method body inside itcl::class inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 27: verify the fix didn't add double-counting for the "for" command's
// start body when it appears inside a [subst]. The start body {set i 0}
// contains "set" — childBodies returns it as a script, but CommandRefs returns
// nil for braced non-expr words. Exact count: 1.
// Also, the "set" in the for-start body should produce a RefCommand for "set".
// ---------------------------------------------------------------------------

func TestSubstScriptBody_ForStartBodyCommandFound(t *testing.T) {
	// [for {innerproc} {0} {incr i} {}] where innerproc is the command in the init body.
	src := `set x [for {innerproc} {0} {incr i} {}]`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "innerproc"); n != 1 {
		t.Errorf("want 1 ref to 'innerproc' (for init body inside subst), got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 28: multiple substitution bodies in a single word (quoted string)
// "text [cmd1 {body1}] more [cmd2 {body2}]" as an argument
// Both body1 and body2 must be found.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_MultipleSubstsInOneWord(t *testing.T) {
	src := `puts "[catch {proc1} e] and [catch {proc2} e]"`
	refs := FileRefs(src)

	if n := countCommandRefs(refs, "proc1"); n != 1 {
		t.Errorf("want 1 ref to 'proc1', got %d; refs=%+v", n, refs)
	}
	if n := countCommandRefs(refs, "proc2"); n != 1 {
		t.Errorf("want 1 ref to 'proc2', got %d; refs=%+v", n, refs)
	}
}

// ---------------------------------------------------------------------------
// ATTACK 29: a call at top level with catch bodies must not regress.
// This specifically checks the walkAll path (childBodies called from walkAll)
// was not broken by the substRefs change. Previously passing test re-expressed
// for clarity.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_TopLevelCatch_Unaffected(t *testing.T) {
	src := `catch {alpha} e
if {$c} { beta }
while {$go} { gamma }`
	refs := FileRefs(src)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if n := countCommandRefs(refs, name); n != 1 {
			t.Errorf("want 1 ref to %q at top level, got %d", name, n)
		}
	}
}

// ---------------------------------------------------------------------------
// ATTACK 30: the try command with multiple on/trap/finally handlers
// inside a [subst] — all handler bodies must be found, no double-counting.
// ---------------------------------------------------------------------------

func TestSubstScriptBody_TryMultipleHandlersInsideSubst(t *testing.T) {
	src := `set x [try {
    risky
} on ok {} {
    onok
} on error {e o} {
    onerror
} trap {POSIX ENOENT} {e o} {
    trapped
} finally {
    finalize
}]`
	refs := FileRefs(src)

	for _, name := range []string{"risky", "onok", "onerror", "trapped", "finalize"} {
		if n := countCommandRefs(refs, name); n != 1 {
			t.Errorf("want 1 ref to %q (try handlers inside subst), got %d; refs=%+v", name, n, refs)
		}
	}
}
