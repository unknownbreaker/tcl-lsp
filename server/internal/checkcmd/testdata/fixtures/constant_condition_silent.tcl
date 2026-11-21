# None of these is a constant condition the Check can be sure of.
while 1 { break }
while {1} { break }
for {} 1 {} { break }
for {} {1} {} { break }
while {0} {}
if {!!$x} {}
set d [expr {60 * 60 * 24}]
expr {1 == 1}
if {true} {}
if {!0} {}
if {(1)} {}
if {01} {}
if {1 + 1} {}
if {1 + 1 == 2} {}
if {0.1 + 0.2 eq "0.30000000000000004"} {}
if {-1 < 0} {}
if {"a" eq "a" == 1} {}
if {1 == 1 && $x} {}
if {$x == 1} {}
if {$x == $x} {}
if {[f] == 1} {}
if {rand() < 2} {}
if {abs(1) == 1} {}
if {"$x" eq "a"} {}
if {"\$x" eq "a"} {}
if "$a == 1" {}
if 1==\x31 {}
if {1 == 1 # comment} {}
if {1_000 == 1000} {}
if {Inf > 1} {}
if {t == 1} {}
after 10 {if {1 == 1} {}}
my_if {1 == 1} {}
if {*}{{1 == 1} {}}
if {1 == 1}
if {1 == 1} {} else
while {1 < 2}
for {} {1 < 2} {}
