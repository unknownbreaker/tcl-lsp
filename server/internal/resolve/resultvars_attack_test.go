package resolve

import (
	"sort"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

func advNth(t *testing.T, src, sub string, n, delta int) int {
	t.Helper()
	from := 0
	for i := 0; i < n; i++ {
		k := strings.Index(src[from:], sub)
		if k < 0 {
			t.Fatalf("occurrence %d of %q not found", n, sub)
		}
		if i == n-1 {
			return from + k + delta
		}
		from += k + len(sub)
	}
	return -1
}

func advDef(t *testing.T, file, src string, use int, want ...int) {
	t.Helper()
	ix := index.New()
	ix.IndexFile(file, src)
	locs := New(ix).Definition(file, src, use)
	var got []int
	for _, l := range locs {
		got = append(got, l.NameStart)
	}
	sort.Ints(got)
	sort.Ints(want)
	if len(got) != len(want) {
		t.Fatalf("Definition = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Definition = %v, want %v", got, want)
		}
	}
}

// The commonest shape: catch inside an if condition's [...]. 8.6/9.0: "p8 msg=boom".
func TestAttackResultVarsCatchInIfCondition(t *testing.T) {
	src := `proc p {} {
  if {[catch {error boom} msg]} {
    puts $msg
  }
}`
	advDef(t, "a.tcl", src, advNth(t, src, "$msg", 1, 1), advNth(t, src, "msg]", 1, 0))
}

func TestAttackResultVarsCatchInSetBracket(t *testing.T) {
	src := `proc p {} {
  set rc [catch {error boom} msg opts]
  puts $msg$opts
}`
	advDef(t, "a.tcl", src, advNth(t, src, "$msg", 1, 1), advNth(t, src, "msg opts", 1, 0))
	advDef(t, "a.tcl", src, advNth(t, src, "$opts", 1, 1), advNth(t, src, "opts]", 1, 0))
}

func TestAttackResultVarsWhileCatchGets(t *testing.T) {
	src := `proc p {f} {
  while {[catch {gets $f} line] == 0} {
    puts $line
  }
}`
	advDef(t, "a.tcl", src, advNth(t, src, "$line", 1, 1), advNth(t, src, "line]", 1, 0))
}

func TestAttackResultVarsRvtTryHandler(t *testing.T) {
	src := `<html><?
proc p {} {
  try { error boom } on error {msg} { puts $msg }
}
?></html>`
	advDef(t, "a.rvt", src, advNth(t, src, "$msg", 1, 1), advNth(t, src, "{msg}", 1, 1))
}

func TestAttackResultVarsTryFallthrough(t *testing.T) {
	src := `proc p1 {} {
  set m pre
  try {error boom} on error {m} - on ok {r} { puts $m }
}`
	// 8.6/9.0 print m=boom on the error path (the `-` handler's varlist binds m)
	// and m=pre on the ok path, so both bindings reach the shared body.
	advDef(t, "a.tcl", src, advNth(t, src, "$m", 1, 1), advNth(t, src, "m pre", 1, 0), advNth(t, src, "{m}", 1, 1))
}
