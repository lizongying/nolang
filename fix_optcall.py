#!/usr/bin/env python3
"""Fix the isNolangOptionCall detection for module-prefixed function calls."""

path = 'src/build/llvm/stmt.go'
with open(path, 'r') as f:
    content = f.read()

# The old code block - exact match from the file
old = '''					if recvType, ok := g.varTypes[recvIdent.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						// Map LLVM struct names to Nolang type names for function lookup
						if srcType == "str-long" {
							candidates = append(candidates, "str")
						}
						for _, cand := range candidates {
							candName := cand + "." + dot.Property
							if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
								isNolangOptionCall = true
								break
							}
						}
					}
				} else if _, isStrLit := recv.(*parser.StringLiteral); isStrLit {'''

# The new code block with else branch for module name receivers
new = '''					if recvType, ok := g.varTypes[recvIdent.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						// Map LLVM struct names to Nolang type names for function lookup
						if srcType == "str-long" {
							candidates = append(candidates, "str")
						}
						for _, cand := range candidates {
							candName := cand + "." + dot.Property
							if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
								isNolangOptionCall = true
								break
							}
						}
					} else {
						// Receiver is a module name (not a variable), e.g.
						// json-util.extract-str(msg, 'content').
						// Check the full qualified name in funcResultLLVMType.
						fullName := recvIdent.Value + "." + dot.Property
						if ts, ok := g.funcResultLLVMType[fullName]; ok && len(ts) == 1 && ts[0] == "%option" {
							isNolangOptionCall = true
						}
					}
				} else if _, isStrLit := recv.(*parser.StringLiteral); isStrLit {'''

if old not in content:
    print('ERROR: old string not found in file')
    # Try to find a partial match
    idx = content.find('if recvType, ok := g.varTypes[recvIdent.Value]; ok {')
    if idx >= 0:
        print(f'Found partial match at index {idx}')
        print(repr(content[idx:idx+300]))
    exit(1)

content = content.replace(old, new, 1)

with open(path, 'w') as f:
    f.write(content)

print('SUCCESS: Applied fix for module-prefixed option call detection')
