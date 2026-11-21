# Sizes are left out: measuring a literal is a readable way to derive a number.
set width [string length {Continue}]
llength {a b c}
# A substitution in any argument.
string index $x 0
string index [f] 0
string index "a$x" 0
lindex $list 0
lindex {a b c} $i
llength [list a b]
string is integer $v
string equal abc $y
string first $x abc
string last b [f]
string match $p abc
string compare abc $y
string index abc $i
string is alpha $v
string is xdigit -strict [f]
# Expansion, a prefix, a quoted head, options, forms not in the table.
string index {*}{abc 0}
string ind abc 0
"string" index abc 0
string is int 5
string is integer -failindex i abc
string is double 1.5
string is boolean yes
string match -nocase A a
string equal -nocase A a
string compare -length 1 ab ac
string first b abcb 2
lindex {a b c} 0 0
lindex {a b c}
string index abc
string index abc 0 1
# Values Tcl 8.6 and 9.0 read differently, or that are not printable ASCII.
string is integer 1_000
string is integer 09
string is integer 9999999999
string is integer 0x10
lindex {a b c} 010
lindex {a b c} e
lindex {a b c} 1+1
lindex {a b c} end+1
string index "\u00e9" 0
string index "a\tb" 0
string index {a\
b} 0
# A list that is not plain words.
lindex {{a b} c} 0
llength {a "b c"}
llength {a\ b}
llength "\{a"
# Constructors and transforms.
list a b
dict create a 1
string repeat a 3
format %d 1
join {a b} ,
concat a b
string toupper abc
lsort {c b a}
string range abc 0 1
# Words that touch, deferred scripts and data.
string index "abc"x 0
after 10 {string index abc 0}
set script {lindex {a b} 0}
