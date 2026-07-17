import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Replace: sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), REG, GEP))
    # With:    REG = g.loadDataPtrField(sb, GEP)
    # Note: REG is already declared with := above, so we use = for reassignment
    pattern = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %s\\n", g\.indent\(\), (\w+), (\w+)\)\)'
    )
    def replace(m):
        return f'{m.group(1)} = g.loadDataPtrField(sb, {m.group(2)})'
    content = pattern.sub(replace, content)
    
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
