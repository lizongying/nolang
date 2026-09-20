#!/usr/bin/env bash
# MIR codegen coverage sweep.
#
# ── 為什麼要改 ────────────────────────────────────────────────────────────
# 這個腳本原本用 `NOLANG_MIR=2` 建置，再靠 grep stderr 裡的 fallback 標記
# （`memory analysis found unsafe constructs` / `EmitLLVM failed` /
# `opt verification failed` / `recovered panic`）把每個檔案分進
# `emitted(MIR)` 或 `fallback(legacy)` 兩桶，用來量「MIR 覆蓋率」。
#
# 但 legacy 後端已經刪掉了（src/build/llvm/ 已移除），`NOLANG_MIR=0` 與 `=2`
# 現在是**硬錯誤**：
#     NOLANG_MIR=2: the legacy codegen backend was removed and MIR is the only
#     backend; unset NOLANG_MIR (or use NOLANG_MIR=1 for the lowering dump)
# 所以舊指令下**每個檔案都進 buildfail**，輸出永遠是
# `total=N emitted(MIR)=0 fallback(legacy)=0 buildfail=N` —— 一個什麼都沒說的數字。
#
# 沒有 fallback 之後，「MIR vs legacy」退化成「MIR 建置成功或失敗」。那幾個標記
# 仍然會被印出來，但意義變成「MIR 在這個檔案上失敗了」，不再是「交給 legacy」。
# 所以現在改成：成功 → ok；失敗 → 從 stderr 抓標記當失敗原因分桶。
#
# 順手修掉另一個讓它全軍覆沒的 bug：原本 `cd "$(dirname "$0")"` 會落到 scripts/，
# 那裡沒有 ./bin/no，所以每一次建置其實都是「找不到執行檔」。
#
# ── 用法 ──────────────────────────────────────────────────────────────────
#   scripts/mir_coverage.sh            STRIDE=8（每 8 個取 1）
#   scripts/mir_coverage.sh 1          全量（很慢）
#   scripts/mir_coverage.sh 8 3        stride=8、從第 3 個開始
#   NO=bin/no_other scripts/mir_coverage.sh 1
set -u
cd "$(dirname "$0")/.." || exit 1
NO=${NO:-./bin/no}
STRIDE=${1:-8}
STARTIDX=${2:-1}
mkdir -p /tmp/mircov
: > /tmp/mircov/ok.txt
: > /tmp/mircov/fail.txt
: > /tmp/mircov/reasons.txt
: > /tmp/mircov/fail.reasons.txt
i=0
total=0
ok=0
fail=0

if [ ! -x "$NO" ]; then
  echo "ERROR: 找不到可執行檔 NO=$NO —— 請先 'make no'" >&2
  exit 2
fi

# emitMIR 在 MIR 處理不了這個檔案時會印出下列其中之一（原本是 fallback 的前置訊息，
# 現在就是失敗原因）。`hir2mir.go` 的 "[MIR] N top-level funcs" 只是普通 debug 行，
# 不是失敗標記，不列入。
MARKERS='memory analysis found unsafe constructs|EmitLLVM failed|opt verification failed|recovered panic'

for f in tests/*.no; do
  i=$((i+1))
  if [ "$STRIDE" != "1" ]; then
    chk=$(( (i - STARTIDX) % STRIDE ))
    [ "$chk" -ne 0 ] && continue
  fi
  total=$((total+1))
  env -u NOLANG_DEBUG_IT NOLANG_MIR_DEBUG=1 "$NO" build "$f" >/dev/null 2>/tmp/mircov/err.txt
  rc=$?
  if [ "$rc" -ne 0 ]; then
    fail=$((fail+1))
    echo "$f" >> /tmp/mircov/fail.txt
    line=$(grep -E "$MARKERS" /tmp/mircov/err.txt | head -1 | sed -E 's/.*\[MIR\] *//')
    if [ -z "$line" ]; then
      # 沒命中已知標記：用 stderr 最後一行，避免整批掉進一個看不出東西的「其他」桶
      line="other: $(grep -v '^$' /tmp/mircov/err.txt | tail -1 | cut -c1-120)"
    fi
    key=$(echo "$line" | sed -E 's/:.*//')
    echo "$key" >> /tmp/mircov/reasons.txt
    echo "$f :: $line" >> /tmp/mircov/fail.reasons.txt
    continue
  fi
  ok=$((ok+1))
  echo "$f" >> /tmp/mircov/ok.txt
done

echo "total=$total ok(MIR built)=$ok buildfail=$fail"
echo "--- build failure reasons (top 25) ---"
sort /tmp/mircov/reasons.txt | uniq -c | sort -rn | head -25
