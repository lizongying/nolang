#!/usr/bin/env python3
import sys, re

f = 'src/build/llvm/call.go'
t = open(f).read()

# Remove the if targetType == "" block (with correct 2-tab indentation)
# Pattern: targetType := g.currentTargetType\n + if block + switch targetType {
pattern = r'\t\ttargetType := g\.currentTargetType\n\t\tif targetType == "" \{\n\t\t\tpos := expr\.Pos\(\)\n\t\t\tg\.AddCodegenError\(fmt\.Sprintf\("line %d, column %d: cannot infer type for %s\(\) without explicit type annotation or previously defined variable", pos\.Line, pos\.Column, forwardFunc\)\)\n\t\t\treturn "0"\n\t\t\}\n\t\tswitch targetType \{'

replacement = '\t\ttargetType := g.currentTargetType\n\t\tswitch targetType {'

count = len(re.findall(pattern, t))
print(f'Found {count} occurrences')
if count == 3:
    t = re.sub(pattern, replacement, t)
    open(f, 'w').write(t)
    print('OK: all 3 reverted')
else:
    print('ERROR: expected 3 occurrences')
    sys.exit(1)
