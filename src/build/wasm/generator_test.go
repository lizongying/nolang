package wasm

import (
	"bytes"
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
		i += int