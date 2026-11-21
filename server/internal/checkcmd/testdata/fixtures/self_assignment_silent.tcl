# None of these is a self-assignment, or none is certainly Tcl code.
set x $y
set x "$x "
set x "${x}y"
set x $x$x
set x [set x]
set x
set x $x extra
set ::x $x
set x $::x
set a(1) $a(1)
set a(1) $a(2)
set x:y $x:y
set a::: $a:::
set {x} $x
set x {$x}
set x \$x
set $x $x
set x [list $x]
"set" x $x
{*}set x $x
set {*}{x $x}
my::set x $x
proc set2 {} {}
set2 x $x
# Not certainly code: a custom command's body, a deferred script, a decorated
# proc, a multi-word eval, an uplevel to another frame and a TclOO body.
after 10 { set a1 $a1 }
mywrap { set a2 $a2 }
CACHE_PROC proc cached {} { set a3 $a3 }
eval set a4 { $a4 }
uplevel 1 { set a5 $a5 }
oo::class create K { method m {} { set a6 $a6 } }
# An Itcl class body declares members; it does not run `set`.
itcl::class D {
    set a7 $a7
}
# A comment: set a8 $a8
# namespace eval joins several script words, and a proc of the wrong arity
# is an error: neither braced word runs as written.
namespace eval foo {list} {set a9 $a9}
namespace eval foo bar {set b1 $b1}
proc wrong a b {set b2 $b2}
