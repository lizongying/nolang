package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCallvecStoreNonEmptyD21 reproduces D21: when a SliceLiteral's elements
// are function calls with output parameters, the callvec codegen must not
// emit "store i64 , i64* ..." (empty value position).
//
// Root cause: the hasOutputParam check for method calls used >= instead of >,
// causing the last input argument (an Identifier) to be misidentified as the
// output parameter. This made the call be treated as statement-form (void
// return) instead of expression-form (voidSingleOutput), producing empty
// values in the generated IR.
func TestCallvecStoreNonEmptyD21(t *testing.T) {
	src := `json-util.kv-raw = (key str, val str) (out str) {
    out = key - ':' - val
}

json-util.kv-str = (key str, val str) (out str) {
    out = key - ':' - val
}

json-util.obj = (pairs []str, count i64) (out str) {
    out = '{'
    i = 0
    {
        i > 0 -> out = out - ','
        -> {}
        out = out - pairs[i]
        i = i + 1
    } (i < count)
    out = out - '}'
}

main = () {
    T = 'test-type'
    X = 'test-msg'
    r = json-util.obj([json-util.kv-raw('type', T), json-util.kv-str('m', X)], 2)
    print(r)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Check for the specific bug: "store i64 , i64* " (empty value)
	if strings.Contains(ir, "store i64 , i64* ") {
		t.Errorf("IR contains empty-value store (D21 bug)")
	}

	// Check for empty store of any type
	if strings.Contains(ir, "store %str-long , %str-long* ") {
		t.Errorf("IR contains empty-value str-long store (D21 bug)")
	}

	// Verify all callvec store instructions have non-empty values
	lines := strings.Split(ir, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store ") && strings.Contains(trimmed, "callvec.gep") {
			if strings.Contains(trimmed, "store i64 ,") || strings.Contains(trimmed, "store %str-long ,") {
				t.Errorf("line %d: empty value in callvec store: %s", i+1, line)
			}
		}
	}
}

// TestMethodCallExpressionFormD21 tests the core fix: when a method call
// with output parameters is used in expression form (not statement form),
// the hasOutputParam check must NOT misidentify the last argument as the
// output parameter.
//
// Before the fix: len(args)+1 >= paramCount was true, so the last arg
// (an Identifier) was treated as the output param, making the call void.
// After the fix:  len(args)+1 >  paramCount is false, so voidSingleOutput
// is triggered and the call returns a proper register.
//
// We define our own str.slice to avoid depending on the std library.
func TestMethodCallExpressionFormD21(t *testing.T) {
	src := `str.sub = (start i64, end i64) (val str) {
    val = 'result'
}

my-conn {
    method str
    path str
}

my-conn.parse = (request-line str) {
    sp1 = 3
    .method = request-line.sub(0, sp1)
}

main = () {
    c = my-conn {
        method: ''
        path: '/'
    }
    c.parse('GET /path HTTP/1.1')
    print(c.method)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR should NOT contain empty-value stores
	if strings.Contains(ir, "store %str-long , %str-long* ") {
		t.Errorf("IR contains empty-value str-long store (D21 bug in method call expression form)")
	}
	if strings.Contains(ir, "store i64 , i64* ") {
		t.Errorf("IR contains empty-value i64 store (D21 bug)")
	}

	// The IR should contain a call to str.sub (the method was not dropped)
	if !strings.Contains(ir, "@str.sub") {
		t.Errorf("IR should contain call to @str.sub\nIR:\n%s", ir)
	}

	// The voidSingleOutput path should allocate a temp and load the result
	if !strings.Contains(ir, "%vso.tmp.") {
		t.Errorf("IR should contain voidSingleOutput temp (vso.tmp) for expression-form method call\nIR:\n%s", ir)
	}
}

// TestInlineObjAsArgNotDroppedD22 reproduces D22: when an inline function call
// with output parameters is passed directly as a function argument, the result
// must not be dropped/empty.
func TestInlineObjAsArgNotDroppedD22(t *testing.T) {
	src := `json-util.kv-raw = (key str, val str) (out str) {
    out = key - ':' - val
}

json-util.kv-str = (key str, val str) (out str) {
    out = key - ':' - val
}

json-util.obj = (pairs []str, count i64) (out str) {
    out = '{'
    i = 0
    {
        i > 0 -> out = out - ','
        -> {}
        out = out - pairs[i]
        i = i + 1
    } (i < count)
    out = out - '}'
}

write-it = (code i64, body str) {
    print(body)
}

main = () {
    T = 'test-type'
    X = 'test-msg'
    ; Inline obj(...) as argument — should not be dropped (D22)
    write-it(404, json-util.obj([json-util.kv-raw('type', T), json-util.kv-str('m', X)], 2))
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR should not contain empty-value stores
	if strings.Contains(ir, "store i64 , i64* ") {
		t.Errorf("IR contains empty-value store (D22 bug)")
	}
	if strings.Contains(ir, "store %str-long , %str-long* ") {
		t.Errorf("IR contains empty-value str-long store (D22 bug)")
	}

	// The IR should contain calls to the functions (not dropped)
	if !strings.Contains(ir, "@json-util.kv-raw") && !strings.Contains(ir, "@json_util.kv_raw") {
		t.Errorf("IR should contain call to json-util.kv-raw")
	}
	if !strings.Contains(ir, "@json-util.obj") && !strings.Contains(ir, "@json_util.obj") {
		t.Errorf("IR should contain call to json-util.obj")
	}
}

// TestStructFieldAssignFromMethodCall tests that struct field assignment
// from a method call with output parameters correctly captures the result.
// This is the specific pattern that triggered D21 in the nowork project:
//   .method = request-line.sub(0, sp1)
func TestStructFieldAssignFromMethodCall(t *testing.T) {
	src := `str.sub = (start i64, end i64) (val str) {
    val = 'result'
}

my-conn {
    method str
    path str
}

my-conn.parse = (request-line str) {
    sp1 = 3
    .method = request-line.sub(0, sp1)
}

main = () {
    c = my-conn {
        method: ''
        path: '/'
    }
    c.parse('GET /path HTTP/1.1')
    print(c.method)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR should NOT contain empty-value stores
	if strings.Contains(ir, "store %str-long , %str-long* ") {
		t.Errorf("IR contains empty-value str-long store in struct field assignment from method call")
	}
	if strings.Contains(ir, "store i64 , i64* ") {
		t.Errorf("IR contains empty-value i64 store")
	}

	// The IR should contain a call to str.sub
	if !strings.Contains(ir, "@str.sub") {
		t.Errorf("IR should contain call to @str.sub\nIR:\n%s", ir)
	}
}

// TestVecTruncateDispatch verifies that out.truncate(n) on a []byte variable
// dispatches to vec.truncate (ForwardFunc) and NOT to the POSIX truncate
// CLibCall. Before the fix, the prefix-stripping logic in the skipBuiltin
// section would strip "out.truncate" → "truncate" and dispatch to the POSIX
// truncate system call, producing invalid IR (treating the i64 argument as a
// %str-long pointer).
func TestVecTruncateDispatch(t *testing.T) {
	src := `bytes-cat = (a []byte, b []byte) (out []byte) {
    an = a.len
    bn = b.len
    out = with-len(an + bn)
    out.truncate(an + bn)
}

main = () {
    x = [1, 2, 3]
    y = [4, 5]
    z = bytes-cat(x, y)
    print(z.len)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR must NOT contain a call to the POSIX truncate function
	if strings.Contains(ir, "call i32 @truncate(") || strings.Contains(ir, "call i32 @nolang.win_truncate(") {
		t.Errorf("IR dispatched to POSIX truncate instead of vec.truncate\nIR:\n%s", ir)
	}

	// The IR must NOT contain the pattern where an i64 is used as a
	// %str-long pointer (the hallmark of the misdispatch bug)
	if strings.Contains(ir, "getelementptr inbounds %str-long, %str-long* %add.tmp") {
		t.Errorf("IR contains i64-as-str-long-pointer bug (POSIX truncate misdispatch)\nIR:\n%s", ir)
	}
}

// TestVecTruncateOptionDispatch verifies that out.truncate(n) on a ?[]byte
// (Option) variable also dispatches to vec.truncate, not POSIX truncate.
// This covers the base64 decode pattern where the output is an optional vec.
func TestVecTruncateOptionDispatch(t *testing.T) {
	src := `decode-opt = (s str) (out ?[]byte) {
    out = nil
    out = with-len(10)
    j = 5
    pad-count = 2
    out.truncate(j - pad-count)
}

main = () {
    r = decode-opt('test')
    print(0)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR must NOT contain a call to the POSIX truncate function
	if strings.Contains(ir, "call i32 @truncate(") || strings.Contains(ir, "call i32 @nolang.win_truncate(") {
		t.Errorf("IR dispatched to POSIX truncate instead of vec.truncate for Option type\nIR:\n%s", ir)
	}

	// The IR must NOT contain the i64-as-str-long-pointer pattern
	if strings.Contains(ir, "getelementptr inbounds %str-long, %str-long* %sub.tmp") {
		t.Errorf("IR contains i64-as-str-long-pointer bug for Option type (POSIX truncate misdispatch)\nIR:\n%s", ir)
	}
}
