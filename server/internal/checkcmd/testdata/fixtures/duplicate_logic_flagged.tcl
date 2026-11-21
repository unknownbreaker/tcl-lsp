# Each line below gets one duplicate-logic warning, at the repeated part,
# but for the one with two copies of a condition: its first holds a repeat.
if {$a == 1 || $a == 1} {}
if {$a == 1||$a==1} {}
while {$x && ($y && $x)} {}
for {set i 0} {$i < $n || $i < $n} {incr i} {}
set r [expr {$x || $y || $x}]
if {!$ok && !$ok} {}
if {"a$x" eq "b" || ("a$x" eq "b")} {}
if {$x != $x || $x != $x} {}
if {[f] || $x || $x} {}
if {$x || $x || [f]} {}
if {$x == 1} {a} elseif {$x == 1} {b}
if {$x} {a} elseif {$y} {b} elseif {($x)} {c} else {d}
if {$x} then {a} elseif {$x} then {b}
if {$x} {set r a} else {set r a}
if {$x} then {set r a} else {set r a}
if {$x} {set r a} {set r a}
if {$x} {a} elseif {$y} {a} else {a}
if {[f]} {
    puts same
} else {
    puts same
}
if {($x || $y) || ($x || $y)} {}
if {$x || $y || $z || $y || $x} {}
if {$x || $x} {} elseif {$x || $x} {}
proc p {} {
    if {$a(k) eq "z" || $a(k) eq "z"} {}
}
