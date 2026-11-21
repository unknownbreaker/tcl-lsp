# A namespace's own `set` may be what any call of `set` resolves to.
namespace eval ::util {
    proc set {args} { return $args }
}
set x $x
