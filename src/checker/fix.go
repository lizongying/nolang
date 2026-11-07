package checker

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// fixSnippet 構造一條修復（預設 span = 診斷自身位置）。
func fixSnippet(snippet, desc string) parser.DiagnosticFix {
	return parser.DiagnosticFix{Snippet: snippet, Description: desc}
}

// fixSnippetSpan 構造帶顯式替換 span 的修復（1-based 行列，與診斷位置口徑一致）。
func fixSnippetSpan(snippet, desc string, sl, sc, el, ec int) parser.DiagnosticFix {
	return parser.DiagnosticFix{Snippet: snippet, Description: desc,
		StartLine: sl, StartCol: sc, EndLine: el, EndCol: ec}
}

// narrowingFixes 把窄化提示（narrowingHint 回傳的 "; hint: ..."）轉為結構化 Fix 的
// Description，使 Message 保持乾淨、修復建議統一到 Fixes 欄位。無可套用窄化提示時回傳 nil。
func narrowingFixes(fromType, toType string) []parser.DiagnosticFix {
	h := narrowingHint(fromType, toType)
	if h == "" {
		return nil
	}
	h = strings.TrimPrefix(h, "; hint: ")
	return []parser.DiagnosticFix{{Description: h}}
}
