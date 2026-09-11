package mir

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/hir"
)


// byteArrayRe matches a `[N]byte` / `[N]i8` nolang global type, used to fold
// module byte-array constants (SBOX, INV-SBOX) into compact c"..." data globals.
var byteArrayRe = regexp.MustCompile(`^\[(\d+)\](byte|i8)$`)

// formatFloat renders an f64 constant in a form LLVM's textual IR accepts.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Diagnostic (MIR-lowering-level) records a construct the current lowering subset
// does not yet handle. Unlike the memory Diagnostics in analysis.go, these are
// about coverage, not correctness; in the verification mode they are reported and
// the offending function falls back to the existing HIR path.
type LowerDiag struct {
	Func string
	Kind string
	Msg  string
}

type loopCtx struct {
	exit   BlockID
	update BlockID
}

type lowerer struct {
	pkg       *hir.Package
	mod       *Module
	b         *Builder
	funcNames map[string]int32 // nolang function name -> HIR node id
	worklist  []string
	lowered   map[string]bool
	locals    map[string]ValueID
	curFunc   FuncID
	voidType  TypeID
	curRecv   ValueID // current method's implicit `self` receiver value (first param)
	diags     []LowerDiag
	loopStack []loopCtx // active for-loops, for break/continue targets

	// contStack is the chain of "merge"/continuation blocks for enclosing
	// control-flow constructs (if/for). When an if's merge block ends up empty
	// (the if is the LAST statement inside its enclosing block — exactly what a
	// desugared match produces: nested if/elif/else), its then/else branches
	// must redirect to the enclosing continuation instead of being orphaned and
	// terminated with `ret void` by ensureReturn (which used to drop every
	// statement after a nested match arm). See lowerIf.
	contStack []BlockID

	// contTargets maps an `if`/`for` merge/continuation block to the block it
	// must fall through to when it ends up unterminated (i.e. its enclosing
	// block's continuation). This is set by lowerIf/lowerFor for their merge
	// block and consulted by ensureReturn so that a merge which is NOT the last
	// statement of its enclosing block (e.g. a then-only `if` followed by
	// unconditional statements in a loop body — binary `pow`) branches to the
	// loop continuation, while a merge that IS the last statement of a nested
	// match arm branches to the enclosing match arm's merge instead of being
	// patched with `ret void` (which used to drop every statement after a match
	// arm). Using a deferred map (rather than redirecting immediately) avoids
	// the timing bug where post-if statements are lowered AFTER the if returns
	// and would otherwise land in the wrong block.
	contTargets map[BlockID]BlockID

	// typeHint is the declared type of the binding currently being lowered.
	// Some builtins (with-len / with-cap / with-cap-len) declare an EMPTY
	// return list because their result type is inferred from the assignment's
	// left-hand side. Lowering consults this hint to give such a call a real
	// result type instead of treating it as void — otherwise the variable is
	// never bound and every later read of it becomes an "unresolved
	// identifier" gap. It is set for the duration of a KLet's value expression
	// and cleared immediately afterwards.
	typeHint TypeID

	// globals maps a package-level binding name to its MIR value. Top-level
	// `let`s and constants (SBOX, TLS-FINISHED-SIZE, ...) live outside every
	// function, so l.locals (per-function) never contains them; without this
	// table every reference from inside a function was an unresolved
	// identifier. Values are created lazily on first reference.
	globals map[string]ValueID

	// globalTypes records the declared/inferred nolang type string of each
	// top-level binding, so a lazily created global value gets the right type.
	globalTypes map[string]string

	// globalNodes records the HIR node id of each top-level KLet so the global's
	// constant initializer can be folded on first reference.
	globalNodes map[string]int32

	// enumVariants maps an enum type name (e.g. "code") to its variant names in
	// declaration order. Used to resolve enum-typed top-level `let`s and enum
	// variant references (`code.io`) to i64 discriminants, and to map enum types
	// to i64 in type resolution.
	enumVariants map[string][]string
}

// collectStructFields scans the HIR package for struct definitions and records
// their ordered fields into mod.StructFields (name + nolang type string), so
// OpGetField/OpSetField can resolve field indices and codegen can emit LLVM
// struct type declarations. Field order is the child order of KStructField
// nodes, which matches the GEP index order.
func (l *lowerer) collectStructFields() {
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KStructDef {
			continue
		}
		name := l.pkg.Str(n.S)
		if name == "" {
			continue
		}
		var fields []FieldInfo
		for c := n.First; c != hir.NoID; c = l.pkg.Node(c).Next {
			fn := l.pkg.Node(c)
			if fn == nil || fn.Kind != hir.KStructField {
				continue
			}
			fname := l.pkg.Str(fn.S)
			ftype := hir.FieldType(l.pkg, fn)
			if fname == "" {
				continue
			}
			fields = append(fields, FieldInfo{Name: fname, TypeRaw: ftype})
			// Make sure the field type is interned so codegen can resolve it.
			if ftype != "" {
				l.b.Type(ftype)
			}
		}
		if len(fields) > 0 {
			l.mod.StructFields[name] = fields
		}
	}
}

// synthesizeMainForTopLevel builds a synthetic `main` function that wraps the
// package's top-level statements (every pkg.Top node that is not a definition:
// KLet / KExprStmt / KFor / KIf / KBlock / KReturn / ...). Top-level-statement
// programs are the common Nolang form and have no explicit `fn main`; without
// this wrapper the MIR lowering has no entry point and used to fall back to
// picking a *random* function from funcNames as entry — producing
// non-deterministic, sometimes-wrong output. Wrapping the top-level statements
// makes the entry deterministic and correct.
//
// The wrapper is constructed directly in the HIR arena: a KFuncDef("main") whose
// only child is a KSlot("body") pointing at a KBlock whose children are the
// top-level statements in source order. lowerFunction handles this shape exactly
// like any user-defined function body, so all existing statement/expression
// lowerers apply unchanged.
func (l *lowerer) synthesizeMainForTopLevel(pkg *hir.Package) {
	if _, ok := l.funcNames["main"]; ok {
		return // explicit fn main already present; nothing to synthesize
	}
	var stmts []int32
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil {
			continue
		}
		switch n.Kind {
		case hir.KStructDef, hir.KFuncDef, hir.KExtern, hir.KEnumDef,
			hir.KTypeAlias, hir.KTaggedEnumDef, hir.KInterfaceDef,
			hir.KUse, hir.KExport, hir.KAnnotation:
			continue // definitions are not top-level statements
		case hir.KLet:
			// Module constants (FlagModuleConst) with a constant initializer
			// (int/float/bool/byte-array/fixed-array literal) are registered as
			// lazily-materialized globals by the registration loop above and must
			// NOT be inlined into the synthetic `main`. A FlagModuleConst `let`
			// whose initializer is NOT constant (e.g. `bad-fd fd = -1`,
			// `x = someFn()`) is NOT a real module constant — it must be inlined
			// as a script-local so it gets a runtime value. Skipping inlining
			// unconditionally used to leave such lets as unmaterialized global
			// markers -> every later read was `undef` (test-fd-newtype trapped at
			// `bad-fd < 0`).
			if n.Has(hir.FlagModuleConst) && l.foldConstText(n, l.letTypeRaw(n)) != "" {
				continue
			}
		raw := l.letTypeRaw(n)
		if raw != "" && l.foldConstText(n, raw) != "" {
			// A real COMPILE-TIME CONSTANT initializer. Scalars/enums/fixed
			// arrays fold to a genuine LLVM constant and stay module globals
			// (registered by the loop above). Slice/option/vec literals also
			// fold, but to a *fixed-array* text that disagrees with their
			// slice/option type, so they must NOT become module globals — they
			// are inlined below as locals (handled by the isUnsafeInlineType
			// branch), matching how a function-local `let v []i64 = [...]`
			// lowers to a real `%vec`. Skip the global keep for those.
			if l.isUnsafeInlineType(raw) {
				// fall through to inline as a local
			} else {
				continue
			}
		}
		// Inline top-level `let`s that are COMPUTED at runtime (call results,
		// arithmetic, negative literals like `bad-fd fd = -1`, slice/array/
		// option/vec literals, uninitialized containers, ...) into the
		// synthetic `main` as locals. A function-local `let` of any of these
		// types lowers correctly (e.g. `let v []i64 = [10,20,30]` becomes a
		// real `%vec` with len 3 and the right elements, and an uninitialized
		// `data []i64` zero-inits to an empty `%vec` that `.push` can extend),
		// so inlining them here is exactly what legacy's top-level-statement
		// lowering does. Keeping them as module globals used to void them
		// (test-oob-ok: `v[0]` -> "index slot: void"; test_vec_push_clear:
		// `data.push(10)` -> "vec.push: needs receiver"; test-vec-assign:
		// `block[i] = x` -> "indexstore slot"). Scalars, integer newtypes
		// (fd), user structs and enums also inline safely.
		if l.letValueIsCall(n) && raw != "" && raw != "void" {
			// Inline a CALL-result top-level `let` into the synthetic `main`
			// as a runtime local. A call result is emitted via EmitCallMulti
			// (which allocates the destination slot for ANY type, including
			// slice/option/vec), so it is always safe to inline regardless of
			// the declared type. The old guard `!isUnsafeInlineType(raw)`
			// wrongly skipped slice/option/vec call-valued lets (e.g.
			// `v = get-slice()` where get-slice returns `[]i64`), dropping them
			// entirely so every later read resolved to a void `const` and
			// `v[0]` indexed void -> "no slot" compile error (slice1). Only
			// CONSTANT initializers of those types must stay module globals
			// (handled by the branch below, which is reached only when the
			// value is NOT a call).
		} else if raw != "" && l.isUnsafeInlineType(raw) {
			// Slice/vec literal or uninitialized container: inline as a local
			// (NOT a module global). `isUnsafeInlineType` matches []T / vec /
			// %vec / %option (a `?T` top-level let is handled by the
			// isInlineableLetType path below, which wraps the scalar payload).
			// See the foldConstText branch above
			// and the registration loop — these would otherwise void.
		} else if raw != "" && !l.isInlineableLetType(raw) {
			// A non-inlineable, non-unsafe type. std prelude structs are only
			// emittable into LLVM when their field layout is registered in
			// StructFields (the prelude declares `%path_path = type { ... }`
			// for every such struct it actually uses). A struct that IS
			// registered (path.path, err.error, os.utsname, io.reader, ...)
			// is genuinely used by the script and lowers cleanly via
			// lowerStructLit, so fall through and inline it as a local. A
			// struct that is NOT registered (e.g. fs.file when its module's
			// layout is not collected) has NO emitted LLVM type — inlining it
			// would produce an undeclared `%fs_file` and malformed IR, so skip
			// it (it is a dead prelude init for the MIR path: print writes to
			// fd 1 directly and never references it). The old heuristic
			// skipped ALL namespaced types (`strings.Contains(raw,".")`),
			// which wrongly dropped `path.path` too — path.path is a scalar-
			// free struct the script constructs and calls methods on
			// (test_path_char: `p = path { p: "." }; p.path_dir()` trapped
			// because `p` read as a void `undef`).
			base := strings.TrimPrefix(raw, "%")
			if _, ok := l.mod.StructFields[base]; !ok {
				continue // unemittable std prelude struct: dead for MIR path
			}
		}
		// otherwise (scalar / newtype / user struct / enum): inline into main
		case hir.KStructLit:
			// Top-level struct literals are std prelude inits (e.g. fs.file
			// structlits for <stdin>/<stdout>/<stderr>). They reference struct
			// types the v1 codegen does not emit, so lowerStructLit would produce
			// an undeclared `%fs.file` type -> malformed IR -> whole-module
			// fallback. MIR's `print` writes directly to fd 1 and never references
			// these globals, so they are dead for the MIR path; skip them. Only
			// inlineable struct literals (e.g. txt) are kept.
			if !l.isInlineableLetType(l.pkg.Str(n.S)) {
				continue
			}
		}
		stmts = append(stmts, id)
	}
	// NOTE: an empty statement list must still synthesize `main`. A script whose
	// top level is nothing but definitions (or whose every top-level `let` is a
	// module constant, e.g. `a = 1`) has no runnable statements, yet the link
	// step still needs a `_main` symbol. Returning early here used to produce an
	// object file with no entry point at all -> `Undefined symbols: "_main"`.
	// The synthetic body is a bare block, and lowerFunction/ensureReturn give it
	// the `ret void` terminator it needs.

	// KBlock body node.
	bodyID := int32(len(pkg.Nodes))
	pkg.Nodes = append(pkg.Nodes, hir.Node{Kind: hir.KBlock, Id: bodyID})
	// Link the top-level statements as the block's children. We set Next
	// explicitly so we never accidentally inherit a prior sibling chain that
	// might pull a definition node into the body.
	for i, id := range stmts {
		if i == 0 {
			pkg.Nodes[bodyID].First = id
		} else {
			pkg.Nodes[stmts[i-1]].Next = id
		}
		if i == len(stmts)-1 {
			pkg.Nodes[id].Next = hir.NoID
		}
	}

	// KSlot("body") -> bodyID
	slotID := int32(len(pkg.Nodes))
	pkg.Nodes = append(pkg.Nodes, hir.Node{
		Kind: hir.KSlot, Id: slotID,
		S: internStrPkg(pkg, "body"), First: bodyID,
	})
	// KFuncDef("main") -> slotID
	mainID := int32(len(pkg.Nodes))
	pkg.Nodes = append(pkg.Nodes, hir.Node{
		Kind: hir.KFuncDef, Id: mainID,
		S: internStrPkg(pkg, "main"), First: slotID,
	})
	l.funcNames["main"] = mainID
}

// internStrPkg interns s into the package's string table, returning its id.
func internStrPkg(pkg *hir.Package, s string) int32 {
	for i, x := range pkg.Strings {
		if x == s {
			return int32(i)
		}
	}
	id := int32(len(pkg.Strings))
	pkg.Strings = append(pkg.Strings, s)
	return id
}

// LowerHIR lowers a HIR package into a MIR module. It is reachability-driven:
// starting from `main`, only the functions the user's code (transitively)
// references are lowered — exactly the "user HIR -> MIR; referenced std HIR ->
// MIR" plan. Unreferenced std functions stay out of the MIR (dead-code
// elimination). After lowering, Analyze runs to insert drops and run the
// memory checks. The returned Report is the memory-safety report; diags are
// lowering-coverage gaps.
func LowerHIR(pkg *hir.Package, enumVariants map[string][]string) (*Module, *Report, []LowerDiag) {
	l := &lowerer{
		pkg:       pkg,
		funcNames: map[string]int32{},
		lowered:   map[string]bool{},
		locals:    map[string]ValueID{},
		globals:   map[string]ValueID{},
		globalTypes: map[string]string{},
		globalNodes: map[string]int32{},
		enumVariants: enumVariants,
	}
	l.b = NewBuilder("hir")
	l.mod = l.b.Module()
	l.voidType = l.b.TypeExplicit("void", KindVoid, false, NoType)

	// Collect struct field layouts so OpGetField/OpSetField can resolve field
	// indices. Builtins have a fixed, known layout; user/std structs come from
	// HIR KStructDef nodes (field order = child order).
	l.mod.StructFields["str"] = []FieldInfo{{Name: "len", TypeRaw: "i64"}, {Name: "cap", TypeRaw: "i64"}, {Name: "data", TypeRaw: "str"}}
	l.mod.StructFields["vec"] = []FieldInfo{{Name: "len", TypeRaw: "i64"}, {Name: "cap", TypeRaw: "i64"}, {Name: "data", TypeRaw: "vec"}}
	// txt: fixed 256-byte stack buffer { [255 x i8] data, i8 len } (capped at
	// 255 bytes). Field order MUST match emitStructTypes' %txt layout so GEP
	// indices stay consistent: data = field 0, len = field 1.
	l.mod.StructFields["txt"] = []FieldInfo{{Name: "data", TypeRaw: "byte"}, {Name: "len", TypeRaw: "byte"}}
	l.collectStructFields()

	hasExplicitMain := false
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil {
			continue
		}
		switch n.Kind {
		case hir.KFuncDef, hir.KExtern:
			name := pkg.Str(n.S)
			if name == "main" {
				hasExplicitMain = true
			}
			l.funcNames[name] = id
		case hir.KLet:
			// A top-level `let` is registered as a lazily-materialized module
			// global when EITHER:
			//   (a) the package is a real module (explicit `fn main`), so every
			//       top-level `let` is a module-level binding (original behavior);
			//       OR
			//   (b) the `let` is a compile-time constant initializer (int/float/
			//       bool/byte-array/fixed-array literal) OR carries FlagModuleConst.
			//       This covers library-module constants whose initializer is a
			//       byte/fixed array (e.g. crypto/aes.SBOX = [256]byte{...}) which
			//       the FlagModuleConst flag does NOT mark (only scalar literals
			//       get that flag), yet which MUST resolve to a global rather than
			//       a void `const` placeholder. The legacy codegen likewise emits
			// every top-level constant `let` as a module global, so this matches
			// proven behavior.
			// Non-constant script top-level `let`s (e.g. `x = someFn()`) are NOT
			// registered here; synthesizeMainForTopLevel inlines the inlineable
			// ones as locals.
			// Register a top-level `let` as a module global ONLY when it is a
			// real compile-time constant. A FlagModuleConst `let` whose
			// initializer is NOT constant (e.g. `bad-fd fd = -1`, `x = someFn()`)
			// must NOT become an unmaterialized global marker — doing so leaves
			// every later read as `undef` and also blocks inlining. Such lets are
			// left for synthesizeMainForTopLevel, which inlines the inlineable
			// ones as locals (test-fd-newtype). For a real module (hasExplicitMain)
			// all top-level `let`s stay module-level globals, as before.
			if !hasExplicitMain {
				raw := l.letTypeRaw(n)
				if raw == "" || l.foldConstText(n, raw) == "" {
					continue
				}
				// A top-level slice/array/option/vec `let` in a SCRIPT (no
				// explicit `fn main`) is NOT a module-scope constant that must
				// outlive every function — its only consumer is the synthetic
				// `main`, which materializes it as a local (synthesizeMainForTop-
				// Level inlines it). Registering it here would emit a broken
				// `@name = global <slice-type> <fixed-array-constant>` whose type
				// and initializer disagree: foldConstText lowers a slice literal
				// (`v []i64 = [10,20,30]`) to a fixed array `[3 x i64]`, but the
				// binding type is a slice (`%vec`), so the LLVM verifier rejects
				// the module and every later read resolves to a void `const`
				// (test-oob-ok: `v[0]` -> "index slot: value ... void"). Skip the
				// global registration so it is inlined as a real local instead.
				if l.isUnsafeInlineType(raw) {
					continue
				}
			}
			gname := pkg.Str(n.S)
			if gname != "" {
				if dt := l.typeOfNode(n); dt != NoType && dt != l.voidType {
					l.globalTypes[gname] = l.mod.Types[dt].Raw
				}
				l.globals[gname] = NoVal // marker: declared, not yet materialized
				l.globalNodes[gname] = id
			}
		}
	}

	// Top-level-statement programs (the common Nolang form) have no explicit
	// `fn main`. Synthesize one that wraps the package's top-level statements so
	// the MIR entry is deterministic and correct; without this, LowerHIR fell
	// back to picking a *random* function from funcNames as entry, yielding
	// non-deterministic (sometimes-wrong) output.
	l.synthesizeMainForTopLevel(pkg)

	// Record every HIR function name so emitCall can prefer a real Nolang
	// function over a builtin matched only via the bare-name fallback
	// (e.g. module function `fs.read-dir` vs the global builtin `read-dir`).
	l.mod.KnownFuncs = make(map[string]bool, len(l.funcNames))
	for name := range l.funcNames {
		l.mod.KnownFuncs[name] = true
	}

	entry := "main"
	if _, ok := l.funcNames[entry]; !ok {
		for n := range l.funcNames {
			entry = n
			break
		}
	}
	if entry != "" {
		l.worklist = append(l.worklist, entry)
	}
	for len(l.worklist) > 0 {
		name := l.worklist[0]
		l.worklist = l.worklist[1:]
		if l.lowered[name] {
			continue
		}
		if id, ok := l.funcNames[name]; ok {
			l.lowerFunction(name, id)
		}
	}
	rep := l.mod.Analyze()
	return l.mod, rep, l.diags
}

// ---- HIR navigation helpers ----

func (l *lowerer) slot(id int32, label string) int32 {
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KSlot && l.pkg.Str(cn.S) == label {
			return cn.First
		}
	}
	return hir.NoID
}

func (l *lowerer) slotArgs(id int32, prefix string) []int32 {
	var out []int32
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KSlot {
			s := l.pkg.Str(cn.S)
			if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
				if cn.First != hir.NoID {
					out = append(out, cn.First)
				}
			}
		}
	}
	return out
}

func (l *lowerer) typeOfNode(n *hir.Node) TypeID {
	if n == nil {
		return l.voidType
	}
	// HIR literal/expression nodes carry no declared Type and InferredType is only
	// partially populated, so derive their types structurally. This is what keeps
	// integer literals/comparisons from collapsing to void (which previously
	// produced `alloca void` and broke the module).
	switch n.Kind {
	case hir.KLet:
		// A top-level `let` binding (`SBOX [256]byte = [...]`, perm-600, ...) —
		// derive its type from the declared annotation so module-level constants
		// in imported library modules register as lazily-materialized globals
		// (without this they resolve to void here and never become globals, so a
		// helper fn referencing them hits the unresolved-identifier placeholder).
		if raw := l.letTypeRaw(n); raw != "" && raw != "void" {
			return l.b.Type(raw)
		}
		return l.inferredTypeOf(n)
	case hir.KIntLit, hir.KCharLit, hir.KByteLit:
		return l.b.Type("i64")
	case hir.KFloatLit:
		return l.b.Type("f64")
	case hir.KBoolLit:
		return l.b.Type("bool")
	case hir.KStrLit:
		return l.b.Type("str")
	case hir.KInfix:
		if _, isCmp := infixOp(l.pkg.Str(n.S)); isCmp {
			return l.b.Type("bool")
		}
		if c := n.First; c != hir.NoID {
			if t := l.typeOfNode(l.pkg.Node(c)); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KPrefix:
		if l.pkg.Str(n.S) == "!" {
			return l.b.Type("bool")
		}
		if c := n.First; c != hir.NoID {
			if t := l.typeOfNode(l.pkg.Node(c)); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KGrouped:
		// Parenthesized expression: strip the grouping and resolve the inner
		// node's type (mirrors lowerExpr's KGrouped handling). Without this,
		// `(i+1) & 255` resolved the infix result type from the group wrapper,
		// which fell through to void and produced `and void`.
		if c := n.First; c != hir.NoID {
			if t := l.typeOfNode(l.pkg.Node(c)); t != l.voidType {
				return t
			}
		}
		for _, c := range l.pkg.Children(n.Id) {
			if t := l.typeOfNode(l.pkg.Node(c)); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KCast:
		// `(x as i64)` — derive the cast target from the node's declared/resolved
		// type before falling back to the source operand.
		raw := l.pkg.InferredType(n.Id)
		if raw == "" {
			raw = l.pkg.Type(n.Type)
		}
		if raw != "" && raw != "void" {
			return l.b.Type(raw)
		}
		if c := n.First; c != hir.NoID {
			if t := l.typeOfNode(l.pkg.Node(c)); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KIdent:
		if v, ok := l.locals[l.pkg.Str(n.S)]; ok {
			if f := l.mod.Func(l.curFunc); f != nil {
				if t, ok := f.LocalTypes[v]; ok {
					return t
				}
			}
		}
	case hir.KIndex:
		// array/vec/str element read `a[i]`. The element type is the indexed
		// value's element type (vec -> i64, [N]T -> T, str -> i8) — exactly what
		// lowerExpr's KIndex computes via elementTypeOf. Without this, arithmetic
		// on vec elements (e.g. fe-mul) resolved the infix result type from the
		// index read, which fell through to void and produced `add void`.
		var arrID int32
		for _, c := range l.pkg.Children(n.Id) {
			arrID = c
			break
		}
		if arrV := l.lowerExpr(arrID); arrV != NoVal {
			if t := l.elementTypeOf(arrV); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KSlice:
		// sub-slice `a[lo:hi]` yields the same (slice) type as its base.
		var arrID int32
		for _, c := range l.pkg.Children(n.Id) {
			arrID = c
			break
		}
		if arrV := l.lowerExpr(arrID); arrV != NoVal {
			if t := l.typeOfNode(l.pkg.Node(arrID)); t != l.voidType {
				return t
			}
		}
		return l.inferredTypeOf(n)
	case hir.KCall:
		// A call expression's type is the callee's (first) result type. Resolve
		// the callee name and look up its signature; nolang calls return a single
		// value (or the first of a multi-return), which is what the infix/operand
		// context needs.
		callee := ""
		for _, c := range l.pkg.Children(n.Id) {
			if cn := l.pkg.Node(c); cn != nil {
				callee = l.pkg.Str(cn.S)
			}
			break
		}
		if callee != "" {
			if cid, ok := l.mod.FuncByName[callee]; ok {
				if cf := l.mod.Func(cid); cf != nil && len(cf.Results) > 0 {
					return cf.Results[0]
				}
			}
		}
		return l.inferredTypeOf(n)
	case hir.KDot:
		// recv.field: derive the field type from the receiver's struct layout
		// (StructFields). Without this, `p.x + p.y` resolved the infix type from
		// the field-read operand (a KDot), which fell through to void and produced
		// `print unsupported arg type void`.
		fieldName := l.pkg.Str(n.S)
		var recvID int32
		for _, c := range l.pkg.Children(n.Id) {
			recvID = c
			break
		}
		recvT := l.typeOfNode(l.pkg.Node(recvID))
		if rt := l.mod.Type(recvT); rt != nil && rt.Raw != "" {
			if fields, ok := l.mod.StructFields[rt.Raw]; ok {
				for _, fld := range fields {
					if fld.Name == fieldName {
						return l.b.Type(fld.TypeRaw)
					}
				}
			}
		}
	}
	return l.inferredTypeOf(n)
}

// inferredTypeOf resolves a node's type from HIR-inferred/declared type info. It
// is the universal fallback used when structural derivation (typeOfNode of a
// child) yields void — without it, arithmetic on vec elements / indexes / other
// node kinds collapsed to void and produced `add void` / `and void` IR.
func (l *lowerer) inferredTypeOf(n *hir.Node) TypeID {
	raw := l.pkg.InferredType(n.Id)
	if raw == "" {
		raw = l.pkg.Type(n.Type)
	}
	if raw == "" || raw == "void" {
		return l.voidType
	}
	// Enum types lower to i64; map them explicitly so enum-typed values
	// (`mc = e.code()`, `code.io`) get a real scalar type instead of collapsing
	// to void.
	if l.isEnumTypeName(raw) {
		return l.b.Type("i64")
	}
	return l.b.Type(raw)
}

func (l *lowerer) unsupported(funcName, kind, msg string) {
	l.diags = append(l.diags, LowerDiag{Func: funcName, Kind: kind, Msg: msg})
}

// interpPreview returns a short, safe-to-log snippet of an interpolated string
// literal (the first ~40 runes around the first `{...}`), used in diagnostics
// so a failure is debuggable without dumping huge string constants.
func interpPreview(s string) string {
	i := strings.Index(s, "{")
	if i < 0 {
		return s
	}
	end := i + 40
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}

// valueRaw returns the nolang raw type string of a lowered value, or "" if the
// value is invalid/unknown. Used to special-case operations whose legality
// depends on the operand type (e.g. string equality must route through @str_eq
// rather than a direct icmp).
func (l *lowerer) valueRaw(v ValueID) string {
	if v <= NoVal {
		return ""
	}
	val := l.mod.Value(v)
	if val == nil {
		return ""
	}
	if t := l.mod.Type(val.Type); t != nil {
		return t.Raw
	}
	return ""
}

// isInlineableLetType reports whether a top-level `let` of the given nolang type
// can be lowered as an executable statement in the synthesized `main`. Types the
// v1 codegen cannot emit (structs, qualified std prelude names like fs.file) are
// excluded so they are not inlined into main (which would produce malformed IR).
func (l *lowerer) isInlineableLetType(raw string) bool {
	switch raw {
	case "i64", "i32", "i16", "i8", "u64", "u32", "u16", "u8", "byte",
		"bool", "f64", "f32", "double", "str", "txt":
		return true
	}
	if len(raw) > 0 && raw[0] == '[' {
		return true // fixed array
	}
	if len(raw) > 0 && raw[0] == '?' {
		return true // option (?i64, ?str, ?vec, ...) — elided to its inner value
	}
	// Registered struct types (user structs + std structs) lower cleanly as
	// inlineable top-level `let` statements: lowerStructLit allocates the local
	// and stores its field initializers, and any unused prelude struct (fs.file
	// for <stdin>/<stdout>/<stderr>) is dead code for the MIR path (print writes
	// to fd 1 directly). vec/option are excluded — their drop is a no-op and
	// their element codegen is incomplete in v1.
	if raw != "vec" && raw != "option" && raw != "%vec" && raw != "%option" {
		base := strings.TrimPrefix(raw, "%")
		if _, ok := l.mod.StructFields[base]; ok {
			// Std prelude structs are namespaced (fs.file, io.reader,
			// err.error, os.utsname, path.path). Their top-level inits
			// (<stdin> = fs.file{...}) carry EMPTY field names in HIR and are
			// dead for the MIR path, so skip them (they stay resolvable as
			// module globals). User structs are unqualified and inline normally.
			if strings.Contains(base, ".") {
				return false
			}
			return true
		}
	}
	// Enum types (`code`, `file-mode`, `file-perm`) lower to i64 and inline
	// cleanly into the synthetic `main`.
	if l.isEnumTypeName(raw) {
		return true
	}
	return false
}

// isEnumTypeName reports whether raw is an enum type name. It accepts both the
// bare name ("code") and a namespaced form ("err.code") by checking the last
// dot-separated segment, so `code` and `err.code`-annotated values both match.
func (l *lowerer) isEnumTypeName(raw string) bool {
	if raw == "" || len(l.enumVariants) == 0 {
		return false
	}
	if _, ok := l.enumVariants[raw]; ok {
		return true
	}
	if i := strings.LastIndex(raw, "."); i >= 0 {
		if _, ok := l.enumVariants[raw[i+1:]]; ok {
			return true
		}
	}
	return false
}

// enumVariantValue resolves an enum variant reference ("code.io") to its
// zero-based discriminant value. The receiver segment must be a known enum name.
func (l *lowerer) enumVariantValue(key string) (int64, bool) {
	if l.enumVariants == nil {
		return 0, false
	}
	if i := strings.LastIndex(key, "."); i >= 0 {
		enum := key[:i]
		variant := key[i+1:]
		if variants, ok := l.enumVariants[enum]; ok {
			for idx, v := range variants {
				if v == variant {
					return int64(idx), true
				}
			}
		}
	}
	return 0, false
}

// letValueIsCall reports whether a top-level KLet's value expression is a
// function call (rather than a literal/struct-literal initializer).
func (l *lowerer) letValueIsCall(n *hir.Node) bool {
	for _, c := range l.pkg.Children(n.Id) {
		cn := l.pkg.Node(c)
		return cn != nil && cn.Kind == hir.KCall
	}
	return false
}

// isUnsafeInlineType reports whether a top-level `let` of the given type should
// NOT be inlined into the synthetic `main` because the v1 codegen cannot emit it
// as a statement (vec/option/slice element codegen is incomplete). Scalars,
// structs and enums inline safely.
func (l *lowerer) isUnsafeInlineType(raw string) bool {
	switch {
	case raw == "vec", raw == "option", raw == "%vec", raw == "%option":
		return true
	case strings.HasPrefix(raw, "[]"), strings.HasPrefix(raw, "%vec"), strings.HasPrefix(raw, "%option"):
		return true
	}
	return false
}

// letTypeRaw recovers the static type of a top-level KLet so the synthesizer
// can decide whether to inline it into the synthetic `main`. n.Type / the
// inferred type cover most cases, but std prelude lets (e.g. <stdin> =
// fs.file{...}) carry NO recoverable type on the KLet node — only the
// value expression knows (a KStructLit whose n.S is the struct name, or a
// plain literal). Recovering it lets us skip non-inlineable prelude lets
// instead of inlining a malformed `%fs_file` into main.
func (l *lowerer) letTypeRaw(n *hir.Node) string {
	if raw := l.pkg.Type(n.Type); raw != "" {
		return raw
	}
	if raw := l.pkg.InferredType(n.Id); raw != "" {
		return raw
	}
	for _, c := range l.pkg.Children(n.Id) {
		cn := l.pkg.Node(c)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KStructLit:
			return l.pkg.Str(cn.S) // struct name, e.g. "fs.file"
		case hir.KStrLit, hir.KCharLit:
			return "str"
		case hir.KIntLit, hir.KByteLit:
			return "i64"
		case hir.KFloatLit:
			return "f64"
		case hir.KBoolLit:
			return "bool"
		}
	}
	return ""
}

// ---- function lowering ----

func (l *lowerer) lowerFunction(name string, hirID int32) {
	l.lowered[name] = true
	// Reset the deferred merge-continuation map for this function. Block IDs
	// are unique per module, but clearing avoids stale cross-function entries
	// and keeps ensureReturn's lookup scoped to the current function.
	l.contTargets = make(map[BlockID]BlockID)
	n := l.pkg.Node(hirID)

	var params []ValueID
	var paramNames []string
	var paramTypes []TypeID
	var results []TypeID
	var resultNames []string
	var resultVals []ValueID
	for _, c := range l.pkg.Children(hirID) {
		cn := l.pkg.Node(c)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			pt := l.typeOfNode(cn)
			pv := l.b.Param(l.pkg.Str(cn.S), pt)
			params = append(params, pv)
			paramNames = append(paramNames, l.pkg.Str(cn.S))
			paramTypes = append(paramTypes, pt)
		case hir.KResult:
			rt := l.typeOfNode(cn)
			rv := l.b.Param(l.pkg.Str(cn.S), rt)
			// The result parameter is a real LLVM parameter (the caller-passed
			// out-pointer), so it must appear in params as well as ResultParams.
			params = append(params, rv)
			results = append(results, rt)
			resultNames = append(resultNames, l.pkg.Str(cn.S))
			resultVals = append(resultVals, rv)
		}
	}
	isExtern := n.Kind == hir.KExtern
	fid := l.b.NewFunc(name, params, results, isExtern)
	l.curFunc = fid
	l.curRecv = NoVal
	// For a method, the receiver is the first parameter (the implicit `self` that
	// `.field` accesses resolve to when the KDot has no explicit receiver child).
	if n.Has(hir.FlagMethod) && len(params) > 0 {
		l.curRecv = params[0]
	}
	l.locals = map[string]ValueID{}
	for i, pn := range paramNames {
		l.locals[pn] = params[i]
	}
	f := l.mod.Func(fid)
	if f != nil {
		f.ResultParams = resultVals
		// Mark method functions so codegen passes the receiver by reference
		// (aliases the caller's self pointer instead of copying into a local
		// alloca). Without this, mutating std methods (vec.insert/remove/…)
		// silently discard self mutations under the MIR backend.
		if n.Has(hir.FlagMethod) && len(paramTypes) > 0 {
			f.IsMethod = true
			f.Receiver = paramTypes[0]
		}
	}
	for i, rn := range resultNames {
		l.locals[rn] = resultVals[i]
		if f != nil {
			f.LocalTypes[resultVals[i]] = results[i]
		}
	}

	// Zero-initialize scalar result parameters so a result that is only assigned
	// inside a conditional branch still holds its type's default (false/0/null)
	// on the fall-through path. Without this, `error.is` (which sets
	// `yes = true` only in the true branch) reads uninitialized memory in the
	// else path and can return the wrong answer (e.g. `e.is(code.not-found)`
	// reporting true).
	//
	// Only scalar kinds (int/bool/char/pointer) are safe to zero here with an
	// integer constant. Aggregate result types (str, struct, slice, array,
	// option, map) must keep the caller-allocated slot untouched — a "constant 0
	// of aggregate type" store would corrupt the returned value (see str.fields).
	for i := range resultNames {
		switch l.mod.Type(results[i]).Kind {
		case KindInt, KindBool, KindChar, KindPtr:
			zero := l.b.EmitInt(OpConst, results[i], 0, "")
			l.b.EmitMoveInto(resultVals[i], zero)
		}
	}

	bodyID := l.slot(hirID, "body")
	if bodyID != hir.NoID {
		l.lowerBlock(bodyID)
	}
	l.ensureReturn(fid)
}

func (l *lowerer) ensureReturn(fid FuncID) {
	f := l.mod.Func(fid)
	if f == nil {
		return
	}
	for _, bid := range f.Blocks {
		blk := l.mod.Block(bid)
		if blk == nil || blk.Term != nil {
			continue
		}
		// Auto-terminate every function body block that lacks an explicit
		// terminator. For functions with a result parameter (the Nolang
		// result-var convention) OpReturn writes the result param back to the
		// caller's out-pointer and then `ret void`; for void functions it is just
		// `ret void`. Extern functions have no body, so skip them.
		if f.IsExtern {
			continue
		}
		l.b.SetBlock(bid)
		if target, ok := l.contTargets[bid]; ok && target != NoBlock {
			// This merge/continuation block was registered by lowerIf/lowerFor
			// to fall through to the enclosing continuation (loop body -> update
			// block; nested match arm -> outer merge). Branch there instead of
			// returning, so control flow continues correctly past an `if` whose
			// merge is not the function's final block.
			l.b.Terminate(OpBr, nil, []BlockID{target}, "")
		} else {
			l.b.Terminate(OpReturn, nil, nil, "")
		}
	}
}

func (l *lowerer) lowerBlock(blockID int32) {
	for _, c := range l.pkg.Children(blockID) {
		l.lowerStmt(c)
	}
}

func (l *lowerer) curFuncName() string {
	if f := l.mod.Func(l.curFunc); f != nil {
		return f.Name
	}
	return ""
}

func (l *lowerer) lowerStmt(id int32) {
	n := l.pkg.Node(id)
	if n == nil {
		return
	}
	switch n.Kind {
	case hir.KLet:
		name := l.pkg.Str(n.S)
		var val ValueID = NoVal
		var childID int32
		// Publish the binding's declared type as a hint while lowering the
		// value expression. Builtins like with-len/with-cap declare an empty
		// return list (the result type comes from the LHS), so without this
		// hint their call lowers to void, the variable never gets bound, and
		// every later read of it cascades into "unresolved identifier".
		l.typeHint = l.typeOfNode(n)
		// A re-assignment to an already-declared binding (e.g. a result
		// parameter like `val = with-cap(.len)` inside a std method) carries
		// NO declared type on the KLet node. Fall back to the existing
		// local's type so the LHS-inferred builtin still gets a real result
		// type instead of lowering to void and dropping the assignment.
		if (l.typeHint == NoType || l.typeHint == l.voidType) && name != "" {
			if slot, ok := l.locals[name]; ok {
				if t := l.valueTypeOf(slot); t != NoType && t != l.voidType {
					l.typeHint = t
				}
			}
		}
	for _, c := range l.pkg.Children(id) {
		childID = c
		val = l.lowerExpr(c)
		break
	}
	l.typeHint = NoType
		if val == NoVal {
			// Declaration with NO initializer: `blk [16]byte` / `buf str`.
			// Nolang zero-initializes these. Lowering used to skip binding
			// entirely (there is no value to bind), so every later read of the
			// variable cascaded into "unresolved identifier" — one of the two
			// largest remaining sources of that diagnostic. Emit an explicit
			// zero-initialized value of the declared type and bind it.
			if dt := l.typeOfNode(n); dt != NoType && dt != l.voidType {
				val = l.b.EmitInt(OpConst, dt, 0, "")
				childID = id
			}
		}
		if val != NoVal {
			// If the binding's declared type is an option (?T) but the
			// initializer lowered to a bare scalar (e.g. `n ?i64 = 42`), wrap
			// the scalar as the option's "some" discriminant {tag=0,
			// payload=val} so the variable carries the option type end-to-end.
			// Without this, `n` collapsed to a bare i64 and later `n == err` /
			// `n == nil` match comparisons (and `print(n)`) saw the wrong type
			// -> opt-inserted trap / wrong output (test-self-write-str,
			// test-it-probe). A value that is ALREADY an option (e.g. from a
			// `?i64`-returning call) is left as-is.
			if dt := l.typeOfNode(n); dt != NoType && dt != l.voidType {
				if tt := l.mod.Type(dt); tt != nil && tt.Kind == KindOption {
					if vt := l.valueTypeOf(val); vt == NoType || vt == l.voidType || l.mod.Type(vt).Kind != KindOption {
						val = l.b.EmitOptionWrap(dt, 0, val)
					}
				}
			}
			// txt is a fixed 256-byte stack struct ({ [255 x i8] data, i8 len }),
			// distinct from the heap-backed %str-long. When a `let x:txt = <str>`
			// is lowered, the RHS lowers to a %str-long value; convert it into a
			// %txt (bounded memcpy + len byte) so the binding carries the declared
			// txt type end-to-end. Without this, x would silently become a str and
			// print/.len would use str semantics (no 255 cap, heap not stack).
			if l.pkg.Type(n.Type) == "txt" {
				val = l.b.Emit(OpTxtFromStr, l.b.Type("txt"), []ValueID{val}, "")
			}
			if existing, ok := l.locals[name]; ok {
				// Re-binding an already-declared variable (e.g. assigning to a
				// result parameter like `out = a + b`, or a reassignment). Move the
				// rhs into the EXISTING slot so the name keeps pointing at the same
				// slot; this also makes result params get written back correctly.
				// Owned reassignment frees the old value exactly once (drop) then
				// transfers ownership of the new value via move; the memory analysis
				// inserts the slot's exit drop, which frees the NEW content.
				if l.isOwnedLocal(existing) {
					l.b.EmitVoid(OpDrop, []ValueID{existing}, "")
				}
				l.b.EmitMoveInto(existing, val)
				break
			}
		// New binding. If the initializer is a direct reference to an
		// existing NON-OWNED local (e.g. `tmp = n`), binding `name` to that
		// local's value id would ALIAS the two names onto one slot. A later
		// reassignment of `name` (a loop-carried variable such as
		// `tmp = tmp/10`) would then write back into the SHARED slot and
		// corrupt the original local — the bug behind `number.i64-to-str`
		// returning "" for multi-digit input (n was divided to 0 by the
		// digit-counting loop, so the second loop never ran). Give `name`
		// its OWN slot and COPY the value in, so the two names stay
		// independent. This also restores correct value semantics: a plain
		// `let x = y` (scalar) is a copy, not a live view of `y`. Owned
		// values keep the alias (view) semantics the memory analysis already
		// handles with a single drop for the shared slot.
		if cn := l.pkg.Node(childID); cn != nil && cn.Kind == hir.KIdent {
				if _, isLocal := l.locals[l.pkg.Str(cn.S)]; isLocal && !l.isOwnedLocal(val) {
					typ := l.valueTypeOf(val)
					if typ == NoType || typ == l.voidType {
						typ = l.typeOfNode(cn)
					}
					if typ != NoType && typ != l.voidType {
						fresh := l.b.Emit(OpMove, typ, []ValueID{val}, "")
						l.locals[name] = fresh
						if f := l.mod.Func(l.curFunc); f != nil {
							if _, ok := f.LocalTypes[fresh]; !ok {
								f.LocalTypes[fresh] = typ
							}
						}
						break
					}
				}
			}
			l.locals[name] = val
			if f := l.mod.Func(l.curFunc); f != nil {
				// Preserve the type Emit already assigned to val (authoritative for
				// call results, which the KLet child node does not carry a type for).
				// Only fill in when missing — e.g. a bare KIdent placeholder.
				if _, ok := f.LocalTypes[val]; !ok {
					f.LocalTypes[val] = l.typeOfNode(l.pkg.Node(childID))
				}
			}
		}
	case hir.KExprStmt:
		// An `if`/`for` used as a statement is wrapped in a KExprStmt whose child
		// is the KIf/KFor node (ForStatement lowers directly to KFor, but
		// IfExpression goes through the expression path and gets wrapped). Route
		// it to the control-flow lowerer instead of lowerExpr, which would reject
		// it as "control flow used as expression value" and drop the whole if.
		child := hir.NoID
		for _, c := range l.pkg.Children(id) {
			child = c
			break
		}
		if child != hir.NoID {
			cn := l.pkg.Node(child)
			if cn != nil && (cn.Kind == hir.KIf || cn.Kind == hir.KFor) {
				if cn.Kind == hir.KIf {
					l.lowerIf(cn)
				} else {
					l.lowerFor(cn)
				}
				return
			}
		}
		for _, c := range l.pkg.Children(id) {
			l.lowerExpr(c)
			break
		}
	case hir.KReturn:
		// Surface the function's option result type so `return nil` lowers to an
		// %option constant (the "none" discriminant) rather than a bare i64 that
		// the %option codegen would misread as some(0).
		savedHint := l.typeHint
		if rf := l.mod.Func(l.curFunc); rf != nil && len(rf.ResultParams) == 1 {
			if rt := l.valueTypeOf(rf.ResultParams[0]); rt != NoType {
				if tt := l.mod.Type(rt); tt != nil && tt.Kind == KindOption {
					l.typeHint = rt
				}
			}
		}
		var vals []ValueID
		for _, c := range l.pkg.Children(id) {
			vals = append(vals, l.lowerExpr(c))
		}
		l.typeHint = savedHint
		l.b.Terminate(OpReturn, vals, nil, "")
	case hir.KAssign:
		l.lowerAssignNode(id)
	case hir.KMultiAssign:
		l.lowerMultiAssign(id)
	case hir.KIf:
		l.lowerIf(n)
	case hir.KFor:
		l.lowerFor(n)
	case hir.KBreak:
		if len(l.loopStack) == 0 {
			l.unsupported(l.curFuncName(), "break", "break outside loop")
			return
		}
		top := l.loopStack[len(l.loopStack)-1]
		l.b.Terminate(OpBr, nil, []BlockID{top.exit}, "")
	case hir.KContinue:
		if len(l.loopStack) == 0 {
			l.unsupported(l.curFuncName(), "continue", "continue outside loop")
			return
		}
		top := l.loopStack[len(l.loopStack)-1]
		l.b.Terminate(OpBr, nil, []BlockID{top.update}, "")
	case hir.KBlock:
		l.lowerBlock(id)
	default:
		l.unsupported(l.curFuncName(), hir.KindNames[n.Kind], "statement kind not lowered yet")
	}
}

// ---- control flow ----

func (l *lowerer) lowerIf(n *hir.Node) {
	condID := l.slot(n.Id, "cond")
	thenID := l.slot(n.Id, "then")
	elseID := l.slot(n.Id, "else")

	// Enclosing continuation: the merge block of the nearest enclosing
	// control-flow construct (pushed by the caller's lowerIf/lowerFor). When
	// THIS if's merge block ends up empty (the if is the last statement in its
	// enclosing block — the exact shape a desugared match yields: nested
	// if/elif/else), its then/else branches must redirect to enclosingCont
	// rather than be orphaned and terminated with `ret void` (which used to
	// drop every statement after a nested match arm).
	enclosingCont := NoBlock
	if len(l.contStack) > 0 {
		enclosingCont = l.contStack[len(l.contStack)-1]
	}

	thenBlk := l.b.NewBlock("if.then")
	elseBlk := l.b.NewBlock("if.else")
	mergeBlk := l.b.NewBlock("if.merge")
	l.contStack = append(l.contStack, mergeBlk)
	if condID != hir.NoID {
		c := l.lowerExpr(condID)
		if c != NoVal {
			l.b.Terminate(OpCondBr, []ValueID{c}, []BlockID{thenBlk, elseBlk}, "")
		} else {
			l.b.Terminate(OpBr, nil, []BlockID{thenBlk}, "")
		}
	} else {
		l.b.Terminate(OpBr, nil, []BlockID{thenBlk}, "")
	}

	l.b.SetBlock(thenBlk)
	if thenID != hir.NoID {
		l.lowerBlock(thenID)
	}
	if l.mod.Block(thenBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	l.b.SetBlock(elseBlk)
	if elseID != hir.NoID {
		l.lowerBlock(elseID)
	}
	if l.mod.Block(elseBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	// Pop OUR merge before deciding its fate so that any nested construct that
	// already redirected its empty merge to mergeBlk sees the correct block.
	l.contStack = l.contStack[:len(l.contStack)-1]

	// Always continue lowering into the merge block. Post-if statements (the
	// code lexically following this if inside its enclosing block) are lowered
	// by the caller (lowerBlock) AFTER this if returns, so they must flow into
	// mergeBlk — not into the false/else branch. Previously the merge was
	// spliced to enclosingCont when it looked empty, which left the current
	// block as elseBlk and made those post-if statements run only on the false
	// branch -> e.g. binary `pow`'s `base*=base; n>>=1` never advanced on the
	// taken branch -> infinite loop (test-number-generic).
	l.b.SetBlock(mergeBlk)

	// Record where the merge must fall through when it ends up unterminated
	// (i.e. it is the last statement of its enclosing block OR carries post-if
	// code that then needs to reach the enclosing continuation). ensureReturn
	// consults this map so the merge branches to the enclosing continuation
	// (loop body -> update; nested match arm -> outer merge) instead of being
	// patched with `ret void` (which used to drop every statement after a
	// match arm, or silently broke the loop). We do NOT terminate it here
	// because post-if code is lowered later; the map defers the decision past
	// that timing.
	if enclosingCont != NoBlock {
		l.contTargets[mergeBlk] = enclosingCont
	}
}

func (l *lowerer) lowerFor(n *hir.Node) {
	iterID := l.slot(n.Id, "iter")
	if iterID != hir.NoID {
		// Range-for: `for v in [a, b, c]` (v is the element) or `for v in lo..hi`.
		// Dispatch to the dedicated lowerer, which binds the loop variable so the
		// body can resolve it (previously `v` was never inserted into l.locals and
		// every use cascaded into an "unresolved identifier" lower-gap).
		bodyID := l.slot(n.Id, "body")
		l.lowerRangeFor(n, iterID, bodyID, l.b.CurrentBlock())
		return
	}

	// Repeat-N-times: `for ... * N { body }` (the HIR carries a `count` slot
	// with the integer N and no init/cond/update). Dispatch to the dedicated
	// lowerer, which runs body exactly N times. Without this, lowerFor ignored
	// the `count` slot and fell through to the init/cond/update path with all
	// three empty -> an unconditional `while(true)` -> infinite loop
	// (test-for3 hung under MIR=3 while legacy terminated correctly).
	countID := l.slot(n.Id, "count")
	if countID != hir.NoID {
		bodyID := l.slot(n.Id, "body")
		l.lowerCountFor(n, countID, bodyID)
		return
	}

	initID := l.slot(n.Id, "init")
	condID := l.slot(n.Id, "cond")
	updateID := l.slot(n.Id, "update")
	bodyID := l.slot(n.Id, "body")

	// Enclosing continuation: the merge block of the nearest enclosing
	// control-flow construct (pushed by the caller's lowerIf/lowerFor). We
	// terminate THIS loop's exit block to it when the loop is NOT the last
	// statement of its block — otherwise the exit block stays unterminated,
	// ensureReturn patches it with `ret void` (dropping every statement after
	// the loop), and the malformed CFG lets `opt` mangle the loop header into a
	// constant condition / infinite loop (e.g. `for v in [0..n)` inside an `if`
	// arm, and the fixed-array `to-str` method). At top level this is NoBlock,
	// so the fix is a no-op and ensureReturn still applies `ret` correctly.
	enclosingCont := NoBlock
	if len(l.contStack) > 0 {
		enclosingCont = l.contStack[len(l.contStack)-1]
	}

	pre := l.b.CurrentBlock()
	if initID != hir.NoID {
		l.lowerStmt(initID)
	}
	header := l.b.NewBlock("for.header")
	body := l.b.NewBlock("for.body")
	update := l.b.NewBlock("for.update")
	exit := l.b.NewBlock("for.exit")

	l.loopStack = append(l.loopStack, loopCtx{exit: exit, update: update})
	defer func() { l.loopStack = l.loopStack[:len(l.loopStack)-1] }()

	// Register the loop continuation (the increment/update block) as the
	// enclosing continuation for any control-flow construct lowered inside the
	// body. Without this, an `if` that is the LAST statement of the loop body
	// sees no enclosing continuation, leaves its merge block unterminated, and
	// ensureReturn appends `ret void` — which terminates the WHOLE function
	// after the first iteration (e.g. str.to-upper emitting only the first
	// char). Pushing `update` makes the empty if-merge redirect here instead.
	l.contStack = append(l.contStack, update)
	defer func() { l.contStack = l.contStack[:len(l.contStack)-1] }()

	l.b.SetBlock(pre)
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")

	l.b.SetBlock(header)
	if condID != hir.NoID {
		c := l.lowerExpr(condID)
		if c != NoVal {
			l.b.Terminate(OpCondBr, []ValueID{c}, []BlockID{body, exit}, "")
		} else {
			l.b.Terminate(OpBr, nil, []BlockID{body}, "")
		}
	} else {
		l.b.Terminate(OpBr, nil, []BlockID{body}, "")
	}

	l.b.SetBlock(body)
	if bodyID != hir.NoID {
		l.lowerBlock(bodyID)
	}
	if l.mod.Block(body).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}
	// Safety net: if the body fell off the end into a dangling (unterminated)
	// nested merge block — e.g. a non-terminal last statement after an `if`
	// whose merge stayed the current block — route it to the loop continuation
	// rather than leaving it orphaned for ensureReturn to patch with `ret void`.
	if cur := l.b.CurrentBlock(); cur != NoBlock && l.mod.Block(cur).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}

	l.b.SetBlock(update)
	if updateID != hir.NoID {
		l.lowerStmt(updateID)
	}
	if l.mod.Block(update).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	}

	l.b.SetBlock(exit)
	if enclosingCont != NoBlock && l.mod.Block(exit).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{enclosingCont}, "")
	}
}

// lowerRangeFor lowers a range-for: `for v in COLLECTION { body }` where v is
// bound to each element of COLLECTION, and `for v in LO..HI { body }` where v is
// bound to each integer in [LO, HI). It is the loop-variable-binding counterpart
// of the classic init/cond/update for (which already worked). The variable is
// inserted into l.locals so body expressions resolve it instead of cascading into
// unresolved-identifier lower-gaps.
func (l *lowerer) lowerRangeFor(n *hir.Node, iterID int32, bodyID int32, pre BlockID) {
	// Enclosing continuation: the merge block of the nearest enclosing
	// control-flow construct (pushed by the caller's lowerIf/lowerFor). Same
	// rationale as lowerFor: terminate this range-for's exit block to it when
	// the loop is not the last statement of its block, so the loop exit is not
	// left unterminated (which mangles the CFG into a constant-condition /
	// infinite loop once `opt` sees it). At top level this is NoBlock.
	enclosingCont := NoBlock
	if len(l.contStack) > 0 {
		enclosingCont = l.contStack[len(l.contStack)-1]
	}

	iterNode := l.pkg.Node(iterID)
	if iterNode == nil {
		l.unsupported(l.curFuncName(), "for-range", "missing iter node")
		return
	}
	varName := l.pkg.Str(iterNode.S)
	// KIter.First is built by Builder.List, which DROPS NoID children — so for
	// `for i in [a,b,c]` (no KRange) First is just [KArrayLit], and for
	// `for i in lo..hi` (no RangeExpr) it is just [KRange]. Detect by child kind
	// rather than by position.
	var rangeNodeID, rangeExprID int32
	for _, c := range l.pkg.Children(iterID) {
		cn := l.pkg.Node(c)
		if cn == nil {
			continue
		}
		if cn.Kind == hir.KRange {
			rangeNodeID = c
		} else {
			rangeExprID = c
		}
	}

	// --- integer range form: for v in lo..hi ---
	if rangeNodeID != hir.NoID {
		rn := l.pkg.Node(rangeNodeID)
		if rn == nil || rn.Kind != hir.KRange {
			l.unsupported(l.curFuncName(), "for-range", "unsupported iter form")
			return
		}
		startID := l.slot(rangeNodeID, "start")
		endID := l.slot(rangeNodeID, "end")
		if startID == hir.NoID || endID == hir.NoID {
			l.unsupported(l.curFuncName(), "for-range", "open range not supported")
			return
		}
		startV := l.lowerExpr(startID)
		endV := l.lowerExpr(endID)
		if startV == NoVal || endV == NoVal {
			return
		}
		leftInc := rn.Has(hir.FlagLeftInc)  // '[' -> start inclusive
		rightInc := rn.Has(hir.FlagRightInc) // ']' -> end inclusive
		elemT := l.b.Type("i64")
		// Allocate the loop variable as a local with its own alloca slot. Using
		// b.Param would mint a value id WITHOUT a slot (params are only slotted
		// when registered into f.Params, which loop vars are not) -> the init
		// move would hit "move destination has no slot". Emitting a zero const
		// gives it a Dst-backed slot; the init move below overwrites it.
		iSlot := l.b.EmitInt(OpConst, elemT, 0, varName)
		l.locals[varName] = iSlot
		// init v = lo  (or lo+1 when the left bound is open '(')
		l.b.SetBlock(pre)
		if leftInc {
			l.b.EmitMoveInto(iSlot, startV)
		} else {
			one0 := l.b.EmitInt(OpConst, elemT, 1, "")
			adjV := l.b.Emit(OpAdd, elemT, []ValueID{startV, one0}, "")
			l.b.EmitMoveInto(iSlot, adjV)
		}
		header := l.b.NewBlock("for.header")
		body := l.b.NewBlock("for.body")
		update := l.b.NewBlock("for.update")
		exit := l.b.NewBlock("for.exit")
		l.loopStack = append(l.loopStack, loopCtx{exit: exit, update: update})
		defer func() { l.loopStack = l.loopStack[:len(l.loopStack)-1] }()
		// Register the loop continuation as the enclosing continuation so an
		// empty if-merge inside the body redirects here (see lowerFor).
		l.contStack = append(l.contStack, update)
		defer func() { l.contStack = l.contStack[:len(l.contStack)-1] }()
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
		l.b.SetBlock(header)
		// '<=' for an inclusive right bound (']'), '<' for an open one (')').
		cmpOp := OpLt
		if rightInc {
			cmpOp = OpLe
		}
		condV := l.b.Emit(cmpOp, l.b.Type("bool"), []ValueID{iSlot, endV}, "")
		l.b.Terminate(OpCondBr, []ValueID{condV}, []BlockID{body, exit}, "")
		l.b.SetBlock(body)
		if bodyID != hir.NoID {
			l.lowerBlock(bodyID)
		}
		if l.mod.Block(body).Term == nil {
			l.b.Terminate(OpBr, nil, []BlockID{update}, "")
		}
		if cur := l.b.CurrentBlock(); cur != NoBlock && l.mod.Block(cur).Term == nil {
			l.b.Terminate(OpBr, nil, []BlockID{update}, "")
		}
		l.b.SetBlock(update)
		one := l.b.EmitInt(OpConst, elemT, 1, "")
		nextV := l.b.Emit(OpAdd, elemT, []ValueID{iSlot, one}, "")
		l.b.EmitMoveInto(iSlot, nextV)
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	l.b.SetBlock(exit)
	if enclosingCont != NoBlock && l.mod.Block(exit).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{enclosingCont}, "")
	}
	return
}

	// --- collection form: for v in [a, b, c]  (v is the element) ---
	if rangeExprID == hir.NoID {
		l.unsupported(l.curFuncName(), "for-range", "empty range expression")
		return
	}
	arrV := l.lowerExpr(rangeExprID)
	if arrV == NoVal {
		return
	}
	elemT := l.elementTypeOf(arrV)
	if elemT == l.voidType {
		l.unsupported(l.curFuncName(), "for-range", "cannot determine element type of collection")
		return
	}
	// Static length: fixed arrays only. Slices/ident-of-slice need a runtime
	// len intrinsic, which is not wired up yet — fall back to the proven legacy
	// path for those (the lower-gap triggers a full-function fallback).
	length := int64(-1)
	if t := l.valueTypeOf(arrV); t != NoType {
		if ty := l.mod.Type(t); ty != nil && ty.Kind == KindArray && len(ty.Sizes) > 0 {
			length = ty.Sizes[0]
		}
	}
	if length < 0 {
		l.unsupported(l.curFuncName(), "for-range", "non-fixed-length collection not supported")
		return
	}

	idxT := l.b.Type("i64")
	// Same slot-allocation rationale as the integer-range form: emit a zero
	// const so the index variable gets a Dst-backed alloca slot.
	idxSlot := l.b.EmitInt(OpConst, idxT, 0, varName+"#idx")
	// pre: idx = 0
	l.b.SetBlock(pre)
	l.b.EmitMoveInto(idxSlot, l.b.EmitInt(OpConst, idxT, 0, ""))

	header := l.b.NewBlock("for.header")
	body := l.b.NewBlock("for.body")
	update := l.b.NewBlock("for.update")
	exit := l.b.NewBlock("for.exit")
	l.loopStack = append(l.loopStack, loopCtx{exit: exit, update: update})
	defer func() { l.loopStack = l.loopStack[:len(l.loopStack)-1] }()
	// Register the loop continuation as the enclosing continuation so an empty
	// if-merge inside the body redirects here (see lowerFor).
	l.contStack = append(l.contStack, update)
	defer func() { l.contStack = l.contStack[:len(l.contStack)-1] }()
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")

	l.b.SetBlock(header)
	lenV := l.b.EmitInt(OpConst, idxT, length, "")
	condV := l.b.Emit(OpLt, l.b.Type("bool"), []ValueID{idxSlot, lenV}, "")
	l.b.Terminate(OpCondBr, []ValueID{condV}, []BlockID{body, exit}, "")

	// Body entry: bind v = collection[idx]. Emitted here (not in pre) so it runs
	// once per iteration, reloading the current element into v's slot.
	l.b.SetBlock(body)
	iSlot := l.b.Emit(OpIndex, elemT, []ValueID{arrV, idxSlot}, "")
	l.locals[varName] = iSlot
	if bodyID != hir.NoID {
		l.lowerBlock(bodyID)
	}
	if l.mod.Block(body).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}
	if cur := l.b.CurrentBlock(); cur != NoBlock && l.mod.Block(cur).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}

	l.b.SetBlock(update)
	one := l.b.EmitInt(OpConst, idxT, 1, "")
	nextIdx := l.b.Emit(OpAdd, idxT, []ValueID{idxSlot, one}, "")
	l.b.EmitMoveInto(idxSlot, nextIdx)
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")

	l.b.SetBlock(exit)
	if enclosingCont != NoBlock && l.mod.Block(exit).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{enclosingCont}, "")
	}
}

// lowerCountFor lowers a repeat-N-times loop: `for ... * N { body }` (the HIR
// carries a `count` slot with the integer N and no init/cond/update). It runs
// body exactly N times. Without this, lowerFor ignored the `count` slot and
// fell through to the init/cond/update path with all three empty -> an
// unconditional `while(true)` -> infinite loop (test-for3 hung under MIR=3
// while legacy terminated correctly). The counter is a real local slot
// decremented each iteration; the loop terminates when it reaches zero.
func (l *lowerer) lowerCountFor(n *hir.Node, countID int32, bodyID int32) {
	// Enclosing continuation: the merge block of the nearest enclosing
	// control-flow construct (pushed by the caller's lowerIf/lowerFor). Same
	// rationale as lowerFor / lowerRangeFor: terminate this loop's exit block
	// to it when the loop is not the last statement of its block, so the loop
	// exit is not left unterminated (which mangles the CFG into a constant
	// condition / infinite loop once `opt` sees it).
	enclosingCont := NoBlock
	if len(l.contStack) > 0 {
		enclosingCont = l.contStack[len(l.contStack)-1]
	}

	cntV := l.lowerExpr(countID)
	if cntV == NoVal {
		return
	}
	idxT := l.valueTypeOf(cntV)
	if idxT == NoType || idxT == l.voidType {
		idxT = l.b.Type("i64")
	}
	// Counter variable: give it a real Dst-backed slot (same rationale as
	// lowerRangeFor — a bare Param value id has no slot and the init move
	// would hit "move destination has no slot"). The zero const below seeds
	// the slot; the move overwrites it with N.
	iSlot := l.b.EmitInt(OpConst, idxT, 0, "ri")
	pre := l.b.CurrentBlock()
	l.b.SetBlock(pre)
	l.b.EmitMoveInto(iSlot, cntV)

	header := l.b.NewBlock("for.header")
	body := l.b.NewBlock("for.body")
	update := l.b.NewBlock("for.update")
	exit := l.b.NewBlock("for.exit")
	l.loopStack = append(l.loopStack, loopCtx{exit: exit, update: update})
	defer func() { l.loopStack = l.loopStack[:len(l.loopStack)-1] }()
	// Register the loop continuation as the enclosing continuation so an empty
	// if-merge inside the body redirects here (see lowerFor / lowerRangeFor).
	l.contStack = append(l.contStack, update)
	defer func() { l.contStack = l.contStack[:len(l.contStack)-1] }()

	l.b.SetBlock(pre)
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	l.b.SetBlock(header)
	zero := l.b.EmitInt(OpConst, idxT, 0, "")
	condV := l.b.Emit(OpGt, l.b.Type("bool"), []ValueID{iSlot, zero}, "")
	l.b.Terminate(OpCondBr, []ValueID{condV}, []BlockID{body, exit}, "")
	l.b.SetBlock(body)
	if bodyID != hir.NoID {
		l.lowerBlock(bodyID)
	}
	if l.mod.Block(body).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}
	if cur := l.b.CurrentBlock(); cur != NoBlock && l.mod.Block(cur).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{update}, "")
	}
	l.b.SetBlock(update)
	one := l.b.EmitInt(OpConst, idxT, 1, "")
	nextV := l.b.Emit(OpSub, idxT, []ValueID{iSlot, one}, "")
	l.b.EmitMoveInto(iSlot, nextV)
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	l.b.SetBlock(exit)
	if enclosingCont != NoBlock && l.mod.Block(exit).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{enclosingCont}, "")
	}
}

// valueTypeOf returns the static type id of a value, consulting the current
// function's LocalTypes first (authoritative for call results) then the global
// value table. Returns NoType when unknown.
func (l *lowerer) valueTypeOf(v ValueID) TypeID {
	if v <= NoVal {
		return NoType
	}
	if f := l.mod.Func(l.curFunc); f != nil {
		if t, ok := f.LocalTypes[v]; ok {
			return t
		}
	}
	if val := l.mod.Value(v); val != nil {
		return val.Type
	}
	return NoType
}

// lowerArrayElems materializes a fixed array [N]elem from a list of element
// expression node ids, storing each element into its slot. Shared by KArrayLit
// (elems come from "elem" slots) and KSliceLit (elems are direct children).
func (l *lowerer) lowerArrayElems(elems []int32) ValueID {
	if len(elems) == 0 {
		return NoVal
	}
	elemT := l.typeOfNode(l.pkg.Node(elems[0]))
	elemRaw := "i64"
	if ty := l.mod.Type(elemT); ty != nil && ty.Raw != "" {
		elemRaw = ty.Raw
	}
	arrRaw := fmt.Sprintf("[%d]%s", len(elems), elemRaw)
	arrT := l.b.Type(arrRaw)
	arrV := l.b.Emit(OpConst, arrT, nil, "")
	for k, e := range elems {
		ev := l.lowerExpr(e)
		if ev == NoVal {
			return NoVal
		}
		idxV := l.b.EmitInt(OpConst, l.b.Type("i64"), int64(k), "")
		l.b.EmitVoid(OpIndexStore, []ValueID{arrV, idxV, ev}, "")
	}
	return arrV
}

// lowerGlobalRef resolves a reference to a module-level binding (SBOX,
// TLS-FINISHED-SIZE, perm-600, ...) that lives outside every function. The
// global value is created lazily on first reference and its LLVM constant
// initializer is folded from the top-level KLet's expression. If the
// initializer cannot be constant-folded (ConstText == ""), the resulting
// `@name` is emitted as an external declaration; the LLVM verifier then rejects
// it, the function falls back to the proven legacy codegen, and — critically —
// we never emit wrong data for a crypto constant.
func (l *lowerer) lowerGlobalRef(name string) ValueID {
	if v, ok := l.globals[name]; ok && v != NoVal {
		return v
	}
	gtype := l.globalTypes[name]
	if gtype == "" {
		gtype = "i64"
	}
	typ := l.b.Type(gtype)
	gv := l.b.Global(name, typ)
	// Fold the constant initializer from the recorded top-level node.
	if nid, ok := l.globalNodes[name]; ok {
		if nn := l.pkg.Node(nid); nn != nil {
			ct := l.foldConstText(nn, gtype)
			for i := range l.mod.Globals {
				if l.mod.Globals[i].Init == gv {
					l.mod.Globals[i].ConstText = ct
					break
				}
			}
		}
	}
	l.globals[name] = gv
	return gv
}

// foldConstText folds an HIR constant expression into LLVM constant-initializer
// text (e.g. `i64 12`, `[256 x i8] c"\63\7c..."`). Returns "" when the
// expression is not a compile-time constant (caller then emits an external
// global and falls back to legacy — never wrong data).
func (l *lowerer) foldConstText(n *hir.Node, gtype string) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case hir.KLet:
		// A module-level `let` constant (`SBOX [256]byte = [...]`): descend to
		// its initializer expression so the global folds to a real LLVM constant
		// (e.g. `[256 x i8] c"..."`) instead of an unresolvable external
		// declaration (which would make the referencing function fall back to
		// legacy codegen).
		for _, c := range l.pkg.Children(n.Id) {
			return l.foldConstText(l.pkg.Node(c), gtype)
		}
		return ""
	case hir.KIntLit:
		return fmt.Sprintf("i64 %d", n.Val)
	case hir.KCharLit:
		return fmt.Sprintf("i64 %d", n.Val)
	case hir.KBoolLit:
		if n.Bool() {
			return "i1 1"
		}
		return "i1 0"
	case hir.KFloatLit:
		return fmt.Sprintf("double %s", formatFloat(n.Float()))
	case hir.KPrefix:
		// Negation / bitwise-complement of a constant operand: fold the operand
		// and apply the operator so negative module constants (e.g.
		// `bad-fd fd = -1`, `min = -128`) become real LLVM constants instead of
		// unmaterialized global markers (which read as `undef` and trap).
		var operand int32
		for _, c := range l.pkg.Children(n.Id) {
			operand = c
			break
		}
		if operand == hir.NoID {
			return ""
		}
		ct := l.foldConstText(l.pkg.Node(operand), gtype)
		if ct == "" {
			return ""
		}
		fields := strings.Fields(ct)
		if len(fields) != 2 {
			return ""
		}
		typ, lit := fields[0], fields[1]
		switch l.pkg.Str(n.S) {
		case "-":
			v, err := strconv.ParseInt(lit, 10, 64)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%s %d", typ, -v)
		case "~":
			v, err := strconv.ParseUint(lit, 10, 64)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%s %d", typ, int64(^v))
		}
		return ""
	case hir.KIdent:
		// A reference to another module constant (e.g. `copy-fd fd = my-stdin`):
		// fold to that constant's initializer text so the alias becomes a real
		// LLVM constant instead of an unmaterialized global marker (which reads
		// as `undef`). Only resolves when the referenced name is already a
		// registered module constant (registration runs in source order, so
		// preceding `let`s are available).
		name := l.pkg.Str(n.S)
		if nid, ok := l.globalNodes[name]; ok {
			if nn := l.pkg.Node(nid); nn != nil {
				if ct := l.foldConstText(nn, gtype); ct != "" {
					return ct
				}
			}
		}
		return ""
	case hir.KArrayLit:
		var elems []int32
		// HIR array literals carry a leading KSlot("size") plus one KSlot("elem")
		// per element; unwrap the "elem" slots to the actual element expressions
		// and ignore the "size" slot.
		for _, c := range l.pkg.Children(n.Id) {
			cn := l.pkg.Node(c)
			if cn == nil || cn.Kind != hir.KSlot || l.pkg.Str(cn.S) != "elem" {
				continue
			}
			elems = append(elems, cn.First)
		}
		if len(elems) == 0 {
			return ""
		}
		// Byte arrays fold to a compact c"..." data global (used by SBOX /
		// INV-SBOX substitution boxes). Match on the declared global type to
		// be robust against element-literal type inference defaults.
		if m := byteArrayRe.FindStringSubmatch(gtype); m != nil {
			n2 := len(elems)
			var data strings.Builder
			for _, e := range elems {
				ev := l.pkg.Node(e)
				// Byte-array elements are emitted as KByteLit in HIR; accept both
				// KByteLit and KIntLit (some array literals use i64 element
				// literals) so substitution boxes ([256]byte) fold correctly.
				if ev == nil || (ev.Kind != hir.KIntLit && ev.Kind != hir.KByteLit) {
					return ""
				}
				data.WriteString(fmt.Sprintf(`\%02X`, ev.Val&0xff))
			}
			_ = n2
			return fmt.Sprintf("[%d x i8] c\"%s\"", len(elems), data.String())
		}
		// Generic integer/float array: [N x i64] [i64 .., ...].
		var parts []string
		for _, e := range elems {
			ct := l.foldConstText(l.pkg.Node(e), "")
			if ct == "" {
				return ""
			}
			parts = append(parts, ct)
		}
		return fmt.Sprintf("[%d x i64] [%s]", len(elems), strings.Join(parts, ", "))
	}
	return ""
}

// ---- expressions ----

func (l *lowerer) lowerExpr(id int32) ValueID {
	n := l.pkg.Node(id)
	if n == nil {
		return NoVal
	}
	switch n.Kind {
	case hir.KGrouped:
		// Parenthesized expression: strip the grouping and lower the inner node.
		var child int32
		for _, c := range l.pkg.Children(id) {
			child = c
			break
		}
		if child != hir.NoID {
			return l.lowerExpr(child)
		}
		return NoVal
	case hir.KAssign:
		// An assignment used as an expression (e.g. `if (x = read())` or an
		// assignment wrapped in a KExprStmt) evaluates to its right-hand side.
		return l.lowerAssignNode(id)
	case hir.KIndex:
		// array/slice element read: a[i]
		var arrID, idxID int32
		i := 0
		for _, c := range l.pkg.Children(id) {
			if i == 0 {
				arrID = c
			} else {
				idxID = c
				break
			}
			i++
		}
		arrV := l.lowerExpr(arrID)
		idxV := l.lowerExpr(idxID)
		if arrV == NoVal || idxV == NoVal {
			return NoVal
		}
		elemT := l.elementTypeOf(arrV)
		return l.b.Emit(OpIndex, elemT, []ValueID{arrV, idxV}, "")
	case hir.KArrayLit:
		// Typed fixed array literal: `a [N] = [e0, e1, ...]`. In HIR the KArrayLit
		// has a "size" slot followed by "elem" slots; gather the element
		// expressions via slotArgs and store each into its slot.
		elems := l.slotArgs(id, "elem")
		return l.lowerArrayElems(elems)
	case hir.KSliceLit:
		// Untyped slice literal: `[e0, e1, ...]`. In HIR the elements are direct
		// children (no "size"/"elem" slots). Materialize as a fixed array so it
		// can be indexed and iterated like a KArrayLit.
		var elems []int32
		for _, c := range l.pkg.Children(id) {
			elems = append(elems, c)
		}
		return l.lowerArrayElems(elems)
	case hir.KIdent:
		name := l.pkg.Str(n.S)
		// Option variant bare references (`err`, `ok`, `some`, `nil`): lower to
		// the option discriminant constant so comparisons / match patterns
		// resolve to the RIGHT tag. `nil` is the "none" discriminant (tag 1);
		// `ok`/`some` are "some" (tag 0); `err` is "err" (tag 2). A bare variant
		// identifier carries NO type in HIR (it resolves to void), so the ?T type
		// must be read from context: the infix comparison sibling (seeded into
		// l.typeHint by the KInfix lowering below) or the active type hint.
		// `err`/`ok`/`nil` may also be registered as module globals, so this
		// MUST run BEFORE the locals / globals lookup — otherwise they resolve to
		// a plain ?T global that codegen materializes as the nil tag (or undef),
		// breaking `n == err`/`v == nil` matches and `it.to-str()` arms (the
		// trap/BPT behind several corpus crashes, including `[n]t.at` / `.fl` /
		// `.last` which compare the option result against `nil` inside their
		// desugared `UnwrapAssign` match).
		if name == "err" || name == "ok" || name == "some" || name == "nil" {
			optTyp := l.typeOfNode(n)
			if t := l.mod.Type(optTyp); t == nil || t.Kind != KindOption {
				if ht := l.typeHint; ht != NoType && ht != l.voidType {
					if tt := l.mod.Type(ht); tt != nil && tt.Kind == KindOption {
						optTyp = ht
					}
				}
			}
			if optTyp != NoType && optTyp != l.voidType {
				if tt := l.mod.Type(optTyp); tt != nil && tt.Kind == KindOption {
					tag := int64(0)
					switch name {
					case "err":
						tag = 2
					case "nil":
						tag = 1
					}
					var payload ValueID = NoVal
					if name != "nil" && tt.Elem != NoType {
						payload = l.b.Emit(OpConst, tt.Elem, nil, "")
					}
					return l.b.EmitOptionWrap(optTyp, tag, payload)
				}
			}
		}
		if v, ok := l.locals[name]; ok {
			return v
		}
		// A top-level module binding (SBOX, TLS-FINISHED-SIZE, perm-600, ...)
		// lives outside every function; resolve it as a module global. This is
		// the last resort before declaring the identifier unresolved, so it
		// must come AFTER the locals lookup above (a local shadow wins).
		if _, isGlobal := l.globals[name]; isGlobal {
			return l.lowerGlobalRef(name)
		}
		// unresolved: create a placeholder value of the inferred type so later
		// instructions can still reference it; flag for diagnostics.
		l.unsupported(l.curFuncName(), "ident", "unresolved identifier "+name)
		return l.b.Emit(OpConst, l.typeOfNode(n), nil, "")
	case hir.KIntLit:
		return l.b.EmitInt(OpConst, l.typeOfNode(n), n.Val, "")
	case hir.KFloatLit:
		v := l.b.Emit(OpConst, l.typeOfNode(n), nil, "")
		l.mod.Insts[len(l.mod.Insts)-1].Flt = n.Float()
		return v
	case hir.KBoolLit:
		val := int64(0)
		if n.Bool() {
			val = 1
		}
		return l.b.EmitInt(OpConst, l.typeOfNode(n), val, "")
	case hir.KStrLit:
		s := l.pkg.Str(n.S)
		if strings.Contains(s, "{") && strings.Contains(s, "}") {
			// Nolang string interpolation (`'x={expr}'`) is not yet lowered by
			// the v1 MIR backend: it would emit the raw literal with the
			// UNSUBSTITUTED `{expr}` text, producing WRONG output that still
			// exits 0 — a silent correctness regression versus the legacy path
			// (which substitutes). Record a FATAL lower diagnostic (kind
			// "interp") so emitMIR falls back to the legacy HIR codegen for the
			// whole module. Shipping unsubstituted output is strictly worse than
			// using the legacy backend, so we never let MIR emit it.
			l.unsupported(l.curFuncName(), "interp", "string interpolation not lowered: "+interpPreview(s))
		}
		return l.b.EmitStr(OpConst, l.b.Type("str"), s, "")
	case hir.KCharLit:
		return l.lowerCharLit(n)
	case hir.KNilLit:
		return l.lowerNilLit(n)
	case hir.KStructLit:
		return l.lowerStructLit(n)
	case hir.KSlice:
		return l.lowerSlice(n)
	case hir.KPrefix:
		var operand int32
		for _, c := range l.pkg.Children(id) {
			operand = c
			break
		}
		ov := l.lowerExpr(operand)
		op := prefixOp(l.pkg.Str(n.S))
		if l.pkg.Str(n.S) == "~" {
			// Bitwise complement (`~x`): x XOR all-ones. Nolang `~` is NOT logical
			// NOT — mapping it to OpNot (which emits `xor 1`) gives 100^1=101
			// instead of the legacy ~100 = 4294967195. Route through OpXor with an
			// all-ones constant of the operand's (integer) type.
			rt := l.typeOfNode(n)
			if rt == l.voidType || rt == NoType {
				if ot := l.valueTypeOf(ov); ot != l.voidType && ot != NoType && l.valueRaw(ov) != "str" {
					rt = ot
				}
			}
			if rt == l.voidType || rt == NoType {
				rt = l.b.Type("i64")
			}
			allOnes := l.b.EmitInt(OpConst, rt, -1, "")
			return l.b.Emit(OpXor, rt, []ValueID{ov, allOnes}, "")
		}
		rt := l.typeOfNode(n)
		if rt == l.voidType || rt == NoType {
			// Unary result type unknown here (e.g. `!x` where x is a global) —
			// derive it from the operand value type so we don't emit `xor void`.
			if ot := l.valueTypeOf(ov); ot != l.voidType && ot != NoType && l.valueRaw(ov) != "str" {
				rt = ot
			}
		}
		return l.b.Emit(op, rt, []ValueID{ov}, "")
	case hir.KInfix:
		var lr [2]int32
		i := 0
		for _, c := range l.pkg.Children(id) {
			if i < 2 {
				lr[i] = c
				i++
			}
		}
		op, isCmp := infixOp(l.pkg.Str(n.S))
		// Option/nil pattern matching (`it == nil`): the nil operand must lower
		// to the option's "none" discriminant (%option {tag=1, val=0}), never a
		// bare i64 0. Publish the *sibling* operand's actual type as a hint so
		// lowerNilLit emits the option constant; emitCmp then extracts the tag
		// from BOTH sides and compares tag==1 (the meaning of `== nil`). Without
		// the hint, nil stays i64 0, emitCmp checks tag==0, and the match INVERTS
		// (some(x) falls into the nil arm, nil falls into the default arm) — the
		// bug behind `a.at(0)` matching nil and `?i64 = nil` matching default.
		var lv, rv ValueID
		if i == 2 && (op == OpEq || op == OpNe) {
			ln := l.pkg.Node(lr[0])
			rn := l.pkg.Node(lr[1])
			if ln != nil && rn != nil {
				// A bare option-variant operand (`nil`, `err`, `ok`, `some`)
				// carries no type of its own; seed l.typeHint with its sibling's
				// ?T type so the variant lowers to the correct discriminant.
				isVar := func(nd *hir.Node) bool {
					if nd == nil {
						return false
					}
					if nd.Kind == hir.KNilLit {
						return true
					}
					if nd.Kind == hir.KIdent {
						switch l.pkg.Str(nd.S) {
						case "err", "ok", "some", "nil":
							return true
						}
					}
					return false
				}
				switch {
				case isVar(ln) && !isVar(rn):
					rv = l.lowerExpr(lr[1])
					saved := l.typeHint
					l.typeHint = l.valueTypeOf(rv)
					lv = l.lowerExpr(lr[0])
					l.typeHint = saved
				case isVar(rn) && !isVar(ln):
					lv = l.lowerExpr(lr[0])
					saved := l.typeHint
					l.typeHint = l.valueTypeOf(lv)
					rv = l.lowerExpr(lr[1])
					l.typeHint = saved
				default:
					lv = l.lowerExpr(lr[0])
					rv = l.lowerExpr(lr[1])
				}
			} else {
				lv = l.lowerExpr(lr[0])
				rv = l.lowerExpr(lr[1])
			}
		} else {
			lv = l.lowerExpr(lr[0])
			rv = l.lowerExpr(lr[1])
		}
		resTyp := l.typeOfNode(n)
		if isCmp {
			resTyp = l.b.Type("bool")
		} else if resTyp == l.voidType || resTyp == NoType {
			// Arithmetic result type unknown here (e.g. an operand is a
			// module-level global whose KIdent type resolves to void, or a
			// char-code expression `c + 1`). Derive it from the lowered operand
			// value types so we never emit an illegal `add void` / `sub void`.
			// Skip str operands — `str + str` is handled just below, and
			// `str + int` must not be forced to a str result type.
			if lt := l.valueTypeOf(lv); lt != l.voidType && lt != NoType && l.valueRaw(lv) != "str" {
				resTyp = lt
			} else if rt := l.valueTypeOf(rv); rt != l.voidType && rt != NoType && l.valueRaw(rv) != "str" {
				resTyp = rt
			}
		}
		// String concatenation (`a - b` / `a + b` on str operands) yields a
		// str, never void. Without this the infix result value is typed void
		// and emitArith emits an illegal `add void` (or `sub void`) on the
		// %str-long operands. Detect it from the operand raw types.
		if !isCmp && l.valueRaw(lv) == "str" && l.valueRaw(rv) == "str" {
			resTyp = l.b.Type("str")
		}
		// String equality/inequality cannot be a direct `icmp` (str is a struct),
		// so route it through the runtime @str_eq helper via OpStrEq. `!=` negates
		// the equality result with a boolean icmp.
		if (op == OpEq || op == OpNe) && l.valueRaw(lv) == "str" {
			eq := l.b.Emit(OpStrEq, l.b.Type("bool"), []ValueID{lv, rv}, "")
			if op == OpEq {
				return eq
			}
			fals := l.b.EmitInt(OpConst, l.b.Type("bool"), 0, "")
			return l.b.Emit(OpNe, l.b.Type("bool"), []ValueID{eq, fals}, "")
		}
		return l.b.Emit(op, resTyp, []ValueID{lv, rv}, "")
	case hir.KCall:
		return l.lowerCall(n)
	case hir.KDot:
		// Enum variant reference (`code.io`, `code.not-found`): the receiver is
		// an enum type and the field is a variant name. Lower to the variant's
		// i64 discriminant constant instead of a field read (which would be a
		// void op on an enum type and produce `icmp eq void undef, undef`).
		var recvID int32
		for _, c := range l.pkg.Children(id) {
			recvID = c
			break
		}
		if recvID != hir.NoID {
			rn := l.pkg.Node(recvID)
			if rn != nil && rn.Kind == hir.KIdent {
				recvName := l.pkg.Str(rn.S)
				if l.isEnumTypeName(recvName) {
					fieldName := l.pkg.Str(n.S)
					if v, ok := l.enumVariantValue(recvName + "." + fieldName); ok {
						return l.b.EmitInt(OpConst, l.b.Type("i64"), v, "")
					}
				}
			}
		}
		// Standalone field/property access `recv.field` (NOT a method call — a
		// method call has the KDot as the KCall's `fn` slot and is handled in
		// lowerCall). Lowers to OpGetField with the field name carried in Str.
		return l.lowerDotRead(n)
	case hir.KIf, hir.KFor:
		l.unsupported(l.curFuncName(), "expr-ctrl", "control flow used as expression value")
		return NoVal
	default:
		l.unsupported(l.curFuncName(), hir.KindNames[n.Kind], "expression kind not lowered yet")
		return NoVal
	}
}

// lowerDotRead lowers a standalone field/property access `recv.field` to an
// lowerCharLit lowers a character literal `'a'` to an i64 constant holding its
// codepoint (Nolang char is a 64-bit codepoint, matching KindChar -> i64 in
// codegen).
func (l *lowerer) lowerCharLit(n *hir.Node) ValueID {
	s := strings.Trim(l.pkg.Str(n.S), "'")
	var cp int64
	for _, r := range s {
		cp = int64(r)
		break
	}
	return l.b.EmitInt(OpConst, l.b.Type("char"), cp, "")
}

// lowerNilLit lowers `nil` to a zero constant of the inferred (option) type. The
// type is usually absent on the node itself; when present (e.g. a typed `?T`
// context) it is used so later field/use analysis has a concrete type to work
// with. v1 emits a zero value; option-layout codegen falls back to legacy.
func (l *lowerer) lowerNilLit(n *hir.Node) ValueID {
	t := l.typeOfNode(n)
	if t == NoType || t == l.voidType {
		t = l.b.Type("i64")
	}
	// nolang `nil` assigned to an option-typed binding (`val = nil`, val : ?T,
	// or `return nil` from an ?T function) is the option's "none" discriminant.
	// Emit it as an %option constant so the later move/store is type-correct: a
	// bare i64 would be misread as the tag field by the %option load/store
	// codegen, turning none into some(0). Only switch when the surrounding
	// context is an option type, so non-option `nil` (e.g. for %err_error) keeps
	// its own representation.
	if ht := l.typeHint; ht != NoType && ht != l.voidType {
		if tt := l.mod.Type(ht); tt != nil && tt.Kind == KindOption {
			t = ht
		}
	}
	return l.b.EmitInt(OpConst, t, 0, "")
}

// lowerStructLit lowers `T{ f: v, ... }` into a struct value: allocate the
// struct, then store each field initializer into it via OpSetField. The result
// value is the (addressable) struct slot, so later field reads GEP into it.
func (l *lowerer) lowerStructLit(n *hir.Node) ValueID {
	raw := l.pkg.Str(n.S)
	typ := l.b.Type(raw)
	res := l.b.Emit(OpStructLit, typ, nil, raw)
	for _, fID := range l.pkg.Children(n.Id) {
		fn := l.pkg.Node(fID)
		if fn == nil || fn.Kind != hir.KStructField {
			continue
		}
		fieldName := l.pkg.Str(fn.S)
		var valID int32
		for _, c := range l.pkg.Children(fID) {
			valID = c
			break
		}
		vv := l.lowerExpr(valID)
		if vv == NoVal {
			continue
		}
		// Store the field name in inst.Str (not inst.Sym) so emitSetField's
		// FieldIndex lookup matches the GETFIELD convention in lowerDotRead.
		sid := l.b.EmitVoid(OpSetField, []ValueID{res, vv}, "")
		l.mod.Insts[sid].Str = fieldName
	}
	return res
}

// lowerSlice lowers `a[lo:hi]` (KSlice: First = [array, RangeExpression]) into an
// OpSliceOp. Open bounds (missing start/end) lower to NoVal operands; codegen
// lowers the sub-range for vec/array receivers and falls back to legacy for types
// it does not yet lay out.
func (l *lowerer) lowerSlice(n *hir.Node) ValueID {
	var leftID, rangeID int32
	i := 0
	for _, c := range l.pkg.Children(n.Id) {
		if i == 0 {
			leftID = c
		} else {
			rangeID = c
			break
		}
		i++
	}
	arrV := l.lowerExpr(leftID)
	if arrV == NoVal {
		return NoVal
	}
	var loV, hiV ValueID = NoVal, NoVal
	var leftInc, rightInc bool = true, true
	if rangeID != hir.NoID {
		rn := l.pkg.Node(rangeID)
		if rn != nil {
			leftInc = rn.Has(hir.FlagLeftInc)
			rightInc = rn.Has(hir.FlagRightInc)
			for _, c := range l.pkg.Children(rangeID) {
				cn := l.pkg.Node(c)
				if cn == nil {
					continue
				}
				switch l.pkg.Str(cn.S) {
				case "start":
					loV = l.lowerExpr(cn.First)
				case "end":
					hiV = l.lowerExpr(cn.First)
				}
			}
		}
	}
	args := []ValueID{arrV}
	if loV != NoVal {
		// Exclusive lower bound `(` excludes the start index -> start+1.
		// Inclusive `[` keeps it; an absent lower bound defaults to 0.
		if !leftInc {
			one := l.b.EmitInt(OpConst, l.b.Type("i64"), 1, "")
			loV = l.b.Emit(OpAdd, l.b.Type("i64"), []ValueID{loV, one}, "")
		}
		args = append(args, loV)
	} else {
		args = append(args, l.b.EmitInt(OpConst, l.b.Type("i64"), 0, ""))
	}
	if hiV != NoVal {
		// Inclusive upper bound `]` includes the end index -> end+1 (the slice
		// op takes an EXCLUSIVE upper bound). Exclusive `)` keeps it; an absent
		// upper bound defaults to the container length.
		if rightInc {
			one := l.b.EmitInt(OpConst, l.b.Type("i64"), 1, "")
			hiV = l.b.Emit(OpAdd, l.b.Type("i64"), []ValueID{hiV, one}, "")
		}
		args = append(args, hiV)
	} else {
		// open upper bound: use the container length
		args = append(args, l.b.Emit(OpLen, l.b.Type("i64"), []ValueID{arrV}, ""))
	}
	resTyp := l.typeOfNode(n)
	if resTyp == NoType || resTyp == l.voidType {
		// typeOfNode(KSlice) returns void when the receiver's declared type is
		// unreachable from the KIdent node (the declared type lives on the KLet,
		// not on every KIdent reference). Derive the slice result type from the
		// lowered receiver value instead: a fixed array [N]Elem slices to []Elem.
		// Without this the result collapsed to the bare element type (i64), so
		// b[0]'s index result was void-typed and got no alloca slot -> "index
		// dst slot" (tests/arr-slice.no). Mirrors the KindArray->[]Elem conversion
		// below for the reachable-type path.
		if rt := l.valueTypeOf(arrV); rt != NoType && rt != l.voidType {
			if rty := l.mod.Type(rt); rty != nil && rty.Kind == KindArray {
				if _, elemRaw, ok := parseArray(rty.Raw); ok {
					resTyp = l.b.Type("[]" + elemRaw)
				}
			} else if rty != nil && rty.Kind == KindSlice {
				resTyp = rt
			}
		}
	}
	if resTyp == NoType || resTyp == l.voidType {
		resTyp = l.b.Type("i64")
	} else if t := l.mod.Type(resTyp); t != nil && t.Kind == KindArray {
		// Slicing a fixed array yields a SLICE (vec), never a fixed array. The
		// HIR node type is the array type ([N]Elem), but the slice result must
		// be []Elem so codegen builds a heap %vec (matching the legacy backend).
		// Without this, emitSliceOp sees a fixed-array destination, copies the
		// element bytes into a stack slot, and the subsequent move into the
		// []i64 result reinterpreters those bytes as a %vec header — leaving a
		// garbage data pointer that aborts (trace/BPT trap) on first use.
		if _, elemRaw, ok := parseArray(t.Raw); ok {
			resTyp = l.b.Type("[]" + elemRaw)
		}
	}
	return l.b.Emit(OpSliceOp, resTyp, args, "")
}

// OpGetField instruction. The receiver is either the KDot's single child (explicit
// `recv.field`) or, when the KDot has no child, the enclosing method's implicit
// `self` (captured as l.curRecv / the `self` local). The field name is the KDot's
// S string and is carried on the instruction's Str so codegen can resolve the
// struct field index.
// typeConstantValue resolves a primitive integer type's MIN/MAX compile-time
// constant (e.g. `i8.MIN`, `u32.MAX`). nolang integer types are all i64 in the
// MIR backend, so the value is returned as an i64. Returns (0,false) when the
// receiver is not a primitive integer type or the field is not MIN/MAX.
func typeConstantValue(typeName, field string) (int64, bool) {
	if field != "MIN" && field != "MAX" {
		return 0, false
	}
	var minv, maxv int64
	switch typeName {
	case "i8":
		minv, maxv = -128, 127
	case "u8", "byte":
		minv, maxv = 0, 255
	case "i16":
		minv, maxv = -32768, 32767
	case "u16":
		minv, maxv = 0, 65535
	case "i32":
		minv, maxv = -2147483648, 2147483647
	case "u32":
		minv, maxv = 0, 4294967295
	case "i64":
		minv, maxv = -9223372036854775808, 9223372036854775807
	case "u64":
		// u64.MAX (18446744073709551615) exceeds i64 range; clamp to the
		// largest representable i64. No corpus test exercises u64.MAX, and
		// the MIR backend has no unsigned-64 storage, so this is the safe
		// best-effort value.
		minv, maxv = 0, 9223372036854775807
	default:
		return 0, false
	}
	if field == "MIN" {
		return minv, true
	}
	return maxv, true
}

func (l *lowerer) lowerDotRead(n *hir.Node) ValueID {
	fieldName := l.pkg.Str(n.S)

	var recvID int32
	for _, c := range l.pkg.Children(n.Id) {
		recvID = c
		break
	}

	// Type-level constant access: `i8.MIN`, `u32.MAX`, ... The receiver is the
	// PRIMITIVE TYPE NAME (a KIdent like `i8`), not a value, so it has no slot
	// to read a field from. Emit the compile-time constant directly. Without
	// this, `m = i8.MIN` resolved `i8` as an (unbound) identifier, returned
	// NoVal, and the top-level global `m` was left uninitialized -> `undef`
	// -> opt-inserted trap at the `m == -128` comparison (test-i8-debug2).
	if recvID != hir.NoID {
		if rn := l.pkg.Node(recvID); rn != nil && rn.Kind == hir.KIdent {
			if v, ok := typeConstantValue(l.pkg.Str(rn.S), fieldName); ok {
				return l.b.EmitInt(OpConst, l.b.Type("i64"), v, "")
			}
		}
	}

	var recvV ValueID
	switch {
	case recvID != hir.NoID:
		recvV = l.lowerExpr(recvID)
	case l.curRecv != NoVal:
		recvV = l.curRecv
	case l.locals["self"] != NoVal:
		recvV = l.locals["self"]
	}
	if recvV == NoVal {
		l.unsupported(l.curFuncName(), "dot", "field "+fieldName+": no receiver (not in a method body)")
		return NoVal
	}

	recvRaw := ""
	recvT := l.valueTypeOf(recvV)
	if t := l.mod.Type(recvT); t != nil {
		recvRaw = t.Raw
	}
	if recvRaw == "" {
		l.unsupported(l.curFuncName(), "dot", "field "+fieldName+": cannot determine receiver type")
		return NoVal
	}

	// Container builtin properties: `.len` / `.cap` are properties of the
	// container itself, not struct fields. There is one such layout per
	// element type (`[]byte`, `[]i64`, `[3]i64`, ...), so a name-keyed
	// StructFields lookup can never resolve them — that mismatch alone
	// accounted for every "no struct layout for []byte (field len)" gap on the
	// corpus. Dispatch on the type KIND and emit a dedicated op instead.
	if ty := l.mod.Type(recvT); ty != nil {
		if fieldName == "len" || fieldName == "cap" {
			switch {
			case ty.Kind == KindStr || ty.Kind == KindSlice || ty.Kind == KindArray || ty.Kind == KindMap:
				op := OpLen
				if fieldName == "cap" {
					op = OpCap
				}
				return l.b.Emit(op, l.b.Type("i64"), []ValueID{recvV}, "")
			case recvRaw == "txt" && fieldName == "len":
				// txt.len reads the i8 len byte and zero-extends to i64 (legacy
				// semantics); cap is not meaningful for a fixed buffer.
				return l.b.Emit(OpLen, l.b.Type("i64"), []ValueID{recvV}, "")
			}
		}
	}
	fields, ok := l.mod.StructFields[recvRaw]
	if !ok {
		// std structs are registered under their module-qualified raw name
		// (e.g. `os.utsname`), but a field read on a local binding only knows
		// the bare type name (`utsname`). Resolve the bare name to its
		// qualified key by suffix so `uts.sysname` finds `os.utsname`, matching
		// the structKeyOf resolution already done in emitGetField.
		for k := range l.mod.StructFields {
			if k == recvRaw || strings.HasSuffix(k, "."+recvRaw) {
				recvRaw = k
				fields, ok = l.mod.StructFields[k]
				break
			}
		}
	}
	if !ok {
		l.unsupported(l.curFuncName(), "dot", "no struct layout for "+recvRaw+" (field "+fieldName+")")
		return NoVal
	}
	idx := -1
	var fieldTypeRaw string
	for i, f := range fields {
		if f.Name == fieldName {
			idx = i
			fieldTypeRaw = f.TypeRaw
			break
		}
	}
	if idx < 0 {
		l.unsupported(l.curFuncName(), "dot", "field "+fieldName+" not found on "+recvRaw)
		return NoVal
	}
	fieldT := l.b.Type(fieldTypeRaw)
	v := l.b.Emit(OpGetField, fieldT, []ValueID{recvV}, "")
	// carry the field name on the instruction for codegen index resolution
	l.mod.Insts[len(l.mod.Insts)-1].Str = fieldName
	return v
}

// canonSliceRecv maps a concrete slice/array receiver type name (e.g.
// "[]i64", "[3]i64", "[]byte", "?[]i64") to the generic "[]t" used to key
// slice-method builtins and the generated `[]t.*` HIR funcs. Non-slice names
// pass through unchanged.
//
// Why: method calls on a slice must resolve to the SAME symbol regardless of the
// element type, because the slice methods are element-type-generic. The legacy
// backend resolves `data.clear()` / `data.push(x)` against the `vec.*` builtins;
// the MIR table mirrors that under the canonical `[]t.<method>` key, so any
// `[]i64.clear` / `[3]i64.reverse` / `[]byte.push` call lowers to `[]t.clear`
// / `[]t.reverse` / `[]t.push` and finds its builtin.
func canonSliceRecv(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return name
	}
	recv := strings.TrimPrefix(name[:dot], "?")
	meth := name[dot:] // includes the leading "."
	if strings.HasPrefix(recv, "[]") {
		return "[]t" + meth
	}
	if strings.HasPrefix(recv, "[") && strings.Contains(recv, "]") {
		return "[]t" + meth
	}
	return name
}

// resolveCallee determines the callee symbol and (for a method call) the
// receiver value of a KCall node. Shared by the single-result expression path
// and the multi-assign statement path.
func (l *lowerer) resolveCallee(n *hir.Node) (callee string, recvV ValueID) {
	// callee: fn slot (KIdent or KDot)
	fnID := l.slot(n.Id, "fn")
	if fnID == hir.NoID {
		return "", NoVal
	}
	fnn := l.pkg.Node(fnID)
	if fnn == nil {
		return "", NoVal
	}
	switch fnn.Kind {
	case hir.KIdent:
		name := l.pkg.Str(fnn.S)
		// Implicit-self method call. nolang lowers `.emit(...)` (called from
		// inside `regexp.regexp.compile`) to a bare KIdent `regexp.regexp.emit`
		// with NO receiver child. The MIR function `regexp.regexp.emit`, however,
		// is a method on the module instance and declares a `self` first param.
		// Inside a method (`self` = l.curRecv), a bare qualified call
		// `Type.method` whose Type prefix matches the receiver's type is that
		// same `self.method(args)` — prepend the current receiver as the first
		// argument. Free functions like `fmt.int` / `u64-to-str` have a Type
		// prefix that never equals a value's receiver type, so they are left
		// receiverless.
		if l.curRecv != NoVal {
			if dot := strings.LastIndex(name, "."); dot > 0 {
				tname := name[:dot]
				recvRaw := ""
				if ty := l.mod.Type(l.valueTypeOf(l.curRecv)); ty != nil && ty.Raw != "" {
					recvRaw = ty.Raw
				}
			if recvRaw != "" && tname == recvRaw {
				return canonSliceRecv(name), l.curRecv
			}
		}
	}
	return canonSliceRecv(name), NoVal
	case hir.KDot:
		method := l.pkg.Str(fnn.S) // property name, e.g. "to-bytes"
		// The receiver is the KDot's single child (e.Receiver).
		var recvID int32
		for _, c := range l.pkg.Children(fnID) {
			recvID = c
			break
		}
		// A bare identifier that is NOT a bound local is a MODULE namespace,
		// not a value: `number.rotate-left(t, 7)`, `net.send(fd, buf, n)`,
		// `fs.open(path)`. Lowering those as a method call tried to evaluate
		// the module name as a value and reported "unresolved identifier
		// <module>" — a large share of the remaining ident gaps. They lower to
		// a plain qualified call with NO receiver argument.
		if rn := l.pkg.Node(recvID); rn != nil && rn.Kind == hir.KIdent {
			if recvName := l.pkg.Str(rn.S); recvName != "" {
				if _, bound := l.locals[recvName]; !bound {
					if _, gbound := l.globals[recvName]; !gbound {
						// A module namespace (`fs`, `os`, `net`, ...) is NOT a
						// bound value: lower to a plain qualified call with NO
						// receiver argument. Crucially, do NOT prepend the
						// enclosing method's `self` type: inside `path.path.is-file`
						// the call `fs.is_file(self.p)` has receiver child `fs`
						// (a module), so its callee must stay `fs.is-file`. The
						// old code rewrote it to `path.path.is-file` (using self's
						// type) which resolved to the enclosing function itself ->
						// infinite recursion -> stack-overflow SIGSEGV at runtime
						// (test-path_char). The implicit-self case
						// (`regexp.regexp.emit`) is a BARE KIdent with no receiver
						// child and is handled by the KIdent branch above.
						return recvName + "." + method, NoVal
					}
					// recvName is a bound GLOBAL (e.g. a top-level `data [4]i64`
					// or `v []str`); fall through to the method-call path below
					// so it is lowered as a real receiver value, not a module.
				}
			}
		}
		// Method call: `obj.method(args)` lowers to `ReceiverType.method` with
		// the receiver passed as the FIRST argument (by pointer for
		// owned/struct receivers, matching the legacy ABI:
		//   call @str.to-bytes(%str-long* %recv, ...)).
		rv := l.lowerExpr(recvID)
		if rv == NoVal {
			return "", NoVal
		}
		// Receiver type name: use the nolang type of the receiver value. For a
		// `str` receiver this is "str"; for `io.writer` it is "io.writer",
		// etc. The callee is then "str.to-bytes".
		recvT := l.valueTypeOf(rv)
		recvTypeName := "str"
		if ty := l.mod.Type(recvT); ty != nil && ty.Raw != "" {
			recvTypeName = ty.Raw
		}
	// Optionals wrap their inner type with a leading '?'
	// (e.g. `?i64`). A method call on the unwrapped value of an optional
	// (`v.to-str()` where v: ?i64) would otherwise form the callee
	// "?i64.to-str", which does not exist — the method table holds
	// "i64.to-str". Strip the marker so the callee matches the legacy
	// backend, which resolves optional receivers to their inner type.
	recvTypeName = strings.TrimPrefix(recvTypeName, "?")
	// A slice/array method that the builtin table supplies for a vec/array
	// receiver must be emitted as that BUILTIN, not as a call to the generic
	// stub (see sliceMethodBuiltin). Otherwise `v.len()` lowers to
	// `[]t.len` -> `_xt.len` -> `self.len` -> `[]t.len` ...: infinite
	// recursion, which is a stack-overflow SIGSEGV at runtime, not a compile
	// error. This single mis-resolution accounted for ~20 of the 37 MIR=3
	// runtime crashes in the corpus sweep.
	if bm := sliceMethodBuiltin(recvTypeName, method); bm != "" {
		return bm, rv
	}
	return canonSliceRecv(recvTypeName + "." + method), rv
	}
	return "", NoVal
}

// sliceMethodBuiltin reports the BARE builtin method name to use for a
// slice/array-receiver method call, or "" when the ordinary qualified call
// must be kept.
//
// Why it exists: nolang materializes a generic slice method as a chain of
// compiler-generated forwarding stubs with no real body —
//
//	[]i64.len  ->  _xi64.len  ->  self.len
//	                             ^ canonSliceRecv -> "[]t.len"
//	[]t.len    ->  _xt.len    ->  self.len  -> "[]t.len"  (cycle!)
//
// `[]t.len` is a real function in the HIR package, so codegen prefers it over
// the bare-name builtin `len` (ForwardFunc vec-len) and emits a self-recursive
// call. Because the chain is generic it is invisible at the call site: the
// only symptom is a runtime stack overflow.
//
// Resolution rule (deliberately narrow so it can never shadow user code):
//   - receiver must be a slice `[]T` or fixed array `[N]T`;
//   - an EXACT `[]t.<method>` builtin entry (vec.push, []t.pop, ...) already
//     routes correctly, so keep the qualified name;
//   - otherwise, if the bare `<method>` is a vec/array-receiver builtin with a
//     ForwardFunc, return it so codegen emits the builtin inline.
func sliceMethodBuiltin(recvTypeName, method string) string {
	if recvTypeName == "" || method == "" {
		return ""
	}
	if !strings.HasPrefix(recvTypeName, "[]") && !strings.HasPrefix(recvTypeName, "[") {
		return ""
	}
	if bm := builtin.FindBuiltinMethod("[]t." + method); bm != nil {
		return ""
	}
	// NOTE: FindBuiltinMethod returns the FIRST entry with that method name,
	// which for `len` is the str one (ForwardFunc str-len) — the vec entry
	// (ForwardFunc vec-len) is registered later. A slice receiver must get the
	// vec/array entry, so scan for the receiver kind explicitly.
	for i := range builtin.BuiltinMethodList {
		bm := &builtin.BuiltinMethodList[i]
		if bm.MethodName != method || bm.ForwardFunc == "" {
			continue
		}
		if bm.ReceiverType == builtin.ReceiverVec || bm.ReceiverType == builtin.ReceiverArr {
			return method
		}
	}
	return ""
}

func (l *lowerer) lowerCall(n *hir.Node) ValueID {
	callee, recvV := l.resolveCallee(n)
	if callee == "" {
		// Multi-assign shape: nolang lowers `a, b = f()` to a KCall whose
		// `fn` slot is the inner call and whose `arg` slots are the LHS target
		// identifiers (`tmp-name, tmp-fd = fs.mkstemp(...)`). resolveCallee
		// reads the `fn` slot as a callee and finds a call node, not an
		// ident/dot, so it returns "". Redirect to the multi-assign path
		// instead of emitting a call with an empty callee (which crashes codegen
		// with "no callee").
		if fnID := l.slot(n.Id, "fn"); fnID != hir.NoID {
			if fnn := l.pkg.Node(fnID); fnn != nil && (fnn.Kind == hir.KCall || fnn.Kind == hir.KDot) {
				return l.lowerMultiAssignCall(n, fnID)
			}
		}
		return NoVal
	}
	// reachability: enqueue the referenced function for lowering
	l.enqueueCallee(callee)

	argv := l.lowerCallArgs(n, recvV)
	// The KCall node carries NO type — the AST CallExpression has no Type field,
	// so InferredType/KType are both empty for it. Derive the result type from
	// the callee's signature, in three tiers:
	//   1. a KFuncDef/KExtern in the HIR package (its KResult child),
	//   2. the builtin signature table (print/sqrt/str.to-bytes/...), which is
	//      authoritative for the ~190 builtins that have no HIR definition,
	//   3. the LHS type hint, for builtins whose result type is inferred from
	//      the assignment (with-len/with-cap/with-cap-len).
	// Only when all three come up empty is the call genuinely void.
	resTyp := l.resultTypeOfCallee(callee)
	if resTyp == l.voidType {
		if br := builtinResultOf(callee); br.Known {
			switch {
			case br.Raw != "":
				resTyp = l.b.Type(br.Raw)
			case br.LHSInferred && l.typeHint != NoType && l.typeHint != l.voidType:
				resTyp = l.typeHint
			}
		}
	}
	// Multi-result builtins (`stat-size` -> (i64, bool), `readlink` ->
	// (str, bool), `mkstemp` -> (str, fd)) declare more than one Return entry.
	// Allocate a destination per entry so `path, ok = fs.readlink(p)` binds both
	// names; a single-destination lowering leaves the second reading an
	// uninitialized slot.
	if resTyp != l.voidType {
		var brs []string
		if _, inHIR := l.funcNames[callee]; !inHIR {
			brs = builtinResultTypes(callee)
		}
		if len(brs) > 1 {
			types := make([]TypeID, 0, len(brs))
			for _, raw := range brs {
				types = append(types, l.b.Type(raw))
			}
			dsts := l.b.EmitCallMulti(types, argv, callee)
			if len(dsts) == 0 {
				return NoVal
			}
			return dsts[0]
		}
	}
	if resTyp == l.voidType {
		// void call: emit without a destination value (statement, not expression)
		l.b.EmitVoid(OpCall, argv, callee)
		return NoVal
	}
	// A single-result call uses the SAME out-parameter convention as a
	// multi-result call: the callee writes its result into a caller-passed
	// out-pointer (Function.ResultParams). EmitCallMulti allocates the result
	// value(s) and records them in inst.Results so emitCall (codegen) can load
	// the out-pointer back into the result slot. Using bare Emit() would set
	// inst.Dst but leave inst.Results empty, so emitCall would never store the
	// returned value — every `x = f()` would read an uninitialized slot (0 /
	// garbage). Routing through EmitCallMulti keeps the single- and multi-result
	// paths identical and correct.
	dsts := l.b.EmitCallMulti([]TypeID{resTyp}, argv, callee)
	if len(dsts) == 0 {
		return NoVal
	}
	return dsts[0]
}

// resultTypeOfCallee derives the return type of a called function from its
// signature in the HIR package. Because the KCall node carries no type (the AST
// CallExpression has no Type field, so PopulateInferredTypes never records one),
// we read the callee's KResult child type directly. Unknown callees (builtins
// like print that are not in the HIR function table) return voidType, which is
// correct for void calls.
func (l *lowerer) resultTypeOfCallee(callee string) TypeID {
	if callee == "" {
		return l.voidType
	}
	id, ok := l.funcNames[callee]
	if !ok {
		return l.voidType
	}
	n := l.pkg.Node(id)
	if n == nil {
		return l.voidType
	}
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KResult {
			t := l.typeOfNode(cn)
			if t != l.voidType {
				return t
			}
		}
	}
	return l.voidType
}

// resultTypesOfCallee returns EVERY declared result type of a called function,
// in declaration order. A Nolang function may return several values
// (`f = () (a i64, b i64)`), which the caller receives via a multi-assign
// (`x, y = f()`); each result becomes an out-parameter at the LLVM level.
func (l *lowerer) resultTypesOfCallee(callee string) []TypeID {
	if callee == "" {
		return nil
	}
	id, ok := l.funcNames[callee]
	if !ok {
		return nil
	}
	var out []TypeID
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KResult {
			out = append(out, l.typeOfNode(cn))
		}
	}
	return out
}

// lowerMultiAssign lowers `a, b = f()` — a statement with one `value` slot and
// N `target` slots. It is how Nolang receives a multi-value return.
//
// The call itself is emitted ONCE with one result value per declared result;
// each target name is then bound to its corresponding result. Lowering this
// statement used to be unsupported, which left every target unbound and turned
// each later read of `a` / `b` into an "unresolved identifier" gap.
func (l *lowerer) lowerMultiAssign(id int32) {
	var valueID int32 = hir.NoID
	var targets []int32
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn == nil || cn.Kind != hir.KSlot {
			continue
		}
		switch l.pkg.Str(cn.S) {
		case "value":
			valueID = cn.First
		case "target":
			if cn.First != hir.NoID {
				targets = append(targets, cn.First)
			}
		}
	}
	if valueID == hir.NoID || len(targets) == 0 {
		l.unsupported(l.curFuncName(), "multi-assign", "malformed multi-assign")
		return
	}
	vn := l.pkg.Node(valueID)
	if vn == nil {
		l.unsupported(l.curFuncName(), "multi-assign", "missing value node")
		return
	}
	if vn.Kind != hir.KCall {
		// Not a call: only a single value is available, so a 1-target form can
		// reuse the plain assignment path (`a = <expr>`).
		if len(targets) == 1 {
			l.lowerAssignTargets([]int32{valueID}, targets)
			return
		}
		l.unsupported(l.curFuncName(), "multi-assign", "unsupported value kind "+hir.KindNames[vn.Kind])
		return
	}

	callee, recvV := l.resolveCallee(vn)
	if callee == "" {
		l.unsupported(l.curFuncName(), "multi-assign", "unresolved callee")
		return
	}
	l.enqueueCallee(callee)

	resTypes := l.resultTypesOfCallee(callee)
	if len(resTypes) == 0 {
		// Builtins have no HIR definition, so resultTypesOfCallee finds nothing.
		// Their signature lives in the builtin table instead: consult it before
		// giving up, or every `size, ok = fs.stat-size(p)` would bind both
		// targets to zero placeholders and silently produce wrong numbers.
		if _, inHIR := l.funcNames[callee]; !inHIR {
			for _, raw := range builtinResultTypes(callee) {
				resTypes = append(resTypes, l.b.Type(raw))
			}
		}
	}
	if len(resTypes) == 0 {
		// Unknown/void callee: emit a plain void call and give each target a
		// zero-initialized placeholder so later reads still resolve.
		l.lowerCallArgs(vn, recvV)
		for _, t := range targets {
			l.bindPlaceholder(t)
		}
		return
	}

	argv := l.lowerCallArgs(vn, recvV)
	dsts := l.b.EmitCallMulti(resTypes, argv, callee)
	for i, t := range targets {
		if i >= len(dsts) {
			break
		}
		l.bindTarget(t, dsts[i])
	}
}

// lowerMultiAssignCall lowers the `a, b = f()` KCall shape: `n` is the outer
// call whose `fn` slot (innerCallID) is the actual callee and whose `arg`
// slots are the LHS target identifiers. It emits the inner call once with one
// result per declared return and binds each target to its result — identical to
// lowerMultiAssign, but for the KCall representation (lowerMultiAssign handles
// the KMultiAssign statement kind, which this program does not always produce).
func (l *lowerer) lowerMultiAssignCall(n *hir.Node, innerCallID int32) ValueID {
	inner := l.pkg.Node(innerCallID)
	if inner == nil {
		return NoVal
	}
	callee, recvV := l.resolveCallee(inner)
	if callee == "" {
		return NoVal
	}
	l.enqueueCallee(callee)

	targets := l.slotArgs(n.Id, "arg")
	argv := l.lowerCallArgs(inner, recvV)

	resTypes := l.resultTypesOfCallee(callee)
	if len(resTypes) == 0 {
		// Builtins have no HIR definition; consult the builtin table before
		// giving up, or `name, ok = fs.mkdtemp(p)` would bind both targets to
		// zero placeholders.
		if _, inHIR := l.funcNames[callee]; !inHIR {
			for _, raw := range builtinResultTypes(callee) {
				resTypes = append(resTypes, l.b.Type(raw))
			}
		}
	}
	if len(resTypes) == 0 {
		l.lowerCallArgs(inner, recvV)
		for _, t := range targets {
			l.bindPlaceholder(t)
		}
		return NoVal
	}
	dsts := l.b.EmitCallMulti(resTypes, argv, callee)
	for i, t := range targets {
		if i >= len(dsts) {
			break
		}
		l.bindTarget(t, dsts[i])
	}
	return NoVal
}

// lowerCallArgs lowers the `arg` slots of a call node and prepends an explicit
// receiver when the call is a method call.
func (l *lowerer) lowerCallArgs(n *hir.Node, recvV ValueID) []ValueID {
	args := l.slotArgs(n.Id, "arg")
	var argv []ValueID
	if recvV != NoVal {
		// nolang's implicit-self convention: a bare `.method(args)` call inside a
		// method already lists the receiver as the FIRST argument node (an ident
		// named "self") in the HIR call args. Prepending recvV again would
		// double the receiver (e.g. `regexp.regexp.emit` would get [self, self,
		// op, arg1, arg2]). Only prepend when the HIR has NOT already supplied
		// the receiver — i.e. explicit-receiver calls (`obj.method()`) whose
		// first arg node is a real value, not the synthetic "self" ident.
		firstIsSelf := false
		if len(args) > 0 {
			if an := l.pkg.Node(args[0]); an != nil && an.Kind == hir.KIdent && l.pkg.Str(an.S) == "self" {
				firstIsSelf = true
			}
		}
		if !firstIsSelf {
			argv = append(argv, recvV)
		}
	}
	for _, a := range args {
		argv = append(argv, l.lowerExpr(a))
	}
	return argv
}

// bindTarget binds a multi-assign target node (normally a KIdent) to the given
// MIR value. An already-bound name is written into its existing slot (a
// reassignment) rather than rebound, matching the single-assign path.
func (l *lowerer) bindTarget(targetID int32, v ValueID) {
	tn := l.pkg.Node(targetID)
	if tn == nil {
		return
	}
	if tn.Kind != hir.KIdent {
		l.unsupported(l.curFuncName(), "multi-assign", "unsupported target kind "+hir.KindNames[tn.Kind])
		return
	}
	nm := l.pkg.Str(tn.S)
	if v == NoVal {
		l.bindPlaceholder(targetID)
		return
	}
	if existing, ok := l.locals[nm]; ok {
		if l.isOwnedLocal(existing) {
			// Owned reassignment: free the old value, move the new one in. See
			// lowerAssignNode for the memory-safety rationale.
			l.b.EmitVoid(OpDrop, []ValueID{existing}, "")
		}
		l.b.EmitMoveInto(existing, v)
		return
	}
	l.locals[nm] = v
}

// bindPlaceholder binds a target name to a zero-initialized value of its
// declared type. Used when a multi-assign's callee has no known result types,
// so the target still resolves for later reads instead of cascading into
// "unresolved identifier" for the rest of the function.
func (l *lowerer) bindPlaceholder(targetID int32) {
	tn := l.pkg.Node(targetID)
	if tn == nil || tn.Kind != hir.KIdent {
		return
	}
	nm := l.pkg.Str(tn.S)
	if _, ok := l.locals[nm]; ok {
		return
	}
	t := l.typeOfNode(tn)
	if t == NoType || t == l.voidType {
		t = l.b.Type("i64")
	}
	l.locals[nm] = l.b.EmitInt(OpConst, t, 0, "")
}

// lowerAssignTargets binds N target nodes from N value expressions. Used for
// the non-call form of a multi-assign.
func (l *lowerer) lowerAssignTargets(valueIDs, targets []int32) {
	for i, t := range targets {
		if i >= len(valueIDs) {
			break
		}
		l.bindTarget(t, l.lowerExpr(valueIDs[i]))
	}
}

// enqueueCallee schedules a referenced function for lowering (reachability).
func (l *lowerer) enqueueCallee(callee string) {
	if callee == "" {
		return
	}
	if id, ok := l.funcNames[callee]; ok && !l.lowered[callee] {
		already := false
		for _, w := range l.worklist {
			if w == callee {
				already = true
				break
			}
		}
		if !already {
			l.worklist = append(l.worklist, callee)
			_ = id
		}
	}
}

// isOwnedLocal reports whether v is an owned (heap-owning) value that is local to
// the current function — i.e. defined by an instruction, not a parameter. Params
// are borrowed (owned by the caller), so assignment to an owned local is the only
// reassignment case that would leak under the v1 move model.
func (l *lowerer) isOwnedLocal(v ValueID) bool {
	if v <= NoVal {
		return false
	}
	f := l.mod.Func(l.curFunc)
	if f == nil {
		return false
	}
	for _, p := range f.Params {
		if p == v {
			return false
		}
	}
	return l.mod.isOwnedVal(f, v)
}

// elementTypeOf returns the element type of a value that is an array/slice/option.
// Used to type the result of an index expression or the target of an index store.
func (l *lowerer) elementTypeOf(v ValueID) TypeID {
	if v <= NoVal {
		return l.voidType
	}
	var t TypeID
	if f := l.mod.Func(l.curFunc); f != nil {
		if tt, ok := f.LocalTypes[v]; ok {
			t = tt
		}
	}
	if t == NoType {
		if val := l.mod.Value(v); val != nil {
			t = val.Type
		}
	}
	if t == NoType {
		return l.voidType
	}
	ty := l.mod.Type(t)
	if ty != nil {
		// A str is `{len, cap, i8* data}`; indexing a str yields a single byte
		// (i8), not a struct element. Without this, `s[i]` lowered to void and
		// every string-builder idiom (to-upper / replace / repeat / ...) that
		// writes bytes in place produced `store void` IR that opt rejected.
		if ty.Raw == "str" {
			return l.b.Type("i8")
		}
		if ty.Elem != NoType {
			return ty.Elem
		}
		// Defensive fallback: some array/slice types in the table lack a
		// registered Elem (e.g. a slice sliced from a fixed array, whose type
		// is `[]Elem` but Elem was never set). Derive the element from the raw
		// type string so `b[0]` is typed `Elem` (i64 for []i64) instead of
		// void. Without this the index result is void-typed, gets no alloca
		// slot, and codegen fails with "index dst slot" (tests/arr-slice.no).
		// Mirrors codegen.elemTypeOfReceiver's defensive path. Only reached
		// when Elem is missing, so it can never change a currently-correct
		// (non-void) element type.
		if e, ok := arrayElemRaw(ty.Raw); ok {
			if et := l.b.Type(e); et != l.voidType {
				return et
			}
		}
		if e, ok := parseSliceElem(ty.Raw); ok {
			if et := l.b.Type(e); et != l.voidType {
				return et
			}
		}
	}
	return l.voidType
}

// lowerAssignNode lowers an assignment node whose first child is the target and
// second child is the value. It supports both identifier targets (`x = v`) and
// indexed targets (`a[i] = v`). It returns the value's MIR value so an assignment
// used as an expression (e.g. wrapped in KExprStmt, or `if (x = f())`) yields its
// right-hand side.
func (l *lowerer) lowerAssignNode(assignID int32) ValueID {
	var target, value int32
	count := 0
	for _, c := range l.pkg.Children(assignID) {
		if count == 0 {
			target = c
			count++
		} else {
			value = c
			break
		}
	}
	tn := l.pkg.Node(target)
	if tn == nil {
		l.unsupported(l.curFuncName(), "assign", "assignment with no target")
		return NoVal
	}
	// Publish the assignment TARGET's type as a hint while lowering the RHS.
	// Builtins like with-len/with-cap leave the result type to the LHS, and the
	// dominant std idiom is an assignment rather than a declaration:
	//
	//	str.to-upper = () (val str) { val = with-cap(.len) ... }
	//
	// Without the hint the call lowers to void, the assignment binds nothing and
	// every later read of `val` cascades into "unresolved identifier".
	if tn.Kind == hir.KIdent {
		if nm := l.pkg.Str(tn.S); nm != "" {
			if slot, ok := l.locals[nm]; ok {
				if t := l.valueTypeOf(slot); t != NoType && t != l.voidType {
					l.typeHint = t
				}
			}
		}
	}
	v := l.lowerExpr(value)
	l.typeHint = NoType
	if v == NoVal {
		return NoVal
	}
	switch tn.Kind {
	case hir.KIdent:
		nm := l.pkg.Str(tn.S)
		slot, ok := l.locals[nm]
		if !ok {
			l.unsupported(l.curFuncName(), "assign", "assignment to unresolved identifier "+nm)
			return NoVal
		}
		if l.isOwnedLocal(slot) {
			// Owned reassignment: the old value must be freed exactly once, then
			// the new value is moved into the existing slot (ownership transfer,
			// so the new value is exempt from the slot's own drop). The memory
			// analysis inserts a drop for the slot at every exit path, which frees
			// the NEW content; the manual drop here frees the OLD content. So each
			// physical allocation is freed exactly once.
			l.b.EmitVoid(OpDrop, []ValueID{slot}, "")
		}
		l.b.EmitMoveInto(slot, v)
		return v
	case hir.KIndex:
		// a[i] = v  -> store v into element i of a.
		var arrID, idxID int32
		i := 0
		for _, c := range l.pkg.Children(target) {
			if i == 0 {
				arrID = c
			} else {
				idxID = c
				break
			}
			i++
		}
		arrV := l.lowerExpr(arrID)
		idxV := l.lowerExpr(idxID)
		if arrV == NoVal || idxV == NoVal {
			return NoVal
		}
		l.b.EmitVoid(OpIndexStore, []ValueID{arrV, idxV, v}, "")
		return v
	case hir.KDot:
		// obj.field = v  -> set field of struct/option receiver.
		var recvID int32
		for _, c := range l.pkg.Children(target) {
			recvID = c
			break
		}
		recvV := l.lowerExpr(recvID)
		if recvV == NoVal {
			return NoVal
		}
		// Store the field name in inst.Str (not inst.Sym) so emitSetField's
		// FieldIndex lookup matches the GETFIELD convention in lowerDotRead.
		sid := l.b.EmitVoid(OpSetField, []ValueID{recvV, v}, "")
		l.mod.Insts[sid].Str = l.pkg.Str(tn.S)
		return v
	default:
		l.unsupported(l.curFuncName(), "assign", "unsupported assign target kind "+hir.KindNames[tn.Kind])
		return NoVal
	}
}

func prefixOp(s string) Op {
	switch s {
	case "-":
		return OpNeg
	case "!":
		return OpNot
	case "~":
		return OpNot
	}
	return OpInvalid
}

func infixOp(s string) (Op, bool) {
	cmp := true
	var op Op
	switch s {
	case "+":
		op, cmp = OpAdd, false
	case "-":
		op, cmp = OpSub, false
	case "*":
		op, cmp = OpMul, false
	case "/":
		op, cmp = OpDiv, false
	case "%":
		op, cmp = OpMod, false
	case "==":
		op = OpEq
	case "!=":
		op = OpNe
	case "<":
		op = OpLt
	case "<=":
		op = OpLe
	case ">":
		op = OpGt
	case ">=":
		op = OpGe
	case "&&":
		op, cmp = OpAnd, false
	case "||":
		op, cmp = OpOr, false
	case "&":
		op, cmp = OpBitAnd, false
	case "|":
		op, cmp = OpBitOr, false
	case "^":
		op, cmp = OpXor, false
	case "<<":
		op, cmp = OpShl, false
	case ">>":
		op, cmp = OpShr, false
	default:
		op, cmp = OpInvalid, false
	}
	return op, cmp
}
