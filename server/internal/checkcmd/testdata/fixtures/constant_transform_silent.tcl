# A substitution in any argument of each entry.
string toupper $x
string tolower [f]
string totitle "a$x"
string trim $s
string trim abc $chars
string trimleft $s
string trimleft abc [f]
string trimright $s
string trimright abc $chars
string reverse $s
string range $s 0 end
string range abc $i end
string range abc 0 $j
lsort $list
lreverse [f]
lrange $list 0 end
lrange {a b c} $i end
lrange {a b c} 0 $j
# Expansion, a prefix, a quoted head, options, forms not in the table.
string toupper {*}{abc}
string toup abc
"string" toupper abc
string toupper abc 0 0
string totitle abc 1
string trim a b c
string range abc 0
lsort -unique {b a b}
lsort -integer {10 9}
lrange {a b c} 0
# Values Tcl 8.6 and 9.0 read differently, or that are not printable ASCII.
string range abc 0 e
lrange {a b c} 010 end
string toupper "é"
string trim "\tx"
# A block of lines laid out for reading, then trimmed.
set usage [string trim {
    usage: tool ?-v? file
}]
# Lists that are not plain words.
lsort {{b a} c}
lreverse {a\ b}
# Constructors, sizes and queries.
string repeat - 40
string map {a b} abc
split a,b ,
join {a b} ,
string length abc
# Words that touch, deferred scripts and data.
string toupper "abc"x
lsort {b a}{c}
after 10 {string toupper abc}
set script {lsort {b a}}
