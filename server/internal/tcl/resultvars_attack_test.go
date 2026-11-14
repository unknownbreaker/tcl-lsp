package tcl

import (
	"sort"
	"strings"
	"testing"
)

// advNth returns the byte offset of the n-th (1-based) occurrence of sub in src,
// plus delta. It fails the test when there is no such occurrence.
func advNth(t *testing.T, src, sub string, n, delta int) int {
	t.Helper()
	from := 0
	for i := 0; i < n; i++ {
		k := strings.Index(src[from:], sub)
		if k < 0 {
			t.Fatalf("occurrence %d of %q not found", n, sub)
		}
		if i == n-1 {
			return from + k + delta
		}
		from += k + len(sub)
	}
	return -1
}

func advStarts(defs []Definition) []int {
	var s []int
	for _, d := range defs {
		s = append(s, d.NameStart)
	}
	sort.Ints(s)
	return s
}

func advCheck(t *testing.T, src string, use int, want ...int) {
	t.Helper()
	sort.Ints(want)
	defs, ok := ReachingAt(src, use)
	got := advStarts(defs)
	if !ok {
		t.Fatalf("ReachingAt not ok; want %v", want)
	}
	if len(got) != len(want) {
		t.Fatalf("reaching defs = %v, want %v (src offsets)", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("reaching defs = %v, want %v (src offsets)", got, want)
		}
	}
}

// Tcl 8.6/9.0: a `-` handler falls through to the next handler's body, but binds
// its OWN variable list, not the next one's. Prints "p1 m=boom r=pre".
const advFallthrough = `proc p1 {} {
  set m pre; set r pre
  try {error boom} on error {m} - on ok {r} { puts "$m $r" }
}`

func TestAttackResultVarsTryFallthroughBindsOwnVars(t *testing.T) {
	src := advFallthrough
	use := advNth(t, src, "$m", 1, 1)
	advCheck(t, src, use,
		advNth(t, src, "m pre", 1, 0), // ok path: m untouched
		advNth(t, src, "{m}", 1, 1),   // error path: the `-` handler's varlist
	)
}

func TestAttackResultVarsTryFallthroughNextVarsNotCertain(t *testing.T) {
	src := advFallthrough
	use := advNth(t, src, "$r", 1, 1)
	advCheck(t, src, use,
		advNth(t, src, "r pre", 1, 0), // error path enters this body without binding r
		advNth(t, src, "{r}", 1, 1),
	)
}

// Tcl 8.6/9.0 inside a proc (compiled catch): with result and options naming the
// same variable, the RESULT is written last. `catch {error x} m m; puts $m` prints
// "x". (At top level, uncompiled, the options win; this is a proc.)
func TestAttackResultVarsCatchSameVarResultWinsInProc(t *testing.T) {
	src := `proc p {} {
  catch {error x} m m
  puts $m
}`
	use := advNth(t, src, "$m", 1, 1)
	advCheck(t, src, use, advNth(t, src, "m m", 1, 0))
}

// `catch {*}$x msg`: with $x empty, msg is the script and nothing is bound
// (8.6/9.0 print "rc=1 msg=pre"); with one element msg is the result var. The
// roles of the words are unknown, so the catch binds nothing and, above all, must
// not kill `set msg pre`. Modeling the expansion is out of scope.
func TestAttackResultVarsCatchExpansionDoesNotKill(t *testing.T) {
	src := `proc p {} {
  set msg pre; set x {}
  catch {*}$x msg
  puts $msg
}`
	use := advNth(t, src, "$msg", 1, 1)
	advCheck(t, src, use, advNth(t, src, "msg pre", 1, 0))
}

// try handler with an array-element varlist writes a(x) (8.6/9.0 print
// "p13 a=boom"). The prior `set a(y)` must not be the only answer for $a(x).
func TestAttackResultVarsTryArrayElementVarNotIgnored(t *testing.T) {
	src := `proc p {} {
  set a(y) 1
  try {error b} on error {a(x)} { puts $a(x) }
}`
	use := advNth(t, src, "$a(x)", 1, 1)
	defs, ok := ReachingAt(src, use)
	setY := advNth(t, src, "a(y)", 1, 0)
	if ok && len(defs) == 1 && defs[0].NameStart == setY {
		t.Fatalf("$a(x) resolves only to `set a(y) 1`, but the handler wrote a(x); want the handler varlist or silence")
	}
}

// Sanity shapes expected to hold.
func TestAttackResultVarsCatchInIfCondition(t *testing.T) {
	src := `proc p {} {
  set msg pre
  if {[catch {error boom} msg]} { puts $msg }
}`
	use := advNth(t, src, "$msg", 1, 1)
	advCheck(t, src, use, advNth(t, src, "msg]", 1, 0))
}

func TestAttackResultVarsCatchLoopBackEdge(t *testing.T) {
	src := `proc p {l} {
  set msg pre
  foreach i $l { puts $msg; catch {foo} msg }
}`
	use := advNth(t, src, "$msg", 1, 1)
	advCheck(t, src, use, advNth(t, src, "msg pre", 1, 0), advNth(t, src, "msg }", 1, 0))
}

func TestAttackResultVarsTryOptsVarAfter(t *testing.T) {
	src := `proc p {} {
  set o pre
  try {error boom} on error {m o} { }
  puts $o
}`
	use := advNth(t, src, "$o", 1, 1)
	advCheck(t, src, use, advNth(t, src, "o pre", 1, 0), advNth(t, src, "m o}", 1, 2))
}

func TestAttackResultVarsFileDefsCatchAndTrap(t *testing.T) {
	src := `proc p {} {
  catch {error boom} msg opts
  try {error x} trap {A B} {e2 o2} {}
}`
	want := map[string]bool{"msg": false, "opts": false, "e2": false, "o2": false}
	for _, d := range FileDefs(src) {
		if d.Kind == DefLocal {
			if _, k := want[d.Name]; k {
				want[d.Name] = true
			}
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("FileDefs missing DefLocal %q", n)
		}
	}
}
