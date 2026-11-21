# The ensemble map sends string equal to user code (tclsh 8.6 and 9.0 print 0),
# so only the expression is flagged.
proc myeq {a b} { return 0 }
namespace ensemble configure string -map [dict replace [namespace ensemble configure string -map] equal ::myeq]
string equal $x $x
if {$x eq $x} {}
