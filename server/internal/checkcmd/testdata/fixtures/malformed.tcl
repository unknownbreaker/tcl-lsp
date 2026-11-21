# Half-typed code: unterminated words must give no wrong findings.
set x $x[
set y "$y
proc p {} {
    set z ${z
    if {[set w $w} {
