import re

with open('build/llvm/stmt.go', 'r') as f:
    lines = f.readlines()

depth = 0
for i, line in enumerate(lines, 1):
    # Remove line comments
    line = re.sub(r'//.*$', '', line)
    # Remove strings
    line = re.sub(r'"[^"]*"', '""', line)
    # Remove backtick strings
    line = re.sub(r'`[^`]*`', '``', line)
    # Remove rune literals
    line = re.sub(r"'[^']*'", "''", line)
    
    opens = line.count('{')
    closes = line.count('}')
    depth += opens - closes
    
    if depth < 0:
        print(f'Line {i}: depth went negative ({depth})')
        break

print(f'Final depth after all lines: {depth}')

# Find where depth jumps unexpectedly
depth = 0
prev_depth = 0
for i, line in enumerate(lines, 1):
    line_clean = re.sub(r'//.*$', '', line)
    line_clean = re.sub(r'"[^"]*"', '""', line_clean)
    line_clean = re.sub(r'`[^`]*`', '``', line_clean)
    line_clean = re.sub(r"'[^']*'", "''", line_clean)
    
    opens = line_clean.count('{')
    closes = line_clean.count('}')
    depth += opens - closes
    
    # Print lines where depth changes by more than 2
    if abs(depth - prev_depth) > 2:
        print(f'Line {i}: depth jumped from {prev_depth} to {depth} (delta={depth-prev_depth})')
    
    prev_depth = depth
