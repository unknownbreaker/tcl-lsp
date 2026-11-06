package resolve

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const guardFile = "a.tcl"

// resolverOverGuardFile indexes src under guardFile, the file the definition
// helpers below ask about.
func resolverOverGuardFile(src string) *Resolver {
	ix := index.New()
	ix.IndexFile(guardFile, src)
	return New(ix)
}

// lineOfOffset returns the 1-based line holding the byte offset in src.
func lineOfOffset(src string, offset int) int {
	return 1 + strings.Count(src[:offset], "\n")
}

// defLinesAt asks for the definition of the last occurrence of mark, with the
// cursor one byte in (on the name of a `$name` use), and returns the answer's
// 1-based lines, sorted. Duplicates are kept, so a repeated answer shows up.
func defLinesAt(t *testing.T, src, mark string) []int {
	t.Helper()
	markOffset := strings.LastIndex(src, mark)
	if markOffset < 0 {
		t.Fatalf("test bug: mark %q not found in %q", mark, src)
	}
	var lines []int
	for _, loc := range resolverOverGuardFile(src).Definition(guardFile, src, markOffset+1) {
		if loc.File != guardFile {
			t.Fatalf("Definition returned a location in %q, want only %q: %#v", loc.File, guardFile, loc)
		}
		lines = append(lines, lineOfOffset(src, loc.NameStart))
	}
	slices.Sort(lines)
	return lines
}

func TestDefinitionBindingsThatCannotReach(t *testing.T) {
	inTry := "proc in_try {x} {\n    try {\n        puts $x\n        set x 2\n    } on error {} {\n    }\n}"
	inCatch := "proc in_catch {y} {\n    catch {\n        puts $y\n        set y 2\n    }\n}"
	afterCatch := "proc after_all {} {\n    set z 0\n    catch { set z 1 }\n    puts $z\n}"
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"try, before set", inTry, "$x", []int{1}},              // was [1,4]
		{"catch, before set", inCatch, "$y", []int{1}},          // was [1,4]
		{"control, after catch", afterCatch, "$z", []int{2, 3}}, // must stay
		{"regexp, then later set", "proc f {} {\n  regexp {a} $s word\n  puts $word\n  set word 2\n}", "$word", nil},
		{"itcl ivar over later set", "itcl::class C {\n  protected variable n 0\n  method m {} {\n    puts $n\n    set n 1\n  }\n}", "$n", []int{2}},
		{"loop, no prior", "proc f {} {\n  while {1} {\n    puts $x\n    set x 1\n  }\n}", "$x", []int{4}},
		{"loop, prior", "proc f {} {\n  set x 0\n  while {1} {\n    puts $x\n    set x 1\n  }\n}", "$x", []int{2, 5}},
		{"loop, in catch", "proc f {} {\n  while {1} {\n    catch {\n      puts $x\n      set x 1\n    }\n  }\n}", "$x", []int{5}},
		{"foreach carried", "proc f {l} {\n  foreach i $l {\n    if {$i} { puts $prev }\n    set prev $i\n  }\n}", "$prev", []int{4}},
		{"handler sees body", "proc f {} {\n  try {\n    set x 1\n  } on error {} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"finally sees body", "proc f {} {\n  try {\n    set x 1\n  } finally {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"finally sees handler", "proc f {} {\n  set x 0\n  try {\n    set a 1\n  } on error {} {\n    set x 2\n  } finally {\n    puts $x\n  }\n}", "$x", []int{2, 6}},
		{"arm, own later set", "proc f {k} {\n  set x 0\n  switch $k {\n    a { puts $x\n      set x 1 }\n  }\n}", "$x", []int{2}},
		{"arm, other arm's set", "proc f {k} {\n  set x 0\n  switch $k {\n    a { set x 1 }\n    b { puts $x }\n  }\n}", "$x", []int{2}},
		{"global before use", "set ::x 1\nproc f {} {\n  global x\n  puts $x\n}", "$x", []int{1}},
		{"array, before set", "proc f {} {\n  puts $a(1)\n  set a(1) 2\n}", "$a", nil},
		{"array, after set", "proc f {} {\n  set a(1) 2\n  puts $a(1)\n}", "$a", []int{2}},
		{"inside a wrapper, before set", "proc f {} {\n  with { puts $x }\n  set x 1\n}", "$x", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

type definitionCase struct {
	name, src, mark string
	want            []int
}

func runDefinitionCases(t *testing.T, cases []definitionCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// Hunting: the fallback's position guard on the link commands. `global`, `variable`
// and `upvar` bind the name only from their own line on, so a use above them has no
// binding, and the chase through the link's origin must not resurrect one.
func TestDefinitionLinkBindingOrderingAgainstUse(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"global after the use", "set ::x 1\nproc f {} {\n  puts $x\n  global x\n}", "$x", nil},
		{"global before the use", "set ::x 1\nproc f {} {\n  global x\n  puts $x\n}", "$x", []int{1}},
		{"variable after the use", "namespace eval ns {\n  variable v 0\n  proc f {} {\n    puts $v\n    variable v\n  }\n}", "$v", nil},
		{"upvar after the use", "proc f {} {\n  puts $alias\n  upvar 1 other alias\n}", "$alias", nil},
		{"upvar before the use", "proc f {} {\n  upvar 1 other alias\n  puts $alias\n}", "$alias", []int{2}},
		{"second upvar pair after the use", "proc f {} {\n  upvar 1 a1 first\n  puts $second\n  upvar 1 b1 second\n}", "$second", nil},
	})
}

// Hunting: params are the earliest binding there is, so no later `set` of the same
// name may take over, and a use inside a loop sees the param and the loop's own set.
func TestDefinitionParamsAgainstLaterBindings(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"param, later set", "proc f {x} {\n  puts $x\n  set x 2\n}", "$x", []int{1}},
		{"param with default, later set", "proc f {{x 5}} {\n  puts $x\n  set x 2\n}", "$x", []int{1}},
		{"param, later set in a loop", "proc f {x} {\n  while {1} {\n    puts $x\n    set x 2\n  }\n}", "$x", []int{1, 4}},
		{"param, later incr", "proc f {x} {\n  puts $x\n  incr x\n}", "$x", []int{1}},
	})
}

// Hunting: array elements bypass reaching and use the fallback. A guard that
// compares against the wrong end of the binding, or the whole subscript, breaks here.
func TestDefinitionArrayElementsAgainstPosition(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"several subscripts, use before set", "proc f {} {\n  puts $a(1,2)\n  set a(1,2) 3\n}", "$a", nil},
		{"several subscripts, use after set", "proc f {} {\n  set a(1,2) 3\n  puts $a(1,2)\n}", "$a", []int{2}},
		{"variable subscripts, use before set", "proc f {i j} {\n  puts $a($i,$j)\n  set a($i,$j) 1\n}", "$a", nil},
		{"variable subscripts, use after set", "proc f {i j} {\n  set a($i,$j) 1\n  puts $a($i,$j)\n}", "$a", []int{2}},
		{"use before incr of the element", "proc f {} {\n  puts $cnt(x)\n  incr cnt(x)\n}", "$cnt", nil},
		{"use after lappend of the element", "proc f {} {\n  lappend q(x) 1\n  puts $q(x)\n}", "$q", []int{2}},
		{"only earlier set is chosen when one is also later", "proc f {} {\n  set a(1) 1\n  puts $a(2)\n  set a(2) 2\n}", "$a", []int{2}},
	})
}

// Hunting: Itcl instance variables. The class's declaration answers only when no
// local binding can reach, so a later local `set` must neither steal the answer
// nor hide the ivar, and a param or earlier local must still win.
func TestDefinitionItclIvarAgainstLocalBindings(t *testing.T) {
	const base = "itcl::class B {\n  protected variable n 0\n}\n"
	runDefinitionCases(t, []definitionCase{
		{"inherited ivar over a later local set", base + "itcl::class D {\n  inherit B\n  method m {} {\n    puts $n\n    set n 1\n  }\n}", "$n", []int{2}},
		{"inherited ivar over a later incr", base + "itcl::class D {\n  inherit B\n  method m {} {\n    puts $n\n    incr n\n  }\n}", "$n", []int{2}},
		{"earlier local set wins over the ivar", "itcl::class C {\n  protected variable n 0\n  method m {} {\n    set n 1\n    puts $n\n  }\n}", "$n", []int{4}},
		{"ivar use in one method ignores another method's local set", "itcl::class C {\n  protected variable n 0\n  method a {} {\n    set n 1\n  }\n  method m {} {\n    puts $n\n  }\n}", "$n", []int{2}},
	})
}

// An empty reaching answer means silence, so a binding in an unknown command's body
// has to answer when nothing else reaches. Any command's brace may be data or deferred
// code, so it stays out of the reach sets; a body that starts a new scope never answers.
func TestDefinitionBindingsInsideCommandBodies(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"wrapper binding reaches a later use", "proc f {} {\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"wrapper body sees an earlier binding", "proc f {} {\n  set x 0\n  with { puts $x }\n}", "$x", []int{2}},
		{"wrapper body before a later set", "proc f {} {\n  with { puts $x }\n  set x 1\n}", "$x", nil},
		{"wrapper binding below the use", "proc f {} {\n  puts $x\n  with { set x 1 }\n}", "$x", nil},
		{"wrapper binding below the use, in a loop", "proc f {} {\n  while {1} {\n    puts $x\n    with { set x 1 }\n  }\n}", "$x", nil},
		{"eval binding reaches a later use", "proc f {} {\n  eval { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"eval binding joins the prior binding", "proc f {} {\n  set x 0\n  eval { set x 1 }\n  puts $x\n}", "$x", []int{2, 3}},
		{"time binding joins the prior binding", "proc f {} {\n  set x 0\n  time { set x 1 }\n  puts $x\n}", "$x", []int{2, 3}},
		{"timerate binding joins the prior binding", "proc f {} {\n  set x 0\n  timerate { set x 1 }\n  puts $x\n}", "$x", []int{2, 3}},
		{"eval of a proc definition does not run the body", "proc f {} {\n  eval proc g {} { set x 1 }\n  puts $x\n}", "$x", nil},
		{"after a wrapper only the prior binding reaches", "proc f {} {\n  set x 0\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"wrapper binding answers an incr", "proc f {} {\n  with { set x 1 }\n  incr x\n}", " x\n}", []int{2}},
		{"wrapper in a catch answers when nothing else does", "proc f {} {\n  catch { with { set x 1 } }\n  puts $x\n}", "$x", []int{2}},
		{"wrapper in a catch stays out of the reach set", "proc f {} {\n  set x 0\n  catch { uplevel 1 {set x 1} }\n  puts $x\n}", "$x", []int{2}},
		{"wrapper in a try body answers its handler", "proc f {} {\n  try {\n    with { set x 1 }\n  } on error {} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"wrapper nested in a wrapper", "proc f {} {\n  with { with { set x 1 } }\n  puts $x\n}", "$x", []int{2}},
		{"proc inside a wrapper does not run here", "proc f {} {\n  with { proc g {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"proc inside a catch does not run here", "proc f {} {\n  catch { proc g {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"decorated proc inside a wrapper does not run here", "proc f {} {\n  with { CACHE_PROC proc g {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"namespace eval inside a wrapper does not bind a local", "proc f {} {\n  with { namespace eval ::n { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"class inside a wrapper does not bind a local", "proc f {} {\n  with { itcl::class C { method m {} { set x 1 } } }\n  puts $x\n}", "$x", nil},
		{"method declared in a definition script does not run here", "proc f {} {\n  with { method m {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"itcl::body inside a wrapper does not run here", "proc f {} {\n  with { itcl::body ::C::m {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"use inside a proc in a wrapper is analyzed in that proc", "proc f {} {\n  set x 0\n  with { proc g {} { set x 1; puts $x } }\n}", "$x", []int{3}},
	})
}

// Each of these commands' braces holds data, or code for another frame or a later
// time, so with a prior binding the answer is that binding alone. Tcl 8.6 agrees.
func TestDefinitionDataAndDeferredBracesDoNotReach(t *testing.T) {
	commands := []string{
		"uplevel 1 {set x 1}",
		"uplevel #0 {set x 1}",
		"after 100 {set x 1}",
		"fileevent $c readable {set x 1}",
		"bind . <Key> {set x 1}",
		"button .b -command {set x 1}",
		"namespace code {set x 1}",
		"namespace inscope ::n {set x 1}",
		"interp eval sub {set x 1}",
		"puts {set x 1}",
		"puts $c {set x 1}",
		"regsub {a} $s {set x 1}",
		"oo::define C method m {} {set x 1}",
		"oo::class create C { self method m {} {set x 1} }",
	}
	var cases []definitionCase
	for _, command := range commands {
		cases = append(cases, definitionCase{command, "proc f {} {\n  set x 0\n  " + command + "\n  puts $x\n}", "$x", []int{2}})
	}
	runDefinitionCases(t, cases)
}

// In a command whose scripts run here, a word spelled like a member keyword is a
// pattern or a variable name, so its bindings still count. Tcl 8.6 sets x in each.
func TestDefinitionMemberKeywordsInControlCommands(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"switch pattern method", "proc f {k} {\n  set x 0\n  switch $k method {set x 1} default {set x 2}\n  puts $x\n}", "$x", []int{2, 3, 3}},
		{"switch pattern destructor", "proc f {k} {\n  set x 0\n  switch $k a {set x 1} destructor {set x 2}\n  puts $x\n}", "$x", []int{2, 3, 3}},
		{"control, switch pattern meth", "proc f {k} {\n  set x 0\n  switch $k meth {set x 1} default {set x 2}\n  puts $x\n}", "$x", []int{2, 3, 3}},
		{"switch pattern method, no prior", "proc f {k} {\n  switch $k method {set x 1} default {set x 2}\n  puts $x\n}", "$x", []int{2, 2}},
		{"loop variable constructor in a catch", "proc f {l} {\n  catch { foreach constructor $l { set x 1 } }\n  puts $x\n}", "$x", []int{2}},
		{"control, loop variable ctor in a catch", "proc f {l} {\n  catch { foreach ctor $l { set x 1 } }\n  puts $x\n}", "$x", []int{2}},
		{"lmap variable constructor", "proc f {l} {\n  set x 0\n  lmap constructor $l { set x 1 }\n  puts $x\n}", "$x", []int{2, 3}},
		{"try handler variable list destructor", "proc f {} {\n  set x 0\n  try {\n    puts hi\n  } on error destructor {\n    set x 1\n  }\n  puts $x\n}", "$x", []int{2, 6}},
	})
}

// A use in an unknown command's body used to take the earliest binding, even one that
// had been killed. It is answered from what reaches it, and the bindings of bodies it
// is not in behave as before: the last row group is the table this rule must not move.
func TestDefinitionUseInsideAnUnknownBody(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"a killed binding", "proc f {} {\n  set x 1\n  set x 2\n  with { puts $x }\n}", "$x", []int{3}},
		{"both branches", "proc f {c} {\n  if {$c} {\n    set x 1\n  } else {\n    set x 2\n  }\n  with { puts $x }\n}", "$x", []int{3, 5}},
		{"a kill in the same body", "proc f {} {\n  with {\n    set x 1\n    set x 2\n    puts $x\n  }\n}", "$x", []int{4}},

		{"table: only binding is in a body", "proc f {} {\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"table: a prior binding hides the body's", "proc f {} {\n  set x 0\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"table: data in puts", "proc f {} {\n  set x 0\n  puts {set x 1}\n  puts $x\n}", "$x", []int{2}},
		{"table: uplevel", "proc f {} {\n  set x 0\n  uplevel 1 {set x 1}\n  puts $x\n}", "$x", []int{2}},
		{"table: after", "proc f {} {\n  set x 0\n  after 100 {set x 1}\n  puts $x\n}", "$x", []int{2}},
		{"table: use in a body", "proc f {} {\n  set x 0\n  with { puts $x }\n}", "$x", []int{2}},
		{"table: use in a body before a set", "proc f {} {\n  with { puts $x }\n  set x 1\n}", "$x", nil},
		{"table: proc in a body", "proc f {} {\n  with { proc g {} { set x 1 } }\n  puts $x\n}", "$x", nil},
		{"table: binding in a body below the use", "proc f {} {\n  puts $x\n  with { set x 1 }\n}", "$x", nil},
		{"table: eval with a prior binding", "proc f {} {\n  set x 0\n  eval { set x 1 }\n  puts $x\n}", "$x", []int{2, 3}},
		{"table: eval alone", "proc f {} {\n  eval { set x 1 }\n  puts $x\n}", "$x", []int{2}},
	})
}

// Only `eval {script}` runs its brace as a script here. With more words eval
// concatenates them and the head decides, so the brace is like any unknown command's.
func TestDefinitionEvalForms(t *testing.T) {
	var cases []definitionCase
	for _, command := range []string{
		"eval after 100 {set x 1}",
		"eval uplevel 1 {set x 1}",
		"eval namespace eval ::n {set x 1}",
		"eval lappend l {set x 1}",
	} {
		cases = append(cases, definitionCase{command, "proc f {} {\n  set x 0\n  " + command + "\n  puts $x\n}", "$x", []int{2}})
	}
	cases = append(cases,
		definitionCase{"eval {set x 1}", "proc f {} {\n  set x 0\n  eval {set x 1}\n  puts $x\n}", "$x", []int{2, 3}},
		definitionCase{"eval of a proc definition, prior", "proc f {} {\n  set x 0\n  eval proc g {} {set x 1}\n  puts $x\n}", "$x", []int{2}},
		definitionCase{"eval with two scripts is not one script", "proc f {} {\n  set x 0\n  eval {set y 1} {set x 1}\n  puts $x\n}", "$x", []int{2}},
	)
	runDefinitionCases(t, cases)
}

// A malformed control command with a proc shape must not run the proc's body here,
// with the guard against it gone from analyzeConservative.
func TestDefinitionControlCommandWithAProcShape(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"catch", "proc f {} {\n  catch proc g {} {set x 1}\n  puts $x\n}", "$x", nil},
		{"try", "proc f {} {\n  try proc g {} {set x 1}\n  puts $x\n}", "$x", nil},
	})
}

// With no other binding, a member's body still answers nothing: it does not run here.
func TestDefinitionMemberBodiesNeverAnswer(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"oo::define method", "proc f {} {\n  oo::define C method m {} {set x 1}\n  puts $x\n}", "$x", nil},
		{"oo::define constructor", "proc f {} {\n  oo::define C constructor {} {set x 1}\n  puts $x\n}", "$x", nil},
		{"oo::define destructor", "proc f {} {\n  oo::define C destructor {set x 1}\n  puts $x\n}", "$x", nil},
		{"self method in a definition script", "proc f {} {\n  oo::class create C { self method m {} {set x 1} }\n  puts $x\n}", "$x", nil},
	})
}

// Tcl substitutes a command's words once, before it runs, so a use in an unbraced test
// sees only what precedes the command. Only a braced test re-runs at the header.
func TestDefinitionUnbracedLoopTests(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"while $x", "proc f {} {\n  set x 0\n  while $x {\n    set x 1\n  }\n}", "$x", []int{2}},
		{"while [more $x]", "proc f {} {\n  set x 0\n  while [more $x] {\n    set x 1\n  }\n}", "$x", []int{2}},
		{"while quoted [more $x]", "proc f {} {\n  set x 0\n  while \"[more $x]\" {\n    set x 1\n  }\n}", "$x", []int{2}},
		{"for test $i, bound only by start", "proc f {} {\n  for {set i 0} $i {incr i} {\n  }\n}", "$i", nil},
		{"for test [more $i] sees what precedes the for", "proc f {} {\n  set i 9\n  for {set i 0} [more $i] {} {\n  }\n}", "$i", []int{2}},
		{"for test, braced, does not see what start killed", "proc f {} {\n  set i 9\n  for {set i 0} {[more $i]} {} {\n  }\n}", "$i", []int{3}},
	})
}

// finally sees the bindings of the main body and the handlers, not its own later ones.
func TestDefinitionFinallyDoesNotSeeItsOwnLaterBinding(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"no prior", "proc f {} {\n  try {\n    puts hi\n  } finally {\n    puts $x\n    set x 1\n  }\n}", "$x", nil},
		{"prior", "proc f {} {\n  set x 0\n  try {\n    puts hi\n  } finally {\n    puts $x\n    set x 1\n  }\n}", "$x", []int{2}},
		{"own earlier binding", "proc f {} {\n  try {\n    puts hi\n  } finally {\n    set x 1\n    puts $x\n  }\n}", "$x", []int{5}},
		{"back edge in a loop", "proc f {} {\n  while {1} {\n    try {\n      puts hi\n    } finally {\n      puts $x\n      set x 1\n    }\n  }\n}", "$x", []int{7}},
		{"after the try it still reaches", "proc f {} {\n  try {\n    puts hi\n  } finally {\n    set x 1\n  }\n  puts $x\n}", "$x", []int{5}},
	})
}

// The same empty reaching answers, in a page: an .rvt must not fall back past them.
func TestDefinitionEmptyReachingAnswerInRVT(t *testing.T) {
	cases := []definitionCase{
		{"earlier sibling switch arm", "proc f {k} {\n  switch $k {\n    a { set x 1 }\n    b { puts $x }\n  }\n}", "$x", nil},
		{"set x $x", "proc f {} {\n  set x $x\n}", "$x", nil},
		{"else branch", "proc f {} {\n  if {$c} {\n    set x 1\n  } else {\n    puts $x\n  }\n}", "$x", nil},
		{"ivar with a sibling branch's set", "itcl::class C {\n  protected variable n 0\n  method m {} {\n    if {$c} {\n      set n 1\n    } else {\n      puts $n\n    }\n  }\n}", "$n", []int{2}},
		{"a reaching binding still answers", "proc f {} {\n  set x 1\n  puts $x\n}", "$x", []int{2}},
	}
	const page = "page.rvt"
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf(".tcl lines = %v, want %v", got, c.want)
			}
			content := "<?\n" + c.src + "\n?>"
			ix := index.New()
			ix.IndexFile(page, content)
			var lines, want []int
			for _, loc := range New(ix).Definition(page, content, strings.LastIndex(content, c.mark)+1) {
				lines = append(lines, lineOfOffset(content, loc.NameStart))
			}
			for _, line := range c.want {
				want = append(want, line+1) // the page's `<?` line comes first
			}
			if !slices.Equal(lines, want) {
				t.Errorf(".rvt lines = %v, want %v\nsource:\n%s", lines, want, content)
			}
		})
	}
}

// `array set` is the one proc-frame binding form that Defs had and the analyzer did
// not, so `array names` below it would otherwise go silent. It merges into the array,
// so it adds to what reaches instead of replacing it.
func TestDefinitionArraySetIsABinding(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"array names after array set", "proc f {} {\n  array set cfg {a 1}\n  puts [array names cfg]\n}", " cfg]", []int{2}},
		{"array set merges with an earlier array set", "proc f {} {\n  array set cfg {a 1}\n  array set cfg {b 2}\n  puts [array names cfg]\n}", " cfg]", []int{2, 3}},
		{"element use after array set", "proc f {} {\n  array set cfg {a 1}\n  puts $cfg(a)\n}", "$cfg", []int{2}},
		{"array names before array set", "proc f {} {\n  puts [array names cfg]\n  array set cfg {a 1}\n}", " cfg]", nil},
	})
}

// A while or for test holds [substitutions] that run at the loop header on every
// pass, after a for's start script. Other loop heads evaluate their words once.
func TestDefinitionLoopHeadUses(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"for test sees start and next", "proc f {} {\n  for {set i 0} {[more $i]} {incr i} {\n  }\n}", "$i", []int{2, 2}},
		{"for test does not see a binding the start killed", "proc f {} {\n  set i 9\n  for {set i 0} {[more $i]} {} {\n  }\n}", "$i", []int{3}},
		{"while test sees the back edge", "proc f {} {\n  set x 0\n  while {[more $x]} {\n    set x 1\n  }\n}", "$x", []int{2, 4}},
		{"foreach list is evaluated once", "proc f {l} {\n  foreach i $l {\n    set l 3\n  }\n}", "$l", []int{1}},
		{"for continue still runs next", "proc f {} {\n  for {} {1} {set x 2} {\n    puts $x\n    set x 1\n    continue\n  }\n}", "$x", []int{2}},
	})
}

// Hunting: the guard compares the cursor with the binding's *name*, so a use that is
// inside the very command that binds the name is not "before" it by position.
// Tcl evaluates the arguments first, so with no earlier binding nothing reaches
// (`set x $x` fails with "no such variable"). NOT in the brief: the rule
// `NameStart > offset` still lets the fallback answer with the command's own binding.
func TestDefinitionUseInsideItsOwnFirstBinding(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"set x $x", "proc f {} {\n  set x $x\n}", "$x", nil},
		{"set x [string trim $x]", "proc f {} {\n  set x [string trim $x]\n}", "$x", nil},
		{"lappend x $x", "proc f {} {\n  lappend x $x\n}", "$x", nil},
	})
}

// The same shapes where an answer is right, and the same-line neighbours of the
// guard: an earlier binding reaches a self-referencing set, a loop carries the
// command's own binding around, and a use on the line of a binding is judged by
// position, not by line.
func TestDefinitionUseOnTheSameLineAsABinding(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"set x $x, with an earlier binding", "proc f {} {\n  set x 0\n  set x [string trim $x]\n}", "$x", []int{2}},
		{"set x $x in a loop reaches itself", "proc f {} {\n  while {1} {\n    set x [string trim $x]\n  }\n}", "$x", []int{3}},
		{"use before a binding later on the same line", "proc f {} {\n  puts $x; set x 1\n}", "$x", nil},
		{"use after a binding earlier on the same line", "proc f {} {\n  set x 1; puts $x\n}", "$x", []int{2}},
		{"brace-less one-liner, use first", "proc f {} { puts $x; set x 1 }", "$x", nil},
		{"brace-less one-liner, binding first", "proc f {} { set x 1; puts $x }", "$x", []int{1}},
	})
}

// Hunting: the reaching analysis reports an empty answer for a use no binding
// reaches. Where a use inside a try handler, a switch arm or an else branch is the
// only mention of a name bound only by a sibling, the fallback must stay silent, not
// jump to the sibling. The cases with the sibling BELOW the use are decided by the
// position guard. The ones with the sibling ABOVE it (`earlier sibling`, `else
// branch`, `handler use`) are NOT in the brief: a position-only guard still returns
// the sibling's binding, so the fallback also has to respect an empty reaching answer.
func TestDefinitionSiblingClauseBindingIsNotAnAnswer(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"handler use, sibling handler binds", "proc f {} {\n  try {\n    puts hi\n  } on error {} {\n    set x 1\n  } on ok {} {\n    puts $x\n  }\n}", "$x", nil},
		{"switch arm use, sibling arm binds", "proc f {k} {\n  switch $k {\n    a { puts $x }\n    b { set x 1 }\n  }\n}", "$x", nil},
		{"switch arm use, earlier sibling arm binds", "proc f {k} {\n  switch $k {\n    a { set x 1 }\n    b { puts $x }\n  }\n}", "$x", nil},
		{"else branch use, if branch binds", "proc f {} {\n  if {$c} {\n    set x 1\n  } else {\n    puts $x\n  }\n}", "$x", nil},
		{"switch inline arm use, sibling arm binds", "proc f {k} {\n  switch $k a { puts $x } b { set x 1 }\n}", "$x", nil},
		{"handler variable name used before any binding", "proc f {} {\n  try {\n    error boom\n  } on error {msg opts} {\n    puts $msg\n    set msg x\n  }\n}", "$msg", nil},
		{"catch body use, binding after the catch", "proc f {} {\n  catch {\n    puts $x\n  }\n  set x 1\n}", "$x", nil},
	})
}

// procWithInnerLength builds `proc f {HEADER} {...}` whose body interior is exactly
// innerLength bytes: a newline, comment filler, then tail. The interior length is what
// the reaching analysis compares with its 200,000-byte limit.
func procWithInnerLength(header string, innerLength int, tail string) string {
	fillerLength := innerLength - 1 - len(tail)
	var filler strings.Builder
	for fillerLength >= 100 {
		filler.WriteString("#" + strings.Repeat("x", 98) + "\n")
		fillerLength -= 100
	}
	switch {
	case fillerLength == 1:
		filler.WriteString("\n")
	case fillerLength >= 2:
		filler.WriteString("#" + strings.Repeat("x", fillerLength-2) + "\n")
	}
	return "proc f {" + header + "} {\n" + filler.String() + tail + "}\n"
}

// Hunting: an off-by-one around the 200,000-byte limit. Just under, exactly at, and
// just over it must give the same answers, because the position guard is in both
// the reaching path and the fallback.
func TestDefinitionOversizedProcBoundary(t *testing.T) {
	const limit = 200000
	for _, innerLength := range []int{limit - 1, limit, limit + 1} {
		useBeforeSet := procWithInnerLength("", innerLength, "  puts $x\n  set x 1\n")
		setBeforeUse := procWithInnerLength("", innerLength, "  set x 1\n  puts $x\n")
		paramThenSet := procWithInnerLength("x", innerLength, "  puts $x\n  set x 1\n")

		// The cases below only mean something if the interior is the length asked for.
		for _, src := range []string{useBeforeSet, setBeforeUse, paramThenSet} {
			start := strings.Index(src, "{\n") + 1
			end := strings.LastIndex(src, "}")
			if got := end - start; got != innerLength {
				t.Fatalf("test bug: interior is %d bytes, want %d", got, innerLength)
			}
		}
		_, reachingRan := tcl.ReachingAt(setBeforeUse, strings.LastIndex(setBeforeUse, "$x")+1)
		if wantReaching := innerLength <= limit; reachingRan != wantReaching {
			t.Fatalf("test bug: interior %d: reaching ran = %v, want %v", innerLength, reachingRan, wantReaching)
		}

		setLine := lineOfOffset(setBeforeUse, strings.Index(setBeforeUse, "set x 1"))
		label := "interior " + strconv.Itoa(innerLength)
		runDefinitionCases(t, []definitionCase{
			{label + ", use before set", useBeforeSet, "$x", nil},
			{label + ", set before use", setBeforeUse, "$x", []int{setLine}},
			{label + ", param then later set", paramThenSet, "$x", []int{1}},
		})
	}
}

// oversizedProc wraps tail in a proc whose body is over the 200,000-byte reaching
// limit, using filler that binds nothing.
func oversizedProc(tail string) string {
	return "proc f {} {\n" + strings.Repeat("  # filler that binds nothing\n", 8000) + tail + "}\n"
}

func TestDefinitionOversizedProcGuard(t *testing.T) {
	useBeforeSet := oversizedProc("  puts $x\n  set x 1\n")
	setBeforeUse := oversizedProc("  set x 1\n  puts $x\n")

	// Without this, the cases below could pass through reaching instead of the fallback.
	for _, src := range []string{useBeforeSet, setBeforeUse} {
		if _, ok := tcl.ReachingAt(src, strings.LastIndex(src, "$x")+1); ok {
			t.Fatalf("test bug: reaching analysis accepted a %d-byte source, want the oversized path", len(src))
		}
	}

	if got := defLinesAt(t, useBeforeSet, "$x"); got != nil {
		t.Errorf("use before its only binding = lines %v, want nil", got)
	}
	want := []int{lineOfOffset(setBeforeUse, strings.Index(setBeforeUse, "set x 1"))}
	if got := defLinesAt(t, setBeforeUse, "$x"); !slices.Equal(got, want) {
		t.Errorf("use after its binding = lines %v, want %v", got, want)
	}
}

func TestDefinitionCursorOnBindingAnswersItself(t *testing.T) {
	cases := []struct{ name, src, binding, command string }{
		{"set", "proc f {} {\n  set x 1\n  puts $x\n}", "set x 1", "set "},
		{"first incr", "proc f {} {\n  incr x\n  puts $x\n}", "incr x", "incr "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nameStart := strings.Index(c.src, c.binding) + len(c.command)
			locs := resolverOverGuardFile(c.src).Definition(guardFile, c.src, nameStart)
			if len(locs) != 1 || locs[0].NameStart != nameStart {
				t.Fatalf("Definition on the binding at %d = %#v, want only that binding", nameStart, locs)
			}
		})
	}
}

// Hunting: the position guard against a loop-carried answer. A binding below the
// use is exactly right when a back edge carries it up, so the guard must not run
// ahead of the reaching answer and silence it.
func TestDefinitionLoopCarriedBindingBelowTheUse(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"inner loop binding, outer use", "proc f {} {\n  while {1} {\n    puts $x\n    while {1} {\n      set x 1\n    }\n  }\n}", "$x", []int{5}},
		{"continue and normal end", "proc f {} {\n  while {1} {\n    puts $x\n    if {$c} {\n      set x 1\n      continue\n    }\n    set x 2\n  }\n}", "$x", []int{5, 8}},
		{"lmap", "proc f {l} {\n  lmap i $l {\n    if {$i} { puts $prev }\n    set prev $i\n  }\n}", "$prev", []int{4}},
		{"catch with a prior binding", "proc f {} {\n  set x 0\n  while {1} {\n    catch {\n      puts $x\n    }\n    set x 1\n  }\n}", "$x", []int{2, 7}},
		{"try handler", "proc f {} {\n  while {1} {\n    try {\n      set x 1\n    } on error {} {\n      puts $x\n    }\n    set x 9\n  }\n}", "$x", []int{4, 8}},
		{"switch sibling arm", "proc f {l} {\n  foreach k $l {\n    switch $k {\n      a { puts $x }\n      b { set x 1 }\n    }\n  }\n}", "$x", []int{5}},
		{"loop variable rebound each iteration", "proc f {l} {\n  foreach item $l {\n    puts $item\n    set item zzz\n  }\n}", "$item", []int{2}},
	})
}

// Hunting: the cursor sits on a binding's name at its first, middle and last byte,
// and one byte past the end. Each byte on the name answers the binding itself and
// only it; the byte past the end is whitespace and answers nothing. Array elements
// take the fallback path, so the guard decides these.
func TestDefinitionCursorAcrossBindingNameBytes(t *testing.T) {
	cases := []struct{ name, src, command, bindingName string }{
		{"set", "proc f {} {\n  set counter 1\n  puts $counter\n}", "set ", "counter"},
		{"first incr", "proc f {} {\n  incr counter\n  puts $counter\n}", "incr ", "counter"},
		{"first lappend", "proc f {} {\n  lappend items a\n  puts $items\n}", "lappend ", "items"},
		{"first append", "proc f {} {\n  append text a\n  puts $text\n}", "append ", "text"},
		{"array element", "proc f {} {\n  set cells(1,2) 1\n  puts $cells(1,2)\n}", "set ", "cells"},
		{"first incr of an array element", "proc f {} {\n  incr cnt(x)\n  puts $cnt(x)\n}", "incr ", "cnt"},
		{"single-byte name", "proc f {} {\n  set x 1\n  puts $x\n}", "set ", "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nameStart := strings.Index(c.src, c.command+c.bindingName) + len(c.command)
			resolver := resolverOverGuardFile(c.src)
			for _, delta := range []int{0, len(c.bindingName) / 2, len(c.bindingName) - 1} {
				locs := resolver.Definition(guardFile, c.src, nameStart+delta)
				if len(locs) != 1 || locs[0].NameStart != nameStart {
					t.Errorf("cursor %d bytes into the name at %d: %#v, want only that binding", delta, nameStart, locs)
				}
			}
			if locs := resolver.Definition(guardFile, c.src, nameStart+len(c.bindingName)); len(locs) != 0 {
				t.Errorf("cursor one past the name: %#v, want nothing", locs)
			}
		})
	}
}
