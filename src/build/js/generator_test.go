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
	"os/exec"
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
		"console.log(\"len:\", (arr).length);",
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

// Test 14 (Integration): Build example/js-test/main.no with the JS backend,
// execute the generated JS with node, and verify expected output strings.
func TestJSGeneratorExampleProject(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	examplePath := "../../../example/js-test/main.no"
	if _, err := os.Stat(examplePath); err != nil {
		t.Skip("example/js-test/main.no not found")
	}

	jsCode, err := nbuild.BuildJS(examplePath, nbuild.BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS error: %v", err)
	}
	t.Logf("generated JS (example):\n%s", jsCode)

	tmpFile, err := os.CreateTemp("", "nolang-js-test-*.js")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(jsCode); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	cmd := exec.Command("node", tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node execution error: %v\noutput:\n%s", err, output)
	}
	out := string(output)
	t.Logf("node output:\n%s", out)

	for _, want := range []string{"running on JS backend", "Hello, Alice!", "done"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected node output to contain %q", want)
		}
	}
}
