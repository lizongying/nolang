package parser

import "testing"

// selfMethodDef builds the AST shape the monomorphizer emits for
// `[]t.join`: a FunctionDefinition whose NAME is mangled (`_xtxt.join`)
// but whose synthetic `self` result parameter still carries the real
// receiver type (`[]txt`). The body contains the source-level `parts = .`
// (parsed as Identifier("self")).
func selfMethodDef(name string, selfType Type, withSelf bool) *FunctionDefinition {
	var results []*Parameter
	if withSelf {
		results = append(results, &Parameter{Name: "self", Type: selfType})
	}
	return &FunctionDefinition{
		Name:          name,
		IsMethodDef:   true,
		FuncSignature: FuncSignature{Results: results},
		Body: &BlockStatement{Statements: []Statement{
			&LetStatement{
				Name:  &Identifier{Value: "parts"},
				Value: &Identifier{Value: "self"},
			},
		}},
	}
}

func sliceOf(elem string) Type {
	return &SliceType{Elem: &NamedType{Value: elem}}
}

func TestIdxLocalTypeUsesSelfParamForMonomorphizedMethod(t *testing.T) {
	// `[]txt.join` is renamed to `_xtxt.join` by monomorphizeGenerics.
	// Deriving the receiver from fd.Name gives `_xtxt`, which
	// containerElemType cannot resolve, so `parts` was never registered
	// and `parts[i]: { ok -> ... }` inside the method lost its option
	// normalisation (tests/match-index-option.no printed "...").
	prog := &Program{
		Sem:        NewSemanticContext(),
		Statements: []Statement{selfMethodDef("_xtxt.join", sliceOf("txt"), true)},
	}
	p := &Parser{sem: prog.Sem}
	p.precomputeIdxLocalTypes(prog)

	got := ""
	if typ := p.idxLocalTypes["_xtxt.join"]["parts"]; typ != nil {
		got = typ.Raw()
	}
	if got != "[]txt" {
		t.Fatalf("monomorphized _xtxt.join: parts type = %q, want []txt (from the self parameter)", got)
	}
}

func TestIdxLocalTypeStillHandlesUnmangledTemplate(t *testing.T) {
	// Backward compatibility: the generic template itself has a
	// name-derived receiver that IS a container (`[]t`), and its self
	// parameter carries the same type. Both spellings must keep working.
	for _, fd := range []*FunctionDefinition{
		selfMethodDef("[]t.join", sliceOf("t"), true),
		selfMethodDef("[]t.join", sliceOf("t"), false), // legacy: no self param, name fallback
	} {
		prog := &Program{Sem: NewSemanticContext(), Statements: []Statement{fd}}
		p := &Parser{sem: prog.Sem}
		p.precomputeIdxLocalTypes(prog)
		got := ""
		if typ := p.idxLocalTypes["[]t.join"]["parts"]; typ != nil {
			got = typ.Raw()
		}
		if got != "[]t" {
			t.Errorf("[]t.join (withSelf=%v): parts type = %q, want []t", len(fd.Results) > 0, got)
		}
	}
}

func TestIdxLocalTypeMangledNameWithoutSelfStaysUnregistered(t *testing.T) {
	// Negative control: when the self parameter is missing AND the name is
	// mangled, the legacy fallback correctly refuses to invent a type.
	// This keeps the test above from passing for the wrong reason.
	prog := &Program{
		Sem:        NewSemanticContext(),
		Statements: []Statement{selfMethodDef("_xtxt.join", nil, false)},
	}
	p := &Parser{sem: prog.Sem}
	p.precomputeIdxLocalTypes(prog)
	if typ := p.idxLocalTypes["_xtxt.join"]["parts"]; typ != nil && typ.Raw() != "" {
		t.Fatalf("mangled name without self must not register parts, got %q", typ.Raw())
	}
}

// inferredSliceFunc builds the AST shape of
//
//	f = (IV []i64) {
//	    h = IV[..8)
//	}
//
// parseLetStatement deliberately leaves the LetStatement's Type nil for a
// SliceExpression, so the safe-index type table is the only place h can be
// recognised as a positional container.
func inferredSliceFunc(paramName string, paramType Type, binding string) *FunctionDefinition {
	return &FunctionDefinition{
		Name: "slice-write",
		FuncSignature: FuncSignature{
			Parameters: []*Parameter{{Name: paramName, Type: paramType}},
		},
		Body: &BlockStatement{Statements: []Statement{
			&LetStatement{
				Name:  &Identifier{Value: binding},
				Value: &SliceExpression{Left: &Identifier{Value: paramName}},
			},
		}},
	}
}

func TestIdxLocalTypeRegistersInferredSliceExpression(t *testing.T) {
	// tests/slice-option-write.no writes through `h[i]: { ok -> it = 999 -> _ }`.
	// Without an idxLocalTypes entry for h, resolveSubjectMatch leaves the
	// match unnormalised and the option place-write is silently dropped.
	prog := &Program{
		Sem:        NewSemanticContext(),
		Statements: []Statement{inferredSliceFunc("IV", sliceOf("i64"), "h")},
	}
	p := &Parser{sem: prog.Sem}
	p.precomputeIdxLocalTypes(prog)

	got := ""
	if typ := p.idxLocalTypes["slice-write"]["h"]; typ != nil {
		got = typ.Raw()
	}
	if got != "[]i64" {
		t.Fatalf("inferred slice h: idxLocalTypes = %q, want []i64", got)
	}
	// The fix is deliberately scoped to the safe-index side table: it must not
	// change the semantic variable type (which would alter codegen's slice-view
	// clone path).
	if got := p.sem.VarTypeRaw("h"); got != "" {
		t.Fatalf("inferred slice h must not be written to VarTypes, got %q", got)
	}
}

func TestIdxLocalTypeDoesNotTreatStrSliceAsPositionalContainer(t *testing.T) {
	// Negative control: slicing a str returns str, not []char. Registering h as
	// a positional container here would make safe-index lowering invent an
	// option<elem> type for a string slice.
	prog := &Program{
		Sem: NewSemanticContext(),
		Statements: []Statement{
			inferredSliceFunc("s", &NamedType{Value: "str"}, "h"),
		},
	}
	p := &Parser{sem: prog.Sem}
	p.precomputeIdxLocalTypes(prog)
	if typ := p.idxLocalTypes["slice-write"]["h"]; typ != nil && typ.Raw() != "" {
		t.Fatalf("str slice h must not be registered as a positional container, got %q", typ.Raw())
	}
}
