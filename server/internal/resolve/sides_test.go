package resolve

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const sidesFile = "main.tcl"

func resolverOver(src string) *Resolver {
	ix := index.New()
	ix.IndexFile(sidesFile, src)
	return New(ix)
}

// sideOffsets locates each site of sideCollisionSource, where one name is a proc
// and a variable at once. A variable use's reference range starts at its `$`, so
// varUseCursor sits on the name and varUseStart is the range the reference reports.
type sideOffsets struct {
	procName, setName, call, varUseCursor, varUseStart int
}

const sideCollisionSource = "proc test {} {}\nset test 1\ntest\nputs $test"

func offsetsInCollisionSource() sideOffsets {
	src := sideCollisionSource
	return sideOffsets{
		procName:     strings.Index(src, "proc test") + len("proc "),
		setName:      strings.Index(src, "set test") + len("set "),
		call:         strings.Index(src, "\ntest\n") + len("\n"),
		varUseCursor: strings.Index(src, "$test") + len("$"),
		varUseStart:  strings.Index(src, "$test"),
	}
}

func TestCommandRefIgnoresSameNamedVariable(t *testing.T) {
	src := "proc ::rmq::debug {m} {}\nset ::rmq::debug 1\n::rmq::debug hello"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.LastIndex(src, "::rmq::debug"))
	if len(locs) != 1 || locs[0].Kind != tcl.DefProc {
		t.Fatalf("Definition on the call = %#v, want only the proc", locs)
	}
}

func TestVariableRefIgnoresSameNamedProc(t *testing.T) {
	src := "proc ::rmq::debug {m} {}\nset ::rmq::debug 1\n::rmq::debug hello\nputs $::rmq::debug"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.Index(src, "$::rmq::debug")+len("$"))
	if len(locs) != 1 || locs[0].Kind != tcl.DefNamespaceVar {
		t.Fatalf("Definition on the variable = %#v, want only the variable", locs)
	}
}

func TestGlobalLinkChaseIgnoresSameNamedProc(t *testing.T) {
	src := "proc test {} {}\nset test 1\nproc f {} {\n  global test\n  puts $test\n}"
	r := resolverOver(src)

	want := strings.Index(src, "set test") + len("set ")
	locs := r.Definition(sidesFile, src, strings.Index(src, "$test")+len("$"))
	if len(locs) != 1 || locs[0].NameStart != want || locs[0].Kind != tcl.DefNamespaceVar {
		t.Fatalf("Definition through the global link = %#v, want only `set test` at %d", locs, want)
	}
}

// Find-references answers from the side of the symbol under the cursor, whether
// the cursor is on a definition or on a use.
func TestReferencesOfProcExcludeVariableUses(t *testing.T) {
	src := sideCollisionSource
	at := offsetsInCollisionSource()
	r := resolverOver(src)

	for label, from := range map[string]int{"proc": at.procName, "call": at.call} {
		if got := startsOf(r.References(sidesFile, src, from)); !sameOffsets(got, []int{at.call}) {
			t.Errorf("References from the %s = %v, want only the call at %d", label, got, at.call)
		}
	}
}

func TestReferencesOfVariableExcludeProcCalls(t *testing.T) {
	src := sideCollisionSource
	at := offsetsInCollisionSource()
	r := resolverOver(src)

	for label, from := range map[string]int{"set": at.setName, "use": at.varUseCursor} {
		if got := startsOf(r.References(sidesFile, src, from)); !sameOffsets(got, []int{at.varUseStart}) {
			t.Errorf("References from the %s = %v, want only $test at %d", label, got, at.varUseStart)
		}
	}
}

func TestDeclarationsHonorSide(t *testing.T) {
	src := sideCollisionSource
	at := offsetsInCollisionSource()
	r := resolverOver(src)

	cases := []struct {
		label    string
		from     int
		want     int
		wantKind tcl.DefKind
	}{
		{"the call", at.call, at.procName, tcl.DefProc},
		{"the proc", at.procName, at.procName, tcl.DefProc},
		{"$test", at.varUseCursor, at.setName, tcl.DefNamespaceVar},
		{"set test", at.setName, at.setName, tcl.DefNamespaceVar},
	}
	for _, c := range cases {
		locs := r.Declarations(sidesFile, src, c.from)
		if len(locs) != 1 || locs[0].NameStart != c.want || locs[0].Kind != c.wantKind {
			t.Errorf("Declarations from %s = %#v, want only the site at %d", c.label, locs, c.want)
		}
	}
}

func TestFileHighlightsHonorSide(t *testing.T) {
	src := sideCollisionSource
	at := offsetsInCollisionSource()
	r := resolverOver(src)

	commandSide := []int{at.procName, at.call}
	variableSide := []int{at.setName, at.varUseStart}
	cases := []struct {
		label string
		from  int
		want  []int
	}{
		{"the proc", at.procName, commandSide},
		{"the call", at.call, commandSide},
		{"set test", at.setName, variableSide},
		{"$test", at.varUseCursor, variableSide},
	}
	for _, c := range cases {
		if got := startsOf(r.FileHighlights(sidesFile, src, c.from)); !sameOffsets(got, c.want) {
			t.Errorf("FileHighlights from %s = %v, want %v", c.label, got, c.want)
		}
	}
}

func TestCommandRefKindIgnoresVariable(t *testing.T) {
	src := "set ::a::f 1\nproc ::a::f {} {}\n::a::f"
	r := resolverOver(src)

	call := strings.LastIndex(src, "::a::f")
	refs := source.Refs(sidesFile, src)
	for i := range refs {
		if refs[i].Ref.Kind != tcl.RefCommand || refs[i].Ref.Start != call {
			continue
		}
		kind, ok := r.CommandRefKind(&refs[i], sidesFile)
		if !ok || kind != tcl.DefProc {
			t.Fatalf("CommandRefKind = (%v, %v), want (DefProc, true)", kind, ok)
		}
		return
	}
	t.Fatalf("no command reference at offset %d in %#v", call, refs)
}

func TestBareCommandWithOnlyVariableStaysSilent(t *testing.T) {
	src := "set key 1\nkey id"
	r := resolverOver(src)

	if locs := r.Definition(sidesFile, src, strings.Index(src, "key id")); len(locs) != 0 {
		t.Fatalf("Definition on `key id` = %#v, want nil", locs)
	}
}

func TestCommandFallsThroughVariableToOuterProc(t *testing.T) {
	src := "proc debug {} {}\nnamespace eval ::n {\n  variable debug 1\n  debug\n}"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.LastIndex(src, "debug"))
	if len(locs) != 1 || locs[0].Name != "::debug" || locs[0].Kind != tcl.DefProc {
		t.Fatalf("Definition on `debug` = %#v, want only the proc ::debug", locs)
	}
}

// Guard: a class is on the command side, because its name is called like a command.
func TestClassHeadStillResolves(t *testing.T) {
	src := "itcl::class ::Foo {}\nset o [::Foo #auto]"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.Index(src, "[::Foo")+len("["))
	if len(locs) != 1 || locs[0].Kind != tcl.DefClass {
		t.Fatalf("Definition on the class head = %#v, want only the class", locs)
	}
}

// ---- Adversarial additions -------------------------------------------------

type namedSource struct{ path, text string }

// resolverOverFiles indexes the files in the order given, so a test controls which
// side of a collision Index.Lookup returns first.
func resolverOverFiles(files ...namedSource) *Resolver {
	ix := index.New()
	for _, file := range files {
		ix.IndexFile(file.path, file.text)
	}
	return New(ix)
}

// offsetOfIn returns the offset of name inside the first occurrence of anchor.
func offsetOfIn(t *testing.T, src, anchor, name string) int {
	t.Helper()
	base := strings.Index(src, anchor)
	inner := strings.Index(anchor, name)
	if base < 0 || inner < 0 {
		t.Fatalf("test bug: %q / %q not found in %q", anchor, name, src)
	}
	return base + inner
}

// sitesOf renders locations as sorted "file@offset" strings, for cross-file checks.
func sitesOf(locs []index.Location) []string {
	out := make([]string, 0, len(locs))
	for _, loc := range locs {
		out = append(out, fmt.Sprintf("%s@%d", loc.File, loc.NameStart))
	}
	sort.Strings(out)
	return out
}

func siteList(file string, offsets ...int) []string {
	out := make([]string, 0, len(offsets))
	for _, offset := range offsets {
		out = append(out, fmt.Sprintf("%s@%d", file, offset))
	}
	sort.Strings(out)
	return out
}

func sameSites(got, want []string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

func kindsOf(locs []index.Location) []tcl.DefKind {
	out := make([]tcl.DefKind, 0, len(locs))
	for _, loc := range locs {
		out = append(out, loc.Kind)
	}
	return out
}

func hasKind(locs []index.Location, kind tcl.DefKind) bool {
	for _, loc := range locs {
		if loc.Kind == kind {
			return true
		}
	}
	return false
}

// Find-references and highlight must start from every kind of site on each side,
// including the bareword variable-name arguments of `info exists`, `unset` and
// `array get`. Those are variable uses, so they must never pull in the proc.
func TestEverySiteKindStaysOnItsOwnSide(t *testing.T) {
	src := "proc ::cfg {} {}\nset ::cfg 1\ninfo exists ::cfg\nunset ::cfg\narray get ::cfg\n::cfg\nputs $::cfg"
	r := resolverOver(src)

	procName := offsetOfIn(t, src, "proc ::cfg", "::cfg")
	setName := offsetOfIn(t, src, "set ::cfg", "::cfg")
	infoExists := offsetOfIn(t, src, "info exists ::cfg", "::cfg")
	unsetName := offsetOfIn(t, src, "unset ::cfg", "::cfg")
	arrayGet := offsetOfIn(t, src, "array get ::cfg", "::cfg")
	call := offsetOfIn(t, src, "\n::cfg\n", "::cfg")
	varUseStart := strings.Index(src, "$::cfg")

	commandHighlights, commandReferences := []int{procName, call}, []int{call}
	variableHighlights := []int{setName, infoExists, unsetName, arrayGet, varUseStart}
	variableReferences := []int{infoExists, unsetName, arrayGet, varUseStart}
	cases := []struct {
		label      string
		cursor     int
		highlights []int
		references []int
	}{
		{"proc name", procName, commandHighlights, commandReferences},
		{"call", call, commandHighlights, commandReferences},
		{"set name", setName, variableHighlights, variableReferences},
		{"info exists name", infoExists, variableHighlights, variableReferences},
		{"unset name", unsetName, variableHighlights, variableReferences},
		{"array get name", arrayGet, variableHighlights, variableReferences},
		{"$use", varUseStart + 1, variableHighlights, variableReferences},
	}
	for _, c := range cases {
		if got := startsOf(r.FileHighlights(sidesFile, src, c.cursor)); !sameOffsets(got, c.highlights) {
			t.Errorf("FileHighlights from %s = %v, want %v", c.label, got, c.highlights)
		}
		if got := startsOf(r.References(sidesFile, src, c.cursor)); !sameOffsets(got, c.references) {
			t.Errorf("References from %s = %v, want %v", c.label, got, c.references)
		}
	}
}

// `variable x` and `proc x` in one namespace: bare uses split by position, not by
// which definition was indexed first.
func TestVariableAndProcInOneNamespaceSplitBySide(t *testing.T) {
	src := "namespace eval ::n {\n  variable x 0\n  proc x {} {}\n  x\n  puts $x\n  info exists x\n}"
	r := resolverOver(src)

	variableDecl := offsetOfIn(t, src, "variable x", "x")
	procDecl := offsetOfIn(t, src, "proc x", "x")
	call := offsetOfIn(t, src, "\n  x\n", "x")
	varUseStart := strings.Index(src, "$x")
	infoExists := offsetOfIn(t, src, "info exists x", " x") + len(" ")

	if locs := r.Definition(sidesFile, src, call); len(locs) != 1 || locs[0].Kind != tcl.DefProc || locs[0].NameStart != procDecl {
		t.Errorf("Definition on the call = %#v, want only proc ::n::x at %d", locs, procDecl)
	}
	for label, cursor := range map[string]int{"$x": varUseStart + 1, "info exists x": infoExists} {
		if locs := r.Definition(sidesFile, src, cursor); len(locs) != 1 || locs[0].Kind != tcl.DefNamespaceVar || locs[0].NameStart != variableDecl {
			t.Errorf("Definition on %s = %#v, want only the variable at %d", label, locs, variableDecl)
		}
	}
	if got := startsOf(r.FileHighlights(sidesFile, src, variableDecl)); !sameOffsets(got, []int{variableDecl, varUseStart, infoExists}) {
		t.Errorf("FileHighlights from `variable x` = %v, want the variable's own sites", got)
	}
	if got := startsOf(r.FileHighlights(sidesFile, src, procDecl)); !sameOffsets(got, []int{procDecl, call}) {
		t.Errorf("FileHighlights from `proc x` = %v, want the proc's own sites", got)
	}
}

// refFQ falls back to the primary candidate when nothing matches, so a call and a
// `$x` use of an undefined (or other-side-only) name get the same FQ string. The
// side filter, not the string, must keep them apart.
func TestOneSidedNamesDoNotGroupAcrossSides(t *testing.T) {
	cases := []struct {
		label string
		src   string
		proc  bool // a proc ::n is defined
		set   bool // a variable ::n is defined
	}{
		{"undefined", "::n\nputs $::n", false, false},
		{"proc only", "proc ::n {} {}\n::n\nputs $::n", true, false},
		{"variable only", "set ::n 1\n::n\nputs $::n", false, true},
	}
	for _, c := range cases {
		r := resolverOver(c.src)
		call := strings.Index(c.src, "::n\nputs")
		varUseStart := strings.Index(c.src, "$::n")
		procDecl, setDecl := -1, -1
		if c.proc {
			procDecl = strings.Index(c.src, "proc ::n") + len("proc ")
		}
		if c.set {
			setDecl = strings.Index(c.src, "set ::n") + len("set ")
		}

		callHighlights, varHighlights := []int{call}, []int{varUseStart}
		var callDecls, varDecls []int
		if c.proc {
			callHighlights, callDecls = append(callHighlights, procDecl), []int{procDecl}
		}
		if c.set {
			varHighlights, varDecls = append(varHighlights, setDecl), []int{setDecl}
		}

		if got := startsOf(r.References(sidesFile, c.src, call)); !sameOffsets(got, []int{call}) {
			t.Errorf("%s: References from the call = %v, want only the call", c.label, got)
		}
		if got := startsOf(r.References(sidesFile, c.src, varUseStart+1)); !sameOffsets(got, []int{varUseStart}) {
			t.Errorf("%s: References from $::n = %v, want only the $ use", c.label, got)
		}
		if got := startsOf(r.FileHighlights(sidesFile, c.src, call)); !sameOffsets(got, callHighlights) {
			t.Errorf("%s: FileHighlights from the call = %v, want %v", c.label, got, callHighlights)
		}
		if got := startsOf(r.FileHighlights(sidesFile, c.src, varUseStart+1)); !sameOffsets(got, varHighlights) {
			t.Errorf("%s: FileHighlights from $::n = %v, want %v", c.label, got, varHighlights)
		}
		if got := startsOf(r.Declarations(sidesFile, c.src, call)); !sameOffsets(got, callDecls) {
			t.Errorf("%s: Declarations from the call = %v, want %v", c.label, got, callDecls)
		}
		if got := startsOf(r.Declarations(sidesFile, c.src, varUseStart+1)); !sameOffsets(got, varDecls) {
			t.Errorf("%s: Declarations from $::n = %v, want %v", c.label, got, varDecls)
		}
	}
}

// Find-references results carry the Kind of the side they were asked about, even
// when the other side's definition is indexed first.
func TestResultKindsFollowSideWhenVariableIsIndexedFirst(t *testing.T) {
	src := "set ::k 1\nproc ::k {} {}\n::k\nputs $::k"
	r := resolverOver(src)

	call := strings.LastIndex(src, "\n::k\n") + len("\n")
	varUse := strings.Index(src, "$::k") + len("$")
	cases := []struct {
		label string
		from  int
		want  tcl.DefKind
	}{
		{"the call", call, tcl.DefProc},
		{"$::k", varUse, tcl.DefNamespaceVar},
	}
	for _, c := range cases {
		for name, locs := range map[string][]index.Location{
			"References":     r.References(sidesFile, src, c.from),
			"FileHighlights": r.FileHighlights(sidesFile, src, c.from),
			"Declarations":   r.Declarations(sidesFile, src, c.from),
		} {
			if len(locs) == 0 {
				t.Errorf("%s from %s returned nothing", name, c.label)
			}
			for _, loc := range locs {
				if loc.Kind != c.want {
					t.Errorf("%s from %s returned kind %v at %d, want %v", name, c.label, loc.Kind, loc.NameStart, c.want)
				}
			}
		}
	}
}

// The command falls through a same-named namespace variable to the outer proc, so
// find-references (and highlight) on that proc must find the call. A refFQ that
// stops at the variable files the call under ::n::debug and loses it.
func TestReferencesToOuterProcSeeCallPastShadowingVariable(t *testing.T) {
	src := "proc debug {} {}\nnamespace eval ::n {\n  variable debug 1\n  debug\n}"
	r := resolverOver(src)

	procName := strings.Index(src, "debug")
	call := strings.LastIndex(src, "debug")
	if got := startsOf(r.References(sidesFile, src, procName)); !sameOffsets(got, []int{call}) {
		t.Errorf("References from proc debug = %v, want the call at %d", got, call)
	}
	if got := startsOf(r.FileHighlights(sidesFile, src, call)); !sameOffsets(got, []int{procName, call}) {
		t.Errorf("FileHighlights from the call = %v, want proc and call", got)
	}
}

// Variable lookups have a single candidate, so a proc ::n::foo is the ONLY thing
// `$foo` in ::n could have matched. It must not be returned.
func TestVariableUseInNamespaceNeverResolvesToSameNamedProc(t *testing.T) {
	src := "proc ::n::foo {} {}\nnamespace eval ::n {\n  puts $foo\n}"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.Index(src, "$foo")+len("$"))
	if hasKind(locs, tcl.DefProc) {
		t.Fatalf("Definition on $foo = %#v, must not contain the proc", locs)
	}
}

// namespace path: the first candidate holds only the variable, the second the proc.
func TestNamespacePathSkipsCandidateHoldingOnlyAVariable(t *testing.T) {
	src := "namespace eval ::vars { variable util 1 }\nnamespace eval ::lib { proc util {} {} }\n" +
		"namespace eval ::app {\n  namespace path {::vars ::lib}\n  util\n}"
	r := resolverOver(src)

	call := strings.LastIndex(src, "util")
	locs := r.Definition(sidesFile, src, call)
	if len(locs) != 1 || locs[0].Name != "::lib::util" || locs[0].Kind != tcl.DefProc {
		t.Fatalf("Definition through namespace path = %#v, want only ::lib::util", locs)
	}

	refs := source.Refs(sidesFile, src)
	for i := range refs {
		if refs[i].Ref.Kind == tcl.RefCommand && refs[i].Ref.Start == call {
			if kind, ok := r.CommandRefKind(&refs[i], sidesFile); !ok || kind != tcl.DefProc {
				t.Fatalf("CommandRefKind through namespace path = (%v, %v), want (DefProc, true)", kind, ok)
			}
			return
		}
	}
	t.Fatalf("no command reference at %d", call)
}

func TestReferencesToPathProcSeeCallPastVariableOnlyCandidate(t *testing.T) {
	src := "namespace eval ::vars { variable util 1 }\nnamespace eval ::lib { proc util {} {} }\n" +
		"namespace eval ::app {\n  namespace path {::vars ::lib}\n  util\n}"
	r := resolverOver(src)

	procName := offsetOfIn(t, src, "proc util", "util")
	call := strings.LastIndex(src, "util")
	if got := startsOf(r.References(sidesFile, src, procName)); !sameOffsets(got, []int{call}) {
		t.Fatalf("References from ::lib::util = %v, want the call at %d", got, call)
	}
}

// namespace import: the glob import of the variable-only namespace comes first.
func TestNamespaceImportSkipsCandidateHoldingOnlyAVariable(t *testing.T) {
	src := "namespace eval ::vars { variable util 1 }\nnamespace eval ::lib { proc util {} {} }\n" +
		"namespace eval ::app {\n  namespace import ::vars::* ::lib::*\n  util\n}"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.LastIndex(src, "util"))
	if len(locs) != 1 || locs[0].Name != "::lib::util" || locs[0].Kind != tcl.DefProc {
		t.Fatalf("Definition through namespace import = %#v, want only ::lib::util", locs)
	}
}

// `upvar #0 NAME alias` chases to ::NAME, which a proc also holds.
func TestUpvarGlobalChaseIgnoresSameNamedProc(t *testing.T) {
	src := "proc test {} {}\nset test 1\nproc g {} {\n  upvar #0 test alias\n  puts $alias\n}"
	r := resolverOver(src)

	want := strings.Index(src, "set test") + len("set ")
	locs := r.Definition(sidesFile, src, strings.Index(src, "$alias")+len("$"))
	if len(locs) != 1 || locs[0].NameStart != want || locs[0].Kind != tcl.DefNamespaceVar {
		t.Fatalf("Definition through upvar #0 = %#v, want only `set test` at %d", locs, want)
	}
}

// Same collision as the global-link test, but only the proc exists: the chase must
// not land on it. (The local link site is the fallback, as for any undefined global.)
func TestGlobalLinkToNameHeldOnlyByProcNeverJumpsToProc(t *testing.T) {
	src := "proc test {} {}\nproc f {} {\n  global test\n  puts $test\n}"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.Index(src, "$test")+len("$"))
	if hasKind(locs, tcl.DefProc) {
		t.Fatalf("Definition through the global link = %#v, must not contain the proc", locs)
	}
}

// `variable cnt` inside a proc links to ::n::cnt, which `proc cnt` also holds.
func TestVariableLinkInProcChaseIgnoresSameNamedProc(t *testing.T) {
	src := "namespace eval ::n {\n  variable cnt 0\n  proc cnt {} {}\n  proc f {} {\n    variable cnt\n    puts $cnt\n  }\n}"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.Index(src, "$cnt")+len("$"))
	if len(locs) == 0 || hasKind(locs, tcl.DefProc) {
		t.Fatalf("Definition through `variable cnt` = %#v, want variable sites only", locs)
	}
}

// Array element uses take the first-binding fallback, a second chase site.
func TestArrayElementUseThroughGlobalLinkIgnoresSameNamedProc(t *testing.T) {
	src := "proc a {} {}\nset a(1) x\nproc f {} {\n  global a\n  puts $a(1)\n}"
	r := resolverOver(src)

	want := strings.Index(src, "set a(1)") + len("set ")
	locs := r.Definition(sidesFile, src, strings.Index(src, "$a(1)")+len("$"))
	if len(locs) != 1 || locs[0].NameStart != want || locs[0].Kind != tcl.DefNamespaceVar {
		t.Fatalf("Definition on $a(1) through the global link = %#v, want only `set a(1)` at %d", locs, want)
	}
}

func TestArrayElementAtTopLevelIsNotACallOfSameNamedProc(t *testing.T) {
	src := "proc a {} {}\nset a(1) x\nputs $a(1)\na"
	r := resolverOver(src)

	procName := offsetOfIn(t, src, "proc a", "a")
	setName := offsetOfIn(t, src, "set a(1)", "a")
	elementUse := strings.Index(src, "$a(1)")
	call := strings.LastIndex(src, "\na") + len("\n")

	if locs := r.Definition(sidesFile, src, elementUse+len("$")); len(locs) != 1 || locs[0].Kind != tcl.DefNamespaceVar {
		t.Errorf("Definition on $a(1) = %#v, want only the array variable", locs)
	}
	if got := startsOf(r.References(sidesFile, src, procName)); !sameOffsets(got, []int{call}) {
		t.Errorf("References from proc a = %v, want only the call at %d", got, call)
	}
	if got := startsOf(r.References(sidesFile, src, setName)); !sameOffsets(got, []int{elementUse}) {
		t.Errorf("References from `set a(1)` = %v, want only $a(1) at %d", got, elementUse)
	}
}

// The index order must not decide which side wins, and a re-index must not let a
// stale or reordered entry leak across sides.
func TestCrossFileCollisionIsIndependentOfIndexOrder(t *testing.T) {
	procFile := namedSource{"proc.tcl", "proc ::shared {} {}"}
	varFile := namedSource{"var.tcl", "set ::shared 1"}
	useSrc := "::shared\nputs $::shared"
	useFile := namedSource{"use.tcl", useSrc}
	call, varUse := 0, strings.Index(useSrc, "$::shared")
	procName, setName := len("proc "), len("set ")

	orders := map[string][]namedSource{
		"proc,var,use": {procFile, varFile, useFile},
		"var,proc,use": {varFile, procFile, useFile},
		"use,var,proc": {useFile, varFile, procFile},
		"var,use,proc": {varFile, useFile, procFile},
	}
	for label, files := range orders {
		r := resolverOverFiles(files...)
		if got := sitesOf(r.Definition("use.tcl", useSrc, call)); !sameSites(got, siteList("proc.tcl", procName)) {
			t.Errorf("%s: Definition on the call = %v, want only proc.tcl", label, got)
		}
		if got := sitesOf(r.Definition("use.tcl", useSrc, varUse+1)); !sameSites(got, siteList("var.tcl", setName)) {
			t.Errorf("%s: Definition on $::shared = %v, want only var.tcl", label, got)
		}
		if got := sitesOf(r.References("proc.tcl", procFile.text, procName)); !sameSites(got, siteList("use.tcl", call)) {
			t.Errorf("%s: References from the proc = %v, want only the call", label, got)
		}
		if got := sitesOf(r.References("var.tcl", varFile.text, setName)); !sameSites(got, siteList("use.tcl", varUse)) {
			t.Errorf("%s: References from the variable = %v, want only $::shared", label, got)
		}
	}
}

// Removing or re-indexing one side's file leaves exactly the other side. When the
// variable's file goes, `$::shared` must go silent, not fall onto the proc.
func TestRemovingOneSidesFileSilencesThatSideOnly(t *testing.T) {
	useSrc := "::shared\nputs $::shared"
	ix := index.New()
	ix.IndexFile("var.tcl", "set ::shared 1")
	ix.IndexFile("proc.tcl", "proc ::shared {} {}")
	ix.IndexFile("use.tcl", useSrc)
	r := New(ix)
	call, varUse := 0, strings.Index(useSrc, "$::shared")+1

	ix.RemoveFile("var.tcl")
	if got := sitesOf(r.Definition("use.tcl", useSrc, call)); !sameSites(got, siteList("proc.tcl", len("proc "))) {
		t.Errorf("after removing var.tcl: call = %v, want the proc", got)
	}
	if locs := r.Definition("use.tcl", useSrc, varUse); len(locs) != 0 {
		t.Errorf("after removing var.tcl: $::shared = %#v, want nil (not the proc)", locs)
	}

	ix.IndexFile("var.tcl", "set ::shared 1")
	ix.RemoveFile("proc.tcl")
	if locs := r.Definition("use.tcl", useSrc, call); len(locs) != 0 {
		t.Errorf("after removing proc.tcl: call = %#v, want nil (not the variable)", locs)
	}
	if got := sitesOf(r.Definition("use.tcl", useSrc, varUse)); !sameSites(got, siteList("var.tcl", len("set "))) {
		t.Errorf("after removing proc.tcl: $::shared = %v, want the variable", got)
	}

	ix.IndexFile("proc.tcl", "proc ::shared {} {}")
	ix.IndexFile("var.tcl", "# no longer defines it")
	if locs := r.Definition("use.tcl", useSrc, varUse); len(locs) != 0 {
		t.Errorf("after re-indexing var.tcl without the variable: $::shared = %#v, want nil", locs)
	}
}

// A class and a variable may share a fully-qualified name; the class is on the
// command side, so only the `$` use and `set` belong to the variable.
func TestClassAndVariableOfSameNameStaySeparate(t *testing.T) {
	src := "itcl::class ::Foo {}\nset ::Foo 1\nset o [::Foo #auto]\nputs $::Foo"
	r := resolverOver(src)

	className := offsetOfIn(t, src, "itcl::class ::Foo", "::Foo")
	setName := offsetOfIn(t, src, "set ::Foo", "::Foo")
	head := strings.Index(src, "[::Foo") + len("[")
	varUse := strings.Index(src, "$::Foo")

	if locs := r.Definition(sidesFile, src, head); len(locs) != 1 || locs[0].Kind != tcl.DefClass {
		t.Errorf("Definition on the class head = %#v, want only the class", locs)
	}
	if locs := r.Definition(sidesFile, src, varUse+1); len(locs) != 1 || locs[0].Kind != tcl.DefNamespaceVar {
		t.Errorf("Definition on $::Foo = %#v, want only the variable", locs)
	}
	if got := startsOf(r.References(sidesFile, src, className)); !sameOffsets(got, []int{head}) {
		t.Errorf("References from the class = %v, want only the head at %d", got, head)
	}
	if got := startsOf(r.FileHighlights(sidesFile, src, className)); !sameOffsets(got, []int{className, head}) {
		t.Errorf("FileHighlights from the class = %v, want class and head", got)
	}
	if got := startsOf(r.FileHighlights(sidesFile, src, setName)); !sameOffsets(got, []int{setName, varUse}) {
		t.Errorf("FileHighlights from `set ::Foo` = %v, want variable and $ use", got)
	}
	if locs := r.Declarations(sidesFile, src, head); len(locs) != 1 || locs[0].Kind != tcl.DefClass {
		t.Errorf("Declarations from the class head = %#v, want only the class", locs)
	}
}

// With the variable indexed first, the call's kind is still the class.
func TestCommandRefKindOfClassIgnoresSameNamedVariable(t *testing.T) {
	src := "set ::Foo 1\nitcl::class ::Foo {}\n::Foo #auto"
	r := resolverOver(src)

	head := strings.LastIndex(src, "::Foo")
	refs := source.Refs(sidesFile, src)
	for i := range refs {
		if refs[i].Ref.Kind == tcl.RefCommand && refs[i].Ref.Start == head {
			if kind, ok := r.CommandRefKind(&refs[i], sidesFile); !ok || kind != tcl.DefClass {
				t.Fatalf("CommandRefKind = (%v, %v), want (DefClass, true)", kind, ok)
			}
			return
		}
	}
	t.Fatalf("no command reference at %d", head)
}

// Guard: a class and a proc are both command-side, so the filter must keep both.
func TestClassAndProcOfSameNameAreBothCommandSide(t *testing.T) {
	src := "itcl::class ::Foo {}\nproc ::Foo {} {}\n::Foo"
	r := resolverOver(src)

	locs := r.Definition(sidesFile, src, strings.LastIndex(src, "::Foo"))
	if !hasKind(locs, tcl.DefClass) || !hasKind(locs, tcl.DefProc) {
		t.Fatalf("Definition on the call has kinds %v, want both DefClass and DefProc", kindsOf(locs))
	}
}

// Page-local names: two pages each define the proc and the variable. Each page
// sees only its own, on the right side.
func TestRVTPageLocalSidesResolveWithinTheirOwnPage(t *testing.T) {
	page := "<? proc item {} {} ?>\n<? set item 1 ?>\n<? item ?>\n<? puts $item ?>"
	r := resolverOverFiles(namedSource{"a.rvt", page}, namedSource{"b.rvt", page})

	procName := offsetOfIn(t, page, "proc item", "item")
	setName := offsetOfIn(t, page, "set item", "item")
	call := offsetOfIn(t, page, "<? item ?>", "item")
	varUseStart := strings.Index(page, "$item")

	for _, file := range []string{"a.rvt", "b.rvt"} {
		if got := sitesOf(r.Definition(file, page, call)); !sameSites(got, siteList(file, procName)) {
			t.Errorf("%s: Definition on the call = %v, want only this page's proc", file, got)
		}
		if got := sitesOf(r.Definition(file, page, varUseStart+1)); !sameSites(got, siteList(file, setName)) {
			t.Errorf("%s: Definition on $item = %v, want only this page's variable", file, got)
		}
		if got := sitesOf(r.References(file, page, call)); !sameSites(got, siteList(file, call)) {
			t.Errorf("%s: References from the call = %v, want only this page's call", file, got)
		}
		if got := sitesOf(r.References(file, page, varUseStart+1)); !sameSites(got, siteList(file, varUseStart)) {
			t.Errorf("%s: References from $item = %v, want only this page's $ use", file, got)
		}
		if got := startsOf(r.FileHighlights(file, page, call)); !sameOffsets(got, []int{procName, call}) {
			t.Errorf("%s: FileHighlights from the call = %v, want proc and call", file, got)
		}
	}
}

// Page A has only the proc, page B only the variable. Neither page may borrow the
// other's definition, and A's `$item` may not fall onto A's own proc.
func TestRVTPageLocalOtherSideAndOtherPageStaySilent(t *testing.T) {
	pageA := "<? proc item {} {} ?>\n<? item ?>\n<? puts $item ?>"
	pageB := "<? set item 1 ?>\n<? item ?>\n<? puts $item ?>"
	r := resolverOverFiles(namedSource{"a.rvt", pageA}, namedSource{"b.rvt", pageB})

	if locs := r.Definition("a.rvt", pageA, strings.Index(pageA, "$item")+1); len(locs) != 0 {
		t.Errorf("a.rvt $item = %#v, want nil (a proc is not a variable, b's variable is another page)", locs)
	}
	if locs := r.Definition("b.rvt", pageB, offsetOfIn(t, pageB, "<? item ?>", "item")); len(locs) != 0 {
		t.Errorf("b.rvt call = %#v, want nil (b's variable is not a command, a's proc is another page)", locs)
	}
	callA := offsetOfIn(t, pageA, "<? item ?>", "item")
	if got := sitesOf(r.Definition("a.rvt", pageA, callA)); !sameSites(got, siteList("a.rvt", offsetOfIn(t, pageA, "proc item", "item"))) {
		t.Errorf("a.rvt call = %v, want a's proc", got)
	}
	// Find-references from a's call must neither cross pages nor sides.
	if got := sitesOf(r.References("a.rvt", pageA, callA)); !sameSites(got, siteList("a.rvt", callA)) {
		t.Errorf("a.rvt References from the call = %v, want only a's call", got)
	}
}
