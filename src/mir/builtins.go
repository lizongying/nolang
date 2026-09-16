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
//
// The second return, exact, reports whether the match used the fully-qualified
// name. A fallback match (`fs.read-dir` -> bare `read-dir`) must NOT shadow a
// real Nolang function that happens to share the bare name: `fs.read-dir` is a
// module function, while the global builtin `read-dir` is a different thing.
// emitCall consults exact to prefer a real function over a fallback-shadowed
// builtin (see codegen.go emitCall).
func lookupBuiltin(callee string) (*builtin.BuiltinMethod, bool, bool) {
	if callee == "" {
		return nil, false, false
	}
	if bm := builtin.FindBuiltinMethod(callee); bm != nil {
		return bm, true, true
	}
	if i := strings.LastIndex(callee, "."); i >= 0 && i+1 < len(callee) {
		if bm := builtin.FindBuiltinMethod(callee[i+1:]); bm != nil {
			return bm, false, true
		}
	}
	return nil, false, false
}

// builtinResultTypes returns the nolang type string of EVERY declared result of
// a builtin, in declaration order.
//
// Multi-result builtins are far from exotic: `stat-size` yields (i64, bool),
// `readlink` yields (str, bool), `mkstemp` yields (str, fd). Lowering only the
// first (as builtinResultOf does) silently drops the second — `path, ok =
// fs.readlink(p)` would leave `ok` reading an uninitialized slot. It also
// starves the C-call emitter of the result count it needs to know how many
// destinations to fill.
func builtinResultTypes(callee string) []string {
	bm, _, ok := lookupBuiltin(callee)
	if !ok || len(bm.Return) == 0 {
		return nil
	}
	var out []string
	for _, r := range bm.Return {
		if r == nil {
			continue
		}
		raw := r.String()
		if raw == "" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

// builtinResultOf reports what a call to `callee` yields.
func builtinResultOf(callee string) builtinResult {
	bm, _, ok := lookupBuiltin(callee)
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

// scalarMethodResult returns the nolang type string of the result of a known
// scalar-type method whose definition lives in a std module that may not have
// been loaded into the HIR package. This mirrors the checker's special-case
// table (checker.go inferExprType: isValidationIntType → to-str/to-i64/...).
//
// Without this, when a scalar method like `i64.to-str()` is called but the
// defining module (e.g. number.no) was not auto-loaded, resultTypeOfCallee
// returns voidType and the call is mis-lowered as a void statement — the
// silent-undef bug #85.
//
// The table is intentionally narrow: only methods that (a) are defined in
// std modules loaded on-demand and (b) have a fixed, type-known result.
// Methods routed through the builtin table (bool.to-str, str.to-bool, ...)
// are already handled by builtinResultOf and need not appear here.
func scalarMethodResult(callee string) string {
	dot := strings.LastIndex(callee, ".")
	if dot < 0 {
		return ""
	}
	recv := callee[:dot]
	method := callee[dot+1:]
	if !isScalarNolangType(recv) {
		return ""
	}
	switch method {
	case "to-str":
		return "str"
	case "to-i64":
		return "i64"
	case "to-u64":
		return "u64"
	case "to-bool":
		return "bool"
	case "to-f64":
		return "f64"
	case "to-f32":
		return "f32"
	}
	return ""
}

// isScalarNolangType reports whether raw is a scalar nolang type that has
// std-defined methods (to-str, to-i64, ...).
func isScalarNolangType(raw string) bool {
	switch raw {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"byte", "char", "int", "uint",
		"f32", "f64":
		return true
	}
	return false
}
