#!/usr/bin/env python3
import sys

f = 'src/build/llvm/call.go'
t = open(f).read()

# 2 tabs indentation
old = '\t\ttargetType := g.currentTargetType\n\t\tswitch targetType {'

new = '\t\ttargetType := g.currentTargetType\n' \
      '\t\tif targetType == "" {\n' \
      '\t\t\tpos := expr.Pos()\n' \
      '\t\t\tg.AddCodegenError(fmt.Sprintf("line %d, column %d: cannot infer type for %s() without explicit type annotation or previously defined variable", pos.Line, pos.Column, forwardFunc))\n' \
      '\t\t\treturn "0"\n' \
      '\t\t}\n' \
      '\t\tswitch targetType {'

count = t.count(old)
print(f'Found {count} occurrences')
if count == 3:
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: all 3 replaced')
else:
    print('ERROR: expected 3 occurrences')
    sys.exit(1)
