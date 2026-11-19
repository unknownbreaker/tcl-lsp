package tcl

import "hash/fnv"

// reachSet maps a local variable name to the bindings that may currently reach
// this program point. It is persistent: with and joinSets return a new set and
// never change their inputs, sharing every subtree they leave alone, so a binding
// costs one path of the trie, not a copy of the set. The zero value is empty.
// A join that adds nothing to one side returns that side itself, so the loop
// fixpoint usually sees convergence as pointer equality.
//
// A defs slice in a set is never written after it is stored; appendDedup and
// unionDefs always allocate when they add.
type reachSet struct{ root *reachNode }

const (
	reachBits     = 4
	reachWidth    = 1 << reachBits
	reachMaxDepth = 32 / reachBits
)

// reachNode is an inner node (entries nil) indexed by reachBits of the name's
// hash per level, or a leaf holding entries. A leaf may sit at any depth; it holds
// more than one entry only for names whose hashes are equal, or at reachMaxDepth.
type reachNode struct {
	kids    [reachWidth]*reachNode
	entries []reachEntry
}

type reachEntry struct {
	hash uint32
	name string
	defs []Definition
}

func reachHash(name string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return h.Sum32()
}

func reachSlot(h uint32, depth int) int {
	return int(h>>(depth*reachBits)) & (reachWidth - 1)
}

// get returns the bindings of name, or nil.
func (s reachSet) get(name string) []Definition {
	h := reachHash(name)
	n := s.root
	for d := 0; n != nil; d++ {
		if n.entries != nil {
			for _, e := range n.entries {
				if e.name == name {
					return e.defs
				}
			}
			return nil
		}
		n = n.kids[reachSlot(h, d)]
	}
	return nil
}

// with returns s with name bound to exactly defs.
func (s reachSet) with(name string, defs []Definition) reachSet {
	e := reachEntry{hash: reachHash(name), name: name, defs: defs}
	return reachSet{upsert(s.root, e, 0, func(_ []Definition, _ bool) []Definition { return defs })}
}

// each calls f for every name in s, in no particular order.
func (s reachSet) each(f func(name string, defs []Definition)) {
	var walk func(n *reachNode)
	walk = func(n *reachNode) {
		if n == nil {
			return
		}
		for _, e := range n.entries {
			f(e.name, e.defs)
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(s.root)
}

// upsert stores e.name in n's subtree at depth d, with the defs merge returns from
// the stored defs (ok false when there are none). When merge returns the stored
// slice itself, n is returned unchanged.
func upsert(n *reachNode, e reachEntry, d int, merge func(old []Definition, ok bool) []Definition) *reachNode {
	if n == nil {
		e.defs = merge(nil, false)
		return &reachNode{entries: []reachEntry{e}}
	}
	if n.entries != nil {
		for i, old := range n.entries {
			if old.name != e.name {
				continue
			}
			defs := merge(old.defs, true)
			if sameSlice(defs, old.defs) {
				return n
			}
			entries := append([]reachEntry(nil), n.entries...)
			entries[i].defs = defs
			return &reachNode{entries: entries}
		}
		if d == reachMaxDepth || n.entries[0].hash == e.hash {
			e.defs = merge(nil, false)
			entries := append(append([]reachEntry(nil), n.entries...), e)
			return &reachNode{entries: entries}
		}
		// Push the leaf down a level, where the hashes may part.
		inner := &reachNode{}
		inner.kids[reachSlot(n.entries[0].hash, d)] = n
		return upsert(inner, e, d, merge)
	}
	i := reachSlot(e.hash, d)
	kid := upsert(n.kids[i], e, d+1, merge)
	if kid == n.kids[i] {
		return n
	}
	cp := *n
	cp.kids[i] = kid
	return &cp
}

// joinSets unions the sets in order, per variable, deduplicating bindings by
// name-range (NameStart/NameEnd): a name's bindings come in the order the sets
// first hold them.
func joinSets(sets []reachSet) reachSet {
	var out reachSet
	for i, s := range sets {
		if i == 0 {
			out = s
			continue
		}
		out = reachSet{joinNodes(out.root, s.root, 0)}
	}
	return out
}

// joinNodes unions a and b at depth d, a's bindings first. It returns a when b
// adds nothing to it, and b when a is a prefix of it.
func joinNodes(a, b *reachNode, d int) *reachNode {
	switch {
	case a == b || b == nil:
		return a
	case a == nil:
		return b
	case b.entries != nil:
		r := a
		for _, e := range b.entries {
			r = upsert(r, e, d, func(old []Definition, ok bool) []Definition {
				if !ok {
					return e.defs
				}
				return unionDefs(old, e.defs)
			})
		}
		if r != a && a.entries != nil && sameEntries(r, b) {
			return b // keeps b's identity, so a later join with b's descendants stays cheap
		}
		return r
	case a.entries != nil:
		r := b
		for _, e := range a.entries {
			r = upsert(r, e, d, func(old []Definition, ok bool) []Definition {
				if !ok {
					return e.defs
				}
				return unionDefs(e.defs, old)
			})
		}
		return r
	}
	var out reachNode
	sameA, sameB := true, true
	for i := range a.kids {
		k := joinNodes(a.kids[i], b.kids[i], d+1)
		out.kids[i] = k
		sameA = sameA && k == a.kids[i]
		sameB = sameB && k == b.kids[i]
	}
	if sameA {
		return a
	}
	if sameB {
		return b
	}
	return &out
}

type defKey struct{ s, e int }

// unionDefs returns x followed by the bindings of y not in x. It returns x itself
// when y adds nothing, and y itself when x is a prefix of y.
func unionDefs(x, y []Definition) []Definition {
	if sameSlice(x, y) {
		return x
	}
	if len(x) <= len(y) {
		prefix := true
		// Whole values, not keys: one site may be carried as two Definitions (a try
		// handler's variable has two Namespace spellings), and x's copy must win.
		for i := range x {
			if x[i] != y[i] {
				prefix = false
				break
			}
		}
		if prefix {
			return y
		}
	}
	// A set's slice holds no duplicates, so only x's keys need looking up; a short
	// x is scanned, which keeps a one-binding arm joining a long list cheap.
	inX := func(d Definition) bool {
		for _, e := range x {
			if e.NameStart == d.NameStart && e.NameEnd == d.NameEnd {
				return true
			}
		}
		return false
	}
	if len(x) > 8 {
		seen := make(map[defKey]bool, len(x))
		for _, d := range x {
			seen[defKey{d.NameStart, d.NameEnd}] = true
		}
		inX = func(d Definition) bool { return seen[defKey{d.NameStart, d.NameEnd}] }
	}
	var out []Definition
	for _, d := range y {
		if !inX(d) {
			if out == nil {
				out = append(make([]Definition, 0, len(x)+len(y)), x...)
			}
			out = append(out, d)
		}
	}
	if out == nil {
		return x
	}
	return out
}

// sameEntries reports whether leaves r and b hold the same names bound to the
// same slices.
func sameEntries(r, b *reachNode) bool {
	if r.entries == nil || len(r.entries) != len(b.entries) {
		return false
	}
	for _, e := range r.entries {
		found := false
		for _, f := range b.entries {
			if e.name == f.name {
				found = sameSlice(e.defs, f.defs)
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sameSlice reports whether x and y are the same slice (same start and length).
func sameSlice(x, y []Definition) bool {
	return len(x) == len(y) && (len(x) == 0 || &x[0] == &y[0])
}

// reachEqual reports whether two reachSets contain the same bindings (by
// variable name and NameStart/NameEnd identity).
func reachEqual(a, b reachSet) bool {
	if a.root == b.root {
		return true
	}
	na, nb := 0, 0
	b.each(func(string, []Definition) { nb++ })
	equal := true
	a.each(func(name string, da []Definition) {
		na++
		if !equal {
			return
		}
		db := b.get(name)
		if db == nil || len(da) != len(db) {
			equal = false
			return
		}
		seen := make(map[defKey]bool, len(da))
		for _, d := range da {
			seen[defKey{d.NameStart, d.NameEnd}] = true
		}
		for _, d := range db {
			if !seen[defKey{d.NameStart, d.NameEnd}] {
				equal = false
				return
			}
		}
	})
	return equal && na == nb
}
