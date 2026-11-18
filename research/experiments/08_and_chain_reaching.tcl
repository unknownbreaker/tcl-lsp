# 08_and_chain_reaching.tcl
#
# Proves which bindings reach a use inside or after a braced expression with
# lazy operators. Each case starts with `set x 0` and may run `[set x 1]`; `use`
# records the value x has at the use. A bracket in a condition returns $b, so it
# can be false. Every case runs for each input, and the line shows the values the
# use ever saw: {0 1} means both bindings reach, {1} only the bracket. Expected on
# 8.6.18 and 9.0.3, as TestReachingExpressionStructure in
# server/internal/tcl/reaching_subst_test.go asserts, except index-quote: 9.0
# rejects a `"` in an array index ("invalid character in array index"), so there
# the use never runs and the line is empty.
#
#   if-and-body {1}       an if body after `$c && [set x 1]`
#   else-and {0 1}        else after it
#   if-or-body {0 1}      an if body after `$c || [set x 1]`
#   later-and {1}         `$c && [set x 1] && $x`
#   later-or {1}          `$c || [set x 1] || $x`
#   and-under-or {1}      `$c || [set x 1] && [use $x]`
#   tern-false {0}        `$c ? [set x 1] : $x`
#   after-tern {0 1}      after `$c ? [set x 1] : 0`
#   paren-tern {1}        after `($c ? 1 : 2) + [set x 1]`
#   index-op {1}          after `$a(?) + [set x 1]`
#   index-quote {1}       after `$a(") + [set x 1]`
#   not-chain {0 1}       an if body after `!($c && [set x 1])`
#   paren-chain {1}       an if body after `($c && [set x 1])`
#   func-args {0 1}       `max($c && [set x 1], $x)`
#   nested {1}            `$a || ($b && ($c ? [set x 1] && [use $x] : 0))`
#   while-body {1}        a while body after `$c && [set x 1]`
#   for-body {1}          a for body after `$c && [set x 1]`
#   array index ends at the first `)`: `$a(k(1))` is an error
#
# See docs/BACKLOG.md ("Reaching, after a conditional bracket").
puts "tcl [info patchlevel]"

proc use {v} { lappend ::seen $v; return 1 }

proc case {name inputs body} {
    set ::seen {}
    proc probe {a b c} $body
    foreach in $inputs { catch {probe {*}$in} }
    puts "$name [lsort -unique $::seen]"
}

set bools {{0 0 0} {0 0 1} {0 1 0} {0 1 1} {1 0 0} {1 0 1} {1 1 0} {1 1 1}}

case if-and-body $bools { set x 0; if {$c && [set x 1; expr {$b}]} { use $x } }
case else-and $bools { set x 0; if {$c && [set x 1; expr {$b}]} {} else { use $x } }
case if-or-body $bools { set x 0; if {$c || [set x 1; expr {$b}]} { use $x } }
case later-and $bools { set x 0; expr {$c && [set x 1; expr {$b}] && [use $x]} }
case later-or $bools { set x 0; expr {$c || [set x 1; expr {$b}] || [use $x]} }
case and-under-or $bools { set x 0; expr {$c || [set x 1; expr {$b}] && [use $x]} }
case tern-false $bools { set x 0; expr {$c ? [set x 1] : [use $x]} }
case after-tern $bools { set x 0; expr {$c ? [set x 1] : 0}; use $x }
case paren-tern $bools { set x 0; expr {($c ? 1 : 2) + [set x 1]}; use $x }
case index-op $bools { set x 0; set arr(?) 1; expr {$arr(?) + [set x 1]}; use $x }
case index-quote $bools { set x 0; set arr(\") 1; expr {$arr(") + [set x 1]}; use $x }
case not-chain $bools { set x 0; if {!($c && [set x 1; expr {$b}])} { use $x } }
case paren-chain $bools { set x 0; if {($c && [set x 1; expr {$b}])} { use $x } }
case func-args $bools { set x 0; expr {max($c && [set x 1; expr {$b}], [use $x])} }
case nested $bools { set x 0; expr {$a || ($b && ($c ? [set x 1] && [use $x] : 0))} }
case while-body $bools { set x 0; set n 0; while {$c && [set x 1; expr {$b}] && $n < 1} { use $x; incr n; set x 0 } }
case for-body $bools { set x 0; for {set n 0} {$c && [set x 1; expr {$b}] && $n < 1} {incr n} { use $x; set x 0 } }

set a(k) v
puts "array index ends at the first ): [catch {expr {$a(k(1)) eq "w"}} m] $m"
