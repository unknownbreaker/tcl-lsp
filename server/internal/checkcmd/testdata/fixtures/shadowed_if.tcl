# A user command named if, while or for makes each condition of that name
# uncertain, in every file of the workspace.
proc ::util::if {args} {}
rename while mywhile
if {1 == 1} {}
if {0} {}
while {1 < 2} {}
for {} {1 > 2} {} {}
