import re

lines = open('/tmp/stmt_orig.go').readlines()
depth = 0
for i, line in enumerate(lines, 1):
    lc = re.sub(r'//.*$', '', line)
    lc = re.sub(r'"[^"]*"', '""', lc)
    lc = re.sub(r'`[^`]*`', '``', lc)
    lc = re.sub(r"'[^']*'", "''", lc)
    old_depth = depth
    depth += lc.count('{') - lc.count('}')
    
    # Show lines where depth returns to 1 or 0
    if (old_depth > 1 and depth <= 1) or (i > 7700 and old_depth <= 2 and depth != old_depth):
        print(f'Line {i}: depth {old_depth} -> {depth} | {line.rstrip()}')
