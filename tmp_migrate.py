import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    changes = []
    
    # Pattern 1: Replace store i8* X, i8** Y with g.storeDataPtrField(sb, X, Y)
    # Match: sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), X, Y))
    # But NOT: store i8** (C ABI like argv)
    pattern1 = r'sb\.WriteString\(fmt\.Sprintf\("%sstore i8\* %s, i8\*\* %s\\n", g\.indent\(\), ([^,]+), ([^)]+)\)\)'
    def replace1(m):
        return f'g.storeDataPtrField(sb, {m.group(1)}, {m.group(2)})'
    content = re.sub(pattern1, replace1, content)
    
    # Pattern 2: Replace load i8*, i8** X
    # Match: sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), REG, X))
    # Replace with: REG := g.loadDataPtrField(sb, X)
    # But we need to also remove the preceding g.tmpIdx++ and REG := fmt.Sprintf(...) lines
    # This is complex, so let's handle it with a simpler approach:
    # Just replace the WriteString line, and the REG variable will still work because
    # loadDataPtrField returns a string (the register name)
    pattern2 = r'sb\.WriteString\(fmt\.Sprintf\("%s%s = load i8\*, i8\*\* %s\\n", g\.indent\(\), ([^,]+), ([^)]+)\)\)'
    def replace2(m):
        return f'// {m.group(1)} := g.loadDataPtrField(sb, {m.group(2)})\n\t\t{m.group(1)} = g.loadDataPtrField(sb, {m.group(2)})'
    # Actually this won't work well because REG is declared with := and we'd get a redeclaration
    # Let's just comment out the old line and add the new one
    # Actually, the simplest approach: just replace the WriteString with the helper call
    # and assign the result to REG. But REG is usually declared before this line.
    # Let's use a different approach: replace the WriteString line with nothing,
    # and change the REG declaration to use loadDataPtrField.
    
    # Actually, the simplest approach for now: just replace the load line
    # The preceding lines (g.tmpIdx++ and REG := fmt.Sprintf(...)) will become dead code
    # but won't cause compilation errors since the variables are just string names.
    # Wait, actually they WILL cause issues because REG is used later.
    # Let me just replace the WriteString with a call that sets REG:
    # REG = g.loadDataPtrField(sb, X)
    # But REG might be declared with := above, so using = would work if in same scope.
    # Actually, since the preceding line is REG := fmt.Sprintf(...), we can't use = .
    # Let me just leave the pattern as is and handle loads manually.
    
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
