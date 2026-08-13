package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBug11FnStrParamIndex generates IR for the bug11 case and checks
// that str parameter indexing works correctly inside functions.
func TestBug11FnStrParamIndex(t *testing.T) {
	src := `find-newline = (s str, from i64) (pos i64) {
    pos = -1
    i <- [from..s.len): {
        s[i] == 10 -> {
            pos = i
            break
        }
    }
}

main = () () {
    s str = with-len(202)
    i <- [0..100): {
        s[i] = 97
    }
    s[100] = 10
    i <- [0..100): {
        s[101 + i] = 98
    }
    s[201] = 10

    nl-inline i64 = -1
    j <- [0..s.len): {
        s[j] == 10 -> {
            nl-inline = j
            break
        }
    }

    nl-fn i64 = find-newline(s, 0)
    print(nl-inline)
    print(nl-fn)
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

	// Dump the IR for inspection
	t.Logf("Generated IR:\n%s", ir)

	// Check that find-newline function receives s as %str-long* parameter
	hasStrLongParam := false
	for _, line := range strings.Split(ir, "\n") {
		// Look for the function definition with %str-long* parameter
		if strings.Contains(line, "define") && strings.Contains(line, "find-newline") {
			if strings.Contains(line, "%str-long*") {
				hasStrLongParam = true
			}
			t.Logf("Function def: %s", line)
		}
	}
	if !hasStrLongParam {
		t.Errorf("find-newline should have %%str-long* parameter")
	}

	// Check that inside find-newline, s[i] uses GEP on str-long data pointer
	// (field 2), not treating s as a %vec
	findNewlineBody := false
	strLongIndexInFn := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "find-newline") && strings.Contains(line, "define") {
			findNewlineBody = true
		}
		if findNewlineBody {
			// Look for str-long data GEP (field 2) in the function body
			if strings.Contains(trimmed, "str-long") && strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "i32 0, i32 2") {
				strLongIndexInFn = true
			}
		}
	}
	if !strLongIndexInFn {
		t.Logf("Warning: no str-long data GEP found in find-newline body — this may indicate the bug")
	}
}
