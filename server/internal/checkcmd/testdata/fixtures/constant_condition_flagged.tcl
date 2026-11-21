# Conditions that compare only literals: each is a warning.
if {1 == 1} {puts a}
if {"a" eq "a"} {puts b}
while {1 < 2} {break}
for {set i 0} {1 > 2} {incr i} {}
if {$x} {} elseif {2 != 2} {}
if {{a} in {a b}} {}
if {"a" ne "b" && 1 <= 2} {}
if {!(1 >= 2)} {}
if {(1) == (1)} {}
if 1==1 {}
if "1 == 1" then {} else {}
if {0x10 == 16 || "a" ni {b}} {}
if {1 == 1 ? 1 : 0} {}
if {"a\x41" eq "aA"} {}
if {
    1 == \
    1
} {}
::if {true == 1} {}
proc p {} {
    if {"x" eq "x"} { return }
}
namespace eval ns {
    while {0 != 0} {}
}
set n [if {1 == 2} {list a} else {list b}]
foreach v {1} { if {"a" ne "b"} {} }
