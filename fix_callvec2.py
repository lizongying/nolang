#!/usr/bin/env python3
"""Fix generateCallExpression's SliceLiteral handling to support str-long element types."""

path = 'src/build/llvm/call.go'
with open(path, 'r') as f:
    content = f.read()

# The old code block in generateCallExpression (line 2413-2453)
old = '''	case *parser.SliceLiteral:
		// Slice literal as function argument (e.g. bn-sub(m, [2, 0, ...]))
		// Determine element type from the parameter's declared type.
		elemType := "i64"
		if g.funcParamTypes != nil {
			if types, ok := g.funcParamTypes[fnName]; ok && argIdx < len(types) {
				paramType := types[argIdx]
				if strings.HasPrefix(paramType, "[]") {
					mapped := g.mapToLLVMType(paramType[2:])
					if g.isIntegerLLVMType(mapped) {
						elemType = mapped
					}
				}
			}
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
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\\n",
						g.indent(), gepReg, arrType, arrType, tmpArr, i))
					storeVal := ev
					if elemType != "i64" && strings.HasPrefix(ev, "%") {
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%callvec.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\\n", g.indent(), truncReg, ev, elemType))
						storeVal = truncReg
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\\n", g.indent(), elemType, storeVal, elemType, gepReg))
				}'''

# The new code block with str-long support
new = '''	case *parser.SliceLiteral:
		// Slice literal as function argument (e.g. bn-sub(m, [2, 0, ...]))
		// Determine element type from the parameter's declared type.
		elemType := "i64"
		if g.funcParamTypes != nil {
			if types, ok := g.funcParamTypes[fnName]; ok && argIdx < len(types) {
				paramType := types[argIdx]
				if strings.HasPrefix(paramType, "[]") {
					mapped := g.mapToLLVMType(paramType[2:])
					if g.isIntegerLLVMType(mapped) || g.isStructLLVMType(mapped) {
						elemType = mapped
					}
				}
			}
		}
		// Fallback: infer from actual elements if param type didn't resolve
		if elemType == "i64" && len(a.Elements) > 0 && g.isStringExpr(a.Elements[0]) {
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
				}'''

if old not in content:
    print('ERROR: old string not found in file')
    idx = content.find('Slice literal as function argument (e.g. bn-sub')
    if idx >= 0:
        print(f'Found comment at index {idx}')
        print(repr(content[idx:idx+500]))
    exit(1)

content = content.replace(old, new, 1)

with open(path, 'w') as f:
    f.write(content)

print('SUCCESS: Applied fix for generateCallExpression callvec str-long support')
