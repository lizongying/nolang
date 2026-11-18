package mir

import (
	"regexp"
	"strings"
	"testing"
)

// TestTaskDepthGuard pins design/nolang-coro.md §9 gap #4. Coroutine frames are
// heap blocks, so recursive spawning has no native stack to overflow — it just
// builds an unbounded chain of live tasks. Both spawn emitters must inherit the
// running task's logical depth from @nolang_current_task, check it BEFORE the
// allocation, and abort with a diagnostic when the limit is crossed.
func TestTaskDepthGuard(t *testing.T) {
	mod := lowerForTest(t, coroChanSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, want := range []string{
		"%task = type { void (i8*)*, i64, i1, i1, i8*, i8*, i8*, i8*, i32, i32, i8* }",
		"@nolang_current_task = global i8* null",
		"load i8*, i8** @nolang_current_task",
		"icmp sgt i32",
		", 1024",
		"call void @nolang_depth_abort()",
		"define void @nolang_depth_abort()",
		"@nolang_depth_msg = private unnamed_addr constant",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("IR does not contain %q (depth guard missing)", want)
		}
	}
	// Checking only the abort call would pass even if every task inherited
	// depth 0 (no growth). Pin the parent-depth LOAD from field 8 and the
	// child-depth STORE into field 8 in both spawn emitters.
	pats := []string{
		`%sd\.depth\.\d+ = phi i32 \[ 1, %sd\.root\.\d+ \], \[ %sd\.d\.\d+, %sd\.nest\.\d+ \]`,
		`getelementptr inbounds %task, %task\* %sd\.t\.\d+, i32 0, i32 8`,
		`store i32 %sd\.depth\.\d+, i32\* %arun\.tf8\.\d+`,
		`store i32 %sd\.depth\.\d+, i32\* %cr\.fdepth\.\d+`,
	}
	for _, p := range pats {
		if !regexp.MustCompile(p).MatchString(ir) {
			t.Errorf("IR does not match %s", p)
		}
	}
}
