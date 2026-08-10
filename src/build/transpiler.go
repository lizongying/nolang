package build
import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/build/llvm"
	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)
// mangleOverloads 對同名函數進行名稱修飾，並更新調用點
func mangleOverloads(program *parser.Program, varTypes map[string]string) {
	// 1. 構建重載表
	overloads := make(map[string][]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			overloads[fd.Name] = append(overloads[fd.Name], fd)
		}
	}
	// 2. 對需要修飾的函數生成新名稱
	mangled := make(map[string]string) // 原始調用簽名 → 修飾後名稱
	// 記錄需要從 program.Statements 中刪除的重複函數
	toRemove := make(map[*parser.FunctionDefinition]bool)
	for name, fns := range overloads {
		if len(fns) <= 1 {
			continue // 無重載，不改名
		}
		// 去重：對於相同名稱+簽名+平台組合的重複定義（多模組同函數），只保留第一個。
		// 不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）的定義視為不同函數，不去重。
		seenSigs := make(map[string]bool)
		uniqueFns := make([]*parser.FunctionDefinition, 0, len(fns))
		for _, fd := range fns {
			sig := platformAwareCallSignature(name, fd.Parameters, program.Sem.PlatformKeysOf(fd))
			if !seenSigs[sig] {
				seenSigs[sig] = true
				uniqueFns = append(uniqueFns, fd)
			} else {
				toRemove[fd] = true
			}
		}
		// 用 uniqueFns 取代 fns 進行後續處理
		fns = uniqueFns
		overloads[name] = uniqueFns
		// 去重後若僅剩單一函數（同簽名重複定義），無需改名
		if len(uniqueFns) <= 1 {
			continue
		}
		for _, fd := range fns {
			parts := []string{name}
			for _, p := range fd.Parameters {
				parts = append(parts, sanitizeTypeForName(p.Type.String()))
			}
			mangledName := strings.Join(parts, "_")
			fd.Name = mangledName // 直接修改 AST
			sig := callSignature(name, fd.Parameters)
			mangled[sig] = mangledName
		}
	}
	// 從 program.Statements 中刪除重複的函數定義
	if len(toRemove) > 0 {
		filtered := make([]parser.Statement, 0, len(program.Statements))
		removedCount := 0
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if toRemove[fd] {
					removedCount++
					continue
				}
			}
			filtered = append(filtered, stmt)
		}
		program.Statements = filtered
	}
	if len(mangled) == 0 {
		return // 沒有重載，無需遍歷
	}
	// 3. 遍歷所有語句，更新 CallExpression 的函數名
	var walk func(stmts []parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *parser.ExpressionStatement:
				updateCallNames(s.Expression, overloads, mangled, varTypes)
			case *parser.LetStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
				}
			case *parser.FunctionDefinition:
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.BlockStatement:
				walk(s.Statements)
			case *parser.ForStatement:
				if s.Condition != nil {
					updateCallNames(s.Condition, overloads, mangled, varTypes)
				}
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.MultiAssignStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
				}
			case *parser.ReturnStatement:
				if s.ReturnValue != nil {
					updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
				}
			}
		}
	}
	walk(program.Statements)
	// 也用於回退查找（無參數類型匹配時的前端保底）
	_ = varTypes
}
// callSignature 生成調用簽名 key，用於查找
func callSignature(name string, params []*parser.Parameter) string {
	parts := []string{name}
	for _, p := range params {
		parts = append(parts, sanitizeTypeForName(p.Type.String()))
	}
	return strings.Join(parts, "_")
}
// platformAwareCallSignature 生成包含平台標註的調用簽名 key。
// 用於 mangleOverloads 去重：不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）
// 的同名同參數函數視為不同函數，不應被去重。
// platformKeys 為空表示平台通用宣告。
func platformAwareCallSignature(name string, params []*parser.Parameter, platformKeys []string) string {
	sig := callSignature(name, params)
	if len(platformKeys) == 0 {
		return sig + "\x00" // 通用平台用 \x00 後綴與特定平台區分
	}
	// 排序平台 key 以確保不同順序的相同組合產生相同簽名
	sorted := make([]string, len(platformKeys))
	copy(sorted, platformKeys)
	sort.Strings(sorted)
	return sig + "\x00" + strings.Join(sorted, ",")
}
// sanitizeTypeForName 將型別字串轉成 LLVM 識別符安全的形式：
// - "[]byte"   → "slice.byte"
// - "?i64"     → "opt.i64"
// - "[4]i64"   → "arr4.i64"
// - "ptr i64"  → "ptr.i64"
func sanitizeTypeForName(s string) string {
	r := strings.NewReplacer(
		"[]", "slice.",
		"?", "opt.",
		"ptr ", "ptr.",
		"[", "arr",
		"]", ".",
		" ", "_",
		"|", "-",
	)
	return r.Replace(s)
}
                                                                                                                             
                 
           
  
                          
                             
                                                        
                        
                                                                        
                
   
              
                           
              
                            
              
                             
               
                          
               
                          
               
                           
                 
                         
                                     
                                                           
                                   
               
    
           
   
                           
                             
                          
                                                       
                                                
                                    
                           
                                 
      
     
    
                                                    
                                                         
                                                                                      
                                                                                     
                                          
                 
     
                  
    
                                                                                    
                                                                          
                       
                                                                                
                
    
            
   
                                                     
                                                        
                      
                                                         
                             
                                                  
                        
                                                                
                        
     
                                                                
                                                                     
                    
                                                                       
                                                                                 
                                  
                                                                                                 
                                                           
                                       
                                                             
                                                          
                            
        
       
      
     
                                                                  
                                                                                            
                                                             
             
    
                      
                                                                                            
                                                                                             
                                                                          
                                      
                          
                   
                  
                   
                  
                   
                  
                    
                   
                   
                  
                   
                  
      
     
                                               
                                                         
                   
     
                                                                          
                                                 
                                                         
                                 
      
     
                                                                                    
                                                                                       
                                                                                           
                                                                                                                  
                         
                                             
                 
                                   
                    
                  
                 
     
    
            
    
                                                                                               
                                                                                           
                                                         
                                                 
                                             
                               
                  
      
                          
                                                                                  
                  
      
     
    
   
                                                                                                   
                                                                                      
           
                              
                                                                                        
                                                                                                                                    
                     
                                                    
                
                          
                                    
                                                                   
                      
                   
    
           
                                
                                                                                       
                                                                 
                                                                  
                                                  
                  
   
           
         
           
  
                             
                                            
                    
                         
   
           
                               
                        
                
   
                                                  
                                                              
                            
                                                            
                                                         
                                    
                      
                                                       
                             
                        
                                                         
                 
     
    
                      
                                                           
                                                 
                      
      
     
    
   
           
                              
                                                                        
           
                              
                                                      
                    
                                                                   
                                        
                                                                                   
                                   
     
    
                         
                
    
                  
   
           
                                
                                                                   
                                    
                                               
                                                                                
                                                                                
                                                                  
                         
   
                            
                         
   
           
                            
                                                                 
                   
                
   
                                                                                                     
           
                           
                                                            
                          
                                                                          
                     
                                                          
   
  
           
                           
                                                           
                          
                                                                          
                     
                                       
   
  
           
                              
                                                                                    
                                                           
             
                         
                                                                               
                       
                            
   
                                                      
                       
                                                                          
                                                                            
                                      
                                        
    
   
           
         
              
  
 
// updateCallNames 遞迴更新 CallExpression 中的函數名
func updateCallNames(expr parser.Expression, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {
	switch e := expr.(type) {
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			name := ident.Value
			if fns, has := overloads[name]; has && len(fns) >= 1 {
				// 收集實參類型
				argTypes := make([]string, len(e.Arguments))
				for i, arg := range e.Arguments {
					t := checker.InferExprType(arg, varTypes, nil, "")
					if t == "" {
						// 無法推斷類型，使用第一個重載
						if i < len(fns[0].Parameters) {
							t = fns[0].Parameters[i].Type.String()
						} else {
							t = "i64"
						}
					}
					argTypes[i] = t
				}
				// 查找匹配的重載
				parts := []string{name}
				for _, t := range argTypes {
					parts = append(parts, sanitizeTypeForName(t))
				}
				sig := strings.Join(parts, "_")
				if mangledName, ok := mangled[sig]; ok {
					ident.Value = mangledName
				} else {
					// 找不到精確匹配，嘗試最接近的重載（取第一個）
					if len(fns) > 0 {
						ident.Value = fns[0].Name
					}
				}
			}
		} else {
			// 方法調用 (receiver.method(args))：e.Function 是 DotExpression
			// 遞迴處理 receiver 中的嵌套調用（如 count(0).to-str()）
			updateCallNames(e.Function, overloads, mangled, varTypes)
		}
		// 遞迴處理參數中的嵌套調用
		for _, arg := range e.Arguments {
			updateCallNames(arg, overloads, mangled, varTypes)
		}
	case *parser.InfixExpression:
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Right, overloads, mangled, varTypes)
	case *parser.PrefixExpression:
		updateCallNames(e.Right, overloads, mangled, varTypes)
	case *parser.DotExpression:
		// receiver.property：遞迴處理 receiver 中的嵌套調用
		updateCallNames(e.Receiver, overloads, mangled, varTypes)
	case *parser.IndexExpression:
		// arr[idx]：遞迴處理索引表達式中的嵌套調用
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Index, overloads, mangled, varTypes)
	case *parser.ConditionalExpression:
		// cond ? a : b：遞迴處理所有子表達式
		updateCallNames(e.Condition, overloads, mangled, varTypes)
		updateCallNames(e.Consequence, overloads, mangled, varTypes)
		updateCallNames(e.Alternative, overloads, mangled, varTypes)
	case *parser.IfExpression:
		if e.Condition != nil {
			updateCallNames(e.Condition, overloads, mangled, varTypes)
		}
		if e.Consequence != nil {
			for _, s := range e.Consequence.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
		if e.Alternative != nil {
			for _, s := range e.Alternative.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
	case *parser.GroupedExpression:
		updateCallNames(e.Expression, overloads, mangled, varTypes)
	}
}
func updateCallNamesInStmt(stmt parser.Statement, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		updateCallNames(s.Expression, overloads, mangled, varTypes)
	case *parser.LetStatement:
		if s.Value != nil {
			updateCallNames(s.Value, overloads, mangled, varTypes)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			updateCallNames(s.Value, overloads, mangled, varTypes)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
		}
	}
}
type Transpiler struct {
	llvmGenerator    *llvm.Generator
	pkg              *Package // 當前套件（用於路徑解析）
	sourcePath       string   // 當前編譯的源碼檔案路徑（用於 std 庫檢測）
	allowAnonymousFn bool     // 是否允許匿名函式型別參數（來自 package.jsonc）
	vetMode          bool     // vet 模式：只做語法+型別檢查，跳過 LLVM IR 生成
	// externFuncSigs/externStructFields: 預載入的跨文件函數簽名和 struct 欄位型別，
	// 注入到所有 parser 實例中以支援 let 型別推斷
	externFuncSigs     map[string][]string
	externStructFields map[string]map[string]string
	// targetGoos/targetGoarch: 編譯目標平台，用於平台變體過濾。
	// 空字串表示 fallback 到 runtime.GOOS/GOARCH（編譯主機平台）。
	targetGoos    string
	targetGoarch  string
	noBoundsCheck bool // skip bounds checks in generated code (unsafe mode)
	// fileCache: per-Transpiler AST 解析快取，消除單次 CompileTarget 內的重复解析。
	// 同一 std 模組檔案在一次編譯中最多被解析 4 次（preload/checker.ValidateFuncArgs/merge/auto-load），
	// 快取後降至 1 次。安全性：preload 和 checker.ValidateFuncArgs 只讀不修改 AST，
	// merge 步驟的 prefixModuleStatements/alias 改名是冪等的（已帶前綴的名稱會被跳過）。
	fileCache map[string]*parser.Program
}
func NewTranspiler(pkg *Package) *Transpiler {
	t := &Transpiler{
		llvmGenerator: llvm.NewGenerator(),
		pkg:           pkg,
	}
	if pkg != nil {
		t.allowAnonymousFn = pkg.Compiler.AnonymousFnType
	}
	return t
}
// SetTargetPlatform sets the target (GOOS, GOARCH) for platform-variant filtering
// during code generation. Empty strings fall back to the host runtime platform.
// This is propagated to the underlying LLVM generator before Generate is called.
func (t *Transpiler) SetTargetPlatform(goos, goarch string) {
	t.targetGoos = goos
	t.targetGoarch = goarch
}
// SetNoBoundsCheck configures whether bounds checks are skipped in generated code.
// When true (unsafe mode), array/slice/string indexing does not emit bounds checks.
func (t *Transpiler) SetNoBoundsCheck(skip bool) {
	t.noBoundsCheck = skip
}

// SetVetMode configures the transpiler to skip LLVM IR generation.
// When true, Compile/CompileTarget performs full front-end validation
// (parse, type-check, module merge, monomorphization) but returns before
// the expensive LLVM codegen step. Used by `no vet`.
func (t *Transpiler) SetVetMode(vet bool) {
	t.vetMode = vet
}
type Target int
const (
	TargetUnknown Target = iota
	TargetLLVM
)
func (t *Transpiler) parseFile(filePath string) (*parser.Program, error) {
	// 快取命中：同一檔案在單次編譯中可能被 resolveUse 調用多次
	if t.fileCache != nil {
		if cached, ok := t.fileCache[filePath]; ok {
			return cached, nil
		}
	}
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	l := lexer.NewCached(filePath, string(source))
	p := parser.New(l)
	p.AllowAnonymousFnType = t.allowAnonymousFn
	p.Filename = filepath.Base(filePath)
	// 注入預載入的跨文件簽名，支援 let 型別推斷
	if t.externFuncSigs != nil || t.externStructFields != nil {
		p.SetExternSignatures(t.externFuncSigs, t.externStructFields)
	}
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s: %v", filePath, p.Errors())
	}
	// 高危警告直出 stderr：單個 ; 行註釋疑似吞掉了同一行後面的代碼
	for _, w := range p.WarningsByCode(parser.WarnSemiSwallow) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if t.fileCache != nil {
		t.fileCache[filePath] = prog
	}
	return prog, nil
}
// parseEmbeddedProgram 從內嵌 FS 的位元組資料解析 Nolang 程式。
// 與 parseFile 平行，但來源為 embed.FS.ReadFile 的結果而非磁碟檔案。
// 用於 js/ 相容層模組解析（# js/<module>）。
func (t *Transpiler) parseEmbeddedProgram(filename string, data []byte) (*parser.Program, error) {
	// 快取命中：同一內嵌檔案在單次編譯中可能被 resolveUse 調用多次
	if t.fileCache != nil {
		if cached, ok := t.fileCache[filename]; ok {
			return cached, nil
		}
	}
	l := lexer.NewCached(filename, string(data))
	p := parser.New(l)
	p.AllowAnonymousFnType = t.allowAnonymousFn
	p.Filename = filepath.Base(filename)
	if t.externFuncSigs != nil || t.externStructFields != nil {
		p.SetExternSignatures(t.externFuncSigs, t.externStructFields)
	}
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s: %v", filename, p.Errors())
	}
	for _, w := range p.WarningsByCode(parser.WarnSemiSwallow) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if t.fileCache != nil {
		t.fileCache[filename] = prog
	}
	return prog, nil
}
// workspaceRoot 回傳當前編譯的工作區根目錄（workspace.jsonc 所在目錄）。
// 優先使用套件宣告的 WorkspaceRoot，否則從 sourcePath 向上查找 workspace.jsonc。
// 這是所有本地導入路徑解析的單一基準，編譯器不再以當前源碼檔目錄作為相對基準。
func (t *Transpiler) workspaceRoot() string {
	if t.pkg != nil {
		if ws := t.pkg.WorkspaceRoot(); ws != "" {
			return ws
		}
		if t.pkg.RootDir != "" {
			return t.pkg.RootDir
		}
	}
	if t.sourcePath != "" {
		if ws, ok := pkg.FindWorkspaceRoot(t.sourcePath); ok {
			return ws
		}
	}
	return ""
}

func (t *Transpiler) resolveUse(use *parser.UseStatement) (*parser.Program, error) {
	// use path.fn → 載入 path.no 並取出 fn 函數
	path := use.Path

	// 源碼導入約束：禁止 "./" 和 "../" 相對跳轉
	// 所有外部代碼引用只能通過 workspace 別名或遠端包標識間接引入
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return nil, fmt.Errorf("import %q: relative paths (./ ../) are not allowed in source code; use workspace alias or remote package identifier instead", path)
	}

	// 本地模塊：/path → 先嘗試依賴匹配，再回退到相對於 workspace 根目錄的路徑解析
	if strings.HasPrefix(path, "/") {
		// 先嘗試作為依賴鍵匹配（如 "/example/test2/utils.greet" 匹配依賴 "/example/test2"）
		if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
			if _, _, matched := t.pkg.MatchDependency(path); matched {
				modPath, err := t.pkg.ResolveDependencyModule(path)
				if err != nil {
					return nil, err
				}
				if modPath != "" {
					return t.resolveFile(modPath)
				}
			}
		}
		// 回退：相對於 workspace 根目錄的直接路徑
		relPath := strings.TrimPrefix(path, "/")
		fullPath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), relPath)
		if !strings.HasSuffix(fullPath, ".no") {
			fullPath = fullPath + ".no"
		}
		return t.resolveFile(fullPath)
	}
	// std/ 開頭 → 標準庫路徑（只從內嵌 StdFS 載入，支援單二進制分發）
	if strings.HasPrefix(path, "std/") || path == "std" {
		// strip "std/" prefix to get module path relative to std/
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		// 1. 直接路徑：std/<relPath>.no
		if relPath != "" {
			embedPath := "std/" + relPath + ".no"
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 2. Lookup table: match ShortPath to FullPath (e.g. "net" → "net/net")
		for _, info := range checker.KnownStdModules() {
			if info.ShortPath == relPath {
				embedPath := "std/" + info.FullPath + ".no"
				if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
					return t.parseEmbeddedProgram(embedPath, data)
				}
			}
		}
		// 找不到模組 → 返回語意化錯誤（不再回退到磁碟）
		return nil, fmt.Errorf("std module not found in embedded StdFS: %s", path)
	}
	// js/ 開頭 → JS 相容層路徑（內建第三方包，與 std/ 機制平行）
	// 模組名稱遵循 Nolang 的小寫中劃線風格（如 js/console-log、js/fs-read-file）
	if strings.HasPrefix(path, "js/") || path == "js" {
		relPath := strings.TrimPrefix(path, "js/")
		if path == "js" {
			relPath = ""
		}
		// 1. 嘗試從內嵌的 JsFS 載入（src/js/<module>.no）
		if relPath != "" {
			embedPath := "js/" + relPath + ".no"
			if data, err := nolang.JsFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 2. Lookup table: match ShortName to FullPath（與 std/ 的 checker.knownStdModules 機制平行）
		for _, info := range checker.KnownJsModules() {
			if info.ShortPath == relPath || info.ShortName == relPath {
				embedPath := "js/" + info.FullPath + ".no"
				if data, err := nolang.JsFS.ReadFile(embedPath); err == nil {
					return t.parseEmbeddedProgram(embedPath, data)
				}
			}
		}
		// 3. fallback: src/js/<module>.no 相對於執行目錄
		if relPath != "" {
			fallback := "src/" + path + ".no"
			if _, err := os.Stat(fallback); err == nil {
				return t.resolveFile(fallback)
			}
		}
		return nil, fmt.Errorf("js compatibility module not found: %s", relPath)
	}
	// 依賴解析：嘗試所有導入路徑匹配 package.jsonc 中宣告的依賴
	// 支援 URL 風格（github.com/...）、路徑風格（/example/pkg、./pkg）、短名稱風格（test2）
	first := strings.SplitN(path, "/", 2)[0]
	isURLStyle := strings.Contains(first, ".")
	if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
		if _, _, matched := t.pkg.MatchDependency(path); matched {
			modPath, err := t.pkg.ResolveDependencyModule(path)
			if err != nil {
				return nil, err
			}
			if modPath != "" {
				return t.resolveFile(modPath)
			}
		}
	}
	if isURLStyle {
		// URL 風格的導入路徑但未在 dependencies 中宣告
		return nil, fmt.Errorf("dependency not found: %q is not declared in package.jsonc dependencies", path)
	}
	// 非 std 路徑 → 透過 alias 解析（一律相對於工作區根目錄，不再以當前源碼檔目錄為基準）
	modulePath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)
	if !strings.HasSuffix(modulePath, ".no") {
		modulePath = modulePath + ".no"
	}
	return t.resolveFile(filepath.Clean(modulePath))
}
// resolveFile parses a .no file and applies lib.no export filtering if present.
func (t *Transpiler) resolveFile(filePath string) (*parser.Program, error) {
	prog, err := t.parseFile(filePath)
	if err != nil {
		return nil, err
	}
	// Apply lib.no export filtering
	pkgRoot := findPackageRootFromFile(filePath)
	if pkgRoot != "" {
		libPath := filepath.Join(pkgRoot, "lib.no")
		if _, err := os.Stat(libPath); err == nil {
			prog = checker.FilterByExports(prog, libPath, filePath)
		}
	}
	return prog, nil
}
func (t *Transpiler) Compile(source string) (string, error) {
	// 初始化 per-Transpiler AST 解析快取，消除單次編譯內的重复解析。
	// 同一 std 模組在 preloadModuleSignatures、checker.ValidateFuncArgs、merge 步驟中
	// 會被重複載入，快取後僅解析一次。
	t.fileCache = make(map[string]*parser.Program)
	// 保留全域模組解析快取（parseProgramFileCache + tokenCache），
	// 使 no test / no build workspace 等多文件場景可跨文件復用。
	// 快取在命令級別清空（BuildFile / BuildWorkspace / VetFile 入口），
	// 而非每次 Compile 調用都清空。
	return t.CompileTarget(source, TargetLLVM)
}
// preloadModuleSignatures 掃描源碼中的 use 語句，預載入模組的函數簽名和 struct 欄位型別。
// 這些簽名會注入到 parser 中，使 let 型別推斷能處理跨文件方法調用。
// 也預載入所有已知 std 模組的簽名，因為 transpiler 會自動載入這些模組。
func (t *Transpiler) preloadModuleSignatures(source string) (map[string][]string, map[string]map[string]string) {
	funcSigs := make(map[string][]string)
	structFields := make(map[string]map[string]string)
	loadedPaths := make(map[string]bool) // 避免重複載入同一模組
	// 0. 合併 std 模組簽名快取，並掛到 t.externFuncSigs：
	// 本地子模組（如 # /agent/agent）在下方步驟 1 就會被 parseFile 解析並
	// 寫入 fileCache，若此時 externFuncSigs 尚未設置，子模組內的
	// `resp = http.do-req(req)` 等 let 推斷拿不到任何 std 簽名，resp 無
	// 型別注解 → codegen 端 option inner 推斷錯誤（i64），字段訪問級聯崩潰。
	// 注意：本地模組簽名（步驟 1）寫入同一 map，同名鍵覆蓋 std 簽名，
	// 維持原「本地優先」語義。
	// 第一遍（簽名預收集）必須全量解析所有 std：nolang 的標準庫隱式可用
	// （無需顯式 `use std/...` 即可呼叫 module.fn()），parser 做 let 型別
	// 推斷時需要全部 std 簽名，故無法按「是否引用」跳過（對應審查點 ②）。
	// 真正可省略的 std 工作在第二遍（語義/IR）：見下方自動載入 std 迴圈，
	// 僅載入程式實際引用到的 std 模組體，其餘略過。
	stdSigs, stdFields := checker.CollectStdModuleSignatures()
	for name, sigs := range stdSigs {
		funcSigs[name] = sigs
	}
	for name, fields := range stdFields {
		structFields[name] = fields
	}
	t.externFuncSigs = funcSigs
	t.externStructFields = structFields
	// collectSignaturesFromProg 從已解析的 Program 中收集函數簽名和 struct 欄位
	collectSignaturesFromProg := func(modProg *parser.Program) {
		for _, stmt := range modProg.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.Results) > 0 {
					rets := make([]string, len(fd.Results))
					for i, r := range fd.Results {
						rets[i] = r.Type.String()
					}
					funcSigs[fd.Name] = rets
				}
			}
			if sd, ok := stmt.(*parser.StructDefinition); ok {
				fields := make(map[string]string)
				for _, f := range sd.Fields {
					if typeStr := checker.StructFieldTypeString(f); typeStr != "" {
						fields[f.Name] = typeStr
					}
				}
				structFields[sd.Name] = fields
			}
		}
	}
	// 1. 掃描顯式 use/# 語句（非 std 模組，std 模組由快取提供）
	useRe := regexp.MustCompile(`(?m)^\s*(?:use|#)\s+([\w/.\-]+)`)
	matches := useRe.FindAllStringSubmatch(source, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawPath := m[1]
		// std/ 模組簽名一律由 checker.CollectStdModuleSignatures 快取提供
		// （含跨模組同名 struct/函數的 module.name 限定鍵）。此處不得重複
		// 載入：直接解析會以裸名註冊簽名（如 do-req → ?response），在步驟 2
		// 的 !exists 合併中壓過快取的限定簽名，令 parser 推斷出未限定的
		// ?response 注解，codegen 端 mapToLLVMType 對裸名失敗回退 i64，
		// 造成 it 綁定型別錯誤與字段訪問級聯崩潰（status-code.to-str bug）。
		if rawPath == "std" || strings.HasPrefix(rawPath, "std/") {
			continue
		}
		// 嘗試解析模組路徑：先嘗試完整路徑，再嘗試去掉最後 .function 部分
		candidates := []string{rawPath}
		if idx := strings.LastIndex(rawPath, "."); idx > 0 {
			candidates = append(candidates, rawPath[:idx])
		}
		var modProg *parser.Program
		for _, usePath := range candidates {
			if loadedPaths[usePath] {
				modProg = nil
				break
			}
			fakeUse := &parser.UseStatement{Path: usePath}
			prog, err := t.resolveUse(fakeUse)
			if err == nil && prog != nil {
				modProg = prog
				loadedPaths[usePath] = true
				break
			}
		}
		if modProg != nil {
			collectSignaturesFromProg(modProg)
		}
	}
	// std 簽名已於步驟 0 合入（本地模組簽名在步驟 1 直接覆蓋同名鍵）。
	return funcSigs, structFields
}

// stdModuleLookup 建立 std 模組 shortName/shortPath → info 的查表。
// 供「第二遍按需載入 std」判斷某個 module.fn() 的 module 是否為已知 std 模組。
// KnownStdModules 本身已 sync.Once 快取，此處每呼叫重建 O(n) 查表（n 為 std
// 模組數，約百位），並發安全且開銷可忽略。
func stdModuleLookup() map[string]checker.StdModuleInfo {
	m := make(map[string]checker.StdModuleInfo)
	for _, info := range checker.KnownStdModules() {
		m[info.ShortName] = info
		m[info.ShortPath] = info
	}
	return m
}

// alwaysAutoLoadStd 是 codegen 內建路徑（print/格式化等）無條件引用、但源碼
// AST 無顯式 module.fn() 的 std 模組，必須始終自動載入（靜態掃描無法捕捉）。
//   - fmt：@fmt-int/@fmt-uint/@fmt-f64/@fmt-str/@fmt-bool 等（print/格式化內建）
//   - io ：@out/@err（emitOutCall 直接發出的裸呼叫，對應 io.out/io.err）
//   - byte：[]byte.to-str / []byte.to-hex 等類型方法經 transpiler 重寫為
//     []byte.fn(receiver) 識別碼呼叫，collectReferencedStdModules 無法偵測
//     （它只掃描 module.fn() DotExpression，不掃描 Type.method Identifier）。
//     byte 模組體積小且 []byte 方法被廣泛使用（net/crypto/encoding 等），
//     始終載入避免未定義函式呼叫。
// 若未來 codegen 新增其他隱式 std 依賴，在此追加即可。
var alwaysAutoLoadStd = []string{"fmt", "io", "byte", "str", "vec"}

// dotModulePath 從 DotExpression 提取模組路徑（如 array.map → "array"，
// hash/sha256.sum → "hash/sha256"），與 checker.extractModulePathAndFunc 同義
// 但獨立實作於 build 包（該函數未導出）。
func dotModulePath(dot *parser.DotExpression) string {
	var segments []string
	cur := dot.Receiver
	for {
		if d, ok := cur.(*parser.DotExpression); ok {
			segments = append([]string{d.Property}, segments...)
			cur = d.Receiver
		} else if ident, ok := cur.(*parser.Identifier); ok {
			segments = append([]string{ident.Value}, segments...)
			break
		} else {
			return ""
		}
	}
	return strings.Join(segments, "/")
}

// collectReferencedStdModules 掃描程式，收集被顯式引用到的 std 模組 short path：
//   - module.fn() / module.CONST 的 module 部分（module 為已知 std shortName/shortPath）；
//   - use std/... 語句路徑（含 use std 導入全部，保守加入所有已知 std）。
//
// 這是「第二遍」確定實際引用哪些 std 的依據。隱式分發（arr./vec./[]/str. 等）
// 走 builtin，不經此路徑，故不會被計入——它們本就不需要 std 模組體。
func (t *Transpiler) collectReferencedStdModules(prog *parser.Program) map[string]bool {
	refs := make(map[string]bool)
	lookup := stdModuleLookup()
	addRef := func(path string) {
		if path == "" {
			return
		}
		if _, ok := lookup[path]; ok {
			refs[path] = true
		}
	}
	var walkExpr func(e parser.Expression)
	var walkStmt func(s parser.Statement)
	var walkType func(t parser.Type)
	// walkType 遞迴遍歷型別節點，從帶點的 NamedType（如 tls.conn）
	// 提取模組前綴並標記為被引用的 std 模組。這解決了 collectReferencedStdModules
	// 無法偵測型別注解（如 `tls-c tls.conn`）中隱含的模組依賴問題：
	// 方法呼叫 tls-c.close() 在 codegen 被重寫為 tls.conn.close(tls-c)，
	// 但靜態掃描在 codegen 前執行，無法得知 tls-c 的型別來自 tls 模組。
	// 透過掃描型別注解，能正確偵測 tls 模組依賴並按需載入。
	walkType = func(t parser.Type) {
		if t == nil {
			return
		}
		switch ty := t.(type) {
		case *parser.NamedType:
			// 從帶點的型別名（如 tls.conn、http.request）提取模組前綴。
			// 前綴對應 std 模組的 ShortName（如 tls → net/tls.no）。
			if idx := strings.Index(ty.Value, "."); idx > 0 {
				addRef(ty.Value[:idx])
			}
		case *parser.ArrayType:
			walkType(ty.Elem)
		case *parser.SliceType:
			walkType(ty.Elem)
		case *parser.MapType:
			walkType(ty.Key)
			walkType(ty.Value)
		case *parser.NullableType:
			walkType(ty.Type)
		case *parser.PointerType:
			walkType(ty.Type)
		case *parser.FunctionType:
			for _, p := range ty.Params {
				walkType(p.Type)
			}
			for _, r := range ty.Results {
				walkType(r.Type)
			}
		case *parser.UnionType:
			for _, ut := range ty.Types {
				walkType(ut)
			}
		}
	}
	walkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		// 防禦 typed-nil：非 nil 的 Expression 介面包裹 nil 指標（如切片中的
		// 元素），`if e == nil` 攔不住，type switch 會把其分派到指標 case 後
		// 解引用欄位（如 ex.Receiver）觸發 SIGSEGV。此處以 reflect 攔截底層
		// 指標為 nil 的情況。
		if v := reflect.ValueOf(e); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch ex := e.(type) {
		case *parser.DotExpression:
			addRef(dotModulePath(ex))
			walkExpr(ex.Receiver)
		case *parser.CallExpression:
			if dot, ok := ex.Function.(*parser.DotExpression); ok {
				addRef(dotModulePath(dot))
			} else if ident, ok := ex.Function.(*parser.Identifier); ok {
				// 裸函數呼叫：若函數名恰好為某個 std 模組的 ShortName，
				// 且該模組確實定義了同名頂層函數（如 crypto/rand.no 的 rand），
				// 則將該模組標記為被引用。這處理 std 模組之間以裸名相互呼叫
				// 的情況（如 x25519.no 呼叫 rand(state)），使傳遞閉包能正確
				// 載入依賴模組。
				if info, ok := lookup[ident.Value]; ok {
					if info.ShortName == ident.Value {
						addRef(ident.Value)
					}
				}
			}
			walkExpr(ex.Function)
			for _, a := range ex.GenericArgs {
				walkExpr(a)
			}
			for _, a := range ex.Arguments {
				walkExpr(a)
			}
		case *parser.PrefixExpression:
			walkExpr(ex.Right)
		case *parser.InfixExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Right)
		case *parser.GroupedExpression:
			walkExpr(ex.Expression)
		case *parser.RunExpression:
			walkExpr(ex.Call)
		case *parser.AwaitExpression:
			walkExpr(ex.Right)
		case *parser.IfExpression:
			walkExpr(ex.Condition)
			walkExpr(ex.MatchedExpr)
			walkExpr(ex.RawCond)
			walkExpr(ex.EqualityPattern)
			if ex.Consequence != nil {
				for _, s := range ex.Consequence.Statements {
					walkStmt(s)
				}
			}
			if ex.Alternative != nil {
				for _, s := range ex.Alternative.Statements {
					walkStmt(s)
				}
			}
			if ex.DotValBody != nil {
				for _, s := range ex.DotValBody.Statements {
					walkStmt(s)
				}
			}
			walkExpr(ex.RangePattern)
			for _, vp := range ex.ValuePatterns {
				walkExpr(vp)
			}
		case *parser.RangeExpression:
			walkExpr(ex.Start)
			walkExpr(ex.End)
		case *parser.SliceExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Range)
		case *parser.IndexExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Index)
		case *parser.AssignExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Value)
		case *parser.ConditionalExpression:
			walkExpr(ex.Condition)
			walkExpr(ex.Consequence)
			walkExpr(ex.Alternative)
		case *parser.CastExpression:
			walkExpr(ex.Expr)
			walkType(ex.Type)
		case *parser.IterationExpr:
			walkExpr(ex.Range)
			walkExpr(ex.RangeExpr)
		case *parser.FunctionLiteral:
			if ex.Body != nil {
				for _, s := range ex.Body.Statements {
					walkStmt(s)
				}
			}
			for _, p := range ex.Parameters {
				walkType(p.Type)
				walkExpr(p.DefaultExpr)
			}
			for _, r := range ex.Results {
				walkType(r.Type)
			}
		case *parser.ArrayLiteral:
			walkExpr(ex.Size)
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.MapLiteral:
			for _, pr := range ex.Pairs {
				walkExpr(pr.Key)
				walkExpr(pr.Value)
			}
		case *parser.SliceLiteral:
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.StructLiteral:
			for _, f := range ex.Fields {
				walkExpr(f.Value)
			}
		}
	}
	walkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		// 同上：防禦 typed-nil（非 nil 的 Statement 介面包裹 nil 指標）。
		if v := reflect.ValueOf(s); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch st := s.(type) {
		case *parser.ExpressionStatement:
			walkExpr(st.Expression)
		case *parser.LetStatement:
			walkType(st.Type)
			walkExpr(st.Value)
		case *parser.MultiAssignStatement:
			for _, tg := range st.Targets {
				walkExpr(tg)
			}
			walkExpr(st.Value)
		case *parser.FunctionDefinition:
			for _, p := range st.Parameters {
				walkType(p.Type)
			}
			for _, r := range st.Results {
				walkType(r.Type)
			}
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
			for _, p := range st.Parameters {
				walkExpr(p.DefaultExpr)
			}
		case *parser.ReturnStatement:
			walkExpr(st.ReturnValue)
		case *parser.BlockStatement:
			for _, bs := range st.Statements {
				walkStmt(bs)
			}
		case *parser.ForStatement:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Condition)
			if st.Update != nil {
				walkStmt(st.Update)
			}
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
			if st.IterRange != nil {
				walkExpr(st.IterRange.Range)
				walkExpr(st.IterRange.RangeExpr)
			}
			walkExpr(st.CountExpr)
		case *parser.UseStatement:
			if st.Path == "std" || strings.HasPrefix(st.Path, "std/") {
				sp := strings.TrimPrefix(st.Path, "std/")
				if sp == "" {
					// use std（導入全部）：保守加入所有已知 std 模組。
					for k := range lookup {
						refs[k] = true
					}
				} else {
					addRef(sp)
				}
			}
		case *parser.ExportStatement:
			if st.Path == "std" || strings.HasPrefix(st.Path, "std/") {
				addRef(strings.TrimPrefix(st.Path, "std/"))
			}
		case *parser.StructDefinition:
			for _, f := range st.Fields {
				walkType(f.Type)
				walkExpr(f.Value)
			}
		case *parser.ExternStatement:
			for _, p := range st.Parameters {
				walkType(p.Type)
			}
			for _, r := range st.Results {
				walkType(r.Type)
			}
		case *parser.TypeAlias:
			if st.Type != nil {
				walkType(st.Type)
			}
			if st.Union != nil {
				for _, ut := range st.Union.Types {
					walkType(ut)
				}
			}
		case *parser.TaggedEnumDefinition:
			for _, v := range st.Variants {
				walkType(v.Type)
			}
		case *parser.InterfaceDefinition:
			for _, m := range st.Methods {
				for _, p := range m.Parameters {
					walkType(p.Type)
				}
				for _, r := range m.Results {
					walkType(r.Type)
				}
			}
		}
	}
	for _, s := range prog.Statements {
		walkStmt(s)
	}
	return refs
}

func (t *Transpiler) CompileTarget(source string, _ Target) (string, error) {
	// 預載入跨文件模組簽名，供 parser 型別推斷使用
	externFuncSigs, externStructFields := t.preloadModuleSignatures(source)
	// 存儲到 Transpiler 中，使 parseFile（用於解析自動載入的模組）也能注入簽名
	t.externFuncSigs = externFuncSigs
	t.externStructFields = externStructFields

	// B（開關 NOLANG_REUSE_STD_AST）：no vet src/std 時，target 即某個 std 模組，
	// 其 Program 已在 CollectStdModuleSignatures PASS1 解析過。若磁盤文件內容與
	// embed 解析內容一致（內容哈希命中），直接復用該 Program，跳過磁盤重新 parse。
	// 限定標準庫：非 std 文件內容不會命中任何 std 模組鍵，自動走正常 parse 路徑。
	var program *parser.Program
	if os.Getenv("NOLANG_REUSE_STD_AST") == "1" {
		if reused := checker.StdProgramForContent(cache.ContentKey(source)); reused != nil {
			program = reused
		}
	}
	if program == nil {
		l := lexer.NewCached(t.sourcePath, source)
		p := parser.New(l)
		p.AllowAnonymousFnType = t.allowAnonymousFn
		if t.sourcePath != "" {
			p.Filename = filepath.Base(t.sourcePath)
		}
		// 注入外部簽名
		if len(externFuncSigs) > 0 || len(externStructFields) > 0 {
			p.SetExternSignatures(externFuncSigs, externStructFields)
		}
		program = p.ParseProgram()
		if len(p.Errors()) > 0 {
			return "", fmt.Errorf("parser errors: %v", p.Errors())
		}
		// 高危警告直出 stderr：單個 ; 行註釋疑似吞掉了同一行後面的代碼
		for _, w := range p.WarningsByCode(parser.WarnSemiSwallow) {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
	}
	// 處理 #{embed=...} 註解：編譯期讀取嵌入文件
	if err := t.processEmbeds(program, t.sourcePath); err != nil {
		return "", err
	}
	// 驗證：僅標準庫能使用的功能
	isUserCode := true
	if t.pkg != nil {
		root := t.pkg.RootDir
		if strings.Contains(root, "src/std") || strings.Contains(root, "std") {
			isUserCode = false
		}
	}
	// 如果 pkg 為 nil，檢查源碼檔案路徑是否為標準庫
	if isUserCode && t.sourcePath != "" {
		if strings.Contains(t.sourcePath, "src/std") || strings.Contains(t.sourcePath, "/std/") {
			isUserCode = false
		}
	}
	// 合併 PASS 1+2+3+4：單次遍歷同時執行：
	// (1) ..any 標準庫限制檢查
	// (2) 構建 varTypes / globalVarTypes
	// (3) 收集 importedModules（UseStatement 的 ShortName）
	// (4) 收集 mainVarNames（頂層 Let + FunctionDefinition 名）
	// 原本為 4 次獨立遍歷，合併後降至 1 次。
	varTypes := make(map[string]string)
	globalVarTypes := make(map[string]string)
	var importedModules []string
	mainVarNames := make(map[string]bool)
	// 預填充已知 std 模塊的 ShortName，允許 math.degrees()、base64.encode-std() 等呼叫無需顯式導入
	for _, info := range checker.KnownStdModules() {
		importedModules = append(importedModules, info.ShortName)
	}
	for _, stmt := range program.Statements {
		// (1) ..any 標準庫限制檢查
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && isUserCode {
			if fd.IsVariadic {
				for _, p := range fd.Parameters {
					if p.Type.String() == "[]any" {
						return "", fmt.Errorf("..any is only allowed in standard library, not in user code (function: %s)", fd.Name)
					}
				}
			}
		}
		// (2) 構建變數類型表
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
				globalVarTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
					globalVarTypes[ls.Name.Value] = t
				}
			}
			// (4) 收集主程序變量名
			if ls.Name != nil {
				mainVarNames[ls.Name.Value] = true
			}
		}
		// (2) 函數參數/返回值類型收集
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for _, p := range fd.Parameters {
				if p.Type != nil {
					varTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fd.Results {
				if r.Type != nil {
					varTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fd.Body, varTypes)
			// (4) 收集主程序函數名
			mainVarNames[fd.Name] = true
		}
		// (3) 收集導入的模塊路徑
		if use, ok := stmt.(*parser.UseStatement); ok {
			importedModules = append(importedModules, checker.ModuleShortName(use.Path))
		}
	}
	// 編譯期陣列邊界檢查（單次遍歷同時收集陣列、切片、字串大小映射）
	sizeMap := buildSizeMaps(program)
	validateFuncStrReturns = sizeMap.funcStrReturns
	if err := validateArrayBounds(program, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes); err != nil {
		return "", err
	}
	// 編譯期重複變數檢查
	if err := validateDuplicates(program); err != nil {
		return "", err
	}
	// 型別檢查（收集所有錯誤後統一報告，而非遇錯即返）
	// 這樣用戶在 no build 時能一次看到所有型別錯誤，而非逐個修復後才能看到下一個。
	var allValidateErrs []string
	if typeErrs := checker.ValidateTypes(program); len(typeErrs) > 0 {
		for _, e := range typeErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message))
		}
	}
	// 函數呼叫引數型別檢查（包括 newtype 語義：fd 變量不能傳給 i64 參數）
	if funcArgErrs := checker.ValidateFuncArgs(program, filepath.Dir(t.sourcePath)); len(funcArgErrs) > 0 {
		for _, e := range funcArgErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message))
		}
	}
	// ?T 輸出參數未初始化檢查（case6）
	if uninitErrs := checker.ValidateUninitOutputParams(program); len(uninitErrs) > 0 {
		for _, e := range uninitErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message))
		}
	}
	if len(allValidateErrs) > 0 {
		return "", fmt.Errorf("validation errors: %s", strings.Join(allValidateErrs, "; "))
	}
	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)
	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)
	// 處理 use 陳述句：載入模組並合併函數定義和常量
	// 註：importedModules 和 mainVarNames 已在上方合併 PASS 中收集完成
	merged := &parser.Program{Statements: []parser.Statement{}}
	// 合併語義副表：merged 匯集主程序與各模塊的 AST 節點，
	// 各自 side-table 中的節點語義（註解/平台鍵/embed 等）也必須匯集。
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(program.Sem)
	// 記錄已顯式導入的模組路徑，避免重複載入
	explicitStdModules := make(map[string]bool)
	moduleConstants := make(map[string]parser.Expression)
	// typeOwner 記錄跨模組型別定義的歸屬（bareName → moduleShortName），
	// 用於將導入模組的 struct/interface/enum 等型別定義加上模組前綴
	// （如 result → sql.result），避免與主檔案變數或型別衝突。
	typeOwner := make(map[string]string)
	// stmtOwner 記錄 merged 中每條模組語句的來源模組短名（主程序語句無記錄）。
	// 供 prefixCollidingFunctions 判定：同名頂層函數衝突時，只有模組側定義
	// 改名為 module.fn，主程序定義永不改名。
	stmtOwner := make(map[parser.Statement]string)
	// loadedUserModules tracks non-std module paths that have already been
	// imported (when use.Alias == ""). Prevents duplicate FunctionDefinition
	// pointers from being appended when the same module is referenced by
	// multiple `# /path/to/module.fn` directives — which would cause
	// mangleOverloads to remove ALL copies (pointer-keyed dedup removes
	// every occurrence of the same pointer).
	loadedUserModules := make(map[string]bool)
	// pendingUses is a worklist of UseStatements from imported modules that
	// need to be processed (transitive imports). When a module is loaded, its
	// UseStatements are added to this worklist so that their target modules
	// are also loaded and merged.
	var pendingUses []*parser.UseStatement
	// processUseAndMerge loads a module from a UseStatement, merges its
	// non-UseStatement statements into merged, and collects its UseStatements
	// into pendingUses for recursive processing.
	processUseAndMerge := func(use *parser.UseStatement) error {
		if use.Alias == "" && loadedUserModules[use.Path] {
			return nil
		}
		modProg, err := t.resolveUse(use)
		if err != nil {
			return fmt.Errorf("loading module %s: %w", use.Path, err)
		}
		merged.Sem.Merge(modProg.Sem)
		if use.Alias == "" {
			loadedUserModules[use.Path] = true
		}
		if strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
			explicitStdModules[use.Path] = true
		}
		// 為導入模組的型別定義加上模組前綴（如 result → sql.result）
		useModShort := checker.ModuleShortName(use.Path)
		prefixModuleStatements(modProg.Statements, useModShort, typeOwner)
		// 將模組中的 FunctionDefinition 和 LetStatement（常量）加入 merged
		for _, ms := range modProg.Statements {
			// Collect transitive UseStatements for recursive processing
			if nestedUse, ok := ms.(*parser.UseStatement); ok {
				if nestedUse.Alias == "" && loadedUserModules[nestedUse.Path] {
					continue
				}
				pendingUses = append(pendingUses, nestedUse)
				continue
			}
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				// If alias is specified, only import the specific function under the alias name
				if use.Alias != "" {
					if use.Function != "" && fd.Name == use.Function {
						fd.Name = use.Alias
						merged.Statements = append(merged.Statements, fd)
						// alias 導入按用戶指定名，不參與衝突前綴
					}
					// Skip other functions when alias is used
				} else {
					merged.Statements = append(merged.Statements, fd)
					stmtOwner[fd] = useModShort
				}
			}
			if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				// If alias is specified, only import the specific function under the alias name
				if use.Alias != "" {
					if use.Function != "" && ls.Name.Value == use.Function {
						if _, ok := ls.Value.(*parser.FunctionLiteral); ok {
							ls.Name.Value = use.Alias
						}
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
							if checker.IsConstantExpr(ls.Value) && checker.MatchesTargetPlatform(modProg.Sem.PlatformKeysOf(ls), t.targetGoos, t.targetGoarch) {
								moduleConstants[ls.Name.Value] = ls.Value
							}
						}
					}
					// Skip other lets when alias is used
				} else {
					// 如果主程序已有同名變量，跳過以避免衝突
					if !mainVarNames[ls.Name.Value] {
						merged.Statements = append(merged.Statements, ls)
						stmtOwner[ls] = useModShort
						if checker.IsConstantExpr(ls.Value) && checker.MatchesTargetPlatform(modProg.Sem.PlatformKeysOf(ls), t.targetGoos, t.targetGoarch) {
							moduleConstants[ls.Name.Value] = ls.Value
						}
					}
				}
			}
			if use.Alias == "" {
				if sd, ok := ms.(*parser.StructDefinition); ok {
					merged.Statements = append(merged.Statements, sd)
				}
				if id, ok := ms.(*parser.InterfaceDefinition); ok {
					merged.Statements = append(merged.Statements, id)
				}
				if ta, ok := ms.(*parser.TypeAlias); ok {
					merged.Statements = append(merged.Statements, ta)
				}
				if ed, ok := ms.(*parser.EnumDefinition); ok {
					merged.Statements = append(merged.Statements, ed)
				}
				if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
					merged.Statements = append(merged.Statements, ted)
				}
				// FFI extern 宣告必須隨模組一起合併，否則 codegen 的 externFuncs
				// 會缺少條目，導致 extern 呼叫走 Nolang by-reference 路徑而非 FFI 路徑。
				if es, ok := ms.(*parser.ExternStatement); ok {
					merged.Statements = append(merged.Statements, es)
				}
			}
		}
		return nil
	}
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			if err := processUseAndMerge(use); err != nil {
				return "", err
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			merged.Statements = append(merged.Statements, stmt)
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			// FFI extern 宣告 — 收集至 merged 供後續 codegen 使用（目前尚未實作）
			merged.Statements = append(merged.Statements, es)
		}
	}
	// Process transitive imports: drain the pendingUses worklist.
	// Each imported module may itself import other modules; those must also
	// be loaded and merged to make their function definitions available.
	for len(pendingUses) > 0 {
		use := pendingUses[0]
		pendingUses = pendingUses[1:]
		if err := processUseAndMerge(use); err != nil {
			return "", err
		}
	}
	// 第二遍按需載入 std 模組體（對應審查點 ②）：
	// 第一遍（簽名預收集 CollectStdModuleSignatures）已全量解析所有 std ——
	// 因為 nolang 標準庫隱式可用（無需顯式 use 即可呼叫 module.fn()），parser
	// 做 let 型別推斷需要全部 std 簽名，故第一遍必須全量、無法跳過。
	// 但 codegen 只需程式「實際引用到」的 std 模組。此處掃描 merged（含主程式
	// 與本地 use 模組）中的 module.fn()/module.CONST/use std/... 引用，取 std
	// 模組間的可達閉包，僅載入這些模組體，其餘略過，避免為未使用的 std 付出
	// 解析 + 合併成本。與全量載入相比正確性等價：隱式分發走 builtin，不依賴
	// 此處載入；std 函數僅能經顯式 module.fn() 觸達，閉包已覆蓋。
	loadedStd := make(map[string]bool)
	var stdWorklist []string
	// 掃描 merged（含已載入模組）和原始 program（含頂層語句）中的 std 模組引用。
	// 原始 program 的頂層 LetStatement / ExpressionStatement 此時尚未加入 merged，
	// 但其中可能引用 module.fn()（如 sha1.sha1(data)），必須在此處一併檢測，
	// 否則這些 std 模組不會被自動載入，導致標準庫函數無法在頂層代碼中使用。
	refs := t.collectReferencedStdModules(merged)
	for sp := range t.collectReferencedStdModules(program) {
		refs[sp] = true
	}
	for sp := range refs {
		if !loadedStd[sp] {
			loadedStd[sp] = true
			stdWorklist = append(stdWorklist, sp)
		}
	}
	// 始終自動載入 codegen 內建隱式依賴的 std 模組（見 alwaysAutoLoadStd 註解）。
	for _, name := range alwaysAutoLoadStd {
		if _, ok := stdModuleLookup()[name]; ok && !loadedStd[name] {
			loadedStd[name] = true
			stdWorklist = append(stdWorklist, name)
		}
	}
	for len(stdWorklist) > 0 {
		sp := stdWorklist[0]
		stdWorklist = stdWorklist[1:]
		info, ok := stdModuleLookup()[sp]
		if !ok {
			continue
		}
		// 頂層變量名與模塊名衝突 → 跳過自動載入（與原全量自動載入邏輯一致）。
		// 註：必須用 globalVarTypes（僅頂層變數），不能用 varTypes（含函數體內的局部變數），
		// 否則函數內的局部變數（如 test-arr-reverse 中的 arr [4] = ...）會導致
		// arr 模組被錯誤跳過，使 [n]t 方法無法載入。
		if _, isVar := globalVarTypes[info.ShortName]; isVar {
			continue
		}
		path := "std/" + info.ShortPath
		if explicitStdModules[path] {
			continue
		}
		use := &parser.UseStatement{Path: path, Function: ""}
		modProg, err := t.resolveUse(use)
		if err != nil {
			return "", fmt.Errorf("auto-loading module %s: %w", path, err)
		}
		merged.Sem.Merge(modProg.Sem)
		// 為自動載入模組的型別定義加上模組前綴（如 result → sql.result）
		prefixModuleStatements(modProg.Statements, info.ShortName, typeOwner)
		for _, ms := range modProg.Statements {
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				merged.Statements = append(merged.Statements, fd)
				stmtOwner[fd] = info.ShortName
			}
			if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				// 如果主程序已有同名變量，跳過以避免衝突
				if !mainVarNames[ls.Name.Value] {
					merged.Statements = append(merged.Statements, ls)
					stmtOwner[ls] = info.ShortName
					if checker.IsConstantExpr(ls.Value) && checker.MatchesTargetPlatform(modProg.Sem.PlatformKeysOf(ls), t.targetGoos, t.targetGoarch) {
						moduleConstants[ls.Name.Value] = ls.Value
					}
				}
			}
			if sd, ok := ms.(*parser.StructDefinition); ok {
				merged.Statements = append(merged.Statements, sd)
			}
			if id, ok := ms.(*parser.InterfaceDefinition); ok {
				merged.Statements = append(merged.Statements, id)
			}
			if ta, ok := ms.(*parser.TypeAlias); ok {
				merged.Statements = append(merged.Statements, ta)
			}
			if ed, ok := ms.(*parser.EnumDefinition); ok {
				merged.Statements = append(merged.Statements, ed)
			}
			if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
				merged.Statements = append(merged.Statements, ted)
			}
		}
		// 傳遞閉包：掃描本模組體內的 std 引用（module.fn()/use std/...），
		// 加入 worklist，處理 std 模組間的相互呼叫。
		for dep := range t.collectReferencedStdModules(modProg) {
			if !loadedStd[dep] {
				loadedStd[dep] = true
				stdWorklist = append(stdWorklist, dep)
			}
		}
	}
	// 第一階段型別參考改寫：將導入模組語句中的裸型別名改寫為 module.type 形式。
	// 必須在 resolveSelfMethodCalls 之前執行，因為 resolveSelfMethodCalls 透過
	// collectStructFields 以 struct 名稱為 key 查找，若型別定義已重命名為
	// module.name 但參考仍為裸名，會無法匹配。
	// 但先要完成跨模組方法名稱重命名：一個方法可能定義在 B 模組但接收者型別
	// 定義在 A 模組（如 bufio.no 的 reader.fill，reader 定義在 io.no）。
	// 此時 typeOwner 已包含所有模組的型別歸屬，可以正確重命名。
	// mainLocalTypes：主程序自行定義的型別裸名。主程序型別與 std 模組同名時
	//（如自定義 tree-set vs std/collection/tree-set），主程序方法不得加模組前綴。
	mainLocalTypes := make(map[string]bool)
	for _, mainStmt := range program.Statements {
		if name := getTypeDefName(mainStmt); name != "" {
			mainLocalTypes[name] = true
		}
	}
	prefixMethodNames(merged.Statements, typeOwner, mainLocalTypes)
	// 型別參考改寫必須區分歸屬：
	//   - 模組語句用完整 typeOwner（模組內的裸名引用要跟隨定義端一起改寫成
	//     module.type，否則模組自身的方法簽名與型別定義對不上）。
	//   - 主程序語句用剔除本地型別後的表：主程序自定義的 struct 與 std 模組
	//     同名時（tests/test-bare-match.no 的 tree-set vs std/collection/tree-set），
	//     主程序的裸名引用必須繼續指向本地定義，否則方法簽名被改成
	//     `%tree-set.tree-set*` 而定義名仍是 `tree-set.test-match`，呼叫端與
	//     定義端錯位，產生 "use of undefined value" 的無效 IR。
	mainLocalOwner := excludeLocalTypes(typeOwner, mainLocalTypes)
	if len(typeOwner) > 0 {
		mainStmtSet := make(map[parser.Statement]bool, len(program.Statements))
		for _, mainStmt := range program.Statements {
			mainStmtSet[mainStmt] = true
		}
		for _, mergedStmt := range merged.Statements {
			if mainStmtSet[mergedStmt] {
				rewriteTypeRefsInStmt(mergedStmt, mainLocalOwner)
			} else {
				rewriteTypeRefsInStmt(mergedStmt, typeOwner)
			}
		}
	}
	checker.DebugCountHashFns("after-prefixMethodNames", merged)
	// 函數衝突前綴：多模組同名頂層函數（connect×4、get×5 …）改名為
	// module.fn 並改寫模組內裸呼叫。必須在 prefixMethodNames 之後執行，
	// 否則 path.join 之類的新名字會被誤認成 struct path 的方法。
	prefixedFns := prefixCollidingFunctions(merged, stmtOwner)
	// 常量傳播：將模組常量替換為字面值，使 module functions 可以直接使用常量
	checker.ResolveModuleConstants(merged, moduleConstants)
	checker.DebugCountHashFns("after-resolveModuleConstants", merged)
	// 解析 module.fn() 呼叫：將 DotExpression 重寫為 Identifier
	// 必須在 monomorphizeGenerics 之前執行，以便泛型模組函數也能被正確處理
	checker.ResolveModuleCalls(merged, importedModules, prefixedFns)
	checker.DebugCountHashFns("after-resolveModuleCalls", merged)
	// 解析 self.method() 呼叫：將方法體內的 self.method(args) 重寫為 Type.method(self, args)
	checker.ResolveSelfMethodCalls(merged)
	checker.DebugCountHashFns("after-resolveSelfMethodCalls", merged)
	// 非函數定義的陳述句（頂層呼叫）加入 merged
	// 必須在 monomorphizeGenerics 之前添加，否則頂層的方法呼叫（如 a.clone()）
	// 不會被解析與單態化；也必須在 monomorphizeUnions/rewriteUnionCalls 之前添加，
	// 否則頂層呼叫（如 pow(2, 10, r)）不會被重寫為具體版本
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		if _, ok := stmt.(*parser.UseStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExternStatement); ok {
			// extern 為宣告，非頂層可執行語句（已於前述步驟收集至 merged）
			continue
		}
		// Convert MultiAssignStatement to old nested-call syntax for codegen
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			if innerCall, ok := mas.Value.(*parser.CallExpression); ok {
				// Create: innerCall(outerArgs) with outerArgs being the target expressions
				outerCall := &parser.CallExpression{
					Token:     innerCall.Token,
					Function:  innerCall,
					Arguments: mas.Targets,
				}
				merged.Statements = append(merged.Statements, &parser.ExpressionStatement{Expression: outerCall})
			}
			continue
		}
		merged.Statements = append(merged.Statements, stmt)
	}
	// 泛型單態化：掃描泛型函數呼叫，生成具體版本
	// 使用 globalVarTypes（僅頂層變數）避免其他函數的局部變數型別洩漏到 method resolution
	// 傳入 typeOwner 以便 resolveMethodCall 為跨模組型別補上模組前綴
	// typeOwner 用剔除主程序本地型別的版本：globalVarTypes 只含主程序頂層變數，
	// 其裸名型別若由主程序自行定義，方法呼叫不得被加上模組前綴。
	monomorphizeGenerics(merged, globalVarTypes, mainLocalOwner)
	checker.DebugCountHashFns("after-monomorphizeGenerics", merged)
	// 過濾：移除尚未具現化的泛型函數定義（只有具體版本才能產生 LLVM IR）
	filtered := make([]parser.Statement, 0, len(merged.Statements))
	for _, stmt := range merged.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				continue // 跳過泛型函數（GenericParams 未被清空說明尚未具現化）
			}
		}
		filtered = append(filtered, stmt)
	}
	merged.Statements = filtered
	checker.DebugCountHashFns("after-filter-generic", merged)
	// 第二階段型別參考改寫：主檔案的頂層語句（struct 定義、let 宣告等）
	// 在此時才加入 merged，需要再次改寫以處理主檔案中對導入模組型別的引用。
	// 已改寫過的型別名（含 "."）會被 prefixTypeName 自動跳過，安全無副作用。
	// 使用剔除主程序本地型別後的表：主程序自定義型別與模組同名時，裸名引用
	// 必須保持指向本地定義（與 prefixMethodNames 的本地優先語義一致）。
	rewriteTypeRefs(merged.Statements, mainLocalOwner)
	// 解析頂層代碼中的 module.fn() 呼叫
	checker.ResolveModuleCalls(merged, importedModules, prefixedFns)
	// 再次傳播模組常量：頂層語句此時已加入 merged，且 module.CONST 已被
	// ResolveModuleCalls 改寫為裸名（如 math.PI → PI），需要再次將裸名
	// 常量替換為字面值。否則頂層代碼中的模組常量會殘留為變數引用。
	checker.ResolveModuleConstants(merged, moduleConstants)
	// 解析 Type.method(args) 靜態方法呼叫：將 bigint.cmp(d, d2) 重寫為
	// bigint.bigint.cmp(d, d2)，與 prefixMethodNames 重命名後的方法定義對齊。
	// 必須在 resolveSelfMethodCalls 之後執行（該 pass 已用正確的 module 前綴
	// 生成 Type.method(self, args)），並在所有頂層代碼加入 merged 之後執行。
	checker.ResolveMethodCalls(merged, typeOwner)
	checker.DebugCountHashFns("after-resolveMethodCalls", merged)
	// 檢查期診斷：主程式中呼叫不存在的 module.fn（如 bigint.from-int）。
	// 以前這類錯誤會落到 LLVM opt 才報 `use of undefined value`，難以定位。
	// 必須在 ResolveModuleCalls + ResolveMethodCalls 之後執行：合法的
	// module.fn / Type.method 呼叫此時都已被改寫，殘留的模組點呼叫即非法。
	if err := checkUnresolvedModuleCalls(merged, stmtOwner, importedModules); err != nil {
		return "", fmt.Errorf("check error: %v", err)
	}
	// 泛型結構體單態化：掃描 map[K]V 使用點，自 hashmap-*-tmpl 模板生成具體結構與方法。
	// 必須在 monomorphizeGenerics 之後（避免與 [n]t 泛型衝突）、monomorphizeUnions 之前執行。
	monomorphizeGenericStructs(merged)
	checker.DebugCountHashFns("after-monomorphizeGenericStructs", merged)
	// 聯合型別單態化：對帶 ..T（T 為 union alias）的函數，
	// 為 union 的每個具體型別生成一個函數版本。生成函數的命名
	// 採用 "<原名>__<成員型別>" 的形式；對函數體內對自己的呼叫也
	// 一併替換。
	monomorphizeUnions(merged)
	checker.DebugCountHashFns("after-monomorphizeUnions", merged)
	// 重寫對聯合型別泛型函數的呼叫：將 max(args) 改為 max__i64(args)
	rewriteUnionCalls(merged, varTypes)
	checker.DebugCountHashFns("after-rewriteUnionCalls", merged)
	// 在合併所有 std 模組後再做一次名稱修飾，
	// 處理跨模組的重載衝突（如 bigint.div-mod vs number.div-mod）
	mangleOverloads(merged, nil)
	checker.DebugCountHashFns("after-mangleOverloads", merged)
	// D16: 模組合併後重新執行 validateArrayBounds。
	// 原本僅在主程式合併前執行（line 1397），導入模組內的錯誤（如 out.len = 1
	// 違反 str.len 只讀約束）被靜默吞掉，直到連結期才以 undefined symbol 報錯。
	// 必須在所有轉換 pass 完成後執行，此時 merged 包含全部主程式與模組陳述句。
	mergedVarTypes := buildVarTypes(merged)
	mergedSizeMap := buildSizeMaps(merged)
	validateFuncStrReturns = mergedSizeMap.funcStrReturns
	if err := validateArrayBounds(merged, mergedSizeMap.arraySizes, mergedSizeMap.sliceSizes, mergedSizeMap.stringSizes, mergedVarTypes); err != nil {
		return "", err
	}
	// 編譯期未初始化變數檢查：循環體內聲明的變數在循環外使用
	// 必須在模組合併後執行，才能檢查到導入模組（如 md5.no）中的問題
	if err := validateLoopScopedVars(merged); err != nil {
		return "", err
	}
	// vet 模式：前端驗證（語法+型別+模組合併+單態化）已全部完成，
	// 跳過 LLVM IR 生成以大幅加速 `no vet`。
	if t.vetMode {
		return "", nil
	}
	// 傳播目標平台到 LLVM generator，讓 Generate 內部的平台過濾使用目標平台
	// 而非編譯主機平台（支援交叉編譯）。
	t.llvmGenerator.SetTargetPlatform(t.targetGoos, t.targetGoarch)
	t.llvmGenerator.SetNoBoundsCheck(t.noBoundsCheck)
	// 傳遞主檔案名稱集合，讓 generator 能區分主檔案全域變數的合法重新賦值
	// 與導入模組函數中的同名局部變數（如 bigint.cmp 中的 result 不應誤寫到 @result）
	t.llvmGenerator.SetMainFileNames(mainVarNames)
	ir := t.llvmGenerator.Generate(merged)
	if errs := t.llvmGenerator.CodegenErrors(); len(errs) > 0 {
		return "", fmt.Errorf("codegen errors: %v", errs)
	}
	return ir, nil
}
// monomorphizeGenerics 對泛型函數進行單態化
func monomorphizeGenerics(program *parser.Program, varTypes map[string]string, typeOwner map[string]string) {
	// 收集所有泛型函數定義
	genericFns := make(map[string]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				genericFns[fd.Name] = fd
			} else if isGenericMethod(fd.Name) {
				// Method definitions like [n]t.fill have implicit generic params
				genericFns[fd.Name] = fd
			}
		}
	}
	if len(genericFns) == 0 {
		return
	}
	// 遞迴掃描所有陳述句尋找泛型呼叫（包括函數體內）
	var newStmts []parser.Statement
	for _, stmt := range program.Statements {
		scanStmtForGenericCalls(stmt, genericFns, varTypes, program, &newStmts, typeOwner)
	}
	program.Statements = append(program.Statements, newStmts...)
}
// isGenericMethod checks if a function name like "[n]t.method" has generic type params
func isGenericMethod(name string) bool {
	if len(name) > 3 && name[0] == '[' {
		closeB := strings.IndexByte(name, ']')
		if closeB > 0 && closeB+1 < len(name) {
			sizeParam := name[1:closeB]
			elemParam := name[closeB+1:]
			// Check for "." separator
			dotIdx := strings.IndexByte(elemParam, '.')
			if dotIdx > 0 {
				elem := elemParam[:dotIdx]
				return (isLowerLetter(sizeParam) || sizeParam == "") && isLowerLetter(elem)
			}
		}
	}
	if strings.HasPrefix(name, "[].") {
		return false // [].method - no generics
	}
	if len(name) > 2 && name[0] == '[' && name[1] == ']' {
		dotIdx := strings.IndexByte(name, '.')
		if dotIdx > 2 {
			elem := name[2:dotIdx]
			return isLowerLetter(elem)
		}
	}
	return false
}
// scanStmtForGenericCalls recursively scans statements for generic calls
func scanStmtForGenericCalls(stmt parser.Statement, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if ce, ok := s.Expression.(*parser.CallExpression); ok {
			processCallExpression(ce, genericFns, varTypes, program, newStmts, typeOwner)
		}
		// Also handle IfExpression (e.g. `if cond { ... }` as a statement),
		// whose Condition may contain method calls (e.g. `elif path.starts-with(x)`).
		if ie, ok := s.Expression.(*parser.IfExpression); ok {
			scanIfExpressionForGenericCalls(ie, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			// Build per-function varTypes to avoid cross-function name pollution.
			// The global varTypes is shared across all functions, so a local variable
			// named `resp` of type `str` in one function would pollute the lookup for
			// a parameter named `resp` of type `response` in another function.
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range s.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range s.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(s.Body, funcVarTypes)
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts, typeOwner)
			}
		}
	case *parser.LetStatement:
		// Method definitions: type.method = (params) { ... }
		// These are LetStatements with FunctionLiteral values; scan their bodies
		// for method calls that need resolution.
		if fl, ok := s.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range fl.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fl.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fl.Body, funcVarTypes)
			for _, bodyStmt := range fl.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts, typeOwner)
			}
		} else if s.Value != nil {
			// Regular variable assignment: scan the value expression
			// for method calls that need resolution (e.g., s = "A".to-str())
			scanExprForGenericCalls(s.Value, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts, typeOwner)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
}
// scanIfExpressionForGenericCalls recursively scans an IfExpression's Condition,
// Consequence, and Alternative for method calls that need resolution.
func scanIfExpressionForGenericCalls(ie *parser.IfExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	if ie.Condition != nil {
		scanExprForGenericCalls(ie.Condition, genericFns, varTypes, program, newStmts, typeOwner)
	}
	if ie.Consequence != nil {
		for _, s := range ie.Consequence.Statements {
			scanStmtForGenericCalls(s, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
	if ie.Alternative != nil {
		for _, s := range ie.Alternative.Statements {
			scanStmtForGenericCalls(s, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
}
// scanExprForGenericCalls recursively walks an expression tree to find
// CallExpressions (including method calls) that need generic/method resolution.
func scanExprForGenericCalls(expr parser.Expression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	switch e := expr.(type) {
	case *parser.CallExpression:
		processCallExpression(e, genericFns, varTypes, program, newStmts, typeOwner)
		for _, arg := range e.Arguments {
			scanExprForGenericCalls(arg, genericFns, varTypes, program, newStmts, typeOwner)
		}
	case *parser.InfixExpression:
		scanExprForGenericCalls(e.Left, genericFns, varTypes, program, newStmts, typeOwner)
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.PrefixExpression:
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.IfExpression:
		scanIfExpressionForGenericCalls(e, genericFns, varTypes, program, newStmts, typeOwner)
	case *parser.GroupedExpression:
		scanExprForGenericCalls(e.Expression, genericFns, varTypes, program, newStmts, typeOwner)
	}
}
// monomorphizeUnions 對聯合型別（union type alias）進行單態化。
// 對每個帶有 ..T（T 為 union alias）的函數（variadic），或參數/結果
// 使用 union alias 的非 variadic 函數，生成 N 個函數（每個 union
// 成員一個），函數名為 "<原名>__<成員>"。原函數定義保留作為「範本」
// 供後續步驟識別用途，但會在 codegen 階段被跳過（靠 IsVariadic &&
// VariadicUnion != "" 判斷；或 GenericUnion != "" 判斷）。
func monomorphizeUnions(program *parser.Program) {
	aliases, _ := checker.ValidateUnionTypes(program)
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] monomorphizeUnions: %d aliases, %d statements\n", len(aliases), len(program.Statements))
		typeAliasCount := 0
		for _, stmt := range program.Statements {
			if ta, ok := stmt.(*parser.TypeAlias); ok {
				typeAliasCount++
				fmt.Fprintf(os.Stderr, "[union-debug]   TypeAlias: name=%s isUnion=%v\n", ta.Name, ta.IsUnion())
			}
		}
		fmt.Fprintf(os.Stderr, "[union-debug]   total TypeAlias statements: %d\n", typeAliasCount)
		for name, ta := range aliases {
			fmt.Fprintf(os.Stderr, "[union-debug]   alias %s: isUnion=%v\n", name, ta.IsUnion())
		}
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				fmt.Fprintf(os.Stderr, "[union-debug]   func %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, fd.VariadicUnion, fd.GenericUnion)
			}
		}
	}
	if len(aliases) == 0 {
		return
	}
	// 收集所有需要單態化的函數
	type pending struct {
		fd        *parser.FunctionDefinition
		unionName string
		members   []parser.Type
	}
	var pendingFns []pending
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var unionName string
		if fd.IsVariadic && fd.VariadicUnion != "" {
			unionName = fd.VariadicUnion
		} else if !fd.IsVariadic && fd.GenericUnion != "" {
			unionName = fd.GenericUnion
		}
		if unionName == "" {
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] skip %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, fd.VariadicUnion, fd.GenericUnion)
			}
			continue
		}
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] monomorphize %s: union=%s\n", fd.Name, unionName)
		}
		members := checker.FlattenUnion(unionName, aliases)
		if len(members) == 0 {
			continue
		}
		pendingFns = append(pendingFns, pending{fd: fd, unionName: unionName, members: members})
	}
	if len(pendingFns) == 0 {
		return
	}
	var newStmts []parser.Statement
	for _, p := range pendingFns {
		for _, mem := range p.members {
			nt, ok := mem.(*parser.NamedType)
			if !ok {
				continue
			}
			concrete := cloneUnionVariant(p.fd, nt.Value, aliases)
			newStmts = append(newStmts, concrete)
		}
		// 標記原函數為「範本」：在 name 末尾加 __TEMPLATE 使其不與生成版本衝突
		p.fd.Name = p.fd.Name + "__" + p.unionName + "_TEMPLATE"
	}
	program.Statements = append(program.Statements, newStmts...)
}
// rewriteUnionCalls 重寫對聯合型別泛型函數的呼叫。
// 在 monomorphizeUnions 之後，原函數被改名為 "<name>__<union>_TEMPLATE"，
// 具體版本為 "<name>__<memberType>"。此函數遍歷所有呼叫點，
// 根據引數型別推斷應使用的具體版本，並將呼叫名改寫為 "<name>__<memberType>"。
func rewriteUnionCalls(program *parser.Program, varTypes map[string]string) {
	// 收集所有模板函數：原名 → templateInfo
	// 模板名格式：<origName>__<unionName>_TEMPLATE
	templates := make(map[string]*unionTemplateInfo)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if strings.HasSuffix(fd.Name, "_TEMPLATE") {
			base := strings.TrimSuffix(fd.Name, "_TEMPLATE")
			parts := strings.SplitN(base, "__", 2)
			if len(parts) != 2 {
				continue
			}
			origName := parts[0]
			unionName := parts[1]
			aliases, _ := checker.ValidateUnionTypes(program)
			members := checker.FlattenUnion(unionName, aliases)
			memberNames := make([]string, 0, len(members))
			for _, m := range members {
				if nt, ok := m.(*parser.NamedType); ok {
					memberNames = append(memberNames, nt.Value)
				}
			}
			templates[origName] = &unionTemplateInfo{origName: origName, unionName: unionName, members: memberNames}
		}
	}
	if len(templates) == 0 {
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: no templates found\n")
		}
		return
	}
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: %d templates\n", len(templates))
		for name, tpl := range templates {
			fmt.Fprintf(os.Stderr, "[union-debug]   template %s: union=%s members=%v\n", name, tpl.unionName, tpl.members)
		}
	}
	// 遍歷所有語句，重寫呼叫
	rewriteUnionCallStmts(program.Statements, templates, varTypes)
}
// rewriteUnionCallStmts 遍歷語句列表，對每個語句中的聯合型別泛型呼叫進行重寫。
// 此函數與 rewriteUnionCallExpr 互相遞迴：rewriteUnionCallExpr 處理 IfExpression
// 的 Consequence/Alternative 時會呼叫本函數，以正確走訪所有語句類型
// （包括 LetStatement、ReturnStatement 等，而不僅是 ExpressionStatement）。
func rewriteUnionCallStmts(stmts []parser.Statement, templates map[string]*unionTemplateInfo, vt map[string]string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.ExpressionStatement:
			rewriteUnionCallExpr(s.Expression, templates, vt)
		case *parser.LetStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt)
			}
		case *parser.FunctionDefinition:
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] walk FunctionDefinition: %s, body=%d stmts\n", s.Name, len(s.Body.Statements))
			}
			if s.Body != nil {
				// Augment varTypes with the function's parameter types to
				// correctly infer argument types for identifier expressions.
				// This prevents cross-module template matching (e.g. bigint.gcd
				// should not be rewritten by the number.gcd template).
				localVt := make(map[string]string)
				for k, v := range vt {
					localVt[k] = v
				}
				for _, param := range s.Parameters {
					if nt, ok := param.Type.(*parser.NamedType); ok {
						localVt[param.Name] = nt.Value
					}
				}
				for _, result := range s.Results {
					if nt, ok := result.Type.(*parser.NamedType); ok {
						localVt[result.Name] = nt.Value
					}
				}
				// Also collect local LetStatement types so that variables shadowing
				// globals or same-name locals from other functions (e.g. `c` in
				// dns-parse-records vs `c []str` elsewhere) resolve to the correct
				// union member type. Without this, a single global varTypes entry
				// would leak across functions and produce wrong memberType inference.
				collectVarTypesFromBody(s.Body, localVt)
				rewriteUnionCallStmts(s.Body.Statements, templates, localVt)
			}
		case *parser.BlockStatement:
			rewriteUnionCallStmts(s.Statements, templates, vt)
		case *parser.ForStatement:
			if s.Condition != nil {
				rewriteUnionCallExpr(s.Condition, templates, vt)
			}
			if s.Body != nil {
				rewriteUnionCallStmts(s.Body.Statements, templates, vt)
			}
		case *parser.MultiAssignStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				rewriteUnionCallExpr(s.ReturnValue, templates, vt)
			}
		}
	}
}
// unionTemplateInfo 記錄聯合型別模板函數的資訊
type unionTemplateInfo struct {
	origName  string
	unionName string
	members   []string
}
// rewriteUnionCallExpr 遞迴重寫表達式中的聯合型別呼叫
func rewriteUnionCallExpr(expr parser.Expression, templates map[string]*unionTemplateInfo, varTypes map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// 先遞迴處理引數中的呼叫
		for _, arg := range e.Arguments {
			rewriteUnionCallExpr(arg, templates, varTypes)
		}
		// 處理 curried 呼叫：(innerCall)(args)
		if _, ok := e.Function.(*parser.CallExpression); ok {
			rewriteUnionCallExpr(e.Function, templates, varTypes)
			return
		}
		// 檢查是否為聯合型別泛型呼叫
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if tpl, exists := templates[ident.Value]; exists {
				memberType := inferArgMemberType(e, tpl, varTypes)
				if os.Getenv("NOLANG_UNION_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[union-debug] rewrite call %s: memberType=%s\n", ident.Value, memberType)
				}
				if memberType != "" {
					// Verify memberType is a valid member of the template's union.
					// This prevents incorrectly rewriting calls from different
					// modules that share the same function name (e.g. bigint.gcd
					// should not be rewritten by the number.gcd template).
					isValid := false
					for _, m := range tpl.members {
						if m == memberType {
							isValid = true
							break
						}
					}
					if isValid {
						ident.Value = ident.Value + "__" + memberType
						if os.Getenv("NOLANG_UNION_DEBUG") != "" {
							fmt.Fprintf(os.Stderr, "[union-debug]   rewritten to %s\n", ident.Value)
						}
					}
				}
			} else {
				// Not found by plain name; try method-style resolution
				// e.g., "sign" → "num.sign"
				for tplName, tpl := range templates {
					if strings.HasSuffix(tplName, "."+ident.Value) {
						// Found a method template, rewrite the identifier to use it
						memberType := inferArgMemberType(e, tpl, varTypes)
						if memberType != "" {
							isValid := false
							for _, m := range tpl.members {
								if m == memberType {
									isValid = true
									break
								}
							}
							if isValid {
								ident.Value = tplName + "__" + memberType
								if os.Getenv("NOLANG_UNION_DEBUG") != "" {
									fmt.Fprintf(os.Stderr, "[union-debug]   rewritten (method) %s → %s\n", ident.Value, ident.Value)
								}
								break
							}
						}
					}
				}
			}
		}
	case *parser.InfixExpression:
		rewriteUnionCallExpr(e.Left, templates, varTypes)
		rewriteUnionCallExpr(e.Right, templates, varTypes)
	case *parser.PrefixExpression:
		rewriteUnionCallExpr(e.Right, templates, varTypes)
	case *parser.IfExpression:
		if e.Condition != nil {
			rewriteUnionCallExpr(e.Condition, templates, varTypes)
		}
		// 走訪 Consequence/Alternative 中的所有語句類型（不僅 ExpressionStatement），
		// 以正確重寫 LetStatement（如 `blen-str = body-len.to-str()`）等內部的呼叫。
		// 僅走 ExpressionStatement 會導致 standalone if-then (`cond -> { let = call() }`)
		// 內的聯合型別呼叫未被重寫，產生 undefined `@int.to-str` 錯誤。
		if e.Consequence != nil {
			rewriteUnionCallStmts(e.Consequence.Statements, templates, varTypes)
		}
		if e.Alternative != nil {
			rewriteUnionCallStmts(e.Alternative.Statements, templates, varTypes)
		}
	case *parser.AssignExpression:
		rewriteUnionCallExpr(e.Value, templates, varTypes)
	}
}
// inferArgMemberType 從呼叫引數推斷應使用的聯合成員型別
func inferArgMemberType(call *parser.CallExpression, tpl *unionTemplateInfo, varTypes map[string]string) string {
	if len(call.Arguments) == 0 {
		return ""
	}
	// 對於 variadic 函數（..num），使用第一個引數的型別
	// 對於非 variadic 函數（abs(a num)），也使用第一個引數的型別
	firstArg := call.Arguments[0]
	return inferExprMemberType(firstArg, varTypes)
}
// inferExprMemberType 從表達式推斷聯合成員型別
func inferExprMemberType(expr parser.Expression, varTypes map[string]string) string {
	switch v := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64" // 整數字面常量預設為 i64
	case *parser.FloatLiteral:
		return "f64"
	case *parser.Identifier:
		// Look up the variable's actual type, default to i64
		if varTypes != nil {
			if t, ok := varTypes[v.Value]; ok {
				return t
			}
		}
		return "i64"
	case *parser.PrefixExpression:
		return inferExprMemberType(v.Right, varTypes)
	case *parser.GroupedExpression:
		return inferExprMemberType(v.Expression, varTypes)
	}
	return "i64"
}
// inferTypeFromExpr 嘗試從值表達式推斷變數型別。無法推斷時返回空白字串。
func inferTypeFromExpr(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		// 十六進位字面量（0xNN）推斷為 byte，十進位整數推斷為 i64
		raw := e.Token.Literal
		if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
			return "byte"
		}
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.StructLiteral:
		return e.Type
	case *parser.PrefixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return inferTypeFromExpr(e.Right)
		}
		if e.Operator == "!" {
			return "bool"
		}
	case *parser.InfixExpression:
		// Comparison and logical operators always produce bool
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		}
		if t := inferTypeFromExpr(e.Left); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Right)
	case *parser.GroupedExpression:
		return inferTypeFromExpr(e.Expression)
	case *parser.ConditionalExpression:
		// `cond ? a : b` — type is the type of the consequence (or alternative)
		if t := inferTypeFromExpr(e.Consequence); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Alternative)
	case *parser.MapLiteral:
		if e.MapType != nil {
			return e.MapType.String()
		}
		if len(e.Pairs) > 0 {
			keyType := inferTypeFromExpr(e.Pairs[0].Key)
			valType := inferTypeFromExpr(e.Pairs[0].Value)
			if keyType != "" && valType != "" {
				return "[" + keyType + "]" + valType
			}
		}
		return ""
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return "str"
			case "i64-to-str":
				return "str"
			case "f64-to-str":
				return "str"
			case "bool-to-str":
				return "str"
			case "byte-to-str":
				return "str"
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
		// Method call: receiver.load-le-u32(off) → DotExpression
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			switch dot.Property {
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
	}
	return ""
}
// isValidType 檢查是否為有效的 Nolang 型別名
func isValidType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128", "f32", "f64":
		return true
	}
	return false
}
// cloneUnionVariant 為 union 函數的某個成員型別複製一份具體實例。
// 替換函數簽名中的 variadic 元素型別為該成員（若為 variadic），
// 並將所有 union 別名的參數/結果型別替換為具體成員型別。
// 對於函數體內所有對「自己」的遞迴呼叫改名為具體版本。
func cloneUnionVariant(fd *parser.FunctionDefinition, memberType string, aliases map[string]*parser.TypeAlias) *parser.FunctionDefinition {
	// 簡單深拷貝：先淺拷貝結構體，再逐欄位拷貝容器。
	clone := *fd
	clone.Parameters = make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		pCopy := *p
		if i == len(fd.Parameters)-1 && fd.IsVariadic {
			// 最後一個參數是 variadic；元素型別改為具體成員
			var tok lexer.Token
			// Use the underlying type's token if available, otherwise the param token
			if p.Type != nil {
				if st, ok := p.Type.(*parser.SliceType); ok {
					tok = st.Token
				}
			}
			if tok.Type == 0 {
				tok = p.Token
			}
			pCopy.Type = &parser.SliceType{
				Token: tok,
				Elem:  &parser.NamedType{Value: memberType},
			}
		} else {
			// 非 variadic 參數：若型別是 union 別名，替換為具體成員
			if nt, ok := p.Type.(*parser.NamedType); ok {
				if _, isUnion := aliases[nt.Value]; isUnion {
					pCopy.Type = &parser.NamedType{Value: memberType}
				}
			}
		}
		clone.Parameters[i] = &pCopy
	}
	clone.Results = make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		rCopy := *r
		// 若結果型別是 union 別名，替換為具體成員型別
		if nt, ok := r.Type.(*parser.NamedType); ok {
			if _, isUnion := aliases[nt.Value]; isUnion {
				rCopy.Type = &parser.NamedType{Value: memberType}
			}
		}
		clone.Results[i] = &rCopy
	}
	clone.Name = fd.Name + "__" + memberType
	// 重設 union 標記：實例化後該函數就是具體的
	clone.VariadicUnion = ""
	clone.GenericUnion = ""
	// 深拷貝 Body
	clone.Body = cloneBlockForUnion(fd.Body, fd.Name, clone.Name, memberType)
	return &clone
}
// cloneBlockForUnion 深拷貝一個 block，遞迴地把對 <oldName> 的呼叫
// 改名為 <newName>。<memberType> 是當前單態化的具體型別。
func cloneBlockForUnion(bs *parser.BlockStatement, oldName, newName, memberType string) *parser.BlockStatement {
	if bs == nil {
		return nil
	}
	out := &parser.BlockStatement{Token: bs.Token, RBrace: bs.RBrace}
	for _, s := range bs.Statements {
		out.Statements = append(out.Statements, cloneStmtForUnion(s, oldName, newName, memberType))
	}
	return out
}
func cloneStmtForUnion(stmt parser.Statement, oldName, newName, memberType string) parser.Statement {
	if stmt == nil {
		return nil
	}
	// IfExpression 在源碼中是 *parser.ExpressionStatement 包裝的
	// IfExpression（因為 IfExpression 實現了 Expression 而非 Statement）。
	// 我們在 ExpressionStatement case 內處理遞歸；不再單獨 case *IfExpression。
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		// shallow-copy the wrapper and rewrite its expression
		es := *s
		es.Expression = cloneExprForUnion(s.Expression, oldName, newName, memberType)
		return &es
	case *parser.LetStatement:
		ls := *s
		if s.Name != nil {
			n := *s.Name
			ls.Name = &n
		}
		ls.Type = s.Type
		ls.Value = cloneExprForUnion(s.Value, oldName, newName, memberType)
		return &ls
	case *parser.BlockStatement:
		return cloneBlockForUnion(s, oldName, newName, memberType)
	case *parser.ForStatement:
		fs := *s
		if s.IterRange != nil {
			fs.IterRange = cloneIterForUnion(s.IterRange, oldName, newName, memberType)
		}
		if s.Condition != nil {
			fs.Condition = cloneExprForUnion(s.Condition, oldName, newName, memberType)
		}
		fs.Body = cloneBlockForUnion(s.Body, oldName, newName, memberType)
		return &fs
	case *parser.ReturnStatement:
		rs := *s
		rs.ReturnValue = cloneExprForUnion(s.ReturnValue, oldName, newName, memberType)
		return &rs
	case *parser.MultiAssignStatement:
		mas := *s
		mas.Targets = append([]parser.Expression{}, s.Targets...)
		mas.Value = cloneExprForUnion(s.Value, oldName, newName, memberType)
		return &mas
	}
	// Fallback: shallow copy via type assertion to the concrete type
	return stmt
}
func cloneIterForUnion(it *parser.IterationExpr, oldName, newName, memberType string) *parser.IterationExpr {
	if it == nil {
		return nil
	}
	cp := *it
	if it.Range != nil {
		// RangeExpression has Start and End
		cp.Range = cloneRangeForUnion(it.Range, oldName, newName, memberType)
	}
	if it.RangeExpr != nil {
		cp.RangeExpr = cloneExprForUnion(it.RangeExpr, oldName, newName, memberType)
	}
	return &cp
}
func cloneRangeForUnion(r *parser.RangeExpression, oldName, newName, memberType string) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Start != nil {
		cp.Start = cloneExprForUnion(r.Start, oldName, newName, memberType)
	}
	if r.End != nil {
		cp.End = cloneExprForUnion(r.End, oldName, newName, memberType)
	}
	return &cp
}
func cloneExprForUnion(expr parser.Expression, oldName, newName, memberType string) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		ce := *e
		ce.Function = cloneExprForUnion(e.Function, oldName, newName, memberType)
		ce.Arguments = make([]parser.Expression, len(e.Arguments))
		for i, a := range e.Arguments {
			ce.Arguments[i] = cloneExprForUnion(a, oldName, newName, memberType)
		}
		return &ce
	case *parser.Identifier:
		cp := *e
		if cp.Value == oldName {
			cp.Value = newName
		}
		return &cp
	case *parser.DotExpression:
		de := *e
		de.Receiver = cloneExprForUnion(e.Receiver, oldName, newName, memberType)
		return &de
	case *parser.IfExpression:
		ie := *e
		ie.Condition = cloneExprForUnion(e.Condition, oldName, newName, memberType)
		ie.Consequence = cloneBlockForUnion(e.Consequence, oldName, newName, memberType)
		if e.Alternative != nil {
			ie.Alternative = cloneBlockForUnion(e.Alternative, oldName, newName, memberType)
		}
		return &ie
	case *parser.InfixExpression:
		ie := *e
		ie.Left = cloneExprForUnion(e.Left, oldName, newName, memberType)
		ie.Right = cloneExprForUnion(e.Right, oldName, newName, memberType)
		return &ie
	case *parser.PrefixExpression:
		pe := *e
		pe.Right = cloneExprForUnion(e.Right, oldName, newName, memberType)
		return &pe
	}
	return expr
}
// processCallExpression handles a single CallExpression for generic resolution
func processCallExpression(ce *parser.CallExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement, typeOwner map[string]string) {
	// Regular function call: fn(args)
	if fnName, ok := ce.Function.(*parser.Identifier); ok {
		if fd, exists := genericFns[fnName.Value]; exists {
			genericArgs := ce.GenericArgs
			if len(genericArgs) == 0 {
				genericArgs = inferGenericArgs(fd, ce, program)
			}
			if len(genericArgs) > 0 {
				concrete := cloneAndSubstitute(fd, genericArgs)
				*newStmts = append(*newStmts, concrete)
				fnName.Value = concrete.Name
				ce.GenericArgs = nil
			}
		}
	}
	// Method call: receiver.method(args)
	if dot, ok := ce.Function.(*parser.DotExpression); ok {
		resolveMethodCall(dot, ce, genericFns, varTypes, newStmts, program, typeOwner)
	}
	// Recurse into arguments
	for _, arg := range ce.Arguments {
		if innerCe, ok := arg.(*parser.CallExpression); ok {
			processCallExpression(innerCe, genericFns, varTypes, program, newStmts, typeOwner)
		}
	}
}
// fnExistsInProgram checks if a function or method with the given name exists
// in the program's top-level statements. Method definitions (e.g. f64.to-str,
// int.to-str) are stored as *parser.FunctionDefinition with the full dotted
// name, so a simple Name match suffices.
func fnExistsInProgram(program *parser.Program, name string) bool {
	if program == nil {
		return false
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == name {
			return true
		}
	}
	return false
}
// resolveMethodCall resolves a DotExpression-based method call.
// Returns true if the call was resolved and rewritten.
func resolveMethodCall(dot *parser.DotExpression, ce *parser.CallExpression,
	genericFns map[string]*parser.FunctionDefinition, varTypes map[string]string,
	newStmts *[]parser.Statement, program *parser.Program, typeOwner map[string]string) bool {
	// Get receiver variable name and type
	recvIdent, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return false
	}
	recvType, ok := varTypes[recvIdent.Value]
	if !ok {
		return false
	}
	methodName := dot.Property
	// Search for matching generic method
	for name, fd := range genericFns {
		dotIdx := strings.LastIndex(name, ".")
		if dotIdx < 0 {
			continue
		}
		typePrefix := name[:dotIdx]
		methodSuffix := name[dotIdx+1:]
		if methodSuffix != methodName {
			continue
		}
		// Try to match typePrefix (e.g., "[n]t") against recvType (e.g., "[4]i64")
		genericArgs := matchTypePattern(typePrefix, recvType, fd)
		if len(genericArgs) == 0 {
			continue
		}
		// Create concrete version
		concrete := cloneAndSubstitute(fd, genericArgs)
		*newStmts = append(*newStmts, concrete)
		// Rewrite call: replace DotExpression with Identifier, prepend receiver
		ce.Function = &parser.Identifier{
			Token: lexer.Token{Type: lexer.IDENT, Literal: concrete.Name},
			Value: concrete.Name,
		}
		// Prepend receiver as first argument
		receiverArg := &parser.Identifier{
			Token: recvIdent.Token,
			Value: recvIdent.Value,
		}
		ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
		return true
	}
	// Try non-generic method: type.method already exists
	// Rewrite to direct call with receiver prepended
	// Map types use "hashmap-K-V" naming convention (not "[K]V")
	// Option type (?T): method is defined on the inner type T, not on option.
	// Strip the leading "?" so concreteName becomes "T.method" (e.g. ?str.to-lower → str.to-lower).
	recvTypeForMethod := recvType
	if strings.HasPrefix(recvTypeForMethod, "?") {
		recvTypeForMethod = recvTypeForMethod[1:]
	}
	// Apply module prefix if recvTypeForMethod is a bare user-defined type name.
	// varTypes is built before rewriteTypeRefs, so it may contain the un-prefixed
	// form (e.g. "conn" instead of "tls.conn"). Without this, the rewrite
	// would produce "conn.recv" but the function definition was renamed to
	// "tls.conn.recv" by prefixMethodNames.
	// typeOwner 的 key 為 "module.name"，value 為 bareName，故複用 prefixTypeName
	// 進行查找：恰好一個模組定義該型別時補上前綴，多個模組同名時保持原樣。
	if typeOwner != nil {
		recvTypeForMethod = prefixTypeName(recvTypeForMethod, typeOwner)
	}
	concreteName := recvTypeForMethod + "." + methodName
	if hmName := mapTypeToHashmapName(recvTypeForMethod); hmName != "" {
		concreteName = hmName + "." + methodName
	}
	// Don't rewrite if this is a builtin method (e.g. vec.push, vec.clear).
	// Builtins are registered with LLVM type prefixes (vec, arr, str, etc.),
	// not Nolang type names ([]str, []i64, etc.). The LLVM code generator
	// handles builtin method calls via DotExpression dispatch + ForwardFunc.
	// Rewriting them to []str.push(out, ...) would create undefined function calls.
	if strings.HasPrefix(recvType, "[]") {
		// Slice types ([]str, []i64, []byte, etc.) map to "vec" builtins.
		// Check both "vec.<method>" and the concrete type prefix.
		if builtin.FindBuiltinMethod("vec."+methodName) != nil {
			return false
		}
		// Also check the concrete name (e.g. []byte.slice is registered directly)
		if builtin.FindBuiltinMethod(concreteName) != nil {
			return false
		}
	}
	if strings.HasPrefix(recvType, "[") && !strings.HasPrefix(recvType, "[]") {
		// Array types ([N]T) map to "arr" builtins
		if builtin.FindBuiltinMethod("arr."+methodName) != nil {
			return false
		}
		// Some array builtins are registered under the slice name (e.g. []byte.zero
		// via ForwardFunc "arr-zero"); the codegen dispatches them for [N]T too.
		// Strip the size prefix so [32]byte.zero matches []byte.zero registration.
		if closeIdx := strings.IndexByte(recvType, ']'); closeIdx > 0 && closeIdx+1 < len(recvType) {
			elemType := recvType[closeIdx+1:]
			sliceBuiltinName := "[]" + elemType + "." + methodName
			if builtin.FindBuiltinMethod(sliceBuiltinName) != nil {
				return false
			}
		}
	}
	// Check concrete name for other types (str, i64, etc.)
	if builtin.FindBuiltinMethod(concreteName) != nil {
		return false
	}
	// Check if recvType is a member of a union type alias
	// If so, use the union alias prefix instead of the concrete type —
	// BUT only when the union method actually exists. When the union method
	// is not defined (e.g. float.to-str was removed in favor of f64.to-str),
	// keep the concrete type name so codegen can dispatch correctly.
	if program != nil {
		for _, stmt := range program.Statements {
			ta, ok := stmt.(*parser.TypeAlias)
			if !ok || ta.Union == nil {
				continue
			}
			for _, member := range ta.Union.Types {
				if nt, ok := member.(*parser.NamedType); ok && nt.Value == recvType {
					unionMethodName := ta.Name + "." + methodName
					if fnExistsInProgram(program, unionMethodName) {
						concreteName = unionMethodName
					}
					break
				}
			}
			if concreteName != recvType+"."+methodName {
				break
			}
		}
	}
	ce.Function = &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
		Value: concreteName,
	}
	receiverArg := &parser.Identifier{
		Token: recvIdent.Token,
		Value: recvIdent.Value,
	}
	ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
	return true
}
// matchTypePattern matches a type pattern like "[n]t" against a concrete type like "[4]i64".
// Returns generic args (e.g., n=4, t=i64) or nil if no match.
func matchTypePattern(pattern, concrete string, fd *parser.FunctionDefinition) []parser.Expression {
	// Match [n]t against [4]i64
	if len(pattern) > 3 && pattern[0] == '[' {
		closeBracket := strings.IndexByte(pattern, ']')
		if closeBracket > 0 && closeBracket+1 < len(pattern) {
			sizeParam := pattern[1:closeBracket]
			elemParam := pattern[closeBracket+1:]
			if len(concrete) > 2 && concrete[0] == '[' {
				argClose := strings.IndexByte(concrete, ']')
				if argClose > 0 {
					argSize := concrete[1:argClose]
					argElem := concrete[argClose+1:]
					// [n]t pattern is for fixed-size arrays only.
					// If argSize is empty, concrete is a slice ([]T), not an array,
					// and must be handled by the []t slice pattern below.
					// Without this guard, both [n]t and []t patterns would match
					// []i64, and non-deterministic map iteration could dispatch
					// vec calls to arr specializations (causing wrong results/segfaults).
					if argSize == "" {
						return nil
					}
					var args []parser.Expression
					if isLowerLetter(sizeParam) {
						// [n]t pattern requires a numeric size; non-numeric argSize
						// (e.g. MapType [str]i64 where argSize="str") must not match.
						val, err := strconv.ParseInt(argSize, 10, 64)
						if err != nil {
							return nil
						}
						args = append(args, &parser.IntegerLiteral{Value: val})
					}
					if isLowerLetter(elemParam) {
						args = append(args, &parser.StringLiteral{Value: argElem})
					}
					if len(args) > 0 {
						return args
					}
				}
			}
		}
	}
	// Match []t against []i64 (slice pattern)
	if strings.HasPrefix(pattern, "[]") {
		elemParam := pattern[2:]
		if strings.HasPrefix(concrete, "[]") {
			argElem := concrete[2:]
			if isLowerLetter(elemParam) {
				return []parser.Expression{&parser.StringLiteral{Value: argElem}}
			}
		}
	}
	return nil
}
// inferGenericArgs 從函數呼叫的引數型別推斷泛型參數
// 例如 fn(arr [n]t) 被以 [8]byte 引數呼叫 → n=8, t=byte
func inferGenericArgs(fd *parser.FunctionDefinition, call *parser.CallExpression, program *parser.Program) []parser.Expression {
	if len(fd.Parameters) == 0 || len(call.Arguments) == 0 {
		return nil
	}
	var args []parser.Expression
	for pi, param := range fd.Parameters {
		if pi >= len(call.Arguments) {
			break
		}
		arg := call.Arguments[pi]
		argType := inferArgType(arg, program)
		paramType := param.Type.String()
		// 匹配泛型型別：t 與具體型別 i64
		if len(paramType) == 1 && paramType[0] >= 'a' && paramType[0] <= 'z' {
			if isLowerLetter(paramType) && argType != "" {
				args = append(args, &parser.StringLiteral{Value: argType})
			}
		}
		// 匹配參數型別 [n]t 與引數型別 [8]byte
		if len(paramType) > 3 && paramType[0] == '[' {
			closeBracket := strings.IndexByte(paramType, ']')
			if closeBracket > 0 && closeBracket+1 < len(paramType) {
				sizeParam := paramType[1:closeBracket]  // n
				elemParam := paramType[closeBracket+1:] // t
				// 從引數型別中提取具體值
				if len(argType) > 2 && argType[0] == '[' {
					argClose := strings.IndexByte(argType, ']')
					if argClose > 0 {
						argSize := argType[1:argClose]  // 8
						argElem := argType[argClose+1:] // byte
						if isLowerLetter(sizeParam) {
							if val, err := strconv.ParseInt(argSize, 10, 64); err == nil {
								args = append(args, &parser.IntegerLiteral{Value: val})
							}
						}
						if isLowerLetter(elemParam) {
							// 型別引數目前用字串表示
							args = append(args, &parser.StringLiteral{Value: argElem})
						}
					}
				}
			}
		}
	}
	return args
}
func isLowerLetter(s string) bool {
	return len(s) == 1 && s[0] >= 'a' && s[0] <= 'z'
}
func inferArgType(expr parser.Expression, program *parser.Program) string {
	switch e := expr.(type) {
	case *parser.Identifier:
		for _, stmt := range program.Statements {
			if ls, ok := stmt.(*parser.LetStatement); ok {
				if ls.Name != nil && ls.Name.Value == e.Value && ls.Type != nil {
					return ls.Type.String()
				}
			}
		}
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.GroupedExpression:
		return inferArgType(e.Expression, program)
	}
	return ""
}
func stmtCount(body *parser.BlockStatement) int {
	if body == nil {
		return -1
	}
	return len(body.Statements)
}
// cloneAndSubstitute 複製泛型函數並以具體值替換泛型參數
func cloneAndSubstitute(fd *parser.FunctionDefinition, genericArgs []parser.Expression) *parser.FunctionDefinition {
	if len(genericArgs) == 0 {
		return fd
	}
	// 複製並替換參數類型中的泛型標記
	subst := make(map[string]string) // 泛型參數名 → 具體值字串
	// For explicit generic params (positional matching)
	// Skip for implicit generic methods like [n]t.method - use name-based matching below
	isImplicitGenericMethod := len(fd.Name) > 3 && fd.Name[0] == '['
	if !isImplicitGenericMethod {
		for i, gp := range fd.GenericParams {
			if i < len(genericArgs) {
				if lit, ok := genericArgs[i].(*parser.IntegerLiteral); ok {
					subst[gp.Value] = fmt.Sprintf("%d", lit.Value)
				} else if lit, ok := genericArgs[i].(*parser.StringLiteral); ok {
					subst[gp.Value] = lit.Value
				}
			}
		}
	}
	// For implicit generic methods like [n]t.method:
	// Extract size/elem param names from the method name and match by type (not position)
	var sizeVal string
	var elemVal string
	for _, arg := range genericArgs {
		if lit, ok := arg.(*parser.IntegerLiteral); ok {
			sizeVal = fmt.Sprintf("%d", lit.Value)
		} else if lit, ok := arg.(*parser.StringLiteral); ok {
			elemVal = lit.Value
		}
	}
	if isImplicitGenericMethod {
		closeB := strings.IndexByte(fd.Name, ']')
		if closeB > 0 && closeB+1 < len(fd.Name) {
			sizeParam := fd.Name[1:closeB]
			elemPart := fd.Name[closeB+1:]
			dotIdx := strings.IndexByte(elemPart, '.')
			var elemParam string
			if dotIdx > 0 {
				elemParam = elemPart[:dotIdx]
			}
			// Add to subst if not already set by positional matching
			if isLowerLetter(sizeParam) && sizeVal != "" {
				if _, exists := subst[sizeParam]; !exists {
					subst[sizeParam] = sizeVal
				}
			}
			if isLowerLetter(elemParam) && elemVal != "" {
				if _, exists := subst[elemParam]; !exists {
					subst[elemParam] = elemVal
				}
			}
		}
	}
	// Build mangled name
	mangledName := fd.Name
	if isImplicitGenericMethod {
		// Replace generic type prefix with LLVM-safe name: [n]t.fill → _4xi64.fill
		closeB := strings.IndexByte(mangledName, ']')
		dotIdx := strings.IndexByte(mangledName, '.')
		if closeB > 0 && dotIdx > closeB {
			sizeParam := mangledName[1:closeB]
			elemParam := mangledName[closeB+1 : dotIdx]
			_ = sizeParam // used implicitly via isLowerLetter check below
			_ = elemParam
			if isLowerLetter(string(mangledName[1])) && isLowerLetter(string(mangledName[closeB+1])) {
				mangledName = "_" + sizeVal + "x" + elemVal + mangledName[dotIdx:]
			}
		}
	} else {
		// Regular generic function: append args to name
		for _, arg := range genericArgs {
			if lit, ok := arg.(*parser.IntegerLiteral); ok {
				mangledName += fmt.Sprintf(".%d", lit.Value)
			} else if lit, ok := arg.(*parser.StringLiteral); ok {
				mangledName += "." + lit.Value
			}
		}
	}
	// 複製參數
	newParams := make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		newParams[i] = &parser.Parameter{
			Token: p.Token,
			Name:  p.Name,
			Type:  substituteType(p.Type, subst),
		}
	}
	// 複製回傳值
	newResults := make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		newResults[i] = &parser.Parameter{
			Token: r.Token,
			Name:  r.Name,
			Type:  substituteType(r.Type, subst),
		}
	}
	// 複製並替換函數體
	newBody := substituteBody(fd.Body, subst)
	return &parser.FunctionDefinition{
		Token: fd.Token,
		Name:  mangledName,
		FuncSignature: parser.FuncSignature{
			GenericParams: nil, // 具體化後無泛型參數
			Parameters:    newParams,
			Results:       newResults,
		},
		Body:        newBody,
		IsMethodDef: fd.IsMethodDef,
	}
}
// substituteBody 遞迴替換函數體中的泛型參數
func substituteBody(body *parser.BlockStatement, subst map[string]string) *parser.BlockStatement {
	if body == nil || len(subst) == 0 {
		return body
	}
	newStmts := make([]parser.Statement, len(body.Statements))
	for i, stmt := range body.Statements {
		newStmts[i] = substituteStmt(stmt, subst)
	}
	return &parser.BlockStatement{
		Token:      body.Token,
		Statements: newStmts,
	}
}
func substituteStmt(stmt parser.Statement, subst map[string]string) parser.Statement {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return &parser.ExpressionStatement{
			Token:      s.Token,
			Expression: substituteExpr(s.Expression, subst),
		}
	case *parser.LetStatement:
		return &parser.LetStatement{
			Token:       s.Token,
			Name:        s.Name,
			Value:       substituteExpr(s.Value, subst),
			Type:        substituteType(s.Type, subst),
			IsSynthetic: s.IsSynthetic,
		}
	case *parser.ForStatement:
		newFor := &parser.ForStatement{
			Token:          s.Token,
			Body:           substituteBody(s.Body, subst),
			Label:          s.Label,
			IsCondWrapper:  s.IsCondWrapper,
		}
		if s.IterRange != nil {
			newFor.IterRange = &parser.IterationExpr{
				Variable:  s.IterRange.Variable,
				Range:     substituteRange(s.IterRange.Range, subst),
				RangeStr:  s.IterRange.RangeStr,
				RangeExpr: s.IterRange.RangeExpr,
			}
			// Also copy RangeExpr (identifier/slice) - it may contain generic types too
			if ident, ok := s.IterRange.RangeExpr.(*parser.Identifier); ok {
				if val, ok2 := subst[ident.Value]; ok2 {
					newFor.IterRange.RangeExpr = &parser.Identifier{Token: ident.Token, Value: val}
				}
			}
		}
		// 也替換 for i < n 條件中的 n
		if s.Condition != nil {
			newFor.Condition = substituteExpr(s.Condition, subst)
		}
		if s.CountExpr != nil {
			newFor.CountExpr = substituteExpr(s.CountExpr, subst)
		}
		return newFor
	case *parser.BlockStatement:
		return substituteBody(s, subst)
	case *parser.ReturnStatement:
		return &parser.ReturnStatement{
			Token:       s.Token,
			ReturnValue: substituteExpr(s.ReturnValue, subst),
		}
	case *parser.MultiAssignStatement:
		newMulti := &parser.MultiAssignStatement{
			Token: s.Token,
		}
		for _, tgt := range s.Targets {
			newMulti.Targets = append(newMulti.Targets, substituteExpr(tgt, subst))
		}
		newMulti.Value = substituteExpr(s.Value, subst)
		return newMulti
	default:
		return stmt
	}
}
func substituteExpr(expr parser.Expression, subst map[string]string) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		if val, ok := subst[e.Value]; ok {
			// 整數替換值（如陣列大小泛型 [n]t）：轉為 IntegerLiteral
			if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
				return &parser.IntegerLiteral{
					Token: e.Token,
					Value: intVal,
				}
			}
			// 型別參數替換（值為簡單型別名稱如 "i64"、"str"、"bool"）：
			// 在表達式語境中跳過。型別參數（如 `v`、`k`）在方法體內作為
			// 變數名使用，不應被替換為型別名稱（否則 `v = .vals[i]` 會變成
			// `i64 = .vals[i]`，產生 `%i64` 未定義錯誤）。型別參數僅在型別
			// 語境中替換（由 substituteType 處理）。
			// 結構/方法名稱替換（值含連字號或點）仍正常套用。
			if !strings.Contains(val, "-") && !strings.Contains(val, ".") {
				return e
			}
			// 非整數替換值（如方法名重寫 hashmap-*-tmpl.hash → hashmap-K-V.hash）：保留為 Identifier
			return &parser.Identifier{
				Token: e.Token,
				Value: val,
			}
		}
		return e
	case *parser.IntegerLiteral:
		return e
	case *parser.InfixExpression:
		return &parser.InfixExpression{
			Token:    e.Token,
			Left:     substituteExpr(e.Left, subst),
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.PrefixExpression:
		return &parser.PrefixExpression{
			Token:    e.Token,
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.CallExpression:
		newCe := &parser.CallExpression{
			Token:     e.Token,
			Function:  substituteExpr(e.Function, subst),
			Arguments: make([]parser.Expression, len(e.Arguments)),
		}
		for i, arg := range e.Arguments {
			newCe.Arguments[i] = substituteExpr(arg, subst)
		}
		return newCe
	case *parser.IndexExpression:
		return &parser.IndexExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Index: substituteExpr(e.Index, subst),
		}
	case *parser.GroupedExpression:
		return &parser.GroupedExpression{
			Token:      e.Token,
			Expression: substituteExpr(e.Expression, subst),
		}
	case *parser.IfExpression:
		var valuePatterns []parser.Expression
		if len(e.ValuePatterns) > 0 && len(subst) > 0 {
			valuePatterns = make([]parser.Expression, len(e.ValuePatterns))
			for i, vp := range e.ValuePatterns {
				valuePatterns[i] = substituteExpr(vp, subst)
			}
		} else {
			valuePatterns = e.ValuePatterns
		}
		// 表層往返標誌（RTBareMatch 等）已遷至語義副表；泛型實例化克隆體
		// 只進編譯管線、不進 formatter，無需複製這些標誌。
		newIf := &parser.IfExpression{
			Token:           e.Token,
			Condition:       substituteExpr(e.Condition, subst),
			MatchedExpr:     substituteExpr(e.MatchedExpr, subst),
			RangePattern:    e.RangePattern,
			EqualityPattern: substituteExpr(e.EqualityPattern, subst),
			OptionPatterns:  e.OptionPatterns,
			ValuePatterns:   valuePatterns,
			RawCond:         substituteExpr(e.RawCond, subst),
		}
		if e.Consequence != nil {
			newIf.Consequence = substituteBody(e.Consequence, subst)
		}
		if e.Alternative != nil {
			newIf.Alternative = substituteBody(e.Alternative, subst)
		}
		return newIf
	case *parser.AssignExpression:
		return &parser.AssignExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Value: substituteExpr(e.Value, subst),
		}
	case *parser.DotExpression:
		return &parser.DotExpression{
			Token:    e.Token,
			Receiver: substituteExpr(e.Receiver, subst),
			Property: e.Property,
		}
	case *parser.ConditionalExpression:
		return &parser.ConditionalExpression{
			Token:       e.Token,
			Condition:   substituteExpr(e.Condition, subst),
			Consequence: substituteExpr(e.Consequence, subst),
			Alternative: substituteExpr(e.Alternative, subst),
		}
	case *parser.SliceExpression:
		return &parser.SliceExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Range: substituteRange(e.Range, subst),
		}
	default:
		return e
	}
}
func substituteRange(r *parser.RangeExpression, subst map[string]string) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	return &parser.RangeExpression{
		Token:    r.Token,
		LeftInc:  r.LeftInc,
		RightInc: r.RightInc,
		Start:    substituteExpr(r.Start, subst),
		End:      substituteExpr(r.End, subst),
	}
}
// substituteType 替換類型中的泛型參數
// 遞迴處理所有 Type 節點
func substituteType(t parser.Type, subst map[string]string) parser.Type {
	if len(subst) == 0 || t == nil {
		return t
	}
	switch typ := t.(type) {
	case *parser.NamedType:
		if val, ok := subst[typ.Value]; ok {
			return &parser.NamedType{Token: typ.Token, Value: val, IsInferred: typ.IsInferred}
		}
		return typ
	case *parser.ArrayType:
		newSize := typ.Size
		if ident, ok := typ.Size.(*parser.Identifier); ok {
			if val, ok := subst[ident.Value]; ok {
				if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
					newSize = &parser.IntegerLiteral{Token: ident.Token, Value: intVal}
				}
			}
		}
		newElem := substituteType(typ.Elem, subst)
		return &parser.ArrayType{Token: typ.Token, Size: newSize, Elem: newElem, IsInferred: typ.IsInferred}
	case *parser.SliceType:
		newElem := substituteType(typ.Elem, subst)
		return &parser.SliceType{Token: typ.Token, Elem: newElem, IsInferred: typ.IsInferred}
	case *parser.NullableType:
		newInner := substituteType(typ.Type, subst)
		return &parser.NullableType{Token: typ.Token, Type: newInner, IsInferred: typ.IsInferred}
	case *parser.PointerType:
		newInner := substituteType(typ.Type, subst)
		return &parser.PointerType{Token: typ.Token, Type: newInner}
	case *parser.FunctionType:
		// Function types are not subject to generic substitution in Phase 1.
		return t
	default:
		return t
	}
}
// collectVarTypesFromBody recursively collects variable types from a function body
func collectVarTypesFromBody(body *parser.BlockStatement, varTypes map[string]string) {
	if body == nil {
		return
	}
	for _, stmt := range body.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
				} else {
					// Can't infer type (e.g., method call result) — delete any
					// stale entry inherited from another scope (e.g., a global
					// variable with the same name) to prevent wrong method resolution.
					delete(varTypes, ls.Name.Value)
				}
			}
		}
		if bs, ok := stmt.(*parser.BlockStatement); ok {
			collectVarTypesFromBody(bs, varTypes)
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.Body != nil {
				collectVarTypesFromBody(fs.Body, varTypes)
			}
		}
		// ExpressionStatement may wrap an IfExpression (e.g. `cond -> { body }`
		// match arms desugared to if/else). Recurse into its consequence and
		// alternative blocks so that local LetStatements inside match arms
		// shadow globals / outer-locals correctly during method resolution.
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			collectVarTypesFromExpr(es.Expression, varTypes)
		}
	}
}
// collectVarTypesFromExpr recurses into expression nodes that contain
// BlockStatement bodies (IfExpression, etc.) and collects local variable types.
func collectVarTypesFromExpr(expr parser.Expression, varTypes map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		collectVarTypesFromBody(e.Consequence, varTypes)
		collectVarTypesFromBody(e.Alternative, varTypes)
		collectVarTypesFromBody(e.DotValBody, varTypes)
	case *parser.GroupedExpression:
		collectVarTypesFromExpr(e.Expression, varTypes)
	case *parser.ConditionalExpression:
		collectVarTypesFromExpr(e.Consequence, varTypes)
		collectVarTypesFromExpr(e.Alternative, varTypes)
	}
}
// makeIdent 建立 Identifier AST 節點
func makeIdent(name string) *parser.Identifier {
	return &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: name},
		Value: name,
	}
}
// makeMethodCall 建立 varName.methodName() 的 ExpressionStatement
func makeMethodCall(varName, method string) *parser.ExpressionStatement {
	return &parser.ExpressionStatement{
		Token: lexer.Token{Type: lexer.IDENT, Literal: varName},
		Expression: &parser.CallExpression{
			Token: lexer.Token{Type: lexer.LPAREN, Literal: "("},
			Function: &parser.DotExpression{
				Token:    lexer.Token{Type: lexer.DOT, Literal: "."},
				Receiver: makeIdent(varName),
				Property: method,
			},
			Arguments: []parser.Expression{},
		},
	}
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
// findTypeForVar 從區塊語句中查找變數的類型（簡化版）
func findTypeForVar(varName string, block *parser.BlockStatement, lifecycleTypes map[string]bool) string {
	for _, stmt := range block.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name.Value == varName {
			if ls.Type != nil {
				return ls.Type.String()
			}
		}
	}
	// 默認返回空
	for t := range lifecycleTypes {
		return t
	}
	return ""
}
// buildArraySizeMap 構建變數名 → 陣列大小的映射
// 從所有 LetStatement 中收集 ArraySize
// sizeMaps 儲存陣列、切片、字串的大小映射，由 buildSizeMaps 單次遍歷填充。
// 合併三個原本各自獨立遍歷 AST 的函數（buildArraySizeMap/buildSliceSizeMap/buildStringSizeMap），
// 將遍歷次數從 3 次降至 1 次，顯著減少大型程序的校驗開銷。
type sizeMaps struct {
	arraySizes    map[string]int64
	sliceSizes    map[string]int64
	stringSizes   map[string]int64
	funcStrReturns map[string]bool // user-defined functions returning str
}

// validateFuncStrReturns is a package-level variable that holds the
// funcStrReturns map from buildSizeMaps. It is set before validateArrayBounds
// runs so that collectStringSizeMapFromStmt (which is called from the
// FunctionDefinition case of validateStmtArrayBounds with a fresh per-function
// stringSizes map) can access funcStrReturns without a signature change.
var validateFuncStrReturns map[string]bool
// buildVarTypes 從程式的頂層陳述句中收集變數型別映射。
// 用於在模組合併後重新建構 varTypes，使 validateArrayBounds 等檢查能應用於 merged 程式。
func buildVarTypes(program *parser.Program) map[string]string {
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
				}
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for _, p := range fd.Parameters {
				if p.Type != nil {
					varTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fd.Results {
				if r.Type != nil {
					varTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fd.Body, varTypes)
		}
	}
	return varTypes
}

// buildSizeMaps 單次遍歷 AST 同時收集陣列、切片、字串的大小映射。
// 替代原本獨立呼叫 buildArraySizeMap + buildSliceSizeMap + buildStringSizeMap 的 3 次遍歷。
func buildSizeMaps(program *parser.Program) *sizeMaps {
	sm := &sizeMaps{
		arraySizes:    make(map[string]int64),
		sliceSizes:    make(map[string]int64),
		stringSizes:   make(map[string]int64),
		funcStrReturns: make(map[string]bool),
	}
	// Pre-scan: collect names of user-defined functions whose single result
	// type is str. This allows isStringExprForCollect to recognize calls to
	// str-returning functions (e.g., s = get-str() where get-str returns str)
	// so that the variable is tracked in stringSizes. Without this, s - 'x'
	// is misidentified as byte arithmetic instead of string concatenation.
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if len(s.Results) == 1 && s.Results[0].Type != nil && s.Results[0].Type.String() == "str" {
				sm.funcStrReturns[s.Name] = true
			}
		case *parser.LetStatement:
			// Method definitions: str.my-method = (self str) (out str) { ... }
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok {
				if s.Name != nil && len(fl.Results) == 1 && fl.Results[0].Type != nil && fl.Results[0].Type.String() == "str" {
					sm.funcStrReturns[s.Name.Value] = true
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		collectSizesFromStmt(stmt, sm)
	}
	return sm
}
// collectSizesFromStmt 遞迴遍歷 AST 節點，同時收集三種大小映射。
// 合併自 collectArraySizesFromStmt / collectSliceSizeMapFromStmt / collectStringSizeMapFromStmt。
func collectSizesFromStmt(stmt parser.Statement, sm *sizeMaps) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// 陣列大小收集
		if at, ok := s.Type.(*parser.ArrayType); ok {
			var arraySize int64
			if at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					arraySize = intLit.Value
				}
			} else if arrLit, ok := s.Value.(*parser.ArrayLiteral); ok {
				if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
					arraySize = intLit.Value
				}
			}
			if arraySize > 0 {
				sm.arraySizes[s.Name.Value] = arraySize
			}
		}
		// 切片大小收集
		if _, ok := s.Type.(*parser.SliceType); ok {
			if sl, ok := s.Value.(*parser.SliceLiteral); ok {
				sm.sliceSizes[s.Name.Value] = int64(len(sl.Elements))
			} else {
				sm.sliceSizes[s.Name.Value] = 0 // unknown size
			}
		} else if sl, ok := s.Value.(*parser.SliceLiteral); ok {
			// Also detect slice from SliceLiteral value (inferred type, no [] annotation)
			sm.sliceSizes[s.Name.Value] = int64(len(sl.Elements))
		}
		// 字串大小收集
		if s.Type != nil && (s.Type.String() == "str") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				sm.stringSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				sm.stringSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
} else if isStringExprForCollect(s.Value, sm.stringSizes, sm.funcStrReturns) {
		// Also detect string from inferred expression (StringLiteral, string concatenation,
		// string method calls like slice/repeat, char-to-str, copy from known string var,
		// or calls to user-defined functions returning str)
		if sl, ok := s.Value.(*parser.StringLiteral); ok {
			sm.stringSizes[s.Name.Value] = int64(len(sl.Value))
		} else {
			sm.stringSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
		}
	}
	case *parser.FunctionDefinition:
		// 字串參數與返回值收集（僅 buildStringSizeMap 有此邏輯）
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				sm.stringSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				sm.stringSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSizesFromStmt(ss, sm)
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectSizesFromStmt(ss, sm)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectSizesFromStmt(ss, sm)
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectSizesFromStmt(s.Init, sm)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSizesFromStmt(ss, sm)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectSizesFromStmt(ss, sm)
		}
	}
}
func buildArraySizeMap(program *parser.Program) map[string]int64 {
	sizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectArraySizesFromStmt(stmt, sizes)
	}
	return sizes
}
func collectArraySizesFromStmt(stmt parser.Statement, sizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if at, ok := s.Type.(*parser.ArrayType); ok {
			var arraySize int64
			if at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					arraySize = intLit.Value
				}
			} else if arrLit, ok := s.Value.(*parser.ArrayLiteral); ok {
				if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
					arraySize = intLit.Value
				}
			}
			if arraySize > 0 {
				sizes[s.Name.Value] = arraySize
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectArraySizesFromStmt(s.Init, sizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectArraySizesFromStmt(ss, sizes)
		}
	}
}
// buildSliceSizeMap collects names of slice variables and their initial element count
func buildSliceSizeMap(program *parser.Program) map[string]int64 {
	slices := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectSliceSizeMapFromStmt(stmt, slices)
	}
	return slices
}
func collectSliceSizeMapFromStmt(stmt parser.Statement, slices map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if _, ok := s.Type.(*parser.SliceType); ok {
			if sl, ok := s.Value.(*parser.SliceLiteral); ok {
				slices[s.Name.Value] = int64(len(sl.Elements))
			} else {
				slices[s.Name.Value] = 0 // unknown size
			}
		} else if sl, ok := s.Value.(*parser.SliceLiteral); ok {
			// Also detect slice from SliceLiteral value (inferred type, no [] annotation)
			slices[s.Name.Value] = int64(len(sl.Elements))
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectSliceSizeMapFromStmt(s.Init, slices)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectSliceSizeMapFromStmt(ss, slices)
		}
	}
}
// buildStringSizeMap collects names of string variables and their literal length
func buildStringSizeMap(program *parser.Program) map[string]int64 {
	strSizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectStringSizeMapFromStmt(stmt, strSizes)
	}
	return strSizes
}
func collectStringSizeMapFromStmt(stmt parser.Statement, strSizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Type != nil && (s.Type.String() == "str") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
} else if isStringExprForCollect(s.Value, strSizes, validateFuncStrReturns) {
	// Also detect string from inferred expression (StringLiteral, string concatenation,
	// string method calls like slice/repeat, char-to-str, copy from known string var,
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		}
	case *parser.FunctionDefinition:
		// Add str parameters and results to stringSizes so they're recognized as strings
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectStringSizeMapFromStmt(s.Init, strSizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectStringSizeMapFromStmt(ss, strSizes)
		}
	}
}

// isSliceTypeOrOptionSlice reports whether a type is a slice ([]T) or an
// option wrapping a slice (?[]T). Used to track slice-typed parameters and
// results so that .len = assignments are rejected.
func isSliceTypeOrOptionSlice(t parser.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*parser.SliceType); ok {
		return true
	}
	if nt, ok := t.(*parser.NullableType); ok {
		if _, ok := nt.Type.(*parser.SliceType); ok {
			return true
		}
	}
	return false
}

// isStringExprForCollect is a stricter version of isStringExpr used during the
// string size map collection phase. Unlike isStringExpr (which defers unknown
// types to LLVM), this function only returns true for expressions that are
// DEFINITELY strings, avoiding false positives like struct field access (DotExpression)
// or array element access (IndexExpression) which may be non-string types.
func isStringExprForCollect(expr parser.Expression, strSizes map[string]int64, funcStrReturns map[string]bool) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.Identifier:
		_, exists := strSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExprForCollect(e.Expression, strSizes, funcStrReturns)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExprForCollect(e.Left, strSizes, funcStrReturns) && isStringExprForCollect(e.Right, strSizes, funcStrReturns)
		}
	case *parser.CallExpression:
		// Check if it's a method call on a known string receiver (e.g., s.slice(), s.repeat())
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				if _, exists := strSizes[ident.Value]; exists {
					return true // method call on known string variable
				}
				// Module-prefixed call to a user-defined function returning str
				// (e.g., my-mod.get-str() where get-str returns str)
				fullName := ident.Value + "." + dot.Property
				if funcStrReturns[fullName] || funcStrReturns[dot.Property] {
					return true
				}
			}
			// 'literal'.method() — method call on string literal
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				return true
			}
		}
		// Check if it's a global builtin function call that returns str (e.g., char-to-str)
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return true
			}
			// Check if it's a user-defined function that returns str
			if funcStrReturns[ident.Value] {
				return true
			}
		}
	}
	return false
}
// isStringExpr checks if an expression is a string type
func isStringExpr(expr parser.Expression, stringSizes map[string]int64) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.NilLiteral:
		// nil can be assigned to ?str (option string) variables
		return true
	case *parser.Identifier:
		_, exists := stringSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExpr(e.Expression, stringSizes)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExpr(e.Left, stringSizes) && isStringExpr(e.Right, stringSizes)
		}
	case *parser.CallExpression:
		// Function/method calls (e.g., i64-to-str, s.to-upper) may return strings.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.DotExpression:
		// Struct field access (e.g., fp.path) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.IndexExpression:
		// Array element access (e.g., req.headers[i], arr[i]) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	}
	return false
}
// validateDuplicates checks for duplicate variable declarations
func validateDuplicates(program *parser.Program) error {
	seen := make(map[string]bool)
	for _, stmt := range program.Statements {
		if err := validateStmtDuplicates(program.Sem, stmt, seen); err != nil {
			return err
		}
	}
	return nil
}
func validateStmtDuplicates(sem *parser.SemanticContext, stmt parser.Statement, seen map[string]bool) error {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// In nolang, first assignment is definition, subsequent are reassignments
		// Check for duplicates when:
		//   1. There's an explicit type annotation in source (e.g., `i i64 = 0`), OR
		//   2. The name follows uppercase constant convention (e.g., `A = 0`, `SBOX = 1`)
		// Parser-inferred types (Type.Token position == Name.Token position) are treated as
		// "no explicit annotation" — but uppercase constants still trigger duplicate detection.
		hasExplicitType := s.Type != nil
		if hasExplicitType {
			if nt, ok := s.Type.(*parser.NamedType); ok {
				if nt.Token.Line == s.Name.Token.Line && nt.Token.Column == s.Name.Token.Column {
					// Parser-inferred type, not explicit annotation
					hasExplicitType = false
				}
			}
			// SliceType/ArrayType：parser 自動推導時 Token 與 nameToken 相同（同行同列），
			// 用戶顯式標註時 Token 是 '[' 位置。比較 Token 來區分推導 vs 顯式標註。
			// 這允許 slice/array 重新賦值（如 `local = [1, 2, 3]` 在已宣告後）。
			if st, ok := s.Type.(*parser.SliceType); ok {
				if st.Token.Line == s.Name.Token.Line && st.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if at, ok := s.Type.(*parser.ArrayType); ok {
				if at.Token.Line == s.Name.Token.Line && at.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if s.Type.String() == s.Name.Value {
				// Parser artifact: Type.String() == Name.Value
				hasExplicitType = false
			}
		}
		isConst := checker.IsConstantName(s.Name.Value)
		if !hasExplicitType && !isConst {
			return nil
		}
		// 使用複合 key：name + "\x00" + platformKey（無平台註解則 suffix 為空）
		// 同名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
		sPlatformKeys := sem.PlatformKeysOf(s)
		var dupKeys []string
		if len(sPlatformKeys) == 0 {
			dupKeys = []string{s.Name.Value + "\x00"}
		} else {
			dupKeys = make([]string, 0, len(sPlatformKeys))
			for _, pk := range sPlatformKeys {
				dupKeys = append(dupKeys, s.Name.Value+"\x00"+pk)
			}
		}
		for _, k := range dupKeys {
			if seen[k] {
				return fmt.Errorf("duplicate variable '%s'", s.Name.Value)
			}
		}
		for _, k := range dupKeys {
			seen[k] = true
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			bodySeen := make(map[string]bool)
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(sem, bStmt, bodySeen); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			if err := validateStmtDuplicates(sem, bStmt, seen); err != nil {
				return err
			}
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(sem, bStmt, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
// validateLoopScopedVars detects variables that are first declared inside a
// ForStatement body and then used after the loop exits. Because the loop might
// execute zero iterations, such variables would be undef at the point of use,
// leading to undefined behavior (e.g. infinite loops after LLVM optimization).
func validateLoopScopedVars(program *parser.Program) error {
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Body != nil {
			// Pre-populate definedBeforeLoop with all function parameters and
			// result parameters, since they are always initialized by the caller
			// or at function entry.
			preDefined := make(map[string]bool)
			for _, p := range fd.Parameters {
				preDefined[p.Name] = true
			}
			for _, r := range fd.Results {
				if r.Name != "" {
					preDefined[r.Name] = true
				}
			}
			if err := validateLoopScopedStmts(fd.Body.Statements, preDefined); err != nil {
				return err
			}
		}
	}
	return nil
}
// validateLoopScopedStmts walks a statement list sequentially, tracking which
// variables are first declared inside a ForStatement body. After a ForStatement,
// it checks ALL subsequent statements for uses of those loop-only variables.
func validateLoopScopedStmts(stmts []parser.Statement, preDefined map[string]bool) error {
	// definedBeforeLoop: variables defined at top level before any loop
	definedBeforeLoop := make(map[string]bool)
	// Pre-populate with function parameters and results
	for k, v := range preDefined {
		definedBeforeLoop[k] = v
	}
	// loopOnlyVars: variables first declared inside a preceding loop body.
	// These might be undef if the loop executed zero iterations.
	loopOnlyVars := make(map[string]bool)
	for _, stmt := range stmts {
		// Check if this statement reads a loop-only variable in an expression
		usedNames := collectExprIdentifiers(stmt)
		for name := range usedNames {
			if loopOnlyVars[name] && !definedBeforeLoop[name] {
				return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when used here (loop might execute zero iterations)",
					stmtPosLine(stmt), name)
			}
		}
		// Check if a ForStatement's loop variable reuses a loop-only variable name.
		// This is the exact pattern that caused the md5 hang: a variable declared
		// inside a loop body (c u32 = h2) was reused as a range-for loop variable
		// (c <- (dremain..56)), causing LLVM to generate undef PHI nodes.
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				v := fs.IterRange.Variable
				if loopOnlyVars[v] && !definedBeforeLoop[v] {
					return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when reused as loop variable here (loop might execute zero iterations)",
						fs.Token.Line, v)
				}
			}
		}
		// Track top-level variable declarations
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedBeforeLoop[ls.Name.Value] = true
			// Once re-declared at top level, it's no longer "loop-only"
			delete(loopOnlyVars, ls.Name.Value)
		}
		// ForStatement: loop variable is always initialized
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				definedBeforeLoop[fs.IterRange.Variable] = true
				delete(loopOnlyVars, fs.IterRange.Variable)
			}
			// Collect variables declared inside the loop body
			bodyVars := collectLoopBodyVarDecls(fs)
			for v := range bodyVars {
				if !definedBeforeLoop[v] {
					loopOnlyVars[v] = true
				}
			}
			// Recursively validate nested statements inside the loop body.
			// Pass current definedBeforeLoop as preDefined so that variables
			// defined before this loop are still recognized inside the body.
			if fs.Body != nil {
				if err := validateLoopScopedStmts(fs.Body.Statements, definedBeforeLoop); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func collectLoopBodyVarDecls(fs *parser.ForStatement) map[string]bool {
	vars := make(map[string]bool)
	if fs.Body == nil {
		return vars
	}
	collectVarDeclsFromStmts(fs.Body.Statements, vars)
	if fs.IterRange != nil && fs.IterRange.Variable != "" {
		delete(vars, fs.IterRange.Variable)
	}
	return vars
}
func collectVarDeclsFromStmts(stmts []parser.Statement, vars map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil {
				vars[s.Name.Value] = true
			}
		case *parser.BlockStatement:
			collectVarDeclsFromStmts(s.Statements, vars)
		case *parser.ForStatement:
			if s.Body != nil {
				collectVarDeclsFromStmts(s.Body.Statements, vars)
			}
		}
	}
}
func collectExprIdentifiers(stmt parser.Statement) map[string]bool {
	idents := make(map[string]bool)
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectIdentsFromExpr(s.Expression, idents)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			collectIdentsFromExpr(s.ReturnValue, idents)
		}
	case *parser.ForStatement:
		if s.IterRange != nil {
			if s.IterRange.Range != nil {
				if s.IterRange.Range.Start != nil {
					collectIdentsFromExpr(s.IterRange.Range.Start, idents)
				}
				if s.IterRange.Range.End != nil {
					collectIdentsFromExpr(s.IterRange.Range.End, idents)
				}
			}
			if s.IterRange.RangeExpr != nil {
				collectIdentsFromExpr(s.IterRange.RangeExpr, idents)
			}
		}
		if s.Condition != nil {
			collectIdentsFromExpr(s.Condition, idents)
		}
		if s.CountExpr != nil {
			collectIdentsFromExpr(s.CountExpr, idents)
		}
	case *parser.MultiAssignStatement:
		for _, target := range s.Targets {
			collectIdentsFromExpr(target, idents)
		}
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	}
	return idents
}
func collectIdentsFromExpr(expr parser.Expression, idents map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		idents[e.Value] = true
	case *parser.PrefixExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.InfixExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Right, idents)
	case *parser.CallExpression:
		collectIdentsFromExpr(e.Function, idents)
		for _, arg := range e.Arguments {
			collectIdentsFromExpr(arg, idents)
		}
		for _, ga := range e.GenericArgs {
			collectIdentsFromExpr(ga, idents)
		}
	case *parser.DotExpression:
		collectIdentsFromExpr(e.Receiver, idents)
	case *parser.IndexExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Index, idents)
	case *parser.SliceExpression:
		collectIdentsFromExpr(e.Left, idents)
		if e.Range != nil {
			collectIdentsFromExpr(e.Range.Start, idents)
			collectIdentsFromExpr(e.Range.End, idents)
		}
	case *parser.AssignExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Value, idents)
	case *parser.ConditionalExpression:
		collectIdentsFromExpr(e.Condition, idents)
		collectIdentsFromExpr(e.Consequence, idents)
		collectIdentsFromExpr(e.Alternative, idents)
	case *parser.IfExpression:
		collectIdentsFromExpr(e.Condition, idents)
	case *parser.CastExpression:
		collectIdentsFromExpr(e.Expr, idents)
	case *parser.GroupedExpression:
		collectIdentsFromExpr(e.Expression, idents)
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.MapLiteral:
		for _, pair := range e.Pairs {
			collectIdentsFromExpr(pair.Key, idents)
			collectIdentsFromExpr(pair.Value, idents)
		}
	case *parser.StructLiteral:
		for _, field := range e.Fields {
			collectIdentsFromExpr(field.Value, idents)
		}
	case *parser.RunExpression:
		collectIdentsFromExpr(e.Call, idents)
	case *parser.AwaitExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.RangeExpression:
		collectIdentsFromExpr(e.Start, idents)
		collectIdentsFromExpr(e.End, idents)
	}
}
func stmtPosLine(stmt parser.Statement) int {
	if stmt == nil {
		return 0
	}
	return stmt.Pos().Line
}
// validateArrayBounds 編譯期陣列邊界檢查
// 檢查所有 IndexExpression 中的常數索引是否超出陣列長度
func validateArrayBounds(program *parser.Program, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	for _, stmt := range program.Statements {
		if err := validateStmtArrayBounds(stmt, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
	}
	return nil
}
func validateStmtArrayBounds(stmt parser.Statement, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return validateExprArrayBounds(s.Expression, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.LetStatement:
		if s.Value != nil {
			// Skip validation for synthetic `it` bindings injected by match desugar.
			// These have sentinel types ("err", "nil") or element types ("str", "i64")
			// but their value is an option variable, not a direct string/integer.
			if s.IsSynthetic {
				return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes)
			}
			// Skip string type check for array/slice variables
			isArrayVar := false
			if at, ok := s.Type.(*parser.ArrayType); ok {
				isArrayVar = at != nil
			}
			if !isArrayVar {
				// Only check explicitly str-typed variables (not inferred ones).
				// Inferred string variables (from StringLiteral, etc.) may later be
				// assigned from struct field access or cross-module calls whose
				// return type is unknown at vet time; deferring to LLVM is safer.
				isExplicitStr := s.Type != nil && (s.Type.String() == "str")
				if isExplicitStr {
					if !isStringExpr(s.Value, stringSizes) {
						return fmt.Errorf("cannot assign non-string value to string variable '%s'", s.Name.Value)
					}
				}
			}
			return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes)
		}
	case *parser.FunctionDefinition:
		// Build a fresh per-function stringSizes map to avoid variable name
		// collisions between functions (e.g., 'pos' may be str in one function
		// and i64 in another). Only include this function's parameters, results,
		// and local variables — not global constants or other functions' variables.
		funcStringSizes := make(map[string]int64)
		funcSliceSizes := make(map[string]int64)
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				funcStringSizes[p.Name] = 0
			}
			// Track slice-typed parameters ([]T and ?[]T) so that .len =
			// assignments are rejected. Skip 'self' to allow vec.truncate/extend
			// and other low-level slice methods to modify .len internally.
			if p.Name != "self" && isSliceTypeOrOptionSlice(p.Type) {
				funcSliceSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				funcStringSizes[p.Name] = 0
			}
			if isSliceTypeOrOptionSlice(p.Type) {
				funcSliceSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, funcStringSizes)
				collectSliceSizeMapFromStmt(ss, funcSliceSizes)
			}
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, funcSliceSizes, funcStringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			if err := validateStmtArrayBounds(s.Init, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return validateExprArrayBounds(s.ReturnValue, arraySizes, sliceSizes, stringSizes, varTypes)
		}
	}
	return nil
}
// tryEvalConstInt 嘗試在編譯期求值整數常數表達式。
// 支援：IntegerLiteral、PrefixExpression(-)、InfixExpression(+,-,*)、GroupedExpression。
// 若成功求值回傳 (value, true)，否則回傳 (0, false)。
func tryEvalConstInt(expr parser.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return e.Value, true
	case *parser.PrefixExpression:
		if e.Operator == "-" {
			if v, ok := tryEvalConstInt(e.Right); ok {
				return -v, true
			}
		}
		if e.Operator == "+" {
			return tryEvalConstInt(e.Right)
		}
	case *parser.InfixExpression:
		lv, lok := tryEvalConstInt(e.Left)
		rv, rok := tryEvalConstInt(e.Right)
		if lok && rok {
			switch e.Operator {
			case "+":
				return lv + rv, true
			case "-":
				return lv - rv, true
			case "*":
				return lv * rv, true
			}
		}
	case *parser.GroupedExpression:
		return tryEvalConstInt(e.Expression)
	}
	return 0, false
}
// checkConstIndexBounds 檢查常數索引是否越界（負數或 >= size）。
// 若索引非常數或 size <= 0 則跳過。回傳錯誤或 nil。
func checkConstIndexBounds(idxExpr parser.Expression, size int64, varName string, typeName string) error {
	if size <= 0 {
		return nil
	}
	idx, ok := tryEvalConstInt(idxExpr)
	if !ok {
		return nil
	}
	if idx < 0 {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	if idx >= size {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	return nil
}
func validateExprArrayBounds(expr parser.Expression, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	switch e := expr.(type) {
	case *parser.IndexExpression:
		// 檢查索引是否為常數且超出陣列長度
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if size, exists := arraySizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "array"); err != nil {
					return err
				}
			}
			// Also check slice bounds
			if size, exists := sliceSizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "slice"); err != nil {
					return err
				}
			}
			// Also check string index bounds
			if size, exists := stringSizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "string"); err != nil {
					return err
				}
			}
		}
		// 遞迴檢查 Left 和 Index（Index 自身也可能有巢狀索引）
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Index, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.AssignExpression:
		// array.len = val / slice.len = val / string.len = val → 不允許修改唯讀的 len 欄位
		// 使用 str.truncate(n) 或 []t.truncate(n) 替代直接修改 .len
		if dot, ok := e.Left.(*parser.DotExpression); ok {
			if dot.Property == "len" {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if _, exists := arraySizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of array '%s'", ident.Value)
					}
					if _, exists := sliceSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of slice '%s'", ident.Value)
					}
					if _, exists := stringSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of string '%s'", ident.Value)
					}
				}
			}
		}
		// Note: string type check for reassignments is intentionally omitted.
		// Inferred string variables may be reassigned from struct field access or
		// cross-module calls whose return type is unknown at vet time.
		// The LetStatement check for explicitly str-typed variables is sufficient.
		// a[i] = val → 檢查 Left 中的 IndexExpression
		// （slice 的索引檢查已在 IndexExpression case 中處理）
		return validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.InfixExpression:
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.PrefixExpression:
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.CallExpression:
		// array.len() / slice.len() / string.len() → 沒有 len() 方法
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if dot.Property == "len" {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					// self.len() inside method bodies is valid — resolveSelfMethodCalls
					// will rewrite it to Type.len(self), which the codegen handles as
					// a builtin field access. Skip validation for the implicit receiver.
					if ident.Value != "self" {
						if _, exists := arraySizes[ident.Value]; exists {
							return fmt.Errorf("array '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
						}
						if _, exists := sliceSizes[ident.Value]; exists {
							return fmt.Errorf("slice '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
						}
						if _, exists := stringSizes[ident.Value]; exists {
							return fmt.Errorf("string '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
						}
						// For any other typed variable, also reject .len() method
						// Exception: map types (hashmap-K-V or [K]V) have a legitimate len() method
						if typeName, exists := varTypes[ident.Value]; exists {
							if strings.Contains(typeName, "hashmap-") || isMapTypeString(typeName) {
								// map types have a len() method — skip rejection
							} else {
								return fmt.Errorf("%s '%s' has no method 'len', use '%s.len' instead", typeName, ident.Value, ident.Value)
							}
						}
					}
				}
			}
		}
		if e.Function != nil {
			if err := validateExprArrayBounds(e.Function, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		for _, arg := range e.Arguments {
			if err := validateExprArrayBounds(arg, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			if err := validateExprArrayBounds(e.Condition, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
// ── 型別檢查 ──────────────────────────────────────────────
// checker.ValidateResult 型別檢查結果
                            
              
              
              
                 
 
// ValidateEmbedAnnotations 校驗 #{embed=...} 註解的用法是否正確。
// sourcePath 用於解析相對路徑（相對於包根目錄）。
                                                                                            
                             
                                          
                                       
          
           
   
                                               
                      
                                        
                                                       
                            
            
    
                     
                                                                 
                        
                                                                       
                        
    
   
                        
           
   
                       
                        
                                            
                      
                                            
                  
                 
                                                         
     
   
                                                   
                     
                                            
                  
                 
                                                                                                
     
          
                              
                       
                                        
                                                 
                                                  
                                                
                        
      
     
    
                                                 
                                                  
                                                
                        
      
     
    
                    
                                             
                   
                  
                                                                                                      
      
    
   
                                  
                      
                            
                                  
                 
                         
                                                  
     
                      
                                       
     
                                                    
    
                                                   
                                             
                   
                  
                                                                                               
      
    
   
  
               
 
// checker.ValidateTypes 對 Program 進行型別檢查，回傳錯誤列表（包含行號）
                                                              
                             
                               
                                   
                                          
                                                      
                            
   
  
                                                             
                                     
                                          
                                                      
                                                        
                                                    
    
   
                                                   
                                                        
                                                          
    
   
  
                                                                                                    
                                        
                           
                           
                            
                            
                            
                           
                           
                           
                           
                           
  
                                      
                                         
                   
   
  
                                                                         
                                                                 
                                          
                                                      
                          
                                    
                                                    
    
                                                              
                                                                                         
                                                                                        
                                                   
                       
                                
                                    
           
                                                    
                                       
                                             
     
    
                          
                              
                                                
                                  
          
     
    
                              
                                             
                            
                              
                                                                                                                   
      
           
                               
                               
     
    
   
  
                                        
                                                                                                      
                                                      
                                                                                         
                                                                     
                                                                    
                                                                                                        
                                                                                
                                           
                                                
                                                             
                                       
   
  
                                                                               
                                                                  
                                            
                                          
                                                
                                                                                     
                                                              
                                                       
     
    
   
  
                                          
                                  
                
                                                      
                                                                 
                                             
    
   
                                                                          
                                          
                                      
                       
   
                                                                                
                                    
  
               
 
// checker.ValidateUnionTypes 收集 type alias（單型別和 union）並對函數的
// 聯合型別/泛型變體做檢查：
//   - 記錄每個 alias 名稱 -> 解析後的具體型別列表
//   - 對帶 ..T 形式的函數，若 T 是 union alias，記錄到 FunctionDefinition.VariadicUnion
//   - 對每個 type alias，遞迴展開成扁平化的 []Type，供 codegen 使用
//
// 不在這裡阻擋編譯；錯誤一律以 checker.ValidateResult 報告（warning 級別）。
                                                                                                   
                                              
                             
                                   
                                          
                                    
          
           
   
                                            
                                            
                           
                             
                                                             
     
           
   
                       
  
                                                                             
                                                                      
                                          
                                             
                            
           
   
                              
           
   
                                             
                       
           
   
                                                                                           
                                                          
                     
           
   
                                                      
                              
   
  
                                                                                             
                                                                                     
                            
                                          
                                             
                           
           
   
                            
           
   
                                               
                      
                              
   
  
                        
 
// findSingleUnionName 檢查函數的參數與結果型別，找出唯一使用的 union alias。
// 若函數只涉及一個 union alias，則返回該名稱；否則返回空字串。
// 對於 variadic 函數，返回空字串（variadic 由 VariadicUnion 處理）。
                                                                                                      
                                    
                                  
                                                    
                       
                       
   
  
                               
                                                    
                       
                       
   
  
                          
                             
           
   
  
          
 
// collectUnionNamesFromType 從型別中收集所有 union alias 名稱。
                                                                                                     
                             
              
            
  
                        
                        
                                                      
                       
   
                        
                                                    
                      
                
   
                        
                                                    
                      
                
   
                          
                                                    
                      
                
   
                           
                                                    
                      
                
   
                           
                                                        
  
           
 
// FlattenUnion 將一個 union alias（或單型別 alias）扁平化為具體型別列表。
// 對於 union：對每個成員遞迴展開（若成員是另一個 union alias 會被展開）。
// 對於單型別 alias：返回 [Type]（長度 1）。
// 對於已知的 builtin（i8/i16/.../f64/bool/byte/char/str）：原樣返回。
                                                                                    
                                                          
              
                                
                            
               
                                
                                                      
  
                        
         
                                            
                                                      
  
                     
                       
                                    
                                           
                                            
                                                         
           
                        
    
   
            
  
                    
                                                
                                         
   
                               
  
           
 
// isValidVarName 檢查名稱是否只包含小寫字母（a-z）、中連接符（-）和數字，且不能以數字開頭
                                       
                
             
  
                          
             
                           
                              
                
    
   
                                                                    
               
   
  
            
 
// checker.ValidateNaming 檢查所有變數/函數名稱是否符合命名規範（只用小寫和中劃線）
                                                               
                             
                                                                
                                                                    
                                           
                                          
                                               
           
   
                                                              
  
               
 
                                                                                      
                             
                          
                                 
                                                                                              
                       
                                                              
                                   
   
                                   
                                            
                          
                            
                                                                                       
     
   
                    
                                            
                                                                
    
   
                           
                                                                       
                                                                      
                                                
                 
   
                                                     
                                            
                               
                                 
                                                                                             
     
   
                             
                                      
                                                               
   
                                  
                                                            
                                 
                                                                             
    
                                 
                                                                             
    
   
  
               
 
// checker.ValidateAsyncNaming 檢查所有由 'run' 調用的函數名稱是否以 '-async' 結尾
                                                                    
                             
                                                     
               
 
                                                                                  
                             
                           
                                  
                     
                                                      
    
                            
                      
                                         
                                                           
                           
                                                            
      
     
    
                              
                                                
                                   
                           
                                              
                                                              
                                   
                                                                    
      
                                   
                                                                    
      
     
    
                            
                     
                                                      
    
   
  
 
// checkRunAsyncNaming 檢查單個表達式是否為 RunExpression，並驗證其調用的函數名稱
                                                                             
                                            
         
        
  
                                                  
         
        
  
                                      
                  
        
  
                                          
                                             
                               
                                 
                                                                                          
    
  
 
                                                                
                                                         
                    
  
                                                          
                     
  
          
 
// checker.ValidateUnusedVars detects top-level variables that are defined but never used.
                                                                   
                             
                                        
                                                            
                      
                                          
                                                
                                              
                                                             
                                
                                  
     
                                              
    
   
  
                            
            
  
                                      
                                  
                                          
                                                         
  
                                     
                                
                      
                            
                                            
                        
                          
                                          
                                                                   
     
   
  
               
 
// markReferencesInStatement walks a statement tree, finding Identifier references to top-level vars.
                                                                                                                               
                          
                           
                                                    
                     
                                                  
   
                                  
                          
                                                       
   
                                 
                    
                                            
                                                      
    
   
                              
                           
                                                        
   
                             
                                      
                                                     
   
                           
                    
                                                      
   
                         
                                                      
   
                      
                                                        
   
                                                                                     
                         
                                
                                       
                                                                    
     
                                     
                                                                  
     
    
                                    
                                                                 
    
   
                                                 
                         
                                                      
   
                    
                                            
                                                      
    
   
                                   
                                                  
                                                                 
                                    
                                                 
   
                     
                                                  
   
  
 
// markReferencesInExpr walks an expression tree, marking Identifiers found in varSet as used.
                                                                                                                           
                          
                         
                                           
                           
   
                              
                    
                                                 
   
                     
                                                  
   
                               
                     
                                                  
   
                             
                        
                                                     
   
                                   
                                              
   
                            
                        
                                                     
   
                                
                          
                                                       
   
                           
                         
                                                      
   
                           
                                                   
                                                      
    
   
                           
                                                   
                                                      
    
   
                           
                                   
                                               
   
                           
                                   
                                               
   
                              
                    
                                                 
   
                     
                                                  
   
                               
                    
                                                 
   
                     
                                                  
   
                              
                    
                                            
                                                      
    
   
                              
                    
                                                 
   
                     
                            
                                                         
    
                          
                                                       
    
   
                                    
                         
                                                      
   
                           
                                                        
   
                           
                                                        
   
                            
                              
                      
                                                   
    
   
                            
                                                                  
                                                              
                                                                
                                                             
                                                    
                 
         
   
                                
                        
            
    
                                                   
                                   
    
   
  
 
// CollectDefinedVars performs the first pass: collects all top-level defined
// names (LetStatements, FunctionDefinitions, ExternStatements) from the program.
// Shared by checker.ValidateNaming (to skip global variable reassignments) and
// checker.ValidateUndefinedVars (as the base for its more comprehensive collection).
                                                                  
                                     
                                          
                                                                  
                                    
   
                                                      
                              
   
                                                                     
                                    
   
                                             
                              
   
  
                   
 
// checker.ValidateUndefinedVars detects references to variables that are not defined.
                                                                                      
                             
                                                    
                                           
                                                     
                                                                      
                                                           
                                          
                                                      
                            
   
                                                                     
                                  
   
  
                                                                         
                                                                          
                                           
                                
                       
  
                                                            
                                  
                       
                                                                         
                                                           
                                                                           
                                                
                         
                                
   
  
                                                                
                                                     
                                                            
                                          
                                                                       
                       
                                 
           
                                    
    
   
                                                  
           
   
  
                                                                         
                                                                          
                                                      
                   
                                
                                           
                                         
           
            
    
                                                             
                                                              
                          
            
    
                                        
                      
            
    
                                          
                                                                     
                                                           
                                                
                                       
                                     
      
     
                                                      
                                
                              
     
                                                                  
                                      
     
    
   
  
                                                                           
                                          
                                                  
                                
                              
    
   
                                                         
                                   
                              
    
   
  
                                                         
                                          
                                                                                      
  
               
 
// checker.ValidateUninitOutputParams checks that ?T (nullable) output parameters are
// directly assigned in the function body before being read. A ?T output
// parameter that is read (used in an expression) but never assigned via '='
// is flagged as an error — reading an uninitialized nullable value is unsafe
// and almost certainly a bug (case6: ?T 未初始化使用 → 編譯器報錯).
//
// Covered scenarios:
//   - Case 7 (?T 先賦值再用 → 允許): param IS assigned → no error.
//   - Case 8 (?T 空函數體 → 返回 nil): param NOT read → no error.
//   - Case 6 (?T 未初始化使用 → 報錯): param read but NOT assigned → error.
                                                                           
                             
                                          
                                             
                            
           
   
                                      
                             
              
           
           
   
                                    
                                
                    
            
    
                                                  
                                                          
                  
                        
                          
      
    
   
                               
           
   
                                                             
                                   
                                                    
                                                
                               
                                            
                                                                
                                    
                                         
                                             
                     
                    
                                                                                                                                                               
      
    
   
  
               
 
// collectAssignedNames walks statements recursively and collects variable names
// that are directly assigned via '=' (LetStatement.Name or MultiAssignStatement
// Identifier targets). IndexExpression/DotExpression targets are NOT direct
// assignments — they read the base variable to write to a field/element.
                                                                               
                             
                  
           
   
                           
                            
                     
                                 
    
                      
                                                 
    
                                    
                                     
                                                     
                                 
     
                                                                    
    
                      
                                                 
    
                              
                                               
                            
                     
                                                              
    
                     
                                                     
    
                                   
                           
                                                      
    
                               
                            
                                                       
    
   
  
 
// collectAssignedNamesInExpr walks expressions for nested statements (if/else)
// that may contain assignments. Does NOT recurse into nested function literals.
                                                                                   
                 
        
  
                          
                           
                           
                                                           
   
                           
                                                           
   
                                    
                                                             
  
 
// collectReadNames walks statements recursively and collects variable names
// that are read (used in expressions). Direct assignment targets (LetStatement.Name,
// MultiAssignStatement Identifier targets) are NOT reads. However, IndexExpression.Left
// and DotExpression.Receiver ARE reads — out[i]=val reads 'out' to get the data ptr.
                                                                       
                             
                  
           
   
                           
                            
                      
                                         
    
                                    
                                     
                               
                                 
                                                              
                                         
                                          
                               
                                                             
                                             
     
                                                       
    
                      
                                         
    
                              
                                       
                            
                     
                                                      
    
                          
                                             
    
                       
                                                        
    
                          
                                     
                                                        
     
                                 
                                        
                                                           
      
                                      
                                                         
      
     
    
                     
                                             
    
                                   
                           
                                              
    
                               
                            
                                               
    
   
  
 
// collectReadNamesInExpr walks an expression tree and collects all variable names
// that are read. Handles all expression types including DotExpression.Receiver,
// IndexExpression.Left, CallExpression args, etc.
                                                                           
                 
        
  
                          
                         
                      
                             
                                          
                                   
                                    
   
                            
                                           
                                          
                              
                               
                                      
                                       
                              
                                      
                     
                            
                                               
    
                          
                                             
    
   
                              
                    
                                       
   
                     
                                        
   
                               
                     
                                        
   
                                
                          
                                             
   
                           
                         
                                            
   
                           
                                                   
   
                           
                                                   
   
                               
                                                               
                                                    
                                             
   
                                                      
                                         
                                          
   
                     
                                        
   
                                    
                         
                                            
   
                           
                                              
   
                           
                                              
   
                           
                                   
                                     
   
                           
                                   
                                     
   
                            
                              
                      
                                         
    
   
                         
                                
                                         
                                           
   
                            
                    
                                       
   
                              
                     
                                        
   
                             
                    
                                       
   
                                                                                            
                                                                       
  
 
// checker.ValidateInterfaceImplementation matches dotted-name function definitions
// (e.g. `i8.gt = ...`) against generic-receiver interface method
// declarations (e.g. `ord { t.gt(b t) (res bool) }`). Emits a warning
// when an implementing type is missing or its method signature does not
// match the interface constraint.
                                                                                
                             
                          
                 
                 
                                             
                   
                      
  
                                                                   
                                          
                                              
          
           
   
                           
                                
                            
            
    
                                                                        
                                   
                      
                                                   
     
    
                                
                      
                                                     
     
    
                                
   
                           
  
                                          
                                             
                             
           
   
                                                            
          
           
   
                                                             
                                                             
                             
                                                          
                              
   
                           
                                  
                              
                             
             
     
                                         
                                              
                               
                                 
                                                
                                                                                      
                                                             
       
             
     
                                  
                       
              
      
                                 
                                                                      
                               
                                               
                               
                                 
                                               
                                                                               
                                                        
        
      
     
                                           
                                              
                               
                                 
                                                
                                                                                   
                                                               
       
            
                                    
                        
               
       
                                
                                                                        
                              
                                                
                                
                                  
                                                
                                                                             
                                                       
         
       
      
     
    
   
  
               
 
// splitDottedMethodName splits a function name like "i8.gt" or
// "[]ord.ast" or "[?]ord.desc" into (implType, methodName). Returns
// false if the name does not contain a dotted-method form.
                                                                
                                    
             
                      
  
                       
                           
                                        
                      
  
                                  
 
// checker.ValidateUseKeyword warns when "use" keyword is used instead of "#".
                                                                   
                             
                                          
                                                                             
                                            
                           
                             
                                                                                        
     
   
  
               
 
// checker.ValidateUseAlias warns when 'as' keyword is used for import aliasing and suggests direct alias style.
                                                                 
                             
                                          
                                                                                           
                                            
                           
                             
                                                                                                                                        
     
   
  
               
 
// checker.ValidateRedundantTypeAnnotation produces hints when a variable's explicit type
// annotation is redundant — the type can be inferred from the value (e.g. `m i64 = 100`).
                                                                                
                             
                                     
                                              
                                          
                                                      
                                                        
                                                              
    
   
                                                   
                                                        
                                                                    
    
   
  
                                    
                                          
                                                                        
  
               
 
                                                                                                   
                 
            
  
                          
                           
                              
                                                                                  
                                   
                                                                            
                                                           
                                             
                                
                                  
                                                                                                      
      
    
                                                 
                                         
   
                
                                 
                    
                                        
                               
                     
    
                                   
                      
                                         
     
    
                                
                                      
                                         
     
    
                               
                                         
                                                                          
    
                 
   
                             
                              
                                   
                                                                       
   
                
  
           
 
// checker.ValidateDuplicateVars checks for duplicate variable declarations and returns diagnostics.
                                                                      
                             
                                  
                                          
                                                                               
  
               
 
                                                                                                                            
                          
                           
                    
             
   
                                                            
                                                                     
                                      
                                                                                         
                                                                                    
                                        
                            
                              
                                                  
          
                                                        
                                     
                                                                 
    
   
                              
                                                                                             
                                                                                              
                                                                                          
                                         
                                       
                                    
                                     
                              
                            
                              
                                                                                
       
     
    
                                                                    
                                    
                        
    
                        
                                                                        
                                                  
                     
                                    
                                     
                     
          
     
    
                  
                                     
                         
     
    
   
                                 
                    
                                        
                                            
                                                           
                         
                   
     
    
   
                             
                                      
                                                      
                        
                  
    
   
  
           
 
// checker.ValidateDependencyImports checks that URL-style import paths (e.g., github.com/...)
// are declared in package.jsonc dependencies. rootDir is the directory to search upward
// from for the project's package.jsonc.
                                                                                          
                   
            
  
                               
                                              
            
  
                             
                                          
                                       
          
           
   
                 
                                                                   
                                          
                                    
           
   
                                      
                                                           
                                            
                           
                             
                                                                                                     
     
   
  
               
 
// checker.ValidateExportSymbols checks that all export declarations in lib.no reference
// symbols that actually exist in the corresponding module source files.
                                                                                      
                              
                                           
            
  
                                
                             
                                          
                                          
          
           
   
                        
           
   
                                 
               
                 
                                   
                                                                         
                                                              
                         
   
                    
           
   
                          
                                     
                 
                                            
                           
                             
                                                               
     
           
   
                                
                    
                             
                          
           
   
                                               
                
                                              
                               
                                   
                              
                 
     
                                 
                              
                 
     
                               
                              
                 
     
                                     
                              
                 
     
                                    
                              
                 
     
                             
                                                     
                 
     
    
   
             
                                            
                           
                             
                                                                                                          
     
   
  
               
 
// checker.ValidateStringConcat warns when "+" is used with string operands and suggests "-" instead.
                                                                     
                             
                                          
                                                             
  
               
 
                                                                      
                             
                          
                                  
                          
                                                                      
   
                           
                     
                                                                 
   
                                 
                    
                                               
                                                                   
    
   
                             
                                         
                                                                  
   
                              
                           
                                                                       
   
                           
                    
                                                                
   
                         
                                                                     
   
                      
                                                                  
   
                    
                                               
                                                                   
    
   
  
               
 
                                                                       
                             
                          
                              
                        
                                                 
                       
                                                   
                      
                                                           
                      
    
                   
                                             
                           
                             
                                                             
      
    
   
                                 
                                                               
                                                                
                             
                                   
                                                             
   
                            
                                                                   
                               
                                                                
                                
                                                                     
                              
                                                               
                                                                
  
               
 
// checker.ValidateHexCase 檢查十六進位字面量是否使用了大寫字母。
// Nolang 慣例：hex 一律小寫（0xff 而非 0xFF）。
// 格式化工具會自動將大寫轉為小寫，此檢查產生 hint 提醒。
                                                                
                             
                                          
                                                        
  
               
 
                                                                 
                             
                          
                                  
                          
                                                                 
   
                           
                     
                                                            
   
                                 
                    
                                               
                                                              
    
   
                             
                                         
                                                             
   
                              
                           
                                                                  
   
                           
                    
                                                           
   
                         
                                                                
   
                      
                                                             
   
                    
                                               
                                                              
    
   
  
               
 
// hasUpperHex returns true if a hex literal contains uppercase hex digits
// or an uppercase '0X' prefix.
                                       
                                                                 
             
  
                                                                 
                                 
                            
               
    
   
  
                                            
                                 
                            
               
    
   
  
             
 
                                                                  
                             
                          
                             
                                   
                                            
                          
                            
                                                                                                                           
     
   
                          
                                   
                                            
                          
                            
                                                                                                                               
     
   
                              
                                                          
                                                           
                               
                                                           
                                
                                                                
                             
                                   
                                                        
   
                            
                                                              
                              
                                                          
                                                           
                               
                                                           
                                    
                                                               
                                                                 
                                                                 
                           
                                   
                                                         
   
                           
                                   
                                                         
   
                            
                                  
                          
                                                                 
    
   
                         
                                
                                                             
                                                               
   
                             
                                                          
  
               
 
// checker.ValidatePrintFormat 檢查 print/printf/eprint/eprintf/sprintf 呼叫中的具名格式字串。
// 對於第一個參數為 StringLiteral 的呼叫，解析 {name:spec} 欄位並驗證：
//   - 欄位名稱在當前作用域內已定義（否則 "undefined variable '<name>' in format string"）
//   - 規格字串可被 ParseFormatSpec 解析（否則 "invalid format spec"）
//   - 規格類型字元與變數型別相容（整數類型對應 b/c/d/o/x/X；
//     浮點數對應 e/E/f/F/g/G/%；str/bool 對應 s）
                                                                    
                             
                                                                     
                                             
                                                                                                    
                                              
                                          
                                                      
                                                        
                                                              
    
   
                                                   
                                                        
                                                                    
    
   
  
                                                                                             
                                                                                   
                                        
                            
                            
                            
                            
                             
                             
                             
                            
                            
                            
                           
                            
                            
                            
                            
                             
                            
                                 
                            
                            
                            
  
                                      
                                                   
                             
   
  
                                              
                                                                                   
                                    
                                          
                                                                                    
  
               
 
// isPrintFormatCall 判斷呼叫是否為 print/eprint/format/printf/eprintf/sprintf（含 fmt. 前綴）
// printf/eprintf/sprintf 為已廢棄的別名，仍保留以維持向後相容。
                                            
                
                                                                  
                                          
                                             
             
  
             
 
// checkPrintFormatInStmt 走訪敘述並驗證 print 格式字串，同時追蹤變數作用域。
                                                                                                                                            
                 
            
  
                          
                                  
                          
                                                                      
   
                           
                              
                     
                                                                                        
                                                                           
                                                                                                   
                                                                  
                     
                                               
                                             
                                                            
                                                                                       
     
    
                 
   
                                     
                                     
                                           
   
                                   
                     
                               
                                                                                        
                                          
                                     
                                                     
                                                     
                                
      
     
    
                 
   
                                 
                                                                    
                                       
                              
                    
   
                                  
                     
                                        
    
   
                               
                                     
                                        
    
   
                    
                               
                                         
                                                                                      
    
                 
   
                             
                              
                                   
                                                                                   
   
                
                           
                              
                                                            
                                                       
                                                            
                                          
    
   
                                                 
                         
                                                                                            
   
                    
                                                                                       
   
                         
                                                                                            
   
                      
                                                                                         
   
                    
                                         
                                                                                    
    
   
                
                              
                           
                                                                       
   
  
           
 
// checkPrintFormatInExpr 走訪表達式並驗證 print 格式字串。
                                                                                                                                             
                 
            
  
                             
                          
                             
                                                      
                                                       
                                                              
                                                                                    
    
   
                                         
                                   
                                                                                    
   
                              
                    
                                                                                       
   
                     
                                                                                        
   
                               
                     
                                                                                        
   
                                
                          
                                                                                             
   
                           
                         
                                                                                            
   
                           
                                                
                                                                                    
    
   
                           
                                                
                                                                                    
    
   
                              
                    
                                                                                       
   
                     
                                                                                        
   
                               
                     
                                                                                        
   
                                    
                         
                                                                                            
   
                           
                                                                                              
   
                           
                                                                                              
   
                           
                                   
                                                                                     
   
                           
                                   
                                                                                     
   
                            
                              
                      
                                                                                         
    
   
                              
                    
                                         
                                                                                    
    
   
  
               
 
// validatePrintFormatCall 驗證單個 print/printf/eprint/eprintf/sprintf 呼叫的格式字串。
// 只驗證含 '{' 的具名格式字串；C-style printf('...%d...', args) 不含 '{' 時跳過，
// 保留向後相容性。
                                                                                                                                                
                                                     
         
                                                                             
            
  
                                                            
                                                                                        
                                          
            
  
                                                        
                
                           
                              
                                
                                                        
    
  
                             
                               
                       
           
   
                    
                                         
                                                                                            
                                          
               
                                                            
                                   
                                         
    
   
               
                                            
                               
                                 
                                                                                 
     
           
   
                                                                                      
                    
           
   
                                                                                  
                          
           
   
                                                        
                                                                                          
                                            
                               
                                 
                 
     
   
  
               
 
// checkFormatSpecTypeCompat 檢查規格類型字元與變數型別是否相容。
// 回傳非空字串表示錯誤訊息。
                                                                               
                   
                                               
           
  
                  
                                   
                 
                                 
                                                                                                                
   
                                        
                    
                               
                                                                                                              
   
          
                       
                                                                          
                                                                                                              
   
  
          
 
// isIntegerTypeStr 判斷型別字串是否為整數類型
                                      
           
                                                                           
             
  
             
 
// isFloatTypeStr 判斷型別字串是否為浮點數類型
                                    
                                
 
// collectModuleNames returns all known module ShortNames (from #use + auto-imported std modules).
                                                           
                              
                   
                                         
                            
                              
                                        
   
  
                                          
                                                 
                                     
                    
                      
                                
    
   
                                                  
           
   
  
             
 
// checker.ModuleExport holds an exported name and its string value from a module file.
                          
             
             
             
 
// Per-module export cache: parsing a module's .no file to extract its exports
// is expensive, and the std modules are identical across all vet calls in a
// process. Cache the parsed exports keyed by module name.
     
                                
                                                       
 
// checker.GetModuleExports resolves module .no files and extracts their top-level
// LetStatement names with values (for hover) and function names.
// Results are cached per-module-name for the lifetime of the process.
                                                            
                              
                           
                                
                                                        
                             
                                     
                               
          
                                                  
                                 
                              
                                 
                                
   
                            
                    
            
    
                      
                               
   
  
               
 
// parseModuleExports resolves a single module's .no file and extracts its
// top-level exports (constants, functions, externs).
// 只從內嵌 StdFS 讀取,支援單二進制分發。
                                                           
                                         
                                                                                                  
                                              
                                                                 
                                             
    
   
  
           
 
// parseModuleExportsFromSource 從原始碼位元組解析模組導出列表。
// 被 parseModuleExports 調用，支援磁碟和內嵌 StdFS 兩種來源。
                                                                 
                               
                   
                            
                         
            
  
                           
                                          
                                                                  
                                   
                
                      
                              
    
                                                                                          
   
                                                      
                                                                    
   
                                                                     
                                                         
                                             
            
    
                                                                          
   
  
               
 
// moduleExprValue extracts the string representation of a module-level expression value.
                                                     
                 
           
  
                          
                             
                                                                                    
                                                                            
                            
                         
   
                                   
                           
                  
               
   
                                   
                            
                              
                             
              
                
   
                
                         
              
         
           
  
 
// collectModuleExports tries to resolve each module's .no file and extract its
// top-level LetStatement names (constants) and function names.
                                                                                   
                                         
                   
                            
                               
  
             
 
// resolveModulePath tries to locate a .no file for the given module name.
// It consults the checker.KnownStdModules() lookup table, matching by ShortPath
// (which omits the redundant directory name when dir==file), then uses
// FullPath to resolve the actual file.
                                                  
                                            
                                                                          
                                                           
                                                              
                                                                 
                                                                
                                         
                                                                                                  
                                             
                                              
                  
    
   
  
                                                                            
                                        
                                            
                
  
                                    
                        
                              
                                  
  
                               
                                       
           
   
  
          
 
// checker.ResolveStdModulePath is the exported version of resolveModulePath,
// for use by the LSP server to locate std module source files.
                                                     
                                     
 
                                                                                                               
                             
                          
                                  
                          
                                                                                                      
   
                           
                                                                       
                     
                                                                                                 
   
                    
                                   
   
                                   
                                                
                                    
                                                    
                                   
    
   
                     
                                                                                                 
   
                                 
                                                                           
                                                                         
                                                                    
                                    
                                 
                   
   
                                  
                             
                           
   
                                      
                               
                             
   
                               
                    
                              
                            
    
   
                    
                                               
                                                                                          
    
   
                             
                                         
                                                                                           
   
                           
                                    
                                 
                   
   
                                                       
                                         
   
                                                                 
                                                                    
                                                                
                                                           
                                                                                 
                                                                         
                      
                                                                                       
                                                            
                               
     
    
          
                     
                                                                                        
    
                          
                                                                                                    
    
                       
                                                                                          
    
   
                    
                                               
                                                                                          
    
   
                              
                           
                                                                                                       
   
                              
                    
                                   
                                 
   
  
               
 
                                                                                                                                    
                             
                 
            
  
                          
                         
                                                               
                            
                                               
                          
              
    
                                                 
              
    
                                                              
                                                                    
                                                                
                                         
              
    
                                                             
                         
                                             
                           
                             
                                                                                                          
      
           
                                             
                           
                             
                                                          
      
    
   
                             
                                                                
                        
                                                                          
                                                                
                                                                                                    
   
                                   
                                                                                            
   
                            
                                                                            
                                                             
                              
                    
                                                                                                
   
                     
                                                                                                 
   
                               
                     
                                                                                                 
   
                                
                          
                                                                                                      
   
                           
                         
                                                                                                     
   
                           
                                                       
                                                                                             
    
   
                           
                                                       
                                                                                             
    
   
                              
                    
                                                                                                
   
                     
                                                                                                 
   
                              
                    
                                                                                                
   
                     
                            
                                                                                                        
    
                          
                                                                                                      
    
   
                               
                                                              
                     
                                                                                                 
   
                                    
                         
                                                                                                     
   
                           
                                                                                                       
   
                           
                                                                                                       
   
                           
                                   
                                                                                              
   
                           
                                   
                                                                                              
   
                            
                              
                      
                                                                                                  
    
   
                              
                    
                                                
                                                                                             
    
   
  
               
 
// validateStmtTypes 檢查單個語句的型別問題
                                                                                                                                                                     
                             
                          
                                 
                                         
                                       
                          
                                  
                     
                                        
    
   
                                
                               
                     
                                        
    
   
                                         
                            
                                                              
                                                 
   
                    
                                            
                                                                                      
                                      
    
   
                           
                                                                                   
                    
                                                                                 
                                              
                                            
    
        
   
                                      
                              
                                            
                          
                            
                                                                             
     
   
                                        
                                                      
                           
                                                                                 
                                                
                  
                                              
                            
                              
                                                                                          
       
     
                   
                                            
         
    
                                                
                                                              
                                                                    
                                              
                            
                              
                                                                                          
       
     
         
    
                                         
                                            
                          
                            
                                                                                       
     
        
   
                 
                                                                                
                                                                                                   
                                                    
                                            
    
   
                     
                                                                               
                                                        
                                                            
                            
                           
                                               
                             
                               
                                                                                                                                
        
      
     
    
                  
                                                                        
                          
                                                               
                                               
                                                                                                          
                                                 
                                                    
                                                                                     
                                                           
                                                                    
                                                             
                       
                                                    
                         
                                       
                   
                                                          
               
                                                          
        
                                      
                                                                          
                                                            
                                                             
                                                  
                                
                                  
                                                                                                                                                                    
           
         
        
       
      
                                                                      
                                                                                               
                          
                                                         
                                               
                          
       
      
                                                          
                                                              
                                                   
                                                 
                            
         
        
                              
                                                                                    
                             
                                                 
                               
                                 
                                                                                                                                  
          
                           
        
       
      
                                                                                      
                                                               
                                                              
                                   
                                                                                             
                          
                                                           
                                                        
                                                                            
                                                                                    
                                                                                                          
                           
        
       
      
                                                                                                                                
                                                                 
                             
                                               
                            
                              
                                                                                                                                                                 
        
      
            
                                         
                                          
     
    
   
                                  
                                                                               
                                                            
                                                                                 
                         
                                             
                           
                             
                                                                                                                              
      
    
   
                        
                                                            
                                                           
                                                                                   
                                                                                              
                                 
                                                         
                                                                               
                                       
     
    
                                 
                                                         
                                                                               
                                       
     
    
        
   
                                                                
                                                         
                                        
                               
                                              
                                
                                  
                                                                              
       
     
                                          
                        
                                                             
                       
                                                               
                                                
                                                
                                  
                                    
                                                                                           
         
       
      
     
                            
                     
                                                             
                                                                         
                                                                       
                                                                   
                           
                                                                
                                                               
                                                    
                                                  
                             
          
         
                               
                              
                                                  
                                
                                  
                                                                                                                                   
           
                            
         
        
       
                                                                                                     
                                                                  
                                                                                      
                                                        
                                                           
                                                                                       
                         
                                                      
                           
                                         
                     
                                                                 
                 
                                                                 
          
                                        
                                                                            
                                                              
                                                               
                                                    
                                       
                                         
                                                                                                                                                                     
             
           
          
         
               
                                                 
                                    
                                      
                                                                                                                                                        
          
        
       
                        
                                          
                                                                           
                        
                                      
       
      
     
    
   
                           
                    
                                            
                                                                              
                                      
    
   
                             
                                      
                                                                             
                                     
   
                                   
                                                                             
                                                                            
                                                    
                     
                                                       
                           
                                                            
                
                                                                
                         
                                                                        
                          
                                                 
                                                           
                                        
      
     
                     
                                                                                       
                                                                                
                                                                               
                                              
                                                                
                                
      
     
    
                                               
                                     
                                                     
                                                               
                              
                                    
                                                                
                                                     
                                       
                                       
                                                               
                                                 
                               
                                 
                                                                                                                     
          
        
              
                                                    
                              
                                            
        
       
      
     
                                                                   
                                                                                   
    
   
  
               
 
// moduleShortName extracts the last path segment as the module name.
// "std/math" → "math", "fmt" → "fmt", "hash/md5" → "md5"
                                          
                                                   
                     
  
            
 
     
                              
                                    
                                                                    
                                                                      
                                                                      
                            
                                      
                                               
                                                                                           
                                                                                                   
 
// checker.StdModuleInfo holds information about a standard library module.
                           
                                                                       
                                                                          
                                                                                                        
 
// debugCountHashFns is a no-op placeholder retained for call-site compatibility.
// All temporary debugging output has been removed.
                                                              
          
           
 
// checker.knownStdModules returns all embedded standard library modules.
// Uses //go:embed to discover all .no files in src/std/ at compile time.
                                        
                                
                           
                               
                              
                              
                                            
                  
          
    
                              
                                
                  
                  
                                                  
                                            
                                               
                         
                           
                           
                                                            
                                   
       
                                                          
                                                                    
                           
                                                            
                            
                               
                       
                        
        
       
                                          
                            
                           
                            
        
      
     
    
   
                
                             
   
                           
 
// checker.GetStdModules returns checker.StdModuleInfo for all embedded standard library modules.
                                      
                         
 
// checker.JsModuleInfo 描述一個 JS 相容層模組（src/js/ 下的 .no 檔案）。
// 與 checker.StdModuleInfo 結構相同，但用於 js/ 命名空間。
                          
                                                                         
                                                      
                                                                                                           
 
     
                             
                                  
 
// knownJsModules 回傳所有內嵌的 JS 相容層模組。
// 使用 //go:embed js 在編譯時發現 src/js/ 下的所有 .no 檔案。
// 與 checker.knownStdModules 機制平行。
                                      
                               
                          
                               
                              
                              
                                           
                  
          
    
                              
                                
                  
                  
                                                  
                                           
                                               
                         
                           
                           
                                                            
                                   
       
                           
                                                            
                                
                               
                           
                        
        
       
                                         
                            
                           
                            
        
      
     
    
   
               
                            
   
                          
 
// checker.GetJsModules returns checker.JsModuleInfo for all embedded JS compatibility modules.
                                    
                        
 
// checker.CollectStdModuleSignatures parses all std module source files and returns
// function signatures (funcName → return types) and struct field types
// (structName → field name → field type). This is used by the LSP to inject
// extern signatures into the parser so that type inference (e.g. option match
// `it` binding) works correctly for cross-module method calls.
                                                                                       
                        
                                       
                                                    
                                    
                                                                           
                                          
                                                        
                                              
                                                  
                  
            
    
                                 
                     
                           
                           
            
    
                                         
                                                        
                             
                                             
                                    
                                
       
                              
      
     
                                                      
                                      
                                  
                                                             
                               
       
      
                                   
                                                                                
                                                  
                                         
      
     
                                                                              
                                                                                       
                                                                                    
                                                      
                                                 
                                          
       
      
     
    
   
                         
                               
                           
                               
   
                                    
 
// checker.CollectStdConcreteAliases returns the cached single concrete type aliases
// collected from std modules (e.g. "fd" → "i64" from fs.no). Triggers
// checker.CollectStdModuleSignatures via sync.Once to populate the cache.
                                                    
                                                              
                       
 
// checker.CollectStdStructModules returns a map from struct name to the short name
// of the module that defines it (e.g. "conn" → "tls"). Used by
// checker.ValidateCrossModuleTypeRefs to enforce module-prefix on cross-module type
// references. Triggers checker.CollectStdModuleSignatures via sync.Once.
                                                  
                                                              
                         
 
// extractBaseTypeName unwraps NullableType, PointerType, ArrayType, and
// SliceType wrappers to find the innermost NamedType value string.
// Returns "" if the type is nil or not a NamedType (after unwrapping).
                                                
              
           
  
                        
                        
                 
                           
                                     
                          
                                     
                        
                                     
                        
                                     
  
          
 
// isInferredType recursively checks if a type node was inferred by the parser
// (not explicitly written by the user). Used by checker.ValidateCrossModuleTypeRefs to
// skip inferred types — they are auto-derived from function call return types,
// so flagging them for missing module prefix would be a false positive.
                                         
              
              
  
                        
                        
                      
                           
                                                 
                          
                                
                        
                                                 
                        
                                                 
  
             
 
// isBuiltinType returns true for primitive type names that don't require
// a module prefix.
                                      
              
                                
                            
               
                        
             
  
             
 
// checker.ValidateCrossModuleTypeRefs checks that struct field types, variable
// declaration types, and function parameter/result types use the proper
// module prefix when referencing a struct defined in another module.
//
// Per the language spec, cross-module type references must use the
// "module.type" form (e.g. `tls.conn`). Using the bare struct name
// (e.g. `conn`) without the module prefix is an error.
//
// Exceptions:
//   - Builtin primitive types (i64, str, bool, ...)
//   - Types already using a module prefix (contain a dot)
//   - Types defined locally in the current file (structs, type aliases)
//   - Type aliases collected via checker.CollectStdConcreteAliases (e.g. "fd" → "i64")
                                                                            
                             
                                                                        
                                       
                         
                
  
                                                                   
                                    
                                          
                                                    
                             
   
                                             
                             
   
  
                                                                             
                                 
                                             
                      
  
                                                                           
                                  
                                                                  
                                    
                     
         
   
                                                              
                                      
         
   
                                                 
                              
         
   
                                               
                           
         
   
                                                            
                                               
                     
         
   
                                         
                      
                    
               
                 
                    
                   
                    
    
   
                                           
                 
                
                                                                                                   
    
  
                                     
                                          
                           
                                
                               
                      
                                                    
     
    
                            
                                                
                                                   
    
                                  
                                   
                      
                                                    
     
    
                                
                      
                                                    
     
    
                                                                    
                                                                   
                                                                    
                                                          
                     
                                                
                                                                                                     
                                                        
      
     
    
                               
                                   
                      
                                                    
     
    
                                
                      
                                                    
     
    
   
  
               
 
// checker.GetStdModuleShortNames returns the short names of all embedded standard library
// modules (for use in definedVars and module name registration).
                                        
                           
                                    
                             
                           
  
             
 
// GetStdModuleFullPaths returns the full paths of all embedded standard library
// modules (for use in file resolution and auto-loading).
                                       
                           
                                    
                             
                          
  
             
 
// resolveModuleCalls walks the program and rewrites module.fn() calls
// where the DotExpression receiver chain matches an imported module ShortName.
// Supports single-level (base64.encode-std → encode-std) module paths.
// Also rewrites module.CONST constant accesses (e.g. base64.BASE64-STD → BASE64-STD).
                                                                            
                               
        
  
                                
                                    
                  
  
                                                                         
                                                                    
                                                                              
                                   
                                                                              
                                                                          
                                                             
                                      
                                          
                                                      
                       
                             
    
   
                                                                  
                                                                                                      
                                                                                             
                                                                                     
                                                                                                   
                                                                                             
                                     
                                      
    
                                                                         
                                                                      
                                                                          
                                                                        
                                                                         
                                                             
                                                           
                                              
                                    
     
    
   
  
                                          
                                                                 
  
 
// extractModulePathAndFunc walks a DotExpression chain to extract the
// module path (joined with "/") and the final property (function name).
// For example:
//
//	DotExpression{Identifier("math"), "sqrt"}     → ("math", "sqrt")
//	DotExpression{DotExpression{Identifier("hash"), "sha256"}, "sha256"}
//	                                              → ("hash/sha256", "sha256")
//
// Returns ("", "") if the chain contains non-Identifier nodes.
                                                                                
                      
                      
                    
      
                                               
                                                       
                   
                                                       
                                                        
        
          
                
   
  
                                   
                    
 
                                                                                                                                       
                          
                                  
                          
                                                                                         
   
                           
                     
                                                                               
   
                                   
                     
                                                                               
   
                                 
                    
                                               
                                                                       
    
   
                             
                                         
                                                                      
   
                           
                         
                                                                                       
   
                    
                                                                    
   
                      
                                                                      
   
                    
                                               
                                                                       
    
   
  
 
                                                                                                                                                          
                 
            
  
                          
                             
                                                                   
                                                                       
                                                               
                                                               
                                                                                     
   
                                                                 
                                                                             
                                                             
                                                        
                                                   
                                                             
                                      
                                    
                                                            
                   
     
    
   
                           
                                   
                                                                                  
   
          
                            
                                                                                              
                                                                                                                 
                                                                                                  
                                                  
                                                                 
                             
                                                             
                    
    
   
                                                                       
                                                                                    
          
                              
                    
                                                                             
   
                     
                                                                               
   
          
                               
                     
                                                                               
   
          
                                    
                         
                                                                                       
   
                           
                                                                                           
   
                           
                                                                                           
   
          
                           
                         
                                                                                       
   
                           
                                                      
                                                                       
    
   
                           
                                                      
                                                                       
    
   
          
                                
                          
                                                                                         
   
          
                              
                    
                                                                             
   
                     
                                                                               
   
          
                              
                    
                                                                             
   
                     
                            
                                                                                            
    
                          
                                                                                        
    
   
          
                               
                    
                                                                             
   
                     
                                                                               
   
          
         
          
  
 
// resolveSelfMethodCalls rewrites self.method(args) calls inside method bodies
// to StructType.method(self, args), where StructType is derived from the
// function's implicit self parameter.
//
// Also rewrites .field.method(args) calls (where .field is self.field) to
// FieldType.method(self.field, args), so that method calls on struct fields
// are dispatched to the field's type method. This mirrors the self.method()
// rewrite and is required because the LLVM generator only handles Identifier
// receivers, not DotExpression receivers.
                                                      
                                             
                                          
                                             
          
           
   
                                                                 
           
   
                                            
                     
                                                
                                                       
    
   
  
 
// resolveMethodCalls rewrites user-written `Type.method(args)` static method
// calls to `module.Type.method(args)` using the typeOwner registry.
//
// prefixMethodNames() renames method definitions from `Type.method` to
// `module.Type.method` (e.g. bigint.cmp → bigint.bigint.cmp).  However,
// checker.ResolveModuleCalls() only rewrites simple `module.fn()` calls — it does
// NOT touch `Type.method()` calls because the method name contains a dot
// and is not collected into moduleFns.
//
// As a result, user-written `bigint.cmp(d, d2)` would keep fnName="bigint.cmp"
// at codegen time, but funcRetTypes has key "bigint.bigint.cmp", causing the
// call to generate undefined `@bigint.cmp`.
//
// This pass is the symmetric counterpart to prefixMethodNames: it rewrites
// call sites so they match the renamed definitions.  Only CallExpressions
// whose receiver is a bare Identifier matching a typeOwner key are rewritten;
// instance method calls (var.method()) and already-prefixed calls are left
// untouched.
                                                                               
                         
        
  
                                                                         
                                                                            
                                                     
                                        
                                          
                                                      
                      
                                  
    
   
  
                                          
                                                           
  
 
                                                                                                                   
                 
        
  
                          
                                  
                          
                                                                                   
   
                           
                     
                                                                         
   
                                   
                     
                                                                         
   
                                 
                    
                                               
                                                                 
    
   
                             
                                         
                                                                
   
                           
                         
                                                                                 
   
                    
                                                              
   
                      
                                                                
   
                    
                                               
                                                                 
    
   
                              
                           
                                                                                     
   
  
 
                                                                                                                                      
                 
            
  
                          
                             
                                                                        
                                                                   
                                                      
                                                        
                                                         
                          
                                                        
                                                          
                                  
                                      
                                                                
                       
       
      
     
    
   
                                            
                                                               
                                                                              
   
                                   
                                                                            
   
          
                            
                                                                              
          
                              
                    
                                                                       
   
                     
                                                                         
   
          
                               
                     
                                                                         
   
          
                              
                    
                                                                       
   
                     
                                                                         
   
          
                           
                         
                                                                                 
   
                           
                                                
                                                           
    
   
                           
                                                
                                                           
    
   
          
                                    
                         
                                                                                 
   
                           
                                                                                     
   
                           
                                                                                     
   
          
  
            
 
// collectStructFields builds a map from struct name to field name → field type
// string. Used by resolveSelfInExpr to look up field types when rewriting
// .field.method(args) calls.
// structFieldTypeString returns the full type string of a struct field,
// taking into account ArraySize and IsSlice flags that are stored separately
// from f.Type (which only holds the element type).
                                                          
                   
           
  
                           
                     
                                                    
  
               
                       
  
               
 
                                                                                
                                             
                                          
                                           
          
           
   
                                   
                               
                                                          
                            
    
   
                          
  
              
 
// collectConcreteTypeAliases builds a map from single concrete type alias
// name to its underlying type string (e.g. "fd" → "i64"). Only aliases of
// the form `name = known-type` (non-union, non-function-type) are collected.
// Used by isConcreteType / isArgTypeCompatible to enforce newtype semantics.
                                                                            
                                  
                                          
                                    
          
           
   
                                                                   
                                                                            
                                        
           
   
                                                  
           
   
                                    
  
              
 
                                                                                                           
                          
                                  
                          
                                                          
   
                           
                     
                                                     
   
                                   
                     
                                                     
   
                                 
                    
                                               
                                                       
    
   
                             
                                         
                                                      
   
                           
                         
                                                         
   
                    
                                               
                                                       
    
   
                              
                           
                                                           
   
  
 
                                                                                                            
                 
        
  
                          
                             
                                                        
                                                                 
                                                                                 
                                                 
                                    
                                                                  
                         
     
                                      
                       
                   
     
                                                                          
    
                                                                    
                                                                                 
                                                                
                                                                                                 
                                   
                                                  
                                                 
                                                     
                                       
                                                                     
                            
        
                                             
                                            
                                 
                            
                                     
                                                                
                       
          
        
                                                                             
       
      
     
    
                                                                            
                                                                
                                                                 
                                                                 
                                                                                                  
                                    
                                                   
                                                  
                                                           
                                                       
                                                             
                                             
                                                      
                                         
                                                                       
                              
          
                                                     
                                                
                               
                                      
                                    
                               
                                        
                                                                   
                          
             
            
                               
          
                                                                               
         
        
       
      
     
    
   
                                                               
                                                       
   
                                   
                                                 
   
                              
                                                   
                                                    
                               
                                                    
                           
                           
                                               
                                                
    
   
                           
                                               
                                                
    
   
                                
                                                         
                                    
                                                        
                                                          
                                                          
  
 
// isConstantExpr returns true if the expression is a compile-time constant literal.
                                                  
                     
                             
             
                           
             
                            
             
  
             
 
// isConstantName 判斷名稱是否符合 Nolang 大寫常數命名規範。
// 規則：首字元為大寫 ASCII 字母（A-Z），且名稱中不含小寫 ASCII 字母（a-z）。
// 例：SBOX, FNV-OFFSET, O-EXCL, A, MAX-LEN 為常數；sum, i, myVar, Foo 不為常數。
// 用於重複定義偵測——大寫常數即使無顯式型別註記也應禁止重複賦值。
                                       
                
              
  
                                    
              
  
                                 
              
                           
               
   
  
            
 
// matchesTargetPlatform 檢查宣告的 PlatformKeys 是否符合目標平台。
// 無 PlatformKeys（平台通用宣告）永遠回傳 true。
// goos/goarch 為空時（未設定目標平台），也回傳 true（向後相容）。
                                                                             
                            
             
  
                                
                                                                       
  
                                               
                     
                                                  
  
                                  
                      
              
   
  
             
 
// resolveModuleConstants walks the program and replaces Identifier references to
// module constants with their literal values, allowing module functions like
// degrees() to reference pi/e directly.
                                                                                              
                         
        
  
                                          
                                                    
  
 
                                                                                                                          
                          
                                  
                          
                                                                               
   
                           
                     
                                                                     
   
                                   
                     
                                                                     
   
                                 
                    
                                      
                     
                              
                      
     
    
                                   
                             
    
                                
                     
                              
     
    
                                        
                                               
                                                                 
    
   
                             
                                         
                                                            
   
                           
                         
                                                                             
   
                    
                                                          
   
                      
                                                            
   
                    
                                               
                                                             
    
   
                              
                           
                                                                                 
   
  
 
                                                                              
                  
        
  
                                        
                                                                  
                               
   
                                                      
                      
                                     
                          
     
                                  
                      
                           
      
     
                                      
    
   
                                                
                      
                                                                       
                                 
     
    
                      
                                                           
                                         
     
                                      
    
   
                                                  
                                
   
  
 
                                                                                                                                             
                 
            
  
                          
                         
                                                                       
                                                                  
                                                                     
                                          
                                                              
           
   
                                                          
                                       
           
   
                                        
             
   
          
                             
                                                                          
                                   
                                                                        
   
          
                              
                    
                                                                   
   
                     
                                                                     
   
          
                               
                     
                                                                     
   
          
                                    
                         
                                                                             
   
                           
                                                                                 
   
                           
                                                                                 
   
          
                           
                         
                                                                             
   
                           
                                                      
                                                             
    
   
                           
                                                      
                                                             
    
   
          
                                
                          
                                                                               
   
          
                              
                    
                                                                   
   
                     
                                                                     
   
          
                              
                    
                                                                   
   
                     
                            
                                                                                  
    
                          
                                                                              
    
   
          
                               
                    
                                                                   
   
                     
                                                                     
   
          
         
          
  
 
// checker.ValidateFuncArgs checks that function call argument types match the function signature.
// rootDir is optional — if empty, only locally defined function signatures are checked.
// If provided, imported function signatures from module files are also resolved.
// funcSigFromDef extracts the parameter and result type info from a function
// definition, used to build the signature table for type inference.
                                                             
                                                
                                  
         
                                
                      
   
                                                                                
  
                                              
                               
         
                                
                      
   
                                               
  
                                                          
 
// funcSigFirstReturnType returns the type of the function's first result
// parameter, or "" if the function has no results.
                                                  
                                             
           
  
                               
 
// processEmbeds 遍歷 program 中帶有 #{embed=...} 註解的 LetStatement，
// 在編譯期讀取指定文件並填充 EmbedData 字段。
// 路徑相對於包根目錄（package.jsonc 所在目錄）解析。
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
// findPackageRootFromFile walks up from a file path to find the directory containing package.jsonc.
func findPackageRootFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		cfgFile := filepath.Join(dir, "package.jsonc")
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
