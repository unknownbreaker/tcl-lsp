package check

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const constantQueryID = "constant-query"

// queries is constant-query's purity table: each pure query, with the shape of
// each of its arguments. `string is` has its own rule (stringIs). The evidence
// that each entry is pure, and gives the same result in both versions for every
// argument of its shape, is testdata/tclsh/constant_query.tcl in package
// checkcmd.
var queries = &purityTable{
	severity: Warning,
	commands: map[string][][]argShape{
		"lindex": {{argList, argIndex}},
	},
	strings: map[string][][]argShape{
		"index":   {{argText, argIndex}},
		"first":   {{argText, argText}},
		"last":    {{argText, argText}},
		"match":   {{argText, argText}},
		"equal":   {{argText, argText}},
		"compare": {{argText, argText}},
	},
	rules: map[string]func([]tcl.Word) bool{"is": stringIs},
}

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
// on every run. Constructors and transforms (`list`, `format`, `string
// toupper`) are not in the table, and neither are `string length` and
// `llength`: measuring a literal is a readable way to derive a number (`set
// width [string length {Continue}]`).
func constantQuery(c tcl.Command, base int, out []Finding) []Finding {
	return queries.find(c, base, out)
}

// stringIs reports whether args, the words after `string is`, are a class the
// table holds, an optional -strict and one value of the class's shape.
func stringIs(args []tcl.Word) bool {
	if len(args) != 2 && len(args) != 3 {
		return false
	}
	if len(args) == 3 && (args[1].Kind != tcl.WordBare || args[1].Text != "-strict") {
		return false
	}
	cls := args[0]
	if cls.Kind != tcl.WordBare || !stringIsClasses[cls.Text] {
		return false
	}
	v, ok := plainText(args[len(args)-1])
	return ok && (cls.Text != "integer" || plainInteger(v))
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
