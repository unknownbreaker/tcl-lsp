#!/usr/bin/env bash
# compare.sh [ROOT]: dump every *.tcl file under ROOT with the Go parser and the
# Rust port, and fail on the first file whose dumps differ, naming the file and
# its first differing command and word. ROOT defaults to $TCL_CORPUS, else the
# Homebrew Tcl library; a missing ROOT skips. The Rust dumps of the golden
# fixtures (server/internal/tcl/testdata/parsedump) are checked first.
# The dumps (which hold the corpus source) and logs go in a $TMPDIR directory,
# deleted on success and kept on failure; KEEP_DUMPS=1 keeps them either way.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
golden="$repo/server/internal/tcl/testdata/parsedump"
root=${1:-${TCL_CORPUS:-/opt/homebrew/opt/tcl-tk/lib}}
if [[ -e $root && ! -d $root ]]; then
	echo "corpus: $root is not a directory" >&2
	exit 2
fi
# Absolute, so Go (run from its package directory) and Rust read the same tree.
if [[ -d $root ]]; then root=$(cd "$root" && pwd); fi
out=$(mktemp -d "${TMPDIR:-/tmp}/parsedump.XXXXXX")
cleanup() {
	local status=$?
	if [[ $status -eq 0 && ${KEEP_DUMPS:-} != 1 ]]; then
		rm -rf "$out"
	else
		echo "dumps and logs kept in $out" >&2
	fi
}
trap cleanup EXIT

cargo build --quiet --release --manifest-path "$here/Cargo.toml"
dump="$here/target/release/parsedump"

# commands DUMP: one line per command, trailing comma dropped ("[]" has none).
commands() { awk 'NR == 1 && $0 == "[]" { exit } NR > 1 && $0 != "]" { sub(/,$/, ""); print }' "$1"; }

# first_diff FILE_A FILE_B: print the first differing command, and word when jq
# is installed.
first_diff() {
	local i a b
	i=$(awk 'FILENAME == ARGV[1] { a[FNR] = $0; na = FNR; next } { b[FNR] = $0; nb = FNR }
		END { n = na > nb ? na : nb
			for (k = 1; k <= n; k++) if (!(k in a) || !(k in b) || a[k] != b[k]) { print k - 1; exit } }' \
		<(commands "$1") <(commands "$2"))
	if [[ -z $i ]]; then
		echo "  same commands; the files differ in layout: $(cmp "$1" "$2" 2>&1 || true)"
		return
	fi
	a=$(commands "$1" | sed -n "$((i + 1))p")
	b=$(commands "$2" | sed -n "$((i + 1))p")
	echo "  first differing command: $i"
	echo "  go:   ${a:-(none)}"
	echo "  rust: ${b:-(none)}"
	if command -v jq >/dev/null; then
		# Through files: a command can exceed the argument-size limit.
		printf '%s' "$a" >"$out/diff-go.json"
		printf '%s' "$b" >"$out/diff-rust.json"
		jq -rn --slurpfile a "$out/diff-go.json" --slurpfile b "$out/diff-rust.json" '
			($a[0]) as $a | ($b[0]) as $b
			| if ($a|type) == "array" and ($b|type) == "array" then
				[range([($a|length), ($b|length)] | max)] | map(select($a[.] != $b[.])) | first
				| "  first differing word: \(.)\n  go:   \($a[.] | tojson)\n  rust: \($b[.] | tojson)"
			else empty end' 2>/dev/null || echo "  (jq could not read the command; compare the lines above)"
	fi
}

# dumps DIR: the path of every dump under DIR, relative as ./PATH,
# NUL-separated and sorted; nothing when DIR doesn't exist.
dumps() {
	[[ -d $1 ]] || return 0
	(cd "$1" && find . -name '*.json' -print0 | LC_ALL=C sort -z)
}

# check DIR_A DIR_B: every dump in DIR_A equals the one in DIR_B, and neither
# side has a file the other lacks.
check() {
	local a=$1 b=$2 n=0 f
	while IFS= read -r -d '' f; do
		f=${f#./}
		if [[ ! -f $b/$f ]]; then
			echo "MISMATCH $f: no Rust dump" >&2
			exit 1
		fi
		if ! cmp -s "$a/$f" "$b/$f"; then
			echo "MISMATCH $f" >&2
			first_diff "$a/$f" "$b/$f" >&2
			exit 1
		fi
		n=$((n + 1))
	done < <(dumps "$a")
	local nrust
	nrust=$(dumps "$b" | tr -cd '\0' | wc -c | tr -d ' ')
	if [[ $nrust != "$n" ]]; then
		echo "MISMATCH: $n Go dumps, $nrust Rust dumps" >&2
		exit 1
	fi
	echo "$n files identical"
}

# crashed SIDE ROOT DIR LOG: a dump tool failed. Both write dumps in sorted path
# order, so the first source file without a dump is the one it died on.
crashed() {
	local f
	echo "FAILED: the $1 dump exited non-zero" >&2
	while IFS= read -r -d '' f; do
		f=${f#./}
		if [[ ! -f $3/$f.json ]]; then
			echo "  first file without a $1 dump: $2/$f" >&2
			break
		fi
	done < <(cd "$2" && find . -type f -name '*.tcl' -print0 | LC_ALL=C sort -z)
	echo "  $1 output (first 10 lines; all of it is in $4):" >&2
	head -n 10 "$4" | sed 's/^/    /' >&2
	exit 1
}

# Golden fixtures: the Rust dump must equal the committed Go golden.
mkdir -p "$out/golden-go"
(cd "$golden" && find . -name '*.json' | while IFS= read -r f; do
	mkdir -p "$out/golden-go/$(dirname "$f")"
	cp "$f" "$out/golden-go/${f%.json}.tcl.json"
done)
"$dump" "$golden" "$out/golden-rust" 2>"$out/golden-rust.log" ||
	crashed Rust "$golden" "$out/golden-rust" "$out/golden-rust.log"
echo -n "golden fixtures: "
check "$out/golden-go" "$out/golden-rust"

# compare_tree ROOT LABEL: Go and Rust dumps of every *.tcl file under ROOT agree.
compare_tree() {
	local dir="$out/$2"
	# "$1/": BSD find doesn't descend into a symlinked root otherwise.
	if [[ -z $(find "$1/" -type f -name '*.tcl' -print -quit) ]]; then
		echo "$2 $1: no .tcl files; skipping"
		return
	fi
	PARSEDUMP_OUT="$dir/go" TCL_CORPUS="$1" \
		go -C "$repo/server" test -count=1 -run '^TestParseDumpWrite$' ./internal/tcl >"$dir.go.log" 2>&1 ||
		crashed Go "$1" "$dir/go" "$dir.go.log"
	"$dump" "$1" "$dir/rust" 2>"$dir.rust.log" ||
		crashed Rust "$1" "$dir/rust" "$dir.rust.log"
	echo -n "$2 $1: "
	check "$dir/go" "$dir/rust"
}

# Fuzz: every prefix of every fixture (each cut leaves something unterminated),
# and random text over the bytes the scanner treats specially. Seeded, so a
# failure reproduces.
fuzz="$out/fuzz-src"
mkdir -p "$fuzz"
for f in "$golden"/*.tcl; do
	b=$(basename "$f" .tcl)
	size=$(wc -c <"$f" | tr -d ' ')
	for ((i = 1; i < size; i++)); do head -c "$i" "$f" >"$fuzz/$b.$i.tcl"; done
done
awk -v dir="$fuzz" 'BEGIN {
	srand(80)
	# "|"-separated: awk splits on any whitespace when the separator is " ".
	n = split("{|}|[|]|\"|$|\\|;|#|(|)|*|:|x|y|{*}|${|$a(|\\\n|set|puts|list", a, "|")
	for (k = 0; k < 4; k++) { a[++n] = " "; a[++n] = "\n"; a[++n] = "x"; a[++n] = "}" }
	a[++n] = "\t"; a[++n] = "\r"
	for (f = 0; f < 4000; f++) {
		len = int(rand() * 120); s = ""
		for (i = 0; i < len; i++) s = s a[1 + int(rand() * n)]
		file = sprintf("%s/rand.%04d.tcl", dir, f)
		printf "%s", s > file; close(file)
	}
}'
compare_tree "$fuzz" fuzz

if [[ ! -e $root ]]; then
	echo "corpus: nothing at $root; skipping"
	exit 0
fi
compare_tree "$root" corpus
