#!/usr/bin/env python3
import sys

f = 'src/build/llvm/expr.go'
t = open(f).read()

# Fix: in generateStructFieldIndexAssign, the currentTargetType setting
# doesn't handle [N x %type] array fields. The field type is like
# "[16 x %str-long]" and we need to extract "%str-long" from it.

old = '''	// Set target type for type-inferred builtins (e.g. with-cap)
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

new = '''	// Set target type for type-inferred builtins (e.g. with-cap)
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
					} else if strings.HasPrefix(f.typ, "[") {
						// Array field: [N x %type] → extract %type
						closeB := strings.IndexByte(f.typ, ']')
						if closeB > 0 {
							inner := f.typ[1:closeB]
							xIdx := strings.LastIndex(inner, " x ")
							if xIdx >= 0 {
								g.currentTargetType = inner[xIdx+3:]
							}
						}
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
