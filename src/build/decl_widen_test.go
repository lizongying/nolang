package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD: a binding's DECLARED integer type must be applied at the
// binding, not merely recorded as a hint.
//
// Before the fix `v i64 = b` (b: byte) allocated an i8 slot and did 8-bit
// arithmetic — 200*200 printed 64 — while the two-statement form
// (`v i64` + `v = b`) widened correctly (40000). Method dispatch still used
// the declared i64, so the value was 8 bits wide while its type said 64.
// The two forms must now be identical, which is also the precondition for
// `no fmt` folding `v T` + `v = <expr>` into `v T = <expr>` (src/fmt/
// decl_assign_fold.go).
func TestDeclaredTypeWidensBinding(t *testing.T) {
	src := `widen-probe = (b byte) (out str) {
    v i64 = b
    out = (v * v).to-str()
}
print(widen-probe(200))
`
	body, ok := defineBody(t, compileWith(t, src, "src/std/number.no"), "widen_probe")
	if !ok {
		t.Fatalf("widen-probe function not emitted in LLVM IR (a caller is required so MIR keeps it reachable)")
	}
	if !strings.Contains(body, "zext i8") {
		t.Errorf("byte→i64 widening missing from the binding (no 'zext i8'):\n%s", body)
	}
	if !strings.Contains(body, "mul i64") {
		t.Errorf("arithmetic on the i64-declared binding is not 64-bit (no 'mul i64'):\n%s", body)
	}
	if strings.Contains(body, "mul i8") {
		t.Errorf("arithmetic on the i64-declared binding stayed 8-bit ('mul i8'):\n%s", body)
	}
}

// The same guarantee for a `.` (self) initializer: `v i64 = .` inside an i32
// method must widen, exactly like `v i64` + `v = .`. Measured before the fix:
// 100000*100000 printed 1410065408 (i32 wrap) instead of 10000000000.
func TestDeclaredTypeWidensSelfBinding(t *testing.T) {
	src := `i32.widen-self = () (out str) {
    v i64 = .
    out = (v * v).to-str()
}
x i32 = 100000
print(x.widen-self())
`
	body, ok := defineBody(t, compileWith(t, src, "src/std/number.no"), "i32_widen_self")
	if !ok {
		t.Fatalf("i32.widen-self function not emitted in LLVM IR")
	}
	if !strings.Contains(body, "mul i64") {
		t.Errorf("arithmetic on an i64-declared self binding is not 64-bit (no 'mul i64'):\n%s", body)
	}
	if strings.Contains(body, "mul i32") {
		t.Errorf("arithmetic on an i64-declared self binding stayed 32-bit ('mul i32'):\n%s", body)
	}
}

// NARROWING is legal for exactly one reason: a conventional conversion method
// `i64.to-i32` exists, so the declaration spells out the truncation. Without
// the method the very same source is a compile error (see
// TestDeclaredNarrowWithoutMethodRejected) — never a silent truncation.
func TestDeclaredTypeNarrowsViaToMethod(t *testing.T) {
	src := `narrow-probe = (v i64) (out i64) {
    x i32 = v
    out = x.to-i64()
}
print(narrow-probe(5000000000))
`
	body, ok := defineBody(t, compileWith(t, src, "src/std/number.no"), "narrow_probe")
	if !ok {
		t.Fatalf("narrow-probe not emitted in LLVM IR (a caller is required so MIR keeps it reachable)")
	}
	if !strings.Contains(body, "trunc i64") {
		t.Errorf("i64→i32 narrowing did not emit 'trunc i64' (expected the i64.to-i32 contract):\n%s", body)
	}
}

// A USER-DEFINED `u16.to-i32` must win over the implicit widening: the binding
// value comes from the method (7), not from zero-extending the receiver (3).
// The signature check makes this a real dispatch, not a name match.
func TestDeclaredTypeUsesUserToMethod(t *testing.T) {
	src := `u16.to-i32 = () (out i32) {
    out = 7
}
user-conv-probe = (v u16) (out i32) {
    x i32 = v
    out = x
}
print(user-conv-probe(3))
`
	body, ok := defineBody(t, compileWith(t, src, "src/std/number.no"), "user_conv_probe")
	if !ok {
		t.Fatalf("user-conv-probe not emitted in LLVM IR")
	}
	if !strings.Contains(body, "u16_to_i32") {
		t.Errorf("declared-target conversion did not call the user-defined u16.to-i32 (no '@u16_to_i32'):\n%s", body)
	}
}

// …and with NO such method the narrowing stays a hard error. This is the guard
// that keeps the exemption in src/checker/checker.go (`15w45dqk`) from turning
// into a blanket "any narrowing is fine".
func TestDeclaredNarrowWithoutMethodRejected(t *testing.T) {
	src := `nope-probe = (v u64) (out i32) {
    x i32 = v
    out = x
}
print(nope-probe(1))
`
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t2 := NewTranspiler(nil)
	t2.sourcePath = "src/std/number.no"
	_, err := t2.Compile(src)
	if err == nil {
		t.Fatalf("u64 → i32 binding compiled silently; expected a 15w45dqk narrowing error")
	}
	if !strings.Contains(err.Error(), "cannot assign") {
		t.Errorf("expected a 'cannot assign' narrowing error, got: %v", err)
	}
}
