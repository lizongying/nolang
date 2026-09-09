package mir

// Builder is a fluent constructor for MIR modules. It maintains the "current
// function" and "current block" and assigns sequential IDs, so callers emit code
// linearly (with explicit block switches for control flow).
type Builder struct {
	Mod      *Module
	CurFunc  FuncID
	CurBlock BlockID
}

// NewBuilder returns a builder bound to an empty module.
func NewBuilder(name string) *Builder {
	return &Builder{Mod: NewModule(name)}
}

// Module returns the underlying module.
func (b *Builder) Module() *Module { return b.Mod }

// ---- ID allocation (reserve slot 0 semantics: each slice already has a nil 0th
// element from NewModule, so the first real element is index 1) ----

func (b *Builder) newFuncID() FuncID {
	id := FuncID(len(b.Mod.Funcs))
	b.Mod.Funcs = append(b.Mod.Funcs, Function{})
	return id
}
func (b *Builder) newBlockID() BlockID {
	id := BlockID(len(b.Mod.Blocks))
	b.Mod.Blocks = append(b.Mod.Blocks, Block{})
	return id
}
func (b *Builder) newInstID() InstID {
	id := InstID(len(b.Mod.Insts))
	b.Mod.Insts = append(b.Mod.Insts, Inst{})
	return id
}
func (b *Builder) newValueID(typ TypeID, name string) ValueID {
	id := ValueID(len(b.Mod.Values))
	b.Mod.Values = append(b.Mod.Values, Value{ID: id, Type: typ, Name: name})
	return id
}
func (b *Builder) newTypeID() TypeID {
	id := TypeID(len(b.Mod.Types))
	b.Mod.Types = append(b.Mod.Types, Type{})
	return id
}

// ---- Types ----

// Type interns a type by its raw nolang string, deriving Kind and Ownership.
// The interning logic lives on Module.internType so codegen can reuse it.
func (b *Builder) Type(raw string) TypeID {
	return b.Mod.internType(raw)
}

// parseArray parses a fixed array type "[N]Elem" and returns (N, Elem, true).
func parseArray(raw string) (int64, string, bool) {
	if len(raw) < 3 || raw[0] != '[' {
		return 0, "", false
	}
	end := -1
	for i := 1; i < len(raw); i++ {
		if raw[i] == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, "", false
	}
	n := int64(0)
	for i := 1; i < end; i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			return 0, "", false
		}
		n = n*10 + int64(c-'0')
	}
	elem := raw[end+1:]
	if elem == "" {
		return 0, "", false
	}
	return n, elem, true
}

// parseSliceElem returns the element type of a slice type "[]Elem".
func parseSliceElem(raw string) (string, bool) {
	if len(raw) < 3 || raw[0:2] != "[]" {
		return "", false
	}
	elem := raw[2:]
	if elem == "" {
		return "", false
	}
	return elem, true
}

// parseOptionElem returns the wrapped type of an option type "?Elem".
func parseOptionElem(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '?' {
		return "", false
	}
	elem := raw[1:]
	if elem == "" {
		return "", false
	}
	return elem, true
}

// TypeExplicit interns a type with explicit kind/owned/elem, bypassing the
// coarse classifier (used when lowering has precise type information).
func (b *Builder) TypeExplicit(raw string, kind TypeKind, owned bool, elem TypeID, sizes ...int64) TypeID {
	if id, ok := b.Mod.TypeMap[raw]; ok {
		return id
	}
	tid := b.newTypeID()
	t := &b.Mod.Types[tid]
	t.ID = tid
	t.Raw = raw
	t.Kind = kind
	t.Owned = owned
	t.Elem = elem
	t.Sizes = sizes
	b.Mod.TypeMap[raw] = tid
	return tid
}

// ---- Functions ----

// NewFunc declares a function. params is a slice of parameter ValueIDs created by
// Param; results are the result types. The entry block is created and selected.
func (b *Builder) NewFunc(name string, params []ValueID, results []TypeID, isExtern bool) FuncID {
	fid := b.newFuncID()
	f := &b.Mod.Funcs[fid]
	f.ID = fid
	f.Name = name
	f.Params = params
	f.Results = results
	f.IsExtern = isExtern
	f.LocalTypes = map[ValueID]TypeID{}
	for _, p := range params {
		f.LocalTypes[p] = b.Mod.Values[p].Type
	}
	b.Mod.FuncByName[name] = fid
	b.CurFunc = fid
	entry := b.NewBlock("entry")
	f.Entry = entry
	b.CurBlock = entry
	return fid
}

// Param creates a parameter value of the given type and returns its ValueID.
// The caller passes the returned ids to NewFunc.
func (b *Builder) Param(name string, typ TypeID) ValueID {
	return b.newValueID(typ, name)
}

// Global declares a module-level constant and returns its ValueID. The caller
// fills mod.Globals[i].ConstText with the LLVM initializer; codegen emits
// `@name = constant <type> <ConstText>` and resolves references to `@name`.
func (b *Builder) Global(name string, typ TypeID) ValueID {
	id := b.newValueID(typ, name)
	b.Mod.Globals = append(b.Mod.Globals, GlobalDecl{Name: name, Type: typ, Init: id})
	return id
}

// NewBlock creates a block and appends it to the current function's block list.
func (b *Builder) NewBlock(name string) BlockID {
	bid := b.newBlockID()
	blk := &b.Mod.Blocks[bid]
	blk.ID = bid
	blk.Name = name
	if f := b.Mod.Func(b.CurFunc); f != nil {
		f.Blocks = append(f.Blocks, bid)
	}
	return bid
}

// SetBlock selects the current block for subsequent Emit/Terminate calls.
func (b *Builder) SetBlock(bid BlockID) { b.CurBlock = bid }

// CurrentBlock returns the selected block.
func (b *Builder) CurrentBlock() BlockID { return b.CurBlock }

// ---- Emission ----

// Emit appends an instruction to the current block and returns its destination
// ValueID (NoVal when the op yields no value).
func (b *Builder) Emit(op Op, typ TypeID, args []ValueID, sym string) ValueID {
	iid := b.newInstID()
	dst := NoVal
	if producesValue(op) {
		dst = b.newValueID(typ, "")
		if f := b.Mod.Func(b.CurFunc); f != nil {
			f.LocalTypes[dst] = typ
		}
	}
	inst := &b.Mod.Insts[iid]
	inst.ID = iid
	inst.Op = op
	inst.Dst = dst
	inst.Args = args
	inst.Type = typ
	inst.Block = b.CurBlock
	inst.Sym = sym
	if f := b.Mod.Func(b.CurFunc); f != nil {
		b.Mod.Blocks[b.CurBlock].Insts = append(b.Mod.Blocks[b.CurBlock].Insts, iid)
	}
	return dst
}

// EmitInt is Emit for ops carrying an integer/bool/enum payload (OpConst).
func (b *Builder) EmitInt(op Op, typ TypeID, val int64, sym string) ValueID {
	v := b.Emit(op, typ, nil, sym)
	b.Mod.Insts[len(b.Mod.Insts)-1].Int = val
	return v
}

// EmitStr is Emit for ops carrying a string payload (OpConst literal text).
func (b *Builder) EmitStr(op Op, typ TypeID, s string, sym string) ValueID {
	v := b.Emit(op, typ, nil, sym)
	b.Mod.Insts[len(b.Mod.Insts)-1].Str = s
	return v
}

// EmitVoid appends a side-effecting, value-less instruction (e.g. a void call) to
// the current block and returns. Unlike Emit, it does NOT create a destination
// value — a void call is a statement, not an expression — so no void-typed value
// (and thus no `alloca void`) is ever created.
func (b *Builder) EmitVoid(op Op, args []ValueID, sym string) InstID {
	iid := b.newInstID()
	inst := &b.Mod.Insts[iid]
	inst.ID = iid
	inst.Op = op
	inst.Args = args
	inst.Sym = sym
	inst.Block = b.CurBlock
	if f := b.Mod.Func(b.CurFunc); f != nil {
		b.Mod.Blocks[b.CurBlock].Insts = append(b.Mod.Blocks[b.CurBlock].Insts, iid)
	}
	return iid
}

// EmitCallMulti emits a call that produces MORE THAN ONE result value
// (`x, y = f()`), creating one destination per result type. Results[0] is also
// stored in Dst so every single-result consumer keeps working unchanged.
//
// A function returning N values is lowered with N out-parameters (see
// Function.ResultParams), so one call instruction yields all N results — the
// call must NOT be emitted once per result.
func (b *Builder) EmitCallMulti(resTypes []TypeID, args []ValueID, sym string) []ValueID {
	if len(resTypes) == 0 {
		b.EmitVoid(OpCall, args, sym)
		return nil
	}
	iid := b.newInstID()
	dsts := make([]ValueID, 0, len(resTypes))
	for _, rt := range resTypes {
		v := b.newValueID(rt, "")
		if f := b.Mod.Func(b.CurFunc); f != nil {
			f.LocalTypes[v] = rt
		}
		dsts = append(dsts, v)
	}
	inst := &b.Mod.Insts[iid]
	inst.ID = iid
	inst.Op = OpCall
	inst.Dst = dsts[0]
	inst.Results = dsts
	inst.Args = args
	inst.Type = resTypes[0]
	inst.Block = b.CurBlock
	inst.Sym = sym
	if f := b.Mod.Func(b.CurFunc); f != nil {
		b.Mod.Blocks[b.CurBlock].Insts = append(b.Mod.Blocks[b.CurBlock].Insts, iid)
	}
	return dsts
}

// Terminate sets the terminator of the current block. Calling it twice on the
// same block is a programming error and will overwrite the previous terminator.
func (b *Builder) Terminate(op Op, args []ValueID, targets []BlockID, sym string) {
	b.Mod.Blocks[b.CurBlock].Term = &Term{Op: op, Args: args, Targets: targets, Sym: sym}
}

// EmitOptionWrap builds an inline ?T option value { i64 tag, payload } from a
// single payload value. It is the codegen primitive behind the nolang ?T
// constructors val/ok/some (tag 0) and err (tag 2). The generic call path would
// mis-resolve `err` to the std io.err stderr-writer (i64 result) and corrupt the
// option payload; OpOptionWrap emits the inline option directly with the correct
// discriminant. tag is the discriminant; payload is the (owned) value moved into
// the option. The returned value carries optType (e.g. %option or %option_str).
func (b *Builder) EmitOptionWrap(optType TypeID, tag int64, payload ValueID) ValueID {
	iid := b.newInstID()
	dst := b.newValueID(optType, "")
	if f := b.Mod.Func(b.CurFunc); f != nil {
		f.LocalTypes[dst] = optType
	}
	inst := &b.Mod.Insts[iid]
	inst.ID = iid
	inst.Op = OpOptionWrap
	inst.Dst = dst
	inst.Args = []ValueID{payload}
	inst.Int = tag
	inst.Type = optType
	inst.Block = b.CurBlock
	if f := b.Mod.Func(b.CurFunc); f != nil {
		b.Mod.Blocks[b.CurBlock].Insts = append(b.Mod.Blocks[b.CurBlock].Insts, iid)
	}
	return dst
}

// EmitMoveInto emits an OpMove that stores src into the EXISTING dst slot without
// allocating a new value id. Used for assignment to a previously-bound variable
// (including a result parameter): the variable keeps its stable slot (so its name
// keeps pointing at it, and a result param gets written back correctly) and the
// rhs becomes a move source and is exempt from dropping (ownership transferred,
// no double-free).
//
// A move-into-an-existing-slot is a reassignment, NOT a value definition: it
// overwrites dst's slot without creating a new value. So Dst stays NoVal and the
// target value is carried in Args[1] (Args[0] is the moved-from source). This
// keeps MIR Validate() happy (no "value defined twice") and lets the codegen
// write into the existing slot.
func (b *Builder) EmitMoveInto(dst ValueID, src ValueID) InstID {
	iid := b.newInstID()
	inst := &b.Mod.Insts[iid]
	inst.ID = iid
	inst.Op = OpMove
	inst.Dst = NoVal
	inst.Args = []ValueID{src, dst}
	inst.Block = b.CurBlock
	if f := b.Mod.Func(b.CurFunc); f != nil {
		b.Mod.Blocks[b.CurBlock].Insts = append(b.Mod.Blocks[b.CurBlock].Insts, iid)
	}
	return iid
}
