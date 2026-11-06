# Real-world TCL/RVT idiom survey & Itcl gap report

**Date:** 2026-06-25

## Why this exists

The earlier test suites were almost entirely *synthetic* — hand-authored snippets
that exercised the shapes the parser was written for. That is a closed loop: the
tests pass because they use the forms the code already handles. This survey breaks
the loop by cataloguing the idioms that **real** TCL/RVT codebases actually use,
vendoring representative excerpts as fixtures, and running the resolver/index/
symbol layers against them. It immediately found that a *documented, shipped*
feature — Itcl method/ivar resolution — was effectively non-functional on real
code.

## Method

Surveyed three real, production Apache-Rivet / Itcl codebases (cloned at the
commits below; every vendored fixture was confirmed structurally complete with
`tclsh`'s `info complete`, which mirrors the server's own word/brace parsing
model):

| Repo | Commit | Role |
| --- | --- | --- |
| `flightaware/speedtables` | `0fe25e1` | itcl `STDisplay` data-display framework; the `$display field`/`$display show` demo pages |
| `mxmanghi/rivetweb` | `13abf80` | an itcl-based web framework (`rweb_*` class hierarchy) |
| `apache/tcl-rivet` | `7ae32f6` | canonical Rivet: `.rvt` page structure + the `::rivet::*` command set |

Vendored fixtures live in `server/internal/resolve/testdata/realworld/` (see its
`MANIFEST.md`); idiom counts below come from a full catalog of the three trees.

## Idiom catalog (highlights)

- **Itcl members are almost always access-modified.** `public method` /
  `protected method` / `private method`, `public variable` / `private variable`,
  `private common`. Counts: rivetweb ~200+ `public method`, ~150+ `private
  variable`; speedtables ~90+ `protected method`, ~40+ `public variable`;
  tcl-rivet ~150+ `public variable`. Bare `method`/`variable` (what the synthetic
  tests used) is the *minority* form.
- **`$this method` and `[$this method]`** are the dominant intra-class call form —
  usually bracketed for their return value (`[$this content_type]`).
- **Inheritance** is real and multi-level (`RWContent → RWPage → RWBasicPage`;
  `Database → Postgresql`), via bare `inherit`.
- **External `::itcl::body Class::method {...} {...}`** definitions are pervasive
  (calendar.tcl alone has ~100+), interleaved with the inline class block.
- **Three-part constructor** `constructor args {Base::constructor …} {body}`
  chaining the base class.
- **`.rvt`**: `<? … ?>` / `<?= … ?>`, control flow spanning multiple tags with
  literal HTML between, cross-file `source`/`::rivet::include`/`::rivet::parse`.

## Gap report — found and FIXED this pass

Each was confirmed against the real fixtures, fixed, and pinned by a test.

1. **Access-modified members were not parsed at all** (only bare
   `method`/`variable`/`common`). Net effect: goto-def, document/workspace
   symbols, and references for itcl methods/ivars failed on essentially all real
   itcl code. *Fix:* strip a leading `public`/`protected`/`private` before a
   member keyword in `emitDefs` and `childBodies` (`memberWords`).
2. **`$obj method` calls inside method bodies were undetected** — `ObjMethodAt`
   hardcoded `FrameNamespace` when recursing, so it never entered a method body
   (which requires `FrameClass`). *Fix:* thread the scope context through
   `objMethodInCmds`.
3. **`[$obj method]` bracketed calls were undetected outside top level** — an
   absolute-vs-relative base error in `objMethodInSubsts` (it used `w.Start`
   without the command's base, which is 0 only at top level). *Fix:* thread the
   command base.
4. **`$this method` never resolved** — the implicit self cannot be typed by
   `ClassOf` (there is no `set this [...]`). *Fix:* when the receiver is `this`
   inside a method body, resolve on the enclosing class directly.
5. **Three-part constructor's init block was not walked**, so the base-class
   chain call inside it was invisible to find-references. *Fix:* walk the init
   block too.
6. **`common name {value}`** had its braced initial value mis-walked as a script
   (emitting bogus references), unlike `variable`. *Fix:* treat `common` as a
   data-brace command, matching `variable`.

Namespaced `::#auto` instantiation (`[::ns::C ::ns::#auto …]`) was on the list but
already worked (`ClassOf` keys on the class head word) — covered by a guard test.

## Remaining known gaps (documented, not yet fixed)

- **TclOO** (`oo::class`, `oo::define`) — out of scope; Itcl only. Since v0.6.3 its
  bodies are silent rather than walked as namespace code, which had turned method
  locals into namespace variables. See `research/08-flightaware-corpus-probe.md`.
- **Protection blocks** `public { … }` / `protected { … }` grouping several
  declarations — 0 occurrences in the surveyed corpus, so deferred.
- **`variable name default { config }`** — the optional third (config-code) block
  of an itcl variable declaration is treated as data and not walked for the calls
  inside it (~10 occurrences). Low value.
- **Receivers with no local class** (method params, factory returns, ivars set in
  another method) stay unresolved by design — Itcl has no type annotations.
- **Dynamic-class instantiation** `[$class ::#auto]` stays unresolved (the class
  is a variable) — correct graceful behavior.
