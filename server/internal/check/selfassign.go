package check

import (
	"fmt"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const selfAssignmentID = "self-assignment"

// selfAssignment flags `set x $x`: a variable assigned the value it already
// holds. The name must be a literal variable name, plain or `::`-qualified, and
// the value exactly one substitution of that same name (`$x`, `${x}`, `"$x"`).
// Anything else may name two variables or change the value, so it is silent:
// `set x:y $x:y` reads x and appends ":y", `set ::x $x` in a proc names a global
// and a local, and `set a(1) $a(2)` two elements (tclsh 8.6 and 9.0).
func selfAssignment(c tcl.Command, base int, out []Finding) []Finding {
	w := c.Words
	if len(w) != 3 || builtinHead(c) != "set" {
		return out
	}
	name := w[1]
	if name.Kind != tcl.WordBare || !isVarName(name.Text) {
		return out
	}
	v := w[2]
	var val string
	switch v.Kind {
	case tcl.WordBare:
		val = v.Text
	case tcl.WordQuoted:
		if len(v.Text) < 2 || v.Text[len(v.Text)-1] != '"' {
			return out
		}
		val = v.Text[1 : len(v.Text)-1]
	default:
		return out
	}
	if !isSoleSubst(val, name.Text) {
		return out
	}
	start, end := commandSpan(c, base)
	return append(out, Finding{
		Severity: Warning,
		Start:    start,
		End:      end,
		Message:  fmt.Sprintf("assigns %s its own value", name.Text),
		Assumes:  []string{"set"},
	})
}

// isSoleSubst reports whether val is exactly `$name` or `${name}`, compared
// without building either string.
func isSoleSubst(val, name string) bool {
	switch {
	case len(val) == len(name)+1:
		return val[0] == '$' && val[1:] == name
	case len(val) == len(name)+3:
		return val[:2] == "${" && val[len(val)-1] == '}' && val[2:len(val)-1] == name
	}
	return false
}

// isVarName reports whether s is a variable name Tcl's `$name` reads whole: an
// optional leading `::`, then name segments of letters, digits and underscores
// joined by `::`. A single colon, a trailing `::`, a parenthesis or any other byte
// ends what `$` reads, so such a name is not one.
func isVarName(s string) bool {
	i := 0
	if len(s) >= 2 && s[:2] == "::" {
		i = 2
	}
	for {
		seg := i
		for i < len(s) && isNameByte(s[i]) {
			i++
		}
		if i == seg {
			return false
		}
		if i == len(s) {
			return true
		}
		if i+2 > len(s) || s[i:i+2] != "::" {
			return false
		}
		i += 2
	}
}

func isNameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
