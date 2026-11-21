package check

import "github.com/unknownbreaker/tcl-lsp/internal/tcl"

const constantTransformID = "constant-transform"

// transforms is constant-transform's purity table: each pure transform, with
// the shapes of the argument forms it is known to give one result for in Tcl
// 8.6 and 9.0. The evidence is testdata/tclsh/constant_transform.tcl in package
// checkcmd. Constructors (`list`, `join`, `split`, `string repeat`, `format`,
// `string map`) are not transforms of one value and stay out: building a value
// from parts is a readable way to write it.
var transforms = &purityTable{
	severity: Hint,
	commands: map[string][][]argShape{
		"lsort":    {{argList}},
		"lreverse": {{argList}},
		"lrange":   {{argList, argIndex, argIndex}},
	},
	strings: map[string][][]argShape{
		"toupper":   {{argText}},
		"tolower":   {{argText}},
		"totitle":   {{argText}},
		"trim":      {{argText}, {argText, argText}},
		"trimleft":  {{argText}, {argText, argText}},
		"trimright": {{argText}, {argText, argText}},
		"reverse":   {{argText}},
		"range":     {{argText, argIndex, argIndex}},
	},
}

// constantTransform hints at a pure transform builtin called with only literal
// arguments, such as `string toupper abc` or `lsort {c b a}`: its result could
// be written in its place. Text must be printable ASCII, so a newline is never
// in it, and the idiom of trimming a braced block of lines is never flagged.
func constantTransform(c tcl.Command, base int, out []Finding) []Finding {
	return transforms.find(c, base, out)
}
