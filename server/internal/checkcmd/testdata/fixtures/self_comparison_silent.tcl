# NaN makes == != <= >= differ ($x != $x is a NaN test), and in or ni depend on
# the value.
if {$x == $x} {}
if {$x != $x} {}
if {$x <= $x} {}
if {$x >= $x} {}
if {$x in $x} {}
if {$x ni $x} {}
# Two values, or a script that may differ between calls.
if {$x eq $y} {}
if {[f] eq [f]} {}
if {$a([f]) eq $a([f])} {}
if {"[f]" eq "[f]"} {}
if {$x eq ${x}} {}
if {$x eq "$x"} {}
if {$x eq $x[f]} {}
if {f($x) eq f($x)} {}
if {$x + 1 eq $x + 1} {}
# Text the expression parser cannot read for certain.
if {$x eq $x #} {}
if {$x lt $x} {}
if {$x eq} {}
# Substituted twice, or not an expression at all.
expr $x eq $x
expr {$x} eq {$x}
if "$x eq $x" {}
after 10 {if {$x eq $x} {}}
list {$x eq $x}
# Options, other subcommands, prefixes, literals, expansion, other arities.
string equal -nocase $x $x
string compare -length 1 $x $x
string equal $x $y
string equal {$x} {$x}
string equal [f] [f]
string eq $x $x
string first $x $x
string equal $x
string equal $x $x $x
string equal {*}$x {*}$x
string equal $x {*}$x
{string} equal $x $x
string {equal} $x $x
# Rejected by Tcl: words that abut.
string equal "$x""$x"
if {$x eq $x}{}
