#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# selfhost_diff.sh — 自舉編譯器（noc）的五級差分門禁
# ═══════════════════════════════════════════════════════════════════════════
#
# 設計文檔：docs/design/nolang-selfhost.md §7（驗證策略）
#
# 目的：把「noc 重現 no 的行為」這件事變成一個**可執行、會失敗**的判據。
#   SUT（受測）通常是 /tmp/noc-s0（nolang 寫的編譯器）
#   CTL（控制組）通常是 ./bin/no（Go 寫的、可信的尺子）
#   對同一份語料跑同一條子命令，比對 (rc, sha256(stdout))。
#
# ── 為什麼「比對兩邊輸出」本身不夠 ──────────────────────────────────────────
# 一個壞掉的差分 harness 最常見的失敗不是「報錯」，而是**報平安**：
#   * 兩邊都因為 PATH 少了 clang 而全部 rc!=0 ⇒ 「rc 全等」看起來很一致；
#   * 語料路徑寫錯 ⇒ 兩邊都跑 0 個檔案 ⇒ 「0 differ」完美；
#   * SUT 什麼都沒實作 ⇒ 全部 rc!=0、stdout 全空 ⇒ 只要控制組也有一樣的
#     症狀就會被當成 SAME。
# 所以每個 level 都必須同時輸出「控制組的可用度」與「空輸出計數」，
# 並且在控制組本身不可用時**直接作廢整輪**（不是報 0 differ）。
# 這一節的規則和 scripts/mir_golden.sh 的 pollution/smoke guard 是同一條原則。
#
# ── 五個 level ────────────────────────────────────────────────────────────
#   L0  單元測試   compiler/tests/*_test.no 經 `run` 執行，比 (rc, stdout)
#   L1  詞法       `<ctl> tokens <file>`，比 (rc, sha256(stdout))
#                  ⚠️ oracle **不是** bin/no：`no` 沒有 `tokens`（§8 P0-②）。
#                  用 --l1-ctl 指向一個只讀 dumper（見 docs §7.1.1）；
#                  沒給、或它不支援 tokens ⇒ L1 誠實報 UNAVAILABLE。
#   L2  語法       `no ast`，比 (rc, stdout) **且**比失敗集合與失敗位置
#   L3  語義       `no vet`，歸一化後逐行比 + `[ERROR]` 集合 diff
#   L4  行為       與 tests/golden/mir-baseline.tsv 的凍結指紋比
#
# ── 退出碼 ────────────────────────────────────────────────────────────────
#   0  所有被要求的 level 都 PASS
#   1  至少一個 level FAIL
#   2  用法錯誤
#   3  **harness 自檢失敗**（假通過防護被觸發）⇒ 本輪結果作廢
#   4  至少一個 level UNAVAILABLE（oracle 不存在／不可信），且沒有 FAIL
#
# ── 用法 ──────────────────────────────────────────────────────────────────
#   scripts/selfhost_diff.sh                          # 預設 SUT=/tmp/noc-s0
#   scripts/selfhost_diff.sh --sut /tmp/noc-s0 --levels L0,L2,L3
#   scripts/selfhost_diff.sh --self-test              # SUT:=CTL，硬斷言見下
#   scripts/selfhost_diff.sh --glob 'arr*.no' --limit 20   # 快速迭代
#   scripts/selfhost_diff.sh --sut /tmp/noc-l1 --levels L1 --l1-ctl /tmp/notok/notok
#
# ⚠️ L4 的 oracle 只涵蓋 tests/：`--corpus src/std` 之類的組合會量到 0 檔，
#    此時 L4 記 UNAVAILABLE（[G4]），不是 FAIL 也不是 PASS。
#
# ⚠️ 兩種「證明 harness 不是橡皮圖章」的跑法（兩邊都要做）：
#      scripts/selfhost_diff.sh --self-test                 ⇒ 不得有任何 FAIL
#      scripts/selfhost_diff.sh --sut /tmp/noc-s0           ⇒ 必須 L0/L2/L3/L4 FAIL
#   只做後者會不知道「FAIL」是 SUT 的錯還是 harness 的錯；
#   只做前者則會漏掉「harness 對所有東西都說 PASS」這種壞法。
#
# --self-test 的契約（刻意**不**要求「全 PASS」）：
#     (a) 不得有任何 level 判 FAIL / SELFCHECK-FAIL  ← 假陰性偵測
#     (b) 至少 1 個 level PASS                        ← 防止自檢本身空洞
#     UNAVAILABLE 是允許的：L1 的 oracle 預設不存在（沒有 `no tokens`），
#     golden 可能過期，語料可能與 golden 無交集。把 UNAVAILABLE 當失敗會讓自檢誤報。
# ═══════════════════════════════════════════════════════════════════════════

set -u

cd /Users/lizongying/IdeaProjects/no || exit 2

# ── 預設值 ────────────────────────────────────────────────────────────────
SUT=${SUT:-/tmp/noc-s0}
CTL=${CTL:-./bin/no}
LEVELS=${LEVELS:-L0,L1,L2,L3,L4}
CORPUS=${CORPUS:-"tests src/std"}
GLOB=${GLOB:-'*.no'}
LIMIT=${LIMIT:-0}              # 0 = 不限
TMO=${TMO:-60}                 # 單檔逾時（秒）
WORKDIR=${WORKDIR:-/tmp/selfhost_diff}
L4_GOLDEN=${L4_GOLDEN:-tests/golden/mir-baseline.tsv}
L4_SELFCHECK=${L4_SELFCHECK:-3}   # 控制組 vs golden 的抽樣自檢檔數（0=跳過）
# L1 的 oracle **不是** `bin/no`：`no` 沒有 `tokens` 子命令（§8 P0-②）。
# 目前用一個 repo 之外的只讀 dumper（直接 import src/lexer，見文檔 §7.1.1）。
# 預設＝$CTL ⇒ 若它不支援 `tokens`，L1 會誠實地報 UNAVAILABLE。
L1_CTL=${L1_CTL:-}
SELF_TEST=0
VERBOSE=0

usage() {
  sed -n '2,60p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --sut)       SUT="$2"; shift 2 ;;
    --ctl)       CTL="$2"; shift 2 ;;
    --levels)    LEVELS="$2"; shift 2 ;;
    --corpus)    CORPUS="$2"; shift 2 ;;
    --glob)      GLOB="$2"; shift 2 ;;
    --limit)     LIMIT="$2"; shift 2 ;;
    --timeout)   TMO="$2"; shift 2 ;;
    --workdir)   WORKDIR="$2"; shift 2 ;;
    --l4-golden) L4_GOLDEN="$2"; shift 2 ;;
    --l4-selfcheck) L4_SELFCHECK="$2"; shift 2 ;;
    --l1-ctl)    L1_CTL="$2"; shift 2 ;;
    --self-test) SELF_TEST=1; shift ;;
    -v)          VERBOSE=1; shift ;;
    -h|--help)   usage ;;
    *) echo "unknown option: $1" >&2; usage ;;
  esac
done

# --self-test：把 SUT 指到控制組，於是「每一格都應該相同」。
# 這是唯一能抓出「harness 對所有東西都說 PASS」的測試。
if [ "$SELF_TEST" = "1" ]; then
  SUT="$CTL"
  echo "[self-test] SUT := CTL = $CTL  —— 預期：所有 level 全 PASS"
fi

mkdir -p "$WORKDIR" || exit 2
# ⚠️ 必須清掉 *.status。L4 的 SUT-vs-golden 迴圈被 `L4.status == UNAVAILABLE` 短路，
# 於是**上一輪**留下的 UNAVAILABLE/FAIL 會被當成這一輪的結果（實測踩過：
# 先跑 `--corpus src/std` 得到 L4=UNAVAILABLE，接著跑 `--corpus tests` 時
# L4 直接沿用舊狀態、連「量測 N 檔」都沒印 —— 答案剛好對，純屬僥倖）。
# 同族的錯還有 §7.2 的「golden 過期」與「binary 被別的 session 重建」。
rm -f "$WORKDIR"/*.tsv "$WORKDIR"/*.txt "$WORKDIR"/*.status "$WORKDIR"/o.* "$WORKDIR"/e.* 2>/dev/null

EMPTY_SHA=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

# ── 工具 ──────────────────────────────────────────────────────────────────

# run_to <秒> <cmd...> — 帶逾時的執行；逾時殺整個 process group。
# 直接照抄 scripts/mir_golden.sh 的實作（perl 只殺直接子行程是不夠的：
# clang / 被編出來的程式 / no run 的孫行程都會活下來）。
# 逾時回 142，這裡正規化成 124。
run_to() {
  perl -MPOSIX -e '
    my $t = shift;
    my $pid = fork();
    exit 127 unless defined $pid;
    if ($pid == 0) { POSIX::setpgid(0, 0); exec @ARGV; exit 127; }
    my $timedout = 0;
    $SIG{ALRM} = sub {
      $timedout = 1;
      kill("-KILL", $pid);
      kill("KILL", $pid);
    };
    alarm $t;
    waitpid($pid, 0);
    alarm 0;
    my $st = $?;
    exit 142 if $timedout;
    exit(($st & 127) ? 128 + ($st & 127) : ($st >> 8));
  ' "$@"
}

sha_of() { shasum -a 256 "$1" 2>/dev/null | awk '{print $1}'; }

cnt() { wc -l < "$1" 2>/dev/null | tr -d ' '; }

# 收集語料：對每個 corpus 目錄做 find，套 GLOB，排序（確定性），再套 LIMIT。
collect_corpus() {
  : > "$WORKDIR/files.all"
  for d in $CORPUS; do
    [ -d "$d" ] || continue
    find "$d" -name "$GLOB" -type f >> "$WORKDIR/files.all"
  done
  sort -u "$WORKDIR/files.all" > "$WORKDIR/files.sorted"
  if [ "$LIMIT" != "0" ]; then
    head -n "$LIMIT" "$WORKDIR/files.sorted" > "$WORKDIR/files.txt"
  else
    cp "$WORKDIR/files.sorted" "$WORKDIR/files.txt"
  fi
  cnt "$WORKDIR/files.txt"
}

# ── 歸一化 ────────────────────────────────────────────────────────────────

# norm_diag：把診斷行開頭的「檔案路徑」拿掉，保留 `line:col: 其餘`。
# 為什麼：`no vet` 的 HINT 會歸屬到 std 檔案，而**歸屬到哪一個是逐次不同的**
# （多個 std 檔在同行同列有相同樣板，去重時「誰先被訪問誰得名」）。
# 丟掉檔名後同一份輸入的輸出才穩定。
norm_diag() {
  sed -E 's#^[^:]*:([0-9]+):([0-9]+): #\1:\2: #' "$1"
}

# 只留 [ERROR] 行（L3 的硬判據），同樣歸一化。
errors_only() {
  grep -E '\[ERROR\]' "$1" 2>/dev/null | sed -E 's#^[^:]*:([0-9]+):([0-9]+): #\1:\2: #' | sort
}

# 版本不匹配警告是**常數型**差異：Makefile 用 -ldflags 注入 git commit，
# 而裸 go build 停在 "dev"，於是每檔都吐一行。
# 常數型 stderr 差異＝假差異 ⇒ 一律濾掉。
strip_noise() {
  grep -v -E '^warning: compiler version mismatch' "$1" 2>/dev/null
}

# ── 指紋 ──────────────────────────────────────────────────────────────────
# fp <binary> <subcommand|-> <file> <tag>  ⇒ 印 "<rc> <sha256(stdout)>"
# 子命令為 "-" 時直接跑 `<binary> <file>`（L0 的 run、L4 的 run 都用得上）。
fp() {
  local bin="$1" sub="$2" f="$3" tag="$4"
  local o="$WORKDIR/o.$tag" e="$WORKDIR/e.$tag"
  local rc
  if [ "$sub" = "-" ]; then
    run_to "$TMO" "$bin" "$f" >"$o" 2>"$e"
  else
    run_to "$TMO" "$bin" "$sub" "$f" >"$o" 2>"$e"
  fi
  rc=$?
  [ "$rc" = "142" ] && rc=124
  local h
  h=$(sha_of "$o"); [ -z "$h" ] && h="$EMPTY_SHA"
  echo "$rc $h"
}

# ── 每個 level 的統計（供報表與防護使用）────────────────────────────────────
# 檔案：$WORKDIR/<lvl>.tsv，每行 "ctl_rc ctl_hash sut_rc sut_hash path"
# 由 run_diff_level 產生。

# run_diff_level <lvl> <subcommand>
#   subcommand = "-" ⇒ 直接 `<bin> <file>`
run_diff_level() {
  local lvl="$1" sub="$2"
  local tsv="$WORKDIR/$lvl.tsv"
  : > "$tsv"
  while IFS= read -r f; do
    local tag
    tag=$(echo "$lvl.$f" | tr '/ ' '__')
    local c s
    c=$(fp "$CTL" "$sub" "$f" "c.$tag")
    s=$(fp "$SUT" "$sub" "$f" "s.$tag")
    echo "$c $s $f" >> "$tsv"
  done < "$WORKDIR/files.txt"
}

# guard_level <lvl> — 假通過防護；印統計行並在必要時**設定 status**。
# 回 0 = 這個 level 有訊號、可以繼續判 PASS/FAIL
# 回 1 = 已經被 G1/G3 定為 SELFCHECK-FAIL / UNAVAILABLE，呼叫者不應再判 PASS
guard_level() {
  local lvl="$1" tsv="$WORKDIR/$1.tsv"
  local files ctl_ok ctl_empty sut_ok sut_empty
  files=$(cnt "$WORKDIR/files.txt")
  ctl_ok=$(awk '$1==0' "$tsv" | wc -l | tr -d ' ')
  ctl_empty=$(awk -v e="$EMPTY_SHA" '$1==0 && $2==e' "$tsv" | wc -l | tr -d ' ')
  sut_ok=$(awk '$3==0' "$tsv" | wc -l | tr -d ' ')
  sut_empty=$(awk -v e="$EMPTY_SHA" '$3==0 && $4==e' "$tsv" | wc -l | tr -d ' ')
  echo "  files=$files  ctl_ok=$ctl_ok  ctl_empty=$ctl_empty  sut_ok=$sut_ok  sut_empty=$sut_empty"

  # G1：控制組一個都跑不動 ⇒ 這條 level 的 oracle 不存在，整輪作廢。
  if [ "$files" -gt 0 ] && [ "$ctl_ok" = "0" ]; then
    echo "  [G1] 控制組 rc==0 的檔案數 = 0/$files ⇒ oracle 不可用，本輪結果作廢"
    echo "       （典型原因：binary 不存在／PATH 少了 clang／語料路徑錯）"
    echo "SELFCHECK-FAIL" > "$WORKDIR/$lvl.status"
    return 1
  fi
  # G2：控制組可用，但 SUT 一個都跑不動 ⇒ 這是「SUT 什麼都沒做」，
  #     必須講成 mass-noop，而不是報 N 個 DIFF（後者會被誤讀成「差一點」）。
  if [ "$ctl_ok" -gt 0 ] && [ "$sut_ok" = "0" ]; then
    echo "  [G2] SUT rc==0 的檔案數 = 0/${files}（控制組 ${ctl_ok}/${files}）"
    echo "       ⇒ SUT 完全沒有實作這條子命令（mass-noop），不是「差一點」"
  fi
  # G3：控制組自己就全是空輸出 ⇒ 這條 level 沒有訊號，不能算 PASS。
  if [ "$files" -gt 0 ] && [ "$ctl_empty" = "$files" ]; then
    echo "  [G3] 控制組 $files/$files 檔的 stdout 都是空字串 ⇒ 此 level 無訊號"
    echo "UNAVAILABLE" > "$WORKDIR/$lvl.status"
    return 1
  fi
  # G7：控制組在整批語料上只吐**一種** stdout ⇒ oracle 沒有鑑別力。
  #     實測踩過：把 `bin/no` 當 L1 的 oracle，而 `no` 沒有 `tokens` 子命令
  #     ⇒ 它對**任何**輸入都印同一份 usage（rc=0、非空）。此時「SUT 與控制組
  #     不同」不是 SUT 的錯，而是 oracle 不存在；報 FAIL 會把責任推錯人。
  #     （rc=0、stdout 非空 ⇒ G1/G2/G3 都攔不住，所以需要這一條。）
  if [ "$files" -ge 3 ]; then
    local ctl_distinct
    ctl_distinct=$(awk '{print $2}' "$tsv" | sort -u | wc -l | tr -d ' ')
    if [ "$ctl_distinct" = "1" ]; then
      echo "  [G7] 控制組在 $files 檔上只產生 1 種 stdout 雜湊 ⇒ oracle 無鑑別力"
      echo "       （典型原因：oracle 不支援這條子命令，對任何輸入都印同一份 usage）"
      echo "UNAVAILABLE" > "$WORKDIR/$lvl.status"
      return 1
    fi
  fi
  return 0
}

# report_level <lvl> — guard + 比 (rc, hash) + 寫 PASS/FAIL
report_level() {
  local lvl="$1" tsv="$WORKDIR/$1.tsv"
  local files same diff
  guard_level "$lvl" || return 0
  files=$(cnt "$WORKDIR/files.txt")
  same=$(awk '$1==$3 && $2==$4' "$tsv" | wc -l | tr -d ' ')
  diff=$((files - same))
  echo "  SAME=$same  DIFF=$diff"
  if [ "$diff" = "0" ]; then
    echo "PASS" > "$WORKDIR/$lvl.status"
  else
    echo "FAIL" > "$WORKDIR/$lvl.status"
    echo "  --- DIFF（最多 10 筆）---"
    awk '$1!=$3 || $2!=$4 {print "    " $5 "  ctl=(" $1 "," substr($2,1,12) ")  sut=(" $3 "," substr($4,1,12) ")"}' "$tsv" | head -10
    [ "$diff" -gt 10 ] && echo "    … 其餘 $((diff - 10)) 筆見 $tsv"
  fi
}

# ── 前置檢查 ──────────────────────────────────────────────────────────────
echo "=== selfhost_diff: SUT=$SUT  CTL=$CTL ==="
if [ ! -x "$SUT" ]; then
  echo "ERROR: SUT 不是可執行檔：$SUT" >&2
  echo "       （M0 階段用：./bin/no build -o /tmp/noc-s0 compiler）" >&2
  exit 2
fi
if [ ! -x "$CTL" ]; then
  echo "ERROR: CTL 不是可執行檔：$CTL" >&2
  exit 2
fi
# 記錄兩支 binary 的指紋：若 sweep 期間有別的 session 重建它們，本輪作廢。
# ⚠️ 並行 session 在本 repo 是**常態**（實測踩過：`bin/no` 在 sweep 進行中被重建，
# mtime 由 21:09:44 變成 21:55:31）。只印不檢查等於沒有防護，所以收尾時會重算比對（G6）。
CTL_FP=$(sha_of "$CTL")
SUT_FP=$(sha_of "$SUT")
GOLDEN_FP=""
[ -f "$L4_GOLDEN" ] && GOLDEN_FP=$(sha_of "$L4_GOLDEN")
echo "ctl: $(ls -l "$CTL" | awk '{print $5}') bytes  ${CTL_FP:0:16}  mtime=$(stat -f %Sm "$CTL" 2>/dev/null)"
echo "sut: $(ls -l "$SUT" | awk '{print $5}') bytes  ${SUT_FP:0:16}  mtime=$(stat -f %Sm "$SUT" 2>/dev/null)"
echo "corpus: $CORPUS   glob=$GLOB   limit=${LIMIT:-0}   workdir=$WORKDIR"

NFILES=$(collect_corpus)
echo "files: $NFILES"
if [ "$NFILES" = "0" ]; then
  echo "ERROR: 語料為 0 個檔案 ⇒ 任何「0 differ」都沒有意義。" >&2
  echo "       檢查 --corpus / --glob。" >&2
  exit 3
fi
echo

# ── L0：單元測試 ──────────────────────────────────────────────────────────
# oracle 是 compiler/tests/*_test.no 自己（測試檔必須自己 os.exit(1)；
# test runner 的斷言失敗**不會**改變 rc，見 docs §7.1）。
# ⚠️ 子命令必須是 `run`，**不能**用 "-"：`-` 會跑 `<bin> <file>`，而 `no <file>`
#    不是「執行」——它印出 usage 並 rc=0。那會讓**每個測試檔的 stdout 都相同**
#    （同一份 usage），G7 於是判 oracle 無鑑別力。
#    實測（2026-10-10）：`./bin/no compiler/tests/tok_test.no` 的 stdout 雜湊
#    = febadc57…（usage），三個測試檔完全一樣；改成 `run` 後才有真訊號。
if echo ",$LEVELS," | grep -q ',L0,'; then
  echo "--- L0 單元測試（compiler/tests/*_test.no）---"
  SAVED_CORPUS="$CORPUS"; SAVED_GLOB="$GLOB"
  CORPUS="compiler/tests"; GLOB='*_test.no'
  n=$(collect_corpus)
  if [ "$n" = "0" ]; then
    echo "  （沒有 *_test.no，UNAVAILABLE）"
    echo "UNAVAILABLE" > "$WORKDIR/L0.status"
  else
    run_diff_level L0 run
    report_level L0
  fi
  CORPUS="$SAVED_CORPUS"; GLOB="$SAVED_GLOB"
  collect_corpus >/dev/null
  echo
fi

# ── L1：詞法（oracle = 外部只讀 dumper）────────────────────────────────────
# `no` 沒有 `tokens` 子命令（§8 P0-②）⇒ 控制組不能當 L1 的 oracle。
# L1 的 oracle 由 --l1-ctl 提供（預設 = $CTL，於是沒有 `tokens` 時誠實報 UNAVAILABLE）。
# 目前實際使用的是 repo 之外的 `/tmp/notok/notok`：它直接 import `src/lexer`
# —— 也就是 P0-② 將來會用的**同一份**程式碼，所以它是一個真 oracle，不是近似。
# 重建方式見 docs/design/nolang-selfhost.md §7.1.1。
if echo ",$LEVELS," | grep -q ',L1,'; then
  echo "--- L1 詞法（<l1-ctl> tokens <file>）---"
  if [ -z "${L1_CTL:-}" ]; then
    echo "  UNAVAILABLE：未提供 L1 oracle（--l1-ctl）"
    # ⚠️ 這三行**必須用單引號**。原本寫成雙引號，於是反引號被當成
    # **命令替換**：第 372 行直接是語法錯誤（`<` `>` 被當重導向），
    # 第 370/371 行則會真的去**執行** `no` 與 `bin/no tokens <f>`。
    # 症狀是「L1 UNAVAILABLE 的理由」印成殘缺訊息（`（必須支援 ）`）。
    echo '  依據：`no` 沒有 `tokens` 子命令（docs §8 P0-②）⇒ 控制組不能當 L1 的尺子。'
    echo '        （實測：`bin/no tokens <f>` 對任何輸入都印同一份 usage，rc=0。）'
    echo '  用法：--l1-ctl /path/to/dumper   （必須支援 `<dumper> tokens <file>`）'
    echo "  ⚠️ 這不是 PASS。"
    echo "UNAVAILABLE" > "$WORKDIR/L1.status"
  elif [ ! -x "$L1_CTL" ]; then
    echo "  UNAVAILABLE：L1 oracle 不存在或不可執行：$L1_CTL"
    echo "UNAVAILABLE" > "$WORKDIR/L1.status"
  else
    echo "  oracle: $L1_CTL"
    SAVED_CTL="$CTL"; CTL="$L1_CTL"
    SAVED_SUT="$SUT"
    # 自檢時 SUT 應等於 L1 的 oracle（不是 bin/no —— 它根本不是 L1 的尺子），
    # 否則自檢會在 L1 這格誤報 FAIL（兩把不同的尺子當然量出不同結果）。
    [ "$SELF_TEST" = "1" ] && SUT="$L1_CTL"
    run_diff_level L1 tokens
    report_level L1
    CTL="$SAVED_CTL"; SUT="$SAVED_SUT"
  fi
  echo
fi

# ── L2：語法（no ast）─────────────────────────────────────────────────────
# 除了比 (rc, stdout)，還要**比失敗集合與失敗位置**（docs §7.4）：
# 只比「成功的檔案是否一致」抓不出「noc 對某個語法過度寬鬆」。
if echo ",$LEVELS," | grep -q ',L2,'; then
  echo "--- L2 語法（no ast）---"
  run_diff_level L2 ast
  report_level L2
  # 失敗集合：rc!=0 的檔案清單，兩邊必須完全相同
  awk '$1!=0 {print $5}' "$WORKDIR/L2.tsv" | sort > "$WORKDIR/L2.ctl_fail"
  awk '$3!=0 {print $5}' "$WORKDIR/L2.tsv" | sort > "$WORKDIR/L2.sut_fail"
  cf=$(cnt "$WORKDIR/L2.ctl_fail"); sf=$(cnt "$WORKDIR/L2.sut_fail")
  echo "  失敗集合：ctl=$cf  sut=$sf"
  if [ "$cf" != "$sf" ] || ! cmp -s "$WORKDIR/L2.ctl_fail" "$WORKDIR/L2.sut_fail"; then
    echo "  ⚠️ 失敗集合不同（僅比成功檔案會漏掉「過度寬鬆」）"
    echo "    only-ctl-fails:"; sed 's/^/      /' "$WORKDIR/L2.ctl_fail" | head -5
    echo "    only-sut-fails:"; comm -13 "$WORKDIR/L2.ctl_fail" "$WORKDIR/L2.sut_fail" | sed 's/^/      /' | head -5
    # 失敗集合不同本身就算 FAIL（若 SUT 全掛，status 已是 FAIL）
    [ "$(cat "$WORKDIR/L2.status" 2>/dev/null)" = "PASS" ] && echo "FAIL" > "$WORKDIR/L2.status"
  fi

  # ── oracle 完整性（docs §5.3.1）────────────────────────────────────────
  # `no ast` 的節點覆蓋已於 2026-10-10 補齊（40 → 71 個 case），語料基準是
  # **0 行 `<unknown>`**。所以這行現在是**回歸偵測**，不再是盲點警告：
  # 出現任何 `<unknown *parser.X>` ⇒ 有節點型別沒被 dump，那些節點的
  # **整個子樹**在 L2 差分中不可見（典型成因：parser 新增了節點而
  # dump.go 沒跟上）。`dump_completeness_test.go` 是第一道閘門，這裡是第二道。
  # ⚠️ 這**不是** FAIL 條件（oracle 仍可用，只是在那個子樹上退化）⇒ 只印，不設 status。
  if cat "$WORKDIR"/o.c.L2.* > "$WORKDIR/L2.ctl_all" 2>/dev/null; then :; fi
  if [ -s "$WORKDIR/L2.ctl_all" ]; then
    tot=$(wc -l < "$WORKDIR/L2.ctl_all" | tr -d ' ')
    unk=$(grep -c '<unknown \*parser\.' "$WORKDIR/L2.ctl_all" 2>/dev/null | tr -d ' ')
    unkt=$(grep -o '<unknown \*parser\.[A-Za-z0-9]*>' "$WORKDIR/L2.ctl_all" 2>/dev/null | sort -u | wc -l | tr -d ' ')
    [ -z "$unk" ] && unk=0
    [ -z "$unkt" ] && unkt=0
    if [ "$unk" -eq 0 ]; then
      echo "  oracle 覆蓋：控制組 dump 共 $tot 行，0 行 <unknown>（71/71 節點型別全覆蓋）"
    else
      echo "  🔴 oracle 退化：控制組 dump 共 $tot 行，其中 $unk 行是 <unknown>（$unkt 種節點型別）"
      echo "       這些節點的**整個子樹**不在 L2 覆蓋內（docs §5.3.1）⇒ L2 綠燈 ≠ AST 一致"
      echo "       基準值是 0；非 0 ⇒ 有節點型別沒被 dump.go 覆蓋，請補 case。"
    fi
  fi
  echo
fi

# ── L3：語義（no vet）────────────────────────────────────────────────────
# ⚠️ 這個 level 的 oracle 有**已知的穩定性問題**（docs §7.3）。所以除了比對，
#    還要先證明「控制組自己重跑兩次是否一致」——不穩定的 oracle 不能當判據。
if echo ",$LEVELS," | grep -q ',L3,'; then
  echo "--- L3 語義（no vet）---"
  run_diff_level L3 vet

  # 先過假通過防護（G1/G2/G3）——沒有訊號的 level 不判 PASS。
  if guard_level L3; then

    # G5：oracle 穩定性自檢 —— 控制組在抽樣檔案上跑兩次，歸一化後必須一致。
    probe=$(head -n 5 "$WORKDIR/files.txt")
    stable=1
    nprobe=0
    for f in $probe; do
      nprobe=$((nprobe + 1))
      tag=$(echo "stab.$f" | tr '/ ' '__')
      run_to "$TMO" "$CTL" vet "$f" >"$WORKDIR/o.$tag.1" 2>/dev/null
      run_to "$TMO" "$CTL" vet "$f" >"$WORKDIR/o.$tag.2" 2>/dev/null
      h1=$(norm_diag "$WORKDIR/o.$tag.1" | shasum -a 256 | awk '{print $1}')
      h2=$(norm_diag "$WORKDIR/o.$tag.2" | shasum -a 256 | awk '{print $1}')
      if [ "$h1" != "$h2" ]; then
        echo "  [G5] 控制組輸出**不確定**：$f"
        echo "       ${h1:0:16}"
        echo "       ${h2:0:16}"
        stable=0
      fi
    done
    if [ "$stable" = "0" ]; then
      echo "  [G5] oracle 不穩定 ⇒ L3 的 DIFF 不能當成 SUT 的錯，本 level 記為 UNAVAILABLE"
      echo "UNAVAILABLE" > "$WORKDIR/L3.status"
    else
      echo "  [G5] oracle 穩定性自檢：控制組抽樣 $nprobe 檔、各跑 2 次，歸一化後一致 ✅"

      # 逐行比對歸一化輸出（而非只比 sha：這裡要的是可讀的差異）
      l3diff=0
      errdiff=0
      while IFS= read -r f; do
        tag=$(echo "L3.$f" | tr '/ ' '__')
        run_to "$TMO" "$CTL" vet "$f" >"$WORKDIR/o.$tag" 2>/dev/null
        norm_diag "$WORKDIR/o.$tag" | sort > "$WORKDIR/n.ctl"
        run_to "$TMO" "$SUT" vet "$f" >"$WORKDIR/o.s.$tag" 2>/dev/null
        norm_diag "$WORKDIR/o.s.$tag" | sort > "$WORKDIR/n.sut"
        if ! cmp -s "$WORKDIR/n.ctl" "$WORKDIR/n.sut"; then
          l3diff=$((l3diff + 1))
          if [ "$l3diff" -le 3 ]; then
            echo "    DIFF $f"
            diff "$WORKDIR/n.ctl" "$WORKDIR/n.sut" | head -4 | sed 's/^/      /'
          fi
        fi
        errors_only "$WORKDIR/o.$tag" > "$WORKDIR/e.ctl"
        errors_only "$WORKDIR/o.s.$tag" > "$WORKDIR/e.sut"
        if ! cmp -s "$WORKDIR/e.ctl" "$WORKDIR/e.sut"; then
          errdiff=$((errdiff + 1))
          [ "$errdiff" -le 3 ] && echo "    [ERROR] 集合不同：$f"
        fi
      done < "$WORKDIR/files.txt"
      echo "  歸一化後不同的檔案數：$l3diff    [ERROR] 集合不同的檔案數：$errdiff"
      if [ "$l3diff" = "0" ] && [ "$errdiff" = "0" ]; then
        echo "PASS" > "$WORKDIR/L3.status"
      else
        echo "FAIL" > "$WORKDIR/L3.status"
      fi
    fi
  fi
  echo
fi

# ── L4：行為（凍結指紋）────────────────────────────────────────────────────
# oracle = tests/golden/mir-baseline.tsv（<rc> <sha256> <path>）。
# 先用**控制組**抽樣自檢 golden 是否還可信：如果控制組自己都對不上 golden，
# 那 golden 已經過期（或工作樹變了），此時 L4 記 UNAVAILABLE 而非 FAIL。
if echo ",$LEVELS," | grep -q ',L4,'; then
  echo "--- L4 行為（vs ${L4_GOLDEN}）---"
  if [ ! -f "$L4_GOLDEN" ]; then
    echo "  UNAVAILABLE：找不到 $L4_GOLDEN"
    echo "UNAVAILABLE" > "$WORKDIR/L4.status"
  else
    gold_n=$(cnt "$L4_GOLDEN")
    echo "  golden: $L4_GOLDEN ($gold_n 筆)"
    # golden 自檢
    if [ "$L4_SELFCHECK" != "0" ]; then
      bad=0; checked=0
      for f in $(awk '{print $3}' "$L4_GOLDEN" | head -n "$L4_SELFCHECK"); do
        [ -f "$f" ] || continue
        tag=$(echo "L4sc.$f" | tr '/ ' '__')
        run_to "$TMO" "$CTL" run "$f" >"$WORKDIR/o.$tag" 2>/dev/null
        rc=$?; [ "$rc" = "142" ] && rc=124
        h=$(sha_of "$WORKDIR/o.$tag"); [ -z "$h" ] && h="$EMPTY_SHA"
        grc=$(awk -v p="$f" '$3==p {print $1; exit}' "$L4_GOLDEN")
        gh=$(awk -v p="$f" '$3==p {print $2; exit}' "$L4_GOLDEN")
        checked=$((checked + 1))
        if [ "$rc" != "$grc" ] || [ "$h" != "$gh" ]; then
          bad=$((bad + 1))
          [ "$bad" -le 3 ] && echo "    golden 對不上：$f   golden=($grc,${gh:0:12})  ctl=($rc,${h:0:12})"
        fi
      done
      echo "  [golden 自檢] 控制組抽 $checked 檔，對不上 $bad 檔"
      if [ "$bad" != "0" ]; then
        echo "  ⇒ golden 已過期或工作樹已變 ⇒ L4 記 UNAVAILABLE（不是 SUT 的錯）"
        echo "UNAVAILABLE" > "$WORKDIR/L4.status"
      fi
    fi
    # SUT vs golden（僅在 golden 自檢通過時才有意義）
    if [ "$(cat "$WORKDIR/L4.status" 2>/dev/null)" != "UNAVAILABLE" ]; then
      : > "$WORKDIR/L4.tsv"
      while IFS= read -r f; do
        gh=$(awk -v p="$f" '$3==p {print $2; exit}' "$L4_GOLDEN")
        grc=$(awk -v p="$f" '$3==p {print $1; exit}' "$L4_GOLDEN")
        [ -z "$grc" ] && continue
        tag=$(echo "L4.$f" | tr '/ ' '__')
        run_to "$TMO" "$SUT" run "$f" >"$WORKDIR/o.$tag" 2>/dev/null
        rc=$?; [ "$rc" = "142" ] && rc=124
        h=$(sha_of "$WORKDIR/o.$tag"); [ -z "$h" ] && h="$EMPTY_SHA"
        echo "$grc $gh $rc $h $f" >> "$WORKDIR/L4.tsv"
      done < "$WORKDIR/files.txt"
      n=$(cnt "$WORKDIR/L4.tsv")
      nf=$(cnt "$WORKDIR/files.txt")
      same=$(awk '$1==$3 && $2==$4' "$WORKDIR/L4.tsv" | wc -l | tr -d ' ')
      sutok=$(awk '$3==0' "$WORKDIR/L4.tsv" | wc -l | tr -d ' ')
      echo "  量測 $n 檔（語料 $nf 檔；其中 $((nf - n)) 檔不在 golden 內 ⇒ 未量測）  SAME=$same  DIFF=$((n - same))  sut_ok=$sutok"
      # G4：語料與 golden 無交集 ⇒ 這一層什麼都沒量到。
      # 這**不是** FAIL（SUT 沒錯），也**不是** PASS（沒有證據）。
      # 早期版本在這裡寫 FAIL，於是 `--self-test` 會出現假陰性（SUT==CTL 卻判 FAIL），
      # 而錯誤訊息會指著 harness 說「你壞了」——正是 --self-test 要抓的那類 bug。
      if [ "$n" = "0" ]; then
        echo "  [G4] 語料與 golden 無交集（量測 0 檔）⇒ 此 level 無訊號，記 UNAVAILABLE"
        echo "       注意：$L4_GOLDEN 只涵蓋 tests/ 底下的檔案。"
        echo "UNAVAILABLE" > "$WORKDIR/L4.status"
      else
        if [ "$sutok" = "0" ]; then
          echo "  [G2] SUT 沒有任何一檔 rc==0 ⇒ mass-noop（SUT 尚未實作 build/run）"
        fi
        if [ "$same" = "$n" ]; then
          echo "PASS" > "$WORKDIR/L4.status"
        else
          echo "FAIL" > "$WORKDIR/L4.status"
          awk '$1!=$3 || $2!=$4 {print "    " $5 "  golden=(" $1 "," substr($2,1,12) ")  sut=(" $3 "," substr($4,1,12) ")"}' "$WORKDIR/L4.tsv" | head -10
        fi
      fi
    fi
  fi
  echo
fi

# ── 總結 ──────────────────────────────────────────────────────────────────
echo "=== SUMMARY ==="
overall=0   # 0=PASS 1=FAIL 3=SELFCHECK 4=UNAVAILABLE
npass=0     # PASS 的 level 數（供 --self-test 判斷「自檢是否空洞」）
VOID_REASON=""   # 非空 ⇒ 本輪作廢的原因（目前只有 "drift"，見 G6）
for L in L0 L1 L2 L3 L4; do
  echo ",$LEVELS," | grep -q ",$L," || continue
  st=$(cat "$WORKDIR/$L.status" 2>/dev/null || echo "NOTRUN")
  printf '  %-3s %s\n' "$L" "$st"
  case "$st" in
    PASS)        npass=$((npass + 1)) ;;
    FAIL)        [ "$overall" = "0" ] && overall=1 ;;
    SELFCHECK-FAIL) overall=3 ;;
    UNAVAILABLE) [ "$overall" = "0" ] && overall=4 ;;
    NOTRUN)      [ "$overall" = "0" ] && overall=4 ;;
  esac
done

# ── G6：收尾複查（跨輪/並行變動偵測）──────────────────────────────────────
# 開跑時記下的 CTL/SUT/golden 指紋，收尾時重算。任一變動 ⇒ **本輪結果作廢**。
# 這不是潔癖：本 repo 常有多個 session 並行改 `src/`，`make no` 會就地覆蓋 `bin/no`；
# 於是同一輪 sweep 的前半段與後半段跑在**兩支不同的尺子**上，結果不可比。
# 實測：2026-10-09 的 L2 全語料自檢期間 `bin/no` 被重建（mtime 21:09:44 → 21:55:31）。
DRIFT=""
[ "$(sha_of "$CTL")" != "$CTL_FP" ] && DRIFT="$DRIFT ctl"
[ "$(sha_of "$SUT")" != "$SUT_FP" ] && DRIFT="$DRIFT sut"
if [ -n "$GOLDEN_FP" ] && [ -f "$L4_GOLDEN" ] && [ "$(sha_of "$L4_GOLDEN")" != "$GOLDEN_FP" ]; then
  DRIFT="$DRIFT golden"
fi
if [ -n "$DRIFT" ]; then
  echo
  echo "[G6] 收尾複查：本輪執行期間下列輸入被改動 ⇒ 結果作廢：${DRIFT}"
  echo "     （並行 session 重建 binary／改 golden 是常態；請在穩定的樹上重跑）"
  overall=3
  VOID_REASON="drift"
fi

# 假通過稽核：把「這輪到底有沒有訊號」一次講清楚。
echo "--- 假通過稽核 ---"
# ⚠️ 這一行曾經寫死成 `L0 L2 L3 L4`（L1 永遠 UNAVAILABLE 的年代）。L1 接線後
#    必須一起稽核，否則「L1 PASS」是唯一沒有訊號佐證的一格。
for L in L0 L1 L2 L3 L4; do
  [ -f "$WORKDIR/$L.tsv" ] || continue
  f=$(cnt "$WORKDIR/files.txt")
  co=$(awk '$1==0' "$WORKDIR/$L.tsv" | wc -l | tr -d ' ')
  ce=$(awk -v e="$EMPTY_SHA" '$1==0 && $2==e' "$WORKDIR/$L.tsv" | wc -l | tr -d ' ')
  so=$(awk '$3==0' "$WORKDIR/$L.tsv" | wc -l | tr -d ' ')
  se=$(awk -v e="$EMPTY_SHA" '$3==0 && $4==e' "$WORKDIR/$L.tsv" | wc -l | tr -d ' ')
  printf '  %-3s ctl_ok=%-5s ctl_empty=%-5s sut_ok=%-5s sut_empty=%-5s\n' "$L" "$co" "$ce" "$so" "$se"
done
echo "  （ctl_ok ≈ 語料大小才是健康的；ctl_empty 偏高＝oracle 沒訊號；"
echo "    sut_ok=0 而 ctl_ok>0 ＝ SUT mass-noop，不是「差一點」）"

echo
case "$overall" in
  0) echo "RESULT: PASS" ;;
  1) echo "RESULT: FAIL" ;;
  3) echo "RESULT: HARNESS SELF-CHECK FAILED — 本輪結果作廢" ;;
  4) echo "RESULT: INCOMPLETE（有 level UNAVAILABLE —— 這不是 PASS）" ;;
esac

# --self-test 的硬斷言。
# 契約：SUT == CTL 時，**不可以有任何 level 判 FAIL**（那是假陰性＝harness 壞了）；
#       而且**至少要有 1 個 level PASS**（否則整份自檢是空洞的，等於另一種橡皮圖章）。
# UNAVAILABLE 是**允許**的：它代表 oracle 不存在（L1 沒有 `no tokens`、golden 過期、
# 語料與 golden 無交集），不是 harness 的判定邏輯有問題。把 UNAVAILABLE 當成失敗，
# 會讓自檢對「L1 還沒接線」這種正當狀態誤報 —— 那也是假陰性。
if [ "$SELF_TEST" = "1" ]; then
  if [ "$VOID_REASON" = "drift" ]; then
    # 這一輪是**被作廢**的，不是 harness 判錯。訊息必須分開，
    # 否則「並行 session 換了 binary」會被誤讀成「harness 有 bug」。
    echo
    echo "SELF-TEST 無法判定：本輪已因 [G6] 作廢（執行期間輸入被改動）。"
    echo "                  這不是 harness 的錯 —— 請在沒有並行改動的樹上重跑。"
    exit 3
  fi
  if [ "$overall" = "1" ] || [ "$overall" = "3" ]; then
    echo
    echo "SELF-TEST FAILED：SUT 與 CTL 是同一支 binary，卻有 level 判 FAIL / SELFCHECK-FAIL。"
    echo "                ⇒ 這個 harness 的判定邏輯本身有問題（假陰性）。"
    echo "                看上面的 SUMMARY 與「假通過稽核」定位是哪一層。"
    exit 3
  fi
  if [ "$npass" = "0" ]; then
    echo
    echo "SELF-TEST FAILED：沒有任何 level PASS ⇒ 這份自檢是空洞的"
    echo "                （對什麼都說 UNAVAILABLE 的 harness 也能通過自檢）。"
    exit 3
  fi
  if [ "$overall" = "4" ]; then
    echo
    echo "SELF-TEST OK：SUT == CTL，無任何 FAIL，$npass 層 PASS。"
    echo "              另有 level UNAVAILABLE —— 那是 oracle 不存在，不是 harness 壞。"
    echo "              請確認原因正當（L1 無 no tokens／golden 過期／語料與 golden 無交集）。"
  fi
  exit 0
fi
exit "$overall"
