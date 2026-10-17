package checker

// compound_assign_lint_test.go — `+=` 一族複合賦算的棄用提示（WARNING）。
//
// 2026-10-13 定案的兩條路（見 parser/compound_assign_test.go）：
//   - 所在函式結果是 option ⇒ WARNING（`a ?= a + 1` 可上拋，fmt 會自動重寫）；
//   - 所在函式結果不是 option ⇒ parser 直接報 ERROR，lint 一條都不發。
//
// 這是 LSP 實時診斷路徑的迴歸釘：LSP 主解析保留 surface AST
// （parser.SkipUnwrapLowering=true，見 lsp/documents.go），所以 checker 的
// deprecated lint 看得到 parser 打上的 DeprecatedOp 標記；而 CLI 管線
// （no vet / no build）在 lowering 後只剩 parser 期的 W_COMPOUND_ASSIGN 警告
// （見 parser/compound_assign_test.go 與 build/transpiler.go）。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseProgSurface 與 match_exhaustive_test.go 的 parseProg 相同，但保留 surface
// AST —— 複合賦算的棄用標記（UnwrapAssignStatement.DeprecatedOp）只在 surface
// 形態可見，lowering 會把它展開成 __unwrap_N 區塊。
func parseProgSurface(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// parseProgSurfaceAllowErrors 與 parseProgSurface 相同，但容忍解析期錯誤 ——
// 非 option 結果函式裡的複合賦算現在由 parser 直接報 ERROR（見
// parser/compound_assign_test.go），而 LSP 仍要顯示 surface AST 上的 lint 結果，
// 所以這條路徑必須能在有 parse error 的 program 上跑完。
func parseProgSurfaceAllowErrors(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	return p.ParseProgram()
}

// TestCompoundAssignWarnsOncePerSite 五個運算符各一次 WARNING，文案要點名原運算符、
// 給出 `?=` 改寫形式。
func TestCompoundAssignWarnsOncePerSite(t *testing.T) {
	src := `f = (x i64) (result ?i64) {
    result = nil
    a i64 = x
    a += 1
    a -= 1
    a *= 2
    a /= 2
    a %= 3
    result ?= a + 0
}
`
	res := ValidateDeprecatedCalls(parseProgSurface(t, src))
	if got := countTrace(res, deprecatedAssignTraceID); got != 5 {
		t.Fatalf("expected 5 %s, got %d: %+v", deprecatedAssignTraceID, got, res)
	}
	for _, r := range res {
		if !strings.Contains(r.Message, "?=") {
			t.Errorf("message does not suggest the `?=` rewrite: %q", r.Message)
		}
	}
	// 位置必須逐行落在各自的運算上（4..8）。
	for i, want := range []int{4, 5, 6, 7, 8} {
		if res[i].Line != want {
			t.Errorf("result %d: Line = %d, want %d", i, res[i].Line, want)
		}
	}
}

// TestCompoundAssignWarnSeverityIsWarning 嚴重度定案：WARNING（用戶要求
// 「lsp 提示 warning」）。經 RunAllLints 路由後仍必須是 WARNING 而不是 HINT。
func TestCompoundAssignWarnSeverityIsWarning(t *testing.T) {
	src := `f = (x i64) (result ?i64) {
    result = nil
    a i64 = x
    a += 1
    result ?= a + 0
}
`
	lints := RunAllLints(parseProgSurface(t, src), LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, l := range lints {
		if l.TraceID != deprecatedAssignTraceID {
			continue
		}
		found = true
		if l.Severity != LintWarning {
			t.Errorf("severity = %v, want LintWarning", l.Severity)
		}
		if l.EndColumn <= l.Column {
			t.Errorf("end column not wider than start: %d <= %d", l.EndColumn, l.Column)
		}
	}
	if !found {
		t.Fatalf("no %s in RunAllLints output: %+v", deprecatedAssignTraceID, lints)
	}
}

// TestCompoundAssignInPlainFunctionNoLint 非 option 結果函式裡的 `a += 1` 由
// parser 報 ERROR（不是棄用警告，因為沒有 option 結果可以上拋），所以 checker
// 的 deprecated lint 一條都不該發 —— 否則同一個 site 會被報兩次。
func TestCompoundAssignInPlainFunctionNoLint(t *testing.T) {
	src := `f = (x i64) () {
    a i64 = x
    a += 1
    io.outln(a)
}
`
	res := ValidateDeprecatedCalls(parseProgSurfaceAllowErrors(t, src))
	if got := countTrace(res, deprecatedAssignTraceID); got != 0 {
		t.Fatalf("plain-function compound assign must be a parser ERROR, not a lint warning, got %d: %+v", got, res)
	}
	lints := RunAllLints(parseProgSurfaceAllowErrors(t, src), LintOptions{SourcePath: "src/app.no"})
	for _, l := range lints {
		if l.TraceID == deprecatedAssignTraceID {
			t.Errorf("RunAllLints must not emit %s for a plain-function compound assign: %+v", deprecatedAssignTraceID, l)
		}
	}
}

// TestGenuineUnwrapAssignNoDeprecationWarning 對照組：手寫 `a ?= a + 1` 不報廢棄。
func TestGenuineUnwrapAssignNoDeprecationWarning(t *testing.T) {
	src := `f = (x i64) (result ?i64) {
    result = nil
    a i64 = x
    a ?= a + 1
    result ?= a + 0
}
`
	res := ValidateDeprecatedCalls(parseProgSurface(t, src))
	if got := countTrace(res, deprecatedAssignTraceID); got != 0 {
		t.Fatalf("genuine `?=` must not warn, got %d: %+v", got, res)
	}
}

// TestDeprecatedCallHintUnchanged 對照組：同一個 visitor 裡的「棄用函數 HINT」
// 不能被複合賦算的改動波及（兩類棄用共用 walkStmt 的 UnwrapAssignStatement 分支）。
func TestDeprecatedCallHintUnchanged(t *testing.T) {
	src := `f = (x i64) (result ?i64) {
    result = nil
    s = i64-to-str(x)
    result ?= 1 + 0
}
`
	res := ValidateDeprecatedCalls(parseProgSurface(t, src))
	if got := countTrace(res, deprecatedCallTraceID); got != 1 {
		t.Fatalf("expected 1 %s, got %d: %+v", deprecatedCallTraceID, got, res)
	}
	if got := countTrace(res, deprecatedAssignTraceID); got != 0 {
		t.Fatalf("no compound-assign warning expected here, got %d: %+v", got, res)
	}
}
