# No dead code: the exit is last, may go on, or is not certainly the builtin.
proc last {} {
    puts hi
    return
}

proc level0 {x} {
    return -level 0 $x
    puts "runs"
}

proc options {opts} {
    return -options $opts value
    puts "may run"
}

proc computed {opt} {
    return $opt 0 x
    puts "may run"
}

proc expanded {args} {
    return {*}$args
    puts "may run"
}

proc outsideLoop {} {
    break
    puts "Tcl reports the break itself"
}

foreach v {1 2} {
    proc inner {} {
        continue
        puts "a proc is a frame of its own"
    }
    after 10 {
        break
        puts "a deferred script"
    }
    mycmd {
        break
        puts "a custom command's body"
    }
}

proc conditional {x} {
    if {$x} { return }
    puts "runs when x is false"
}

proc badArity {} {
    error
    puts "wrong # args: left alone"
}

itcl::class C {
    method m {} {
        return
    }
    method n {} {}
}

proc halfTyped {} {
    return
    puts "unterminated
