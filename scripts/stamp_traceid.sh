#!/bin/bash
# stamp_traceid.sh — replace each PLACEHOLDER in checker .go files
# with a unique 8-char random base36 string.
#
# Base36 uses [0-9a-z] so IDs are case-insensitive and compact.
#
# If no PLACEHOLDER remains, the script is a no-op so repeated `make`
# without a source reset keeps existing IDs.
#
# Usage: stamp_traceid.sh <file1> [file2 ...]

set -eu

FILES=("$@")
FOUND=0

for f in "${FILES[@]}"; do
    if grep -q '"PLACEHOLDER"' "$f" 2>/dev/null; then
        FOUND=1
        break
    fi
done

if [ "$FOUND" -eq 0 ]; then
    echo "trace IDs already set, skipping"
    exit 0
fi

COUNT=0
for f in "${FILES[@]}"; do
    # Use perl to replace each PLACEHOLDER with a unique 8-char random base36 string.
    # We pick 8 random characters from [0-9a-z] directly.
    # This gives 36^8 ≈ 2.8 trillion possible IDs — far more than enough for unique trace IDs.
    perl -pe '
        sub base36 {
            my @d = (0..9, "a".."z");
            my $s = "";
            $s .= $d[int(rand(36))] for 1..8;
            return $s;
        }
        s/"PLACEHOLDER"/"\"" . base36() . "\""/ge
    ' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    N=$(grep -c 'TraceID:' "$f" 2>/dev/null || true)
    COUNT=$((COUNT + N))
done

echo "trace IDs stamped: $COUNT total in ${#FILES[@]} file(s)"
