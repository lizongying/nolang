package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// option_divmod_infer_test.go — 有號整數 `/` `%` 的 option 型別推斷。
//
// 背景：`a / b`、`a % b`（有號整數）本身就產生 option（除零/溢出為 err），
// 因此 `q = a / b` 的綁定型別必然是 `?T`，無需用戶顯式標註。lowering pass
// （inferOptionDivMod）就地補上合成的 `?T` 註解，使 checker / MIR / LSP 復用
// 顯式寫法 `q ?int = a / b` 的既有路徑。反之，推斷不出時必須保持不改寫
// （無標註 → 由 ovfhndld 引導），否則會改動既有語義。

// letTypeOf 解析 src，回傳 named 為 `name` 的 LetStatement 之型別字串
// （無型別註解回 ""），並附帶該變數在語義表中的型別。
func letTypeOf(t *testing.T, src, name string) (astType string, semType string) {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	for _, s := range prog.Statements {
		ls, ok := s.(*LetStatement)
		if !ok || ls.Name == nil || ls.Name.Value != name {
			continue
		}
		if ls.Type != nil {
			astType = typeString(ls.Type)
		}
		if p.sem != nil {
			semType = p.sem.VarTypes[name]
		}
		return astType, semType
	}
	t.Fatalf("no LetStatement named %q in %q", name, src)
	return "", ""
}

func TestOptionDivModInference(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		varName string
		want    string
	}{
		{
			name:    "div of annotated vars infers ?int",
			src:     "a int = 10\nb int = 2\nq = a / b\n",
			varName: "q",
			want:    "?int",
		},
		{
			name:    "mod of annotated vars infers ?int",
			src:     "a int = 10\nb int = 2\nr = a % b\n",
			varName: "r",
			want:    "?int",
		},
		{
			name:    "i64 operands infer ?i64",
			src:     "a i64 = 10\nb i64 = 2\nq = a / b\n",
			varName: "q",
			want:    "?i64",
		},
		{
			name:    "literal divisor is signed int",
			src:     "a int = 10\nq = a / 2\n",
			varName: "q",
			want:    "?int",
		},
		{
			name:    "grouped operands still inferred",
			src:     "a int = 10\nb int = 2\nq = (a) / (b)\n",
			varName: "q",
			want:    "?int",
		},
		{
			// 冪等：顯式標註必須原樣保留，不得被二次包裹成 ??int。
			name:    "explicit annotation untouched",
			src:     "a int = 10\nb int = 2\nq ?int = a / b\n",
			varName: "q",
			want:    "?int",
		},
		{
			// `+ - *` 不屬於本推斷（仍由 ovfhndld 要求顯式處理）。
			name:    "addition not inferred",
			src:     "a int = 10\nb int = 2\nc = a + b\n",
			varName: "c",
			want:    "",
		},
		{
			name:    "multiplication not inferred",
			src:     "a int = 10\nb int = 2\nc = a * b\n",
			varName: "c",
			want:    "",
		},
		{
			// 無號除法是普通整數值，不產生 option。
			name:    "unsigned div not inferred",
			src:     "a u8 = 10\nb u8 = 2\nq = a / b\n",
			varName: "q",
			want:    "",
		},
		{
			// i128 的 codegen 退化為回繞，不產生 option。
			name:    "i128 div not inferred",
			src:     "a i128 = 10\nb i128 = 2\nq = a / b\n",
			varName: "q",
			want:    "",
		},
		{
			name:    "float div not inferred",
			src:     "a float = 10.0\nb float = 2.0\nq = a / b\n",
			varName: "q",
			want:    "",
		},
		{
			// `#{overflow=wrap}` 是用戶已顯式處理溢出的標記，不可覆寫。
			name:    "overflow annotation wins",
			src:     "a int = 10\nb int = 2\n#{overflow=wrap}\nq = a / b\n",
			varName: "q",
			want:    "",
		},
		{
			// 運算元型別未知 → 保守不改寫。
			name:    "unknown operand type not inferred",
			src:     "q = unknown-fn() / 2\n",
			varName: "q",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, sem := letTypeOf(t, tc.src, tc.varName)
			if got != tc.want {
				t.Errorf("AST type = %q, want %q (src=%q)", got, tc.want, tc.src)
			}
			wantSem := tc.want
			if wantSem == "?int" {
				wantSem = "?number.int" // 全域表以限定名記錄 int
			}
			if sem != tc.want && sem != wantSem {
				t.Errorf("semantic table type = %q, want %q (src=%q)", sem, tc.want, tc.src)
			}
		})
	}
}

// TestOptionDivModInferenceSkippedForFmt 保護 formatter 路徑：`no fmt` 必須看到
// 未經改寫的表層 AST，否則會把推斷出的 `?int` 印回原始碼（用戶從未寫過它）。
func TestOptionDivModInferenceSkippedForFmt(t *testing.T) {
	const src = "a int = 10\nb int = 2\nq = a / b\n"
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = true
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	for _, s := range prog.Statements {
		if ls, ok := s.(*LetStatement); ok && ls.Name != nil && ls.Name.Value == "q" {
			if ls.Type != nil {
				t.Fatalf("fmt path synthesized type %q; formatter would leak the annotation", typeString(ls.Type))
			}
			return
		}
	}
	t.Fatal("no LetStatement named q")
}
