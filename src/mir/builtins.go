package mir

import (
	"strings"

	"github.com/lizongying/nolang/builtin"
)

// This file bridges the builtin signature table (src/builtin) into MIR.
//
// Why it is needed: a Nolang call may target a builtin (print, sqrt, with-len,
// str.to-bytes, ...) that has NO KFuncDef in the HIR package. Lowering used to
// treat every unknown callee as a void call, which meant
//
//	x []byte = with-len(32)
//
// produced no destination value, so `x` was never bound in l.locals and EVERY
// later read of `x` cascaded into an "unresolved identifier" lowering gap. On
// the corpus that single mistake accounted for the overwhelming majority of all
// reported gaps. Consulting the real signature table fixes the whole class.
//
// Dependency direction: builtin imports only parser, so mir -> builtin adds no
// cycle and keeps mir free of build/llvm (it stays independently testable).

// lhsInferredBuiltins are builtins whose result type is NOT declared in the
// signature table because it is inferred from the assignment's left-hand side:
//
//	v []i64 = with-len(10)     -> vec with len=10, cap=10
//	s str   = with-cap(256)    -> str-long with cap=256
//
// For these the declared Return list is empty, so lowering falls back to the
// KLet's declared type (see lowerer.typeHint).
var lhsInferredBuiltins = map[string]bool{
	"with-len":       true,
	"with-cap":       true,
	"with-cap-len":   true,
	"vec.with-len":   true,
	"vec.with-cap":   true,
	"vec.with-len-cap": true,
}

// builtinResult describes what a builtin call produces.
type builtinResult struct {
	Known       bool   // the callee is a builtin at all
	Raw         string // nolang type string of the result; "" when void
	LHSInferred bool   // result type comes from the assignment LHS
}

// lookupBuiltin resolves a callee name against the builtin table.
//
// Naming in the table is inconsistent by historical accident: methods are
// registered both qualified ("str.eq") and bare ("len", with a ReceiverType).
// Lowering always builds the qualified form (`recvType.method`), so we try the
// exact name first and then fall back to the bare method name.
func lookupBuiltin(callee string) (*builtin.BuiltinMethod, bool) {
	if callee == "" {
		return nil, false
	}
	if bm := builtin.FindBuiltinMethod(callee); bm != nil {
		return bm, true
	}
	if i := strings.LastIndex(callee, "."); i >= 0 && i+1 < len(callee) {
		if bm := builtin.FindBuiltinMethod(callee[i+1:]); bm != nil {
			return bm, true
		}
	}
	return nil, false
}

// builtinResultOf reports what a call to `callee` yields.
func builtinResultOf(callee string) builtinResult {
	bm, ok := lookupBuiltin(callee)
	if !ok {
		return builtinResult{}
	}
	if len(bm.Return) > 0 && bm.Return[0] != nil {
		if raw := bm.Return[0].String(); raw != "" && raw != "void" {
			return builtinResult{Known: true, Raw: raw}
		}
	}
	if lhsInferredBuiltins[callee] {
		return builtinResult{Known: true, LHSInferred: true}
	}
	return builtinResult{Known: true} // known builtin, void result
}
