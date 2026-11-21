# Each body has dead code after an exit: one hint per body, from the first dead
# command to the last.
proc early {x} {
    return $x
    puts "never"
    set y 2   ;# still dead
}

proc fail {} {
    error "no way" ; puts never
}

proc nested {items} {
    foreach item $items {
        if {$item eq "stop"} {
            break
            puts "after break"
        }
        continue
        incr n
    }
    return
    return
}

proc handlers {} {
    try {
        return -code error boom
        set lost 1
    } on error {msg} {
        return $msg
        set lost 2
    }
}

proc inline {} {
    set v [return 1; expr {2}]
}

namespace eval ns {
    return
    variable x 1
}
