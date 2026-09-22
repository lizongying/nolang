// Package mir defines the Nolang mid-level intermediate representation.
//
// MIR sits between HIR (a de-pointered structural clone of the surface AST) and
// the LLVM backend. Unlike HIR it is explicitly control-flow-aware and
// memory-aware: every value carries an ownership tag (Owned/Borrowed), every
// function is a graph of basic blocks, and memory operations (Move/Clone/Drop/
// Borrow) are first-class instructions. This lets the compiler reason about and
// verify memory safety (no double-free, no use-after-move, exactly-one-drop)
// in one place instead of scattering the logic across a 450KB emitter.
//
// Design contract (mirrored from src/hir): the module is a set of flat slices
// indexed by typed IDs (FuncID, BlockID, InstID, ValueID, TypeID). There are no
// Go pointers between nodes, so the GC cost is proportional to the handful of
// slices the module owns. ID 0 of every slice is a reserved nil element.
//
// Dependency direction: mir depends on parser/hir only (hir -> mir). It does NOT
// import build/llvm, so it can be tested in isolation.
package mir

import (
	"os"
	"strings"
)

// FieldPtrLayout enables the Phase 1 layout flip: a struct-typed field whose
// semantic tag is Owned/Borrow is laid out as a POINTER to its pointee (%T*)
// instead of inlining the pointee by value.
//
// It is behind a switch purely for ATTRIBUTION. The workspace carries other
// people's uncommitted changes, so the frozen golden baseline is not a valid
// "before" reference; the only reliable way to attribute a moved test is to
// compare the SAME binary with this on and off (same precedent as
// NOLANG_MIR_NO_LVALUE). Enable with NOLANG_FIELD_PTR=1.
//
// Default OFF until the flip is complete, because a half-applied flip is worse
// than none: with the layout changed but the borrow mechanism not yet in place,
// `child = T { p: src.p }` shares a pointer that drop then frees twice.
var FieldPtrLayout = os.Getenv("NOLANG_FIELD_PTR") != ""

// IsStructType reports whether raw names a struct whose layout this module
// knows: a struct declared in this package (including one declared later — see
// StructNames), an already-registered qualified std struct, or a bare name that
// resolves to exactly one qualified struct.
//
// The builtin inline descriptors str/vec/txt are deliberately NOT structs here:
// their ownership is decided by ClassifyOwnership, and `txt` is a plain 256-byte
// stack buffer that owns nothing — tagging it Owned would have made Phase 1
// emit a drop for it.
//
// Lives on Module (not on the lowerer) so the analysis passes and codegen ask
// the SAME question. It reads only StructNames / StructFields / the qualified
// suffix scan, so unlike internType it has no interning side effect and is safe
// to call at any point in the pipeline.
func (m *Module) IsStructType(raw string) bool {
	switch raw {
	case "", "str", "vec", "txt":
		return false
	}
	if m.StructNames[raw] {
		return true
	}
	if _, ok := m.StructFields[raw]; ok {
		return true
	}
	if strings.Contains(raw, ".") {
		return false
	}
	return m.uniqueQualifiedStruct(raw) != ""
}

// FieldIsPointer reports whether a field's slot holds a `%T*` rather than an
// inlined `%T`.
//
// Three guards, all deliberate:
//
//  0. An explicit `#{inline=...}` wins outright, and it wins in BOTH states of
//     NOLANG_FIELD_PTR. That is the whole point of the annotation: it is how a
//     field opts into the layout the module-wide default does not give it.
//     `#{inline=false}` is therefore NOT a synonym for "no annotation" — it
//     forces a pointer even when the default is by-value.
//
//  1. `FieldTagInline` short-circuits. Inline is the ZERO value of FieldTag, so
//     any FieldInfo that never went through tag analysis keeps the pre-existing
//     by-value layout. That is what makes this flip safe to land incrementally:
//     an untagged field can never silently become a pointer.
//
//  2. The field's type must be a STRUCT. Owned leaves (str / vec / []T / map)
//     are tagged Owned too, but they are already-owned inline descriptors whose
//     layout must not change — `str.data` in particular is the descriptor's own
//     raw buffer, and pointerizing it would add a hop and break the drop walk.
//     `?T` (T a struct) is Owned as well, but its payload stays inline; giving
//     it a pointer is a separate step (see the plan's L1 item 5). The checker
//     rejects `#{inline}` on any of these, so guard 0 can only ever be reached
//     for a struct — the IsStructType below keeps that true even for a
//     FieldInfo built without the checker having run.
//
// Note the test is on the field's WHOLE type, not its element: `[N]T` and `[]T`
// are not structs, so their elements stay inline by design (the
// container-element decision). That falls out of this check rather than being a
// separate special case.
func (m *Module) FieldIsPointer(f FieldInfo) bool {
	switch f.Layout {
	case FieldLayoutInline:
		return false
	case FieldLayoutPointer:
		return m.IsStructType(f.TypeRaw)
	}
	if !FieldPtrLayout {
		return false
	}
	if f.Tag == FieldTagInline {
		return false
	}
	return m.IsStructType(f.TypeRaw)
}

// StructKeyOf resolves a raw struct name to the key StructFields is registered
// under. A bare name may live under its module-qualified key (`utsname` ->
// `os.utsname`), so an exact miss falls back to a suffix scan.
func (m *Module) StructKeyOf(raw string) string {
	if raw == "" {
		return ""
	}
	if _, ok := m.StructFields[raw]; ok {
		return raw
	}
	for k := range m.StructFields {
		if k == raw || strings.HasSuffix(k, "."+raw) {
			return k
		}
	}
	return ""
}

// StructPtrFieldIdxs returns the indices of key's fields whose slot is a `%T*`,
// in declaration order. Returns nil for an unknown key.
func (m *Module) StructPtrFieldIdxs(key string) []int {
	fields := m.StructFields[key]
	var out []int
	for i := range fields {
		if m.FieldIsPointer(fields[i]) {
			out = append(out, i)
		}
	}
	return out
}

// StructHasPtrFields reports whether any of key's fields is pointer-laid-out,
// i.e. whether a value of this struct owns separately-allocated pointees that a
// bitwise copy would SHARE and that a drop must therefore free.
func (m *Module) StructHasPtrFields(key string) bool {
	return len(m.StructPtrFieldIdxs(key)) > 0
}

// StructOwnedLeafFieldIdxs returns the indices of key's fields whose descriptor
// is INLINED in the struct yet owns a separately-allocated buffer — `str` today,
// `vec` / `[]T` / `map` once they get a clone helper.
//
// These share EXACTLY like pointees do: a bitwise struct copy copies the
// {len,cap,data} triple and both structs end up freeing the same buffer. The
// only difference from a pointer field is that the descriptor is inline instead
// of a `%T*`, which is why StructPtrFieldIdxs (and therefore every "does this
// struct need a deep copy / a recursive drop" decision) never saw them.
//
// Deliberately restricted to types this backend can actually deep-copy: widening
// it to every Owned leaf would make the analysis promise a copy that emitClone
// cannot perform, which is worse than sharing (the destination would look
// independent while still aliasing).
func (m *Module) StructOwnedLeafFieldIdxs(key string) []int {
	fields := m.StructFields[key]
	var out []int
	for i := range fields {
		if m.IsStructType(fields[i].TypeRaw) {
			continue // structs are handled by the pointee walk
		}
		ty := m.Type(m.internType(fields[i].TypeRaw))
		if ty == nil || !ty.Owned || ty.Kind != KindStr {
			continue
		}
		out = append(out, i)
	}
	return out
}

// StructHasOwnedLeafFields reports whether a bitwise copy of this struct would
// share an inline owned buffer with the source.
//
// It looks THROUGH inline nested structs, because their storage is part of this
// struct's own bytes — `outer { sub inner }` with `inner { name str }` shares
// inner's buffer exactly as if `name` were declared on outer. Without the
// recursion, outer is classified as owning no heap, so it gets neither a drop
// nor a deep copy; the leaves then leak (measured: 206 MB vs 4.7 MB for the
// equivalent single-level struct).
//
// The recursion is bounded and skips built-in containers: `%vec` and friends
// ARE in StructFields (see the blocklist note in the plan), so an unguarded
// walk would classify any struct holding a `[]str` field as owning a `str`.
func (m *Module) StructHasOwnedLeafFields(key string) bool {
	return m.structHasOwnedLeafFields(key, 0)
}

func (m *Module) structHasOwnedLeafFields(key string, depth int) bool {
	if depth > 8 {
		return false
	}
	if len(m.StructOwnedLeafFieldIdxs(key)) > 0 {
		return true
	}
	for i := range m.StructFields[key] {
		f := m.StructFields[key][i]
		if m.FieldIsPointer(f) || !m.IsStructType(f.TypeRaw) {
			continue // pointees are a separate allocation, not inline bytes
		}
		sub := m.StructKeyOf(f.TypeRaw)
		if sub == "" || sub == "vec" || sub == "str" || sub == "txt" {
			continue
		}
		if m.structHasOwnedLeafFields(sub, depth+1) {
			return true
		}
	}
	return false
}

// typeOwnsHeap is the drop machinery's ownership test: the pre-existing owner
// set (`Type.Owned`: str / vec / []T / map / ?owned) OR a struct whose pointer
// layout gives it separately-allocated pointees.
//
// Deliberately SEPARATE from Type.Owned. Type.Owned is stamped into TypeMap at
// intern time and also drives the calling convention and clone decisions, so
// widening it there would change the ABI for every struct. This predicate is
// consulted only by insertDrops / checkDropCount / isBorrowRead.
func (m *Module) typeOwnsHeap(ty *Type) bool {
	if ty == nil {
		return false
	}
	if ty.Owned {
		return true
	}
	// An OPTION owns heap through its PAYLOAD — see OptionOwnsHeap. `?i64`
	// owns nothing and keeps its (correct) no-op drop; `?big` owns the box the
	// codegen had to malloc because `big` does not fit the payload slot.
	if ty.Kind == KindOption {
		return m.OptionOwnsHeap(ty.Raw)
	}
	if ty.Kind != KindStruct {
		return false
	}
	// Inline owned leaves share exactly like pointees: a bitwise struct copy
	// copies the {len,cap,data} triple, so without a deep copy BOTH copies
	// would free one buffer. Now that every by-value write path deep-copies
	// them — struct assignment (emitLeafStructClone), field set (emitSetField),
	// and container element writes (cloneStructElemLeaves) — the drop can
	// finally own them. Deliberately NOT gated on FieldPtrLayout: sharing an
	// inline str buffer is a property of the bitwise copy, identical in both
	// switch states, while the frees that make it observable run in both.
	if m.StructHasOwnedLeafFields(m.StructKeyOf(ty.Raw)) {
		return true
	}
	// NOT gated on FieldPtrLayout. A field can be a pointer WITHOUT the flag,
	// via `#{inline=false}`: StructHasPtrFields already asks the real question
	// (it goes through FieldIsPointer), so gating here on the global switch
	// would under-report for exactly the field the annotation asked to
	// pointerize — and an under-reported owner is a leaked pointee.
	if ty.Kind != KindStruct {
		return false
	}
	return m.StructHasPtrFields(m.StructKeyOf(ty.Raw))
}

// OptionOwnsHeap reports whether a `?T` value owns heap memory and therefore
// needs a real drop (emitOptionDrop) rather than the no-op a scalar option
// gets.
//
// An option owns heap in exactly two ways:
//
//   - the PAYLOAD owns heap — `?str`, `?vec`, `?[]T`, `?map`. This is the same
//     question ClassifyOwnership already answers for a plain value.
//
//   - the payload is heap-BOXED, i.e. it does not fit the inline payload slot
//     (§4.1: anything above `option-inline-threshold` bytes, default 24). The
//     BOX is a malloc the codegen made for this option, so it is the option's
//     to free — even when the payload is a POD struct that owns nothing itself
//     (`big { pad [32]i64 }`).
//
// A struct payload is deliberately NOT claimed here on the strength of its
// leaves alone: a payload small enough to be inline is stored by a bitwise
// copy that SHARES those leaves with the value it was wrapped from, and
// emitOptionDrop does not free inline struct payloads for exactly that reason.
// Only the boxed case — where the option has its own heap block — is claimed.
func (m *Module) OptionOwnsHeap(raw string) bool {
	elem, ok := parseOptionElem(raw)
	if !ok {
		return false
	}
	if ClassifyOwnership(elem) {
		return true
	}
	return m.OptionPayloadBoxed(elem)
}

// OptionPayloadBoxed reports whether a payload of raw type elemRaw is heap-
// boxed, i.e. does not fit the inline payload slot.
//
// The measurement goes through the EMITTER's own code — a throwaway codegen
// with only `mod` set, then optionPayloadLLVMType + llvmTypeSizeUpper, which
// are pure functions of the Module (they reach it through c.mod). That matters:
// this answer decides whether the analysis exempts an option→option copy's
// source from dropping, and the emitter decides whether it re-boxes it. If the
// two ever disagreed, one side would free a box the other shared.
// Reimplementing the size rules here instead is precisely how they would drift.
//
// An unsizable payload counts as boxed: the emitter boxes what it cannot prove
// fits (see optionPayloadInline), and over-reporting here only costs a drop
// that emitOptionDrop turns into a no-op.
func (m *Module) OptionPayloadBoxed(elemRaw string) bool {
	if v, ok := m.optBoxed[elemRaw]; ok {
		return v
	}
	c := &codegen{mod: m, optSlotBytes: optionSlotBytesFor(m.OptionInlineThreshold)}
	lt := c.optionPayloadLLVMType(elemRaw)
	boxed := true
	if lt != "" {
		if sz, ok := c.llvmTypeSizeUpper(lt, map[string]bool{}); ok {
			boxed = sz > c.optSlotBytes
		}
	}
	if m.optBoxed == nil {
		m.optBoxed = map[string]bool{}
	}
	m.optBoxed[elemRaw] = boxed
	return boxed
}

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

type (
	FuncID   int32
	BlockID  int32
	InstID   int32
	ValueID  int32
	TypeID   int32
	ConstID  int32
)

const (
	NoFunc   FuncID   = 0
	NoBlock  BlockID  = 0
	NoInst   InstID   = 0
	NoVal    ValueID  = 0
	NoType   TypeID   = 0
	NoConst  ConstID  = 0
)

// ---------------------------------------------------------------------------
// Ops
// ---------------------------------------------------------------------------

type Op uint16

const (
	OpInvalid Op = iota

	// terminators
	OpReturn
	OpBr
	OpCondBr
	OpSwitch

	// memory
	OpAlloc   // stack/heap slot (carries Owned flag via its type)
	OpLoad    // load from a slot
	OpStore   // store into a slot
	OpMove    // transfer ownership: source becomes invalid, drop follows dest
	OpClone   // deep copy (heap duplicated); both source and dest stay owned
	OpDrop    // destructor + @free (owned, exactly once)
	OpBorrow  // take a reference (no ownership transfer)

	// data
	OpConst     // integer/float/bool/string literal
	OpGetField  // struct field read
	OpSetField  // struct field write
	OpStructLit // struct literal: allocate the struct; fields stored by OpSetField
	// OpOptionWrap builds an inline ?T option value { i64 tag, payload } from a
	// single payload value. It is the codegen primitive behind the nolang ?T
	// constructors val/ok/some (tag 0) and err (tag 2). Routing those
	// constructors through the generic call path mis-resolves `err` to the std
	// io.err stderr-writer (returns i64) and corrupts the option's payload slot;
	// OpOptionWrap emits the inline option directly with the correct
	// discriminant. inst.Int carries the tag; inst.Args[0] is the payload value
	// (ownership transferred into the option, so it is exempt from dropping).
	OpOptionWrap // build inline option {tag, payload} for val/ok/some/err
	OpIndex      // array/slice element read
	OpIndexStore // array/slice element write
	OpSliceOp   // slice sub-range
	OpLen       // container length (str/vec/slice/array/map) — see lowerDotRead
	OpCap       // container capacity (str/vec/slice/array)

	// OpUtf8At: decode the UTF-8 code point that STARTS at a BYTE offset.
	// Args = [strValue, byteOffset], Dst = char (i64 code point), -1 past the
	// end. This is the `for c <- s` traversal primitive: the loop keeps a byte
	// cursor and advances it by @nolang.utf8_width(cp), so the whole walk is a
	// single O(n) pass. `s[i]` (OpIndex on a str receiver) is the code-point
	// INDEXED form and is O(i) per read, which is why a loop must not use it.
	// See docs/docs/lang/str.md.
	OpUtf8At

	// Tagged-enum primitives (§13.3.18). An enum value is
	// `{ i64 tag, [N x i64] payload }`; the payload is a union shared by every
	// variant, so field access is a bitcast at a slot offset rather than a
	// struct GEP.
	OpEnumNew    // build a variant value: args = payload fields, Int = tag, Name = enum raw
	OpEnumTag    // read the discriminant (i64) of an enum value
	OpEnumField  // read payload field at slot Int, typed by inst.Type

	// arithmetic / logic
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	// OpUDiv / OpUMod: UNSIGNED integer division/remainder. Nolang u64/u32/u16/
	// u8/byte use unsigned semantics, but MIR flattens every integer to the LLVM
	// i64 width and registers u64 locals as i64, so the signedness is NOT
	// recoverable from the lowered value type. It is read from the HIR infix
	// node's inferred (u64) type at hir2mir time and routed through these ops,
	// which emit `udiv`/`urem` (otherwise i64.MIN's 2^63 magnitude, which has
	// the i64.MIN bit pattern, signed-divides to garbage in i64-to-str /
	// u64-to-str).
	OpUDiv
	OpUMod
	OpNeg
	OpNot
	OpAnd
	OpOr
	OpBitAnd // bitwise AND  (&)
	OpBitOr  // bitwise OR   (|)
	OpXor
	OpShl
	OpShr

	// comparison
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpStrEq // string equality: `a == b` on %str-long operands -> i1 (routes
	//   through @str_eq; emits an `icmp` directly would be illegal since str is
	//   a struct, not an integer)

	// control value
	OpPhi

	// calls
	OpCall
	OpCallExtern
	OpCallFFI

	// conversion
	OpCast
	OpTxtFromStr // str -> txt: copy string bytes into the fixed 256-byte txt buffer, set len (<=255)
	// OpStrFromVec reinterprets a []byte (%vec) as a str (%str-long). The two
	// layouts are already byte-identical except that %vec keeps its data pointer
	// as i64 while %str-long keeps it as i8*, so this is a field-wise copy with
	// an inttoptr. Legacy does exactly this: `data str = fs.read-file(path)`
	// (tests/mem-safety/bug12-builtin-slice-to-str.no) stores the read-file
	// %vec straight into the %str-long slot, and nolang treats the bytes as text.
	OpStrFromVec

	// OpFuncRef takes the address of a named function (Sym carries the raw
	// HIR function name) and yields a function-pointer value of KindFunc type.
	// Used when a function name is passed as an argument to another function
	// (e.g. `run-suite(my-setup, my-teardown)`), so the callee receives a
	// callable function pointer. codegen's loadVal already handles KindFunc
	// values with a Name by emitting `@funcname` directly.
	OpFuncRef

	// async task runtime (cooperative scheduler; mirrors legacy build/llvm)
	// OpRun builds a lazily-enqueued %task for an `-async` function call and
	// returns its opaque i8* handle. Sym carries the async callee's LLVM name;
	// Args are the already-lowered argument values. When Sym is empty the
	// operand is already a handle (run <future-var>) and OpRun just returns it.
	OpRun
	// OpAwait drives a %task handle to completion (synchronously invokes its
	// resume_fn if not yet done) and loads the result. Dst is the result value;
	// its LLVM type is the async function's result type. Args[0] is the handle.
	OpAwait

	opCount
)

// OpSliceOp flag bits (Inst.Int).
const (
	// SliceFlagRightInc: the range's upper bound is inclusive (`]`, not `)`),
	// so codegen adds 1 to hi before computing the length. Pre-existing.
	SliceFlagRightInc = 1
	// SliceFlagView: the sub-range ALIASES the receiver's backing buffer
	// instead of copying it (a slice VIEW). Non-ownership is encoded as
	// cap == 0, which @vec_free / @str_free already skip. Set by lowerSlice
	// only when the range is statically FORWARD (a reversed range cannot be
	// represented as a contiguous borrow, so it must keep copying), and
	// cleared again by the escape pass when the result outlives its source.
	SliceFlagView = 2
)

var opNames = [opCount]string{
	OpInvalid:   "invalid",
	OpReturn:    "return",
	OpBr:        "br",
	OpCondBr:    "cond-br",
	OpSwitch:    "switch",
	OpAlloc:     "alloc",
	OpLoad:      "load",
	OpStore:     "store",
	OpMove:      "move",
	OpClone:     "clone",
	OpDrop:      "drop",
	OpBorrow:    "borrow",
	OpConst:     "const",
	OpGetField:  "getfield",
	OpSetField:  "setfield",
	OpStructLit: "structlit",
	OpOptionWrap: "option-wrap",
	OpIndex:     "index",
	OpIndexStore: "indexstore",
	OpSliceOp:   "sliceop",
	OpLen:       "len",
	OpCap:       "cap",
	OpUtf8At:    "utf8-at",
	OpAdd:       "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpUDiv: "udiv", OpUMod: "umod",
	OpNeg: "neg", OpNot: "not", OpAnd: "and", OpOr: "or", OpBitAnd: "bitand", OpBitOr: "bitor", OpXor: "xor", OpShl: "shl", OpShr: "shr",
	OpEq: "eq", OpNe: "ne", OpLt: "lt", OpLe: "le", OpGt: "gt", OpGe: "ge", OpStrEq: "streq",
	OpPhi:  "phi",
	OpCall:      "call",
	OpCallExtern: "call-extern",
	OpCallFFI:   "call-ffi",
	OpCast:      "cast",
	OpTxtFromStr: "txt-from-str",
	OpStrFromVec: "str-from-vec",
	OpFuncRef:   "func-ref",
	OpRun:       "run",
	OpAwait:     "await",
	OpEnumNew:   "enum-new",
	OpEnumTag:   "enum-tag",
	OpEnumField: "enum-field",
}

func (o Op) String() string {
	if o < 0 || o >= opCount {
		return "Op?"
	}
	return opNames[o]
}

// producesValue reports whether op yields a destination value. Terminators,
// pure side-effecting stores, and Drop/SetField do not.
func producesValue(op Op) bool {
	switch op {
	case OpReturn, OpBr, OpCondBr, OpSwitch, OpStore, OpDrop, OpSetField, OpIndexStore:
		return false
	}
	return true
}

// isTerminator reports whether op may appear as a block terminator.
func (o Op) isTerminator() bool {
	switch o {
	case OpReturn, OpBr, OpCondBr, OpSwitch:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Types & ownership
// ---------------------------------------------------------------------------

type TypeKind uint8

const (
	KindUnknown TypeKind = iota
	KindInt
	KindFloat
	KindBool
	KindChar
	KindStr
	KindSlice
	KindArray
	KindMap
	KindOption
	KindPtr
	KindStruct
	KindFunc
	KindVoid
	KindEnum
)

// String returns a readable name for a TypeKind, used by diagnostics, the MIR
// printer, and codegen error messages.
func (k TypeKind) String() string {
	switch k {
	case KindUnknown:
		return "unknown"
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindBool:
		return "bool"
	case KindChar:
		return "char"
	case KindStr:
		return "str"
	case KindSlice:
		return "slice"
	case KindArray:
		return "array"
	case KindMap:
		return "map"
	case KindOption:
		return "option"
	case KindPtr:
		return "ptr"
	case KindStruct:
		return "struct"
	case KindFunc:
		return "func"
	case KindVoid:
		return "void"
	case KindEnum:
		return "enum"
	}
	return "TypeKind?"
}

// Type is an interned type. Raw is the nolang type string; Owned marks a type
// that owns heap memory and therefore needs exactly one Drop.
// FieldInfo describes one field of a struct type: its source name and nolang
// type string. The order within a struct's []FieldInfo is the GEP index order.
type FieldInfo struct {
	Name    string
	TypeRaw string
	// Tag is the field's compile-time semantic tag, fixed at the DEFINITION
	// site (see FieldTag). It is derived from the declared type plus the field's
	// own annotations — never from cross-statement dataflow.
	Tag FieldTag
	// Layout is the field's explicit `#{inline=...}` override, if any. It
	// decides the LAYOUT (bytes inside the host vs. a `%T*`); Tag decides the
	// OWNERSHIP. Keeping the two apart is what lets `#{inline=false}` mean
	// "pointer" without also meaning "does not own its pointee".
	Layout FieldLayout
}

// FieldLayout is an explicit, per-field layout override written as
// `#{inline=true}` / `#{inline=false}`. FieldLayoutDefault means the field
// carries no such annotation, so the module-wide rule applies (the one
// NOLANG_FIELD_PTR flips). An override therefore lets a single field be laid
// out against the grain of the rest of the program.
type FieldLayout uint8

const (
	// FieldLayoutDefault: no `#{inline}` on this field.
	FieldLayoutDefault FieldLayout = iota
	// FieldLayoutInline: `#{inline}` / `#{inline=true}`. Store the value inside
	// the host even when the default is a pointer.
	FieldLayoutInline
	// FieldLayoutPointer: `#{inline=false}`. Store a `%T*` and heap-allocate
	// the pointee, even when the default is by-value.
	FieldLayoutPointer
)

// String renders the override using the spelling the language uses.
func (l FieldLayout) String() string {
	switch l {
	case FieldLayoutInline:
		return "inline"
	case FieldLayoutPointer:
		return "pointer"
	}
	return "default"
}

// FieldTag is the semantic tag nolang stamps on a struct field at its
// DEFINITION site. The tag is a DECLARATION: it says who owns the field's
// storage, and is therefore decided without any use-site analysis. (Deciding
// whether an assignment must CLONE or may MOVE is a separate, purely
// optimizing question — see analysis.go's field-assignment pass.)
//
// Layout consequence (see emitStructTypes): a struct-typed field is Owned by
// DEFAULT and stored as a pointer (`%T*`); `#{inline}` opts a struct-typed
// field into the by-value layout.
//
// Ownership is transitive and orthogonal to the tag: an Inline field whose type
// contains owned members (e.g. `#{inline} s holder` where holder has a str
// field) still needs a RECURSIVE drop. The tag decides layout and the field
// slot's own ownership; the drop walk is structural.
type FieldTag uint8

const (
	// FieldTagInline: the field's value lives inside the host struct and the
	// slot owns no separate heap object. Scalars are always Inline, and a
	// struct-typed field opts in with `#{inline}`.
	//
	// This is the ZERO value on purpose: it is the layout every field had
	// before field tags existed, so a FieldInfo built without tag analysis
	// keeps the previous by-value behaviour instead of silently turning into a
	// pointer.
	FieldTagInline FieldTag = iota
	// FieldTagOwned: the field slot owns heap storage. A struct-typed field is
	// Owned by default and stored as a pointer; owned leaves (str, vec, []T,
	// maps, option-of-owned) stay Owned and keep their inline descriptor, whose
	// pointer lives inside. Dropping the host drops the field, recursively.
	FieldTagOwned
	// FieldTagBorrow: the field aliases storage owned elsewhere. It is never
	// dropped, and it must not outlive its owner — a borrow escaping its
	// defining frame is a compile error, not a silent dangling read.
	//
	// NOTE: this is NOT the same concept as the `&T` view. `&T` is a LIFETIME
	// annotation ("this value's lifetime is bound to the method receiver") and
	// may only appear in a method's result list; Borrow is a field/variable
	// ownership tag. Keeping them distinct is deliberate.
	FieldTagBorrow
)

// String renders the tag using the names the language uses.
func (t FieldTag) String() string {
	switch t {
	case FieldTagOwned:
		return "Owned"
	case FieldTagBorrow:
		return "Borrow"
	}
	return "Inline"
}

// VariantInfo describes one variant (constructor) of a tagged enum: its source
// name, its discriminant (the declaration order within the enum body), and the
// raw nolang types of its payload fields in declaration order. A unit variant
// (`red`, `empty`) simply has no fields.
type VariantInfo struct {
	Name   string
	Tag    int64
	Fields []string
	// FieldNames holds the source name of each payload field, parallel to
	// Fields. A single-field payload declared on the variant itself
	// (`ok(v i64)` -> KVariant.Type) has no KStructField child and therefore
	// no name; such an entry is "" and matches any binding name by position.
	FieldNames []string
}

// TaggedEnumInfo is the variant table of one tagged enum. PayloadSlots is the
// width of the shared payload area in 8-byte slots, which is the max over all
// variants of the sum of their fields' slot widths.
//
// Layout: the enum is represented as `{ i64 tag, [PayloadSlots x i64] payload }`.
// The payload is a UNION — every variant writes its fields into the same
// storage — so all field reads/writes go through a bitcast to the field's real
// type at its slot offset, never through a per-variant struct type.
//
// Inline records the boolean value of the enum definition's `#{inline}`
// annotation (the enum-level annotation written on its own line above the whole
// enum). All three spellings are accepted, and the explicit ones are the
// preferred, self-documenting form:
//
//	#{inline}        -> true   (shorthand)
//	#{inline=true}   -> true
//	#{inline=false}  -> false  (explicitly not inline)
//
// The stack form above is a small object already: 16 bytes when every variant
// is payload-less (PayloadSlots == 1) and growing only as far as the widest
// variant needs. The annotation is the opt-in marker for that form, so an enum
// declared with `#{inline=true}` is guaranteed to keep it. `#{inline=false}`
// spells out the default instead of leaving it implicit; today both values
// produce the same layout, because the stack form is what the default already
// is.
type TaggedEnumInfo struct {
	Name         string
	Variants     []VariantInfo
	PayloadSlots int64
	Inline       bool
}

type Type struct {
	ID    TypeID
	Raw   string
	Kind  TypeKind
	Owned bool
	Elem  TypeID
	Sizes []int64 // for KindArray: dimensions
	// Func is non-nil for KindFunc types. Params/Results are the by-VALUE
	// MIR type IDs of the function's inputs and outputs (NOT the by-reference
	// LLVM pointers). The by-ref LLVM signature of a function of this type is
	//   void (Params[0]*, ..., Params[N-1]*, Results[0]*, ..., Results[M-1]*)*
	// which is exactly the layout emitCall uses for a direct call, so an
	// indirect call through a value of this type reuses that machinery by
	// substituting the loaded function pointer for @funcname.
	Func *FuncType
}

// FuncType describes the signature of a KindFunc MIR type. Params and Results
// are by-value MIR type IDs (matching the nolang declaration `(p T)(r R)?`).
type FuncType struct {
	Params  []TypeID
	Results []TypeID
}

// parseMapTypes splits a nolang map type string "[Key]Value" into its key and
// value type strings.
//
// Maps are spelled with a TYPE in the brackets (`[str]i64`), which is
// syntactically ambiguous with a fixed array (`[32]byte`) — only the content
// distinguishes them. Earlier code keyed off a "map[" prefix, which no real
// type string ever has, so every map was misclassified as a fixed array (and
// therefore as non-owned, and with a bogus element type). Detection is now:
// bracket content that is not a plain integer => a key type => map.
func parseMapTypes(raw string) (key, val string, ok bool) {
	if len(raw) < 4 || raw[0] != '[' || raw[1] == ']' {
		return "", "", false
	}
	end := strings.Index(raw, "]")
	if end < 0 || end+1 >= len(raw) {
		return "", "", false
	}
	inner := raw[1:end]
	if inner == "" {
		return "", "", false
	}
	// A fixed array has a (possibly digit-only) size; anything else is a key type.
	allDigits := true
	for i := 0; i < len(inner); i++ {
		if inner[i] < '0' || inner[i] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "", "", false // [32]byte -> fixed array, not a map
	}
	return inner, raw[end+1:], true
}

// isMapRaw reports whether raw is a nolang map type ("[Key]Value").
func isMapRaw(raw string) bool {
	_, _, ok := parseMapTypes(raw)
	return ok
}

// ClassifyOwnership derives whether a nolang type string owns heap memory.
// Scalars, pointers (borrows), and unannotated structs are not owned; str,
// slices (vec/[]T), maps, and options-of-owned are owned.
func ClassifyOwnership(raw string) bool {
	if raw == "" {
		return false
	}
	switch {
	case raw == "str", raw == "vec":
		return true
	case strings.HasPrefix(raw, "[]"):
		return true
	case isMapRaw(raw):
		return true
	case strings.HasPrefix(raw, "?"):
		return ClassifyOwnership(strings.TrimPrefix(raw, "?"))
	}
	return false
}

// KindOfRaw derives a coarse structural kind from a nolang type string. It is a
// best-effort classifier used by the printer and analysis; an empty Raw maps to
// KindUnknown (callers may override with explicit struct knowledge).
func KindOfRaw(raw string) TypeKind {
	if raw == "" {
		return KindUnknown
	}
	switch {
	case raw == "str", raw == "vec":
		return KindStr
	case strings.HasPrefix(raw, "[]"):
		return KindSlice
	case isMapRaw(raw):
		return KindMap
	case strings.HasPrefix(raw, "?"):
		return KindOption
	case strings.HasPrefix(raw, "&"):
		// VIEW (`&T`). `&` is a LIFETIME ANNOTATION — "this value's lifetime is
		// bound to the method receiver (`self`)" — NOT a pointer and NOT a
		// distinct type. So a view keeps T's kind (and therefore T's layout);
		// see Module.internType for the two things that do differ.
		return KindOfRaw(strings.TrimPrefix(raw, "&"))
	case strings.HasPrefix(raw, "["):
		return KindArray
	case raw == "i8", raw == "i16", raw == "i32", raw == "i64", raw == "u8", raw == "u16", raw == "u32", raw == "u64":
		return KindInt
	case raw == "f32", raw == "f64":
		return KindFloat
	case raw == "bool":
		return KindBool
	case raw == "byte":
		return KindInt
	case raw == "char":
		return KindChar
	case strings.HasPrefix(raw, "fn("):
		// Function-type signature produced by parser.FunctionType.String():
		//   fn()            -> no params, no results
		//   fn(i64)         -> one i64 param, no results
		//   fn(i64)(i64)    -> one i64 param, one i64 result
		return KindFunc
	case raw == "":
		return KindUnknown
	}
	// a bare identifier is a struct or named type; treat as struct (may be owned
	// via registry). See Module.MarkOwnedStruct.
	return KindStruct
}

// ---------------------------------------------------------------------------
// Values, Instructions, Blocks, Functions, Module
// ---------------------------------------------------------------------------

type Value struct {
	ID   ValueID
	Name string
	Type TypeID
}

type Inst struct {
	ID    InstID
	Op    Op
	Dst   ValueID // result; NoVal when op yields nothing
	// Results holds every result value of a multi-result call
	// (`x, y = f()`). Results[0] always equals Dst, so all single-result
	// consumers can keep reading Dst unchanged.
	Results []ValueID
	Args  []ValueID
	Type  TypeID
	Block BlockID
	Int   int64   // integer / bool / enum payload (OpConst/OpInt payloads)
	Flt   float64 // float payload
	Str   string  // literal text / label / field name
	Sym   string  // callee name / extern symbol / builtin
	// Callee is the indirect-call target value (a function-pointer local) for an
	// OpCall whose callee is NOT a named function but a fn-typed local/param
	// (e.g. `setup()` where `setup` has type `test-cb`). When Callee != NoVal,
	// emitCall loads the function pointer from Callee's slot and calls through it
	// instead of emitting `call void @Sym(...)`. Sym may carry the fn-type name
	// for diagnostics only.
	Callee ValueID
	Line  int32
	Col   int32
	// MovesArg marks an aggregate-constructor store (OpSetField emitted by
	// lowerStructLit) whose value argument (Args[1]) is CONSUMED: ownership
	// transfers into the struct field, so the value's temporary heap must not be
	// dropped separately (doing so frees a buffer the escaped struct still
	// points to). The drop pass treats it as a move source when it is dead after
	// the store.
	MovesArg bool
}

type Term struct {
	Op      Op // OpReturn / OpBr / OpCondBr / OpSwitch
	Args    []ValueID
	Targets []BlockID
	Sym     string
}

type Block struct {
	ID     BlockID
	Name   string
	Insts  []InstID
	Term   *Term
	Preds  []BlockID
	Succs  []BlockID
}

type Function struct {
	ID         FuncID
	Name       string
	Params     []ValueID
	Results    []TypeID
	ResultParams []ValueID // which Params are out-params (store back at return)
	Blocks     []BlockID
	Entry      BlockID
	LocalTypes map[ValueID]TypeID // value -> type (carries ownership)
	IsExtern   bool
	IsMethod   bool
	// Variadic marks a `f (a ..T)` spread function (hir.FlagVariadic). The
	// call site must pack trailing scalar arguments into the []T spread
	// parameter; emitCallBody needs this flag because the "more actual args
	// than in-params" count heuristic cannot tell a spread element from an
	// explicitly written trailing out-param actual.
	Variadic   bool
	Receiver   TypeID
	SourceName string
}

type Const struct {
	ID   ConstID
	Str  string
	Type TypeID
}

type ExternDecl struct {
	Name    string
	Params  []TypeID
	Results []TypeID
	Lang    string
}

type GlobalDecl struct {
	Name  string
	Type  TypeID
	Init  ValueID
	// ConstText is the LLVM constant initializer text (e.g. `[256 x i8] [..]`).
	// Empty means the initializer could not be constant-folded; the reference
	// then resolves to an undefined @Name, which the verifier rejects and the
	// caller falls back to the legacy codegen (never emits wrong data).
	ConstText string
	// EmbedBytes, when non-nil, is the compile-time payload of an
	// `#{embed='file'}` binding. Codegen emits it as a private constant byte
	// array `@.embed.<Name> = private constant [N x i8] c"..."`, which
	// ConstText references through a `ptrtoint([N x i8]* @.embed.<Name> to
	// i64)`. Mirrors the legacy backend's embed global (build/llvm/generator.go).
	EmbedBytes []byte
}

type Module struct {
	Name      string
	// OptionInlineThreshold 是 `?T` 载荷内联的字节阈值：载荷 sizeof 小于等于
	// 它则内联进 option 的 slot，大于它则堆装箱、slot[0] 存指针。默认 24
	// （err 载荷是 24 字节的 %str-long，24 是它能原样内联的最小值）；可配置
	// 更大，最小 8；小于 8 是编译错误。由 build 从 package.jsonc 的
	// compiler.option-inline-threshold（或 NOLANG_OPTION_INLINE_THRESHOLD）
	// 填入，0 表示取默认值。
	OptionInlineThreshold int

	// optBoxed memoizes OptionPayloadBoxed per element raw type. The answer is
	// a pure function of (element type, OptionInlineThreshold), but computing
	// it stands up a throwaway codegen, so it is cached. Not part of the
	// module's identity — purely a query cache.
	optBoxed map[string]bool

	Funcs     []Function
	Blocks    []Block
	Insts     []Inst
	Values    []Value
	Types     []Type
	TypeMap   map[string]TypeID
	Consts    []Const
	Externs   []ExternDecl
	Globals   []GlobalDecl
	FuncByName map[string]FuncID

	// KnownFuncs is the set of every Nolang function name present in the HIR
	// package (populated during lowering from the HIR func table). emitCall
	// uses it to prefer a real function over a builtin that was only matched
	// via the bare-name fallback (e.g. module function `fs.read-dir` vs the
	// global builtin `read-dir`).
	KnownFuncs map[string]bool

	// Lowered records which function names have already been lowered during
	// reachability-driven HIR->MIR lowering (user functions pull in only the
	// std functions they reference).
	Lowered map[string]bool

	// OwnedStructs is a registry of struct raw-type strings that own heap
	// memory (contain an owned field). Lowering consults it; without an entry a
	// named/struct type is treated as non-owned for v1 (defer to emitter).
	OwnedStructs map[string]bool

	// StructFields maps a struct raw-type name to its ordered fields (name +
	// nolang type string), extracted from HIR struct definitions during
	// lowering. It drives OpGetField/OpSetField index resolution and the
	// emission of LLVM struct type declarations.
	StructFields map[string][]FieldInfo

	// StructNames is the set of struct names known to this module, populated by
	// a PRE-PASS before any field type is interned. internType's bare-identifier
	// fixup collapses a name it does not recognize to KindInt and CACHES that
	// result in TypeMap, so a struct whose field references a struct declared
	// LATER used to be permanently mis-typed as i64 — the field access then
	// emitted `getelementptr inbounds i64` and LLVM verification failed:
	//
	//	holder { p pt }   ; pt declared below
	//	pt { x i64  y i64 }
	//
	// Consulting this set keeps the field KindStruct regardless of declaration
	// order.
	StructNames map[string]bool

	// TaggedEnums maps a tagged-enum raw type name (e.g. "color", "box") to
	// its variant table, collected from HIR KTaggedEnumDef nodes during
	// lowering. It drives variant-constructor resolution, match dispatch on
	// the discriminant, and the emission of the enum's LLVM layout.
	//
	// Variants are NAME-SPACED BY ENUM: two enums may both define `ok` with
	// different tags and different payloads (see tests/tagged-enum.no).
	// Resolution therefore always goes through the target enum's own table,
	// never a global variant-name index.
	TaggedEnums map[string]*TaggedEnumInfo

	// TypeAliases maps a named function-type alias (e.g. `test-cb` from
	// `test-cb = ()`) to the KindFunc MIR type ID it denotes. Populated during
	// HIR lowering from KTypeAlias nodes flagged FlagFuncType. internType
	// consults it first so a param typed `test-cb` resolves to the function
	// pointer type rather than being misclassified as a struct.
	TypeAliases map[string]TypeID

	// ValueTypeAliases maps a nolang *value* type alias (e.g. `fd = i64`,
	// `code = i32`) to the bare nolang type name of its underlying type.
	// Populated during HIR lowering from KTypeAlias nodes whose target is a
	// concrete (non-func, non-union) type. It lets method-call dispatch expand
	// a newtype receiver (`fd.to-str`) to the method table key the legacy
	// backend actually emits (`i64.to-str`) — otherwise the bare alias name
	// forms a callee (`fd.to-str`) that no function matches
	// ("unknown callee fd.to-str", tests/errno-basic.no).
	ValueTypeAliases map[string]string
}

// blockEmpty reports whether b has no instructions and no terminator — i.e. it
// is a freshly-created merge/continuation block that no code has been lowered
// into yet. Such a block must be wired to its enclosing continuation (see
// lowerIf) rather than left for ensureReturn to terminate with `ret void`.
func (m *Module) blockEmpty(b BlockID) bool {
	blk := m.Block(b)
	return blk != nil && len(blk.Insts) == 0 && blk.Term == nil
}

// redirectTermTargets rewrites every terminator edge that targets `from` to
// instead target `to`. Used to splice an orphaned (empty) merge block out of the
// CFG by redirecting its predecessors to the enclosing continuation.
func (m *Module) redirectTermTargets(from, to BlockID) {
	for i := range m.Blocks {
		blk := &m.Blocks[i]
		if blk.Term == nil {
			continue
		}
		for j := range blk.Term.Targets {
			if blk.Term.Targets[j] == from {
				blk.Term.Targets[j] = to
			}
		}
	}
}

// NewModule allocates an empty module with reserved nil slots at index 0.
func NewModule(name string) *Module {
	m := &Module{
		Name:         name,
		TypeMap:      map[string]TypeID{},
		FuncByName:   map[string]FuncID{},
		Lowered:      map[string]bool{},
		OwnedStructs: map[string]bool{},
		StructFields: map[string][]FieldInfo{},
		StructNames:  map[string]bool{},
		TaggedEnums:  map[string]*TaggedEnumInfo{},
	}
	// reserve index 0 of each slice as a nil element
	m.Funcs = append(m.Funcs, Function{})
	m.Blocks = append(m.Blocks, Block{})
	m.Insts = append(m.Insts, Inst{})
	m.Values = append(m.Values, Value{})
	m.Types = append(m.Types, Type{})
	return m
}

// MarkOwnedStruct records that a struct raw type owns heap memory.
func (m *Module) MarkOwnedStruct(raw string) {
	m.OwnedStructs[raw] = true
}

// internType interns a type by its raw nolang string, deriving Kind and
// Ownership, and parsing composite element types / array dimensions. It is the
// shared implementation behind both Builder.Type and codegen's on-the-fly
// interning (e.g. when emitting struct type declarations).
// uniqueQualifiedStruct resolves a bare struct type name to the single
// registered module-qualified key that owns it (e.g. "utsname" -> "os.utsname").
// Returns "" when there is no such key, or when more than one module defines
// the same bare name — in that case the caller keeps its previous (scalar)
// interpretation rather than guessing.
func (m *Module) uniqueQualifiedStruct(raw string) string {
	if raw == "" || strings.Contains(raw, ".") {
		return ""
	}
	suffix := "." + raw
	found := ""
	for k := range m.StructFields {
		if !strings.HasSuffix(k, suffix) {
			continue
		}
		if found != "" && found != k {
			return "" // ambiguous: two modules define the same struct name
		}
		found = k
	}
	return found
}

func (m *Module) internType(raw string) TypeID {
	if id, ok := m.TypeMap[raw]; ok {
		return id
	}
	// A named function-type alias (e.g. `test-cb`) resolves to the KindFunc
	// type registered from its `test-cb = (...)` alias definition. This keeps
	// fn-typed params/results at the correct function-pointer type instead of
	// being misclassified as a struct by the bare-identifier fallback below.
	if m.TypeAliases != nil {
		if id, ok := m.TypeAliases[raw]; ok {
			return id
		}
	}
	kind := KindOfRaw(raw)
	owned := ClassifyOwnership(raw)
	// A raw name that names a tagged enum definition is KindEnum, not the
	// KindStruct that the bare-identifier fallback would produce. Without this
	// an enum-typed local was materialized as a struct with no fields, and its
	// variant constructors resolved to nothing ("unknown callee full").
	if _, isEnum := m.TaggedEnums[raw]; isEnum {
		kind = KindEnum
	}
	if kind == KindStruct && m.OwnedStructs[raw] {
		owned = true
	}
	// A bare-identifier type that is neither a registered struct layout nor an
	// owned struct is a scalar newtype / enum (e.g. `fd`, `code`) — nolang models
	// these as integer aliases, and the MIR backend represents every integer as
	// i64, so lower them as KindInt. Without this, `fd`-typed locals resolved to
	// a bogus KindStruct type and `let bad-fd fd = -1` bound the variable to a
	// struct-typed const that codegen emitted as `undef` -> opt-inserted trap at
	// the first `bad-fd < 0` comparison (test-fd-newtype). Real user/std structs
	// ARE registered in StructFields (by collectStructFields), so they keep
	// KindStruct; namespaced types (contain ".") are left as struct too.
	if kind == KindStruct && !m.OwnedStructs[raw] && !strings.Contains(raw, ".") {
		if _, isStruct := m.StructFields[raw]; !isStruct && !m.StructNames[raw] {
			// std structs are registered under their module-qualified name
			// (e.g. `os.utsname`) while a builtin signature or a local decl may
			// only know the bare name (`utsname`). Resolve the bare name to its
			// qualified key so `uts = os.uname()` binds a real %os_utsname slot
			// instead of collapsing to i64 — which made `uts.sysname` emit a
			// getelementptr on i64 and fail LLVM verification. Mirrors the
			// suffix fallback already used for field reads (hir2mir KDot).
			if q := m.uniqueQualifiedStruct(raw); q != "" {
				return m.internType(q)
			}
			kind = KindInt
		}
	}
	tid := TypeID(len(m.Types))
	m.Types = append(m.Types, Type{ID: tid, Raw: raw, Kind: kind, Owned: owned})
	// The nested m.internType calls below APPEND to m.Types and can therefore
	// REALLOCATE its backing array. Holding `t := &m.Types[tid]` across such a
	// call — or writing `m.Types[tid].Elem = m.internType(...)`, whose left-hand
	// index is resolved before the call runs — stores through a stale pointer
	// and is silently dropped.
	//
	// The damage is partial, which is what made it hard to see: the first
	// component survives (`Sizes` is written before the nested call) while the
	// second is lost, so a type came out half-populated. It only bit when the
	// element type had not been interned earlier in the compilation, because the
	// TypeMap lookup at the top makes every later intern a no-append early
	// return. Concretely `[512]byte` (the `cls` field of std regexp) interned
	// with Elem == NoType, and llvmTypeOf's KindArray fallback then rendered it
	// as the ZERO-BYTE `[0 x i64]` — silently shrinking the struct by 512 bytes,
	// so every index-store through the field wrote past the allocation and
	// aborted with SIGBUS (tests/dump2.no, chain-copy.no, regexp.no
	// and the other regexp tests).
	//
	// So: resolve every nested type into a local first, and re-index m.Types
	// only afterwards.
	var arrSizes []int64
	var elemTID TypeID
	var fnT *FuncType
	switch {
	case kind == KindArray:
		if n, elemRaw, ok := parseArray(raw); ok {
			arrSizes = []int64{n}
			elemTID = m.internType(elemRaw)
		}
	case kind == KindSlice:
		if elemRaw, ok := parseSliceElem(raw); ok {
			elemTID = m.internType(elemRaw)
		}
	case kind == KindOption:
		if elemRaw, ok := parseOptionElem(raw); ok {
			elemTID = m.internType(elemRaw)
		}
	case kind == KindPtr && strings.HasPrefix(raw, "&"):
		// View type `&T`: record the borrowed element type so codegen can
		// render the view as `llvm(T)*` instead of a bare `i8*`.
		elemTID = m.internType(strings.TrimPrefix(raw, "&"))
	case kind == KindFunc:
		// Parse the `fn(p0,p1)(r0,r1)` signature into by-value MIR type IDs.
		// Each parameter/result raw is interned so emitCall can derive the
		// by-reference LLVM pointer type for the indirect call.
		if params, results, ok := parseFuncType(raw); ok {
			ftt := FuncType{}
			for _, pr := range params {
				ftt.Params = append(ftt.Params, m.internType(pr))
			}
			for _, rr := range results {
				ftt.Results = append(ftt.Results, m.internType(rr))
			}
			fnT = &ftt
		}
	}
	res := &m.Types[tid]
	res.Sizes = arrSizes
	res.Elem = elemTID
	if fnT != nil {
		res.Func = fnT
	}
	m.TypeMap[raw] = tid
	return tid
}

// parseFuncType parses a nolang function-type raw string of the form
//   fn(p0,p1,...)(r0,r1,...)?
// (exactly parser.FunctionType.String()) into its parameter and result type
// raw strings. The optional result list may be absent (no results). Type
// strings are comma-separated within each parenthesized group; nested
// parentheses are not expected in the supported subset (scalar / slice / option
// / array element types, not function-typed parameters).
func parseFuncType(raw string) (params []string, results []string, ok bool) {
	if !strings.HasPrefix(raw, "fn(") {
		return nil, nil, false
	}
	rest := raw[len("fn("):]
	// First group: params.
	closeIdx := strings.IndexByte(rest, ')')
	if closeIdx < 0 {
		return nil, nil, false
	}
	params = splitTypeGroup(rest[:closeIdx])
	rest = rest[closeIdx+1:]
	// Optional second group: results.
	if strings.HasPrefix(rest, "(") {
		inner := rest[1:]
		c := strings.IndexByte(inner, ')')
		if c < 0 {
			return nil, nil, false
		}
		results = splitTypeGroup(inner[:c])
		rest = inner[c+1:]
	}
	if rest != "" {
		return nil, nil, false
	}
	return params, results, true
}

// splitTypeGroup splits a comma-separated list of type raw strings. A trailing
// comma (or empty content) yields no entry, matching parser behavior.
func splitTypeGroup(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FieldIndex returns the GEP index of a named field within a struct type, and
// whether the field exists.
func (m *Module) FieldIndex(structRaw, fieldName string) (int, bool) {
	fields, ok := m.StructFields[structRaw]
	if !ok {
		return 0, false
	}
	for i, f := range fields {
		if f.Name == fieldName {
			return i, true
		}
	}
	return 0, false
}

// Type returns a pointer into the type slice.
func (m *Module) Type(id TypeID) *Type {
	if id <= NoType || int(id) >= len(m.Types) {
		return nil
	}
	return &m.Types[id]
}

// Func returns a pointer into the function slice.
func (m *Module) Func(id FuncID) *Function {
	if id <= NoFunc || int(id) >= len(m.Funcs) {
		return nil
	}
	return &m.Funcs[id]
}

// Block returns a pointer into the block slice.
func (m *Module) Block(id BlockID) *Block {
	if id <= NoBlock || int(id) >= len(m.Blocks) {
		return nil
	}
	return &m.Blocks[id]
}

// Inst returns a pointer into the instruction slice.
func (m *Module) Inst(id InstID) *Inst {
	if id <= NoInst || int(id) >= len(m.Insts) {
		return nil
	}
	return &m.Insts[id]
}

// Value returns a pointer into the value slice.
func (m *Module) Value(id ValueID) *Value {
	if id <= NoVal || int(id) >= len(m.Values) {
		return nil
	}
	return &m.Values[id]
}
