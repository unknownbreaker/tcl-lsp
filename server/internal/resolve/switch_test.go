package resolve

import (
	"sort"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// A switch pattern that shares a proc's name is not a call to that proc: it does
// not jump to the proc, and find-references does not list it. A real call in the
// arm's body still does both, so the exclusion cannot pass vacuously.
func TestSwitchPatternIsNotAReference(t *testing.T) {
	const file = "main.tcl"
	src := "proc stop {} {}\nswitch $x {\n  stop { stop }\n}\n"
	ix := index.New()
	ix.IndexFile(file, src)
	r := New(ix)

	patternOffset := strings.Index(src, "  stop {") + len("  ")
	callOffset := strings.Index(src, "{ stop }") + len("{ ")

	if locs := r.Definition(file, src, patternOffset); len(locs) != 0 {
		t.Errorf("Definition on the pattern = %#v, want nil", locs)
	}

	locs := r.References(file, src, strings.Index(src, "stop"))
	for _, loc := range locs {
		if loc.NameStart == patternOffset {
			t.Errorf("References of ::stop includes the pattern: %#v", locs)
		}
	}
	if len(locs) != 1 || locs[0].NameStart != callOffset {
		t.Errorf("References of ::stop = %#v, want only the call at %d", locs, callOffset)
	}
}

// A variable inside an arm whose pattern is quoted is still a use of the param.
func TestSwitchQuotedArmVariableResolvesToParam(t *testing.T) {
	const file = "main.tcl"
	src := "proc f {y} {\n  switch $k {\n    \"a\" { puts $y }\n  }\n}"
	r := New(index.New())

	locs := r.Definition(file, src, strings.Index(src, "$y")+len("$"))
	paramOffset := strings.Index(src, "{y}") + len("{")
	if len(locs) != 1 || locs[0].NameStart != paramOffset {
		t.Fatalf("Definition of $y = %#v, want the param at %d", locs, paramOffset)
	}
}

// expandMarkers replaces each marker in template with its text and returns the
// result plus the byte offset every marker landed at, in source order.
func expandMarkers(template string, replacements map[string]string) (string, map[string][]int) {
	var out strings.Builder
	offsets := map[string][]int{}
	for position := 0; position < len(template); {
		matched := false
		for marker, text := range replacements {
			if strings.HasPrefix(template[position:], marker) {
				offsets[marker] = append(offsets[marker], out.Len())
				out.WriteString(text)
				position += len(marker)
				matched = true
				break
			}
		}
		if !matched {
			out.WriteByte(template[position])
			position++
		}
	}
	return out.String(), offsets
}

func startsOf(locs []index.Location) []int {
	starts := make([]int, 0, len(locs))
	for _, loc := range locs {
		starts = append(starts, loc.NameStart)
	}
	sort.Ints(starts)
	return starts
}

func sameOffsets(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	sorted := append([]int(nil), right...)
	sort.Ints(sorted)
	for i := range left {
		if left[i] != sorted[i] {
			return false
		}
	}
	return true
}

// Review gap: the pattern is spelled like a defined proc in EVERY form (@P marks the
// pattern, @C a real call in the body). Hunting: a form whose pattern still jumps to
// the proc or is listed by find-references, from either side; a form whose body is
// not walked, so the call is missed.
func TestSwitchPatternIsNotAReferenceInEveryForm(t *testing.T) {
	const file = "main.tcl"
	const procLine = "proc stop {} {}\n"
	templates := map[string]string{
		"bare":                      "switch $x {\n  @P { @C }\n}\n",
		"quoted":                    "switch $x {\n  \"@P\" { @C }\n}\n",
		"braced":                    "switch $x {\n  {@P} { @C }\n}\n",
		"glob":                      "switch -glob $x {\n  @P { @C }\n}\n",
		"same line":                 "switch $x { @P { @C } @P { @C } }\n",
		"inline":                    "switch $x @P { @C } @P { @C }\n",
		"fall-through":              "switch $x {\n  @P -\n  @P { @C }\n}\n",
		"bracket":                   "set r [switch $x { @P { @C } }]\n",
		"pattern still being typed": "switch $x {\n  a { @C }\n  @P\n}\n",
		"in a proc":                 "proc g {} {\n  switch $x {\n    @P { @C }\n  }\n}\n",
		"in an itcl method":         "itcl::class K {\n  method m {} {\n    switch $x {\n      @P { @C }\n    }\n  }\n}\n",
	}
	for name, template := range templates {
		t.Run(name, func(t *testing.T) {
			body, offsets := expandMarkers(template, map[string]string{"@P": "stop", "@C": "stop"})
			src := procLine + body
			shift := len(procLine)
			patterns, calls := offsets["@P"], offsets["@C"]
			for i := range patterns {
				patterns[i] += shift
			}
			for i := range calls {
				calls[i] += shift
			}
			ix := index.New()
			ix.IndexFile(file, src)
			r := New(ix)
			procNameOffset := strings.Index(src, "stop")

			for _, pattern := range patterns {
				if locs := r.Definition(file, src, pattern); len(locs) != 0 {
					t.Errorf("Definition on pattern at %d = %#v, want nil", pattern, locs)
				}
				if locs := r.References(file, src, pattern); len(locs) != 0 {
					t.Errorf("References from pattern at %d = %#v, want none", pattern, locs)
				}
			}
			for _, call := range calls {
				locs := r.Definition(file, src, call)
				if len(locs) != 1 || locs[0].NameStart != procNameOffset {
					t.Errorf("Definition on the call at %d = %#v, want the proc at %d", call, locs, procNameOffset)
				}
				if got := startsOf(r.References(file, src, call)); !sameOffsets(got, calls) {
					t.Errorf("References from the call at %d = %v, want exactly the calls %v", call, got, calls)
				}
			}
			if got := startsOf(r.References(file, src, procNameOffset)); !sameOffsets(got, calls) {
				t.Errorf("References from the proc = %v, want exactly the calls %v (patterns %v)", got, calls, patterns)
			}
		})
	}
}

// The index keeps precomputed refs for files other than the one asked about, so the
// find-references scan of the workspace reads a different copy than the live file.
// Hunting: a pattern listed as a reference only when it comes from the index.
func TestSwitchPatternIsNotAReferenceFromAnotherFile(t *testing.T) {
	const library = "lib.tcl"
	const rvtPage = "page.rvt"
	const tclPage = "page.tcl"
	librarySrc := "proc stop {} {}\n"
	pages := map[string]string{
		tclPage: "switch $x {\n  stop { stop }\n  \"stop\" { stop }\n}\nswitch $x stop { stop }\n",
		rvtPage: "<?\nswitch $x {\n  stop { ?>\n<li>one</li>\n<? stop\n  }\n}\n?>\n",
	}
	for page, pageSrc := range pages {
		t.Run(page, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(library, librarySrc)
			ix.IndexFile(page, pageSrc)
			r := New(ix)

			var wantCalls []int
			for from := 0; ; {
				found := strings.Index(pageSrc[from:], "{ stop")
				if found < 0 {
					break
				}
				wantCalls = append(wantCalls, from+found+len("{ "))
				from += found + 1
			}
			if page == rvtPage {
				wantCalls = []int{strings.Index(pageSrc, "<? stop") + len("<? ")}
			}

			locs := r.References(library, librarySrc, strings.Index(librarySrc, "stop"))
			var got []int
			for _, loc := range locs {
				if loc.File == page {
					got = append(got, loc.NameStart)
				}
			}
			sort.Ints(got)
			if !sameOffsets(got, wantCalls) {
				t.Errorf("references in %s = %v, want exactly the body calls %v; all = %#v", page, got, wantCalls, locs)
			}
		})
	}
}

// Review gap: a local used in an arm body resolves to its binding, and find-references
// on either end lists both, in every scope a switch can sit in. @B marks the binding
// name and @U the use. Hunting: arm bodies walked with the wrong scope, so the use
// resolves to nothing or to another proc's variable.
func TestSwitchLocalsResolveInArmsAcrossScopes(t *testing.T) {
	const file = "main.tcl"
	templates := map[string]string{
		"quoted pattern":        "proc f {@B} {\n  switch $k {\n    \"a\" { puts $@U }\n  }\n}",
		"braced pattern":        "proc f {@B} {\n  switch $k {\n    {a b} { puts $@U }\n  }\n}",
		"inline":                "proc f {@B} {\n  switch $k \"a\" { puts $@U }\n}",
		"fall-through":          "proc f {@B} {\n  switch $k {\n    \"a\" -\n    \"b\" { puts $@U }\n  }\n}",
		"set before the switch": "proc f {} {\n  set @B 1\n  switch $k {\n    \"a\" { puts $@U }\n  }\n}",
		"nested switch":         "proc f {@B} {\n  switch $k {\n    \"a\" {\n      switch $j {\n        \"b\" { puts $@U }\n      }\n    }\n  }\n}",
		"in a namespace":        "namespace eval ns {\n  proc f {@B} {\n    switch $k {\n      {a b} { puts $@U }\n    }\n  }\n}",
		"in an itcl method":     "itcl::class C {\n  method m {} {\n    set @B 1\n    switch $k {\n      \"a\" { puts $@U }\n    }\n  }\n}",
		"after multi-byte text": "proc f {@B} {\n  # 世界 😀\n  switch $k {\n    \"é\" { puts $@U }\n  }\n}",
		"two uses in two arms":  "proc f {@B} {\n  switch $k {\n    \"a\" { puts $@U }\n    \"b\" { puts $@U }\n  }\n}",
	}
	for name, template := range templates {
		t.Run(name, func(t *testing.T) {
			src, offsets := expandMarkers(template, map[string]string{"@B": "y", "@U": "y"})
			binding, uses := offsets["@B"][0], offsets["@U"]
			r := New(index.New())

			for _, use := range uses {
				locs := r.Definition(file, src, use)
				if len(locs) != 1 || locs[0].NameStart != binding {
					t.Errorf("Definition of the use at %d = %#v, want the binding at %d", use, locs, binding)
				}
			}
			// A use's reference range starts at its `$`, one byte before the name.
			want := []int{binding}
			for _, use := range uses {
				want = append(want, use-1)
			}
			for _, from := range append([]int{binding}, uses...) {
				if got := startsOf(r.References(file, src, from)); !sameOffsets(got, want) {
					t.Errorf("References from %d = %v, want the binding and every use %v", from, got, want)
				}
			}
		})
	}
}

// A `$y` in a braced arm list is literal text; in an inline pattern it is a real use.
// Hunting: the literal one resolving to a binding (goto-def from text that is not a
// variable) and the inline one failing to.
func TestSwitchDollarPatternResolvesOnlyWhenSubstituted(t *testing.T) {
	const file = "main.tcl"
	r := New(index.New())

	literal, literalOffsets := expandMarkers("proc f {y} {\n  switch $k {\n    $@P { puts 1 }\n  }\n}", map[string]string{"@P": "y"})
	if locs := r.Definition(file, literal, literalOffsets["@P"][0]); len(locs) != 0 {
		t.Errorf("Definition on a literal $y pattern = %#v, want nil", locs)
	}

	inline, inlineOffsets := expandMarkers("proc f {y} {\n  switch $k $@P { puts 1 }\n}", map[string]string{"@P": "y"})
	locs := r.Definition(file, inline, inlineOffsets["@P"][0])
	if want := strings.Index(inline, "{y}") + 1; len(locs) != 1 || locs[0].NameStart != want {
		t.Errorf("Definition on an inline $y pattern = %#v, want the param at %d", locs, want)
	}
}

// Reaching definitions through the resolver: a use after the switch jumps to the
// binding in EVERY arm (the may-reach answer), whatever shape the arms have.
func TestSwitchArmBindingsResolveFromAUseAfterTheSwitch(t *testing.T) {
	const file = "main.tcl"
	cases := map[string]string{
		"quoted, braced and default": "proc f {} {\n  set x 0\n  switch $k {\n    \"a\" { set x 1 }\n    {b c} { set x 2 }\n    default { set x 3 }\n  }\n  puts $x\n}",
		"inline with fall-through":   "proc f {} {\n  set x 0\n  switch -glob -- $k a - b { set x 1 } default { set x 2 }\n  puts $x\n}",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			r := New(index.New())
			locs := r.Definition(file, src, strings.LastIndex(src, "$x")+1)
			got := map[int]bool{}
			for _, loc := range locs {
				got[loc.NameStart] = true
			}
			for from := 0; ; {
				found := strings.Index(src[from:], "set x ")
				if found < 0 {
					break
				}
				site := from + found
				if site != strings.Index(src, "set x 0") && !got[site+len("set ")] {
					t.Errorf("binding at %d does not reach the use; got %#v", site+len("set "), locs)
				}
				from = site + 1
			}
		})
	}
}

// Review Focus 5. Shape from rvt/testdata/corpus/control_flow_spanning.rvt: the
// arms of a switch open and close across Rivet tags.
func TestSwitchArmsSpanRVTTags(t *testing.T) {
	const file = "p.rvt"
	src := "<?\nproc helper {} {}\nswitch $x {\n  a { ?>\n<li>one</li>\n<? helper\n  }\n  b { ?>\n<li>two</li>\n<? puts 2\n  }\n}\n?>\n"
	ix := index.New()
	ix.IndexFile(file, src)
	r := New(ix)

	helperOffset := strings.Index(src, "<? helper") + len("<? ")
	if locs := r.Definition(file, src, helperOffset); len(locs) != 1 {
		t.Errorf("Definition of helper in the arm = %#v, want 1 location", locs)
	}

	patternOffset := strings.Index(src, "  a {") + len("  ")
	if locs := r.Definition(file, src, patternOffset); len(locs) != 0 {
		t.Errorf("Definition on pattern `a` = %#v, want nil", locs)
	}
}

// Flagged gap: the test above passes without the fix, because there is no `proc a`
// for the pattern to jump to. Here the patterns are the names of defined procs, in
// the list form and the inline form, with the arms open and closed across tags and
// a `<?=` expression inside an arm. Hunting: a pattern that jumps to or is listed as
// a call, a body that is lost across the tags, and a local that stops resolving.
func TestSwitchArmsSpanRVTTagsWithPatternsNamedLikeProcs(t *testing.T) {
	const file = "p.rvt"
	cases := map[string]string{
		"list":   "<?\nproc a {} {}\nproc b {} {}\nproc helper {} {}\nproc f {y} {\n  switch $x {\n    a { ?>\n<li>one</li>\n<? helper\n    }\n    b { ?>\n<li><?= $y ?></li>\n<? helper\n    }\n  }\n}\n?>\n",
		"inline": "<?\nproc a {} {}\nproc b {} {}\nproc helper {} {}\nproc f {y} {\n  switch $x a { ?>\n<li>one</li>\n<? helper\n  } b { ?>\n<li><?= $y ?></li>\n<? helper\n  }\n}\n?>\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(file, src)
			r := New(ix)

			procA := strings.Index(src, "proc a") + len("proc ")
			procB := strings.Index(src, "proc b") + len("proc ")
			patternA := strings.Index(src, "switch $x") + len("switch $x ")
			if name == "list" {
				patternA = strings.Index(src, "    a {") + len("    ")
			}
			patternB := strings.Index(src, "b { ?>")

			for label, offset := range map[string]int{"a": patternA, "b": patternB} {
				if locs := r.Definition(file, src, offset); len(locs) != 0 {
					t.Errorf("Definition on pattern %s = %#v, want nil", label, locs)
				}
			}
			for label, procName := range map[string]int{"a": procA, "b": procB} {
				if locs := r.References(file, src, procName); len(locs) != 0 {
					t.Errorf("References of proc %s = %#v, want none (it is only ever a pattern)", label, locs)
				}
			}

			helperProc := strings.Index(src, "proc helper") + len("proc ")
			for from := 0; ; {
				found := strings.Index(src[from:], "<? helper")
				if found < 0 {
					break
				}
				call := from + found + len("<? ")
				if locs := r.Definition(file, src, call); len(locs) != 1 || locs[0].NameStart != helperProc {
					t.Errorf("Definition of helper at %d = %#v, want the proc at %d", call, locs, helperProc)
				}
				from = call
			}

			param := strings.Index(src, "{y}") + 1
			use := strings.Index(src, "<?= $y") + len("<?= $")
			if locs := r.Definition(file, src, use); len(locs) != 1 || locs[0].NameStart != param {
				t.Errorf("Definition of $y inside an arm = %#v, want the param at %d", locs, param)
			}
		})
	}
}
