package wasm

import (
	"bytes"

	"github.com/lizongying/nolang/parser"
)

// Task 8：堆與複合型別 codegen 輔助。
//
// 本檔案集中實作 str / vec / arr / struct 的 emitExpr 分派與輔助函數，
// 由 codegen.go 的 emitExpr / emitStmt / emitPrint 在遇到對應 AST 節點時呼叫。
//
// 記憶體模型見 heap.go 註釋；所有複合型別變數均以單一 i32 local（描述符指標）
// 表示，描述符格式為 [len:i32, cap:i32, data:i32]（共 12 bytes）。

// memory.copy opcode（WASM 2.0 bulk memory operations）。
// 編碼：0xFC 0x0A 0x00 0x00（prefix + u10 + dst_mem + src_mem）。
// 堆疊需求：[dst: i32, src: i32, n: i32]。結果：無。
const (
	OpBulkMemoryPrefix byte = 0xFC
	OpMemoryCopy       byte = 0x0A
)

// emitMemoryCopy 發射 memory.copy 指令。
// 堆疊：[dst, src, n] → []。
func (g *Generator) emitMemoryCopy(sb *bytes.Buffer) {
	sb.WriteByte(OpBulkMemoryPrefix)
	sb.WriteByte(OpMemoryCopy)
	sb.WriteByte(0x00) // dst memory index = 0
	sb.WriteByte(0x00) // src memory index = 0
}

// ---- kind 推斷 ----

// inferKind 推斷表達式的 ValKind（純量/str/vec/arr/struct）。
// 用於 emitLetStmt 設定變數 kind，以及 emitInfix 判斷 `-` 是否為字串拼接。
func (g *Generator) inferKind(expr parser.Expression) ValKind {
	if expr == nil {
		return KindScalar
	}
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return KindStr
	case *parser.Identifier:
		if kmap, ok := g.localKindMap[g.currentFunc]; ok {
			if k, ok := kmap[e.Value]; ok {
				return k
			}
		}
		return KindScalar
	case *parser.SliceLiteral:
		return KindVec
	case *parser.StructLiteral:
		return KindStruct
	case *parser.InfixExpression:
		// 字串拼接 `-`：若左側為 str，結果為 str
		if e.Operator == "-" && g.inferKind(e.Left) == KindStr {
			return KindStr
		}
		return KindScalar
	case *parser.DotExpression:
		// 查 struct 欄位的 kind
		var structName string
		if ident, ok := e.Receiver.(*parser.Identifier); ok {
			if smap, ok := g.localStructTypeMap[g.currentFunc]; ok {
				structName = smap[ident.Value]
			}
		}
		if layout, ok := g.structDefs[structName]; ok {
			for i := range layout.Fields {
				if layout.Fields[i].Name == e.Property {
					return layout.Fields[i].Kind
				}
			}
		}
		return KindScalar
	case *parser.IndexExpression:
		return KindScalar
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "with-cap", "with-len", "with-cap-len":
				return KindVec
			}
		}
		// 方法呼叫 receiver.to-str() → 回傳 str
		if de, ok := e.Function.(*parser.DotExpression); ok {
			if de.Property == "to-str" || de.Property == "to_str" {
				return KindStr
			}
		}
		return KindScalar
	case *parser.IfExpression:
		// match desugar 後 `grade = x: { ... }` 變成 `grade = if {...} else {...}`。
		// 整體 kind 由最後一個 arm 的結果表達式決定（consequence 與 alternative 應一致）。
		// 優先看 consequence 末尾；若為空，再看 alternative。
		if k := g.ifArmResultKind(e.Consequence); k != KindScalar {
			return k
		}
		return g.ifArmResultKind(e.Alternative)
	}
	return KindScalar
}

// ifArmResultKind 回傳 if/match 分支 body 末尾表達式的 kind。
// body 為 nil 或不含 ExpressionStatement 時回傳 KindScalar。
// 若 body 是 elif 鏈包裝（單條 ExpressionStatement{IfExpression}），遞迴推斷。
func (g *Generator) ifArmResultKind(body *parser.BlockStatement) ValKind {
	if body == nil || len(body.Statements) == 0 {
		return KindScalar
	}
	last := body.Statements[len(body.Statements)-1]
	if es, ok := last.(*parser.ExpressionStatement); ok {
		return g.inferKind(es.Expression)
	}
	return KindScalar
}

// localKind 回傳當前函數中某變數的 ValKind。
func (g *Generator) localKind(name string) ValKind {
	if kmap, ok := g.localKindMap[g.currentFunc]; ok {
		if k, ok := kmap[name]; ok {
			return k
		}
	}
	return KindScalar
}

// setLocalKind 設定當前函數中某變數的 ValKind 與相關型別資訊。
// 用於 emitLetStmt 在 addLocal 之後呼叫。
func (g *Generator) setLocalKind(name string, ls *parser.LetStatement) {
	kind := KindScalar
	if ls.Type != nil {
		kind = kindFromType(ls.Type)
	} else if ls.Value != nil {
		kind = g.inferKind(ls.Value)
	}
	if kmap, ok := g.localKindMap[g.currentFunc]; ok {
		kmap[name] = kind
	}

	// 記錄 struct 型別名稱
	var structName string
	if sl, ok := ls.Value.(*parser.StructLiteral); ok {
		structName = sl.Type
	} else if ls.Type != nil {
		if nt, ok := ls.Type.(*parser.NamedType); ok {
			if _, isStruct := g.structDefs[nt.Value]; isStruct {
				structName = nt.Value
			}
		}
	}
	if structName != "" {
		if smap, ok := g.localStructTypeMap[g.currentFunc]; ok {
			smap[name] = structName
		}
	}

	// 記錄 vec/arr 元素型別
	if lt, ok := ls.Type.(*parser.SliceType); ok && lt.Elem != nil {
		if emap, ok := g.localElemTypeMap[g.currentFunc]; ok {
			emap[name] = ValTypeFromName(lt.Elem)
		}
	} else if at, ok := ls.Type.(*parser.ArrayType); ok && at.Elem != nil {
		if emap, ok := g.localElemTypeMap[g.currentFunc]; ok {
			emap[name] = ValTypeFromName(at.Elem)
		}
	} else if _, ok := ls.Value.(*parser.SliceLiteral); ok {
		// 從字面量推斷元素型別（皆視為 i64）
		if emap, ok := g.localElemTypeMap[g.currentFunc]; ok {
			emap[name] = I64
		}
	}
}

// ---- str 描述符負載 ----

// emitStrDescriptorLoad 發射從 str 描述符載入 data 指標與 len 的指令。
// 堆疊：[descPtr] → [dataPtr, len]（len 在頂端）。
func (g *Generator) emitStrDescriptorLoad(sb *bytes.Buffer) {
	// 複製 descPtr：local.tee $tmp; 之後再 local.get $tmp
	tmpIdx := g.addLocal("$str.desc", I32)
	sb.WriteByte(OpLocalTee)
	writeU32LEB(sb, uint32(tmpIdx))
	// 載入 data (desc+8)
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	// 載入 len (desc+0)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
}

// ---- 字串拼接 ----

// emitStrConcat 發射字串拼接程式碼。
// 堆疊：[desc1: i32, desc2: i32] → [newDesc: i32]。
//
// 流程：
//  1. 載入 len1, len2；total = len1 + len2
//  2. malloc(total) → newBuf
//  3. 載入 data1, memcpy newBuf ← data1 for len1 bytes
//  4. 載入 data2, memcpy newBuf+len1 ← data2 for len2 bytes
//  5. malloc(12) → newDesc
//  6. store len=total, cap=total, data=newBuf
//  7. 回傳 newDesc
func (g *Generator) emitStrConcat(sb *bytes.Buffer) {
	// locals
	desc1Idx := g.addLocal("$cat.d1", I32)
	desc2Idx := g.addLocal("$cat.d2", I32)
	len1Idx := g.addLocal("$cat.l1", I32)
	len2Idx := g.addLocal("$cat.l2", I32)
	totalIdx := g.addLocal("$cat.tot", I32)
	bufIdx := g.addLocal("$cat.buf", I32)
	descIdx := g.addLocal("$cat.desc", I32)

	// pop desc2, desc1
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(desc2Idx))
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(desc1Idx))

	// len1 = desc1.len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(desc1Idx))
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(len1Idx))

	// len2 = desc2.len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(desc2Idx))
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(len2Idx))

	// total = len1 + len2
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(len1Idx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(len2Idx))
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(totalIdx))

	// buf = malloc(total)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(totalIdx))
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(bufIdx))

	// memcpy(buf, data1, len1) via memory.copy
	// 堆疊順序：dst, src, n
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(desc1Idx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(len1Idx))
	g.emitMemoryCopy(sb)

	// memcpy(buf + len1, data2, len2)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(len1Idx))
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(desc2Idx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(len2Idx))
	g.emitMemoryCopy(sb)

	// desc = malloc(12)
	writeI32ConstU(sb, DescriptorHeaderSize)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// desc.len = total
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(totalIdx))
	g.emitI32Store(sb)

	// desc.cap = total
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldCap)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(totalIdx))
	g.emitI32Store(sb)

	// desc.data = buf
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	g.emitI32Store(sb)

	// return desc
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
}

// ---- 方法呼叫 ----

// emitMultiAssignStmt 處理多目標賦值：v1, v2 = expr。
// 支援：
//   - rand.rand(state) → 2 個 i64 回傳值（new-state, r），分配給 2 個 Identifier 目標
//   - 用戶多返回值函數 swap(5, 9) → a, b（結果數量須等於目標數量）
//
// 對其他情況，發射 expr 後丟棄結果（最小可用）。
func (g *Generator) emitMultiAssignStmt(sb *bytes.Buffer, mas *parser.MultiAssignStatement) {
	n := len(mas.Targets)
	if n == 0 {
		return
	}
	// 特判 rand.rand(state)：產生 2 個 i64 值
	if ce, ok := mas.Value.(*parser.CallExpression); ok {
		if de, ok := ce.Function.(*parser.DotExpression); ok {
			if ident, ok := de.Receiver.(*parser.Identifier); ok {
				if ident.Value == "rand" && de.Property == "rand" && n == 2 {
					// 發射 rand.rand：堆疊留下 (new-state, r)，r 在頂端
					g.emitRandRand(sb, ce.Arguments)
					// 依序分配：第 2 個目標先（r 在頂端），第 1 個目標後
					// 目標必須是 Identifier
					for i := n - 1; i >= 0; i-- {
						if id, ok := mas.Targets[i].(*parser.Identifier); ok {
							idx := g.addLocal(id.Value, I64)
							g.localTypeMap[g.currentFunc][id.Value] = I64
							sb.WriteByte(OpLocalSet)
							writeU32LEB(sb, uint32(idx))
						}
					}
					return
				}
			}
		}
		// 用戶多返回值函數：swap(5, 9) → a, b
		if ident, ok := ce.Function.(*parser.Identifier); ok {
			if funcIdx, ok := g.funcTable[ident.Value]; ok {
				results := g.funcResults[ident.Value]
				if len(results) == n {
					// 發射引數
					for _, arg := range ce.Arguments {
						g.emitExpr(sb, arg)
					}
					// call：堆疊留下 n 個回傳值，最後一個在頂端
					sb.WriteByte(OpCall)
					writeU32LEB(sb, funcIdx)
					// 依序分配：第 n 個目標先（在堆疊頂端），第 1 個目標後
					for i := n - 1; i >= 0; i-- {
						if id, ok := mas.Targets[i].(*parser.Identifier); ok {
							idx := g.addLocal(id.Value, results[i])
							g.localTypeMap[g.currentFunc][id.Value] = results[i]
							sb.WriteByte(OpLocalSet)
							writeU32LEB(sb, uint32(idx))
						}
					}
					return
				}
			}
		}
	}
	// 其他情況：發射 value 並丟棄
	t := g.emitExpr(sb, mas.Value)
	if t != Void {
		sb.WriteByte(OpDrop)
	}
}

// emitMethodCall 處理 receiver.method(args) 方法呼叫。
// 區分三種情況：
//  1. 模組限定呼叫：receiver 是未定義的識別字且匹配已知模組（math/rand），
//     走 emitModuleCall。
//  2. 用戶定義方法：receiver 是 local 變數，且 "structType.method" 在 funcTable 中，
//     發射 call（self 作為首個參數）。
//  3. 內建方法：如 x.to-str()，走 emitToStrMethod。
func (g *Generator) emitMethodCall(sb *bytes.Buffer, de *parser.DotExpression, args []parser.Expression) ValType {
	// 判斷是否為模組限定呼叫（receiver 不是 local 變數）
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		if _, isLocal := g.lookupLocal(ident.Value); !isLocal {
			return g.emitModuleCall(sb, ident.Value, de.Property, args)
		}
		// 用戶定義方法：查 "structType.method" 是否在 funcTable 中
		if structName, ok := g.localStructTypeMap[g.currentFunc][ident.Value]; ok {
			fullName := structName + "." + de.Property
			if funcIdx, ok := g.funcTable[fullName]; ok {
				// 發射 self 作為首個參數
				sb.WriteByte(OpLocalGet)
				if idx, ok := g.lookupLocal(ident.Value); ok {
					writeU32LEB(sb, uint32(idx))
				}
				// 發射其餘參數
				for _, arg := range args {
					g.emitExpr(sb, arg)
				}
				sb.WriteByte(OpCall)
				writeU32LEB(sb, funcIdx)
				results := g.funcResults[fullName]
				if len(results) > 0 {
					return results[0]
				}
				return Void
			}
		}
	}
	switch de.Property {
	case "to-str", "to_str":
		return g.emitToStrMethod(sb, de.Receiver)
	default:
		sb.WriteByte(OpUnreachable)
		return Void
	}
}

// emitModuleCall 處理模組限定呼叫 module.func(args)。
// 支援：
//   - math.max(a, b) / math.min(a, b) / math.sqrt(x) → f64
//   - rand.rand(state) → 留下 (new-state: i64, r: i64) 在堆疊上（多回傳值）
func (g *Generator) emitModuleCall(sb *bytes.Buffer, module, name string, args []parser.Expression) ValType {
	switch module {
	case "math":
		switch name {
		case "max":
			// f64.max (0xA5)
			g.emitExpr(sb, args[0])
			g.emitExpr(sb, args[1])
			sb.WriteByte(0xA5) // f64.max
			return F64
		case "min":
			// f64.min (0xA4)
			g.emitExpr(sb, args[0])
			g.emitExpr(sb, args[1])
			sb.WriteByte(0xA4) // f64.min
			return F64
		case "sqrt":
			// f64.sqrt (0x9F)
			g.emitExpr(sb, args[0])
			sb.WriteByte(OpF64Sqrt)
			return F64
		}
	case "rand":
		if name == "rand" {
			return g.emitRandRand(sb, args)
		}
	}
	sb.WriteByte(OpUnreachable)
	return Void
}

// emitRandRand 發射 rand.rand(state) 內建：xorshift32。
// 輸入：state i64。輸出：堆疊留下 (new-state i64, r i64)，r 在頂端。
//   new-state = state ^ (state << 13) ^ (state << 13 >> 17) ^ (state << 13 >> 17 << 5)
//   r = new-state & 0xFFFFFFFF
func (g *Generator) emitRandRand(sb *bytes.Buffer, args []parser.Expression) ValType {
	stateIdx := g.addLocal("$rr.s", I64)
	tmpIdx := g.addLocal("$rr.t", I64)
	rIdx := g.addLocal("$rr.r", I64)

	// state = arg
	g.emitExpr(sb, args[0])
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(stateIdx))

	// tmp = state ^ (state << 13)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(stateIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(stateIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 13)
	sb.WriteByte(OpI64Shl)
	sb.WriteByte(OpI64Xor)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(tmpIdx))

	// tmp = tmp ^ (tmp >> 17)  (logical shift for unsigned behavior)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 17)
	sb.WriteByte(OpI64ShrU)
	sb.WriteByte(OpI64Xor)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(tmpIdx))

	// tmp = tmp ^ (tmp << 5)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 5)
	sb.WriteByte(OpI64Shl)
	sb.WriteByte(OpI64Xor)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(tmpIdx))

	// r = tmp & 0xFFFFFFFF
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 0xFFFFFFFF)
	sb.WriteByte(OpI64And)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(rIdx))

	// 推入 (new-state, r): state 在底，r 在頂
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(tmpIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(rIdx))
	return I64 // 回傳型別標記為 I64（頂端值）；多回傳值由呼叫端處理
}

// emitToStrMethod 發射 receiver.to-str()：將數值轉為 str 描述符（I32）。
// receiver 型別由 lookupLocalType 推斷（i64 → itoa；f64 → 定點格式化）。
func (g *Generator) emitToStrMethod(sb *bytes.Buffer, receiver parser.Expression) ValType {
	// 推斷 receiver 型別
	recType := I64
	if ident, ok := receiver.(*parser.Identifier); ok {
		if t, ok := g.lookupLocalType(ident.Value); ok {
			recType = t
		}
	}

	lenIdx := g.addLocal("$ts.len", I32)
	ptrIdx := g.addLocal("$ts.ptr", I32)
	bufIdx := g.addLocal("$ts.buf", I32)
	descIdx := g.addLocal("$ts.desc", I32)

	// 依型別產生 (ptr, len) — len 在堆疊頂端
	switch recType {
	case F64:
		// f64.to-str()：預設精度 6，使用 emitF64ToBuffer 寫入 FmtBuffer
		g.emitExpr(sb, receiver)
		g.emitF64ToBuffer(sb, 6) // 堆疊：[ptr, len]
	default:
		// i64.to-str()（含 bool/byte/char 等 i32/i64 型別）
		g.emitExpr(sb, receiver)
		if recType == I32 {
			sb.WriteByte(OpI64ExtendI32S)
		}
		g.emitI64ToString(sb) // 堆疊：[ptr, len]
	}

	// 儲存 ptr/len
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(lenIdx))
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(ptrIdx))

	// buf = malloc(len)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(bufIdx))

	// memory.copy(buf, ptr, len)：堆疊 [dst, src, n]
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(ptrIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitMemoryCopy(sb)

	// desc = malloc(12)
	writeI32ConstU(sb, DescriptorHeaderSize)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// desc.len = len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitI32Store(sb)

	// desc.cap = len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldCap)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitI32Store(sb)

	// desc.data = buf
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	g.emitI32Store(sb)

	// return desc
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	return I32
}

// ---- vec literal ----

// emitVecLiteral 發射 vec 字面量 [e1, e2, ...] 的程式碼。
// 回傳 ValType = I32（描述符指標）。元素型別固定為 I64（最小可用）。
func (g *Generator) emitVecLiteral(sb *bytes.Buffer, sl *parser.SliceLiteral) ValType {
	n := uint32(len(sl.Elements))
	elemSize := uint32(8) // i64

	descIdx := g.addLocal("$vec.desc", I32)
	bufIdx := g.addLocal("$vec.buf", I32)

	// buf = malloc(n * elemSize)（n=0 時配置 0 byte，malloc 仍回傳有效指標）
	writeI32ConstU(sb, n*elemSize)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(bufIdx))

	// 對每個元素：emit 值，store 至 buf + i*elemSize
	for i, elem := range sl.Elements {
		// addr = buf + i*elemSize
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(bufIdx))
		writeI32ConstU(sb, uint32(i)*elemSize)
		sb.WriteByte(OpI32Add)
		// 值
		g.emitExpr(sb, elem)
		g.emitI64Store(sb)
	}

	// desc = malloc(12)
	writeI32ConstU(sb, DescriptorHeaderSize)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// desc.len = n
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	writeI32ConstU(sb, n)
	g.emitI32Store(sb)

	// desc.cap = n
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldCap)
	sb.WriteByte(OpI32Add)
	writeI32ConstU(sb, n)
	g.emitI32Store(sb)

	// desc.data = buf
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	g.emitI32Store(sb)

	// return desc
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	return I32
}

// emitWithCap 發射 with-cap(n) / with-len(n) / with-cap-len(cap, len) 內建。
// 建立一個空 vec 描述符，配置 cap*8 byte 的 data 緩衝。
// stackIn: [n: i32] for with-cap/with-len; [cap: i32, len: i32] for with-cap-len。
// 回傳 I32（描述符指標）。
func (g *Generator) emitWithCap(sb *bytes.Buffer, name string, argc int) ValType {
	descIdx := g.addLocal("$wc.desc", I32)
	bufIdx := g.addLocal("$wc.buf", I32)
	capIdx := g.addLocal("$wc.cap", I32)
	lenIdx := g.addLocal("$wc.len", I32)

	if argc == 2 {
		// with-cap-len(cap, len): stack = [cap, len]
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(lenIdx))
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(capIdx))
	} else {
		// with-cap(n) / with-len(n): len = (name == "with-len") ? n : 0; cap = n
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(capIdx))
		if name == "with-len" {
			sb.WriteByte(OpLocalGet)
			writeU32LEB(sb, uint32(capIdx))
			sb.WriteByte(OpLocalSet)
			writeU32LEB(sb, uint32(lenIdx))
		} else {
			writeI32ConstU(sb, 0)
			sb.WriteByte(OpLocalSet)
			writeU32LEB(sb, uint32(lenIdx))
		}
	}

	// buf = malloc(cap * 8)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(capIdx))
	// i32.shl by 3 = *8（cap << 3）
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 3)
	sb.WriteByte(OpI32Shl)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(bufIdx))

	// desc = malloc(12)
	writeI32ConstU(sb, DescriptorHeaderSize)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// desc.len = len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	g.emitI32Store(sb)

	// desc.cap = cap
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldCap)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(capIdx))
	g.emitI32Store(sb)

	// desc.data = buf
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(bufIdx))
	g.emitI32Store(sb)

	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	return I32
}

// ---- IndexExpression ----

// emitIndexExpr 發射 vec[i] / arr[i] 索引表達式（含 bounds check）。
// 回傳元素 ValType（I64 for i64 元素）。
func (g *Generator) emitIndexExpr(sb *bytes.Buffer, ie *parser.IndexExpression) ValType {
	// 推斷元素型別：由 Left 識別字的 elemType 決定，預設 I64
	elemType := I64
	if ident, ok := ie.Left.(*parser.Identifier); ok {
		if emap, ok := g.localElemTypeMap[g.currentFunc]; ok {
			if et, ok := emap[ident.Value]; ok {
				elemType = et
			}
		}
	}

	descIdx := g.addLocal("$idx.desc", I32)
	idxIdx := g.addLocal("$idx.i", I32)

	// 評估 left → desc ptr
	g.emitExpr(sb, ie.Left)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// 評估 index → i32
	t := g.emitExpr(sb, ie.Index)
	if t == I64 {
		sb.WriteByte(OpI32WrapI64)
	}
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(idxIdx))

	// bounds check: if (idx >= len) proc_exit(1)
	// 堆疊需求：[idx, len]
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idxIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	g.emitBoundsCheck(sb)

	// 計算 addr = data + idx * elemSize
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idxIdx))
	// idx * elemSize
	switch elemType {
	case I64, F64:
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 3)
		sb.WriteByte(OpI32Shl)
		sb.WriteByte(OpI32Add)
		g.emitI64Load(sb)
		return I64
	default:
		// i32 元素：idx * 4
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 2)
		sb.WriteByte(OpI32Shl)
		sb.WriteByte(OpI32Add)
		g.emitI32Load(sb)
		return I32
	}
}

// emitIndexAssign 發射 vec[i] = value 賦值。
// 即 AssignExpression{Left: IndexExpression, Value: expr}。
func (g *Generator) emitIndexAssign(sb *bytes.Buffer, ie *parser.IndexExpression, value parser.Expression) {
	// 推斷元素型別
	elemType := I64
	if ident, ok := ie.Left.(*parser.Identifier); ok {
		if emap, ok := g.localElemTypeMap[g.currentFunc]; ok {
			if et, ok := emap[ident.Value]; ok {
				elemType = et
			}
		}
	}

	descIdx := g.addLocal("$ia.desc", I32)
	idxIdx := g.addLocal("$ia.i", I32)

	// desc = left
	g.emitExpr(sb, ie.Left)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(descIdx))

	// idx = index
	t := g.emitExpr(sb, ie.Index)
	if t == I64 {
		sb.WriteByte(OpI32WrapI64)
	}
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(idxIdx))

	// bounds check
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idxIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	g.emitBoundsCheck(sb)

	// 計算 addr = data + idx * elemSize
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(descIdx))
	g.emitI32ConstOffset(sb, DescriptorFieldData)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idxIdx))
	switch elemType {
	case I64, F64:
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 3)
		sb.WriteByte(OpI32Shl)
		sb.WriteByte(OpI32Add)
		// 值
		vt := g.emitExpr(sb, value)
		if vt == I32 {
			sb.WriteByte(OpI64ExtendI32S)
		}
		g.emitI64Store(sb)
	default:
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 2)
		sb.WriteByte(OpI32Shl)
		sb.WriteByte(OpI32Add)
		g.emitExpr(sb, value)
		g.emitI32Store(sb)
	}
}

// ---- struct literal / field access ----

// emitStructLit 發射 struct literal（user{name: 'a', age: 20}）。
// malloc struct.Size bytes，按欄位偏移寫入值，回傳 struct 指標（I32）。
func (g *Generator) emitStructLit(sb *bytes.Buffer, sl *parser.StructLiteral) ValType {
	layout, ok := g.structDefs[sl.Type]
	if !ok {
		// 未知 struct：推入 0
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 0)
		return I32
	}

	structIdx := g.addLocal("$st.ptr", I32)

	// ptr = malloc(layout.Size)
	writeI32ConstU(sb, layout.Size)
	g.emitMallocCall(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(structIdx))

	// 對每個欄位：查 offset，emit 值，store
	for _, fld := range sl.Fields {
		// 查欄位佈局
		var fl *StructFieldLayout
		for i := range layout.Fields {
			if layout.Fields[i].Name == fld.Name {
				fl = &layout.Fields[i]
				break
			}
		}
		if fl == nil || fld.Value == nil {
			continue
		}
		// addr = ptr + offset
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(structIdx))
		writeI32ConstU(sb, fl.Offset)
		sb.WriteByte(OpI32Add)
		// 值
		vt := g.emitExpr(sb, fld.Value)
		// 依欄位型別 store
		switch fl.Type {
		case I64, F64:
			if vt == I32 {
				sb.WriteByte(OpI64ExtendI32S)
			}
			g.emitI64Store(sb)
		default:
			if vt == I64 {
				sb.WriteByte(OpI32WrapI64)
			}
			g.emitI32Store(sb)
		}
	}

	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(structIdx))
	return I32
}

// emitDotExpr 發射 struct 欄位存取（u.name）。
// 堆疊：留下一個值（欄位型別）。
func (g *Generator) emitDotExpr(sb *bytes.Buffer, de *parser.DotExpression) ValType {
	// 取得 receiver 的 struct 型別名稱
	var structName string
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		if smap, ok := g.localStructTypeMap[g.currentFunc]; ok {
			structName = smap[ident.Value]
		}
	}
	layout, ok := g.structDefs[structName]
	if !ok {
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		return I64
	}

	// 查欄位佈局
	var fl *StructFieldLayout
	for i := range layout.Fields {
		if layout.Fields[i].Name == de.Property {
			fl = &layout.Fields[i]
			break
		}
	}
	if fl == nil {
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		return I64
	}

	// 評估 receiver → struct ptr
	g.emitExpr(sb, de.Receiver)
	// addr = ptr + offset
	writeI32ConstU(sb, fl.Offset)
	sb.WriteByte(OpI32Add)

	// 依欄位型別 load
	switch fl.Type {
	case I64, F64:
		g.emitI64Load(sb)
		return I64
	default:
		g.emitI32Load(sb)
		return I32
	}
}

// emitDotAssign 發射 u.name = value 賦值。
// 即 AssignExpression{Left: DotExpression, Value: expr}。
func (g *Generator) emitDotAssign(sb *bytes.Buffer, de *parser.DotExpression, value parser.Expression) {
	var structName string
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		if smap, ok := g.localStructTypeMap[g.currentFunc]; ok {
			structName = smap[ident.Value]
		}
	}
	layout, ok := g.structDefs[structName]
	if !ok {
		return
	}
	var fl *StructFieldLayout
	for i := range layout.Fields {
		if layout.Fields[i].Name == de.Property {
			fl = &layout.Fields[i]
			break
		}
	}
	if fl == nil {
		return
	}

	// addr = receiver + offset
	g.emitExpr(sb, de.Receiver)
	writeI32ConstU(sb, fl.Offset)
	sb.WriteByte(OpI32Add)

	vt := g.emitExpr(sb, value)
	switch fl.Type {
	case I64, F64:
		if vt == I32 {
			sb.WriteByte(OpI64ExtendI32S)
		}
		g.emitI64Store(sb)
	default:
		if vt == I64 {
			sb.WriteByte(OpI32WrapI64)
		}
		g.emitI32Store(sb)
	}
}

// ---- len / cap 內建 ----

// emitLenCall 發射 len(arg) 或 arg.len：載入描述符的 len 欄位。
// 堆疊：[arg] → [len: i32]。呼叫端會視需要 extend 至 i64。
func (g *Generator) emitLenCall(sb *bytes.Buffer, arg parser.Expression) ValType {
	g.emitExpr(sb, arg)
	g.emitI32ConstOffset(sb, DescriptorFieldLen)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	return I32
}

// emitCapCall 發射 cap(arg) 或 arg.cap：載入描述符的 cap 欄位。
func (g *Generator) emitCapCall(sb *bytes.Buffer, arg parser.Expression) ValType {
	g.emitExpr(sb, arg)
	g.emitI32ConstOffset(sb, DescriptorFieldCap)
	sb.WriteByte(OpI32Add)
	g.emitI32Load(sb)
	return I32
}

// ---- print str 變數 ----

// emitPrintStrVar 印出一個 str 變數的內容。
// 變數名稱指向一個 str 描述符（i32 local）。
func (g *Generator) emitPrintStrVar(sb *bytes.Buffer, name string) {
	idx, ok := g.lookupLocal(name)
	if !ok {
		return
	}
	// 從描述符載入 data 與 len
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idx))
	g.emitStrDescriptorLoad(sb)
	// 堆疊：[data, len]
	lenIdx := g.addLocal("$ps.len", I32)
	ptrIdx := g.addLocal("$ps.ptr", I32)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(lenIdx))
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(ptrIdx))
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
