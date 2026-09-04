package mir

import (
	"fmt"
	"os"

	"github.com/lizongying/nolang/hir"
)

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

// LowerHIR lowers a HIR package into a MIR module. It is reachability-driven:
// starting from `main`, only the functions the user's code (transitively)
// references are lowered — exactly the "user HIR -> MIR; referenced std HIR ->
// MIR" plan. Unreferenced std functions stay out of the MIR (dead-code
// elimination). After lowering, Analyze runs to insert drops and run the
// memory checks. The returned Report is the memory-safety report; diags are
// lowering-coverage gaps.
func LowerHIR(pkg *hir.Package) (*Module, *Report, []LowerDiag) {
	l := &lowerer{
		pkg:       pkg,
		funcNames: map[string]int32{},
		lowered:   map[string]bool{},
		locals:    map[string]ValueID{},
	}
	l.b = NewBuilder("hir")
	l.mod = l.b.Module()
	l.voidType = l.b.TypeExplicit("void", KindVoid, false, NoType)

	// Collect struct field layouts so OpGetField/OpSetField can resolve field
	// indices. Builtins have a fixed, known layout; user/std structs come from
	// HIR KStructDef nodes (field order = child order).
	l.mod.StructFields["str"] = []FieldInfo{{Name: "len", TypeRaw: "i64"}, {Name: "cap", TypeRaw: "i64"}, {Name: "data", TypeRaw: "str"}}
	l.mod.StructFields["vec"] = []FieldInfo{{Name: "len", TypeRaw: "i64"}, {Name: "cap", TypeRaw: "i64"}, {Name: "data", TypeRaw: "vec"}}
	l.collectStructFields()

	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil {
			continue
		}
		switch n.Kind {
		case hir.KFuncDef, hir.KExtern:
			name := pkg.Str(n.S)
			l.funcNames[name] = id
		}
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
			return l.typeOfNode(l.pkg.Node(c))
		}
		return l.voidType
	case hir.KPrefix:
		if l.pkg.Str(n.S) == "!" {
			return l.b.Type("bool")
		}
		if c := n.First; c != hir.NoID {
			return l.typeOfNode(l.pkg.Node(c))
		}
		return l.voidType
	case hir.KIdent:
		if v, ok := l.locals[l.pkg.Str(n.S)]; ok {
			if f := l.mod.Func(l.curFunc); f != nil {
				if t, ok := f.LocalTypes[v]; ok {
					return t
				}
			}
		}
	}
	raw := l.pkg.InferredType(n.Id)
	if raw == "" {
		raw = l.pkg.Type(n.Type)
	}
	if raw == "" || raw == "void" {
		return l.voidType
	}
	return l.b.Type(raw)
}

func (l *lowerer) unsupported(funcName, kind, msg string) {
	l.diags = append(l.diags, LowerDiag{Func: funcName, Kind: kind, Msg: msg})
}

// ---- function lowering ----

func (l *lowerer) lowerFunction(name string, hirID int32) {
	l.lowered[name] = true
	n := l.pkg.Node(hirID)
	if os.Getenv("NOLANG_MIR_DEBUG") != "" {
		var ks []string
		for _, c := range l.pkg.Children(hirID) {
			cn := l.pkg.Node(c)
			if cn != nil {
				ks = append(ks, fmt.Sprintf("%s(S=%q,T=%q)", hir.KindNames[cn.Kind], l.pkg.Str(cn.S), l.pkg.Str(cn.Type)))
			}
		}
		fmt.Fprintf(os.Stderr, "[mir-dbg] FUNC %q children: %v\n", name, ks)
	}

	var params []ValueID
	var paramNames []string
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
	}
	for i, rn := range resultNames {
		l.locals[rn] = resultVals[i]
		if f != nil {
			f.LocalTypes[resultVals[i]] = results[i]
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
		l.b.Terminate(OpReturn, nil, nil, "")
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
	if os.Getenv("NOLANG_MIR_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[mir-dbg] STMT kind=%s id=%d\n", hir.KindNames[n.Kind], id)
	}
	switch n.Kind {
	case hir.KLet:
		name := l.pkg.Str(n.S)
		var val ValueID = NoVal
		var childID int32
		for _, c := range l.pkg.Children(id) {
			childID = c
			val = l.lowerExpr(c)
			break
		}
		if os.Getenv("NOLANG_MIR_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[mir-dbg] LET name=%q val=%d childKind=%v\n", name, val, l.pkg.Node(childID).Kind)
		}
		if val != NoVal {
			if existing, ok := l.locals[name]; ok {
				// Re-binding an already-declared variable (e.g. assigning to a
				// result parameter like `out = a + b`, or a reassignment). Move the
				// rhs into the EXISTING slot so the name keeps pointing at the same
				// (borrowed) slot; this also makes result params get written back
				// correctly. Owned reassignment would leak the previous value, so
				// only allow it for borrowed locals (params / result params); flag
				// the rest so the caller can fall back to the proven legacy path.
				if l.isOwnedLocal(existing) {
					l.unsupported(l.curFuncName(), "let-reassign", "owned reassignment of "+name+" not yet supported in MIR")
					return
				}
				l.b.EmitMoveInto(existing, val)
				break
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
		var vals []ValueID
		for _, c := range l.pkg.Children(id) {
			vals = append(vals, l.lowerExpr(c))
		}
		l.b.Terminate(OpReturn, vals, nil, "")
	case hir.KAssign:
		l.lowerAssignNode(id)
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
	if os.Getenv("NOLANG_MIR_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[mir-dbg] LOWERIF cond=%d then=%d else=%d curFunc=%s\n", condID, thenID, elseID, l.curFuncName())
	}

	thenBlk := l.b.NewBlock("if.then")
	elseBlk := l.b.NewBlock("if.else")
	mergeBlk := l.b.NewBlock("if.merge")
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

	l.b.SetBlock(mergeBlk)
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

	initID := l.slot(n.Id, "init")
	condID := l.slot(n.Id, "cond")
	updateID := l.slot(n.Id, "update")
	bodyID := l.slot(n.Id, "body")

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

	l.b.SetBlock(update)
	if updateID != hir.NoID {
		l.lowerStmt(updateID)
	}
	if l.mod.Block(update).Term == nil {
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
	}

	l.b.SetBlock(exit)
}

// lowerRangeFor lowers a range-for: `for v in COLLECTION { body }` where v is
// bound to each element of COLLECTION, and `for v in LO..HI { body }` where v is
// bound to each integer in [LO, HI). It is the loop-variable-binding counterpart
// of the classic init/cond/update for (which already worked). The variable is
// inserted into l.locals so body expressions resolve it instead of cascading into
// unresolved-identifier lower-gaps.
func (l *lowerer) lowerRangeFor(n *hir.Node, iterID int32, bodyID int32, pre BlockID) {
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
		iSlot := l.b.Param(varName, elemT)
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
		l.b.SetBlock(update)
		one := l.b.EmitInt(OpConst, elemT, 1, "")
		nextV := l.b.Emit(OpAdd, elemT, []ValueID{iSlot, one}, "")
		l.b.EmitMoveInto(iSlot, nextV)
		l.b.Terminate(OpBr, nil, []BlockID{header}, "")
		l.b.SetBlock(exit)
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
	idxSlot := l.b.Param(varName+"#idx", idxT)
	// pre: idx = 0
	l.b.SetBlock(pre)
	l.b.EmitMoveInto(idxSlot, l.b.EmitInt(OpConst, idxT, 0, ""))

	header := l.b.NewBlock("for.header")
	body := l.b.NewBlock("for.body")
	update := l.b.NewBlock("for.update")
	exit := l.b.NewBlock("for.exit")
	l.loopStack = append(l.loopStack, loopCtx{exit: exit, update: update})
	defer func() { l.loopStack = l.loopStack[:len(l.loopStack)-1] }()
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

	l.b.SetBlock(update)
	one := l.b.EmitInt(OpConst, idxT, 1, "")
	nextIdx := l.b.Emit(OpAdd, idxT, []ValueID{idxSlot, one}, "")
	l.b.EmitMoveInto(idxSlot, nextIdx)
	l.b.Terminate(OpBr, nil, []BlockID{header}, "")

	l.b.SetBlock(exit)
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
		if v, ok := l.locals[name]; ok {
			return v
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
		return l.b.EmitStr(OpConst, l.b.Type("str"), l.pkg.Str(n.S), "")
	case hir.KPrefix:
		var operand int32
		for _, c := range l.pkg.Children(id) {
			operand = c
			break
		}
		ov := l.lowerExpr(operand)
		op := prefixOp(l.pkg.Str(n.S))
		return l.b.Emit(op, l.typeOfNode(n), []ValueID{ov}, "")
	case hir.KInfix:
		var lr [2]int32
		i := 0
		for _, c := range l.pkg.Children(id) {
			if i < 2 {
				lr[i] = c
				i++
			}
		}
		lv := l.lowerExpr(lr[0])
		rv := l.lowerExpr(lr[1])
		op, isCmp := infixOp(l.pkg.Str(n.S))
		resTyp := l.typeOfNode(n)
		if isCmp {
			resTyp = l.b.Type("bool")
		}
		return l.b.Emit(op, resTyp, []ValueID{lv, rv}, "")
	case hir.KCall:
		return l.lowerCall(n)
	case hir.KDot:
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
// OpGetField instruction. The receiver is either the KDot's single child (explicit
// `recv.field`) or, when the KDot has no child, the enclosing method's implicit
// `self` (captured as l.curRecv / the `self` local). The field name is the KDot's
// S string and is carried on the instruction's Str so codegen can resolve the
// struct field index.
func (l *lowerer) lowerDotRead(n *hir.Node) ValueID {
	fieldName := l.pkg.Str(n.S)

	var recvID int32
	for _, c := range l.pkg.Children(n.Id) {
		recvID = c
		break
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
	if t := l.mod.Type(l.valueTypeOf(recvV)); t != nil {
		recvRaw = t.Raw
	}
	if recvRaw == "" {
		l.unsupported(l.curFuncName(), "dot", "field "+fieldName+": cannot determine receiver type")
		return NoVal
	}
	fields, ok := l.mod.StructFields[recvRaw]
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

func (l *lowerer) lowerCall(n *hir.Node) ValueID {
	// callee: fn slot (KIdent or KDot)
	fnID := l.slot(n.Id, "fn")
	callee := ""
	var recvV ValueID // for method calls: the receiver value, passed as first arg
	if fnID != hir.NoID {
		fnn := l.pkg.Node(fnID)
		if fnn != nil {
			switch fnn.Kind {
			case hir.KIdent:
				callee = l.pkg.Str(fnn.S)
			case hir.KDot:
				// Method call: `obj.method(args)` lowers to `ReceiverType.method`
				// with the receiver passed as the FIRST argument (by pointer for
				// owned/struct receivers, matching the legacy ABI:
				//   call @str.to-bytes(%str-long* %recv, ...)).
				method := l.pkg.Str(fnn.S) // property name, e.g. "to-bytes"
				// The receiver is the KDot's single child (e.Receiver).
				var recvID int32
				for _, c := range l.pkg.Children(fnID) {
					recvID = c
					break
				}
				rv := l.lowerExpr(recvID)
				if rv == NoVal {
					return NoVal
				}
				recvV = rv
				// Receiver type name: use the nolang type of the receiver value.
				// For a `str` receiver this is "str"; for `io.writer` it is
				// "io.writer", etc. The callee is then "str.to-bytes".
				recvT := l.valueTypeOf(recvV)
				recvTypeName := "str"
				if ty := l.mod.Type(recvT); ty != nil && ty.Raw != "" {
					recvTypeName = ty.Raw
				}
				callee = recvTypeName + "." + method
			}
		}
	}
	// reachability: enqueue the referenced function for lowering
	if callee != "" {
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

	args := l.slotArgs(n.Id, "arg")
	var argv []ValueID
	if recvV != NoVal {
		argv = append(argv, recvV)
	}
	for _, a := range args {
		argv = append(argv, l.lowerExpr(a))
	}
	// The KCall node carries NO type — the AST CallExpression has no Type field,
	// so InferredType/KType are both empty for it. Derive the result type from
	// the callee's signature instead (its KResult child carries the declared
	// type). Builtins/unknowns fall back to voidType (NoVal), which is correct
	// for void calls such as print.
	resTyp := l.resultTypeOfCallee(callee)
	if resTyp == l.voidType {
		// void call: emit without a destination value (statement, not expression)
		l.b.EmitVoid(OpCall, argv, callee)
		return NoVal
	}
	return l.b.Emit(OpCall, resTyp, argv, callee)
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
	if ty != nil && ty.Elem != NoType {
		return ty.Elem
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
	v := l.lowerExpr(value)
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
			l.unsupported(l.curFuncName(), "assign", "owned reassignment of "+nm+" not yet supported in MIR")
			return NoVal
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
	default:
		op, cmp = OpInvalid, false
	}
	return op, cmp
}
