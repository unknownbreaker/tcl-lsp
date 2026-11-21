---
paths:
  - "server/**"
---

# Go Server

## Concurrency invariants

One parse per file feeds every analysis, and for an open document the Checks too
(`index.IndexFileChecked`). The initial index and the workspace
read-scans (find-references, incoming calls, workspace symbols) fan out across
GOMAXPROCS **while the single-goroutine dispatch loop is blocked on the request**,
so the index takes no locks. Preserve that: anything new that reads the index in
parallel runs under the same blocked loop. The one lazily-mutated cache,
`nsCache`, is pre-warmed single-threaded before a parallel scan. Test with
`go test -race`.

## Semantic tokens

Semantic tokens color only defs, `$`-vars and calls that resolve to user procs
or methods. Builtins fall back to the editor's syntax highlighting.
