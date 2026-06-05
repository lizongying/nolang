package parser

import (
	"os"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestHashFiles(t *testing.T) {
	files := []string{
		"../../src/std/hash/md5.no",
		"../../src/std/hash/fnv-1a.no",
		"../../src/std/hash/crc32.no",
		"../../src/std/arr.no",
		"../../src/std/vec.no",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Skipf("file not found: %v", err)
				return
			}
			l := lexer.New(string(data))
			p := New(l)
			p.ParseProgram()
			if len(p.Errors()) > 0 {
				for _, e := range p.Errors() {
					t.Errorf("parse error: %s", e)
				}
			}
		})
	}
}
