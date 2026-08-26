package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// cloneSliceExprResult handles slice expression assignment (e.g. s = t.raw[6..11))
// when generateSliceViewAssignment can't handle it (base is not a plain Identifier).
// It generates the slice expression result (which shares the original data),
// then clones the data into the target variable so it independently owns its data.
// This prevents use-after-free when the original data source is released (e.g.
// struct fields freed at function exit — bug19).
func (g *Generator) cloneSliceExprResult(sb *strings.Builder, stmt *parser.LetStatement, name string) {
	sliceExpr, ok := stmt.Value.(*parser.SliceExpression)
	if !ok {
		return
	}

	// Generate the slice expression result — this creates a temporary
	// %slic.N = alloca %str-long (or %vec) with {len, cap, data} pointing
	// into the original data (no clone, just a view).
	resultReg := g.generateSliceExpression(sb, sliceExpr)

	// Determine result type (str or vec)
	resultType := "%vec"
	isStr := false
	recvType := g.exprResultLLVMType(sliceExpr.Left)
	if recvType == "%str-long" {
		resultType = "%str-long"
		isStr = true
	}

	// Determine element type and size
	elemType := "i64"
	elemSize := int64(8)
	if isStr {
		elemType = "i8"
		elemSize = 1
	} else {
		// Try to get elem type from arrayElemTypes
		if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
			if et, ok := g.arrayElemTypes[ident.Value]; ok {
				elemType = et
				if s := g.llvmTypeSize(elemType); s > 0 {
					elemSize = s
				}
			}
		}
	}

	// Load source data pointer and length from the slice result
	// %str-long / %vec = { i64 len, i64 cap, i64 data }
	dataFieldIdx := uint32(2)

	// Load len (field 0)
	g.tmpIdx++
	srcLenGEP := fmt.Sprintf("%%cse.src.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	srcLenReg := fmt.Sprintf("%%cse.src.len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), srcLenGEP, resultType, resultType, resultReg))
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
		g.indent(), srcLenReg, srcLenGEP))

	// Load data pointer (field 2)
	g.tmpIdx++
	srcDataGEP := fmt.Sprintf("%%cse.src.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcDataGEP, resultType, resultType, resultReg, dataFieldIdx))
	srcDataReg := g.loadDataPtrField(sb, srcDataGEP)

	// Clone: malloc new buffer + memcpy, write len/cap/data into target variable
	g.emitSliceClone(sb, name, srcDataReg, srcLenReg, elemType, elemSize, isStr, "0")

	// Track target as heap variable (only for local variables, not output params)
	if g.outputParamNames == nil || !g.outputParamNames[name] {
		trackType := "%vec"
		if isStr {
			trackType = "%str-long"
		}
		g.trackLocalHeapVar(name, trackType)
	}
}
