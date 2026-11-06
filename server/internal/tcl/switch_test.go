package tcl

import (
	"sort"
	"strings"
	"testing"
)

// A switch has (pattern, body) pairs: every braced body is walked as a script and
// no pattern is ever a command, whichever of switch's shapes holds them.
func TestSwitchBodiesWalked(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"bare", "switch $x {\n stop { helper }\n}", 1},
		{"quoted pattern", "switch $x {\n \"--\" { helper }\n}", 1},
		{"braced pattern", "switch $x {\n {a b} { helper }\n}", 1},
		{"glob pattern", "switch -glob $x {\n a* { helper }\n}", 1},
		{"same line", "switch $x { a { helper } b { helper } }", 2},
		{"inline", "switch $x a { helper } b { helper }", 2},
		{"options", "switch -regexp -nocase -matchvar m -indexvar i -- $x {\n a { helper }\n}", 1},
		{"fall-through", "switch $x {\n a -\n b { helper }\n}", 1},
		{"pattern still being typed", "switch $x {\n a { helper }\n b\n}", 1},
		{"odd, braced last: pairing lost", "switch $x {\n a { helper }\n { puts 1 }\n}", 0},
		{"crlf", "switch $x {\r\n a { helper }\r\n}\r\n", 1},
		{"comment line", "switch $x {\n # note\n a { helper }\n}", 1},
		{"in a bracket", "set r [switch $x { a { helper } }]", 1},
		// isCmd's accepted limitation: a user proc named switch is read as switch.
		{"user proc, even", "switch a b { helper }", 1},
		{"user proc, odd", "switch a b c { helper }", 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
			if got := countCommandRefs(refs, "stop"); got != 0 {
				t.Errorf("pattern `stop` read as a command %d time(s); refs = %+v", got, refs)
			}
		})
	}
}

// A fall-through arm whose pattern is the word `proc` must not define a symbol.
func TestSwitchProcDashArmMakesNoSymbol(t *testing.T) {
	src := "switch $k {\n  proc -\n  method { puts 1 }\n}"
	if defs := FileDefs(src); len(defs) != 0 {
		t.Fatalf("switch arms made symbols: %+v", defs)
	}
}

// A definition inside an arm body is still a definition.
func TestSwitchDefsInsideArms(t *testing.T) {
	src := "switch $x {\n a { proc g {} {} }\n}"
	if defs := defsNamed(FileDefs(src), "::g"); len(defs) != 1 {
		t.Fatalf("::g defs = %+v, want 1", defs)
	}
}

// Both the arm list and each arm body fold.
func TestFileFolds_SwitchOuterAndInner(t *testing.T) {
	src := "switch $x {\n  a {\n    puts 1\n  }\n}\n"
	folds := FileFolds(src)

	armListOpen := strings.Index(src, "{")
	armListClose := strings.LastIndex(src, "}")
	if !foldAt(t, src, folds, armListOpen, armListClose) {
		t.Errorf("arm list not folded; got %+v", folds)
	}

	armBodyOpen := strings.Index(src, "a {") + len("a ")
	armBodyClose := strings.Index(src, "  }\n") + len("  ")
	if !foldAt(t, src, folds, armBodyOpen, armBodyClose) {
		t.Errorf("arm body not folded; got %+v", folds)
	}
}

// Review Focus 2. A switch is half-typed for most of its life in an editor: no
// prefix may panic, read a pattern as a command, or lose the definition before it.
func TestSwitchHalfTypedAtEveryPrefix(t *testing.T) {
	const procLine = "proc ok {} {}\n"
	full := procLine + "switch -glob -- $x {\n  stop { helper }\n  \"q\" -\n  {b c} { set y 1 }\n}\n"

	for end := len(procLine); end <= len(full); end++ {
		prefix := full[:end]

		refs := FileRefs(prefix)
		if got := countCommandRefs(refs, "stop"); got != 0 {
			t.Fatalf("prefix %q: pattern `stop` read as a command %d time(s)", prefix, got)
		}

		defs := FileDefs(prefix)
		if got := defsNamed(defs, "::ok"); len(got) != 1 {
			t.Fatalf("prefix %q: ::ok defs = %+v, want 1", prefix, got)
		}

		FileFolds(prefix)
		for useOffset := 0; useOffset <= len(prefix); useOffset++ {
			ReachingAt(prefix, useOffset)
		}
	}
}

// commandRefStarts returns the Start offset of every command reference named name.
func commandRefStarts(refs []ContextRef, name string) []int {
	var starts []int
	for _, ref := range refs {
		if ref.Ref.Kind == RefCommand && ref.Ref.Name == name {
			starts = append(starts, ref.Ref.Start)
		}
	}
	sort.Ints(starts)
	return starts
}

// everyIndex returns the offset of each non-overlapping occurrence of sub in src at
// or after from.
func everyIndex(src, sub string, from int) []int {
	var offsets []int
	for from <= len(src) {
		found := strings.Index(src[from:], sub)
		if found < 0 {
			break
		}
		offsets = append(offsets, from+found)
		from += found + len(sub)
	}
	return offsets
}

func sameInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// Review gap: TestSwitchBodiesWalked only checks silence for `stop`, and only the
// bare case spells its pattern `stop`. Here every shape spells its pattern with the
// name of a defined proc. Hunting: a form whose patterns still come back as calls,
// or whose bodies are not walked.
func TestSwitchPatternNamedLikeProcIsSilentInEveryForm(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"bare", "switch $x {\n stop { helper }\n}", 1},
		{"quoted", "switch $x {\n \"stop\" { helper }\n}", 1},
		{"braced", "switch $x {\n {stop} { helper }\n}", 1},
		{"glob", "switch -glob $x {\n stop { helper }\n}", 1},
		{"options", "switch -exact -nocase -- $x {\n stop { helper }\n}", 1},
		{"same line", "switch $x { stop { helper } stop { helper } }", 2},
		{"no spaces", "switch $x {stop {helper} go {helper}}", 2},
		{"inline", "switch $x stop { helper } stop { helper }", 2},
		{"inline quoted", "switch $x \"stop\" { helper }", 1},
		{"fall-through", "switch $x {\n stop -\n stop { helper }\n}", 1},
		{"inline fall-through", "switch $x stop - go { helper }", 1},
		{"pattern still being typed", "switch $x {\n a { helper }\n stop\n}", 1},
		{"inline, pattern still being typed", "switch $x a { helper } stop", 1},
		{"bracket", "set r [switch $x { stop { helper } }]", 1},
		{"bracket, inline", "set r [switch $x stop { helper }]", 1},
		{"crlf", "switch $x {\r\n stop {\r\n helper }\r\n}\r\n", 1},
		{"before default", "switch $x {\n stop { helper }\n default { helper }\n}", 2},
		{"in a proc", "proc p {} {\n switch $x {\n  stop { helper }\n }\n}", 1},
		{"in an if", "if {1} {\n switch $x {\n  stop { helper }\n }\n}", 1},
		{"in a catch", "catch {switch $x {stop {helper}}}", 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "stop"); got != 0 {
				t.Errorf("pattern `stop` read as a command %d time(s); refs = %+v", got, refs)
			}
			if got := countCommandRefs(refs, "go"); got != 0 {
				t.Errorf("pattern `go` read as a command %d time(s)", got)
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d", got, testCase.helpers)
			}
		})
	}
}

// `-` means "same body as the next arm" only in body position. Hunting: a `-`
// handled by position-blind code, which would shift every later pairing by one or
// drop the arm that follows, and a `-` as the very last word.
func TestSwitchDashEdges(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"dash as last arm", "switch $x {\n a { helper }\n b -\n}", 1},
		{"dash as only arm", "switch $x {\n b -\n}", 0},
		{"dash as pattern", "switch $x {\n - { helper }\n}", 1},
		{"dash body then dash pattern", "switch $x {\n a - - { helper }\n}", 1},
		{"dash chain", "switch $x {\n a -\n b -\n c -\n d { helper }\n}", 1},
		{"dash chain, then dash last", "switch $x {\n a { helper }\n b -\n c -\n}", 1},
		{"inline dash last", "switch $x a { helper } b -", 1},
		{"inline dash pattern", "switch $x - { helper }", 1},
		{"dash then default", "switch $x {\n a -\n default { helper }\n}", 1},
		{"dash pattern, odd braced last: pairing lost", "switch $x {\n a { helper }\n - -\n { helper }\n}", 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			for _, name := range []string{"-", "a", "b", "c", "d"} {
				if got := countCommandRefs(refs, name); got != 0 {
					t.Errorf("pattern or body `%s` read as a command %d time(s); refs = %+v", name, got, refs)
				}
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
		})
	}
}

// `default` is an ordinary pattern word to the parser. Hunting: it read as a call,
// or special-cased so the arms around it are dropped.
func TestSwitchDefaultArm(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"only arm", "switch $x { default { helper } }", 1},
		{"between arms", "switch $x {\n a { helper }\n default { helper }\n b { helper }\n}", 3},
		{"inline", "switch $x default { helper }", 1},
		{"quoted", "switch $x {\n \"default\" { helper }\n}", 1},
		{"fall-through", "switch $x {\n default -\n a { helper }\n}", 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "default"); got != 0 {
				t.Errorf("`default` read as a command %d time(s)", got)
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d", got, testCase.helpers)
			}
		})
	}
}

// A switch in an arm of a switch. Hunting: the inner list flattened into the outer
// pairing (shifting it), and wrong offsets from stacking the shift twice.
func TestSwitchNestedInArm(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"list in list", "switch $x {\n outerpat {\n  switch $y {\n   innerpat { helper }\n  }\n }\n}", 1},
		{"inline in list", "switch $x {\n outerpat { switch $y innerpat { helper } }\n}", 1},
		{"list in inline", "switch $x outerpat { switch $y {\n innerpat { helper }\n} }", 1},
		{"three deep", "switch $a {\n p1 {\n switch $b {\n  p2 {\n   switch $c {\n    p3 { helper }\n   }\n  }\n }\n }\n}", 1},
		{"inner between two calls", "switch $x {\n outerpat {\n  helper\n  switch $y {\n   innerpat { helper }\n  }\n  helper\n }\n}", 3},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			for _, name := range []string{"outerpat", "innerpat", "p1", "p2", "p3"} {
				if got := countCommandRefs(refs, name); got != 0 {
					t.Errorf("pattern `%s` read as a command %d time(s)", name, got)
				}
			}
			starts := commandRefStarts(refs, "helper")
			want := everyIndex(testCase.src, "helper", 0)
			if !sameInts(starts, want) {
				t.Errorf("helper ref offsets = %v, want %v", starts, want)
			}
			if len(starts) != testCase.helpers {
				t.Errorf("helper refs = %d, want %d", len(starts), testCase.helpers)
			}
		})
	}
}

// Every ref offset is an absolute byte offset. Hunting: the `list.Start + 1` shift
// computed in runes instead of bytes, applied to the wrong base when the switch is
// itself inside a body, or lost when the arm list follows multi-byte text on the
// same line or hundreds of lines earlier.
func TestSwitchArmBodyOffsetsAfterMultiByteText(t *testing.T) {
	padding := strings.Repeat("# 世界 😀 é\n", 300)
	cases := []struct{ name, src string }{
		{"deep in the file", padding + "proc helper {} {}\nswitch -glob -- $s {\n  \"é*\" { helper }\n  {日本 語} -\n  日本* { helper }\n  default { helper }\n}\n"},
		{"after multi-byte text on the same line", "set s \"日本語 😀\"; switch $s { a { helper } 世 { helper } }"},
		{"multi-byte subject", "switch \"日本語\" { a { helper } b { helper } }"},
		{"inline, multi-byte patterns", padding + "switch $s 世界 { helper } 😀 { helper }"},
		{"in a proc in a namespace", padding + "namespace eval ns {\n  proc f {} {\n    set é 1\n    switch $é {\n      \"世\" { helper }\n      b { helper }\n    }\n  }\n}\n"},
		{"in an itcl method", padding + "itcl::class C {\n  method m {} {\n    switch $é {\n      \"世\" { helper }\n    }\n  }\n}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want := everyIndex(testCase.src, "helper", strings.Index(testCase.src, "switch"))
			if got := commandRefStarts(FileRefs(testCase.src), "helper"); !sameInts(got, want) {
				t.Errorf("helper ref offsets = %v, want %v", got, want)
			}
		})
	}
}

// A ref found in an arm must carry the same namespace, frame, scope and class as a
// call written next to the switch. Hunting: arm bodies walked with the default
// context instead of the enclosing one, which silently resolves locals and methods
// against the wrong scope.
func TestSwitchArmBodyKeepsEnclosingContext(t *testing.T) {
	cases := []struct{ name, src string }{
		{"proc", "proc f {y} {\n  helper\n  switch $k {\n    \"a\" { helper }\n  }\n}"},
		{"proc in a namespace", "namespace eval n {\n  proc f {y} {\n    helper\n    switch $k {\n      \"a\" { helper }\n    }\n  }\n}"},
		{"namespace body", "namespace eval n {\n  helper\n  switch $k {\n    \"a\" { helper }\n  }\n}"},
		{"itcl method", "itcl::class C {\n  method m {y} {\n    helper\n    switch $k {\n      \"a\" { helper }\n    }\n  }\n}"},
		{"inline form in a proc", "proc f {y} {\n  helper\n  switch $k \"a\" { helper }\n}"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var helpers []ContextRef
			for _, ref := range FileRefs(testCase.src) {
				if ref.Ref.Kind == RefCommand && ref.Ref.Name == "helper" {
					helpers = append(helpers, ref)
				}
			}
			if len(helpers) != 2 {
				t.Fatalf("helper refs = %d, want 2 (one beside the switch, one in the arm)", len(helpers))
			}
			sort.Slice(helpers, func(i, j int) bool { return helpers[i].Ref.Start < helpers[j].Ref.Start })
			beside, inArm := helpers[0], helpers[1]
			if beside.Namespace != inArm.Namespace || beside.Frame != inArm.Frame ||
				beside.Scope != inArm.Scope || beside.Class != inArm.Class {
				t.Errorf("arm ref context differs from the call beside the switch:\n beside = %+v\n in arm = %+v", beside, inArm)
			}
		})
	}
}

// Hunting: a body that ends in an escaped brace or backslash, which breaks any
// re-scan of the arm list that counts braces itself instead of using Parse.
func TestSwitchEscapesInsideArmBodies(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"escaped close brace", "switch $x {\n a { puts \\} ; helper }\n b { helper }\n}", 2},
		{"escaped open brace", "switch $x {\n a { puts \\{ ; helper }\n b { helper }\n}", 2},
		{"balanced braces in a string", "switch $x {\n a { puts \"{x}\" ; helper }\n b { helper }\n}", 2},
		{"escaped backslash before the close", "switch $x {\n a { puts \\\\ ; helper \\\\ }\n b { helper }\n}", 2},
		{"backslash-newline between pattern and body", "switch $x {\n a \\\n { helper }\n b { helper }\n}", 2},
		{"body on its own line", "switch $x {\n a\n { helper }\n b\n { helper }\n}", 2},
		{"inline, escaped close brace", "switch $x a { puts \\} ; helper } b { helper }", 2},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
			for _, name := range []string{"a", "b"} {
				if got := countCommandRefs(refs, name); got != 0 {
					t.Errorf("pattern `%s` read as a command %d time(s)", name, got)
				}
			}
		})
	}
}

// Without a subject there are no arms. Hunting: option parsing that runs off the
// end, an arm list picked from the first braced word although it is the subject,
// and `-matchvar` failing to swallow `--` as its variable.
func TestSwitchWithoutSubjectWalksNothing(t *testing.T) {
	sources := []string{
		"switch",
		"switch -glob",
		"switch --",
		"switch -glob --",
		"switch -matchvar",
		"switch -matchvar m",
		"switch -matchvar --",
		"switch -matchvar -- { a { helper } }",
		"switch -indexvar -- { a { helper } }",
		"switch -exact { a { helper } }",
		"switch -glob -- { a { helper } }",
		"switch { a { helper } }",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			refs := FileRefs(src)
			FileDefs(src)
			FileFolds(src)
			for useOffset := 0; useOffset <= len(src); useOffset++ {
				ReachingAt(src, useOffset)
			}
			if got := countCommandRefs(refs, "helper"); got != 0 {
				t.Errorf("helper refs = %d, want 0 (no subject, so no arms); refs = %+v", got, refs)
			}
		})
	}
}

// After `--` the next word is the subject whatever it looks like, and a subject of
// any word kind is skipped before the arms. Hunting: an option loop that keeps
// matching after `--`, and an arm list taken from the first braced word.
func TestSwitchSubjectShapes(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"subject looks like -glob", "switch -- -glob { a { helper } }", 1},
		{"subject looks like -matchvar", "switch -- -matchvar { a { helper } }", 1},
		{"subject is --", "switch -regexp -- -- { a { helper } }", 1},
		{"subject looks like -exact, after real options", "switch -glob -nocase -- -exact { a { helper } }", 1},
		{"every option", "switch -exact -glob -regexp -nocase -matchvar m -indexvar i -- $x { a { helper } }", 1},
		{"empty subject", "switch {} { a { helper } }", 1},
		{"braced subject", "switch {c d} { a { helper } }", 1},
		{"braced subject, inline", "switch {c d} a { helper }", 1},
		{"quoted subject", "switch \"c d\" { a { helper } }", 1},
		{"bracket subject", "switch [string tolower $x] { a { helper } }", 1},
		{"inline, bracket subject", "switch [string tolower $x] a { helper }", 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			for _, name := range []string{"a", "c", "d"} {
				if got := countCommandRefs(refs, name); got != 0 {
					t.Errorf("`%s` read as a command %d time(s)", name, got)
				}
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
		})
	}
}

// Substitution applies to the subject always, to inline patterns, and to nothing in
// a braced arm list. Hunting: a literal `[helper]` or `$y` pattern reported as a
// call or a variable use (a wrong answer), and a real `[helper]` in the subject or
// an inline pattern lost (a regression from the current walker).
func TestSwitchSubstitutionInPatterns(t *testing.T) {
	cases := []struct {
		name, src      string
		helpers, yUses int
	}{
		{"braced list: bracket pattern is literal", "switch $x {\n [helper] { puts 1 }\n}", 0, 0},
		{"braced list: dollar pattern is literal", "switch $x {\n $y { puts 1 }\n}", 0, 0},
		{"braced list: bracket in a braced pattern is literal", "switch $x {\n {[helper]} { puts 1 }\n}", 0, 0},
		{"subject bracket is a call", "switch [helper] {\n a { puts 1 }\n}", 1, 0},
		{"subject dollar is a use", "switch $y {\n a { puts 1 }\n}", 0, 1},
		{"inline: bracket pattern is a call", "switch $x [helper] { puts 1 }", 1, 0},
		{"inline: dollar pattern is a use", "switch $x $y { puts 1 }", 0, 1},
		{"inline: quoted dollar pattern is a use", "switch $x \"$y\" { puts 1 }", 0, 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
			if got := countVarRefs(refs, "y"); got != testCase.yUses {
				t.Errorf("$y uses = %d, want %d; refs = %+v", got, testCase.yUses, refs)
			}
		})
	}
}

// A pattern or body word that spells a defining command is data, not a definition.
// Hunting: arm words still handed to the definition walker, which creates symbols
// (a wrong answer: a jump target that does not exist). `proc f {} {}` has 4 words,
// so it pairs as (proc, f) and ({}, {}).
func TestSwitchPatternWordsDefineNothing(t *testing.T) {
	cases := []struct{ name, arm string }{
		{"proc", "proc f {} {}"},
		{"proc with a body", "proc f {} { puts 1 }"},
		{"itcl::class", "itcl::class K {}"},
		{"namespace eval", "namespace eval n {}"},
		{"variable", "variable v"},
		{"set", "set v"},
		{"global", "global v"},
		{"foreach", "foreach v"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			list := "switch $k {\n  " + testCase.arm + "\n}"
			if defs := FileDefs(list); len(defs) != 0 {
				t.Errorf("arm list made symbols: %+v", defs)
			}
			inline := "switch $k " + testCase.arm
			if defs := FileDefs(inline); len(defs) != 0 {
				t.Errorf("inline form made symbols: %+v", defs)
			}
		})
	}
}

// Same hunt for the arms of a switch directly in an itcl class body, where `method`
// and `proc` are definitions.
func TestSwitchInClassBodyPatternsDefineNoMethods(t *testing.T) {
	src := "itcl::class K {\n  switch $k {\n    method m {} {}\n    proc p {} {}\n    a { puts 1 }\n  }\n}"
	for _, def := range FileDefs(src) {
		if def.Kind == DefMethod || def.Name == "::p" || def.Name == "p" {
			t.Errorf("pattern word made a symbol: %+v", def)
		}
	}
}

// `#` is a comment only at the start of a command. Hunting: a comment stripper that
// swallows a real `#` pattern in the middle of a line, and a `#` first word that
// takes the next arm with it.
func TestSwitchHashPattern(t *testing.T) {
	cases := []struct {
		name, src string
		helpers   int
	}{
		{"comment, then arm", "switch $x {\n # note\n a { helper }\n}", 1},
		{"comment between arms", "switch $x {\n a { helper }\n # note\n b { helper }\n}", 2},
		{"comment last", "switch $x {\n a { helper }\n # note\n}", 1},
		{"comment on the opening line", "switch $x {# note\n a { helper }\n}", 1},
		{"real hash pattern mid-line", "switch $x {\n a - # { helper }\n}", 1},
		{"inline: hash pattern", "switch $x # { helper }", 1},
		{"hash line, then another arm", "switch $x {\n # { puts 1 }\n a { helper }\n}", 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "#"); got != 0 {
				t.Errorf("`#` read as a command %d time(s)", got)
			}
			if got := countCommandRefs(refs, "helper"); got != testCase.helpers {
				t.Errorf("helper refs = %d, want %d; refs = %+v", got, testCase.helpers, refs)
			}
		})
	}
}

// Hunting: a panic or a wrong answer in an unterminated switch, at every cut of a
// source that mixes the arm list, the inline form, options, nesting and a
// definition on either side. Folds must stay inside the source and keep Open < Close.
func TestSwitchUnterminatedAtEveryPrefix(t *testing.T) {
	const procLine = "proc ok {} {}\n"
	tails := []string{
		"switch -exact -nocase -- $x {\n  stop { helper }\n  {a b} -\n  \"c\" { switch $y {\n    stop { helper }\n  } }\n  default { set z 1 }\n}\nproc after {} {}\n",
		"switch -glob $x stop { helper } {a b} - \"c\" { helper } default { helper }\nproc after {} {}\n",
		"set r [switch $x { stop { helper } }]\nproc after {} {}\n",
		"proc p {} {\n  switch $x {\n    stop { helper }\n  }\n}\nproc after {} {}\n",
	}
	for tailIndex, tail := range tails {
		full := procLine + tail
		for end := len(procLine); end <= len(full); end++ {
			prefix := full[:end]
			if got := countCommandRefs(FileRefs(prefix), "stop"); got != 0 {
				t.Fatalf("tail %d, prefix %q: pattern `stop` read as a command %d time(s)", tailIndex, prefix, got)
			}
			if got := defsNamed(FileDefs(prefix), "::ok"); len(got) != 1 {
				t.Fatalf("tail %d, prefix %q: ::ok defs = %+v, want 1", tailIndex, prefix, got)
			}
			for _, fold := range FileFolds(prefix) {
				if fold.Open < 0 || fold.Open >= fold.Close || fold.Close > len(prefix) {
					t.Fatalf("tail %d, prefix %q: bad fold %+v", tailIndex, prefix, fold)
				}
			}
			for useOffset := 0; useOffset <= len(prefix); useOffset++ {
				ReachingAt(prefix, useOffset)
			}
		}
	}
}

// Deterministic unterminated shapes: a complete pair before the cut is still
// certain, a half-written one is silent, and the pattern is never a call.
func TestSwitchUnterminatedBodies(t *testing.T) {
	cases := []struct {
		name, src              string
		minHelpers, maxHelpers int
	}{
		{"inline: last body open", "switch $x a { helper } b {", 1, 1},
		{"inline: only body open", "switch $x a {", 0, 0},
		{"arm list open, no arms", "switch $x {", 0, 0},
		{"arm list open after a complete arm", "switch $x {\n a { helper }\n", 0, 1},
		{"arm list open inside an arm", "switch $x {\n a { helper", 0, 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs := FileRefs(testCase.src)
			if got := countCommandRefs(refs, "a"); got != 0 {
				t.Errorf("pattern `a` read as a command %d time(s)", got)
			}
			got := countCommandRefs(refs, "helper")
			if got < testCase.minHelpers || got > testCase.maxHelpers {
				t.Errorf("helper refs = %d, want %d..%d; refs = %+v", got, testCase.minHelpers, testCase.maxHelpers, refs)
			}
		})
	}
}

// Hunting: arm bindings dropped from the set that reaches a use after the switch,
// for every shape of switch (the inline workaround is being removed).
func TestSwitchArmBindingsReachAfterSwitch(t *testing.T) {
	cases := []struct {
		name, src string
		hasEntry  bool // `set x 0` before the switch may reach: no arm is provably taken
	}{
		{"quoted patterns", "proc f {} {\n  set x 0\n  switch $k {\n    \"a\" { set x 1 }\n    \"b\" { set x 2 }\n  }\n  puts $x\n}", true},
		{"braced patterns", "proc f {} {\n  set x 0\n  switch $k {\n    {a b} { set x 1 }\n    {c d} { set x 2 }\n  }\n  puts $x\n}", true},
		{"fall-through and default", "proc f {} {\n  set x 0\n  switch -glob -- $k {\n    a -\n    b { set x 1 }\n    default { set x 2 }\n  }\n  puts $x\n}", false},
		{"inline with options", "proc f {} {\n  set x 0\n  switch -exact -- $k a { set x 1 } b - c { set x 2 }\n  puts $x\n}", true},
		{"same line", "proc f {} {\n  set x 0\n  switch $k { a { set x 1 } b { set x 2 } }\n  puts $x\n}", true},
		{"nested switch", "proc f {} {\n  set x 0\n  switch $k {\n    a {\n      switch $j {\n        q { set x 1 }\n      }\n    }\n    b { set x 2 }\n  }\n  puts $x\n}", true},
		{"inside an if", "proc f {} {\n  set x 0\n  if {$c} {\n    switch $k {\n      a { set x 1 }\n    }\n  }\n  puts $x\n}", true},
		{"inside a foreach", "proc f {} {\n  set x 0\n  foreach i $l {\n    switch $k {\n      a { set x 1 }\n    }\n  }\n  puts $x\n}", true},
		{"after multi-byte text", "proc f {} {\n  # 世界 😀\n  set x 0\n  switch $k {\n    \"é\" { set x 1 }\n    \"世\" { set x 2 }\n  }\n  puts $x\n}", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			defs := reachAtMarker(t, testCase.src, "$x")
			reaching := map[int]bool{}
			for _, def := range defs {
				reaching[def.NameStart] = true
			}
			setSites := everyIndex(testCase.src, "set x ", 0)
			for index, site := range setSites {
				if index == 0 && !testCase.hasEntry {
					continue
				}
				if !reaching[site+len("set ")] {
					t.Errorf("binding #%d at %d does not reach the use; got %+v", index, site+len("set "), defs)
				}
			}
			if len(defs) > len(setSites) {
				t.Errorf("more reaching defs than bindings: %+v", defs)
			}
		})
	}
}

// Hunting: a pattern or body word spelling set, incr or lappend still read as a
// binding of the variable named by the next word (a wrong reaching answer).
func TestSwitchPatternWordsBindNothing(t *testing.T) {
	for _, word := range []string{"set", "incr", "append", "lappend", "variable", "global"} {
		t.Run(word, func(t *testing.T) {
			src := "proc f {} {\n  switch $k {\n    " + word + " y\n    a { puts 1 }\n  }\n  puts $y\n}"
			if defs, ok := ReachingAt(src, strings.LastIndex(src, "$y")+1); ok && len(defs) != 0 {
				t.Errorf("a pattern word bound y: %+v", defs)
			}
		})
	}
}

// Hunting: the extra folds. Only real braced script bodies fold: not the subject,
// not a braced pattern, not a braced option argument, and each only once.
func TestFileFolds_SwitchFoldsExactlyTheBodies(t *testing.T) {
	cases := []struct {
		name, src string
		folds     int
	}{
		{"arm list and two bodies", "switch $x {\n  a {\n    puts 1\n  }\n  b {\n    puts 2\n  }\n}\n", 3},
		{"inline: two bodies, no arm list", "switch $x a {\n puts 1\n} b {\n puts 2\n}\n", 2},
		{"braced pattern does not fold", "switch $x {\n  {a b} {\n    puts 1\n  }\n}\n", 2},
		{"braced subject does not fold", "switch {a b} {\n  c {\n    puts 1\n  }\n}\n", 2},
		{"fall-through folds one body", "switch $x {\n  a -\n  b {\n    puts 1\n  }\n}\n", 2},
		{"nested switch", "switch $x {\n  a {\n    switch $y {\n      b {\n        puts 1\n      }\n    }\n  }\n}\n", 4},
		{"options only, nothing to fold", "switch -glob\n", 0},
		{"subject only, nothing to fold", "switch {a b}\n", 0},
		{"braced subject without arms", "switch -exact {\n  a {\n    puts 1\n  }\n}\n", 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			folds := FileFolds(testCase.src)
			if len(folds) != testCase.folds {
				t.Errorf("folds = %d, want %d: %+v", len(folds), testCase.folds, folds)
			}
			seen := map[FoldRange]bool{}
			for _, fold := range folds {
				if seen[fold] {
					t.Errorf("duplicate fold %+v", fold)
				}
				seen[fold] = true
				if testCase.src[fold.Open] != '{' || testCase.src[fold.Close] != '}' {
					t.Errorf("fold %+v is not on braces", fold)
				}
			}
		})
	}
}

// Fold offsets are bytes: deep in the file, after multi-byte text, inside a proc.
func TestFileFolds_SwitchOffsetsAfterMultiByteText(t *testing.T) {
	padding := strings.Repeat("# 世界 😀 é\n", 300)
	src := padding + "proc f {} {\n  switch \"世\" {\n    \"é\" {\n      puts 1\n    }\n    日本 -\n    default {\n      puts 2\n    }\n  }\n}\n"
	folds := FileFolds(src)

	armListOpen := strings.Index(src, "\"世\" {") + len("\"世\" ")
	armListClose := strings.LastIndex(src, "  }\n}") + len("  ")
	firstBodyOpen := strings.Index(src, "\"é\" {") + len("\"é\" ")
	firstBodyClose := strings.Index(src, "    }\n    日本") + len("    ")
	defaultBodyOpen := strings.Index(src, "default {") + len("default ")
	defaultBodyClose := strings.Index(src, "    }\n  }\n}") + len("    ")
	spans := map[string][2]int{
		"arm list":     {armListOpen, armListClose},
		"first body":   {firstBodyOpen, firstBodyClose},
		"default body": {defaultBodyOpen, defaultBodyClose},
	}
	for label, span := range spans {
		if !foldAt(t, src, folds, span[0], span[1]) {
			t.Errorf("%s not folded at [%d,%d]; got %+v", label, span[0], span[1], folds)
		}
	}
}

// The inline form `switch $k proc - method { ... }` has the shape the decorated-proc
// matcher looks for (`WRAPPER ... proc NAME ARGS BODY`), so its head `switch` reads
// as a macro and `-` as a proc name. Hunting: that matcher still running on a
// switch whose pattern is the word `proc`; the list form is covered by
// TestSwitchProcDashArmMakesNoSymbol, this is the inline twin.
func TestSwitchInlineProcDashMakesNoSymbol(t *testing.T) {
	sources := []string{
		"switch $k proc - method { puts 1 }",
		"switch -exact -- $k proc - method { puts 1 }",
		"switch $k a { puts 1 } proc - method { puts 1 }",
		"switch $k proc f {} {}",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			if defs := FileDefs(src); len(defs) != 0 {
				t.Errorf("inline switch made symbols: %+v", defs)
			}
		})
	}
}

// Namespace imports and class inheritance declared in an arm BODY are still
// declarations; the same words as a PATTERN are not.
func TestSwitchArmsDeclareNamespacesAndClassesOnlyInBodies(t *testing.T) {
	imports := func(src string) []string {
		if info := FileNamespaces(src)["::"]; info != nil {
			return info.Imports
		}
		return nil
	}

	if got := imports("switch $k {\n  a { namespace import ::p::x }\n}"); len(got) != 1 || got[0] != "::p::x" {
		t.Errorf("import in an arm body = %v, want [::p::x]", got)
	}
	if got := FileClasses("switch $k {\n  a { itcl::class ::D { inherit ::B } }\n}")["::D"]; len(got) != 1 || got[0] != "::B" {
		t.Errorf("inherit in an arm body = %v, want [::B]", got)
	}

	for _, src := range []string{
		"switch $k {\n  namespace import ::p::x ::p::y\n}",
		"switch $k namespace import ::p::x ::p::y",
	} {
		if got := imports(src); len(got) != 0 {
			t.Errorf("%q: pattern words imported %v", src, got)
		}
	}
	for _, src := range []string{
		"switch $k {\n  itcl::class ::D { inherit ::B } x\n}",
		"switch $k itcl::class ::D { inherit ::B } x",
	} {
		if got := FileClasses(src); len(got) != 0 {
			t.Errorf("%q: pattern words declared classes %v", src, got)
		}
	}
}
