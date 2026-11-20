package tcl

import "testing"

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
