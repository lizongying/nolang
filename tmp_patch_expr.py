#!/usr/bin/env python3
import sys

f = 'src/build/llvm/expr.go'
t = open(f).read()

# In generateStructFieldIndexAssign, the RHS value is generated before
# the field type is determined. We need to:
# 1. Determine struct name and field type FIRST
# 2. Set currentTargetType from the element type
# 3. Then generate the RHS value

old = '''func (g *Generator) generateStructFieldIndexAssign(sb *strings.Builder, dot *parser.DotExpression, index parser.Expression, value parser.Expression) string {
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	fieldName := dot.Property
	idx := g.generateExprWithSB(sb, index)
	val := g.generateExprWithSB(sb, value)

	// 判定 struct 名稱與基底指標
	// - Identifier receiver: 使用變數名稱（%%%s）
	// - 非 Identifier receiver: 使用 generateExprPtr 取得指標
	structName := ""
	basePtr := ""
	if recvName != "" {
		if t, ok := g.varTypes[recvName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
		recvType := g.exprResultLLVMType(dot.Receiver)
		if g.isStructLLVMType(recvType) {
			structName = strings.TrimPrefix(recvType, "%")
		}
		if sb != nil {
			basePtr = g.generateExprPtr(sb, dot.Receiver)
		}
	}'''

new = '''func (g *Generator) generateStructFieldIndexAssign(sb *strings.Builder, dot *parser.DotExpression, index parser.Expression, value parser.Expression) string {
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	fieldName := dot.Property
	idx := g.generateExprWithSB(sb, index)

	// 判定 struct 名稱與基底指標
	// - Identifier receiver: 使用變數名稱（%%%s）
	// - 非 Identifier receiver: 使用 generateExprPtr 取得指標
	structName := ""
	basePtr := ""
	if recvName != "" {
		if t, ok := g.varTypes[recvName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
		recvType := g.exprResultLLVMType(dot.Receiver)
		if g.isStructLLVMType(recvType) {
			structName = strings.TrimPrefix(recvType, "%")
		}
		if sb != nil {
			basePtr = g.generateExprPtr(sb, dot.Receiver)
		}
	}

	// Set target type for type-inferred builtins (e.g. with-cap)
	// before generating the RHS value. The target type is the element
	// type of the array/slice field being indexed.
	prevTargetType := g.currentTargetType
	g.currentTargetType = ""
	if structName != "" {
		if fields, ok := g.structTypes[structName]; ok {
			for _, f := range fields {
				if f.name == fieldName {
					if f.elemType != "" {
						g.currentTargetType = f.elemType
					} else if f.typ == "%str-long" {
						g.currentTargetType = "%str-long"
					}
					break
				}
			}
		}
	}

	val := g.generateExprWithSB(sb, value)
	g.currentTargetType = prevTargetType'''

count = t.count(old)
print(f'Found {count} occurrences')
if count == 1:
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: replaced')
else:
    print('ERROR: expected 1 occurrence')
    sys.exit(1)
