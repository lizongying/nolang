#!/usr/bin/env python3
import sys

f = 'src/build/llvm/expr.go'
t = open(f).read()

# In exprResultLLVMType, the IndexExpression case handles struct.field[i]
# but only when dot.Receiver is an Identifier. We need to also handle
# DotExpression receivers (e.g., .pool.nodes[idx] where .pool is a DotExpression).

# Current code (simplified):
#   if dot, ok := v.Left.(*parser.DotExpression); ok {
#       recvName := ""
#       if ident, ok := dot.Receiver.(*parser.Identifier); ok {
#           recvName = ident.Value
#       }
#       if recvName != "" && g.varTypes != nil { ... }
#   }

# We need to add an else branch that handles non-Identifier receivers
# by recursively calling exprResultLLVMType on the receiver.

old = '''		// struct.field[i] — when the field is an inline array (e.g. .domains[i]
		// where domains is [64 x %str-long]), exprResultLLVMType(v.Left) returns
		// "%arr" (per the DotExpression case above), which hides the element type.
		// Look up the raw array type from the struct definition and extract it.
		if dot, ok := v.Left.(*parser.DotExpression); ok {
			recvName := ""
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				recvName = ident.Value
			}
			if recvName != "" && g.varTypes != nil {'''

new = '''		// struct.field[i] — when the field is an inline array (e.g. .domains[i]
		// where domains is [64 x %str-long]), exprResultLLVMType(v.Left) returns
		// "%arr" (per the DotExpression case above), which hides the element type.
		// Look up the raw array type from the struct definition and extract it.
		if dot, ok := v.Left.(*parser.DotExpression); ok {
			recvName := ""
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				recvName = ident.Value
			}
			// For non-Identifier receivers (e.g., .pool.nodes[idx]),
			// recursively resolve the receiver type to find the struct name.
			if recvName == "" {
				recvType := g.exprResultLLVMType(dot.Receiver)
				if g.isStructLLVMType(recvType) {
					recvStructName := strings.TrimPrefix(recvType, "%")
					if fields, _ := g.resolveStructFields(recvStructName); fields != nil {
						for _, f := range fields {
							if f.name == dot.Property && strings.HasPrefix(f.typ, "[") {
								closeB := strings.IndexByte(f.typ, ']')
								if closeB > 0 {
									inner := f.typ[1:closeB]
									xIdx := strings.LastIndex(inner, " x ")
									if xIdx >= 0 {
										return inner[xIdx+3:]
									}
								}
							}
						}
					}
				}
			}
			if recvName != "" && g.varTypes != nil {'''

count = t.count(old)
print(f'Found {count} occurrences')
if count == 1:
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: replaced')
else:
    print('ERROR: expected 1 occurrence')
    sys.exit(1)
