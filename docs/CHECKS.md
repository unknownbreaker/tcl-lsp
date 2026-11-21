# Checks: certain-only diagnostics

tcl-lsp flags Tcl that is valid but cannot mean anything sensible. Each such
rule is a **Check**. A Check ships only if it can never fire on correct code.
When it is in doubt, it stays silent, so a finding is a fact about your code,
not a guess. The full design is issue #93.

## Where findings appear

- **In the editor**, as ordinary LSP diagnostics on open `.tcl` and `.rvt`
  files, refreshed on every change. Each one carries its Check id, as the
  diagnostic code and at the end of the message.
- **In CI or a pre-commit hook**, through `tcl-lsp check`:

  ```sh
  tcl-lsp check src/ templates/page.rvt
  ```

  It prints one line per finding, `file:line:col: severity check-id message`,
  with a 1-based line and byte column, the way Vim's quickfix reads them. A
  directory is walked for `.tcl` and `.rvt` files, skipping `.git`. A file you
  name directly is checked whatever its extension. The exit status is 1 if any
  warning is found, 0 if there are only hints or nothing, and 2 for a usage
  error or a path that cannot be read.

## Turning diagnostics off

One switch turns every Check off, leaving tcl-lsp navigation-only:

- Neovim: `diagnostics = false` in the plugin's `opts`.
- Vim: `let g:tcl_lsp_diagnostics = 0` before sourcing `editors/vim/tcl-lsp.vim`.
- Any other client: `"diagnostics": false` in `initializationOptions`.

## What a Check looks at

- **Only code that certainly runs as Tcl:** the top level, the bodies of
  `proc`, `namespace eval`, Itcl methods, constructors, destructors, class procs
  and `itcl::body`, the bodies of control-flow builtins (`if`, `while`, `for`,
  `foreach`, `lmap`, `switch`, `catch`, `try`, `dict for`, `dict with`, `eval`
  of one braced word, `uplevel 0`), and every `[command substitution]` inside
  those. The braced argument of any other command (`after 10 {…}`, your own
  wrapper, a TclOO definition) may be data or deferred code, so nothing inside
  it is checked. An Itcl class body's own lines declare members, so they are
  not checked either; the member bodies are.
  Each of those must have exactly its words: `namespace eval ns a b` joins its
  words into a script of its own, and `proc p a b {…}` is an error.
- **`.rvt` templates:** the code in `<? ?>` and `<?= ?>` blocks, with positions
  in the template's own coordinates. Rivet runs `<?= x ?>` as
  `puts -nonewline x`, so the block's first command is `puts`'s arguments and
  is not checked; its `[substitutions]` are, and so is any command after a
  newline or `;` in the block.
- **Redefined builtins:** a finding relies on the builtin it reports on and on
  every builtin whose body it sits in (`set`, `foreach`, `proc`, …). It is
  dropped when any indexed file defines, creates, renames or aliases a command
  with one of those names, in any namespace: `proc set`, `proc {set}`,
  `rename myset set`, `rename set {}`, `interp alias {} set {} myset`,
  `oo::class create set`, an Itcl method `set`. A redefinition typed in one open
  buffer updates the diagnostics of every other open buffer at once, as does one
  saved to disk. Names built at run time (`proc $name`) are not seen; see
  `docs/BACKLOG.md`.
- **Half-typed code:** a command whose last word never closes (a brace, quote or
  bracket left open) is rejected by Tcl, so neither it nor anything in it gives a
  finding.

## The Checks

### `self-assignment` (warning)

A variable assigned the value it already holds.

Flagged:

```tcl
set x $x
set x ${x}
set x "$x"
set ::count $::count
set n [set m $m]        ;# the inner set
```

Not flagged:

```tcl
set x $y                 ;# two variables
set x "$x "              ;# a different value
set x [set x]            ;# not a plain substitution
set ::x $x               ;# in a proc: a global and a local
set a(1) $a(1)           ;# array elements are left alone
set x:y $x:y             ;# $x:y reads x, then appends ":y"
set {x} $x               ;# only a bare name is read
after 10 { set x $x }    ;# a deferred script
```

`set x $x` does fire a variable's write traces, so it is sometimes written on
purpose to refresh a widget's `-textvariable`. Write `set x [set x]` instead,
which says so and is not flagged.

More Checks are on the way; #93 lists them.
