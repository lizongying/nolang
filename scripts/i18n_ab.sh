#!/bin/bash
# i18n_ab.sh — capture (rc, stdout sha256) fingerprints for a fixed fixture set.
#
# Usage: scripts/i18n_ab.sh <no-binary> <out-file>
#
# Two runs of this script with different binaries must produce identical output
# for the i18n migration to be considered behaviour-preserving in English. Only
# (rc, stdout) is compared, never stderr: an assertion failure still exits 0 and
# would hide a regression, while stderr carries timing and path noise.
#
# The fixtures live at a FIXED path on purpose — diagnostic output embeds the
# file path, so a temp dir per run would make every hash differ.

set -u

BIN="${1:?usage: i18n_ab.sh <no-binary> <out-file>}"
OUT="${2:?usage: i18n_ab.sh <no-binary> <out-file>}"
FIX=/tmp/nolang-i18n-fixtures

REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO" || exit 1

mkdir -p "$FIX"
# Parser error: assignment with a missing right-hand side.
printf 'x = \ny = 1\n' > "$FIX/badparse.no"
# Checker error: undefined identifier used inside an expression.
printf 'f = (a i64) (r i64) {\n  r = a + missing\n}\n' > "$FIX/badname.no"
# Lint: unhandled index result + unused top-level variable.
printf 'vals = [1, 2, 3]\ng = (r i64) {\n  r = vals[0]\n}\n' > "$FIX/badindex.no"

: > "$OUT"

run() {
  local label="$1"; shift
  local stdout rc
  stdout=$("$BIN" "$@" 2>/dev/null)
  rc=$?
  printf '%s|%d|%s\n' "$label" "$rc" "$(printf '%s' "$stdout" | shasum -a 256 | cut -d' ' -f1)" >> "$OUT"
}

run help            --help
run vet-str-json    vet --json src/std/str.no
run vet-str-text    vet src/std/str.no
run vet-vec-json    vet --json src/std/vec.no
run vet-vec-strict  vet --strict --json src/std/vec.no
run vet-io-json     vet --json src/std/io.no
run vet-badparse    vet --json "$FIX/badparse.no"
run vet-badname     vet --json "$FIX/badname.no"
run vet-badindex    vet --json "$FIX/badindex.no"
run build-badparse  build "$FIX/badparse.no"
run build-badname   build "$FIX/badname.no"
run vet-dir-json    vet --json src/std

echo "captured $(wc -l < "$OUT" | tr -d ' ') fingerprints -> $OUT"
