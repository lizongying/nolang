// esm.go 集中放置「第三方 ESM（npm）導入」在建置層的關注點：package.jsonc 宣告
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
		return fmt.Errorf("%s already exists and is not an ES module (no \"type\": \"module\"); emit the output as .mjs instead (no build --js -o out.mjs)", sidecar)
	}
	if err := os.WriteFile(sidecar, []byte("{\n  \"type\": \"module\"\n}\n"), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", sidecar, err)
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

// CheckESMDeps parses inputPath (with the same module resolution the JS
// backend uses) and reports which third-party ESM specifiers it imports are
// not satisfiable from the nearest installed node_modules tree, plus that
// tree's directory ("" when none is found). It backs `no run --js`, which
// executes the emitted bundle with node: node resolves bare specifiers by
// walking up from the script's directory, so a bundle written to a temp dir
// needs the project's node_modules linked into it (see runCommand).
func CheckESMDeps(inputPath string) (missing []string, nmDir string, err error) {
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, "", fmt.Errorf("reading input file: %w", err)
	}
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return nil, "", fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return nil, "", fmt.Errorf("%s: %v", inputPath, errs)
	}
	// P7 re-home：js 後端改走 Plain 變體（純 IfExpression 鏈，不含 match 專用欄位）。
	p.DesugarSurfaceMatchesPlain(program)
	pkgConfig, _ := LoadPackage(filepath.Dir(inputPath))
	program, merr := resolveAndMergeJSModules(program, inputPath, pkgConfig)
	if merr != nil {
		return nil, "", fmt.Errorf("module resolution: %w", merr)
	}
	imports, ierr := js.ExtractESMImports(program)
	if ierr != nil {
		return nil, "", ierr
	}
	if len(imports) == 0 {
		return nil, "", nil
	}
	nmDir = findNodeModules(filepath.Dir(inputPath))
	for _, imp := range imports {
		spec := imp.Specifier
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
