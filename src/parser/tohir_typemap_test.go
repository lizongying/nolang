package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestPopulateInferredTypesKeyedByHIRID proves stage 3 of the HIR plan: the
// checker's in-place type inference (which mutates astNode.Type) can be lifted
// into a TypeMap keyed by HIR node id, with zero AST pointers in the HIR
// package. Codegen then reads pkg.InferredType(id) instead of the AST.
//
// It also proves the declared type and the inferred type live in SEPARATE
// slots: Node.Type holds the declared type (set at lowering), Inferred holds
// the checker's result. They must not collide.
func TestPopulateInferredTypesKeyedByHIRID(t *testing.T) {
	// One let with a declared type "i64"; the simulated checker infers "i32"
	// (a distinct value) so we can tell the two slots apart.
	src := "x i64 = 1\n"
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	pkg, idMap := ASTToHIRWithMap(prog)
	if pkg == nil || len(idMap) == 0 {
		t.Fatal("empty HIR package or id map")
	}

	// Find the let and confirm its declared type was lowered into Node.Type.
	var let *LetStatement
	for _, s := range prog.Statements {
		if ls, ok := s.(*LetStatement); ok {
			let = ls
			break
		}
	}
	if let == nil || let.Name == nil {
		t.Fatal("test setup: let not found")
	}
	letID := idMap[let]
	if got := pkg.Type(letID); got != "i64" {
		t.Fatalf("declared Node.Type = %q, want %q", got, "i64")
	}
	if got := pkg.InferredType(letID); got != "" {
		t.Fatalf("InferredType should be empty before PopulateInferredTypes, got %q", got)
	}

	// Simulate the checker having inferred "i32" into the AST node's Type field.
	let.Type = &NamedType{Value: "i32"}
	PopulateInferredTypes(pkg, idMap)

	if got := pkg.InferredType(letID); got != "i32" {
		t.Errorf("InferredType = %q, want %q", got, "i32")
	}
	// Declared type must be untouched by the inferred-type population.
	if got := pkg.Type(letID); got != "i64" {
		t.Errorf("declared Node.Type mutated to %q, want %q", got, "i64")
	}
}
