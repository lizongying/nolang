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
// type's own methods.
//
// std/str.no declares the layout of the builtin str as
//
//	#{buildin}
//	str { len u32; cap u32; data []byte }
//
// `prefixModuleStatements` used to treat that declaration as "this module
// defines a struct named str" and therefore rewrote `str.copy` (whose receiver
// is the BUILTIN str, not that descriptor) into `str.str.copy`, with self typed
// `str.str`. Call sites stayed `str.copy` (prefixTypeName short-circuits builtin
// names), so the two signatures disagreed and MIR emitted
//
//	%lv = load %str_str
//	%cl = call %str-long @str_clone(%str-long %lv)
//
// which opt-verify rejects ("'%lv' defined with type '%str_str' but expected
// '%str-long'"). Every string interpolation goes through fmt-apply-spec →
// s.copy(), so even `print('half-{k}-tail')` failed to compile.
func TestPrefixModuleStatementsKeepsBuiltinReceiverMethods(t *testing.T) {
	structDef := &parser.StructDefinition{Name: "str"}
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

	// The builtin descriptor struct is still renamed away from the bare builtin
	// name: MIR registers its own `str` descriptor by hand (with `data` tagged
	// Inline, so the drop walk stops there) and a std struct reusing the bare
	// name would overwrite it.
	if structDef.Name != "str.str" {
		t.Errorf("builtin descriptor struct = %q, want %q (it must not shadow the "+
			"MIR-registered descriptor)", structDef.Name, "str.str")
	}
	// Sanity: the real struct is registered so later passes can resolve it.
	if got := owner["str.entry"]; got != "entry" {
		t.Errorf("typeOwner[str.entry] = %q, want %q", got, "entry")
	}
}

// TestPrefixMethodNamesKeepsBuiltinReceiverMethods covers the second pass:
// after all modules are merged, prefixMethodNames resolves a method's receiver
// through typeOwner. A builtin receiver must never be resolved to the
// `str.str` descriptor that Phase 2 registered for std/str.no — that would
// produce `str.str.str.copy` (and, before the fix, `str.str.copy`), neither of
// which any call site names.
func TestPrefixMethodNamesKeepsBuiltinReceiverMethods(t *testing.T) {
	builtinRecv := newMethodDef("str.copy", "str")
	// Already carrying the descriptor prefix: the existing two-segment guard
	// must leave it alone instead of stacking a third `str.`.
	alreadyPrefixed := newMethodDef("str.str.copy", "str")

	owner := map[string]string{
		"str.str": "str", // the builtin descriptor declared in std/str.no
	}
	prefixMethodNames([]parser.Statement{builtinRecv, alreadyPrefixed}, owner, map[string]bool{})

	if builtinRecv.Name != "str.copy" {
		t.Errorf("builtin receiver method renamed to %q, want %q", builtinRecv.Name, "str.copy")
	}
	if alreadyPrefixed.Name != "str.str.copy" {
		t.Errorf("already-prefixed method renamed to %q, want %q", alreadyPrefixed.Name, "str.str.copy")
	}
}
