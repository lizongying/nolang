//go:build !wasm

package wasm

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// generateTestModule produces a fresh WASM module for testing. It uses a nil
// program because the skeleton does not yet perform AST codegen (Task 7+).
func generateTestModule(t *testing.T) []byte {
	t.Helper()
	g := &Generator{}
	out, err := g.Generate(&parser.Program{})
	if err != nil {
		t.Fatalf("Generator.Generate returned error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Generator.Generate returned empty bytes")
	}
	return out
}

// --- minimal WASM binary section walker (test-only) ---

// readU32LEB128 decodes an unsigned LEB128 integer from data, returning the
// value and the number of bytes consumed.
func readU32LEB128(data []byte) (uint32, int) {
	var result uint32
	var shift uint
	for i := 0; i < len(data); i++ {
		b := data[i]
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, i + 1
		}
		shift += 7
	}
	return 0, 0
}

// readName reads a WASM name (LEB128 length + UTF-8 bytes).
func readName(data []byte, off int) (string, int) {
	n, ln := readU32LEB128(data[off:])
	end := off + ln + int(n)
	return string(data[off+ln : end]), end
}

// findSection returns the body bytes of the section with the given id, or nil.
func findSection(data []byte, id byte) []byte {
	if len(data) < 8 {
		return nil
	}
	i := 8 // skip magic + version
	for i < len(data) {
		sid := data[i]
		i++
		size, n := readU32LEB128(data[i:])
		i += n
		body := data[i : i+int(size)]
		if sid == id {
			return body
		}
		i += int(size)
	}
	return nil
}

// parsedFuncType is a decoded functype entry from the type section.
type parsedFuncType struct {
	params  []byte
	results []byte
}

// parseTypeSection parses the type section into a slice of functypes.
func parseTypeSection(t *testing.T, data []byte) []parsedFuncType {
	t.Helper()
	body := findSection(data, TypeSection)
	if body == nil {
		t.Fatal("type section not found")
	}
	count, n := readU32LEB128(body)
	types := make([]parsedFuncType, 0, count)
	off := n
	for i := uint32(0); i < count; i++ {
		if body[off] != 0x60 {
			t.Fatalf("expected functype marker 0x60, got 0x%02x at off %d", body[off], off)
		}
		off++
		pCount, pn := readU32LEB128(body[off:])
		off += pn
		params := make([]byte, pCount)
		copy(params, body[off:off+int(pCount)])
		off += int(pCount)
		rCount, rn := readU32LEB128(body[off:])
		off += rn
		results := make([]byte, rCount)
		copy(results, body[off:off+int(rCount)])
		off += int(rCount)
		types = append(types, parsedFuncType{params: params, results: results})
	}
	return types
}

// parsedImport is a decoded import entry.
type parsedImport struct {
	module    string
	name      string
	kind      byte
	typeIndex uint32 // valid when kind == FuncImport
}

// parseImportSection parses the import section into a slice of imports.
func parseImportSection(t *testing.T, data []byte) []parsedImport {
	t.Helper()
	body := findSection(data, ImportSection)
	if body == nil {
		t.Fatal("import section not found")
	}
	count, n := readU32LEB128(body)
	imports := make([]parsedImport, 0, count)
	off := n
	for i := uint32(0); i < count; i++ {
		mod, end := readName(body, off)
		off = end
		name, end2 := readName(body, off)
		off = end2
		kind := body[off]
		off++
		imp := parsedImport{module: mod, name: name, kind: kind}
		if kind == byte(FuncImport) {
			idx, ln := readU32LEB128(body[off:])
			off += ln
			imp.typeIndex = idx
		}
		imports = append(imports, imp)
	}
	return imports
}

// parsedExport is a decoded export entry.
type parsedExport struct {
	name  string
	kind  byte
	index uint32
}

// parseExportSection parses the export section into a slice of exports.
func parseExportSection(t *testing.T, data []byte) []parsedExport {
	t.Helper()
	body := findSection(data, ExportSection)
	if body == nil {
		t.Fatal("export section not found")
	}
	count, n := readU32LEB128(body)
	exports := make([]parsedExport, 0, count)
	off := n
	for i := uint32(0); i < count; i++ {
		name, end := readName(body, off)
		off = end
		kind := body[off]
		off++
		idx, ln := readU32LEB128(body[off:])
		off += ln
		exports = append(exports, parsedExport{name: name, kind: kind, index: idx})
	}
	return exports
}

// findExport returns the export with the given name, or false.
func findExport(exports []parsedExport, name string) (parsedExport, bool) {
	for _, e := range exports {
		if e.name == name {
			return e, true
		}
	}
	return parsedExport{}, false
}

// --- tests ---

func TestWasmMagicAndVersion(t *testing.T) {
	out := generateTestModule(t)
	want := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(out[:8], want) {
		t.Fatalf("magic+version = %x, want %x", out[:8], want)
	}
}

func TestWasiImportsPresent(t *testing.T) {
	out := generateTestModule(t)
	types := parseTypeSection(t, out)
	imports := parseImportSection(t, out)

	// Expect exactly two imports from wasi_snapshot_preview1.
	var fdWrite, procExit *parsedImport
	for i := range imports {
		imp := imports[i]
		if imp.module != "wasi_snapshot_preview1" {
			t.Errorf("import module = %q, want wasi_snapshot_preview1", imp.module)
			continue
		}
		switch imp.name {
		case "fd_write":
			fdWrite = &imports[i]
		case "proc_exit":
			procExit = &imports[i]
		}
	}
	if fdWrite == nil {
		t.Fatal("fd_write import missing")
	}
	if procExit == nil {
		t.Fatal("proc_exit import missing")
	}

	// fd_write type: (i32, i32, i32, i32) -> i32
	if int(fdWrite.typeIndex) >= len(types) {
		t.Fatalf("fd_write typeIndex %d out of range (types=%d)", fdWrite.typeIndex, len(types))
	}
	ft := types[fdWrite.typeIndex]
	wantParams := []byte{byte(I32), byte(I32), byte(I32), byte(I32)}
	wantResults := []byte{byte(I32)}
	if !bytes.Equal(ft.params, wantParams) {
		t.Errorf("fd_write params = %x, want %x", ft.params, wantParams)
	}
	if !bytes.Equal(ft.results, wantResults) {
		t.Errorf("fd_write results = %x, want %x", ft.results, wantResults)
	}

	// proc_exit type: (i32) -> ()
	if int(procExit.typeIndex) >= len(types) {
		t.Fatalf("proc_exit typeIndex %d out of range (types=%d)", procExit.typeIndex, len(types))
	}
	pt := types[procExit.typeIndex]
	wantParams = []byte{byte(I32)}
	if !bytes.Equal(pt.params, wantParams) {
		t.Errorf("proc_exit params = %x, want %x", pt.params, wantParams)
	}
	if len(pt.results) != 0 {
		t.Errorf("proc_exit results = %x, want empty", pt.results)
	}

	// Imports must be the first two function indices (0 and 1).
	if fdWrite.kind != byte(FuncImport) {
		t.Errorf("fd_write kind = %d, want %d (func)", fdWrite.kind, FuncImport)
	}
	if procExit.kind != byte(FuncImport) {
		t.Errorf("proc_exit kind = %d, want %d (func)", procExit.kind, FuncImport)
	}
}

func TestStartFunctionExported(t *testing.T) {
	out := generateTestModule(t)
	exports := parseExportSection(t, out)
	exp, ok := findExport(exports, "_start")
	if !ok {
		t.Fatal(`export "_start" missing`)
	}
	if exp.kind != byte(FuncExport) {
		t.Errorf("_start export kind = %d, want %d (func)", exp.kind, FuncExport)
	}
	// _start is the first defined function: index 2 (after fd_write=0, proc_exit=1).
	if exp.index != 2 {
		t.Errorf("_start export index = %d, want 2", exp.index)
	}
}

func TestMemoryExported(t *testing.T) {
	out := generateTestModule(t)
	exports := parseExportSection(t, out)
	exp, ok := findExport(exports, "memory")
	if !ok {
		t.Fatal(`export "memory" missing`)
	}
	if exp.kind != byte(MemoryExport) {
		t.Errorf("memory export kind = %d, want %d (memory)", exp.kind, MemoryExport)
	}
	if exp.index != 0 {
		t.Errorf("memory export index = %d, want 0", exp.index)
	}

	// Verify the memory section declares at least 1 page min.
	body := findSection(out, MemorySection)
	if body == nil {
		t.Fatal("memory section not found")
	}
	count, n := readU32LEB128(body)
	if count < 1 {
		t.Fatalf("memory section count = %d, want >= 1", count)
	}
	flag := body[n]
	if flag != 0x00 {
		t.Errorf("memory limits flag = 0x%02x, want 0x00 (no max)", flag)
	}
	min, _ := readU32LEB128(body[n+1:])
	if min < 1 {
		t.Errorf("memory min pages = %d, want >= 1", min)
	}
}

func TestValidateWithWasmTools(t *testing.T) {
	if _, err := exec.LookPath("wasm-tools"); err != nil {
		t.Skip("wasm-tools not in PATH; skipping validation")
	}
	out := generateTestModule(t)
	cmd := exec.Command("wasm-tools", "validate", "-")
	cmd.Stdin = bytes.NewReader(out)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wasm-tools validate failed: %v\nstderr: %s", err, stderr.String())
	}
}

func TestValidateWithWasmtime(t *testing.T) {
	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	out := generateTestModule(t)
	tmp := t.TempDir() + "/skel.wasm"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		t.Fatalf("write tmp wasm: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", tmp)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// wasmtime reports proc_exit(0) as exit code 0; a non-zero exit code is failure.
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("wasmtime run exited %d\nstderr: %s", exitErr.ExitCode(), stderr.String())
		}
		t.Fatalf("wasmtime run failed: %v\nstderr: %s", err, stderr.String())
	}
}

// --- Task 7 codegen tests ---

// parseAndGenerate parses a Nolang source program and generates WASM bytecode.
// Fails the test if parsing or generation produces errors.
func parseAndGenerate(t *testing.T, src string) []byte {
	t.Helper()
	lex := lexer.New(src)
	p := parser.New(lex)
	program := p.ParseProgram()
	if program == nil {
		t.Fatal("ParseProgram returned nil")
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors: %v", errs)
	}
	g := &Generator{}
	out, err := g.Generate(program)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Generate returned empty bytes")
	}
	return out
}

// validateWithWasmTools validates the WASM module with wasm-tools if available.
// Returns true if validation was performed (and passed). Returns false (without
// failing the test) if wasm-tools is not installed.
func validateWithWasmTools(t *testing.T, wasm []byte) bool {
	t.Helper()
	if _, err := exec.LookPath("wasm-tools"); err != nil {
		return false
	}
	cmd := exec.Command("wasm-tools", "validate", "-")
	cmd.Stdin = bytes.NewReader(wasm)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wasm-tools validate failed: %v\nstderr: %s", err, stderr.String())
	}
	return true
}

// runWithWasmtime runs the WASM module with wasmtime and returns stdout.
// Returns (output, true) on success, or ("", false) if wasmtime is not installed.
// Fails the test if wasmtime is installed but the run fails or exits non-zero.
func runWithWasmtime(t *testing.T, wasm []byte) (string, bool) {
	t.Helper()
	if _, err := exec.LookPath("wasmtime"); err != nil {
		return "", false
	}
	tmp := t.TempDir() + "/task7.wasm"
	if err := os.WriteFile(tmp, wasm, 0o644); err != nil {
		t.Fatalf("write tmp wasm: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", tmp)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("wasmtime exited %d\nstdout: %s\nstderr: %s",
				exitErr.ExitCode(), stdout.String(), stderr.String())
		}
		t.Fatalf("wasmtime run failed: %v\nstderr: %s", err, stderr.String())
	}
	return stdout.String(), true
}

// TestHelloWorldCodeGen verifies SubTask 7.3 (print builtin) and SubTask 7.1
// (string literal loading) by emitting `print('Hello, World!')` and checking
// the runtime stdout.
func TestHelloWorldCodeGen(t *testing.T) {
	src := `print('Hello, World!')`
	out := parseAndGenerate(t, src)

	if !validateWithWasmTools(t, out) {
		t.Skip("wasm-tools not in PATH; skipping validation")
	}

	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}

	want := "Hello, World!\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestArithmeticCodeGen verifies SubTask 7.2 (arithmetic operators) and
// SubTask 7.3 (integer print) by emitting a sequence of arithmetic prints.
func TestArithmeticCodeGen(t *testing.T) {
	src := `print(3 + 4)
print(10 - 3)
print(6 * 7)
print(20 / 4)
print(17 % 5)`
	out := parseAndGenerate(t, src)

	if !validateWithWasmTools(t, out) {
		t.Skip("wasm-tools not in PATH; skipping validation")
	}

	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}

	want := "7\n7\n42\n5\n2\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestFibonacciCodeGen verifies SubTask 7.5 (function definition and call),
// SubTask 7.4 (for-range control flow), and SubTask 7.2 (arithmetic) by
// computing fib(10) iteratively and printing the result.
func TestFibonacciCodeGen(t *testing.T) {
	src := `fib = (n i64) (r i64) {
	a = 0
	b = 1
	i <- [2..n]: {
		c = a + b
		a = b
		b = c
	}
	r = b
}
result = fib(10)
print(result)`
	out := parseAndGenerate(t, src)

	if !validateWithWasmTools(t, out) {
		t.Skip("wasm-tools not in PATH; skipping validation")
	}

	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}

	want := "55\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// --- Task 8 codegen tests ---

// runWithWasmtimeExit runs the WASM module with wasmtime and returns
// (stdout, exitCode, true). Returns ("", 0, false) if wasmtime is not installed.
// Unlike runWithWasmtime, this does NOT fail the test on non-zero exit; the
// caller checks the exit code (used for bounds-check tests).
func runWithWasmtimeExit(t *testing.T, wasm []byte) (string, int, bool) {
	t.Helper()
	if _, err := exec.LookPath("wasmtime"); err != nil {
		return "", 0, false
	}
	tmp := t.TempDir() + "/task8.wasm"
	if err := os.WriteFile(tmp, wasm, 0o644); err != nil {
		t.Fatalf("write tmp wasm: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", tmp)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("wasmtime run failed: %v\nstderr: %s", err, stderr.String())
		}
	}
	return stdout.String(), exitCode, true
}

// TestStringVariable verifies SubTask 8.2: assigning a string literal to a
// variable and printing the variable reads back the descriptor's data/len.
func TestStringVariable(t *testing.T) {
	src := `s = 'hello'
print(s)`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "hello\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestStringConcat verifies SubTask 8.2: string concatenation with `-`
// produces a new descriptor whose data is the concatenation of both strings.
func TestStringConcat(t *testing.T) {
	src := `a = 'foo'
b = 'bar'
c = a - b
print(c)
print('Hello' - ' ' - 'World')`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "foobar\nHello World\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestStringLen verifies SubTask 8.2: len(s) returns the descriptor's len field.
func TestStringLen(t *testing.T) {
	src := `s = 'hello'
print(len(s))
print(s.len)`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "5\n5\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestVecBasic verifies SubTask 8.3: vec literal construction and index read.
func TestVecBasic(t *testing.T) {
	src := `v = [10, 20, 30]
print(v[0])
print(v[1])
print(v[2])`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "10\n20\n30\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestVecLen verifies SubTask 8.3: len(v) and v.len return the vec's len field.
func TestVecLen(t *testing.T) {
	src := `v = [1, 2, 3]
print(len(v))
print(v.len)`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "3\n3\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestVecIndexAssign verifies SubTask 8.3: vec[i] = value writes via descriptor.
func TestVecIndexAssign(t *testing.T) {
	src := `v = [1, 2, 3]
v[0] = 99
v[2] = 77
print(v[0])
print(v[1])
print(v[2])`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "99\n2\n77\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestWithLen verifies SubTask 8.3: with-len(n) creates a vec with len=n
// backed by an allocated data buffer that can be indexed and assigned.
func TestWithLen(t *testing.T) {
	src := `v []i64 = with-len(3)
v[0] = 100
v[1] = 200
v[2] = 300
print(v[0])
print(v[1])
print(v[2])
print(len(v))`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "100\n200\n300\n3\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestStructBasic verifies SubTask 8.4: struct definition, literal construction,
// field access, and field assignment.
func TestStructBasic(t *testing.T) {
	src := `user {
    name str
    age i64
}
u = user{
    name: 'Alice'
    age: 30
}
print(u.name)
print(u.age)
u.age = 31
print(u.age)`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, ok := runWithWasmtime(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	want := "Alice\n30\n31\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestBoundsCheck verifies SubTask 8.5: out-of-bounds vec index triggers
// proc_exit(1).
func TestBoundsCheck(t *testing.T) {
	src := `v = [1, 2, 3]
print(v[5])`
	out := parseAndGenerate(t, src)
	validateWithWasmTools(t, out)
	stdout, exitCode, ok := runWithWasmtimeExit(t, out)
	if !ok {
		t.Skip("wasmtime not in PATH; skipping execution")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for OOB index, got %d (stdout=%q)", exitCode, stdout)
	}
}
