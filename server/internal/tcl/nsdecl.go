package tcl

// NamespaceInfo aggregates the resolution-affecting declarations of one
// namespace. Path is the command search path (set by `namespace path`); Exports
// are export glob patterns; Imports are qualified source patterns from
// `namespace import`.
type NamespaceInfo struct {
	Name    string
	Path    []string
	Exports []string
	Imports []string
}

// FileNamespaces parses src and returns per-namespace declarations, keyed by the
// fully-qualified namespace name. Only namespaces with at least one declaration
// appear in the map.
func FileNamespaces(src string) map[string]*NamespaceInfo {
	m := map[string]*NamespaceInfo{}
	walkFile(src, collectors{ns: m})
	return m
}

func recordNSDecl(c Command, ns string, m map[string]*NamespaceInfo) {
	w := c.Words
	if !isCmd(w, "namespace") || len(w) < 3 || w[1].Kind != WordBare {
		return
	}
	switch w[1].Text {
	case "export":
		info := ensureNS(m, ns)
		for _, pw := range w[2:] {
			if pw.Text == "" || pw.Text[0] == '-' {
				continue // skip flags like -noclash and empty words
			}
			info.Exports = append(info.Exports, unbrace(pw.Text))
		}
	case "import":
		info := ensureNS(m, ns)
		for _, pw := range w[2:] {
			if pw.Text == "" || pw.Text[0] == '-' {
				continue // skip flags like -force
			}
			info.Imports = append(info.Imports, qualifyNamespace(unbrace(pw.Text), ns))
		}
	case "path":
		info := ensureNS(m, ns)
		info.Path = parsePathList(w[2], ns) // `namespace path` sets (replaces) the path
	}
}

func ensureNS(m map[string]*NamespaceInfo, ns string) *NamespaceInfo {
	if m[ns] == nil {
		m[ns] = &NamespaceInfo{Name: ns}
	}
	return m[ns]
}

// unbrace strips a single layer of surrounding braces, if present.
func unbrace(s string) string {
	if len(s) >= 2 && s[0] == '{' && s[len(s)-1] == '}' {
		return s[1 : len(s)-1]
	}
	return s
}

// parsePathList resolves the entries of a `namespace path` list argument
// (braced `{a b}` or a single name) into qualified namespace names.
func parsePathList(w Word, ns string) []string {
	var out []string
	for _, c := range Parse(unbrace(w.Text)) {
		for _, word := range c.Words {
			// Outer unbrace (on w.Text) stripped the list braces; this inner
			// unbrace handles a list element that is itself braced, e.g.
			// `namespace path {{::a} ::b}`.
			name := unbrace(word.Text)
			if name != "" {
				out = append(out, qualifyNamespace(name, ns))
			}
		}
	}
	return out
}
