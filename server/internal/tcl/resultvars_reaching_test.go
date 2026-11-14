package tcl

import "testing"

// Result variables of catch and try. tclsh 8.6.18 and 9.0.3 are the oracle:
// catch always writes its result and options variables, overwriting anything the
// body set; a try handler's variable list binds its first two names when that
// handler starts, and finally sees them.

// reachStarts returns the NameStart of each reaching def for the use at the LAST
// occurrence of mark.
func reachStarts(t *testing.T, src, mark string) []int {
	t.Helper()
	var starts []int
	for _, d := range reachAtMarker(t, src, mark) {
		starts = append(starts, d.NameStart)
	}
	return starts
}

func wantStarts(t *testing.T, got []int, want ...int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("reaching defs at %v, want %v", got, want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Fatalf("reaching defs at %v, want %v", got, want)
		}
	}
}

func TestReachingCatchResultVar(t *testing.T) {
	src := "proc f {} {\n  if {[catch {foo} msg]} {\n    puts $msg\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$msg"), indexOf(t, src, "msg]"))
}

func TestReachingCatchResultVarKillsBodyAndPrior(t *testing.T) {
	src := "proc f {} {\n  set msg 0\n  catch {set msg 1} msg\n  puts $msg\n}"
	wantStarts(t, reachStarts(t, src, "$msg"), indexOf(t, src, "msg\n  puts"))
}

func TestReachingCatchOptionsVar(t *testing.T) {
	src := "proc f {} {\n  catch {foo} r opts\n  puts $opts\n}"
	wantStarts(t, reachStarts(t, src, "$opts"), indexOf(t, src, "r opts")+len("r "))
}

func TestReachingCatchResultVarNotSeenInsideBody(t *testing.T) {
	src := "proc f {} {\n  set msg 0\n  catch {puts $msg} msg\n}"
	wantStarts(t, reachStarts(t, src, "$msg"), indexOf(t, src, "msg 0"))
}

func TestReachingTryHandlerVar(t *testing.T) {
	src := "proc f {} {\n  try {foo} on error {e o} {\n    puts $e\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e o}"))
}

func TestReachingTryHandlerVarBare(t *testing.T) {
	src := "proc f {} {\n  try {foo} on error e {\n    puts $e\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e {"))
}

func TestReachingTryHandlerOptionsVar(t *testing.T) {
	src := "proc f {} {\n  try {foo} trap {POSIX} {m o} {\n    puts $o\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$o"), indexOf(t, src, "o}"))
}

func TestReachingTryHandlerVarKillsPrior(t *testing.T) {
	src := "proc f {} {\n  set e 0\n  try {set e 1} on error {e} {\n    puts $e\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e} {"))
}

func TestReachingTryHandlerVarJoinsAtExit(t *testing.T) {
	src := "proc f {} {\n  set e 0\n  try {foo} on error {e} {}\n  puts $e\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e 0"), indexOf(t, src, "e} {}"))
}

func TestReachingTryFinallySeesHandlerVar(t *testing.T) {
	src := "proc f {} {\n  try {foo} on error {e} {} finally {\n    puts $e\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e} {}"))
}

func TestReachingTrySiblingHandlerVarDoesNotReach(t *testing.T) {
	src := "proc f {} {\n  try {foo} on error {e} {} on break {} {\n    puts $e\n  }\n}"
	wantStarts(t, reachStarts(t, src, "$e"))
}

// FileDefs is the index path (emitDefs); it must agree with localBindings.
// When both names in a handler's variable list are the same, Tcl writes the
// options dict last (tclsh 8.6 and 9.0), so only the second name binds: the mirror
// of catch, where the result is written last.
func TestReachingTrySameNameOptionsWins(t *testing.T) {
	src := "proc f {} {\n  try {error boom} on error {e e} {}\n  puts $e\n}"
	wantStarts(t, reachStarts(t, src, "$e"), indexOf(t, src, "e} {}"))
}

func TestDefsCatchAndTryResultVars(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{"catch result and options", "proc f {} { catch {foo} r o }", []string{"r", "o"}},
		{"catch array element binds the base", "proc f {} { catch {foo} r(x) }", []string{"r"}},
		{"catch with too many words is an error", "proc f {} { catch {foo} r o x }", nil},
		{"catch with no var", "proc f {} { catch {foo} }", nil},
		{"catch substituted var", "proc f {} { catch {foo} $v }", nil},
		{"catch at namespace frame", "catch {foo} r", nil},
		{"try handler var list", "proc f {} { try {foo} on error {r o} {} }", []string{"r", "o"}},
		{"try only the first two names", "proc f {} { try {foo} on error {r o x} {} }", []string{"r", "o"}},
		{"try same name twice binds the options word", "proc f {} { try {foo} on error {e e} {} }", []string{"e"}},
		{"catch same var spelled two ways", "proc f {} { catch {foo} {m} m }", nil},
		{"catch scalar and element of one var", "proc f {} { catch {foo} m m(x) }", nil},
		{"try scalar and element of one var", "proc f {} { try {foo} on error {e e(x)} {} }", nil},
		{"try empty var list", "proc f {} { try {foo} on error {} {} }", nil},
		{"try nested braces in var list", "proc f {} { try {foo} on error {a {b c}} {} }", nil},
		{"try every handler", "proc f {} { try {foo} on error e {} trap {X} {m} {} finally {} }", []string{"e", "m"}},
		{"try at namespace frame", "try {foo} on error e {}", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, d := range FileDefs(c.src) {
				if d.Kind == DefLocal {
					got = append(got, d.Name)
				}
			}
			if len(got) != len(c.want) {
				t.Fatalf("locals %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("locals %v, want %v", got, c.want)
				}
			}
		})
	}
}
