package parser

import (
	"fmt"
	"reflect"

	"github.com/lizongying/nolang/hir"
)

// ASTToHIR lowers a parsed Program into an immutable HIR package.
//
// Layout conventions, per node kind. S is the primary name, S2 the secondary,
// Type is the interned *declared* type string (never an inferred type), Val
// holds integer/bool/enum payloads, and children are either positional (safe
// because the leading ones are always present) or wrapped in a labelled
// hir.KSlot when any of them is optional or repeatable.
//
//	use/export   S=path   S2=function   slot "alias"
//	let          S=name   Type=declared  S2=it-arm-type  child=value?
//	fn           S=name   S2=variadic-union  child=param* result* generic* union-name? slot "body"
//	extern       S=name   S2=lang       child=param* result*
//	for          S=label                slot init/cond/update/body/iter/count
//	if                                  slot cond/then/else/matched/dot-val/range/eq/raw/opt/val
//	call                                slot fn/gen*/arg*
//	array-lit                           slot size/elem*
//	multi-assign                        slot value/target*
//	struct       S=name                child=field*
//	enum         S=name                child=enum-value*
//	tagged-enum  S=name                child=variant*
//	interface    S=name                child=iface-method* slot "implements"
//	alias        S=name   Type=target   FlagUnion when a union, FlagFuncType when a fn type
//
// Type nodes are intentionally not emitted as children: HIR is an untyped
// tree, and inferred types belong in a side TypeMap keyed by node id (stage 3
// of the HIR plan), not in the tree itself.
//
// #{...} annotations are not statements in the AST; the parser files them in
// prog.Sem keyed by AST pointer. They are re-keyed onto node ids in
// Package.Anns, because codegen uses them to drop statements that do not match
// the target platform.
func ASTToHIR(prog *Program) *hir.Package {
	pkg, _ := ASTToHIRWithMap(prog)
	return pkg
}

// ASTToHIRWithMap lowers a parsed Program into an immutable HIR package and
// returns the AST-node -> HIR-id map. The map lets the checker record inferred
// types under HIR node ids (parser.PopulateInferredTypes), keeping codegen
// free of AST pointers. ASTToHIR discards the map for callers that don't need it.
func ASTToHIRWithMap(prog *Program) (*hir.Package, map[Node]int32) {
	b := hir.NewBuilder("")
	c := &hirConv{b: b}
	if prog == nil {
		return b.Package(), c.astOf
	}
	c.sem = prog.Sem
	for _, stmt := range prog.Statements {
		c.top(c.stmt(stmt))
	}
	return b.Package(), c.astOf
}

type hirConv struct {
	b     *hir.Builder
	sem   *SemanticContext
	astOf map[Node]int32 // AST node -> HIR id, for checker-side TypeMap keying
}

// remember records the HIR id assigned to an AST node so the checker can later
// key its inferred types by HIR node id (see parser.PopulateInferredTypes).
func (c *hirConv) remember(n Node, id int32) {
	if n == nil || id == hir.NoID {
		return
	}
	if c.astOf == nil {
		c.astOf = make(map[Node]int32, 256)
	}
	c.astOf[n] = id
}

// nodeType extracts the declared/inferred Type of an AST node via reflection so
// PopulateInferredTypes need not enumerate every node kind. Most type-carrying
// nodes expose a `Type parser.Type` field; those that don't return nil and are
// skipped. Reflection is safe here: the set of AST types is closed and the
// field is always exported and of interface type parser.Type.
func nodeType(n Node) Type {
	rv := reflect.ValueOf(n)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	f := rv.FieldByName("Type")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	if t, ok := f.Interface().(Type); ok {
		return t
	}
	return nil
}

// PopulateInferredTypes fills pkg.Inferred with the checker's inferred types,
// keyed by HIR node id, using the AST-node -> HIR-id map returned by
// ASTToHIRWithMap. Call it AFTER type checking has mutated the AST nodes' Type
// fields (the checker infers in place). This is stage 3 of the HIR plan: the
// inferred TypeMap lets codegen read types without holding AST pointers.
func PopulateInferredTypes(pkg *hir.Package, idMap map[Node]int32) {
	if pkg == nil || len(idMap) == 0 {
		return
	}
	for astNode, hirID := range idMap {
		if t := nodeType(astNode); t != nil && t.String() != "" {
			pkg.SetInferred(hirID, t.String())
		}
	}
}

// annotate copies the annotation entries the parser filed for n onto id.
// Called for every lowered statement and struct field, i.e. exactly the nodes
// SetRawAnnotations is called on.
func (c *hirConv) annotate(n Node, id int32) int32 {
	if c.sem == nil || id == hir.NoID || isNil(n) {
		return id
	}
	entries := c.sem.AnnotationsOf(n)
	if len(entries) == 0 {
		return id
	}
	nodes := make([]int32, 0, len(entries))
	for _, e := range entries {
		nodes = append(nodes, c.annEntry(e))
	}
	c.b.AddAnn(id, c.b.Add(hir.Node{Kind: hir.KAnnotation, First: c.kids(nodes)}))
	return id
}

func (c *hirConv) top(id int32) { c.b.AddTop(id) }

// slot wraps child in a labelled hir.KSlot, returning NoID when child is
// absent so that missing optional operands add no nodes to the arena.
func (c *hirConv) slot(label string, child int32) int32 {
	if child == hir.NoID {
		return hir.NoID
	}
	return c.b.Add(hir.Node{Kind: hir.KSlot, S: c.b.Intern(label), First: child})
}

// kids converts a slice then links it as a sibling chain.
func (c *hirConv) kids(nodes []int32) int32 { return c.b.List(nodes) }

func (c *hirConv) typeID(t Type) int32 {
	if isNil(t) {
		return hir.NoID
	}
	return c.b.InternType(t.String())
}

func pos(n Node) (line, col int32) {
	if isNil(n) {
		return 0, 0
	}
	p := n.Pos()
	return int32(p.Line), int32(p.Column)
}

// ---- statements ----

// stmt lowers a statement and attaches whatever annotations the parser filed
// for it. The annotation hook lives here rather than in each case so a new
// statement kind cannot forget it.
func (c *hirConv) stmt(s Statement) int32 {
	id := c.annotate(s, c.stmtNode(s))
	c.embed(s, id)
	c.remember(s, id)
	return id
}

// embed re-keys the parser's compile-time embed data (single-file bytes and
// directory maps, stored in Sem keyed by AST pointer) onto the lowered node id.
// Called for every statement, but only let-statements carry embed data today.
func (c *hirConv) embed(n Node, id int32) {
	if c.sem == nil || id == hir.NoID || isNil(n) {
		return
	}
	data := c.sem.EmbedDataOf(n)
	files := c.sem.EmbedFilesOf(n)
	if len(data) == 0 && len(files) == 0 {
		return
	}
	c.b.AddEmbed(id, data, files)
}

func (c *hirConv) stmtNode(s Statement) int32 {
	if isNil(s) {
		return hir.NoID
	}
	line, col := pos(s)

	switch s := s.(type) {
	case *UseStatement:
		return c.b.Add(hir.Node{
			Kind:  hir.KUse,
			S:     c.b.Intern(s.Path),
			S2:    c.b.Intern(s.Function),
			Flags: hir.FlagAsKeyword * u32(s.AsKeyword),
			First: c.slot("alias", c.b.Add(hir.Node{Kind: hir.KName, S: c.b.Intern(s.Alias)})),
			Line:  line, Col: col,
		})

	case *ExportStatement:
		return c.b.Add(hir.Node{
			Kind:  hir.KExport,
			S:     c.b.Intern(s.Path),
			S2:    c.b.Intern(s.Function),
			Flags: hir.FlagAsKeyword * u32(s.AsKeyword),
			First: c.slot("alias", c.b.Add(hir.Node{Kind: hir.KName, S: c.b.Intern(s.Alias)})),
			Line:  line, Col: col,
		})

	case *MultiAssignStatement:
		nodes := make([]int32, 0, len(s.Targets)+1)
		nodes = append(nodes, c.slot("value", c.expr(s.Value)))
		for _, t := range s.Targets {
			nodes = append(nodes, c.slot("target", c.expr(t)))
		}
		return c.b.Add(hir.Node{Kind: hir.KMultiAssign, First: c.kids(nodes), Line: line, Col: col})

	case *UnwrapAssignStatement:
		return c.b.Add(hir.Node{
			Kind:  hir.KUnwrapAssign,
			S:     c.b.Intern(s.TargetName()),
			First: c.expr(s.Value),
			Line:  line, Col: col,
		})

	case *LetStatement:
		return c.b.Add(hir.Node{
			Kind: hir.KLet,
			S:    c.b.Intern(idValue(s.Name)),
			S2:   c.b.Intern(s.ItArmType),
			Type: c.typeID(s.Type),
			Flags: hir.FlagSynthetic*u32(s.IsSynthetic) |
				hir.FlagModuleConst*u32(s.IsModuleConst),
			First: c.expr(s.Value),
			Line:  line, Col: col,
		})

	case *ReturnStatement:
		return c.b.Add(hir.Node{Kind: hir.KReturn, First: c.expr(s.ReturnValue), Line: line, Col: col})

	case *ExpressionStatement:
		return c.b.Add(hir.Node{Kind: hir.KExprStmt, First: c.expr(s.Expression), Line: line, Col: col, Flags: overflowModeFlags(s.OverflowMode)})

	case *BlockStatement:
		stmts := make([]int32, 0, len(s.Statements))
		for _, st := range s.Statements {
			stmts = append(stmts, c.stmt(st))
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KBlock,
			Flags: hir.FlagInline * u32(s.IsInline),
			First: c.kids(stmts),
			Line:  line, Col: col,
		})

	case *FunctionDefinition:
		return c.funcLike(hir.KFuncDef, &s.FuncSignature, s.Name, s.Body, s.VariadicUnion, line, col,
			hir.FlagMethod*u32(s.IsMethodDef)|
				hir.FlagColon*u32(s.ColonSyntax)|
				hir.FlagSkipNaming*u32(s.IsSkipNamingCheck)|
				overflowModeFlags(s.OverflowMode))

	case *ExternStatement:
		nodes := make([]int32, 0, len(s.Parameters)+len(s.Results))
		for _, p := range s.Parameters {
			nodes = append(nodes, c.param(p, hir.KParam))
		}
		for _, r := range s.Results {
			nodes = append(nodes, c.param(r, hir.KResult))
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KExtern,
			S:     c.b.Intern(idValue(s.Name)),
			S2:    c.b.Intern(s.Lang),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *AnnotationStatement:
		nodes := make([]int32, 0, len(s.Entries))
		for _, e := range s.Entries {
			nodes = append(nodes, c.annEntry(e))
		}
		return c.b.Add(hir.Node{Kind: hir.KAnnotation, First: c.kids(nodes), Line: line, Col: col})

	case *ForStatement:
		nodes := []int32{
			c.slot("init", c.stmt(s.Init)),
			c.slot("cond", c.expr(s.Condition)),
			c.slot("update", c.stmt(s.Update)),
			c.slot("body", c.stmt(s.Body)),
			c.slot("iter", c.expr(s.IterRange)),
			c.slot("count", c.expr(s.CountExpr)),
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KFor,
			S:     c.b.Intern(s.Label),
			Flags: hir.FlagCondWrapper*u32(s.IsCondWrapper) | overflowModeFlags(s.OverflowMode),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *BreakStatement:
		return c.b.Add(hir.Node{
			Kind: hir.KBreak, S: c.b.Intern(s.Label),
			Val: int64(s.LabelKind), Line: line, Col: col,
		})

	case *ContinueStatement:
		return c.b.Add(hir.Node{
			Kind: hir.KContinue, S: c.b.Intern(s.Label),
			Val: int64(s.LabelKind), Line: line, Col: col,
		})

	case *EnumDefinition:
		nodes := make([]int32, 0, len(s.Values))
		for _, v := range s.Values {
			if v == nil {
				continue
			}
			nodes = append(nodes, c.b.Add(hir.Node{
				Kind:  hir.KEnumValue,
				S:     c.b.Intern(v.Name),
				Val:   v.Value,
				Flags: hir.FlagExplicit * u32(v.Explicit),
				Line:  int32(v.Token.Line), Col: int32(v.Token.Column),
			}))
		}
		return c.b.Add(hir.Node{Kind: hir.KEnumDef, S: c.b.Intern(s.Name), First: c.kids(nodes), Line: line, Col: col})

	case *TypeAlias:
		n := hir.Node{Kind: hir.KTypeAlias, S: c.b.Intern(s.Name), Line: line, Col: col}
		switch {
		case s.Union != nil:
			n.Flags |= hir.FlagUnion
			n.Type = c.b.InternType(s.Union.String())
		case s.Type != nil:
			if _, ok := s.Type.(*FunctionType); ok {
				n.Flags |= hir.FlagFuncType
			}
			n.Type = c.b.InternType(s.Type.String())
		}
		return c.b.Add(n)

	case *TaggedEnumDefinition:
		nodes := make([]int32, 0, len(s.Variants))
		for _, v := range s.Variants {
			if v == nil {
				continue
			}
			// 載荷欄位（括號形式 ok(v t) / rect(w f64, h f64)）以子節點
			// （KStructField）附於變體節點下，讓 HIR 往返對多欄位載荷無損；
			// 單載荷的 Type 仍照舊填入，保持既有單欄位行為逐位元組不變。
			fieldNodes := make([]int32, 0, len(v.Fields))
			for _, f := range v.Fields {
				if f == nil {
					continue
				}
				fieldNodes = append(fieldNodes, c.b.Add(hir.Node{
					Kind: hir.KStructField,
					S:    c.b.Intern(f.Name),
					Type: c.typeID(f.Type),
					Line: int32(f.Token.Line), Col: int32(f.Token.Column),
				}))
			}
			nodes = append(nodes, c.b.Add(hir.Node{
				Kind:  hir.KVariant,
				S:     c.b.Intern(v.Name),
				Type:  c.typeID(v.Type),
				Val:   v.Index,
				First: c.kids(fieldNodes),
				Line:  int32(v.Token.Line), Col: int32(v.Token.Column),
			}))
		}
		return c.b.Add(hir.Node{Kind: hir.KTaggedEnumDef, S: c.b.Intern(s.Name), First: c.kids(nodes), Line: line, Col: col})

	case *InterfaceDefinition:
		nodes := make([]int32, 0, len(s.Methods)+len(s.Implements))
		for _, m := range s.Methods {
			nodes = append(nodes, c.ifaceMethod(m))
		}
		for _, impl := range s.Implements {
			nodes = append(nodes, c.slot("implements", c.b.Add(hir.Node{
				Kind: hir.KName, S: c.b.Intern(impl),
			})))
		}
		return c.b.Add(hir.Node{Kind: hir.KInterfaceDef, S: c.b.Intern(s.Name), First: c.kids(nodes), Line: line, Col: col})

	case *StructDefinition:
		nodes := make([]int32, 0, len(s.Fields))
		for _, f := range s.Fields {
			nodes = append(nodes, c.structField(f))
		}
		return c.b.Add(hir.Node{Kind: hir.KStructDef, S: c.b.Intern(s.Name), First: c.kids(nodes), Line: line, Col: col})
	}

	// Unhandled statement kind. Keeping a KUnknown placeholder instead of
	// dropping the node keeps the tree well formed; TestASTToHIRCoversAll
	// fails whenever this branch is reachable, so it can never ship silently.
	return c.b.Add(hir.Node{Kind: hir.KUnknown, S: c.b.Intern(fmt.Sprintf("%T", s)), Line: line, Col: col})
}

// funcLike lowers anything carrying a FuncSignature plus a body:
// FunctionDefinition today, and reusable for future declaration forms.
func (c *hirConv) funcLike(
	kind hir.Kind, sig *FuncSignature, name string, body *BlockStatement,
	variadicUnion string, line, col int32, extra uint32,
) int32 {
	nodes := make([]int32, 0, len(sig.Parameters)+len(sig.Results)+len(sig.GenericParams)+2)
	// Method definitions have `self` as the first output parameter (Results[0]),
	// inserted by parseMethodDefinition / parseArrayTypeMethodDefinition /
	// parseColonMethodDefinition. The self KResult node is emitted FIRST (before
	// KParam nodes) so that in the HIR children list the ordering is:
	// [self_result, param1, param2, ..., result1, result2, ...].
	// This ensures that `lowerFunction` puts self at params[0] (the first LLVM
	// argument), matching `emitCallBody`'s callArgs ordering:
	// [self_ptr, arg1, arg2, ...].
	isMethod := extra&hir.FlagMethod != 0
	selfSkipped := false
	if isMethod && len(sig.Results) > 0 && sig.Results[0].Name == "self" {
		selfSkipped = true
		nodes = append(nodes, c.param(sig.Results[0], hir.KResult))
	}
	for _, p := range sig.Parameters {
		nodes = append(nodes, c.param(p, hir.KParam))
	}
	for i, r := range sig.Results {
		if isMethod && selfSkipped && i == 0 {
			continue // already emitted as KResult above
		}
		nodes = append(nodes, c.param(r, hir.KResult))
	}
	for _, g := range sig.GenericParams {
		if g == nil {
			continue
		}
		nodes = append(nodes, c.b.Add(hir.Node{
			Kind:  hir.KGeneric,
			S:     c.b.Intern(g.Value),
			Flags: hir.FlagImplicitGeneric * u32(g.IsImplicitGeneric),
		}))
	}
	if sig.GenericUnion != "" {
		nodes = append(nodes, c.b.Add(hir.Node{
			Kind: hir.KUnionName, S: c.b.Intern(sig.GenericUnion),
		}))
	}
	if body != nil {
		nodes = append(nodes, c.slot("body", c.stmt(body)))
	}
	return c.b.Add(hir.Node{
		Kind:  kind,
		S:     c.b.Intern(name),
		S2:    c.b.Intern(variadicUnion),
		Flags: extra | hir.FlagVariadic*u32(sig.IsVariadic),
		First: c.kids(nodes),
		Line:  line, Col: col,
	})
}

// overflowModeFlags 將 FunctionDefinition.OverflowMode
// （#{overflow = wrap|clamp0|min|max|saturate}）編碼為 HIR 標誌位，
// 貫穿 HIR 重建；generateFunctionDefinition 重建時再解碼回字串。
func overflowModeFlags(mode string) uint32 {
	switch mode {
	case "wrap":
		return hir.FlagOverflowWrap
	case "clamp0":
		return hir.FlagOverflowClamp0
	case "min":
		return hir.FlagOverflowMin
	case "max":
		return hir.FlagOverflowMax
	case "saturate":
		return hir.FlagOverflowSaturate
	default:
		return 0
	}
}

func (c *hirConv) param(p *Parameter, kind hir.Kind) int32 {
	if p == nil {
		return hir.NoID
	}
	id := c.b.Add(hir.Node{
		Kind:  kind,
		S:     c.b.Intern(p.Name),
		Type:  c.typeID(p.Type),
		First: c.expr(p.DefaultExpr),
		Line:  int32(p.Token.Line), Col: int32(p.Token.Column),
	})
	c.remember(p, id)
	return id
}

func (c *hirConv) structField(f *StructField) int32 {
	if f == nil {
		return hir.NoID
	}
	id := c.annotate(f, c.structFieldNode(f))
	c.remember(f, id)
	return id
}

func (c *hirConv) structFieldNode(f *StructField) int32 {
	return c.b.Add(hir.Node{
		Kind: hir.KStructField,
		S:    c.b.Intern(f.Name),
		// The "[N]" / "[]" prefix is folded into the rendered string here,
		// following checker.structFieldTypeString: only apply the legacy
		// ArraySize / IsSlice modifiers when the type is not already an
		// ArrayType or SliceType, whose String() carries its own prefix.
		Type: c.b.InternType(structFieldTypeString(f)),
		Val:  f.ArraySize,
		Flags: hir.FlagReadOnly*u32(f.ReadOnly) |
			hir.FlagSealed*u32(f.Sealed) |
			hir.FlagSlice*u32(f.IsSlice),
		First: c.expr(f.Value),
		Line:  int32(f.Token.Line), Col: int32(f.Token.Column),
	})
}

// structFieldTypeString mirrors checker.structFieldTypeString. It lives on
// the parser side so that HIR stores an already-correct declared type and hir
// never needs to know about the AST's legacy modifier fields.
func structFieldTypeString(f *StructField) string {
	if f == nil || f.Type == nil {
		return ""
	}
	typeStr := f.Type.String()
	switch f.Type.(type) {
	case *ArrayType, *SliceType:
		return typeStr
	}
	if f.ArraySize > 0 {
		return fmt.Sprintf("[%d]%s", f.ArraySize, typeStr)
	}
	if f.IsSlice {
		return "[]" + typeStr
	}
	return typeStr
}

func (c *hirConv) ifaceMethod(m *InterfaceMethod) int32 {
	if m == nil {
		return hir.NoID
	}
	nodes := make([]int32, 0, len(m.Parameters)+len(m.Results))
	for _, p := range m.Parameters {
		nodes = append(nodes, c.param(p, hir.KParam))
	}
	for _, r := range m.Results {
		nodes = append(nodes, c.param(r, hir.KResult))
	}
	id := c.b.Add(hir.Node{
		Kind: hir.KInterfaceMethod,
		S:    c.b.Intern(m.Name),
		S2:   c.b.Intern(m.Receiver),
		Flags: hir.FlagVariadic*u32(m.IsVariadic) |
			hir.FlagGenericReceiver*u32(m.IsGenericReceiver),
		First: c.kids(nodes),
		Line:  int32(m.Token.Line), Col: int32(m.Token.Column),
	})
	return id
}

func (c *hirConv) annEntry(e *AnnotationEntry) int32 {
	if e == nil {
		return hir.NoID
	}
	id := c.b.Add(hir.Node{
		Kind:  hir.KAnnEntry,
		S:     c.b.Intern(e.Key),
		First: c.annValue(e.Value),
		Line:  int32(e.Token.Line), Col: int32(e.Token.Column),
	})
	c.remember(e, id)
	return id
}

func (c *hirConv) annValue(v AnnotationValue) int32 {
	if isNil(v) {
		return hir.NoID
	}
	line, col := pos(v)

	switch v := v.(type) {
	case *AnnotationBoolValue:
		// 保留真假：`#{inline=false}` 必須與 `#{inline=true}` 區分開，否則
		// HIR 側（Package.AnnotationBool）無法還原語義。
		return c.b.Add(hir.Node{Kind: hir.KBoolLit, Val: u32i(v.Value), Line: line, Col: col})

	case *AnnotationIntValue:
		return c.b.Add(hir.Node{Kind: hir.KIntLit, Val: v.Value, Line: line, Col: col})

	case *AnnotationStringValue:
		return c.b.Add(hir.Node{Kind: hir.KStrLit, S: c.b.Intern(v.Value), Line: line, Col: col})

	case *AnnotationIdentValue:
		return c.b.Add(hir.Node{Kind: hir.KIdent, S: c.b.Intern(v.Value), Line: line, Col: col})

	case *AnnotationArrayValue:
		nodes := make([]int32, 0, len(v.Elements))
		for _, el := range v.Elements {
			nodes = append(nodes, c.annValue(el))
		}
		return c.b.Add(hir.Node{Kind: hir.KArrayLit, First: c.kids(nodes), Line: line, Col: col})

	case *AnnotationRangeValue:
		return c.b.Add(hir.Node{
			Kind: hir.KRange,
			Flags: hir.FlagLeftInc*u32(v.LeftInc) |
				hir.FlagRightInc*u32(v.RightInc),
			First: c.b.List([]int32{c.annValue(v.Start), c.annValue(v.End)}),
			Line:  line, Col: col,
		})
	}
	return c.b.Add(hir.Node{Kind: hir.KUnknown, S: c.b.Intern(fmt.Sprintf("%T", v)), Line: line, Col: col})
}

// ---- expressions ----

func (c *hirConv) expr(e Expression) int32 {
	id := c.exprNode(e)
	c.remember(e, id)
	return id
}

func (c *hirConv) exprNode(e Expression) int32 {
	if isNil(e) {
		return hir.NoID
	}
	line, col := pos(e)

	switch e := e.(type) {
	case *Identifier:
		return c.b.Add(hir.Node{
			Kind:  hir.KIdent,
			S:     c.b.Intern(e.Value),
			Flags: hir.FlagImplicitGeneric * u32(e.IsImplicitGeneric),
			Line:  line, Col: col,
		})

	case *IntegerLiteral:
		// S = Raw (as written after normalisation), S2 = the original token
		// literal. They differ: some synthesised literals carry Raw="?" or an
		// empty Raw while Token.Literal holds the real digits, and module
		// export values must render the token literal so that values above
		// int64 range (18446744073709551615) do not display as -1.
		return c.b.Add(hir.Node{
			Kind: hir.KIntLit, Val: e.Value,
			S:    c.b.Intern(e.Raw),
			S2:   c.b.Intern(e.Token.Literal),
			Line: line, Col: col,
		})

	case *ByteLiteral:
		return c.b.Add(hir.Node{Kind: hir.KByteLit, Val: e.Value, S: c.b.Intern(e.Raw), Line: line, Col: col})

	case *FloatLiteral:
		n := hir.Node{Kind: hir.KFloatLit, S: c.b.Intern(e.Raw), Line: line, Col: col}
		n.SetFloat(e.Value)
		return c.b.Add(n)

	case *StringLiteral:
		return c.b.Add(hir.Node{Kind: hir.KStrLit, S: c.b.Intern(e.Value), S2: c.b.Intern(e.Raw), Line: line, Col: col})

	case *CharLiteral:
		return c.b.Add(hir.Node{Kind: hir.KCharLit, S: c.b.Intern(e.Value), S2: c.b.Intern(e.Raw), Line: line, Col: col})

	case *RegexLiteral:
		return c.b.Add(hir.Node{Kind: hir.KRegexLit, S: c.b.Intern(e.Pattern), S2: c.b.Intern(e.Flags), Line: line, Col: col})

	case *BooleanLiteral:
		return c.b.Add(hir.Node{Kind: hir.KBoolLit, Val: u32i(e.Value), Line: line, Col: col})

	case *NilLiteral:
		return c.b.Add(hir.Node{Kind: hir.KNilLit, Line: line, Col: col})

	case *PrefixExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KPrefix,
			S:     c.b.Intern(e.Operator),
			First: c.expr(e.Right),
			Line:  line, Col: col,
		})

	case *InfixExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KInfix,
			S:     c.b.Intern(e.Operator),
			First: c.b.List([]int32{c.expr(e.Left), c.expr(e.Right)}),
			Line:  line, Col: col,
		})

	case *RunExpression:
		return c.b.Add(hir.Node{Kind: hir.KRun, First: c.expr(e.Call), Line: line, Col: col})

	case *AwaitExpression:
		return c.b.Add(hir.Node{Kind: hir.KAwait, First: c.expr(e.Right), Line: line, Col: col})

	case *GroupedExpression:
		return c.b.Add(hir.Node{Kind: hir.KGrouped, First: c.expr(e.Expression), Line: line, Col: col})

	case *IfExpression:
		nodes := []int32{
			c.slot("cond", c.expr(e.Condition)),
			c.slot("then", c.stmt(e.Consequence)),
			c.slot("else", c.stmt(e.Alternative)),
			c.slot("matched", c.expr(e.MatchedExpr)),
			c.slot("dot-val", c.stmt(e.DotValBody)),
			c.slot("range", c.expr(e.RangePattern)),
			c.slot("eq", c.expr(e.EqualityPattern)),
			c.slot("raw", c.expr(e.RawCond)),
		}
		for _, o := range e.OptionPatterns {
			nodes = append(nodes, c.slot("opt", c.b.Add(hir.Node{
				Kind: hir.KName, S: c.b.Intern(o),
			})))
		}
		for _, v := range e.ValuePatterns {
			nodes = append(nodes, c.slot("val", c.expr(v)))
		}
		return c.b.Add(hir.Node{Kind: hir.KIf, First: c.kids(nodes), Line: line, Col: col})

	case *RangeExpression:
		// Store Start/End as named slots rather than positionally: either bound
		// may be nil (open ranges like ..5 or 5..), and Builder.List drops NoID
		// children, which would otherwise collapse a one-sided range and let the
		// reconstructor mis-assign the surviving child. Slots preserve which
		// bound is present even when the other is absent.
		nodes := make([]int32, 0, 2)
		if se := c.slot("start", c.expr(e.Start)); se != hir.NoID {
			nodes = append(nodes, se)
		}
		if ee := c.slot("end", c.expr(e.End)); ee != hir.NoID {
			nodes = append(nodes, ee)
		}
		return c.b.Add(hir.Node{
			Kind: hir.KRange,
			Flags: hir.FlagLeftInc*u32(e.LeftInc) |
				hir.FlagRightInc*u32(e.RightInc),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *SliceExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KSlice,
			First: c.b.List([]int32{c.expr(e.Left), c.expr(e.Range)}),
			Line:  line, Col: col,
		})

	case *IndexExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KIndex,
			First: c.b.List([]int32{c.expr(e.Left), c.expr(e.Index)}),
			Line:  line, Col: col,
		})

	case *AssignExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KAssign,
			First: c.b.List([]int32{c.expr(e.Left), c.expr(e.Value)}),
			Line:  line, Col: col,
		})

	case *ConditionalExpression:
		return c.b.Add(hir.Node{
			Kind: hir.KCond,
			First: c.b.List([]int32{
				c.expr(e.Condition), c.expr(e.Consequence), c.expr(e.Alternative),
			}),
			Line: line, Col: col,
		})

	case *CastExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KCast,
			Type:  c.typeID(e.Type),
			First: c.expr(e.Expr),
			Line:  line, Col: col,
		})

	case *IterationExpr:
		return c.b.Add(hir.Node{
			Kind:  hir.KIter,
			S:     c.b.Intern(e.Variable),
			S2:    c.b.Intern(e.RangeStr),
			First: c.b.List([]int32{c.expr(e.Range), c.expr(e.RangeExpr)}),
			Line:  line, Col: col,
		})

	case *ArrayLiteral:
		nodes := make([]int32, 0, len(e.Elements)+1)
		nodes = append(nodes, c.slot("size", c.expr(e.Size)))
		for _, el := range e.Elements {
			nodes = append(nodes, c.slot("elem", c.expr(el)))
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KArrayLit,
			Flags: hir.FlagWasSlice * u32(e.WasSliceLiteral),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *MapLiteral:
		nodes := make([]int32, 0, len(e.Pairs))
		for i := range e.Pairs {
			p := &e.Pairs[i]
			nodes = append(nodes, c.b.Add(hir.Node{
				Kind:  hir.KMapPair,
				First: c.b.List([]int32{c.expr(p.Key), c.expr(p.Value)}),
				Line:  int32(p.Token.Line), Col: int32(p.Token.Column),
			}))
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KMapLit,
			Type:  c.typeID(e.MapType),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *SliceLiteral:
		nodes := make([]int32, 0, len(e.Elements))
		for _, el := range e.Elements {
			nodes = append(nodes, c.expr(el))
		}
		return c.b.Add(hir.Node{Kind: hir.KSliceLit, First: c.kids(nodes), Line: line, Col: col})

	case *StructLiteral:
		nodes := make([]int32, 0, len(e.Fields))
		for _, f := range e.Fields {
			nodes = append(nodes, c.structField(f))
		}
		return c.b.Add(hir.Node{
			Kind:  hir.KStructLit,
			S:     c.b.Intern(e.Type),
			First: c.kids(nodes),
			Line:  line, Col: col,
		})

	case *CallExpression:
		nodes := make([]int32, 0, len(e.GenericArgs)+len(e.Arguments)+1)
		nodes = append(nodes, c.slot("fn", c.expr(e.Function)))
		for _, g := range e.GenericArgs {
			nodes = append(nodes, c.slot("gen", c.expr(g)))
		}
		for _, a := range e.Arguments {
			nodes = append(nodes, c.slot("arg", c.expr(a)))
		}
		return c.b.Add(hir.Node{Kind: hir.KCall, First: c.kids(nodes), Line: line, Col: col})

	case *DotExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KDot,
			S:     c.b.Intern(e.Property),
			First: c.expr(e.Receiver),
			Line:  line, Col: col,
		})

	case *FunctionLiteral:
		return c.funcLike(hir.KFuncLit, &e.FuncSignature, "", e.Body, e.VariadicUnion, line, col, 0)

	case *Parameter:
		// Parameter satisfies expressionNode, so it is legal in expression
		// position even though the parser never puts one there today. Lowering
		// it defensively costs one line and keeps the exhaustiveness test
		// honest instead of carrying a hand-maintained exemption list.
		return c.param(e, hir.KParam)

	case *NullableType:
		return c.b.Add(hir.Node{Kind: hir.TNullable, Type: c.typeID(e.Type), Line: line, Col: col})

	case *PointerType:
		return c.b.Add(hir.Node{Kind: hir.TPointer, Type: c.typeID(e.Type), Line: line, Col: col})

	case *ViewType:
		// A view is a borrow — pointer-like at the ABI level — so it lowers with
		// the pointer type kind. The exact type string ("&json") still flows
		// through c.typeID on the Parameter/Result nodes, which is what MIR
		// reads to classify ownership (non-owned) and pick the LLVM type.
		return c.b.Add(hir.Node{Kind: hir.TPointer, Type: c.typeID(e.Type), Line: line, Col: col})
	}

	return c.b.Add(hir.Node{Kind: hir.KUnknown, S: c.b.Intern(fmt.Sprintf("%T", e)), Line: line, Col: col})
}

// ---- small helpers ----

func u32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func u32i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func idValue(id *Identifier) string {
	if id == nil {
		return ""
	}
	return id.Value
}

// isNil reports whether v is a nil interface or holds a typed nil pointer.
// Typed nils survive interface conversion and would otherwise panic on the
// field accesses in the lowering switches above.
func isNil(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
