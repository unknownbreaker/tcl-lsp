package tcl

import (
	"slices"
	"strings"
)

// This file is the single source of truth for two things every tree walker in
// this package needs: (1) which braced words of a command are *script bodies*
// (vs. data or expressions), and (2) the namespace/frame each body executes in.
// The definition walker (defs.go), reference walker (context.go), and namespace
// walker (nsdecl.go) all recurse via childBodies, so they cannot drift -- e.g.
// one recursing into control-flow bodies while another silently does not (the
// bug that hid procs defined inside `if`/`catch` blocks from goto-definition).

// bodyScope is a braced script body to recurse into, with the namespace and
// frame it executes in.
type bodyScope struct {
	Inner string    // source text of the body's interior
	Base  int       // absolute offset of Inner's first byte
	NS    string    // namespace the body runs in
	Frame FrameKind // frame kind the body runs in
	Scope int       // enclosing proc body's interior offset; 0 at namespace frame
	Class string    // fully-qualified itcl class name, or "" when not inside a class
}

// memberWords strips a leading Itcl access modifier (`public`/`protected`/
// `private`) from a class-body command's words when it directly precedes a
// modifiable member keyword (`method`/`proc`/`variable`/`common`), returning the
// effective declaration words. Returns w unchanged otherwise. Only meaningful at
// FrameClass; the precise keyword guard avoids mis-stripping an unrelated command
// whose first word happens to be `public`/`private`/`protected`.
func memberWords(w []Word) []Word {
	if len(w) >= 2 && w[0].Kind == WordBare && isAccessModifier(w[0].Text) &&
		w[1].Kind == WordBare && isModifiableMember(w[1].Text) {
		return w[1:]
	}
	return w
}

func isAccessModifier(s string) bool {
	return s == "public" || s == "protected" || s == "private"
}

func isModifiableMember(s string) bool {
	switch s {
	case "method", "proc", "variable", "common":
		return true
	}
	return false
}

// minMemberBodyWords returns the word count at and above which the LAST braced
// word of a class member declaration is its body rather than its parameter list:
//
//	method NAME ARGS BODY        — 4
//	constructor ARGS ?INIT? BODY — 3
//	destructor BODY              — 2
//
// Below this count the declaration is signature-only (a forward declaration whose
// body is supplied later by itcl::body), so its trailing brace is the parameter
// list and must not be descended as a script.
func minMemberBodyWords(w []Word) int {
	switch {
	case isCmd(w, "constructor"):
		return 3
	case isCmd(w, "destructor"):
		return 2
	default: // method
		return 4
	}
}

// methodArgs returns the parameter list of an Itcl method, constructor or itcl::body
// that has a body. frame is the frame the command sits in: `method` and `constructor`
// mean this only in a class body. A signature-only declaration has no body, so its
// names bind nothing until itcl::body supplies one. The shapes match childBodies.
func methodArgs(w []Word, frame FrameKind) (Word, bool) {
	if isCmd(w, "itcl::body") || isCmd(w, "::itcl::body") {
		if len(w) >= 4 && w[len(w)-1].Kind == WordBraced && strings.LastIndex(w[1].Text, "::") > 0 {
			return w[2], true
		}
		return Word{}, false
	}
	if frame != FrameClass {
		return Word{}, false
	}
	member := memberWords(w)
	if len(member) < minMemberBodyWords(member) || member[len(member)-1].Kind != WordBraced {
		return Word{}, false
	}
	switch {
	case isCmd(member, "method"):
		return member[2], true
	case isCmd(member, "constructor"):
		return member[1], true
	}
	return Word{}, false
}

// childBodies returns the script bodies of c that walkers should recurse into,
// each tagged with the scope it runs in. `namespace eval` and `proc` introduce a
// new scope. Control-flow bodies run in the enclosing namespace and frame. A
// custom command's body keeps the namespace, but at namespace frame its frame is
// unknown, so it is FrameOpaque. Inside a proc or class the frame stays. class is
// the fully-qualified itcl class name of the enclosing class body, or "" when not
// inside a class.
func childBodies(c Command, base int, ns string, frame FrameKind, scope int, class string) []bodyScope {
	w := c.Words
	// Inside a class body, a member may carry a leading access modifier
	// (`public method foo {…}`); strip it so the keyword and body are matched
	// the same as the bare form. Confined to FrameClass, where modifiers are valid.
	if frame == FrameClass {
		w = memberWords(w)
	}
	switch {
	case isCmd(w, "namespace") && len(w) >= 4 && w[1].Text == "eval" && w[len(w)-1].Kind == WordBraced:
		// Body is the last word (Tcl's `namespace eval name script` form; for
		// multi-arg scripts Tcl concatenates, and we recurse the final braced word).
		inner, innerBase := bracedInner(w[len(w)-1], base)
		return []bodyScope{{Inner: inner, Base: innerBase, NS: qualifyNamespace(w[2].Text, ns), Frame: FrameNamespace, Scope: 0, Class: class}}
	case isCmd(w, "proc") && len(w) >= 4 && w[len(w)-1].Kind == WordBraced:
		// The proc body runs in the namespace where the proc is DEFINED: for a
		// qualified name (proc ::a::b {} {...}) that is ::a -- the name's own
		// namespace -- not the lexically enclosing one. This is what makes
		// `variable cfg` inside proc ::a::b link to ::a::cfg.
		inner, innerBase := bracedInner(w[len(w)-1], base)
		return []bodyScope{{Inner: inner, Base: innerBase, NS: procBodyNS(w[1].Text, ns), Frame: FrameProc, Scope: innerBase, Class: class}}
	case (isCmd(w, "itcl::class") || isCmd(w, "::itcl::class")) && len(w) >= 3 && w[len(w)-1].Kind == WordBraced:
		// itcl::class NAME BODY — the body is a member-declaration scope.
		inner, innerBase := bracedInner(w[len(w)-1], base)
		return []bodyScope{{Inner: inner, Base: innerBase, NS: ns, Frame: FrameClass, Scope: 0, Class: qualifyName(w[1].Text, ns)}}
	case frame == FrameClass && (isCmd(w, "method") || isCmd(w, "constructor") || isCmd(w, "destructor")) && w[len(w)-1].Kind == WordBraced:
		// method/constructor/destructor inside a class body — each runs as a proc
		// frame carrying the enclosing class name.
		//
		// The last braced word is the *body* only when the declaration actually
		// has one. A signature-only declaration — a method forward-declared in the
		// class block whose body is supplied later by `itcl::body` — has no body,
		// and its last braced word is the *parameter list* (data, not code). Return
		// nil (rather than falling through to the trailing-braced-argument heuristic
		// in the default branch, which would walk the param list and emit bogus
		// refs to its first name).
		if len(w) < minMemberBodyWords(w) {
			return nil
		}
		inner, innerBase := bracedInner(w[len(w)-1], base)
		out := []bodyScope{{Inner: inner, Base: innerBase, NS: ns, Frame: FrameProc, Scope: innerBase, Class: class}}
		// A constructor may carry an optional init block before the body
		// (`constructor args {Base::constructor …} {body}`) that chains the base
		// class; walk it too so the chained call is found as a reference.
		if isCmd(w, "constructor") && len(w) >= 4 && w[len(w)-2].Kind == WordBraced {
			initInner, initBase := bracedInner(w[len(w)-2], base)
			out = append(out, bodyScope{Inner: initInner, Base: initBase, NS: ns, Frame: FrameProc, Scope: initBase, Class: class})
		}
		return out
	case (isCmd(w, "itcl::body") || isCmd(w, "::itcl::body")) && len(w) >= 4 && w[len(w)-1].Kind == WordBraced:
		// itcl::body ::Class::method args body — the body is a method scope; extract
		// the class from the qualified name (split on last ::) so locals/calls inside
		// resolve with the correct class context.
		full := w[1].Text
		if i := strings.LastIndex(full, "::"); i > 0 {
			classFQ := qualifyName(full[:i], ns)
			inner, innerBase := bracedInner(w[len(w)-1], base)
			return []bodyScope{{Inner: inner, Base: innerBase, NS: ns, Frame: FrameProc, Scope: innerBase, Class: classFQ}}
		}
		return nil
	default:
		// A decorated proc definition (`CACHE_PROC proc name args body`): its body
		// is a proc scope, exactly like a plain proc (including the qualified-name
		// namespace rule).
		if name, _, body, ok := decoratedProcDef(w); ok {
			inner, innerBase := bracedInner(body, base)
			return []bodyScope{{Inner: inner, Base: innerBase, NS: procBodyNS(name.Text, ns), Frame: FrameProc, Scope: innerBase, Class: class}}
		}
		// Control-flow bodies (if/while/for/foreach/catch/try/switch/...) run in the
		// enclosing namespace and frame. A custom command's trailing brace is walked for calls,
		// but at namespace frame nothing says which frame it runs in: it may be a method
		// or a proc-like scope, with locals of its own. No new scope either way.
		//
		// Known limit: calls there still resolve against the lexical namespace. A TclOO
		// method runs in the object's namespace, so a bare `helper` in one errors even
		// when `::n::helper` exists, yet it answers `::n::helper`. A wrapper's body cannot
		// be told apart from a method's, and real code calling `helper` there usually has
		// `namespace path` set, so calls stay resolved.
		bodyFrame := frame
		if frame == FrameNamespace && !bodiesRunHere(c) {
			bodyFrame = FrameOpaque
		}
		var out []bodyScope
		for _, body := range scriptBodies(w) {
			inner, innerBase := bracedInner(body, base)
			out = append(out, bodyScope{Inner: inner, Base: innerBase, NS: ns, Frame: bodyFrame, Scope: scope, Class: class})
		}
		return out
	}
}

// decoratedProcDef recognizes a proc definition created through a proc-defining
// macro: `WRAPPER... proc NAME ARGS BODY ...`, where the command head is the macro
// (e.g. CACHE_PROC) rather than `proc`. It matches the first `proc NAME ARGS BODY`
// quadruple -- NAME a plain name, BODY braced -- that has at least one decorator
// word before `proc`. Words *after* BODY (e.g. `-ttl 60`, `-log debug`) are macro
// flags and are ignored, so the body need not be the command's last word. The
// head must not be `proc` (handled directly by the proc cases) nor a list/data
// builtin (so `lappend x proc foo {} {}` is not misread). This covers single,
// argument-taking, and stacked decorators, a qualified NAME, and trailing flags.
// procBodyNS returns the namespace a proc's body runs in: for a qualified proc
// name (`::a::b`, `a::b`) the namespace part of the qualified name; for a plain
// name, the lexically enclosing namespace.
func procBodyNS(name, ns string) string {
	if !strings.Contains(name, "::") {
		return ns
	}
	fq := qualifyName(name, ns)
	if i := strings.LastIndex(fq, "::"); i > 0 {
		return fq[:i]
	}
	return "::" // `::name` -- defined directly in the global namespace
}

func decoratedProcDef(w []Word) (name, args, body Word, ok bool) {
	if len(w) < 5 || w[0].Kind != WordBare || listOrDataHeads[w[0].Text] {
		return // need at least DECORATOR proc NAME ARGS BODY, head a decorator word
	}
	// Scan for `proc NAME ARGS BODY` starting after the first (decorator) word.
	for i := 1; i+3 < len(w); i++ {
		if w[i].Kind == WordBare && w[i].Text == "proc" && isPlainName(w[i+1]) && w[i+3].Kind == WordBraced {
			return w[i+1], w[i+2], w[i+3], true
		}
	}
	return
}

// listOrDataHeads are builtins whose arguments may literally contain the word
// `proc` as data (a list element, a value or a switch pattern), so a trailing
// `proc name args body` in them is not a decorated proc definition.
var listOrDataHeads = map[string]bool{
	"set": true, "list": true, "lappend": true, "lassign": true, "lset": true,
	"linsert": true, "lreplace": true, "concat": true, "dict": true,
	"array": true, "append": true, "return": true, "expr": true, "switch": true,
}

// scriptBodies returns the braced script-argument words of a command. Expression
// arguments (a while/if condition, a for test) are deliberately excluded so their
// contents are not misread as commands. It says nothing about the frame a script
// runs in: childBodies decides that.
func scriptBodies(w []Word) []Word {
	if len(w) == 0 || w[0].Kind != WordBare {
		return nil
	}
	braced := func(i int) bool { return i >= 0 && i < len(w) && w[i].Kind == WordBraced }
	last := len(w) - 1
	switch w[0].Text {
	case "while", "foreach", "lmap":
		// while test body ; foreach/lmap (var list)+ body -- body is the last word.
		if braced(last) {
			return []Word{w[last]}
		}
	case "dict":
		// dict for {k v} dict body ; dict map {k v} dict body -- body is last.
		if len(w) >= 2 && w[1].Kind == WordBare && (w[1].Text == "for" || w[1].Text == "map") && braced(last) {
			return []Word{w[last]}
		}
	case "catch":
		// catch script ?resultVar? ?optionsVar? -- the first arg is the script.
		if braced(1) {
			return []Word{w[1]}
		}
	case "for":
		// for start test next body -- start, next and body are scripts; test is expr.
		var bodies []Word
		for _, i := range []int{1, 3, 4} {
			if braced(i) {
				bodies = append(bodies, w[i])
			}
		}
		return bodies
	case "if":
		return ifBodies(w)
	case "try":
		return tryBodies(w)
	case "switch":
		return switchBodies(w)
	default:
		// User-defined or unrecognised commands: by strong Tcl convention a
		// trailing braced argument is a script body (custom control structures,
		// test harnesses, oo definitions). It is walked for calls, but its frame is
		// unknown, so childBodies does not treat its bare names as this frame's.
		// Builtins whose braced argument is data or an expression are excluded so
		// their contents are not misread as command calls.
		if !dataBraceCommands[w[0].Text] && braced(last) {
			return []Word{w[last]}
		}
	}
	return nil
}

// exprBodies returns the braced words a command evaluates as *expressions* (not
// scripts): every braced argument of `expr`, the conditions of if/elseif, the
// test of while, and the test clause of for. Tcl's expr engine evaluates
// [command substitutions] inside these braces even though braces normally
// suppress substitution, so the reference walker must scan them for embedded
// calls -- a proc called only inside `if {[ready]} { ... }` is otherwise
// invisible. This is the exact complement of scriptBodies/ifBodies for these
// commands: a braced if-word is either a condition (here) or a body (there),
// never both, so the two classifications cannot drift.
func exprBodies(w []Word) []Word {
	if len(w) == 0 || w[0].Kind != WordBare {
		return nil
	}
	braced := func(i int) bool { return i >= 0 && i < len(w) && w[i].Kind == WordBraced }
	switch w[0].Text {
	case "expr":
		var out []Word
		for _, x := range w[1:] {
			if x.Kind == WordBraced {
				out = append(out, x)
			}
		}
		return out
	case "while":
		if braced(1) { // while TEST body -- TEST is an expression.
			return []Word{w[1]}
		}
	case "for":
		if braced(2) { // for start TEST next body -- TEST is an expression.
			return []Word{w[2]}
		}
	case "if":
		return ifConditions(w)
	}
	return nil
}

// isIfCondition reports whether word i of the if command w is a condition: the word
// immediately following `if` or `elseif`.
func isIfCondition(w []Word, i int) bool {
	if i < 1 || i >= len(w) {
		return false
	}
	prev := w[i-1]
	return prev.Kind == WordBare && (prev.Text == "if" || prev.Text == "elseif")
}

// ifConditions returns the braced condition words of an if command: the words
// immediately following `if` or `elseif`. It is the exact complement of ifBodies,
// which returns every other braced word.
func ifConditions(w []Word) []Word {
	var conds []Word
	for i := 1; i < len(w); i++ {
		if w[i].Kind == WordBraced && isIfCondition(w, i) {
			conds = append(conds, w[i])
		}
	}
	return conds
}

// dataBraceCommands are builtins whose braced arguments hold data or an
// expression -- never a script body -- so they are excluded from the
// trailing-braced-argument script heuristic in scriptBodies.
var dataBraceCommands = map[string]bool{
	"expr": true, "set": true, "list": true, "lappend": true, "lset": true,
	"append": true, "incr": true, "array": true, "string": true,
	"global": true, "variable": true, "common": true, "return": true,
}

// ifBodies returns the braced body words of an if command, excluding the
// condition expressions. A braced word is a body unless it is the expression
// immediately following `if` or `elseif`.
func ifBodies(w []Word) []Word {
	var bodies []Word
	for i := 1; i < len(w); i++ {
		if w[i].Kind != WordBraced {
			continue
		}
		if isIfCondition(w, i) {
			continue // condition expression, not a body
		}
		bodies = append(bodies, w[i])
	}
	return bodies
}

// tryHandler is one complete `on`/`trap` clause of a try command. body is nil when
// the handler script is not braced. fallsThrough marks a `-` body: the clause runs
// the next clause's body. vars is nil when the command has an expanded word,
// because the clauses then cannot be told apart with certainty.
type tryHandler struct {
	vars         *Word
	body         *Word
	fallsThrough bool
}

// hasExpansion reports whether any word of a command is expanded with `{*}`, so its
// final word count and the role of each word are not known.
func hasExpansion(w []Word) bool {
	for i := 0; i+1 < len(w); i++ {
		if w[i].Kind == WordBraced && w[i].Text == "{*}" && w[i].End == w[i+1].Start {
			return true
		}
	}
	return false
}

// tryParts splits a try command's words by role. main is nil for `try $script ...`,
// so a handler is never mistaken for the main body. The scan steps by clause:
// `on CODE VARLIST BODY` and `trap PATTERN VARLIST BODY` are four words and
// `finally BODY` is two, so a variable list spelled `finally` does not start a
// clause. A clause cut short by the end of the command is an error and is dropped.
func tryParts(w []Word) (main *Word, handlers []tryHandler, finally *Word) {
	if len(w) >= 2 && w[1].Kind == WordBraced {
		main = &w[1]
	}
	expanded := hasExpansion(w)
	for i := 2; i < len(w); {
		clause := ""
		if w[i].Kind == WordBare {
			clause = w[i].Text
		}
		switch clause {
		case "on", "trap":
			if i+3 < len(w) {
				var h tryHandler
				if !expanded {
					h.vars = &w[i+2]
				}
				switch {
				case w[i+3].Kind == WordBraced:
					h.body = &w[i+3]
				case w[i+3].Kind == WordBare && w[i+3].Text == "-":
					h.fallsThrough = true
				}
				handlers = append(handlers, h)
			}
			i += 4
		case "finally":
			if i+1 < len(w) && w[i+1].Kind == WordBraced {
				finally = &w[i+1]
			}
			i += 2
		default:
			i++
		}
	}
	return main, handlers, finally
}

// tryBodies returns the braced body words of a try command: the main body, then
// the on/trap handler bodies and the finally body in source order.
func tryBodies(w []Word) []Word {
	main, handlers, finally := tryParts(w)
	var bodies []Word
	if main != nil {
		bodies = append(bodies, *main)
	}
	for _, h := range handlers {
		if h.body != nil {
			bodies = append(bodies, *h.body)
		}
	}
	if finally != nil {
		bodies = append(bodies, *finally)
	}
	slices.SortFunc(bodies, func(a, b Word) int { return a.Start - b.Start })
	return bodies
}

// switchSubjectIndex returns the index in w of a switch command's subject word:
// the first word after its options. It is len(w) or more when there is no subject.
func switchSubjectIndex(w []Word) int {
	for i := 1; i < len(w); i++ {
		if w[i].Kind != WordBare {
			return i
		}
		switch w[i].Text {
		case "--":
			return i + 1
		case "-exact", "-glob", "-regexp", "-nocase":
		case "-matchvar", "-indexvar":
			i++ // the option's variable name, whatever it looks like
		default:
			return i
		}
	}
	return len(w)
}

// switchArgs returns the words of a switch command after its options and subject,
// or nil if there are none.
func switchArgs(w []Word) []Word {
	armsStart := switchSubjectIndex(w) + 1
	if armsStart >= len(w) {
		return nil
	}
	return w[armsStart:]
}

// switchArmList returns the braced word holding a switch command's arms, when the
// arms are written as that single word (`switch $x {a {...} b {...}}`) rather than
// inline (`switch $x a {...} b {...}`).
func switchArmList(w []Word) (Word, bool) {
	if !isCmd(w, "switch") {
		return Word{}, false
	}
	args := switchArgs(w)
	if len(args) != 1 || args[0].Kind != WordBraced {
		return Word{}, false
	}
	return args[0], true
}

// switchArmListWords parses an arm list into one flat word list. Each word is
// shifted into the coordinates of the list's enclosing source, so bracedInner works
// on it with the same base as on the list itself.
func switchArmListWords(list Word) []Word {
	inner, interiorStart := bracedInner(list, 0)
	var words []Word
	for _, command := range Parse(inner) {
		for _, word := range command.Words {
			word.Start += interiorStart
			word.End += interiorStart
			words = append(words, word)
		}
	}
	return words
}

// switchBodies returns every braced arm body of a switch command, in source order.
// Arms are (pattern, body) pairs, and a pattern is never a script. A non-braced
// body (including the fall-through `-`) is skipped. An odd word count with a braced
// last word has lost its pairing, so no word can be called a body with certainty
// and the result is nil. An unbraced last word is a pattern still being typed: it
// is dropped and the complete pairs before it still count.
func switchBodies(w []Word) []Word {
	arms := switchArgs(w)
	if list, ok := switchArmList(w); ok {
		arms = switchArmListWords(list)
	}
	if len(arms)%2 == 1 && arms[len(arms)-1].Kind == WordBraced {
		return nil
	}
	var bodies []Word
	for i := 1; i < len(arms); i += 2 {
		if arms[i].Kind == WordBraced {
			bodies = append(bodies, arms[i])
		}
	}
	return bodies
}
