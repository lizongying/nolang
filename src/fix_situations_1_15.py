import re

with open('build/llvm/stmt.go', 'r') as f:
    content = f.read()

# Pattern for situation 1 (global var) and 1.5 (func param) - they're identical except context
# We need to replace the canClone logic in both

old_pattern_1 = """\t\t\t\t\t\tcanClone := true
\t\t\t\t\t\tif (srcType == "%vec" || srcType == "%arr") &&
\t\t\t\t\t\t\t(srcElemType == "%vec" || srcElemType == "%arr") {
\t\t\t\t\t\t\tcanClone = false
\t\t\t\t\t\t}
\t\t\t\t\t\tif srcType != "%vec" && srcType != "%arr" && srcType != "%str-long" {
\t\t\t\t\t\t\tif !g.canDeepCloneStruct(srcType) {
\t\t\t\t\t\t\t\tcanClone = false
\t\t\t\t\t\t\t}
\t\t\t\t\t\t}
\t\t\t\t\t\tif canClone && (isLocal || isOutput || isGlobal) {
\t\t\t\t\t\t\tg.freeOldHeapValue(sb, stmt, name)
\t\t\t\t\t\t\tg.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcType, srcElemType)"""

new_pattern_1 = """\t\t\t\t\t\tcanClone := true
\t\t\t\t\t\tsrcElemElemType := ""
\t\t\t\t\t\tif g.elemElemTypes != nil {
\t\t\t\t\t\t\tsrcElemElemType = g.elemElemTypes[ident.Value]
\t\t\t\t\t\t}
\t\t\t\t\t\t// 所有型別均可深層 clone：嵌套容器透過 elemElemType 機制遞迴處理，
\t\t\t\t\t\t// 用戶結構體透過 emitStructClone 遞迴處理。
\t\t\t\t\t\tif canClone && (isLocal || isOutput || isGlobal) {
\t\t\t\t\t\t\tg.freeOldHeapValue(sb, stmt, name)
\t\t\t\t\t\t\tg.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcType, srcElemType, srcElemElemType)"""

# Count occurrences
count = content.count(old_pattern_1)
print(f"Pattern 1 found {count} times")

if count == 2:
    # Also need to add elemElemType propagation after srcElemType propagation
    # Find the pattern: after srcElemType block, before return
    old_propagate = """\t\t\t\t\t\t\t\treturn
\t\t\t\t\t\t\t}
\t\t\t\t\t\t}
\t\t\t\t\t}
\t\t\t\t}"""
    
    # Actually, let's just replace the canClone logic and emitDeepClone call
    content = content.replace(old_pattern_1, new_pattern_1)
    
    # Now add elemElemType propagation after srcElemType propagation blocks
    # Pattern: the srcElemType propagation block followed by return
    old_prop = """\t\t\t\t\t\t\t\tif isGlobal && g.moduleArrayElemTypes != nil {
\t\t\t\t\t\t\t\t\tg.moduleArrayElemTypes[name] = srcElemType
\t\t\t\t\t\t\t\t}
\t\t\t\t\t\t\t}
\t\t\t\t\t\t\treturn"""
    new_prop = """\t\t\t\t\t\t\t\tif isGlobal && g.moduleArrayElemTypes != nil {
\t\t\t\t\t\t\t\t\tg.moduleArrayElemTypes[name] = srcElemType
\t\t\t\t\t\t\t\t}
\t\t\t\t\t\t\t}
\t\t\t\t\t\t\tif srcElemElemType != "" {
\t\t\t\t\t\t\t\tif g.elemElemTypes != nil {
\t\t\t\t\t\t\t\t\tg.elemElemTypes[name] = srcElemElemType
\t\t\t\t\t\t\t\t}
\t\t\t\t\t\t\t\tif isGlobal && g.moduleElemElemTypes != nil {
\t\t\t\t\t\t\t\t\tg.moduleElemElemTypes[name] = srcElemElemType
\t\t\t\t\t\t\t\t}
\t\t\t\t\t\t\t}
\t\t\t\t\t\t\treturn"""
    
    count_prop = content.count(old_prop)
    print(f"Propagate pattern found {count_prop} times")
    
    if count_prop == 2:
        content = content.replace(old_prop, new_prop)
    else:
        print("WARNING: propagate pattern not found expected number of times")
    
    with open('build/llvm/stmt.go', 'w') as f:
        f.write(content)
    print("Done with situations 1 and 1.5")
else:
    print(f"ERROR: Expected 2 occurrences, found {count}")
