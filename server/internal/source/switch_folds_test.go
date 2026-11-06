package source

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

func hasFold(folds []tcl.FoldRange, open, close int) bool {
	for _, fold := range folds {
		if fold.Open == open && fold.Close == close {
			return true
		}
	}
	return false
}

// In a template whose switch arms span Rivet tags, the arm list and
// the first arm still fold, on real braces in the template's own coordinates.
func TestFolds_SwitchArmsSpanRVTTags(t *testing.T) {
	src := "<?\nproc helper {} {}\nswitch $x {\n  a { ?>\n<li>one</li>\n<? helper\n  }\n  b { ?>\n<li>two</li>\n<? puts 2\n  }\n}\n?>\n"
	folds := Folds("p.rvt", src)

	for _, fold := range folds {
		if src[fold.Open] != '{' || src[fold.Close] != '}' {
			t.Errorf("fold %+v is not on braces: src[Open]=%q src[Close]=%q", fold, src[fold.Open], src[fold.Close])
		}
	}

	armListOpen := strings.Index(src, "switch $x {") + len("switch $x ")
	armListClose := strings.LastIndex(src, "}")
	if !hasFold(folds, armListOpen, armListClose) {
		t.Errorf("arm list not folded at [%d,%d]; got %+v", armListOpen, armListClose, folds)
	}

	firstArmOpen := strings.Index(src, "a {") + len("a ")
	firstArmClose := strings.Index(src, "  }\n  b") + len("  ")
	if !hasFold(folds, firstArmOpen, firstArmClose) {
		t.Errorf("first arm not folded at [%d,%d]; got %+v", firstArmOpen, firstArmClose, folds)
	}
}

// The inline form across tags has no arm list, so only the two bodies fold. Hunting:
// an arm-list fold invented from the last body, and a body whose brace comes back
// in stitched-script coordinates instead of template coordinates.
func TestFolds_SwitchInlineArmsSpanRVTTags(t *testing.T) {
	src := "<?\nswitch $x a { ?>\n<li>one</li>\n<? } b { ?>\n<li>two</li>\n<? }\n?>\n"
	folds := Folds("p.rvt", src)

	for _, fold := range folds {
		if src[fold.Open] != '{' || src[fold.Close] != '}' {
			t.Errorf("fold %+v is not on braces: src[Open]=%q src[Close]=%q", fold, src[fold.Open], src[fold.Close])
		}
	}
	firstOpen := strings.Index(src, "a {") + len("a ")
	firstClose := strings.Index(src, "<? } b") + len("<? ")
	secondOpen := strings.Index(src, "b {") + len("b ")
	secondClose := strings.LastIndex(src, "}")
	if !hasFold(folds, firstOpen, firstClose) || !hasFold(folds, secondOpen, secondClose) {
		t.Errorf("want folds [%d,%d] and [%d,%d]; got %+v", firstOpen, firstClose, secondOpen, secondClose, folds)
	}
	if len(folds) != 2 {
		t.Errorf("folds = %d, want exactly the 2 bodies: %+v", len(folds), folds)
	}
}

// A braced pattern and a fall-through `-` in a list that spans tags: the arm list and
// the one real body fold, the braced pattern does not.
func TestFolds_SwitchBracedPatternSpanRVTTags(t *testing.T) {
	src := "<?\nswitch $x {\n  {a b} -\n  c { ?>\n<li>one</li>\n<? puts 1\n  }\n}\n?>\n"
	folds := Folds("p.rvt", src)

	for _, fold := range folds {
		if src[fold.Open] != '{' || src[fold.Close] != '}' {
			t.Errorf("fold %+v is not on braces: src[Open]=%q src[Close]=%q", fold, src[fold.Open], src[fold.Close])
		}
	}
	armListOpen := strings.Index(src, "switch $x {") + len("switch $x ")
	armListClose := strings.LastIndex(src, "}")
	bodyOpen := strings.Index(src, "c {") + len("c ")
	bodyClose := strings.Index(src, "  }\n}") + len("  ")
	if !hasFold(folds, armListOpen, armListClose) || !hasFold(folds, bodyOpen, bodyClose) {
		t.Errorf("want folds [%d,%d] and [%d,%d]; got %+v", armListOpen, armListClose, bodyOpen, bodyClose, folds)
	}
	if len(folds) != 2 {
		t.Errorf("folds = %d, want exactly the arm list and one body: %+v", len(folds), folds)
	}
}
