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
	"strings"
)

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

	// arithmetic / logic
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
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

	opCount
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
	OpAdd:       "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpNeg: "neg", OpNot: "not", OpAnd: "and", OpOr: "or", OpBitAnd: "bitand", OpBitOr: "bitor", OpXor: "xor", OpShl: "shl", OpShr: "shr",
	OpEq: "eq", OpNe: "ne", OpLt: "lt", OpLe: "le", OpGt: "gt", OpGe: "ge", OpStrEq: "streq",
	OpPhi:  "phi",
	OpCall:      "call",
	OpCallExtern: "call-extern",
	OpCallFFI:   "call-ffi",
	OpCast:      "cast",
	OpTxtFromStr: "txt-from-str",
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
		return KindPtr
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
}

type Module struct {
	Name      string
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

	// TypeAliases maps a named function-type alias (e.g. `test-cb` from
	// `test-cb = ()`) to the KindFunc MIR type ID it denotes. Populated during
	// HIR lowering from KTypeAlias nodes flagged FlagFuncType. internType
	// consults it first so a param typed `test-cb` resolves to the function
	// pointer type rather than being misclassified as a struct.
	TypeAliases map[string]TypeID
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
	if kind == KindStruct && m.OwnedStructs[raw] {
		owned = true
	}
	tid := TypeID(len(m.Types))
	m.Types = append(m.Types, Type{ID: tid, Raw: raw, Kind: kind, Owned: owned})
	t := &m.Types[tid]
	if kind == KindArray {
		if n, elemRaw, ok := parseArray(raw); ok {
			t.Sizes = []int64{n}
			t.Elem = m.internType(elemRaw)
		}
	} else if kind == KindSlice {
		if elemRaw, ok := parseSliceElem(raw); ok {
			t.Elem = m.internType(elemRaw)
		}
	} else if kind == KindOption {
		if elemRaw, ok := parseOptionElem(raw); ok {
			t.Elem = m.internType(elemRaw)
		}
	} else if kind == KindFunc {
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
			t.Func = &ftt
		}
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
