# An if or elseif condition of just 0 or 1 disables or forces a block: a hint.
if {0} { puts disabled }
if {1} { puts forced }
if 0 {}
if { 1 } {}
if {$x} {} elseif {0} {} elseif 1 {}
