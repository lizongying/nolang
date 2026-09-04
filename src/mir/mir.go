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

import "strings"

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
	OpIndex     // array/slice element read
	OpSliceOp   // slice sub-range

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

	// control value
	OpPhi

	// calls
	OpCall
	OpCallExtern
	OpCallFFI

	// conversion
	OpCast

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
	OpIndex:     "index",
	OpSliceOp:   "sliceop",
	OpAdd:       "add", OpSub: "sub", OpMul: "mul", OpDiv: "div", OpMod: "mod",
	OpNeg: "neg", OpNot: "not", OpAnd: "and", OpOr: "or", OpXor: "xor", OpShl: "shl", OpShr: "shr",
	OpEq: "eq", OpNe: "ne", OpLt: "lt", OpLe: "le", OpGt: "gt", OpGe: "ge",
	OpPhi:  "phi",
	OpCall:      "call",
	OpCallExtern: "call-extern",
	OpCallFFI:   "call-ffi",
	OpCast:      "cast",
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
	case OpReturn, OpBr, OpCondBr, OpSwitch, OpStore, OpDrop, OpSetField:
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

// Type is an interned type. Raw is the nolang type string; Owned marks a type
// that owns heap memory and therefore needs exactly one Drop.
type Type struct {
	ID    TypeID
	Raw   string
	Kind  TypeKind
	Owned bool
	Elem  TypeID
	Sizes []int64 // for KindArray: dimensions
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
	case strings.HasPrefix(raw, "map["):
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
	case strings.HasPrefix(raw, "map["):
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
	case raw == "char":
		return KindChar
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
	Args  []ValueID
	Type  TypeID
	Block BlockID
	Int   int64   // integer / bool / enum payload (OpConst/OpInt payloads)
	Flt   float64 // float payload
	Str   string  // literal text / label / field name
	Sym   string  // callee name / extern symbol / builtin
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

	// Lowered records which function names have already been lowered during
	// reachability-driven HIR->MIR lowering (user functions pull in only the
	// std functions they reference).
	Lowered map[string]bool

	// OwnedStructs is a registry of struct raw-type strings that own heap
	// memory (contain an owned field). Lowering consults it; without an entry a
	// named/struct type is treated as non-owned for v1 (defer to emitter).
	OwnedStructs map[string]bool
}

// NewModule allocates an empty module with reserved nil slots at index 0.
func NewModule(name string) *Module {
	m := &Module{
		Name:         name,
		TypeMap:      map[string]TypeID{},
		FuncByName:   map[string]FuncID{},
		Lowered:      map[string]bool{},
		OwnedStructs: map[string]bool{},
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
