package parser

// value_match_call_subject_test.go — 值位 match 的主語是呼叫表達式時，
// 合成的 `let it` 必須帶上選項載荷型別。
//
// 症狀（修復前）：`v = mb.to-i64(): { ok -> it.to-f64()  -> 0.0 }` 能過 checker，
// 但 codegen 報 `EmitLLVM: unknown callee byte.to-f64`。原因是 buildMatchDesugar
// 的 matchedVarType 只在主語是 *Identifier 時推斷（函數局部表 / AST 推導兩條路
// 都要求 ident），主語是 *CallExpression 時型別恆為空 ⇒ elemType 為空 ⇒ 合成的
// `let it` 沒有 Type ⇒ 臂內 `it.<method>()` 的派發只能拿接收者殘型別猜。
//
// 修法：matchedVarType 為空且主語是 *CallExpression 時，改用
// returnTypeFromCallExpr 推返回型別（`?i64` → elemType `i64`）。
//
// 本檔釘住可觀測結果：ok 臂的 `it` 型別必須是 `i64`。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

const valueMatchCallSubjectSrc = `probe = (mb bigint) (v f64) {
    v = mb.to-i64(): {
        ok -> it.to-f64()

        -> 0.0
    }
}
`

// TestValueMatchCallSubjectInfersItType 驗證主語是方法呼叫時 `it` 有型別。
func TestValueMatchCallSubjectInfersItType(t *testing.T) {
	p := New(lexer.New(valueMatchCallSubjectSrc))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	// 裝上最小語義/簽名表：接收者型別 + 方法回傳型別。
	p.sem.SetFuncVarType("probe", "mb", "bigint")
	if p.methodSignatures == nil {
		p.methodSignatures = map[string][]string{}
	}
	p.methodSignatures["bigint.to-i64"] = []string{"?i64"}

	p.DesugarSurfaceMatchesPlain(prog)

	var found []string
	collectItTypes(prog, &found)
	// else 臂（`-> 0.0`）也會合成一個 it 綁定，型別是剔除分支的殘餘
	// （`err | nil`），那不是本測試關注的；只看 ok 臂是否拿到 i64。
	if !contains(found, "i64") {
		t.Fatalf("ok-arm `it` should be typed i64, got bindings: %v", found)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestValueMatchIdentSubjectUnchanged 對照組：主語是識別符時走原本的
// FuncVarType / AST 推導路徑，新加的呼叫分支不得改變其結果。
func TestValueMatchIdentSubjectUnchanged(t *testing.T) {
	src := `probe2 = () (v f64) {
    o ?i64 = 7
    v = o: {
        ok -> it.to-f64()

        -> 0.0
    }
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	p.DesugarSurfaceMatchesPlain(prog)

	var found []string
	collectItTypes(prog, &found)
	if !contains(found, "i64") {
		t.Fatalf("ok-arm `it` should be typed i64, got bindings: %v", found)
	}
}
