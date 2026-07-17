import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Pattern: load i8*, i8** X
    # Match three-line pattern:
    #   g.tmpIdx++
    #   VAR := fmt.Sprintf("%%...%d", g.tmpIdx)
    #   sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), VAR, GEP))
    # Replace with:
    #   VAR := g.loadDataPtrField(sb, GEP)
    
    # Also match two-line pattern (without g.tmpIdx++):
    #   VAR := fmt.Sprintf("%%...%d", g.tmpIdx)
    #   sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), VAR, GEP))
    
    # Three-line pattern
    pattern3 = re.compile(
        r'\tg\.tmpIdx\+\+\n'
        r'\t+(\w+) := fmt\.Sprintf\([^)]*\)\n'
        r'\t+sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %s\\n", g\.indent\(\), \1, (\w+)\)\)',
        re.MULTILINE
    )
    def replace3(m):
        return f'\t{m.group(1)} := g.loadDataPtrField(sb, {m.group(2)})'
    content = pattern3.sub(replace3, content)
    
    # Two-line pattern
    pattern2 = re.compile(
        r'\t+(\w+) := fmt\.Sprintf\([^)]*\)\n'
        r'\t+sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %s\\n", g\.indent\(\), \1, (\w+)\)\)',
        re.MULTILINE
    )
    def replace2(m):
        return f'\t{m.group(1)} := g.loadDataPtrField(sb, {m.group(2)})'
    content = pattern2.sub(replace2, content)
    
    # Also handle insertvalue patterns: insertvalue %str-long ... i8* ... 2
    # and insertvalue %vec ... i8* ... 2
    # These need ptrtoint before insertvalue
    # Pattern: sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i8* %s, 2\n", g.indent(), REG, PREV, PTR))
    # We need to add: g.tmpIdx++; intReg := fmt.Sprintf(...); sb.WriteString(ptrtoint...); then use i64 intReg in insertvalue
    # This is complex, let's handle it differently - replace i8* with i64 in insertvalue and add ptrtoint
    
    if content != original:
        with open(filepath, 'w') as f:
            f.write(content)
        return True
    return False

llvm_dir = 'src/build/llvm'
for fname in os.listdir(llvm_dir):
    if fname.endswith('.go'):
        filepath = os.path.join(llvm_dir, fname)
        if process_file(filepath):
            print(f'Processed: {filepath}')
