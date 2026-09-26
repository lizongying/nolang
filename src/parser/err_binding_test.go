package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestErrDestructuringBindingNamesValue 鎖定：
// `err(e) ->` 必須把 err 變體的載荷綁定到 e（而不是隱含的 it），並且在
// option 完整性檢查中等價於 `err ->`（不得報「missing: err」缺臂錯誤）。
// 嵌套 match 時這能保住外層的 `it`/綁定名（否則內層會把它覆蓋掉）。
func TestErrDestructuringBindingNamesValue(t *testing.T) {
	src := `main = () {
    x ?i64 = err('boom')
    r str = 'init'
    x: {
        ok(v) -> r = v.to-str()
        err(e) -> r = e
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
	if binds["e"] == 0 {
		t.Fatalf("expected a synthetic binding named 'e' for `err(e) ->`, got bindings: %v", binds)
	}
}

// TestErrBindingConditionFormNotConsumedAsBinding 鎖定：括號內不是「單一裸識別符」
// （如 `err(it > 3)`）時不得誤判為析構綁定。
func TestErrBindingConditionFormNotConsumedAsBinding(t *testing.T) {
	src := `main = () {
    x ?i64 = err('boom')
    r str = 'init'
    x: {
        ok(v) -> r = v.to-str()
        err(it > 3) -> r = 'big'
        nil -> r = 'n'
        -> r = 'd'
    }
}`
	l := lexer.New(src)
	p := New(l)
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
}

// TestErrDestructuringCompleteness 鎖定：`err(e) ->` 必須被 option match
// 完整性檢查視為 err 臂（缺臂錯誤中不得再出現 missing: err）。
func TestErrDestructuringCompleteness(t *testing.T) {
	src := `main = () {
    x ?str = err('boom')
    r str = 'init'
    x: {
        err(e) -> r = e
        nil -> r = 'n'
        ok -> r = it
    }
}`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	for _, msg := range p.Errors() {
		if strings.Contains(msg, "missing: err") || strings.Contains(msg, "must handle all branches") {
			t.Fatalf("`err(e) ->` must count as the err arm, got: %v", p.Errors())
		}
	}
	binds := findSyntheticBindings(prog)
	if binds["e"] == 0 {
		t.Fatalf("expected a synthetic binding named 'e', got bindings: %v", binds)
	}
}
