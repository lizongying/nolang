package checker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// ─────────────────────────────────────────────────────────────────────────────
// S1（match 表示统一到 SurfaceMatch）的 checker 側回歸門禁。
//
// 翻轉（2026-10-18，parser/lowering.go：ParseProgram 保留 SurfaceMatch、
// LowerSurface 不再展開 match）之後，SurfaceMatch 第一次活到校驗期。下面兩條
// 是翻轉後實測出的真實回歸（其餘 499 個語料文件行為不變）：
//
//   ① 裸型別臂（`i32 ->`）被 checkUndefinedVarsInExpr 當成值標識符
//      ⇒ "'i32' is not defined"（4bek3xc6）。tests/type-match.no 即此案。
//   ② `ok ->` 臂不登記隱式 it 綁定 ⇒ err 臂註冊的 it="err" 沿扁平 varTypes
//      洩漏到後續臂，且 err 在 LetStatement 路徑被正規化成 "str"
//      （checker.go 的 `if inferredType == "err" { inferredType = "str" }`）
//      ⇒ `ok -> v = it` 誤報 "cannot assign str value to i64 variable 'v'"
//      （15w45dqk）。tests/heap.no 即此案。
//
// 兩條都按「糖形態與降糖果形態口径一致」斷言——這正是 S1 的核心不變量
// （cf. checker/index_write_test.go:14「surface AST 與降級 AST 兩端口徑一致」），
// 也是降糖路徑既有回歸測試 match_it_arm_order_test.go 的鏡像：那份測的是
// 降糖形態下 err/nil 臂的 it 洩漏，本檔測的是糖形態下的同一類問題。
// ─────────────────────────────────────────────────────────────────────────────

// diagSet 把一个程序的校驗結果渲染成有序無關的集合鍵。
func diagSet(rs []ValidateResult) map[string]bool {
	out := make(map[string]bool, len(rs))
	for _, r := range rs {
		out[fmt.Sprintf("%d:%d|%s|%s", r.Line, r.Column, r.TraceID, r.Message)] = true
	}
	return out
}

// validateBothShapes 以糖（SkipLowering=true，SurfaceMatch 存活）與降糖兩種
// 形態解析並校驗同一份源碼，回傳兩側的診斷集合與各自的解析錯誤。
func validateBothShapes(t *testing.T, src string) (sugar, lowered map[string]bool) {
	t.Helper()
	run := func(skip bool) (map[string]bool, []string) {
		p := parser.New(lexer.New(src))
		p.SkipLowering = skip
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			return nil, errs
		}
		set := diagSet(ValidateTypes(prog))
		for k := range diagSet(ValidateUndefinedVars(prog, "")) {
			set[k] = true
		}
		return set, nil
	}
	sugar, sugarErr := run(true)
	lowered, loweredErr := run(false)
	if len(sugarErr) > 0 || len(loweredErr) > 0 {
		t.Fatalf("parse errors: sugar=%v lowered=%v", sugarErr, loweredErr)
	}
	return sugar, lowered
}

// requireShapeParity 斷言兩種形態的診斷集合完全相同。任一側為空且另一側也為
// 空時會顺带做反空洞檢查（呼叫方負責保證語料本身有診斷）。
func requireShapeParity(t *testing.T, name string, sugar, lowered map[string]bool) {
	t.Helper()
	if len(sugar) != len(lowered) {
		t.Fatalf("%s: shape mismatch — sugar has %d diagnostics, lowered has %d\nsugar:   %v\nlowered: %v",
			name, len(sugar), len(lowered), keysOf(sugar), keysOf(lowered))
	}
	for k := range sugar {
		if !lowered[k] {
			t.Fatalf("%s: diagnostic only on the sugar shape: %s", name, k)
		}
	}
	for k := range lowered {
		if !sugar[k] {
			t.Fatalf("%s: diagnostic only on the lowered shape: %s", name, k)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// okAfterSentinelArmsSrc 是回歸②的最小形：nil / err 兩條哨兵臂之後才寫
// `ok -> v = it`。err 臂會把 it 登記成 "err"（正規化後 "str"），若 ok 臂自己
// 不重新登記 it = 元素型別，v（i64）就會收到那個洩漏過來的 str。
const okAfterSentinelArmsSrc = `deref = (x ?i64) (v i64) {
    v = -999
    x: {
        nil -> v = -999

        err -> v = -999

        ok -> v = it
    }
}
a ?i64 = 5
print(deref(a))
`

// TestSugarShapeItBindingInOkArm pins regression ②.
//
// 修復：checker/sugar_walk.go 的 matchArmBindings 為
// `IsWildcard && IsDotVal && BindingName == ""` 的臂登記 it = elemTypeOf(matched)，
// 與降糖路徑 buildItBindingForArm(armType="ok") 的 typeStr=elemType 等價
// （parser/lowering.go:2998-3001、3949-3950）。
func TestSugarShapeItBindingInOkArm(t *testing.T) {
	sugar, lowered := validateBothShapes(t, okAfterSentinelArmsSrc)
	if len(sugar) != 0 {
		t.Fatalf("sugar shape must be clean, got %v", keysOf(sugar))
	}
	requireShapeParity(t, "ok-after-sentinel-arms", sugar, lowered)
}

// TestSugarShapeItIsTypedNotJustSilent proves the fix registers a *typed*
// binding rather than merely staying quiet: assigning `it` to a str variable
// must now report "cannot assign i64 value to str variable" — the same
// diagnosis the lowered shape gives. Before the fix the sugar shape reported
// "cannot assign **str** value" (the leaked err payload), which is both wrong
// and different from the lowered shape.
func TestSugarShapeItIsTypedNotJustSilent(t *testing.T) {
	src := `deref = (x ?i64) (v str) {
    v = ''
    x: {
        nil -> v = ''

        err -> v = ''

        ok -> v = it
    }
}
a ?i64 = 5
print(deref(a))
`
	sugar, lowered := validateBothShapes(t, src)
	requireShapeParity(t, "it-typed", sugar, lowered)
	// Anti-vacuity: the program must actually be rejected, and the message must
	// name i64 (the ok payload), not str (the leaked err payload).
	if len(sugar) == 0 {
		t.Fatal("expected a type-mismatch diagnostic; the program is silently accepted")
	}
	for k := range sugar {
		if !strings.Contains(k, "cannot assign i64 value to str variable") {
			t.Errorf("unexpected diagnostic: %s", k)
		}
		if strings.Contains(k, "cannot assign str value") {
			t.Errorf("the leaked err payload ('str') is still reaching the ok arm: %s", k)
		}
	}
}

// bareTypeArmSrc 是回歸①的最小形：被匹配值是 union，臂是裸型別名
// （`i32 ->` / `i64 ->`），既非帶綁定型別臂（那類有 IsTypeArm 標記）也非
// 值表達式。
const bareTypeArmSrc = `num = i32 | i64 | f64

clamp0 = (x num, lo num, hi num) (r num) {
  r = x: {
    i32 -> lo
    i64 -> hi
    -> x
  }
}
ai64 i64 = 5
print(clamp0(ai64, 0, 10))
`

// TestSugarShapeBareTypeArmNotUndefined pins regression ①.
//
// 修復：checkUndefinedVarsInExpr 的 Identifier 分支放行型別名
// （isBuiltinType / char / validationStructNames；union 別名早已由
// CollectDefinedVars 收進 definedVars）。
func TestSugarShapeBareTypeArmNotUndefined(t *testing.T) {
	sugar, lowered := validateBothShapes(t, bareTypeArmSrc)
	if len(sugar) != 0 {
		t.Fatalf("sugar shape must be clean, got %v", keysOf(sugar))
	}
	requireShapeParity(t, "bare-type-arm", sugar, lowered)
}

// TestSugarShapeUndefinedVarStillReported is the anti-vacuity guard for
// regression ①'s fix: the exemption is for *type names*, so a genuinely
// undefined value inside an arm body must still be reported, on both shapes.
func TestSugarShapeUndefinedVarStillReported(t *testing.T) {
	src := `num = i32 | i64 | f64

f = (x num) (r num) {
  r = x: {
    i32 -> zzz
    -> x
  }
}
ai64 i64 = 5
print(f(ai64))
`
	sugar, lowered := validateBothShapes(t, src)
	requireShapeParity(t, "undefined-in-arm", sugar, lowered)
	if len(sugar) == 0 {
		t.Fatal("the undefined-variable rule stopped firing inside a match arm")
	}
	found := false
	for k := range sugar {
		if strings.Contains(k, "'zzz' is not defined") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected \"'zzz' is not defined\", got %v", keysOf(sugar))
	}
}

// TestSugarShapeOkDestructureBindingUnchanged guards the boundary of the
// regression ② fix: `ok(payload) ->` must bind ONLY payload (the desugar path
// renames the implicit binding instead of emitting both, parser/lowering.go:
// 3018-3021), so the new `it` branch must not fire for it.
func TestSugarShapeOkDestructureBindingUnchanged(t *testing.T) {
	src := `deref = (x ?i64) (v i64) {
    v = -999
    x: {
        ok(payload) -> v = payload
        -> v = -1
    }
}
a ?i64 = 5
print(deref(a))
`
	sugar, lowered := validateBothShapes(t, src)
	if len(sugar) != 0 {
		t.Fatalf("sugar shape must be clean, got %v", keysOf(sugar))
	}
	requireShapeParity(t, "ok-destructure", sugar, lowered)
}
