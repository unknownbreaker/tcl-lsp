package tcl

import (
	"reflect"
	"testing"
)

func findVar(refs []ContextRef, name string) *ContextRef {
	for i := range refs {
		if refs[i].Ref.Kind == RefVariable && refs[i].Ref.Name == name {
			return &refs[i]
		}
	}
	return nil
}

func TestFileRefsFlatGlobal(t *testing.T) {
	got := FileRefs("set x $y")
	want := []ContextRef{
		{Ref: Reference{Kind: RefCommand, Name: "set", Start: 0, End: 3}, Namespace: "::", Frame: FrameNamespace},
		{Ref: Reference{Kind: RefVariable, Name: "y", Start: 6, End: 8}, Namespace: "::", Frame: FrameNamespace},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFileRefsNamespaceEval(t *testing.T) {
	src := "namespace eval ::app {\n    set v $w\n}"
	got := FileRefs(src)
	vw := findVar(got, "w")
	if vw == nil {
		t.Fatalf("did not find var w in %#v", got)
	}
	if vw.Namespace != "::app" {
		t.Fatalf("namespace = %q, want ::app", vw.Namespace)
	}
	if vw.Frame != FrameNamespace {
		t.Fatalf("frame = %d, want FrameNamespace", vw.Frame)
	}
	if src[vw.Ref.Start:vw.Ref.End] != "$w" {
		t.Fatalf("offset slice = %q, want $w", src[vw.Ref.Start:vw.Ref.End])
	}
}

func TestFileRefsNestedNamespace(t *testing.T) {
	src := "namespace eval ::a { namespace eval b { set x $y } }"
	got := FileRefs(src)
	vy := findVar(got, "y")
	if vy == nil {
		t.Fatalf("did not find var y in %#v", got)
	}
	if vy.Namespace != "::a::b" {
		t.Fatalf("namespace = %q, want ::a::b", vy.Namespace)
	}
}

func TestFileRefsProcBody(t *testing.T) {
	src := "proc p {} {\n    set a $b\n}"
	got := FileRefs(src)
	vb := findVar(got, "b")
	if vb == nil {
		t.Fatalf("did not find var b in %#v", got)
	}
	if vb.Frame != FrameProc {
		t.Fatalf("frame = %d, want FrameProc", vb.Frame)
	}
	if vb.Namespace != "::" {
		t.Fatalf("namespace = %q, want ::", vb.Namespace)
	}
}

func TestFileRefsProcInNamespace(t *testing.T) {
	src := "namespace eval ::app {\n  proc f {} { set a $b }\n}"
	got := FileRefs(src)
	vb := findVar(got, "b")
	if vb == nil {
		t.Fatalf("did not find var b in %#v", got)
	}
	if vb.Frame != FrameProc {
		t.Fatalf("frame = %d, want FrameProc", vb.Frame)
	}
	if vb.Namespace != "::app" {
		t.Fatalf("namespace = %q, want ::app", vb.Namespace)
	}
}

func TestFileRefsCombinedAndOffsets(t *testing.T) {
	src := "namespace eval ::app {\n  variable base 10\n  proc scale {n} {\n    return [expr {$n * $base}]\n  }\n}"
	got := FileRefs(src)

	// $n is used inside proc scale's body, in namespace ::app, proc frame.
	// (It appears outside the braced expr arg? No: it is inside {$n * $base},
	// which is braced, so per the expr-braced limitation it is NOT found.)
	// Instead, assert the command refs we DO expect, and offset fidelity for one.

	// The `expr` command ref is inside proc scale's body (namespace ::app, proc frame).
	var exprRef *ContextRef
	for i := range got {
		if got[i].Ref.Kind == RefCommand && got[i].Ref.Name == "expr" {
			exprRef = &got[i]
		}
	}
	if exprRef == nil {
		t.Fatalf("did not find expr command in %#v", got)
	}
	if exprRef.Namespace != "::app" || exprRef.Frame != FrameProc {
		t.Fatalf("expr context = (%q, %d), want (::app, FrameProc)", exprRef.Namespace, exprRef.Frame)
	}
	if src[exprRef.Ref.Start:exprRef.Ref.End] != "expr" {
		t.Fatalf("expr offset slice = %q", src[exprRef.Ref.Start:exprRef.Ref.End])
	}

	// Guard the expr-braced limitation: vars inside the braced expr arg are not found.
	if findVar(got, "n") != nil || findVar(got, "base") != nil {
		t.Fatalf("did not expect expr-braced vars n/base to be found: %#v", got)
	}
}

func TestFileRefsControlFlowBodies(t *testing.T) {
	// A proc called inside control-flow bodies must be found. These bodies run in
	// the enclosing frame/namespace, not a new scope, so refs inside them count.
	src := "proc helper {} {}\n" +
		"foreach x {1 2} { helper }\n" +
		"lmap y {1 2} { helper }\n" +
		"dict for {k v} $d { helper }\n" +
		"while {$go} { helper }\n" +
		"if {$c} { helper } else { helper }\n" +
		"for {set i 0} {$i < 3} {incr i} { helper }\n" +
		"catch { helper }"
	n := 0
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefCommand && r.Ref.Name == "helper" {
			n++
		}
	}
	if n != 8 {
		t.Fatalf("expected 8 helper call refs inside control-flow bodies, got %d", n)
	}
}

func TestFileRefsScriptBlockArgument(t *testing.T) {
	// A proc call inside a braced block passed as an argument to another
	// (user-defined) command must be found -- custom control structures, test
	// harnesses, DSLs all do this. The trailing braced arg is taken as a script.
	src := "proc helper {} {}\n" +
		"with_lock {\n" +
		"    helper\n" +
		"    helper\n" +
		"}\n" +
		"my_each x $items {\n" +
		"    helper\n" +
		"}"
	n := 0
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefCommand && r.Ref.Name == "helper" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("expected 3 helper calls inside script-block arguments, got %d", n)
	}
}

func TestFileRefsSwitchArms(t *testing.T) {
	// switch's arm list holds (pattern, body) pairs: each body is walked as a
	// script, and no pattern is read as a command.
	src := "proc helper {} {}\n" +
		"switch $x {\n" +
		"    a { helper }\n" +
		"    b { helper }\n" +
		"    default { helper }\n" +
		"}"
	n := 0
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefCommand && r.Ref.Name == "helper" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("expected 3 helper calls in switch arms, got %d", n)
	}
}

func TestFileRefsExprBraceNotScript(t *testing.T) {
	// Guard the heuristic's exclusion: expr's braced argument is an expression,
	// not a script, so a name in it must not be reported as a command call.
	src := "proc total {} {}\nset n [expr {total + 1}]"
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefCommand && r.Ref.Name == "total" {
			t.Fatalf("expr-braced name must not be a command ref: %#v", r)
		}
	}
}

func TestFileRefsCallInsideExprBraces(t *testing.T) {
	// expr evaluates [command substitutions] inside its braces even though braces
	// otherwise suppress substitution, so a proc called there must be found. A
	// bare occurrence of the same name as an operand must NOT be a command ref --
	// only the bracketed call counts.
	src := "proc area {r} {}\nset s [expr {area + [area $r]}]"
	n := 0
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefCommand && r.Ref.Name == "area" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 area command ref (the bracketed call), got %d", n)
	}
}

func TestFileRefsLocalScopeMatchesDef(t *testing.T) {
	src := "proc f {} {\n  set y 1\n  puts $y\n}"
	defs := FileDefs(src)
	ys := defsNamed(defs, "y")
	if len(ys) != 1 {
		t.Fatalf("want 1 def y, got %#v", defs)
	}
	vy := findVar(FileRefs(src), "y")
	if vy == nil || vy.Scope == 0 || vy.Scope != ys[0].Scope {
		t.Fatalf("$y ref scope %#v vs def scope %d", vy, ys[0].Scope)
	}
}

func TestFileRefsNamespaceInsideProc(t *testing.T) {
	// Inverse nesting: namespace eval inside a proc body must RESET to
	// FrameNamespace (not inherit FrameProc) and use the inner namespace.
	src := "proc p {} { namespace eval ::sub { set x $y } }"
	got := FileRefs(src)
	vy := findVar(got, "y")
	if vy == nil {
		t.Fatalf("did not find var y in %#v", got)
	}
	if vy.Namespace != "::sub" {
		t.Fatalf("namespace = %q, want ::sub", vy.Namespace)
	}
	if vy.Frame != FrameNamespace {
		t.Fatalf("frame = %d, want FrameNamespace", vy.Frame)
	}
}
