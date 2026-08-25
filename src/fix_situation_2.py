with open('build/llvm/stmt.go', 'r') as f:
    content = f.read()

# Situation 2: local heap var clone path
# Replace canClone logic in the clone path (after canMove)
old_s2 = """\t\t\t\t\t// clone：深層 clone（源仍需拥有 data）
\t\t\t\t\tcanClone := true
\t\t\t\t\tif (srcHeapType == "%vec" || srcHeapType == "%arr") &&
\t\t\t\t\t\t(srcElemType == "%vec" || srcElemType == "%arr") {
\t\t\t\t\t\tcanClone = false
\t\t\t\t\t}
\t\t\t\t\tif srcHeapType != "%vec" && srcHeapType != "%arr" && srcHeapType != "%str-long" {
\t\t\t\t\t\tif !g.canDeepCloneStruct(srcHeapType) {
\t\t\t\t\t\t\tcanClone = false
\t\t\t\t\t\t}
\t\t\t\t\t}
\t\t\t\t\tif canClone {
\t\t\t\t\t\tg.freeOldHeapValue(sb, stmt, name)
\t\t\t\t\t\tg.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcHeapType, srcElemType)"""

new_s2 = """\t\t\t\t\t// clone：深層 clone（源仍需拥有 data）
\t\t\t\t\tcanClone := true
\t\t\t\t\tsrcElemElemType := ""
\t\t\t\t\tif g.elemElemTypes != nil {
\t\t\t\t\t\tsrcElemElemType = g.elemElemTypes[ident.Value]
\t\t\t\t\t}
\t\t\t\t\t// 所有型別均可深層 clone：嵌套容器透過 elemElemType 機制遞迴處理，
\t\t\t\t\t// 用戶結構體透過 emitStructClone 遞迴處理。
\t\t\t\t\tif canClone {
\t\t\t\t\t\tg.freeOldHeapValue(sb, stmt, name)
\t\t\t\t\t\tg.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcHeapType, srcElemType, srcElemElemType)"""

count_s2 = content.count(old_s2)
print(f"Situation 2 pattern found {count_s2} times")

if count_s2 == 1:
    content = content.replace(old_s2, new_s2)
    
    # Now handle the forced move path removal
    # The old code has: if canClone { ... return } followed by forced move path
    # We need to remove the forced move path and the if canClone wrapper
    
    # Find the forced move path after the clone return
    old_forced = """\t\t\t\t\t\t\treturn
\t\t\t\t\t\t}
\t\t\t\t\t\t}
\t\t\t\t\t\t// P1-5 退化路径：canClone==false（巢狀容器等無法深層 clone）。
\t\t\t\t\t\t// forced move：浅拷贝 + 标记源为 moved，防止 double-free。
\t\t\t\t\t\t// 源後續若被引用，值為已 moved 狀態（數據已轉移），但優於 double-free 崩潰。
\t\t\t\t\t\tif !isOutput {
\t\t\t\t\t\t\tg.freeOldHeapValue(sb, stmt, name)
\t\t\t\t\t\t}
\t\t\t\t\t\tfmoveReg := g.tmpReg("fmove.val")
\t\t\t\t\t\tsb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\\n",
\t\t\t\t\t\t\tg.indent(), fmoveReg, srcHeapType, srcHeapType, g.varAddr(ident.Value)))
\t\t\t\t\t\tsb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\\n",
\t\t\t\t\t\t\tg.indent(), srcHeapType, fmoveReg, srcHeapType, g.varAddr(name)))
\t\t\t\t\t\tif isOutput {
\t\t\t\t\t\t\tg.handleMoveToOut(sb, ident.Value, name)
\t\t\t\t\t\t} else {
\t\t\t\t\t\t\tg.handleMoveLocal(sb, ident.Value)
\t\t\t\t\t\t\tif !isGlobal {
\t\t\t\t\t\t\t\tg.trackLocalHeapVar(name, srcHeapType)
\t\t\t\t\t\t\t}
\t\t\t\t\t\t}
\t\t\t\t\t\tif srcElemType != "" {
\t\t\t\t\t\t\tif g.arrayElemTypes != nil {
\t\t\t\t\t\t\t\tg.arrayElemTypes[name] = srcElemType
\t\t\t\t\t\t\t}
\t\t\t\t\t\t\tif isGlobal && g.moduleArrayElemTypes != nil {
\t\t\t\t\t\t\t\tg.moduleArrayElemTypes[name] = srcElemType
\t\t\t\t\t\t\t}
\t\t\t\t\t\t}
\t\t\t\t\treturn
\t\t\t\t}"""
    
    new_forced = """\t\t\t\t\t\t\treturn
\t\t\t\t\t\t}
\t\t\t\t\t}"""
    
    count_forced = content.count(old_forced)
    print(f"Forced move pattern found {count_forced} times")
    
    if count_forced == 1:
        content = content.replace(old_forced, new_forced)
    else:
        print("WARNING: forced move pattern not found!")
    
    with open('build/llvm/stmt.go', 'w') as f:
        f.write(content)
    print("Done with situation 2")
else:
    print(f"ERROR: Expected 1 occurrence, found {count_s2}")
