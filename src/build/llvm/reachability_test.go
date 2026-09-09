package llvm

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestComputeReachableFunctionsBFS 驗證函數級可達性分析的核心不變量：
//  1. 入口 main 與其傳遞閉包（a → b）標記為可達；
//  2. 未被任何路徑呼叫的函數（c）被剪枝；
//  3. 方法呼叫（recv.method()）經由方法後綴過近似，把所有已註冊的
//     ".method" 函數（str.to-str / i64.to-str）標記為可達；
//  4. 頂層 LetStatement 形式的函數（helper）被其呼叫點標記為可達。
//
// 此測試不依賴 LLVM 工具鏈，純 Go 邏輯，秒級完成。
func TestComputeReachableFunctionsBFS(t *testing.T) {
	src := `main = () {
  a()
  helper(5)
  s = 'hi'
  s.to-str()
}
a = () {
  b()
}
b = () {
  1 + 2
}
c = () {
  3 + 4
}
helper = (x i64) (y i64) {
  x + 1
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	// 預掃描階段全量註冊的函數名（computeReachableFunctions 僅從此集合挑選可達者）
	g.funcRetTypes = map[string]string{
		"main":        "",
		"a":           "",
		"b":           "",
		"c":           "",
		"helper":      "",
		"str.to-str":  "",
		"i64.to-str":  "",
	}

	reachable := g.computeReachableFunctions(prog)

	for _, name := range []string{"main", "a", "b", "helper", "str.to-str", "i64.to-str"} {
		if !reachable[name] {
			t.Errorf("expected function %q to be reachable", name)
		}
	}
	// c 從未被任何入口路徑呼叫 → 必須被剪枝
	if reachable["c"] {
		t.Errorf("expected unused function c to be pruned (reachability over-approximation should not pull it in)")
	}
}

// TestComputeReachableFunctionsImplicit 驗證 codegen 內建隱式呼叫
// （fmt-int / out / err 等）始終標記為可達，即使程式未顯式呼叫它們。
// 這些函數被 codegen 直接發出裸呼叫，collectCallTargets 偵測不到，
// 若漏標記會導致連結期 undefined symbol。
func TestComputeReachableFunctionsImplicit(t *testing.T) {
	src := `main = () {
  io.out('hi')
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	g.funcRetTypes = map[string]string{
		"main":   "",
		"out":    "",
		"err":    "",
		"fmt-int": "",
	}

	reachable := g.computeReachableFunctions(prog)
	// io.out → 解析為 out；out 與 err / fmt-int 屬隱式內建呼叫，必須可達
	for _, name := range []string{"main", "out", "err", "fmt-int"} {
		if !reachable[name] {
			t.Errorf("expected implicit-reachable function %q to be reachable", name)
		}
	}
}
