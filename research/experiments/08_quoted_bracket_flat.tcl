# Issue #73: a `]` inside quotes or braces in a bracket script does not end the
# script. Each case prints what x holds at the use; run in tclsh 8.6 and 9.0.

proc quoted {} {
  set x 0
  if {[string length "]"; set x 1] && $x ne {"}} {puts "quoted: x=$x"}
}
proc quotedNextLine {} {
  set x 0
  if {[string length "]"; set x 1]} {}
  puts "quotedNextLine: x=$x"
}
proc braced {s} {
  set x 0
  if {[string map {] [set x 1]} $s] ne {} && $x == 0} {puts "braced: x=$x"}
}
proc inLoop {} {
  set x 0
  set n 0
  while {[incr n] < 3} {
    puts "inLoop pass $n: x=$x"
    if {[string length "]"; set x 1]} {}
  }
}
proc balancedMultiWord {} {
  set x 0
  puts "balancedMultiWord: [expr {[string length "[set x 1]"]} + {$x}]"
}
quoted
quotedNextLine
braced abc
inLoop
balancedMultiWord

# A `"` inside a word is literal, so it cannot pair with the quote that opens "]".
proc midWordQuote {} {
  set x 0
  if {[list a"b "]"; set x 1] && $x ne {"}} {puts "midWordQuote: x=$x"}
}
# A `]` in a comment inside a bracket script does not end the script.
proc commentBracket {} {
  set x 0
  if {[list a;# x]
       set x 1] ne {} && $x} {puts "commentBracket: x=$x"}
}
# A backslash-newline separates words, so the next `"` opens a quote.
proc backslashNewline {} {
  set x 0
  if {[list a\
"]"; set x 1] ne {}} {}
  puts "backslashNewline: x=$x"
}
midWordQuote
commentBracket
backslashNewline

# An array index hides a `]` too: the index runs to its `)`, so x=1 both times.
proc arrayIndex {} {
  set x 0; array set a {] 5}
  if {[list $a(]); set x 1] ne {} && $x} {puts "arrayIndex: x=$x"}
  puts "arrayIndexNextLine: x=$x"
}
# A bracket nested in an index is a command substitution; the script is balanced.
proc arrayIndexNested {} {
  set x 0; array set a {k 5}
  if {[list $a([string trim k])] ne {} && $x == 0} {puts "arrayIndexNested: x=$x"}
}
arrayIndex
arrayIndexNested
