package tcl

import (
	"slices"
	"strings"
	"testing"
)

// indexLast returns the byte offset of the last occurrence of sub in s, or -1.
func indexLast(s, sub string) int {
	return strings.LastIndex(s, sub)
}

// reachAtMarker returns the reaching-def name ranges for the variable use whose
// text starts at marker `mark` in src. +1 lands inside `$x` on the name.
func reachAtMarker(t *testing.T, src, mark string) []Definition {
	t.Helper()
	off := indexOf(t, src, mark) + 1 // +1 to land inside `$x` on the name
	defs, ok := ReachingAt(src, off)
	if !ok {
		t.Fatalf("ReachingAt(%q) ok=false", mark)
	}
	return defs
}

func indexOf(t *testing.T, src, sub string) int {
	t.Helper()
	i := indexLast(src, sub)
	if i < 0 {
		t.Fatalf("substring %q not found", sub)
	}
	return i
}

func TestReachingStraightLineLatestAssignment(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  set x 2\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x")
	if len(defs) != 1 {
		t.Fatalf("want 1 reaching def, got %d: %#v", len(defs), defs)
	}
	// It must be the SECOND `set x`, not the first.
	wantStart := indexLast(src, "set x 2") + len("set ")
	if defs[0].NameStart != wantStart {
		t.Fatalf("reaching def at %d, want the `set x 2` binding at %d", defs[0].NameStart, wantStart)
	}
}

func TestReachingParamReaches(t *testing.T) {
	src := "proc f {a} {\n  puts $a\n}"
	defs := reachAtMarker(t, src, "$a")
	if len(defs) != 1 || defs[0].Name != "a" {
		t.Fatalf("param should reach: %#v", defs)
	}
}

func TestReachingIfElseJoin(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  if {$c} {\n    set x 2\n  } else {\n    set x 3\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x")
	if len(defs) != 2 { // both branches reassign; set x 1 is dead
		t.Fatalf("want 2 reaching defs (x2, x3), got %d: %#v", len(defs), defs)
	}
}

func TestReachingIfNoElseKeepsPrior(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  if {$c} {\n    set x 2\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x")
	if len(defs) != 2 { // fall-through keeps x1; if-branch adds x2
		t.Fatalf("want 2 (x1 fall-through + x2), got %d: %#v", len(defs), defs)
	}
}

func TestReachingLoopCarried(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  while {$c} {\n    set y $x\n    set x 9\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // the `puts $x` after the loop
	if len(defs) != 2 {                 // x0 (zero iterations) or x9 (ran)
		t.Fatalf("want 2 reaching defs (x0, x9), got %d: %#v", len(defs), defs)
	}
}

func TestReachingUseInsideLoopSeesBackEdge(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  while {$c} {\n    set y $x\n    set x 9\n  }\n}"
	defs := reachAtMarker(t, src, "$x") // x0 (first iteration) or x9 (later iterations)
	if len(defs) != 2 {
		t.Fatalf("want 2 reaching defs (x0, x9), got %d: %#v", len(defs), defs)
	}
}

func TestReachingTryWithUnbracedBody(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  try $script on error {} {\n    puts $x\n  }\n}"
	defs := reachAtMarker(t, src, "$x") // the handler must not be mistaken for the main body
	if len(defs) != 1 {
		t.Fatalf("want 1 reaching def (x1), got %d: %#v", len(defs), defs)
	}
}

func TestReachingForeachVar(t *testing.T) {
	src := "proc f {items} {\n  foreach it $items {\n    puts $it\n  }\n}"
	defs := reachAtMarker(t, src, "$it")
	if len(defs) != 1 || defs[0].Name != "it" {
		t.Fatalf("foreach var should reach its use: %#v", defs)
	}
}

func TestReachingEarlyReturnBranch(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  if {$c} {\n    return\n  } else {\n    set x 2\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // return branch dead; else assigns x2; has else → x2 only
	if len(defs) != 1 {
		t.Fatalf("want 1 reaching def (x2), got %d: %#v", len(defs), defs)
	}
}

func TestReachingBreakExit(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  while {$c} {\n    set x 1\n    if {$d} { break }\n    set x 2\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // x0 (0 iters), x1 (broke), x2 (end of iter)
	if len(defs) != 3 {
		t.Fatalf("want 3 reaching defs (x0,x1,x2), got %d: %#v", len(defs), defs)
	}
}

func TestReachingCatchConservative(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  catch {\n    set x 2\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // x1 (error before set) OR x2
	if len(defs) != 2 {
		t.Fatalf("want 2 reaching defs (x1,x2), got %d: %#v", len(defs), defs)
	}
}

func TestReachingSwitchArms(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  switch $k {\n    a { set x 1 }\n    b { set x 2 }\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // x0 + x1 + x2 (conservative join)
	if len(defs) != 3 {
		t.Fatalf("want 3 reaching defs (x0,x1,x2), got %d: %#v", len(defs), defs)
	}
}

func TestReachingContinueExit(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  while {$c} {\n    set x 1\n    continue\n  }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // x0 (0 iters) AND x1 (iteration completed via continue)
	if len(defs) != 2 {
		t.Fatalf("want 2 reaching defs (x0, x1) — continue must not drop x1, got %d: %#v", len(defs), defs)
	}
}

func TestReachingSwitchInlineArms(t *testing.T) {
	src := "proc f {} {\n  set x 0\n  switch $k a { set x 1 } b { set x 2 }\n  puts $x\n}"
	defs := reachAtMarker(t, src, "$x") // x0 + x1 + x2 (no arm dropped)
	if len(defs) != 3 {
		t.Fatalf("want 3 reaching defs (x0,x1,x2) — inline switch must not drop arms, got %d: %#v", len(defs), defs)
	}
}

func TestReachingSizeCapFallsBack(t *testing.T) {
	var b strings.Builder
	b.WriteString("proc f {} {\n")
	for i := 0; i < 40000; i++ { // ~ > 200 KB of body
		b.WriteString("  set x 1\n")
	}
	b.WriteString("  puts $x\n}")
	src := b.String()
	off := indexLast(src, "$x") + 1
	if _, ok := ReachingAt(src, off); ok {
		t.Fatalf("expected ok=false (fallback) for oversized proc body")
	}
}

func TestReachingRMWTargetIsUse(t *testing.T) {
	src := "proc f {} {\n  set x 1\n  incr x\n}"
	off := strings.Index(src, "incr x") + len("incr ") // the `x` of `incr x`
	defs, ok := ReachingAt(src, off)
	if !ok || len(defs) != 1 {
		t.Fatalf("rmw target should be a use: ok=%v defs=%#v", ok, defs)
	}
	if defs[0].NameStart != strings.Index(src, "set x 1")+len("set ") {
		t.Fatalf("want the prior `set x 1`, got %#v", defs)
	}
}

// ---------------------------------------------------------------------------
// Edge cases: bindings that cannot reach, and loop-carried bindings that can.
// ---------------------------------------------------------------------------

// reachLinesAt returns the sorted 1-based source lines of the bindings that reach
// the use whose text starts at the last occurrence of mark (cursor one byte in).
// Duplicates are kept so a repeated answer shows up. nil means the analyzer visited
// the use and nothing reaches it; a use it never found fails the test, because the
// resolver treats those two results differently.
func reachLinesAt(t *testing.T, src, mark string) []int {
	t.Helper()
	markOffset := strings.LastIndex(src, mark)
	if markOffset < 0 {
		t.Fatalf("test bug: mark %q not found in %q", mark, src)
	}
	defs, ok := ReachingAt(src, markOffset+1)
	if !ok {
		t.Fatalf("analyzer did not find the use %q (ok=false)\nsource:\n%s", mark, src)
	}
	var lines []int
	for _, def := range defs {
		lines = append(lines, 1+strings.Count(src[:def.NameStart], "\n"))
	}
	slices.Sort(lines)
	return lines
}

type reachCase struct {
	name, src, mark string
	want            []int
}

func runReachCases(t *testing.T, cases []reachCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reachLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("binding lines reaching %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// Hunting: first-visit-wins in a loop. A use is visited once per fixpoint
// iteration, and its answer must be the union over every visit. Each case has a
// binding that reaches the use only on a later visit (a back edge).
func TestReachingLoopUsesUnionEveryVisit(t *testing.T) {
	runReachCases(t, []reachCase{
		{"nested loops, outer binding reaches inner use", `proc f {} {
  set x 0
  while {1} {
    while {2} {
      puts $x
    }
    set x 1
  }
}`, "$x", []int{2, 7}},
		{"inner binding reaches an outer use through the outer back edge", `proc f {} {
  while {1} {
    puts $x
    while {1} {
      set x 1
    }
  }
}`, "$x", []int{5}},
		{"loop inside if inside loop", `proc f {} {
  set x 0
  while {1} {
    if {$c} {
      while {1} {
        puts $x
      }
    }
    set x 1
  }
}`, "$x", []int{2, 9}},
		{"use on a break path still sees the later binding of the next iteration", `proc f {} {
  set x 0
  while {1} {
    if {$c} {
      puts $x
      break
    }
    set x 1
  }
}`, "$x", []int{2, 8}},
		{"continue path and normal end both feed the back edge", `proc f {} {
  while {1} {
    puts $x
    if {$c} {
      set x 1
      continue
    }
    set x 2
  }
}`, "$x", []int{5, 8}},
		{"lmap carried use", `proc f {l} {
  lmap i $l {
    if {$i} { puts $prev }
    set prev $i
  }
}`, "$prev", []int{4}},
		{"dict for carried use", `proc f {d} {
  dict for {k v} $d {
    puts $last
    set last $k
  }
}`, "$last", []int{4}},
		{"while carried use through a try main body", `proc f {} {
  set x 0
  while {1} {
    try {
      puts $x
    } on error {} {
    }
    set x 1
  }
}`, "$x", []int{2, 8}},
		{"try handler in a loop sees a binding made after the try", `proc f {} {
  while {1} {
    try {
      set x 1
    } on error {} {
      puts $x
    }
    set x 9
  }
}`, "$x", []int{4, 8}},
		{"finally binding reaches the main body on the next iteration", `proc f {} {
  while {1} {
    try {
      puts $x
    } finally {
      set x 1
    }
  }
}`, "$x", []int{6}},
		{"catch in a loop sees the binding made after it", `proc f {} {
  set x 0
  while {1} {
    catch {
      puts $x
    }
    set x 1
  }
}`, "$x", []int{2, 7}},
		{"switch arm in a loop sees the later binding of the same arm", `proc f {l} {
  foreach k $l {
    switch $k {
      a {
        puts $x
        set x 1
      }
      b { puts hi }
    }
  }
}`, "$x", []int{6}},
	})
}

// Hunting: a union that forgets kills. A binding that always runs before the use
// in the same iteration must hide everything from the back edge and from before.
func TestReachingLoopUnionRespectsKillsInTheSameIteration(t *testing.T) {
	runReachCases(t, []reachCase{
		{"set, use, set again: only the first reaches", `proc f {} {
  set x 0
  while {1} {
    set x 1
    puts $x
    set x 2
  }
}`, "$x", []int{4}},
		{"no kill: the one binding is listed once, not once per visit", `proc f {} {
  set x 0
  while {1} {
    puts $x
  }
}`, "$x", []int{2}},
		{"binding reaching through several visits is deduplicated", `proc f {} {
  set x 0
  while {1} {
    while {1} {
      while {1} {
        puts $x
      }
    }
  }
}`, "$x", []int{2}},
	})
}

// Hunting: the isRMW branch keeps its own first-visit guard. The cursor is on the
// name of incr/append/lappend, whose target is both a use and a binding.
func TestReachingReadModifyWriteInLoopUnionsEveryVisit(t *testing.T) {
	runReachCases(t, []reachCase{
		{"lappend with a prior binding", `proc f {l} {
  set acc {}
  foreach i $l {
    lappend acc $i
  }
}`, " acc", []int{2, 4}},
		{"incr with a prior binding", `proc f {} {
  set n 0
  while {1} {
    incr n
  }
}`, " n", []int{2, 4}},
		{"append with a prior binding", `proc f {} {
  set s {}
  while {1} {
    append s x
  }
}`, " s x", []int{2, 4}},
		{"incr as the first binding reaches itself through the back edge", `proc f {} {
  while {1} {
    incr n
  }
}`, " n", []int{3}},
		{"incr outside any loop with no prior binding reaches nothing", `proc f {} {
  incr n
}`, " n", nil},
		{"incr after a prior binding in straight-line code", `proc f {} {
  set n 0
  incr n
}`, " n", []int{2}},
	})
}

// Hunting: `for` runs its start script once, before the first test. Folding start
// into the iterated block makes it kill itself on every pass, so the body no longer
// sees the initial binding. Tcl: the body sees the start's `set i 0` (first pass) and
// the next script's `incr i` (later passes).
func TestReachingForLoopStartScriptReachesTheBody(t *testing.T) {
	src := `proc f {} {
  for {
    set i 0
  } {$i < 3} {
    incr i
  } {
    puts $i
  }
}`
	runReachCases(t, []reachCase{
		{"body use sees start and next", src, "$i\n", []int{3, 5}},
		{"next-script incr sees start and itself", src, " i\n  }", []int{3, 5}},
	})
}

// Hunting: a union that lets a reassigned loop variable leak across iterations.
// foreach/lmap/dict for rebind their variables at the top of every iteration, so
// `set item zzz` below the use never reaches it. Tcl: `foreach item {a b c} {lappend
// seen $item; set item zzz}` collects a b c. NOT in the brief: a naive union of
// visits answers two bindings here, a regression from today's single answer.
func TestReachingLoopVariableIsReboundEachIteration(t *testing.T) {
	runReachCases(t, []reachCase{
		{"foreach", `proc f {l} {
  foreach item $l {
    puts $item
    set item zzz
  }
}`, "$item", []int{2}},
		{"foreach, second variable", `proc f {l} {
  foreach {a b} $l {
    puts $b
    set b zzz
  }
}`, "$b", []int{2}},
		{"dict for", `proc f {d} {
  dict for {k v} $d {
    puts $k
    set k zzz
  }
}`, "$k", []int{2}},
	})
}

// Hunting: try entry views. The main body sees only what precedes the try; an
// on/trap handler sees that plus the main body's bindings (an error can arrive at
// any point in it), and never a sibling handler's; finally sees everything.
func TestReachingTryEntryViews(t *testing.T) {
	runReachCases(t, []reachCase{
		{"main body does not see a handler's binding", `proc f {} {
  try {
    puts $x
  } on error {} {
    set x 1
  }
}`, "$x", nil},
		{"handler does not see a sibling handler's binding", `proc f {} {
  set x 0
  try {
    puts hi
  } on error {} {
    set x 1
  } trap {A B} {} {
    puts $x
  }
}`, "$x", []int{2}},
		{"handler sees the main body but not the sibling handler", `proc f {} {
  try {
    set a 1
  } on error {} {
    set a 2
  } on ok {} {
    puts $a
  }
}`, "$a", []int{3}},
		{"handler sees every main-body binding, even an overwritten one", `proc f {} {
  try {
    set x 1
    set x 2
  } on error {} {
    puts $x
  }
}`, "$x", []int{3, 4}},
		{"handler sees a binding nested in an if in the main body", `proc f {} {
  try {
    if {$c} { set x 1 }
  } on error {} {
    puts $x
  }
}`, "$x", []int{3}},
		{"finally sees the main body and every handler", `proc f {} {
  set x 0
  try {
    set x 1
  } on error {} {
    set x 2
  } trap {A} {} {
    set x 3
  } finally {
    puts $x
  }
}`, "$x", []int{2, 4, 6, 8}},
		{"nested try: inner handler does not see the outer handler", `proc f {} {
  set x 0
  try {
    try {
      set x 1
    } on error {} {
      puts $x
    }
  } on error {} {
    set x 2
  }
}`, "$x", []int{2, 5}},
		{"nested try: outer handler sees the inner try's handler binding", `proc f {} {
  set x 0
  try {
    try {
      set x 1
    } on error {} {
      set x 2
    }
  } on error {} {
    puts $x
  }
}`, "$x", []int{2, 5, 7}},
		{"nested try: inner main body sees only what precedes the outer try", `proc f {} {
  set x 0
  try {
    try {
      puts $x
    } on error {} {
      set x 1
    }
  } on error {} {
    set x 2
  }
}`, "$x", []int{2}},
	})
}

// Hunting: the try argument scanner. A variable list spelled like a keyword must
// not start a second clause, and a script held in a variable is not a body.
func TestReachingTryClauseShapes(t *testing.T) {
	runReachCases(t, []reachCase{
		{"variable list named finally does not turn the handler body into finally", `proc f {} {
  set x 0
  try {
    set a 1
  } on error finally {
    puts $x
    set x 5
  }
}`, "$x", []int{2}},
		{"unbraced main body: handler does not see a sibling handler", `proc f {} {
  set x 0
  try $script on error {} {
    set x 1
  } on ok {} {
    puts $x
  }
}`, "$x", []int{2}},
		{"unbraced main body: first handler is not taken as the main body", `proc f {} {
  try $script on error {} {
    puts $x
  } on ok {} {
    set x 1
  }
}`, "$x", nil},
		{"unbraced main body: finally still sees the binding before the try", `proc f {} {
  set x 0
  try $script finally {
    puts $x
  }
}`, "$x", []int{2}},
		{"unbraced main body: finally sees the handler's binding", `proc f {} {
  try $script on error {} {
    set x 1
  } finally {
    puts $x
  }
}`, "$x", []int{3}},
	})
}

// Hunting: catch and mixed views. A catch body sees what precedes the catch;
// the code after the construct sees the join, which must stay conservative.
func TestReachingCatchEntryViews(t *testing.T) {
	mixed := `proc f {} {
  set x 0
  catch {
    puts $x ;# A
    set x 1
    puts $x ;# B
  }
  puts $x ;# C
}`
	runReachCases(t, []reachCase{
		{"catch body before its own set", mixed, "$x ;# A", []int{2}},
		{"catch body after its own set", mixed, "$x ;# B", []int{5}},
		{"after the catch: both may reach", mixed, "$x ;# C", []int{2, 5}},
		{"nested catch: inner body does not see the outer body's later binding", `proc f {} {
  set x 0
  catch {
    catch {
      puts $x
      set x 1
    }
    set x 2
  }
}`, "$x", []int{2}},
		{"catch inside try main body: sees the binding before, not the handler's", `proc f {} {
  set x 0
  try {
    catch {
      puts $x
    }
  } on error {} {
    set x 1
  }
}`, "$x", []int{2}},
		{"try inside catch: handler sees only the try's own main body", `proc f {} {
  set x 0
  catch {
    try {
      set y 1
    } on error {} {
      puts $x
    }
    set x 1
  }
}`, "$x", []int{2}},
	})
}

// Hunting: the exit set must stay exactly as conservative as it is today, so code
// after a try or a switch still sees every binding made inside any clause.
func TestReachingExitSetStaysConservative(t *testing.T) {
	runReachCases(t, []reachCase{
		{"after try with handlers and finally", `proc f {} {
  set x 0
  try {
    set x 1
  } on error {} {
    set x 2
  } finally {
    set x 3
  }
  puts $x
}`, "$x", []int{2, 4, 6, 8}},
		{"after a switch with fall-through arms", `proc f {k} {
  set x 0
  switch $k {
    a -
    b { set x 1 }
    c { set x 2 }
  }
  puts $x
}`, "$x", []int{2, 5, 6}},
	})
}

// Hunting: switch arms are alternatives. An arm sees what precedes the switch and
// never a sibling arm's binding, in either list or inline form, however deep.
func TestReachingSwitchArmEntryViews(t *testing.T) {
	runReachCases(t, []reachCase{
		{"fall-through arm does not see a later sibling", `proc f {k} {
  set x 0
  switch $k {
    a -
    b { puts $x }
    c { set x 1 }
  }
}`, "$x", []int{2}},
		{"inline form: sibling on the same line", `proc f {k} {
  set x 0
  switch $k a { puts $x } b { set x 1 }
}`, "$x", []int{2}},
		{"with options before the subject", `proc f {k} {
  set x 0
  switch -regexp -- $k {
    a { puts $x }
    b { set x 1 }
  }
}`, "$x", []int{2}},
		{"nested switch in an arm", `proc f {k j} {
  set x 0
  switch $k {
    a {
      switch $j {
        b { puts $x }
        c { set x 2 }
      }
    }
    d { set x 1 }
  }
}`, "$x", []int{2}},
		{"binding after the switch does not reach an arm", `proc f {k} {
  switch $k {
    a { puts $x }
  }
  set x 1
}`, "$x", nil},
		{"arm own binding before its use", `proc f {k} {
  switch $k {
    a {
      set x 1
      puts $x
    }
    b { set x 2 }
  }
}`, "$x", []int{4}},
		{"in a loop a sibling arm's binding legitimately reaches by the back edge", `proc f {l} {
  set x 0
  foreach k $l {
    switch $k {
      a { puts $x }
      b { set x 1 }
    }
  }
}`, "$x", []int{2, 6}},
		{"in a loop with no prior binding only the sibling reaches", `proc f {l} {
  foreach k $l {
    switch $k {
      a { puts $x }
      b { set x 1 }
    }
  }
}`, "$x", []int{5}},
	})
}

// A binding in an unknown command's body stays out of the reach sets and answers only
// a use that nothing else reaches.
func TestReachingUnknownCommandBodies(t *testing.T) {
	// This pin was "the analyzer does not find the use", which left the answer to the
	// earliest binding: `set x 1; set x 2; with { puts $x }` answered the killed `set`.
	// The analyzer now visits the use, in the body that holds it.
	inBody := "proc f {} {\n  set x 0\n  with { puts $x }\n}"
	if defs, ok := ReachingAt(inBody, strings.LastIndex(inBody, "$x")+1); !ok || len(defs) != 1 {
		t.Errorf("use inside an unknown body: ok=%v defs=%#v, want the analyzer to answer with the one reaching binding", ok, defs)
	}
	runReachCases(t, []reachCase{
		{"only binding is in a body above the use", "proc f {} {\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"a binding that reaches hides the one in a body", "proc f {} {\n  set x 0\n  with { set x 1 }\n  puts $x\n}", "$x", []int{2}},
		{"a body's binding kills nothing", "proc f {} {\n  set x 0\n  with { set x 1 }\n  with { set x 2 }\n  puts $x\n}", "$x", []int{2}},
		{"a binding in a body below the use does not answer", "proc f {} {\n  puts $x\n  with { set x 1 }\n}", "$x", nil},
		{"another name's binding does not answer", "proc f {} {\n  with { set y 1 }\n  puts $x\n}", "$x", nil},
	})
}

// A use in an unknown command's body is answered from what reaches the command, then
// through the body itself, so a killed binding does not answer it. The body's own
// bindings stop at the end of the body.
func TestReachingUseInsideAnUnknownBody(t *testing.T) {
	runReachCases(t, []reachCase{
		{"a killed binding does not reach", "proc f {} {\n  set x 1\n  set x 2\n  with { puts $x }\n}", "$x", []int{3}},
		{"both branches reach", "proc f {c} {\n  if {$c} {\n    set x 1\n  } else {\n    set x 2\n  }\n  with { puts $x }\n}", "$x", []int{3, 5}},
		{"a binding in the same body kills", "proc f {} {\n  with {\n    set x 1\n    set x 2\n    puts $x\n  }\n}", "$x", []int{4}},
		{"a binding below the use in the same body", "proc f {} {\n  with {\n    puts $x\n    set x 1\n  }\n}", "$x", nil},
		{"control flow inside the body", "proc f {c} {\n  set x 0\n  with {\n    if {$c} { set x 1 }\n    puts $x\n  }\n}", "$x", []int{2, 4}},
		{"a wrapper nested in a wrapper", "proc f {} {\n  set x 0\n  with {\n    set x 1\n    with { puts $x }\n  }\n}", "$x", []int{4}},
		{"inside a catch", "proc f {} {\n  set x 0\n  catch { with { puts $x } }\n}", "$x", []int{2}},
		{"a loop carries a binding to the body", "proc f {} {\n  set x 0\n  while {1} {\n    with { puts $x }\n    set x 1\n  }\n}", "$x", []int{2, 5}},
		{"another wrapper's binding answers only when nothing reaches", "proc f {} {\n  with { set x 1 }\n  with { puts $x }\n}", "$x", []int{2}},
		{"another wrapper's binding stays out of the reach set", "proc f {} {\n  set x 0\n  with { set x 1 }\n  with { puts $x }\n}", "$x", []int{2}},
		// The body's bindings must not leave it by a loop's exits either.
		{"continue does not carry the body's binding round", "proc f {} {\n  while {1} {\n    with { puts $x; set x 1; continue }\n  }\n}", "$x", nil},
		{"a later use is not affected by the body's break", "proc f {} {\n  set x 0\n  while {1} {\n    with { set x 1; break }\n  }\n  puts $x\n}", "$x", []int{2}},
		{"a new scope in a wrapper is analyzed on its own", "proc f {} {\n  set x 0\n  with { proc g {} { set x 1; puts $x } }\n}", "$x", []int{3}},
	})
}

func TestClassOfUsesABindingInAnUnknownBody(t *testing.T) {
	src := "proc f {} {\n  with { set d [::A #auto] }\n  $d field\n}"
	if got := ClassOf(src, strings.LastIndex(src, "$d")+1); !slices.Equal(got, []string{"::A"}) {
		t.Errorf("ClassOf = %v, want [::A]", got)
	}
}

// Hunting: ClassOf reads ReachingAt, so a wrong reaching set is a wrong method
// jump. A loop-carried instantiation must contribute its class; a sibling
// handler's must not.
func TestClassOfFollowsLoopBackEdgeAndTryHandlers(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{"object re-created later in the loop", "proc f {} {\n  set d [::A #auto]\n  while {1} {\n    $d field\n    set d [::B #auto]\n  }\n}", []string{"::A", "::B"}},
		{"sibling handler's class does not leak", "proc f {} {\n  set d [::A #auto]\n  try {\n    puts hi\n  } on error {} {\n    set d [::B #auto]\n  } on ok {} {\n    $d field\n  }\n}", []string{"::A"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassOf(c.src, strings.LastIndex(c.src, "$d")+1)
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("ClassOf = %v, want %v\nsource:\n%s", got, c.want, c.src)
			}
		})
	}
}

func TestClassOfSeesAnInstantiationInABracket(t *testing.T) {
	src := "proc f {} {\n  set o [::B #auto]\n  set rc [catch { set o [::A #auto] }]\n  $o m\n}"
	got := ClassOf(src, strings.LastIndex(src, "$o")+1)
	slices.Sort(got)
	if !slices.Equal(got, []string{"::A", "::B"}) {
		t.Errorf("ClassOf = %v, want [::A ::B]", got)
	}
}
