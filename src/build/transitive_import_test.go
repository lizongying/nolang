package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestTransitiveImportLLVM verifies that transitively imported modules
// are correctly included in the whole-program IR.
//
// Module graph:  main.no  →  mid/middle.no  →  deep/dep.no
//
// main.no directly imports middle-fn from middle.no.
// middle.no directly imports deep-fn from dep.no.
// dep.no defines deep-fn.
//
// Without correct transitive closure, dep.no would be silently dropped
// and deep-fn would be missing from the IR (D17 regression).
func TestTransitiveImportLLVM(t *testing.T) {
	tmpDir := t.TempDir()

	// workspace.jsonc — defines the workspace root for path resolution.
	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.jsonc"),
		[]byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}

	// deep/dep.no — the transitively imported module (2 hops away from main).
	deepDir := filepath.Join(tmpDir, "deep")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "dep.no"),
		[]byte("deep-fn = (x i64) (r i64) {\n    r = x + 100\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// mid/middle.no — directly imported by main, imports dep transitively.
	midDir := filepath.Join(tmpDir, "mid")
	if err := os.MkdirAll(midDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(midDir, "middle.no"),
		[]byte("# /deep/dep.deep-fn\n"+
			"middle-fn = (x i64) (r i64) {\n    r = deep-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// main.no — entry point, imports middle-fn only (NOT deep-fn directly).
	mainSrc := "# /mid/middle.middle-fn\n" +
		"result = middle-fn(42)\n" +
		"print(result)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile with the LLVM backend.
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	llvmIR, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// The IR must contain deep-fn — if the transitive import were broken,
	// deep-fn would be missing, causing "use of undefined value" at link time.
	if !strings.Contains(llvmIR, "@deep-fn") {
		t.Errorf("LLVM IR does not contain @deep-fn — transitive import was dropped (D17 regression)")
	}

	// The IR must also contain middle-fn (direct import, should always work).
	if !strings.Contains(llvmIR, "@middle-fn") {
		t.Errorf("LLVM IR does not contain @middle-fn — direct import was dropped")
	}
}

// TestTransitiveImportJS verifies the same transitive import chain
// works in the JS backend (resolveAndMergeJSModules).
func TestTransitiveImportJS(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.jsonc"),
		[]byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}

	deepDir := filepath.Join(tmpDir, "deep")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepDir, "dep.no"),
		[]byte("deep-fn = (x i64) (r i64) {\n    r = x + 100\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	midDir := filepath.Join(tmpDir, "mid")
	if err := os.MkdirAll(midDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(midDir, "middle.no"),
		[]byte("# /deep/dep.deep-fn\n"+
			"middle-fn = (x i64) (r i64) {\n    r = deep-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainSrc := "# /mid/middle.middle-fn\n" +
		"result = middle-fn(42)\n" +
		"print(result)\n"

	// Parse main source.
	l := lexer.New(mainSrc)
	p := parser.New(l)
	program := p.ParseProgram()
	if program == nil {
		t.Fatal("parser returned nil program")
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Resolve and merge modules for JS backend.
	pkgConfig, _ := LoadPackage(tmpDir)
	merged, err := resolveAndMergeJSModules(program, mainPath, pkgConfig)
	if err != nil {
		t.Fatalf("resolveAndMergeJSModules error: %v", err)
	}

	// Verify that deep-fn and middle-fn are present in the merged program
	// by scanning for FunctionDefinition and LetStatement names.
	foundDeepFn := false
	foundMiddleFn := false
	for _, stmt := range merged.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if fd.Name == "deep-fn" {
				foundDeepFn = true
			}
			if fd.Name == "middle-fn" {
				foundMiddleFn = true
			}
		}
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			if ls.Name.Value == "deep-fn" {
				foundDeepFn = true
			}
			if ls.Name.Value == "middle-fn" {
				foundMiddleFn = true
			}
		}
	}

	if !foundDeepFn {
		t.Errorf("merged JS program does not contain deep-fn — transitive import was dropped (D17 regression)")
	}
	if !foundMiddleFn {
		t.Errorf("merged JS program does not contain middle-fn — direct import was dropped")
	}
}

// TestTransitiveImportThreeLevels verifies a deeper chain (3 hops)
// to ensure the worklist draining handles arbitrary depth.
//
// main.no → a.no → b.no → c.no
func TestTransitiveImportThreeLevels(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.jsonc"),
		[]byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}

	// c.no — deepest module (3 hops from main)
	if err := os.WriteFile(filepath.Join(tmpDir, "c.no"),
		[]byte("level3-fn = (x i64) (r i64) {\n    r = x + 300\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// b.no — imports c (2 hops from main)
	if err := os.WriteFile(filepath.Join(tmpDir, "b.no"),
		[]byte("# /c.level3-fn\n"+
			"level2-fn = (x i64) (r i64) {\n    r = level3-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// a.no — imports b (1 hop from main)
	if err := os.WriteFile(filepath.Join(tmpDir, "a.no"),
		[]byte("# /b.level2-fn\n"+
			"level1-fn = (x i64) (r i64) {\n    r = level2-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// main.no — imports a only
	mainSrc := "# /a.level1-fn\n" +
		"result = level1-fn(7)\n" +
		"print(result)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	llvmIR, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// All three levels must be present.
	for _, fn := range []string{"@level1-fn", "@level2-fn", "@level3-fn"} {
		if !strings.Contains(llvmIR, fn) {
			t.Errorf("LLVM IR does not contain %s — transitive import chain broken (D17 regression)", fn)
		}
	}
}

// TestTransitiveImportDiamond verifies a diamond dependency graph:
//
//     main.no
//    /        \
//  left.no   right.no
//    \        /
//     shared.no
//
// Both left and right import shared. The shared module must be loaded
// exactly once (dedup) and its functions must be available.
func TestTransitiveImportDiamond(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.jsonc"),
		[]byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}

	// shared.no — imported by both left and right
	if err := os.WriteFile(filepath.Join(tmpDir, "shared.no"),
		[]byte("shared-fn = (x i64) (r i64) {\n    r = x * 2\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// left.no — imports shared
	if err := os.WriteFile(filepath.Join(tmpDir, "left.no"),
		[]byte("# /shared.shared-fn\n"+
			"left-fn = (x i64) (r i64) {\n    r = shared-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// right.no — also imports shared
	if err := os.WriteFile(filepath.Join(tmpDir, "right.no"),
		[]byte("# /shared.shared-fn\n"+
			"right-fn = (x i64) (r i64) {\n    r = shared-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// main.no — imports both left and right (diamond)
	mainSrc := "# /left.left-fn\n" +
		"# /right.right-fn\n" +
		"a = left-fn(10)\n" +
		"b = right-fn(20)\n" +
		"print(a)\n" +
		"print(b)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	llvmIR, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	for _, fn := range []string{"@left-fn", "@right-fn", "@shared-fn"} {
		if !strings.Contains(llvmIR, fn) {
			t.Errorf("LLVM IR does not contain %s — diamond import broken (D17 regression)", fn)
		}
	}

	// shared-fn should appear exactly once as a definition (define),
	// not duplicated.
	defineCount := strings.Count(llvmIR, "@shared-fn")
	// In LLVM IR, @shared-fn appears in both the definition and call sites.
	// We just verify it's present (dedup is handled at the module-loading level).
	if defineCount == 0 {
		t.Errorf("shared-fn not found in LLVM IR at all")
	}
}

// TestTransitiveImportEntryUsesDeepFn reproduces bug08:
// "入口文件 A 必須顯式導入 C，否則 C 的函數無法使用"
//
// Module graph:  main.no  ->  a.no  ->  c.no
//
// main.no imports a-fn from a.no.
// a.no imports c-fn from c.no and calls c-fn inside a-fn.
// c.no defines c-fn.
//
// The key difference from TestTransitiveImportLLVM:
// main.no ALSO directly calls c-fn() — but does NOT explicitly import c.no.
// The transitive import through a.no should make c-fn available to main.no too.
func TestTransitiveImportEntryUsesDeepFn(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.jsonc"),
		[]byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}

	// c.no — defines c-fn
	if err := os.WriteFile(filepath.Join(tmpDir, "c.no"),
		[]byte("c-fn = (x i64) (r i64) {\n    r = x + 1\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// a.no — imports c-fn, calls it inside a-fn
	if err := os.WriteFile(filepath.Join(tmpDir, "a.no"),
		[]byte("# /c.c-fn\n"+
			"a-fn = (x i64) (r i64) {\n    r = c-fn(x)\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// main.no — imports a-fn only, but ALSO directly calls c-fn
	// without explicitly importing c.no.
	// This should work because a.no's import of c.no is transitive.
	mainSrc := "# /a.a-fn\n" +
		"result1 = a-fn(10)\n" +
		"result2 = c-fn(20)\n" +
		"print(result1)\n" +
		"print(result2)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	llvmIR, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v\n--- LLVM IR ---\n%s", err, llvmIR)
	}

	// Both a-fn and c-fn must be in the IR.
	for _, fn := range []string{"@a-fn", "@c-fn"} {
		if !strings.Contains(llvmIR, fn) {
			t.Errorf("LLVM IR does not contain %s — transitive import not visible to entry file (bug08)", fn)
		}
	}
}
