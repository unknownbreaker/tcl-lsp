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
  `foreach`, `lmap`, `switch`, `catch`, `try`, `time`, `dict for`, `dict map`,
  `dict with`, `dict update`, `eval` of one braced word, `uplevel 0`), and
  every `[command substitution]` inside those. The braced argument of any other
  command (`after 10 {…}`, your own wrapper, a TclOO definition) may be data or
  deferred code, so nothing inside it is checked. An Itcl class body's own lines declare members, so they are
  not checked either; the member bodies are.
  Each of those must have words Tcl accepts: `namespace eval ns a b` joins its
  words into a script of its own, and `proc p a b {…}`, `if {$c} {…} else`,
  `foreach v {…}` or `catch {…} r o x` is an error that runs nothing. A braced
  condition (`if {[…]}`, `expr {[…]}`) is substituted by the command that
  evaluates it, so its scripts count only when that command is well formed.
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
  `oo::class create set`, an Itcl method `set`, an ensemble made or remapped
  with `namespace ensemble create -command set`, `namespace ensemble configure
  set -map …`, or `namespace ensemble create` run in namespace `set` (by
  `namespace eval set …` or `namespace inscope set …`). A redefinition typed in
  one open buffer updates the diagnostics of every other open buffer at once, as
  does one saved to disk. Names built at run time (`proc $name`) are not seen;
  see `docs/BACKLOG.md`.
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

### `constant-condition` (warning, or hint)

An `if` or `elseif` condition, `while` test or `for` test whose value is fixed
because it compares only literals. A literal is a number, a boolean word
(`true`, `yes`, `on`, …), a quoted string with no `$` or `[`, or a braced
string. The comparisons may be joined with `!`, `&&`, `||`, `?:` and
parentheses, and each operand of those (each `?:` arm too) is a comparison or
a bare `0` or `1`.

Flagged as a warning:

```tcl
if {1 == 1} {…}
if {"a" eq "a"} {…}
while {1 < 2} {…}
for {set i 0} {1 > 2} {incr i} {…}
if {$x} {…} elseif {2 != 2} {…}
if {{a} in {a b}} {…}
if {"a" ne "b" && !(1 >= 2)} {…}
if 1==1 {…}
```

A hint, since it is the usual way to switch a block off or on:

```tcl
if {0} {…}               ;# condition is always false
if {1} {…}               ;# condition is always true
if {$x} {…} elseif 0 {…}
```

Not flagged:

```tcl
while 1 {…}              ;# the idiomatic infinite loops
while {1} {…}
for {} 1 {} {…}
while {0} {…}            ;# only if and elseif get the 0/1 hint
if {true} {…}            ;# compares nothing
if {!!$x} {…}
expr {60 * 60 * 24}      ;# expr is not a condition
if {1 + 1 == 2} {…}      ;# an operand Tcl computes
if {0.1 + 0.2 eq "0.30000000000000004"} {…}   ;# hangs on tcl_precision in 8.6
if {rand() < 2} {…}      ;# a math function, which may be your own
if {1 == 1 && $x} {…}    ;# a variable anywhere
if {1 == 1 # note} {…}   ;# a comment: Tcl 9.0 only
if {1_000 == 1000} {…}   ;# a number Tcl 8.6 and 9.0 read differently
if {010 == 8} {…}        ;# octal in 8.6, decimal in 9.0
if {"a" lt "b"} {…}      ;# lt, gt, le and ge: Tcl 9.0 only
if {"a" || 1 == 1} {…}   ;# no value: it always raises an error
if {1 == 1 ? "a" : "b"} {…}   ;# gives a string, not a boolean
if {"a" in {{a}b}} {…}   ;# a list that may not parse
if {1 == 1}{…}           ;# words that touch: Tcl rejects the command
if {1 ==} {…}            ;# malformed: Tcl rejects it
```

A comparison counts only when both operands are literals as written, so a value
Tcl computes first never does: its string form can change with `tcl_precision`.
Text the expression parser cannot read for certain (a malformed or half-typed
condition, an unknown bareword, a `#` comment, `${name}` with a `{` or `\` in
it) gives no finding. The evidence, each condition evaluated in four settings
on tclsh 8.6 and 9.0, is `server/internal/checkcmd/testdata/tclsh/constant_condition.tcl`.

### `self-comparison` (warning)

A comparison of a value with itself, whose result is fixed. It works on any
comparison inside an `if` or `elseif` condition, a `while` or `for` test, or
`expr {…}`, and on the commands `string equal` and `string compare`. The two
operands must be written alike. Each must hold a `$var` and no `[script]`.

Flagged:

```tcl
if {$x eq $x} {…}                 ;# always true
if {$x ne $x} {…}                 ;# always false
while {$i < $i} {…}               ;# < and > are always false
if {$a($i) eq $a($i) || $y} {…}   ;# the first comparison
set r [expr {"a$x" ne "a$x"}]
string equal $x $x                ;# always 1
string compare $x $x              ;# always 0
```

Not flagged:

```tcl
if {$x != $x} {…}                 ;# true when x is NaN: a NaN test
if {$x == $x} {…}                 ;# so are ==, <= and >=
if {$x in $x} {…}                 ;# depends on the value
if {$x eq $y} {…}
if {[f] eq [f]} {…}               ;# f may return something new each call
if {$x eq ${x}} {…}               ;# only operands written alike
string equal -nocase $x $x        ;# no options
string equal {$x} {$x}            ;# braced words substitute nothing
expr $x eq $x                     ;# substituted twice
string equal $y(a $y(a            ;# the index runs on to the next `)`, as in Tcl
```

`set x NaN` makes `$x == $x` false and `$x != $x` true in Tcl 8.6 and 9.0,
which is why only `eq`, `ne`, `<` and `>` count. A read trace on the variable
could change its value between the two reads; that is the one way a flagged
comparison can vary. To compare across such a trace on purpose, write
`[set x] eq [set x]`, which says so and is not flagged. A command-form operand counts only when it has no
backslash and closes every `(` it opens: Tcl reads an array index across
spaces, newlines and `;` to its `)`, and a backslash-newline splits a word. A
redefined `string`, `equal` or `compare` silences the command form (see
Redefined builtins). The message names the operand when it is short and
printable ASCII, and says "a value" otherwise. The evidence, each comparison
evaluated with `$x` bound to NaN, Inf, empty, lists and other values on tclsh
8.6 and 9.0, is
`server/internal/checkcmd/testdata/tclsh/self_comparison.tcl`.

### `duplicate-logic` (warning)

Logic that repeats itself, so the repeat changes nothing. It works on three
shapes:

- an operand of a chain of `||` or `&&` that repeats an earlier operand of the
  same chain, in an `if` or `elseif` condition, a `while` or `for` test, or
  `expr {…}`. The earlier copy already decided the chain if it could.
- an `elseif` condition that repeats an earlier condition of the same `if`.
  The earlier one was false, so the repeat is false too and its branch never
  runs.
- an `if` with an `else` whose bodies are all the same braced text, so the
  branch taken does not matter.

A repeat must be written like the earlier copy, up to spaces and parentheses,
and hold a `$var`. Nothing from the earlier copy to the repeat may call a
command or a math function, since a call may return something new each time
or change a variable the repeat reads.

Flagged:

```tcl
if {$a == 1 || $a == 1} {…}         ;# the second $a == 1
while {$x && ($y && $x)} {…}        ;# the second $x
set r [expr {$x || $y || $x}]
if {$x != $x || $x != $x} {…}       ;# NaN or not, the same value twice
if {[f] || $x || $x} {…}            ;# the call comes before both copies
if {$x} {…} elseif {$y} {…} elseif {($x)} {…}   ;# the last condition
if {$x} {set r a} else {set r a}    ;# the else body
if {$x} {a} elseif {$y} {a} else {a}
```

Not flagged:

```tcl
if {$x || [set x 1; expr 0] || $x} {…}   ;# a script between the copies
if {$x || g() || $x} {…}            ;# so is a math function: it may set x
if {[f] || [f]} {…}                 ;# f may return something new
if {rand() < 0.5 || rand() < 0.5} {…}
if {$x && $y || $x} {…}             ;# ($x && $y) || $x: different operands
if {$x || ${x}} {…}                 ;# only repeats written alike
if {1 || 1} {…}                     ;# no variable: see constant-condition
if {$x} {a} elseif {[set x 1; expr 0]} {b} elseif {$x} {c}
if $x {a} elseif $x {b}             ;# substituted before if runs
if {$x} "set r [incr n]" else "set r [incr n]"   ;# two different bodies
if {$x} {a} elseif {$y} {b} else {a}   ;# not every body
if {$x} {a} else { a}               ;# bodies written differently
expr {$x ? $y : $y}                 ;# ?: is left alone
```

A read trace on a variable runs at each read and may change its value, and
`[info frame]` or an error's line number tells two equal bodies apart; those
are the ways a flagged repeat can matter. The evidence, each flagged command
run beside its form with the repeat dropped, for every pair of values of `$x`
and `$y` (NaN, Inf, empty, non-booleans and others), on tclsh 8.6 and 9.0, is
`server/internal/checkcmd/testdata/tclsh/duplicate_logic.tcl`.

### `constant-query` (warning)

A pure query builtin called with only literal arguments, so it returns the same
result on every run. A literal is a braced word with no backslash, or a bare or
quoted word with no `$` or `[` (backslash escapes are decoded). The purity
table lists each query and the shape each argument must have; any other call
is silent:

| Query | Arguments |
| --- | --- |
| `string index S I` | text, index |
| `string first S S`, `string last S S` | text, text |
| `string match S S` | text, text |
| `string equal S S`, `string compare S S` | text, text |
| `string is CLASS ?-strict? S` | a class below, text |
| `lindex L I` | plain list, index |

- **Text** is a literal whose value is printable ASCII. Other text can differ
  between Tcl 8.6 and 9.0, through their Unicode tables and 8.6's UTF-16
  strings.
- **CLASS** is one of `alnum`, `alpha`, `ascii`, `control`, `digit`, `graph`,
  `lower`, `print`, `punct`, `space`, `upper`, `wordchar`, `xdigit` and
  `integer`. For `integer`, the value must also be a decimal of at most 9
  digits with no leading zero (with an optional sign and spaces around it), or
  hold no digit at all.
- An **index** is `0`, a decimal of at most 9 digits with no leading zero,
  `end`, or `end-` followed by such a decimal.
- A **plain list** is text with no `{`, `}`, `"` or `\`, so its elements are
  the words between its spaces.

Flagged:

```tcl
string is integer 5
string is alpha -strict abc
string index abc end-1
string first b abcb
string match {a*[bc]?} abcd
string equal abc abc
lindex {a b c} 1
set c [::string index abc 0]
```

Not flagged:

```tcl
set width [string length {Continue}]   ;# sizes are left out, see below
llength {a b c}
lindex $list 0                  ;# a substitution
string index abc $i
string index {*}{abc 0}         ;# expansion
string ind abc 0                ;# a prefix: only full names count
string equal -nocase A a        ;# options
string first b abcb 2           ;# a form not in the table
lindex {a b c} 0 0
string is double 1.5            ;# a class not in the table
string is integer 1_000         ;# 1 in Tcl 9.0, 0 in 8.6
string is integer 09            ;# octal in 8.6
lindex {a b c d e f g h i j} 010   ;# i in 8.6, empty in 9.0
lindex {a b c} e                ;# end in 8.6, an error in 9.0
string index "\u00e9" 0         ;# not ASCII
lindex {{a b} c} 0              ;# not a plain list
list a b                        ;# constructors
format %d 1
string toupper abc              ;# transforms: see constant-transform
after 10 {lindex {a b} 0}       ;# a deferred script
```

`string length` and `llength` are left out on purpose. Measuring a literal is
a readable way to derive a number: `set width [string length {Continue}]`
sizes a button from its label, where `set width 8` would hide where the 8
comes from. A table entry goes in only if its result is the same in Tcl 8.6
and 9.0, so a constant query is never a version test. A redefined `string`,
`lindex`, or a command named after the subcommand (`index`, `is`, …) silences
the finding (see Redefined builtins). The evidence is
`server/internal/checkcmd/testdata/tclsh/constant_query.tcl`. Each flagged
command is run twice on tclsh 8.6 and 9.0, and shown to change no variable,
command or namespace and to give one result in both. Each shape is also
fuzzed: every short word over an alphabet of the characters that matter is
run in both versions, and the outputs agree on every word the table accepts.

### `constant-transform` (hint)

A pure transform builtin called with only literal arguments, so its result
could be written in its place. It uses `constant-query`'s literal rule and
shapes, with its own purity table; any other call is silent:

| Transform | Arguments |
| --- | --- |
| `string toupper S`, `string tolower S`, `string totitle S` | text |
| `string trim S ?S?`, `string trimleft S ?S?`, `string trimright S ?S?` | text, optional text |
| `string reverse S` | text |
| `string range S I I` | text, index, index |
| `lsort L`, `lreverse L` | plain list |
| `lrange L I I` | plain list, index, index |

Flagged:

```tcl
string toupper abc
string totitle {hELLO wORLD}
string trim "  x  "
string trimleft xxa x
string range abcdef 1 end-1
lsort {c b a}
lreverse {a b c}
set s [::string tolower ABC]
```

Not flagged:

```tcl
string toupper $x               ;# a substitution
lrange {a b c} $i end
string toup abc                 ;# a prefix: only full names count
lsort -unique {b a b}           ;# options
string toupper abc 0 0          ;# a form not in the table
string range abc 0 e            ;# end in 8.6, an error in 9.0
lrange {a b c} 010 end          ;# octal in 8.6
string toupper "\u00e9"         ;# not ASCII
lsort {{b a} c}                 ;# not a plain list
set usage [string trim {
    usage: tool ?-v? file
}]                              ;# a block of lines: a newline is not text
string repeat - 40              ;# constructors
string map {a b} abc
split a,b ,
join {a b} ,
after 10 {lsort {b a}}          ;# a deferred script
```

It is a hint, not a warning: a constant transform is harmless, only longer
than its result. Constructors (`list`, `join`, `split`, `string repeat`,
`string map`, `format`, `concat`) are left out on purpose, since building a
value from parts is a readable way to write it. So is trimming a braced block
of lines, the usual way to lay out long text; text must be printable ASCII, so
a newline keeps it silent. A table entry goes in only if its result is the
same in Tcl 8.6 and 9.0. A redefined `string`, `lsort`, `lreverse`, `lrange`,
or a command named after the subcommand (`toupper`, `trim`, …) silences the
finding (see Redefined builtins). The evidence is
`server/internal/checkcmd/testdata/tclsh/constant_transform.tcl`, run the same
way as `constant-query`'s: each flagged command twice on tclsh 8.6 and 9.0,
and each shape fuzzed over every short word of an alphabet.

### `unreachable-code` (hint)

Commands after an exit that always ends their script: `return`, `error`, or a
`break` or `continue` inside a loop. Tcl stops a script at the first command
that does not finish normally, so nothing after it runs. There is one hint per
script, covering every command from the first dead one to the last, and it is
tagged as unnecessary code, so editors fade it rather than underline it.

Flagged:

```tcl
proc p {x} {
    return $x
    puts never              ;# from here…
    set y 2                 ;# …to here
}
proc q {} { error "no way"; cleanup }
foreach v $list {
    if {$v eq "stop"} {
        break
        puts never
    }
    continue
    incr n
}
set v [return 1; expr {2}]  ;# a [script] is a script too
return -code error boom; log  ;# any -code
```

A `break` or `continue` counts inside the body of `while`, `for`, `foreach`,
`lmap`, `dict for` or `dict map`, and inside anything that runs a script in
place within one: `if`, `switch`, `catch`, `try`, `eval`, `uplevel 0`, `time`,
`dict with`, `dict update`, `namespace eval`, a `[script]`, an expression's
`[script]`, or `for`'s start and next scripts. Each passes the exit on to the
loop. `for`'s next script is not a loop body of its own: it rejects
`continue`, so neither exit counts there unless an outer loop takes it.

Not flagged:

```tcl
proc p {} { puts hi; return }            ;# nothing after the exit
return -level 0 $x; puts runs            ;# -level 0 gives its value and goes on
return -options $opts x; puts may-run    ;# so may -options
return $opt 0 x; puts may-run            ;# an option name Tcl computes
return {*}$args; puts may-run            ;# expansion hides the word count
error; puts never                        ;# wrong # args: left alone
proc p {} { break; puts never }          ;# outside a loop: Tcl reports the break
foreach v $l { proc q {} { continue; x } }   ;# a proc is a frame of its own
after 10 { return; x }                   ;# a deferred script
if {$x} { return }; puts runs            ;# the exit is in another script
```

Any `-level` or `-options` silences `return`, whatever its value: options are
matched exactly, so `-lev` or `-LEVEL` is just another option and still
returns. In a `.rvt` template, dead code that runs on into a later `<? ?>`
block has no single range in the template, so it gives no finding. A redefined
exit (`proc return`, `rename break {}`), or a redefined builtin whose body holds
the dead code, silences the hint (see Redefined builtins). The evidence, each
case run as a proc body with a marker after the exit, on tclsh 8.6 and 9.0, is
`server/internal/checkcmd/testdata/tclsh/unreachable_code.tcl`.
