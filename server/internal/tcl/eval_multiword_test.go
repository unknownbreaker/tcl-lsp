package tcl

import (
	"strings"
	"testing"
)

// eval with more than one word concatenates its words as concat does and runs the
// result as a script in this frame, so `set x 0; eval set x 1; puts $x` prints 1
// (tclsh 8.6 and 9.0). The analysis does not read the joined script, so uses after it stay silent:
// a missed answer, never only `set x 0` (#132). Any literal head runs the builtin.
func TestReachingMultiWordEvalSilent(t *testing.T) {
	var cases []silentReachCase
	for _, h := range []string{"eval", `"eval"`, "{eval}", "::eval", "{*}eval"} {
		cases = append(cases,
			silentReachCase{h + "/bare words", "proc f {} {\n  set x 0\n  " + h + " set x 1\n  puts $x\n}", "$x\n}"},
			silentReachCase{h + "/braced words", "proc f {} {\n  set x 0\n  " + h + " {set x} {1}\n  puts $x\n}", "$x\n}"},
		)
	}
	runSilentReachCases(t, append(cases,
		silentReachCase{"second braced script binds", "proc f {} {\n  set x 0\n  eval {set y 1;} {set x 1}\n  puts $x\n}", "$x\n}"},
		silentReachCase{"use in a braced word", "proc f {} {\n  set x 0\n  eval {set x 1;} {puts $x}\n}", "$x}"},
		silentReachCase{"use in a braced word of a substituted eval", "proc f {} {\n  set x 0\n  puts [eval {set x 1;} {list $x}]\n}", "$x}"},
		silentReachCase{"back edge in a loop", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    puts $x\n    eval set x 1\n  }\n}", "$x\n"},
		silentReachCase{"after the loop", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    eval set x 1\n  }\n  puts $x\n}", "$x\n}"},
		silentReachCase{"inside catch", "proc f {} {\n  set x 0\n  catch {eval set x 1}\n  puts $x\n}", "$x\n}"},
		silentReachCase{"inside an unknown command's body", "proc f {} {\n  set x 0\n  foo {eval set x 1}\n  puts $x\n}", "$x\n}"},
		silentReachCase{"inside a bracket", "proc f {} {\n  set x 0\n  puts [eval set x 1]\n  puts $x\n}", "$x\n}"},
		silentReachCase{"inside an if body", "proc f {c} {\n  set x 0\n  if {$c} {\n    eval set x 1\n  }\n  puts $x\n}", "$x\n}"},
		silentReachCase{"page level", "set x 0\neval set x 1\nputs $x\n", "$x\n"},
	))
}

// One-word eval runs its braced script as catch does, and the words of a multi-word
// eval are substituted before it runs, so those uses keep their answers.
func TestReachingEvalAnswersKept(t *testing.T) {
	runReachCases(t, []reachCase{
		{"one-word eval binds", "proc f {} {\n  set x 0\n  eval {set x 1}\n  puts $x\n}", "$x\n}", []int{2, 3}},
		{"one-word qualified eval binds", "proc f {} {\n  set x 0\n  ::eval {set x 1}\n  puts $x\n}", "$x\n}", []int{2, 3}},
		{"use in an unbraced word", "proc f {} {\n  set x 0\n  eval set y $x\n}", "$x\n}", []int{2}},
		{"use in a bracket word", "proc f {} {\n  set x 0\n  eval set y [list $x]\n}", "$x]", []int{2}},
		{"use before the eval", "proc f {} {\n  set x 0\n  puts $x\n  eval set x 1\n}", "$x\n", []int{2}},
	})
}

// The braced words of a multi-word eval are part of its joined script, never a
// decorated proc's body or a scope of their own (#140). `eval DECO proc g {} {set
// x 1; puts $x}` runs `DECO proc g {} set x 1; puts $x`, and `eval list {set x 1;
// puts $x}` runs `list set x 1; puts $x`, so x is 0 at each puts (tclsh 8.6 and
// 9.0). A use in such a word stays silent at every level.
func TestReachingMultiWordEvalBracedWordsNotAScope(t *testing.T) {
	var cases []silentReachCase
	for _, h := range []string{"eval", `"eval"`, "{eval}", "::eval", "{*}eval"} {
		cases = append(cases,
			silentReachCase{h + "/decorator in a proc", "proc p {} { set x 0; " + h + " DECO proc g {} {set x 1; puts $x} }", "$x}"},
			silentReachCase{h + "/decorator in a proc, multi-line", "proc p {} {\n  set x 0\n  " + h + " DECO proc g {} {\n    set x 1\n    puts $x\n  }\n}", "$x\n"},
			silentReachCase{h + "/decorator at page level", "set x 0\n" + h + " DECO proc g {} {\n  set x 1\n  puts $x\n}\n", "$x\n"},
			silentReachCase{h + "/decorator in a namespace", "namespace eval ns {\n  " + h + " DECO proc g {} {set x 1; puts $x}\n}", "$x}"},
			silentReachCase{h + "/namespace level", "namespace eval ns { set x 0; " + h + " list {set x 1; puts $x} }", "$x}"},
			silentReachCase{h + "/page level", "set x 0\n" + h + " list {set x 1; puts $x}\n", "$x}"},
			silentReachCase{h + "/no prior binding at page level", h + " list {set x 1; puts $x}\n", "$x}"},
			silentReachCase{h + "/if body in the word", "set x 0\n" + h + " list {if {1} {set x 1; puts $x}}\n", "$x}"},
			silentReachCase{h + "/wrapper body in the word", "set x 0\n" + h + " list {foo {set x 1; puts $x}}\n", "$x}"},
			silentReachCase{h + "/namespace eval in the word", "set x 0\n" + h + " list {namespace eval q {set x 1; puts $x}}\n", "$x}"},
			silentReachCase{h + "/inside a wrapper at page level", "foo {\n  set x 0\n  " + h + " list {set x 1; puts $x}\n}\n", "$x}"},
		)
	}
	runSilentReachCases(t, cases)
}

// Outside a multi-word eval, the scopes around it keep their answers: a proc in its
// word still has a body of its own, and a decorated proc beside it still defines.
func TestReachingMultiWordEvalNeighboursKept(t *testing.T) {
	runReachCases(t, []reachCase{
		{"decorated proc", "set x 0\nDECO proc g {} {\n  set x 1\n  puts $x\n}\n", "$x\n}", []int{3}},
		{"wrapper body at page level", "set x 0\nfoo {set x 1; puts $x}\n", "$x}", []int{2}},
		{"before the eval in a namespace body", "namespace eval ns {\n  set x 2\n  puts $x\n  eval list {set y 1}\n}", "$x\n", []int{2}},
	})
}

// Definitions indexing agrees with reaching: `eval DECO proc g {} {...}` defines no
// proc g and no scope for its braced word, while the brace still folds as the word
// the walkers descend for calls (#140).
func TestDefsMultiWordEvalDecoratorDefinesNoProc(t *testing.T) {
	for _, c := range []struct {
		src string
		// fold says the brace folds: it is the command's last word, which the
		// walkers descend for calls. A brace before trailing flags is not walked.
		fold bool
	}{
		{"eval DECO proc g {} {\n  set x 1\n}\n", true},
		{"::eval DECO proc g {} {\n  set x 1\n}\n", true},
		{"proc p {} {\n  eval DECO proc g {a} {\n    set x 1\n  }\n}\n", true},
		{"namespace eval ns {\n  eval CACHE_PROC proc g {} {\n    set x 1\n  } -ttl 60\n}\n", false},
	} {
		src := c.src
		// In proc p the word is walked as a wrapper's body, so a local it binds
		// belongs to p's scope, never to a scope of g's.
		pScope := -1
		if i := strings.Index(src, "proc p {} {"); i >= 0 {
			pScope = i + len("proc p {} {")
		}
		for _, d := range FileDefs(src) {
			if d.Name == "::g" || d.Name == "::ns::g" || d.Name == "a" || (d.Name == "x" && d.Kind == DefLocal && d.Scope != pScope) {
				t.Errorf("FileDefs(%q) has %+v, want no proc g and no local in a scope of its own", src, d)
			}
		}
		open := strings.LastIndex(src, "{\n")
		found := false
		for _, f := range FileFolds(src) {
			found = found || f.Open == open
		}
		if found != c.fold {
			t.Errorf("FileFolds(%q) = %v, fold at the braced word (offset %d) = %v, want %v", src, FileFolds(src), open, found, c.fold)
		}
	}
}
