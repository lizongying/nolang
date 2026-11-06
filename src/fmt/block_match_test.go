package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 塊 match 是 match 主語的第三種形態（值 / 表達式 / 塊）。formatter 必須把
// `{ body }: { arms }` 原樣印回；尤其不能把 H0 展開用的合成 subject
// （`__bm_N`）或合成 match 陳述印出來。
const blockMatchCanonical = `f = (d i64) {
    {
        v ?= 6 / d
        print(v.to-str())
    }: {
        err -> print(it.msg())

        -> _
    }
}
`

func TestFormatBlockMatchRoundTrip(t *testing.T) {
	l := lexer.New(blockMatchCanonical)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	out := FormatProgram(prog, blockMatchCanonical)
	if out != blockMatchCanonical {
		t.Fatalf("block match did not round-trip:\n--- got ---\n%s\n--- want ---\n%s", out, blockMatchCanonical)
	}
	if again := FormatFile(out); again != out {
		t.Fatalf("block match formatting is not idempotent:\n%s", again)
	}
}

// H0 展開與 formatter 共用同一份糖 *Program（`no fmt` 會先跑
// checker.BuildTHIRView → parser.ASTToHIR）。展開若原地改寫 bm.Body，
// formatter 就會把合成的 `__bm_N: { … }` 當成使用者陳述印出來。
func TestFormatBlockMatchAfterH0IsUnchanged(t *testing.T) {
	l := lexer.New(blockMatchCanonical)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	parser.ASTToHIR(prog)
	out := FormatProgram(prog, blockMatchCanonical)
	if out != blockMatchCanonical {
		t.Fatalf("H0 expansion leaked into the sugar AST:\n%s", out)
	}
	if strings.Contains(out, "__bm_") {
		t.Fatalf("synthetic subject leaked into formatted source:\n%s", out)
	}
}

// 三種主語形態並存時，只有塊主語該印成 `}: {`。
func TestFormatBlockMatchKeepsOtherSubjectForms(t *testing.T) {
	src := `f = () {
    x = 1
    x: {
        1 -> print('v')

        -> _
    }
    x + 1: {
        2 -> print('e')

        -> _
    }
    {
        x + 1
    }: {
        nil -> print('b')

        -> _
    }
}
`
	if out := FormatFile(src); out != src {
		t.Fatalf("subject forms changed:\n--- got ---\n%s\n--- want ---\n%s", out, src)
	}
}
