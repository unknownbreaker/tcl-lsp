set cases {
  {if 1 {puts ran}}
  {if 1 then {puts ran}}
  {if 0 {} {puts ran}}
  {if 0 {} else {puts ran}}
  {if 0 {} elseif 1 {puts ran}}
  {if 0 {} elseif 1 then {puts ran} else {}}
  {if 1 {puts ran} else}
  {if 1 {puts ran} elseif}
  {if 1 {puts ran} {} {}}
  {if 1 then}
  {"if" 1 {puts ran} "else" {}}
  {if 1 {puts ran} {else} {}}
  {while {[set ::w 0]} {puts ran} x}
  {for {set i 0} {$i<1} {incr i} {puts ran}}
  {for {set i 0} {$i<1} {incr i}}
  {foreach v {1} {puts ran}}
  {foreach v {1} w {2} {puts ran}}
  {foreach v {1} w {puts ran}}
  {lmap v {1} {puts ran}}
  {dict for {k v} {a 1} {puts ran}}
  {dict for {k v} {a 1}}
  {catch {puts ran}}
  {catch {puts ran} m o}
  {catch {puts ran} m o x}
  {try {puts ran}}
  {try {puts ran} on error {m} {}}
  {try {puts ran} on error {m} {} finally {}}
  {try {puts ran} finally {} on error m {}}
  {try {puts ran} on error}
  {try {puts ran} on error m -}
  {try {puts ran} on ok m - on error m {}}
  {try {puts ran} finally}
  {try {puts ran} finally {} {}}
  {switch a a {puts ran}}
  {switch a {a {puts ran}}}
  {switch a a {puts ran} b}
  {switch a {a {puts ran} b}}
  {switch a a -}
  {switch a b - a {puts ran}}
  {time {puts ran} 1}
  {time {puts ran} 1 2}
}
foreach c $cases {
  set r [catch {uplevel #0 $c} msg]
  puts [format "%-50s %s" $c [expr {$r ? "ERR: [string range $msg 0 50]" : "ok"}]]
}
