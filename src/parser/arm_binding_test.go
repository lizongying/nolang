package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// collectArmBindings walks the parsed program and returns the names of every
// synthetic binding flagged IsArmBinding, together with the names of every
// OTHER synthetic binding. The two sets must stay disjoint: only the
// destructuring binding the parser prepends to a match arm body may be
// arm-scoped, because MIR restores an enclosing variable it shadowed when the
// arm ends (a `?=` capture's synthetic lets must keep writing the REAL
// variable instead).
func collectArmBindings(prog *Program) (arm, other map[string]int) {
	arm = map[string]int{}
	other = map[string]int{}
	var walk func(Node)
	walk = func(n Node) {
		if n == nil {
			return
		}
		switch x := n.(type) {
		case *Program:
			for _, s := range x.Statements {
				walk(s)
			}
		case *FunctionDefinition:
			if x.Body != nil {
				for _, s := range x.Body.Statements {
					walk(s)
				}
			}
		case *BlockStatement:
			for _, s := range x.Statements {
				walk(s)
			}
		case *IfExpression:
			if x.Consequence != nil {
				walk(x.Consequence)
			}
			if x.Alternative != nil {
				walk(x.Alternative)
			}
		case *LetStatement:
			if (x.IsSynthetic || x.IsPropagation) && x.Name != nil {
				if x.IsArmBinding {
					arm[x.Name.Value]++
				} else {
					other[x.Name.Value]++
				}
			}
			if x.Value != nil {
				walk(x.Value)
			}
		case *SurfaceMatch:
			// S1：flip 後 ParseProgram 保留 SurfaceMatch，析構綁定不再以合成
			// `let` 注入，而是編碼在 MatchArm 元資料（BindingName / BindingNames /
			// 隐式 it）。糖形態下「臂綁定」即爲這些名稱。
			for i := range x.Arms {
				a := &x.Arms[i]
				if a.BindingName != "" {
					arm[a.BindingName]++
				}
				for _, bn := range a.BindingNames {
					arm[bn]++
				}
				// 枚舉變體析構綁定 `full(v) ->`：綁定名在 Condition 的
				// CallExpression 參數裡（parser 在枚舉上下文才把 arm.Condition
				// 規範化成 Identifier；純語法期仍保留 CallExpression）。
				if ce, ok := a.Condition.(*CallExpression); ok {
					for _, arg := range ce.Arguments {
						if id, ok := arg.(*Identifier); ok && id.Value != "" {
							arm[id.Value]++
						}
					}
				}
				if armBindsIt(a) {
					arm["it"]++
				}
				if a.Body != nil {
					for _, s := range a.Body.Statements {
						walk(s)
					}
				}
			}
		case *ExpressionStatement:
			if x.Expression != nil {
				walk(x.Expression)
			}
		case *CallExpression:
			if x.Function != nil {
				walk(x.Function)
			}
			for _, a := range x.Arguments {
				walk(a)
			}
		}
	}
	walk(prog)
	return arm, other
}

// armBindsIt 回報某臂在糖形態下是否引入隐式 `it` 綁定（與 checker 的
// matchArmBindings 同口徑）：ok / err / 列舉變體臂、以及非 dot-val 的 catch-all
// 臂都綁定 it；nil 臂與型別臂不綁定 it（型別臂綁定的是 TypeBindingName）。
func armBindsIt(a *MatchArm) bool {
	if a.SkipItBinding || a.IsTypeArm {
		return false
	}
	if a.IsWildcard && !a.IsDotVal {
		return true
	}
	id, ok := a.Condition.(*Identifier)
	if !ok {
		return false
	}
	switch id.Value {
	case "ok", "err":
		return true
	case "nil":
		return false
	default:
		// 枚舉變體臂（`s1 ->`）綁定 it。
		return true
	}
}

func parseArmTest(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	return prog
}

// TestArmBindingFlagOnDestructuring locks that `ok(v) ->` / `full(v) ->` mark
// their synthetic payload binding IsArmBinding — that flag is what makes MIR
// give the binding its own slot and restore the enclosing variable afterwards.
func TestArmBindingFlagOnDestructuring(t *testing.T) {
	src := `box { full(v i64) empty }
main = () {
    a ?i64 = ok(1)
    a: {
        ok(x) -> print(x)
        nil -> print(0)
        err -> print(1)
    }
    p box = full(2)
    p: {
        full(v) -> print(v)
        empty -> print(0)
    }
}`
	prog := parseArmTest(t, src)
	arm, _ := collectArmBindings(prog)
	if arm["x"] == 0 {
		t.Errorf("`ok(x) ->` must flag its binding IsArmBinding; arm bindings: %v", arm)
	}
	if arm["v"] == 0 {
		t.Errorf("`full(v) ->` must flag its binding IsArmBinding; arm bindings: %v", arm)
	}
}

// TestArmBindingFlagNotOnOptionCapture locks the other direction: only the
// name a match arm actually destructures is flagged. The `?=` desugar builds
// its own synthetic match, so its `it` bindings are arm bindings too — but the
// capture's TARGET (`out`, a result parameter) and its `__unwrap_N` temporaries
// must NOT be, or MIR would give the result parameter an arm-scoped slot
// instead of writing the real one.
func TestArmBindingFlagNotOnOptionCapture(t *testing.T) {
	src := `div = (c i64, d i64) (out ?i64) {
    out ?= c / d
}
main = () {
    r = div(6, 3)
    r: {
        ok(v) -> print(v)
        nil -> print(0)
        err -> print(1)
    }
}`
	prog := parseArmTest(t, src)
	arm, _ := collectArmBindings(prog)
	for name := range arm {
		if name != "it" && name != "v" {
			t.Errorf("only match-arm pattern names may be arm-scoped; %q was flagged: %v", name, arm)
		}
	}
	if arm["v"] == 0 {
		t.Errorf("expected the user match arm's binding to be flagged; got %v", arm)
	}
}
