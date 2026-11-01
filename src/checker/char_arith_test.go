package checker

// char_arith_test.go — 「char 與沒有數值意義的運算元做算術」必須報錯（trace k2m9xq4t）。
//
// Nolang 的引號方向容易搞反，先釘死：
//   - `"..."` 是 **char** 字面量（i32 碼點），`"x"` 就是 120；
//   - `'...'` 是 **str** 字面量（即使只有一個碼點），`'x'` 是長度 1 的字串。
//
// char 與整數 / char / str 都有明確語意（115 / 23 / "xhi"），這幾種必須放行；
// 但 bool / f64 / 結構體 / 容器沒有位元組語意 —— 舊行為把 bool 當 1、把 f64 截成 1，
// `"x" - flag` 靜默得到 119。這是一個看似合理、實則毫無意義的數字，且與
// `'x' - flag` 的 "xtrue"（str 拼接）不一致，所以一律報錯。
//
// 判據走 THIRView.TypeOf（表達式級型別表），任一側型別未知即不報 —— 這是保守口徑，
// 與 StringConcat 的型別精化同源。因此本測試**必須**先 ResolveProgram 再
// BuildTHIRView，否則型別表是空的，測試會「綠得很假」。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

const charArithTraceID = "k2m9xq4t"

// parseProgResolved 解析 + 解析語意（型別表填充）。ValidateCharArith 的判據全在
// THIRView.TypeOf 上，沒有這一步就什麼都查不到。
func parseProgResolved(t *testing.T, src string) *parser.Program {
	t.Helper()
	prog := parseProg(t, src)
	parser.ResolveProgram(prog)
	return prog
}

// validateCharArith 走生產路徑：BuildTHIRView（HIR 側表）→ ValidateCharArith。
func validateCharArith(t *testing.T, src string) []ValidateResult {
	t.Helper()
	prog := parseProgResolved(t, src)
	return ValidateCharArith(prog, BuildTHIRView(prog))
}

// TestCharArithRejectsNonNumericOperand 正例：char 與 bool / f64 的算術必須報。
func TestCharArithRejectsNonNumericOperand(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantBad string // 文案裡必須點名的運算元
		wantTyp string // 文案裡必須印出的型別
	}{
		{
			name:    "char minus bool",
			src:     `f = (flag bool) () { io.outln("x" - flag) }`,
			wantBad: "flag",
			wantTyp: "bool",
		},
		{
			name:    "char minus float",
			src:     `f = (r f64) () { io.outln("x" - r) }`,
			wantBad: "r",
			wantTyp: "f64",
		},
		{
			name:    "bool minus char",
			src:     `f = (flag bool) () { io.outln(flag - "x") }`,
			wantBad: "flag",
			wantTyp: "bool",
		},
		{
			name:    "char times float",
			src:     `f = (r f64) () { io.outln("x" * r) }`,
			wantBad: "r",
			wantTyp: "f64",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := validateCharArith(t, tc.src)
			if len(res) != 1 {
				t.Fatalf("expected exactly 1 %s, got %d: %+v", charArithTraceID, len(res), res)
			}
			r := res[0]
			if r.TraceID != charArithTraceID {
				t.Errorf("TraceID = %q, want %q", r.TraceID, charArithTraceID)
			}
			if !strings.Contains(r.Message, tc.wantBad) {
				t.Errorf("message does not name the offending operand %q:\n%s", tc.wantBad, r.Message)
			}
			if !strings.Contains(r.Message, tc.wantTyp) {
				t.Errorf("message does not print the operand type %q:\n%s", tc.wantTyp, r.Message)
			}
			// 文案必須給出可執行的改寫方向（改用 str 字面量 ⇒ 走 to-str 拼接）。
			if !strings.Contains(r.Message, "'x' - "+tc.wantBad) {
				t.Errorf("message does not show the str-literal rewrite:\n%s", r.Message)
			}
		})
	}
}

// TestCharArithAllowsNumericOperand 負例：整數 / char / str 都是 char 的合法同伴。
// 這幾條若被誤報，等於把 `c - '0'` 這類慣用法打死。
func TestCharArithAllowsNumericOperand(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"char minus int", `f = () () { io.outln("x" - 1) }`},
		{"char minus char", `f = (c char) () { io.outln("x" - c) }`},
		{"char minus str", `f = (s str) () { io.outln("x" - s) }`},
		{"char minus char literal", `f = () () { io.outln("x" - "y") }`},
		{"char times int", `f = () () { io.outln("x" * 3) }`},
		{"int minus char", `f = () () { io.outln(1 - "x") }`},
		// `'...'` 是 str 字面量 ⇒ 走拼接，不是 char 算術，本 pass 不管。
		{"str literal minus bool", `f = (flag bool) () { io.outln('x' - flag) }`},
		// 比較運算符不歸本 pass（ValidateStrOrdering 負責）。
		{"char compared to bool", `f = (flag bool) () { io.outln("x" == flag) }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if res := validateCharArith(t, tc.src); len(res) != 0 {
				t.Fatalf("expected no %s, got %d: %+v", charArithTraceID, len(res), res)
			}
		})
	}
}

// TestCharArithPositionIsInfixOperator 位置必須錨在運算符本身，而不是運算元上 ——
// LSP 用它劃波浪線，錨錯地方用戶會以為是某個變數有問題。
func TestCharArithPositionIsInfixOperator(t *testing.T) {
	src := `f = (flag bool) () {
    x i64 = "x" - flag
    io.outln(x)
}`
	res := validateCharArith(t, src)
	if len(res) != 1 {
		t.Fatalf("expected 1 report, got %d: %+v", len(res), res)
	}
	if res[0].Line != 2 {
		t.Errorf("Line = %d, want 2 (the infix line)", res[0].Line)
	}
	// `-` 在第 2 行第 17 列（4 空格缩进 + "x i64 = \"x\" " ）。
	if res[0].Column != 17 {
		t.Errorf("Column = %d, want 17 (the `-` operator)", res[0].Column)
	}
}

// TestCharArithSkipsUnknownTypes 型別未知時必須沉默 —— 保守口徑是這個 pass 的
// 安全底線：把未解析的拼寫當成「非數值」會製造假陽性。
func TestCharArithSkipsUnknownTypes(t *testing.T) {
	// `nope` 未宣告，型別表查不到 ⇒ 不報（未定義變數由 ValidateUndefinedVars 負責）。
	prog := parseProgResolved(t, `f = () () { io.outln("x" - nope) }`)
	if res := ValidateCharArith(prog, BuildTHIRView(prog)); len(res) != 0 {
		t.Fatalf("unknown operand type must not be flagged, got %d: %+v", len(res), res)
	}
}

// TestCharArithLintRegistered 這條規則同時掛在 lint 表上（surface=false ⇒ THIR 階段），
// 且必須走 RunAllLints 也能拿到 —— 否則 `no vet` / LSP 看不到。
func TestCharArithLintRegistered(t *testing.T) {
	prog := parseProgResolved(t, `f = (flag bool) () { io.outln("x" - flag) }`)
	lints := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, l := range lints {
		if l.TraceID != charArithTraceID {
			continue
		}
		found = true
		if l.Severity != LintError {
			t.Errorf("severity = %v, want LintError", l.Severity)
		}
		// Source 是這條 lint 的來源標籤；File 在「記憶體字串解析」的 program 上
		// 本來就是空的（沒有檔案歸因），由調用方回退到輸入路徑。
		if l.Source != "nolang-type-checker" {
			t.Errorf("Source = %q, want nolang-type-checker", l.Source)
		}
	}
	if !found {
		t.Fatalf("no %s in RunAllLints output: %+v", charArithTraceID, lints)
	}
}
