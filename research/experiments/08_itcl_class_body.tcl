# 08_itcl_class_body.tcl
#
# Proves where an itcl::class body's own commands look up what they call, and
# where an unqualified proc defined inside a method is created.
#
#   8.6.18 (Itcl 4.3.7) and 9.0.3 (Itcl 4.3.5) agree:
#   - A class body runs in ::itcl::parser, whose namespace path is empty, so
#     `common c [g]` tries ::itcl::parser::g, then ::g. Neither the enclosing
#     namespace (::ns) nor the class namespace (::ns::K) is searched, even when
#     the class namespace existed before the class.
#   - An unqualified `proc inner` in a method, constructor, destructor, class
#     proc or itcl::body, including in a control-flow body there, is created in
#     the class namespace (::ns::K::inner). In an `after` body it is created in
#     ::, in an `uplevel 1` body in the caller's namespace, and so is a proc
#     nested in one created there (::nested).
#
# Run: tclsh8.6 08_itcl_class_body.tcl; env -u TCLLIBPATH tclsh9.0 08_itcl_class_body.tcl
# Pinned by server/internal/resolve/itcl_class_namespace_test.go; the lookup is
# commandNamespace in server/internal/resolve/resolve.go and the definition
# name is nestedDefName in server/internal/tcl/defs.go.
package require itcl
puts "tcl [info patchlevel] itcl [package require itcl]"
proc ::g {} { return ::g }
namespace eval ::util { proc f {} { return ::util::f } }
namespace eval ::ns::M { proc g {} { return ::ns::M::g } }
namespace eval ::ns {
    proc g {} { return ::ns::g }
    proc h {} { return ::ns::h }
    namespace eval util { proc f {} { return ::ns::util::f } }
    itcl::class K {
        common c [g]
        common u [util::f]
        common here [namespace current]
        common n [expr {[catch {h} r] ? "error" : $r}]
        method m {} { proc inner {} {} }
        method w {} { after 0 {proc inner2 {} {}}; uplevel 1 {proc inner3 {} {}} }
        method n {} { after 0 {proc outer {} { proc nested {} {} }} }
        constructor {} { if {1} { proc inner5 {} {} } }
        destructor { proc inner7 {} {} }
        method later
        proc cp {} { proc inner4 {} {} }
        proc show {} { return "c=$c u=$u here=$here n=$n" }
    }
    # The class namespace exists first, with a g of its own.
    itcl::class M { common c [g]; proc show {} { return "c=$c" } }
}
puts "K [::ns::K::show]"
puts "M [::ns::M::show]"
::ns::K k
k m
::ns::K::cp
itcl::body ::ns::K::later {} { catch { proc inner6 {} {} } }
k later
k n
update
::outer
namespace eval ::q { proc call {} { k w } }
::q::call
update
itcl::delete object k
foreach p {::ns::K::inner ::ns::inner ::ns::K::inner4 ::ns::K::inner5 ::ns::K::inner6 ::ns::K::inner7 ::inner2 ::q::inner3 ::ns::inner3 ::nested ::ns::K::nested} {
    puts "$p: [llength [info commands $p]]"
}
# Expected, both versions:
#   K c=::g u=::util::f here=::itcl::parser n=error
#   M c=::g
#   ::ns::K::inner: 1   ::ns::inner: 0   ::ns::K::inner4: 1
#   ::ns::K::inner5: 1  ::ns::K::inner6: 1   ::ns::K::inner7: 1
#   ::inner2: 1         ::q::inner3: 1   ::ns::inner3: 0
#   ::nested: 1         ::ns::K::nested: 0
