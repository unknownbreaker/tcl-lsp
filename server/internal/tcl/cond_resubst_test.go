package tcl

import "testing"

// if, while and for evaluate their condition as expr does. An unbraced condition
// is substituted as a command word first, and the expression evaluation then
// substitutes the result again, so an escaped bracket or a substituted value may
// form a script that binds (#110). Each source below leaves x=1 at the mark in
// tclsh 8.6 and 9.0, so reaching there stays silent. The condition is evaluated
// before its body, and a loop's on every pass, so body uses are silent too.
func TestReachingCondResubstSilent(t *testing.T) {
	runSilentReachCases(t, []silentReachCase{
		{"if escaped brackets, after", "proc f {} {\n  set x 0\n  if \"\\[set x 1\\]\" {}\n  puts $x\n}", "$x\n}"},
		{"if escaped brackets, body", "proc f {} {\n  set x 0\n  if \"\\[set x 1\\]\" {puts $x}\n}", "$x}"},
		{"if then body", "proc f {} {\n  set x 0\n  if \"\\[set x 1\\]\" then {puts $x}\n}", "$x}"},
		{"if value, body", "proc f {s} {\n  set x 0\n  if $s {puts $x}\n}", "$x}"},
		{"if command result, after", "proc f {} {\n  set x 0\n  if [g] {}\n  puts $x\n}", "$x\n}"},
		{"elseif value, body", "proc f {s} {\n  set x 0\n  if 0 {} elseif $s {puts $x}\n}", "$x}"},
		{"while value, after", "proc f {s} {\n  set x 0\n  while $s {}\n  puts $x\n}", "$x\n}"},
		{"while value, body", "proc f {s} {\n  set x 0\n  while $s {\n    puts $x\n  }\n}", "$x\n  }"},
		{"for value, body", "proc f {s} {\n  set x 0\n  for {set i 0} $s {incr i} {\n    puts $x\n  }\n}", "$x\n  }"},
		{"for value, next", "proc f {s} {\n  set x 0\n  for {set i 0} $s {puts $x} {}\n}", "$x}"},
		{"for value, after", "proc f {s} {\n  set x 0\n  for {} $s {} {}\n  puts $x\n}", "$x\n}"},
		{"qualified if", "proc f {} {\n  set x 0\n  ::if \"\\[set x 1\\]\" {}\n  puts $x\n}", "$x\n}"},
		{"expanded if words", "proc f {args} {\n  set x 0\n  if {*}$args\n  puts $x\n}", "$x\n}"},
		{"quoted elseif keyword", "proc f {s} {\n  set x 0\n  if 0 {} \"elseif\" $s {puts $x}\n}", "$x}"},
		{"braced elseif keyword", "proc f {s} {\n  set x 0\n  if 0 {} {elseif} $s {puts $x}\n}", "$x}"},
		{"substituted keyword", "proc f {s k} {\n  set x 0\n  if 0 {} $k $s {puts $x}\n}", "$x}"},
		{"expanded braced if words", "proc f {} {\n  set x 0\n  if {*}{{[set x 1]} {}}\n  puts $x\n}", "$x\n}"},
		{"expanded braced while words", "proc f {} {\n  set x 0\n  while {*}{{[set x 1] == 0} {}}\n  puts $x\n}", "$x\n}"},
		{"braced elseif condition after", "proc f {s} {\n  set x 0\n  if $s {} elseif {$x} {}\n}", "$x}"},
		// Ruling (#110): Tcl reads this $x on the first pass, before the condition
		// runs, but distrust is one offset and a braced body sits between, so it
		// stays silent: a missed answer, never a wrong one.
		{"use in a later unbraced condition after a body", "proc f {s} {\n  set x 0\n  if $s {} elseif \"$x > 1\" {}\n}", "$x >"},
		{"while inside an outer loop", "proc f {s} {\n  set x 0\n  while 1 {\n    puts $x\n    while $s {}\n  }\n}", "$x\n    while"},
	})
}

// Uses read in the condition word itself, on its first substitution, keep their
// answers, and so does code before the condition. Braced conditions are
// substituted once, by expr, and keep every answer.
func TestReachingCondResubstKept(t *testing.T) {
	runReachCases(t, []reachCase{
		{"use in the if condition", "proc f {} {\n  set x 0\n  if \"$x == \\[set x 1\\]\" {}\n}", "$x ==", []int{2}},
		{"use in an unbraced word before the body", "proc f {s} {\n  set x 0\n  if $s then \"puts $x\"\n}", "$x\"", []int{2}},
		{"use in the while condition", "proc f {s} {\n  set x 0\n  while \"$x && $s\" {}\n}", "$x &&", []int{2}},
		{"if body before the elseif", "proc f {s} {\n  set x 0\n  if 0 {puts $x} elseif $s {}\n}", "$x}", []int{2}},
		{"for start script", "proc f {s} {\n  set x 0\n  for {puts $x} $s {} {}\n}", "$x}", []int{2}},
		{"braced if", "proc f {a} {\n  set x 0\n  if {$a > 1} {puts $x}\n  puts $x\n}", "$x\n}", []int{2}},
		{"braced if body", "proc f {a} {\n  set x 0\n  if {$a > 1} {puts $x}\n}", "$x}", []int{2}},
		{"braced while", "proc f {a} {\n  set x 0\n  while {$a > 1} {puts $x}\n}", "$x}", []int{2}},
		{"literal unbraced condition", "proc f {} {\n  set x 0\n  if 1 {puts $x}\n}", "$x}", []int{2}},
		{"escaped brackets in a body word", "proc f {} {\n  set x 0\n  if 1 \"puts \\[set y 1\\]\"\n  puts $x\n}", "$x\n}", []int{2}},
	})
}
