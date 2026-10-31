package js

import (
	"strconv"
	"strings"

	"github.com/lizongying/nolang/parser"

	"github.com/lizongying/nolang/i18n"
)

// Native package calls for the JS backend.
//
// A Nolang program can call into a native package three ways, decided by an
// annotation on the import line:
//
//	#{bind = ffi}   # /src/sqlite     — load a native shared library with koffi
//	                              (Node/Bun/Deno only; needs #{c} externs)
//	#{bind = wasm, wasm = 'pkg/wasm'}
//	                # /src/sqlite   — import a .wasm module and forward the
//	                              #{c} externs by same name
//	#{bind = auto}  # /src/sqlite   — browser ⇒ wasm; node ⇒ ffi if the program
//	                              declares #{c} externs, else wasm
//
// Browser targets force wasm (koffi cannot run in a browser), so an explicit
// bind=ffi on a browser build is a hard error.
//
// Design note — the policy is *program-level*, not per-import. After module
// merge the #{c} externs from an imported native package are flattened into the
// main program and lose their "belongs to module X" association, so a single
// resolved BindMode drives the whole emit. The originating import path(s) are
// kept for diagnostics only.

// BindMode is the native-call strategy chosen for the program.
type BindMode string

const (
	// BindAuto resolves to wasm in the browser and, on node, to ffi when the
	// program declares #{c} externs, otherwise to wasm.
	BindAuto BindMode = "auto"
	// BindFFI loads a native shared library via koffi (node/bun only).
	BindFFI BindMode = "ffi"
	// BindWASM imports a .wasm module and forwards externs by same name.
	BindWASM BindMode = "wasm"
)

// NativePolicy is the resolved native-call configuration for a program.
type NativePolicy struct {
	Mode    BindMode // resolved strategy (never BindAuto after Resolve)
	WASM    string   // wasm import specifier (wasm mode)
	Init    string   // wasm init/export name; defaults to "default"
	Lib     string   // FFI library name override (from lib=); empty ⇒ link-libs
	UsePath string   // originating import path, for diagnostics
}

// IsNativeUse reports whether a use statement could be a native package import:
// it is simply anything that is not a third-party ESM/npm import. A native
// package is declared by a bind=/wasm=/lib= annotation on the import line;
// without one the import carries no native intent and is skipped downstream.
func IsNativeUse(use *parser.UseStatement) bool {
	if use == nil {
		return false
	}
	return !IsESMUse(use)
}

// extractNativeKeys reads the bind=/wasm=/lib=/init= annotation keys from a
// statement's semantic entries. It is shared by native import lines and by
// #{c} extern declarations (which may carry the policy directly).
func extractNativeKeys(entries []*parser.AnnotationEntry) (mode BindMode, wasm, lib, initNm string, explicit bool) {
	for _, e := range entries {
		switch e.Key {
		case "bind":
			explicit = true
			switch v := e.Value.(type) {
			case *parser.AnnotationIdentValue:
				mode = BindMode(v.Value)
			case *parser.AnnotationStringValue:
				mode = BindMode(v.Value)
			}
		case "wasm":
			if v, ok := e.Value.(*parser.AnnotationStringValue); ok {
				wasm = v.Value
			}
		case "lib":
			if v, ok := e.Value.(*parser.AnnotationStringValue); ok {
				lib = v.Value
			}
		case "init":
			if v, ok := e.Value.(*parser.AnnotationStringValue); ok {
				initNm = v.Value
			}
		}
	}
	return
}

// NativeUsePolicy extracts the per-import native policy from a use statement's
// annotations. The second return is false when the import carries no native
// intent (no bind=/wasm=/lib= annotation), so callers can skip it.
func NativeUsePolicy(use *parser.UseStatement, sem *parser.SemanticContext) (NativePolicy, bool) {
	if !IsNativeUse(use) || sem == nil {
		return NativePolicy{}, false
	}
	mode, wasm, lib, initNm, explicit := extractNativeKeys(sem.AnnotationsOf(use))
	if !explicit && wasm == "" && lib == "" {
		return NativePolicy{}, false
	}
	if mode == "" {
		mode = BindAuto
	}
	if initNm == "" {
		initNm = "default"
	}
	return NativePolicy{
		Mode:    mode,
		WASM:    wasm,
		Init:    initNm,
		Lib:     lib,
		UsePath: use.Path,
	}, true
}

// NativeExternPolicy extracts the native policy carried directly on a #{c}
// extern declaration (the "existing #{c}" annotation location from the plan).
// The second return is false when the extern carries no native keys.
func NativeExternPolicy(es *parser.ExternStatement, sem *parser.SemanticContext) (NativePolicy, bool) {
	if es == nil || sem == nil || !parser.IsFFILang(es.Lang) {
		return NativePolicy{}, false
	}
	mode, wasm, lib, initNm, explicit := extractNativeKeys(sem.AnnotationsOf(es))
	if !explicit && wasm == "" && lib == "" {
		return NativePolicy{}, false
	}
	if mode == "" {
		mode = BindAuto
	}
	if initNm == "" {
		initNm = "default"
	}
	path := ""
	if es.Name != nil {
		path = es.Name.Value
	}
	return NativePolicy{
		Mode:    mode,
		WASM:    wasm,
		Init:    initNm,
		Lib:     lib,
		UsePath: path,
	}, true
}

// hasFFIExterns reports whether the program declares any #{c}-style FFI
// extern (the signal that a native package intends the koffi path).
func hasFFIExterns(program *parser.Program) bool {
	if program == nil {
		return false
	}
	for _, stmt := range program.Statements {
		es, ok := stmt.(*parser.ExternStatement)
		if !ok || es.Lang == "" {
			continue
		}
		if parser.IsFFILang(es.Lang) {
			return true
		}
	}
	return false
}

// CollectNativePolicy aggregates every native import's policy into one
// program-level policy, rejecting conflicting explicit modes or wasm
// specifiers. It returns (policy, found, error). found is false when no import
// or #{c} extern declares native intent (the caller then emits no native
// machinery — byte-for-byte identical to the pre-feature output).
func CollectNativePolicy(program *parser.Program) (NativePolicy, bool, error) {
	if program == nil || program.Sem == nil {
		return NativePolicy{}, false, nil
	}
	return CollectNativePolicyStmts(program.Sem, program.Statements)
}

// CollectNativePolicyStmts is CollectNativePolicy over an explicit statement
// list (the JS backend filters by platform before collecting).
func CollectNativePolicyStmts(sem *parser.SemanticContext, stmts []parser.Statement) (NativePolicy, bool, error) {
	var (
		merged       NativePolicy
		seenExplicit bool
		seenWASM     bool
		seenLib      bool
		seenNative   bool
	)
	merged.Mode = BindAuto
	add := func(p NativePolicy) error {
		if p.Mode != BindAuto {
			if seenExplicit && p.Mode != merged.Mode {
				return i18n.Errorf("zvybywvf",
					"conflicting native bind modes: %q (from %q) and %q (from %q)",
					merged.Mode, merged.UsePath, p.Mode, p.UsePath)
			}
			merged.Mode = p.Mode
			seenExplicit = true
		}
		if p.WASM != "" {
			if seenWASM && merged.WASM != "" && merged.WASM != p.WASM {
				return i18n.Errorf("e9kbpohv",
					"conflicting native wasm specifiers: %q (from %q) and %q (from %q)",
					merged.WASM, merged.UsePath, p.WASM, p.UsePath)
			}
			merged.WASM = p.WASM
			seenWASM = true
		}
		if p.Lib != "" {
			if seenLib && merged.Lib != "" && merged.Lib != p.Lib {
				return i18n.Errorf("6c1nlj7d",
					"conflicting native lib overrides: %q and %q", merged.Lib, p.Lib)
			}
			merged.Lib = p.Lib
			seenLib = true
		}
		if p.Init != "" {
			merged.Init = p.Init
		}
		merged.UsePath = p.UsePath
		return nil
	}
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.UseStatement:
			if p, has := NativeUsePolicy(s, sem); has {
				seenNative = true
				if err := add(p); err != nil {
					return NativePolicy{}, true, err
				}
			}
		case *parser.ExternStatement:
			if p, has := NativeExternPolicy(s, sem); has {
				seenNative = true
				if err := add(p); err != nil {
					return NativePolicy{}, true, err
				}
			}
		}
	}
	if !seenNative {
		return NativePolicy{}, false, nil
	}
	if merged.Init == "" {
		merged.Init = "default"
	}
	return merged, true, nil
}

// ResolveNativePolicy finalizes a collected policy against the target
// environment and the available link libraries. It:
//   - forces wasm in the browser and rejects an explicit bind=ffi there;
//   - resolves bind=auto: node + #{c} externs ⇒ ffi, otherwise wasm.
func ResolveNativePolicy(p NativePolicy, found, browser bool, linkLibs []string) (NativePolicy, error) {
	if !found {
		return NativePolicy{}, nil
	}
	if browser {
		if p.Mode == BindFFI {
			return NativePolicy{}, i18n.Errorf("rclf258h",
				"native bind=ffi is unavailable in the browser (import %q); use bind=wasm or omit bind",
				p.UsePath)
		}
		p.Mode = BindWASM
	}
	switch p.Mode {
	case BindAuto:
		if !browser && hasLinkTarget(linkLibs, p) {
			p.Mode = BindFFI
		} else {
			p.Mode = BindWASM
		}
	case BindFFI:
		// lib override takes precedence; otherwise require a link lib.
		if p.Lib == "" && len(linkLibs) == 0 {
			return NativePolicy{}, i18n.Errorf("n5k784wb",
				"native bind=ffi requires a link library (compiler.link-libs or lib= on import %q)",
				p.UsePath)
		}
	}
	// A resolved wasm binding must name a real module specifier. The import path
	// is a Nolang module path, never a JS module specifier, so an omitted wasm=
	// is a hard error rather than a silent guess.
	if p.Mode == BindWASM && p.WASM == "" {
		return NativePolicy{}, i18n.Errorf("jeq6vteu",
			"native bind=wasm requires a wasm= specifier (import %q), e.g. #{bind = wasm, wasm = '@scope/pkg'}",
			p.UsePath)
	}
	if p.Init == "" {
		p.Init = "default"
	}
	return p, nil
}

// hasLinkTarget reports whether an FFI binding has a concrete library to load:
// either an explicit lib= override or at least one compiler.link-libs entry.
func hasLinkTarget(linkLibs []string, p NativePolicy) bool {
	if p.Lib != "" {
		return true
	}
	return len(linkLibs) > 0
}

// DetectNativePolicy collects and resolves a program's native policy in one
// step, for callers outside the generator (build/runtime dependency checks,
// browser import map). It returns the resolved policy, whether any native
// intent was found, and any policy/conflict error.
func DetectNativePolicy(program *parser.Program, browser bool, linkLibs []string) (NativePolicy, bool, error) {
	p, found, err := CollectNativePolicy(program)
	if err != nil || !found {
		return NativePolicy{}, found, err
	}
	resolved, rerr := ResolveNativePolicy(p, true, browser, linkLibs)
	if rerr != nil {
		return NativePolicy{}, true, rerr
	}
	return resolved, true, nil
}

// NativeESMImports returns the synthetic ESM imports a program's native
// bindings require — the koffi package for FFI, or the wasm specifier for
// bind=wasm — resolved against the target environment. These are appended to a
// program's ordinary ESM imports so the {"type":"module"} sidecar, dependency
// checks and the browser import map all account for them. Nil when there is no
// native intent.
func NativeESMImports(program *parser.Program, browser bool, linkLibs []string) ([]ESMImport, error) {
	p, found, err := DetectNativePolicy(program, browser, linkLibs)
	if err != nil || !found {
		return nil, err
	}
	switch p.Mode {
	case BindFFI:
		return []ESMImport{{Specifier: koffiSpecifier, ShortName: koffiSpecifier, Namespace: true}}, nil
	case BindWASM:
		return []ESMImport{{
			Specifier: p.WASM,
			ShortName: esmShortName(p.UsePath),
			Namespace: true,
			Bind:      "wasm",
			Init:      p.Init,
		}}, nil
	}
	return nil, nil
}

// SetNativeConfig carries the build-time native configuration into the
// Generator: the FFI library names (compiler.link-libs) and whether the target
// is a browser. Policy collection and resolution happen in Generate (over the
// platform-filtered statements) so a #{js-browser}-only native import cannot
// influence a node build.
func (g *Generator) SetNativeConfig(linkLibs []string, browser bool) {
	g.nativeLibs = linkLibs
	g.nativeBrowser = browser
}

// ---- FFI emission (P2) ----

// koffiSpecifier is the npm package that provides the native FFI bridge. It is
// synthesized as an ESM import so the CJS-interop layer, the {"type":"module"}
// sidecar and dependency checks all reuse the existing third-party path.
const koffiSpecifier = "koffi"

// collectFFIExterns returns the program's #{c}-style extern declarations in
// source order (already platform-filtered) — these become the FFI bindings.
func collectFFIExterns(stmts []parser.Statement) []*parser.ExternStatement {
	var out []*parser.ExternStatement
	for _, s := range stmts {
		if es, ok := s.(*parser.ExternStatement); ok && es.Name != nil && parser.IsFFILang(es.Lang) {
			out = append(out, es)
		}
	}
	return out
}

// ffiSymbolName maps a Nolang FFI name to its C symbol: drop a leading '_' and
// turn '-' into '_' (so `_sqlite3-open` → `sqlite3_open`), matching the ABI
// the LLVM backend links against.
func ffiSymbolName(name string) string {
	return strings.ReplaceAll(strings.TrimPrefix(name, "_"), "-", "_")
}

// koffiScalar maps a Nolang scalar type name to a koffi type-string literal.
// See the plan's §5.3 mapping table.
func koffiScalar(name string) (string, bool) {
	switch name {
	case "i8":
		return `"int8"`, true
	case "u8", "byte":
		return `"uint8"`, true
	case "i16":
		return `"int16"`, true
	case "u16":
		return `"uint16"`, true
	case "i32":
		return `"int"`, true
	case "u32":
		return `"uint"`, true
	case "i64":
		return `"int64"`, true
	case "u64":
		return `"uint64"`, true
	case "f32":
		return `"float"`, true
	case "f64":
		return `"double"`, true
	case "bool":
		return `"bool"`, true
	case "str":
		return `"str"`, true
	}
	return "", false
}

// koffiType maps a Nolang FFI parameter/result type to the JS expression koffi
// expects in a func() signature: a quoted type-string for scalars,
// koffi.pointer(...) for pointers, and a "void *" fallback for composites
// (slices/arrays/structs) in v1.
func koffiType(t parser.Type) string {
	switch tt := t.(type) {
	case nil:
		return `"void"`
	case *parser.PointerType:
		return "koffi.pointer(" + koffiType(tt.Type) + ")"
	case *parser.NamedType:
		if s, ok := koffiScalar(tt.Value); ok {
			return s
		}
		return `"void *"`
	default:
		return `"void *"`
	}
}

// uniqueTaken returns base, suffixed with "$native", "$native2", … until it is
// not present in taken; the chosen name is then marked taken.
func uniqueTaken(base string, taken map[string]bool) string {
	candidate := base
	for i := 2; taken[candidate]; i++ {
		if i == 2 {
			candidate = base + "$native"
		} else {
			candidate = base + "$native" + strconv.Itoa(i)
		}
	}
	taken[candidate] = true
	return candidate
}

// koffiBinding synthesizes the koffi ESM import as an ESMBinding so it is
// emitted by the existing interop layer (import * as …; const X = ns.default
// ?? ns). The module object identifier is recorded in esmObjByShort afterwards.
func (g *Generator) koffiBinding(existing []ESMBinding, stmts []parser.Statement) ESMBinding {
	taken := g.collectTakenNames(stmts)
	for _, b := range existing {
		taken[b.ObjName] = true
		taken[b.NSName] = true
	}
	ns := uniqueTaken("__ns_koffi", taken)
	obj := uniqueTaken("koffi", taken)
	return ESMBinding{
		ESMImport: ESMImport{Specifier: koffiSpecifier, ShortName: koffiSpecifier, Namespace: true},
		NSName:    ns,
		ObjName:   obj,
		Locals:    map[string]string{},
	}
}

// emitFFIBindings writes the koffi loader, the multi-library __ffi_fn
// resolver and one binding per #{c} extern. It runs at phase 0d, after the
// ESM header, so the koffi module object is already in scope.
func (g *Generator) emitFFIBindings(externs []*parser.ExternStatement, policy NativePolicy) {
	if len(externs) == 0 {
		return
	}
	koffi := g.esmObjByShort[koffiSpecifier]
	if koffi == "" {
		koffi = "koffi"
	}
	libs := g.nativeLibs
	if policy.Lib != "" {
		libs = []string{policy.Lib}
	}
	g.writeRaw("// Nolang native FFI (koffi)\n")
	// The loader resolves a bare library name to its platform-specific file;
	// names that already carry a path or extension are used verbatim.
	g.writeRaw("function __ffi_load(libName) {\n")
	g.writeRaw("    const file = libName.indexOf('/') < 0 && libName.indexOf('.') < 0\n")
	g.writeRaw("        ? (process.platform === 'darwin' ? 'lib' + libName + '.dylib'\n")
	g.writeRaw("            : process.platform === 'win32' ? libName + '.dll'\n")
	g.writeRaw("            : 'lib' + libName + '.so')\n")
	g.writeRaw("        : libName;\n")
	g.writeRaw("    return koffi.load(file);\n")
	g.writeRaw("}\n")
	// Multiple libraries are tried in declaration order; the first that
	// resolves the symbol wins.
	g.writeRaw("const __ffi_libs = [")
	for i, lib := range libs {
		if i > 0 {
			g.writeRaw(", ")
		}
		g.writeRaw("__ffi_load(" + jsLiteral(lib) + ")")
	}
	g.writeRaw("];\n")
	g.writeRaw("function __ffi_fn(sym, ret, args) {\n")
	g.writeRaw("    for (const lib of __ffi_libs) {\n")
	g.writeRaw("        try {\n")
	g.writeRaw("            return lib.func(sym, ret, args);\n")
	g.writeRaw("        } catch (e) {\n")
	g.writeRaw("            if (!String(e).includes('not found')) throw e;\n")
	g.writeRaw("        }\n")
	g.writeRaw("    }\n")
	g.writeRaw("    throw new Error('FFI symbol not found: ' + sym);\n")
	g.writeRaw("}\n")
	for _, es := range externs {
		sym := ffiSymbolName(es.Name.Value)
		local := jsIdent(es.Name.Value)
		ret := `"void"`
		if len(es.Results) > 0 {
			ret = koffiType(es.Results[0].Type)
		}
		g.writeRaw("const " + local + " = __ffi_fn(" + jsLiteral(sym) + ", " + ret + ", [")
		for i, p := range es.Parameters {
			if i > 0 {
				g.writeRaw(", ")
			}
			g.writeRaw(koffiType(p.Type))
		}
		g.writeRaw("]);\n")
	}
	g.writeRaw("\n")
}

// ---- WASM emission (P3) ----

// wasmBinding synthesizes the native wasm module as an ESM binding with a
// `__wasm_ns_<short>` namespace, publishing the short name and init export on
// the Generator for the same-name forwarding pass.
func (g *Generator) wasmBinding(policy NativePolicy, existing []ESMBinding, stmts []parser.Statement) ESMBinding {
	taken := g.collectTakenNames(stmts)
	for _, b := range existing {
		taken[b.ObjName] = true
		taken[b.NSName] = true
	}
	short := esmShortName(policy.UsePath)
	ns := uniqueTaken("__wasm_ns_"+jsIdent(short), taken)
	init := policy.Init
	if init == "" {
		init = "default"
	}
	g.nativeWasmNS = ns
	g.nativeWasmName = short
	g.nativeWasmInit = init
	return ESMBinding{
		ESMImport: ESMImport{
			Specifier: policy.WASM,
			ShortName: short,
			Namespace: true,
			Bind:      "wasm",
			Init:      init,
		},
		NSName: ns,
		// Not used as a module-object binding (emitWASMImport emits the bootstrap
		// instead), but kept unique so it never collides with a real one.
		ObjName: uniqueTaken("__wasm_"+jsIdent(short), taken),
		Locals:  map[string]string{},
	}
}

// emitWASMImport writes the wasm bootstrap for a native wasm module: the import,
// the init factory, the shared ready-state object and the ready Promise. The
// Promise is not awaited; modules that initialize synchronously set the ready
// state immediately, async ones settle it and publish to
// globalThis.__nolang_wasm_ready[<short>].
func (g *Generator) emitWASMImport(b ESMBinding) {
	short := b.ShortName
	stateKey := jsLiteral(short)
	readyKey := jsLiteral(short)
	initProp := jsProp(b.Init)
	g.writeRaw("import * as " + b.NSName + " from " + jsLiteral(b.Specifier) + ";\n")
	g.writeRaw("const __wasm_init_" + jsIdent(short) + " = " + b.NSName + initProp + ";\n")
	g.writeRaw("const __wasm_state = globalThis.__nolang_wasm_state || (globalThis.__nolang_wasm_state = {});\n")
	g.writeRaw("const __wasm_ret_" + jsIdent(short) + " = __wasm_init_" + jsIdent(short) + "();\n")
	g.writeRaw("const __wasm_ready_" + jsIdent(short) + " =\n")
	g.writeRaw("    (__wasm_ret_" + jsIdent(short) + " && typeof __wasm_ret_" + jsIdent(short) + ".then === 'function')\n")
	g.writeRaw("        ? __wasm_ret_" + jsIdent(short) + ".then(() => { __wasm_state[" + stateKey + "] = true; })\n")
	g.writeRaw("        : (__wasm_state[" + stateKey + "] = true, Promise.resolve());\n")
	g.writeRaw("(globalThis.__nolang_wasm_ready || (globalThis.__nolang_wasm_ready = {}))[" + readyKey + "] = __wasm_ready_" + jsIdent(short) + ";\n")
}

// emitWASMBindings forwards each #{c} extern to the wasm module's same-named
// export, guarded by the ready state. The wasm export name is the C symbol name
// (leading '_' dropped, '-' → '_'), so `_sqlite3-open` → `sqlite3_open`.
func (g *Generator) emitWASMBindings(externs []*parser.ExternStatement) {
	if len(externs) == 0 {
		return
	}
	ns := g.nativeWasmNS
	if ns == "" {
		ns = "__wasm_ns_" + jsIdent(g.nativeWasmName)
	}
	short := g.nativeWasmName
	g.writeRaw("// Nolang native wasm (" + g.nativePolicy.WASM + ")\n")
	for _, es := range externs {
		export := ffiSymbolName(es.Name.Value)
		local := jsIdent(es.Name.Value)
		g.writeRaw("const " + local + " = (...args) => {\n")
		g.writeRaw("    if (!__wasm_state[" + jsLiteral(short) + "]) {\n")
		g.writeRaw("        throw new Error('WASM module " + short + " is not ready; await globalThis.__nolang_wasm_ready[" + jsLiteral(short) + "] before calling native functions');\n")
		g.writeRaw("    }\n")
		g.writeRaw("    return " + ns + "[" + jsLiteral(export) + "](...args);\n")
		g.writeRaw("};\n")
	}
	g.writeRaw("\n")
}
