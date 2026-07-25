package llvm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// llvmTypeAlign 返回 LLVM 類型的對齊字節數（與 LLVM ABI 對齊規則一致）。
func (g *Generator) llvmTypeAlign(llvmType string) int64 {
	if strings.HasPrefix(llvmType, "[") {
		var n int64
		var elem string
		if _, err := fmt.Sscanf(llvmType, "[%d x %s]", &n, &elem); err == nil {
			return g.llvmTypeAlign(elem)
		}
		return 8
	}
	if strings.HasPrefix(llvmType, "%") {
		structName := strings.TrimPrefix(llvmType, "%")
		if g.structTypes != nil {
			if fields, ok := g.structTypes[structName]; ok {
				var maxAlign int64 = 1
				for _, f := range fields {
					a := g.llvmTypeAlign(f.typ)
					if a > maxAlign {
						maxAlign = a
					}
				}
				return maxAlign
			}
		}
	}
	switch llvmType {
	case "i1", "i8":
		return 1
	case "i16":
		return 2
	case "i32", "float":
		return 4
	case "i64", "i8*", "double":
		return 8
	default:
		return 8
	}
}

func (g *Generator) llvmTypeSize(llvmType string) int64 {
	if strings.HasPrefix(llvmType, "[") {
		// [N x T] → N * sizeof(T)
		// 注意：fmt.Sscanf 的 %s 會貪婪讀取到字串末尾（包含 ']'），
		// 導致 elem 型別解析錯誤。改用 parseInlineArrayType（基於 SplitN）。
		if n, elem, ok := parseInlineArrayType(llvmType); ok {
			return n * g.llvmTypeSize(elem)
		}
		return 64 // fallback
	}
	// struct（含內建 %vec/%str-long 與用戶自定義）：依 LLVM ABI 對齊規則計算
	// 每個欄位先對齊到自身對齊值，末尾再補齊到結構體最大對齊值。
	if strings.HasPrefix(llvmType, "%") {
		structName := strings.TrimPrefix(llvmType, "%")
		if g.structTypes != nil {
			if fields, ok := g.structTypes[structName]; ok {
				var offset int64
				var maxAlign int64 = 1
				for _, f := range fields {
					fa := g.llvmTypeAlign(f.typ)
					if fa > maxAlign {
						maxAlign = fa
					}
					// 將 offset 向上對齊到欄位對齊值
					offset = (offset + fa - 1) / fa * fa
					offset += g.llvmTypeSize(f.typ)
				}
				// 結構體總大小補齊到最大對齊值
				if maxAlign > 0 {
					offset = (offset + maxAlign - 1) / maxAlign * maxAlign
				}
				if offset > 0 {
					return offset
				}
			}
		}
	}
	switch llvmType {
	case "i1":
		return 1
	case "i8":
		return 1
	case "i16":
		return 2
	case "i32":
		return 4
	case "i64", "i8*", "double":
		return 8
	case "%option":
		return 24
	case "float":
		return 4
	default:
		return 8
	}
}

// shouldStackAllocArray 判定局部固定陣列是否適合用 alloca（棧分配）而非 malloc（堆分配）。
// 條件：元素為基礎定寬型別（i1/i8/i16/i32/i64/double/float）且總尺寸 ≤ 閾值。
// 超大陣列、vec、含動態類型元素（struct/指標）的陣列仍用堆分配。
func shouldStackAllocArray(llvmElemType string, totalSize int64) bool {
	const maxStackArrayBytes = int64(256) // 64 * i8 = 64B, 足夠 buf[64]byte 等常見小陣列
	switch llvmElemType {
	case "i1", "i8", "i16", "i32", "i64", "double", "float":
		return totalSize > 0 && totalSize <= maxStackArrayBytes
	default:
		return false
	}
}

func (g *Generator) emitLifetimeEnd(sb *strings.Builder) {
	for _, v := range g.funcVars {
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.end.p0i8(i64 %d, i8* %s)\n", g.indent(), v.Size, llvmVarRef(v.Name)))
	}
}

// flushOutputBindings 在函數返回前，按當前 SSA 版本查表，
// 將延遲綁定的輸出參數值實際寫入輸出參數指標。
// SSA 版本由 if 分支前後的 save/restore 隔離：
// - 分支內的賦值遞增版本，return 時 flush 用遞增後的版本查表 → 正確
// - 分支後版本恢復，隱式返回 flush 用恢復後的版本查表 → 不會命中分支內的綁定
// - 無綁定的版本（如常量賦值的立即 store）跳過 flush
func (g *Generator) flushOutputBindings(sb *strings.Builder) {
	if g.outputBindings == nil || len(g.outputBindings) == 0 {
		return
	}
	for name, versions := range g.outputBindings {
		if len(versions) == 0 {
			continue
		}
		version := 0
		if g.ssaVersion != nil {
			version = g.ssaVersion[name]
		}
		binding, ok := versions[version]
		if !ok {
			continue // 該版本無延遲綁定，立即 store 已處理
		}
		paramPtr := g.varAddr(name)
		paramType, ok := g.varTypes[name]
		if !ok {
			paramType = binding.llvmType
		}
		g.tmpIdx++
		srcLoad := fmt.Sprintf("%%outmove.src.%d", g.tmpIdx)
		srcPtr := g.varAddr(binding.sourceVar)
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), srcLoad, binding.llvmType, binding.llvmType, srcPtr))
		// 型別轉換：源型別與參數型別可能不同（如 i64 → i8, i64 → float）
		storeVal := srcLoad
		if binding.llvmType != paramType {
			g.tmpIdx++
			convReg := fmt.Sprintf("%%outmove.conv.%d", g.tmpIdx)
			converted := true
			switch {
			case g.isIntegerLLVMType(binding.llvmType) && g.isIntegerLLVMType(paramType):
				// 整數 → 整數：trunc 或 zext
				order := map[string]int{"i8": 8, "i16": 16, "i32": 32, "i64": 64}
				if order[binding.llvmType] > order[paramType] {
					sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
				}
			case g.isIntegerLLVMType(binding.llvmType) && (paramType == "float" || paramType == "double"):
				// 整數 → 浮點：sitofp
				sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			case (binding.llvmType == "float" || binding.llvmType == "double") && g.isIntegerLLVMType(paramType):
				// 浮點 → 整數：fptosi
				sb.WriteString(fmt.Sprintf("%s%s = fptosi %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			case binding.llvmType == "float" && paramType == "double":
				// float → double：fpext
				sb.WriteString(fmt.Sprintf("%s%s = fpext %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			case binding.llvmType == "double" && paramType == "float":
				// double → float：fptrunc
				sb.WriteString(fmt.Sprintf("%s%s = fptrunc %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			default:
				// 其他（如 struct 型別相同但名稱不同）：直接 store
				converted = false
			}
			if converted {
				storeVal = convReg
			}
		}
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
			g.indent(), paramType, storeVal, paramType, paramPtr))
	}
}

// emitHeapFree 在函數返回前（move 之後），釋放未被 move 的局部堆變數的資料緩衝區。
// heapVars 記錄所有有堆分配的局部變數，movedVars 記錄已 move 到輸出參數的變數（不應 free）。
// 各型別的 data 欄位索引（data 為 i64 地址值）：
//
//	%vec:       field 2 (i64 data)
//	%str-long:  field 2 (i64 data)
//	%arr:       field 1 (i64 data)

// trackLocalHeapVar 將局部變數加入 heapVars 追蹤，用於函數結束時深層 free。
// 跳過參數（paramNames）和輸出參數（outputParamNames，由呼叫者管理）。
// 同時分配 varIdx（堆變數下標），用於 bitmap bit 定位。
func (g *Generator) trackLocalHeapVar(name, llvmType string) {
	if g.heapVars == nil {
		return
	}
	if _, isLocal := g.funcLocalNames[name]; !isLocal {
		return
	}
	if _, isParam := g.paramNames[name]; isParam {
		return
	}
	if g.outputParamNames != nil && g.outputParamNames[name] {
		return
	}
	g.heapVars[name] = llvmType
	if g.heapVarIndex != nil {
		if _, exists := g.heapVarIndex[name]; !exists {
			g.heapVarIndex[name] = g.nextHeapVarIdx
			g.nextHeapVarIdx++
		}
	}
}

func (g *Generator) emitHeapFree(sb *strings.Builder) {
	if g.heapVars == nil || len(g.heapVars) == 0 {
		return
	}
	for name, llvmType := range g.heapVars {
		// 輸出參數本身不 free（由呼叫者管理）
		if g.outputParamNames != nil && g.outputParamNames[name] {
			continue
		}
		elemType := ""
		if g.arrayElemTypes != nil {
			elemType = g.arrayElemTypes[name]
		}
		varIdx, hasIdx := g.heapVarIndex[name]
		if !hasIdx {
			// 無 varIdx（非局部堆變數或未追蹤），直接 free
			g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
			continue
		}
		if g.hasBranchMove && g.movedBitmapBase != "" {
			// 有運行時 bitmap：生成 IR 檢查 bit
			// bit=1 → move 已發生，所有權轉移，跳過 free
			// bit=0 → move 未發生（分支未執行），仍擁有數據，需 free
			g.emitBitCheckFree(sb, name, varIdx, llvmType, elemType)
		} else {
			// 無 bitmap：編譯期檢查 movedVarBitset
			if g.isMovedVar(varIdx) {
				continue // 跳過 free（所有權轉移）
			}
			g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
		}
	}
}

// emitGlobalHeapFree 釋放模組級堆變數（LLVM globals，如 @vec、@str）。
// 這些變數由 generateMainFunction 的 top-level 語句初始化（malloc data），
// 但不在 heapVars 中（因 trackLocalHeapVar 跳過 globalVars），
// 需單獨釋放以避免長期運行服務的記憶體泄漏。
func (g *Generator) emitGlobalHeapFree(sb *strings.Builder) {
	if g.globalVars == nil || g.moduleVarTypes == nil {
		return
	}
	// 排序確保輸出順序確定
	sortedNames := make([]string, 0, len(g.moduleVarTypes))
	for name := range g.moduleVarTypes {
		if g.globalVars[name] {
			sortedNames = append(sortedNames, name)
		}
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		llvmType := g.moduleVarTypes[name]
		var elemType string
		if g.moduleArrayElemTypes != nil {
			elemType = g.moduleArrayElemTypes[name]
		}
		// 只釋放堆擁有型別（%vec/%str-long/%arr/%option/用戶結構體）
		// %option 需進一步檢查 inner type 是否為堆型別（在 emitOptionHeapFree 內處理）
		if llvmType != "%vec" && llvmType != "%str-long" && llvmType != "%arr" &&
			llvmType != "%option" && !g.isUserStructType(llvmType) {
			continue
		}
		g.emitVarHeapFree(sb, llvmGlobalRef(name), llvmType, elemType, name)
	}
}

// emitVarHeapFree 釋放單一變數的堆數據。
// 內建型別（%vec/%str-long/%arr）透過 data 欄位索引找到 i8* 並 free；
// 用戶結構體則遞迴釋放所有含堆數據的欄位。
// name 為變數名，用於查詢 optionInnerTypes（option 釋放時需要）；
// 遞迴場景（釋放 option 內部結構）可傳 ""，此時 %option 分支會走兜底路徑。
func (g *Generator) emitVarHeapFree(sb *strings.Builder, varPtr, llvmType, elemType, name string) {
	switch llvmType {
	case "%option":
		g.emitOptionHeapFree(sb, varPtr, name)
		return
	}
	var dataFieldIdx int
	switch llvmType {
	case "%vec", "%str-long":
		dataFieldIdx = 2
	case "%arr":
		dataFieldIdx = 1
	default:
		if g.isUserStructType(llvmType) {
			g.emitStructFieldsFree(sb, varPtr, llvmType)
		}
		return
	}
	if llvmType == "%str-long" {
		g.emitShallowDataFree(sb, varPtr, llvmType, dataFieldIdx)
		return
	}
	if g.isHeapOwningType(elemType) {
		g.emitDeepContainerFree(sb, varPtr, llvmType, dataFieldIdx, elemType)
		return
	}
	g.emitShallowDataFree(sb, varPtr, llvmType, dataFieldIdx)
}

// emitOptionHeapFree 釋放 %option 變數持有的堆 box。
// 僅當 inner type 為堆型別（%str-long/%vec/%arr/用戶結構體）且 tag != nil 時才釋放：
//  1. 從 optionInnerTypes[name] 取得 inner type；若無記錄或為純量，直接返回（安全但洩漏）
//  2. load tag (field 0)；若 == 1 (nil)，data 是 0，跳過（避免 inttoptr i64 0）
//  3. load data (field 1) 為 i64，inttoptr 回 i8*
//  4. NULL check（data 可能為 0 即使 tag != 1）
//  5. 若 inner 為堆型別：bitcast 到 inner*，呼叫 emitVarHeapFree 遞迴釋放 inner 的 data
//     （inner 是 %str-long → 釋放字串 data；是 %vec/%arr → 釋放容器 data；
//     是用戶結構體 → emitStructFieldsFree 遞迴釋放欄位）
//  6. call @free(i8* boxPtr) 釋放 box 本身
//
// 順序：先釋放 inner 的 data，再 free box，避免懸垂。
func (g *Generator) emitOptionHeapFree(sb *strings.Builder, optPtr, name string) {
	innerType := ""
	if g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[name]
	}
	// 全域 option 變數的 inner type 不在函數級 optionInnerTypes 中，
	// 嘗試從模組級備份讀取（emitGlobalHeapFree 場景）
	if innerType == "" && g.moduleOptionInnerTypes != nil {
		innerType = g.moduleOptionInnerTypes[name]
	}
	if innerType == "" || !g.isHeapOwningType(innerType) {
		return // 純量 option 或無 inner 資訊：不持有堆，跳過
	}

	g.tmpIdx++
	tid := g.tmpIdx

	// 1. load tag (field 0)
	tagGEP := fmt.Sprintf("%%optfree.tag.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), tagGEP, optPtr))
	tagReg := fmt.Sprintf("%%optfree.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), tagReg, tagGEP))

	// 2. icmp eq i64 tag, 1 → skip（tag==1 表示 nil，data 為 0）
	nilCmp := fmt.Sprintf("%%optfree.nil.%d", tid)
	skipLabel := fmt.Sprintf("optfree.skip.%d", tid)
	dataLabel := fmt.Sprintf("optfree.data.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 1\n", g.indent(), nilCmp, tagReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nilCmp, skipLabel, dataLabel))

	// 3. 非 nil：load data (field 1) i64, inttoptr to i8*
	g.emitLabel(sb, dataLabel)
	dataGEP := fmt.Sprintf("%%optfree.data.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
		g.indent(), dataGEP, optPtr))
	dataIntReg := fmt.Sprintf("%%optfree.data.i64.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataIntReg, dataGEP))
	boxPtrReg := fmt.Sprintf("%%optfree.box.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), boxPtrReg, dataIntReg))

	// 4. NULL check
	nullCmp := fmt.Sprintf("%%optfree.null.%d", tid)
	freeLabel := fmt.Sprintf("optfree.free.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, boxPtrReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, freeLabel))

	// 5. 非 NULL：先遞迴釋放 inner 的 data
	g.emitLabel(sb, freeLabel)
	innerPtrReg := fmt.Sprintf("%%optfree.inner.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), innerPtrReg, boxPtrReg, innerType))
	// 遞迴釋放 inner 的 data（不傳 name，inner 不是 option 變數本身）
	// inner 是 %str-long → 釋放字串 data；%vec/%arr → 釋放容器 data；用戶結構體 → 遞迴釋放欄位
	g.emitVarHeapFree(sb, innerPtrReg, innerType, "", "")

	// 6. 釋放 box 本身
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), boxPtrReg))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))

	g.emitLabel(sb, skipLabel)
}

// emitOptionDeepClone 深層 clone option 變數 b = a：
//   - malloc 新 box 並遞迴 clone inner 的堆數據，使 a/b 各自獨立擁有 box
//   - 與 vec/struct 的 b = a 一致，避免 a/b 共享 box 導致 double-free
//
// 流程：
//  1. load tag from src；store tag to dst
//  2. 若 tag == 1 (nil)：dst.data = 0，return
//  3. 若 inner 為堆型別：load src.data (i64) → inttoptr to innerType*；
//     malloc 新 box（inner struct size）→ bitcast to innerType*；
//     呼叫 emitDeepClone(srcInnerPtr, dstInnerPtr, innerType, "")；
//     ptrtoint 新 box to i64 → store to dst.data
//  4. 若 inner 為純量：直接 load src.data i64 → store to dst.data
//
// 若 inner type 未知（optionInnerTypes 缺失），退化為淺拷貝（共享 box）作為兜底。
func (g *Generator) emitOptionDeepClone(sb *strings.Builder, srcName, dstName string) {
	innerType := ""
	if g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[srcName]
	}
	// 也嘗試從 dst 取（option-to-option 推導可能寫到 dst）
	if innerType == "" && g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[dstName]
	}

	g.tmpIdx++
	tid := g.tmpIdx

	// 1. load tag from src
	srcTagGEP := fmt.Sprintf("%%optclone.src.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), srcTagGEP, llvmVarRef(srcName)))
	srcTagReg := fmt.Sprintf("%%optclone.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcTagReg, srcTagGEP))

	// store tag to dst
	dstTagGEP := fmt.Sprintf("%%optclone.dst.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), dstTagGEP, llvmVarRef(dstName)))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcTagReg, dstTagGEP))

	// 2. 若 tag == 1 (nil)：dst.data = 0，return
	nilCmp := fmt.Sprintf("%%optclone.nil.%d", tid)
	skipLabel := fmt.Sprintf("optclone.skip.%d", tid)
	cloneLabel := fmt.Sprintf("optclone.do.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 1\n", g.indent(), nilCmp, srcTagReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nilCmp, skipLabel, cloneLabel))

	g.emitLabel(sb, cloneLabel)

	// 3. 非 nil：根據 inner 決定 clone 策略
	if innerType == "" {
		// 兜底：無 inner 資訊，退化為淺拷貝 data（共享 box）
		// 此分支不應常見，僅作防禦
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataReg := fmt.Sprintf("%%optclone.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataReg, srcDataGEP))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcDataReg, dstDataGEP))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		g.emitLabel(sb, skipLabel)
		return
	}

	if g.isHeapOwningType(innerType) {
		// inner 為堆型別：malloc 新 box + emitDeepClone
		// load src.data i64
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataIntReg := fmt.Sprintf("%%optclone.src.i64.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataIntReg, srcDataGEP))
		// inttoptr to innerType*
		srcInnerPtrReg := fmt.Sprintf("%%optclone.src.inner.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %s*\n", g.indent(), srcInnerPtrReg, srcDataIntReg, innerType))

		// malloc 新 box（inner struct size）
		structSize := g.llvmTypeSize(innerType)
		if structSize == 0 {
			structSize = 8
		}
		newBoxI8Reg := fmt.Sprintf("%%optclone.newbox.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), newBoxI8Reg, structSize))
		// bitcast to innerType*
		newInnerPtrReg := fmt.Sprintf("%%optclone.dst.inner.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), newInnerPtrReg, newBoxI8Reg, innerType))

		// NULL check src inner ptr（src 可能是 nil option 但 tag 非 1 的邊界情況）
		srcNullCmp := fmt.Sprintf("%%optclone.srcnull.%d", tid)
		deepCloneLabel := fmt.Sprintf("optclone.deep.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s* %s, null\n", g.indent(), srcNullCmp, innerType, srcInnerPtrReg))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), srcNullCmp, skipLabel, deepCloneLabel))

		// 深層 clone inner
		g.emitLabel(sb, deepCloneLabel)
		g.emitDeepClone(sb, srcInnerPtrReg, newInnerPtrReg, innerType, "")

		// ptrtoint 新 box to i64, store to dst.data
		newBoxIntReg := fmt.Sprintf("%%optclone.newbox.i64.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), newBoxIntReg, newBoxI8Reg))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newBoxIntReg, dstDataGEP))
	} else {
		// inner 為純量：直接 copy i64 data
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataReg := fmt.Sprintf("%%optclone.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataReg, srcDataGEP))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcDataReg, dstDataGEP))
	}

	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.emitLabel(sb, skipLabel)
}

// outputParamBitIndex 返回 out 參數在 outputParamOrder 中的索引（即位圖 bit index）。
// 若不是 out 參數，返回 -1。
func (g *Generator) outputParamBitIndex(outName string) int {
	for i, n := range g.outputParamOrder {
		if n == outName {
			return i
		}
	}
	return -1
}

// detectBranchMoveToOut 預掃描函數體語句，檢測是否存在「分支內 move 到 out 參數」。
// move 定義：LetStatement 將局部變數（Identifier）賦值給 out 參數。
// 分支定義：IfExpression（Consequence/Alternative）、ForStatement（Body）、ConditionalExpression。
// 僅當此類模式存在時返回 true，用於決定是否分配函數級 u64 位圖變數。
//   - 無 move → 返回 false → 不分配位圖，全部 free
//   - move 不在分支 → 返回 false → 不分配位圖，編譯期 movedVarBitset 確定性跳過 free
//   - move 在分支 → 返回 true → 分配位圖，運行時雙重校驗
func detectBranchMoveToOut(stmts []parser.Statement, outNames map[string]bool) bool {
	found := false
	var scanExpr func(expr parser.Expression, inBranch bool)
	var scanStmts func(stmts []parser.Statement, inBranch bool)
	scanStmts = func(ss []parser.Statement, inBranch bool) {
		for _, st := range ss {
			if found {
				return
			}
			switch s := st.(type) {
			case *parser.LetStatement:
				if inBranch && s.Name != nil && outNames[s.Name.Value] {
					if _, ok := s.Value.(*parser.Identifier); ok {
						found = true
						return
					}
				}
				if s.Value != nil {
					scanExpr(s.Value, inBranch)
				}
			case *parser.ExpressionStatement:
				if s.Expression != nil {
					scanExpr(s.Expression, inBranch)
				}
			case *parser.ForStatement:
				if s.Init != nil {
					scanStmts([]parser.Statement{s.Init}, inBranch)
				}
				if s.Condition != nil {
					scanExpr(s.Condition, inBranch)
				}
				if s.Update != nil {
					scanStmts([]parser.Statement{s.Update}, inBranch)
				}
				if s.Body != nil {
					scanStmts(s.Body.Statements, true)
				}
				if s.IterRange != nil {
					scanExpr(s.IterRange, inBranch)
				}
				if s.CountExpr != nil {
					scanExpr(s.CountExpr, inBranch)
				}
			case *parser.BlockStatement:
				scanStmts(s.Statements, inBranch)
			case *parser.ReturnStatement:
				if s.ReturnValue != nil {
					scanExpr(s.ReturnValue, inBranch)
				}
			case *parser.MultiAssignStatement:
				for _, t := range s.Targets {
					scanExpr(t, inBranch)
				}
				if s.Value != nil {
					scanExpr(s.Value, inBranch)
				}
			}
		}
	}
	scanExpr = func(expr parser.Expression, inBranch bool) {
		if expr == nil || found {
			return
		}
		switch e := expr.(type) {
		case *parser.IfExpression:
			if e.Condition != nil {
				scanExpr(e.Condition, inBranch)
			}
			if e.Consequence != nil {
				scanStmts(e.Consequence.Statements, true)
			}
			if e.Alternative != nil {
				scanStmts(e.Alternative.Statements, true)
			}
		case *parser.ConditionalExpression:
			if e.Condition != nil {
				scanExpr(e.Condition, inBranch)
			}
			if e.Consequence != nil {
				scanExpr(e.Consequence, true)
			}
			if e.Alternative != nil {
				scanExpr(e.Alternative, true)
			}
		case *parser.InfixExpression:
			scanExpr(e.Left, inBranch)
			scanExpr(e.Right, inBranch)
		case *parser.PrefixExpression:
			scanExpr(e.Right, inBranch)
		case *parser.CallExpression:
			scanExpr(e.Function, inBranch)
			for _, a := range e.Arguments {
				scanExpr(a, inBranch)
			}
		case *parser.GroupedExpression:
			scanExpr(e.Expression, inBranch)
		case *parser.IndexExpression:
			scanExpr(e.Left, inBranch)
			if e.Index != nil {
				scanExpr(e.Index, inBranch)
			}
		case *parser.DotExpression:
			scanExpr(e.Receiver, inBranch)
		case *parser.SliceExpression:
			scanExpr(e.Left, inBranch)
			if e.Range != nil {
				scanExpr(e.Range.Start, inBranch)
				scanExpr(e.Range.End, inBranch)
			}
		case *parser.AssignExpression:
			scanExpr(e.Left, inBranch)
			scanExpr(e.Value, inBranch)
		case *parser.CastExpression:
			scanExpr(e.Expr, inBranch)
		case *parser.RunExpression:
			scanExpr(e.Call, inBranch)
		case *parser.AwaitExpression:
			scanExpr(e.Right, inBranch)
		case *parser.IterationExpr:
			if e.RangeExpr != nil {
				scanExpr(e.RangeExpr, inBranch)
			}
			if e.Range != nil {
				scanExpr(e.Range.Start, inBranch)
				scanExpr(e.Range.End, inBranch)
			}
		}
	}
	scanStmts(stmts, false)
	return found
}

// ---- 編譯期位圖操作（無運行時 bitmap 時用）----

// markMovedVar 編譯期標記堆變數為 moved（無 bitmap 時用）。
// 所有權已轉移，函數結束時跳過 free。
func (g *Generator) markMovedVar(varIdx int) {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	for len(g.movedVarBitset) <= block {
		g.movedVarBitset = append(g.movedVarBitset, 0)
	}
	g.movedVarBitset[block] |= 1 << offset
}

// unmarkMovedVar 編譯期清除堆變數 moved 標記（覆蓋清舊）。
// out 參數重新綁定到別的變數時，舊變數恢復所有權，函數結束時需 free。
func (g *Generator) unmarkMovedVar(varIdx int) {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	if block < len(g.movedVarBitset) {
		g.movedVarBitset[block] &^= 1 << offset
	}
}

// isMovedVar 編譯期檢查堆變數是否 moved（無 bitmap 時用）。
func (g *Generator) isMovedVar(varIdx int) bool {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	if block >= len(g.movedVarBitset) {
		return false
	}
	return g.movedVarBitset[block]&(1<<offset) != 0
}

// ---- 運行時 bitmap IR 生成（有 bitmap 時用）----

// emitSetMovedBitIR 生成 IR：設置堆變數對應的 bitmap bit=1（表示 move 已發生）。
// IR: %old = load bitmap[block]; %mask = or %old, (1<<offset); store %mask, bitmap[block]
func (g *Generator) emitSetMovedBitIR(sb *strings.Builder, varIdx int) {
	if g.movedBitmapBase == "" {
		return
	}
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	g.tmpIdx++
	oldVal := fmt.Sprintf("%%mb.old.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldVal, bvName))
	g.tmpIdx++
	maskVal := fmt.Sprintf("%%mb.mask.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %d\n", g.indent(), maskVal, oldVal, 1<<offset))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), maskVal, bvName))
}

// emitClearMovedBitIR 生成 IR：清除堆變數對應的 bitmap bit=0（覆蓋清舊）。
// IR: %old = load bitmap[block]; %mask = and %old, ~(1<<offset); store %mask, bitmap[block]
func (g *Generator) emitClearMovedBitIR(sb *strings.Builder, varIdx int) {
	if g.movedBitmapBase == "" {
		return
	}
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	g.tmpIdx++
	oldVal := fmt.Sprintf("%%mb.clr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldVal, bvName))
	g.tmpIdx++
	maskVal := fmt.Sprintf("%%mb.clrm.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %d\n", g.indent(), maskVal, oldVal, ^(1 << offset)))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), maskVal, bvName))
}

// emitBitCheckFree 生成 IR：檢查堆變數對應的 bitmap bit。
// bit=1 → move 已發生，所有權轉移，跳過 free；
// bit=0 → move 未發生（分支未執行），仍擁有數據，需 free。
func (g *Generator) emitBitCheckFree(sb *strings.Builder, name string, varIdx int, llvmType, elemType string) {
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	g.tmpIdx++
	bv := fmt.Sprintf("%%dc.bv.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), bv, bvName))
	g.tmpIdx++
	masked := fmt.Sprintf("%%dc.masked.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %d\n", g.indent(), masked, bv, 1<<offset))
	g.tmpIdx++
	moved := fmt.Sprintf("%%dc.moved.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp ne i64 %s, 0\n", g.indent(), moved, masked))
	g.tmpIdx++
	freeLabel := fmt.Sprintf("dc.free.%d", g.tmpIdx)
	g.tmpIdx++
	skipLabel := fmt.Sprintf("dc.skip.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n",
		g.indent(), moved, skipLabel, freeLabel))
	// free block: move 未發生（bit=0），仍擁有數據，需 free
	g.emitLabel(sb, freeLabel)
	g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	// skip block: move 已發生（bit=1），所有權已轉移，跳過 free
	g.emitLabel(sb, skipLabel)
}

// ---- move 賦值處理 ----

// handleMoveToOut 處理 move 到 out 參數：清舊 bit + 設新 bit + 更新 outBindState。
// 適用於 `out = x`（x 是局部堆變數，out 是輸出參數）。
func (g *Generator) handleMoveToOut(sb *strings.Builder, srcName, outName string) {
	srcVarIdx, ok := g.heapVarIndex[srcName]
	if !ok {
		return // 源不是局部堆變數
	}
	outIdx := g.outputParamBitIndex(outName)
	if outIdx < 0 {
		return
	}
	// 1. 清舊：若 out 參數之前綁定了別的變數，清除舊變數的 bit
	if outIdx < len(g.outBindState) {
		oldVarIdx := g.outBindState[outIdx]
		if oldVarIdx >= 0 {
			if g.hasBranchMove {
				g.emitClearMovedBitIR(sb, oldVarIdx) // 運行時清 bit
			} else {
				g.unmarkMovedVar(oldVarIdx) // 編譯期清 bit
			}
		}
	}
	// 2. 設新：設置當前變數的 bit
	if g.hasBranchMove {
		g.emitSetMovedBitIR(sb, srcVarIdx) // 運行時設 bit
	} else {
		g.markMovedVar(srcVarIdx) // 編譯期設 bit
	}
	// 3. 更新 outBindState
	if outIdx < len(g.outBindState) {
		g.outBindState[outIdx] = srcVarIdx
	}
}

// handleMoveLocal 處理局部間 move（b = a，不能深拷貝時）：僅設 bit。
// 所有權從 a 轉移到 b，a 不再擁有數據，函數結束時跳過 free。
func (g *Generator) handleMoveLocal(sb *strings.Builder, srcName string) {
	srcVarIdx, ok := g.heapVarIndex[srcName]
	if !ok {
		return
	}
	if g.hasBranchMove {
		g.emitSetMovedBitIR(sb, srcVarIdx) // 運行時設 bit
	} else {
		g.markMovedVar(srcVarIdx) // 編譯期設 bit
	}
}

// emitShallowDataFree releases a container's data buffer without iterating elements.
func (g *Generator) emitShallowDataFree(sb *strings.Builder, containerPtr, containerType string, dataFieldIdx int) {
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%heapfree.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, containerType, containerType, containerPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	g.emitNullCheckFree(sb, dataLoad)
}

// emitNullCheckFree frees an i8* pointer with NULL check.
func (g *Generator) emitNullCheckFree(sb *strings.Builder, dataPtr string) {
	g.tmpIdx++
	nullCmp := fmt.Sprintf("%%heapfree.null.%d", g.tmpIdx)
	g.tmpIdx++
	freeLabel := fmt.Sprintf("heapfree.free.%d", g.tmpIdx)
	g.tmpIdx++
	skipLabel := fmt.Sprintf("heapfree.skip.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, dataPtr))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, freeLabel))
	g.emitLabel(sb, freeLabel)
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), dataPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.emitLabel(sb, skipLabel)
}

// emitDeepContainerFree deep-frees a %vec/%arr: iterates elements to free their heap data, then frees the data buffer.
func (g *Generator) emitDeepContainerFree(sb *strings.Builder, containerPtr, containerType string, dataFieldIdx int, elemType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	lenGEP := fmt.Sprintf("%%df.len.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, containerType, containerType, containerPtr))
	lenReg := fmt.Sprintf("%%df.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
	dataGEP := fmt.Sprintf("%%df.data.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, containerType, containerType, containerPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	nullCmp := fmt.Sprintf("%%df.null.%d", tid)
	skipLabel := fmt.Sprintf("df.skip.%d", tid)
	loopStartLabel := fmt.Sprintf("df.loop.start.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, dataLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, loopStartLabel))
	g.emitLabel(sb, loopStartLabel)
	iPtr := fmt.Sprintf("%%df.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("df.loop.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopCondLabel)
	iVal := fmt.Sprintf("%%df.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%df.loop.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("df.loop.body.%d", tid)
	loopEndLabel := fmt.Sprintf("df.loop.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.emitLabel(sb, loopBodyLabel)
	elemArr := fmt.Sprintf("%%df.elemarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), elemArr, dataLoad, elemType))
	elemGEP := fmt.Sprintf("%%df.elem.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), elemGEP, elemType, elemType, elemArr, iVal))
	g.emitElementFree(sb, elemGEP, elemType)
	iNext := fmt.Sprintf("%%df.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopEndLabel)
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), dataLoad))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.emitLabel(sb, skipLabel)
}

// emitElementFree frees heap data of a single container element.
func (g *Generator) emitElementFree(sb *strings.Builder, elemPtr, elemType string) {
	var dataFieldIdx int
	switch elemType {
	case "%vec", "%str-long":
		dataFieldIdx = 2
	case "%arr":
		dataFieldIdx = 1
	default:
		if g.isUserStructType(elemType) {
			g.emitStructFieldsFree(sb, elemPtr, elemType)
		}
		return
	}
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%df.elem.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, elemType, elemType, elemPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	g.emitNullCheckFree(sb, dataLoad)
}

// freeOldHeapValue 釋放重新賦值變數的舊堆數據。
// 跳過合成 let（IsSynthetic）。
// 輸出參數也釋放舊值（函數結束時由 emitHeapFree 跳過最終值，歸呼叫者）。
// 對已 move 的變數執行雙重校驗：bit=0 仍需 free，bit=1 跳過（所有權已轉移）。
func (g *Generator) freeOldHeapValue(sb *strings.Builder, stmt *parser.LetStatement, name string) {
	if g.heapVars == nil || stmt.IsSynthetic {
		return
	}
	oldType, isHeap := g.heapVars[name]
	if !isHeap {
		return
	}
	elemType := ""
	if g.arrayElemTypes != nil {
		elemType = g.arrayElemTypes[name]
	}
	varIdx, hasIdx := g.heapVarIndex[name]
	if !hasIdx {
		// 無 varIdx，直接 free 舊值
		g.emitVarHeapFree(sb, g.varAddr(name), oldType, elemType, name)
		return
	}
	if g.hasBranchMove && g.movedBitmapBase != "" {
		// 有運行時 bitmap：生成 IR 檢查 bit
		// bit=1 → 所有權已轉移，跳過 free（舊 data 不屬於此變數）
		// bit=0 → 仍擁有數據，free 舊值
		g.emitBitCheckFree(sb, name, varIdx, oldType, elemType)
	} else {
		// 無 bitmap：編譯期檢查 movedVarBitset
		if g.isMovedVar(varIdx) {
			return // 跳過 free（所有權轉移）
		}
		g.emitVarHeapFree(sb, g.varAddr(name), oldType, elemType, name)
	}
}

// emitStructFieldsFree 遞迴釋放用戶結構體中所有含堆數據的欄位。
// 對 %vec/%str-long/%arr 欄位，呼叫 emitStructFieldDataFree 釋放其 data；
// 對嵌套用戶結構體欄位，GEP 取得欄位指標後遞迴呼叫本函數。
// 純量欄位（i64/i8/...）不持有堆數據，直接跳過。
func (g *Generator) emitStructFieldsFree(sb *strings.Builder, structPtr, structType string) {
	structName := strings.TrimPrefix(structType, "%")
	fields, ok := g.structTypes[structName]
	if !ok {
		return
	}
	for i, f := range fields {
		switch f.typ {
		case "%vec", "%str-long":
			g.emitStructFieldFree(sb, structPtr, structType, i, 2, f.typ, f.elemType)
		case "%arr":
			g.emitStructFieldFree(sb, structPtr, structType, i, 1, f.typ, f.elemType)
		default:
			if g.isUserStructType(f.typ) {
				g.tmpIdx++
				fieldGEP := fmt.Sprintf("%%structfield.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structType, structType, structPtr, i))
				g.emitStructFieldsFree(sb, fieldGEP, f.typ)
				continue
			}
			// 內聯固定陣列字段 [N x T]（如 hashmap 的 keys [256]str）：
			// 遍歷 N 個元素遞迴釋放其堆數據。純量元素（i64/i8/...）無需釋放。
			if n, elemType, ok := parseInlineArrayType(f.typ); ok && g.isHeapOwningType(elemType) {
				g.emitInlineArrayFieldFree(sb, structPtr, structType, i, n, elemType, f.typ)
			}
		}
	}
}

// parseInlineArrayType 解析 LLVM 內聯固定陣列類型字串 "[N x T]"。
// 返回 (N, elemType, true)；非內聯陣列或解析失敗返回 (0, "", false)。
// 例："[256 x %str-long]" → (256, "%str-long", true)。
func parseInlineArrayType(s string) (int64, string, bool) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return 0, "", false
	}
	inner := s[1 : len(s)-1] // "256 x %str-long"
	parts := strings.SplitN(inner, " x ", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	var n int64
	if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
		return 0, "", false
	}
	elem := strings.TrimSpace(parts[1])
	if n <= 0 || elem == "" {
		return 0, "", false
	}
	return n, elem, true
}

// emitInlineArrayFieldFree 釋放結構體內聯固定陣列字段 [N x T] 中每個元素的堆數據。
// 用於 hashmap 等含 [N]str 鍵字段的場景：遍歷 N 個 slot，對每個非空元素遞迴釋放。
// fieldIdx 為字段在結構體中的索引，arrayType 為字段的 LLVM 類型字串（如 "[256 x %str-long]"）。
func (g *Generator) emitInlineArrayFieldFree(sb *strings.Builder, structPtr, structType string, fieldIdx int, n int64, elemType, arrayType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	fieldGEP := fmt.Sprintf("%%inlarr.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), fieldGEP, structType, structType, structPtr, fieldIdx))
	// 循環計數器
	iPtr := fmt.Sprintf("%%inlarr.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	condLabel := fmt.Sprintf("inlarr.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, condLabel)
	iVal := fmt.Sprintf("%%inlarr.iv.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	cmp := fmt.Sprintf("%%inlarr.cmp.%d", tid)
	bodyLabel := fmt.Sprintf("inlarr.body.%d", tid)
	endLabel := fmt.Sprintf("inlarr.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmp, iVal, n))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), cmp, bodyLabel, endLabel))
	g.emitLabel(sb, bodyLabel)
	// GEP 第 i 個元素：結果類型為 elemType*，可直接傳給 emitElementFree
	// 注意：[N x T]* 的 GEP 需要兩個索引 (0, i)：
	//   - 第一個索引 0：不穿過 [N x T] 陣列本身（步長 = sizeof([N x T])）
	//   - 第二個索引 i：存取陣列內部第 i 個元素（步長 = sizeof(T)）
	// 若只用一個索引 i，步長會變成 sizeof([N x T])，導致越界存取。
	elemGEP := fmt.Sprintf("%%inlarr.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), elemGEP, arrayType, arrayType, fieldGEP, iVal))
	// 對 %str-long 元素加 len==0 跳過檢查：
	// hashmap 等 std 容器的 init 只清零 len 未清零 data，未使用 slot 的 data 為垃圾值。
	// len==0 表示該 slot 未持有堆數據，跳過 free 避免 free 垃圾指標。
	// 對 %vec/%arr 同理（len=0 表示無元素）。三者 len 均為 field 0。
	if elemType == "%str-long" || elemType == "%vec" || elemType == "%arr" {
		lenGEP := fmt.Sprintf("%%inlarr.len.gep.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, elemType, elemType, elemGEP))
		lenLoad := fmt.Sprintf("%%inlarr.len.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
		lenCmp := fmt.Sprintf("%%inlarr.lencmp.%d", tid)
		skipLabel := fmt.Sprintf("inlarr.skip.%d", tid)
		freeLabel := fmt.Sprintf("inlarr.free.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenCmp, lenLoad))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), lenCmp, skipLabel, freeLabel))
		g.emitLabel(sb, freeLabel)
		g.emitElementFree(sb, elemGEP, elemType)
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		g.emitLabel(sb, skipLabel)
	} else {
		g.emitElementFree(sb, elemGEP, elemType)
	}
	next := fmt.Sprintf("%%inlarr.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), next, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), next, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, endLabel)
}

// emitStructFieldFree frees heap data of struct field (deep free for vec/arr with heap-owning elements).
func (g *Generator) emitStructFieldFree(sb *strings.Builder, structPtr, structType string, fieldIdx, dataFieldIdx int, fieldType, fieldElemType string) {
	g.tmpIdx++
	fieldGEP := fmt.Sprintf("%%structfield.fgep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), fieldGEP, structType, structType, structPtr, fieldIdx))
	if fieldType == "%str-long" {
		g.emitShallowDataFree(sb, fieldGEP, fieldType, dataFieldIdx)
		return
	}
	if g.isHeapOwningType(fieldElemType) {
		g.emitDeepContainerFree(sb, fieldGEP, fieldType, dataFieldIdx, fieldElemType)
		return
	}
	g.emitShallowDataFree(sb, fieldGEP, fieldType, dataFieldIdx)
}

// canDeepCloneStruct 遞迴檢查用戶結構體是否可以安全深層 clone。
// 若任何 %vec/%arr 欄位的 elemType 是 %vec/%arr（巢狀容器），則無法安全 clone
// （子元素型別未知，無法正確計算 memcpy 大小）。
func (g *Generator) canDeepCloneStruct(structType string) bool {
	structName := strings.TrimPrefix(structType, "%")
	fields, ok := g.structTypes[structName]
	if !ok {
		return true
	}
	for _, f := range fields {
		if f.typ == "%vec" || f.typ == "%arr" {
			if f.elemType == "%vec" || f.elemType == "%arr" {
				return false
			}
		}
		if g.isUserStructType(f.typ) {
			if !g.canDeepCloneStruct(f.typ) {
				return false
			}
		}
	}
	return true
}

// emitDeepClone 生成深層 clone 代碼：從 srcPtr 深層複製到 dstPtr。
// 對於 %vec/%arr：malloc 新 data 緩衝區，memcpy，遞迴 clone 元素。
// 對於 %str-long：malloc 新 data 緩衝區，memcpy（元素為 i8，無需遞迴）。
// 對於用戶結構體：memcpy 整個結構體，遞迴 clone 含堆數據的欄位。
func (g *Generator) emitDeepClone(sb *strings.Builder, srcPtr, dstPtr, llvmType, elemType string) {
	switch llvmType {
	case "%vec":
		g.emitContainerClone(sb, srcPtr, dstPtr, "%vec", 2, elemType)
	case "%arr":
		g.emitContainerClone(sb, srcPtr, dstPtr, "%arr", 1, elemType)
	case "%str-long":
		g.emitContainerClone(sb, srcPtr, dstPtr, "%str-long", 2, "i8")
	default:
		if g.isUserStructType(llvmType) {
			g.emitStructClone(sb, srcPtr, dstPtr, llvmType)
		}
	}
}

// emitContainerClone 深層 clone %vec/%arr/%str-long：
// 先 store zeros 到 dst（處理 NULL 源資料），再 malloc+memcpy+遞迴 clone 元素。
func (g *Generator) emitContainerClone(sb *strings.Builder, srcPtr, dstPtr, containerType string, dataFieldIdx int, elemType string) {
	g.tmpIdx++
	tid := g.tmpIdx

	// 先 store zeros 到 dst（處理源 data 為 NULL 的情況）
	dstLenGEP := fmt.Sprintf("%%clone.dst.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), dstLenGEP, containerType, containerType, dstPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstLenGEP))
	if containerType == "%vec" || containerType == "%str-long" {
		dstCapGEP := fmt.Sprintf("%%clone.dst.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), dstCapGEP, containerType, containerType, dstPtr))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstCapGEP))
	}
	// 運行時 move 標記改用函數級位圖變數，結構體內無 is_moved 欄位，深拷貝無需重置。
	dstDataGEP := fmt.Sprintf("%%clone.dst.data.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstDataGEP, containerType, containerType, dstPtr, dataFieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstDataGEP))

	// load source len
	srcLenGEP := fmt.Sprintf("%%clone.src.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), srcLenGEP, containerType, containerType, srcPtr))
	srcLenReg := fmt.Sprintf("%%clone.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcLenReg, srcLenGEP))

	// load source cap（%vec/%str-long 有 cap 欄位；%arr 用 len 作為 cap）
	var srcCapReg string
	if containerType == "%vec" || containerType == "%str-long" {
		srcCapGEP := fmt.Sprintf("%%clone.src.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), srcCapGEP, containerType, containerType, srcPtr))
		srcCapReg = fmt.Sprintf("%%clone.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcCapReg, srcCapGEP))
	} else {
		srcCapReg = srcLenReg
	}

	// load source data
	srcDataGEP := fmt.Sprintf("%%clone.src.data.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcDataGEP, containerType, containerType, srcPtr, dataFieldIdx))
	srcDataLoad := g.loadDataPtrField(sb, srcDataGEP)

	// NULL check
	nullCmp := fmt.Sprintf("%%clone.null.%d", tid)
	skipLabel := fmt.Sprintf("clone.skip.%d", tid)
	doLabel := fmt.Sprintf("clone.do.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, srcDataLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, doLabel))

	// clone.do: malloc + memcpy + deep clone elements + overwrite dst
	g.emitLabel(sb, doLabel)
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}
	bufSizeReg := fmt.Sprintf("%%clone.bufsize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), bufSizeReg, srcCapReg, elemSize))
	cloneBuf := fmt.Sprintf("%%clone.buf.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), cloneBuf, bufSizeReg))
	copySizeReg := fmt.Sprintf("%%clone.copysize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), copySizeReg, srcLenReg, elemSize))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), cloneBuf, srcDataLoad, copySizeReg))

	// 若元素是堆擁有型別，遞迴 clone 每個元素的 data
	if g.isHeapOwningType(elemType) {
		g.emitDeepElementClone(sb, cloneBuf, srcDataLoad, srcLenReg, elemType)
	}

	// overwrite dst with actual values
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcLenReg, dstLenGEP))
	if containerType == "%vec" || containerType == "%str-long" {
		dstCapGEP := fmt.Sprintf("%%clone.dst.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcCapReg, dstCapGEP))
	}
	g.storeDataPtrField(sb, cloneBuf, dstDataGEP)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))

	// clone.skip: dst 已有 zeros，無需操作
	g.emitLabel(sb, skipLabel)
}

// emitDeepElementClone 遍歷容器元素，clone 每個元素的堆 data。
// 用於 %str-long 元素（clone 每個字串的 data）和用戶結構體元素（遞迴 clone 欄位）。
// %vec/%arr 元素不應到達此處（canClone 檢查已排除巢狀容器）。
func (g *Generator) emitDeepElementClone(sb *strings.Builder, dstBuf, srcBuf, lenReg, elemType string) {
	// 用戶結構體元素：遞迴 clone 每個元素的欄位
	if g.isUserStructType(elemType) {
		g.emitStructElementsClone(sb, dstBuf, srcBuf, lenReg, elemType)
		return
	}

	// %str-long 元素：clone 每個字串的 data 緩衝區
	var dataFieldIdx int
	switch elemType {
	case "%vec", "%str-long":
		dataFieldIdx = 2
	case "%arr":
		dataFieldIdx = 1
	default:
		return // 純量元素，已由 memcpy 複製
	}

	g.tmpIdx++
	tid := g.tmpIdx
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}

	// 迴圈: for i = 0; i < len; i++
	iPtr := fmt.Sprintf("%%clonec.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("clonec.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopCondLabel)
	iVal := fmt.Sprintf("%%clonec.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%clonec.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("clonec.body.%d", tid)
	loopEndLabel := fmt.Sprintf("clonec.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.emitLabel(sb, loopBodyLabel)

	// 取得 src 和 dst 元素指標
	srcElemArr := fmt.Sprintf("%%clonec.srcarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), srcElemArr, srcBuf, elemType))
	srcElemGEP := fmt.Sprintf("%%clonec.srcgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), srcElemGEP, elemType, elemType, srcElemArr, iVal))
	dstElemArr := fmt.Sprintf("%%clonec.dstarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dstElemArr, dstBuf, elemType))
	dstElemGEP := fmt.Sprintf("%%clonec.dstgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), dstElemGEP, elemType, elemType, dstElemArr, iVal))

	// load src 元素的 data
	srcElemDataGEP := fmt.Sprintf("%%clonec.srcdata.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcElemDataGEP, elemType, elemType, srcElemGEP, dataFieldIdx))
	srcElemData := g.loadDataPtrField(sb, srcElemDataGEP)

	// NULL check
	elemNullCmp := fmt.Sprintf("%%clonec.null.%d", tid)
	elemSkipLabel := fmt.Sprintf("clonec.skip.%d", tid)
	elemDoLabel := fmt.Sprintf("clonec.do.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), elemNullCmp, srcElemData))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), elemNullCmp, elemSkipLabel, elemDoLabel))

	// clonec.do: malloc + memcpy + store to dst
	g.emitLabel(sb, elemDoLabel)
	// load src 元素的 len（用於 memcpy 大小）
	srcElemLenGEP := fmt.Sprintf("%%clonec.srclen.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), srcElemLenGEP, elemType, elemType, srcElemGEP))
	srcElemLen := fmt.Sprintf("%%clonec.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcElemLen, srcElemLenGEP))
	// load src 元素的 cap（用於 malloc 大小）
	var srcElemCap string
	if elemType == "%vec" || elemType == "%str-long" {
		srcElemCapGEP := fmt.Sprintf("%%clonec.srccap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), srcElemCapGEP, elemType, elemType, srcElemGEP))
		srcElemCap = fmt.Sprintf("%%clonec.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcElemCap, srcElemCapGEP))
	} else {
		srcElemCap = srcElemLen
	}
	// 子元素大小（%str-long=1 byte；其他容器不應到達此處）
	subElemSize := int64(1)
	if elemType == "%vec" || elemType == "%arr" {
		subElemSize = 8 // fallback（不應到達此處，canClone 已排除）
	}
	elemBufSize := fmt.Sprintf("%%clonec.bufsize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), elemBufSize, srcElemCap, subElemSize))
	elemCloneBuf := fmt.Sprintf("%%clonec.buf.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), elemCloneBuf, elemBufSize))
	elemCopySize := fmt.Sprintf("%%clonec.copysize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), elemCopySize, srcElemLen, subElemSize))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), elemCloneBuf, srcElemData, elemCopySize))
	// store new data to dst 元素
	dstElemDataGEP := fmt.Sprintf("%%clonec.dstdata.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstElemDataGEP, elemType, elemType, dstElemGEP, dataFieldIdx))
	g.storeDataPtrField(sb, elemCloneBuf, dstElemDataGEP)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), elemSkipLabel))

	// clonec.skip: 繼續下一個元素
	g.emitLabel(sb, elemSkipLabel)
	iNext := fmt.Sprintf("%%clonec.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopEndLabel)
}

// emitStructElementsClone 遍歷用戶結構體元素陣列，遞迴 clone 每個元素的欄位。
func (g *Generator) emitStructElementsClone(sb *strings.Builder, dstBuf, srcBuf, lenReg, structType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	elemSize := g.llvmTypeSize(structType)
	if elemSize == 0 {
		elemSize = 8
	}
	iPtr := fmt.Sprintf("%%clones.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("clones.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopCondLabel)
	iVal := fmt.Sprintf("%%clones.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%clones.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("clones.body.%d", tid)
	loopEndLabel := fmt.Sprintf("clones.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.emitLabel(sb, loopBodyLabel)
	srcElemArr := fmt.Sprintf("%%clones.srcarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), srcElemArr, srcBuf, structType))
	srcElemGEP := fmt.Sprintf("%%clones.srcgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), srcElemGEP, structType, structType, srcElemArr, iVal))
	dstElemArr := fmt.Sprintf("%%clones.dstarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dstElemArr, dstBuf, structType))
	dstElemGEP := fmt.Sprintf("%%clones.dstgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), dstElemGEP, structType, structType, dstElemArr, iVal))
	// 遞迴 clone 結構體欄位
	g.emitStructClone(sb, srcElemGEP, dstElemGEP, structType)
	iNext := fmt.Sprintf("%%clones.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopEndLabel)
}

// emitStructClone 深層 clone 用戶結構體：先 memcpy 整個結構體，再遞迴 clone 含堆數據的欄位。
func (g *Generator) emitStructClone(sb *strings.Builder, srcPtr, dstPtr, structType string) {
	// 先 memcpy 整個結構體（複製所有欄位，包括 data 指標）
	structSize := g.llvmTypeSize(structType)
	if structSize == 0 {
		structSize = 8
	}
	g.tmpIdx++
	srcI8 := fmt.Sprintf("%%structclone.src.i8.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), srcI8, structType, srcPtr))
	g.tmpIdx++
	dstI8 := fmt.Sprintf("%%structclone.dst.i8.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), dstI8, structType, dstPtr))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n",
		g.indent(), dstI8, srcI8, structSize))

	// 遞迴 clone 含堆數據的欄位（覆寫 data 指標為獨立的 clone）
	structName := strings.TrimPrefix(structType, "%")
	fields, ok := g.structTypes[structName]
	if !ok {
		return
	}
	for i, f := range fields {
		switch f.typ {
		case "%vec", "%str-long":
			g.emitStructFieldClone(sb, srcPtr, dstPtr, structType, i, 2, f.typ, f.elemType)
		case "%arr":
			g.emitStructFieldClone(sb, srcPtr, dstPtr, structType, i, 1, f.typ, f.elemType)
		default:
			if g.isUserStructType(f.typ) {
				g.tmpIdx++
				srcFieldGEP := fmt.Sprintf("%%structclone.src.fgep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), srcFieldGEP, structType, structType, srcPtr, i))
				g.tmpIdx++
				dstFieldGEP := fmt.Sprintf("%%structclone.dst.fgep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), dstFieldGEP, structType, structType, dstPtr, i))
				g.emitStructClone(sb, srcFieldGEP, dstFieldGEP, f.typ)
				continue
			}
			// 內聯固定陣列字段 [N x T]（如 hashmap 的 keys [256]str）：
			// memcpy 已淺拷貝所有元素的 data 指標，需遍歷 N 個元素遞迴 clone，
			// 覆寫 dst 元素的 data 為獨立 clone，避免 a/b 共享 data 導致 double-free。
			if n, elemType, ok := parseInlineArrayType(f.typ); ok && g.isHeapOwningType(elemType) {
				g.emitInlineArrayFieldClone(sb, srcPtr, dstPtr, structType, i, n, elemType, f.typ)
			}
		}
	}
}

// emitInlineArrayFieldClone 深層 clone 結構體內聯固定陣列字段 [N x T] 的每個元素。
// 用於 hashmap 等含 [N]str 鍵字段的場景：遍歷 N 個 slot，對每個元素遞迴 clone，
// 使 dst 擁有獨立的堆數據，避免與 src 共享 data 指標。
func (g *Generator) emitInlineArrayFieldClone(sb *strings.Builder, srcPtr, dstPtr, structType string, fieldIdx int, n int64, elemType, arrayType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	srcFieldGEP := fmt.Sprintf("%%inlarrc.src.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcFieldGEP, structType, structType, srcPtr, fieldIdx))
	dstFieldGEP := fmt.Sprintf("%%inlarrc.dst.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstFieldGEP, structType, structType, dstPtr, fieldIdx))
	// 循環計數器
	iPtr := fmt.Sprintf("%%inlarrc.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	condLabel := fmt.Sprintf("inlarrc.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, condLabel)
	iVal := fmt.Sprintf("%%inlarrc.iv.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	cmp := fmt.Sprintf("%%inlarrc.cmp.%d", tid)
	bodyLabel := fmt.Sprintf("inlarrc.body.%d", tid)
	endLabel := fmt.Sprintf("inlarrc.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmp, iVal, n))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), cmp, bodyLabel, endLabel))
	g.emitLabel(sb, bodyLabel)
	// GEP src/dst 第 i 個元素
	// 注意：[N x T]* 的 GEP 需要兩個索引 (0, i)，詳見 emitInlineArrayFieldFree 的註解。
	srcElemGEP := fmt.Sprintf("%%inlarrc.src.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), srcElemGEP, arrayType, arrayType, srcFieldGEP, iVal))
	dstElemGEP := fmt.Sprintf("%%inlarrc.dst.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), dstElemGEP, arrayType, arrayType, dstFieldGEP, iVal))
	// 遞迴 clone 元素（%str-long → emitContainerClone；用戶結構體 → emitStructClone）
	g.emitDeepClone(sb, srcElemGEP, dstElemGEP, elemType, "")
	next := fmt.Sprintf("%%inlarrc.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), next, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), next, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, endLabel)
}

// emitStructFieldClone clone 結構體欄位的堆 data。
func (g *Generator) emitStructFieldClone(sb *strings.Builder, srcPtr, dstPtr, structType string, fieldIdx, dataFieldIdx int, fieldType, fieldElemType string) {
	g.tmpIdx++
	srcFieldGEP := fmt.Sprintf("%%structclone.src.flg.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcFieldGEP, structType, structType, srcPtr, fieldIdx))
	g.tmpIdx++
	dstFieldGEP := fmt.Sprintf("%%structclone.dst.flg.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstFieldGEP, structType, structType, dstPtr, fieldIdx))
	if fieldType == "%str-long" {
		g.emitContainerClone(sb, srcFieldGEP, dstFieldGEP, "%str-long", 2, "i8")
		return
	}
	g.emitContainerClone(sb, srcFieldGEP, dstFieldGEP, fieldType, dataFieldIdx, fieldElemType)
}

// initVecFieldFromSliceLiteral 在 struct literal 中將 SliceLiteral 初始化到 %vec 欄位。
// 使用 malloc 分配堆內存（而非 alloca），確保 struct 通過 out 參數返回後 data 仍有效。
func (g *Generator) initVecFieldFromSliceLiteral(sb *strings.Builder, structVar, structTy string, fieldIdx int, slice *parser.SliceLiteral, fieldElemType string) {
	elemType := fieldElemType
	if elemType == "" {
		elemType = "i64"
	}
	n := int64(len(slice.Elements))
	g.tmpIdx++
	tid := g.tmpIdx
	tmpArr := fmt.Sprintf("%%st.slice.tmp.%d", tid)
	arrType := fmt.Sprintf("[%d x %s]", n, elemType)
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}
	bufSize := n * elemSize

	// malloc 分配堆內存（避免函數返回後棧幀銷毀導致懸垂指針）
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), tmpArr, bufSize))
	// bitcast to element array pointer for GEP store
	g.tmpIdx++
	arrPtrReg := fmt.Sprintf("%%st.slice.arrptr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), arrPtrReg, tmpArr, arrType))

	// 逐元素 store
	for i, elem := range slice.Elements {
		ev := g.generateExprWithSB(sb, elem)
		ev = g.stripLLVMType(ev)
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%st.slice.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
			g.indent(), gepReg, arrType, arrType, arrPtrReg, i))
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
	}

	// data 指針即 malloc 返回的 i8*
	ptrReg := tmpArr

	// store len（欄位的 field 0）
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%st.slice.len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 0\n",
		g.indent(), lenGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

	// store cap（欄位的 field 1）
	g.tmpIdx++
	capGEP := fmt.Sprintf("%%st.slice.cap.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
		g.indent(), capGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

	// store data（欄位的 field 2）
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%st.slice.data.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
		g.indent(), dataGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	g.storeDataPtrField(sb, ptrReg, dataGEP)
}

// resolveParamLLVMType 計算參數的 LLVM 型別字串。
// 當參數型別為具名型別且對應至具名函式型別別名時，解析為函式指標型別 void (...)*。
func (g *Generator) resolveParamLLVMType(t parser.Type) string {
	if nt, ok := t.(*parser.NamedType); ok && g.fnTypeAliases != nil {
		if ft, ok := g.fnTypeAliases[nt.Value]; ok {
			return g.mapToLLVMType(ft.String())
		}
	}
	// MapType: String() returns [K]V which is ambiguous with [N]T arrays in
	// string-based dispatch; use LLVMName() to get the hashmap-K-V struct name.
	if mt, ok := t.(*parser.MapType); ok {
		return g.mapToLLVMType(mt.LLVMName())
	}
	// Fixed-size ArrayType (e.g. [32]byte) → raw LLVM array [32 x i8] to match
	// struct field type (collectStructTypeFields uses the same raw array form).
	// Without this, [32]byte params would be typed as %arr (struct with len/data
	// pointer), which is incompatible with [32 x i8] struct fields.
	if at, ok := t.(*parser.ArrayType); ok && at.Size != nil {
		return g.arrayTypeToLLVM(at)
	}
	// SliceType (e.g. []str, []byte) → %vec (built-in struct: vec { len, cap, data }).
	// Without this, String() returns "[]str" which mapToLLVMType would route through
	// its "[" prefix branch and correctly return "%vec" — but only when called
	// directly. The dedicated handling here makes the intent explicit and ensures
	// funcResultLLVMType registers "%vec" for slice-returning functions (e.g.
	// list-dir returns []str), so downstream varLLVMType can infer %vec for
	// `entries = fs.list-dir(dir)` instead of defaulting to i64.
	if st, ok := t.(*parser.SliceType); ok {
		_ = st
		return "%vec"
	}
	return g.mapToLLVMType(t.String())
}

func (g *Generator) generateFunctionDefinition(sb *strings.Builder, fd *parser.FunctionDefinition) {
	g.funcVars = nil
	g.curFuncName = fd.Name
	g.inMainFunction = false
	g.varTypes = make(map[string]string) // reset varTypes for each function
	g.funcLocalNames = make(map[string]bool)
	g.funcParams = make(map[string]bool)
	g.optionInnerTypes = make(map[string]string)              // reset option inner types for each function
	g.ssaTypes = make(map[string]string)                      // reset SSA type tracking for each function
	g.varFnTypes = make(map[string]*parser.FunctionType)      // reset function-type params for each function
	g.arraySizes = make(map[string]int64)                     // reset array size tracking for each function
	g.sliceViews = make(map[string]*sliceViewInfo)            // reset slice view tracking for each function
	g.outputParamNames = make(map[string]bool)                // reset output param tracking
	g.outputParamOrder = nil                                  // reset output param order (for outBindState index)
	g.hasBranchMove = false                                   // reset branch move flag (set by pre-scan)
	g.heapVarIndex = make(map[string]int)                     // reset heap var index (varIdx assignment)
	g.nextHeapVarIdx = 0                                      // reset next heap var idx counter
	g.outBindState = nil                                      // reset out param bind state (allocated after outputParamOrder known)
	g.movedVarBitset = nil                                    // reset compile-time moved bitset
	g.movedBitmapBase = ""                                    // reset runtime bitmap var prefix
	g.bitmapCount = 0                                         // reset bitmap block count
	g.outputBindings = make(map[string]map[int]outputBinding) // reset delayed move bindings (SSA versioned)
	g.ssaVersion = make(map[string]int)                       // reset SSA version counters
	g.heapVars = make(map[string]string)                      // reset heap var tracking
	g.stackArrVars = make(map[string]bool)                    // reset stack-allocated array tracking
	g.varAlias = make(map[string]string)                      // reset var alias tracking (用於 %arr → %vec 重定向)
	g.taskResultTypes = make(map[string]string)               // reset task result types for each function
	g.futureResultTypes = make(map[string]string)             // reset future result types for each function
	// 重置 arrayElemTypes 並恢復模組級元素型別，避免函數參數（如 rsa.no bn-add 的 c []i64）
	// 覆蓋模組級同名變數（如 main.no 的 c []str）導致後續 push 索引型別錯誤
	g.arrayElemTypes = make(map[string]string)
	for k, v := range g.moduleArrayElemTypes {
		g.arrayElemTypes[k] = v
	}
	// 恢復模組級變數的型別資訊
	for k, v := range g.moduleVarTypes {
		g.varTypes[k] = v
	}
	if g.paramNames == nil {
		g.paramNames = make(map[string]bool)
	}
	// 无栈协程：检测含 awy 的函数，变换为状态机。
	// 含 awy 的函数不再生成原始函数体，而是生成 coro_resume.N 状态机函数。
	if fd.Body != nil && len(fd.Body.Statements) > 0 {
		if len(collectTopLevelAwaitIndices(fd.Body.Statements)) > 0 {
			g.transformAsyncFunction(sb, fd)
			return
		}
	}
	for _, p := range fd.Parameters {
		g.paramNames[p.Name] = true
		g.funcLocalNames[p.Name] = true
		g.funcParams[p.Name] = true
		g.varTypes[p.Name] = g.resolveParamLLVMType(p.Type)
		// Track FunctionType parameters for indirect call codegen
		if ft, ok := p.Type.(*parser.FunctionType); ok {
			g.varFnTypes[p.Name] = ft
		}
		// 參數型別為具名型別且對應至具名函式型別別名時，亦登錄為間接呼叫目標
		if nt, ok := p.Type.(*parser.NamedType); ok && g.fnTypeAliases != nil {
			if ft, ok := g.fnTypeAliases[nt.Value]; ok {
				g.varFnTypes[p.Name] = ft
			}
		}
		// 陣列型輸入參數需註冊元素型別，供後續索引賦值/讀取使用
		if at, ok := p.Type.(*parser.ArrayType); ok && at.Elem != nil {
			g.arrayElemTypes[p.Name] = g.mapToLLVMType(at.Elem.String())
		}
		// 切片型輸入參數也需註冊元素型別，供 IndexExpression 使用正確型別
		if st, ok := p.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.arrayElemTypes[p.Name] = g.mapToLLVMType(st.Elem.String())
		}
	}
	// 結果參數（無論單結果或多結果）皆以 by-reference 形式傳遞，
	// 與 call.go 的 hasOutputParam / voidSingleOutput 約定保持一致。
	for _, r := range fd.Results {
		if r.Name != "" {
			g.paramNames[r.Name] = true
			g.funcLocalNames[r.Name] = true
			g.funcParams[r.Name] = true
			g.outputParamNames[r.Name] = true
			g.outputParamOrder = append(g.outputParamOrder, r.Name)
			// MapType: use LLVMName() to avoid [K]V matching the array branch.
			var typeStr string
			if mt, ok := r.Type.(*parser.MapType); ok {
				typeStr = mt.LLVMName()
			} else {
				typeStr = r.Type.String()
			}
			g.varTypes[r.Name] = g.resolveParamLLVMType(r.Type)
			if strings.HasPrefix(typeStr, "?") {
				g.optionInnerTypes[r.Name] = g.mapToLLVMType(typeStr[1:])
			}
			// 陣列型結果參數需註冊元素型別，供後續索引賦值/讀取使用
			if at, ok := r.Type.(*parser.ArrayType); ok && at.Elem != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(at.Elem.String())
			}
			// 切片型結果參數也需註冊元素型別，供 IndexExpression 使用正確型別
			if st, ok := r.Type.(*parser.SliceType); ok && st.Elem != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(st.Elem.String())
			}
			// out 參數加入 heapVars（用於 freeOldHeapValue 釋放賦值時的舊值）。
			// 不加入 heapVarIndex（不參與 move bitmap），函數結束 emitHeapFree 仍跳過 out。
			if llvmType := g.varTypes[r.Name]; g.isHeapOwningType(llvmType) || llvmType == "%option" {
				if g.heapVars == nil {
					g.heapVars = make(map[string]string)
				}
				g.heapVars[r.Name] = llvmType
			}
		}
	}

	returnType := "void"
	// All Nolang functions with results use void + pointer-passing convention:
	// multi-result: each result passed by reference as an additional parameter
	// single-result: also passed by reference (for consistency with multi-result
	// and to support struct types like %option that can't be returned by value
	// in all contexts)
	if len(fd.Results) == 1 {
		_ = g.mapToLLVMType(fd.Results[0].Type.String()) // result type used in pointer param
	}

	g.curFuncRetType = returnType
	g.curFuncRetName = ""
	if len(fd.Results) == 1 && fd.Results[0].Name != "" {
		g.curFuncRetName = fd.Results[0].Name
	}

	// Rename user-defined main to _nolang_main to avoid conflict with the
	// C entry point wrapper main(i32, i8**) generated by generateMainFunction.
	// 為避免與 clib 系統調用（@open、@read、@write、@close、@mkdir、@unlink、
	// @rename、@stat、@chdir 等）衝突，僅在用戶函數名稱命中 clibFuncNames 時
	// 加 "n." 前綴；其他情況保留原名以維持與 builtin 的 dispatch 優先級。
	llvmFnName := fd.Name
	if clibFuncNames[fd.Name] {
		llvmFnName = "n." + fd.Name
	}
	if fd.Name == "main" {
		llvmFnName = "_nolang_main"
	}

	// Use internal linkage + alwaysinline for user-defined functions so LLVM
	// can inline them at -O2/-O3. This is critical for performance: without
	// inlining, every function call has overhead and prevents cross-function
	// optimizations (e.g., constant propagation, DCE).
	// Skip for main (renamed to _nolang_main, called from C wrapper).
	isMain := fd.Name == "main"
	linkage := "internal"
	if isMain {
		linkage = ""
	}
	if linkage != "" {
		sb.WriteString(fmt.Sprintf("define %s %s @%s(", linkage, returnType, sanitizeLLVMName(llvmFnName)))
	} else {
		sb.WriteString(fmt.Sprintf("define %s @%s(", returnType, sanitizeLLVMName(llvmFnName)))
	}

	for i, param := range fd.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		// 引用傳遞：參數為指標 i64* %n（函式型別參數為 void (...)** %n）
		llvmType := g.resolveParamLLVMType(param.Type) + "*"
		sb.WriteString(fmt.Sprintf("%s %s", llvmType, llvmVarRef(param.Name)))
	}
	// 結果參數（單結果或多結果）以指標形式附加到參數列表
	firstResult := true
	for _, r := range fd.Results {
		if r.Name != "" {
			llvmType := g.resolveParamLLVMType(r.Type) + "*"
			sep := ", "
			if firstResult && len(fd.Parameters) == 0 {
				sep = "" // 第一個參數前不需逗號
			}
			sb.WriteString(fmt.Sprintf("%s%s %s", sep, llvmType, llvmVarRef(r.Name)))
			firstResult = false
		}
	}

	sb.WriteString(") {\n")
	g.indentLevel++
	g.emitLabel(sb, "entry")
	g.indentLevel++

	// 收集所有變數（一次分配），排除參數（已是指標）
	localVarTypes := make(map[string]string)
	// Reset itAllocTypes per function to prevent type leakage from prior functions
	g.itAllocTypes = make(map[string]string)
	g.collectVarDeclsFromStmt(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
		g.funcLocalNames[k] = true
	}
	// 確保 range 變數有型別
	g.collectRangeVarTypes(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
		g.funcLocalNames[k] = true
	}
	// 結果參數為 passed by reference（單結果與多結果皆同），不分配本地 alloca。
	for _, r := range fd.Results {
		if r.Name != "" {
			g.varTypes[r.Name] = g.resolveParamLLVMType(r.Type)
			g.funcLocalNames[r.Name] = true
			// Register slice element type for []T result params (e.g. []str → "%str-long").
			// Without this, vec-push defaults to i64 (8 bytes) instead of %str-long (24 bytes),
			// causing heap corruption and segfaults in functions like str.split that push to []str results.
			if st, ok := r.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(st.Elem.String())
			}
		}
	}

	for _, p := range fd.Parameters {
		delete(localVarTypes, p.Name)
	}
	// 結果參數：均為 by-reference 形式（單結果與多結果），不應作為本地變量分配。
	for _, r := range fd.Results {
		delete(localVarTypes, r.Name)
	}

	// 分配 + lifetime.start
	for varName, varType := range localVarTypes {
		sz := g.llvmTypeSize(varType)
		g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: varType, Size: sz})
		// Record allocated type for synthetic `it` variables so that
		// bitcasts can be generated when the actual type differs (e.g.
		// allocated as %http2-frame but storing/loading i64).
		if varName == "it" && g.itAllocTypes != nil {
			g.itAllocTypes[varName] = varType
		}
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(varName), varType))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), sz, llvmVarRef(varName)))
		// %str-long 局部變數零初始化：確保 data 指標為 NULL。
		// 若變數在循環/條件塊內賦值但運行時未執行，emitHeapFree 的 NULL 檢查能安全跳過 free。
		if varType == "%str-long" {
			sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), llvmVarRef(varName)))
		}
		// %vec (slice) 局部變數需要 malloc 資料緩衝區，否則 buf[i] = val 會因 data 為 null 而崩潰。
		// 使用 malloc（而非 alloca）使得資料在函數返回後仍然有效（例如函數輸出 []byte 給呼叫者）。
		if varType == "%vec" {
			vecCap := int64(256)
			elemSize := int64(8) // default i64
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[varName]; ok {
					elemSize = llvmTypeSize(et)
				}
			}
			vecBufSize := vecCap * elemSize
			g.tmpIdx++
			dataBuf := fmt.Sprintf("%%local.vecdata.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%local.veclen.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%local.veccap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%local.vecdata.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, llvmVarRef(varName)))
			g.storeDataPtrField(sb, dataBuf, dataGEP)
			// 追蹤 prologue buffer 為堆變數，使首次賦值（如 v = [1,2,3]）時
			// freeOldHeapValue 能釋放 prologue buffer，避免每次函數調用洩漏 256*elemSize 字節。
			// 若變數從未被賦值，emitHeapFree 會在函數結束時釋放 prologue buffer。
			// trackLocalHeapVar 跳過參數和輸出參數，所以這裡只追蹤真正的局部變數。
			g.trackLocalHeapVar(varName, "%vec")
		}
		// [N]T 局部陣列需分配資料緩衝區並初始化 len/data，否則 arr[i] = val
		// 會因 data 為未初始化（stack 殘值）而寫入垃圾地址，造成堆損壞。
		// 小尺寸/定寬元素陣列用 alloca（棧分配），避免 malloc/free 開銷與記憶體洩漏。
		if varType == "%arr" {
			arrSize := int64(0)
			if g.arraySizes != nil {
				if sz, ok := g.arraySizes[varName]; ok {
					arrSize = sz
				}
			}
			elemSize := int64(8)
			llvmElemType := "i64"
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[varName]; ok {
					elemSize = g.llvmTypeSize(et)
					llvmElemType = et
				}
			}
			totalSize := arrSize * elemSize
			if totalSize <= 0 {
				totalSize = 64
			}
			g.tmpIdx++
			arrDataBuf := fmt.Sprintf("%%local.arrdata.%d", g.tmpIdx)
			if shouldStackAllocArray(llvmElemType, totalSize) {
				// 棧分配：entry block 中的 alloca 不會隨迴圈增長棧，且無需 free
				sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), arrDataBuf, totalSize))
				g.stackArrVars[varName] = true
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), arrDataBuf, totalSize))
				// 僅 malloc 路徑需要追蹤；棧分配的 arr 無需 free。
				// 追蹤目的同 %vec：使首次賦值時 freeOldHeapValue 能釋放 prologue buffer。
				g.trackLocalHeapVar(varName, "%arr")
			}
			g.tmpIdx++
			arrLenGEP := fmt.Sprintf("%%local.arrlen.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), arrLenGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arrSize, arrLenGEP))
			g.tmpIdx++
			arrDataGEP := fmt.Sprintf("%%local.arrdata.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), arrDataGEP, llvmVarRef(varName)))
			g.storeDataPtrField(sb, arrDataBuf, arrDataGEP)
		}
	}

	// 輸出參數（單結果或多結果）的緩衝區合法性由調用方負責。
	// 函數 prologue 不再自動 malloc 兜底緩衝區，避免：
	//   1. 越權替調用方處理參數生命週期
	//   2. 覆蓋調用方傳入的有效緩衝區指標（導致洩漏）
	//   3. 單/多輸出參數邏輯割裂
	// 調用方在 call.go voidSingleOutput 路徑中分配緩衝區。

	// 參數化為指標（引用傳遞模型）
	for _, param := range fd.Parameters {
		llvmType := g.mapToLLVMType(param.Type.String())
		// 已是指標型別，不需 alloca/store
		// %n 是 i64*，可直接 load
		_ = llvmType
	}

	// 初始化 outBindState：每個 out 參數初始無綁定（-1）。
	if len(g.outputParamOrder) > 0 {
		g.outBindState = make([]int, len(g.outputParamOrder))
		for i := range g.outBindState {
			g.outBindState[i] = -1
		}
	}

	// 預掃描函數體：檢測是否存在分支內 move-to-out（決定是否需要分配位圖變數）。
	// 僅當 move 存在且在分支中（IfExpression/ForStatement/ConditionalExpression）時，
	// 才需要位圖變數做運行時雙重校驗。
	if fd.Body != nil && len(g.outputParamOrder) > 0 {
		g.hasBranchMove = detectBranchMoveToOut(fd.Body.Statements, g.outputParamNames)
	}

	// 預先設置 bitmap 變數名前綴，使函數體生成期間的 emitSetMovedBitIR/emitClearMovedBitIR
	// 能正確生成 IR。實際的 alloca + store 0 在後面根據最終 nextHeapVarIdx 一次性發出。
	// bitmapCount 在函數體生成後根據實際堆變數數量計算。
	if g.hasBranchMove {
		g.movedBitmapBase = "%__mb"
	}

	// 生成函數體到獨立緩衝區，同時收集 entry-block alloca（來自字面量參數的臨時變量）。
	// 將 alloca 提升到 entry block 可避免循環體內的 call 參數每次迭代都增長棧，
	// 導致長循環（如 n-body 1000000 次 advance()）棧溢出。
	// 生成過程中 trackLocalHeapVar 會分配 varIdx，結束後 nextHeapVarIdx 為最終值。
	g.entryAllocaBuf = &strings.Builder{}
	bodyBuf := &strings.Builder{}
	if fd.Body != nil {
		for _, stmt := range fd.Body.Statements {
			g.generateStatement(bodyBuf, stmt)
		}
	}

	// 分配運行時 bitmap（僅當存在分支內 move 且有局部堆變數時）：
	// bitmap 的每個 bit 對應一個局部堆變數的 varIdx。
	// 一塊 u64 存 64 個標記位；塊號 = varIdx / 64，塊內偏移 = varIdx % 64。
	// 僅當 move 在分支中（無法靜態推斷）時才分配：
	//   - 無 move → 全部 free，不需位圖
	//   - move 不在分支 → move 綁定確定，編譯期 movedVarBitset 跳過 free，不需位圖
	//   - move 在分支 → 需位圖運行時判斷 move 是否實際發生
	if g.hasBranchMove && g.nextHeapVarIdx > 0 {
		g.bitmapCount = (g.nextHeapVarIdx + 63) / 64
		// movedBitmapBase 已在函數體生成前預先設置
		for i := 0; i < g.bitmapCount; i++ {
			sb.WriteString(fmt.Sprintf("%s%s%d = alloca i64\n", g.indent(), g.movedBitmapBase, i))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s%d\n", g.indent(), g.movedBitmapBase, i))
		}
	}

	// 先寫入 entry-block alloca（在所有局部變量 alloca 之後、函數體之前）
	sb.WriteString(g.entryAllocaBuf.String())
	// 再寫入函數體
	sb.WriteString(bodyBuf.String())

	// 若函數無 return 陳述句（void），自動銷毀 + return
	if returnType == "void" {
		g.flushOutputBindings(sb)
		g.emitHeapFree(sb)
		g.emitLifetimeEnd(sb)
		sb.WriteString(g.indent() + "ret void\n")
	} else if len(fd.Results) > 0 && fd.Results[0].Name != "" {
		// 有輸出參數但無顯式 return：載入輸出參數並返回
		g.flushOutputBindings(sb)
		g.emitHeapFree(sb)
		g.emitLifetimeEnd(sb)
		resultName := fd.Results[0].Name
		resultLLVMType := g.mapToLLVMType(fd.Results[0].Type.String())
		g.tmpIdx++
		resultLoad := fmt.Sprintf("%%ret.val.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), resultLoad, resultLLVMType, resultLLVMType, llvmVarRef(resultName)))
		sb.WriteString(fmt.Sprintf("%sret %s %s\n", g.indent(), resultLLVMType, resultLoad))
	}
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

func (g *Generator) generateMainFunction(sb *strings.Builder, program *parser.Program) {
	g.funcVars = nil
	g.funcLocalNames = make(map[string]bool)
	g.funcParams = make(map[string]bool)
	// main returns i32 but has no named result parameter. We keep
	// curFuncRetType = "void" so if-expression PHI nodes default to i64
	// (the standard integer type). The inMainFunction flag lets the
	// ReturnStatement handler emit `ret i32 0` for bare returns in main.
	// Without resetting curFuncRetName, it leaks from the last processed
	// function (e.g. "yes"), causing conditional expressions at module
	// level to emit `load i64, i64* %yes` for a non-existent variable.
	g.inMainFunction = true
	g.curFuncRetName = ""
	g.curFuncRetType = "void"

	// 初始化 main 入口的追蹤映射（與 generateFunctionDefinition 一致），
	// 用於追蹤 top-level 堆變數並在 ret 前釋放。
	g.heapVars = make(map[string]string)
	g.heapVarIndex = make(map[string]int)
	g.nextHeapVarIdx = 0
	g.movedVarBitset = nil
	g.movedBitmapBase = ""
	g.hasBranchMove = false
	g.varAlias = make(map[string]string)
	g.sliceViews = make(map[string]*sliceViewInfo)
	g.outputParamNames = make(map[string]bool)
	g.outputBindings = make(map[string]map[int]outputBinding)
	g.stackArrVars = make(map[string]bool)

	// Restore module-level variable types (reset by generateFunctionDefinition)
	if g.moduleVarTypes != nil {
		g.varTypes = make(map[string]string)
		for k, v := range g.moduleVarTypes {
			g.varTypes[k] = v
		}
	}
	// 同樣恢復模組級陣列/切片元素型別
	if g.moduleArrayElemTypes != nil {
		g.arrayElemTypes = make(map[string]string)
		for k, v := range g.moduleArrayElemTypes {
			g.arrayElemTypes[k] = v
		}
	}
	// 恢復模組級 option 變數的 inner type 備份，
	// 讓 main 函數內 top-level option 變數的 inner type 可被查詢
	// （expr.go / call.go 等多處讀取 optionInnerTypes）。
	if g.moduleOptionInnerTypes != nil {
		g.optionInnerTypes = make(map[string]string)
		for k, v := range g.moduleOptionInnerTypes {
			g.optionInnerTypes[k] = v
		}
	}

	hasTopLevel := false
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *parser.FunctionDefinition, *parser.StructDefinition, *parser.TypeAlias:
			continue
		default:
			hasTopLevel = true
			break
		}
		if hasTopLevel {
			break
		}
	}

	if !hasTopLevel {
		return
	}

	// 模块级代码含顶层 awy 时，包装为匿名 async 函数（无栈协程状态机变换），
	// @main 仅创建 coro_state + task 并启动 @nolang_async_run 事件循环。
	// 此路径下不再生成普通 @main 函数体。
	if g.transformModuleAsync(sb, program) {
		return
	}

	// Check if user-defined main function exists (renamed to _nolang_main)
	hasUserMain := false
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == "main" {
			hasUserMain = true
			break
		}
	}

	// WASI 平台入口函數名必須為 __main_argc_argv：wasi-libc 的 _start 呼叫
	// __main_void（crt1-command.o），後者初始化 argv 後呼叫 weak symbol
	// __main_argc_argv。若用戶 main 仍命名為 @main，__main_argc_argv 為未定義
	// weak symbol，呼叫會跳轉到 unreachable trap。原生平台仍使用 @main。
	mainName := "main"
	if g.goos() == "wasi" {
		mainName = "__main_argc_argv"
	}
	sb.WriteString(fmt.Sprintf("define i32 @%s(i32 %%c-argc, i8** %%c-argv) {\n", mainName))
	g.indentLevel++
	g.emitLabel(sb, "entry")
	g.indentLevel++

	// Store argc/argv into globals for use by args-count / args-get builtins.
	// These globals are declared in decl.go and accessible from any function.
	sb.WriteString(fmt.Sprintf("%sstore i32 %%c-argc, i32* @.argc.addr\n", g.indent()))
	sb.WriteString(fmt.Sprintf("%sstore i8** %%c-argv, i8*** @.argv.addr\n", g.indent()))

	g.emitLifetimeEnd(sb)

	// Pre-allocate all top-level local variables (not globals, not functions)
	// This is needed because CallExpression values use the variable's address
	// as an output parameter before generateLet would allocate it.
	if g.moduleVarTypes != nil {
		// Sort keys for deterministic alloca ordering (Go map iteration is randomized).
		// Non-deterministic alloca order causes LLVM opt -O3 to make different
		// optimization decisions across builds, leading to intermittent issues.
		sortedNames := make([]string, 0, len(g.moduleVarTypes))
		for name := range g.moduleVarTypes {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)
		for _, name := range sortedNames {
			varType := g.moduleVarTypes[name]
			if g.globalVars != nil && g.globalVars[name] {
				continue
			}
			if g.funcRefVars != nil && g.funcRefVars[name] {
				continue
			}
			if varType == "" || strings.HasPrefix(varType, "%") == false && varType != "i64" && varType != "double" && varType != "i1" && varType != "i8" && varType != "i32" {
				// Skip complex types that need special allocation (handled by generateLet)
				continue
			}
			if !g.funcLocalNames[name] {
				g.funcLocalNames[name] = true
				sz := g.llvmTypeSize(varType)
				if sz == 0 {
					sz = 8
				}
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(name), varType))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), sz, llvmVarRef(name)))
			}
		}
	}

	// 生成 top-level 語句到獨立緩衝區，同時收集 entry-block alloca。
	// 與 generateFunctionDefinition 相同的修復：避免循環體內 call 參數的 alloca 每次迭代增長棧。
	g.entryAllocaBuf = &strings.Builder{}
	bodyBuf := &strings.Builder{}
	// Generate top-level statements (e.g. h = crc-32('', 0), test-str-len(), print(0))
	// Skip calls to user-defined main() when hasUserMain is true, since _nolang_main()
	// already calls the user's main. Otherwise we get infinite recursion.
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			// Skip LetStatements already emitted as globals, EXCEPT for:
			// - string/array types (need runtime init via generateLet)
			// - reassigned variables (need store instruction for new value)
			if g.globalVars != nil && g.globalVars[ls.Name.Value] {
				// Reassigned global variables (e.g. h0 in SHA tests) must generate
				// a store instruction — don't skip them.
				if g.reassignedVars != nil && g.reassignedVars[ls.Name.Value] {
					// Fall through to generateLet
				} else {
					lt := g.varLLVMType(ls)
					// For assignments (Type=nil), also check the variable's declared type
					if ls.Type == nil && g.varTypes != nil {
						if t, ok := g.varTypes[ls.Name.Value]; ok {
							lt = t
						}
					}
					if lt != "%str-long" && lt != "%arr" && lt != "%vec" {
						continue
					}
				}
			}
			// Skip function-typed LetStatements (already collected as functions)
			if g.funcRefVars != nil && g.funcRefVars[ls.Name.Value] {
				continue
			}
			g.generateLet(bodyBuf, ls)
		}
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			if hasUserMain {
				if call, ok := es.Expression.(*parser.CallExpression); ok {
					if ident, ok := call.Function.(*parser.Identifier); ok && ident.Value == "main" {
						continue
					}
				}
			}
			g.generateExpressionStmt(bodyBuf, es)
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			g.generateForStatement(bodyBuf, fs)
		}
	}
	// 先寫入 entry-block alloca，再寫入函數體（模組級語句）。
	// 模組級語句包含所有導入模組的初始化（如 aes.no 的 SBOX），
	// 必須在使用者 main 函數之前執行，否則 main 內呼叫的函數會存取未初始化的全域變數。
	sb.WriteString(g.entryAllocaBuf.String())
	sb.WriteString(bodyBuf.String())

	// 使用者 main 函數必須在所有模組級初始化完成後才呼叫，
	// 以確保 main 內呼叫的函數能正確存取已初始化的全域變數（如 SBOX）。
	if hasUserMain {
		sb.WriteString(fmt.Sprintf("%scall void @_nolang_main()\n", g.indent()))
	}
	// 釋放 top-level 堆變數（模組級 vec/str/arr 等），避免長期運行服務的記憶體泄漏。
	// 同時釋放 top-level 局部堆變數（非 globalVars 的局部）。
	g.emitHeapFree(sb)
	g.emitGlobalHeapFree(sb)
	sb.WriteString(g.indent() + "ret i32 0\n")
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

// lookupMethodReturnType returns the LLVM return type for a method call,
// looking up both user-defined methods (g.funcRetTypes + g.funcResultLLVMType
// for void + by-reference output) and builtin methods. Returns "" if the
// method is not found or has no determinable return type.
//
// This is used for literal receivers (IntegerLiteral/FloatLiteral/etc.) where
// the method may be either a user-defined Nolang function (e.g. i64.to-str
// implemented in number.no) or a builtin (e.g. f64.to-str).
func (g *Generator) lookupMethodReturnType(shortName string) string {
	if g.funcRetTypes != nil {
		if t, ok := g.funcRetTypes[shortName]; ok {
			if t != "void" {
				return t
			}
			// void + 單輸出函數（如 i64.to-str = () (out str)）：
			// 使用 funcResultLLVMType 中的輸出型別
			if g.funcResultLLVMType != nil {
				if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
					retType := ts[0]
					if retType == "i1" {
						retType = "i64"
					}
					return retType
				}
			}
		}
	}
	if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
		return g.mapToLLVMType(m.Return[0].String())
	}
	return ""
}

func (g *Generator) varLLVMType(stmt *parser.LetStatement) string {
	// Option type: ?type
	if _, ok := stmt.Type.(*parser.NullableType); ok {
		return "%option"
	}
	// 結構體
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		if t, ok := g.varTypes[sl.Type]; ok {
			return t
		}
		return "%" + sl.Type
	}
	// struct 欄位讀取 (e.g. p-local = fp.path)：依 receiver 型別與欄位名稱查詢 LLVM 型別
	// 支援鏈式存取：非 Identifier receiver 透過 exprResultLLVMType 推導
	if dot, ok := stmt.Value.(*parser.DotExpression); ok {
		recvType := g.exprResultLLVMType(dot.Receiver)
		// .len on str → i64 (must check before struct field lookup,
		// but .len should always return i64)
		if dot.Property == "len" && (recvType == "%str-long") {
			return "i64"
		}
		if strings.HasPrefix(recvType, "%") {
			structName := strings.TrimPrefix(recvType, "%")
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == dot.Property {
						return f.typ
					}
				}
			}
		}
		return "i64"
	}
	// 陣列/切片
	if at, ok := stmt.Type.(*parser.ArrayType); ok {
		// Nested array type (e.g. [12][16]i64): use raw LLVM array type [12 x [16 x i64]]
		if _, isNested := at.Elem.(*parser.ArrayType); isNested {
			return g.arrayTypeToLLVM(at)
		}
		return "%arr"
	}
	if _, ok := stmt.Type.(*parser.SliceType); ok {
		return "%vec"
	}
	// 映射表：m [K]V — use LLVMName() because String() returns [K]V which is
	// indistinguishable from [N]T arrays in string-based dispatch.
	if mt, ok := stmt.Type.(*parser.MapType); ok {
		return g.mapToLLVMType(mt.LLVMName())
	}
	// 顯式型別註釋（如 n i32 = 0 或 a i8）：優先使用型別而非從值推斷
	if stmt.Type != nil {
		ts := stmt.Type.String()
		// 對於基本型別別名（如 i8 同時是 byte/char），使用最精確的 LLVM 型別
		mapped := g.mapToLLVMType(ts)
		if mapped != "" {
			return mapped
		}
	}
	switch v := stmt.Value.(type) {
	case *parser.Identifier:
		// Look up the type of the source variable
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				// When the source is an %option (e.g. `it` from a match block),
				// prefer the target variable's existing type, since the option's
				// inner value has already been extracted by generateExprWithSB.
				if t == "%option" && stmt.Name != nil {
					if existingType, ok := g.varTypes[stmt.Name.Value]; ok {
						return existingType
					}
				}
				return t
			}
		}
		return "i64"
	case *parser.StringLiteral:
		return "%str-long"
	case *parser.CharLiteral:
		return "i32"
	case *parser.RegexLiteral:
		return "%regexp"
	case *parser.InfixExpression:
		// 位元組算術（單字元 StringLiteral 配非字串運算元，如 c - 'A'）應推導為整數
		// 型別，而非 %str-long。需在字串相接檢查之前判斷。
		if v.Operator == "-" || v.Operator == "+" || v.Operator == "*" {
			if (isSingleCharStringLit(v.Left) && !g.isStringExpr(v.Right)) ||
				(isSingleCharStringLit(v.Right) && !g.isStringExpr(v.Left)) {
				// 落入整數算術推導路徑（intExprLLVMType 回傳 i64）
				if ft := g.floatLLVMType(v); ft != "" {
					return ft
				}
				return g.intExprLLVMType(v)
			}
		}
		if (v.Operator == "-" || v.Operator == "+") && (g.isStringExpr(v.Left) || g.isStringExpr(v.Right)) {
			return "%str-long"
		}
		// floatLLVMType 已處理混合 float/double 的型別提升
		if ft := g.floatLLVMType(v); ft != "" {
			return ft
		}
		// 整數算術：使用 intExprLLVMType 推斷正確的整數寬度
		return g.intExprLLVMType(v)
	case *parser.SliceLiteral:
		return "%vec"
	case *parser.ArrayLiteral:
		return "%arr"
	case *parser.IndexExpression:
		// s = arr2d[i] — when arr2d has a raw LLVM array type like [12 x [16 x i64]],
		// the result is a pointer to the element row: [16 x i64]*
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && strings.HasPrefix(t, "[") {
					elemType := extractArrayElemType(t)
					if elemType != "" {
						// Only return a pointer for nested array elements
						// (e.g. arr2d[i] where arr2d is [12 x [16 x i64]] → [16 x i64]*).
						// For scalar elements (e.g. [4 x i64] → i64), the value is
						// loaded, not a pointer.
						if strings.HasPrefix(elemType, "[") {
							return elemType + "*"
						}
						return elemType
					}
				}
				// 切片/陣列元素讀取（如 r = records[i] 其中 records []dns-record）：
				// 依 arrayElemTypes 推導元素型別。struct 元素返回值型別
				// （generateIndexExpression 對 vec/arr 載入元素值而非指標）；
				// 基本型別（i8 等）保持 i64（generateIndexExpression 會 zext i8 → i64）。
				// 注意：必須先檢查變數實際型別是否為 str-long（字串），避免被
				// moduleArrayElemTypes 中同名變數（如 spectral-norm 的 u []f64）污染。
				// 例如 http2-do 中 u = url 使 u 成為 str-long 區域變數，但
				// moduleArrayElemTypes["u"] 可能是 "double"（來自 spectral-norm main），
				// 導致 u[i] 被誤判為 double 比較，實際應為 i64（字串索引 zext 結果）。
				if actualType, ok := g.varTypes[ident.Value]; ok && actualType == "%str-long" {
					return "i64"
				}
				if g.arrayElemTypes != nil {
					if elemType, ok := g.arrayElemTypes[ident.Value]; ok {
						if strings.HasPrefix(elemType, "%") {
							return elemType
						}
						// 浮點數陣列元素（如 [15]f64 → double）必須返回 double/float，
						// 否則 generateLet 會誤判為 i64 並套用 sitofp 於已是 double 的值
						if elemType == "double" || elemType == "float" {
							return elemType
						}
						// 基本型別仍回 i64（byte 讀取在 codegen 中 zext 為 i64）
						return "i64"
					}
				}
			}
		}
		// struct.field[i] — 推導欄位陣列的元素型別
		// （如 .vals[idx] 其中 vals 為 [256 x %str-long]）
		if dot, ok := v.Left.(*parser.DotExpression); ok {
			recvName := ""
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				recvName = ident.Value
			}
			if recvName != "" && g.varTypes != nil {
				if t, ok := g.varTypes[recvName]; ok {
					structName := strings.TrimPrefix(t, "%")
					if fields, ok := g.structTypes[structName]; ok {
						for _, f := range fields {
							if f.name == dot.Property && strings.HasPrefix(f.typ, "[") {
								closeB := strings.IndexByte(f.typ, ']')
								if closeB > 0 {
									inner := f.typ[1:closeB]
									xIdx := strings.LastIndex(inner, " x ")
									if xIdx >= 0 {
										elemType := inner[xIdx+3:]
										if strings.HasPrefix(elemType, "%") {
											return elemType
										}
										return "i64"
									}
								}
							}
						}
					}
				}
			}
		}
		return "i64"
	case *parser.SliceExpression:
		// Check source type: slicing %str-long produces %str-long, otherwise %vec
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && (t == "%str-long") {
					return "%str-long"
				}
			}
		}
		return "%vec"
	case *parser.CastExpression:
		// `expr as Type`: use the target type's LLVM mapping
		if v.Type != nil {
			t := g.mapToLLVMType(v.Type.String())
			if t != "" {
				return t
			}
		}
		return "i64"
	case *parser.CallExpression:
		// -async 函数调用返回 %future（惰性，未执行）
		if g.isAsyncCall(v) {
			if _, _, resultType := g.resolveAsyncCallInfo(v); resultType != "" {
				if g.futureResultTypes != nil && stmt != nil && stmt.Name != nil {
					g.futureResultTypes[stmt.Name.Value] = resultType
				}
			}
			return "%future"
		}
		if ident, ok := v.Function.(*parser.Identifier); ok {
			name := ident.Value
			// FFI extern 函式：依 extern 宣告的 result 型別推斷 Nolang 儲存型別。
			// callExtern 會將 str 構造為 %str-long、ptr/pptr/ppptr/i32/bool 轉為 i64、f64 保持 double。
			if g.externFuncs != nil {
				if ext, ok := g.externFuncs[name]; ok && len(ext.ResultTypes) > 0 {
					return ffiTypeToNolangStorage(ext.ResultTypes[0])
				}
			}
			// Option constructors: val(...), err(...), ok(...) return %option
			if name == "val" || name == "err" || name == "ok" {
				return "%option"
			}
			// with-cap / with-len / with-cap-len: type inferred from LHS type annotation
			if name == "with-cap" || name == "with-len" || name == "with-cap-len" {
				if stmt.Type != nil {
					ts := stmt.Type.String()
					if ts == "str" {
						return "%str-long"
					}
					// Slice types ([]i64, []byte, etc.) → %vec
					return "%vec"
				}
				// Reassignment: infer from existing var type
				if g.varTypes != nil && stmt.Name != nil {
					if t, ok := g.varTypes[stmt.Name.Value]; ok {
						if t == "%str-long" {
							return "%str-long"
						}
						return t
					}
				}
				return "%vec"
			}
			strFns := map[string]bool{
				"int.to-str": true,
				"i64.to-str": true, "i32.to-str": true, "i16.to-str": true, "i8.to-str": true,
				"u64.to-str": true, "u32.to-str": true, "u16.to-str": true, "u8.to-str": true,
				"f64.to-str": true, "f32.to-str": true,
				"bool.to-str": true, "byte.to-str": true, "char-to-str": true,
				"get-env": true, "get-wd": true, "host-name": true,
			}
			if strFns[name] {
				return "%str-long"
			}
			// Check if the function is a builtin that returns f64 or str.
			// Note: bool-returning builtins intentionally fall through to "i64"
			// because their IR emits `zext i1 to i64`, producing an i64 SSA
			// value that the int-coercion path (trunc i64 to i1) handles correctly.
			if m := builtin.FindBuiltinMethod(name); m != nil && len(m.Return) > 0 {
				if m.Return[0] == parser.TypeF64 {
					return "double"
				}
				if m.Return[0] == parser.TypeStr {
					return "%str-long"
				}
			}
			// Check funcRetTypes for non-builtin functions (e.g. module functions like degrees)
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[name]; ok && t != "void" {
					return t
				}
			}
			// 用戶自定義函數使用 void + by-reference 輸出約定，
			// funcRetTypes 為 "void" 但 funcResultLLVMType 仍保留語意型別。
			// 對於單結果函數，從 funcResultLLVMType 取得輸出型別。
			if g.funcNumResults != nil {
				if n, ok := g.funcNumResults[name]; ok && n == 1 {
					if g.funcResultLLVMType != nil {
						if ts, ok := g.funcResultLLVMType[name]; ok && len(ts) == 1 {
							retType := ts[0]
							if retType == "i1" {
								retType = "i64"
							}
							return retType
						}
					}
				}
			}
		}
		// DotExpression receiver call (e.g. s.contains, s.index): look up str.<method>
		if dot, ok := v.Function.(*parser.DotExpression); ok {
			// Module-prefixed builtin call (e.g. fs.get-line, fs.is-file):
			// if the receiver is an Identifier not in varTypes (i.e., a module name),
			// look up the property as a builtin method for return type inference.
			if recvIdent, ok := dot.Receiver.(*parser.Identifier); ok {
				_, isVar := g.varTypes[recvIdent.Value]
				if !isVar {
					// First check user-defined functions with module prefix
					fullName := recvIdent.Value + "." + dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[fullName]; ok && t != "void" {
							return t
						}
					}
					// Then check builtins (strip module prefix)
					if m := builtin.FindBuiltinMethod(dot.Property); m != nil && len(m.Return) > 0 {
						if m.Return[0] == parser.TypeF64 {
							return "double"
						}
						if m.Return[0] == parser.TypeStr {
							return "%str-long"
						}
					}
				}
			}
			recvExpr := dot.Receiver
			// Unwrap GroupedExpression: (123).to-str() → 123.to-str()
			if ge, ok := recvExpr.(*parser.GroupedExpression); ok {
				recvExpr = ge.Expression
			}
			if recv, ok := recvExpr.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					// 基本型別可能對應多個 nolang 型別名稱（如 i32 → char, i32, u32）
					if primAliases, ok := llvmTypeToNolang[srcType]; ok {
						candidates = append(candidates, primAliases...)
					}
					// 切片/陣列 receiver（如 query []byte）：依元素型別構造 []T 候選名稱，
					// 使 []byte.to-str 等方法能被查找到。
					if (srcType == "vec" || srcType == "arr") && g.arrayElemTypes != nil {
						if elemLLVMType, ok := g.arrayElemTypes[recv.Value]; ok {
							if elemAliases, ok := llvmTypeToNolang[elemLLVMType]; ok {
								for _, alias := range elemAliases {
									candidates = append(candidates, "[]"+alias)
								}
							}
						}
					}
					// Option type (?T): try the inner type as a candidate
					// (e.g. conn-val is ?str, conn-val.to-lower() → str.to-lower)
					if srcType == "option" && g.optionInnerTypes != nil {
						if innerType, ok := g.optionInnerTypes[recv.Value]; ok && innerType != "" {
							innerSrc := strings.TrimPrefix(innerType, "%")
							candidates = append(candidates, innerSrc)
							if primAliases, ok := llvmTypeToNolang[innerSrc]; ok {
								candidates = append(candidates, primAliases...)
							}
						}
					}
					for _, cand := range candidates {
						shortName := cand + "." + dot.Property
						if g.funcRetTypes != nil {
							if t, ok := g.funcRetTypes[shortName]; ok {
								if t != "void" {
									return t
								}
								// void + 單輸出函數（如 str.empty 返回 i1）：
								// 使用 funcResultLLVMType 中的輸出型別
								// Nolang bools are stored as i64, not i1
								if g.funcResultLLVMType != nil {
									if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
										retType := ts[0]
										if retType == "i1" {
											retType = "i64"
										}
										return retType
									}
								}
							}
						}
						// Also check build-in methods (e.g., str.eq, str.copy, i64.to-str)
						if m := builtin.FindBuiltinMethod(shortName); m != nil {
							if len(m.Return) > 0 {
								return g.mapToLLVMType(m.Return[0].String())
							}
							// ForwardFunc builtins with empty Return: infer from ForwardFunc name
							if m.ForwardFunc != "" {
								switch m.ForwardFunc {
								case "with-cap", "with-len", "with-cap-len":
									return "%vec"
								case "bool-to-str", "ffi-cstr-at":
									return "%str-long"
								}
							}
						}
					}
					// Fallback: ReceiverGlobal builtins (sqrt, abs, ...) not prefixed by type;
					// look up by method name alone. e.g. r2.sqrt() -> sqrt -> double.
					if m := builtin.FindBuiltinMethod(dot.Property); m != nil && len(m.Return) > 0 {
						return g.mapToLLVMType(m.Return[0].String())
					}
				} else {
					// Module-prefixed function call (e.g., str.with-cap, vec.with-cap)
					// where receiver is a module name, not a variable.
					fullName := recv.Value + "." + dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[fullName]; ok && t != "void" {
							return t
						}
					}
					if m := builtin.FindBuiltinMethod(fullName); m != nil {
						if len(m.Return) > 0 {
							return g.mapToLLVMType(m.Return[0].String())
						}
						if m.ForwardFunc != "" {
							switch m.ForwardFunc {
							case "with-cap", "with-len", "with-cap-len":
								return "%vec"
							case "bool-to-str", "ffi-cstr-at":
								return "%str-long"
							}
						}
					}
					// Fallback: user-defined functions are registered in the maps
					// under their SIMPLE name (e.g. "list-dir"), not the module-prefixed
					// name (e.g. "fs.list-dir"). When the receiver is a module name
					// (not a variable), try looking up the property directly as a
					// user-defined function name. This makes `entries = fs.list-dir(dir)`
					// resolve to "%vec" (the LLVM type of list-dir's []str result)
					// instead of defaulting to "i64", which is critical for downstream
					// arrayElemTypes inference and correct slice indexing codegen.
					if g.funcNumResults != nil {
						if n, ok := g.funcNumResults[dot.Property]; ok {
							if n == 1 {
								if g.funcResultLLVMType != nil {
									if ts, ok2 := g.funcResultLLVMType[dot.Property]; ok2 && len(ts) == 1 {
										retType := ts[0]
										if retType == "i1" {
											retType = "i64"
										}
										return retType
									}
								}
							}
						}
					}
				}
			}
			// IntegerLiteral receiver (e.g., (123).to-str())
			if _, ok := recvExpr.(*parser.IntegerLiteral); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// FloatLiteral receiver (e.g., (3.14).to-str())
			if _, ok := recvExpr.(*parser.FloatLiteral); ok {
				shortName := "f64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// BooleanLiteral receiver (e.g., true.to-str())
			if _, ok := recvExpr.(*parser.BooleanLiteral); ok {
				shortName := "bool." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// PrefixExpression receiver (e.g., (-42).to-str())
			if _, ok := recvExpr.(*parser.PrefixExpression); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// InfixExpression receiver (e.g., (-9223372036854775807 - 1).to-str()，
			// 已被上方 Unwrap GroupedExpression 處理後為 InfixExpression)
			// 算術表達式視為 i64
			if _, ok := recvExpr.(*parser.InfixExpression); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// StringLiteral receiver (e.g., 'hello'.eq(b, n))
			if _, ok := recvExpr.(*parser.StringLiteral); ok {
				shortName := "str." + dot.Property
				if g.funcRetTypes != nil {
					if t, ok := g.funcRetTypes[shortName]; ok {
						if t != "void" {
							return t
						}
						// void + 單輸出函數（如 str.repeat 返回 str）：使用 funcResultLLVMType
						// Nolang bools are stored as i64, not i1
						if g.funcResultLLVMType != nil {
							if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
								retType := ts[0]
								if retType == "i1" {
									retType = "i64"
								}
								return retType
							}
						}
					}
				}
				if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
					return g.mapToLLVMType(m.Return[0].String())
				}
			}
			// 結構欄位 / 陣列元素 / 切片結果 / 函數呼叫結果接收者
			// （如 c.name.trim()、names[i].slice()、buf[..].to-str()、foo().trim()）
			// 透過 exprResultLLVMType 推導接收者型別，再映射到 nolang 型別名查找方法返回型別
			switch recvExpr.(type) {
			case *parser.DotExpression, *parser.IndexExpression, *parser.SliceExpression, *parser.CallExpression:
				elemType := g.exprResultLLVMType(recvExpr)
				srcType := strings.TrimPrefix(elemType, "%")
				candidates := []string{srcType}
				if primAliases, ok := llvmTypeToNolang[srcType]; ok {
					candidates = append(candidates, primAliases...)
				}
				// vec/arr 切片結果：依元素型別構造 []T 候選（如 []byte.to-str）
				if srcType == "vec" || srcType == "arr" {
					if sliceExpr, ok := recvExpr.(*parser.SliceExpression); ok {
						if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
							if g.arrayElemTypes != nil {
								if et, ok := g.arrayElemTypes[ident.Value]; ok {
									if elemAliases, ok := llvmTypeToNolang[et]; ok {
										for _, alias := range elemAliases {
											candidates = append(candidates, "[]"+alias)
										}
									}
								}
							}
						}
					}
				}
				for _, cand := range candidates {
					shortName := cand + "." + dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[shortName]; ok {
							if t != "void" {
								return t
							}
							if g.funcResultLLVMType != nil {
								if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
									retType := ts[0]
									if retType == "i1" {
										retType = "i64"
									}
									return retType
								}
							}
						}
					}
					if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
						return g.mapToLLVMType(m.Return[0].String())
					}
				}
			}
		}
		return "i64"
	case *parser.FloatLiteral:
		return "double"
	case *parser.PrefixExpression:
		// 負號前置運算：根據運算元型別推導
		// -1.0 → double, -1 → i64, -x → 視 x 型別而定
		if v.Operator == "-" {
			return g.varLLVMType(&parser.LetStatement{Value: v.Right})
		}
		return "i64"
	case *parser.GroupedExpression:
		return g.varLLVMType(&parser.LetStatement{Value: v.Expression})
	case *parser.IfExpression:
		// if 表達式：從 consequence 推斷型別
		if v.Consequence != nil && len(v.Consequence.Statements) > 0 {
			last := v.Consequence.Statements[len(v.Consequence.Statements)-1]
			if es, ok := last.(*parser.ExpressionStatement); ok {
				return g.varLLVMType(&parser.LetStatement{Value: es.Expression})
			}
		}
		// 從 alternative 推斷
		if v.Alternative != nil && len(v.Alternative.Statements) > 0 {
			last := v.Alternative.Statements[len(v.Alternative.Statements)-1]
			if es, ok := last.(*parser.ExpressionStatement); ok {
				return g.varLLVMType(&parser.LetStatement{Value: es.Expression})
			}
		}
		return "i64"
	case *parser.RunExpression:
		// Track result type for awy type inference
		switch c := v.Call.(type) {
		case *parser.CallExpression:
			_, _, resultType := g.resolveAsyncCallInfo(c)
			if resultType != "" && g.taskResultTypes != nil && stmt != nil && stmt.Name != nil {
				g.taskResultTypes[stmt.Name.Value] = resultType
			}
		case *parser.Identifier:
			// run future_var — 从 futureResultTypes 传播到 taskResultTypes
			if g.futureResultTypes != nil && g.taskResultTypes != nil && stmt != nil && stmt.Name != nil {
				if t, ok := g.futureResultTypes[c.Value]; ok {
					g.taskResultTypes[stmt.Name.Value] = t
				}
			}
		}
		return "%task*"
	case *parser.AwaitExpression:
		// awy f-async(args) — 直接调用 -async 函数
		if call, ok := v.Right.(*parser.CallExpression); ok {
			if g.isAsyncCall(call) {
				if _, _, resultType := g.resolveAsyncCallInfo(call); resultType != "" {
					return resultType
				}
			}
		}
		// awy <identifier> — future 变量或 task 变量
		if ident, ok := v.Right.(*parser.Identifier); ok {
			if g.futureResultTypes != nil {
				if t, ok := g.futureResultTypes[ident.Value]; ok {
					return t
				}
			}
			if g.taskResultTypes != nil {
				if t, ok := g.taskResultTypes[ident.Value]; ok {
					return t
				}
			}
		}
		return "i64"
	default:
		return "i64"
	}
}

// inferOptionInnerType determines the inner LLVM type of a ?T option variable
// from its LetStatement. Used during var collection (before codegen) so that
// subsequent varLLVMType calls can resolve method calls on option variables
// (e.g. cl = conn-val.to-lower() where conn-val is ?str).
func (g *Generator) inferOptionInnerType(stmt *parser.LetStatement) string {
	// From explicit type annotation (e.g. val ?f64)
	if nt, ok := stmt.Type.(*parser.NullableType); ok {
		return g.mapToLLVMType(nt.Type.String())
	}
	// From option variable assignment (e.g. it = n where n is ?i64)
	if ident, ok := stmt.Value.(*parser.Identifier); ok {
		if g.optionInnerTypes != nil {
			if srcInner, ok := g.optionInnerTypes[ident.Value]; ok && srcInner != "" {
				return srcInner
			}
		}
	}
	// From function call return type (e.g. f = .get-header(...) returning ?str)
	if call, ok := stmt.Value.(*parser.CallExpression); ok {
		fnName := ""
		if ident, ok := call.Function.(*parser.Identifier); ok {
			fnName = ident.Value
		} else if dot, ok := call.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					fnName = srcType + "." + dot.Property
				}
			}
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				fnName = "str." + dot.Property
			}
		}
		if fnName != "" && g.funcResultInnerTypes != nil {
			if innerTypes, ok := g.funcResultInnerTypes[fnName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
				return innerTypes[0]
			}
		}
	}
	return ""
}

func (g *Generator) collectRangeVarTypes(stmt parser.Statement, vars map[string]string) {
	switch s := stmt.(type) {
	case *parser.ForStatement:
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectRangeVarTypes(ss, vars)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectRangeVarTypes(ss, vars)
		}
	}
}

func (g *Generator) collectVarDecls(program *parser.Program) map[string]string {
	vars := make(map[string]string)
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			// Don't overwrite existing type — first declaration wins (e.g., a i8; a = 2)
			if _, exists := vars[s.Name.Value]; !exists {
				t := g.varLLVMType(s)
				vars[s.Name.Value] = t
				g.varTypes[s.Name.Value] = t // register immediately for later varLLVMType calls
				// Register array element type for module-level [N]T globals (e.g. SBOX [256]byte)
				// so that IndexExpression codegen uses the correct element type instead of
				// defaulting to i64.
				if at, ok := s.Type.(*parser.ArrayType); ok && at.Size != nil && at.Elem != nil && g.arrayElemTypes != nil {
					g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(at.Elem.String())
				}
				// Register slice element type for module-level []T globals
				if st, ok := s.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
					g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(st.Elem.String())
				}
			}
			// Recurse into value expression to collect inner variables
			// (e.g. synthetic `it` injected by match desugar inside if-expression branches)
			if s.Value != nil {
				g.collectVarDeclsFromExpr(s.Value, vars)
			}
		case *parser.FunctionDefinition:
		// Skip function bodies - variables inside functions are collected
		// in generateFunctionDefinition via collectVarDeclsFromStmt.
		// This prevents synthetic `it` variables from one function (e.g., %file type)
		// from polluting module-level variable types and leaking into other functions.
		default:
			g.collectVarDeclsFromStmtInner(s, vars, true)
		}
	}
	return vars
}

func (g *Generator) collectVarDeclsFromStmt(stmt parser.Statement, vars map[string]string) {
	g.collectVarDeclsFromStmtInner(stmt, vars, false)
}

func (g *Generator) collectVarDeclsFromStmtInner(stmt parser.Statement, vars map[string]string, isModuleLevel bool) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// Skip synthetic let statements with "err"/"nil" type sentinels.
		// 這些是 match 對應 err/nil arm 注入的 `it = matched`，
		// 變數型別語意上是 err/nil（無值），LLVM 端以 i64 佔位即可。
		// 不註冊為 option 型別，否則後續查找會誤用 matched 的型別。
		if s.IsSynthetic {
			// Note: cross-function type pollution is already prevented because
			// generateFunctionDefinition resets g.varTypes per function (line 1599)
			// and collectVarDecls skips function bodies. Module-level synthetic `it`
			// must be collected so that %it gets allocated for top-level match.
			if nt, ok := s.Type.(*parser.NamedType); ok {
				if nt.Value == "err" || nt.Value == "nil" {
					if _, exists := vars[s.Name.Value]; !exists {
						vars[s.Name.Value] = "i64"
						if g.varTypes != nil {
							g.varTypes[s.Name.Value] = "i64"
						}
					}
					return
				}
			}
			// Synthetic let with non-err/non-nil type (e.g. ok arm with elemType "file"):
			// 若之前已有 placeholder（i64 from err/nil arm），需覆寫以使用真實型別。
			// 否則註冊新變數。
			// `it` 變數跨多個 match 共用：每個 match 的 ok arm 可能有不同的元素型別
			// （如 ?str → %str-long, ?client → %client），必須每次更新以確保
			// 方法呼叫解析（it.close()）能正確找到型別前綴。
			vt := g.varLLVMType(s)
			if existing, exists := vars[s.Name.Value]; !exists || existing == "i64" || (strings.HasPrefix(existing, "%") && existing != vt) {
				// 選擇較大的型別以避免 overflow：struct 型別（%）通常比 i64 大，
				// 多個 struct 型別則保留先到的（因為 option data field 固定 16 bytes）。
				if !exists || existing == "i64" || g.llvmTypeSize(vt) > g.llvmTypeSize(existing) {
					vars[s.Name.Value] = vt
					if g.varTypes != nil {
						g.varTypes[s.Name.Value] = vt
					}
				}
			}
			return
		}
		// Don't overwrite existing type (e.g. %option declared with ?type)
		if _, exists := vars[s.Name.Value]; !exists {
			// 對模組級全域變數的重新賦值（Type==nil 表示賦值而非宣告），
			// 不應創建本地 alloca 覆蓋全域變數。例如 gen-random 中的
			// `LAST = (LAST * IA + IC) % IM` 必須更新全域 @LAST，
			// 否則每次呼叫都從未初始化的本地 LAST 開始，破壞 RNG 狀態。
			// 但僅限主檔案函數：導入模組的函數（如 bigint.cmp、abs-add）
			// 若有名為 result 的局部變數，不應誤寫到主檔案的全域 @result。
			// 例外：若同名變數是當前函數的參數（如 make-repeat-fasta 的參數 n
			// 與模組級 n i64 = 1000 同名），參數應遮蔽全域變數，不可刪除
			// funcLocalNames 中的記錄，否則 varAddr 會錯誤返回 @n 而非 %n。
			if s.Type == nil && g.globalVars != nil && g.globalVars[s.Name.Value] &&
				g.curFuncName != "" && g.mainFileNames != nil && g.mainFileNames[g.curFuncName] &&
				(g.funcParams == nil || !g.funcParams[s.Name.Value]) {
				if g.funcLocalNames != nil {
					delete(g.funcLocalNames, s.Name.Value)
				}
				if s.Value != nil {
					g.collectVarDeclsFromExpr(s.Value, vars)
				}
				return
			}
			// 切片表達式（view = arr[0..4]）總是走 clone 路徑（malloc + memcpy），
			// 變量需要獨立的 alloca 存儲空間。不再跳過 alloca。
			vt := g.varLLVMType(s)
			vars[s.Name.Value] = vt
			// Update g.varTypes immediately so subsequent lookups work
			if g.varTypes != nil {
				g.varTypes[s.Name.Value] = vt
			}
			// Populate optionInnerTypes for ?T variables during collection so that
			// subsequent varLLVMType calls can resolve method calls on option
			// variables (e.g. cl = conn-val.to-lower() where conn-val is ?str).
			if vt == "%option" && g.optionInnerTypes != nil {
				if _, exists := g.optionInnerTypes[s.Name.Value]; !exists {
					if inner := g.inferOptionInnerType(s); inner != "" {
						g.optionInnerTypes[s.Name.Value] = inner
					}
				}
				// 若 inner 為堆型別，將 option 變數加入 heapVars 追蹤，
				// 確保 v = m.get(...) 這類經 collectVarDecls 推導型別的 option 變數
				// 也在函數結束時釋放其持有的 box。
				if g.heapVars != nil {
					if inner, ok := g.optionInnerTypes[s.Name.Value]; ok && g.isHeapOwningType(inner) {
						if _, isLocal := g.funcLocalNames[s.Name.Value]; isLocal {
							if _, isParam := g.paramNames[s.Name.Value]; !isParam {
								if g.outputParamNames == nil || !g.outputParamNames[s.Name.Value] {
									g.trackLocalHeapVar(s.Name.Value, "%option")
								}
							}
						}
					}
				}
			}
			// Track array size for [N]T locals so we can malloc the data buffer
			if at, ok := s.Type.(*parser.ArrayType); ok && at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					if g.arraySizes != nil {
						g.arraySizes[s.Name.Value] = intLit.Value
					}
					// Also register element type for IndexExpression
					if at.Elem != nil && g.arrayElemTypes != nil {
						g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(at.Elem.String())
					}
				}
			}
			// Register slice element type for []T locals (e.g. []byte → "i8").
			// This must happen during collection (not just in generateLet) so that
			// subsequent varLLVMType calls can resolve []T.method calls (e.g.
			// packet-str = packet.to-str() needs arrayElemTypes["packet"]="i8"
			// to find []byte.to-str and infer %str-long instead of defaulting to i64).
			if st, ok := s.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
				g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(st.Elem.String())
			}
			// Infer array/slice element type and size from function call return type
			// (e.g. inner-hash = sha256(...) where sha256 returns [32]byte,
			// or raw = list-dir(...) where list-dir returns []str)
			if (vt == "%arr" || vt == "%vec") && g.funcResultNolangTypes != nil {
				fnName := ""
				if call, ok := s.Value.(*parser.CallExpression); ok {
					if ident, ok := call.Function.(*parser.Identifier); ok {
						fnName = ident.Value
					} else if dot, ok := call.Function.(*parser.DotExpression); ok {
						// Resolve method name (e.g. content.split → str.split)
						if recv, ok := dot.Receiver.(*parser.Identifier); ok {
							if recvType, ok := g.varTypes[recv.Value]; ok {
								srcType := strings.TrimPrefix(recvType, "%")
								candidates := []string{srcType}
								if primAliases, ok := llvmTypeToNolang[srcType]; ok {
									candidates = append(candidates, primAliases...)
								}
								for _, cand := range candidates {
									fullName := cand + "." + dot.Property
									if _, ok := g.funcResultNolangTypes[fullName]; ok {
										fnName = fullName
										break
									}
								}
							} else {
								// Fallback: receiver is a module name (e.g. "fs"),
								// not a variable. User-defined functions are
								// registered under their simple name (e.g.
								// "list-dir"), so look up dot.Property directly.
								// This makes `entries = fs.list-dir(dir)` infer
								// the []str element type (%str-long) and register
								// g.arrayElemTypes["entries"], fixing slice
								// indexing codegen (24-byte stride vs 8-byte).
								if _, ok := g.funcResultNolangTypes[dot.Property]; ok {
									fnName = dot.Property
								}
							}
						}
					}
				}
				if fnName != "" {
					if nolangRets, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangRets) == 1 {
						// Parse [N]T or []T format
						nolangType := nolangRets[0]
						if strings.HasPrefix(nolangType, "[") {
							// Extract element type: [N]T → T or []T → T
							if rbracket := strings.Index(nolangType, "]"); rbracket > 0 {
								elemType := nolangType[rbracket+1:]
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(elemType)
								}
								// Extract size: [N]T → N (empty for slices []T)
								sizeStr := nolangType[1:rbracket]
								if n, err := fmt.Sscanf(sizeStr, "%d", new(int64)); err == nil && n == 1 {
									if g.arraySizes != nil {
										var sz int64
										fmt.Sscanf(sizeStr, "%d", &sz)
										g.arraySizes[s.Name.Value] = sz
									}
								}
							}
						}
					}
				}
			}
		}
		// Recurse into value expression to collect inner variables
		// (e.g. `it` injected by match desugar inside if-expression branches)
		if s.Value != nil {
			g.collectVarDeclsFromExpr(s.Value, vars)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			g.collectVarDeclsFromStmt(s.Init, vars)
		}
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
				// Update g.varTypes immediately so that subsequent varLLVMType
				// calls for variables referencing this range variable (e.g. nx = y)
				// see the local i64 type, not a stale module-level type (e.g. y = 1.0
				// at module level would leave g.varTypes["y"] = "double").
				if g.varTypes != nil {
					g.varTypes[s.IterRange.Variable] = "i64"
				}
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	case *parser.ExpressionStatement:
		g.collectVarDeclsFromExpr(s.Expression, vars)
	case *parser.MultiAssignStatement:
		// 註冊多賦值左側的所有變數
		// 對於多結果函數調用，需要根據函數的輸出參數型別推斷每個變數的型別
		if callExpr, ok := s.Value.(*parser.CallExpression); ok {
			fnName := ""
			if ident, ok := callExpr.Function.(*parser.Identifier); ok {
				fnName = ident.Value
			} else if dot, ok := callExpr.Function.(*parser.DotExpression); ok {
				fnName = dot.Property
				// 嘗試解析完整方法名（如 str.to-upper）以查詢 funcResultLLVMType
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					recvType := ""
					if t, ok := vars[recv.Value]; ok {
						recvType = strings.TrimPrefix(t, "%")
					} else if g.varTypes != nil {
						if t, ok := g.varTypes[recv.Value]; ok {
							recvType = strings.TrimPrefix(t, "%")
						}
					}
					if recvType != "" {
						fullName := recvType + "." + dot.Property
						if g.funcResultLLVMType != nil {
							if _, ok := g.funcResultLLVMType[fullName]; ok {
								fnName = fullName
							}
						}
					}
				}
			}
			// Determine the return types for each target position
			var retTypes []string
			if fnName != "" {
				if g.funcResultLLVMType != nil {
					if rets, ok := g.funcResultLLVMType[fnName]; ok && len(rets) >= len(s.Targets) {
						for _, t := range rets {
							if t == "i1" {
								t = "i64"
							}
							retTypes = append(retTypes, t)
						}
					} else if m := builtin.FindBuiltinMethod(fnName); m != nil && len(m.Return) >= len(s.Targets) {
						for _, r := range m.Return {
							t := g.mapToLLVMType(r.String())
							if t == "i1" {
								t = "i64"
							}
							retTypes = append(retTypes, t)
						}
					}
				} else if m := builtin.FindBuiltinMethod(fnName); m != nil && len(m.Return) >= len(s.Targets) {
					for _, r := range m.Return {
						t := g.mapToLLVMType(r.String())
						if t == "i1" {
							t = "i64"
						}
						retTypes = append(retTypes, t)
					}
				}
			}
			// Register each Identifier target with the corresponding return type
			for i, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					if _, exists := vars[ident.Value]; !exists {
						if i < len(retTypes) {
							vars[ident.Value] = retTypes[i]
						} else {
							vars[ident.Value] = "i64"
						}
					}
				}
				// IndexExpression targets (e.g., fields[n]) are not new variables
			}
		} else {
			for _, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					if _, exists := vars[ident.Value]; !exists {
						vars[ident.Value] = "i64"
					}
				}
			}
		}
		// 遞迴收集值中的變數宣告
		if s.Value != nil {
			g.collectVarDeclsFromExpr(s.Value, vars)
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectVarDeclsFromStmt(ss, vars)
		}
	}
}

func (g *Generator) collectVarDeclsFromExpr(expr parser.Expression, vars map[string]string) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	case *parser.CallExpression:
		// 註冊輸出參數變數（函調用的最後一個參數為 Identifier 時）
		if g.funcRetTypes != nil && len(e.Arguments) > 0 {
			fnName := ""
			if ident, ok := e.Function.(*parser.Identifier); ok {
				fnName = ident.Value
			} else if dot, ok := e.Function.(*parser.DotExpression); ok {
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					fnName = recv.Value + "." + dot.Property
				}
			}
			if fnName != "" {
				if retType, ok := g.funcRetTypes[fnName]; ok && retType != "void" {
					if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
						// Single named result: last arg might be output param
						lastArgIdent, isIdent := e.Arguments[len(e.Arguments)-1].(*parser.Identifier)
						if isIdent {
							paramCount := len(e.Arguments)
							if g.funcParamCount != nil {
								if pc, ok := g.funcParamCount[fnName]; ok {
									paramCount = pc
								}
							}
							if len(e.Arguments) > paramCount {
								varName := lastArgIdent.Value
								if _, exists := vars[varName]; !exists {
									vars[varName] = retType
									if g.varTypes != nil {
										g.varTypes[varName] = retType
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func (g *Generator) collectStructType(sd *parser.StructDefinition) {
	g.collectStructTypeFields(sd)
	// 註冊 struct type 名稱
	g.varTypes[sd.Name] = "%" + sd.Name
}

// collectStructTypeFields 只收集結構體欄位型別到 g.structTypes，不寫入 g.varTypes。
// 適用於需要 structTypes 已被填充但不希望汙染 varTypes 的早期階段
// （如函數參數/回傳型別預掃描）。
func (g *Generator) collectStructTypeFields(sd *parser.StructDefinition) {
	var fields []structField
	for _, f := range sd.Fields {
		llvmType := "i64"
		elemTy := "" // element type for %vec fields
		if f.ArraySize > 0 {
			elemType := "i64"
			if f.Type != nil {
				elemType = g.mapToLLVMType(f.Type.String())
			}
			llvmType = fmt.Sprintf("[%d x %s]", f.ArraySize, elemType)
		} else if f.IsSlice {
			// 切片用 %vec 型別
			llvmType = "%vec"
			// 記錄元素型別（byte → i8, i64 → i64, 等）
			if f.Type != nil {
				elemTy = g.mapToLLVMType(f.Type.String())
			}
			if elemTy == "" {
				elemTy = "i64"
			}
		} else if f.Type != nil {
			llvmType = g.mapToLLVMType(f.Type.String())
		}
		fields = append(fields, structField{name: f.Name, typ: llvmType, elemType: elemTy})
	}
	g.structTypes[sd.Name] = fields
}

func (g *Generator) generateStatement(sb *strings.Builder, stmt parser.Statement) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		g.generateLet(sb, s)
	case *parser.ExpressionStatement:
		g.generateExpressionStmt(sb, s)
	case *parser.ForStatement:
		g.generateForStatement(sb, s)
	case *parser.BreakStatement:
		// Do NOT emit lifetime.end here — break only exits the innermost loop,
		// not the function. Calling emitLifetimeEnd would mark all function
		// locals as dead, causing -O3 to treat subsequent accesses (e.g. outer
		// loop's `i`, `matched`, `limit`) as undefined behavior.
		target := g.findLoopTarget(s.Label, true)
		if target != "" {
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
			g.blockTerminated = true
		}

	case *parser.ContinueStatement:
		target := g.findLoopTarget(s.Label, false)
		if target != "" {
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
			g.blockTerminated = true
		}

	case *parser.InterfaceDefinition:
		// 介面定義不生成 LLVM IR

	case *parser.EnumDefinition:
		g.generateEnumDefinition(sb, s)

	case *parser.StructDefinition:
		// type already emitted in Generate()
	// struct definition 本身不生成 IR（type 已由 Generate 發出）
	case *parser.MultiAssignStatement:
		if innerCall, ok := s.Value.(*parser.CallExpression); ok {
			outerCall := &parser.CallExpression{
				Token:     innerCall.Token,
				Function:  innerCall,
				Arguments: s.Targets,
			}
			g.generateExpressionStmt(sb, &parser.ExpressionStatement{Expression: outerCall})
		}

	case *parser.ReturnStatement:
		g.flushOutputBindings(sb)
		g.emitHeapFree(sb)
		g.emitLifetimeEnd(sb)
		if s.ReturnValue != nil {
			val := g.generateExprWithSB(sb, s.ReturnValue)
			retType := g.curFuncRetType
			if retType == "" {
				retType = "i64"
			}
			sb.WriteString(fmt.Sprintf("%sret %s %s\n", g.indent(), retType, val))
		} else if g.curFuncRetType != "void" && g.curFuncRetName != "" {
			// 有輸出參數的裸 return：載入輸出參數並返回
			g.tmpIdx++
			resultLoad := fmt.Sprintf("%%ret.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %%%s\n",
				g.indent(), resultLoad, g.curFuncRetType, g.curFuncRetType, g.curFuncRetName))
			sb.WriteString(fmt.Sprintf("%sret %s %s\n", g.indent(), g.curFuncRetType, resultLoad))
		} else if g.curFuncRetType != "void" && g.curFuncRetType != "" {
			// 無輸出參數但有回傳型別：回傳零值
			sb.WriteString(fmt.Sprintf("%sret %s 0\n", g.indent(), g.curFuncRetType))
		} else if g.inMainFunction {
			// main 函數的裸 return：回傳 i32 0（C 入口點慣例）
			sb.WriteString(g.indent() + "ret i32 0\n")
		} else {
			sb.WriteString(g.indent() + "ret void\n")
		}
		g.blockTerminated = true
	}
}

func (g *Generator) generateForStatement(sb *strings.Builder, stmt *parser.ForStatement) {
	// range for: for i in [a..b] — push/pop handled in generateRangeFor
	if stmt.IterRange != nil {
		g.generateRangeFor(sb, stmt)
		return
	}

	// Push loop exit target
	g.tmpIdx++
	labelId := g.tmpIdx

	// 次數循環：{ } * N
	if stmt.CountExpr != nil {
		// 編譯期優化：若 N 為常數且 ≤ 0，循環體不執行，直接跳過
		if intLit, ok := stmt.CountExpr.(*parser.IntegerLiteral); ok && intLit.Value <= 0 {
			return
		}
		counterVar := fmt.Sprintf("__lc_%d", labelId)
		// init: %__lc_N = alloca i64, store 0
		sb.WriteString(fmt.Sprintf("%s%%%s = alloca i64\n", g.indent(), counterVar))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), counterVar))

		g.loopExits = append(g.loopExits, loopExit{
			name: stmt.Label,
			cond: fmt.Sprintf("for.step.%d", labelId),
			exit: fmt.Sprintf("for.end.%d", labelId),
		})
		defer func() {
			g.loopExits = g.loopExits[:len(g.loopExits)-1]
		}()

		// br → cond
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))

		// cond block
		g.emitLabel(sb, fmt.Sprintf("for.cond.%d", labelId))
		g.indentLevel++
		counterLoad := fmt.Sprintf("%%%s.val", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), counterLoad, counterVar))
		cmpVal := g.generateExprWithSB(sb, stmt.CountExpr)
		cmpResult := fmt.Sprintf("%%%s.cmp", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpResult, counterLoad, cmpVal))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
			g.indent(), cmpResult, labelId, labelId))
		g.indentLevel--

		// body block
		g.emitLabel(sb, fmt.Sprintf("for.body.%d", labelId))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		// body 未終止時跳到 step 執行更新
		if !g.blockTerminated {
			sb.WriteString(fmt.Sprintf("%sbr label %%for.step.%d\n", g.indent(), labelId))
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: counter++（continue 跳轉目標）
		g.emitLabel(sb, fmt.Sprintf("for.step.%d", labelId))
		g.indentLevel++
		// update: %val = load i64, %cnt; %inc = add i64 %val, 1; store i64 %inc, %cnt
		updateLoad := fmt.Sprintf("%%%s.val2", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), updateLoad, counterVar))
		updateInc := fmt.Sprintf("%%%s.inc", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), updateInc, updateLoad))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), updateInc, counterVar))
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("for.end.%d", labelId))
		return
	}

	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("for.step.%d", labelId),
		exit: fmt.Sprintf("for.end.%d", labelId),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// init
	if stmt.Init != nil {
		g.generateStatement(sb, stmt.Init)
	}

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))

	// cond block
	g.emitLabel(sb, fmt.Sprintf("for.cond.%d", labelId))
	g.indentLevel++
	condVal := ""
	if stmt.Condition != nil {
		// 若條件是 InfixExpression（比較運算），直接取 i1
		if infix, ok := stmt.Condition.(*parser.InfixExpression); ok {
			isCmp := infix.Operator == "==" || infix.Operator == "!=" ||
				infix.Operator == "<" || infix.Operator == ">" ||
				infix.Operator == "<=" || infix.Operator == ">="
			if isCmp {
				condVal = g.generateInfixI1(sb, infix)
			} else {
				// 非比較運算（如 && / ||）返回 i64，需 trunc 到 i1
				rawVal := g.generateExprWithSB(sb, stmt.Condition)
				if strings.HasPrefix(rawVal, "%") {
					g.tmpIdx++
					truncReg := fmt.Sprintf("%%for.trunc.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), truncReg, rawVal))
					condVal = truncReg
				} else {
					condVal = rawVal
				}
			}
		} else {
			// Option variable as boolean condition (e.g. for cond = recv-f where
			// recv-f is ?T): check tag != 1 (nil) instead of truncating the data
			// pointer to i1. Without this, struct inner types (e.g. ?http2-frame)
			// would return a %T* data pointer and fail to truncate.
			if ident, ok := stmt.Condition.(*parser.Identifier); ok {
				if t, ok := g.varTypes[ident.Value]; ok && t == "%option" {
					g.tmpIdx++
					tagGEP := fmt.Sprintf("%%for.opt.tag.gep.%d", g.tmpIdx)
					g.tmpIdx++
					tagLoad := fmt.Sprintf("%%for.opt.tag.%d", g.tmpIdx)
					g.tmpIdx++
					cmpReg := fmt.Sprintf("%%for.opt.cmp.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
						g.indent(), tagGEP, llvmVarRef(ident.Value)))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), tagLoad, tagGEP))
					sb.WriteString(fmt.Sprintf("%s%s = icmp ne i64 %s, 1\n", g.indent(), cmpReg, tagLoad))
					condVal = cmpReg
				}
			}
			if condVal == "" {
				rawVal := g.generateExprWithSB(sb, stmt.Condition)
				if strings.HasPrefix(rawVal, "%") {
					// 判斷條件表達式的 LLVM 型別。若已是 i1（如 bool 返回的用戶函數 rows.next()），
					// 直接使用；否則為 i64，需 trunc 到 i1。
					condType := g.intExprLLVMType(stmt.Condition)
					if condType == "i1" {
						condVal = rawVal
					} else {
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%for.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), truncReg, rawVal))
						condVal = truncReg
					}
				} else {
					condVal = rawVal
				}
			}
		}
	} else {
		condVal = "1" // infinite loop
	}
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
		g.indent(), condVal, labelId, labelId))
	g.indentLevel--

	// body block
	g.emitLabel(sb, fmt.Sprintf("for.body.%d", labelId))
	g.indentLevel++
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// body 未終止時跳到 step 執行更新
	if !g.blockTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%for.step.%d\n", g.indent(), labelId))
	}
	g.blockTerminated = false
	g.indentLevel--

	// step block: 執行 update（continue 跳轉目標）
	g.emitLabel(sb, fmt.Sprintf("for.step.%d", labelId))
	g.indentLevel++
	// update
	if stmt.Update != nil {
		g.generateStatement(sb, stmt.Update)
	}
	sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
	g.indentLevel--

	// end block
	g.emitLabel(sb, fmt.Sprintf("for.end.%d", labelId))
}

func (g *Generator) generateStringRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	str := ir.RangeStr
	g.tmpIdx++
	lbl := g.tmpIdx

	// 建立字串常數
	idx := g.stringIdx
	g.stringIdx++
	escaped := g.escapeLLVMString(str)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", idx, len(str), escaped))

	g.tmpIdx++
	idxReg := fmt.Sprintf("%%str-longidx.%d", g.tmpIdx)
	g.tmpIdx++
	ptrReg := fmt.Sprintf("%%str-longptr.%d", g.tmpIdx)

	// init: idx = 0
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), idxReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), idxReg, varName))
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), ptrReg))

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%str-long.cond.%d\n", g.indent(), lbl))

	// cond block
	g.emitLabel(sb, fmt.Sprintf("str.cond.%d", lbl))
	g.indentLevel++
	g.tmpIdx++
	iLoad := fmt.Sprintf("%%str-longi.%d", g.tmpIdx)
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%str-longcmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmpReg, iLoad, len(str)))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%str-long.body.%d, label %%str-long.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	g.indentLevel--

	// body: char = str[i]; varName = char
	g.emitLabel(sb, fmt.Sprintf("str.body.%d", lbl))
	g.indentLevel++
	g.tmpIdx++
	chReg := fmt.Sprintf("%%str-longch.%d", g.tmpIdx)
	g.tmpIdx++
	chZext := fmt.Sprintf("%%str-longchz.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [%d x i8], [%d x i8]* @.str.%d, i64 0, i64 %s\n",
		g.indent(), chReg, len(str)+1, len(str)+1, idx, iLoad))
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), chZext, chReg))
	g.tmpIdx++
	charVal := fmt.Sprintf("%%str-longcv.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), charVal, chZext))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), charVal, varName))

	// body statements
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}

	// update: idx++
	g.tmpIdx++
	iNext := fmt.Sprintf("%%str-longnext.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%str-long.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	g.emitLabel(sb, fmt.Sprintf("str.end.%d", lbl))
}

func (g *Generator) generateArrayRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	var structPtr string // "%arr* %%identName" or "%vec* %vec.tmp.N"
	var structType string
	var isVec bool
	var elemType string

	// Determine the source: named variable or inline slice literal
	if ident, ok := ir.RangeExpr.(*parser.Identifier); ok {
		// Named variable: for i in a
		identName := ident.Value

		// Slice view: use adjusted data pointer + view length directly
		if g.isSliceViewVar(identName) {
			view := g.sliceViews[identName]
			elemType = view.elemType
			structType = view.baseType
			isVec = !view.isStr

			g.tmpIdx++
			lbl := g.tmpIdx
			g.loopExits = append(g.loopExits, loopExit{
				name: stmt.Label,
				cond: fmt.Sprintf("arr.step.%d", lbl),
				exit: fmt.Sprintf("arr.end.%d", lbl),
			})
			defer func() {
				g.loopExits = g.loopExits[:len(g.loopExits)-1]
			}()

			// Initialize i = 0
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))

			// br → cond
			sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))

			// cond block: i < viewLen
			g.emitLabel(sb, fmt.Sprintf("arr.cond.%d", lbl))
			g.indentLevel++
			g.tmpIdx++
			iLoad := fmt.Sprintf("%%arr.i.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%arr.cmp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, view.viewLen))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%arr.body.%d, label %%arr.end.%d\n", g.indent(), cmpReg, lbl, lbl))
			g.indentLevel--

			// body block
			g.emitLabel(sb, fmt.Sprintf("arr.body.%d", lbl))
			g.indentLevel++

			// Load element from view data[i] using adjusted data pointer
			g.tmpIdx++
			castReg := fmt.Sprintf("%%arr.cast.%d", g.tmpIdx)
			ptrType := elemType + "*"
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s\n", g.indent(), castReg, view.dataPtrReg, ptrType))

			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%arr.elem.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s %s, i64 %s\n",
				g.indent(), elemGEP, elemType, ptrType, castReg, iLoad))

			g.tmpIdx++
			elemLoad := fmt.Sprintf("%%arr.elem.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), elemLoad, elemType, ptrType, elemGEP))

			// Store element into loop variable
			g.varTypes[varName] = elemType
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %%%s\n", g.indent(), elemType, elemLoad, ptrType, varName))

			// Generate body statements
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
			// body 未終止時跳到 step 執行更新
			if !g.blockTerminated {
				sb.WriteString(fmt.Sprintf("%sbr label %%arr.step.%d\n", g.indent(), lbl))
			}
			g.blockTerminated = false
			g.indentLevel--

			// step block: i++（continue 跳轉目標）
			g.emitLabel(sb, fmt.Sprintf("arr.step.%d", lbl))
			g.indentLevel++
			// Increment i
			g.tmpIdx++
			iLoad2 := fmt.Sprintf("%%arr.i2.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
			g.tmpIdx++
			iInc := fmt.Sprintf("%%arr.inc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iInc, iLoad2))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInc, varName))
			sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
			g.indentLevel--

			// end block
			g.emitLabel(sb, fmt.Sprintf("arr.end.%d", lbl))
			return
		}

		structType = g.varTypes[identName]
		isVec = structType == "%vec"
		if structType == "" {
			structType = "%arr"
		}
		// 使用 varAddr 以正確處理全域變數（@name）vs 局部變數（%name）。
		// 直接拼 "%%%s" 會把全域變數誤當作局部，導致 LLVM「undefined value」錯誤。
		structPtr = g.varAddr(identName)

		// Get element type
		elemType = "i64"
		if et, ok := g.arrayElemTypes[identName]; ok {
			elemType = et
		}
	} else if sliceLit, ok := ir.RangeExpr.(*parser.SliceLiteral); ok {
		// Inline slice literal: for i in [1, 2, 3]
		structType = "%vec"
		isVec = true
		elemType = "i64"

		g.tmpIdx++
		tid := g.tmpIdx
		tmpVec := fmt.Sprintf("%%vec.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), tmpVec))

		n := int64(len(sliceLit.Elements))

		// alloca temp array
		arrType := fmt.Sprintf("[%d x %s]", n, elemType)
		tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))

		// store elements via GEP
		for i, elem := range sliceLit.Elements {
			ev := g.generateExprWithSB(sb, elem)
			ev = g.stripLLVMType(ev)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%slice.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), gepReg, arrType, arrType, tmpArr, i))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
		}

		// bitcast to i8*
		g.tmpIdx++
		ptrReg := fmt.Sprintf("%%slice.ptr.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))

		// store len (field 0)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%vec.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

		// store cap (field 1)
		g.tmpIdx++
		capGEP := fmt.Sprintf("%%vec.cap.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
			g.indent(), capGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

		// store data (field 2)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%vec.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, tmpVec))
		g.storeDataPtrField(sb, ptrReg, dataGEP)

		structPtr = tmpVec
	}

	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("arr.step.%d", lbl),
		exit: fmt.Sprintf("arr.end.%d", lbl),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// Load len (field 0 for both %arr and %vec)
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%arr.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%arr.len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, structType, structType, structPtr))
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))

	// Initialize i = 0
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))

	// cond block: i < len
	g.emitLabel(sb, fmt.Sprintf("arr.cond.%d", lbl))
	g.indentLevel++
	g.tmpIdx++
	iLoad := fmt.Sprintf("%%arr.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%arr.cmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, lenLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%arr.body.%d, label %%arr.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	g.indentLevel--

	// body block
	g.emitLabel(sb, fmt.Sprintf("arr.body.%d", lbl))
	g.indentLevel++

	// Load element from data[i]
	// Data field index: %arr → field 1, %vec → field 2
	dataField := uint32(1)
	if isVec {
		dataField = 2
	}
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataLoad := fmt.Sprintf("%%arr.data.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, structType, structType, structPtr, dataField))
	dataLoad = g.loadDataPtrField(sb, dataGEP)

	// Bitcast data to element type pointer
	g.tmpIdx++
	castReg := fmt.Sprintf("%%arr.cast.%d", g.tmpIdx)
	ptrType := elemType + "*"
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s\n", g.indent(), castReg, dataLoad, ptrType))

	// GEP into element array by index
	g.tmpIdx++
	elemGEP := fmt.Sprintf("%%arr.elem.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s %s, i64 %s\n",
		g.indent(), elemGEP, elemType, ptrType, castReg, iLoad))

	// Load element value
	g.tmpIdx++
	elemLoad := fmt.Sprintf("%%arr.elem.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), elemLoad, elemType, ptrType, elemGEP))

	// Store element into loop variable
	g.varTypes[varName] = elemType
	ptr2 := elemType + "*"
	sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %%%s\n", g.indent(), elemType, elemLoad, ptr2, varName))

	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// body 未終止時跳到 step 執行更新
	if !g.blockTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%arr.step.%d\n", g.indent(), lbl))
	}
	g.blockTerminated = false
	g.indentLevel--

	// step block: i++（continue 跳轉目標）
	g.emitLabel(sb, fmt.Sprintf("arr.step.%d", lbl))
	g.indentLevel++
	// Update: i++
	g.tmpIdx++
	iNext := fmt.Sprintf("%%arr.next.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	// end block
	g.emitLabel(sb, fmt.Sprintf("arr.end.%d", lbl))
}

func (g *Generator) generateRangeFor(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	// 字串遍歷: for i in 'hello'
	if ir.RangeStr != "" {
		g.generateStringRange(sb, stmt)
		return
	}

	// 陣列/切片遍歷: for i in a
	if ir.RangeExpr != nil {
		g.generateArrayRange(sb, stmt)
		return
	}

	r := ir.Range
	varName := ir.Variable
	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("rng.step.%d", lbl),
		exit: fmt.Sprintf("rng.end.%d", lbl),
	})
	// Track the loop variable's upper bound for bounds check elimination.
	// Save the old bound and restore it after the loop body (handles nested loops
	// and variable name reuse across functions).
	oldBound, hadOldBound := int64(0), false
	if g.rangeLoopBounds != nil {
		if old, ok := g.rangeLoopBounds[varName]; ok {
			oldBound = old
			hadOldBound = true
		}
		// Record the new bound if the end is a constant.
		if endLit, ok := r.End.(*parser.IntegerLiteral); ok {
			if r.RightInc {
				g.rangeLoopBounds[varName] = endLit.Value + 1
			} else {
				g.rangeLoopBounds[varName] = endLit.Value
			}
		} else {
			// Non-constant end: remove any stale bound so we don't make
			// incorrect assumptions.
			delete(g.rangeLoopBounds, varName)
		}
	}
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
		// Restore the old bound
		if g.rangeLoopBounds != nil {
			if hadOldBound {
				g.rangeLoopBounds[varName] = oldBound
			} else {
				delete(g.rangeLoopBounds, varName)
			}
		}
	}()

	// 計算 start 和 end 值
	startVal := g.generateExprWithSB(sb, r.Start)
	endVal := g.generateExprWithSB(sb, r.End)

	// Check if both start and end are compile-time constants.
	// If so, the loop direction is known at compile time and we can
	// generate a simple loop with NO per-iteration select instructions.
	startLit, startIsLit := r.Start.(*parser.IntegerLiteral)
	endLit, endIsLit := r.End.(*parser.IntegerLiteral)

	if startIsLit && endIsLit {
		// ================================================================
		// Fast path: both bounds are constants — direction known at compile time.
		// Generates a simple loop with NO select instructions:
		//   cond: single icmp (no select)
		//   step: single add/sub (no select)
		// ================================================================
		ascending := startLit.Value <= endLit.Value

		// Compute initial value: start (left-closed) or start±1 (left-open)
		var iInitVal string
		if r.LeftInc {
			iInitVal = startVal
		} else {
			g.tmpIdx++
			iInitReg := fmt.Sprintf("%%rng.init.%d", g.tmpIdx)
			if ascending {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iInitReg, startVal))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), iInitReg, startVal))
			}
			iInitVal = iInitReg
		}
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInitVal, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))

		// cond block: single comparison (no select!)
		g.emitLabel(sb, fmt.Sprintf("rng.cond.%d", lbl))
		g.indentLevel++
		g.tmpIdx++
		iLoad := fmt.Sprintf("%%rng.i.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%rng.cmp.%d", g.tmpIdx)
		if ascending {
			cmpOp := "sle"
			if !r.RightInc {
				cmpOp = "slt"
			}
			sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, iLoad, endVal))
		} else {
			cmpOp := "sge"
			if !r.RightInc {
				cmpOp = "sgt"
			}
			sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, iLoad, endVal))
		}
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), cmpReg, lbl, lbl))
		g.indentLevel--

		// body block
		g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		if !g.blockTerminated {
			sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: simple increment or decrement (no select!)
		g.emitLabel(sb, fmt.Sprintf("rng.step.%d", lbl))
		g.indentLevel++
		g.tmpIdx++
		iLoad2 := fmt.Sprintf("%%rng.i2.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
		g.tmpIdx++
		iNext := fmt.Sprintf("%%rng.next.%d", g.tmpIdx)
		if ascending {
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad2))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), iNext, iLoad2))
		}
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
		return
	}

	// ================================================================
	// Fast path: start is literal 0, end is non-constant.
	// This is the most common pattern: `i <- [0..n)` for array iteration.
	//
	// When start=0 and the range is right-exclusive `[0..end)`:
	//   - If end >= 0: ascending (0, 1, ..., end-1) — correct
	//   - If end < 0: condition `0 < end` is false, loop doesn't execute
	//     (intuitive behavior — no iterations for negative count)
	//
	// This generates a simple ascending-only loop with NO select and NO
	// per-iteration branch on direction, matching C/Rust for-loop performance.
	// ================================================================
	if startIsLit && startLit.Value == 0 && r.LeftInc && !r.RightInc {
		// i = 0 (left-inclusive)
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))

		// cond block: i < end (ascending only, single icmp)
		g.emitLabel(sb, fmt.Sprintf("rng.cond.%d", lbl))
		g.indentLevel++
		g.tmpIdx++
		iLoad := fmt.Sprintf("%%rng.i.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%rng.cmp.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, endVal))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), cmpReg, lbl, lbl))
		g.indentLevel--

		// body block
		g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		if !g.blockTerminated {
			sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: i = i + 1 (simple increment)
		g.emitLabel(sb, fmt.Sprintf("rng.step.%d", lbl))
		g.indentLevel++
		g.tmpIdx++
		iLoad2 := fmt.Sprintf("%%rng.i2.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
		g.tmpIdx++
		iNext := fmt.Sprintf("%%rng.next.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad2))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
		return
	}

	// ================================================================
	// General path: at least one bound is non-constant.
	//
	// Uses a single condition block with `select` for the comparison
	// instead of separate ascending/descending blocks with a per-iteration
	// branch. This reduces the loop to 3 blocks (cond, body, step) and
	// eliminates the loop-invariant `br dirCmp` back-edge branch.
	//
	// On ARM64, `select` compiles to a single `CSEL` instruction, and the
	// two `icmp` instructions can be computed in parallel. The eliminated
	// branch — even though perfectly predicted — was preventing LLVM from
	// performing loop unswitching and other loop optimizations because the
	// loop had multiple exits (break statements in the body).
	//
	// Per-iteration cost: 1 load + 2 icmp + 1 select + 1 br (cond)
	//                       1 load + 1 add + 1 store + 1 br (back-edge)
	// ================================================================

	// 計算方向標誌：start <= end 為遞增 (true)，start > end 為遞減 (false)
	g.tmpIdx++
	dirCmp := fmt.Sprintf("%%rng.dircmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp sle i64 %s, %s\n", g.indent(), dirCmp, startVal, endVal))

	// Pre-compute step: 1 for ascending, -1 for descending (computed once)
	g.tmpIdx++
	stepReg := fmt.Sprintf("%%rng.stepval.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 1, i64 -1\n", g.indent(), stepReg, dirCmp))

	// 計算初始值 i = start（左閉）或 start+step（左開）
	g.tmpIdx++
	iInit := fmt.Sprintf("%%rng.init.%d", g.tmpIdx)
	if r.LeftInc {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 0\n", g.indent(), iInit, startVal))
	} else {
		// 左開：i = start + step (step is 1 for ascending, -1 for descending)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), iInit, startVal, stepReg))
	}
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInit, varName))

	// Single condition block: uses select to choose asc/desc comparison.
	// No per-iteration branch on dirCmp needed.
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))

	g.emitLabel(sb, fmt.Sprintf("rng.cond.%d", lbl))
	g.indentLevel++
	g.tmpIdx++
	condILoad := fmt.Sprintf("%%rng.cond.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), condILoad, varName))
	// Ascending comparison: i < end (or i <= end if right-inclusive)
	g.tmpIdx++
	ascCmp := fmt.Sprintf("%%rng.asc.cmp.%d", g.tmpIdx)
	ascOp := "sle"
	if !r.RightInc {
		ascOp = "slt"
	}
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), ascCmp, ascOp, condILoad, endVal))
	// Descending comparison: i > end (or i >= end if right-inclusive)
	g.tmpIdx++
	descCmp := fmt.Sprintf("%%rng.desc.cmp.%d", g.tmpIdx)
	descOp := "sge"
	if !r.RightInc {
		descOp = "sgt"
	}
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), descCmp, descOp, condILoad, endVal))
	// Select the appropriate comparison result based on direction
	g.tmpIdx++
	condResult := fmt.Sprintf("%%rng.cond.result.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i1 %s, i1 %s\n", g.indent(), condResult, dirCmp, ascCmp, descCmp))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), condResult, lbl, lbl))
	g.indentLevel--

	// body block
	g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
	g.indentLevel++
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// body 未終止時跳到 step 執行更新
	if !g.blockTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
	}
	g.blockTerminated = false
	g.indentLevel--

	// step block: i = i + step (single add, no branch on dirCmp!)
	// continue 跳轉目標
	g.emitLabel(sb, fmt.Sprintf("rng.step.%d", lbl))
	g.indentLevel++
	g.tmpIdx++
	stepILoad := fmt.Sprintf("%%rng.step.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), stepILoad, varName))
	g.tmpIdx++
	iNext := fmt.Sprintf("%%rng.next.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), iNext, stepILoad, stepReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	// Back-edge: directly to cond block (no dirCmp branch!)
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	// end block
	g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
}

func (g *Generator) generateLet(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// 處理 match 對應 err/nil arm 注入的合成 let 陳述句（`it = matched`）。
	// 這些 let 的 Type 為 "err" / "nil" / "err | nil" 哨兵字串，無法映射到真實的 LLVM 型別。
	// 為了避免嘗試將 ?file option 的內部 struct 指標存入 i64* 而產生型別衝突，
	// 將其型別降為 i64 並直接賦值 0 作為佔位符（`it` 在 err/nil 分支語意上無值）。
	if stmt.IsSynthetic {
		if nt, ok := stmt.Type.(*parser.NamedType); ok {
			// 判斷型別字串是否僅由 err/nil 組成（如 "err"、"nil"、"err | nil"），
			// 不含任何具體元素型別（如 "[]byte | err" 仍需綁定 it = inner value）。
			onlyErrNil := true
			for _, p := range strings.Split(nt.Value, "|") {
				p = strings.TrimSpace(p)
				if p == "err" || p == "nil" || p == "" {
					continue
				}
				onlyErrNil = false
				break
			}
			if onlyErrNil {
				if g.varTypes != nil {
					if _, exists := g.varTypes[name]; !exists {
						g.varTypes[name] = "i64"
						g.funcLocalNames[name] = true
						sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), llvmVarRef(name)))
						sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %s)\n", g.indent(), llvmVarRef(name)))
						sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), llvmVarRef(name)))
					}
				}
				return
			}
		}
	}

	// oldValFreed 標記舊值是否已釋放。
	// 對於 RHS 安全（不引用舊值）的型別特定分支（SliceLiteral），在進入分支前立即釋放舊值。
	// 對於一般情況，在 RHS 求值後釋放舊值，避免 `x = f(x)` 造成 use-after-free。
	oldValFreed := false

	// 堆變數 moved 追蹤：若目標是輸出參數且源是局部堆變數，走 move（所有權轉移）。
	// move 不 free out 舊值（舊值可能與前一個 source 共享 data），
	// 設 oldValFreed=true 跳過通用路徑的 freeOldHeapValue。
	// 非 move 的 out 賦值（表達式）由通用路徑 freeOldHeapValue 釋放舊值。
	if g.heapVars != nil && g.outputParamNames != nil && g.outputParamNames[name] && g.heapVarIndex != nil && !stmt.IsSynthetic {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if _, isHeap := g.heapVars[ident.Value]; isHeap {
				g.handleMoveToOut(sb, ident.Value, name)
				oldValFreed = true
			}
		}
	}

	// 切片視圖賦值：view = arr[0..4]
	// 註冊為 slice view alias，不創建獨立結構體，通過 offset 計算訪問原始數據
	// 注意：不自動釋放舊值，因為 generateSliceViewAssignment 可能建立引用舊值的視圖（自切片）。
	// 若 generateSliceViewAssignment 未處理則回退到一般路徑，在那裡釋放舊值。
	if _, isSliceExpr := stmt.Value.(*parser.SliceExpression); isSliceExpr {
		if g.generateSliceViewAssignment(sb, stmt, name) {
			return
		}
		// 若 generateSliceViewAssignment 無法處理（如 base 不是 Identifier），
		// 則回退到原本的 generateSliceExpression 路徑
	}

	// 切片視圖變數賦值給輸出參數或顯式 []T 型別：out = view
	// view 是 slice view alias（RHS 是 Identifier 而非 SliceExpression），
	// 繞過 generateSliceViewAssignment 的 clone 保護（該函數只處理 SliceExpression RHS）。
	// 若目標會逃逸函數作用域，必須 clone view 的 data，
	// 避免共享原數組 data 導致原數組 free 後 out 懸空（use-after-free）。
	if ident, ok := stmt.Value.(*parser.Identifier); ok {
		if g.isSliceViewVar(ident.Value) {
			needClone := false
			if g.outputParamNames != nil && g.outputParamNames[name] {
				needClone = true
			}
			if _, isSliceType := stmt.Type.(*parser.SliceType); isSliceType {
				needClone = true
			}
			if needClone {
				view := g.sliceViews[ident.Value]
				// 釋放目標變數的舊堆值（如有）
				g.freeOldHeapValue(sb, stmt, name)
				// 計算 elemSize
				elemSize := int64(8)
				if view.isStr {
					elemSize = 1
				} else {
					if s := g.llvmTypeSize(view.elemType); s > 0 {
						elemSize = s
					}
				}
				// clone view data 到目標變數（malloc + memcpy + store len/cap/data）
				g.emitSliceClone(sb, name, view.dataPtrReg, view.viewLen, view.elemType, elemSize, view.isStr)
				// 追蹤目標為堆變數（僅局部變數，非輸出參數；輸出參數由呼叫者管理）
				resultType := "%vec"
				if view.isStr {
					resultType = "%str-long"
				}
				if g.outputParamNames == nil || !g.outputParamNames[name] {
					g.trackLocalHeapVar(name, resultType)
				}
				return
			}
		}
	}

	// 切片儲存：使用 %vec 結構體
	_, isSliceLit := stmt.Value.(*parser.SliceLiteral)
	_, isSliceExpr2 := stmt.Value.(*parser.SliceExpression)
	_, isSliceType := stmt.Type.(*parser.SliceType)
	// Skip default slice init when using with-cap/with-len/with-cap-len builtin (type-inferred allocation)
	isWithCapCall := false
	if call, ok := stmt.Value.(*parser.CallExpression); ok {
		if ident, ok := call.Function.(*parser.Identifier); ok && (ident.Value == "with-cap" || ident.Value == "with-len" || ident.Value == "with-cap-len") {
			isWithCapCall = true
		}
	}
	if (isSliceType || isSliceLit) && !isSliceExpr2 && !isWithCapCall {
		// SliceLiteral 的 RHS 是字面量，不引用舊值，可安全在求值前釋放舊值
		if !oldValFreed && isSliceLit {
			g.freeOldHeapValue(sb, stmt, name)
			oldValFreed = true
		}
		// 若變數原本宣告為 %arr（固定大小陣列，alloca 16 字節），但被 SliceLiteral
		// 重新賦值為 %vec（24 字節，3 個欄位），原本的 alloca 空間不足，寫入 field 2
		// 會越界。此時 alloca 一個新的 %vec 變數並通過 varAlias 重定向所有後續存取。
		if g.varTypes != nil && g.varTypes[name] == "%arr" {
			g.tmpIdx++
			vecVarName := fmt.Sprintf("%s.vec.%d", name, g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), llvmVarRef(vecVarName)))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 24, i8* %s)\n", g.indent(), llvmVarRef(vecVarName)))
			g.varAlias[name] = vecVarName
			g.funcLocalNames[vecVarName] = true
			// 從 stackArrVars 移除（不再使用棧陣列路徑）
			if g.stackArrVars != nil {
				delete(g.stackArrVars, name)
			}
		}
		// 註冊變數型別為 %vec，供後續索引賦值/讀取/指標取得使用
		g.varTypes[name] = "%vec"
		// Only mark as local if not already a global variable
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		// 記錄切片元素型別，供 IndexExpression 使用正確型別讀取
		if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.arrayElemTypes[name] = g.mapToLLVMType(st.Elem.String())
		} else {
			g.arrayElemTypes[name] = "i64"
		}
		if isSliceLit {
			slice := stmt.Value.(*parser.SliceLiteral)
			elemType := "i64"
			if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
				elemType = g.mapToLLVMType(st.Elem.String())
			}
			n := int64(len(slice.Elements))
			g.tmpIdx++
			tid := g.tmpIdx
			tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
			arrType := fmt.Sprintf("[%d x %s]", n, elemType)

			// malloc temp array on heap（而非 alloca 棧分配），
			// 因為 %vec 擁有 data 並在函數結束時 free，棧指標會導致 crash。
			elemSize := g.llvmTypeSize(elemType)
			if elemSize == 0 {
				elemSize = 8
			}
			bufSize := n * elemSize
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), tmpArr, bufSize))

			// bitcast i8* to array type pointer for element GEP
			g.tmpIdx++
			arrPtr := fmt.Sprintf("%%slice.arrptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), arrPtr, tmpArr, arrType))

			// store each element via GEP
			for i, elem := range slice.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				g.tmpIdx++
				gepReg := fmt.Sprintf("%%slice.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), gepReg, arrType, arrType, arrPtr, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
			}

			// tmpArr is already i8* (from malloc), use directly as vec data
			ptrReg := tmpArr

			// store len (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%vec.len.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
				g.indent(), lenGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

			// store cap (field 1)
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%vec.cap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
				g.indent(), capGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

			// store data (field 2)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vec.data.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
				g.indent(), dataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, ptrReg, dataGEP)
			// 追蹤局部 vec 變數，用於函數結束時深層 free
			g.trackLocalHeapVar(name, "%vec")
			return
		}

		// Non-literal slice declaration (e.g. `lines []str`): initialize %vec
		// with a default data buffer so push() and index assignment work.
		// 僅當沒有 RHS 值時才執行預設初始化。若有 RHS 值（如 `v []i64 = view`），
		// 必須 fall through 到深層 clone / 一般賦值路徑，避免忽略 RHS 導致 len=0。
		if stmt.Value == nil {
			elemType := "i64"
			if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
				elemType = g.mapToLLVMType(st.Elem.String())
			}
			elemSize := g.llvmTypeSize(elemType)
			if elemSize == 0 {
				elemSize = 8
			}
			defaultCap := int64(1024)
			bufSize := defaultCap * elemSize
			g.tmpIdx++
			vecBuf := fmt.Sprintf("%%vec.init.buf.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), vecBuf, bufSize))
			// store len = 0 (field 0)
			g.tmpIdx++
			vecLenGEP := fmt.Sprintf("%%vec.init.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), vecLenGEP))
			// store cap = defaultCap (field 1)
			g.tmpIdx++
			vecCapGEP := fmt.Sprintf("%%vec.init.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), defaultCap, vecCapGEP))
			// store data = buf (field 2)
			g.tmpIdx++
			vecDataGEP := fmt.Sprintf("%%vec.init.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, vecBuf, vecDataGEP)
			// 追蹤局部 vec 變數，用於函數結束時深層 free
			g.trackLocalHeapVar(name, "%vec")
			return
		}
		// 有 RHS 值但非 SliceLiteral：型別註冊已在上方完成，
		// fall through 到深層 clone / 一般賦值路徑評估 RHS。
	}

	// Option type assignment: handle nil, val(), err(), and implicit values
	llvmTypeCheck := g.varLLVMType(stmt)
	// Also check if variable already has %option type (reassignment)
	if llvmTypeCheck != "%option" && g.varTypes != nil {
		if t, ok := g.varTypes[stmt.Name.Value]; ok && t == "%option" {
			llvmTypeCheck = "%option"
		}
	}
	// 若變數有明確非 option 型別（如函數輸出參數 fd i64），
	// 優先使用變數型別而非從 funcRetTypes 推斷的 %option。
	// 解決 fs.no 中 open-write-raw 內 fd = open-write(p) 場景：
	// varLLVMType 從 funcRetTypes 推斷為 %option，但 fd 是 i64，實際 dispatch 走 builtin 返回 i64。
	// 但 synthetic `it` 綁定（來自 match desugar）不在此列：
	// it 的值來自 option 變數，必須走 option 賦值路徑，不能被 nil arm 的 i64 佔位符覆蓋。
	if llvmTypeCheck == "%option" && g.varTypes != nil && !stmt.IsSynthetic {
		if t, ok := g.varTypes[stmt.Name.Value]; ok && t != "" && t != "%option" {
			llvmTypeCheck = t
		}
	}
	if llvmTypeCheck == "%option" {
		// Ensure variable is allocated (needed for `it = x` injected in match arm bodies)
		if g.varTypes != nil {
			if _, exists := g.varTypes[name]; !exists {
				g.varTypes[name] = "%option"
				g.funcLocalNames[name] = true
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%option\n", g.indent(), llvmVarRef(name)))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 16, i8* %s)\n", g.indent(), llvmVarRef(name)))
			}
		}
		// Track inner type for ?T option variables
		if g.optionInnerTypes != nil {
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From explicit type annotation (e.g. val ?f64)
				if nt, ok := stmt.Type.(*parser.NullableType); ok {
					g.optionInnerTypes[name] = g.mapToLLVMType(nt.Type.String())
				}
			}
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From option variable assignment (e.g. it = n where n is ?i64)
				if ident, ok := stmt.Value.(*parser.Identifier); ok {
					if srcInner, ok := g.optionInnerTypes[ident.Value]; ok && srcInner != "" {
						g.optionInnerTypes[name] = srcInner
					}
				}
			}
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From function call return type (e.g. f = '3.14'.to-f64())
				if call, ok := stmt.Value.(*parser.CallExpression); ok {
					fnName := ""
					if ident, ok := call.Function.(*parser.Identifier); ok {
						fnName = ident.Value
					} else if dot, ok := call.Function.(*parser.DotExpression); ok {
						if recv, ok := dot.Receiver.(*parser.Identifier); ok {
							if recvType, ok := g.varTypes[recv.Value]; ok {
								srcType := strings.TrimPrefix(recvType, "%")
								fnName = srcType + "." + dot.Property
							}
						}
						// Also try string literal receiver
						if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
							fnName = "str." + dot.Property
						}
					}
					if fnName != "" && g.funcResultInnerTypes != nil {
						if innerTypes, ok := g.funcResultInnerTypes[fnName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
							g.optionInnerTypes[name] = innerTypes[0]
						}
					}
				}
			}
		}
		g.generateOptionAssign(sb, stmt)
		return
	}

	// Determine target type for type-inferred builtins (e.g. with-cap)
	g.currentTargetType = ""
	g.currentTargetElemType = ""
	if stmt.Type != nil {
		g.currentTargetType = g.mapToLLVMType(stmt.Type.String())
		if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.currentTargetElemType = g.mapToLLVMType(st.Elem.String())
		}
	} else if g.varTypes != nil {
		if t, ok := g.varTypes[name]; ok {
			g.currentTargetType = t
		}
	}
	// Fallback: look up element type from arrayElemTypes (for inferred assignments)
	if g.currentTargetElemType == "" && g.arrayElemTypes != nil {
		if et, ok := g.arrayElemTypes[name]; ok {
			g.currentTargetElemType = et
		}
	}

	// For with-cap/with-len/with-cap-len with slice type: register element type
	// (skipped the default slice init path, so need manual element type registration)
	// Note: alloca is already done during variable declaration collection phase.
	if isWithCapCall && isSliceType {
		if g.currentTargetElemType != "" {
			g.arrayElemTypes[name] = g.currentTargetElemType
		} else {
			g.arrayElemTypes[name] = "i64"
		}
	}

	// 深層 clone 路徑：b = a，其中 a 是堆擁有型別的局部變數。
	// 不使用淺拷貝 + move（會泄漏或 double-free），而是深層 clone a 的堆數據到 b，
	// 使 a 和 b 各自獨立擁有 data，函數結束時各自 free。
	// 巢狀容器（%vec/%arr 元素為 %vec/%arr）因子元素型別未知，使用 move 代替。
	if g.heapVars != nil && !stmt.IsSynthetic {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if srcHeapType, isHeap := g.heapVars[ident.Value]; isHeap {
				if ident.Value != name {
					_, isLocal := g.funcLocalNames[name]
					isOutput := g.outputParamNames != nil && g.outputParamNames[name]
					if isLocal && !isOutput {
						srcElemType := ""
						if g.arrayElemTypes != nil {
							srcElemType = g.arrayElemTypes[ident.Value]
						}
						// 檢查是否可以安全深層 clone
						canClone := true
						if (srcHeapType == "%vec" || srcHeapType == "%arr") &&
							(srcElemType == "%vec" || srcElemType == "%arr") {
							canClone = false // 巢狀容器，子元素型別未知
						}
						if srcHeapType != "%vec" && srcHeapType != "%arr" && srcHeapType != "%str-long" {
							if !g.canDeepCloneStruct(srcHeapType) {
								canClone = false
							}
						}
						if canClone {
							// 釋放目標變數的舊堆值（如有）
							g.freeOldHeapValue(sb, stmt, name)
							// 深層 clone
							g.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcHeapType, srcElemType)
							// 追蹤目標變數為堆變數
							g.trackLocalHeapVar(name, srcHeapType)
							// 傳播 elemType
							if srcElemType != "" && g.arrayElemTypes != nil {
								g.arrayElemTypes[name] = srcElemType
							}
							return
						}
					}
				}
			}
		}
	}

	val := g.generateExprWithSB(sb, stmt.Value)
	val = g.stripLLVMType(val)

	// 一般情況：在 RHS 求值後釋放舊值，避免 `x = f(x)` 造成 use-after-free。
	// 型別特定分支（SliceLiteral）已在進入分支前釋放並設置 oldValFreed。
	if !oldValFreed {
		g.freeOldHeapValue(sb, stmt, name)
	}
	// 在清除 currentTargetType 前保存值的實際型別，
	// 供後續賦值時的型別轉換使用（zext/trunc）。
	valActualType := ""
	if strings.HasPrefix(val, "%") {
		valActualType = g.intExprLLVMType(stmt.Value)
	}
	llvmType := g.varLLVMType(stmt)
	g.currentTargetType = ""     // clear after use
	g.currentTargetElemType = "" // clear after use
	// For assignments (Type=nil) to %arr variables, varLLVMType may return
	// the function's raw result type (e.g. [16 x i8]) instead of %arr.
	// Override with the variable's declared type so the switch matches %arr.
	if stmt.Type == nil && g.varTypes != nil {
		if t, ok := g.varTypes[name]; ok && t == "%arr" {
			llvmType = "%arr"
		}
	}

	// Empty string assignment optimization: s = "" → set len=0 in-place, no storage switch
	// str-long: store i64 0 to field 0, cap/ptr unchanged
	if llvmType == "%str-long" {
		if strLit, ok := stmt.Value.(*parser.StringLiteral); ok && len(strLit.Value) == 0 {
			storeAddr := g.varAddr(name)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%sc.len.gep.%d", g.tmpIdx)
			// str-long: field 0 is i64 len
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, storeAddr))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
			}
			return
		}
	}

	// Synthetic `it = matched` for `ok` arm of a match on ?T:
	// matched is an option variable whose inner type is a struct
	// (e.g. ?file), and the target `it` is the inner struct type.
	// generateExprWithSB returns a pointer to the option's data field
	// (bitcast to inner struct pointer) for struct inner types.
	// We need to load the struct value before storing into `it`.
	// Additionally, `it` may be allocated with a different struct type
	// (e.g. %client) when shared across multiple matches with different
	// element types (e.g. ?str → %str-long, ?client → %client).
	// In that case, bitcast the alloca pointer to the value's type.
	if stmt.IsSynthetic && strings.HasPrefix(llvmType, "%") {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if srcType, ok := g.varTypes[ident.Value]; ok && srcType == "%option" {
					if strings.HasPrefix(val, "%") {
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%it.syn.load.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, llvmType, llvmType, val))
						val = loadReg
					}
				}
			}
		}
	}

	// Synthetic `it = matched` where `it` was pre-allocated as i64 (from a
	// nil/err arm) but the matched value is a struct pointer extracted from
	// an option (generateExprWithSB returns %var.data.ptr.N via inttoptr).
	// The pointer must be converted back to i64 via ptrtoint before storing
	// into the i64 alloca. Without this, LLVM reports:
	//   '%var.data.ptr.N' defined with type 'ptr' but expected 'i64'
	if stmt.IsSynthetic && llvmType == "i64" {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if srcType, ok := g.varTypes[ident.Value]; ok && srcType == "%option" {
					if g.optionInnerTypes != nil {
						if innerType, ok := g.optionInnerTypes[ident.Value]; ok && strings.HasPrefix(innerType, "%") {
							if strings.HasPrefix(val, "%") && strings.Contains(val, ".data.ptr.") {
								g.tmpIdx++
								ptrToIntReg := fmt.Sprintf("%%it.p2i.%d", g.tmpIdx)
								sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s* %s to i64\n", g.indent(), ptrToIntReg, innerType, val))
								val = ptrToIntReg
							}
						}
					}
				}
			}
		}
	}

	// 若變數已有整數型別（如函數輸出參數 yes bool 為 i1），
	// 但值是字面常量（如 true → "1"），使用變數的型別以避免型別不匹配
	// （例如 store i64 1, i64* %yes 但 %yes 是 alloca i1）。
	if existingType, ok := g.varTypes[name]; ok && g.isIntegerLLVMType(existingType) && g.isIntegerLLVMType(llvmType) {
		if !strings.HasPrefix(val, "%") {
			llvmType = existingType
		}
	}

	// 函數呼叫（用結果參數模式）會被 generateExpr 寫成 call void @...，
	// 並返回空字串表示沒有 SSA 值。對於這種情況，result param
	// 已經被函數就地修改，不需要再 store。
	if _, isCall := stmt.Value.(*parser.CallExpression); isCall && val == "" {
		return
	}

	// 結構體儲存
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		structName := sl.Type
		fields := g.structTypes[structName]
		structTy := "%" + structName
		// Ensure variable is allocated (needed for top-level LetStatements in main function)
		if _, exists := g.funcLocalNames[name]; !exists {
			if _, isGlobal := g.globalVars[name]; !isGlobal {
				g.varTypes[name] = structTy
				g.funcLocalNames[name] = true
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(name), structTy))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 0, i8* %s)\n", g.indent(), llvmVarRef(name)))
			}
		}
		// 先將整個結構體初始化為 zeroinitializer，避免未指定的欄位帶有 stack 殘值
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), structTy, structTy, g.varAddr(name)))
		// 建立欄位名稱 → 索引映射
		fieldIndexByName := make(map[string]int)
		for i, f := range fields {
			fieldIndexByName[f.name] = i
		}
		// 記錄哪些欄位已由 literal 明確設定
		setFields := make(map[int]bool)
		// 逐欄位 store（按欄位名稱查找定義中的索引）
		for _, f := range sl.Fields {
			fieldIdx := -1
			fieldType := "i64"
			if idx, ok := fieldIndexByName[f.Name]; ok {
				fieldIdx = idx
				fieldType = fields[idx].typ
			}
			if fieldIdx < 0 {
				continue
			}
			setFields[fieldIdx] = true
			// %vec 欄位且值為 SliceLiteral：執行完整切片初始化
			//（alloca 臨時數組 + 逐元素 store + 構造 %vec {len, cap, data}）
			if fieldType == "%vec" {
				if sliceLit, ok := f.Value.(*parser.SliceLiteral); ok {
					g.initVecFieldFromSliceLiteral(sb, name, structTy, fieldIdx, sliceLit, fields[fieldIdx].elemType)
					continue
				}
			}
			fieldVal := g.generateExprWithSB(sb, f.Value)
			fieldVal = g.stripLLVMType(fieldVal)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%st.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), gepReg, structTy, structTy, g.varAddr(name), fieldIdx))
			// 處理欄位型別與值的型別不一致：
			//   1. 欄位型別為 struct/string 但值是純量（如整數字面量）：使用 zeroinitializer
			//   2. 欄位型別為 struct/string 且值是指標（如 StringLiteral 回傳的 alloca 指針）：
			//      需要先 load 出 struct 值再 store
			//   3. 欄位型別為 %str-long 但值是不同型別：先轉換為 %str-long 再 store
			if strings.HasPrefix(fieldType, "%") {
				if !strings.HasPrefix(fieldVal, "%") {
					// 純量值，無法轉為 struct：使用 zeroinitializer
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), fieldType, fieldType, gepReg))
				} else {
					// String literals and str pointer regs are %str-long* pointers (alloca),
					// need to load the %str-long value before storing.
					if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%st.fload.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, fieldType, fieldType, fieldVal))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, loadReg, fieldType, gepReg))
					} else {
						// 決定實際的 source str 型別
						sourceStrType := g.inferSourceStrType(f.Value)
						if sourceStrType == "" {
							// 非 str 值，直接 store（已是 struct 值）
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
						} else if sourceStrType == fieldType {
							// 同型別，直接 store
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
						} else {
							// 不同型別：先取得 source 指標，轉換為目標型別的指標，再 load + store
							sourcePtr := g.materializeStrPtr(sb, f.Value, sourceStrType, fieldVal)
							convertedPtr := sourcePtr
							g.tmpIdx++
							loadReg := fmt.Sprintf("%%st.fload.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, fieldType, fieldType, convertedPtr))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, loadReg, fieldType, gepReg))
						}
					}
				}
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
			}
		}
		// 為未明確設定的 %vec 欄位分配 data 緩衝區
		for i, f := range fields {
			if setFields[i] {
				continue
			}
			if f.typ != "%vec" {
				continue
			}
			vecCap := int64(256)
			elemSize := int64(8)
			if f.elemType != "" {
				elemSize = llvmTypeSize(f.elemType)
			}
			vecBufSize := vecCap * elemSize
			g.tmpIdx++
			dataBuf := fmt.Sprintf("%%st.let.vecdata.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%st.let.veccap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
				g.indent(), capGEP, structTy, structTy, g.varAddr(name), i))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%st.let.vecdataptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
				g.indent(), dataGEP, structTy, structTy, g.varAddr(name), i))
			g.storeDataPtrField(sb, dataBuf, dataGEP)
		}
		return
	}

	// MapType: m [K]V = { k1:v1, k2:v2 } → alloca %hashmap-K-V + init() + put() calls
	// The alloca itself is emitted by generateFunctionDefinition's collection phase
	// (collectVarDeclsFromStmt → varLLVMType → mapToLLVMType(mt.LLVMName()) → "%hashmap-K-V").
	// Here we only emit the init() call and put() calls for each MapLiteral pair.
	if mt, ok := stmt.Type.(*parser.MapType); ok {
		llvmType := g.mapToLLVMType(mt.LLVMName()) // e.g. %hashmap-str-i64
		g.varTypes[name] = llvmType
		// 區分全局變數和局部變數：全局變數的 varAddr 需返回 @name（透過 globalVars），
		// 不能設 funcLocalNames（否則 varAddr 會返回未 alloca 的 %name）。
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		structName := strings.TrimPrefix(llvmType, "%")
		recvArg := llvmType + "* " + g.varAddr(name)
		// init() call: @hashmap-str-i64.init(%hashmap-str-i64* %m)
		sb.WriteString(fmt.Sprintf("%scall void @%s.init(%s)\n", g.indent(), sanitizeLLVMName(structName), recvArg))
		// If value is MapLiteral, generate put() calls for each pair
		if ml, ok := stmt.Value.(*parser.MapLiteral); ok {
			for _, pair := range ml.Pairs {
				keyArg := g.generateCallArg(sb, pair.Key)
				valArg := g.generateCallArg(sb, pair.Value)
				// is-new bool result param (discarded); map.no declares is-new as bool result
				g.tmpIdx++
				isNewTmp := fmt.Sprintf("%%map.isnew.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca i1\n", g.indent(), isNewTmp))
				sb.WriteString(fmt.Sprintf("%scall void @%s.put(%s, %s, %s, i1* %s)\n",
					g.indent(), sanitizeLLVMName(structName), recvArg, keyArg, valArg, isNewTmp))
			}
		}
		// 追蹤為堆變數：hashmap 內聯 [N]str 鍵等欄位持有堆數據，
		// 函數結束時由 emitHeapFree → emitVarHeapFree → emitStructFieldsFree 遞迴釋放。
		g.trackLocalHeapVar(name, llvmType)
		return
	}

	if at, ok := stmt.Type.(*parser.ArrayType); ok {
		// 註冊變數型別為 %arr，供後續索引賦值/讀取/指標取得使用
		g.varTypes[name] = "%arr"
		// 區分全局變數和局部變數：全局變數的 varAddr 需返回 @name（透過 globalVars），
		// 不能設 funcLocalNames（否則 varAddr 會返回未 alloca 的 %name）
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		var arraySize int64
		if at.Size != nil {
			if v, ok := g.constFoldInt(at.Size); ok {
				arraySize = v
			} else if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
				arraySize = intLit.Value
			}
		} else if arrLit, ok := stmt.Value.(*parser.ArrayLiteral); ok {
			if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
				arraySize = intLit.Value
			}
		}
		elemType := "i64"
		if at.Elem != nil {
			elemType = at.Elem.String()
		}
		llvmElemType := g.mapToLLVMType(elemType)
		elemSize := g.llvmTypeSize(llvmElemType)

		// Register element type and size for later index resolution and
		// %arr → [N x T]* argument conversion (genTypedArg / generateCallArg)
		g.arrayElemTypes[name] = llvmElemType
		if g.arraySizes != nil {
			g.arraySizes[name] = arraySize
		}

		// Store len field
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%arr.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, g.varAddr(name)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arraySize, lenGEP))

		// Allocate data buffer: arraySize * elemSize
		totalSize := arraySize * elemSize
		var dataReg string
		if g.stackArrVars != nil && g.stackArrVars[name] {
			// 棧分配陣列：prologue 已用 alloca 分配並存入 data 欄位，直接載入重用
			g.tmpIdx++
			reuseGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), reuseGEP, g.varAddr(name)))
			dataReg = g.loadDataPtrField(sb, reuseGEP)
		} else {
			g.tmpIdx++
			dataReg = fmt.Sprintf("%%arr.data.malloc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataReg, totalSize))

			// Store data pointer in struct
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), dataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, dataReg, dataGEP)
		}

		// Store elements from array literal (if any)
		if arrLit, ok := stmt.Value.(*parser.ArrayLiteral); ok && len(arrLit.Elements) > 0 {
			g.tmpIdx++
			dataCast := fmt.Sprintf("%%arr.data.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataReg, llvmElemType))

			for i, elem := range arrLit.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%arr.elem.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %d\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataCast, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), llvmElemType, ev, llvmElemType, elemGEP))
			}
		} else if stmt.Value != nil {
			// Non-ArrayLiteral value (e.g., function call returning [N]T):
			// val holds the raw LLVM array value (e.g., [20 x i8] %call.tmp.N).
			// Store it into the data buffer via bitcast to [N x T]*.
			rawArrType := fmt.Sprintf("[%d x %s]", arraySize, llvmElemType)
			g.tmpIdx++
			dataCast := fmt.Sprintf("%%arr.data.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataReg, rawArrType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), rawArrType, val, rawArrType, dataCast))
		}
		return
	}

	// Coerce value to declared type if variable was already typed (e.g., a i8; a = 2)
	alreadyCoerced := false
	if existingType, ok := g.varTypes[name]; ok && existingType != llvmType {
		if strings.HasPrefix(val, "%") {
			if g.isIntegerLLVMType(existingType) && g.isIntegerLLVMType(llvmType) {
				// int → int: trunc/zext
				g.tmpIdx++
				convReg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
				order := map[string]int{"i8": 8, "i16": 16, "i32": 32, "i64": 64}
				if order[llvmType] > order[existingType] {
					sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, llvmType, val, existingType))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to %s\n", g.indent(), convReg, llvmType, val, existingType))
				}
				val = convReg
				llvmType = existingType
				alreadyCoerced = true
			} else if existingType == "double" && g.isIntegerLLVMType(llvmType) {
				// int → double: sitofp
				g.tmpIdx++
				convReg := fmt.Sprintf("%%sitofp.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to double\n", g.indent(), convReg, llvmType, val))
				val = convReg
				llvmType = "double"
				alreadyCoerced = true
			} else if g.isIntegerLLVMType(existingType) && llvmType == "double" {
				// double → int: fptosi
				g.tmpIdx++
				convReg := fmt.Sprintf("%%fptosi.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = fptosi double %s to %s\n", g.indent(), convReg, val, existingType))
				val = convReg
				llvmType = existingType
				alreadyCoerced = true
			}
		}
	}

	// 對單態化後的小整數型別（i8/i16/i32），若值來自 i64 上下文
	// （如陣列索引 zext 或字面常量運算），需要 trunc 到變數型別
	// 注意：若上面的型別強制轉換已經處理過，則跳過，避免重複 trunc
	// 字串字面量 → byte (i8)：從 str-long 資料欄位載入第一個 byte
	if !alreadyCoerced && llvmType == "i8" && strings.HasPrefix(val, "%str-longlit.") {
		if newVal, ok := g.coerceStrLitToByte(sb, val, stmt.Value); ok {
			val = newVal
			alreadyCoerced = true
		}
	}
	if !alreadyCoerced && g.isIntegerLLVMType(llvmType) && llvmType != "i64" && strings.HasPrefix(val, "%") {
		valType := g.intExprLLVMType(stmt.Value)
		if valType == "i64" {
			g.tmpIdx++
			truncReg := fmt.Sprintf("%%trunc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, val, llvmType))
			val = truncReg
		}
	}
	// 寬變數賦值窄整數值（如 u64 = u32 | u32, u64 = u32 + u32）：
	// 兩個窄型別運算的結果仍是窄型別，需要 zext 到變數的寬型別。
	// 使用 valActualType（在 currentTargetType 清除前保存），因為
	// 目標型別傳播可能已使值成為 i64，此時不需要再 zext。
	if !alreadyCoerced && llvmType == "i64" && valActualType != "" {
		if g.isIntegerLLVMType(valActualType) && valActualType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%zext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, valActualType, val))
			val = zextReg
		}
	}

	// Compute the store address for `it` when it's shared across matches
	// with different element types (e.g. allocated as %http2-frame but storing i64).
	// Also update g.varTypes so subsequent reads of `it` in this match arm
	// use the correct type (not the allocated type from a different match).
	storeAddr := g.varAddr(name)
	if stmt.IsSynthetic {
		allocType := ""
		if g.itAllocTypes != nil {
			if at, ok := g.itAllocTypes[name]; ok {
				allocType = at
			}
		}
		if allocType == "" {
			if at, ok := g.varTypes[name]; ok {
				allocType = at
			}
		}
		if allocType != "" && allocType != llvmType {
			// Bitcast pointer when allocated type differs from actual type
			// (e.g. allocated as %http2-frame* but storing/loading i64)
			g.tmpIdx++
			castReg := fmt.Sprintf("%%it.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %s*\n", g.indent(), castReg, allocType, storeAddr, llvmType))
			storeAddr = castReg
		}
		// Update varTypes so reads of `it` in this arm use the correct type
		if g.varTypes != nil {
			g.varTypes[name] = llvmType
		}
	}

	// 延遲綁定（SSA 版本化）：若目標是輸出參數且源是簡單變數，
	// 不立即 store，而是遞增 SSA 版本並記錄綁定（源變數名 + 型別）。
	// flushOutputBindings 時按當前 SSA 版本查表：
	// - 找到綁定 → load 源變數並 store 到輸出參數
	// - 找不到 → 跳過（立即 store 已處理，如常量賦值）
	// SSA 版本由 if 分支前後的 save/restore 隔離，不同分支的綁定互不覆蓋。
	if g.outputParamNames != nil && g.outputParamNames[name] && !stmt.IsSynthetic {
		// 每次賦值都遞增 SSA 版本（無論是否延遲綁定）
		g.ssaVersion[name]++
		ver := g.ssaVersion[name]
		if paramType, ptOk := g.varTypes[name]; ptOk && g.isIntegerLLVMType(paramType) {
			if ident, ok := stmt.Value.(*parser.Identifier); ok {
				if srcType, srcOk := g.varTypes[ident.Value]; srcOk {
					// 簡單變數賦值：res = x → 記錄綁定，不 store
					if g.outputBindings[name] == nil {
						g.outputBindings[name] = make(map[int]outputBinding)
					}
					g.outputBindings[name][ver] = outputBinding{
						sourceVar: ident.Value,
						llvmType:  srcType,
					}
					return
				}
			}
			// 複雜表達式：不延遲，正常 store（值不會變化）
			// 版本已遞增但無綁定，flush 時查不到 → 跳過
		}
		// 非整數型別：版本已遞增但無綁定，正常 store
	}

	// 堆變數追蹤：記錄有堆分配資料的局部變數，用於函數結束時 free。
	// 同時，若目標從局部變數接收堆資料（淺拷貝），標記源變數為 moved（不 free），
	// 以避免 emitHeapFree 同時 free 兩個指向同一塊堆內存的變數（double-free）。
	// 適用於輸出參數和局部變數間的賦值（如 ls.no 中 `dir = a`）。
	if g.heapVars != nil {
		// 僅追蹤局部變數（非參數、非輸出參數）
		if _, isLocal := g.funcLocalNames[name]; isLocal {
			if _, isParam := g.paramNames[name]; !isParam {
				if g.outputParamNames == nil || !g.outputParamNames[name] {
					switch llvmType {
					case "%vec", "%str-long":
						g.trackLocalHeapVar(name, llvmType)
					case "%arr":
						// [N]byte 等固定大小陣列若以 alloca 棧分配（見 shouldStackAllocArray），
						// 則 data 指向棧記憶體，不可在函數退出時 free。
						// 僅當變數不在 stackArrVars 中（即 malloc 路徑）才追蹤為 heap var。
						if g.stackArrVars == nil || !g.stackArrVars[name] {
							g.trackLocalHeapVar(name, llvmType)
						}
					case "%option":
						// option 是否持有堆數據由 inner type 決定。
						// 僅當 inner 為堆型別（%str-long/%vec/%arr/用戶結構體）時才追蹤，
						// 純量 option（?i64/?f64/...）不 malloc，無需釋放。
						if inner, ok := g.optionInnerTypes[name]; ok && g.isHeapOwningType(inner) {
							g.trackLocalHeapVar(name, llvmType)
						}
					default:
						// 用戶自定義結構體：若含堆數據欄位（%vec/%str-long/%arr 或嵌套結構體），
						// 加入追蹤以便函數結束時遞迴釋放。
						if g.isUserStructType(llvmType) {
							g.trackLocalHeapVar(name, llvmType)
						}
					}
				}
			}
		}
		// b = a 是深層 clone（malloc 新 data + memcpy），源變數仍擁有獨立 data，不需 move 標記。
		// out = ident 的 move 由觸發點 1（handleMoveToOut）統一處理。
	}

	switch llvmType {
	case "%str-long":
		// Copy %str-long struct: load from source, store to dest
		// String literal produces %str-long* pointer (alloca).
		// Function call results are already %%str-long values and can be stored directly.
		isGlobal := g.globalVars != nil && g.globalVars[name] && !(g.funcLocalNames != nil && g.funcLocalNames[name])
		if strings.HasPrefix(val, "%str-longlit.") {
			// All string literals are now %str-long* alloca pointers.
			// Load the %str-long value for storing into the target variable.
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%str-long.load.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
			val = loadReg
		} else if g.isStrPtrReg(val) {
			// val is a %str-long* pointer (from generateStrConcat or convertShortToLong).
			// Load the %str-long value from the pointer before storing.
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%str-long.load.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
			val = loadReg
		} else if !strings.HasPrefix(val, "%") {
			// 宣告但無初值（如 `dummy str`）：val 為 "0"，使用 zeroinitializer
			if isGlobal {
				sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), llvmGlobalRef(name)))
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), storeAddr))
			}
			return
		}
		if isGlobal {
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, llvmGlobalRef(name)))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, storeAddr))
		}
	case "%vec":
		// Copy %vec struct: load from source, store to dest
		// val may be either an SSA value (already loaded) or an alloca pointer.
		// Identifiers produce loaded values; slice expressions produce alloca pointers.
		isGlobal := g.globalVars != nil && g.globalVars[name] && !(g.funcLocalNames != nil && g.funcLocalNames[name])
		vecPtrPrefixes := []string{
			"%slic.",    // generateSliceExpression
			"%vec.tmp.", // for-range with slice literal
			"%vvec.",    // variadic call args
		}
		isPtr := false
		for _, p := range vecPtrPrefixes {
			if strings.HasPrefix(val, p) {
				isPtr = true
				break
			}
		}
		if isPtr {
			g.tmpIdx++
			copyReg := fmt.Sprintf("%%vec.copy.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), copyReg, val))
			val = copyReg
		}
		if isGlobal {
			sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), val, llvmGlobalRef(name)))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), val, llvmVarRef(name)))
		}
	case "i8*":
		g.storeDataPtrField(sb, val, g.varAddr(name))
	case "%arr":
		// Assignment to an [N]T variable (e.g., `out = func()` where out was
		// declared as `out [16]byte`). val is a raw LLVM array value (e.g.,
		// [16 x i8] %call.tmp.N from voidSingleOutput). Store it into the
		// %arr struct's data buffer via bitcast.
		if val == "0" || val == "" {
			sb.WriteString(fmt.Sprintf("%sstore %%arr zeroinitializer, %%arr* %s\n", g.indent(), storeAddr))
		} else {
			arraySize := int64(0)
			llvmElemType := "i64"
			if s, ok := g.arraySizes[name]; ok {
				arraySize = s
			}
			if et, ok := g.arrayElemTypes[name]; ok {
				llvmElemType = et
			}
			rawArrType := fmt.Sprintf("[%d x %s]", arraySize, llvmElemType)
			// Get data pointer from %arr struct (field 1)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%arr.let.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%arr.let.data.%d", g.tmpIdx)
			g.tmpIdx++
			dataCast := fmt.Sprintf("%%arr.let.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), dataGEP, storeAddr))
			dataLoad = g.loadDataPtrField(sb, dataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataLoad, rawArrType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), rawArrType, val, rawArrType, dataCast))
		}
	case "float", "double":
		// Convert integer literal to float format (e.g. "1" → "1.0")
		// This is needed when monomorphized union-type functions substitute
		// integer values into float-typed contexts.
		if !strings.HasPrefix(val, "%") && !strings.ContainsAny(val, ".eE") {
			if _, err := fmt.Sscanf(val, "%d", new(int64)); err == nil {
				val = val + ".0"
			}
		}
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), llvmType, val, llvmType, g.varAddr(name)))
	default:
		ptrType := llvmType + "*"
		// 宣告但無初值（如 `f http2-frame`）：val 為 "0"，struct 需用 zeroinitializer
		if strings.HasPrefix(llvmType, "%") && !strings.HasPrefix(val, "%") {
			sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s %s\n", g.indent(), llvmType, ptrType, storeAddr))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %s\n", g.indent(), llvmType, val, ptrType, storeAddr))
		}
	}
}

func (g *Generator) isIntegerLLVMType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i1":
		return true
	}
	return false
}

// isStrPtrReg checks if a register name is a %str-long* pointer (from alloca).
// In LLVM 21 with opaque pointers, both values and pointers are SSA registers
// starting with '%', so we identify pointers by naming convention.
func (g *Generator) isStrPtrReg(val string) bool {
	if !strings.HasPrefix(val, "%") {
		return false
	}
	// Known alloca patterns that produce a %str-long* pointer
	ptrPatterns := []string{
		"%str-longlit.",        // string literal alloca
		"%str-long.s2s.",       // str conversion alloca
		"%s2s.result.",         // s2s conversion in stmt.go
		"%concat.result.",      // generateStrConcat
		"%nfmt.concat.result.", // callNamedFormat sprintf concatenation
		"%str-longrepeat.null", // generateStrRepeat (no sb)
		"%str-longconcat.null", // generateStrConcat (no sb)
		"%argv.str.",           // args-get in call_stdlib.go
		"%sprintf.val.",        // sprintf-based str returns (to-str etc.)
		"%str-long.s2s.",       // duplicate, keep
		"%idx.arr.elem.",       // generateStructFieldIndexRead: [N x %str-long] element GEP
		"%getline.str.",        // get-line builtin in call_stdlib.go
		"%readdir.str.",        // read-dir builtin in call_stdlib.go
		"%rf.str.",             // read-file builtin in call_stdlib.go
		"%archstr.",            // get-arch builtin in call_stdlib.go
		"%slic.",               // generateSliceExpression (string slice → %str-long*)
	}
	for _, p := range ptrPatterns {
		if strings.HasPrefix(val, p) {
			return true
		}
	}
	return false
}

func (g *Generator) stripLLVMType(val string) string {
	prefixes := []string{"i8* ", "i64 ", "i32 ", "i16 ", "i8 ", "double ", "float ", "i1 "}
	for _, p := range prefixes {
		if strings.HasPrefix(val, p) {
			return val[len(p):]
		}
	}
	return val
}

// inferSourceStrType 從 AST 表達式推斷 str 值的 LLVM 型別（%str-long）。
// 用於 struct literal 欄位賦值時判斷 source 與 target 型別是否一致。
// 回傳 "" 表示該表達式不是 str 值。
func (g *Generator) inferSourceStrType(expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.StringLiteral:
		return "%str-long"
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				if t == "%str-long" {
					return t
				}
			}
		}
	case *parser.DotExpression:
		// 結構體欄位存取：欄位型別即為 source 型別
		// 簡化處理：若是 file.path（type = str → %str-long），返回 %str-long
		// 更精確的處理需要查 struct definition，這裡先返回 fieldType
		if ident, ok := v.Receiver.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					structName := strings.TrimPrefix(t, "%")
					if fields, ok := g.structTypes[structName]; ok {
						for _, f := range fields {
							if f.name == v.Property {
								if f.typ == "%str-long" {
									return f.typ
								}
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// materializeStrPtr 將 str 表達式物化為指標（%str-long*）。
// 對於 Identifier，使用其 varAddr；對於 StringLiteral，值本身已是指標；對於其他情況，
// 將值存入臨時 alloca 後回傳其地址。
func (g *Generator) materializeStrPtr(sb *strings.Builder, expr parser.Expression, strType, fieldVal string) string {
	switch expr.(type) {
	case *parser.StringLiteral:
		// StringLiteral 回傳的 fieldVal 本身就是 alloca 指標
		return fieldVal
	case *parser.Identifier:
		ident := expr.(*parser.Identifier)
		// Identifier 的 alloca 指標就是 varAddr
		return g.varAddr(ident.Value)
	case *parser.DotExpression:
		// DotExpression：值是 loaded struct，需要物化為指標
		g.tmpIdx++
		tmpAlloca := fmt.Sprintf("%%st.fmat.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, strType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), strType, fieldVal, strType, tmpAlloca))
		}
		return tmpAlloca
	}
	// Fallback：將 fieldVal 存入臨時 alloca
	g.tmpIdx++
	tmpAlloca := fmt.Sprintf("%%st.fmat.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, strType))
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), strType, fieldVal, strType, tmpAlloca))
	}
	return tmpAlloca
}

func (g *Generator) generateExpressionStmt(sb *strings.Builder, stmt *parser.ExpressionStatement) {
	if stmt.Expression == nil {
		return
	}

	switch e := stmt.Expression.(type) {
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			sb.WriteString(g.indent() + result + "\n")
		}
	case *parser.AssignExpression:
		// 欄位賦值: u.name = value
		g.generateAssignExpression(sb, e)
		// 結果已由 generateAssignExpression 寫入 sb
	default:
		// 用 sb 確保 side effect（如 ++/--）被發出
		_ = g.generateExprWithSB(sb, e)
	}
}

// generateOptionAssign handles assignment to %option typed variables.
// Cases: nil, val(x), err(x), implicit value (tag=0).
func (g *Generator) generateOptionAssign(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// 釋放目標變數的舊堆 box（若有）。
	// 場景：v = f1(); v = f2() — 第二次賦值前需先釋放 f1 返回的 box，否則洩漏。
	// freeOldHeapFree 內部會處理：
	//   - 若 name 不在 heapVars 中（純量 option 或未追蹤），直接 return
	//   - 若 name 是 out 參數，return（呼叫者管理）
	//   - 透過 emitVarHeapFree 的 %option 分支正確釋放 option box
	//   - 透過 bitmap 機制處理 move 後的雙重釋放
	g.freeOldHeapValue(sb, stmt, name)

	// Helper: store tag value
	storeTag := func(tag int64) {
		g.tmpIdx++
		tagGEP := fmt.Sprintf("%%opt.tag.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 0\n", g.indent(), tagGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), tag, tagGEP))
	}

	// Helper: zero data field
	zeroData := func() {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dataGEP))
	}

	// Helper: copy a %str-long struct into data field via heap allocation.
	// malloc the struct, store the value, then ptrtoint the pointer to i64
	// and store that i64 in the data field.
	copyStrToData := func(srcPtr string) {
		g.tmpIdx++
		heapPtr := fmt.Sprintf("%%opt.heap.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 32)\n", g.indent(), heapPtr))
		g.tmpIdx++
		heapCast := fmt.Sprintf("%%opt.heap.cast.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %%str-long*\n", g.indent(), heapCast, heapPtr))
		g.tmpIdx++
		copyReg := fmt.Sprintf("%%opt.str.copy.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), copyReg, srcPtr))
		sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), copyReg, heapCast))
		g.tmpIdx++
		ptrInt := fmt.Sprintf("%%opt.ptr.int.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
	}

	// Helper: store an i64 value directly into data field (fits in 8 bytes)
	copyI64ToData := func(val string) {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), val, dataGEP))
	}

	// Helper: store a double value directly into data field (fits in 8 bytes)
	copyF64ToData := func(val string) {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), val, dataGEP))
	}

	// Helper: store a float (f32) value directly into data field (fits in 8 bytes)
	copyF32ToData := func(val string) {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore float %s, float* %s\n", g.indent(), val, dataGEP))
	}

	// copyToData dispatches to the correct copy function based on the option's inner type.
	// For struct types: malloc on heap, store struct, ptrtoint pointer to i64, store i64.
	// For i64/double/float: store directly in the data field (8 bytes, no malloc needed).
	copyToData := func(val string) {
		innerType := "i64"
		if g.optionInnerTypes != nil {
			if it, ok := g.optionInnerTypes[name]; ok && it != "" {
				innerType = it
			}
		}
		if innerType == "double" {
			copyF64ToData(val)
		} else if innerType == "float" {
			copyF32ToData(val)
		} else if strings.HasPrefix(innerType, "%") {
			// Struct types (e.g. %str-long, %client, %conn):
			// malloc heap, store struct value, ptrtoint pointer to i64, store i64
			structSize := g.llvmTypeSize(innerType)
			if structSize == 0 {
				structSize = 8
			}
			g.tmpIdx++
			heapPtr := fmt.Sprintf("%%opt.heap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), heapPtr, structSize))
			g.tmpIdx++
			heapCast := fmt.Sprintf("%%opt.heap.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), heapCast, heapPtr, innerType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), innerType, val, innerType, heapCast))
			g.tmpIdx++
			ptrInt := fmt.Sprintf("%%opt.ptr.int.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
		} else {
			// All integer types (i8/u8/i16/u16/i32/u32/i64/u64/bool) stored as i64
			copyI64ToData(val)
		}
	}

	switch v := stmt.Value.(type) {
	case *parser.NilLiteral:
		// x = nil → tag=1, zero data
		storeTag(1)
		zeroData()

	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			if (ident.Value == "val" || ident.Value == "ok") && len(v.Arguments) == 1 {
				// x = val(expr) / ok(expr) → tag=0, copy expr to data
				storeTag(0)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && (t == "%str-long") {
						// String variable: load and copy %str-long struct
						// For %str-long, copy directly
						if t == "%str-long" {
							copyStrToData("%" + argIdent.Value)
						} else {
							// Unknown type: store as i64 placeholder
							zeroData()
						}
					} else {
						// i64/f64 variable
						val := g.generateExprWithSB(sb, arg)
						copyToData(val)
					}
				} else if g.isStringExpr(arg) {
					// String expression (e.g. concat 'a' - b): result is %str-long*
					// alloca, copy as str struct regardless of option inner type
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyToData(val)
				}
				return
			}
			if ident.Value == "err" && len(v.Arguments) == 1 {
				// x = err(expr) → tag=2, copy expr to data
				storeTag(2)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && t == "%str-long" {
						copyStrToData("%" + argIdent.Value)
					} else {
						val := g.generateExprWithSB(sb, arg)
						copyToData(val)
					}
				} else if g.isStringExpr(arg) {
					// String expression (e.g. concat 'a' - b): result is %str-long*
					// alloca, copy as str struct regardless of option inner type
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyToData(val)
				}
				return
			}
		}
		// Fallback: 處理函數呼叫返回值
		// Nolang 函數呼叫（如 .to-i64()）透過 voidSingleOutput 路徑會回傳 %option 結構的 load 暫存器，
		// 此時需複製整個結構到 LHS 變數；其他情況（內建函數、純量運算）視為隱含 val 並存成 i64。
		// 判斷依據：call 的函式名稱是否在 funcResultLLVMType 中（即 Nolang 函數）且 LLVM 型別為 %option。
		isNolangOptionCall := false
		if g.funcResultLLVMType != nil {
			if dot, isDot := v.Function.(*parser.DotExpression); isDot {
				// 重組方法名稱：依 varTypes/內建別名表推導型別前綴
				recv := dot.Receiver
				if recvIdent, ok := recv.(*parser.Identifier); ok {
					if recvType, ok := g.varTypes[recvIdent.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						// Map LLVM struct names to Nolang type names for function lookup
						if srcType == "str-long" {
							candidates = append(candidates, "str")
						}
						for _, cand := range candidates {
							candName := cand + "." + dot.Property
							if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
								isNolangOptionCall = true
								break
							}
						}
					}
				} else if _, isStrLit := recv.(*parser.StringLiteral); isStrLit {
					candName := "str." + dot.Property
					if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
						isNolangOptionCall = true
					}
				} else {
					// Receiver is CallExpression / DotExpression / etc. (e.g. arg(1).to-i64()):
					// derive receiver type via exprResultLLVMType and look up method.
					recvType := g.exprResultLLVMType(recv)
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					if srcType == "str-long" {
						candidates = append(candidates, "str")
					}
					for _, cand := range candidates {
						candName := cand + "." + dot.Property
						if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
							isNolangOptionCall = true
							break
						}
					}
				}
			} else if ident, isIdent := v.Function.(*parser.Identifier); isIdent {
				// 已被 transpiler 改寫為 str.to-i64() 形式
				if ts, ok := g.funcResultLLVMType[ident.Value]; ok && len(ts) == 1 && ts[0] == "%option" {
					isNolangOptionCall = true
				}
			}
		}
		if isNolangOptionCall {
			// 呼叫函數並複製 %option 結構
			val := g.generateExprWithSB(sb, v)
			// val 是 %option 的 load 暫存器，需儲存到 name
			sb.WriteString(fmt.Sprintf("%sstore %%option %s, %%option* %%%s\n", g.indent(), val, name))
			return
		}
		storeTag(0)
		val := g.generateExprWithSB(sb, v)
		copyToData(val)

	case *parser.Identifier:
		// option = option：區分 move（out 參數）vs 深層 clone（局部）
		if t, ok := g.varTypes[v.Value]; ok && t == "%option" {
			isOutput := g.outputParamNames != nil && g.outputParamNames[name]
			if isOutput {
				// out = a：走 move（與 vec/struct 一致）。
				// 淺拷貝 %option 結構（共享 box），由 handleMoveToOut 標記 source moved，
				// 函數結束時跳過 source 的 free，由呼叫者管理 out。
				g.tmpIdx++
				copyReg := fmt.Sprintf("%%opt.copy.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load %%option, %%option* %%%s\n", g.indent(), copyReg, v.Value))
				sb.WriteString(fmt.Sprintf("%sstore %%option %s, %%option* %%%s\n", g.indent(), copyReg, name))
				g.handleMoveToOut(sb, v.Value, name)
			} else {
				// b = a：走深層 clone（與 vec/struct 一致）。
				// malloc 新 box + 遞迴 clone inner 的堆數據，
				// 使 a 和 b 各自獨立擁有 box，函數結束時各自 free。
				g.emitOptionDeepClone(sb, v.Value, name)
				// 追蹤目標為堆變數（若 inner 為堆型別）
				if inner, ok := g.optionInnerTypes[name]; ok && g.isHeapOwningType(inner) {
					if _, isLocal := g.funcLocalNames[name]; isLocal {
						if _, isParam := g.paramNames[name]; !isParam {
							if g.outputParamNames == nil || !g.outputParamNames[name] {
								g.trackLocalHeapVar(name, "%option")
							}
						}
					}
				}
			}
		} else {
			// Implicit value: val = n (plain value → ?T, tag=0)
			storeTag(0)
			// If option inner type is a struct (e.g. %str-long) but source
			// variable was alloca'd as i64 (type inference gap), bitcast
			// the i64* to the struct pointer and load the struct value.
			if g.optionInnerTypes != nil {
				if innerType, ok := g.optionInnerTypes[name]; ok && strings.HasPrefix(innerType, "%") {
					if srcType, ok := g.varTypes[v.Value]; !ok || (srcType != innerType && !strings.HasPrefix(srcType, "%")) {
						g.tmpIdx++
						castReg := fmt.Sprintf("%%opt.src.cast.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = bitcast i64* %%%s to %s*\n", g.indent(), castReg, v.Value, innerType))
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%opt.struct.load.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, innerType, innerType, castReg))
						copyToData(loadReg)
						return
					}
				}
			}
			val := g.generateExprWithSB(sb, stmt.Value)
			copyToData(val)
		}

	default:
		// 隱含值：x = expr → tag=0, 將 expr 寫入 data 欄位
		// 對於 struct literal（如 ?file = file{...}），將 option data
		// 欄位 bitcast 為 inner struct pointer，逐欄位直接 store。
		if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
			innerName := strings.TrimPrefix(g.optionInnerTypes[name], "%")
			innerTy := g.mapToLLVMType(innerName)
			fields := g.structTypes[innerName]
			if innerTy != "" && innerTy != "%option" && len(fields) > 0 {
				storeTag(0)
				// malloc struct on heap, store fields, ptrtoint to i64
				structSize := g.llvmTypeSize(innerTy)
				if structSize == 0 {
					structSize = 8
				}
				g.tmpIdx++
				heapPtr := fmt.Sprintf("%%opt.heap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), heapPtr, structSize))
				g.tmpIdx++
				heapCast := fmt.Sprintf("%%opt.heap.cast.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), heapCast, heapPtr, innerTy))
				fieldIndexByName := make(map[string]int)
				for i, f := range fields {
					fieldIndexByName[f.name] = i
				}
				for _, f := range sl.Fields {
					fieldIdx, ok := fieldIndexByName[f.Name]
					if !ok {
						continue
					}
					fieldType := fields[fieldIdx].typ
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%opt.fld.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, innerTy, innerTy, heapCast, fieldIdx))
					fieldVal := g.generateExprWithSB(sb, f.Value)
					fieldVal = g.stripLLVMType(fieldVal)
					if strings.HasPrefix(fieldType, "%") && !strings.HasPrefix(fieldVal, "%") {
						sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), fieldType, fieldType, gepReg))
					} else if strings.HasPrefix(fieldType, "%") && strings.HasPrefix(fieldVal, "%") {
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
					} else {
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
					}
				}
				// ptrtoint heap pointer to i64 and store in option data field
				g.tmpIdx++
				ptrInt := fmt.Sprintf("%%opt.ptr.int.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
				return
			}
		}
		storeTag(0)
		if _, isStr := stmt.Value.(*parser.StringLiteral); isStr {
			srcPtr := g.generateExprWithSB(sb, stmt.Value)
			copyStrToData(srcPtr)
		} else if g.isStringExpr(stmt.Value) {
			// String expression: obtain a pointer to the %str-long value and let
			// copyStrToData load + store it. generateExprPtr covers Identifier,
			// DotExpression (field GEP), and IndexExpression (element GEP).
			// For expressions where generateExprPtr is unsupported (e.g. concat
			// returning an alloca pointer), fall back to generateExprWithSB.
			srcPtr := g.generateExprPtr(sb, stmt.Value)
			if srcPtr == "" {
				srcPtr = g.generateExprWithSB(sb, stmt.Value)
			}
			copyStrToData(srcPtr)
		} else {
			val := g.generateExprWithSB(sb, stmt.Value)
			val = g.stripLLVMType(val)
			// For struct inner types (e.g. ?conn), generateExprWithSB may return a
			// pointer (e.g. %conn* from .conns[i]) rather than a loaded value.
			// Detect this: if val starts with "%" and the option's inner type is a
			// struct, treat val as a pointer and load the struct value first.
			if g.optionInnerTypes != nil {
				if innerType, ok := g.optionInnerTypes[name]; ok && strings.HasPrefix(innerType, "%") {
					if strings.HasPrefix(val, "%") {
						// val is a pointer to the struct; load the value
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%opt.struct.load.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, innerType, innerType, val))
						val = loadReg
					}
				}
			}
			copyToData(val)
		}
	}
}

func (g *Generator) generateEnumDefinition(sb *strings.Builder, ed *parser.EnumDefinition) {
	for _, v := range ed.Values {
		g.tmpIdx++
		sb.WriteString(fmt.Sprintf("%s@%s = constant i64 %d\n", g.indent(), v.Name, v.Value))
	}
}
