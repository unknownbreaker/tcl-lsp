package tcl

import "testing"

// A catch traps every completion code, and a try the codes its `on` clauses name,
// so a break or continue they trap ends there: it never carries the bindings before
// it to the loop's exit or next pass (tclsh 8.6 and 9.0). An exit they do not trap,
// or one from a handler, still leaves for the enclosing loop.
func TestReachingTrappedLoopExits(t *testing.T) {
	loop := func(body string) string {
		return "proc f {} {\n  set x 9\n  while {[incr n] < 3} {\n    set x 0\n    " + body + "\n    set x 2\n  }\n  puts $x\n}"
	}
	for _, h := range controlHeads("catch") {
		runReachCases(t, []reachCase{
			{h + "/traps break", loop(h + " { set x 1; break }"), "$x\n}", []int{2, 6}},
			{h + "/traps continue", loop(h + " { set x 1; continue }"), "$x\n}", []int{2, 6}},
		})
	}
	for _, h := range controlHeads("try") {
		runReachCases(t, []reachCase{
			{h + "/on break traps break", loop(h + " { set x 1; break } on break {} {}"), "$x\n}", []int{2, 6}},
		})
	}
	runReachCases(t, []reachCase{
		{"on 3 traps break", loop("try { set x 1; break } on 3 {} {}"), "$x\n}", []int{2, 6}},
		{"on 03 traps break", loop("try { set x 1; break } on 03 {} {}"), "$x\n}", []int{2, 6}},
		{"quoted on code traps break", loop(`try { set x 1; break } on "break" {} {}`), "$x\n}", []int{2, 6}},
		{"on 4 traps continue", loop("try { set x 1; continue } on 4 {} {}"), "$x\n}", []int{2, 6}},
		{"on error lets break out", loop("try { set x 1; break } on error {} {}"), "$x\n}", []int{2, 5, 6}},
		{"trap lets break out", loop("try { set x 1; break } trap {} {} {}"), "$x\n}", []int{2, 5, 6}},
		{"on break lets continue out", loop("try { set x 1; continue } on break {} {}"), "$x\n}", []int{2, 5, 6}},
		{"a handler's break leaves", loop("try { error e } on error {} { set x 1; break } on break {} {}"), "$x\n}", []int{2, 5, 6}},
		{"a finally break leaves", loop("try { } on break {} {} finally { set x 1; break }"), "$x\n}", []int{2, 5, 6}},
		{"a loop inside catch takes its own break", loop("catch { while 1 { set x 1; break } }"), "$x\n}", []int{2, 6}},
		{"a catch inside a loop inside catch", "proc f {} {\n  set x 9\n  catch {\n    while {[incr n] < 3} {\n      catch { set x 1; break }\n      set x 2\n    }\n    puts $x\n  }\n}", "$x\n  }", []int{2, 6}},
		{"a break out of a loop in catch", "proc f {} {\n  set x 9\n  catch {\n    while {[incr n] < 3} {\n      set x 1\n      break\n    }\n  }\n  puts $x\n}", "$x\n}", []int{2, 5}},
	})
}
