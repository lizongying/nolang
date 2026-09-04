package mir

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
	paramPtr    map[ValueID]string // MIR param value -> incoming llvm param name
	resultParam map[ValueID]bool   // MIR value is an out-param
	strGlobals  map[ValueID]strGlobal
	cf          FuncID
	loadSeq     int // unique suffix for materialized load registers
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
	case "i64", "double", "i1", "void", "%str-long":
		return true
	}
	// Fixed arrays of supported element types are emitted as [N x elem]; the
	// element type check is enforced at index/store emission time (owned-element
	// arrays fall back to the legacy path there).
	if len(lt) > 0 && lt[0] == '[' {
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
func (m *Module) EmitLLVM() (string, error) {
	c := &codegen{
		mod:          m,
		fname:        map[FuncID]string{},
		labelFor:     map[BlockID]string{},
		valSlot:      map[ValueID]string{},
		paramPtr:     map[ValueID]string{},
		resultParam:  map[ValueID]bool{},
		strGlobals:   map[ValueID]strGlobal{},
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

// ---- prelude / runtime ----

func (c *codegen) emitPrelude() {
	c.sb.WriteString(`target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

%str-long = type { i64, i64, i8* }   ; len, cap, data
%vec = type { i64, i64, i64 }
%option = type { i64, i64 }

declare i8* @malloc(i64)
declare void @free(i8*)
declare i64 @write(i64, i8*, i64)
declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)

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

define void @print_str(%str-long %s) {
entry:
  %len = extractvalue %str-long %s, 0
  %data = extractvalue %str-long %s, 2
  call i64 @write(i64 1, i8* %data, i64 %len)
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
  call i64 @write(i64 1, i8* %start, i64 %len)
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
  call i64 @write(i64 1, i8* %sp, i64 %len)
  call i64 @write(i64 1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.dot, i64 0, i64 0), i64 1)
  %scaled = fmul double %fpart, 1.0e+06
  %fi = fptosi double %scaled to i64
  %fend = getelementptr [40 x i8], [40 x i8]* %buf, i64 0, i64 39
  store i8 0, i8* %fend
  %fsp = call i8* @digits(i64 %fi, i8* %fend)
  %fend_p = ptrtoint i8* %fend to i64
  %fsp_p = ptrtoint i8* %fsp to i64
  %flen = sub i64 %fend_p, %fsp_p
  call i64 @write(i64 1, i8* %fsp, i64 %flen)
  ret void
}

define void @print_bool(i1 %v) {
entry:
  br i1 %v, label %t, label %f
t:
  call i64 @write(i64 1, i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.true, i64 0, i64 0), i64 4)
  ret void
f:
  call i64 @write(i64 1, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.false, i64 0, i64 0), i64 5)
  ret void
}

@.true = private constant [4 x i8] c"true"
@.false = private constant [5 x i8] c"false"

`)
}

func (c *codegen) emitGlobals() {
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
func dataStr(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(fmt.Sprintf(`\%02X`, r))
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
			return fmt.Errorf("unsupported")
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
					return fmt.Errorf("unsupported")
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
	c.valSlot = map[ValueID]string{}
	entry := f.Blocks[0]
	c.sb.WriteString(fmt.Sprintf("%s.bb0:\n", name))
	slotIdx := 0
	allocaFor := func(v ValueID) string {
		if s, ok := c.valSlot[v]; ok {
			return s
		}
		lt, _ := c.ptype(v)
		s := fmt.Sprintf("%%v%d.s", slotIdx)
		slotIdx++
		c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", s, lt))
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
			if inst != nil && inst.Dst > NoVal {
				allocaFor(inst.Dst)
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
	case OpAnd, OpOr:
		return c.emitLogic(inst)
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
	_, aV := c.loadVal(inst.Args[0])
	_, bV := c.loadVal(inst.Args[1])
	// string concat if both operands are %str-long
	if lt == "%str-long" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = call %s @str_concat(%s %s, %s %s)\n", inst.Dst, lt, lt, aV, lt, bV))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
		return nil
	}
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
	_, v := c.loadVal(inst.Args[0])
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
	_, v := c.loadVal(inst.Args[0])
	c.sb.WriteString(fmt.Sprintf("  %%c%d = xor %s 1, %s\n", inst.Dst, lt, v))
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

func (c *codegen) emitCmp(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	aT, aV := c.loadVal(inst.Args[0])
	_, bV := c.loadVal(inst.Args[1])
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
	if aT == "double" {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = fcmp o%s double %s, %s\n", inst.Dst, op, aV, bV))
	} else {
		c.sb.WriteString(fmt.Sprintf("  %%c%d = icmp %s %s %s, %s\n", inst.Dst, op, aT, aV, bV))
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", lt, inst.Dst, lt, slot))
	return nil
}

func (c *codegen) emitLogic(inst *Inst) error {
	lt, _ := c.ptype(inst.Dst)
	slot := c.valSlot[inst.Dst]
	_, aV := c.loadVal(inst.Args[0])
	_, bV := c.loadVal(inst.Args[1])
	op := "and"
	if inst.Op == OpOr {
		op = "or"
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
	case "%vec", "%option":
		// v1 runtime free not yet wired; no-op (avoids freeing wrong layout)
		c.sb.WriteString(fmt.Sprintf("  ; drop %s (%s runtime free not yet wired)\n", v, lt))
	default:
		// scalar: nothing to free
	}
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

func (c *codegen) emitIndex(inst *Inst) error {
	arrT, _ := c.ptype(inst.Args[0])
	elemT, _ := c.ptype(inst.Dst)
	arrSlot := c.valSlot[inst.Args[0]]
	if arrSlot == "" {
		c.fail("index of value with no slot in func %d", c.cf)
		return fmt.Errorf("index slot")
	}
	_, idxV := c.loadVal(inst.Args[1])
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		c.fail("index result has no slot in func %d", c.cf)
		return fmt.Errorf("index dst slot")
	}
	c.loadSeq++
	gep := fmt.Sprintf("%%gp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 0, i64 %s\n", gep, arrT, arrT, arrSlot, idxV))
	c.loadSeq++
	lv := fmt.Sprintf("%%lx%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, elemT, elemT, gep))
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemT, lv, elemT, dstSlot))
	return nil
}

// emitIndexStore lowers `a[i] = v`: compute the element address via GEP and store
// v there. For owned element types the previous element is dropped first (memory
// safety: the overwritten buffer must be freed exactly once, and the new value is
// moved in — not cloned — so it is exempt from the array's own (non-existent)
// drop).
func (c *codegen) emitIndexStore(inst *Inst) error {
	arrT, _ := c.ptype(inst.Args[0])
	elemT, owned := c.ptype(inst.Args[2])
	arrSlot := c.valSlot[inst.Args[0]]
	if arrSlot == "" {
		c.fail("index-store of value with no slot in func %d", c.cf)
		return fmt.Errorf("indexstore slot")
	}
	_, idxV := c.loadVal(inst.Args[1])
	_, valV := c.loadVal(inst.Args[2])
	c.loadSeq++
	gep := fmt.Sprintf("%%gp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s* %s, i64 0, i64 %s\n", gep, arrT, arrT, arrSlot, idxV))
	if owned {
		c.loadSeq++
		old := fmt.Sprintf("%%old%d", c.loadSeq)
		c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", old, elemT, elemT, gep))
		c.sb.WriteString(fmt.Sprintf("  call void @str_free(%s %s)\n", elemT, old))
	}
	c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemT, valV, elemT, gep))
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
	idx, ok := c.mod.FieldIndex(recvRaw, inst.Str)
	if !ok {
		c.fail("getfield: field %q not found on %q in func %d", inst.Str, recvRaw, c.cf)
		return fmt.Errorf("getfield field")
	}
	structLT := recvLT
	fieldLT, _ := c.ptype(inst.Dst)
	if fieldLT == "" {
		fieldLT = "i64"
	}
	c.loadSeq++
	gep := fmt.Sprintf("%%gp%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", gep, structLT, structLT, recvSlot, idx))
	c.loadSeq++
	lv := fmt.Sprintf("%%lx%d", c.loadSeq)
	c.sb.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", lv, fieldLT, fieldLT, gep))
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
	idx, ok := c.mod.FieldIndex(recvRaw, inst.Str)
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

// emitStructTypes emits LLVM struct type declarations for every user/std struct
// encountered (builtins %str-long/%vec/%option are declared in the prelude). The
// field layout comes from the module's StructFields so GEP indices stay
// consistent with the declared type.
func (c *codegen) emitStructTypes() {
	// Pass 1: forward-declare every struct as opaque so later references (in
	// other structs' field lists) resolve even if emitted out of order.
	for raw := range c.mod.StructFields {
		if raw == "str" || raw == "vec" {
			continue // declared in the prelude
		}
		lt := "%" + sanitize(raw)
		c.sb.WriteString(fmt.Sprintf("%s = type opaque\n", lt))
	}
	// Pass 2: emit the concrete field layouts.
	for raw, fields := range c.mod.StructFields {
		if raw == "str" || raw == "vec" {
			continue // declared in the prelude
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

func (c *codegen) emitCall(f *Function, inst *Inst) error {
	callee := inst.Sym
	if callee == "" {
		c.fail("call without callee in func %s", f.Name)
		return fmt.Errorf("no callee")
	}
	if callee == "print" {
		if len(inst.Args) != 1 {
			c.fail("print expects 1 arg in func %s", f.Name)
			return fmt.Errorf("print arity")
		}
		argT, argV := c.loadVal(inst.Args[0])
		switch argT {
		case "%str-long":
			c.sb.WriteString(fmt.Sprintf("  call void @print_str(%s %s)\n", argT, argV))
		case "i64":
			c.sb.WriteString(fmt.Sprintf("  call void @print_i64(%s %s)\n", argT, argV))
		case "double":
			c.sb.WriteString(fmt.Sprintf("  call void @print_double(%s %s)\n", argT, argV))
		case "i1":
			c.sb.WriteString(fmt.Sprintf("  call void @print_bool(%s %s)\n", argT, argV))
		default:
			c.fail("print unsupported arg type %s in func %s", argT, f.Name)
			return fmt.Errorf("print type")
		}
		return nil
	}
	cid, ok := c.mod.FuncByName[callee]
	if !ok {
		c.fail("unknown callee %s in func %s", callee, f.Name)
		return fmt.Errorf("unknown callee")
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
		_, av := c.loadVal(inst.Args[i])
		if owned {
			slot := fmt.Sprintf("%%carg%d", int(inst.Dst)*100+i)
			c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
			c.sb.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", plt, av, plt, slot))
			callArgs = append(callArgs, plt+"* "+slot)
		} else {
			callArgs = append(callArgs, plt+" "+av)
		}
	}
	var resSlots []string
	for _, p := range outParams {
		plt, _ := c.ptype(p)
		slot := fmt.Sprintf("%%cres%d", int(inst.Dst)*100+len(resSlots))
		c.sb.WriteString(fmt.Sprintf("  %s = alloca %s\n", slot, plt))
		callArgs = append(callArgs, plt+"* "+slot)
		resSlots = append(resSlots, slot)
	}
	c.sb.WriteString(fmt.Sprintf("  call void @%s(%s)\n", c.fname[cid], strings.Join(callArgs, ", ")))
	if inst.Dst > NoVal && len(resSlots) > 0 {
		dlt, _ := c.ptype(inst.Dst)
		rslot := resSlots[0]
		c.sb.WriteString(fmt.Sprintf("  %%c%d = load %s, %s* %s\n", inst.Dst, dlt, dlt, rslot))
		c.sb.WriteString(fmt.Sprintf("  store %s %%c%d, %s* %s\n", dlt, inst.Dst, dlt, c.valSlot[inst.Dst]))
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
			_, v := c.loadVal(t.Args[0])
			c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", v, c.labelFor[t.Targets[0]], c.labelFor[t.Targets[1]]))
			return nil
		}
	}
	c.fail("malformed terminator %s", t.Op)
	return fmt.Errorf("term")
}
