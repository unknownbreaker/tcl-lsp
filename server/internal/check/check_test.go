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

// only returns the findings of the Check id.
func only(fs []Finding, id string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Check == id {
			out = append(out, f)
		}
	}
	return out
}

// The parser's hostile inputs (unterminated braces, quotes and brackets,
// continuations, comment edges) hold no self-assignment and one constant
// condition, hidden_close.tcl's `"if" {1}`, which Tcl runs as `if 1`, so no
// other Check fires.
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
		src := string(b)
		var got []string
		for _, fd := range findings(src) {
			got = append(got, fd.Check+" "+src[fd.Start:fd.End])
		}
		var want []string
		if filepath.Base(f) == "hidden_close.tcl" {
			want = []string{"constant-condition {1}"}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %q, want %q", f, got, want)
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

// Every prefix of a constant condition, as it is typed, is silent until the
// if has its body.
func TestConstantConditionHalfTyped(t *testing.T) {
	src := "if {1 == 1} {\n    puts x\n}\n"
	whole := len("if {1 == 1} {\n    puts x\n}")
	for n := 0; n <= len(src); n++ {
		fs := only(findings(src[:n]), constantConditionID)
		if want := n >= whole; (len(fs) == 1) != want || len(fs) > 1 {
			t.Errorf("prefix %q: got %+v, want a finding: %v", src[:n], fs, want)
		}
	}
}

func TestSelfAssignmentRange(t *testing.T) {
	src := "if 1 {\n  set  x  $x\n}"
	fs := only(findings(src), selfAssignmentID)
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
