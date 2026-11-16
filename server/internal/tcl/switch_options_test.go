package tcl

import "testing"

// switch reads its options the way Tcl's parser does (tclsh 8.6 and 9.0): a word
// whose value starts with `-` before the last two words is an option, matched by
// unique, case-sensitive abbreviation, whether bare, quoted or braced. Tcl rejects
// a bad or ambiguous option, a second mode option, `-matchvar` or `-indexvar`
// without `-regexp`, and a variable option with no variable before the last two
// words; those switches run no arm, so nothing in them is walked. In every case a
// pattern is never a command.
func TestSwitchOptionsFollowTcl(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"abbreviated", "switch -reg $x {stop { helper }}", 1},
		{"one-letter abbreviation", "switch -e $x {stop { helper }}", 1},
		{"abbreviations and --", "switch -n -g -- $x {stop { helper }}", 1},
		{"abbreviated variable option", "switch -reg -mat m $x {stop { helper }}", 1},
		{"quoted option", "switch \"-glob\" $x {stop { helper }}", 1},
		{"braced option", "switch {-glob} $x {stop { helper }}", 1},
		{"escaped option", "switch \"-gl\\x6fb\" $x {stop { helper }}", 1},
		{"abbreviated, inline", "switch -reg $x stop { helper }", 1},
		{"variable option after -regexp", "switch -matchvar m -regexp $x {stop { helper }}", 1},
		{"repeated -nocase", "switch -nocase -nocase $x {stop { helper }}", 1},
		{"::switch", "::switch $x {stop { helper }}", 1},
		{"::switch, abbreviated", "::switch -reg $x {stop { helper }}", 1},
		{"::switch, inline", "::switch $x stop { helper } stop { helper }", 2},

		// The last two words are never options.
		{"subject looks like -exact", "switch -exact {stop { helper }}", 1},
		{"subject is --", "switch -glob -- {stop { helper }}", 1},
		{"subject looks like an abbreviation", "switch -- -reg {stop { helper }}", 1},

		// Shapes Tcl rejects.
		{"ambiguous -", "switch - $x {stop { helper }}", 0},
		{"unknown option", "switch -bogus $x {stop { helper }}", 0},
		{"case matters", "switch -GLOB $x {stop { helper }}", 0},
		{"two modes", "switch -exact -glob $x {stop { helper }}", 0},
		{"same mode twice", "switch -glob -glob $x {stop { helper }}", 0},
		{"-matchvar without -regexp", "switch -matchvar m $x {stop { helper }}", 0},
		{"-indexvar without -regexp", "switch -indexvar i $x {stop { helper }}", 0},
		{"variable option eats the subject", "switch -regexp -matchvar m {stop { helper }}", 0},
		{"option of unknown name", "switch -$mode $x {stop { helper }}", 0},

		// A substituted word before the last two may be an option at run time. It
		// is the subject only when no option reading gives a shape Tcl accepts.
		{"substituted option", "switch $o $x {stop { helper }}", 0},
		{"bracketed option", "switch [opts] $x {stop { helper }}", 0},
		{"after -regexp, may be -matchvar", "switch -regexp $x a { helper } b { helper }", 0},
		{"inline arms, odd if an option", "switch $x a { helper } b { helper }", 2},
		{"inline arms after -glob", "switch -glob $x a { helper } b { helper }", 2},
		{"one inline arm, odd as a list", "switch $x stop { helper }", 1},
		{"substituted pattern", "switch $x $y { helper }", 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			FileDefs(testCase.src)
			FileFolds(testCase.src)
			for _, name := range []string{"stop", "a", "b"} {
				if got := countCommandRefs(refs, name); got != 0 {
					t.Errorf("pattern `%s` read as a command %d time(s); refs = %+v", name, got, refs)
				}
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
		})
	}
}

// `::switch` is the switch builtin: a `proc` pattern in its inline arms is data,
// not a decorated proc definition, and its arms are alternatives for reaching.
func TestQualifiedSwitchIsSwitch(t *testing.T) {
	for _, src := range []string{
		"::switch $k proc - method { puts 1 }",
		"::switch $k {\n  proc -\n  method { puts 1 }\n}",
	} {
		if defs := FileDefs(src); len(defs) != 0 {
			t.Errorf("::switch arms made symbols: %+v\nsource:\n%s", defs, src)
		}
	}
	runReachCases(t, []reachCase{
		{"arms join", "proc f {} {\n  set x 0\n  ::switch $k {\n    a { set x 1 }\n  }\n  puts $x\n}", "$x", []int{2, 4}},
	})
}
