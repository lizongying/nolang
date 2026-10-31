package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// workspaceRoot 回傳當前編譯的工作區根目錄（workspace.noml 所在目錄）。
// 優先使用套件宣告的 WorkspaceRoot，否則從 sourcePath 向上查找 workspace.noml。
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

// resolveModuleFile resolves a module path (e.g. "std/byte", "/example/test1/src/utils")
// to its source file path on disk. For std/ modules it uses GetStdSourceFile;
// for user modules it resolves relative to the workspace root.
// Returns empty string if the path cannot be resolved (best-effort).
func resolveModuleFile(modPath, workspaceRoot string) string {
	if strings.HasPrefix(modPath, "std/") || modPath == "std" {
		relPath := strings.TrimPrefix(modPath, "std/")
		if modPath == "std" {
			relPath = ""
		}
		// Try direct path first
		if relPath != "" {
			if p := GetStdSourceFile(relPath); p != "" {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		// Try lookup table (e.g. "net" → "net/net")
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			if p := GetStdSourceFile(full); p != "" {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		// Disk source unavailable (production embeds std via StdFS, and the
		// gob AST cache carries no on-disk path). Return the *logical* std path
		// so merged std statements still carry a std-recognizable SourceFile.
		// This lets `no vet` attribute and filter std-internal diagnostics
		// correctly instead of leaking them as empty-File lints at unrelated
		// line numbers (the "位置不可靠（报在无关语句 / 函数定义行）" symptom,
		// e.g. a std-internal lint at line 266 mis-attributed to a user module
		// because that module happens to have a function spanning that line).
		if relPath == "" {
			return "std"
		}
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			return "std/" + full
		}
		return "std/" + relPath
	}
	// User module: resolve relative to workspace root
	relPath := strings.TrimPrefix(modPath, "/")
	fullPath := filepath.Join(workspaceRoot, relPath)
	if !strings.HasSuffix(fullPath, ".no") {
		fullPath = fullPath + ".no"
	}
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}
	// .noml 直接引入：磁碟上沒有同名 .no 時，將診斷（vet lint）歸因到 .noml。
	if noml := findNomlImport(fullPath); noml != "" {
		return noml
	}
	return ""
}

// resolveASTDependency 嘗試把已匹配的依賴鍵當成「預編 AST 依賴」處理：
// 鍵形如 nogit@github.com/lizongying/notools，發布產物是 <tool>.ast.zip。
//
// 這類依賴沒有原始碼可下載（zip 裡就是 AST），所以不能走
// ResolveDependencyModule → DownloadPackage 那條路；命中時直接回傳
// mmast 解出的 *parser.Program。
//
// 回傳 (prog, true, err) 表示這是 AST 依賴（成功或失敗都由它定奪）；
// (nil, false, nil) 表示不是 AST 依賴，呼叫方續走原本的磁碟/下載解析。
func (t *Transpiler) resolveASTDependency(key, version, importPath string) (*parser.Program, bool, error) {
	if !pkg.IsASTDependencyKey(key) {
		return nil, false, nil
	}
	// 一個 zip = 一個模組。只接受 `use nogit`（工具名）與
	// `use <host/owner/repo>/nogit`（倉庫路徑 + 工具名）兩種寫法；
	// 指向更深的模組路徑時明確報錯，不靜默回退成「找不到檔案」。
	at := strings.Index(key, "@")
	tool, repo := key[:at], key[at+1:]
	rest, hit := subPathAfter(importPath, tool)
	if !hit {
		rest, hit = subPathAfter(importPath, repo)
		if !hit {
			return nil, true, fmt.Errorf(
				"import %q does not match AST dependency %s (use %q or %q)",
				importPath, key, tool, repo+"/"+tool)
		}
	}
	if rest != "" && rest != tool {
		return nil, true, fmt.Errorf(
			"dependency %s ships a single prebuilt module %q; cannot import subpath %q",
			key, tool, importPath)
	}
	prog, err := mmast.EnsureModuleAST(key, version)
	if err != nil {
		return nil, true, err
	}
	return prog, true, nil
}

// subPathAfter 回傳 importPath 去掉前綴 p 之後的剩餘段（已去掉前導 "/"）。
// hit 為 false 表示 importPath 不以 p 開頭（該前綴沒命中）。
func subPathAfter(importPath, p string) (rest string, hit bool) {
	if !strings.HasPrefix(importPath, p) {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimPrefix(importPath, p), "/"), true
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
			if key, ver, matched := t.pkg.MatchDependency(path); matched {
				// 預編 AST 依賴（nogit@host/owner/repo）不落原始碼，直接從
				// <tool>.ast.zip 取回解碼後的 AST。
				if prog, ok, err := t.resolveASTDependency(key, ver, path); ok {
					return prog, err
				}
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
		// .noml 直接引入：同名 .no 不存在時，把 noml 重建為 nolang 源碼
		// （結構體名 = 文件名，見 noml_import.go）。
		if nomlPath := nomlImportPath(fullPath); nomlPath != "" {
			return t.resolveNomlFile(nomlPath)
		}
		return t.resolveFile(fullPath)
	}
	// std/ 開頭 → 標準庫路徑（預設只從內嵌 StdFS 載入，支援單二進制分發）
	if strings.HasPrefix(path, "std/") || path == "std" {
		// strip "std/" prefix to get module path relative to std/
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		// 0. 開發期覆寫：當 $NOLANG_STD_SRC 設定時，優先從磁碟原始碼載入，
		//    讓對 std/ 模組的原始碼修改（含偵錯插樁）立即生效，免於每次重編譯編譯器。
		//    生產路徑（未設定環境變數）不受影響，仍走內嵌 StdFS。
		if env := os.Getenv(pkg.NOLANG_STD_SRC); env != "" {
			tryDisk := func(diskRel string) (*parser.Program, error) {
				diskPath := pkg.GetStdSourceFile(diskRel)
				if diskPath == "" {
					return nil, nil
				}
				data, err := os.ReadFile(diskPath)
				if err != nil {
					return nil, nil
				}
				return t.parseEmbeddedProgram(diskPath, data)
			}
			if relPath != "" {
				if prog, err := tryDisk(relPath); prog != nil {
					return prog, nil
				} else if err != nil {
					return nil, err
				}
			}
			if full, ok := checker.KnownStdModules()[relPath]; ok {
				if prog, err := tryDisk(full); prog != nil {
					return prog, nil
				} else if err != nil {
					return nil, err
				}
			}
		}
		// 1. 直接路徑：std/<relPath>.no
		if relPath != "" {
			embedPath := "std/" + relPath + ".no"
			// Tier 0: 优先用烤进二进制的 std AST（gzip+gob），省掉 lex+parse。
			if prog, ok := checker.StdASTFor(embedPath); ok {
				return prog, nil
			}
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 2. Lookup table: match ShortName to FullPath (e.g. "net" → "net/net")
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			embedPath := "std/" + full + ".no"
			if prog, ok := checker.StdASTFor(embedPath); ok {
				return prog, nil
			}
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
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
			if info.FullPath == relPath || info.ShortName == relPath {
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
	// npm/ 開頭 → 第三方 ESM 模組導入（僅 JS 後端支援：LLVM 無法連結 JS 模組）。
	if strings.HasPrefix(path, "npm/") || path == "npm" {
		spec := strings.TrimPrefix(path, "npm/")
		return nil, fmt.Errorf("import %q: ESM (npm) imports are only supported by the JS backend; rebuild with `no build --js` or `no run --js` (specifier %q cannot be linked into a native binary)", path, spec)
	}
	// 依賴解析：嘗試所有導入路徑匹配 package.noml 中宣告的依賴
	// 支援 URL 風格（github.com/...）、路徑風格（/example/pkg、./pkg）、短名稱風格（test2）
	first := strings.SplitN(path, "/", 2)[0]
	isURLStyle := strings.Contains(first, ".")
	if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
		if key, ver, matched := t.pkg.MatchDependency(path); matched {
			// 同上：預編 AST 依賴走 .ast.zip，不下載原始碼。
			if prog, ok, err := t.resolveASTDependency(key, ver, path); ok {
				return prog, err
			}
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
		return nil, fmt.Errorf("dependency not found: %q is not declared in package.noml dependencies", path)
	}
	// 非 std 路徑 → 透過 alias 解析（一律相對於工作區根目錄，不再以當前源碼檔目錄為基準）
	modulePath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)
	if !strings.HasSuffix(modulePath, ".no") {
		modulePath = modulePath + ".no"
	}
	modulePath = filepath.Clean(modulePath)
	// .noml 直接引入（與上面的 / 分支同口徑）。
	if nomlPath := nomlImportPath(modulePath); nomlPath != "" {
		return t.resolveNomlFile(nomlPath)
	}
	return t.resolveFile(modulePath)
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
