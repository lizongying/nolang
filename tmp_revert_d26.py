#!/usr/bin/env python3
import sys

f = 'src/build/llvm/call.go'
t = open(f).read()

# Revert: remove the error check, but keep the empty type defaulting to %vec
old = '''\t\t\ttargetType := g.currentTargetType
\t\t\tif targetType == "" {
\t\t\t\tpos := expr.Pos()
\t\t\t\tg.AddCodegenError(fmt.Sprintf("line %d, column %d: cannot infer type for %s() without explicit type annotation or previously defined variable", pos.Line, pos.Column, forwardFunc))
\t\t\t\treturn "0"
\t\t\t}
\t\t\tswitch targetType {'''

new = '''\t\t\ttargetType := g.currentTargetType
\t\t\tswitch targetType {'''

count = t.count(old)
print(f'Found {count} occurrences')
if count == 3:
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: all 3 reverted')
else:
    print('ERROR: expected 3 occurrences')
    sys.exit(1)
