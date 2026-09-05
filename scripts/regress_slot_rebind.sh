#!/bin/bash
# Regression: mine vs baseline across tests/*.no (exit code + output match).
cd /Users/lizongying/IdeaProjects/no
out=/tmp/regress.log
: > "$out"
for f in tests/*.no; do
  ./no run "$f" >/tmp/m.out 2>/dev/null
  me=$?
  ./no-baseline run "$f" >/tmp/b.out 2>/dev/null
  be=$?
  if [ "$me" = "$be" ]; then
    if cmp -s /tmp/m.out /tmp/b.out; then
      status="OK"
    else
      status="DIFF_OUT"
    fi
  else
    status="DIFF_EXIT"
  fi
  printf '%s\tme=%s\tbe=%s\t%s\n' "$status" "$me" "$be" "$f" >> "$out"
done
echo "DONE"
grep -c OK "$out" | sed 's/^/OK_COUNT=/'
grep -v OK "$out" | sed 's/^/MISMATCH=/'
