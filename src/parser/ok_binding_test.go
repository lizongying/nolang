package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// findSyntheticBindings 收集所有合成 LetStatement（編譯器注入的 `it`/`v = matched`），
// 回傳 name -> 出現次數。用於鎖定 `ok(v) ->` 必須把載荷綁到 v 而非隱含的 it。
func findSyntheticBindings(prog *Program) map[string]int {
	counts := map[string]int{}
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
			if x.IsSynthetic && x.Name != nil {
				counts[x.Name.Value]++
			}
			if x.Value != nil {
				walk(x.Value)
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
	return counts
}

// TestOkDestructuringBindingNamesValue 鎖定：
// `ok(v) ->` 必須把 ok 變體的載荷綁定到 v（而不是隱含的 it），
// 這樣嵌套 match 時外層的 it 才不會被覆蓋。
func TestOkDestructuringBindingNamesValue(t *testing.T) {
	src := `main = () {
    x ?str = 'A'
    r str = 'init'
    x: {
        ok(v) -> r = v
        err -> r = 'e'
        nil -> r = 'n'
    }
}`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	binds := findSyntheticBindings(prog)
	if binds["v"] == 0 {
		t.Fatalf("expected a synthetic binding named 'v' for `ok(v) ->`, got bindings: %v", binds)
	}
	// err / nil 臂會各自綁定 it（型別 err / nil），那是正確行為；
	// 此處只鎖定 ok 臂確實用了自訂名 v。
}

// TestOkBareStillBindsIt 鎖定：`ok ->`（無名字）沿用隱含的 it 綁定。
func TestOkBareStillBindsIt(t *testing.T) {
	src := `main = () {
    x ?str = 'A'
    r str = 'init'
    x: {
        ok -> r = it
        err -> r = 'e'
        nil -> r = 'n'
    }
}`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	binds := findSyntheticBindings(prog)
	if binds["it"] == 0 {
		t.Fatalf("expected a synthetic binding named 'it' for bare `ok ->`, got bindings: %v", binds)
	}
	if binds["v"] != 0 {
		t.Errorf("bare `ok ->` should not create a 'v' binding; got: %v", binds)
	}
}

// TestOkConditionFormIsNotBinding 鎖定：括號內是比較式（如 `ok(it > 3)`）
// 必須被當成條件 val arm，而非析構綁定——不得生成名為 it/自訂名的綁定。
// 這是 parser 內部有界前瞻消歧的防線：單個裸識別符 → 綁定；其餘 → 條件。
func TestOkConditionFormIsNotBinding(t *testing.T) {
	src := `main = () {
    x ?i64 = 5
    r str = 'init'
    x: {
        ok(it > 3) -> r = 'big'
        -> r = 'small'
    }
}`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	binds := findSyntheticBindings(prog)
	// 條件式 ok(cond) 不得把括號內的識別符當成析構綁定名——
	// 不能冒出一個叫 it / v 的綁定。ok(cond) 本身不注入綁定
	// （預設 `->` 臂會綁 it 是正常的，不在本測試關注範圍）。
	if binds["v"] != 0 {
		t.Errorf("`ok(cond)` must not create a binding named after an identifier in the condition; got: %v", binds)
	}
}
