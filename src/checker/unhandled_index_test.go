package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 本檔案是 ValidateUnhandledIndex 的回歸測試：arr/vec/slice 索引 b[i] 越界時
// 預設回傳 option<elem>（永不 panic），若結果既沒被 `?=` 上拋、沒被 `#{index-out
// = DEF}` 註解處理、也沒在返回 ?T 的函式中被就地捕獲（`a = b[i]` → `a: ?elem`），
// 即「沉默泄漏」，必須報錯
// （no vet / LSP 診斷，TraceID = idxhndld）。本規則僅作為診斷（非編譯硬錯誤），
// 以免讓 `no build` / `no run` / `no test` 對既有（含 std）廣泛使用的舊式越界
// panic 寫法全面失敗。

// 未處理的索引（裸 `res = arr[i]` / `b = arr[i]`，結果非 ?T、非 option 函式）。
const unhandledIdxSrc = `get = (arr []i64, i i64) (res i64) {
    res = arr[i]
}`

// TestUnhandledIndexDetectsLeaks 驗證真正的沉默泄漏會被報錯。
func TestUnhandledIndexDetectsLeaks(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantMin int
	}{
		{
			// 函式結果參數（非 ?T）直接承接安全索引 → 越界 option 未處理。
			name:    "let_result_non_option",
			src:     unhandledIdxSrc,
			wantMin: 1,
		},
		{
			// 既有變數的裸賦值 `b = arr[i]`（AssignExpression），LHS 非 ?T。
			name: "bare_assign_existing_var",
			src: `f = (arr []i64, i i64) () {
    b i64 = 0
    b = arr[i]
}`,
			wantMin: 1,
		},
		{
			// vec[T] 基底：越界同樣回傳 option，未處理應報。
			name: "vec_base",
			src: `get = (arr []i64, i i64) (res i64) {
    av = arr.to-vec()
    res = av[i]
}`,
			wantMin: 1,
		},
		{
			// 固定陣列 [N]T 基底：越界同樣回傳 option，未處理應報。
			name: "fixed_array_base",
			src: `get = (i i64) (res i64) {
    a [4]i64 = [10, 20, 30, 40]
    res = a[i]
}`,
			wantMin: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no")
			if len(res) < c.wantMin {
				t.Fatalf("expected >= %d unhandled-index error(s), got %d: %+v", c.wantMin, len(res), res)
			}
			for _, r := range res {
				if r.TraceID != unhandledIndexTraceID {
					t.Fatalf("unexpected TraceID %q: %+v", r.TraceID, r)
				}
			}
		})
	}
}

// TestUnhandledIndexAllowed 驗證合法寫法不應誤報。
func TestUnhandledIndexAllowed(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 顯式 `?=` 上拋 → 已處理。
			name: "explicit_unwrap_assign",
			src: `get = (arr []i64, i i64) (res ?i64) {
    res ?= arr[i]
}`,
		},
		{
			// `#{index-out = 0}` 註解 → 越界取預設值，已處理。
			name: "index_out_annotation",
			src: `get = (arr []i64, i i64) (res i64) {
    res = arr[i]  #{index-out = 0}
}`,
		},
		{
			// 返回 ?T 的函式中裸 `res = arr[i]` → lowering 自動改寫為 `?=`，已處理。
			name: "option_fn_auto_propagate",
			src: `get = (arr []i64, i i64) (res ?i64) {
    res = arr[i]
}`,
		},
		{
			// 顯式宣告為 option 區域變數 → 已處理。
			name: "explicit_option_local",
			src: `f = (arr []i64, i i64) () {
    x ?i64 = arr[i]
    io.outln('done')
}`,
		},
		{
			// str 索引回傳字元（非 option），不應報。
			name: "str_index_returns_char",
			src: `f = (s str, i i64) (res char) {
    res = s[i]
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no")
			if len(res) != 0 {
				t.Fatalf("expected no error for %s, got: %+v", c.name, res)
			}
		})
	}
}

// TestUnhandledIndexProvablyInBounds 驗證「可證明越界安全」的定長陣列讀取不再被
// 上報（見 parser/bounds.go）：基底為定長 [N]T、下標為整數字面量 0<=k<N，或下標恰
// 為 for-range 迴圈變數且其字面量取值區間恆在 [0,N) 時，讀取永不越界，也就無需
// `#{index-out}` 處理。此判定必須與 codegen（isSafeIndexBase）、`no fmt`（移除冗餘
// 註解）三方一致，否則「fmt 刪註解 → vet 立刻新增 ERROR」。
func TestUnhandledIndexProvablyInBounds(t *testing.T) {
	allowed := []struct {
		name string
		src  string
	}{
		{
			// 定長陣列 + 字面量下標在界內。
			name: "fixed_array_literal_index",
			src: `f = () (res i64) {
    a [4]i64 = [10, 20, 30, 40]
    res = a[0]
}`,
		},
		{
			// 定長陣列 + for-range 迴圈變數下標（[0..4) → 取值 [0..3] 恆 < 4）。
			name: "fixed_array_loop_index",
			src: `f = () (res i64) {
    a [4]i64 = [10, 20, 30, 40]
    i <- [0..4): {
        res = a[i]
    }
}`,
		},
	}
	for _, c := range allowed {
		t.Run(c.name, func(t *testing.T) {
			if res := ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no"); len(res) != 0 {
				t.Fatalf("expected no error for provably-in-bounds %s, got: %+v", c.name, res)
			}
		})
	}

	blocked := []struct {
		name string
		src  string
	}{
		{
			// 切片（[]T）運行期長度未知，保守視為可能越界。
			name: "slice_base_still_reported",
			src: `f = (v []i64) (res i64) {
    i <- [0..4): {
        res = v[i]
    }
}`,
		},
		{
			// for-range 端點非字面量（n 為參數）→ 無法證明界內。
			name: "non_literal_loop_bound",
			src: `f = (n i64) (res i64) {
    a [4]i64 = [10, 20, 30, 40]
    i <- [0..n): {
        res = a[i]
    }
}`,
		},
		{
			// 下標為複合算式（i + 1），非純字面量 / 迴圈變數。
			name: "composite_index",
			src: `f = () (res i64) {
    a [8]i64 = [0, 1, 2, 3, 4, 5, 6, 7]
    i <- [0..4): {
        #{overflow=wrap}
        res = a[i + 1]
    }
}`,
		},
	}
	for _, c := range blocked {
		t.Run(c.name, func(t *testing.T) {
			var idxErrs int
			for _, r := range ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no") {
				if r.TraceID == unhandledIndexTraceID {
					idxErrs++
				}
			}
			if idxErrs == 0 {
				t.Fatalf("expected unhandled-index error for non-provable %s, got none", c.name)
			}
		})
	}
}

// TestUnhandledIndexCompositeIndexNotProvable 單獨驗證複合下標（`a[i + 1]`）即使
// 迴圈區間看似安全，也不被證明在界內（bounds.go 只認整數字面量 / 純迴圈變數識別符）。
func TestUnhandledIndexCompositeIndexNotProvable(t *testing.T) {
	src := `f = (g i64) (res i64) {
    a [8]i64 = [0, 1, 2, 3, 4, 5, 6, 7]
    res = a[g]
}`
	if res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no"); len(res) == 0 {
		t.Fatalf("expected error for param-index (not provable), got none")
	}
}

// TestUnhandledIndexNoStdExempt 驗證撤銷標準庫豁免後的統一檢查：不再因
// mainFile 路徑含 std 而放行。原本的 std 豁免是為了繞開 merged 歸因不準導致的
// 偽報，現歸因已修好且 std 自身沉默泄漏已以 `#{index-out = DEF}` / `?=` 逐站
// 修復，故對所有程式碼（含路徑看似 std 的檔）一律檢查未處理索引。此與
// ValidateUnhandledOverflow 的 TestUnhandledOverflowNoStdExempt 保持對齊。
func TestUnhandledIndexNoStdExempt(t *testing.T) {
	cases := []struct {
		mainFile string
		want     int
	}{
		{mainFile: "std/str.no", want: 1},
		{mainFile: "/repo/src/std/str.no", want: 1},
		{mainFile: "src/std/byte.no", want: 1},
		{mainFile: "src/std/", want: 1}, // vet 目錄形態
		{mainFile: "src/app.no", want: 1},
		{mainFile: "", want: 1}, // 無來源資訊時保守檢查，避免漏報
	}
	for _, c := range cases {
		t.Run(c.mainFile, func(t *testing.T) {
			res := ValidateUnhandledIndex(parseProg(t, unhandledIdxSrc), c.mainFile)
			if len(res) != c.want {
				t.Fatalf("mainFile=%q: expected %d error(s), got %d: %+v", c.mainFile, c.want, len(res), res)
			}
		})
	}
}

// parseSurfaceProg 以「跳過安全索引降級」的方式解析（與 LSP 編輯器主解析同旗標，
// 見 lsp/documents.go：SkipSafeIndexLowering=true，為保住可 format-on-save 的 surface
// AST）。此時 `#{index-out = DEF}` 註解不會被降級成合成的 __idx_out_ tmp，
// ValidateUnhandledIndex 必須直接辨識陳述上的註解，否則會對已正確標註的越界索引
// 誤報「未處理」（編輯器持續提示，但 `no vet`（走降級）卻不報——兩端口徑不一致）。
func parseSurfaceProg(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// TestUnhandledIndexSurfaceAnnotationNotReported 驗證在 surface AST（未經降級）下，
// `#{index-out}` 行注解／尾隨注解所標註的索引讀取不再被誤報為未處理。
func TestUnhandledIndexSurfaceAnnotationNotReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "line_above_index_out_zero",
			src: `detect = (buf []byte) (b0 i64) {
    #{index-out=zero}
    b0 = buf[0]
}`,
		},
		{
			name: "trailing_index_out",
			src: `get = (arr []i64, i i64) (res i64) {
    res = arr[i]  #{index-out = 0}
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateUnhandledIndex(parseSurfaceProg(t, c.src), "src/app.no")
			for _, r := range res {
				if r.TraceID == unhandledIndexTraceID {
					t.Fatalf("surface AST false-positive on annotated index: L%d:C%d %s", r.Line, r.Column, r.Message)
				}
			}
		})
	}
}

// TestUnhandledIndexWithLenLiteralSliceInBounds 驗證 `buf []T = with-len(<字面量 K>)`
// 且本函數內未被重賦值的切片，其字面量下標 `< K` 被視為可證明界內——既不需要
// `#{index-out}`，也不應被上報（surface AST 與降級 AST 兩端口徑一致）。
func TestUnhandledIndexWithLenLiteralSliceInBounds(t *testing.T) {
	src := `get = () (res i64) {
    buf []i64 = with-len(4)
    res = buf[0]
}`
	for _, prog := range []*parser.Program{parseProg(t, src), parseSurfaceProg(t, src)} {
		for _, r := range ValidateUnhandledIndex(prog, "src/app.no") {
			if r.TraceID == unhandledIndexTraceID {
				t.Fatalf("with-len literal slice in-bounds read wrongly reported: L%d:C%d %s", r.Line, r.Column, r.Message)
			}
		}
	}
}

// TestUnhandledIndexWithLenReassignedSliceReported 驗證重賦值後長度不再可證明：
// `buf = with-len(2)` 之後的 `buf[0]` 無註解仍屬未處理越界索引，必須上報。
func TestUnhandledIndexWithLenReassignedSliceReported(t *testing.T) {
	src := `get = () (res i64) {
    buf []i64 = with-len(4)
    buf = with-len(2)
    res = buf[0]
}`
	res := ValidateUnhandledIndex(parseSurfaceProg(t, src), "src/app.no")
	found := false
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unhandled-index report for reassigned slice read, got %+v", res)
	}
}

// TestUnhandledIndexWiredIntoLints 確認規則已接入 RunAllLints（no vet / LSP 路徑），
// 且來源標記為 nolang-index。
func TestUnhandledIndexWiredIntoLints(t *testing.T) {
	prog := parseProg(t, unhandledIdxSrc)
	lints := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, l := range lints {
		if l.TraceID == unhandledIndexTraceID && l.Source == "nolang-index" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ValidateUnhandledIndex not surfaced through RunAllLints as nolang-index")
	}
}
