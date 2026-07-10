package tcl

import (
	"strings"
	"testing"
)

// Regression guard: unset's flag handling is positionally aware, matching
// unset(n): `-`-prefixed words are flags only in a LEADING run, and `--`
// terminates flag parsing -- after it, every word (even one starting with `-`)
// is a literal variable name. An earlier version skipped every `-`-prefixed
// word anywhere in the list, silently dropping `unset -- -foo`'s real name.
func TestUnsetHyphenNameAfterTerminator(t *testing.T) {
	src := "unset -- -foo\n"
	refs := FileRefs(src)
	off := strings.Index(src, "-foo")
	found := false
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Start == off {
			found = true
		}
	}
	if !found {
		t.Errorf("unset -- -foo: -foo (a real var name after the -- terminator) not referenced: %#v", refs)
	}
}

// Second form: a second literal "--" after the terminator is itself a valid
// (if unusual) variable name -- only the FIRST -- terminates flag parsing.
func TestUnsetDoubleDashNameAfterTerminator(t *testing.T) {
	src := "unset -- --\n"
	refs := FileRefs(src)
	count := 0
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "--" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("unset -- --: second -- (a literal var name) not referenced, got %d matching refs: %#v", count, refs)
	}
}

// ATTACK: never-wrong check -- a quoted name in "info exists" position is
// skipped (arrayBaseName requires WordBare), so a real static name written
// as a quoted string produces NO reference at all (silence, not a wrong
// ref) -- confirming the documented restriction, not inventing anything.
func TestInfoExistsQuotedNameSkippedNotWrong(t *testing.T) {
	src := `info exists "foo"` + "\n"
	refs := FileRefs(src)
	for _, r := range refs {
		if r.Ref.Kind == RefVariable {
			t.Errorf("quoted name in info-exists position should never produce a ref (or must be exactly 'foo' if ever supported): %#v", r.Ref)
		}
	}
}

// ATTACK: never-wrong check -- `array $sub ::x` where the subcommand itself
// is dynamic must not be treated as `array exists`/etc. by accident (e.g. if
// some fallback partial-match crept in).
func TestArrayDynamicSubcommandNeverGuessed(t *testing.T) {
	src := "array $sub ::x\n"
	refs := FileRefs(src)
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "::x" {
			t.Errorf("array with dynamic subcommand must not treat ::x as a name arg: %#v", r.Ref)
		}
	}
}

// ATTACK: never-wrong / shadowing hazard -- a user-defined proc named `info`
// with a DIFFERENT contract (its second argument is not a variable name) is
// indistinguishable, at this syntactic layer, from the builtin. Calling it
// invents a reference to something that is NOT actually a variable in the
// running program. This is a known limitation shared with the rest of the
// codebase's structural-certainty bet (same as `set`/`proc` shadowing), but
// is worth a regression test documenting the exposure specifically for the
// new nameArgRefs command list.
func TestInfoExistsInventsRefEvenWhenInfoIsUserShadowed(t *testing.T) {
	src := "proc info {mode target} {\n  puts $mode-$target\n}\n" +
		"info exists notAVariable\n"
	refs := FileRefs(src)
	off := strings.Index(src, "notAVariable")
	found := false
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "notAVariable" && r.Ref.Start == off {
			found = true
		}
	}
	if found {
		t.Logf("EXPECTED-GAP (documented): info exists treats notAVariable as a var ref even though `info` is user-shadowed here and its second arg is not contractually a var name: %#v", refs)
	} else {
		t.Errorf("expected the documented shadowing gap to still be present (no ref emitted) -- if this now passes, the shadowing hazard may have been fixed; update this test's expectation")
	}
}

// ATTACK: nested-substitution offset correctness -- info exists 3 levels
// deep: if-condition -> catch body -> nested if-condition. Every reported
// offset must be absolute in the ORIGINAL top-level source string.
func TestNameArgOffsetsDeeplyNested(t *testing.T) {
	src := "proc p {} {\n" +
		"  if {[catch {\n" +
		"    if {[info exists deepvar]} {\n" +
		"      puts hit\n" +
		"    }\n" +
		"  } err]} {\n" +
		"    puts $err\n" +
		"  }\n" +
		"}\n"
	refs := FileRefs(src)
	want := strings.Index(src, "deepvar")
	found := false
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "deepvar" {
			found = true
			if r.Ref.Start != want || r.Ref.End != want+len("deepvar") {
				t.Errorf("deepvar ref offset wrong: got [%d,%d), want [%d,%d)", r.Ref.Start, r.Ref.End, want, want+len("deepvar"))
			}
		}
	}
	if !found {
		t.Errorf("info exists deepvar inside catch-inside-if not found at all: %#v", refs)
	}
}

// ATTACK: double-counting -- `array unset` must emit exactly ONE reference
// and no Definition, even though `array set` (a different subcommand, same
// command family) emits a Definition for the same syntactic position.
// Regression target: a copy/paste bug that treats "unset" like "set" and
// wires up both a def AND a ref for the same site.
func TestArrayUnsetEmitsExactlyOneRefNoDef(t *testing.T) {
	src := "array unset ::cfg\n"
	refs := FileRefs(src)
	refCount := 0
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "::cfg" {
			refCount++
		}
	}
	if refCount != 1 {
		t.Errorf("array unset ::cfg: want exactly 1 ref, got %d: %#v", refCount, refs)
	}
	defs := FileDefs(src)
	for _, d := range defs {
		if d.Name == "::cfg" {
			t.Errorf("array unset must not ALSO emit a Definition for ::cfg: %#v", d)
		}
	}
}

// ATTACK: double-counting across the two independent scanners that both look
// at the same word. `info exists arr($k)` must yield the base-name ref
// exactly once (from nameArgRefs) and the subscript ref exactly once (from
// the ordinary $-scan) -- not twice each via some future refactor that makes
// nameArgRefs re-scan the whole word text instead of just the base.
func TestInfoExistsArrayElementRefsNotDoubled(t *testing.T) {
	src := "info exists arr($k)\n"
	refs := FileRefs(src)
	baseCount, subCount := 0, 0
	for _, r := range refs {
		if r.Ref.Kind != RefVariable {
			continue
		}
		switch r.Ref.Name {
		case "arr":
			baseCount++
		case "k":
			subCount++
		}
	}
	if baseCount != 1 {
		t.Errorf("want exactly 1 ref to base 'arr', got %d: %#v", baseCount, refs)
	}
	if subCount != 1 {
		t.Errorf("want exactly 1 ref to subscript 'k', got %d: %#v", subCount, refs)
	}
}
