# The unreachable-code Check's evidence (tclsh 8.6 and 9.0). Each case is run
# as the body of a proc p, after `set d {k v}` (for dict with and dict update),
# with ::ran cleared. Every case ends with `set ::ran 1` after its exit, so
# ran=0 means the command after the exit never ran. A flagged case is shown
# never reaching it. An exempt one either reaches it (return -level 0 goes on)
# or is exempt by design: a break or continue outside a loop is an error Tcl
# reports itself, and a word count hidden by {*} or an option name Tcl
# computes is not read.
#
# Inside a loop, every construct that runs a script in place (if, switch,
# catch, try, eval, uplevel, time, dict with, namespace eval, a [script], an
# expression's [script], for's start script) passes a break or continue on to
# the loop, so the code after one is dead there too.
proc row {kind c} {
  set ::ran 0
  catch {namespace delete ::ns}
  proc p {} "set d {k v}; $c"
  set code [catch {p} res]
  puts [format "%-8s %-68s ran=%s code=%s" $kind $c $::ran $code]
}
puts "== Tcl [info patchlevel]"
foreach c {
  {return; set ::ran 1}
  {return x; set ::ran 1}
  {::return x; set ::ran 1}
  {"return" x; set ::ran 1}
  {return -code ok; set ::ran 1}
  {return -code error x; set ::ran 1}
  {return -code break; set ::ran 1}
  {set c 0; return -code $c x; set ::ran 1}
  {return -lev 0 x; set ::ran 1}
  {return -LEVEL 0 x; set ::ran 1}
  {return -opt {-level 0} x; set ::ran 1}
  {return -level; set ::ran 1}
  {return -errorcode x -errorinfo y x; set ::ran 1}
  {return [list -level 0]; set ::ran 1}
  {return; return; set ::ran 1}
  {error x; set ::ran 1}
  {error x info; set ::ran 1}
  {error x info code; set ::ran 1}
  {::error x; set ::ran 1}
  {if 1 {return; set ::ran 1}}
  {catch {return; set ::ran 1}}
  {set y [return; set ::ran 1]}
  {namespace eval ns {return; set ::ran 1}}
  {foreach v {1} {break; set ::ran 1}}
  {foreach v {1} {continue; set ::ran 1}}
  {foreach v {1} {::break; set ::ran 1}}
  {while 1 {break; set ::ran 1}}
  {for {} 1 {} {break; set ::ran 1}}
  {for {} 1 {break; set ::ran 1} {}}
  {lmap v {1} {break; set ::ran 1}}
  {dict for {k v} {a 1} {break; set ::ran 1}}
  {dict map {k v} {a 1} {continue; set ::ran 1}}
  {foreach v {1} {if 1 {break; set ::ran 1}}}
  {while 1 {if 0 {} elseif 1 {break; set ::ran 1}}}
  {while 1 {if 0 {} else {break; set ::ran 1}}}
  {foreach v {1} {switch a a {break; set ::ran 1}}}
  {foreach v {1} {switch a {a {break; set ::ran 1}}}}
  {foreach v {1} {catch {break; set ::ran 1}}}
  {foreach v {1} {try {break; set ::ran 1} finally {}}}
  {foreach v {1} {try {error x} on error m {break; set ::ran 1}}}
  {foreach v {1} {try {} finally {break; set ::ran 1}}}
  {foreach v {1} {eval {break; set ::ran 1}}}
  {foreach v {1} {uplevel 0 {break; set ::ran 1}}}
  {foreach v {1} {time {break; set ::ran 1}}}
  {foreach v {1} {dict with d {break; set ::ran 1}}}
  {foreach v {1} {dict update d k w {break; set ::ran 1}}}
  {foreach v {1} {namespace eval ns {break; set ::ran 1}}}
  {foreach v {1} {set y [break; set ::ran 1]}}
  {foreach v {1} {set y "[break; set ::ran 1]"}}
  {foreach v {1} {if {[break; set ::ran 1]} {}}}
  {foreach v {1} {set y [expr {[break; set ::ran 1]}]}}
  {foreach v {1} {for {break; set ::ran 1} {0} {} {}}}
  {foreach v {1} {while {[continue; set ::ran 1]} {}}}
  {foreach v {1} {if 1 {foreach w {1} {} ; break; set ::ran 1}}}
} { row flagged $c }
foreach c {
  {return -level 0 x; set ::ran 1}
  {return -level 0; set ::ran 1}
  {return -level 00 x; set ::ran 1}
  {return -code ok -level 0 x; set ::ran 1}
  {return -level 1 x; set ::ran 1}
  {return -options {-level 0} x; set ::ran 1}
  {return {-level} 0 x; set ::ran 1}
  {return "-level" 0 x; set ::ran 1}
  {return -leve\l 0 x; set ::ran 1}
  {set o -level; return $o 0 x; set ::ran 1}
  {return {*}{-level 0} x; set ::ran 1}
  {set y [return -level 0 a; set ::ran 1]}
  {catch {return -level 0; set ::ran 1}}
  {error; set ::ran 1}
  {error a b c d; set ::ran 1}
  {error {*}{x}; set ::ran 1}
  {break; set ::ran 1}
  {continue; set ::ran 1}
  {break x; set ::ran 1}
  {foreach v {1} {break x; set ::ran 1}}
  {foreach v {1} {break {*}{}; set ::ran 1}}
  {if 1 {break; set ::ran 1}}
  {catch {break; set ::ran 1}}
  {time {break; set ::ran 1}}
  {for {break; set ::ran 1} {0} {} {}}
  {while {[break; set ::ran 1]} {}}
  {foreach v {1} {proc q {} {break; set ::ran 1}; q}}
  {foreach v {1} {apply {{} {break; set ::ran 1}}}}
  {foreach v {1} {after 0 {break; set ::ran 1}}}
  {return}
} { row exempt $c }
