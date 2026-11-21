package checkcmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every command in constant_query.out gets the finding its row names. An
// exempt transform row may get constant-transform's hint, and nothing else.
func TestConstantQueryTranscript(t *testing.T) {
	purityTranscript{
		path:     "testdata/tclsh/constant_query.out",
		finding:  " warning constant-query ",
		allow:    " hint constant-transform ",
		fuzzList: cqFuzzCases,
	}.verify(t)
}

var cqClasses = []string{"alnum", "alpha", "ascii", "control", "digit", "graph", "lower", "print", "punct", "space", "upper", "wordchar", "xdigit"}

var cqFuzzCases = map[string]fuzzCase{
	"classes": {asciiAlpha(), 1, func(v string) []string {
		var out []string
		for _, cls := range cqClasses {
			out = append(out, "string is "+cls+" "+tq(v), "string is "+cls+" -strict "+tq(v))
		}
		return out
	}},
	"integer": {[]string{"0", "1", "8", "9", "x", "o", "b", "d", "e", "_", "+", "-", " ", "a"}, 4, func(v string) []string {
		return []string{"string is integer " + tq(v), "string is integer -strict " + tq(v)}
	}},
	"index": {[]string{"0", "1", "9", "e", "n", "d", "-", "+", "x", "_", " "}, 4, func(v string) []string {
		return []string{"lindex {a b c d e f g h i j k} " + tq(v), "string index abcdefghijk " + tq(v)}
	}},
	"match": {[]string{"a", "b", "*", "?", "[", "]", "-", `\`, "^", "!"}, 3, func(v string) []string {
		var out []string
		for _, s := range enumWords([]string{"a", "b", "-", "]", `\`}, 2) {
			out = append(out, "string match "+tq(v)+" "+tq(s))
		}
		return out
	}},
	"text": {[]string{"a", "b", " ", "-", `\`}, 3, func(v string) []string {
		out := []string{"string index " + tq(v) + " end"}
		for _, s := range enumWords([]string{"a", "b"}, 2) {
			for _, sub := range []string{"first", "last", "equal", "compare"} {
				out = append(out, "string "+sub+" "+tq(s)+" "+tq(v))
			}
		}
		return out
	}},
	"list": {[]string{"a", "b", " ", ";", "#", "$", "[", "]"}, 4, func(v string) []string {
		return []string{"lindex " + tq(v) + " 0", "lindex " + tq(v) + " end"}
	}},
}

// A redefined command, or a command named after the subcommand in any
// namespace (`::tcl::string::index` is what `string index` runs), silences
// the finding; a namespace's own `string` does too.
func TestConstantQueryShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"proc string args {}\nstring index abc 0\nlindex {a b} 0\n", 1},
		{"namespace eval ns { proc string args {} }\nstring index abc 0\n", 0},
		{"proc ::tcl::string::index args {}\nstring index abc 0\nstring is alpha a\n", 1},
		{"proc is args {}\nstring is alpha a\nstring index abc 0\n", 1},
		{"rename lindex {}\nlindex {a b} 0\nstring index abc 0\n", 1},
		{"interp alias {} lindex {} myindex\nlindex {a b} 0\n", 0},
		{"proc for args {}\nproc p {} { string index abc 0 }\n", 1},
		{"proc proc args {}\nproc p {} { string index abc 0 }\n", 0},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if got := strings.Count(out, " constant-query "); got != tc.want {
			t.Errorf("%q: got %d findings, want %d:\n%s", tc.src, got, tc.want, out)
		}
	}
}

// The message names the query, and the finding starts at the command's head.
func TestConstantQueryMessage(t *testing.T) {
	dir := t.TempDir()
	for src, want := range map[string]string{
		"string index abc 0\n":          "1:1: warning constant-query string index has only literal arguments, so it always returns the same result",
		"set n [::string is alpha a]\n": "1:8: warning constant-query string is has only literal arguments, so it always returns the same result",
		"puts [lindex {a b} 0]\n":       "1:7: warning constant-query lindex has only literal arguments, so it always returns the same result",
	} {
		path := filepath.Join(dir, "m.tcl")
		write(t, path, src)
		out, _, _ := run(path)
		if out != path+":"+want+"\n" {
			t.Errorf("%q: got %q, want %q", src, out, want)
		}
	}
}

// Touching words are a Tcl parse error ("extra characters after close-quote"
// or "close-brace" on 8.6 and 9.0), so nothing runs and nothing is flagged.
func TestConstantQueryTouchingWordsSilent(t *testing.T) {
	for _, src := range []string{
		"string equal \"a\"\"b\"\n",
		"string equal {a}{b}\n",
		"lindex {a b}{0}\n",
	} {
		if got := countFindings(src, "constant-query"); got != 0 {
			t.Errorf("%q: got %d findings, want 0", src, got)
		}
	}
}
