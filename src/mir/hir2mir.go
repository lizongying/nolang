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
	diags     []LowerDiag
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
		// first child: target (KIdent), second child: value
		var target, value int32
		count := 0
		for _, c := range l.pkg.Children(id) {
			if count == 0 {
				target = c
				count++
			} else {
				value = c
				break
			}
		}
		tn := l.pkg.Node(target)
		if tn != nil && tn.Kind == hir.KIdent {
			nm := l.pkg.Str(tn.S)
			slot, ok := l.locals[nm]
			if !ok {
				l.unsupported(l.curFuncName(), "assign", "assignment to unresolved identifier "+nm)
				return
			}
			v := l.lowerExpr(value)
			if v != NoVal {
				// Reassignment: move rhs into the EXISTING variable slot (do NOT
				// rebind the name). A move transfers ownership, so the rhs is
				// exempt from dropping — no double-free. Owned-variable
				// reassignment leaks the previous value (not yet supported); flag
				// it so the caller can fall back to the proven legacy path.
				if l.isOwnedLocal(slot) {
					l.unsupported(l.curFuncName(), "assign", "owned reassignment of "+nm+" not yet supported in MIR")
					return
				}
				l.b.EmitMoveInto(slot, v)
			}
		}
	case hir.KIf:
		l.lowerIf(n)
	case hir.KFor:
		l.lowerFor(n)
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

// ---- expressions ----

func (l *lowerer) lowerExpr(id int32) ValueID {
	n := l.pkg.Node(id)
	if n == nil {
		return NoVal
	}
	switch n.Kind {
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
	case hir.KIf, hir.KFor:
		l.unsupported(l.curFuncName(), "expr-ctrl", "control flow used as expression value")
		return NoVal
	default:
		l.unsupported(l.curFuncName(), hir.KindNames[n.Kind], "expression kind not lowered yet")
		return NoVal
	}
}

func (l *lowerer) lowerCall(n *hir.Node) ValueID {
	// callee: fn slot (KIdent or KDot)
	fnID := l.slot(n.Id, "fn")
	callee := ""
	if fnID != hir.NoID {
		fnn := l.pkg.Node(fnID)
		if fnn != nil {
			switch fnn.Kind {
			case hir.KIdent:
				callee = l.pkg.Str(fnn.S)
			case hir.KDot:
				callee = l.pkg.Str(fnn.S2) // property name (e.g. to-upper)
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
