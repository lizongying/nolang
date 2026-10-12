package main

// 本檔案實作 `no fmt --fix=unwrap`：把「產生 option 卻從未消費」的綁定
// （checker trace `optunuse`，D2 下唯一的 ERROR）改寫為顯式消費形式。
//
// 修復形態（docs/design/option-unified.md §6.1 / D3）：
//
//	; 情況 A：所在函式已有 `?T` 結果參數
//	x = <expr>   =>   x ?= <expr>
//
//	; 情況 B：否則（4 行標準形；空兜底臂 = 默認零值，D4/D8）
//	x = <expr>   =>   x-opt = <expr>
//	                  x = x-opt: {
//	                      ok -> it
//	                      ->
//	                  }
//
// 與既有 fix 類同風格：手術式文字替換、不改排版、不改動其它註解、重跑冪等。
// 情況 B 刻意「只把原陳述改名」後原樣保留：其上方緊鄰的 `#{overflow=…}` /
// `#{index-out=…}` 註解因此仍作用在同一個運算上（D1：註解仍負責「失敗時怎麼
// 取值」，只是不解包），而新舊兩行都還在同一個縮排層級，排版不被打散。
//
// ⚠️ 變數名不用 §6.1 原文寫的 `__o<N>`：實測 `__` 前綴識別字在 Nolang 源碼中
// **根本不可書寫**（`__o1 = 5` 之後 `out = __o1` 直接 `expected expression,
// got nil instead`），見 §11.16。改用 `<name>-opt`（`-` 是合法識別字字元，且
// 由目標名派生、可讀），碰撞時追加序號。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// unwrapOptTraceID 是 `--fix=unwrap` 唯一認可的診斷來源：D2 之下 option 的
// 「產生處」（ovfhndld / idxhndld / ovf-int-default）都已降為 HINT，只有
// 「既沒上拋也沒解包」的 optunuse 還是 ERROR —— 也正是需要被修掉的那一批。
const unwrapOptTraceID = "optunuse"

// unwrapTempSuffix 是情況 B 暫存變數名的後綴（`<name>-opt`）。
const unwrapTempSuffix = "-opt"

// fixUnwrapFixes 收集單檔/目錄下所有 `--fix=unwrap` 的修復結果。
func fixUnwrapFixes(arg string) map[string]string {
	info, err := os.Stat(arg)
	if err != nil {
		return nil
	}
	files := []string{}
	if info.IsDir() {
		walkErr := filepath.Walk(arg, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil || fi == nil || fi.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".no") {
				files = append(files, path)
			}
			return nil
		})
		if walkErr != nil {
			return nil
		}
	} else {
		files = append(files, arg)
	}
	sort.Strings(files)
	fixes := map[string]string{}
	for _, f := range files {
		fixed, ok := fixUnwrapInFile(f)
		if !ok {
			continue
		}
		abs, aerr := filepath.Abs(f)
		if aerr != nil {
			abs = f
		}
		fixes[filepath.Clean(abs)] = fixed
	}
	return fixes
}

// unwrapSite 是一個待修站點：surface AST 裡的那條綁定，加上它所屬函式的
// option 結果參數名（情況 A 的判據）。
type unwrapSite struct {
	stmt   *parser.LetStatement
	optRes string // "" = 所在函式沒有 `?T` 結果參數
}

// fixUnwrapInFile 讀入單檔（磁碟內容），把每個被 `optunuse` 報告的綁定改寫為
// 顯式消費形式，回傳修復後源碼；無可修處則回傳 ok=false。
//
// 分析刻意走**兩次**解析：
//
//  1. 完整解析（含 lowering）→ 取得 `optunuse` 的站點行號。option 推斷與捕獲
//     展開都是 lowering 的產物，surface AST 上看不到 `?T`，也就不會有 optunuse。
//  2. surface 解析（SkipUnwrapLowering / SkipSafeIndexLowering）→ 取得使用者
//     真正寫下的那條陳述（名稱 token 的確切行列、型別標註、語句跨度、所屬函式）。
//     lowering 會把 `x = arr[i]` 換成合成區塊，靠它拿不到「使用者寫的那個名字」。
//
// 兩者的行號同源（同一份 src），故以行號 join 是安全的。
//
// 與 --fix=overflow / --fix=match 一樣刻意以「磁碟檔案自身」為分析來源（不經
// `no vet <pkg>`），避免內嵌 std / 合併模組造成行號錯位而改到錯誤的陳述。
func fixUnwrapInFile(filename string) (string, bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	src := string(data)

	// (1) 完整解析 → optunuse 站點行號。
	lp := parser.New(lexer.New(src))
	lprog := lp.ParseProgram()
	if len(lp.Errors()) > 0 || lprog == nil {
		return "", false
	}
	reportLines := map[int]bool{}
	for _, r := range checker.ValidateUnhandledOverflow(lprog, filename) {
		if r.TraceID == unwrapOptTraceID && r.Line > 0 {
			reportLines[r.Line] = true
		}
	}
	if len(reportLines) == 0 {
		return "", false
	}

	// (2) surface 解析 → 使用者寫下的陳述。
	sp := parser.New(lexer.New(src))
	sp.SkipUnwrapLowering = true
	sp.SkipSafeIndexLowering = true
	sprog := sp.ParseProgram()
	if len(sp.Errors()) > 0 || sprog == nil {
		return "", false
	}

	sites := collectUnwrapSites(sprog, reportLines)
	if len(sites) == 0 {
		return "", false
	}

	lines := strings.Split(src, "\n")
	// 已用過的識別字（保守：整份檔案出現過的 token 都算），供暫存變數名避讓。
	taken := collectIdentifierTokens(src)
	// line(1-based) -> 替換後的完整內容（可含換行）
	edits := map[int]string{}
	// 被某條多行陳述「吃進」edits[start] 的後續行號（start+1..end），
	// 主迴圈必須跳過，否則續行會被重複輸出一次。
	consumed := map[int]bool{}
	skipped := 0
	for _, site := range sites {
		s := site.stmt
		start, end := s.Pos().Line, s.EndPos().Line
		if start < 1 || end < start {
			skipped++
			continue
		}
		// EndPos().Line 有時會指到陳述之後那一行（例如換行結尾的運算式），
		// 一律收斂到最後一個有內容的行，避免把後續陳述捲進來。
		if end > len(lines) {
			end = len(lines)
		}
		if end < 1 {
			skipped++
			continue
		}
		if _, taken := edits[start]; taken {
			skipped++ // 同一行兩條陳述：只修第一條，避免互相覆蓋
			continue
		}
		first := lines[start-1]
		indent := leadingWhitespace(first)
		name := s.Name.Value
		col := s.Name.Pos().Column
		if col < 1 || col-1+len(name) > len(first) || first[col-1:col-1+len(name)] != name {
			skipped++
			continue
		}
		rest := lines[start-1 : end]
		if site.optRes != "" && s.Type == nil && name != site.optRes {
			// 情況 A：上拋。`x = <expr>` → `x ?= <expr>`，只動 `=` 那一個字元。
			eqRel := strings.Index(first[col-1+len(name):], "=")
			if eqRel < 0 {
				skipped++
				continue
			}
			eq := col - 1 + len(name) + eqRel
			out := make([]string, len(rest))
			copy(out, rest)
			out[0] = first[:eq] + "?" + first[eq:]
			edits[start] = strings.Join(out, "\n")
			continue
		}
		// 情況 B：改名 + match 解包。
		tmp := freshUnwrapName(name, taken)
		taken[tmp] = true
		out := make([]string, len(rest))
		copy(out, rest)
		out[0] = first[:col-1] + tmp + first[col-1+len(name):]
		unit := indentUnit(indent)
		block := []string{
			indent + name + " = " + tmp + ": {",
			indent + unit + "ok -> it",
			indent + unit + "->",
			indent + "}",
		}
		edits[start] = strings.Join(out, "\n") + "\n" + strings.Join(block, "\n")
	}
	if len(edits) == 0 {
		if skipped > 0 {
			fmt.Fprintf(os.Stderr, "no fmt --fix=unwrap: %s: %d site(s) need manual handling (cannot rewrite safely)\n", filename, skipped)
		}
		return "", false
	}
	// 依行號重算 consumed：對每條成功建檔的編輯，把它替換掉的陳述跨度
	// [start, end] 內的後續行標記為「已被吃掉」，主迴圈不再輸出。
	for _, site := range sites {
		s := site.stmt
		start, end := s.Pos().Line, s.EndPos().Line
		if end > len(lines) {
			end = len(lines)
		}
		if _, ok := edits[start]; !ok {
			continue
		}
		for k := start + 1; k <= end; k++ {
			consumed[k] = true
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "no fmt --fix=unwrap: %s: %d site(s) need manual handling (cannot rewrite safely)\n", filename, skipped)
	}
	outLines := make([]string, 0, len(lines)+len(edits))
	for i := 1; i <= len(lines); i++ {
		if rep, ok := edits[i]; ok {
			outLines = append(outLines, rep)
		} else if !consumed[i] {
			outLines = append(outLines, lines[i-1])
		}
	}
	return strings.Join(outLines, "\n"), true
}

// collectUnwrapSites 在 surface AST 中找出「行號被 optunuse 報告」的綁定。
// 只認 `name = <val>` 形式的 LetStatement：`_ = expr`（顯式丟棄）本就豁免、
// 合成陳述（lowering 產物）在 surface AST 裡不存在，都不該被改寫。
func collectUnwrapSites(prog *parser.Program, reportLines map[int]bool) []unwrapSite {
	var sites []unwrapSite
	var walk func(stmts []parser.Statement, optRes string)
	walk = func(stmts []parser.Statement, optRes string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch v := s.(type) {
			case *parser.LetStatement:
				if v.Name != nil && v.Value != nil && !v.IsSynthetic &&
					v.Name.Value != "_" && reportLines[v.Pos().Line] {
					sites = append(sites, unwrapSite{stmt: v, optRes: optRes})
				}
				if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements, firstOptionResult(fl.Results))
				}
			case *parser.FunctionDefinition:
				if v.Body != nil {
					walk(v.Body.Statements, firstOptionResult(v.Results))
				}
			case *parser.ForStatement:
				if v.Init != nil {
					walk([]parser.Statement{v.Init}, optRes)
				}
				if v.Update != nil {
					walk([]parser.Statement{v.Update}, optRes)
				}
				if v.Body != nil {
					walk(v.Body.Statements, optRes)
				}
			case *parser.BlockStatement:
				walk(v.Statements, optRes)
			case *parser.ExpressionStatement:
				if ie, ok := v.Expression.(*parser.IfExpression); ok {
					if ie.Consequence != nil {
						walk(ie.Consequence.Statements, optRes)
					}
					if ie.Alternative != nil {
						walk(ie.Alternative.Statements, optRes)
					}
				}
			}
		}
	}
	walk(prog.Statements, "")
	return sites
}

// firstOptionResult 回傳結果參數列表中第一個 `?T` 的名稱（"" = 沒有）。
// `?=` 上拋的落點正是這個參數（見 lowering.go lowerUnwrapAssign）。
func firstOptionResult(results []*parser.Parameter) string {
	for _, r := range results {
		if r == nil || r.Type == nil {
			continue
		}
		if strings.HasPrefix(r.Type.String(), "?") {
			return r.Name
		}
	}
	return ""
}

// freshUnwrapName 由目標名派生一個檔案內未出現過的暫存變數名：`x` → `x-opt`，
// 碰撞則 `x-opt2`、`x-opt3`……
func freshUnwrapName(name string, taken map[string]bool) string {
	base := name + unwrapTempSuffix
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
}

// collectIdentifierTokens 保守地收集整份源碼裡出現過的所有識別字狀 token，
// 供 freshUnwrapName 避讓。切成 token 而非 substring 比對，避免 `abc` 命中
// `xabc` 這類誤判；分隔符集合刻意保守（非識別字字元一律切開）。
func collectIdentifierTokens(src string) map[string]bool {
	taken := map[string]bool{}
	start := -1
	for i := 0; i <= len(src); i++ {
		isIdent := false
		if i < len(src) {
			c := src[i]
			isIdent = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '_' || c == '-'
		}
		if isIdent {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			taken[src[start:i]] = true
			start = -1
		}
	}
	return taken
}

// indentUnit 依既有縮排風格回傳一個縮排單位：檔案用 tab 就補 tab，否則補 4 空格。
func indentUnit(indent string) string {
	if strings.ContainsRune(indent, '\t') {
		return "\t"
	}
	return "    "
}
