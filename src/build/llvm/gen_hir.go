package llvm

// gen_hir.go — HIR codegen entry point.
//
// GenerateHIR is the strangler-fig seam that makes the LLVM code generator
// consume a *hir.Package instead of the surface *parser.Program. It does so by
// converting the HIR back into an AST (hirToAST) and handing it to the existing,
// proven emit path (Generator.Generate). The inferred types that the checker
// computed in place on the AST are carried in pkg.Inferred (a side-table keyed
// by HIR node id) and restored during reconstruction, so the emitter observes
// exactly the type information it would from the original AST.
//
// First cut scope: the reconstruction covers the common statement / expression /
// type / annotation kinds. Unsupported kinds return an explicit error rather
// than dropping a node silently. Subsequent cuts teach each generateX to read
// HIR directly, shrinking then eliminating this adapter.

import (
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// GenerateHIR emits LLVM IR from a lowered HIR package. When the package
// contains a kind hirToAST cannot yet reconstruct, it returns a module that
// announces the gap instead of panicking, so `no build` fails loudly rather
// than producing a silently-wrong binary.
// GenerateHIR emits LLVM IR from a lowered HIR package. It no longer delegates to
// the surface-AST Generate: it runs the shared prepare (setup + declarations),
// then emits the top-level entry through generateMainFunction with the HIR package
// threaded through (so the emission order follows pkg.Top), and finishes with the
// shared trailing declarations. The HIR -> AST reconstruction (hirToAST) is still
// used to feed the proven emit functions until per-kind native emitters replace
// them one at a time (see the generateMainFunction doc comment and the
// strangle-fig plan). The inferred types the checker computed on the AST are
// carried in pkg.Inferred and restored during reconstruction, so the emitter sees
// exactly the type information it would from the original AST.
func (g *Generator) GenerateHIR(pkg *hir.Package) string {
	if pkg == nil {
		return ""
	}
	prog, astOf, err := hirToAST(pkg)
	if err != nil {
		return "; HIR codegen unsupported: " + err.Error() + "\n"
	}
	// 語義查找 HIR-id 鍵控：建立「重建 AST 節點 -> HIR id」反查表，使 embed /
	// annotation 查找能直接按 HIR id 解析（pkg.EmbedDataOf / pkg.AnnotationsOf），
	// 不再依賴以 AST 指標身份為 key 的側表。這是邁向原生 HIR emitter 的基礎：
	// 未來逐 kind 替換重建 AST 後，語義查找仍可由 HIR id 取得，不會靜默丟失。
	g.hirPkg = pkg
	g.hirOf = make(map[parser.Node]int32, len(astOf))
	for id, node := range astOf {
		g.hirOf[node] = id
	}
	// hirAstOf 是 hirToAST 重建的全部節點（語句 + 表達式）反查表，供 generateHIRExpr
	// 對尚未原生支援的表達式 kind 回退到重建 AST 表達式（generateExprWithSB），
	// 保證輸出與表面 AST 路徑逐位元組相同；原生 kind 則直接由 HIR 發射，不經重建。
	g.hirAstOf = astOf
	// 頂層 HIR id -> 重建 AST 語句 映射：emitTopLevelHIR 對已原生支援的 kind 直接發射，
	// 其餘（let/expr/for 等）暫以重建 AST 委派，輸出與舊路徑逐位元組相同。prepare 已按
	// pkg.Top 順序重建 prog.Statements，故索引對齊。
	g.hirStmtOf = make(map[int32]parser.Statement, len(pkg.Top))
	for i, id := range pkg.Top {
		if i < len(prog.Statements) {
			g.hirStmtOf[id] = prog.Statements[i]
		}
	}
	defer func() { g.hirPkg = nil; g.hirOf = nil; g.hirStmtOf = nil; g.hirAstOf = nil }()
	defer DFStatDump()
	var sb strings.Builder
	// 收集「可證明為純 ASCII」的字串變數（顯式 #{ascii} 註解 / ASCII 字面量 /
	// 識別字傳播）。必須在 prepare 之前完成：下標 codegen 據此在 O(1) 直接定址
	// 與 O(n) UTF-8 迭代之間擇一。HIR 模式下註解以 HIR id 為鍵，故須在
	// g.hirOf 反查表建立之後（上面已完成）才能解析 #{ascii}。
	g.collectAsciiVars(prog)
	// prepare is now statement-slice driven; the HIR path feeds it the
	// reconstructed program so declaration emission and state maps stay
	// identical to the surface-AST path. Top-level LOGIC emission order is
	// driven by pkg.Top inside generateMainFunction.
	g.prepare(prog.Statements, prog.Sem, &sb)
	g.generateMainFunction(&sb, prog, pkg)
	g.finishModule(&sb)
	if os.Getenv("NOLANG_IRDUMP") != "" {
		if f, err := os.Create("/tmp/nolang_hir.ll"); err == nil {
			f.WriteString(sb.String())
			f.Close()
		}
	}
	return sb.String()
}

// hirASTConv drives the HIR -> AST reconstruction.
type hirASTConv struct {
	pkg   *hir.Package
	astOf map[int32]parser.Node // hir id -> reconstructed AST node (for Sem attachment)
	sem   *parser.SemanticContext
}

func hirToAST(pkg *hir.Package) (*parser.Program, map[int32]parser.Node, error) {
	c := &hirASTConv{pkg: pkg, astOf: make(map[int32]parser.Node), sem: parser.NewSemanticContext()}
	prog := &parser.Program{Sem: c.sem}
	for _, id := range pkg.Top {
		s, err := c.stmt(id)
		if err != nil {
			return nil, nil, err
		}
		if s != nil {
			prog.Statements = append(prog.Statements, s)
		}
	}
	// NOTE: attachSem() and parser.ResolveProgram(prog) were removed. The HIR
	// codegen now resolves embed/annotation data exclusively through the
	// HIR-id-keyed helpers (embedDataFor/embedFilesFor/annotationsFor and
	// filterByPlatformG), which read pkg.Embeds/pkg.Anns by node id. The
	// reconstructed AST's SemanticContext is therefore intentionally left empty
	// in HIR mode; no code path must read g.sem for annotations/embeds while
	// g.hirPkg != nil. The equivalence oracle (TestGenerateHIRMatchesGenerate)
	// and the embed/platform-variant cases guard this invariant.
	return prog, c.astOf, nil
}

// ---- small accessors ----

func (c *hirASTConv) node(id int32) *hir.Node { return c.pkg.Node(id) }

func (c *hirASTConv) strOf(id int32) string {
	n := c.node(id)
	if n == nil {
		return ""
	}
	return c.pkg.Str(n.S)
}

func (c *hirASTConv) typeStrOf(id int32) string {
	n := c.node(id)
	if n == nil {
		return ""
	}
	return c.pkg.Type(n.Type)
}

func (c *hirASTConv) tok(id int32) lexer.Token {
	n := c.node(id)
	if n == nil {
		return lexer.Token{}
	}
	return lexer.Token{Line: int(n.Line), Column: int(n.Col)}
}

func (c *hirASTConv) children(id int32) []int32 { return c.pkg.Children(id) }

// slotChild returns the wrapped child of the KSlot whose label matches, or NoID.
func (c *hirASTConv) slotChild(id int32, label string) int32 {
	for _, ch := range c.children(id) {
		cn := c.node(ch)
		if cn != nil && cn.Kind == hir.KSlot && c.pkg.Str(cn.S) == label {
			return cn.First
		}
	}
	return hir.NoID
}

func (c *hirASTConv) nameOf(id int32) string {
	n := c.node(id)
	if n == nil || n.Kind != hir.KName {
		return ""
	}
	return c.pkg.Str(n.S)
}

// setInferred restores the checker-computed type (from pkg.Inferred) onto the
// reconstructed node's Type field, mirroring how nodeType reads it on the AST.
func (c *hirASTConv) setInferred(id int32, node interface{}) {
	if node == nil {
		return
	}
	t := c.pkg.InferredType(id)
	if t == "" {
		return
	}
	parsed := parseHIRType(t)
	if parsed == nil {
		return
	}
	rv := reflect.ValueOf(node)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return
	}
	ev := rv.Elem()
	if ev.Kind() != reflect.Struct {
		return
	}
	f := ev.FieldByName("Type")
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Type().Kind() != reflect.Interface {
		return
	}
	f.Set(reflect.ValueOf(parsed))
}

func (c *hirASTConv) record(id int32, node parser.Node) parser.Node {
	if node != nil {
		c.setInferred(id, node)
		c.astOf[id] = node
	}
	return node
}

// ---- statements ----

func (c *hirASTConv) stmt(id int32) (parser.Statement, error) {
	n := c.node(id)
	if n == nil {
		return nil, nil
	}
	var s parser.Statement
	var err error
	switch n.Kind {
	case hir.KUse:
		s = &parser.UseStatement{
			Token:     c.tok(id),
			Path:      c.strOf(id),
			Function:  c.pkg.Str(n.S2),
			Alias:     c.nameOf(c.slotChild(id, "alias")),
			AsKeyword: n.Has(hir.FlagAsKeyword),
		}
	case hir.KExport:
		s = &parser.ExportStatement{
			Token:     c.tok(id),
			Path:      c.strOf(id),
			Function:  c.pkg.Str(n.S2),
			Alias:     c.nameOf(c.slotChild(id, "alias")),
			AsKeyword: n.Has(hir.FlagAsKeyword),
		}
	case hir.KMultiAssign:
		mas := &parser.MultiAssignStatement{Token: c.tok(id), Value: c.expr(c.slotChild(id, "value"))}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn != nil && cn.Kind == hir.KSlot && c.pkg.Str(cn.S) == "target" {
				mas.Targets = append(mas.Targets, c.expr(cn.First))
			}
		}
		s = mas
	case hir.KUnwrapAssign:
		s = &parser.UnwrapAssignStatement{
			Token: c.tok(id),
			Name:  &parser.Identifier{Token: c.tok(id), Value: c.strOf(id)},
			Value: c.expr(n.First),
		}
	case hir.KLet:
		s = &parser.LetStatement{
			Token:         c.tok(id),
			Name:          &parser.Identifier{Token: c.tok(id), Value: c.strOf(id)},
			ItArmType:     c.pkg.Str(n.S2),
			IsSynthetic:   n.Has(hir.FlagSynthetic),
			IsModuleConst: n.Has(hir.FlagModuleConst),
			Type:          parseHIRType(c.pkg.Type(n.Type)),
			Value:         c.expr(n.First),
		}
	case hir.KReturn:
		s = &parser.ReturnStatement{Token: c.tok(id), ReturnValue: c.expr(n.First)}
	case hir.KExprStmt:
		es := &parser.ExpressionStatement{Token: c.tok(id), Expression: c.expr(n.First)}
		// 解碼 HIR 標誌位還原 #{overflow = ...} 模式（貫穿 HIR 重建）。
		if n.Has(hir.FlagOverflowWrap) {
			es.OverflowMode = "wrap"
		} else if n.Has(hir.FlagOverflowClamp0) {
			es.OverflowMode = "clamp0"
		} else if n.Has(hir.FlagOverflowMin) {
			es.OverflowMode = "min"
		} else if n.Has(hir.FlagOverflowMax) {
			es.OverflowMode = "max"
		} else if n.Has(hir.FlagOverflowSaturate) {
			es.OverflowMode = "saturate"
		}
		s = es
	case hir.KBlock:
		s = c.block(id)
	case hir.KFuncDef:
		s, err = c.funcDef(id)
	case hir.KExtern:
		es := &parser.ExternStatement{Token: c.tok(id), Name: &parser.Identifier{Token: c.tok(id), Value: c.strOf(id)}, Lang: c.pkg.Str(n.S2)}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil {
				continue
			}
			switch cn.Kind {
			case hir.KParam:
				es.Parameters = append(es.Parameters, c.param(ch))
			case hir.KResult:
				es.Results = append(es.Results, c.param(ch))
			}
		}
		s = es
	case hir.KFor:
		s = c.forStmt(id)

	case hir.KBreak:
		s = &parser.BreakStatement{Token: c.tok(id), Label: c.strOf(id), LabelKind: parser.LabelKind(n.Val)}
	case hir.KContinue:
		s = &parser.ContinueStatement{Token: c.tok(id), Label: c.strOf(id), LabelKind: parser.LabelKind(n.Val)}
	case hir.KEnumDef:
		ed := &parser.EnumDefinition{Token: c.tok(id), Name: c.strOf(id)}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil {
				continue
			}
			ed.Values = append(ed.Values, &parser.EnumValue{
				Token:    c.tok(ch),
				Name:     c.pkg.Str(cn.S),
				Value:    cn.Val,
				Explicit: cn.Has(hir.FlagExplicit),
			})
		}
		s = ed
	case hir.KTypeAlias:
		ta := &parser.TypeAlias{Token: c.tok(id), Name: c.strOf(id)}
		ts := c.typeStrOf(id)
		switch {
		case n.Has(hir.FlagUnion):
			ta.Union = &parser.UnionType{Types: []parser.Type{parseHIRType(ts)}}
		case n.Has(hir.FlagFuncType):
			// Named function-type alias (e.g. "cb = ()(i64)"): reconstruct a
			// real *parser.FunctionType, not a NamedType wrapping the "fn(...)" string.
			ta.Type = parseHIRFuncType(ts)
		case ts != "":
			ta.Type = parseHIRType(ts)
		}
		s = ta
	case hir.KTaggedEnumDef:
		td := &parser.TaggedEnumDefinition{Token: c.tok(id), Name: c.strOf(id)}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil {
				continue
			}
			td.Variants = append(td.Variants, &parser.TaggedEnumVariant{
				Token: c.tok(ch),
				Name:  c.pkg.Str(cn.S),
				Type:  parseHIRType(c.pkg.Str(cn.Type)),
				Index: cn.Val,
			})
		}
		s = td
	case hir.KInterfaceDef:
		idf := &parser.InterfaceDefinition{Token: c.tok(id), Name: c.strOf(id)}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil {
				continue
			}
			switch cn.Kind {
			case hir.KInterfaceMethod:
				idf.Methods = append(idf.Methods, c.ifaceMethod(ch))
			case hir.KSlot:
				if c.pkg.Str(cn.S) == "implements" {
					idf.Implements = append(idf.Implements, c.nameOf(cn.First))
				}
			}
		}
		s = idf
	case hir.KStructDef:
		sd := &parser.StructDefinition{Token: c.tok(id), Name: c.strOf(id)}
		for _, ch := range c.children(id) {
			sd.Fields = append(sd.Fields, c.structField(ch))
		}
		s = sd
	case hir.KAnnotation:
		as := &parser.AnnotationStatement{Token: c.tok(id)}
		for _, ch := range c.children(id) {
			if e := hirAnnEntry(c.pkg, ch); e != nil {
				as.Entries = append(as.Entries, e)
			}
		}
		s = as
	default:
		return nil, errUnsupported("statement", n.Kind)
	}
	if err != nil {
		return nil, err
	}
	return c.record(id, s).(parser.Statement), nil
}

func (c *hirASTConv) funcDef(id int32) (parser.Statement, error) {
	n := c.node(id)
	fd := &parser.FunctionDefinition{
		Token:            c.tok(id),
		Name:             c.strOf(id),
		ColonSyntax:      n.Has(hir.FlagColon),
		IsMethodDef:      n.Has(hir.FlagMethod),
		IsSkipNamingCheck: n.Has(hir.FlagSkipNaming),
	}
	// 解碼 HIR 標誌位還原 #{overflow = wrap|clamp0|min|max|saturate} 模式（貫穿單態化複本）。
	if n.Has(hir.FlagOverflowWrap) {
		fd.OverflowMode = "wrap"
	} else if n.Has(hir.FlagOverflowClamp0) {
		fd.OverflowMode = "clamp0"
	} else if n.Has(hir.FlagOverflowMin) {
		fd.OverflowMode = "min"
	} else if n.Has(hir.FlagOverflowMax) {
		fd.OverflowMode = "max"
	} else if n.Has(hir.FlagOverflowSaturate) {
		fd.OverflowMode = "saturate"
	}
	fd.IsVariadic = n.Has(hir.FlagVariadic)
	for _, ch := range c.children(id) {
		cn := c.node(ch)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			fd.Parameters = append(fd.Parameters, c.param(ch))
		case hir.KResult:
			fd.Results = append(fd.Results, c.param(ch))
		case hir.KGeneric:
			fd.GenericParams = append(fd.GenericParams, &parser.Identifier{Value: c.pkg.Str(cn.S)})
		case hir.KUnionName:
			fd.GenericUnion = c.pkg.Str(cn.S)
		case hir.KSlot:
			if c.pkg.Str(cn.S) == "body" {
				fd.Body = c.block(cn.First)
			}
		}
	}
	return fd, nil
}

func (c *hirASTConv) stmt1(id int32) parser.Statement {
	s, _ := c.stmt(id)
	return s
}

func (c *hirASTConv) forStmt(id int32) parser.Statement {
	n := c.node(id)
	fs := &parser.ForStatement{
		Token:         c.tok(id),
		Label:         c.strOf(id),
		IsCondWrapper: n.Has(hir.FlagCondWrapper),
		Init:          c.stmt1(c.slotChild(id, "init")),
		Condition:     c.expr(c.slotChild(id, "cond")),
		Update:        c.stmt1(c.slotChild(id, "update")),
		Body:          c.block(c.slotChild(id, "body")),
		IterRange:     c.iterationExpr(c.slotChild(id, "iter")),
		CountExpr:     c.expr(c.slotChild(id, "count")),
	}
	// 解碼 HIR 標誌位還原 #{overflow = wrap|clamp0|min|max|saturate} 模式（貫穿 HIR 重建）。
	if n.Has(hir.FlagOverflowWrap) {
		fs.OverflowMode = "wrap"
	} else if n.Has(hir.FlagOverflowClamp0) {
		fs.OverflowMode = "clamp0"
	} else if n.Has(hir.FlagOverflowMin) {
		fs.OverflowMode = "min"
	} else if n.Has(hir.FlagOverflowMax) {
		fs.OverflowMode = "max"
	} else if n.Has(hir.FlagOverflowSaturate) {
		fs.OverflowMode = "saturate"
	}
	return fs
}

func (c *hirASTConv) block(id int32) *parser.BlockStatement {
	n := c.node(id)
	if n == nil || n.Kind != hir.KBlock {
		return nil
	}
	bs := &parser.BlockStatement{Token: c.tok(id), IsInline: n.Has(hir.FlagInline)}
	for _, ch := range c.children(id) {
		if st, err := c.stmt(ch); err == nil && st != nil {
			bs.Statements = append(bs.Statements, st)
		}
	}
	return bs
}

func (c *hirASTConv) param(id int32) *parser.Parameter {
	n := c.node(id)
	if n == nil {
		return nil
	}
	return &parser.Parameter{
		Token:       c.tok(id),
		Name:        c.strOf(id),
		Type:        parseHIRType(c.typeStrOf(id)),
		DefaultExpr: c.expr(n.First),
	}
}

func (c *hirASTConv) structField(id int32) *parser.StructField {
	n := c.node(id)
	if n == nil {
		return nil
	}
	return &parser.StructField{
		Token:     c.tok(id),
		Name:      c.strOf(id),
		Type:      parseHIRType(c.typeStrOf(id)),
		ArraySize: n.Val,
		ReadOnly:  n.Has(hir.FlagReadOnly),
		Sealed:    n.Has(hir.FlagSealed),
		IsSlice:   n.Has(hir.FlagSlice),
		Value:     c.expr(n.First),
	}
}

func (c *hirASTConv) ifaceMethod(id int32) *parser.InterfaceMethod {
	n := c.node(id)
	if n == nil {
		return nil
	}
	im := &parser.InterfaceMethod{
		Token:              c.tok(id),
		Name:               c.strOf(id),
		Receiver:           c.pkg.Str(n.S2),
		IsVariadic:         n.Has(hir.FlagVariadic),
		IsGenericReceiver:  n.Has(hir.FlagGenericReceiver),
	}
	for _, ch := range c.children(id) {
		cn := c.node(ch)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			im.Parameters = append(im.Parameters, c.param(ch))
		case hir.KResult:
			im.Results = append(im.Results, c.param(ch))
		}
	}
	return im
}

func (c *hirASTConv) rangeExpr(id int32) *parser.RangeExpression {
	n := c.node(id)
	if n == nil || n.Kind != hir.KRange {
		return nil
	}
	re := &parser.RangeExpression{Token: c.tok(id), LeftInc: n.Has(hir.FlagLeftInc), RightInc: n.Has(hir.FlagRightInc)}
	// Start/End are stored as named slots ("start"/"end"); either may be absent
	// for an open range (..5 or 5..). Absent slot reads as NoID -> leave nil.
	if s := c.slotChild(id, "start"); s != hir.NoID {
		re.Start = c.expr(s)
	}
	if e := c.slotChild(id, "end"); e != hir.NoID {
		re.End = c.expr(e)
	}
	return re
}

func (c *hirASTConv) iterationExpr(id int32) *parser.IterationExpr {
	n := c.node(id)
	if n == nil || n.Kind != hir.KIter {
		return nil
	}
	kids := c.children(id)
	ie := &parser.IterationExpr{Token: c.tok(id), Variable: c.strOf(id), RangeStr: c.pkg.Str(n.S2)}
	// tohir.go stores [Range, RangeExpr] positionally, but Builder.List drops
	// NoID children. When Range is nil (array/string iteration) the leading
	// NoID is dropped, so the surviving child is the RangeExpr, not Range.
	// Dispatch by node kind: a KRange child is the numeric Range, anything
	// else is the collection RangeExpr. This stays correct regardless of which
	// optional operand is present or dropped.
	for _, k := range kids {
		kn := c.node(k)
		if kn == nil {
			continue
		}
		if kn.Kind == hir.KRange {
			ie.Range = c.rangeExpr(k)
		} else {
			ie.RangeExpr = c.expr(k)
		}
	}
	return ie
}

// ---- expressions ----

func (c *hirASTConv) expr(id int32) parser.Expression {
	n := c.node(id)
	if n == nil {
		return nil
	}
	var e parser.Expression
	switch n.Kind {
	case hir.KIdent:
		e = &parser.Identifier{Token: c.tok(id), Value: c.strOf(id), IsImplicitGeneric: n.Has(hir.FlagImplicitGeneric)}
	case hir.KIntLit:
		e = &parser.IntegerLiteral{Token: c.tok(id), Value: n.Val, Raw: c.strOf(id)}
		e.(*parser.IntegerLiteral).Token.Literal = c.pkg.Str(n.S2)
	case hir.KByteLit:
		e = &parser.ByteLiteral{Token: c.tok(id), Value: n.Val, Raw: c.strOf(id)}
	case hir.KFloatLit:
		e = &parser.FloatLiteral{Token: c.tok(id), Value: n.Float(), Raw: c.strOf(id)}
	case hir.KStrLit:
		e = &parser.StringLiteral{Token: c.tok(id), Value: c.strOf(id), Raw: c.pkg.Str(n.S2)}
	case hir.KCharLit:
		e = &parser.CharLiteral{Token: c.tok(id), Value: c.strOf(id), Raw: c.pkg.Str(n.S2)}
	case hir.KRegexLit:
		e = &parser.RegexLiteral{Token: c.tok(id), Pattern: c.strOf(id), Flags: c.pkg.Str(n.S2)}
	case hir.KBoolLit:
		e = &parser.BooleanLiteral{Token: c.tok(id), Value: n.Val != 0}
	case hir.KNilLit:
		e = &parser.NilLiteral{Token: c.tok(id)}
	case hir.KPrefix:
		e = &parser.PrefixExpression{Token: c.tok(id), Operator: c.strOf(id), Right: c.expr(n.First)}
	case hir.KInfix:
		kids := c.children(id)
		ie := &parser.InfixExpression{Token: c.tok(id), Operator: c.strOf(id)}
		if len(kids) > 0 {
			ie.Left = c.expr(kids[0])
		}
		if len(kids) > 1 {
			ie.Right = c.expr(kids[1])
		}
		e = ie
	case hir.KRun:
		e = &parser.RunExpression{Token: c.tok(id), Call: c.expr(n.First)}
	case hir.KAwait:
		e = &parser.AwaitExpression{Token: c.tok(id), Right: c.expr(n.First)}
	case hir.KGrouped:
		e = &parser.GroupedExpression{Token: c.tok(id), Expression: c.expr(n.First)}
	case hir.KIf:
		e = c.ifExpr(id)
	case hir.KRange:
		e = c.rangeExpr(id)
	case hir.KSlice:
		se := &parser.SliceExpression{Token: c.tok(id)}
		// tohir stores children positionally as [Left, Range], but Builder.List
		// drops NoID children. When Left is nil (e.g. s[..5]) the surviving
		// child is the KRange, and a positional assignment would wrongly put it
		// in se.Left while leaving se.Range nil. Dispatch by node kind instead:
		// a KRange child is the range, anything else is the start expression.
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil {
				continue
			}
			if cn.Kind == hir.KRange {
				se.Range = c.rangeExpr(ch)
			} else {
				se.Left = c.expr(ch)
			}
		}
		e = se
	case hir.KIndex:
		kids := c.children(id)
		ie := &parser.IndexExpression{Token: c.tok(id)}
		if len(kids) > 0 {
			ie.Left = c.expr(kids[0])
		}
		if len(kids) > 1 {
			ie.Index = c.expr(kids[1])
		}
		e = ie
	case hir.KAssign:
		kids := c.children(id)
		ae := &parser.AssignExpression{Token: c.tok(id)}
		if len(kids) > 0 {
			ae.Left = c.expr(kids[0])
		}
		if len(kids) > 1 {
			ae.Value = c.expr(kids[1])
		}
		e = ae
	case hir.KCond:
		kids := c.children(id)
		ce := &parser.ConditionalExpression{Token: c.tok(id)}
		if len(kids) > 0 {
			ce.Condition = c.expr(kids[0])
		}
		if len(kids) > 1 {
			ce.Consequence = c.expr(kids[1])
		}
		if len(kids) > 2 {
			ce.Alternative = c.expr(kids[2])
		}
		e = ce
	case hir.KCast:
		e = &parser.CastExpression{Token: c.tok(id), Type: parseHIRType(c.typeStrOf(id)), Expr: c.expr(n.First)}
	case hir.KIter:
		e = c.iterationExpr(id)
	case hir.KArrayLit:
		al := &parser.ArrayLiteral{Token: c.tok(id), Size: c.expr(c.slotChild(id, "size")), WasSliceLiteral: n.Has(hir.FlagWasSlice)}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn != nil && cn.Kind == hir.KSlot && c.pkg.Str(cn.S) == "elem" {
				al.Elements = append(al.Elements, c.expr(cn.First))
			}
		}
		e = al
	case hir.KMapLit:
		ml := &parser.MapLiteral{Token: c.tok(id)}
		if mt, ok := parseHIRType(c.typeStrOf(id)).(*parser.MapType); ok {
			ml.MapType = mt
		}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil || cn.Kind != hir.KMapPair {
				continue
			}
			pk := c.children(ch)
			mp := parser.MapPair{Token: c.tok(ch)}
			if len(pk) > 0 {
				mp.Key = c.expr(pk[0])
			}
			if len(pk) > 1 {
				mp.Value = c.expr(pk[1])
			}
			ml.Pairs = append(ml.Pairs, mp)
		}
		e = ml
	case hir.KSliceLit:
		sl := &parser.SliceLiteral{Token: c.tok(id)}
		for _, ch := range c.children(id) {
			if ex := c.expr(ch); ex != nil {
				sl.Elements = append(sl.Elements, ex)
			}
		}
		e = sl
	case hir.KStructLit:
		sl := &parser.StructLiteral{Token: c.tok(id), Type: c.strOf(id)}
		for _, ch := range c.children(id) {
			if sf := c.structField(ch); sf != nil {
				sl.Fields = append(sl.Fields, sf)
			}
		}
		e = sl
	case hir.KCall:
		ce := &parser.CallExpression{Token: c.tok(id), Function: c.expr(c.slotChild(id, "fn"))}
		for _, ch := range c.children(id) {
			cn := c.node(ch)
			if cn == nil || cn.Kind != hir.KSlot {
				continue
			}
			switch c.pkg.Str(cn.S) {
			case "gen":
				ce.GenericArgs = append(ce.GenericArgs, c.expr(cn.First))
			case "arg":
				ce.Arguments = append(ce.Arguments, c.expr(cn.First))
			}
		}
		e = ce
	case hir.KDot:
		e = &parser.DotExpression{Token: c.tok(id), Receiver: c.expr(n.First), Property: c.strOf(id)}
	case hir.KFuncLit:
		e, _ = c.funcLit(id)
	default:
		// Unsupported expression kind: surface as a build error below.
		return nil
	}
	if e == nil {
		return nil
	}
	c.setInferred(id, e)
	c.astOf[id] = e
	return e
}

func (c *hirASTConv) ifExpr(id int32) parser.Expression {
	ie := &parser.IfExpression{
		Token:          c.tok(id),
		Condition:      c.expr(c.slotChild(id, "cond")),
		Consequence:    c.block(c.slotChild(id, "then")),
		Alternative:    c.block(c.slotChild(id, "else")),
		MatchedExpr:    c.expr(c.slotChild(id, "matched")),
		DotValBody:     c.block(c.slotChild(id, "dot-val")),
		RangePattern:   c.rangeExpr(c.slotChild(id, "range")),
		EqualityPattern: c.expr(c.slotChild(id, "eq")),
		RawCond:        c.expr(c.slotChild(id, "raw")),
	}
	return ie
}

func (c *hirASTConv) funcLit(id int32) (parser.Expression, error) {
	n := c.node(id)
	fl := &parser.FunctionLiteral{Token: c.tok(id)}
	fl.IsVariadic = n.Has(hir.FlagVariadic)
	for _, ch := range c.children(id) {
		cn := c.node(ch)
		if cn == nil {
			continue
		}
		switch cn.Kind {
		case hir.KParam:
			fl.Parameters = append(fl.Parameters, c.param(ch))
		case hir.KResult:
			fl.Results = append(fl.Results, c.param(ch))
		case hir.KGeneric:
			fl.GenericParams = append(fl.GenericParams, &parser.Identifier{Value: c.pkg.Str(cn.S)})
		case hir.KUnionName:
			fl.VariadicUnion = c.pkg.Str(cn.S)
		case hir.KSlot:
			if c.pkg.Str(cn.S) == "body" {
				fl.Body = c.block(cn.First)
			}
		}
	}
	return fl, nil
}

// ---- annotations / embed ----

// hirTok reconstructs a lexer.Token from a HIR node's line/column.
func hirTok(n *hir.Node) lexer.Token {
	if n == nil {
		return lexer.Token{}
	}
	return lexer.Token{Line: int(n.Line), Column: int(n.Col)}
}

func hirAnnEntry(pkg *hir.Package, id int32) *parser.AnnotationEntry {
	n := pkg.Node(id)
	if n == nil {
		return nil
	}
	entry := &parser.AnnotationEntry{Key: pkg.Str(n.S)}
	if n.First != hir.NoID {
		entry.Value = hirAnnValue(pkg, n.First)
	}
	return entry
}

func hirAnnValue(pkg *hir.Package, id int32) parser.AnnotationValue {
	n := pkg.Node(id)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case hir.KBoolLit:
		return &parser.AnnotationBoolValue{Token: hirTok(n)}
	case hir.KIntLit:
		return &parser.AnnotationIntValue{Value: n.Val}
	case hir.KStrLit:
		return &parser.AnnotationStringValue{Value: pkg.Str(n.S)}
	case hir.KIdent:
		return &parser.AnnotationIdentValue{Value: pkg.Str(n.S)}
	case hir.KArrayLit:
		arr := &parser.AnnotationArrayValue{}
		for _, ch := range pkg.Children(id) {
			if v := hirAnnValue(pkg, ch); v != nil {
				arr.Elements = append(arr.Elements, v)
			}
		}
		return arr
	case hir.KRange:
		kids := pkg.Children(id)
		rv := &parser.AnnotationRangeValue{LeftInc: n.Has(hir.FlagLeftInc), RightInc: n.Has(hir.FlagRightInc)}
		if len(kids) > 0 {
			rv.Start = hirAnnValue(pkg, kids[0])
		}
		if len(kids) > 1 {
			rv.End = hirAnnValue(pkg, kids[1])
		}
		return rv
	}
	return nil
}

// hirAnnotationEntries reconstructs the platform/metadata annotation entries
// attached to the HIR node id, mirroring the loop attachSem runs over pkg.Anns.
// The codegen's HIR-id-keyed annotation lookup (Generator.annotationsFor) uses
// this instead of the reconstructed-AST semantic side-table.
func hirAnnotationEntries(pkg *hir.Package, id int32) []*parser.AnnotationEntry {
	if pkg == nil || id <= hir.NoID {
		return nil
	}
	i := sort.Search(len(pkg.Anns), func(i int) bool { return pkg.Anns[i].Owner >= id })
	if i >= len(pkg.Anns) || pkg.Anns[i].Owner != id {
		return nil
	}
	var entries []*parser.AnnotationEntry
	for _, ch := range pkg.Children(pkg.Anns[i].Node) {
		if e := hirAnnEntry(pkg, ch); e != nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// parseHIRType reconstructs an AST Type from a rendered type string. It mirrors
// the shapes tohir.go interns, covering named / nullable / array / slice /
// map / pointer forms used in practice.
func parseHIRType(s string) parser.Type {
	if s == "" {
		return nil
	}
	switch {
	case strings.HasPrefix(s, "fn("):
		return parseHIRFuncType(s)
	case strings.HasPrefix(s, "?"):
		return &parser.NullableType{Type: parseHIRType(strings.TrimPrefix(s, "?"))}
	case strings.HasPrefix(s, "["):
		close := strings.Index(s, "]")
		if close < 0 {
			return &parser.NamedType{Value: s}
		}
		inner := s[close+1:]
		key := s[1:close]
		if key == "" {
			return &parser.SliceType{Elem: parseHIRType(inner)}
		}
		if key == "?" {
			// Dynamic / unsized array: Size == nil renders as "[?]Elem"
			// (see parser.ArrayType.String). Must NOT be treated as a map
			// with a "?" key, which would produce a degenerate NullableType.
			return &parser.ArrayType{Elem: parseHIRType(inner)}
		}
		if isAllDigits(key) {
			return &parser.ArrayType{Size: &parser.IntegerLiteral{Value: atoi64(key)}, Elem: parseHIRType(inner)}
		}
		return &parser.MapType{Key: parseHIRType(key), Value: parseHIRType(inner)}
	case strings.HasPrefix(s, "*"):
		return &parser.PointerType{Type: parseHIRType(strings.TrimPrefix(s, "*"))}
	}
	return &parser.NamedType{Value: s}
}

// splitTopLevelComma splits s on commas that are not nested inside () or []
// brackets, so a function-type param list like "i64,fn(i64)(str),[3]u8" is
// split into its three logical parameters rather than on the inner commas.
func splitTopLevelComma(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseHIRFuncType reconstructs a *parser.FunctionType from its rendered form
// "fn(Params)(Results)" (see parser.FunctionType.String). Each param/result is
// parsed recursively via parseHIRType, so nested function/array/map types work.
func parseHIRFuncType(s string) *parser.FunctionType {
	ft := &parser.FunctionType{}
	s = strings.TrimPrefix(s, "fn")
	// Extract the params section "(...)".
	paramsEnd := strings.Index(s, ")")
	if !strings.HasPrefix(s, "(") || paramsEnd < 0 {
		return ft
	}
	paramsStr := s[1:paramsEnd]
	rest := s[paramsEnd+1:]
	for _, ps := range splitTopLevelComma(paramsStr) {
		ps = strings.TrimSpace(ps)
		if ps == "" {
			continue
		}
		ft.Params = append(ft.Params, &parser.Parameter{Type: parseHIRType(ps)})
	}
	// Optional results section "(...)".
	if strings.HasPrefix(rest, "(") {
		resEnd := strings.Index(rest, ")")
		if resEnd >= 0 {
			resStr := rest[1:resEnd]
			for _, rs := range splitTopLevelComma(resStr) {
				rs = strings.TrimSpace(rs)
				if rs == "" {
					continue
				}
				ft.Results = append(ft.Results, &parser.Parameter{Type: parseHIRType(rs)})
			}
		}
	}
	return ft
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func atoi64(s string) int64 {
	var v int64
	for _, r := range s {
		v = v*10 + int64(r-'0')
	}
	return v
}

type hirGenError struct {
	kind string
	k    hir.Kind
}

func (e *hirGenError) Error() string {
	return e.kind + " kind " + e.k.String()
}

func errUnsupported(kind string, k hir.Kind) error {
	return &hirGenError{kind: kind, k: k}
}
