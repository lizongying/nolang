package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 迴歸釘：`#{overflow = wrap}` 行註解在「安全索引寫入」被 desugar 之後仍須有效。
//
// 缺陷（已修）：安全索引 desugar（`arr[base+i] = v` → `__tmp = arr[...]` + match）
// 會以全新節點取代原陳述，而行註解綁在原節點上、未被帶到取代節點。於是
// ValidateIntOverflow（跑在 lowering **之後**的程式上，見 build/transpiler.go
// 對 merged 呼叫 RunAllLints）把已標註的索引算式 `base + i` 當成未處理整數運算，
// 繼續報 ovf-int-default —— 即使原始碼上方明明寫了 `#{overflow=wrap}`。
//
// 修法兩處：
//   - parser.(*lowerer).carryOverflowAnnotation 把 overflow 條目轉掛到取代節點；
//   - parser.applyLineOverflowAnnotations 不再因為中間夾著無關的 `#{index-out=0}`
//     就中斷傳播（std 的 `#{overflow=wrap}` + `#{index-out=0}` 組合正是這種情形）。
//
// 兩種陳述形狀都要覆蓋：識別符索引（`out[8+i]`，parser 直接把註解附加到陳述）
// 與點選欄位索引（`.buf[.pos+i]`，開頭是 DOT，註解只能退化成獨立註解行、
// 模式只寫進 OverflowMode 欄位）。
func TestOverflowAnnotationSurvivesIndexDesugar(t *testing.T) {
	const identShape = `probe = (data []byte, out []byte) {
    i <- [0..4): {
        #{overflow=wrap}
        #{index-out = 0}
        out[8 + i] = data[i]
    }
}
`
	const fieldShape = `box {
    buf []byte
    pos i64
}

box.fill = (data []byte) {
    i <- [0..4): {
        #{overflow=wrap}
        #{index-out = 0}
        .buf[.pos + i] = data[i]
    }
}
`
	lintCount := func(src string) int {
		p := parser.New(lexer.New(src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors: %v", errs)
		}
		return len(ValidateIntOverflow(prog))
	}
	cases := []struct{ name, src string }{
		{"identShape", identShape},
		{"fieldShape", fieldShape},
	}
	// 逐一檢查、不用 Fatalf 提前中斷：兩種形狀都要被實際驗到，
	// 否則 map 迭代順序會讓其中一種在失敗時永遠沒被執行。
	for _, c := range cases {
		// 對照組：拿掉 overflow 註解就必須被報告。少了這一步，測試可能因為
		// 根本掃不到算式而「假通過」。
		plain := strings.Replace(c.src, "        #{overflow=wrap}\n", "", 1)
		if plain == c.src {
			t.Errorf("%s: fixture is stale — overflow annotation line not found", c.name)
			continue
		}
		if got := lintCount(plain); got == 0 {
			t.Errorf("%s: sanity failed — unannotated indexed arithmetic was not reported", c.name)
		}
		if got := lintCount(c.src); got != 0 {
			t.Errorf("%s: annotated indexed arithmetic still reported %d time(s)", c.name, got)
		}
	}
}
