package mir

import (
	"fmt"
	"math"
	"os"
	"sort"
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
	loadSeq     int // unique suffix for materialized load registers

	// externals discovered during emission: a CLibCall may reach a C symbol
	// that the prelude does not know about, so declarations are accumulated
	// here and appended to the module once every function has been emitted
	// (LLVM module order is irrelevant, forward references are legal).
	extDecls     map[string]bool
	extDeclOrder []string
	extraGlobals []string
	strConsts    map[string]string // dedupe string-constant globals by content

	// optPayload maps a per-payload inline option LLVM type (%option_<elem>)
	// to its payload field LLVM type, so print/compare code can peel field 1
	// and route it to the correct scalar printer. Populated at type-decl time.
	optPayload map[string]string
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
		mod:          m,
		fname:        map[FuncID]string{},
		labelFor:     map[BlockID]string{},
		valSlot:      map[ValueID]string{},
		paramPtr:     map[ValueID]string{},
		resultParam:  map[ValueID]bool{},
		strGlobals:   map[ValueID]strGlobal{},
		extDecls:     map[string]bool{},
		optPayload:   map[string]string{},
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
	c.emitPrelude()
	c.emitStructTypes()
	c.emitGlobals()
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
	for _, g := range c.extraGlobals {
		c.sb.WriteString(g + "\n")
	}
	for _, d := range c.extDeclOrder {
		c.sb.WriteString(d + "\n")
	}
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
	case KindOption:
		// Per-payload inline option type: ?fs.file -> %option_fs_file, ?str ->
		// %option_str, ?i64 -> %option (flat {i64,i64}). The full payload is
		// stored by-value, keeping every MIR code path self-consistent.
		if e, ok := parseOptionElem(t.Raw); ok {
			lt, _ := c.optionType(e)
			return lt
		}
		return "%option"
	case KindArray:
		// Fixed array: LLVM [N x elem]. Element type is required for the layout.
		if t.Elem != NoType && c.mod.Type(t.Elem) != nil {
			return fmt.Sprintf("[%d x %s]", sizeOfArray(t), c.llvmTypeOf(c.mod.Type(t.Elem)))
		}
		return "[0 x i64]"
	case KindPtr:
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
func (c *codegen) coerceIndex(idxT, idxV string) string {
	// An option-typed index (`a[res]` where `res ?i64 = ...`) must be unwrapped
	// to its payload before it can address memory: `%option` is a {tag,payload}
	// struct and opt rejects it as a GEP index ("defined with type '%option'
	// but expected 'i64'"). Legacy stores/reads the scalar payload for an
	// option index, so do the same (tests/test-option-index.no).
	if isOptionType(idxT) {
		c.loadSeq++
		pl := fmt.Sprintf("%%ixp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, idxT, idxV))
		idxT, idxV = "i64", pl
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

// isOptionType reports whether lt is an option LLVM type — either the flat
// scalar `%option` (= { i64, i64 }) or a per-payload inline type
// `%option_<elem>` (= { i64 tag, <payload> }).
func isOptionType(lt string) bool {
	return lt == "%option" || strings.HasPrefix(lt, "%option_")
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
	case "double":
		return "double"
	case "i64", "i8", "i1", "byte", "u8", "char", "bool", "":
		return "i64"
	}
	if strings.HasPrefix(elemRaw, "[]") {
		return "%vec"
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

// optionType returns the (LLVM option type, payload LLVM type) for an option
// whose element raw type is elemRaw. Scalar elements reuse the flat `%option =
// { i64, i64 }`; everything else gets a per-payload inline type
// `%option_<elem> = { i64 tag, <payload> }` so the payload is stored by-value
// (no heap boxing). This keeps every MIR code path (construct / move / field
// access / drop / compare / print) self-consistent.
func (c *codegen) optionType(elemRaw string) (string, string) {
	payload := c.optionPayloadLLVMType(elemRaw)
	if payload == "i64" {
		return "%option", "i64"
	}
	return "%option_" + sanitize(elemRaw), payload
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
%option = type { i64, i64 }

declare i8* @malloc(i64)
declare void @free(i8*)
declare i64 @write(i32, i8*, i64)
declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)
declare i32 @memcmp(i8*, i8*, i64)

define void @str_free(%str-long %s) {
entry:
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
  call i64 @write(i32 1, i8* %start, i64 %len)
  ret void
}

@.dot = private constant [2 x i8] c".\00"
define void @print_double(double %v) {
entry:
  %buf = alloca [40 x i8]
  %ipart = fptosi double %v to i64
  %ipartd = sitofp i64 %ipart to double
  %fpart = fsub double %v, %ipartd
  %end = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  store i8 0, i8* %end
  %sp = call i8* @digits(i64 %ipart, i8* %end)
  %ep = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  %ep_p = ptrtoint i8* %ep to i64
  %sp_p = ptrtoint i8* %sp to i64
  %len = sub i64 %ep_p, %sp_p
  call i64 @write(i32 1, i8* %sp, i64 %len)
  call i64 @write(i32 1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.dot, i64 0, i64 0), i64 1)
  %scaled = fmul double %fpart, 1.0e+06
  %fi = fptosi double %scaled to i64
  %fend = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  store i8 0, i8* %fend
  %fsp = call i8* @digits(i64 %fi, i8* %fend)
  %fend_p = ptrtoint i8* %fend to i64
  %fsp_p = ptrtoint i8* %fsp to i64
  %flen = sub i64 %fend_p, %fsp_p
  call i64 @write(i32 1, i8* %fsp, i64 %flen)
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
  %tag = extractvalue %option %o, 0
  %isnil = icmp eq i64 %tag, 1
  br i1 %isnil, label %nil, label %some
nil:
  call i64 @write(i32 1, i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.nilstr, i64 0, i64 0), i64 3)
  ret void
some:
  %inner = extractvalue %option %o, 1
  call void @print_i64(i64 %inner)
  ret void
}

; eprint_* mirror print_* but write to stderr (fd 2) and append a newline,
; matching the legacy io.errln behavior. Reuse @digits for itoa.
define void @eprint_i64(i64 %v) {
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
		if g.ConstText == "" {
			// Unfoldable initializer: emit as an external declaration. The
			// LLVM verifier rejects the undefined reference, the referencing
			// function falls back to the legacy codegen, and we never emit
			// wrong data for a crypto constant.
			lt, _ := c.ptype(g.Init)
			c.sb.WriteString(fmt.Sprintf("@%s = external constant %s\n", g.Name, lt))
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
		if owned {
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
			continue // out-params are written back by emitReturn, not aliased
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
	if lt == "void" || slot == "" {
		return "void", "undef"
	}
	c.loadSeq++
	reg := fmt.Sprintf("%%lv%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", reg, lt, lt, slot))
	return lt, reg
}

// optionTag returns an i64 register holding the option's discriminant (tag,
// field 0). nolang `opt == nil` / `opt == err` always compares the tag, not the
// whole struct, so callers (e.g. emitCmp) extract it before an integer icmp.
func (c *codegen) optionTag(v, optLT string) string {
	c.loadSeq++
	r := fmt.Sprintf("%%ot%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", r, optLT, v))
	return r
}

func (c *codegen) emitInst(f *Function, inst *Inst, allocaFor func(ValueID) string) error {
	switch inst.Op {
	case OpConst:
		return c.emitConst(inst, allocaFor)
	case OpAdd, OpSub, OpMul, OpDiv, OpMod:
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
	case OpClone:
		return c.emitClone(inst)
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
		// OpSetField instructions, so there is nothing to emit here.
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
		// (`%option_fs_file` = { i64, %fs_file }) initialize correctly.
		elem := ""
		if dt := c.mod.Value(inst.Dst); dt != nil {
			elem = optionElemRaw(c.mod.Type(dt.Type))
		}
		_, payloadLT := c.optionType(elem)
		c.sb.WriteString(fmt.Sprintf("  store %s { i64 1, %s zeroinitializer }, %s* %s\n", lt, payloadLT, lt, slot))
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

// emitOptionWrap builds an inline ?T option value { i64 tag, payload } from a
// single payload argument. It backs the nolang ?T constructors val/ok/some (tag
// 0) and err (tag 2). The discriminant comes from inst.Int (the tag set by the
// lowerer); inst.Args[0] is the payload value, whose ownership has already been
// transferred into the option by the move analysis (so it is exempt from
// dropping, and this store is the only free site). The option's LLVM type
// (%option for scalar payloads, %option_<elem> otherwise) and its payload type
// are derived from the option type's element raw type.
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
	optLT, payloadLT := c.optionType(elem)
	tag := inst.Int
	if len(inst.Args) == 0 || inst.Args[0] == NoVal {
		// No payload (bare err / val / ok / some constructor, or a zero-arg
		// call): emit the discriminant with the requested tag and a zero
		// payload. tag 0 = val/ok/some, tag 2 = err (NOT the "none" tag 1,
		// which is reserved for nil / KNilLit).
		c.sb.WriteString(fmt.Sprintf("  store %s { i64 %d, %s zeroinitializer }, %s* %s\n", optLT, tag, payloadLT, optLT, slot))
		return nil
	}
	svType, sv := c.loadVal(inst.Args[0])
	if payloadLT == "i64" {
		// scalar payload: coerce the source to i64 (e.g. i8/u8 -> i64) before
		// inserting it into the flat %option. An owned non-integer source (a
		// string error message in str.to-i64, etc.) is heap-cloned and its
		// pointer stored as the i64 payload — see optionScalarPayload.
		srcT, _ := c.ptype(inst.Args[0])
		sv = c.optionScalarPayload(srcT, sv)
		c.loadSeq++
		w1 := fmt.Sprintf("%%ow%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s { i64 0, i64 0 }, i64 %d, 0\n", w1, optLT, tag))
		c.loadSeq++
		w2 := fmt.Sprintf("%%ow%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", w2, optLT, w1, sv))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", optLT, w2, optLT, slot))
		return nil
	}
	// non-scalar payload: store the payload value inline in the per-payload
	// option type (%option_fs_file / %option_str / ...), by-value.
	//
	// Type-pun guard: nolang permits `err(msg)` / `val(x)` to wrap a value
	// whose type differs from the option's declared payload element. The
	// canonical case is `err(str-msg)` into a non-str option (?file, ?[]byte,
	// ?i64's heap-err, ...): the err message is a %str-long, but the inline
	// option slot is typed %fs_file / %vec / .... The legacy backend always
	// lowers ?T to the FLAT `%option = { i64 tag, i64 data }` and stores the
	// message's heap pointer (ptrtoint → i64) into the i64 data field, so the
	// payload bytes are type-erased and only the tag is ever inspected at
	// runtime. MIR's per-payload inline type is stricter; to keep the IR
	// verifier-happy we re-pun the source value into the declared payload type
	// via a pointer bitcast + load (the runtime only reads the tag, so the
	// reinterpreted payload bytes are never used). Without this,
	// `err(err-msg)` into `?file` emits `insertvalue %option_fs_file, %str-long
	// %lv, 1` and opt rejects it ("defined with type %str-long but expected
	// %fs_file").
	if svType != "" && svType != payloadLT {
		if srcSlot := c.valSlot[inst.Args[0]]; srcSlot != "" {
			c.loadSeq++
			bc := fmt.Sprintf("%%owbc%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to %s*\n", bc, svType, srcSlot, payloadLT))
			c.loadSeq++
			ld := fmt.Sprintf("%%owld%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", ld, payloadLT, payloadLT, bc))
			sv = ld
		}
	}
	c.loadSeq++
	w1 := fmt.Sprintf("%%ow%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, i64 %d, 0\n", w1, optLT, tag))
	c.loadSeq++
	w2 := fmt.Sprintf("%%ow%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, 1\n", w2, optLT, w1, payloadLT, sv))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", optLT, w2, optLT, slot))
	return nil
}

// optionScalarPayload coerces a constructor payload value into the i64 data
// field of a flat scalar %option ({ i64 tag, i64 data }). nolang permits ANY
// value to be wrapped by val/ok/some/err, even into a scalar option (?i64):
// an integer source coerces to i64, while an owned non-integer source (a
// string error message in str.to-i64 / str.to-u8 / ..., which all return ?X
// but raise err('msg')) is heap-cloned and its pointer stored as the i64
// payload. That mirrors the legacy backend's copyStrToData: the runtime only
// inspects the tag (is-err / match arm), so the message survives as a heap
// pointer (legacy leaks it too — acceptable for behavior parity).
func (c *codegen) optionScalarPayload(srcT, sv string) string {
	if srcT == "%str-long" {
		cl := fmt.Sprintf("%%osspc%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_clone(%s %s)\n", cl, srcT, srcT, sv))
		dp := fmt.Sprintf("%%osspd%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", dp, srcT, cl))
		pi := fmt.Sprintf("%%ospp%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pi, dp))
		return pi
	}
	if cv := c.coerce(srcT, sv, "i64"); cv != "" {
		return cv
	}
	return sv
}

// unwrapOptionOperand extracts the ok-payload of an %option operand when the
// arithmetic/comparison destination is a plain scalar. The overflow-default
// integration makes integer arithmetic yield ?T, so a variable that holds such
// a result is typed %option; when it is reused as an operand of a scalar op
// (`%c = mul i64 %x, %y` where %y is %option) opt rejects the IR as "defined
// with type %option but expected i64". Mirroring legacy, the variable
// contributes its scalar payload (tag 0 = ok), never the {tag,payload} struct.
func (c *codegen) unwrapOptionOperand(t, v, dstLT string) (string, string) {
	if t == "%option" && dstLT != "%option" && dstLT != "" {
		c.loadSeq++
		pl := fmt.Sprintf("%%opu%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, t, v))
		return dstLT, pl
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
					aT, aV = c.unwrapOptionOperand(aT, aV, resLT)
					bT, bV = c.unwrapOptionOperand(bT, bV, resLT)
				}
			}
		}
	} else {
		aT, aV = c.unwrapOptionOperand(aT, aV, lt)
		bT, bV = c.unwrapOptionOperand(bT, bV, lt)
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
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, resLT, aV, bV))
	if wrapResult {
		c.loadSeq++
		wreg := fmt.Sprintf("%%cw%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %%c%d, 1\n", wreg, lt, optionZeroLit(lt, resLT), resLT, inst.Dst))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, wreg, lt, slot))
	} else {
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	}
	return nil
}

// optionZeroLit returns an LLVM aggregate literal of the given option type with
// both fields zeroed ({ i64 0, <payload> 0 }), used as the seed for
// insertvalue when wrapping a scalar result into an option (tag 0 = ok).
func optionZeroLit(optionLT, payloadLT string) string {
	if payloadLT == "double" {
		return "{ i64 0, double 0.0 }"
	}
	return fmt.Sprintf("{ i64 0, %s 0 }", payloadLT)
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
					vT, v = c.unwrapOptionOperand(vT, v, resLT)
				}
			}
		}
	} else {
		vT, v = c.unwrapOptionOperand(vT, v, lt)
	}
	v = c.coerceInt(v, vT, resLT)
	if resLT == "double" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fneg double %s\n", inst.Dst, v))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = sub %s 0, %s\n", inst.Dst, resLT, v))
	}
	if wrapResult {
		c.loadSeq++
		wreg := fmt.Sprintf("%%cw%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %%c%d, 1\n", wreg, lt, optionZeroLit(lt, resLT), resLT, inst.Dst))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, wreg, lt, slot))
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
	// Option-vs-nil / option-vs-err comparisons compare the option's TAG
	// (discriminant, field 0), not the whole struct. A direct `icmp` on
	// %option is illegal, and nolang `opt == nil` / `opt == err` always
	// compares the discriminant, so extract it and compare as i64.
	if isOptionType(aT) {
		aV = c.optionTag(aV, aT)
		aT = "i64"
	}
	if isOptionType(bT) {
		bV = c.optionTag(bV, bT)
		bT = "i64"
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
		c.sb.WriteString(fmt.Sprintf("  %%c%d = call i1 @str_eq(%s %s, %s %s)\n", inst.Dst, aT, aV, bT, bV))
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
			fop = "one"
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
	_, bV := c.loadVal(inst.Args[1])
	c.sb.WriteString(fmt.Sprintf("  %%c%d = call i1 @str_eq(%s %s, %s %s)\n", inst.Dst, aT, aV, aT, bV))
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
					aT, aV = c.unwrapOptionOperand(aT, aV, resLT)
					bT, bV = c.unwrapOptionOperand(bT, bV, resLT)
				}
			}
		}
	} else {
		aT, aV = c.unwrapOptionOperand(aT, aV, lt)
		bT, bV = c.unwrapOptionOperand(bT, bV, lt)
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
		c.loadSeq++
		wreg := fmt.Sprintf("%%cw%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %%c%d, 1\n", wreg, lt, optionZeroLit(lt, resLT), resLT, inst.Dst))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, wreg, lt, slot))
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
		// scalar: nothing to free
	}
	return nil
}

// emitOptionDrop frees an option's owned heap element. The element type is read
// from the option value's nolang raw type (?str / ?vec / ?T) so we free with the
// right helper; unknown/non-heap elements are left alone (safe no-op). optLT is
// the option's concrete LLVM type (%option or %option_<elem>).
func (c *codegen) emitOptionDrop(inst *Inst, v, optLT string) {
	elemRaw := ""
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil && t.Raw != "" {
			if e, ok := parseOptionElem(t.Raw); ok {
				elemRaw = e
			}
		}
	}
	switch elemRaw {
	case "str":
		// Per-payload inline option: field 1 is a by-value %str-long. Free its
		// heap data buffer (field 2) directly.
		c.loadSeq++
		pl := fmt.Sprintf("%%optpl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, optLT, v))
		c.loadSeq++
		pd := fmt.Sprintf("%%optpd%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%str-long %s, 2\n", pd, pl))
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", pd))
	case "vec":
		// Per-payload inline option: field 1 is a by-value %vec. Free its heap
		// data buffer (field 2) directly.
		c.loadSeq++
		pl := fmt.Sprintf("%%optpl%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, optLT, v))
		c.loadSeq++
		pd := fmt.Sprintf("%%optpd%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%vec %s, 2\n", pd, pl))
		c.sb.WriteString(fmt.Sprintf("  call void @free(i8* %s)\n", pd))
	default:
		// ?i64 / ?bool / user-struct / unrecognized element: nothing heap-owned
		// to free (or a shallow no-op for structs; deep-free is a follow-up).
		c.sb.WriteString(fmt.Sprintf("  ; drop %s (option element %q owns nothing / shallow)\n", v, elemRaw))
	}
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
		return fmt.Errorf("move src slot")
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
	// Option-aware move. nolang `x ?t = y` wraps y into some(y); the MIR
	// subset models options with per-payload inline types (%option for scalars,
	// %option_<elem> = { i64 tag, <payload> } for non-scalars), so the payload
	// is stored by-value. Conversely `x = opt` (an option source into a
	// non-option dst) extracts the payload field (field 1). A plain
	// `load %option, %option* srcSlot` would misread the payload's bits as the
	// discriminant and turn some(v) into nil / none into some(0).
	if isOptionType(dstT) && !isOptionType(srcT) {
		dstRaw := ""
		if dt := c.mod.Value(dstVal); dt != nil {
			if t := c.mod.Type(dt.Type); t != nil {
				dstRaw = t.Raw
			}
		}
		elem, _ := parseOptionElem(dstRaw)
		optLT, payloadLT := c.optionType(elem)
		_, sv := c.loadVal(inst.Args[0])
		if payloadLT == "i64" {
			// scalar payload: coerce to i64 and insertvalue into the flat %option.
			// An owned non-integer source (string error message into a scalar
			// option) is heap-cloned and its pointer stored — see
			// optionScalarPayload.
			sv = c.optionScalarPayload(srcT, sv)
			c.loadSeq++
			w1 := fmt.Sprintf("%%mvw%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option { i64 0, i64 0 }, i64 0, 0\n", w1))
			c.loadSeq++
			w2 := fmt.Sprintf("%%mvw%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%option %s, i64 %s, 1\n", w2, w1, sv))
			c.sb.WriteString(fmt.Sprintf("  store %%option %s, %%option* %s\n", w2, dstSlot))
			return nil
		}
		// non-scalar payload: store the payload value inline in the per-payload
		// option type (%option_fs_file / %option_str / ...).
		c.loadSeq++
		w1 := fmt.Sprintf("%%mvw%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, i64 0, 0\n", w1, optLT))
		c.loadSeq++
		w2 := fmt.Sprintf("%%mvw%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, 1\n", w2, optLT, w1, payloadLT, sv))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", optLT, w2, optLT, dstSlot))
		return nil
	}
	if !isOptionType(dstT) && isOptionType(srcT) {
		srcRaw := ""
		if st := c.mod.Value(inst.Args[0]); st != nil {
			if t := c.mod.Type(st.Type); t != nil {
				srcRaw = t.Raw
			}
		}
		elem, _ := parseOptionElem(srcRaw)
		optLT, payloadLT := c.optionType(elem)
		_, sv := c.loadVal(inst.Args[0])
		if payloadLT == "i64" {
			u1 := fmt.Sprintf("%%mvu%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", u1, optLT, sv))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, u1, dstT, dstSlot))
			return nil
		}
		u1 := fmt.Sprintf("%%mvu%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", u1, optLT, sv))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", payloadLT, u1, payloadLT, dstSlot))
		return nil
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
	// Use a unique temp name (%mv<instID>) because Dst is an existing variable
	// slot already defined by its original binding, so reusing %c<Dst> would
	// collide.
	tmp := fmt.Sprintf("%%mv%d", inst.ID)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", tmp, dstT, dstT, srcSlot))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, tmp, dstT, dstSlot))
	return nil
}

func (c *codegen) emitClone(inst *Inst) error {
	if lt, _ := c.ptype(inst.Args[0]); lt == "%str-long" {
		dstT, _ := c.ptype(inst.Dst)
		dstSlot := c.valSlot[inst.Dst]
		_, srcV := c.loadVal(inst.Args[0])
		tmp := fmt.Sprintf("%%cl%d", inst.ID)
		c.sb.WriteString(fmt.Sprintf("  %s = call %s @str_concat(%s %s, %s %s)\n", tmp, dstT, dstT, srcV, dstT, srcV))
		c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", dstT, tmp, dstT, dstSlot))
		return nil
	}
	return c.emitMove(inst)
}

// elemAddr computes the address of element `idx` of a container value held in
// arrSlot. The container layout depends on arrT:
//   - %str-long {i64, i64, i8*} : data is field 2 (i8*); index a byte.
//   - %vec      {i64, i64, i64} : data is field 2 (pointer stored as i64);
//                            inttoptr to the element-type pointer, then index.
//   - fixed array [N x T]      : the array type is its own element type; a
//                            single-level GEP does the right thing.
// Returns the element pointer register (typed `<elemT>*`).
func (c *codegen) elemAddr(arrSlot, idxV, arrT, elemT string) string {
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

// elemByteSize returns the byte width of an LLVM element type stored inside a
// %vec / fixed array. Used to size a freshly materialized backing buffer.
func elemByteSize(t string) int64 {
	switch t {
	case "i1", "i8":
		return 1
	case "i16":
		return 2
	case "i32":
		return 4
	case "i64":
		return 8
	case "%str-long", "%vec", "%option":
		return 24
	default:
		return 8
	}
}

// ensureVecBuffer lazily allocates a backing buffer for an empty %vec whose data
// pointer is still null (declared with no capacity), so that a `buf[i] = x` store
// never writes through a null pointer. The MIR %vec layout is {i64 len, i64 cap,
// i64 data} where data is stored as an i64 (inttoptr'd at use).
//
// Mirrors the legacy backend, which materializes a real buffer (default cap 1024)
// for an empty []byte at declaration time. We allocate lazily (only on the first
// write) to avoid over-allocating slices that are only grown via push, but the
// allocation is guarded by a data==0 check so it happens at most once per slot
// (no leak). A borrowed view (string->[]byte coercion, cap==0 but a non-null
// data pointing at constant memory) keeps its non-null data and is left alone.
func (c *codegen) ensureVecBuffer(arrSlot string, v ValueID, idxV, elemT string) {
	const vecDefaultCap = int64(1024)
	sizeBytes := vecDefaultCap * elemByteSize(elemT)

	// Load the data field (field 2) and branch if it is still null.
	c.loadSeq++
	dg := fmt.Sprintf("%%vbdg%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", dg, arrSlot))
	c.loadSeq++
	di := fmt.Sprintf("%%vbdi%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", di, dg))
	c.loadSeq++
	isnull := fmt.Sprintf("%%vbnull%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, 0\n", isnull, di))

	c.loadSeq++
	lAlloc := fmt.Sprintf("vbA%d", c.loadSeq)
	lDone := fmt.Sprintf("vbD%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isnull, lAlloc, lDone))

	// alloc block: malloc the default buffer, store data + cap back into the slot.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lAlloc))
	c.loadSeq++
	buf := fmt.Sprintf("%%vbbuf%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", buf, sizeBytes))
	c.loadSeq++
	ptri := fmt.Sprintf("%%vbptri%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", ptri, buf))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", ptri, dg))
	// cap is field 1
	c.loadSeq++
	cg := fmt.Sprintf("%%vbcap%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", cg, arrSlot))
	c.sb.WriteString(fmt.Sprintf("  store i64 %d, i64* %s\n", vecDefaultCap, cg))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", lDone))

	// done block: merge point; the caller's subsequent store lands here.
	c.sb.WriteString(fmt.Sprintf("%s:\n", lDone))
	_ = v
	_ = idxV
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
		switch t.Kind {
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
		}
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
	idxV = c.coerceIndex(idxVT, idxV)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		dt, _ := c.ptype(inst.Dst)
		c.fail("index result has no slot in func %s (dst=%d type=%s) arrT=%s elemT=%s", c.fname[c.cf], inst.Dst, dt, arrT, elemT)
		return fmt.Errorf("index dst slot (dst=%d type=%s)", inst.Dst, dt)
	}
	ep := c.elemAddr(arrSlot, idxV, arrT, elemT)
	c.loadSeq++
	lv := fmt.Sprintf("%%lx%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, elemT, elemT, ep))
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
	_, idxV := c.loadVal(inst.Args[1])
	idxVT, _ := c.ptype(inst.Args[1])
	idxV = c.coerceIndex(idxVT, idxV)
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
	ep := c.elemAddr(arrSlot, idxV, arrT, elemT)
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
	} else if strings.HasPrefix(valT, "%option") && elemT != "%str-long" && elemT != "%vec" && elemT != "%option" {
		// Overflow-default integer arithmetic yields ?i64 (an %option). Assigning
		// it to a plain scalar element (`a[i] = a[j]+a[k]` with a:[N]i64) unwraps
		// the ok payload, matching the legacy codegen (which stores the scalar
		// payload, not the whole {tag,payload} struct). Without this, opt rejects
		// the store as "defined with type '%option' but expected 'i64'" and the
		// build fails under NOLANG_MIR=3 (regression of test-arr.no).
		c.loadSeq++
		pl := fmt.Sprintf("%%opay%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, valT, valV))
		if cv2 := c.coerce("i64", pl, elemT); cv2 != "" {
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
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			recvRaw = t.Raw
			recvLT = c.llvmTypeOf(t)
		}
	}
	if recvLT == "" {
		recvLT = "%" + sanitize(recvRaw)
	}
	if isOptionType(recvLT) {
		// `?T.field`: peel the option (field 1 holds the inline payload of type
		// payloadLT), then GEP into the inner struct's field.
		elem, _ := parseOptionElem(recvRaw)
		_, payloadLT := c.optionType(elem)
		innerRaw := elem
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
		c.loadSeq++
		pg := fmt.Sprintf("%%opg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", pg, recvLT, recvLT, recvSlot))
		c.loadSeq++
		gp := fmt.Sprintf("%%gp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gp, payloadLT, payloadLT, pg, idx))
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
		if strings.HasPrefix(recvLT, "%option") && fieldLT != "%str-long" && fieldLT != "%vec" && fieldLT != "%option" {
			c.loadSeq++
			pl := fmt.Sprintf("%%opay%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, recvLT, valV))
			if cv2 := c.coerce("i64", pl, fieldLT); cv2 != "" {
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
		c.loadSeq++
		pg := fmt.Sprintf("%%opg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", pg, recvLT, recvLT, recvSlot))
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
	if strings.HasPrefix(valLT, "%option") && fieldLT != "%str-long" && fieldLT != "%vec" && fieldLT != "%option" {
		c.loadSeq++
		pl := fmt.Sprintf("%%opay%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, valLT, valV))
		if cv2 := c.coerce("i64", pl, fieldLT); cv2 != "" {
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
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldLT, valV, fieldLT, gep))
	return nil
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
	stride := int64(8)
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
				stride = elemStride(c.llvmTypeOf(et))
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
		stride = 1
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
					stride = elemStride(c.llvmTypeOf(et))
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

	newLen := c.treg("sonl")
	c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newLen, hiV, loV))

	// Byte offset of the sub-range start inside the backing buffer.
	var off string
	if stride == 1 {
		off = loV
	} else {
		loBytes := c.treg("solb")
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", loBytes, loV, stride))
		off = loBytes
	}
	// Total byte count to copy.
	var bytes string
	if stride == 1 {
		bytes = newLen
	} else {
		nlBytes := c.treg("sonb")
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", nlBytes, newLen, stride))
		bytes = nlBytes
	}

	srcBase := c.treg("sosb")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", srcBase, srcPtr, off))

	// Fixed-array receiver sliced into a %vec: a slice result can ESCAPE the
	// array's lifetime (e.g. returned from a function, or stored in a variable
	// that outlives the array). Building a borrow VIEW over the array's storage
	// (data = &arr[lo], cap = len-lo) is unsafe in that case: the backing array
	// dies, leaving a dangling pointer, and `@vec_free` would `free()` a stack/
	// borrowed address (abort trap). So we ALWAYS materialize an owned heap copy
	// here — exactly like the %vec and %str-long receiver paths below. This also
	// matches the legacy backend, which copies the sub-range into a heap buffer.
	// (The transient borrow views used for fixed-array -> %vec *receiver* coercion
	// at call sites are a separate mechanism with cap=0 and are NOT affected.)

	// Fresh backing buffer + copy the sub-range (uniform ownership model: the
	// slice gets its own copy and never aliases the source).
	newBuf := c.treg("sonb2")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newBuf, bytes))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", newBuf, srcBase, bytes))

	if dstLT == "%str-long" {
		s0 := c.treg("sos0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long { i64 0, i64 0, i8* null }, i64 %s, 0\n", s0, newLen))
		s1 := c.treg("sos1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%str-long %s, i64 %s, 1\n", s1, s0, newLen))
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
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, newLen))
		s2 := c.treg("sos2")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dp))
		c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, dstSlot))
	}
	return nil
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
			parts = append(parts, c.llvmTypeOf(c.mod.Type(tid)))
		}
		c.sb.WriteString(fmt.Sprintf("%s = type { %s }\n", lt, strings.Join(parts, ", ")))
	}
	// Declare per-payload inline option types: %option_<elem> = { i64 tag,
	// <payload> }. Scalar options (?i64, ?bool, ...) keep the flat `%option`
	// ({ i64, i64 }) already in the prelude; only non-scalar payloads (str/vec/
	// user-struct/fixed-array) get a dedicated type so the payload is stored
	// by-value. Dedup by type name.
	seenOpt := map[string]bool{}
	for _, t := range c.mod.Types {
		if t.Kind != KindOption {
			continue
		}
		elem, ok := parseOptionElem(t.Raw)
		if !ok {
			continue
		}
		optLT, payloadLT := c.optionType(elem)
		if optLT == "%option" {
			continue
		}
		if seenOpt[optLT] {
			continue
		}
		seenOpt[optLT] = true
		c.optPayload[optLT] = payloadLT
		c.sb.WriteString(fmt.Sprintf("%s = type { i64, %s }\n", optLT, payloadLT))
	}
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
				// inline options (%option_<elem>) the payload is by-value, so we
				// peel field 1 and print it with the matching scalar printer —
				// the payload is NOT always i64 (a ?str option carries a
				// %str-long payload, a ?User option a %User struct payload), so
				// routing to print_i64 unconditionally is wrong and trips the
				// LLVM verifier (e.g. `%optpl19` defined as %str-long but
				// expected i64 at `call @print_i64`).
				if argT == "%option" {
					c.sb.WriteString(fmt.Sprintf("  call void @print_option(%s %s)\n", argT, argV))
				} else {
					c.loadSeq++
					pl := fmt.Sprintf("%%optpl%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", pl, argT, argV))
					// Route the peeled payload to the correct scalar printer by
					// its LLVM type (optPayload is populated at type-decl time).
					payloadLT := c.optPayload[argT]
					if payloadLT == "" {
						payloadLT = "i64"
					}
					switch payloadLT {
					case "%str-long":
						c.sb.WriteString(fmt.Sprintf("  call void @print_str(%s %s)\n", payloadLT, pl))
					case "double":
						c.sb.WriteString(fmt.Sprintf("  call void @print_double(%s %s)\n", payloadLT, pl))
					case "i64":
						c.sb.WriteString(fmt.Sprintf("  call void @print_i64(%s %s)\n", payloadLT, pl))
					case "i8":
						c.loadSeq++
						z := fmt.Sprintf("%%optz%d", c.loadSeq)
						c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", z, pl))
						c.sb.WriteString(fmt.Sprintf("  call void @print_i64(i64 %s)\n", z))
					case "i1":
						c.sb.WriteString(fmt.Sprintf("  call void @print_bool(%s %s)\n", payloadLT, pl))
					default:
						// %vec / user-struct / fixed-array payload has no scalar
						// printer in MIR yet; legacy prints via to-str. Skip
						// rather than crash the verifier.
						c.fail("print of option payload type %s unsupported in func %s", payloadLT, f.Name)
					}
				}
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
				// byte: zero-extend to i64 and print the integer value, matching
				// legacy print-byte (fmt-int of the zext).
				c.loadSeq++
				z := fmt.Sprintf("%%ptz%d", c.loadSeq)
				c.sb.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", z, argV))
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
		return fmt.Errorf("unknown callee %s", callee)
	}
	cf := c.mod.Func(cid)
	if cf == nil {
		c.fail("callee %s unresolved in func %s", callee, f.Name)
		return fmt.Errorf("callee nil")
	}
	return c.emitCallBody(f, cf, inst, "@"+c.fname[cid])
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
	total := n * elemStride(elemT)
	buf := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", buf, total))
	src := c.treg("bva")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 0\n", src, argT, argT, arrSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n", buf, src, total))
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
	variadicLast := false
	if nIn := len(inParams); nIn >= 1 && len(inst.Args) > nIn {
		lastP := inParams[nIn-1]
		if pv := c.mod.Value(lastP); pv != nil {
			if pt := c.mod.Type(pv.Type); pt != nil && pt.Kind == KindSlice {
				variadicLast = true
			}
		}
	}
	for i, p := range inParams {
		if variadicLast && i == len(inParams)-1 {
			// Collect every remaining call argument into a borrow %vec view and
			// pass it as the variadic (slice) parameter. The element LLVM type
			// is taken from the first collected argument's loaded type.
			var argLLVMs []string
			elemLLT := "i64"
			for j := i; j < len(inst.Args); j++ {
				at, av := c.loadVal(inst.Args[j])
				if j == i && at != "" {
					elemLLT = at
				}
				argLLVMs = append(argLLVMs, av)
			}
			callArgs = append(callArgs, c.buildVecViewFromValues(elemLLT, argLLVMs))
			continue
		}
		plt, owned := c.ptype(p)
		if i >= len(inst.Args) {
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
		argT, av := c.loadVal(inst.Args[i])
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
		// Method receiver (first input parameter): pass the receiver's *real slot
		// address* by reference so self mutations (len/cap fields, elements)
		// propagate to the caller, matching legacy codegen (which GEPs the
		// %self pointer in place). The default owned-param path below copies the
		// value into a fresh alloca and passes that address, which silently
		// discards self mutations for container methods (vec.insert / vec.remove
		// / …) under the MIR backend — the element writes still hit the shared
		// backing buffer, but the caller's len/cap never update, corrupting
		// subsequent indexing.
		if cf.IsMethod && i == 0 {
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
					c.loadSeq++
					pg := fmt.Sprintf("%%optrcv%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n", pg, recvLT, recvLT, rs))
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
		if cf.IsMethod && i == 0 && (owned || byPointerLLVM(plt, owned)) {
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
				arrSlot := c.valSlot[inst.Args[i]]
				var n int64
				if at := c.mod.Value(inst.Args[i]); at != nil {
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
				if argSlot := c.valSlot[inst.Args[i]]; argSlot != "" {
					callArgs = append(callArgs, plt+"* "+argSlot)
				} else {
					c.loadSeq++
					slot := fmt.Sprintf("%%carg%d", c.loadSeq)
					c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
					c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
					callArgs = append(callArgs, plt+"* "+slot)
				}
			} else {
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
			argSlot := c.valSlot[inst.Args[i]]
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
	for _, p := range outParams {
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
	for i, rs := range resSlots {
		if i >= len(inst.Results) {
			break
		}
		rv := inst.Results[i]
		if rv <= NoVal {
			continue
		}
		rlt, _ := c.ptype(rv)
		c.sb.WriteString(fmt.Sprintf("  %%c%d = load %s, %s* %s\n", rv, rlt, rlt, rs))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", rlt, rv, rlt, c.valSlot[rv]))
	}
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
	for _, rp := range f.ResultParams {
		plt, _ := c.ptype(rp)
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
	stride := int64(8)
	if lt == "%str-long" {
		stride = 1
	}
	var szStr string
	if stride == 1 {
		szStr = capV
	} else {
		sz := fmt.Sprintf("%%ba%d", c.loadSeq)
		c.loadSeq++
		c.sb.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", sz, capV, stride))
		szStr = sz
	}
	mp := fmt.Sprintf("%%ba%d", c.loadSeq)
	c.loadSeq++
	c.sb.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", mp, szStr))
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
