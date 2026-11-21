# Nothing below gets a duplicate-logic finding.
if {$x || [set x 1; expr 0] || $x} {}
if {$x || g() || $x} {}
if {[f] || [f]} {}
if {rand() < 0.5 || rand() < 0.5} {}
if {$x && $y || $x} {}
if {$x || $y && $x} {}
if {$x || ${x}} {}
if {$a([f]) || $a([f])} {}
if {"[f]" || "[f]"} {}
if {1 || 1} {}
if {0} {a} elseif {0} {b}
if {$x} {a} elseif {[set x 1; expr 0]} {b} elseif {$x} {c}
if {$x} {a} elseif {g()} {b} elseif {$x} {c}
if {[f]} {a} elseif {[f]} {b}
if {$x} {a} elseif $x {b}
if $x {a} elseif $x {b}
if {$x} {a} elseif {${x}} {b}
if {$x} "set r [incr n]" else "set r [incr n]"
if {$x} {a} elseif {$y} {b} else {a}
if {$x} set\ r\ [incr ::n]\;#} else set\ r\ [incr ::n]\;#}
if {$x} {a} else { a}
if {$x} {a}
if {$x} {a} elseif {$y} {a}
if {$x} {a} else {a} extra
if {$x} {a} else
if {$x || $x}{a}
while {$x || $y} {}
after 10 {if {$x || $x} {}}
expr $x || $x
expr {$x ? $y : $y}
set s {if {$x} {a} else {a}}
