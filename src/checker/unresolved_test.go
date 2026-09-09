package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func parseForUnresolvedTest(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	p.Filename = "test.no"
	funcSigs, structFields := CollectStdModuleSignatures()
	methodSigs := CollectStdMethodSigs()
	p.SetExternSignatures(funcSigs, methodSigs, structFields)
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil for source:\n%s", src)
	}
	return prog
}

// 本地變數與 std 模組同名時（如參數 path），path.slice 是對本地 str
// 變數的方法呼叫，不應被誤判為「模組函數呼叫」。
func TestCheckUnresolvedModuleCalls_LocalVarShadowingModule(t *testing.T) {
	src := `
get-ext = (path str) (ext str) {
    ext = path.slice(1, 2)
}
`
	prog := parseForUnresolvedTest(t, src)
	res := CheckUnresolvedModuleCalls(prog)
	if len(res) != 0 {
		t.Fatalf("expected no unresolved-module errors for local var method call, got: %+v", res)
	}
}

// 頂層 path.missing_fn() 中 path 不是本地變數，應照常報錯（ckaj3355）。
func TestCheckUnresolvedModuleCalls_GenuineModuleTypoStillReported(t *testing.T) {
	src := `
main = () {
    x = path.missing_fn('a')
    print(x)
}
`
	prog := parseForUnresolvedTest(t, src)
	res := CheckUnresolvedModuleCalls(prog)
	found := false
	for _, r := range res {
		if r.TraceID == "ckaj3355" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unresolved-module error (TraceID ckaj3355) for genuine module typo, got: %+v", res)
	}
}
