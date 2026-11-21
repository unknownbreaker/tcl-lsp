# Each pure transform in the table, with only literal arguments.
string toupper abc
string tolower "A\x42c"
string totitle {hELLO wORLD}
string trim "  x  "
string trim xxaxx x
string trimleft " x "
string trimleft xxa x
string trimright " x "
string trimright axx x
string reverse abc
string range abcdef 1 end-1
lsort {c b a}
lreverse {a $b [c]}
lrange {a b c d} 1 end
::string toupper abc
::lsort {b a}
# Inside a substitution, a proc and a control-flow body.
set s [string toupper abc]
proc p {} {
    if {[string trim { x }] eq "x"} {
        return [lsort {b a}]
    }
}
foreach v {a b} { puts [lreverse {a b}] }
