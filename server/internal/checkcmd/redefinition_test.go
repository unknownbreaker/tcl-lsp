package checkcmd

import "testing"

// Each other file makes `set` (or a builtin whose body holds the finding) a
// command of the user's, by a single form, so the finding in a.tcl is not
// certain. Every form is checked on its own, without a `rename set` that would
// silence the run by itself.
func TestEachRedefinitionFormSilences(t *testing.T) {
	use := "set x $x\nforeach i {1} { set y $y }\n"
	for name, other := range map[string]string{
		"proc":               "proc set {args} {}\n",
		"proc braced":        "proc {set} {args} {}\n",
		"proc quoted":        "proc \"set\" {args} {}\n",
		"proc qualified":     "proc ::util::set {args} {}\n",
		"proc in a deferred": "after 10 { proc set {args} {} }\n",
		"proc in a subst":    "puts [proc set {args} {}]\n",
		"decorated proc":     "CACHE_PROC proc set {args} {}\n",
		"rename to":          "rename myset set\n",
		"rename away":        "rename set {}\n",
		"interp alias":       "interp alias {} set {} myset\n",
		"oo create":          "oo::class create set\n",
		"object create":      "Widget create set\n",
		"itcl method":        "itcl::class K { method set {args} {} }\n",
		"itcl class":         "itcl::class set {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, _, code := attackRun(t, map[string]string{"a.tcl": "set x $x\n", "z.tcl": other})
			if out != "" || code != exitClean {
				t.Errorf("got exit %d, output:\n%s", code, out)
			}
		})
	}
	for name, other := range map[string]string{
		"proc foreach":   "proc foreach {args} {}\n",
		"rename foreach": "rename foreach _f\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, _, _ := attackRun(t, map[string]string{"a.tcl": use, "z.tcl": other})
			want := "a.tcl:1:1: warning self-assignment assigns x its own value\n"
			if len(out) < len(want) || out[len(out)-len(want):] != want {
				t.Errorf("got:\n%s\nwant only the top-level finding", out)
			}
		})
	}
}

// A name built at run time is unknown, and an unrelated command is not `set`.
func TestUnrelatedDefinitionsKeepFindings(t *testing.T) {
	for name, other := range map[string]string{
		"other proc":     "proc setup {args} {}\n",
		"dynamic proc":   "proc $name {args} {}\n",
		"set variable":   "set set 1\n",
		"constructor":    "itcl::class K { constructor {} {} }\n",
		"rename other":   "rename a b\n",
		"set in comment": "# proc set {} {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, _, code := attackRun(t, map[string]string{"a.tcl": "set x $x\n", "z.tcl": other})
			if code != exitWarnings || out == "" {
				t.Errorf("got exit %d, output %q; want the finding", code, out)
			}
		})
	}
}
