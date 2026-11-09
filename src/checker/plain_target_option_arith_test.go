package checker

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// 本檔案是 D1 嚴格模式（2026-10-14；2026-11-08 收緊）的回歸釘：把 option 值
// 直接指派給 **plain 變數**（不論標註寫在哪一行）都是把 err 分支靜默丟掉的隱含
// 解包，因此必須是 ERROR。
//
// 使用者原始需求（src/std/json.no 的 `p = p + 1`）：
//
//	k i64 = i + 1 #{overflow=wrap}   ; ← 錯誤（本行明文標註）
//	p = 0; p = p + 1                ; ← 錯誤（標註在前一行／結果參數）
//	k = i + 1 #{overflow=wrap}       ; ← 唯一合法形狀，k 是 ?i64，接著 match 解包
//
// 判據：`#{overflow=...}` 註解只改變「失敗時怎麼取值」，不改變結果型別 ——
// 見 parser/lowering.go inferOptionArith 的 D1 段。因此帶不帶註解，右值都是 `?T`。
//
// 🔴 2026-11-08 收緊：過去只有「標註寫在本行」被拒絕；「先 plain 宣告、之後重
// 指派」（t58 `hold`／`pna`、blake2_mask 的 `i = 0; i = i + 1`）與結果參數重綁定
// 靠 lowering 的 captureTargetAllowsOption 否決 option 合成，被當成自洽的 plain
// 放行。那是實作品面的妥協，不是語言語意，使用者裁定撤銷：如今全部是硬錯誤，
// 在 TestPlainTargetRejectsOptionArithReassignment 的拒絕清單裡釘死。
//
// 實作在 checker.go：upgradeOptionArithTargetType（本行標註）與 validateStmtTypes
// 後段（已存在 plain 變數）把推斷型別升格為 `?T`，交由既有的 15w45dqk（let）
// 判定開火。AssignExpression 路徑刻意不接
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

// TestPlainTargetRejectsOptionArithReassignment 是 2026-11-08 收緊的回歸釘：
// **已存在的 plain 變數**（含結果參數）收到 option 算術／option 呼叫同樣是隱含
// 解包，必須是 ERROR。標註寫在前一行或函式簽名上都不構成豁免。
//
// 首例即使用者回報的 src/std/json.no 真實形狀：
//
//	#{overflow=wrap}
//	p = p + 1
//
// 過去 inferExprType 對它只回 "i64"（看不到 overflow option），所以整條型別比對
// 被跳過；現在 checker 把右值本質升格為 `?i64`，讓既有 15w45dqk 開火。
func TestPlainTargetRejectsOptionArithReassignment(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "json_no_wrap_counter",
			src: `f = (r i64) {
    p = 0
    #{overflow=wrap}
    p = p + 1
    r = p
}`,
		},
		{
			name: "reassign_into_plain_declared_local",
			src: `f = (a i64, b i64) (r i64) {
    x i64 = 0
    x = a + b
    r = x
}`,
		},
		{
			name: "reassign_into_inferred_plain_local",
			src: `f = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    x = 0
    x = a + b
    r = x
}`,
		},
		{
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
			name: "nested_arith_reassign",
			src: `f = (a i64, b i64) (r i64) {
    x = 0
    x = a + b * 2
    r = x
}`,
		},
		{
			// 呼叫版：已存在的 plain 變數收到 option-returning 模組函式。
			name: "option_call_reassign_into_plain_local",
			src: `f = (i i64) (r i64) {
    x str = ''
    x = os.arg(i + 1)
    r = 1
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n != 1 {
				t.Fatalf("existing plain target must be rejected exactly once, got %d diagnostic(s): %v", n, results)
			}
		})
	}
}

// TestPlainTargetRejectsImplicitOptionBindingToPlain 釘住「option 值直接指派給
// plain 變數」的第二種來源：右值不是算術，而是**先前無標註綁定的 option 變數**
// （`k = a + 1` 在 lowering 合成 `?i64`）。舊實作在首次綁定時把 varTypes 記成
// inferExprType 看到的 `i64`，於是後續 `r = k` 被當成 plain→plain 放行 ——
// 正是使用者點名的 `zn = zn-opt2`：把一個 option 直接塞進 i64，tag 被靜默丟掉。
//
// 判別力：撤掉 validateStmtTypes 首次綁定分支的 `?T` 升格，本測試立刻失敗。
//
// 邊界：option-returning call 的首次綁定（`k = os.arg(i + 1)`）由
// optionResultOfCallTarget 覆蓋，但 ValidateTypes 單測裡 StdSigKey 為空、
// stdFuncOptionResult 會靜默回 ""，故此處只釘算術路徑；CLI 實測該形狀
// 同樣報 15w45dqk。
func TestPlainTargetRejectsImplicitOptionBindingToPlain(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "option_binding_then_plain_result_param",
			src: `f = (a i64) (r i64) {
    k = a + 1
    r = k
}`,
		},
		{
			name: "option_binding_then_plain_local",
			src: `f = (a i64) (r i64) {
    k = a + 1
    v i64 = 0
    v = k
    r = v
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n != 1 {
				t.Fatalf("assigning an option value to a plain variable must be rejected exactly once, got %d diagnostic(s): %v", n, results)
			}
		})
	}
}

// TestPlainTargetRejectsFieldLHS 是 2026-11-09 使用者點名補上的最後一塊
// 邊界：`b.n = b.n + 1`。欄位 LHS 走 ExpressionStatement 的
// AssignExpression，過去 validateStmtTypes 只對 Identifier LHS 做型別比對，
// 整條路徑完全沒有 option→plain 判定 ⇒ `no vet` 回 "No issues found"。
//
// 兩處實作共同構成這條規則：validateStmtTypes 的 AssignExpression 非 Identifier
// 分支（目標型別 + valType 升格 + 15w45dqk），以及 ovfOptionArithOperandType
// 的 *DotExpression 分支（否則欄位運算元讓右值推不出 ?T）。
// 判別力：撤掉任一處，第一個 case 立刻回到 0 diagnostics。
func TestPlainTargetRejectsFieldLHS(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "field_counter_wrap",
			src: `box {
    n i64
}
f = (a i64) (r i64) {
    b = box {}
    b.n = 0
    #{overflow=wrap}
    b.n = b.n + 1
    r = b.n
}`,
			want: 1,
		},
		{
			name: "field_option_value",
			src: `box {
    n i64
}
f = (a i64) (r i64) {
    b = box {}
    k = a + 1
    b.n = k
    r = b.n
}`,
			want: 1,
		},
		{
			// 反向守衛：一步式值位 match 是合法修法，不得被誤報。
			name: "field_one_step_value_match",
			src: `box {
    n i64
}
f = (a i64) (r i64) {
    b = box {}
    b.n = 0
    #{overflow=wrap}
    b.n = b.n + 1: {
        ok -> it
        ->
    }
    r = b.n
}`,
			want: 0,
		},
		{
			// 反向守衛：目標本來就是 ?T，option 拷貝不是隱含解包。
			name: "option_typed_field",
			src: `box {
    n ?i64
}
f = (a i64) (r i64) {
    b = box {}
    b.n = a + 1
    r = 1
}`,
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n != c.want {
				t.Fatalf("field LHS option→plain gate: want %d diagnostic(s), got %d: %v", c.want, n, results)
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
			// 一步式值位 match：option 算術直接當 subject，不需要中繼 option
			// 變數、也不需要 `?i64` 標註（`zn + 1` 本身就是 `?i64`）。
			// 使用者指定的形狀，src/std/archive/bzip2.no 全檔採此寫法。
			name: "one_step_expr_subject_value_match",
			src: `f = (a i64) (r i64) {
    zn = 0
    zn = zn + 1: {
        ok -> it
        ->
    }
    r = zn
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
			// 目標本來就是 option：重指派 option 算術不是隱含解包，必須放行。
			name: "reassign_into_option_declared_local",
			src: `f = (a i64, b i64) (r i64) {
    x ?i64 = ok(0)
    x = a + b
    r = 0
    x: {
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
			// 新綁定（不在 varTypes）：lowering 就地合成 ?T，合法。
			name: "new_binding_of_option_arith",
			src: `f = (a i64, b i64) (r i64) {
    x = a + b
    r = 0
    x: {
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
			// 純字面算術是 comptime 常數（`0 - 1` 會折成 -1），沒有 option tag 可丟。
			// 誤報會讓 --fix=unwrap 造出 `x ?= 0 - 1`，撞上 5l952ztl。
			name: "pure_literal_arith_reassign",
			src: `f = () (r i64) {
    exp-sign = 1
    exp-sign = 0 - 1
    r = exp-sign
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
//
// 斷言的是英文原文（en 是唯一事實來源，各 locale 由 catalog 翻譯），因此
// 這裡的關鍵詞與 optionUnwrapRecipe 的 i18n.T 預設文案同進同退。
func TestOptionUnwrapRecipeRecommendsParseableForm(t *testing.T) {
	msg := optionUnwrapRecipe("k")
	for _, bad := range []string{"ok -> it, ->", " = expr: {"} {
		if contains(msg, bad) {
			t.Fatalf("recipe still recommends the broken spelling %q: %s", bad, msg)
		}
	}
	for _, good := range []string{"two-step", "separate lines", "?="} {
		if !contains(msg, good) {
			t.Fatalf("recipe must mention %q: %s", good, msg)
		}
	}
	h := optionUnwrapAssignHint("?i64")
	// 建議必須點名具體的 option 型別（否則使用者不知道要解包成什麼）。
	if !contains(h, "?i64") {
		t.Fatalf("hint must name the concrete option type: %s", h)
	}
	// 不得再推薦「用显式型别把結果拉回 plain」——那正是 D1 禁止的隱含解包。
	if contains(h, "explicit type") {
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
