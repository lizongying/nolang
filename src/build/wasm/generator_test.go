//go:build !wasm

package wasm

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

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
