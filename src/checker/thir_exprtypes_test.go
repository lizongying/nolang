package checker

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// 本文件验证 6C-optional 第二步「显式 type map」的能力落地：
//  1. 表达式节点首次可答（此前 TypeOf 对表达式恒 ""）；
//  2. 宣告类节点答案不变、且不触发全树收集（生产路径零开销）；
//  3. 全语料收集不 panic，覆盖率供后续批次决策（log-only，非硬断言）。

// findFunc 在顶层语句里定位具名函数定义。
func findFunc(prog *parser.Program, name string) *parser.FunctionDefinition {
	if prog == nil {
		return nil
	}
	for _, s := range prog.Statements {
		if fn, ok := s.(*parser.FunctionDefinition); ok && fn.Name == name {
			return fn
		}
	}
	return nil
}

// findLet 在函数体内定位具名 let。
func findLet(fn *parser.FunctionDefinition, name string) *parser.LetStatement {
	if fn == nil || fn.Body == nil || fn.Name == "" {
		return nil
	}
	for _, s := range fn.Body.Statements {
		if ls, ok := s.(*parser.LetStatement); ok && ls.Name != nil && ls.Name.Value == name {
			return ls
		}
	}
	return nil
}

// TestExprTypeMapAnswersExpressions 验证 TypeOf 对表达式节点可答，且宣告节点
// 路径不受影响（零行为变化 + 零开销）。
func TestExprTypeMapAnswersExpressions(t *testing.T) {
	src := "main = (a i64) {\n\tb str = 'x'\n\tc = a + 1\n\tprint(b)\n}\n"
	prog, view := shadowBuildView(t, src)
	fn := findFunc(prog, "main")
	if fn == nil {
		t.Fatalf("main not found in:\n%s", src)
	}
	if len(fn.FuncSignature.Parameters) == 0 {
		t.Fatal("main has no parameters")
	}

	// ① 只查宣告节点：走既有前两档，显式 type map 必须**仍未构建**——
	// 这是「生产路径零开销」的硬保证（当下唯一消费方 declTypeOf 只查宣告节点）。
	if got := view.TypeOf(fn.FuncSignature.Parameters[0]); got != "i64" {
		t.Errorf("declared param TypeOf = %q, want %q", got, "i64")
	}
	if view.exprTypes != nil {
		t.Error("explicit type map was built by a declaration-only query — lazy build guard broken")
	}

	// ② 查表达式节点：首次触发全树收集，之后可答。
	letB := findLet(fn, "b")
	letC := findLet(fn, "c")
	if letB == nil || letC == nil {
		t.Fatalf("lets b/c not found in:\n%s", src)
	}
	if got := view.TypeOf(letB.Value); got != "str" {
		t.Errorf("string literal expr TypeOf = %q, want %q", got, "str")
	}
	// `a + 1` 是 InfixExpression：此前恒 ""（无 Type 字段、不在 Inferred），
	// 落地后必须非空。不断言具体值——整数算术的推断结果可能是 `?i64`
	// （option-arith 口径），那是 inferExprType 的职责而非本表。
	if got := view.TypeOf(letC.Value); got == "" {
		t.Errorf("infix expr TypeOf = %q, want non-empty (expression-level inference not in side table)", got)
	}

	// ③ 宣告节点在表构建后答案不变（第三档只含表达式键）。
	if got := view.TypeOf(fn.FuncSignature.Parameters[0]); got != "i64" {
		t.Errorf("declared param TypeOf after build = %q, want %q", got, "i64")
	}
}

// TestExprTypeMapCoverage 对影子语料全量跑一次收集，统计表达式条目数并
// 校验「表内键必为表达式节点」（不变量：宣告节点不得入表，否则会改变
// declTypeOf 的取值路径）。
func TestExprTypeMapCoverage(t *testing.T) {
	total := 0
	for _, tc := range shadowSynth {
		prog, view := shadowBuildView(t, tc.src)
		m := collectExprTypes(prog, view)
		for n := range m {
			total++
			if _, isExpr := n.(parser.Expression); !isExpr {
				t.Errorf("%s: non-expression node %T entered the explicit type map", tc.name, n)
			}
		}
	}
	if total == 0 {
		t.Fatal("explicit type map collected 0 entries across the shadow corpus — walker is vacuous")
	}
	t.Logf("explicit type map: %d expression entries across %d shadow corpus programs", total, len(shadowSynth))
}
