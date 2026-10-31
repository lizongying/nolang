// js_backend.go 集中放置 JS 後端（`no build --js` / `no run --js`）的建置管線：
// buildJSBackend / buildJSPkg 兩個入口、resolveAndMergeJSModules 本地模組合併、
// BuildJS/BuildJSEx/BuildJSBrowser codegen 呼叫，以及瀏覽器 import map 的 CDN 解析。
// 與 builder.go 的 native（LLVM）與 Direct-WASM 路徑分離，JS 相關改動只動本檔。
package build

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// buildJSBackend 對應原 main.go 的 -js 分支：繞過 LLVM 工具鏈，發射 JS 原始碼並寫入
// opts.Output（預設 dist/<name>.js）。BrowserMode 時額外產出 HTML 包裝並返回 .html 路徑。
func buildJSBackend(inputPath string, opts BuildOptions) (string, error) {
	// 目錄 → main.no（workspace 模式不支援 JS 後端，要求明確輸入檔）。
	if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
		mainPath := filepath.Join(inputPath, "main.no")
		if _, err := os.Stat(mainPath); err == nil {
			inputPath = mainPath
		} else {
			return "", fmt.Errorf("JS backend requires an explicit input file or main.no (workspace mode not supported)")
		}
	}

	if opts.BrowserMode {
		baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
		if baseName == "main" {
			if pkg, _ := LoadPackage(filepath.Dir(inputPath)); pkg != nil && pkg.Name != "" {
				baseName = pkg.Name
			}
		}
		outJs := filepath.Join("dist", baseName+".js")
		outHtml := filepath.Join("dist", baseName+".html")
		if opts.Output != "" {
			outJs = opts.Output
			outHtml = strings.TrimSuffix(opts.Output, filepath.Ext(opts.Output)) + ".html"
		}
		os.MkdirAll("dist", 0755)
		jsCode, htmlCode, esmImports, err := BuildJSBrowser(inputPath, BuildOptions{Verbose: opts.Verbose, UseJS: true, BrowserMode: true}, baseName+".js", esmCDNBase(inputPath, opts))
		if err != nil {
			return "", fmt.Errorf("JS build error: %w", err)
		}
		if err := os.WriteFile(outJs, []byte(jsCode), 0644); err != nil {
			return "", fmt.Errorf("writing JS output %s: %w", outJs, err)
		}
		if err := os.WriteFile(outHtml, []byte(htmlCode), 0644); err != nil {
			return "", fmt.Errorf("writing HTML output %s: %w", outHtml, err)
		}
		if len(esmImports) > 0 {
			if err := writeESMModuleSidecar(outJs); err != nil {
				return "", err
			}
		}
		return outHtml, nil
	}

	res, err := BuildJSEx(inputPath, opts)
	if err != nil {
		return "", fmt.Errorf("JS build error: %w", err)
	}
	out := opts.Output
	if out == "" {
		baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
		if baseName == "main" {
			if pkg, _ := LoadPackage(filepath.Dir(inputPath)); pkg != nil && pkg.Name != "" {
				baseName = pkg.Name
			}
		}
		out = filepath.Join("dist", baseName+".js")
		os.MkdirAll("dist", 0755)
	}
	if err := os.WriteFile(out, []byte(res.Code), 0644); err != nil {
		return "", fmt.Errorf("writing JS output %s: %w", out, err)
	}
	// ESM output must be loaded as a module: drop a package.json sidecar next
	// to the artifact (npm convention) so `node dist/x.js` treats it as ESM.
	if len(res.ESMImports) > 0 {
		if err := writeESMModuleSidecar(out); err != nil {
			return "", err
		}
	}
	return out, nil
}

// buildJSPkg 使用 JS 後端從 Nolang 源碼發射 JavaScript 並寫入輸出檔案。
// 當 package.noml 的 compiler.emit 為 "js" 時由 buildWithPkg 呼叫，
// 使 workspace 模式下的單個 JS 目標也能自動走 JS 後端。
func buildJSPkg(source []byte, inputPath string, pkg *Package, opts BuildOptions, outPath string, sink *bytes.Buffer) error {
	// 輸出路徑加上 .js 副檔名
	if !strings.HasSuffix(outPath, ".js") {
		outPath = outPath + ".js"
	}

	// 解析為 AST
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return fmt.Errorf("%s: %v", inputPath, errs)
	}

	// P7 re-home：js 後端改走 DesugarSurfaceMatchesPlain（純 IfExpression 鏈，不含 match 專用欄位）。
	p.DesugarSurfaceMatchesPlain(program)

	// Inject -ld-KEY=VALUE compile-time global constants
	injectLDFlags(program, opts.LDFlags)

	// 解析並合併本地模組導入
	program, err := resolveAndMergeJSModules(program, inputPath, pkg)
	if err != nil {
		return fmt.Errorf("module resolution: %w", err)
	}

	// 型別校驗（與 LLVM 路徑一致）
	if err := validateProgram(program, inputPath); err != nil {
		return err
	}

	// JS 後端 codegen（型別擦除）
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	envMode := "node"
	if opts.BrowserMode {
		g.SetTargetEnv("browser")
		envMode = "browser"
	}
	if opts.Verbose {
		vprintf(sink, "[js] emit: js (type erasure), env: %s\n", envMode)
	}
	jsCode, err := g.Generate(program)
	if err != nil {
		return fmt.Errorf("JS generation error: %w", err)
	}

	// 確保輸出目錄存在
	if dir := filepath.Dir(outPath); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(outPath, []byte(jsCode), 0644); err != nil {
		return fmt.Errorf("writing JS output: %w", err)
	}
	// ESM output needs the package.json {"type":"module"} sidecar too.
	if imports, err := js.ExtractESMImports(program); err != nil {
		return err
	} else if len(imports) > 0 {
		if err := writeESMModuleSidecar(outPath); err != nil {
			return err
		}
	}

	return nil
}

// resolveAndMergeJSModules resolves local module imports (UseStatements with
// paths starting with "/") and merges their statements into the main program.
// This is necessary because the JS backend, unlike the LLVM backend, does not
// go through the full Transpiler.Compile pipeline that handles module merging.
//
// Without this step, global variables and functions defined in imported local
// modules (e.g. state.no, editor.no) would be missing from the generated JS,
// causing runtime ReferenceErrors (e.g. "EDITOR is not defined").
//
// js/ modules are skipped — their function calls are mapped to JS builtins by
// builtin.go. std/ modules are also skipped — common std functions are mapped
// by builtin.go, and merging full std source would bloat the output.
// npm/ (third-party ESM) modules are never merged as source either: the use
// statement itself is kept in the merged program so the JS generator can emit
// the import header and the checker can register the imported names.
func resolveAndMergeJSModules(program *parser.Program, inputPath string, pkgConfig *Package) (*parser.Program, error) {
	if program == nil {
		return program, nil
	}

	// Determine workspace root for resolving "/"-prefixed import paths.
	var wsRoot string
	if pkgConfig != nil {
		wsRoot = pkgConfig.WorkspaceRoot()
		if wsRoot == "" {
			wsRoot = pkgConfig.RootDir
		}
	}
	if wsRoot == "" {
		if ws, ok := FindWorkspaceRoot(inputPath); ok {
			wsRoot = ws
		}
	}

	// S5 stage 6（2026-10-21）：JS 後端同口徑——進入裝配前取工作副本，
	// ParseProgram 輸出不可變（§1.4），後續合併/改寫全落在副本上。
	program = parser.CloneProgram(program)
	// 隱式提升（賦值位置整數 → bigint）與 native 後端同口徑：checker 已依同一張
	// 表放行，兩邊都得改寫，否則 JS 目標上 `x bigint = 5` 又變回靜默誤編譯。
	promoteScalarAssignments(program)

	// Collect main program's top-level variable and function names to avoid
	// duplicating them when merging module statements.
	mainVarNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			mainVarNames[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			mainVarNames[fd.Name] = true
		}
	}

	// Build the merged program: start with a fresh statement list and merge
	// semantic contexts from the main program and all imported modules.
	merged := &parser.Program{Statements: []parser.Statement{}}
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(program.Sem)
	// 排版 trivia 表（formatter 專用，與 Sem 並列），合併點同步匯集。
	merged.FmtTrivia = parser.NewFmtTrivia()
	merged.FmtTrivia.Merge(program.FmtTrivia)

	loadedModules := make(map[string]bool) // track resolved file paths to avoid duplicate loading

	// parseModuleFile reads and parses a .no file, applying lib.no export
	// filtering if the module belongs to a package with exports.
	parseModuleFile := func(filePath string) (*parser.Program, error) {
		source, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		l := lexer.NewCached(filePath, string(source))
		p := parser.New(l)
		p.Filename = filepath.Base(filePath)
		prog := p.ParseProgram()
		// P7 re-home：合併的模組也改走 Plain 變體（純 IfExpression 鏈）。
		p.DesugarSurfaceMatchesPlain(prog)
		if len(p.Errors()) > 0 {
			return nil, fmt.Errorf("%s: %v", filePath, p.Errors())
		}
		// Apply lib.no export filtering if the module's package has exports.
		pkgRoot := FindPackageRoot(filePath)
		if pkgRoot != "" {
			libPath := filepath.Join(pkgRoot, "lib.no")
			if _, err := os.Stat(libPath); err == nil {
				prog = checker.FilterByExports(prog, libPath, filePath)
			}
		}
		return prog, nil
	}

	// resolveModulePath converts a UseStatement path to a file path.
	// Only local paths (starting with "/") are handled; std/ and js/ paths
	// are skipped by the caller.
	resolveModulePath := func(usePath string) string {
		relPath := strings.TrimPrefix(usePath, "/")
		fullPath := ResolveToWorkspaceRoot(wsRoot, relPath)
		if !strings.HasSuffix(fullPath, ".no") {
			fullPath = fullPath + ".no"
		}
		return filepath.Clean(fullPath)
	}

	// processUseAndMerge loads a module from a UseStatement, merges its
	// non-UseStatement statements into merged, and collects its UseStatements
	// for transitive processing.
	var pendingUses []*parser.UseStatement
	// esmSeen deduplicates ESM use statements (the same import reached from
	// the main program and a transitively-imported module).
	esmSeen := make(map[string]bool)
	processUseAndMerge := func(use *parser.UseStatement) error {
		// Third-party ESM imports (# npm/<specifier>): never resolved to .no
		// source. The specifier must be declared in package.noml, and the use
		// statement is kept verbatim so the JS backend can emit the import
		// header and the checker can register the imported names.
		if js.IsESMUse(use) {
			spec, _ := js.ESMSpecifier(use)
			if _, err := js.SanitizeSpecifier(spec); err != nil {
				return fmt.Errorf("import %q: %w", use.Path, err)
			}
			if !esmDeclared(pkgConfig, spec) {
				return fmt.Errorf("ESM module %q is not declared in package.noml dependencies; add dependencies: ['%s:^latest'] (or dev-dependencies) and run npm install", spec, spec)
			}
			key := use.Path + "\x00" + use.Function + "\x00" + use.Alias
			if !esmSeen[key] {
				esmSeen[key] = true
				merged.Statements = append(merged.Statements, use)
			}
			return nil
		}

		// 原生包導入（#{bind = ffi|wasm}）：import 行是策略標記，不解析為本地 .no
		// 模組——extern 由程式內的 #{c} 宣告提供。本地 / 模組的語句照例合併，但 use
		// 行本身一貫被丟棄，那會連帶丟掉附在行上的 bind=/wasm=/lib= 註解，故對原生
		// 導入改為保留 use 行，讓 merge 後的 CollectNativePolicy 仍讀得到策略。
		if np, isNative := js.NativeUsePolicy(use, merged.Sem); isNative {
			// A relative wasm= specifier ("./math-wasm.mjs") is a local file
			// next to the emitted output, not an npm package: it needs no
			// dependencies entry and is not installed by npm.
			if np.WASM != "" && !js.IsRelativeSpecifier(np.WASM) && !esmDeclared(pkgConfig, np.WASM) {
				return fmt.Errorf("native wasm module %q is not declared in package.noml dependencies; add dependencies: ['%s:^latest'] (or dev-dependencies) and run npm install", np.WASM, np.WASM)
			}
			merged.Statements = append(merged.Statements, use)
			return nil
		}
		// Only resolve local module imports (path starts with "/").
		// Skip js/ modules (handled by builtin.go) and std/ modules
		// (common functions handled by builtin.go).
		if !strings.HasPrefix(use.Path, "/") {
			return nil
		}

		modulePath := resolveModulePath(use.Path)
		if loadedModules[modulePath] {
			return nil
		}
		loadedModules[modulePath] = true

		modProg, err := parseModuleFile(modulePath)
		if err != nil {
			return fmt.Errorf("loading module %s: %w", use.Path, err)
		}
		modProg = parser.CloneProgram(modProg) // S5 stage 6：模組工作副本
		merged.Sem.Merge(modProg.Sem)
		merged.FmtTrivia.Merge(modProg.FmtTrivia)

		for _, ms := range modProg.Statements {
			// Collect transitive UseStatements for recursive processing.
			// Only local module imports (path starts with "/") are merged;
			// std/ and js/ modules are handled by builtin.go. ESM imports are
			// queued too — they are kept verbatim by the ESM branch above.
			if nestedUse, ok := ms.(*parser.UseStatement); ok {
				if strings.HasPrefix(nestedUse.Path, "/") || js.IsESMUse(nestedUse) {
					pendingUses = append(pendingUses, nestedUse)
				}
				continue
			}

			// When alias is specified, only import the specific function/constant.
			if use.Alias != "" {
				if fd, ok := ms.(*parser.FunctionDefinition); ok {
					if use.Function != "" && fd.Name == use.Function {
						fd.Name = use.Alias
						merged.Statements = append(merged.Statements, fd)
					}
				}
				if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
					if use.Function != "" && ls.Name.Value == use.Function {
						ls.Name.Value = use.Alias
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
						}
					}
				}
				continue
			}

			// No alias: merge all non-UseStatement statements.
			// Skip variables/functions that already exist in the main program.
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				if !mainVarNames[fd.Name] {
					merged.Statements = append(merged.Statements, fd)
				}
			} else if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				if !mainVarNames[ls.Name.Value] {
					merged.Statements = append(merged.Statements, ls)
				}
			} else if sd, ok := ms.(*parser.StructDefinition); ok {
				merged.Statements = append(merged.Statements, sd)
			} else if id, ok := ms.(*parser.InterfaceDefinition); ok {
				merged.Statements = append(merged.Statements, id)
			} else if ta, ok := ms.(*parser.TypeAlias); ok {
				merged.Statements = append(merged.Statements, ta)
			} else if ed, ok := ms.(*parser.EnumDefinition); ok {
				merged.Statements = append(merged.Statements, ed)
			} else if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
				merged.Statements = append(merged.Statements, ted)
			} else if es, ok := ms.(*parser.ExternStatement); ok {
				merged.Statements = append(merged.Statements, es)
			}
		}
		return nil
	}

	// Process main program's UseStatements first, then drain transitive imports.
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			if err := processUseAndMerge(use); err != nil {
				return nil, err
			}
			continue
		}
		// Add main program's own statements to merged (except UseStatements).
		merged.Statements = append(merged.Statements, stmt)
	}

	// Process transitive imports: drain the pendingUses worklist.
	for len(pendingUses) > 0 {
		use := pendingUses[0]
		pendingUses = pendingUses[1:]
		if err := processUseAndMerge(use); err != nil {
			return nil, err
		}
	}

	return merged, nil
}

// BuildJSResult carries the JS backend output plus the third-party ESM module
// list, so callers can emit the ESM sidecar (package.json {"type":"module"})
// only when the program actually imports external modules.
type BuildJSResult struct {
	Code       string
	ESMImports []js.ESMImport
}

// BuildJS 使用 JS 後端從 Nolang 源碼直接產生 JavaScript 原始碼字串。
// 當 opts.UseJS 為 true 時呼叫。輸出的 JS 可由 node 直接執行。
//
// 流程：
//  1. 讀取源碼檔案
//  2. 以 lexer + parser 解析為 *parser.Program
//  3. 解析並合併本地模組導入（resolveAndMergeJSModules）
//  4. 由 js.Generator.Generate(program) 直接發射 JS 原始碼（型別擦除）
//  5. 回傳 JS 原始碼字串
//
// 此路徑不經過 LLVM 工具鏈（opt/llc/clang），適用於 Node.js 與瀏覽器環境。
func BuildJS(inputPath string, opts BuildOptions) (string, error) {
	res, err := BuildJSEx(inputPath, opts)
	if err != nil {
		return "", err
	}
	return res.Code, nil
}

// BuildJSEx is BuildJS with the ESM module list exposed (see BuildJS for the
// pipeline description).
func BuildJSEx(inputPath string, opts BuildOptions) (*BuildJSResult, error) {
	// 1. 讀取源碼
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	// 2. 解析為 AST（與 BuildDirectWasm 一致的最小流程）
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return nil, fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s: %v", inputPath, errs)
	}

	// P7 re-home：js 後端改走 DesugarSurfaceMatchesPlain（純 IfExpression 鏈，不含 match 專用欄位）。
	p.DesugarSurfaceMatchesPlain(program)

	// Inject -ld-KEY=VALUE compile-time global constants
	injectLDFlags(program, opts.LDFlags)

	// 3. 解析並合併本地模組導入
	pkgConfig, _ := LoadPackage(filepath.Dir(inputPath))
	program, err = resolveAndMergeJSModules(program, inputPath, pkgConfig)
	if err != nil {
		return nil, fmt.Errorf("module resolution: %w", err)
	}

	// 3.5 型別校驗（與 LLVM 路徑一致，避免型別錯誤延遲到執行期才炸）
	if err := validateProgram(program, inputPath); err != nil {
		return nil, err
	}

	// 3.6 第三方 ESM 模組清單（供呼叫方決定是否寫 package.json sidecar）
	esmImports, err := js.ExtractESMImports(program)
	if err != nil {
		return nil, err
	}

	// 3.7 原生包呼叫：FFI 需 koffi、WASM 需 wasm specifier，都以合成 ESMImport
	// 形態併入清單，使 sidecar、依賴探測與瀏覽器 import map 涵蓋它們。無原生
	// 意圖時不新增任何項目（逐位元組相容）。
	linkLibs := []string(nil)
	if pkgConfig != nil {
		linkLibs = pkgConfig.Compiler.LinkLibs
	}
	nativeImports, nerr := js.NativeESMImports(program, opts.BrowserMode, linkLibs)
	if nerr != nil {
		return nil, nerr
	}
	esmImports = append(esmImports, nativeImports...)

	// 4. JS 後端 codegen（型別擦除）
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	envMode := "node"
	if opts.BrowserMode {
		g.SetTargetEnv("browser")
		envMode = "browser"
	}
	g.SetNativeConfig(linkLibs, opts.BrowserMode)
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "[js] target: js (type erasure), env: %s\n", envMode)
	}
	code, err := g.Generate(program)
	if err != nil {
		return nil, err
	}
	return &BuildJSResult{Code: code, ESMImports: esmImports}, nil
}

// esmCDNBase resolves the browser import-map CDN base: an explicit
// BuildOptions.ESMCDN (the --esm-cdn flag) wins, then package.noml
// compiler.esm-cdn, then "" (the built-in default in the js package).
func esmCDNBase(inputPath string, opts BuildOptions) string {
	if opts.ESMCDN != "" {
		return opts.ESMCDN
	}
	if pkg, err := LoadPackage(filepath.Dir(inputPath)); err == nil && pkg != nil {
		return pkg.Compiler.ESMCDN
	}
	return ""
}

// BuildJSBrowser compiles Nolang source to browser-targeted JS and an HTML wrapper.
// Returns (jsCode, htmlCode, esmImports, error). The HTML references the JS file
// by name (basename only); when the program imports third-party ESM modules the
// HTML becomes a module script plus an import map resolving bare specifiers
// through cdnBase ("" = the built-in default).
func BuildJSBrowser(inputPath string, opts BuildOptions, jsFileName, cdnBase string) (jsCode, htmlCode string, esmImports []js.ESMImport, err error) {
	opts.BrowserMode = true
	res, err := BuildJSEx(inputPath, opts)
	if err != nil {
		return "", "", nil, err
	}

	// Determine title from package name or filename
	title := strings.TrimSuffix(filepath.Base(inputPath), ".no")
	if pkg, perr := LoadPackage(filepath.Dir(inputPath)); perr == nil && pkg != nil && pkg.Name != "" {
		title = pkg.Name
	}

	if len(res.ESMImports) > 0 {
		htmlCode = js.RenderHTMLModule(title, jsFileName, res.ESMImports, cdnBase)
	} else {
		htmlCode = js.RenderHTML(title, jsFileName)
	}
	return res.Code, htmlCode, res.ESMImports, nil
}
