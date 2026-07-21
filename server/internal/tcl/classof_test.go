package tcl

import (
	"strings"
	"testing"
)

func TestClassOfLocalInstantiation(t *testing.T) {
	src := "proc f {} {\n  set d [::STDisplay #auto]\n  $d field isbn\n}"
	off := strings.LastIndex(src, "$d") + 1 // the receiver use in `$d field`
	got := ClassOf(src, off)
	if len(got) != 1 || got[0] != "::STDisplay" {
		t.Fatalf("ClassOf = %#v, want [::STDisplay]", got)
	}
}

func TestClassOfUnknownIsNil(t *testing.T) {
	// receiver is a parameter -> no local instantiation -> no type
	src := "proc f {obj} {\n  $obj field\n}"
	off := strings.LastIndex(src, "$obj") + 1
	if got := ClassOf(src, off); got != nil {
		t.Fatalf("ClassOf on a param should be nil, got %#v", got)
	}
}

func TestClassOfConditionalInstantiation(t *testing.T) {
	src := "proc f {} {\n  if {$c} {\n    set d [::STDisplay #auto]\n  }\n  $d field\n}"
	off := strings.LastIndex(src, "$d") + 1
	got := ClassOf(src, off)
	if len(got) != 1 || got[0] != "::STDisplay" {
		t.Fatalf("conditional instantiation: ClassOf = %#v, want [::STDisplay]", got)
	}
}

func TestClassOfUnionAcrossBranches(t *testing.T) {
	src := "proc f {} {\n  if {$c} {\n    set d [::A #auto]\n  } else {\n    set d [::B #auto]\n  }\n  $d field\n}"
	off := strings.LastIndex(src, "$d") + 1
	got := ClassOf(src, off)
	// may-reach: both branch classes, deduped, order-independent
	set := map[string]bool{}
	for _, c := range got {
		set[c] = true
	}
	if len(got) != 2 || !set["::A"] || !set["::B"] {
		t.Fatalf("union across branches: ClassOf = %#v, want {::A, ::B}", got)
	}
}
