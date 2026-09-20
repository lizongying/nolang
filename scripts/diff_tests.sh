#!/usr/bin/env bash
# diff_tests.sh — 固定測試集的 MIR 自一致性 / 冒煙檢查
#
# ── 為什麼要改 ────────────────────────────────────────────────────────────
# 這個腳本原本把同一個測試跑兩次再做 diff：
#     NOLANG_MIR=3   → MIR 後端
#     NOLANG_MIR=0   → legacy HIR 後端（對照組）
# 但 legacy 後端已經刪掉了（src/build/llvm/ 已移除），`NOLANG_MIR=0`（以及 =2）
# 現在是硬錯誤：
#     NOLANG_MIR=0: the legacy codegen backend was removed and MIR is the only
#     backend; unset NOLANG_MIR (or use NOLANG_MIR=1 for the lowering dump)
# 也就是說舊腳本的每一次迭代都只是把這句錯誤訊息寫進 /tmp/leg_*.txt，
# 然後 diff 出「整份檔案都不一樣」——看起來像有差異，其實是對照組不存在。
#
# ── 現在比什麼 ────────────────────────────────────────────────────────────
# 預設後端 vs NOLANG_MIR=1，兩者輸出必須**逐位元組相同**。
# NOLANG_MIR=1 現在只是純診斷模式：它先把 lowered MIR + 記憶體分析報告
# dump 到 $TMPDIR 下的 nolang-mir-*.txt，然後**照常**用 MIR 生成程式
# （見 src/build/transpiler.go 的註解）。所以它絕不該影響 codegen；
# 如果哪天影響了，這個腳本就是用來抓到的偵測器。
#
# 注意 NOLANG_MIR=3 雖然不會報錯，但它現在等同於「不設」——只有 0 和 2 被拒絕。
# 與其留一個暗示「還有一種模式」的魔術數字，這裡直接不設，語意比較誠實。
#
# ── 用法 ──────────────────────────────────────────────────────────────────
#   scripts/diff_tests.sh                 跑內建測試集
#   TESTS="test-sha3" scripts/diff_tests.sh          只跑指定測試（空白分隔）
#   NO=bin/no_other scripts/diff_tests.sh            換二元檔比對
#
# 退出碼：0 = 全部相同且成功；1 = 有測試失敗或兩側輸出不一致。
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd /Users/lizongying/IdeaProjects/no || exit 1

NO=${NO:-./bin/no}
TESTS=${TESTS:-"test-std-unix-fs-os test-sha256-block-abc test-x25519-dh-inline test-split test-embed test-sha1-minimal test-hmac2 test-sha3"}

if [ ! -x "$NO" ]; then
  echo "ERROR: 找不到可執行檔 NO=$NO —— 請先 'make no'" >&2
  exit 2
fi

fail=0
for t in $TESTS; do
  # A 側：目前預設後端（MIR）。
  "$NO" run "tests/$t.no" >"/tmp/mir_$t.txt" 2>"/tmp/mir_$t.err"
  rc_a=$?
  # B 側：NOLANG_MIR=1（lowering dump 診斷）。輸出必須與 A 側一致。
  NOLANG_MIR=1 "$NO" run "tests/$t.no" >"/tmp/dump_$t.txt" 2>"/tmp/dump_$t.err"
  rc_b=$?

  echo "=== $t ==="
  echo "default rc=$rc_a   NOLANG_MIR=1 rc=$rc_b"

  if [ "$rc_a" != 0 ]; then
    echo "FAIL: 預設後端 rc=$rc_a"
    sed -n '1,5p' "/tmp/mir_$t.err"
    fail=1
    echo
    continue
  fi
  if [ "$rc_b" != 0 ]; then
    echo "FAIL: NOLANG_MIR=1 rc=$rc_b（診斷模式不該改變結束碼）"
    sed -n '1,5p' "/tmp/dump_$t.err"
    fail=1
    echo
    continue
  fi

  if diff "/tmp/mir_$t.txt" "/tmp/dump_$t.txt" >/tmp/diff_$t.txt 2>&1; then
    echo "ok: 兩側輸出一致（$(wc -l < "/tmp/mir_$t.txt" | tr -d ' ') 行）"
  else
    echo "DIVERGE: NOLANG_MIR=1 的輸出與預設後端不同"
    head -10 /tmp/diff_$t.txt
    fail=1
  fi
  echo
done

if [ "$fail" = 0 ]; then
  echo "結果：✅ 全部通過（預設後端與 NOLANG_MIR=1 輸出一致，且皆成功結束）"
else
  echo "結果：❌ 有失敗或不一致（細節見上方；右側輸出在 /tmp/mir_*.txt、/tmp/dump_*.txt）"
fi
exit "$fail"
