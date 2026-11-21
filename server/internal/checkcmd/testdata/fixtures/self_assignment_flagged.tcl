# Each `set` below assigns a variable its own value.
set x $x
set x ${x}
set x "$x"
set ::g $::g
set ::a::b $::a::b
set a::b $a::b
::set y $y
set n [set m $m]
puts "[set q $q]"
if {[set c $c]} {
    set d $d
} elseif {$c} {
    set e $e
} else {
    set f $f
}
while {[set w $w] < 3} { set w2 $w2 }
for {set i $i} {$i < 3} {set j $j} { set k $k }
foreach v {1 2} { set v1 $v1 }
catch { set c1 $c1 }
try { set t1 $t1 } on error {} { set t2 $t2 } finally { set t3 $t3 }
switch -- $x { a { set s1 $s1 } default { set s2 $s2 } }
eval { set e1 $e1 }
dict for {k v} $dict { set dv $dv }
expr {[set ex $ex]}
proc p {} {
    set local $local
}
namespace eval ns {
    set nv $nv
    proc q {} { set qv $qv }
}
itcl::class C {
    method m {} { set mv $mv }
    public method pm {} { set pmv $pmv }
    constructor {} { set cv $cv }
    destructor { set dv $dv }
    proc cp {} { set cpv $cpv }
}
itcl::body C::later {} { set bv $bv }
