package mir

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/parser"
)

// byteArrayRe matches a `[N]byte` / `[N]i8` nolang global type, used to fold
// module byte-array constants (SBOX, INV-SBOX) into compact c"..." data globals.
var byteArrayRe = regexp.MustCompile(`^\[(\d+)\](byte|i8)$`)

// formatFloat renders an f64 constant in a form LLVM's textual IR accepts.
// It uses the 16-hex-digit bit pattern (`0x3FF0000000000000`), NOT the
// shortest decimal form: `strconv.FormatFloat(0,'g',-1,64)` yields "0", and
// `double 0` is REJECTED by the LLVM parser (a decimal FP literal must
// contain '.' or an exponent). That broke every module-level float global
// (`@px2 = private global double 0` -> opt-verify failure, test-tmp-nbody).
// The hex form is always accepted and matches what instruction-level
// constants already emit (`store double 0x%016X`).
func formatFloat(f float64) string {
	return fmt.Sprintf("0x%016X", math.Float64bits(f))
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
	// localRaw records each local's DECLARED nolang raw type by name. MIR
	// flattens every integer to i64 and registers u64 locals as i64, so the
	// signedness is lost from the lowered value — but it is needed to choose
	// `udiv`/`urem` over `sdiv`/`srem` for unsigned division (i64-to-str's u64
	// magnitude loop for i64.MIN). Populated from the KLet declaration's type
	// annotation; reassignments (no type) leave the earlier entry intact.
	localRaw  map[string]string
	curFunc   FuncID
	voidType  TypeID
	curRecv   ValueID // current method's implicit `self` receiver value (first param)
	diags     []LowerDiag
	loopStack []loopCtx // active for-loops, for break/continue targets

	// matchDepth counts how many enclosing match arms are currently being
	// lowered. A match desugars to a chain of `if`/`elif`/`else` (each arm a
	// `then`/`else` block whose FIRST statement is the synthetic `let it =
	// <matched>`). Because `it` is a single function-global slot, a NESTED
	// match's arm would rebind `it` to its own subject and clobber the outer
	// match's `it` — so a later `it.method()` inside the nested arm (or after
	// it) resolved to the wrong receiver type. Legacy preserves the outer `it`
	// across nested matches (src/build/llvm/expr.go saves/restores
	// g.varTypes["it"] around every branch; the parser's intent at
	// lowering.go:1138 is "嵌套 match 時這能保住外層的 `it`"). MIR mirrors that
	// by skipping the `it` rebind when matchDepth > 1 (i.e. the `let it` is
	// itself inside a nested match arm), so `it` keeps referring to the nearest
	// enclosing match's subject. See the `name == "it"` branch in lowerStmt and
	// the match-arm detection in lowerIf.
	matchDepth int

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

	// inArmCond marks the condition position of a match arm, where a variant
	// constructor is a PATTERN (not a value being built). armEnum/armVariant
	// carry it into the arm body so field bindings can be projected onto the
	// payload (see lowerKLet).
	inArmCond  bool
	armEnum    *TaggedEnumInfo
	armVariant *VariantInfo
	// armEnumPrefer is the subject's enum type and armSubjectID the subject
	// expression, both captured while an arm condition is lowered; see lowerIf.
	armEnumPrefer string
	armSubjectID  int32
	// localEnums records the PLAIN enums defined by the program being
	// compiled (as opposed to std's). It bounds the "resolve a bare variant
	// name globally" fallback below: std's variant tables contain very common
	// names, and matching those unconditionally turned ordinary identifiers
	// into enum constants (map tests regressed on it).
	localEnums map[string]bool

	// itSrc is the value the current match arm bound to `it` — i.e. the
	// matched subject itself. It is the projection source for a pattern's
	// field names. `it`'s own slot is shared across arms (and across matches
	// in a function) so its declared type may be a previous arm's; the SOURCE
	// value always has the right type.
	itSrc ValueID

	// typeHint is the declared type of the binding currently being lowered.
	// Some builtins (with-len / with-cap / with-cap-len) declare an EMPTY
	// return list because their result type is inferred from the assignment's
	// left-hand side. Lowering consults this hint to give such a call a real
	// result type instead of treating it as void — otherwise the variable is
	// never bound and every later read of it becomes an "unresolved
	// identifier" gap. It is set for the duration of a KLet's value expression
	// and cleared immediately afterwards.
	typeHint TypeID

	// exprSink is the value slot that a control-flow expression (a `match`/`if`
	// used as a value, e.g. `r = n: { ok(v) -> v+1 }`) must write its RESULT
	// into. MIR is statement-oriented, so a match used as an expression value
	// is lowered as a STATEMENT whose arms each store their final value into
	// this slot; the enclosing `let`/`assign` then reads the slot as `r`'s
	// value. It is set by the enclosing binding for the duration of the
	// control-flow lowering and reset to NoVal afterwards. A NoVal exprSink
	// means "statement context" — arms run for side effects only, no capture.
	exprSink ValueID

	// exprCapture, when true, means the currently-lowered KIf is a match/if
	// used as an EXPRESSION value (`r = subject: { arms }` / `r = if c { a }
	// else { b }`). Each arm's final value is stored into exprSink (a shared
	// slot created on the first arm) so the enclosing binding can alias that
	// slot. Set by the enclosing KLet for the duration of the control-flow
	// lowering and restored afterwards. Crucially this is ONLY active while a
	// control-flow node is the RHS of a `let`/`assign` — statement-mode
	// matches (`{ cond -> ... }`) and ordinary uses of `if` never set it, so
	// they are unaffected (no regression of the 260 MATCH tests).
	exprCapture bool

	// exprSinkInit is the binding's CURRENT value while a capture is lowered.
	// The capture slot is seeded with it so a path that produces NO value —
	// a false condition, or a `?T` node that short-circuited the pipeline —
	// leaves the binding HOLDING ITS PREVIOUS VALUE instead of reading a slot
	// that was never stored to (uninitialised stack). Set by the enclosing
	// `let`/`assign` alongside exprCapture, NoVal when the binding is new.
	exprSinkInit ValueID

	// stmtVal carries the value produced by the most recent lowerStmt, so
	// lowerBlock can return the LAST statement's value (needed to capture a
	// match/if-arm's result into exprSink). Reset by lowerStmt at entry;
	// value-bearing statements overwrite it.
	stmtVal ValueID

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

	// inPrintArgs is >0 while the arguments of a print-family call are being
	// lowered. A `{name}` field inside a string literal is ONLY substituted at
	// such a call site (legacy: llvm.shouldInterceptNamedFormat is consulted
	// from callFmt, i.e. print/eprint/printf/format and nothing else); every
	// other context — `s = 'x={n}'`, an argument to a user function, a struct
	// field — prints the braces LITERALLY. This flag lets lowerExpr's KStrLit
	// case raise the "interp" fallback diagnostic only for the print-family
	// case, instead of the old blanket `Contains("{") && Contains("}")` test
	// that also flagged plain brace-bearing literals (JSON, code templates)
	// whose legacy output is the literal text.
	inPrintArgs int

	// wrapPrintArgs is true only while the argument list of the OUTERMOST
	// print-family call is being lowered (see lowerCallArgs). It drives
	// printableValue, the to-str wrapping of container arguments; inPrintArgs
	// cannot be reused for it because that one intentionally stays >0 for the
	// whole argument subtree (the string-interpolation diagnostic must fire for
	// a nested literal too), which would make a nested method call wrap its own
	// receiver.
	wrapPrintArgs bool

	// structLitTypes maps an anonymous struct-literal HIR node id to the struct
	// type name it must be given. The parser records an EMPTY Type for a bare
	// `{ a: 1 }` literal (parseStructLit: "由 codegen 推斷" — inferred from
	// context), and legacy resolves that context at codegen time from the call
	// site. MIR has no call context at codegen, so lowerCallArgs seeds the
	// expected type here from the callee's declared parameter types (e.g.
	// `fs.open(p, opts file-opts)` makes `{ mode: 0 }` a `file-opts`) before the
	// argument is lowered. Without it lowerStructLit emits an untyped structlit
	// and emitSetField's FieldIndex lookup fails with "setfield field" — the
	// 10-test fs-open/errno family (test-open-*, test_fs_error_*, test-fs-struct,
	// test-uninit-output).
	structLitTypes map[int32]string

	// forceRunCall is the HIR node id of a KCall that must be lowered as an
	// async launch (OpRun) even though its callee name lacks the `-async`
	// suffix. nolang's async-ness is SYNTACTIC: `run f(args)` launches a task
	// for ANY callee (legacy generateRunExpression calls prepareAsyncCall
	// unconditionally). Only a BARE call to an `-async`-suffixed function
	// creates a future. lowerAsyncRun sets this to the direct call child while
	// lowering it, then restores the previous value; lowerCall consumes it by
	// comparing against n.Id so nested calls (e.g. `run f(g())`) are unaffected.
	forceRunCall int32

	// asyncResTypes maps the MIR value holding an async task handle to the
	// TypeID of that task's RESULT. The handle itself is an opaque i64, so the
	// result type would otherwise be lost between the `run` site (where the
	// result buffer's size/type is known from the callee signature) and the
	// `awy` site (which must read the result with the right type — e.g. a `str`
	// result read as i64 prints the length instead of the string). Populated in
	// lowerCall's OpRun branch and propagated through scalar let-copies.
	asyncResTypes map[ValueID]TypeID
}

// copiedEnumVariants returns a private copy of the enum-variant table so the
// lowerer can add the program's own definitions without mutating the shared
// std cache that the caller passed in.
func copiedEnumVariants(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// collectLocalEnumVariants registers the PLAIN (payload-less) enums defined by
// the program being compiled into l.enumVariants, which otherwise only holds
// std enums (checker.CollectStdEnumVariants). Without this a user enum's
// variant names were invisible to MIR: `c color = green` lowered to a void
// const and `c: { red -> ... }` could not resolve `red`
// (tests/tagged-enum.no).
//
// Only KEnumDef is registered here. A tagged enum (KTaggedEnumDef) is a
// struct value, not a plain i64 discriminant, so adding its variant names to
// this table would make isEnumTypeName report true for it and let it be
// inlined as an integer; its arms are resolved through mod.TaggedEnums.
func (l *lowerer) collectLocalEnumVariants() {
	if l.enumVariants == nil {
		l.enumVariants = map[string][]string{}
	}
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KEnumDef {
			continue
		}
		name := l.pkg.Str(n.S)
		if name == "" {
			continue
		}
		if _, exists := l.enumVariants[name]; exists {
			continue // std definition wins
		}
		var vs []string
		for _, c := range l.pkg.Children(id) {
			cn := l.pkg.Node(c)
			if cn == nil || cn.Kind != hir.KEnumValue {
				continue
			}
			if vn := l.pkg.Str(cn.S); vn != "" {
				vs = append(vs, vn)
			}
		}
		if len(vs) > 0 {
			l.enumVariants[name] = vs
			if l.localEnums == nil {
				l.localEnums = map[string]bool{}
			}
			l.localEnums[name] = true
		}
	}
}

// collectTaggedEnums scans the HIR package for tagged-enum definitions and
// records their variant tables into mod.TaggedEnums. This must run before any
// function body is lowered: a variant constructor (`full(7)`) and a match arm
// (`full(v) -> ...`) are both resolved through this table.
func (l *lowerer) collectTaggedEnums() {
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KTaggedEnumDef {
			continue
		}
		l.collectTaggedEnum(l.pkg, id)
	}
}

// collectTaggedEnum records one tagged-enum definition. The discriminant of a
// variant is its declaration order within the enum body, which is what the
// match desugar and the constructor both agree on.
func (l *lowerer) collectTaggedEnum(pkg *hir.Package, id int32) {
	n := pkg.Node(id)
	if n == nil {
		return
	}
	name := pkg.Str(n.S)
	if name == "" {
		return
	}
	_, inlineVal := l.inlineAnnotation(id)
	info := &TaggedEnumInfo{Name: name, Inline: inlineVal}
	slots := int64(1) // never zero-width: a unit-only enum still needs a payload array
	for _, c := range pkg.Children(id) {
		vn := pkg.Node(c)
		if vn == nil || vn.Kind != hir.KVariant {
			continue
		}
		v := VariantInfo{Name: pkg.Str(vn.S), Tag: int64(len(info.Variants))}
		var total int64
		for _, fc := range pkg.Children(c) {
			fn := pkg.Node(fc)
			if fn == nil || fn.Kind != hir.KStructField {
				continue
			}
			raw := pkg.Type(fn.Type)
			if raw == "" {
				continue
			}
			v.Fields = append(v.Fields, raw)
			v.FieldNames = append(v.FieldNames, pkg.Str(fn.S))
			total += l.enumFieldSlots(raw)
		}
		if len(v.Fields) == 0 {
			// Single-field payloads are carried on the variant's own Type in
			// HIR (`ok(v i64)` -> KVariant.Type = i64) with no KStructField
			// children; multi-field ones use the children.
			if raw := pkg.Type(vn.Type); raw != "" && raw != "void" {
				v.Fields = append(v.Fields, raw)
				v.FieldNames = append(v.FieldNames, "")
				total += l.enumFieldSlots(raw)
			}
		}
		info.Variants = append(info.Variants, v)
		if total > slots {
			slots = total
		}
	}
	info.PayloadSlots = slots
	l.mod.TaggedEnums[name] = info
}

// inlineAnnotation returns the `#{inline}` annotation's boolean value on a
// definition node. It is the shared reader for the inline opt-in marker, used
// both for struct fields (fieldTag) and for the enum definition itself
// (`#{inline}` on its own line above the whole enum), which guarantees the
// stack-form enum layout.
//
// Three spellings, one meaning each:
//
//	#{inline}        -> true   (shorthand)
//	#{inline=true}   -> true
//	#{inline=false}  -> false  (explicitly the default, non-inline layout)
//
// present is false when there is no annotation at all, in which case the
// caller keeps the default layout.
func (l *lowerer) inlineAnnotation(id int32) (present, value bool) {
	return l.pkg.AnnotationBool(id, "inline")
}

// enumFieldIndex returns the payload-field index that `name` binds in a
// variant pattern, or -1. A single-field payload declared on the variant
// itself has no recorded name, so it matches any binding name at position 0.
func enumFieldIndex(v *VariantInfo, name string) int {
	if v == nil || len(v.Fields) == 0 {
		return -1
	}
	for i, fn := range v.FieldNames {
		if i < len(v.Fields) && fn == name {
			return i
		}
	}
	if len(v.Fields) == 1 && v.FieldNames[0] == "" {
		return 0
	}
	return -1
}

// enumFieldSlots returns how many 8-byte payload slots one field occupies.
// The payload is a union, so a field only has to fit — but it must fit for
// every variant, hence the enum's slot width is the max over variants.
func (l *lowerer) enumFieldSlots(raw string) int64 {
	switch raw {
	case "str", "vec":
		return 3 // %str-long / %vec = 24 bytes
	}
	if strings.HasPrefix(raw, "[]") {
		return 3
	}
	if strings.HasPrefix(raw, "?") {
		return 4 // %option = 32 bytes (i64 tag + 24-byte payload slot)
	}
	if fields, ok := l.mod.StructFields[raw]; ok {
		var n int64
		for _, f := range fields {
			n += l.enumFieldSlots(f.TypeRaw)
		}
		if n > 0 {
			return n
		}
	}
	return 1
}

// typeHintRaw returns the nolang type string of the type expected at the
// current lowering site (the declared type of the binding or the parameter
// type of the call being filled), or "" when it is unknown.
func (l *lowerer) typeHintRaw() string {
	if l.typeHint == NoType || l.typeHint == l.voidType {
		return ""
	}
	if t := l.mod.Type(l.typeHint); t != nil {
		return t.Raw
	}
	return ""
}

// enumVariantOf resolves a variant (constructor) name to its enum and variant
// entry. `prefer` is the enum raw type expected at this site — the declared
// type of the binding being initialized, or the parameter type at a call site.
//
// Disambiguation rule (deliberately strict): when `prefer` is known, ONLY that
// enum is searched. This is what keeps three different `ok`s apart —
// `a-res.ok(i64)`, `b-res.ok(str)` and the option constructor `ok(x)` of ?T —
// and it is why a failed lookup must NOT fall through to a global scan: with
// `x ?i64 = ok(5)` the expected type is an option, and a global scan would
// happily match some unrelated enum's `ok` and build the wrong value.
//
// With no expected type a global scan is the only option, but it is accepted
// only when the variant name is unique across all enums; an ambiguous name
// resolves to nothing rather than picking an arbitrary enum.
// enumPrefer returns the enum type to resolve a variant name against: the
// declared type at this site when it IS an enum, otherwise the enclosing
// match arm's subject type (see lowerIf). An empty result means "unknown",
// which restricts resolution to globally unique variant names.
func (l *lowerer) enumPrefer() string {
	if r := l.typeHintRaw(); r != "" {
		if _, ok := l.mod.TaggedEnums[r]; ok {
			return r
		}
	}
	return l.armEnumPrefer
}

func (l *lowerer) enumVariantOf(prefer, variant string) (*TaggedEnumInfo, *VariantInfo) {
	if variant == "" {
		return nil, nil
	}
	if prefer != "" {
		ei, ok := l.mod.TaggedEnums[prefer]
		if !ok {
			return nil, nil
		}
		for i := range ei.Variants {
			if ei.Variants[i].Name == variant {
				return ei, &ei.Variants[i]
			}
		}
		return nil, nil
	}
	var found *TaggedEnumInfo
	var vi *VariantInfo
	for _, ei := range l.mod.TaggedEnums {
		for i := range ei.Variants {
			if ei.Variants[i].Name != variant {
				continue
			}
			if found != nil {
				return nil, nil // ambiguous across enums
			}
			found, vi = ei, &ei.Variants[i]
		}
	}
	if found == nil {
		return nil, nil
	}
	return found, vi
}

// collectStructFields scans the HIR package for struct definitions and records
// their ordered fields into mod.StructFields (name + nolang type string), so
// OpGetField/OpSetField can resolve field indices and codegen can emit LLVM
// struct type declarations. Field order is the child order of KStructField
// nodes, which matches the GEP index order.
// collectStructFields registers every struct's field layout, and with it each
// field's DEFINITION-SITE semantic tag (see mir.FieldTag).
//
// It runs in two passes because a field's tag depends on whether its declared
// type names a struct, and a struct may be declared AFTER the one that
// references it:
//
//	holder { p pt }   ; pt is declared below
//	pt { x i64  y i64 }
//
// Pass 1 therefore registers every struct NAME before any field type is
// interned. Without it internType's bare-identifier fixup collapses `pt` to
// KindInt and CACHES that in TypeMap, so the field stays mis-typed for the rest
// of the compilation: the field access then emits
// `getelementptr inbounds i64` and LLVM verification fails with "invalid
// getelementptr indices".
func (l *lowerer) collectStructFields() {
	// Pass 1: names only.
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KStructDef {
			continue
		}
		if name := l.pkg.Str(n.S); name != "" {
			l.mod.StructNames[name] = true
		}
	}

	// Pass 2: fields and their tags.
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
			fields = append(fields, FieldInfo{
				Name:    fname,
				TypeRaw: ftype,
				Tag:     l.fieldTag(c, ftype),
				Layout:  l.fieldLayout(c),
			})
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

// fieldLayout reads a field's explicit `#{inline=...}` override. It is the
// DECLARATION of intent, independent of the module-wide default: `#{inline}`
// and `#{inline=true}` ask for the by-value layout, `#{inline=false}` asks for
// a pointer, and no annotation asks for whatever the default is.
//
// This is deliberately a separate axis from fieldTag. Layout says where the
// bytes live; the tag says who owns them. A pointer field is Owned either way,
// so `#{inline=false}` must NOT change the tag — only where the pointee is
// stored.
func (l *lowerer) fieldLayout(fieldID int32) FieldLayout {
	present, value := l.inlineAnnotation(fieldID)
	if !present {
		return FieldLayoutDefault
	}
	if value {
		return FieldLayoutInline
	}
	return FieldLayoutPointer
}

// fieldTag computes a field's definition-site semantic tag from its declared
// type plus its OWN annotation. It is a pure function of the declaration: no
// use site is consulted, which is the whole point of the tag.
//
//	#{inline} / #{inline=true} on a struct-typed field -> Inline (by-value layout)
//	#{inline=false}                                   -> Owned  (forced pointer)
//	struct-typed field (default)      -> Owned  (pointer; recursive drop)
//	owned leaf (str/vec/[]T/map/?owned)-> Owned  (inline descriptor, owns heap)
//	?T where T is a struct            -> Owned  (nullable pointer)
//	everything else (scalars, txt)    -> Inline
//
// `#{inline=false}` falls through to the type-based rules on purpose: a forced
// pointer field OWNS its pointee, which is exactly what a default struct-typed
// field does. Only the layout differs, and that is fieldLayout's job.
func (l *lowerer) fieldTag(fieldID int32, raw string) FieldTag {
	if present, value := l.inlineAnnotation(fieldID); present && value {
		return FieldTagInline
	}
	if l.isStructType(raw) {
		return FieldTagOwned
	}
	if strings.HasPrefix(raw, "?") && l.isStructType(strings.TrimPrefix(raw, "?")) {
		return FieldTagOwned
	}
	if ClassifyOwnership(raw) {
		return FieldTagOwned
	}
	return FieldTagInline
}

// isStructType reports whether raw names a struct whose layout this module
// knows. The definition lives on Module (see Module.IsStructType) so the
// analysis passes and codegen share it; this is the lowerer-side alias.
func (l *lowerer) isStructType(raw string) bool {
	return l.mod.IsStructType(raw)
}

// collectValueTypeAliases scans the HIR package for value-type alias
// definitions (e.g. `fd = i64`, `code = i32`) and records the mapping from the
// alias name to its underlying nolang type name in mod.ValueTypeAliases. A
// scalar newtype (`fd`) is represented in MIR as the same KindInt as its
// underlying `i64`, but its type Raw keeps the alias name (`fd`), so a method
// call `fd.to-str()` would otherwise form the callee `fd.to-str` instead of the
// `i64.to-str` the legacy backend emits. Expanding the alias at method-dispatch
// time fixes "unknown callee fd.to-str" (tests/errno-basic.no). Function
// type aliases (FlagFuncType) and unions (FlagUnion) are excluded — the former
// are tracked by mod.TypeAliases, the latter have no single underlying type.
func (l *lowerer) collectValueTypeAliases() {
	if l.mod.ValueTypeAliases == nil {
		l.mod.ValueTypeAliases = map[string]string{}
	}
	if l.mod.TypeAliases == nil {
		l.mod.TypeAliases = map[string]TypeID{}
	}
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil || n.Kind != hir.KTypeAlias {
			continue
		}
		if n.Has(hir.FlagUnion) || n.Type == hir.NoID {
			continue
		}
		name := l.pkg.Str(n.S)
		if name == "" {
			continue
		}
		if target := l.pkg.Type(n.Type); target != "" {
			if n.Has(hir.FlagFuncType) {
				// Register the function-type alias so internType resolves
				// `test-cb` to the KindFunc type (e.g. `fn()`), not a
				// misclassified KindInt. This lets resolveCallee detect
				// fn-typed parameters and emit indirect calls
				// (tests/named-fn-type.no).
				l.mod.TypeAliases[name] = l.b.Type(target)
				if i := strings.LastIndex(name, "."); i >= 0 {
					bare := name[i+1:]
					if _, ok := l.mod.TypeAliases[bare]; !ok {
						l.mod.TypeAliases[bare] = l.b.Type(target)
					}
				}
				continue
			}
			// Register under the module-qualified name (e.g. `fs.fd`) so a
			// fully-qualified receiver type resolves, AND under the bare name
			// (e.g. `fd`) because method dispatch sees the receiver's MIR type
			// Raw which keeps the BARE alias name (`fd`, not `fs.fd`) — the
			// type checker normalizes `fs.open-file`'s return to the alias `fd`
			// while the KTypeAlias node is stored module-qualified. Without the
			// bare entry, `fd.to-str` would not expand to `i64.to-str`.
			l.mod.ValueTypeAliases[name] = target
			if i := strings.LastIndex(name, "."); i >= 0 {
				if bare := name[i+1:]; bare != "" {
					if _, ok := l.mod.ValueTypeAliases[bare]; !ok {
						l.mod.ValueTypeAliases[bare] = target
					}
				}
			}
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
		// A node carrying platform annotations (#{mac-arm64}, #{win-amd64}, ...)
		// participates only when one of its keys matches the target. The
		// registration loops above already apply this check to KFuncDef/KLet so
		// the MATCHING variant is the one that lands in the name tables (see
		// nodeMatchesPlatform). This inlining path must apply the SAME check:
		// without it a filtered-out variant was still inlined as a script local,
		// and its wrong-platform value overwrote the global registered by the
		// matching variant. Measured on arm64 macOS (a script, no `fn main`):
		//
		//	#{mac-arm64} V = 1
		//	#{mac-amd64} V = 2
		//	print(V)
		//
		// printed 2 instead of 1, and a lone `#{mac-amd64} V = 8` compiled and
		// printed 8 where the legacy backend correctly reports an undefined
		// `%V`. Filtering here makes both cases agree with legacy. Note the
		// bug was invisible in the test corpus because std's mac-amd64 and
		// mac-arm64 constants happen to hold equal values (std/fs.no O-CREAT
		// 512/512, O-TRUNC 1024/1024).
		if !nodeMatchesPlatform(pkg, id) {
			continue
		}
		// A tagged-enum definition carries the variant table that later
		// lowering needs: it is not a statement, but it must be collected
		// before any function body that constructs or matches an enum is
		// lowered. (Previously it fell into the `continue` below and MIR knew
		// nothing about enums, so `full(7)` resolved to nothing at all.)
		if n.Kind == hir.KTaggedEnumDef {
			l.collectTaggedEnum(pkg, id)
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
				} else if gnid, ok := l.globalNodes[pkg.Str(n.S)]; ok && gnid == id {
					// This `let` IS the registered module-constant declaration
					// (its initializer folds to a real LLVM constant and was
					// materialized as a lazily-materialized global). It must NOT
					// be inlined into the synthetic `main` — the global already
					// holds the value. A LATER top-level `let` of the SAME name
					// (a runtime reassignment, e.g. `x = 10` after `x i64 = 5`)
					// has `gnid != id`, so it is NOT skipped here: it is inlined
					// and lowerStmt/lowerAssignNode emit the runtime store into
					// the existing global (test-ifelse2: `five`/`not five`, not
					// `not five`/`not five`). Without this guard the reassignment
					// was folded into the initializer and the runtime store was
					// dropped, so the variable never changed value.
					continue
				}
				// Otherwise (a reassignment to a previously-declared global, or a
				// runtime-computed `let`): inline into `main` so it stores /
				// computes at runtime and stays consistent with the legacy
				// backend.
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
		pkg:         pkg,
		funcNames:   map[string]int32{},
		lowered:     map[string]bool{},
		locals:      map[string]ValueID{},
		globals:     map[string]ValueID{},
		globalTypes: map[string]string{},
		globalNodes: map[string]int32{},
		// Copy the caller's table: it is the SHARED std cache
		// (checker.stdEnumVariantsCache), and collectLocalEnumVariants adds the
		// program's own enum definitions to it. Writing into the shared map
		// would leak one program's enums into every later compilation.
		enumVariants:  copiedEnumVariants(enumVariants),
		asyncResTypes: map[ValueID]TypeID{},
	}
	l.b = NewBuilder("hir")
	l.mod = l.b.Module()
	l.voidType = l.b.TypeExplicit("void", KindVoid, false, NoType)

	// Collect struct field layouts so OpGetField/OpSetField can resolve field
	// indices. Builtins have a fixed, known layout; user/std structs come from
	// HIR KStructDef nodes (field order = child order).
	// The builtin descriptors are registered by hand, so their field tags are
	// explicit. `str.data` / `vec.data` are the raw buffers the descriptor
	// itself owns: the tag is Inline (the field slot owns no SEPARATE heap
	// object) precisely so a drop walk stops here instead of freeing the buffer
	// twice — the descriptor's own drop already freed it.
	l.mod.StructFields["str"] = []FieldInfo{
		{Name: "len", TypeRaw: "i64", Tag: FieldTagInline},
		{Name: "cap", TypeRaw: "i64", Tag: FieldTagInline},
		{Name: "data", TypeRaw: "str", Tag: FieldTagInline},
	}
	l.mod.StructFields["vec"] = []FieldInfo{
		{Name: "len", TypeRaw: "i64", Tag: FieldTagInline},
		{Name: "cap", TypeRaw: "i64", Tag: FieldTagInline},
		{Name: "data", TypeRaw: "vec", Tag: FieldTagInline},
	}
	// txt: fixed 256-byte stack buffer { [255 x i8] data, i8 len } (capped at
	// 255 bytes). Field order MUST match emitStructTypes' %txt layout so GEP
	// indices stay consistent: data = field 0, len = field 1. A plain stack
	// buffer owns nothing, so both fields are Inline.
	l.mod.StructFields["txt"] = []FieldInfo{
		{Name: "data", TypeRaw: "byte", Tag: FieldTagInline},
		{Name: "len", TypeRaw: "byte", Tag: FieldTagInline},
	}
	l.collectStructFields()
	l.collectTaggedEnums()
	l.collectLocalEnumVariants()
	l.collectValueTypeAliases()

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
			// Skip the platform variants that do not apply to the target, so a
			// declaration with several platform bodies registers the matching
			// one (see nodeMatchesPlatform; `process.cmd`'s POSIX vs Win32 pair
			// otherwise collapsed onto a single mangled symbol and the Win32
			// body won).
			if !nodeMatchesPlatform(pkg, id) {
				continue
			}
			l.funcNames[name] = id
		case hir.KLet:
			if !nodeMatchesPlatform(pkg, id) {
				continue
			}
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
			// fixedGlobalType overrides the global's declared type when the
			// binding's own type cannot describe its constant initializer (a
			// slice-typed array literal folds to a FIXED array). See the
			// !hasExplicitMain guard below.
			fixedGlobalType := ""
			if !hasExplicitMain {
				raw := l.letTypeRaw(n)
				gname := pkg.Str(n.S)
				// A top-level binding that a named FUNCTION references must be
				// module storage, even when the rules below would normally skip
				// it. Those rules exist because a script's top-level binding is
				// materialized as a LOCAL of the synthetic main, which is correct
				// only while main is its sole consumer — a local of main cannot
				// serve another frame, so a helper function referencing the name
				// resolved it to a void `const` placeholder. Two shapes hit this:
				//
				//  1. A DECLARATION-ONLY binding (`zw-data []byte`, no
				//     initializer), written by add-file/finalize and read by
				//     create in notools' zip.no: "vec.push: needs receiver and
				//     element" / "write-file: data arg has no slot (arg type
				//     void)". There is no initializer to fold and no
				//     inline-as-local alternative, so register it as a mutable
				//     global — codegen emits `private global <T>
				//     zeroinitializer` for an empty ConstText, the correct
				//     zero-initialized shared state.
				//
				//  2. A CONSTANT array literal (`XZ-MAGIC = [0xfd, 0x37, ...]`)
				//     read by a std function (std/archive/xz.no's xz-decompress
				//     does `data[i] != XZ-MAGIC[i]`). Its inferred type is a slice
				//     (`[]i64`) but foldConstText lowers the literal to a FIXED
				//     array `[6 x i64] [...]`, so the global must be declared with
				//     the fixed-array type or the declaration and initializer
				//     disagree and the verifier rejects the module.
				//
				// A binding WITH a runtime initializer and a non-fixed type
				// (e.g. `x = compute()`) is deliberately NOT covered: it would
				// need the initializer statement to write into the global plus
				// owned-value move/clone bookkeeping this path does not have.
				referencedByFunc := gname != "" && l.nameUsedInFuncBodies(gname)
				declOnly := n.First == hir.NoID && raw != "" && raw != "void"
				constArr := false
				if referencedByFunc && l.isUnsafeInlineType(raw) {
					if ft := l.fixedArrayConstGlobalType(n, raw); ft != "" {
						fixedGlobalType = ft
						constArr = true
					}
				}
				if !(referencedByFunc && (declOnly || constArr)) {
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
			}
			gname := pkg.Str(n.S)
			if gname != "" {
				// Only the FIRST top-level `let` of a name is a module-level
				// declaration whose LLVM constant initializer is materialized
				// once. A LATER `let` of the same name (`x = 10` after
				// `x i64 = 5`) is a runtime REASSIGNMENT, not a fresh
				// declaration: registering it would (a) overwrite the global's
				// `@x` initializer with the reassignment value (test-ifelse2:
				// `@x` became `i64 10` instead of `i64 5`) and (b) make
				// synthesizeMainForTopLevel skip it as a second module constant,
				// so the runtime store into `@x` was dropped and every later
				// read saw the (wrong) initializer. The reassignment is handled
				// by synthesizeMainForTopLevel, which inlines it so
				// lowerStmt/lowerAssignNode emit the store. Guard with existence
				// so reassignments never overwrite the declaration's slot.
				if _, exists := l.globals[gname]; !exists {
					// A fixed-array override wins: the binding's own type is a
					// slice, which cannot describe the fixed-array constant its
					// initializer folds to (see fixedArrayConstGlobalType).
					if fixedGlobalType != "" {
						l.globalTypes[gname] = fixedGlobalType
					} else if dt := l.typeOfNode(n); dt != NoType && dt != l.voidType {
						l.globalTypes[gname] = l.mod.Types[dt].Raw
					}
					l.globals[gname] = NoVal // marker: declared, not yet materialized
					l.globalNodes[gname] = id
				}
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
	// Slice views alias their receiver's buffer, so a view that ESCAPES the
	// frame would leave the caller holding a pointer into storage that dies
	// with it. Demote those back to an owned copy before the analysis runs.
	l.mod.demoteUnsafeSliceViews()
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
	case hir.KIntLit, hir.KByteLit:
		return l.b.Type("i64")
	case hir.KCharLit:
		// A char literal's value is produced by lowerCharLit with type `char`
		// (i32). Reporting i64 here would type the node wider than the value
		// actually materialized, so `c = 'A'` allocated an i64 slot and then
		// tried to store an i32 into it.
		return l.b.Type("char")
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
		"bool", "f64", "f32", "double", "str", "txt",
		// `char` is a scalar like any other integer lane (KindChar -> i32) and
		// lowers the same way. Leaving it out made EVERY top-level `x char =
		// <runtime value>` fall into the "non-inlineable" branch below and get
		// dropped from the synthesized `main` entirely, so the name never bound
		// and `print(x)` emitted nothing (codegen skipped the unresolved
		// argument). That is the shape the `s[i]`-returns-char semantics produce
		// all the time (`c char = s[0]`), so it has to inline like any other
		// scalar.
		"char":
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

// enumVariantValueOf resolves the bare variant name `variant` against the enum
// type `enumRaw` (which may be module-qualified, e.g. "fs.file-mode" for the
// table key "file-mode"), returning its zero-based discriminant.
//
// `enumVariantValue` above only accepts an exact "enum.variant" key; this
// variant mirrors `isEnumTypeName`'s leniency so a module-qualified *type* name
// still finds the bare table key.
func (l *lowerer) enumVariantValueOf(enumRaw, variant string) (int64, bool) {
	if enumRaw == "" || variant == "" || l.enumVariants == nil {
		return 0, false
	}
	lookup := func(key string) ([]string, bool) {
		v, ok := l.enumVariants[key]
		return v, ok
	}
	variants, ok := lookup(enumRaw)
	if !ok {
		if i := strings.LastIndex(enumRaw, "."); i >= 0 {
			variants, ok = lookup(enumRaw[i+1:])
		}
	}
	if !ok {
		return 0, false
	}
	for idx, v := range variants {
		if v == variant {
			return int64(idx), true
		}
	}
	return 0, false
}

// enumArmFieldValue projects a tagged-enum variant pattern's field name onto
// the matched subject's payload slot. It applies only while an arm body is
// being lowered (armVariant set by the arm's condition), and only when the
// name really is one of that variant's fields. The subject is the arm's `it`
// binding, which the parser always emits as the first statement of an arm.
func (l *lowerer) enumArmFieldValue(name string) (ValueID, bool) {
	if l.armVariant == nil || l.armEnum == nil || name == "" || name == "it" {
		return NoVal, false
	}
	fi := enumFieldIndex(l.armVariant, name)
	if fi < 0 || fi >= len(l.armVariant.Fields) {
		return NoVal, false
	}
	subj := l.itSrc
	if subj == NoVal {
		// No `let it` seen (an arm whose body does not start with the
		// synthetic binding): fall back to the `it` local itself.
		if v, ok := l.locals["it"]; ok {
			subj = v
		}
	}
	if subj == NoVal {
		return NoVal, false
	}
	// The subject's declared type is NOT checked against the enum here: an
	// arm's `it` slot is shared across arms of the same match (and across
	// matches in a function), so it may still carry the first arm's type.
	// The arm's own variant table is authoritative — it was resolved from the
	// subject in the arm's condition.
	if subj == NoVal {
		return NoVal, false
	}
	ft := l.b.Type(l.armVariant.Fields[fi])
	if ft == NoType || ft == l.voidType {
		return NoVal, false
	}
	var slot int64
	for k := 0; k < fi; k++ {
		slot += l.enumFieldSlots(l.armVariant.Fields[k])
	}
	fv := l.b.Emit(OpEnumField, ft, []ValueID{subj}, "")
	l.mod.Insts[len(l.mod.Insts)-1].Int = slot
	return fv, true
}

// plainEnumVariantValue resolves a bare variant name of a PLAIN (payload-less)
// enum to its discriminant. The expected type at the site is tried first; when
// it is not an enum name (a plain enum value is just an i64 in MIR, and a match
// arm's subject carries no hint at all) the name is resolved globally, but only
// if it is unique across all enums — an ambiguous name resolves to nothing
// rather than picking an arbitrary enum.
func (l *lowerer) plainEnumVariantValue(name string) (int64, bool) {
	if raw := l.typeHintRaw(); raw != "" {
		if val, ok := l.enumVariantValueOf(raw, name); ok {
			return val, true
		}
	}
	if raw := l.armEnumPrefer; raw != "" {
		if val, ok := l.enumVariantValueOf(raw, name); ok {
			return val, true
		}
	}
	if l.enumVariants == nil || len(l.localEnums) == 0 {
		return 0, false
	}
	var val int64
	found := false
	for en, vs := range l.enumVariants {
		// Only the program's OWN enums participate in the global fallback.
		// std's variant names (found / removed / ok / ...) collide with
		// ordinary identifiers far too often to match them speculatively.
		if !l.localEnums[en] {
			continue
		}
		for i, vn := range vs {
			if vn != name {
				continue
			}
			if found {
				return 0, false // ambiguous across enums
			}
			val, found = int64(i), true
		}
	}
	return val, found
}

// lowerBareEnumVariant resolves a match-arm variant reference with NO receiver.
// The parser desugars `m: { read -> ... }` (m: file-mode) into the condition
// `m == read`, where `read` is a bare KIdent carrying no type — HIR cannot say
// which enum it belongs to, so `lowerIdent` fell through to an unresolved
// placeholder and emitted `const void`, producing
// `icmp eq i64 %lv, undef` (verified: MIR `fs.open` compared opts.mode against
// undef instead of 0/1/2/3, so every file mode took the WRONG branch — the
// runtime trap behind the fs-open family).
//
// The enum type comes from the SIBLING operand (`m`), which is a real value
// of an enum-typed slot. Only a genuinely free identifier qualifies: a bound
// local is a value, not a variant.
func (l *lowerer) lowerBareEnumVariant(nd, sibling *hir.Node) (ValueID, bool) {
	if nd == nil || nd.Kind != hir.KIdent {
		return NoVal, false
	}
	name := l.pkg.Str(nd.S)
	if name == "" {
		return NoVal, false
	}
	// A bound local shadows any same-named variant (`read` as a variable).
	if _, isLocal := l.locals[name]; isLocal {
		return NoVal, false
	}
	if sibling == nil {
		return NoVal, false
	}
	enumRaw := ""
	switch sibling.Kind {
	case hir.KIdent:
		if sv, ok := l.locals[l.pkg.Str(sibling.S)]; ok {
			if ty := l.mod.Type(l.valueTypeOf(sv)); ty != nil {
				enumRaw = ty.Raw
			}
		}
	}
	if enumRaw == "" {
		if st := l.typeOfNode(sibling); st != NoType && st != l.voidType {
			if ty := l.mod.Type(st); ty != nil {
				enumRaw = ty.Raw
			}
		}
	}
	if enumRaw == "" {
		return NoVal, false
	}
	v, ok := l.enumVariantValueOf(strings.TrimPrefix(enumRaw, "?"), name)
	if !ok {
		// The sibling's MIR type is the enum's underlying i64, so it no longer
		// names the enum; fall back to a globally unique variant name.
		v, ok = l.plainEnumVariantValue(name)
	}
	if !ok {
		return NoVal, false
	}
	return l.b.EmitInt(OpConst, l.b.Type("i64"), v, ""), true
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
		case hir.KStrLit:
			return "str"
		case hir.KCharLit:
			// A char literal is a `char` (i32 code point), NOT a str. This
			// fallback previously mapped it to "str", which mistyped any
			// top-level `c = 'A'` whose type could not be recovered elsewhere.
			return "char"
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

// funcSigRaw builds a `fn(p0,p1,...)(r0,r1,...)?` type string from a HIR
// function definition's parameter and result nodes. This is used by lowerExpr's
// KIdent branch to create a KindFunc-typed value when a function name is used
// as a value (passed as an argument to another function). The string must
// match the format expected by internType/parseFuncType.
func (l *lowerer) funcSigRaw(hirID int32) string {
	var params, results []string
	for _, c := range l.pkg.Children(hirID) {
		cn := l.pkg.Node(c)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			if t := l.pkg.Type(cn.Type); t != "" {
				params = append(params, t)
			}
		case hir.KResult:
			if t := l.pkg.Type(cn.Type); t != "" {
				results = append(results, t)
			}
		}
	}
	sig := "fn(" + strings.Join(params, ",") + ")"
	if len(results) > 0 {
		sig += "(" + strings.Join(results, ",") + ")"
	}
	return sig
}

func (l *lowerer) lowerFunction(name string, hirID int32) {
	l.lowered[name] = true
	// Reset the deferred merge-continuation map for this function. Block IDs
	// are unique per module, but clearing avoids stale cross-function entries
	// and keeps ensureReturn's lookup scoped to the current function.
	l.contTargets = make(map[BlockID]BlockID)
	// Reset the declared-raw-type table so each function starts clean (the
	// signedness lookup for unsigned division must not leak across functions).
	l.localRaw = make(map[string]string)
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
	// For a method, `self` is the first KResult (an out-param the caller
	// passes by pointer so mutations propagate).  It is the implicit
	// receiver that `.field` accesses resolve to when the KDot has no
	// explicit receiver child.  Before the self-to-Result refactor, self
	// was the first KParam and lived at params[0]; now it is the first
	// result value.
	if n.Has(hir.FlagMethod) && len(resultVals) > 0 {
		l.curRecv = resultVals[0]
	}
	l.locals = map[string]ValueID{}
	// Build l.locals: each KParam/KResult name maps to its value. The params
	// slice is built in HIR children order (interleaving KParam and KResult),
	// so a simple index loop over paramNames is wrong when KResult (self)
	// precedes KParam children. Instead, iterate params in order and match
	// names from paramNames (for KParam) and resultNames (for KResult).
	{
		pIdx := 0 // index into paramNames
		rIdx := 0 // index into resultNames
		for _, c := range l.pkg.Children(hirID) {
			cn := l.pkg.Node(c)
			if cn == nil {
				continue
			}
			switch cn.Kind {
			case hir.KParam:
				if pIdx < len(paramNames) {
					l.locals[paramNames[pIdx]] = params[pIdx+rIdx]
					// Record the DECLARED raw type for unsigned division.
					if isUnsignedRaw(l.mod.Type(paramTypes[pIdx]).Raw) {
						l.localRaw[paramNames[pIdx]] = l.mod.Type(paramTypes[pIdx]).Raw
					}
					pIdx++
				}
			case hir.KResult:
				if rIdx < len(resultNames) {
					l.locals[resultNames[rIdx]] = params[pIdx+rIdx]
					rIdx++
				}
			}
		}
	}
	f := l.mod.Func(fid)
	if f != nil {
		f.ResultParams = resultVals
		// `f (a ..T)` — record variadicness so emitCallBody knows to pack the
		// trailing scalar arguments into the []T spread parameter instead of
		// relying on an arg-count heuristic that explicit out-param actuals
		// make ambiguous.
		f.Variadic = n.Has(hir.FlagVariadic)
		// Mark method functions so codegen passes the receiver by reference
		// (aliases the caller's self pointer instead of copying into a local
		// alloca). Without this, mutating std methods (vec.insert/remove/…)
		// silently discard self mutations under the MIR backend.
		if n.Has(hir.FlagMethod) && len(results) > 0 {
			f.IsMethod = true
			f.Receiver = results[0]
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
	//
	// For a method, the first KResult is `self` (the out-param receiver). Its
	// value comes from the caller and must NOT be zero-initialized — doing so
	// overwrites the receiver with 0 before the method body runs (e.g.
	// `i64.to-str` always saw self=0 and printed "0" regardless of the actual
	// integer value).
	isMethod := n.Has(hir.FlagMethod)
	for i := range resultNames {
		if isMethod && i == 0 {
			continue // skip self
		}
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

func (l *lowerer) lowerBlock(blockID int32) ValueID {
	var last ValueID
	for _, c := range l.pkg.Children(blockID) {
		l.stmtVal = NoVal
		l.lowerStmt(c)
		last = l.stmtVal
	}
	return last
}

func (l *lowerer) curFuncName() string {
	if f := l.mod.Func(l.curFunc); f != nil {
		return f.Name
	}
	return ""
}

// unsignedIntWidth returns the bit width of an unsigned nolang raw integer
// type, or 0 when raw is not an unsigned integer type. MIR flattens every
// integer to i64, so u64/u128 are reported as 64 (no wider representation
// exists at runtime).
func unsignedIntWidth(raw string) int {
	switch raw {
	case "byte", "u8":
		return 8
	case "u16":
		return 16
	case "u32":
		return 32
	case "u64", "u128":
		return 64
	}
	return 0
}

// foldUnsignedIntConst converts a NEGATIVE integer constant into the value it
// denotes under the two's-complement bit pattern of an unsigned target type
// (e.g. -1 as byte becomes 255). Non-negative constants are returned unchanged.
//
// This is the codegen half of the checker's constant-conversion rule for
// `x byte = -1`: MIR flattens scalar integers to i64, so without the fold a
// byte-typed binding would store i64 -1 and read back as -1, while the same
// literal stored into a []byte element (truncated to i8, printed via zext)
// reads back as 255. Folding at the assignment makes both agree.
func foldUnsignedIntConst(v int64, width int) int64 {
	if v >= 0 || width <= 0 {
		return v
	}
	if width > 64 {
		width = 64
	}
	mask := uint64(1)<<uint(width) - 1
	return int64(uint64(v) & mask)
}

// tryUnsignedLitFold lowers an integer literal (optionally negated, e.g. -1)
// that is being assigned to a binding declared with the unsigned raw integer
// type raw. The constant is folded into the target's value range so the stored
// value matches unsigned semantics — `b byte = -1` stores 255, not -1.
//
// Returns (value, true) on success and (NoVal, false) when the node is not a
// negative integer literal, raw is not an unsigned integer type, or the fold is
// a no-op (u64/u128, where i64 cannot represent values above 2^63).
func (l *lowerer) tryUnsignedLitFold(nodeID int32, raw string) (ValueID, bool) {
	w := unsignedIntWidth(raw)
	if w == 0 || nodeID == hir.NoID {
		return NoVal, false
	}
	n := l.pkg.Node(nodeID)
	if n == nil {
		return NoVal, false
	}
	var val int64
	switch n.Kind {
	case hir.KIntLit, hir.KByteLit:
		val = n.Val
	case hir.KPrefix:
		// `-1` lowers as KPrefix("-") over KIntLit(1).
		if l.pkg.Str(n.S) != "-" {
			return NoVal, false
		}
		operand := hir.NoID
		for _, c := range l.pkg.Children(n.Id) {
			operand = c
			break
		}
		if operand == hir.NoID {
			return NoVal, false
		}
		on := l.pkg.Node(operand)
		if on == nil || (on.Kind != hir.KIntLit && on.Kind != hir.KByteLit) {
			return NoVal, false
		}
		val = -on.Val
	default:
		return NoVal, false
	}
	if val >= 0 {
		return NoVal, false
	}
	folded := foldUnsignedIntConst(val, w)
	if folded == val {
		return NoVal, false
	}
	// Keep the i64 MIR type: scalar integer bindings are flattened to i64
	// regardless of their declared width, so emitting a byte-typed constant
	// here would diverge from every other byte assignment path.
	return l.b.EmitInt(OpConst, l.b.Type("i64"), folded, ""), true
}

func (l *lowerer) lowerStmt(id int32) {
	l.stmtVal = NoVal
	n := l.pkg.Node(id)
	if n == nil {
		return
	}
	switch n.Kind {
	case hir.KLet:
		name := l.pkg.Str(n.S)
		// Record the binding's DECLARED raw type (needed later to pick
		// `udiv`/`urem` for unsigned division — MIR flattens integers to i64
		// and loses the u64-ness of the lowered value). Only set when the
		// declaration actually carries an unsigned type; reassignments (no
		// type annotation) leave the earlier entry intact.
		if name != "" {
			if raw := l.pkg.Type(n.Type); isUnsignedRaw(raw) {
				l.localRaw[name] = raw
			}
		}
		// `#{embed='path'}` binding (`DATA []byte`): the bytes are embedded at
		// compile time. Materialize the binding as a module-level `%vec` global
		// whose data pointer targets a private constant byte array, exactly as
		// the legacy backend does (build/llvm/generator.go). The MIR path had NO
		// embed handling at all, so DATA silently became an empty slice
		// (test-embed printed "embed len: 0", then an out-of-bounds read).
		if data := l.pkg.EmbedDataOf(id); len(data) > 0 && name != "" {
			l.lowerEmbedBinding(name, data)
			break
		}
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
			cn := l.pkg.Node(c)
			// A `match`/`if` used as an EXPRESSION value: `r = subject: { arms }`
			// or `r = if cond { a } else { b }`. MIR is statement-oriented and
			// lowerExpr rejects control flow in expression position (returns
			// NoVal), so `r` was never bound and `print(r)` printed nothing. Lower
			// the if as a STATEMENT and capture each arm's final value into a
			// shared slot (l.exprSink); `r` then aliases that slot. Only the FIRST
			// arm creates the slot (typed from its value); later arms store into
			// the same slot. Statement-mode matches (`{ cond -> ... }`) and
			// ordinary `if` uses never set l.exprCapture, so they are unaffected
			// (no regression of the existing MATCH tests). Nested match guards
			// (`ok(it > 127) -> ...`) are themselves if-chains; lowerStmt recurses
			// with capture active, so their arms converge into the same slot.
			if cn != nil && cn.Kind == hir.KIf && name != "" {
				savedCap := l.exprCapture
				savedInit := l.exprSinkInit
				l.exprCapture = true
				l.exprSink = NoVal
				// Seed for a path that produces no value (see exprSinkInit).
				// A re-assignment keeps the binding's current value; a fresh
				// binding has none and falls back to the slot's zero.
				l.exprSinkInit = NoVal
				if old, ok := l.locals[name]; ok {
					l.exprSinkInit = old
				}
				l.lowerIf(cn)
				if l.exprSink != NoVal {
					l.locals[name] = l.exprSink
					if f := l.mod.Func(l.curFunc); f != nil {
						if _, ok := f.LocalTypes[l.exprSink]; !ok {
							f.LocalTypes[l.exprSink] = l.valueTypeOf(l.exprSink)
						}
					}
				} else {
					// All arms side-effecting / no value produced: bind a zero so
					// later reads don't cascade to "unresolved identifier".
					l.locals[name] = l.b.EmitInt(OpConst, l.b.Type("i64"), 0, name)
				}
				l.exprCapture = savedCap
				l.exprSink = NoVal
				l.exprSinkInit = savedInit
				break
			}
			// An anonymous `{...}` initializer takes its struct type from the
			// binding's declared type. A call ARGUMENT is already seeded by
			// lowerCallArgs (see noteAnonymousStructLit); a let / re-assignment is
			// seeded here. Without it the literal stayed untyped, OpSetField had no
			// struct layout to index and the whole module failed with
			// "setfield field" (tests/uninit-output.no: `out = { val: 42,
			// data: [...] }` where `out ?uninit-struct` is the result parameter, so
			// the declared type is option-wrapped — strip the marker and let the
			// option-wrap path below re-add it).
			if l.typeHint != NoType && l.typeHint != l.voidType {
				if ty := l.mod.Type(l.typeHint); ty != nil && ty.Raw != "" {
					l.noteAnonymousStructLit(c, strings.TrimPrefix(ty.Raw, "?"))
				}
			}
			// 整數字面量指派給無號型別的綁定：常數轉換（`b byte = -1` 存 255 而非 -1）。
			// 只對「整個初始化式就是（負）整數字面量」生效，不會外溢到巢狀呼叫實參。
			if name != "" {
				if raw, ok := l.localRaw[name]; ok {
					if folded, foldedOK := l.tryUnsignedLitFold(c, raw); foldedOK {
						val = folded
						break
					}
				}
			}
			val = l.lowerExpr(c)
			break
		}
		l.typeHint = NoType
		if name == "it" && val != NoVal {
			l.itSrc = val
		}
		// Tagged-enum arm destructuring. Inside a `circle(r) -> ...` arm the
		// parser binds each field NAME to the matched subject, so `r` would
		// receive the whole `{tag, payload}` enum and `r * 3.0` then multiplied a
		// struct by a float (void result -> "move src slot" in codegen). Project
		// the binding onto the variant's payload slot instead.
		if l.armVariant != nil && val != NoVal && name != "" && name != "it" {
			if vt := l.valueTypeOf(val); vt != NoType && vt != l.voidType {
				if ty := l.mod.Type(vt); ty != nil && ty.Kind == KindEnum {
					if l.armEnum == nil || l.armEnum.Name == ty.Raw {
						if fi := enumFieldIndex(l.armVariant, name); fi >= 0 && fi < len(l.armVariant.Fields) {
							if ft := l.b.Type(l.armVariant.Fields[fi]); ft != NoType && ft != l.voidType {
								var slot int64
								for k := 0; k < fi; k++ {
									slot += l.enumFieldSlots(l.armVariant.Fields[k])
								}
								fv := l.b.Emit(OpEnumField, ft, []ValueID{val}, "")
								l.mod.Insts[len(l.mod.Insts)-1].Int = slot
								val = fv
							}
						}
					}
				}
			}
		}
		// Match-arm `it` binding whose declared HIR type is the `err` variant
		// marker (the parser puts `t=err` on the synthetic `it = matched` let of
		// an `err ->` arm). The err payload is ALWAYS `str` (the builtin option
		// is `option { ok(v t), nil, err(e str) }`), so bind `it` as a str by
		// peeling the option's payload and reinterpreting it as %str-long. The
		// previous code let `it` keep the WHOLE option type (?fs.file / ?[]byte /
		// ...), so uses like `print('...' - it)` peeled to the OK payload
		// (fs.file / []byte) instead of the err message (str) — tripping
		// opt-verify with a type mismatch (tests/fs-error-complete.no,
		// opt-struct-field.no).
		// Clone the peeled payload: the option ALSO owns the err-payload buffer,
		// so sharing it would double-free on drop — the same trap as the ?str
		// receiver unwrap (resolveCallee). The err payload is bitcast-compatible
		// with the option slot's declared OK-payload type (both 24-byte
		// {len,cap,data} / {a,b,c} structs), so emitMove's peel+bitcast path
		// yields a correct %str-long.
		if val != NoVal {
			if raw := l.pkg.Type(n.Type); raw == "err" {
				if vt := l.valueTypeOf(val); vt != NoType && vt != l.voidType {
					if vty := l.mod.Type(vt); vty != nil && vty.Kind == KindOption {
						// The err payload is ALWAYS `str` (option { ...,
						// err(e str) }), whatever the OK element is, so peel
						// + clone it into `it` as an independently-owned
						// %str-long for BOTH shapes:
						//   - non-scalar (?str / ?fs.file / ...): the err str
						//     fits in the payload slot, so emitMove re-puns
						//     the slot (test_fs_error_complete,
						//     test-opt-struct-field).
						//   - scalar (?i64 / ?u8 / ?bool / ...): with the OLD
						//     flat `%option = { i64, i64 }` the payload was 8
						//     bytes, far too small for a %str-long, so the
						//     peel was deliberately skipped and `it` stayed
						//     the whole option — every str use of it then
						//     emitted IR opt rejected ("defined with type
						//     'i1' but expected '%str-long'"). The unified
						//     layout (`%option = { i64 tag, [N x i64] slot }`,
						//     see the "unified option layout" comment in
						//     codegen.go) gives EVERY option a 24-byte slot,
						//     so the err message fits and the same peel is
						//     correct for scalars too.
						// DEPENDS ON the unified %option slot in codegen.go —
						// scalar err arms cannot be validated until it lands.
						if elem, ok := parseOptionElem(vty.Raw); ok && elem != "" {
							if et := l.mod.Type(l.b.Type(elem)); et != nil {
								switch et.Kind {
								case KindStr, KindStruct:
									peeled := l.b.Emit(OpMove, l.b.Type("str"), []ValueID{val}, "")
									val = l.b.Emit(OpClone, l.b.Type("str"), []ValueID{peeled}, "")
								case KindInt, KindBool, KindChar, KindFloat, KindPtr:
									// SCALAR option (?i64 / ?bool / ?u8 / ...).
									// See the comment above: the unified
									// %option slot is 24 bytes, so the err
									// message is recoverable and `it` is the
									// message — the same semantic as a
									// non-scalar option.
									peeled := l.b.Emit(OpMove, l.b.Type("str"), []ValueID{val}, "")
									val = l.b.Emit(OpClone, l.b.Type("str"), []ValueID{peeled}, "")
								}
							}
						}
					}
				}
			}
		}
		// A binding DECLARED as `str` whose initializer is a []byte value must
		// be reinterpreted, not stored as-is: `data str = fs.read-file(path)`
		// (tests/mem-safety/bug12-builtin-slice-to-str.no) assigns the read-file
		// %vec into a %str-long slot. Legacy does that reinterpret directly; MIR
		// needs an explicit conversion or every later use of `data` (concat,
		// `==`) passes a %vec where a %str-long is expected and opt rejects it.
		//
		// The same applies to a binding DECLARED as `str` whose initializer is a
		// `char` — the implicit `a str = s[0]` conversion (docs/docs/lang/str.md).
		// When the initializer is not a bare identifier the binder ALIASES the
		// name onto the initializer's value (`l.locals[name] = val`, further
		// down), so without an explicit conversion the declared `str` was
		// silently discarded and `a` stayed the char code point: `print(a)`
		// printed "104" instead of "h" and `a - b` did integer arithmetic
		// instead of concatenation. The conversion is an OpMove into `str`;
		// codegen's emitMove sees a char source against a %str-long destination
		// and encodes it with @str_from_cp (UTF-8) rather than @str_from_i64
		// (decimal text).
		if val != NoVal {
			if dt := l.typeOfNode(n); dt != NoType && dt != l.voidType {
				if dty := l.mod.Type(dt); dty != nil && dty.Raw == "str" {
					if vt := l.valueTypeOf(val); vt != NoType && vt != l.voidType {
						if vty := l.mod.Type(vt); vty != nil {
							switch vty.Kind {
							case KindSlice:
								val = l.b.Emit(OpStrFromVec, l.b.Type("str"), []ValueID{val}, "")
							case KindChar:
								val = l.b.Emit(OpMove, l.b.Type("str"), []ValueID{val}, "")
								// Pin the value's type to `str`. The generic
								// LocalTypes back-fill further down derives the
								// type from the HIR child node, and a KIndex
								// child resolves to `char` — which would retype
								// this %str-long value as char and make codegen
								// emit a `store i64` into a %str-long slot.
								if f := l.mod.Func(l.curFunc); f != nil {
									f.LocalTypes[val] = l.b.Type("str")
								}
							}
						}
					}
				}
			}
		}
		// `?=` desugaring: the initializer is an OPTION but the binding's
		// declared type is the PAYLOAD (`let s=size t=i64` inside the
		// desugared `__unwrap` match — see fs.file.read-bytes). Legacy stores
		// the option's data field into the scalar (build/llvm
		// `__unwrap_606.data.gep`); MIR must do the same. Without this the
		// binding stays `?T` and every later use — `size == 0`,
		// `size - total`, `with-len(size)` — passes the whole %option struct
		// where an i64 is expected (tests/open-read.no).
		if val != NoVal {
			// A declared type of `err` / `err | nil` is a VARIANT MARKER the
			// parser puts on the synthetic `it` binding of a match arm
			// (`let s=it t=err`), not a real type. Taking it literally retypes
			// the binding as "err", and the next `it.read-bytes()` then
			// resolves to `err.read-bytes` — "unknown callee"
			// (tests/open-read.no, tests/fs-struct.no).
			if dt := l.letDeclaredType(n); dt != NoType && dt != l.voidType {
				if dty := l.mod.Type(dt); dty != nil && dty.Kind != KindOption {
					if vt := l.valueTypeOf(val); vt != NoType && vt != l.voidType {
						if vty := l.mod.Type(vt); vty != nil && vty.Kind == KindOption {
							// Only peel a NON-HEAP payload. OpMove is a bitwise
							// copy, so peeling an owned payload (?str -> str)
							// aliases the SAME heap pointer: the binding would
							// then be dropped after the option's own drop frees
							// it, i.e. a real double-free that checkMoves
							// reports as "value N dropped after move". That is
							// exactly the match-arm narrowing (`n ?str` narrowed
							// to `str` in the default arm) — tests/option-test.no
							// and tests/option-match.no, which the legacy
							// backend handles by keeping the option intact.
							if elem, ok := parseOptionElem(vty.Raw); ok {
								if et := l.b.Type(elem); et != NoType {
									if ety := l.mod.Type(et); ety == nil || !ety.Owned {
										val = l.b.Emit(OpMove, dt, []ValueID{val}, "")
									} else if dty.Raw == elem {
										// Owned payload (?str -> str,
										// ?Struct -> Struct) whose type IS
										// the binding's declared type: the
										// match-arm narrowing (`it` in an
										// `ok ->` arm of a `?str` match) and
										// the `#{index-out=DEF}` desugar's
										// `x = it` arm both land here.
										//
										// A bare OpMove would alias the
										// option's heap buffer — the binding
										// gets its OWN drop while the option
										// keeps its own, so the same bytes
										// would be freed twice. That is why
										// this used to be skipped outright
										// (option-test.no / option-match.no).
										// Skipping is not an option either: the
										// binding's slot is allocated as the
										// payload type, so leaving the %option
										// in it made every later use emit IR
										// the verifier rejects ("'%lv' defined
										// with type '%option' ... but expected
										// '%str-long'") — `#{index-out=0}
										// s = xs[i]` followed by `f(s)` was the
										// first live case (nonpm cmd-add).
										//
										// Peel + CLONE instead, exactly as the
										// err arm above does: the binding owns
										// an independent copy, the option keeps
										// its own, and each owner frees its
										// buffer exactly once.
										peeled := l.b.Emit(OpMove, dt, []ValueID{val}, "")
										val = l.b.Emit(OpClone, dt, []ValueID{peeled}, "")
									}
								}
							}
						}
					}
				}
			}
		}
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
		// VIEW binding (`&T` / `?&T`): `v &point = self` BORROWS the source's
		// storage instead of copying it. No allocation, no ownership transfer,
		// and no drop — this is what makes `json.get = (key str) (child ?&json)`
		// cheap (a pointer, not a copy of the struct).
		if val != NoVal && l.viewTargetType(name, n) != NoType {
			if bv := l.lowerBorrow(childID); bv != NoVal {
				val = bv
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
				// Match-arm synthetic `it` is bound PER ARM but the `locals` map is
				// function-global, so every arm after the first finds `it` already
				// declared. The arms can carry DIFFERENT `it` types (err arm →
				// str, ok arm → fs.file / ?fs.file). The previous code mutated the
				// SHARED slot's LocalType to follow the new value
				// (`f.LocalTypes[existing] = typ`), which CORRUPTED the earlier
				// arm's bindings that already captured the same value id: the f1
				// err arm's `it` (value 24) holds a cloned `str` produced by FIX-1's
				// OpClone, but the f1 ok arm then retyped value 24 to %fs_file, so
				// at codegen time `ptype(24)` was %fs_file and the err clone emitted
				// `call %fs_file @str_clone` — an opt-verify mismatch
				// (tests/fs-error-complete.no). The fix is to give EACH arm its
				// OWN value for `it` (a fresh move of the arm's matched value) and
				// repoint `locals["it"]` at it, leaving the prior arm's value id and
				// its type untouched. Resolution of `it.foo` then uses the correct
				// per-arm type, and the prior arm's clone keeps its `str` type.
				// This introduces one extra move (a by-value copy of an option /
				// struct), which the memory analysis already handles with a single
				// drop for the fresh slot — no double-free.
				if name == "it" {
					// A match arm's synthetic `let it = <matched>` rebinds `it` to that
					// arm's own subject — including for a match NESTED inside another
					// arm (`child: { ok -> it.get-str(...) }` must see `child`, not the
					// outer match's subject). Previously the nested case was skipped and
					// `it` kept pointing at the OUTER subject, so an inner arm silently
					// read the parent value (tests/mem-safety/json-nested-match.no:
					// `it.get-str('inner')` looked up the key on the PARENT object and
					// reported "not found"). Bind unconditionally; `val` is consumed by
					// the binding (aliased, not moved), so no drop is needed here.
					if typ := l.valueTypeOf(val); typ != NoType && typ != l.voidType {
						l.locals[name] = val
						break
					}
					l.b.EmitMoveInto(existing, val)
					break
				}
				// Self-assignment (`it = it`): the RHS lowered to the same slot as
				// the target, so no ownership transfer occurs. Emitting an OpDrop
				// here would free the slot's current content and the following
				// EmitMoveInto(existing, val) (== move slot->slot) would move
				// already-freed memory -> double free / use-after-free (observed as
				// a runtime SIGTRAP in str.replace-n). The memory analysis inserts
				// the single correct exit-drop for this slot, so skip entirely.
				if val == existing {
					break
				}
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
			// Re-binding a MODULE-LEVEL binding. A script-level `px2 = 0.0`
			// whose initializer folds to a constant is registered as a module
			// global (synthesizeMainForTopLevel keeps it out of the synthetic
			// `main`), so it has no local slot. The READ side already resolves
			// it through lowerIdent -> lowerGlobalRef (the global's @slot), but
			// the WRITE side used to fall through to the "new binding" path
			// below, binding the name to a FRESH local slot. Read and write then
			// target different storage: a loop like `px2 = px2 + mi * vi` re-read
			// the never-updated global every iteration and produced only the
			// last term (test-tmp-nbody-debug printed -5.04e-05 instead of
			// -3.87e-04). Store into the global and bind the name to the global's
			// value so both sides agree. Owned (heap) types are left to the
			// fresh-slot path: their move needs clone/drop bookkeeping the
			// global slot does not have.
			if _, isGlobal := l.globals[name]; isGlobal {
				if gv := l.lowerGlobalRef(name); gv != NoVal && !l.isOwnedLocal(gv) {
					if gvt := l.valueTypeOf(gv); gvt != NoType && gvt != l.voidType {
						if vt := l.valueTypeOf(val); vt == gvt {
							l.b.EmitMoveInto(gv, val)
							l.locals[name] = gv
							break
						}
					}
				}
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
				// Same hazard for a module-level binding: `K = 100` lowers to a
				// global VALUE, so `t = K` binds `t` onto @K's own storage and a
				// later `t = t + 1` writes back into the GLOBAL — corrupting the
				// constant for every reader, and the next call then starts from
				// the previous call's leftover instead of from K. str-map's
				// FNV-OFFSET was the victim: the hash differed on every call, so
				// put() and get() landed on different slots and every lookup
				// missed. Restricted to SCALARS: array/vec/str/struct globals
				// (crypto SBOX tables, #{embed} byte arrays) keep the existing
				// alias so reads still GEP straight from @global and no 2KB+
				// aggregate copy is introduced.
				cnName := l.pkg.Str(cn.S)
				_, isLocal := l.locals[cnName]
				isScalarGlobal := false
				if _, isGlobal := l.globals[cnName]; isGlobal {
					if vt := l.mod.Type(l.valueTypeOf(val)); vt != nil {
						switch vt.Kind {
						case KindInt, KindFloat, KindBool, KindChar, KindPtr:
							isScalarGlobal = true
						}
					}
				}
				if (isLocal || isScalarGlobal) && !l.isOwnedLocal(val) {
					typ := l.valueTypeOf(val)
					if typ == NoType || typ == l.voidType {
						typ = l.typeOfNode(cn)
					}
					if typ != NoType && typ != l.voidType {
						fresh := l.b.Emit(OpMove, typ, []ValueID{val}, "")
						// A copied async handle keeps its task's result type.
						if rt, ok := l.asyncResTypes[val]; ok {
							l.asyncResTypes[fresh] = rt
						}
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
					nt := l.typeOfNode(l.pkg.Node(childID))
					if nt == NoType || nt == l.voidType {
						// The bound value usually ALREADY carries a real type: a
						// module global produced by lowerGlobalRef, or a call
						// result typed by Builder.Emit. The KLet's child node is
						// very often a bare `ident` / `call` that carries NO type
						// of its own, so typeOfNode returns void here. Propagating
						// that void would RETYPE a live value to void and make
						// codegen emit `undef` for every later use of it — e.g.
						// top-level `x = 3` referenced by a match arm's synthetic
						// `let it = x` (match on a module-global subject). Keep
						// the value's own type whenever it is known.
						if vt := l.valueTypeOf(val); vt != NoType && vt != l.voidType {
							nt = vt
						}
					}
					f.LocalTypes[val] = nt
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
		if child == hir.NoID {
			return
		}
		cn := l.pkg.Node(child)
		if cn != nil && (cn.Kind == hir.KIf || cn.Kind == hir.KFor) {
			// Statement-mode control flow: lower it directly. When this
			// statement is itself an arm of a match/if being captured as an
			// expression value, publish the captured slot as this statement's
			// value so the caller can read it back.
			if cn.Kind == hir.KIf {
				l.lowerIf(cn)
			} else {
				l.lowerFor(cn)
			}
			if l.exprCapture && l.exprSink != NoVal {
				l.stmtVal = l.exprSink
			}
			return
		}
		// Bare expression statement (`'hello'` in `r = if 1 { 'hello' }`):
		// record its value for the enclosing control-flow-expression capture,
		// then lower it exactly ONCE. (Returning here is essential — the old
		// trailing `for` loop re-lowered this same child, duplicating every
		// statement in the program.)
		l.stmtVal = l.lowerExpr(child)
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
		if l.exprCapture && l.exprSink != NoVal {
			l.stmtVal = l.exprSink
		}
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

// blockStartsWithIt reports whether the given block's first statement is the
// synthetic `let it = <matched>` that the parser prepends to every match arm
// body (lowering.go ~1180-1228). A plain `if` body never leads with `let it`,
// so this is a reliable match-arm detector used to maintain matchDepth.
func (l *lowerer) blockStartsWithIt(blockID int32) bool {
	if blockID == hir.NoID {
		return false
	}
	for _, c := range l.pkg.Children(blockID) {
		stmt := l.pkg.Node(c)
		if stmt == nil {
			continue
		}
		if stmt.Kind == hir.KLet && l.pkg.Str(stmt.S) == "it" {
			return true
		}
		// Only inspect the first top-level statement of the block.
		return false
	}
	return false
}

func (l *lowerer) lowerIf(n *hir.Node) {
	condID := l.slot(n.Id, "cond")
	thenID := l.slot(n.Id, "then")
	elseID := l.slot(n.Id, "else")

	// Detect match arms: a match desugars to an if-chain where each arm's body
	// block starts with the synthetic `let it = <matched>` (the parser prepends
	// it; see lowering.go ~1180-1228). A plain `if` never leads with `let it`,
	// so this reliably distinguishes a match arm from an ordinary block. We bump
	// matchDepth while lowering such a block so that a `let it` nested inside it
	// (a nested match arm) is recognized as nested and does NOT clobber the
	// outer `it` (parser intent: lowering.go:1138). The else-chain continuation
	// of a match is itself an `if`, not a `let it` block, so it is NOT counted —
	// only genuine arm bodies increment depth.
	thenIsArm := l.blockStartsWithIt(thenID)
	elseIsArm := l.blockStartsWithIt(elseID)

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
	// initBlk dominates both arms; it is where the capture slot's seed has to
	// be stored (see the seeding block at the end of this function).
	initBlk := l.b.CurBlock
	if condID != hir.NoID {
		// An arm condition may be a tagged-enum variant pattern
		// (`s: { circle(r) -> ... }`), which the parser desugars into an
		// equality test against the variant constructor. Mark the condition
		// position so lowerEnumCtor can record WHICH variant the arm is for;
		// the arm body's bindings (`r`) are then projected onto that variant's
		// payload instead of receiving the whole enum.
		l.inArmCond = true
		// Disambiguate the arm's variant name by the SUBJECT's type: an
		// equality test `p == fail` does not say which enum `fail` belongs to
		// on its own, and both `a-res` and `b-res` declare one. Read the
		// subject's type from the HIR (no lowering, so no duplicate
		// instructions) instead of guessing globally.
		if cn := l.pkg.Node(condID); cn != nil && (l.pkg.Str(cn.S) == "==" || l.pkg.Str(cn.S) == "!=") {
			for _, ch := range l.pkg.Children(condID) {
				if chn := l.pkg.Node(ch); chn != nil {
					if lt := l.typeOfNode(chn); lt != NoType && lt != l.voidType {
						if ty := l.mod.Type(lt); ty != nil && ty.Kind == KindEnum {
							l.armEnumPrefer = ty.Raw
							// Only a plain identifier is safe to re-evaluate in
							// the arm body; anything else (a call, an index)
							// may have side effects.
							if chn.Kind == hir.KIdent {
								l.armSubjectID = ch
							}
						}
					}
				}
				break // first child is the subject
			}
		}
		c := l.lowerExpr(condID)
		l.inArmCond = false
		l.armEnumPrefer = ""
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
		// Re-read the subject HERE rather than trusting `it`: after the first
		// arm, `it` is re-assigned into a shared slot, and a later arm's
		// projection could still see the earlier subject.
		if l.armVariant != nil && l.armSubjectID != hir.NoID {
			if sv := l.lowerExpr(l.armSubjectID); sv != NoVal {
				l.itSrc = sv
			}
		}
		l.armSubjectID = hir.NoID
		// A NESTED match arm (matchDepth > 0 before the increment) rebinds the
		// function-global `it` slot to its own subject — that rebinding is
		// REQUIRED, the arm body must see the inner value
		// (tests/mem-safety/json-nested-match.no). But `it` has to revert
		// to the enclosing arm's subject once this arm ENDS, otherwise every
		// `it` read after the nested match silently resolves to the INNER
		// subject:
		//   d ?yaml = yaml.parse(src)
		//   d: { -> {
		//           a ?yaml = it.at(0)      ; it = document
		//           a: { -> ... }           ; inner arm rebinds it = child
		//           b ?yaml = it.at(1)      ; ⚠ it is STILL the child -> nil
		//       } }
		// The parser cannot fix this (a match is an expression, so there is no
		// statement position after it to restore into), so restore here —
		// `locals["it"]` is a plain name->value map with no ownership attached.
		nestedArm := thenIsArm && l.matchDepth > 0
		var savedItVal ValueID
		var savedItOK bool
		if nestedArm {
			savedItVal, savedItOK = l.locals["it"]
		}
		if thenIsArm {
			l.matchDepth++
		}
		savedItSrc := l.itSrc
		armVal := l.lowerBlock(thenID)
		// The variant pattern only applies to THIS arm's body.
		l.armVariant = nil
		l.armEnum = nil
		l.itSrc = savedItSrc
		if thenIsArm {
			l.matchDepth--
		}
		if nestedArm {
			if savedItOK {
				l.locals["it"] = savedItVal
			} else {
				delete(l.locals, "it")
			}
		}
		l.captureArmValue(armVal)
	}
	if l.mod.Block(thenBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	l.b.SetBlock(elseBlk)
	if elseID != hir.NoID {
		nestedArm := elseIsArm && l.matchDepth > 0
		var savedItVal ValueID
		var savedItOK bool
		if nestedArm {
			savedItVal, savedItOK = l.locals["it"]
		}
		if elseIsArm {
			l.matchDepth++
		}
		armVal := l.lowerBlock(elseID)
		if elseIsArm {
			l.matchDepth--
		}
		// Same restore as the then-branch: a nested arm's `it` rebind must not
		// outlive the arm (see the comment there).
		if nestedArm {
			if savedItOK {
				l.locals["it"] = savedItVal
			} else {
				delete(l.locals, "it")
			}
		}
		l.captureArmValue(armVal)
	}
	if l.mod.Block(elseBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	// Seed the capture slot in the block that DOMINATES both arms.
	//
	// captureArmValue creates the slot lazily, on the first arm that produces a
	// value — which puts its zero-initialiser INSIDE that arm. Any path that
	// skips the value-producing arm (a false condition, a `?T` pipeline node
	// that short-circuited, a match with no arm matching) therefore read a slot
	// that was never stored to, i.e. uninitialised stack. The garbage happened
	// to be the arm's value often enough to hide it: `x = cond -> 42` printed
	// 42 on the false path, and `x = print('F') -> might-fail(bad) -> 99`
	// ignored the short circuit entirely.
	//
	// Seeding here — in initBlk, after the arms are known — is defined for
	// every path: the arms overwrite it when they produce a value, and
	// everything else reads either the binding's previous value or zero.
	// (Appending an instruction to an already-terminated block is safe: MIR
	// keeps Term separate from Insts and codegen emits Insts first.)
	if l.exprCapture && l.exprSink != NoVal {
		st := l.valueTypeOf(l.exprSink)
		src := l.exprSinkInit
		if src == NoVal || l.valueTypeOf(src) != st {
			src = l.b.EmitInt(OpConst, st, 0, "")
		}
		savedBlk := l.b.CurBlock
		l.b.SetBlock(initBlk)
		// Owned seed: the binding's own slot still drops its copy, so the seed
		// must own a separate one.
		if l.isOwnedLocal(src) {
			src = l.b.Emit(OpClone, st, []ValueID{src}, "")
		}
		l.b.EmitMoveInto(l.exprSink, src)
		l.b.SetBlock(savedBlk)
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

// captureArmValue records one match/if arm's final value into the shared
// expression slot l.exprSink. The slot is created (zero-initialized, of the
// arm value's type) on the FIRST captured arm; later arms store into the same
// slot. Only active while l.exprCapture is set (a match/if used as an
// expression value). Owned values (str/vec/option) are CLONED before storing so
// the slot owns its own copy and the arm's original value drops exactly once
// (no double-free). A value equal to l.exprSink itself means a nested guard
// arm already wrote the slot — skip storing the slot into itself.
func (l *lowerer) captureArmValue(v ValueID) {
	if v == NoVal || !l.exprCapture {
		return
	}
	if l.exprSink == NoVal {
		typ := l.valueTypeOf(v)
		if typ == NoType || typ == l.voidType {
			typ = l.b.Type("i64")
		}
		l.exprSink = l.b.EmitInt(OpConst, typ, 0, "")
	}
	if v == l.exprSink {
		return
	}
	typ := l.valueTypeOf(l.exprSink)
	stored := v
	if l.isOwnedLocal(v) {
		stored = l.b.Emit(OpClone, typ, []ValueID{v}, "")
	}
	l.b.EmitMoveInto(l.exprSink, stored)
}

// lowerCond lowers the ternary `c ? a : b` (hir.KCond; children are
// [cond, consequence, alternative]).
//
// MIR had NO case for KCond at all, so the whole expression lowered to NoVal:
// `max = sum > 10 ? sum : 10` never bound `max`, and every later read of it
// cascaded into "unresolved identifier" / "unresolved format field max"
// (tests/all.no). A DECLARED binding (`max i64 = c ? a : b`) took the
// "declaration with no initializer" zero-init fallback, so it compiled but
// silently always held 0 — worse than an error, because the wrong value is
// invisible.
//
// The arms are lowered as real branches into a shared result slot, reusing the
// same capture machinery as `r = if c { a } else { b }`. Unlike that path the
// slot is created in the block that DOMINATES both arms (before the cond-br),
// so the merge block can legally read it whichever way the branch went.
func (l *lowerer) lowerCond(id int32) ValueID {
	var condID, thenID, elseID int32 = hir.NoID, hir.NoID, hir.NoID
	i := 0
	for _, c := range l.pkg.Children(id) {
		switch i {
		case 0:
			condID = c
		case 1:
			thenID = c
		case 2:
			elseID = c
		}
		i++
	}
	if condID == hir.NoID || thenID == hir.NoID {
		return NoVal
	}
	// Result slot type: prefer the consequence's inferred type, else the
	// alternative's, else i64. Created here so it dominates both arms.
	resT := l.typeOfNode(l.pkg.Node(thenID))
	if resT == NoType || resT == l.voidType {
		if elseID != hir.NoID {
			resT = l.typeOfNode(l.pkg.Node(elseID))
		}
	}
	if resT == NoType || resT == l.voidType {
		resT = l.b.Type("i64")
	}
	sink := l.b.EmitInt(OpConst, resT, 0, "")

	thenBlk := l.b.NewBlock("cond.then")
	elseBlk := l.b.NewBlock("cond.else")
	mergeBlk := l.b.NewBlock("cond.merge")
	cv := l.lowerExpr(condID)
	if cv != NoVal {
		l.b.Terminate(OpCondBr, []ValueID{cv}, []BlockID{thenBlk, elseBlk}, "")
	} else {
		l.b.Terminate(OpBr, nil, []BlockID{thenBlk}, "")
	}

	savedCap, savedSink := l.exprCapture, l.exprSink
	l.exprCapture = true
	l.exprSink = sink

	l.b.SetBlock(thenBlk)
	if v := l.lowerExpr(thenID); v != NoVal {
		l.captureArmValue(v)
	}
	if l.mod.Block(thenBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	l.b.SetBlock(elseBlk)
	if elseID != hir.NoID {
		if v := l.lowerExpr(elseID); v != NoVal {
			l.captureArmValue(v)
		}
	}
	if l.mod.Block(elseBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	l.exprCapture, l.exprSink = savedCap, savedSink
	l.b.SetBlock(mergeBlk)
	return sink
}

// lowerSafeIndex lowers a bounds-checked element read whose result is an
// option: in range -> ok(elem), out of range -> nil (tag 1). It backs the
// parser's safe-index desugar (`x ?= v[i]` and `#{index-out=DEF} x = v[i]`),
// which both lower the read into a `?elem` binding and then match on it.
//
// The result slot is created in the block that dominates both arms (as an
// option-typed constant, i.e. nil), so the arms only ever overwrite it — MIR
// has no phi, every branch convergence goes through such a shared slot.
func (l *lowerer) lowerSafeIndex(arrV, idxV ValueID, elemT, optT TypeID) ValueID {
	i64T := l.b.Type("i64")
	boolT := l.b.Type("bool")
	if i64T == l.voidType || boolT == l.voidType {
		return NoVal
	}
	n := l.b.Emit(OpLen, i64T, []ValueID{arrV}, "")
	if n == NoVal {
		return NoVal
	}
	zero := l.b.EmitInt(OpConst, i64T, 0, "")
	nonneg := l.b.Emit(OpGe, boolT, []ValueID{idxV, zero}, "")
	inRange := l.b.Emit(OpLt, boolT, []ValueID{idxV, n}, "")
	ok := l.b.Emit(OpAnd, boolT, []ValueID{nonneg, inRange}, "")

	sink := l.b.EmitInt(OpConst, optT, 0, "") // nil: tag=1, zero payload

	okBlk := l.b.NewBlock("idx.ok")
	noneBlk := l.b.NewBlock("idx.none")
	mergeBlk := l.b.NewBlock("idx.merge")
	l.b.Terminate(OpCondBr, []ValueID{ok}, []BlockID{okBlk, noneBlk}, "")

	l.b.SetBlock(okBlk)
	e := l.b.Emit(OpIndex, elemT, []ValueID{arrV, idxV}, "")
	if e != NoVal {
		// An owned element (str) read out of the container is only BORROWED
		// (OpIndex aliases the owner's storage). Wrapping it into the option
		// makes the move analysis treat the option as its owner, so the later
		// drop of the option would free a buffer the container still owns.
		// Clone so each side owns its own copy.
		if et := l.mod.Type(elemT); et != nil && et.Kind == KindStr {
			e = l.b.Emit(OpClone, elemT, []ValueID{e}, "")
		}
		wrapped := l.b.EmitOptionWrap(optT, 0, e)
		l.b.EmitMoveInto(sink, wrapped)
	}
	if l.mod.Block(okBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	// The none arm leaves the slot at its nil initial value.
	l.b.SetBlock(noneBlk)
	if l.mod.Block(noneBlk).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{mergeBlk}, "")
	}

	l.b.SetBlock(mergeBlk)
	return sink
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

// resultOutParamFor returns the MIR value of the enclosing function's result
// out-parameter when `name` is currently bound to it, and NoVal otherwise.
//
// A Nolang function returns through named out-params (`skip-ws = (s str, pos i64)
// (p i64)`), so a loop that uses the result name as its loop variable
// (`p <- [pos..s.len-bytes()): { … }`) is writing the RETURN VALUE. Binding the
// loop variable to a freshly minted local therefore left the out-param at its
// zero-initialized default and the function always returned 0 — which made
// json-pool.skip-ws return 0 for every input and broke json.parse. Reusing the
// out-param's slot keeps the loop's writes visible to the caller.
func (l *lowerer) resultOutParamFor(name string) ValueID {
	if name == "" {
		return NoVal
	}
	f := l.mod.Func(l.curFunc)
	if f == nil {
		return NoVal
	}
	cur, ok := l.locals[name]
	if !ok || cur == NoVal {
		return NoVal
	}
	for _, rv := range f.ResultParams {
		if rv == cur {
			return rv
		}
	}
	return NoVal
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
		leftInc := rn.Has(hir.FlagLeftInc)   // '[' -> start inclusive
		rightInc := rn.Has(hir.FlagRightInc) // ']' -> end inclusive
		elemT := l.b.Type("i64")
		// Allocate the loop variable as a local with its own alloca slot. Using
		// b.Param would mint a value id WITHOUT a slot (params are only slotted
		// when registered into f.Params, which loop vars are not) -> the init
		// move would hit "move destination has no slot". Emitting a zero const
		// gives it a Dst-backed slot; the init move below overwrites it.
		// resultOutParamFor: when the loop variable IS the function's named
		// result out-param, drive that slot directly instead of a fresh local.
		iSlot := l.resultOutParamFor(varName)
		if iSlot == NoVal {
			iSlot = l.b.EmitInt(OpConst, elemT, 0, varName)
		}
		l.locals[varName] = iSlot
		// init v = lo +/- (leftInc?0:1), stepping toward hi. Nolang ranges are
		// bidirectional: `for i in [5..0)` counts DOWN (5,4,3,2,1). The loop
		// direction is inferred from the bounds (ascending iff lo <= hi); the
		// step and the comparison operator both depend on it. The IR has no
		// select/mux op, so the direction is resolved with branches on the
		// loop-invariant bounds (opt hoists them out of the loop body).
		exclOff := int64(0)
		if !leftInc {
			exclOff = 1
		}
		offV := l.b.EmitInt(OpConst, elemT, exclOff, "")
		initAsc := l.b.Emit(OpAdd, elemT, []ValueID{startV, offV}, "")
		initDesc := l.b.Emit(OpSub, elemT, []ValueID{startV, offV}, "")
		initAscB := l.b.NewBlock("rng.init.asc")
		initDescB := l.b.NewBlock("rng.init.desc")
		initJoin := l.b.NewBlock("rng.init.join")
		l.b.SetBlock(pre)
		l.b.Terminate(OpCondBr, []ValueID{l.b.Emit(OpLe, l.b.Type("bool"), []ValueID{startV, endV}, "")},
			[]BlockID{initAscB, initDescB}, "")
		l.b.SetBlock(initAscB)
		l.b.EmitMoveInto(iSlot, initAsc)
		l.b.Terminate(OpBr, nil, []BlockID{initJoin}, "")
		l.b.SetBlock(initDescB)
		l.b.EmitMoveInto(iSlot, initDesc)
		l.b.Terminate(OpBr, nil, []BlockID{initJoin}, "")

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
		l.b.SetBlock(initJoin)
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
		l.b.SetBlock(header)
		// Ascending: `i < hi` (']' -> `<=`). Descending: `i > hi` (']' -> `>=`).
		ascCmpOp, descCmpOp := OpLt, OpGt
		if rightInc {
			ascCmpOp, descCmpOp = OpLe, OpGe
		}
		hdAsc := l.b.NewBlock("for.hd.asc")
		hdDesc := l.b.NewBlock("for.hd.desc")
		l.b.Terminate(OpCondBr, []ValueID{l.b.Emit(OpLe, l.b.Type("bool"), []ValueID{startV, endV}, "")},
			[]BlockID{hdAsc, hdDesc}, "")
		l.b.SetBlock(hdAsc)
		ca := l.b.Emit(ascCmpOp, l.b.Type("bool"), []ValueID{iSlot, endV}, "")
		l.b.Terminate(OpCondBr, []ValueID{ca}, []BlockID{body, exit}, "")
		l.b.SetBlock(hdDesc)
		cd := l.b.Emit(descCmpOp, l.b.Type("bool"), []ValueID{iSlot, endV}, "")
		l.b.Terminate(OpCondBr, []ValueID{cd}, []BlockID{body, exit}, "")
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
		// Step: +1 ascending, -1 descending (direction resolved by branch).
		upAsc := l.b.NewBlock("for.upd.asc")
		upDesc := l.b.NewBlock("for.upd.desc")
		l.b.Terminate(OpCondBr, []ValueID{l.b.Emit(OpLe, l.b.Type("bool"), []ValueID{startV, endV}, "")},
			[]BlockID{upAsc, upDesc}, "")
		l.b.SetBlock(upAsc)
		l.b.EmitMoveInto(iSlot, l.b.Emit(OpAdd, elemT, []ValueID{iSlot, l.b.EmitInt(OpConst, elemT, 1, "")}, ""))
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
		l.b.SetBlock(upDesc)
		l.b.EmitMoveInto(iSlot, l.b.Emit(OpSub, elemT, []ValueID{iSlot, l.b.EmitInt(OpConst, elemT, 1, "")}, ""))
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
	// --- string form: for c <- s  (c is a CODE POINT, not a byte) ---
	// docs/docs/lang/str.md promises that `for c <- s` walks Unicode code
	// points and is the O(n) alternative to the O(n^2) `for i <- [0..s.len())`
	// + `s[i]` pattern. The generic collection loop below indexes with a
	// LINEAR index and steps by 1, which over a str walks BYTES (measured
	// before this branch: '日本語' yielded 230,151,165,230,156,172,232,170,158
	// instead of 26085,26412,35486). Give str its own loop: a byte cursor
	// advanced by the decoded code point's UTF-8 width.
	if ty := l.valueTypeOf(arrV); ty != NoType {
		if sty := l.mod.Type(ty); sty != nil && sty.Raw == "str" {
			l.lowerStrRangeFor(arrV, varName, bodyID, pre, enclosingCont)
			return
		}
	}
	elemT := l.elementTypeOf(arrV)
	if elemT == l.voidType {
		l.unsupported(l.curFuncName(), "for-range", "cannot determine element type of collection")
		return
	}
	idxT := l.b.Type("i64")
	// Determine the collection length. Fixed arrays have a compile-time
	// size (KindArray.Sizes[0]); slices/vecs need a runtime OpLen (field 0
	// of %vec). Support both so `for i in slice_expr` works, not just
	// `for i in [a, b, c]`.
	var lengthV ValueID = NoVal
	if t := l.valueTypeOf(arrV); t != NoType {
		if ty := l.mod.Type(t); ty != nil && ty.Kind == KindArray && len(ty.Sizes) > 0 {
			lengthV = l.b.EmitInt(OpConst, idxT, ty.Sizes[0], "")
		}
	}
	if lengthV == NoVal {
		// Runtime length: emit OpLen on the collection value. codegen
		// extracts field 0 (len) from %vec / %str-long.
		lengthV = l.b.Emit(OpLen, idxT, []ValueID{arrV}, "")
	}

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
	condV := l.b.Emit(OpLt, l.b.Type("bool"), []ValueID{idxSlot, lengthV}, "")
	l.b.Terminate(OpCondBr, []ValueID{condV}, []BlockID{body, exit}, "")

	// Body entry: bind v = collection[idx]. Emitted here (not in pre) so it runs
	// once per iteration, reloading the current element into v's slot.
	l.b.SetBlock(body)
	iSlot := l.b.Emit(OpIndex, elemT, []ValueID{arrV, idxSlot}, "")
	if outP := l.resultOutParamFor(varName); outP != NoVal {
		// `for v in [a,b,c]` where v is the function's result out-param: move
		// each element into the out-param slot so the caller sees it.
		l.b.EmitMoveInto(outP, iSlot)
		iSlot = outP
	}
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

// lowerStrRangeFor lowers `for c <- s` on a `str`: c is bound to each UTF-8
// CODE POINT of s, in order, as a `char` (i32) — the traversal counterpart of
// the code-point indexed `s[i]` (see codegen.emitStrCpIndex and
// docs/docs/lang/str.md).
//
// Shape:
//
//	off = 0
//	while off < s.len-bytes() {
//	    c   = utf8_at(s, off)        ; OpUtf8At — one sequence, O(1)
//	    w   = 1 + (c >= 0x80) + (c >= 0x800) + (c >= 0x10000)
//	    <body>
//	    off = off + w
//	}
//
// The byte length comes from OpLen (field 0 of %str-long, which is the BYTE
// count — `s.len()` is a std function that counts code points on top of it).
// The cursor advances by the width DERIVED FROM THE CODE POINT rather than by
// re-scanning: the three UTF-8 length classes are nested thresholds, so the
// arithmetic is exact, and the resulting walk is a single O(n) pass.
//
// The width is computed and parked in its own slot BEFORE the body runs, so a
// body that reassigns the loop variable cannot shift the cursor. `c` is bound
// to a private slot seeded from the decoded value; the body sees a copy.
func (l *lowerer) lowerStrRangeFor(arrV ValueID, varName string, bodyID int32, pre BlockID, enclosingCont BlockID) {
	idxT := l.b.Type("i64")
	charT := l.b.Type("char")
	boolT := l.b.Type("bool")
	zero := func() ValueID { return l.b.EmitInt(OpConst, idxT, 0, "") }

	// Byte length of the buffer (the cursor's bound).
	byteLen := l.b.Emit(OpLen, idxT, []ValueID{arrV}, "")
	// Each of these becomes a real Dst-backed alloca slot (see lowerRangeFor's
	// note: a bare Param value id would have no slot and the moves would fail).
	offSlot := l.b.EmitInt(OpConst, idxT, 0, varName+"#off")
	cpSlot := l.b.EmitInt(OpConst, charT, 0, varName+"#cp")
	widthSlot := l.b.EmitInt(OpConst, idxT, 0, varName+"#w")

	l.b.SetBlock(pre)
	l.b.EmitMoveInto(offSlot, zero())

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

	l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	l.b.SetBlock(header)
	condV := l.b.Emit(OpLt, boolT, []ValueID{offSlot, byteLen}, "")
	l.b.Terminate(OpCondBr, []ValueID{condV}, []BlockID{body, exit}, "")

	l.b.SetBlock(body)
	cpV := l.b.Emit(OpUtf8At, charT, []ValueID{arrV, offSlot}, "")
	l.b.EmitMoveInto(cpSlot, cpV)
	// w = 1 + (c >= 0x80) + (c >= 0x800) + (c >= 0x10000)
	w := l.b.EmitInt(OpConst, idxT, 1, "")
	for _, bound := range []int64{0x80, 0x800, 0x10000} {
		ge := l.b.Emit(OpGe, boolT, []ValueID{cpSlot, l.b.EmitInt(OpConst, charT, bound, "")}, "")
		gi := l.b.Emit(OpCast, idxT, []ValueID{ge}, "")
		w = l.b.Emit(OpAdd, idxT, []ValueID{w, gi}, "")
	}
	l.b.EmitMoveInto(widthSlot, w)

	// Bind the loop variable to a private slot holding this iteration's code
	// point (the body must not be able to move the cursor by writing to it).
	iSlot := l.b.EmitInt(OpConst, charT, 0, varName)
	l.b.EmitMoveInto(iSlot, cpSlot)
	if outP := l.resultOutParamFor(varName); outP != NoVal {
		l.b.EmitMoveInto(outP, iSlot)
		iSlot = outP
	}
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
	l.b.EmitMoveInto(offSlot, l.b.Emit(OpAdd, idxT, []ValueID{offSlot, widthSlot}, ""))
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

// fieldTypeOf returns the MIR TypeID of a named field within the struct type
// of the receiver value. It mirrors codegen's structKeyOf + FieldIndex lookup
// so the lowerer can set a type hint for LHS-inferred builtins assigned to a
// struct field (`.hs-buf = with-cap(65536)`). Returns NoType when the field
// or struct type cannot be resolved.
func (l *lowerer) fieldTypeOf(recvV ValueID, fieldName string) TypeID {
	raw := l.valueRaw(recvV)
	if raw == "" {
		return NoType
	}
	// Strip option prefix: `?T.field` should resolve to T's field.
	raw = strings.TrimPrefix(raw, "?")
	// Resolve the struct key: try exact, then suffix match (e.g. `conn` ->
	// `tls.conn`), mirroring codegen's structKeyOf.
	key := raw
	if _, ok := l.mod.StructFields[key]; !ok {
		found := ""
		for k := range l.mod.StructFields {
			if k == raw || strings.HasSuffix(k, "."+raw) {
				found = k
				break
			}
		}
		if found == "" {
			// Fallback for hashmap specialized structs: the generic_structs
			// pass instantiates hashmap-str-tmpl into hashmap-str-i64 (etc.),
			// but the HIR struct definition may use the template name while the
			// method's self parameter type uses the specialized name. Try
			// matching by prefix: if raw starts with "hashmap-", look for any
			// StructFields key that starts with the same prefix and ends with
			// "-tmpl".
			if strings.HasPrefix(raw, "hashmap-") {
				parts := strings.SplitN(raw, "-", 3)
				prefix := ""
				if len(parts) >= 2 {
					prefix = parts[0] + "-" + parts[1] + "-"
				}
				for k := range l.mod.StructFields {
					if strings.HasPrefix(k, prefix) && strings.HasSuffix(k, "-tmpl") {
						found = k
						break
					}
				}
			}
			if found == "" {
				return NoType
			}
		}
		key = found
	}
	fields, ok := l.mod.StructFields[key]
	if !ok {
		return NoType
	}
	for _, f := range fields {
		if f.Name == fieldName && f.TypeRaw != "" {
			return l.b.Type(f.TypeRaw)
		}
	}
	return NoType
}

// expandTypeAlias resolves a nolang value-type alias (e.g. `fd` -> `i64`,
// recorded by collectValueTypeAliases) to its underlying type name. It is used
// so a method call on a newtype receiver (`fd.to-str`) forms the same callee
// the legacy backend emits (`i64.to-str`). Names that are not registered
// aliases are returned unchanged.
func (l *lowerer) expandTypeAlias(name string) string {
	if l.mod.ValueTypeAliases != nil {
		if t, ok := l.mod.ValueTypeAliases[name]; ok {
			return t
		}
	}
	return name
}

// resolveModuleCallName resolves a MODULE-namespace call (`mod.fn`, e.g.
// `helper.compute-str`, `num.rotate-left`) to the funcNames key the MIR backend
// actually registered for that free function. nolang stores free functions from
// a `# /path` module under their BARE name (the HIR merge keeps `compute-str`,
// not `helper.compute-str`), while the source call uses the module prefix. The
// legacy backend resolves the module-prefixed call to the same bare function via
// its module table; mirroring that here lets cross-module calls lower instead of
// reporting "unknown callee helper.compute-str" (tests/mem-safety/bug13-*.no).
// When the prefixed name IS registered (e.g. `io.writer.write`, a method whose
// name is naturally qualified) it is preferred; otherwise the bare name wins.
func (l *lowerer) resolveModuleCallName(recvName, method string) string {
	qualified := recvName + "." + method
	if _, ok := l.funcNames[qualified]; ok {
		return qualified
	}
	if _, ok := l.funcNames[method]; ok {
		return method
	}
	return l.resolveOverloadedFuncName(qualified)
}

// mangleTypeReplacer mirrors build.sanitizeTypeForName — the surface-AST
// mangler that turns a declared parameter type into an LLVM-identifier-safe
// token. It is duplicated here (rather than imported) because package build
// depends on package mir, so the dependency cannot be inverted.
var mangleTypeReplacer = strings.NewReplacer(
	"[]", "slice.",
	"?", "opt.",
	"ptr ", "ptr.",
	"[", "arr",
	"]", ".",
	" ", "_",
	"|", "-",
)

// resolveOverloadedFuncName maps a callee whose overload-mangling suffix was
// lost back to the single HIR definition that carries it.
//
// Why it is needed: build.mangleOverloads renames every top-level function
// whose bare name collides with another definition to
// `<name>_<sanitized param types>` and rewrites *bare Identifier* call sites to
// match. A QUALIFIED call (`process.cmd(...)`) is a DotExpression, which that
// rewrite pass deliberately leaves alone, so the caller keeps the unmangled
// name while the only definition in the module carries the mangled one.
// `process.cmd` is exactly this case — it has a POSIX and a Win32 variant, both
// named `cmd`, so it is mangled to
// `process.cmd_str_slice.str_str_str_slice.str_i64_bool`.
//
// Without this the callee missed `funcNames` entirely, `resultTypesOfCallee`
// returned nothing, and every one of the four multi-assign targets was bound to
// an i64 zero placeholder (tests/process-run.no then reported the bogus
// "unknown callee i64.trim" for `out.trim()`, because `out` had become an i64).
//
// The substitution is verified, never guessed: a candidate is accepted only
// when its suffix is EXACTLY the sanitized parameter-type list of its own HIR
// definition. A different function whose name merely starts with `callee_`
// (say `x_y` for callee `x`) therefore can never be picked up by accident.
func (l *lowerer) resolveOverloadedFuncName(callee string) string {
	if callee == "" {
		return callee
	}
	if _, ok := l.funcNames[callee]; ok {
		return callee
	}
	prefix := callee + "_"
	match := ""
	found := 0
	for name, id := range l.funcNames {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if !l.mangledSuffixMatches(id, name[len(prefix):]) {
			continue
		}
		found++
		if found > 1 {
			// Genuinely overloaded: the request is ambiguous without argument
			// types, so leave the name untouched and let the caller's normal
			// diagnostic surface instead of silently picking one.
			return callee
		}
		match = name
	}
	if found == 1 {
		return match
	}
	return callee
}

// mangledSuffixMatches reports whether `suffix` is exactly the sanitized
// parameter-type list of the function definition node `id`.
func (l *lowerer) mangledSuffixMatches(id int32, suffix string) bool {
	if id == hir.NoID || suffix == "" {
		return false
	}
	var parts []string
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn == nil || cn.Kind != hir.KParam {
			continue
		}
		raw := ""
		if cn.Type != hir.NoID {
			raw = l.pkg.Type(cn.Type)
		}
		if raw == "" {
			if ty := l.mod.Type(l.typeOfNode(cn)); ty != nil {
				raw = ty.Raw
			}
		}
		if raw == "" {
			return false
		}
		parts = append(parts, mangleTypeReplacer.Replace(raw))
	}
	return len(parts) > 0 && strings.Join(parts, "_") == suffix
}

// lowerArrayElems materializes a fixed array [N]elem from a list of element
// expression node ids, storing each element into its slot. Shared by KArrayLit
// (elems come from "elem" slots) and KSliceLit (elems are direct children).
func (l *lowerer) lowerArrayElems(elems []int32) ValueID {
	if len(elems) == 0 {
		return NoVal
	}
	// Determine the element type. Priority:
	//  1. The typeHint's element type (when assigning to a typed slice like
	//     `data []byte = [0x61, 0x62, 0x63]`, the hint is `[]byte` so the
	//     element should be `byte`, NOT `i64` from the integer literal).
	//  2. The first element's HIR type (authoritative when known, but void for
	//     some call expressions like `a = [a.len()]`).
	//  3. The first element's lowered value type.
	//  4. Fallback to `i64`.
	var firstVal ValueID = NoVal
	elemRaw := ""
	// Check typeHint for a slice/array element type first.
	if l.typeHint != NoType && l.typeHint != l.voidType {
		if ht := l.mod.Type(l.typeHint); ht != nil {
			if (ht.Kind == KindSlice || ht.Kind == KindArray) && ht.Elem != NoType {
				if et := l.mod.Type(ht.Elem); et != nil && et.Raw != "" && et.Raw != "void" {
					elemRaw = et.Raw
				}
			}
		}
	}
	elemT := l.typeOfNode(l.pkg.Node(elems[0]))
	if elemRaw == "" {
		if ty := l.mod.Type(elemT); ty != nil && ty.Raw != "" && ty.Raw != "void" {
			elemRaw = ty.Raw
		}
	}
	if elemRaw == "" {
		firstVal = l.lowerExpr(elems[0])
		if firstVal != NoVal {
			if ty := l.mod.Type(l.valueTypeOf(firstVal)); ty != nil && ty.Raw != "" && ty.Raw != "void" {
				elemRaw = ty.Raw
			}
		}
	}
	if elemRaw == "" || elemRaw == "void" {
		elemRaw = "i64"
	}
	arrRaw := fmt.Sprintf("[%d]%s", len(elems), elemRaw)
	arrT := l.b.Type(arrRaw)
	arrV := l.b.Emit(OpConst, arrT, nil, "")
	for k, e := range elems {
		ev := firstVal
		if k != 0 || ev == NoVal {
			ev = l.lowerExpr(e)
		}
		if ev == NoVal {
			return NoVal
		}
		idxV := l.b.EmitInt(OpConst, l.b.Type("i64"), int64(k), "")
		l.b.EmitVoid(OpIndexStore, []ValueID{arrV, idxV, ev}, "")
	}
	return arrV
}

// lowerEmbedBinding materializes an `#{embed='file'}` top-level binding as a
// module-level `%vec` global over a private constant byte array. The global's
// fixedArrayConstGlobalType returns the Nolang FIXED-ARRAY type (`[N]i64`) that
// a slice-typed top-level slice-literal binding's constant initializer actually
// folds to, or "" when the binding is not such a literal (or its elements do not
// all fold).
//
// The type must be the fixed-array form, not the slice form the binding infers:
// a slice literal folds to `[N x i64] [...]`, so declaring the global as `%vec`
// would disagree with its initializer and the LLVM verifier would reject the
// module. The candidate type is confirmed against the SAME fold that produces the
// initializer, so the two can never drift; a literal whose elements do not fold
// stays on the previous skip path rather than silently registering a
// zero-initialized global carrying the wrong bytes.
func (l *lowerer) fixedArrayConstGlobalType(n *hir.Node, raw string) string {
	if n == nil || !strings.HasPrefix(raw, "[]") {
		return ""
	}
	var init *hir.Node
	for _, c := range l.pkg.Children(n.Id) {
		init = l.pkg.Node(c)
		break
	}
	if init == nil || init.Kind != hir.KSliceLit {
		return ""
	}
	count := 0
	for _, c := range l.pkg.Children(init.Id) {
		if l.pkg.Node(c) == nil {
			return ""
		}
		count++
	}
	if count == 0 {
		return ""
	}
	ft := fmt.Sprintf("[%d]i64", count)
	if l.foldConstText(n, ft) == "" {
		return ""
	}
	return ft
}

// nameUsedInFuncBodies reports whether a top-level binding name is referenced
// from inside any top-level FUNCTION body (as opposed to only from the
// top-level statement sequence, which the synthetic main inlines).
//
// This is the discriminator that decides whether a script's top-level binding
// may be materialized as a local of the synthetic main or must live in module
// storage. A local of main is invisible to every other frame, so a helper
// function reading/writing the name would otherwise resolve it to a void
// `const` placeholder — see the guard in LowerHIR for the concrete failure
// (zip.no's `zw-data []byte`).
func (l *lowerer) nameUsedInFuncBodies(name string) bool {
	if name == "" {
		return false
	}
	for _, id := range l.pkg.Top {
		n := l.pkg.Node(id)
		if n == nil {
			continue
		}
		if n.Kind != hir.KFuncDef && n.Kind != hir.KExtern {
			continue
		}
		if !nodeMatchesPlatform(l.pkg, id) {
			continue
		}
		if l.subtreeHasIdent(n, name) {
			return true
		}
	}
	return false
}

// subtreeHasIdent walks n's First/Next subtree looking for a KIdent interned to
// name. Used by nameUsedInFuncBodies.
func (l *lowerer) subtreeHasIdent(n *hir.Node, name string) bool {
	for c := n.First; c != hir.NoID; {
		cn := l.pkg.Node(c)
		if cn == nil {
			break
		}
		if cn.Kind == hir.KIdent && l.pkg.Str(cn.S) == name {
			return true
		}
		if l.subtreeHasIdent(cn, name) {
			return true
		}
		c = cn.Next
	}
	return false
}

// lowerEmbedBinding resolves a `#{embed=...}` binding to a module global whose
// LLVM initializer is `%vec { N, N, ptrtoint([N x i8]* @.embed.<name> to i64) }`
// and its bytes are stashed on the GlobalDecl so codegen emits the backing
// `@.embed.<name>` constant. cap == len marks the vec owned, so the binding
// behaves like a real slice for reads; its data points into constant memory.
func (l *lowerer) lowerEmbedBinding(name string, data []byte) {
	if v, ok := l.globals[name]; ok && v != NoVal {
		l.locals[name] = v
		return
	}
	gv := l.b.Global(name, l.b.Type("[]byte"))
	n := len(data)
	for i := range l.mod.Globals {
		if l.mod.Globals[i].Init == gv {
			l.mod.Globals[i].ConstText = fmt.Sprintf(
				"%%vec { i64 %d, i64 %d, i64 ptrtoint ([%d x i8]* @.embed.%s to i64) }",
				n, n, n, name)
			l.mod.Globals[i].EmbedBytes = data
			break
		}
	}
	l.globals[name] = gv
	l.locals[name] = gv
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
	case hir.KByteLit:
		// A byte literal (`0xfd`, `b'a'`) carries its value in Val and is
		// emitted as an i64 in the generic array fold (`[N x i64] [...]`), so it
		// must fold like an int literal. Without this case the generic branch of
		// KArrayLit hit an unfoldable element and returned "", leaving the whole
		// array constant unregistered — a top-level `XZ-MAGIC = [0xfd, ...]`
		// read from a std function then resolved to a void `const` and
		// `XZ-MAGIC[i]` failed with "index slot: value ... void". (The
		// `[N]byte` byteArrayRe branch above handles KByteLit itself, so
		// substitution boxes were unaffected.)
		return fmt.Sprintf("i64 %d", n.Val&0xff)
	case hir.KCharLit:
		// The code point lives in S (the interned character TEXT), not in Val:
		// tohir stores a char literal as `{Kind: KCharLit, S: <text>}` and
		// leaves Val at 0 (see parser.tohir's *CharLiteral case). Reading Val
		// here folded EVERY module-level char literal to `i64 0`, so a
		// top-level `p char = 'p'` materialized as `@p = private global i64 0`
		// and printed 0 instead of 112 — while the same literal inside a
		// function printed 112 (lowerCharLit decodes S correctly). Decode it
		// the same way lowerCharLit does.
		cp := int64(0)
		for _, r := range strings.Trim(l.pkg.Str(n.S), "'") {
			cp = int64(r)
			break
		}
		// char is i32 (Unicode scalar value), so the global's declared type and
		// its initializer must agree at that width.
		return fmt.Sprintf("i32 %d", cp)
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
	case hir.KSliceLit:
		// A SLICE literal used as a module constant (`XZ-MAGIC = [0xfd, ...]`).
		// Its children ARE the element expressions — unlike KArrayLit, there are
		// no "elem" slots to unwrap.
		//
		// Folded ONLY when the requested type is a FIXED array. A slice-typed
		// binding must stay unfoldable: registering `@x = global %vec <fixed
		// array constant>` makes the declaration and initializer disagree and
		// the LLVM verifier reject the module, which is exactly why LowerHIR's
		// isUnsafeInlineType guard skips slice literals in scripts. Keeping the
		// `gtype` test here means the guard's behaviour is unchanged for every
		// slice-typed binding, and the fixed-array type supplied by
		// fixedArrayConstGlobalType is the only thing that unlocks the fold.
		if !strings.HasPrefix(gtype, "[") {
			return ""
		}
		var parts []string
		for _, c := range l.pkg.Children(n.Id) {
			ct := l.foldConstText(l.pkg.Node(c), "i64")
			if ct == "" {
				return ""
			}
			parts = append(parts, ct)
		}
		if len(parts) == 0 {
			return ""
		}
		return fmt.Sprintf("[%d x i64] [%s]", len(parts), strings.Join(parts, ", "))
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
	case hir.KCond:
		// ternary: `c ? a : b`
		return l.lowerCond(id)
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
		// A narrow (byte/u8) element READ is promoted to i64, matching the
		// legacy backend, which materializes a byte loaded out of a
		// byte-addressed buffer as a full i64 register. Keeping the read at i8
		// made every wider expression built on it truncate: `padded[i] << 24`
		// in std/crypto/sha1 yielded 0 (0x61 << 24 truncated to one byte)
		// instead of 0x61000000, producing a wrong-but-exit-0 SHA-1 digest.
		// Only the READ is promoted — OpIndexStore keeps the true element type
		// (see codegen.elemTypeOfReceiver) so a write still stores one byte.
		if elemT != l.voidType {
			if et := l.mod.Type(elemT); et != nil && (et.Raw == "byte" || et.Raw == "u8" || et.Raw == "i8") {
				if i64T := l.b.Type("i64"); i64T != l.voidType {
					elemT = i64T
				}
			}
		}
		// Safe index: when the binding site is declared as an option (?elem)
		// the read must be bounds-checked and yield nil when out of range.
		// This is what the parser's safe-index desugar emits for
		// `x ?= v[i]` and `#{index-out=DEF} x = v[i]`:
		//
		//	__idx_out_N ?i64 = v[i]
		//	__idx_out_N: { ok -> { x = it }  ok -> { x = DEF } }
		//
		// Without the check OpIndex read raw out-of-bounds memory, wrapped the
		// garbage into ok(...) and the `ok` arm then assigned it — a silent
		// wrong answer at best, and for owned element types (str) a wild
		// buffer pointer that SIGSEGVs on the first use
		// (tests/safe-index.no: `get-default OOB`).
		if l.typeHint != NoType && l.typeHint != l.voidType {
			if ht := l.mod.Type(l.typeHint); ht != nil && ht.Kind == KindOption {
				return l.lowerSafeIndex(arrV, idxV, elemT, l.typeHint)
			}
		}
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
		// Tagged-enum arm destructuring, checked BEFORE any local binding.
		// A variant pattern's field name (`ok(v) -> print(v)`) belongs to the
		// arm, and an earlier arm that bound the same name must not shadow it:
		// `b-res` follows `a-res` in the same function and both arms call their
		// payload `v`, so a locals-first lookup made the second arm print the
		// first arm's value.
		if fv, ok := l.enumArmFieldValue(name); ok {
			return fv
		}
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
			// A variable/parameter sharing one of these names must resolve as a
			// normal identifier, NEVER as a variant keyword. Check locals/globals
			// FIRST so we never short-circuit a real variable (this is exactly
			// what caused the 19th-round regression on test-ok-shadow /
			// test-bare-match / bug13, which all declare `ok bool`).
			if v, ok := l.locals[name]; ok {
				return v
			}
			if _, isGlobal := l.globals[name]; isGlobal {
				return l.lowerGlobalRef(name)
			}
			// A tagged enum may declare a variant named `ok` / `err` / `nil`
			// (`b-res { ok(v str), fail }`). When the expected type at this
			// site is such an enum the name is that enum's constructor, NOT
			// the option variant keyword: the keyword path below would emit a
			// plain const of the enum type, leaving the arm to compare against
			// discriminant 0 regardless of the variant's real tag
			// (tests/tagged-enum.no prints `0` instead of `hi`).
			if ei, vi := l.enumVariantOf(l.enumPrefer(), name); ei != nil && vi != nil {
				return l.lowerEnumUnit(ei, vi)
			}
			// Genuine bare variant keyword. Determine the option type from the
			// surrounding context (the matched subject's type).
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
			// Non-option context: `match 42: { err -> ... }` compares a plain
			// integer against the `err` discriminant, which is meaningless. The
			// old path fell through to the unresolved-identifier emission (an
			// `undef` constant) and `subject == undef` is LLVM poison that opt
			// folds into a runtime trace/BPT trap (a red-line crash). Emit a
			// concrete zero of the subject's (inferred) type so the comparison
			// is well-defined (false → no match → fall through) instead of
			// poisoning. Legacy's output for such a program (it prints the `err`
			// arm) is itself a quirk and is deliberately NOT replicated.
			if ht := l.typeHint; ht != NoType && ht != l.voidType {
				if tt := l.mod.Type(ht); tt != nil && tt.Kind != KindOption {
					return l.b.EmitInt(OpConst, ht, 0, "")
				}
			}
			return l.b.EmitInt(OpConst, l.b.Type("i64"), 0, "")
		}
		if v, ok := l.locals[name]; ok {
			return v
		}
		// A unit variant of a tagged enum used as a VALUE (`c color = green`).
		// It is a constructor with no payload; the enum is taken from the type
		// expected at this site, which is what distinguishes `color.green`
		// from an identically named variant of another enum. Checked after the
		// locals lookup so a local binding still shadows a variant name.
		if ei, vi := l.enumVariantOf(l.enumPrefer(), name); ei != nil && vi != nil {
			return l.lowerEnumUnit(ei, vi)
		}
		// A top-level module binding (SBOX, TLS-FINISHED-SIZE, perm-600, ...)
		// lives outside every function; resolve it as a module global. This is
		// the last resort before declaring the identifier unresolved, so it
		// must come AFTER the locals lookup above (a local shadow wins).
		if _, isGlobal := l.globals[name]; isGlobal {
			return l.lowerGlobalRef(name)
		}
		// A function name used as a value (not called): `run-suite(my-setup,
		// my-teardown)` passes the function's address as a callable argument.
		// resolveCallee already handles the CALL site (`setup()` inside the
		// callee body), but the ARGUMENT site (where the function name is
		// passed BY VALUE) falls through to "unresolved identifier" because the
		// name is neither a local nor a global. Emit an OpFuncRef that yields a
		// KindFunc-typed value whose Name carries the function name; codegen's
		// loadVal resolves it to `@funcname` (tests/named-fn-type.no).
		if hirID, ok := l.funcNames[name]; ok {
			fnStr := l.funcSigRaw(hirID)
			fnTyp := l.b.Type(fnStr)
			v := l.b.Emit(OpFuncRef, fnTyp, nil, name)
			// Set the Value's Name so loadVal can resolve it to @funcname.
			l.mod.Values[v].Name = name
			// Enqueue the function for lowering so its body is emitted.
			l.enqueueCallee(name)
			return v
		}
		// A unit variant of a PLAIN (payload-less) enum used as a VALUE:
		// `c color = green` in a let, and `red` in a match arm. The parser
		// classifies `color { red, green, blue }` as a plain EnumDefinition
		// (no variant carries a payload), whose values are plain i64
		// discriminants — the same representation `Color.red`-style qualified
		// references already use.
		//
		// The declared type at this site is usually NOT the enum name: a plain
		// enum value IS its i64 discriminant in MIR, so `c color = green`
		// exposes `i64` as the hint and a match arm's subject carries no hint
		// at all. Fall back to a globally unique variant name in that case,
		// which is why this sits after the local/global lookups — a real
		// binding must always win.
		if val, ok := l.plainEnumVariantValue(name); ok {
			return l.b.EmitInt(OpConst, l.b.Type("i64"), val, "")
		}
		// Tagged-enum arm destructuring. A variant pattern's field names
		// (`s: { rect(w, h) -> ... }`) are NOT local bindings — the parser only
		// binds the subject to `it`, so `w` / `h` reached this point as
		// unresolved identifiers and lowered to void constants, making
		// `a = w * h` a void multiply. Project them onto the matched subject's
		// payload slot instead.
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
		// Nolang string interpolation (`print('x={expr}')`) is substituted ONLY
		// at a print-family call site; see lowerNamedFormat. Everything else
		// (`s = 'x={n}'`, a struct field, an argument to a user function) emits
		// the braces literally — verified against the legacy backend, whose
		// named-format interception is reachable solely from callFmt.
		//
		// The old test here was `Contains("{") && Contains("}")`, which also
		// flagged ordinary brace-bearing literals (JSON, code templates, `{}`
		// in embedded sources) and forced a whole-module fallback for them.
		// Now the diagnostic is raised only when the literal really is a
		// format string AND it appears as a print-family argument that the
		// interception failed to lower — i.e. exactly the case where emitting
		// the raw text would be silently wrong.
		if strings.Contains(s, "{") && strings.Contains(s, "}") {
			if segs, err := parser.ParseFormatString(s); err == nil {
				hasField := false
				for _, sg := range segs {
					if sg.Field != nil {
						hasField = true
						break
					}
				}
				if hasField && l.inPrintArgs > 0 {
					l.unsupported(l.curFuncName(), "interp", "string interpolation not lowered: "+interpPreview(s))
				}
			}
		}
		return l.b.EmitStr(OpConst, l.b.Type("str"), s, "")
	case hir.KRegexLit:
		return l.lowerRegexLit(n)
	case hir.KCharLit:
		return l.lowerCharLit(n)
	case hir.KNilLit:
		return l.lowerNilLit(n)
	case hir.KStructLit:
		return l.lowerStructLit(n)
	case hir.KMapLit:
		return l.lowerMapLit(n)
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
		// Unsigned division/remainder: nolang's u64/u32/u16/u8/byte family uses
		// unsigned semantics, but MIR flattens every integer to the LLVM i64
		// width and registers u64 locals as i64, so the signedness is NOT
		// recoverable from the lowered value. Route through OpUDiv/OpUMod so
		// codegen emits `udiv`/`urem`. The signedness is read from the
		// DECLARED raw type of the operand variables (recorded when their KLet
		// was lowered), since MIR flattens u64 to i64 everywhere else. Without
		// this, i64.MIN's 2^63 magnitude (which carries the i64.MIN bit
		// pattern) signed-divides to garbage in i64-to-str / u64-to-str.
		if !isCmp && (op == OpDiv || op == OpMod) {
			unsigned := false
			for _, cid := range lr[:i] {
				cn := l.pkg.Node(cid)
				if cn != nil && cn.Kind == hir.KIdent {
					if isUnsignedRaw(l.localRaw[l.pkg.Str(cn.S)]) {
						unsigned = true
						break
					}
				}
			}
			if unsigned {
				if op == OpDiv {
					op = OpUDiv
				} else {
					op = OpUMod
				}
			}
		}
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
					// Bare user-enum variant (`m: { read -> ... }`): the variant
					// carries no receiver and no type, so resolve it from the
					// sibling's enum type. Must be tried BEFORE lowering the bare
					// operand, which would otherwise emit an unresolved `void` const.
					if ev, ok := l.lowerBareEnumVariant(ln, rn); ok {
						rv = l.lowerExpr(lr[1])
						lv = ev
						break
					}
					if ev, ok := l.lowerBareEnumVariant(rn, ln); ok {
						lv = l.lowerExpr(lr[0])
						rv = ev
						break
					}
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
		// ARITHMETIC on an option-typed operand must use the PAYLOAD. `?=` is
		// desugared by the parser into a plain assignment from the option
		// value, so `size ?= fstat-size(.fd)` leaves `size` typed `?T` and
		// `size - total` would otherwise hand the whole `%option` struct to
		// `sub` (EmitLLVM: "cannot coerce arg from %option to i64",
		// tests/open-read.no). Moving into the element type lowers to an
		// `extractvalue` of the payload field. Comparisons are left alone:
		// `x == err` / `x == nil` are TAG comparisons handled above.
		unwrapped := false
		if !isCmp {
			nlv := l.unwrapOptionOperand(lv)
			nrv := l.unwrapOptionOperand(rv)
			unwrapped = nlv != lv || nrv != rv
			lv, rv = nlv, nrv
		}
		// Arithmetic on a single-character string literal (`"A" + 1`) is BYTE
		// arithmetic, not string concatenation: legacy's isStringExpr rejects
		// one-rune string literals, so `"A"` folds to 65 and the result is 66.
		// Without this the literal stayed a %str-long, the infix fell through to
		// `add i64 <str>, 1`, and LLVM verification rejected the module
		// (tests/str-ops.no). Comparisons are left alone — `s == "A"` is a
		// string compare, handled by the OpStrEq path below.
		// Determine str-ness of each operand BEFORE any byte folding below.
		rawOf := func(v ValueID) string {
			t := l.valueTypeOf(v)
			if t == l.voidType || t == NoType {
				return ""
			}
			if ty := l.mod.Type(t); ty != nil {
				return ty.Raw
			}
			return ""
		}
		lStr := rawOf(lv) == "str"
		rStr := rawOf(rv) == "str"
		// Nolang quoting: `'...'` is a StringLiteral, `"..."` is a CharLiteral
		// (already a byte-valued i64 here). A ONE-character StringLiteral paired
		// with a non-string operand is BYTE arithmetic, not concatenation:
		// `'A' + 1` is 66 (tests/str-ops.no). Legacy's isStringExpr gates
		// this on the sibling NOT being a string, so `'a' + 'b'` stays "ab".
		// It applies to `+`/`-` only — for `*` a string literal is always a
		// string (`'x' * 5` is repeat -> "xxxxx"), never a byte.
		srcOp := l.pkg.Str(n.S)
		if !isCmp && (srcOp == "+" || srcOp == "-") {
			if sn := l.pkg.Node(lr[0]); sn != nil && sn.Kind == hir.KStrLit && !rStr {
				if b, ok := singleCharStrByte(l.pkg.Str(sn.S)); ok {
					lv = l.b.EmitInt(OpConst, l.b.Type("i64"), b, "")
					lStr = false
				}
			}
			if sn := l.pkg.Node(lr[1]); sn != nil && sn.Kind == hir.KStrLit && !lStr {
				if b, ok := singleCharStrByte(l.pkg.Str(sn.S)); ok {
					rv = l.b.EmitInt(OpConst, l.b.Type("i64"), b, "")
					rStr = false
				}
			}
		}
		// ORDERING comparison against a ONE-character string literal is a CHAR
		// comparison (`'e' >= 'a'`, `'e' >= "a"`, `['a'..'m')`): fold the literal
		// to its code point so both sides are integers. nolang has no
		// lexicographic string comparison — without this the operands stayed
		// %str-long, fell into the @str_eq path in codegen, and EVERY ordering
		// comparison returned false (only `==` was ever meaningful).
		// The checker (checker.ValidateStrOrdering) rejects the multi-character
		// case at compile time, so a %str-long operand surviving here is a bug
		// (codegen fails loudly rather than answering `false`).
		// Equality is deliberately excluded: `s == 'A'` / `'ab' == 'ab'` are real
		// string comparisons and must stay in the @str_eq path.
		if isCmp && (srcOp == "<" || srcOp == "<=" || srcOp == ">" || srcOp == ">=") {
			if sn := l.pkg.Node(lr[0]); sn != nil && sn.Kind == hir.KStrLit {
				if b, ok := singleCharStrByte(l.pkg.Str(sn.S)); ok {
					lv = l.b.EmitInt(OpConst, l.b.Type("i64"), b, "")
					lStr = false
				}
			}
			if sn := l.pkg.Node(lr[1]); sn != nil && sn.Kind == hir.KStrLit {
				if b, ok := singleCharStrByte(l.pkg.Str(sn.S)); ok {
					rv = l.b.EmitInt(OpConst, l.b.Type("i64"), b, "")
					rStr = false
				}
			}
		}
		// `str <op> char` (`hi - "B"`) is concatenation with the character
		// rendered as a ONE-character string, not as its decimal code — legacy
		// uses byteToSingleCharStr here. Materialize the character as a str
		// constant so emitArith sees two %str-long operands and never falls
		// into emitIntToStr (which would print "hello66" instead of "helloB").
		if !isCmp && (srcOp == "+" || srcOp == "-" || srcOp == "*") {
			if lStr && !rStr {
				if sn := l.pkg.Node(lr[1]); sn != nil && sn.Kind == hir.KCharLit {
					if cp, ok := charLitCode(l.pkg.Str(sn.S)); ok {
						rv = l.b.EmitStr(OpConst, l.b.Type("str"), string(rune(cp)), "")
						rStr = true
					}
				}
			}
			if rStr && !lStr {
				if sn := l.pkg.Node(lr[0]); sn != nil && sn.Kind == hir.KCharLit {
					if cp, ok := charLitCode(l.pkg.Str(sn.S)); ok {
						lv = l.b.EmitStr(OpConst, l.b.Type("str"), string(rune(cp)), "")
						lStr = true
					}
				}
			}
		}
		resTyp := l.typeOfNode(n)
		if isCmp {
			resTyp = l.b.Type("bool")
		} else if unwrapped {
			// Both operands were option payloads (see unwrapOptionOperand):
			// the node's declared type is still the `?T` the operand carried,
			// but the arithmetic result is a plain `T`. Keeping `?T` made
			// `size - total` an option, which then failed to coerce to the i64
			// argument of `read(...)` (tests/open-read.no).
			if lt := l.valueTypeOf(lv); lt != l.voidType && lt != NoType {
				resTyp = lt
			}
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
		// String concatenation (`a - b` / `a + b` with a str operand) yields a
		// str, never void or a scalar. Without this the infix result value is
		// typed i64 and emitArith emits an illegal `sub i64 %str-long, ...`
		// (tests/str-ops.no: `hi - "B"`).
		//
		// A single str operand is enough: mixed `str + int` promotes the int to
		// a decimal string (emitArith does exactly that), and `str * int` is
		// string repeat, not multiplication — both need the str result type.
		// Only `+`, `-` (concat) and `*` (repeat) are string operators; for
		// every other op a str operand is a type error and we keep the old
		// behaviour rather than silently turning it into a concat.
		if !isCmp && (srcOp == "+" || srcOp == "-" || srcOp == "*") && (lStr || rStr) {
			resTyp = l.b.Type("str")
		}
		// A `+`/`-` where NEITHER operand is a string after the folding above is
		// byte/int arithmetic even when the checker inferred `str` from the
		// literal (`'A' + 1` -> inferred str, but legacy yields the int 66).
		// Without this the result stays a str and emitArith concatenates the
		// decimal forms — "651" instead of 66.
		if !isCmp && (srcOp == "+" || srcOp == "-") && !lStr && !rStr {
			if ty := l.mod.Type(resTyp); ty != nil && ty.Raw == "str" {
				resTyp = l.b.Type("i64")
			}
		}
		// String equality/inequality cannot be a direct `icmp` (str is a struct),
		// so route it through the runtime @str_eq helper via OpStrEq. `!=` negates
		// the equality result with a boolean icmp.
		if (op == OpEq || op == OpNe) && l.valueRaw(lv) == "str" {
			eq := l.b.Emit(OpStrEq, l.b.Type("bool"), []ValueID{lv, rv}, "")
			if op == OpEq {
				return eq
			}
			// `!=` is the negation of ==. OpNe(eq, false) would be
			// `eq != false` which is just `eq` — not a negation.
			// Use OpNot (xor with 1) to correctly invert the result.
			return l.b.Emit(OpNot, l.b.Type("bool"), []ValueID{eq}, "")
		}
		// An arithmetic/bitwise op whose DECLARED result type is a narrow int
		// but whose operands lowered to a WIDER value must keep the wider type.
		// `padded[i] << 24` (a byte element, promoted to i64 by the KIndex read
		// above) would otherwise truncate straight back to one byte — 0x61 << 24
		// becomes 0, and `(a<<24)|(b<<16)|c` collapses to just `c` — which
		// silently corrupted std/crypto/sha1's message schedule and digest.
		// Legacy behaves the same way: it computes in the operand's register
		// width, so a byte VARIABLE (`b byte = 255; b << 4` -> 240) still wraps
		// at 8 bits while a byte read out of a buffer yields the full value.
		// Only widen when an operand really IS wider; never narrow a result.
		if !isCmp && isArithOrBitwiseOp(op) {
			if rt := l.mod.Type(resTyp); rt != nil && (rt.Raw == "byte" || rt.Raw == "u8" || rt.Raw == "i8") {
				for _, side := range [...]ValueID{lv, rv} {
					if side == NoVal {
						continue
					}
					st := l.valueTypeOf(side)
					if st == l.voidType || st == NoType {
						continue
					}
					if stt := l.mod.Type(st); stt != nil && stt.Raw == "i64" {
						resTyp = st
						break
					}
				}
			}
		}
		return l.b.Emit(op, resTyp, []ValueID{lv, rv}, "")
	case hir.KCall:
		return l.lowerCall(n)
	case hir.KRun:
		return l.lowerAsyncRun(n)
	case hir.KAwait:
		return l.lowerAsyncAwait(n)
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
	case hir.KIf:
		// A `match`/`if` used as an expression value (`r = subject: { arms }`)
		// is lowered by the enclosing `let`/`assign` via the exprSink capture
		// mechanism: lowerIf stores each arm's result into l.exprSink and the
		// binding aliases that slot. When capture is active, run the if as a
		// statement and return the shared slot so nested arms (match guards)
		// converge correctly. When NOT capturing (ordinary use of `if` in
		// expression position) we keep the historical "unsupported" behaviour.
		if l.exprCapture && l.exprSink != NoVal {
			l.lowerIf(n)
			return l.exprSink
		}
		l.unsupported(l.curFuncName(), "expr-ctrl", "control flow used as expression value")
		return NoVal
	case hir.KFor:
		l.unsupported(l.curFuncName(), "expr-ctrl", "control flow used as expression value")
		return NoVal
	default:
		l.unsupported(l.curFuncName(), hir.KindNames[n.Kind], "expression kind not lowered yet")
		return NoVal
	}
}

// lowerRegexLit lowers a `/pattern/flags` regex literal.
//
// The legacy backend desugars this at CODEGEN time, rewriting the AST node into
// a `regexp-compile('pattern')` call (build/llvm/expr.go). MIR has no codegen-
// stage AST — lowering is the last point where the shape exists — so the
// desugaring has to happen here. Without it the literal produced no value at
// all: the enclosing `let re2 = /hello/gi` never bound a local, and every later
// use of `re2` surfaced as "unresolved identifier" / "unresolved format field"
// (which reads like an interpolation bug but is really a missing literal).
//
// Flags are carried on the node (n.S2) but the regexp engine does not consume
// them yet — legacy drops them the same way, so we stay byte-compatible.
func (l *lowerer) lowerRegexLit(n *hir.Node) ValueID {
	pat := l.pkg.Str(n.S)
	reT := l.b.Type("regexp")
	// std functions register under their bare name; fall back to the
	// module-qualified spelling in case only that one was loaded.
	callee := "regexp-compile"
	if _, ok := l.funcNames[callee]; !ok {
		if _, ok2 := l.funcNames["regexp."+callee]; ok2 {
			callee = "regexp." + callee
		}
	}
	l.enqueueCallee(callee)
	argV := l.b.EmitStr(OpConst, l.b.Type("str"), pat, "")
	if dsts := l.b.EmitCallMulti([]TypeID{reT}, []ValueID{argV}, callee); len(dsts) > 0 {
		return dsts[0]
	}
	l.unsupported(l.curFuncName(), "regex", "regexp-compile produced no result")
	return l.b.Emit(OpConst, reT, nil, "")
}

// lowerCharLit lowers a character literal `'a'` to a `char` (i32) constant
// holding its Unicode code point. Nolang char is a single Unicode scalar value
// stored as i32 (matching KindChar -> i32 in codegen); a code point needs only
// 21 bits, so i32 is exact.
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
		if tt := l.mod.Type(ht); tt != nil {
			if tt.Kind == KindOption {
				t = ht
			} else if tt.Kind == KindStr {
				// nolang `nil` compared against / assigned to a plain `str`
				// means the null/empty string. Emit an empty %str-long so the
				// later @str_eq (or move/store) is type-correct AND semantically
				// right (a null str equals the empty string). Without this,
				// `r == nil` where r : str lowers `nil` to a bare i64 0 and
				// emitStrEq emits an illegal `@str_eq(%str-long, i64)`
				// (tests/mem-safety/ffi-str-return.no: `r == nil`).
				return l.b.EmitStr(OpConst, l.b.Type("str"), "", "")
			}
		}
	}
	return l.b.EmitInt(OpConst, t, 0, "")
}

// canonStructRaw resolves a possibly-unqualified struct type name to the exact
// key used by mod.StructFields. std structs are module-qualified there (e.g.
// `fs.file-opts`), so a bare `file-opts` must be canonicalized to the qualified
// key before it is used as a MIR type: codegen names the LLVM struct type from
// the StructFields key (`%` + sanitize(raw)), so a mismatch would make the
// struct's alloca reference a non-existent LLVM type.
func (l *lowerer) canonStructRaw(raw string) string {
	if raw == "" {
		return raw
	}
	if _, ok := l.mod.StructFields[raw]; ok {
		return raw
	}
	for k := range l.mod.StructFields {
		if strings.HasSuffix(k, "."+raw) {
			return k
		}
	}
	return raw
}

// paramRawTypesOfCallee returns the declared parameter type strings of a nolang
// function in declaration order. For a method, the first entry is the receiver
// (self) type — taken from the first KResult — so that lowerCallArgs' argOffset
// alignment stays correct (argv[0] is the receiver, argv[1+] are real args,
// paramRaws[0] is the receiver type, paramRaws[1+] are real param types).
// Returns nil when the callee has no HIR definition (builtins).
func (l *lowerer) paramRawTypesOfCallee(callee string) []string {
	if callee == "" {
		return nil
	}
	id, ok := l.funcNames[callee]
	if !ok {
		return nil
	}
	fnNode := l.pkg.Node(id)
	isMethod := fnNode != nil && fnNode.Has(hir.FlagMethod)
	var out []string
	// For a method, prepend self's type (from the first KResult) so that
	// paramRaws aligns with argv (which has the receiver at index 0).
	if isMethod {
		for _, c := range l.pkg.Children(id) {
			cn := l.pkg.Node(c)
			if cn == nil || cn.Kind != hir.KResult {
				continue
			}
			raw := ""
			if t := l.mod.Type(l.typeOfNode(cn)); t != nil {
				raw = t.Raw
			}
			out = append(out, raw)
			break // only the first KResult (self)
		}
	}
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn == nil || cn.Kind != hir.KParam {
			continue
		}
		raw := ""
		if t := l.mod.Type(l.typeOfNode(cn)); t != nil {
			raw = t.Raw
		}
		out = append(out, raw)
	}
	return out
}

// noteAnonymousStructLit records the struct type an anonymous `{...}` literal
// argument should take, derived from the callee's parameter type at that
// position. Only bare literals (empty n.S) and non-option parameter types are
// seeded: an option-typed parameter (`?T`) would additionally need wrapping,
// which is out of scope here and left to the existing promotion path.
func (l *lowerer) noteAnonymousStructLit(id int32, typeRaw string) {
	if id == hir.NoID || typeRaw == "" || strings.HasPrefix(typeRaw, "?") {
		return
	}
	n := l.pkg.Node(id)
	if n == nil || n.Kind != hir.KStructLit || l.pkg.Str(n.S) != "" {
		return
	}
	raw := l.canonStructRaw(typeRaw)
	if raw == "" {
		return
	}
	if _, ok := l.mod.StructFields[raw]; !ok {
		// Not a known struct (e.g. a builtin like `txt`): leave it alone rather
		// than inventing a struct type codegen cannot lay out.
		return
	}
	if l.structLitTypes == nil {
		l.structLitTypes = map[int32]string{}
	}
	l.structLitTypes[id] = raw
}

// lowerStructLit lowers `T{ f: v, ... }` into a struct value: allocate the
// struct, then store each field initializer into it via OpSetField. The result
// value is the (addressable) struct slot, so later field reads GEP into it.
func (l *lowerer) lowerStructLit(n *hir.Node) ValueID {
	raw := l.pkg.Str(n.S)
	if raw == "" {
		// Anonymous `{...}` literal: the type is inferred from context. Use the
		// parameter type seeded by lowerCallArgs, canonicalized to the
		// StructFields key so codegen's LLVM struct type matches.
		if ov, ok := l.structLitTypes[n.Id]; ok {
			raw = l.canonStructRaw(ov)
		}
	}
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
		// The field initializer is consumed by the constructor: its heap
		// transfers into the struct field. Mark it so the drop pass does not
		// free the initializer's temporary separately (which would free a
		// buffer the struct — and any struct moved out of the frame — still
		// points to; the SIGSEGV / NUL-name behind struct-field-leak and
		// struct-move-is-moved).
		l.mod.Insts[sid].MovesArg = true
	}
	return res
}

// lowerMapLit lowers a map literal `{ k1:v1, k2:v2, ... }` into a series of
// calls: first `hashmap-<K>-<V>.init(self)`, then `hashmap-<K>-<V>.put(self,
// key, val, &is_new)` for each pair. This mirrors the legacy backend's
// generateLet MapType path (stmt.go ~9199). The map type is carried by the
// KMapLit node's Type field (e.g. [str]i64); it is resolved to the specialized
// hashmap struct name (hashmap-str-i64) for the callee.
func (l *lowerer) lowerMapLit(n *hir.Node) ValueID {
	mapRaw := l.pkg.Type(n.Type)
	if mapRaw == "" {
		mapRaw = l.pkg.InferredType(n.Id)
	}
	k, v, ok := parseMapTypes(mapRaw)
	if !ok {
		l.unsupported(l.curFuncName(), "map-lit", "cannot parse map type "+mapRaw)
		return NoVal
	}
	vSan := strings.ReplaceAll(v, "[]", "slice_")
	hmName := "hashmap-" + k + "-" + vSan
	hmT := l.b.Type(hmName)

	// Allocate the hashmap struct (OpStructLit zeroes it).
	res := l.b.Emit(OpStructLit, hmT, nil, hmName)

	// Call hashmap.init(self) — enqueue so the function is lowered.
	initCallee := hmName + ".init"
	l.enqueueCallee(initCallee)
	if _, ok := l.funcNames[initCallee]; ok {
		l.b.EmitVoid(OpCall, []ValueID{res}, initCallee)
	}

	// For each key:value pair, call hashmap.put(self, key, val, &is_new).
	// The put method has a bool result param (is-new); allocate a local for it.
	putCallee := hmName + ".put"
	l.enqueueCallee(putCallee)
	if _, ok := l.funcNames[putCallee]; !ok {
		return res
	}
	for _, pairID := range l.pkg.Children(n.Id) {
		pn := l.pkg.Node(pairID)
		if pn == nil || pn.Kind != hir.KMapPair {
			continue
		}
		pairChildren := l.pkg.Children(pairID)
		if len(pairChildren) < 2 {
			continue
		}
		keyV := l.lowerExpr(pairChildren[0])
		valV := l.lowerExpr(pairChildren[1])
		if keyV == NoVal || valV == NoVal {
			continue
		}
		// put(self, key, val, &is_new) — the bool result param is an out-param.
		isNewT := l.b.Type("bool")
		isNew := l.b.Emit(OpConst, isNewT, nil, "")
		l.b.EmitVoid(OpCall, []ValueID{res, keyV, valV, isNew}, putCallee)
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
	// Static values of the bounds, used to prove the range is FORWARD (see
	// sliceForward below).
	var loLit, hiLit int64
	var haveLo, haveHi bool
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
					if bn := l.pkg.Node(cn.First); bn != nil && bn.Kind == hir.KIntLit && leftInc {
						loLit, haveLo = bn.Val, true
					}
				case "end":
					hiV = l.lowerExpr(cn.First)
					if bn := l.pkg.Node(cn.First); bn != nil && bn.Kind == hir.KIntLit {
						hiLit, haveHi = bn.Val, true
					}
				}
			}
		}
	}
	// sliceForward reports whether the sub-range is statically known to run
	// FORWARD (lo <= hi'). Only then can it be represented as a contiguous
	// borrow: a REVERSED range (a[3..1]) yields a different element order and
	// has no aliasing representation, so it keeps the copying path.
	//
	// hi' is hi+1 when the upper bound is inclusive (matches emitSliceOp, which
	// adds 1 for rightInc before taking abs(hi - lo)).
	sliceForward := func() bool {
		switch {
		case !haveLo && !haveHi:
			// `a[..]`: lo = 0, hi = len — always forward.
			return true
		case !haveLo && haveHi:
			// `a[..n]`: lo = 0; forward iff hi' >= 0.
			hiEff := hiLit
			if rightInc {
				hiEff++
			}
			return hiEff >= 0
		case haveLo && haveHi:
			hiEff := hiLit
			if rightInc {
				hiEff++
			}
			return hiEff >= loLit
		default:
			// `a[n..]`: hi is the container length, unknown at compile time.
			return false
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
		// The exclusive upper bound is computed in codegen (emitSliceOp) so
		// that reverse slices (start > end) are handled uniformly: codegen
		// applies +1 for rightInc in BOTH forward and reverse directions, then
		// takes abs(hi - lo) for the length.  Recording rightInc on the inst
		// (Int field) lets codegen know to add 1.
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
	sliceDst := l.b.Emit(OpSliceOp, resTyp, args, "")
	// Record rightInc on the instruction (Int=1) so codegen knows the upper
	// bound is inclusive and must add 1 to hi before computing the length.
	// This is needed for BOTH forward and reverse slices; codegen takes
	// abs(hi - lo) to handle reverse (start > end) correctly.
	if rightInc {
		l.mod.Insts[len(l.mod.Insts)-1].Int = SliceFlagRightInc
	}
	// Mark the slice as a VIEW candidate: it aliases the receiver's buffer
	// (cap = 0 ⇒ never freed) instead of copying it. Only a provably forward
	// range qualifies. The safety pass (demoteUnsafeSliceViews) turns the flag
	// back off for any result that outlives its source.
	if sliceForward() {
		l.mod.Insts[len(l.mod.Insts)-1].Int |= SliceFlagView
	}
	return sliceDst
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

	v, _ := l.lowerFieldReadOn(recvV, fieldName)
	return v
}

// lowerFieldReadOn reads `fieldName` off the value recvV.
//
// It is the shared tail of lowerDotRead (an explicit `recv.field` in HIR) and
// lookupFormatValue's `ident.field` format-field pattern. The latter has no HIR
// node of its own — a format string carries SOURCE TEXT (`{img.width}`) and MIR
// only sees the base binding — so the receiver arrives as a bare ValueID.
//
// Reports failure with (NoVal,false); the caller decides how to diagnose (an
// explicit `recv.field` records an "unsupported" lower gap, a format field
// refuses the field so the print-family call can fall back).
func (l *lowerer) lowerFieldReadOn(recvV ValueID, fieldName string) (ValueID, bool) {
	recvRaw := ""
	recvT := l.valueTypeOf(recvV)
	if t := l.mod.Type(recvT); t != nil {
		recvRaw = t.Raw
	}
	if recvRaw == "" {
		l.unsupported(l.curFuncName(), "dot", "field "+fieldName+": cannot determine receiver type")
		return NoVal, false
	}
	// A `?T.field` read peels the option to reach the inner struct's layout.
	// The method-call path already strips the leading '?' (see resolveCallee:
	// `v.to-str()` on ?i64 -> "i64.to-str"), but the FIELD path looked up
	// StructFields["?conn"], which is never registered -> "no struct layout for
	// ?conn" -> the binding never materialized, so a later `port.to-str()` saw
	// `port` as an unbound name and resolved it as a MODULE namespace
	// ("unknown callee port.to-str", test-opt-struct-field). emitGetField
	// already emits the option peel, so only the lookup key needs fixing.
	recvRaw = strings.TrimPrefix(recvRaw, "?")
	// A VIEW (`&T`) read reaches transparently through the borrow: `v.x` on a
	// `&point` resolves against `point`'s field layout, and codegen's
	// emitGetField loads the pointer before the GEP. `viewOf` remembers the
	// borrowed type so container properties (`.len` / `.cap`) can be dispatched
	// on the POINTEE's kind rather than the pointer's.
	viewOf := ""
	if strings.HasPrefix(recvRaw, "&") {
		viewOf = strings.TrimPrefix(recvRaw, "&")
		recvRaw = viewOf
	}

	// Container builtin properties: `.len` / `.cap` are properties of the
	// container itself, not struct fields. There is one such layout per
	// element type (`[]byte`, `[]i64`, `[3]i64`, ...), so a name-keyed
	// StructFields lookup can never resolve them — that mismatch alone
	// accounted for every "no struct layout for []byte (field len)" gap on the
	// corpus. Dispatch on the type KIND and emit a dedicated op instead.
	if ty := l.mod.Type(recvT); ty != nil {
		effTy := ty
		if viewOf != "" && ty.Elem != NoType {
			if et := l.mod.Type(ty.Elem); et != nil {
				effTy = et
			}
		}
		if fieldName == "len" || fieldName == "cap" {
			switch {
			case effTy.Kind == KindStr || effTy.Kind == KindSlice || effTy.Kind == KindArray || effTy.Kind == KindMap:
				op := OpLen
				if fieldName == "cap" {
					op = OpCap
				}
				return l.b.Emit(op, l.b.Type("i64"), []ValueID{recvV}, ""), true
			case recvRaw == "txt" && fieldName == "len":
				// txt.len reads the i8 len byte and zero-extends to i64 (legacy
				// semantics); cap is not meaningful for a fixed buffer.
				return l.b.Emit(OpLen, l.b.Type("i64"), []ValueID{recvV}, ""), true
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
		return NoVal, false
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
		return NoVal, false
	}
	fieldT := l.b.Type(fieldTypeRaw)
	v := l.b.Emit(OpGetField, fieldT, []ValueID{recvV}, "")
	// carry the field name on the instruction for codegen index resolution
	l.mod.Insts[len(l.mod.Insts)-1].Str = fieldName
	return v, true
}

// sliceMangledName returns the transpiler's mangled form of a slice/array type
// name, matching the rule in transpiler.go:
//
//	`[]t`  -> `_xt`
//	`[N]t` -> `_Nxt`
//
// This is used to match the implicit-self call name (`_xt.len`) against the
// receiver's raw type (`[]t`) in resolveCallee.
func sliceMangledName(raw string) string {
	raw = strings.TrimPrefix(raw, "?")
	if strings.HasPrefix(raw, "[]") {
		return "_x" + raw[2:]
	}
	if strings.HasPrefix(raw, "[") {
		if idx := strings.IndexByte(raw, ']'); idx > 0 {
			return "_" + raw[1:idx] + "x" + raw[idx+1:]
		}
	}
	return raw
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
	// Map types like [str]i64 start with '[' but must NOT be canonicalised
	// to []t — they have their own concrete method definitions.
	if strings.HasPrefix(recv, "[") && strings.Contains(recv, "]") && !isMapRaw(recv) {
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
		// Indirect call through a fn-typed local/parameter: `setup()` where
		// `setup` is a parameter of named function type (e.g. `test-cb`).
		// The callee is the VALUE held in the local's slot (a function pointer),
		// not a function NAME. Return the local's ValueID as recvV with an
		// empty callee so lowerCall emits an OpCall with inst.Callee set
		// (triggering emitIndirectCall in codegen). Without this the callee
		// falls through to canonSliceRecv("setup") which is not a registered
		// function name -> "unknown callee setup" (tests/named-fn-type.no).
		if v, ok := l.locals[name]; ok && v != NoVal {
			if vt := l.valueTypeOf(v); vt != NoType {
				if ty := l.mod.Type(vt); ty != nil && ty.Kind == KindFunc {
					return "", v
				}
			}
		}
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
				method := name[dot+1:]
				recvRaw := ""
				if ty := l.mod.Type(l.valueTypeOf(l.curRecv)); ty != nil && ty.Raw != "" {
					recvRaw = ty.Raw
				}
				// The transpiler mangles `[]t.method` to `_xt.method` and
				// `[N]t.method` to `_Nxt.method` (see transpiler.go). The
				// generic stub's body calls `.method()` which resolves to the
				// mangled name, but `tname` (`_xt`) never equals `recvRaw`
				// (`[]t`). Match the mangled form too, and route slice/array
				// builtins (len/cap) through sliceMethodBuiltin to avoid the
				// self-recursive call `_xt.len` -> `_xt.len`.
				if recvRaw != "" && (tname == recvRaw || tname == sliceMangledName(recvRaw)) {
					if bm := sliceMethodBuiltin(recvRaw, method); bm != "" {
						return bm, l.curRecv
					}
					// txt.len / txt.cap: txt is a fixed buffer, not a slice, so
					// sliceMethodBuiltin does not match. Its .len() method body
					// calls .len() which re-enters txt.len (same cycle as []t.len).
					// lowerDotRead already handles txt.len as OpLen, so route the
					// method call to the bare builtin "len" to avoid the cycle.
					if (method == "len" || method == "cap") && recvRaw == "txt" {
						return method, l.curRecv
					}
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
		// Implicit receiver `.method()` used OUTSIDE a method body. The parser
		// lowers a bare `.method(args)` to `self.method(args)` (see
		// parser/expr.go, lexer.DOT), but `self` is only bound inside a method
		// body: in a match arm the enclosing subject is bound to `it`, so
		// `.method()` there denotes `it.method()` — precisely the explicit form
		// the sibling arm pattern uses (`it.write-str(payload)`, cf.
		// tests/open-read.no, which already lowers correctly). Without this
		// the unbound `self` fell into the module-namespace branch below and the
		// callee became "self.write-str" -> "unknown callee self.write-str"
		// (tests/open-perm.no, open-write.no, fs-error-complete.no).
		var implicitIt ValueID = NoVal
		if rn := l.pkg.Node(recvID); rn != nil && rn.Kind == hir.KIdent &&
			l.pkg.Str(rn.S) == "self" {
			if _, bound := l.locals["self"]; !bound {
				if itv, ok := l.locals["it"]; ok {
					implicitIt = itv
				}
			}
		}
		// A bare identifier that is NOT a bound local is a MODULE namespace,
		// not a value: `number.rotate-left(t, 7)`, `net.send(fd, buf, n)`,
		// `fs.open(path)`. Lowering those as a method call tried to evaluate
		// the module name as a value and reported "unresolved identifier
		// <module>" — a large share of the remaining ident gaps. They lower to
		// a plain qualified call with NO receiver argument.
		if implicitIt == NoVal {
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
							return l.resolveModuleCallName(recvName, method), NoVal
						}
						// recvName is a bound GLOBAL (e.g. a top-level `data [4]i64`
						// or `v []str`); fall through to the method-call path below
						// so it is lowered as a real receiver value, not a module.
					}
				}
			}
		}
		// Method call: `obj.method(args)` lowers to `ReceiverType.method` with
		// the receiver passed as the FIRST argument (by pointer for
		// owned/struct receivers, matching the legacy ABI:
		//   call @str.to-bytes(%str-long* %recv, ...)).
		rv := implicitIt
		if rv == NoVal {
			rv = l.lowerExpr(recvID)
		}
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
		// A scalar newtype (`fd = i64`) is interned as a KindInt with Raw
		// "fd"; expand it to the underlying type so the callee matches the
		// legacy method table (`i64.to-str`, not `fd.to-str`).
		recvTypeName = l.expandTypeAlias(recvTypeName)
		// Optionals wrap their inner type with a leading '?'
		// (e.g. `?i64`). A method call on the unwrapped value of an optional
		// (`v.to-str()` where v: ?i64) would otherwise form the callee
		// "?i64.to-str", which does not exist — the method table holds
		// "i64.to-str". Strip the marker so the callee matches the legacy
		// backend, which resolves optional receivers to their inner type.
		//
		// The RECEIVER VALUE must be peeled too, not just the type name. Legacy
		// stores `?T` inline as {tag, T}, so handing the whole optional to a
		// method call already yields the payload for free. MIR keeps non-scalar
		// payloads in a dedicated `%option_<elem>` struct, so the peel is only
		// explicit here: without it `r.len()` on `r ?[]byte` reached
		// emitBuiltinLen with a `%option___byte` receiver -> "unsupported receiver
		// type %option___byte" (tests/fs-struct.no).
		if strings.HasPrefix(recvTypeName, "?") {
			if uv := l.unwrapOptionOperand(rv); uv != NoVal {
				rv = uv
				// Owned-string receivers (?str -> str) share their heap buffer with
				// the original option value: the unwrap is an `extractvalue` of the
				// payload, so the fresh unwrapped value aliases the option's data
				// pointer. The unwrapped value is a separate owned local (it gets
				// its own drop), while the option keeps a drop at function exit —
				// so both would free the same buffer (double free / use-after-free,
				// observed as a runtime SIGTRAP in str.replace-n's `s = parts[i]`
				// + `s.len-bytes()` / `s.byte(j)` inner loop). Clone the unwrapped
				// receiver so it owns an independent copy. String methods are
				// read-only (strings are immutable), so the clone is
				// behaviour-identical and the printed output is unchanged — the
				// clone is simply dropped alongside the original.
				if ty := l.mod.Type(l.valueTypeOf(rv)); ty != nil && ty.Kind == KindStr {
					rv = l.b.Emit(OpClone, l.b.Type("str"), []ValueID{rv}, "")
				}
				if ty := l.mod.Type(l.valueTypeOf(rv)); ty != nil && ty.Raw != "" {
					recvTypeName = ty.Raw
				}
				recvTypeName = l.expandTypeAlias(recvTypeName)
			}
		}
		recvTypeName = strings.TrimPrefix(recvTypeName, "?")
		// A VIEW receiver (`&T`, e.g. a struct field declared `p &point`) reads
		// through the borrow: the method is defined on the BORROWED type, so the
		// callee must be `point.sum`, not `&point.sum` ("unknown callee
		// &point.sum"). Only the NAME is stripped — the receiver VALUE stays the
		// view, which is already `T*` and is passed to `self` unchanged by
		// emitCallBody's view branch.
		recvTypeName = strings.TrimPrefix(recvTypeName, "&")
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
		// Map types ([str]i64, [i64]bool, ...) have user-defined methods on the
		// concrete hashmap struct (hashmap-str-i64, hashmap-i64-bool, ...), not
		// on the [key]val form. The generic_structs pass in the legacy backend
		// instantiates hashmap-<key>-<val> from the hashmap-str-tmpl template and
		// renames all methods accordingly. Without this mapping, m.len() on a
		// [str]i64 map searched for "[str]i64.len" which does not exist, falling
		// through to canonSliceRecv → "[]t.len" → "unknown callee" (or worse,
		// before the map guard was added, dispatching to the str-len builtin on
		// an i64 receiver). The naming convention is:
		//   [key]val → hashmap-<key>-<val>
		// e.g. [str]i64 → hashmap-str-i64, [i64]bool → hashmap-i64-bool.
		if k, v, ok := parseMapTypes(recvTypeName); ok {
			// Match the legacy generic_structs naming: the value type is sanitised
			// (e.g. []str → slice_str) so hashmap-str-[]str becomes
			// hashmap-str-slice_str. Key types in the corpus are always scalar
			// (str/i64/bool) and need no sanitisation.
			vSan := strings.ReplaceAll(v, "[]", "slice_")
			hashmapName := "hashmap-" + k + "-" + vSan
			hashmapCallee := hashmapName + "." + method
			if _, ok := l.funcNames[hashmapCallee]; ok {
				return hashmapCallee, rv
			}
		}
		// A CONCRETE slice method (`[]byte.slice`, `[]char.to-str`, `[]ord.sort-asc`,
		// `[]str.join`, ...) is a real function with a real body, defined by the std
		// library for that element type. It must win over the canonicalised `[]t.m`
		// form: canonSliceRecv is a fallback for GENERIC slice methods (`[]t.len`),
		// and it maps unconditionally, so `buf.slice(0, n)` on a `[]byte` became the
		// non-existent `[]t.slice` -> "unknown callee []t.slice"
		// (tests/mem-safety/bug15-read-dowhile-copyfile.no). Preferring the concrete
		// name also matches the legacy backend, which tries `[]<elem>.m` before the
		// `_x<elem>.m` / generic candidates.
		concrete := recvTypeName + "." + method
		if _, ok := l.funcNames[concrete]; ok {
			return concrete, rv
		}
		return canonSliceRecv(concrete), rv
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
	// Map types like [str]i64 also start with '[', but they are NOT slices —
	// their .len() / .put() / .get() etc. are user-defined methods, not
	// builtins. Without this guard, m.len() on a [str]i64 map was dispatched
	// to the str-len builtin, which then rejected the i64 (opaque handle)
	// receiver with "unsupported receiver type i64".
	if isMapRaw(recvTypeName) {
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

// variantCtorOptType derives the ?T result type of an option-variant
// constructor call (`err(x)` / `ok(x)` / `some(x)`). It prefers the LHS/context
// type hint when that is an option (e.g. `x3 ?str = err('msg')` publishes
// `?str` via lowerAssignNode), and otherwise derives `?<elem>` from the single
// payload argument's type. Returns NoType when neither is available, so the
// caller falls through to the (incorrect) generic path and surfaces a
// diagnostic instead of building a malformed option.
func (l *lowerer) variantCtorOptType(argv []ValueID) TypeID {
	if ht := l.typeHint; ht != NoType && ht != l.voidType {
		if tt := l.mod.Type(ht); tt != nil && tt.Kind == KindOption {
			return ht
		}
	}
	if len(argv) > 0 {
		at := l.valueTypeOf(argv[0])
		if at != NoType && at != l.voidType {
			if at2 := l.mod.Type(at); at2 != nil && at2.Raw != "" {
				if ot := l.b.Type("?" + at2.Raw); ot != l.voidType {
					return ot
				}
			}
		}
	}
	return NoType
}

// namedFormatKind describes how a print-family callee consumes its format
// string: which stream it writes to, whether a trailing newline is appended,
// and whether it RETURNS the formatted string instead of writing it.
type namedFormatKind struct {
	stderr  bool
	newline bool
	retStr  bool
}

// namedFormatFns mirrors llvm.shouldInterceptNamedFormat: exactly these
// builtins perform {name:spec} substitution. `println` is deliberately ABSENT
// — the legacy table does not list it either, so `println('x={n}')` prints the
// braces literally in both backends.
var namedFormatFns = map[string]namedFormatKind{
	"print":   {newline: true},
	"eprint":  {stderr: true, newline: true},
	"printf":  {},
	"eprintf": {stderr: true},
	"format":  {retStr: true},
	"sprintf": {retStr: true},
}

// isPrintFamilyCallee reports whether callee is one of the builtins that
// perform named-format substitution (possibly `fmt.`-qualified). A method call
// such as `w.print(...)` is NOT one of them.
func isPrintFamilyCallee(callee string) bool {
	base := callee
	if i := strings.LastIndex(base, "."); i >= 0 {
		if base[:i] != "fmt" {
			return false
		}
		base = base[i+1:]
	}
	_, ok := namedFormatFns[base]
	return ok
}

// lowerNamedFormat lowers a print-family call whose format string contains
// {name} / {name:spec} fields. It mirrors the legacy path
// (llvm.callNamedFormat) in two respects that matter for output equality:
//
//   - interception happens ONLY for a print-family callee, and for
//     print/eprint only when the call has exactly one argument; and
//   - each literal segment is written verbatim, each field is rendered by the
//     std fmt-* helper (fmt-int / fmt-uint / fmt-f64 / fmt-str / fmt-bool),
//     and print/eprint append a single trailing newline.
//
// Returns false when the call is not a named-format call (or the format string
// parses but has no fields), so the generic path handles it. Returns true when
// the call was lowered — or when it deliberately refused to lower and recorded
// an "interp" fallback diagnostic, because emitting the un-substituted text
// would be silently wrong.
func (l *lowerer) lowerNamedFormat(callee string, n *hir.Node) (bool, ValueID) {
	base := callee
	if i := strings.LastIndex(base, "."); i >= 0 {
		if base[:i] != "fmt" {
			return false, NoVal
		}
		base = base[i+1:]
	}
	k, ok := namedFormatFns[base]
	if !ok {
		return false, NoVal
	}
	args := l.slotArgs(n.Id, "arg")
	if len(args) == 0 {
		return false, NoVal
	}
	// Legacy intercepts print/eprint only for a single string-literal argument;
	// a multi-arg call goes through the variadic (space-separated) path.
	if (base == "print" || base == "eprint") && len(args) != 1 {
		return false, NoVal
	}
	a0 := l.pkg.Node(args[0])
	if a0 == nil || a0.Kind != hir.KStrLit {
		return false, NoVal
	}
	s := l.pkg.Str(a0.S)
	if !strings.Contains(s, "{") {
		return false, NoVal
	}
	segs, err := parser.ParseFormatString(s)
	if err != nil {
		return false, NoVal
	}
	hasField := false
	for _, sg := range segs {
		if sg.Field != nil {
			hasField = true
			break
		}
	}
	if !hasField {
		return false, NoVal
	}
	if k.retStr {
		// format()/sprintf() CONCATENATE the segments into one str result.
		return l.lowerNamedFormatResult(segs, s)
	}
	writeFn := "$print_str"
	nlFn := "$print_nl"
	if k.stderr {
		writeFn = "$eprint_str"
		nlFn = "$eprint_nl"
	}
	for _, sg := range segs {
		if sg.Field == nil {
			if sg.Literal == "" {
				continue
			}
			lit := l.b.EmitStr(OpConst, l.b.Type("str"), sg.Literal, "")
			l.b.EmitVoid(OpCall, []ValueID{lit}, writeFn)
			continue
		}
		v, ok := l.lowerFormatField(sg.Field)
		if !ok {
			// lowerFormatField already recorded the fallback diagnostic.
			return true, NoVal
		}
		l.b.EmitVoid(OpCall, []ValueID{v}, writeFn)
	}
	if k.newline {
		l.b.EmitVoid(OpCall, nil, nlFn)
	}
	return true, NoVal
}

// lowerNamedFormatResult builds the str RESULT of format()/sprintf() by folding
// every rendered segment with the synthetic $str_concat helper (legacy folds the
// same way in callNamedFormat via concatStrLongPtrs). A single segment is
// returned as-is; an empty format string yields an empty str constant.
func (l *lowerer) lowerNamedFormatResult(segs []parser.FormatSegment, src string) (bool, ValueID) {
	strT := l.b.Type("str")
	var pieces []ValueID
	for _, sg := range segs {
		if sg.Field == nil {
			if sg.Literal == "" {
				continue
			}
			pieces = append(pieces, l.b.EmitStr(OpConst, strT, sg.Literal, ""))
			continue
		}
		v, ok := l.lowerFormatField(sg.Field)
		if !ok {
			// lowerFormatField already recorded the fallback diagnostic.
			return true, NoVal
		}
		pieces = append(pieces, v)
	}
	if len(pieces) == 0 {
		return true, l.b.EmitStr(OpConst, strT, "", "")
	}
	acc := pieces[0]
	for _, p := range pieces[1:] {
		dsts := l.b.EmitCallMulti([]TypeID{strT}, []ValueID{acc, p}, "$str_concat")
		if len(dsts) == 0 {
			l.unsupported(l.curFuncName(), "interp", "format() concat produced no result: "+interpPreview(src))
			return true, NoVal
		}
		acc = dsts[0]
	}
	return true, acc
}

// lowerFormatField renders one {name[:spec]} field into a str value by calling
// the matching std fmt-* helper. It handles only fields whose name is a simple
// identifier bound in the current function or as a module global — an
// expression field (`{hash[i]:02x}`) is source text that can no longer be
// parsed here, so it is refused with an "interp" fallback diagnostic.
func (l *lowerer) lowerFormatField(f *parser.FormatField) (ValueID, bool) {
	if f == nil || f.Name == "" {
		l.unsupported(l.curFuncName(), "interp", "empty format field")
		return NoVal, false
	}
	v, ok := l.lookupFormatValue(f.Name)
	if !ok {
		l.unsupported(l.curFuncName(), "interp", "unresolved format field "+f.Name)
		return NoVal, false
	}
	// A format field whose value is an `?T` option must be PEELED before it
	// reaches a fmt-* helper, because the helpers are typed on the payload
	// (fmt-str takes a %str-long, fmt-int an i64) and never on the option.
	//
	// The option case is the common one, not a corner: the parser types a
	// match arm's synthetic `it` binding as the option's PAYLOAD
	// (`let it=matched t=str` for an `ok ->` arm of a `?str` match — see
	// buildItBindingForArm), but the let-lowering deliberately refuses to peel
	// an OWNED payload, so `it` keeps the whole %option end-to-end. `print(it)`
	// is fine (emitCall has a dedicated option-print path), but
	// `print('{it} is installed')` lowered the field straight into fmt-str and
	// emitted `store %str-long %lv<option>` -> LLVM verifier "defined with
	// type '%option' ... but expected '%str-long'" (nonpm cmd-why).
	//
	// Mirror the optional-RECEIVER unwrap in resolveCallee: extractvalue the
	// payload, and CLONE an owned payload so the temp owns an independent
	// buffer — the unwrapped value aliases the option's data pointer, and the
	// option keeps its own drop, so sharing would free the same bytes twice.
	if t := l.mod.Type(l.valueTypeOf(v)); t != nil && t.Kind == KindOption {
		if uv := l.unwrapOptionOperand(v); uv != NoVal {
			v = uv
			if ty := l.mod.Type(l.valueTypeOf(v)); ty != nil && ty.Kind == KindStr {
				v = l.b.Emit(OpClone, l.b.Type("str"), []ValueID{v}, "")
			}
		}
	}
	raw := ""
	if t := l.mod.Type(l.valueTypeOf(v)); t != nil {
		raw = t.Raw
	}
	raw = strings.TrimPrefix(raw, "?")
	fn := ""
	switch {
	case raw == "str":
		fn = "fmt-str"
	case raw == "f64" || raw == "float" || raw == "double":
		fn = "fmt-f64"
		// NOTE: `bool` deliberately does NOT go through std fmt-bool here.
		// fmt-bool renders "true"/"false", but the legacy backend renders a
		// bool format field as "1"/"0" in function bodies (its varTypes table
		// does not expose i1 for locals, so dispatchFmtCall falls through to
		// fmt-int) — the same 1/0 that MIR's own print_bool emits for plain
		// `print(b)`. Routing through fmt-int keeps MIR self-consistent
		// (print(b) and print('{b}') agree) and matches the corpus-dominant
		// legacy output. Only legacy's top-level-script mode prints "true",
		// which is the inconsistent case (its print(b) prints "true" there
		// too — MIR standardizes on 1/0 for both).
	case raw == "" || strings.HasPrefix(raw, "option") || strings.HasPrefix(raw, "[]") ||
		strings.HasPrefix(raw, "[") || strings.Contains(raw, "{"):
		// Container / option / unknown: legacy has no fmt-* helper for these
		// either (it routes them through .to-str or emits ""). Refuse instead
		// of guessing.
		l.unsupported(l.curFuncName(), "interp", "format field type "+raw+" not lowered")
		return NoVal, false
	case isIntegerMIRType(raw):
		// Integer (i8..i64, u8..u64, byte, int, ...) and bool. The unsigned renderings
		// (b/o/x/X/p) go through fmt-uint, everything else through fmt-int —
		// exactly llvm.dispatchFmtCall.
		fn = "fmt-int"
		if f.Parsed != nil {
			switch f.Parsed.Type {
			case 'b', 'o', 'x', 'X', 'p':
				fn = "fmt-uint"
			}
		}
	default:
		// Structs (regexp, user types) and anything else unrecognised: there is
		// no fmt-* helper for them. The old `default:` branch assumed "not one
		// of the special cases ⇒ integer" and handed a %struct value to
		// fmt-int, which produced invalid IR ("'%lv' defined with type
		// '%regexp_regexp' but expected 'i64'") instead of a clean refusal.
		// Refusing lets the caller fall back rather than emit garbage.
		l.unsupported(l.curFuncName(), "interp", "format field type "+raw+" not lowered")
		return NoVal, false
	}
	l.enqueueCallee(fn)
	strT := l.b.Type("str")
	specV := l.b.EmitStr(OpConst, strT, f.Spec, "")
	argV := v
	if fn == "fmt-int" || fn == "fmt-uint" {
		// fmt-int / fmt-uint take an i64; widen a narrower integer so the
		// out-parameter store is not a type mismatch.
		if i64T := l.b.Type("i64"); l.valueTypeOf(v) != i64T {
			if lt := l.mod.Type(l.valueTypeOf(v)); lt != nil && isIntegerMIRType(lt.Raw) && lt.Raw != "i64" {
				argV = l.b.Emit(OpCast, i64T, []ValueID{v}, "")
			}
		}
	}
	dsts := l.b.EmitCallMulti([]TypeID{strT}, []ValueID{argV, specV}, fn)
	if len(dsts) == 0 {
		l.unsupported(l.curFuncName(), "interp", "fmt call "+fn+" produced no result")
		return NoVal, false
	}
	return dsts[0], true
}

// fmtIndexFieldRe matches the only expression shape a format field
// realistically uses: `ident[index]` (e.g. `{hash[i]:02x}`).
var fmtIndexFieldRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\[([A-Za-z_][A-Za-z0-9_-]*|[0-9]+)\]$`)

// fmtMethodFieldRe matches a zero-argument method call on a simple identifier:
// `ident.method()` (e.g. `{content.len-bytes()}`). This is the second most
// common expression-field shape after `ident[index]`, and it covers all the
// `.len()`, `.len-bytes()`, `.to-str()` calls that appear in debug print
// statements. Only NO-ARGUMENT methods are supported — the format field syntax
// has no way to express arguments, and any method that takes args would need a
// full re-parse.
var fmtMethodFieldRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)\(\)$`)

// fmtDotFieldRe matches a single property/field read on a simple identifier:
// `ident.field` (e.g. `{img.width}`, `{resolved.len}`). Struct fields and the
// container properties `.len` / `.cap` both land here — lowerFieldReadOn
// dispatches on the receiver's kind. Exactly ONE dot is accepted: a chained
// `{a.b.c}` still has no lowering.
var fmtDotFieldRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\.([A-Za-z_][A-Za-z0-9_-]*)$`)

// lookupFormatValue resolves the value a {name} format field refers to.
//
// A field name is SOURCE TEXT (`hash[i]`, `content.len-bytes()`), and MIR sees
// only HIR — there is no parser left at this stage, so a general expression
// field cannot be lowered and is refused (legacy re-parses it with
// lexer+parser+checker, which is not available here). Two expression shapes
// are supported because they cover the vast majority of real-world usage:
//
//   - `ident[index]` (e.g. `{hash[i]:02x}`) — container element read, lowered
//     to OpIndex.
//   - `ident.method()` (e.g. `{content.len-bytes()}`) — zero-argument method
//     call, lowered to OpCall with the receiver as the first argument.
//   - `ident.field` (e.g. `{img.width}`, `{resolved.len}`) — struct field or
//     container property read on the base binding, lowered through
//     lowerFieldReadOn (the shared tail of `recv.field`).
//
// General expression fields (e.g. `{a.b.c}`, `{f(x)}`) are still refused.
func (l *lowerer) lookupFormatValue(name string) (ValueID, bool) {
	if v, ok := l.locals[name]; ok {
		return v, true
	}
	if _, isGlobal := l.globals[name]; isGlobal {
		return l.lowerGlobalRef(name), true
	}
	// ident[index] pattern
	if m := fmtIndexFieldRe.FindStringSubmatch(name); m != nil {
		base, ok := l.lookupFormatValue(m[1])
		if !ok {
			return NoVal, false
		}
		var idxV ValueID
		if n, err := strconv.ParseInt(m[2], 0, 64); err == nil {
			idxV = l.b.EmitInt(OpConst, l.b.Type("i64"), n, "")
		} else if iv, ok := l.lookupFormatValue(m[2]); ok {
			idxV = iv
		} else {
			return NoVal, false
		}
		elemT := l.elementTypeOf(base)
		if elemT == l.voidType {
			return NoVal, false
		}
		return l.b.Emit(OpIndex, elemT, []ValueID{base, idxV}, ""), true
	}
	// ident.field pattern — struct field / container property read. Checked
	// AFTER the method pattern so `ident.method()` never reaches it (the
	// trailing `()` makes the two regexes disjoint anyway, but the order keeps
	// the intent explicit).
	if m := fmtDotFieldRe.FindStringSubmatch(name); m != nil {
		if base, ok := l.lookupFormatValue(m[1]); ok {
			if v, ok := l.lowerFieldReadOn(base, m[2]); ok {
				return v, true
			}
		}
		// Fall through to the refusal below: an unresolvable base or a field
		// that does not exist on it must still be reported as an interp gap.
	}
	// ident.method() pattern — zero-argument method call
	if m := fmtMethodFieldRe.FindStringSubmatch(name); m != nil {
		base, ok := l.lookupFormatValue(m[1])
		if !ok {
			return NoVal, false
		}
		method := m[2]
		// Determine the receiver type to form the callee name.
		recvT := l.valueTypeOf(base)
		recvTypeName := "str"
		if ty := l.mod.Type(recvT); ty != nil && ty.Raw != "" {
			recvTypeName = ty.Raw
		}
		recvTypeName = l.expandTypeAlias(recvTypeName)
		recvTypeName = strings.TrimPrefix(recvTypeName, "?")
		// Check if there's a builtin for this receiver+method.
		// Builtins are dispatched by the Inst.Sym string at codegen time
		// (emitBuiltinForward), so we just emit the call with the right
		// symbol — no enqueue needed (builtins have no HIR body to lower).
		if bm := sliceMethodBuiltin(recvTypeName, method); bm != "" {
			// Builtin slice/array method (e.g. len, cap, ...).
			callee := bm
			if bmEntry := builtin.FindBuiltinMethod(recvTypeName + "." + method); bmEntry != nil {
				callee = bmEntry.ForwardFunc
			}
			return l.b.Emit(OpCall, l.b.Type("i64"), []ValueID{base}, callee), true
		}
		// Check the builtin table for a str/scalar method (e.g. str.len-bytes).
		if bm := builtin.FindBuiltinMethod(recvTypeName + "." + method); bm != nil {
			callee := bm.ForwardFunc
			resT := l.b.Type("i64")
			if len(bm.Return) > 0 && bm.Return[0] == parser.TypeStr {
				resT = l.b.Type("str")
			}
			return l.b.Emit(OpCall, resT, []ValueID{base}, callee), true
		}
		// Not a builtin — try a user-defined method. The callee is
		// "recvType.method" (e.g. "str.to-str").
		callee := canonSliceRecv(recvTypeName + "." + method)
		l.enqueueCallee(callee)
		// We don't know the return type; use i64 as a safe default.
		// The format field rendering will coerce to str via fmt-* helpers.
		return l.b.Emit(OpCall, l.b.Type("i64"), []ValueID{base}, callee), true
	}
	return NoVal, false
}

// isIntegerMIRType reports whether a nolang type string is a scalar integer
// that needs widening before being handed to fmt-int / fmt-uint.
func isIntegerMIRType(raw string) bool {
	switch raw {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "byte", "char", "int", "uint":
		return true
	}
	// `bool` is an i1 at the LLVM level and is rendered through fmt-int
	// (see lowerFormatField); it needs the same zext to i64.
	return raw == "bool"
}

// letDeclaredType returns a KLet's declared type, or NoType when that type is
// a bare option-variant marker (`err`, `ok`, `err | nil`) rather than a real
// Nolang type.
func (l *lowerer) letDeclaredType(n *hir.Node) TypeID {
	dt := l.typeOfNode(n)
	if dt == NoType || dt == l.voidType {
		return dt
	}
	ty := l.mod.Type(dt)
	if ty == nil || ty.Raw == "" {
		return dt
	}
	for _, p := range strings.Split(ty.Raw, "|") {
		switch strings.TrimSpace(p) {
		case "err", "ok", "some", "nil", "":
		default:
			return dt
		}
	}
	return NoType
}

// unwrapOptionOperand rewrites a `?T`-typed operand into its payload `T`
// (emitMove lowers the move as an `extractvalue` of the option's payload
// field). Non-option operands are returned unchanged.
func (l *lowerer) unwrapOptionOperand(v ValueID) ValueID {
	if v == NoVal {
		return v
	}
	t := l.valueTypeOf(v)
	if t == NoType || t == l.voidType {
		return v
	}
	ty := l.mod.Type(t)
	if ty == nil || ty.Kind != KindOption {
		return v
	}
	elem, ok := parseOptionElem(ty.Raw)
	if !ok || elem == "" {
		return v
	}
	et := l.b.Type(elem)
	if et == NoType {
		return v
	}
	return l.b.Emit(OpMove, et, []ValueID{v}, "")
}

// bareCalleeName strips a module prefix from a callee name (`fs.stat-size` ->
// `stat-size`) so builtin tables keyed by bare method name can be consulted.
func bareCalleeName(callee string) string {
	if i := strings.LastIndex(callee, "."); i >= 0 {
		return callee[i+1:]
	}
	return callee
}

func (l *lowerer) lowerCall(n *hir.Node) ValueID {
	callee, recvV := l.resolveCallee(n)
	if callee == "" && recvV != NoVal {
		// Indirect call through a fn-typed local/parameter (e.g. `setup()`
		// where `setup` has named function type `test-cb`). resolveCallee
		// returned the local's ValueID as recvV with an empty callee.
		// Emit an OpCall with inst.Callee = recvV so codegen's
		// emitIndirectCall loads the function pointer and calls through it.
		argv := l.lowerCallArgs(n, NoVal, "")
		resTyp := l.typeOfNode(n)
		if resTyp == NoType || resTyp == l.voidType {
			resTyp = l.b.Type("i64")
		}
		v := l.b.Emit(OpCall, resTyp, argv, "")
		l.mod.Insts[len(l.mod.Insts)-1].Callee = recvV
		return v
	}
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
	// Tagged-enum variant constructor: `full(7)` / `rect(2.0, 3.0)` / `ok(5)`.
	// Checked BEFORE the option constructors below, because a tagged enum may
	// legitimately declare a variant named `ok` — and when it does, the
	// expected type at this site is the enum, not an option
	// (tests/tagged-enum.no: `a-res` and `b-res` both declare `ok` with
	// different payload types and different tags).
	if ei, vi := l.enumVariantOf(l.enumPrefer(), callee); ei != nil && vi != nil {
		return l.lowerEnumCtor(n, ei, vi)
	}

	// reachability: enqueue the referenced function for lowering
	// Option variant constructors `err(x)` / `ok(x)` / `some(x)` are CALLS
	// (they carry a payload argument), distinct from the bare variant
	// identifiers handled in the KIdent case (hir2mir.go). Route them through
	// EmitOptionWrap so the ?T option value is built inline with the correct
	// discriminant and payload type. The generic call path mis-resolves `err`
	// to the std io.err stderr-writer (i64 result) and the caller then inserts
	// that i64 into the %str-long option payload slot, which opt rejects
	// (tests/option-test.no, tests/option-match.no, ...). This is the
	// documented intent of Builder.EmitOptionWrap — see its comment.
	if callee == "err" || callee == "ok" || callee == "some" {
		argv := l.lowerCallArgs(n, recvV, callee)
		if optTyp := l.variantCtorOptType(argv); optTyp != NoType {
			tag := int64(0)
			if callee == "err" {
				tag = 2
			}
			var payload ValueID = NoVal
			if len(argv) > 0 {
				payload = argv[0]
			}
			return l.b.EmitOptionWrap(optTyp, tag, payload)
		}
	}

	// Named-format interception: `print('x={n}')` / `eprint('{a}:{b}')` /
	// `printf('{pi:.2f}')`. The legacy backend substitutes {name[:spec]}
	// fields at the print site only (llvm.callFmt -> callNamedFormat); MIR
	// must do the same HERE, at lowering time, because the field name is
	// source text that is gone by codegen. Returns true when the call has
	// been fully lowered (or deliberately refused with a fallback
	// diagnostic) and the generic path below must not run.
	if handled, res := l.lowerNamedFormat(callee, n); handled {
		return res
	}

	l.enqueueCallee(callee)

	// #84: A method called via module namespace (e.g. `json.parse('')`) has
	// recvV == NoVal because `json` was treated as a module name, not a
	// receiver value. But the callee is registered as a method
	// (f.IsMethod=true) and its first parameter is the implicit `self`
	// receiver. Without a receiver value, lowerCallArgs does not prepend one,
	// so inst.Args[0] is the first REAL argument (e.g. `s str`), which
	// emitCallBody's `cf.IsMethod && i == 0` branch then mistakes for the
	// receiver — passing a %str-long alloca (24 bytes) where the callee
	// expects a %json_json struct (22KB), causing a SIGSEGV when the callee
	// reads self fields past the 24-byte boundary.
	//
	// Fix: synthesize a zero-initialized receiver of the callee's declared
	// receiver type and set recvV to it, so lowerCallArgs prepends it as the
	// first argument. The zero value is safe because `json.parse` (and similar
	// module-namespace methods) do not read from `self` — they construct a new
	// value and return it. Methods that DO read self are always called with an
	// explicit receiver (obj.method()), never via module namespace.
	if recvV == NoVal {
		// Only synthesize a receiver when the HIR call args do NOT already
		// include one. A method call via KDot (`a.fill(99)`) is lowered by
		// resolveCallee to a KIdent callee (`_3xi64.fill`) with recvV=NoVal,
		// BUT the HIR call args already list the receiver `a` as the first
		// arg slot — lowerCallArgs will lower it directly. Synthesizing ANOTHER
		// receiver here would prepend a spurious extra argument (3 args instead
		// of 2), causing a type mismatch at the call site.
		//
		// The synthesis is only needed for TRUE module-namespace calls
		// (e.g. `json.parse('')`) where the HIR call args do NOT include a
		// receiver — the module name was consumed as a namespace prefix, not
		// an argument. Detect this by checking whether the number of HIR arg
		// slots is less than the callee's parameter count (excluding the
		// receiver param): if the args already cover all non-receiver params,
		// the receiver must be among them.
		hirArgs := l.slotArgs(n.Id, "arg")
		needSynthRecv := true
		if fid, ok := l.funcNames[callee]; ok {
			if fn := l.pkg.Node(fid); fn != nil && fn.Has(hir.FlagMethod) {
				// Count the callee's non-receiver params (all KParam children).
				// self is now a KResult (out-param), so all KParams are real
				// parameters — none is the receiver.
				paramCount := 0
				for _, c := range l.pkg.Children(fid) {
					cn := l.pkg.Node(c)
					if cn == nil || cn.Kind != hir.KParam {
						continue
					}
					paramCount++
				}
				// If HIR args >= non-receiver params, the args already include
				// the receiver (e.g. `a.fill(99)` has 2 args for 1 non-receiver
				// param). Do NOT synthesize.
				if len(hirArgs) > paramCount {
					needSynthRecv = false
				}
				if needSynthRecv {
					// Get the receiver type from the first KResult child
					// (self is the first result / out-param).
					for _, c := range l.pkg.Children(fid) {
						cn := l.pkg.Node(c)
						if cn == nil || cn.Kind != hir.KResult {
							continue
						}
						recvTyp := l.typeOfNode(cn)
						if recvTyp != NoType && recvTyp != l.voidType {
							recvV = l.b.Emit(OpConst, recvTyp, nil, "")
							l.mod.Values[recvV].Name = "zero-recv"
						}
						break // only the first KResult (self)
					}
				}
			}
		}
	}

	if isPrintFamilyCallee(callee) {
		l.inPrintArgs++
		defer func() { l.inPrintArgs-- }()
		savedWrap := l.wrapPrintArgs
		l.wrapPrintArgs = true
		defer func() { l.wrapPrintArgs = savedWrap }()
	}
	argv := l.lowerCallArgs(n, recvV, callee)
	// A `run`-launched call is async regardless of its name (syntactic async);
	// a bare call to an `-async`-suffixed function also yields a lazily
	// enqueued handle. Both lower to OpRun — a lazily-enqueued %task whose
	// opaque i8* handle is returned as an i64. The matching `awy` (OpAwait)
	// drives the task to completion and reads the result. This keeps async
	// calls out of the normal out-param call path (which would execute the
	// function eagerly and return its value, not a handle).
	if (l.forceRunCall != hir.NoID && n.Id == l.forceRunCall) || strings.HasSuffix(callee, "-async") {
		handleTyp := l.b.Type("i64")
		v := l.b.Emit(OpRun, handleTyp, argv, callee)
		// Remember the task's result type: the handle is an opaque i64, so the
		// matching `awy` cannot recover it and would otherwise read the result
		// buffer as i64 (e.g. printing a str result as its length).
		if rt := l.resultTypeOfCallee(callee); rt != l.voidType && rt != NoType {
			l.asyncResTypes[v] = rt
		}
		return v
	}
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
	// Option-returning builtins (stat-size / file-size / fstat-size) are
	// declared in std as a single `?i64`, but the builtin table describes
	// them as a (value, ok) PAIR. Legacy collapses that pair into
	// `%option { tag = ok ? 0 : 1, data = value }` (generateOptionAssign /
	// optionReturnBuiltinName in build/llvm/stmt.go). MIR must do the same:
	// otherwise the call lowers to a BARE i64, and `size ?= fstat-size(.fd)`
	// then compares it against `err` — a bare variant whose type can only be
	// recovered from an option hint, which a non-option i64 cannot supply —
	// so it degrades to a void `undef` constant, `icmp eq i64 %size, undef`
	// folds to `unreachable`, and the program dies with SIGTRAP
	// (tests/open-read.no).
	optRet := callee != "" && builtin.IsOptionReturnBuiltin(bareCalleeName(callee))
	if optRet {
		resTyp = l.b.Type("?i64")
	}
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
		// An option-returning builtin's (T, ok) pair is NOT a two-result
		// contract at the language level — std declares it as a single `?T`.
		// Lowering it as two results hands the caller a bare i64 and loses the
		// discriminant entirely (see the optionRet note above).
		if len(brs) > 1 && !optRet {
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
	// Out-parameter binding: a call like `src.copy(dst)` passes `dst` as the
	// caller-side argument of `str.copy`'s named result parameter
	// (`str.copy = () (dst str)`). Nolang's out-parameter convention writes the
	// callee's result into `dst`, so the caller's `dst` must be updated. The
	// HIR represents this with no result binding (a bare expr-stmt), so without
	// this the returned value is discarded and the caller keeps reading the
	// pre-call `dst` (test-str's copy test prints the unmodified '------').
	// Only fire when every callee parameter is supplied as an argument: if the
	// argument count is short, the result params are pure RETURN values bound by
	// an LHS assign (`s = src.to-upper()`), not caller-supplied out-arguments.
	l.bindOutParams(n, callee, dsts)
	return dsts[0]
}

// countResultParams returns the number of named result (out-) parameters of the
// given callee, EXCLUDING `self` for methods (self is an out-param but is not a
// return value the caller receives). Returns 0 for builtins/externs not present
// in the HIR package.
func (l *lowerer) countResultParams(callee string) int {
	fid, ok := l.funcNames[callee]
	if !ok {
		return 0
	}
	fdef := l.pkg.Node(fid)
	if fdef == nil {
		return 0
	}
	isMethod := fdef.Has(hir.FlagMethod)
	n := 0
	firstSkipped := false
	for _, c := range l.pkg.Children(fid) {
		if cn := l.pkg.Node(c); cn != nil && cn.Kind == hir.KResult {
			if isMethod && !firstSkipped {
				firstSkipped = true
				continue // skip self
			}
			n++
		}
	}
	return n
}

// bindOutParams moves the results of a call whose callee has named result
// (out-) parameters back into the caller-side variables supplied at those
// parameter positions. See the call site above for the nolang out-parameter
// semantics. The k-th result parameter binds the
// (len(args)-len(resultParams)+k)-th HIR argument, which is the variable that
// receives the result. Only identifiers (not arbitrary expressions) are bound,
// matching legacy (which passes the variable's address as the out-pointer).
func (l *lowerer) bindOutParams(n *hir.Node, callee string, dsts []ValueID) {
	fid, ok := l.funcNames[callee]
	if !ok {
		return
	}
	fdef := l.pkg.Node(fid)
	if fdef == nil {
		return
	}
	kParam, kResult := 0, 0
	isMethod := fdef.Has(hir.FlagMethod)
	resultSkipped := false
	for _, c := range l.pkg.Children(fid) {
		cn := l.pkg.Node(c)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			kParam++
		case hir.KResult:
			// For a method, the first KResult is `self` (the receiver
			// out-param), not a real result — do not count it.
			if isMethod && !resultSkipped {
				resultSkipped = true
				continue
			}
			kResult++
		}
	}
	if kResult == 0 || len(dsts) < kResult {
		return
	}
	args := l.slotArgs(n.Id, "arg")
	if len(args) < kResult {
		return
	}
	// Arg-form vs LHS-form detection.
	//   - Non-variadic callee: arg-form requires len(args) == kParam+kResult;
	//     otherwise the result params are pure RETURN values bound by an LHS
	//     assign (`r = f(...)`), not caller-supplied out-arguments.
	//   - For a METHOD, the HIR call args include the receiver as the first
	//     argument (e.g. `src.copy(dst)` has args=[src, dst]), so arg-form
	//     requires len(args) == 1(receiver) + kParam + kResult. Without the
	//     +1 for the receiver, `src.copy(dst)` is rejected (2 != 0+1) and the
	//     result is never moved back into `dst` (test-str copy test reads the
	//     unmodified '------').
	//   - Variadic callee: the variadic formal packs >=1 actual args into one
	//     formal, so len(args) exceeds kParam+kResult. The old strict equality
	//     guard wrongly rejected these (e.g. `number.max(10, 20, r)`), silently
	//     discarding the result. For them, arg-form is detected by the trailing
	//     kResult arguments all being identifiers (legacy passes the variable's
	//     address as the out-pointer).
	recvOffset := 0
	if isMethod {
		recvOffset = 1 // the receiver (self) is the first HIR arg
	}
	if len(args) != recvOffset+kParam+kResult && !fdef.Has(hir.FlagVariadic) {
		return
	}
	base := len(args) - kResult
	// Arg-form requires every trailing kResult argument to be an identifier; a
	// non-ident trailing argument means this is an LHS-form call and the result
	// is bound by the assignment instead.
	for k := 0; k < kResult; k++ {
		an := l.pkg.Node(args[base+k])
		if an == nil || an.Kind != hir.KIdent {
			return
		}
	}
	for k := 0; k < kResult; k++ {
		ai := base + k
		if ai < 0 || ai >= len(args) || k >= len(dsts) {
			continue
		}
		an := l.pkg.Node(args[ai])
		if an == nil || an.Kind != hir.KIdent {
			continue
		}
		nm := l.pkg.Str(an.S)
		slot, ok := l.locals[nm]
		if !ok {
			continue
		}
		if dsts[k] == slot {
			// Self-move: the callee's result slot coincides with the caller's
			// out-parameter slot. A move slot->slot is a no-op; skip it to
			// avoid a redundant (and potentially unsafe) transfer.
			continue
		}
		l.b.EmitMoveInto(slot, dsts[k])
	}
}

// lowerAsyncRun lowers `run <expr>` (hir.KRun). The operand is either a call
// (lowered into an OpRun handle — `run` is synchronous-async for ANY callee,
// not just `-async`-suffixed ones) or an existing handle variable (`run f`
// where f already holds a handle) — in the latter case we simply return the
// handle value. Returns the opaque handle (i64).
func (l *lowerer) lowerAsyncRun(n *hir.Node) ValueID {
	child := n.First
	if child == hir.NoID {
		return NoVal
	}
	cn := l.pkg.Node(child)
	if cn != nil && cn.Kind == hir.KIdent {
		// run <handle-var>: operand already a handle; return it as-is.
		return l.lowerExpr(child)
	}
	// run <call>: force THIS call node to lower as OpRun, regardless of the
	// callee's name. The flag is scoped to the exact node id and restored
	// afterwards so argument sub-calls stay ordinary eager calls.
	if cn != nil && cn.Kind == hir.KCall {
		prev := l.forceRunCall
		l.forceRunCall = child
		v := l.lowerExpr(child)
		l.forceRunCall = prev
		return v
	}
	return l.lowerExpr(child)
}

// lowerAsyncAwait lowers `awy <expr>` (hir.KAwait) into an OpAwait that drives
// the task to completion and loads the result. The operand is either an
// `-async` call (lowered to a handle first) or a handle variable.
func (l *lowerer) lowerAsyncAwait(n *hir.Node) ValueID {
	child := n.First
	if child == hir.NoID {
		return NoVal
	}
	// awy <call>: same syntactic-async rule as run — a direct call operand is
	// launched as a task and then awaited (mirrors legacy generateAwaitForCoro
	// Case 1, which routes any direct call through createTaskAndEnqueue).
	h := NoVal
	if cn := l.pkg.Node(child); cn != nil && cn.Kind == hir.KCall {
		prev := l.forceRunCall
		l.forceRunCall = child
		h = l.lowerExpr(child)
		l.forceRunCall = prev
	} else {
		h = l.lowerExpr(child) // OpRun handle for a call, or the handle var
	}
	if h == NoVal {
		return NoVal
	}
	resTyp := l.b.Type("i64")
	if rt, ok := l.asyncResTypes[h]; ok {
		// 1. The handle value carries the task's result type (recorded at the
		//    run site). This is the only reliable source for `awy <handle-var>`.
		resTyp = rt
	} else if cn := l.pkg.Node(child); cn != nil && cn.Kind == hir.KCall {
		// 2. Direct call operand: derive the async result type from the callee.
		if callee, _ := l.resolveCallee(cn); callee != "" {
			if rt := l.resultTypeOfCallee(callee); rt != l.voidType && rt != NoType {
				resTyp = rt
			}
		}
	}
	return l.b.Emit(OpAwait, resTyp, []ValueID{h}, "")
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
		// The callee is not a registered HIR function. Before falling back to
		// void, check whether it is a known scalar method whose definition
		// lives in a std module that may not have been loaded (e.g. i64.to-str
		// is in number.no, which is loaded on-demand only when the `number`
		// module is explicitly referenced). The checker has the same special-
		// case table (checker.go §inferExprType). Without this fallback, the
		// call is mis-lowered as void and the consumer reads NoVal — the
		// silent-undef bug #85.
		if rt := scalarMethodResult(callee); rt != "" {
			return l.b.Type(rt)
		}
		return l.voidType
	}
	n := l.pkg.Node(id)
	if n == nil {
		return l.voidType
	}
	// For a method, the first KResult is `self` (the out-param receiver),
	// not a real return value.  Skip it so the result type is the actual
	// return value, not the receiver's type.
	isMethod := n.Has(hir.FlagMethod)
	firstSkipped := false
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KResult {
			if isMethod && !firstSkipped {
				firstSkipped = true
				continue // skip self
			}
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
	// For a method, the first KResult is `self` (the out-param receiver),
	// not a real return value.  Skip it.
	fnNode := l.pkg.Node(id)
	isMethod := fnNode != nil && fnNode.Has(hir.FlagMethod)
	firstSkipped := false
	for _, c := range l.pkg.Children(id) {
		cn := l.pkg.Node(c)
		if cn != nil && cn.Kind == hir.KResult {
			if isMethod && !firstSkipped {
				firstSkipped = true
				continue // skip self
			}
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
		l.lowerCallArgs(vn, recvV, callee)
		for _, t := range targets {
			l.bindPlaceholder(t)
		}
		return
	}

	argv := l.lowerCallArgs(vn, recvV, callee)
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
	argv := l.lowerCallArgs(inner, recvV, callee)

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
		l.lowerCallArgs(inner, recvV, callee)
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
// receiver when the call is a method call. `callee` is used to seed the type of
// any anonymous `{...}` literal argument from the callee's declared parameter
// types (see structLitTypes); pass "" when there is no meaningful callee.
// lowerEnumCtor lowers a tagged-enum variant constructor call (`full(7)`,
// `rect(2.0, 3.0)`). The payload arguments are evaluated in order and packed
// into the enum's shared payload area; the discriminant comes from the
// variant's declaration order.
func (l *lowerer) lowerEnumCtor(n *hir.Node, ei *TaggedEnumInfo, vi *VariantInfo) ValueID {
	enumT := l.b.Type(ei.Name)
	if enumT == l.voidType || enumT == NoType {
		return NoVal
	}
	argv := l.lowerCallArgs(n, NoVal, "")
	if len(argv) > len(vi.Fields) {
		// A unit variant written in call position (`green()`) carries no
		// payload; drop any stray (e.g. out-param) trailing arguments rather
		// than storing them past the declared fields.
		argv = argv[:len(vi.Fields)]
	}
	v := l.b.Emit(OpEnumNew, enumT, argv, "")
	inst := &l.mod.Insts[len(l.mod.Insts)-1]
	inst.Int = vi.Tag
	if l.inArmCond {
		// This constructor is a match-arm PATTERN. Record the variant so the
		// arm body's field bindings can be projected onto the payload.
		l.armEnum, l.armVariant = ei, vi
	}
	return v
}

// lowerEnumUnit lowers a bare unit-variant reference (`green` in
// `c color = green`): a constructor with no payload.
func (l *lowerer) lowerEnumUnit(ei *TaggedEnumInfo, vi *VariantInfo) ValueID {
	enumT := l.b.Type(ei.Name)
	if enumT == l.voidType || enumT == NoType {
		return NoVal
	}
	v := l.b.Emit(OpEnumNew, enumT, nil, "")
	inst := &l.mod.Insts[len(l.mod.Insts)-1]
	inst.Int = vi.Tag
	if l.inArmCond {
		// Same as lowerEnumCtor: a variant name in an arm condition is a
		// PATTERN, so record it for the arm body's field bindings.
		l.armEnum, l.armVariant = ei, vi
	}
	return v
}

func (l *lowerer) lowerCallArgs(n *hir.Node, recvV ValueID, callee string) []ValueID {
	args := l.slotArgs(n.Id, "arg")
	// For a VARIADIC callee in arg-form (`f(..., r)`), drop the trailing
	// out-parameter arguments: the result is returned via the out-pointer, not
	// as a normal LLVM argument. If left in, emitCallBody's variadic spread packs
	// them into the %vec (corrupting the slice and dropping the out-pointer) —
	// e.g. `number.max(10, 20, r)` would bundle `r`'s value into the variadic
	// slice and never write the result back. Detect arg-form by the trailing
	// kResult HIR arguments being identifiers (LHS-form `r = f(...)` omits them
	// entirely, so it is left untouched). Restricted to variadic callees so
	// non-variadic / method out-parameters (e.g. `str.copy = () (dst str)`) keep
	// their existing, working path.
	if fid, ok := l.funcNames[callee]; ok {
		if fdef := l.pkg.Node(fid); fdef != nil && fdef.Has(hir.FlagVariadic) {
			if kResult := l.countResultParams(callee); kResult > 0 && len(args) >= kResult {
				isArgForm := true
				for k := 0; k < kResult; k++ {
					an := l.pkg.Node(args[len(args)-kResult+k])
					if an == nil || an.Kind != hir.KIdent {
						isArgForm = false
						break
					}
				}
				if isArgForm {
					args = args[:len(args)-kResult]
				}
			}
		}
	}
	var argv []ValueID
	argOffset := 0
	// Only the OUTERMOST print-family call wraps its arguments in to-str. The
	// flag is cleared while each argument expression is lowered: `inPrintArgs`
	// alone is not enough, because it stays >0 for the whole subtree, so a
	// nested method call such as `print(b.to-str())` saw the flag and wrapped
	// `b.to-str()`'s own RECEIVER — lowering the source to
	// `print(b.to-str().to-str())`. The outer to-str then received a str and
	// produced garbage (`[9, 9, <address>]` for a `[3]i64`).
	wrapArgs := l.wrapPrintArgs
	l.wrapPrintArgs = false
	defer func() { l.wrapPrintArgs = wrapArgs }()
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
			argOffset = 1
		}
	}
	// Seed anonymous struct-literal argument types from the callee's declared
	// parameter types before lowering, so lowerStructLit can emit a typed
	// structlit (see structLitTypes).
	paramRaws := l.paramRawTypesOfCallee(callee)
	for i, a := range args {
		if pi := i + argOffset; pi < len(paramRaws) {
			l.noteAnonymousStructLit(a, paramRaws[pi])
		}
	}
	for i, a := range args {
		// The EXPECTED TYPE at an argument position is the CALLEE's declared
		// parameter type — NOT the enclosing statement's hint. Letting the
		// statement hint through leaked an OPTION type into plain-`T`
		// parameters: in `r = fs.read-str(files[fi])` the binding `r` is
		// inferred `?str` (read-str returns ?str), that hint reached the
		// `files[fi]` argument, and the index then lowered as a SAFE index
		// yielding `?str`, which was stored into read-str's plain-`str`
		// parameter slot — opt-verify: "%lv defined with type '%option' but
		// expected '%str-long'". Seeding from paramRaws also covers the
		// tagged-enum case this code originally existed for: `area(rect(2.0,
		// 3.0))` only knows `rect` belongs to `shape` from the parameter's
		// declared type (tests/tagged-enum.no).
		saved := l.typeHint
		if pi := i + argOffset; pi < len(paramRaws) {
			if raw := paramRaws[pi]; raw != "" {
				if t := l.b.Type(raw); t != NoType && t != l.voidType {
					l.typeHint = t
				}
			}
		}
		v := l.lowerExpr(a)
		l.typeHint = saved
		if wrapArgs {
			v = l.printableValue(v)
		}
		argv = append(argv, v)
	}
	return argv
}

// printableValue renders a container-typed print-family argument through its
// receiver's `to-str` method, mirroring the legacy backend (which lowers
// `print(v)` / `print(a[0..2])` to a `[]t.to-str` call).
//
// Without it the argument reaches codegen as a raw `%vec` / fixed array and
// print fails with "print unsupported arg type %vec"
// (tests/slice-heavy.no). Scalar types (including `str`, whose Kind is
// KindStr rather than KindSlice) are returned untouched.
func (l *lowerer) printableValue(v ValueID) ValueID {
	if v == NoVal {
		return v
	}
	t := l.valueTypeOf(v)
	ty := l.mod.Type(t)
	if ty == nil || (ty.Kind != KindSlice && ty.Kind != KindArray) {
		return v
	}
	if callee := l.toStrCalleeFor(ty); callee != "" {
		l.enqueueCallee(callee)
		if dsts := l.b.EmitCallMulti([]TypeID{l.b.Type("str")}, []ValueID{v}, callee); len(dsts) > 0 {
			return dsts[0]
		}
	}
	return v
}

// toStrCalleeFor returns the `to-str` callee registered for a slice/array
// receiver type, or "" when none exists (the argument is then printed as-is,
// which codegen will report rather than silently mis-render).
//
// Generic std methods are monomorphized by the front end under a mangled name
// (`[]t.to-str` on a []i64 becomes `_xi64.to-str`, `[n]t.to-str` on a [4]i64
// becomes `_4xi64.to-str`), so the mangled forms must be tried BEFORE the
// generic template names — a call to the bare template has no body and would
// be reported as an undefined callee.
func (l *lowerer) toStrCalleeFor(ty *Type) string {
	elem := ""
	if ty.Elem != NoType {
		if et := l.mod.Type(ty.Elem); et != nil {
			elem = et.Raw
		}
	}
	raw := strings.TrimPrefix(ty.Raw, "?")
	var cands []string
	if elem != "" {
		if closeB := strings.IndexByte(raw, ']'); closeB > 0 {
			size := raw[1:closeB]
			if size != "" {
				cands = append(cands, "_"+size+"x"+elem+".to-str")
			}
		}
		cands = append(cands, "_x"+elem+".to-str", "[]"+elem+".to-str")
	}
	cands = append(cands, "[]t.to-str", "[n]t.to-str")
	for _, c := range cands {
		if _, ok := l.funcNames[c]; ok {
			return c
		}
	}
	return ""
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
		if v == existing {
			// Self-assignment (`it = it`): the RHS lowered to the same slot as
			// the target. Skipping the drop+move avoids freeing already-freed
			// memory (double free / use-after-free). See lowerAssignNode /
			// lowerLet for the rationale.
			return
		}
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
// trimViewPrefix strips the option and view markers from a receiver type name
// so a method call through a view resolves against the BORROWED type:
// `?&point` / `&point` -> `point`. A view is `T*` at the LLVM level and a
// method's `self` parameter is also `T*`, so the receiver passes through
// unchanged — only the callee NAME needs the strip.
func trimViewPrefix(raw string) string {
	raw = strings.TrimPrefix(raw, "?")
	raw = strings.TrimPrefix(raw, "&")
	return raw
}

// isViewRaw reports whether a nolang type string is a VIEW type (`&T`) or an
// option of one (`?&T`). Views are non-owning borrows: they are never dropped
// (ClassifyOwnership returns false) and are represented as `T*` in LLVM.
func isViewRaw(raw string) bool {
	raw = strings.TrimPrefix(raw, "?")
	return strings.HasPrefix(raw, "&")
}

// typeRaw returns the nolang type string of a MIR type id ("" when unknown).
func (l *lowerer) typeRaw(t TypeID) string {
	if t == NoType {
		return ""
	}
	if ty := l.mod.Type(t); ty != nil {
		return ty.Raw
	}
	return ""
}

// viewTargetType returns the type ID when `name` is a VIEW-typed binding
// (already declared, or declared on this very `let`), and NoType otherwise.
// Views are `&T` / `?&T` and are bound by borrow, not by copy.
func (l *lowerer) viewTargetType(name string, n *hir.Node) TypeID {
	if ex, ok := l.locals[name]; ok {
		if t := l.valueTypeOf(ex); t != NoType && isViewRaw(l.typeRaw(t)) {
			return t
		}
	}
	if n == nil {
		return NoType
	}
	if dt := l.letDeclaredType(n); dt != NoType && dt != l.voidType {
		if dty := l.mod.Type(dt); dty != nil && isViewRaw(dty.Raw) {
			return dt
		}
	}
	return NoType
}

// lowerBorrow lowers the `&x` half of a view binding: it emits an OpBorrow on
// the value `x` named by the HIR node id, yielding a `&T` pointer to x's
// storage. Returns NoVal when the node is not a borrowable local/parameter
// reference (the checker has already rejected such a binding).
func (l *lowerer) lowerBorrow(id int32) ValueID {
	n := l.pkg.Node(id)
	if n == nil || n.Kind != hir.KIdent {
		return NoVal
	}
	nm := l.pkg.Str(n.S)
	// A view's lifetime is the RECEIVER's: it may only bind to `self`. Binding
	// it to any other local would leave it dangling the moment the method
	// returns, which is exactly what "view 只綁定 self" rules out.
	if nm != "self" {
		l.unsupported(l.curFuncName(), "view", "a view can only bind to 'self', not '"+nm+"' (the borrow would outlive it)")
		return NoVal
	}
	src, ok := l.locals[nm]
	if !ok {
		return NoVal
	}
	st := l.valueTypeOf(src)
	if st == NoType || st == l.voidType {
		return NoVal
	}
	// The borrow's type is `&T` where T is the SOURCE's type. Strip a leading
	// `?` so binding to an optional view (`v ?&point = self`) yields `&point`
	// (the option wrap is applied afterwards) rather than `&?point`.
	raw := strings.TrimPrefix(l.typeRaw(st), "?")
	if raw == "" {
		return NoVal
	}
	// A source that is ALREADY a view (e.g. the peeled payload of a `?&T`, or
	// a `&T` local) is already a pointer — borrowing it again would produce a
	// `&&T` and point at the view's own slot instead of the borrowed value.
	// Leave those to the normal (copy / alias) path.
	if strings.HasPrefix(raw, "&") {
		return NoVal
	}
	return l.borrowValue(src)
}

// borrowValue emits an OpBorrow on an already-lowered value, yielding a `&T`
// pointer to that value's storage.
//
// It is the generalized form of lowerBorrow: that one only ever borrows `self`
// (a method-result view, whose lifetime is the receiver's), whereas a STRUCT
// FIELD declared `f &T` borrows whatever value the field is initialized or
// assigned from. Returns NoVal when the value has no addressable storage or is
// already a pointer, in which case the caller falls back to the copy path.
func (l *lowerer) borrowValue(v ValueID) ValueID {
	if v == NoVal {
		return NoVal
	}
	st := l.valueTypeOf(v)
	if st == NoType || st == l.voidType {
		return NoVal
	}
	raw := strings.TrimPrefix(l.typeRaw(st), "?")
	if raw == "" || strings.HasPrefix(raw, "&") {
		return NoVal
	}
	return l.b.Emit(OpBorrow, l.b.Type("&"+raw), []ValueID{v}, "")
}

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
		return l.elemTypeOfType(ty)
	}
	return l.voidType
}

// elemTypeOfType is the type-level half of elementTypeOf: it maps a container
// type to the type produced by a single index operation.
func (l *lowerer) elemTypeOfType(ty *Type) TypeID {
	if ty != nil {
		// An option wrapping a container is indexed THROUGH its payload. A
		// match arm binds `it` to the option itself, so `it[0]` on the `?[]byte`
		// returned by `'6162'.from-hex()` must unwrap twice: `?[]byte` ->
		// `[]byte` -> `byte`. Without this the index result was typed `[]byte`
		// and codegen GEP'd into the option struct itself, which opt rejects
		// with "invalid getelementptr indices" (tests/strconv.no,
		// test-from-hex-even).
		if ty.Kind == KindOption && ty.Elem != NoType {
			if et := l.mod.Type(ty.Elem); et != nil {
				return l.elemTypeOfType(et)
			}
			return ty.Elem
		}
		// A str is `{len(bytes), cap, i8* data}`; `s[i]` reads the i-th CODE
		// POINT as a nolang `char` (docs/docs/lang/str.md), not a byte. char
		// lowers to i32 (KindChar -> i32) — a code point fits in 21 bits — so
		// the i64 the UTF-8 runtime returns is truncated into the i32 slot by
		// emitStrCpIndex. The raw type MUST be "char" and not an integer
		// spelling: it is the only carrier of the code-point identity, and it
		// is what codegen.rawTypeOfValue consults to pick @str_from_cp (UTF-8
		// encoding) over @str_from_i64 (decimal text) for the implicit
		// `a str = s[0]` conversion — otherwise that printed "104" for "h".
		//
		// The BYTE identity is preserved on the write side: `s[i] = v` goes
		// through OpIndexStore, whose element width comes from
		// codegen.elemTypeOfReceiver (str -> i8), not from here.
		if ty.Raw == "str" {
			return l.b.Type("char")
		}
		// A txt is `{ [255 x i8], i8 }`; indexing a txt yields a single byte
		// (i8), just like indexing a str. Without this, `t[0]` on a txt-typed
		// value lowered to void and codegen failed with "index dst slot
		// (type=void)" (tests/txt.no).
		if ty.Raw == "txt" {
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
			} else if _, isGlobal := l.globals[nm]; isGlobal {
				// Target is a module-level binding, not a function local.
				// The type hint matters here too: a global `px2 f64 = 0.0`
				// has no local slot, and without the hint a void-typed RHS
				// would make the store a no-op.
				if gv := l.lowerGlobalRef(nm); gv != NoVal {
					if t := l.valueTypeOf(gv); t != NoType && t != l.voidType {
						l.typeHint = t
					}
				}
			}
		}
	} else if tn.Kind == hir.KDot {
		// Field assignment `.field = with-cap(n)`: the LHS-inferred builtin
		// needs the FIELD's type as a hint, not the receiver's. Without this,
		// `.hs-buf = with-cap(65536)` inside a method lowers the call to void
		// (no typeHint set), the field gets NoVal, and codegen fails with
		// "builtin with-cap: no result slot" (tests/net-http.no, which
		// pulls in tls.no's conn.init).
		//
		// For implicit-self field assignments (`.keys = with-len(16)` inside
		// a method body), the KDot target has NO child node — the receiver is
		// the method's implicit `self`/`curRecv`. Without this fallback, the
		// typeHint is never set and `with-len` lowers to void (NoVal Dst),
		// causing "builtin with-len: no result slot" at codegen. This affects
		// all hashmap methods (init/rehash/clear) that use `with-len` to
		// allocate keys/vals/occ slices (tests/map.no, basic.no).
		fieldName := l.pkg.Str(tn.S)
		if fieldName != "" {
			var recvID int32
			for _, c := range l.pkg.Children(target) {
				recvID = c
				break
			}
			var recvV ValueID
			if recvID != hir.NoID {
				recvV = l.lowerExpr(recvID)
			} else {
				// Implicit self receiver: .field = ... inside a method body.
				recvV = l.curRecv
				if recvV == NoVal {
					recvV = l.locals["self"]
				}
			}
			if recvV != NoVal {
				if ft := l.fieldTypeOf(recvV, fieldName); ft != NoType && ft != l.voidType {
					l.typeHint = ft
				}
			}
		}
	} else if tn.Kind == hir.KIndex {
		// Indexed assignment `a[i] = with-len(n)`: the LHS-inferred builtin
		// needs the ELEMENT type as a hint. Without this, `.keys[cnt] = with-len(...)`
		// inside json.no lowers the call to void (no typeHint), and codegen fails
		// with "builtin with-len: no result slot" (tests/mem-safety/json-nested-match.no).
		var arrID int32
		for _, c := range l.pkg.Children(target) {
			arrID = c
			break
		}
		if arrID != hir.NoID {
			arrV := l.lowerExpr(arrID)
			if arrV != NoVal {
				if et := l.elementTypeOf(arrV); et != NoType && et != l.voidType {
					l.typeHint = et
				}
			}
		}
	}
	// 整數字面量指派給無號型別的綁定：常數轉換（`b = -1`（b 為 byte）存 255 而非 -1）。
	// 與 KLet 分支同源，只對「整個右側就是（負）整數字面量」生效。
	var v ValueID = NoVal
	if tn.Kind == hir.KIdent && value != hir.NoID {
		if nm := l.pkg.Str(tn.S); nm != "" {
			if raw, ok := l.localRaw[nm]; ok {
				v, _ = l.tryUnsignedLitFold(value, raw)
			}
		}
	}
	// VIEW binding (`&T` / `?&T`): assigning to a view-typed target BORROWS the
	// source's storage instead of copying it. `json.get = (key str) (child ?&json)`
	// binds `child` to `self` — no struct copy, no heap allocation, and nothing
	// to free (ClassifyOwnership reports `&T` as not owned, so the analysis
	// inserts no drop). The checker restricts the source to `self` (or a
	// self-rooted lvalue), which is what keeps the borrow from dangling.
	if v == NoVal && tn.Kind == hir.KIdent && value != hir.NoID {
		if nm := l.pkg.Str(tn.S); nm != "" {
			if slot, ok := l.locals[nm]; ok {
				if t := l.valueTypeOf(slot); t != NoType && isViewRaw(l.typeRaw(t)) {
					if bv := l.lowerBorrow(value); bv != NoVal {
						v = bv
					}
				}
			}
		}
	}
	if v == NoVal {
		v = l.lowerExpr(value)
	}
	l.typeHint = NoType
	if v == NoVal {
		return NoVal
	}
	switch tn.Kind {
	case hir.KIdent:
		nm := l.pkg.Str(tn.S)
		// Keep the arm-projection source in sync: after the first arm has
		// bound `it`, later arms re-assign it (same slot) rather than
		// declaring it, so a `let`-only update missed every arm but the first
		// and projected the field names onto the FIRST arm's subject
		// (tests/tagged-enum.no printed an empty string for `b-res`).
		if nm == "it" && v != NoVal {
			l.itSrc = v
		}
		slot, ok := l.locals[nm]
		if !ok {
			// The target may be a module-level binding (a script-level
			// `px2 = 0.0` with a constant initializer stays a module global
			// rather than being inlined into the synthetic `main`). Reading
			// such a global already works (lowerIdent -> lowerGlobalRef), but
			// the STORE was missing: the old code reported "assignment to
			// unresolved identifier" and dropped the value, so a loop like
			// `px2 = px2 + mi * vi` silently kept its initial value and every
			// iteration recomputed from zero (test-tmp-nbody-debug printed
			// only the last term). Resolve the global and move into it.
			if _, isGlobal := l.globals[nm]; isGlobal {
				if slot, ok := l.globals[nm]; ok && slot == v {
					// Self-assignment to a module global: no-op.
					return v
				}
				gv := l.lowerGlobalRef(nm)
				if gv == NoVal {
					l.unsupported(l.curFuncName(), "assign", "assignment to unresolvable global "+nm)
					return NoVal
				}
				if t := l.valueTypeOf(gv); t == NoType || t == l.voidType {
					l.unsupported(l.curFuncName(), "assign", "assignment to void-typed global "+nm)
					return NoVal
				}
				l.b.EmitMoveInto(gv, v)
				return v
			}
			l.unsupported(l.curFuncName(), "assign", "assignment to unresolved identifier "+nm)
			return NoVal
		}
		if slot == v {
			// Self-assignment (`it = it`): the target and the value are the
			// same owned slot, so no ownership transfer occurs. Emitting an
			// OpDrop here would free the slot's current content, and the
			// following EmitMoveInto(slot, v) (== move slot->slot) would then
			// move already-freed memory, i.e. a double free / use-after-free
			// (observed as a runtime SIGTRAP in str.replace-n). The memory
			// analysis inserts the single correct exit-drop for this slot,
			// so skipping the manual drop+move entirely is sound.
			return v
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
		// Implicit `ok(v)` wrap: assigning a non-option value `v` to an
		// option-typed local (`val ?bool = true`) must build the option
		// {tag=0, payload=v}, NOT move the bare scalar into the option slot
		// (which would overwrite the discriminant, turning `ok(true)` into
		// `nil` and corrupting `== nil` / `== err` comparisons and prints).
		// A value that is ALREADY an option (e.g. `val = otherOpt` / `val =
		// nil` / `val = err(...)`) is left as-is. This mirrors the wrap already
		// done for `let` declarations (lowerStmt ~1368); this assignment path
		// was missing it (tests/test-bool-debug, test-bool-direct,
		// test-min-u8-bool.minimal and the §16 bool-print family).
		v = l.wrapOptionIfNeeded(l.valueTypeOf(slot), v)
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
		// Implicit `ok(v)` wrap for `a[i] = scalar` where `a` is a `[]?T` (same
		// rule as the KIdent reassignment path above).
		if eT := l.indexElemType(arrV); eT != NoType {
			v = l.wrapOptionIfNeeded(eT, v)
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
		var recvV ValueID
		if recvID != hir.NoID {
			recvV = l.lowerExpr(recvID)
		} else {
			// Implicit self receiver: .field = ... inside a method body.
			recvV = l.curRecv
			if recvV == NoVal {
				recvV = l.locals["self"]
			}
		}
		if recvV == NoVal {
			return NoVal
		}
		fieldName := l.pkg.Str(tn.S)
		// Store the field name in inst.Str (not inst.Sym) so emitSetField's
		// FieldIndex lookup matches the GETFIELD convention in lowerDotRead.
		sid := l.b.EmitVoid(OpSetField, []ValueID{recvV, v}, "")
		l.mod.Insts[sid].Str = fieldName
		// The field assignment consumes the RHS value: ownership of an owned
		// RHS (str/vec/option) transfers into the struct field, so the value's
		// temporary must NOT be dropped separately — that would free a buffer
		// the struct still points to (use-after-free -> SIGSEGV). This mirrors
		// lowerStructLit's MovesArg; without it, `.keys = with-len(n)` inside
		// hashmap.rehash drops the freshly allocated slice immediately after
		// the setfield, and every subsequent index/getfield on it crashes
		// (tests/map.no).
		l.mod.Insts[sid].MovesArg = true
		return v
	default:
		l.unsupported(l.curFuncName(), "assign", "unsupported assign target kind "+hir.KindNames[tn.Kind])
		return NoVal
	}
}

// wrapOptionIfNeeded implements nolang's implicit `ok(v)` wrap on assignment: a
// non-option value `v` assigned to an option-typed target (`?T`) must be built
// into the option {tag=0, payload=v}, never moved in as a bare scalar (which
// would clobber the discriminant and turn `ok(true)` into `nil`, corrupting
// `== nil` / `== err` comparisons and `print`). A `v` that is ALREADY an option
// (reassignment of an option, `nil`, `err(...)`) is returned unchanged.
func (l *lowerer) wrapOptionIfNeeded(targetType TypeID, v ValueID) ValueID {
	if targetType == NoType || targetType == l.voidType {
		return v
	}
	tt := l.mod.Type(targetType)
	if tt == nil || tt.Kind != KindOption {
		return v
	}
	vt := l.valueTypeOf(v)
	if vt != NoType && vt != l.voidType {
		if vty := l.mod.Type(vt); vty != nil && vty.Kind == KindOption {
			return v // already an option — leave as-is
		}
	}
	return l.b.EmitOptionWrap(targetType, 0, v)
}

// indexElemType returns the element type of a slice/array value (used to apply
// the implicit `ok(v)` wrap when storing into a `[]?T` / `[N]?T`).
func (l *lowerer) indexElemType(arrV ValueID) TypeID {
	at := l.mod.Type(l.valueTypeOf(arrV))
	if at == nil {
		return NoType
	}
	if at.Kind == KindSlice || at.Kind == KindArray {
		return at.Elem
	}
	return NoType
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

// isArithOrBitwiseOp reports whether op is an integer arithmetic / bitwise
// infix operator (i.e. NOT a comparison and NOT a string op), used by the
// narrow-result widening rule in lowerExpr's KInfix case.
// isArithOrBitwiseOp reports whether op is a numeric/bitwise operator.
func isArithOrBitwiseOp(op Op) bool {
	switch op {
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpUDiv, OpUMod, OpBitAnd, OpBitOr, OpXor, OpShl, OpShr:
		return true
	}
	return false
}

// singleCharStrByte returns the byte value of a one-rune string literal
// (`"A"` -> 65). In arithmetic context nolang treats a single-character string
// literal as a BYTE, not a string — legacy's isStringExpr returns false for it,
// which is why `"A" + 1` is 66 and not "A1" (tests/str-ops.no). Returns
// false for the empty string and for multi-rune literals, which stay strings.
// charLitCode returns the code point of a char literal node's text. The raw
// text may or may not still carry quotes (`'B'`, `"B"` or bare `B` depending on
// where in the pipeline it was produced), so strip both quote styles and take
// the first rune — mirroring lowerCharLit.
func charLitCode(s string) (int64, bool) {
	t := strings.Trim(s, "'\"")
	for _, r := range t {
		return int64(r), true
	}
	return 0, false
}

func singleCharStrByte(s string) (int64, bool) {
	rs := []rune(s)
	if len(rs) != 1 {
		return 0, false
	}
	return int64(rs[0]), true
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
