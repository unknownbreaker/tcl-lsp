# Shapes Tcl accepts run their bodies: each `set yN $yN` is flagged.
if 1 then {set y1 $y1}
if 0 {} {set y2 $y2}
if 0 {} elseif 1 then {set y3 $y3} else {}
"if" 1 {set y4 $y4} {else} {}
foreach v {1} w {2} {set y5 $y5}
catch {set y6 $y6} m o
try {set y7 $y7} on ok m - on error m {} finally {}
switch a b - a {set y8 $y8}
switch a {a {set y9 $y9}}
# Silent for now: time's script is not walked when a count follows it (#135).
time {set z1 $z1} 1
while {[set z2 $z2]} {}
# Shapes Tcl rejects with wrong # args run nothing: all silent.
if 1 {set n1 $n1} else
if 1 {set n2 $n2} elseif
if 1 {set n3 $n3} {} {}
while {[set n4 $n4]} {set n5 $n5} x
for {set n6 $n6} {0} {}
foreach v {set n7 $n7}
foreach v {1} w {set n8 $n8}
dict for {k v} {set n9 $n9}
catch {set m1 $m1} m o x
try {set m2 $m2} finally {} on error m {}
try {set m3 $m3} on error m -
try {set m4 $m4} finally
switch a a {set m5 $m5} b
switch a {a {set m6 $m6} b}
time {set m7 $m7} 1 2
foreach {*}$args {set m8 $m8}
timerate {set m9 $m9}
