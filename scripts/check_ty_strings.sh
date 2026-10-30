#!/usr/bin/env bash
# check_ty_strings.sh — the type-system gate.
#
# A type is a STRUCTURE (src/ty), not a string. This script fails the build when
# a package outside src/ty re-introduces ad-hoc string parsing of a type
# spelling: prefix scans (`HasPrefix(t, "?")`), `|`-splits of union text,
# `strings.Index(t, "]")` bracket scans and friends. Those are exactly the
# heuristics the ty migration removed, and each one silently disagrees with the
# structural answer the moment an alias, a qualifier or an extra space appears.
#
# Legitimate exceptions are registered in ALLOW_FILES below, each with the reason
# its string-level behaviour is the contract. Adding an entry is a deliberate act
# — the gate is supposed to be noisy when new string parsing appears.
#
# Usage: scripts/check_ty_strings.sh [--update]   (--update prints the current
#        baseline rows in ty_strings_baseline.txt format, for review)
set -uo pipefail

cd "$(dirname "$0")/.." || exit 1
SRC=src

# ---------------------------------------------------------------------------
# Classes of type-string parsing. A class is what the ratchet is counted in: the
# baseline below records, per file and class, how many hits exist TODAY. The gate
# fails when a file exceeds its baseline; when a file drops below it, the gate
# says so and the baseline can be lowered (rotating forward, never backward).
#
# The operated-on expression is restricted to names that carry a NOLANG spelling:
# `x.Raw`, `recvType`, `typeName`, `typ`, `elemRaw`, … Deliberately NOT a bare
# `t`/`lt`/`plt`: those are LLVM type names in codegen (`[8 x i8]`, `%vec`), a
# different grammar. Flagging them would make the gate unusable and teach
# everyone to add baselines instead of fixing types.
# ---------------------------------------------------------------------------
NAME='([A-Za-z_]*([Tt]ype|[Rr]aw)[A-Za-z_]*|typ|tin|tout)'
CLASSES=(prefix split trim)
declare -a CLASS_PATTERNS=(
  "HasPrefix\\([^,]*${NAME}[^,]*, *\"[?&*[]\""
  "Split\\([^,]*${NAME}[^,]*, *\"\\|\""
  "TrimPrefix\\([^,]*${NAME}[^,]*, *\"[?&]\""
)

# Files where string-level behaviour is the contract, not a shortcut: printers
# reproduce the source spelling, the plot cache keys on it, the shell-facing
# loader reads it. Everything else is debt tracked by the baseline.
ALLOW_FILES=$(cat <<'EOF'
parser/ast.go
parser/type.go
parser/dump/dump.go
parser/lower.go
parser/resolve.go
EOF
)

BASELINE_FILE=scripts/ty_strings_baseline.txt

is_allowed() {
  printf '%s\n' "$ALLOW_FILES" | grep -qxF "$1"
}

# baseline_count FILE CLASS -> recorded count (0 when absent)
baseline_count() {
  [ -f "$BASELINE_FILE" ] || { echo 0; return; }
  awk -F'\t' -v f="$1" -v c="$2" '$1==f && $2==c { print $3; found=1 } END { if (!found) print 0 }' "$BASELINE_FILE"
}

fails=0
hits=0
report=""
rows=""

scan() { # $1 = extended regexp
  find "$SRC" -name '*.go' \
    -not -path "$SRC/ty/*" \
    -not -name '*_test.go' -print0 \
    | xargs -0 grep -nE "$1" 2>/dev/null
}

for i in "${!CLASSES[@]}"; do
  cls=${CLASSES[$i]}
  pat=${CLASS_PATTERNS[$i]}
  # per-file counts for this class
  counted=$(scan "$pat" | awk -F: '{ print $1 }' | sort | uniq -c)
  while read -r n path; do
    [ -z "${path:-}" ] && continue
    rel=${path#"$SRC"/}
    hits=$((hits + n))
    if is_allowed "$rel"; then
      report="${report}  ok   ${rel} [${cls}] ${n} (allowlisted)"$'\n'
      continue
    fi
    rows="${rows}${rel}\t${cls}\t${n}"$'\n'
    base=$(baseline_count "$rel" "$cls")
    if [ "$n" -gt "$base" ]; then
      fails=$((fails + n - base))
      report="${report}  FAIL ${rel} [${cls}] ${n} > baseline ${base}"$'\n'
    elif [ "$n" -lt "$base" ]; then
      report="${report}  down ${rel} [${cls}] ${n} < baseline ${base} (lower the baseline)"$'\n'
    fi
  done <<EOF2
$counted
EOF2
done

if [ "${1:-}" = "--update" ]; then
  # Emit the baseline rows for the current hits. Review them, then paste over
  # scripts/ty_strings_baseline.txt — the ratchet only ever turns one way, so
  # numbers here should go down, never up.
  printf '%s' "$rows" | sort -t "$(printf '\t')" -k1,1 -k2,2
  echo
  echo "# hits: $hits  (above baseline: $fails)" >&2
  exit 0
fi

printf '%s' "$report"
echo
echo "type-string scan: $hits hit(s), $fails unregistered"

if [ "$fails" -gt 0 ]; then
  cat <<'EOF'

The hits above parse a TYPE out of a string. Types are structures now:

    st := pool.Parse(raw)      // or, on an existing *ty.Type: st.Kind
    switch st.Kind { case ty.Option: ...; case ty.Map: ... }

Replace the prefix/split test with the Kind test (or with TypeRef /
InferredRef / TypeRefOf on the HIR/THIR side), then re-run. If the string form
is genuinely required — a printer, a cache key, a debug dump — register the
file in the ALLOW table at the top of this script with the reason.
EOF
  exit 1
fi

echo "ok: no unregistered type-string parsing outside src/ty"
