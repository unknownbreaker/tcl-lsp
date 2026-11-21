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

// Shapes the spec keeps silent: half-typed words, expansion, data that is not
// code, escaped brackets, user-defined names in a namespace or class, and
// redefinitions through a colon run or an ensemble remap (each verified on
// tclsh 8.6 and 9.0).
func TestConstantTransformSilentShapes(t *testing.T) {
	dir := t.TempDir()
	for name, src := range map[string]string{
		"half-typed brace":            "string toupper {a {b}",
		"half-typed in substitution":  "set x [string toupper {a {b}]",
		"escaped quote at EOF":        "string toupper \"a\\\"",
		"expand quoted":               "string trim {*}\"a b\"\n",
		"expand bare":                 "string toupper {*}abc\n",
		"braced data":                 "set x {[lsort {b a}]}\n",
		"escaped bracket":             "set y \"\\[string toupper abc]\"\n",
		"proc default":                "proc p {{a {[lsort {b a}]}}} {}\n",
		"expr braced string":          "set e [expr {{[lsort {b a}]}}]\n",
		"comment continuation":        "# c \\\nstring toupper abc\n",
		"backslash-newline in quotes": "lsort \"b \\\n a\"\n",
		"tab escape":                  "string trim \"\\ta\"\n",
		"local proc in namespace":     "namespace eval ns {\n proc lsort args {}\n lsort {b a}\n}\n",
		"itcl method":                 "itcl::class C {\n method toupper {s} {}\n method m {} {string toupper abc}\n}\n",
		"colon-run proc":              "proc :::lsort args {}\nset r [lsort {b a}]\n",
		"string ensemble remap":       "namespace ensemble configure ::string -map {toupper ::list}\nset r [string toupper abc]\n",
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, src)
		if out, _, _ := run(path); strings.Contains(out, " constant-transform ") {
			t.Errorf("%s: want silent, got:\n%s", name, out)
		}
	}
}

// In .rvt, `<?= x ?>` runs as `puts -nonewline x`, continuation line included,
// so only the command after `;` is checked.
func TestConstantTransformRvt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.rvt")
	write(t, path, "<?= string toupper abc ?>\n<?= x \\\nstring toupper abc ?>\n<?=string toupper abc?>\n<?= x; lsort {b a} ?>\n")
	out, _, _ := run(path)
	if strings.Count(out, "constant-transform") != 1 || !strings.Contains(out, "a.rvt:5:8: hint constant-transform lsort") {
		t.Errorf("want only the command after `;` flagged, got:\n%s", out)
	}
}
