package checker

import "testing"

// 本檔案是「Option 捕獲賦值」（見 .trae/documents/option-capture-assign.md §4）
// 在 checker 側的回歸測試。D1/D2 之後，「捕獲」只決定 option 存在於哪個變數，
// 不再等於「已處理」：
//   - `_ = expr`（顯式捨棄）豁免 ovfhndld / idxhndld / ovf-int-default，也不報 optunuse；
//   - 被就地捕獲的賦值 → 產生處 HINT ovfhndld，且該變數仍必須被消費；
//   - 捕獲了卻從未被讀取 → ERROR optunuse；
//   - 沒有捕獲點的場景（非 option 函式內的根索引、return 運算式、索引寫入 LHS、
//     裸表達式陳述）仍照報。
//
// 這些規則由 parser 單測（src/parser/option_capture_test.go）與
// tests/option-capture.no 的執行期輸出覆蓋了改寫形狀，但 checker 的偵測面
// （ovfhndld / idxhndld / ovf-int-default / optunuse 的豁免與仍報）此前沒有
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

// TestCaptureAssignmentStillNeedsConsumption 驗證捕獲推斷（lowering 合成 `?T`）
// **不是**消費路徑：綁定本身仍必須被讀取。
//
// 這是 D1/D2 與舊語義的分界線。舊語義把「捕獲成功」等同於「已處理」，於是
// `a = b + c / 2` 之後 a 從此沒人讀也完全靜默。D1 明定註解／推斷都不消費 option，
// 所以：
//   - 產生處 → HINT ovfhndld（「該變數是 ?T，記得 match 或 ?=」，見 D2 分級）；
//   - 從未被讀取 → ERROR optunuse（錯誤被靜默丟棄，唯一該阻斷建置的診斷）。
//
// 舊斷言是「captured assignment must not raise ovfhndld」——它同時掩蓋了
// 「a 從未被讀取」這一事實，正是本次要修掉的盲點。
func TestCaptureAssignmentStillNeedsConsumption(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		traces map[string]int
	}{
		{
			// 內部可錯子表達式（`c / 2`）被就地捕獲 → 不再被當成沉默泄漏的硬錯誤，
			// 但仍要提示 + 仍要消費。
			name: "capture_inner_div_in_plain_fn",
			src: `f = (b i64, c i64) () {
    a = b + c / 2
    io.outln('done')
}`,
			traces: map[string]int{"ovfhndld": 1, "optunuse": 1, "idxhndld": 0},
		},
		{
			// 根即除法（inferOptionArith）→ 同上。
			name: "capture_root_div_in_plain_fn",
			src: `f = (b i64, c i64) () {
    a = b / c
    io.outln('done')
}`,
			traces: map[string]int{"ovfhndld": 1, "optunuse": 1, "idxhndld": 0},
		},
		{
			// option 結果函式內裸 `a = arr[i]` → 就地捕獲；索引不是算術，無 ovfhndld。
			name: "capture_root_safe_index_in_option_fn",
			src: `f = (arr []i64, i i64) (r ?i64) {
    a = arr[i]
    io.outln('done')
}`,
			traces: map[string]int{"ovfhndld": 0, "optunuse": 1, "idxhndld": 0},
		},
		{
			// option 型運算元作為可錯源被捕獲：捕獲的是運算元，不是本陳述的算術。
			name: "capture_option_operand",
			src: `f = (x ?i64) () {
    a = x + 1
    io.outln('done')
}`,
			traces: map[string]int{"ovfhndld": 0, "optunuse": 1, "idxhndld": 0},
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

// TestCaptureAssignmentConsumedIsClean 是上面那條的正面對照：同一個捕獲形狀，
// 只要把 option 真正消費掉（match / `?=` / `_ =`），三條診斷全消失。沒有這一條，
// 上面的 optunuse 斷言就無法區分「規則太嚴」與「確實未消費」。
func TestCaptureAssignmentConsumedIsClean(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 標準 4 行解包形態（D4/D8：空 `->` 兜底臂 = 默認零值 + 滿足窮盡性）。
			// 注意空臂必須獨立成行——`{ ok -> it, -> }` 的逗號形態不受支援
			//（`,` 在語句層被當作多重賦值分隔符），見 docs/design/option-unified.md §4.2。
			name: "consumed_by_match_empty_arm",
			src: `f = (b i64, c i64) () {
    a = b + c / 2
    d = a: {
        ok -> it
        ->
    }
    io.outln(d)
}`,
		},
		{
			name: "consumed_by_explicit_discard",
			src: `f = (b i64, c i64) () {
    a = b + c / 2
    _ = a
    io.outln('done')
}`,
		},
		{
			name: "consumed_by_propagate",
			src: `f = (b i64, c i64) (r ?i64) {
    a = b + c / 2
    r ?= a
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			counts := lintCounts(t, c.src)
			for _, id := range []string{"idxhndld", "optunuse"} {
				if counts[id] != 0 {
					t.Fatalf("%s: consumed option must not raise %s, got %d (%v)", c.name, id, counts[id], counts)
				}
			}
		})
	}
}

// TestCaptureTopLevelStillNeedsConsumption 驗證頂層指令碼的捕獲同樣不是消費路徑：
// `a = b + c / 2` 展開成 `a ?T = nil` 預宣告 + ok 臂最終賦值，a 從此沒人讀時
// 仍是 optunuse。
func TestCaptureTopLevelStillNeedsConsumption(t *testing.T) {
	const src = `b i64 = 6
c i64 = 2
a = b + c / 2
io.outln('done')`
	counts := lintCounts(t, src)
	if counts["idxhndld"] != 0 {
		t.Fatalf("top-level capture must not raise idxhndld, got %d (%v)", counts["idxhndld"], counts)
	}
	if counts["ovfhndld"] != 1 {
		t.Fatalf("top-level capture must raise one ovfhndld HINT, got %d (%v)", counts["ovfhndld"], counts)
	}
	if counts["optunuse"] != 1 {
		t.Fatalf("top-level unconsumed option must raise optunuse, got %d (%v)", counts["optunuse"], counts)
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
			// 純算術綁定：lowering 推斷 x 為 ?i64，故產生處只報 ovfhndld HINT
			//（ovf-int-default 的「建議加註解」被「目標已是 ?T」吸收——見
			// TestCaptureOptionalTargetSuppressesIntOverflow）。x 被 io.outln 讀取
			// 故不報 optunuse。
			name: "plain_arith_assignment",
			src: `f = (a i64, b i64) () {
    x = a + b
    io.outln(x)
}`,
			traces: map[string]int{"ovfhndld": 1, "ovf-int-default": 0, "optunuse": 0},
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
