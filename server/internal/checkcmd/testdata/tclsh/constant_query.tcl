# The constant-query Check's evidence (tclsh 8.6 and 9.0). Its purity table
# lists each query it flags, with the shape every argument must have; a call
# with any other argument is silent.
#
#   string index S I          string first S S         string last S S
#   string match S S          string equal S S         string compare S S
#   string is CLASS ?-strict? S                        lindex L I
#
# S is a literal whose value is printable ASCII (0x20-0x7e): Unicode tables and
# 8.6's UTF-16 strings make other text differ by version. CLASS is one of the
# classes below, each of which tests a value char by char; for integer, S is
# also a decimal of at most 9 digits with no leading zero (an optional sign and
# spaces around it), or holds no digit at all. L is an S with no { } " or \, so
# it is a list of the words between its spaces. I is 0, a decimal of at most 9
# digits with no leading zero, end, or end- and such a decimal.
#
# Each row runs a command at global level, twice, and prints whether it left
# every variable, command and namespace as it was and gave the same result
# both times ("pure"), and the result. A flagged row is pure and gives the same
# result in both versions. An exempt row is shown to differ by version, to
# have a substitution, or is exempt by design (a prefix, an option, an entry
# that is not in the table, a constructor). string length and llength are
# left out by design: measuring a literal is a readable way to derive a number
# (`set width [string length {Continue}]` sizes a button from its label).
#
# Each fuzz line enumerates every word up to a length over an alphabet, runs a
# query on each, and prints the count and CRC-32 of the words the table's rule
# accepts and of their results. The two versions print the same line, and the
# Check flags exactly the accepted words (TestConstantQueryTranscript).
set x abc
proc f {} { return abc }
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
puts "== Tcl [info patchlevel]"
foreach cmd {
  {string index "a b" 1}
  {string first {$b} {a $b [c]}}
  {string equal "a\x41\"" {aA"}}
  {string first "" abc}
  {string index abc 1}
  {string index abc end}
  {string index abc end-1}
  {string index abc 5}
  {string first b abcb}
  {string last b abcb}
  {string match {a*[bc]?} abcd}
  {string match "a\\*" {a*}}
  {string equal abc abc}
  {string equal abc abd}
  {string compare abc abd}
  {string is integer 5}
  {string is integer -strict ""}
  {string is integer " -42 "}
  {string is integer 999999999}
  {string is integer abc}
  {string is alpha abc}
  {string is digit -strict 123}
  {string is space " "}
  {string is xdigit 0fF}
  {string is upper ABC}
  {string is lower abc}
  {string is alnum a1}
  {string is ascii abc~}
  {string is punct !?}
  {string is graph a!}
  {string is print "a b"}
  {string is wordchar a_1}
  {string is control abc}
  {lindex "a  b ; c" 2}
  {lindex {$a [b] #c} end}
  {lindex "" 0}
  {lindex {a b c} 1}
  {lindex {a b c} end}
  {lindex {a b c} end-2}
  {lindex {a b c} 7}
  {::string index abc 0}
  {::lindex {a b} 0}
} { row flagged $cmd }
foreach cmd {
  {string length abc}
  {llength {a b c}}
  {string length $x}
  {string length [f]}
  {string length "a$x"}
  {string length {*}{abc}}
  {string length "\u00e9"}
  {string length "a\tb"}
  {string length "\U1F600"}
  {string len abc}
  {"string" length abc}
  {string is int 5}
  {string is integer 1_000}
  {string is integer 09}
  {string is integer 9999999999}
  {string is integer 0d10}
  {string is integer 0x10}
  {string is double 1_0}
  {string is boolean yes}
  {string is integer -failindex i abc}
  {string match -nocase A a}
  {string equal -nocase A a}
  {string compare -length 1 ab ac}
  {string first b abcb 2}
  {lindex {a b c d e f g h i j} 010}
  {lindex {a b c} 09}
  {lindex {a b c} e}
  {lindex {a b c} 1+1}
  {lindex {a b c} end+1}
  {lindex {a b c} 0 0}
  {lindex {a b c}}
  {lindex {{a b} c} 0}
  {llength "\{a"}
  {llength {a "b c"}}
  {llength {a\ b}}
  {string repeat a 3}
  {list a b}
  {dict create a 1}
  {format %d 1}
  {join {a b} ,}
  {concat a b}
  {string toupper abc}
  {lsort {c b a}}
  {string range abc 0 1}
  {after 0 {string length abc}}
} { row exempt $cmd }

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
set ascii {}
for {set c 0x20} {$c < 0x7f} {incr c} { lappend ascii [format %c $c] }
set classes {alnum alpha ascii control digit graph lower print punct space upper wordchar xdigit}
fuzz classes $ascii 1 {expr 1} {
  set r {}
  foreach cls $::classes { lappend r [string is $cls $v] [string is $cls -strict $v] }
  return $r
}
fuzz integer {0 1 8 9 x o b d e _ + - " " a} 4 {
  expr {[regexp {^ *[+-]?(0|[1-9][0-9]{0,8}) *$} $v] || ![regexp {[0-9]} $v]}
} {
  list [string is integer $v] [string is integer -strict $v]
}
fuzz index {0 1 9 e n d - + x _ " "} 4 {
  regexp {^(end|end-(0|[1-9][0-9]{0,8})|0|[1-9][0-9]{0,8})$} $v
} {
  list [lindex {a b c d e f g h i j k} $v] [string index abcdefghijk $v]
}
fuzz match {a b * ? [ ] - \\ ^ !} 3 {expr 1} {
  set r {}
  foreach s [words {a b - ] \\} 2] { append r [string match $v $s] }
  return $r
}
fuzz text {a b " " - \\} 3 {expr 1} {
  set r {}
  foreach s [words {a b} 2] {
    lappend r [string first $s $v] [string last $s $v] [string equal $s $v] [string compare $s $v]
  }
  list [string index $v end] $r
}
fuzz list {a b " " ; # $ [ ]} 4 {expr 1} {
  list [lindex $v 0] [lindex $v end]
}
