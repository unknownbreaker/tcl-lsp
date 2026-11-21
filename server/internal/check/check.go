// Package check holds the Checks: certain-only diagnostics for Tcl that is valid
// but cannot mean anything sensible. A Check reports a finding only when the code
// is certainly pointless whatever the program does, and is silent otherwise: a
// missing finding is fine, a wrong one is a bug. Checks read the parse only; they
// run on every command, or every script, the tcl package's visitor reaches
// (tcl.Visit).
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
	// Unnecessary marks code that has no effect, so an editor fades its range
	// rather than underlining it.
	Unnecessary bool
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

// checks lists every Check that reads one command, in the order a command's
// findings are reported. scriptChecks lists those that read a whole script.
var checks = []check{
	{id: selfAssignmentID, run: selfAssignment},
	{id: constantConditionID, run: constantCondition},
	{id: selfComparisonID, run: selfComparison},
	{id: duplicateLogicID, run: duplicateLogic},
	{id: constantQueryID, run: constantQuery},
	{id: constantTransformID, run: constantTransform},
}

// scriptCheck is one Check that reads a script: an id and a function that
// appends its findings on cmds, whose word offsets are relative to base, to
// out. loop says a break or continue in the script ends a loop
// (tcl.Visit.Script).
type scriptCheck struct {
	id  string
	run func(cmds []tcl.Command, base int, loop bool, out []Finding) []Finding
}

var scriptChecks = []scriptCheck{
	{id: unreachableCodeID, run: unreachableCode},
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
		col.label(from, ch.id, assumes)
	}
}

// Script runs every script Check on cmds. It is a tcl.Visit's Script.
func (col *Collector) Script(cmds []tcl.Command, base int, assumes []string, loop bool) {
	for _, ch := range scriptChecks {
		from := len(col.findings)
		col.findings = ch.run(cmds, base, loop, col.findings)
		col.label(from, ch.id, assumes)
	}
}

// label gives the findings from index from on the Check id, and prefixes their
// Assumes with assumes, the builtins the visitor trusted to reach them.
func (col *Collector) label(from int, id string, assumes []string) {
	for i := from; i < len(col.findings); i++ {
		f := &col.findings[i]
		f.Check = id
		f.Assumes = append(append([]string(nil), assumes...), f.Assumes...)
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

// expands reports whether w is an expanded word, `{*}…`, which Tcl splits into
// several.
func expands(w tcl.Word) bool {
	return len(w.Text) > 3 && w.Text[:3] == "{*}"
}

// quoteCloses reports whether quoted word w ends with its closing quote; a word
// left open runs to the end of the script and is rejected by Tcl.
func quoteCloses(w tcl.Word) bool {
	return len(w.Text) >= 2 && w.Text[len(w.Text)-1] == '"'
}

// appendExprWords appends to dst the words c hands to expr, with the name of
// the command that evaluates them: every if and elseif condition, a while or
// for test (tcl.AppendConditions), or the one braced argument of `expr {…}`.
func appendExprWords(dst []tcl.Word, c tcl.Command) ([]tcl.Word, string) {
	if conds := tcl.AppendConditions(dst, c.Words); len(conds) > len(dst) {
		return conds, c.Words[0].Text
	}
	w := c.Words
	if len(w) != 2 || builtinHead(c) != "expr" || w[1].Kind != tcl.WordBraced {
		return dst, ""
	}
	return append(dst, w[1]), "expr"
}

// exprOffset is the absolute offset of the text exprText returns for w, a word
// of a command whose offsets are relative to base.
func exprOffset(w tcl.Word, base int) int {
	if w.Kind != tcl.WordBare {
		return base + w.Start + 1 // past the opening brace or quote
	}
	return base + w.Start
}
