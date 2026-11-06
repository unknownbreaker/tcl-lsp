# Changelog

## v0.6.3 (2026-11-05)

Fixes from probing the server against 10 public Tcl repos. These remove the
wrong-answer categories that probe measures, and known wrong answers remain, including
a stale answer for a use inside `[catch {...}]`. See "Known limits that remain" in
`research/08-flightaware-corpus-probe.md`.

### Fixed

- **Switch patterns are no longer read as commands.** A `stop { ... }` arm no longer
  jumps to `proc stop`, colors as a call, or appears in that proc's references, and a
  `-` fall-through no longer makes a symbol named `-`. Arm bodies with quoted or
  braced patterns are now walked.
- **Commands and variables are kept apart.** A call never resolves to a variable, and
  a `$x` never resolves to a proc, even when they share a qualified name. This covers
  goto-definition, references, document highlight and semantic tokens.
- **Goto-definition no longer answers with these bindings that cannot reach the use:**
  one below the cursor, one in a sibling `switch` arm, one in an `if` branch that returned,
  or, for a use in an unknown command's body, one a later `set` had replaced. Uses that a
  loop carries around now get every binding that reaches them, including uses in a braced
  `while` or `for` test. A second `array set` adds to the first instead of replacing it.
- **Itcl method parameters are locals,** so a parameter that shadows an ivar answers
  the parameter.
- **Bodies of unknown commands are no longer treated as namespace code.** TclOO
  method locals, `tsp::proc` locals and wrapper-body variables stopped becoming
  namespace variables, which had made a `$x` jump to another class's or another file's
  `set x`. They also leave the outline.
- **Slowdown on large files.** Positions no longer rescan the file from the start. On
  a 1.35 MB file, documentSymbol went from about 40 s to under 300 ms and semanticTokens
  from about 20 s to under 100 ms. Goto-definition with about 30,000 targets no longer
  times out.

### Changed

- **More silence where the server cannot be certain.** In the body of an unknown
  command at namespace level, only absolute names (`$::x`) count as variables. This
  includes TclOO, which is still unsupported, and a custom wrapper that really does run
  its body in place. A use bound only by something the analyzer does not model, such as
  `regexp` or `gets`, now goes silent instead of jumping to an unrelated `set`.
