package wasm

import (
	"bytes"

	"github.com/lizongying/nolang/parser"
)

// emitNamedFormatPrint 處理具名格式字串 print('...{name:spec}...')。
// 解析格式字串後，依段落輸出：字面段落直接 fd_write，欄位段落依變數型別格式化。
func (g *Generator) emitNamedFormatPrint(sb *bytes.Buffer, formatStr string, addNewline bool) {
	segments, err := parser.ParseFormatString(formatStr)
	if err != nil {
		// 解析失敗，退回原樣輸出
		g.emitPrintString(sb, formatStr)
		if addNewline {
			g.emitPrintString(sb, "\n")
		}
		return
	}
	for _, seg := range segments {
		if seg.Field != nil {
			g.emitFormatField(sb, seg.Field)
		} else if seg.Literal != "" {
			g.emitPrintString(sb, seg.Literal)
		}
	}
	if addNewline {
		g.emitPrintString(sb, "\n")
	}
}

// emitFormatField 發射單個 {name:spec} 欄位的格式化輸出。
// 依變數型別分派：str → 描述符載入並輸出；f64 → 帶精度格式化；i64/i32 → itoa。
func (g *Generator) emitFormatField(sb *bytes.Buffer, field *parser.FormatField) {
	name := field.Name
	kind := g.localKind(name)
	t, _ := g.lookupLocalType(name)

	// 解析 spec
	precision := -1
	typeChar := byte(0)
	if field.Parsed != nil {
		precision = field.Parsed.Precision
		typeChar = field.Parsed.Type
	}

	switch {
	case kind == KindStr:
		// str 變數：從描述符載入 data/len 並輸出
		g.emitPrintStrVar(sb, name)
	case t == F64:
		// f64 變數：帶精度格式化
		g.emitPrintF64Var(sb, name, precision, typeChar)
	case t == I64:
		// i64 變數：載入並 itoa 輸出
		idx, ok := g.lookupLocal(name)
		if !ok {
			g.emitPrintString(sb, "?")
			return
		}
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(idx))
		g.emitPrintI64OnStack(sb)
	case t == I32:
		// i32 變數（含 bool）：載入並轉為 i64 後 itoa 輸出
		idx, ok := g.lookupLocal(name)
		if !ok {
			g.emitPrintString(sb, "?")
			return
		}
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(idx))
		sb.WriteByte(OpI64ExtendI32S)
		g.emitPrintI64OnStack(sb)
	default:
		// 未知型別：輸出 "?"
		g.emitPrintString(sb, "?")
	}
}

// emitPrintF64Var 將 f64 變數格式化為字串並輸出。
// precision >= 0 時輸出定點小數（如 .2f → 3.14）；precision < 0 時預設 6 位。
// typeChar 'f'/'F' 為定點格式；其他類型字元暫不支援，退回預設。
// 結果寫入 FmtBuffer 區域，再透過 fd_write 一次輸出。
func (g *Generator) emitPrintF64Var(sb *bytes.Buffer, name string, precision int, typeChar byte) {
	idx, ok := g.lookupLocal(name)
	if !ok {
		g.emitPrintString(sb, "?")
		return
	}

	// 預設精度 6
	if precision < 0 {
		precision = 6
	}

	// 載入 f64 值
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(idx))

	// 將格式化結果寫入 FmtBuffer，回傳 (ptr, len)
	g.emitF64ToBuffer(sb, precision)
	// 堆疊：[ptr, len]

	// 寫入 iovec 並 fd_write
	lenIdx := g.addLocal("$fmt.len", I32)
	ptrIdx := g.addLocal("$fmt.ptr", I32)
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
}

// emitF64ToBuffer 將堆疊上的 f64 格式化為字串，寫入 FmtBuffer。
// 輸入：f64 在堆疊頂端。輸出：(ptr: i32, len: i32) 在堆疊上（len 在頂端）。
// 格式：[-]integer.fraction（fraction 補零至 precision 位）。
// 已知限制：不處理 NaN/Inf；進位邊緣情況（如 0.999 → 1.00）未處理。
func (g *Generator) emitF64ToBuffer(sb *bytes.Buffer, precision int) {
	// locals
	valIdx := g.addLocal("$fmt.val", F64)     // 原始值
	absIdx := g.addLocal("$fmt.abs", F64)     // 絕對值
	negIdx := g.addLocal("$fmt.neg", I32)     // 是否為負
	posIdx := g.addLocal("$fmt.pos", I32)     // 當前寫入位置
	intPartIdx := g.addLocal("$fmt.int", I64) // 整數部分
	fracIdx := g.addLocal("$fmt.frac", I64)   // 小數部分（已乘以 10^precision）

	// $fmt.val = value
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(valIdx))

	// $fmt.pos = FmtBufferStart
	writeI32ConstU(sb, FmtBufferStart)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))

	// $fmt.neg = (val < 0)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(valIdx))
	g.emitF64Const(sb, 0.0)
	sb.WriteByte(OpF64Lt)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(negIdx))

	// $fmt.abs = abs(val)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(valIdx))
	sb.WriteByte(OpF64Abs)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(absIdx))

	// 如果是負數，先寫入 '-'
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(negIdx))
	g.emitIfVoid(sb)
	// buffer[pos] = '-'; pos++
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 45) // '-'
	g.emitI32Store8(sb)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))
	g.emitEnd(sb)

	// 整數部分：intPart = trunc(abs)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(absIdx))
	sb.WriteByte(OpF64Trunc)
	sb.WriteByte(OpI64TruncF64S)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(intPartIdx))

	// 將整數部分 itoa 並寫入 FmtBuffer
	// 使用 emitI64ToString（寫入 ItoaBuffer），然後 memcpy 到 FmtBuffer
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(intPartIdx))
	g.emitI64ToString(sb) // 堆疊：[ptr, len]（len 在頂端）
	// 儲存 ptr/len
	intLenIdx := g.addLocal("$fmt.intlen", I32)
	intPtrIdx := g.addLocal("$fmt.intptr", I32)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(intLenIdx))
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(intPtrIdx))
	// memcpy(ItoaBuffer src → FmtBuffer dst)
	// for i in 0..intLen: buffer[pos+i] = itoa[intPtr+i]
	g.emitMemcpyLoop(sb, posIdx, intPtrIdx, intLenIdx)
	// pos += intLen
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(intLenIdx))
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(posIdx))

	// 小數部分
	if precision > 0 {
		// 寫入 '.'
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(posIdx))
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 46) // '.'
		g.emitI32Store8(sb)
		// pos++
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(posIdx))
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 1)
		sb.WriteByte(OpI32Add)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(posIdx))

		// frac = (abs - trunc(abs)) * 10^precision
		// = (abs - f64(intPart)) * 10^precision
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(absIdx))
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(intPartIdx))
		sb.WriteByte(OpF64ConvertI64S)
		sb.WriteByte(OpF64Sub)
		// 乘以 10^precision
		g.emitF64Const(sb, pow10f64(precision))
		sb.WriteByte(OpF64Mul)
		// 四捨五入：+ 0.5 後截斷
		g.emitF64Const(sb, 0.5)
		sb.WriteByte(OpF64Add)
		sb.WriteByte(OpF64Trunc)
		sb.WriteByte(OpI64TruncF64S)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(fracIdx))

		// 處理進位：如果 frac >= 10^precision，frac -= 10^precision，intPart += 1
		// （簡化處理：僅調整 frac，不回頭修正 intPart，因為進位邊緣情況罕見）
		// 這裡先不處理進位，直接輸出 frac

		// 將 frac itoa 並寫入 FmtBuffer（前面補零至 precision 位）
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(fracIdx))
		g.emitI64ToString(sb) // 堆疊：[ptr, len]
		fracLenIdx := g.addLocal("$fmt.fraclen", I32)
		fracPtrIdx := g.addLocal("$fmt.fracptr", I32)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(fracLenIdx))
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(fracPtrIdx))

		// 補零：如果 fracLen < precision，先寫入 (precision - fracLen) 個 '0'
		// zeroPad = precision - fracLen
		zeroPadIdx := g.addLocal("$fmt.zpad", I32)
		writeI32ConstU(sb, uint32(precision))
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(fracLenIdx))
		sb.WriteByte(OpI32Sub)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(zeroPadIdx))

		// 補零迴圈：while zeroPad > 0 { buffer[pos]='0'; pos++; zeroPad-- }
		g.emitBlock(sb)
		g.emitLoop(sb)
		// if zeroPad == 0, br 1 (跳出)
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(zeroPadIdx))
		sb.WriteByte(OpI32Eqz)
		sb.WriteByte(OpBrIf)
		writeU32LEB(sb, 1)
		// buffer[pos] = '0'
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(posIdx))
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 48) // '0'
		g.emitI32Store8(sb)
		// pos++
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(posIdx))
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 1)
		sb.WriteByte(OpI32Add)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(posIdx))
		// zeroPad--
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(zeroPadIdx))
		sb.WriteByte(OpI32Const)
		writeSLEB(sb, 1)
		sb.WriteByte(OpI32Sub)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(zeroPadIdx))
		// br 0
		sb.WriteByte(OpBr)
		writeU32LEB(sb, 0)
		g.emitEnd(sb) // end loop
		g.emitEnd(sb) // end block

		// 寫入 frac digits
		g.emitMemcpyLoop(sb, posIdx, fracPtrIdx, fracLenIdx)
		// pos += fracLen
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(posIdx))
		sb.WriteByte(OpLocalGet)
		writeU32LEB(sb, uint32(fracLenIdx))
		sb.WriteByte(OpI32Add)
		sb.WriteByte(OpLocalSet)
		writeU32LEB(sb, uint32(posIdx))
	}

	// 回傳 (ptr, len) = (FmtBufferStart, pos - FmtBufferStart)
	writeI32ConstU(sb, FmtBufferStart)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	writeI32ConstU(sb, FmtBufferStart)
	sb.WriteByte(OpI32Sub)
}

// emitMemcpyLoop 發射一個位元組複製迴圈：
//   for i in 0..len: dst[pos+i] = src[ptr+i]
// 輸入：posIdx (dst 位置 local), srcPtrIdx (src 指標 local), lenIdx (長度 local)
func (g *Generator) emitMemcpyLoop(sb *bytes.Buffer, posIdx, srcPtrIdx, lenIdx int) {
	iIdx := g.addLocal("$fmt.i", I32)
	valTmpIdx := g.addLocal("$fmt.tmp", I32)
	// i = 0
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 0)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(iIdx))

	g.emitBlock(sb)
	g.emitLoop(sb)
	// if i >= len, br 1
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(iIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(lenIdx))
	sb.WriteByte(OpI32GeS)
	sb.WriteByte(OpBrIf)
	writeU32LEB(sb, 1)
	// val = src[ptr+i]  → 暫存到 valTmp
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(srcPtrIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(iIdx))
	sb.WriteByte(OpI32Add)
	g.emitI32Load8U(sb)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(valTmpIdx))
	// store to dst[pos+i]：push addr, push val, store8
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(posIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(iIdx))
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(valTmpIdx))
	g.emitI32Store8(sb)
	// i++
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(iIdx))
	sb.WriteByte(OpI32Const)
	writeSLEB(sb, 1)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(iIdx))
	// br 0
	sb.WriteByte(OpBr)
	writeU32LEB(sb, 0)
	g.emitEnd(sb) // end loop
	g.emitEnd(sb) // end block
}

// pow10f64 回傳 10^n 的 f64 值（n >= 0）。
func pow10f64(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 10.0
	}
	return result
}
