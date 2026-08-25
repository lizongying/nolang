import re

with open('build/llvm/stmt.go', 'r') as f:
    lines = f.readlines()

depth = 0
for i, line in enumerate(lines, 1):
    lc = re.sub(r'//.*$', '', line)
    lc = re.sub(r'"[^"]*"', '""', lc)
    lc = re.sub(r'`[^`]*`', '``', lc)
    lc = re.sub(r"'[^']*'", "''", lc)
    old_depth = depth
    depth += lc.count('{') - lc.count('}')
    
    # Show lines where depth returns to 0 or 1
    if (old_depth > 1 and depth <= 1) and i > 6300:
        print(f'Line {i}: depth {old_depth} -> {depth} | {line.rstrip()}')
