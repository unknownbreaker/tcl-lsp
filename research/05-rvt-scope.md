# 05 — RVT (Rivet Template) Scope

**Status: scope consequences simulation-verified on 8.6; Rivet specifics now
resolved from official Apache Rivet documentation (citations below).** Rivet is an
Apache C module (`mod_rivet`) and is not installed here, so real request
processing was not run. The transform's *scope consequences* were verified with a
faithful simulator (`experiments/05_rvt/rivet_sim.tcl` + `sample.rvt`); the
Rivet-specific behaviors were confirmed against the docs. Only FlightAware-local
items remain pending (see end).

> **CORRECTION vs the simulation:** the simulator ran the template in global `::`.
> Per the docs, **real Rivet runs each template in a dedicated `::request`
> namespace** (`namespace eval ::request $script`). The *structural* findings
> (one concatenated script, blocks span, position mapping) hold; the *namespace*
> of template-top-level symbols is `::request`, not `::`. Corrected below.

## The Rivet processing model (now documented)

A `.rvt` file is HTML/text with embedded TCL:
- `<? code ?>` — TCL code, inline and verbatim.
- `<?= expr ?>` — shorthand to output a single string (since Rivet 2.0.5);
  equivalent to a `puts` of the value. [B]
- everything else — literal output: "everything outside the `<? ?>` tags becomes
  a large puts statement." [B]

Rivet compiles the whole file into **one TCL script** and evaluates it per request
with **`namespace eval ::request $script`**. The `::request` namespace is
**deleted and recreated at every request**; unless names are fully qualified,
"every variable and procedure created in .rvt files is by default placed in it"
and deleted before the next request. [A]

## Verified scope consequences (simulator) + documented corrections

### V1 — One concatenated script; control structures span `<? ?>` blocks
Verified: `<? foreach { ?> ...html... <? } ?>` puts the literal HTML *inside* the
loop in the generated script.
**Parser implication (critical, unchanged):** stitch all TCL regions of a `.rvt`
into one logical script and parse them together — never per-block.

### V2 — Template top level runs in `::request` (CORRECTED)
Unqualified vars/procs defined at template top level are created in **`::request`**
(not `::`). [A] Within the template they behave like namespace-top-level code
(01/04: unqualified var = the namespace's own variable).
**Resolver implication:** a bare `proc foo {}` in a template defines
`::request::foo`; a bare `set x` defines `::request::x`. Application code that
wants shared/persistent symbols must use explicit namespaces or the global `::`
(the docs note globals are used for things like DB connections/IO channels). [A]
**Resolver rule (shipped):** from a template, `::request` and its children are
page-local. Their definitions resolve only within the page, and so do their
`namespace path` entries and `namespace import`s: one page's `namespace path` never
steers command lookup on another page. Every other namespace merges its
declarations across the workspace.

### V3 — `<?= expr ?>` regions contain real references
`<?= $title ?>` outputs a variable; the region is real TCL and its symbols must be
parsed/indexed for goto-definition. [B]

### V4 — Source-position mapping back to the `.rvt` (unchanged)
Every token in the stitched script must carry its original `.rvt` byte
offset/line:col so goto-def/ref point into the `.rvt`, not a generated artifact.

## Resolved Category-1 confirmations (from docs)

- **[A] Request namespace — RESOLVED.** Templates evaluate via
  `namespace eval ::request $script`; `::request` is recreated/destroyed per
  request; template-level unqualified symbols default to `::request`.
  Sources: processing manuals (3.0/3.2), request lifecycle manual (2.3).
- **[B] `<?= ?>` semantics — RESOLVED.** Shorthand for outputting a single string
  (since 2.0.5); text outside `<? ?>` becomes a large `puts`. Source: Templates
  manual.
- **[C] Template composition — RESOLVED.** `::rivet::parse FILE` "parses a Rivet
  template file" and works **like Tcl's `source`** (evaluates in the current
  context, with `<? ?>` processing) → analogous to `source` (topic 04).
  `::rivet::include FILE` inserts a file **raw** (no parsing). Source: parse
  manual + commands reference.
- **[D] Rivet builtin command set — RESOLVED (catalog captured).** The `::rivet::`
  command namespace includes (3.2): `abort_code`, `abort_page`, `apache_log_error`,
  `apache_table`, `catch`, `clock_to_rfc850_gmt`, `cookie`, `debug`, `env`,
  `escape_sgml_chars`, `escape_shell_command`, `escape_string`, `exit`, `headers`,
  `html`, `http_accept`, `import_keyvalue_pairs`, `include`, `inspect`,
  `lassign_array`, `lempty`, `lmatch`, `load_cookies`, `load_env`, `load_headers`,
  `load_response`, `lremove`, `makeurl`, `no_body`, `parray`, `parse`, `raw_post`,
  `redirect`, `read_file`, `thread_id`, `try`, `unescape_string`, `upload`,
  `url_script`, `var`, `wrap`, `wrapline`, `xml`. Source: 3.2 commands reference.
  **Resolver implication:** these are external (C/Tcl-provided) commands. For v2
  they resolve to "no definition," but this list is a ready-made **stub/known-
  commands table** so the LSP can recognize them (and avoid false "unknown
  command" noise) without indexing Rivet's own sources.

## Resolver implications summary (updated)

1. **Parser:** extract TCL from `<? ?>` / `<?= ?>`; literals are opaque output;
   **stitch all TCL regions into one script per file**; preserve `.rvt` positions.
2. **Scope:** template top level = **`::request`** namespace (apply 01/04 rules
   within it). Symbols meant to be shared live in explicit namespaces or `::`.
3. **Indexing:** index `.rvt` alongside `.tcl` in the workspace-wide FQ-keyed
   table (topic 04). Note that bare template symbols are `::request::*` and are
   effectively page-local (the namespace is per-request); reusable code is
   normally in its own namespaces in `.tcl`/package files and indexes normally.
4. **Composition:** `::rivet::parse` behaves like `source` (shared context) →
   model like a `source` edge if/when we follow them; `include` is raw text.
   Defer following these for v2; workspace indexing still covers naming.
5. **External symbols:** ship the [D] command list as a known-commands table;
   everything else external resolves to "no definition."

## Still pending — FlightAware-local only

- **[E] App bootstrap** (`global.tcl`, BeforeScript/AfterScript, request hooks):
  affects what's predefined; informs diagnostics later, not core resolution.
- **[F] House conventions + a representative real `.rvt` + the exact Rivet version
  in production** (3.0/3.1/3.2 differ slightly; command set above is 3.2). Needed
  to validate the model against real templates and pin the command set.

## Open questions

- **OQ14 (narrowed):** confirm production Rivet version + grab one real `.rvt`.
- **OQ17 (new):** decide how the resolver treats `::request` symbols — page-local
  by default, with best-effort across `parse` chains? (Most reusable code lives in
  explicit namespaces, so this mainly affects template-local procs/vars.)

## Sources

- Request processing / `::request` evaluation:
  https://tcl.apache.org/rivet/manual3.2/processing.html ,
  https://tcl.apache.org/rivet/manual3.0/processing.html ,
  https://tcl.apache.org/rivet/manual2.3/request.html
- Templates / `<? ?>` / `<?= ?>`: https://tcl.apache.org/rivet/html/templates.html ,
  https://wiki.tcl-lang.org/page/Rivet
- Commands reference + `parse`: https://tcl.apache.org/rivet/manual3.2/ ,
  https://tcl.apache.org/rivet/manual3.2/parse.html
