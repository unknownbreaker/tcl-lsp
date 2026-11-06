package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// A bare variable in an unknown command's body has no answer, because nothing says
// which frame the body runs in. Qualified names, control flow at namespace level,
// and scopes that set their own frame keep their answers.
func TestOpaqueFrameResolution(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"TclOO local", "oo::class create X {\n  method m {} {\n    set x 1\n    puts $x\n  }\n}", "$x", nil},
		{"tsp::proc param vs global", "set file 1\ntsp::proc f {file} {\n  puts $file\n}", "$file", nil},
		{"no crossing classes", "oo::class create A {\n  method m {} {\n    set _value 0\n  }\n}\noo::class create B {\n  method n {} {\n    return $_value\n  }\n}", "$_value", nil},
		{"bare in wrapper", "set x 1\nwith {\n  puts $x\n}", "$x", nil},
		{"qualified in wrapper", "set ::x 1\nwith {\n  puts $::x\n}", "$::x", []int{1}},
		{"namespace-level if", "set x 1\nif {1} {\n  puts $x\n}", "$x", []int{1}},
		{"wrapper inside a proc", "proc f {a} {\n  with {\n    puts $a\n  }\n}", "$a", []int{1}},
		{"proc in bracket, global set", "set a 1\nif [catch {proc g {a} {puts $a}}] {}", "$a", nil},
		{"class in bracket, global set", "set a 1\nif [catch {itcl::class X { method m {} { puts $a } }}] {}", "$a", nil},
		{"wrapper in bracket in proc", "proc f {y} {\n  set r [with_lock { puts $y }]\n}", "$y", []int{1}},
		// Scopes that set their own frame reset it inside an opaque body.
		{"namespace eval in wrapper", "with {\n  namespace eval ::n {\n    set v 1\n    puts $v\n  }\n}", "$v", []int{3}},
		{"proc in wrapper", "with {\n  proc g {a} {\n    puts $a\n  }\n}", "$a", []int{2}},
		{"itcl::class in wrapper", "with {\n  itcl::class C {\n    variable v\n    method m {} {\n      puts $v\n    }\n  }\n}", "$v", []int{3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// Typing a receiver takes reaching definitions, not a namespace variable, so `$o m`
// stays typed inside an unknown command's body. ::C is indexed once per case, because
// one index holding ::C in two files correctly answers with two locations.
func TestObjMethodTypedInsideOpaqueBodies(t *testing.T) {
	const classFile = "c.tcl"
	const classSource = "itcl::class ::C {\n  method m {} {}\n}"
	cases := []struct{ name, file, src string }{
		{"wrapper at top level", "use.tcl", "with {\n  set o [::C #auto]\n  $o m\n}"},
		{"TclOO method", "use.tcl", "oo::class create X {\n  method run {} {\n    set o [::C #auto]\n    $o m\n  }\n}"},
		{"wrapper in an rvt page", "page.rvt", "<? with {\n  set o [::C #auto]\n  $o m\n} ?>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(classFile, classSource)
			ix.IndexFile(c.file, c.src)
			methodOffset := strings.LastIndex(c.src, "$o m") + len("$o ")
			locations := New(ix).Definition(c.file, c.src, methodOffset)
			if len(locations) != 1 || locations[0].File != classFile || locations[0].Name != "m" {
				t.Errorf("definition of $o m = %#v, want ::C's method m in %s\nsource:\n%s", locations, classFile, c.src)
			}
		})
	}
}

// startLinesOf returns the sorted 1-based lines on which locations in file start.
func startLinesOf(src, file string, locations []index.Location) []int {
	var lines []int
	for _, location := range locations {
		if location.File == file {
			lines = append(lines, lineOfOffset(src, location.NameStart))
		}
	}
	slices.Sort(lines)
	return lines
}

// Bare variable uses in every spelling the reference scanner knows, set elsewhere at
// namespace level. In an unknown body each is silent; the same line at namespace
// level resolves, which proves the cursor sits on a real reference. Bug hunted: a
// frame check that covers `$x` but not `${x}`, an array element, a quoted word, a
// bracket, or a name passed as data to `info exists`, `unset` and `array`.
func TestOpaqueFrameBareUseFormsStaySilent(t *testing.T) {
	forms := []struct{ name, setup, use, mark string }{
		{"plain", "set x 1", "puts $x", "$x"},
		{"braced name", "set x 1", "puts ${x}", "${x}"},
		{"inside quotes", "set x 1", `puts "v=$x"`, "$x"},
		{"inside brackets", "set x 1", "puts [string length $x]", "$x"},
		{"array element", "set x(k) 1", "puts $x(k)", "$x(k)"},
		{"info exists", "set x 1", "puts [info exists x]", " x]"},
		{"info exists in a condition", "set x 1", "if {[info exists x]} { }", " x]"},
		{"unset", "set x 1", "unset x", " x"},
		{"array get", "set x(k) 1", "array get x", " x"},
	}
	for _, form := range forms {
		t.Run("namespace level: "+form.name, func(t *testing.T) {
			src := form.setup + "\n" + form.use + "\n"
			if got := defLinesAt(t, src, form.mark); !slices.Equal(got, []int{1}) {
				t.Errorf("control: definition lines of %s = %v, want [1]\nsource:\n%s", form.mark, got, src)
			}
		})
		t.Run("wrapper: "+form.name, func(t *testing.T) {
			src := form.setup + "\nwith {\n  " + form.use + "\n}\n"
			if got := defLinesAt(t, src, form.mark); got != nil {
				t.Errorf("definition lines of %s = %v, want none\nsource:\n%s", form.mark, got, src)
			}
		})
		t.Run("TclOO method: "+form.name, func(t *testing.T) {
			src := form.setup + "\noo::class create X {\n  method m {} {\n    " + form.use + "\n  }\n}\n"
			if got := defLinesAt(t, src, form.mark); got != nil {
				t.Errorf("definition lines of %s = %v, want none\nsource:\n%s", form.mark, got, src)
			}
		})
	}
}

// A use nested in further bodies is as silent as one directly in the unknown body,
// except under a head that really runs here, and only when the whole chain does.
// Bug hunted: nested bodies that reset to namespace frame (the old default) or that
// treat `eval foo` and `uplevel` as pass-through.
func TestOpaqueFrameBareUseThroughNestedBodies(t *testing.T) {
	containers := []struct {
		name, open, close string
		atNamespaceLevel  []int
	}{
		{"if", "if {1} {", "}", []int{1}},
		{"foreach", "foreach i $l {", "}", []int{1}},
		{"while", "while {1} {", "}", []int{1}},
		{"catch", "catch {", "}", []int{1}},
		{"try", "try {", "}", []int{1}},
		{"switch", "switch $v {\n    a {", "}\n    }", []int{1}},
		{"one-word eval", "eval {", "}", []int{1}},
		{"eval with a command", "eval foo {", "}", nil},
		{"uplevel", "uplevel {", "}", nil},
		{"uplevel with a level", "uplevel 1 {", "}", nil},
	}
	for _, c := range containers {
		body := "    puts $x\n"
		t.Run("namespace level: "+c.name, func(t *testing.T) {
			src := "set x 1\n" + c.open + "\n" + body + c.close + "\n"
			if got := defLinesAt(t, src, "$x"); !slices.Equal(got, c.atNamespaceLevel) {
				t.Errorf("definition lines of $x = %v, want %v\nsource:\n%s", got, c.atNamespaceLevel, src)
			}
		})
		t.Run("in wrapper: "+c.name, func(t *testing.T) {
			src := "set x 1\nwith {\n" + c.open + "\n" + body + c.close + "\n}\n"
			if got := defLinesAt(t, src, "$x"); got != nil {
				t.Errorf("definition lines of $x = %v, want none\nsource:\n%s", got, src)
			}
		})
		t.Run("in TclOO method: "+c.name, func(t *testing.T) {
			src := "set x 1\noo::class create X {\n  method m {} {\n" + c.open + "\n" + body + c.close + "\n  }\n}\n"
			if got := defLinesAt(t, src, "$x"); got != nil {
				t.Errorf("definition lines of $x = %v, want none\nsource:\n%s", got, src)
			}
		})
	}
}

// Everything else about names around an unknown body. Bug hunted: a binding that
// leaks out of the body to later namespace-level code, a fake binding that shadows
// the real one, arguments of the wrapper itself (evaluated outside) going silent,
// a qualified definition inside the body that stops being visible outside, and a
// namespace that is lost on the way in.
func TestOpaqueFrameNamesAroundUnknownBodies(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"set inside does not leak out", "with {\n  set x 1\n}\nputs $x", "$x", nil},
		{"TclOO local does not leak out", "oo::class create X {\n  method m {} {\n    set x 1\n  }\n}\nputs $x", "$x", nil},
		{"variable inside does not leak out", "oo::class create X {\n  variable x\n}\nputs $x", "$x", nil},
		{"set inside does not shadow the real one", "set x 1\nwith {\n  set x 2\n}\nputs $x", "$x", []int{1}},
		{"bare use of a name set in the same body", "with {\n  set x 1\n  puts $x\n}", "$x", nil},
		{"bare use with only a qualified definition", "set ::x 1\nwith {\n  puts $x\n}", "$x", nil},
		{"other namespace: bare use", "namespace eval ::n {\n  set x 1\n  with {\n    puts $x\n  }\n}", "$x", nil},
		{"other namespace: control", "namespace eval ::n {\n  set x 1\n  puts $x\n}", "$x", []int{2}},
		{"wrapper argument is evaluated outside", "set x 1\nwith $x {\n  puts hi\n}", "$x", []int{1}},
		{"wrapper option is evaluated outside", "set x 1\nwith -timeout $x {\n  puts hi\n}", "$x", []int{1}},
		{"class name from a variable", "set x X\noo::class create $x {\n}", "$x", []int{1}},
		// Qualified names mean the same thing in every frame.
		{"relative qualified use", "set a::b 1\nwith {\n  puts $a::b\n}", "$a::b", []int{1}},
		{"relative qualified use in namespace", "namespace eval ::n {\n  set a::b 1\n  with {\n    puts $a::b\n  }\n}", "$a::b", []int{2}},
		{"qualified array element", "set ::x(k) 1\nwith {\n  puts $::x(k)\n}", "$::x(k)", []int{1}},
		{"qualified braced name", "set ::x 1\nwith {\n  puts ${::x}\n}", "${::x}", []int{1}},
		{"qualified info exists", "set ::x 1\nwith {\n  puts [info exists ::x]\n}", " ::x]", []int{1}},
		{"qualified unset", "set ::x 1\nwith {\n  unset ::x\n}", " ::x\n", []int{1}},
		{"qualified definition is visible outside", "with {\n  set ::x 1\n}\nputs $::x", "$::x", []int{2}},
		{"qualified variable is visible outside", "with {\n  variable ::y\n}\nputs $::y", "$::y", []int{2}},
		{"qualified definition from a TclOO method", "oo::class create X {\n  method m {} {\n    set ::g 1\n  }\n}\nputs $::g", "$::g", []int{3}},
		{"qualified definition then use inside", "with {\n  set ::x 1\n  puts $::x\n}", "$::x", []int{2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// Find-references and highlight from a namespace variable must not pick up a bare use
// in an unknown body, and from inside that body they say nothing. A qualified use in
// the same body still counts. Bug hunted: a ContextRef the frame check forgot, such
// as one reached through References, FileHighlights or Declarations rather than
// Definition.
func TestOpaqueFrameReferencesAndHighlights(t *testing.T) {
	src := "set x 1\nputs $x\nwith {\n  puts $x\n  puts $::x\n}\n"
	resolver := resolverOverGuardFile(src)
	namespaceUse := strings.Index(src, "$x") + 1
	opaqueUse := strings.LastIndex(src, "$x") + 1
	qualifiedUse := strings.Index(src, "$::x") + 1
	declaration := strings.Index(src, "x 1")

	referenceLines := func(offset int) []int {
		return startLinesOf(src, guardFile, resolver.References(guardFile, src, offset))
	}
	highlightLines := func(offset int) []int {
		return startLinesOf(src, guardFile, resolver.FileHighlights(guardFile, src, offset))
	}

	cases := []struct {
		name string
		got  []int
		want []int
	}{
		{"references from the namespace-level use", referenceLines(namespaceUse), []int{2, 5}},
		{"references from the declaration", referenceLines(declaration), []int{2, 5}},
		{"references from the qualified use", referenceLines(qualifiedUse), []int{2, 5}},
		{"references from the bare use in the body", referenceLines(opaqueUse), nil},
		{"highlights from the namespace-level use", highlightLines(namespaceUse), []int{1, 2, 5}},
		{"highlights from the declaration", highlightLines(declaration), []int{1, 2, 5}},
		{"highlights from the bare use in the body", highlightLines(opaqueUse), nil},
		{"declarations of the bare use in the body", startLinesOf(src, guardFile, resolver.Declarations(guardFile, src, opaqueUse)), nil},
		{"definition of the bare use in the body", startLinesOf(src, guardFile, resolver.Definition(guardFile, src, opaqueUse)), nil},
	}
	for _, c := range cases {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v\nsource:\n%s", c.name, c.got, c.want, src)
		}
	}
}

// A binding in an unknown body is neither a definition nor a reference target: the
// name range of `set x` there leads nowhere, and find-references from it is empty.
// Bug hunted: a fake local the cursor-on-binding path (localAt) would still answer.
func TestOpaqueFrameBindingSiteIsNotASymbol(t *testing.T) {
	cases := []struct{ name, src string }{
		{"wrapper", "set x 1\nwith {\n  set x 2\n}"},
		{"TclOO method", "set x 1\noo::class create X {\n  method m {} {\n    set x 2\n  }\n}"},
		{"array set", "set x(a) 1\nwith {\n  array set x {b 2}\n}"},
		{"variable", "set x 1\nwith {\n  variable x\n}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resolver := resolverOverGuardFile(c.src)
			site := strings.LastIndex(c.src, " x") + 1
			if site <= 0 {
				t.Fatalf("test bug: no binding site in %q", c.src)
			}
			for what, locations := range map[string][]index.Location{
				"definition":   resolver.Definition(guardFile, c.src, site),
				"declarations": resolver.Declarations(guardFile, c.src, site),
				"references":   resolver.References(guardFile, c.src, site),
				"highlights":   resolver.FileHighlights(guardFile, c.src, site),
			} {
				if len(locations) != 0 {
					t.Errorf("%s from the binding site in %q = %#v, want none", what, c.src, locations)
				}
			}
		})
	}
}

// commandKindAt returns the kind a call resolves to, the way semantic tokens ask:
// the command reference whose range starts one byte past mark's start.
func commandKindAt(t *testing.T, src, mark string) (tcl.DefKind, bool) {
	t.Helper()
	markOffset := strings.LastIndex(src, mark)
	if markOffset < 0 {
		t.Fatalf("test bug: mark %q not found in %q", mark, src)
	}
	resolver := resolverOverGuardFile(src)
	refs := source.Refs(guardFile, src)
	for i := range refs {
		if refs[i].Ref.Kind == tcl.RefCommand && refs[i].Ref.Start == markOffset+1 {
			return resolver.CommandRefKind(&refs[i], guardFile)
		}
	}
	t.Fatalf("no command reference starts at offset %d in %q", markOffset+1, src)
	return 0, false
}

// Commands resolve the same at an opaque frame as anywhere: the namespace of the
// call is kept and procs defined in the body are visible outside it. Each mark
// starts one byte before the called name. Bug hunted: an opaque frame that loses the
// enclosing namespace, drops command references, or hides the procs a body defines.
func TestOpaqueFrameCommandsStillResolve(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"enclosing-namespace proc from a TclOO method", "namespace eval ::app {\n  proc helper {} {}\n  oo::class create X {\n    method m {} {\n      helper\n    }\n  }\n}", " helper\n", []int{2}},
		{"global proc from a TclOO method in a namespace", "proc helper {} {}\nnamespace eval ::app {\n  oo::class create X {\n    method m {} {\n      helper\n    }\n  }\n}", " helper\n", []int{1}},
		{"global proc from a wrapper", "proc helper {} {}\nwith {\n  helper\n}", " helper\n", []int{1}},
		{"proc from a wrapper in a namespace", "namespace eval ::app {\n  proc helper {} {}\n  with {\n    helper\n  }\n}", " helper\n", []int{2}},
		{"relative call", "proc ::app::util::go {} {}\nnamespace eval ::app {\n  with {\n    util::go\n  }\n}", " util::go\n", []int{1}},
		{"qualified call", "proc ::a::f {} {}\nwith {\n  ::a::f\n}", " ::a::f\n", []int{1}},
		{"global proc from tsp::proc", "proc helper {} {}\ntsp::proc f {a} {\n  helper\n}", " helper\n", []int{1}},
		{"call in an if in a wrapper", "proc helper {} {}\nwith {\n  if {1} {\n    helper\n  }\n}", " helper\n", []int{1}},
		{"call in a condition in a wrapper", "proc ready {} {}\nwith {\n  if {[ready]} {\n  }\n}", "[ready]", []int{1}},
		{"proc defined in a wrapper, called outside", "with {\n  proc g {} {}\n}\ng\n", "\ng\n", []int{2}},
		{"proc defined in a TclOO class body, called outside", "oo::class create X {\n  proc g {} {}\n}\ng\n", "\ng\n", []int{2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := strings.LastIndex(c.src, c.mark)
			if start < 0 {
				t.Fatalf("test bug: mark %q not found in %q", c.mark, c.src)
			}
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of the call = %v, want %v\nsource:\n%s", got, c.want, c.src)
			}
			if kind, ok := commandKindAt(t, c.src, c.mark); !ok || kind != tcl.DefProc {
				t.Errorf("semantic-token kind of the call = (%v, %v), want (DefProc, true)", kind, ok)
			}
		})
	}
}

// Itcl keeps its own frames. A class inside an unknown body, a method body supplied
// by itcl::body at the top level, and a `public { ... }` block all stay class or proc
// frames, and a wrapper inside a method is still the method's frame. Bug hunted: a
// default branch that turns every unknown head opaque instead of only at namespace
// frame, and one that loses the class name on the way into a wrapper.
func TestOpaqueFrameLeavesItclFramesAlone(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"method call inside a class in a wrapper", "with {\n  itcl::class C {\n    method a {} {}\n    method b {} {\n      a\n    }\n  }\n}", " a\n", []int{3}},
		{"itcl::body local", "itcl::class C {\n  method m {}\n}\nitcl::body C::m {} {\n  set z 1\n  puts $z\n}", "$z", []int{5}},
		{"itcl::body ivar", "itcl::class C {\n  variable v\n  method m {}\n}\nitcl::body C::m {} {\n  puts $v\n}", "$v", []int{2}},
		{"ivar in a public block", "itcl::class C {\n  variable v\n  public {\n    method m {} {\n      puts $v\n    }\n  }\n}", "$v", []int{2}},
		{"local in a public block", "itcl::class C {\n  public {\n    method m {} {\n      set z 1\n      puts $z\n    }\n  }\n}", "$z", []int{4}},
		{"wrapper in a method: ivar", "itcl::class C {\n  variable v\n  method m {} {\n    with {\n      puts $v\n    }\n  }\n}", "$v", []int{2}},
		{"wrapper in a method: local", "itcl::class C {\n  method m {} {\n    with {\n      set z 1\n      puts $z\n    }\n  }\n}", "$z", []int{4}},
		{"wrapper in a proc: local", "proc f {} {\n  with {\n    set x 1\n    puts $x\n  }\n}", "$x", []int{3}},
		{"if in a wrapper in a proc: param", "proc f {a} {\n  with {\n    if {1} {\n      puts $a\n    }\n  }\n}", "$a", []int{1}},
		{"class in a wrapper in a namespace", "namespace eval ::n {\n  with {\n    itcl::class C {\n      variable v\n      method m {} {\n        puts $v\n      }\n    }\n  }\n}", "$v", []int{4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// The substitution walker has no frame of its own, so a bracket decides what is
// dropped. A proc or class body inside a bracket drops its variable uses at any depth
// but keeps its command calls, while the rest of the bracket is untouched. The two
// pinned cases are known and accepted: a wrapper in a bracket at namespace level
// keeps its namespace-frame answer. Bug hunted: a drop that stops at the first level
// of the body, one that takes the whole bracket with it, and one that drops commands.
func TestOpaqueFrameBracketedBodies(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"accepted residual: wrapper in a bracket", "set x 1\nset r [with {\n  puts $x\n}]", "$x", []int{1}},
		{"accepted residual: lock around a script", "set x 1\nputs [with_lock {\n  puts $x\n}]", "$x", []int{1}},
		{"catch in a bracket keeps the answer", "set y 1\nif [catch {puts $y}] {}", "$y", []int{1}},
		{"control flow in a catch in a bracket", "set y 1\nif [catch {foreach i $l {puts $y}}] {}", "$y", []int{1}},
		{"nested brackets keep the answer", "set y 1\nset r [string length [catch {puts $y}]]", "$y", []int{1}},
		{"proc in a bracket: deep use", "set a 1\nif [catch {proc g {a} {\n  if {1} {\n    puts $a\n  }\n}}] {}", "$a", nil},
		{"proc in a bracket: use in a nested bracket", "set a 1\nif [catch {proc g {} {\n  set r [string length $a]\n}}] {}", "$a", nil},
		{"proc in a bracket: sibling use stays", "set a 1\nif [catch {\n  proc g {} {}\n  puts $a\n}] {}", "$a", []int{1}},
		{"proc in a bracket in a proc", "proc f {a} {\n  if [catch {proc g {a} {puts $a}}] {}\n}", "$a", nil},
		{"class in a bracket in a proc", "proc f {a} {\n  if [catch {itcl::class X {method m {} {puts $a}}}] {}\n}", "$a", nil},
		{"class in a bracket: deep use", "set a 1\nif [catch {itcl::class X {\n  method m {} {\n    if {1} {\n      puts $a\n    }\n  }\n}}] {}", "$a", nil},
		{"proc in a bracket keeps its calls", "proc helper {} {}\nif [catch {proc g {} {helper}}] {}", "{helper}", []int{1}},
		{"class in a bracket keeps its calls", "proc helper {} {}\nif [catch {itcl::class X {method m {} {helper}}}] {}", "{helper}", []int{1}},
		{"wrapper in a bracket in a proc", "proc f {y} {\n  set r [with_lock {\n    puts $y\n  }]\n}", "$y", []int{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// rvtDefinitionLines asks for the definition of the last occurrence of mark in a
// page, with the cursor one byte in, and returns the 1-based lines it answers with.
func rvtDefinitionLines(t *testing.T, src, mark string) []int {
	t.Helper()
	const page = "page.rvt"
	markOffset := strings.LastIndex(src, mark)
	if markOffset < 0 {
		t.Fatalf("test bug: mark %q not found in %q", mark, src)
	}
	ix := index.New()
	ix.IndexFile(page, src)
	return startLinesOf(src, page, New(ix).Definition(page, src, markOffset+1))
}

// A page's top level is a namespace frame (::request), so control flow there keeps
// resolving page variables and an unknown command's body does not. Bug hunted: a page
// whose `<? with { ?> ... <? } ?>` split across tags still binds or resolves, and a
// qualified ::request name that stops working inside the body.
func TestOpaqueFrameInRvtPages(t *testing.T) {
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"if keeps the answer", "<? set x 1 ?>\n<? if {1} { ?>\n<?= $x ?>\n<? } ?>", "$x", []int{1}},
		{"foreach keeps the answer", "<? set x 1 ?>\n<? foreach i $l { ?>\n<?= $x ?>\n<? } ?>", "$x", []int{1}},
		{"wrapper split across tags is silent", "<? set x 1 ?>\n<? with { ?>\n<?= $x ?>\n<? } ?>", "$x", nil},
		{"set in a wrapper does not reach the page", "<? with { set x 1 } ?>\n<?= $x ?>", "$x", nil},
		{"set in an if reaches the page", "<? if {1} { set x 1 } ?>\n<?= $x ?>", "$x", []int{1}},
		{"qualified set in a wrapper reaches the page", "<? with { set ::request::x 1 } ?>\n<?= $::request::x ?>", "$::request::x", []int{1}},
		{"qualified use in a wrapper", "<? set ::request::x 1 ?>\n<? with { ?>\n<?= $::request::x ?>\n<? } ?>", "$::request::x", []int{1}},
		{"proc defined in a wrapper is callable", "<? with { proc g {} {} } ?>\n<? g ?>", " g ", []int{1}},
		{"proc param in a wrapper", "<? with { proc g {a} { ?>\n<?= $a ?>\n<? } } ?>", "$a", []int{1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rvtDefinitionLines(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// A typed receiver keeps its type wherever an unknown body sits, because reaching
// definitions look at the innermost scope, not at a namespace variable. Bug hunted:
// enclosingProcOrScope refusing the new frame as the innermost scope. (A wrapper
// inside a proc is not listed: the analyzer does not visit a use inside an unknown
// body there, today or after this change, so `$o m` stays silent.)
func TestObjMethodTypedAroundOpaqueBodies(t *testing.T) {
	const classFile = "c.tcl"
	const classSource = "itcl::class ::C {\n  method m {} {}\n}"
	cases := []struct{ name, src string }{
		{"wrapper in a namespace eval", "namespace eval ::n {\n  with {\n    set o [::C #auto]\n    $o m\n  }\n}"},
		{"if in a wrapper", "with {\n  if {1} {\n    set o [::C #auto]\n    $o m\n  }\n}"},
		{"wrapper in a wrapper", "with {\n  with {\n    set o [::C #auto]\n    $o m\n  }\n}"},
		{"TclOO method in a namespace", "namespace eval ::n {\n  oo::class create X {\n    method run {} {\n      set o [::C #auto]\n      $o m\n    }\n  }\n}"},
		{"foreach in a TclOO method", "oo::class create X {\n  method run {} {\n    foreach i $l {\n      set o [::C #auto]\n      $o m\n    }\n  }\n}"},
		{"eval with a command", "eval foo {\n  set o [::C #auto]\n  $o m\n}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(classFile, classSource)
			ix.IndexFile("use.tcl", c.src)
			methodOffset := strings.LastIndex(c.src, "$o m") + len("$o ")
			locations := New(ix).Definition("use.tcl", c.src, methodOffset)
			if len(locations) != 1 || locations[0].File != classFile || locations[0].Name != "m" {
				t.Errorf("definition of $o m = %#v, want ::C's method m in %s\nsource:\n%s", locations, classFile, c.src)
			}
		})
	}
}

const (
	classSetsValueSource = "namespace eval ::app {\n  oo::class create A {\n    method m {} {\n      set _value 0\n    }\n  }\n}"
	classUsesValueSource = "namespace eval ::app {\n  oo::class create B {\n    method n {} {\n      return $_value\n    }\n  }\n}"
	plainSetsValueSource = "namespace eval ::app {\n  set _value 0\n}"
	plainUsesValueSource = "namespace eval ::app {\n  puts $_value\n}"
)

func definitionOfValueUse(ix *index.Index, file, src string) []index.Location {
	return New(ix).Definition(file, src, strings.LastIndex(src, "$_value")+1)
}

// Two TclOO classes in one namespace, in separate files, no longer see each other's
// method locals, whichever file is indexed first. This is the leak the frame exists
// to stop. Bug hunted: index order mattering, and the leak surviving in the
// index's name table even when the resolver is silent.
func TestOpaqueFrameDoesNotLeakAcrossFilesInEitherIndexOrder(t *testing.T) {
	orders := []struct {
		name  string
		files [][2]string
	}{
		{"setter first", [][2]string{{"a.tcl", classSetsValueSource}, {"b.tcl", classUsesValueSource}}},
		{"user first", [][2]string{{"b.tcl", classUsesValueSource}, {"a.tcl", classSetsValueSource}}},
	}
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			ix := index.New()
			for _, file := range order.files {
				ix.IndexFile(file[0], file[1])
			}
			if locations := definitionOfValueUse(ix, "b.tcl", classUsesValueSource); len(locations) != 0 {
				t.Errorf("$_value in one class resolves to %#v, want nothing", locations)
			}
			if locations := ix.Lookup("::app::_value"); len(locations) != 0 {
				t.Errorf("the index holds ::app::_value at %#v, want nothing", locations)
			}
			for _, symbol := range ix.AllSymbols() {
				if symbol.Kind == tcl.DefNamespaceVar {
					t.Errorf("workspace symbols hold the namespace variable %#v, want none", symbol)
				}
			}
		})
	}
}

// Re-indexing a file replaces what it defined: a namespace variable set at
// namespace level answers across files, and moving the same assignment into a TclOO
// method takes the answer away, and moving it back restores it. Bug hunted: a stale
// definition surviving the re-index, and the control that proves the answer exists.
func TestOpaqueFrameReindexingOneFileChangesTheAnswer(t *testing.T) {
	ix := index.New()
	ix.IndexFile("a.tcl", plainSetsValueSource)
	ix.IndexFile("b.tcl", plainUsesValueSource)

	if locations := definitionOfValueUse(ix, "b.tcl", plainUsesValueSource); len(locations) != 1 || locations[0].File != "a.tcl" {
		t.Fatalf("control: $_value resolves to %#v, want the definition in a.tcl", locations)
	}

	ix.IndexFile("a.tcl", classSetsValueSource)
	if locations := definitionOfValueUse(ix, "b.tcl", plainUsesValueSource); len(locations) != 0 {
		t.Errorf("after the assignment moved into a TclOO method, $_value resolves to %#v, want nothing", locations)
	}

	ix.IndexFile("a.tcl", plainSetsValueSource)
	if locations := definitionOfValueUse(ix, "b.tcl", plainUsesValueSource); len(locations) != 1 || locations[0].File != "a.tcl" {
		t.Errorf("after the assignment moved back, $_value resolves to %#v, want the definition in a.tcl", locations)
	}

	// Re-indexing the user, not the setter, changes nothing either way.
	ix.IndexFile("a.tcl", classSetsValueSource)
	ix.IndexFile("b.tcl", classUsesValueSource)
	if locations := definitionOfValueUse(ix, "b.tcl", classUsesValueSource); len(locations) != 0 {
		t.Errorf("after re-indexing the user, $_value resolves to %#v, want nothing", locations)
	}
}

// A namespace variable's references stay inside what can see it, across files. Bug
// hunted: another file's bare use in an unknown body being counted as a reference to
// the namespace variable, and the same use resolving to it.
func TestOpaqueFrameReferencesAcrossFiles(t *testing.T) {
	declaring := "namespace eval ::app {\n  variable x 1\n  puts $x\n}"
	wrapped := "namespace eval ::app {\n  with {\n    puts $x\n  }\n}"
	methodBody := "namespace eval ::app {\n  oo::class create B {\n    method n {} {\n      puts $x\n    }\n  }\n}"
	qualified := "namespace eval ::app {\n  with {\n    puts $::app::x\n  }\n}"

	ix := index.New()
	ix.IndexFile("decl.tcl", declaring)
	ix.IndexFile("wrapped.tcl", wrapped)
	ix.IndexFile("method.tcl", methodBody)
	ix.IndexFile("qualified.tcl", qualified)
	resolver := New(ix)

	references := resolver.References("decl.tcl", declaring, strings.Index(declaring, "$x")+1)
	var files []string
	for _, reference := range references {
		files = append(files, reference.File)
	}
	slices.Sort(files)
	if want := []string{"decl.tcl", "qualified.tcl"}; !slices.Equal(files, want) {
		t.Errorf("references to ::app::x are in %v, want %v (the bare uses in unknown bodies are not references)\n%#v", files, want, references)
	}

	for _, file := range []struct{ name, src string }{{"wrapped.tcl", wrapped}, {"method.tcl", methodBody}} {
		offset := strings.LastIndex(file.src, "$x") + 1
		if locations := resolver.Definition(file.name, file.src, offset); len(locations) != 0 {
			t.Errorf("$x in %s resolves to %#v, want nothing", file.name, locations)
		}
		if locations := resolver.References(file.name, file.src, offset); len(locations) != 0 {
			t.Errorf("references from $x in %s = %#v, want nothing", file.name, locations)
		}
	}
	if locations := resolver.Definition("qualified.tcl", qualified, strings.LastIndex(qualified, "$::app::x")+1); len(locations) != 1 || locations[0].File != "decl.tcl" {
		t.Errorf("$::app::x in an unknown body resolves to %#v, want the definition in decl.tcl", locations)
	}
}

// A parameter or local in a TclOO method or tsp::proc must not resolve to a variable
// of the same name defined in another file. Bug hunted: the single-file case holding
// while the cross-file lookup (the workspace table) still answers.
func TestOpaqueFrameParameterDoesNotResolveToAnotherFilesVariable(t *testing.T) {
	ix := index.New()
	ix.IndexFile("globals.tcl", "set file 1\nset count 0")
	user := "tsp::proc f {file} {\n  puts $file\n}\noo::class create X {\n  method m {} {\n    puts $count\n  }\n}"
	ix.IndexFile("user.tcl", user)
	resolver := New(ix)
	for _, mark := range []string{"$file", "$count"} {
		if locations := resolver.Definition("user.tcl", user, strings.LastIndex(user, mark)+1); len(locations) != 0 {
			t.Errorf("%s in an unknown body resolves to %#v, want nothing", mark, locations)
		}
	}
}

// The workspace outline of a TclOO class lists the procs and no variables.
func TestOpaqueFrameWorkspaceSymbolsHoldProcsNotLocals(t *testing.T) {
	ix := index.New()
	ix.IndexFile("a.tcl", "oo::class create X {\n  variable v\n  constructor {} {set a 1}\n  method m {p} {set b 2}\n  self method s {} {}\n}\nproc helper {} {}\nwith {\n  proc inner {} {}\n  set w 1\n}")
	var procs []string
	for _, symbol := range ix.AllSymbols() {
		switch symbol.Kind {
		case tcl.DefProc:
			procs = append(procs, symbol.Name)
		case tcl.DefNamespaceVar:
			t.Errorf("workspace symbols hold the namespace variable %#v, want none", symbol)
		}
	}
	slices.Sort(procs)
	if want := []string{"helper", "inner"}; !slices.Equal(procs, want) {
		t.Errorf("workspace procs = %v, want %v", procs, want)
	}
}
