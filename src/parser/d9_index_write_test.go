package parser

// d9_index_write_test.go — D9「下标写入必须显式解包」的 parser 侧单测。
//
// 用户侧恒 option（无任何豁免，D7 两层分离延伸到写入侧），编译器内部只在
// 可证明越界安全时消除检查。本档验证两种用户写法的 lowering 形态：
//
//  1. place-match 解包（多行；match 臂以换行分隔）：a[i]: { ok -> it = v / -> _ }
//     写入目标（ok 臂的 `it`）绑定的是元素「位置」，可读可写；parser 把它
//     降糖为「求值一次下标 → 写回元素 → 重绑 it」的 place-write 序列。
//
//  2. 错误上抛：        a[i] ?= v
//     `?=` 的 option 性来自「下标读取」（a[i] 越界回传 nil），不是右侧值 v；
//     parser 把它降糖成安全索引 match：nil/err 臂向上传播，ok 臂经 place-write
//     写回元素。
//
// 关键不变量（D9 与 no fmt / checker / LSP 互操作的基础）：纯 surface 场景
// （SkipSafeIndexLowering=true）不得触发 place-write 合成写回——formatter 只
// 能重建 __match_subj 包装，无法重建 __idx/__place 合成节点，故必须保留解包
// 语法的原始 surface 形态。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseCompilePath 解析并完整 lowering（两个 Skip 旗标都 false），供验证 D9
// 写入侧真正的 codegen 形态（place-write 合成写回序列）。
func parseCompilePath(t *testing.T, src string) (*Parser, *Program) {
	t.Helper()
	p := New(lexer.New(src))

	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	return p, prog
}

// parseSurfaceMode 解析但保留 surface（两个 Skip 旗标都 true），供验证
// formatter / checker / LSP 看到的仍是解包语法的原始节点。
func parseSurfaceMode(t *testing.T, src string) (*Parser, *Program) {
	t.Helper()
	p := New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return p, prog
}

// hasSyntheticIndexStore 走查 AST：是否存在「写入元素位置」的合成
// AssignExpression（IsSynthetic=true，Left 为 IndexExpression，base 与 index
// 都已是标识符）。这是 place-write 改写发生过的可观测证据——formatter 无法重建
// 此节点，故纯 surface 场景必须没有它。
//
// 注意 Statement 与 Expression 是两个不相交的接口（Statement 要求
// statementNode 方法，*AssignExpression / *IfExpression 都只实现 expressionNode），
// 故走查必须分成语句闭包与表达式闭包相互递归，不能塞进同一个 type switch。
func hasSyntheticIndexStore(stmts []Statement) bool {
	found := false
	var walkExpr func(e Expression)
	var walkStmt func(s Statement)

	walkStmt = func(s Statement) {
		if s == nil || found {
			return
		}
		switch v := s.(type) {
		case *LetStatement:
			walkExpr(v.Value)
		case *ExpressionStatement:
			walkExpr(v.Expression)
		case *BlockStatement:
			for _, c := range v.Statements {
				walkStmt(c)
			}
		case *FunctionDefinition:
			if v.Body != nil {
				for _, c := range v.Body.Statements {
					walkStmt(c)
				}
			}
		}
	}

	walkExpr = func(e Expression) {
		if e == nil || found {
			return
		}
		switch v := e.(type) {
		case *AssignExpression:
			// place-write 的合成写回：`a[__idx_L_C] = __place_L_C`。
			if v.IsSynthetic {
				if idx, ok := v.Left.(*IndexExpression); ok {
					if _, ok := idx.Left.(*Identifier); ok {
						if _, ok := idx.Index.(*Identifier); ok {
							found = true
							return
						}
					}
				}
			}
			walkExpr(v.Value)
		case *IfExpression:
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
		case *IndexExpression:
			walkExpr(v.Index)
		case *InfixExpression:
			walkExpr(v.Left)
			walkExpr(v.Right)
		}
	}

	for _, s := range stmts {
		walkStmt(s)
	}
	return found
}

// TestD9UnwrapAssignToIndexLowersWithPlaceWrite 写入侧 `?=` 的 option 性来自
// 下标读取：a[i] ?= v 在编译场景（两 Skip 旗标 false）必须降糖成含 nil/err
// 传播臂 + 通配 ok 臂的安全索引 match，且 ok 臂触发 place-write 合成写回。
func TestD9UnwrapAssignToIndexLowersWithPlaceWrite(t *testing.T) {
	src := "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1] ?= 42\n}"
	p, prog := parseCompilePath(t, src)
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("compile-path parse errors (D9 ?= write should lower cleanly): %v", errs)
	}
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("statement 0 is %T, want *FunctionDefinition", prog.Statements[0])
	}
	// `a[1] ?= 42` 是函式体第 3 条陈述（0=函式头, 1=result=nil, 2=a[3]i64=...）。
	es, ok := fd.Body.Statements[2].(*ExpressionStatement)
	if !ok {
		t.Fatalf("lowered `?=` is %T, want *ExpressionStatement (match wrapper)", fd.Body.Statements[2])
	}
	if _, ok := es.Expression.(*IfExpression); !ok {
		t.Fatalf("lowered `?=` Expression is %T, want *IfExpression", es.Expression)
	}
	// place-write 合成写回必须存在（核心 D9 行为：越界检查与写回共用一次下标求值）。
	if !hasSyntheticIndexStore(prog.Statements) {
		t.Errorf("D9 `?=` write did NOT produce the synthetic element store (`a[__idx] = __place`) — place-write rewrite missing")
	}
}

// TestD9UnwrapAssignToIndexInPlainFunctionIsError 非 option 结果函数内
// `a[i] ?= v` 无法上抛越界错误，必须报既有口径错误，且不得静默降糖成别的东西。
func TestD9UnwrapAssignToIndexInPlainFunctionIsError(t *testing.T) {
	src := "foo = (x i64) () {\n    a [3]i64 = [1, 2, 3]\n    a[1] ?= x\n    io.outln(a[1])\n}"
	p := New(lexer.New(src))

	p.ParseProgram()
	errs := p.Errors()
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 parse error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "`?=` can only be used inside a function with an option-typed result param") {
		t.Errorf("error does not explain the option-result requirement: %q", errs[0])
	}
	// 位置锚在 `?=` 運算符本身（第 3 行 `    a[1] ?= x`，`?` 在第 10 列），
	// 不是基底識別符 `a`（第 5 列）——LSP quickfix 需要落在運算符上。
	if !strings.Contains(errs[0], "line 3, column 10") {
		t.Errorf("error not anchored at the `?=` write: %q", errs[0])
	}
}

// TestD9CompoundAssignOnIndexIsError 複合賦算落在下標上（`a[i] += v`）必須明確
// 報錯並給出兩種正確寫法。此前 `+=` 被**靜默丟棄**：AST 裂成 `a[1]` / nil / `1`
// 三條陳述，程式看起來能編譯但語義全錯——這是比 parse error 更糟的沉默洞。
func TestD9CompoundAssignOnIndexIsError(t *testing.T) {
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		t.Run(op, func(t *testing.T) {
			src := "set = () () {\n    a [3]i64 = [1, 2, 3]\n    a[1] " + op + " 1\n}"
			p := New(lexer.New(src))
			p.ParseProgram()
			errs := p.Errors()
			if len(errs) != 1 {
				t.Fatalf("`a[1] %s 1`: want exactly 1 parse error, got %d: %v", op, len(errs), errs)
			}
			// 兩種正確寫法都要印出來：place-match（讀改写）與 `?=`（上拋）。
			want := []string{"`a[1] " + op + " 1`", "a[1]: {", "ok -> it = it " + op[:1] + " 1", "`a[1] ?= a[1] " + op[:1] + " 1`"}
			for _, w := range want {
				if !strings.Contains(errs[0], w) {
					t.Errorf("error missing %q:\n%s", w, errs[0])
				}
			}
		})
	}
}

// TestD9PlaceMatchWriteLowersWithPlaceWrite 写入侧 place-match 解包：
// a[i]: { ok -> it = v } 在编译场景必须触发 place-write 合成写回。
func TestD9PlaceMatchWriteLowersWithPlaceWrite(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "it = literal",
			src:  "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1]: {\n    ok -> it = 42\n    -> _\n}\n}",
		},
		{
			name: "it = it + 1 (read-modify-write)",
			src:  "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1]: {\n    ok -> it = it + 1\n    -> _\n}\n}",
		},
		{
			name: "str element",
			src:  "foo = () (result ?str) {\n    result = nil\n    a [3]str = [\"x\", \"y\", \"z\"]\n    a[1]: {\n    ok -> it = \"w\"\n    -> _\n}\n}",
		},
		{
			name: "ok arm rhs binary expression",
			src:  "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1]: {\n    ok -> it = 1 + 2\n    -> _\n}\n}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, prog := parseCompilePath(t, c.src)
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("compile-path parse errors: %v", errs)
			}
			// S1 翻转：match desugar 是独立步骤，compile path 需显式调用
			// （place-write 改写位于 buildMatchDesugar 内）。
			p.DesugarSurfaceMatchesPlain(prog)
			if !hasSyntheticIndexStore(prog.Statements) {
				t.Errorf("D9 place-match write `%s` did NOT produce the synthetic element store — place-write rewrite missing", c.name)
			}
		})
	}
}

// TestD9PlaceMatchUsesNormalizedSubject 回归门禁（修复 S-d 收尾的 prep.matched
// 归属 bug 后加入）：写入侧 place-match `a[i]: { ok -> ... }` 经过索引主体归一化
// 必须生成 `__match_subj ?elem = a[i]`，且 ok 臂的条件必须是 `__match_subj == ok`
// （tag 比较），而非退化成 `a[i] == 0` 这种对裸下标取值的值比较。
//
// 该 bug 的历史表现：resolveSubjectMatch 在索引归一化（matched 改写为 __match_subj
// 标识符）之前就把 `prep.matched` 捕获成了原始的 `a[i]` 索引表达式，导致 ok 臂条件
// 直接引用裸下标、H0 codegen 把它降糖成 `a[i] == 0`，写回静默失效（i64 案例打印原值、
// str 案例报 "index-store of a 'i32' value into a '%str-long' element"）。
func TestD9PlaceMatchUsesNormalizedSubject(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "i64 array",
			src:  "foo = () (result ?i64) {\n    result = nil\n    arr [3]i64 = [1, 2, 3]\n    arr[1]: {\n    ok -> it = 99\n    -> _\n}\n    result = arr[1]\n}",
		},
		{
			// 单引号双字符字面量是 Nolang 的 str 字面量（双引号是 char）。
			name: "str array",
			src:  "foo = () (result ?str) {\n    result = nil\n    a [3]str = ['x', 'y', 'z']\n    a[1]: {\n    ok -> it = 'w'\n    -> _\n}\n    result = a[1]\n}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, prog := parseCompilePath(t, c.src)
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("compile-path parse errors: %v", errs)
			}
			p.DesugarSurfaceMatchesPlain(prog)
			if !okArmReferencesNormalizedSubject(prog.Statements) {
				t.Errorf("D9 place-match ok-arm condition does not reference the normalized `__match_subj` identifier (prep.matched regression)")
			}
		})
	}
}

// okArmReferencesNormalizedSubject walks the AST looking for the ok-arm of an
// option match: an IfExpression whose Condition is `__match_subj == ok` (an
// InfixExpression with a `==` operator and a left operand that is the
// `__match_subj` Identifier). It also fails the case where the left operand is a
// raw IndexExpression (the prep.matched bug), since that degrades the ok check
// into a value comparison against the bare index.
func okArmReferencesNormalizedSubject(stmts []Statement) bool {
	found := false
	var walkStmt func(s Statement)
	var walkExpr func(e Expression)
	walkStmt = func(s Statement) {
		if s == nil || found {
			return
		}
		switch v := s.(type) {
		case *LetStatement:
			walkExpr(v.Value)
		case *ExpressionStatement:
			walkExpr(v.Expression)
		case *BlockStatement:
			for _, c := range v.Statements {
				walkStmt(c)
			}
		case *FunctionDefinition:
			if v.Body != nil {
				for _, c := range v.Body.Statements {
					walkStmt(c)
				}
			}
		}
	}
	walkExpr = func(e Expression) {
		if e == nil || found {
			return
		}
		switch v := e.(type) {
		case *IfExpression:
			if isOkArmCond(v.Condition) {
				found = true
				return
			}
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
		case *IndexExpression:
			walkExpr(v.Index)
		case *InfixExpression:
			walkExpr(v.Left)
			walkExpr(v.Right)
		}
	}
	for _, s := range stmts {
		walkStmt(s)
	}
	return found
}

// isOkArmCond reports whether cond is `__match_subj == ok` (the correct,
// normalized option-tag check) and NOT a value comparison against a raw index
// (e.g. `a[i] == ok` / `a[i] == 0`), which is the prep.matched regression.
func isOkArmCond(cond Expression) bool {
	inf, ok := cond.(*InfixExpression)
	if !ok || inf.Operator != "==" {
		return false
	}
	id, ok := inf.Left.(*Identifier)
	if !ok {
		return false // left operand must be the normalized __match_subj identifier
	}
	if !strings.Contains(id.Value, "__match_subj") {
		return false
	}
	rhs, ok := inf.Right.(*Identifier)
	if !ok || rhs.Value != "ok" {
		return false
	}
	return true
}

// TestD9PlaceWriteSkippedInSafeIndexSurfaceMode 不变量：纯 surface 场景
// （no fmt / checker / LSP，SkipSafeIndexLowering=true）不得触发 place-write
// 合成写回——否则 formatter 无法完整往返解包语法。
func TestD9PlaceWriteSkippedInSafeIndexSurfaceMode(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "place-match write",
			src:  "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1]: {\n    ok -> it = 42\n    -> _\n}\n}",
		},
		{
			name: "?= write",
			src:  "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1] ?= 42\n}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, prog := parseSurfaceMode(t, c.src)
			if hasSyntheticIndexStore(prog.Statements) {
				t.Errorf("surface mode must NOT produce a synthetic element store (`a[__idx] = __place`) for `%s` — formatter cannot round-trip it", c.name)
			}
			_ = p
		})
	}
}

// TestD9UnwrapAssignToIndexSurfacePreserved 纯 surface 场景里 `a[i] ?= v` 必须
// 保留为 UnwrapAssignStatement（Target=下标表达式，Value=右侧值），供 formatter
// / LSP 直接渲染与诊断；不得被降糖成 match。
func TestD9UnwrapAssignToIndexSurfacePreserved(t *testing.T) {
	src := "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[1] ?= 42\n}"
	_, prog := parseSurfaceMode(t, src)
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("statement 0 is %T, want *FunctionDefinition", prog.Statements[0])
	}
	uas, ok := fd.Body.Statements[2].(*UnwrapAssignStatement)
	if !ok {
		t.Fatalf("surface `a[1] ?= 42` is %T, want *UnwrapAssignStatement", fd.Body.Statements[2])
	}
	idx, ok := uas.Target.(*IndexExpression)
	if !ok {
		t.Fatalf("UnwrapAssignStatement.Target is %T, want *IndexExpression (the write target)", uas.Target)
	}
	if id, ok := idx.Left.(*Identifier); !ok || id.Value != "a" {
		t.Errorf("index base = %v, want identifier `a`", idx.Left)
	}
	// Value 应为字面量 42（未被改写）。
	if _, ok := uas.Value.(*IntegerLiteral); !ok {
		t.Errorf("UnwrapAssignStatement.Value is %T, want *IntegerLiteral", uas.Value)
	}
}
