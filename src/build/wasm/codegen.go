package wasm

import (
	"bytes"
	"math"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// Void 標記某個表達式不產生值（如 void if、無結果的函式呼叫）。
// 0x00 不是合法的 WASM value type（合法值為 0x7F/0x7E/0x7D/0x7C），可作為哨兵。
const Void ValType = 0

// writeI32Const emits an i32.const instruction with a signed LEB128 immediate.
// OpI32Const (0x41) expects a signed LEB128 value per the WASM spec; using
// unsigned LEB128 for values whose last byte has bit 6 set (e.g. 8224 = [0x80, 0x40])
// would be misinterpreted as negative.
func writeI32Const(sb *bytes.Buffer, val int32) {
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, int64(val))
}

// writeI32ConstU is a convenience wrapper for non-negative uint32 values.
func writeI32ConstU(sb *bytes.Buffer, val uint32) {
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, int64(val))
}

// ---- 局部變數管理 ----

// addLocal 在當前函數中加入一個局部變數（若已存在則回傳既有 index）。
// 回傳該變數的 local index（參數為 0..n-1，宣告的 local 自 n 起）。
func (g *Generator) addLocal(name string, t ValType) int {
	lmap := g.locals[g.currentFunc]
	if idx, ok := lmap[name]; ok {
		return idx
	}
	paramCount := len(g.funcParamNames[g.currentFunc])
	idx := paramCount + len(g.localDecls[g.currentFunc])
	lmap[name] = idx
	g.localDecls[g.currentFunc] = append(g.localDecls[g.currentFunc], t)
	g.localTypeMap[g.currentFunc][name] = t
	return idx
}

// lookupLocal 回傳當前函數中某變數的 local index。
func (g *Generator) lookupLocal(name string) (int, bool) {
	lmap := g.locals[g.currentFunc]
	idx, ok := lmap[name]
	return idx, ok
}

// localType 回傳當前函數中某變數的 ValType（須存在）。
func (g *Generator) localType(name string) ValType {
	if t, ok := g.localTypeMap[g.currentFunc][name]; ok {
		return t
	}
	return I64
}

// lookupLocalType 回傳當前函數中某變數的 ValType 與是否存在。
func (g *Generator) lookupLocalType(name string) (ValType, bool) {
	t, ok := g.localTypeMap[g.currentFunc][name]
	return t, ok
}

// ---- 字串常數池 ----

// addStringLiteral 將字串加入常數池（重用已存在者），回傳其記憶體 offset。
// offset 從 StringPoolBase 起向上配置。
func (g *Generator) addStringLiteral(s string) uint32 {
	if off, ok := g.stringPool[s]; ok {
		return off
	}
	off := g.nextMemOffset
	g.stringPool[s] = off
	g.stringData = append(g.stringData, s)
	g.nextMemOffset += uint32(len(s))
	return off
}

// ---- 控制流結構輔助（追蹤 ctrlDepth 以正確計算 br 深度）----

func (g *Generator) emitBlock(sb *bytes.Buffer) {
	sb.WriteByte(OpBlock)
	sb.WriteByte(BlockTypeEmpty)
	g.ctrlDepth++
}

func (g *Generator) emitLoop(sb *bytes.Buffer) {
	sb.WriteByte(OpLoop)
	sb.WriteByte(BlockTypeEmpty)
	g.ctrlDepth++
}

func (g *Generator) emitIfVoid(sb *bytes.Buffer) {
	sb.WriteByte(OpIf)
	sb.WriteByte(BlockTypeEmpty)
	g.ctrlDepth++
}

func (g *Generator) emitElse(sb *bytes.Buffer) {
	sb.WriteByte(OpElse)
}

func (g *Generator) emitEnd(sb *bytes.Buffer) {
	sb.WriteByte(OpEnd)
	g.ctrlDepth--
}

// ---- 記憶體 store/load 輔助 ----

// emitI32Store 發射 i32.store align=2 offset=0。堆疊需求：[addr, val]。
func (g *Generator) emitI32Store(sb *bytes.Buffer) {
	sb.WriteByte(OpI32Store)
	sb.WriteByte(0x02) // align = log2(4)
	writeU32LEB(sb, 0)
}

// emitI32Store8 發射 i32.store8 align=0 offset=0。堆疊需求：[addr, val]。
func (g *Generator) emitI32Store8(sb *bytes.Buffer) {
	sb.WriteByte(OpI32Store8)
	sb.WriteByte(0x00) // align = log2(1)
	writeU32LEB(sb, 0)
}

// ---- 敘述分派 ----

// emitStmt 將一條 Nolang 敘述發射為 WASM 指令序列。
func (g *Generator) emitStmt(sb *bytes.Buffer, stmt parser.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		g.emitLetStmt(sb, s)
	case *parser.MultiAssignStatement:
		g.emitMultiAssignStmt(sb, s)
	case *parser.ExpressionStatement:
		g.emitExprStmt(sb, s)
	case *parser.ReturnStatement:
		g.emitReturnStmt(sb, s)
	case *parser.ForStatement:
		g.emitForStmt(sb, s)
	case *parser.BlockStatement:
		for _, inner := range s.Statements {
			g.emitStmt(sb, inner)
		}
	case *parser.BreakStatement:
		g.emitBreakStmt(sb, s)
	case *parser.ContinueStatement:
		g.emitContinueStmt(sb, s)
	case *parser.FunctionDefinition:
		// 巢狀函數定義：Task 7 僅支援頂層函數，忽略主體內的定義。
	default:
		// 其餘型別（Use/Export/Struct/Enum 等）不屬 Task 7 codegen 範圍。
	}
}

// emitLetStmt 處理變數宣告/賦值：x i64、x = 5、x i64 = 5、x = a + b。
// Task 8：對 str/vec/arr/struct 變數，於 addLocal 後設定 localKindMap 與相關型別資訊。
func (g *Generator) emitLetStmt(sb *bytes.Buffer, ls *parser.LetStatement) {
	if ls.Name == nil {
		return
	}
	name := ls.Name.Value

	// 決定目標型別
	targetType := I64
	if _, ok := g.lookupLocal(name); ok {
		targetType = g.localType(name)
	} else if ls.Type != nil {
		targetType = ValTypeFromName(ls.Type)
	} else if ls.Value != nil {
		// Task 8：str/vec/struct 推斷為 I32（描述符指標）
		switch ls.Value.(type) {
		case *parser.StringLiteral, *parser.SliceLiteral, *parser.StructLiteral:
			targetType = I32
		default:
			// 字串拼接結果（InfixExpression `-`）亦為 I32
			if infix, ok := ls.Value.(*parser.InfixExpression); ok && infix.Operator == "-" {
				if g.inferKind(infix.Left) == KindStr {
					targetType = I32
				} else {
					targetType = g.inferType(ls.Value)
				}
			} else if call, ok := ls.Value.(*parser.CallExpression); ok {
				if ident, ok := call.Function.(*parser.Identifier); ok {
					switch ident.Value {
					case "with-cap", "with-len", "with-cap-len":
						targetType = I32
					default:
						targetType = g.inferType(ls.Value)
					}
				} else {
					targetType = g.inferType(ls.Value)
				}
			} else {
				targetType = g.inferType(ls.Value)
			}
		}
	}

	idx := g.addLocal(name, targetType)
	// Task 8：設定變數 kind 與型別資訊
	g.setLocalKind(name, ls)

	if ls.Value == nil {
		// 僅宣告（如 `x i64`），不發射賦值。
		return
	}

	// 發射值
	switch v := ls.Value.(type) {
	case *parser.IntegerLiteral:
		g.emitIntConst(sb, v.Value, targetType)
	case *parser.FloatLiteral:
		g.emitFloatConst(sb, v.Value, targetType)
	case *parser.BooleanLiteral:
		g.emitIntConst(sb, boolToInt64(v.Value), targetType)
	default:
		vt := g.emitExpr(sb, ls.Value)
		g.emitConvert(sb, vt, targetType)
	}

	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(idx))
}

// emitExprStmt 處理 expression statement：若是 print 呼叫則直接輸出，
// 否則發射表達式並丟棄結果（非 void 時）。
func (g *Generator) emitExprStmt(sb *bytes.Buffer, es *parser.ExpressionStatement) {
	if es == nil || es.Expression == nil {
		return
	}
	// print 呼叫在 emitExpr 內透過 emitPrint 處理，不留下堆疊值以外的副作用。
	t := g.emitExpr(sb, es.Expression)
	if t != Void {
		sb.WriteByte(OpDrop)
	}
}

// emitReturnStmt 處理 return 敘述。
// 無值 return：推入所有結果變數後 return。
// 有值 return：發射值後 return（假設函數結果型別與值相符）。
func (g *Generator) emitReturnStmt(sb *bytes.Buffer, rs *parser.ReturnStatement) {
	if rs.ReturnValue != nil {
		g.emitExpr(sb, rs.ReturnValue)
	} else {
		// 推入所有結果變數（與 emitFunctionBody 結尾相同順序）。
		for _, r := range g.funcResultNames[g.currentFunc] {
			if idx, ok := g.lookupLocal(r); ok {
				sb.WriteByte(OpLocalGet)
				writeU32LEB(sb, uint32(idx))
			}
		}
	}
	sb.WriteByte(OpReturn)
}

// emitForStmt 處理 for 迴圈。Task 7 支援 for i in [a..b) 等範圍形式。
func (g *Generator) emitForStmt(sb *bytes.Buffer, fs *parser.ForStatement) {
	if fs.IterRange == nil || fs.IterRange.Range == nil {
		// C-style for 或 count expr 不屬 Task 7 基礎範圍；不發射。
		return
	}
	iter := fs.IterRange
	rng := iter.Range
	varName := iter.Variable
	idx := g.addLocal(varName, I64)

	// i = Start + (LeftInc ? 0 : 1)  —— 必須在 block/loop 之前初始化，
	// 否則每次迴圈迭代都會重置 i，造成無限迴圈。
	g.emitExpr(sb, rng.Start)
	if !rng.LeftInc {
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 1)
		sb.WriteByte(OpI64Add)
	}
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(idx))

	// block (break target)
	g.emitBlock(sb)
	frame := loopFrame{blockIndex: g.ctrlDepth}
	// loop (continue target)
	g.emitLoop(sb)
	frame.loopIndex = g.ctrlDepth
	g.loopStack = append(g.loopStack, frame)

	// 條件：若 (RightInc ? i > End : i >= End) 則 break
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idx))
	g.emitExpr(sb, rng.End)
	if rng.RightInc {
		sb.WriteByte(OpI64GtS) // i > End → 不滿足 i <= End，跳出
	} else {
		sb.WriteByte(OpI64GeS) // i >= End → 不滿足 i < End，跳出
	}
	sb.WriteByte(OpBrIf)
	writeU32LEB(sb, 1) // 跳出 block（break）

	// 迴圈主體
	if fs.Body != nil {
		for _, s := range fs.Body.Statements {
			g.emitStmt(sb, s)
		}
	}

	// i = i + 1
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI64Add)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(idx))

	// 回到迴圈頭
	sb.WriteByte(OpBr)
	writeU32LEB(sb, 0)

	g.emitEnd(sb) // end loop
	g.emitEnd(sb) // end block
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
}

// emitBreakStmt 發射 break：br 到最內層迴圈的 block（break 目標）。
func (g *Generator) emitBreakStmt(sb *bytes.Buffer, bs *parser.BreakStatement) {
	if len(g.loopStack) == 0 {
		return
	}
	frame := g.loopStack[len(g.loopStack)-1]
	depth := uint32(g.ctrlDepth - frame.blockIndex)
	sb.WriteByte(OpBr)
	writeU32LEB(sb, depth)
}

// emitContinueStmt 發射 continue：br 到最內層迴圈的 loop（continue 目標）。
func (g *Generator) emitContinueStmt(sb *bytes.Buffer, cs *parser.ContinueStatement) {
	if len(g.loopStack) == 0 {
		return
	}
	frame := g.loopStack[len(g.loopStack)-1]
	depth := uint32(g.ctrlDepth - frame.loopIndex)
	sb.WriteByte(OpBr)
	writeU32LEB(sb, depth)
}

// ---- 表達式分派 ----

// emitExpr 將表達式發射為 WASM 指令序列，回傳其 ValType。
// 呼叫端保證表達式在堆疊上留下恰好一個值（Void 例外：不留值）。
func (g *Generator) emitExpr(sb *bytes.Buffer, expr parser.Expression) ValType {
	if expr == nil {
		return Void
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, e.Value)
		return I64
	case *parser.ByteLiteral:
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, e.Value)
		return I64
	case *parser.CharLiteral:
		// char 視為 i64（其碼點）。
		var code int64
		if len(e.Value) > 0 {
			code = int64(e.Value[0])
		}
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, code)
		return I64
	case *parser.FloatLiteral:
		g.emitF64Const(sb, e.Value)
		return F64
	case *parser.BooleanLiteral:
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, boolToInt64(e.Value))
		return I32
	case *parser.NilLiteral:
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		return I64
	case *parser.StringLiteral:
		// Task 8：字串表達式產生描述符指標（i32），指向靜態配置的 str descriptor。
		// 描述符內容 [len, cap, data] 在 data section 中預先寫入。
		descOff := g.addStrDescriptor(e.Value)
		writeI32ConstU(sb, descOff)
		return I32
	case *parser.Identifier:
		if idx, ok := g.lookupLocal(e.Value); ok {
			sb.WriteByte(OpLocalGet)
			writeU32LEB(sb, uint32(idx))
			return g.localType(e.Value)
		}
		// 未知識別字：推入 0 以維持堆疊契約。
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		return I64
	case *parser.InfixExpression:
		return g.emitInfix(sb, e)
	case *parser.PrefixExpression:
		return g.emitPrefix(sb, e)
	case *parser.CallExpression:
		return g.emitCallExpr(sb, e)
	case *parser.IfExpression:
		return g.emitIfExpr(sb, e)
	case *parser.GroupedExpression:
		return g.emitExpr(sb, e.Expression)
	case *parser.DotExpression:
		// Task 8：struct 欄位存取（u.name）或 vec.len / vec.cap。
		return g.emitDotOrField(sb, e)
	case *parser.IndexExpression:
		// Task 8：vec/arr 索引（含 bounds check）。
		return g.emitIndexExpr(sb, e)
	case *parser.StructLiteral:
		// Task 8：struct literal 構造。
		return g.emitStructLit(sb, e)
	case *parser.SliceLiteral:
		// Task 8：vec 字面量 [e1, e2, ...]。
		return g.emitVecLiteral(sb, e)
	case *parser.AssignExpression:
		// Task 8：u.name = value 或 v[i] = value（作為 expression statement 使用）。
		g.emitAssignExpr(sb, e)
		return Void
	default:
		// 未支援的表達式型別：推入 0 以維持堆疊契約。
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		return I64
	}
}

// emitInfix 處理中序運算子：算術、比較、邏輯、字串拼接。
// Nolang 整數預設為 i64；比較/邏輯結果為 i32。
// Task 8：`-` 在兩側為 str 時為字串拼接（產生新 str 描述符）。
func (g *Generator) emitInfix(sb *bytes.Buffer, ie *parser.InfixExpression) ValType {
	op := ie.Operator
	// Task 8：字串拼接判斷（`-` 兩側為 str）
	if op == "-" && g.inferKind(ie.Left) == KindStr && g.inferKind(ie.Right) == KindStr {
		g.emitExpr(sb, ie.Left)
		g.emitExpr(sb, ie.Right)
		g.emitStrConcat(sb)
		return I32
	}
	switch op {
	case "+", "-", "*", "/", "%":
		g.emitExpr(sb, ie.Left)
		g.emitExpr(sb, ie.Right)
		switch op {
		case "+":
			sb.WriteByte(OpI64Add)
		case "-":
			sb.WriteByte(OpI64Sub)
		case "*":
			sb.WriteByte(OpI64Mul)
		case "/":
			sb.WriteByte(OpI64DivS)
		case "%":
			sb.WriteByte(OpI64RemS)
		}
		return I64
	case "^", "&", "|", "<<", ">>":
		g.emitExpr(sb, ie.Left)
		g.emitExpr(sb, ie.Right)
		switch op {
		case "^":
			sb.WriteByte(OpI64Xor)
		case "&":
			sb.WriteByte(OpI64And)
		case "|":
			sb.WriteByte(OpI64Or)
		case "<<":
			sb.WriteByte(OpI64Shl)
		case ">>":
			sb.WriteByte(OpI64ShrU)
		}
		return I64
	case "<", ">", "<=", ">=", "==", "!=":
		g.emitExpr(sb, ie.Left)
		g.emitExpr(sb, ie.Right)
		switch op {
		case "<":
			sb.WriteByte(OpI64LtS)
		case ">":
			sb.WriteByte(OpI64GtS)
		case "<=":
			sb.WriteByte(OpI64LeS)
		case ">=":
			sb.WriteByte(OpI64GeS)
		case "==":
			sb.WriteByte(OpI64Eq)
		case "!=":
			sb.WriteByte(OpI64Ne)
		}
		return I32
	case "&&", "||":
		// 邏輯運算子：將兩側轉為 i32 後以 i32.and/or 組合（非短路）。
		lt := g.emitExpr(sb, ie.Left)
		g.emitToBool(sb, lt)
		rt := g.emitExpr(sb, ie.Right)
		g.emitToBool(sb, rt)
		if op == "&&" {
			sb.WriteByte(OpI32And)
		} else {
			sb.WriteByte(OpI32Or)
		}
		return I32
	}
	// 未支援運算子：推入 0。
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 0)
	return I64
}

// emitPrefix 處理前序運算子：-（負號）、!（邏輯非）。
func (g *Generator) emitPrefix(sb *bytes.Buffer, pe *parser.PrefixExpression) ValType {
	switch pe.Operator {
	case "-":
		t := g.inferType(pe.Right)
		if t == I32 {
			sb.WriteByte(OpI32Const)
			writeSLEB(sb, 0)
			g.emitExpr(sb, pe.Right)
			sb.WriteByte(OpI32Sub)
			return I32
		}
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		g.emitExpr(sb, pe.Right)
		sb.WriteByte(OpI64Sub)
		return I64
	case "!":
		t := g.emitExpr(sb, pe.Right)
		if t == I64 {
			sb.WriteByte(OpI64Eqz)
		} else {
			sb.WriteByte(OpI32Eqz)
		}
		return I32
	}
	return g.emitExpr(sb, pe.Right)
}

// emitCallExpr 處理函式呼叫：print/println/printf 內建、Task 8 len/with-cap 等、用戶函數。
func (g *Generator) emitCallExpr(sb *bytes.Buffer, ce *parser.CallExpression) ValType {
	// 方法呼叫：receiver.method(args) — ce.Function 為 DotExpression。
	if de, ok := ce.Function.(*parser.DotExpression); ok {
		return g.emitMethodCall(sb, de, ce.Arguments)
	}
	ident, ok := ce.Function.(*parser.Identifier)
	if !ok {
		sb.WriteByte(OpUnreachable)
		return Void
	}
	switch ident.Value {
	case "print", "fmt.print", "println", "fmt.println":
		g.emitPrint(sb, ce.Arguments, true)
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 0)
		return I32
	case "printf", "fmt.printf", "eprintf", "fmt.eprintf":
		// printf/eprintf deprecated; kept for backward compat.
		g.emitPrint(sb, ce.Arguments, false)
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 0)
		return I32
	case "len":
		// Task 8：len(s) / len(v) → 載入描述符的 len 欄位。
		if len(ce.Arguments) != 1 {
			sb.WriteByte(OpI64Const)
			writeSLEB(sb, 0)
			return I64
		}
		return g.emitLenCall(sb, ce.Arguments[0])
	case "cap":
		// Task 8：cap(s) / cap(v) → 載入描述符的 cap 欄位。
		if len(ce.Arguments) != 1 {
			sb.WriteByte(OpI64Const)
			writeSLEB(sb, 0)
			return I64
		}
		return g.emitCapCall(sb, ce.Arguments[0])
	case "with-cap", "with-len":
		// Task 8：with-cap(n) / with-len(n) → 配置 cap=n 的空 vec（len=0 或 n）。
		if len(ce.Arguments) != 1 {
			sb.WriteByte(OpI32Const)
			writeSLEB(sb, 0)
			return I32
		}
		// 引數需為 i32（容量值），若為 i64 則 wrap
		t1 := g.emitExpr(sb, ce.Arguments[0])
		g.emitConvert(sb, t1, I32)
		return g.emitWithCap(sb, ident.Value, 1)
	case "with-cap-len":
		// Task 8：with-cap-len(cap, len)。
		if len(ce.Arguments) != 2 {
			sb.WriteByte(OpI32Const)
			writeSLEB(sb, 0)
			return I32
		}
		t1 := g.emitExpr(sb, ce.Arguments[0])
		g.emitConvert(sb, t1, I32)
		t2 := g.emitExpr(sb, ce.Arguments[1])
		g.emitConvert(sb, t2, I32)
		return g.emitWithCap(sb, ident.Value, 2)
	}
	// 用戶函數呼叫
	funcIdx, ok := g.funcTable[ident.Value]
	if !ok {
		sb.WriteByte(OpUnreachable)
		return Void
	}
	for _, arg := range ce.Arguments {
		g.emitExpr(sb, arg)
	}
	sb.WriteByte(OpCall)
	writeU32LEB(sb, funcIdx)
	results := g.funcResults[ident.Value]
	if len(results) > 0 {
		return results[0]
	}
	return Void
}

// emitDotOrField 處理 DotExpression：struct 欄位存取 或 vec/str 的 .len / .cap。
func (g *Generator) emitDotOrField(sb *bytes.Buffer, de *parser.DotExpression) ValType {
	// 判斷是否為 vec/str 的 .len / .cap
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		kind := g.localKind(ident.Value)
		if kind == KindVec || kind == KindArr || kind == KindStr {
			switch de.Property {
			case "len":
				sb.WriteByte(OpLocalGet)
				if idx, ok := g.lookupLocal(ident.Value); ok {
					writeU32LEB(sb, uint32(idx))
				}
				g.emitI32ConstOffset(sb, DescriptorFieldLen)
				sb.WriteByte(OpI32Add)
				g.emitI32Load(sb)
				return I32
			case "cap":
				sb.WriteByte(OpLocalGet)
				if idx, ok := g.lookupLocal(ident.Value); ok {
					writeU32LEB(sb, uint32(idx))
				}
				g.emitI32ConstOffset(sb, DescriptorFieldCap)
				sb.WriteByte(OpI32Add)
				g.emitI32Load(sb)
				return I32
			}
		}
	}
	// 否則視為 struct 欄位存取
	return g.emitDotExpr(sb, de)
}

// emitAssignExpr 處理 AssignExpression：u.name = value 或 v[i] = value。
func (g *Generator) emitAssignExpr(sb *bytes.Buffer, ae *parser.AssignExpression) {
	if ae == nil {
		return
	}
	switch left := ae.Left.(type) {
	case *parser.DotExpression:
		g.emitDotAssign(sb, left, ae.Value)
	case *parser.IndexExpression:
		g.emitIndexAssign(sb, left, ae.Value)
	}
}

// emitIfExpr 處理 if/else（Task 7 僅支援 void 形式，作為敘述使用）。
func (g *Generator) emitIfExpr(sb *bytes.Buffer, ie *parser.IfExpression) ValType {
	g.emitCondition(sb, ie.Condition)
	g.emitIfVoid(sb)
	if ie.Consequence != nil {
		for _, s := range ie.Consequence.Statements {
			g.emitStmt(sb, s)
		}
	}
	if ie.Alternative != nil {
		g.emitElse(sb)
		for _, s := range ie.Alternative.Statements {
			g.emitStmt(sb, s)
		}
	}
	g.emitEnd(sb)
	return Void
}

// ---- print 內建 ----

// emitPrint 依序輸出每個引數，並在 addNewline 為真時附加換行。
// 字串引數：直接寫入 iovec 並 fd_write。
// 整數引數：透過 itoa 轉為字串後輸出。
func (g *Generator) emitPrint(sb *bytes.Buffer, args []parser.Expression, addNewline bool) {
	// 具名格式字串攔截：當只有一個 StringLiteral 引數且包含 '{' 時，
	// 走具名格式路徑（如 print('pi = {pi:.2f}')）。
	if len(args) == 1 {
		if strLit, ok := args[0].(*parser.StringLiteral); ok && strings.Contains(strLit.Value, "{") {
			g.emitNamedFormatPrint(sb, strLit.Value, addNewline)
			return
		}
	}
	for _, arg := range args {
		switch a := arg.(type) {
		case *parser.StringLiteral:
			g.emitPrintString(sb, a.Value)
		case *parser.IntegerLiteral:
			sb.WriteByte(OpI64Const)
			writeSLEB(sb, a.Value)
			g.emitPrintI64OnStack(sb)
		case *parser.ByteLiteral:
			sb.WriteByte(OpI64Const)
			writeSLEB(sb, a.Value)
			g.emitPrintI64OnStack(sb)
		case *parser.BooleanLiteral:
			if a.Value {
				g.emitPrintString(sb, "true")
			} else {
				g.emitPrintString(sb, "false")
			}
		case *parser.Identifier:
			if idx, ok := g.lookupLocal(a.Value); ok {
				t := g.localType(a.Value)
				kind := g.localKind(a.Value)
				// Task 8：str 變數 → 從描述符載入 data/len 並 fd_write
				if kind == KindStr {
					g.emitPrintStrVar(sb, a.Value)
					continue
				}
				if t == I64 {
					sb.WriteByte(OpLocalGet)
					writeU32LEB(sb, uint32(idx))
					g.emitPrintI64OnStack(sb)
				} else if t == I32 {
					sb.WriteByte(OpLocalGet)
					writeU32LEB(sb, uint32(idx))
					sb.WriteByte(OpI64ExtendI32S)
					g.emitPrintI64OnStack(sb)
				}
			}
		default:
			// 一般表達式：先檢查 kind，str 描述符需走 fd_write 路徑。
			if g.inferKind(arg) == KindStr {
				// 發射描述符指標後，從描述符載入 data/len 並 fd_write
				g.emitExpr(sb, arg)
				g.emitStrDescriptorLoad(sb)
				lenIdx := g.addLocal("$ps.len", I32)
				ptrIdx := g.addLocal("$ps.ptr", I32)
				sb.WriteByte(OpLocalSet)
				writeU32LEB(sb, uint32(lenIdx))
				sb.WriteByte(OpLocalSet)
				writeU32LEB(sb, uint32(ptrIdx))
				writeI32ConstU(sb, IovecBase)
				sb.WriteByte(OpLocalGet)
				writeU32LEB(sb, uint32(ptrIdx))
				g.emitI32Store(sb)
				writeI32ConstU(sb, IovecBase+4)
				sb.WriteByte(OpLocalGet)
				writeU32LEB(sb, uint32(lenIdx))
				g.emitI32Store(sb)
				g.emitFdWrite(sb, 1, IovecBase, 1, NwrittenPtr)
				sb.WriteByte(OpDrop)
				continue
			}
			// 一般表達式：發射後嘗試以 i64 印出。
			t := g.emitExpr(sb, arg)
			switch t {
			case I64:
				g.emitPrintI64OnStack(sb)
			case I32:
				sb.WriteByte(OpI64ExtendI32S)
				g.emitPrintI64OnStack(sb)
			case F64:
				// f64 印出非 Task 7 範範圍；丟棄。
				sb.WriteByte(OpDrop)
			}
		}
	}
	if addNewline {
		g.emitPrintString(sb, "\n")
	}
}

// emitPrintString 將字串寫入 iovec 並呼叫 fd_write(stdout)。
func (g *Generator) emitPrintString(sb *bytes.Buffer, s string) {
	off := g.addStringLiteral(s)
	length := uint32(len(s))
	// iovec[0] = off
	writeI32ConstU(sb, IovecBase)
	writeI32ConstU(sb, off)
	g.emitI32Store(sb)
	// iovec[1] = length
	writeI32ConstU(sb, IovecBase+4)
	writeI32ConstU(sb, length)
	g.emitI32Store(sb)
	// fd_write(1, IovecBase, 1, NwrittenPtr)
	g.emitFdWrite(sb, 1, IovecBase, 1, NwrittenPtr)
	sb.WriteByte(OpDrop)
}

// emitPrintI64OnStack 將堆疊上的 i64 轉為字串並輸出。
// 輸入：i64 在堆疊頂端。輸出：無（fd_write 結果已 drop）。
func (g *Generator) emitPrintI64OnStack(sb *bytes.Buffer) {
	// itoa：將 i64 轉為字串，留下 (ptr, len)。
	g.emitI64ToString(sb)
	// 將 ptr/len 存入臨時 local，再寫入 iovec。
	ptrIdx := g.addLocal("$print.ptr", I32)
	lenIdx := g.addLocal("$print.len", I32)
	// 堆疊：[ptr, len]（len 在頂端）
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(lenIdx)) // pop len
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(ptrIdx)) // pop ptr
	// iovec[0] = ptr
	writeI32ConstU(sb, IovecBase)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(ptrIdx))
	g.emitI32Store(sb)
	// iovec[1] = len
	writeI32ConstU(sb, IovecBase+4)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitI32Store(sb)
	g.emitFdWrite(sb, 1, IovecBase, 1, NwrittenPtr)
	sb.WriteByte(OpDrop)
}

// emitFdWrite 發射 fd_write 呼叫：fd_write(fd, iovs, iovsLen, nwritten)。
func (g *Generator) emitFdWrite(sb *bytes.Buffer, fd, iovs, iovsLen, nwritten uint32) {
	writeI32ConstU(sb, fd)
	writeI32ConstU(sb, iovs)
	writeI32ConstU(sb, iovsLen)
	writeI32ConstU(sb, nwritten)
	sb.WriteByte(OpCall)
	writeU32LEB(sb, fdWriteIdx)
}

// ---- itoa：i64 → 字串 ----

// emitI64ToString 將堆疊上的 i64 轉為十進位字串，寫入 itoa 緩衝區，
// 並留下 (ptr: i32, len: i32) 在堆疊上（len 在頂端）。
// 緩衝區位於 [ItoaBufferEnd - ItoaBufferSize, ItoaBufferEnd)，digits 由結尾往前寫。
// 注意：INT64_MIN 因 negation 溢位無法正確處理（Task 7 已知限制）。
func (g *Generator) emitI64ToString(sb *bytes.Buffer) {
	nIdx := g.addLocal("$itoa.n", I64)
	posIdx := g.addLocal("$itoa.pos", I32)
	negIdx := g.addLocal("$itoa.neg", I32)

	// $itoa.n = value
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(nIdx))
	// $itoa.pos = ItoaBufferEnd
	writeI32ConstU(sb, ItoaBufferEnd)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))
	// $itoa.neg = 0
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 0)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(negIdx))

	// if n < 0: neg = 1; n = -n (0 - n)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 0)
	sb.WriteByte(OpI64LtS)
	g.emitIfVoid(sb)
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(negIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 0)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Sub)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(nIdx))
	g.emitEnd(sb)

	// if n == 0: 寫入 '0'；else: 迴圈提取 digits
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Eqz)
	g.emitIfVoid(sb)
	// pos -= 1
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI32Sub)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))
	// store '0' (48) at pos  [addr, val]
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 48)
	g.emitI32Store8(sb)
	g.emitElse(sb)
	// block; loop
	g.emitBlock(sb)
	g.emitLoop(sb)
	// if n == 0, br 1 (跳出 block)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Eqz)
	sb.WriteByte(OpBrIf)
	writeU32LEB(sb, 1)
	// pos -= 1
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI32Sub)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))
	// digit = (n % 10) + 48；store at pos  [addr, val]
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 10)
	sb.WriteByte(OpI64RemS)
	sb.WriteByte(OpI32WrapI64)
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 48)
	sb.WriteByte(OpI32Add)
	g.emitI32Store8(sb)
	// n = n / 10
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(nIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 10)
	sb.WriteByte(OpI64DivS)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(nIdx))
	// br 0 (回到 loop)
	sb.WriteByte(OpBr)
	writeU32LEB(sb, 0)
	g.emitEnd(sb) // end loop
	g.emitEnd(sb) // end block
	g.emitEnd(sb) // end if

	// if neg: pos -= 1; store '-' (45) at pos
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(negIdx))
	g.emitIfVoid(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI32Sub)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 45)
	g.emitI32Store8(sb)
	g.emitEnd(sb)

	// 推入 ptr = pos, len = ItoaBufferEnd - pos
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx)) // ptr
	writeI32ConstU(sb, ItoaBufferEnd)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Sub) // len
}

// ---- 型別轉換與常數輔助 ----

// emitCondition 發射條件表達式並轉為 i32（布林）。
func (g *Generator) emitCondition(sb *bytes.Buffer, expr parser.Expression) {
	t := g.emitExpr(sb, expr)
	if t == I64 {
		// i64 → i32 真值：n != 0
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		sb.WriteByte(OpI64Ne)
	}
	// I32：直接使用。F64 不屬 Task 7 條件範圍。
}

// emitToBool 將型別 t 的堆疊值轉為 i32 真值。
func (g *Generator) emitToBool(sb *bytes.Buffer, t ValType) {
	if t == I64 {
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		sb.WriteByte(OpI64Ne)
	}
}

// emitConvert 在 from 與 to 不符時插入轉換指令。
func (g *Generator) emitConvert(sb *bytes.Buffer, from, to ValType) {
	if from == to || from == Void || to == Void {
		return
	}
	switch {
	case from == I64 && to == I32:
		sb.WriteByte(OpI32WrapI64)
	case from == I32 && to == I64:
		sb.WriteByte(OpI64ExtendI32S)
	case from == F64 && to == F32:
		// f32.demote_f64 (0xB6)
		sb.WriteByte(0xB6)
	case from == F32 && to == F64:
		// f64.promote_f32 (0xBB)
		sb.WriteByte(0xBB)
	}
}

// emitIntConst 依型別發射整數常數。
func (g *Generator) emitIntConst(sb *bytes.Buffer, val int64, t ValType) {
	switch t {
	case I32:
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, val)
	default:
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, val)
	}
}

// emitFloatConst 依型別發射浮點常數。
func (g *Generator) emitFloatConst(sb *bytes.Buffer, val float64, t ValType) {
	switch t {
	case F32:
		sb.WriteByte(OpF32Const)
		f := math.Float32bits(float32(val))
		sb.Write([]byte{byte(f), byte(f >> 8), byte(f >> 16), byte(f >> 24)})
	default:
		g.emitF64Const(sb, val)
	}
}

// emitF64Const 發射 f64.const。
func (g *Generator) emitF64Const(sb *bytes.Buffer, val float64) {
	sb.WriteByte(OpF64Const)
	b := math.Float64bits(val)
	sb.Write([]byte{
		byte(b), byte(b >> 8), byte(b >> 16), byte(b >> 24),
		byte(b >> 32), byte(b >> 40), byte(b >> 48), byte(b >> 56),
	})
}

// inferType 推斷表達式的 ValType（用於無顯式型別的變數宣告）。
func (g *Generator) inferType(expr parser.Expression) ValType {
	switch e := expr.(type) {
	case *parser.IntegerLiteral, *parser.ByteLiteral, *parser.CharLiteral, *parser.NilLiteral:
		return I64
	case *parser.FloatLiteral:
		return F64
	case *parser.BooleanLiteral:
		return I32
	case *parser.StringLiteral:
		return I32 // 字串指標
	case *parser.Identifier:
		if t, ok := g.lookupLocalType(e.Value); ok {
			return t
		}
		return I64
	case *parser.InfixExpression:
		switch e.Operator {
		case "+", "-", "*", "/", "%":
			return I64
		case "<", ">", "<=", ">=", "==", "!=", "&&", "||":
			return I32
		}
		return I64
	case *parser.PrefixExpression:
		if e.Operator == "!" {
			return I32
		}
		return g.inferType(e.Right)
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if results, ok := g.funcResults[ident.Value]; ok && len(results) > 0 {
				return results[0]
			}
		}
		// 模組限定呼叫：math.max/min/sqrt 回傳 f64
		if de, ok := e.Function.(*parser.DotExpression); ok {
			if ident, ok := de.Receiver.(*parser.Identifier); ok {
				if _, isLocal := g.lookupLocal(ident.Value); !isLocal {
					switch ident.Value {
					case "math":
						switch de.Property {
						case "max", "min", "sqrt":
							return F64
						}
					}
				}
			}
		}
		return I64
	case *parser.GroupedExpression:
		return g.inferType(e.Expression)
	}
	return I64
}

// boolToInt64 將 bool 轉為 int64（true=1, false=0）。
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// _ 確保 bytes 套件被使用（writeU32LEB/writeSLEB 接收 *bytes.Buffer）。
var _ = bytes.NewBuffer
