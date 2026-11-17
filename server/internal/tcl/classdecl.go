package tcl

// FileClasses parses src and returns per-class inherit edges, keyed by the
// fully-qualified class name. Only classes that declare at least one `inherit`
// command appear in the map. The values are slices of fully-qualified base class
// names, in declaration order.
func FileClasses(src string) map[string][]string {
	m := map[string][]string{}
	walkAll(Parse(src), 0, "::", FrameNamespace, 0, "", false, false, collectors{classes: m})
	return m
}

// recordInherit records `inherit Base1 Base2 ...` inside a class body.
func recordInherit(c Command, ns string, class string, m map[string][]string) {
	w := c.Words
	if !isCmd(w, "inherit") || len(w) < 2 {
		return
	}
	for _, pw := range w[1:] {
		baseName := unbrace(pw.Text)
		if baseName == "" {
			continue
		}
		baseFQ := qualifyName(baseName, ns)
		// Dedup within this call (the index deduplicates across files).
		already := false
		for _, existing := range m[class] {
			if existing == baseFQ {
				already = true
				break
			}
		}
		if !already {
			m[class] = append(m[class], baseFQ)
		}
	}
}
