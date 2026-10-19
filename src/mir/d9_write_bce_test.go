package mir

// d9_write_bce_test.go — D9 P3「下标写入零开销消除」的 MIR 级回归。
//
// D9 用户侧恒 option（a[i] = v 直写报 idxwrite ERROR，必须 a[i]: { ok -> ... }
// 或 a[i] ?= v）。编译器内部在「可证明越界安全」时，于 HIR→MIR 的
// lowerSafeIndex 内跳过运行时界检查（OpLen/OpGe/OpLt/OpAnd/OpCondBr 与
// idx.ok/idx.none/idx.merge 三块），直接 OpIndex + OptionWrap 写入 ?elem 槽。
//
// 写侧被读侧支配：place-match 的 ok 臂仅在「读 a[i] 已证界内」时进入，故消除
// 读检查即同步消除写检查。本档钉住这一不变量：
//
//   - 用例 A：const 下标写（a[1]）—— IR 不得含 idx.ok/idx.none/idx.merge 块，
//     也不得含该下标的 OpLt/OpGe/OpLen 界比较。
//   - 用例 B：for-range 归纳变量写（a[i]，i∈[0,3)）—— 同上消除。
//   - 用例 C：非可证下标写（a[i]，i 为函式参数）—— 同上块与界比较必须存在
//     （控制组，证明消除是「条件触发」而非「从不发射」）。
//   - 用例 D：parser AST/side-table 單測——surface 模式 parse 後斷言界內證明
//     （const 下標有 side-table 標記 / 非可證下標無標記）。
//
// 记法：idx.ok 等块名由 lowerSafeIndex 经 NewBlock 原样保留（builder.go:173），
// 故可在 MIR Module 上按名断言。MIR 入口为 main，故 A/B/C 的代码都落在 main
// （C 经 main 调用 foo(i i64)），否则未被 lowering 的代码不会出现在 MIR 里。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// hasBlockNamed 报告 MIR 模块中是否存在名为 name 的基本块（仅统计会被 codegen
// 发射的 live 函数，与 optBlockCount 同口径，避免 std/extern 干扰）。
func hasBlockNamed(m *Module, name string) bool {
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			if blk.Name == name {
				return true
			}
		}
	}
	return false
}

// mirCountOp 统计模块中某 opcode 的出现次数（仅 live 函数，与 optOpCounts 同口径）。
func mirCountOp(m *Module, op Op) int {
	n := 0
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				if inst := m.Inst(iid); inst != nil && inst.Op == op {
					n++
				}
			}
		}
	}
	return n
}

// TestD9ConstIndexWriteHasNoBoundsCheck 用例 A：const 下标写必须消除运行时界检查。
func TestD9ConstIndexWriteHasNoBoundsCheck(t *testing.T) {
	src := `main = () () {
    a [3]i64 = [0, 0, 0]
    a[1]: {
        ok -> it = 7
        -> _
    }
}`
	m := lowerSrc(t, src)
	if hasBlockNamed(m, "idx.ok") || hasBlockNamed(m, "idx.none") || hasBlockNamed(m, "idx.merge") {
		t.Errorf("const-subscript write still emitted bounds-check blocks (idx.ok/idx.none/idx.merge); in-bounds fast path in lowerSafeIndex did not fire")
	}
	if c := mirCountOp(m, OpLen); c > 0 {
		t.Errorf("const-subscript write still emitted OpLen (len read) %d times; expected 0", c)
	}
	if c := mirCountOp(m, OpLt); c > 0 {
		t.Errorf("const-subscript write still emitted OpLt %d times; expected 0", c)
	}
	if c := mirCountOp(m, OpGe); c > 0 {
		t.Errorf("const-subscript write still emitted OpGe %d times; expected 0", c)
	}
}

// TestD9ForRangeIndexWriteHasNoBoundsCheck 用例 B：for-range 归纳变量写必须消除。
func TestD9ForRangeIndexWriteHasNoBoundsCheck(t *testing.T) {
	src := `main = () () {
    a [3]i64 = [0, 0, 0]
    for i <- [0..3) {
        a[i]: {
            ok -> it = i
            -> _
        }
    }
}`
	m := lowerSrc(t, src)
	if hasBlockNamed(m, "idx.ok") || hasBlockNamed(m, "idx.none") || hasBlockNamed(m, "idx.merge") {
		t.Errorf("for-range induction-var write still emitted bounds-check blocks; in-bounds proof (markFor) did not reach lowerSafeIndex")
	}
	// OpLen / OpGe 只来自下标界检查（for 回圈本身用 OpLt 比较 i<3，不读阵列长、
	// 不用 >=），故可证界内时应均归零。OpLt 不在此断言——回圈条件 i<3 合法占用。
	if c := mirCountOp(m, OpLen); c > 0 {
		t.Errorf("for-range induction-var write still emitted OpLen %d times; expected 0", c)
	}
	if c := mirCountOp(m, OpGe); c > 0 {
		t.Errorf("for-range induction-var write still emitted OpGe %d times; expected 0", c)
	}
}

// TestD9NonProvableIndexWriteKeepsBoundsCheck 用例 C：非可证下标写（函式参数 i）
// 必须保留界检查（控制组）。证明消除是「可证界内时触发」，而非「永不发射」。
func TestD9NonProvableIndexWriteKeepsBoundsCheck(t *testing.T) {
	src := `foo = (i i64) () {
    a [3]i64 = [0, 0, 0]
    a[i]: {
        ok -> it = 7
        -> _
    }
}
main = () {
    foo(5)
}`
	m := lowerSrc(t, src)
	if !hasBlockNamed(m, "idx.ok") || !hasBlockNamed(m, "idx.none") || !hasBlockNamed(m, "idx.merge") {
		t.Errorf("non-provable-subscript write omitted bounds-check blocks; safe-index lowering should keep the runtime check when in-bounds cannot be proven")
	}
	if c := mirCountOp(m, OpLen); c == 0 {
		t.Errorf("non-provable-subscript write emitted no OpLen; expected the bounds check to read the array length")
	}
	if c := mirCountOp(m, OpLt); c == 0 {
		t.Errorf("non-provable-subscript write emitted no OpLt; expected the upper-bound comparison")
	}
}

// collectIndexExprs 走查 AST 收集所有 IndexExpression 节点（语句/表达式两闭包
// 相互递归，因 Statement 与 Expression 接口不相交）。surface 模式下 place-match
// 是 *SurfaceMatch（Matched 即 a[1]）；完整 lowering 后变成 *IfExpression（MatchedExpr）。
func collectIndexExprs(stmts []parser.Statement) []*parser.IndexExpression {
	var out []*parser.IndexExpression
	var walkExpr func(e parser.Expression)
	var walkStmt func(s parser.Statement)
	walkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		switch v := s.(type) {
		case *parser.LetStatement:
			walkExpr(v.Value)
		case *parser.ExpressionStatement:
			walkExpr(v.Expression)
		case *parser.BlockStatement:
			for _, c := range v.Statements {
				walkStmt(c)
			}
		case *parser.FunctionDefinition:
			if v.Body != nil {
				for _, c := range v.Body.Statements {
					walkStmt(c)
				}
			}
		}
	}
	walkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *parser.IndexExpression:
			out = append(out, v)
			walkExpr(v.Left)
			walkExpr(v.Index)
		case *parser.AssignExpression:
			walkExpr(v.Value)
		case *parser.IfExpression:
			walkExpr(v.Condition)
			if v.Consequence != nil {
				for _, c := range v.Consequence.Statements {
					walkStmt(c)
				}
			}
			if v.Alternative != nil {
				for _, c := range v.Alternative.Statements {
					walkStmt(c)
				}
			}
		case *parser.SurfaceMatch:
			// 仅走查被匹配表达式（a[1]）；arm 体内可能还有下标，但本档只断言
			// 「匹配下标」自身的 InBounds 旗标，无需下钻。（MatchArm 的字段自
			// 2026-10-16 起已导出，下钻在技术上可行，只是本档不需要。）
			walkExpr(v.Matched)
		case *parser.InfixExpression:
			walkExpr(v.Left)
			walkExpr(v.Right)
		}
	}
	for _, s := range stmts {
		walkStmt(s)
	}
	return out
}

// indexLeftName 返回下标基底标识符名（非标识符时返回空）。
func indexLeftName(idx *parser.IndexExpression) string {
	if id, ok := idx.Left.(*parser.Identifier); ok {
		return id.Value
	}
	return ""
}

// TestD9IndexInBoundsFlag 用例 D：surface AST 必須保留原始語法節點，bounds 證明
// 則存在 Program.Sem side-table；非可證（函式參數下標）不得帶該語義標記。
func TestD9IndexInBoundsFlag(t *testing.T) {
	// surface 模式保留 place-match 原始節點，便於按名找到 a[1]/a[i]；解析期
	// bounds analysis 已將證明結果寫入 Program.Sem，而不會修改 IndexExpression。
	parseSurface := func(src string) *parser.Program {
		p := parser.New(lexer.New(src))
		// NOTE: this used to set SkipUnwrapLowering + SkipSafeIndexLowering too,
		// but those flags were deleted with §4.3 (see parser/lowering.go:337). The
		// replacement edit left `SkipLowering = true` written twice; collapsed to
		// one. Whatever used to be "surface" is now just SkipLowering.
		p.SkipLowering = true
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("surface parse errors: %v", errs)
		}
		return prog
	}

	// const 下标（a[1]，阵列长 3）必为可证界内。
	constSrc := `foo = () () {
    a [3]i64 = [0, 0, 0]
    a[1]: {
        ok -> it = 7
        -> _
    }
}`
	prog := parseSurface(constSrc)
	foundConst := false
	for _, idx := range collectIndexExprs(prog.Statements) {
		if indexLeftName(idx) != "a" {
			continue
		}
		if lit, ok := idx.Index.(*parser.IntegerLiteral); ok && lit.Value == 1 {
			foundConst = true
			if !prog.Sem.IndexIsInBounds(idx) {
				t.Errorf("const in-bounds subscript a[1] missing from Program.Sem side-table")
			}
		}
	}
	if !foundConst {
		t.Fatalf("could not locate the const-subscript a[1] IndexExpression in the surface AST")
	}

	// 非可證下標（a[i]，i 為函式參數）不得進入 side-table。
	paramSrc := `foo = (i i64) () {
    a [3]i64 = [0, 0, 0]
    a[i]: {
        ok -> it = 7
        -> _
    }
}`
	prog2 := parseSurface(paramSrc)
	foundParam := false
	for _, idx := range collectIndexExprs(prog2.Statements) {
		if indexLeftName(idx) != "a" {
			continue
		}
		if _, ok := idx.Index.(*parser.Identifier); ok {
			foundParam = true
			if prog2.Sem.IndexIsInBounds(idx) {
				t.Errorf("non-provable subscript a[i] (param index) wrongly marked in Program.Sem")
			}
		}
	}
	if !foundParam {
		t.Fatalf("could not locate the param-subscript a[i] IndexExpression in the surface AST")
	}
}
