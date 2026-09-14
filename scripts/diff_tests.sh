#!/usr/bin/env bash
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no
for t in test-std-unix-fs-os test-sha256-block-abc test-x25519-dh-inline test-split test-embed test-sha1-minimal test-hmac2; do
  NOLANG_MIR=3 ./bin/no run tests/$t.no >/tmp/mir_$t.txt 2>/dev/null
  NOLANG_MIR=0 ./bin/no run tests/$t.no >/tmp/leg_$t.txt 2>/dev/null
  echo "=== $t ==="
  diff /tmp/mir_$t.txt /tmp/leg_$t.txt | head -10
  echo
done
