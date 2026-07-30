#!/usr/bin/env python3
"""Fix generateCallArg's SliceLiteral handling to infer element type from actual elements."""

path = 'src/build/llvm/call.go'
with open(path, 'r') as f:
    content = f.read()

# The old code block - exact match from the file (line 364-405)
old = '''	case *parser.SliceLiteral:
		// Slice literal passed as function argument in indirect/curried calls.
		// Default to i64 element type (parameter type info unavailable here).
		n := int64(len(a.Elements))
		g.tmpIdx++
		vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\\n", g.indent(), vecName))
		}
		if n > 0 {
			g.tmpIdx++
			tmpArr := fmt.Sprintf("%%callvec.arr.%d", g.tmpIdx)
			arrType := fmt.Sprintf("[%d x i64]", n)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\\n", g.indent(), tmpArr, arrType))
				for i, elem := range a.Elements {
					ev := g.generateExprWithSB(sb, elem)
					ev = g.stripLLVMType(ev)
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\\n",
						g.indent(), gepReg, arrType, arrType, tmpArr, i))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\\n", g.indent(), ev, gepReg))
				}
				g.tmpIdx++
				ptrReg := fmt.Sprintf("%%callvec.ptr.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\\n", g.indent(), ptrReg, arrType, tmpArr))
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%callvec.len.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\\n", g.indent(), lenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\\n", g.indent(), n, lenGEP))
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%callvec.cap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\\n", g.indent(), capGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\\n", g.indent(), n, capGEP))
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%callvec.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\\n", g.indent(), dataGEP, vecName))
				g.storeDataPtrField(sb, ptrReg, dataGEP)
			}
		}
		return "%vec* " + vecName'''

# The new code block with element type inference
new = '''	case *parser.SliceLiteral:
		// Slice literal passed as function argument in indirect/curried calls.
		// Infer element type from the actual elements (parameter type info
		// unavailable here). Default to i64 for integer elements.
		elemType := "i64"
		if len(a.Elements) > 0 && g.isStringExpr(a.Elements[0]) {
			elemType = "%str-long"
		}
		n := int64(len(a.Elements))
		g.tmpIdx++
		vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\\n", g.indent(), vecName))
		}
		if n > 0 {
			g.tmpIdx++
			tmpArr := fmt.Sprintf("%%callvec.arr.%d", g.tmpIdx)
			arrType := fmt.Sprintf("[%d x %s]", n, elemType)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\\n", g.indent(), tmpArr, arrType))
				for i, elem := range a.Elements {
					ev := g.generateExprWithSB(sb, elem)
					ev = g.stripLLVMType(ev)
					// For struct types (e.g. %str-long), strip the type prefix
					// if present (generateExprWithSB may return "%str-long %reg").
					if g.isStructLLVMType(elemType) && strings.HasPrefix(ev, elemType+" ") {
						ev = ev[len(elemType)+1:]
					}
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\\n",
						g.indent(), gepReg, arrType, arrType, tmpArr, i))
					// For struct types, generateExprWithSB may return a pointer
					// (e.g. StringLiteral returns %str-longlit.N which is a %str-long*).
					// Load the struct value before storing into the array.
					storeVal := ev
					if g.isStructLLVMType(elemType) {
						// StringLiteral returns an alloca pointer; load the value.
						if strings.HasPrefix(ev, "%str-longlit") {
							g.tmpIdx++
							loadReg := fmt.Sprintf("%%callvec.load.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\\n", g.indent(), loadReg, elemType, elemType, ev))
							storeVal = loadReg
						}
					} else if g.isIntegerLLVMType(elemType) && elemType != "i64" && strings.HasPrefix(ev, "%") {
						// Truncate i64 to smaller integer types
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%callvec.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\\n", g.indent(), truncReg, ev, elemType))
						storeVal = truncReg
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\\n", g.indent(), elemType, storeVal, elemType, gepReg))
				}
				g.tmpIdx++
				ptrReg := fmt.Sprintf("%%callvec.ptr.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\\n", g.indent(), ptrReg, arrType, tmpArr))
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%callvec.len.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\\n", g.indent(), lenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\\n", g.indent(), n, lenGEP))
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%callvec.cap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\\n", g.indent(), capGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\\n", g.indent(), n, capGEP))
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%callvec.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\\n", g.indent(), dataGEP, vecName))
				g.storeDataPtrField(sb, ptrReg, dataGEP)
			}
		}
		return "%vec* " + vecName'''

if old not in content:
    print('ERROR: old string not found in file')
    # Try to find a partial match
    idx = content.find('Slice literal passed as function argument in indirect/curried calls')
    if idx >= 0:
        print(f'Found comment at index {idx}')
        print(repr(content[idx:idx+200]))
    else:
        print('Comment not found at all')
    exit(1)

content = content.replace(old, new, 1)

with open(path, 'w') as f:
    f.write(content)

print('SUCCESS: Applied fix for callvec element type inference')
