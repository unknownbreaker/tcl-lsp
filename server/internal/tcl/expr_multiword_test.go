package tcl

import (
	"strings"
	"testing"
)

// A multi-word expr joins its words with spaces and evaluates the result, so a
// braced word is expression text only once joined: `expr "{" {[$o m]} "}"`
// returns the literal " [$o m] " and never reads $o, while `expr {"} {[set x 1]}
// {"}` runs the script. Its unbraced words are substituted as command words
// before expr runs. Checked against tclsh 8.6 and 9.0.
func TestCommandRefsMultiWordExprBracedWordsSilent(t *testing.T) {
	cases := []struct{ name, src, mark, varName string }{
		{"script joined into a literal", "expr \"{\" {[$o m]} \"}\"", "$o", "o"},
		{"var joined into a literal", "expr \"{\" {$x} \"}\"", "$x", "x"},
		{"braced operand", "expr {$x} + 1", "$x", "x"},
		{"script in a quoted join", "expr {\"} {[set x 1; $y]} {\"}", "$y", "y"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if strings.Index(c.src, c.mark) < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			if got := exprVarOffsets(c.src, c.varName); len(got) != 0 {
				t.Errorf("refs to %s at %v, want none in %q", c.varName, got, c.src)
			}
		})
	}
}

// The script inside a braced word of a multi-word expr is no command reference.
func TestCommandRefsMultiWordExprBracedScriptNoCalls(t *testing.T) {
	src := "expr \"{\" {[helper]} \"}\""
	for _, r := range CommandRefs(Parse(src)[0]) {
		if r.Kind == RefCommand && r.Name == "helper" {
			t.Fatalf("CommandRefs lists helper at %d, want none in %q", r.Start, src)
		}
	}
}

// Unbraced words of a multi-word expr are substituted as command words.
func TestCommandRefsMultiWordExprUnbracedWordsKept(t *testing.T) {
	cases := []struct{ name, src, mark, varName string }{
		{"unbraced script", "expr 1 + [$o m]", "$o", "o"},
		{"unbraced vars", "expr $a + $b", "$b", "b"},
		{"unbraced beside braced", "expr {1} + $b", "$b", "b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := strings.Index(c.src, c.mark) // a reference starts at its '$'
			if want < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			got := exprVarOffsets(c.src, c.varName)
			if len(got) != 1 || got[0] != want {
				t.Errorf("refs to %s = %v, want one at the mark (%d) in %q", c.varName, got, want, c.src)
			}
		})
	}
}

// Reaching at a $x in a braced word of a multi-word expr is silent, and so is a
// later use, since a braced script may or may not run.
func TestReachingMultiWordExprBracedWordsSilent(t *testing.T) {
	runSilentReachCases(t, []silentReachCase{
		{"use in a braced word", "proc f {} {\n  set x 0\n  expr \"{\" {$x} \"}\"\n}", "$x}"},
		{"use after a braced script", "proc f {} {\n  set x 0\n  expr {\"} {[set x 1]} {\"}\n  puts $x\n}", "$x\n}"},
		{"use after && between expr words", "proc f {c} {\n  set x 0\n  set r [expr {$c} && {[set x 1]}]\n  puts $x\n}", "$x\n}"},
		{"use after a bracket escaped in another word", "proc f {} {\n  set x 0\n  expr \"\\[\" {set x 1]}\n  puts $x\n}", "$x\n}"},
		{"use after a braced word holding only the closing bracket", "proc f {} {\n  set b {[}\n  set x 0\n  expr $b {set x 1]}\n  puts $x\n}", "$x\n}"},
		{"use after a bracket split over braced words", "proc f {} {\n  set x 0\n  expr {[set x} { 1]}\n  puts $x\n}", "$x\n}"},
		{"braced use after a balanced braced script", "proc f {} {\n  set x 0\n  expr {[string length \"[set x 1]\"]} + {$x}\n}", "$x}"},
	})
}

// silentReachCase is a source and the text mark of a use that ReachingAt must
// leave silent.
type silentReachCase struct{ name, src, mark string }

// runSilentReachCases fails each case whose mark is missing or whose use
// ReachingAt answers.
func runSilentReachCases(t *testing.T, cases []silentReachCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			off := strings.Index(c.src, c.mark)
			if off < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			if defs, _ := ReachingAt(c.src, off); len(defs) > 0 {
				t.Errorf("ReachingAt(%q) = %+v, want silent\nsource:\n%s", c.mark, defs, c.src)
			}
		})
	}
}

// A multi-word expr with no script in its braced words leaves later uses alone.
func TestReachingMultiWordExprPlainBracedWordKept(t *testing.T) {
	runReachCases(t, []reachCase{
		{"braced literal operand", "proc f {} {\n  set x 0\n  expr {1} + 2\n  puts $x\n}", "$x\n}", []int{2}},
		{"unbraced use", "proc f {} {\n  set x 0\n  expr $x + 1\n}", "$x +", []int{2}},
	})
}
