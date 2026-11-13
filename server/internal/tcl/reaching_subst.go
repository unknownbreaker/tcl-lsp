package tcl

import "strings"

// The reaching analyzer threads its set through a command's substitutions (see
// subst.go): a use inside a [script] sees the bindings made before it in the same
// script, and the script's bindings reach the rest of the command and the code
// after it. A script that may not run joins with the set before it instead of
// replacing it. A script that ends its path (return, break, continue) ends the
// command's path too.

// substitute threads in through the substitutions of c's words, left to right,
// recording the use at useOff when it is one of them. Words that deferred accepts
// are evaluated later by the caller (a loop test, an if condition). The
// variable-name arguments of info exists, unset and array are read last, by the
// command itself. live is false when a script ends the path.
func (a *analyzer) substitute(c Command, base int, in reachSet, deferred func(idx int) bool) (out reachSet, live bool) {
	exprWords := exprWordStarts(c)
	// The words of a multi-word expr form one expression, so an operator in one
	// makes a later word's scripts conditional.
	carry := isCmd(c.Words, "expr")
	cur, live, conditional := in, true, false
	for idx, w := range c.Words {
		if deferred(idx) {
			continue
		}
		cur, conditional = a.substituteWord(c, exprWords, idx, base, cur, conditional, &live)
		if !carry {
			conditional = false
		} else if w.Kind != WordBraced && strings.ContainsAny(w.Text, "&|?") {
			conditional = true
		}
	}
	for _, r := range nameArgRefs(c) {
		a.recordUseAt(r.Name, r.Start+base, r.End+base, cur)
	}
	return cur, live
}

// substituteWord threads in through the substitutions of word idx of c, setting
// *live to false when a script ends the path. conditional is as for walkSubst.
func (a *analyzer) substituteWord(c Command, exprWords map[int]bool, idx, base int, in reachSet, conditional bool, live *bool) (reachSet, bool) {
	cur := in
	conditional = walkWordSubst(c, exprWords, idx, base, conditional,
		func(r VarRef) { a.recordUseAt(r.Name, r.Start, r.End, cur) },
		func(inner string, innerBase int, mayNotRun bool) {
			out, scriptLive := a.seq(Parse(inner), innerBase, cur)
			switch {
			case mayNotRun && scriptLive:
				cur = joinAll([]reachSet{cur, out})
			case mayNotRun:
				// The path that runs the script ends; the one that skips it goes on.
			case !scriptLive:
				*live = false
			default:
				cur = out
			}
		})
	return cur, conditional
}

// recordUseAt records the reaching set for name when the use at useOff lies in the
// reference [start, end).
func (a *analyzer) recordUseAt(name string, start, end int, in reachSet) {
	if a.useOff >= start && a.useOff < end {
		a.recordAnswer(name, in[name])
	}
}

// collectSubstBindings unions the bindings made in every [script] substituted in
// c's words, including expression words, which may be evaluated.
func (a *analyzer) collectSubstBindings(c Command, base int, into reachSet, follow func(Command) bool) {
	exprWords := exprWordStarts(c)
	for idx := range c.Words {
		walkWordSubst(c, exprWords, idx, base, false, func(VarRef) {}, func(inner string, innerBase int, _ bool) {
			a.collectBindings(Parse(inner), innerBase, into, follow)
		})
	}
}
