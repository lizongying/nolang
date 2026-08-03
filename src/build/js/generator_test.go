// Package js_test contains unit tests for the Nolang JS backend generator.
//
// These tests exercise the type-erasure JS codegen: Nolang source is parsed
// into *parser.Program and fed to (*js.Generator).Generate, then the emitted
// JavaScript is asserted via string containment checks.
//
// The test package is external (js_test) so the integration test can import
// the parent build package (which itself imports build/js) without forming
// an import cycle.
package js_test

import (
	"os"
	"strings"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// generateJS is the shared helper: parse Nolang source and emit JS via the
// JS backend generator. A parse error fails the calling test immediately.
func generateJS(t *testing.T, source string) string {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	p.Filename = "test.no"
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	out, err := g.Generate(program)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	return out
}

// generateJSWithEnv is like generateJS but selects the target environment
// ("node" or "browser"). Browser mode injects the prelude and uses shims.
func generateJSWithEnv(t *testing.T, source string, env string) string {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	p.Filename = "test.no"
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	if env == "browser" {
		g.SetTargetEnv("browser")
	}
	out, err := g.Generate(program)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	return out
}

// Test 1: Type erasure for variable declarations.
func TestJSGeneratorBasicVariables(t *testing.T) {
	src := "x int = 5\n" +
		"name str = 'Alice'\n" +
		"flag bool = true\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"let x = 5;",
		"let name = \"Alice\";",
		"let flag = true;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
	// Type annotations must be erased.
	for _, banned := range []string{"int", "str", "bool"} {
		if strings.Contains(out, banned) {
			t.Errorf("type annotation %q should not appear in output:\n%s", banned, out)
		}
	}
}

// Test 2: Function with a single out-parameter.
func TestJSGeneratorFunction(t *testing.T) {
	src := "add = (a int, b int) (r int) {\n" +
		"    r = a + b\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"function add(a, b) {",
		"let r;",
		"return r;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
	if strings.Contains(out, "int") {
		t.Errorf("type annotation \"int\" should not appear in output:\n%s", out)
	}
}

// Test 3: Struct definition and struct literal become a JS class + new.
func TestJSGeneratorStruct(t *testing.T) {
	src := "Point {\n" +
		"    x int\n" +
		"    y int\n" +
		"}\n" +
		"\n" +
		"p = Point {\n" +
		"    x: 3\n" +
		"    y: 4\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"class Point {",
		"constructor(x, y) {",
		"this.x = x;",
		"this.y = y;",
		"new Point(3, 4)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 4: Struct with a method — method emitted inside the class body.
func TestJSGeneratorStructMethod(t *testing.T) {
	src := "Point {\n" +
		"    x int\n" +
		"    y int\n" +
		"}\n" +
		"\n" +
		"Point.sum = () (r int) {\n" +
		"    r = self.x + self.y\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"class Point {",
		"sum() {",
		"let self = this;",
		"let r;",
		"return r;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 5: Range-for loop (left-closed, right-open).
func TestJSGeneratorRangeFor(t *testing.T) {
	src := "i <- [0..3): {\n" +
		"    print('range:', i)\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"for (let i = 0; i < 3; i++) {",
		"console.log(\"range:\", i);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 6: Iterator for-loop over a slice identifier.
func TestJSGeneratorIteratorFor(t *testing.T) {
	src := "nums = [10, 20, 30]\n" +
		"for n in nums {\n" +
		"    print('elem:', n)\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	if !strings.Contains(out, "for (const n of nums) {") &&
		!strings.Contains(out, "for (const n of [10, 20, 30]) {") {
		t.Errorf("expected for-of loop over nums, got:\n%s", out)
	}
	if !strings.Contains(out, "console.log(\"elem:\", n);") {
		t.Errorf("expected console.log(\"elem:\", n); in output")
	}
}

// Test 7: Platform filter — #{js} kept, #{linux-amd64} dropped.
func TestJSGeneratorPlatformFilter(t *testing.T) {
	src := "#{js}\n" +
		"print('js only')\n" +
		"\n" +
		"#{linux-amd64}\n" +
		"print('linux only')\n" +
		"\n" +
		"print('always')\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"console.log(\"js only\");",
		"console.log(\"always\");",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
	if strings.Contains(out, "console.log(\"linux only\");") {
		t.Errorf("linux-only branch should be filtered out:\n%s", out)
	}
}

// Test 8: Match expression desugars to a nested if/else chain in JS.
func TestJSGeneratorMatch(t *testing.T) {
	src := "m = 2\n" +
		"m: {\n" +
		"    1 -> print('one')\n" +
		"    2 -> print('two')\n" +
		"    3 -> print('three')\n" +
		"    -> print('other')\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"console.log(\"one\");",
		"console.log(\"two\");",
		"console.log(\"three\");",
		"console.log(\"other\");",
		"if (",
		"} else {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 9: print/eprint/len builtin mappings.
func TestJSGeneratorBuiltinPrint(t *testing.T) {
	src := "print('hello')\n" +
		"eprint('error')\n" +
		"arr = [1, 2, 3]\n" +
		"print('len:', len(arr))\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"console.log(\"hello\");",
		"console.error(\"error\");",
		"console.log(\"len:\", arr.length);",
		"let arr = [1, 2, 3];",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 10: String concatenation with the Nolang `-` operator becomes JS `+`.
func TestJSGeneratorStringConcat(t *testing.T) {
	src := "name = 'World'\n" +
		"greeting = 'Hello, ' - name - '!'\n" +
		"print(greeting)\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"let greeting = ((\"Hello, \" + name) + \"!\");",
		"console.log(greeting);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 11: math module maps to JS Math.
func TestJSGeneratorMathModule(t *testing.T) {
	src := "x = math.sin(0.0)\n" +
		"y = math.PI\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"Math.sin(0)",
		"Math.PI",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 12: Multiple out-parameters returned as a JS array.
func TestJSGeneratorMultiOutParam(t *testing.T) {
	src := "divmod = (a int, b int) (q int, r int) {\n" +
		"    q = a / b\n" +
		"    r = a % b\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"function divmod(a, b) {",
		"let q;",
		"let r;",
		"return [q, r];",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Test 13: Bare return inside an if becomes `return <out-param>`; the
// function tail auto-emits a final return for the out-param.
func TestJSGeneratorBareReturn(t *testing.T) {
	src := "clamp = (x int, lo int, hi int) (r int) {\n" +
		"    if x < lo {\n" +
		"        r = lo\n" +
		"        return\n" +
		"    }\n" +
		"    r = x\n" +
		"}\n"
	out := generateJS(t, src)
	t.Logf("generated JS:\n%s", out)

	for _, want := range []string{
		"if ((x < lo)) {",
		"r = lo;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
	// Bare return → return r; plus auto-emit trailing return r; → at least 2 occurrences.
	if n := strings.Count(out, "return r;"); n < 2 {
		t.Errorf("expected at least 2 \"return r;\" occurrences (bare return + auto trailing), got %d:\n%s", n, out)
	}
}

// Test 14 (Integration): Build example/js-test/main.no with the JS backend.
// The example is now browser-focused (uses DOM/canvas/storage APIs), so we
// only verify that the JS builds without error — executing it requires a
// browser environment (document, localStorage, etc.) that node cannot provide.
func TestJSGeneratorExampleProject(t *testing.T) {
	examplePath := "../../../example/js-test/main.no"
	if _, err := os.Stat(examplePath); err != nil {
		t.Skip("example/js-test/main.no not found")
	}

	jsCode, err := nbuild.BuildJS(examplePath, nbuild.BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS error: %v", err)
	}
	t.Logf("generated JS (example):\n%s", jsCode)

	// The example is browser-focused: verify the generated JS contains the
	// expected browser API calls rather than executing it with node.
	for _, want := range []string{
		"document.createElement",
		"document.body",
		"localStorage.setItem",
	} {
		if !strings.Contains(jsCode, want) {
			t.Errorf("expected generated JS to contain %q", want)
		}
	}
}

// Test 15: Browser mode injects the prelude that redirects console output.
func TestJSGeneratorBrowserPrelude(t *testing.T) {
	source := `print('hello')`
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "Nolang browser prelude") {
		t.Errorf("browser mode should inject prelude; got:\n%s", out)
	}
	if !strings.Contains(out, "nolang-output") {
		t.Errorf("prelude should reference #nolang-output; got:\n%s", out)
	}
}

// Test 16: Node mode (default) does NOT inject the browser prelude.
func TestJSGeneratorNodeModeNoPrelude(t *testing.T) {
	source := `print('hello')`
	out := generateJSWithEnv(t, source, "node")
	t.Logf("generated JS:\n%s", out)
	if strings.Contains(out, "Nolang browser prelude") {
		t.Errorf("node mode should NOT inject prelude; got:\n%s", out)
	}
}

// Test 17: os.* in browser mode uses shims (window.__nolang_env), not process.*.
func TestJSGeneratorOsModuleBrowser(t *testing.T) {
	source := "x = os.get-env('PATH')\n" +
		"y = os.get-pid()\n"
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	if strings.Contains(out, "process.env") {
		t.Errorf("browser mode should not emit process.env; got:\n%s", out)
	}
	if strings.Contains(out, "process.pid") {
		t.Errorf("browser mode should not emit process.pid; got:\n%s", out)
	}
	if !strings.Contains(out, "window.__nolang_env") {
		t.Errorf("browser mode should emit window.__nolang_env; got:\n%s", out)
	}
}

// Test 18: os.* in node mode still uses process.* (backward compat).
func TestJSGeneratorOsModuleNode(t *testing.T) {
	source := "x = os.get-env('PATH')\n"
	out := generateJSWithEnv(t, source, "node")
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "process.env") {
		t.Errorf("node mode should emit process.env; got:\n%s", out)
	}
}

// Test 19: dom.* module mappings (get-element-by-id, body).
func TestJSGeneratorDomModule(t *testing.T) {
	source := "el = dom.get-element-by-id('mydiv')\n" +
		"body = dom.body()\n"
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "document.getElementById(\"mydiv\")") {
		t.Errorf("dom.get-element-by-id should map to document.getElementById; got:\n%s", out)
	}
	if !strings.Contains(out, "document.body") {
		t.Errorf("dom.body should map to document.body; got:\n%s", out)
	}
}

// Test 20: DOM element method calls (el.set-text(...) → .textContent = ...).
func TestJSGeneratorDomMethodCall(t *testing.T) {
	source := "el = dom.get-element-by-id('mydiv')\n" +
		"el.set-text('hello')\n"
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	// el.set-text maps to (el).textContent = "hello"
	if !strings.Contains(out, ".textContent = \"hello\"") {
		t.Errorf("el.set-text should map to .textContent = ...; got:\n%s", out)
	}
}

// Test 21: canvas.* module and context method calls.
func TestJSGeneratorCanvasModule(t *testing.T) {
	source := "el = dom.create-element('canvas')\n" +
		"ctx = canvas.get-context-2d(el)\n" +
		"ctx.set-fill('red')\n" +
		"ctx.fill-rect(10, 10, 50, 50)\n"
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	// canvas.get-context-2d emits single-quoted '2d' literal
	if !strings.Contains(out, ".getContext('2d')") {
		t.Errorf("canvas.get-context-2d should map to .getContext('2d'); got:\n%s", out)
	}
	if !strings.Contains(out, ".fillStyle = \"red\"") {
		t.Errorf("ctx.set-fill should map to .fillStyle = ...; got:\n%s", out)
	}
	if !strings.Contains(out, ".fillRect(10, 10, 50, 50)") {
		t.Errorf("ctx.fill-rect should map to .fillRect(...); got:\n%s", out)
	}
}

// Test 22: storage.* module mappings (localStorage).
func TestJSGeneratorStorageModule(t *testing.T) {
	source := "storage.set-item('key', 'value')\n" +
		"v = storage.get-item('key')\n"
	out := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "localStorage.setItem(\"key\", \"value\")") {
		t.Errorf("storage.set-item should map to localStorage.setItem; got:\n%s", out)
	}
	if !strings.Contains(out, "localStorage.getItem(\"key\")") {
		t.Errorf("storage.get-item should map to localStorage.getItem; got:\n%s", out)
	}
}

// Test 23: function names ending with -async emit `async function`.
func TestJSGeneratorAsyncFunction(t *testing.T) {
	source := "fetch-data-async = (url str) (data str) {\n" +
		"    data = 'result'\n" +
		"}\n"
	out := generateJSWithEnv(t, source, "node") // async works in both modes
	t.Logf("generated JS:\n%s", out)
	if !strings.Contains(out, "async function fetchDataAsync") {
		t.Errorf("function ending with -async should generate 'async function'; got:\n%s", out)
	}
}

// Test 24: #{js-browser} is kept in browser mode and filtered out in node mode.
func TestJSGeneratorPlatformFilterBrowser(t *testing.T) {
	source := "#{js-browser}\n" +
		"print('browser only')\n" +
		"\n" +
		"#{js}\n" +
		"print('js always')\n" +
		"\n" +
		"print('always')\n"

	// Browser mode: js-browser kept
	browserOut := generateJSWithEnv(t, source, "browser")
	t.Logf("generated JS (browser):\n%s", browserOut)
	if !strings.Contains(browserOut, "console.log(\"browser only\")") {
		t.Errorf("browser mode should keep #{js-browser}; got:\n%s", browserOut)
	}
	if !strings.Contains(browserOut, "console.log(\"js always\")") {
		t.Errorf("browser mode should keep #{js}; got:\n%s", browserOut)
	}

	// Node mode: js-browser filtered out
	nodeOut := generateJSWithEnv(t, source, "node")
	t.Logf("generated JS (node):\n%s", nodeOut)
	if strings.Contains(nodeOut, "console.log(\"browser only\")") {
		t.Errorf("node mode should filter out #{js-browser}; got:\n%s", nodeOut)
	}
	if !strings.Contains(nodeOut, "console.log(\"js always\")") {
		t.Errorf("node mode should keep #{js}; got:\n%s", nodeOut)
	}
}

// Test 25: RenderHTML generates a correct HTML wrapper.
func TestJSGeneratorHTMLWrapper(t *testing.T) {
	html := js.RenderHTML("Test App", "app.js")
	t.Logf("generated HTML:\n%s", html)
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Errorf("HTML should start with <!DOCTYPE html>; got:\n%s", html)
	}
	if !strings.Contains(html, "<title>Test App</title>") {
		t.Errorf("HTML should contain title; got:\n%s", html)
	}
	if !strings.Contains(html, `id="nolang-output"`) {
		t.Errorf("HTML should contain #nolang-output div; got:\n%s", html)
	}
	if !strings.Contains(html, `<script src="app.js"></script>`) {
		t.Errorf("HTML should contain script tag; got:\n%s", html)
	}
}
