// esm.go 集中放置「第三方 ESM（npm）導入」在建置層的關注點：package.noml 宣告
// 檢查（esmDeclared）、ESM 產物的 package.json sidecar（writeESMModuleSidecar）與
// 執行期依賴探測（CheckESMDeps，供 `no run --js` 以 node 執行前檢查 node_modules）。
// codegen 側的 import 發射在 build/js/esm.go；此檔只做建置／運行決策，與 native 路徑無關。
package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"

	"github.com/lizongying/nolang/i18n"
)

// esmDeclared reports whether an ESM package specifier is declared in the
// package manifest (dependencies or dev-dependencies). The whole package name
// matches, as does any subpath beneath it ("lodash" covers "lodash/fp",
// "@scope/pkg" covers "@scope/pkg/deep").
func esmDeclared(pkg *Package, spec string) bool {
	if pkg == nil {
		return false
	}
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for key := range deps {
			if spec == key || strings.HasPrefix(spec, key+"/") {
				return true
			}
		}
	}
	return false
}

// writeESMModuleSidecar writes (or verifies) the package.json next to the
// emitted JS so that node treats the .js output as an ES module. An existing
// package.json that does not declare "type":"module" is refused with a hint to
// emit a .mjs file instead.
func writeESMModuleSidecar(outPath string) error {
	dir := filepath.Dir(outPath)
	sidecar := filepath.Join(dir, "package.json")
	if existing, err := os.ReadFile(sidecar); err == nil {
		if jsoncDeclaresESM(existing) {
			return nil
		}
		return i18n.Errorf("k8je3usi", "%s already exists and is not an ES module (no \"type\": \"module\"); emit the output as .mjs instead (no build --js -o out.mjs)", sidecar)
	}
	if err := os.WriteFile(sidecar, []byte("{\n  \"type\": \"module\"\n}\n"), 0644); err != nil {
		return i18n.Errorf("zae1l26t", "writing %s: %w", sidecar, err)
	}
	return nil
}

// jsoncDeclaresESM reports whether a package.json body contains
// "type": "module" (JSONC comments/trailing commas tolerated by the substring
// scan — this only ever guards the sidecar, nothing parses the file).
func jsoncDeclaresESM(b []byte) bool {
	s := string(b)
	for _, sep := range []string{":", " :", ": ", " : "} {
		if strings.Contains(s, "\"type\""+sep+"\"module\"") {
			return true
		}
	}
	return false
}

// nativeLinkLibs returns the package's compiler.link-libs entries, or nil.
func nativeLinkLibs(pkg *Package) []string {
	if pkg == nil {
		return nil
	}
	return pkg.Compiler.LinkLibs
}

// programESMImports parses inputPath with the same module resolution the JS
// backend uses and returns every external ESM module it imports — third-party
// packages plus the native koffi/wasm specifiers.
func programESMImports(inputPath string) ([]js.ESMImport, error) {
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, i18n.Errorf("8apmtyk6", "reading input file: %w", err)
	}
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return nil, i18n.Errorf("umcsmsz6", "%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s: %v", inputPath, errs)
	}
	// P7 re-home：js 後端改走 Plain 變體（純 IfExpression 鏈，不含 match 專用欄位）。
	p.DesugarSurfaceMatchesPlain(program)
	pkgConfig, _ := LoadPackage(filepath.Dir(inputPath))
	program, merr := resolveAndMergeJSModules(program, inputPath, pkgConfig)
	if merr != nil {
		return nil, i18n.Errorf("gczvzsdh", "module resolution: %w", merr)
	}
	imports, ierr := js.ExtractESMImports(program)
	if ierr != nil {
		return nil, ierr
	}
	// 原生包呼叫：FFI 需 koffi、WASM 需其 specifier，兩者都以 ESM 形式在 node
	// 執行期解析。這裡以 node（browser=false）視角解析；瀏覽器分支只用 hasESM
	// 決定是否起 http 服務。瀏覽器＋ffi 的硬錯不會到這裡（runCommand 已先建置，
	// 建置期就失敗）。
	if extra, xerr := js.NativeESMImports(program, false, nativeLinkLibs(pkgConfig)); xerr == nil {
		imports = append(imports, extra...)
	}
	return imports, nil
}

// LocalESMAssets returns the relative (local-file) ESM specifiers the program
// imports — e.g. `wasm = './math-wasm.mjs'`. `no run --js` executes its bundle
// from a temp directory, so those files must be linked in (see runCommand):
// unlike bare specifiers they are not reachable through node_modules, and they
// resolve against the bundle's own location.
func LocalESMAssets(inputPath string) ([]string, error) {
	imports, err := programESMImports(inputPath)
	if err != nil {
		return nil, err
	}
	var locals []string
	for _, imp := range imports {
		if js.IsRelativeSpecifier(imp.Specifier) {
			locals = append(locals, imp.Specifier)
		}
	}
	return locals, nil
}

// CheckESMDeps parses inputPath (with the same module resolution the JS
// backend uses) and reports which third-party ESM specifiers it imports are
// not satisfiable from the nearest installed node_modules tree, plus that
// tree's directory ("" when none is found). It backs `no run --js`, which
// executes the emitted bundle with node: node resolves bare specifiers by
// walking up from the script's directory, so a bundle written to a temp dir
// needs the project's node_modules linked into it (see runCommand).
func CheckESMDeps(inputPath string) (missing []string, nmDir string, err error) {
	imports, err := programESMImports(inputPath)
	if err != nil {
		return nil, "", err
	}
	if len(imports) == 0 {
		return nil, "", nil
	}
	nmDir = findNodeModules(filepath.Dir(inputPath))
	for _, imp := range imports {
		spec := imp.Specifier
		// A relative specifier ("./math-wasm.mjs") is a file the JS engine
		// resolves itself (node: next to the bundle; browser: next to the
		// document) — node_modules never contains it.
		if js.IsRelativeSpecifier(spec) {
			continue
		}
		if nmDir == "" {
			missing = append(missing, spec)
			continue
		}
		// A subpath specifier ("lodash/fp", "@scope/pkg/deep") must exist
		// beneath the installed package tree; a bare package name just needs
		// its directory.
		if _, serr := os.Stat(filepath.Join(nmDir, spec)); serr != nil {
			missing = append(missing, spec)
		}
	}
	return missing, nmDir, nil
}

// findNodeModules walks up from the directory of file looking for an
// installed node_modules.
func findNodeModules(file string) string {
	dir, err := filepath.Abs(file)
	if err != nil {
		dir = file
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	} else if err != nil {
		// Non-existent path (e.g. "main.no" resolved against another cwd):
		// still resolve its directory section.
		dir = filepath.Dir(file)
	}
	for {
		cand := filepath.Join(dir, "node_modules")
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
