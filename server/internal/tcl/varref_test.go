package tcl

import (
	"reflect"
	"testing"
)

func TestWordVarRefsBracedNone(t *testing.T) {
	w := Word{Kind: WordBraced, Text: "{a $x b}", Start: 0, End: 8}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("braced word should yield no refs, got %#v", got)
	}
}

func TestWordVarRefsSimple(t *testing.T) {
	w := Word{Kind: WordBare, Text: "$x", Start: 4, End: 6}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "x", Start: 4, End: 6}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsLoneDollar(t *testing.T) {
	w := Word{Kind: WordBare, Text: "price$", Start: 0, End: 6}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("lone $ is not a ref, got %#v", got)
	}
}

func TestWordVarRefsBracedName(t *testing.T) {
	// ${...} allows any characters (including spaces) in the name.
	w := Word{Kind: WordBare, Text: "${my var}", Start: 0, End: 9}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "my var", Start: 0, End: 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsNamespaceQualified(t *testing.T) {
	w := Word{Kind: WordBare, Text: "$ns::name", Start: 0, End: 9}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "ns::name", Start: 0, End: 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsGlobalQualified(t *testing.T) {
	w := Word{Kind: WordBare, Text: "$::g", Start: 0, End: 4}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "::g", Start: 0, End: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsArrayName(t *testing.T) {
	w := Word{Kind: WordBare, Text: "$arr(key)", Start: 0, End: 9}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "arr", Start: 0, End: 4, Element: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsArrayIndexVarAlsoFound(t *testing.T) {
	// The array name AND a variable used in the index are both references.
	w := Word{Kind: WordBare, Text: "$arr($idx)", Start: 0, End: 10}
	got := WordVarRefs(w)
	want := []VarRef{
		{Name: "arr", Start: 0, End: 4, Element: true},
		{Name: "idx", Start: 5, End: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsSkipsBracketSpan(t *testing.T) {
	// Command-substitution interiors are deferred: no refs extracted from inside.
	w := Word{Kind: WordBare, Text: "[expr {$x}]", Start: 0, End: 11}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("bracket interior should be skipped, got %#v", got)
	}
}

func TestWordVarRefsEscapedDollar(t *testing.T) {
	w := Word{Kind: WordBare, Text: `\$x`, Start: 0, End: 3}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("escaped dollar is literal, got %#v", got)
	}
}

func TestWordVarRefsQuotedMultiple(t *testing.T) {
	w := Word{Kind: WordQuoted, Text: `"$a and $b"`, Start: 0, End: 11}
	got := WordVarRefs(w)
	want := []VarRef{
		{Name: "a", Start: 1, End: 3},
		{Name: "b", Start: 8, End: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsAbsoluteOffsets(t *testing.T) {
	src := "puts $count"
	cmds := Parse(src)
	if len(cmds) != 1 || len(cmds[0].Words) != 2 {
		t.Fatalf("unexpected parse: %#v", cmds)
	}
	got := WordVarRefs(cmds[0].Words[1])
	want := []VarRef{{Name: "count", Start: 5, End: 11}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
	// The range slices back to the exact source text.
	if src[got[0].Start:got[0].End] != "$count" {
		t.Fatalf("offset slice = %q, want %q", src[got[0].Start:got[0].End], "$count")
	}
}

func TestWordVarRefsEmptyBraced(t *testing.T) {
	w := Word{Kind: WordBare, Text: "${}", Start: 0, End: 3}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("${} is not a valid ref, got %#v", got)
	}
}

func TestWordVarRefsDollarBeforeBracket(t *testing.T) {
	w := Word{Kind: WordBare, Text: "$[expr 1]", Start: 0, End: 9}
	if got := WordVarRefs(w); len(got) != 0 {
		t.Fatalf("$ before [ is not a ref, got %#v", got)
	}
}

func TestWordVarRefsTrailingBackslash(t *testing.T) {
	w := Word{Kind: WordBare, Text: `$x\`, Start: 0, End: 3}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "x", Start: 0, End: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWordVarRefsTrailingNamespaceSep(t *testing.T) {
	// "$ns::" with no trailing segment -> name is "ns::": tclsh 8.6 and 9.0 read
	// the trailing :: as part of the name (`set ns:: 7; puts $ns::-x` prints 7-x).
	w := Word{Kind: WordBare, Text: "$ns::", Start: 0, End: 5}
	got := WordVarRefs(w)
	want := []VarRef{{Name: "ns::", Start: 0, End: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %#v\nwant: %#v", got, want)
	}
}
