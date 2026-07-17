import re, os

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    original = content
    
    # 1. Replace type checks: "%str-long" || X == "%str-short" → "%str-long"
    content = re.sub(r'"%str-long" \|\| \w+ == "%str-short"', '"%str-long"', content)
    content = re.sub(r'\w+ == "%str-long" \|\| \w+ == "%str-short"', lambda m: m.group(0).split(' || ')[0], content)
    
    # 2. Replace srcType checks: "str-short" || "str-long" → "str-long"
    content = re.sub(r'\w+ == "str-short" \|\| \w+ == "str-long"', lambda m: m.group(0).split(' || ')[0], content)
    content = re.sub(r'\w+ == "str-long" \|\| \w+ == "str-short"', lambda m: m.group(0).split(' || ')[0], content)
    
    # 3. Replace: if srcType == "str-short" { ... } → remove (dead code)
    # This is tricky because we need to remove the entire if block
    # For now, just replace the condition with "false"
    content = re.sub(r'if \w+ == "%str-short" \{', 'if false {', content)
    content = re.sub(r'if \w+ == "str-short" \{', 'if false {', content)
    
    # 4. Replace: convertShortToLong(sb, X) → X
    content = re.sub(r'g\.convertShortToLong\(sb, ([^)]+)\)', r'\1', content)
    
    # 5. Replace: convertStrLongLitToLongValue(sb, X) → X
    content = re.sub(r'g\.convertStrLongLitToLongValue\(sb, ([^)]+)\)', r'\1', content)
    
    # 6. Replace: g.extractStrShortDataPtr(sb, X) → g.extractStrDataPtr(sb, X)
    content = re.sub(r'g\.extractStrShortDataPtr\(sb, ([^)]+)\)', r'g.extractStrDataPtr(sb, \1)', content)
    
    # 7. Replace: g.extractStrShortLen(sb, X) → g.extractStrLen(sb, X)
    content = re.sub(r'g\.extractStrShortLen\(sb, ([^)]+)\)', r'g.extractStrLen(sb, \1)', content)
    
    # 8. Replace len(a.Value) <= 127 checks that gate str-short path
    # "if len(a.Value) <= 127 {" → remove (always use str-long)
    # These are typically followed by ev = g.convertShortToLong(sb, ev) which we already replaced
    # So now they're just: if len(a.Value) <= 127 { ev = ev } which is a no-op
    # Let's remove these if blocks entirely
    content = re.sub(r'if len\([a-zA-Z.]+\.Value\) <= 127 \{\n\s*\w+ = \w+\n\s*\}', '', content)
    
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
