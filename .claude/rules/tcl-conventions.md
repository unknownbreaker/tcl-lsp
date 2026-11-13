---
paths:
  - "**/*.tcl"
  - "**/*.rvt"
---

# TCL Coding Conventions

Conventions for TCL code written in this repo: examples and test fixtures.
A fixture that probes the parser keeps whatever odd shape it tests (unbraced
`expr`, quoted bodies), because the shape is the test. These conventions apply
to the code around it.

## Language Baseline

- **Bracing:** prefer `{}` over `""` for blocks and expressions; only use quotes
  when you specifically want substitution.
- **Expressions:** always brace `expr` arguments — `expr {$a + $b}`, never
  `expr $a + $b` (correctness and performance).
- **Lists:** build and manipulate lists with `list`, `lappend`, `lindex`, etc.,
  not string concatenation.
- **Conditionals/loops:** brace the condition and body — `if {$x} { ... }`.
- **Quoting:** be deliberate about when substitution happens; avoid relying on
  double-substitution.

## Error Handling

- Use `try`/`trap`/`finally` (TCL 8.6+) or `catch` for recoverable errors.
- Fail loudly on programming errors; don't silently swallow exceptions.

## Style

- Keep procs small and single-purpose.
- Name things descriptively; avoid cryptic abbreviations.
- Comment *why*, not *what*, where intent isn't obvious.
