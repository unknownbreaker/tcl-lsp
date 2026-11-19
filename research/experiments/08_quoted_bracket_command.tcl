# Issue #78: in a command word, a `]` inside braces or quotes, a comment or a
# `$name(` index does not end a command substitution. Each case prints what x
# holds at the use; run in tclsh 8.6 and 9.0. Both agree on every case but two.
# quotedIndex: 9.0 rejects a `"` in an array index ("invalid character in array
# index"), 8.6 reads the index as running to `)`. nameBackslash: see below.
# bareBrace never sets x; its y=0 shows the braced [set y 2] never ran.

proc quotedWord {} { set x 0; set r "[list {]}; set x 1]"; puts "quotedWord: x=$x r=$r" }
proc bareQuote {} { set x 0; puts [string length "]"; set x 1]; puts "bareQuote: x=$x" }
proc bareBrace {} { set x 0; set s abc; puts [string map {] [set y 2]} $s]; puts "bareBrace: x=$x y=[info exists y]" }
proc quotedIf {} { set x 0; if "[list {]}; set x 1] ne {} && \$x" {puts "quotedIf: x=$x"} }
proc quotedQuote {} { set x 0; set r "[string length "]"; set x 1]"; puts "quotedQuote: x=$x r=$r" }
proc quotedIndex {} { set x 0; array set a {{k " ] } 5}; set r "[list "$a(k " ] )" ; set x 1]"; puts "quotedIndex: x=$x r=$r" }
proc bareIndex {} { set x 0; array set a {{]} 5}; puts [list $a(]); set x 1]; puts "bareIndex: x=$x" }
proc comment {} { set x 0; puts [list a;# x]
  set x 1]; puts "comment: x=$x" }
# A `{*}` prefix makes the brace or quote after it open the word, so it hides a
# `]` too: x=1. After `a`, `{*}` is mid-word and literal, so the `]` closes: x=0.
proc expandBrace {} { set x 0; set r "[list {*}{]} ; set x 1]"; puts "expandBrace: x=$x" }
proc expandQuote {} { set x 0; set r "[list {*}"]" ; set x 1]"; puts "expandQuote: x=$x" }
proc expandMidWord {} { set x 0; set r "[list a{*}{]} ; set x 1]"; puts "expandMidWord: x=$x" }
# A lone `[` in braces is literal, and the script closes at its own `]`: x=1.
proc loneOpen {} { set s a; set i [string first {[} $s]; set x 1; puts "loneOpen: x=$x i=$i" }
# A `${name}` holding `\` is read differently by 8.6 (the name ends at the first
# `}`, here `a\`) and 9.0 (`\}` does not end it, so the name is `a}]`, and the
# `]` after it is part of the name). The server stays silent after it. The
# `set _ "}"` only balances the proc body's braces around the `\}`.
proc nameBackslash {} { set x 0; set "a\\" p; set "a\}\]" q; set r "[list ${a\}] ; set x 1]"; set _ "}"; puts "nameBackslash: x=$x r=$r" }
# A later word of the same command sees the script's binding: prints "1 1".
proc laterWord {} { set x 0; puts "laterWord: [list [list {]}; set x 1] $x]" }
# A lone `[` in braces in a quoted word: x=1.
proc loneOpenQuoted {} { set s a; set x 0; set i "[string map {[ (} $s]"; set x 1; puts "loneOpenQuoted: x=$x i=$i" }
# In a loop, a use before the script sees the earlier binding first, then the
# script's on the next pass: x=0 then x=1.
proc loop {} { set x 0; set n 0; while {$n < 2} { puts "loop: x=$x"; set r "[list {]}; set x 1]"; incr n } }
# A use before the script sees only the earlier binding: x=0.
proc useBefore {} { set x 0; puts "useBefore: x=$x"; set r "[list {]}; set x 1]" }
# An unterminated script is a parse error in both versions, so nothing runs.
proc unterminated {} { eval {set x 0; set r "[list {]} ; set x 1"; puts "unterminated: x=$x"} }
# A balanced braced script: x=1.
proc balancedBraced {} { set x 0; puts [string map {a b} [set x 1]]; puts "balancedBraced: x=$x" }
# A balanced script keeps its reading: x=1, r=1.
proc balanced {} { set x 0; set r "[string length "[set x 1]"]"; puts "balanced: x=$x r=$r" }
foreach p {quotedWord bareQuote bareBrace quotedIf quotedQuote quotedIndex bareIndex comment expandBrace expandQuote expandMidWord loneOpen nameBackslash laterWord loneOpenQuoted loop useBefore unterminated balancedBraced balanced} {
  if {[catch {$p} e]} {puts "$p: ERR $e"}
}
