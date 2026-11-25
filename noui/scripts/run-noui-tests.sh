#!/bin/sh
# run-noui-tests.sh — noui 无头全量测试运行器
#
# 为什么需要它：noui 的测试分两类，两类都要真编真跑（语言没有解释器）：
#   1. 纯逻辑测试（tests/cmp-*.no / app-*.no / dv-*.no …）
#      —— 直接跑，靠自己的断言 + exit(1) 报错；
#   2. 窗口 demo（tests/main-*.no）
#      —— 要 SDL，且主循环是「等到退出事件」；dummy 视频驱动不投递
#      SDL_EVENT_QUIT，所以必须靠 shim 的帧闸（NOUI_MAX_FRAMES）收尾。
#
# 判定：进程退出码为 0 即通过。失败测试已统一 exit(1)（见各文件尾部），
# 编译错误 / 信号死亡（SIGSEGV / SIGTRAP）也都是非 0，不会被静默吞掉。
#
# 用法：
#   ./scripts/run-noui-tests.sh                       # 跑 tests/*.no
#   JOBS=8 ./scripts/run-noui-tests.sh                # 并行度（默认 = CPU 核数）
#   NOUI_MAX_FRAMES=120 ./scripts/run-noui-tests.sh
#   ./scripts/run-noui-tests.sh tests/cmp-popover.no  # 只跑指定文件
#
# 环境：SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy（脚本自带，无需 export）
set -u

W=$(cd "$(dirname "$0")/.." && pwd)
NO=$(cd "$W/.." && pwd)/bin/no
CCWRAP="$W/ccwrap.sh"

if [ ! -x "$NO" ]; then
  echo "找不到编译器：${NO}（先在仓库根 make no）" >&2
  exit 2
fi
if [ ! -f "$W/dist/libnoui-shim.a" ]; then
  echo "[shim] dist/libnoui-shim.a 不存在，先构建 shim" >&2
  "$W/build-shims.sh" || exit 2
fi

export PATH="/opt/homebrew/bin:/opt/homebrew/opt/llvm/bin:$PATH"
export SDL_VIDEODRIVER=dummy
export SDL_AUDIODRIVER=dummy
# 窗口 demo 的帧闸：>0 时第 N 帧置 g_quit（src/shim/shim.c）。0 = 不限。
export NOUI_MAX_FRAMES="${NOUI_MAX_FRAMES:-120}"

RUN_TIMEOUT="${RUN_TIMEOUT:-180}"   # 单个测试的墙钟上限（秒）
if [ "$(uname -s)" = "Darwin" ]; then
  if command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT_CMD=gtimeout
  else
    TIMEOUT_CMD=""   # 没有 coreutils 就退化为不封顶（依赖测试自身退出）
  fi
else
  TIMEOUT_CMD=timeout
fi
export RUN_TIMEOUT TIMEOUT_CMD

if [ $# -gt 0 ]; then
  TESTS="$*"
else
  TESTS=""
  for f in "$W"/tests/*.no; do
    [ -f "$f" ] || continue
    case "$(basename "$f")" in
      _*) continue ;;   # 下划线开头 = 临时/禁用
    esac
    TESTS="$TESTS $f"
  done
fi

JOBS="${JOBS:-$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)}"
OUTDIR=$(mktemp -d "${TMPDIR:-/tmp}/noui-tests.XXXXXX") || exit 2

# 单测工作器：每个测试独立写 <name>.log / <name>.result，避免并行写同一文件交错。
cat >"$OUTDIR/run-one.sh" <<'EOS'
#!/bin/sh
set -u
t="$1"
name=${t##*/}
log="$OUTDIR/$name.log"
res="$OUTDIR/$name.result"
start=$(date +%s)
if [ -n "$TIMEOUT_CMD" ]; then
  "$TIMEOUT_CMD" "$RUN_TIMEOUT" "$NO" run -cc "$CCWRAP" "$t" >"$log" 2>&1
else
  "$NO" run -cc "$CCWRAP" "$t" >"$log" 2>&1
fi
rc=$?
secs=$(( $(date +%s) - start ))
if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
  verdict=TIMEOUT
elif [ "$rc" -eq 0 ]; then
  verdict=PASS
else
  verdict=FAIL
fi
printf '%s\t%s\t%s\n' "$verdict" "$rc" "$secs" >"$res"
EOS
chmod +x "$OUTDIR/run-one.sh"
export OUTDIR NO CCWRAP

echo "[noui-tests] 测试数：$(echo $TESTS | wc -w | tr -d ' ')　并行度：${JOBS}　帧闸：${NOUI_MAX_FRAMES}"
echo "[noui-tests] 日志目录：$OUTDIR"

printf '%s\n' $TESTS | xargs -P "$JOBS" -I{} "$OUTDIR/run-one.sh" {}

pass=0; fail=0; to=0
for res in "$OUTDIR"/*.result; do
  [ -f "$res" ] || continue
  v=$(cut -f1 "$res")
  rc=$(cut -f2 "$res")
  name=$(basename "$res" .result)
  case "$v" in
    PASS) pass=$((pass+1)) ;;
    TIMEOUT)
      to=$((to+1)); fail=$((fail+1))
      echo "---- $name (TIMEOUT rc=$rc) ----"; tail -15 "$OUTDIR/$name.log"; echo
      ;;
    *)
      fail=$((fail+1))
      echo "---- $name (FAIL rc=$rc) ----"; tail -15 "$OUTDIR/$name.log"; echo
      ;;
  esac
done

total=$((pass + fail))
echo "======== [noui-tests] 汇总 ========"
echo "通过 ${pass} / ${total}　（超时 ${to}）"
if [ "$fail" -ne 0 ]; then
  echo "失败清单："
  for res in "$OUTDIR"/*.result; do
    [ "$(cut -f1 "$res")" = PASS ] || echo "  $(basename "$res" .result)	$(cut -f1 "$res")	$(cut -f2 "$res")s"
  done
  echo "（完整日志在 ${OUTDIR}，退出时不自动删）"
  exit 1
fi
rm -rf "$OUTDIR"
exit 0
