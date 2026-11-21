package check

import (
	"fmt"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// argShape is what one argument of a pure builtin must be for a Check to know
// the call gives the same result in Tcl 8.6 and 9.0.
type argShape int

const (
	argText  argShape = iota // printable ASCII (plainText)
	argIndex                 // an index both versions read alike (plainIndex)
	argList                  // a list of the words between its spaces
)

// purityTable is a Check's table of pure builtins: each one, with the forms
// (argument shapes, one per argument) it is known to give one result for in
// Tcl 8.6 and 9.0. Its tclsh transcript in package checkcmd is the evidence.
type purityTable struct {
	severity   Severity
	commands   map[string][][]argShape // commands by name, such as lindex
	stringSubs map[string][][]argShape // string subcommands, such as index
	// rules are string subcommands that decide their own arguments (string is).
	rules map[string]func(args []tcl.Word) bool
}

// find appends a finding when c calls an entry of t with only literal
// arguments of one of its forms, such as `lindex {a b c} 1`: its result is the
// same on every run. The builtin and its subcommand must be spelled in full,
// and the words must not touch.
func (t *purityTable) find(c tcl.Command, base int, out []Finding) []Finding {
	w := c.Words
	head := builtinHead(c)
	var sub string // the subcommand of string, or ""
	var forms [][]argShape
	args := w[1:]
	switch {
	case t.commands[head] != nil:
		forms = t.commands[head]
	case head == "string" && len(w) >= 2 && w[1].Kind == tcl.WordBare:
		sub, args = w[1].Text, w[2:]
		if rule := t.rules[sub]; rule != nil {
			if !rule(args) {
				return out
			}
		} else if forms = t.stringSubs[sub]; forms == nil {
			return out
		}
	default:
		return out
	}
	if !separated(w) || forms != nil && !hasForm(args, forms) {
		return out
	}
	name, assumes := head, []string{head}
	if sub != "" {
		name, assumes = "string "+sub, []string{"string", sub}
	}
	start, end := commandSpan(c, base)
	return append(out, Finding{
		Severity: t.severity,
		Start:    start,
		End:      end,
		Message:  fmt.Sprintf("%s has only literal arguments, so it always returns the same result", name),
		Assumes:  assumes,
	})
}

// hasForm reports whether args match one of forms, argument by argument.
func hasForm(args []tcl.Word, forms [][]argShape) bool {
next:
	for _, form := range forms {
		if len(args) != len(form) {
			continue
		}
		for i, s := range form {
			if !hasShape(args[i], s) {
				continue next
			}
		}
		return true
	}
	return false
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
