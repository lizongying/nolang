//go:build !wasm

package wasm

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// Regression guard for the playground's "structs & methods" demo.
//
// Nolang methods (`user.greet = () { ... }`) carry the receiver as an implicit
// FIRST RESULT named "self" (the main backend's write-back convention); the
// surface syntax declares no receiver parameter. The WASM backend used to feed
// `fd.Results` straight into the function signature, so a method was emitted as
// `() -> (i32)` while `emitMethodCall` pushed the receiver as an argument.
// Every method call therefore left one unconsumed i32 on the stack and the
// whole module failed WebAssembly validation with:
//
//	block with type: () -> [] returns: 0 but stack has: N values
//
// Direct-WASM represents a struct as a POINTER, so mutation through it is
// already visible to the caller: the receiver only needs to be passed IN.
// `declareFunction` now converts that implicit result into the first
// parameter. These tests are deliberately tool-free so they run on hosts
// without wasmtime / wasm-tools.

func generateForSignatureTest(t *testing.T, src string) *Generator {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	program := p.ParseProgram()
	if program == nil {
		t.Fatal("ParseProgram returned nil")
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	p.DesugarSurfaceMatchesPlain(program)
	g := &Generator{}
	if out, err := g.Generate(program); err != nil || len(out) == 0 {
		t.Fatalf("Generate: err=%v outLen=%d", err, len(out))
	}
	return g
}

const methodSignatureSrc = `user {
    name str
    age i64
}
user.greet = () {
    print('hello')
}
user.get = () (n str) {
    n = .name
}
plain = (x i64) (y i64) {
    y = x
}
u = user{
    name: 'Alice'
    age: 30
}
u.greet()
x = u.get()
print('{x}')
p = plain(7)
print('{p}')
`

func TestMethodReceiverIsParameterNotResult(t *testing.T) {
	g := generateForSignatureTest(t, methodSignatureSrc)

	for _, name := range []string{"user.greet", "user.get"} {
		if got := g.funcParamNames[name]; !reflect.DeepEqual(got, []string{"self"}) {
			t.Errorf("%s: paramNames = %v, want [self]", name, got)
		}
		if got := g.funcParams[name]; !reflect.DeepEqual(got, []ValType{I32}) {
			t.Errorf("%s: params = %v, want [i32] (struct pointer)", name, got)
		}
	}
	if got := g.funcResultNames["user.greet"]; len(got) != 0 {
		t.Errorf("user.greet: resultNames = %v, want [] (self must not be returned)", got)
	}
	if got := g.funcResultNames["user.get"]; !reflect.DeepEqual(got, []string{"n"}) {
		t.Errorf("user.get: resultNames = %v, want [n]", got)
	}
	if got := g.funcResults["user.get"]; !reflect.DeepEqual(got, []ValType{I32}) {
		t.Errorf("user.get: results = %v, want [i32]", got)
	}

	// A plain function must be completely unaffected by the rewrite.
	if got := g.funcParamNames["plain"]; !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("plain: paramNames = %v, want [x]", got)
	}
	if got := g.funcResultNames["plain"]; !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("plain: resultNames = %v, want [y]", got)
	}
}

func TestMethodCallsEmitValidatableModule(t *testing.T) {
	out := parseAndGenerate(t, methodSignatureSrc)

	// The type section must contain a `(i32) -> ()` signature for greet and a
	// `(i32) -> (i32)` one for get. Pre-fix, both methods were `() -> (i32)`.
	types := parseTypeSection(t, out)
	wantVoid, wantValue := false, false
	for _, ft := range types {
		if len(ft.params) == 1 && ft.params[0] == 0x7f {
			if len(ft.results) == 0 {
				wantVoid = true
			}
			if len(ft.results) == 1 && ft.results[0] == 0x7f {
				wantValue = true
			}
		}
	}
	if !wantVoid || !wantValue {
		t.Errorf("type section missing method signatures: (i32)->() = %v, (i32)->(i32) = %v; got %+v",
			wantVoid, wantValue, types)
	}

	if !validateWithWasmTools(t, out) {
		if stdout, ok := runWithWasmtime(t, out); ok {
			if !strings.Contains(stdout, "Alice") {
				t.Errorf("method demo stdout = %q, want it to contain Alice", stdout)
			}
		}
		// Neither external validator is installed: the structural assertions
		// above are still load-bearing and must not be skipped silently.
		return
	}
	if stdout, ok := runWithWasmtime(t, out); ok && !strings.Contains(stdout, "Alice") {
		t.Errorf("method demo stdout = %q, want it to contain Alice", stdout)
	}
}

func TestMethodMutationAndReturnRoundTrip(t *testing.T) {
	src := `counter {
    n i64
}
counter.bump = () {
    .n = .n + 1
}
counter.read = () (v i64) {
    v = .n
}
c = counter{
    n: 41
}
c.bump()
v = c.read()
print('v={v}')
`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	if stdout, ok := runWithWasmtime(t, out); ok && !strings.Contains(stdout, "v=42") {
		t.Errorf("method mutation stdout = %q, want v=42", stdout)
	}
}
