package tcl

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Visiting rides the indexing walk: the index it builds must be the one FileAll
// builds, on the hostile inputs and the examples.
func TestFileAllVisitMatchesFileAll(t *testing.T) {
	hostile, _ := filepath.Glob("testdata/parsedump/*.tcl")
	examples, _ := filepath.Glob("../../../examples/*.tcl")
	if len(hostile) == 0 || len(examples) == 0 {
		t.Fatal("inputs not found")
	}
	for _, f := range append(hostile, examples...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got := FileAllVisit(string(b), &Visit{Command: func(Command, int, []string) {}})
		if want := FileAll(string(b)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: FileAllVisit index differs from FileAll", f)
		}
	}
}

// visited returns the text of every command the visitor reaches in src.
func visited(src string) []string {
	var out []string
	VisitCommands(src, Visit{Command: func(c Command, base int, _ []string) {
		out = append(out, src[base+c.Words[0].Start:base+c.Words[len(c.Words)-1].End])
	}})
	return out
}

func TestVisitReachesCertainScriptsOnly(t *testing.T) {
	src := `a [b [c]]
proc p {} { d }
namespace eval n { e }
if {[f]} { g } else { h }
itcl::class K {
    variable v
    method m {} { i }
}
after 1 { x1 }
wrap { x2 }
uplevel 1 { x3 }
oo::class create O { method m {} { x4 } }
DECO proc q {} { x5 }
eval x6 { x7 }
set s {x8}
`
	got := visited(src)
	want := []string{
		"a [b [c]]", "b [c]", "c",
		"proc p {} { d }", "d",
		"namespace eval n { e }", "e",
		"if {[f]} { g } else { h }", "f", "g", "h",
		"itcl::class K {\n    variable v\n    method m {} { i }\n}", "i",
		"after 1 { x1 }", "wrap { x2 }", "uplevel 1 { x3 }",
		"oo::class create O { method m {} { x4 } }", "DECO proc q {} { x5 }",
		"eval x6 { x7 }", "set s {x8}",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("visited:\n%q\nwant:\n%q", got, want)
	}
}
