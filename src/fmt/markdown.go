package fmt

import (
	"regexp"
	"strings"
)

// Markdown 柵欄程式碼塊支援。
//
// 本文件讓 `no fmt` 除 `.no` 源碼之外，也能處理 Markdown /
// MDX 文件中的 Nolang 程式碼片段——即 ```no / ```nolang 柵欄程式碼塊
// （fenced code block）。文件站（docs/）的範例程式同樣要符合 `no fmt` 的排版
// 權威，否則「文檔裡的範例」與「實際提交的代碼」會慢慢走樣。
//
// 三條不可妥協的邊界：
//  1. **只動柵欄內部**。柵欄行本身、以及柵欄外的每一行（正文、標題、表格、
//     列表）逐字保留，永不重排；未標語言、或標 `bash` / `noml` 等非 Nolang
//     語言的塊一律不碰（`noml` 是套件配置語言，不能按 `.no` 語法格式化）。
//  2. **解析失敗絕不修改**。文檔裡大量片段是示意偽碼（佔位符、跨行簡寫、
//     故意寫錯的反例），無法解析的塊原樣留底，只回報「已跳過」，讓
//     `no fmt` 對整個 docs/ 樹永遠可冪等執行而不會寫壞文件。
//  3. **縮排跟著柵欄走**。列表項內的縮排柵欄（兩個空格的 ```no）其內容同樣
//     縮排；格式化前剝掉共同縮排、格式化後原樣貼回，輸出塊的縮排與原文件一致。

// mdNolangLangs 是柵欄 info string 中被視為 Nolang 程式碼的語言標記。
// `nolang` 是早期寫法，文件站兩種都有，故兩者等價處理。
var mdNolangLangs = map[string]bool{"no": true, "nolang": true}

// 跳過標記有三種寫法，作者可任選：
//   - 塊頂部註解：#{fmt-skip} —— **推薦**。用 Nolang 自己的註解語法，寫在柵欄內
//     第一行；它標明「這段是刻意寫的示例，別碰」，語義自解釋，也不依賴渲染器
//     如何處置柵欄 meta；
//   - 柵欄 meta：```no skip —— 渲染器只看首詞當語言，其餘 meta 不顯示；
//   - 塊內註解：; nolang-fmt:skip —— 早期寫法，完全不依賴渲染器對 meta 的態度。
//
// 三者的效果完全相同：整塊既不格式化、也不驗證，逐字節原樣保留。
const (
	mdSkipAnnotation = "fmt-skip"        // 塊頂部註解項：#{fmt-skip}
	mdSkipToken      = "skip"            // 柵欄 meta 首選詞
	mdSkipTokenAlt   = "nofmt"           // 柵欄 meta 別名
	mdSkipComment    = "nolang-fmt:skip" // 塊內行註解（早期寫法）
	mdSkipCommentAt  = ";"               // Nolang 行註解標記
	mdAnnotationOpen = "#{"              // Nolang 附着註解起始
	mdAnnotationEnd  = "}"
)

// mdFenceRe 匹配柵欄行：捕組 1 = 行首縮排、2 = 柵欄字元串（``` 或 ~~~）、
// 3 = info string（語言 + 其餘元數據）。
// 用雙引號字面量而非 raw string：pattern 本身要匹配反引號。
var mdFenceRe = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})[ \t]*(.*)$")

// mdCloseRe 匹配結尾柵欄：只有縮排 + 同種字元（數量不少於起始柵欄）+ 行尾空白。
var mdCloseRe = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})[ \t]*$")

// MdSkippedBlock 記錄一個未能格式化的 Nolang 代碼塊，供上層回報作者。
type MdSkippedBlock struct {
	// Line 是該塊起始柵欄行的 1-based 行號。
	Line int
	// Reason 是跳過原因（解析失敗時為第一條 parser error 訊息）。
	Reason string
}

// IsMarkdownPath 報告路徑是否為 `no fmt` 應按 Markdown 規則處理的文件
// （`.md` / `.mdx`，大小寫不敏感）。Markdown 不是 Nolang 源碼，其檔名
// 不受源碼命名規範（小寫 + 中劃線）約束，上層據此豁免路徑命名檢查。
func IsMarkdownPath(path string) bool {
	switch strings.ToLower(pathExt(path)) {
	case ".md", ".mdx":
		return true
	}
	return false
}

// pathExt 回傳小寫擴展名（含點；無擴展名回傳空字串）。
func pathExt(path string) string {
	base := path
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i:]
	}
	return ""
}

// FormatMarkdownFile 格式化 Markdown / MDX 源碼中所有 ```no / ```nolang
// 代碼塊，回傳結果與被跳過的塊清單（預設循環拼寫風格）。
func FormatMarkdownFile(source string) (string, []MdSkippedBlock) {
	return FormatMarkdownFileWithLoopStyle(source, LoopStylePrefix)
}

// FormatMarkdownFileWithLoopStyle 格式化源碼中所有 Nolang 代碼塊。
//
// 呼叫方可以完全信任的契約：
//   - 柵欄外的每一行、非 Nolang 塊、標了 skip 的塊、以及解析失敗的塊，
//     在輸出中與 source 逐字節相同；
//   - skipped 只含「未標 skip 且解析失敗」的塊，供呼叫方提示作者補標記；
//   - 冪等：對同一份源碼再跑一次，輸出不再變化，skipped 亦相同。
func FormatMarkdownFileWithLoopStyle(source string, style LoopStyle) (string, []MdSkippedBlock) {
	lines := strings.Split(source, "\n")
	var skipped []MdSkippedBlock
	// delta 是「已寫回的行數增減」累計。格式化會改變塊的長度，而後續塊的
	// 掃描位置 i 是**當前** lines 的下標；回報給作者的行號必須是**原文件**
	// 的行號，否則作者照著行號找不到那個塊（早期就踩過：報告 1040 行，
	// 實際柵欄在 1024）。delta 讓兩者互不干擾。
	delta := 0

	for i := 0; i < len(lines); i++ {
		open := mdFenceRe.FindStringSubmatch(lines[i])
		if open == nil {
			continue
		}
		fenceChar := open[2][0]
		info := strings.TrimSpace(open[3])
		// 反引號柵欄的 info string 不得再含反引號（含者不是合法柵欄行）。
		if strings.ContainsRune(info, rune(fenceChar)) {
			continue
		}
		end := mdFenceEnd(lines, i+1, fenceChar, len(open[2]))
		if end < 0 {
			// 直到文件尾都沒有結尾柵欄：其後內容語意不明，整體原樣保留。
			break
		}
		body := lines[i+1 : end]
		if !mdIsNolangFence(info) || mdMarkedSkip(info, body) {
			i = end
			continue
		}
		// 空塊（```no 緊接 ```）沒有可格式化的內容：原樣留底，且**不記為
		// skipped** —— skipped 的語義是「這裡有東西我想格式化但做不到，請作者
		// 處理」，空塊沒有需要作者處理的東西，回報只會製造噪音。
		if strings.TrimSpace(strings.Join(body, "\n")) == "" {
			i = end
			continue
		}
		out, reason, ok := formatMdBlock(strings.Join(body, "\n"), style, commonIndent(body))
		if !ok {
			// 報告原文件行號：i 是當前緩衝區下標，delta 抹掉先前塊的長度變化。
			skipped = append(skipped, MdSkippedBlock{Line: i + 1 - delta, Reason: reason})
			i = end
			continue
		}
		// 只替換內容行：起始與結尾柵欄行、其餘文件內容完全不動。
		replaced := end - (i + 1)
		lines = spliceLines(lines, i+1, end, out)
		delta += len(out) - replaced
		// 跳過剛寫入的內容行**與結尾柵欄**：結尾柵欄在語法上同樣符合起始柵欄
		// 的正則，若停在它上面，下一輪會把它當成新塊的開始，於是它與下一個
		// 真正的起始柵欄配對，中間那個 Nolang 塊就被當成「非 Nolang 塊的內容」
		// 整段跳過 —— 一份文件裡只有每隔一個塊會被格式化。
		i += len(out) + 1
	}
	return strings.Join(lines, "\n"), skipped
}

// mdFenceEnd 自 from 起找同種且不短於 fenceLen 的結尾柵欄行，回傳其索引；
// 找不到回傳 -1。
func mdFenceEnd(lines []string, from int, fenceChar byte, fenceLen int) int {
	for j := from; j < len(lines); j++ {
		c := mdCloseRe.FindStringSubmatch(lines[j])
		if c == nil || c[2][0] != fenceChar || len(c[2]) < fenceLen {
			continue
		}
		return j
	}
	return -1
}

// mdIsNolangFence 報告 info string 的首詞是否 Nolang 語言標記。
func mdIsNolangFence(info string) bool {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return false
	}
	return mdNolangLangs[strings.ToLower(fields[0])]
}

// mdMarkedSkip 報告該塊是否被作者顯式標記為不格式化：塊頂部 `#{fmt-skip}`
// 註解、柵欄 meta 含 skip / nofmt、或塊內存在一行 `; ...nolang-fmt:skip...`
// 註解。三種寫法等效，第一種是推薦寫法。
func mdMarkedSkip(info string, body []string) bool {
	fields := strings.Fields(info)
	for _, tok := range fields[1:] {
		switch strings.ToLower(tok) {
		case mdSkipToken, mdSkipTokenAlt:
			return true
		}
	}
	if mdHasSkipAnnotation(body) {
		return true
	}
	for _, l := range body {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, mdSkipCommentAt) && strings.Contains(t, mdSkipComment) {
			return true
		}
	}
	return false
}

// mdHasSkipAnnotation 報告塊頂部（第一個非空行）是否為 `#{fmt-skip}` 註解。
//
// 只認頂部：註解是給「整塊」的指令，寫在中間會讓「從哪一行開始不格式化」變得
// 有歧義，而 no fmt 的契約是整塊原子保留。同一行的多個註解項以逗號分隔，
// 例如 `#{fmt-skip, overflow=wrap}`，任一項為 fmt-skip 即命。比對不分大小寫。
func mdHasSkipAnnotation(body []string) bool {
	for _, l := range body {
		t := strings.TrimSpace(l)
		if t == "" {
			continue // 跳過柵欄後的空白行，註解仍算「頂部」
		}
		if !strings.HasPrefix(t, mdAnnotationOpen) || !strings.HasSuffix(t, mdAnnotationEnd) {
			return false
		}
		inner := t[len(mdAnnotationOpen) : len(t)-len(mdAnnotationEnd)]
		for _, item := range strings.Split(inner, ",") {
			if strings.EqualFold(strings.TrimSpace(item), mdSkipAnnotation) {
				return true
			}
		}
		return false
	}
	return false
}

// commonIndent 回傳內容行的共同縮排前綴（全空白行忽略）。塊內若没有任何
// 非空白行，回傳空字串。前綴取各行縮排的最長公共前綴，因此代碼自身的
// 層級縮排會被保留（只剝掉柵欄帶來的基礎縮排）。
func commonIndent(body []string) string {
	common := ""
	first := true
	for _, l := range body {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if first {
			common, first = ind, false
			continue
		}
		n := 0
		for n < len(common) && n < len(ind) && common[n] == ind[n] {
			n++
		}
		common = common[:n]
	}
	return common
}

// formatMdBlock 格式化一個代碼塊的內容，回傳帶回縮排的輸出行。
// indentTo 是剝掉並稍後貼回的共同縮排前綴。解析失敗時 ok=false 且 reason
// 為第一條 parser error，呼叫方必須原樣保留該塊。
func formatMdBlock(body string, style LoopStyle, indentTo string) (out []string, reason string, ok bool) {
	if strings.TrimSpace(body) == "" {
		return nil, "", false
	}
	// 剝掉共同縮排，讓 formatter 從 0 縮排起步（formatter 不認識「整塊多 N 格」）。
	dedented := dedentLines(strings.Split(body, "\n"), indentTo)
	formatted, fmtOK, errs := formatProgramWithLoopStyle(strings.Join(dedented, "\n"), style)
	if !fmtOK {
		if len(errs) == 0 {
			// 解析成功但**一條陳述都沒有**：整塊只有註解（文檔裡很常見的
			// 「把正確寫法註起來」示例）。formatProgramAST 把「無陳述」與
			// 「解析失敗」歸成同一種 ok=false，於是這裡會被報成
			// 「not parseable as Nolang」——那不是真的，會逼作者去加
			// #{fmt-skip} 掩蓋一個並不存在的語法錯誤。這種塊沒有任何可排版
			// 的程式碼，原樣留底且不計入 skipped。
			return strings.Split(body, "\n"), "", true
		}
		reason = errs[0]
		return nil, reason, false
	}
	formatted = strings.TrimRight(formatted, "\n")
	for _, l := range strings.Split(formatted, "\n") {
		if strings.TrimSpace(l) == "" {
			// 空行不貼回縮排，避免「縮排殭屍行」在 diff 裡製造噪音。
			out = append(out, "")
			continue
		}
		out = append(out, indentTo+l)
	}
	return out, "", true
}

// dedentLines 剝掉每一行的 indentTo 前綴（不具該前綴的非空白行原樣保留，
// 全空白行歸零）。indentTo 為空時是恆等變換。
func dedentLines(lines []string, indentTo string) []string {
	if indentTo == "" {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case strings.TrimSpace(l) == "":
			out = append(out, "")
		case strings.HasPrefix(l, indentTo):
			out = append(out, l[len(indentTo):])
		default:
			out = append(out, l)
		}
	}
	return out
}

// spliceLines 以 repl 取代 lines 的 [start, end) 區間，回傳新切片。
func spliceLines(lines []string, start, end int, repl []string) []string {
	out := make([]string, 0, len(lines)-(end-start)+len(repl))
	out = append(out, lines[:start]...)
	out = append(out, repl...)
	out = append(out, lines[end:]...)
	return out
}
