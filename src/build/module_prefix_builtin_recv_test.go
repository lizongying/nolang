package build

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// newMethodDef builds the minimal AST shape the prefix passes look at: a method
// definition plus its synthetic `self` result parameter (the receiver), exactly
// as parser.parseMethodDefinition produces it.
func newMethodDef(name, selfType string) *parser.FunctionDefinition {
	fd := &parser.FunctionDefinition{Name: name, IsMethodDef: true}
	fd.Results = []*parser.Parameter{
		{Name: "self", Type: &parser.NamedType{Value: selfType}},
	}
	return fd
}

func selfTypeOf(t *testing.T, fd *parser.FunctionDefinition) string {
	t.Helper()
	if len(fd.Results) == 0 {
		t.Fatalf("%s: no result parameters", fd.Name)
	}
	nt, ok := fd.Results[0].Type.(*parser.NamedType)
	if !ok {
		t.Fatalf("%s: self type is %T, want *parser.NamedType", fd.Name, fd.Results[0].Type)
	}
	return nt.Value
}

// TestPrefixModuleStatementsKeepsBuiltinReceiverMethods pins the fix for a bug
// where a std module whose name equals a BUILTIN type name renamed the builtin
// type's own layout declaration and, with it, the builtin type's own methods.
//
// std/str.no declares the layout of the builtin str as
//
//	#{buildin}
//	str { len u32; cap u32; data []byte }
//
// That declaration is DOCUMENTATION for the LSP: the real layout is the
// hand-written MIR descriptor. `prefixModuleStatements` used to treat it as
// "this module defines a struct named str", which cascaded into
//
//	Phase 2  str  -> str.str      (a shadow struct ⇒ %str_str)
//	Phase 3  str.copy -> str.str.copy, self typed str.str
//	Phase 4  type refs -> str.str
//
// Call sites stayed `str.copy` (prefixTypeName short-circuits builtin names),
// so the two signatures disagreed and MIR emitted
//
//	%lv = load %str_str
//	%cl = call %str-long @str_clone(%str-long %lv)
//
// which opt-verify rejects ("'%lv' defined with type '%str_str' but expected
// '%str-long'"). Every string interpolation goes through fmt-apply-spec →
// s.copy(), so even `print('half-{k}-tail')` failed to compile. The `txt`
// variant (`#{buildin} txt { data [255]byte; len u8 }`) failed earlier and more
// bluntly: `unknown callee txt.to-str`.
func TestPrefixModuleStatementsKeepsBuiltinReceiverMethods(t *testing.T) {
	structDef := &parser.StructDefinition{Name: "str", Builtin: true}
	method := newMethodDef("str.copy", "str")
	// A genuine, non-builtin type in the same module: its own method MUST still
	// be module-prefixed, otherwise this fix would disable method namespacing.
	otherStruct := &parser.StructDefinition{Name: "entry"}
	otherMethod := newMethodDef("entry.show", "entry")

	stmts := []parser.Statement{structDef, method, otherStruct, otherMethod}
	owner := map[string]string{}
	prefixModuleStatements(stmts, "str", owner)

	// The builtin receiver is untouched: name and self type.
	if method.Name != "str.copy" {
		t.Errorf("builtin receiver method renamed to %q, want %q", method.Name, "str.copy")
	}
	if got := selfTypeOf(t, method); got != "str" {
		t.Errorf("builtin receiver self type = %q, want %q", got, "str")
	}

	// Real module types keep the existing behaviour.
	if otherMethod.Name != "str.entry.show" {
		t.Errorf("module struct method = %q, want %q", otherMethod.Name, "str.entry.show")
	}
	if got := selfTypeOf(t, otherMethod); got != "str.entry" {
		t.Errorf("module struct self type = %q, want %q", got, "str.entry")
	}

	// The `#{buildin}` layout declaration stays on the bare builtin name: it is
	// not a type this module defines, so Phase 2 must neither rename it (that
	// produced the %str_str shadow struct) nor register it in typeOwner (that
	// is what made Phase 3/4 rewrite the real methods and type refs).
	if structDef.Name != "str" {
		t.Errorf("builtin layout struct = %q, want %q (it must stay on the bare "+
			"builtin name; the real layout is the MIR descriptor)", structDef.Name, "str")
	}
	if got, ok := owner["str.str"]; ok {
		t.Errorf("typeOwner[str.str] = %q, want no entry (the descriptor is not a "+
			"module type)", got)
	}
	// Sanity: the real struct is registered so later passes can resolve it.
	if got := owner["str.entry"]; got != "entry" {
		t.Errorf("typeOwner[str.entry] = %q, want %q", got, "entry")
	}
}

// TestPrefixModuleStatementsSkipsAllLayoutDecls is the same rule for the whole
// set. src/std carries four `#{buildin}` layout declarations today — str, vec,
// txt, arr — and each one failed differently when the prefix pass adopted it:
//
//	str  → %str_str + "'%lv' defined with type '%str_str' but expected '%str-long'"
//	txt  → %txt_txt + "unsupported receiver type %txt_txt"
//	vec  → %vec_vec (the hand-written %vec descriptor is 16 bytes: i32/i32/i64)
//	arr  → an %arr aggregate for a type whose layout is `[N x T]`, not a struct
//
// The assertion is uniform: name unchanged, no typeOwner entry, methods on that
// receiver untouched. Adding a fifth layout declaration to src/std must extend
// parser.IsBuiltinLayoutStructName or this test (and the compiler) will catch it.
func TestPrefixModuleStatementsSkipsAllLayoutDecls(t *testing.T) {
	for _, name := range []string{"str", "vec", "txt", "arr"} {
		t.Run(name, func(t *testing.T) {
			structDef := &parser.StructDefinition{Name: name, Builtin: true}
			method := newMethodDef(name+".probe", name)
			otherStruct := &parser.StructDefinition{Name: "entry"}
			otherMethod := newMethodDef("entry.show", "entry")

			owner := map[string]string{}
			prefixModuleStatements(
				[]parser.Statement{structDef, method, otherStruct, otherMethod}, "m", owner)

			if structDef.Name != name {
				t.Errorf("layout struct renamed to %q, want %q", structDef.Name, name)
			}
			if _, ok := owner["m."+name]; ok {
				t.Errorf("typeOwner[m.%s] present, want no entry", name)
			}
			if method.Name != name+".probe" {
				t.Errorf("method renamed to %q, want %q", method.Name, name+".probe")
			}
			if got := selfTypeOf(t, method); got != name {
				t.Errorf("self type = %q, want %q", got, name)
			}
			// The real module type in the same module is unaffected.
			if otherMethod.Name != "m.entry.show" {
				t.Errorf("module struct method = %q, want %q", otherMethod.Name, "m.entry.show")
			}
		})
	}
}

// TestPrefixModuleStatementsPrefersUnannotatedStruct pins the DISCRIMINATOR: the
// annotation, not the name. Two structs with the same shape, one carrying
// `#{buildin}` and one not, must take opposite paths — the annotated one stays
// bare (it is a layout declaration, not a type), the plain one is prefixed like
// any other module type.
//
// The `str`-with-annotation case is the std declaration; the plain case uses the
// same name on purpose. A user really writing `struct str` is rejected earlier
// by checker.ValidateBuiltinStructDecls (see builtin_struct_decl_test.go), which
// is why the prefix pass is allowed to leave such a struct's methods alone: the
// program never gets that far.
func TestPrefixModuleStatementsPrefersUnannotatedStruct(t *testing.T) {
	annotated := &parser.StructDefinition{Name: "str", Builtin: true}
	plain := &parser.StructDefinition{Name: "str"} // Builtin == false

	owner := map[string]string{}
	prefixModuleStatements([]parser.Statement{annotated}, "m", owner)
	if annotated.Name != "str" {
		t.Errorf("annotated struct = %q, want %q (a layout declaration is not a type "+
			"this module defines)", annotated.Name, "str")
	}
	if len(owner) != 0 {
		t.Errorf("typeOwner = %v, want empty (nothing to register)", owner)
	}

	owner = map[string]string{}
	prefixModuleStatements([]parser.Statement{plain}, "m", owner)
	if plain.Name != "m.str" {
		t.Errorf("unannotated struct = %q, want %q", plain.Name, "m.str")
	}
	if got := owner["m.str"]; got != "str" {
		t.Errorf("typeOwner[m.str] = %q, want %q", got, "str")
	}
}

// TestPrefixMethodNamesKeepsBuiltinReceiverMethods covers the second pass:
// after all modules are merged, prefixMethodNames resolves a method's receiver
// through typeOwner. A builtin receiver must never be resolved to a descriptor
// registered for std/str.no — that would produce `str.str.copy` (it did, before
// the fix) or `str.str.str.copy`, neither of which any call site names.
//
// Since the layout declaration no longer enters typeOwner at all, the owner map
// here only holds real module types: the guard is now pure defence in depth.
func TestPrefixMethodNamesKeepsBuiltinReceiverMethods(t *testing.T) {
	builtinRecv := newMethodDef("str.copy", "str")
	// Already carrying the descriptor prefix: the existing two-segment guard
	// must leave it alone instead of stacking a third `str.`.
	alreadyPrefixed := newMethodDef("str.str.copy", "str")

	owner := map[string]string{
		"sql.result": "result", // an ordinary module type, for contrast
	}
	prefixMethodNames([]parser.Statement{builtinRecv, alreadyPrefixed}, owner, map[string]bool{})

	if builtinRecv.Name != "str.copy" {
		t.Errorf("builtin receiver method renamed to %q, want %q", builtinRecv.Name, "str.copy")
	}
	if alreadyPrefixed.Name != "str.str.copy" {
		t.Errorf("already-prefixed method renamed to %q, want %q", alreadyPrefixed.Name, "str.str.copy")
	}
}
