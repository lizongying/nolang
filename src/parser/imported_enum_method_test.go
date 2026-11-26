package parser

import (
	"strings"
	"testing"
)

// TestImportedEnumMethodPayloadLowering reproduces the merged-module shape:
// method and receiver names are module-prefixed, enum metadata keeps the bare
// enum key, and the receiver copy's type is available only from the AST.
func TestImportedEnumMethodPayloadLowering(t *testing.T) {
	src := `nested {
    nested-integer(v i64),
    nested-empty,
}

sql-value {
    sql-null,
    integer(v nested),
    text(v str),
}

sql-value.read-method = () (out i64) {
    out = 0
    current = .
    current: {
        integer(value) -> {
            value: {
                nested-integer(n) -> out = n

                nested-empty -> out = 0
            }
        }

        text(s) -> out = s.len-bytes()

        sql-null ->
    }
}
`
	prog := parseArmTest(t, src)
	fd := findFuncDef(prog, "sql-value.read-method")
	if fd == nil || len(fd.Results) == 0 {
		t.Fatal("test setup: enum method or synthetic self result not found")
	}
	oldName := fd.Name
	if vars := prog.Sem.FuncVarTypes[oldName]; vars != nil {
		delete(vars, "current")
	}
	fd.Name = "value." + oldName
	fd.Results[0].Type = &NamedType{Value: "value.sql-value"}

	pkg, _ := ASTToHIRWithMap(prog)
	dump := pkg.Dump()
	if !strings.Contains(dump, "let s=n t=i64") {
		t.Fatalf("prefixed enum method did not lower its nested payload binding:\n%s", dump)
	}
	if strings.Contains(dump, "call nested-integer(n)") {
		t.Fatalf("nested enum payload remained a constructor call:\n%s", dump)
	}
}
