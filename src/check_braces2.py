import re

with open('build/llvm/stmt.go', 'r') as f:
    lines = f.readlines()

depth = 0
for i, line in enumerate(lines, 1):
    line_clean = re.sub(r'//.*$', '', line)
    line_clean = re.sub(r'"[^"]*"', '""', line_clean)
    line_clean = re.sub(r'`[^`]*`', '``', line_clean)
    line_clean = re.sub(r"'[^']*'", "''", line_clean)
    
    opens = line_clean.count('{')
    closes = line_clean.count('}')
    old_depth = depth
    depth += opens - closes
    
    # Print lines 6700 to 7760 with depth changes
    if 6700 <= i <= 7760 and (opens > 0 or closes > 0):
        print(f'Line {i}: depth {old_depth} -> {depth}')
