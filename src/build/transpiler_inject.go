package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"

	"github.com/lizongying/nolang/i18n"
)

// injectLDFlags prepends synthetic LetStatement nodes for each -ld-KEY=VALUE pair
// to the program's statement list. These become compile-time global constants
// accessible exactly like top-level `name = value` declarations in source code.
//
// Value type inference mirrors inferTypeFromExpr:
//   - numeric → i64 or f64
//   - `true`/`false` → bool
//   - everything else → str (single-quoted string literal)
func injectLDFlags(program *parser.Program, ldFlags map[string]string) {
	if len(ldFlags) == 0 {
		return
	}
	// Build a set of existing top-level variable names so we can replace their
	// values in-place instead of injecting duplicates (which would trigger
	// validateDuplicates).
	existingVars := make(map[string]*parser.LetStatement)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			existingVars[ls.Name.Value] = ls
		}
	}

	// Build the list of variables to inject (sorted for deterministic order).
	keys := make([]string, 0, len(ldFlags))
	for k := range ldFlags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// buildExpr converts a -ld value string into the appropriate literal AST node.
	buildExpr := func(val string) parser.Expression {
		if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
			return &parser.IntegerLiteral{
				Token: lexer.Token{Type: lexer.INT, Literal: val, Line: 0, Column: 0},
				Value: intVal,
				Raw:   val,
			}
		} else if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return &parser.FloatLiteral{
				Token: lexer.Token{Type: lexer.FLOAT, Literal: val, Line: 0, Column: 0},
				Value: floatVal,
				Raw:   val,
			}
		} else if val == "true" {
			return &parser.BooleanLiteral{
				Token: lexer.Token{Type: lexer.TRUE, Literal: val, Line: 0, Column: 0},
				Value: true,
			}
		} else if val == "false" {
			return &parser.BooleanLiteral{
				Token: lexer.Token{Type: lexer.FALSE, Literal: val, Line: 0, Column: 0},
				Value: false,
			}
		}
		// Treat as string literal (single-quoted in Nolang).
		return &parser.StringLiteral{
			Token: lexer.Token{Type: lexer.STRING, Literal: val, Raw: "'" + val + "'", Line: 0, Column: 0},
			Value: val,
			Raw:   "'" + val + "'",
		}
	}

	// synthetic accumulates statements for variables that don't exist in source.
	synthetic := make([]parser.Statement, 0, len(keys))
	for _, name := range keys {
		val := ldFlags[name]
		expr := buildExpr(val)
		if ls, ok := existingVars[name]; ok {
			// Source already declares this variable: replace its value in-place.
			ls.Value = expr
			ls.IsSynthetic = true
		} else {
			// Variable not in source: inject a new synthetic declaration.
			synthetic = append(synthetic, &parser.LetStatement{
				Token:       lexer.Token{Type: lexer.IDENT, Literal: name, Line: 0, Column: 0},
				Name:        &parser.Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: name, Line: 0, Column: 0}, Value: name},
				Value:       expr,
				IsSynthetic: true,
			})
		}
	}
	// Prepend only genuinely new variables before existing statements.
	if len(synthetic) > 0 {
		program.Statements = append(synthetic, program.Statements...)
	}
}

// stripBuiltinStubs removes FunctionDefinitions marked with the #{buildin=...}
// annotation from the program. Such stubs are documentation-only declarations for
// standard-library builtins whose real implementation lives in the Go runtime; their
// nolang body (typically empty `{}`) must not be validated or codegen'd. The compiler
// simply skips them ("遇到這個註解不處理"). The real builtin resolution always goes
// through the Go BuiltinMethod table, so dropping the stub from the program is safe.
func stripBuiltinStubs(prog *parser.Program) {
	if prog == nil {
		return
	}
	kept := prog.Statements[:0]
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.BuiltinStub {
			// 記錄被剝離的內建樁函式名稱，使後續校驗（RunAllLints）即便在
			// 同名多載延續定義仍保留的情況下，仍能依名稱跳過其返回值校驗。
			if prog.BuiltinFuncNames == nil {
				prog.BuiltinFuncNames = make(map[string]bool)
			}
			prog.BuiltinFuncNames[fd.Name] = true
			continue
		}
		kept = append(kept, stmt)
	}
	prog.Statements = kept
}

// declTypeName 解析一條變數宣告的**有效型別名**，供 enter/leave 注入判定。
//
// 只看源碼寫下的標註（LetStatement.Type）是不夠的：`no fmt` 會主動移除「標註
// 型別 == 推斷型別」的冗餘標註（src/cmd/no 的 fixRedundantTypeSource，與
// `--fix=redundant` 同源），於是
//
//	d db-sqlite = db-sqlite{handle: h}   →   d = db-sqlite{handle: h}
//
// 這兩者在型別上等價（標註本來就是冗餘的），但注入器若只認 LetStatement.Type，
// 改寫後 `d.enter()` / `d.leave()` 整段消失——連線不再自動關閉，而且是**靜默**的：
// 編譯通過、行為錯誤。排版不得改變語義，所以這裡必須把推斷得到的型別一起納入。
//
// 解析順序：
//  1. s.Type —— 顯式標註，以及 parser 為多數推斷形態寫入的 inferred 型別
//     （IntegerLiteral / StringLiteral / IndexExpression / CallExpression …
//     都會寫，只是 IsInferred=true；String() 仍是正確的型別名，故
//     `d = open()` 這種「呼叫推斷」在去掉標註後行為不變）；
//  2. 結構體字面量 —— `d = db-sqlite{…}` 是 parser **不**寫 s.Type 的常見形態
//     （StructLiteral 分支只登記 VarTypes 供方法解析），得從字面量自帶的型別名取；
//     漏掉它正是「`{ }` 圈起來的資源不關閉」那類靜默洩漏的來源；
//  3. 語義表 —— 兜底（如 `x = y` 的型別傳播），按**函式作用域**查，避免不同函式
//     的同名變數互撞。
func declTypeName(program *parser.Program, funcName string, s *parser.LetStatement) string {
	if s == nil || s.Name == nil {
		return ""
	}
	if s.Type != nil {
		if n := s.Type.String(); n != "" {
			return n
		}
	}
	if sl, ok := s.Value.(*parser.StructLiteral); ok && sl != nil && sl.Type != "" {
		return sl.Type
	}
	if program != nil && program.Sem != nil && funcName != "" {
		if t, ok := program.Sem.FuncVarType(funcName, s.Name.Value); ok && t != "" {
			return t
		}
	}
	return ""
}

// injectEnterLeave 為實現了 enter()/leave() 的類型自動插入作用域調用
func injectEnterLeave(program *parser.Program) {
	// 1. 收集實現了 enter/leave 的類型
	hasEnter := make(map[string]bool)
	hasLeave := make(map[string]bool)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		// 方法名格式：TypeName.methodName
		dotIdx := -1
		for i := len(fd.Name) - 1; i >= 0; i-- {
			if fd.Name[i] == '.' {
				dotIdx = i
				break
			}
		}
		if dotIdx < 0 {
			continue
		}
		typeName := fd.Name[:dotIdx]
		methodName := fd.Name[dotIdx+1:]
		if methodName == "enter" {
			hasEnter[typeName] = true
		} else if methodName == "leave" {
			hasLeave[typeName] = true
		}
	}
	if len(hasEnter) == 0 && len(hasLeave) == 0 {
		return // 沒有類型需要處理
	}
	// 找出既有 enter 又有 leave 的類型
	lifecycleTypes := make(map[string]bool)
	for t := range hasEnter {
		lifecycleTypes[t] = true
	}
	for t := range hasLeave {
		lifecycleTypes[t] = true
	}
	// 2. 遍歷所有函數體，注入 enter/leave
	//
	// scopeVars 與 scopeTypes 平行成長：宣告當下就把「解析出來的有效型別名」記進
	// scopeTypes，塊尾倒序插入 leave 時直接查它。舊版靠 findTypeForVar 回頭掃
	// block 裡的 LetStatement，而該函式在「宣告沒有 Type」時會落到「集合裡隨便挑
	// 一個 lifecycle 型別」的保底分支——那正是下面 declTypeName 要修的根源。
	var walkBlock func(funcName string, block *parser.BlockStatement, inScope []string, inScopeTypes []string)
	walkBlock = func(funcName string, block *parser.BlockStatement, inScope []string, inScopeTypes []string) {
		var newStmts []parser.Statement
		scopeVars := make([]string, len(inScope))
		copy(scopeVars, inScope)
		scopeTypes := make([]string, len(inScopeTypes))
		copy(scopeTypes, inScopeTypes)
		depth := len(inScope)
		for _, stmt := range block.Statements {
			newStmts = append(newStmts, stmt)
			switch s := stmt.(type) {
			case *parser.LetStatement:
				typeName := declTypeName(program, funcName, s)
				if lifecycleTypes[typeName] {
					varName := s.Name.Value
					// 只有**真的實作了 enter** 才插 enter()。lifecycleTypes 是
					// hasEnter ∪ hasLeave 的聯集，若拿它直接當條件，只實作 leave
					// 的型別會被插入一個不存在的 enter() 呼叫，整個檔案在 codegen
					// 階段直接失敗（EmitLLVM: unknown callee T.enter）——
					// 而 `src/std/leave.no` 的說明正是「提供 T.leave 即可」。
					// 2026-11-12 修；迴歸：test/std/leave.no 的 'leave only type'。
					if hasEnter[typeName] {
						newStmts = append(newStmts, makeMethodCall(varName, "enter"))
					}
					// leave 需要的登記不能省：scopeVars 是塊尾反向插入的依據。
					scopeVars = append(scopeVars, varName)
					scopeTypes = append(scopeTypes, typeName)
				}
			case *parser.ReturnStatement:
				// 不在 return 前插 leave()：本語言的作用域退出清理由各層區塊尾部的
				// leave() 承擔，`return` 跳出時它們照樣會被執行（含函式最外層區塊）。
				// 原本這裡再插一遍，實測造成同一變數 leave 兩次、甚至三次：
				//   普通 return            ⇒ LhLh
				//   巢狀區塊裡 return      ⇒ LiLoLiLo（全部兩遍）
				//   迴圈體內 return        ⇒ LiLoLi
				// 對 `fs.close()` 這類 leave 來說，重複關閉就是真 bug。
				// 2026-11-12 移除；迴歸：test/std/leave.no 的 'leave before return'。
			case *parser.ForStatement:
				if s.Body != nil {
					walkBlock(funcName, s.Body, scopeVars, scopeTypes)
				}
			case *parser.BlockStatement:
				// 裸塊 `{ ... }` 是本語言最常用的「顯式作用域」寫法
				// （`sql.db` 這類合併 enter/leave 的型別就靠它限定生命週期）。
				// 漏掉這一支時，塊內的宣告完全不會被注入 enter/leave，
				// 也就是說「用 `{ }` 圈起來的資源不會自動關閉」——
				// 症狀完全靜默（編譯通過、行為錯誤），2026-11-12 修。
				// 迴歸：test/std/enter.no 的 'enter nested blocks'。
				walkBlock(funcName, s, scopeVars, scopeTypes)
			case *parser.BlockMatchStatement:
				// `{ body }: { arms }` 的 body 同樣是一個作用域。
				if s.Body != nil {
					walkBlock(funcName, s.Body, scopeVars, scopeTypes)
				}
			case *parser.ExpressionStatement:
				if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
					if ifExpr.Consequence != nil {
						walkBlock(funcName, ifExpr.Consequence, scopeVars, scopeTypes)
					}
					if ifExpr.Alternative != nil {
						walkBlock(funcName, ifExpr.Alternative, scopeVars, scopeTypes)
					}
				}
			}
		}
		// 區塊結尾插入 leave()（反向）
		if len(scopeVars) > depth {
			for i := len(scopeVars) - 1; i >= depth; i-- {
				if hasLeave[scopeTypes[i]] {
					newStmts = append(newStmts, makeMethodCall(scopeVars[i], "leave"))
				}
			}
		}
		block.Statements = newStmts
	}
	// 遍歷頂層函數和區塊
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if s.Body != nil {
				walkBlock(s.Name, s.Body, nil, nil)
			}
		}
	}
}

// processEmbeds 遍歷 program 中帶有 #{embed=...} 註解的 LetStatement，
// 在編譯期讀取指定文件並填充 EmbedData 字段。
// 路徑相對於包根目錄（package.noml 所在目錄）解析。
func (t *Transpiler) processEmbeds(program *parser.Program, sourcePath string) error {
	for _, stmt := range program.Statements {
		ls, ok := stmt.(*parser.LetStatement)
		if !ok {
			continue
		}
		// 查找 embed 註解（經由 side-table）
		var embedPath string
		for _, annot := range program.Sem.AnnotationsOf(ls) {
			if annot.Key != "embed" {
				continue
			}
			if sv, ok := annot.Value.(*parser.AnnotationStringValue); ok {
				embedPath = sv.Value
			} else if iv, ok := annot.Value.(*parser.AnnotationIdentValue); ok {
				embedPath = iv.Value
			}
		}
		if embedPath == "" {
			continue
		}
		// 解析路徑：與 import 路徑一致，前置 "/" 表示相對於工作區根目錄
		embedRel := strings.TrimPrefix(embedPath, "/")
		resolvedPath := filepath.Join(pkg.ResolveEmbedBase(sourcePath), embedRel)
		// 檢測路徑是文件還是目錄
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return i18n.Errorf("t22pcf4p", "embed: file not found: %s (resolved: %s)", embedPath, resolvedPath)
		}
		if info.IsDir() {
			// 文件夾嵌入：遞迴讀取所有文件
			files := make(map[string][]byte)
			err = filepath.Walk(resolvedPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi.IsDir() {
					return nil
				}
				data, rerr := os.ReadFile(path)
				if rerr != nil {
					return rerr
				}
				// 以相對於嵌入目錄的路徑作為鍵
				rel, rerr := filepath.Rel(resolvedPath, path)
				if rerr != nil {
					rel = path
				}
				// 統一為正斜槓（跨平台一致）
				rel = filepath.ToSlash(rel)
				files[rel] = data
				return nil
			})
			if err != nil {
				return i18n.Errorf("x649sxw4", "embed: failed to read directory %s: %w", resolvedPath, err)
			}
			if len(files) == 0 {
				return i18n.Errorf("so08hhvh", "embed: directory is empty: %s (resolved: %s)", embedPath, resolvedPath)
			}
			program.Sem.SetEmbedFiles(ls, files)
		} else {
			// 單個文件嵌入：讀取文件字節
			data, err := os.ReadFile(resolvedPath)
			if err != nil {
				return i18n.Errorf("t22pcf4p", "embed: file not found: %s (resolved: %s)", embedPath, resolvedPath)
			}
			program.Sem.SetEmbedData(ls, data)
		}
	}
	return nil
}

// findPackageRootFromFile walks up from a file path to find the directory containing package.noml.
func findPackageRootFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		cfgFile := filepath.Join(dir, pkg.PackageManifestName)
		if _, err := os.Stat(cfgFile); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func dumpMergedLeak(merged *parser.Program, stage string) {
	if os.Getenv("NOLANG_DEBUG_SELF") == "" {
		return
	}
	out2 := 0
	fdBody := -1
	total := len(merged.Statements)
	for _, stmt := range merged.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil && ls.Name.Value == "out2" {
			out2++
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == "txt.from-hex" {
			if fd.Body != nil {
				fdBody = len(fd.Body.Statements)
			} else {
				fdBody = -2
			}
		}
	}
	fmt.Fprintf(os.Stderr, "[debug-self][%s] total=%d top-level out2=%d txt.from-hex fdBody=%d\n", stage, total, out2, fdBody)
}
