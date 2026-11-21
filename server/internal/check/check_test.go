package check

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// findings runs every Check over src.
func findings(src string) []Finding {
	var col Collector
	tcl.VisitCommands(src, tcl.Visit{Command: col.Visit})
	return col.Findings()
}

// The parser's hostile inputs (unterminated braces, quotes and brackets,
// continuations, comment edges) hold no self-assignment, so no Check fires.
func TestHostileInputsGiveNoFindings(t *testing.T) {
	files, _ := filepath.Glob("../tcl/testdata/parsedump/*.tcl")
	if len(files) == 0 {
		t.Fatal("no hostile inputs found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if fs := findings(string(b)); len(fs) != 0 {
			t.Errorf("%s: got %+v, want none", f, fs)
		}
	}
}

// Every prefix of a self-assignment, as it is typed, is silent until the
// command is whole.
func TestHalfTypedIsSilent(t *testing.T) {
	src := "proc p {} {\n    set x ${x}\n}\n"
	whole := len("proc p {} {\n    set x ${x}")
	for n := 0; n < len(src); n++ {
		fs := findings(src[:n])
		if n < whole && len(fs) != 0 {
			t.Errorf("prefix %q: got %+v, want none", src[:n], fs)
		}
	}
}

func TestSelfAssignmentRange(t *testing.T) {
	src := "if 1 {\n  set  x  $x\n}"
	fs := findings(src)
	if len(fs) != 1 {
		t.Fatalf("got %+v, want one finding", fs)
	}
	f := fs[0]
	if got := src[f.Start:f.End]; got != "set  x  $x" || f.Check != "self-assignment" || f.Severity != Warning || !reflect.DeepEqual(f.Assumes, []string{"if", "set"}) {
		t.Errorf("got %+v covering %q", f, got)
	}
}

func TestUnshadowed(t *testing.T) {
	fs := []Finding{
		{Check: "a", Assumes: []string{"set"}},
		{Check: "b", Assumes: []string{"string"}},
		{Check: "c"},
		{Check: "d", Assumes: []string{"foreach", "string"}},
		{Check: "e", Assumes: []string{"set", "string"}},
	}
	got := Unshadowed(fs, func(name string) bool { return name == "set" || name == "foreach" })
	if len(got) != 2 || got[0].Check != "b" || got[1].Check != "c" {
		t.Fatalf("got %+v, want b and c", got)
	}
}

func TestIsVarName(t *testing.T) {
	for s, want := range map[string]bool{
		"x": true, "x1_": true, "::x": true, "a::b": true, "::a::b": true, "1": true,
		"": false, "::": false, "a::": false, "a:b": false, "a:::b": false, "a(1)": false,
		"a b": false, "$x": false, "::::a": false, "a::::b": false,
	} {
		if got := isVarName(s); got != want {
			t.Errorf("isVarName(%q) = %v, want %v", s, got, want)
		}
	}
}
