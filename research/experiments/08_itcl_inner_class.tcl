# 08_itcl_inner_class.tcl
#
# Proves where an itcl::class defined inside a method is created, and where its
# own methods look up what they call.
#
#   8.6.18 (Itcl 4.3.7) and 9.0.3 (Itcl 4.3.5) agree:
#   - An unqualified `itcl::class Inner` (or `::itcl::class`) in a method,
#     constructor, class proc or itcl::body, including in a control-flow body
#     there, is created in the class namespace (::ns::K::Inner), as an
#     unqualified proc is (08_itcl_class_body.tcl). A bare `Inner` call in a
#     method of K finds it.
#   - In an `after` body it is created in ::, in an `uplevel 1` body in the
#     caller's namespace (::q::Inner4).
#   - An absolute name is created where it says. A relative qualified name
#     (`sub::Inner5`) goes under ::ns::K::sub when that exists, not ::ns::sub
#     (still a known wrong answer, docs/BACKLOG.md).
#   - Inner's methods look up from ::ns::K::Inner, then global; ::ns is never
#     searched.
#   - `inherit B` resolves in the namespace that holds the class, then global:
#     ::ns::K for Inner, ::ns::sub for `itcl::class sub::X` in ::ns. A base that
#     exists only in the lexical ::ns is an error.
#
# Run: tclsh8.6 08_itcl_inner_class.tcl; env -u TCLLIBPATH tclsh9.0 08_itcl_inner_class.tcl
# Pinned by server/internal/resolve/itcl_inner_class_test.go; the name is
# nestedDefName in server/internal/tcl/defs.go, the body's class is set in walkAll
# (server/internal/tcl/walk.go), and inherit names are qualified in recordInherit
# (server/internal/tcl/classdecl.go).
package require itcl
puts "tcl [info patchlevel] itcl [package require itcl]"
proc ::g {} { return ::g }
proc ::h {} { return ::h }
namespace eval ::ns {
    proc g {} { return ::ns::g }
    proc h {} { return ::ns::h }
    itcl::class K {
        method m {} { itcl::class Inner { method hi {} { return "[g] [h]" } } }
        method c {} { if {1} { ::itcl::class Inner2 {} } }
        method w {} { after 0 {itcl::class Inner3 {}}; uplevel 1 {itcl::class Inner4 {}} }
        method q {} { itcl::class sub::Inner5 {}; itcl::class ::abs::Inner9 {} }
        constructor {} { itcl::class Inner6 {} }
        proc cp {} { itcl::class Inner7 {} }
        method later
        method who {} { return [Inner #auto] }
    }
    namespace eval K::sub {}
}
namespace eval ::abs {}
::ns::K k
k m
k c
k q
::ns::K::cp
itcl::body ::ns::K::later {} { itcl::class Inner8 {} }
k later
namespace eval ::q { proc call {} { k w } }
::q::call
update
puts "who=[k who]"
foreach c {::ns::K::Inner ::ns::Inner ::ns::K::Inner2 ::Inner3 ::q::Inner4 ::ns::Inner4
           ::ns::K::sub::Inner5 ::ns::sub::Inner5 ::abs::Inner9 ::ns::K::Inner6
           ::ns::K::Inner7 ::ns::K::Inner8} {
    puts "$c: [llength [itcl::find classes $c]]"
}
namespace eval ::ns::K::Inner { proc g {} { return ::ns::K::Inner::g } }
::ns::K::Inner i
puts "hi=[i hi]"
namespace eval ::ns {
    itcl::class B { method bm {} { return ::ns::B } }
    itcl::class sub::B { method bm {} { return ::ns::sub::B } }
    itcl::class sub::X { inherit B }
    itcl::class K::B { method bm {} { return ::ns::K::B } }
    itcl::class B2 {}
    itcl::body K::later {} { itcl::class InnerI { inherit B } }
}
k later
puts "X=[namespace eval ::ns::sub::X {info inherit}] InnerI=[namespace eval ::ns::K::InnerI {info inherit}]"
itcl::body ::ns::K::later {} { itcl::class InnerJ { inherit B2 } }
puts "B2: [catch {k later} e] $e"
# Expected, both versions:
#   who=inner0
#   ::ns::K::Inner: 1   ::ns::Inner: 0   ::ns::K::Inner2: 1   ::Inner3: 1
#   ::q::Inner4: 1      ::ns::Inner4: 0  ::ns::K::sub::Inner5: 1
#   ::ns::sub::Inner5: 0   ::abs::Inner9: 1   ::ns::K::Inner6: 1
#   ::ns::K::Inner7: 1  ::ns::K::Inner8: 1
#   hi=::ns::K::Inner::g ::h
#   X=::ns::sub::B InnerI=::ns::K::B
#   B2: 1 cannot inherit from "B2" (class "B2" not found in context "::ns::K")
