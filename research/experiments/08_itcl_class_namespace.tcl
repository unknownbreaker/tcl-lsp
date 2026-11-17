# 08_itcl_class_namespace.tcl
#
# Proves where an Itcl method body looks up a command it calls. A method, a
# class `proc` and an `itcl::body` all run in the class namespace, not in the
# namespace that lexically holds the class.
#
#   8.6.18 (Itcl 4.3.7) and 9.0.3 (Itcl 4.3.5) agree: the class namespace
#   first, then its own `namespace path` and imports, then global. The
#   enclosing namespace (::ns) and its `namespace path` are never searched, and
#   neither is a base class's namespace for a plain proc.
#
# Run: tclsh8.6 08_itcl_class_namespace.tcl; tclsh9.0 08_itcl_class_namespace.tcl
# Pinned by server/internal/resolve/itcl_class_namespace_test.go; the lookup is
# commandNamespace in server/internal/resolve/resolve.go (since v0.6.17).
package require itcl
puts "tcl [info patchlevel] itcl [package require itcl]"
namespace eval ::util { proc f {} { return ::util::f } }
proc ::g {} { return ::g }
namespace eval ::lib { proc lp {} { return ::lib::lp } }
namespace eval ::src { namespace export *; proc imp {} { return ::src::imp } }
namespace eval ::ns {
    namespace eval util { proc f {} { return ::ns::util::f } }
    proc g {} { return ::ns::g }
    proc lex {} { return ::ns::lex }
    namespace eval nlib { proc pp {} { return ::ns::nlib::pp } }
    namespace path ::ns::nlib
    itcl::class K {
        method qualified {} { return [util::f] }
        method bare {} { return [g] }
        method lexical {} { return [lex] }
        method lexicalPath {} { return [pp] }
        method classNs {} { return [kh] }
        method classPath {} { return [lp] }
        method classImport {} { return [imp] }
        method later
        proc classProc {} { return [g] }
    }
    proc K::kh {} { return ::ns::K::kh }
    # The class namespace has a path of its own (::oo::Helpers ::oo), so append.
    namespace eval K { namespace path [list {*}[namespace path] ::lib]; namespace import ::src::imp }
}
itcl::body ::ns::K::later {} { return [g] }
itcl::class ::Base {}
proc ::Base::bh {} { return ::Base::bh }
itcl::class ::D { inherit ::Base; method baseNs {} { return [bh] } }

set o [::ns::K #auto]
foreach m {qualified bare lexical lexicalPath classNs classPath classImport later} {
    puts "$m: [catch {$o $m} r] $r"
}
puts "classProc: [catch {::ns::K::classProc} r] $r"
puts "baseNs: [catch {[::D #auto] baseNs} r] $r"
# Expected, both versions:
#   qualified: 0 ::util::f       bare: 0 ::g
#   lexical: 1 invalid command name "lex"
#   lexicalPath: 1 invalid command name "pp"
#   classNs: 0 ::ns::K::kh       classPath: 0 ::lib::lp
#   classImport: 0 ::src::imp    later: 0 ::g
#   classProc: 0 ::g             baseNs: 1 invalid command name "bh"
