package tcl

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
	cur, live := in, true
	// expr substitutes its other words before it evaluates the joined expression,
	// so those run first, and the braced words then run as one expression.
	isExpr := isCmd(c.Words, "expr")
	if isExpr {
		for idx, w := range c.Words {
			if !exprWords[w.Start] && !deferred(idx) {
				cur, _ = a.substituteWord(c, exprWords, idx, base, cur, flowAlways, &live)
			}
		}
	}
	flow := flowAlways
	for idx, w := range c.Words {
		if deferred(idx) {
			continue
		}
		if !isExpr || exprWords[w.Start] {
			cur, flow = a.substituteWord(c, exprWords, idx, base, cur, flow, &live)
		}
		flow = carryFlow(c, w, flow)
	}
	for _, r := range nameArgRefs(c) {
		a.recordUseAt(r.Name, r.Start+base, r.End+base, cur)
	}
	return cur, live
}

// substituteWord threads in through the substitutions of word idx of c, setting
// *live to false when a script ends the path. flow is as for walkSubst.
func (a *analyzer) substituteWord(c Command, exprWords map[int]bool, idx, base int, in reachSet, flow exprFlow, live *bool) (reachSet, exprFlow) {
	if all, _, ok := a.evalExprWord(c, exprWords, idx, base, in); ok {
		if !all.live {
			if flow < flowConditional {
				*live = false
			}
			return in, flow // the path that runs the expression ends; one that skips it goes on
		}
		if flow >= flowConditional {
			return joinSets([]reachSet{in, all.set}), flow
		}
		return all.set, flow
	}
	cur := in
	flow = walkWordSubst(c, exprWords, idx, base, flow, &a.ends,
		func(r VarRef) { a.recordUseAt(r.Name, r.Start, r.End, cur) },
		func(inner string, innerBase int, mayNotRun bool) {
			out, scriptLive := a.seq(a.parse(inner), innerBase, cur)
			switch {
			case mayNotRun && scriptLive:
				cur = joinSets([]reachSet{cur, out})
			case mayNotRun:
				// The path that runs the script ends; the one that skips it goes on.
			case !scriptLive:
				*live = false
			default:
				cur = out
			}
		})
	a.distrust(base+c.Words[idx].Start, flow)
	return cur, flow
}

// recordUseAt records the reaching set for name when the use at useOff lies in the
// reference [start, end).
func (a *analyzer) recordUseAt(name string, start, end int, in reachSet) {
	if a.useOff >= start && a.useOff < end {
		a.recordAnswer(name, in.get(name))
	}
}

// collectSubstBindings unions the bindings made in every [script] substituted in
// c's words, including expression words, which may be evaluated.
func (a *analyzer) collectSubstBindings(c Command, base int, into *reachSet, every bool) {
	exprWords := exprWordStarts(c)
	for idx := range c.Words {
		flow := walkWordSubst(c, exprWords, idx, base, flowAlways, &a.ends, func(VarRef) {}, func(inner string, innerBase int, _ bool) {
			a.collectScript(inner, innerBase, into, every)
		})
		a.distrust(base+c.Words[idx].Start, flow)
	}
}

// condOut is the reach set where some paths end; live is false when none get there.
type condOut struct {
	set  reachSet
	live bool
}

// joinOuts joins the paths that get there.
func joinOuts(outs ...condOut) condOut {
	var sets []reachSet
	for _, o := range outs {
		if o.live {
			sets = append(sets, o.set)
		}
	}
	if len(sets) == 0 {
		return condOut{set: reachSet{}}
	}
	return condOut{set: joinSets(sets), live: true}
}

// evalExprWord threads in through word idx of c when it is a braced expression
// that parses (exprTreeOf). all is where the expression ends; whenTrue is where it
// ends true, the set an if or loop body sees. ok is false when the word is not
// such an expression, and the caller walks it flat.
func (a *analyzer) evalExprWord(c Command, exprWords map[int]bool, idx, base int, in reachSet) (all, whenTrue condOut, ok bool) {
	w := c.Words[idx]
	if w.Kind != WordBraced || !exprWords[w.Start] {
		return condOut{}, condOut{}, false
	}
	t, ok := exprTreeOf(c, w, base, &a.ends)
	if !ok {
		return condOut{}, condOut{}, false
	}
	all, whenTrue = a.evalTern(t, condOut{set: in, live: true})
	return all, whenTrue, true
}

// evalTern evaluates t from in. A false result is not tracked apart from all,
// which includes it, so a set after a false operand may over-include.
func (a *analyzer) evalTern(t *exprTern, in condOut) (all, whenTrue condOut) {
	all, whenTrue = a.evalOr(t.cond, in)
	if t.then == nil {
		return all, whenTrue
	}
	thenAll, thenTrue := a.evalTern(t.then, whenTrue)
	elsAll, elsTrue := a.evalTern(t.els, all)
	return joinOuts(thenAll, elsAll), joinOuts(thenTrue, elsTrue)
}

// evalOr evaluates an || chain: each operand runs after the earlier ones ran false.
func (a *analyzer) evalOr(ops [][]exprSeq, in condOut) (all, whenTrue condOut) {
	var alls, trues []condOut
	cur := in
	for _, op := range ops {
		opAll, opTrue := a.evalAnd(op, cur)
		alls, trues = append(alls, opAll), append(trues, opTrue)
		cur = opAll
	}
	if len(ops) == 1 {
		return alls[0], trues[0]
	}
	return joinOuts(alls...), joinOuts(trues...)
}

// evalAnd evaluates an && chain: each operand runs after the earlier ones ran true.
func (a *analyzer) evalAnd(ops []exprSeq, in condOut) (all, whenTrue condOut) {
	var alls []condOut
	cur := in
	for _, op := range ops {
		opAll, opTrue := a.evalSeq(op, cur)
		alls = append(alls, opAll)
		cur = opTrue
	}
	if len(ops) == 1 {
		return alls[0], cur
	}
	return joinOuts(alls...), cur
}

// evalSeq evaluates one operand's substitutions left to right.
func (a *analyzer) evalSeq(s exprSeq, in condOut) (all, whenTrue condOut) {
	if !in.live {
		return in, in
	}
	cur := in.set
	var groupTrue condOut
	for _, it := range s.items {
		switch {
		case it.isVar:
			a.recordUseAt(it.ref.Name, it.ref.Start, it.ref.End, cur)
		case it.isScript:
			out, live := a.seq(a.parse(it.inner), it.innerBase, cur)
			if !live {
				dead := condOut{set: reachSet{}}
				return dead, dead
			}
			cur = out
		case it.isGroup:
			for _, g := range it.group {
				gAll, gTrue := a.evalTern(g, condOut{set: cur, live: true})
				if !gAll.live {
					dead := condOut{set: reachSet{}}
					return dead, dead
				}
				cur, groupTrue = gAll.set, gTrue
			}
		}
	}
	all = condOut{set: cur, live: true}
	if s.pure {
		return all, groupTrue
	}
	return all, all
}
