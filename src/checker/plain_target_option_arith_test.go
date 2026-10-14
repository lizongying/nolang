package checker

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// 本檔案是 D1 嚴格模式（2026-10-14）的回歸釘：**寫在收到算術那一行上**的 plain
// 整數標註不是「把 option 拉回 plain」的開關，而是把 err 分支靜默丟掉的隱含解包，
// 因此必須是 ERROR。
//
// 使用者原始需求（nouv/src/settings.no）：
//
//	k i64 = i + 1 #{overflow=wrap}   ; ← 錯誤（本檔的拒絕清單）
//	k = i + 1 #{overflow=wrap}       ; ← 唯一合法形狀，k 是 ?i64，接著 match 解包
//
// 判據：`#{overflow=...}` 註解只改變「失敗時怎麼取值」，不改變結果型別 ——
// 見 parser/lowering.go inferOptionArith 的 D1 段。因此帶不帶註解，右值都是 `?T`。
//
// 🔴 邊界（與 lowering 逐字鏡像，改動前必讀 upgradeOptionArithTargetType 的註解）：
// 明文標註只有寫在「本行」才算靜默解包。plain 宣告在別的陳述／簽名上時，
// captureTargetAllowsOption 會以「已存在變數不改型別」否決 option 合成，得到的是
// 自洽的 plain（t58 `hold`／`pna`、blake2_mask 的 `i = 0; i = i + 1`）——
// 那些形狀**合法**，在本檔的 legal 清單裡釘死，不許回歸成誤報。
//
// 實作在 checker.go：upgradeOptionArithTargetType 把推斷型別升格為 `?T`，
// 交由既有的 15w45dqk（let）判定開火。AssignExpression 路徑刻意不接
// （見 TestAssignExpressionIdentifierLHSIsUnreachable）。

// TestPlainTargetRejectsOptionArith 只有一種形狀必須是 ERROR：**本行**帶著使用者
// 親手寫下的 plain 整數標註。帶不帶 `#{overflow=wrap}` 等價（註解只改「失敗時怎麼
// 取值」，不改結果型別）。
func TestPlainTargetRejectsOptionArith(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "annotated_let",
			src: `f = (i i64) (r i64) {
    k i64 = i + 1
    r = k
}`,
		},
		{
			name: "wrap_annotated_let",
			src: `f = (i i64) (r i64) {
    #{overflow=wrap}
    k i64 = i + 1
    r = k
}`,
		},
		{
			name: "wrap_annotated_let_same_line",
			src: `f = (i i64) (r i64) {
    k i64 = i + 1 #{overflow=wrap}
    r = k
}`,
		},
		{
			name: "annotation_after_inferred_binding",
			// 先無標註推斷出 plain，之後又用明文標註重宣告 —— 「plain」是這一行
			// 自己寫的，靜默解包同樣發生在這一行。
			src: `f = (a i64, b i64) (r i64) {
    k = 0
    k i64 = a + b
    r = k
}`,
		},
		{
			name: "redeclare_option_var_as_plain",
			// 已存在變數被重宣告成 plain 並收到 option 算術。
			src: `f = (a i64, b i64) (r i64) {
    k ?i64 = a + b
    r = 0
    k: {
        ok -> {
            r = it
        }
        -> {
            r = -1
        }
    }
    k i64 = a - b
    r = k
}`,
		},
		{
			name: "nested_arith",
			src: `f = (a i64, b i64, c i64) (r i64) {
    k i64 = a + b * c
    r = k
}`,
		},
		{
			name: "modulo_root",
			src: `f = (a i64, b i64) (r i64) {
    k i64 = a % b
    r = k
}`,
		},
		{
			name: "grouped_root",
			src: `f = (a i64, b i64) (r i64) {
    k i64 = (a + b)
    r = k
}`,
		},
	}
	// 判據 trace 是 15w45dqk（let 的型別比對）。AssignExpression 不需要：
	// parser/expr.go 只為 `a.b = v` / `a[i] = v` 產生 AssignExpression，裸識別符
	// `x = v` 一律解析成 LetStatement（見 checkAssignExpressionIdentifierLHS）。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n == 0 {
				t.Fatalf("explicit plain annotation on option arithmetic must be rejected, got 0; all: %v", results)
			}
		})
	}
}

// TestAssignExpressionIdentifierLHSIsUnreachable 釘住上面只用 15w45dqk 的理由：
// ExpressionStatement 的 AssignExpression 左側永遠不是裸識別符（那種寫法在
// parser 就變成 LetStatement），所以 AssignExpression 路徑不可能收到
// `plain-target ← option 算術`，升格規則不該放在那邊。
func TestAssignExpressionIdentifierLHSIsUnreachable(t *testing.T) {
	prog := parseProg(t, `f = (a i64, b i64) {
    m = a + b
    m = a - b
}`)
	var assignStmts, identLHS int
	for _, st := range prog.Statements {
		if fd, ok := st.(*parser.FunctionDefinition); ok {
			for _, bs := range fd.Body.Statements {
				if es, ok := bs.(*parser.ExpressionStatement); ok {
					if ae, ok := es.Expression.(*parser.AssignExpression); ok {
						assignStmts++
						if _, ok := ae.Left.(*parser.Identifier); ok {
							identLHS++
						}
					}
				}
			}
		}
	}
	if assignStmts != 0 {
		t.Fatalf("bare `m = ...` must parse as LetStatement, got %d AssignExpression(s)", assignStmts)
	}
	_ = identLHS
}

// TestPlainTargetOptionArithLegalShapes 是反向守衛：合法的形狀不得被誤報。
// 每一條都實測可編譯可執行（見 tmp/zz-cx/r2.no、p20.no、okarm.no）。
func TestPlainTargetOptionArithLegalShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 未標註綁定：lowering 合成 ?T，two-step match 解包。
			name: "two_step_match_on_identifier",
			src: `f = (a i64, b i64) (r i64) {
    m = a + b
    t = m: {
        ok -> it
        -> -1
    }
    r = t
}`,
		},
		{
			// match 語句形狀。
			name: "match_statement_form",
			src: `f = (a i64, b i64) (r i64) {
    m = a + b
    r = 0
    m: {
        ok -> {
            r = it
        }
        -> {
            r = -1
        }
    }
}`,
		},
		{
			// wrap 語義經 ok 臂取出（真溢位時仍是 ok，payload 已回繞）。
			name: "wrap_reaches_ok_arm",
			src: `f = (i i64) (r i64) {
    k = i + 1 #{overflow=wrap}
    v = k: {
        ok -> it
        -> -1
    }
    r = v
}`,
		},
		{
			// ?= 上拋到 option 結果參數。
			name: "unwrap_assign_upcast",
			src: `f = (a i64, b i64) (r ?i64) {
    m = a + b
    r ?= m
}`,
		},
		{
			// 目標已宣告 ?T：相容比對放行，選項內容由 match 檢查。
			name: "option_target_declared",
			src: `f = (a i64, b i64) (r i64) {
    k ?i64 = a + b
    r = 0
    k: {
        ok -> {
            r = it
        }
        -> {
            r = -1
        }
    }
}`,
		},
		{
			// 純量出參：plain 宣告在簽名上，本行無標註。lowering 的
			// captureTargetAllowsOption 以「已存在變數不改型別」否決 option 合成，
			// 是自洽的 plain；由 ovf-int-default / ovfhndld HINT 提示。
			// 回歸依據：src/build/lib_export_sem_test.go 的 bad.no 反向斷言
			// （只看「有沒有報」，不看 severity）。
			name: "result_param_plain_target",
			src: `f = (a i64, b i64) (r i64) {
    r = a + b
}`,
		},
		{
			name: "result_param_plain_target_wrap",
			src: `f = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    r = a + b
}`,
		},
		{
			// 先明文宣告 plain、之後無標註重指派。合成被「已存在變數不改型別」
			// 否決，靜默解包不發生在本行的任何標註上。
			// 回歸依據：compiler/tests/fixtures/t58-d6-arith-option.no 的 `hold`。
			name: "reassign_into_plain_declared_local",
			src: `f = (a i64, b i64) (r i64) {
    x i64 = 0
    x = a + b
    r = x
}`,
		},
		{
			// 先無標註（推斷出 plain）、之後無標註重指派。t58 的 `pna` / `fnb` 形狀，
			// 也是 corpus 裡 `i = 0` + `i = i + 1 #{overflow=wrap}` 迴圈的真實形狀
			// （回歸依據：src/checker/blake2_mask_test.go、src/build/lib_export_sem_test.go）。
			name: "reassign_into_inferred_plain_local",
			src: `f = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    x = 0
    x = a + b
    r = x
}`,
		},
		{
			// 無號運算元不產生 option（lowering 的 signedIntOperandType 只收有號）。
			name: "unsigned_operands_plain_target",
			src: `f = (a u8, b u8) (r i64) {
    m = a + b
    r = m
}`,
		},
		{
			// 字串拼接：`-` 不是整數算術。
			name: "string_concat",
			src: `f = (a str, b str) (r str) {
    r = a - b
}`,
		},
		{
			// 整數字面量不需要解包。
			name: "plain_literal",
			src: `f = () (r i64) {
    k i64 = 5
    r = k
}`,
		},
		{
			// 顯式捨棄。
			name: "explicit_discard",
			src: `f = (a i64, b i64) (r i64) {
    _ = a + b
    r = 1
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk") + countTrace(results, "1mf3x79l"); n != 0 {
				t.Fatalf("legal shape must not be rejected, got %d diagnostic(s): %v", n, results)
			}
		})
	}
}

// TestPlainTargetOperandTypeMustBeStaticallyKnown 守住「向寬鬆方向失敗」：
// 解析不出型別的運算元（跨模組呼叫、未知識別字）不得讓本規則開火 ——
// 誤報一條明文型別的最壞結果是讓原本能編譯的程式失敗。
func TestPlainTargetOperandTypeMustBeStaticallyKnown(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 右運算元型別未知（未宣告的跨模組函式）。
			name: "unknown_callee_operand",
			src: `f = (a i64) (r i64) {
    k i64 = a + unknown-fn(a)
    r = k
}`,
		},
		{
			// 左運算元型別未知。
			name: "unknown_left_operand",
			src: `f = (r i64) {
    k i64 = unknown-var + 1
    r = k
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n != 0 {
				t.Fatalf("operand type must be statically known before rejecting, got %d: %v", n, results)
			}
		})
	}
}

// TestOptionUnwrapRecipeRecommendsParseableForm 釘住建議文字本身可用：
// 舊文案推薦的 `k = expr: { ok -> it, -> 0 }` 有兩種實測不成立的形態
// （逗號臂是解析錯誤；同行寫法在 expr 非裸識別字時 match 會脫離）。
func TestOptionUnwrapRecipeRecommendsParseableForm(t *testing.T) {
	msg := optionUnwrapRecipe("k")
	for _, bad := range []string{"ok -> it, ->", " = expr: {"} {
		if contains(msg, bad) {
			t.Fatalf("recipe still recommends the broken spelling %q: %s", bad, msg)
		}
	}
	for _, good := range []string{"两段式", "换行", "?="} {
		if !contains(msg, good) {
			t.Fatalf("recipe must mention %q: %s", good, msg)
		}
	}
	if h := optionUnwrapAssignHint("?i64"); contains(h, "用显式型别") {
		t.Fatalf("hint must not recommend the plain-type coercion idiom: %s", h)
	}
}

// contains reports whether s contains sub (local helper, no strings import).
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
