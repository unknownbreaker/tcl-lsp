# 08_relative_qualified.tcl
#
# Proves how a relative qualified name (`a::b`, no leading `::`) resolves when
# <current>::a::b does not exist. research/02 F-C only tested a target that
# exists, so it read as "relative to the current namespace, full stop".
#
#   Commands, 8.6.18 and 9.0.3: fall back to ::a::b; when both exist, the
#   current-relative target wins.
#   Variables, 8.6.18: fall back too. 9.0.3: error ("no such variable"), so the
#   last line below ends the script there. That error is the expected result.
#
# See research/08-flightaware-corpus-probe.md (miss 1 and miss 10).
#
# Does a relative qualified command name fall back to the global namespace
# when <current>::<relative> does not exist?
puts "tcl [info patchlevel]"
namespace eval ::prom { proc helper {} { return ::prom::helper } }
namespace eval ::other {
    proc caller {} { return [prom::helper] }
}
puts "relative prom::helper from ::other -> [::other::caller]"

# And when BOTH exist, the relative-to-current one wins?
namespace eval ::other::prom { proc helper {} { return ::other::prom::helper } }
puts "with ::other::prom::helper also defined -> [::other::caller]"

# Variables: relative qualified var from inside a proc.
namespace eval ::cfg { variable level 3 }
namespace eval ::user {
    proc readit {} { return $cfg::level }
}
puts "relative \$cfg::level from proc in ::user -> [::user::readit]"
