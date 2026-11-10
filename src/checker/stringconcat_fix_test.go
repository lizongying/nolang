package checker

// stringconcat_fix_test.go — FixStringConcatOperators（`no fmt` 自動替換）的
// 同口徑測試：修的位置必須等於報的位置（a3yrogp1），修完報告清零、二次調用
// 零翻轉（冪等）。走訪形態與 `no fmt` 一致：SkipLowering 糖 AST + BuildTHIRView。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// fixParseSugar 以 `no fmt` 的形態（糖 AST）解析並建 view。
func fixParseSugar(t *testing.T, src string) (*parser.Program, *THIRView) {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	view := BuildTHIRView(prog)
	if view == nil {
		t.Fatal("BuildTHIRView returned nil")
	}
	return prog, view
}

// concatFixCountInfixOp 統計各函式體 let 值表達式裡指定運算符的中綴節點數。
func concatFixCountInfixOp(prog *parser.Program, op string) int {
	var n int
	var walkExpr func(e parser.Expression)
	walkExpr = func(e parser.Expression) {
		switch x := e.(type) {
		case *parser.InfixExpression:
			if x.Operator == op {
				n++
			}
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *parser.CallExpression:
			for _, a := range x.Arguments {
				walkExpr(a)
			}
		}
	}
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Body != nil {
			for _, bs := range fd.Body.Statements {
				if ls, ok := bs.(*parser.LetStatement); ok {
					walkExpr(ls.Value)
				}
			}
		}
	}
	return n
}

func TestFixStringConcatParityWithReports(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantFix int
	}{
		// 任一側為 str 字面量：報並翻。
		{"literal-left", "f = (a str) (r str) {\n\tr = 'x' + a\n}\n", 1},
		// 兩側都確定為字串（THIR 精化，a3yrogp1 補報口徑）：報並翻。
		{"str-plus-str", "f = (a str, b str) (r str) {\n\tr = a + b\n}\n", 1},
		// 純數值加法：不報也不翻。
		{"num-plus-num", "f = (a i64, b i64) (r i64) {\n\tr = a + b\n}\n", 0},
		// 鏈式：兩站點都命中（外側右邊字面量、內側左邊字面量）。
		{"chain", "f = (a str) (r str) {\n\tr = 'x' + a + 'y'\n}\n", 2},
	}
	for _, c := range cases {
		prog, view := fixParseSugar(t, c.src)
		before := ValidateStringConcat(prog, view)
		if n := FixStringConcatOperators(prog, view); n != c.wantFix {
			t.Errorf("[%s] flips = %d, want %d", c.name, n, c.wantFix)
		}
		// 同口徑：修復站點數 == 報告站點數。
		if len(before) != c.wantFix {
			t.Errorf("[%s] reports before fix = %d, want %d", c.name, len(before), c.wantFix)
		}
		// 修完清零：同 view 再報必須為 0（否則 fmt 後 vet 仍見同一 HINT）。
		if after := ValidateStringConcat(prog, view); len(after) != 0 {
			t.Errorf("[%s] reports after fix = %d, want 0: %v", c.name, len(after), after)
		}
		// 冪等：二次翻轉必須零改動。
		if n := FixStringConcatOperators(prog, view); n != 0 {
			t.Errorf("[%s] second fix flips = %d, want 0", c.name, n)
		}
	}
}

// TestFixStringConcatKeepsNumericPlus 確認翻只發生在拼接站點：數值 `+` 原樣保留。
func TestFixStringConcatKeepsNumericPlus(t *testing.T) {
	src := "f = (a i64, s str) (r str, n i64) {\n\tn = a + 1\n\tr = s + 'x'\n}\n"
	prog, view := fixParseSugar(t, src)
	if n := FixStringConcatOperators(prog, view); n != 1 {
		t.Fatalf("flips = %d, want 1", n)
	}
	if got := concatFixCountInfixOp(prog, "+"); got != 1 {
		t.Errorf("remaining '+' infix count = %d, want 1 (numeric a + 1 untouched)", got)
	}
	if got := concatFixCountInfixOp(prog, "-"); got != 1 {
		t.Errorf("'-' infix count = %d, want 1", got)
	}
}

// TestFixStringConcatUnannotatedOperandStays 型別未知的運算元（呼叫結果未標註）
// 維持保守口徑：不報不翻，避免把數值 `+` 誤改成拼接 `-`。
func TestFixStringConcatUnannotatedOperandStays(t *testing.T) {
	src := "g = () (v i64) {\n\tv = 1\n}\nf = () (r i64) {\n\tr = g() + 1\n}\n"
	prog, view := fixParseSugar(t, src)
	if got := len(ValidateStringConcat(prog, view)); got != 0 {
		t.Fatalf("reports = %d, want 0", got)
	}
	if n := FixStringConcatOperators(prog, view); n != 0 {
		t.Errorf("flips = %d, want 0", n)
	}
}

// TestFixStringConcatMixedChain 回歸 2026-11-10：鏈式拼接裡摻了非字面量運算元
// （欄位存取、方法呼叫）時，`(… + 'b') + y` 這種中間節點兩側都不是字面量，
// THIR 又因外層 `+` 給不出 str 型別 ⇒ 舊口徑只翻一半，`no fmt` 產出
// `'a' - x - 'b' + y - 'c'` 的混合 +/- 壞碼。現在整條鏈必須一致翻成 '-'。
func TestFixStringConcatMixedChain(t *testing.T) {
	src := "f = () (r str) {\n\tr = 'line ' + a + ', column ' + b + ': ' + c + ' [' + d + ']'\n}\n"
	prog, view := fixParseSugar(t, src)
	const want = 8
	if got := len(ValidateStringConcat(prog, view)); got != want {
		t.Fatalf("reports before fix = %d, want %d", got, want)
	}
	if n := FixStringConcatOperators(prog, view); n != want {
		t.Fatalf("flips = %d, want %d", n, want)
	}
	if got := len(ValidateStringConcat(prog, view)); got != 0 {
		t.Errorf("reports after fix = %d, want 0: %v", got, ValidateStringConcat(prog, view))
	}
	if n := FixStringConcatOperators(prog, view); n != 0 {
		t.Errorf("second fix flips = %d, want 0", n)
	}
	if got := concatFixCountInfixOp(prog, "+"); got != 0 {
		t.Errorf("remaining '+' infix count = %d, want 0", got)
	}
	if got := concatFixCountInfixOp(prog, "-"); got != want {
		t.Errorf("'-' infix count = %d, want %d", got, want)
	}
}

// TestFixStringConcatChainGuards 守住「鏈式補報」的三條邊界：純數值加法鏈、
// 數值減法（'-' 不必然是拼接）、括號包裹的拼接鏈。
func TestFixStringConcatChainGuards(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantFix  int
		wantPlus int
	}{
		{
			name:     "pure numeric chain stays untouched",
			src:      "f = (a i64, b i64) (n i64) {\n\tn = a + b + 1\n}\n",
			wantFix:  0,
			wantPlus: 2,
		},
		{
			name:     "numeric minus is not a concat chain",
			src:      "f = (a i64, b i64) (n i64) {\n\tn = (a - 1) + b\n}\n",
			wantFix:  0,
			wantPlus: 1,
		},
		{
			name:     "grouped concat chain flips both nodes",
			src:      "f = (a str, b str) (r str) {\n\tr = ('x' + a) + b\n}\n",
			wantFix:  2,
			wantPlus: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog, view := fixParseSugar(t, c.src)
			if got := len(ValidateStringConcat(prog, view)); got != c.wantFix {
				t.Fatalf("reports = %d, want %d", got, c.wantFix)
			}
			if n := FixStringConcatOperators(prog, view); n != c.wantFix {
				t.Fatalf("flips = %d, want %d", n, c.wantFix)
			}
			if got := concatFixCountInfixOp(prog, "+"); got != c.wantPlus {
				t.Errorf("remaining '+' infix count = %d, want %d", got, c.wantPlus)
			}
			if got := len(ValidateStringConcat(prog, view)); got != 0 {
				t.Errorf("reports after fix = %d, want 0", got)
			}
		})
	}
}
