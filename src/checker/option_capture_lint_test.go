package checker

import "testing"

// 本檔案是「Option 捕獲賦值」（見 .trae/documents/option-capture-assign.md §4）
// 在 checker 側的回歸測試：捕獲推斷（lowering 合成 `?T`）之後，
//   - `_ = expr`（顯式捨棄）豁免 ovfhndld / idxhndld / ovf-int-default；
//   - 被就地捕獲的賦值不再報「沉默泄漏」；
//   - 沒有捕獲點的場景（非 option 函式內的根索引、return 運算式、索引寫入 LHS、
//     裸表達式陳述）仍照報。
//
// 這些規則由 parser 單測（src/parser/option_capture_test.go）與
// tests/option-capture.no 的執行期輸出覆蓋了改寫形狀，但 checker 的偵測面
// （ovfhndld / idxhndld / ovf-int-default 三條 lint 的豁免與仍報）此前沒有
// 專屬單元測試，故補上。

// lintCounts 回傳 RunAllLints 結果中每個 TraceID 的出現次數。
func lintCounts(t *testing.T, src string) map[string]int {
	t.Helper()
	lints := RunAllLints(parseProg(t, src), LintOptions{SourcePath: "src/app.no"})
	counts := map[string]int{}
	for _, l := range lints {
		counts[l.TraceID]++
	}
	return counts
}

// TestCaptureDiscardUnderscoreExempt 驗證 `_ = expr` 三種可錯源一律豁免：
// 使用者已顯式表明不要這個值與其錯誤，且求值在執行期安全（捕獲推斷讓除零 /
// 越界 / 溢出都走 option 路徑，不會 UB）。
func TestCaptureDiscardUnderscoreExempt(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "discard_arithmetic_overflow",
			src: `f = (a i64, b i64) () {
    _ = a + b
    io.outln('done')
}`,
		},
		{
			name: "discard_negation",
			src: `f = (a i64) () {
    _ = -a
    io.outln('done')
}`,
		},
		{
			name: "discard_safe_index",
			src: `f = (arr []i64, i i64) () {
    _ = arr[i]
    io.outln('done')
}`,
		},
		{
			name: "discard_division",
			src: `f = (b i64, c i64) () {
    _ = b / c
    io.outln('done')
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n := len(ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no")); n != 0 {
				t.Fatalf("`_ =` must be exempt from idxhndld, got %d", n)
			}
			if n := len(ValidateUnhandledOverflow(parseProg(t, c.src), "src/app.no")); n != 0 {
				t.Fatalf("`_ =` must be exempt from ovfhndld, got %d", n)
			}
			counts := lintCounts(t, c.src)
			for _, id := range []string{"idxhndld", "ovfhndld", "ovf-int-default"} {
				if counts[id] != 0 {
					t.Fatalf("`_ =` must not raise %s, got %d (%v)", id, counts[id], counts)
				}
			}
		})
	}
}

// TestCaptureAssignmentNotReported 驗證被就地捕獲的賦值不再被報成沉默泄漏。
// 這是本次語義變更的核心：`a = expr`（expr 含可錯源）推斷 a 為 `?T` 並把
// nil/err 存進 a，於是「option 未被處理」的前提不成立。
func TestCaptureAssignmentNotReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 內部可錯子表達式（`c / 2`）被就地捕獲 → 不再 ovfhndld。
			name: "capture_inner_div_in_plain_fn",
			src: `f = (b i64, c i64) () {
    a = b + c / 2
    io.outln('done')
}`,
		},
		{
			// 根即除法的推斷（inferOptionDivMod）→ 維持既有豁免。
			name: "capture_root_div_in_plain_fn",
			src: `f = (b i64, c i64) () {
    a = b / c
    io.outln('done')
}`,
		},
		{
			// option 結果函式內裸 `a = arr[i]` → 就地捕獲（取代舊的自動 `?=`）。
			name: "capture_root_safe_index_in_option_fn",
			src: `f = (arr []i64, i i64) (r ?i64) {
    a = arr[i]
    io.outln('done')
}`,
		},
		{
			// option 型運算元作為可錯源被捕獲。
			name: "capture_option_operand",
			src: `f = (x ?i64) () {
    a = x + 1
    io.outln('done')
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n := len(ValidateUnhandledIndex(parseProg(t, c.src), "src/app.no")); n != 0 {
				t.Fatalf("captured assignment must not raise idxhndld, got %d", n)
			}
			if n := len(ValidateUnhandledOverflow(parseProg(t, c.src), "src/app.no")); n != 0 {
				t.Fatalf("captured assignment must not raise ovfhndld, got %d", n)
			}
			counts := lintCounts(t, c.src)
			for _, id := range []string{"idxhndld", "ovfhndld", "ovf-int-default"} {
				if counts[id] != 0 {
					t.Fatalf("captured assignment must not raise %s, got %d (%v)", id, counts[id], counts)
				}
			}
		})
	}
}

// TestCaptureTopLevelNotReported 驗證頂層指令稿的捕獲同樣不被 ovf-int-default
// 誤報：`a = b + c / d` 展開成 `a ?T = nil` 預宣告 + ok 臂最終賦值，被 collectTopLevelLets
// 登記為 `?T` 而豁免。
func TestCaptureTopLevelNotReported(t *testing.T) {
	const src = `b i64 = 6
c i64 = 2
a = b + c / 2
io.outln('done')`
	counts := lintCounts(t, src)
	for _, id := range []string{"idxhndld", "ovfhndld", "ovf-int-default"} {
		if counts[id] != 0 {
			t.Fatalf("top-level capture must not raise %s, got %d (%v)", id, counts[id], counts)
		}
	}
}

// TestCaptureNoCapturePointStillReported 驗證沒有捕獲點的場景仍照報——捕獲只
// 覆蓋「普通 `=` 綁定含可錯源」的形狀，其餘上下文維持舊語義。
func TestCaptureNoCapturePointStillReported(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		traces map[string]int
	}{
		{
			// 非 option 函式內的根索引：捕獲閘門（enclosingFuncHasOptResult）
			// 不成立，維持純量元素型別 → 越界 option 未被處理。
			name: "root_index_outside_option_fn",
			src: `f = (arr []i64, i i64) () {
    a = arr[i]
    io.outln('done')
}`,
			traces: map[string]int{"idxhndld": 1},
		},
		{
			// return / 結果參數賦值：非 LetStatement 綁定形狀，無捕獲點。
			name: "index_into_plain_result_param",
			src: `f = (arr []i64, i i64) (r i64) {
    r = arr[i]
}`,
			traces: map[string]int{"idxhndld": 1},
		},
		{
			// 索引寫入 LHS：`k[i]` 是目標而非賦值來源，不捕獲。
			name: "index_write_lhs",
			src: `f = (k []i64, v []i64, i i64) () {
    k[i] = v[i]
    io.outln('done')
}`,
			traces: map[string]int{"idxhndld": 1},
		},
		{
			// 純算術（無可錯源）不做捕獲，裸賦值仍是沉默溢出。
			name: "plain_arith_assignment",
			src: `f = (a i64, b i64) () {
    x = a + b
    io.outln(x)
}`,
			traces: map[string]int{"ovfhndld": 1, "ovf-int-default": 1},
		},
		{
			// 裸表達式陳述（無 `_ =`）多半是誤寫，與 `_ =` 豁免區分開。
			name: "bare_expression_statement",
			src: `f = (a i64, b i64) () {
    a + b
}`,
			traces: map[string]int{"ovfhndld": 1, "ovf-int-default": 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			counts := lintCounts(t, c.src)
			for id, want := range c.traces {
				if counts[id] != want {
					t.Fatalf("%s: %s count = %d, want %d (all: %v)", c.name, id, counts[id], want, counts)
				}
			}
		})
	}
}

// TestCaptureOptionalTargetSuppressesIntOverflow 驗證「目標本身是 option」時
// ovf-int-default 不再誤報：`x ?i64 = nil` 之後的 `x = a + b` 把溢出收進 option
// 變數（運行期存成 err），屬已處理。這同時是捕獲 pass 生成的 `a ?T = nil` 預宣告
// + ok 臂賦值（後者不帶型別節點）依賴的豁免路徑。
func TestCaptureOptionalTargetSuppressesIntOverflow(t *testing.T) {
	const src = `f = (a i64, b i64) () {
    x ?i64 = nil
    x = a + b
    io.outln('done')
}`
	counts := lintCounts(t, src)
	if counts["ovf-int-default"] != 0 {
		t.Fatalf("option-typed target must suppress ovf-int-default, got %d (%v)", counts["ovf-int-default"], counts)
	}
}
