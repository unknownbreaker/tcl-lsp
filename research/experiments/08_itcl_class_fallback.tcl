# 08_itcl_class_fallback.tcl
#
# Proves where Itcl looks up the class a relative name in `inherit` or
# `itcl::body` names.
#
#   8.6.18 (Itcl 4.3.7) and 9.0.3 (Itcl 4.3.5) agree:
#   - `inherit Base` in a class in ::ns finds ::ns::Base, else ::Base.
#   - `itcl::body G::run` inside `namespace eval other` names ::other::G, else
#     ::G. The body runs in the class's namespace.
#   - The local class wins when both exist.
#   - A relative qualified name (`sub::B` in ::z) falls back the same way
#     (::sub::B).
#   - No parent namespace (::p::B from ::p::q) and no `namespace path` entry is
#     searched: either is an error ("class ... not found in context").
#
# Run: tclsh8.6 08_itcl_class_fallback.tcl; env -u TCLLIBPATH tclsh9.0 08_itcl_class_fallback.tcl
# Pinned by server/internal/resolve/itcl_class_fallback_test.go; the key is
# classRefKey in server/internal/tcl/bodies.go and the index resolves it in
# CanonicalClass (server/internal/index/index.go).
package require itcl
puts "tcl [info patchlevel] itcl [package require itcl]"

itcl::class ::Base { method w {} { return ::Base } }
namespace eval ns { itcl::class X { inherit Base } }
puts "inherit global:      [[::ns::X #auto] w]"            ;# ::Base

itcl::class ::B2 { method w {} { return ::B2 } }
itcl::class ::ns::B2 { method w {} { return ::ns::B2 } }
namespace eval ns { itcl::class Y { inherit B2 } }
puts "inherit both:        [[::ns::Y #auto] w]"            ;# ::ns::B2

itcl::class ::G { method run {} }
namespace eval other { itcl::body G::run {} { return [namespace current] } }
puts "body global:         [[::G #auto] run]"              ;# ::G

itcl::class ::sub::B { method w {} { return ::sub::B } }
namespace eval z { itcl::class Z { inherit sub::B } }
puts "inherit relative:    [[::z::Z #auto] w]"             ;# ::sub::B

itcl::class ::p::B {}
namespace eval p::q { puts "inherit parent:      [catch {itcl::class Q { inherit B }}]" }   ;# 1
itcl::class ::lib::C {}
namespace eval w { namespace path ::lib; puts "inherit path:        [catch {itcl::class W { inherit C }}]" }  ;# 1
