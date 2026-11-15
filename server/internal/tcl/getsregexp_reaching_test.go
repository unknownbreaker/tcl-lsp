package tcl

import (
	"strings"
	"testing"
)

// Variables written by gets, regsub, regexp and binary scan. tclsh 8.6.18 and 9.0.3
// are the oracle: gets always writes its variable (an empty string at end of file),
// and regsub always writes its variable (the input when nothing matches); regexp
// writes its match variables only when it matches, and none with -about or
// -inline; binary scan stops writing at the end of its data. A leading word starting
// with `-` is a regexp option even when substituted, and options must be spelled in
// full.

// at is a want position: the byte offset of mark's last occurrence plus skip.
type at struct {
	mark string
	skip int
}

func TestReachingResultVarCommands(t *testing.T) {
	cases := []struct {
		name, body, use string
		want            []at
	}{
		{"gets", "set f [open x]\ngets $f line\nputs $line", "$line", []at{{"line\nputs", 0}}},
		{"gets kills prior", "set line 0\ngets $f line\nputs $line", "$line", []at{{"line\nputs", 0}}},
		{"gets in while test", "while {[gets $f line] >= 0} {\n  puts $line\n}", "$line", []at{{"line]", 0}}},
		{"gets array element", "gets $f a(x)\nputs $a(x)", "$a(x)", []at{{"a(x)\nputs", 0}}},
		{"regexp match var", "if {[regexp {^(\\w+)} $s -> word]} {\n  puts $word\n}", "$word", []at{{"word]", 0}}},
		{"regexp joins prior", "set word {}\nregexp {(\\w+)} $s -> word\nputs $word", "$word", []at{{"word {}", 0}, {"word\nputs", 0}}},
		{"regexp match and subs", "regexp {(a)(b)} $s all x y\nputs $y", "$y", []at{{"y\nputs", 0}}},
		{"regexp options", "regexp -nocase -line -- {a} $s m\nputs $m", "$m", []at{{"m\nputs", 0}}},
		{"regexp -start arg", "regexp -start $i {a} $s m\nputs $m", "$m", []at{{"m\nputs", 0}}},
		{"regexp quoted option", "regexp \"-nocase\" {a} $s m\nputs $m", "$m", []at{{"m\nputs", 0}}},
		{"regexp substituted exp after --", "regexp -- $re $s m\nputs $m", "$m", []at{{"m\nputs", 0}}},
		{"regexp literal exp, substituted string", "regexp {a} $s m\nputs $m", "$m", []at{{"m\nputs", 0}}},
		{"regsub", "regsub -all {a} $s {b} out\nputs $out", "$out", []at{{"out\nputs", 0}}},
		{"regsub kills prior", "regsub -all {a} $s {b} s\nwhile {[regexp {(x)} $s _ s]} {}\nputs $s", "$s\n}", []at{{"s\nwhile", 0}, {"s]", 0}}},
		{"binary scan", "binary scan $d cc a b\nputs $b", "$b", []at{{"b\nputs", 0}}},
		{"binary scan joins prior", "set b 0\nbinary scan $d cc a b\nputs $b", "$b", []at{{"b 0", 0}, {"b\nputs", 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "proc f {} {\n" + c.body + "\n}"
			var want []int
			for _, w := range c.want {
				want = append(want, indexOf(t, src, w.mark)+w.skip)
			}
			wantStarts(t, reachStarts(t, src, c.use), want...)
		})
	}
}

// Shapes whose variables are not certain bind nothing, so the use falls back to
// the earlier binding (`set v 0`) or to silence.
func TestReachingResultVarCommandsUncertain(t *testing.T) {
	cases := []struct{ name, cmd string }{
		{"gets no var", "gets $f"},
		{"gets extra word", "gets $f v junk"},
		{"gets expanded", "gets {*}$args v"},
		{"regexp substituted word, then a substituted operand", "regexp $opt {a} $s v"},
		{"regexp abbreviated option", "regexp -noc {a} $s v"},
		{"regexp unknown option", "regexp -bogus {a} $s v"},
		{"regexp -inline", "regexp -inline {a} $s v"},
		{"regexp -about", "regexp -about {a} v"},
		{"regexp no vars", "regexp {a} v"},
		{"regexp expanded", "regexp {*}$opts {a} $s v"},
		{"regexp same var twice", "regexp {(a)} $s v v"},
		{"regexp two spellings", "regexp {(a)} $s v(x) v"},
		{"regexp dynamic var name", "regexp {(a)} $s v $n"},
		{"regsub no var", "regsub {a} $v {b}"},
		{"regsub extra word", "regsub {a} $s {b} v junk"},
		{"regsub -command", "regsub -command {a} $s {string toupper} v"},
		{"binary not scan", "binary format c v"},
		{"binary scan same var", "binary scan $d cc v v"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "proc f {} {\n  set v 0\n  " + c.cmd + "\n  puts $v\n}"
			wantStarts(t, reachStarts(t, src, "$v"), strings.Index(src, "v 0"))
		})
	}
}

// Shapes whose variable may be written, though not certainly: the write joins the
// earlier `set v 0`. A substituted word before the pattern may be an option at run
// time (tclsh: `regexp $re a a m` with re=-nocase matches), which shifts every role
// right, so each variable is still one of the words after the string, but may be
// unwritten. binary is an ensemble, so a prefix of scan is scan.
func TestReachingResultVarCommandsMayWrite(t *testing.T) {
	cases := []struct{ name, cmd string }{
		{"regexp substituted pattern", "regexp $re $s -> v"},
		{"regsub substituted pattern", "regsub $re $s {b} v"},
		{"regsub substituted option", "regsub -all $re $s {b} v"},
		{"binary scan prefix", "binary sc $d c v"},
		{"binary scan one-letter prefix", "binary s $d c v"},
		{"binary scan quoted", "binary \"scan\" $d c v"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "proc f {} {\n  set v 0\n  " + c.cmd + "\n  puts $v\n}"
			wantStarts(t, reachStarts(t, src, "$v"), strings.Index(src, "v 0"), strings.LastIndex(src, "v\n  puts"))
		})
	}
}

// Outside a proc these commands bind nothing, like catch and foreach.
func TestReachingResultVarCommandsTopLevelSilent(t *testing.T) {
	src := "gets $f line\nputs $line"
	defs, _ := ReachingAt(src, indexOf(t, src, "$line")+1)
	if len(defs) != 0 {
		t.Fatalf("top-level gets answered %#v, want nothing", defs)
	}
}
