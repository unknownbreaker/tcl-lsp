package tcl

import (
	"slices"
	"strings"
	"testing"
)

// The body of a command the server does not know has an unknown frame: it may be a
// proc-like scope (a TclOO method, tsp::proc) or code running right here. A bare
// name assigned there defines nothing, neither a namespace variable nor a local.
// An absolute name, a proc, and any scope that sets its own frame still define.
func TestOpaqueFrameDefs(t *testing.T) {
	cases := []struct {
		src, name string
		want      int
	}{
		{"oo::class create X { method m {} { set x 1 } }", "::x", 0},
		{"oo::class create X { method m {} { set x 1 } }", "x", 0}, // no fake local either
		{"oo::class create X { variable v }", "::v", 0},
		{"tsp::proc f {a} { set b 1 }", "::b", 0},
		{"tsp::proc f {a} { set b 1 }", "b", 0},
		{"with { if {1} { set x 1 } }", "::x", 0}, // if inside opaque stays opaque
		{"namespace eval ::n { with { set x 1 } }", "::n::x", 0},
		{"if {1} { set x 1 }", "::x", 1},
		{"eval { set x 1 }", "::x", 1},
		{"foreach i $l { set x 1 }", "::x", 1},
		{"namespace eval ::n { if {1} { set x 1 } }", "::n::x", 1},
		{"with { set ::x 1 }", "::x", 1},
		{"with { variable ::y }", "::y", 1},
		{"with { proc g {} {} }", "::g", 1},
		// Scopes that set their own frame reset it inside an opaque body.
		{"with { namespace eval ::n { set v 1 } }", "::n::v", 1},
	}
	for _, c := range cases {
		t.Run(c.src+" defines "+c.name, func(t *testing.T) {
			defs := FileDefs(c.src)
			if got := len(defsNamed(defs, c.name)); got != c.want {
				t.Errorf("definitions named %q in %q = %d, want %d\ndefs: %#v", c.name, c.src, got, c.want, defs)
			}
		})
	}
}

// isVariableBinding reports the definition kinds that bind a variable name. A body
// whose frame is unknown must produce none of them for a bare name.
func isVariableBinding(kind DefKind) bool {
	switch kind {
	case DefNamespaceVar, DefLocal, DefGlobalLink, DefIvar:
		return true
	}
	return false
}

// variableBindingNames returns the sorted names of every definition that binds a
// variable, so a test can say "nothing binds a variable here" in one comparison.
func variableBindingNames(defs []Definition) []string {
	var names []string
	for _, def := range defs {
		if isVariableBinding(def.Kind) {
			names = append(names, def.Name)
		}
	}
	slices.Sort(names)
	return names
}

// countDefs counts definitions with this exact name and kind.
func countDefs(defs []Definition, name string, kind DefKind) int {
	count := 0
	for _, def := range defs {
		if def.Name == name && def.Kind == kind {
			count++
		}
	}
	return count
}

// Every command that binds a name at namespace frame, run inside an unknown body.
// Bug hunted: an implementation that guards `set` but not `variable`, `array set`
// or an array-element target; one that tests the whole word for "::" instead of
// the base name; one that lets a binder fall into its proc-frame branch because the
// frame is "not namespace"; and promoteLinkedAssignments minting a global from a
// link that should never have been recorded.
func TestOpaqueFrameBindersDefineNothing(t *testing.T) {
	binders := []string{
		"set x 1",
		"set x(k) 1",
		"set x(a::b) 1", // "::" in the subscript does not qualify the base name
		"array set x {a 1}",
		"variable x",
		"variable x 1",
		"global x",
		"global x\nset x 2",
		"upvar 1 y x",
		"upvar #0 y x",
		"upvar #0 g x\nset x 1",
		"foreach i $l {}",
		"foreach {a b} $l {}",
		"lassign $l a b",
		"lmap v $l {}",
		"incr n",
		"append s v",
		"lappend l v",
		"dict for {k v} $d {}",
		"dict map {k v} $d {}",
	}
	contexts := []struct{ name, prefix, suffix string }{
		{"wrapper", "with {\n", "\n}"},
		{"TclOO method", "oo::class create X {\n  method m {} {\n", "\n  }\n}"},
		{"tsp::proc", "tsp::proc f {a} {\n", "\n}"},
		{"wrapper in namespace eval", "namespace eval ::n {\n  with {\n", "\n  }\n}"},
		{"wrapper in if in wrapper", "with {\n  if {1} {\n", "\n  }\n}"},
	}
	for _, context := range contexts {
		for _, binder := range binders {
			src := context.prefix + binder + context.suffix
			t.Run(context.name+"/"+strings.ReplaceAll(binder, "\n", "; "), func(t *testing.T) {
				if names := variableBindingNames(FileDefs(src)); len(names) != 0 {
					t.Errorf("an unknown body bound variables %v, want none\nsource:\n%s", names, src)
				}
			})
		}
	}
}

// An absolute name is the same variable in every frame, so it still defines in an
// opaque body, as a namespace variable and never as a local. A relative qualified
// name is not: it resolves from the namespace the body runs in, and a TclOO method
// runs in the object's namespace, not the lexical one. So it defines nothing there.
// Bug hunted: a guard that drops every assignment, one that counts the unqualified
// twin as well, and one that tests for "::" anywhere instead of a leading "::".
func TestOpaqueFrameQualifiedBindersStillDefine(t *testing.T) {
	cases := []struct {
		src, name string
		want      int
	}{
		{"with { set ::x 1 }", "::x", 1},
		{"with { set ::x(k) 1 }", "::x", 1},
		{"with { set ::a::b 1 }", "::a::b", 1},
		{"with { set a::b 1 }", "::a::b", 0},
		{"with { set a::b(k) 1 }", "::a::b", 0},
		{"namespace eval ::n { with { set a::b 1 } }", "::n::a::b", 0},
		{"namespace eval ::n { with { set ::x 1 } }", "::x", 1},
		{"namespace eval ::n { with { set ::x 1 } }", "::n::x", 0},
		{"with { array set ::cfg {a 1} }", "::cfg", 1},
		{"with { array set a::cfg {a 1} }", "::a::cfg", 0},
		{"with { variable a::b }", "::a::b", 0},
		{"with { variable ::y 1 }", "::y", 1},
		{"with { variable ::a::b }", "::a::b", 1},
		{"namespace eval ::n { with { variable a::b } }", "::n::a::b", 0},
		// A relative qualified name keeps its meaning at every other frame.
		{"set a::b 1", "::a::b", 1},
		{"variable a::b", "::a::b", 1},
		{"if {1} { set a::b 1 }", "::a::b", 1},
		{"namespace eval ::n { set a::b 1 }", "::n::a::b", 1},
		{"namespace eval ::n { variable a::b }", "::n::a::b", 1},
		{"namespace eval ::n { proc f {} { set a::b 1 } }", "::n::a::b", 1},
		{"proc f {} { with { set a::b 1 } }", "::a::b", 1},
		{"oo::class create X { method m {} { set ::g 1 } }", "::g", 1},
		{"tsp::proc f {} { set ::g 1 }", "::g", 1},
		// The qualified twin defines once; the bare one defines nothing.
		{"with { set ::x 1; set x 2 }", "::x", 1},
		{"with { set ::x 1; set x 2 }", "x", 0},
	}
	for _, c := range cases {
		t.Run(c.src+" defines "+c.name, func(t *testing.T) {
			defs := FileDefs(c.src)
			if got := countDefs(defs, c.name, DefNamespaceVar); got != c.want {
				t.Errorf("namespace variables named %q in %q = %d, want %d\ndefs: %#v", c.name, c.src, got, c.want, defs)
			}
			if got := countDefs(defs, c.name, DefLocal); got != 0 {
				t.Errorf("locals named %q in %q = %d, want 0 (a qualified name is never a local)", c.name, c.src, got)
			}
		})
	}
}

// Scopes that set their own frame still bind normally, wherever they sit inside an
// unknown body, and an unknown body inside them is opaque again. Bug hunted: an
// implementation that stamps the opaque frame onto everything under an unknown
// command, or that forgets to reset it at proc, namespace eval and itcl::class; and
// one that makes a wrapper inside a proc opaque instead of leaving the proc frame.
func TestOpaqueFrameScopesResetAndProcFrameIsUntouched(t *testing.T) {
	const anyKind = DefKind(-1)
	cases := []struct {
		name, src, defName string
		kind               DefKind
		want               int
	}{
		{"proc in wrapper: proc", "with { proc g {a} { set b 1 } }", "::g", DefProc, 1},
		{"proc in wrapper: param", "with { proc g {a} { set b 1 } }", "a", DefLocal, 1},
		{"proc in wrapper: local", "with { proc g {a} { set b 1 } }", "b", DefLocal, 1},
		{"proc in wrapper: no ns var", "with { proc g {a} { set b 1 } }", "::b", anyKind, 0},
		{"proc in if in wrapper: proc", "with { if {1} { proc g {a} { set b 1 } } }", "::g", DefProc, 1},
		{"proc in if in wrapper: local", "with { if {1} { proc g {a} { set b 1 } } }", "b", DefLocal, 1},
		{"proc in if in wrapper: no ns var", "with { if {1} { proc g {a} { set b 1 } } }", "::b", anyKind, 0},
		{"decorated proc in wrapper: proc", "with { CACHE proc g {a} { set b 1 } }", "::g", DefProc, 1},
		{"decorated proc in wrapper: local", "with { CACHE proc g {a} { set b 1 } }", "b", DefLocal, 1},
		{"qualified proc in wrapper", "with { proc ns::g {} {} }", "::ns::g", DefProc, 1},
		{"proc in wrapper in namespace", "namespace eval ::n { with { proc g {} {} } }", "::n::g", DefProc, 1},
		{"namespace eval in wrapper", "with { namespace eval ::n { proc g {} {} } }", "::n::g", DefProc, 1},
		{"relative namespace eval in wrapper", "with { namespace eval ::n { namespace eval m { set v 1 } } }", "::n::m::v", DefNamespaceVar, 1},
		{"wrapper in namespace eval in wrapper", "with { namespace eval ::n { with { set v 1 } } }", "::n::v", anyKind, 0},
		{"namespace eval in wrapper in namespace eval", "with { namespace eval ::n { with { namespace eval ::m { set v 1 } } } }", "::m::v", DefNamespaceVar, 1},
		{"wrapper in namespace eval in proc: no ns var", "proc f {} { namespace eval ::n { with { set x 1 } } }", "::n::x", anyKind, 0},
		{"wrapper in namespace eval in proc: no local", "proc f {} { namespace eval ::n { with { set x 1 } } }", "x", anyKind, 0},
		{"namespace eval in proc: control flow still namespace frame", "proc f {} { namespace eval ::n { if {1} { set x 1 } } }", "::n::x", DefNamespaceVar, 1},
		// A proc frame stays a proc frame: a wrapper there is not opaque.
		{"wrapper in proc: local", "proc f {} { with { set x 1 } }", "x", DefLocal, 1},
		{"wrapper in proc: no ns var", "proc f {} { with { set x 1 } }", "::x", anyKind, 0},
		{"if in wrapper in proc: local", "proc f {} { with { if {1} { set x 1 } } }", "x", DefLocal, 1},
		{"foreach var in wrapper in proc", "proc f {l} { with { foreach i $l {} } }", "i", DefLocal, 1},
		{"global in wrapper in proc", "proc f {} { with { global g } }", "g", DefGlobalLink, 1},
		{"wrapper in method: local", "itcl::class C { method m {} { with { set x 1 } } }", "x", DefLocal, 1},
		// Itcl keeps its own frames inside and outside an unknown body.
		{"itcl class in wrapper: class", "with { itcl::class C { variable v; method m {} { set z 1 } } }", "::C", DefClass, 1},
		{"itcl class in wrapper: ivar", "with { itcl::class C { variable v; method m {} { set z 1 } } }", "v", DefIvar, 1},
		{"itcl class in wrapper: no ns var", "with { itcl::class C { variable v; method m {} { set z 1 } } }", "::v", anyKind, 0},
		{"itcl class in wrapper: method", "with { itcl::class C { variable v; method m {} { set z 1 } } }", "m", DefMethod, 1},
		{"itcl class in wrapper: method local", "with { itcl::class C { variable v; method m {} { set z 1 } } }", "z", DefLocal, 1},
		{"itcl::body at top level: local", "itcl::class C { method m {} }\nitcl::body C::m {} { set z 1 }", "z", DefLocal, 1},
		{"itcl::body at top level: no ns var", "itcl::class C { method m {} }\nitcl::body C::m {} { set z 1 }", "::z", anyKind, 0},
		{"itcl::body in wrapper: local", "with { itcl::body C::m {} { set z 1 } }", "z", DefLocal, 1},
		{"public block: method", "itcl::class C { public { method m {} { set z 1 } } }", "m", DefMethod, 1},
		{"public block: local", "itcl::class C { public { method m {} { set z 1 } } }", "z", DefLocal, 1},
		{"public block: ivar", "itcl::class C { public { variable v } }", "v", DefIvar, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defs := FileDefs(c.src)
			got := 0
			if c.kind == anyKind {
				got = len(defsNamed(defs, c.defName))
			} else {
				got = countDefs(defs, c.defName, c.kind)
			}
			if got != c.want {
				t.Errorf("definitions named %q (kind %d) in %q = %d, want %d\ndefs: %#v", c.defName, c.kind, c.src, got, c.want, defs)
			}
		})
	}
}

// Controller rulings on which heads run their script right here. `eval` does only in
// its one-word form: `eval foo {script}` hands the script to `foo`, so it is opaque.
// `uplevel` runs its script in another frame, so it is never a pass-through.
// Bug hunted: an implementation that tests `eval` by name alone, that counts words
// with >= instead of ==, or that leaves `uplevel` in the pass-through list.
func TestOpaqueFrameEvalAndUplevelRulings(t *testing.T) {
	cases := []struct {
		src, name string
		want      int
	}{
		{"eval {set x 1}", "::x", 1},
		{"eval foo {set x 1}", "::x", 0},
		{"eval foo {set x 1}", "x", 0},
		{"eval foo bar {set x 1}", "::x", 0},
		{"uplevel {set x 1}", "::x", 0},
		{"uplevel {set x 1}", "x", 0},
		{"uplevel 1 {set x 1}", "::x", 0},
		{"uplevel #0 {set x 1}", "::x", 0},
		{"time {set x 1}", "::x", 1},
		{"timerate {set x 1}", "::x", 1},
		{"namespace eval ::n { eval {set x 1} }", "::n::x", 1},
		{"namespace eval ::n { eval foo {set x 1} }", "::n::x", 0},
		{"namespace eval ::n { uplevel {set x 1} }", "::n::x", 0},
		{"if {1} { eval foo {set x 1} }", "::x", 0},
		{"if {1} { uplevel {set x 1} }", "::x", 0},
		{"with { eval {set x 1} }", "::x", 0}, // pass-through inside an unknown body stays unknown
		{"eval { eval foo {set x 1} }", "::x", 0},
		// A qualified binding still defines through either.
		{"eval foo {set ::x 1}", "::x", 1},
		{"uplevel {set ::x 1}", "::x", 1},
	}
	for _, c := range cases {
		t.Run(c.src+" defines "+c.name, func(t *testing.T) {
			defs := FileDefs(c.src)
			if got := len(defsNamed(defs, c.name)); got != c.want {
				t.Errorf("definitions named %q in %q = %d, want %d\ndefs: %#v", c.name, c.src, got, c.want, defs)
			}
		})
	}
}

// Every control-flow head runs its bodies in the frame it sits in. At namespace
// frame the body binds a namespace variable; inside an unknown body it binds
// nothing. Bug hunted: a head missing from the pass-through list (so its body turns
// opaque at namespace level, silently dropping real definitions), and a head that
// resets the frame the other way (so a body inside an unknown body turns
// namespace-level again). Each snippet sets `x` in a body.
func TestOpaqueFrameControlFlowKeepsTheFrameItSitsIn(t *testing.T) {
	snippets := []string{
		"if {1} { set x 1 }",
		"if {1} { } else { set x 1 }",
		"if {1} { } elseif {0} { set x 1 }",
		"foreach i $l { set x 1 }",
		"foreach {a b} $l { set x 1 }",
		"while {1} { set x 1 }",
		"for {set i 0} {$i < 1} {incr i} { set x 1 }",
		"for {} {1} {} { set x 1 }",
		"catch { set x 1 }",
		"catch { set x 1 } err",
		"try { set x 1 } finally { }",
		"try { } finally { set x 1 }",
		"try { } on error {} { set x 1 }",
		"try { } trap {A} {} { set x 1 }",
		"switch $v { a { set x 1 } }",
		"switch -exact -- $v { a { set x 1 } default { } }",
		"switch $v a { set x 1 } default { }",
		"lmap i $l { set x 1 }",
		"dict for {k v} $d { set x 1 }",
		"dict map {k v} $d { set x 1 }",
		"time { set x 1 }",
		"timerate { set x 1 }",
		"eval { set x 1 }",
		"if {1} { foreach i $l { catch { switch $v { a { set x 1 } } } } }",
	}
	for _, snippet := range snippets {
		t.Run("namespace frame: "+snippet, func(t *testing.T) {
			defs := FileDefs(snippet)
			if got := countDefs(defs, "::x", DefNamespaceVar); got != 1 {
				t.Errorf("namespace variables named ::x in %q = %d, want 1\ndefs: %#v", snippet, got, defs)
			}
		})
		t.Run("in namespace eval: "+snippet, func(t *testing.T) {
			src := "namespace eval ::n {\n" + snippet + "\n}"
			defs := FileDefs(src)
			if got := countDefs(defs, "::n::x", DefNamespaceVar); got != 1 {
				t.Errorf("namespace variables named ::n::x in %q = %d, want 1\ndefs: %#v", src, got, defs)
			}
		})
		t.Run("unknown body: "+snippet, func(t *testing.T) {
			src := "with {\n" + snippet + "\n}"
			if names := variableBindingNames(FileDefs(src)); len(names) != 0 {
				t.Errorf("an unknown body bound variables %v, want none\nsource:\n%s", names, src)
			}
		})
		t.Run("in proc: "+snippet, func(t *testing.T) {
			src := "proc f {l v d} {\n" + snippet + "\n}"
			if got := countDefs(FileDefs(src), "x", DefLocal); got != 1 {
				t.Errorf("locals named x in %q = %d, want 1 (the proc frame is untouched)", src, got)
			}
		})
	}
}

// The outline of a real TclOO class: methods, constructors, and ivars-by-keyword
// make no variables, and a proc inside still appears with its own locals. Bug
// hunted: a class body that leaks `a`, `b` or `v` into the outline, and a guard
// so broad that the proc and its local vanish too.
func TestOpaqueFrameRealTclOOClassMakesNoBareVariables(t *testing.T) {
	src := `oo::class create X {
  variable v
  constructor {} {set a 1}
  method m {p} {set b 2}
  self method s {} {}
  proc helper {} { set c 3 }
}`
	defs := FileDefs(src)
	if names := variableBindingNames(defs); !slices.Equal(names, []string{"c"}) {
		t.Errorf("variable bindings in a TclOO class = %v, want only the proc's local [c]\ndefs: %#v", names, defs)
	}
	if got := countDefs(defs, "::helper", DefProc); got != 1 {
		t.Errorf("procs named ::helper = %d, want 1\ndefs: %#v", got, defs)
	}
}

// Other real unknown-body shapes: none may define a bare variable.
func TestOpaqueFrameRealWrapperShapesMakeNoBareVariables(t *testing.T) {
	cases := []string{
		"oo::define X method m {} { set a 1 }",
		"oo::define X { method m {} { set a 1 } }",
		"::oo::class create X { method m {} { set a 1 } }",
		"oo::class create X {\n  superclass Y\n  constructor {} {\n    next\n    set a 1\n  }\n}",
		"oo::class create ::app::X { method m {} { global g; set g 1; variable v; set v 2 } }",
		"tsp::proc f {a b} {\n  foreach i $a { lappend r $i }\n  set r\n}",
		"snit::type T {\n  variable v\n  method m {} { set a 1 }\n}",
		"with_lock $mutex {\n  set x 1\n}",
		"namespace inscope ::n {set x 1}",          // runs in another namespace, which is not tracked
		"foo::bar $a $b {\n  set x 1\n  incr y\n}", // a qualified unknown head
	}
	for _, src := range cases {
		t.Run(strings.ReplaceAll(src, "\n", " "), func(t *testing.T) {
			if names := variableBindingNames(FileDefs(src)); len(names) != 0 {
				t.Errorf("variable bindings = %v, want none\nsource:\n%s", names, src)
			}
		})
	}
}

// Calls and namespaces survive the new frame. Command references inside an unknown
// body are still reported with the namespace they sit in, and a bare variable use
// there is no longer tagged as a namespace-frame use. Bug hunted: an implementation
// that drops the references of an opaque body wholesale, that resets its namespace,
// or that leaves its variable uses tagged FrameNamespace.
func TestOpaqueFrameReferencesKeepCommandsAndNamespace(t *testing.T) {
	cases := []struct {
		name, src, command, namespace string
	}{
		{"wrapper", "with { helper }", "helper", "::"},
		{"wrapper in namespace eval", "namespace eval ::n { with { helper } }", "helper", "::n"},
		{"TclOO method", "oo::class create X { method m {} { helper } }", "helper", "::"},
		{"TclOO method in namespace eval", "namespace eval ::n { oo::class create X { method m {} { helper } } }", "helper", "::n"},
		{"tsp::proc", "tsp::proc f {a} { helper }", "helper", "::"},
		{"if in wrapper", "with { if {1} { helper } }", "helper", "::"},
		{"qualified", "with { ::a::helper }", "::a::helper", "::"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := false
			for _, ref := range FileRefs(c.src) {
				if ref.Ref.Kind == RefCommand && ref.Ref.Name == c.command {
					found = true
					if ref.Namespace != c.namespace {
						t.Errorf("command ref %q has namespace %q, want %q", c.command, ref.Namespace, c.namespace)
					}
				}
			}
			if !found {
				t.Errorf("no command reference to %q in %q", c.command, c.src)
			}
		})
	}

	// A bare use in an unknown body is not a namespace-frame use; a qualified one is
	// still reported, and a use in an ordinary control-flow body still is one.
	bareUse := func(src string) *ContextRef {
		for _, ref := range FileRefs(src) {
			if ref.Ref.Kind == RefVariable && ref.Ref.Name == "x" {
				return &ref
			}
		}
		return nil
	}
	if ref := bareUse("with { puts $x }"); ref == nil || ref.Frame == FrameNamespace {
		t.Errorf("bare $x in an unknown body = %#v, want a reference whose frame is not FrameNamespace", ref)
	}
	if ref := bareUse("oo::class create X { method m {} { puts $x } }"); ref == nil || ref.Frame == FrameNamespace || ref.Frame == FrameProc {
		t.Errorf("bare $x in a TclOO method = %#v, want a reference that is neither namespace nor proc frame", ref)
	}
	if ref := bareUse("if {1} { puts $x }"); ref == nil || ref.Frame != FrameNamespace {
		t.Errorf("bare $x in a namespace-level if = %#v, want FrameNamespace", ref)
	}
	if ref := bareUse("proc f {} { with { puts $x } }"); ref == nil || ref.Frame != FrameProc {
		t.Errorf("bare $x in a wrapper inside a proc = %#v, want FrameProc", ref)
	}
	qualified := false
	for _, ref := range FileRefs("with { puts $::x }") {
		if ref.Ref.Kind == RefVariable && ref.Ref.Name == "::x" {
			qualified = true
		}
	}
	if !qualified {
		t.Errorf("no reference to $::x in an unknown body")
	}
}

// A variable in a proc or class body inside a bracket keeps its reference only when
// the tag it gets from the enclosing command cannot change its meaning. Bug hunted: a
// drop that takes absolute names too, in any spelling, one that keeps relative
// qualified names because they contain "::", and one that takes the calls with it.
func TestSubstRefsKeepOnlyWhatTheTagCannotChange(t *testing.T) {
	bodies := []struct{ name, open, close string }{
		{"proc", "if [catch {proc g {} {", "}}] {}"},
		{"class method", "if [catch {itcl::class X {method m {} {", "}}}] {}"},
		{"proc, nested if", "if [catch {proc g {} {if {1} {", "}}}] {}"},
		{"proc, nested bracket", "if [catch {proc g {} {set r [string length ", "]}}] {}"},
	}
	cases := []struct {
		name, use, refName string
		want               bool
	}{
		{"absolute", "$::x", "::x", true},
		{"absolute braced", "${::x}", "::x", true},
		{"absolute nested", "$::a::b", "::a::b", true},
		{"relative qualified", "$a::b", "a::b", false},
		{"bare", "$y", "y", false},
	}
	for _, body := range bodies {
		for _, c := range cases {
			t.Run(body.name+"/"+c.name, func(t *testing.T) {
				src := body.open + c.use + body.close
				found := false
				for _, ref := range FileRefs(src) {
					found = found || (ref.Ref.Kind == RefVariable && ref.Ref.Name == c.refName)
				}
				if found != c.want {
					t.Errorf("variable ref %q in %q: found = %v, want %v", c.refName, src, found, c.want)
				}
			})
		}
	}
}

// Folds, and so selection ranges, come from the same bodies as before: the frame
// does not change which braces are script bodies. Bug hunted: a change that stops
// returning (or starts returning) a body while retagging its frame.
func TestOpaqueFrameFoldsUnchanged(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{
			"oo::class create X { method m {p} { set b 2 }; self method s {} { } }",
			[]string{"{ method m {p} { set b 2 }; self method s {} { } }", "{ set b 2 }", "{ }"},
		},
		{"with { if {1} { set x 1 } }", []string{"{ if {1} { set x 1 } }", "{ set x 1 }"}},
		{"eval foo {set x 1}", []string{"{set x 1}"}},
		{"uplevel 1 {set x 1}", []string{"{set x 1}"}},
		{"tsp::proc f {a} { set b 1 }", []string{"{ set b 1 }"}},
		{"with { switch $v { a { set x 1 } } }", []string{"{ switch $v { a { set x 1 } } }", "{ a { set x 1 } }", "{ set x 1 }"}},
		{"with { namespace eval ::n { set v 1 } }", []string{"{ namespace eval ::n { set v 1 } }", "{ set v 1 }"}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			var got []string
			for _, fold := range FileFolds(c.src) {
				got = append(got, c.src[fold.Open:fold.Close+1])
			}
			want := slices.Clone(c.want)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("folds of %q = %q, want %q", c.src, got, want)
			}
		})
	}
}
