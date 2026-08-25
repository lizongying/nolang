import re

lines = open('/tmp/stmt_orig.go').readlines()
depth = 0
for i, line in enumerate(lines, 1):
    lc = re.sub(r'//.*$', '', line)
    lc = re.sub(r'"[^"]*"', '""', lc)
    lc = re.sub(r'`[^`]*`', '``', lc)
    lc = re.sub(r"'[^']*'", "''", lc)
    depth += lc.count('{') - lc.count('}')
    if 6405 <= i <= 6420 or (i >= 6595 and i <= 6625):
        print(f'Line {i}: depth={depth} | {line.rstrip()}')
