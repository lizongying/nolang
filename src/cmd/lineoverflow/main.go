// Command lineoverflow materializes block-scoped `#{overflow = <mode>}`
// annotations into per-statement (line) annotations.
//
// Background: `#{overflow = <mode>}` used to be BLOCK-scoped — a single
// annotation enabled the mode for every statement of its enclosing block
// (bidirectionally, and recursively into nested blocks/loops). The formatter
// relied on that to de-duplicate the repeated per-line annotations, so a
// `no fmt` pass silently deleted thousands of annotations; worse, any mistake in
// the block-scope propagation silently changed integer semantics (an
// un-annotated op returns option<int> instead of wrapping — no compile error,
// just wrong behaviour).
//
// `#{overflow = <mode>}` is now a strict LINE annotation: it applies only to the
// statement on the next line. Function-level annotations (a `#{overflow}` line
// directly above a definition) are no longer honoured either. This tool rewrites
// existing sources so every statement which previously inherited its mode from
// the enclosing block gets an explicit annotation of its own, and drops
// annotations that no longer apply to any arithmetic statement.
//
// Safety: for every rewritten file the tool re-parses the result with overflow
// propagation DISABLED and compares, statement by statement, the effective
// overflow mode against the original parsed with propagation ENABLED. Any
// mismatch leaves the file untouched.
//
// Usage: lineoverflow [-dry] [-v] <file.no|dir> [...]
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// pureOverflowRe matches a line that consists solely of `#{overflow = <mode>}`.
var pureOverflowRe = regexp.MustCompile(`^#\{\s*overflow\s*=\s*[\w-]+\s*\}\s*$`)

// anyOverflowRe matches a `#{...}` line that contains an overflow entry.
var anyOverflowRe = regexp.MustCompile(`^#\{.*\boverflow\b.*\}\s*$`)

// opRe matches a binary arithmetic operator (+ * /) without requiring spaces
// (e.g. `a*b`). `-` is excluded because nolang identifiers contain hyphens.
var opRe = regexp.MustCompile(`[A-Za-z0-9_\)\]\}]\s*[\+\*/]\s*[A-Za-z0-9_\(\[\{]`)

func main() {
	var files []string
	dry, verbose, force := false, false, false
	for _, a := range os.Args[1:] {
		switch a {
		case "-dry":
			dry = true
		case "-v":
			verbose = true
		case "-force":
			force = true
		default:
			info, err := os.Stat(a)
			if err != nil {
				fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", a, err)
				continue
			}
			if info.IsDir() {
				_ = filepath.WalkDir(a, func(p string, d os.DirEntry, err error) error {
					if err == nil && !d.IsDir() && strings.HasSuffix(p, ".no") {
						files = append(files, p)
					}
					return nil
				})
			} else {
				files = append(files, a)
			}
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lineoverflow [-dry] [-v] <file.no|dir> [...]")
		os.Exit(2)
	}

	totalIns, totalDel, changed, skipped := 0, 0, 0, 0
	for _, path := range files {
		ins, del, err := migrateFile(path, dry, verbose, force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SKIP %s: %v\n", path, err)
			skipped++
			continue
		}
		if ins > 0 || del > 0 {
			changed++
			totalIns += ins
			totalDel += del
			if verbose || dry {
				fmt.Printf("%s: inserted=%d removed=%d\n", path, ins, del)
			}
		}
	}
	fmt.Printf("TOTAL: changed=%d skipped=%d inserted=%d removed=%d\n", changed, skipped, totalIns, totalDel)
}

// refEntry 描述一條陳述在「傳播 ON」下的參照生效溢出模式。
type refEntry struct {
	startLine int    // 1-based
	endLine   int    // 1-based
	refMode   string // 生效模式（自身註解優先，其次函式級作用域）
	arith     bool   // 該陳述「自身」是否含算術運算（不含嵌套區塊/臂體）
}

func migrateFile(path string, dry, verbose, force bool) (int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	src := string(data)
	srcLines := strings.Split(src, "\n")

	// ── 參照：以「傳播 ON」解析，逐陳述取得生效溢出模式 ────────────────
	pRef := parser.New(lexer.New(src))
	pRef.Filename = path
	pRef.LegacyBlockOverflowPropagation = true
	progRef := pRef.ParseProgram()
	if errs := pRef.Errors(); len(errs) > 0 {
		return 0, 0, fmt.Errorf("parse errors: %v", firstN(errs, 2))
	}
	entries, _ := collectRef(pRef, progRef.Statements, "", srcLines)

	if os.Getenv("LINEOVERFLOW_DUMP") != "" {
		for _, e := range entries {
			fmt.Fprintf(os.Stderr, "  ENTRY %s:%d-%d mode=%q arith=%v\n", path, e.startLine, e.endLine, e.refMode, e.arith)
		}
	}

	// 需要註解的目標行（1-based）→ 模式。
	targets := map[int]string{}
	for _, e := range entries {
		if e.refMode == "" || !e.arith {
			continue
		}
		targets[e.startLine] = e.refMode
	}

	// fdRemove：函式級 `#{overflow}` 行（位於函式定義上方）——新的行注解語意
	// 取消函式級作用域，這些行必須移除（其覆蓋的函式體改由逐行注解承擔）。
	fdRemove := fdLevelAnnLines(pRef, progRef.Statements, srcLines)

	// 規劃每個目標行：找到其上方的 overflow 註解行則重用（模式不符則改寫），
	// 找不到則在其上方插入一行。
	type plan struct {
		reuseIdx int    // 0-based 既有註解行索引（-1 = 需插入新行）
		mode     string // 目標模式
	}
	plans := map[int]plan{} // 目標行（1-based）→ 計畫
	reuseOf := map[int]int{} // 既有註解行索引 → 目標行
	for t, mode := range targets {
		idx, _ := overflowAnnAbove(srcLines, t)
		if idx >= 0 && !fdRemove[idx] {
			plans[t] = plan{reuseIdx: idx, mode: mode}
			reuseOf[idx] = t
			continue
		}
		plans[t] = plan{reuseIdx: -1, mode: mode}
	}

	// ── 重建原始碼 ─────────────────────────────────────────────────
	var out []string
	removed := 0
	inserted := 0
	emitted := map[int]bool{} // 已因重寫而輸出的既有註解行
	for i := 0; i < len(srcLines); i++ {
		raw := srcLines[i]
		trimmed := strings.TrimSpace(raw)
		if anyOverflowRe.MatchString(trimmed) {
			if fdRemove[i] {
				removed++
				if !pureOverflowRe.MatchString(trimmed) {
					if rw := dropOverflowEntry(raw); rw != "" {
						out = append(out, rw)
					}
				}
				continue
			}
			if t, ok := reuseOf[i]; ok {
				emitted[i] = true
				if annotationMode(raw) == plans[t].mode {
					out = append(out, raw)
				} else {
					out = append(out, replaceOverflowEntry(raw, plans[t].mode))
					removed++
				}
				continue
			}
			// 保守：不再刪除「看似失效」的註解行。
			out = append(out, raw)
			continue
		}
		if t := i + 1; targets[t] != "" {
			if p := plans[t]; p.reuseIdx < 0 {
				out = append(out, leadingWS(raw)+"#{overflow="+p.mode+"}")
				inserted++
			}
		}
		out = append(out, raw)
	}
	_ = emitted

	newSrc := strings.Join(out, "\n")
	if newSrc == src {
		return 0, 0, nil
	}

	// ── 校驗：新檔以「傳播 OFF」解析，逐陳述比對生效模式 ──────────────
	newLines := strings.Split(newSrc, "\n")
	pChk := parser.New(lexer.New(newSrc))
	pChk.Filename = path
	progChk := pChk.ParseProgram()
	if errs := pChk.Errors(); len(errs) > 0 {
		return 0, 0, fmt.Errorf("rewritten file has parse errors: %v", firstN(errs, 2))
	}
	refOps := arithOps(pRef, progRef.Statements, srcLines)
	chkOps := arithOps(pChk, progChk.Statements, newLines)
	if len(refOps) != len(chkOps) {
		return 0, 0, fmt.Errorf("verification failed: op count changed ref=%d chk=%d", len(refOps), len(chkOps))
	}
	for i := range refOps {
		if refOps[i].refMode != chkOps[i].refMode {
			if !force {
				return 0, 0, fmt.Errorf("verification failed at output line %d (src line %d): ref=%q chk=%q",
					chkOps[i].startLine, refOps[i].startLine, refOps[i].refMode, chkOps[i].refMode)
			}
			fmt.Fprintf(os.Stderr, "[FORCE] %s: output line %d (src line %d) ref=%q chk=%q\n",
				path, chkOps[i].startLine, refOps[i].startLine, refOps[i].refMode, chkOps[i].refMode)
		}
	}
	_ = entries

	if verbose {
		fmt.Fprintf(os.Stderr, "[lineoverflow] %s verified: %d ops\n", path, len(refOps))
	}

	if dry {
		return inserted, removed, nil
	}
	if err := os.WriteFile(path, []byte(newSrc), 0644); err != nil {
		return 0, 0, err
	}
	return inserted, removed, nil
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// collectRef 遞迴走訪陳述，回傳每個陳述的參照生效模式與所有陳述起始行。
// scope 為外層函式級溢出模式（僅 FunctionDefinition 會設定）。
func collectRef(p *parser.Parser, stmts []parser.Statement, scope string, srcLines []string) ([]refEntry, map[int]bool) {
	var out []refEntry
	starts := map[int]bool{}
	var walk func([]parser.Statement, string)
	walk = func(stmts []parser.Statement, scope string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			if _, ok := s.(*parser.AnnotationStatement); ok {
				continue
			}
			starts[s.Pos().Line] = true
			switch v := s.(type) {
			case *parser.FunctionDefinition:
				// 函式定義本身不是運算陳述；其註解作為函式級作用域向內傳遞。
				ns := modeOf(p, s)
				if v.Body != nil {
					walk(v.Body.Statements, ns)
				}
				continue
			case *parser.LetStatement:
				if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements, "")
					continue
				}
			case *parser.ExpressionStatement:
				if fl, ok := v.Expression.(*parser.FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements, "")
					continue
				}
			}
			m := modeOf(p, s)
			if m == "" {
				m = scope
			}
			out = append(out, refEntry{
				startLine: s.Pos().Line,
				endLine:   s.EndPos().Line,
				refMode:   m,
				arith:     stmtOwnArith(s, srcLines),
			})
			switch v := s.(type) {
			case *parser.BlockStatement:
				walk(v.Statements, scope)
			case *parser.ForStatement:
				if v.Init != nil {
					walk([]parser.Statement{v.Init}, scope)
				}
				if v.Update != nil {
					walk([]parser.Statement{v.Update}, scope)
				}
				if v.Body != nil {
					walk(v.Body.Statements, scope)
				}
			}
			// 刻意**不**遞迴進 IfExpression 的 Consequence/Alternative：
			// `c1 -> s1` / `c2 -> s2` 這類鏈式臂在 AST 上是「同一個
			// ExpressionStatement」的巢狀 else-if，codegen 以該陳述的
			// curOverflowMode 覆蓋全部臂（含臂體），故只需在鏈首標註一次。
			// 若逐一標註各臂，註解行會插在臂之間、把鏈切斷，且臂體的
			// 「自身行」並不足以代表整條鏈的運算需求。
		}
	}
	walk(stmts, scope)
	return out, starts
}

// stmtOwnArith 報告陳述「自身」（不含嵌套區塊/臂體）是否含整數算術運算。
// 對 Block 容器只看其表頭 `{` 行與尾行（`} (cond)` 後置條件的行）；對 For 容器
// 只看表頭行。其餘陳述（含 if 鏈，已由 chain-head 一次涵蓋全部臂）看整個行範圍。
func stmtOwnArith(s parser.Statement, srcLines []string) bool {
	start := s.Pos().Line
	end := s.EndPos().Line
	switch s.(type) {
	case *parser.BlockStatement:
		if lineRangeNeedsAnnotation(srcLines, start, start) {
			return true
		}
		return lineRangeNeedsAnnotation(srcLines, end, end)
	case *parser.ForStatement:
		return lineRangeNeedsAnnotation(srcLines, start, start)
	}
	return lineRangeNeedsAnnotation(srcLines, start, end)
}

// arithOps 回傳「自身含算術運算的陳述」及其生效溢出模式（前序），用於校驗。
func arithOps(p *parser.Parser, stmts []parser.Statement, srcLines []string) []refEntry {
	entries, _ := collectRef(p, stmts, "", srcLines)
	var out []refEntry
	for _, e := range entries {
		if e.arith {
			out = append(out, e)
		}
	}
	return out
}

// modeOf 讀取陳述自身的溢出模式（註解副表優先，其次 For/ExpressionStatement 欄位）。
func modeOf(p *parser.Parser, s parser.Statement) string {
	for _, e := range p.RawAnnotationsOf(s) {
		if e.Key != "overflow" || e.Value == nil {
			continue
		}
		switch v := e.Value.(type) {
		case *parser.AnnotationIdentValue:
			if m := parser.NormalizeOverflowMode(v.Value); m != "" {
				return m
			}
		case *parser.AnnotationStringValue:
			if m := parser.NormalizeOverflowMode(v.Value); m != "" {
				return m
			}
		}
	}
	switch n := s.(type) {
	case *parser.ForStatement:
		return n.OverflowMode
	case *parser.ExpressionStatement:
		return n.OverflowMode
	}
	return ""
}

// overflowAnnAbove 自目標行（1-based）向上尋找「最近的 overflow 註解行」，
// 途中允許跳過空行、`;` 註解行、以及其他 `#{...}` 註解行。找不到回 (-1, "")。
func overflowAnnAbove(srcLines []string, target int) (int, string) {
	for j := target - 2; j >= 0; j-- {
		t := strings.TrimSpace(srcLines[j])
		if t == "" || strings.HasPrefix(t, ";") {
			continue
		}
		if anyOverflowRe.MatchString(t) {
			return j, annotationMode(t)
		}
		if strings.HasPrefix(t, "#{") {
			continue
		}
		return -1, ""
	}
	return -1, ""
}

// annotationMode 從一個註解行文字取出 overflow 模式。
func annotationMode(line string) string {
	i := strings.Index(line, "overflow")
	if i < 0 {
		return ""
	}
	rest := line[i+len("overflow"):]
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "=") {
		return ""
	}
	rest = strings.TrimLeft(rest[1:], " \t")
	end := strings.IndexAny(rest, ",}")
	if end >= 0 {
		rest = rest[:end]
	}
	return parser.NormalizeOverflowMode(strings.TrimSpace(rest))
}

// dropOverflowEntry 從混合註解行中移除 overflow 條目；若移除後無其他條目則回傳 ""。
func dropOverflowEntry(line string) string {
	open := strings.Index(line, "#{")
	close := strings.LastIndex(line, "}")
	if open < 0 || close < 0 || close < open {
		return ""
	}
	body := line[open+2 : close]
	var keep []string
	for _, part := range strings.Split(body, ",") {
		p := strings.TrimSpace(part)
		if p == "" || strings.HasPrefix(p, "overflow") {
			continue
		}
		keep = append(keep, p)
	}
	if len(keep) == 0 {
		return ""
	}
	indent := leadingWS(line)
	return indent + "#{" + strings.Join(keep, ", ") + "}"
}

// replaceOverflowEntry 將註解行的 overflow 條目改寫為 mode（保留其他條目）。
func replaceOverflowEntry(line, mode string) string {
	if pureOverflowRe.MatchString(strings.TrimSpace(line)) {
		return leadingWS(line) + "#{overflow=" + mode + "}"
	}
	open := strings.Index(line, "#{")
	close := strings.LastIndex(line, "}")
	if open < 0 || close < 0 || close < open {
		return line
	}
	body := line[open+2 : close]
	var keep []string
	for _, part := range strings.Split(body, ",") {
		p := strings.TrimSpace(part)
		if p == "" || strings.HasPrefix(p, "overflow") {
			continue
		}
		keep = append(keep, p)
	}
	keep = append(keep, "overflow="+mode)
	return leadingWS(line) + "#{" + strings.Join(keep, ", ") + "}"
}

// fdLevelAnnLines 回傳所有「函式級」overflow 註解行的 0-based 索引：這些註解
// 位於某個 FunctionDefinition 正上方，且已由 attachAnnotations 寫入其 OverflowMode。
func fdLevelAnnLines(p *parser.Parser, stmts []parser.Statement, srcLines []string) map[int]bool {
	out := map[int]bool{}
	var walk func([]parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch v := s.(type) {
			case *parser.FunctionDefinition:
				if v.OverflowMode != "" {
					if idx := findOverflowAbove(srcLines, v.Token.Line-1); idx >= 0 {
						out[idx] = true
					}
				}
				if v.Body != nil {
					walk(v.Body.Statements)
				}
			case *parser.BlockStatement:
				walk(v.Statements)
			case *parser.ForStatement:
				if v.Body != nil {
					walk(v.Body.Statements)
				}
			case *parser.ExpressionStatement:
				if ie, ok := v.Expression.(*parser.IfExpression); ok {
					if ie.Consequence != nil {
						walk(ie.Consequence.Statements)
					}
					if ie.Alternative != nil {
						walk(ie.Alternative.Statements)
					}
				}
			case *parser.LetStatement:
				if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements)
				}
			}
		}
	}
	walk(stmts)
	return out
}

// findOverflowAbove 自 defLineIdx（0-based 定義行）向上、跳過空行與 `;` 註解，
// 尋找獨立的 overflow 註解行，回傳其 0-based 行索引；找不到回 -1。
func findOverflowAbove(srcLines []string, defLineIdx int) int {
	for j := defLineIdx - 1; j >= 0; j-- {
		t := strings.TrimSpace(srcLines[j])
		if t == "" || strings.HasPrefix(t, ";") {
			continue
		}
		if anyOverflowRe.MatchString(t) {
			return j
		}
		return -1
	}
	return -1
}

func lineRangeNeedsAnnotation(srcLines []string, start, end int) bool {
	if end < start {
		end = start
	}
	for ln := start; ln <= end; ln++ {
		if ln < 1 || ln > len(srcLines) {
			continue
		}
		if lineNeedsAnnotation(srcLines[ln-1]) {
			return true
		}
	}
	return false
}

func lineNeedsAnnotation(raw string) bool {
	if i := strings.Index(raw, ";"); i >= 0 {
		raw = raw[:i]
	}
	if strings.TrimSpace(raw) == "" {
		return false
	}
	if strings.Contains(raw, " + ") || strings.Contains(raw, " - ") ||
		strings.Contains(raw, " * ") || strings.Contains(raw, " / ") {
		return true
	}
	return opRe.MatchString(raw)
}

func leadingWS(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}
