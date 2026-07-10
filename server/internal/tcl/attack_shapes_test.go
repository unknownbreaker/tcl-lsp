package tcl

import "testing"

// `set :: 1` -- the literal two-colon "name" is a pathological but
// syntactically valid bareword. arrayBaseName accepts it (no '(', no $/[),
// and emitVarAssign's strings.Contains(name, "::") check fires, routing it
// through qualifyName, which short-circuits on the "::" prefix and returns
// the name UNCHANGED. Net effect: a DefNamespaceVar named exactly "::" (empty
// tail). Not a crash, but a strange indexed symbol; document behavior.
func TestSetDoubleColonAlone(t *testing.T) {
	defs := FileDefs("set :: 1\n")
	for _, d := range defs {
		if d.Kind == DefNamespaceVar {
			t.Logf("`set :: 1` produced DefNamespaceVar Name=%q (degenerate empty-tail namespace var)", d.Name)
			if d.Name != "::" {
				t.Errorf("expected degenerate Name \"::\", got %q", d.Name)
			}
		}
	}
}

// `array set` where w[1] ("set") is itself a SUBSTITUTED/quoted word (not
// WordBare) must not match -- guards against `array "set" x {a 1}` or
// `array ${cmd} x {a 1}` being treated as `array set`.
func TestArraySetSubcommandMustBeBareword(t *testing.T) {
	cases := []string{
		"array \"set\" gvar {a 1}\n",
	}
	for _, src := range cases {
		defs := FileDefs(src)
		for _, d := range defs {
			if d.Kind == DefNamespaceVar || d.Kind == DefLocal {
				t.Errorf("src %q: spurious def from non-bareword `array set` subcommand: %#v", src, d)
			}
		}
	}
}

// `array set` with too few words (`array set` alone, or `array set gvar` with
// no data arg) must not panic and must not (for the 2-word form) emit a def --
// len(w) >= 3 is required.
func TestArraySetTooFewWords(t *testing.T) {
	for _, src := range []string{"array set\n", "array set gvar\n"} {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("src %q panicked: %v", src, r)
			}
		}()
		defs := FileDefs(src)
		if src == "array set\n" {
			for _, d := range defs {
				if d.Kind == DefNamespaceVar {
					t.Errorf("src %q: spurious DefNamespaceVar: %#v", src, d)
				}
			}
		}
	}
}
