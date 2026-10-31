package parser

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/ty"
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
	// prog is threaded to the H0 match-desugar scratch parser so it can derive
	// local variable types from source declarations when the per-function side
	// table (FuncVarTypes) has no entry for a blob-loaded std function (S1 flip).
	c.prog = prog
	// S1 flip — "Lowering 唯一性": match desugaring must read idxLocalTypes (the
	// safe-index local container-type table used by isSafeIndexBase /
	// inferIndexElemType) from ONE source. H0 desugars SurfaceMatch WITHOUT
	// running lowerProgram's walk, so for blob/AST-cache-loaded std modules the
	// table is empty here. precomputeIdxLocalTypes is the single collector (also
	// used by prepareInBoundsMetadata / lowerProgram); run it once per program at
	// this single H0 entry so every desugar path — H0 and (where still used) the
	// no-flip path — sees the same table. Idempotent: it only adds the entries
	// the table is missing (overwrites with identical values otherwise).
	if c.sem != nil {
		if c.sem.IdxLocalTypes == nil {
			c.sem.IdxLocalTypes = make(map[string]map[string]*ty.Type)
		}
		(&Parser{sem: c.sem, prog: c.prog, idxLocalTypes: c.sem.IdxLocalTypes}).precomputeIdxLocalTypes(c.prog)
	}
	// Owners is filled while the top-level statements are lowered, because the
	// owner is recorded per STATEMENT in the AST but is needed per NODE id by
	// the backend (see hir.Package.Owners).
	owners := make(map[int32]string, len(prog.Statements))
	for _, stmt := range prog.Statements {
		id := c.stmt(stmt)
		if owner := c.sem.ModuleOwnerOf(stmt); owner != "" {
			owners[id] = owner
		}
		c.top(id)
	}
	pkg := b.Package()
	pkg.Owners = owners
	// Fold diagnostics produced by desugaring done inside H0 (SurfaceMatch) into
	// the program's diagnostic channels. Without this they would be silently
	// dropped — the exact failure mode this migration exists to remove.
	//
	// Severity is preserved, and deliberately so. Program.Warnings carries no
	// severity, so it can only hold warnings; H0-side desugaring also produces
	// ERRORS (buildMatchDesugar's "empty match arm cannot determine a value"),
	// and those go to Program.Errors. Downgrading them to warnings would have
	// turned a build failure into a successful build of a program whose match
	// arms have no value — see the Program.Errors block comment for the
	// measured witness, and src/parser/dump/h0_diag_test.go for the gate.
	//
	// Ordering note: this mutates the caller's Program, so it is not idempotent
	// — lowering the same *Program twice appends twice. That is pre-existing
	// behaviour for Warnings and no shipped path does it.
	for _, d := range c.h0Diags {
		switch d.Severity {
		case SeverityWarning:
			prog.Warnings = append(prog.Warnings, d.Message)
		case SeverityError:
			prog.Errors = append(prog.Errors, d.Message)
		}
	}
	// Colorless async (`go`): monomorphize each `go <call>` callee into its
	// `-async` variant and propagate the async coloring transitively. This
	// MUST run before DesugarAsyncGroups so the rewritten bare `-async` calls
	// are recognized as spawnable by the group pass. See hir.MonomorphizeGo.
	pkg.GoSpawns = c.goSpawns
	hir.MonomorphizeGo(pkg)
	// Coroutine groups run first (above) and claim the `go` spawns that sit
	// inside a bare block; any `go` left at statement level (outside a group)
	// is wrapped in `run`/`awy` here so its RESULT is read, not the handle.
	hir.DesugarGoInlineAwait(pkg)
	// Record the owning module of every top-level free function so backends
	// can validate module-qualified bare-name resolution (see
	// hir.Package.FuncOwners). Names are taken AFTER build.mangleOverloads /
	// prefixCollidingFunctions have run, so they match the registered HIR
	// function names exactly.
	pkg.FuncOwners = make(map[string]string, len(prog.Statements))
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *FunctionDefinition:
			if !s.IsMethodDef {
				pkg.FuncOwners[s.Name] = c.sem.ModuleOwnerOf(stmt)
			}
		case *LetStatement:
			if s.Name == nil {
				continue
			}
			if _, isFn := s.Value.(*FunctionLiteral); isFn && !strings.Contains(s.Name.Value, ".") {
				pkg.FuncOwners[s.Name.Value] = c.sem.ModuleOwnerOf(stmt)
			}
		}
	}
	// Coroutine groups (協程組): a bare block holding `-async` calls is
	// rewritten into explicit `run` / `awy` bindings. It runs HERE, after the
	// surface AST has been fully lowered and — critically — after the checker
	// has already run on the AST, so a synthesized handle binding never has to
	// satisfy a declaration check. See hir.DesugarAsyncGroups.
	hir.DesugarAsyncGroups(pkg)
	return pkg, c.astOf
}

type hirConv struct {
	b   *hir.Builder
	sem *SemanticContext
	// prog is the merged Program being lowered; handed to the scratch parser so
	// H0 match desugar can derive local variable types from source declarations.
	prog  *Program
	astOf map[Node]int32 // AST node -> HIR id, for checker-side TypeMap keying
	// goSpawns accumulates the HIR node ids of `co <call>` KCalls lowered from
	// CoExpression nodes, so ASTToHIRWithMap can hand them to the
	// monomorphization pass via hir.Package.GoSpawns.
	goSpawns []int32
	// curFile is the base name of the source file being lowered, threaded down
	// the statement tree (inherited by nested statements whose SourceFile is
	// empty). Stamp onto KCall nodes so the MIR lowerer can bake a caller
	// "file:line:col" literal for FlagTrackCaller callees (see std/log).
	curFile string
	// curFunc is the name of the function whose body is currently being lowered
	// ("" at module level). It mirrors Parser.curFuncName, which the lowering
	// pass maintains while it walks, and is needed by anything inside H0 that
	// resolves a name against the per-function side tables — see
	// desugarSurfaceMatch, which looks up sem.FuncVarType(curFunc, …) exactly
	// the way buildMatchDesugar does.
	//
	// FunctionLiteral deliberately does NOT open a scope: the lowering pass has
	// no case for it either, so a literal's body keeps the ENCLOSING function's
	// name. Matching that is what keeps HIR identical whichever stage desugars.
	curFunc string
	// curResultName is the name of the option-typed result param (?T) of the
	// function whose body is currently being lowered ("" if none). Extracted by
	// funcLike from sig.Results for FunctionDefinition; FunctionLiteral inherits
	// the enclosing scope's value (matching lowerUnwrapAssign's use of
	// curFuncDef, which the walk only sets on FunctionDefinition). Consumed by
	// lowerUnwrapAssignToHIR to feed lowerUnwrapAssign's resultNameOverride.
	curResultName string
	// h0Diags collects diagnostics produced by desugaring done inside H0 (see
	// desugarSurfaceMatch). They have no channel of their own, so they are
	// gathered here and folded into Program.Warnings (warnings) / Program.Errors
	// (errors) by ASTToHIRWithMap — without that they would be silently
	// dropped, which is the failure mode this whole migration exists to remove.
	h0Diags []Diagnostic
	// captureOrigin，非空時是「本條陳述正由 H0 捕獲軌產出」的 AST 節點。
	//
	// S3-b 雙軌對齊用，只影響 **remember**（astOf 對映）：
	// AST 軌會把該陳述**換成** BlockStatement，原 LetStatement 從此不在樹裡、
	// 也不會進 c.stmt ⇒ 不會有 astOf[ls] 條目。H0 軌裡該陳述仍在、照常進
	// c.stmt ⇒ 會多出一條 astOf[ls]，讓 PopulateInferredTypes 把 ls 的推斷
	// 型別寫到鏈的 block id 上（MIR 的 typeOfNode 讀得到）⇒ 與 AST 軌發散。
	//
	// annotate **刻意保留**：實測（tests/byte-option-index.no）AST 軌仍會把
	// `#{overflow=wrap}` 之類的註解帶進 HIR，H0 軌若一併抑制反而丟失。
	// 兩處需求相反 ⇒ 沒有通用抑制規則，只對齊 remember 這一項。
	//
	// 只對**這一個**節點生效：巢狀的綁定 let、if 鏈等仍照常 remember。
	captureOrigin Node
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

// nodeType extracts the declared/inferred Type of an AST node via an explicit
// type switch (6C-optional「去反射」). The set of AST types exposing a
// `Type` field is closed and enumerated here; nodes without a Type field
// (expressions, blocks, etc.) return nil and are skipped by
// PopulateInferredTypes. Typed-nil receivers return nil (mirrors the old
// reflect-based behaviour).
func nodeType(n Node) Type {
	switch t := n.(type) {
	case *LetStatement:
		if t != nil {
			return t.Type
		}
	case *Parameter:
		if t != nil {
			return t.Type
		}
	case *NullableType:
		if t != nil {
			return t.Type
		}
	case *PointerType:
		if t != nil {
			return t.Type
		}
	case *ViewType:
		if t != nil {
			return t.Type
		}
	case *CastExpression:
		if t != nil {
			return t.Type
		}
	case *StructField:
		if t != nil {
			return t.Type
		}
	case *TypeAlias:
		if t != nil {
			return t.Type
		}
	case *TaggedEnumVariant:
		if t != nil {
			return t.Type
		}
	}
	return nil
}

// effectiveTypeMap 建構「AST 節點 → 有效型別字串」的**顯式 type map**
// （設計文檔 §3.3：「現在就地寫 `.Type` 的做法改為返回 type map」，
// `IdxLocalTypes`（parser.go）是同一模式的先例）。
//
// 解析序與 KLet 的有效型別解析序（TypeOverride > Type > InferredType）前兩檔
// 一致：
//  1. TypeOverride：裝配期（slice-structs / devirtualize / union clone）為某個
//     LetStatement 定下的有效型別覆寫，優先於節點宣告的 Type；
//  2. 宣告型別：AST 節點自帶的 `.Type`（nodeType 顯式 type switch，6C-optional
//     第一步已去反射）——這是 parser 期就存在的用戶註解，非 checker 推斷產物。
//
// ⚠️ 表達式級**推斷**型別（checker.inferExprType 的結果）刻意**不在**本表：
// 它活在 checker 側的 THIRView.ExprTypes（thir_exprtypes.go），經 TypeOf 第三
// 檔查詢。理由見該文件頭——若寫進 pkg.Inferred，MIR 的 4 處消費點
// （hir2mir.go KCast 1540 / inferredTypeOf 1641 / letTypeRaw 2038 /
// lowerMapLit 7050）會把 codegen 的 void 兜底翻成型別，屬 codegen 級行為變更。
//
// 只收錄有型別的節點（無宣告、無覆寫的節點不入表），與舊版逐節點判定等價。
func effectiveTypeMap(idMap map[Node]int32, sem *SemanticContext) map[Node]string {
	m := make(map[Node]string, len(idMap))
	for n := range idMap {
		if ov := sem.TypeOverrideOf(n); ov != "" {
			m[n] = ov
			continue
		}
		if t := nodeType(n); t != nil && t.String() != "" {
			m[n] = t.String()
		}
	}
	return m
}

// PopulateInferredTypes fills pkg.Inferred with the effective types keyed by
// HIR node id, using the AST-node -> HIR-id map returned by ASTToHIRWithMap.
// Call it AFTER type checking has run.
//
// 本函數已是**純搬運**：型別結論全部來自 effectiveTypeMap 這張顯式 type map
// （§3.3 終態——「反射讀被刪除，改為消費 checker 的 type map」），函數體不再
// 逐節點解析 AST。sem 可為 nil（TypeOverrideOf 容忍 nil receiver）。
func PopulateInferredTypes(pkg *hir.Package, idMap map[Node]int32, sem *SemanticContext) {
	if pkg == nil || len(idMap) == 0 {
		return
	}
	types := effectiveTypeMap(idMap, sem)
	for astNode, hirID := range idMap {
		if t := types[astNode]; t != "" {
			pkg.SetInferred(hirID, t)
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

// hasAnnotation reports whether the parser filed a `#{key}` annotation on n.
// Reads the semantic side-tables, falling back to RawAnnotations: during
// AST→HIR conversion the merged-`Annotations` copy is only populated by
// ResolveProgram, which has NOT run yet for the MAIN program (module bodies are
// resolved first). Mirrors annotation.go's `AnnotationsOf → RawAnnotationsOf`
// fallback so `#{track-caller}` is recognised in both std and main sources.
func (c *hirConv) hasAnnotation(n Node, key string) bool {
	if c.sem == nil || isNil(n) {
		return false
	}
	for _, e := range c.sem.AnnotationsOf(n) {
		if e != nil && e.Key == key {
			return true
		}
	}
	for _, e := range c.sem.RawAnnotationsOf(n) {
		if e != nil && e.Key == key {
			return true
		}
	}
	return false
}

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

// typeFromInferredString 把「語義副表裡的 ?T 字串」（如 "?int"）還原為 Type 節點，
// 使 ASTToHIR 在 `s.Type == nil` 時仍能產生與舊版（直接寫 AST）相同的 HIR Type ID。
//
// 該節點只活在 ASTToHIR 局部棧上、不入 AST，所以 IsInferred 不必為 true（後續的
// typeID 只讀 String()）。重建對稱型別節點以匹配舊版 NullableType/NamedType 的
// String() 輸出（"Optional`?`包 inner"）。
func typeFromInferredString(s string) Type {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "?") {
		inner := s[1:]
		if inner == "" {
			return nil
		}
		return &NullableType{Type: &NamedType{Value: inner}}
	}
	return &NamedType{Value: s}
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
	// Thread the origin file down the statement tree. Only top-level statements
	// are tagged via SetSourceFileOf for the MAIN program (module bodies use
	// SetSourceFileDeep), so a nested statement with an empty SourceFile keeps
	// the enclosing statement's file instead of losing it.
	if sf := c.sem.SourceFileOf(s); sf != "" {
		c.curFile = filepath.Base(sf)
	}
	id := c.stmtNode(s)
	// S3-b：H0 捕獲軌的來源陳述刻意不 annotate / remember —— 見 captureOrigin
	// 的註解（複刻 AST 軌「替換後註解丟失」的既有行為，以保零行為變化）。
	//
	// ⚠️ skip 必須在 stmtNode **之後**判定、且清空放在末尾：lowerCaptureToHIR
	// 內部會巢狀呼叫 c.stmt（綁定 let、if 鏈），那些呼叫的 s 不是來源陳述，
	// 必須照常 annotate/remember，也不能由它們來清空旗標。
	skip := s == c.captureOrigin
	id = c.annotate(s, id)
	c.embed(s, id)
	if !skip {
		c.remember(s, id)
	}
	if skip {
		c.captureOrigin = nil
	}
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
		// S3 H0 直出：?= 由 H0 降成基礎 HIR（KBlock+KLet+KIf 鏈），不再依賴
		// parser walk 的 lowerUnwrapAssign 原地展開回寫 AST。lowerUnwrapAssignToHIR
		// 呼叫 lowerUnwrapAssign（語義變換完全共享：resultName 探測、D9 分支、
		// guardOptionOperands、型別副表推斷、targetIsResult 兩形態、目標預宣告、
		// ok 臂 AssignExpression/LetStatement），再將產出的 BlockStatement 轉 HIR。
		// KUnwrapAssign 降為 tombstone（G3 門禁釘住計數 = 0）。
		if id := c.lowerUnwrapAssignToHIR(s, line, col); id != hir.NoID {
			return id
		}
		// 直出失敗（錯誤已由 h0Diags 上報）：發 KUnknown 避免 tombstone KUnwrapAssign。
		return c.b.Add(hir.Node{Kind: hir.KUnknown, S: c.b.Intern("UnwrapAssignStatement"), Line: line, Col: col})

	case *LetStatement:
		// S3-b 雙軌：H0 軌讀捕獲副表直接產出 KLet + KIf 鏈，取代 AST 軌的
		// 「就地改寫用戶 AST」（V4 違規）。副表為 nil 表示本陳述未判定要捕獲，
		// 走下方原有路徑。產出失敗（NoID）時同樣回落，避免留下 tombstone。
		if HIRCaptureEnabled && c.sem != nil {
			if f := c.sem.CaptureFactOf(s); f != nil {
				// 旗標由 stmt() 的末尾清空（見該處註解）：這裡不能清，
				// annotate/remember 是在 stmtNode 返回之後才跑的。
				c.captureOrigin = s
				id := c.lowerCaptureToHIR(s, f)
				if id != hir.NoID {
					return id
				}
			}
		}
		// 有效型別解析序（S5 stage 5 起）：TypeOverride（裝配期具現化/具象化的
		// 有效型別覆蓋，語義副表）> s.Type（宣告）> InferredType（lowering 的
		// ?T 推斷，S3 起也在副表）。覆蓋必須最先——它改寫的正是「已宣告為介面/
		// 範本、但有效型別已收窄」的綁定。
		var typ Type
		if c.sem != nil {
			if ov := c.sem.TypeOverrideOf(s); ov != "" {
				typ = typeFromInferredString(ov)
			}
		}
		if typ == nil {
			typ = s.Type
		}
		// lowering 不再就地寫 `s.Type = &NullableType{IsInferred: true}`，改走
		// 語義副表 (Program.Sem.InferredType)。ASTToHIR 在 `s.Type` 為空時補讀
		// 推斷型別，使 HIR Type ID 與「寫 AST」時代逐字節一致。typeString 內含
		// 既有 Null/NamedType 格式化路徑，行為相容。
		if typ == nil && c.sem != nil {
			if inf := c.sem.InferredType(s); inf != "" {
				typ = typeFromInferredString(inf)
			}
		}
		return c.b.Add(hir.Node{
			Kind: hir.KLet,
			S:    c.b.Intern(idValue(s.Name)),
			S2:   c.b.Intern(s.ItArmType),
			Type: c.typeID(typ),
			Flags: hir.FlagSynthetic*u32(s.IsSynthetic) |
				hir.FlagArmBinding*u32(s.IsArmBinding) |
				hir.FlagModuleConst*u32(c.sem.ModuleConstOf(s)) |
				overflowModeFlags(s.OverflowMode),
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
		return c.funcLike(hir.KFuncDef, &s.FuncSignature, s.Name, s.Body,
			c.sem.VariadicUnionOf(s), c.sem.GenericUnionOf(s), line, col,
			hir.FlagMethod*u32(s.IsMethodDef)|
				hir.FlagColon*u32(s.ColonSyntax)|
				hir.FlagSkipNaming*u32(s.IsSkipNamingCheck)|
				hir.FlagTrackCaller*u32(c.hasAnnotation(s, "track-caller"))|
				overflowModeFlags(s.OverflowMode),
			s.Name)

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
	variadicUnion, genericUnion string, line, col int32, extra uint32,
	scope string,
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
	// 運算符重載方法（bigint._add 等，含模組限定名 "bigint.bigint._add"）：
	// 名字的最後一段屬於保留閉集 `_add/_sub/_mul/_div/_mod` 即命中。
	isOpOverloadResult := kind == hir.KFuncDef && isMethod &&
		IsOperatorOverloadName(OperatorOverloadBaseName(name))
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
		// 運算符重載方法（_add 等）的結果參數書寫為 `?T`，僅為宣告標記：
		// HIR 層把 NullableType 剝成 payload T，使 callee 的 ABI 為純 T*
		// 結果槽（非 %option*），不產生 heap 包裝／淺複製——鏈式運算子
		// （`a + b + c`）把前一個呼叫結果再作接收者時，淺複製的 vec 欄位
		// 會隨臨釋釋放而失效（讀到 0），剝掉包裝即無此生命週期問題。
		if isOpOverloadResult {
			nodes = append(nodes, c.resultParamPayload(r))
			continue
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
	if genericUnion != "" {
		nodes = append(nodes, c.b.Add(hir.Node{
			Kind: hir.KUnionName, S: c.b.Intern(genericUnion),
		}))
	}
	if body != nil {
		// scope is the function name that side-table lookups inside this body
		// must use. FunctionDefinition passes its own name; FunctionLiteral
		// passes the enclosing scope unchanged, matching the lowering pass.
		saved := c.curFunc
		c.curFunc = scope
		// Extract the option-typed result param name (?T) for UnwrapAssign
		// expansion. FunctionDefinition opens a new result scope; FunctionLiteral
		// inherits the enclosing one (matching lowerUnwrapAssign's use of
		// curFuncDef, which the walk only sets on FunctionDefinition).
		savedResult := c.curResultName
		if kind == hir.KFuncDef {
			c.curResultName = ""
			// 運算符重載方法的 `?T` 結果只是宣告標記，HIR 已剝成 payload T
			// （見上方 resultParamPayload）；不能再把它的名字登記為 option
			// 結果名，否則 body 內的 `?=`/unwrap 展開會找不到 option 槽。
			if !isOpOverloadResult {
				for _, res := range sig.Results {
					if res.Type != nil {
						ts := typeString(res.Type)
						if strings.HasPrefix(ts, "?") {
							c.curResultName = res.Name
							break
						}
					}
				}
			}
		}
		nodes = append(nodes, c.slot("body", c.stmt(body)))
		c.curFunc = saved
		c.curResultName = savedResult
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

// resultParamPayload 是運算符重載方法結果參數的專用降形：與 param 相同的
// KResult 節點，但把書寫型別 `?T`（NullableType）剝成 payload T 再取 typeID，
// 使 callee ABI 的結果槽為 T* 而非 %option*。非 NullableType 型別原樣透傳。
// 不修改 AST（ParseProgram 輸出不可變），只在 HIR 層換型別。
//
// 刻意 **不** 呼叫 c.remember：idMap 會餵給 checker 的 PopulateInferredTypes，
// 其 nodeType() 讀 AST 原始 `?T` 並把 `?bigint` 寫進 HIR InferredType 表；
// MIR 的 typeOfNode→inferredTypeOf 優先取 InferredType，會把剝掉的 `?` 又
// 塞回結果槽（option ABI → heap 包裝 → 鏈式中間值淺複製後讀 0）。不登記
// 這條映射，MIR 即退回節點宣告型別（payload T），ABI 乾淨。
func (c *hirConv) resultParamPayload(p *Parameter) int32 {
	if p == nil {
		return hir.NoID
	}
	ty := p.Type
	if nt, ok := ty.(*NullableType); ok && nt != nil && nt.Type != nil {
		ty = nt.Type
	}
	return c.b.Add(hir.Node{
		Kind: hir.KResult,
		S:    c.b.Intern(p.Name),
		Type: c.typeID(ty),
		Line: int32(p.Token.Line), Col: int32(p.Token.Column),
	})
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

	case *AnnotationStructValue:
		// 複合型別（結構體）字面量：`T{name:'x', age:0}`。HIR 沒有專門的結構體
		// 字面量節點，以 map-lit（鍵/值交替）承載，保證 annValue 對每種
		// annotationValueNode 型別都有對應分支（見 tohir_exhaustive_test.go）。
		nodes := make([]int32, 0, 2*len(v.Fields))
		for _, f := range v.Fields {
			nodes = append(nodes, c.b.Add(hir.Node{Kind: hir.KIdent, S: c.b.Intern(f.Name), Line: line, Col: col}))
			nodes = append(nodes, c.annValue(f.Value))
		}
		return c.b.Add(hir.Node{Kind: hir.KMapLit, First: c.kids(nodes), Type: c.b.Intern(v.Type), Line: line, Col: col})

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

	case *CoExpression:
		// Lower `co F(args)` to a plain KCall to F; the monomorphization pass
		// (hir.MonomorphizeGo) rewrites the callee to `F-async` and, when the
		// call sits inside a coroutine group, lets DesugarAsyncGroups spawn it.
		// The call node id is recorded in c.goSpawns so the pass can find it.
		callID := c.expr(e.Call)
		c.goSpawns = append(c.goSpawns, callID)
		return callID

	case *AwaitExpression:
		return c.b.Add(hir.Node{Kind: hir.KAwait, First: c.expr(e.Right), Line: line, Col: col})

	case *GroupedExpression:
		// W5 / §10.2（2026-10-21）：括号是纯语法、无语义，H0 抹除——直接
		// 透传内层表达式的 HIR（P2「HIR 无糖」；KGrouped 自此计数为 0，
		// 由 hir/sugar_kinds_test.go 的 G3 门禁钉住）。
		return c.expr(e.Expression)

	case *IfExpression:
		// S-d (2026-10-19) 把 match 降糖改为走 `case *SurfaceMatch` 流式直出
		// base HIR（KTypeMatch/KTypeArm/KIf 由 H0 從糖節點產生）；本 case 現只
		// 承載純 if/elif/else。match 痕迹字段（MatchedExpr/DotValBody/
		// RangePattern/EqualityPattern/OptionPatterns/ValuePatterns/RawCond/
		// IsTypeMatch/TypePattern/TypeBindingName/IsTypeDefault/IsTypeDefault）
		// 已由 §5 清单待刪，此處不再消費，避免死代碼維持 12 字段存活。
		return c.b.Add(hir.Node{
			Kind: hir.KIf,
			First: c.kids([]int32{
				c.slot("cond", c.expr(e.Condition)),
				c.slot("then", c.stmt(e.Consequence)),
				c.slot("else", c.stmt(e.Alternative)),
			}),
			Line: line, Col: col,
		})

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
		node := hir.Node{
			Kind:  hir.KIndex,
			First: c.b.List([]int32{c.expr(e.Left), c.expr(e.Index)}),
			Line:  line, Col: col,
		}
		if c.sem.IndexIsInBounds(e) {
			node.Flags |= hir.FlagInBounds
		}
		if c.sem.GrowWriteOf(e) {
			node.Flags |= hir.FlagGrowWrite
		}
		return c.b.Add(node)

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
		file := hir.NoID
		if c.curFile != "" {
			file = c.b.Intern(c.curFile)
		}
		// S2 on a KCall is repurposed to carry the interned source-file base name
		// (NoID when unknown), so the MIR lowerer can bake a caller
		// "file:line:col" for FlagTrackCaller callees (see hir.Node.S2, std/log).
		//
		// FlagQualifiedCall keeps the record that this call was written
		// QUALIFIED (`io.err(...)`) and flattened to a bare name by
		// checker.ResolveModuleCalls. The flattened shape is identical to a
		// hand-written bare call, and the MIR lowerer must not hand the bare
		// name to a global callable alias (`@ std/err.new err`).
		var flags uint32
		if c.sem != nil && c.sem.CalleeOf(e) == CalleeModuleFn {
			flags |= hir.FlagQualifiedCall
		}
		return c.b.Add(hir.Node{Kind: hir.KCall, First: c.kids(nodes), Line: line, Col: col, S2: file, Flags: flags})

	case *DotExpression:
		return c.b.Add(hir.Node{
			Kind:  hir.KDot,
			S:     c.b.Intern(e.Property),
			First: c.expr(e.Receiver),
			Line:  line, Col: col,
		})

	case *FunctionLiteral:
		// Passes c.curFunc, NOT "": the lowering pass has no FunctionLiteral
		// case either, so a literal's body resolves names against the enclosing
		// function. Diverging here would change which side table a nested match
		// reads.
		return c.funcLike(hir.KFuncLit, &e.FuncSignature, "", e.Body,
			c.sem.VariadicUnionOf(e), c.sem.GenericUnionOf(e), line, col, 0, c.curFunc)

	case *SurfaceMatch:
		// S2: H0 lowers the sugar node itself instead of relying on a pre-pass
		// having expanded it. desugarSurfaceMatch is non-mutating, and the chain
		// it returns still contains the ORIGINAL nested SurfaceMatch nodes, so
		// the c.expr below re-enters this case for them — the streaming
		// equivalent of lowerSurfaceMatch's bottom-up walk.
		//
		// Since the S1 flip (commit b918dba9) this case is the MAIN path for
		// matches: lowering.go no longer expands SurfaceMatch, so every match
		// reaches H0 as surface sugar and is desugared here. The "unreachable"
		// note below is historical — it was true only before the flip, when
		// lowerProgram expanded every match ahead of H0.
		//
		// Today the two paths are:
		//   flip (shipped):    SurfaceMatch -> this case -> desugarSurfaceMatch
		//   no-flip (control): lowerSurfaceMatch -> IfExpression -> case *IfExpression
		// Both must yield byte-identical HIR; hir_equivalence_test.go is that gate.
		//
		// ⚠️ The desugar here is NOT a pure node-shape remap: buildTypeMatchDesugar /
		// buildMatchDesugar apply semantic rewrites off parser side tables
		// (it-place writes, index-out read/write normalisation, safe-index synthesis)
		// and materialise an IfExpression, which case *IfExpression then folds into
		// KTypeMatch/KIf. Any move to streaming HIR directly (see design doc §5.2)
		// must PORT those transformations, not merely re-map the arms.
		sp := c.scratchParser()
		// Preparation is shared with the AST route (see prepareSurfaceMatch): it
		// carries D9 index-out annotations onto the clone and runs armPreChecks,
		// whose diagnostics must reach c.h0Diags exactly once.
		clone := sp.prepareSurfaceMatch(e)
		c.h0Diags = append(c.h0Diags, sp.diags...)

		// S-a (§5.2): a type-destructuring match streams straight to KTypeMatch,
		// skipping the IfExpression chain that exists only for case *IfExpression
		// to fold back down. Everything else still takes the legacy two-hop route.
		if t, ok := sp.typeDestructuringSubjectType(clone); ok {
			if id := c.lowerTypeMatchToHIR(sp, clone, t, line, col); id != hir.NoID {
				c.h0Diags = append(c.h0Diags, sp.diags...)
				return id
			}
		}

		// S-b (§5.2): a bare match — one with no subject — streams straight to
		// KIf. Everything else still takes the legacy two-hop route.
		if clone.Matched == nil {
			if id := c.lowerBareMatchToHIR(sp, clone, line, col); id != hir.NoID {
				c.h0Diags = append(c.h0Diags, sp.diags...)
				return id
			}
		}

		// S-c1 (§5.2.9): a subject match on a plain identifier — no index-subject
		// normalisation, no D9 place-write, no ok(cond) wrapper, no enum complement —
		// streams straight to KIf. resolveSubjectMatch must run only here, AFTER
		// S-a and S-b have declined: it carries the A/B/C side effects, and running
		// it before a slice that then returns would apply them twice.
		prep := sp.resolveSubjectMatch(clone)
		c.h0Diags = append(c.h0Diags, sp.diags...)
		if sp.subjectMatchDirectHIR(prep) {
			if id := c.emitSubjectMatchHIR(sp, prep, line, col); id != hir.NoID {
				c.h0Diags = append(c.h0Diags, sp.diags...)
				return id
			}
			break // no arms: fall through to KUnknown, as before
		}
		// resolveSubjectMatch returned nil (a 0-arm subject match, which is
		// ill-formed): emitSubjectMatchAST(nil) would nil-panic, so fall through
		// to KUnknown instead. P7 (design doc §5) deleted the legacy AST desugar
		// route that used to live here.
		break

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

// lowerUnwrapAssignToHIR is the H0 direct emitter for `?=` (UnwrapAssignStatement).
//
// S3: instead of relying on the parser walk's lowerUnwrapAssign to expand the
// sugar node into a BlockStatement (in-place v.Set AST rewrite) and then
// converting that expanded AST to HIR, H0 calls the SAME lowerUnwrapAssign
// (sharing every semantic transformation: resultName detection, D9 index
// branch, guardOptionOperands, type side-table inference, targetIsResult two
// forms, target pre-declaration, ok-arm AssignExpression/LetStatement) and
// converts the resulting BlockStatement to HIR on the spot.
//
// The HIR is byte-identical to the legacy path because the same AST is fed
// through the same c.stmt / case *IfExpression folding — the only difference
// is WHERE the expansion happens (H0 vs parser walk).
//
// Returns hir.NoID on error (diagnostics already collected into c.h0Diags);
// the caller falls back to KUnknown to avoid the tombstone KUnwrapAssign.
func (c *hirConv) lowerUnwrapAssignToHIR(uas *UnwrapAssignStatement, line, col int32) int32 {
	sp := c.scratchParser()
	l := &lowerer{
		p:                  sp,
		visited:            map[uintptr]bool{},
		declaredLocals:     map[string]bool{},
		curFuncName:        c.curFunc,
		resultNameOverride: c.curResultName,
	}
	stmt := l.lowerUnwrapAssign(uas)
	c.h0Diags = append(c.h0Diags, sp.diags...)
	if stmt == nil {
		return hir.NoID
	}
	return c.stmt(stmt)
}

// scratchParser builds the minimal *Parser that H0-side desugaring needs.
//
// ASTToHIR takes only a Program, so hirConv has no parser of its own. Everything
// buildMatchDesugar reaches for is either on SemanticContext (which hirConv
// already holds), or a slice/map read that is nil-safe, so a zero-valued Parser
// with those few fields set is enough:
//
//	sem          — type inference + side tables (shared, not copied)
//	curFuncName  — c.curFunc, so FuncVarType resolves in the right scope
//	idxLocalTypes— restored from the semantic context, which precomputeIdxLocalTypes
//	                now fills whether or not lowering ran (see parser.go)
//	Filename     — for diagnostic text only
//
// saveError/saveWarning land in this parser's own diags slice, which the caller
// folds into c.h0Diags.
func (c *hirConv) scratchParser() *Parser {
	sp := &Parser{
		sem:         c.sem,
		curFuncName: c.curFunc,
		Filename:    c.curFile,
		prog:        c.prog,
	}
	// funcSignatures drives inferTypeFromCallExpr (used by lowerUnwrapAssign
	// for RHS type inference). The real Parser fills it during parsing; the
	// scratch parser must reconstruct it from the merged Program's
	// FunctionDefinitions so ?= expansion sees the same return-type facts.
	if c.prog != nil {
		sp.funcSignatures = make(map[string][]string, len(c.prog.Statements))
		for _, stmt := range c.prog.Statements {
			fd, ok := stmt.(*FunctionDefinition)
			if !ok || fd.Name == "" {
				continue
			}
			rets := make([]string, 0, len(fd.Results))
			for _, res := range fd.Results {
				if res.Type != nil {
					rets = append(rets, typeString(res.Type))
				}
			}
			sp.funcSignatures[fd.Name] = rets
		}
		// 合併內置 / 跨文件簽名快照（SetExternSignatures 注入）：H0 ?= 解箱對
		// 內置呼叫（如 fs.fstat-size）的型別推斷依賴它們。本地定義優先（不覆蓋）。
		for k, v := range c.sem.ExternFuncSigs {
			if _, exists := sp.funcSignatures[k]; !exists {
				sp.funcSignatures[k] = v
			}
		}
		// methodSignatures 同樣由 scratch parser 重建時遺漏，補齊以免方法呼叫型別
		// 推斷缺漏（H0 ?= 解箱僅依賴 funcSignatures 的模組鍵查表，但保持對稱）。
		if len(c.sem.ExternMethodSigs) > 0 {
			if sp.methodSignatures == nil {
				sp.methodSignatures = make(map[string][]string, len(c.sem.ExternMethodSigs))
			}
			for k, v := range c.sem.ExternMethodSigs {
				if _, exists := sp.methodSignatures[k]; !exists {
					sp.methodSignatures[k] = v
				}
			}
		}
	}
	if c.sem != nil {
		if c.sem.IdxLocalTypes == nil {
			c.sem.IdxLocalTypes = make(map[string]map[string]*ty.Type)
		}
		sp.idxLocalTypes = c.sem.IdxLocalTypes
		// Union aliases drive isUnionType, which decides whether a match is a
		// static type-dispatch match or a runtime `x === i32` chain. Without
		// this the two produce different HIR for the same source — see
		// SemanticContext.UnionMembers.
		sp.unionMembers = c.sem.UnionMembers
	}
	return sp
}

// lowerCaptureToHIR 在 H0 內部產出「結構捕獲」的 KLet + KIf 鏈（S3-b 第二步）。
//
// 對應的 AST 軌（默認）：parser 走查期的 maybeCaptureOptionAssign 建出同樣的
// 結構並**寫回用戶的 AST**（V4 違規）。H0 軌改由 CaptureFacts 副表重建素材、
// 呼叫**共用**的 buildCaptureChain，產物只進 HIR——用戶的 AST 全程凍結。
//
// 兩軌共用同一個建構函式，且同樣經 c.stmt / case *IfExpression 折疊，故 HIR
// 逐字節一致；`TestHIRCaptureMatchesASTDesugar` 釘住這條等價。
func (c *hirConv) lowerCaptureToHIR(ls *LetStatement, f *CaptureFact) int32 {
	if ls == nil || f == nil || len(f.Binds) == 0 {
		return hir.NoID
	}
	sp := c.scratchParser()
	l := &lowerer{
		p:              sp,
		visited:        map[uintptr]bool{},
		declaredLocals: map[string]bool{},
		curFuncName:    c.curFunc,
	}
	tok := ls.Token
	subst := make(map[Node]*Identifier, len(f.Binds))
	nodes := make([]captureNode, 0, len(f.Binds))
	for _, b := range f.Binds {
		expr, ok := b.Expr.(Expression)
		if !ok || expr == nil {
			return hir.NoID
		}
		subst[b.Expr] = &Identifier{Token: tok, Value: b.Name}
		nodes = append(nodes, captureNode{expr: expr, inner: b.Inner, name: b.Name})
	}
	// rewritten 由**非破壞性**替換取得：AST 軌是就地改寫 ls.Value，H0 軌不能
	// 碰用戶的樹，故另建一份替換版（只拷貝路徑上的容器，見 substCaptureExprs）。
	rewritten := substCaptureExprs(ls.Value, subst)
	stmt := l.buildCaptureChain(ls, f.Inner, nodes, f.WasDeclared, rewritten)
	c.h0Diags = append(c.h0Diags, sp.diags...)
	if stmt == nil {
		return hir.NoID
	}
	return c.stmt(stmt)
}

// substCaptureExprs 回傳 e 的替換版：m 中的節點換成對應的 `__cap_N` 識別符。
//
// **非破壞性**——原樹一律不動（AST 凍結正是 S3-b 的目的）。只有「通往被捕獲
// 節點的路徑上」的容器被淺拷貝，其餘子樹回傳**原指標**：語義副表
// （InferredType / TypeOverride）以原始指標為鍵，盡量保留原身份才能讓型別
// 解析與 AST 降糖路徑一致（見 CaptureFact.Binds 的註解）。
//
// 只需處理 collectCaptureNodes 實際會捕獲的容器型別；其餘節點原樣回傳。
func substCaptureExprs(e Expression, m map[Node]*Identifier) Expression {
	if e == nil || len(m) == 0 {
		return e
	}
	if id, ok := m[e]; ok {
		return id
	}
	switch v := e.(type) {
	case *GroupedExpression:
		if sub := substCaptureExprs(v.Expression, m); sub != v.Expression {
			cp := *v
			cp.Expression = sub
			return &cp
		}
	case *InfixExpression:
		left, right := substCaptureExprs(v.Left, m), substCaptureExprs(v.Right, m)
		if left != v.Left || right != v.Right {
			cp := *v
			cp.Left, cp.Right = left, right
			return &cp
		}
	case *IndexExpression:
		left, index := substCaptureExprs(v.Left, m), substCaptureExprs(v.Index, m)
		if left != v.Left || index != v.Index {
			cp := *v
			cp.Left, cp.Index = left, index
			return &cp
		}
	case *CallExpression:
		changed := false
		args := make([]Expression, len(v.Arguments))
		for i, a := range v.Arguments {
			args[i] = substCaptureExprs(a, m)
			if args[i] != a {
				changed = true
			}
		}
		if changed {
			cp := *v
			cp.Arguments = args
			return &cp
		}
	}
	return e
}

// lowerTypeMatchToHIR emits KTypeMatch for a type-destructuring match directly
// from the resolved arms, without materialising the IfExpression chain that
// buildTypeMatchDesugar builds purely for case *IfExpression to fold back down.
//
// This is slice S-a of design doc §5.2, the first step toward retiring the 12
// lowered-match fields on IfExpression (P7): once every match kind streams HIR,
// nothing writes those fields any more.
//
// It MUST produce byte-identical HIR to the two-hop route. The invariants that
// make that true today:
//
//  1. Position — pos tells them apart only via Token, and buildTypeMatchDesugar
//     stamps IfExpression.Token = sm.Token, so pos(sm) == pos(ifExpr).
//  2. Node creation order — HIR node ids are allocation order sensitive, so this
//     mirrors case *IfExpression exactly: every arm's body (and optional binding
//     name) is emitted before its KTypeArm, all arms precede the "matched" slot,
//     and KTypeMatch is created last.
//  3. Per-arm decisions — shared via resolveTypeMatchArms, so the two routes
//     cannot disagree on pattern / binding / default-ness / injected let.
func (c *hirConv) lowerTypeMatchToHIR(sp *Parser, sm *SurfaceMatch, matchedVarType string, line, col int32) int32 {
	resolved := sp.resolveTypeMatchArms(sm, matchedVarType)
	if len(resolved) == 0 {
		return hir.NoID
	}
	arms := make([]int32, 0, len(resolved))
	for _, ra := range resolved {
		armNodes := []int32{c.slot("body", c.stmt(ra.body))}
		if ra.bindingName != "" {
			armNodes = append(armNodes, c.slot("bind", c.b.Add(hir.Node{
				Kind: hir.KName, S: c.b.Intern(ra.bindingName),
			})))
		}
		arms = append(arms, c.b.Add(hir.Node{
			Kind:  hir.KTypeArm,
			S:     c.b.Intern(ra.typePattern),
			Flags: hir.FlagTypeDefault * u32(ra.isDefault),
			First: c.kids(armNodes),
			Line:  line, Col: col,
		}))
	}
	return c.b.Add(hir.Node{
		Kind: hir.KTypeMatch,
		First: c.kids(append([]int32{
			c.slot("matched", c.expr(sm.Matched)),
		}, arms...)),
		Line: line, Col: col,
	})
}

// lowerBareMatchToHIR emits KIf for a bare match (a match with no subject)
// directly from the resolved plan, without materialising the IfExpression chain
// that buildBareMatchDesugar builds purely for case *IfExpression to fold back.
//
// This is slice S-b of design doc §5.2. Same contract as lowerTypeMatchToHIR:
// byte-identical HIR or it does not ship.
//
//  1. Position — every synthetic IfExpression carries Token = sm.Token, so
//     pos(sm) == pos(ifExpr) for the outer node and for each wrapper.
//  2. Node creation order — case *IfExpression allocates slots in the fixed
//     order cond, then, else, matched, dot-val, range, eq, raw, then the KIf
//     itself. A bare match has no matched / raw / option / value operands, so
//     those slots collapse to NoID and vanish; the rest keep their relative
//     order. An `alt` arm lowers through a synthetic
//     BlockStatement{ExpressionStatement} wrapper, which is
//     KBlock -> KExprStmt -> KIf: the inner KIf is allocated FIRST, then
//     KExprStmt, then KBlock, mirroring c.stmt's inside-out evaluation. An
//     `altBody` (a wildcard arm's own body) is a real BlockStatement reached
//     directly and must still go through c.stmt so its annotations and embed
//     data attach. The synthetic wrappers carry no annotations and no embed
//     data (both side tables are keyed by node pointer), so emitting them by
//     hand drops nothing.
//  3. Per-arm decisions — shared via resolveBareMatchPlan, so the AST and HIR
//     emitters cannot disagree on guard-vs-elif, dotVal, or default-body rules.
//  4. RTBareMatch / RTMatchWildcard / RTMatchWrapper are parser-side flags with
//     no HIR representation (case *IfExpression sets no Flags on KIf); their
//     readers are fmt/control.go and tools/check_enum.go, both of which walk
//     the sugar tree. Dropping them here is therefore HIR-neutral.
func (c *hirConv) lowerBareMatchToHIR(sp *Parser, sm *SurfaceMatch, line, col int32) int32 {
	plan := sp.resolveBareMatchPlan(sm.Token, sm.Arms)
	if plan == nil {
		return hir.NoID
	}
	return c.emitMatchIfHIR(plan, line, col)
}

// emitMatchIfHIR streams a matchIfPlan as KIf nodes. It is the single HIR
// emitter for every match kind now that bare and subject matches share the plan
// type.
//
// Slot order is the one case *IfExpression uses — cond, then, else, matched,
// dot-val, range, eq, raw, then the OptionPatterns / ValuePatterns lists in
// their original order. A bare match leaves matched / raw / both pattern lists
// empty, so those slots collapse to NoID and vanish, which is exactly what the
// two-hop route produces for it.
func (c *hirConv) emitMatchIfHIR(plan *matchIfPlan, line, col int32) int32 {
	if plan.isWrapper {
		// `if (1) { if c1 { B1 }; if c2 { B2 }; ... }`: each guard lowers to its
		// own KExprStmt (hir2mir's lowerBlock evaluates them one by one, so every
		// matching arm runs), and the wrapper KIf is allocated last.
		stmts := make([]int32, 0, len(plan.stmts))
		for _, sub := range plan.stmts {
			inner := c.emitMatchIfHIR(sub, line, col)
			stmts = append(stmts, c.b.Add(hir.Node{
				Kind: hir.KExprStmt, First: inner, Line: line, Col: col,
			}))
		}
		return c.b.Add(hir.Node{
			Kind: hir.KIf,
			First: c.kids([]int32{
				c.slot("cond", c.expr(plan.cond)),
				c.slot("then", c.b.Add(hir.Node{
					Kind: hir.KBlock, First: c.kids(stmts), Line: line, Col: col,
				})),
				c.slot("matched", c.expr(plan.matched)),
			}),
			Line: line, Col: col,
		})
	}
	alt := hir.NoID
	switch {
	case plan.alt != nil:
		inner := c.emitMatchIfHIR(plan.alt, line, col)
		alt = c.b.Add(hir.Node{
			Kind: hir.KBlock,
			First: c.kids([]int32{c.b.Add(hir.Node{
				Kind: hir.KExprStmt, First: inner, Line: line, Col: col,
			})}),
			Line: line, Col: col,
		})
	case plan.altBody != nil:
		alt = c.stmt(plan.altBody)
	}
	nodes := []int32{
		c.slot("cond", c.expr(plan.cond)),
		c.slot("then", c.stmt(plan.body)),
		c.slot("else", alt),
		c.slot("matched", c.expr(plan.matched)),
		c.slot("dot-val", c.stmt(plan.dotValBody)),
		c.slot("range", c.expr(plan.rangePattern)),
		c.slot("eq", c.expr(plan.eqPattern)),
		c.slot("raw", c.expr(plan.rawCond)),
	}
	for _, o := range plan.optionPatterns {
		nodes = append(nodes, c.slot("opt", c.b.Add(hir.Node{
			Kind: hir.KName, S: c.b.Intern(o),
		})))
	}
	for _, v := range plan.valuePatterns {
		nodes = append(nodes, c.slot("val", c.expr(v)))
	}
	return c.b.Add(hir.Node{Kind: hir.KIf, First: c.kids(nodes), Line: line, Col: col})
}

// subjectMatchDirectEmits counts how many subject matches took the S-c1
// direct-HIR route, i.e. emitSubjectMatchHIR returned a real node id.
//
// It exists for the dump package's anti-vacuity gate
// (TestSubjectMatchDirectHIRIsExercised): the byte-equivalence gate compares
// Route A against Route B, and if the corpus never reaches the new emitter the
// comparison is 0 == 0 — the exact failure mode that once made that gate
// vacuous (see matchDesugarViaH0's history note). Atomic because ASTToHIR is
// not single-threaded in every caller.
var subjectMatchDirectEmits atomic.Int64

// SubjectMatchDirectEmits reports the running total of S-c1 direct emissions
// since process start. Test-only visibility into the counter above.
func SubjectMatchDirectEmits() int64 { return subjectMatchDirectEmits.Load() }

// emitSubjectMatchHIR streams a subject match (a match WITH a subject
// expression) straight to KIf, bypassing the IfExpression chain that
// emitSubjectMatchAST builds purely for case *IfExpression to fold back.
//
// This is slice S-c1 of design doc §5.2.9. Same contract as lowerBareMatchToHIR:
// byte-identical HIR or it does not ship. The two extra invariants over S-b:
//
//  1. `matched` is a real slot here (KIf's 4th child), emitted for every arm and
//     for the all-wildcard tail; a bare match leaves it nil so it vanishes.
//  2. OptionPatterns / ValuePatterns become labelled "opt" / "val" slots in their
//     original list order, AFTER the eight fixed slots — the same order
//     case *IfExpression uses.
//
// The plan tree (resolveSubjectMatchPlan) exists because D's plain-wildcard
// branch patches an already-built node's else slot, which no emitted KIf can
// accept; see the comment there.
func (c *hirConv) emitSubjectMatchHIR(sp *Parser, prep *subjectMatchPrep, line, col int32) int32 {
	if !sp.subjectMatchDirectHIR(prep) {
		return hir.NoID
	}
	plan := sp.resolveSubjectMatchPlan(prep)
	if plan == nil {
		// Only reachable when the match has no arms at all, which
		// resolveSubjectMatch already reported by returning a nil prep. Emitting
		// nothing matches buildMatchDesugar returning nil.
		return hir.NoID
	}
	subjectMatchDirectEmits.Add(1)
	inner := c.emitMatchIfHIR(plan, line, col)
	// The E-segment wrappers that emitSubjectMatchAST builds around the chain
	// must be mirrored here so the HIR is byte-identical to the legacy route.
	// hasRawCond wraps the chain in `if 1 { it = matched; <chain> }`; subjName
	// (index-subject normalisation) is the OUTER of the two (emitSubjectMatchAST
	// applies it after hasRawCond), so we wrap in that order. matchedIsEnum needs
	// no extra wrapper (its per-arm rewrite is shared via rewriteSubjectArm).
	return c.emitSubjectMatchWrappersHIR(prep, inner, line, col)
}

// emitSubjectMatchWrappersHIR wraps the already-emitted chain KIf in the same
// `if 1 { ... }` blocks the legacy AST route (emitSubjectMatchAST) builds for the
// ok(cond) and index-subject-normalisation cases. It only touches prep state that
// emitSubjectMatchAST also reads for the wrappers, so the two routes cannot
// disagree about the wrapper shape.
func (c *hirConv) emitSubjectMatchWrappersHIR(prep *subjectMatchPrep, inner int32, line, col int32) int32 {
	tok := prep.tok
	matched := prep.matched
	// Reuse c.expr so the int-literal node is byte-identical to the one
	// emitSubjectMatchAST produces (KIntLit with its S/S2 interned strings).
	one := func() int32 {
		return c.expr(&IntegerLiteral{Token: tok, Value: 1})
	}
	chainExpr := func(id int32) int32 {
		return c.b.Add(hir.Node{Kind: hir.KExprStmt, First: id, Line: line, Col: col})
	}

	// ok(cond): `if 1 { it = matched; <chain> }` — bind `it` before the chain so
	// the condition can reference it. Mirrors emitSubjectMatchAST's hasRawCond block.
	if prep.hasRawCond && prep.itStmt != nil {
		block := c.b.Add(hir.Node{
			Kind: hir.KBlock,
			First: c.kids([]int32{
				c.stmt(prep.itStmt),
				chainExpr(inner),
			}),
			Line: line, Col: col,
		})
		inner = c.b.Add(hir.Node{
			Kind: hir.KIf,
			First: c.kids([]int32{
				c.slot("cond", one()),
				c.slot("then", block),
				c.slot("matched", c.expr(matched)),
			}),
			Line: line, Col: col,
		})
	}

	// Index-subject normalisation: `if 1 { [let __idx = <orig>;] let __match_subj ?elem = <orig>; <chain> }`.
	// The chained arm `it` bindings and `== ok` conditions then operate on the
	// synthesised ?elem option variable. Mirrors emitSubjectMatchAST's subjName block.
	if prep.subjName != "" && prep.subjElem != "" {
		stmts := make([]int32, 0, 3)
		if prep.placeWriteUsed && prep.origIdxExpr != nil {
			idxLet := &LetStatement{
				Token:       tok,
				Name:        &Identifier{Token: tok, Value: prep.idxTempName},
				IsSynthetic: true,
				Value:       prep.origIdxExpr,
			}
			stmts = append(stmts, c.stmt(idxLet))
		}
		subjValue := prep.origMatched
		if prep.placeWriteUsed && prep.origIdxExpr != nil {
			// D9: rewrite the read index to the __idx temp so the bounds-check
			// read and the ok-arm write-back share one index evaluation. Built
			// fresh (never mutate prep.origMatched — H0 must not touch the AST).
			if idx, ok := prep.origMatched.(*IndexExpression); ok {
				subjValue = &IndexExpression{
					Token: idx.Token,
					Left:  idx.Left,
					Index: &Identifier{Token: idx.Token, Value: prep.idxTempName},
				}
				// Transfer the InBounds proof from the original (proven) read to
				// this fresh read node. prepareSurfaceMatch shallow-copies the
				// SurfaceMatch, so prep.origMatched is the SAME *IndexExpression the
				// bounds analyzer proved in-bounds in prepareInBoundsMetadata; the
				// rewritten node reads the same element (via __idx == the original
				// index), so it is equally in bounds. Without this, c.expr(subjValue)
				// sees no FlagInBounds and lowerSafeIndex re-inserts a runtime check
				// (mir/d9_write_bce_test.go regresses). c.sem == prog.Sem == p.sem.
				if c.sem.IndexIsInBounds(prep.origMatched) {
					c.sem.SetIndexInBounds(subjValue, true)
				}
				// D9 grow-write: this fresh node is the WRITE subject. For a
				// growable container (slice/vec) the match subject must gate on
				// `i >= 0` (grow/allocate on write), not the read semantics
				// `0 <= i < len` — an unallocated vec (len=0) would otherwise
				// make every index out-of-bounds and the write is silently lost.
				// hir2mir routes a FlagGrowWrite index to lowerGrowWriteIndex
				// ONLY when the container is actually growable and not already
				// provably in-bounds, so fixed arrays keep the read-style bounds
				// check (mir/d9_write_bce_test.go regresses otherwise).
				c.sem.SetGrowWrite(subjValue, true)
			}
		}
		subjAssign := &LetStatement{
			Token:       tok,
			Name:        &Identifier{Token: tok, Value: prep.subjName},
			IsSynthetic: true,
			Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: prep.subjElem}},
			Value:       subjValue,
		}
		stmts = append(stmts, c.stmt(subjAssign), chainExpr(inner))
		block := c.b.Add(hir.Node{
			Kind:  hir.KBlock,
			First: c.kids(stmts),
			Line:  line, Col: col,
		})
		// The wrapper's matched slot is the synthesised subject variable
		// (__match_subj), NOT prep.matched: emitSubjectMatchAST sets the
		// wrapper's MatchedExpr to &Identifier{subjName} explicitly, while
		// prep.matched stays the original index expression. HasRawCond uses
		// prep.matched on purpose; subjName does not.
		inner = c.b.Add(hir.Node{
			Kind: hir.KIf,
			First: c.kids([]int32{
				c.slot("cond", one()),
				c.slot("then", block),
				c.slot("matched", c.expr(&Identifier{Token: tok, Value: prep.subjName})),
			}),
			Line: line, Col: col,
		})
	}
	return inner
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
