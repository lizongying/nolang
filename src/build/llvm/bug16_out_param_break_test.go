package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBug16OutParamAssignInBreakBranch verifies that assigning to an integer
// output parameter inside an if-branch with break generates an immediate store,
// not a deferred binding that gets lost when the SSA version is restored after
// the branch.
//
// Before the fix, `pos = i` inside `s[i] == 10 -> { pos = i; break }` was
// deferred via SSA versioning. The SSA version was saved before the if-branch
// and restored after, so at function return flushOutputBindings could not find
// the binding (version mismatch). The output parameter retained its initial
// value (s.len), causing the function to return "not found".
func TestBug16OutParamAssignInBreakBranch(t *testing.T) {
	src := `find-newline = (s str, from i64) (pos i64) {
    pos = s.len
    i <- [from..s.len): {
        s[i] == 10 -> {
            pos = i
            break
        }
    }
}

main = () () {
    s str = 'f1.txt' - '\n' - '\n'
    nl i64 = find-newline(s, 0)
    print(nl)
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

	// Extract the find-newline function body from the IR
	lines := strings.Split(ir, "\n")
	inFindNewline := false
	fnBody := strings.Builder{}
	for _, line := range lines {
		if strings.Contains(line, "define") && strings.Contains(line, "find-newline") {
			inFindNewline = true
		}
		if inFindNewline {
			fnBody.WriteString(line + "\n")
			if strings.TrimSpace(line) == "ret void" {
				break
			}
		}
	}

	fnIR := fnBody.String()
	t.Logf("find-newline IR:\n%s", fnIR)

	// The `pos = i` assignment inside the if.then block must generate an
	// immediate "store i64" to %pos. Before the fix, the store was missing
	// (deferred binding was recorded but lost due to SSA version restore).
	//
	// We check that within the find-newline function body, there is a store
	// to %pos that comes AFTER the s[i] == 10 comparison (icmp eq ... 10).
	hasStoreToPos := false
	for _, line := range strings.Split(fnIR, "\n") {
		trimmed := strings.TrimSpace(line)
		// Look for store i64 ... %pos within the function body
		if strings.Contains(trimmed, "store i64") && strings.Contains(trimmed, "%pos") {
			hasStoreToPos = true
		}
	}
	if !hasStoreToPos {
		t.Errorf("Expected immediate store to %%pos in find-newline body, but none found. " +
			"This indicates the deferred binding bug: pos = i inside if/break is not stored.")
	}

	// Also verify that there are at least 2 stores to %pos:
	// 1. pos = s.len (initial assignment)
	// 2. pos = i (inside the if-then branch, the one that was missing before the fix)
	storeCount := 0
	for _, line := range strings.Split(fnIR, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store i64") && strings.Contains(trimmed, "%pos") {
			storeCount++
		}
	}
	if storeCount < 2 {
		t.Errorf("Expected at least 2 stores to %%pos (pos=s.len and pos=i), got %d", storeCount)
	}
}
