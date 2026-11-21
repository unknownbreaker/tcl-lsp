# The constant-condition Check's evidence (tclsh 8.6 and 9.0). Each condition is
# evaluated in four settings: plain; with tcl_precision 3; in a namespace that
# defines the variables, commands and math functions a condition could name; and
# with every comparison's tcl::mathop command redefined. A flagged condition
# gives one value (or one error) in all four. An exempt one is shown varying, or
# is exempt by design: a math function may differ per call (rand), and an operand
# Tcl computes (`1 + 1`, `-1`, a comparison's result) is left alone because its
# string form may hang on tcl_precision, as the 0.1 + 0.2 row shows in 8.6.
# Also exempt: what 8.6 and 9.0 read differently (lt, a leading-zero integer:
# compare the two transcripts), and a condition with no value, one that always
# errors or, through a ?: arm, gives a string where if wants a boolean.
namespace eval probe {
  variable x 5
  proc f {} { return 9 }
  namespace eval tcl::mathfunc { proc abs {v} { return 42 } }
}
proc eval4 {cond} {
  set out {}
  foreach setting {plain precision ns mathop} {
    switch $setting {
      precision { set ::tcl_precision 3 }
      mathop {
        foreach op {== != < > <= >= eq ne in ni lt gt le ge} {
          catch {rename ::tcl::mathop::$op ::saved_$op}
          proc ::tcl::mathop::$op args { return 7 }
        }
      }
    }
    if {$setting eq "ns"} {
      set r [catch {namespace eval probe [list expr $cond]} v]
    } else {
      set r [catch {uplevel #0 [list expr $cond]} v]
    }
    lappend out [expr {$r ? "ERR [lindex [split $v \n] 0]" : $v}]
    switch $setting {
      precision { set ::tcl_precision 0 }
      mathop {
        foreach op {== != < > <= >= eq ne in ni lt gt le ge} {
          rename ::tcl::mathop::$op {}
          catch {rename ::saved_$op ::tcl::mathop::$op}
        }
      }
    }
  }
  return $out
}
proc row {kind cond} {
  set vals [eval4 $cond]
  set same [expr {[llength [lsort -unique $vals]] == 1 ? "same" : "varies"}]
  puts [format "%-8s %-40s %-6s %s" $kind $cond $same [lsort -unique $vals]]
}
puts "== Tcl [info patchlevel]"
foreach c {
  {1 == 1} {"a" eq "a"} {1 < 2} {1 > 2} {2 != 2} {0 != 0} {{a} in {a b}}
  {"a" ne "b" && 1 <= 2} {!(1 >= 2)} {(1) == (1)} {1==1} {0x10 == 16 || "a" ni {b}}
  {1 == 1 ? 1 : 0} {"x" eq "x"} {1 == 2} {1.0 eq 1.00}
  {"10" < "9"} {true == 1}
} { row flagged $c }
foreach c {0 1} { row hint $c }
foreach c {
  {0.1 + 0.2 eq "0.30000000000000004"} {abs(1) == 1} {$x == 5} {[f] == 9}
  {rand() < 2} {"a" eq "a" == 1} {1 + 1 == 2} {-1 < 0}
  {"a" lt "b"} {010 == 8} {09 == 9}
  {1 == 1 ? "a" : "b"} {"a" || 1 == 1} {"a" in {{a}b}}
} { row exempt $c }
