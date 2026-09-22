package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// `#{overflow = wrap}` 是「行注解」，印在陳述上方獨立一行。當它落在 match arm
// 之前時（arm 條件本身含整數運算，或 arm 本體是單一裸 match），內聯守衛必須
// 阻止 formatter 把 arm 本體內聯成 `cond -> #{overflow=wrap}`。
//
// 該形式無法被解析器還原：`#{...}` 會被當成 arm 本體，二次格式化於是產生
// `overflow = wrap` 之類的錯亂（真實案例：src/std/toml.no 讓 TestFormatIdempotent
// 失敗）。根因是內聯守衛誤用了「排除 overflow」的 attachedAnnotationsWillEmit。
// 本測試鎖死：輸出可再解析、冪等，且不得出現 `-> #{` 內聯形式。
func TestFormatOverflowAnnotationOnArmStaysParseable(t *testing.T) {
	cases := map[string]string{
		// arm 本體是單一裸 match：isBareMatchBody 讓 canInline 為真，
		// 若守衛忽略 overflow 就會把註解內聯到 `-> ` 之後（toml.no 缺陷形狀）。
		"nestedBareMatchArm": "f = (n i64, i i64) (r i64) {\n" +
			"    {\n" +
			"        n == 1 -> r = i + 1\n" +
			"\n" +
			"        #{overflow=wrap}\n" +
			"        n == 3 -> {\n" +
			"            #{overflow=wrap}\n" +
			"            i + 2 < n -> {\n" +
			"                r = i + 1\n" +
			"            }\n" +
			"        }\n" +
			"\n" +
			"        -> r = 0\n" +
			"    }\n" +
			"}\n",
		// 註解落在「非首個」arm 之前，且 arm 本體原本是單行內聯。
		"annotationBeforeNonFirstArm": "g = (n i64, i i64) (r i64) {\n" +
			"    {\n" +
			"        n == 1 -> r = i + 1\n" +
			"\n" +
			"        #{overflow=wrap}\n" +
			"        n == 3 -> r = i + 2\n" +
			"\n" +
			"        -> r = 0\n" +
			"    }\n" +
			"}\n",
	}
	for name, src := range cases {
		name, src := name, src
		t.Run(name, func(t *testing.T) {
			first := FormatFile(src)

			// 1) 輸出必須可再解析（冪等的前提，也是這個 bug 的直接症狀）。
			p := parser.New(lexer.New(first))
			p.SkipUnwrapLowering = true
			p.SkipSafeIndexLowering = true
			p.ParseProgram()
			if perrs := p.Errors(); len(perrs) > 0 {
				t.Fatalf("formatted output no longer parses: %v\n--- output ---\n%s", perrs, first)
			}

			// 2) 註解不得被內聯到 `->` 之後（無法還原的形式）。
			if strings.Contains(first, "-> #{") {
				t.Fatalf("overflow annotation inlined after `->` (unparseable form):\n%s", first)
			}

			// 3) 註解不得消失。
			if !strings.Contains(first, "overflow") {
				t.Fatalf("overflow annotation vanished:\n--- in ---\n%s\n--- out ---\n%s", src, first)
			}

			// 4) 冪等。
			if second := FormatFile(first); second != first {
				t.Fatalf("formatter not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
			}
		})
	}
}
