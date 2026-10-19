package js_test

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseForESMTest parses Nolang source for tests that inspect the ESM import
// model directly (ExtractESMImports / ResolveESMBindings).
func parseForESMTest(t *testing.T, source string) *parser.Program {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	p.Filename = "test.no"
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return program
}

// Test 26: namespace import — `# npm/lodash` emits `import * as` plus the
// module-object binding, and the call site is untouched.
func TestJSGeneratorESMNamespaceImport(t *testing.T) {
	source := "# npm/lodash\n" +
		"x = lodash.get(obj, 'a.b')\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, `import * as __ns_lodash from "lodash";`) {
		t.Errorf("namespace import header missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const lodash = __ns_lodash.default ?? __ns_lodash;") {
		t.Errorf("module object binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, "lodash.get(obj, \"a.b\")") {
		t.Errorf("namespace call site should be unchanged; got:\n%s", out)
	}
}

// Test 27: named import with alias — the call site becomes the local binding.
func TestJSGeneratorESMNamedImport(t *testing.T) {
	source := "# npm/lodash.get as gt\n" +
		"x = gt(obj, 'a.b')\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, `import * as __ns_lodash from "lodash";`) {
		t.Errorf("import header missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const gt = __ns_lodash.get ?? lodash.get;") {
		t.Errorf("named local binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, "gt(obj, \"a.b\")") {
		t.Errorf("call site should use the local binding; got:\n%s", out)
	}
	if strings.Contains(out, "lodash.gt(") {
		t.Errorf("call site should not be module-qualified; got:\n%s", out)
	}
}

// Test 28: default import — binds the module object itself.
func TestJSGeneratorESMDefaultImport(t *testing.T) {
	source := "# npm/react.default as React\n" +
		"x = React('div')\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, `import * as __ns_react from "react";`) {
		t.Errorf("import header missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const react = __ns_react.default ?? __ns_react;") {
		t.Errorf("module object binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const React = react;") {
		t.Errorf("default local binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, "React(\"div\")") {
		t.Errorf("call site should use the default binding; got:\n%s", out)
	}
}

// Test 29: scoped package and subpath specifier.
func TestJSGeneratorESMScopedAndSubpath(t *testing.T) {
	source := "# npm/@scope/pkg\n" +
		"x = pkg.mount(1)\n" +
		"# npm/lodash/fp.get as get\n" +
		"y = get(obj, 'a')\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, `import * as __ns__scope_pkg from "@scope/pkg";`) {
		t.Errorf("scoped import header missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const pkg = __ns__scope_pkg.default ?? __ns__scope_pkg;") {
		t.Errorf("scoped module object binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, `import * as __ns_lodash_fp from "lodash/fp";`) {
		t.Errorf("subpath import header missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const get = __ns_lodash_fp.get ?? fp.get;") {
		t.Errorf("subpath named binding missing; got:\n%s", out)
	}
	if !strings.Contains(out, "get(obj, \"a\")") {
		t.Errorf("subpath named call site wrong; got:\n%s", out)
	}
}

// Test 30: multiple use statements for the same specifier merge into one import.
func TestJSGeneratorESMSameSpecifierAggregated(t *testing.T) {
	source := "# npm/lodash.get\n" +
		"# npm/lodash.trim as clean\n" +
		"a = get(obj, 'x')\n" +
		"b = clean(' y ')\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if n := strings.Count(out, `import * as __ns_lodash from "lodash";`); n != 1 {
		t.Errorf("expected exactly one import for lodash, got %d;\n%s", n, out)
	}
	if !strings.Contains(out, "const get = __ns_lodash.get ?? lodash.get;") {
		t.Errorf("named binding 'get' missing; got:\n%s", out)
	}
	if !strings.Contains(out, "const clean = __ns_lodash.trim ?? lodash.trim;") {
		t.Errorf("aliased binding 'clean' missing; got:\n%s", out)
	}
}

// Test 31: a colliding local binding is renamed ($esm suffix) so the
// program's own function keeps its name and its call sites.
func TestJSGeneratorESMLocalNameCollision(t *testing.T) {
	source := "# npm/lodash.get\n" +
		"get = (x i64) (r i64) {\n" +
		"    r = x\n" +
		"}\n" +
		"y = get(1)\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "const get$esm = __ns_lodash.get ?? lodash.get;") {
		t.Errorf("colliding local should be renamed get$esm; got:\n%s", out)
	}
	if !strings.Contains(out, "function get(x)") {
		t.Errorf("the program's own get function should keep its name; got:\n%s", out)
	}
	if !strings.Contains(out, "get(1)") {
		t.Errorf("the program's own call site should be unchanged; got:\n%s", out)
	}
}

// Test 32: an explicit ESM import takes precedence over the builtin module
// name mappings (here `math` is an npm package, not Math.*).
func TestJSGeneratorESMShadowsBuiltinModule(t *testing.T) {
	withImport := generateJS(t, "# npm/math\nx = math.sin(1)\n")
	t.Logf("with ESM import:\n%s", withImport)
	if !strings.Contains(withImport, `import * as __ns_math from "math";`) {
		t.Errorf("ESM import header missing; got:\n%s", withImport)
	}
	if !strings.Contains(withImport, "math.sin(1)") {
		t.Errorf("ESM call should target the package object; got:\n%s", withImport)
	}
	if strings.Contains(withImport, "Math.sin(") {
		t.Errorf("explicit ESM import must shadow the builtin mapping; got:\n%s", withImport)
	}
	// Control: without the import the builtin mapping is unchanged.
	without := generateJS(t, "x = math.sin(1)\n")
	if !strings.Contains(without, "Math.sin(1)") {
		t.Errorf("builtin mapping should still apply without an import; got:\n%s", without)
	}
}

// Test 33: property (non-call) access on a namespace import.
func TestJSGeneratorESMNamespacePropertyAccess(t *testing.T) {
	source := "# npm/lodash\n" +
		"v = lodash.VERSION\n"
	out := generateJS(t, source)
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "lodash.VERSION") {
		t.Errorf("namespace property access wrong; got:\n%s", out)
	}
}

// Test 34: regression guard — a program without ESM imports must not emit
// any import statement (output stays a classic script).
func TestJSGeneratorNoESMNoImport(t *testing.T) {
	source := "x = 1\n" +
		"print(x)\n"
	out := generateJS(t, source)
	if strings.Contains(out, "import ") {
		t.Errorf("program without ESM imports must not emit import statements; got:\n%s", out)
	}
}

// Test 35: sanitizer rejects path-escape specifiers.
func TestJSGeneratorESMSanitizeSpecifier(t *testing.T) {
	for _, spec := range []string{"", "..", "../evil", "/abs", `back\slash`, "a b"} {
		if _, err := js.SanitizeSpecifier(spec); err == nil {
			t.Errorf("SanitizeSpecifier(%q) should fail", spec)
		}
	}
	for _, spec := range []string{"lodash", "lodash/fp", "@scope/pkg", "@scope/pkg/deep"} {
		if _, err := js.SanitizeSpecifier(spec); err != nil {
			t.Errorf("SanitizeSpecifier(%q) should pass: %v", spec, err)
		}
	}
}

// Test 36: ambiguous short names (two specifiers ending in the same segment)
// are refused instead of silently shadowing.
func TestJSGeneratorESMAmbiguousShortName(t *testing.T) {
	source := "# npm/@scope/pkg\n" +
		"# npm/other/pkg\n"
	l := parseForESMTest(t, source)
	imports, err := js.ExtractESMImports(l)
	if err != nil {
		t.Fatalf("ExtractESMImports: %v", err)
	}
	if _, err := js.ResolveESMBindings(imports, map[string]bool{}); err == nil {
		t.Errorf("ambiguous short name should be refused")
	}
}
