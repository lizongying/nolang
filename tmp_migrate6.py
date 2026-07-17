import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Multi-line pattern: 
    # sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
    #     g.indent(), VAR, GEP))
    # Replace with:
    # VAR = g.loadDataPtrField(sb, GEP)
    pattern = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %s\\n",\s*\n\s*g\.indent\(\), (\w+), (\w+)\)\)',
        re.MULTILINE
    )
    def replace(m):
        return f'{m.group(1)} = g.loadDataPtrField(sb, {m.group(2)})'
    content = pattern.sub(replace, content)
    
    # Also handle: sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %%%s\n", g.indent(), dataLoad, varName))
    # This is for loading from a variable name directly (like %varname)
    pattern2 = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %%%s\\n", g\.indent\(\), (\w+), (\w+)\)\)'
    )
    def replace2(m):
        return f'{m.group(1)} = g.loadDataPtrField(sb, "%"+{m.group(2)})'
    content = pattern2.sub(replace2, content)
    
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
