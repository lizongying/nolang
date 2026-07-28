package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// generateSliceViewAssignment handles `view = base[start..end]` by registering
// a slice view alias instead of creating an independent struct.
// Returns true if handled, false if fallback to generateSliceExpression is needed.
//
// 當目標是輸出參數（會逃逸函數作用域）或顯式標註為 SliceType（[]T，即 vec 類型）時，
// 執行完全克隆（malloc + memcpy），因為切片是視圖，不能脫離函數作用域；
// vec 需要獨立擁有數據。
func (g *Generator) generateSliceViewAssignment(sb *strings.Builder, stmt *parser.LetStatement, name string) bool {
	sliceExpr, ok := stmt.Value.(*parser.SliceExpression)
	if !ok {
		return false
	}

	// Only handle Identifier base (not complex expressions like arr[i][0..4])
	ident, ok := sliceExpr.Left.(*parser.Identifier)
	if !ok {
		return false
	}
	baseVar := ident.Value

	// Get base variable type
	baseType := ""
	if g.varTypes != nil {
		baseType = g.varTypes[baseVar]
	}
	if baseType == "" {
		return false
	}

	// 切片表達式總是克隆：slice 是視圖，共享原數組 data。
	// 若後續原數組被修改或釋放，視圖會懸空（use-after-free）。
	// 因此所有 slice 賦值都執行 malloc + memcpy，使目標獨立擁有 data。
	needClone := true

	// Check if base is itself a slice view (chained slicing)
	// 注意：由於總是 clone，sliceViews map 不再被填充，此分支實際上不會觸發。
	// 保留以維持向後相容性。
	if baseView, isView := g.sliceViews[baseVar]; isView {
		return g.generateChainedSliceViewClone(sb, name, baseView, sliceExpr)
	}

	isStr := baseType == "%str-long"

	// Determine element type and size
	elemType := "i64"
	if isStr {
		elemType = "i8"
	} else if et, ok := g.arrayElemTypes[baseVar]; ok {
		elemType = et
	}
	elemSize := int64(8)
	if isStr {
		elemSize = 1
	} else {
		if s := g.llvmTypeSize(elemType); s > 0 {
			elemSize = s
		}
	}

	basePtr := g.varAddr(baseVar)

	// Get source data pointer and length from the base struct
	var srcData, srcLen string

	{
		structType := "%arr"
		dataField := uint32(1)
		if baseType == "%vec" {
			structType = "%vec"
			dataField = 2
		} else if baseType == "%str-long" {
			structType = "%str-long"
			dataField = 2 // %str-long = {i64 len, i64 cap, i64 data}
		}

		// Load source len (field 0)
		g.tmpIdx++
		srcLenGEP := fmt.Sprintf("%%sv.srclen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		srcLen = fmt.Sprintf("%%sv.srclen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
				g.indent(), srcLenGEP, structType, structType, basePtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
				g.indent(), srcLen, srcLenGEP))
		}

		// Load source data pointer
		g.tmpIdx++
		srcDataGEP := fmt.Sprintf("%%sv.srcdata.gep.%d", g.tmpIdx)
		g.tmpIdx++
		srcData = fmt.Sprintf("%%sv.srcdata.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), srcDataGEP, structType, structType, basePtr, dataField))
			srcData = g.loadDataPtrField(sb, srcDataGEP)
		}
	}

	// Compute start offset and view length
	startReg, viewLenReg := g.computeSliceBounds(sb, sliceExpr.Range, srcLen)

	// Compute adjusted data pointer: srcData + start * elemSize
	dataPtrReg := g.computeAdjustedDataPtr(sb, srcData, startReg, elemSize)

	// 需要完全克隆：malloc 新緩衝區 + memcpy，寫入目標變量地址
	// 注意：克隆後的變量獨立擁有 data，不再是切片視圖。
	// 必須追蹤為 heapVars，使後續 b = view 賦值走深層 clone 路徑，
	// 並在函數結束時正確 free。
	if needClone {
		g.emitSliceClone(sb, name, dataPtrReg, viewLenReg, elemType, elemSize, isStr)
		// 追蹤為堆變數（僅局部變數，非輸出參數；輸出參數由呼叫者管理）
		if g.outputParamNames == nil || !g.outputParamNames[name] {
			resultType := "%vec"
			if isStr {
				resultType = "%str-long"
			}
			g.trackLocalHeapVar(name, resultType)
		}
		return true
	}

	// Register the slice view
	resultType := baseType
	if isStr {
		resultType = "%str-long"
	}

	g.sliceViews[name] = &sliceViewInfo{
		baseVar:    baseVar,
		baseType:   resultType,
		startOff:   startReg,
		viewLen:    viewLenReg,
		dataPtrReg: dataPtrReg,
		elemType:   elemType,
		isStr:      isStr,
	}

	// Set variable type to match the base (so accessors know the element type)
	g.varTypes[name] = resultType
	g.arrayElemTypes[name] = elemType
	g.funcLocalNames[name] = true

	return true
}

// generateChainedSliceView handles `sub = view[2..5]` where view is already a slice view.
// Computes offsets relative to the original base.
func (g *Generator) generateChainedSliceView(sb *strings.Builder, name string, baseView *sliceViewInfo, sliceExpr *parser.SliceExpression) bool {
	// Compute start offset and view length relative to the base view
	startReg, viewLenReg := g.computeSliceBounds(sb, sliceExpr.Range, baseView.viewLen)

	// Compute the absolute start offset from the original base
	var absStartReg string
	if startReg == "0" {
		absStartReg = baseView.startOff
	} else if baseView.startOff == "0" {
		absStartReg = startReg
	} else {
		g.tmpIdx++
		absStartReg = fmt.Sprintf("%%sv.absstart.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n",
				g.indent(), absStartReg, baseView.startOff, startReg))
		}
	}

	// Compute adjusted data pointer from the base view's data pointer
	elemSize := int64(8)
	if baseView.isStr {
		elemSize = 1
	} else {
		if s := g.llvmTypeSize(baseView.elemType); s > 0 {
			elemSize = s
		}
	}
	dataPtrReg := g.computeAdjustedDataPtr(sb, baseView.dataPtrReg, startReg, elemSize)

	g.sliceViews[name] = &sliceViewInfo{
		baseVar:    baseView.baseVar,
		baseType:   baseView.baseType,
		startOff:   absStartReg,
		viewLen:    viewLenReg,
		dataPtrReg: dataPtrReg,
		elemType:   baseView.elemType,
		isStr:      baseView.isStr,
	}

	g.varTypes[name] = baseView.baseType
	g.arrayElemTypes[name] = baseView.elemType
	g.funcLocalNames[name] = true

	return true
}

// generateChainedSliceViewClone 處理 `out = view[2..5]`，其中 view 已是切片視圖，
// 且目標需要完全克隆（輸出參數或顯式 vec 類型）。
// 計算相對偏移後，從原始 base 的數據指針執行 malloc + memcpy 克隆到目標變量。
func (g *Generator) generateChainedSliceViewClone(sb *strings.Builder, name string, baseView *sliceViewInfo, sliceExpr *parser.SliceExpression) bool {
	// 計算相對於 base view 的起始偏移和視圖長度
	startReg, viewLenReg := g.computeSliceBounds(sb, sliceExpr.Range, baseView.viewLen)

	// 計算從 base view 數據指針出發的調整後指針
	elemSize := int64(8)
	if baseView.isStr {
		elemSize = 1
	} else {
		if s := g.llvmTypeSize(baseView.elemType); s > 0 {
			elemSize = s
		}
	}
	dataPtrReg := g.computeAdjustedDataPtr(sb, baseView.dataPtrReg, startReg, elemSize)

	// 執行完全克隆到目標變量
	// 注意：克隆後的變量獨立擁有 data，不再是切片視圖。
	// 必須追蹤為 heapVars，使後續 b = view 賦值走深層 clone 路徑，
	// 並在函數結束時正確 free。
	g.emitSliceClone(sb, name, dataPtrReg, viewLenReg, baseView.elemType, elemSize, baseView.isStr)
	if g.outputParamNames == nil || !g.outputParamNames[name] {
		resultType := "%vec"
		if baseView.isStr {
			resultType = "%str-long"
		}
		g.trackLocalHeapVar(name, resultType)
	}
	return true
}

// emitSliceClone 將切片數據完全克隆到目標變量：malloc 新緩衝區 + memcpy。
// 用於切片視圖逃逸函數作用域（輸出參數）或顯式 vec 類型賦值。
// 切片是視圖，不能脫離函數作用域；vec 需要獨立擁有數據。
func (g *Generator) emitSliceClone(sb *strings.Builder, destVar, srcDataPtr, viewLenReg, elemType string, elemSize int64, isStr bool) {
	if sb == nil {
		// 類型註冊仍需執行（即使 sb 為 nil，例如類型推導階段）
		resultType := "%vec"
		if isStr {
			resultType = "%str-long"
		}
		g.varTypes[destVar] = resultType
		g.arrayElemTypes[destVar] = elemType
		g.funcLocalNames[destVar] = true
		return
	}

	// 計算字節長度：byteLen = viewLen * elemSize
	g.tmpIdx++
	byteLenReg := fmt.Sprintf("%%svclone.bytelen.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n",
		g.indent(), byteLenReg, viewLenReg, elemSize))

	// malloc 新緩衝區
	g.tmpIdx++
	bufReg := fmt.Sprintf("%%svclone.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n",
		g.indent(), bufReg, byteLenReg))

	// memcpy 從源數據指針到新緩衝區
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), bufReg, srcDataPtr, byteLenReg))

	// 將 len/cap/data 寫入目標變量地址
	destPtr := g.varAddr(destVar)
	resultType := "%vec"
	dataFieldIdx := uint32(2)
	if isStr {
		resultType = "%str-long"
		dataFieldIdx = 2 // %str-long 也是 field 2 存 data
	}

	// store len (field 0)
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%svclone.len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, resultType, resultType, destPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), viewLenReg, lenGEP))

	// store cap (field 1) = viewLen
	g.tmpIdx++
	capGEP := fmt.Sprintf("%%svclone.cap.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
		g.indent(), capGEP, resultType, resultType, destPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), viewLenReg, capGEP))

	// store data (field 2)
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%svclone.data.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, resultType, resultType, destPtr, dataFieldIdx))
	g.storeDataPtrField(sb, bufReg, dataGEP)

	// 註冊變量類型
	g.varTypes[destVar] = resultType
	g.arrayElemTypes[destVar] = elemType
	// 全域變數不應標記為 funcLocalNames，否則後續存取會用 %name 而非 @name
	if g.globalVars == nil || !g.globalVars[destVar] {
		g.funcLocalNames[destVar] = true
	}
}

// computeSliceBounds computes the start offset and view length from a RangeExpression.
// srcLen is the source's total length (used for [..] and [start..] cases).
func (g *Generator) computeSliceBounds(sb *strings.Builder, r *parser.RangeExpression, srcLen string) (startReg, viewLenReg string) {
	startReg = "0"
	if r != nil && r.Start != nil {
		startVal := g.generateExprWithSB(sb, r.Start)
		if !r.LeftInc {
			// ( exclusive: start = start + 1
			g.tmpIdx++
			startPlus := fmt.Sprintf("%%sv.start.plus.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), startPlus, startVal))
			}
			startReg = startPlus
		} else {
			startReg = startVal
		}
	} else if r != nil && r.Start == nil && !r.LeftInc {
		// (..end / (..] / (..) / (.. : 左開無下限，起始偏移 +1
		startReg = "1"
	}

	if r == nil || (r.Start == nil && r.End == nil) {
		// Full slice: [..] / [..) / (..] / (..)
		// right-inclusive (]) → viewLen = srcLen - start
		// right-exclusive ()) → viewLen = srcLen - start - 1 (排除最後一個元素)
		rightExcl := r != nil && !r.RightInc
		if startReg == "0" && !rightExcl {
			viewLenReg = srcLen
		} else {
			g.tmpIdx++
			viewLenReg = fmt.Sprintf("%%sv.viewlen.%d", g.tmpIdx)
			if sb != nil {
				if startReg == "0" {
					// right-exclusive only: viewLen = srcLen - 1
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n",
						g.indent(), viewLenReg, srcLen))
				} else if rightExcl {
					// left-exclusive + right-exclusive: viewLen = srcLen - start - 1
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
						g.indent(), viewLenReg, srcLen, startReg))
					g.tmpIdx++
					extraSub := fmt.Sprintf("%%sv.viewlen.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n",
						g.indent(), extraSub, viewLenReg))
					viewLenReg = extraSub
				} else {
					// left-exclusive only: viewLen = srcLen - start
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
						g.indent(), viewLenReg, srcLen, startReg))
				}
			}
		}
	} else if r.Start == nil && r.End != nil {
		// [..end] / (..end]: view_len = end - start + (1 if ] else 0)
		endVal := g.generateExprWithSB(sb, r.End)
		g.tmpIdx++
		subReg := fmt.Sprintf("%%sv.sublen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
				g.indent(), subReg, endVal, startReg))
		}
		if r.RightInc {
			g.tmpIdx++
			viewLenReg = fmt.Sprintf("%%sv.viewlen.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), viewLenReg, subReg))
			}
		} else {
			viewLenReg = subReg
		}
	} else if r.Start != nil && r.End == nil {
		// [start..]: view_len = src_len - start
		if startReg == "0" {
			viewLenReg = srcLen
		} else {
			g.tmpIdx++
			viewLenReg = fmt.Sprintf("%%sv.viewlen.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
					g.indent(), viewLenReg, srcLen, startReg))
			}
		}
	} else {
		// [start..end]: view_len = end - start + (1 if ] else 0)
		endVal := g.generateExprWithSB(sb, r.End)
		g.tmpIdx++
		subReg := fmt.Sprintf("%%sv.sublen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
				g.indent(), subReg, endVal, startReg))
		}
		if r.RightInc {
			g.tmpIdx++
			viewLenReg = fmt.Sprintf("%%sv.viewlen.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), viewLenReg, subReg))
			}
		} else {
			viewLenReg = subReg
		}
	}

	return startReg, viewLenReg
}

// computeAdjustedDataPtr computes srcData + start * elemSize as an i8* GEP.
func (g *Generator) computeAdjustedDataPtr(sb *strings.Builder, srcData, startReg string, elemSize int64) string {
	if startReg == "0" {
		return srcData
	}
	g.tmpIdx++
	offsetReg := fmt.Sprintf("%%sv.offset.%d", g.tmpIdx)
	g.tmpIdx++
	dataPtrReg := fmt.Sprintf("%%sv.newdata.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n",
			g.indent(), offsetReg, startReg, elemSize))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
			g.indent(), dataPtrReg, srcData, offsetReg))
	}
	return dataPtrReg
}

// materializeSliceView creates a temporary %vec or %str-long struct from a slice view.
// Used when a struct pointer is needed (method calls, function arguments).
// The created struct shares the original data (no copy).
func (g *Generator) materializeSliceView(sb *strings.Builder, varName string) string {
	view, ok := g.sliceViews[varName]
	if !ok {
		return ""
	}

	if view.isStr {
		// Materialize as %str-long { len, cap, data }
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%svmat.str.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultReg))
			// Store len (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%svmat.str.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
				g.indent(), lenGEP, resultReg))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), view.viewLen, lenGEP))
			// Store cap (field 1) = viewLen
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%svmat.str.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
				g.indent(), capGEP, resultReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), view.viewLen, capGEP))
			// Store data (field 2)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%svmat.str.data.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, resultReg))
		g.storeDataPtrField(sb, view.dataPtrReg, dataGEP)
	}
	return resultReg
}

	// Materialize as %vec { len, cap, data }
	// cap is set to view length (no separate capacity tracking for views)
	g.tmpIdx++
	resultReg := fmt.Sprintf("%%svmat.vec.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), resultReg))
		// Store len (field 0)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%svmat.vec.len.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, resultReg))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), view.viewLen, lenGEP))
		// Store cap = len (field 1) — no separate cap for views
		g.tmpIdx++
		capGEP := fmt.Sprintf("%%svmat.vec.cap.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
			g.indent(), capGEP, resultReg))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), view.viewLen, capGEP))
		// Store data (field 2)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%svmat.vec.data.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, resultReg))
		g.storeDataPtrField(sb, view.dataPtrReg, dataGEP)
	}
	return resultReg
}

// isSliceViewVar checks if a variable name is a registered slice view.
func (g *Generator) isSliceViewVar(name string) bool {
	if g.sliceViews == nil {
		return false
	}
	_, ok := g.sliceViews[name]
	return ok
}

// sliceViewLen returns the LLVM i64 value for a slice view's length.
func (g *Generator) sliceViewLen(name string) string {
	if view, ok := g.sliceViews[name]; ok {
		return view.viewLen
	}
	return ""
}

// sliceViewDataPtr returns the LLVM i8* register for a slice view's adjusted data pointer.
func (g *Generator) sliceViewDataPtr(name string) string {
	if view, ok := g.sliceViews[name]; ok {
		return view.dataPtrReg
	}
	return ""
}
