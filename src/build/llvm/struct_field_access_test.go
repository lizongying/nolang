package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStructStrFieldConcat tests D3: accessing a struct str field directly
// in string concatenation (e.g. 'prefix' - cfg.field) should work correctly.
func TestStructStrFieldConcat(t *testing.T) {
	src := `
app-config {
    llm-model str
    api-key str
    max-iter i64
}

test-fn = (cfg app-config) (result str) {
    ; Direct struct field access in string concatenation
    result = 'model: ' - cfg.llm-model
}

main = () {
    cfg app-config
    cfg.llm-model = 'gpt-4'
    cfg.api-key = 'secret'
    cfg.max-iter = 10
    out = test-fn(cfg)
    out.len > 0 -> print-line(out)
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

	// The IR should contain a load of the struct field (dot.val)
	if !strings.Contains(ir, "dot.gep") {
		t.Errorf("IR should contain GEP for struct field access, got:\n%s", ir)
	}
	if !strings.Contains(ir, "dot.val") {
		t.Errorf("IR should contain load of struct field value, got:\n%s", ir)
	}

	// Verify the field is loaded as %str-long (not i64)
	hasStrLongLoad := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%str-long") && strings.Contains(trimmed, "dot.val") {
			hasStrLongLoad = true
		}
	}
	if !hasStrLongLoad {
		t.Errorf("IR should load %%str-long from struct field, got:\n%s", ir)
	}

	// Verify string concatenation is performed (memcpy calls)
	if !strings.Contains(ir, "llvm.memcpy") {
		t.Errorf("IR should contain memcpy for string concatenation, got:\n%s", ir)
	}
}

// TestStructStrFieldAsArg tests D3: accessing a struct str field directly
// as a function argument should work correctly.
func TestStructStrFieldAsArg(t *testing.T) {
	src := `
app-config {
    llm-model str
    api-key str
}

print-model = (model str) {
}

test-fn = (cfg app-config) {
    ; Direct struct field access as function argument
    print-model(cfg.llm-model)
}

main = () {
    cfg app-config
    cfg.llm-model = 'gpt-4'
    cfg.api-key = 'secret'
    test-fn(cfg)
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

	// The IR should contain a load of the struct field as %str-long
	if !strings.Contains(ir, "dot.gep") {
		t.Errorf("IR should contain GEP for struct field access, got:\n%s", ir)
	}

	// Verify the argument is passed as %str-long* (pointer to temp alloca)
	hasStrLongArg := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call") && strings.Contains(trimmed, "%str-long*") {
			hasStrLongArg = true
		}
	}
	if !hasStrLongArg {
		t.Errorf("IR should pass %%str-long* as function argument for struct str field, got:\n%s", ir)
	}
}

// TestStructFieldMethodCall tests D3: calling a method directly on a struct field
// (e.g. ag.tools.build-tools-json()) should work correctly.
func TestStructFieldMethodCall(t *testing.T) {
	src := `
tool-registry {
    names [16]str
    count i64
}

tool-registry.build-tools-json = () (result str) {
    result = '[]'
}

agent {
    tools tool-registry
    initialized bool
}

agent.init = () {
    .tools.count = 0
    .initialized = true
}

test-fn = (ag agent) (result str) {
    ; Direct method call on struct field (no workaround copy to local)
    result = ag.tools.build-tools-json()
}

main = () {
    ag agent
    ag.init()
    out = test-fn(ag)
    out.len > 0 -> print-line(out)
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

	// The IR should contain a call to build-tools-json
	if !strings.Contains(ir, "call") || !strings.Contains(ir, "build-tools-json") {
		t.Errorf("IR should contain call to build-tools-json, got:\n%s", ir)
	}

	// The method call should pass the struct field pointer (not a loaded value)
	// The call should have %tool-registry* in the argument list
	hasStructPtrArg := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call") && strings.Contains(trimmed, "%tool-registry*") {
			hasStructPtrArg = true
		}
	}
	if !hasStructPtrArg {
		t.Errorf("IR should pass %%tool-registry* as self argument for method call on struct field, got:\n%s", ir)
	}
}

// TestStructStrFieldMultipleConcat tests a more complex scenario:
// multiple struct str fields used in the same concatenation chain
func TestStructStrFieldMultipleConcat(t *testing.T) {
	src := `
app-config {
    llm-model str
    api-key str
    workspace str
}

test-fn = (cfg app-config) (result str) {
    ; Multiple struct str fields in concatenation chain
    result = cfg.llm-model - ' ' - cfg.workspace - ' ' - cfg.api-key
}

main = () {
    cfg app-config
    cfg.llm-model = 'gpt-4'
    cfg.api-key = 'secret'
    cfg.workspace = '/tmp'
    out = test-fn(cfg)
    out.len > 0 -> print-line(out)
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

	// Should have multiple GEP loads for struct fields
	dotGepCount := strings.Count(ir, "dot.gep")
	if dotGepCount < 3 {
		t.Errorf("IR should have at least 3 GEP for struct field accesses, got %d, IR:\n%s", dotGepCount, ir)
	}

	// Should have multiple memcpy for concatenation
	memcpyCount := strings.Count(ir, "llvm.memcpy")
	if memcpyCount < 2 {
		t.Errorf("IR should have at least 2 memcpy for string concatenation, got %d, IR:\n%s", memcpyCount, ir)
	}
}

// TestSelfFieldMethodCall tests D3: calling a method directly on a self struct field
// (e.g. .sessions.get-or-create-idx()) should work correctly.
// This mimics the nowork/agent/agent.no pattern.
func TestSelfFieldMethodCall(t *testing.T) {
	src := `
session-manager {
    count i64
    capacity i64
}

session-manager.get-or-create-idx = (id str) (idx i64) {
    idx = 0
}

agent {
    sessions session-manager
    tools i64
    initialized bool
}

; Method on agent that calls a method on self.sessions directly
agent.process = (msg str, session-id str) (response str) {
    response = ''
    ; Direct method call on self struct field (no workaround copy to local)
    sess-idx = .sessions.get-or-create-idx(session-id)
    response = session-id
}

main = () {
    ag agent
    ag.sessions = session-manager{}
    ag.sessions.count = 0
    ag.sessions.capacity = 10
    ag.tools = 0
    ag.initialized = true
    out = ag.process('hello', 'sess1')
    out.len > 0 -> print-line(out)
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

	// The IR should contain a call to get-or-create-idx
	if !strings.Contains(ir, "call") || !strings.Contains(ir, "get-or-create-idx") {
		t.Errorf("IR should contain call to get-or-create-idx, got:\n%s", ir)
	}

	// The method call should pass the struct field pointer
	// The self argument should be %session-manager*
	hasStructPtrArg := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call") && strings.Contains(trimmed, "%session-manager*") {
			hasStructPtrArg = true
		}
	}
	if !hasStructPtrArg {
		t.Errorf("IR should pass %%session-manager* as self argument for method call on self field, got:\n%s", ir)
	}
}

// TestStructStrFieldLen tests D3: accessing .len on a struct str field
// (e.g. cfg.api-key.len) should work correctly.
func TestStructStrFieldLen(t *testing.T) {
	src := `
app-config {
    api-key str
    model str
}

test-fn = (cfg app-config) (result i64) {
    ; Direct .len access on struct str field
    result = cfg.api-key.len
}

main = () {
    cfg app-config
    cfg.api-key = 'secret-key'
    cfg.model = 'gpt-4'
    n = test-fn(cfg)
    n > 0 -> print-line('has key')
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

	// The IR should contain a GEP for the struct field
	// (dot.ptr.gep for .len access, which goes through generateExprPtr)
	if !strings.Contains(ir, "dot.gep") && !strings.Contains(ir, "dot.ptr.gep") {
		t.Errorf("IR should contain GEP for struct field access, got:\n%s", ir)
	}

	// The IR should contain a load of the str-long length (extract from str-long*)
	// This would be a GEP to field 0 of %str-long
	hasLenExtract := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "str-long") {
			hasLenExtract = true
		}
	}
	if !hasLenExtract {
		t.Errorf("IR should extract len from str-long struct field, got:\n%s", ir)
	}
}
