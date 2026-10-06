package build

import (
	"regexp"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// These tests guard that a `.len()` call written inside a slice/string-typed
// method body resolves to a real length access — never to a phantom call to a
// non-existent `<type>.len` function symbol.
//
// Contract vs. the (now deleted) legacy LLVM backend: the MIR backend only
// emits REACHABLE functions, so each test defines the method AND calls it from
// `main` (otherwise the definition is dead-code-eliminated and absent from the
// IR). Function names are `sanitize()`d — every non-[A-Za-z0-9_] rune becomes
// `_`, so `[]byte.test-len` emits as `@_xbyte_test_len`, `[]i64.test-len` as
// `@__i64_test_len`, `str.test-len` as `@str_test_len`.

// defineBody returns the body (from the `define` line through its closing `}`)
// of the first function whose definition line contains nameSubstr.
func defineBody(t *testing.T, llvmIR, nameSubstr string) (string, bool) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^define [^\n]*` + regexp.QuoteMeta(nameSubstr) + `[^\n]*\{`)
	loc := re.FindStringIndex(llvmIR)
	if loc == nil {
		return "", false
	}
	rest := llvmIR[loc[0]:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return rest, true
	}
	return rest[:end+2], true
}

// compileWith is the shared driver: parse + transpile `src` (tagged with the
// given std sourcePath so method definitions on primitive receivers are
// permitted) and return the emitted LLVM IR.
func compileWith(t *testing.T, src, sourcePath string) string {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t2 := NewTranspiler(nil)
	t2.sourcePath = sourcePath
	llvmIR, err := t2.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	return llvmIR
}

// TestSliceMethodLenCall verifies that .len() called inside a []byte method
// body is inlined to a %vec field-0 (i64 len) load — NOT a call to a
// non-existent []byte.len function. The []byte method has no user-defined
// str-like .len, so a phantom `_LB__RB_byte.len`/`@_xbyte_len` call would be a
// bug; the correct lowering is a direct getelementptr+load with zero calls.
func TestSliceMethodLenCall(t *testing.T) {
	src := `[]byte.test-len = () (n i64) {
    n = .len()
}
main = () {
    b []byte = [1, 2, 3]
    n = b.test-len()
    print(n)
}
`
	llvmIR := compileWith(t, src, "src/std/byte.no")

	body, ok := defineBody(t, llvmIR, "test_len")
	if !ok {
		t.Fatalf("test-len function not emitted in LLVM IR (a caller is required so MIR keeps it reachable)")
	}
	if !strings.Contains(body, "getelementptr") {
		t.Errorf("test-len body does not load the %%vec len field via getelementptr:\n%s", body)
	}
	if !strings.Contains(body, "load i64") {
		t.Errorf("test-len body does not contain 'load i64' for the len field:\n%s", body)
	}
	// The byte length is inlined directly — there must be no call at all, and
	// in particular no call to a non-existent byte.len helper.
	if strings.Contains(body, "call") {
		t.Errorf("test-len body should inline the vec len field access, but contains a call:\n%s", body)
	}
}

// TestSliceMethodLenCallOnI64 verifies .len() works inside a []i64 (generic
// slice, as opposed to the concrete []byte buffer) method body. This also
// regression-guards the resolveCallee dispatch fix: the front end lowers
// `v.test-len()` (v: []i64) to a KIdent callee `[]i64.test-len` that IS a
// registered function, so the resolver must keep that concrete name instead of
// canonicalising it to the undefined `[]t.test-len` (which previously failed
// with "unknown callee []t.test-len in func main").
func TestSliceMethodLenCallOnI64(t *testing.T) {
	src := `[]i64.test-len = () (n i64) {
    n = .len()
}
main = () {
    v []i64 = [1, 2, 3]
    n = v.test-len()
    print(n)
}
`
	// If the callee were mis-resolved to the generic stub, Compile would fail
	// with "unknown callee []t.test-len" and compileWith would t.Fatalf.
	llvmIR := compileWith(t, src, "src/std/vec.no")

	body, ok := defineBody(t, llvmIR, "test_len")
	if !ok {
		t.Fatalf("test-len function not emitted in LLVM IR")
	}
	if !strings.Contains(body, "load i64") {
		t.Errorf("test-len body does not contain 'load i64' for the len field:\n%s", body)
	}
}

// TestSliceMethodLenCallOnStr verifies .len() works inside a str method body.
// Unlike []byte (len is a pure builtin field access), str.len is BOTH a builtin
// and a user-defined function in str.no; the current backend resolves
// `.len()` on a str receiver to the genuine @str_len function. The guard here is
// behavioural: the call must COMPILE and the body must obtain the length via an
// i64 load — it must not fall through to a phantom/unresolved symbol.
func TestSliceMethodLenCallOnStr(t *testing.T) {
	src := `str.test-len = () (n i64) {
    n = .len()
}
main = () {
    s = 'hello'
    n = s.test-len()
    print(n)
}
`
	llvmIR := compileWith(t, src, "src/std/str.no")

	body, ok := defineBody(t, llvmIR, "test_len")
	if !ok {
		t.Fatalf("test-len function not emitted in LLVM IR")
	}
	// The length must be obtained (an i64 load from either the inline field or
	// the @str_len result). A broken dispatch would reference an undefined callee
	// and never reach a valid load.
	if !strings.Contains(body, "load") {
		t.Errorf("test-len body does not contain a 'load' for the len value:\n%s", body)
	}
}
