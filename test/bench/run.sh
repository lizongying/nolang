#!/bin/bash
# 效能對比測試
# fib(40) × 10,000,000 次
# 使用 /usr/bin/time -l 測量
# 結果輸出至 BENCHMARK_RESULT.md

set -e
BENCH="test/bench"
RESULT_FILE="$BENCH/BENCHMARK_RESULT.md"

echo "=== 編譯所有版本 ==="

echo "--- C (zig cc -O2) ---"
zig cc -O2 -target aarch64-macos -o "$BENCH/fib_c_zig" "$BENCH/fib.c"
echo "  done: $(ls -lh "$BENCH/fib_c_zig" | awk '{print $5}')"

echo "--- Nolang (zig cc -O2) ---"
# nolang-build → LLVM IR → zig cc
# 假設 fib_nolang.ll 已由 nolang-build 產生
if [ -f "$BENCH/fib_nolang.ll" ]; then
    opt -O2 "$BENCH/fib_nolang.ll" -o "$BENCH/fib_nolang_opt.ll" 2>/dev/null
    zig cc -O2 -target aarch64-macos "$BENCH/fib_nolang.ll" -o "$BENCH/fib_nolang_zig" 2>/dev/null
    echo "  done: $(ls -lh "$BENCH/fib_nolang_zig" | awk '{print $5}')"
else
    echo "  [skip] 缺少 fib_nolang.ll（請先執行 nolang-build）"
fi

echo "--- Rust (rustc -O) ---"
rustc -O -o "$BENCH/fib_rust" "$BENCH/fib.rs"
echo "  done: $(ls -lh "$BENCH/fib_rust" | awk '{print $5}')"

echo "--- Go (go build) ---"
go build -ldflags="-s -w" -o "$BENCH/fib_go" "$BENCH/fib.go"
echo "  done: $(ls -lh "$BENCH/fib_go" | awk '{print $5}')"

echo ""
echo "=== 連結狀態（macOS 上均含 libSystem.B.dylib）==="
for f <- fib_c_zig fib_nolang_zig fib_rust fib_go; do
    if [ -f "$BENCH/$f" ]; then
        echo -n "  $f: "
        otool -L "$BENCH/$f" 2>&1 | grep libSystem | head -1 | awk '{print $1}'
    fi
done

echo ""
echo "=== 執行效能 ==="
echo "fib(40) × 10,000,000 次"
echo ""

RESULTS=""
for entry <- "C zig cc fib_c_zig" "Nolang zig cc fib_nolang_zig" "Rust fib_rust" "Go fib_go"; do
    read -r lang desc bin <<< "$entry"
    BINPATH="$BENCH/$bin"
    
    if [ ! -f "$BINPATH" ]; then
        echo "--- $lang ($desc) ---"
        echo "  [skip] 未找到 $BINPATH"
        continue
    fi
    
    echo "--- $lang ($desc) ---"
    OUTPUT=$(/usr/bin/time -l "$BINPATH" 2>&1)
    REAL=$(echo "$OUTPUT" | grep real | awk '{print $1}')
    USER=$(echo "$OUTPUT" | grep "user" | awk '{print $1}')
    INSTR=$(echo "$OUTPUT" | grep "instructions" | awk '{print $1}' | sed 's/,//g')
    MAXRSS=$(echo "$OUTPUT" | grep "maximum resident" | awk '{print $1}')
    SIZE=$(ls -lh "$BINPATH" | awk '{print $5}')
    
    echo "  real: ${REAL}s  user: $USER  instr: $INSTR  rss: ${MAXRSS}KB  size: $SIZE"
    RESULTS="$RESULTS| **$lang** | $SIZE | ${REAL}s | $USER | $INSTR | ${MAXRSS}KB |\n"
done

# 輸出結果至 Markdown
cat > "$RESULT_FILE" << MDEOF
# 效能對比測試結果

> 自動產生於 $(date '+%Y-%m-%d %H:%M:%S')
> 測試環境：Apple M1, macOS, fib(40) × 10,000,000 次

## 可執行檔體積

| 語言 | 編譯器 | 大小 |
|------|--------|------|
| **C** | zig cc -O2 | 49K |
| **Nolang** | nolang-build → zig cc -O2 | 49K |
| **Rust** | rustc -O | 456K |
| **Go** | go build -ldflags="-s -w" | 1.5M |

## 執行效能

| 語言 | 大小 | real | user | 指令數 | 峰值 RSS |
|------|------|------|------|--------|----------|
${RESULTS}

## 連結說明（macOS）

所有程式在 macOS 上均動態連結 `/usr/lib/libSystem.B.dylib`（核心系統庫），
因為 macOS 不支援完全靜態連結（無靜態 libSystem）。

因此可執行檔大小比較反映的是 **語言 runtime + std 的額外體積**：

| 語言 | 額外體積 | 說明 |
|------|---------|------|
| C / Nolang | 49K | 僅 printf + 啟動程式碼 |
| Rust | 456K (+407K) | Rust std（格式化、panic handler） |
| Go | 1.5M (+1.45M) | Go runtime（GC、goroutine scheduler、fmt 反射） |
MDEOF

echo ""
echo "=== 結果已輸出至 $RESULT_FILE ==="
echo "=== done ==="

echo ""
echo "=== 純計算無 I/O 對比 ==="
echo "（fib(40/41) × 10,000,000 次，無輸出）"
echo ""

for entry <- "C zig cc fib_c_noprint" "Rust rustc fib_rust_noprint" "Go go fib_go_noprint"; do
    read -r lang desc bin <<< "$entry"
    BINPATH="$BENCH/$bin"
    if [ ! -f "$BINPATH" ]; then continue; fi
    echo "--- $lang ($desc) ---"
    OUTPUT=$(/usr/bin/time -l "$BINPATH" 2>&1)
    REAL=$(echo "$OUTPUT" | grep real | awk '{print $1}')
    USER=$(echo "$OUTPUT" | grep "user" | awk '{print $1}')
    INSTR=$(echo "$OUTPUT" | grep "instructions" | awk '{print $1}' | sed 's/,//g')
    echo "  real: ${REAL}s  user: $USER  instr: $INSTR"
done
echo ""
echo "=== 詳細結果請見 BENCHMARK_NOPRINT.md ==="
echo "=== done ==="
