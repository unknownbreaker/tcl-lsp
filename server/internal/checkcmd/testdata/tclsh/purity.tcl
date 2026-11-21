# Procs the purity-table transcripts (constant_query.tcl, constant_transform.tcl)
# share. Each script sources this file and prints its rows and fuzz lines.
#
# row runs a command at global level, twice, and prints whether it left every
# variable, command and namespace as it was and gave the same result both
# times ("pure"), and the result.
proc state {} {
  list [lsort [info vars ::*]] [lsort [info commands ::*]] [lsort [namespace children ::]]
}
proc run1 {cmd} {
  set c [catch {uplevel #0 $cmd} res]
  if {$c} { return "ERR [lindex [split $res \n] 0]" }
  return $res
}
proc row {kind cmd} {
  set before [state]
  set r1 [run1 $cmd]
  set r2 [run1 $cmd]
  set pure [expr {$r1 eq $r2 && [state] eq $before ? "pure" : "impure"}]
  puts [join [list $kind $cmd $pure [list $r1]] \t]
}
# words returns every word over alpha, shortest first, up to n chars.
proc words {alpha n} {
  set out [list ""]
  set cur [list ""]
  for {set i 0} {$i < $n} {incr i} {
    set nxt {}
    foreach w $cur { foreach c $alpha { lappend nxt $w$c } }
    lappend out {*}$nxt
    set cur $nxt
  }
  return $out
}
# fuzz runs body with v bound to each word that accept takes, and prints the
# count and CRC-32 of those words, one per line, and of their results.
proc fuzz {name alpha n accept body} {
  set in {}
  set out {}
  set k 0
  foreach v [words $alpha $n] {
    if {![apply [list v $accept] $v]} continue
    incr k
    append in $v\n
    append out [apply [list v $body] $v]\n
  }
  puts [join [list fuzz $name [list $alpha] $n $k [zlib crc32 $in] [zlib crc32 $out]] \t]
}
