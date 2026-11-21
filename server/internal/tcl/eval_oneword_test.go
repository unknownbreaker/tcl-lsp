package tcl

import "testing"

// A one-word eval runs its word's value as a script in this frame whether the word
// is braced, quoted or substituted, and `uplevel 0` runs its script here too:
// `set x 0; eval "set x 1"; puts $x` prints 1, as do `eval [list set x 1]`,
// `eval $s`, `uplevel 0 {set x 1}` and `uplevel 0 set x 1` (tclsh 8.6 and 9.0).
// The analysis reads only a braced eval word, so uses after the others stay
// silent: a missed answer, never only `set x 0` (#139).
func TestReachingUnreadEvalSilent(t *testing.T) {
	proc := func(line string) string { return "proc f {} {\n  set x 0\n  " + line + "\n  puts $x\n}" }
	var cases []silentReachCase
	for _, line := range []string{
		`eval "set x 1"`,
		`eval [list set x 1]`,
		`eval $s`,
		`eval set\ x\ 1`,
		`"eval" "set x 1"`,
		`::eval [list set x 1]`,
		`{*}eval $s`,
		"uplevel 0 set x 1",
		`uplevel 0 "set x 1"`,
		"uplevel 0 [list set x 1]",
		"uplevel 0 $s",
		"uplevel 0 {set x} {1}",
		"::uplevel 0 set x 1",
		`"uplevel" 0 $s`,
		"{*}uplevel 0 set x 1",
		// Tcl also reads integer prefixes as level 0 (tclsh 8.6 and 9.0).
		"uplevel 0x0 {set x 1}",
		"uplevel 0o0 {set x 1}",
		"uplevel 0b0 {set x 1}",
		// An absolute level names this frame when the proc runs at that depth:
		// `#1` does for a proc called from the top level.
		"uplevel #1 {set x 1}",
		// Tcl reads an absolute level as an integer, hex included: `#0xa` names a
		// proc that runs at depth 10.
		"uplevel #0xa {set x 1}",
		"uplevel #0XA {set x 1}",
		// Tcl decodes escapes in the level and trims space around it.
		`uplevel \x30 {set x 1}`,
		`uplevel "\x30" {set x 1}`,
		`uplevel #\x31 {set x 1}`,
		`uplevel " 0" set x 1`,
		`uplevel "\t0" "set x 1"`,
	} {
		cases = append(cases, silentReachCase{line, proc(line), "$x\n}"})
	}
	runSilentReachCases(t, append(cases,
		silentReachCase{"back edge in a loop", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    puts $x\n    eval \"set x 1\"\n  }\n}", "$x\n"},
		silentReachCase{"inside catch", "proc f {} {\n  set x 0\n  catch {uplevel 0 set x 1}\n  puts $x\n}", "$x\n}"},
		silentReachCase{"use in a braced word of a multi-word uplevel 0", "proc f {} {\n  set x 0\n  uplevel 0 {set x 1;} {puts $x}\n}", "$x}"},
		silentReachCase{"inside an unknown command's body", "proc f {} {\n  set x 0\n  foo {eval $s}\n  puts $x\n}", "$x\n}"},
		silentReachCase{"inside a bracket", "proc f {} {\n  set x 0\n  puts [eval \"set x 1\"]\n  puts $x\n}", "$x\n}"},
		silentReachCase{"page level eval", "set x 0\neval \"set x 1\"\nputs $x\n", "$x\n"},
		silentReachCase{"page level uplevel 0", "set x 0\nuplevel 0 {set x 1}\nputs $x\n", "$x\n"},
		silentReachCase{"page level uplevel #0", "set x 0\nuplevel #0 {set x 1}\nputs $x\n", "$x\n"},
	))
}

// The words of a one-word eval are substituted before its script runs, and a braced
// eval word is read. `uplevel 0 {script}` runs its script here as `eval {script}`
// does, so it is read the same way, whichever spelling of level 0 Tcl accepts. A
// script `uplevel 1` runs in the caller's frame, and `uplevel #0` in a proc runs in
// the global frame, so both keep today's policy.
func TestReachingOneWordEvalAnswersKept(t *testing.T) {
	var cases []reachCase
	for _, line := range []string{
		"uplevel 0 {set x 1}", "::uplevel 0 {set x 1}", `"uplevel" 0 {set x 1}`, "{*}uplevel 0 {set x 1}",
		`uplevel "0" {set x 1}`, "uplevel {0} {set x 1}", "uplevel 00 {set x 1}", "uplevel +0 {set x 1}",
		"uplevel -0 {set x 1}", "uplevel { 0} {set x 1}", "uplevel {0 } {set x 1}",
	} {
		cases = append(cases, reachCase{line, "proc f {} {\n  set x 0\n  " + line + "\n  puts $x\n}", "$x\n}", []int{2, 3}})
	}
	runReachCases(t, append(cases, []reachCase{
		{"braced eval binds", "proc f {} {\n  set x 0\n  eval {set x 1}\n  puts $x\n}", "$x\n}", []int{2, 3}},
		{"use in a quoted eval word", "proc f {} {\n  set x 0\n  eval \"puts $x\"\n}", "$x\"", []int{2}},
		{"use in an eval bracket word", "proc f {} {\n  set x 0\n  eval [list puts $x]\n}", "$x]", []int{2}},
		{"use before the eval", "proc f {} {\n  set x 0\n  puts $x\n  eval $s\n}", "$x\n", []int{2}},
		{"uplevel 1", "proc f {} {\n  set x 0\n  uplevel 1 {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel with no level", "proc f {} {\n  set x 0\n  uplevel {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel 1 with words", "proc f {} {\n  set x 0\n  uplevel 1 set x 1\n  puts $x\n}", "$x\n}", []int{2}},
		{"space before an absolute level", "proc f {} {\n  set x 0\n  uplevel { #1} {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel #0 in a proc", "proc f {} {\n  set x 0\n  uplevel #0 {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel #00 in a proc", "proc f {} {\n  set x 0\n  uplevel #00 {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel #+0 in a proc", "proc f {} {\n  set x 0\n  uplevel #+0 {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel # 0 in a proc", "proc f {} {\n  set x 0\n  uplevel {# 0} {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel #0 with space after in a proc", "proc f {} {\n  set x 0\n  uplevel {#0 } {set x 1}\n  puts $x\n}", "$x\n}", []int{2}},
		{"uplevel 0 in a loop", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    puts $x\n    uplevel 0 {set x 1}\n  }\n}", "$x\n", []int{2, 5}},
		{"use after a binding in uplevel 0's script", "proc f {} {\n  set x 0\n  uplevel 0 {\n    set x 1\n    puts $x\n  }\n}", "$x\n", []int{4}},
	}...))
}
