// Package check holds the Checks: certain-only diagnostics for Tcl that is valid
// but cannot mean anything sensible. A Check reports a finding only when the code
// is certainly pointless whatever the program does, and is silent otherwise: a
// missing finding is fine, a wrong one is a bug. Checks read the parse only; they
// run on every command the tcl package's visitor reaches (tcl.Visit).
package check

import "github.com/unknownbreaker/tcl-lsp/internal/tcl"

// Severity is how a finding is shown. A warning fails `tcl-lsp check`; a hint
// does not.
type Severity int

const (
	Warning Severity = iota + 1
	Hint
)

func (s Severity) String() string {
	if s == Hint {
		return "hint"
	}
	return "warning"
}

// Finding is one Check's report on a byte range of the source.
type Finding struct {
	Check    string // the Check's id, e.g. "self-assignment"
	Severity Severity
	Start    int // byte range [Start,End) in the source
	End      int
	Message  string
	// Assumes names, by last name segment, every builtin whose meaning the
	// finding relies on: the command it reports on, and each command whose body
	// the visitor entered to reach it. A finding is dropped when user code
	// defines a command of any of those names (Unshadowed).
	Assumes []string
}

// check is one Check: an id and a function that appends its findings on a
// command, whose word offsets are relative to base, to out. Appending, rather
// than calling back, keeps a command with no finding free of allocation.
type check struct {
	id  string
	run func(c tcl.Command, base int, out []Finding) []Finding
}

// checks lists every Check, in the order a command's findings are reported.
var checks = []check{
	{id: selfAssignmentID, run: selfAssignment},
	{id: constantConditionID, run: constantCondition},
	{id: selfComparisonID, run: selfComparison},
	{id: duplicateLogicID, run: duplicateLogic},
}

// Collector gathers the findings of every Check over the commands it visits.
type Collector struct {
	findings []Finding
}

// Visit runs every Check on c. It is a tcl.Visit's Command.
func (col *Collector) Visit(c tcl.Command, base int, assumes []string) {
	if len(c.Words) == 0 {
		return
	}
	for _, ch := range checks {
		from := len(col.findings)
		col.findings = ch.run(c, base, col.findings)
		for i := from; i < len(col.findings); i++ {
			f := &col.findings[i]
			f.Check = ch.id
			f.Assumes = append(append([]string(nil), assumes...), f.Assumes...)
		}
	}
}

// Findings returns what the Collector has gathered, in visit order.
func (col *Collector) Findings() []Finding { return col.findings }

// Unshadowed returns the findings none of whose assumed builtins user code
// redefines. userCommand reports whether the workspace defines a command whose
// last name segment is name, in any namespace or class: such a command may be
// the one a call resolves to, so the finding is not certain.
func Unshadowed(fs []Finding, userCommand func(name string) bool) []Finding {
	var out []Finding
next:
	for _, f := range fs {
		for _, name := range f.Assumes {
			if userCommand(name) {
				continue next
			}
		}
		out = append(out, f)
	}
	return out
}

// commandSpan returns c's byte range, from its first word's start to its last
// word's end.
func commandSpan(c tcl.Command, base int) (int, int) {
	return base + c.Words[0].Start, base + c.Words[len(c.Words)-1].End
}

// builtinHead returns the builtin name c's head spells, bare or global (`set`,
// `::set`), or "" for any other head.
func builtinHead(c tcl.Command) string {
	h := c.Words[0]
	if h.Kind != tcl.WordBare {
		return ""
	}
	if len(h.Text) > 2 && h.Text[:2] == "::" {
		return h.Text[2:]
	}
	return h.Text
}

// quoteCloses reports whether quoted word w ends with its closing quote; a word
// left open runs to the end of the script and is rejected by Tcl.
func quoteCloses(w tcl.Word) bool {
	return len(w.Text) >= 2 && w.Text[len(w.Text)-1] == '"'
}
