#!/bin/sh
W=$(cd "$(dirname "$0")" && pwd)
cat "$W/tests/kernel.no" "$W/tests/$1.txt" > "$W/tests/test-kernel.no"
