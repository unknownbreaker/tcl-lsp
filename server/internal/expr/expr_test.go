package expr

import (
	"strings"
	"testing"
)

// group renders n fully parenthesized, from the source text, so a test reads
// the grouping Parse found.
func group(src string, n *Node) string {
	switch n.Kind {
	case Unary:
		return "(" + n.Op + group(src, n.Args[0]) + ")"
	case Binary:
		return "(" + group(src, n.Args[0]) + " " + n.Op + " " + group(src, n.Args[1]) + ")"
	case Ternary:
		return "(" + group(src, n.Args[0]) + " ? " + group(src, n.Args[1]) + " : " + group(src, n.Args[2]) + ")"
	case Paren:
		return group(src, n.Args[0])
	case Call:
		var args []string
		for _, a := range n.Args {
			args = append(args, group(src, a))
		}
		return n.Op + "(" + strings.Join(args, ", ") + ")"
	}
	return src[n.Start:n.End]
}

// Each grouping matches what tclsh 8.6 and 9.0 compute for the expression (the
// comment gives the value that tells the groupings apart).
func TestPrecedence(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`"a" == "a" eq "1"`, `(("a" == "a") eq "1")`}, // 1; eq first gives 0
		{`"a" eq "a" in {1}`, `(("a" eq "a") in {1})`}, // 1; in first gives 0
		{`1 & 2 == 2`, `(1 & (2 == 2))`},               // 1
		{`2 ** 3 ** 2`, `(2 ** (3 ** 2))`},             // 512
		{`-2 ** 2`, `((-2) ** 2)`},                     // 4
		{`1 | 2 ^ 3`, `(1 | (2 ^ 3))`},                 // 1
		{`1 ^ 3 & 2`, `(1 ^ (3 & 2))`},                 // 3
		{`1 || 0 && 0`, `(1 || (0 && 0))`},             // 1
		{`1 ? 0 : 1 ? 2 : 3`, `(1 ? 0 : (1 ? 2 : 3))`}, // 0
		{`1 << 2 < 5`, `((1 << 2) < 5)`},               // 1
		{`1 + 2 << 1`, `((1 + 2) << 1)`},               // 6
		{`2 * 3 + 1`, `((2 * 3) + 1)`},                 // 7
		{`7 % 3 * 2`, `((7 % 3) * 2)`},                 // 2
		{`1 <= 2 == 1`, `((1 <= 2) == 1)`},             // 1
		{`!0 + 1`, `((!0) + 1)`},                       // 2
		{`1 - 2 - 3`, `((1 - 2) - 3)`},                 // -4
		{`abs (-1) == 1`, `(abs((-1)) == 1)`},          // 1
		{`max(1, 2+3) > $x`, `(max(1, (2 + 3)) > $x)`}, //
		{`(1 == 1)`, `(1 == 1)`},                       // 1
		{`"a"eq"a"`, `("a" eq "a")`},                   // 1
		{`1<2`, `(1 < 2)`},                             // 1
		{"1 == \\\n    1", `(1 == 1)`},                 // backslash-newline is a space
		{`!!$x`, `(!(!$x))`},                           //
		{`$a(k) ne [f]`, `($a(k) ne [f])`},             //
		{`"x$y" in {a b}`, `("x$y" in {a b})`},         //
		{`rand()`, `rand()`},                           //
		{`$::ns::v == ${a b}`, `($::ns::v == ${a b})`}, //
		{`$a($i,[f]) eq 1`, `($a($i,[f]) eq 1)`},       //
	} {
		n, ok := Parse(c.src, 0)
		if !ok {
			t.Errorf("Parse(%q) failed", c.src)
			continue
		}
		if got := group(c.src, n); got != c.want {
			t.Errorf("Parse(%q) = %s, want %s", c.src, got, c.want)
		}
	}
}

// Every leaf gets the kind a Check reads it by.
func TestKinds(t *testing.T) {
	for _, c := range []struct {
		src  string
		want Kind
	}{
		{`1`, Literal}, {`0x1F`, Literal}, {`0o7`, Literal}, {`0b101`, Literal},
		{`1.5e-3`, Literal}, {`.5`, Literal}, {`5.`, Literal}, {`0`, Literal},
		{`00.5`, Literal}, {`01e1`, Literal}, {`0.5`, Literal},
		{`true`, Literal}, {`OFF`, Literal}, {`"a\x41"`, Literal}, {`"\$x"`, Literal},
		{`{$x [y]}`, Literal}, {`""`, Literal}, {`{}`, Literal},
		{`$x`, Var}, {`${x y}`, Var}, {`$a(1)`, Var}, {`$ns::x`, Var},
		{`[f]`, Script}, {`"a $x"`, Quoted}, {`"[f]"`, Quoted},
		{`f()`, Call}, {`(1)`, Paren}, {`-1`, Unary},
	} {
		n, ok := Parse(c.src, 0)
		if !ok || n.Kind != c.want {
			t.Errorf("Parse(%q) = %+v %v, want kind %d", c.src, n, ok, c.want)
		}
	}
}

// Text Tcl rejects, or that Tcl 8.6 and 9.0 read differently, or that the parser
// does not model, gives no tree.
func TestUnparsed(t *testing.T) {
	for _, src := range []string{
		``, ` `, `1 ==`, `== 1`, `(1 == 1`, `1 == 1)`, `1 ? 2`, `1 === 1`, `1 = 1`,
		`1 <> 1`, `abc == abc`, `0x`, `1e`, `1_000 == 1000`, `0d10`, `1 == 1 # c`,
		`# c`, `1eq 1`, `1 eq1`, `"a" eqx "b"`, `t`, `fal`, `Inf > 1`, `NaN`,
		`"abc`, `{abc`, `[f`, `$`, `$(k)`, `${}`, `${a{b}`, `${a\}`, `$a(k`, `"$"`,
		`f(1,`, `f(1 2)`, `f(,)`, `1 2`, `.`, `1..2`, `0b12`, `[string map {] x} $s]`,
		`$x !`, `!`, `1 +`, `()`, `1 ? 2 : `, `"a" "b"`, `abc::d(1)`, `1 == 1;`,
		"\\x31 == 1", `1 == [f] [g]`, `"\`, `$a(\`,
		// Tcl 9.0 only: 8.6 rejects lt, le, gt and ge as barewords, and reads a
		// leading-zero integer as octal (010 is 8, 09 an error), where 9.0 reads
		// it as decimal.
		`"a" lt "b"`, `"a" le "b"`, `"a" gt "b"`, `"a" ge "b"`, `1 < "b" lt "a"`,
		`09 == 9`, `08`, `010 == 8`, `00`, `-07`,
	} {
		if n, ok := Parse(src, 0); ok {
			t.Errorf("Parse(%q) = %+v, want unparsed", src, n)
		}
	}
}

// Offsets are absolute: base plus the position in text.
func TestOffsets(t *testing.T) {
	n, ok := Parse(` "a" eq  {b} `, 100)
	if !ok || n.Start != 101 || n.End != 112 {
		t.Fatalf("got %+v %v", n, ok)
	}
	l, r := n.Args[0], n.Args[1]
	if l.Start != 101 || l.End != 104 || r.Start != 109 || r.End != 112 {
		t.Errorf("operands %+v %+v", l, r)
	}
}

// Nesting past maxDepth fails instead of exhausting the stack, and nesting within
// it parses.
func TestDepth(t *testing.T) {
	for _, c := range []struct {
		src string
		ok  bool
	}{
		{strings.Repeat("(", 100) + "1" + strings.Repeat(")", 100), true},
		{strings.Repeat("(", 100000) + "1" + strings.Repeat(")", 100000), false},
		{strings.Repeat("!", 100000) + "1", false},
		{"2" + strings.Repeat(" ** 2", 100000), false},
		{"1" + strings.Repeat(" ? 1 : 1", 100000), false},
		{"1" + strings.Repeat(" + 1", 100000), true}, // left to right: no nesting
	} {
		if _, ok := Parse(c.src, 0); ok != c.ok {
			t.Errorf("Parse(%.20q...) ok = %v, want %v", c.src, ok, c.ok)
		}
	}
}

// No input panics, and every tree's ranges nest inside the text.
func FuzzParse(f *testing.F) {
	for _, s := range []string{`1 == 1`, `"a$b(c[d])" eq {e}`, `f(1, $x ? 2 : 3) ** -4`, `${x}(`, `[a "]"]`, "1\\\n== 1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		n, ok := Parse(src, 0)
		if !ok {
			return
		}
		var walk func(*Node, int, int)
		walk = func(n *Node, lo, hi int) {
			if n.Start < lo || n.End > hi || n.Start >= n.End {
				t.Fatalf("%q: node %+v outside [%d,%d)", src, n, lo, hi)
			}
			for _, a := range n.Args {
				walk(a, n.Start, n.End)
			}
		}
		walk(n, 0, len(src))
	})
}
