package check

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const unreachableCodeID = "unreachable-code"

// unreachableCode flags the commands of a script after an exit that always
// ends it: return, error, or a break or continue that ends a loop (loop, see
// tcl.Visit.Script). Tcl runs a script's commands in order and stops at the
// first that does not complete normally, so nothing after the exit runs. The
// finding covers every command from the first dead one to the last, once per
// script, so an editor fades them; a nested body's dead code is its own
// script's finding.
//
// The exit must be written so it cannot complete normally. A return with a
// -level or -options option may (`return -level 0` gives its value and goes
// on), so any of those, or an option name Tcl computes, is silent. error takes
// one to three arguments, and break and continue none. A break or continue
// outside a loop is an error Tcl reports itself, so it is silent too. An
// expanded word hides the word count, so it is silent, and so is a vertical
// tab, form feed or carriage return in a word: Tcl splits words there, and the
// parser does not.
func unreachableCode(cmds []tcl.Command, base int, loop bool, out []Finding) []Finding {
	for i, c := range cmds[:max(len(cmds)-1, 0)] {
		head, ok := exitHead(c, loop)
		if !ok {
			continue
		}
		start, _ := commandSpan(cmds[i+1], base)
		_, end := commandSpan(cmds[len(cmds)-1], base)
		return append(out, Finding{
			Severity:    Hint,
			Start:       start,
			End:         end,
			Message:     "code after " + head + " never runs",
			Unnecessary: true,
			Assumes:     []string{head},
		})
	}
	return out
}

// exitHead returns the name of the builtin c's head when c always ends its
// script (unreachableCode).
func exitHead(c tcl.Command, loop bool) (string, bool) {
	head := builtinHead(c)
	if head != "return" && head != "error" && head != "break" && head != "continue" {
		return "", false
	}
	w := c.Words
	if !separated(w) {
		return "", false
	}
	for _, x := range w {
		if expands(x) || strings.ContainsAny(x.Text, "\v\f\r") {
			return "", false
		}
	}
	switch head {
	case "return":
		return head, endsReturn(w[1:])
	case "error":
		return head, len(w) >= 2 && len(w) <= 4
	}
	return head, loop && len(w) == 1
}

// endsReturn reports whether a return with arguments args returns from its
// level at least once: none of its option names is -level or -options, and
// each is a literal (exprText: a braced backslash-newline gives a space, so
// it never spells either). Tcl reads args as option and value pairs, then the result
// when one word is left over, and matches option names exactly.
func endsReturn(args []tcl.Word) bool {
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := exprText(args[i])
		if !ok || name == "-level" || name == "-options" {
			return false
		}
	}
	return true
}
