package fmt

// compound_assign_test.go — `no fmt` 保存時把 `+=` 一族自動重寫成 `a ?= a + 1`。
//
// 機制：parser 把複合賦算降糖成 UnwrapAssignStatement（DeprecatedOp 記下原運算符），
// formatter 渲染 surface AST 時一律輸出 `?=`，於是「重寫」不需要任何專門的 fmt 邏輯
// —— 它只是降糖結果的可見化。這裡釘住三件事：
//   1. 五個運算符都重寫，運算符保真（`/=` → `/`，不是別的）；
//   2. 冪等：第二遍不再改；
//   3. 右式可以是任意表達式（呼叫、索引、算術），不是只有字面量。

import (
	"strings"
	"testing"
)

// TestFormatRewritesCompoundAssign 五個運算符各重寫一條，斷言整段輸出。
func TestFormatRewritesCompoundAssign(t *testing.T) {
	in := "f = (x i64) (result ?i64) {\n" +
		"    result = nil\n" +
		"    a i64 = x\n" +
		"    a += 1\n" +
		"    a -= 2\n" +
		"    a *= 3\n" +
		"    a /= 4\n" +
		"    a %= 5\n" +
		"    result ?= a + 0\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	want := "f = (x i64) (result ?i64) {\n" +
		"    result = nil\n" +
		"    a i64 = x\n" +
		"    a ?= a + 1\n" +
		"    a ?= a - 2\n" +
		"    a ?= a * 3\n" +
		"    a ?= a / 4\n" +
		"    a ?= a % 5\n" +
		"    result ?= a + 0\n" +
		"}\n"
	if out != want {
		t.Fatalf("rewrite mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		if strings.Contains(out, op) {
			t.Errorf("compound assign %s survived formatting:\n%s", op, out)
		}
	}
}

// TestFormatCompoundAssignIsIdempotent 冪等：重寫後的檔案再格式化必須零變更
// （保存兩次不該產生 diff，否則用戶每次 Ctrl-S 都被改一次）。
func TestFormatCompoundAssignIsIdempotent(t *testing.T) {
	in := "f = (x i64) (result ?i64) {\n    result = nil\n    a i64 = x\n    a += 1\n    result ?= a + 0\n}\n"
	first, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors (pass 1): %v", errs)
	}
	second, errs := FormatFileWithErrors(first)
	if len(errs) > 0 {
		t.Fatalf("format errors (pass 2): %v", errs)
	}
	if first != second {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", first, second)
	}
}

// TestFormatCompoundAssignComplexRhs 右式是任意表達式（呼叫 + 算術）而不是
// 字面量時，降糖一樣成立 —— 反面排除「只認字面量」的假實現。
func TestFormatCompoundAssignComplexRhs(t *testing.T) {
	in := "f = (x i64, y i64) (result ?i64) {\n    result = nil\n    a i64 = x\n    a += g(y) * 2\n    result ?= a + 0\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if !strings.Contains(out, "a ?= a + g(y) * 2") {
		t.Fatalf("complex RHS not rewritten faithfully:\n%s", out)
	}
}

// TestFormatRefusesCompoundAssignInPlainFunction 非 option 結果函式裡的
// `a += 1` 是 parser ERROR，`no fmt` 不得重寫（無從決定非 ok 分支）：
// 輸出必須與輸入逐位元組相同，並把錯誤透出來讓用戶看到改寫建議。
func TestFormatRefusesCompoundAssignInPlainFunction(t *testing.T) {
	in := "f = (x i64) () {\n    a i64 = x\n    a += 1\n    io.outln(a)\n}\n"
	out, errs := FormatFileWithErrors(in)
	if out != in {
		t.Fatalf("plain-function compound assign must be left untouched:\n--- in ---\n%s\n--- out ---\n%s", in, out)
	}
	if len(errs) == 0 {
		t.Fatalf("expected the parser error to be surfaced, got none")
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"`a += 1`", "`a + 1: { ok -> a = it, -> }`", "`a ?= a + 1`"} {
		if !strings.Contains(joined, want) {
			t.Errorf("error does not suggest %q:\n%s", want, joined)
		}
	}
}

// TestFormatCompoundAssignBadTargetUntouched 對照組：`a[0] += 1` 這類非識別字
// 目標不降糖（parser 側刻意不支援，見 parser/compound_assign_test.go的對照組），
// formatter 不得把它變成錯誤的 `a[0] ?= ...`。
func TestFormatCompoundAssignBadTargetUntouched(t *testing.T) {
	in := "f = (x i64) (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[0] += 1\n    result ?= a[0] + 0\n}\n"
	out, errs := FormatFileWithErrors(in)
	// 解析本身就已經壞掉（舊行為），fmt 只要求「不解釋成 `?=`」。
	if strings.Contains(out, "a[0] ?=") {
		t.Fatalf("index target must not be rewritten to `?=`:\n%s (errs=%v)", out, errs)
	}
}
