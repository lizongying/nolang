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
	var walkBlock func(block *parser.BlockStatement, inScope []string)
	walkBlock = func(block *parser.BlockStatement, inScope []string) {
		var newStmts []parser.Statement
		scopeVars := make([]string, len(inScope))
		copy(scopeVars, inScope)
		for _, stmt := range block.Statements {
			newStmts = append(newStmts, stmt)
			switch s := stmt.(type) {
			case *parser.LetStatement:
				typeName := ""
				if s.Type != nil {
					typeName = s.Type.String()
				}
				if lifecycleTypes[typeName] {
					varName := s.Name.Value
					// 插入 varName.enter()
					newStmts = append(newStmts, makeMethodCall(varName, "enter"))
					scopeVars = append(scopeVars, varName)
				}
			case *parser.ReturnStatement:
				// 在 return 前插入 leave()
				for i := len(scopeVars) - 1; i >= 0; i-- {
					if hasLeave[findTypeForVar(scopeVars[i], block, lifecycleTypes)] {
						newStmts = append(newStmts, makeMethodCall(scopeVars[i], "leave"))
					}
				}
			case *parser.ForStatement:
				if s.Body != nil {
					walkBlock(s.Body, scopeVars)
				}
			case *parser.ExpressionStatement:
				if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
					if ifExpr.Consequence != nil {
						walkBlock(ifExpr.Consequence, scopeVars)
					}
					if ifExpr.Alternative != nil {
						walkBlock(ifExpr.Alternative, scopeVars)
					}
				}
			}
		}
		// 區塊結尾插入 leave()（反向）
		if len(scopeVars) > len(inScope) {
			for i := len(scopeVars) - 1; i >= len(inScope); i-- {
				if hasLeave[findTypeForVar(scopeVars[i], block, lifecycleTypes)] {
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
				walkBlock(s.Body, nil)
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
			return fmt.Errorf("embed: file not found: %s (resolved: %s)", embedPath, resolvedPath)
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
				return fmt.Errorf("embed: failed to read directory %s: %w", resolvedPath, err)
			}
			if len(files) == 0 {
				return fmt.Errorf("embed: directory is empty: %s (resolved: %s)", embedPath, resolvedPath)
			}
			program.Sem.SetEmbedFiles(ls, files)
		} else {
			// 單個文件嵌入：讀取文件字節
			data, err := os.ReadFile(resolvedPath)
			if err != nil {
				return fmt.Errorf("embed: file not found: %s (resolved: %s)", embedPath, resolvedPath)
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
