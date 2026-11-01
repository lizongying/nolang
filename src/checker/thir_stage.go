package checker

// thir_stage.go — Stage 6C：Checker-on-THIR 影子双跑（分组迁移的 THIR 变体承载）。
//
// 背景（docs/design/nolang-parser-lowering-invariants.md §3.5 目标管线）：
// H0（ASTToHIRWithMap）之后的 L1 THIR = hir.Package + 类型化副表
// （Inferred / Node.Type / Syms），Checker-on-THIR 应从副表取类型结论，
// 而不是在 AST 上重算。迁移方式（计划 §4 6C）：**walker 结构保留 AST +
// 糖 case，类型结论改读 THIR 副表**——避免 11.5k 行 arena walker 重写。
//
// 已迁移（ShadowCompareTHIR 十四对）：
//
//   - THIRView：idMap（AST 节点 → HIR id）× 副表的查询门户。TypeOf 是
//     类型结论的唯一入口；FuncSigs / FuncResultTypes 是宣告事实的 HIR 侧
//     提取（KFuncDef / KExtern → 签名 / 回传型别）。
//   - ValidateFuncArgCountTHIR：签名表改由 HIR 提取（KParam.First 恢复
//     HasDefault），call-site walker（checkArgCountInStmt/Expr）不动。
//   - ValidateDivByZeroTHIR：触发条件纯语法（isLiteralZeroDivisor），
//     类型确认（「这是不是整数除法」）因表达式级型别尚未入副表而暂缓
//     ——见 TypeOf 注释；本批保持触发条件与旧版逐字一致。
//   - ValidateStrOrderingTHIR：funcTypes 表改由 HIR 回传型别优先、
//     collectStrOrderingFuncTypes 兜底；走查（walkStrOrderingStmt）不动。
//   - ValidateStringConcatTHIR：同 DivByZero，触发纯语法，型别精化暂缓。
//   - ValidateOptionComparisonTHIR：funcTypes 改由 FuncResultTypes 优先、
//     collectOptionCmpFuncTypes 兜底；walker 不动。
//   - ValidateIntOverflowTHIR：collect 系登记值经 declTypeOf 改读 THIR
//     副表（TypeOf 优先、AST 值兜底）；walker / operandIntKind 不动，
//     funcTypes 快照与 sem.FuncVarTypes gap-fill 保留 AST 来源（牵动
//     registerInferredNonIntLet 推断面，换源属行为变更，影子期禁）。
//   - ValidateInterfaceImplementationTHIR：接口方法与 impl 方法的
//     param/result 签名字串四处取值点经 declTypeOf 改读 THIR 副表；
//     签名比对（含 receiver 替换）与 Implements 范围限定不动。
//   - ValidateLHSInferredBuiltinsTHIR：param/result/let 登记值经 declTypeOf
//     改读 THIR 副表；**硬闸**——登记 gate 与 hasRealTypeAnnotation（AST
//     糖面事实）刻意保留 AST 口径，见 runLHSInferredCore 注释。
//   - ValidateStrIndexComplexityTHIR：scopeTypes / funcReturns 的声明登记值
//     （let 声明型別 / param / funcReturns）经 declTypeOf 改读 THIR 副表；
//     exprTypeString 推断面（字面量、标识符传播、调用回传）与 asciiVars
//     收集保持 AST。
//   - ValidateUnhandledOverflowTHIR：seedVarTypes / functionScopeFacts / let /
//     for-init 登记值与 option 宣告检查（`?T` 前缀）的字串来源经 declTypeOf；
//     inferExprType / iterElemType 推断面保持 AST。
//   - ValidateUnhandledIndexTHIR：option 宣告检查（`?T` 前缀）字串来源经
//     declTypeOf；基底型别查询（sem.IdxLocalTypes / FuncVarType）已是副表不动。
//   - ValidateIndexWriteTHIR：零 AST 型别直读（容器型别只查 IdxLocalTypes），
//     纯 wrapper——保留 view 参数为未来基底型别迁 THIR 留口。
//   - ValidateFuncArgsTHIR：sigs 签名表改 FuncSigs 优先（AST/导入收集补缺）；
//     topLevelVarTypes 与 walker 声明登记值经 declTypeOf。structFields（折叠
//     修饰口径分叉）与推断面（inferExprType / funcSigFirstReturnType）留 AST。
//   - ValidateTypesTHIR：funcTypes（含 extern）/ 重复签名键 paramTypes /
//     topLevelVarTypes / selfType 与 validateStmtTypes 声明登记值经 declTypeOf；
//     推断面与 hasRealTypeAnnotation 留原口径，子验证器两变体同调。
//
// 影子双跑（ShadowCompareTHIR）：同一只程序上旧版 vs THIR 版诊断多重集
// 对拍，键 = (File,Line,Column,TraceID,Message)。本批门禁要求全等——
// 迁移是「换类型结论来源」，不是行为变更。

import (
	"fmt"
	"sync"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// THIRView couples the AST→HIR id map produced by parser.ASTToHIRWithMap with
// the typed HIR side tables, so a checker walker that stays on the AST for
// sugar-node coverage can ask THIR-side questions without reaching back into
// the checker's own inference pass.
type THIRView struct {
	Pkg   *hir.Package
	IdMap map[parser.Node]int32

	// 显式 type map（6C-optional 第二步）：表达式级推断结果的节点键表，
	// **惰性构建** —— 只在真有调用方查询表达式型别时才付一次全树收集代价。
	// 见 thir_exprtypes.go 文件头注释（为什么不写 pkg.Inferred / 时序桥接）。
	prog      *parser.Program
	exprOnce  sync.Once
	exprTypes map[parser.Node]*ty.Type
	// exprBuilding 標記惰性全樹收集正在進行中（見 thir_exprtypes.go
	// ExprTypeOf）。sync.Once 不可重入：collectExprTypes 的走查會經
	// collectFuncDeclared → declTypeOf → TypeOf 對「不在 IdMap 的運算式節點
	// （含 *Parameter，它實現了 expressionNode）」再次呼叫 ExprTypeOf，
	// 若不擋下就會 Do 套 Do 自鎖 → 全進程死鎖。構建中直接回 ""，讓
	// declTypeOf 退回 legacy AST 值兜底。見 ExprTypeOf 註解。
	exprBuilding bool
}

// NewTHIRView returns a view over the AST→HIR mapping produced by
// parser.ASTToHIRWithMap + parser.PopulateInferredTypes. Either argument may
// be nil; the view then answers "" / empty (callers fall back to the legacy
// AST-side sources).
func NewTHIRView(pkg *hir.Package, idMap map[parser.Node]int32) *THIRView {
	return &THIRView{Pkg: pkg, IdMap: idMap}
}

// BuildTHIRView builds a THIRView for the program by running the H0 lowerer
// (parser.ASTToHIRWithMap) and the type populator (parser.PopulateInferredTypes),
// with full isolation of H0 side effects so that lint / validator callers see
// exactly the same program state as the legacy pre-H0 path:
//
//   - h0Diags: ASTToHIRWithMap folds desugar diagnostics into prog.Errors /
//     prog.Warnings (tohir.go ordering note — "this mutates the caller's
//     Program, so it is not idempotent"). Roll back by restoring the original
//     slices so runLintParserWarnings and the hard-error block see the same
//     diagnostics as today.
//   - Sem name-level maps: H0 match desugar writes VarTypes / FuncVarTypes /
//     DeclaredVars (lowering.go arm-binding registration at 3479/3490,
//     __match_subj synthesis at 2986, unwrap-assign targets at 4787+, etc.)
//     and IdxLocalTypes (precomputeIdxLocalTypes gap-fill). Roll back so
//     validators that read these maps (ValidateUndefinedVars' DeclaredVars
//     iteration, match_exhaustive's FuncVarType lookup, IntOverflow /
//     UnhandledOverflow gap-fill, etc.) see the pre-H0 state — the exact
//     state the legacy path observes today (RunAllLints / hard-error block
//     run BEFORE ASTToHIRWithMap in the shipped pipeline).
//
// Node-keyed tables (TypeFacts / NodeFacts) are NOT restored: H0 does not
// write TypeFacts / NodeFacts. Annotations are no longer a table at all —
// they live on the AST nodes (parser.AnnotatedNode) and ride with the tree.
//
// The returned view is built from the HIR side tables (Inferred / Node.Type)
// which are independent of the restored name-level maps, so the view's answers
// are correct regardless of the rollback.
func BuildTHIRView(program *parser.Program) *THIRView {
	if program == nil {
		return nil
	}
	// Snapshot diagnostic slices for h0Diags rollback.
	savedErrs := program.Errors
	savedWarns := program.Warnings

	// Snapshot name-level Sem maps for H0 desugar write rollback.
	sem := program.Sem
	var savedVarTypes map[string]*ty.Type
	var savedFuncVarTypes map[string]map[string]*ty.Type
	var savedDeclaredVars map[string]bool
	var savedIdxLocalTypes map[string]map[string]*ty.Type
	if sem != nil {
		savedVarTypes = copyTypeMap(sem.VarTypes)
		savedFuncVarTypes = copyTypeTypeMap(sem.FuncVarTypes)
		savedDeclaredVars = copyBoolMap(sem.DeclaredVars)
		savedIdxLocalTypes = copyTypeTypeMap(sem.IdxLocalTypes)
	}

	hirPkg, idMap := parser.ASTToHIRWithMap(program)

	// Roll back h0Diags (not idempotent — see tohir.go ordering note).
	program.Errors = savedErrs
	program.Warnings = savedWarns

	// Roll back H0 match desugar Sem name-level writes.
	if sem != nil {
		sem.VarTypes = savedVarTypes
		sem.FuncVarTypes = savedFuncVarTypes
		sem.DeclaredVars = savedDeclaredVars
		sem.IdxLocalTypes = savedIdxLocalTypes
	}

	parser.PopulateInferredTypes(hirPkg, idMap, sem)
	view := NewTHIRView(hirPkg, idMap)
	// 惰性收集入口：只持引用，真正的全树走查推迟到首次表达式型别查询
	// （ExprTypeOf）。当下无生产消费方 ⇒ 生产路径零次构建、零开销。
	view.prog = program
	return view
}

// copyStrMap returns a shallow copy of m (nil in → nil out).
func copyStrMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// copyBoolMap returns a shallow copy of m (nil in → nil out).
func copyBoolMap(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	cp := make(map[string]bool, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// copyTypeMap returns a shallow copy of m (nil in → nil out). Types are
// immutable, so copying the map is enough to snapshot it.
func copyTypeMap(m map[string]*ty.Type) map[string]*ty.Type {
	if m == nil {
		return nil
	}
	cp := make(map[string]*ty.Type, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// copyTypeTypeMap returns a deep copy of m (inner maps copied; nil in → nil out).
func copyTypeTypeMap(m map[string]map[string]*ty.Type) map[string]map[string]*ty.Type {
	if m == nil {
		return nil
	}
	cp := make(map[string]map[string]*ty.Type, len(m))
	for k, inner := range m {
		if inner == nil {
			cp[k] = nil
			continue
		}
		icp := make(map[string]*ty.Type, len(inner))
		for ik, iv := range inner {
			icp[ik] = iv
		}
		cp[k] = icp
	}
	return cp
}

// copyStrStrMap returns a deep copy of m (inner maps copied; nil in → nil out).
func copyStrStrMap(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]map[string]string, len(m))
	for k, inner := range m {
		if inner == nil {
			cp[k] = nil
			continue
		}
		icp := make(map[string]string, len(inner))
		for ik, iv := range inner {
			icp[ik] = iv
		}
		cp[k] = icp
	}
	return cp
}

// TypeOf returns the effective type string for an AST node from the THIR side
// tables，按三档解析：
//
//  1. Inferred 副表（PopulateInferredTypes：TypeOverride 优先，其次 nodeType
//     读宣告 .Type）—— 宣告类节点的主要来源；
//  2. HIR 节点自身的 interned Type；
//  3. **显式 type map**（6C-optional 第二步落地）：表达式级推断结果，
//     惰性构建，见 ExprTypeOf / thir_exprtypes.go。
//
// 第 3 档使 Identifier / InfixExpression / CallExpression / DotExpression /
// IndexExpression 等**表达式**节点首次可答——它们没有 `Type` 字段、不在
// idMap 的两档里，此前恒为 ""。第 3 档只含表达式键，宣告类节点即使触发
// 构建也查不到自己，答案不变（零行为变化）。调用方仍须为 "" 留 legacy 兜底。
func (v *THIRView) TypeOf(n parser.Node) string {
	if t := v.TypeRefOf(n); t != nil {
		return t.Raw()
	}
	return ""
}

// TypeRefOf is the STRUCTURED form of TypeOf: the same three-tier resolution
// (Inferred side table → the HIR node's own interned type → the expression map)
// but returning a *ty.Type so callers ask Kind/Bits/Signed instead of matching
// spelling prefixes. Returns nil when THIR knows nothing about n, which is the
// same condition that made TypeOf answer "".
func (v *THIRView) TypeRefOf(n parser.Node) *ty.Type {
	if v == nil || v.Pkg == nil || n == nil {
		return nil
	}
	if id, ok := v.IdMap[n]; ok {
		if t := v.Pkg.InferredRef(id); t != nil {
			return t
		}
		if node := v.Pkg.Node(id); node != nil {
			if t := v.Pkg.TypeRef(node.Type); t != nil {
				return t
			}
		}
	}
	// 第三檔：顯式 type map（運算式級推斷，惰性構建）。只對運算式節點開放——
	// 宣告類節點（LetStatement / Parameter / StructField …）不是 Expression，
	// 在此直接回 nil，絕不觸發全樹收集 ⇒ 生產路徑零開銷。
	if _, isExpr := n.(parser.Expression); isExpr {
		return v.ExprTypeRef(n)
	}
	return nil
}

// FuncSigs extracts named-function signatures from the HIR arena — the
// THIR-side replacement for building the map from AST FunctionDefinitions via
// funcSigFromDef。与 funcSigFromDef 逐字段对齐：
//
//   - ParamTypes：KParam 子节点（Name=S、Type=interned Type），
//     HasDefault 由 KParam.First 恢复（默认表达式降糖后挂 First，
//     NoID 即无默认值——tohir.go param()）。
//   - ResultTypes：KResult 子节点；方法的隐式 self 是第一个 KResult
//     （funcLike 先发 self 再发 KParam），按 FlagMethod+名 "self" 跳过，
//     与 funcSigFromDef 的 Results[0] 剔除逻辑一致。
//   - Variadic：KFuncDef 的 FlagVariadic。
//
// 只收集 KFuncDef（ValidateFuncArgCount 的签名表同样只收
// FunctionDefinition，不含 extern——保持逐字口径）。
func (v *THIRView) FuncSigs() map[string]*funcSig {
	out := make(map[string]*funcSig)
	if v == nil || v.Pkg == nil {
		return out
	}
	pkg := v.Pkg
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil || n.Kind != hir.KFuncDef {
			continue
		}
		sig := &funcSig{
			Variadic:    n.Has(hir.FlagVariadic),
			ParamTypes:  make([]paramInfo, 0),
			ResultTypes: make([]paramInfo, 0),
		}
		selfPending := n.Has(hir.FlagMethod)
		for c := n.First; c != hir.NoID; c = pkg.Nodes[c].Next {
			cn := &pkg.Nodes[c]
			switch cn.Kind {
			case hir.KParam:
				sig.ParamTypes = append(sig.ParamTypes, paramInfo{
					Name:       pkg.Str(cn.S),
					Type:       pkg.Type(cn.Type),
					HasDefault: cn.First != hir.NoID,
				})
			case hir.KResult:
				if selfPending {
					selfPending = false
					if pkg.Str(cn.S) == "self" {
						continue
					}
				}
				sig.ResultTypes = append(sig.ResultTypes, paramInfo{
					Name: pkg.Str(cn.S),
					Type: pkg.Type(cn.Type),
				})
			}
		}
		out[pkg.Str(n.S)] = sig
	}
	return out
}

// FuncResultTypes returns name → first declared result type for every named
// function（KFuncDef）与 extern 宣告（KExtern）——collectStrOrderingFuncTypes
// 宣告走查的 THIR 侧等价物。方法的 self 结果按同口径剔除。只登记有型别的
// 首结果（与 `rs[0].Type != nil` 才登记一致）。collectStrOrderingFuncTypes
// 为未合并 std 预填的方法表（str.slice 等）不在这里复制——调用方用 AST
// 收集器兜底。
func (v *THIRView) FuncResultTypes() map[string]string {
	out := make(map[string]string)
	if v == nil || v.Pkg == nil {
		return out
	}
	pkg := v.Pkg
	for _, id := range pkg.Top {
		n := pkg.Node(id)
		if n == nil || (n.Kind != hir.KFuncDef && n.Kind != hir.KExtern) {
			continue
		}
		isMethod := n.Kind == hir.KFuncDef && n.Has(hir.FlagMethod)
		selfPending := isMethod
		for c := n.First; c != hir.NoID; c = pkg.Nodes[c].Next {
			cn := &pkg.Nodes[c]
			if cn.Kind != hir.KResult {
				continue
			}
			if selfPending {
				selfPending = false
				if pkg.Str(cn.S) == "self" {
					continue
				}
			}
			if t := pkg.Type(cn.Type); t != "" {
				out[pkg.Str(n.S)] = t
			}
			break
		}
	}
	return out
}

// ValidateFuncArgCountTHIR 是 ValidateFuncArgCount 的 THIR 侧变体：
// call-site walker（checkArgCountInStmt / checkArgCountInExpr）不动
// （含 SurfaceMatch 糖 case），签名表改由 THIRView.FuncSigs（HIR
// KFuncDef 提取）构建；HIR 漏掉的名字退回 funcSigFromDef（防御性——
// 影子双跑须多重集全等）。
func ValidateFuncArgCountTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	if program == nil {
		return nil
	}
	sigs := make(map[string]*funcSig)
	for name, sig := range view.FuncSigs() {
		sigs[name] = sig
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if _, exists := sigs[fd.Name]; !exists {
				sigs[fd.Name] = funcSigFromDef(fd)
			}
		}
	}
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, checkArgCountInStmt(stmt, sigs)...)
	}
	return results
}

// ValidateDivByZeroTHIR 是 ValidateDivByZero 的 THIR 侧变体。
//
// 触发条件仍以纯语法为主——「除数是整数字面量 0」；6C-optional 第三批起接入
// **型別确认精化**（isFloatArithOperand）：被除數確定為浮點時跳過，因為該除法
// 走 LLVM fdiv（IEEE-754 定義 x/0.0 = ±inf），不是 sdiv/srem UB。判據走
// THIRView.TypeOf（表達式級型別表），這是該表的第一個生產消費方。
//
// 與 legacy 同步精化（ValidateDivByZero 兩側同 view）⇒ 影子對拍保持全等；
// 行為變更（浮點用例由報轉不報）由獨立正向測試鎖定。
func ValidateDivByZeroTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return ValidateDivByZero(program, view)
}

// ValidateStringConcatTHIR 是 ValidateStringConcat 的 THIR 侧变体。
//
// 触发条件：`+` 任一侧是 StringLiteral（既有）；6C-optional 第四批起接入**型別
// 精化補報**——两侧都确定为字串（str/txt）时同样报，覆盖 `str_var + str_var`
// 此前的漏报。判据走 THIRView.TypeOf（表达式级型别表，同 isFloatArithOperand）。
//
// 保守口徑：型別未知即不補報，故本精化**只增不減**（新增 `str+str` 報告，
// 其餘用例報告集合不變）。與 legacy 同步（ValidateStringConcat 兩側同 view）
// ⇒ 影子對拍保持全等；行為變更由獨立正向測試鎖定。
func ValidateStringConcatTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return ValidateStringConcat(program, view)
}

// ValidateOptionComparisonTHIR 是 ValidateOptionComparison 的 THIR 侧变体：
// 回传型别表（funcTypes，含 `?T` option 标记）改由 THIRView.FuncResultTypes
// （KFuncDef/KExtern 首结果型别）优先、collectOptionCmpFuncTypes（AST 宣告 +
// FunctionLiteral let）兜底。walker（optCmpWalker，含 SurfaceMatch 糖 case）
// 与 locals/untyped/unwrapped 作用域跟踪全不动。
//
// 口径备忘：collectOptionCmpFuncTypes 对方法（Results[0]=self）登记的是 self
// 型别，与 FuncResultTypes 的「剔 self 首结果」不同——但 optCmpWalker.typeOf
// 只查裸識別符呼叫（CallExpression.Function 为 Identifier 才进 funcTypes），
// 方法条目是死数据，两种口径不触达同一查询键。duplicate 名字的 first-wins
// （AST）vs last-wins（HIR）差异由影子双跑把关。
func ValidateOptionComparisonTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	if program == nil {
		return nil
	}
	funcTypes := make(map[string]string)
	for k, t := range view.FuncResultTypes() {
		funcTypes[k] = t
	}
	for k, t := range collectOptionCmpFuncTypes(program) {
		if _, exists := funcTypes[k]; !exists {
			funcTypes[k] = t
		}
	}
	return runOptionCmpCore(program, funcTypes)
}

// ValidateStrOrderingTHIR 是 ValidateStrOrdering 的 THIR 侧变体：
// 回传型别表（funcTypes）改由 THIRView.FuncResultTypes（KFuncDef/KExtern
// 提取）优先、collectStrOrderingFuncTypes（AST 宣告 + std 预填）兜底；
// 走查（walkStrOrderingStmt / walkStrOrderingExpr，含 SurfaceMatch 糖
// case）与 varTypes 作用域跟踪、去重逻辑全不动。
func ValidateStrOrderingTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	if program == nil {
		return nil
	}
	funcTypes := make(map[string]string)
	for k, t := range view.FuncResultTypes() {
		funcTypes[k] = t
	}
	for k, t := range collectStrOrderingFuncTypes(program) {
		if _, exists := funcTypes[k]; !exists {
			funcTypes[k] = t
		}
	}
	return runStrOrderingCore(program, funcTypes)
}

// ValidateCharArithTHIR 是 ValidateCharArith 的 THIR 侧变体：判据全部来自
// THIRView.TypeOf 的表达式级型别表（无 AST 直读），两侧同传 view ⇒ 恒等对拍。
func ValidateCharArithTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return ValidateCharArith(program, view)
}

// runStrOrderingCore 是 ValidateStrOrdering 两变体共享的走查 + 去重尾巴。
func runStrOrderingCore(program *parser.Program, funcTypes map[string]string) []ValidateResult {
	varTypes := make(map[string]string)
	var results []ValidateResult
	for _, stmt := range program.Statements {
		walkStrOrderingStmt(program.Sem, stmt, funcTypes, varTypes, "", program.Sem.SourceFileOf(stmt), &results)
	}
	// 去重：一个 match 的区间臂 `['a'..'m')` 会被 desugar 成
	// `(s >= 'a') && (s < 'm')`，两个 InfixExpression 落在同一行同一列，
	// 不去重会对同一个臂报两条完全相同的错误。
	if len(results) < 2 {
		return results
	}
	seen := make(map[string]bool, len(results))
	deduped := results[:0]
	for _, r := range results {
		key := fmt.Sprintf("%d:%d:%s:%s", r.Line, r.Column, r.File, r.Message)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, r)
	}
	return deduped
}

// declTypeOf 是 collect 系（collectFuncDeclared / collectLetsInStmts /
// collectTopLevelLets / collectStrIndexTypes）登记点的统一取值出口：THIR 副表
// （TypeOf）非空时优先，否则回退 legacy 值（AST 直读）；view 为 nil 或 legacy
// 为空时直接回 legacy——旧版路径零行为变化。
//
// legacy 空的硬闸：无宣告（Type==nil → legacy ""）的登记点，TypeOf 若因
// TypeOverride 回值，会把「未知型別」翻成「已知型別」——对 lookupVarType
// 之类 ok-停止查找语义是行为变更（查到类型会放行告警）。故换源只发生在
// legacy 非空（有宣告）的登记值上；既有八处调用点 gate 下 legacy 恒非空，
// 此收紧对它们无影响。
//
// 值等价性（影子双跑全等的前提）：gate 梯子不变的前提下——params 仅
// NamedType、results/lets 任何型别——TypeOf 的 Inferred 路径（
// PopulateInferredTypes = TypeOverride > nodeType 反射读 .Type 字段）与
// AST 直读给出同一字串：NamedType.String() ≡ nt.Value，Parameter /
// LetStatement 的 .Type.String() ≡ 原 s.Type.String()。唯一例外是
// TypeOverride（装配期具现化/具象化的有效型别覆写）非空时 TypeOf 回覆写值，
// 这恰是 KLet 有效型别解析序（TypeOverride > Type > InferredType）的第一档，
// 比 AST 直读更贴近节点的真实有效型别；该分叉面由 shadow 语料把关。
func declTypeOf(view *THIRView, n parser.Node, legacy string) *ty.Type {
	if t := view.TypeRefOf(n); t != nil {
		return t
	}
	if legacy == "" {
		return nil
	}
	return thirPool.Parse(legacy)
}

// declaredIsOption reports whether a declared type is an option (`?T`). It is
// the structural replacement for a leading-`?` prefix test: the two are the same
// predicate, but Kind answers it even when the type arrived already parsed (no
// re-scan of the text).
func declaredIsOption(t *ty.Type) bool {
	if t == nil {
		return false
	}
	return t.Kind == ty.Option || t.Canonical().Kind == ty.Option
}

// declTypeOfRaw is the raw-spelling form of declTypeOf, used by the validator
// plumbing that still keeps its scope tables as map[string]string. It exists so
// that the structured entry point (declTypeOf) is the single place the
// THIR-vs-legacy precedence is decided; when those tables become
// map[string]*ty.Type the call sites switch to declTypeOf directly.
func declTypeOfRaw(view *THIRView, n parser.Node, legacy string) string {
	if t := declTypeOf(view, n, legacy); t != nil {
		return t.Raw()
	}
	return ""
}

// ValidateIntOverflowTHIR 是 ValidateIntOverflow 的 THIR 侧变体：主体走共享的
// runIntOverflowCore，宣告型別表（params/results/lets）的登记值经 declTypeOf
// 改读 THIR 副表。walkStmtForOverflow / operandIntKind / walkExprForIntOverflow
// 的 AST 走查与糖 case 不动；funcTypes 快照（snapshotValidationFuncTypes）与
// sem.FuncVarTypes gap-fill 也刻意保留 AST 来源——它们牵动
// registerInferredNonIntLet 的推断面，换源属行为变更，影子期禁。
func ValidateIntOverflowTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return runIntOverflowCore(program, view)
}

// ValidateInterfaceImplementationTHIR 是 ValidateInterfaceImplementation 的
// THIR 侧变体：主体走共享的 runInterfaceImplCore，接口方法与 impl 方法的
// param/result 签名字串经 declTypeOf 改读 THIR 副表。签名比对（receiver
// 替换 `m.Receiver → implType`）、self 首结果剔除、Implements 范围限定
// （declaredIfaces）全不动。接口方法 param/result 是 *Parameter 且经
// param() 进 idMap，TypeOf 可查；值等价性同 declTypeOf 注释（Parameter
// 的 .Type.String() ≡ 原值，TypeOverride 分叉面由 shadow 把关）。
func ValidateInterfaceImplementationTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return runInterfaceImplCore(program, view)
}

// ValidateLHSInferredBuiltinsTHIR 是 ValidateLHSInferredBuiltins 的 THIR 侧
// 变体：主体走共享的 runLHSInferredCore，param/result/let 登记值经 declTypeOf
// 改读 THIR 副表。硬闸（登记 gate、hasRealTypeAnnotation 的 AST 口径）保留，
// 见 runLHSInferredCore 注释——「副表有型别」不可当作 has-type，否则推断的
// `?T` 会吞掉诊断（行为变更，影子期禁）。
func ValidateLHSInferredBuiltinsTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return runLHSInferredCore(program, view)
}

// ValidateStrIndexComplexityTHIR 是 ValidateStrIndexComplexity 的 THIR 侧变体：
// 主体走共享的 runStrIndexCore，scopeTypes / funcReturns 的声明登记值经
// declTypeOf 改读 THIR 副表（活数据：直接判定 s[i] 是否为 O(n²) 告警）。
// exprTypeString 推断面、asciiVars 收集、std 跳过与作用域键全不动。
func ValidateStrIndexComplexityTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return runStrIndexCore(program, view)
}

// ValidateUnhandledOverflowTHIR 是 ValidateUnhandledOverflow 的 THIR 侧变体：
// 主体走共享的 runUnhandledOverflowCore，宣告型别登记值与 option 宣告检查
// 的字串来源经 declTypeOf 改读 THIR 副表。
func ValidateUnhandledOverflowTHIR(program *parser.Program, mainFile string, view *THIRView) []ValidateResult {
	return runUnhandledOverflowCore(program, mainFile, view)
}

// ValidateUnhandledIndexTHIR 是 ValidateUnhandledIndex 的 THIR 侧变体：主体走
// 共享的 runUnhandledIndexCore，option 宣告检查的字串来源经 declTypeOf。
func ValidateUnhandledIndexTHIR(program *parser.Program, mainFile string, view *THIRView) []ValidateResult {
	return runUnhandledIndexCore(program, mainFile, view)
}

// ValidateIndexWriteTHIR 是 ValidateIndexWrite 的 THIR 侧变体：纯 wrapper，
// 见 runIndexWriteCore 注释（零 AST 型别直读）。
func ValidateIndexWriteTHIR(program *parser.Program, mainFile string, view *THIRView) []ValidateResult {
	return runIndexWriteCore(program, mainFile, view)
}

// ValidateFuncArgsTHIR 是 ValidateFuncArgs 的 THIR 侧变体：主体走共享的
// runFuncArgsCore（sigs 改 FuncSigs 优先、登记值经 declTypeOf）。
func ValidateFuncArgsTHIR(program *parser.Program, rootDir string, view *THIRView) []ValidateResult {
	return runFuncArgsCore(program, rootDir, view)
}

// ValidateTypesTHIR 是 ValidateTypes 的 THIR 侧变体：主体走共享的 runTypesCore
// （funcTypes / 签名键 / topLevelVarTypes / selfType / validateStmtTypes 登记值
// 经 declTypeOf）。
func ValidateTypesTHIR(program *parser.Program, view *THIRView) []ValidateResult {
	return runTypesCore(program, view)
}

// ─────────────────────────────────────────────────────────────
// 影子双跑对拍
// ─────────────────────────────────────────────────────────────

// ShadowMismatch 描述一对旧版 / THIR 版诊断多重集的不一致。
type ShadowMismatch struct {
	Check  string
	Detail string
}

func (m ShadowMismatch) String() string { return m.Check + ": " + m.Detail }

// shadowDiagKey 归一 ValidateResult 为多重集键
// (File,Line,Column,TraceID,Message)——与计划 §2.D 对拍键一致。
func shadowDiagKey(r ValidateResult) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", r.File, r.Line, r.Column, r.TraceID, r.Message)
}

func shadowMultiset(rs []ValidateResult) map[string]int {
	m := make(map[string]int, len(rs))
	for _, r := range rs {
		m[shadowDiagKey(r)]++
	}
	return m
}

// ShadowCompareTHIR 在同一只程序上跑已迁移检查的旧版与 THIR 版，
// 返回诊断多重集的不一致清单（空 = 全等）。对拍键
// (File,Line,Column,TraceID,Message)。transpiler 的 H0 挂载点与
// thir_shadow_test 的语料门禁都走这一个入口。
func ShadowCompareTHIR(program *parser.Program, view *THIRView) []ShadowMismatch {
	if program == nil {
		return nil
	}
	var out []ShadowMismatch
	cmp := func(name string, old, cur []ValidateResult) {
		om, cm := shadowMultiset(old), shadowMultiset(cur)
		for k, c := range om {
			if cm[k] != c {
				out = append(out, ShadowMismatch{Check: name,
					Detail: fmt.Sprintf("old=%d new=%d diag=%q", c, cm[k], k)})
			}
		}
		for k, c := range cm {
			if om[k] != c {
				out = append(out, ShadowMismatch{Check: name,
					Detail: fmt.Sprintf("old=%d new=%d diag=%q", om[k], c, k)})
			}
		}
	}
	cmp("func-args", ValidateFuncArgCount(program), ValidateFuncArgCountTHIR(program, view))
	// div-by-zero：legacy 与 THIR 两侧同传 view（同步精化）⇒ 恒等对拍，验证两个
	// wrapper 调用同一核心、无分叉。行为变更（浮点被除数由报转不報）由
	// TestDivByZeroFloatRefine 等独立正向测试锁定。
	cmp("div-by-zero", ValidateDivByZero(program, view), ValidateDivByZeroTHIR(program, view))
	cmp("str-ordering", ValidateStrOrdering(program), ValidateStrOrderingTHIR(program, view))
	// char-arith：legacy 与 THIR 两侧同传 view（同步精化）⇒ 恒等对拍。
	cmp("char-arith", ValidateCharArith(program, view), ValidateCharArithTHIR(program, view))
	// string-concat：legacy 与 THIR 两侧同传 view（同步精化）⇒ 恒等对拍。行为
	// 变更（`str+str` 由漏报转补报）由 TestStringConcatRefine 等独立正向测试锁定。
	cmp("string-concat", ValidateStringConcat(program, view), ValidateStringConcatTHIR(program, view))
	cmp("option-comparison", ValidateOptionComparison(program), ValidateOptionComparisonTHIR(program, view))
	cmp("int-overflow", ValidateIntOverflow(program), ValidateIntOverflowTHIR(program, view))
	cmp("interface-impl", ValidateInterfaceImplementation(program), ValidateInterfaceImplementationTHIR(program, view))
	cmp("lhs-inferred-builtins", ValidateLHSInferredBuiltins(program), ValidateLHSInferredBuiltinsTHIR(program, view))
	cmp("str-index-complexity", ValidateStrIndexComplexity(program), ValidateStrIndexComplexityTHIR(program, view))
	// 三件套带 mainFile 参数：两侧同参（影子对拍只关心多重集等价，mainFile 用
	// 空串即双方一致；File 字段来源相同，键含 File 仍可比）。
	cmp("unhandled-overflow", ValidateUnhandledOverflow(program, ""), ValidateUnhandledOverflowTHIR(program, "", view))
	cmp("unhandled-index", ValidateUnhandledIndex(program, ""), ValidateUnhandledIndexTHIR(program, "", view))
	cmp("index-write", ValidateIndexWrite(program, ""), ValidateIndexWriteTHIR(program, "", view))
	// FuncArgs 与 FuncArgCount 同名不同规则（实参型别兼容），键用 func-args-compat。
	cmp("func-args-compat", ValidateFuncArgs(program, ""), ValidateFuncArgsTHIR(program, "", view))
	cmp("validate-types", ValidateTypes(program), ValidateTypesTHIR(program, view))
	return out
}
