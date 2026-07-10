package resolve

import "testing"

// Qualified array-ELEMENT set inside a proc, with NO `global` link at all --
// `set ::cfg(key) 1` should promote directly via emitVarAssign's qualified-name
// branch (not via promoteLinkedAssignments), same as a qualified scalar.
func TestQualifiedArrayElementSetInProc_NoGlobalLink(t *testing.T) {
	def := "proc init {} {\n  set ::cfg(key) 1\n}\n"
	use := "puts $::cfg\n"
	requireDefInFile(t, resolveUse(t, def, use, "::cfg"), "qualified array-element set, no global link")
}

// Qualified incr/append/lappend targets (no global link) inside a proc must
// also promote directly.
func TestQualifiedIncrAppendLappendInProc_NoGlobalLink(t *testing.T) {
	shapes := []struct {
		name   string
		defSrc string
	}{
		{"qualified incr", "proc init {} {\n  set ::counter 0\n  incr ::counter\n}\n"},
		{"qualified append", "proc init {} {\n  append ::buf x\n}\n"},
		{"qualified lappend", "proc init {} {\n  lappend ::items x\n}\n"},
	}
	for _, c := range shapes {
		t.Run(c.name, func(t *testing.T) {
			var target string
			switch c.name {
			case "qualified incr":
				target = "::counter"
			case "qualified append":
				target = "::buf"
			case "qualified lappend":
				target = "::items"
			}
			requireDefInFile(t, resolveUse(t, c.defSrc, "puts $"+target+"\n", target), c.name)
		})
	}
}
