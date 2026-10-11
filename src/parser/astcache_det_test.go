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
//     structural guard against a new map field being added to one of the four
//     map-bearing types without a matching shadow, which would silently
//     reintroduce the nondeterminism this test exists to prevent.
func TestCachedProgramEncodingIsDeterministic(t *testing.T) {
	// Source copied from TestProgramGobRoundTripPreservesNodeSemMapping: known
	// to parse cleanly AND to produce annotations (hence a non-empty nodeSem,
	// so the map[uint32]*NodeSemantics path is exercised too).
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
	prog.Sem.VarTypes["b"] = "i64"
	prog.Sem.VarTypes["a"] = "str"
	prog.Sem.EnumVariants["E"] = []string{"x", "y"}
	prog.Sem.EnumVariants["D"] = []string{"p"}
	prog.Sem.DeclaredVars["z"] = true
	prog.Sem.DeclaredVars["a"] = false
	prog.Sem.EnumVariantPayload["E"] = map[string]string{"y": "i64", "x": "str"}
	prog.Sem.EnumVariantFields["E"] = map[string][]string{"y": {"i64"}, "x": {"str", "i64"}}
	prog.Sem.IfaceImpls["I"] = []string{"A", "B"}
	prog.Sem.IfaceImpls["J"] = []string{"C"}
	prog.Sem.FuncVarTypes["f"] = map[string]string{"q": "i64", "p": "str"}
	prog.Sem.FuncDeclaredVars["f"] = map[string]bool{"q": true, "p": false}
	prog.Sem.IdxLocalTypes["f"] = map[string]string{"q": "[]i64", "p": "[]str"}
	prog.BuiltinFuncNames = map[string]bool{"sqrt": true, "abs": true}

	cp := &CachedProgram{Prog: prog, NodeSem: SerializeNodeSem(prog)}
	if cp.NodeSem == nil {
		cp.NodeSem = map[uint32]*NodeSemantics{}
	}
	// NodeSemantics.EmbedFiles is the fourth map-bearing type; it is reached
	// through the side-table, so inject it explicitly.
	cp.NodeSem[1] = &NodeSemantics{EmbedFiles: map[string][]byte{"b.bin": {2}, "a.bin": {1}}}
	cp.NodeSem[2] = &NodeSemantics{PlatformKeys: []string{"mac-arm64"}}

	// Non-vacuity #1: the maps really are populated.
	if len(prog.Sem.VarTypes) < 2 || len(cp.NodeSem) < 2 {
		t.Fatalf("fixture is vacuous: VarTypes=%d NodeSem=%d", len(prog.Sem.VarTypes), len(cp.NodeSem))
	}

	// Non-vacuity #2: no map may survive into the shadow. A raw map there is
	// encoded by gob in random order and the byte comparison below becomes luck.
	shadow := detCached{Prog: detFromProgram(prog), NodeSem: detFromNodeSemMap(cp.NodeSem)}
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
	if !mapStrEqual(dcp.Prog.Sem.VarTypes, prog.Sem.VarTypes) {
		t.Fatalf("Sem.VarTypes lost across the round-trip: %v", dcp.Prog.Sem.VarTypes)
	}
	if !mapSliceEqual(dcp.Prog.Sem.EnumVariants, prog.Sem.EnumVariants) {
		t.Fatalf("Sem.EnumVariants lost across the round-trip: %v", dcp.Prog.Sem.EnumVariants)
	}
	if len(dcp.Prog.BuiltinFuncNames) != len(prog.BuiltinFuncNames) {
		t.Fatalf("BuiltinFuncNames lost across the round-trip: %v", dcp.Prog.BuiltinFuncNames)
	}
	if ns := dcp.NodeSem[1]; ns == nil || len(ns.EmbedFiles) != 2 {
		t.Fatalf("NodeSemantics.EmbedFiles lost across the round-trip: %+v", ns)
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
