package tcl

import "testing"

// Every want below is what tclsh 8.6 and 9.0 name the variable in `set WORD 1`.
func TestDecodeBackslashes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a\x62`, "ab"},
		{`a\x626`, "ab6"},   // \x reads at most 2 hex digits
		{`a\xg`, "axg"},     // no hex digit: the letter stands for itself
		{`a\142`, "ab"},     // octal
		{`a\1420`, "ab0"},   // at most 3 octal digits
		{`a\777`, "a?7"},    // a third digit only while the value is below 040
		{`a\u0062c`, "abc"}, // \u reads at most 4 hex digits
		{`a\u00621`, "ab1"},
		{`a\U00000062z`, "abz"},
		{`a\U0000FFFFz`, "a\uffffz"},
		{`a\Uq`, "aUq"},
		{`a\q`, "aq"},
		{`a\n`, "a\n"},
		{`a\\b`, `a\b`},
		{`a\(k)`, "a(k)"},
		{`a\x28k)`, "a(k)"},
		{`a\é`, "aé"},
		{`a\u00e9`, "aé"},
		{`a\xe9`, "aé"},
	}
	for _, c := range cases {
		got, at, ok := decodeBackslashes(c.in)
		if !ok || got != c.want || len(at) != len(got) {
			t.Errorf("decodeBackslashes(%q) = %q, %v, %v; want %q", c.in, got, at, ok, c.want)
		}
	}
	for _, in := range []string{"a\\", "a\\\nb", `a\uD800`, `a\U00110000`, `a\U1F600`} {
		if _, _, ok := decodeBackslashes(in); ok {
			t.Errorf("decodeBackslashes(%q) is ok, want not certain", in)
		}
	}
}

// The binding sites decode the name word. tclsh 8.6 and 9.0: each command below,
// in a proc, creates the variable named in want. foreach var lists and proc args are
// lists, decoded a second time; lassign targets are not.
func TestEscapedNamesBindDecoded(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"set", `set a\x62 1`, "ab"},
		{"array element", `set a\(k) 1`, "a"},
		{"incr", `incr a\x62`, "ab"},
		{"scan", `scan $s %d a\x62`, "ab"},
		{"catch", `catch {foo} a\x62`, "ab"},
		{"global", `global a\x62`, "ab"},
		{"upvar", `upvar 1 x a\x62`, "ab"},
		{"variable", `variable a\x62`, "ab"},
		{"foreach", `foreach a\x62 {1} {}`, "ab"},
		{"foreach twice", `foreach a\\x62 {1} {}`, "ab"},
		{"foreach braced", `foreach {a\x62 c} {1 2} {}`, "ab"},
		{"lassign once", `lassign {1} a\\x62`, `a\x62`},
		{"braced element undecoded", `foreach {{a\x62}} {1} {}`, `a\x62`},
		{"braced element one name", `foreach {{a b}} {1} {}`, "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, cmd := range Parse(c.body) {
				for _, d := range localBindings(cmd, 0, "::", FrameProc) {
					if d.Name == c.want {
						return
					}
				}
			}
			t.Fatalf("%s binds no %q", c.body, c.want)
		})
	}
	src := "proc p {a\\x62 {c\\x64 1}} {}\nproc q a\\\\x62 {}\n"
	got := map[string]bool{}
	for _, d := range FileDefs(src) {
		if d.Kind == DefLocal {
			got[d.Name] = true
		}
	}
	for _, want := range []string{"ab", "cd"} {
		if !got[want] {
			t.Errorf("proc params bind %v, want %q", got, want)
		}
	}
}
