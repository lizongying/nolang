package parser

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestChainedIfWarning 验证「链式 -> 条件（a -> b -> c）不推荐」警告（W_CHAIN_IF）。
// 仅当链长 >= 3 时告警一次；两链 a -> b 与 { } 块写法不告警。
func TestChainedIfWarning(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantWarns int
	}{
		{
			// 两链 a -> b 只是普通 if-then，不告警。
			name:      "two_link_no_warn",
			input:     "main = () {\n  a -> b -> x = 1\n}\n",
			wantWarns: 0,
		},
		{
			// 三链 a -> b -> c 触发一次告警。
			name:      "three_link_one_warn",
			input:     "main = () {\n  a -> b -> c -> x = 1\n}\n",
			wantWarns: 1,
		},
		{
			// 四链 a -> b -> c -> d 仍只告警一次（每条链仅一次）。
			name:      "four_link_one_warn",
			input:     "main = () {\n  a -> b -> c -> d -> x = 1\n}\n",
			wantWarns: 1,
		},
		{
			// 单条 if 后接 { } 块是推荐的规避写法，不告警。
			name:      "block_body_no_warn",
			input:     "main = () {\n  a -> {\n    b = 1\n    c = 2\n  }\n}\n",
			wantWarns: 0,
		},
		{
			// 链后接块：前段三链仍告警一次。
			name:      "chained_then_block_one_warn",
			input:     "main = () {\n  a -> b -> c -> {\n    d = 1\n  }\n}\n",
			wantWarns: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Fatalf("unexpected parse errors: %v", p.Errors())
			}
			warns := p.WarningsByCode(WarnChainedIf)
			if len(warns) != tt.wantWarns {
				t.Errorf("expected %d W_CHAIN_IF warnings, got %d: %v", tt.wantWarns, len(warns), warns)
			}
		})
	}
}

// TestChainedIfNesting 锁定「a -> b -> c -> d」必须解析为正确嵌套的
// if(a){if(b){if(c){d}}}。这是根因修复的回归防护：旧版非递归 wrapStandaloneChain
// 只建一层嵌套，会把最后一条链接错挂成前一条的 else 分支，静默生成错误代码。
func TestChainedIfNesting(t *testing.T) {
	src := "main = () {\n  a -> b -> c -> x = 1\n}\n"
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}

	ifExpr := findChainedIf(prog)
	if ifExpr == nil {
		t.Fatal("no chained IfExpression found")
	}

	depth := chainDepth(ifExpr)
	if depth != 3 {
		t.Errorf("expected chained if depth 3, got %d", depth)
	}

	conds := chainConditions(ifExpr)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(conds, want) {
		t.Errorf("expected conditions %v (in order), got %v", want, conds)
	}
}

// findChainedIf 返回程序中链深度最大的 IfExpression。
func findChainedIf(prog *Program) *IfExpression {
	var found *IfExpression
	var walkStmt func(s Statement)
	var walkExpr func(e Expression)
	walkStmt = func(s Statement) {
		if s == nil {
			return
		}
		switch n := s.(type) {
		case *BlockStatement:
			for _, st := range n.Statements {
				walkStmt(st)
			}
		case *ExpressionStatement:
			if n.Expression != nil {
				walkExpr(n.Expression)
			}
		case *FunctionDefinition:
			if n.Body != nil {
				walkStmt(n.Body)
			}
		case *LetStatement:
			if n.Value != nil {
				walkExpr(n.Value)
			}
		}
	}
	walkExpr = func(e Expression) {
		if e == nil {
			return
		}
		if n, ok := e.(*IfExpression); ok {
			if found == nil || chainDepth(n) > chainDepth(found) {
				found = n
			}
			if n.Consequence != nil {
				walkStmt(n.Consequence)
			}
			if n.Alternative != nil {
				walkStmt(n.Alternative)
			}
		}
	}
	for _, s := range prog.Statements {
		walkStmt(s)
	}
	return found
}

// chainDepth 沿 Consequence 的 if 链统计嵌套层数。
func chainDepth(ifx *IfExpression) int {
	d := 0
	cur := ifx
	for cur != nil {
		d++
		nx := innerIf(cur.Consequence)
		if nx == nil {
			break
		}
		cur = nx
	}
	return d
}

// chainConditions 沿 Consequence 的 if 链收集各层 Condition 的标识符名（按顺序）。
func chainConditions(ifx *IfExpression) []string {
	var out []string
	cur := ifx
	for cur != nil {
		if id, ok := cur.Condition.(*Identifier); ok {
			out = append(out, id.Value)
		} else {
			out = append(out, fmt.Sprintf("%T", cur.Condition))
		}
		nx := innerIf(cur.Consequence)
		if nx == nil {
			break
		}
		cur = nx
	}
	return out
}

// innerIf 若 block 仅含单个 if 表达式语句，则返回该 if；否则返回 nil。
func innerIf(b *BlockStatement) *IfExpression {
	if b == nil || len(b.Statements) != 1 {
		return nil
	}
	es, ok := b.Statements[0].(*ExpressionStatement)
	if !ok {
		return nil
	}
	if nx, ok := es.Expression.(*IfExpression); ok {
		return nx
	}
	return nil
}
