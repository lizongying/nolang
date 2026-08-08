#!/usr/bin/env python3
import sys

f = 'src/build/transpiler.go'
t = open(f).read()

old = '\treturn t.llvmGenerator.Generate(merged), nil\n}'

new = '\tir := t.llvmGenerator.Generate(merged)\n' \
      '\tif errs := t.llvmGenerator.CodegenErrors(); len(errs) > 0 {\n' \
      '\t\treturn "", fmt.Errorf("codegen errors: %v", errs)\n' \
      '\t}\n' \
      '\treturn ir, nil\n}'

count = t.count(old)
print(f'Found {count} occurrences')
if count == 1:
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: replaced')
else:
    print('ERROR: expected 1 occurrence')
    sys.exit(1)
