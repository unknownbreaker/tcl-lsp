# 02 — Namespace Name Resolution (Commands vs Variables)

**Evidence:** `experiments/02_namespace_resolution.tcl`. Outputs below verified
**identical on 8.6.18 and 9.0.3**.

This is the heart of goto-definition. Given an unqualified or qualified name at a
cursor, which declaration does it bind to? TCL uses **two different algorithms**
depending on whether the name is in command position or variable position.

## The two resolution algorithms

### Command name resolution (for `cmd` in command position)
For an unqualified command name referenced from namespace `N`:
1. Look in `N`.
2. Look in each namespace on `N`'s **`namespace path`** (in listed order).
3. Look in **global `::`**.
4. Otherwise: *invalid command name*.

It does **NOT** walk `N`'s ancestor namespaces. (F-A)

### Variable name resolution (for `var` in variable position, inside a proc)
1. **Local** frame only.
2. Otherwise: *no such variable*.

No namespace step, no `namespace path` step, no global fallback. The only ways a
non-local variable enters the frame are `global`/`variable`/`upvar` (see 01) or a
**fully-qualified** name (`$::ns::var`). (F-E)

> **This asymmetry is the #1 thing the resolver must get right.** Same textual
> namespace context, two different lookup rules depending on syntactic position.

## Findings

### F-A — Unqualified command = current namespace, then global only
```
unqualified ancestor_cmd from ::a::b -> ERROR: invalid command name "ancestor_cmd"
unqualified global_cmd  from ::a::b -> ::global_cmd
```
A command in the *parent* namespace `::a` is **not** visible unqualified from
`::a::b`. A global command **is** (step 3).
**Resolver implication:** do not resolve unqualified commands by walking up the
namespace tree. Only {current} ∪ {path} ∪ {global}.

### F-B — `namespace path` extends the command search set (ordered)
```
helper via namespace path ::lib -> ::lib::helper
```
**Resolver implication:** the resolver must track each namespace's `namespace
path` declaration and include those namespaces (in order) when resolving
unqualified commands. `namespace path` is itself a statement to parse and bind.

### F-C — Relative vs absolute qualified names
```
relative   y2::target  -> ::x::y::y2::target   (resolved against current ns ::x::y)
absolute   ::x::target  -> ::x::target
```
**Resolver implication:** a name containing `::` but not starting with `::` is
**relative** to the current namespace; a leading `::` is **absolute** from the
global root. The resolver must compute the current namespace at each point and
prepend it for relative qualified names.

**Correction (2026-11-05).** This finding only tested a target that exists
(`experiments/02_namespace_resolution.tcl:51-58`), so it missed the fallback. Run
with `experiments/08_relative_qualified.tcl`:
```
relative prom::helper from ::other -> ::prom::helper                ;# 8.6.18 and 9.0.3
with ::other::prom::helper also defined -> ::other::prom::helper    ;# 8.6.18 and 9.0.3
relative $cfg::level from proc in ::user -> 3                       ;# 8.6.18
can't read "cfg::level": no such variable                           ;# 9.0.3
```
A relative qualified **command** tries `<current>::a::b` first and then falls back
to `::a::b`, on both versions. A relative qualified **variable** falls back on 8.6
and errors on 9.0, so the two versions differ. The server still implements only the
current-relative lookup; see `08-flightaware-corpus-probe.md` (misses 1 and 10).

### F-D — `namespace export`/`import` create command aliases; non-exported imports silently no-op
```
imported pub             -> ::provider::pub
which -command pub (in consumer) -> ::consumer::pub   ;# import made a real command in consumer
import of non-exported priv ->                         ;# empty, NO error: silently imported nothing
```
**Resolver implications:**
- `namespace import ::provider::pub` creates a command **`::consumer::pub`** that
  is an alias to `::provider::pub`. goto-definition on `pub` in the consumer
  should ideally jump through the alias to `::provider::pub`'s real `proc`.
- Only **exported** commands (`namespace export pub`) are importable; importing a
  non-exported name does nothing and raises no error. The resolver must model
  `namespace export` patterns to know what an `import` actually brings in.

### F-E — `namespace path` does NOT affect variable resolution
```
unqualified shared via path (proc) -> ERROR: can't read "shared": no such variable
qualified ::vlib::shared           -> ::vlib::shared
```
Confirms OQ3: `namespace path` is **commands-only**. (Closes OQ3 from doc 01.)

### F-F — `namespace which` is the resolution oracle
```
which -command cmd  -> ::oracle::cmd
which -variable var -> ::oracle::var
which -command set  -> ::set        ;# builtins live in ::
```
**Resolver implication:** `namespace which -command`/`-variable` is exactly the
function our static resolver reimplements. It is our **golden test oracle**: for
any fixture, our resolver's answer should match what `namespace which` returns at
that point (where statically determinable).

### F-G — Commands resolve at CALL time (forward references work)
```
calls not-yet-defined callee -> ::fwd::callee (defined after caller)
```
A proc can call another proc defined **later** in the source.
**Resolver implication:** resolution is not order-sensitive within a scope. The
indexer must do a **full pass to collect all definitions first**, then resolve
references against the complete symbol table — never resolve in a single
top-to-bottom streaming pass.

## Consolidated resolver rules (so far)

**To resolve a name at a cursor, first classify its syntactic position:**

| Position | Algorithm |
|----------|-----------|
| Command (first word of a command, or after `[`) | current ns → `namespace path` → global `::` |
| Variable (`$name`, `set name`, etc., inside a proc) | local only; else needs `global`/`variable`/`upvar`/FQN |
| Variable at `namespace eval` top level (not in a proc) | the namespace's own variable (needs experiment — see OQ5) |
| Any qualified name | leading `::` = absolute; else relative to current ns, then falls back to global (commands on 8.6 and 9.0; variables on 8.6 only; see the F-C correction) |

## Open questions raised

- **OQ5:** At `namespace eval` top level (outside any proc), does an unqualified
  `set x`/`$x` refer to the namespace's variable? (Expected yes — frame is the
  namespace. Verify in 03 or a dedicated experiment.)
- **OQ6:** `namespace import` with glob patterns (`namespace import ::p::*`) — how
  to statically enumerate what's imported? Interacts with `namespace export`.
- **OQ7:** Does `namespace import` create a re-exportable command? Chained imports?
- **OQ8 (carried):** classifying command vs variable position is itself nontrivial
  (e.g., a bareword inside `[expr {...}]`, `if`/`while` condition bodies, command
  substitution). The parser must label positions accurately — this is a
  prerequisite for applying the right algorithm. Belongs in the parser design.
