# The duplicate-logic Check's evidence (tclsh 8.6 and 9.0). Each row pairs a
# command with the command it reduces to once the repeated part is dropped: the
# repeated operand of || or && (`X || X` reduces to `X || 0`, `X && X` to
# `X && 1`), the elseif branch whose condition repeats an earlier one, or the
# if/else whose bodies are all the same (`if {C} {B} else {B}` reduces to
# `if {C} {}; B`). Both run at global level once for every pair of values of x
# and y, after a reset of n, r and f's counter, and the row says whether the
# two agree on every result and error, and on r. A flagged row always agrees.
# An exempt row is shown to differ, or is exempt by design: operands or
# conditions spelled differently are left alone even where they agree.
#
# NaN: a repeated operand reads the same value twice, so `$x != $x || $x != $x`
# agrees with its reduction for NaN as for any value.
#
# Not modeled: a read trace on a variable runs at each read and may change it,
# and [info frame] or an error's line number tells two equal bodies apart.
expr {srand(1)}
proc f {} { expr {[incr ::n] % 2 == 0} }
proc tcl::mathfunc::g {} { set ::x 1; return 0 }
set values {NaN Inf 0 1 -1 {} abc true no {a b} 0x10 1e400}
proc eval1 {cmd x y} {
  set ::x $x
  set ::y $y
  set ::n 0
  set ::r {}
  set c [catch {uplevel #0 $cmd} res]
  return [list $c [lindex [split $res \n] 0] $::r]
}
proc row {kind cmd reduced} {
  set verdict agree
  foreach x $::values {
    foreach y $::values {
      if {[eval1 $cmd $x $y] ne [eval1 $reduced $x $y]} { set verdict differ }
    }
  }
  puts [join [list $kind $verdict $cmd $reduced] \t]
}
puts "== Tcl [info patchlevel]"
foreach {cmd reduced} {
  {expr {$x == 1 || $x == 1}} {expr {$x == 1 || 0}}
  {expr {$x && $x}} {expr {$x && 1}}
  {expr {$x != $x || $x != $x}} {expr {$x != $x || 0}}
  {expr {$x || $y || $x}} {expr {$x || $y}}
  {expr {$x && ($y && $x)}} {expr {$x && $y}}
  {expr {($x eq "a") || ($x eq "a")}} {expr {($x eq "a") || 0}}
  {expr {$x eq "a" || $x  eq  "a"}} {expr {$x eq "a" || 0}}
  {expr {[f] || $x || $x}} {expr {[f] || $x}}
  {expr {$x || $x || [f]}} {expr {$x || [f]}}
  {if {$x == 1} {set r a} elseif {$x == 1} {set r b}} {if {$x == 1} {set r a}}
  {if {$x} {set r a} elseif {$y} {set r b} elseif {$x} {set r c}} {if {$x} {set r a} elseif {$y} {set r b}}
  {if {$x eq "a"} {set r a} elseif {($x eq "a")} {set r b} else {set r c}} {if {$x eq "a"} {set r a} else {set r c}}
  {if {$x} {set r a} else {set r a}} {if {$x} {}; set r a}
  {if {$x} then {set r a} else {set r a}} {if {$x} {}; set r a}
  {if {$x} {set r a} {set r a}} {if {$x} {}; set r a}
  {if {$x} {set r a} elseif {$y} {set r a} else {set r a}} {if {$x} {} elseif {$y} {}; set r a}
  {if {[f]} {set r a} else {set r a}} {if {[f]} {}; set r a}
} { row flagged $cmd $reduced }
foreach {cmd reduced} {
  {expr {$x || [set x 1; expr 0] || $x}} {expr {$x || [set x 1; expr 0]}}
  {expr {$x || g() || $x}} {expr {$x || g()}}
  {expr {[f] || [f]}} {expr {[f] || 0}}
  {expr {rand() < 0.5 || rand() < 0.5}} {expr {rand() < 0.5 || 0}}
  {expr {$x && $y || $x}} {expr {$x && $y || 0}}
  {expr {$y && $x || $x}} {expr {$y && $x || 0}}
  {expr {$x || $y && $x}} {expr {$x || $y && 0}}
  {expr {$x || ${x}}} {expr {$x || 0}}
  {expr {1 || 1}} {expr {1 || 0}}
  {if {$x} {set r a} elseif {[set x 1; expr 0]} {set r b} elseif {$x} {set r c}} {if {$x} {set r a} elseif {[set x 1; expr 0]} {set r b}}
  {if {$x} {set r a} elseif {g()} {set r b} elseif {$x} {set r c}} {if {$x} {set r a} elseif {g()} {set r b}}
  {if {[f]} {set r a} elseif {[f]} {set r b}} {if {[f]} {set r a}}
  {if {$x} {set r a} elseif $x {set r b}} {if {$x} {set r a}}
  {if {$x} {set r a} elseif {${x}} {set r b}} {if {$x} {set r a}}
  {if {$x} "set r [incr n]" else "set r [incr n]"} {if {$x} {}; set r [incr n]}
  {if {$x} {set r a} elseif {$y} {set r b} else {set r a}} {if {$x} {} elseif {$y} {}; set r a}
  {if {$x} {set r a} else { set r a}} {if {$x} {}; set r a}
  {if {$x} {set r a}} {set r a}
} { row exempt $cmd $reduced }
