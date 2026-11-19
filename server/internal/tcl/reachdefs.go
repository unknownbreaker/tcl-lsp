package tcl

// defList is the ordered bindings of one name, deduplicated by name-range
// (NameStart/NameEnd), the first copy of a site winning. It is persistent and
// never written once built: unionDefs adds a node over its two inputs and a trie
// of their keys, so joining one binding onto a list of n costs O(log n), not a
// copy of n. The order is read only when slice walks the nodes. The zero value
// is empty.
type defList struct{ n *defNode }

// defNode is a leaf (defs, deduplicated) or the union of head then tail. keys
// holds every site in the list either way.
type defNode struct {
	keys       *keyNode
	defs       []Definition
	head, tail *defNode
}

// defKey identifies a binding site by its name's range.
type defKey struct{ start, end int }

func keyOf(d Definition) defKey { return defKey{d.NameStart, d.NameEnd} }

// defsOf returns the list of ds, keeping the first copy of each site.
func defsOf(ds ...Definition) defList {
	if len(ds) == 0 {
		return defList{}
	}
	n := &defNode{}
	for _, d := range ds {
		k := keyOf(d)
		if keys := keyInsert(n.keys, hashedKey{keyHash(k), k}, 0); keys != n.keys {
			n.keys = keys
			n.defs = append(n.defs, d)
		}
	}
	return defList{n}
}

// same reports whether l and m are the same list, not merely equal ones.
func (l defList) same(m defList) bool { return l.n == m.n }

// sameSites reports whether l and m hold the same sites, in any order.
func (l defList) sameSites(m defList) bool {
	if l.n == nil || m.n == nil {
		return l.n == m.n
	}
	return l.len() == m.len() && keySubset(l.n.keys, m.n.keys, 0)
}

// len returns the number of sites in l.
func (l defList) len() int {
	if l.n == nil {
		return 0
	}
	return l.n.keys.count
}

// slice returns l's bindings in order. The caller must not write to it: a leaf's
// own slice may be returned.
func (l defList) slice() []Definition {
	if l.n == nil {
		return nil
	}
	if l.n.head == nil {
		return l.n.defs
	}
	out := make([]Definition, 0, l.len())
	seen := make(map[defKey]bool, l.len())
	visited := map[*defNode]bool{}
	// A node already walked has had every site it holds emitted, so it is skipped.
	var walk func(n *defNode)
	walk = func(n *defNode) {
		if n == nil || visited[n] || len(out) == cap(out) {
			return
		}
		visited[n] = true
		if n.head == nil {
			for _, d := range n.defs {
				if k := keyOf(d); !seen[k] {
					seen[k] = true
					out = append(out, d)
				}
			}
			return
		}
		walk(n.head)
		walk(n.tail)
	}
	walk(l.n)
	return out
}

// unionDefs returns x followed by the bindings of y not in x. It returns x itself
// when y adds nothing, and y itself when y already starts with x.
func unionDefs(x, y defList) defList {
	switch {
	case x.n == y.n || y.n == nil:
		return x
	case x.n == nil || y.n.head == x.n || leafPrefix(x.n, y.n):
		return y
	}
	keys := keyUnion(x.n.keys, y.n.keys, 0)
	if keys == x.n.keys {
		return x
	}
	return defList{&defNode{keys: keys, head: x.n, tail: y.n}}
}

// leafPrefix reports whether leaves x and y hold the same bindings y starts
// with. It compares whole values, not keys: one site may be carried as two
// Definitions (a try handler's variable has two Namespace spellings), and x's copy
// must win. Returning y then keeps its identity, so a join of equal leaves built
// twice returns the right-hand one, as joinNodes expects.
func leafPrefix(x, y *defNode) bool {
	if x.head != nil || y.head != nil || len(x.defs) > len(y.defs) {
		return false
	}
	for i := range x.defs {
		if x.defs[i] != y.defs[i] {
			return false
		}
	}
	return true
}

// keyNode is a persistent hash trie of sites, shaped like reachNode: an inner
// node indexed by reachBits of the key's hash per level, or a leaf holding keys.
// count is the number of keys in the subtree.
type keyNode struct {
	kids  [reachWidth]*keyNode
	keys  []hashedKey
	count int
}

// hashedKey is a site with its keyHash, computed once.
type hashedKey struct {
	hash uint32
	key  defKey
}

// keyHash mixes both ends of a site's range into 32 bits.
func keyHash(k defKey) uint32 {
	h := uint64(k.start)*0x9E3779B97F4A7C15 ^ uint64(k.end)*0xC2B2AE3D27D4EB4F
	return uint32(h ^ h>>32)
}

func keyCount(n *keyNode) int {
	if n == nil {
		return 0
	}
	return n.count
}

// keyInsert returns n's subtree at depth d with k added, or n itself when it
// already holds k.
func keyInsert(n *keyNode, k hashedKey, d int) *keyNode {
	if n == nil {
		return &keyNode{keys: []hashedKey{k}, count: 1}
	}
	if n.keys != nil {
		for _, o := range n.keys {
			if o.key == k.key {
				return n
			}
		}
		if d == reachMaxDepth || n.keys[0].hash == k.hash {
			keys := append(append([]hashedKey(nil), n.keys...), k)
			return &keyNode{keys: keys, count: len(keys)}
		}
		// Push the leaf down a level, where the hashes may part.
		inner := &keyNode{count: n.count}
		inner.kids[reachSlot(n.keys[0].hash, d)] = n
		return keyInsert(inner, k, d)
	}
	i := reachSlot(k.hash, d)
	kid := keyInsert(n.kids[i], k, d+1)
	if kid == n.kids[i] {
		return n
	}
	cp := *n
	cp.kids[i] = kid
	cp.count++
	return &cp
}

// keyUnion returns the union of a and b at depth d: a itself when b adds
// nothing, b itself when a adds nothing.
func keyUnion(a, b *keyNode, d int) *keyNode {
	switch {
	case a == b || b == nil:
		return a
	case a == nil:
		return b
	}
	var r *keyNode
	switch {
	case b.keys != nil:
		r = a
		for _, k := range b.keys {
			r = keyInsert(r, k, d)
		}
	case a.keys != nil:
		r = b
		for _, k := range a.keys {
			r = keyInsert(r, k, d)
		}
	default:
		out := keyNode{}
		for i := range a.kids {
			out.kids[i] = keyUnion(a.kids[i], b.kids[i], d+1)
			out.count += keyCount(out.kids[i])
		}
		r = &out
	}
	// r holds both, so a count equal to one side's means the same set.
	switch r.count {
	case a.count:
		return a
	case b.count:
		return b
	}
	return r
}

// keySubset reports whether every key of a at depth d is in b.
func keySubset(a, b *keyNode, d int) bool {
	switch {
	case a == nil || a == b:
		return true
	case b == nil || a.count > b.count:
		return false
	case a.keys != nil:
		for _, k := range a.keys {
			if !keyHas(b, k, d) {
				return false
			}
		}
		return true
	case b.keys != nil:
		ok := true
		keyEach(a, func(k hashedKey) { ok = ok && keyHas(b, k, d) })
		return ok
	}
	for i := range a.kids {
		if !keySubset(a.kids[i], b.kids[i], d+1) {
			return false
		}
	}
	return true
}

// keyHas reports whether n's subtree at depth d holds k.
func keyHas(n *keyNode, k hashedKey, d int) bool {
	for ; n != nil; d++ {
		if n.keys != nil {
			for _, o := range n.keys {
				if o.key == k.key {
					return true
				}
			}
			return false
		}
		n = n.kids[reachSlot(k.hash, d)]
	}
	return false
}

// keyEach calls f for every key in n's subtree, in no particular order.
func keyEach(n *keyNode, f func(hashedKey)) {
	if n == nil {
		return
	}
	for _, k := range n.keys {
		f(k)
	}
	for _, kid := range n.kids {
		keyEach(kid, f)
	}
}
