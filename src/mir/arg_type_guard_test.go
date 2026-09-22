package mir

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// These tests pin the "argument type is not validated" guard rails.
//
// Background: the checker does NOT type-check arguments of METHOD calls
// (`recv.method(args)` lowers to a CallExpression whose Function is a
// *parser.DotExpression, and checkCallArgsInExpr only handles the Identifier
// case). On top of that, the baked std signature table
// (embeddedStdFuncSigs) carries RETURN types only — map[string][]string — so
// even a hypothetical DotExpression branch would have no parameter types to
// check against.
//
// The result was a "vet passes, run explodes" class of failure: `no vet`
// reported 0 errors, and only `no run` failed — either with an internal
// opt-verify dump pointing at a temp .ll file, or (worse) with a heap
// corruption abort. These tests assert the codegen now refuses such calls
// with an explicit, descriptive error instead.

// lowerSourceForTest parses and lowers a nolang source string to MIR.
func lowerSourceForTest(t *testing.T, src string) *Module {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	return mod
}

// TestEmitLLVMRejectsStrIntoCharParam covers the `%str-long` argument reaching a
// scalar (char/i32) parameter. Before the guard, the final `else` of the
// argument marshalling emitted
//
//	store i32 %str-long_val, i32* %carg
//
// and LLVM rejected the whole module in opt-verify:
//
//	call void @str_replace_char(%str-long* %v0.s, i32 %lv1, i32 %lv2, ...)
//	opt: '%lv1' defined with type '%str-long' but expected 'i32'
//
// The call is written against a locally-defined function so that LowerHIR can
// resolve the callee without loading the std module; the checker would reject
// this at the call site ("argument 1 of 'take-char': expected 'char', got
// 'str'"), so this test deliberately bypasses it to exercise the codegen guard
// that must hold even when the checker is out of the picture.
//
// Note `''` is a STR in nolang and `""` is a CHAR — the same mistake in real
// code is `s.replace-char('-', '*')` (correct: s.replace-char("-", "*")).
func TestEmitLLVMRejectsStrIntoCharParam(t *testing.T) {
	src := `take-char = (c char) (out char) {
  out = c
  return
}
main = () () {
  s str = 'hello'
  r = take-char(s)
  print(r)
}
`
	_, err := lowerSourceForTest(t, src).EmitLLVM()
	if err == nil {
		t.Fatal("expected EmitLLVM to reject a str argument for a char parameter, got nil error")
	}
	if !strings.Contains(err.Error(), "aggregate cannot be passed where a scalar is expected") {
		t.Fatalf("expected the aggregate-into-scalar diagnostic, got: %v", err)
	}
	if !strings.Contains(err.Error(), "char") {
		t.Fatalf("expected the diagnostic to name the parameter type, got: %v", err)
	}
}

// TestEmitLLVMRejectsStrPushIntoI64Slice covers vec.push adopting the ARGUMENT's
// type when no conversion exists. `[]i64` has an 8-byte element, but a str
// argument is a 24-byte %str-long; the old fallback (`elemLL = elemTy`) made
// push write with a 24-byte stride while emitIndex/emitIndexStore still read
// with an 8-byte stride — a heap overflow, not merely a wrong value:
//
//	a []i64 = [1, 2, 3]
//	a.push('x')        ; aborted with "signal: abort trap"
func TestEmitLLVMRejectsStrPushIntoI64Slice(t *testing.T) {
	src := `main = () () {
  a []i64 = [1, 2, 3]
  a.push('x')
  print(a.len())
}
`
	_, err := lowerSourceForTest(t, src).EmitLLVM()
	if err == nil {
		t.Fatal("expected EmitLLVM to reject pushing a str into []i64, got nil error")
	}
	if !strings.Contains(err.Error(), "vec.push: cannot push a value of type") {
		t.Fatalf("expected the vec.push element-mismatch diagnostic, got: %v", err)
	}
}

// TestEmitLLVMRejectsMismatchedAggregateLayouts covers an aggregate actual
// reaching an aggregate parameter of a DIFFERENT layout. The by-pointer branch
// passes the argument's own slot address as a `plt*`, and with opaque pointers
// LLVM accepts the pun — so the callee reads the parameter's layout out of a
// smaller object. The real-world case is a std method call (which the checker
// never validates):
//
//	t txt = 'hello'
//	b = t.eq([1, 2, 3])
//
// emitted `call void @txt_eq(%txt* %v1.s, %txt* %v2.s, i1* %cres)` where %v2.s
// is `alloca [3 x i64]` (24 B), so the callee read 256 B out of it. The `%txt`
// len byte at offset 255 happened to be 0, giving a stable-but-wrong `false`.
//
// Written against a local function because LowerHIR cannot resolve a std callee
// (the checker would reject this at the call site, which is why the test bypasses
// it to reach the codegen guard).
func TestEmitLLVMRejectsMismatchedAggregateLayouts(t *testing.T) {
	src := `take-txt = (x txt) () {
  print(x.len())
  return
}
main = () () {
  take-txt([1, 2, 3])
}
`
	_, err := lowerSourceForTest(t, src).EmitLLVM()
	if err == nil {
		t.Fatal("expected EmitLLVM to reject an array argument for a txt parameter, got nil error")
	}
	if !strings.Contains(err.Error(), "different aggregate layouts cannot be passed by pointer") {
		t.Fatalf("expected the aggregate-layout diagnostic, got: %v", err)
	}
}

// TestEmitLLVMAcceptsLegitimatePushCoercions is the control: the guards must not
// reject the coercions that genuinely work, i.e. `[]f64.push(<int>)` (sitofp)
// and `[]str.push(<str literal>)` (no conversion needed).
func TestEmitLLVMAcceptsLegitimatePushCoercions(t *testing.T) {
	src := `main = () () {
  d []f64
  d.push(1)
  d.push(2)
  print(d.len())
  c []str
  c.push('a')
  c.push('b')
  print(c.len())
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("legitimate push coercions must still lower, got: %v", err)
	}
}

// TestEmitLLVMStrFieldFromTxtValue pins the `setfield` declared-type fix.
//
// `emitSetField` derives `fieldLT` from the RHS, so a `str` field assigned a
// `txt` VALUE got `%txt` as the field type. Because an owned `str` field is a
// POINTER field, the store then ran the owned-leaf clone walk with `%txt` as the
// STRUCT type while indexing the DECLARED struct's field list, emitting
//
//	getelementptr inbounds %txt, ptr %gp14, i32 0, i32 2
//
// — field 2 of a 2-field struct → `invalid getelementptr indices`, an internal
// opt-verify failure. It must now convert to an owned %str-long instead.
func TestEmitLLVMStrFieldFromTxtValue(t *testing.T) {
	src := `point { s str }
main = () () {
  p point = point { s: 'abc' }
  t txt = 'hello'
  p.s = t
  print(p.s)
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("assigning a txt to a str field must lower (converted to an owned str), got: %v", err)
	}
}

// TestEmitLLVMRejectsArrayIntoTxtField covers the other half of the same
// declared-type gap: an aggregate whose layout differs from the field's. `no
// vet` reports 0 errors (struct field assignment types are not checked) and the
// old behaviour wrote the array's bytes into the 256-byte `%txt`, leaving the
// i8 length byte at 0 so every read came back empty.
//
// The literal's MIR type moved with the slice-literal fix: `[1, 2, 3]` used to
// lower to a stack `[3 x i64]` and now lowers to a real `%vec` (a slice literal
// is `[]i64`, so `[]t.*` builtins read it as `%vec { len, cap, data }`). For a
// `txt` field that means emitSetField's SLICE-specific branch fires — a more
// precise diagnostic than the generic layout one, but a different string. Both
// are a rejection, which is what this test pins; accept either.
func TestEmitLLVMRejectsArrayIntoTxtField(t *testing.T) {
	src := `point { s txt }
main = () () {
  p point = point { s: 'abc' }
  p.s = [1, 2, 3]
  print(p.s)
}
`
	_, err := lowerSourceForTest(t, src).EmitLLVM()
	if err == nil {
		t.Fatal("expected EmitLLVM to reject an array assigned to a txt field, got nil error")
	}
	if !strings.Contains(err.Error(), "different aggregate layouts cannot be stored") &&
		!strings.Contains(err.Error(), "a slice cannot back a fixed 256-byte txt buffer") {
		t.Fatalf("expected the setfield aggregate-layout diagnostic, got: %v", err)
	}
}

// TestEmitLLVMStrElementFromTxtValue covers the element-store mirror of the
// `setfield` case: a `[]str` element written from a `txt` VALUE. The store is
// typed by the ELEMENT type, so it emitted
//
//	store %str-long %txt_value, %str-long* %ep
//
// → opt-verify "'%lv31' defined with type '%txt' but expected '%str-long'".
// It must now convert to an owned %str-long.
func TestEmitLLVMStrElementFromTxtValue(t *testing.T) {
	src := `main = () () {
  ss []str
  ss.push('x')
  t txt = 'hello'
  ss[0] = t
  print(ss[0])
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("assigning a txt to a []str element must lower (converted to an owned str), got: %v", err)
	}
}

// TestEmitLLVMRejectsStrIntoI64Element covers `emitIndexStore` having no
// aggregate/scalar guard at all (only a `%txt` element case). A `str` or `txt`
// written into an i64 element emitted `store i64 <aggregate>`:
//
//	a []i64 = [1, 2, 3]
//	a[0] = 'abc'      -> "'%lv10' defined with type '%str-long' but expected 'i64'"
//
// `no vet` reports 0 errors for both, so this was another "vet passes, run
// explodes" case.
func TestEmitLLVMRejectsStrIntoI64Element(t *testing.T) {
	for name, src := range map[string]string{
		"str": `main = () () {
  a []i64 = [1, 2, 3]
  a[0] = 'abc'
  print(a[0])
}
`,
		"txt": `main = () () {
  a []i64 = [1, 2, 3]
  t txt = 'hello'
  a[0] = t
  print(a[0])
}
`,
	} {
		_, err := lowerSourceForTest(t, src).EmitLLVM()
		if err == nil {
			t.Fatalf("%s: expected EmitLLVM to reject the element store, got nil error", name)
		}
		if !strings.Contains(err.Error(), "index-store of a") {
			t.Fatalf("%s: expected the index-store diagnostic, got: %v", name, err)
		}
	}
}

// TestEmitLLVMAcceptsStrIntoByteElement is the control for the index-store
// guard: writing a `str` into a BYTE element is the one legitimate cross-type
// pair — the `valT == "%str-long" && elemT == "i8"` branch takes the string's
// first byte (`b[0] = 'a'` -> 97). If the guard ever starts rejecting this, the
// `strIntoByteElem` exception has been lost.
func TestEmitLLVMAcceptsStrIntoByteElement(t *testing.T) {
	src := `main = () () {
  b []byte
  b.push(1)
  b[0] = 'a'
  print(b[0])
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("a str written into a byte element must still lower, got: %v", err)
	}
}

// TestEmitLLVMAcceptsArrayArgumentForSliceParam is the control for the
// aggregate-layout guard: `%vec` and `%str-long` are deliberately excluded
// because they are layout-identical 24-byte triples and the codebase relies on
// that pun (a fixed array argument reaching a slice parameter, a `[]byte`
// parameter receiving a `str`). If the guard ever starts rejecting this, the
// exclusion list has been over-tightened.
func TestEmitLLVMAcceptsArrayArgumentForSliceParam(t *testing.T) {
	src := `show = (a []i64) () {
  print(a.len())
  print(a[0])
  return
}
main = () () {
  show([1, 2, 3])
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("a fixed array argument for a slice parameter must still lower, got: %v", err)
	}
}

// TestEmitLLVMRejectsSliceElementMismatchInField covers struct-field stores
// where the two sides are both slices (or an array decaying to a slice) but
// their ELEMENT types disagree.
//
// Every slice is `%vec` — a 24-byte {len, cap, data} header — whatever its
// element type, so the store itself always verifies: the header shape is
// identical and under opaque pointers even `%str-long*` and `%vec*` are the
// same `ptr`. The damage shows up later, when the DECLARED element stride is
// used to read the buffer. Measured before this guard, all with
// `no vet` == 0 errors:
//
//	[]i64 -> []str field : trace/BPT trap (element 1 read at offset 8 as a
//	                       {len,cap,data}, then freed as a heap pointer)
//	[]str -> []i64 field : printed 4 — element 1 read at offset 8, the len
//	                       field of element 0, instead of at offset 24
//	[]txt -> []str field : empty string — offset 24 instead of 256
//	txt   -> []i64 field : len printed 478560413032
//	txt   -> []byte field: len printed 478560413032
//	str   -> []i64 field : len 2 (a BYTE count) read as i64 elements
//	str   -> []str field : len 2, elements are byte fragments
//	[3]i64 -> []str field: the array->slice copy used the ARRAY's element
//	                       width while every read used the field's
//
// The assertion is deliberately on the SHAPE of the failure rather than one
// exact sentence: the point of the fix is that a clean, descriptive diagnostic
// replaces an internal opt-verify dump, a heap abort, or a silent wrong value.
func TestEmitLLVMRejectsSliceElementMismatchInField(t *testing.T) {
	cases := map[string]string{
		"vec_i64_into_str_field": `
box {
    v []str
}
main = () () {
  b box
  a []i64 = [1, 2, 3]
  b.v = a
  print(b.v.len())
}
`,
		"vec_str_into_i64_field": `
box {
    v []i64
}
main = () () {
  b box
  ss []str
  ss.push('aaaa')
  b.v = ss
  print(b.v.len())
}
`,
		"vec_txt_into_str_field": `
box {
    v []str
}
main = () () {
  b box
  vt []txt
  vt.push('aaaa')
  b.v = vt
  print(b.v.len())
}
`,
		"txt_into_i64_field": `
box {
    v []i64
}
main = () () {
  b box
  t txt = 'hello'
  b.v = t
  print(b.v.len())
}
`,
		"str_into_i64_field": `
box {
    v []i64
}
main = () () {
  b box
  b.v = 'ab'
  print(b.v.len())
}
`,
		"str_into_str_field": `
box {
    v []str
}
main = () () {
  b box
  b.v = 'ab'
  print(b.v.len())
}
`,
		"array_i64_into_str_field": `
box {
    v []str
}
main = () () {
  b box
  a [3]i64 = [1, 2, 3]
  b.v = a
  print(b.v.len())
}
`,
	}
	for name, src := range cases {
		_, err := lowerSourceForTest(t, src).EmitLLVM()
		if err == nil {
			t.Fatalf("%s: expected EmitLLVM to reject the store, got nil error", name)
		}
		// The whole point of the guard: a clean diagnostic, never an internal
		// opt-verify dump (the old failure mode for the array cases).
		if strings.Contains(err.Error(), "opt-verify") {
			t.Fatalf("%s: expected a clean diagnostic, got an internal LLVM failure: %v", name, err)
		}
	}
}

// TestEmitLLVMRejectsSliceIntoTxtField covers a slice VALUE reaching a `txt`
// field. The generic setfield case excludes `%vec`, so this used to slip
// through and store a 24-byte {len,cap,data} header into the 256-byte txt
// buffer: the i8 length byte at offset 255 kept its 0, so the field read back
// empty while `.len()` reported the vec's length.
func TestEmitLLVMRejectsSliceIntoTxtField(t *testing.T) {
	src := `
box {
    v txt
}
main = () () {
  b box
  bb []byte
  bb.push('a')
  b.v = bb
  print(b.v)
}
`
	_, err := lowerSourceForTest(t, src).EmitLLVM()
	if err == nil {
		t.Fatal("expected EmitLLVM to reject a slice assigned to a txt field, got nil error")
	}
	if !strings.Contains(err.Error(), "txt") {
		t.Fatalf("expected the diagnostic to name the txt field, got: %v", err)
	}
}

// TestEmitLLVMAcceptsLegitSliceFieldStores is the control for the slice-field
// guard: every coercion the codebase genuinely relies on must keep lowering.
//
//	[]str -> []str field  : same element type
//	[3]i64 -> []i64 field : fixed array decaying to a slice, same element
//	[]i64 -> [3]i64 field : the reverse, also supported
//	str -> []byte field   : the documented {len,cap,data} byte-view pun
//	[1,2,3] -> []byte     : the literal is typed [3]byte from the target
func TestEmitLLVMAcceptsLegitSliceFieldStores(t *testing.T) {
	cases := map[string]string{
		"matching_str_field": `
box {
    v []str
}
main = () () {
  b box
  ss []str
  ss.push('aaaa')
  ss.push('bbbb')
  b.v = ss
  print(b.v.len())
  print(b.v[1])
}
`,
		"array_into_i64_field": `
box {
    v []i64
}
main = () () {
  b box
  a [3]i64 = [1, 2, 3]
  b.v = a
  print(b.v.len())
  print(b.v[2])
}
`,
		"slice_into_array_field": `
box {
    v [3]i64
}
main = () () {
  b box
  a []i64 = [1, 2, 3]
  b.v = a
  print(b.v[0])
}
`,
		"str_into_byte_field": `
box {
    v []byte
}
main = () () {
  b box
  b.v = 'hello'
  print(b.v.len())
}
`,
		"literal_into_byte_field": `
box {
    v []byte
}
main = () () {
  b box
  b.v = [1, 2, 3]
  print(b.v.len())
  print(b.v[0])
  print(b.v[2])
}
`,
	}
	for name, src := range cases {
		if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
			t.Fatalf("%s: must still lower, got: %v", name, err)
		}
	}
}

// TestEmitLLVMAcceptsBytePushOfStr pins the push mirror of the index-store
// `strIntoByteElem` branch: `b []byte; b.push('a')` takes byte 0 of the str.
//
// Before, `coerce` could not bridge %str-long -> i8, so the `elemLL = elemTy`
// fallback adopted the ARGUMENT's 24-byte %str-long as the element type while
// emitIndex/emitIndexStore still read with an i8 stride — a 24-byte write into
// an i8-strided buffer, i.e. heap corruption. The element guard then reported
// it as an error instead; this test pins that the natural operation WORKS.
func TestEmitLLVMAcceptsBytePushOfStr(t *testing.T) {
	src := `main = () () {
  b []byte
  b.push('a')
  b.push('b')
  print(b.len())
  print(b[0])
  print(b[1])
}
`
	if _, err := lowerSourceForTest(t, src).EmitLLVM(); err != nil {
		t.Fatalf("pushing a single-char str into a []byte must lower, got: %v", err)
	}
}
