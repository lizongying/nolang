package main

// 本檔案實作 `no fmt --fix=unwrap`：把「產生 option 卻從未消費」的綁定
// （checker trace `optunuse`，D2 下唯一的 ERROR）改寫為顯式消費形式；
// 同時也處理第二類站點——option 被顯式綁定到 **plain** 型別變數
// （`k i64 = a + b`，trace `15w45dqk`，同樣是硬 ERROR）。第二類多一步：
// 先剝掉那枚 plain 型別標註（算術/索引恆為 `?T`，標註本就是錯的），再走
// 與 optunuse 相同的消費。有無 `#{overflow=…}` 註解皆然（D1：註解只改行為、
// 不解包，結果仍是 `?T`）。
//
// 修復形態（docs/design/option-unified.md §6.1 / D3）：
//
//	; 情況 A：所在函式已有 `?T` 結果參數
//	x = <expr>   =>   x ?= <expr>
//
//	; 情況 B：否則（陳述式 match 標準形；空兜底臂 = no-op，D4/D8）
//	x = <expr>   =>   x-opt = <expr>
//	                  x-opt: {
//	                      ok -> x = it
//	                      ->
//	                  }
//
// 情況 B **一律用宣告式 match**，不能用值上下文寫法（`x = x-opt: { ok -> it }`，
// 即使寫成多行）：實測整段會被推成 `err | nil`，下游當引數用時報 6fgg3htw
// （`expected 'i64', got 'err | nil'`），見 §11 實測邊界 #2，勿改回值上下文。
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
//
// 站點收集走 `nbuild.VetFileWithLints`（與 `no vet <file>` 同一前端），不能用
// 裸解析：模組限定的 std 呼叫結果型別（`c = fs.read-str(path)`）只有完整管線
// 能推出來，獨立解析會整站漏報。依賴檔的診斷依 LintResult.File 歸屬過濾。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/lexer"
	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/parser"
)

// unwrapOptTraceID 是 `--fix=unwrap` 唯一認可的診斷來源：D2 之下 option 的
// 「產生處」（ovfhndld / idxhndld / ovf-int-default）都已降為 HINT，只有
// 「既沒上拋也沒解包」的 optunuse 還是 ERROR —— 也正是需要被修掉的那一批。
const unwrapOptTraceID = "optunuse"

// plainTargetOptArithTrace 是第二類待修站點的診斷：把 option 顯式綁定到 plain
// 型別變數（`k i64 = a + b`），由 ValidateTypes 以 trace 15w45dqk 報為硬錯誤。
// 這類站點的消費形態與 optunuse 相同（情況 A/B），但需先剝離那枚 plain 型別
// 標註——否則情況 B 會把 `k i64` 一起改名成 `k-opt i64`，仍是同一錯誤。
const plainTargetOptArithTrace = "15w45dqk"

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
	// predeclared = 同名變數在本次綁定之前已於可見作用域宣告過。情況 B 的
	// 預宣告（`<name> <T> = <零值>`）只對新鮮綁定需要；已存在的變數再預宣告
	// 會把舊值清掉，且在函式內預宣告會遮住同名模組級變數（捕獲語義被破壞）。
	predeclared bool
}

// fixUnwrapInFile 讀入單檔（磁碟內容），把每個被 `optunuse` / `15w45dqk` 報告的
// 綁定改寫為顯式消費形式，回傳修復後源碼；無可修處則回傳 ok=false。
//
// 分析刻意走**兩步**：
//
//  1. 完整 vet 管線（nbuild.VetFileWithLints，與 `no vet <file>` 同一入口）
//     → 取得 `optunuse` / `15w45dqk` 的站點行號。option 推斷與捕獲展開都是
//     lowering 的產物，surface AST 上看不到 `?T`，也就不會有這兩條診斷。
//     之所以不能退回「裸 parser.ParseProgram + checker.Validate*」：獨立解析
//     沒有模組上下文，模組限定的 std 呼叫（`c = fs.read-str(path)`）結果型別
//     無法解析，這類站點在獨立解析下根本不報——實測 shs-check.no 的
//     `src = c`（15w45dqk）就是漏網的那一個。
//  2. 糖形態解析（SkipLowering）→ 取得使用者真正寫下的那條陳述（名稱 token 的
//     確切行列、型別標註、語句跨度、所屬函式）。lowering 會把 `x = arr[i]` 換成
//     合成區塊，靠它拿不到「使用者寫的那個名字」。S4 起與 formatter 內部解析
//     （fmt/api.go）同形態；量測與門禁見 no_fix_shape_gate_test.go 的
//     TestFixUnwrapRuleIsShapeParity（含「報告點在 match 臂內」的敏感 fixture，
//     那是 collectUnwrapSites 需要 SurfaceMatch 下鑽的原因）。
//
// 兩者的行號同源（同一份 src），故以行號 join 是安全的。
//
// 管線回傳的 lint 陣列混有「依賴檔（std / 本地模組）自己的診斷」——合併程式
// 裡各模組語句都帶著原本的 LintResult.File。那些行號屬於依賴檔的檔案空間，
// 混進來會改到錯誤的陳述，必須依 File 歸屬過濾（空 = 主檔語句的預設歸屬；
// 非空則正規化後必須等於本檔）。這正是早先「不經 `no vet <pkg>`」顧慮的解法：
// 仍然逐檔分析、不建 <pkg>，但型別上下文是完整的。
func fixUnwrapInFile(filename string) (string, bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	return fixUnwrapSourceShape(filename, string(data))
}

// fixUnwrapSourceShape 是 fixUnwrapInFile 的純字串版（正式路徑，糖形態）。
// §4.3 已刪除 SkipUnwrapLowering / SkipSafeIndexLowering 兩旗標與 surface 形態，
// 糖形態（SkipLowering）是唯一形態。
//
// vet 管線（第 1 步）與形態無關——它跑完整 lowering 管線，兩種形態下同一份
// src 得到同一組行號；只有第 2 步的「使用者原句」解析走糖形態。
func fixUnwrapSourceShape(filename, src string) (string, bool) {
	// (1) 完整 vet 管線 → optunuse / 15w45dqk 站點行號。
	reportLines := map[int]bool{}
	typedOptLines := map[int]bool{}
	lints, verr := nbuild.VetFileWithLints(filename, nbuild.BuildOptions{})
	if verr != nil {
		return "", false
	}
	absName, absErr := filepath.Abs(filename)
	for _, l := range lints {
		if l.Line <= 0 {
			continue
		}
		if l.File != "" {
			// 依賴檔的診斷（File 指向別的模組）：不屬於本檔，跳過。
			lf, e := filepath.Abs(l.File)
			if e != nil || absErr != nil || lf != absName {
				continue
			}
		}
		switch {
		case l.TraceID == unwrapOptTraceID:
			reportLines[l.Line] = true
		case l.TraceID == plainTargetOptArithTrace && strings.Contains(l.Message, "assign ?"):
			// 只處理「option → plain」這一種 15w45dqk（訊息含 "assign ?<T>"，即推斷型別
			// 是 option）。plain → plain 的型別不符（如 `i64 → str`）同樣掛 15w45dqk，
			// 但那不是 option 消費問題——剝註解 + match 會把非 option 值餵進 match，製造
			// 壞碼，必須排除。
			typedOptLines[l.Line] = true
			reportLines[l.Line] = true
		}
	}
	if len(reportLines) == 0 {
		return "", false
	}

	// (2) 糖形態解析 → 使用者寫下的陳述。（§4.3 刪除 surface 旗標，糖形態唯一）
	sp := parser.New(lexer.New(src))
	sp.SkipLowering = true
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
		// typed：這一站是 15w45dqk（option 綁到 plain 型別變數）。必須先把
		// `<name> <type> =` 剝成 `<name> =`（推斷為 ?T），否則情況 B 會把型別標註
		// 一起搬到暫存變數上（`k-opt i64 = …` 仍是同一錯誤）。僅在單行綁定上剝；
		// 多行 typed 綁定罕見，交給人工處理。
		vline := start
		if s.Value != nil {
			vline = s.Value.Pos().Line
		}
		typed := s.Type != nil && (typedOptLines[start] || typedOptLines[vline])
		if typed && start != end {
			skipped++
			continue
		}
		var eq int // typed 時：`=` 在第一行的位元組索引（剝註解用）
		if typed {
			nameEnd := col - 1 + len(name)
			eqRel := strings.Index(first[nameEnd:], "=")
			if eqRel < 0 {
				skipped++
				continue
			}
			eq = nameEnd + eqRel
		}
		useCaseA := site.optRes != "" && name != site.optRes && (s.Type == nil || typed)
		if useCaseA {
			// 情況 A：上拋。`x = <expr>` → `x ?= <expr>`，只動 `=` 那一個字元。
			out := make([]string, len(rest))
			copy(out, rest)
			if typed {
				out[0] = first[:col-1+len(name)] + " ?" + first[eq:]
			} else {
				eqRel := strings.Index(first[col-1+len(name):], "=")
				if eqRel < 0 {
					skipped++
					continue
				}
				e := col - 1 + len(name) + eqRel
				out[0] = first[:e] + "?" + first[e:]
			}
			edits[start] = strings.Join(out, "\n")
			continue
		}
		// 情況 B：改名 + 陳述式 match 解包。
		//
		// 形態（docs/design/option-unified.md §11 實測邊界 #2 + 語料實際採用法）：
		//	<name>-opt = <expr>
		//	[<name> <T> = <零值>]      ← 僅「新鮮綁定」需要預宣告
		//	<name>-opt: {
		//	    ok -> <name> = it
		//	    ->
		//	}
		// 一律用**陳述式** match：值上下文寫法（`x = x-opt: { ok -> it }`，
		// 即使多行）會被整段推成 `err | nil`，下游當引數用時報 6fgg3htw
		// （`expected 'i64', got 'err | nil'`）——實測邊界，勿改回值上下文。
		// 空兜底臂 = no-op（陳述語境）：變數已存在則保留原值，新鮮綁定則保持
		// 預宣告的零值（D4「空表示忽略」）。
		tmp := freshUnwrapName(name, taken)
		taken[tmp] = true
		out := make([]string, len(rest))
		copy(out, rest)
		if typed {
			out[0] = first[:col-1] + tmp + " " + first[eq:]
		} else {
			out[0] = first[:col-1] + tmp + first[col-1+len(name):]
		}
		unit := indentUnit(indent)
		lines := []string{strings.Join(out, "\n")}
		// 新鮮綁定（本函式內尚未宣告過 <name>）必須預宣告：否則臂內的
		// `<name> = it` 只建出臂作用域變數，函式後續看不到。型別與零值取自
		// 被剝掉的那枚 plain 標註（只有 15w45dqk 站點有）。optunuse 站點要麼
		// 變數已存在、要麼從未被讀取，都不需要預宣告。
		if typed && !site.predeclared {
			zero, zok := zeroLiteralFor(s.Type.String())
			if !zok {
				skipped++
				continue
			}
			lines = append(lines, indent+name+" "+s.Type.String()+" = "+zero)
		}
		lines = append(lines,
			indent+tmp+": {",
			indent+unit+"ok -> "+name+" = it",
			indent+unit+"->",
			indent+"}",
		)
		edits[start] = strings.Join(lines, "\n")
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
	// walk 逐語句序列走訪；bound 累積「本函式內目前為止已宣告的變數名」
	// （進入函式/函式字面量時換新 map，並以外層 map 播種模擬詞法可見性），
	// 供 unwrapSite.predeclared 判斷預宣告是否需要。
	var walk func(stmts []parser.Statement, optRes string, bound map[string]bool)
	walk = func(stmts []parser.Statement, optRes string, bound map[string]bool) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch v := s.(type) {
			case *parser.LetStatement:
				if v.Name != nil && v.Value != nil && !v.IsSynthetic &&
					v.Name.Value != "_" && reportLines[v.Pos().Line] {
					sites = append(sites, unwrapSite{
						stmt:        v,
						optRes:      optRes,
						predeclared: bound[v.Name.Value],
					})
				}
				if v.Name != nil {
					bound[v.Name.Value] = true
				}
			if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
				inner := map[string]bool{}
				for k := range bound {
					inner[k] = true
				}
				walk(fl.Body.Statements, firstOptionResult(fl.Results), inner)
			}
			// 值上下文 match：`v = x: { ok -> w = arr[i] … }`——臂體裡的站點
			// 同樣要可見（與下面 ExpressionStatement 的 SurfaceMatch 分支同理由）。
			if sm, ok := v.Value.(*parser.SurfaceMatch); ok {
				parser.WalkSurfaceMatchArmBodies(sm, func(stmts []parser.Statement) {
					walk(stmts, optRes, bound)
				})
			}
			case *parser.FunctionDefinition:
				if v.Body != nil {
					inner := map[string]bool{}
					for k := range bound {
						inner[k] = true
					}
					walk(v.Body.Statements, firstOptionResult(v.Results), inner)
				}
			case *parser.ForStatement:
				if v.Init != nil {
					walk([]parser.Statement{v.Init}, optRes, bound)
				}
				if v.Update != nil {
					walk([]parser.Statement{v.Update}, optRes, bound)
				}
				if v.Body != nil {
					walk(v.Body.Statements, optRes, bound)
				}
			case *parser.BlockStatement:
				walk(v.Statements, optRes, bound)
		case *parser.ExpressionStatement:
			if ie, ok := v.Expression.(*parser.IfExpression); ok {
				if ie.Consequence != nil {
					walk(ie.Consequence.Statements, optRes, bound)
				}
				if ie.Alternative != nil {
					walk(ie.Alternative.Statements, optRes, bound)
				}
			}
			// S4 糖形態：match 不再被降糖成 if 鏈，臂體是 SurfaceMatch 的直接
			// 子節點。少了這一條，臂內的 optunuse 站點對本 walker 不可見 ⇒
			// `--fix=unwrap` 靜默漏修（surface 形態下同一站點走上面的
			// IfExpression 分支、是能修的——實測 fixture 見
			// no_fix_shape_gate_test.go 的 TestFixUnwrapRuleIsShapeParity）。
			if sm, ok := v.Expression.(*parser.SurfaceMatch); ok {
				parser.WalkSurfaceMatchArmBodies(sm, func(stmts []parser.Statement) {
					walk(stmts, optRes, bound)
				})
			}
		}
		}
	}
	walk(prog.Statements, "", map[string]bool{})
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

// zeroLiteralFor 回傳型別標註對應的零值字面量（情況 B 預宣告用）。只收縮
// 已知型別；查不到（自定 struct / newtype / 泛型佔位等）回傳 ok=false，
// 讓呼叫方把該站點標為「需人工處理」——寧可漏改也不亂填零值。
func zeroLiteralFor(typeStr string) (string, bool) {
	switch typeStr {
	case "i8", "i16", "i32", "i64", "int",
		"u8", "u16", "u32", "u64", "uint", "byte":
		return "0", true
	case "str":
		return "''", true
	case "bool":
		return "false", true
	case "f32", "f64":
		return "0.0", true
	}
	return "", false
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
