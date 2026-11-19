package tcl

import "unsafe"

// Script ends found once. Finding where a [script] ends reads every script nested
// in it, and each nested script is then parsed and walked in turn, which finds
// the ends inside it again: n levels of `[list a [list a ...]]` cost n² (#84).
// A scriptEnds remembers each end it finds, so a walk that threads one through
// the scanner and the substitution walk reads each byte a bounded number of times.
//
// An entry is keyed by the address of the script's first interior byte, not by an
// offset, so it can never answer for different text: Go strings are immutable,
// and the entry keeps the string it was found in alive, so that memory is not
// reused while the memo lives. A memo serves one walk on one goroutine and is
// dropped with it. A nil *scriptEnds remembers nothing; a zero one is ready.
//
// scriptClose(s, i) depends only on the bytes from i to the end of s, and a
// shorter s can only fail sooner: an end found at e answers any s that holds
// the byte at e, and is "none" for one that stops before it. A failure answers
// any s that stops no later than the one that failed.
type scriptEnds struct {
	ends map[uintptr]scriptEnd
}

type scriptEnd struct {
	keep  string  // the text the end was found in, held so its memory stays put
	at    uintptr // address of the closing `]`, or of the end of keep on failure
	found bool
}

// addr returns the address of s[i]; i < len(s).
func addr(s string, i int) uintptr {
	return uintptr(unsafe.Pointer(unsafe.StringData(s))) + uintptr(i)
}

// lookup returns the remembered answer for scriptClose(s, i), and false when
// there is none that applies.
func (m *scriptEnds) lookup(s string, i int) (end int, found, hit bool) {
	if m == nil || i >= len(s) {
		return 0, false, false
	}
	e, ok := m.ends[addr(s, i)]
	if !ok {
		return 0, false, false
	}
	start := addr(s, 0)
	limit := start + uintptr(len(s))
	switch {
	case e.found && e.at < limit:
		return int(e.at - start), true, true
	case e.found:
		return 0, false, true // s stops before the `]` that closes it
	case limit <= e.at:
		return 0, false, true // a longer text already failed
	}
	return 0, false, false
}

// store remembers the answer scriptClose(s, i) returned.
func (m *scriptEnds) store(s string, i, end int, found bool) {
	if m == nil || i >= len(s) {
		return
	}
	if m.ends == nil {
		m.ends = map[uintptr]scriptEnd{}
	}
	e := scriptEnd{keep: s, found: found}
	if found {
		e.at = addr(s, end)
	} else {
		e.at = addr(s, 0) + uintptr(len(s))
	}
	m.ends[addr(s, i)] = e
}
