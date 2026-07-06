// Package envfile parses the tcl-lsp environment artifact (.tcl-lsp.env)
// produced by tools/extract.tcl. The artifact carries facts a live tclsh
// extracted that the static index cannot see: the source files external
// packages load (indexed for real definitions) and the commands those packages
// provide (declared names -- including C commands and runtime-generated procs).
//
// The format is line-based, tab-separated, tolerant: unknown or malformed
// lines are skipped, so the artifact can grow fields without breaking older
// servers. See tools/extract.tcl for the emitter.
package envfile

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// Env is a parsed environment artifact.
type Env struct {
	IndexFiles []string // absolute paths of package sources to index
	Commands   []string // package-provided command names (declared; no location)
	Builtins   []string // baseline interpreter commands (reserved; unused today)
}

// Parse reads the artifact format. It never fails on content: unknown record
// types, missing fields, and comment/blank lines are skipped. Only a read
// error is returned.
func Parse(r io.Reader) (Env, error) {
	var env Env
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[1] == "" {
			continue
		}
		switch fields[0] {
		case "index_file":
			env.IndexFiles = append(env.IndexFiles, fields[1])
		case "command":
			env.Commands = append(env.Commands, fields[1])
		case "builtin":
			env.Builtins = append(env.Builtins, fields[1])
		}
	}
	return env, sc.Err()
}

// Load parses the artifact at path. A missing file is not an error -- the
// environment file is optional -- so Load returns an empty Env and ok=false.
func Load(path string) (Env, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Env{}, false, nil
		}
		return Env{}, false, err
	}
	defer f.Close()
	env, perr := Parse(f)
	return env, perr == nil, perr
}
