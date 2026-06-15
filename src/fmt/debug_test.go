package fmt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestDebugFunc7(t *testing.T) {
	input := strings.TrimSpace(`
// aes-key-expand: 將 16-byte 金鑰展開為 176-byte 輪金鑰
// ek: 輸出輪金鑰字串（176 位元組）
aes-key-expand = (key str, ek str) {
    // 複製原始金鑰（前 16 位元組）
    i = 0
    for i < 16 {
        ek[i] = key[i]
        i = i + 1
    }

    // 產生 w[4..43]（共 44 個 32-bit 字 = 176 位元組）
    i = 4
    for i < 44 {
        // 讀取前一個字
        off = (i - 1) * 4
        w = (ek[off] << 24) | (ek[off + 1] << 16) | (ek[off + 2] << 8) | ek[off + 3]
        if i % 4 == 0 {
            rot-word(w, rw)
            sub-word(rw, sw)
            rcon-val(i / 4, rc)
            w = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w ^ sw ^ (rc << 24)) & 4294967295
        } else {
            w-prev4 = (ek[(i-4) * 4] << 24) | (ek[(i-4) * 4 + 1] << 16) | (ek[(i-4) * 4 + 2] << 8) | ek[(i-4) * 4 + 3]
            w = (w-prev4 ^ w) & 4294967295
        }
    }
    ek[i * 4] = (w >> 24) & 255
    ek[i * 4 + 1] = (w >> 16) & 255
    ek[i * 4 + 2] = (w >> 8) & 255
    ek[i * 4 + 3] = w & 255
    i = i + 1
}
`)
	cleanLines := strings.Split(input+"\n", "\n")
	cleanCode, _, lineTypes := stripAndClassify(input)
	l := lexer.New(cleanCode)
	p := parser.New(l)
	program := p.ParseProgram()

	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements")
	}

	f := &formatter{
		sourceLines: cleanLines,
		lineTypes:   lineTypes,
	}

	// Format to get stmtLineCnt and stmtOrigLine
	f.formatProgram(program)

	fmt.Printf("Total top-level statements: %d\n", len(program.Statements))
	for i, stmt := range program.Statements {
		line := stmtTokenLine(stmt)
		fmt.Printf("  [%d] line=%d type=%T\n", i, line, stmt)
	}
	fmt.Printf("stmtLineCnt: %v\n", f.stmtLineCnt)
	fmt.Printf("stmtOrigLine: %v (1-based)\n", f.stmtOrigLine)

	// Print function definition body
	if len(program.Statements) > 0 {
		if fd, ok := program.Statements[0].(*parser.FunctionDefinition); ok {
			fmt.Printf("\nFunction body has %d statements:\n", len(fd.Body.Statements))
			for i, bs := range fd.Body.Statements {
				line := stmtTokenLine(bs)
				fmt.Printf("  [%d] line=%d type=%T\n", i, line, bs)
			}
		}
	}

	// formatted output
	formattedCode := f.buf.String()
	formattedLines := strings.Split(formattedCode, "\n")
	fmt.Printf("\nFormatted output (%d lines):\n", len(formattedLines))
	for i, line := range formattedLines {
		if strings.TrimSpace(line) == "" {
			fmt.Printf("  [%2d] %q\n", i, "")
		} else {
			fmt.Printf("  [%2d] %s\n", i, line)
		}
	}

	// Build stmtStartLines
	stmtStartLines := make(map[int]bool)
	for _, l := range f.stmtOrigLine {
		if l > 0 {
			stmtStartLines[l-1] = true
		}
	}
	fmt.Printf("\nstmtStartLines (0-based):\n")
	for _, l := range f.stmtOrigLine {
		if l > 0 {
			fmt.Printf("  line %d: type=%s content=%q\n", l-1, lineTypeStr(lineTypes[l-1]), cleanLines[l-1])
		}
	}

	fmt.Printf("\nAll lines:\n")
	for i, lt := range lineTypes {
		marker := "  "
		if stmtStartLines[i] {
			marker = "S>"
		}
		ltStr := "PR" // preserved
		if lt == lineCode {
			ltStr = "CD" // code
		}
		if strings.TrimSpace(cleanLines[i]) == "" {
			fmt.Printf("%s [%2d] %s %q\n", marker, i, ltStr, "")
		} else {
			fmt.Printf("%s [%2d] %s %q\n", marker, i, ltStr, cleanLines[i])
		}
	}
}

func lineTypeStr(lt lineType) string {
	if lt == lineCode {
		return "code"
	}
	return "preserved"
}
