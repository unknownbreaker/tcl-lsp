// Package checkcmd is the `tcl-lsp check` subcommand: it runs the Checks
// (package check) over files and directories without starting LSP, for
// pre-commit hooks and CI.
package checkcmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/check"
	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// Exit statuses. A warning fails the run; a hint alone does not.
const (
	exitClean    = 0
	exitWarnings = 1
	exitUsage    = 2
)

const usage = "usage: tcl-lsp check [--disable check-id[,check-id...]]... [--] <file or directory>...\n"

// Run checks the files and directories named by args and prints one line per
// finding to stdout: `file:line:col: severity check-id message`, with a 1-based
// line and byte column. A directory is walked for .tcl and .rvt files (skipping
// .git); a file named directly is checked whatever its extension, as .rvt when it
// has that one. Every file is indexed before any finding is reported, so a
// builtin one file redefines silences the Checks that assume it in all of them.
// Leading `--disable ids` or `--disable=ids` flags, each a comma-separated list
// of Check ids (spaces around an id are dropped), turn those Checks off: their
// findings are neither printed nor counted, and ids no Check has are ignored.
// `--` ends the flags, as does the first other argument; every argument after
// them is a path.
// It returns 1 if any warning was found, 2 for a usage error or a path that could
// not be read, and 0 otherwise.
func Run(args []string, stdout, stderr io.Writer) int {
	off, args, ok := parseFlags(args)
	if !ok || len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	status := exitClean
	files, err := collect(args)
	if err != nil {
		fmt.Fprintf(stderr, "tcl-lsp check: %v\n", err)
		status = exitUsage
	}

	ix := index.New()
	raw := map[string][]check.Finding{}
	src := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(stderr, "tcl-lsp check: %v\n", err)
			status = exitUsage
			continue
		}
		src[f] = string(b)
		raw[f] = ix.IndexFileChecked(f, string(b))
	}

	warned := false
	for _, f := range files {
		findings := check.Unshadowed(check.Enabled(raw[f], off), ix.DefinesCommand)
		sort.SliceStable(findings, func(i, j int) bool { return findings[i].Start < findings[j].Start })
		lines := lineStarts(src[f])
		for _, fd := range findings {
			line, col := lineCol(lines, fd.Start)
			fmt.Fprintf(stdout, "%s:%d:%d: %s %s %s\n", f, line, col, fd.Severity, fd.Check, fd.Message)
			if fd.Severity == check.Warning {
				warned = true
			}
		}
	}
	if status == exitClean && warned {
		status = exitWarnings
	}
	return status
}

// parseFlags splits args into the Checks the leading --disable flags turn off
// (each id mapped to false, the shape check.Enabled reads) and the paths after
// the flags. ok is false when a --disable has no value.
func parseFlags(args []string) (off map[string]bool, paths []string, ok bool) {
	off = map[string]bool{}
	for len(args) > 0 {
		a := args[0]
		var ids string
		switch {
		case a == "--":
			return off, args[1:], true
		case a == "--disable":
			if len(args) < 2 {
				return off, nil, false
			}
			ids, args = args[1], args[2:]
		case strings.HasPrefix(a, "--disable="):
			ids, args = strings.TrimPrefix(a, "--disable="), args[1:]
		default:
			return off, args, true
		}
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				off[id] = false
			}
		}
	}
	return off, nil, true
}

// collect returns the files args name, each once however it is spelled (by its
// absolute path with symlinks resolved), in argument order with a directory's
// files in lexical order. The error reports the first path that could not be
// read; the files found up to and after it are still returned.
func collect(args []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	add := func(p string) {
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
			if real, err := filepath.EvalSymlinks(abs); err == nil {
				key = real
			}
		}
		if !seen[key] {
			seen[key] = true
			files = append(files, p)
		}
	}
	var firstErr error
	for _, a := range args {
		a = filepath.Clean(a)
		info, err := os.Stat(a)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !info.IsDir() {
			add(a)
			continue
		}
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if index.IsIndexable(p) {
				add(p)
			}
			return nil
		})
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return files, firstErr
}

// lineStarts returns the offset where each line of src begins.
func lineStarts(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineCol returns the 1-based line and byte column of offset, given the
// source's lineStarts.
func lineCol(starts []int, offset int) (int, int) {
	line := sort.SearchInts(starts, offset+1) - 1
	return line + 1, offset - starts[line] + 1
}
