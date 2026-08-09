#!/usr/bin/env python3
import sys

f = 'src/build/llvm/call.go'
t = open(f).read()

# Add debug print in with-cap handler to see currentTargetType
old = '\t\tcapVal := g.evalI64Arg(sb, args[0])\n\t\ttargetType := g.currentTargetType\n\t\tswitch targetType {'
new = '''\t\tcapVal := g.evalI64Arg(sb, args[0])
\t\ttargetType := g.currentTargetType
\t\tif sb != nil {
\t\t\tfmt.Fprintf(os.Stderr, "DEBUG with-cap: targetType=%q\\n", targetType)
\t\t}
\t\tswitch targetType {'''

count = t.count(old)
print(f'Found {count} occurrences')
if count == 3:
    import os
    # Check if "os" is imported
    if '"os"' not in t and "'os'" not in t:
        # Add import at the top of the file
        # Find the import block
        pass  # fmt is already imported, let's use fmt.Fprintf to stderr
    t = t.replace(old, new)
    open(f, 'w').write(t)
    print('OK: debug added')
else:
    print('ERROR: expected 3 occurrences')
    sys.exit(1)
