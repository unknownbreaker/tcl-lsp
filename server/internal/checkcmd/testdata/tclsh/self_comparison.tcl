# The self-comparison Check's evidence (tclsh 8.6 and 9.0). Each condition is
# evaluated at global level once for every value in $values, with x and a(k)
# holding it, a(j) and (but for the first value) y holding something else, and
# f returning k, k, j in turn. A flagged condition gives one result for every
# value. An exempt one is shown varying, or is exempt by design: an option to string equal or string compare, or two
# operands spelled differently, are left alone even where the result is fixed.
#
# NaN is why == != <= >= are exempt: `set x NaN` makes $x == $x false and
# $x != $x true in both versions, so `$x != $x` is a correct NaN test. eq and
# ne compare strings, and < and > are false for NaN as for every other value.
#
# Not modeled: a read trace on the variable runs between the two reads and may
# change it. That is the one way a flagged condition can vary.
set n 0
proc f {} { expr {[incr ::n] % 3 ? "k" : "j"} }
set values {
  NaN nan -NaN +nan Inf -Inf {} { 1 } { NaN } 0x10 1e400 -1e400 abc {a b}
  true 99999999999999999999 1.5 -0 010 0o7 {{a}b} "\{" ;
}
proc eval1 {cond v} {
  set ::x $v
  # Not a ?: expression: one whose result is NaN raises a domain error.
  set ::y "$v-other"
  if {$v eq [lindex $::values 0]} { set ::y $v }
  set ::i k
  set ::a(k) $v
  set ::a(j) "$v-other"
  set r [catch {uplevel #0 [list expr $cond]} res]
  if {$r} { return "ERR [lindex [split $res \n] 0]" }
  return $res
}
proc row {kind cond} {
  set vals {}
  foreach v $::values { lappend vals [eval1 $cond $v] }
  set u [lsort -unique $vals]
  set same [expr {[llength $u] == 1 ? "same" : "varies"}]
  puts [format "%-8s %-40s %-6s %s" $kind $cond $same $u]
}
puts "== Tcl [info patchlevel]"
foreach c {
  {$x eq $x} {$x ne $x} {$x < $x} {$x > $x} {"$x" eq "$x"} {${x} ne ${x}}
  {$a($i) eq $a($i)} {($x) < ($x)} {!($x eq $x)} {"a$x" ne "a$x"}
  {$::x > $::x} {[string equal $x $x]} {[string compare $x $x]}
  {[::string equal $x $x]} {[string equal "$x" "$x"]}
} { row flagged $c }
foreach c {
  {$x == $x} {$x != $x} {$x <= $x} {$x >= $x} {$x in $x} {$x ni $x}
  {$x eq $y} {[f] eq [f]} {$a([f]) eq $a([f])} {"[f]" eq "[f]"}
  {$x eq ${x}} {$x eq "$x"} {[string equal -nocase $x $x]}
  {[string compare -length 1 $x $x]} {[string equal $x $y]}
  {[string equal {$x} {$x}]}
} { row exempt $c }
