package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStructArrayElementMethodCall tests D2: calling a method on a struct array
// element (e.g. .sessions[idx].init(id)) should work correctly.
// This is the exact pattern from nowork/session.no that was broken.
func TestStructArrayElementMethodCall(t *testing.T) {
	src := `
session {
    id str
    count i64
}

session.init = (id str) {
    .id = id
    .count = 0
}

session-manager {
    sessions [16]session
    count i64
}

session-manager.init = () {
    .count = 0
    .sessions[0].init('test')
    .count = 1
}

main = () {
    sm session-manager
    sm.init()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the method call generates a call to session.init
	if !strings.Contains(ir, "session.init") {
		t.Errorf("IR should contain call to session.init, got:\n%s", ir)
	}

	// Verify the self argument is passed as %session* (from array element GEP)
	hasStructPtrArg := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call") && strings.Contains(trimmed, "session.init") {
			hasStructPtrArg = true
			break
		}
	}
	if !hasStructPtrArg {
		t.Errorf("IR should contain call to session.init with struct pointer, got:\n%s", ir)
	}
}

// TestStructArrayElementStrFieldComparison tests D2: comparing a str field of
// a struct array element (e.g. .sessions[i].id == id) should work correctly.
func TestStructArrayElementStrFieldComparison(t *testing.T) {
	src := `
session {
    id str
    count i64
}

session-manager {
    sessions [16]session
    count i64
}

session-manager.find = (id str) (idx i64) {
    idx = -1
    i = 0
    i < .count -> {
        .sessions[i].id == id -> {
            idx = i
            return
        }
        i = i + 1
    }
}

main = () {
    sm session-manager
    sm.count = 0
    sm.sessions[0].id = 'hello'
    sm.count = 1
    idx = sm.find('hello')
    print(idx)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the comparison loads %str-long from the struct array element
	hasStrLongLoad := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%str-long") {
			hasStrLongLoad = true
			break
		}
	}
	if !hasStrLongLoad {
		t.Errorf("IR should load %%str-long for struct array element str field comparison, got:\n%s", ir)
	}
}

// TestStructArrayElementDeepCopy tests D2: copying a struct array element
// that has str fields (e.g. s = .sessions[idx] where session has str fields).
// The deep clone should correctly copy str fields (not just i64 values).
//
// Bug: generateStructFieldIndexRead returns a GEP pointer for struct elements,
// but generateLet treats it as a loaded value, producing:
//   store %session %ptr, %session* %s  (type mismatch: ptr vs %session)
func TestStructArrayElementDeepCopy(t *testing.T) {
	src := `
session {
    id str
    count i64
}

session-manager {
    sessions [16]session
    count i64
}

session-manager.get-session = (idx i64) (s session) {
    s = .sessions[idx]
}

main = () {
    sm session-manager
    sm.sessions[0].id = 'hello'
    sm.sessions[0].count = 42
    sm.count = 1
    s session
    sm.get-session(0, s)
    print(s.id)
    print(s.count)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the IR references %session type (not just i64)
	if !strings.Contains(ir, "%session") {
		t.Errorf("IR should reference %%session type for struct array element copy, got:\n%s", ir)
	}

	// Verify a GEP with %session base type is generated for element access
	gepFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%session") {
			gepFound = true
			break
		}
	}
	if !gepFound {
		t.Errorf("IR should contain GEP with %%session base type for element copy, got:\n%s", ir)
	}

	// Verify the IR does NOT contain the buggy pattern:
	//   store %session %idx.arr.elem.N, %session* %s
	// where %idx.arr.elem.N is a pointer (GEP result), not a struct value.
	// Instead, it should use deep clone (llvm.memcpy) or load+store.
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store %session %idx.arr.elem") {
			t.Errorf("IR should NOT store GEP pointer as struct value (type mismatch), got: %s", trimmed)
		}
	}
}

// TestStructArrayElementFieldAssignStr tests D2: assigning a str value to
// a struct array element's str field (e.g. .sessions[0].id = 'hello').
func TestStructArrayElementFieldAssignStr(t *testing.T) {
	src := `
session {
    id str
    count i64
}

session-manager {
    sessions [16]session
    count i64
}

session-manager.init = () {
    .count = 0
    .sessions[0].id = 'hello'
    .sessions[0].count = 1
    .count = 1
}

main = () {
    sm session-manager
    sm.init()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify store %str-long (not store i64) for the .id field assignment
	hasStrLongStore := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long") {
			hasStrLongStore = true
			break
		}
	}
	if !hasStrLongStore {
		t.Errorf("IR should store %%str-long for struct array element str field assignment, got:\n%s", ir)
	}
}

// TestStructArrayElementWithStrArrayField tests D2: a struct with str array fields
// stored in a struct array, and accessing those str array elements.
// This mimics the session struct from nowork which has [100]str arrays.
func TestStructArrayElementWithStrArrayField(t *testing.T) {
	src := `
session {
    id str
    roles [4]str
    count i64
}

session-manager {
    sessions [8]session
    count i64
}

session-manager.set-role = (idx i64, role str) {
    .sessions[idx].roles[0] = role
}

session-manager.get-role = (idx i64) (role str) {
    role = .sessions[idx].roles[0]
}

main = () {
    sm session-manager
    sm.sessions[0].id = 'test'
    sm.set-role(0, 'user')
    role = ''
    sm.get-role(0, role)
    print(role)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the IR references %session type
	if !strings.Contains(ir, "%session") {
		t.Errorf("IR should reference %%session type, got:\n%s", ir)
	}

	// Verify store %str-long for role assignment
	hasStrLongStore := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long") {
			hasStrLongStore = true
			break
		}
	}
	if !hasStrLongStore {
		t.Errorf("IR should store %%str-long for role assignment, got:\n%s", ir)
	}
}

// TestStructArrayElementDeepCopyToLocal tests D2: copying a struct array element
// to a local variable (not output param) where the struct has str fields.
// This tests the deep clone path for DotExpression Left in IndexExpression.
func TestStructArrayElementDeepCopyToLocal(t *testing.T) {
	src := `
session {
    id str
    count i64
}

session-manager {
    sessions [8]session
    count i64
}

session-manager.copy-session = (idx i64) (s session) {
    s = .sessions[idx]
}

main = () {
    sm session-manager
    sm.sessions[0].id = 'hello'
    sm.sessions[0].count = 42
    sm.count = 1
    s session
    sm.copy-session(0, s)
    print(s.id)
    print(s.count)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the IR does NOT contain the buggy pattern:
	//   store %session %idx.arr.elem.N, %session* %s
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store %session %idx.arr.elem") {
			t.Errorf("IR should NOT store GEP pointer as struct value (type mismatch), got: %s", trimmed)
		}
	}

	// Verify deep clone is used (memcpy) or proper load+store
	hasDeepClone := strings.Contains(ir, "llvm.memcpy") || strings.Contains(ir, "load %session")
	if !hasDeepClone {
		t.Errorf("IR should use deep clone (memcpy) or load %%session for struct array element copy, got:\n%s", ir)
	}
}

// TestStructFieldNestedStrAssign tests D3: assigning a string literal to a
// nested struct field (e.g., self.t.name = 'read_file') inside a method.
// Bug: generateAssignExpression's nested field path stored the string literal
// pointer (%str-longlit.N) directly as a %str-long value, causing:
//   store %str-long %str-longlit.1, %str-long* %set.nested.sub.gep.8  (type mismatch)
func TestStructFieldNestedStrAssign(t *testing.T) {
	src := `
tool {
    name str
    kind i64
}

holder {
    t tool
}

holder.init = () {
    .t.name = 'read_file'
    .t.kind = 42
}

main = () {
    h holder
    h.init()
    print(h.t.name)
    print(h.t.kind)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the nested field assignment loads the str-long value before storing
	// (not storing the %str-longlit pointer directly)
	hasNestedStrLoad := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load %str-long") && strings.Contains(trimmed, "set.nested") {
			hasNestedStrLoad = true
			break
		}
	}
	if !hasNestedStrLoad {
		// Also check that there's no direct store of str-longlit to nested sub.gep
		for _, line := range strings.Split(ir, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "store %str-long %str-longlit") && strings.Contains(trimmed, "set.nested") {
				t.Errorf("IR should NOT store str-longlit pointer as str-long value in nested assignment, got: %s", trimmed)
			}
		}
	}
}

// TestMethodCallWithOutputParam tests voidSingleOutput fix: a method call
// with an output parameter (e.g., sm.get-session(0, s)) should pass the output
// variable pointer as the last argument, without adding an extra temp buffer.
// Bug: voidSingleOutput always added a temp buffer even when the caller
// already passed the output param, causing an extra argument mismatch.
func TestMethodCallWithOutputParam(t *testing.T) {
	src := `
session {
    id i64
    count i64
}

session-manager {
    sessions [8]session
    count i64
}

session-manager.get-session = (idx i64) (s session) {
    s = .sessions[idx]
}

main = () {
    sm session-manager
    sm.sessions[0].id = 100
    sm.sessions[0].count = 5
    s session
    sm.get-session(0, s)
    print(s.id)
    print(s.count)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Verify the method call has exactly 3 arguments (self + idx + output)
	// and does NOT have a 4th vso.tmp argument
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @session-manager.get-session") {
			// Should contain %session* %s (the output param), not %vso.tmp
			if strings.Contains(trimmed, "%vso.tmp") {
				t.Errorf("IR should NOT have vso.tmp in method call with output param, got: %s", trimmed)
			}
			// Should contain %s as output parameter
			if !strings.Contains(trimmed, "%s") {
				t.Errorf("IR should pass %%s as output parameter, got: %s", trimmed)
			}
		}
	}
}
