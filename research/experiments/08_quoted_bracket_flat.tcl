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
# `$(` indexes the empty-named array, and a qualified name takes an index too.
proc emptyName {} {
  set x 0; set (]) 5
  if {[list $(]); set x 1] ne {}} {}
  puts "emptyName: x=$x"
}
proc qualifiedName {} {
  set x 0; array set ::a {] 5}
  if {[list $::a(]); set x 1] ne {}} {}
  puts "qualifiedName: x=$x"
}
emptyName
qualifiedName
# In an index, quotes, braces and spaces are literal: the nested index closes at
# the first `)`, the outer one swallows `] ` and closes at the second, so `set`
# runs. Tcl 8.6 prints x=1; Tcl 9.0 rejects the `"` ("invalid character in array
# index") and prints x=0, so only the 8.6 answer exercises the parse.
set ::a(x\ \"\ ) V
set ::a(x\ \"V\ \]\ ) 1
proc indexQuote {} {
  set x 0
  catch {if {[list $::a(x "$::a(x " ) ] ) ; set x 1] ne {}} {}}
  puts "indexQuote: x=$x"
}
# A `{` inside a word is literal, so `;# }` is a comment that hides the `]`.
proc midWordBrace {} {
  set x 0
  if {[list a{;# }
   set x 1] ne {}} {}
  puts "midWordBrace: x=$x"
}
indexQuote
midWordBrace
# A braced variable name runs to the first `}`, and a bracket in it is literal.
proc bracedName {} {
  set x 0; set {a]} 1
  if {[list ${a]}; set x 1] ne {}} {}
  puts "bracedName: x=$x"
}
# `${a}(` takes no index, and `a{b}` is literal: both scripts balance.
proc bracedNameNoIndex {} {
  set x 0; set a 1
  if {[list ${a}( a{b}] ne {} && $x == 0} {puts "bracedNameNoIndex: x=$x"}
}
bracedName
bracedNameNoIndex
# Tcl 9.0 nests braces in a braced name: here it is `c{} ; set x 1 ]`, so `set`
# never runs (x=0). Tcl 8.6 ends the name at the first `}` and rejects the
# expression. The versions disagree, so the guard distrusts a `{` or `\` there.
proc bracedNameNests {} {
  set x 0
  set "c\{\} ; set x 1 \]" 5
  catch {if {[list ${c{} ; set x 1 ]}] ne {}} {}}
  puts "bracedNameNests: x=$x"
}
bracedNameNests
