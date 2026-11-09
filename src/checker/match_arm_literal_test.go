package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// validateSrc parses and type-checks one snippet, returning the results.
func validateSrc(t *testing.T, src string) []ValidateResult {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return ValidateTypes(prog)
}

// hasCannotAssign reports whether results contain the 15w45dqk narrowing
// diagnostic ("cannot assign X value to Y variable").
func hasCannotAssign(results []ValidateResult) bool {
	for _, r := range results {
		if strings.Contains(r.Message, "cannot assign") {
			return true
		}
	}
	return false
}

// TestMatchArmIntLiteralNarrowsToDeclared：數字型別都可以用 `0` 這樣的字面量。
//
// 值 match 的預設臂 `-> 0` 是常數轉換，不是執行期 i64→u8 窄化：
//
//	res u8 = arr[i]: { ok -> it  -> 0 }
//
// REGRESSION GUARD：inferExprType 對值 match 只回報「第一個可解析臂」的型別，
// ok 臂的 `it` 在 matchArmBindings 拿不到型別（Matched 是 IndexExpression）而
// 回空，於是落到 `-> 0` 的 i64，賦值判定就誤報 i64 → u8。曾讓
// tests/safe-index.no 與 tests/safe-index-zero.no 三處站點紅燈。
func TestMatchArmIntLiteralNarrowsToDeclared(t *testing.T) {
	results := validateSrc(t, `
f = (a []u8, i i64) (res u8) {
    res = a[i]: {
        ok -> it

        -> 0
    }
}

g = (a []u8, i i64) (res u8) {
    res = a[i]: {
        ok -> it

        -> 255
    }
}
`)
	if hasCannotAssign(results) {
		t.Fatalf("integer literal in a match arm must be a constant conversion, got: %v", results)
	}
}

// TestMatchArmOutOfRangeLiteralStillRejected：只有「落在目標範圍內」的字面量
// 才算常數轉換。`-> 300` 寫進 u8 是真實的數值溢位，必須繼續報錯。
func TestMatchArmOutOfRangeLiteralStillRejected(t *testing.T) {
	results := validateSrc(t, `
f = (a []u8, i i64) (res u8) {
    res = a[i]: {
        ok -> it

        -> 300
    }
}
`)
	if !hasCannotAssign(results) {
		t.Fatalf("out-of-range literal 300 assigned to u8 must still be rejected, got: %v", results)
	}
}

// TestMatchArmRuntimeValueStillRejected：執行期 i64 值寫進 u8 是真正的窄化，
// 豁免只涵蓋字面量，不能把變數一起放行。
func TestMatchArmRuntimeValueStillRejected(t *testing.T) {
	results := validateSrc(t, `
f = (a []u8, i i64, k i64) (res u8) {
    res = a[i]: {
        ok -> it

        -> k
    }
}
`)
	if !hasCannotAssign(results) {
		t.Fatalf("runtime i64 value assigned to u8 must still be rejected, got: %v", results)
	}
}

// TestMatchArmCharLiteralTypes：char 的字面量是雙引號（`"x"`），單引號 `'x'`
// 是 str。豁免不得讓 str 混進 char。
func TestMatchArmCharLiteralTypes(t *testing.T) {
	ok := validateSrc(t, `
f = (a []char, i i64) (res char) {
    res = a[i]: {
        ok -> it

        -> "x"
    }
}
`)
	if hasCannotAssign(ok) {
		t.Fatalf("char literal \"x\" in a match arm must be accepted, got: %v", ok)
	}

	bad := validateSrc(t, `
f = (a []char, i i64) (res char) {
    res = a[i]: {
        ok -> it

        -> 'x'
    }
}
`)
	if !hasCannotAssign(bad) {
		t.Fatalf("str literal 'x' assigned to char must still be rejected, got: %v", bad)
	}
}
