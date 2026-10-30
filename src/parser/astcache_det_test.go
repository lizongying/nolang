package parser

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestCachedProgramEncodingIsDeterministic pins the byte-reproducibility of the
// AST cache serialization.
//
// encoding/gob encodes maps in randomized iteration order, so before
// CachedProgram implemented GobEncoder the same program produced different
// bytes on every encode. That is invisible to a cache HIT but breaks the
// ARTIFACT: genstdsig's "skip the write when the content is unchanged" guard
// never fired, so every `make gen` rewrote the 2.4 MB Tier-0 blob, recompiled
// every package embedding it, and produced a different `no` binary from
// identical sources.
//
// The test is deliberately built so it cannot pass vacuously:
//
//   - every map-bearing field is populated with MORE THAN ONE entry (a
//     single-entry map has only one possible iteration order, so it would not
//     exercise the nondeterminism at all);
//   - the shadow is walked and must contain NO map anywhere — that is the
//     structural guard against a new map field being added to one of the
//     map-bearing types without a matching shadow, which would silently
//     reintroduce the nondeterminism this test exists to prevent.
func TestCachedProgramEncodingIsDeterministic(t *testing.T) {
	// Source copied from TestProgramGobRoundTripPreservesNodeSemMapping: known
	// to parse cleanly AND to produce annotations and index proofs, so the
	// symbol-table maps below are populated by a real parse rather than by the
	// test alone.
	src := "f = (a []i64, i i64) {\n" +
		"    n [24]i64\n" +
		"    #{index-out=zero}\n" +
		"    l = a[i]\n" +
		"    #{index-out=zero, overflow=wrap}\n" +
		"    n[l] = n[l] + 1\n" +
		"}\n"
	p := New(lexer.NewCached("det.no", src))
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	if prog.Sem == nil {
		t.Fatal("ParseProgram returned a program with a nil Sem side-table")
	}

	// Populate every map-bearing field with >= 2 entries. Keys are inserted in
	// non-sorted order on purpose: a sorted insertion order would let a
	// "happens to iterate in insertion order" implementation pass by luck.
	prog.Sem.SetVarType("b", "i64")
	prog.Sem.SetVarType("a", "str")
	prog.Sem.EnumVariants["E"] = []string{"x", "y"}
	prog.Sem.EnumVariants["D"] = []string{"p"}
	prog.Sem.DeclaredVars["z"] = true
	prog.Sem.DeclaredVars["a"] = false
	prog.Sem.SetEnumVariantPayload("E", "y", "i64")
	prog.Sem.SetEnumVariantPayload("E", "x", "str")
	prog.Sem.EnumVariantFields["E"] = map[string][]string{"y": {"i64"}, "x": {"str", "i64"}}
	prog.Sem.IfaceImpls["I"] = []string{"A", "B"}
	prog.Sem.IfaceImpls["J"] = []string{"C"}
	prog.Sem.SetFuncVarType("f", "q", "i64")
	prog.Sem.SetFuncVarType("f", "p", "str")
	prog.Sem.FuncDeclaredVars["f"] = map[string]bool{"q": true, "p": false}
	prog.Sem.SetIdxLocalType("f", "q", "[]i64")
	prog.Sem.SetIdxLocalType("f", "p", "[]str")
	// SetUnionMembers lazily initializes the map (a bare parse of this fixture
	// records no union aliases).
	prog.Sem.SetUnionMembers("num", []string{"i32", "i64", "f64"})
	prog.Sem.SetUnionMembers("ord", []string{"i64", "f64"})
	prog.BuiltinFuncNames = map[string]bool{"sqrt": true, "abs": true}

	// Since 2026-10-26 the envelope is the syntax tree and nothing else.
	cp := &CachedProgram{Prog: prog}

	// Non-vacuity #1: the maps really are populated.
	if len(prog.Sem.VarTypes) < 2 || len(prog.Sem.FuncVarTypes["f"]) < 2 ||
		len(prog.Sem.UnionMembers) < 2 || len(prog.BuiltinFuncNames) < 2 {
		t.Fatalf("fixture is vacuous: VarTypes=%d FuncVarTypes=%d UnionMembers=%d BuiltinFuncNames=%d",
			len(prog.Sem.VarTypes), len(prog.Sem.FuncVarTypes["f"]),
			len(prog.Sem.UnionMembers), len(prog.BuiltinFuncNames))
	}

	// Non-vacuity #2: no map may survive into the shadow. A raw map there is
	// encoded by gob in random order and the byte comparison below becomes luck.
	shadow := detCached{Prog: detFromProgram(prog)}
	if maps := shadowMapTypes(reflect.ValueOf(&shadow), map[uintptr]bool{}); len(maps) > 0 {
		names := make([]string, 0, len(maps))
		for m := range maps {
			names = append(names, m)
		}
		t.Fatalf("the deterministic shadow still contains %d map type(s): %v\n"+
			"A map reaching gob is encoded in randomized order — add it to the "+
			"matching det* shadow in astcache_det.go.", len(names), names)
	}

	var prev []byte
	for i := 0; i < 3; i++ {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if i == 0 {
			prev = buf.Bytes()
			continue
		}
		if !bytes.Equal(prev, buf.Bytes()) {
			t.Fatalf("CachedProgram encoding is not deterministic: run 0 produced %d bytes, "+
				"run %d produced %d bytes with different content", len(prev), i, buf.Len())
		}
	}
	if len(prev) == 0 {
		t.Fatal("encoded 0 bytes")
	}

	// And the round-trip must still work — determinism achieved by dropping
	// data would be worthless.
	var dcp CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(prev)).Decode(&dcp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dcp.Prog == nil {
		t.Fatal("decoded CachedProgram has a nil Program")
	}
	if !mapTypeEqual(dcp.Prog.Sem.VarTypes, prog.Sem.VarTypes) {
		t.Fatalf("Sem.VarTypes lost across the round-trip: %v", dcp.Prog.Sem.VarTypes)
	}
	if !mapSliceEqual(dcp.Prog.Sem.EnumVariants, prog.Sem.EnumVariants) {
		t.Fatalf("Sem.EnumVariants lost across the round-trip: %v", dcp.Prog.Sem.EnumVariants)
	}
	if !mapSliceEqual(dcp.Prog.Sem.UnionMembers, prog.Sem.UnionMembers) {
		t.Fatalf("Sem.UnionMembers lost across the round-trip: %v", dcp.Prog.Sem.UnionMembers)
	}
	if len(dcp.Prog.BuiltinFuncNames) != len(prog.BuiltinFuncNames) {
		t.Fatalf("BuiltinFuncNames lost across the round-trip: %v", dcp.Prog.BuiltinFuncNames)
	}
	// The derived fact tables are NOT on the wire — that is the point of the
	// collapse, and BuildSemViews is what brings them back.
	if got := dcp.Prog.TypeFacts().Len(); got != 0 {
		t.Fatalf("decoded blob carries %d type facts; CachedProgram must hold the syntax tree only", got)
	}
	if got := dcp.Prog.NodeFacts().Len(); got != 0 {
		t.Fatalf("decoded blob carries %d node facts; CachedProgram must hold the syntax tree only", got)
	}
}

// shadowMapTypes returns the set of map type strings reachable from v.
func shadowMapTypes(v reflect.Value, seen map[uintptr]bool) map[string]bool {
	out := map[string]bool{}
	var rec func(reflect.Value)
	rec = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				rec(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if p := v.Pointer(); seen[p] {
				return
			} else {
				seen[p] = true
			}
			rec(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				rec(v.Index(i))
			}
		case reflect.Map:
			out[v.Type().String()] = true
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				rec(v.Field(i))
			}
		}
	}
	rec(v)
	return out
}

// TestCachedProgramCarriesProgOnly pins the ENVELOPE's field set to exactly
// `{Prog}`.
//
// The architecture decision (2026-10-26, see semviews.go): the AST cache is
// *structured source code*. It may switch serialization format, it must not
// carry compiler-derived results (those change with the compiler, while the
// cache key only covers the source text + ASTVersion + salt), and it is
// intended to become a distribution format. So the envelope holds the tree and
// nothing else; TypeFacts / NodeFacts (and before them Anns) were removed and
// are rebuilt by BuildSemViews.
//
// This gate exists because that deletion is invisible to every behavioural test
// if it is ever reverted: re-adding a redundant field would keep the whole
// suite green (it would just be written twice and read from the copy), while
// re-introducing exactly the redundancy the refactor removed.
//
// It is written so it cannot pass vacuously:
//   - the expected field list is EXACT, so both an addition and a rename fail;
//   - it asserts the walk actually saw fields, so a reflection bug that returns
//     nothing cannot masquerade as "no redundant field present";
//   - it checks the wire shadow (detCached) as well as the public envelope,
//     because a field present only on the shadow still ships in the blob.
func TestCachedProgramCarriesProgOnly(t *testing.T) {
	want := []string{"Prog"}

	got := fieldNames(reflect.TypeOf(CachedProgram{}))
	if len(got) == 0 {
		t.Fatal("reflection saw zero fields on CachedProgram — the gate would be vacuous")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CachedProgram fields = %v, want exactly %v\n"+
			"  A derived table must be REBUILT from Prog (see BuildSemViews), not\n"+
			"  stored: the blob is structured source, so it must not carry results\n"+
			"  that vary with the compiler build.", got, want)
	}

	shadowFields := fieldNames(reflect.TypeOf(detCached{}))
	if len(shadowFields) == 0 {
		t.Fatal("reflection saw zero fields on detCached — the gate would be vacuous")
	}
	if !reflect.DeepEqual(shadowFields, want) {
		t.Errorf("detCached fields = %v, want exactly %v\n"+
			"  detCached is what actually hits the wire; a field that exists only\n"+
			"  there is just as redundant as one on the envelope.", shadowFields, want)
	}
}

// TestDetSemCoversEverySemMapField is the structural half of the determinism
// gate: every exported map-typed field on SemanticContext must appear in the
// detSem shadow, otherwise gob encodes it directly and in randomized order.
//
// `ExternFuncSigs` / `ExternMethodSigs` are the ONLY allowed exceptions, and
// for a different reason than determinism: they are injected from outside the
// file (cross-module / std signature tables), so putting them in a
// content-addressed blob would bake caller state into a source artifact. They
// are restored by SemRebuildConfig.Apply during BuildSemViews.
func TestDetSemCoversEverySemMapField(t *testing.T) {
	shadowed := map[string]bool{}
	for _, n := range fieldNames(reflect.TypeOf(detSem{})) {
		shadowed[n] = true
	}
	if len(shadowed) < 5 {
		t.Fatalf("detSem has only %d fields — the gate would be vacuous", len(shadowed))
	}

	injected := map[string]bool{"ExternFuncSigs": true, "ExternMethodSigs": true}

	typ := reflect.TypeOf(SemanticContext{})
	checked := 0
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" || f.Type.Kind() != reflect.Map {
			continue
		}
		checked++
		if shadowed[f.Name] || injected[f.Name] {
			continue
		}
		t.Errorf("SemanticContext.%s is a map with no detSem shadow: gob would encode "+
			"it in randomized order and break byte-reproducibility. Add it to detSem "+
			"(and detFromSem/detToSem) in astcache_det.go, or to the injected list "+
			"above if it must not be in the blob at all.", f.Name)
	}
	if checked == 0 {
		t.Fatal("reflection saw no map fields on SemanticContext — the gate would be vacuous")
	}
}

// fieldNames returns the exported field names of a struct type, in declaration
// order. Unexported fields are skipped because gob drops them — they cannot be
// part of the wire layout this gate protects.
func fieldNames(typ reflect.Type) []string {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue
		}
		out = append(out, f.Name)
	}
	return out
}
