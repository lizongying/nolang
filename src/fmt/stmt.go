package fmt

import (
	"fmt"
	"os"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// formatStatement formats a single statement. It returns true if it wrote any
// visible output, false if the statement is a no-op (synthetic `let`, bare `;`
// expression, or a block-scoped overflow annotation that was already active and
// thus emitted nothing). The caller uses this to skip the inter-statement gap so
// a no-op statement produces zero output (no blank line) — essential for idempotency.
func (f *formatter) formatStatement(stmt parser.Statement) bool {
	// Skip compiler-injected synthetic statements (e.g., `it = matched`)
	if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
		return false
	}
	// Use CommentedNode interface to get Doc comments
	var doc *parser.CommentGroup
	if d, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		doc = d.GetDoc()
	}
	// 輸出語句前的註釋（Doc），保留註釋與語句之間的空行
	f.formatDocComments(doc)
	if doc != nil && len(doc.List) > 0 {
		lastDocLine := doc.List[len(doc.List)-1].Pos.Line
		stmtLine := stmtTokenLine(stmt)
		// 用 hasBlankLineBetween 偵測「語句前是否真的有空行」，而非單純行號落差：
		// 區塊級 #{overflow=...} 經 attachedAnnotations 合併到每個陳述、又由
		// propagateBlockScopedOverflow 以獨立註解陳述形式存在，formatter 會把註解
		// 印在 Doc 與陳述本體之間的一行上；若用 stmtLine > lastDocLine+1，二次
		// 格式化時陳述 token 行號被這一行推後，會誤判「有空行」而插入空行、再被
		// 偵測、再插入（非冪等）。hasBlankLineBetween 會略過 overflow 註解行，
		// 正確反映「用戶究竟有無在 Doc 與陳述間留空行」。
		if lastDocLine > 0 && f.hasBlankLineBetween(lastDocLine, stmtLine) {
			// Preserve blank line between last Doc comment and statement
			f.write("\n") // bare blank line (no indent)
		}
		// 獨立 #{...} 註解若因 activeOverflow 去重而不輸出本體，就只有其 Doc
		// 會印出；此時不可收尾換行，否則會留下只有縮排的空行（非冪等）。行尾
		// 交由下一條陳述的 gap 邏輯或區塊的 `}` 收束。
		emitBody := true
		if as, ok := stmt.(*parser.AnnotationStatement); ok {
			emitBody = f.annotationStatementEmits(as)
		}
		if emitBody {
			f.newline() // indent for statement
		}
	}

	// Output attached annotations before the statement.
	// 平台/泛型註解（如 #{mac-arm64}）是陳述級附加，每次都輸出；overflow 註解則是
	// 區塊級：parser 的 propagateBlockScopedOverflow 已將其合併進區塊內每個陳述的
	// side-table，若在此對每個陳述都輸出會重複印 N 次。故 overflow 走 activeOverflow
	// 去重（模式不變則跳過），平台/泛型註解不受影響照常輸出。兩者分屬不同行。
	// 尾隨註解（`stmt #{...}`）收集起來，待陳述本體輸出後寫在同一行後方。
	var trailingAnns []*parser.AnnotationEntry
	if anns := f.attachedAnnotations(stmt); len(anns) > 0 {
		if os.Getenv("NOLANG_FMTDBG") != "" {
			fmt.Fprintf(os.Stderr, "[DBG] formatStatement attached-overflow on %T\n", stmt)
		}
		var others []*parser.AnnotationEntry
		var overflowModes []string
		seenMode := make(map[string]bool)
		for _, e := range anns {
			if e.Trailing {
				trailingAnns = append(trailingAnns, e)
				continue
			}
			if e.Key == "overflow" {
				if m := overflowModeStringOf(e); m != "" && !seenMode[m] {
					seenMode[m] = true
					overflowModes = append(overflowModes, m)
				}
			} else {
				others = append(others, e)
			}
		}
		if len(others) > 0 {
			f.write("#{")
			for i, e := range others {
				if i > 0 {
					f.write(", ")
				}
				f.write(e.String())
			}
			f.write("}")
			f.newline()
		}
		if len(overflowModes) > 0 {
			f.emitOverflowAnnotation(strings.Join(overflowModes, ","), true)
		}
	}

	emitted := true
	switch s := stmt.(type) {
	case *parser.UseStatement:
		f.formatUseStatement(s)
	case *parser.ExportStatement:
		f.formatExportStatement(s)
	case *parser.LetStatement:
		f.formatLetStatement(s)
	case *parser.TypeAlias:
		f.formatTypeAlias(s)
	case *parser.ReturnStatement:
		f.formatReturnStatement(s)
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			f.formatExpression(s.Expression)
		} else {
			// nil expression = bare { from condition: { body } syntax — skip silently
			emitted = false
		}
	case *parser.FunctionDefinition:
		f.formatFunctionDefinition(s)
	case *parser.ForStatement:
		f.formatForStatement(s)
	case *parser.BreakStatement:
		f.formatBreakStatement(s)
	case *parser.ContinueStatement:
		f.formatContinueStatement(s)
	case *parser.BlockStatement:
		f.formatBlockStatement(s)
	case *parser.EnumDefinition:
		f.formatEnumDefinition(s)
	case *parser.TaggedEnumDefinition:
		f.formatTaggedEnumDefinition(s)
	case *parser.InterfaceDefinition:
		f.formatInterfaceDefinition(s)
	case *parser.StructDefinition:
		f.formatStructDefinition(s)
	case *parser.MultiAssignStatement:
		f.formatMultiAssignStatement(s)
	case *parser.UnwrapAssignStatement:
		if s.Target != nil {
			f.formatExpression(s.Target)
		} else {
			f.formatExpression(s.Name)
		}
		// IsAutoPropagated=True 表示此節點由 lowering 從普通 `x = v[5]` 自動
		// 提升而來（來源中並非顯式 `?=`），formatter 須渲染回 `=` 以忠實還原
		// 來源（見 ast.go 對 UnwrapAssignStatement.IsAutoPropagated 的說明）。
		if s.IsAutoPropagated {
			f.write(" = ")
		} else {
			f.write(" ?= ")
		}
		f.formatExpression(s.Value)
	case *parser.ExternStatement:
		f.formatExternStatement(s)
	case *parser.AnnotationStatement:
		if !f.formatAnnotationStatement(s) {
			emitted = false
		}
	}

	// 尾隨註解：寫回陳述同一行的後方（位置規則只允許「上方獨立成行」或「同一行
	// 尾隨」，搬到上方或丟掉都會改變使用者寫下的位置；index-out 遺失更會直接
	// 讓越界保護消失）。輸出在行內註釋之前，得到 `stmt #{...} ; comment`。
	if len(trailingAnns) > 0 {
		f.write(" #{")
		for i, e := range trailingAnns {
			if i > 0 {
				f.write(", ")
			}
			f.write(e.String())
		}
		f.write("}")
	}

	// For FunctionDefinition and ForStatement, inline comment is handled inside the specific formatter.
	// For other statement types, output inline comment here.
	if _, isFunc := stmt.(*parser.FunctionDefinition); !isFunc {
		var comment *parser.CommentGroup
		if c, ok := stmt.(interface{ GetComment() *parser.CommentGroup }); ok {
			comment = c.GetComment()
		}
		f.formatInlineComment(comment)
	}
	return emitted
}

// isStandaloneIfThen reports whether stmt is a standalone if-then
// (`cond -> body`, RTStandalone) that still lacks an else branch.
func (f *formatter) isStandaloneIfThen(stmt parser.Statement) bool {
	es, ok := stmt.(*parser.ExpressionStatement)
	if !ok || es.Expression == nil {
		return false
	}
	ie, ok := es.Expression.(*parser.IfExpression)
	if !ok {
		return false
	}
	return f.hasRT(ie, parser.RTStandalone) &&
		!f.hasRT(ie, parser.RTMatchWildcard) &&
		ie.Alternative == nil
}

// isBareElseAfterIfThen reports whether cur is a bare wildcard `-> body` arm
// (RTStandalone + RTMatchWildcard) that directly follows an else-less standalone
// if-then (prev) in the same block. Such a pair is semantically an if/else; the
// parser normally folds the `->` into the if's Alternative, but a preceding
// standalone `#{...}` annotation makes parseAnnotationStatement consume the `->`
// first, bypassing that folding and leaving two sibling statements. The
// formatter must render this shape identically to the folded one (a blank line
// before the else) to stay idempotent.
func (f *formatter) isBareElseAfterIfThen(prev, cur parser.Statement) bool {
	if prev == nil {
		return false
	}
	es, ok := cur.(*parser.ExpressionStatement)
	if !ok || es.Expression == nil {
		return false
	}
	ie, ok := es.Expression.(*parser.IfExpression)
	if !ok {
		return false
	}
	if !f.hasRT(ie, parser.RTStandalone) || !f.hasRT(ie, parser.RTMatchWildcard) {
		return false
	}
	return f.isStandaloneIfThen(prev)
}

// statementEmitsSomething reports whether formatting this statement will produce
// any visible output. It mirrors the emit logic of formatStatement without
// side effects (except reading the read-only f.activeOverflow for overflow
// de-duplication), so callers can decide whether to insert the inter-statement
// gap. Synthetic `let`s, bare `;` expressions, and block-scoped overflow
// annotations that are already active emit nothing.
func (f *formatter) statementEmitsSomething(stmt parser.Statement) bool {
	switch s := stmt.(type) {
	case *parser.AnnotationStatement:
		// 即使註解本身因去重而不輸出，其前置 Doc 註釋仍會被 formatStatement
		// 印出，因此仍算「有輸出」：否則呼叫端 (formatBlockInner) 會因為
		// emits==false 而跳過換行，formatStatement 的 formatDocComments 就把
		// 註解直接接在上一條陳述結尾（典型：gzip.no 的
		// `}; FHCRC (bit 0) — 2 bytes CRC16`），非冪等。
		return f.annotationStatementEmits(s) || f.hasDocComment(s)
	case *parser.LetStatement:
		return !s.IsSynthetic
	case *parser.ExpressionStatement:
		return s.Expression != nil
	}
	return true
}

// annotationStatementEmits reports whether a standalone #{...} annotation
// statement will actually emit output. With `#{overflow=...}` as a line
// annotation (no more cross-statement de-duplication) every entry is emitted
// verbatim, so any entry yields output.
func (f *formatter) annotationStatementEmits(s *parser.AnnotationStatement) bool {
	return len(s.Entries) > 0
}

func (f *formatter) formatUseStatement(s *parser.UseStatement) {
	f.write("# ")
	f.write(s.Path)
	if s.Function != "" {
		f.write(".")
		f.write(s.Function)
	}
	if s.Alias != "" {
		f.write(" ")
		f.write(s.Alias)
	}
}

func (f *formatter) formatExportStatement(s *parser.ExportStatement) {
	f.write("@ ")
	f.write(s.Path)
	if s.Function != "" {
		f.write(".")
		f.write(s.Function)
	}
	if s.Alias != "" {
		f.write(" ")
		f.write(s.Alias)
	}
}

func (f *formatter) formatTypeAlias(s *parser.TypeAlias) {
	f.write(s.Name)
	f.write(" = ")
	if s.IsUnion() {
		for i, t := range s.Union.Types {
			if i > 0 {
				f.write(" | ")
			}
			f.write(t.String())
		}
	} else if s.Type != nil {
		if ft, ok := s.Type.(*parser.FunctionType); ok {
			f.formatFunctionTypeAlias(ft)
		} else {
			f.write(s.Type.String())
		}
	}
}

// formatFunctionTypeAlias outputs a function type in alias syntax: (params)(results)?

// formatFunctionTypeAlias outputs a function type in alias syntax: (params)(results)?
func (f *formatter) formatFunctionTypeAlias(ft *parser.FunctionType) {
	f.write("(")
	for i, p := range ft.Params {
		if i > 0 {
			f.write(", ")
		}
		if p.Name != "" {
			f.write(p.Name)
			f.write(" ")
		}
		f.write(p.Type.String())
	}
	f.write(")")
	if len(ft.Results) > 0 {
		f.write(" (")
		for i, r := range ft.Results {
			if i > 0 {
				f.write(", ")
			}
			if r.Name != "" {
				f.write(r.Name)
				f.write(" ")
			}
			f.write(r.Type.String())
		}
		f.write(")")
	}
}

func (f *formatter) formatLetStatement(s *parser.LetStatement) {
	f.formatExpression(s.Name)
	// Render array/slice type: a [3]u16, v []u8, a [?]u16
	if at, ok := s.Type.(*parser.ArrayType); ok {
		if at.IsInferred {
			// 推斷型別：checker 合成、原始碼未書寫，不渲染。否則會把合成型別
			// （其 size/elem 的 token literal 可能為合成值，如複用變數名 "hash"）
			// 印出，且二次格式化無法還原 → 非冪等。原始碼沒寫型別就不印。
		} else {
			f.write(" [")
			if at.Size != nil {
				f.formatExpression(at.Size)
			} else {
				f.write("?") // [?] — infer size from literal
			}
			f.write("]")
			// Only output element type if explicitly written (not inferred default i64)
			if at.Elem != nil && !elemTypeInferred(at.Elem) {
				f.write(at.Elem.String())
			}
		}
	} else if st, ok := s.Type.(*parser.SliceType); ok && !st.IsInferred {
		f.write(" []")
		// Only output element type if explicitly written (not inferred default i64)
		if st.Elem != nil && !elemTypeInferred(st.Elem) {
			f.write(st.Elem.String())
		}
	} else if nt, ok := s.Type.(*parser.NamedType); ok && nt.Value != "" && !nt.IsInferred {
		f.write(" ")
		f.write(nt.Value)
	} else if nt, ok := s.Type.(*parser.NullableType); ok && !nt.IsInferred {
		f.write(" ?")
		f.write(nt.Type.String())
	} else if mt, ok := s.Type.(*parser.MapType); ok {
		// map 型別：輸出 " [K]V"（MapType.String() 已回傳 "[K]V" 形式）
		f.write(" ")
		f.write(mt.String())
	}
	if s.Value != nil {
		f.write(" = ")
		f.stringAlign = f.column
		// 當 ArraySize > 0 且值為 ArrayLiteral（由 [1, 2, 3] 轉換而來）
		// 以切片風格輸出 [1, 2, 3]，避免重複 size
		if at, ok := s.Type.(*parser.ArrayType); ok {
			if intLit, ok := at.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
				if arr, ok := s.Value.(*parser.ArrayLiteral); ok && isSliceConverted(arr) {
					f.write("[")
					multiLine := len(arr.Elements) > 8
					if multiLine {
						f.indent++
					}
					for i, el := range arr.Elements {
						if i > 0 && !(multiLine && i%8 == 0) {
							f.write(", ")
						}
						if multiLine && i%8 == 0 {
							f.newline()
						}
						f.formatExpression(el)
						if multiLine && (i%8 == 7 || i == len(arr.Elements)-1) {
							f.write(",")
						}
					}
					if multiLine {
						f.indent--
						f.newline()
					}
					f.write("]")
				} else {
					f.formatExpression(s.Value)
				}
			} else {
				f.formatExpression(s.Value)
			}
		} else {
			f.formatExpression(s.Value)
		}
		f.stringAlign = 0
	}
}

// formatMultiAssignStatement: q, r = func(args)

// formatMultiAssignStatement: q, r = func(args)
func (f *formatter) formatMultiAssignStatement(s *parser.MultiAssignStatement) {
	for i, target := range s.Targets {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(target)
	}
	if s.Value != nil {
		f.write(" = ")
		f.formatExpression(s.Value)
	}
}

// isSliceConverted checks if ArrayLiteral was converted from SliceLiteral.
// 直接讀取 ArrayLiteral.WasSliceLiteral 欄位（由 parser 在轉換時設置），
// 避免依賴 Size.Token.Literal == "[" 的隱式 token 約定。

// isSliceConverted checks if ArrayLiteral was converted from SliceLiteral.
// 直接讀取 ArrayLiteral.WasSliceLiteral 欄位（由 parser 在轉換時設置），
// 避免依賴 Size.Token.Literal == "[" 的隱式 token 約定。
func isSliceConverted(arr *parser.ArrayLiteral) bool {
	return arr.WasSliceLiteral
}

// elemTypeInformed 判斷陣列/切片的元素型別是否為 parser 推斷。
// 直接讀取 IsInferred 欄位（parser 在推斷位置顯式設置），
// 避免依賴 token 位置相等啟發式。

// elemTypeInformed 判斷陣列/切片的元素型別是否為 parser 推斷。
// 直接讀取 IsInferred 欄位（parser 在推斷位置顯式設置），
// 避免依賴 token 位置相等啟發式。
func elemTypeInferred(t parser.Type) bool {
	switch typ := t.(type) {
	case *parser.NamedType:
		return typ.IsInferred
	case *parser.SliceType:
		return typ.IsInferred
	case *parser.ArrayType:
		return typ.IsInferred
	case *parser.NullableType:
		return typ.IsInferred
	}
	return false
}

func (f *formatter) formatReturnStatement(s *parser.ReturnStatement) {
	f.write("return")
	if s.ReturnValue != nil {
		f.write(" ")
		f.formatExpression(s.ReturnValue)
	}
}

func (f *formatter) formatFunctionDefinition(s *parser.FunctionDefinition) {
	f.write(s.Name)
	// 只顯示明確泛型參數（大寫），跳過隱式推斷的單字母小寫泛型
	explicitGenericParams := filterExplicitGenericParams(s.GenericParams)
	if len(explicitGenericParams) > 0 {
		f.write("<")
		for i, gp := range explicitGenericParams {
			if i > 0 {
				f.write(", ")
			}
			f.write(gp)
		}
		f.write(">")
	}
	if s.ColonSyntax {
		f.write(": (")
	} else {
		f.write(" = (")
	}
	// self is now the first output parameter (Results[0]) for method definitions.
	// It is implicit and should not be rendered.
	f.formatParameters(s.Parameters, s.IsVariadic)
	f.write(")")
	results := s.Results
	if isMethodDef(s) && len(results) > 0 && results[0].Name == "self" {
		results = results[1:]
	}
	if len(results) > 0 {
		f.write(" (")
		f.formatParameters(results, false)
		f.write(")")
	}
	f.write(" {")
	// Output inline comment on the same line as the opening brace
	if s.Comment != nil && len(s.Comment.List) > 0 {
		c := s.Comment.List[0]
		f.writef("; %s", strings.TrimSpace(c.Text))
	}
	f.indent++
	// 傳入真實的 '{' 行號：{ 與首陳述之間的空白行以源碼為準（有則保留、無則
	// 不憑空插入）。此前傳 0 並依賴 hasDocComment 無條件插空行，會在源碼無
	// 空行時於函式體開頭製造多餘空行。
	f.formatBlockInner(s.Body, s.Body.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

// isMethodDef reports whether a function definition is a method.
// 直接讀取 IsMethodDef 欄位（parser 在方法定義位置顯式設置），
// 避免依賴 `strings.Contains(Name, ".")` 的字串子串啟發式。

// isMethodDef reports whether a function definition is a method.
// 直接讀取 IsMethodDef 欄位（parser 在方法定義位置顯式設置），
// 避免依賴 `strings.Contains(Name, ".")` 的字串子串啟發式。
func isMethodDef(s *parser.FunctionDefinition) bool {
	return s.IsMethodDef
}

// filterExplicitGenericParams 過濾隱式推斷的泛型參數，只保留明確聲明的泛型參數。
// 直接讀取 Identifier.IsImplicitGeneric 欄位（由 addImplicitGeneric 設置），
// 避免依賴「單字母小寫 a-z 視為隱式」的命名規則啟發式。

// filterExplicitGenericParams 過濾隱式推斷的泛型參數，只保留明確聲明的泛型參數。
// 直接讀取 Identifier.IsImplicitGeneric 欄位（由 addImplicitGeneric 設置），
// 避免依賴「單字母小寫 a-z 視為隱式」的命名規則啟發式。
func filterExplicitGenericParams(params []*parser.Identifier) []string {
	var result []string
	for _, p := range params {
		if !p.IsImplicitGeneric {
			result = append(result, p.Value)
		}
	}
	return result
}

func (f *formatter) formatParameters(params []*parser.Parameter, isVariadic bool) {
	for i, p := range params {
		if i > 0 {
			f.write(", ")
		}
		f.write(p.Name)
		if p.Type != nil {
			f.write(" ")
			if isVariadic && i == len(params)-1 {
				f.write("..")
				if st, ok := p.Type.(*parser.SliceType); ok {
					f.write(st.Elem.String())
				} else {
					f.write(p.Type.String())
				}
			} else {
				f.write(p.Type.String())
			}
		}
		if p.DefaultExpr != nil {
			f.write(" = ")
			f.formatExpression(p.DefaultExpr)
		}
	}
}

// formatBlockInner formats the statements inside a block body (without writing
// the enclosing braces). It handles statement filtering, blank-line preservation,
// and doc-comment spacing. The caller is responsible for writing braces and
// managing indent. openBraceLine is the source line of '{' (0 if unknown).

// formatBlockInner formats the statements inside a block body (without writing
// the enclosing braces). It handles statement filtering, blank-line preservation,
// and doc-comment spacing. The caller is responsible for writing braces and
// managing indent. openBraceLine is the source line of '{' (0 if unknown).
func (f *formatter) formatBlockInner(body *parser.BlockStatement, openBraceLine int) {
	// 過濾掉 ; 分隔符產生的空表達式語句及 compiler 注入的合成語句
	statements := make([]parser.Statement, 0, len(body.Statements))
	for _, stmt := range body.Statements {
		if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
			continue
		}
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
			continue
		}
		statements = append(statements, stmt)
	}

	// 追踪上一個「有輸出」的陳述，使無輸出的陳述（如已生效的區塊級
	// overflow 註解）不會產生空白行，且空白行保留以最後一個有輸出的陳述為基準。
	lastEmitEndLine := 0
	var lastEmitStmt parser.Statement
	prevEmitted := false
	for i, stmt := range statements {
		emits := f.statementEmitsSomething(stmt)
		if i > 0 {
			if emits {
				prevTokenLine := stmtTokenLine(statements[i-1])
				currTokenLine := stmtTokenLine(stmt)
				if prevEmitted && prevTokenLine > 0 && prevTokenLine == currTokenLine {
					// Same line: never emit ';' (reserved for comments); split onto a new line.
					f.newline()
				} else {
					prevEndLine := lastEmitEndLine
					// When the last emitting statement's end line is unknown,
					// fall back to the block's opening brace line.
					if prevEndLine == 0 {
						prevEndLine = openBraceLine
					}
					currStartLine := stmtFirstLine(stmt)
					// 帶 doc 註解的陳述一律與上方程式碼以一個空行分隔（與
					// api.go 的頂層規則一致）：註解是「下一段程式碼的標題」，
					// 沒有分隔就會與上一條陳述糊在一起（例如
					// `} (total < size)` 之後緊接 `; 截斷到實際讀取長度`）。
					// 注意：此規則**只**適用於區塊內第二條及之後的陳述；區塊
					// 首條陳述不套用，否則會在 `{` 之後憑空插入空行（見下方
					// i == 0 分支與 fmt/block_blank_test.go 的守衛）。
					// 獨立的 wildcard `-> body` 臂若緊跟在一條尚無 else 的
					// standalone if-then 之後，語意上就是該 if 的 else 分支：
					// 與「直接相鄰、被 parser 鏈成 Alternative」的情形採用
					// 相同的排版（else 另起一行時補一個空行），否則前置
					// `#{...}` 註解造成的非鏈式 AST 會與鏈式版本輸出不同 →
					// 非冪等（fmt.no / net.no 的漂移）。
					if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) || f.attachedAnnotationsWillEmit(stmt) || f.isBareElseAfterIfThen(lastEmitStmt, stmt) {
						f.write("\n") // blank line (no indent)
					}
					f.newline()
				}
			}
			// else: current statement emits nothing — skip the gap entirely so
			// it produces zero output (no leading newline, no blank line).
		} else {
			// Preserve an actual blank line between '{' and the first statement.
			// 用 hasBlankLineBetween 偵測「真實空白行」而非單純行號落差：後者會因
			// formatter 自身輸出的註解（如區塊級 #{overflow=wrap}）推移首陳述行號，
			// 導致二次格式化時誤插入空白行而非冪等。
			// 注意：不能加 `|| f.hasDocComment(stmt)`——那會在源碼無空行時於
			// `{` 與首條帶註解陳述之間憑空插入空行（頂層語句間隔由 api.go 的
			// 程式級迴圈負責，區塊內一律以源碼空白為準）。區塊內「第二條及
			// 之後」的陳述則相反：帶 doc 註解會強制空一行，見上方 i > 0 分支。
			if emits {
				firstDocStartLine := stmtFirstLine(stmt)
				if openBraceLine > 0 && f.hasBlankLineBetween(openBraceLine, firstDocStartLine) {
					f.write("\n") // blank line (no indent)
				}
				f.newline()
			}
		}
		f.formatStatement(stmt)
		if emits {
			lastEmitEndLine = stmtTokenEndLine(stmt)
			lastEmitStmt = stmt
		}
		prevEmitted = emits
	}

	// 輸出尾隨註釋（保留其與上方程式碼之間的空行）
	prevEnd := lastEmitEndLine
	if prevEnd == 0 {
		prevEnd = openBraceLine
	}
	f.formatTrailingComments(body.TrailingComments, prevEnd)
}

func (f *formatter) formatBlockStatement(s *parser.BlockStatement) {
	f.write("{")
	// Output opening brace comment on the same line as {
	if obc := f.obcOf(s); obc != nil && len(obc.List) > 0 {
		f.write("; ")
		for _, c := range obc.List {
			f.write(strings.TrimSpace(c.Text))
		}
	}
	// Empty block with no comments: output `{}` on one line
	if len(s.Statements) == 0 && s.TrailingComments == nil && s.ClosingBraceComment == nil {
		f.write("}")
		return
	}
	f.indent++
	f.formatBlockInner(s, s.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatBreakStatement(s *parser.BreakStatement) {
	// Preserve `*` shorthand if it was the original source.
	if s.Token.Type == lexer.MUL {
		f.write("*")
	} else {
		f.write("break")
	}
	// 直接讀取 LabelKind 枚舉（parser 在 LABEL/IDENT token 位置顯式設置），
	// 避免依賴 isNumericLabel 字串內容啟發式。
	// `*` 簡寫形式固定使用 `#` 前綴（如 `* #1`），與 LabelKind 無關。
	if s.Label != "" {
		if s.Token.Type == lexer.MUL || s.LabelKind == parser.LabelNumeric {
			f.write(" #")
		} else {
			f.write(" ")
		}
		f.write(s.Label)
	}
}

func (f *formatter) formatContinueStatement(s *parser.ContinueStatement) {
	// Preserve `**` shorthand if it was the original source.
	if s.Token.Type == lexer.STAR_STAR {
		f.write("**")
	} else {
		f.write("continue")
	}
	// 直接讀取 LabelKind 枚舉（parser 在 LABEL/IDENT token 位置顯式設置），
	// 避免依賴 isNumericLabel 字串內容啟發式。
	// `**` 簡寫形式固定使用 `#` 前綴（如 `** #1`），與 LabelKind 無關。
	if s.Label != "" {
		if s.Token.Type == lexer.STAR_STAR || s.LabelKind == parser.LabelNumeric {
			f.write(" #")
		} else {
			f.write(" ")
		}
		f.write(s.Label)
	}
}

func (f *formatter) formatExternStatement(s *parser.ExternStatement) {
	// 輸出 #{c} 或 #{c, extra=...} 格式
	f.write("#{")
	f.write(s.Lang)
	for _, a := range f.sem.AnnotationsOf(s) {
		f.write(", ")
		f.write(a.String())
	}
	f.write("}")
	f.newline()
	// 輸出函式宣告
	f.write(s.Name.Value)
	f.write(" = (")
	for i, p := range s.Parameters {
		if i > 0 {
			f.write(", ")
		}
		f.write(p.Name)
		if p.Type != nil {
			f.write(" ")
			f.write(p.Type.String())
		}
	}
	f.write(")")
	if len(s.Results) > 0 {
		f.write(" (")
		for i, r := range s.Results {
			if i > 0 {
				f.write(", ")
			}
			f.write(r.Name)
			if r.Type != nil {
				f.write(" ")
				f.write(r.Type.String())
			}
		}
		f.write(")")
	}
}

// formatAnnotationStatement 輸出獨立 #{...} 註解陳述。overflow 條目走 activeOverflow
// 去重（與 formatStatement 的附加註解路徑共用同一機制），避免區塊級 overflow 既以
// 獨立節點輸出、又經 side-table 合併到各陳述而重複印；平台/泛型條目則每次都輸出。
// formatAnnotationStatement 輸出獨立 #{...} 註解陳述。overflow 條目走 activeOverflow
// 去重（與 formatStatement 的附加註解路徑共用同一機制），避免區塊級 overflow 既以
// 獨立節點輸出、又經 side-table 合併到各陳述而重複印；平台/泛型條目則每次都輸出。
// 回傳是否實際輸出任何內容（區塊級 overflow 已生效時為 false，供上層跳過間隙不產生空行）。
func (f *formatter) formatAnnotationStatement(s *parser.AnnotationStatement) bool {
	if os.Getenv("NOLANG_FMTDBG") != "" {
		fmt.Fprintf(os.Stderr, "[DBG] formatAnnotationStatement standalone node, doc=%v\n", s.GetDoc() != nil)
	}
	var others []*parser.AnnotationEntry
	var overflowModes []string
	seenMode := make(map[string]bool)
	for _, e := range s.Entries {
		if e.Key == "overflow" {
			if m := overflowModeStringOf(e); m != "" && !seenMode[m] {
				seenMode[m] = true
				overflowModes = append(overflowModes, m)
			}
		} else {
			others = append(others, e)
		}
	}
	emitted := false
	if len(others) > 0 {
		f.write("#{")
		for i, e := range others {
			if i > 0 {
				f.write(", ")
			}
			f.write(e.String())
		}
		f.write("}")
		emitted = true
	}
	if len(overflowModes) > 0 {
		// 與 others 分屬不同行：若已印過 others 先換行。
		if emitted {
			f.newline()
		}
		// 獨立註解陳述：不帶尾隨換行，換行由間隙邏輯統一負責（避免與下一
		// 陳述的間隙 newline 疊加產生空行、破壞冪等）。
		if f.emitOverflowAnnotation(strings.Join(overflowModes, ","), false) {
			emitted = true
		}
	}
	return emitted
}

// attachedAnnotations returns annotations attached to a statement by the parser
// (e.g. platform annotations #{mac-arm64}, #{linux-amd64} attached via attachAnnotations),
// read from the semantic side-table.
