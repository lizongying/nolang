package parser

import (
	"os"
	"sort"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestZZDebugDnsTypes(t *testing.T) {
	data, err := os.ReadFile("../std/net/dns.no")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := New(lexer.New(string(data)))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse failed: %v", errs)
	}
	vars := prog.Sem.FuncVarTypes["parse-name"]
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		t.Logf("%-16s = %q", n, vars[n].Raw())
	}
}
