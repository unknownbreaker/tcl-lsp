package checkcmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every command in constant_transform.out gets the finding its row names, and
// the Check flags exactly the words each fuzz line accepts.
func TestConstantTransformTranscript(t *testing.T) {
	purityTranscript{
		path:     "testdata/tclsh/constant_transform.out",
		finding:  " hint constant-transform ",
		fuzzList: ctFuzzCases,
	}.verify(t)
}

var ctTrimChars = enumWords([]string{" ", "-", "a"}, 2)

var ctFuzzCases = map[string]fuzzCase{
	"case": {asciiAlpha(), 2, func(v string) []string {
		var out []string
		for _, sub := range []string{"toupper", "tolower", "totitle", "reverse", "trim", "trimleft", "trimright"} {
			out = append(out, "string "+sub+" "+tq(v))
		}
		return out
	}},
	"trim": {[]string{"a", "b", " ", "-", `\`, "x"}, 3, func(v string) []string {
		var out []string
		for _, s := range ctTrimChars {
			for _, sub := range []string{"trim", "trimleft", "trimright"} {
				out = append(out, "string "+sub+" "+tq(v)+" "+tq(s))
			}
		}
		return out
	}},
	"range": {[]string{"0", "1", "9", "e", "n", "d", "-", "+", "x", "_", " "}, 4, func(v string) []string {
		return []string{
			"string range abcdefghijk " + tq(v) + " end",
			"string range abcdefghijk 0 " + tq(v),
			"lrange {a b c d e f g h i j k} " + tq(v) + " end",
			"lrange {a b c d e f g h i j k} 0 " + tq(v),
		}
	}},
	"list": {[]string{"a", "A", "b", " ", ";", "#", "$", "[", "]"}, 4, func(v string) []string {
		return []string{"lsort " + tq(v), "lreverse " + tq(v), "lrange " + tq(v) + " 0 end", "lrange " + tq(v) + " 1 end-1"}
	}},
}

// A redefined command, or a command named after the subcommand in any
// namespace (`::tcl::string::toupper` is what `string toupper` runs), silences
// the hint.
func TestConstantTransformShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"proc string args {}\nstring toupper abc\nlsort {b a}\n", 1},
		{"proc ::tcl::string::toupper args {}\nstring toupper abc\nstring trim { x }\n", 1},
		{"proc trim args {}\nstring trim { x }\nstring tolower A\n", 1},
		{"rename lsort {}\nlsort {b a}\nlreverse {a b}\n", 1},
		{"interp alias {} lrange {} myrange\nlrange {a b} 0 0\n", 0},
		{"namespace eval ns { proc lreverse args {} }\nlreverse {a b}\n", 0},
		{"proc proc args {}\nproc p {} { string toupper abc }\n", 0},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if got := strings.Count(out, " constant-transform "); got != tc.want {
			t.Errorf("%q: got %d findings, want %d:\n%s", tc.src, got, tc.want, out)
		}
	}
}

// The hint names the transform and starts at the command's head, and a hint
// does not fail `tcl-lsp check`.
func TestConstantTransformMessage(t *testing.T) {
	dir := t.TempDir()
	for src, want := range map[string]string{
		"string toupper abc\n":          "1:1: hint constant-transform string toupper has only literal arguments, so it always returns the same result",
		"set s [::string trim { x }]\n": "1:8: hint constant-transform string trim has only literal arguments, so it always returns the same result",
		"puts [lsort {b a}]\n":          "1:7: hint constant-transform lsort has only literal arguments, so it always returns the same result",
	} {
		path := filepath.Join(dir, "m.tcl")
		write(t, path, src)
		out, _, status := run(path)
		if out != path+":"+want+"\n" || status != 0 {
			t.Errorf("%q: got %q (exit %d), want %q (exit 0)", src, out, status, want)
		}
	}
}
