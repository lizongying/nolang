package fmt

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 布林比較化簡的可執行契約（docs/docs/lang/code-style.md、SKILL.md 同名章節）：
//
//	**已知的化簡，未知的不化簡。**
//
// 本檔刻意用裸 `nfmt.Format*` 呼叫（零值 ParseConfig）驗證**單檔視野**：
// 現在 `no fmt` / LSP 會注入 `FmtSigLoader` 的 std + 本地模組簽名，跨模組的
// bool 證據由 `signatures_test.go` 與 `lsp/fmt_*_test.go` 覆蓋，別把這裡的
// 「未知」誤讀成「工具鏈看不到」。只有「可確證為 bool 值」的運算元才重寫：
// `x == true` → `x`、`x == false` → `! x`（`!=` 翻轉極性）。
// 單檔可確證的來源有三類：顯式 bool 標註、bool 字面量賦值、本檔定義函式的
// bool 宣告回傳型別。其它一切 —— `?T`、查不到結果型別的呼叫、裸名、
// 點號存取、運算結果 —— 都是未知，一律保留 `== true` / `== false`。
//
// v0.3.3（8a8ecb59）曾因 option 折成裸 guard 觸發 MIR "option-as-i1" crash
// 而整條 revert；2026-11-15 恢復時改成上述「正向確證」分界，不再用
// 「查不到 ?T 就當值化簡」的反向推論（那會誤傷無簽名的跨模組 option）。
//
// 這些期望值就是規格：任何一條變紅都代表規則又丟了，請修 formatter，
// 不要逕自把 expected 改成另一種寫法。

type boolCmpCase struct {
	name  string
	input string
	want  string
}

// boolCmpKnownFn 把一個條件放進「已有 bool 證據」的函式裡：b 是顯式標註、
// flag 是字面量賦值。兩者都是 formatter 在單檔內可確證的 bool 值。
func boolCmpKnownFn(cond string) string {
	return "f = () {\n    b bool = true\n    flag = false\n    " + cond + "\n}"
}

// boolCmpKnownValueCases：已知是 bool 值 → 化簡。
var boolCmpKnownValueCases = []boolCmpCase{
	{"eq_true", boolCmpKnownFn("b == true -> return"), boolCmpKnownFn("b -> return")},
	{"eq_false", boolCmpKnownFn("b == false -> return"), boolCmpKnownFn("! b -> return")},
	{"ne_true", boolCmpKnownFn("b != true -> return"), boolCmpKnownFn("! b -> return")},
	{"ne_false", boolCmpKnownFn("b != false -> return"), boolCmpKnownFn("b -> return")},
	{"mirror_true_lhs", boolCmpKnownFn("true == b -> return"), boolCmpKnownFn("b -> return")},
	{"mirror_false_lhs", boolCmpKnownFn("false == b -> return"), boolCmpKnownFn("! b -> return")},
	{"mirror_ne_lhs", boolCmpKnownFn("true != b -> return"), boolCmpKnownFn("! b -> return")},
	{"literal_assignment_eq_false", boolCmpKnownFn("flag == false -> return"), boolCmpKnownFn("! flag -> return")},
	{"shorthand_bang_bang", boolCmpKnownFn("b == !! -> return"), boolCmpKnownFn("b -> return")},
	{"shorthand_bang", boolCmpKnownFn("b == ! -> return"), boolCmpKnownFn("! b -> return")},
	{"shorthand_ne_bang_bang", boolCmpKnownFn("b != !! -> return"), boolCmpKnownFn("! b -> return")},
	{"shorthand_ne_bang", boolCmpKnownFn("b != ! -> return"), boolCmpKnownFn("b -> return")},
	{"conjunction_of_two", boolCmpKnownFn("b == true && flag == false -> return"), boolCmpKnownFn("b && ! flag -> return")},
	{"already_simplified", boolCmpKnownFn("! b -> return"), boolCmpKnownFn("! b -> return")},
	{"both_literals_untouched", "true == false", "true == false"},
	{
		// 真實案例：tests/x25519-more.no 的 same 是同檔 bytes-eq(...) 的結果，
		// 而 bytes-eq 宣告 `(eq bool)` ⇒ 本檔函式簽名可確證 same 是 bool 值。
		name: "real_world_same_local_bool_fn",
		input: `bytes-eq = (a i64, b i64) (eq bool) {
    eq = true
}
f = () {
    same = bytes-eq(a, b)
    same == true -> print('SAME')
    same == false -> print('DIFF')
}`,
		want: `bytes-eq = (a i64, b i64) (eq bool) {
    eq = true
}

f = () {
    same = bytes-eq(a, b)
    same -> print('SAME')
    ! same -> print('DIFF')
}`,
	},
}

// boolCmpUnknownCases：單檔內無 bool 證據 → 保留原比較。
// 這組是防止「查不到 ?T 就當值化簡」復辟的護欄，尤其是跨模組 option。
var boolCmpUnknownCases = []boolCmpCase{
	{"bare_name", "f == true", "f == true"},
	{"bare_name_ne", "f != false", "f != false"},
	{"dot_receiver", "a.b == false", "a.b == false"},
	{"call_result", "isReady() == true", "isReady() == true"},
	{"infix_operand", "a + b == false", "a + b == false"},
	{"grouped_operand", "(a && b) == true", "(a && b) == true"},
	{"mirror_unknown_lhs", "true == f", "true == f"},
	// 簡寫仍要正規化成 true/false，但不得順手化簡未知運算元。
	{"shorthand_normalised_only", "f == !!", "f == true"},
	{"shorthand_bang_normalised_only", "f == !", "f == false"},
	{
		// fs.write-str 回 ?bool；fmt 看不到 std 簽名 ⇒ 未知，必須保留比較。
		// 這是 v0.3.3 option-as-i1 crash 的實際觸發形狀，同時也是 CLI 的
		// fixRedundantTypeSource 移除 `ok ?bool = ...` 的冗餘標註後，
		// formatter 實際看到的輸入形狀（標註該刪，比較仍要留）。
		name: "cross_module_std_option_result",
		input: `# std/fs
f = () {
    ok = fs.write-str('/tmp/x', 'y')
    ok == true -> return
    ok == false -> return
}`,
		want: `# std/fs

f = () {
    ok = fs.write-str('/tmp/x', 'y')
    ok == true -> return
    ok == false -> return
}`,
	},
	{
		// 本檔函式回非 bool ⇒ 明確不是 bool 值，也算不可化簡。
		name: "local_fn_returning_non_bool",
		input: `count = () (n i64) {
    n = 1
}
f = () {
    n = count()
    n == true -> return
}`,
		want: `count = () (n i64) {
    n = 1
}

f = () {
    n = count()
    n == true -> return
}`,
	},
	{
		// 同名變數在別的函式裡是未知 ⇒ 單向降級，連原本可確證的那處也不化簡。
		name: "same_name_conflict_downgrades",
		input: `f = () {
    ok = true
    ok == true -> return
}
g = () {
    ok = fs.write-str('/tmp/x', 'y')
    ok == true -> return
}`,
		want: `f = () {
    ok = true
    ok == true -> return
}

g = () {
    ok = fs.write-str('/tmp/x', 'y')
    ok == true -> return
}`,
	},
}

// boolCmpOptionCases：已知是 ?T → 沿用 == true / == false。
var boolCmpOptionCases = []boolCmpCase{
	{
		name: "explicit_nullable_bool_local",
		input: `f = () {
    b ?bool = nil
    b == true -> return
    b == false -> return
}`,
		want: `f = () {
    b ?bool = nil
    b == true -> return
    b == false -> return
}`,
	},
	{
		name: "nullable_bool_parameter",
		input: `f = (b ?bool) {
    b == true -> return
    b != false -> return
}`,
		want: `f = (b ?bool) {
    b == true -> return
    b != false -> return
}`,
	},
	{
		// 算術結果恆為 ?i64（預設溢位 err），x 是 option 而非純值。
		name: "arithmetic_result_is_option",
		input: `f = () {
    x = 1 + 2
    x == false -> return
}`,
		want: `f = () {
    x = 1 + 2
    x == false -> return
}`,
	},
	{
		// 標註留在原始檔（例如直接呼叫 FormatFile、或 CLI 未判定為冗餘）：
		// `no fmt` 看不到 std 簽名，但 `?bool` 標註本身就是 option 硬證據，
		// 比較必須保留。這條與 unknown 組的 cross_module_std_option_result
		// 是一對：標註在 / 標註被移除，兩種輸入都不得化簡。
		name: "annotated_option_cross_module_call",
		input: `# std/fs
f = () {
    ok ?bool = fs.write-str('/tmp/x', 'y')
    ok == true -> return
    ok == false -> return
}`,
		want: `# std/fs

f = () {
    ok ?bool = fs.write-str('/tmp/x', 'y')
    ok == true -> return
    ok == false -> return
}`,
	},
	{
		// option 變數以 bool 字面量初始化：`?bool` 必須壓過「= true」這條
		// bool 值證據，否則會被誤化簡成裸 guard。
		name: "annotated_option_with_bool_literal_value",
		input: `f = () {
    ok ?bool = true
    ok == true -> return
    ok == false -> return
}`,
		want: `f = () {
    ok ?bool = true
    ok == true -> return
    ok == false -> return
}`,
	},
	{
		// 先宣告 option、再以 bool 字面量重新指派：option 是單向降級，
		// 後面的字面量不得把變數「升」回可化簡。
		name: "option_declared_then_literal_reassign",
		input: `f = () {
    ok ?bool = nil
    ok = true
    ok == true -> return
}`,
		want: `f = () {
    ok ?bool = nil
    ok = true
    ok == true -> return
}`,
	},
	{
		// 同名變數：一個函式裡是 bool 值、另一個裡是 option。
		// option 證據必須單向降級，兩處都不化簡。
		name: "same_name_bool_vs_option_downgrades",
		input: `f = () {
    ok = true
    ok == true -> return
}
g = () {
    ok ?bool = nil
    ok == true -> return
}`,
		want: `f = () {
    ok = true
    ok == true -> return
}

g = () {
    ok ?bool = nil
    ok == true -> return
}`,
	},
	{
		// 本地函式回傳 ?bool，且呼叫端又補了 ?bool 標註：雙重 option 證據。
		name: "annotated_local_call_returns_option",
		input: `maybe = () (v ?bool) {
    v = true
}
f = () {
    ok ?bool = maybe()
    ok == true -> return
}`,
		want: `maybe = () (v ?bool) {
    v = true
}

f = () {
    ok ?bool = maybe()
    ok == true -> return
}`,
	},
	{
		// 本地函式回傳 ?bool → 明確的 option 證據。
		name: "local_call_returns_option",
		input: `maybe = () (v ?bool) {
    v = true
}
f = () {
    b = maybe()
    b == true -> return
}`,
		want: `maybe = () (v ?bool) {
    v = true
}

f = () {
    b = maybe()
    b == true -> return
}`,
	},
}

func allBoolCmpCases() []boolCmpCase {
	all := make([]boolCmpCase, 0, len(boolCmpKnownValueCases)+len(boolCmpUnknownCases)+len(boolCmpOptionCases))
	all = append(all, boolCmpKnownValueCases...)
	all = append(all, boolCmpUnknownCases...)
	all = append(all, boolCmpOptionCases...)
	return all
}

func TestBoolComparisonKnownValueSimplification(t *testing.T) {
	for _, tt := range boolCmpKnownValueCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.input); got != tt.want {
				t.Errorf("Format(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBoolComparisonUnknownKeepsComparison(t *testing.T) {
	for _, tt := range boolCmpUnknownCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.input); got != tt.want {
				t.Errorf("Format(%q) = %q, want %q (unknown operand must keep == true / == false)", tt.input, got, tt.want)
			}
		})
	}
}

func TestBoolComparisonOptionKeepsComparison(t *testing.T) {
	for _, tt := range boolCmpOptionCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.input); got != tt.want {
				t.Errorf("Format(%q) = %q, want %q (option operand must keep == true / == false)", tt.input, got, tt.want)
			}
		})
	}
}

// option 硬閘門必須**先於** bool 值證據判定。刻意製造「同一變數同時被標為
// bool 值與 option」的矛盾狀態，option 仍須勝出；這是白盒護欄，防止日後
// 放寬 bool 證據收集或調整判定順序時，把 v0.3.3 的 option-as-i1 crash 放回來。
func TestBoolComparisonOptionGatePrecedesValueProof(t *testing.T) {
	newExpr := func() *parser.InfixExpression {
		return &parser.InfixExpression{
			Left:     &parser.Identifier{Value: "ok"},
			Operator: "==",
			Right:    &parser.BooleanLiteral{Value: true},
		}
	}
	t.Run("option_evidence_wins", func(t *testing.T) {
		f := &formatter{boolFacts: &boolComparisonFacts{
			value:   map[string]bool{"ok": true},
			option:  map[string]bool{"ok": true},
			unknown: map[string]bool{},
		}}
		if f.tryFormatBoolComparison(newExpr()) {
			t.Fatal("option operand must never be simplified, even when a bool value proof exists")
		}
	})
	t.Run("value_alone_still_simplifies", func(t *testing.T) {
		f := &formatter{boolFacts: &boolComparisonFacts{
			value:   map[string]bool{"ok": true},
			option:  map[string]bool{},
			unknown: map[string]bool{},
		}}
		if !f.tryFormatBoolComparison(newExpr()) {
			t.Fatal("control case: a proven bool value should still be simplified")
		}
		if got := f.buf.String(); got != "ok" {
			t.Errorf("simplified output = %q, want %q", got, "ok")
		}
	})
}

// 化簡結果自己必須是 formatter 的不動點，否則 `no fmt` 每跑一次就改一次。
func TestBoolComparisonIdempotent(t *testing.T) {
	for _, tt := range allBoolCmpCases() {
		t.Run(tt.name, func(t *testing.T) {
			once := Format(tt.input)
			twice := Format(once)
			if twice != once {
				t.Errorf("not idempotent: once=%q twice=%q", once, twice)
			}
			// 期望值本身也必須是不動點（規格不能自相矛盾）。
			if stable := Format(tt.want); stable != tt.want {
				t.Errorf("want %q is not a fixed point, got %q", tt.want, stable)
			}
		})
	}
}

// 輸出必須能被重新解析；`! ` 前綴與括號重綁是這條規則最常見的破口。
func TestBoolComparisonReparseClean(t *testing.T) {
	for _, tt := range allBoolCmpCases() {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.want)
			p := parser.New(l)
			p.SkipLowering = true
			p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Errorf("formatted output %q no longer parses: %v", tt.want, errs)
			}
		})
	}
}
