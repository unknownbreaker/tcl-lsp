package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Hunting: a method parameter shadows the instance variable of the same name. Method
// parameters are not modeled as bindings (only `proc` parameters are), so today a
// later `set n` is returned for the use, and once the guard silences that the ivar
// would be returned instead: also wrong. Tcl: the use is the parameter. NOT in the
// brief. Accepted answers: the parameter's line, or silence. Never the ivar's line
// or a line below the use.
func TestDefinitionMethodParamShadowsIvar(t *testing.T) {
	cases := []struct{ name, src string }{
		{"later set in the method", "itcl::class C {\n  protected variable n 0\n  method m {n} {\n    puts $n\n    set n 1\n  }\n}"},
		{"no other binding", "itcl::class C {\n  protected variable n 0\n  method m {n} {\n    puts $n\n  }\n}"},
	}
	const paramLine = 3
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defLinesAt(t, c.src, "$n")
			if len(got) != 0 && !slices.Equal(got, []int{paramLine}) {
				t.Errorf("definition lines of $n = %v, want the parameter on line %d or nothing\nsource:\n%s", got, paramLine, c.src)
			}
		})
	}
}

func TestDefinitionMethodParametersAreBindings(t *testing.T) {
	const ivar = "itcl::class C {\n  protected variable n 0\n"
	runDefinitionCases(t, []definitionCase{
		{"method", ivar + "  method m {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"method, default value", ivar + "  method m {{n 5}} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"method, bare args word", ivar + "  method m args {\n    puts $args\n  }\n}", "$args", []int{3}},
		{"public method", ivar + "  public method m {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"protected method", ivar + "  protected method m {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"private method", ivar + "  private method m {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"method, parameter then later set", ivar + "  method m {n} {\n    puts $n\n    set n 1\n  }\n}", "$n", []int{3}},
		{"method, set before the use wins", ivar + "  method m {n} {\n    set n 1\n    puts $n\n  }\n}", "$n", []int{4}},
		{"method, another name is still the ivar", ivar + "  method m {k} {\n    puts $n\n  }\n}", "$n", []int{2}},
		{"class proc", ivar + "  public proc p {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"constructor", ivar + "  constructor {n} {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"constructor, body after an init block", ivar + "  constructor {n} {\n    B::constructor 1\n  } {\n    puts $n\n  }\n}", "$n", []int{3}},
		{"constructor, init block", ivar + "  constructor {n} {\n    B::constructor $n\n  } {\n  }\n}", "$n", []int{3}},
		{"destructor has no parameters", ivar + "  destructor {\n    puts $n\n  }\n}", "$n", []int{2}},
		{"itcl::body", ivar + "  method m {n}\n}\nitcl::body C::m {n} {\n  puts $n\n}", "$n", []int{5}},
		{"::itcl::body, qualified class", ivar + "  method m {n}\n}\n::itcl::body ::C::m {n} {\n  puts $n\n}", "$n", []int{5}},
		{"itcl::body, later set", ivar + "  method m {n}\n}\nitcl::body C::m {n} {\n  puts $n\n  set n 1\n}", "$n", []int{5}},
		{"signature-only method binds nothing", ivar + "  method m {n}\n  method k {} {\n    puts $n\n  }\n}", "$n", []int{2}},
		{"signature-only constructor binds nothing", ivar + "  constructor {n}\n  method k {} {\n    puts $n\n  }\n}", "$n", []int{2}},
		{"parameter of another method does not reach", ivar + "  method a {n} {\n  }\n  method b {} {\n    puts $n\n  }\n}", "$n", []int{2}},
	})
}

func TestDefinitionCursorOnMethodParameterAnswersItself(t *testing.T) {
	cases := []struct{ name, src, mark string }{
		{"method", "itcl::class C {\n  variable n 0\n  method m {n} {\n    puts $n\n  }\n}", "{n}"},
		{"constructor", "itcl::class C {\n  variable n 0\n  constructor {n} {\n  }\n}", "{n}"},
		{"itcl::body", "itcl::body C::m {n} {\n  puts $n\n}", "{n}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			param := strings.Index(c.src, c.mark) + 1
			locs := resolverOverGuardFile(c.src).Definition(guardFile, c.src, param)
			if len(locs) != 1 || locs[0].NameStart != param {
				t.Fatalf("Definition on the parameter at %d = %#v, want only the parameter", param, locs)
			}
		})
	}
}

// A signature-only declaration's braces are data until itcl::body supplies a body, so
// its names are not locals of anything.
func TestSignatureOnlyDeclarationEmitsNoParameters(t *testing.T) {
	for _, src := range []string{
		"itcl::class C {\n  method m {n}\n}",
		"itcl::class C {\n  public method m {n}\n}",
		"itcl::class C {\n  constructor {n}\n}",
	} {
		for _, def := range source.Defs(guardFile, src) {
			if def.Kind == tcl.DefLocal {
				t.Errorf("source %q emitted the local %q, want none", src, def.Name)
			}
		}
	}
}
