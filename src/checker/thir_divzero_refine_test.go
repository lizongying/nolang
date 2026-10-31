package checker

import "testing"

// 本文件锁定 6C-optional 第三批「DivByZero 型別確認精化」的行為變更語義。
//
// 背景（摸底結論）：Nolang 允許 `f64 / 0` 混合運算（0 提升為 0.0，types=0），
// 該除法走 LLVM fdiv → IEEE-754 定義 ±inf，**不是** sdiv/srem UB。精化前對
// `f64 / 0` / `f64 % 0` 一律報 div-by-zero —— 真實誤報。精化後被除數確定為
// 浮點時跳過。
//
// 行為變更性質：**只減不增**（浮點用例由報轉不報，其餘用例報告數不變）。

func TestDivByZeroFloatRefine(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantRefine int // 精化後（生產路徑）應有的報告數
		wantLegacy int // view=nil（純語法、精化前口徑）應有的報告數
	}{
		// 整數除數 0：真 UB，精化後仍必須報
		{"int-div-zero", "main = (a i64) (r i64) {\n\tr = a / 0\n}\n", 1, 1},
		{"int-mod-zero", "main = (a i64) (r i64) {\n\tr = a % 0\n}\n", 1, 1},
		// 浮點被除數 ÷ 整數字面 0：fdiv 非 UB ⇒ 精化後不報（此為本批行為變更）
		{"float-div-int-zero", "main = (a f64) (r f64) {\n\tr = a / 0\n}\n", 0, 1},
		{"float-mod-int-zero", "main = (a f64) (r f64) {\n\tr = a % 0\n}\n", 0, 1},
		// 浮點字面 0.0 作除數：isLiteralZeroDivisor 本就排除，兩側都不報
		{"float-div-float-zero", "main = (a f64) (r f64) {\n\tr = a / 0.0\n}\n", 0, 0},
		// 負零包住的整數 0：仍報
		{"int-div-neg-zero", "main = (a i64) (r i64) {\n\tr = a / -0\n}\n", 1, 1},
	}
	for _, c := range cases {
		// view 與 program 必須同源：IdMap 以 AST 指針為鍵，重建 program 會讓
		// TypeOf 查空 → 精化判據失效。
		prog, view := shadowBuildView(t, c.src)
		if got := len(ValidateDivByZero(prog, view)); got != c.wantRefine {
			t.Errorf("[%s] refined div-by-zero reports = %d, want %d", c.name, got, c.wantRefine)
		}
		if got := len(ValidateDivByZero(prog, nil)); got != c.wantLegacy {
			t.Errorf("[%s] legacy (view=nil) div-by-zero reports = %d, want %d", c.name, got, c.wantLegacy)
		}
	}
}

// TestDivByZeroRefineOnlyRemovesReports 斷言精化的**只減不增**性質：在影子語料上，
// 帶 view 的報告多重集必須是 view=nil 多重集的子集（計數逐鍵 <=）。
func TestDivByZeroRefineOnlyRemovesReports(t *testing.T) {
	for _, tc := range shadowSynth {
		prog, view := shadowBuildView(t, tc.src)
		refined := ValidateDivByZero(prog, view)
		legacy := ValidateDivByZero(prog, nil)
		type key struct {
			file  string
			line  int
			col   int
			trace string
		}
		count := func(rs []ValidateResult) map[key]int {
			m := map[key]int{}
			for _, r := range rs {
				m[key{r.File, r.Line, r.Column, r.TraceID}]++
			}
			return m
		}
		rc, lc := count(refined), count(legacy)
		for k, n := range rc {
			if n > lc[k] {
				t.Errorf("%s: refined report %v count %d exceeds legacy %d — refine must only remove", tc.name, k, n, lc[k])
			}
		}
	}
}
