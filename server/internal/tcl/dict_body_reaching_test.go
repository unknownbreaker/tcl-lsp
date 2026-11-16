package tcl

import (
	"strings"
	"testing"
)

// starts returns the NameStart of each def.
func starts(defs []Definition) []int {
	var out []int
	for _, d := range defs {
		out = append(out, d.NameStart)
	}
	return out
}

func wantReach(t *testing.T, src, use string, defs ...string) {
	t.Helper()
	got := reachAtMarker(t, src, use)
	var want []int
	for _, d := range defs {
		want = append(want, strings.Index(src, d))
	}
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", use, starts(got), want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g.NameStart == w
		}
		if !found {
			t.Fatalf("%s: got %v, want %v", use, starts(got), want)
		}
	}
}

// A dict with or dict update body runs once, in place, and passes break, continue
// and return through (tclsh 8.6 and 9.0), so the analyzer threads it like inline code.
func TestReachingDictBodiesRunInPlace(t *testing.T) {
	cases := []struct {
		name, src, use string
		defs           []string
	}{
		{"with: body kills", "proc f {d} {\n  set x 0\n  dict with d {\n    set x 1\n  }\n  puts $x!\n}", "$x!", []string{"x 1"}},
		{"with: use in body", "proc f {d} {\n  set x 0\n  dict with d {\n    puts $x!\n  }\n}", "$x!", []string{"x 0"}},
		{"with: keys form", "proc f {d} {\n  set x 0\n  dict with d a b {\n    set x 1\n  }\n  puts $x!\n}", "$x!", []string{"x 1"}},
		{"with: conditional in body", "proc f {d} {\n  set x 0\n  dict with d {\n    if {$c} { set x 1 }\n  }\n  puts $x!\n}", "$x!", []string{"x 0", "x 1"}},
		{"update: var killed", "proc f {d} {\n  set v 9\n  dict update d k v {\n    puts $v!\n  }\n}", "$v!", []string{"v {"}},
		{"update: other var reaches", "proc f {d} {\n  set y 9\n  dict update d k v {\n    puts $y!\n  }\n}", "$y!", []string{"y 9"}},
		{"continue passes through", "proc f {d} {\n  set x 0\n  foreach i {1 2} {\n    dict with d {\n      if {$i} continue\n      set x 1\n    }\n  }\n  puts $x!\n}", "$x!", []string{"x 0", "x 1"}},
		{"return passes through", "proc f {d} {\n  set x 0\n  if {$c} {\n    dict with d {\n      set x 1\n      return\n    }\n  }\n  puts $x!\n}", "$x!", []string{"x 0"}},
		{"in catch", "proc f {d} {\n  set x 0\n  catch {\n    dict with d {\n      set x 1\n    }\n  }\n  puts $x!\n}", "$x!", []string{"x 0", "x 1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantReach(t, c.src, c.use, c.defs...) })
	}
}

// Only a braced body runs in place. An unbraced body, a dict update with a missing
// varName and other dict subcommands keep their old shape.
func TestDictBodyHeadShape(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"dict with d {}", true},
		{"dict with d a b {}", true},
		{"dict update d k v {}", true},
		{"dict update d k v j w {}", true},
		{"dict update d k {}", false},
		{"dict update d k v j {}", false},
		{"dict with d $script", false},
		{"dict with {}", false},
		{"dict set d k {}", false},
	}
	for _, c := range cases {
		if got := dictBodyHead(Parse(c.src)[0].Words); got != c.want {
			t.Errorf("dictBodyHead(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}
