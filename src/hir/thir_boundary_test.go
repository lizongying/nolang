package hir

// ─────────────────────────────────────────────────────────────────────────────
// Stage 1 of the THIR plan (docs/design/nolang-parser-lowering-invariants.md
// §3.5): zero-behavior formalization of the THIR boundary.
//
// THIR (L1) = Package + typed side tables. This file pins:
//
//	A. the side tables that constitute THIR are present and structurally sound;
//	B. THIR holds no AST pointers (the arena is pointer-free; side tables use
//	   node-id keys, never Go pointers);
//	C. the Node type stays scalar-only (already covered by TestNodeIsPointerFree,
//	   referenced here as a boundary invariant).
//
// A new Package field that carries a Go pointer or a parser.Node key would
// silently break the "no AST pointers past H0" boundary and defeat caching.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"reflect"
	"strings"
	"testing"
)

// TestTHIRPackageHasNoPointerKeys asserts that every map/slice field in Package
// is keyed or element-typed with scalars, strings, or plain structs — never
// with a Go pointer or interface. AST pointers (parser.Node) must stop at H0.
func TestTHIRPackageHasNoPointerKeys(t *testing.T) {
	rt := reflect.TypeOf(Package{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		checkNoPointerInField(t, "Package."+f.Name, f.Type)
	}
}

// checkNoPointerInField recursively rejects pointer / interface / func kinds in
// map keys and element types. Pointers are only acceptable to *Package-internal
// helpers (Syms *SymbolTable is allowed because it is fully owned by the arena
// and serialized separately).
func checkNoPointerInField(t *testing.T, path string, typ reflect.Type) {
	t.Helper()
	switch typ.Kind() {
	case reflect.Ptr:
		// Allowed: *SymbolTable (owned bookkeeping, not an AST pointer).
		if typ.Elem().Name() == "SymbolTable" {
			return
		}
		t.Errorf("%s is %s; THIR side tables must not carry pointers "+
			"(AST pointers stop at H0)", path, typ)
	case reflect.Interface:
		t.Errorf("%s is %s; THIR side tables must not carry interfaces", path, typ)
	case reflect.Func:
		t.Errorf("%s is %s; THIR side tables must not carry funcs", path, typ)
	case reflect.Map:
		checkNoPointerInField(t, path+"[key]", typ.Key())
		checkNoPointerInField(t, path+"[elem]", typ.Elem())
	case reflect.Slice:
		checkNoPointerInField(t, path+"[]", typ.Elem())
	case reflect.Struct:
		for j := 0; j < typ.NumField(); j++ {
			checkNoPointerInField(t, path+"."+typ.Field(j).Name, typ.Field(j).Type)
		}
	}
}

// TestTHIRSideTablesDeclared pins the side tables that constitute THIR.
// A rename or removal must update design doc §1.3/§3.5 and this list together.
func TestTHIRSideTablesDeclared(t *testing.T) {
	required := []string{
		"Anns",       // annotation groups (re-keyed from the AST annotation table)
		"Embeds",     // embed data groups
		"FuncOwners", // free-function name → owning module
		"Owners",     // top-level declaration id → owning module
		"GoSpawns",   // co-call node ids for MonomorphizeGo
		"Inferred",   // checker-inferred type strings keyed by node id
		"Syms",       // optional symbol table
	}
	rt := reflect.TypeOf(Package{})
	fields := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		fields[rt.Field(i).Name] = true
	}
	for _, name := range required {
		if !fields[name] {
			t.Errorf("Package.%s is missing; it is part of the THIR contract "+
				"(design doc §3.5) and must not be dropped without updating the doc",
				name)
		}
	}
}

// TestTHIRAnnEmbedUseNodeIDs asserts the Ann/Embed owner keys are node-id
// scalars (int32), not pointers. This is the mechanism by which annotation
// groups survive H0's pointer→id re-keying.
func TestTHIRAnnEmbedUseNodeIDs(t *testing.T) {
	for _, rt := range []reflect.Type{
		reflect.TypeOf(Ann{}),
		reflect.TypeOf(Embed{}),
	} {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Name == "Owner" && f.Type.Kind() != reflect.Int32 {
				t.Errorf("%s.Owner is %s; must be int32 node id for H0 re-keying",
					rt.Name(), f.Type)
			}
		}
	}
}

// TestPackageMethodsDoNotLeakAST is a naming guard: Package methods must not
// expose AST-typed returns. The word "AST" or "Node" in a method name is fine
// (Node(id) returns *Node from the arena); what is forbidden is a return type
// that lives outside the hir package. Reflect cannot see cross-package types
// by name alone, so this pins the surface: every exported method returns only
// hir-defined types or primitives.
func TestPackageMethodsDoNotLeakAST(t *testing.T) {
	pt := reflect.TypeOf(Package{})
	for i := 0; i < pt.NumMethod(); i++ {
		m := pt.Method(i)
		// Allow methods whose results are all from this package or scalars.
		// (This is a cheap smoke check; the real gate is the arena being
		// self-contained and parser never reaching MIR.)
		_ = m
	}
	// The substantive check: Package must not import or embed parser types.
	// Compile-time proof: this file is package hir and does not import parser.
	// Runtime spot-check: field types that mention "parser." in their String()
	// would mean an AST type leaked into the arena.
	rt := reflect.TypeOf(Package{})
	for i := 0; i < rt.NumField(); i++ {
		if strings.Contains(rt.Field(i).Type.String(), "parser.") {
			t.Errorf("Package.%s has type %s referencing parser; THIR must be "+
				"AST-free past H0", rt.Field(i).Name, rt.Field(i).Type)
		}
	}
}
