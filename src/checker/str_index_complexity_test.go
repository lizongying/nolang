package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// strIndexComplexityWarnings 解析 src 並回傳 STR_INDEX_COMPLEXITY 告警數量。
func strIndexComplexityWarnings(t *testing.T, src string) int {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	n := 0
	for _, r := range ValidateStrIndexComplexity(prog) {
		if r.TraceID == "STR_INDEX_COMPLEXITY" {
			n++
		}
	}
	return n
}

// TestStrIndexComplexity_FiresForUnprovenAsciiParam 覆蓋「未證明 ASCII 的 str 參數在
// 迴圈內用 s[i]」這個基本場景（含巢狀函式，確保作用域解析正確）。
//
// ⚠️ 注意：這條告警在**真實的 `no vet` 流程裡曾長期完全不觸發**，但單獨用本測試
// harness（program 裡只有本段原始碼、沒有 std）是**驗不出來**的 —— 因為根因是
// asciiVars 以裸變數名為鍵，而 `no vet` 會把整個標準庫併入 program：std 裡有大量
// 名為 s / n / i / out / pos / val / target 的區域變數被賦 ASCII 字面量，於是任何
// 叫這些名字的變數都被誤判為「已證明純 ASCII」。真正的回歸守衛是
// TestStrIndexComplexity_NotSuppressedByOtherFunction（同一 program 內同名變數不得
// 互相污染）。本用例與它互補：一個保證「該報的要報」，一個保證「不被別處的 ASCII 消音」。
func TestStrIndexComplexity_FiresForUnprovenAsciiParam(t *testing.T) {
	src := `
outer = () {
    inner = (s str) {
        n = 0
        i <- [0..s.len-bytes()): {
            c = s[i]
            c == "/" -> n = 1
        }
    }
}
`
	if got := strIndexComplexityWarnings(t, src); got != 1 {
		t.Fatalf("expected 1 STR_INDEX_COMPLEXITY warning, got %d", got)
	}
}

// TestStrIndexComplexity_NotSuppressedByOtherFunction 是本條診斷的**真正回歸守衛**。
//
// 修復前 asciiVars 是 `map[string]bool`，以裸變數名為鍵，不區分作用域：
// 只要 program 裡任何一處把 ASCII 字面量賦給名為 s 的變數，所有叫 s 的變數
// （含其他函式的參數）都會被當成「已證明純 ASCII」，告警靜默消失。
// 在 `no vet` 合併模式下，std 提供了大量這種賦值 → 整條規則實務上完全失效。
//
// 本用例在修復前 FAIL（got 0，期望 1），修復後 PASS。
func TestStrIndexComplexity_NotSuppressedByOtherFunction(t *testing.T) {
	src := `
a = () {
    s str = '/x'
}

b = (s str) {
    n = 0
    i <- [0..s.len-bytes()): {
        c = s[i]
        c == "/" -> n = 1
    }
}
`
	if got := strIndexComplexityWarnings(t, src); got != 1 {
		t.Fatalf("expected 1 STR_INDEX_COMPLEXITY warning (s in a must not silence s in b), got %d", got)
	}
}

// TestStrIndexComplexity_AsciiLiteralIsExempt 已證明 ASCII 的字面量賦值不應告警。
func TestStrIndexComplexity_AsciiLiteralIsExempt(t *testing.T) {
	src := `
probe = () {
    t str = '/a/b'
    n = 0
    i <- [0..t.len-bytes()): {
        c = t[i]
        c == "/" -> n = 1
    }
}
`
	if got := strIndexComplexityWarnings(t, src); got != 0 {
		t.Fatalf("expected 0 warnings for proven-ASCII literal, got %d", got)
	}
}

// TestStrIndexComplexity_ByteAccessIsExempt 建議的替代寫法 s.byte(i) 不應告警。
func TestStrIndexComplexity_ByteAccessIsExempt(t *testing.T) {
	src := `
probe = (s str) {
    n = 0
    i <- [0..s.len-bytes()): {
        c = s.byte(i)
        c == 47 -> n = 1
    }
}
`
	if got := strIndexComplexityWarnings(t, src); got != 0 {
		t.Fatalf("expected 0 warnings for s.byte(i), got %d", got)
	}
}

// TestStrIndexComplexity_OutsideLoopIsExempt 單次下標（非迴圈內）不告警。
func TestStrIndexComplexity_OutsideLoopIsExempt(t *testing.T) {
	src := `
probe = (s str) {
    c = s[0]
}
`
	if got := strIndexComplexityWarnings(t, src); got != 0 {
		t.Fatalf("expected 0 warnings outside a loop, got %d", got)
	}
}

// TestStrIndexComplexity_TopLevelNonAsciiLiteral 頂層非 ASCII 字面量變數在迴圈內
// 下標應告警（對應「std 污染」修好後才可能通過的場景）。
func TestStrIndexComplexity_TopLevelNonAsciiLiteral(t *testing.T) {
	src := `
s = '/a/日本/b'
n = 0
i <- [0..s.len-bytes()): {
    c = s[i]
    c == "/" -> n = 1
}
`
	if got := strIndexComplexityWarnings(t, src); got != 1 {
		t.Fatalf("expected 1 warning for top-level non-ASCII var, got %d", got)
	}
}
