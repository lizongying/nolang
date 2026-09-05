package mir

import (
	"fmt"
	"math"
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
	loadSeq     int // unique suffix for materialized load registers

	// externals discovered during emission: a CLibCall may reach a C symbol
	// that the prelude does not know about, so declarations are accumulated
	// here and appended to the module once every function has been emitted
	// (LLVM module order is irrelevant, forward references are legal).
	extDecls     map[string]bool
	extDeclOrder []string
	extraGlobals []string
	strConsts    map[string]string // dedupe string-constant globals by content
}

// ptype returns the LLVM type string for a MIR value and whether it is owned.
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

func sizeOfArray(t *Type) int64 {
	if len(t.Sizes) > 0 {
		return t.Sizes[0]
	}
	return 0
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
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.ID == NoFunc {
			continue
		}
		// user `main` collides with the C entry `i32 @main`; rename it.
		if f.Name == "main" {
			c.fname[f.ID] = "_nolang_main"
		} else {
			c.fname[f.ID] = f.Name
		}
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
	if _, ok := c.fname[findMainFunc(m)]; ok {
		c.emitEntry()
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
		return "%option"
	case KindArray:
		// Fixed array: LLVM [N x elem]. Element type is required for the layout.
		if t.Elem != NoType && c.mod.Type(t.Elem) != nil {
			return fmt.Sprintf("[%d x %s]", sizeOfArray(t), c.llvmTypeOf(c.mod.Type(t.Elem)))
		}
		return "[0 x i64]"
	case KindPtr:
		return "i8*"
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
	switch idxT {
	case "i8", "i1", "i16", "i32":
		c.loadSeq++
		r := fmt.Sprintf("%%izx%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", r, idxT, idxV))
		return r
	}
	return idxV
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
  call i64 @write(i32 1, i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.true, i64 0, i64 0), i64 4)
  ret void
f:
  call i64 @write(i32 1, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.false, i64 0, i64 0), i64 5)
  ret void
}

@.true = private constant [4 x i8] c"true"
@.false = private constant [5 x i8] c"false"
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
		c.sb.WriteString(fmt.Sprintf("@%s = private constant %s\n", g.Name, g.ConstText))
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

func (c *codegen) emitEntry() {
	c.sb.WriteString("define i32 @main(i32 %0, i8** %1) {\nentry:\n")
	c.sb.WriteString("  call void @_nolang_main()\n")
	c.sb.WriteString("  ret i32 0\n}\n")
}

// ---- function emission ----

func (c *codegen) emitFunc(f *Function) error {
	c.cf = f.ID
	c.resultParam = map[ValueID]bool{}
	for _, rp := range f.ResultParams {
		c.resultParam[rp] = true
	}

	// Reject unsupported types (struct/vec/option/ptr) for a clean fallback.
	for _, p := range f.Params {
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
		pn := fmt.Sprintf("%%p%d", idx)
		if c.resultParam[p] {
			decls = append(decls, lt+"* "+pn)
		} else if owned {
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
	// store incoming params into slots (skip out-params)
	for _, p := range f.Params {
		if c.resultParam[p] {
			continue
		}
		lt, owned := c.ptype(p)
		slot := c.valSlot[p]
		if lt == "void" || slot == "" {
			continue
		}
		pin := c.paramPtr[p]
		if owned {
			// owned params arrive as pointers; load the struct value, then store.
			c.sb.WriteString(fmt.Sprintf("  %%p2v%d = load %s, %s* %s\n", p, lt, lt, pin))
			c.sb.WriteString(fmt.Sprintf("  store %s %%p2v%d, %s* %s\n", lt, p, lt, slot))
		} else {
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", lt, pin, lt, slot))
		}
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
func (c *codegen) optionTag(v string) string {
	c.loadSeq++
	r := fmt.Sprintf("%%ot%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%option %s, 0\n", r, v))
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
	case OpTxtFromStr:
		return c.emitTxtFromStr(inst)
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

func (c *codegen) emitArith(f *Function, inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// string concat if both operands are %str-long
	if lt == "%str-long" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = call %s @str_concat(%s %s, %s %s)\n", inst.Dst, lt, lt, aV, lt, bV))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
		return nil
	}
	// Coerce both operands to the result type: a str-byte (i8) arithmetic like
	// `c - 32` feeds an i8 literal (i64) into an i8 op; mismatched widths are a
	// hard LLVM type error. Truncate/extend so the operation type matches.
	aV = c.coerceInt(aV, aT, lt)
	bV = c.coerceInt(bV, bT, lt)
	var op string
	switch inst.Op {
	case OpAdd:
		op = "add"
	case OpSub:
		op = "sub"
	case OpMul:
		op = "mul"
	case OpDiv:
		op = "sdiv"
	case OpMod:
		op = "srem"
	}
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, lt, aV, bV))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

func (c *codegen) emitNeg(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	vT, v := c.loadVal(inst.Args[0])
	v = c.coerceInt(v, vT, lt)
	if lt == "double" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fneg double %s\n", inst.Dst, v))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = sub %s 0, %s\n", inst.Dst, lt, v))
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
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

func (c *codegen) emitCmp(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	bT, bV := c.loadVal(inst.Args[1])
	// Option-vs-nil / option-vs-err comparisons compare the option's TAG
	// (discriminant, field 0), not the whole struct. A direct `icmp` on
	// %option is illegal, and nolang `opt == nil` / `opt == err` always
	// compares the discriminant, so extract it and compare as i64.
	if aT == "%option" {
		aV = c.optionTag(aV)
		aT = "i64"
	}
	if bT == "%option" {
		bV = c.optionTag(bV)
		bT = "i64"
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
		aV = c.coerceInt(aV, aT, "double")
		bV = c.coerceInt(bV, bT, "double")
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fcmp o%s double %s, %s\n", inst.Dst, op, aV, bV))
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
	aV = c.coerceInt(aV, aT, lt)
	bV = c.coerceInt(bV, bT, lt)
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
	c.sb.WriteString(fmt.Sprintf("  %%c%d = %s %s %s, %s\n", inst.Dst, op, lt, aV, bV))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

func (c *codegen) emitDrop(inst *Inst) error {
	lt, _ := c.ptype(inst.Args[0])
	_, v := c.loadVal(inst.Args[0])
	switch lt {
	case "%str-long":
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", lt, v))
	case "%vec":
		// Real drop: free the backing store (data pointer at field 2). The %vec
		// struct is by-value and needs no free.
		c.sb.WriteString(fmt.Sprintf("  call void @vec_free(%s %s)\n", lt, v))
	case "%option":
		// Real drop for an option that owns a heap element. %option = { i64 tag,
		// i64 payload }. Field 1 is a pointer (stored as i64) to the owned value
		// when tag != 0. Free it according to the element type; scalar options
		// (?i64, ?bool, ...) own nothing and stay a no-op (correct).
		c.emitOptionDrop(inst, v)
	default:
		// scalar: nothing to free
	}
	return nil
}

// emitOptionDrop frees an option's owned heap element. The element type is read
// from the option value's nolang raw type (?str / ?vec / ?T) so we free with the
// right helper; unknown/non-heap elements are left alone (safe no-op).
func (c *codegen) emitOptionDrop(inst *Inst, v string) {
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
		c.loadSeq++
		p := fmt.Sprintf("%%optp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%option %s, 1\n", p, v))
		c.loadSeq++
		sp := fmt.Sprintf("%%optsp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %%str-long*\n", sp, p))
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%%str-long *%s)\n", sp))
	case "vec":
		c.loadSeq++
		p := fmt.Sprintf("%%optp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = extractvalue %%option %s, 1\n", p, v))
		c.loadSeq++
		sp := fmt.Sprintf("%%optsp%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %%vec*\n", sp, p))
		c.sb.WriteString(fmt.Sprintf("  call void @vec_free(%%vec *%s)\n", sp))
	default:
		// ?i64 / ?bool / unrecognized element: nothing heap-owned to free.
		c.sb.WriteString(fmt.Sprintf("  ; drop %s (option element %q owns nothing)\n", v, elemRaw))
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
	default:
		p := c.treg("ep")
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 0, i64 %s\n", p, arrT, arrT, arrSlot, idxV))
		return p
	}
}

// elemTypeOfReceiver returns the LLVM *element* type for indexing/store into a
// slice/array/str receiver. The byte-addressed backing store of a %vec (slice)
// or %str-long means the element type is the slice's declared element — i8 for
// []byte, i64 for []i64 — and NEVER the value being stored or the destination
// variable's type. Writing d0:i64 into a []byte must store ONE byte (truncating
// to i8) at offset i; using the value's i64 type would instead store 8 bytes at
// offset i*8 and overrun the array (the AES stack-corruption crash).
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
		c.fail("index result has no slot in func %d", c.cf)
		return fmt.Errorf("index dst slot")
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
	_, owned := c.ptype(inst.Args[2])
	arrSlot := c.valSlot[inst.Args[0]]
	if arrSlot == "" {
		c.fail("index-store of value with no slot in func %d", c.cf)
		return fmt.Errorf("indexstore slot")
	}
	_, idxV := c.loadVal(inst.Args[1])
	idxVT, _ := c.ptype(inst.Args[1])
	idxV = c.coerceIndex(idxVT, idxV)
	ep := c.elemAddr(arrSlot, idxV, arrT, elemT)
	if owned {
		c.loadSeq++
		old := fmt.Sprintf("%%old%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", old, elemT, elemT, ep))
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", elemT, old))
	}
	// Truncate/extend the stored value to the element type (e.g. i64 -> i8 for a
	// []byte, which keeps the store to a single byte at offset i).
	if cv := c.coerce(valT, valV, elemT); cv != "" {
		valV = cv
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemT, valV, elemT, ep))
	if arrT == "%str-long" {
		// Writing a byte may extend the logical length (a freshly with-cap'd
		// str has len=0, but its data buffer has cap bytes). Mirror legacy:
		// len = max(len, idx+1) so the written region becomes visible to callers
		// (print_str reads field 0 as the length). Without this, `val[i] = c`
		// inside a loop like str.to-upper leaves len=0 and prints nothing.
		c.loadSeq++
		lenGep := fmt.Sprintf("%%slg%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", lenGep, arrSlot))
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
	return nil
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
	if val := c.mod.Value(inst.Args[0]); val != nil {
		if t := c.mod.Type(val.Type); t != nil {
			recvRaw = t.Raw
			recvLT = c.llvmTypeOf(t)
		}
	}
	if recvLT == "" {
		recvLT = "%" + sanitize(recvRaw)
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
	structLT := recvLT
	fieldLT, _ := c.ptype(inst.Args[1])
	if fieldLT == "" {
		fieldLT = "i64"
	}
	_, valV := c.loadVal(inst.Args[1])
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

	// Aliasing view for a fixed-array receiver sliced into a %vec: nolang treats
	// slice-of-array as a VIEW over the array's own storage, so mutations through
	// the slice reach the original array (e.g. sub-bytes(out[..]) mutating out).
	// The view owns no backing buffer, so it must never be vec_free'd — and the
	// HIR does not emit a drop for slice-of-array temporaries (verified: the AES
	// IR contains no vec_free calls), so this is safe.
	if isFixedArray && dstLT == "%vec" {
		dp := c.treg("sodp")
		c.sb.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", dp, srcBase))
		capV := c.treg("socap")
		c.sb.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", capV, rlen, loV))
		s0 := c.treg("sos0")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec { i64 0, i64 0, i64 0 }, i64 %s, 0\n", s0, newLen))
		s1 := c.treg("sos1")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 1\n", s1, s0, capV))
		s2 := c.treg("sos2")
		c.sb.WriteString(fmt.Sprintf("  %s = insertvalue %%vec %s, i64 %s, 2\n", s2, s1, dp))
		c.sb.WriteString(fmt.Sprintf("  store %%vec %s, %%vec* %s\n", s2, dstSlot))
		return nil
	}

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

// arraySizeOf parses the leading "[N x E]" of an LLVM fixed-array type string and
// returns N (the element count). It is used to derive the length of a slice view
// built over a fixed array (fixed-array -> vec coercion at call sites).
func arraySizeOf(lt string) (int64, bool) {
	if len(lt) < 3 || lt[0] != '[' {
		return 0, false
	}
	end := strings.IndexByte(lt, ']')
	if end < 0 {
		return 0, false
	}
	n := int64(0)
	for i := 1; i < end; i++ {
		c := lt[i]
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
}

// emitTxtFromStr lowers `OpTxtFromStr`: copy a %str-long's bytes into the
// fixed 256-byte %txt buffer (data[0..min(len,255)]) and store the length as
// an i8. The destination %txt slot is pre-allocated by the prologue. This is
// the MIR analogue of the legacy str->txt conversion in build/llvm/stmt.go.
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
			case "%option":
				c.sb.WriteString(fmt.Sprintf("  call void @print_option(%s %s)\n", argT, argV))
			default:
				// Unknown scalar type: skip rather than fail the build (matches
				// legacy's graceful handling of print of exotic values).
				c.fail("print unsupported arg type %s in func %s", argT, f.Name)
			}
		}
		c.sb.WriteString("  call void @print_nl()\n")
		return nil
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
	for i, p := range inParams {
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
			c.loadSeq++
			slot := fmt.Sprintf("%%carg%d", c.loadSeq)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
			callArgs = append(callArgs, plt+"* "+slot)
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
	c.sb.WriteString(fmt.Sprintf("  call void @%s(%s)\n", c.fname[cid], strings.Join(callArgs, ", ")))
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
