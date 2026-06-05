#!/bin/bash
# run-test.sh — 執行標準庫測試並比對 Go 對照組
#
# 用法：
#   ./tests/run-test.sh              # 完整測試 + 比對
#   ./tests/run-test.sh -u           # 更新 Go 對照輸出（首次或 Go 更新後）
#   ./tests/run-test.sh -v           # 詳細模式（顯示完整 diff）
#
# 退出碼：
#   0 — 全部通過
#   1 — 有差異（NoLang 實作偏離 Go 標準庫）
#   2 — 執行錯誤

set -e

NOLANG_CMD="${NOLANG_CMD:-nolang}"
GO_CMD="${GO_CMD:-go}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GO_OUTPUT="$SCRIPT_DIR/compare/go-output.txt"
NOLANG_OUTPUT="$SCRIPT_DIR/nolang-output.txt"
DIFF_OUTPUT="$SCRIPT_DIR/diff-result.txt"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "============================================"
echo " Nolang 標準庫測試 — 與 Go 標準庫比對"
echo "============================================"
echo ""

# ── 解析參數 ──
UPDATE_GO=false
VERBOSE=false
while getopts "uv" opt; do
    case $opt in
        u) UPDATE_GO=true ;;
        v) VERBOSE=true ;;
        *) echo "用法: $0 [-u] [-v]"; exit 2 ;;
    esac
done

# ── 步驟 1：執行 Go 對照組 ──
echo -e "${YELLOW}[1/3] 執行 Go 對照組...${NC}"

if command -v "$GO_CMD" &> /dev/null; then
    cd "$PROJECT_DIR"
    $GO_CMD run "$SCRIPT_DIR/compare/compare.go" > "$GO_OUTPUT" 2>&1
    echo -e "      Go 輸出 → ${GREEN}$GO_OUTPUT${NC}"
    echo "      $(wc -l < "$GO_OUTPUT") 行"
else
    echo -e "      ${RED}錯誤：找不到 Go ($GO_CMD)${NC}"
    echo "      請安裝 Go 或設定 GO_CMD 環境變數"
    exit 2
fi

# ── 步驟 2：執行 Nolang 測試 ──
echo -e "${YELLOW}[2/3] 執行 Nolang 測試...${NC}"

if command -v "$NOLANG_CMD" &> /dev/null; then
    cd "$PROJECT_DIR"
    $NOLANG_CMD run "$SCRIPT_DIR/test_std_hash.no" > "$NOLANG_OUTPUT" 2>&1
    echo -e "      Nolang 輸出 → ${GREEN}$NOLANG_OUTPUT${NC}"
    echo "      $(wc -l < "$NOLANG_OUTPUT") 行"
else
    echo -e "      ${RED}錯誤：找不到 Nolang ($NOLANG_CMD)${NC}"
    echo "      請安裝 Nolang 或設定 NOLANG_CMD 環境變數"
    exit 2
fi

# ── 步驟 3：比對 ──
echo -e "${YELLOW}[3/3] 比對輸出...${NC}"

cd "$PROJECT_DIR"
set +e
diff "$GO_OUTPUT" "$NOLANG_OUTPUT" > "$DIFF_OUTPUT" 2>&1
DIFF_EXIT=$?
set -e

if [ $DIFF_EXIT -eq 0 ]; then
    echo -e "      ${GREEN}✓ 全部通過！Nolang 實作與 Go 標準庫一致${NC}"
    echo ""
    echo "============================================"
    echo " 結果：✅ 通過"
    echo "============================================"
    rm -f "$DIFF_OUTPUT"
    exit 0
else
    echo -e "      ${RED}✗ 發現差異！${NC}"
    echo ""
    echo "  差異行數：$(wc -l < "$DIFF_OUTPUT")"
    echo ""
    if [ "$VERBOSE" = true ]; then
        echo "  ── diff 輸出 ──"
        cat "$DIFF_OUTPUT"
        echo "  ────────────────"
    else
        echo "  使用 -v 參數顯示完整 diff"
        echo "  或直接查看：cat $DIFF_OUTPUT"
    fi
    echo ""
    echo "============================================"
    echo " 結果：❌ 失敗 — 有 $DIFF_EXIT 處差異"
    echo "============================================"
    exit 1
fi
