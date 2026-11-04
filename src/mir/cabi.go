package mir

// C ABI export (`#{cabi}`) — the compiler-native way to expose Nolang
// functions to C / C++ / Rust / Go / Java / Python / C# / ...
//
// Nolang's own call ABI is `define void @f(args..., T* %out)`: inputs by value,
// result through a caller-supplied out-pointer. That shape is NOT a C ABI — no
// C caller can use it directly, and the symbol name is an internal sanitized
// one that may change when unrelated functions are added (collision suffixes).
//
// `#{cabi[='symbol']}` on a top-level function asks the compiler to:
//
//   1. keep the function alive even when NO Nolang code calls it (LowerHIR is
//      reachability-driven from the synthesized main);
//   2. emit a stable, normally-exported wrapper with a real return value;
//   3. emit a matching C header (`Module.CABIHeader`);
//   4. let `no build --shared` link the IR into a real dynamic library.
//
// v1 is deliberately SCALAR ONLY. str / vec / ?T / structs / pointers need a
// lifecycle ABI (who owns the allocation, who runs the runtime's init/drop
// epilogue) that a direct C call cannot provide yet; cabiTypeOf rejects them
// with a hard error instead of emitting a wrapper that would corrupt memory.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lizongying/nolang/hir"
)

// CABIAnnotationKey is the annotation key that marks an exported function.
// It is intentionally NOT an FFI language key (parser.IsFFILang): `#{c}` means
// "Nolang calls C", while `#{cabi}` means "C calls Nolang".
const CABIAnnotationKey = "cabi"

// CABIExport is one exported function. Symbol is the public C symbol name;
// FuncName is the raw (possibly module-qualified) HIR function name.
type CABIExport struct {
	Symbol   string
	FuncName string
}

var cabiSymbolRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// cabiReservedSymbols must never be claimed by an export: `main` is emitted by
// emitEntry (or by the host program) and `_nolang_main` is the renamed Nolang
// entry function. Redefining either in the same module is an LLVM error, and
// exporting a `main` from a dylib is a trap for `dlsym(RTLD_DEFAULT, "main")`.
var cabiReservedSymbols = map[string]bool{
	"main":         true,
	"_nolang_main": true,
}

// cabiType is one scalar value crossing the C ABI boundary.
//   - llvm: the type the INTERNAL Nolang function uses (i64, i1, double, ...)
//   - abi:  the type the WRAPPER uses (i1 is widened to i8, see below)
//   - c:    the C declaration type
//
// LLVM `i1` has no portable C representation: clang passes/returns it as an
// i8 with zeroext/signext attributes. Using `bool` in the header and `i1` in
// the wrapper signature is a mis-match waiting to happen, so v1 exposes a bool
// as `uint8_t` and inserts trunc/zext inside the wrapper.
type cabiType struct {
	llvm string
	abi  string
	c    string
	i1   bool
}

func cabiTypeOf(lt string, owned bool) (cabiType, bool) {
	if owned {
		return cabiType{}, false
	}
	switch lt {
	case "i64":
		return cabiType{llvm: "i64", abi: "i64", c: "int64_t"}, true
	case "i32":
		return cabiType{llvm: "i32", abi: "i32", c: "int32_t"}, true
	case "i8":
		return cabiType{llvm: "i8", abi: "i8", c: "int8_t"}, true
	case "double":
		return cabiType{llvm: "double", abi: "double", c: "double"}, true
	case "i1":
		return cabiType{llvm: "i1", abi: "i8", c: "uint8_t", i1: true}, true
	}
	// i128, %str-long, %vec, %option, %txt, pointers, structs, fixed arrays:
	// all outside v1's scalar-only contract.
	return cabiType{}, false
}

// cabiArg is one argument of the INTERNAL call, in f.Params order. Keeping the
// order (instead of assuming "out-param is last") matters because HIR children
// order is not guaranteed once methods / implicit receivers are involved.
type cabiArg struct {
	isResult bool
	paramIdx int // index into cabiSig.params; valid when !isResult
}

type cabiSig struct {
	symbol   string
	internal string // sanitized LLVM symbol of the Nolang function
	params   []cabiType
	result   cabiType
	args     []cabiArg
}

// cabiSignature resolves one export against the lowered MIR module. It is the
// SINGLE source of truth for both the wrapper and the header, so the two can
// never disagree about a C type.
func (c *codegen) cabiSignature(e CABIExport) (*cabiSig, error) {
	fid, ok := c.mod.FuncByName[e.FuncName]
	if !ok {
		return nil, fmt.Errorf("#{cabi} function %q was not lowered (internal error)", e.FuncName)
	}
	f := c.mod.Func(fid)
	if f == nil || f.IsExtern {
		return nil, fmt.Errorf("#{cabi} function %q must be a Nolang function definition, not an extern", e.FuncName)
	}
	if len(f.ResultParams) != 1 {
		return nil, fmt.Errorf("#{cabi} function %q needs exactly one result (got %d)", e.FuncName, len(f.ResultParams))
	}
	// ptype prefers the CURRENT function's LocalTypes. We are resolving a
	// different function here, so force the module-value fallback to avoid a
	// stale per-function type answer.
	savedCF := c.cf
	c.cf = NoFunc
	defer func() { c.cf = savedCF }()

	sig := &cabiSig{symbol: e.Symbol, internal: c.fname[f.ID]}
	rlt, rowned := c.ptype(f.ResultParams[0])
	rt, ok := cabiTypeOf(rlt, rowned)
	if !ok {
		return nil, fmt.Errorf("#{cabi} function %q: unsupported result type %q; only i64/i32/i8/bool/f64 scalars are exportable in v1", e.FuncName, rlt)
	}
	sig.result = rt

	isResult := make(map[ValueID]bool, len(f.ResultParams))
	for _, rp := range f.ResultParams {
		isResult[rp] = true
	}
	for _, p := range f.Params {
		if isResult[p] {
			sig.args = append(sig.args, cabiArg{isResult: true})
			continue
		}
		lt, owned := c.ptype(p)
		ct, ok := cabiTypeOf(lt, owned)
		if !ok {
			return nil, fmt.Errorf("#{cabi} function %q: unsupported parameter type %q; only i64/i32/i8/bool/f64 scalars are exportable in v1", e.FuncName, lt)
		}
		sig.args = append(sig.args, cabiArg{paramIdx: len(sig.params)})
		sig.params = append(sig.params, ct)
	}
	return sig, nil
}

// emitCABIWrappers appends one normal-return wrapper per export to
// extraFuncsBody (flushed at the end of EmitLLVM). usedFname is EmitLLVM's
// reserved-symbol set, so an export cannot silently redefine a runtime helper
// or another exported symbol.
func (c *codegen) emitCABIWrappers(usedFname map[string]bool) error {
	if len(c.mod.CABIExports) == 0 {
		return nil
	}
	for _, e := range c.mod.CABIExports {
		sig, err := c.cabiSignature(e)
		if err != nil {
			return err
		}
		if sig.internal == "" {
			return fmt.Errorf("#{cabi} function %q has no emitted LLVM symbol", e.FuncName)
		}
		if usedFname[e.Symbol] {
			return fmt.Errorf("#{cabi} symbol %q collides with an existing LLVM symbol; choose a different name", e.Symbol)
		}
		usedFname[e.Symbol] = true

		var b strings.Builder
		decls := make([]string, 0, len(sig.params))
		for i, p := range sig.params {
			decls = append(decls, fmt.Sprintf("%s %%a%d", p.abi, i))
		}
		args := make([]string, 0, len(sig.args))
		fmt.Fprintf(&b, "; #{cabi} export of %s\n", e.FuncName)
		fmt.Fprintf(&b, "define %s @%s(%s) {\nentry:\n",
			sig.result.abi, e.Symbol, strings.Join(decls, ", "))
		fmt.Fprintf(&b, "  %%cabi.r = alloca %s\n", sig.result.llvm)
		for i, a := range sig.args {
			if a.isResult {
				args = append(args, fmt.Sprintf("%s* %%cabi.r", sig.result.llvm))
				continue
			}
			p := sig.params[a.paramIdx]
			if p.i1 {
				fmt.Fprintf(&b, "  %%cabi.t%d = trunc i8 %%a%d to i1\n", i, a.paramIdx)
				args = append(args, fmt.Sprintf("i1 %%cabi.t%d", i))
			} else {
				args = append(args, fmt.Sprintf("%s %%a%d", p.abi, a.paramIdx))
			}
		}
		fmt.Fprintf(&b, "  call void @%s(%s)\n", sig.internal, strings.Join(args, ", "))
		fmt.Fprintf(&b, "  %%cabi.v = load %s, %s* %%cabi.r\n", sig.result.llvm, sig.result.llvm)
		if sig.result.i1 {
			b.WriteString("  %cabi.z = zext i1 %cabi.v to i8\n  ret i8 %cabi.z\n}\n")
		} else {
			fmt.Fprintf(&b, "  ret %s %%cabi.v\n}\n", sig.result.abi)
		}
		c.extraFuncsBody.WriteString(b.String())
	}
	return nil
}

// CABIHeader renders the C declarations matching the wrappers emitted by
// EmitLLVM. It is pure: the build layer calls it after a successful EmitLLVM
// and writes the result next to the shared library. An empty string means the
// program declared no exports.
func (m *Module) CABIHeader() (string, error) {
	if len(m.CABIExports) == 0 {
		return "", nil
	}
	c := &codegen{mod: m}
	var b strings.Builder
	b.WriteString("/* Generated by `no build --shared` — do not edit. */\n")
	b.WriteString("#ifndef NOLANG_CABI_H\n#define NOLANG_CABI_H\n\n")
	b.WriteString("#include <stdint.h>\n\n")
	b.WriteString("#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	for _, e := range m.CABIExports {
		sig, err := c.cabiSignature(e)
		if err != nil {
			return "", err
		}
		decls := make([]string, 0, len(sig.params))
		for i, p := range sig.params {
			decls = append(decls, fmt.Sprintf("%s a%d", p.c, i))
		}
		if len(decls) == 0 {
			decls = append(decls, "void")
		}
		fmt.Fprintf(&b, "%s %s(%s);\n", sig.result.c, e.Symbol, strings.Join(decls, ", "))
	}
	b.WriteString("\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* NOLANG_CABI_H */\n")
	return b.String(), nil
}

// collectCABIExports scans the merged HIR package for #{cabi} function
// definitions and records them as extra reachability roots. Called by
// LowerHIR once funcNames is complete and before the worklist is drained.
//
// Validation that can be done here (methods, result count, symbol syntax,
// duplicates) becomes a fatal LowerDiag with kind "cabi" — see
// build.firstFatalLowerDiag. Type-level validation lives in cabiSignature,
// where the authoritative LLVM types are available.
func (l *lowerer) collectCABIExports() []LowerDiag {
	var diags []LowerDiag
	seen := map[string]bool{}
	if l.cabiNames == nil {
		l.cabiNames = map[string]bool{}
	}
	fail := func(name, msg string) {
		diags = append(diags, LowerDiag{Func: name, Kind: "cabi", Msg: msg})
	}

	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KFuncDef {
			continue
		}
		present, sym := l.pkg.AnnotationString(id, CABIAnnotationKey)
		if !present {
			continue
		}
		name := l.pkg.Str(n.S)
		if n.Has(hir.FlagMethod) {
			fail(name, fmt.Sprintf("method %q cannot be exported; #{cabi} is only supported on top-level functions", name))
			continue
		}
		if sym == "" {
			sym = "nolang_" + sanitize(name)
		}
		if !cabiSymbolRE.MatchString(sym) {
			fail(name, fmt.Sprintf("C symbol %q is not a valid C identifier", sym))
			continue
		}
		if cabiReservedSymbols[sym] {
			fail(name, fmt.Sprintf("C symbol %q is reserved by the Nolang entry point", sym))
			continue
		}
		if seen[sym] {
			fail(name, fmt.Sprintf("duplicate C symbol %q", sym))
			continue
		}
		results := 0
		for _, ch := range l.pkg.Children(id) {
			cn := l.pkg.Node(ch)
			if cn != nil && cn.Kind == hir.KResult {
				results++
			}
		}
		if results != 1 {
			fail(name, fmt.Sprintf("#{cabi} function %q needs exactly one result (got %d)", name, results))
			continue
		}
		if _, ok := l.funcNames[name]; !ok {
			fail(name, fmt.Sprintf("#{cabi} function %q is not available on the target platform", name))
			continue
		}
		seen[sym] = true
		l.mod.CABIExports = append(l.mod.CABIExports, CABIExport{Symbol: sym, FuncName: name})
		l.cabiNames[name] = true
	}
	return diags
}
