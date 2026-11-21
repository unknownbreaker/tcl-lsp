# Malformed and half-typed conditions give no finding.
if {1 ==} {}
if {== 1} {}
if {(1 == 1} {}
if {1 == 1)} {}
if {1 ? 2} {}
if {1 = 1} {}
if {1 === 1} {}
if {abc == abc} {}
if {"a" == "a} {}
if {{a} == {a} {}
while {1 < } {}
for {} {1 <} {} {}
if {1 == 1} {} elseif {2 ==} {}
if {1 == 1
