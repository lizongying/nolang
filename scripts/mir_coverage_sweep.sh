#!/usr/bin/env bash
# MIR coverage sweep — 單後端冒煙（timeout-per-test，避免 stdin/網路/無窮迴圈測試卡住）
#
# ── 為什麼要改 ────────────────────────────────────────────────────────────
# 原本跑兩次做配對：`NOLANG_MIR=2` 當 baseline、`NOLANG_MIR=3` 當受測，再用
# `diff o2 o3` 分出 MATCH / DIVERGE。legacy 後端刪除後，`NOLANG_MIR=0` 與 `=2`
# 都是硬錯誤（"the legacy codegen backend was removed and MIR is the only
# backend"），所以 o2 永遠是空的、rc2 永遠非 0 → 每個能跑的測試都被報成 DIVERGE。
# 沒有第二個後端，DIVERGE 這個分類在定義上就不存在了。
#
# 現在改成單後端跑一遍，按結果分桶：PASS / COMPILE_ERR / RUNTIME_CRASH / HANG。
# 想做「輸出是否變了」的回歸比對，請用 scripts/mir_golden.sh（它對照凍結的
# tests/golden/mir-baseline.tsv，才是後 legacy 時代的對應物）。
#
# 另外修掉兩個讓它在 macOS 上完全跑不起來的問題：
#   - `timeout` 不是 macOS 內建（coreutils 才有）→ 改用 perl alarm 版 run_to，
#     並且用 process group kill，避免超時後 clang/編譯出的程式繼續跑。
#   - `./no` 指向 repo root 一個 2026-09-12 的過期 Mach-O 二元檔 → 改用 ./bin/no。
#
# ── 用法 ──────────────────────────────────────────────────────────────────
#   scripts/mir_coverage_sweep.sh
#   MIR_COV_TIMEOUT=30 scripts/mir_coverage_sweep.sh
#   MIR_COV_GLOB='tests/test-sh*.no' scripts/mir_coverage_sweep.sh   # 只跑子集
#   NO=bin/no_other scripts/mir_coverage_sweep.sh
set -u
export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"
cd "$(dirname "$0")/.." || exit 1
NO=${NO:-./bin/no}
TMO=${MIR_COV_TIMEOUT:-15}
GLOB=${MIR_COV_GLOB:-'tests/*.no'}
pass=0; cerr=0; crash=0; hang=0
> /tmp/mir_cov_pass.txt
> /tmp/mir_cov_cerr.txt
> /tmp/mir_cov_crash.txt
> /tmp/mir_cov_hang.txt

if [ ! -x "$NO" ]; then
  echo "ERROR: 找不到可執行檔 NO=$NO —— 請先 'make no'" >&2
  exit 2
fi

# run_to <seconds> <cmd...> — alarm 版 timeout（超時回傳 142）。
# 天真的 `perl -e 'alarm shift; exec @ARGV'` 只會訊號直接子行程：超時後 perl 死了，
# 但它 exec 的行程（以及 clang / 編譯出的程式 / `no run` 的孫行程）會繼續跑。
# 這裡讓子行程自成 process group（macOS 沒有 setsid，用 setpgid），
# 超時時 `kill -KILL -$pgid` 整棵樹一起砍掉。
run_to() {
  perl -MPOSIX -e '
    my $t = shift;
    my $pid = fork();
    exit 127 unless defined $pid;
    if ($pid == 0) { POSIX::setpgid(0, 0); exec @ARGV; exit 127; }
    my $timedout = 0;
    $SIG{ALRM} = sub { $timedout = 1; kill("-KILL", $pid); kill("KILL", $pid); };
    alarm $t;
    waitpid($pid, 0);
    alarm 0;
    my $st = $?;
    exit 142 if $timedout;
    exit(($st & 127) ? 128 + ($st & 127) : ($st >> 8));
  ' "$@"
}

for f in $GLOB; do
  [ -f "$f" ] || continue
  run_to "$TMO" "$NO" run "$f" >/tmp/o.txt 2>/tmp/e.txt
  rc=$?
  if [ "$rc" = "124" ] || [ "$rc" = "142" ]; then
    hang=$((hang+1)); echo "$f" >> /tmp/mir_cov_hang.txt; continue
  fi
  if [ "$rc" != 0 ]; then
    if grep -q "compilation error" /tmp/e.txt; then
      cerr=$((cerr+1)); echo "$f" >> /tmp/mir_cov_cerr.txt
    else
      crash=$((crash+1)); echo "$f" >> /tmp/mir_cov_crash.txt
    fi
    continue
  fi
  pass=$((pass+1)); echo "$f" >> /tmp/mir_cov_pass.txt
done
echo "=== SUMMARY ==="
echo "PASS=$pass COMPILE_ERR=$cerr RUNTIME_CRASH=$crash HANG=$hang"
