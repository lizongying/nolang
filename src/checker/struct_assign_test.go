package checker

import (
	"strings"
	"testing"
)

// TestStructAssignFromScalarRejected pins the 「拒收并报诊断」 policy for a
// struct-annotated variable initialised from a plain scalar: `x node = 5`.
//
// Today the assignment passes every checker gate — the LetStatement
// compatibility check is gated on isConcreteType(declared), which answers
// false for a nominal struct type — and codegen then stores the i64 bits into
// a struct slot. `x node = 5` + `x.to-i64()` reads garbage back, and for
// `bigint` it is a straight memory-unsafety hole (the struct's slice header is
// read out of an integer).
//
// The mirror of D2b: a type the backend cannot convert must be rejected with a
// source diagnostic, never silently miscompiled.
func TestStructAssignFromScalarRejected(t *testing.T) {
	src := `node {
    limbs []i64
    len i64
    sign i64
}

f = () (out i64) {
    n node = 5
    out = n.len
}
`
	results := ValidateFuncArgs(mustParse(t, src), "")
	got := 0
	for _, r := range results {
		if strings.Contains(r.Message, "cannot assign i64 value to node variable 'n'") {
			got++
		} else {
			t.Logf("other: %s [%s]", r.Message, r.TraceID)
		}
	}
	if got != 1 {
		t.Fatalf("expected 1 struct-assign error for `n node = 5`, got %d: %+v", got, results)
	}
}

// TestStructAssignStdStructRejected pins the cross-module half: `conn` is a
// struct declared in another module's file, so a single-file validation never
// sees its StructDefinition in program.Statements. The declaration side must
// come from the baked std struct table; without it the guard is silently off
// for exactly the shapes that motivated it (`x bigint = 5` used to compile into
// a garbage struct and abort at runtime).
func TestStructAssignStdStructRejected(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test` (StdSigKey is injected there)")
	}
	src := `f = () (out i64) {
    x conn = 5
    out = 0
}
`
	got := 0
	for _, r := range ValidateFuncArgs(mustParse(t, src), "") {
		if r.TraceID == "structasg" && strings.Contains(r.Message, "to conn variable 'x'") {
			got++
		} else {
			t.Logf("other: %s [%s]", r.Message, r.TraceID)
		}
	}
	if got != 1 {
		t.Fatalf("expected 1 structasg error for `x conn = 5`, got %d", got)
	}
}

// TestStructAssignPromotableIntAllowed pins the implicit-promotion policy at
// the assignment position: an integer into `bigint` has exactly one lossless,
// unambiguous constructor (`bigint.from-i64`), so it is safe and explicit
// enough to allow — the build assembly rewrites it (see
// build/transpiler_promote.go). The checker must not convict what the backend
// converts, otherwise the two halves disagree and the source stops compiling.
func TestStructAssignPromotableIntAllowed(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test` (StdSigKey is injected there)")
	}
	src := `f = () (out i64) {
    k i64 = 2
    a bigint = 5
    b bigint = 1 + 2
    c bigint = -k
    d bigint = k
    e bigint = d
    out = e.to-i64()
}
`
	for _, r := range ValidateFuncArgs(mustParse(t, src), "") {
		if r.TraceID == "structasg" {
			t.Errorf("promotable integer assignment convicted: L%d:C%d %s", r.Line, r.Column, r.Message)
		} else {
			t.Logf("other: %s [%s]", r.Message, r.TraceID)
		}
	}
}

// TestStructAssignNonPromotableStillRejected is the other half of the same
// policy: promotion only covers provably-lossless integers into a struct with
// a single constructor. A str, a float, or an option-typed destination stays
// rejected — `?bigint` in particular cannot take an implicit widen without
// first deciding the ok/err tag, and the backend has no conversion there.
func TestStructAssignNonPromotableStillRejected(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test` (StdSigKey is injected there)")
	}
	cases := []struct {
		body, want string
	}{
		{"x bigint = \"5\"\n", "to bigint variable 'x'"},
		{"x bigint = 1.5\n", "to bigint variable 'x'"},
		{"x ?bigint = 5\n", "to ?bigint variable 'x'"},
	}
	for _, c := range cases {
		src := "f = () (out i64) {\n" + c.body + "    out = 0\n}\n"
		got := 0
		for _, r := range ValidateFuncArgs(mustParse(t, src), "") {
			if r.TraceID == "structasg" && strings.Contains(r.Message, c.want) {
				got++
			}
		}
		if got != 1 {
			t.Errorf("src %q: expected 1 structasg error, got %d", c.body, got)
		}
	}
}

// TestStructAssignControlsAllowed is the "must stay silent" half: the struct
// literal, the struct-returning call, the plain scalar annotation, and the
// reassignment of a struct variable from another struct.
func TestStructAssignControlsAllowed(t *testing.T) {
	src := `node {
    limbs []i64
    len i64
    sign i64
}

mk = () (out node) {
    out = node { limbs: [1], len: 1, sign: 1 }
}

f = () (out i64) {
    a node = node { limbs: [1], len: 1, sign: 1 }
    b node = mk()
    c i64 = 5
    a = b
    out = a.len + b.len + c
}
`
	for _, r := range ValidateFuncArgs(mustParse(t, src), "") {
		t.Errorf("unexpected diagnostic: L%d:C%d %s [%s]", r.Line, r.Column, r.Message, r.TraceID)
	}
}
