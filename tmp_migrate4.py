import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # Pattern: insertvalue %str-long or %vec with i8* at index 2
    # Match: sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i8* %s, 2\n", g.indent(), REG, PREV, PTR))
    # Replace with: 
    #   intVal := g.ptrToIntVal(sb, PTR)
    #   sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), REG, PREV, intVal))
    
    # For %str-long
    pattern_str = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = insertvalue %%str-long %s, i8\* %s, 2\\n", g\.indent\(\), (\w+), (\w+), (\w+)\)\)'
    )
    def replace_str(m):
        reg, prev, ptr = m.group(1), m.group(2), m.group(3)
        return f'intVal := g.ptrToIntVal(sb, {ptr})\n\t\tsb.WriteString(fmt.Sprintf("%s{reg} = insertvalue %str-long {prev}, i64 %s, 2\\n", g.indent(), intVal))'.replace('{reg}', reg).replace('{prev}', prev)
    # Actually this is getting messy with the %% escaping. Let me use a different approach.
    
    # Simpler: just replace "i8* %s, 2" with "i64 %s, 2" in insertvalue patterns
    # and add ptrtoint before the insertvalue line
    # But the ptrtoint needs to be a separate statement...
    
    # Let me just replace the insertvalue line to use ptrToIntVal inline
    # Actually, we can't do it inline in Go. We need to add a separate line.
    
    # Let me use a multi-line replacement
    def replace_insertvalue(m):
        reg, prev, ptr = m.group(1), m.group(2), m.group(3)
        # Check if prev is 'zeroinitializer' - it won't be a \w+ match
        return f'g.tmpIdx++\n\t\t_p2i := fmt.Sprintf("%%p2i.%d", g.tmpIdx)\n\t\tsb.WriteString(fmt.Sprintf("%s_p2i = ptrtoint i8* {ptr} to i64\\n", g.indent()))\n\t\tsb.WriteString(fmt.Sprintf("%s{reg} = insertvalue %str-long {prev}, i64 _p2i, 2\\n", g.indent()))'.replace('{reg}', reg).replace('{prev}', prev).replace('{ptr}', ptr)
    
    # This is getting too complex with string manipulation. Let me use a simpler approach:
    # Just replace i8* with i64 in the insertvalue patterns, and add a ptrtoint call before.
    # The ptrtoint value will be stored in a new variable.
    
    # Actually, the simplest approach: use ptrToIntVal helper
    # Replace the whole WriteString line with two lines:
    # 1. _ptrInt := g.ptrToIntVal(sb, PTR)
    # 2. sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), REG, PREV, _ptrInt))
    
    # But the %% needs to be preserved in the format string.
    # Let me try a different regex approach.
    
    content = original  # Reset
    
    # Match insertvalue with i8* for %str-long field 2
    # The actual line looks like:
    # sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i8* %s, 2\n", g.indent(), strReg3, strReg2, selectReg))
    pattern1 = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%(\w+) = insertvalue %%str-long %(\w+), i8\* %(\w+), 2\\n", g\.indent\(\)\)\)',
        re.MULTILINE
    )
    # Hmm, the format args are not captured correctly. Let me look at the actual pattern more carefully.
    # The actual pattern is:
    # sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i8* %s, 2\n", g.indent(), strReg3, strReg2, selectReg))
    # The format string has %s placeholders: %s%s = insertvalue %%str-long %s, i8* %s, 2\n
    # Arguments: g.indent(), strReg3, strReg2, selectReg
    
    pattern2 = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = insertvalue %%str-long %s, i8\* %s, 2\\n", g\.indent\(\), (\w+), (\w+), (\w+)\)\)'
    )
    def replace2(m):
        reg, prev, ptr = m.group(1), m.group(2), m.group(3)
        return (f'_p2i_{reg} := g.ptrToIntVal(sb, {ptr})\n'
                f'\t\tsb.WriteString(fmt.Sprintf("%s{reg} = insertvalue %%str-long {prev}, i64 %s, 2\\n", g.indent(), _p2i_{reg}))')
    content = pattern2.sub(replace2, content)
    
    # Same for %vec
    pattern3 = re.compile(
        r'sb\.WriteString\(fmt\.Sprintf\("%s%s = insertvalue %%vec %s, i8\* %s, 2\\n", g\.indent\(\), (\w+), (\w+), (\w+)\)\)'
    )
    def replace3(m):
        reg, prev, ptr = m.group(1), m.group(2), m.group(3)
        return (f'_p2i_{reg} := g.ptrToIntVal(sb, {ptr})\n'
                f'\t\tsb.WriteString(fmt.Sprintf("%s{reg} = insertvalue %%vec {prev}, i64 %s, 2\\n", g.indent(), _p2i_{reg}))')
    content = pattern3.sub(replace3, content)
    
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
