# Each comparison below has two operands that are the same substitution.
if {$x eq $x} {}
if {$x ne $x} {}
while {$x < $x} {}
for {set i 0} {$i > $i} {incr i} {}
if {$a} {} elseif {"$x" eq "$x"} {}
if {${x} ne ${x}} {}
if {$a($i) eq $a($i)} {}
if {($x) < ($x)} {}
if {!($x eq $x)} {}
if {$y == 1 && $x eq $x || $z} {}
if {$c ? $x eq $x : 0} {}
if {"a$x" ne "a$x"} {}
set r [expr {$::x > $::x}]
expr {$x eq $x}
::expr {$x eq $x}
if {$x eq $x && $y ne $y} {}
string equal $x $x
string compare $x $x
::string equal $x $x
string equal "$x" "$x"
string equal $a($i) $a($i)
set r [string equal $x$y $x$y]
if {[string compare $x $x]} {}
proc p {x} {
    if {$x eq $x} {}
    return [string equal $x $x]
}
namespace eval ns {
    if {$x ne $x} {}
}
