# Each pure query in the table, with only literal arguments.
string index "a b" 1
string index abc end-1
string first {$b} {a $b [c]}
string first b abcb
string last b abcb
string match {a*[bc]?} abcd
string equal "a\x41\"" {aA"}
string equal abc abc
string compare abc abd
string is integer 5
string is integer -strict " -42 "
string is alpha abc
string is digit -strict 123
string is wordchar a_1
lindex {a b c} 1
lindex {a b c} end
lindex "a  b ; c" 2
::string index abc 0
::lindex {a b} 0
string index abc"d" 0
# Inside a substitution, a proc and a control-flow body.
set c [string index abc 0]
proc p {} {
    if {[string first a abc] > 0} {
        return [lindex {a b} 0]
    }
}
foreach v {a b} { puts [string is upper A] }
