package checker

import "testing"

// 本檔案是 ValidateUnhandledIndex 的回歸測試：arr/vec/slice 索引 b[i] 越界時
// 預設回傳 option<elem>（永不 panic），若結果既沒被 `?=` 上拋、沒被 `#{index-out
// = DEF}` 註解處理、也沒在返回 ?T 的函式中自動上拋，即「沉默泄漏」，必須報錯
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

// TestUnhandledIndexStdExempt 驗證標準庫（std）豁免：std 內部以舊式越界 panic
// 寫法（數千處）出現，暫不強制處理。mainFile 含 "/std/" 片段（絕對路徑或目錄
// 形態）皆應豁免。
func TestUnhandledIndexStdExempt(t *testing.T) {
	cases := []struct {
		mainFile string
		want     int
	}{
		{mainFile: "std/str.no", want: 0},
		{mainFile: "/repo/src/std/str.no", want: 0},
		{mainFile: "src/std/byte.no", want: 0},
		{mainFile: "src/std/", want: 0}, // vet 目錄形態
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
