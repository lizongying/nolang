package parser

// DiagnosticFix 携带一條診斷的可套用修復。Snippet 是 Nolang 源片段，語言中立，
// 絕不經 i18n 翻譯（譯者會把程式碼本地化）。Description 給用戶的簡短說明；
// 當 Snippet 為空時，Description 作為純文字提示（如 "cross-module calls must be
// module-qualified"）。
//
// 放在 parser 包：它是依賴葉子（僅 import hir / i18n），而 checker / build / lsp /
// cmd 全部 import parser，故無循環依賴。
type DiagnosticFix struct {
	// Snippet 是要替換進源碼的 Nolang 片段（不被翻譯）。可多行。
	Snippet string `json:"snippet,omitempty"`
	// Description 給用戶的簡短說明；可含一小段 i18n 標籤（如 "建議修復:"）+ 上下文。
	// 當 Snippet 為空時，Description 作為純文字提示。
	Description string `json:"description,omitempty"`
	// File 套用目標文件；空則回退到診斷所屬文件。
	File string `json:"file,omitempty"`
	// 要被替換掉的源 span（1-based 行列，與診斷位置口徑一致）。
	// 預設 = 診斷自身位置 span（Start=Pos/Line-Column，End=End 或 Pos）。
	StartLine int `json:"start_line,omitempty"`
	StartCol  int `json:"start_col,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
	EndCol    int `json:"end_col,omitempty"`
}

// WithSpan 設替換 span（返回自身，便於鏈式/字面量構造）。
func (f DiagnosticFix) WithSpan(sl, sc, el, ec int) DiagnosticFix {
	f.StartLine, f.StartCol, f.EndLine, f.EndCol = sl, sc, el, ec
	return f
}

// DefaultSpanFrom 用診斷位置填充預設 span。
func (f DiagnosticFix) DefaultSpanFrom(line, col, endCol int) DiagnosticFix {
	if f.StartLine == 0 {
		f.StartLine, f.StartCol = line, col
		f.EndLine = line
		if f.EndCol == 0 {
			if endCol > col {
				f.EndCol = endCol
			} else {
				f.EndCol = col + 1
			}
		}
	}
	return f
}
