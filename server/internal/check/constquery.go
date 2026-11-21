package check

import (
	"fmt"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const constantQueryID = "constant-query"

// argShape is what one argument of a pure query must be for the Check to know
// the query gives the same result in Tcl 8.6 and 9.0.
type argShape int

const (
	argText  argShape = iota // printable ASCII (plainText)
	argIndex                 // an index both versions read alike (plainIndex)
	argList                  // a list of the words between its spaces (plainListValue)
)

// stringQueries and listQueries are the purity table: each pure query, with the
// shape of each of its arguments. `string is` has its own rule (stringIs). The
// evidence that each entry is pure, and gives the same result in both versions
// for every argument of its shape, is testdata/tclsh/constant_query.tcl in
// package checkcmd.
var (
	stringQueries = map[string][]argShape{
		"index":   {argText, argIndex},
		"first":   {argText, argText},
		"last":    {argText, argText},
		"match":   {argText, argText},
		"equal":   {argText, argText},
		"compare": {argText, argText},
	}
	listQueries = map[string][]argShape{
		"lindex":  {argList, argIndex},
	}
)

// stringIsClasses are the classes of `string is` the table holds. Each but
// integer tests a value char by char, and both versions class every printable
// ASCII char alike; integer also needs plainInteger.
var stringIsClasses = map[string]bool{
	"alnum": true, "alpha": true, "ascii": true, "control": true, "digit": true,
	"graph": true, "lower": true, "print": true, "punct": true, "space": true,
	"upper": true, "wordchar": true, "xdigit": true, "integer": true,
}

// constantQuery flags a pure query builtin called with only literal arguments,
// such as `string is integer 5` or `lindex {a b c} 1`: its result is the same
// on every run. The query and each option must be spelled in full, the words
// must not touch, and each argument must have its entry's shape. Constructors
// and transforms (`list`, `format`, `string toupper`) are not in the table, and
// neither are `string length` and `llength`: measuring a literal is a readable
// way to derive a number (`set width [string length {Continue}]`).
func constantQuery(c tcl.Command, base int, out []Finding) []Finding {
	w := c.Words
	head := builtinHead(c)
	var sub string // the subcommand of string, or ""
	var shapes []argShape
	args := w[1:]
	switch {
	case listQueries[head] != nil:
		shapes = listQueries[head]
	case head == "string" && len(w) >= 2 && w[1].Kind == tcl.WordBare:
		sub, args = w[1].Text, w[2:]
		if sub == "is" {
			if !stringIs(args) {
				return out
			}
		} else if shapes = stringQueries[sub]; shapes == nil {
			return out
		}
	default:
		return out
	}
	if !separated(w) {
		return out
	}
	if shapes != nil {
		if len(args) != len(shapes) {
			return out
		}
		for i, s := range shapes {
			if !hasShape(args[i], s) {
				return out
			}
		}
	}
	name, assumes := head, []string{head}
	if sub != "" {
		name, assumes = "string "+sub, []string{"string", sub}
	}
	start, end := commandSpan(c, base)
	return append(out, Finding{
		Severity: Warning,
		Start:    start,
		End:      end,
		Message:  fmt.Sprintf("%s has only literal arguments, so it always returns the same result", name),
		Assumes:  assumes,
	})
}

// stringIs reports whether args, the words after `string is`, are a class the
// table holds, an optional -strict and one value of the class's shape.
func stringIs(args []tcl.Word) bool {
	if len(args) == 3 && (args[1].Kind != tcl.WordBare || args[1].Text != "-strict") || len(args) != 2 && len(args) != 3 {
		return false
	}
	cls := args[0]
	if cls.Kind != tcl.WordBare || !stringIsClasses[cls.Text] {
		return false
	}
	v, ok := plainText(args[len(args)-1])
	return ok && (cls.Text != "integer" || plainInteger(v))
}

// separated reports whether no two of w touch: Tcl rejects `"a"b` and `{a}b`.
func separated(w []tcl.Word) bool {
	for i := 1; i < len(w); i++ {
		if w[i-1].End >= w[i].Start {
			return false
		}
	}
	return true
}

func hasShape(w tcl.Word, s argShape) bool {
	v, ok := plainText(w)
	if !ok {
		return false
	}
	switch s {
	case argIndex:
		return plainIndex(v)
	case argList:
		return !strings.ContainsAny(v, "{}\"\\")
	}
	return true
}

// plainText returns the value of w when w is a literal and its value is
// printable ASCII. A literal is a braced word with no backslash (a
// backslash-newline is replaced even in braces), or a bare or closed quoted word
// with no unescaped `$` or `[`, whose backslash escapes are decoded. Other text
// differs by version: Unicode tables, and 8.6's UTF-16 strings.
func plainText(w tcl.Word) (string, bool) {
	t := w.Text
	var v string
	switch w.Kind {
	case tcl.WordBraced:
		if len(t) < 2 || t[len(t)-1] != '}' || strings.HasPrefix(t, "{*}") && len(t) > 3 || strings.IndexByte(t, '\\') >= 0 {
			return "", false // `{*}…` expands its word into several
		}
		v = t[1 : len(t)-1]
	case tcl.WordQuoted:
		if !quoteCloses(w) {
			return "", false
		}
		t = t[1 : len(t)-1]
		fallthrough
	case tcl.WordBare:
		if substitutes(t) {
			return "", false
		}
		v = t
		if strings.IndexByte(t, '\\') >= 0 {
			var ok bool
			if v, ok = tcl.DecodeBackslashes(t); !ok {
				return "", false
			}
		}
	default:
		return "", false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return "", false
		}
	}
	return v, true
}

// substitutes reports whether t, a bare word or a quoted word's inside, holds a
// `$` or `[` that no backslash escapes.
func substitutes(t string) bool {
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '\\':
			i++
		case '$', '[':
			return true
		}
	}
	return false
}

// plainIndex reports whether v is an index Tcl 8.6 and 9.0 read alike: a small
// decimal, end, or end- and a small decimal. 8.6 reads 010 as octal and `e` as
// end; 9.0 does neither.
func plainIndex(v string) bool {
	if v == "end" {
		return true
	}
	return smallDecimal(strings.TrimPrefix(v, "end-"))
}

// plainInteger reports whether v is a value `string is integer` tests alike in
// Tcl 8.6 and 9.0: a small decimal with an optional sign and spaces around it,
// or text with no digit. 8.6 reads a leading 0 as octal and stops at 32 bits;
// 9.0 reads `1_000` and `0d10`.
func plainInteger(v string) bool {
	if strings.IndexAny(v, "0123456789") < 0 {
		return true
	}
	v = strings.Trim(v, " ")
	if len(v) > 0 && (v[0] == '+' || v[0] == '-') {
		v = v[1:]
	}
	return smallDecimal(v)
}

// smallDecimal reports whether v is 0, or a decimal of at most 9 digits with no
// leading zero, which every Tcl reads as the same integer.
func smallDecimal(v string) bool {
	if v == "0" {
		return true
	}
	if len(v) == 0 || len(v) > 9 || v[0] == '0' {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}
