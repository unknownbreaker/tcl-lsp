package tcl

import "hash/fnv"

// reachSet maps a local variable name to the bindings that may currently reach
// this program point. It is persistent: with and joinSets return a new set and
// never change their inputs, sharing every subtree they leave alone, so a binding
// costs one path of the trie, not a copy of the set. The zero value is empty.
// A join that adds nothing to one side returns that side itself, so the loop
// fixpoint usually sees convergence as pointer equality. A name's bindings are a
// defList, persistent too, so a join that adds one binding to a long list costs
// a path of its key trie, not a copy of the list.
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
	defs defList
}

func reachHash(name string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return h.Sum32()
}

func reachSlot(h uint32, depth int) int {
	return int(h>>(depth*reachBits)) & (reachWidth - 1)
}

// get returns the bindings of name, or the empty list.
func (s reachSet) get(name string) defList { return getAt(s.root, name, 0) }

// getAt returns the bindings of name in the subtree n at depth d.
func getAt(n *reachNode, name string, d int) defList {
	h := reachHash(name)
	for ; n != nil; d++ {
		if n.entries != nil {
			for _, e := range n.entries {
				if e.name == name {
					return e.defs
				}
			}
			return defList{}
		}
		n = n.kids[reachSlot(h, d)]
	}
	return defList{}
}

// with returns s with name bound to exactly defs.
func (s reachSet) with(name string, defs defList) reachSet {
	e := reachEntry{hash: reachHash(name), name: name, defs: defs}
	return reachSet{upsert(s.root, e, 0, func(defList, bool) defList { return defs })}
}

// withAdded returns s with d joined to the bindings of its name.
func (s reachSet) withAdded(d Definition) reachSet {
	return s.with(d.Name, unionDefs(s.get(d.Name), defsOf(d)))
}

// each calls f for every name in s, in no particular order.
func (s reachSet) each(f func(name string, defs defList)) {
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
// list itself, n is returned unchanged.
func upsert(n *reachNode, e reachEntry, d int, merge func(old defList, ok bool) defList) *reachNode {
	if n == nil {
		e.defs = merge(defList{}, false)
		return &reachNode{entries: []reachEntry{e}}
	}
	if n.entries != nil {
		for i, old := range n.entries {
			if old.name != e.name {
				continue
			}
			defs := merge(old.defs, true)
			if defs.same(old.defs) {
				return n
			}
			entries := append([]reachEntry(nil), n.entries...)
			entries[i].defs = defs
			return &reachNode{entries: entries}
		}
		if d == reachMaxDepth || n.entries[0].hash == e.hash {
			e.defs = merge(defList{}, false)
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
// adds nothing to it, and b when the union equals b.
func joinNodes(a, b *reachNode, d int) *reachNode {
	switch {
	case a == b || b == nil:
		return a
	case a == nil:
		return b
	case b.entries != nil:
		r := a
		for _, e := range b.entries {
			r = upsert(r, e, d, func(old defList, ok bool) defList {
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
			r = upsert(r, e, d, func(old defList, ok bool) defList {
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

// sameEntries reports whether leaves r and b hold the same names bound to the
// same lists.
func sameEntries(r, b *reachNode) bool {
	if r.entries == nil || len(r.entries) != len(b.entries) {
		return false
	}
	for _, e := range r.entries {
		found := false
		for _, f := range b.entries {
			if e.name == f.name {
				found = e.defs.same(f.defs)
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// reachEqual reports whether two reachSets contain the same bindings (by
// variable name and NameStart/NameEnd identity). Sets built from one another share
// every subtree a change left alone, so it compares by node and skips shared ones.
func reachEqual(a, b reachSet) bool { return nodesEqual(a.root, b.root, 0) }

// nodesEqual reports whether subtrees a and b, both at depth d, hold the same
// names bound to the same sites.
func nodesEqual(a, b *reachNode, d int) bool {
	if a == b {
		return true
	}
	if a != nil && b != nil && a.entries == nil && b.entries == nil {
		for i := range a.kids {
			if !nodesEqual(a.kids[i], b.kids[i], d+1) {
				return false
			}
		}
		return true
	}
	// A leaf against a subtree: the same names may sit in either shape. A name bound
	// to no sites counts as absent.
	na, nb := 0, 0
	reachSet{b}.each(func(_ string, db defList) {
		if db.len() > 0 {
			nb++
		}
	})
	equal := true
	reachSet{a}.each(func(name string, da defList) {
		if da.len() > 0 {
			na++
		}
		if equal {
			equal = da.sameSites(getAt(b, name, d))
		}
	})
	return equal && na == nb
}
