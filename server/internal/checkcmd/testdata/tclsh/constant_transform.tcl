# The constant-transform Check's evidence (tclsh 8.6 and 9.0). Its purity table
# lists each transform it hints at, with the shape every argument must have; a
# call with any other argument is silent.
#
#   string toupper S     string tolower S     string totitle S
#   string trim S ?S?    string trimleft S ?S?    string trimright S ?S?
#   string reverse S     string range S I I
#   lsort L              lreverse L           lrange L I I
#
# S, I and L are constant_query.tcl's shapes: S is a literal whose value is
# printable ASCII (0x20-0x7e), so it holds no newline; L is an S with no { } "
# or \; I is 0, a decimal of at most 9 digits with no leading zero, end, or
# end- and such a decimal.
#
# A flagged row is pure and gives the same result in both versions. An exempt
# row is shown to differ by version, to have a substitution, or is exempt by
# design (a prefix, an option, a form that is not in the table, a constructor,
# a block of lines). Constructors (list, join, split, string repeat, string
# map, format, concat) build a value from parts, which is a readable way to
# write it, and a braced block of lines trimmed with string trim is the usual
# way to lay out long text.
#
# Each fuzz line enumerates every word up to a length over an alphabet, runs
# the transforms on each, and prints the count and CRC-32 of the words the
# table's rule accepts and of their results. The two versions print the same
# line, and the Check flags exactly the accepted words
# (TestConstantTransformTranscript).
set x abc
set l {b a}
set i 0
proc f {} { return abc }
source [file join [file dirname [info script]] purity.tcl]
puts "== Tcl [info patchlevel]"
foreach cmd {
  {string toupper abc}
  {string toupper "a b~z"}
  {string toupper {$x [f]}}
  {string toupper ""}
  {string tolower ABC}
  {string tolower "A\x42c"}
  {string totitle "hELLO wORLD"}
  {string totitle 1abc}
  {string trim "  x  "}
  {string trim xxaxx x}
  {string trim "--a--" -}
  {string trim abc ""}
  {string trimleft " x "}
  {string trimleft xxa x}
  {string trimright " x "}
  {string trimright axx x}
  {string reverse abc}
  {string reverse "a\\b"}
  {string range abcdef 1 end-1}
  {string range abc 2 1}
  {string range abc end end}
  {string range abc 0 9}
  {lsort {c b a}}
  {lsort {b #a}}
  {lsort {B a A b}}
  {lsort "a  b ; c"}
  {lsort ""}
  {lreverse {a b c}}
  {lreverse {a $b [c] ; #}}
  {lrange {a b c d} 1 end}
  {lrange {#a b} 0 0}
  {lrange {a b c} 2 1}
  {::string toupper abc}
  {::lsort {b a}}
} { row flagged $cmd }
foreach cmd {
  {string toupper $x}
  {string toupper [f]}
  {string toupper "a$x"}
  {lsort $l}
  {string range abc 0 $i}
  {string toupper {*}{abc}}
  {string toup abc}
  {lsor {b a}}
  {"string" toupper abc}
  {string toupper abc 0 0}
  {string totitle abc 1}
  {string trim a b c}
  {string range abc 0}
  {lsort -unique {b a b}}
  {lsort -integer {10 9}}
  {lsort -decreasing {a b}}
  {lrange {a b c} 0}
  {string range abc 0 e}
  {string range abc 09 end}
  {lrange {a b c d e f g h i j} 010 end}
  {lrange {a b c} e end}
  {string range abc 0 end+1}
  {string toupper "é"}
  {string totitle "ǆ"}
  {string trim " x"}
  {string trim "\tx\n"}
  {string trim "\n  a block of one line\n"}
  {lsort {{b a} c}}
  {lsort {a "b c"}}
  {lreverse {a\ b}}
  {string repeat - 3}
  {string map {a b} abc}
  {split a,b ,}
  {join {a b} ,}
  {list a b}
  {format %s a}
  {concat a b}
  {string length abc}
  {after 0 {string toupper abc}}
} { row exempt $cmd }

set ascii {}
for {set c 0x20} {$c < 0x7f} {incr c} { lappend ascii [format %c $c] }
fuzz case $ascii 2 {expr 1} {
  list [string toupper $v] [string tolower $v] [string totitle $v] [string reverse $v] \
    [string trim $v] [string trimleft $v] [string trimright $v]
}
fuzz trim {a b " " - \\ x} 3 {expr 1} {
  set r {}
  foreach s [words {" " - a} 2] {
    lappend r [string trim $v $s] [string trimleft $v $s] [string trimright $v $s]
  }
  return $r
}
fuzz range {0 1 9 e n d - + x _ " "} 4 {
  regexp {^(end|end-(0|[1-9][0-9]{0,8})|0|[1-9][0-9]{0,8})$} $v
} {
  list [string range abcdefghijk $v end] [string range abcdefghijk 0 $v] \
    [lrange {a b c d e f g h i j k} $v end] [lrange {a b c d e f g h i j k} 0 $v]
}
fuzz list {a A b " " ; # $ [ ]} 4 {expr 1} {
  list [lsort $v] [lreverse $v] [lrange $v 0 end] [lrange $v 1 end-1]
}
