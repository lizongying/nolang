package mir

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/builtin"
)

// ---------------------------------------------------------------------------
// MIR -> LLVM codegen (v1: core subset + self-contained minimal runtime)
//
// Design goals:
//   - Every MIR value is backed by an LLVM alloca slot, so ownership / drop are
//     trivial: a Drop lowers to `call void @str_free(%str-long loaded-from-slot)`.
//     Exactly-one-drop is guaranteed by the analysis, which eliminates the entire
//     double-free class that plagues the 450KB emitter.
//   - Owned types (str, vec, ...) are passed/returned BY POINTER (sret-like);
//     scalars by value. This mirrors the legacy ABI and sidesteps SSA/phi for
//     owned merges across branches.
//   - A self-contained minimal runtime (%str-long, @malloc/@free, @str_concat,
//     @str_from_const, @digits, @print_*) is emitted into every module, so the
//     output is a complete, linkable LLVM IR text independent of the legacy
//     2.6MB runtime.
//
// Unsupported ops / types return an error so the transpiler can fall back to the
// proven HIR path (strangler-fig).
// ---------------------------------------------------------------------------

type strGlobal struct {
	name string
	n    int
	str  string
}

type codegen struct {
	mod  *Module
	sb   strings.Builder
	errs []string

	fname       map[FuncID]string // MIR func id -> llvm function name
	labelFor    map[BlockID]string
	valSlot     map[ValueID]string // MIR value -> alloca name
	globalSlots map[ValueID]string // module-global ValueID -> "@name" (preserved across functions)
	paramPtr    map[ValueID]string // MIR param value -> incoming llvm param name
	resultParam map[ValueID]bool   // MIR value is an out-param
	strGlobals  map[ValueID]strGlobal
	cf          FuncID
	curFn       *Function // function currently being emitted (for param ownership checks)
	loadSeq     int       // unique suffix for materialized load registers

	// mirSliceCopyEmitted tracks whether @mir_slice_copy has been emitted.
	mirSliceCopyEmitted bool

	// externals discovered during emission: a CLibCall may reach a C symbol
	// that the prelude does not know about, so declarations are accumulated
	// here and appended to the module once every function has been emitted
	// (LLVM module order is irrelevant, forward references are legal).
	extDecls     map[string]bool
	extDeclOrder []string
	extraGlobals []string
	strConsts    map[string]string // dedupe string-constant globals by content

	// extraFuncs / extraFuncsBody hold helper functions that emission discovers
	// while lowering a function body but that must NOT be spliced into it.
	// Phase 1's lazy-pointee accessors (`@__nolang_get_<T>`) need their own
	// basic blocks, and codegen cannot open new blocks inside a function body
	// (blocks come from iterating f.Blocks) — so the body is buffered here and
	// flushed once every function has been emitted. extraFuncs is the
	// already-emitted set, so a helper is written exactly once.
	extraFuncs     map[string]bool
	extraFuncsBody strings.Builder

	// sanIdx maps a sanitized LLVM struct name (`json_json_pool`) back to its
	// StructFields key (`json.json-pool`), built lazily by sanitizedStructKey.
	//
	// WHY IT IS NEEDED: sanitize() maps EVERY non-alphanumeric rune to '_', so
	// both '.' and '-' collapse and the mapping is many-to-one. unsanitize()
	// only tries the dotted form, so any key containing '-' (e.g.
	// `json.json-pool`) is unreachable that way. computeTypeSize then reports
	// the type as unsizable, which silently disables shouldUseMemcpy for it.
	// Ambiguous collisions are omitted (see sanitizedStructKey), so lookups stay
	// deterministic and never more aggressive than before.
	sanIdx map[string]string

	// async task runtime: per-callee wrapper define blocks (callee raw name ->
	// wrapper LLVM name) and a monotonic sequence counter for fresh names.
	asyncWrappers   map[string]string
	asyncWrapperSeq int

	// optPrintHelper maps an option PAYLOAD LLVM type to the dedicated print
	// helper function name (e.g. @print_option_str) that performs the nil-tag
	// check and prints "nil" or the payload inside its OWN function body.
	// Routed through from emitCall's print special-case so that no new basic
	// blocks are emitted mid-function (which breaks LLVM verification).
	optPrintHelper map[string]string

	// optBoxRebox maps a BOXED option payload LLVM type to the re-box helper
	// `@__nolang_opt_rebox_<key>`, which gives a freshly copied option its own
	// heap block. Without it a 32-byte copy of a boxed option would alias the
	// source's payload — something the old inline layout never did.
	optBoxRebox map[string]string

	// optSlotBytes / optSlotLT are the payload slot's size and its LLVM spelling
	// (`[N x i64]`), resolved per module by initOptionSlot from the configured
	// option-inline-threshold (default 24, minimum 8).
	optSlotBytes int64
	optSlotLT    string

	// optBoxZero maps a BOXED option payload LLVM type to a private zero-filled
	// global of that type, `@__nolang_opt_zero_<key>`. It is what
	// optPayloadAddr returns for a non-ok tag — see there for why pointing at
	// the slot instead is not an option.
	optBoxZero map[string]string

	// defInst maps a MIR value to the instruction that defined it (by InstID),
	// so emitCallBody can detect when a method-call receiver is the result of an
	// OpGetField. In that case the callee receives the struct field's GEP
	// address directly (so mutations propagate back to the struct), instead of
	// a pointer to a temporary alloca slot (which silently discards writes).
	// This is the `m.rows.push(x)` case: without it, push modifies a copy of
	// m.rows and the struct field stays unchanged.
	defInst map[ValueID]InstID
}

// ptype returns the LLVM type string for a MIR value and whether it is owned.
// elemToStrMethod maps an LLVM element type (as produced by loadVal for the
// argument of `number.i64-to-str` / `i64-to-str`) to the nolang `.to-str`
// method that should format it. Returns "" when the native i64_to_str handles
// the type (i64) or when no dedicated method exists (str/i1). The target
// methods are std methods enqueued by hir2mir for the generic container
// to-str templates, so they are guaranteed to be present in the module.
func (c *codegen) elemToStrMethod(lt string) string {
	base := strings.TrimPrefix(lt, "%")
	switch base {
	case "i64", "str", "i1":
		return ""
	case "double":
		return "f64.to-str"
	case "float":
		return "f32.to-str"
	case "txt":
		return "txt.to-str"
	}
	if strings.HasPrefix(base, "i") || strings.HasPrefix(base, "u") ||
		base == "byte" || base == "char" {
		return base + ".to-str"
	}
	return ""
}

func (c *codegen) ptype(v ValueID) (llvm string, owned bool) {
	if c.cf != NoFunc {
		if f := c.mod.Func(c.cf); f != nil {
			if t, ok := f.LocalTypes[v]; ok {
				if ty := c.mod.Type(t); ty != nil {
					return c.llvmTypeOf(ty), ty.Owned
				}
			}
		}
	}
	if val := c.mod.Value(v); val != nil {
		if ty := c.mod.Type(val.Type); ty != nil {
			return c.llvmTypeOf(ty), ty.Owned
		}
	}
	return "i64", false
}

func (c *codegen) fail(format string, args ...interface{}) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

// optionElemKind returns the nolang element Kind of an option-typed value (the
// Kind of T for a ?T), or KindUnknown if the value isn't a (flat) option or the
// element type is unavailable. Used at print time to route a flat `%option` to a
// type-aware printer — e.g. `?bool` must print "true"/"false", not the i64
// payload "1"/"0" that the generic @print_option would emit. The LLVM `%option`
// type is the same for every scalar option (?i64, ?bool, ?u8, ...) so the element
// Kind can only be recovered from the nolang type table.
func (c *codegen) optionElemKind(v ValueID) TypeKind {
	var tid TypeID
	if c.cf != NoFunc {
		if f := c.mod.Func(c.cf); f != nil {
			if t, ok := f.LocalTypes[v]; ok {
				tid = t
			}
		}
	}
	if tid == NoType {
		if val := c.mod.Value(v); val != nil {
			tid = val.Type
		}
	}
	if tid == NoType {
		return KindUnknown
	}
	ty := c.mod.Type(tid)
	if ty == nil || ty.Kind != KindOption {
		return KindUnknown
	}
	if ty.Elem == NoType {
		return KindUnknown
	}
	et := c.mod.Type(ty.Elem)
	if et == nil {
		return KindUnknown
	}
	return et.Kind
}

func supportedLLVM(lt string) bool {
	switch lt {
	case "i64", "double", "i1", "void", "%str-long", "i8", "%txt", "%vec", "%option":
		return true
	}
	// Fixed arrays of supported element types are emitted as [N x elem]; the
	// element type check is enforced at index/store emission time (owned-element
	// arrays fall back to the legacy path there).
	if len(lt) > 0 && lt[0] == '[' {
		return true
	}
	// Named struct types (user structs and std structs such as fs.file,
	// err.error) are laid out by emitStructTypes and supported by OpGetField/
	// OpSetField. %vec and %option are now first-class: their by-value codegen
	// (alloca/load/store/arith/field) is handled generically via ptype, and their
	// drops are implemented in emitDrop (vec_free / option element free).
	if len(lt) > 2 && lt[0] == '%' {
		return true
	}
	return false
}

// isScalarLLVM reports whether an LLVM type is a non-pointer scalar (i64, i1,
// double, i8). Such parameters are passed BY VALUE in the MIR call ABI; every
// other type (aggregate / pointer / struct) is passed BY POINTER.
func isScalarLLVM(lt string) bool {
	switch lt {
	case "i64", "i1", "double", "i8":
		return true
	}
	return false
}

// byPointerLLVM reports whether a parameter / argument of LLVM type lt must be
// passed BY POINTER in the function signature rather than by value. Scalars and
// already-pointer types are passed by value; every aggregate (fixed array
// `[N x T]`, struct / %vec / %option / named struct) is passed by pointer so the
// MIR body — which GEPs c.valSlot / c.paramPtr as a pointer for indexing and
// field access — always sees a legal pointer. Owned values are already passed by
// pointer (the caller owns the buffer); this extends that to non-owned aggregates.
//
// Without this, a non-owned fixed-array receiver (e.g. `[3 x i64] %p0`) is
// declared by value while the body emits `getelementptr [3 x i64], [3 x i64]*
// %p0`, and opt rejects it ("%p0 defined with type '[3 x i64]' but expected
// 'ptr'"). See tests/test-arr.no.
func byPointerLLVM(lt string, owned bool) bool {
	if owned {
		return true
	}
	if strings.HasSuffix(lt, "*") {
		return false // already a pointer; pass by value
	}
	if strings.HasPrefix(lt, "[") {
		return true // fixed array aggregate
	}
	if strings.HasPrefix(lt, "%") {
		return true // struct / %vec / %option / named struct aggregate
	}
	return false
}

// isViewValue reports whether v is a VIEW (`&T` / `?&T`) — a non-owning
// borrow. Used to gate the view-specific codegen paths so no other receiver
// shape can fall into them by accident.
func (c *codegen) isViewValue(v ValueID) bool {
	raw := ""
	if f := c.mod.Func(c.cf); f != nil {
		if t, ok := f.LocalTypes[v]; ok {
			if ty := c.mod.Type(t); ty != nil {
				raw = ty.Raw
			}
		}
	}
	if raw == "" {
		if val := c.mod.Value(v); val != nil {
			if ty := c.mod.Type(val.Type); ty != nil {
				raw = ty.Raw
			}
		}
	}
	raw = strings.TrimPrefix(raw, "?")
	return strings.HasPrefix(raw, "&")
}

func sizeOfArray(t *Type) int64 {
	if len(t.Sizes) > 0 {
		return t.Sizes[0]
	}
	return 0
}

// isFuncParam reports whether v is an input/result parameter of f. Parameters are
// borrowed by the callee (the caller retains ownership and drops them), so a move
// OUT of a parameter cannot transfer heap ownership — it must clone.
func isFuncParam(f *Function, v ValueID) bool {
	for _, p := range f.Params {
		if p == v {
			return true
		}
	}
	return false
}

// EmitLLVM lowers the analyzed MIR module into a complete, linkable LLVM IR
// string. It returns an error if any construct is outside the v1 subset, so the
// caller can fall back to the legacy codegen.
//
// Panic safety: the v1 codegen still has gaps (field access / array / vec /
// option paths can hit a nil dereference on constructs outside the supported
// subset). Any such panic is recovered and converted into an error so the
// caller's strangler-fig fallback (NOLANG_MIR=2) always triggers — MIR can
// never crash the compiler; it only ever gracefully degrades to the legacy
// path.
func (m *Module) EmitLLVM() (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = ""
			err = fmt.Errorf("MIR->LLVM panicked (recovered, falling back to legacy): %v", r)
		}
	}()
	c := &codegen{
		mod:            m,
		fname:          map[FuncID]string{},
		labelFor:       map[BlockID]string{},
		valSlot:        map[ValueID]string{},
		paramPtr:       map[ValueID]string{},
		resultParam:    map[ValueID]bool{},
		strGlobals:     map[ValueID]strGlobal{},
		extDecls:       map[string]bool{},
		optPrintHelper: map[string]string{},
		optBoxRebox:    map[string]string{},
		optBoxZero:     map[string]string{},
		defInst:        map[ValueID]InstID{},
		extraFuncs:     map[string]bool{},
	}
	// Sanitize LLVM symbol names, disambiguating collisions. Nolang method
	// names embed their receiver type (e.g. `[]t.len`, `vec.reverse`,
	// `fs.file.close`); `[` `]` `<` `>` `*` are reserved tokens in LLVM IR, so a
	// raw `define void @[]t.len` is rejected by opt ("expected value token").
	// sanitize() maps every non-[a-zA-Z0-9_] char to `_`, yielding a legal
	// symbol. The SAME sanitized name is used at every call site (c.fname[cid])
	// and in FuncByName lookups, so definitions and references stay consistent.
	//
	// Two DISTINCT Nolang functions can sanitize to the SAME symbol — the
	// classic case is the method `i64.to_str` (HIR name `i64.to_str`) and the
	// free helper `i64_to_str` (HIR name `i64_to_str`): both become `i64_to_str`.
	// Emitting two `define @i64_to_str` is rejected by opt ("redefinition of
	// '@i64_to_str'"). When a sanitized name is already taken we suffix it with a
	// numeric index. Crucially, FuncByName is keyed by the RAW HIR name (which
	// still distinguishes `i64.to_str` from `i64_to_str`), so each call resolves
	// to the correct FuncID; c.fname[that FuncID] then yields the (suffixed,
	// unique) symbol used by both the definition and the call site. The call
	// graph is therefore preserved and no two definitions collide.
	usedFname := map[string]bool{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.ID == NoFunc {
			continue
		}
		// user `main` collides with the C entry `i32 @main`; rename it.
		var base string
		if f.Name == "main" {
			base = "_nolang_main"
		} else {
			base = sanitize(f.Name)
		}
		name := base
		if usedFname[name] {
			k := 1
			for usedFname[fmt.Sprintf("%s_%d", base, k)] {
				k++
			}
			name = fmt.Sprintf("%s_%d", base, k)
		}
		usedFname[name] = true
		c.fname[f.ID] = name
	}
	c.collectStrings()
	c.initOptionSlot()
	c.emitPrelude()
	c.emitStructTypes()
	c.emitGlobals()
	// Async cooperative scheduler runtime (globals + nolang_async_* functions).
	// Buffered into extraGlobals so it is emitted after every function body;
	// forward references to these functions from OpRun/OpAwait caller code are
	// legal in LLVM. The %task type itself is declared in the prelude so it is
	// visible to the function bodies that reference %task*.
	c.asyncWrappers = map[string]string{}
	c.emitAsyncScheduler()
	// Module globals resolve to their @name (a pointer); register the slot so
	// any instruction referencing the global value emits `@name` directly.
	c.globalSlots = map[ValueID]string{}
	for _, g := range m.Globals {
		c.globalSlots[g.Init] = "@" + g.Name
		c.valSlot[g.Init] = "@" + g.Name
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.ID == NoFunc || f.IsExtern {
			continue
		}
		if err := c.emitFunc(f); err != nil {
			return "", err
		}
	}
	if mid := findMainFunc(m); mid != NoFunc {
		if _, ok := c.fname[mid]; ok {
			c.emitEntry(m.Func(mid))
		}
	}
	// Module-level trailing section: globals and external declarations that
	// emission discovered along the way (MIR has no separate "declare" pass).
	// Helper bodies come first: a later function may call them, and while LLVM
	// allows forward references, keeping definitions ahead of the globals that
	// sometimes reference them reads better in a dump.
	if c.extraFuncsBody.Len() > 0 {
		c.sb.WriteString(c.extraFuncsBody.String())
	}
	for _, g := range c.extraGlobals {
		c.sb.WriteString(g + "\n")
	}
	for _, d := range c.extDeclOrder {
		c.sb.WriteString(d + "\n")
	}
	// Marker: this module was produced by the MIR backend. The build pipeline
	// detects it (see build.builder.go's llc invocation) to select MIR-specific
	// codegen flags — notably --fp-contract=off, because MIR's IR shape lets
	// llc fuse `fmul`+`fadd` into an FMA where the legacy IR does not, which
	// shifts float results by 1 ulp relative to legacy (test-tmp-nbody-debug).
	c.sb.WriteString("!nolang.mir.backend = !{!0}\n!0 = !{i32 1}\n")
	if len(c.errs) > 0 {
		return "", fmt.Errorf("MIR->LLVM: %s", strings.Join(c.errs, "; "))
	}
	return c.sb.String(), nil
}

func findMainFunc(m *Module) FuncID {
	for i := range m.Funcs {
		if m.Funcs[i].Name == "main" {
			return m.Funcs[i].ID
		}
	}
	return NoFunc
}

func (c *codegen) collectStrings() {
	for _, inst := range c.mod.Insts {
		if inst.Op == OpConst && inst.Type != NoType {
			lt := c.mod.Type(inst.Type)
			if lt != nil && lt.Kind == KindStr {
				if _, ok := c.strGlobals[inst.Dst]; !ok {
					c.strGlobals[inst.Dst] = strGlobal{
						name: fmt.Sprintf("@.mir.str.%d", inst.Dst),
						n:    len(inst.Str),
						str:  inst.Str,
					}
				}
			}
		}
	}
}

func (c *codegen) llvmTypeOf(t *Type) string {
	switch t.Kind {
	case KindInt, KindChar:
		if t.Raw == "byte" || t.Raw == "u8" || t.Raw == "i8" {
			return "i8"
		}
		return "i64"
	case KindFloat:
		return "double"
	case KindBool:
		return "i1"
	case KindVoid:
		return "void"
	case KindStr:
		return "%str-long"
	case KindSlice:
		return "%vec"
	case KindMap:
		// Map types ([str]i64, [i64]bool, ...) are backed by a specialized
		// hashmap struct (hashmap-str-i64, hashmap-i64-bool, ...) generated by
		// the generic_structs pass. Without this mapping, a map value's LLVM
		// type falls through to "i64" (8 bytes), while the real struct needs
		// 4 fields (keys/vals/occ/cap = 4×24+8 bytes). Every getfield/setfield
		// then writes out-of-bounds -> SIGSEGV. The naming convention mirrors
		// hir2mir's resolveMapMethod: [key]val → hashmap-<key>-<val>, with
		// []-bearing value types sanitised (e.g. []str → slice_str).
		if k, v, ok := parseMapTypes(t.Raw); ok {
			vSan := strings.ReplaceAll(v, "[]", "slice_")
			hmName := "hashmap-" + k + "-" + vSan
			if _, ok := c.mod.StructFields[hmName]; ok {
				return "%" + sanitize(hmName)
			}
			// Fall back to suffix match (module-qualified registration).
			if s := c.structLLVMType(hmName); s != "" {
				return s
			}
		}
		return "i64"
	case KindOption:
		// Every ?T lowers to the SAME `%option`; the element survives only in
		// the nolang type, so the payload must be re-punned from that when the
		// slot is read (see optPayloadAddr / optPayloadLTOf).
		if e, ok := parseOptionElem(t.Raw); ok {
			lt, _ := c.optionType(e)
			return lt
		}
		return "%option"
	case KindEnum:
		// Tagged enum: { i64 tag, [N x i64] payload }. The payload is a UNION
		// shared by every variant, so it is a flat slot array rather than a
		// per-variant struct — a field access bitcasts to the field's real
		// type at its slot offset.
		return "%tenum_" + sanitize(t.Raw)
	case KindArray:
		// Fixed array: LLVM [N x elem]. Element type is required for the layout.
		if t.Elem != NoType && c.mod.Type(t.Elem) != nil {
			return fmt.Sprintf("[%d x %s]", sizeOfArray(t), c.llvmTypeOf(c.mod.Type(t.Elem)))
		}
		return "[0 x i64]"
	case KindPtr:
		// View type `&T`: a borrow is represented as a POINTER to the borrowed
		// value's LLVM type — `&json` -> `%json*`, `&[]i64` -> `%vec*`,
		// `&str` -> `%str-long*`, `&i64` -> `i64*`. Because aggregates are
		// already passed as `T*` in the MIR call ABI, a view can be handed
		// straight to a method as its `self` receiver with no conversion.
		if strings.HasPrefix(t.Raw, "&") {
			if t.Elem != NoType {
				if et := c.mod.Type(t.Elem); et != nil {
					return c.llvmTypeOf(et) + "*"
				}
			}
			if et := c.mod.internType(strings.TrimPrefix(t.Raw, "&")); et != NoType {
				if ety := c.mod.Type(et); ety != nil {
					return c.llvmTypeOf(ety) + "*"
				}
			}
		}
		return "i8*"
	case KindFunc:
		// A function-pointer type renders to the by-reference LLVM signature:
		// every parameter and result is passed as a pointer (matching Nolang's
		// call ABI), so the pointer type is
		//   void (Param0*, ..., ParamN-1*, Result0*, ..., ResultM-1*)*
		if t.Func != nil {
			var parts []string
			for _, p := range t.Func.Params {
				if pt := c.mod.Type(p); pt != nil {
					parts = append(parts, c.llvmTypeOf(pt)+"*")
				}
			}
			for _, r := range t.Func.Results {
				if rt := c.mod.Type(r); rt != nil {
					parts = append(parts, c.llvmTypeOf(rt)+"*")
				}
			}
			return "void (" + strings.Join(parts, ", ") + ")*"
		}
		return "void ()*"
	default:
		if t.Raw != "" {
			// Only emit a named struct reference for types that are *real*
			// structs (a KStructDef was collected into StructFields). Qualified
			// names like `fs.fd` may be primitive type aliases (e.g. fd = i64),
			// in which case emitting `%fs_fd` would reference an undefined type.
			if _, ok := c.mod.StructFields[t.Raw]; ok {
				return "%" + sanitize(t.Raw)
			}
			// std structs are registered module-qualified (e.g. `os.utsname` ->
			// `%os_utsname`); a bare reference like `utsname` won't hit the
			// exact-key check above. Resolve it via suffix so the result slot is
			// allocated with the real struct type instead of i64 (allocating the
			// wrong type made `getelementptr %os_utsname, i64* slot` UB ->
			// trace/BPT trap on every `os.uname` return).
			if s := c.structLLVMType(t.Raw); s != "" {
				return s
			}
			return "i64"
		}
		return "i64"
	}
}

func sanitize(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// intWidth returns the bit width of an integer-like LLVM type, used to decide
// how to coerce operands of mismatched widths (e.g. an i8 str byte against an
// i64 literal in `c - 32` / `c >= 97`). Returns ok=false for non-integer types.
func intWidth(t string) (int, bool) {
	switch t {
	case "i1":
		return 1, true
	case "i8":
		return 8, true
	case "i32":
		return 32, true
	case "i64":
		return 64, true
	}
	return 0, false
}

// coerceInt inserts a truncation/extension so that value v (typed fromT) can be
// used where toT is expected. Wider->narrower truncates low bits; narrower->
// wider sign-extends (preserving signed byte/char semantics). If the types are
// already equal (or either is non-integer) it returns v unchanged.
func (c *codegen) coerceInt(v, fromT, toT string) string {
	if fromT == toT || toT == "" {
		return v
	}
	// Integer -> double: when a mixed-type arithmetic (e.g. i64 + f64)
	// feeds an i64 operand into a double result, LLVM requires an explicit
	// sitofp conversion. Without it, `fadd double %d, %i64_val` fails opt
	// verification ("defined with type 'i64' but expected 'double'").
	// This covers the JSON parse test family where `x = i + f` produces an
	// i64-typed MIR value used in a double fadd.
	if toT == "double" && fromT != "double" && fromT != "float" {
		fw, fok := intWidth(fromT)
		if fok {
			c.loadSeq++
			r := fmt.Sprintf("%%cv%d", c.loadSeq)
			if fw == 64 {
				c.sb.WriteString(fmt.Sprintf("  %s = sitofp i64 %s to double\n", r, v))
			} else {
				c.sb.WriteString(fmt.Sprintf("  %s = sitofp %s %s to double\n", r, fromT, v))
			}
			return r
		}
	}
	// double/float -> integer: the reverse direction (fptosi).
	if (fromT == "double" || fromT == "float") && toT != "double" && toT != "float" {
		c.loadSeq++
		r := fmt.Sprintf("%%cv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = fptosi %s %s to %s\n", r, fromT, v, toT))
		return r
	}
	fw, fok := intWidth(fromT)
	tw, tok := intWidth(toT)
	if !fok || !tok {
		return v
	}
	if fw == tw {
		return v
	}
	c.loadSeq++
	r := fmt.Sprintf("%%cv%d", c.loadSeq)
	if fw > tw {
		c.sb.WriteString(fmt.Sprintf("  %s = trunc %s %s to %s\n", r, fromT, v, toT))
	} else if fromT == "i8" {
		// Narrowing->widening an i8 must ZERO-extend: nolang's `byte`/`u8` are
		// UNSIGNED and are by far the dominant user of the i8 LLVM type (MIR
		// maps byte/u8/i8 all to i8), and the legacy backend zexts i8 to i64.
		// Sign-extending made a byte >= 0x80 poison every wider expression it
		// took part in — `padded[i] = 0x80` then
		// `(padded[i+0]<<24)|...|padded[i+3]` yielded a negative word, which is
		// why std/crypto/sha1 produced a wrong (but exit-0) digest under MIR.
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to %s\n", r, fromT, v, toT))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = sext %s %s to %s\n", r, fromT, v, toT))
	}
	return r
}

// coerceIndex widens an array/slice index operand to i64. Indices are always
// non-negative, so unsigned integer types (i8/i16/i32/i1) are ZERO-extended —
// not sign-extended — so a byte index like 200 stays 200 instead of wrapping to a
// huge i64 (which would read past the array). i64 indices are returned unchanged.
func (c *codegen) coerceIndex(idxSrc ValueID, idxT, idxV string) string {
	// An option-typed index (`a[res]` where `res ?i64 = ...`) must be unwrapped
	// to its payload before it can address memory: `%option` is a struct and opt
	// rejects it as a GEP index ("defined with type '%option' but expected
	// 'i64'"). Legacy stores/reads the scalar payload for an option index, so do
	// the same (tests/test-option-index.no).
	//
	// The peel goes through optionPayloadOf, NOT `extractvalue %option %v, 1`:
	// field 1 is now the whole `[N x i64]` payload slot, so a raw
	// extractvalue would hand the GEP an array where it wants an i64.
	if isOptionType(idxT) {
		pv, pt := c.optionPayloadOf(idxSrc, idxV, idxT)
		if pv != "" {
			idxT, idxV = pt, pv
		}
	}
	switch idxT {
	case "i8", "i1", "i16", "i32":
		c.loadSeq++
		r := fmt.Sprintf("%%izx%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", r, idxT, idxV))
		return r
	}
	return idxV
}

// isOptionType reports whether lt is the option LLVM type. Every `?T` lowers to
// the SAME `%option = { i64 tag, [N x i64] slot }`, so this is a plain
// equality test; the element type is recovered from the MIR type when a caller
// needs it (see optElemRawOf / optPayloadLTOf).
func isOptionType(lt string) bool {
	return lt == "%option"
}

// peelOptionValue unwraps a by-value option into its payload field: it reads
// the payload out of the 24-byte slot (dereferencing it first when the payload
// is BOXED) and returns the payload's LLVM type plus the fresh register. Returns ("", "") when lt is not an option type, when the
// payload type is unknown (nothing is emitted in that case), so callers can
// apply it unconditionally and fall back to the original operand.
//
// This is the sink half of the option-unwrap contract (§13.1): an `?T` flows
// into a position that wants a bare `T`. The canonical case is std/str.no
// `str.to-f32`, which does `f ?f64 = .to-f64()`, returns early on nil/err, and
// then calls `number.f64-to-f32(f)` — the surviving value is ok(payload), so
// the payload must be peeled before the conversion. Without this the option
// struct itself is handed to the converter and opt rejects the module:
// "builtin number.f64-to-f32: cannot coerce %option_f64 to double".
func (c *codegen) peelOptionValue(src ValueID) (string, string) {
	if src <= NoVal {
		return "", ""
	}
	lt, _ := c.ptype(src)
	if !isOptionType(lt) {
		return "", ""
	}
	v, t := c.optionPayloadOf(src, "", lt)
	return t, v
}

// optionPayloadLLVMType returns the LLVM type of an option's payload field for
// element raw type elemRaw. Scalar elements stay i64 (stored inline in the flat
// %option); non-scalars (str/vec/user-struct/fixed-array) get their real type
// so the payload can be stored by-value in a per-payload inline option type.
func (c *codegen) optionPayloadLLVMType(elemRaw string) string {
	switch elemRaw {
	case "str":
		return "%str-long"
	case "vec":
		return "%vec"
	case "double", "f64":
		return "double"
	case "i64", "i8", "i1", "byte", "u8", "char", "bool", "":
		return "i64"
	}
	if strings.HasPrefix(elemRaw, "[]") {
		return "%vec"
	}
	if strings.HasPrefix(elemRaw, "&") {
		// `?&T` — an optional view. The payload is the view pointer itself
		// (`%T*`), so the option stays a two-field inline struct and no
		// ownership is implied (ClassifyOwnership reports `&T` as not owned).
		if t := c.mod.internType(elemRaw); t != NoType {
			if ty := c.mod.Type(t); ty != nil {
				return c.llvmTypeOf(ty)
			}
		}
		return "i8*"
	}
	if strings.HasPrefix(elemRaw, "[") {
		if t := c.mod.internType(elemRaw); t != NoType {
			if ty := c.mod.Type(t); ty != nil {
				if lt := c.llvmTypeOf(ty); strings.HasPrefix(lt, "[") {
					return lt
				}
			}
		}
		return "i64"
	}
	// user struct
	if lt := c.structLLVMType(elemRaw); lt != "" {
		return lt
	}
	if _, ok := c.mod.StructFields[elemRaw]; ok {
		return "%" + sanitize(elemRaw)
	}
	return "i64"
}

// ---- unified option layout -------------------------------------------------
//
// EVERY `?T` lowers to the single 32-byte type
//
//	%option = type { i64 tag, [N x i64] slot }   ; N = 8*ceil(option-inline-threshold/8)
//
// The slot is 24 bytes because the payload that must ALWAYS fit is the err
// message, a %str-long. The previous flat `%option = { i64, i64 }` could only
// hold 8 bytes, so `err('msg')` into a narrow option (?i64 / ?bool / ?f64 /
// ?byte ...) stored the message's heap POINTER as an integer and threw away
// len/cap — the err arm printed a raw address (e.g. `err=4385955280`) and the
// str_clone it took leaked. `?str` was fine only by accident: its per-payload
// type `%option_str = { i64, %str-long }` happened to be 32 bytes.
//
// Two storage classes, decided per payload type by optionPayloadInline:
//
//   - INLINE (payload <= 24 bytes): the payload's bytes live in the slot,
//     reached by bit-casting the slot address to `<payload>*`.
//   - BOXED (payload > 24 bytes, or of unknown size): the payload is copied
//     into a heap block and slot[0] holds that pointer (as i64).
//
// Boxing is ONLY used for the ok tag (0). nil (1) and err (2) always keep
// their payload inline, because an err message is a 24-byte str and a nil has
// no payload — allocating a 36 KB box for `err('bad json')` would be absurd.
// Consequently every read of a payload that *may* be boxed goes through
// optPayloadAddr, which selects between the box and a payload-sized zero
// constant on `tag == 0`. It must NOT select the slot as the non-ok side:
// a boxed payload is LARGER than the slot, so that would be an
// out-of-bounds `getelementptr inbounds`, i.e. UB that LLVM resolves by
// hoisting the load above the branch and dereferencing the err message
// as a pointer (SIGSEGV only in the optimized build).

// optSlotDefaultBytes is the default payload-slot size. The err payload is a
// %str-long, which is exactly this wide — so at the default every err message
// fits inline, and only payloads the user explicitly made bigger than this go
// through a pointer.
const optSlotDefaultBytes = 24

// optSlotMinBytes is the smallest slot a user may ask for. Below it a payload
// could not even hold an i64, so it is a hard compile error.
const optSlotMinBytes = 8

// initOptionSlot resolves the payload-slot size for this module and the LLVM
// type that spells it. The slot is `[N x i64]`, so N is a whole number of
// i64s and the size is rounded UP: a payload the user said fits (<= threshold)
// must actually fit.
//
// Range rules (OptionInlineThreshold comes from package.jsonc's
// compiler.option-inline-threshold or NOLANG_OPTION_INLINE_THRESHOLD):
//
//	unset / 0   -> optSlotDefaultBytes (24)
//	< 8         -> compile error, clamped to 8 so the rest of the emitter
//	               still produces verifiable IR
//	8 .. 23     -> accepted, but WARN: the err message (a 24-byte %str-long)
//	               no longer fits, so err payloads are heap-boxed
//	>= 24       -> accepted
func (c *codegen) initOptionSlot() {
	t := int64(c.mod.OptionInlineThreshold)
	switch {
	case t == 0:
		t = optSlotDefaultBytes
	case t < optSlotMinBytes:
		c.fail("option-inline-threshold %d is below the minimum %d (a payload must at least hold an i64)", t, optSlotMinBytes)
		t = optSlotMinBytes
	case t < optSlotDefaultBytes:
		fmt.Fprintf(os.Stderr, "warning: option-inline-threshold %d is below %d: err payloads (a 24-byte str) no longer fit the slot and are heap-boxed\n", t, optSlotDefaultBytes)
	}
	// Round up to a whole number of i64 slots.
	n := (t + 7) / 8
	c.optSlotBytes = n * 8
	c.optSlotLT = fmt.Sprintf("[%d x i64]", n)
}

// optionType returns the (LLVM option type, payload LLVM type) for an option
// whose element raw type is elemRaw. The option type is ALWAYS `%option`; the
// payload type is returned separately because callers need it to pun the slot
// correctly (the slot itself is untyped bytes).
func (c *codegen) optionType(elemRaw string) (string, string) {
	return "%option", c.optionPayloadLLVMType(elemRaw)
}

// optionPayloadInline reports whether a payload of LLVM type payloadLT is
// stored inline in the module's payload slot (width = option-inline-threshold). Anything larger — or of a size we cannot
// prove — is heap-boxed. The decision must be CONSERVATIVE: claiming a payload
// fits when it does not corrupts the bytes after it, while boxing a payload
// that would have fitted only costs a malloc.
func (c *codegen) optionPayloadInline(payloadLT string) bool {
	sz, ok := c.llvmTypeSizeUpper(payloadLT, map[string]bool{})
	return ok && sz <= c.optSlotBytes
}

// llvmTypeSizeUpper returns a value >= sizeof(lt), or (0, false) when the size
// cannot be determined. It is an UPPER BOUND on purpose: struct fields are
// rounded up to 8 bytes each, so alignment padding can never make the real
// size exceed the answer. Under-estimating here would be the dangerous
// direction — it would let a payload be stored inline past the end of the slot.
func (c *codegen) llvmTypeSizeUpper(lt string, visited map[string]bool) (int64, bool) {
	if n, ok := mirScalarByteSize[lt]; ok {
		return n, true
	}
	if strings.HasSuffix(lt, "*") {
		return 8, true
	}
	switch lt {
	case "%str-long", "%vec":
		return 24, true
	case "%option":
		return 32, true
	case "%txt":
		return 256, true
	}
	// Fixed array: [N x T] -> N * sizeof(T).
	if strings.HasPrefix(lt, "[") {
		inner := lt[1:strings.LastIndex(lt, "]")]
		xIdx := strings.Index(inner, " x ")
		if xIdx < 0 {
			return 0, false
		}
		n, err := strconv.ParseInt(strings.TrimSpace(inner[:xIdx]), 10, 64)
		if err != nil {
			return 0, false
		}
		esz, ok := c.llvmTypeSizeUpper(inner[xIdx+3:], visited)
		if !ok {
			return 0, false
		}
		return n * esz, true
	}
	if strings.HasPrefix(lt, "%") {
		name := lt[1:]
		if visited[name] {
			return 0, false // recursive struct — size unknown, so BOX it
		}
		visited[name] = true
		structKey := c.structKeyOf(unsanitize(name))
		if structKey == "" {
			structKey = unsanitize(name)
		}
		fields, ok := c.mod.StructFields[structKey]
		if !ok {
			fields, ok = c.mod.StructFields[name]
		}
		if !ok {
			if k := c.sanitizedStructKey(name); k != "" {
				fields, ok = c.mod.StructFields[k]
			}
		}
		if !ok {
			return 0, false
		}
		var total int64
		for _, f := range fields {
			if c.fieldIsPointer(f) {
				total += 8
				continue
			}
			fsz, ok := c.llvmTypeSizeUpper(c.nolangTypeToLLVM(f.TypeRaw), visited)
			if !ok {
				return 0, false
			}
			total += (fsz + 7) &^ 7 // round up: an over-estimate is safe
		}
		return total, true
	}
	return 0, false
}

// optSlotAddr returns a `[N x i64]*` register pointing at the payload slot of
// the option stored at optSlot (a `%option*`).
func (c *codegen) optSlotAddr(optSlot string) string {
	c.loadSeq++
	r := fmt.Sprintf("%%osl%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", r, optSlot))
	return r
}

// optLoadTag loads the option's discriminant (field 0) out of optSlot.
func (c *codegen) optLoadTag(optSlot string) string {
	c.loadSeq++
	tp := fmt.Sprintf("%%otp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n", tp, optSlot))
	c.loadSeq++
	t := fmt.Sprintf("%%otg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", t, tp))
	return t
}

// optStoreTag stores `tag` with a fully zeroed payload slot into optSlot. Every
// payload store must be preceded by this: it clears the bytes of any previous,
// longer payload so a later read of a shorter type cannot see stale data.
func (c *codegen) optStoreTag(optSlot string, tag int64) {
	c.sb.WriteString(fmt.Sprintf("  store %%option { i64 %d, %s zeroinitializer }, %%option* %s\n", tag, c.optSlotLT, optSlot))
}

// optSpill materialises a by-value `%option` register into a fresh stack slot
// and returns the slot. Needed wherever an option arrives as an SSA value (a
// call result, a load) but the payload must be addressed by pointer.
func (c *codegen) optSpill(val string) string {
	c.loadSeq++
	s := fmt.Sprintf("%%ospi%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = alloca %%option\n", s))
	c.sb.WriteString(fmt.Sprintf("  store %%option %s, %%option* %s\n", val, s))
	return s
}

// optSlotOfValue returns the `%option*` slot for an option held by MIR value v,
// spilling when v has no slot of its own.
func (c *codegen) optSlotOfValue(v ValueID) string {
	if s := c.valSlot[v]; s != "" {
		return s
	}
	_, val := c.loadVal(v)
	if val == "" {
		return ""
	}
	return c.optSpill(val)
}

// optPayloadAddr returns an i8* register holding the address of the option's
// payload bytes for a payload being read as `payloadLT`.
//
// STORAGE CLASS IS A PROPERTY OF THE PAYLOAD TYPE, NOT OF THE TAG
// ---------------------------------------------------------------
// A payload is inline when sizeof(payloadLT) <= the slot, and boxed otherwise.
// That is a compile-time fact here: `payloadLT` is the type the caller wants to
// read, and it is exactly the type that was stored, so the size test decides
// the storage class with no tag check. This matters because err payloads are
// NOT always inline any more: an err message is a 24-byte %str-long, so once
// the slot is configured below 24 ("option-inline-threshold 8..23") err uses a
// pointer too.
//
// WHAT THE RUNTIME `select` IS FOR
// --------------------------------
// Only "was a box actually allocated?" is a runtime question. Nothing is boxed
// for nil, nor for a bare `err()` with no message, and in both cases the slot
// is zero — so `slot[0] == 0` selects the zero constant. Testing `tag == 0`
// instead would be wrong twice over: it would miss a boxed err message, and it
// would hand back an inline err message's first 8 bytes as a pointer.
//
// WHY THE NON-BOXED SIDE IS A GLOBAL AND NOT THE SLOT
// ---------------------------------------------------
// A boxed payload is by definition LARGER THAN THE SLOT, so reading it from the
// slot is an out-of-bounds `getelementptr inbounds` — undefined behaviour on
// exactly the branch where the box pointer is garbage. LLVM exploits it: SROA
// turns the `select` into a branch, concludes the out-of-bounds side cannot
// happen, and HOISTS THE LOAD above the branch. At -O0 the program is fine;
// after `opt` it dereferences the err message's length field as a pointer and
// dies (tests/test_fs_error_simple.no, tests/test-sse.no — both SEGFAULT only
// in the optimized build). A zero-filled global of the payload's own type keeps
// both sides of the select valid and in-bounds, so there is nothing to
// speculate.
// optPayloadAddr treats the declared payload and the type being read as the
// same thing — the common case. See optPayloadAddrAs.
func (c *codegen) optPayloadAddr(optSlot, readLT string) string {
	return c.optPayloadAddrAs(optSlot, readLT, readLT)
}

// optPayloadAddrAs is the general form: `declaredLT` is the option's declared
// element as lowered to LLVM, `readLT` is the type the CALLER wants to read out
// of the slot. They differ for the err-message type pun (readLT = %str-long out
// of an option declared `?i64`, say).
func (c *codegen) optPayloadAddrAs(optSlot, declaredLT, readLT string) string {
	slotAddr := c.optSlotAddr(optSlot)
	if c.optionPayloadInline(readLT) {
		c.loadSeq++
		r := fmt.Sprintf("%%opa%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", r, c.optSlotLT, slotAddr))
		return r
	}
	// slot[0] holds the box pointer — as an i64, because the slot is typed
	// [N x i64] so that N can follow the configured threshold.
	c.loadSeq++
	p0 := fmt.Sprintf("%%opp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", p0, c.optSlotLT, c.optSlotLT, slotAddr))
	c.loadSeq++
	bx := fmt.Sprintf("%%opb%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", bx, p0))
	// Which tag means "there is a box of THIS type in slot[0]". It is not one
	// rule for every option: a box is allocated with sizeof(whatever was
	// stored), so a tag whose stored type differs from readLT must NOT be
	// dereferenced as readLT.
	//
	//   readLT == declared, not %str-long -> only tag 0 (ok) boxes the declared
	//       payload. An err sits there as an inline message (default slot) or as
	//       a 24-byte %str-long box (slot < 24); handing either to a caller that
	//       wants an 824-byte %sse_client is a heap over-read followed by a
	//       free() of garbage — "pointer being freed was not allocated" on an
	//       option that matched into its catch-all arm while holding an err.
	//   readLT == %str-long == declared -> tag 0 and tag 2 both hold a
	//       %str-long box (the ok string and the err message), so tag 1 (nil) is
	//       the only one that does not.
	//   readLT == %str-long != declared -> only tag 2: the err message pun.
	//
	// On top of that slot[0] must be non-zero: nil stores nothing, and so does
	// a bare `err()` with no message.
	tg := c.optLoadTag(optSlot)
	c.loadSeq++
	hasTag := fmt.Sprintf("%%opk%d", c.loadSeq)
	switch {
	case readLT == "%str-long" && declaredLT == "%str-long":
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i64 %s, 1\n", hasTag, tg))
	case readLT == "%str-long":
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 2\n", hasTag, tg))
	default:
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", hasTag, tg))
	}
	c.loadSeq++
	nz := fmt.Sprintf("%%opn%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ne i64 %s, 0\n", nz, bx))
	c.loadSeq++
	has := fmt.Sprintf("%%oph%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", has, hasTag, nz))
	c.loadSeq++
	bp := fmt.Sprintf("%%opq%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", bp, bx))
	// Zero stand-in for "nothing was boxed". %str-long is registered
	// unconditionally because an err message can be read out of ANY option,
	// including one whose declared element is a scalar.
	zero := c.optBoxZero[readLT]
	c.loadSeq++
	zg := fmt.Sprintf("%%opz%d", c.loadSeq)
	if zero == "" {
		c.fail("internal: no zero-initializer global for boxed option payload %s", readLT)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", zg, c.optSlotLT, slotAddr))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, ptr %s, i64 0\n", zg, readLT, zero))
	}
	c.loadSeq++
	sel := fmt.Sprintf("%%opr%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i8* %s, i8* %s\n", sel, has, bp, zg))
	return sel
}

// optStoreInlinePayload stores a by-value payload of type payloadLT into the
// option's slot. The slot must already have been cleared by optStoreTag.
func (c *codegen) optStoreInlinePayload(optSlot, payloadLT, val string) {
	slotAddr := c.optSlotAddr(optSlot)
	c.loadSeq++
	p := fmt.Sprintf("%%oip%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to %s*\n", p, c.optSlotLT, slotAddr, payloadLT))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", payloadLT, val, payloadLT, p))
}

// optStoreBoxedPayload heap-allocates a box big enough for payloadLT, copies
// srcAddr's bytes into it, and records the pointer in slot[0].
func (c *codegen) optStoreBoxedPayload(optSlot string, tag int64, payloadLT, srcAddr string) {
	c.optStoreTag(optSlot, tag)
	sz := c.typeSizeOperand(payloadLT)
	c.loadSeq++
	m := fmt.Sprintf("%%obm%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", m, sz))
	c.emitMemcpy(m, srcAddr, sz)
	c.loadSeq++
	pi := fmt.Sprintf("%%obn%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pi, m))
	c.loadSeq++
	w := fmt.Sprintf("%%obo%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option { i64 %d, %s zeroinitializer }, i64 %s, 1, 0\n", w, tag, c.optSlotLT, pi))
	c.sb.WriteString(fmt.Sprintf("  store %%option %s, %%option* %s\n", w, optSlot))
}

// addrOfValue returns an i8* register pointing at the storage of MIR value v,
// spilling to a temporary alloca when v has no slot of its own (a transient
// call result or a literal).
func (c *codegen) addrOfValue(v ValueID, lt, val string) string {
	if lt == "" || lt == "void" {
		return ""
	}
	if s := c.valSlot[v]; s != "" {
		c.loadSeq++
		r := fmt.Sprintf("%%adv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", r, lt, s))
		return r
	}
	c.loadSeq++
	s := fmt.Sprintf("%%ads%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", s, lt))
	if val != "" {
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, val, lt, s))
	}
	c.loadSeq++
	r := fmt.Sprintf("%%adv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", r, lt, s))
	return r
}

// optStoreRawBytes stores a value whose type DIFFERS from the option's declared
// payload — the type-pun case. The canonical one is `err(msg)` into a non-str
// option: ok payloads and err messages are a union, so the message is stored
// under whatever class its own size dictates.
//
//   - fits the slot  -> copied verbatim into the slot and read back verbatim.
//     Only min(sizeof(src), slot) bytes are moved; the slot was zeroed first,
//     so a shorter source leaves zero padding (which is what made a bare
//     `err()` read back as an empty string).
//   - does NOT fit   -> heap-boxed, exactly like an oversized ok payload. That
//     is the option-inline-threshold < 24 case: the message is a 24-byte
//     %str-long and no longer fits, so err goes through a pointer.
//
// A source of UNKNOWN size still goes through the inline path with a
// zero-length copy rather than a bitcast+load pun — that pun read past the
// source's alloca and SIGSEGV'd when the payload was much larger
// (%str-long 24 B -> %json_json 36 KB).
func (c *codegen) optStoreRawBytes(optSlot string, tag int64, srcLT string, src ValueID, sv string) {
	srcAddr := c.addrOfValue(src, srcLT, sv)
	if srcAddr == "" {
		c.optStoreTag(optSlot, tag)
		return
	}
	if !c.optionPayloadInline(srcLT) {
		c.optStoreBoxedPayload(optSlot, tag, srcLT, srcAddr)
		return
	}
	c.optStoreTag(optSlot, tag)
	srcUpper, ok := c.llvmTypeSizeUpper(srcLT, map[string]bool{})
	if !ok {
		return
	}
	n := srcUpper
	if n > c.optSlotBytes {
		n = c.optSlotBytes
	}
	slotAddr := c.optSlotAddr(optSlot)
	c.loadSeq++
	dst := fmt.Sprintf("%%orb%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dst, c.optSlotLT, slotAddr))
	c.emitMemcpy(dst, srcAddr, fmt.Sprintf("%d", n))
}

// optPayloadTypedAddr returns a `<wantLT>*` register for the option's payload.
//
// The storage class follows wantLT, the type being READ, because that is the
// type that was stored: an err message read out of a scalar option is a
// %str-long, so it is inline or boxed by %str-long's size — not by the size of
// the option's declared element. (With the default 24-byte slot this is the
// same thing for err messages; below 24 it is what makes err use a pointer.)
func (c *codegen) optPayloadTypedAddr(optSlot, payloadLT, wantLT string) string {
	lt := wantLT
	if lt == "" {
		lt = payloadLT
	}
	raw := c.optPayloadAddrAs(optSlot, payloadLT, lt)
	if wantLT == "" {
		return raw
	}
	c.loadSeq++
	r := fmt.Sprintf("%%opt%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", r, raw, wantLT))
	return r
}

// optReadPayloadInto copies the option's payload out of optSlot into dstSlot as
// type dstLT. It is the single peel implementation shared by OpMove and the
// option-aware helpers.
func (c *codegen) optReadPayloadInto(optSlot, payloadLT, dstLT, dstSlot string) bool {
	if dstLT == "" || dstSlot == "" {
		return false
	}
	addr := c.optPayloadTypedAddr(optSlot, payloadLT, dstLT)
	if dstLT == payloadLT && c.shouldUseMemcpy(dstLT) {
		c.loadSeq++
		d := fmt.Sprintf("%%opd%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", d, dstLT, dstSlot))
		c.emitMemcpy(d, addr, c.typeSizeOperand(dstLT))
		return true
	}
	c.loadSeq++
	v := fmt.Sprintf("%%opv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", v, dstLT, dstLT, addr))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return true
}

// optBoxClone copies the option at srcSlot to dstSlot and then gives the copy
// its OWN heap block when the payload is boxed. A boxed option is 32 bytes of
// {tag, pointer}, so a bitwise copy would share the payload — the inline layout
// copied the payload itself, and code that mutates through one binding must not
// see the change through the other.
//
// The re-box is a helper call rather than inline branches because emitMove runs
// mid-block: emitting new basic blocks there breaks LLVM verification.
func (c *codegen) optBoxClone(srcSlot, dstSlot, payloadLT string) {
	c.loadSeq++
	v := fmt.Sprintf("%%obc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %%option, %%option* %s\n", v, srcSlot))
	c.sb.WriteString(fmt.Sprintf("  store %%option %s, %%option* %s\n", v, dstSlot))
	name, ok := c.optBoxRebox[payloadLT]
	if !ok {
		return
	}
	c.sb.WriteString(fmt.Sprintf("  call void %s(%%option* %s)\n", name, dstSlot))
}

// emitOptionBoxHelpers emits `@__nolang_opt_rebox_<key>` for a BOXED option
// payload type: after a bitwise copy of the option it replaces slot[0] with a
// freshly malloc'd copy of the box, but ONLY when the tag says ok (0). A nil or
// err option keeps its payload inline, so there is nothing to re-box and the
// pointer field must be left alone (it is a str's length in the err case).
func (c *codegen) emitOptionBoxHelpers(payloadLT string) {
	// Idempotent: the err message (%str-long) helper is registered up front, so
	// a `?str` in the same module must not declare the same global twice.
	if _, seen := c.optBoxZero[payloadLT]; seen {
		return
	}
	key := strings.NewReplacer("%", "", "-", "_", ".", "_", "*", "_", " ", "_").Replace(payloadLT)
	name := "@__nolang_opt_rebox_" + key
	c.optBoxRebox[payloadLT] = name
	// The zero-filled stand-in optPayloadAddr returns for a non-ok tag. It must
	// be a real, in-bounds, payload-sized object — see optPayloadAddr.
	zg := "@__nolang_opt_zero_" + key
	c.optBoxZero[payloadLT] = zg
	c.sb.WriteString(fmt.Sprintf("%s = private unnamed_addr constant %s zeroinitializer\n", zg, payloadLT))
	sz := fmt.Sprintf("ptrtoint ptr getelementptr (%s, ptr null, i64 1) to i64", payloadLT)
	c.sb.WriteString(fmt.Sprintf("define void %s(%%option* %%o) {\n", name))
	c.sb.WriteString("entry:\n")
	// Re-box only when a box is really there. The test is the same one
	// optPayloadAddr uses (see there for the full argument): once err messages
	// fit the slot, an inline err's first 8 bytes — the string LENGTH — sit in
	// slot[0] and look exactly like a box pointer, so the tag has to say which
	// it is.
	c.sb.WriteString("  %tp = getelementptr inbounds %option, %option* %o, i32 0, i32 0\n")
	c.sb.WriteString("  %t = load i64, i64* %tp\n")
	// Both tags that can own a box need re-boxing, and they own boxes of
	// DIFFERENT sizes: tag 0 holds sizeof(payloadLT), tag 2 holds a 24-byte
	// %str-long err message — but only when err messages are boxed at all
	// (slot < 24). With the default slot an err message is inline, so its first
	// 8 bytes — the string LENGTH — sit in slot[0] and must not be mistaken for
	// a box pointer; that is why tag 2 is not accepted by default.
	c.sb.WriteString("  %h0 = icmp eq i64 %t, 0\n")
	if c.optionPayloadInline("%str-long") {
		c.sb.WriteString("  %hastag = icmp eq i64 %t, 0\n")
	} else {
		c.sb.WriteString("  %h2 = icmp eq i64 %t, 2\n")
		c.sb.WriteString("  %hastag = or i1 %h0, %h2\n")
	}
	c.sb.WriteString("  %sp = getelementptr inbounds %option, %option* %o, i32 0, i32 1\n")
	c.sb.WriteString(fmt.Sprintf("  %%p0 = getelementptr inbounds %s, %s* %%sp, i64 0, i64 0\n", c.optSlotLT, c.optSlotLT))
	c.sb.WriteString("  %old = load i64, i64* %p0\n")
	c.sb.WriteString("  %nonzero = icmp ne i64 %old, 0\n")
	c.sb.WriteString("  %has = and i1 %hastag, %nonzero\n")
	c.sb.WriteString("  br i1 %has, label %rb, label %done\n")
	c.sb.WriteString("rb:\n")
	c.sb.WriteString("  %oldp = inttoptr i64 %old to i8*\n")
	// How many bytes to copy depends on WHAT was boxed, not on the declared
	// element: with the default slot an err message is inline so only tag 0
	// boxes (sizeof payloadLT), but once the slot is configured below 24 the
	// message is boxed too — and it is only 24 bytes, so copying
	// sizeof(payloadLT) (824 for ?sse_client) out of it is a heap over-read
	// that aborts under malloc's guard pages.
	if !c.optionPayloadInline("%str-long") {
		// Both sizes as real registers: `ptrtoint ... getelementptr` is a
		// constant expression, which LLVM only accepts inside a `select` in
		// parenthesised form — materialising it keeps the IR simple.
		c.sb.WriteString(fmt.Sprintf("  %%nOK = %s\n", sz))
		c.sb.WriteString(fmt.Sprintf("  %%nERR = %s\n",
			fmt.Sprintf("ptrtoint ptr getelementptr (%%str-long, ptr null, i64 1) to i64")))
		c.sb.WriteString("  %isok = icmp eq i64 %t, 0\n")
		c.sb.WriteString("  %n = select i1 %isok, i64 %nOK, i64 %nERR\n")
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%n = %s\n", sz))
	}
	c.sb.WriteString("  %m = call i8* @malloc(i64 %n)\n")
	c.sb.WriteString("  call void @llvm.memcpy.p0.p0.i64(ptr %m, ptr %oldp, i64 %n, i1 false)\n")
	c.sb.WriteString("  %nm = ptrtoint i8* %m to i64\n")
	c.sb.WriteString("  store i64 %nm, i64* %p0\n")
	c.sb.WriteString("  ret void\n")
	c.sb.WriteString("done:\n")
	c.sb.WriteString("  ret void\n}\n")
}

// optPayloadLTOf returns the payload LLVM type of the option-typed MIR value v,
// read from its nolang type. With a single `%option` for every element the LLVM
// type no longer carries the element, so this is the only reliable source.
func (c *codegen) optPayloadLTOf(v ValueID) string {
	_, elem := c.optElemRawOf(v)
	return c.optionPayloadLLVMType(elem)
}

// optPayloadLTFor resolves the payload's LLVM type for an option-typed value
// more robustly than optPayloadLTOf. optPayloadLTOf reads Value.Type, which for
// a re-wrapped value can already hold the INNER element type (`[]i64`) rather
// than the option — exactly the trap emitGetField documents — and then returns
// "". Here that silently degraded the index to a bare
// `getelementptr i64, i64* %p, i64 0, i64 %i`, which opt rejects with "invalid
// getelementptr indices": `v[i].to-str()` on a `?[]i64` failed to compile even
// though the plain `v[i]` in the same loop was fine, because the method call
// makes the compiler read the receiver out into a temp.
//
// Fall back to the function-local type (authoritative after lowering, the same
// source emitGetField prefers), and finally to the inner container's own kind.
func (c *codegen) optPayloadLTFor(v ValueID) string {
	if lt := c.optPayloadLTOf(v); lt != "" {
		return lt
	}
	if f := c.mod.Func(c.cf); f != nil {
		if tid, ok := f.LocalTypes[v]; ok {
			if t := c.mod.Type(tid); t != nil {
				if t.Kind == KindOption {
					if e, ok := parseOptionElem(t.Raw); ok {
						if pl := c.optionPayloadLLVMType(e); pl != "" {
							return pl
						}
					}
				}
				// Already the inner container: map its kind directly.
				switch t.Kind {
				case KindSlice:
					return "%vec"
				case KindStr:
					return "%str-long"
				}
			}
		}
	}
	return ""
}

// optElemRawOf returns the element raw type (?T -> T) of the option-typed MIR
// value v, and whether v really is an option.
func (c *codegen) optElemRawOf(v ValueID) (bool, string) {
	if val := c.mod.Value(v); val != nil {
		if t := c.mod.Type(val.Type); t != nil && t.Kind == KindOption {
			if e, ok := parseOptionElem(t.Raw); ok {
				return true, e
			}
		}
	}
	return false, ""
}

// optionElemRaw extracts the element raw type (?Elem) from an option raw type
// (?fs.file -> fs.file). Returns "" when t.Raw is not an option.
func optionElemRaw(t *Type) string {
	if t == nil || t.Kind != KindOption {
		return ""
	}
	if e, ok := parseOptionElem(t.Raw); ok {
		return e
	}
	return ""
}

// ---- prelude / runtime ----

func (c *codegen) emitPrelude() {
	c.sb.WriteString(`target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

%str-long = type { i64, i64, i8* }   ; len, cap, data
%vec = type { i64, i64, i64 }
`)
	c.sb.WriteString(fmt.Sprintf("%%option = type { i64, %s }    ; tag, %d-byte payload slot (%d bytes total)\n",
		c.optSlotLT, c.optSlotBytes, 8+c.optSlotBytes))
	c.sb.WriteString(`

; async task runtime (mirrors legacy build/llvm cooperative scheduler).
; %task = { resume_fn, data(i64 ptr), done, cancelled }; 24 bytes.
%task = type { void (i8*)*, i64, i1, i1 }

declare i8* @malloc(i64)
declare void @free(i8*)
declare i64 @write(i32, i8*, i64)
declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)
declare i32 @memcmp(i8*, i8*, i64)

define void @str_free(%str-long %s) {
entry:
  ; Borrowed slice VIEWS carry cap=0: their data pointer aliases another
  ; string's buffer and must NOT be freed. Only real heap strings (cap>0) own
  ; their buffer. Mirrors the @vec_free cap==0 skip below.
  %scap = extractvalue %str-long %s, 1
  %scap0 = icmp eq i64 %scap, 0
  br i1 %scap0, label %done, label %check
check:
  %data = extractvalue %str-long %s, 2
  %null = icmp eq i8* %data, null
  br i1 %null, label %done, label %freeit
freeit:
  call void @free(i8* %data)
  br label %done
done:
  ret void
}

; str_eq: structural string equality on %str-long. Two strings are equal iff
; their lengths match AND their bytes match (memcmp == 0). Never emits an icmp
; on the struct itself (that is illegal); the MIR OpStrEq op lowers to this call.
define i1 @str_eq(%str-long %a, %str-long %b) {
entry:
  %la = extractvalue %str-long %a, 0
  %lb = extractvalue %str-long %b, 0
  %lenEq = icmp eq i64 %la, %lb
  br i1 %lenEq, label %cmpto, label %noteq
cmpto:
  %da = extractvalue %str-long %a, 2
  %db = extractvalue %str-long %b, 2
  %cmp = call i32 @memcmp(i8* %da, i8* %db, i64 %la)
  %same = icmp eq i32 %cmp, 0
  br label %done
noteq:
  br label %done
done:
  %res = phi i1 [ 0, %noteq ], [ %same, %cmpto ]
  ret i1 %res
}

; vec_free: free a %vec's backing store. %vec = { i64 len, i64 cap, i64 data }
; (data is a heap pointer stored as an integer). The 3rd field is the malloc'd
; buffer; free it unless null. The %vec struct itself is by-value (stack/inline)
; and needs no free.
define void @vec_free(%vec %v) {
entry:
  ; Borrowed slice views (over string constants or stack arrays) carry cap=0 and
  ; must NOT be freed: their backing store is not heap-owned. Only real heap vecs
  ; (cap>0) own their buffer. Mirrors the legacy @vec_free cap==0 skip.
  %cap = extractvalue %vec %v, 1
  %cap0 = icmp eq i64 %cap, 0
  br i1 %cap0, label %done, label %check
check:
  %data = extractvalue %vec %v, 2
  %ptr = inttoptr i64 %data to i8*
  %null = icmp eq i8* %ptr, null
  br i1 %null, label %done, label %freeit
freeit:
  call void @free(i8* %ptr)
  br label %done
done:
  ret void
}

define %str-long @str_from_const(i8* %ptr, i64 %len) {
entry:
  %buf = call i8* @malloc(i64 %len)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %ptr, i64 %len, i1 0)
  %r0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %len, 0
  %r1 = insertvalue %str-long %r0, i64 %len, 1
  %r2 = insertvalue %str-long %r1, i8* %buf, 2
  ret %str-long %r2
}

define %str-long @str_concat(%str-long %a, %str-long %b) {
entry:
  %la = extractvalue %str-long %a, 0
  %lb = extractvalue %str-long %b, 0
  %total = add i64 %la, %lb
  %buf = call i8* @malloc(i64 %total)
  %da = extractvalue %str-long %a, 2
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %da, i64 %la, i1 0)
  %db = extractvalue %str-long %b, 2
  %dst2 = getelementptr i8, i8* %buf, i64 %la
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst2, i8* %db, i64 %lb, i1 0)
  %r0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %total, 0
  %r1 = insertvalue %str-long %r0, i64 %total, 1
  %r2 = insertvalue %str-long %r1, i8* %buf, 2
  ret %str-long %r2
}

; _mir_str_repeat: duplicate a string COUNT times (Nolang S * N). Mirrors the
; legacy generator's generateStrRepeat loop with a memcpy per iteration.
; Named with a _mir_ prefix so it does NOT collide with the MIR-lowered std
; function str.repeat (which lowers to the void result-param form
; "define void @str_repeat(...)"). Emitting both under the same LLVM name
; trips LLVM verification ("invalid redefinition of function 'str_repeat'").
define %str-long @_mir_str_repeat(%str-long %s, i64 %count) {
entry:
  %len = extractvalue %str-long %s, 0
  %total = mul i64 %len, %count
  %buf = call i8* @malloc(i64 %total)
  %data = extractvalue %str-long %s, 2
  br label %loop
loop:
  %i = phi i64 [ 0, %entry ], [ %i.next, %loop ]
  %off = mul i64 %i, %len
  %dst = getelementptr i8, i8* %buf, i64 %off
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst, i8* %data, i64 %len, i1 0)
  %i.next = add i64 %i, 1
  %cont = icmp slt i64 %i.next, %count
  br i1 %cont, label %loop, label %end
end:
  %r0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %total, 0
  %r1 = insertvalue %str-long %r0, i64 %total, 1
  %r2 = insertvalue %str-long %r1, i8* %buf, 2
  ret %str-long %r2
}

define i8* @digits(i64 %abs, i8* %end) {
entry:
  br label %loop
loop:
  %p = phi i8* [ %end, %entry ], [ %pleft, %loop ]
  %n = phi i64 [ %abs, %entry ], [ %n2, %loop ]
  %digit = urem i64 %n, 10
  %ch = add i64 %digit, 48
  %ch8 = trunc i64 %ch to i8
  %pleft = getelementptr i8, i8* %p, i64 -1
  store i8 %ch8, i8* %pleft
  %n2 = udiv i64 %n, 10
  %last = icmp eq i64 %n2, 0
  br i1 %last, label %done, label %loop
done:
  ret i8* %pleft
}

; print_* helpers write ONLY the value (no trailing newline). The print
; emitter writes a single trailing newline after all args, matching the legacy
; print(a, b, c) -> "a b\n" semantics (space-separated, one newline).
define void @print_str(%str-long %s) {
entry:
  %len = extractvalue %str-long %s, 0
  %data = extractvalue %str-long %s, 2
  call i64 @write(i32 1, i8* %data, i64 %len)
  ret void
}

define void @print_i64(i64 %v) {
entry:
  %buf = alloca [24 x i8]
  %neg = icmp slt i64 %v, 0
  ; Magnitude without signed-overflow poison: sub i64 0, %v is undefined
  ; (poison) for i64.MIN (-9223372036854775808) and InstCombine folds it to
  ; garbage at runtime. Guard the subtraction with a select keyed on an
  ; explicit isMin check: the poison sub result is only chosen when isMin is
  ; true, so InstCombine cannot fold it unconditionally. For MIN the select
  ; returns its (unsigned) magnitude 2^63 directly.
  %isMin = icmp eq i64 %v, -9223372036854775808
  %negv = sub i64 0, %v
  %absNeg = select i1 %isMin, i64 9223372036854775808, i64 %negv
  %abs = select i1 %neg, i64 %absNeg, i64 %v
  %end = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %abs, i8* %end)
  br i1 %neg, label %negw, label %emit
negw:
  %sn = getelementptr i8, i8* %sp, i64 -1
  store i8 45, i8* %sn
  br label %emit
emit:
  %start = phi i8* [ %sn, %negw ], [ %sp, %entry ]
  %ep = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  %epp = ptrtoint i8* %ep to i64
  %startp = ptrtoint i8* %start to i64
  %len = sub i64 %epp, %startp
  call i64 @write(i32 1, i8* %start, i64 %len)
  ret void
}

@.dot = private constant [2 x i8] c".\00"
; @print_double formats a double with the SAME %g convention the legacy backend
; uses (legacy print(f64) routes through std f64-to-str, which replaced sprintf
; %g). We implement it BY HAND instead of calling libc snprintf, because the
; variadic snprintf call is mis-lowered by the opaque-pointer rewrite / opt on
; this toolchain: the double argument arrives corrupted, printing garbage like
; 5.3e-315 instead of 3.14. The hand-rolled formatter (a) applies the sign to
; the INTEGER part only and passes the absolute magnitude to @digits (which
; expects a non-negative i64), and (b) emits the fractional part with trailing
; zeros trimmed (matching %g: 1500 -> "1500", 3.14 -> "3.14", -2.5 -> "-2.5"),
; instead of the old fixed 6-decimal "1500.000000" / negative-garbage output.
define void @print_double(double %v) {
entry:
  %isneg = fcmp olt double %v, 0.0
  %negv = fneg double %v
  %abs = select i1 %isneg, double %negv, double %v
  %ipart = fptosi double %abs to i64
  %ipartd = sitofp i64 %ipart to double
  %fpart = fsub double %abs, %ipartd
  %buf = alloca [40 x i8]
  %end = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %ipart, i8* %end)
  %ep = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  %epp = ptrtoint i8* %ep to i64
  %spp = ptrtoint i8* %sp to i64
  %ilen = sub i64 %epp, %spp
  br i1 %isneg, label %negw, label %intw
negw:
  store i8 45, i8* %end
  %dw = call i64 @write(i32 1, i8* %end, i64 1)
  br label %intw
intw:
  %iw = call i64 @write(i32 1, i8* %sp, i64 %ilen)
  %isz = fcmp oeq double %fpart, 0.0
  br i1 %isz, label %done, label %frac
frac:
  %dotw = call i64 @write(i32 1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.dot, i64 0, i64 0), i64 1)
  %scaled = fmul double %fpart, 1.0e6
  %fi = fptosi double %scaled to i64
  %q1 = sdiv i64 %fi, 10
  %d0 = srem i64 %fi, 10
  %q2 = sdiv i64 %q1, 10
  %d1 = srem i64 %q1, 10
  %q3 = sdiv i64 %q2, 10
  %d2 = srem i64 %q2, 10
  %q4 = sdiv i64 %q3, 10
  %d3 = srem i64 %q3, 10
  %q5 = sdiv i64 %q4, 10
  %d4 = srem i64 %q4, 10
  %nz0 = icmp ne i64 %d0, 0
  %nz1 = icmp ne i64 %d1, 0
  %nz2 = icmp ne i64 %d2, 0
  %nz3 = icmp ne i64 %d3, 0
  %nz4 = icmp ne i64 %d4, 0
  %nz5 = icmp ne i64 %q5, 0
  %tz5 = select i1 %nz5, i64 5, i64 6
  %tz4 = select i1 %nz4, i64 4, i64 %tz5
  %tz3 = select i1 %nz3, i64 3, i64 %tz4
  %tz2 = select i1 %nz2, i64 2, i64 %tz3
  %tz1 = select i1 %nz1, i64 1, i64 %tz2
  %tz = select i1 %nz0, i64 0, i64 %tz1
  %p5 = icmp sge i64 5, %tz
  %p4 = icmp sge i64 4, %tz
  %p3 = icmp sge i64 3, %tz
  %p2 = icmp sge i64 2, %tz
  %p1 = icmp sge i64 1, %tz
  %p0 = icmp eq i64 %tz, 0
  %ob = alloca i8
  br i1 %p5, label %w5, label %c4
w5:
  %c5 = trunc i64 %q5 to i8
  %cc5 = add i8 %c5, 48
  store i8 %cc5, i8* %ob
  %ww5 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %c4
c4:
  br i1 %p4, label %w4, label %c3
w4:
  %c4v = trunc i64 %d4 to i8
  %cc4 = add i8 %c4v, 48
  store i8 %cc4, i8* %ob
  %ww4 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %c3
c3:
  br i1 %p3, label %w3, label %c2
w3:
  %c3v = trunc i64 %d3 to i8
  %cc3 = add i8 %c3v, 48
  store i8 %cc3, i8* %ob
  %ww3 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %c2
c2:
  br i1 %p2, label %w2, label %c1
w2:
  %c2v = trunc i64 %d2 to i8
  %cc2 = add i8 %c2v, 48
  store i8 %cc2, i8* %ob
  %ww2 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %c1
c1:
  br i1 %p1, label %w1, label %c0
w1:
  %c1v = trunc i64 %d1 to i8
  %cc1 = add i8 %c1v, 48
  store i8 %cc1, i8* %ob
  %ww1 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %c0
c0:
  br i1 %p0, label %w0, label %done
w0:
  %c0v = trunc i64 %d0 to i8
  %cc0 = add i8 %c0v, 48
  store i8 %cc0, i8* %ob
  %ww0 = call i64 @write(i32 1, i8* %ob, i64 1)
  br label %done
done:
  ret void
}

define void @print_bool(i1 %v) {
entry:
  br i1 %v, label %t, label %f
t:
  call i64 @write(i32 1, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.true, i64 0, i64 0), i64 1)
  ret void
f:
  call i64 @write(i32 1, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.false, i64 0, i64 0), i64 1)
  ret void
}

; Bare bool literals: legacy print(true)/print(false) emit "1"/"0" (the integer
; representation of the bool), NOT "true"/"false". Option-of-bool (?bool) is
; printed by @print_option_bool as "true"/"false" instead — legacy dispatches
; the two differently, so MIR must too.
@.true = private constant [1 x i8] c"1"
@.false = private constant [1 x i8] c"0"
@.nl = private constant [2 x i8] c"\0A\00"
@.sp = private constant [2 x i8] c" \00"

; Emit a trailing newline after every MIR print, matching legacy print.
define void @print_nl() {
entry:
  call i64 @write(i32 1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.nl, i64 0, i64 0), i64 1)
  ret void
}

; Space separator between print arguments (legacy print(a, b) -> "a b\n").
define void @print_space() {
entry:
  call i64 @write(i32 1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.sp, i64 0, i64 0), i64 1)
  ret void
}

; Best-effort option printer: "nil" for the nil tag, the inner value otherwise.
; Inner value field is read as i64 (exact for option-i64; an approximation for
; other element types, matching the v1 MIR print subset).
@.nilstr = private constant [3 x i8] c"nil"
define void @print_option(%option %o) {
entry:
  %os = alloca %option
  store %option %o, %option* %os
  %tp = getelementptr inbounds %option, %option* %os, i32 0, i32 0
  %tag = load i64, i64* %tp
  %isnil = icmp eq i64 %tag, 1
  br i1 %isnil, label %nil, label %some
nil:
  call i64 @write(i32 1, i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.nilstr, i64 0, i64 0), i64 3)
  ret void
some:
  %ps = getelementptr inbounds %option, %option* %os, i32 0, i32 1
` + fmt.Sprintf("  %%pp = bitcast %s* %%ps to i64*\n", c.optSlotLT) + `
  %inner = load i64, i64* %pp
  call void @print_i64(i64 %inner)
  ret void
}

; @print_option_bool mirrors @print_option but formats the inner value of a
; ?bool as "true"/"false" instead of the raw i64 payload (1/0) — matching the
; legacy backend's bool print for option types. The flat %option layout loses
; the element type, so the print-call site must route ?bool here explicitly
; (see emitCall's print special-case), never through the generic @print_option.
define void @print_option_bool(%option %o) {
entry:
  %os = alloca %option
  store %option %o, %option* %os
  %tp = getelementptr inbounds %option, %option* %os, i32 0, i32 0
  %tag = load i64, i64* %tp
  %isnil = icmp eq i64 %tag, 1
  br i1 %isnil, label %nil, label %some
nil:
  call i64 @write(i32 1, i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.nilstr, i64 0, i64 0), i64 3)
  ret void
some:
  %ps = getelementptr inbounds %option, %option* %os, i32 0, i32 1
` + fmt.Sprintf("  %%pp = bitcast %s* %%ps to i64*\n", c.optSlotLT) + `
  %inner = load i64, i64* %pp
  %b = trunc i64 %inner to i1
  br i1 %b, label %ot, label %of
ot:
  call i64 @write(i32 1, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.otrue, i64 0, i64 0), i64 4)
  ret void
of:
  call i64 @write(i32 1, i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.ofalse, i64 0, i64 0), i64 5)
  ret void
}

; ?bool literals for @print_option_bool: legacy print(?bool) of ok(true)/ok(false)
; emits "true"/"false" (distinct from bare-bool print's "1"/"0"). Trailing NUL
; keeps the inbounds GEP in-bounds; write length is the visible char count.
@.otrue = private constant [5 x i8] c"true\00"
@.ofalse = private constant [6 x i8] c"false\00"

; eprint_* mirror print_* but write to stderr (fd 2) and append a newline,
; matching the legacy io.errln behavior. Reuse @digits for itoa.
define void @eprint_i64(i64 %v) {
entry:
  %buf = alloca [24 x i8]
  %neg = icmp slt i64 %v, 0
  %isMin = icmp eq i64 %v, -9223372036854775808
  %negv = sub i64 0, %v
  %absNeg = select i1 %isMin, i64 9223372036854775808, i64 %negv
  %abs = select i1 %neg, i64 %absNeg, i64 %v
  %end = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %abs, i8* %end)
  br i1 %neg, label %negw, label %emit
negw:
  %sn = getelementptr i8, i8* %sp, i64 -1
  store i8 45, i8* %sn
  br label %emit
emit:
  %start = phi i8* [ %sn, %negw ], [ %sp, %entry ]
  %ep = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  %epp = ptrtoint i8* %ep to i64
  %startp = ptrtoint i8* %start to i64
  %len = sub i64 %epp, %startp
  call i64 @write(i32 2, i8* %start, i64 %len)
  call void @eprint_nl()
  ret void
}

define void @eprint_byte(i8 %b) {
entry:
  %buf = alloca [1 x i8]
  store i8 %b, i8* %buf
  call i64 @write(i32 2, i8* %buf, i64 1)
  call void @eprint_nl()
  ret void
}

define void @eprint_nl() {
entry:
  call i64 @write(i32 2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.nl, i64 0, i64 0), i64 1)
  ret void
}

; eprint_str writes ONLY the string (no trailing newline) so the named-format
; lowering can emit each format segment separately and append a single newline
; at the end — mirroring legacy callNamedFormat's io.err-per-segment behavior.
define void @eprint_str(%str-long %s) {
entry:
  %len = extractvalue %str-long %s, 0
  %data = extractvalue %str-long %s, 2
  call i64 @write(i32 2, i8* %data, i64 %len)
  ret void
}

; _mir_str_concat: heap-concatenate two strings into a fresh %str-long. Used by
; the named-format lowering to build the str RESULT of format()/sprintf(), which
; legacy produces with concatStrLongPtrs. Named with a _mir_ prefix for the same
; reason as _mir_str_repeat: "str_concat" is already a runtimeFns entry and a
; same-named definition would trip LLVM verification ("invalid redefinition").
define %str-long @_mir_str_concat(%str-long %a, %str-long %b) {
entry:
  %alen = extractvalue %str-long %a, 0
  %blen = extractvalue %str-long %b, 0
  %total = add i64 %alen, %blen
  %size = add i64 %total, 1
  %buf = call i8* @malloc(i64 %size)
  %adata = extractvalue %str-long %a, 2
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %adata, i64 %alen, i1 false)
  %bdst = getelementptr i8, i8* %buf, i64 %alen
  %bdata = extractvalue %str-long %b, 2
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %bdst, i8* %bdata, i64 %blen, i1 false)
  %nul = getelementptr i8, i8* %buf, i64 %total
  store i8 0, i8* %nul
  %r0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %total, 0
  %r1 = insertvalue %str-long %r0, i64 %total, 1
  %r2 = insertvalue %str-long %r1, i8* %buf, 2
  ret %str-long %r2
}

; --- C string bridge (CLibCall builtins) -------------------------------------
; Nolang strings are (len, cap, data) triples with NO NUL terminator, and libc
; static buffers must never be adopted by a %str-long (it would be freed). These
; two helpers are the only places where the two worlds meet.

declare i64 @strlen(i8*)

; str_cstr: NUL-terminated heap copy of a container's bytes, for C functions
; that take a char*. The caller frees the result.
define i8* @str_cstr(%str-long %s) {
entry:
  %len = extractvalue %str-long %s, 0
  %data = extractvalue %str-long %s, 2
  %sz = add i64 %len, 1
  %buf = call i8* @malloc(i64 %sz)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %data, i64 %len, i1 0)
  %np = getelementptr inbounds i8, i8* %buf, i64 %len
  store i8 0, i8* %np
  ret i8* %buf
}

; str_from_cstr: adopt a C string into a %str-long by copying it onto the heap.
; NULL maps to the empty string, so an unset get-env keeps its nil semantics.
define %str-long @str_from_cstr(i8* %p) {
entry:
  %isnull = icmp eq i8* %p, null
  br i1 %isnull, label %nil, label %copy
nil:
  %z = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 0, 0
  ret %str-long %z
copy:
  %len = call i64 @strlen(i8* %p)
  %sz = add i64 %len, 1
  %buf = call i8* @malloc(i64 %sz)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %p, i64 %len, i1 0)
  %s0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %len, 0
  %s1 = insertvalue %str-long %s0, i64 %len, 1
  %s2 = insertvalue %str-long %s1, i8* %buf, 2
  ret %str-long %s2
}

; str_clone: deep-copy a %str-long onto the heap. Used when an owned string
; field is READ out of a struct (emitGetField): the read is a by-value copy of
; the {len,cap,data} triple that shares the underlying heap buffer with the
; field. Without an independent copy, the read temp and the struct field would
; both be freed by the drop pass -> double free. Cloning gives the read its own
; buffer, matching nolang copy-on-read string semantics and keeping the field's
; drop independent.
define %str-long @str_clone(%str-long %s) {
entry:
  %len = extractvalue %str-long %s, 0
  %data = extractvalue %str-long %s, 2
  %isnull = icmp eq i8* %data, null
  br i1 %isnull, label %nil, label %copy
nil:
  %z = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 0, 0
  ret %str-long %z
copy:
  %sz = add i64 %len, 1
  %buf = call i8* @malloc(i64 %sz)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %buf, i8* %data, i64 %len, i1 0)
  %s0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %len, 0
  %s1 = insertvalue %str-long %s0, i64 %len, 1
  %s2 = insertvalue %str-long %s1, i8* %buf, 2
  ret %str-long %s2
}

; str_from_i64: render an i64 as a freshly-allocated %str-long (decimal, signed).
; Used by emitCmp when an integer is compared against a string, and reusable for
; string interpolation (str<-int) later. Mirrors print_i64's digit emission.
define %str-long @str_from_i64(i64 %v) {
entry:
  %buf = alloca [24 x i8]
  %neg = icmp slt i64 %v, 0
  %negv = sub i64 0, %v
  %abs = select i1 %neg, i64 %negv, i64 %v
  %end = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %abs, i8* %end)
  br i1 %neg, label %negw, label %emit
negw:
  %sn = getelementptr i8, i8* %sp, i64 -1
  store i8 45, i8* %sn
  br label %emit
emit:
  %start = phi i8* [ %sn, %negw ], [ %sp, %entry ]
  %ep = getelementptr [24 x i8], [24 x i8]* %buf, i64 0, i64 23
  %epp = ptrtoint i8* %ep to i64
  %startp = ptrtoint i8* %start to i64
  %len = sub i64 %epp, %startp
  %nbuf = call i8* @malloc(i64 %len)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %nbuf, i8* %start, i64 %len, i1 0)
  %s0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %len, 0
  %s1 = insertvalue %str-long %s0, i64 %len, 1
  %s2 = insertvalue %str-long %s1, i8* %nbuf, 2
  ret %str-long %s2
}

; str_from_char: render a single i8 (char/byte) as a 1-character %str-long. Used
; by emitCmp for char/byte == str comparisons so the operands share the
; %str-long type and go through @str_eq instead of an illegal icmp i8, %str-long.
define %str-long @str_from_char(i8 %c) {
entry:
  %nbuf = call i8* @malloc(i64 1)
  store i8 %c, i8* %nbuf
  %s0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 1, 0
  %s1 = insertvalue %str-long %s0, i64 1, 1
  %s2 = insertvalue %str-long %s1, i8* %nbuf, 2
  ret %str-long %s2
}

; str_from_double: render a double as a freshly-allocated %str-long, mirroring
; print_double's %g convention (sign on integer part, trailing zeros trimmed).
; Used by emitCall's i64/double -> str auto-coercion when a double value is
; passed to a str parameter (e.g. io.out(3.14)).
define %str-long @str_from_double(double %v) {
entry:
  %isneg = fcmp olt double %v, 0.0
  %negv = fneg double %v
  %abs = select i1 %isneg, double %negv, double %v
  %ipart = fptosi double %abs to i64
  %ipartd = sitofp i64 %ipart to double
  %fpart = fsub double %abs, %ipartd
  %buf = alloca [40 x i8]
  %end = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %ipart, i8* %end)
  %epp = ptrtoint i8* %end to i64
  %spp = ptrtoint i8* %sp to i64
  %ilen = sub i64 %epp, %spp
  %wpos = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 0
  br i1 %isneg, label %negw, label %intw
negw:
  store i8 45, i8* %wpos
  %wnext = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 1
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %wnext, i8* %sp, i64 %ilen, i1 0)
  %tlen_neg = add i64 %ilen, 1
  br label %frac
intw:
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %wpos, i8* %sp, i64 %ilen, i1 0)
  %tlen_int = add i64 %ilen, 0
  br label %frac
frac:
  %tlen = phi i64 [ %tlen_neg, %negw ], [ %tlen_int, %intw ]
  %isz = fcmp oeq double %fpart, 0.0
  br i1 %isz, label %done, label %fracbuild
fracbuild:
  %dotpos = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen
  store i8 46, i8* %dotpos
  %tlen2 = add i64 %tlen, 1
  %scaled = fmul double %fpart, 1.0e6
  %fi = fptosi double %scaled to i64
  %q1 = sdiv i64 %fi, 10
  %d0 = srem i64 %fi, 10
  %q2 = sdiv i64 %q1, 10
  %d1 = srem i64 %q1, 10
  %q3 = sdiv i64 %q2, 10
  %d2 = srem i64 %q2, 10
  %q4 = sdiv i64 %q3, 10
  %d3 = srem i64 %q3, 10
  %q5 = sdiv i64 %q4, 10
  %d4 = srem i64 %q4, 10
  %nz0 = icmp ne i64 %d0, 0
  %nz1 = icmp ne i64 %d1, 0
  %nz2 = icmp ne i64 %d2, 0
  %nz3 = icmp ne i64 %d3, 0
  %nz4 = icmp ne i64 %d4, 0
  %nz5 = icmp ne i64 %q5, 0
  %tz5 = select i1 %nz5, i64 5, i64 6
  %tz4 = select i1 %nz4, i64 4, i64 %tz5
  %tz3 = select i1 %nz3, i64 3, i64 %tz4
  %tz2 = select i1 %nz2, i64 2, i64 %tz3
  %tz1 = select i1 %nz1, i64 1, i64 %tz2
  %tz = select i1 %nz0, i64 0, i64 %tz1
  %p5 = icmp sge i64 5, %tz
  %p4 = icmp sge i64 4, %tz
  %p3 = icmp sge i64 3, %tz
  %p2 = icmp sge i64 2, %tz
  %p1 = icmp sge i64 1, %tz
  br i1 %p5, label %w5, label %c4
w5:
  %c5 = trunc i64 %q5 to i8
  %cc5 = add i8 %c5, 48
  %wp5 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen2
  store i8 %cc5, i8* %wp5
  %tlen3 = add i64 %tlen2, 1
  br label %c4
c4:
  %tlen4 = phi i64 [ %tlen3, %w5 ], [ %tlen2, %fracbuild ]
  br i1 %p4, label %w4, label %c3
w4:
  %c4v = trunc i64 %d4 to i8
  %cc4 = add i8 %c4v, 48
  %wp4 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen4
  store i8 %cc4, i8* %wp4
  %tlen5 = add i64 %tlen4, 1
  br label %c3
c3:
  %tlen6 = phi i64 [ %tlen5, %w4 ], [ %tlen4, %c4 ]
  br i1 %p3, label %w3, label %c2
w3:
  %c3v = trunc i64 %d3 to i8
  %cc3 = add i8 %c3v, 48
  %wp3 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen6
  store i8 %cc3, i8* %wp3
  %tlen7 = add i64 %tlen6, 1
  br label %c2
c2:
  %tlen8 = phi i64 [ %tlen7, %w3 ], [ %tlen6, %c3 ]
  br i1 %p2, label %w2, label %c1
w2:
  %c2v = trunc i64 %d2 to i8
  %cc2 = add i8 %c2v, 48
  %wp2 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen8
  store i8 %cc2, i8* %wp2
  %tlen9 = add i64 %tlen8, 1
  br label %c1
c1:
  %tlen10 = phi i64 [ %tlen9, %w2 ], [ %tlen8, %c2 ]
  br i1 %p1, label %w1, label %c0
w1:
  %c1v = trunc i64 %d1 to i8
  %cc1 = add i8 %c1v, 48
  %wp1 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen10
  store i8 %cc1, i8* %wp1
  %tlen11 = add i64 %tlen10, 1
  br label %c0
c0:
  %tlen12 = phi i64 [ %tlen11, %w1 ], [ %tlen10, %c1 ]
  %p0 = icmp eq i64 %tz, 0
  br i1 %p0, label %w0, label %done
w0:
  %c0v = trunc i64 %d0 to i8
  %cc0 = add i8 %c0v, 48
  %wp0 = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 %tlen12
  store i8 %cc0, i8* %wp0
  %tlen13 = add i64 %tlen12, 1
  br label %done
done:
  %flen = phi i64 [ %tlen, %frac ], [ %tlen12, %c0 ], [ %tlen13, %w0 ]
  %nbuf = call i8* @malloc(i64 %flen)
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %nbuf, i8* %wpos, i64 %flen, i1 0)
  %s0 = insertvalue %str-long { i64 0, i64 0, i8* null }, i64 %flen, 0
  %s1 = insertvalue %str-long %s0, i64 %flen, 1
  %s2 = insertvalue %str-long %s1, i8* %nbuf, 2
  ret %str-long %s2
}

; --- nolang.* runtime helpers ------------------------------------------------
; The builtin table refers to these names as if they were plain libc symbols
; (CLibCall{FuncName: "nolang.now_ms"}), but libc has never provided them: the
; legacy backend defines them in its prelude (build/llvm/decl.go). MIR used to
; only *declare* them, so every program touching the clock or sleeping linked
; against symbols nobody defined -> "Undefined symbols for architecture arm64".
; Defining them here (matching the rest of this prelude) makes the CLibCall
; path work unchanged.

declare i32 @gettimeofday(i8*, i8*)
declare i32 @clock_gettime(i32, i8*)
declare i32 @usleep(i32)
declare i32 @nanosleep(i8*, i8*)

define i64 @nolang.now_s() {
entry:
  %tv = alloca [16 x i8]
  %tv.ptr = bitcast [16 x i8]* %tv to i8*
  call i32 @gettimeofday(i8* %tv.ptr, i8* null)
  %sec.ptr = bitcast [16 x i8]* %tv to i64*
  %sec = load i64, i64* %sec.ptr
  ret i64 %sec
}

define i64 @nolang.now_ms() {
entry:
  %tv = alloca [16 x i8]
  %tv.ptr = bitcast [16 x i8]* %tv to i8*
  call i32 @gettimeofday(i8* %tv.ptr, i8* null)
  %sec.ptr = bitcast [16 x i8]* %tv to i64*
  %sec = load i64, i64* %sec.ptr
  %usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
  %usec = load i64, i64* %usec.ptr
  %sec.ms = mul i64 %sec, 1000
  %usec.ms = sdiv i64 %usec, 1000
  %result = add i64 %sec.ms, %usec.ms
  ret i64 %result
}

define i64 @nolang.now_us() {
entry:
  %tv = alloca [16 x i8]
  %tv.ptr = bitcast [16 x i8]* %tv to i8*
  call i32 @gettimeofday(i8* %tv.ptr, i8* null)
  %sec.ptr = bitcast [16 x i8]* %tv to i64*
  %sec = load i64, i64* %sec.ptr
  %usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
  %usec = load i64, i64* %usec.ptr
  %sec.us = mul i64 %sec, 1000000
  %result = add i64 %sec.us, %usec
  ret i64 %result
}

define i64 @nolang.now_ns() {
entry:
  %ts = alloca [16 x i8]
  %ts.ptr = bitcast [16 x i8]* %ts to i8*
  call i32 @clock_gettime(i32 0, i8* %ts.ptr)
  %sec.ptr = bitcast [16 x i8]* %ts to i64*
  %sec = load i64, i64* %sec.ptr
  %nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
  %nsec = load i64, i64* %nsec.ptr
  %sec.ns = mul i64 %sec, 1000000000
  %result = add i64 %sec.ns, %nsec
  ret i64 %result
}

define void @nolang.sleep_us(i64 %us) {
entry:
  %us.trunc = trunc i64 %us to i32
  call i32 @usleep(i32 %us.trunc)
  ret void
}

define i64 @nolang.sleep_s(i64 %sec) {
entry:
  %req = alloca [16 x i8]
  %sec.ptr = bitcast [16 x i8]* %req to i64*
  store i64 %sec, i64* %sec.ptr
  %nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
  store i64 0, i64* %nsec.ptr
  %req.ptr = bitcast [16 x i8]* %req to i8*
  call i32 @nanosleep(i8* %req.ptr, i8* null)
  ret i64 0
}

define void @nolang.sleep_ns(i64 %ns) {
entry:
  %req = alloca [16 x i8]
  %sec = sdiv i64 %ns, 1000000000
  %nsec = srem i64 %ns, 1000000000
  %sec.ptr = bitcast [16 x i8]* %req to i64*
  store i64 %sec, i64* %sec.ptr
  %nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1
  store i64 %nsec, i64* %nsec.ptr
  %req.ptr = bitcast [16 x i8]* %req to i8*
  call i32 @nanosleep(i8* %req.ptr, i8* null)
  ret void
}
`)
}

func (c *codegen) emitGlobals() {
	// Module-level constant bindings (SBOX, TLS-FINISHED-SIZE, perm-600, ...).
	for _, g := range c.mod.Globals {
		// `#{embed='file'}` binding: emit the embedded bytes as a private
		// constant byte array. The `%vec` global below references it through a
		// `ptrtoint([N x i8]* @.embed.<name> to i64)` initializer, so the
		// slice's data pointer targets constant memory (never freed).
		if g.EmbedBytes != nil {
			c.sb.WriteString(fmt.Sprintf("@.embed.%s = private constant [%d x i8] c\"%s\"\n",
				g.Name, len(g.EmbedBytes), dataStr(string(g.EmbedBytes))))
		}
		if g.ConstText == "" {
			// Uninitialized module-level variable (e.g. `ga-priv [32]byte`
			// declared at top level outside an explicit `fn main`, then
			// written inside `main`). nolang zero-initializes such bindings and
			// they are mutable, so emit a DEFINED mutable global
			// (`private global <T> zeroinitializer`) — NOT an `external
			// constant` declaration, which leaves an undefined symbol that
			// fails to link under NOLANG_MIR=3 (test-x25519-keypair-diff:
			// `_ga-priv` / `_gb-priv` undefined). The old `external constant`
			// form was only safe under NOLANG_MIR=2 (strangler-fig fallback);
			// with fallback disabled it must be a real definition.
			lt, _ := c.ptype(g.Init)
			if lt == "" || lt == "void" {
				// Type not resolvable: keep the (benign) external form rather
				// than emit malformed IR.
				c.sb.WriteString(fmt.Sprintf("@%s = external constant %s\n", g.Name, lt))
				continue
			}
			c.sb.WriteString(fmt.Sprintf("@%s = private global %s zeroinitializer\n", g.Name, lt))
			continue
		}
		// Module-level bindings (SBOX, data, ...) are mutable in nolang — a
		// top-level `data [4]i64 = [...]` can be reassigned or zeroed
		// (`data.zero()`), which writes through the global's address. Emitting
		// it as `constant` makes that store/llvm.memset a no-op (writing to a
		// constant global is dropped), so the mutation silently disappears.
		// Emit mutable `global` instead; only the string-literal globals
		// (strGlobals) stay `constant`, and those are never written.
		c.sb.WriteString(fmt.Sprintf("@%s = private global %s\n", g.Name, g.ConstText))
	}
	var vids []ValueID
	for vid := range c.strGlobals {
		vids = append(vids, vid)
	}
	sort.Slice(vids, func(i, j int) bool { return vids[i] < vids[j] })
	for _, vid := range vids {
		g := c.strGlobals[vid]
		c.sb.WriteString(fmt.Sprintf("%s = private constant [%d x i8] c\"%s\"\n", g.name, g.n, dataStr(g.str)))
	}
}

// dataStr converts a go string to an LLVM char-array initializer where every
// byte is a \XX hex escape (always safe, no interpretation surprises).
//
// IMPORTANT: it must iterate over BYTES, not runes. A Nolang string holds raw
// UTF-8 bytes (e.g. the DES test vector '\x01\x23\x45\x67\x89\xab\xcd\xef', or
// arbitrary Chinese text). If we ranged over runes, each byte ≥0x80 that is not
// valid UTF-8 would be decoded to U+FFFD, and `dataStr` would emit a 4-hex-digit
// \FFFD escape that LLVM stores as THREE bytes — while the surrounding constant
// array is sized by len(s) (the raw byte count). The dimension (byte count)
// would then disagree with the emitted content (runes expanded to bytes), and
// `opt` rejects the module ("constant expression type mismatch"). Ranging over
// bytes keeps the escape count equal to len(s), so the array size and content
// always agree.
func dataStr(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteString(fmt.Sprintf(`\%02X`, s[i]))
	}
	return b.String()
}

func (c *codegen) emitEntry(main *Function) {
	// The nolang `main` may declare an out parameter, which is its process exit
	// code: `main = () (out i64) { ... out = 2 }`. `_nolang_main` is emitted with
	// that result as a leading out-pointer (see the by-reference ABI), so the C
	// entry MUST allocate the slot, pass its address, and return the value as the
	// i32 status. Emitting `call void @_nolang_main()` with no argument dropped
	// the out pointer: the callee's `store ... , i64* %p0` then targets a
	// garbage/null pointer, which -O3 folds to `unreachable` (SIGTRAP) and the
	// epilogue (globals free + ret) is deleted — the crash behind
	// tests/mem-safety/cross-fn-str-return-dfree.no.
	c.sb.WriteString("define i32 @main(i32 %0, i8** %1) {\nentry:\n")
	if main != nil && len(main.ResultParams) > 0 {
		rt, _ := c.ptype(main.ResultParams[0])
		if rt == "" {
			rt = "i64"
		}
		c.sb.WriteString(fmt.Sprintf("  %%rc = alloca %s\n", rt))
		c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %%rc\n", rt, rt))
		c.sb.WriteString(fmt.Sprintf("  call void @_nolang_main(%s* %%rc)\n", rt))
		switch rt {
		case "i32":
			c.sb.WriteString("  %rv = load i32, i32* %rc\n")
			c.sb.WriteString("  ret i32 %rv\n")
		case "i64":
			c.sb.WriteString("  %rv = load i64, i64* %rc\n")
			c.sb.WriteString("  %rs = trunc i64 %rv to i32\n")
			c.sb.WriteString("  ret i32 %rs\n")
		default:
			c.sb.WriteString("  ret i32 0\n")
		}
		c.sb.WriteString("}\n")
		return
	}
	c.sb.WriteString("  call void @_nolang_main()\n")
	c.sb.WriteString("  ret i32 0\n}\n")
}

// ---- function emission ----

func (c *codegen) emitFunc(f *Function) error {
	c.cf = f.ID
	c.curFn = f
	c.resultParam = map[ValueID]bool{}
	for _, rp := range f.ResultParams {
		c.resultParam[rp] = true
	}

	// Reject unsupported types (struct/vec/option/ptr) for a clean fallback.
	for _, p := range f.Params {
		// Function-pointer parameters are supported via the by-reference ABI
		// (see the param-declaration branch below); their void(...)* type is
		// not in supportedLLVM's scalar/aggregate list, so exempt them here.
		if pv := c.mod.Value(p); pv != nil {
			if ty := c.mod.Type(pv.Type); ty != nil && ty.Kind == KindFunc {
				continue
			}
		}
		lt, _ := c.ptype(p)
		if !supportedLLVM(lt) {
			c.fail("unsupported param type %s in func %s", lt, f.Name)
			return fmt.Errorf("unsupported param type %s in func %s", lt, f.Name)
		}
	}
	for _, bid := range f.Blocks {
		blk := c.mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := c.mod.Inst(iid)
			if inst != nil && inst.Dst > NoVal {
				// Build the defInst map: record which instruction produced
				// each value, so emitCallBody can detect a method-call
				// receiver that is the result of OpGetField and pass the
				// struct field's GEP address directly (so mutations
				// propagate back to the struct, e.g. `m.rows.push(x)`).
				c.defInst[inst.Dst] = iid
				// Function-pointer constants (a fn name passed as a callback)
				// have a void(...)* MIR type that supportedLLVM does not list;
				// they are referenced by @name directly (loadVal) and need no
				// special handling, so skip the gate for them.
				if dv := c.mod.Value(inst.Dst); dv != nil {
					if ty := c.mod.Type(dv.Type); ty != nil && ty.Kind == KindFunc {
						continue
					}
				}
				lt, _ := c.ptype(inst.Dst)
				if !supportedLLVM(lt) {
					c.fail("unsupported value type %s in func %s", lt, f.Name)
					return fmt.Errorf("unsupported value type %s in func %s", lt, f.Name)
				}
			}
		}
	}

	// Build LLVM parameter declarations (f.Params order). Owned -> pointer param.
	// Result (out-) parameters are ALWAYS passed as pointers regardless of
	// ownership: the caller provides an out-pointer that emitReturn writes back
	// into, so declaring them by-value (as the non-owned branch would) clashes
	// with the `store ...* %pp` in emitReturn.
	var decls []string
	c.paramPtr = map[ValueID]string{}
	idx := 0
	for _, p := range f.Params {
		lt, owned := c.ptype(p)
		pk := KindUnknown
		if v := c.mod.Value(p); v != nil {
			if ty := c.mod.Type(v.Type); ty != nil {
				pk = ty.Kind
			}
		}
		pn := fmt.Sprintf("%%p%d", idx)
		if c.resultParam[p] {
			decls = append(decls, lt+"* "+pn)
		} else if pk == KindFunc {
			// Function-pointer parameters are passed BY REFERENCE: the caller
			// hands a `void(...)**` (address of the slot holding the fn ptr),
			// so the callee receives a pointer and loads the fn ptr from it —
			// identical to how the by-ref argument ABI passes every other
			// parameter. Declaring it by-value (`void()* %pN`) would clash with
			// the caller's `void()**` argument and reject at opt/link time.
			decls = append(decls, lt+"* "+pn)
		} else if byPointerLLVM(lt, owned) {
			decls = append(decls, lt+"* "+pn)
		} else {
			decls = append(decls, lt+" "+pn)
		}
		c.paramPtr[p] = pn
		idx++
	}
	name := c.fname[f.ID]
	c.sb.WriteString(fmt.Sprintf("define void @%s(%s) {\n", name, strings.Join(decls, ", ")))

	// Pre-create alloca slots for all values (params + Dst) in the entry block.
	// Start from a fresh map but re-seed module-global slots (@name) so that
	// references to module constants (e.g. crypto/aes.SBOX) inside any function
	// resolve to their @global address (emitIndex/emitGetField GEP from it).
	c.valSlot = map[ValueID]string{}
	for gid, gslot := range c.globalSlots {
		c.valSlot[gid] = gslot
	}
	entry := f.Blocks[0]
	c.sb.WriteString(fmt.Sprintf("%s.bb0:\n", name))
	slotIdx := 0
	allocaFor := func(v ValueID) string {
		if s, ok := c.valSlot[v]; ok {
			return s
		}
		lt, owned := c.ptype(v)
		if lt == "void" {
			// Void values carry no data; never allocate a slot (LLVM rejects
			// `alloca void`). They are dead bindings such as `let x = voidFn()`
			// and are skipped by consumers — loadVal returns ("void","undef").
			return ""
		}
		s := fmt.Sprintf("%%v%d.s", slotIdx)
		slotIdx++
		c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", s, lt))
		// Zero-initialize heap-owning slots so a value that is never written
		// (most notably an OUT-parameter left untouched by an empty function
		// body, e.g. the macOS `net.ifconfig-list` stub that returns an empty
		// list) is a valid, free-able "empty" value rather than uninitialized
		// garbage. A %vec/%str-long/%option left as garbage and later freed by
		// emitDrop/@vec_free/@str_free would free a random pointer and abort
		// (trace/BPT trap). Legacy zero-initializes locals, so this is required
		// for parity. Written Dst/params overwrite the zero below, so this is
		// safe.
		//
		// An option slot is the ONE type whose default is not its zero value:
		// `%option` is `{ i64 tag, payload }` with tag 0 = ok, 2 = err and
		// 1 = nil, so `zeroinitializer` reads back as `ok(0)` — a non-nil
		// some(zero). Store the nil discriminant instead. This matters most for
		// a named OUT-parameter that the body returns without assigning (e.g.
		// `hashmap.get` on a miss, which just falls out of its probe loop): the
		// write-back then published either uninitialized garbage or a stale
		// `ok` from a previously reused slot, so a removed key still came back
		// "found" carrying the previous call's payload
		// (tests/mem-safety/map-tombstone.no, tests/test-map-generics.no).
		// Written Dst/params overwrite this, so the extra store is dead for
		// every value that is actually assigned.
		if isOptionType(lt) {
			c.optStoreTag(s, 1)
		} else if owned || c.structSlotNeedsZero(lt) {
			// A STRUCT THAT GETS A DROP must start zeroed even though it is
			// not `owned` (Type.Owned is false for a struct — it is only true
			// for str/vec/[]T/map/?owned). Before structs with inline owned
			// `str` leaves had a destructor this was harmless: an
			// uninitialized slot was simply never read. With the destructor,
			// a struct local that every branch leaves unwritten (a loop that
			// only assigns it in one arm) frees stack garbage —
			// `free(0xffffffffffffffff)`, "pointer being freed was not
			// allocated", abort. Verified: tests/test-diff-debug.no.
			// Guarded by the SAME predicate as the drop, so a struct that is
			// never dropped pays nothing.
			c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %s\n", lt, lt, s))
		}
		c.valSlot[v] = s
		return s
	}
	// By-pointer parameters (owned values, aggregates `[N x T]`, structs /
	// %vec / %option, and already-pointer types) are passed by reference. Alias
	// their slot directly to the incoming param pointer so the body reads AND
	// writes THROUGH the caller's storage — matching the legacy codegen and
	// Nolang's reference semantics for arrays/structs. This is essential for
	// mutation propagation: `inner-write(s [32]byte) { s[i] = ... }` must modify
	// the caller's array, and `self.field = ...` inside a method must update the
	// caller's receiver. Copying the param into a fresh local alloca (the old
	// behaviour) silently discards those writes — the test-arr-param "first 8
	// bytes zero" bug, and the by-value-vs-by-reference bug that broke mutating
	// std methods (vec.insert / vec.remove / vec.reverse …).
	//
	// Only by-pointer params are aliased. Scalar (non-owned, non-pointer) params
	// are declared by value (`i64 %p0`); their `paramPtr` is a by-value name, so
	// aliasing valSlot to it and then `load i64, i64* %p0` would be a type error
	// opt rejects ("%p0 defined with type 'i64' but expected 'ptr'"). Scalars are
	// copied into a normal slot below. The receiver is just a (usually
	// by-pointer) param, so it is covered by this same loop — no special case.
	for _, p := range f.Params {
		if c.resultParam[p] {
			// self (the first result param of a method) IS aliased to the
			// caller's pointer so in-body mutations propagate directly.
			// Other result params are written back by emitReturn.
			if f.IsMethod {
				isSelf := false
				if len(f.ResultParams) > 0 && f.ResultParams[0] == p {
					isSelf = true
				}
				if isSelf {
					c.valSlot[p] = c.paramPtr[p]
					continue
				}
			}
			continue // other out-params are written back by emitReturn, not aliased
		}
		plt, powned := c.ptype(p)
		if powned || byPointerLLVM(plt, powned) || strings.HasSuffix(plt, "*") {
			c.valSlot[p] = c.paramPtr[p]
		}
	}
	// params
	for _, p := range f.Params {
		allocaFor(p)
	}
	// all dst across blocks
	for _, bid := range f.Blocks {
		blk := c.mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := c.mod.Inst(iid)
			if inst == nil {
				continue
			}
			if inst.Dst > NoVal {
				allocaFor(inst.Dst)
			}
			// Multi-result calls (x, y = f()) carry additional result values
			// in Results[1:]; each needs its own alloca slot or loadVal of the
			// second+ target returns "void" and the consumer (e.g. print(b))
			// fails.
			for _, rv := range inst.Results {
				if rv > NoVal {
					allocaFor(rv)
				}
			}
		}
	}
	// store incoming params into slots (skip out-params and by-pointer params)
	for _, p := range f.Params {
		if c.resultParam[p] {
			continue // out-param: written back by emitReturn
		}
		lt, owned := c.ptype(p)
		slot := c.valSlot[p]
		if lt == "void" || slot == "" {
			continue
		}
		// By-pointer params were aliased to the caller's pointer above, so their
		// slot IS that pointer; the body reads/writes through it. Do NOT copy
		// into a local slot (that would discard the caller's writes). Scalar
		// (by-value) params are aliased to nothing, so their slot is a fresh
		// alloca; store the incoming value into it here.
		if owned || byPointerLLVM(lt, owned) || strings.HasSuffix(lt, "*") {
			continue
		}
		pin := c.paramPtr[p]
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, pin, lt, slot))
	}

	// Assign LLVM labels for EVERY block BEFORE emitting any of them. LLVM text
	// allows forward references, but our emitTerm reads labelFor[target] at the
	// moment the terminator is written; if labels are populated lazily during the
	// emission loop, a block that terminates to a later block sees an empty label
	// (producing the malformed `br label %`). Pre-populating fixes that.
	c.labelFor = map[BlockID]string{}
	c.labelFor[entry] = fmt.Sprintf("%s.bb0", name)
	for bi := 1; bi < len(f.Blocks); bi++ {
		bid := f.Blocks[bi]
		c.labelFor[bid] = fmt.Sprintf("%s.bb%d", name, bi)
	}

	// emit entry block insts + term
	if err := c.emitBlock(f, entry, allocaFor); err != nil {
		return err
	}
	// remaining blocks
	for bi := 1; bi < len(f.Blocks); bi++ {
		bid := f.Blocks[bi]
		blk := c.mod.Block(bid)
		if blk == nil {
			continue
		}
		c.sb.WriteString(fmt.Sprintf("%s.bb%d:\n", name, bi))
		if err := c.emitBlock(f, bid, allocaFor); err != nil {
			return err
		}
	}
	c.sb.WriteString("}\n")
	c.cf = NoFunc
	return nil
}

func (c *codegen) emitBlock(f *Function, bid BlockID, allocaFor func(ValueID) string) error {
	blk := c.mod.Block(bid)
	if blk == nil {
		return nil
	}
	for _, iid := range blk.Insts {
		inst := c.mod.Inst(iid)
		if inst == nil {
			continue
		}
		if err := c.emitInst(f, inst, allocaFor); err != nil {
			return err
		}
	}
	if blk.Term != nil {
		if err := c.emitTerm(f, blk.Term); err != nil {
			return err
		}
	}
	return nil
}

// loadVal returns the LLVM type and a register name holding the value of v. Unlike
// a bare `load ...` expression (which is illegal as a call/arith operand in LLVM
// IR), it materializes the load into a uniquely-named register so the result can be
// used anywhere a value is expected. A single value may be loaded multiple times;
// each call emits a fresh register to avoid name collisions.
func (c *codegen) loadVal(v ValueID) (string, string) {
	if val := c.mod.Value(v); val != nil {
		if t := c.mod.Type(val.Type); t != nil && t.Kind == KindFunc {
			fnLT := c.llvmTypeOf(t) // void (Param*, ...)*
			// A function-pointer constant is referenced directly by its @name;
			// there is no storage to load from. Return (type, value) consistently
			// with the local branch below so callers can use the second result as
			// the value token.
			if val.Name != "" {
				// Resolve the raw function name through FuncByName -> c.fname
				// so the referenced symbol matches the (sanitized) definition
				// emitted by EmitLLVM. Function definitions are sanitized
				// (e.g. `my-setup` -> `@my_setup`), but the constant's Name
				// carries the RAW HIR name (`my-setup`); emitting `@my-setup`
				// directly would dangle ("use of undefined value '@my-setup'").
				if cid, ok := c.mod.FuncByName[val.Name]; ok {
					if sym, ok := c.fname[cid]; ok {
						return fnLT, "@" + sym
					}
				}
				return fnLT, "@" + val.Name
			}
			// A fn-typed local/parameter: the function pointer is stored in its
			// slot. Load it (the slot holds a `void(...)*`); callers store it
			// into a fresh alloca and pass that address (see emitCallBody's
			// KindFunc-argument branch), matching the by-reference fn param ABI.
			if slot := c.valSlot[v]; slot != "" {
				c.loadSeq++
				reg := fmt.Sprintf("%%fnlv%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", reg, fnLT, fnLT, slot))
				return fnLT, reg
			}
		}
	}
	lt, _ := c.ptype(v)
	slot := c.valSlot[v]
	if lt == "void" {
		// A genuinely void value has no storage, so there is nothing to load.
		// This is the common, legitimate case (a unit/() result, or a call to a
		// void function used for its side effects): callers test for `"void"`
		// and skip the value — see emitCall's print loop (`argT == "void"` ->
		// continue). Measured on the full corpus: 51 of the 55
		// NOLANG_MIR_DEBUG_UNDEF hits are this branch and every one of them is
		// correct; making it fail would break legitimate code en masse.
		return "void", "undef"
	}
	if slot == "" {
		// THE SILENT-UNDEF HOLE (bug #85, docs/MIR_DESIGN.md §12 #85).
		//
		// A value with a real (non-void) LLVM type but no alloca slot is a
		// *lowering failure*: the consumer is asking for a value that no
		// instruction ever produced. Handing back `undef` compiles cleanly,
		// runs, and exits 0 — LLVM folds `undef` into any value it needs — so
		// the failure is invisible:
		//
		//   i <- [0..3): { print('item' + i.to-str()) }   (tests/mem-safety/
		//   str-concat-leak.no, and /tmp/fz/c1.no)
		//
		// lowers to `call dst=0 args=[3]` (the `i.to-str()` call never gets a
		// result value: Dst = NoVal) followed by `add dst=12:str args=[11 0]`,
		// i.e. the concat's second operand IS NoVal. This function then
		// returned `undef`, the IR became
		// `@str_concat(%str-long %lv13, %str-long undef)`, and the program
		// printed 0..775 MB of stack bytes with rc=0.
		//
		// The legacy backend carried a guard of exactly this shape — its
		// emitArgAsStrLong hard-errored with "expression produced empty value"
		// — so both backends recognised the condition and only one reported
		// it. Ringing here restores that parity: the only two corpus files that
		// reach this branch (tests/mem-safety/str-concat-leak.no and
		// tests/test-std-hash.no) both carry legacy-baseline rc=1, and after
		// the guard their fingerprints are BYTE-IDENTICAL to that frozen
		// semantic oracle (`1 e3b0c442…`, empty stdout). So this is the
		// oracle's own verdict, not a regression — see docs/MIR_DESIGN.md
		// §13.3.17 ③ for why `no build` rc and `no run` rc must not be
		// confused when checking that claim.
		//
		// The `else` arm has never been observed on real input: every
		// non-NoVal hit in the corpus is a legitimate void value (the branch
		// above). It is kept so that the other half of the class cannot
		// silently return `undef` again.
		//
		// c.fail() accumulates instead of aborting, so ONE build reports every
		// offending site in the function — which is why this replaces the
		// NOLANG_MIR_DEBUG_UNDEF measurement pass (kept below for log greps).
		if v == NoVal {
			c.fail("func %s: consumer reads value 0 (NoVal), which no instruction produced — "+
				"a lowering failure previously emitted as `undef` (bug #85); "+
				"the instruction feeding this operand has no Dst", c.fname[c.cf])
		} else {
			c.fail("func %s: value %d has LLVM type %s but no storage slot — "+
				"nothing produced it, and the `undef` fallback would silently "+
				"miscompile (bug #85)", c.fname[c.cf], v, lt)
		}
		if os.Getenv("NOLANG_MIR_DEBUG_UNDEF") != "" {
			fmt.Fprintf(os.Stderr, "[mir-undef] func=%s value=%d llvm=%q slot=%q\n",
				c.fname[c.cf], v, lt, slot)
		}
		return lt, "undef"
	}
	// #83 SROA guard: for large aggregate types, do NOT emit a `load T` —
	// loading a 22KB struct into an SSA value triggers SROA to scalarize
	// every nested field, causing compile-time explosion. Return the slot
	// pointer directly; callers that need to copy the value should use
	// memcpy (see emitMove/emitGetField/emitSetField/emitReturn, all of
	// which have shouldUseMemcpy guards). Callers that use the returned
	// value for arithmetic/comparison/store will get the slot pointer,
	// which is fine for aggregate types because they are never used in
	// scalar operations — they are only stored/loaded/copied/fielded.
	if c.shouldUseMemcpy(lt) {
		return lt, slot
	}
	c.loadSeq++
	reg := fmt.Sprintf("%%lv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", reg, lt, lt, slot))
	return lt, reg
}

// optionTag returns an i64 register holding the option's discriminant (tag,
// field 0). nolang `opt == nil` / `opt == err` always compares the tag, not the
// whole struct, so callers (e.g. emitCmp) extract it before an integer icmp.
// isEnumLLVMType reports whether lt is a tagged-enum layout (%tenum_<name>).
func isEnumLLVMType(lt string) bool {
	return strings.HasPrefix(lt, "%tenum_")
}

// enumTagOf extracts an enum value's discriminant (field 0).
func (c *codegen) enumTagOf(v, enumLT string) string {
	// Large enums: loadVal hands back a slot pointer rather than a loaded
	// value, so extractvalue is not applicable — GEP field 0 and load.
	if c.shouldUseMemcpy(enumLT) {
		c.loadSeq++
		gp := fmt.Sprintf("%%etg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", gp, enumLT, enumLT, v))
		c.loadSeq++
		r := fmt.Sprintf("%%et%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", r, gp))
		return r
	}
	c.loadSeq++
	r := fmt.Sprintf("%%et%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", r, enumLT, v))
	return r
}

func (c *codegen) optionTag(v, optLT string) string {
	// #83 SROA guard: for large option types, loadVal returns a slot pointer
	// (not a loaded value), so extractvalue cannot be used. GEP field 0 (the
	// tag) and load the i64 directly.
	if c.shouldUseMemcpy(optLT) {
		c.loadSeq++
		gp := fmt.Sprintf("%%otg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", gp, optLT, optLT, v))
		c.loadSeq++
		r := fmt.Sprintf("%%ot%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", r, gp))
		return r
	}
	c.loadSeq++
	r := fmt.Sprintf("%%ot%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", r, optLT, v))
	return r
}

// optionPayloadOf extracts an option's payload and returns it together with its
// LLVM type. Used when an option is compared against a plain value, or handed to
// a builtin that wants the bare element.
//
// With one `%option` for every element the payload type is no longer part of the
// LLVM type, so `src` (the MIR value) is what identifies it. A BOXED payload is
// returned as its ADDRESS rather than a loaded value — the same convention
// loadVal uses for large aggregates, and it keeps a 36 KB `load %json_json`
// away from SROA.
func (c *codegen) optionPayloadOf(src ValueID, val, optLT string) (string, string) {
	payload := c.optPayloadLTOf(src)
	if payload == "" {
		payload = "i64"
	}
	slot := c.optSlotOfValue(src)
	if slot == "" {
		return "", ""
	}
	addr := c.optPayloadTypedAddr(slot, payload, payload)
	if c.shouldUseMemcpy(payload) {
		return addr, payload
	}
	c.loadSeq++
	r := fmt.Sprintf("%%op%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", r, payload, payload, addr))
	return r, payload
}

func (c *codegen) emitInst(f *Function, inst *Inst, allocaFor func(ValueID) string) error {
	switch inst.Op {
	case OpConst:
		return c.emitConst(inst, allocaFor)
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpUDiv, OpUMod:
		return c.emitArith(f, inst)
	case OpNeg:
		return c.emitNeg(inst)
	case OpNot:
		return c.emitNot(inst)
	case OpCast:
		return c.emitCast(inst)
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		return c.emitCmp(inst)
	case OpStrEq:
		return c.emitStrEq(inst)
	case OpAnd, OpOr:
		return c.emitLogic(inst)
	case OpBitAnd, OpBitOr, OpXor, OpShl, OpShr:
		return c.emitBitwise(inst)
	case OpDrop:
		return c.emitDrop(inst)
	case OpMove:
		return c.emitMove(inst)
	case OpBorrow:
		return c.emitBorrow(inst)
	case OpClone:
		return c.emitClone(inst)
	case OpEnumNew:
		return c.emitEnumNew(inst)
	case OpEnumTag:
		return c.emitEnumTag(inst)
	case OpEnumField:
		return c.emitEnumField(inst)
	case OpIndex:
		return c.emitIndex(inst)
	case OpIndexStore:
		return c.emitIndexStore(inst)
	case OpGetField:
		return c.emitGetField(inst)
	case OpSetField:
		return c.emitSetField(inst)
	case OpStructLit:
		// The struct slot is pre-allocated by the prologue (allocaFor runs over
		// every Dst value); field initializers are stored by the separate
		// OpSetField instructions. However, the prologue only zero-initializes
		// OWNED slots; a struct (e.g. hashmap-str-i64) is typically not owned,
		// so its alloca is left uninitialized. Without zeroing, fields not
		// touched by the initializer (or by init()) contain stack garbage —
		// free() on a random pointer -> trace/BPT trap. Legacy always stores
		// zeroinitializer for struct allocas; mirror that here.
		slot := c.valSlot[inst.Dst]
		if slot != "" {
			lt, _ := c.ptype(inst.Dst)
			if lt != "" && lt != "i64" && lt != "void" {
				c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %s\n", lt, lt, slot))
			}
		}
		return nil
	case OpFuncRef:
		// A function-reference value (OpFuncRef) carries a KindFunc type and
		// a Name; loadVal resolves it to `@funcname` directly. No IR needs to
		// be emitted here — the value is materialized at its use site.
		return nil
	case OpOptionWrap:
		return c.emitOptionWrap(inst)
	case OpTxtFromStr:
		return c.emitTxtFromStr(inst)
	case OpStrFromVec:
		return c.emitStrFromVec(inst)
	case OpSliceOp:
		return c.emitSliceOp(inst)
	case OpLen, OpCap:
		return c.emitLenCap(inst)
	case OpCall:
		return c.emitCall(f, inst)
	case OpRun:
		return c.emitAsyncRun(f, inst)
	case OpAwait:
		return c.emitAsyncAwait(f, inst)
	case OpReturn:
		c.sb.WriteString("  ret void\n")
		return nil
	default:
		c.fail("unsupported op %s in func %s", inst.Op, f.Name)
		return fmt.Errorf("MIR->LLVM: %s", strings.Join(c.errs, "; "))
	}
}

func (c *codegen) emitConst(inst *Inst, allocaFor func(ValueID) string) error {
	if t := c.mod.Type(inst.Type); t != nil && t.Kind == KindFunc {
		// A function-pointer constant is referenced by its @name directly (see
		// loadVal); it carries no storage to materialize. Skip emission so we
		// never emit a (type-rejected) `store void()* @name, void()** %slot`.
		return nil
	}
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	// A void-typed constant (e.g. a unit/()` value) has no storage and cannot
	// be stored (LLVM rejects `store void zeroinitializer, void*`). Treat it as
	// a no-op — there is nothing to materialize.
	if lt == "void" || slot == "" {
		return nil
	}
	if inst.Type != NoType {
		t := c.mod.Type(inst.Type)
		if t != nil && t.Kind == KindStr {
			g := c.strGlobals[inst.Dst]
			c.sb.WriteString(fmt.Sprintf("  %%c%d = call %s @str_from_const(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), i64 %d)\n", inst.Dst, lt, g.n, g.n, g.name, g.n))
			c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
			return nil
		}
	}
	if isOptionType(lt) {
		// `nil` is the only option constant the MIR subset emits. It is the
		// option's "none" discriminant: tag=1, payload=zero. A bare
		// zeroinitializer would be misread as some(0) by the option
		// load/store codegen, turning a `nil` into a non-nil some(0). The
		// payload type is derived from the option's element raw type so both
		// the flat `%option` ({ i64, i64 }) and per-payload inline types
		// (`%option` = { i64 tag, [N x i64] slot }) initialize correctly.
		c.optStoreTag(slot, 1)
		return nil
	}
	switch lt {
	case "i64":
		c.sb.WriteString(fmt.Sprintf("  store i64 %d, i64* %s\n", inst.Int, slot))
	case "i1":
		c.sb.WriteString(fmt.Sprintf("  store i1 %d, i1* %s\n", inst.Int, slot))
	case "double":
		bits := math.Float64bits(inst.Flt)
		c.sb.WriteString(fmt.Sprintf("  store double 0x%016X, double* %s\n", bits, slot))
	default:
		c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %s\n", lt, lt, slot))
	}
	return nil
}

// emitOptionWrap builds a ?T option value `{ i64 tag, [N x i64] slot }` from a
// single payload argument. It backs the nolang ?T constructors val/ok/some (tag
// 0) and err (tag 2). The discriminant comes from inst.Int (the tag set by the
// lowerer); inst.Args[0] is the payload value, whose ownership has already been
// transferred into the option by the move analysis (so it is exempt from
// dropping, and this store is the only free site). The payload's LLVM type is
// derived from the option type's element raw type and decides whether the
// payload goes inline into the 24-byte slot or into a heap box.
func (c *codegen) emitOptionWrap(inst *Inst) error {
	dstT, _ := c.ptype(inst.Dst)
	if dstT == "void" || dstT == "" {
		return nil
	}
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		c.fail("option-wrap destination has no slot in func %d", c.cf)
		return fmt.Errorf("option-wrap slot")
	}
	elem := optionElemRaw(c.mod.Type(inst.Type))
	_, payloadLT := c.optionType(elem)
	tag := inst.Int
	if len(inst.Args) == 0 || inst.Args[0] == NoVal {
		// No payload (bare err / val / ok / some constructor, or a zero-arg
		// call): emit the discriminant with the requested tag and a zero
		// payload. tag 0 = val/ok/some, tag 2 = err (NOT the "none" tag 1,
		// which is reserved for nil / KNilLit).
		c.optStoreTag(slot, tag)
		return nil
	}
	src := inst.Args[0]
	svType, sv := c.loadVal(src)
	// A fixed-array literal into a `?[]T` option must become a real slice
	// first: its bytes are the ELEMENTS, so storing them raw would make the
	// payload read back as {len=first-elem, cap=second-elem, ...}.
	if payloadLT == "%vec" && strings.HasPrefix(svType, "[") {
		arrSlot := c.valSlot[src]
		if arrSlot == "" {
			c.loadSeq++
			arrSlot = fmt.Sprintf("%%owa%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", arrSlot, svType))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", svType, sv, svType, arrSlot))
		}
		sv = c.vecFromArraySink(svType, arrSlot)
		svType = "%vec"
	}
	// Type-pun: the wrapped value's type differs from the declared payload.
	// `err(msg)` into ?i64 / ?file / ?json is the canonical case — the message
	// is a 24-byte %str-long that fits the slot exactly, so it is stored as
	// raw bytes and read back as raw bytes by the err arm. This replaces the
	// old `str_clone` + ptrtoint hack, which threw away len/cap (the err arm
	// printed a raw heap address) and leaked the clone.
	if svType != "" && svType != payloadLT {
		c.optStoreRawBytes(slot, tag, svType, src, sv)
		return nil
	}
	if sv == "" {
		c.optStoreTag(slot, tag)
		return nil
	}
	if c.optionPayloadInline(payloadLT) {
		// Scalars, str, vec, small structs / arrays: store by value into the
		// (already zeroed) slot.
		c.optStoreTag(slot, tag)
		c.optStoreInlinePayload(slot, payloadLT, sv)
		return nil
	}
	// Payload too big for the slot (or of unprovable size): heap-box it and
	// record the pointer. This also removes the old #83 SROA hazard — the
	// option is now a fixed 32 bytes regardless of how large the payload is,
	// so no `load %json_json` ever reaches SROA.
	srcAddr := c.addrOfValue(src, svType, sv)
	if srcAddr == "" {
		c.optStoreTag(slot, tag)
		return nil
	}
	c.optStoreBoxedPayload(slot, tag, payloadLT, srcAddr)
	return nil
}

// unwrapOptionOperand extracts the ok-payload of an %option operand when the
// arithmetic/comparison destination is a plain scalar. The overflow-default
// integration makes integer arithmetic yield ?T, so a variable that holds such
// a result is typed %option; when it is reused as an operand of a scalar op
// (`%c = mul i64 %x, %y` where %y is %option) opt rejects the IR as "defined
// with type %option but expected i64". Mirroring legacy, the variable
// contributes its scalar payload (tag 0 = ok), never the {tag,payload} struct.
func (c *codegen) unwrapOptionOperand(src ValueID, t, v, dstLT string) (string, string) {
	if t == "%option" && dstLT != "%option" && dstLT != "" {
		pv, pt := c.optionPayloadOf(src, v, t)
		if pv == "" {
			return t, v
		}
		if cv := c.coerce(pt, pv, dstLT); cv != "" {
			return dstLT, cv
		}
		return pt, pv
	}
	return t, v
}

func (c *codegen) emitArith(f *Function, inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// string concat: both operands should be %str-long. A mixed %str-long vs
	// integer operand (`s + n`) treats the integer as a single/decimal string,
	// so promote it to %str-long first (reuse @str_from_i64/@str_from_char). A
	// bare `call @str_concat(%str-long, i64)` is illegal and opt rejects it.
	if lt == "%str-long" {
		// `str * int` is string repeat (Nolang `s * n`), not concatenation.
		if inst.Op == OpMul {
			c.sb.WriteString(fmt.Sprintf("  %%c%d = call %s @_mir_str_repeat(%s %s, %s %s)\n", inst.Dst, lt, aT, aV, bT, bV))
			c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
			return nil
		}
		if aT != "%str-long" && isIntType(aT) {
			aV = c.emitIntToStr(aV, aT)
		}
		if bT != "%str-long" && isIntType(bT) {
			bV = c.emitIntToStr(bV, bT)
		}
		c.sb.WriteString(fmt.Sprintf("  %%c%d = call %s @str_concat(%s %s, %s %s)\n", inst.Dst, lt, lt, aV, lt, bV))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
		return nil
	}
	// Overflow-default arithmetic: integer/float ops yield ?T. Unwrap any
	// %option operands to their scalar payload, compute in the scalar element
	// type, then wrap the result back into the option (tag 0 = ok) for storage.
	resLT := lt
	wrapResult := false
	if strings.HasPrefix(lt, "%option") {
		if val := c.mod.Value(inst.Dst); val != nil {
			if t := c.mod.Type(val.Type); t != nil && t.Kind == KindOption {
				if elem := optionElemRaw(t); elem != "" {
					_, payloadLT := c.optionType(elem)
					resLT = payloadLT
					wrapResult = true
					aT, aV = c.unwrapOptionOperand(inst.Args[0], aT, aV, resLT)
					bT, bV = c.unwrapOptionOperand(inst.Args[1], bT, bV, resLT)
				}
			}
		}
	} else {
		aT, aV = c.unwrapOptionOperand(inst.Args[0], aT, aV, lt)
		bT, bV = c.unwrapOptionOperand(inst.Args[1], bT, bV, lt)
	}
	// Coerce both operands to the (possibly unwrapped) result type: a str-byte
	// (i8) arithmetic like `c - 32` feeds an i8 literal (i64) into an i8 op;
	// mismatched widths are a hard LLVM type error. Truncate/extend so the
	// operation type matches. (coerceInt is a no-op for floating-point types.)
	aV = c.coerceInt(aV, aT, resLT)
	bV = c.coerceInt(bV, bT, resLT)
	// Integer ops (add/sub/mul/sdiv/srem) are ILLEGAL on floating-point values;
	// f64 arithmetic must use the float-family opcodes or opt rejects the IR with
	// "invalid operand type for instruction" (the f64↔str type-width bug).
	isFloat := resLT == "double"
	var op string
	switch inst.Op {
	case OpAdd:
		if isFloat {
			op = "fadd"
		} else {
			op = "add"
		}
	case OpSub:
		if isFloat {
			op = "fsub"
		} else {
			op = "sub"
		}
	case OpMul:
		if isFloat {
			op = "fmul"
		} else {
			op = "mul"
		}
	case OpDiv:
		if isFloat {
			op = "fdiv"
		} else {
			op = "sdiv"
		}
	case OpMod:
		if isFloat {
			op = "frem"
		} else {
			op = "srem"
		}
	case OpUDiv:
		// Unsigned division (nolang u64 family). Never float.
		op = "udiv"
	case OpUMod:
		// Unsigned remainder (nolang u64 family). Never float.
		op = "urem"
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, resLT, aV, bV))
	if wrapResult {
		c.optStoreTag(slot, 0)
		c.optStoreInlinePayload(slot, resLT, fmt.Sprintf("%%c%d", inst.Dst))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	}
	return nil
}

func (c *codegen) emitNeg(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	vT, v := c.loadVal(inst.Args[0])
	resLT := lt
	wrapResult := false
	if strings.HasPrefix(lt, "%option") {
		if val := c.mod.Value(inst.Dst); val != nil {
			if t := c.mod.Type(val.Type); t != nil && t.Kind == KindOption {
				if elem := optionElemRaw(t); elem != "" {
					_, payloadLT := c.optionType(elem)
					resLT = payloadLT
					wrapResult = true
					vT, v = c.unwrapOptionOperand(inst.Args[0], vT, v, resLT)
				}
			}
		}
	} else {
		vT, v = c.unwrapOptionOperand(inst.Args[0], vT, v, lt)
	}
	v = c.coerceInt(v, vT, resLT)
	if resLT == "double" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fneg double %s\n", inst.Dst, v))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = sub %s 0, %s\n", inst.Dst, resLT, v))
	}
	if wrapResult {
		c.optStoreTag(slot, 0)
		c.optStoreInlinePayload(slot, resLT, fmt.Sprintf("%%c%d", inst.Dst))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	}
	return nil
}

func (c *codegen) emitNot(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	vT, v := c.loadVal(inst.Args[0])
	v = c.coerceInt(v, vT, lt)
	c.sb.WriteString(fmt.Sprintf("  %%c%d = xor %s 1, %s\n", inst.Dst, lt, v))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

// emitCast lowers an integer-width conversion (MIR's only width-conversion op,
// used e.g. by string-interpolation lowering to promote byte/u8/u16/u32/i8/i16/
// i32 to i64/u64 before calling fmt-int/fmt-uint, and bool (i1) to i64 so it
// formats as "1"/"0"). LLVM types are width-only (i8 covers both signed and
// unsigned), so signedness is taken from the nolang RAW type: signed i8/i16/i32
// sign-extend, i1 and all unsigned (u*/byte) zero-extend.
func (c *codegen) emitCast(inst *Inst) error {
	dstLT, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	srcT, srcV := c.loadVal(inst.Args[0])
	if srcT == dstLT {
		v := c.coerceInt(srcV, srcT, dstLT)
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, slot))
		return nil
	}
	srcRaw := ""
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			srcRaw = t.Raw
		}
	}
	// `byte`/`u8` zero-extend (see coerceInt); only genuinely signed widths
	// sign-extend. Note `i8` is zero-extended too — MIR maps byte/u8/i8 onto
	// the same LLVM i8, and unsigned bytes dominate the corpus.
	signed := strings.HasPrefix(srcRaw, "i") && srcRaw != "i1" && srcRaw != "i8"
	if signed {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = sext %s %s to %s\n", inst.Dst, srcT, srcV, dstLT))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = zext %s %s to %s\n", inst.Dst, srcT, srcV, dstLT))
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", dstLT, inst.Dst, dstLT, slot))
	return nil
}

func (c *codegen) emitCmp(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// Option operands. Which half of the `{ tag, payload }` struct is compared
	// depends on the OTHER side:
	//
	//   - option vs option (`opt == nil`, `opt == err`, `a == b` on two
	//     optionals) is nolang's null/error TEST, so compare the TAG
	//     (discriminant, field 0). A direct `icmp` on %option is illegal.
	//   - option vs a plain value (`content == 'Hello World'` on a `?str`) uses
	//     the PAYLOAD — that is the legacy behaviour, and it is the only reading
	//     that makes the expression mean anything.
	//
	// Extracting the tag unconditionally (the previous behaviour) made the
	// second case compare the discriminant's DECIMAL TEXT against the literal:
	// `?str` became the string "0", so `content == 'Hello World'` was never
	// true, and the follow-up `x != v -> ...` arm was equally false, leaving
	// both arms of the guard silently skipped
	// (tests/test-process-run.no: `out.trim()`-style checks on `?str` values
	// coming out of `[]byte.to-str`).
	// Tagged-enum operands. A match arm compiles to
	// `subject == <variant constructor>` (the parser desugars
	// `s: { circle(r) -> ... }` into an equality test against the variant),
	// and an enum is `{ tag, payload }`, so a whole-struct `icmp` is illegal.
	// The comparison that the source means is the DISCRIMINANT: an arm matches
	// when the subject holds that variant, regardless of the payload — and the
	// constructor side carries no payload anyway (its fields are the arm's
	// binding names, lowered separately).
	aEnum, bEnum := isEnumLLVMType(aT), isEnumLLVMType(bT)
	if aEnum && bEnum {
		aV = c.enumTagOf(aV, aT)
		aT = "i64"
		bV = c.enumTagOf(bV, bT)
		bT = "i64"
	}
	aOpt, bOpt := isOptionType(aT), isOptionType(bT)
	switch {
	case aOpt && bOpt:
		aV = c.optionTag(aV, aT)
		aT = "i64"
		bV = c.optionTag(bV, bT)
		bT = "i64"
	case aOpt:
		// Only extract the payload when the comparison is meaningful:
		// - option vs scalar (int) with scalar payload → payload compare
		// - option vs str/vec with matching payload → payload compare
		// When the payload is an aggregate but the other side is a scalar
		// (e.g. ?[]str vs bool), comparing the payload is meaningless and
		// produces a type mismatch. Fall back to comparing the TAG.
		payloadT := c.optPayloadLTOf(inst.Args[0])
		if payloadT == bT || (isIntType(bT) && (payloadT == "i64" || payloadT == "i32" || payloadT == "i8" || payloadT == "i1")) || (bT == "%str-long" && payloadT == "%str-long") || (bT == "%vec" && payloadT == "%vec") {
			aV, aT = c.optionPayloadOf(inst.Args[0], aV, aT)
		} else {
			aV = c.optionTag(aV, aT)
			aT = "i64"
		}
	case bOpt:
		payloadT := c.optPayloadLTOf(inst.Args[1])
		if payloadT == aT || (isIntType(aT) && (payloadT == "i64" || payloadT == "i32" || payloadT == "i8" || payloadT == "i1")) || (aT == "%str-long" && payloadT == "%str-long") || (aT == "%vec" && payloadT == "%vec") {
			bV, bT = c.optionPayloadOf(inst.Args[1], bV, bT)
		} else {
			bV = c.optionTag(bV, bT)
			bT = "i64"
		}
	}
	// Mixed %str-long vs integer (char/byte/i64) comparison: nolang `char/byte
	// == str` treats the integer as a single/decimal string, so promote it to
	// %str-long and compare through @str_eq. A bare `icmp i8, %str-long` is
	// illegal (opt rejects it). This is the test-path_char class of bug.
	if aT == "%str-long" && isIntType(bT) {
		bV = c.emitIntToStr(bV, bT)
		bT = "%str-long"
	}
	if bT == "%str-long" && isIntType(aT) {
		aV = c.emitIntToStr(aV, aT)
		aT = "%str-long"
	}
	if aT == "%str-long" && bT == "%str-long" {
		// The comparison is always performed as EQUALITY (that is what the
		// runtime @str_eq helper provides); `!=` must explicitly invert it.
		// Emitting @str_eq for both operators made `a != b` behave exactly like
		// `a == b` whenever an operand reached this branch through the int->str
		// promotion below (e.g. `?str != 'lit'`), so the "not equal" arm fired on
		// equal strings.
		if inst.Op == OpNe {
			eqR := fmt.Sprintf("%%c%de", inst.Dst)
			c.sb.WriteString(fmt.Sprintf("  %s = call i1 @str_eq(%s %s, %s %s)\n", eqR, aT, aV, bT, bV))
			c.sb.WriteString(fmt.Sprintf("  %%c%d = xor i1 %s, true\n", inst.Dst, eqR))
		} else {
			c.sb.WriteString(fmt.Sprintf("  %%c%d = call i1 @str_eq(%s %s, %s %s)\n", inst.Dst, aT, aV, bT, bV))
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
		return nil
	}
	var op string
	switch inst.Op {
	case OpEq:
		op = "eq"
	case OpNe:
		op = "ne"
	case OpLt:
		op = "slt"
	case OpLe:
		op = "sle"
	case OpGt:
		op = "sgt"
	case OpGe:
		op = "sge"
	}
	// Fixed-array comparison ([N x T] == [N x T]): LLVM's icmp does NOT support
	// aggregate types. For equality (== / !=) use memcmp on the raw byte
	// representation, which is the structural comparison nolang semantics call
	// for. Ordering comparisons (<, <=, >, >=) on arrays are not meaningful in
	// nolang and are not expected in the corpus; if one appears we let it fall
	// through to the icmp path, which will fail LLVM verification (an honest
	// signal rather than silently wrong output).
	if strings.HasPrefix(aT, "[") && strings.HasPrefix(bT, "[") && (inst.Op == OpEq || inst.Op == OpNe) {
		c.decl("declare i32 @memcmp(i8*, i8*, i64)")
		// Compute the byte size of the array type via a one-element GEP trick
		// (end - start), avoiding manual N*sizeof(E) arithmetic.
		aSlot := c.valSlot[inst.Args[0]]
		bSlot := c.valSlot[inst.Args[1]]
		// bitcast both array slots to i8*
		c.loadSeq++
		bcA := fmt.Sprintf("%%ac%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", bcA, aT, aSlot))
		c.loadSeq++
		bcB := fmt.Sprintf("%%bc%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", bcB, bT, bSlot))
		// size = sizeof(aT) via GEP
		c.loadSeq++
		endPtr := fmt.Sprintf("%%ae%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* null, i64 1\n", endPtr, aT, aT))
		c.loadSeq++
		szInt := fmt.Sprintf("%%as%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s* %s to i64\n", szInt, aT, endPtr))
		// memcmp(a, b, size) == 0 means equal
		c.loadSeq++
		mr := fmt.Sprintf("%%am%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call i32 @memcmp(i8* %s, i8* %s, i64 %s)\n", mr, bcA, bcB, szInt))
		c.loadSeq++
		eqR := fmt.Sprintf("%%aq%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", eqR, mr))
		if inst.Op == OpNe {
			c.sb.WriteString(fmt.Sprintf("  %%c%d = xor i1 %s, true\n", inst.Dst, eqR))
		} else {
			c.sb.WriteString(fmt.Sprintf("  %%c%d = select i1 %s, i1 true, i1 false\n", inst.Dst, eqR))
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
		return nil
	}
	if aT == "double" || bT == "double" {
		// Integer predicates (slt/sle/...) are ILLEGAL in fcmp; map to the
		// ordered float predicates (olt/ole/ogt/oge). The bare "o"+op form only
		// works for eq/ne (oeq/one) and produces invalid "oslt"/"osle"/... for
		// the signed comparisons (the f64↔str type-width bug).
		var fop string
		switch inst.Op {
		case OpEq:
			fop = "oeq"
		case OpNe:
			// `!=` is the UNORDERED predicate (une), not `one`. With `one`,
			// `NaN != NaN` evaluated to false — which is wrong under IEEE-754
			// and made `x != x` useless as the NaN test that `f64-to-str` and
			// friends rely on (they printed NaN as "0"). `==` stays ordered
			// (oeq), so `NaN == NaN` is still false and `!=` remains its
			// exact negation.
			fop = "une"
		case OpLt:
			fop = "olt"
		case OpLe:
			fop = "ole"
		case OpGt:
			fop = "ogt"
		case OpGe:
			fop = "oge"
		}
		aV = c.coerceInt(aV, aT, "double")
		bV = c.coerceInt(bV, bT, "double")
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fcmp %s double %s, %s\n", inst.Dst, fop, aV, bV))
	} else {
		// Coerce both operands to a common integer width so the icmp is
		// well-typed (e.g. i8 str byte vs i64 literal). Use the wider operand's
		// width; narrow operands are sign-extended (preserving signed char/byte
		// semantics).
		ct := aT
		aw, _ := intWidth(aT)
		bw, _ := intWidth(bT)
		if bw > aw {
			ct = bT
		}
		aV = c.coerceInt(aV, aT, ct)
		bV = c.coerceInt(bV, bT, ct)
		c.sb.WriteString(fmt.Sprintf("  %%c%d = icmp %s %s %s, %s\n", inst.Dst, op, ct, aV, bV))
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

// emitStrEq lowers OpStrEq (a == b on %str-long operands) to a call to the
// runtime @str_eq helper. A direct `icmp` on the struct would be illegal, so the
// comparison always goes through the helper (defined in the prelude).
func (c *codegen) emitStrEq(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// Mixed %str-long vs integer operand (e.g. a `str == char` where the char
	// sits on the right, or any `nil` that for some reason lowered to a scalar):
	// promote the integer to a %str-long through the same runtime helpers
	// emitCmp uses, so the @str_eq call is type-correct and semantically right
	// (char -> one-char string). `nil` against a str is already lowered to an
	// empty %str-long by lowerNilLit, so this path only sees genuine integers.
	if aT == "%str-long" && bT != "%str-long" && isIntType(bT) {
		bV = c.emitIntToStr(bV, bT)
		bT = "%str-long"
	}
	if bT == "%str-long" && aT != "%str-long" && isIntType(aT) {
		aV = c.emitIntToStr(aV, aT)
		aT = "%str-long"
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = call i1 @str_eq(%s %s, %s %s)\n", inst.Dst, aT, aV, bT, bV))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

// isIntType reports whether an LLVM type is an integer width that may be
// compared against a string (char/byte/i64/...). Floating points are excluded —
// they lower to fcmp, not string promotion.
func isIntType(lt string) bool {
	switch lt {
	case "i64", "i8", "i32", "i16", "u64", "u32", "u16", "u8":
		return true
	}
	return false
}

// isUnsignedRaw reports whether a nolang RAW integer type is unsigned
// (u64/u32/u16/u8/byte). MIR flattens every integer to the LLVM i64 width, so
// signed vs unsigned is NOT recoverable from the LLVM type alone — it must be
// read from the source type. Division/remainder on an unsigned operand must
// use `udiv`/`urem` (otherwise the i64.MIN magnitude 2^63 corrupts the digit
// loop of i64-to-str / u64-to-str).
func isUnsignedRaw(raw string) bool {
	switch raw {
	case "u64", "u32", "u16", "u8", "byte":
		return true
	}
	return false
}

// emitIntToStr materializes an integer value as a %str-long (1-char for i8/byte,
// decimal for wider ints) and returns the result register. Used by emitCmp to
// bring a string/int comparison onto a common %str-long type.
func (c *codegen) emitIntToStr(v, t string) string {
	c.loadSeq++
	r := fmt.Sprintf("%%cis%d", c.loadSeq)
	if t == "i8" || t == "u8" {
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_from_char(i8 %s)\n", r, "%str-long", v))
		return r
	}
	av := c.coerceInt(v, t, "i64")
	c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_from_i64(i64 %s)\n", r, "%str-long", av))
	return r
}

func (c *codegen) emitLogic(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	aV = c.coerceInt(aV, aT, lt)
	bV = c.coerceInt(bV, bT, lt)
	op := "and"
	if inst.Op == OpOr {
		op = "or"
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, lt, aV, bV))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

// emitBitwise lowers the bitwise AND/OR/XOR and shift-left/right operators.
// Mirrors emitArith's width coercion: both operands are coerced to the result
// integer type, and the shift amount is coerced to the value type as well
// (LLVM requires the shift amount to share the value's type). nolang `>>` is a
// logical shift (lshr), matching the legacy backend (expr.go:6384).
func (c *codegen) emitBitwise(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// Overflow-default arithmetic yields ?T, so a bitwise operand (and/or/xor/
	// shl/shr) may carry an %option value. Unwrap %option operands to their
	// scalar payload, compute in the scalar element type, then wrap the result
	// back into the option (tag 0 = ok) for storage — mirroring emitArith.
	// Without this, `lshr %option %lv, %lv` where the shift amount is a plain
	// i64 fails LLVM verification ("defined with type 'i64' but expected
	// '%option'") (test-vec-assign).
	resLT := lt
	wrapResult := false
	if strings.HasPrefix(lt, "%option") {
		if val := c.mod.Value(inst.Dst); val != nil {
			if t := c.mod.Type(val.Type); t != nil && t.Kind == KindOption {
				if elem := optionElemRaw(t); elem != "" {
					_, payloadLT := c.optionType(elem)
					resLT = payloadLT
					wrapResult = true
					aT, aV = c.unwrapOptionOperand(inst.Args[0], aT, aV, resLT)
					bT, bV = c.unwrapOptionOperand(inst.Args[1], bT, bV, resLT)
				}
			}
		}
	} else {
		aT, aV = c.unwrapOptionOperand(inst.Args[0], aT, aV, lt)
		bT, bV = c.unwrapOptionOperand(inst.Args[1], bT, bV, lt)
	}
	aV = c.coerceInt(aV, aT, resLT)
	bV = c.coerceInt(bV, bT, resLT)
	var op string
	switch inst.Op {
	case OpBitAnd:
		op = "and"
	case OpBitOr:
		op = "or"
	case OpXor:
		op = "xor"
	case OpShl:
		op = "shl"
	case OpShr:
		op = "lshr"
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, resLT, aV, bV))
	if wrapResult {
		c.optStoreTag(slot, 0)
		c.optStoreInlinePayload(slot, resLT, fmt.Sprintf("%%c%d", inst.Dst))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	}
	return nil
}

func (c *codegen) emitDrop(inst *Inst) error {
	lt, _ := c.ptype(inst.Args[0])
	_, v := c.loadVal(inst.Args[0])
	if isOptionType(lt) {
		// Real drop for an option that owns a heap element. Per-payload inline
		// options store the payload by-value; free it according to the element
		// type. Scalar options (?i64, ?bool, ...) own nothing and stay a
		// no-op (correct); struct/str/vec payloads are freed by emitOptionDrop.
		c.emitOptionDrop(inst, v, lt)
		return nil
	}
	switch lt {
	case "%str-long":
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", lt, v))
	case "%vec":
		// Real drop: free the backing store (data pointer at field 2). The %vec
		// struct is by-value and needs no free.
		c.sb.WriteString(fmt.Sprintf("  call void @vec_free(%s %s)\n", lt, v))
	default:
		// STRUCT WITH POINTER FIELDS: free the pointees, recursively. Pass the
		// SLOT address, not the loaded value — the destructor mutates nothing but
		// needs the struct's address to GEP its fields.
		if key := c.structKeyOfLLVM(lt); key != "" && (c.mod.StructHasPtrFields(key) || c.mod.StructHasOwnedLeafFields(key)) {
			if slot := c.valSlot[inst.Args[0]]; slot != "" {
				c.emitStructDropHelper(lt, key)
				c.sb.WriteString(fmt.Sprintf("  call void @%s(%s* %s)\n", structDropName(lt), lt, slot))
			}
			return nil
		}
		// scalar: nothing to free
	}
	return nil
}

// emitOptionDrop frees an option's owned heap element. The element type is read
// from the option value's nolang raw type (?str / ?vec / ?T) so we free with the
// right helper; unknown/non-heap elements are left alone (safe no-op). optLT is
// the option's single LLVM type (%option).
func (c *codegen) emitOptionDrop(inst *Inst, v, optLT string) {
	elemRaw := ""
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil && t.Raw != "" {
			if e, ok := parseOptionElem(t.Raw); ok {
				elemRaw = e
			}
		}
	}
	// A BOXED payload is by definition a payload that does not fit the slot,
	// i.e. a struct — and a struct is never ClassifyOwnership-owned, so no drop
	// is ever emitted for it (see insertDrops). Only ?str / ?vec / ?[]T reach
	// this function, and all three are exactly 24 bytes, hence always INLINE.
	// That is why there is no box-free branch here: leaking a box is impossible
	// today, and a free guarded on `tag == 0` would be the first thing to break
	// if an owned payload ever outgrew the slot.
	slot := c.optSlotOfValue(inst.Args[0])
	if slot == "" {
		c.sb.WriteString(fmt.Sprintf("  ; drop %s (option element %q has no slot)\n", v, elemRaw))
		return
	}
	switch elemRaw {
	case "str":
		// Inline %str-long payload: free its heap data buffer (field 2).
		c.loadSeq++
		pl := fmt.Sprintf("%%optpl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%str-long*\n", pl, c.optPayloadAddr(slot, "%str-long")))
		c.loadSeq++
		pv := fmt.Sprintf("%%optpv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %%str-long, %%str-long* %s\n", pv, pl))
		c.loadSeq++
		pd := fmt.Sprintf("%%optpd%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", pd, pv))
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", pd))
	case "vec":
		// Inline %vec payload: free its backing store (field 2, a pointer held
		// as i64).
		c.loadSeq++
		pl := fmt.Sprintf("%%optpl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%vec*\n", pl, c.optPayloadAddr(slot, "%vec")))
		c.loadSeq++
		pv := fmt.Sprintf("%%optpv%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %%vec, %%vec* %s\n", pv, pl))
		c.loadSeq++
		pd := fmt.Sprintf("%%optpd%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", pd, pv))
		c.loadSeq++
		pp := fmt.Sprintf("%%optpp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", pp, pd))
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", pp))
	default:
		// ?i64 / ?bool / user-struct / unrecognized element: nothing heap-owned
		// to free (or a shallow no-op for structs; deep-free is a follow-up).
		c.sb.WriteString(fmt.Sprintf("  ; drop %s (option element %q owns nothing / shallow)\n", v, elemRaw))
	}
}

// emitBorrow lowers OpBorrow: take the ADDRESS of a value's storage and store
// that pointer into the destination — no ownership transfer, no copy.
//
// This is the primitive behind a VIEW (`&T`). A view is `T*` at the LLVM level,
// so `&json` is `%json*`. Every MIR value lives in a slot (`%vN.s`, or the
// caller's pointer `%pN` for an aliased by-ref parameter), and that slot name IS
// the address of the value's storage — so borrowing is a single store of the
// slot pointer, with no load of the value itself.
//
//	&self  ->  store %json* %p0,        %json** %dst.s
//	&local ->  store %json* %v3.s,      %json** %dst.s
func (c *codegen) emitBorrow(inst *Inst) error {
	if len(inst.Args) < 1 || inst.Args[0] <= NoVal {
		c.fail("borrow: missing source in func %d", c.cf)
		return fmt.Errorf("borrow arity")
	}
	src := inst.Args[0]
	srcSlot := c.valSlot[src]
	if srcSlot == "" {
		c.fail("borrow: source %d has no slot in func %d", src, c.cf)
		return fmt.Errorf("borrow src slot")
	}
	srcLT, _ := c.ptype(src)
	if srcLT == "" {
		srcLT = "i64"
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("borrow: destination %d has no slot in func %d", inst.Dst, c.cf)
		return fmt.Errorf("borrow dst slot")
	}
	ptrLT := srcLT + "*"
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", ptrLT, srcSlot, ptrLT, dstSlot))
	return nil
}

func (c *codegen) emitMove(inst *Inst) error {
	// OpMove has two shapes:
	//   b.Emit(OpMove, typ, [src])  -> Dst is a NEW value; src moves into it.
	//   EmitMoveInto(dst, src)      -> Dst is NoVal, Args=[src, dst]; src moves
	//                                  into the EXISTING dst slot (no new value).
	dstVal := inst.Dst
	if dstVal == NoVal && len(inst.Args) >= 2 {
		dstVal = inst.Args[1]
	}
	if dstVal == NoVal {
		c.fail("move with no destination in func %d", c.cf)
		return fmt.Errorf("move no dst")
	}
	dstT, _ := c.ptype(dstVal)
	if dstT == "void" {
		// Moving into a void binding is a no-op (the value carries nothing).
		return nil
	}
	dstSlot := c.valSlot[dstVal]
	if dstSlot == "" {
		c.fail("move destination has no slot in func %d", c.cf)
		return fmt.Errorf("move slot")
	}
	srcSlot := c.valSlot[inst.Args[0]]
	if srcSlot == "" {
		c.fail("move source has no slot in func %d", c.cf)
		return fmt.Errorf("move src slot v%d -> v%d", inst.Args[0], dstVal)
	}
	srcT, _ := c.ptype(inst.Args[0])
	// Ownership-correctness guard: moving a BORROWED input parameter into an owned
	// heap slot must CLONE the heap, never bitwise-copy the {len,cap,data} triple.
	// A bitwise copy shares the data pointer; the caller still owns the parameter
	// (Nolang passes owned arguments by reference and drops them at the caller's
	// scope), so BOTH the caller's parameter and the move destination would free
	// the same buffer -> double-free. The classic repro is `fn foo(a str) str = a`
	// (move2 / test-it-probe / test-call-heavy ...): foo returns its input param,
	// the caller stores the result into a fresh local AND still owns the argument,
	// then drops both. The callee does NOT own the parameter's heap, so it cannot
	// transfer ownership; it must hand the destination an independent copy.
	// %str-long clones via @str_clone; %vec / heap-option have no clone helper yet,
	// so we reject (clean fallback to the proven legacy path) rather than emit a
	// binary that would double-free at runtime.
	if c.curFn != nil && isFuncParam(c.curFn, inst.Args[0]) && (dstT == "%str-long" || dstT == "%vec") {
		if dstT == "%str-long" {
			_, sv := c.loadVal(inst.Args[0])
			tmp := fmt.Sprintf("%%cl%d", inst.ID)
			c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", tmp, dstT, dstT, sv))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, tmp, dstT, dstSlot))
			return nil
		}
		c.fail("moving a borrowed %s parameter transfers ownership the callee does not hold (double-free risk); unsupported in MIR backend", dstT)
		return fmt.Errorf("borrowed %s param move unsupported in MIR backend", dstT)
	}
	// Option-aware move. nolang `x ?t = y` wraps y into some(y) and `x = opt`
	// peels the payload back out. Both directions go through the unified
	// `%option = { i64 tag, [N x i64] slot }`: a plain
	// `store %option %loaded, %option* dst` on the WRAP side would write y's
	// bytes into the tag field and turn some(v) into nil (and, worse, copy a
	// payload that no longer fits the slot).
	if isOptionType(dstT) && !isOptionType(srcT) {
		payloadLT := c.optPayloadLTOf(dstVal)
		src := inst.Args[0]
		_, sv := c.loadVal(src)
		// `out ?[]i64 = [10, 20, 30]`: the literal's bytes are the ELEMENTS, so
		// it must become a real slice before it can be a %vec payload.
		if payloadLT == "%vec" && strings.HasPrefix(srcT, "[") {
			arrSlot := c.valSlot[src]
			if arrSlot == "" {
				c.loadSeq++
				arrSlot = fmt.Sprintf("%%mva%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", arrSlot, srcT))
				c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", srcT, sv, srcT, arrSlot))
			}
			sv = c.vecFromArraySink(srcT, arrSlot)
			srcT = "%vec"
		}
		// Type-pun (`x ?i64 = msg`): store the source's raw bytes. This is the
		// err-message path and it is exactly what makes `err('msg')` survive in
		// a narrow option — the old code str_clone'd and stored only the heap
		// pointer, so len/cap were lost and the clone leaked.
		if srcT != "" && srcT != payloadLT {
			c.optStoreRawBytes(dstSlot, 0, srcT, src, sv)
			return nil
		}
		if sv == "" {
			c.optStoreTag(dstSlot, 0)
			return nil
		}
		if c.optionPayloadInline(payloadLT) {
			c.optStoreTag(dstSlot, 0)
			c.optStoreInlinePayload(dstSlot, payloadLT, sv)
			return nil
		}
		srcAddr := c.addrOfValue(src, srcT, sv)
		if srcAddr == "" {
			c.optStoreTag(dstSlot, 0)
			return nil
		}
		c.optStoreBoxedPayload(dstSlot, 0, payloadLT, srcAddr)
		return nil
	}
	if !isOptionType(dstT) && isOptionType(srcT) {
		payloadLT := c.optPayloadLTOf(inst.Args[0])
		optSlot := c.optSlotOfValue(inst.Args[0])
		if optSlot == "" || dstSlot == "" {
			c.fail("option peel has no slot in func %d", c.cf)
			return fmt.Errorf("option peel slot")
		}
		// Owned-string payload: extracting the payload copies the
		// {len,cap,data} triple by value, so the destination would SHARE the
		// option's heap buffer. nolang `x = opt` does NOT transfer ownership
		// (the option may be unwrapped again later — str.replace-n unwraps the
		// same `?str` twice), so both would drop the SAME buffer -> double
		// free. Clone so the destination owns an independent buffer; the option
		// keeps its own and frees it exactly once on its own drop.
		if payloadLT == "%str-long" && dstT == "%str-long" {
			addr := c.optPayloadTypedAddr(optSlot, payloadLT, "%str-long")
			c.loadSeq++
			u1 := fmt.Sprintf("%%mvu%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = load %%str-long, %%str-long* %s\n", u1, addr))
			c.loadSeq++
			cl := fmt.Sprintf("%%mvucl%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_clone(%%str-long %s)\n", cl, u1))
			c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", cl, dstSlot))
			return nil
		}
		// `err` arm: `it` is the err message `str` but the option's declared
		// payload is the OK payload's type. The wrap side stores the message's
		// raw bytes in the slot, so read them back with the same raw view.
		// Reading a %str-long out of the 24-byte slot is always in bounds —
		// that width is the invariant the whole layout is built on.
		if dstT == "%str-long" || dstT == payloadLT {
			if c.optReadPayloadInto(optSlot, payloadLT, dstT, dstSlot) {
				return nil
			}
		}
		// Mismatched scalar widths: load the declared payload and coerce
		// (`?byte` yields an i64 payload that must be truncated to i8 — storing
		// the raw payload is rejected by opt and killed the whole `str[i]` ->
		// `?byte` family).
		addr := c.optPayloadTypedAddr(optSlot, payloadLT, payloadLT)
		c.loadSeq++
		u1 := fmt.Sprintf("%%mvu%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", u1, payloadLT, payloadLT, addr))
		if cv := c.coerce(payloadLT, u1, dstT); cv != "" {
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, cv, dstT, dstSlot))
			return nil
		}
		// Structurally incompatible payload/destination (a type-punned
		// option): store through a bitcast pointer. Only the tag is ever read
		// at runtime.
		c.loadSeq++
		bc := fmt.Sprintf("%%mvub%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to %s*\n", bc, dstT, dstSlot, payloadLT))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", payloadLT, u1, payloadLT, bc))
		return nil
	}
	// An option→option assignment of a BOXED payload must deep-copy the box:
	// a bitwise copy of the 32-byte option would make both sides share one
	// heap block, which the inline layout never did (it copied the payload).
	if isOptionType(dstT) && isOptionType(srcT) {
		if ok, elem := c.optElemRawOf(inst.Args[0]); ok {
			_, payloadLT := c.optionType(elem)
			if !c.optionPayloadInline(payloadLT) {
				if srcSlot := c.optSlotOfValue(inst.Args[0]); srcSlot != "" && dstSlot != "" {
					c.optBoxClone(srcSlot, dstSlot, payloadLT)
					return nil
				}
			}
		}
	}
	// Fixed-array source into a slice (%vec) destination: `a = x` where x is a
	// locally-materialized array literal. A plain `load %vec, [N x T]* src` would
	// reinterpret the array's first three elements as {len,cap,data} (len=first
	// element) — e.g. get-pair's `x=[1,2,3]; a=x` read len=1. Build a real slice
	// view instead. Mirrors the call-site / emitSetField array->vec coercion.
	if dstT == "%vec" && strings.HasPrefix(srcT, "[") {
		v := c.vecFromArraySink(srcT, srcSlot)
		c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", v, dstSlot))
		return nil
	}
	// #83 SROA guard: for large aggregate types (user structs like %json_json
	// at ~22KB, or their containing option/struct types), a `load T, T* src`
	// + `store T, T* dst` pair exposes the entire struct's internal layout to
	// LLVM's SROA pass, which tries to scalarize every field — 22KB of nested
	// arrays explodes into tens of thousands of SSA values and stalls opt
	// for 25+ seconds. Using `llvm.memcpy` instead hides the struct internals
	// from SROA (memcpy of an opaque byte blob is not scalarizable), reducing
	// compile time from 43s to under 1s for the json test case.
	if c.shouldUseMemcpy(dstT) {
		sz := c.typeSizeOperand(dstT)
		c.emitMemcpy(dstSlot, srcSlot, sz)
		return nil
	}
	// Use a unique temp name (%mv<instID>) because Dst is an existing variable
	// slot already defined by its original binding, so reusing %c<Dst> would
	// collide.
	tmp := fmt.Sprintf("%%mv%d", inst.ID)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", tmp, dstT, dstT, srcSlot))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, tmp, dstT, dstSlot))
	return nil
}

// clonePtrStructKey returns the struct key of an OpClone/OpMove payload that is
// a struct owning separately-allocated pointees. A bitwise copy of such a value
// would leave the source and destination SHARING those pointees.
//
// The destination's type wins when it has one (emitMove types the copy by the
// destination), falling back to the source.
func (c *codegen) clonePtrStructKey(inst *Inst) (string, bool) {
	// NOT gated on FieldPtrLayout: `#{inline=false}` makes a field a pointer in
	// the default state too, and the struct key test below asks FieldIsPointer,
	// not the switch. Gating here would skip the deep copy for exactly that
	// struct.
	if len(inst.Args) == 0 {
		return "", false
	}
	dstVal := inst.Dst
	if dstVal == NoVal && len(inst.Args) >= 2 {
		dstVal = inst.Args[1]
	}
	for _, v := range []ValueID{dstVal, inst.Args[0]} {
		if v <= NoVal {
			continue
		}
		t := c.mod.Type(c.localTypeOf(v))
		if t == nil || t.Kind != KindStruct {
			continue
		}
		if key := c.mod.StructKeyOf(t.Raw); key != "" && c.mod.StructHasPtrFields(key) {
			return key, true
		}
	}
	return "", false
}

// emitPtrStructClone lowers a deep copy of a struct with pointer fields:
// a bitwise copy of the struct, then a fresh pointee for every pointer field
// (recursively), so the destination shares no heap with the source.
//
// This is the CLONE half of the plan's clone-vs-move decision; the MOVE half is
// the unchanged bitwise copy in emitMove, chosen by the analysis when the source
// is provably dead afterwards.
func (c *codegen) emitPtrStructClone(inst *Inst, key string) error {
	dstVal := inst.Dst
	if dstVal == NoVal && len(inst.Args) >= 2 {
		dstVal = inst.Args[1]
	}
	dstSlot := c.valSlot[dstVal]
	srcSlot := c.valSlot[inst.Args[0]]
	if dstSlot == "" || srcSlot == "" {
		c.fail("clone: missing slot in func %d", c.cf)
		return fmt.Errorf("clone slot")
	}
	structLT := "%" + sanitize(key)
	if t := c.mod.Type(c.localTypeOf(dstVal)); t != nil {
		if lt := c.llvmTypeOf(t); lt != "" {
			structLT = lt
		}
	}
	// 1. Bitwise copy: scalars, inline members, and the pointer VALUES.
	c.emitMemcpy(dstSlot, srcSlot, c.typeSizeOperand(structLT))
	// 2. Give every pointer field its own pointee, recursively.
	c.emitPtrFieldsClone(dstSlot, srcSlot, structLT, key, map[string]bool{})
	// 3. Give every inline owned `str` leaf its own buffer too. A struct can
	//    have BOTH (pointees and leaves); without this the leaves stay shared.
	c.emitLeafFieldsClone(dstSlot, structLT, key)
	return nil
}

// cloneLeafStructKey returns the struct key of an OpClone payload that has no
// pointer fields but DOES carry inline owned `str` leaves — the case
// clonePtrStructKey rejects, and the common one (`person { name str }`).
func (c *codegen) cloneLeafStructKey(inst *Inst) (string, bool) {
	if len(inst.Args) == 0 {
		return "", false
	}
	dstVal := inst.Dst
	if dstVal == NoVal && len(inst.Args) >= 2 {
		dstVal = inst.Args[1]
	}
	for _, v := range []ValueID{dstVal, inst.Args[0]} {
		if v <= NoVal {
			continue
		}
		t := c.mod.Type(c.localTypeOf(v))
		if t == nil || t.Kind != KindStruct {
			continue
		}
		if key := c.mod.StructKeyOf(t.Raw); key != "" && c.mod.StructHasOwnedLeafFields(key) {
			return key, true
		}
	}
	return "", false
}

// emitLeafStructClone lowers a deep copy of a struct whose only shared heap is
// inline owned `str` leaves: a bitwise copy, then @str_clone per leaf. Without
// it `b = a` leaves a.name and b.name pointing at one buffer, so the first
// `b.name = x` would free a buffer `a` still reads (verified: leafshare.no went
// from `A/A/B` to `A//B` when emitSetField freed the old occupant).
func (c *codegen) emitLeafStructClone(inst *Inst, key string) error {
	dstVal := inst.Dst
	if dstVal == NoVal && len(inst.Args) >= 2 {
		dstVal = inst.Args[1]
	}
	dstSlot := c.valSlot[dstVal]
	srcSlot := c.valSlot[inst.Args[0]]
	if dstSlot == "" || srcSlot == "" {
		c.fail("clone: missing slot in func %d", c.cf)
		return fmt.Errorf("clone slot")
	}
	structLT := "%" + sanitize(key)
	if t := c.mod.Type(c.localTypeOf(dstVal)); t != nil {
		if lt := c.llvmTypeOf(t); lt != "" {
			structLT = lt
		}
	}
	c.emitMemcpy(dstSlot, srcSlot, c.typeSizeOperand(structLT))
	c.emitLeafFieldsClone(dstSlot, structLT, key)
	return nil
}

// emitLeafFieldsClone replaces each inline owned `str` leaf of the struct at
// dstSlot with a fresh @str_clone of it.
//
// Discarding the descriptor that the preceding bitwise copy installed is NOT a
// leak: that buffer is the SOURCE's, and the source still owns and drops it.
func (c *codegen) emitLeafFieldsClone(dstSlot, structLT, key string) {
	c.emitLeafFieldsCloneR(dstSlot, structLT, key, 0)
}

// emitLeafFieldsCloneR is the recursive form. The recursion through inline
// nested structs is NOT optional: emitStructDropHelper frees those nested
// leaves too, so a clone that stopped at the top level would leave them shared
// while both copies are dropped — a double free. Drop and clone must walk the
// same shape.
//
// depth caps the walk. A by-value inline cycle is rejected by
// ValidateFieldTags, so the cap is belt-and-braces against a StructFields
// entry that resolves to itself.
func (c *codegen) emitLeafFieldsCloneR(dstSlot, structLT, key string, depth int) {
	if depth > 8 {
		return
	}
	fields := c.mod.StructFields[key]
	for _, i := range c.mod.StructOwnedLeafFieldIdxs(key) {
		fLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		if fLT != "%str-long" {
			continue
		}
		g := c.treg("lfc")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", g, structLT, structLT, dstSlot, i))
		v := c.treg("lfv")
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", v, fLT, fLT, g))
		cl := c.treg("lfl")
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, fLT, fLT, v))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fLT, cl, fLT, g))
	}
	for i := range fields {
		if c.mod.FieldIsPointer(fields[i]) || !c.mod.IsStructType(fields[i].TypeRaw) {
			continue
		}
		subKey := c.mod.StructKeyOf(fields[i].TypeRaw)
		subLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		if subKey == "" || isBuiltinContainerLT(subLT) {
			continue
		}
		if !c.inlineSubOwnsLeavesOnly(subKey) {
			continue
		}
		g := c.treg("lfs")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", g, structLT, structLT, dstSlot, i))
		c.emitLeafFieldsCloneR(g, subLT, subKey, depth+1)
	}
}

// cloneStructElemLeaves deep-copies the inline owned `str` leaves of the struct
// element that was just stored BY VALUE at `eptr`.
//
// Container element writes (vec.push / vec.insert / v[i] = x) store the whole
// struct by value, so without this the element keeps sharing the source local's
// buffer. That sharing is invisible today only because nothing frees a struct's
// leaves at scope exit; the moment a drop does, the source local's drop pulls
// the buffer out from under the container (tests/test-diff-debug.no aborts with
// trace/BPT trap: `op = diff-op {}`; `op.line = ...`; `ops.push(op)` inside a
// loop, where `op` dies at the bottom of every iteration).
//
// Non-struct element types (scalars, %str-long, %vec) are left to their own
// already-correct paths — structKeyOfLLVM returns "" for them and this is a
// no-op.
func (c *codegen) cloneStructElemLeaves(eptr, elemLL string) {
	if !strings.HasPrefix(elemLL, "%") {
		return
	}
	// Built-in container descriptors are NOT user structs, but they ARE in
	// StructFields and %vec's `data` field is registered as an owned `str`.
	// Letting the lookup below see them "deep clones" a vec element's data
	// pointer as if it were a string: `store %str-long` (24 bytes) into an
	// 8-byte field. Verified: tests/mem-safety/nested-container-clone.no
	// aborts with trace/BPT trap before printing its first line, because
	// `a.push(make-vec3(1,2,3))` on a `[][]i64` hits exactly this.
	if isBuiltinContainerLT(elemLL) {
		return
	}
	key := c.structKeyOfLLVM(elemLL)
	if key == "" || !c.mod.StructHasOwnedLeafFields(key) {
		return
	}
	c.emitLeafFieldsClone(eptr, elemLL, key)
}

// emitPtrFieldsClone replaces each pointer field of the struct at dstSlot with a
// fresh allocation holding a copy of the corresponding source pointee, and
// recurses into the copies so the two structs share nothing at any depth.
//
// `onPath` breaks recursion on a self-referential struct (a linked list or
// tree). Walking the chain IS the correct value semantics, but it is unbounded —
// and a CYCLE would never terminate. A cycle can only be built through a pointer
// field, which ValidateFieldTags cannot reject statically (it only catches
// by-value inline cycles), so the deeper levels are left SHARED and this is
// recorded as a known limitation rather than allowed to hang.
func (c *codegen) emitPtrFieldsClone(dstSlot, srcSlot, structLT, key string, onPath map[string]bool) {
	if onPath[key] {
		return
	}
	onPath[key] = true
	defer delete(onPath, key)
	fields := c.mod.StructFields[key]
	for _, i := range c.mod.StructPtrFieldIdxs(key) {
		pointeeLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		if pointeeLT == "" {
			continue
		}
		// The DESTINATION gets a FRESH allocation, never @__nolang_get_<T>.
		// The bitwise copy at the top of emitPtrStructClone already copied the
		// source's pointer into this field, so `get` would see it non-NULL and
		// hand back that very pointer — making the two structs share one pointee
		// and the "deep copy" a no-op (which is exactly what it did before this
		// was split out). Discarding the copied pointer is not a leak: it is the
		// SOURCE's pointer, and the source still owns it.
		df := c.treg("pcd")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", df, structLT, structLT, dstSlot, i))
		dp := c.emitAllocPointee(pointeeLT)
		c.sb.WriteString(fmt.Sprintf("  store %s* %s, %s** %s\n", pointeeLT, dp, pointeeLT, df))
		// The SOURCE goes through @__nolang_get_<T>: its pointee may be NULL (a
		// freshly declared struct), and a memcpy from NULL would fault.
		// Allocating into the source is a benign side effect — it makes the
		// source's own field valid, and the source still owns and drops it.
		sf := c.treg("pcs")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", sf, structLT, structLT, srcSlot, i))
		sp := c.ptrFieldAddr(pointeeLT, sf)
		c.emitMemcpy(dp, sp, c.typeSizeOperand(pointeeLT))
		if sub := c.mod.StructKeyOf(fields[i].TypeRaw); sub != "" {
			// The pointee's OWN inline leaves must be cloned too. The memcpy
			// above copied the {len,cap,data} triples, so without this the two
			// pointees share one buffer — and since emitStructDropHelper now
			// frees a pointee's leaves, both sides would free it: a double
			// free. This is the pointer-field mirror of the inline recursion
			// in emitLeafFieldsCloneR.
			if c.mod.StructHasOwnedLeafFields(sub) {
				c.emitLeafFieldsClone(dp, pointeeLT, sub)
			}
			if c.mod.StructHasPtrFields(sub) {
				c.emitPtrFieldsClone(dp, sp, pointeeLT, sub, onPath)
			}
		}
	}
}

// structDropName is the LLVM name of the recursive destructor for a struct type.
func structDropName(lt string) string {
	return "__nolang_drop_" + sanitize(strings.TrimPrefix(lt, "%"))
}

// structKeyOfLLVM resolves an LLVM struct type name (`%os_utsname`) back to the
// StructFields key (`os.utsname`). The sanitized form is tried first so a name
// that legitimately contains an underscore is not mangled into a dotted one.
func (c *codegen) structKeyOfLLVM(lt string) string {
	name := strings.TrimPrefix(lt, "%")
	if k := c.mod.StructKeyOf(name); k != "" {
		return k
	}
	if k := c.mod.StructKeyOf(unsanitize(name)); k != "" {
		return k
	}
	// Last resort: invert sanitize() using the real keys. unsanitize() only
	// re-inserts '.', so a key containing any OTHER separator is unreachable
	// through it — `my-thing` becomes `%my_thing`, and neither StructKeyOf
	// ("my_thing") nor StructKeyOf("my.thing") matches the key `my-thing`.
	//
	// This is not cosmetic: the caller (emitDrop) uses the result to decide
	// whether a struct owns pointees and therefore needs a recursive drop. A
	// failed lookup returned "" -> StructHasPtrFields("") is false -> the drop
	// was silently SKIPPED, leaking every pointee of any dashed-name struct.
	// Verified: `my-thing { p inner }` emitted no @__nolang_drop_my_thing at all,
	// while the identical undashed `mything` emitted one.
	return c.sanitizedStructKey(name)
}

// emitStructDropHelper emits the recursive destructor for one struct type: it
// frees the pointee of every pointer field (recursing into it first), and
// nothing else.
//
// SCOPE — what this deliberately does NOT free yet: owned LEAF fields (str / vec
// / []T / map) whose descriptor is inlined in the struct. Their buffers are
// reachable only through the field, and the current read path
// (`isBorrowRead`'s "the struct owns its fields; a field read borrows") hands out
// aliases of them, so freeing them here would double-free against a value that
// was legitimately read out. Landing leaf frees needs the clone/move machinery to
// be in place for field READS too, and is tracked as the next step. Structs
// therefore still leak their leaf buffers, exactly as they did before Phase 1 —
// this change fixes the leak Phase 1 itself introduced (the pointees), and does
// not make the pre-existing one worse.
//
// Each pointer field is null-checked before being recursed into and freed: a
// freshly declared struct has NULL pointees, and `free` on NULL is legal but a
// recursive `load` through NULL is not.
func (c *codegen) emitStructDropHelper(lt, key string) {
	fn := structDropName(lt)
	if c.extraFuncs[fn] {
		return
	}
	c.extraFuncs[fn] = true
	fields := c.mod.StructFields[key]
	idx := c.mod.StructPtrFieldIdxs(key)
	c.decl("declare void @free(i8*)")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("define void @%s(%s* %%p) {\n", fn, lt))
	b.WriteString("entry:\n")
	b.WriteString("  br label %b0\n")
	for n, i := range idx {
		pointeeLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		next := fmt.Sprintf("b%d", n+1)
		b.WriteString(fmt.Sprintf("b%d:\n", n))
		b.WriteString(fmt.Sprintf("  %%f%d = getelementptr inbounds %s, %s* %%p, i32 0, i32 %d\n", n, lt, lt, i))
		b.WriteString(fmt.Sprintf("  %%q%d = load %s*, %s** %%f%d\n", n, pointeeLT, pointeeLT, n))
		b.WriteString(fmt.Sprintf("  %%z%d = icmp eq %s* %%q%d, null\n", n, pointeeLT, n))
		b.WriteString(fmt.Sprintf("  br i1 %%z%d, label %%%s, label %%r%d\n", n, next, n))
		b.WriteString(fmt.Sprintf("r%d:\n", n))
		if sub := c.mod.StructKeyOf(fields[i].TypeRaw); sub != "" && c.mod.StructHasPtrFields(sub) {
			c.emitStructDropHelper(pointeeLT, sub)
			b.WriteString(fmt.Sprintf("  call void @%s(%s* %%q%d)\n", structDropName(pointeeLT), pointeeLT, n))
		}
		b.WriteString(fmt.Sprintf("  call void @free(i8* %%q%d)\n", n))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", next))
	}
	b.WriteString(fmt.Sprintf("b%d:\n", len(idx)))
	// Inline owned leaves: @str_free per `str` field. @str_free already skips
	// cap==0 / null data, so a zero-initialised struct is a no-op and no
	// null check is needed here.
	for n, i := range c.mod.StructOwnedLeafFieldIdxs(key) {
		fLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		if fLT != "%str-long" {
			continue
		}
		b.WriteString(fmt.Sprintf("  %%lf%d = getelementptr inbounds %s, %s* %%p, i32 0, i32 %d\n", n, lt, lt, i))
		b.WriteString(fmt.Sprintf("  %%lv%d = load %s, %s* %%lf%d\n", n, fLT, fLT, n))
		b.WriteString(fmt.Sprintf("  call void @str_free(%s %%lv%d)\n", fLT, n))
	}
	// Inline nested structs: their leaves are part of THIS struct's storage,
	// so they must be freed by this destructor — and, symmetrically, cloned
	// by emitLeafFieldsClone. A nested struct whose leaves are freed here
	// while the clone left them shared is a double free, which is why the
	// two walks must be the same traversal.
	sub := 0
	for i := range fields {
		if c.mod.FieldIsPointer(fields[i]) || !c.mod.IsStructType(fields[i].TypeRaw) {
			continue // pointees are handled above; non-structs own nothing
		}
		subKey := c.mod.StructKeyOf(fields[i].TypeRaw)
		subLT := c.llvmTypeOf(c.mod.Type(c.mod.internType(fields[i].TypeRaw)))
		if subKey == "" || isBuiltinContainerLT(subLT) {
			continue
		}
		if !c.inlineSubOwnsLeavesOnly(subKey) {
			continue
		}
		c.emitStructDropHelper(subLT, subKey)
		b.WriteString(fmt.Sprintf("  %%ls%d = getelementptr inbounds %s, %s* %%p, i32 0, i32 %d\n", sub, lt, lt, i))
		b.WriteString(fmt.Sprintf("  call void @%s(%s* %%ls%d)\n", structDropName(subLT), subLT, sub))
		sub++
	}
	b.WriteString("  ret void\n}\n")
	c.extraFuncsBody.WriteString(b.String())
}

// inlineSubOwnsLeavesOnly decides whether the drop/clone walk may recurse into
// an INLINE nested struct. It requires owned leaves and NO pointer fields.
//
// Pointer fields are the disqualifier: emitLeafFieldsCloneR can only deep-copy
// `str` leaves. If it recursed into a nested struct that owns pointees, the
// drop would free those pointees while the clone left them shared — a double
// free. Emitting nothing is the safe side to err on (it leaks, it does not
// corrupt), and it is what emitPtrStructClone already does for self-reference.
func (c *codegen) inlineSubOwnsLeavesOnly(subKey string) bool {
	return c.mod.StructHasOwnedLeafFields(subKey) && !c.mod.StructHasPtrFields(subKey)
}

// structSlotNeedsZero reports whether a local of LLVM type lt will get a
// destructor (emitDrop) and therefore must never be left uninitialized. It is
// deliberately the same predicate as the drop: pointer fields OR inline owned
// leaves, with containers excluded (see isBuiltinContainerLT).
func (c *codegen) structSlotNeedsZero(lt string) bool {
	if isBuiltinContainerLT(lt) {
		return false
	}
	key := c.structKeyOfLLVM(lt)
	if key == "" {
		return false
	}
	return c.mod.StructHasPtrFields(key) || c.mod.StructHasOwnedLeafFields(key)
}

// isBuiltinContainerLT reports whether an LLVM type name is a runtime container
// descriptor. Those ARE in StructFields (%vec's `data` is an owned `str`), so
// any "does this type own leaves" test must reject them first — see
// cloneStructElemLeaves.
func isBuiltinContainerLT(lt string) bool {
	return lt == "%vec" || lt == "%str-long" || strings.HasPrefix(lt, "%option")
}

func (c *codegen) emitClone(inst *Inst) error {
	if lt, _ := c.ptype(inst.Args[0]); lt == "%str-long" {
		dstT, _ := c.ptype(inst.Dst)
		dstSlot := c.valSlot[inst.Dst]
		_, srcV := c.loadVal(inst.Args[0])
		// A true deep copy: @str_clone duplicates the buffer at the SAME length.
		// The previous code used @str_concat(s, s), which concatenates the
		// string with ITSELF and doubles the length — so every byte copied out
		// of a cloned string (e.g. the `?str` receiver unwrap in
		// str.replace-n's `s = parts[i]` inner loop) was emitted twice,
		// producing `aa_bb-cc` instead of `a_b-c`.
		tmp := fmt.Sprintf("%%cl%d", inst.ID)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", tmp, dstT, dstT, srcV))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, tmp, dstT, dstSlot))
		return nil
	}
	// STRUCT WITH POINTER FIELDS: a bitwise copy (what emitMove would do) leaves
	// the two values sharing their pointees, so the clone must deep-copy them.
	if key, ok := c.clonePtrStructKey(inst); ok {
		return c.emitPtrStructClone(inst, key)
	}
	if key, ok := c.cloneLeafStructKey(inst); ok {
		return c.emitLeafStructClone(inst, key)
	}
	return c.emitMove(inst)
}

// elemAddr computes the address of element `idx` of a container value held in
// arrSlot. The container layout depends on arrT:
//   - %str-long {i64, i64, i8*} : data is field 2 (i8*); index a byte.
//   - %vec      {i64, i64, i64} : data is field 2 (pointer stored as i64);
//     inttoptr to the element-type pointer, then index.
//   - fixed array [N x T]      : the array type is its own element type; a
//     single-level GEP does the right thing.
//
// Returns the element pointer register (typed `<elemT>*`).
func (c *codegen) elemAddr(src ValueID, arrSlot, idxV, arrT, elemT string) string {
	// An option whose payload is a container is indexed through the payload
	// (mirrors lowerer.elemTypeOfType): a match arm binds `it` to the option,
	// so `it[0]` on `?[]byte` GEPs through the option payload slot (%vec)
	// first. Doing the GEP on the option struct directly is rejected by opt
	// with "invalid getelementptr indices".
	if isOptionType(arrT) {
		// The unified option is 32 bytes of {tag, slot}, and the payload is
		// reached the same way everywhere else — through optPayloadAddr, so a
		// BOXED payload is dereferenced rather than read out of the slot.
		payloadLT := c.optPayloadLTFor(src)
		if payloadLT == "" {
			payloadLT = "i64"
		}
		addr := c.optPayloadTypedAddr(arrSlot, payloadLT, payloadLT)
		return c.elemAddrRaw(addr, idxV, payloadLT, elemT)
	}
	return c.elemAddrRaw(arrSlot, idxV, arrT, elemT)
}

// elemAddrRaw is the pre-option-peel body of elemAddr: it computes the address
// of element `idxV` inside a container of LLVM type arrT held at arrSlot.
func (c *codegen) elemAddrRaw(arrSlot, idxV, arrT, elemT string) string {
	switch arrT {
	case "%str-long":
		g := c.treg("eg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g, arrSlot))
		d := c.treg("ed")
		c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", d, g))
		p := c.treg("ep")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr i8, i8* %s, i64 %s\n", p, d, idxV))
		return p
	case "%vec":
		g := c.treg("eg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g, arrSlot))
		pi := c.treg("epi")
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", pi, g))
		dp := c.treg("edp")
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s*\n", dp, pi, elemT))
		p := c.treg("ep")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 %s\n", p, elemT, elemT, dp, idxV))
		return p
	case "%txt":
		// %txt = type { [255 x i8], i8 }. Element i lives in the field-0 data
		// buffer, so GEP to field 0 then index into the [255 x i8] array. The
		// GEP element type must be the array type [255 x i8] (matching the
		// base pointer's pointee), NOT the element i8 — using i8 produced
		// "invalid getelementptr indices".
		g := c.treg("eg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0\n", g, arrSlot))
		p := c.treg("ep")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr [255 x i8], [255 x i8]* %s, i64 0, i64 %s\n", p, g, idxV))
		return p
	default:
		p := c.treg("ep")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 0, i64 %s\n", p, arrT, arrT, arrSlot, idxV))
		return p
	}
}

// mirScalarByteSize maps the LLVM-level scalar MIR types to their byte sizes.
var mirScalarByteSize = map[string]int64{
	"i1": 1, "i8": 1, "i16": 2, "i32": 4, "i64": 8,
	"double": 8, "float": 4,
}

// mirStaticTypeSize returns sizeof(lt) when it is known at code-emission time.
// It covers the scalars plus the three fixed MIR aggregates; every other type
// (user struct, boxed option payload, nested fixed array) is sized by the
// opt-foldable `getelementptr` trick in typeSizeOperand instead of duplicating
// LLVM's layout rules here.
// c.mirStaticTypeSize is the codegen-aware form: %option's size depends on the
// module's payload-slot width, which is only known after initOptionSlot.
//
// The package-level mirStaticTypeSize below keeps the old signature for the
// call sites that genuinely cannot see a codegen (none today) and defaults
// %option to the DEFAULT slot size — correct for any module that did not
// configure a threshold, and only ever used where an approximation is fine.
func (c *codegen) mirStaticTypeSize(lt string) (int64, bool) {
	if lt == "%option" && c.optSlotBytes > 0 {
		return 8 + c.optSlotBytes, true
	}
	return mirStaticTypeSize(lt)
}

func mirStaticTypeSize(lt string) (int64, bool) {
	if n, ok := mirScalarByteSize[lt]; ok {
		return n, true
	}
	switch lt {
	case "%str-long", "%vec":
		return 24, true // {i64,i64,i8*} / {i64,i64,i64}
	case "%option":
		return 8 + optSlotDefaultBytes, true // { i64 tag, [N x i64] slot } at the default threshold
	}
	return 0, false
}

// typeSizeOperand returns an i64 LLVM operand equal to sizeof(lt): an inline
// literal for the well-known types, otherwise a freshly emitted (and by opt
// constant-folded) `ptrtoint (getelementptr (T, ptr null, i64 1))`.
func (c *codegen) typeSizeOperand(lt string) string {
	if n, ok := c.mirStaticTypeSize(lt); ok {
		return fmt.Sprintf("%d", n)
	}
	c.loadSeq++
	r := fmt.Sprintf("%%tsz%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint ptr getelementptr (%s, ptr null, i64 1) to i64\n", r, lt))
	return r
}

// shouldUseMemcpy reports whether a load+store of LLVM type `lt` should be
// replaced by an equivalent `llvm.memcpy` to avoid SROA scalar-replacement
// explosion on large aggregate types.
//
// SROA decomposes every alloca that participates in a `load T` / `store T`
// into individual scalar fields. For a large struct like %json_json (~22KB,
// containing [64]json_value each holding [16]str + [16]i64), this produces
// thousands of SSA values and stalls opt for tens of seconds. Using memcpy
// instead hides the struct layout from SROA (the pass treats memcpy as an
// opaque byte copy and does not scalarize through it), bringing compile time
// from ~43s down to under 1s.
//
// The threshold of 4096 bytes is chosen so that medium-sized structs with
// flat arrays of small elements (e.g. %http_response at ~1.6KB, containing
// [32 x %str-long]) still use normal load+store — SROA handles these fine —
// while truly large nested structs like %json_json (~22KB, with [64]json_value
// each holding [16]str + [16]i64) use memcpy to avoid the SROA explosion.
//
// Unlike the previous name-based heuristic, this method recursively computes
// the actual type size using c.mod.StructFields, so small user-defined structs
// (e.g. %MyType { %str-long, %vec } = 48B) correctly use normal load+store.
func (c *codegen) shouldUseMemcpy(lt string) bool {
	// Only aggregate types (named structs, options wrapping structs, fixed
	// arrays of aggregates) can be large enough to matter; scalars and the
	// small MIR builtins (%str-long, %vec, %option) are always fine.
	if lt == "" || lt == "void" || lt == "i64" || lt == "double" || lt == "i1" || lt == "i8" {
		return false
	}
	if lt == "%str-long" || lt == "%vec" || lt == "%option" || lt == "%txt" {
		return false
	}
	// Compute the actual type size; only use memcpy for types above the
	// SROA explosion threshold. If we cannot determine the size (unknown
	// type), default to false (normal load+store) — this is safe because
	// SROA only explodes on *known* large structs with nested arrays.
	size, ok := c.computeTypeSize(lt, map[string]bool{})
	if !ok {
		return false
	}
	return size > 4096
}

// sanitizedStructKey resolves a sanitized LLVM struct name (`json_json_pool`)
// back to its StructFields key (`json.json-pool`), returning "" when the name is
// unknown or ambiguous.
//
// This is the missing inverse of sanitize(). sanitize() replaces every
// non-alphanumeric rune with '_', so '.' and '-' are indistinguishable
// afterwards, and unsanitize() only reconstructs the dotted form — a key like
// `json.json-pool` therefore cannot be recovered by it. The index is built once
// from the actual StructFields keys, which is the only place the original
// spelling still exists.
//
// Ambiguous names (two keys sanitizing identically) are DROPPED rather than
// resolved arbitrarily: the build iterates a Go map, so keeping "the first one"
// would make the result depend on map iteration order — and a golden baseline
// must not drift. Dropping them keeps the previous conservative behaviour.
func (c *codegen) sanitizedStructKey(san string) string {
	if c.sanIdx == nil {
		idx := make(map[string]string, len(c.mod.StructFields))
		ambiguous := map[string]bool{}
		for k := range c.mod.StructFields {
			s := sanitize(k)
			if _, dup := idx[s]; dup {
				ambiguous[s] = true
				continue
			}
			idx[s] = k
		}
		for s := range ambiguous {
			delete(idx, s)
		}
		c.sanIdx = idx
	}
	return c.sanIdx[san]
}

// computeTypeSize recursively computes the byte size of an LLVM type string.
// Returns (size, true) when the size is known, or (0, false) for unknown types.
// The visited map prevents infinite recursion on self-referential struct types.
func (c *codegen) computeTypeSize(lt string, visited map[string]bool) (int64, bool) {
	// Scalars and known-small builtins.
	if n, ok := c.mirStaticTypeSize(lt); ok {
		return n, true
	}
	if lt == "%txt" {
		return 256, true // { [255 x i8] data, i8 len } ≈ 256B
	}
	// Fixed arrays: [N x elem] -> N * sizeof(elem).
	if len(lt) > 0 && lt[0] == '[' {
		// Parse "[N x T]" — extract N and T.
		inner := lt[1:strings.LastIndex(lt, "]")]
		xIdx := strings.Index(inner, " x ")
		if xIdx < 0 {
			return 0, false
		}
		nStr := inner[:xIdx]
		elemLT := inner[xIdx+3:]
		var n int64
		for _, ch := range nStr {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			n = n*10 + int64(ch-'0')
		}
		elemSz, ok := c.computeTypeSize(elemLT, visited)
		if !ok {
			return 0, false
		}
		return n * elemSz, true
	}
	// Named struct types (%foo): resolve via StructFields and sum field sizes.
	if len(lt) > 1 && lt[0] == '%' {
		name := lt[1:] // strip leading '%'
		if visited[name] {
			return 0, false // recursive struct — can't compute statically
		}
		visited[name] = true
		// There is only one option type now (`%option`), and it is answered by
		// mirStaticTypeSize above; %option_<T> no longer exists.
		// Look up struct fields by the sanitized name (StructFields keys are
		// raw nolang names; the LLVM type name has dots replaced with _).
		structKey := c.structKeyOf(unsanitize(name))
		if structKey == "" {
			structKey = unsanitize(name)
		}
		fields, ok := c.mod.StructFields[structKey]
		if !ok {
			// Try the sanitized name directly (some structs are registered
			// under their sanitized name).
			fields, ok = c.mod.StructFields[name]
		}
		if !ok {
			// Last resort: invert sanitize() using the real keys. Needed for any
			// struct whose name contains a separator OTHER than '.' (e.g.
			// `json.json-pool` -> `%json_json_pool`), which unsanitize() above
			// cannot reconstruct because it only re-inserts '.'.
			if k := c.sanitizedStructKey(name); k != "" {
				fields, ok = c.mod.StructFields[k]
			}
		}
		if !ok {
			return 0, false // unknown struct — can't compute size
		}
		var total int64
		for _, f := range fields {
			// POINTER FIELD (Phase 1 layout flip): the slot holds a `%T*`, so it
			// contributes 8 bytes — NOT the pointee's inline size. Without this
			// the size is over-reported, which both mis-sizes the lazy-pointee
			// allocation and can keep a shrunk struct above shouldUseMemcpy's
			// SROA threshold.
			if c.fieldIsPointer(f) {
				total += 8
				continue
			}
			fieldLT := c.nolangTypeToLLVM(f.TypeRaw)
			fsz, ok := c.computeTypeSize(fieldLT, visited)
			if !ok {
				return 0, false
			}
			total += fsz
		}
		return total, true
	}
	return 0, false
}

// nolangTypeToLLVM converts a nolang type string (e.g. "str", "i64", "[]byte",
// "foo", "?bar") to its LLVM type string. This is a simplified version of
// llvmTypeOf that works from the raw type string instead of a Type pointer.
func (c *codegen) nolangTypeToLLVM(raw string) string {
	switch raw {
	case "str":
		return "%str-long"
	case "vec":
		return "%vec"
	case "i64", "int", "uint", "usize", "isize":
		return "i64"
	case "byte", "u8", "i8":
		return "i8"
	case "u16", "i16":
		return "i16"
	case "u32", "i32":
		return "i32"
	case "bool":
		return "i1"
	case "double", "f64", "f32", "float":
		return "double"
	}
	// Option type: ?T
	if strings.HasPrefix(raw, "?") {
		elem := raw[1:]
		if lt, _ := c.optionType(elem); lt != "" {
			return lt
		}
		return "%option"
	}
	// Slice type: []T
	if strings.HasPrefix(raw, "[]") {
		return "%vec"
	}
	// Fixed array: [N]T
	if strings.HasPrefix(raw, "[") {
		// Try to resolve via the type system.
		if tID := c.mod.internType(raw); tID != NoType {
			if ty := c.mod.Type(tID); ty != nil {
				return c.llvmTypeOf(ty)
			}
		}
		return "i64"
	}
	// Map type: [K]V
	if k, v, ok := parseMapTypes(raw); ok {
		vSan := strings.ReplaceAll(v, "[]", "slice_")
		hmName := "hashmap-" + k + "-" + vSan
		if _, ok := c.mod.StructFields[hmName]; ok {
			return "%" + sanitize(hmName)
		}
		if s := c.structLLVMType(hmName); s != "" {
			return s
		}
		return "i64"
	}
	// User struct: try StructFields lookup.
	if lt := c.structLLVMType(raw); lt != "" {
		return lt
	}
	if _, ok := c.mod.StructFields[raw]; ok {
		return "%" + sanitize(raw)
	}
	return "i64"
}

// unsanitize reverses the sanitize() transformation: replaces '_' with '.'
// when the result matches a known struct name in StructFields. This is needed
// because LLVM type names use '_' as a separator (e.g. %fs_file) while
// StructFields keys use the original nolang names (e.g. "fs.file").
func unsanitize(s string) string {
	if strings.Contains(s, "_") {
		candidate := strings.ReplaceAll(s, "_", ".")
		// We can't check StructFields here (this is a package-level function),
		// so just return the dot-version; the caller will look it up.
		return candidate
	}
	return s
}

// emitMemcpy emits a `call void @llvm.memcpy.p0.p0.i64(dst, src, size, false)`
// that copies `size` bytes from src to dst. This is semantically equivalent to
// `load T; store T` but does not expose the struct layout to SROA, avoiding
// the scalar-replacement explosion on large aggregates (#83).
// emitAllocPointee mallocs a zeroed block large enough for one `lt` and returns
// the register holding the pointer to it.
//
// The block is ZEROED on purpose: the pointee's members are owned, and the first
// store into an owned member drops the previous value first. A garbage member
// there would free() an arbitrary pointer -- the same hazard emitBuiltinAlloc
// guards against for slices.
//
// With opaque pointers the returned `i8*` IS the `%lt*` as far as LLVM is
// concerned, so callers use it directly as a typed pointer.
func (c *codegen) emitAllocPointee(lt string) string {
	c.decl("declare i8* @malloc(i64)")
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	sz := c.typeSizeOperand(lt)
	c.loadSeq++
	buf := fmt.Sprintf("%%pa%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, sz))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", buf, sz))
	return buf
}

// ptrFieldGetName is the LLVM name of the lazy-pointee accessor for a pointee
// type. The name is derived from the pointee's LLVM type (not from a struct key)
// so it is always non-empty and stable even when a type has no StructFields
// entry.
func ptrFieldGetName(pointeeLT string) string {
	return "__nolang_get_" + sanitize(strings.TrimPrefix(pointeeLT, "%"))
}

// emitPtrFieldGetHelper emits the lazy-pointee accessor for one pointee type:
//
//	define %T* @__nolang_get_T(%T** %slot) {
//	entry:
//	  %p = load %T*, %T** %slot
//	  %isnull = icmp eq %T* %p, null
//	  br i1 %isnull, label %alloc, label %done
//	alloc:
//	  %n = call i8* @malloc(sizeof(T))
//	  call void @llvm.memset.p0i8.i64(i8* %n, i8 0, i64 sizeof(T), i1 false)
//	  store %T* %n, %T** %slot
//	  br label %done
//	done:
//	  %r = phi %T* [ %p, %entry ], [ %n, %alloc ]
//	  ret %T* %r
//	}
//
// WHY A FUNCTION AND NOT INLINE CODE: `e1 employee` zero-initialises the struct,
// so every pointer field starts NULL, and `e1.addr.street = 'x'` — or even a
// plain read of `e1.addr.street` — would dereference it. Allocating on demand
// needs a conditional, and a conditional needs basic blocks, which codegen
// cannot safely introduce in the middle of an existing function body (blocks are
// emitted by iterating f.Blocks; the option-print helper documents the same
// constraint). A separate function has its own entry block and may branch freely.
//
// The call is a single instruction at the use site and the helper is small
// enough that LLVM inlines it, so the steady-state cost is one predictable
// compare plus the load that was already there.
//
// Semantics note: this makes an untouched pointer field read as ZEROS rather
// than faulting, which matches the pre-flip inline layout where the field was
// part of a zero-initialised struct.
func (c *codegen) emitPtrFieldGetHelper(pointeeLT string) {
	fn := ptrFieldGetName(pointeeLT)
	if c.extraFuncs[fn] {
		return
	}
	c.extraFuncs[fn] = true
	c.decl("declare i8* @malloc(i64)")
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	c.extraFuncsBody.WriteString(fmt.Sprintf("define %s* @%s(%s** %%slot) {\n", pointeeLT, fn, pointeeLT))
	c.extraFuncsBody.WriteString("entry:\n")
	c.extraFuncsBody.WriteString(fmt.Sprintf("  %%p = load %s*, %s** %%slot\n", pointeeLT, pointeeLT))
	c.extraFuncsBody.WriteString(fmt.Sprintf("  %%isnull = icmp eq %s* %%p, null\n", pointeeLT))
	c.extraFuncsBody.WriteString("  br i1 %isnull, label %alloc, label %done\n")
	c.extraFuncsBody.WriteString("alloc:\n")
	// sizeof(T) is obtained from LLVM itself, via the same
	// `ptrtoint (getelementptr (T, ptr null, i64 1))` idiom typeSizeOperand uses.
	//
	// This deliberately does NOT call computeTypeSize. That sizer resolves a
	// pointee LLVM type name back to a StructFields key, and sanitize() collapses
	// BOTH '.' and '-' to '_' — so the key `json.json-pool` becomes
	// `%json_json_pool`, which unsanitize() (which only tries '.') cannot map
	// back. `?json` therefore failed to compile with
	// "lazy pointee accessor: cannot size %json_json_pool".
	//
	// Deriving the size from LLVM also means the malloc is guaranteed to match
	// the layout LLVM actually assigns, rather than trusting a second, divergent
	// size calculator — the class of disagreement opaque pointers cannot catch.
	// The instruction is a constant expression and is folded away by opt.
	c.extraFuncsBody.WriteString(fmt.Sprintf("  %%sz = ptrtoint ptr getelementptr (%s, ptr null, i64 1) to i64\n", pointeeLT))
	c.extraFuncsBody.WriteString("  %n = call i8* @malloc(i64 %sz)\n")
	c.extraFuncsBody.WriteString("  call void @llvm.memset.p0i8.i64(i8* %n, i8 0, i64 %sz, i1 false)\n")
	c.extraFuncsBody.WriteString(fmt.Sprintf("  store %s* %%n, %s** %%slot\n", pointeeLT, pointeeLT))
	c.extraFuncsBody.WriteString("  br label %done\n")
	c.extraFuncsBody.WriteString("done:\n")
	c.extraFuncsBody.WriteString(fmt.Sprintf("  %%r = phi %s* [ %%p, %%entry ], [ %%n, %%alloc ]\n", pointeeLT))
	c.extraFuncsBody.WriteString("  ret " + pointeeLT + "* %r\n}\n")
}

// ptrFieldAddr returns a `%T*` operand for the pointee of the pointer field
// whose slot address is `fieldSlotAddr` (`%T**`), allocating it on demand.
func (c *codegen) ptrFieldAddr(pointeeLT, fieldSlotAddr string) string {
	c.emitPtrFieldGetHelper(pointeeLT)
	p := c.treg("pfg")
	c.sb.WriteString(fmt.Sprintf("  %s = call %s* @%s(%s** %s)\n", p, pointeeLT, ptrFieldGetName(pointeeLT), pointeeLT, fieldSlotAddr))
	return p
}

func (c *codegen) emitMemcpy(dst, src, size string) {
	c.decl("declare void @llvm.memcpy.p0.p0.i64(ptr noalias nocapture writeonly, ptr noalias nocapture readonly, i64, i1 immarg)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0.p0.i64(ptr %s, ptr %s, i64 %s, i1 false)\n", dst, src, size))
}

// allocBytesOperand returns an i64 operand holding cap * sizeof(elemLT), the
// byte count of a slice/string backing store with `cap` slots.
//
// A slice's backing store holds ELEMENT-sized slots: []str is an array of
// 24-byte %str-long, []?i64 of 32-byte %option. Sizing it with a constant 8
// under-allocates, and a later `a[i] = v` then loads a whole element out of the
// end of the block, so the element's `data` field is heap garbage and
// @str_free aborts with "pointer being freed was not allocated"
// (SIGABRT at -O0, the same UB turning into SIGTRAP at -O3).
func (c *codegen) allocBytesOperand(capV, elemLT string) string {
	n, ok := c.mirStaticTypeSize(elemLT)
	if ok && n == 1 {
		return capV // str / []byte: one byte per slot
	}
	sz := c.typeSizeOperand(elemLT)
	c.loadSeq++
	r := fmt.Sprintf("%%basz%d", c.loadSeq)
	if ok {
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", r, capV, n))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", r, capV, sz))
	}
	return r
}

// ensureVecBuffer makes a `buf[i] = x` store safe for the %vec at arrSlot: it
// guarantees the backing buffer has room for element idxV, so the store can
// never write through a null pointer NOR past the end of the buffer. The MIR
// %vec layout is {i64 len, i64 cap, i64 data} where data is stored as an i64
// (inttoptr'd at use).
//
// TWO CASES, and the second one is why this function is not merely an allocator:
//
//  1. data == 0 (a slice declared with no capacity). Mirrors the legacy backend,
//     which materializes a real buffer (default cap 1024) at declaration time.
//     We allocate lazily, on the first write, so a slice that is only ever grown
//     via push is not over-allocated. Guarded by the data==0 test, so it happens
//     at most once per slot (no leak).
//
//  2. data != 0 AND cap > 0 AND idxV >= cap. This is a REAL GROW: allocate, copy
//     the live elements, free the old buffer. It is not an optimisation, it is
//     the fix for a heap overflow. The `cap > 0` conjunct is what excludes
//     borrowed views (see the check block below); without it the grow frees
//     memory this code does not own.
//
// WHY CASE 2 EXISTS. The capacity a %vec ends up with is decided by whoever grew
// it last, and the two growers disagree:
//
//   - the builtin push (`emitBuiltinVecPush`) grows to max(1, cap*2), so after
//     the first push cap is 1 and len == cap;
//   - std `[]t.insert` / `[]t.remove` are ordinary std METHOD BODIES (they do NOT
//     route to the vec-insert/vec-remove builtins — the std definition shadows
//     the registry entry), and they write through `.[i] = v` at index == len
//     WITHOUT growing capacity. Their own comment says so: "調用方需確保容量足夠"
//     (the caller must ensure capacity is sufficient).
//
// So `v.push(a); v.insert(0, b)` stores at index 1 into a buffer sized for 1
// element. That is a genuine out-of-bounds write; it only *looks* harmless for
// small elements because malloc's size-class slack absorbs the overrun. With a
// 24-byte struct element it corrupts the heap: verified by
// tests/mem-safety/nested-container-clone.no-adjacent probe `[]person` +
// push + insert, which segfaults before the fix and prints correctly after it.
// The same shape is reachable without push, because `.[i] = v` on a slice whose
// cap was reached extends len (emitExtendLen) without touching cap.
//
// A borrowed view (string->[]byte coercion: cap==0 with a non-null data pointing
// at the source string's storage) is deliberately left ALONE, exactly as before —
// case 2 does not apply to it. Writing through such a view is a separate,
// pre-existing question; silently re-homing it here would free a buffer the
// string still owns.
func (c *codegen) ensureVecBuffer(arrSlot string, v ValueID, idxV, elemT string) {
	const vecDefaultCap = int64(1024)
	// Minimum capacity to allocate when growing. Matches the documented vec growth
	// strategy in src/std/vec.no ("cap == 0 → new-cap = 4") and keeps insert's
	// one-spare-slot requirement satisfiable without a second grow per element.
	const vecMinCap = int64(4)
	elemSz := c.typeSizeOperand(elemT)

	// Fresh-allocation size (case 1): the legacy default capacity.
	c.loadSeq++
	sizeReg := fmt.Sprintf("%%vbsz%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", sizeReg, elemSz, vecDefaultCap))

	// The store needs room for element idxV, i.e. idxV+1 elements.
	c.loadSeq++
	needReg := fmt.Sprintf("%%vbneed%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", needReg, idxV))

	// Load data (field 2) and cap (field 1) once, in the entry block, so both
	// dominate every branch below.
	c.loadSeq++
	dg := fmt.Sprintf("%%vbdg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", dg, arrSlot))
	c.loadSeq++
	di := fmt.Sprintf("%%vbdi%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", di, dg))
	c.loadSeq++
	cg := fmt.Sprintf("%%vbcg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", cg, arrSlot))
	c.loadSeq++
	ci := fmt.Sprintf("%%vbci%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", ci, cg))

	c.loadSeq++
	isnull := fmt.Sprintf("%%vbnull%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", isnull, di))

	c.loadSeq++
	lAlloc := fmt.Sprintf("vbA%d", c.loadSeq)
	lCheck := fmt.Sprintf("vbC%d", c.loadSeq)
	lGrow := fmt.Sprintf("vbG%d", c.loadSeq)
	lDone := fmt.Sprintf("vbD%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isnull, lAlloc, lCheck))

	// alloc block (case 1): malloc the default buffer, store data + cap.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lAlloc))
	c.loadSeq++
	buf := fmt.Sprintf("%%vbbuf%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, sizeReg))
	// Zero it for the same reason as emitBuiltinAlloc: the elements are owned
	// (e.g. []str), so the first `a[i] = v` drops the previous element and a
	// garbage %str-long there would free() an arbitrary pointer.
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", buf, sizeReg))
	c.loadSeq++
	ptri := fmt.Sprintf("%%vbptri%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", ptri, buf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ptri, dg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %d, i64* %s\n", vecDefaultCap, cg))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lDone))

	// check block (case 2): a buffer exists — is there room for element idxV?
	//
	// `cap > 0` is a LOAD-BEARING guard, not a sanity check. A borrowed view
	// (string -> []byte coercion) is exactly `cap == 0` with a NON-NULL data
	// pointer that aliases someone else's storage — the string's buffer. Such a
	// view must be left alone, as it always was. Growing it would malloc a fresh
	// buffer, memcpy min(len, 0) == 0 bytes, and then `free` a pointer this code
	// does not own: the source string's buffer. That is a use-after-free, and it
	// is not hypothetical — tests/test-tls-partial.no (`buf str` passed to
	// `tls.put-u16(buf, ...)`) traps with SIGTRAP on the very first write when
	// this guard is missing. A real vec with a non-null data pointer always has
	// cap >= 1 (push grows 1,2,4…; the fresh path allocates 1024), so cap > 0
	// separates the two cases exactly.
	//
	// Signed compare on purpose: a negative idxV must not wrap to a huge u64 and
	// request a petabyte from malloc.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lCheck))
	c.loadSeq++
	posCap := fmt.Sprintf("%%vbpc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, 0\n", posCap, ci))
	c.loadSeq++
	full0 := fmt.Sprintf("%%vbfull%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", full0, needReg, ci))
	c.loadSeq++
	full := fmt.Sprintf("%%vbfull%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", full, posCap, full0))
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", full, lGrow, lDone))

	// grow block (case 2): newCap = max(cap*2, need, 4); copy the live elements,
	// then free the old buffer. Freeing is safe: the elements are MOVED (a
	// shallow copy of the descriptors), not duplicated — the new array takes over
	// ownership of the same element payloads, and the vec's own drop later frees
	// only the current (new) data pointer.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lGrow))
	c.loadSeq++
	dbl := fmt.Sprintf("%%vbdbl%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, 2\n", dbl, ci))
	c.loadSeq++
	over := fmt.Sprintf("%%vbov%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", over, dbl, needReg))
	c.loadSeq++
	n1 := fmt.Sprintf("%%vbn1%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", n1, over, dbl, needReg))
	c.loadSeq++
	small := fmt.Sprintf("%%vbsm%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %d\n", small, n1, vecMinCap))
	c.loadSeq++
	newCap := fmt.Sprintf("%%vbnc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %d, i64 %s\n", newCap, small, vecMinCap, n1))
	c.loadSeq++
	gsz := fmt.Sprintf("%%vbgsz%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", gsz, newCap, elemSz))
	c.loadSeq++
	gbuf := fmt.Sprintf("%%vbgbuf%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", gbuf, gsz))
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", gbuf, gsz))
	// Copy min(len, cap) elements — never more than the old buffer holds.
	c.loadSeq++
	lg := fmt.Sprintf("%%vblg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", lg, arrSlot))
	c.loadSeq++
	li := fmt.Sprintf("%%vbli%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", li, lg))
	c.loadSeq++
	lenOver := fmt.Sprintf("%%vblo%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", lenOver, li, ci))
	c.loadSeq++
	copyN := fmt.Sprintf("%%vbcn%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", copyN, lenOver, ci, li))
	c.loadSeq++
	cbytes := fmt.Sprintf("%%vbcb%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", cbytes, copyN, elemSz))
	c.loadSeq++
	oldp := fmt.Sprintf("%%vbop%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", oldp, di))
	c.decl("declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", gbuf, oldp, cbytes))
	c.decl("declare void @free(i8*)")
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", oldp))
	c.loadSeq++
	ptri2 := fmt.Sprintf("%%vbpti%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", ptri2, gbuf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ptri2, dg))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", newCap, cg))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lDone))

	// done block: merge point; the caller's subsequent store lands here.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lDone))
	_ = v
}

// elemTypeOfReceiver returns the LLVM *element* type for indexing/store into a
// slice/array/str receiver. The byte-addressed backing store of a %vec (slice)
// or %str-long means the element type is the slice's declared element — i8 for
// []byte, i64 for []i64 — and NEVER the value being stored or the destination
// variable's type. Writing d0:i64 into a []byte must store ONE byte (truncating
// to i8) at offset i; using the value's i64 type would instead store 8 bytes at
// offset i*8 and overrun the array (the AES stack-corruption crash).
// arrayElemRaw extracts the element raw type from a nolang fixed-array raw type
// string like "[128]i64" -> "i64" or "[512]byte" -> "byte". It returns ok=false
// for non-array raws so callers can fall through to a default.
func arrayElemRaw(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "[") {
		return "", false
	}
	closeB := strings.IndexByte(raw, ']')
	if closeB < 0 {
		return "", false
	}
	elem := strings.TrimSpace(raw[closeB+1:])
	if elem == "" {
		return "", false
	}
	return elem, true
}

func (c *codegen) elemTypeOfReceiver(recv ValueID) string {
	if val := c.mod.Value(recv); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			return c.elemLTOfType(t)
		}
	}
	return "i8"
}

// elemLTOfType is the type-driven half of elemTypeOfReceiver, split out so an
// option can recurse into its own element (see the KindOption case).
func (c *codegen) elemLTOfType(t *Type) string {
	switch t.Kind {
	case KindOption:
		// `?[]T` / `?str` — an option wrapping a container. The element width
		// is that of the INNER container, not of the option header: falling
		// through to the "i8" default made `v[i]` on a `?[]i64` load a single
		// BYTE of the backing store, so `v[0]` happened to read 10 (the low
		// byte of element 0) while `v[1]` read 0 (byte 1 of element 0) instead
		// of 20. Indexing goes through elemAddr's option peel, so only the
		// element width was missing.
		if t.Elem != NoType {
			if et := c.mod.Type(t.Elem); et != nil {
				return c.elemLTOfType(et)
			}
		}
	case KindSlice, KindArray:
		if t.Elem != NoType {
			if et := c.mod.Type(t.Elem); et != nil {
				return c.llvmTypeOf(et)
			}
		}
		// Fallback: derive the element type from the raw string when Elem
		// is missing (some array types in the table lack it). Mirrors
		// elementTypeOf's defensive path so the backing-store element width
		// is correct (i64 for [128]i64, i8 for [512]byte) instead of the
		// default i8, which would overrun non-byte arrays.
		if e, ok := arrayElemRaw(t.Raw); ok {
			if et := c.mod.Type(c.mod.internType(e)); et != nil {
				return c.llvmTypeOf(et)
			}
		}
		return "i8"
	case KindStr:
		return "i8"
	}
	return "i8"
}

func (c *codegen) emitIndex(inst *Inst) error {
	arrT, _ := c.ptype(inst.Args[0])
	elemT := c.elemTypeOfReceiver(inst.Args[0])
	dstT, _ := c.ptype(inst.Dst)
	arrSlot := c.valSlot[inst.Args[0]]
	if arrSlot == "" {
		return fmt.Errorf("index slot: value id %d (type %s) has no slot in func %d; elemT=%s", inst.Args[0], arrT, c.cf, elemT)
	}
	_, idxV := c.loadVal(inst.Args[1])
	idxVT, _ := c.ptype(inst.Args[1])
	idxV = c.coerceIndex(inst.Args[1], idxVT, idxV)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		dt, _ := c.ptype(inst.Dst)
		c.fail("index result has no slot in func %s (dst=%d type=%s) arrT=%s elemT=%s", c.fname[c.cf], inst.Dst, dt, arrT, elemT)
		return fmt.Errorf("index dst slot (dst=%d type=%s)", inst.Dst, dt)
	}
	ep := c.elemAddr(inst.Args[0], arrSlot, idxV, arrT, elemT)
	c.loadSeq++
	lv := fmt.Sprintf("%%lx%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, elemT, elemT, ep))
	// Owned element types (%str-long) must be DEEP CLONED on read, not just
	// shallow-copied: `s = arr[i]` loads the {len,cap,data} triple, which
	// SHARES the heap buffer with arr[i]. When `s` goes out of scope it is
	// dropped (@str_free frees the data pointer), leaving arr[i] with a
	// dangling pointer. The next read of arr[i] passes garbage to @str_cmp /
	// @str_eq, producing wrong results or crashes. This mirrors the write
	// side (emitIndexStore) which already calls @str_clone on assignment.
	//
	// test-diff-debug.no was the canonical victim: `s = lines2[i]` inside an
	// eprint('{s}') loop shared the buffer, the drop freed it, and the
	// subsequent diff-engine-lcs compared against dangling strings → every
	// `compare == 0` was false → the DP table stayed all zeros → segfault
	// on backtracking.
	if elemT == "%str-long" && dstT == "%str-long" {
		c.loadSeq++
		cl := fmt.Sprintf("%%ixc%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, elemT, elemT, lv))
		lv = cl
	}
	// Coerce the loaded element to the destination variable's type (e.g. i8 ->
	// i64 for `x = slice[i]` where x is i64).
	if cv := c.coerce(elemT, lv, dstT); cv != "" {
		lv = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, lv, dstT, dstSlot))
	return nil
}

// emitIndexStore lowers `a[i] = v`: compute the element address via GEP and store
// v there. For owned element types the previous element is dropped first (memory
// safety: the overwritten buffer must be freed exactly once, and the new value is
// moved in — not cloned — so it is exempt from the array's own (non-existent)
// drop).
func (c *codegen) emitIndexStore(inst *Inst) error {
	arrT, _ := c.ptype(inst.Args[0])
	// The element type is the receiver's declared element (i8 for []byte), NOT the
	// value's type — writing a wider value into a byte slice must truncate to one
	// byte (see elemTypeOfReceiver). Using the value's type here stores 8 bytes
	// per index and overruns the array.
	elemT := c.elemTypeOfReceiver(inst.Args[0])
	valT, valV := c.loadVal(inst.Args[2])
	// Ownership of the OVERWRITTEN slot is determined by the ELEMENT type we are
	// writing INTO, not the value's type. A char/byte buffer (i8) is not owned,
	// so storing into it must NOT emit @str_free on an i8 as if it were a
	// %str-long — that previously both freed an i8 and fed a %str-long value
	// straight into a `store i8` (opt rejected it: "defined with type
	// '%str-long' but expected 'i8'"). Only owned element types (string/vec/
	// option sub-elements) get a drop of the previous content.
	elemOwned := elemT == "%str-long" || elemT == "%vec" || elemT == "%option"
	arrSlot := c.valSlot[inst.Args[0]]
	if arrSlot == "" {
		c.fail("index-store of value with no slot in func %d", c.cf)
		return fmt.Errorf("indexstore slot")
	}
	// Lvalue projection: `a[i] = v` where `a` is itself a field read
	// (`self.pool.nodes[i] = x`) or an element read — the alloca holds a COPY of
	// the container, so storing through it would be lost. Write through the
	// real container address instead.
	if p, ptid, projected := c.lvalueAddrOf(inst.Args[0]); projected && p != "" && os.Getenv("NOLANG_LV_INDEX") == "" {
		if pt := c.mod.Type(ptid); pt != nil {
			arrSlot = p
			arrT = c.llvmTypeOf(pt)
		}
	}
	_, idxV := c.loadVal(inst.Args[1])
	idxVT, _ := c.ptype(inst.Args[1])
	idxV = c.coerceIndex(inst.Args[1], idxVT, idxV)
	// An empty slice (declared with no capacity) is zero-initialized to
	// {len=0,cap=0,data=0}; a direct `buf[i] = x` would store through a null data
	// pointer and crash. Legacy allocates a real backing buffer for an empty []byte,
	// so we do the same lazily here: if the buffer is null, malloc a default one
	// (only when actually written, to avoid wasting memory / leaking on slices that
	// are only ever grown via push).
	//
	// CRITICAL ORDERING: this MUST run before elemAddr (below), because elemAddr
	// captures the current (possibly null) data pointer into the element address.
	// Allocating the buffer afterwards would leave the captured address pointing at
	// the old null location, and the store would still segfault.
	if arrT == "%vec" {
		c.ensureVecBuffer(arrSlot, inst.Args[0], idxV, elemT)
	}
	ep := c.elemAddr(inst.Args[0], arrSlot, idxV, arrT, elemT)
	if elemOwned {
		c.loadSeq++
		old := fmt.Sprintf("%%old%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", old, elemT, elemT, ep))
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", elemT, old))
	}
	// Truncate/extend the stored value to the element type (e.g. i64 -> i8 for a
	// []byte, which keeps the store to a single byte at offset i).
	if cv := c.coerce(valT, valV, elemT); cv != "" {
		valV = cv
	} else if strings.HasPrefix(valT, "%option") && !strings.HasPrefix(elemT, "%option") && elemT != "%str-long" && elemT != "%vec" {
		// Overflow-default integer arithmetic yields ?i64 (an %option). Assigning
		// it to a plain scalar element (`a[i] = a[j]+a[k]` with a:[N]i64) unwraps
		// the ok payload, matching the legacy codegen (which stores the scalar
		// payload, not the whole {tag,payload} struct). Without this, opt rejects
		// the store as "defined with type '%option' but expected 'i64'" and the
		// build fails under NOLANG_MIR=3 (regression of test-arr.no).
		// optionPayloadOf, not `extractvalue %option %v, 1`: field 1 is the whole
		// 24-byte slot, so a bare extractvalue yields an array where an i64 is
		// wanted (and misses the box deref for an oversized payload).
		plt, pl := c.optionPayloadOf(inst.Args[2], valV, valT)
		if pl == "" {
			pl, plt = valV, valT
		}
		if cv2 := c.coerce(plt, pl, elemT); cv2 != "" {
			valV = cv2
		} else {
			valV = pl
		}
	} else if valT == "%str-long" && elemT == "i8" {
		// Storing a (single-char) string value into a char slot: take its first
		// byte. `s[i] = c` where c is a one-char string stores byte 0 of c's
		// data; loading the data pointer and reading byte 0 yields the char.
		dp := c.dataPtrOf(inst.Args[2])
		if dp != "" {
			c.loadSeq++
			by := fmt.Sprintf("%%sby%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", by, dp))
			valV = by
		}
	}
	// Owned element assignment must DEEP CLONE the incoming value: `s[0] = a`
	// stores a by-value copy of the {len,cap,data} triple that still SHARES a's
	// heap buffer. A later `a = 'changed'` drops a's old buffer, leaving s[0]
	// dangling, and s[0]'s own drop then double-frees it. Legacy deep-clones on
	// the write side to mirror the already-cloned read side (`x = s[0]`), which
	// is exactly what tests/mem-safety/element-assign-clone.no asserts.
	// %vec / heap-option elements still share: there is no vec clone helper.
	if elemT == "%str-long" && valT == "%str-long" {
		c.loadSeq++
		cl := fmt.Sprintf("%%icl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, elemT, elemT, valV))
		valV = cl
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemT, valV, elemT, ep))
	// Same by-value sharing as vec.push — see cloneStructElemLeaves. Only
	// reached for struct element types (str/vec elements have their own
	// clone path above, scalars own nothing).
	c.cloneStructElemLeaves(ep, elemT)
	if arrT == "%str-long" || arrT == "%vec" {
		// Writing an element may extend the logical length. Two cases matter and
		// both match the legacy codegen:
		//   - a freshly with-cap'd str has len=0 but a cap-sized buffer;
		//     without this `val[i] = c` in a loop (str.to-upper) leaves len=0
		//     and prints nothing;
		//   - a slice declared with no capacity (`padded []byte`) is
		//     {len=0,cap=0,data=0}; legacy grows it on the first OOB write
		//     (len = max(len, idx+1)) so `padded[i] = 0` inside a loop
		//     materializes it. MIR used to store the bytes (ensureVecBuffer
		//     mallocs a buffer) but leave len=0, so every later `.len()` /
		//     iteration over the slice saw an EMPTY container — the root cause
		//     of a wrong (but exit-0) SHA-1 digest, since std/crypto/sha1
		//     builds its padding buffer exactly this way.
		c.emitExtendLen(arrT, arrSlot, idxV)
	}
	return nil
}

// emitExtendLen stores len = max(len, idx+1) into field 0 of a %str-long /
// %vec slot, so a write past the current logical length becomes visible.
func (c *codegen) emitExtendLen(arrT, arrSlot, idxV string) {
	c.loadSeq++
	lenGep := fmt.Sprintf("%%slg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", lenGep, arrT, arrT, arrSlot))
	c.loadSeq++
	curLen := fmt.Sprintf("%%slc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", curLen, lenGep))
	c.loadSeq++
	idx1 := fmt.Sprintf("%%sli%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", idx1, idxV))
	c.loadSeq++
	cmp := fmt.Sprintf("%%slm%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", cmp, idx1, curLen))
	c.loadSeq++
	newLen := fmt.Sprintf("%%sln%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", newLen, cmp, idx1, curLen))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", newLen, lenGep))
}

// localTypeOf resolves the MIR type of a value, preferring the enclosing
// function's LocalTypes table (authoritative after lowering — e.g. a `?T`
// option wrap is recorded there) over the value's own declared type.
func (c *codegen) localTypeOf(v ValueID) TypeID {
	if f := c.mod.Func(c.cf); f != nil {
		if tid, ok := f.LocalTypes[v]; ok {
			return tid
		}
	}
	if val := c.mod.Value(v); val != nil {
		return val.Type
	}
	return NoType
}

// lvalueAddrOf computes the LLVM address of the storage a MIR value was READ
// OUT OF, for values that are *projections* of an addressable base — i.e. the
// value is defined by a chain of OpGetField (struct field read) and/or
// OpIndex (container element read) instructions rooted at an alloca or a
// by-reference parameter.
//
// Why this exists: `self.pool.nodes[i].kind = x` lowers to a chain of reads
// that each copy their operand into a fresh alloca (for `.nodes` that is a
// whole [64 x json_value] array). OpSetField then writes into that COPY and
// the mutation is silently lost — the reason every mutating std method
// (json.set / json-pool.alloc / vec.insert / …) was a no-op under the MIR
// backend. Emitting the GEP chain instead makes the write land in the real
// storage, and it also removes the redundant copies.
//
// Returns (ptr, tid, true) when an address was computed — ptr may then be used
// wherever the value's slot would have been used — or (slot, tid, false) when
// the value is not a projection, in which case callers keep their existing
// slot-based handling unchanged.
func (c *codegen) lvalueAddrOf(v ValueID) (string, TypeID, bool) {
	slot := c.valSlot[v]
	tid := c.localTypeOf(v)
	if slot == "" || tid == NoType {
		return slot, tid, false
	}
	if os.Getenv("NOLANG_MIR_NO_LVALUE") != "" {
		return slot, tid, false
	}
	defIID, ok := c.defInst[v]
	if !ok {
		return slot, tid, false
	}
	di := c.mod.Inst(defIID)
	if di == nil {
		return slot, tid, false
	}
	switch di.Op {
	case OpGetField:
		if len(di.Args) < 1 {
			return slot, tid, false
		}
		bptr, btid, _ := c.lvalueAddrOf(di.Args[0])
		if bptr == "" || btid == NoType {
			return slot, tid, false
		}
		bt := c.mod.Type(btid)
		if bt == nil {
			return slot, tid, false
		}
		basePtr := bptr
		baseLT := c.llvmTypeOf(bt)
		baseRaw := bt.Raw
		// `?T.field`: the payload lives in field 1 of the option.
		if isOptionType(baseLT) {
			elem, ok := parseOptionElem(baseRaw)
			if !ok {
				return slot, tid, false
			}
			_, payloadLT := c.optionType(elem)
			// optPayloadTypedAddr, not a raw GEP to field 1: field 1 is the 24-byte
			// slot, and a payload larger than that is BOXED, so the struct lives
			// behind the pointer in slot[0], not in the slot itself.
			basePtr = c.optPayloadTypedAddr(basePtr, payloadLT, payloadLT)
			baseLT = payloadLT
			baseRaw = elem
		}
		structKey := c.structKeyOf(baseRaw)
		if structKey == "" {
			structKey = baseRaw
		}
		idx, ok := c.mod.FieldIndex(structKey, di.Str)
		if !ok {
			return slot, tid, false
		}
		g := c.treg("lvg")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", g, baseLT, baseLT, basePtr, idx))
		// POINTER FIELD (Phase 1 layout flip): the field slot holds a `%T*`, so
		// the GEP above yields the address OF THE POINTER, not of the value.
		// Load the pointer to reach the storage the projection actually
		// addresses. Skipping this makes `h.p.x = 42` overwrite the pointer
		// bits themselves (the store below writes an i64 into the first 8 bytes
		// of the `%T*` slot) and the next `h.p.x` read segfaults — see
		// tmp/field-mut.no. Opaque pointers give no safety net: `%T*` and `%T**`
		// are both `ptr`, so the bad IR VERIFIES.
		if f, ok := c.fieldAt(structKey, idx); ok && c.fieldIsPointer(f) {
			if pointeeLT := c.llvmTypeOf(c.mod.Type(tid)); pointeeLT != "" {
				// LAZY allocation, not a plain load: a freshly declared struct is
				// zero-initialised, so the pointer starts NULL and `e1.addr.street
				// = 'x'` would write through it. @__nolang_get_<T> allocates the
				// pointee on first touch.
				return c.ptrFieldAddr(pointeeLT, g), tid, true
			}
		}
		return g, tid, true
	case OpIndex:
		if len(di.Args) < 2 {
			return slot, tid, false
		}
		bptr, btid, _ := c.lvalueAddrOf(di.Args[0])
		if bptr == "" || btid == NoType {
			return slot, tid, false
		}
		bt := c.mod.Type(btid)
		if bt == nil {
			return slot, tid, false
		}
		elemT := c.localTypeOf(v)
		if elemT == NoType || c.mod.Type(elemT) == nil {
			return slot, tid, false
		}
		elemLT := c.llvmTypeOf(c.mod.Type(elemT))
		// A projection is only sound when the container's ELEMENT type is the
		// same type as the value that was read out of it. `z = ba[0]` on a
		// `[N]byte` / `[]byte` / `str` widens i8 -> i64: emitIndex zero-extends
		// the byte into the value's own i64 slot, so the value is a COPY and not
		// an alias of the element. Projecting it back to `&ba[0]` hands callers
		// an `i8*` typed as `i64*` — `z.to-str()` (which takes `i64*`) then
		// loads 8 bytes from a 1-byte element and prints garbage. Same class of
		// type confusion for any other widening read.
		if recElemLT := c.elemTypeOfReceiver(di.Args[0]); recElemLT != "" && recElemLT != elemLT {
			return slot, tid, false
		}
		_, idxV := c.loadVal(di.Args[1])
		idxVT, _ := c.ptype(di.Args[1])
		idxV = c.coerceIndex(di.Args[1], idxVT, idxV)
		// The CONTAINER (di.Args[0]) is the value elemAddr must resolve the
		// option payload from, not `v` — `v` is the element that was read out
		// (i64 here), so passing it made elemAddr treat the option's payload as
		// an i64 and emit `getelementptr i64, ptr %payload, i64 0, i64 %i`,
		// which opt rejects ("invalid getelementptr indices"). This only
		// surfaced on `v[i].method()`, where the element is projected back to
		// its address to serve as the callee's `self` out-param.
		ep := c.elemAddr(di.Args[0], bptr, idxV, c.llvmTypeOf(bt), elemLT)
		return ep, tid, true
	}
	return slot, tid, false
}

// emitGetField lowers `recv.field`: compute the field address via GEP into the
// receiver's struct slot, load the field, and store it in the result slot. The
// field index comes from the module's StructFields layout (keyed by the
// receiver's struct raw name); the field name lives in inst.Str.
func (c *codegen) emitGetField(inst *Inst) error {
	recvSlot := c.valSlot[inst.Args[0]]
	if recvSlot == "" {
		c.fail("getfield receiver has no slot in func %d", c.cf)
		return fmt.Errorf("getfield slot")
	}
	recvRaw := ""
	recvLT := ""
	// Prefer the function-local type (f.LocalTypes) for the receiver: it is the
	// AUTHORITATIVE type after lowering (e.g. a `?T` option wrap is recorded
	// there), whereas the bare Value.Type field can hold the INNER element type
	// (e.g. `conn` instead of `?conn`) for a call result whose payload was
	// re-wrapped. Using Value.Type here mis-classified `?conn.path` as a plain
	// `conn` getfield and emitted a GEP that skipped the option peel, producing
	// `getelementptr inbounds %net_conn, ...` against an %option_conn slot
	// (invalid getelementptr indices, opt-verify) — tests/test-opt-struct-field.no.
	if f := c.mod.Func(c.cf); f != nil {
		if tid, ok := f.LocalTypes[inst.Args[0]]; ok {
			if t := c.mod.Type(tid); t != nil {
				recvRaw = t.Raw
				recvLT = c.llvmTypeOf(t)
			}
		}
	}
	if recvLT == "" {
		if val := c.mod.Value(inst.Args[0]); val != nil {
			if t := c.mod.Type(val.Type); t != nil {
				recvRaw = t.Raw
				recvLT = c.llvmTypeOf(t)
			}
		}
	}
	if recvLT == "" {
		recvLT = "%" + sanitize(recvRaw)
	}
	// VIEW auto-deref: a `&T` view is `T*` at the LLVM level, so a field read
	// through a view must load the pointer first and then GEP into the pointee.
	// Without this the GEP indexes into the POINTER slot and opt rejects it.
	if strings.HasPrefix(recvRaw, "&") && strings.HasSuffix(recvLT, "*") {
		// recvLT is `%T*`; the slot is `%T**`, so load the POINTER (not the
		// pointee) and GEP through it.
		c.loadSeq++
		pv := fmt.Sprintf("%%vld%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", pv, recvLT, recvLT, recvSlot))
		recvLT = strings.TrimSuffix(recvLT, "*")
		recvRaw = strings.TrimPrefix(recvRaw, "&")
		recvSlot = pv
	}
	if isOptionType(recvLT) {
		// `?T.field`: peel the option (field 1 holds the inline payload of type
		// payloadLT), then GEP into the inner struct's field.
		elem, _ := parseOptionElem(recvRaw)
		_, payloadLT := c.optionType(elem)
		innerRaw := elem
		// `?&T` — an optional VIEW: the option's payload IS the view pointer
		// (`%T*`), so the field GEP must peel that pointer first.
		viewElem := strings.HasPrefix(innerRaw, "&")
		if viewElem {
			innerRaw = strings.TrimPrefix(innerRaw, "&")
		}
		structKey := c.structKeyOf(innerRaw)
		if structKey == "" {
			structKey = innerRaw
		}
		idx, ok := c.mod.FieldIndex(structKey, inst.Str)
		if !ok {
			c.fail("getfield: field %q not found on %q (option %q) in func %d", inst.Str, innerRaw, recvRaw, c.cf)
			return fmt.Errorf("getfield field")
		}
		fieldLT, owned := c.ptype(inst.Dst)
		if fieldLT == "" {
			fieldLT = "i64"
		}
		// optPayloadTypedAddr, not `getelementptr %option ... , 1`: a payload
		// larger than the 24-byte slot is BOXED, and field 1 then holds the box
		// POINTER rather than the struct (tests/test-opt-struct-field.no).
		pg := c.optPayloadTypedAddr(recvSlot, payloadLT, payloadLT)
		payloadBase := payloadLT
		if viewElem && strings.HasSuffix(payloadLT, "*") {
			// Load the view pointer out of the payload field (`%T**` -> `%T*`).
			payloadBase = strings.TrimSuffix(payloadLT, "*")
			c.loadSeq++
			pv := fmt.Sprintf("%%vld%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", pv, payloadBase, payloadLT, pg))
			pg = pv
		}
		c.loadSeq++
		gp := fmt.Sprintf("%%gp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gp, payloadBase, payloadBase, pg, idx))
		// #83 SROA guard: for large aggregate field types, use memcpy.
		if c.shouldUseMemcpy(fieldLT) {
			sz := c.typeSizeOperand(fieldLT)
			c.emitMemcpy(c.valSlot[inst.Dst], gp, sz)
			return nil
		}
		c.loadSeq++
		lv := fmt.Sprintf("%%lx%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, fieldLT, fieldLT, gp))
		// Owned string fields are read by value (sharing the heap buffer with the
		// field). Clone so the read temp owns its own buffer and the field's later
		// drop can't double-free it.
		if owned && fieldLT == "%str-long" {
			c.loadSeq++
			cl := fmt.Sprintf("%%lc%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, fieldLT, fieldLT, lv))
			lv = cl
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, lv, fieldLT, c.valSlot[inst.Dst]))
		return nil
	}
	structKey := c.structKeyOf(recvRaw)
	if structKey == "" {
		structKey = recvRaw
	}
	idx, ok := c.mod.FieldIndex(structKey, inst.Str)
	if !ok {
		c.fail("getfield: field %q not found on %q in func %d", inst.Str, recvRaw, c.cf)
		return fmt.Errorf("getfield field")
	}
	structLT := recvLT
	fieldLT, owned := c.ptype(inst.Dst)
	if fieldLT == "" {
		fieldLT = "i64"
	}
	c.loadSeq++
	gep := fmt.Sprintf("%%gp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, structLT, structLT, recvSlot, idx))
	// POINTER FIELD (Phase 1 layout flip): the slot holds a `%T*`, not the `%T`
	// itself. Load the pointer, then read the pointee THROUGH it, so the value
	// this op yields is still the plain `%T` that every downstream consumer
	// already expects. Only fields whose tag says so are affected; `#{inline}`
	// fields and owned leaves (str/vec/[]T/map) keep the old by-value read.
	//
	// The pointee is fetched through @__nolang_get_<T> rather than a bare load
	// so that reading an UNASSIGNED field yields zeros instead of dereferencing
	// NULL — matching the pre-flip inline layout, where the field was simply
	// part of a zero-initialised struct.
	//
	// There is NO LLVM-level safety net for this. With opaque pointers `%T*` and
	// `%T**` are both `ptr`, so getting it wrong yields IR that VERIFIES but
	// reads the wrong bytes -- see the `store %pt %lv, %pt* %gp` bug that
	// produced `7 -7` instead of `7 8` in tests/field-tag.no.
	if f, ok := c.fieldAt(structKey, idx); ok && c.fieldIsPointer(f) {
		gep = c.ptrFieldAddr(fieldLT, gep)
	}
	// #83 SROA guard: for large aggregate field types, use memcpy from the
	// GEP'd field address to the destination slot instead of load+store.
	if c.shouldUseMemcpy(fieldLT) {
		sz := c.typeSizeOperand(fieldLT)
		c.emitMemcpy(c.valSlot[inst.Dst], gep, sz)
		return nil
	}
	c.loadSeq++
	lv := fmt.Sprintf("%%lx%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, fieldLT, fieldLT, gep))
	// Owned string fields are read by value (the triple is copied, sharing the
	// heap buffer with the field). Clone so the read temp owns its own buffer
	// and the field's later drop can't double-free it.
	if owned && fieldLT == "%str-long" {
		c.loadSeq++
		cl := fmt.Sprintf("%%lc%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, fieldLT, fieldLT, lv))
		lv = cl
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, lv, fieldLT, c.valSlot[inst.Dst]))
	return nil
}

// containerFieldIndex maps a container pseudo-field name (len/cap/data) to its
// GEP index within the slice/str aggregate. These are layout-derived, not
// StructFields entries, so emitSetField handles them directly (mirroring
// emitLenCap's read path). The aggregate layout is { len:i64, cap:i64, data:ptr }.
func containerFieldIndex(name string) (int, bool) {
	switch name {
	case "len":
		return 0, true
	case "cap":
		return 1, true
	case "data":
		return 2, true
	}
	return 0, false
}

// ensureContainerStorageForLen backs a `X.len = n` write on a slice/str whose
// backing buffer is still NULL by allocating n elements' worth of storage and
// publishing it through the container's data/cap fields, so the length that is
// about to be stored is actually addressable.
//
// Deliberately NARROW: it only fires when data == NULL. When the container
// already owns a buffer the emitted code is byte-for-byte what it was before,
// which keeps the documented std contract intact — `src/std/vec.no` grows with
// an inline `.len = cur + 1` and its comment says "調用方需確保容量足夠", i.e.
// std relies on `.len = n` being a plain field write inside an already
// allocated buffer. Growing (or realloc-ing) there too would change ownership
// of every std slice/string write at once; this only removes the null-buffer
// crash, which no correct program can depend on.
//
// Layouts: %str-long = { i64 len, i64 cap, i8* data } (a real pointer);
// %vec = { i64 len, i64 cap, i64 data } (ptrtoint-encoded).
func (c *codegen) ensureContainerStorageForLen(slot, recvLT string, ty *Type, nV string) {
	// Element size: a str's backing store is bytes; a slice's is its element.
	elemSz := "1"
	if ty.Kind == KindSlice && ty.Elem != NoType {
		if et := c.mod.Type(ty.Elem); et != nil {
			elemSz = c.typeSizeOperand(c.llvmTypeOf(et))
		}
	}
	dataLT := "i64"
	if ty.Kind == KindStr {
		dataLT = "i8*"
	}
	// Field 2 is data, field 1 is cap.
	c.loadSeq++
	dg := fmt.Sprintf("%%lsg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 2\n", dg, recvLT, recvLT, slot))
	c.loadSeq++
	dv := fmt.Sprintf("%%lsv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", dv, dataLT, dataLT, dg))
	c.loadSeq++
	cg := fmt.Sprintf("%%lcg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", cg, recvLT, recvLT, slot))

	// is-null test: %str-long holds a pointer, %vec an integer.
	c.loadSeq++
	isnull := fmt.Sprintf("%%lsn%d", c.loadSeq)
	if dataLT == "i8*" {
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i8* %s, null\n", isnull, dv))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", isnull, dv))
	}
	c.loadSeq++
	lAlloc := fmt.Sprintf("lsA%d", c.loadSeq)
	lDone := fmt.Sprintf("lsD%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isnull, lAlloc, lDone))

	c.sb.WriteString(fmt.Sprintf("%s:\n", lAlloc))
	c.loadSeq++
	sizeReg := fmt.Sprintf("%%lssz%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", sizeReg, nV, elemSz))
	c.loadSeq++
	buf := fmt.Sprintf("%%lsbuf%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, sizeReg))
	// Zero it: for owned element types (e.g. []str) the first `a[i] = v` drops
	// the previous element, and garbage there would free() a wild pointer.
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", buf, sizeReg))
	if dataLT == "i8*" {
		c.sb.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", buf, dg))
	} else {
		c.loadSeq++
		ptri := fmt.Sprintf("%%lspi%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", ptri, buf))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ptri, dg))
	}
	// cap = n: the buffer is exactly big enough for the length being written,
	// and cap > 0 marks the container as owning its buffer (cap == 0 means
	// "borrowed view" and must never be freed).
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", nV, cg))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lDone))

	c.sb.WriteString(fmt.Sprintf("%s:\n", lDone))
}

// emitSetField lowers `recv.field = v`: compute the field address via GEP into
// the receiver's struct slot and store v there.
func (c *codegen) emitSetField(inst *Inst) error {
	recvSlot := c.valSlot[inst.Args[0]]
	if recvSlot == "" {
		c.fail("setfield receiver has no slot in func %d", c.cf)
		return fmt.Errorf("setfield slot")
	}
	recvRaw := ""
	recvLT := ""
	var recvTy *Type
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			recvRaw = t.Raw
			recvLT = c.llvmTypeOf(t)
			recvTy = t
		}
	}
	if recvLT == "" {
		recvLT = "%" + sanitize(recvRaw)
	}
	// Lvalue projection: when the receiver was read out of a struct field or a
	// container element (`self.pool.nodes[i].kind = x`, `o.i.v = n`), its alloca
	// holds a COPY and the store below would be lost. Redirect the write at the
	// real storage (GEP chain from the addressable base) instead.
	if p, ptid, projected := c.lvalueAddrOf(inst.Args[0]); projected && p != "" && os.Getenv("NOLANG_LV_FIELD") == "" {
		if pt := c.mod.Type(ptid); pt != nil {
			recvSlot = p
			recvTy = pt
			recvRaw = pt.Raw
			recvLT = c.llvmTypeOf(pt)
		}
	}
	// VIEW auto-deref: a `&T` view is `T*`; writing a field through it must
	// load the stored pointer first so the GEP lands on the borrowed struct
	// (otherwise the store writes into the view's own slot and is lost).
	if c.isViewValue(inst.Args[0]) && strings.HasSuffix(recvLT, "*") {
		c.loadSeq++
		pv := fmt.Sprintf("%%vld%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", pv, recvLT, recvLT, recvSlot))
		recvLT = strings.TrimSuffix(recvLT, "*")
		recvRaw = strings.TrimPrefix(recvRaw, "&")
		recvSlot = pv
		if id := c.mod.internType(recvRaw); id != NoType {
			recvTy = c.mod.Type(id)
		}
	}
	if isOptionType(recvLT) {
		// `?T.field = v`: peel the option (field 1 holds the inline payload),
		// then GEP into the inner struct's field and store v there.
		elem, _ := parseOptionElem(recvRaw)
		_, payloadLT := c.optionType(elem)
		innerRaw := elem
		structKey := c.structKeyOf(innerRaw)
		if structKey == "" {
			structKey = innerRaw
		}
		idx, ok := c.mod.FieldIndex(structKey, inst.Str)
		if !ok {
			c.fail("setfield: field %q not found on %q (option %q) in func %d", inst.Str, innerRaw, recvRaw, c.cf)
			return fmt.Errorf("setfield field")
		}
		_, valV := c.loadVal(inst.Args[1])
		fieldLT, _ := c.ptype(inst.Args[1])
		if fieldLT == "" {
			fieldLT = "i64"
		}
		// Overflow-default: if the RHS is ?T (an %option) but the field is a
		// scalar, unwrap the ok payload before storing — mirror legacy, which
		// stores the scalar payload, not the whole {tag,payload} struct. Without
		// this, opt rejects the store as "defined with type '%option' but
		// expected 'i64'" and the build fails under NOLANG_MIR=3.
		if strings.HasPrefix(recvLT, "%option") && !strings.HasPrefix(fieldLT, "%option") && fieldLT != "%str-long" && fieldLT != "%vec" {
			plt, pl := c.optionPayloadOf(inst.Args[1], valV, recvLT)
			if pl == "" {
				pl, plt = valV, recvLT
			}
			if cv2 := c.coerce(plt, pl, fieldLT); cv2 != "" {
				valV = cv2
			} else {
				valV = pl
			}
		}
		// Fixed-array RHS into a slice-typed (option-wrapped) field: same
		// [N x T] -> %vec borrow-view coercion as the plain setfield path below.
		if strings.HasPrefix(fieldLT, "[") {
			if fi := c.mod.StructFields[structKey]; idx < len(fi) && strings.HasPrefix(fi[idx].TypeRaw, "[]") {
				if rs := c.valSlot[inst.Args[1]]; rs != "" {
					valV = c.vecViewValue(fieldLT, rs)
					fieldLT = "%vec"
				}
			}
		}
		pg := c.optPayloadTypedAddr(recvSlot, payloadLT, payloadLT)
		c.loadSeq++
		gp := fmt.Sprintf("%%gp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gp, payloadLT, payloadLT, pg, idx))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, valV, fieldLT, gp))
		return nil
	}
	valLT, valV := c.loadVal(inst.Args[1])
	fieldLT, _ := c.ptype(inst.Args[1])
	if fieldLT == "" {
		fieldLT = "i64"
	}
	// Overflow-default: if the RHS is ?T (an %option) but the field is a scalar,
	// unwrap the ok payload before storing — mirror legacy, which stores the
	// scalar payload, not the whole {tag,payload} struct. Without this, opt
	// rejects the store as "defined with type '%option' but expected 'i64'" and
	// the build fails under NOLANG_MIR=3. Mirrors emitIndexStore's unwrap.
	if strings.HasPrefix(valLT, "%option") && !strings.HasPrefix(fieldLT, "%option") && fieldLT != "%str-long" && fieldLT != "%vec" {
		plt, pl := c.optionPayloadOf(inst.Args[1], valV, valLT)
		if pl == "" {
			pl, plt = valV, valLT
		}
		if cv2 := c.coerce(plt, pl, fieldLT); cv2 != "" {
			valV = cv2
		} else {
			valV = pl
		}
	}
	// Container pseudo-fields (len/cap/data) for slice/str are layout-derived
	// and have no StructFields entry. Handle them via GEP into the aggregate,
	// exactly mirroring emitLenCap's read path. This is what lets std methods
	// like vec.push write `self.len = self.len + 1` under pure MIR.
	if recvTy != nil && (recvTy.Kind == KindSlice || recvTy.Kind == KindStr) {
		if idx, ok := containerFieldIndex(inst.Str); ok {
			// `X.len = n` on a container that has NO buffer yet (a freshly
			// zero-initialised str field, e.g. `p.nodes[i].str-val.len = 3`)
			// used to write the length while leaving data NULL, so the very
			// next `X[0] = b` / `X.len()` dereferenced null and SIGSEGV'd.
			// Back the length write with an allocation so the container really
			// has n elements' worth of storage.
			if inst.Str == "len" {
				c.ensureContainerStorageForLen(recvSlot, recvLT, recvTy, valV)
			}
			c.loadSeq++
			gep := fmt.Sprintf("%%gp%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, recvLT, recvLT, recvSlot, idx))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, valV, fieldLT, gep))
			return nil
		}
	}
	structKey := c.structKeyOf(recvRaw)
	if structKey == "" {
		structKey = recvRaw
	}
	idx, ok := c.mod.FieldIndex(structKey, inst.Str)
	if !ok {
		c.fail("setfield: field %q not found on %q in func %d", inst.Str, recvRaw, c.cf)
		return fmt.Errorf("setfield field")
	}
	// Fixed-array RHS stored into a slice-typed field (`c.data = [1,2,3]` where
	// `data []i64`): coerce [N x T] -> %vec borrow view (len=N, cap=0,
	// data=&arr[0]) so the field holds a real slice header. Legacy treats [N]T
	// as []T pointing at the array's storage; without this the raw array bytes
	// are written into the 24-byte %vec slot, so `.len()` reads the first
	// element and `.[i]` derefs the (misread) data pointer -> SIGSEGV.
	if strings.HasPrefix(fieldLT, "[") {
		if fi := c.mod.StructFields[structKey]; idx < len(fi) && strings.HasPrefix(fi[idx].TypeRaw, "[]") {
			if rs := c.valSlot[inst.Args[1]]; rs != "" {
				valV = c.vecFromArraySink(fieldLT, rs)
				fieldLT = "%vec"
			}
		}
	}
	structLT := recvLT
	c.loadSeq++
	gep := fmt.Sprintf("%%gp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, structLT, structLT, recvSlot, idx))
	// POINTER FIELD (Phase 1 layout flip): the slot holds a `%T*`, so the value
	// must be written into the POINTEE -- never into the slot itself (that
	// overruns the 8-byte slot) and never as the source's own pointer.
	//
	// The pointee is reused when it already exists rather than reallocated. Two
	// reasons: an in-place overwrite is what value assignment means, and
	// reallocating leaks the previous pointee on every store (a loop that
	// assigns the field would leak one pointee per iteration).
	//
	// Copying rather than sharing the source's pointer is deliberate for Phase
	// 1. Sharing would be a DOUBLE FREE: the source is still live (its owner
	// drops it later) and nothing yet records that this field merely borrows.
	// Sharing is what Phase 1.5 introduces, and only for a field PROVEN to
	// borrow from a parameter. So Phase 1 accepts the copy cost; the json pool
	// keeps being copied until the borrow mechanism lands.
	if f, ok := c.fieldAt(structKey, idx); ok && c.fieldIsPointer(f) {
		p := c.ptrFieldAddr(fieldLT, gep)
		// memcpy only when valV is exactly the source slot's own value; if the
		// value was coerced above (option unwrap), copying the raw slot would
		// copy the wrong bytes.
		if srcSlot := c.valSlot[inst.Args[1]]; srcSlot != "" && valLT == fieldLT && c.shouldUseMemcpy(fieldLT) {
			c.emitMemcpy(p, srcSlot, c.typeSizeOperand(fieldLT))
		} else {
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, valV, fieldLT, p))
		}
		c.cloneStructFieldLeaves(structKey, idx, p, fieldLT)
		return nil
	}
	// #83 SROA guard: for large aggregate field types, use memcpy from the
	// source slot to the GEP'd field address instead of load+store.
	if c.shouldUseMemcpy(fieldLT) {
		if srcSlot := c.valSlot[inst.Args[1]]; srcSlot != "" {
			sz := c.typeSizeOperand(fieldLT)
			c.emitMemcpy(gep, srcSlot, sz)
			c.cloneStructFieldLeaves(structKey, idx, gep, fieldLT)
			return nil
		}
	}
	// OWNED `str` FIELD: deep-clone the incoming value. A plain
	// `store %str-long` copies only the {len,cap,data} descriptor, so the field
	// ends up SHARING the source's heap buffer; when the source is reassigned or
	// dropped, @str_free runs on it and the field dangles. Verified before this
	// fix:
	//   `s = 'C'; p.name = s; s = 'D'; print(p.name)`  ->  printed ""
	//   `mk = (n str) (out ?person) { p.name = n; out = p }` -> the caller freed
	//   the argument and `it.name` was lost entirely.
	//
	// The PREVIOUS occupant is freed, which is only sound because struct copies
	// now deep-copy their owned leaves (emitLeafStructClone + the
	// moveStructSharesHeap promotion). Before that, `b = a` shared the leaf
	// buffer, so freeing the old occupant on `b.name = 'B'` freed a buffer
	// `a.name` still read — verified, it turned `leafshare.no` from `A/A/B`
	// into `A//B`. That is why the first version of this clone had to leak:
	// measured at 206 MB vs 2.7 MB for 200 assignments of a 1 MiB string.
	//
	// Order matters: load the old descriptor, clone the incoming value, store
	// the clone, then free. Cloning BEFORE the free keeps `p.name = p.name`
	// correct (the clone is independent of the buffer being released).
	if fieldLT == "%str-long" && valLT == "%str-long" {
		c.loadSeq++
		old := fmt.Sprintf("%%sfold%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", old, fieldLT, fieldLT, gep))
		c.loadSeq++
		cl := fmt.Sprintf("%%sfcl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, fieldLT, fieldLT, valV))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, cl, fieldLT, gep))
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", fieldLT, old))
		return nil
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, valV, fieldLT, gep))
	c.cloneStructFieldLeaves(structKey, idx, gep, fieldLT)
	return nil
}

// cloneStructFieldLeaves deep-copies the inline owned `str` leaves of the
// STRUCT-TYPED value that was just written into `recv.field` (at `dst`).
//
// A struct-typed field write is a bitwise copy — memcpy or a plain `store` —
// which duplicates the `{len,cap,data}` descriptor of every inline `str` inside
// WITHOUT duplicating the buffer. The field and the source then share one
// buffer while both are eventually dropped, so whichever side is released first
// pulls the memory out from under the other:
//
//	outer { p inner }        inner { name str }
//	c = outer { p: obj.p }   ; obj borrowed -> caller still owns the buffer
//	... drop the child ...   ; parent's `p.name` now reads freed memory
//
// Verified before this fix (`o10.no`): the child printed `alice`, then dropping
// it turned the parent's `p.name` into an empty string; with an option-held
// parent the child was already blank on the first read.
//
// This is the struct-field mirror of the `%str-long` clone a few lines above
// (same reasoning, one level of indirection deeper). It is deliberately
// unconditional: deciding "is the source still live?" needs liveness the
// codegen does not carry, and an unnecessary clone only costs a copy, whereas a
// missed clone is a use-after-free.
//
// Scope, both deliberate:
//   - Built-in container types are skipped: `%vec`/`%str-long`/`%option` ARE in
//     StructFields, and cloning them as user structs would store 24 bytes into
//     an 8-byte slot.
//   - Fixed arrays of structs are skipped: `emitLeafFieldsCloneR` walks struct
//     fields, not array elements, so it would clone nothing while
//     `emitStructDropHelper` (which does not walk arrays either) frees nothing —
//     the two stay consistent and the array case keeps its current behaviour.
func (c *codegen) cloneStructFieldLeaves(structKey string, idx int, dst, fieldLT string) {
	if fieldLT == "" || strings.HasPrefix(fieldLT, "[") || isBuiltinContainerLT(fieldLT) {
		return
	}
	fields := c.mod.StructFields[structKey]
	if idx < 0 || idx >= len(fields) {
		return
	}
	subKey := c.mod.StructKeyOf(fields[idx].TypeRaw)
	if subKey == "" {
		return
	}
	if !c.mod.StructHasOwnedLeafFields(subKey) {
		return
	}
	c.emitLeafFieldsClone(dst, fieldLT, subKey)
}

// emitLenCap lowers a container length/capacity read (OpLen / OpCap).
//
// These are properties of the container, not struct fields: the layout is
// derived from the type kind, so it works for every element type instead of
// needing a StructFields entry per slice/array type.
//
//	%str-long { len, cap, data } -> len = field 0, cap = field 1
//	%vec      { len, cap, ...  } -> len = field 0, cap = field 1
//	[N x T]   fixed array       -> len = cap = N (compile-time constant)
func (c *codegen) emitLenCap(inst *Inst) error {
	recvT := NoType
	if c.cf != NoFunc {
		if f := c.mod.Func(c.cf); f != nil {
			recvT = f.LocalTypes[inst.Args[0]]
		}
	}
	if recvT == NoType {
		if val := c.mod.Value(inst.Args[0]); val != nil {
			recvT = val.Type
		}
	}
	ty := c.mod.Type(recvT)
	if ty == nil {
		c.fail("len/cap on untyped value in func %d", c.cf)
		return fmt.Errorf("lencap type")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("len/cap result has no slot in func %d", c.cf)
		return fmt.Errorf("lencap dst slot")
	}
	switch ty.Kind {
	case KindArray:
		n := int64(0)
		if len(ty.Sizes) > 0 {
			n = ty.Sizes[0]
		}
		c.sb.WriteString(fmt.Sprintf("  store i64 %d, i64* %s\n", n, dstSlot))
		return nil
	case KindStr, KindSlice:
		fieldIdx := 0
		if inst.Op == OpCap {
			fieldIdx = 1
		}
		recvLT := c.llvmTypeOf(ty)
		recvSlot := c.valSlot[inst.Args[0]]
		if recvSlot == "" {
			c.fail("len/cap receiver has no slot in func %d", c.cf)
			return fmt.Errorf("lencap slot")
		}
		c.loadSeq++
		lv := fmt.Sprintf("%%lc%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, recvLT, recvLT, recvSlot))
		c.loadSeq++
		ev := fmt.Sprintf("%%le%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", ev, recvLT, lv, fieldIdx))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ev, dstSlot))
		return nil
	case KindStruct:
		if ty.Raw == "txt" {
			// txt.len reads the i8 len byte (field 1) and zero-extends to i64.
			recvSlot := c.valSlot[inst.Args[0]]
			if recvSlot == "" {
				c.fail("len/cap receiver has no slot in func %d", c.cf)
				return fmt.Errorf("lencap slot")
			}
			c.loadSeq++
			lGEP := fmt.Sprintf("%%tlg%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 1\n", lGEP, recvSlot))
			c.loadSeq++
			lLd := fmt.Sprintf("%%tll%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", lLd, lGEP))
			c.loadSeq++
			lZx := fmt.Sprintf("%%tlz%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", lZx, lLd))
			c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", lZx, dstSlot))
			return nil
		}
		c.fail("len/cap unsupported on struct %s in func %d", ty.Raw, c.cf)
		return fmt.Errorf("lencap struct")
	default:
		c.fail("len/cap unsupported on %s (%s) in func %d", ty.Raw, ty.Kind, c.cf)
		return fmt.Errorf("lencap kind")
	}
}

// emitSliceOp lowers `a[lo:hi]` (OpSliceOp) for %vec (vec/slice) and %str-long
// (str) receivers. It allocates a fresh backing buffer and copies the [lo, hi)
// byte range out of the receiver. The copied-out sub-slice does NOT alias the
// original, which keeps the MIR ownership model simple: each backing buffer is
// freed exactly once by its own vec_free/str_free. newLen = hi - lo; the element
// stride comes from the slice element type (inst.Type.Elem); for strings the
// stride is always 1 byte. The receiver is always read through its slot, so
// by-reference containers (a %vec passed by alloca) are handled uniformly with a
// value held in a local.
func (c *codegen) emitSliceOp(inst *Inst) error {
	if len(inst.Args) < 1 {
		c.fail("slice op: missing receiver in func %d", c.cf)
		return fmt.Errorf("slice op arity")
	}
	recvLT, _ := c.ptype(inst.Args[0])
	recvSlot := c.valSlot[inst.Args[0]]
	if recvSlot == "" {
		c.fail("slice op receiver has no slot in func %d", c.cf)
		return fmt.Errorf("slice op recv slot")
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("slice op result has no slot in func %d", c.cf)
		return fmt.Errorf("slice op dst slot")
	}

	// Determine the source data pointer (i8*), the logical length (i64) and the
	// element stride (bytes) from the receiver shape:
	//   %vec        : data stored as i64 intptr in field 2; len = field 0
	//   %str-long   : data is i8* in field 2; len = field 0; stride = 1
	//   [N x E]     : fixed array; data = &arr[0]; len = N; stride = sizeof(E)
	var srcPtr, rlen string
	// Element size, carried as an i64 OPERAND so that both the statically known
	// cases (a literal) and the unknown ones (an LLVM-derived operand) work.
	//
	// WHY NOT elemStride: it falls back to 8 for every type it does not know
	// (user structs, boxed option payloads, fixed arrays). The sub-range START is then
	// computed as a manual `lo * stride`, while the element ADDRESSES inside the
	// buffer are typed GEPs that LLVM scales by the REAL size — so the two
	// disagree and the slice reads from the wrong offset. Measured on
	// `[]person` (`tmp/wslice.no`, `people[1..3]`): reported len 3 instead of 2
	// and then aborted — segfault with the corrected element size, `trace/BPT
	// trap` with the old under-sized buffer, i.e. broken either way.
	//
	// For a statically known size the operand is the SAME decimal literal the
	// old code emitted, so every previously-correct case keeps byte-identical
	// IR; only the types elemStride guessed wrong change.
	stride := int64(0) // statically known size; 0 => use strideOp
	strideOp := "8"    // conservative default = elemStride's old fallback
	setElemStride := func(elemLT string) {
		if n, ok := c.mirStaticTypeSize(elemLT); ok {
			stride, strideOp = n, fmt.Sprintf("%d", n)
			return
		}
		strideOp = c.typeSizeOperand(elemLT)
	}
	isFixedArray := strings.HasPrefix(recvLT, "[")
	switch {
	case recvLT == "%vec":
		rv := c.treg("sor")
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", rv, recvLT, recvLT, recvSlot))
		rlen = c.treg("sol")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", rlen, recvLT, rv))
		rdata := c.treg("sod")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", rdata, recvLT, rv))
		srcPtr = c.treg("sosp")
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", srcPtr, rdata))
		if st := c.mod.Type(inst.Type); st != nil && st.Elem != NoType {
			if et := c.mod.Type(st.Elem); et != nil {
				setElemStride(c.llvmTypeOf(et))
			}
		}
	case recvLT == "%str-long":
		rv := c.treg("sor")
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", rv, recvLT, recvLT, recvSlot))
		rlen = c.treg("sol")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", rlen, recvLT, rv))
		rdata := c.treg("sod")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", rdata, recvLT, rv))
		srcPtr = rdata
		stride, strideOp = 1, "1"
	case isFixedArray:
		// Fixed array [N x E]: length is the compile-time size N; the data
		// pointer is the address of element 0.
		var n int64
		if t := c.mod.Value(inst.Args[0]); t != nil {
			if mt := c.mod.Type(t.Type); mt != nil && len(mt.Sizes) > 0 {
				n = mt.Sizes[0]
			}
		}
		rlen = c.treg("sol")
		c.sb.WriteString(fmt.Sprintf("  %s = add i64 %d, 0\n", rlen, n))
		dataPtr := c.treg("sod")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", dataPtr, recvLT, recvLT, recvSlot))
		srcPtr = dataPtr
		if t := c.mod.Value(inst.Args[0]); t != nil {
			if mt := c.mod.Type(t.Type); mt != nil && mt.Elem != NoType {
				if et := c.mod.Type(mt.Elem); et != nil {
					setElemStride(c.llvmTypeOf(et))
				}
			}
		}
	default:
		c.fail("slice op on unsupported receiver type %s in func %d", recvLT, c.cf)
		return fmt.Errorf("slice op recv type")
	}

	// lo / hi are always i64 slice indices; default [lo=0 : hi=len].
	loV := "0"
	if len(inst.Args) > 1 {
		loT, loR := c.loadVal(inst.Args[1])
		if r := c.coerce(loT, loR, "i64"); r != "" {
			loV = r
		} else {
			loV = loR
		}
	}
	hiV := rlen
	if len(inst.Args) > 2 {
		hiT, hiR := c.loadVal(inst.Args[2])
		if r := c.coerce(hiT, hiR, "i64"); r != "" {
			hiV = r
		} else {
			hiV = hiR
		}
	}

	// rightInc: the SliceFlagRightInc bit of inst.Int means the upper bound is
	// inclusive (']'). Test the BIT, not `inst.Int == 1`: SliceFlagView shares
	// the same word, so a forward view slice carries 0b11 and an exact compare
	// against 1 would silently drop the +1 (tests/str-slice.no regressed).
	// Compute abs(hi - lo) first, then add 1 for rightInc — matching
	// legacy's computeReversibleLen which does:
	//   forward:  len = end - start + (1 if rightInc)
	//   reverse:  len = start - end + (1 if rightInc)
	// We must NOT pre-adjust hi before the abs, because
	//   abs(lo - (hi+1)) != abs(lo - hi) + 1  (off by one for reverse).

	// Detect reverse slice (lo > hi) at runtime and compute abs(hi - lo).
	// Forward: len = hi - lo.  Reverse: len = lo - hi.
	revCmp := c.treg("sorv")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", revCmp, loV, hiV))
	fwdLen := c.treg("sofl")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", fwdLen, hiV, loV))
	revLen := c.treg("sorl")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", revLen, loV, hiV))
	absLen := c.treg("sonl")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", absLen, revCmp, revLen, fwdLen))

	// Add 1 for inclusive upper bound (']')
	newLen := absLen
	if inst.Int&SliceFlagRightInc != 0 {
		newLen = c.treg("sohi")
		c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", newLen, absLen))
	}

	// Byte offset of the sub-range start inside the backing buffer.
	// For forward slices this is lo*stride; for reverse slices it's also lo
	// (the high index), because the copy starts from there.
	var off string
	if stride == 1 {
		off = loV
	} else {
		loBytes := c.treg("solb")
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", loBytes, loV, strideOp))
		off = loBytes
	}
	// Total byte count to copy.
	var bytes string
	if stride == 1 {
		bytes = newLen
	} else {
		nlBytes := c.treg("sonb")
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", nlBytes, newLen, strideOp))
		bytes = nlBytes
	}

	srcBase := c.treg("sosb")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", srcBase, srcPtr, off))

	// SLICE VIEW: when the sub-range is statically FORWARD and does not escape,
	// the result ALIASES the receiver's buffer instead of copying it.
	//
	// Non-ownership is encoded as cap == 0 — exactly the convention @vec_free
	// already uses to skip freeing borrowed slice views, so the existing drop
	// insertion stays correct without any analysis change: the value is still
	// "owned" to the analysis, but freeing it is a no-op.
	//
	// Only `%vec` / `%str-long` destinations can carry a cap; a fixed-array
	// destination has no header, so it keeps the copy path.
	// A fixed array is NOT heap-backed: aliasing its inline stack storage would
	// (a) hand out a pointer that dies with the enclosing scope rather than with
	// the frame, and (b) change what an out-of-range sub-range reads (the copy
	// path lands in fresh malloc'd memory, the view reads neighbouring stack
	// slots — tests/test-slice-heavy.no prints `a[2..5]` of a `[5]i64`).
	// Heap containers (str / vec / slice) keep the zero-copy view.
	isView := inst.Int&SliceFlagView != 0 && !isFixedArray &&
		(dstLT == "%vec" || dstLT == "%str-long")
	var newBuf string
	capV := newLen
	if isView {
		newBuf = srcBase
		capV = "0"
	} else {
		// Fresh backing buffer: the sub-slice gets its own copy and never
		// aliases the source.
		newBuf = c.treg("snbuf")
		c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newBuf, bytes))

		// Delegate the copy to @mir_slice_copy, a runtime helper that handles
		// both forward (memcpy) and reverse (per-element backward copy) cases
		// without introducing basic-block branches in the emitted code.
		c.ensureMirSliceCopy()
		c.sb.WriteString(fmt.Sprintf("  call void @mir_slice_copy(i8* %s, i8* %s, i64 %s, i64 %s, i1 %s)\n", newBuf, srcBase, newLen, strideOp, revCmp))
	}

	if dstLT == "%str-long" {
		s0 := c.treg("sos0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", s0, newLen))
		s1 := c.treg("sos1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", s1, s0, capV))
		s2 := c.treg("sos2")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", s2, s1, newBuf))
		c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", s2, dstSlot))
	} else if strings.HasPrefix(dstLT, "[") {
		// Fixed-array destination [M x E]: dstSlot is already a pointer to a
		// stack slot of type [M x E]; copy the sub-range bytes straight into it
		// (no alloca, no store — a fixed array is stored inline by value).
		dstData := c.treg("sodd")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", dstData, dstLT, dstLT, dstSlot))
		c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", dstData, newBuf, bytes))
	} else {
		// %vec (default): data stored as i64 intptr.
		dp := c.treg("sodp")
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dp, newBuf))
		s0 := c.treg("sos0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, newLen))
		s1 := c.treg("sos1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, capV))
		s2 := c.treg("sos2")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dp))
		c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, dstSlot))
	}
	return nil
}

// ensureMirSliceCopy emits the @mir_slice_copy runtime helper if it hasn't
// been emitted yet. The helper copies `count` elements of `stride` bytes each
// from `src` to `dst`. When `reversed` is true, it walks `src` backward (from
// the high index toward the low index) and writes them forward into `dst`,
// producing a reversed copy. When false, it's a plain memcpy.
//
// Encapsulating the branch and loop inside a function avoids emitting basic-
// block branches in the MIR codegen's linear instruction stream, which would
// break the alloca-slot model (non-entry-block allocas interact badly with
// llc -O0's register spilling).
func (c *codegen) ensureMirSliceCopy() {
	if c.mirSliceCopyEmitted {
		return
	}
	c.mirSliceCopyEmitted = true
	c.global(`define void @mir_slice_copy(i8* %dst, i8* %src, i64 %count, i64 %stride, i1 %reversed) {
entry:
  br i1 %reversed, label %rev, label %fwd
fwd:
  %fwd_bytes = mul i64 %count, %stride
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst, i8* %src, i64 %fwd_bytes, i1 false)
  ret void
rev:
  %i_ptr = alloca i64
  store i64 0, i64* %i_ptr
  br label %rev_cond
rev_cond:
  %i = load i64, i64* %i_ptr
  %cmp = icmp slt i64 %i, %count
  br i1 %cmp, label %rev_body, label %rev_end
rev_body:
  %src_off = mul i64 %i, %stride
  %neg_off = sub i64 0, %src_off
  %src_p = getelementptr i8, i8* %src, i64 %neg_off
  %dst_p = getelementptr i8, i8* %dst, i64 %src_off
  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %dst_p, i8* %src_p, i64 %stride, i1 false)
  %next = add i64 %i, 1
  store i64 %next, i64* %i_ptr
  br label %rev_cond
rev_end:
  ret void
}`)
}

// elemStride returns the byte size of an LLVM element type, used to scale slice
// index arithmetic for non-byte element types.
func elemStride(lt string) int64 {
	switch lt {
	case "i8", "i1":
		return 1
	case "i64", "double":
		return 8
	case "%str-long":
		return 24
	default:
		return 8
	}
}

// arraySizeOf parses the leading "[N x E]" (LLVM) or "[N]E" (compact) of a
// fixed-array type string and returns N (the element count). It is used to
// derive the length of a slice view built over a fixed array (fixed-array ->
// vec coercion at call sites and in buildVecViewFromArray).
//
// The previous implementation only accepted a space-less "[N…]" prefix and
// bailed at the space in the standard LLVM "[N x E]" form, so it returned 0 for
// every real array type. That made every fixed-array -> vec view carry len=0 —
// e.g. `['a','b','c']` (a [3 x %str-long] coerced to %vec) built `%vec{0,0,..}`
// and `[1,2,3].len()` was 0 under MIR=3 while the legacy backend returns 3.
func arraySizeOf(lt string) (int64, bool) {
	if len(lt) < 3 || lt[0] != '[' {
		return 0, false
	}
	rest := lt[1:]
	// N is the integer immediately inside the leading "["; it ends at " x "
	// (LLVM separator) or "]" (compact form), whichever comes first.
	limit := -1
	if i := strings.Index(rest, " x "); i >= 0 {
		limit = i
	}
	if j := strings.IndexByte(rest, ']'); j >= 0 && (limit < 0 || j < limit) {
		limit = j
	}
	if limit <= 0 {
		return 0, false
	}
	var n int64
	for i := 0; i < limit; i++ {
		c := rest[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if n == 0 {
		return 0, false
	}
	return n, true
}

// emitStructTypes emits LLVM struct type declarations for every user/std struct
// encountered (builtins %str-long/%vec/%option are declared in the prelude). The
// field layout comes from the module's StructFields so GEP indices stay
// consistent with the declared type.
//
// Only concrete definitions are emitted — NOT an opaque forward declaration
// followed by a concrete one. Modern LLVM (opaque-pointer era, LLVM 15+) treats
// `type opaque` as a full definition, so a later `type { ... }` redefinition is
// rejected by `opt` ("redefinition of type"). Named struct types in LLVM IR may
// be forward-referenced freely (a struct may reference another named struct that
// is declared later in the module), so a single concrete pass is both sufficient
// and correct regardless of field-dependency order.
func (c *codegen) emitStructTypes() {
	for raw, fields := range c.mod.StructFields {
		if raw == "str" || raw == "vec" {
			continue // declared in the prelude
		}
		if raw == "txt" {
			// Fixed 256-byte stack buffer: { [255 x i8] data, i8 len }. The
			// FieldInfo can't express the [255 x i8] array width, so emit it
			// explicitly to match the GEP indices used by print/.len/txt-from-str.
			c.sb.WriteString("%txt = type { [255 x i8], i8 }\n")
			continue
		}
		lt := "%" + sanitize(raw)
		parts := make([]string, 0, len(fields))
		for _, f := range fields {
			tid := c.mod.internType(f.TypeRaw)
			flt := c.llvmTypeOf(c.mod.Type(tid))
			if c.fieldIsPointer(f) {
				flt += "*"
			}
			parts = append(parts, flt)
		}
		c.sb.WriteString(fmt.Sprintf("%s = type { %s }\n", lt, strings.Join(parts, ", ")))
	}
	// The err message is a %str-long and can be read out of ANY option, whatever
	// its declared element is, so its zero stand-in must exist even in a module
	// that never mentions `?str` — and it must be declared before the helpers
	// that reference it. Without it a boxed err read would fall through to the
	// (UB) slot-pointer fallback.
	if !c.optionPayloadInline("%str-long") {
		c.emitOptionBoxHelpers("%str-long")
	}
	// Emit a print helper per distinct option PAYLOAD type. With one `%option`
	// for every element the payload type is no longer recoverable from the LLVM
	// type name, so the helper is keyed (and named) by the payload instead.
	seenOpt := map[string]bool{}
	for _, t := range c.mod.Types {
		if t.Kind != KindOption {
			continue
		}
		elem, ok := parseOptionElem(t.Raw)
		if !ok {
			continue
		}
		_, payloadLT := c.optionType(elem)
		if seenOpt[payloadLT] {
			continue
		}
		seenOpt[payloadLT] = true
		// The helper does the nil-tag branch INSIDE its own function so
		// emitCall can route the print through a plain `call` without emitting
		// new basic blocks mid-function (which breaks LLVM verification).
		// Non-printable payloads (%vec / user-struct / fixed-array) are
		// skipped; emitCall c.fail()s.
		// Box helpers first: the print helper dereferences the boxed payload
		// through the zero stand-in global they declare.
		if !c.optionPayloadInline(payloadLT) {
			c.emitOptionBoxHelpers(payloadLT)
		}
		c.emitOptionPrintHelper(payloadLT)
	}
	// Tagged enums: %tenum_<name> = type { i64, [N x i64] }.
	for raw, ei := range c.mod.TaggedEnums {
		n := ei.PayloadSlots
		if n < 1 {
			n = 1
		}
		c.sb.WriteString(fmt.Sprintf("%%tenum_%s = type { i64, [%d x i64] }\n", sanitize(raw), n))
	}
}

// fieldIsPointer reports whether a field's slot holds a `%T*` rather than an
// inlined `%T`. The definition lives on Module (see Module.FieldIsPointer) so
// the analysis passes and codegen share it; this is the codegen-side alias.
//
// EVERY Phase 1 behaviour change is gated on this predicate, which returns
// false when the switch is off — that is what makes the switch-off path
// provably byte-identical to the pre-Phase-1 backend.
func (c *codegen) fieldIsPointer(f FieldInfo) bool {
	return c.mod.FieldIsPointer(f)
}

// fieldAt returns the FieldInfo at index idx of the struct registered under
// structKey. `structKey` must be the same key FieldIndex resolved with, so the
// index is guaranteed to line up. Returns false for container pseudo-fields
// (len/cap/data) and unknown structs, which have no tag and are never pointers.
func (c *codegen) fieldAt(structKey string, idx int) (FieldInfo, bool) {
	fields := c.mod.StructFields[structKey]
	if idx < 0 || idx >= len(fields) {
		return FieldInfo{}, false
	}
	return fields[idx], true
}

// taggedEnumOf looks up an enum's variant table by raw type name.
func (c *codegen) taggedEnumOf(raw string) *TaggedEnumInfo {
	return c.mod.TaggedEnums[raw]
}

// enumVariantByTag returns the variant whose discriminant is tag.
func enumVariantByTag(ei *TaggedEnumInfo, tag int64) *VariantInfo {
	if ei == nil {
		return nil
	}
	for i := range ei.Variants {
		if ei.Variants[i].Tag == tag {
			return &ei.Variants[i]
		}
	}
	return nil
}

// enumFieldSlot returns the 8-byte slot offset of field `idx` within a
// variant's payload, plus the number of slots that field occupies. Slots are
// what make the union layout computable without reimplementing LLVM's struct
// alignment rules: every field starts on an 8-byte boundary, which satisfies
// the alignment of every scalar and of %str-long / %vec / %option.
func (c *codegen) enumFieldSlot(v *VariantInfo, idx int) (int64, int64) {
	if v == nil || idx < 0 || idx >= len(v.Fields) {
		return 0, 1
	}
	var off int64
	for i := 0; i < idx; i++ {
		off += c.enumRawSlots(v.Fields[i])
	}
	return off, c.enumRawSlots(v.Fields[idx])
}

// enumRawSlots returns the payload-slot width of a nolang type.
func (c *codegen) enumRawSlots(raw string) int64 {
	switch raw {
	case "str", "vec":
		return 3
	}
	if strings.HasPrefix(raw, "[]") {
		return 3
	}
	if strings.HasPrefix(raw, "?") {
		return 2
	}
	if fields, ok := c.mod.StructFields[raw]; ok {
		var n int64
		for _, f := range fields {
			n += c.enumRawSlots(f.TypeRaw)
		}
		if n > 0 {
			return n
		}
	}
	return 1
}

// emitEnumNew builds a tagged-enum variant value `{ tag, payload }`.
//
// The whole value is stored with a zeroed payload first, then each field is
// written into its slot through a bitcast of the payload base pointer. The
// zeroing matters: the payload is a union, so slots not written by this
// variant would otherwise hold whatever was in the alloca, and a later read of
// a wider field (a %str-long) would hand a garbage data pointer to @str_free.
func (c *codegen) emitEnumNew(inst *Inst) error {
	dstLT, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	if slot == "" || dstLT == "void" || dstLT == "" {
		return nil
	}
	ei := c.taggedEnumOf(enumRawOfType(c.mod, inst.Type))
	nSlots := int64(1)
	if ei != nil && ei.PayloadSlots > 0 {
		nSlots = ei.PayloadSlots
	}
	c.sb.WriteString(fmt.Sprintf("  store %s { i64 %d, [%d x i64] zeroinitializer }, %s* %s\n",
		dstLT, inst.Int, nSlots, dstLT, slot))
	if len(inst.Args) == 0 {
		return nil
	}
	v := enumVariantByTag(ei, inst.Int)
	// Payload base: GEP to field 1 -> [N x i64]*.
	c.loadSeq++
	base := fmt.Sprintf("%%en%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
		base, dstLT, dstLT, slot))
	arrLT := fmt.Sprintf("[%d x i64]", nSlots)
	for i, a := range inst.Args {
		if a <= NoVal {
			continue
		}
		off, _ := c.enumFieldSlot(v, i)
		valT, valV := c.loadVal(a)
		fieldLT := valT
		if i < len(v.Fields) {
			if ft := c.mod.Type(c.mod.internType(v.Fields[i])); ft != nil {
				fieldLT = c.llvmTypeOf(ft)
			}
		}
		if fieldLT == "" {
			fieldLT = "i64"
		}
		c.loadSeq++
		gp := fmt.Sprintf("%%en%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %d\n",
			gp, arrLT, arrLT, base, off))
		storeLT, storeV := fieldLT, valV
		ptr := gp
		if fieldLT != "i64" {
			if cv := c.coerce(fieldLT, valV, valT); cv != "" {
				storeV = cv
			}
			c.loadSeq++
			bc := fmt.Sprintf("%%en%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = bitcast i64* %s to %s*\n", bc, gp, fieldLT))
			ptr = bc
		} else {
			if cv := c.coerce("i64", valV, valT); cv != "" {
				storeV = cv
			}
		}
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", storeLT, storeV, storeLT, ptr))
	}
	return nil
}

// emitEnumTag reads an enum value's discriminant (field 0).
func (c *codegen) emitEnumTag(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return nil
	}
	srcT, srcV := c.loadVal(inst.Args[0])
	valT := srcT
	if t := c.mod.Type(c.mod.Value(inst.Args[0]).Type); t != nil {
		valT = c.llvmTypeOf(t)
	}
	if valT == "" || valT == "void" {
		valT = srcT
	}
	c.loadSeq++
	tv := fmt.Sprintf("%%et%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", tv, valT, srcV))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", tv, dstSlot))
	return nil
}

// emitEnumField reads payload field `inst.Int` of an enum value, typed by
// inst.Type. The field lives at an 8-byte slot offset in the union payload, so
// the read is a GEP to the slot plus a bitcast to the field's real type.
func (c *codegen) emitEnumField(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return nil
	}
	srcSlot := c.valSlot[inst.Args[0]]
	srcT, srcV := c.loadVal(inst.Args[0])
	valT := ""
	if t := c.mod.Type(c.mod.Value(inst.Args[0]).Type); t != nil {
		valT = c.llvmTypeOf(t)
	}
	if valT == "" || valT == "void" {
		valT = srcT
	}
	fieldLT, _ := c.ptype(inst.Dst)
	if fieldLT == "" || fieldLT == "void" {
		fieldLT = "i64"
	}
	if srcSlot == "" {
		// Value (not address) form: extract from the register directly.
		c.loadSeq++
		tv := fmt.Sprintf("%%ef%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", tv, valT, srcV, inst.Int))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", tv, dstSlot))
		return nil
	}
	// The payload array's declared width must match the enum's real layout:
	// a str payload occupies 3 slots, so a hard-coded `[1 x i64]` GEP read the
	// wrong element type and yielded 0 (tests/test-tagged-enum.no: `b-res`).
	nSlots := int64(1)
	if ei := c.taggedEnumOf(enumRawOfType(c.mod, c.mod.Value(inst.Args[0]).Type)); ei != nil && ei.PayloadSlots > 0 {
		nSlots = ei.PayloadSlots
	}
	arrLT := fmt.Sprintf("[%d x i64]", nSlots)
	c.loadSeq++
	base := fmt.Sprintf("%%ef%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
		base, valT, valT, srcSlot))
	c.loadSeq++
	gp := fmt.Sprintf("%%ef%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %d\n",
		gp, arrLT, arrLT, base, inst.Int))
	if fieldLT == "i64" {
		c.loadSeq++
		lv := fmt.Sprintf("%%ef%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", lv, gp))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", lv, dstSlot))
		return nil
	}
	c.loadSeq++
	bc := fmt.Sprintf("%%ef%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i64* %s to %s*\n", bc, gp, fieldLT))
	c.loadSeq++
	lv := fmt.Sprintf("%%ef%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, fieldLT, fieldLT, bc))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, lv, fieldLT, dstSlot))
	return nil
}

// enumRawOfType returns the raw nolang type string of a MIR type id.
func enumRawOfType(m *Module, t TypeID) string {
	if ty := m.Type(t); ty != nil {
		return ty.Raw
	}
	return ""
}

// emitOptionPrintHelper emits a dedicated `define void @print_option_<payload>`
// helper for one option payload type. The helper spills the by-value `%option`,
// reads the tag (field 0), branches on nil (tag == 1) to print the literal
// "nil", and otherwise reads the payload out of the 24-byte slot and routes it
// to the matching scalar printer — mirroring the generic @print_option helper.
// Keeping the branch inside its own function lets emitCall route the print
// through a plain `call` instead of synthesizing new basic blocks mid-function
// (which breaks LLVM verification). Only payload types with a scalar printer
// (str/i64/i8/double/bool) produce a helper; the rest — including every BOXED
// payload, which is by definition a large aggregate — are skipped and left to
// emitCall's c.fail(). The helper name is stored in c.optPrintHelper[payloadLT].
func (c *codegen) emitOptionPrintHelper(payloadLT string) {
	var body string
	switch payloadLT {
	case "%str-long":
		body = "  call void @print_str(%str-long %p)\n"
	case "double":
		body = "  call void @print_double(double %p)\n"
	case "i64":
		body = "  call void @print_i64(i64 %p)\n"
	case "i8":
		body = "  %pz = zext i8 %p to i64\n  call void @print_i64(i64 %pz)\n"
	case "i1":
		body = "  call void @print_bool(i1 %p)\n"
	default:
		// %vec / user-struct / fixed-array / boxed: no scalar printer — skip.
		return
	}
	name := "@print_option_" + strings.NewReplacer("%", "", "-", "_", ".", "_", " ", "_", "*", "_").Replace(payloadLT)
	c.optPrintHelper[payloadLT] = name
	c.sb.WriteString(fmt.Sprintf("define void %s(%%option %%o) {\n", name))
	c.sb.WriteString("entry:\n")
	c.sb.WriteString("  %os = alloca %option\n")
	c.sb.WriteString("  store %option %o, %option* %os\n")
	// A BOXED payload lives behind slot[0]; an inline one sits in the slot.
	// optPayloadAddr is a codegen helper, so inline the same select here.
	c.sb.WriteString("  %otp = getelementptr inbounds %option, %option* %os, i32 0, i32 0\n")
	c.sb.WriteString("  %otag = load i64, i64* %otp\n")
	c.sb.WriteString("  %oisnil = icmp eq i64 %otag, 1\n")
	c.sb.WriteString("  br i1 %oisnil, label %onil, label %osome\n")
	c.sb.WriteString("onil:\n")
	c.sb.WriteString("  call i64 @write(i32 1, i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.nilstr, i64 0, i64 0), i64 3)\n")
	c.sb.WriteString("  ret void\n")
	c.sb.WriteString("osome:\n")
	// optPayloadAddr, not a raw GEP to field 1: when the slot is configured
	// below 24 even a %str-long payload is heap-boxed, and field 1 then holds
	// the box POINTER rather than the string.
	raw := c.optPayloadAddr("%os", payloadLT)
	c.sb.WriteString(fmt.Sprintf("  %%pp = bitcast i8* %s to %s*\n", raw, payloadLT))
	c.sb.WriteString(fmt.Sprintf("  %%p = load %s, %s* %%pp\n", payloadLT, payloadLT))
	c.sb.WriteString(body)
	c.sb.WriteString("  ret void\n}\n")
}

// emitTxtFromStr lowers `OpTxtFromStr`: copy a %str-long's bytes into the
// fixed 256-byte %txt buffer (data[0..min(len,255)]) and store the length as
// an i8. The destination %txt slot is pre-allocated by the prologue. This is
// the MIR analogue of the legacy str->txt conversion in build/llvm/stmt.go.
// emitStrFromVec lowers `OpStrFromVec`: reinterpret a []byte (%vec) as a str
// (%str-long). Both layouts are { i64 len, i64 cap, <data> }; only the data
// field differs (i64 vs i8*), so the conversion copies len/cap verbatim and
// inttoptr's the pointer. Legacy stores the read-file %vec straight into the
// %str-long slot, so nolang sees the file's bytes as text — `data str =
// fs.read-file(path)` (tests/mem-safety/bug12-builtin-slice-to-str.no).
//
// The result ALIASES the slice's buffer (no copy), exactly like legacy. The
// caller must not drop both.
func (c *codegen) emitStrFromVec(inst *Inst) error {
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("str-from-vec dst has no slot in func %d", c.cf)
		return fmt.Errorf("str-from-vec dst slot")
	}
	srcT, srcV := c.loadVal(inst.Args[0])
	if srcT == dstLT {
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, srcV, dstLT, dstSlot))
		return nil
	}
	if srcT != "%vec" || dstLT != "%str-long" {
		c.fail("str-from-vec: cannot convert %s to %s in func %d", srcT, dstLT, c.cf)
		return fmt.Errorf("str-from-vec: bad types %s -> %s", srcT, dstLT)
	}
	c.loadSeq++
	lv := fmt.Sprintf("%%svl%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 0\n", lv, srcV))
	c.loadSeq++
	cv := fmt.Sprintf("%%svc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 1\n", cv, srcV))
	c.loadSeq++
	dv := fmt.Sprintf("%%svd%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", dv, srcV))
	c.loadSeq++
	pv := fmt.Sprintf("%%svp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", pv, dv))
	c.loadSeq++
	s0 := fmt.Sprintf("%%svs%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", s0, lv))
	c.loadSeq++
	s1 := fmt.Sprintf("%%svs%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", s1, s0, cv))
	c.loadSeq++
	s2 := fmt.Sprintf("%%svs%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", s2, s1, pv))
	c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", s2, dstSlot))
	return nil
}

func (c *codegen) emitTxtFromStr(inst *Inst) error {
	dstSlot := c.valSlot[inst.Dst] // %txt*
	if dstSlot == "" {
		c.fail("txt-from-str dst has no slot in func %d", c.cf)
		return fmt.Errorf("txt dst slot")
	}
	srcT, srcV := c.loadVal(inst.Args[0]) // %str-long
	c.loadSeq++
	sl := fmt.Sprintf("%%tls%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", sl, srcT, srcV))
	// clamp to 255 (txt is bounded at 255 bytes)
	c.loadSeq++
	tgt := fmt.Sprintf("%%tgt%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ugt i64 %s, 255\n", tgt, sl))
	c.loadSeq++
	cl := fmt.Sprintf("%%tlc%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 255, i64 %s\n", cl, tgt, sl))
	// source data pointer (field 2 of %str-long)
	c.loadSeq++
	dp := fmt.Sprintf("%%tdp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", dp, srcT, srcV))
	// GEP to txt.data[0]
	c.loadSeq++
	dx := fmt.Sprintf("%%txd%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0, i64 0\n", dx, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", dx, dp, cl))
	// store len byte (i8)
	c.loadSeq++
	l8 := fmt.Sprintf("%%tl8%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", l8, cl))
	c.loadSeq++
	lx := fmt.Sprintf("%%txl%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 1\n", lx, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  store i8 %s, i8* %s\n", l8, lx))
	return nil
}

func (c *codegen) emitCall(f *Function, inst *Inst) error {
	callee := inst.Sym
	if os.Getenv("NOLANG_MIR_DEBUG") != "" && (callee == "i64-to-str" || callee == "number.i64-to-str") {
		argT := ""
		if len(inst.Args) > 0 {
			argT, _ = c.loadVal(inst.Args[0])
		}
		fmt.Fprintf(os.Stderr, "[mir-dbg] emitCall callee=%q argT=%q nargs=%d\n", callee, argT, len(inst.Args))
	}
	// Indirect call through a fn-typed local/param (e.g. `setup()` where
	// `setup` has type `test-cb`). The callee is a value holding a function
	// pointer; load it and call through it, reusing the shared by-reference
	// argument/result emission (emitCallBody). Handled before every other
	// branch — including the `callee == ""` guard, since an indirect call
	// legitimately has an empty Sym (the callee is a value, not a name).
	if inst.Callee != NoVal {
		return c.emitIndirectCall(f, inst)
	}
	if callee == "" {
		c.fail("call without callee in func %s", f.Name)
		return fmt.Errorf("no callee in %s", f.Name)
	}
	// `print` is special-cased (not a regular builtin dispatch) and must be
	// checked BEFORE lookupBuiltin, because `print` is also registered in the
	// builtin table and would otherwise be mis-routed to emitBuiltin.
	if callee == "print" || callee == "println" {
		// Variadic print, matching legacy print(a, b, c) -> "a b c\n": each
		// arg is written space-separated, with a single trailing newline. Void
		// (void-call) arguments are skipped (call for side effects only).
		for i, a := range inst.Args {
			argT, argV := c.loadVal(a)
			if argV == "" || argT == "void" {
				continue
			}
			if i > 0 {
				c.sb.WriteString("  call void @print_space()\n")
			}
			if isOptionType(argT) {
				// Option print: emit the inner value (or "nil"), matching the
				// legacy backend, NOT the discriminant tag. The dedicated
				// print_option helper handles the flat scalar %option = { i64
				// tag, i64 data } by reading the inner field; for per-payload
				// inline option payloads the bytes are by-value, so we
				// peel field 1 and print it with the matching scalar printer —
				// the payload is NOT always i64 (a ?str option carries a
				// %str-long payload, a ?User option a %User struct payload), so
				// routing to print_i64 unconditionally is wrong and trips the
				// LLVM verifier (e.g. `%optpl19` defined as %str-long but
				// expected i64 at `call @print_i64`).
				// The element type is not part of `%option` any more, so it is
				// recovered from the nolang type table and the print is routed to
				// the helper generated for that payload type. A `?bool` must
				// print "true"/"false" (@print_option_bool); every other
				// printable payload goes through @print_option_<payload>, which
				// performs the nil-tag check and prints "nil" or the payload
				// inside its OWN function body — emitting the branch inline here
				// (mid-function, inside emitCall) breaks LLVM verification.
				if c.optionElemKind(a) == KindBool {
					c.sb.WriteString(fmt.Sprintf("  call void @print_option_bool(%%option %s)\n", argV))
					continue
				}
				if helper, ok := c.optPrintHelper[c.optPayloadLTOf(a)]; ok {
					c.sb.WriteString(fmt.Sprintf("  call void %s(%%option %s)\n", helper, argV))
					continue
				}
				// Payload type (%vec / user-struct / fixed-array / boxed) has no
				// scalar printer in MIR yet; legacy prints via to-str. Skip
				// rather than crash the verifier.
				c.fail("print of option payload type %s unsupported in func %s", c.optPayloadLTOf(a), f.Name)
				continue
			}
			switch argT {
			case "%str-long":
				c.sb.WriteString(fmt.Sprintf("  call void @print_str(%s %s)\n", argT, argV))
			case "%txt":
				// Build a transient %str-long { len, cap=len, data=&txt.data[0] }.
				slot := c.valSlot[a]
				if slot == "" {
					c.fail("print(txt) receiver has no slot in func %s", f.Name)
					continue
				}
				c.loadSeq++
				lGEP := fmt.Sprintf("%%ptlg%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 1\n", lGEP, slot))
				c.loadSeq++
				lLd := fmt.Sprintf("%%ptll%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", lLd, lGEP))
				c.loadSeq++
				l64 := fmt.Sprintf("%%ptl6%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", l64, lLd))
				c.loadSeq++
				dGEP := fmt.Sprintf("%%ptdg%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0, i64 0\n", dGEP, slot))
				c.loadSeq++
				dPtr := fmt.Sprintf("%%ptdp%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = bitcast [255 x i8]* %s to i8*\n", dPtr, dGEP))
				c.loadSeq++
				r0 := fmt.Sprintf("%%ptr0%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", r0, l64))
				c.loadSeq++
				r1 := fmt.Sprintf("%%ptr1%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", r1, r0, l64))
				c.loadSeq++
				r2 := fmt.Sprintf("%%ptr2%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", r2, r1, dPtr))
				c.sb.WriteString(fmt.Sprintf("  call void @print_str(%%str-long %s)\n", r2))
			case "i64":
				c.sb.WriteString(fmt.Sprintf("  call void @print_i64(%s %s)\n", argT, argV))
			case "i8":
				// byte/u8 and i8 share the LLVM i8 representation, so the
				// static nolang type decides the extension: unsigned byte/u8
				// zero-extends (0xFF prints 255), signed i8 sign-extends (0xFF
				// prints -1). Without this split every i8 slot printed unsigned,
				// so a module-level `s i8 = -1` came out as 255.
				ext := "zext"
				if c.rawTypeOf(a) == "i8" {
					ext = "sext"
				}
				c.loadSeq++
				z := fmt.Sprintf("%%ptz%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = %s i8 %s to i64\n", z, ext, argV))
				c.sb.WriteString(fmt.Sprintf("  call void @print_i64(i64 %s)\n", z))
			case "double":
				c.sb.WriteString(fmt.Sprintf("  call void @print_double(%s %s)\n", argT, argV))
			case "i1":
				c.sb.WriteString(fmt.Sprintf("  call void @print_bool(%s %s)\n", argT, argV))
			default:
				// Unknown scalar type: skip rather than fail the build (matches
				// legacy's graceful handling of print of exotic values).
				c.fail("print unsupported arg type %s in func %s", argT, f.Name)
			}
		}
		c.sb.WriteString("  call void @print_nl()\n")
		return nil
	}
	// Synthetic raw-write callees, emitted ONLY by the named-format lowering
	// (lowerNamedFormat in hir2mir.go). They write ONE value with no separator
	// and no trailing newline — which is exactly what a format segment needs
	// and what `print` above cannot do (it joins its variadic arguments with
	// spaces and appends a newline). The leading `$` is not a legal nolang
	// identifier character, so these names can never collide with a real
	// function or shadow `lookupBuiltin`.
	if rw, ok := rawWriteFns[callee]; ok {
		if rw == "" {
			c.sb.WriteString("  call void @" + callee[1:] + "()\n")
			return nil
		}
		if len(inst.Args) == 0 || inst.Args[0] == NoVal {
			c.fail("%s without argument in func %s", callee, f.Name)
			return fmt.Errorf("%s: missing argument", callee)
		}
		argT, argV := c.loadVal(inst.Args[0])
		if argV == "" || argT == "void" {
			return nil
		}
		if argT != rw {
			c.fail("%s expects %s, got %s in func %s", callee, rw, argT, f.Name)
			return fmt.Errorf("%s: argument type mismatch", callee)
		}
		c.sb.WriteString(fmt.Sprintf("  call void @%s(%s %s)\n", callee[1:], rw, argV))
		return nil
	}
	// Synthetic str concat, emitted only by the named-format lowering to build
	// the returned string of format()/sprintf() (legacy: concatStrLongPtrs).
	if callee == "$str_concat" {
		if len(inst.Args) < 2 {
			c.fail("$str_concat needs 2 arguments in func %s", f.Name)
			return fmt.Errorf("$str_concat: bad arity")
		}
		aT, aV := c.loadVal(inst.Args[0])
		bT, bV := c.loadVal(inst.Args[1])
		if aT != "%str-long" || bT != "%str-long" {
			c.fail("$str_concat got %s/%s in func %s", aT, bT, f.Name)
			return fmt.Errorf("$str_concat: argument type mismatch")
		}
		c.loadSeq++
		r := fmt.Sprintf("%%cat%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @_mir_str_concat(%%str-long %s, %%str-long %s)\n", r, aV, bV))
		if len(inst.Results) > 0 && inst.Results[0] > NoVal {
			if slot := c.valSlot[inst.Results[0]]; slot != "" {
				c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", r, slot))
			}
		}
		return nil
	}
	// `str.byte` / `txt.byte` are raw-byte accessors (`s.byte(i)` in source):
	// they are NOT real functions and have no body in the HIR package. Legacy
	// codegen expands them inline via generateRawByteAt (GEP+load+zext on the
	// underlying byte buffer). The MIR lowering keeps the qualified callee name
	// because `byte` carries no builtin-table entry (ForwardFunc), so we must
	// special-case it here — exactly like `print` above — instead of letting it
	// fall through to lookupBuiltin (which would raise "unknown callee str.byte").
	if callee == "str.byte" || callee == "txt.byte" {
		return c.emitBuiltinRawByteAt(f, inst)
	}
	// `.zero()` is a receiver-mutating method (`data.zero()` / `buf.zero()`)
	// with NO body in the HIR package — the legacy backend rewrites it to
	// `<recvType>.zero(recv)` and intercepts the suffix to emit llvm.memset on
	// the receiver's backing buffer. MIR mirrors that here: any callee ending
	// in `.zero` that is not a real Nolang function is lowered to a memset of
	// the receiver (inst.Args[0]) — a fixed [N x E] array zeroes its whole
	// by-value slot, a []E slice zeroes len*stride bytes of its heap buffer.
	if strings.HasSuffix(callee, ".zero") && !c.mod.KnownFuncs[callee] {
		return c.emitBuiltinArrZero(inst)
	}
	// `i64.to-str` is the std method form of the i64-to-str conversion helper
	// (defined in number.no as `i64.to-str = () (out str) { out = i64-to-str(.) }`).
	// The body delegates to `i64-to-str`, which the codegen already handles below.
	// When the HIR package's `i64.to-str` wrapper is not reachable (e.g. a
	// standalone script that does not pull in the number module's to-str method),
	// the callee falls through here as "unknown callee i64.to-str". Rewrite it to
	// the underlying `i64-to-str` so the conversion is emitted inline, matching
	// legacy (which resolves i64.to-str → i64-to-str through its method table).
	// The same applies to u64.to-str → u64-to-str, i32.to-str → i32-to-str, etc.
	if strings.HasSuffix(callee, ".to-str") {
		base := strings.TrimSuffix(callee, ".to-str")
		if isIntegerMIRType(base) || base == "u64" || base == "u32" || base == "u16" || base == "u8" {
			// Only rewrite to the bare-name form (e.g. `i64.to-str` →
			// `i64-to-str`) when the method form is NOT a real function in
			// the module. When the method IS in the module (e.g.
			// `char.to-str` defined in std/char.no, or `i64.to-str` defined
			// in std/number.no), rewriting would lose the link to the
			// lowered function and report "unknown callee".
			rewritten := base + "-to-str"
			_, hasMethod := c.mod.FuncByName[callee]
			_, hasBare := c.mod.FuncByName[rewritten]
			if !hasMethod && !hasBare {
				callee = rewritten
			} else if hasBare && !hasMethod {
				callee = rewritten
			}
			// else: keep callee as-is (the method form is in the module)
		}
	}
	// `bool-to-str` is the ForwardFunc of the builtin `bool.to-str`. Lowering
	// (hir2mir) emits the callee as `bm.ForwardFunc` ("bool-to-str") rather
	// than the MethodName ("bool.to-str"), so lookupBuiltin — which matches
	// by MethodName — cannot find it. Rewrite back to the MethodName form so
	// the builtin dispatch below resolves it to emitBuiltinBoolToStr.
	if callee == "bool-to-str" {
		callee = "bool.to-str"
	}
	// `number.i64-to-str` / `i64-to-str` are hardcoded inside the generic
	// `[]t.to-str` / `[n]t.to-str` std templates (`number.i64-to-str(.[i])`).
	// For a non-i64 element type (txt, u64, ...) that call is a type mismatch
	// (i64_to_str expects i64). The MIR value is typed generically as `t`, so
	// the concrete element type is only recoverable here at codegen, where the
	// argument's real LLVM type is known. Redirect to the element's own
	// `.to-str` method (e.g. %txt -> txt.to-str) so the conversion is correct.
	// The target method is enqueued by hir2mir whenever it sees this callee.
	if callee == "i64-to-str" || callee == "number.i64-to-str" {
		if len(inst.Args) > 0 {
			if argT, argV := c.loadVal(inst.Args[0]); argT != "" && argT != "i64" {
				// A `str` element inside the generic `[]t.to-str` / `[n]t.to-str`
				// template (`number.i64-to-str(.[i])`) is a string, not an
				// integer. Legacy codegen formats it by its LENGTH — the first
				// i64 field of %str-long — so `['a','b','c'].str-to-str()`
				// yields "[1, 1, 1]". Extract field 0 and feed it to @i64_to_str
				// to match that legacy behavior exactly; otherwise the whole
				// %str-long struct is passed to @i64_to_str (which expects a bare
				// i64) -> opt-verify type mismatch -> MIR build failure (bug #2b).
				if argT == "%str-long" {
					c.loadSeq++
					lenR := fmt.Sprintf("%%istr%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", lenR, argT, argV))
					c.loadSeq++
					cres := fmt.Sprintf("%%icr%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = alloca %%str-long\n", cres))
					c.sb.WriteString(fmt.Sprintf("  call void @i64_to_str(i64 %s, %%str-long* %s)\n", lenR, cres))
					if len(inst.Results) > 0 && inst.Results[0] > NoVal {
						if rlt, _ := c.ptype(inst.Results[0]); rlt != "void" && c.valSlot[inst.Results[0]] != "" {
							c.loadSeq++
							ld := fmt.Sprintf("%%icl%d", c.loadSeq)
							c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", ld, rlt, rlt, cres))
							c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", rlt, ld, rlt, c.valSlot[inst.Results[0]]))
						}
					}
					return nil
				}
				if method := c.elemToStrMethod(argT); method != "" {
					// Guard against self-redirection (mirrors hir2mir.go). When
					// a `X.to-str` method (X = i8/i16/i32) delegates to
					// `i64-to-str(.)` in its body, rewriting to `X.to-str` would
					// recurse infinitely. Skip the rewrite so the call resolves
					// to the real `i64-to-str` helper (i64_to_str_1).
					if method != f.Name {
						if _, ok := c.mod.FuncByName[method]; ok {
							inst.Sym = method
							return c.emitCall(f, inst)
						}
					}
				}
			}
		}
		// When the argument is already i64 (the common case: `i.to-str()`
		// where i is a loop counter), emit @str_from_i64 directly instead of
		// falling through to "unknown callee i64-to-str" (the function is
		// defined in std/number.no but may not be reachable from a script
		// that does not import the number module's method table).
		if len(inst.Args) > 0 {
			if argT, argV := c.loadVal(inst.Args[0]); argT == "i64" {
				if len(inst.Results) > 0 && inst.Results[0] > NoVal {
					if rlt, _ := c.ptype(inst.Results[0]); rlt != "void" && c.valSlot[inst.Results[0]] != "" {
						c.loadSeq++
						tmp := fmt.Sprintf("%%its%d", c.loadSeq)
						c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_i64(i64 %s)\n", tmp, argV))
						c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", tmp, c.valSlot[inst.Results[0]]))
					}
				}
				// #85 guard: if Results is empty the lowering failed to
				// allocate a destination — the call would silently vanish.
				// Report it instead of returning nil (which hides the bug).
				if len(inst.Results) == 0 || inst.Results[0] <= NoVal {
					c.fail("i64-to-str: no result slot (lowering failure in func %s)", f.Name)
				}
				return nil
			}
		}
	}
	// Builtin dispatch: any callee registered in the builtin table (and not the
	// special-cased `print` above) is emitted directly here — allocation
	// builtins, LLVM intrinsics, FFI-free forwards — rather than as a regular
	// module function call. This clears the whole `callee:<name>` gap class
	// (with-cap / with-len / eprint / sqrt / ...).
	bm, exact, ok := lookupBuiltin(callee)
	if ok {
		// A fallback (bare-name) match must not shadow a real Nolang function
		// that merely shares the bare builtin name. `fs.read-dir` is a module
		// function; the global builtin `read-dir` would otherwise be emitted
		// for it and fail. When the qualified callee is a known function,
		// route it to the normal function-call path below.
		if exact || !c.mod.KnownFuncs[callee] {
			if err := c.emitBuiltin(f, inst, bm); err != nil {
				return err
			}
			return nil
		}
	}
	cid, ok := c.mod.FuncByName[callee]
	if !ok {
		c.fail("unknown callee %s in func %s", callee, f.Name)
		return fmt.Errorf("unknown callee %s in func %s", callee, f.Name)
	}
	cf := c.mod.Func(cid)
	if cf == nil {
		c.fail("callee %s unresolved in func %s", callee, f.Name)
		return fmt.Errorf("callee nil")
	}
	// User-declared FFI extern (`#{c} name = (...) (...)`) uses the C ABI,
	// NOT the Nolang by-reference ABI. Route it to the FFI marshaller.
	if cf.IsExtern {
		return c.emitExternCall(f, inst, cf)
	}
	return c.emitCallBody(f, cf, inst, "@"+c.fname[cid])
}

// mirFFITypeToLLVM maps a Nolang FFI type name to its C-side LLVM type for the
// `declare` signature (mirrors build/llvm ffiTypeToLLVM). The Nolang *storage*
// type differs (str stores as %str-long, ptr as i64), so the marshalling in
// emitExternCall consults the raw Nolang type, not this mapping.
func mirFFITypeToLLVM(t string) string {
	switch t {
	case "i64":
		return "i64"
	case "i32", "bool":
		return "i32"
	case "f64":
		return "double"
	case "str", "ptr":
		return "i8*"
	case "pptr":
		return "i8**"
	case "ppptr":
		return "i8***"
	default:
		return "i64"
	}
}

// emitExternCall lowers a call to a user-declared FFI extern function
// (`#{c} name = (...) (...)`) using the C calling convention, replicating the
// legacy build/llvm callExtern ABI exactly so output matches byte-for-byte:
//
//   - inputs:  str -> NUL-terminated i8* (@str_cstr); i64 -> i64; i32/bool ->
//     trunc to i32; f64 -> double; ptr -> inttoptr i64 to i8*; pptr/ppptr ->
//     alloca i8*/i8** passed by pointer (written back after the call).
//   - outputs: i64 -> i64; i32 -> sext to i64; f64 -> double; str -> build an
//     owned %str-long via @str_from_cstr (strlen + malloc copy, so a static
//     C string is not freed as heap); ptr/pptr/ppptr -> ptrtoint to i64;
//     bool -> icmp ne 0 then zext to i64; void -> nothing.
//
// The C symbol name maps '-' to '_' and drops a leading '_' (private marker),
// matching externSymbolRef.
func (c *codegen) emitExternCall(f *Function, inst *Inst, cf *Function) error {
	cName := cf.Name
	if strings.HasPrefix(cName, "_") {
		cName = cName[1:]
	}
	cName = strings.ReplaceAll(cName, "-", "_")
	sym := "@" + cName

	// Emit the `declare` once (MIR has no separate declaration pass). Function.
	// Params holds ValueIDs; the Nolang FFI type name lives on each param's type.
	declParams := make([]string, 0, len(cf.Params))
	for _, p := range cf.Params {
		raw := ""
		if val := c.mod.Value(p); val != nil {
			if t := c.mod.Type(val.Type); t != nil {
				raw = t.Raw
			}
		}
		declParams = append(declParams, mirFFITypeToLLVM(raw))
	}
	retRaw := ""
	if len(cf.Results) > 0 {
		if t := c.mod.Type(cf.Results[0]); t != nil {
			retRaw = t.Raw
		}
	}
	retLLVM := "void"
	if retRaw != "" {
		retLLVM = mirFFITypeToLLVM(retRaw)
	}
	c.decl(fmt.Sprintf("declare %s %s(%s)", retLLVM, sym, strings.Join(declParams, ", ")))

	// Marshal arguments (Nolang storage -> C ABI).
	var callArgs []string
	var toFree []string
	type pptrSlot struct {
		slotReg string
		argVal  ValueID
		levels  int // 1 = pptr (i8**), 2 = ppptr (i8***)
	}
	var pptrs []pptrSlot
	for i, p := range cf.Params {
		if i >= len(inst.Args) {
			break
		}
		raw := ""
		if val := c.mod.Value(p); val != nil {
			if t := c.mod.Type(val.Type); t != nil {
				raw = t.Raw
			}
		}
		av, avV := c.loadVal(inst.Args[i])
		switch raw {
		case "str":
			cs := c.cstrOf(inst.Args[i])
			if cs == "" {
				c.fail("extern %s: cannot marshal str arg %d", cf.Name, i)
				return fmt.Errorf("extern str arg")
			}
			callArgs = append(callArgs, "i8* "+cs)
			toFree = append(toFree, cs)
		case "i64":
			callArgs = append(callArgs, "i64 "+avV)
		case "i32", "bool":
			c.loadSeq++
			reg := fmt.Sprintf("%%exti%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", reg, av, avV))
			callArgs = append(callArgs, "i32 "+reg)
		case "f64":
			callArgs = append(callArgs, "double "+avV)
		case "ptr":
			c.loadSeq++
			reg := fmt.Sprintf("%%extp%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", reg, avV))
			callArgs = append(callArgs, "i8* "+reg)
		case "pptr":
			c.loadSeq++
			slot := fmt.Sprintf("%%extpp%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca i8*\n", slot))
			c.sb.WriteString(fmt.Sprintf("  store i8* inttoptr (i64 %s to i8*), i8** %s\n", avV, slot))
			callArgs = append(callArgs, "i8** "+slot)
			pptrs = append(pptrs, pptrSlot{slotReg: slot, argVal: inst.Args[i], levels: 1})
		case "ppptr":
			c.loadSeq++
			slot := fmt.Sprintf("%%extpp%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca i8**\n", slot))
			c.sb.WriteString(fmt.Sprintf("  store i8** inttoptr (i64 %s to i8**), i8*** %s\n", avV, slot))
			callArgs = append(callArgs, "i8*** "+slot)
			pptrs = append(pptrs, pptrSlot{slotReg: slot, argVal: inst.Args[i], levels: 2})
		default:
			callArgs = append(callArgs, "i64 "+avV)
		}
	}

	// Emit the C call.
	var callReg string
	if retLLVM == "void" {
		c.sb.WriteString(fmt.Sprintf("  call void %s(%s)\n", sym, strings.Join(callArgs, ", ")))
	} else {
		c.loadSeq++
		callReg = fmt.Sprintf("%%extr%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", callReg, retLLVM, sym, strings.Join(callArgs, ", ")))
	}

	// Write back pptr/ppptr outputs (load the i8*/i8** and store the i64 back
	// into the caller's slot, mirroring legacy's ptrtoint store-back).
	for _, ps := range pptrs {
		elemLT := "i8*"
		if ps.levels == 2 {
			elemLT = "i8**"
		}
		c.loadSeq++
		ld := fmt.Sprintf("%%extpl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", ld, elemLT, elemLT, ps.slotReg))
		c.loadSeq++
		pi := fmt.Sprintf("%%extpi%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pi, elemLT, ld))
		if aslot := c.valSlot[ps.argVal]; aslot != "" {
			c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", pi, aslot))
		}
	}

	// Convert the C return value into Nolang storage.
	if retLLVM == "void" || len(inst.Results) == 0 {
		// No return to convert; the NUL-terminated copies are no longer needed.
		for _, cs := range toFree {
			c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", cs))
		}
		return nil
	}
	rv := inst.Results[0]
	if rv <= NoVal {
		for _, cs := range toFree {
			c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", cs))
		}
		return nil
	}
	rslot := c.valSlot[rv]
	if rslot == "" {
		for _, cs := range toFree {
			c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", cs))
		}
		return nil
	}
	switch retRaw {
	case "str":
		c.loadSeq++
		strReg := fmt.Sprintf("%%extrs%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_cstr(i8* %s)\n", strReg, callReg))
		c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", strReg, rslot))
	case "i64":
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", callReg, rslot))
	case "i32":
		c.loadSeq++
		sext := fmt.Sprintf("%%extrs%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", sext, callReg))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", sext, rslot))
	case "f64":
		c.sb.WriteString(fmt.Sprintf("  store double %s, double* %s\n", callReg, rslot))
	case "ptr", "pptr", "ppptr":
		c.loadSeq++
		pi := fmt.Sprintf("%%extrp%d", c.loadSeq)
		llvmRet := mirFFITypeToLLVM(retRaw)
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pi, llvmRet, callReg))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", pi, rslot))
	case "bool":
		c.loadSeq++
		cmp := fmt.Sprintf("%%extrb%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", cmp, retLLVM, callReg))
		c.loadSeq++
		ze := fmt.Sprintf("%%extrz%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", ze, cmp))
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ze, rslot))
	default:
		c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", callReg, rslot))
	}
	// The NUL-terminated argument copies are only safe to free once the return
	// (if any) has been converted: a str return points into the copy's buffer,
	// so freeing first would read freed memory in @str_from_cstr.
	for _, cs := range toFree {
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", cs))
	}
	return nil
}

// buildVecViewFromArray emits a borrow slice view over a fixed stack array and
// returns the argument string ("%vec* %slot") to pass. The view is
// %vec{ len=N, cap=0, data=&arr[0] }: cap==0 marks it as non-owning so
// @vec_free skips it (the backing store is stack memory, not a heap alloc).
// N comes from arraySizeOf(argT) (argT like "[4 x i64]"). Used when a fixed
// array is passed where a %vec (slice) is expected — both for ordinary
// arguments (emitCallBody's owned branch) and for the method receiver (so
// `[1,2,3,4].to-str()` hands the array to []i64.to-str as a real slice view
// instead of reinterpreting the array bytes as a %vec, which crashes).
func (c *codegen) buildVecViewFromArray(argT, arrSlot string) string {
	s2 := c.vecViewValue(argT, arrSlot)
	c.loadSeq++
	slot := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = alloca %%vec\n", slot))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	return "%vec* " + slot
}

// vecViewValue emits a borrow slice view over a fixed stack array and returns
// the %vec{ len=N, cap=0, data=&arr[0] } SSA register name (cap==0 marks the
// view as non-owning so @vec_free skips the stack backing store). It is the
// value-producing counterpart of buildVecViewFromArray (which additionally
// spills the view to a temporary slot for pass-by-pointer call arguments): use
// this when the %vec must be stored inline (e.g. a struct field of slice type
// assigned a fixed-array literal `c.data = [1,2,3]`, where storing the raw
// array bytes into the %vec slot would make len/data read garbage).
func (c *codegen) vecViewValue(argT, arrSlot string) string {
	n := int64(0)
	if m, ok := arraySizeOf(argT); ok {
		n = m
	}
	c.loadSeq++
	dataPtr := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", dataPtr, argT, argT, arrSlot))
	c.loadSeq++
	pt := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pt, dataPtr))
	c.loadSeq++
	s0 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %d, 0\n", s0, n))
	c.loadSeq++
	s1 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 1\n", s1, s0))
	c.loadSeq++
	s2 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, pt))
	return s2
}

// vecOwnedFromArray heap-allocates a %vec holding a memcpy'd copy of the N
// elements of the fixed stack array arrSlot (LLVM type argT = "[N x T]") and
// returns the %vec{len=N, cap=N, data=heap} SSA value. cap==len marks the vec
// OWNED: @vec_free releases the heap buffer when the owning binding is dropped.
// Unlike the cap=0 borrow view (vecViewValue) this is valid when the resulting
// slice OUTLIVES the current frame — stored into a struct field that is
// returned/moved out — where a borrow over stack memory would dangle once the
// frame is popped (garbage reads / SIGSEGV). Only safe for trivially-copyable
// elements (memcpy would alias a nested heap for owned element types).
func (c *codegen) vecOwnedFromArray(argT, arrSlot string) string {
	n := int64(0)
	if m, ok := arraySizeOf(argT); ok {
		n = m
	}
	elemT := ""
	if i := strings.Index(argT, " x "); i >= 0 {
		elemT = strings.TrimSuffix(argT[i+3:], "]")
	}
	// Element size for the malloc and the memcpy. Do NOT use elemStride: it
	// falls back to 8 for any type it does not know, so coercing `[N x struct]`
	// (or `[N x %option]`, or a nested fixed array) to a slice allocated
	// and copied only 8 bytes per element — a heap overflow, and the copy
	// truncated every element. typeSizeOperand asks LLVM, which is the same
	// authority the element GEPs below use.
	//
	// The statically known cases keep emitting the SAME decimal literal as
	// before, so their IR stays byte-identical.
	var total string
	switch {
	case elemT == "":
		total = fmt.Sprintf("%d", n*8) // no element type parsed — old fallback
	default:
		if sz, ok := c.mirStaticTypeSize(elemT); ok {
			total = fmt.Sprintf("%d", n*sz)
		} else {
			tt := c.treg("bva")
			c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %d, %s\n", tt, n, c.typeSizeOperand(elemT)))
			total = tt
		}
	}
	buf := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", buf, total))
	src := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", src, argT, argT, arrSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", buf, src, total))
	dp := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dp, buf))
	s0 := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %d, 0\n", s0, n))
	s1 := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %d, 1\n", s1, s0, n))
	s2 := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dp))
	return s2
}

// vecFromArraySink selects the array -> slice coercion for a sink that may
// escape the frame: a heap-owned copy for trivially-copyable elements, falling
// back to the cap=0 borrow view for element types whose nested heap would be
// aliased by a raw memcpy (needs a deep clone that does not exist yet).
func (c *codegen) vecFromArraySink(argT, arrSlot string) string {
	elemT := ""
	if i := strings.Index(argT, " x "); i >= 0 {
		elemT = strings.TrimSuffix(argT[i+3:], "]")
	}
	switch elemT {
	case "i8", "i1", "i64", "double":
		return c.vecOwnedFromArray(argT, arrSlot)
	default:
		return c.vecViewValue(argT, arrSlot)
	}
}

// buildVecViewFromValues emits a borrow slice view over a freshly-allocated
// stack array holding the given element values, and returns the "%vec* slot"
// argument string to pass. This is the call-site counterpart of
// buildVecViewFromArray: where that one wraps an EXISTING fixed array, this
// one collects a list of scalar call arguments (the spread elements of a
// variadic generic call like `number.max(10, 20)`) into a real %vec so the
// callee (whose variadic parameter is `[]T`) indexes them correctly. cap==0
// marks the view as non-owning so @vec_free skips the stack backing store.
func (c *codegen) buildVecViewFromValues(elemLLT string, argLLVMs []string) string {
	n := int64(len(argLLVMs))
	arrType := fmt.Sprintf("[%d x %s]", n, elemLLT)
	c.loadSeq++
	arr := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", arr, arrType))
	for i, av := range argLLVMs {
		c.loadSeq++
		elp := fmt.Sprintf("%%cav%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %d\n", elp, arrType, arrType, arr, i))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemLLT, av, elemLLT, elp))
	}
	c.loadSeq++
	dataPtr := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", dataPtr, arrType, arrType, arr))
	c.loadSeq++
	pt := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pt, dataPtr))
	c.loadSeq++
	s0 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %d, 0\n", s0, n))
	c.loadSeq++
	s1 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 1\n", s1, s0))
	c.loadSeq++
	s2 := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, pt))
	c.loadSeq++
	slot := fmt.Sprintf("%%cav%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = alloca %%vec\n", slot))
	c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	return "%vec* " + slot
}

// emitCallBody emits a call to calleeName using the by-reference ABI: every
// input parameter is passed as a pointer to the caller's slot, every result is
// passed as an out-pointer, and results are loaded back into inst.Results.
// Shared by direct calls (calleeName = "@funcname") and indirect calls through
// a fn-typed local (calleeName = the loaded function-pointer register). The
// signature cf supplies the by-value parameter/result MIR type IDs used to
// derive each argument's LLVM pointer type.
func (c *codegen) emitCallBody(f *Function, cf *Function, inst *Inst, calleeName string) error {
	var inParams, outParams []ValueID
	rpSet := map[ValueID]bool{}
	for _, rp := range cf.ResultParams {
		rpSet[rp] = true
	}
	for _, p := range cf.Params {
		if rpSet[p] {
			outParams = append(outParams, p)
		} else {
			inParams = append(inParams, p)
		}
	}
	var callArgs []string
	// Variadic call detection: a nolang variadic function (e.g. `max (a ..num)`)
	// is monomorphized with its spread parameter as a single []T slice (the
	// receiver). A call that supplies MORE scalar arguments than the function
	// has in-parameters collects those trailing arguments into a %vec and
	// passes it as the (last) variadic parameter. Without this, the trailing
	// args are dropped and the slice parameter receives the first arg's storage
	// as its %vec header -> the callee dereferences a garbage data pointer ->
	// SIGTRAP at runtime (test-number-generic). Only trigger when the last
	// in-param is actually a slice, so ordinary fixed-arity functions are
	// unaffected.
	//
	// Out-param actuals are NOT variadic spread elements. A caller may write a
	// named out-parameter explicitly as a trailing argument (`lcs(a, b, ops)`
	// for `lcs = (a []str, b []str) (ops []diff-op)`), in which case inst.Args
	// has exactly len(inParams)+len(outParams) entries. Counting those as spread
	// elements packs the INPUT arguments into a bogus %vec — and because the
	// freshly-lowered slice literal is still a fixed array (`[3 x %str-long]`)
	// while the packer stores into a `%cav` slot typed for its first element,
	// the emitted IR mixes `%vec` and `[3 x %str-long]` and LLVM's verifier
	// rejects the whole module (opt: "'%lv39' defined with type '%vec' but
	// expected '[3 x %str-long]'"). When the element type happens to agree the
	// IR is accepted and instead silently corrupts the slice. (lowerCallArgs
	// strips the same trailing args, but only for VARIADIC callees; a
	// non-variadic callee keeps them, which is why this has to be handled here
	// too.)
	//
	// The count is only ambiguous for a variadic callee, where n spread
	// elements look exactly like n out-param actuals (`number.max(10, 20)` has
	// 2 args, 1 spread in-param and 1 result param). cf.Variadic resolves it:
	// subtract the trailing args only for a non-variadic callee, where there is
	// no other way to have extras.
	// For a method, the first out-param is `self` (the receiver out-param).
	// It is passed as inst.Args[0]'s slot address — NOT as a freshly
	// allocated %cres — so that mutations propagate to the caller.  The
	// remaining out-params (if any) are real return values and use %cres.
	selfOut := 0
	if cf.IsMethod && len(outParams) > 0 {
		selfOut = 1 // the first out-param is self
	}
	nOut := len(outParams) - selfOut    // real out-params (excluding self)
	effArgs := len(inst.Args) - selfOut // inst.Args[0] is the receiver
	if !cf.Variadic && nOut > 0 && effArgs == len(inParams)+nOut {
		effArgs -= nOut
	}
	variadicLast := false
	if nIn := len(inParams); nIn >= 1 && effArgs > nIn {
		lastP := inParams[nIn-1]
		if pv := c.mod.Value(lastP); pv != nil {
			if pt := c.mod.Type(pv.Type); pt != nil && pt.Kind == KindSlice {
				variadicLast = true
			}
		}
	}
	// For a method, pass the receiver's slot address as the self out-param.
	if selfOut > 0 && len(inst.Args) > 0 {
		selfP := outParams[0]
		selfLT, _ := c.ptype(selfP)
		if selfLT == "" || selfLT == "void" {
			selfLT = "i64"
		}
		recvT, _ := c.ptype(inst.Args[0])
		// Lvalue projection: `j.pool.parse(s, 0)` — the receiver was READ OUT
		// of a struct field (or a container element), so its alloca holds a
		// COPY and every mutation the callee makes to `self` would be lost
		// (this is why json.parse / json.set-str silently did nothing). Pass
		// the address of the REAL field instead so `self` aliases it.
		rs := ""
		if p, _, projected := c.lvalueAddrOf(inst.Args[0]); projected && p != "" && os.Getenv("NOLANG_LV_RECV") == "" {
			rs = p
		} else if s, ok := c.valSlot[inst.Args[0]]; ok && s != "" {
			rs = s
		}
		// Only a genuine VIEW takes this path: the check is deliberately
		// narrow (exact `T*` receiver against a `T` self) so no existing
		// receiver shape can fall into it by accident.
		if recvT == selfLT+"*" && c.isViewValue(inst.Args[0]) {
			// VIEW receiver (`&T`): the value IS a `T*`, so its slot is a
			// `T**`. Passing the slot address would hand the callee a pointer
			// TO THE VIEW rather than to the borrowed value (a method called
			// through a view then read garbage). Load the stored pointer and
			// pass that — it is exactly the `%T*` the callee's `self` expects.
			if _, lv := c.loadVal(inst.Args[0]); lv != "" {
				callArgs = append(callArgs, selfLT+"* "+lv)
			}
		} else if rs != "" {
			// A fixed stack array ([N x T]) passed as the self out-param of a
			// %vec (slice) method (e.g. `arr.to-str()` where arr is [3]i64 and
			// the callee is []t.to-str) must NOT be passed raw: the callee
			// would reinterpret the array's element bytes as a
			// %vec{len,cap,data} and dereference a tiny integer as a pointer
			// -> SIGSEGV. Build a borrow slice view (len=N, cap=0,
			// data=&arr[0]) so the callee indexes correctly. This mirrors the
			// same coercion already done for ordinary in-param arguments
			// (see the plt=="%vec" && HasPrefix(argT,"[") branch below).
			if isOptionType(recvT) && selfLT != "" && !isOptionType(selfLT) {
				// `?T.method()` — an OPTION receiver against a method whose
				// `self` out-param is the INNER type. Passing the option's own
				// slot hands the callee an `%inner*` that actually points at
				// the option header, so its first field read lands on the TAG
				// instead of on the payload: with `?[]i64` out = [10,20,30],
				// `out.len()` returned 0 (the ok tag) rather than 3, because
				// %vec's field 0 is `len` while %option's field 0 is `tag`.
				// Peel to the payload address — optPayloadTypedAddr covers
				// both the inline and the boxed layout, exactly as
				// emitGetField already does for a `?T.field` read.
				callArgs = append(callArgs, selfLT+"* "+c.optPayloadTypedAddr(rs, selfLT, selfLT))
			} else if selfLT == "%vec" && strings.HasPrefix(recvT, "[") {
				callArgs = append(callArgs, c.buildVecViewFromArray(recvT, rs))
			} else {
				callArgs = append(callArgs, selfLT+"* "+rs)
			}
		} else {
			c.loadSeq++
			slot := fmt.Sprintf("%%cself%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, selfLT))
			c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %s\n", selfLT, selfLT, slot))
			callArgs = append(callArgs, selfLT+"* "+slot)
		}
	}
	for i, p := range inParams {
		// For a method, inst.Args[0] is the receiver (passed as self out-param
		// above).  Real arguments start at inst.Args[selfOut].
		//
		// argIdx is the ONLY correct index into inst.Args for the i-th input
		// parameter; `i` alone is off by one whenever selfOut > 0. Every branch
		// below that reaches for the argument's own slot must use argIdx —
		// reaching for inst.Args[i] there silently picks up the receiver
		// instead, which is exactly how `tls.conn.append-hs` came to be called
		// as append-hs(self, self): the []byte parameter received the receiver
		// pointer, the callee read it as a %vec header, and the process
		// segfaulted on a null data pointer (tests/test-tls.no).
		argIdx := i + selfOut
		if variadicLast && i == len(inParams)-1 {
			// Collect every remaining call argument into a borrow %vec view and
			// pass it as the variadic (slice) parameter. The element LLVM type
			// is taken from the first collected argument's loaded type.
			var argLLVMs []string
			elemLLT := "i64"
			for j := i; j < effArgs; j++ {
				at, av := c.loadVal(inst.Args[j+selfOut])
				if j == i && at != "" {
					elemLLT = at
				}
				argLLVMs = append(argLLVMs, av)
			}
			callArgs = append(callArgs, c.buildVecViewFromValues(elemLLT, argLLVMs))
			continue
		}
		plt, owned := c.ptype(p)
		if argIdx >= len(inst.Args) {
			// The HIR sometimes drops a trailing literal argument (e.g.
			// `.emit(RE-OP-ANY, 0, 0)` lowers with the second `0` missing because
			// the parser/semantic pass collapses identical consecutive literals).
			// Legacy codegen tolerates this by treating absent tail arguments as
			// their zero value, so mirror that to keep parity: pad with the
			// parameter's zero initializer (the dropped argument is always a
			// literal 0). This also covers the method-receiver edge cases without
			// killing the build under NOLANG_MIR=3.
			callArgs = append(callArgs, plt+" zeroinitializer")
			continue
		}
		argT, av := c.loadVal(inst.Args[argIdx])
		// Function-pointer argument to a fn-typed parameter. The callee declares
		// such a parameter BY REFERENCE as a `<sig>**` slot (see emitFunc's
		// fn-type param gate). We must therefore pass the *address* of a slot
		// holding the function pointer — not the pointer by value — otherwise the
		// call type-checks against `void(...)*` but the callee expects
		// `void(...)**` (yielding a malformed `void ()* void ()*` argument). Store
		// the loaded function pointer into a fresh `<sig>*` alloca and pass that
		// alloca's address, exactly like the owned-aggregate argument path.
		if pt := c.mod.Type(c.mod.Value(p).Type); pt != nil && pt.Kind == KindFunc {
			fnLT := c.llvmTypeOf(pt) // void (Param*, ...)*
			c.loadSeq++
			slot := fmt.Sprintf("%%fnarg%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, fnLT))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fnLT, av, fnLT, slot))
			callArgs = append(callArgs, fnLT+"* "+slot)
			continue
		}
		// Method receiver special handling is NOT needed here when self is
		// an out-param: the receiver is already passed as the self out-param
		// above.  For extern methods that still have self as a KParam, the
		// receiver is inst.Args[0] (argIdx == 0 when selfOut == 0).
		if cf.IsMethod && i == 0 && selfOut == 0 {
			if rs, ok := c.valSlot[inst.Args[0]]; ok && rs != "" {
				recvLT, recvOwned := c.ptype(inst.Args[0])
				if isOptionType(recvLT) {
					// The method is on the inner type T (resolveCallee strips
					// the leading '?' from the receiver type to form the callee),
					// but the receiver slot holds the whole ?T option. Pass the
					// payload, not the option: GEP into field 1 (the inline
					// payload) to obtain the inner T. Mirrors the legacy backend
					// for `it.method()` inside a match SOME-arm, where the
					// synthetic `it` is bound to the whole ?T option but typed
					// as the inner T. Without this the whole option struct is
					// passed where the callee expects the inner type — reading
					// the option's {tag, vec} as a %vec yields a bogus length
					// and crashes (trace/BPT trap).
					// optPayloadTypedAddr, not a raw GEP to field 1: a payload
					// larger than the slot is boxed, and field 1 then holds the
					// box POINTER rather than the inner value.
					pg := c.optPayloadTypedAddr(rs, plt, plt)
					if owned || byPointerLLVM(plt, owned) || recvOwned {
						callArgs = append(callArgs, plt+"* "+pg)
					} else {
						c.loadSeq++
						pv := fmt.Sprintf("%%optrcvv%d", c.loadSeq)
						c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", pv, plt, plt, pg))
						callArgs = append(callArgs, plt+" "+pv)
					}
					continue
				}
			}
		}
		if cf.IsMethod && i == 0 && selfOut == 0 && (owned || byPointerLLVM(plt, owned)) {
			// The receiver slot (c.valSlot[receiver]) is the address of the
			// caller's container/aggregate (a pointer to the alloca). Pass it
			// directly — `plt` is the value type and the "*" makes the argument a
			// pointer matching the callee's by-reference receiver param — so the
			// method writes through to the caller's value (self mutations
			// propagate). This is exactly the normal owned-param call form
			// (`%vec* %slot`) except we reuse the existing caller slot instead of
			// copying into a fresh %carg alloca (which would discard self
			// mutations). Covers non-owned aggregate receivers (fixed arrays,
			// non-owned structs) that are now also passed by pointer.
			//
			// EXCEPTION: when the receiver is the result of OpGetField (e.g.
			// `m.rows.push(x)` — receiver is `m.rows`), the temporary alloca slot
			// holds a *copy* of the field value. Passing that slot's pointer makes
			// the callee mutate the copy, and the struct field stays unchanged
			// (observed: `rows len: 0` after `push`). Instead, compute the GEP
			// address of the struct field directly and pass THAT, so mutations
			// propagate back into the struct.
			if defIID, ok := c.defInst[inst.Args[0]]; ok {
				if defInst := c.mod.Inst(defIID); defInst != nil && defInst.Op == OpGetField {
					// The getfield's receiver (the struct value) and field name.
					gfRecv := defInst.Args[0]
					gfFieldName := defInst.Str
					if gfRecvSlot, ok2 := c.valSlot[gfRecv]; ok2 && gfRecvSlot != "" {
						// Resolve the struct type and field index, mirroring
						// emitGetField's lookup but only the GEP part.
						gfRecvRaw := ""
						gfRecvLT := ""
						if fl := c.mod.Func(c.cf); fl != nil {
							if tid, ok3 := fl.LocalTypes[gfRecv]; ok3 {
								if t := c.mod.Type(tid); t != nil {
									gfRecvRaw = t.Raw
									gfRecvLT = c.llvmTypeOf(t)
								}
							}
						}
						if gfRecvLT == "" {
							if v := c.mod.Value(gfRecv); v != nil {
								if t := c.mod.Type(v.Type); t != nil {
									gfRecvRaw = t.Raw
									gfRecvLT = c.llvmTypeOf(t)
								}
							}
						}
						if gfRecvLT == "" {
							gfRecvLT = "%" + sanitize(gfRecvRaw)
						}
						// Strip option wrapper if needed (mirrors emitGetField).
						gfRecvRaw = strings.TrimPrefix(gfRecvRaw, "?")
						structKey := c.structKeyOf(gfRecvRaw)
						if structKey == "" {
							structKey = gfRecvRaw
						}
						// Try container pseudo-fields first (len/cap/data).
						if idx, ok3 := containerFieldIndex(gfFieldName); ok3 {
							if t := c.mod.Type(c.mod.Value(gfRecv).Type); t != nil &&
								(t.Kind == KindSlice || t.Kind == KindStr) {
								c.loadSeq++
								gep := fmt.Sprintf("%%gfrcv%d", c.loadSeq)
								c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, gfRecvLT, gfRecvLT, gfRecvSlot, idx))
								callArgs = append(callArgs, plt+"* "+gep)
								continue
							}
						}
						// Regular struct field.
						if idx, ok3 := c.mod.FieldIndex(structKey, gfFieldName); ok3 {
							c.loadSeq++
							gep := fmt.Sprintf("%%gfrcv%d", c.loadSeq)
							c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, gfRecvLT, gfRecvLT, gfRecvSlot, idx))
							callArgs = append(callArgs, plt+"* "+gep)
							continue
						}
						// Field lookup failed: fall through to the normal path.
					}
				}
			}
			//
			// Exception: a fixed stack array ([N x T]) passed as the receiver of
			// a %vec (slice) method (e.g. `[1,2,3,4].to-str()` → []i64.to-str)
			// must NOT be passed raw: the callee would reinterpret the array's
			// element bytes as a %vec{len,cap,data} and dereference a tiny
			// integer as a pointer -> SIGSEGV. Build a borrow slice view
			// (len=N, cap=0, data=&arr[0]) so the callee indexes correctly.
			if plt == "%vec" && strings.HasPrefix(argT, "[") {
				if rs, ok := c.valSlot[inst.Args[0]]; ok && rs != "" {
					callArgs = append(callArgs, c.buildVecViewFromArray(argT, rs))
					continue
				}
			}
			if rs, ok := c.valSlot[inst.Args[0]]; ok && rs != "" {
				callArgs = append(callArgs, plt+"* "+rs)
				continue
			}
		}
		if owned {
			if plt == "%vec" && strings.HasPrefix(argT, "[") {
				// Fixed array argument passed to a slice (vec) parameter: build a
				// %vec{len=N, cap=0, ptr=&arr[0]} so the callee sees a slice view
				// of the array in place (writes through the slice mutate the
				// array, matching nolang [N]T -> []T coercion). cap is 0 because the
				// backing store is stack memory, not a heap allocation: its drop must
				// NOT free the borrowed buffer (legacy @vec_free skips cap==0). N
				// comes from the array type; the data pointer is the address of
				// element 0.
				arrSlot := c.valSlot[inst.Args[argIdx]]
				var n int64
				if at := c.mod.Value(inst.Args[argIdx]); at != nil {
					if mtt := c.mod.Type(at.Type); mtt != nil && len(mtt.Sizes) > 0 {
						n = mtt.Sizes[0]
					}
				}
				if n == 0 {
					if m, ok := arraySizeOf(argT); ok {
						n = m
					}
				}
				c.loadSeq++
				dataPtr := fmt.Sprintf("%%cav%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", dataPtr, argT, argT, arrSlot))
				c.loadSeq++
				pt := fmt.Sprintf("%%cav%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pt, dataPtr))
				c.loadSeq++
				s0 := fmt.Sprintf("%%cav%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %d, 0\n", s0, n))
				c.loadSeq++
				s1 := fmt.Sprintf("%%cav%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 1\n", s1, s0))
				c.loadSeq++
				s2 := fmt.Sprintf("%%cav%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, pt))
				c.loadSeq++
				slot := fmt.Sprintf("%%carg%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = alloca %%vec\n", slot))
				c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
				callArgs = append(callArgs, "%vec* "+slot)
				continue
			}
			if plt == "%vec" && argT == "%str-long" {
				// string -> []byte view: a %str-long passed to a []byte parameter
				// becomes a %vec{ len, len, data-as-intptr } so the callee reads
				// the string's bytes. len is field 0; data (i8*) is field 2 and
				// must be cast to the %vec's i64 intptr. Mirrors the legacy
				// str->vec coercion in genForwardFunc / call args.
				strLen := c.treg("csv.l")
				c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 0\n", strLen, av))
				strData := c.treg("csv.d")
				c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", strData, av))
				strPt := c.treg("csv.p")
				c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", strPt, strData))
				s0 := c.treg("csv0")
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, strLen))
				s1 := c.treg("csv1")
				// cap is 0: this is a borrowed view over the string constant's data,
				// not a heap allocation. Its drop must NOT free the constant memory
				// (legacy @vec_free skips cap==0). len is still correct for reads.
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 1\n", s1, s0))
				s2 := c.treg("csv2")
				c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, strPt))
				slot := c.treg("carg")
				c.sb.WriteString(fmt.Sprintf("  %s = alloca %%vec\n", slot))
				c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
				callArgs = append(callArgs, "%vec* "+slot)
				continue
			}
			if plt == "%vec" {
				// Slices are passed BY REFERENCE (pointer to the caller's slot),
				// matching legacy and Nolang's reference semantics for aggregates.
				// This lets a callee mutate the caller's slice in place (e.g.
				// tls.put-u16 writing into a []byte buffer) — the previous
				// copy-by-value stored into a throwaway copy and, for an empty
				// []byte whose data pointer is null, wrote through a null pointer
				// (trace/BPT trap). Only the slice receiver (i==0) was passed by
				// reference before; regular slice arguments were copied, silently
				// discarding self-mutations. %str-long/%option keep the safe
				// copy-by-value path (they are not mutated in place by callees).
				if argSlot := c.valSlot[inst.Args[argIdx]]; argSlot != "" {
					callArgs = append(callArgs, plt+"* "+argSlot)
				} else {
					c.loadSeq++
					slot := fmt.Sprintf("%%carg%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
					c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
					callArgs = append(callArgs, plt+"* "+slot)
				}
			} else {
				// i64/double -> %str-long auto-coercion: nolang allows
				// passing a scalar where a string parameter is expected
				// (e.g. io.out(x) where x: i64 or x: f64). Legacy silently
				// reinterprets the bits (which passes opt but aborts at
				// runtime); MIR must emit an explicit conversion to produce
				// a valid %str-long. Without this, `store %str-long %lv,
				// %str-long* %carg` has a type mismatch and opt-verify
				// rejects the module (test-tls-prf-only.no,
				// test-iout-autoconvert.no, ...).
				if plt == "%str-long" && argT == "i64" {
					c.loadSeq++
					conv := fmt.Sprintf("%%ic%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_i64(i64 %s)\n", conv, av))
					av = conv
					argT = "%str-long"
				} else if plt == "%str-long" && argT == "i8" {
					// char/byte -> str: render as a single-character string
					c.loadSeq++
					conv := fmt.Sprintf("%%cc%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_char(i8 %s)\n", conv, av))
					av = conv
					argT = "%str-long"
				} else if plt == "%str-long" && argT == "double" {
					c.loadSeq++
					conv := fmt.Sprintf("%%dc%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_double(double %s)\n", conv, av))
					av = conv
					argT = "%str-long"
				} else if plt == "%str-long" && argT == "i1" {
					// bool -> str: "true" or "false"
					c.decl("@.mir.true = private constant [4 x i8] c\"true\"")
					c.decl("@.mir.false = private constant [5 x i8] c\"false\"")
					c.loadSeq++
					treg := fmt.Sprintf("%%bt%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_const(ptr getelementptr inbounds ([4 x i8], ptr @.mir.true, i64 0, i64 0), i64 4)\n", treg))
					c.loadSeq++
					freg := fmt.Sprintf("%%bf%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = call %%str-long @str_from_const(ptr getelementptr inbounds ([5 x i8], ptr @.mir.false, i64 0, i64 0), i64 5)\n", freg))
					c.loadSeq++
					conv := fmt.Sprintf("%%bc%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, %%str-long %s, %%str-long %s\n", conv, av, treg, freg))
					av = conv
					argT = "%str-long"
				}
				c.loadSeq++
				slot := fmt.Sprintf("%%carg%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
				c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
				callArgs = append(callArgs, plt+"* "+slot)
			}
		} else if byPointerLLVM(plt, owned) {
			// Non-owned aggregate parameter (fixed array / non-owned struct):
			// pass the address of the argument's local slot so the callee's
			// by-pointer param is satisfied. The argument is copied by value
			// (call-by-value semantics), matching the prologue's load-into-slot.
			argSlot := c.valSlot[inst.Args[argIdx]]
			if argSlot == "" {
				// The argument has no alloca slot of its own (e.g. a struct /
				// fixed-array literal passed directly at the call site, or a
				// module-global aggregate). Materialize it into a fresh stack
				// slot and pass that address — mirroring the owned-aggregate
				// branch just above. Without this, valid programs that pass an
				// aggregate literal to a function hit "aggregate arg has no
				// slot" and silently fall back to the legacy backend.
				c.loadSeq++
				slot := fmt.Sprintf("%%carg%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
				c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
				callArgs = append(callArgs, plt+"* "+slot)
			} else {
				callArgs = append(callArgs, plt+"* "+argSlot)
			}
		} else {
			// Coerce the actual argument to the callee's parameter type. nolang
			// ints are i64 by default but byte/i8 values may be passed to an i64
			// parameter (e.g. byte.to-str calling u64-to-str); without the sext
			// LLVM rejects `i64 %reg` where %reg is defined as i8.
			av = c.coerceInt(av, argT, plt)
			callArgs = append(callArgs, plt+" "+av)
		}
	}
	var resSlots []string
	for idx, p := range outParams {
		if idx == 0 && selfOut > 0 {
			// The first out-param (self) was already passed as the receiver's
			// slot address above; no %cres alloca needed.
			continue
		}
		plt, _ := c.ptype(p)
		c.loadSeq++
		slot := fmt.Sprintf("%%cres%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
		callArgs = append(callArgs, plt+"* "+slot)
		resSlots = append(resSlots, slot)
	}
	c.sb.WriteString(fmt.Sprintf("  call void %s(%s)\n", calleeName, strings.Join(callArgs, ", ")))
	// Store every result value back into its slot. A multi-value return
	// (x, y = f()) lowers to one call with N out-params; inst.Results holds all
	// N result values, so each resSlot[i] must be loaded and stored into
	// inst.Results[i]'s slot — not just the first (inst.Dst).
	// A call whose result lands in inst.Dst instead of inst.Results — an
	// indirect call through a fn-typed parameter (emitIndirectCall) — has no
	// Results entry, so the loop below breaks on the first iteration and the
	// destination slot is left UNINITIALIZED: the callee writes into %cres but
	// nothing ever copies it back out, so `out = cb()` returned stack garbage.
	// Verified in IR (tests/test-fn-type-return.no, @apply_ret):
	//     %cres30 = alloca i64
	//     call void %fnptr29(ptr %cres30)   ; cb() writes here ...
	//     %mv31 = load i64, ptr %v2.s       ; ... but %v2.s was never stored
	//     store i64 %mv31, ptr %v0.s
	//     store i64 %lv31, ptr %p1          ; so the out-param gets garbage
	// The three fn-type tests passed only because the garbage happened to match
	// the expected value. The read is undef, so LLVM may exploit it: with an
	// unrelated change perturbing stack layout, InstCombine deleted the entire
	// print path and the tests went silent.
	results := inst.Results
	if len(results) == 0 && inst.Dst > NoVal && len(resSlots) == 1 && c.valSlot[inst.Dst] != "" {
		results = []ValueID{inst.Dst}
	}
	for i, rs := range resSlots {
		if i >= len(results) {
			break
		}
		rv := results[i]
		if rv <= NoVal {
			continue
		}
		rlt, _ := c.ptype(rv)
		// #83 SROA guard: same as emitMove, use memcpy for large aggregates.
		if c.shouldUseMemcpy(rlt) {
			sz := c.typeSizeOperand(rlt)
			c.emitMemcpy(c.valSlot[rv], rs, sz)
			continue
		}
		c.sb.WriteString(fmt.Sprintf("  %%c%d = load %s, %s* %s\n", rv, rlt, rlt, rs))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", rlt, rv, rlt, c.valSlot[rv]))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Async task runtime (OpRun / OpAwait + cooperative scheduler)
// ---------------------------------------------------------------------------
//
// An `-async` call is lowered to OpRun, which builds a heap %task (resume_fn =
// a generated async_wrapper.N, data = an args struct whose field 0 is the
// result pointer, done/cancelled = false) and enqueues it. OpAwait drives the
// task to completion: if not yet done it synchronously calls resume_fn(task);
// then it reads the result from the args struct's field 0. This mirrors the
// legacy build/llvm model; for flat (top-level) await trees — the only shape
// tests/test-async.no uses — the synchronous drive yields byte-identical
// output to the legacy event loop without ever entering nolang_async_run.

// mallocBytesFor returns a heap size (bytes) large enough to hold an LLVM value
// of type lt. Scalars get their natural width; aggregates get their header size.
// This is used only to size the per-arg and per-result buffers the wrapper
// reads through, so the value is copied in/out intact.
func (c *codegen) mallocBytesFor(lt string) int64 {
	switch lt {
	case "i64", "double":
		return 8
	case "i1", "i8":
		return 1
	case "%str-long", "%vec":
		return 24
	case "%option":
		return 8 + c.optSlotBytes // i64 tag + payload slot (configured width)
	}
	if strings.HasPrefix(lt, "%") {
		// named user struct: approximate with its emitted layout size.
		if sz := c.structLLVMSize(lt); sz > 0 {
			return sz
		}
		return 24
	}
	if strings.HasPrefix(lt, "[") {
		// fixed array: best-effort element size * length.
		if n, elem := parseArrayType(lt); n > 0 {
			return n * c.mallocBytesFor(elem)
		}
		return 8
	}
	return 8
}

// structLLVMSize returns the byte size of a named struct type %name from the
// emitted struct layout (StructFields). Returns 0 when unknown. The StructFields
// key is the raw type; the sanitized %name is reconstructed back to raw by
// turning underscores into dots.
func (c *codegen) structLLVMSize(name string) int64 {
	raw := strings.TrimPrefix(name, "%")
	raw = strings.ReplaceAll(raw, "_", ".")
	if fields, ok := c.mod.StructFields[raw]; ok {
		var sz int64
		for _, fld := range fields {
			// A pointer field occupies one pointer, NOT the pointee's size.
			if c.fieldIsPointer(fld) {
				sz += 8
				continue
			}
			if tid := c.mod.internType(fld.TypeRaw); tid != NoType {
				if ty := c.mod.Type(tid); ty != nil {
					sz += c.mallocBytesFor(c.llvmTypeOf(ty))
					continue
				}
			}
			sz += 8
		}
		return sz
	}
	return 0
}

// parseArrayType parses a fixed-array LLVM type "[N x elem]" into (N, elem).
func parseArrayType(lt string) (int64, string) {
	if !strings.HasPrefix(lt, "[") {
		return 0, ""
	}
	// [N x elem]
	inner := lt[1 : len(lt)-1]
	sp := strings.Index(inner, " x ")
	if sp < 0 {
		return 0, ""
	}
	var n int64
	if _, err := fmt.Sscanf(inner[:sp], "%d", &n); err != nil {
		return 0, ""
	}
	return n, inner[sp+3:]
}

// emitAsyncScheduler emits the cooperative-scheduler globals and functions
// (nolang_async_enqueue / _yield / _wait / _done / _run) into the module tail.
// These are ported verbatim from the legacy build/llvm emitter (decl.go); the
// %task type they reference is declared in the prelude.
func (c *codegen) emitAsyncScheduler() {
	var b strings.Builder
	b.WriteString("@nolang_ready_q = global [256 x i8*] zeroinitializer\n")
	b.WriteString("@nolang_ready_head = global i32 0\n")
	b.WriteString("@nolang_ready_tail = global i32 0\n")
	b.WriteString("@nolang_current_task = global i8* null\n")
	b.WriteString("@nolang_waiters = global [256 x i8*] zeroinitializer\n")
	// nolang_async_enqueue(task): enqueue into the ready queue (ring of 256).
	b.WriteString("define void @nolang_async_enqueue(i8* %task) {\n")
	b.WriteString("entry:\n")
	b.WriteString("\t%tail = load i32, i32* @nolang_ready_tail\n")
	b.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_ready_q, i32 0, i32 %tail\n")
	b.WriteString("\tstore i8* %task, i8** %gep\n")
	b.WriteString("\t%next = add i32 %tail, 1\n")
	b.WriteString("\t%mod = urem i32 %next, 256\n")
	b.WriteString("\tstore i32 %mod, i32* @nolang_ready_tail\n")
	b.WriteString("\tret void\n}\n")
	// nolang_async_yield(): re-enqueue the current task.
	b.WriteString("define void @nolang_async_yield() {\n")
	b.WriteString("entry:\n")
	b.WriteString("\t%cur = load i8*, i8** @nolang_current_task\n")
	b.WriteString("\tcall void @nolang_async_enqueue(i8* %cur)\n")
	b.WriteString("\tret void\n}\n")
	// nolang_async_wait(waited): register the current task as waiter on `waited`.
	b.WriteString("define void @nolang_async_wait(i8* %waited) {\n")
	b.WriteString("entry:\n")
	b.WriteString("\t%cur = load i8*, i8** @nolang_current_task\n")
	b.WriteString("\t%idx = ptrtoint i8* %waited to i64\n")
	b.WriteString("\t%idx8 = and i64 %idx, 255\n")
	b.WriteString("\t%idx32 = trunc i64 %idx8 to i32\n")
	b.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_waiters, i32 0, i32 %idx32\n")
	b.WriteString("\tstore i8* %cur, i8** %gep\n")
	b.WriteString("\tret void\n}\n")
	// nolang_async_done(task): wake the task's waiter (if any).
	b.WriteString("define void @nolang_async_done(i8* %task) {\n")
	b.WriteString("entry:\n")
	b.WriteString("\t%idx = ptrtoint i8* %task to i64\n")
	b.WriteString("\t%idx8 = and i64 %idx, 255\n")
	b.WriteString("\t%idx32 = trunc i64 %idx8 to i32\n")
	b.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_waiters, i32 0, i32 %idx32\n")
	b.WriteString("\t%waiter = load i8*, i8** %gep\n")
	b.WriteString("\t%is_null = icmp eq i8* %waiter, null\n")
	b.WriteString("\tbr i1 %is_null, label %ret, label %wake\n")
	b.WriteString("wake:\n")
	b.WriteString("\tcall void @nolang_async_enqueue(i8* %waiter)\n")
	b.WriteString("\tstore i8* null, i8** %gep\n")
	b.WriteString("\tbr label %ret\n")
	b.WriteString("ret:\n")
	b.WriteString("\tret void\n}\n")
	// nolang_async_run(main_task): the event loop. Drives every enqueued task to
	// completion, waking waiters when a task finishes. Only needed for nested
	// async (await inside an -async fn); flat top-level awaits drive inline.
	b.WriteString("define void @nolang_async_run(i8* %main_task) {\n")
	b.WriteString("entry:\n")
	b.WriteString("\tcall void @nolang_async_enqueue(i8* %main_task)\n")
	b.WriteString("\tbr label %loop\n")
	b.WriteString("loop:\n")
	b.WriteString("\t%head = load i32, i32* @nolang_ready_head\n")
	b.WriteString("\t%tail = load i32, i32* @nolang_ready_tail\n")
	b.WriteString("\t%eq = icmp eq i32 %head, %tail\n")
	b.WriteString("\tbr i1 %eq, label %exit, label %run_one\n")
	b.WriteString("run_one:\n")
	b.WriteString("\t%gep = getelementptr [256 x i8*], [256 x i8*]* @nolang_ready_q, i32 0, i32 %head\n")
	b.WriteString("\t%task = load i8*, i8** %gep\n")
	b.WriteString("\tstore i8* %task, i8** @nolang_current_task\n")
	b.WriteString("\t%next = add i32 %head, 1\n")
	b.WriteString("\t%mod = urem i32 %next, 256\n")
	b.WriteString("\tstore i32 %mod, i32* @nolang_ready_head\n")
	b.WriteString("\t%task_typed = bitcast i8* %task to { void (i8*)*, i64, i1, i1 }*\n")
	b.WriteString("\t%fn_gep = getelementptr { void (i8*)*, i64, i1, i1 }, { void (i8*)*, i64, i1, i1 }* %task_typed, i32 0, i32 0\n")
	b.WriteString("\t%resume_fn = load void (i8*)*, void (i8*)** %fn_gep\n")
	b.WriteString("\tcall void %resume_fn(i8* %task)\n")
	b.WriteString("\t%done_gep = getelementptr { void (i8*)*, i64, i1, i1 }, { void (i8*)*, i64, i1, i1 }* %task_typed, i32 0, i32 2\n")
	b.WriteString("\t%done_val = load i1, i1* %done_gep\n")
	b.WriteString("\tbr i1 %done_val, label %done_handler, label %loop\n")
	b.WriteString("done_handler:\n")
	b.WriteString("\tcall void @nolang_async_done(i8* %task)\n")
	b.WriteString("\tbr label %loop\n")
	b.WriteString("exit:\n")
	b.WriteString("\tret void\n}\n")
	c.extraGlobals = append(c.extraGlobals, b.String())
}

// asyncWrapperFor returns (generating if necessary) the LLVM name of the
// async_wrapper.N define that resumes an `-async` callee. The wrapper calls the
// target MIR function with the arg/result pointers packed in the args struct,
// then marks the task done. argTypes are the callee's non-result (input)
// parameter LLVM types in order; resLT is the single result type.
func (c *codegen) asyncWrapperFor(calleeName, targetName string, argTypes []string, resLT string) string {
	if w, ok := c.asyncWrappers[calleeName]; ok {
		return w
	}
	c.asyncWrapperSeq++
	n := c.asyncWrapperSeq
	wname := fmt.Sprintf("async_wrapper.%d", n)

	numFields := len(argTypes) + 1
	argsTypeStr := "{ "
	for i := 0; i < numFields; i++ {
		if i > 0 {
			argsTypeStr += ", "
		}
		argsTypeStr += "i8*"
	}
	argsTypeStr += " }"

	var b strings.Builder
	fmt.Fprintf(&b, "define void @%s(i8* %%task_ptr) {\n", wname)
	b.WriteString("entry:\n")
	b.WriteString("\t%t = bitcast i8* %task_ptr to %task*\n")
	// Guard: already done -> skip (event loop may re-schedule a finished task).
	b.WriteString("\t%done.gep = getelementptr inbounds %task, %task* %t, i32 0, i32 2\n")
	b.WriteString("\t%done.val = load i1, i1* %done.gep\n")
	b.WriteString("\tbr i1 %done.val, label %w_exit, label %w_can\n")
	// Cancelled -> mark done and skip (graceful abort).
	b.WriteString("w_can:\n")
	b.WriteString("\t%can.gep = getelementptr inbounds %task, %task* %t, i32 0, i32 3\n")
	b.WriteString("\t%can.val = load i1, i1* %can.gep\n")
	b.WriteString("\tbr i1 %can.val, label %w_skip, label %w_exec\n")
	b.WriteString("w_skip:\n")
	b.WriteString("\tstore i1 true, i1* %done.gep\n")
	b.WriteString("\tbr label %w_exit\n")
	// Execute: read args struct from data field, call target, set done.
	b.WriteString("w_exec:\n")
	b.WriteString("\t%data.gep = getelementptr inbounds %task, %task* %t, i32 0, i32 1\n")
	b.WriteString("\t%data.i64 = load i64, i64* %data.gep\n")
	b.WriteString("\t%data.i8 = inttoptr i64 %data.i64 to i8*\n")
	fmt.Fprintf(&b, "\t%%args.typed = bitcast i8* %%data.i8 to %s*\n", argsTypeStr)
	// result ptr (args field 0)
	fmt.Fprintf(&b, "\t%%result.ptr.gep = getelementptr inbounds %s, %s* %%args.typed, i32 0, i32 0\n", argsTypeStr, argsTypeStr)
	b.WriteString("\t%result.ptr = load i8*, i8** %result.ptr.gep\n")
	fmt.Fprintf(&b, "\t%%result.typed = bitcast i8* %%result.ptr to %s*\n", resLT)
	var callArgs []string
	for i, at := range argTypes {
		fmt.Fprintf(&b, "\t%%warg.%d.gep = getelementptr inbounds %s, %s* %%args.typed, i32 0, i32 %d\n", i, argsTypeStr, argsTypeStr, i+1)
		fmt.Fprintf(&b, "\t%%warg.%d.ptr = load i8*, i8** %%warg.%d.gep\n", i, i)
		fmt.Fprintf(&b, "\t%%warg.%d.typed = bitcast i8* %%warg.%d.ptr to %s*\n", i, i, at)
		// Honor the MIR call ABI: scalar (non-pointer) parameters are passed BY
		// VALUE, every aggregate/owned parameter is passed BY POINTER. The args
		// struct stores an i8* to a heap copy; for scalars we load the value and
		// pass it directly, matching how emitCallBody lowers a normal call.
		if isScalarLLVM(at) {
			fmt.Fprintf(&b, "\t%%warg.%d.val = load %s, %s* %%warg.%d.typed\n", i, at, at, i)
			callArgs = append(callArgs, fmt.Sprintf("%s %%warg.%d.val", at, i))
		} else {
			callArgs = append(callArgs, fmt.Sprintf("%s* %%warg.%d.typed", at, i))
		}
	}
	callArgs = append(callArgs, fmt.Sprintf("%s* %%result.typed", resLT))
	fmt.Fprintf(&b, "\tcall void @%s(%s)\n", targetName, strings.Join(callArgs, ", "))
	// Free each arg container (the malloc'd per-arg buffer); the data the
	// target may have moved into its result is owned by the result buffer, not
	// by these arg buffers, so freeing only the container is safe.
	for i := range argTypes {
		fmt.Fprintf(&b, "\tcall void @free(i8* %%warg.%d.ptr)\n", i)
	}
	b.WriteString("\tstore i1 true, i1* %done.gep\n")
	b.WriteString("\tbr label %w_exit\n")
	b.WriteString("w_exit:\n")
	b.WriteString("\tret void\n}\n\n")
	w := b.String()
	c.extraGlobals = append(c.extraGlobals, w)
	c.asyncWrappers[calleeName] = wname
	return wname
}

// coerceAsyncArg loads an async-launch argument and coerces it to the target
// parameter's declared LLVM type `plt`, returning the (type, register) pair to
// store into the args struct. The wrapper reinterprets the stored bytes as
// `plt`, so a mismatch would make the callee read garbage: notably a fixed
// stack array `[N x T]` bound to a `[]T` (%vec) parameter. The array->slice
// cases reuse the exact coercions emitCallBody applies for ordinary calls
// (heap-owned copy for trivially-copyable elements, borrow view otherwise;
// string byte view for %str-long -> []byte).
func (c *codegen) coerceAsyncArg(av ValueID, plt string) (string, string) {
	alt, areg := c.loadVal(av)
	if alt == plt {
		return alt, areg
	}
	if plt == "%vec" && strings.HasPrefix(alt, "[") {
		slot := c.valSlot[av]
		if slot == "" {
			// Value has no addressable slot (a pure SSA constant): spill it so
			// the coercion can take &arr[0].
			c.loadSeq++
			slot = fmt.Sprintf("%%arun.spill.%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, alt))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", alt, areg, alt, slot))
		}
		return "%vec", c.vecFromArraySink(alt, slot)
	}
	if plt == "%vec" && alt == "%str-long" {
		// string -> []byte view: %vec{ len, 0, data-as-intptr }. cap is 0 so the
		// borrowed constant data is never freed.
		l := c.treg("asv.l")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 0\n", l, areg))
		d := c.treg("asv.d")
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", d, areg))
		p := c.treg("asv.p")
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", p, d))
		s0 := c.treg("asv0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, l))
		s1 := c.treg("asv1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 0, 1\n", s1, s0))
		s2 := c.treg("asv2")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, p))
		return "%vec", s2
	}
	return alt, areg
}

// emitAsyncRun emits caller code for OpRun: it resolves the `-async` callee,
// builds a heap %task (resume_fn = a generated wrapper, data = an args struct
// {result_ptr, arg0_ptr, ...}, done/cancelled = false), enqueues it, and stores
// the opaque task handle (i8* bitcast to i64) into inst.Dst. Each argument is
// copied into its own heap buffer so the value survives until the wrapper runs.
func (c *codegen) emitAsyncRun(f *Function, inst *Inst) error {
	calleeName := inst.Sym
	if calleeName == "" {
		// run <handle-var>: the operand is already a handle; forward it.
		hlt, hreg := c.loadVal(inst.Args[0])
		slot := c.valSlot[inst.Dst]
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", hlt, hreg, hlt, slot))
		return nil
	}
	cid, ok := c.mod.FuncByName[calleeName]
	if !ok {
		c.fail("OpRun: unknown async callee %q", calleeName)
		return fmt.Errorf("OpRun: unknown async callee %q", calleeName)
	}
	cf := c.mod.Func(cid)
	targetName := c.fname[cid]

	isResult := map[ValueID]bool{}
	for _, rp := range cf.ResultParams {
		isResult[rp] = true
	}
	var argTypes []string
	for _, p := range cf.Params {
		if isResult[p] {
			continue
		}
		lt, _ := c.ptype(p)
		argTypes = append(argTypes, lt)
	}
	resLT := "i64"
	if len(cf.ResultParams) > 0 {
		resLT, _ = c.ptype(cf.ResultParams[0])
	}
	wrapperName := c.asyncWrapperFor(calleeName, targetName, argTypes, resLT)

	numFields := len(argTypes) + 1
	argsTypeStr := "{ "
	for i := 0; i < numFields; i++ {
		if i > 0 {
			argsTypeStr += ", "
		}
		argsTypeStr += "i8*"
	}
	argsTypeStr += " }"

	// 1. result buffer (heap), zero-initialized so an owned payload slot is valid.
	c.loadSeq++
	resBuf := fmt.Sprintf("%%arun.resbuf.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", resBuf, c.mallocBytesFor(resLT)))
	c.loadSeq++
	resBufT := fmt.Sprintf("%%arun.resbuf.t.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", resBufT, resBuf, resLT))
	c.sb.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %s\n", resLT, resLT, resBufT))

	// 2. args struct (heap): { i8*, i8*, ... }
	c.loadSeq++
	argsStruct := fmt.Sprintf("%%arun.args.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", argsStruct, int64(numFields)*8))
	c.loadSeq++
	argsStructT := fmt.Sprintf("%%arun.args.t.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", argsStructT, argsStruct, argsTypeStr))

	// field 0 = result buffer (as i8*)
	c.loadSeq++
	resI8 := fmt.Sprintf("%%arun.resi8.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", resI8, resLT, resBufT))
	c.loadSeq++
	f0 := fmt.Sprintf("%%arun.f0.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", f0, argsTypeStr, argsTypeStr, argsStructT))
	c.sb.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", resI8, f0))

	// each arg: coerce to the PARAMETER's declared LLVM type, then copy into a
	// heap buffer and store its i8* into field i+1. The wrapper passes each
	// buffer's ADDRESS straight to the target, so the buffer's layout must
	// equal the declared parameter type. Binding the buffer to the argument
	// VALUE's type instead (a raw fixed array `[N x i64]` for a `[]i64`
	// parameter) makes the callee reinterpret the element bytes as a
	// %vec{len,cap,data} header and dereference an integer as a data pointer ->
	// SIGSEGV (async-shared-race / test-slot-rebind-unsafe family). Mirrors
	// emitCallBody's array->slice coercion.
	for i, av := range inst.Args {
		plt := "i64"
		if i < len(argTypes) {
			plt = argTypes[i]
		}
		alt, areg := c.coerceAsyncArg(av, plt)
		c.loadSeq++
		abuf := fmt.Sprintf("%%arun.argbuf.%d_%d", c.loadSeq, i)
		c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", abuf, c.mallocBytesFor(alt)))
		c.loadSeq++
		abufT := fmt.Sprintf("%%arun.argbuf.t.%d_%d", c.loadSeq, i)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", abufT, abuf, alt))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", alt, areg, alt, abufT))
		c.loadSeq++
		ai8 := fmt.Sprintf("%%arun.argi8.%d_%d", c.loadSeq, i)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", ai8, alt, abufT))
		c.loadSeq++
		fi := fmt.Sprintf("%%arun.fi.%d_%d", c.loadSeq, i)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", fi, argsTypeStr, argsTypeStr, argsStructT, i+1))
		c.sb.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", ai8, fi))
	}

	// bitcast args struct to i8*, then to i64 for the task's data field.
	c.loadSeq++
	argsI8 := fmt.Sprintf("%%arun.argsi8.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", argsI8, argsTypeStr, argsStructT))
	c.loadSeq++
	argsI64 := fmt.Sprintf("%%arun.argsi64.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", argsI64, argsI8))

	// 3. task struct (heap).
	c.loadSeq++
	taskBuf := fmt.Sprintf("%%arun.task.%d", c.loadSeq)
	c.sb.WriteString("  " + taskBuf + " = call i8* @malloc(i64 24)\n")
	c.loadSeq++
	taskT := fmt.Sprintf("%%arun.task.t.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%task*\n", taskT, taskBuf))
	c.loadSeq++
	tf0 := fmt.Sprintf("%%arun.tf0.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", tf0, taskT))
	c.sb.WriteString(fmt.Sprintf("  store void (i8*)* @%s, void (i8*)** %s\n", wrapperName, tf0))
	c.loadSeq++
	tf1 := fmt.Sprintf("%%arun.tf1.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", tf1, taskT))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", argsI64, tf1))
	c.loadSeq++
	tf2 := fmt.Sprintf("%%arun.tf2.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", tf2, taskT))
	c.sb.WriteString(fmt.Sprintf("  store i1 false, i1* %s\n", tf2))
	c.loadSeq++
	tf3 := fmt.Sprintf("%%arun.tf3.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 3\n", tf3, taskT))
	c.sb.WriteString(fmt.Sprintf("  store i1 false, i1* %s\n", tf3))

	// enqueue the task.
	c.loadSeq++
	taskI8 := fmt.Sprintf("%%arun.taski8.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %%task* %s to i8*\n", taskI8, taskT))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_async_enqueue(i8* %s)\n", taskI8))

	// return handle (i8* -> i64) into inst.Dst.
	slot := c.valSlot[inst.Dst]
	c.loadSeq++
	handleI64 := fmt.Sprintf("%%arun.handle.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", handleI64, taskI8))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", handleI64, slot))
	return nil
}

// emitAsyncAwait emits caller code for OpAwait: it loads the task handle,
// synchronously drives the task to completion if not already done, then reads
// the result from the args struct's field 0 and stores it into inst.Dst. The
// result/args/task heap containers are freed afterwards (their inner owned data
// is now owned by the result slot, so only the containers are freed).
func (c *codegen) emitAsyncAwait(f *Function, inst *Inst) error {
	resLT, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		c.fail("OpAwait: no slot for destination")
		return fmt.Errorf("OpAwait: no slot")
	}
	// load handle (i64) -> %task*
	_, hreg := c.loadVal(inst.Args[0])
	c.loadSeq++
	taskI8 := fmt.Sprintf("%%aawy.ti8.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", taskI8, hreg))
	c.loadSeq++
	taskT := fmt.Sprintf("%%aawy.tt.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%task*\n", taskT, taskI8))

	// check done (field 2)
	c.loadSeq++
	doneGep := fmt.Sprintf("%%aawy.dg.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", doneGep, taskT))
	c.loadSeq++
	doneVal := fmt.Sprintf("%%aawy.dv.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i1, i1* %s\n", doneVal, doneGep))

	notDoneDef := fmt.Sprintf("aawy.notdone.%d", c.loadSeq)
	notDoneRef := "%" + notDoneDef
	doneDef := fmt.Sprintf("aawy.done.%d", c.loadSeq)
	doneRef := "%" + doneDef
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %s, label %s\n", doneVal, doneRef, notDoneRef))

	// not done: call resume_fn(task) to drive it to completion.
	c.sb.WriteString(notDoneDef + ":\n")
	c.loadSeq++
	fnGep := fmt.Sprintf("%%aawy.fg.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", fnGep, taskT))
	c.loadSeq++
	fnVal := fmt.Sprintf("%%aawy.fn.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load void (i8*)*, void (i8*)** %s\n", fnVal, fnGep))
	// Publish this task as the current task for the duration of the synchronous
	// drive, so an `async-cancelled()` call inside the target reads the right
	// %task.cancelled flag (the legacy event loop sets @nolang_current_task the
	// same way before invoking a task's resume_fn). Restored to null afterwards;
	// MIR drives one task at a time (no nested coroutine suspend), so a single
	// save/restore pair is sufficient.
	c.sb.WriteString(fmt.Sprintf("  store i8* %s, i8** @nolang_current_task\n", taskI8))
	c.sb.WriteString(fmt.Sprintf("  call void %s(i8* %s)\n", fnVal, taskI8))
	c.sb.WriteString("  store i8* null, i8** @nolang_current_task\n")
	c.sb.WriteString(fmt.Sprintf("  br label %s\n", doneRef))

	// done: read result from args struct field 0.
	c.sb.WriteString(doneDef + ":\n")
	c.loadSeq++
	dataGep := fmt.Sprintf("%%aawy.dataGep.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", dataGep, taskT))
	c.loadSeq++
	dataI64 := fmt.Sprintf("%%aawy.dataI64.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", dataI64, dataGep))
	c.loadSeq++
	dataI8 := fmt.Sprintf("%%aawy.dataI8.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dataI8, dataI64))
	c.loadSeq++
	argsTyped := fmt.Sprintf("%%aawy.argsT.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to { i8* }*\n", argsTyped, dataI8))
	c.loadSeq++
	resPtrGep := fmt.Sprintf("%%aawy.rpGep.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", resPtrGep, argsTyped))
	c.loadSeq++
	resPtr := fmt.Sprintf("%%aawy.rp.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", resPtr, resPtrGep))
	c.loadSeq++
	resTyped := fmt.Sprintf("%%aawy.resT.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", resTyped, resPtr, resLT))
	c.loadSeq++
	resVal := fmt.Sprintf("%%aawy.res.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", resVal, resLT, resLT, resTyped))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", resLT, resVal, resLT, slot))

	// free the containers (result buffer, args struct, task struct). Inner owned
	// data now lives in the result slot, so freeing only the 24/8-byte wrappers
	// is safe.
	c.loadSeq++
	fr := fmt.Sprintf("%%aawy.freeres.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i8*\n", fr, resPtr))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", fr))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", dataI8))
	c.loadSeq++
	ft := fmt.Sprintf("%%aawy.freetask.%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast %%task* %s to i8*\n", ft, taskT))
	c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", ft))
	return nil
}

// emitIndirectCall emits a call through a function-pointer local/param. The
// inst.Callee value holds a `void(...)*` (or precisely-typed) function pointer
// in its slot; we load it and call through the register, reusing emitCallBody
// with a synthetic signature built from the callee's KindFunc MIR type. The
// synthetic cf's Params carry the fn type's input types, and its ResultParams
// carry the fn type's result types (also appended to Params so emitCallBody's
// inParams/outParams split separates them). Synthetic param ValueIDs exist only
// to carry a MIR type for ptype(); they have no slot and are never referenced.
func (c *codegen) emitIndirectCall(f *Function, inst *Inst) error {
	ct := c.mod.Type(c.mod.Value(inst.Callee).Type)
	if ct == nil || ct.Kind != KindFunc || ct.Func == nil {
		c.fail("indirect call target is not a function type in func %s", f.Name)
		return fmt.Errorf("bad indirect callee in %s", f.Name)
	}
	slot := c.valSlot[inst.Callee]
	if slot == "" {
		c.fail("indirect call target has no slot in func %s", f.Name)
		return fmt.Errorf("no slot for indirect callee in %s", f.Name)
	}
	fnPtrLT := c.llvmTypeOf(ct) // void (...)*
	c.loadSeq++
	fnReg := fmt.Sprintf("%%fnptr%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", fnReg, fnPtrLT, fnPtrLT, slot))

	cf := &Function{}
	for _, pt := range ct.Func.Params {
		cf.Params = append(cf.Params, c.syntheticVal(pt))
	}
	for _, rt := range ct.Func.Results {
		v := c.syntheticVal(rt)
		cf.Params = append(cf.Params, v)
		cf.ResultParams = append(cf.ResultParams, v)
	}
	return c.emitCallBody(f, cf, inst, fnReg)
}

// syntheticVal mints a Value whose only role is to carry a MIR type for ptype()
// during synthetic-signature call emission (indirect calls). It has no slot and
// is never referenced by name.
func (c *codegen) syntheticVal(tid TypeID) ValueID {
	v := ValueID(len(c.mod.Values))
	c.mod.Values = append(c.mod.Values, Value{ID: v, Type: tid})
	return v
}

// emitBuiltinRawByteAt lowers `str.byte(i)` / `txt.byte(i)`: the raw-byte
// accessor `s.byte(i)` in source. It returns the byte at index i of the
// receiver's underlying buffer, zero-extended to i64 — always O(1), unlike the
// UTF-8 codepoint index `s[i]`. No bounds check is emitted here because the MIR
// backend has no bounds-check helper (consistent with the rest of its codegen);
// in-bounds access (the only case used by the std library) is unaffected.
// Mirrors legacy build/llvm strchar_at.go generateRawByteAt.
func (c *codegen) emitBuiltinRawByteAt(f *Function, inst *Inst) error {
	if len(inst.Args) < 2 {
		c.fail("builtin %s: missing receiver or index", inst.Sym)
		return fmt.Errorf("builtin %s: bad args", inst.Sym)
	}
	if inst.Dst <= NoVal {
		return nil
	}
	rt, _ := c.ptype(inst.Args[0])
	rslot := c.valSlot[inst.Args[0]]
	if rslot == "" {
		c.fail("builtin %s: receiver has no slot", inst.Sym)
		return fmt.Errorf("builtin %s: no receiver slot", inst.Sym)
	}
	_, idxV := c.loadVal(inst.Args[1])
	var dataPtr string
	c.loadSeq++
	dGEP := fmt.Sprintf("%%rbdg%d", c.loadSeq)
	if rt == "%txt" {
		// %txt = [255 x i8] at field 0; bitcast to i8*.
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0, i64 0\n", dGEP, rslot))
		c.loadSeq++
		dataPtr = fmt.Sprintf("%%rbdp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast [255 x i8]* %s to i8*\n", dataPtr, dGEP))
	} else {
		// %str-long = { i64 len, i64 cap, i8* data } — field 2 is the data ptr.
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", dGEP, rslot))
		c.loadSeq++
		dataPtr = fmt.Sprintf("%%rbdp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", dataPtr, dGEP))
	}
	c.loadSeq++
	gep := fmt.Sprintf("%%rbg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr i8, i8* %s, i64 %s\n", gep, dataPtr, idxV))
	c.loadSeq++
	val := fmt.Sprintf("%%rbv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", val, gep))
	c.loadSeq++
	zext := fmt.Sprintf("%%rbz%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", zext, val))
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("builtin %s: no result slot", inst.Sym)
		return fmt.Errorf("builtin %s: no dst slot", inst.Sym)
	}
	v := c.coerce("i64", zext, dstLT)
	if v == "" {
		return fmt.Errorf("builtin %s: cannot store i64 into %s", inst.Sym, dstLT)
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstLT, v, dstLT, dstSlot))
	return nil
}

func (c *codegen) emitReturn(f *Function) error {
	for idx, rp := range f.ResultParams {
		// For a method, the first result param is `self` (the out-param
		// receiver).  Its value is modified in-place through the caller's
		// pointer during the method body, so there is nothing to write back
		// at return — writing back would be a no-op self-store.
		if f.IsMethod && idx == 0 {
			continue
		}
		plt, _ := c.ptype(rp)
		// #83 SROA guard: use memcpy for large aggregates.
		if c.shouldUseMemcpy(plt) {
			sz := c.typeSizeOperand(plt)
			c.emitMemcpy(c.paramPtr[rp], c.valSlot[rp], sz)
			continue
		}
		_, v := c.loadVal(rp)
		pp := c.paramPtr[rp]
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, v, plt, pp))
	}
	c.sb.WriteString("  ret void\n")
	return nil
}

// emitBuiltin emits a call to a builtin (registered in the builtin table) that
// has no real Nolang body in the MIR module. It mirrors the legacy call.go
// builtin dispatch and clears the `callee:<name>` gap class.
//
// Dispatch order matters and follows the shape of the table itself:
//
//	CLibCall      -> generic C call (largest family: os/fs/process/net)
//	LLVMIntrinsic -> scalar LLVM intrinsic (sqrt/fabs/...)
//	LLVMConv      -> scalar conversion
//	ForwardFunc   -> hand-written inline IR, by name
//
// CLibCall is checked first because a builtin may carry several annotations and
// the C-call ABI is the one flavour that a single generic implementation covers
// completely; everything else needs a name-specific lowering.
func (c *codegen) emitBuiltin(f *Function, inst *Inst, bm *builtin.BuiltinMethod) error {
	switch {
	case bm.CLibCall != nil:
		return c.emitBuiltinCLib(f, inst, bm)
	case bm.LLVMIntrinsic != "":
		// Scalar LLVM intrinsic (sqrt/fabs/...): double args -> double result.
		var argTys, argRegs []string
		for _, a := range inst.Args {
			lt, v := c.loadVal(a)
			argTys = append(argTys, lt)
			argRegs = append(argRegs, v)
		}
		argStr := ""
		for i, t := range argTys {
			if i > 0 {
				argStr += ", "
			}
			argStr += t + " " + argRegs[i]
		}
		resSlot := c.valSlot[inst.Dst]
		if resSlot == "" {
			c.sb.WriteString(fmt.Sprintf("  call double @%s(%s)\n", bm.LLVMIntrinsic, argStr))
			return nil
		}
		rl, _ := c.ptype(inst.Dst)
		if rl == "" {
			rl = "double"
		}
		r := fmt.Sprintf("%%bi%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", r, rl, bm.LLVMIntrinsic, argStr))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", rl, r, rl, resSlot))
		return nil
	case bm.LLVMConv != nil:
		return c.emitBuiltinConv(f, inst, bm)
	case bm.ForwardFunc != "":
		return c.emitBuiltinForward(f, inst, bm)
	default:
		c.fail("unsupported builtin %s (no CLibCall/LLVMIntrinsic/LLVMConv/ForwardFunc) in func %s", inst.Sym, f.Name)
		return fmt.Errorf("unsupported builtin %s", inst.Sym)
	}
}

// emitBuiltinAlloc lowers with-cap/with-len/with-cap-len: heap allocation builtins
// that build a %vec or %str-long by-value (data = malloc(cap*stride)). The result
// type (inst.Dst) selects the element stride: 8 for %vec, 1 for %str-long.
func (c *codegen) emitBuiltinAlloc(inst *Inst, ff string) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	if slot == "" {
		return fmt.Errorf("builtin %s: no result slot", ff)
	}
	if len(inst.Args) == 0 {
		c.fail("builtin %s expects an argument", ff)
		return fmt.Errorf("builtin %s arity", ff)
	}
	_, capV := c.loadVal(inst.Args[0])
	var lenV string
	switch ff {
	case "with-cap":
		lenV = "0"
	case "with-len":
		lenV = capV
	case "with-cap-len":
		if len(inst.Args) > 1 {
			_, lenV = c.loadVal(inst.Args[1])
		} else {
			lenV = capV
		}
	}
	// The backing store holds ELEMENT-sized slots, so the stride comes from the
	// destination's own element type ([]str -> 24-byte %str-long, []?i64 ->
	// 32-byte %option), not from a constant 8. elemTypeOfReceiver returns "i8"
	// for a str destination, which is exactly the 1-byte-per-slot string case.
	var szStr string
	if lt == "%str-long" {
		szStr = capV
	} else {
		szStr = c.allocBytesOperand(capV, c.elemTypeOfReceiver(inst.Dst))
	}
	mp := fmt.Sprintf("%%ba%d", c.loadSeq)
	c.loadSeq++
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", mp, szStr))
	// Zero the fresh buffer. A malloc'd block is UNINITIALIZED, and for owned
	// element types (%str-long / nested %vec / %option) the slice's element slots
	// must read back as a defined empty value: `a[i] = v` drops the PREVIOUS
	// element before overwriting, and a garbage %str-long there is handed to
	// @str_free -> free(<random pointer>) -> abort trap. Legacy zeroes the buffer
	// for exactly this reason ("load undef -> icmp -> free(undef) is UB that SCCP
	// deletes the whole fn"); MIR must match. For a %str-long the element slots are
	// 1 byte, so this also makes with-len(n) a zero-filled string like legacy.
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", mp, szStr))
	if lt == "%vec" {
		dp := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dp, mp))
		s0 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, lenV))
		s1 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, capV))
		s2 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dp))
		c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, slot))
	} else {
		s0 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", s0, lenV))
		s1 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", s1, s0, capV))
		s2 := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", s2, s1, mp))
		c.sb.WriteString(fmt.Sprintf("  store %%str-long %s, %%str-long* %s\n", s2, slot))
	}
	return nil
}

// emitBuiltinEprint lowers eprint(s str): write the value's bytes to stderr
// (fd 2) followed by a newline, mirroring the legacy io.errln behavior. It
// accepts %str-long / i64 / byte(i8) / %txt; the str path writes raw bytes to
// fd 2, the i64/byte paths route through the @eprint_* runtime helpers.
func (c *codegen) emitBuiltinEprint(inst *Inst) error {
	if len(inst.Args) == 0 {
		return nil
	}
	lt, v := c.loadVal(inst.Args[0])
	switch lt {
	case "void":
		// Void operands (e.g. a unit/() expression or a void-returning call)
		// have no value to print; skip them, mirroring print's void handling.
		return nil
	case "%str-long":
		lReg := fmt.Sprintf("%%be%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 0\n", lReg, v))
		dReg := fmt.Sprintf("%%be%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", dReg, v))
		c.sb.WriteString(fmt.Sprintf("  call i64 @write(i32 2, i8* %s, i64 %s)\n", dReg, lReg))
		c.sb.WriteString("  call void @eprint_nl()\n")
		return nil
	case "i64":
		c.sb.WriteString(fmt.Sprintf("  call void @eprint_i64(i64 %s)\n", v))
		return nil
	case "i8":
		c.sb.WriteString(fmt.Sprintf("  call void @eprint_byte(i8 %s)\n", v))
		return nil
	case "%txt":
		// Build a transient %str-long { len, cap=len, data=&txt.data[0] } and
		// write it to fd 2 (mirrors the print(txt) path but to stderr).
		slot := c.valSlot[inst.Args[0]]
		if slot == "" {
			c.fail("eprint(txt) receiver has no slot in func %d", c.cf)
			return fmt.Errorf("eprint txt slot")
		}
		c.loadSeq++
		lGEP := fmt.Sprintf("%%etlg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 1\n", lGEP, slot))
		c.loadSeq++
		lLd := fmt.Sprintf("%%etll%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", lLd, lGEP))
		c.loadSeq++
		l64 := fmt.Sprintf("%%etl6%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", l64, lLd))
		c.loadSeq++
		dGEP := fmt.Sprintf("%%etdg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%txt, %%txt* %s, i32 0, i32 0, i64 0\n", dGEP, slot))
		c.loadSeq++
		dPtr := fmt.Sprintf("%%etdp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = bitcast [255 x i8]* %s to i8*\n", dPtr, dGEP))
		c.loadSeq++
		r0 := fmt.Sprintf("%%etr0%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", r0, l64))
		c.loadSeq++
		r1 := fmt.Sprintf("%%etr1%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", r1, r0, l64))
		c.loadSeq++
		r2 := fmt.Sprintf("%%etr2%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i8* %s, 2\n", r2, r1, dPtr))
		lReg := fmt.Sprintf("%%be%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 0\n", lReg, r2))
		dReg := fmt.Sprintf("%%be%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", dReg, r2))
		c.sb.WriteString(fmt.Sprintf("  call i64 @write(i32 2, i8* %s, i64 %s)\n", dReg, lReg))
		c.sb.WriteString("  call void @eprint_nl()\n")
		return nil
	default:
		c.fail("eprint expects str/i64/byte arg, got %s", lt)
		return fmt.Errorf("eprint expects str/i64/byte arg, got %s", lt)
	}
}

func (c *codegen) emitTerm(f *Function, t *Term) error {
	switch t.Op {
	case OpReturn:
		// Write result-parameter values back to the caller-provided out-pointers,
		// then return. (Result params are owned by the caller, so the callee must
		// not drop them.)
		if err := c.emitReturn(f); err != nil {
			return err
		}
		return nil
	case OpBr:
		if len(t.Targets) >= 1 {
			c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", c.labelFor[t.Targets[0]]))
			return nil
		}
	case OpCondBr:
		if len(t.Args) >= 1 && len(t.Targets) >= 2 {
			vT, v := c.loadVal(t.Args[0])
			// Nolang booleans ride on i64 (the HIR `bool` type), but LLVM `br`
			// requires a strict i1. Coerce any non-i1 condition to i1 by testing
			// non-zero: `&&`/`||` results arrive as i64, comparison results as
			// i1, and both must land as i1 here.
			if vT != "i1" {
				c.loadSeq++
				nb := fmt.Sprintf("%%cb%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", nb, vT, v))
				v = nb
			}
			c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", v, c.labelFor[t.Targets[0]], c.labelFor[t.Targets[1]]))
			return nil
		}
	}
	c.fail("malformed terminator %s", t.Op)
	return fmt.Errorf("term")
}
