package main

// 本文件实现 `no fmt --fix=index-out`：把带 `#{index-out=DEF}` 注解的「安全索引
// 单值读取赋值」（`v = c[i]`，c 为 arr/vec/slice 变量，越界用 DEF 替代）改写为显式
// match 形态：
//
//	#{index-out=zero}
//	    s = lines1[i]
//	↓ ↓ ↓
//	    s = lines1[i]: {
//	        ok -> it
//	        -> ''      ; 元素类型的具体零值字面量（str→''、i64→0、bool→false…）
//	    }
//
// 等价性：编译器内部对 `#{index-out=DEF}` 正是经 maybeIndexOutAssign 脱糖成同样的
// match（ok 臂绑定 it 为解箱元素、wildcard 臂返回 DEF），故手写形态与编译器行为一致，
// 只是把「隐式」变「显式」并移除注解。注意 `zero` 不是 Nolang 关键字——`=zero` 必须
// 按元素类型生成具体零值字面量（str→''、i64→0、bool→false、f64→0.0），不能写死 `zero`。
//
// 检测口径对齐 maybeIndexOutAssign（parser/lowering.go）：赋值 Value 是
// *IndexExpression、base 为直接变量（不接受 .field[i] / 非容器基底）、且带
// `#{index-out=DEF}` 注解。DEF 解释：
//   - `zero` → 按元素类型生成具体零值字面量（需推断元素类型）；
//   - 其它字面量（0 / 1 / '' / nil / [] / T{…}）→ 直接复用注解里的字面量文本。
//
// 仅处理「安全子集」：base 变量类型在解析期可推断（FuncVarType 命中）时才改写；
// 跨模块调用才得到容器类型（解析期未知）的站点，跳过并 stderr 报告，留人工处理。
// 子表达式索引读取（如 `out = out - a[i] #{index-out=''}`）、索引写入 LHS
// （`arr[i] = x`）、`.field[i]` 均不在本 fixer 范围，自动跳过。
//
// 与既有 fix 同风格：手术式文本替换、不改排版、幂等（改写后的语句不再带注解，
// 重跑不会再触发）。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// origSite / site 是 fixIndexOutSource 在 AST walk 期间登记的「原赋值」与「需改写站点」。
type origSite struct {
	name string
	rhs  string
	typ  string // 源码显式型别标注（如 `av i64 = a[i]` 的 `i64`）；无则 ""
}
type site struct {
	defLit string
	line   int
}

// fixIndexOutFixes 收集单文件/目录下所有 `--fix=index-out` 的修复结果。
func fixIndexOutFixes(arg string) map[string]string {
	info, err := os.Stat(arg)
	if err != nil {
		return nil
	}
	files := []string{}
	if info.IsDir() {
		_ = filepath.Walk(arg, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil || fi == nil || fi.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".no") {
				files = append(files, path)
			}
			return nil
		})
	} else {
		files = append(files, arg)
	}
	sort.Strings(files)
	fixes := map[string]string{}
	for _, f := range files {
		fixed, ok := fixIndexOutInFile(f)
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

func fixIndexOutInFile(filename string) (string, bool) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	return fixIndexOutSource(filename, string(data))
}

func fixIndexOutSource(filename, src string) (string, bool) {
	// 糖形态解析（SkipLowering）：拿到用户写下的赋值语句与注解。
	sp := parser.New(lexer.New(src))
	sp.SkipLowering = true
	prog := sp.ParseProgram()
	if len(sp.Errors()) > 0 || prog == nil {
		return "", false
	}
	lines := strings.Split(src, "\n")

	origs := map[int]origSite{}
	var sites []site
	skipped := 0

	var walk func(stmts []parser.Statement, curFunc string)
	walk = func(stmts []parser.Statement, curFunc string) {
		for _, s := range stmts {
			switch v := s.(type) {
			case *parser.LetStatement:
				if v.Name != nil && v.Value != nil && !v.IsSynthetic {
					if idx, ok := v.Value.(*parser.IndexExpression); ok {
						handleIndexRead(prog, lines, v, v.Name.Value, explicitTypeText(v.Type), idx, curFunc, origs, &sites, &skipped)
					}
				}
			case *parser.ExpressionStatement:
				switch tgt := v.Expression.(type) {
				case *parser.AssignExpression:
					if id, ok := tgt.Left.(*parser.Identifier); ok {
						if idx, ok := tgt.Value.(*parser.IndexExpression); ok {
							handleIndexRead(prog, lines, v, id.Value, "", idx, curFunc, origs, &sites, &skipped)
						}
					}
				case *parser.SurfaceMatch:
					parser.WalkSurfaceMatchArmBodies(tgt, func(armStmts []parser.Statement) {
						walk(armStmts, curFunc)
					})
				case *parser.IfExpression:
					// `cond -> { ... }` 在解析期是 ExpressionStatement(IfExpression)，
					// 臂體在 Consequence / Alternative（均 *BlockStatement）。
					if tgt.Consequence != nil {
						walk(tgt.Consequence.Statements, curFunc)
					}
					if tgt.Alternative != nil {
						walk(tgt.Alternative.Statements, curFunc)
					}
				}
			case *parser.FunctionDefinition:
				if v.Body != nil {
					walk(v.Body.Statements, funcNameOf(v))
				}
			case *parser.ForStatement:
				if v.Body != nil {
					walk(v.Body.Statements, curFunc)
				}
				if v.Init != nil {
					walk([]parser.Statement{v.Init}, curFunc)
				}
				if v.Update != nil {
					walk([]parser.Statement{v.Update}, curFunc)
				}
				// SkipLowering 形态下，标签条件包裝 ForStatement（#N cond: { ... }）
				// 的臂體還掛在 Condition.(*IfExpression).Consequence，lowering 才搬到
				// .Body。此處補一次下降，確保安全索引讀取不被漏掉。
				if v.Body == nil {
					if ife, ok := v.Condition.(*parser.IfExpression); ok {
						if ife.Consequence != nil {
							walk(ife.Consequence.Statements, curFunc)
						}
						if ife.Alternative != nil {
							walk(ife.Alternative.Statements, curFunc)
						}
					}
				}
			case *parser.BlockStatement:
				walk(v.Statements, curFunc)
			case *parser.BlockMatchStatement:
				// `cond -> { ... }` / `x: { ... }` 語句：臂體在 .Body，匹配結構在 .Match。
				if v.Body != nil {
					walk(v.Body.Statements, curFunc)
				}
				if v.Match != nil {
					parser.WalkSurfaceMatchArmBodies(v.Match, func(armStmts []parser.Statement) {
						walk(armStmts, curFunc)
					})
				}
			}
		}
	}
	walk(prog.Statements, "")
	if len(sites) == 0 {
		return src, false
	}

	edits := map[int]string{}       // 赋值行 -> 替换内容
	annRemovals := map[int]string{} // 注解行 -> 新内容（"" 表示删除）
	for _, st := range sites {
		o, ok := origs[st.line]
		if !ok || o.name == "" {
			skipped++
			continue
		}
		first := lines[st.line-1]
		indent := leadingWhitespace(first)
		unit := indentUnit(indent)
		// 生成「matched match 表达式」形态：整段索引读取作为 match 主体，其求值结果
		// 经 `name = <rhs>: { ... }` 赋值给目标变量；ok 臂直接 yield 解箱元素 `it`，
		// catch-all 臂 yield 默认值 DEF（二者皆为表达式、不在臂内重复赋值）。这等价于
		// 编译器 maybeIndexOutAssign 的脱糖结果，只是把「隐式」变「显式」并移除注解。
		// 注意 `rhs: { ok -> name = it -> name = DEF }`（把赋值写进臂内）不是干净形态。
		//
		// ⚠️ 必须保留原声明的显式型别标注（`av i64 = a[i]` 的 i64）：元素型别可能是
		// byte（回绕型别），原代码靠显式 i64 让算术升到 i64 域；丢掉标注会让目标被推成
		// byte，整条大数加法/乘法静默 mod 256（ecdsa/bzip2/dns 实测回归）。
		lhs := o.name
		if o.typ != "" {
			lhs = o.name + " " + o.typ
		}
		// 臂之间留一个空行：这是 `no fmt` 对 match 臂的固定排版（formatSurfaceMatch
		// 每臂前写一个裸 "\n"）。生成时就跟上，改写结果才是 fmt 规范态——否则每个
		// 被改写的文件还要再跑一次 fmt 才稳定（2026-11-06 实测 std+tests 79 个文件
		// 因此产生纯排版 diff）。
		rep := fmt.Sprintf("%s%s = %s: {\n%s%sok -> it\n\n%s%s-> %s\n%s}",
			indent, lhs, o.rhs, indent, unit, indent, unit, st.defLit, indent)
		edits[st.line] = rep
		// 移除（或剥离）管辖注解行。
		if annLine, newContent, safe := stripGoverningIndexOut(lines, st.line); safe {
			if annLine > 0 {
				annRemovals[annLine] = newContent
			}
		} else {
			// 注解无法安全剥离（如含复合默认值）→ 撤销本站点改写。
			delete(edits, st.line)
			skipped++
		}
	}
	if len(edits) == 0 {
		if skipped > 0 {
			fmt.Fprintf(os.Stderr, "no fmt --fix=index-out: %s: %d site(s) need manual handling (cannot rewrite safely)\n", filename, skipped)
		}
		return src, false
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "no fmt --fix=index-out: %s: %d site(s) skipped (type not inferable / unsafe annotation)\n", filename, skipped)
	}
	outLines := make([]string, 0, len(lines)+len(edits))
	for i := 1; i <= len(lines); i++ {
		if rep, ok := edits[i]; ok {
			outLines = append(outLines, rep)
		} else if newC, ok := annRemovals[i]; ok {
			if newC != "" {
				outLines = append(outLines, newC)
			}
			// newC == "" → 删除该行
		} else {
			outLines = append(outLines, lines[i-1])
		}
	}
	return strings.Join(outLines, "\n"), true
}

// handleIndexRead 检测单个「安全索引读取赋值」候选，若可改写则登记到 origs / sites。
func handleIndexRead(prog *parser.Program, lines []string, stmt parser.Statement, name, typ string, idx *parser.IndexExpression, curFunc string, origs map[int]origSite, sites *[]site, skipped *int) {
	line := stmt.Pos().Line
	end := stmt.EndPos().Line
	if end != line {
		return // 多行赋值：保守跳过
	}
	// base 必须是直接变量（对齐 isSafeIndexBase）。
	ident, ok := idx.Left.(*parser.Identifier)
	if !ok {
		return // .field[i] 等不处理
	}
	// 必须带 #{index-out=DEF} 注解。
	entries := prog.Sem.RawAnnotationsOf(stmt)
	var defVal parser.AnnotationValue
	found := false
	for _, e := range entries {
		if e != nil && e.Key == "index-out" {
			defVal = e.Value
			found = true
			break
		}
	}
	if !found {
		return
	}
	// 源码向上确认注解确实「紧贴」本语句：只允许中间夹空白 / 注释行，一旦遇到真实
	// 语句即视为未依附。否则已改写过的 `c = b[pos]: { ok -> it -> 0 }` 会被上方
	// for-condition 等遗留的孤儿 #{index-out} 注解误命中，重复生成 match 块。
	if annLine, _, ok := stripGoverningIndexOut(lines, line); !ok || annLine == 0 {
		return
	}
	// 推断元素类型（解析期 FuncVarType，含全域 VarTypes 回退）。
	lt, ok2 := prog.Sem.FuncVarType(curFunc, ident.Value)
	if !ok2 || lt == "" {
		lt, ok2 = prog.Sem.FuncVarType("", ident.Value)
	}
	if !ok2 || lt == "" {
		*skipped++
		return
	}
	elem := parser.ContainerElemType(lt)
	if elem == "" {
		*skipped++
		return
	}
	// 解释 DEF。
	var defLit string
	switch dv := defVal.(type) {
	case *parser.AnnotationIdentValue:
		if dv.Value == "zero" {
			z, zok := zeroLiteralForElem(elem)
			if !zok {
				*skipped++
				return
			}
			defLit = z
		} else {
			defLit = dv.String()
		}
	case nil:
		*skipped++
		return
	default:
		defLit = dv.String()
	}
	if defLit == "" {
		*skipped++
		return
	}
	rhs := rebuildIndexText(lines[line-1], idx)
	if rhs == "" {
		*skipped++
		return
	}
	origs[line] = origSite{name: name, rhs: rhs, typ: typ}
	*sites = append(*sites, site{defLit: defLit, line: line})
}

// zeroLiteralForElem 依元素类型 elem 产生越界默认零值字面量（对齐 scalarZeroLiteral /
// zeroValueLiteralFor）。标量走精确字面量；切片→`[]`、映射→`{}`。定長陣列 `[N]T`、
// char、以及结构體 / 具名型別的 `Name {}` 一律交人工（返回 false）：空大括号在臂体
// 位置不是 struct literal，值语境下会被解析器拒收（见下方 default 分支的说明）。
func zeroLiteralForElem(elem string) (string, bool) {
	switch elem {
	case "str", "txt":
		return "''", true
	case "char":
		return "", false // char 零值歧义，留人工
	case "i8", "i16", "i32", "i64", "int", "u8", "u16", "u32", "u64", "uint", "byte":
		return "0", true
	case "f32", "f64":
		return "0.0", true
	case "bool":
		return "false", true
	}
	switch {
	case strings.HasPrefix(elem, "[]"): // 切片 → 空切片字面量
		return "[]", true
	case strings.HasPrefix(elem, "{"): // 映射 → 空映射
		return "{}", true
	case strings.HasPrefix(elem, "["): // 定長陣列 [N]T：需元素數，交人工
		return "", false
	default:
		// 结构體 / 具名型別的零值 `Name {}`：**跳過不改寫**。
		//
		// 值語境（`name = c[i]: { … }`）裡 `-> Name {}` 不是表達式：空大括號在
		// 臂體位置被解析成 block 而非 struct literal，`no` 直接報「match arm in
		// expression context must end with an expression」（2026-11-06 `make gen`
		// 在 std/math.no 的 `-> num {}` 上實測）。語句形態
		// `c[i]: { ok -> name = it -> name = Name {} }` 雖可寫，但與本 fixer 的
		// 統一輸出形態不一致，交人工決定，故一律跳過（保留原註解形態）。
		return "", false
	}
}

// rebuildIndexText 从源码行中提取完整 `c[i]` 文本（含 base 变量，兼容嵌套下标）。
// 从 base 标识符起点列开始扫描到匹配 `]`，避免只截到 `[i]` 而丢掉 base。
func rebuildIndexText(line string, idx *parser.IndexExpression) string {
	var baseCol int
	if id, ok := idx.Left.(*parser.Identifier); ok {
		baseCol = id.Token.Column
	} else {
		baseCol = idx.Token.Column // 兜底：从 '[' 起（会丢失 base，仅极少数非直接变量情形）
	}
	if baseCol < 1 {
		return ""
	}
	start0 := baseCol - 1
	if start0 >= len(line) {
		return ""
	}
	depth := 0
	end0 := -1
	for j := start0; j < len(line); j++ {
		switch line[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end0 = j
			}
		}
		if end0 >= 0 {
			break
		}
	}
	if end0 < 0 {
		return ""
	}
	return line[start0 : end0+1]
}

// stripGoverningIndexOut 找到管辖 line 行的 `#{index-out=...}` 注解并安全剥离其
// index-out 条目（保留同行的 overflow 等兄弟注解）。返回 (注解所在行, 剥离后内容,
// 是否安全)。注解内容是复合默认值（含 `{`）而无法安全剥离时 safe=false，调用方应
// 撤销该站点改写；未找到独立成行注解（尾随写法由替换自然排除 / 无注解）时返回
// (0, "", true) 表示可继续。
func stripGoverningIndexOut(lines []string, line int) (int, string, bool) {
	for a := line - 1; a >= 1; a-- {
		l := lines[a-1]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.Contains(l, "index-out") {
			newLine, removed := stripIndexOutFromLine(l)
			if !removed {
				return 0, "", false
			}
			return a, newLine, true
		}
		// 遇到真实语句而未见注解 → 停止向上搜索（尾随写法不在此列）。
		return 0, "", true
	}
	return 0, "", true
}

// stripIndexOutFromLine 从单行注解 `#{...}` 中移除 index-out 条目，保留其余条目。
// 无法安全处理（含复合默认值 `{`）时返回 ("", false)。
func stripIndexOutFromLine(l string) (string, bool) {
	start := strings.Index(l, "#{")
	if start < 0 {
		return l, false
	}
	rest := l[start:]
	end := strings.Index(rest, "}")
	if end < 0 {
		return l, false
	}
	inner := rest[2:end]
	if strings.Contains(inner, "{") {
		return l, false // 复合默认值，无法安全剥离
	}
	parts := strings.Split(inner, ",")
	kept := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k := p
		if i := strings.Index(p, "="); i >= 0 {
			k = strings.TrimSpace(p[:i])
		}
		if k == "index-out" {
			continue
		}
		kept = append(kept, p)
	}
	prefix := l[:start]
	trailing := l[start+end+1:]
	if len(kept) == 0 {
		return "", true // 整条注解删除
	}
	return prefix + "#{" + strings.Join(kept, ", ") + "}" + trailing, true
}

// explicitTypeText 返回 let 声明上「源码显式」的型别标注文本（如 `av i64 = a[i]`
// 的 i64）；无标注或由 parser 推断（IsInferred）时返回 ""。仅处理 *parser.NamedType
// （标量/具名型），这已覆盖全部需要保宽的站点（i64/u64/str…）；指针/切片等显式标注
// 在索引读取目标上罕见，忽略。
func explicitTypeText(t parser.Type) string {
	nt, ok := t.(*parser.NamedType)
	if !ok || nt == nil || nt.IsInferred {
		return ""
	}
	return nt.Value
}

// funcNameOf 返回函数定义的函数作用域名（供 FuncVarType 查询）。
func funcNameOf(fd *parser.FunctionDefinition) string {
	return fd.Name
}

// 注：leadingWhitespace 已在 main.go 定义（同包复用），本文件仅调用不再重定义。
