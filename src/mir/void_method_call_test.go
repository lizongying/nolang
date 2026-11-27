package mir_test

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/build"
)

// A zero-result method still has an implicit receiver out-parameter. Calling it
// from a free function must keep both the call and its definition reachable.
func TestVoidMethodCallFromFreeFunctionResolves(t *testing.T) {
	src := `probe {
    value i64
}

probe.run = () {
    .value = 7
}

run = () {
    p probe
    p.run()
    print(p.value)
}

run()
`
	tr := build.NewTranspiler(nil)
	ll, err := tr.Compile(src)
	if err != nil {
		t.Fatalf("EmitLLVM failed (zero-result method call must resolve): %v", err)
	}
	if !strings.Contains(ll, "probe_run") {
		t.Fatalf("zero-result method definition was not emitted:\n%s", ll)
	}
}

// A constructor-initialized global keeps its result type even though its value
// cannot be folded into an LLVM constant.
func TestVoidMethodCallOnConstructorInitializedGlobalResolves(t *testing.T) {
	src := `probe {
    value i64
}

new-probe = () (out probe) {
    out = probe{value: 7}
}

probe.run = () {
    .value = 8
}

DB = new-probe()

run = () {
    DB.run()
    print(DB.value)
}

run()
`
	tr := build.NewTranspiler(nil)
	ll, err := tr.Compile(src)
	if err != nil {
		t.Fatalf("EmitLLVM failed (constructor-initialized global method call must resolve): %v", err)
	}
	if !strings.Contains(ll, "@DB = private global %probe zeroinitializer") {
		t.Fatalf("constructor-initialized global lost its struct type:\n%s", ll)
	}
	if !strings.Contains(ll, "probe_run") {
		t.Fatalf("global receiver method definition was not emitted:\n%s", ll)
	}
	if !strings.Contains(ll, "@new_probe") {
		t.Fatalf("global constructor was not executed at startup:\n%s", ll)
	}
}
