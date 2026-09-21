package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countStdargtypErrors runs ValidateFuncArgs and returns the number of
// stdargtyp errors — the ones raised against a std method/function ARGUMENT.
//
// Before the std signature tables carried parameter types, `recv.m(args)` was
// never type-checked at all: `checkCallArgsInExpr` only handled a callee that
// is a *parser.Identifier, and a method call's callee is a
// *parser.DotExpression. The symptom was "vet passes, run explodes".
func countStdargtypErrors(t *testing.T, src string) int {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	n := 0
	for _, r := range ValidateFuncArgs(prog, "") {
		if r.TraceID == "stdargtyp" {
			n++
		}
	}
	return n
}

// TestStdMethodArgWrongTypeRejected checks the concrete case: `[]byte.index`
// declares its parameter as `str`, so an i64 argument is reported.
func TestStdMethodArgWrongTypeRejected(t *testing.T) {
	src := `b []byte = [1,2,3]
i = b.index(42)
print(i)
`
	if n := countStdargtypErrors(t, src); n != 1 {
		t.Fatalf("expected 1 stdargtyp error for []byte.index(42), got %d", n)
	}
}

// TestStdMethodArgStrMethodRejected covers a concrete non-slice receiver.
func TestStdMethodArgStrMethodRejected(t *testing.T) {
	src := `s = 'abcdef'
print(s.starts-with(42))
`
	if n := countStdargtypErrors(t, src); n != 1 {
		t.Fatalf("expected 1 stdargtyp error for str.starts-with(42), got %d", n)
	}
}

// TestStdMethodArgCorrectCallPasses guards the other side: a well-typed std
// method call must NOT be flagged.
func TestStdMethodArgCorrectCallPasses(t *testing.T) {
	src := `b []byte = [1,2,3]
i = b.index('x')
print(i)
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("well-typed []byte.index('x') wrongly rejected: %d errors", n)
	}
}

// TestStdMethodArgGenericElementDerived covers the receiver-generic path.
//
// `[]i64` has no table entry of its own; its methods come from the
// receiver-generic `[]t.*` entries of std/vec.no, whose parameter is written
// `[]t`. The element is DERIVED from the receiver (`[]i64` pins t = i64), not
// guessed — so the check applies, and a `[3]i64` argument is accepted through
// the array-to-slice coercion.
func TestStdMethodArgGenericElementDerived(t *testing.T) {
	src := `a []i64 = [1,2,3]
print(a.eq([1,2,3]))
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("[]i64.eq([1,2,3]) must pass via array->slice coercion, got %d errors", n)
	}
}

// TestStdMethodArgGenericWrongTypeRejected is the payoff of deriving the
// element: `[]t.eq` expects `[]i64`, so a str argument is now caught. Before
// the signature tables carried parameter types this was silently mis-lowered
// (the callee read the str's bytes using the slice element stride).
func TestStdMethodArgGenericWrongTypeRejected(t *testing.T) {
	src := `a []i64 = [1,2,3]
print(a.eq('x'))
`
	if n := countStdargtypErrors(t, src); n != 1 {
		t.Fatalf("expected 1 stdargtyp error for []i64.eq('x'), got %d", n)
	}
}

// TestStdMethodArgGenericScalarParamRejected covers a scalar generic parameter:
// `[]t.contains` declares `t`, which becomes i64 for a `[]i64` receiver.
func TestStdMethodArgGenericScalarParamRejected(t *testing.T) {
	src := `a []i64 = [1,2,3]
print(a.contains('x'))
`
	if n := countStdargtypErrors(t, src); n != 1 {
		t.Fatalf("expected 1 stdargtyp error for []i64.contains('x'), got %d", n)
	}
}

// TestSliceElemOf pins what HAS an element and what does not. The builtin
// `vec` and map types must report no element: reading a slice as `vec` and
// then type-checking against it is precisely the mis-inference to avoid.
func TestSliceElemOf(t *testing.T) {
	cases := []struct{ typ, want string }{
		{"[]i64", "i64"},
		{"[]byte", "byte"},
		{"[]server-conn", "server-conn"},
		{"[3]i64", "i64"}, // fixed array
		{"[str]i64", ""},  // MAP: [K]V, no element type
		{"vec", ""},       // builtin: no element type
		{"str", ""},
		{"i64", ""},
	}
	for _, c := range cases {
		if got := sliceElemOf(c.typ); got != c.want {
			t.Errorf("sliceElemOf(%q) = %q, want %q", c.typ, got, c.want)
		}
	}
}

// TestSubstituteTypeVar pins whole-token replacement.
func TestSubstituteTypeVar(t *testing.T) {
	cases := []struct{ typ, want string }{
		{"t", "i64"},
		{"[]t", "[]i64"},
		{"?t", "?i64"},
		{"i64", "i64"}, // no t token
		{"txt", "txt"}, // 't' inside "txt" is not a token
		{"starts-with", "starts-with"},
	}
	for _, c := range cases {
		if got := substituteTypeVar(c.typ, "t", "i64"); got != c.want {
			t.Errorf("substituteTypeVar(%q,t,i64) = %q, want %q", c.typ, got, c.want)
		}
	}
}

// TestStdMethodArgAmbiguousReceiverSkipped covers multi-module struct names.
// `server-conn` is defined by tls, ws and sse, so the bare "server-conn.send"
// entry in the table is last-wins over those modules and its parameter types
// belong to whichever module merged last. Resolving through it flagged a real
// `str` argument against another module's `i64` parameter, so an ambiguous
// receiver must be skipped rather than guessed at.
func TestStdMethodArgAmbiguousReceiverSkipped(t *testing.T) {
	if !structMethodIsAmbiguous("server-conn.send") {
		t.Fatalf("server-conn.send should be detected as ambiguous")
	}
	if structMethodIsAmbiguous("enc-conn.init") {
		t.Fatalf("enc-conn.init is single-module and must not be treated as ambiguous")
	}
	if params, key, _ := stdMethodParamTypes("server-conn", "send"); len(params) != 0 {
		t.Fatalf("ambiguous receiver must resolve to no params, got %v (key %q)", params, key)
	}
}

// TestStdMethodArgMapReceiverNotTreatedAsSlice guards the `[K]V` map type.
//
// A map also starts with '[', but it is NOT a slice. Testing only the single
// '[' prefix routed `[str]i64.remove` through the receiver-generic
// `[]t.remove`, whose parameter is the slice INDEX (i64), and flagged the map's
// str KEY as "expected 'i64', got 'str'" — which regressed
// tests/test-map-generics.no and tests/mem-safety/map-tombstone.no. A slice is
// the two characters "[]"; anything else starting with '[' must be skipped.
func TestStdMethodArgMapReceiverNotTreatedAsSlice(t *testing.T) {
	src := `test = () {
    m [str]i64 = { 'a': 0, 'b': 1 }
    m.remove('b')
}
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("map [str]i64.remove('b') must not be checked as a slice, got %d errors", n)
	}
}

// TestStdMethodArgBuiltinPushElementDerived covers push, the ONE slice method
// that takes a value and has no std declaration.
//
// std/vec.no declares []t.pop / []t.insert / []t.remove / []t.truncate itself,
// so those reach the signature tables with the index written literally as i64
// (`[]t.insert = (i i64, val t)`). push is deliberately NOT declared: it is a
// global `#{buildin}` stub (`vec-push = (val t)`) so that no `[]t.push` method
// body is generated. Its parameter type therefore has to come from the builtin
// registry — where Params records i64 for BOTH the element parameter of push
// and the index parameter of remove, with nothing to tell them apart.
// BuiltinMethod.ElemParams carries that distinction; without it push could not
// be checked at all ("vet passes, run explodes").
func TestStdMethodArgBuiltinPushElementDerived(t *testing.T) {
	src := `v []i64 = [1,2,3]
v.push('x')
print(v.len())
`
	if n := countStdargtypErrors(t, src); n != 1 {
		t.Fatalf("expected 1 stdargtyp error for []i64.push('x'), got %d", n)
	}
}

// TestStdMethodArgBuiltinPushWellTypedPasses is the other side: pushing the
// element type must stay silent.
func TestStdMethodArgBuiltinPushWellTypedPasses(t *testing.T) {
	src := `v []str = ['a']
v.push('b')
print(v.len())
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("[]str.push('b') must pass, got %d errors", n)
	}
}

// TestStdMethodArgPushIntegerStrideAllowed guards the STORAGE rule: push stores
// the argument at the receiver's element stride (emitBuiltinVecPush /
// elemTypeOfReceiver, pinned by tests/test-push-narrow.no), so pushing an i64
// into a []byte truncates to one byte by design. src/std depends on it —
// zip-writer.put takes `b i64` and pushes it into a []byte — so integer-vs-
// integer must never be reported. Only a mismatch of KIND is (see
// TestStdMethodArgBuiltinPushElementDerived).
func TestStdMethodArgPushIntegerStrideAllowed(t *testing.T) {
	src := `put = (b i64) {
    d []byte
    d.push(b)
}
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("[]byte.push(i64 var) must pass (element stride narrows), got %d errors", n)
	}
}

// TestStdMethodArgPushNarrowingLiteralAllowed guards integer literals: they are
// inferred i64 but narrow to the element type when the value fits, so the
// existing tests/test-push-narrow.no style (`by []byte; by.push(1)`) must not
// be flagged.
func TestStdMethodArgPushNarrowingLiteralAllowed(t *testing.T) {
	src := `b []byte = [1,2]
b.push(1)
print(b.len())
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("[]byte.push(1) must pass (i64 literal narrows to byte), got %d errors", n)
	}
}

// TestStdMethodArgSliceIndexParamNotElement is the regression guard for the
// whole approach: `[]t.remove` takes an INDEX, so a `[]str` receiver must NOT
// have its element type substituted into that parameter. Reading the builtin
// Params (all i64) and replacing every one of them with the element type is
// what would report "expected 'str', got 'i64'" here.
func TestStdMethodArgSliceIndexParamNotElement(t *testing.T) {
	src := `v []str = ['a', 'b']
v.remove(0)
print(v.len())
`
	if n := countStdargtypErrors(t, src); n != 0 {
		t.Fatalf("[]str.remove(0) must pass: the parameter is an index, got %d errors", n)
	}
}

// TestBuiltinSliceMethodParamTypes pins what the builtin-only fallback knows:
// push's parameter is the ELEMENT ("t"), remove's is an INDEX ("i64"), and a
// receiver with no element (the builtin `vec`, a map) yields nothing.
func TestBuiltinSliceMethodParamTypes(t *testing.T) {
	if params, key := builtinSliceMethodParamTypes("[]i64", "push"); len(params) != 1 || params[0] != "t" || key != "[]t.push" {
		t.Errorf(`builtinSliceMethodParamTypes("[]i64","push") = %v, %q; want ["t"], "[]t.push"`, params, key)
	}
	if params, _ := builtinSliceMethodParamTypes("[]str", "remove"); len(params) != 1 || params[0] != "i64" {
		t.Errorf(`builtinSliceMethodParamTypes("[]str","remove") = %v; want ["i64"] (an index)`, params)
	}
	for _, recv := range []string{"vec", "i64", "str", "[str]i64"} {
		if params, _ := builtinSliceMethodParamTypes(recv, "push"); len(params) != 0 {
			t.Errorf("builtinSliceMethodParamTypes(%q,%q) = %v; want nothing (no element type)", recv, "push", params)
		}
	}
	if params, _ := builtinSliceMethodParamTypes("[]i64", "no-such-method"); len(params) != 0 {
		t.Errorf("unknown method must resolve to nothing, got %v", params)
	}
}

// TestGenericElemOfKey pins the `[]T` layout: Nolang writes slices with EMPTY
// brackets followed by the element (`[]t`), not `[t]`.
func TestGenericElemOfKey(t *testing.T) {
	cases := []struct{ key, want string }{
		{"[]t.eq", "t"},         // receiver-generic: type variable
		{"[]byte.index", ""},    // concrete element
		{"[]str.join", ""},      // concrete element
		{"str.starts-with", ""}, // not a slice receiver
		{"json.get-str", ""},    // not a slice receiver
	}
	for _, c := range cases {
		if got := genericElemOfKey(c.key); got != c.want {
			t.Errorf("genericElemOfKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}
