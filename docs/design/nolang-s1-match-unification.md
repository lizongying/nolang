# S1 立项：match 表示统一到 SurfaceMatch（消除已降糖 IfExpression 表示）

> 状态：**立项（设计 + 实施计划）**——本文件是 §4.3 第 5 步经调查证伪后的正确归宿。
> 关联：`docs/design/nolang-parser-lowering-invariants.md` §4.3（步骤 1–4 已落地、第 5 步 mis-scope）、§1（P1/P2 不变量）、§5（base/sugar 裁定）。
> 行号基准：HEAD `583791e1`（§4.3 steps 1-4 已提交）。`src/parser/**` 与 `src/checker/**` 并行 session 常态在改，引用前请重新定位（本文给函数名 + 行号双锚）。

---

## 0. 缘起：§4.3 第 5 步为何不能做

§4.3.1 第 5 步原文把「删 12 痕迹字段 + RTFlag 位 + formatter 降糖代码 + hir.KUnwrapAssign」当成一次局部清理。实测证伪：

- `IfExpression` 上的 12 字段（`MatchedExpr`/`DotValBody`/`RangePattern`/`EqualityPattern`/`OptionPatterns`/`ValuePatterns`/`RawCond`/`MatchEndPos`/`IsTypeMatch`/`TypePattern`/`TypeBindingName`/`IsTypeDefault`，定义见 `parser/ast.go:1642-1681`）**不是「降糖 trace」**，而是 match 的**已降糖表示**（lowered representation）。它们在默认编译路径被消费：
  - `checker/match_exhaustive.go`（17× 读 `MatchedExpr`）、`option_compare.go`（5×）
  - `parser/tohir.go:906 case *IfExpression`（14×，读全部 12 字段造 `KTypeMatch`/`KTypeArm`/`KCond`）
  - `build/transpiler_stdload.go:159,675`、`transpiler_monomorph.go`（20× 读 12 字段）
- `hir.KUnwrapAssign` **非死 kind**：`parser/tohir.go:383` 仍在 `Kind: hir.KUnwrapAssign` 产出。
- `RTFlag`（`parser/resolver.go:112` 注释自称「fmt 往返專用」）实则被 `parser` 在降糖时写入、`build/transpiler_monomorph.go` 读取——非 formatter 专用。

默认编译入口 `cmd/no/main.go:3390`、`:3539` 调 `ParseProgram()` **不设 `SkipLowering`**，走已降糖路径。单纯删除这三者会直接破坏 `no build`/`no vet`/`no test`。详见 invariants 文档 §4.3.2 之后的调查记录。

**结论**：消除这三者 = 消除「match 的已降糖 `IfExpression` 表示」本身，是 invariants 文档定义的 **S1** 大重构，不是清理步骤。本文即 S1 的立项设计。

---

## 1. 当前架构事实（已实测，非推断）

### 1.1 节点定义
- `SurfaceMatch`（`parser/lowering.go:24`）：`{ Token, Matched Expression, Arms []MatchArm, RBracePos }`。**唯一的 match 表层节点**。
- `IfExpression`（`parser/ast.go:1637`）：承载 12 痕迹字段 + `Condition`/`Consequence`/`Alternative`（arm 链的 if/elif/else 形式）。

### 1.2 数据流（两种形态）
```
            parser 产出（两种形态都只产 SurfaceMatch）
                          │
            ┌─────────────┴─────────────┐
   默认路径（SkipLowering=false）   糖形态（SkipLowering=true，fmt/fix/LSP）
            │                              │
   lowerProgram（lowering.go）        lowerProgram 被跳过
   展开 SurfaceMatch →                SurfaceMatch 原样保留
   IfExpression 链（带 12 字段）            │
            │                              │
   checker 读 IfExpression            checker 读 SurfaceMatch
   tohir:906 case *IfExpression      tohir:1128 case *SurfaceMatch（调用 desugarSurfaceMatch）
   build/transpiler 读 12 字段        build/transpiler 读 SurfaceMatch
```

### 1.3 已具备的基础（S1 不是从零开始）
1. **Parser 已只产 SurfaceMatch**（`lowering.go:1-7`）——✅ 无需改。
2. **H0 侧 desugar 已实现且证等价**：
   - `parser/lowering.go:2079 desugarSurfaceMatch(sm *SurfaceMatch) Expression`
   - `parser/lowering.go:2404 buildMatchDesugar(sm *SurfaceMatch) Expression`
   - `parser/tohir.go:1128 case *SurfaceMatch` 调用之，注释明言「**every shipped path today 不可达**，因为 lowering.go 先展开了 SurfaceMatch；`parser/dump/h0_sugar_gap_test.go` 证明它与降糖路径产出**相同 HIR**」。→ S1 只需让此 case 可达。
3. **checker 已是双形态**：`checker/match_exhaustive.go:56-59` 的 `NonExhaustiveMatch` 同时持 `Root *IfExpression`（降糖）与 `Sugar *SurfaceMatch`（糖）；`:455` 起整段标注为 **「S1」** 糖形态收集器（`collectSugarMatchRoots`/`sugarMatchVariants`/`sugarMatchSubjectName`，实现在 `match_arm_reach.go`）。穷尽/可达性/arm reach 诊断在两种形态下共用语义助手。→ checker 侧 S1 **大体已就绪**。
4. **formatter / LSP / tools** 的糖形态路径已读 `SurfaceMatch`（`fmt/control.go:6`、`lsp/walker.go`、`tools/*`），无需改。

### 1.4 实质缺口 —— ✅ 已补齐（翻转后实测）

> **立项时的原文**：`build/transpiler` 不认识 `SurfaceMatch` —— `transpiler_stdload.go:159,675`
> 只有 `case *parser.IfExpression` 且读 `ex.MatchedExpr`/`ex.RawCond`/`ex.EqualityPattern`；
> `transpiler_monomorph.go` 同理（10× 读 12 字段）。transpiler 位于 checker 之后、H0 之前，
> 默认路径若保留 SurfaceMatch 则必须加 SurfaceMatch case，否则崩溃。

**现状：已补齐，且默认管线已真正走原生路径。**

- **翻转落地**：`parser/lowering.go:187` 的 `deferMatchDesugar: true` 无条件置位 ⇒
  `ParseProgram()` 默认路径**保留** `SurfaceMatch`，不再展成 if 链。展开改由显式
  `DesugarSurfaceMatches`（`lowering.go:202`）承担，**只由实验性 js/wasm 后端调用**
  （`builder.go:1171/1221/1327/1511`，四处注释均标注「主编译管线已原生消费 SurfaceMatch，不走此处」）。
- **transpiler 侧补的 SurfaceMatch case**（实测全在）：`transpiler_stdload.go:159,681`、
  `transpiler_monomorph.go:179(scanSurfaceMatchForGenericCalls)/227/514/870/1965/2181`、
  `transpiler_mangle.go:245`、`transpiler.go:2103`、`slice_structs.go:275`、
  `module_check.go:146`、`module_prefix.go:540,920`；统一经 `parser.WalkSurfaceMatchChildren`
  枚举 `Matched` + 各臂 body。
- **checker 侧原生声明臂绑定**：`checker/sugar_walk.go#matchArmBindings` 在 `TypeBindingName`
  之外补**枚举变体析构绑定**（从 `arm.Condition.(*parser.CallExpression)` 参数抽名，跳过
  ok/err/nil，型别经 `enumVariantPayloadOf`）。

### 1.4.1 翻转暴露的真实回归（已修）：跨模块类型解析污染

翻转后 `no build tests/type-match.no` 由 EXIT=0 变 EXIT=1（**A/B 基线 `ab157ea7` 实测确认是回归**，
不是预存问题）。根因链（全部实测，非推断）：

1. `SemanticContext.FuncVarType`（`parser/resolver.go:617-628`）在 per-function 表未命中时
   **回退到全局 `VarTypes`**，而该表**没有文件/模块作用域**。
2. std 模块由 blob 以 **surface 形态**载入（`SerializeStdAST` 不 desugar），翻转后其
   `SurfaceMatch` 改在 **H0** 降糖；而 blob 模块**没有 per-function 类型表**。
3. ⇒ 局部变量经全局回退解析到**别的模块的同名全局**。实测探针：
   `file=fs.no func="fs.file.read-str" subject=b` → `funcLocal=""(false)`、
   `global="num"(true)`（`num` 是 type-match.no 的 union，其模块级 `b` 的类型）。
4. ⇒ `isUnionType("num")` 为真，fs.no 的 **option match**（ok/nil/默认臂）被误判为
   **类型解构 match**，报 `type match arm pattern "err" is not a member of union "num"`。

**修法**：新增 `isOptionMatchArm` / `hasOptionMatchArm`（`lowering.go`），在两处类型 match
派发点（`lowerSurfaceMatch` 与 `desugarSurfaceMatch`）加**臂形前置守卫**——含 option 臂
（ok/nil/err/`IsDotVal`）的 match 语义上绝不可能是静态类型派发 match。选臂形而非去修
`VarTypes` 作用域，是因为**臂形与 subject 类型解析无关，是可靠判据**，且无需动全局类型表。

**另一处失败非回归**：`tests/err-binding.no` 报 `i64-to-str: no result slot`，
基线与当前**同为 EXIT=1**，属预存 MIR codegen 限制（与 `tests/std-new.no` 同类）。

### 1.4.2 翻转收尾暴露的另三类回归（已修）+ 两个既有缺陷（2026-10-18）

以下全部经 `/tmp/vB/bin/no`（基线 `ab157ea7` 重编）A/B 确认；既有缺陷一项在基线上同样坏，
明确记录以免后人重复追逐。

**(a) checker：`ok ->` 值臂不登记 `it` ⇒ 泄漏值被推断成 `str`**（`tests/heap.no` 构建+运行回归）

- SurfaceMatch 分支的 varTypes 是**扁平单表**：`walkSurfaceMatchChildren` /
  `validateStmtTypes` 按臂把绑定写进**同一张** varTypes 且**不恢复**。err 臂注册
  `it="err"`，而 LetStatement 路径会把 `"err"` 正规化成 `"str"`
  （`checker.go:8796-8805`）。
- `ok ->` 臂的解析形状：parser（`expr.go:1100-1102/1046-1050/1107-1113`）只设
  `IsWildcard=true; IsDotVal=true` 且 **Condition 留空** ⇒ `matchArmBindings` 两个
  switch 分支都不进 ⇒ 不注册 it。
- **修法**：`matchArmBindings` 新增 `arm.IsWildcard && arm.IsDotVal &&
  arm.BindingName == ""` 分支，登记 `it: elemTypeOf(matchedType)`。
  `BindingName == ""` 条件保证 `ok(v) ->` 只登记 v（与降糖路径 `bindName` 改名一致）。

**(b) checker：裸型别臂报 `'i32' is not defined`**（`tests/type-match.no`）

- `i64 ->` 在语法期只是 `Condition = Identifier{"i64"}`；`IsTypeArm` 只在
  `v T ->`（`expr.go:1152-1158`）设置，型别臂认定延后到 `buildTypeMatchDesugar`
  （`lowering.go:2335-2336`）。未定义变量豁免表因此不认裸型别臂的型别名——
  `i64` 因 std 点号导出短名巧合通过，`i32` 暴露。
- **修法**：`checkUndefinedVarsInExpr` 的 Identifier 分支在 val/err/ok/nil/it 豁免之后
  放行 `isBuiltinType(e.Value)` / `char` / `validationStructNames`（union 别名已由
  `CollectDefinedVars:1856-1858` 收进 definedVars）。
- **测试**：`src/checker/s1_sugar_match_binding_test.go` 5 个——
  `validateBothShapes` 同时以糖/降糖两形态跑 `ValidateTypes` + `ValidateUndefinedVars`
  并比较诊断集合；含反空洞用例（`TestSugarShapeUndefinedVarStillReported`）。

**(c) D1：单态化克隆上型别 match 派发失效**（tm1/tm2/tm3 最小复现）

- `monomorphizeUnions`（`transpiler.go:1654`）在 **ASTToHIR 之前**跑，
  `cloneUnionVariant` 把 `x: num` 改成 `x: i64` 并命名 `f__i64`；克隆的
  `SemanticContext.FuncVarTypes` **没有以克隆名为键的条目**（实测
  `FuncVarType NOT FOUND curFunc="classify__i64"`）。
- ⇒ `desugarSurfaceMatch` 原只靠「subject 型别是 union」派发，克隆上永不成立
  ⇒ 型别臂被误降糖成 `x == i64` 执行期比较（tm1 编译错 `unknown callee v.to-str`；
  tm2 构建成功但运行 SIGTRAP 133）。
- **修法**：`isUnionMemberName` + `armsCarryTypePatterns` 臂形派发（两处派发点同时改），
  未知主体型别跳过成员校验——`flattenUnion("")` 的叶子回退返回 `[""]`，会被当成成员集，
  触发伪错误 `not a member of union ""`。

**(d) MIR 实例化：型别臂绑定晚于 transpiler ⇒ 呼叫不存在的 `@i32_to_str`**
（`tests/type-match.no` 运行期 SIGSEGV，`classify(bi32)` 打印后崩溃）

- H0 才合成绑定（`let v = x` 窄化为 i32），晚于 `resolveMethodCall`
  （`transpiler_monomorph.go:1157`，以 `varTypes[recv]` 为入口，缺失即早退）
  ⇒ int 族泛型方法未实例化，codegen 把 `v.to-str` 摊成**不存在**的 `@i32_to_str`。
  基线 IR 为 `call void @int_int_to_str__i64(...)`。
- **修法**：`scanSurfaceMatchForGenericCalls` 在**臂作用域**内按臂元数据登记窄化型别
  （新增 `surfaceMatchIsOptionArm` / `surfaceMatchIsTypeDispatch` /
  `surfaceMatchArmTypeBinding` / `isUnionMemberNameIn`，mirror parser 侧同名判据）；
  模板扫描的就地改写随后被 union 克隆继承。修复后 IR 与基线逐字节一致，
  `tests/type-match.no` 输出 `10/3/2.5/7/3/2.5/8/5/5.5`、EXIT=0。

**(e) transpiler：option match 降糖三分支致臂内声明泄漏**（`tests/match-index-option.no` 运行期回归）

- 降糖 option match 是**三分支** `IfExpression`：`Consequence` / `Alternative` /
  **`DotValBody`**（dumper 不打印该字段，肉眼审查 AST 易看漏）。ok 臂落
  `DotValBody` 且**最后处理**，其 `let p = it` 不可推断 ⇒ p 从表里删掉，
  等效「match 之后没有 p」。
- 糖形态按**源序**收集（ok 臂先、默认臂后）⇒ 默认臂 `p = ''` 推断出 `str`
  **泄漏**到 match 之后并胜出；`parts[i]: { ok -> { p = it } -> { p = '' } }`
  之后的 `p.to-str()` 被摊平成不存在的 `@str_to_str` ⇒
  `unknown callee str.to-str in func t-in-bounds`。
- 最小复现：`s txt; parts = s.split('-'); parts[0]: { ok -> … }`——字面量数组
  两边同坏，`split` 结果才暴露（泄漏的型别来自默认臂，不是 ok 臂）。
- **修法**：`collectVarTypesFromExpr` 的 `case *parser.SurfaceMatch` 改
  **每臂独立拷贝** varTypes 再收集（`collectItScoped` 继续只保护 `it`），
  臂内 let 的推断型别不得泄漏到 match 之后。修复后糖/降糖两形态收集结果一致，
  `match-index-option.no` 输出 `ALL TESTS PASSED`。
- ⚠️ 该坑**两种形态都有**（糖形态泄漏胜出、降糖形态删键），不是翻转引入的
  新形态问题——只是翻转让默认路径改走另一条收集路线从而暴露。

**两个既有缺陷（基线同坏，非本轮回归，记录在案不予追逐）**：

- **tm4**：单态化函数内 option match + `it.to-str()` 输出为空（基线同样为空）。
- **臂内插值 `{v.to-str()}`**：`i64-to-str: no result slot`（`tests/err-binding.no`
  同类预存 MIR codegen 限制）。

**P4/P5 fixture 补强与钉住门禁**（`tests/match-raw-cond.no`、`tests/match-type-arm.no`、
`tests/minimal-ok.no`、`tests/type-match.no`）：

- P5 fixture 被迫重设计为 `mixed = num | bool`，暴露出两个**既有限制**（基线同症）：
  1. union 成员若是聚合型别（str/vec/struct），单态化克隆参数虽换成具体成员，
     呼叫点仍以标量通路传参 ⇒ MIR codegen 报
     `parameter 'mixed' expects a scalar (LLVM i64) but the argument has aggregate
     type 'str'`；
  2. `checker.ValidateUnionTypes` 的 `findSingleUnionName` 只在「整个函数只使用同一个
     union 别名」时才标 `GenericUnion` ⇒ 单态化只复制那种函数；带两个不同 union 参数的
     函数不被复制，模板直接被呼叫，union 参数退化成标量、派发全落默认臂
     （实测：六次驱动全部输出 `other`）。
- 钉住测试 `TestS1RawCondFixturesAreExercised` / `TestS1TypeArmFixturesAreExercised`
  （`parser/dump/hir_subform_coverage_test.go`）。P5 断言刻意排除**嵌套** match 的臂——
  破坏实验实测：未排除时删掉外层 `v bool ->` 绑定臂仍 PASS（内层 `u i64 ->` 遮住了计数）。
  三个破坏面（外层绑定臂改值臂 / 裸臂全删 / `ok(cond)` 改裸条件）均实测精确失败。

**`SkipItBinding` 死码实测**：糖形态普查 535 程序 / 1818 臂 / **0 个** `SkipItBinding` 臂；
`TestS1SubformsAreCovered` 加「非零即失败」断言防腐烂（8 处合成点都在同址被
`buildMatchDesugar` 立即消费，SurfaceMatch 从不存活）。

**AST 磁盘缓存投毒方法论**：`UserASTCache` 的 key = 源码哈希 + ASTVersion + salt，
**不含路径、不含二进制版本** ⇒ 任何改变 `ParseProgram` 产出的改动都必须 bump
`ASTVersion`（本轮 `2026-10-16.4` → `2026-10-18.1`）；A/B 两边都要
`NOLANG_AST_CACHE_OFF=1`，否则一侧写的缓存会污染另一侧。

### 1.4.3 全量 corpus A/B：翻转本身导致 std 模块 9 处运行期回归（已解决，2026-10-18 收尾）

535 文件 × 双二进制（工作区 `bin/no` vs 基线 `ab157ea7` 重编 `/tmp/vB/bin/no`，
`NOLANG_AST_CACHE_OFF=1`，6 路并行）实测 **10 处差异**（run-2，已含 §1.4.2 (e) 修复；
run-1 为 11 处，多出的 `match-index-option.no` 已被 (e) 修复，复跑后**不再出现在差异表里**）：

| 文件 | 性质 | 症状 |
|---|---|---|
| json.no、json-get-clone.no、json-heap-cap.no、json-set-key-self.no、json-crosspool-mutate.no、mem-safety/json-nested-match.no、mem-safety/json-parse-option.no（7 个） | **回归** | 键内容损坏成 NUL/空格（如 `stringify={"\x00..."}`） |
| set.no | **回归** | `intersection FAIL` / `difference FAIL` |
| option-struct-payload-drop.no | **回归** | 工作树 `1999/1999` vs 基线 `1999/Alice` |
| test-div-mod-option.no | **改进** | 工作树正确打印 `5`/`0`，基线误打印 `<int.int>` |

另有 5 个 TIMEOUT 两边一致（既存挂起，非本轮引入）。

**归因实验（决定性）**：临时把 `deferMatchDesugar` 改回 `false`，其余不动，编出
对照二进制 `/tmp/no_noflip`（与工作区**只差这一个旗标**）⇒ json / set /
option-struct-payload-drop 及全部 6 个 json 家族测试**全部通过**。
⇒ 这 9 处回归是**翻转本身**造成的，与 §1.4.1/§1.4.2 各项窄修复无关
（checker 修复只影响诊断；D1 与 (d) 只影响型别派发 match）。

> ⚠️ **早期归因（已推翻）**：曾假设根因是「std blob 的 `FuncVarType` 在 H0 派发查表点
> 未命中、回退无作用域的全局 `VarTypes` 选错型别」（`lowering.go:2122` 附近）。对应修法
> 取向 1「`FuncVarType` 未命中时从 AST 推导 subject 型别」(`astLocalVarType`) 已落地为防御性
> 回退，但**对这 9 处无效**——探针证明 `buildMatchDesugar` 实际已正确解析出 `?byte/?i64/?num/
> ?u32` 等型别，故 subject 型别查表不是阻塞点。实证链：① `CachedProgram` 无自定义 `GobEncode/GobDecode`
> （走 gob 反射），`Program.Sem` 导出、其 `FuncVarTypes` 导出 map 必然被序列化；
> `TestCachedProgramEncodingIsDeterministic`（`astcache_det_test.go`）证同形 map 可回程；
> ② `SemanticContext.Merge`（`resolver.go:269-280`）携带 `FuncVarTypes`；两条导入路径
> （`transpiler.go:1156` 显式、`transpiler_stdload.go:365` 自动）都
> `merged.Sem.Merge(modProg.Sem)`；③ `desugarSurfaceMatch`（`lowering.go:2122`）确实查
> `sem.FuncVarType(curFunc, subject)`。
> ④ **定向探针 `TestProbeStdBlobFuncVarTypes`（当前烘焙 blob）**：`FuncVarTypes` **确被携带**
> （`json-pool.get-key` 4 项、`set.difference` 6 项…），但含 match 的 std 函数条目**空或残缺**——
> `json.stringify` 仅 `out:str`（输出参数）、`fs.file.read-str` **整个 absent**。
> ⇒ std 函数 match 相关的局部变量型别在**烘焙期根本没填入**（因 match 降糖随翻转延后到 H0），
> H0 那次定派发的查表未命中、回退全局 `VarTypes` ⇒ 选错型别。方案 b「带 side-table 进 blob」
> 因此**是伪前提**，真实缺口是「H0 查表缺稳健回退」。

**真实根因（2026-10-18 探针实证的决定性结论）**：回归形态是 **safe-index match**
（`arr[i]: { ok -> … }`），正是 json/set 家族对 `vec` / `[]T` 下标的安全索引。这类 match 的
降糖依赖 `idxLocalTypes`——「安全索引专用」的**函数作用域本地容器型别表**，由
`lowerer.collectLocalTypes` 填充，供 `isSafeIndexBase` / `inferIndexElemType` 识别
`arr[i]` / `vec[i]` 基底、判定能否走安全降级（`#{index-out}` / bounds 豁免）。
默认（no-flip）路径靠 `lowerProgram` 的 walk 跑 `collectLocalTypes` 填好该表；**翻转后 H0
走 `desugarSurfaceMatch` 跳过 `lowerProgram`，从不调用 `collectLocalTypes`**，于是 blob / AST
缓存载入的 std 模块 `Sem.IdxLocalTypes` 为空 ⇒ safe-index match 误降糖 ⇒ json 键内容损坏成
NUL、set 的 intersection/difference 错、option-struct-payload-drop 取值错。

**修复（已落地，9/9 通过，符合 Lowering 唯一性）**：match 降糖读 `idxLocalTypes`（safe-index 专用
容器型别表）的收集必须只有**一处**。`precomputeIdxLocalTypes`（`parser/lowering.go:739`）是唯一的、
只读 AST 的收集器，同时被 `prepareInBoundsMetadata`/`lowerProgram` 复用。H0 走 `desugarSurfaceMatch`
跳过 `lowerProgram` 的 walk，故在 H0 的唯一入口 `ASTToHIRWithMap`（`tohir.go:52`）进入降糖循环前，
对整程序调一次 `(&Parser{sem, prog, idxLocalTypes}).precomputeIdxLocalTypes(prog)`，把
`Sem.IdxLocalTypes` 一次填好并共享给 `scratchParser`（scratchParser 仅拷贝该表、不再重复收集）。
如此 H0 与（仍保留的）no-flip 路径读到**同一张表**，不再于 `scratchParser` 内第二处跑
`collectLocalTypes`（那违反 Lowering 唯一性）。`precomputeIdxLocalTypes` 幂等（只补缺、其余覆盖值
相同），无行为回归；「按函数守卫」在此也不再需要——单点遍历整程序已覆盖所有函数条目。

**验证**：
- 9 个回归文件 ×（工作区 `bin/no` vs `/tmp/no_noflip`）`no run` stdout + exit 逐字节一致 → 全部 PASS（本次单点版复跑确认 9/9）。
- `make test` 全绿（`./build/... ./mir/... ./checker/... ./fmt/... ./parser/... ./tools/...` 等 15 个包，无 FAIL）。
- 535 文件 corpus A/B（`NOLANG_AST_CACHE_OFF=1`，带 per-file 看门狗）仅剩 `test-div-mod-option.no` 1 处既有改进、0 新增差异（单点版与按函数惰性版对 `IdxLocalTypes` 填充行为等价，故该结论对单点版同样成立）。

**附注**：取向 1 的 `astLocalVarType` 防御性回退仍保留（subject 型别查表 `FuncVarType` 未命中时
从 AST 推导），作为独立防线；它覆盖的是型别派发 match 的 subject 解析，与本次 safe-index
表缺失是两条独立链路——两者互补，互不替代。

### 1.4.4 全量 A/B 再发现：union 单态化克隆体类型解构 match 误报（已解决，2026-10-18 收尾）

**现象**：在 §1.4.3 的 `IdxLocalTypes` 单点修复落地后，补跑 535 文件 A/B（flip `bin/no` vs no-flip `/tmp/no_noflip`，逐字节比对 `no run` 的 stdout+exit）仍剩 8 处差异：

| 文件 | 性质 | 症状 |
|---|---|---|
| `type-match.no` | **回归** | flip 报 `type match arm pattern "i64" is not a member of union "i32"` 等，EXIT=1；noflip 正确打印数字 |
| `match-type-arm.no` | **回归** | flip 报 `type match on union "mixed" requires type patterns` 等，EXIT=1；noflip 正确打印 `bool+int64/bare-i64/bare-i32/bare-f64` |
| `tagged-enum-alias.no` / `tagged-enum-cross-fn.no` / `tagged-enum-zero-match.no` / `tagged-enum-payload-ownership.no` | 既存失败（非翻转回归） | 两边均 EXIT=1，同一 IR 行 `%lv438' defined with type '%tenum_e_res...' but expected 'i64'` |
| `std-unix-fs-os.no` / `std-unix-fs-os-2.no` | cosmetic | 两边均 EXIT=0，仅 `-Woverride-module` 警告位置差 |

**根因**：含「类型解构 match」（`x num: { i32 -> ; i64 -> ; -> }`）且其 subject 是 union 的函数，在 H0 之前会被 `build/transpiler_monomorph.go` 的 `cloneUnionVariant` 按 union 的**叶成员**克隆（如 `clamp0` → `clamp0__i32/__i64/__f64`；`nested` → `nested__i32/__i64/__f64/__bool`）。克隆体的 subject 静态型别是**具体成员**（i32/i64/f64/bool），而非原 union（num/mixed）。

- 翻转前（`deferMatchDesugar:false`）：原函数在克隆前就 desugar，克隆体继承已展开的 if 链，subject 型别问题不存在。
- 翻转后（H0 `desugarSurfaceMatch` 在克隆之后各自 desugar）：`buildTypeMatchDesugar` 以具体成员型别（如 `i32`）作 subject 调 `flattenUnion("i32")` → 递归展开自身为 `["i32"]`，于是 `i64`/`f64`/`bool` 臂被校验成「不属于 union i32」而误报。
- `match-type-arm.no` 暴露第二个缺陷：`mixed = num | bool`，外层 `nested` match 的臂用到 `bool`/`i64`/`i32`/`f64`。旧 `recoverUnionFromMember` 只比对 union **直接成员**，于是 `i64`/`i32`/`f64` 克隆体把 subject 恢复成 `num`（直接成员含这三个）而非 `mixed`（直接成员是 `num`/`bool`，不含叶成员），漏掉 `bool` 臂 ⇒ 误报。而 `bool-str` 的 `b bool: { true -> ... }` 是**值 match**，subject `b` 是 `bool` 标量、`bool` 恰为 `mixed` 的成员，旧恢复逻辑把它错恢复成 `mixed` 并路由进 `buildTypeMatchDesugar`，撞上值臂 `true ->` 报 `type match on union "mixed" requires type patterns`。

**修复（已落地）**：
1. `parser/match.go` 新增 `recoverUnionFromMember(name, armPatterns)`：仅当 `name` 是已知 union 成员且非 union 时才恢复；用 `flattenUnion` 的**递归展开**成员集判定候选 union（故 `mixed` 经 `num` 含 `i32`），优先选「展开成员集覆盖全部臂模式」的 union（最具体者优先），落回「覆盖最多臂模式」者。这把 `nested__i32` 的 subject 正确恢复成 `mixed`（覆盖 {bool,i64,i32,f64}）而非 `num`（仅覆盖 {i64,i32,f64}）。
2. `parser/lowering.go` `desugarSurfaceMatch`：仅在 `armsCarryTypePatterns(clone.Arms)` 为真（确为类型解构 match）时才做 union 恢复并路由 `buildTypeMatchDesugar`；否则（值 match，如 `b bool: { true -> }`）跳过恢复、回落 `buildMatchDesugar`——与 no-flip 行为一致（`bool` 标量永不视为 union）。

**验证**：
- `type-match.no` / `match-type-arm.no` 的 flip `no run` 由 EXIT=1 变 EXIT=0，stdout 与 no-flip 逐字节一致（前者 `10/3/2.5/7/3/2.5/8/5/5.5`；后者 `bool+int64/bare-i64/bare-i32/bare-f64`）。
- `make test` 全绿（15 包无 FAIL）。
- 受影响文件集 A/B（8 处既有差异文件 + 其余含类型解构 match 的文件，共 12 个）：`total=12 same=12 diff=0`。2 处真实回归已修，4 个 `tagged-enum-*` 与 2 个 `std-unix-fs-os*` 维持良性（与翻转无关）。
- 扩到 **13 个**（新增 `opt-box-drop.no`，其含类型臂 `vec ->`/`option ->`）重跑：`total=13 same=13 diff=0`，结论不变。
- **全量 535 文件 `no build --js` 内容级 A/B（2026-10-18 收尾实测）**：受 `no run` 全量太慢（含 link+exec，约 45 分钟、会话内常被后台超时打断）限制，改用 `no build --js`（绕过 LLVM 链接，单文件 ~4s，前端+HIR lowering 照常执行）做全量。先验证 `--js` 产物**不含版本戳**（trivial 样例 flip/noflip 产物逐字节一致 ⇒ 不会系统性误报），再把比较从「仅比 rc」升级为「rc=0 比 emitted JS 内容 / rc≠0 比错误文本」的逐字节比对。切片 `FILE_OFFSET/FILE_LIMIT` 跑完 535：`total=535 same=535 diff=0`。**含义**：翻转只改动 match 降糖时机（逻辑同一），除 13 文件集外的 522 文件 `SurfaceMatch` 在 H0 与 parse-time 展开的 HIR 逐字节相同 ⇒ 前端/HIR 零回归。**注意**：`--js` 后端对部分文件（如 4 个 `tagged-enum-*`、若干 std 特性）自身会报错，这类文件在两端都失败 ⇒ 计为 SAME，故 `--js` 全量只能证「前端/HIR 等价」，**不能证 LLVM 后端等价**。
- **全量 535 文件 `no run` LLVM 端到端 A/B（2026-10-18 收尾实测，最终结论）**：切片 `FILE_OFFSET/FILE_LIMIT` 三段（200/200/135）后台跑完 535，`no run`（含 link+exec，真实程序 rc+stdout+stderr 逐字节比对）：`total=535 same=535 diff=0`。其中 531 个 flip/noflip 输出逐字节一致；4 个 `tagged-enum-*`（alias/cross-fn/payload-ownership/zero-match）两端均 `EXIT=1`，根因同为既存的 tagged-enum LLVM IR 代码生成 bug（`'%lvNNN' defined with type '%tenum_e_res = type { i64, [3 x i64] }' but expected 'i64'`、`icmp eq i64 %ot, %lv`）。首轮脚本因错误文本含随机临时目录路径（`/var/folders/.../T/nolang<RANDOM>/`）把它们计为 DIFF；归一化该路径后 4 个**全部 SAME**（rc 与错误文本除随机路径外逐字节相同）。⇒ **翻转在整個 535 文件 test corpus 上零回归**：既触及的 `desugarSurfaceMatch` 类型解构路径零回归，非类型臂文件 LLVM 产物亦因 HIR 已证等价、后端未改而必然一致。
- **13 文件集 `no run` A/B 作为交叉验证**：13 文件含全部 7 个含类型臂文件 + 6 个既有差异文件，`total=13 same=13 diff=0`（与全量结论一致）。
- **`cmd/no` 单测门禁（`make test` 未覆盖）**：该包含 S4 翻形态门禁（`src/cmd/no/no_fix_shape_gate_test.go`，覆盖 overflow/unwrap/redundant/match/main 五个 fixer），直接消费本次改动的 parser lowering。`make test` 不含此包，须按 project 约定注入 key 单独跑：`cd src && KEY=$(go run ./checker/genstdsig -key-only | tail -1) && go test -ldflags "-X github.com/lizongying/nolang/checker.StdSigKey=$KEY" ./cmd/no/` ⇒ **`ok ... 9.881s`**（通过）。
- **`no fmt` round-trip 冪等门禁（532/535 语料）**：糖形态/SurfaceMatch 路径由 formatter 直读，须确认翻转未改变格式化行为。只读方式（**绝不用 `-w`**、范围仅限 `tests/`、不动 `src/std`）跑全量 535：① flip 与 noflip 的 `no fmt` 输出逐字节比对 ⇒ **`fmt_identical=535 fmt_diff=0`**；② 冪等性 `fmt(fmt(x)) == fmt(x)`（经 stdin 二次格式化）⇒ **`ok=535 broken=0`**。⇒ formatter 在翻转下行为完全不变。
- ⚠️ **构建方法坑（本次收尾差点误判回归）**：`bin/no` **必须**经 `make no` 构建——它先 `make gen` 再从 `src/std` 烘焙签名表并注入 `checker.StdSigKey`。若用 `cd src && go build` 直编，`StdSigKey` 为空，运行期 `CollectStdModuleSignatures` 直接硬失败报「標準庫簽名 key 未注入」，导致**所有用 std 的文件** flip_rc=1 / noflip_rc=0 假阳性（本次一度误报 9 DIFF）。正确二进制前提：构建日志须出现 `-X ...checker.StdSigKey=...`。凡 A/B 前先 `make no` 并验证 `bin/no run tests/type-match.no` 能出数。

---

## 2. 目标架构

1. **Parser 不变**：继续只产 `SurfaceMatch`。
2. **`lowerProgram` 不再展开 `SurfaceMatch`**（其余糖节点 `UnwrapAssign`/`GroupedExpression` 仍可展开，或随 S1 一并移到 H0——见 §4 风险）。`SurfaceMatch` 作为 match 的**唯一表示**贯穿 `ResolveProgram → checker → 装配(transpiler) → H0(tohir)`。
3. **checker 收敛到 SurfaceMatch**：双形态 helper 保留（仍被糖形态/LSP 用），但默认路径不再产生 `IfExpression` match，故 `IfExpression` 分支自然成为死代码（§5 删除）。
4. **`tohir` 的 `case *SurfaceMatch`（:1128）成为可达主路径**，经 `desugarSurfaceMatch` 流式产出 base HIR——与 P2「H0 消费糖节点」一致。
5. **`build/transpiler` 加 `SurfaceMatch` case**，mirror 现有 `IfExpression` 处理逻辑（monomorph 化 arm、std 加载遍历 arm body 等）。
6. **删除死代码**（§5 清单）：已降糖 `IfExpression` match 处理 + 12 字段 + `RTFlag` 位 + `KUnwrapAssign` + formatter 降糖代码 + `lowering.go` 内联的 SurfaceMatch 展开（即 `lowerProgram` 里调用 `buildMatchDesugar` 的那段；`desugarSurfaceMatch` 保留给 H0 用）。

**不变量校验**：P1（parser 忠实，产出 `SurfaceMatch` 节点，不写 12 字段——字段本就定义在 `IfExpression` 上、随其删除消失）、P2（H0 是唯一 AST→HIR pass，消费 `SurfaceMatch`）。

---

## 3. 实施阶段（按 match 子形态分步，每步可独立验证 + 回退）

> 每阶段目标：把该类 arm 的「in-lowering 展开」改为「H0 `desugarSurfaceMatch` 处理」，并让 `build/transpiler` 对该类 `SurfaceMatch` arm 有对应 case；验证 HIR 逐字节等价（`h0_sugar_gap_test.go`）后，该类不再产生 `IfExpression` match。

- **P1 — 值/通配 match**：`x: { 1 -> a; 2 -> b; -> c }`。`EqualityPattern`/`ValuePatterns`/`wildcard` arm。最简单，先打通端到端管线（默认路径保留 SurfaceMatch → checker → transpiler SurfaceMatch case → H0 desugarSurfaceMatch → 等价 HIR）。
- **P2 — option match**：`x: { ok -> v; nil -> a; err -> e }`。`OptionPatterns`/`DotValBody`（ok/err 绑定与 `.->` val 分支）。
- **P3 — range match**：`x: { [a..b) -> ... }`。`RangePattern`。
- **P4 — raw-cond / 安全索引 match**：`ok(cond)`、`#{index-out}` 合成 match。`RawCond` + 索引主体归一化。
- **P5 — 类型解构 match（union 静态单态派发）**：`v i32 -> ...`。`IsTypeMatch`/`TypePattern`/`TypeBindingName`/`IsTypeDefault`。tohir:907-942 已有 `IsTypeMatch` 分支，确认其从 `SurfaceMatch` arm 读取等效信息。
- **P6 — 嵌套 / 独立 guard / 多臂链**：independent-guard（`MatchEndPos` 角色）、`wrapperArms` 链（fmt/control.go:224-257）。确认 `desugarSurfaceMatch` 对嵌套有底向上处理。
- **P7 — 收尾删除**：§5 全部死代码删除；`IfExpression` 12 字段定义移除；`RTFlag` 位移除；`KUnwrapAssign` 移除（`hir/golden_test.go:322` 豁免随之消失）；formatter 降糖代码（`formatBareMatchExpressionSubj` 的 `RTMatchWrapper`/索引主体归一化反推、`writeBareMatchArm` 合成 let 打捞）移除，formatter 改读 `SurfaceMatch` 字段。

每阶段门禁：
- `go test` parser/checker/fmt/lsp/tools/cmd/no（注入 `StdSigKey`）全绿；
- `h0_sugar_gap_test.go` 对受影响子形态仍 PASS（HIR 等价）；
- 重建 `bin/no` 并 `no build tests/str.no` 等 EXIT=0；
- `no fmt` round-trip 冪等（532 语料）不回归。

### 3.1 逐子形态覆盖普查（2026-10-18 实测）

翻转是一次性落在**所有**子形态上的，所以「P1–P6 分阶段实施」实际变成「分阶段**验证覆盖**」。
新增 `parser/dump/hir_subform_coverage_test.go`：把语料里每个 `SurfaceMatch` 按 `MatchArm`
导出字段分类，要求 §3 的六种子形态**各有 ≥1 个 fixture**，缺哪种就点名哪种。

实测覆盖（糖形态全语料）：

| 子形态 | 证据文件（抽样） |
|---|---|
| P1 值/通配 | `arch.no`、`arr-at-minimal.no`、`arr-param.no` |
| P2 option | `arr-at-minimal.no`、`arr-test.no`、`err-binding.no` |
| P3 range 模式 | `fmt-heuristic.no`（`[0..60)/[60..80)/[80..90)`）、`str-order.no`（`[1..10)`、`["a".."z")`）、`match-range-pattern.no`（新增补强） |
| P4 raw-cond/索引合成 | `minimal-ok.no`（`ok(it > 127)`） |
| P5 型别解构 | `type-match.no`（`v i64 ->`） |
| P6 巢状/独立 guard | `database-sql.no`、`err-binding.no`、`ffi-mysql.no` |

⚠️ **纠错记录（方法论教训）**：普查过程中我曾据 `grep -E '^\s*\[.*\)\s*->'` 判定
「P3 无 fixture」并据此宣称发现空洞。**该 grep 是错的**——macOS BSD grep 的 ERE
**不支持 `\s`**，所以它什么都没匹配到。结构普查自己纠正了这个误判。
⇒ 结论：**本项目的「语料里有没有 X」一律用结构普查回答，不要用 shell grep**
（这已是第二次 grep 误判；上一次是把 SurfaceMatch 当 removedByParser）。

**第 3 次复发（2026-10-18，提交当日，教训更重）**：Bash `grep` 对 `lowering.go` /
`tohir.go` 再次**返回假空**，据此我先后得出两个**错误**结论——
①「`buildMatchDesugar` / `buildTypeMatchDesugar` 已不存在」（实际存在且被 8+ 处调用，
其中一处正是本次 union 别名修复点 `lowering.go:2367`）；
②「`tohir.go` 已不再读 12 字段」（实际 `tohir.go:928 case *IfExpression` 起仍在读）。
若据此执行 §5 的 P7 删除，会**直接打断流水线**。改用 Grep 工具后全部纠正（见 §5.1）。

⇒ 补充铁律：**Bash `grep` 返回「空」不等于「不存在」**。凡「某个符号/字段是否还在」的
**否定结论**，必须用 Grep 工具（或 awk/python）复核，且最好再找一个**肯定锚点**（如已知存在的
函数名）同行验证，确认 grep 本身在本文件上是工作的。

---

## 4. 风险与决策点

1. **`lowerProgram` 是否「只跳过 SurfaceMatch」还是「全跳过、所有糖移到 H0」**：
   - 最小风险 = 只让 `lowerProgram` 跳过 `SurfaceMatch` 展开，其余糖节点（`UnwrapAssign`/`GroupedExpression`）维持现状。这样 transpiler/checker 的其它糖节点处理不动。
   - 但 `UnwrapAssignStatement` 的 `?=` 合成 match（lowering.go:500 注释：「`?=` / 安全索引的合成 match 只存在于 lowering」）须在 SurfaceMatch 保留路径下仍能构造——需确认 `desugarSurfaceMatch` 或 H0 能补上这些合成。这是 P2/P4 的关键调研项。
   - **决策**：先走「只跳过 SurfaceMatch」最小方案；若 `?=`/索引合成 match 在 SurfaceMatch 路径缺失，则在 P2/P4 内于 H0 侧补合成（不动其它糖节点）。
2. **`build/transpiler` 加 SurfaceMatch case 的工作量**：transpiler 的 match 处理是「遍历 arm body 做 monomorph/std 加载」。最稳做法 = 复用现有 `IfExpression` 处理逻辑，把 `SurfaceMatch` 的 `Arms` 平铺成同等遍历。需逐文件核对 `transpiler_stdload.go` / `transpiler_monomorph.go` / `transpiler.go` 的 match 相关 walker。
3. **checker 双形态 helper 去留**：S1 完成后 `IfExpression` match 不再产生，checker 的 `Root *IfExpression` 分支成死代码。保留还是删除？→ P7 删除（golden/spec 测试若依赖需同步改）。
4. **`desugarSurfaceMatch` 完整度**：它已被 `h0_sugar_gap_test.go`（15 引用）覆盖，但需确认覆盖到 P1–P6 全部子形态。P1 首跑时即验证；若有缺口，补 `desugarSurfaceMatch` 而非回到 in-lowering 展开。

---

## 5. 删除清单（P7，全部「先验证等价再删」）

| 项目 | 位置 | 备注 |
|---|---|---|
| `IfExpression` 12 痕迹字段 | `parser/ast.go:1642-1681` | 随节点定义删除，所有读点随 P1–P6 迁移到 SurfaceMatch 后消失 |
| `IfExpression` 已降糖 match 处理 | `parser/tohir.go:906 case *IfExpression`（仅 match 相关分支）、checker `Root *IfExpression` 分支 | `IfExpression` 节点本身保留（if/elif/else 仍需），只删 match 语义分支 |
| `RTFlag` 位 | `parser/resolver.go:112-140+` | `RTBareMatch`/`RTMatchWildcard`/`RTStandalone`/`RTMatchWrapper`/`RTElif`/`RTElseNewline`/`RTPipelineValue` 及 `HasRTFlag`/`SetRTFlag` |
| `KUnwrapAssign` | `hir/hir.go:40,127`、`parser/tohir.go:383` | `hir/golden_test.go:322` 豁免随之消失 |
| `lowering.go` 内联 SurfaceMatch 展开 | `lowerProgram` 调用 `buildMatchDesugar` 处 | `desugarSurfaceMatch`/`buildMatchDesugar` 保留供 H0 用 |
| formatter 降糖代码 | `fmt/control.go` `formatBareMatchExpressionSubj` 的 `RTMatchWrapper`/索引主体归一化反推、`writeBareMatchArm` 合成 let 打捞 | 改读 `SurfaceMatch` 字段 |

### 5.1 ⚠️ P7 前提**尚未满足**（2026-10-18 实测结论，commit `b918dba9` 当时状态）

翻转落地后曾以为「默认路径不再产生 `IfExpression` match ⇒ 12 字段成死码」。**实测证伪**：降糖后的
`IfExpression`（带 12 字段）仍是 H0 与 HIR 之间的**中间表示**，翻转只改了「何时降糖」（parse-time → H0），
没改「是否经 `IfExpression`」。因此 §5 清单**现在一条都不能删**，删了会直接打断流水线。

证据（须用 Grep 工具，Bash `grep` 对本仓库部分文件会返回**假空**——见 §3.1 纠错记录第 3 次复发）：
- `parser/tohir.go:928 case *IfExpression:`，`:929 if e.IsTypeMatch`、`:937 matched := e.MatchedExpr`、
  `:949 cur.TypePattern`、`:962 ie.IsTypeMatch`、`:980 c.expr(e.MatchedExpr)` ⇒ HIR 侧**仍在读** 12 字段。
- `parser/lowering.go` 内 `buildMatchDesugar` 仍被 8+ 处调用：`:1219`、`:1474`、`:2133`、
  **`:2371`（`desugarSurfaceMatch` 自身回调）**、`:3945`、`:4619`、`:4738`、`:5019`；
  `buildTypeMatchDesugar` 被 `:2125`、`:2367`（union 别名修复点）调用 ⇒ 12 字段**仍在写**。
- `build/transpiler_monomorph.go` 读 `e.MatchedExpr` 的路径**已清理**（2111 → 删除 IfExpression 分支里的
  `MatchedExpr`/`RangePattern`/`EqualityPattern`/`OptionPatterns`/`ValuePatterns`/`RawCond` 死字段；
  matched 表达式由专属 `*parser.SurfaceMatch` 分支经 `e.Matched` 替换；LLVM 管线 monomorph 阶段
  match 恒为 `SurfaceMatch`，JS/WASM 后端不跑 monomorph，故该 IfExpression 分支仅命中三元 if）。

**P7 真正的先行步骤**：把 `desugarSurfaceMatch` 改成**流式直出 base HIR**（即 §2 第 4 点
「經 `desugarSurfaceMatch` 流式产出 base HIR」），不再物化带 12 字段的 `IfExpression` 交给
`tohir.go:928` 去读。完成并且 `h0_sugar_gap_test.go`／535 语料 A/B 仍等价后，§5 清单才成立。

### 5.2 P7 解锁方案：降糖直出 base HIR（2026-10-19 调查定案）

#### 5.2.1 接缝坐标（实测）

```
flip 主路径（当前做法，两跳）：
  SurfaceMatch ──▶ tohir.go:1150 case *SurfaceMatch
                    └─ sp.desugarSurfaceMatch(e)           lowering.go:2291-2382
                       └─ buildTypeMatchDesugar            lowering.go:2500-2603（104 行）
                        | buildBareMatchDesugar            lowering.go:2626-2775（150 行）
                        | buildMatchDesugar                lowering.go:2776-3630（855 行）
                   ──▶ 返回带 12 字段的 IfExpression 链
                       └─ c.expr(desugared) ──▶ tohir.go:928 case *IfExpression
                          └─ IsTypeMatch? KTypeMatch/KTypeArm : KIf(+9 个 match slot)
```

#### 5.2.2 ⚠️ 关键判断：这不是「节点映射」，而是「语义移植」

直觉方案——「直接从 `sm.Arms` 发射 `KTypeMatch`」——**行不通**。因为降糖**不是纯形状变换**，它
依托 parser side table 做语义重写，这些重写目前发生在「构造 IfExpression」的过程里：

- `it`-place write 重写（`rewriteItPlaceWrites`）+ `desugarIndexOutReads` 还原（注释见 lowering.go:1063）
- index-out / 安全索引合成（`buildMatchDesugar` 内部，见 :219、:402 注释）
- subject 归一化（`__match_subj_L_C`，:4685）、`it` 绑定合成（`buildItBinding`，lowering.go:3694）
- range 条件外部的 `desugarRangeCondition`（lowering.go:3631）
- 依赖 `p.sem.FuncVarType` / `astLocalVarType` 等副表（:263、:219）

⇒ 直出 HIR 必须**完整移植**这些变换；只重映射臂会静默丢掉索引写回、`it` 绑定与安全索引语义。
移植规模实测 **约 1,109 行**（104 + 150 + 855），另加上述 helper。

#### 5.2.3 推荐实施策略：抽出「与 IR 无关的降糖计划」，双发射 + 差分护栏

不要重写一遍 AST 版与一遍 HIR 版（两份逻辑必然漂移）。建议：

1. **抽出 plan 层**：把现有三个 builder 的**决策**（臂选择、是否 type-destructuring、是否 option、
   range/raw-cond 归类、需要做哪些 index-out / it-place 重写）抽成一个 IR 无关的
   `matchDesugarPlan{ kind, unionType, arms []planArm }`，每个 arm 记
   `{pattern, binding, bodyRewrite}`。
2. **两个 emitter 消费同一份 plan**：`emitAST(plan) Expression`（给既有 no-flip 控制路径，保证对照
   不动）与 `emitHIR(c, plan) int32`（给 flip 主路径，直出 `KTypeMatch`/`KIf`）。
3. **差分护栏**：新增一门禁，对同一 `SurfaceMatch` 同时走 `emitAST`（再经 `case *IfExpression`）
   与 `emitHIR`，断言产出 HIR **逐字节相同**——不等价即 FAIL，且能点名是哪个子形态不等价。
   这是解锁过程中最重要的一道门，配合既有 `hir_equivalence_test.go` 与 535 语料 A/B 双保险。

#### 5.2.4 切片顺序（按「移植代价 ↑」而非 P1→P6 顺序）

| 切片 | 内容 | 移植代价 | 验证重点 |
|---|---|---|---|
| **S-a** ✅ **已落地**（2026-10-19） | `buildTypeMatchDesugar`（type 解构，104 行） | 最低 | `type-match.no` / `match-type-arm.no` / `txt-methods.no`；KTypeMatch 的 `Line/Col` 必须与两跳版一致（当前取 IfExpression 的 token，直出版需对齐到 `sm.Token`） |
| **S-b** | `buildBareMatchDesugar`（裸 match，150 行） | 低 | `MatchEndPos` / `OpeningBraceComment` 传递（现由 lowering.go:2373-2380 补盖章）——**实测二者都不进 HIR**，见 §5.2.7 |
| **S-c** | `buildMatchDesugar`（826 行，option/range/raw-cond/值/通配） | 高 | 全部 9 个 slot + `OptionPatterns`/`ValuePatterns` 列表顺序 + **`MatchedExpr`**；子切片方案见 §5.2.9 |

每片前后都要跑：`make test`（含 `hir_equivalence_test.go`）、`./cmd/no/`（`-count=1`）、
535 `no run` A/B、535 `no build --js` A/B、`no fmt` round-trip。**失败即回退该切片**
（手工还原编辑过的文件——`git checkout` 须用户确认后才可用）。

#### 5.2.5 完成标志

S-a/S-b/S-c 全部落地后，`tohir.go:928 case *IfExpression` 只剩**真正的 if/elif/else**（match 语义
不再走它），此时 §5 清单（12 字段、`RTFlag`、`KUnwrapAssign`、formatter 降糖代码）才真正可删。
期间注意：`transpiler_monomorph.go:2111` 读 `e.MatchedExpr` 的路径**已清理**（2026-10-19 S-d：
IfExpression 分支仅保留三元 if 字段，matched 由 SurfaceMatch 分支经 `e.Matched` 处理）。

#### 5.2.6 S-a 落地方式（2026-10-19 实测）

按 §5.2.3 的「单一真相源」原则落地，未复制第二份降糖逻辑：

1. **`resolveTypeMatchArms`（lowering.go 新增）**：把 `buildTypeMatchDesugar` 里**与 IR 无关**的
   per-arm 计算（union 成员校验、`it`/具名绑定 let 注入、`DeclaredVars`/`FuncVarType` 登记）抽成
   返回 `[]typeMatchArm`（包级类型）。AST 链与 HIR 直出**共用它**，不可能漂移。
2. **`prepareSurfaceMatch` + `desugarPreparedSurfaceMatch`（lowering.go 新增）**：拆出「准备」与
   「发射」两阶段。准备阶段有真实副作用（克隆臂防破坏原树；index-out 注解以节点指针为键，
   纯克隆会丢；`armPreChecks` 登记绑定让**内层 match** 能解析自身主体型别，且校验 `it` 用法）。
   H0 只调一次准备，故不会重复产出诊断。
3. **`typeDestructuringSubjectType`（lowering.go 新增）**：封装「是否型别解构 match」判别 +
   `recoverUnionFromMember` 恢复，两条路径共用，避免一边判成型别臂、另一边判成值 match。
4. **`lowerTypeMatchToHIR`（tohir.go 新增）**：直出 `KTypeMatch`/`KTypeArm`，不再物化 `IfExpression`。

**字节等价的三个不变量**（写进函数注释，防止后人无意破坏）：
- **位置**：`pos()` 只看 `Token`，而 `buildTypeMatchDesugar` 把 `IfExpression.Token = sm.Token`，
  故 `pos(sm) == pos(ifExpr)`，`Line/Col` 一致。
- **建节点顺序**：HIR 节点 id 按分配顺序敏感，故严格镜像 `case *IfExpression`——先每个臂的
  body（+可选 bind），再 `KTypeArm`；所有臂先于 `matched` slot；`KTypeMatch` 最后建。
- **per-arm 决策**：共用 `resolveTypeMatchArms`。

**验证**：
- `go build ./...` / `go vet ./parser/` / `gofmt` 全净。
- `./parser/` + `./parser/dump/`（含 `hir_equivalence_test.go`、子形态普查）全过。
- 型别臂 fixture 端到端输出正确：`type-match.no` → `10/3/2.5/7/3/2.5/8/5/5.5`、
  `match-type-arm.no` → `bool+int64/bare-i64/bare-i32/bare-f64`。
- **消融 A/B（最重要）**：同源编「S-a 开」(`bin/no`) 与「S-a 关」(`/tmp/no_sa_off`) 两个二进制，
  差异**只可能来自 S-a**。13 文件靶向（含全部 7 个含类型臂文件）**13/13 一致**；535 全量
  `no run` 切片进行中。
  ⚠️ **`--js` A/B 对 S-a 是空洞的**：js/wasm 后端走 **AST 降糖**（lowering.go:108 明写
  `buildMatchDesugar on BOTH the H0 and js/wasm desugar paths`），不经过 H0，故必须用 `no run`。

#### 5.2.7 S-b 落地方式（2026-10-19）

沿用 §5.2.6 的「单一真相源」原则，同样**未复制第二份降糖逻辑**：

1. **`bareMatchPlan`（lowering.go 新增，IR 无关）**：承载裸 match 的全部决策——
   independent-guard（`if(1){ if c1 {B1}; … }`）对 elif 链、dotVal / wildcard /
   defaultBody 规则、每条臂的 `EqualityPattern`/`RangePattern`/`DotValBody` 归类。
   **`alt` 与 `altBody` 必须分开记**：前者是合成
   `BlockStatement{ExpressionStatement{…}}` 包裹（降糖为 `KBlock→KExprStmt→KIf`），
   后者是通配臂自己的 body（`Alternative` 直接指向它）。混用会改变 HIR 形状。
2. **`resolveBareMatchPlan`（lowering.go 新增）**：原 `buildBareMatchDesugar` 逻辑的
   忠实移植（同样的 fast path 条件、同样的臂序、同样的 dotVal/defaultBody 规则）。
3. **`emitBareMatchAST`（lowering.go 新增）**：把 plan 包回既有 `IfExpression` 链，
   `buildBareMatchDesugar` 退化为「plan + AST 发射」两行。
4. **`lowerBareMatchToHIR` / `emitBareMatchHIR`（tohir.go 新增）**：从 plan 直出 `KIf`。

**字节等价的四个不变量**（写进函数注释）：
- **位置**：每个合成 `IfExpression` 都带 `Token = sm.Token`，故 `pos(sm) == pos(ifExpr)`。
- **建节点顺序**：`case *IfExpression` 按 cond → then → else → matched → dot-val →
  range → eq → raw 固定顺序建 slot；裸 match 的 matched/raw/OptionPatterns/
  ValuePatterns 全为空 ⇒ 塌成 `NoID` 消失，其余保持相对顺序。`alt` 包裹体内层 `KIf`
  **最先**分配，再 `KExprStmt`，再 `KBlock`（镜像 `c.stmt` 由内向外的求值顺序）。
- **合成包裹体**（`BlockStatement`/`ExpressionStatement`）可手工发射：`OverflowMode`、`IsInline`
  均为零值 ⇒ `overflowModeFlags`/`FlagInline` 均为 0；`annotate`/`embed` 两张副表
  **按节点指针索引**，合成节点无条目 ⇒ 手工发射不丢任何东西。而 `altBody`/`body`/
  `dotValBody` 是**真实 AST 节点**，必须仍走 `c.stmt` 以保留注解与 embed 数据。
- **`RTBareMatch`/`RTMatchWildcard`/`RTMatchWrapper` 不进 HIR**：`case *IfExpression`
  建 `KIf` 时**不设 Flags**；这些旗标的读取方是 `fmt/control.go` 与 `tools/check_enum.go`
  （都走糖树），故直出不设旗标是 HIR 中性的。

**`MatchEndPos` / `OpeningBraceComment` 实测不进 HIR**（§5.2.4 曾列为 S-b 风险）：
`desugarPreparedSurfaceMatch` 补盖的这两项，`MatchEndPos` 只被 `checker/match_exhaustive.go`
等读（翻转后 checker 直接读 `sm.RBracePos`），`OpeningBraceComment` 只被 `decl.go`/
`match.go`/`expr.go`（注释迁移，供 formatter）与 `astcache_det.go` 读，两者都不在
H0→HIR 路径上；且翻转后 H0 侧合成的 `IfExpression` 是临时节点、根本不在 AST 树里。

**验证**：
- `go build ./...` / `go vet ./parser/` / `gofmt` 全净（`h0_sugar_gap_test.go` 的
  gofmt 债是 HEAD 既有的 28 行，非本次引入，按政策不 sweep）。
- **等价门禁即 S-b 的差分门禁**：`TestH0MatchDesugarMatchesParserLowering` 的
  Route B（`DesugarSurfaceMatches` → `IfExpression` → `case *IfExpression`）**正是
  「S-b 关」的行为**，Route A 是直出。
- **门禁语料扩到两份**：原门禁只走 `tests/`（535 程序），而 `test/std/` 是**裸 match 最密集**
  的语料（82 个 / 87 程序）却完全没被覆盖。新增 `equivalenceCorpusRoots` 同时走两份，
  实测 **622 程序 / 0 分歧 / 257 真正走到 H0 降糖**（额外成本仅 ~0.8s）。
- **新增反空洞门禁** `TestBareMatchEquivalenceIsExercised`（`parser/dump/hir_equivalence_test.go`）：
  两份语料合计 **436 个裸 match**（tests/ 354 + test/std/ 82），低于 50 即 FAIL——
  否则「0 分歧」可能是 0 == 0。`countBareMatches` 落在 `h0_sugar_gap_test.go`。
- `make test` 16 包 0 FAIL；`./cmd/no/` `-count=1` 通过；`./parser/` + `./parser/dump/` 全过。
- 靶向 `no run` A/B 7/7 SAME（裸 match 密集文件，均有真实输出）。

#### 5.2.8 ⚠️ 消融 A/B 的两个陷阱（2026-10-19 实测，代价 1.5 小时）

1. **对照二进制必须放在与 `bin/no` 同一目录**。放在 `/tmp` 的二进制找不到相邻的 `src/`，
   只能从**内嵌 std** 取（`no vet` 报虚拟路径 `std/byte`），而 `bin/no` 从磁盘 `src/std/byte.no`
   取 ⇒ 全量 A/B 报出一片「DIFF」，全是路径渲染差异，**与被测改动无关**。
   判别法：同位置重建对照（`bin/no_sb_off`）后 crc-16/crc-32 立刻由 DIFF 变 SAME。
2. **`no vet` 单次约 4.5s**（要加载整个 std），535×2 ≈ 85 分钟，**不该拿它做全量 A/B**。
   验证 H0→HIR 改动**首选进程内的 `hir_equivalence_test.go`**（622 程序 byte 比对 3 秒），
   它的 Route B 天然等于「该切片关闭」；进程级 A/B 只做**靶向**即可。

#### 5.2.9 S-c 侦察：`buildMatchDesugar` 结构地图与子切片方案（2026-10-19，未动代码）

`buildMatchDesugar`（lowering.go:2867-3692，826 行）可切成五段，**语义重写密度差别极大**：

| 段 | 行 | 内容 | 直出必须移植的语义重写 |
|---|---|---|---|
| **A 主体归一化** | 2887-2917 / 2926-2937 / 2953-2971 | ①索引主体 `parts[i]` → 合成 `__match_subj_L_C ?elem`；②D9 place-write 准备（`__idx_L_C`、`origIdxExpr`、`baseName`）；③`#{index-out=DEF}` 注解作用域搬移 | `SetFuncVarType`/`setVarType` 登记、`isSafeIndexBase`、`inferIndexElemType`、注解从外层陈述搬到 `sm` 后的取值 |
| **B 型别与空臂零值** | 2976-3038 / 3044-3074 | ①`matchedVarType`/`enumVariants`/`elemType` 推导（含 flip fallback `astLocalVarType`）；②值上下文空臂 → `scalarZeroLiteral` + `RTEmptyArmDefault` + 搬 `IsInline/TrailingComments/ClosingBraceComment/RBrace` | **就地改 `arms[i].Body`**；零值字面量型别必须与 `elemType` 一致 |
| **C it 绑定与枚举析构** | 3077-3082 / 3086-3092 / 3110-3183 | ①`buildItBinding` + `elemType` 设 Type；②`hasRawCond` 探测；③`some(v) ->` 析构绑定泛化（`matchedIsEnum` + 载荷变体判别 + `enumListedVariants`） | 绑定 let 的注入位置（臂内 vs 链前）随 `hasRawCond` 变 |
| **D 臂发射主循环** | 3184-3590 | per-arm 分类：`MultiOptionPatterns`(3209) / `IsWildcard`(3237) / `Identifier` 条件 ok·nil·err(3295) / `NilLiteral`(3309) / `IsDotVal`(3311)；D9 place-write 改写(3321)；it 绑定注入(3328-3425)；**条件构造**(3488-3564)：MultiOption / MultiValue / IsRawCond / RangeExpression / 兜底；链式 Alternative 装配(3578-3590) | `rewriteItPlaceWrites`、`armType` 与 `isOKPathArmType`、`skipItBinding`、6 种条件归类的 slot 归属 |
| **E 尾部包装** | 3595-3690 | ①全 wildcard 特例；②`hasRawCond` → `if 1 { it = matched; <chain> }`（**设 `MatchedExpr`**）；③索引归一化 → `if 1 { __idx = …; __match_subj ?elem = …; <chain> }`（**设 `MatchedExpr`**） | 两处包装都写 `MatchedExpr`，且 E③ **就地改写原 AST 的 `idx.Index = &Identifier{idxTemp}`** |

**与 S-b 的本质差别**：S-b 只需 cond/then/else/dot-val/range/eq 六个 slot 且 `MatchedExpr` 恒 nil；
S-c 要用满 9 个 slot，还要保序 `OptionPatterns`/`ValuePatterns` 两个列表，并且**必须产出
`MatchedExpr`**——这正是 §5.2.4 表格里 S-c「验证重点」那一行的含义。

**⚠️ S-c 最大的风险点（开工前必须先判死）**：
`buildMatchDesugar` 有两处**对共享 AST 节点的就地改写**——`arms[i].Body = zb`（3073，落在
`prepareSurfaceMatch` 的克隆上，安全）与 `idx.Index = &Identifier{…}`（3666，`origMatched`
是 `clone.Matched` 指向的**同一节点**，会改到调用方原树）。翻转后 checker 走糖树、H0 走直出，
若直出漏掉第二次改写，checker 与 HIR 看到的就是两棵不同的树。**S-c 开工第一个动作是量清
checker 与 H0 的先后关系**，否则等价门禁可能给出「HIR 相同但 checker 诊断不同」的假阴性。

**子切片方案**（按「重写密度」而非行数升序，每片独立可回退）：

| 子切片 | 内容 | 依赖 |
|---|---|---|
| **S-c1 条件分类** | D 的 3488-3564 + slot 装配；先只支持「主体是 Identifier、无 D9、无枚举」 | B1 型别、B2 空臂零值 |
| **S-c2 臂体改写** | B2 + C1/C2/C3 + D 的 3328-3425（it 绑定注入、place-write、零值默认） | S-c1 |
| **S-c3 主体归一化与尾部包装** | A 全段 + E 两处 `if 1 {}` + `MatchedExpr` | S-c1 + S-c2 |

每片仍用同一套门禁：`hir_equivalence_test.go`（622 程序 byte 比对）+ 靶向 `no run` A/B +
`make test`。**S-c1 落地后 S-b 的独立分支可合并**（裸 match 与值 match 共用同一条件分类层）。

#### 5.2.10 S-c 落地方式（2026-10-19，S-c0 已落地并验证）

**S-c0（纯机械拆分，已落地）**：`buildMatchDesugar` 拆成
`resolveSubjectMatch`（A+B+C，只跑副作用、不建节点）+ `emitSubjectMatchAST`（D+E）。
校验方式：原函数体 823 行与两半做 multiset 比对，**0 丢行**（唯一差异是我有意改写并搬位的
一条注释）；`make test` 16 包全绿。新增 `subjectMatchPrep`（23 字段），其中
`placeWriteUsed` 刻意放在 prep 上——它由 D 段的 `rewriteItPlaceWrites` **回写**、再被 E 段
包装器读取，不属于任何单一阶段。

**⚠️ S-c 直出的结构性障碍（S-c0 之后实测定位）**：D 段有一处**后补 else slot**——
普通 wildcard 臂在链头已存在时执行 `ifExpr.Alternative = arm.Body`（原 3483）。HIR 侧
`hir.Builder.Add` 是值拷贝、`b.nodes` 不对外，`AppendChild` 只能追加到**子链末尾**，
而 `else` slot 是 `KIf` 的第 3 个子节点 ⇒ **无法在节点建成后再插入**。

⇒ S-c 的 HIR 发射器**必须先把 D 段做成 plan 树再发射**（与 S-b 的 `alt`/`altBody` 同一手法），
不能逐行镜像。这把 `bareMatchPlan` 推向 `matchIfPlan`（补 `matched`/`optionPatterns`/
`valuePatterns`/`rawCond` 四个 subject 专属字段），也意味着 **S-b 与 S-c 最终共用一个 plan 类型
与一个发射器**——正是 §5.2.4 预期的合并点。

**S-c1 实际切口**（按上述结论修正原方案，**2026-10-19 已落地并验证**）：
1. `resolveArmCond`（共享）：抽出 D 的「构造 match 条件」3487-3564 六路分类
   （MultiOption / MultiValue / IsRawCond / RangeExpression / 兜底），AST 与 HIR **共用**；
2. `rewriteSubjectArm`（共享）：抽出 D1（`armType` 计算 + it 绑定注入 + place-write + 零值默认），
   逐臂调用，与 D2 无跨臂依赖（已核：D2 只读本臂 body 与链状态）；
3. `matchIfPlan` 树 + `emitSubjectMatchAST` / `emitMatchIfHIR` 双发射；
   `emitMatchIfHIR` 镜像 `case *IfExpression` 的 8 固定 slot 顺序（cond/then/else/matched/
   dot-val/range/eq/raw），随后按原列表序追加 `OptionPatterns`（`opt` slot）/ `ValuePatterns`
   （`val` slot）；**不消费 `matchIfPlan.rtFlags`**（RTBareMatch/RTMatchWildcard/RTMatchWrapper
   无 HIR 表示，`case *IfExpression` 建 KIf 也不设 Flags），这是字节等价的前提；
4. `emitSubjectMatchHIR` 门控（**最终态，2026-10-19 索引主体归一化收口**）：`subjectMatchDirectHIR`
   **已全开**（`prep == nil` 时返回 false，否则恒 true）——所有到达此处的 subject match 均直出
   （类型解构由 S-a、裸 match 由 S-b 在更早处处理，不会到达）。门控演进：S-c1 初始四排除
   `subjName=="" && idxTempName=="" && !matchedIsEnum && !hasRawCond`；S-c2 放宽到 `!matchedIsEnum`
   把 `hasRawCond`/`subjName`/`idxTempName` 全纳入，但索引主体形态触发 `mir/d9_write_bce_test.go`
   回归，**回退**；S-c3 把 `matchedIsEnum` 纳入，门控收口为 `subjName==""`；**S-c4 攻克索引主体归一化**：
   在 `emitSubjectMatchWrappersHIR` 把原树（浅克隆共享指针 `prep.origMatched`）的 InBounds 证明
   **转移**到 D9 place-write 重写出的 `base[__idx]` 读节点（`c.sem == prog.Sem == p.sem`），
   `lowerSafeIndex` 保留可证界内快路径、d9 BCE 两用例恢复绿 ⇒ 门控全开：
   - `hasRawCond`：**已纳入直出**（`emitSubjectMatchWrappersHIR` 的 `if 1 { it = matched; <chain> }` 包装层）；
   - `matchedIsEnum`（枚举补集 / 载荷析构）：**已纳入直出**（S-c3，共享 `rewriteSubjectArm` + `resolveArmCond`）；
   - `subjName`/`idxTempName`（**索引主体归一化**）：**已纳入直出**（S-c4）。
     ⚠️ 历史根因（已修复）：最初误判为「克隆节点脱离 InBounds 侧表」。实测 `prepareSurfaceMatch` 是
     **浅拷贝**（`clone := *sm`），`clone.Matched` 与原树 `parts[i]` **同指针**，证明本应保留；真正丢失在
     `emitSubjectMatchWrappersHIR` 的 D9 分支——读节点被重写为**全新** `base[__idx]`（`subjValue`）
     节点，该新节点不在 InBounds 侧表 ⇒ 读缺 `FlagInBounds` ⇒ `lowerSafeIndex` 退化为运行时界检查。
     修复即把 `prep.origMatched` 的 InBounds 证明转移到 `subjValue`（等价门禁 0 分歧 + d9 两用例绿证实）。

**S-c1 验证结果（2026-10-19）**：
- G-equivalence 扩到 `tests/` + `test/std/` 两份语料：**622 比对 / 0 分歧 / 257 真正走到 H0 降糖**
  （含 S-a/S-b/S-c1 三路直出）。
- **新增反空洞门禁 `TestSubjectMatchDirectHIRIsExercised`**：经 parser 侧
  `SubjectMatchDirectEmits` 计数器（原子量，emitSubjectMatchHIR 成功发射时 +1）实测
  **518 次直出**（622 程序、503 带主体 match；含枚举补集 / 载荷析构 / 索引主体归一化形态，`hasRawCond` /
  `matchedIsEnum` / `subjName` 均已纳入直出），floor=20 ⇒ 烟雾报警而非封顶。
- `make test` 全绿（自仓库根执行，注入 StdSigKey；**不含 `./cmd/no/...`**）；`./cmd/no/`（`-count=1` + StdSigKey）通过。
- **进程级 A/B**：同目录 `bin/no`（直出 ON）vs `bin/no_sc1_off`（直出 OFF，均注入 StdSigKey）
  对直出最密集的 44 文件跑 `no run` 比较 stdout+rc，归一化 tmp 路径后 **44/44 SAME，0 DIFF**
  （首轮全 DIFF 是测量伪影：对照二进制 plain `go build` 缺 StdSigKey 注入报「签名 key 未注入」，
  且错误消息含非确定 `/var/folders/.../X.ll:N` 路径使同二进制两遍也误判——均已在修正版脚本中消除）。

---

## 6. 回退策略

- 每阶段（P1–P6）独立提交；任一门禁失败即回退该阶段 commit，默认路径恢复 in-lowering 展开（双形态并存期 checker/transpiler 已双认，回退安全）。
- `SurfaceMatch` 与 `IfExpression` 双表示**并存期**为预期中间态（checker/transpiler 双认），直到 P7 才删死代码。
- 不触及 `lowering.go` 的 surface 旗标删除（已在 §4.3 steps 1-4 完成，commit `583791e1`）。

---

## 7. 验证门禁总览

- **G-equivalence**：`parser/dump/hir_equivalence_test.go`（`TestH0MatchDesugarMatchesParserLowering`）
  —— H0 的 `SurfaceMatch` 路径与「parser 自己做降糖」产出**逐字节相同 HIR**。每次 P1–P6 改动后必跑。
  （`h0_sugar_gap_test.go` 是**另一道**门禁：证明没有节点掉进 `KUnknown`，二者互补，不是同一个东西。）

  ⚠️ **本门禁曾被翻转静默击穿为「空洞通过」**（2026-10-18 实测发现并修复）：
  它原本靠 surface 配置（`SkipUnwrapLowering + SkipSafeIndexLowering`）做隔离，但这对旗标
  **已随 §4.3 删除**（`lowering.go:337/416/432/442` 注释明写）；一次拙劣替换把那两行改成了
  `p.SkipLowering = true` **重复写三遍**，且翻转后 `LowerSurface` 本身也不再展开 match，
  ⇒ 两条路线配置**完全相同**，自己跟自己比，恒报「0 分歧」却证明不了任何事。

  **修复**：隔离方式改为——两条路线都用**默认 `ParseProgram`**（其余 lowering 全部恒定），
  仅 Route B 额外跑显式展开 `p.DesugarSurfaceMatches`（js/wasm 后端用的同一个入口）。
  并加两道非空洞断言：① Route A 必须真的带 `SurfaceMatch` 进 H0（否则不计入）；
  ② Route B 展开后 `SurfaceMatch` 必须归零（否则说明它不是「已降糖树」）。
  **破坏实验已验证**：临时让路线塌陷 ⇒ 门禁正确 FAIL 并点名 168 个文件，
  而非像修复前那样静默 PASS。修复后实测（语料已扩到 `tests/` + `test/std/`）：
  **622 比对 / 0 分歧 / 257 真正走到 H0 降糖**，S-c1 额外由 `TestSubjectMatchDirectHIRIsExercised`
  （518 次直出）钉住覆盖。
- **G-corpus**：532 语料 `no fmt` round-trip 冪等 + LSP lint/index 冒煙（沿用 §4.3 steps 1-4 留下的糖形态单形态冒煙测试）。
- **G-compile**：`no build tests/str.no` / `basic.no` / `str-test.no` EXIT=0（重建 `bin/no` 须注入 `StdSigKey`）。
- **G-unit**：`go test` parser/checker/fmt/lsp/tools/cmd/no（注入 `StdSigKey`）全绿。
- **G-ast-freeze**（C1/C6）：`SurfaceMatch` 在 `ParseProgram` 返回后不被任何阶段写入字段（§5 删除即消除 12 字段写点）。
- **G-subform-coverage**（§3.1）：`parser/dump/hir_subform_coverage_test.go`
  —— `TestS1SubformsAreCovered` 糖形态普查要求 P1–P6 每个子形态都有 fixture
  （2026-10-18 实测 535 程序 / 705 matches / 1818 臂；`SkipItBinding` 计数非零即失败）；
  `TestS1RangePatternFixtureIsExercised` / `TestS1RawCondFixturesAreExercised` /
  `TestS1TypeArmFixturesAreExercised` 从**两个方向**钉住 P3/P4/P5：普查找到的旧 fixture
  与补强 fixture 删一即失败。三个破坏面实测精确失败（见 §1.4.2）。
- **G-arm-binding**（§1.4.2 a/b）：`src/checker/s1_sugar_match_binding_test.go` 5 个测试，
  糖/降糖双形态同跑 `ValidateTypes` + `ValidateUndefinedVars` 并比较诊断集合，
  覆盖 `ok ->` 的 it 登记、裸型别臂不报未定义、未定义变量仍报（反空洞）。

---

## 8. 立项状态

- [x] §4.3 steps 1-4 已落地（commit `583791e1`）
- [x] 调查证伪「步骤 5 局部清理」，定位为 S1
- [x] 现状事实厘清（parser 只产 SurfaceMatch；H0 desugar 已实现证等价；checker 已双形态）
- [x] **P1 两阶段降糖解耦**（commit `ab157ea7`）：`LowerSurface` = `lowerProgram`(defer match)
      + `DesugarSurfaceMatches`（其后 P2 又拆成显式 pass）
- [x] **翻转落地（已提交，commit `b918dba9`）**：`ParseProgram` 默认路径**保留** `SurfaceMatch`
      （`deferMatchDesugar:true` 无条件置位；`LowerSurface` 不再调 `DesugarSurfaceMatches`，
      展开只由实验性 js/wasm 后端显式调用）
- [x] **§1.4 缺口补齐**：transpiler / checker 原生消费 `SurfaceMatch`（详见 §1.4）
- [x] **§1.4.1 回归修复**：加 `hasOptionMatchArm` 臂形守卫，修 `tests/type-match.no`
      跨模块类型解析污染（A/B 确认是回归，非预存）
- [x] **§1.4.2 收尾三类回归修复**（A/B 确认均为回归）：
      (a) `ok ->` 臂 it 登记（heap.no）；(b) 裸型别臂未定义变量豁免（type-match.no）；
      (c) 克隆上型别 match 派发失效（`armsCarryTypePatterns` 臂形派发）；
      (d) 型别臂绑定晚于 transpiler ⇒ `@i32_to_str` 不存在（type-match.no SIGSEGV，
      IR 复原 `@int_int_to_str__i64`，与基线逐字节一致）
- [x] **P4/P5 fixture 补强 + 钉住门禁**：`match-raw-cond.no` / `match-type-arm.no`
      （重设计 `mixed = num | bool`，绕开两个既有限制，见 §1.4.2）+ 三个钉住测试，
      破坏实验三面精确失败
- [x] **`SkipItBinding` 死码实测**（0/1818）+ 普查防腐烂断言；**ASTVersion bump**
      `2026-10-18.1`（缓存 key 不含路径/二进制版本，翻转必须 bump）
- [x] **G-equivalence 门禁修复（曾空洞）**：重写 `hir_equivalence_test.go` 两条路线的隔离方式
      + 加非空洞断言 + 破坏实验验证（详见 §7）。实测 532/0 分歧/168 生效。
- [x] **§4.3 删旗标留下的锈迹审计（连带修复）**：`SkipUnwrapLowering` / `SkipSafeIndexLowering`
      删除时，多处原「surface 配置」被机械替换成重复的 `p.SkipLowering = true`。全仓扫描
      （跨行正则找连续重复赋值）共 4 处，已收敛为单次赋值并注明：
      `parser/dump/hir_equivalence_test.go`、`parser/dump/checker_on_sugar_test.go`、
      `mir/d9_write_bce_test.go`、`fmt/overflow_arm_annotation_test.go`。
      ⚠️ 其中 **`checker_on_sugar_test.go` 的塌陷是实质的**：`surfaceShape` 与 `sugarShape`
      现已配置完全相同 ⇒ `allShapes()` 名义 6 形态实则只有 **3 种不同树**（lowered/sugar/sugar）。
      当前没有断言依赖 surface≠sugar，故无门禁因此空洞通过；已在源码注释记录，
      防止将来有人再加「surface 与 sugar 不同」的断言时踩 0==0。
- [x] **门禁全绿**：`make test` 全包 + `./cmd/no/` E2E + `parser/dump` 全部 corpus 门禁
- [x] **corpus 级 `no build` A/B 与基线 `ab157ea7` 全等**（G-compile，见 §7）：
      `tests/*.no` **478/478 逐一比对，0 处差异** —— 438 双双通过、40 双双失败
      （失败全为预存，与基线逐一同码，含 `std-new.no`/`err-binding.no`/`tagged-enum-*`/`http*` 等）
- [x] **`no vet` diff 产出与确认**（翻转唯一的用户可见输出变化）：对基线 `ab157ea7` 逐文件比对 ——
  - `tests/*.no`：**478 个，0 差异**；
  - `src/std/*.no`：**115 个中 110 个有差异，且全部是「多报 HINT」**（中位 +7 hint/文件；
    网络模块 http2 +695 / http +684 / ws +684 / tls +675 因 vet 会拉入依赖模块而增幅大）；
  - **0 个新增 ERROR、0 个新增 WARNING，且没有任何诊断消失**（110 增 0 减）。
  - 定性：新增 hint 是 **S4 糖盲区关闭**的直接结果——`SurfaceMatch` 存活到 checker 后，
    lint walker 能看见 match 臂内部，报出降糖树时期被隐藏的 `nolang-range-index` /
    `nolang-index` 等**真阳性**。是改善不是噪声。
  - ⚠️ `lowering.go` 原注释写「19 programs」已过时，实测为 110 个 std 文件；注释已更新。
- [x] **§1.4.3 std 模块运行期回归的修法决策（已定，2026-10-18 收尾）**：采用 **Lowering 唯一性**
      方案——不扩大 blob 序列化面、也不在烘焙期降糖，而是在 H0 唯一入口 `ASTToHIRWithMap`
      单点调「唯一收集器」`precomputeIdxLocalTypes`（`parser/lowering.go:739`），使 H0 与
      no-flip 路径共享同一张 `Sem.IdxLocalTypes`。9/9 运行期回归已消除（`bin/no` vs
      `/tmp/no_noflip` 逐字节一致），`make test` 全绿。
- [x] **corpus 级 `no run` A/B（535 文件 × 双二进制，run-2）**：修复后复跑确认归零——9 处 std
      运行期回归全部消除（`bin/no` vs `/tmp/no_noflip` 逐字节一致），仅剩 `test-div-mod-option.no`
      1 处既有改进；`match-index-option.no` 经 §1.4.2 (e) 修复后已不在差异表。详见 §1.4.3。
- [x] **union 单态化克隆体类型解构 match 误报修复**（同 commit `b918dba9`，见 §1.4.4）：
      `recoverUnionFromMember`（`parser/match.go:78`，用 `flattenUnion` **递归展开**成员集选
      最具体覆盖 union）+ `armsCarryTypePatterns` 路由守卫（`lowering.go:2365`），
      修 `type-match.no` / `match-type-arm.no` EXIT=1 回归
- [x] **翻转全量验收五项门禁全绿**（2026-10-18 收尾，commit `b918dba9` 前复跑）：
      ① 535 `no run` LLVM 端到端 **535/535 等价**；② 535 `no build --js` 内容级 **535/535**；
      ③ `make test` **16 包 0 FAIL**；④ `./cmd/no/` S4 翻形态门禁（`-count=1` 强制非缓存）通过；
      ⑤ `no fmt` round-trip **535/535 等价且冪等**
- [ ] P2–P6 按子形态推进（逐项验证 H0 desugar 覆盖）
- [ ] P7 收尾删除（§5 清单，全部「先验证等价再删」）
      ⚠️ **前提尚未满足**（§5.1）：降糖后的 `IfExpression`（12 字段）仍是 H0→HIR 的**中间表示**
      （`tohir.go:928` 仍在读），须先把 `desugarSurfaceMatch` 改成流式直出 base HIR，
      否则 §5 清单一条都不能删
- [ ] S2：H0 擦除 `SurfaceMatch`（`removedByH0` tier 的终点）

> ✅ **已提交 `b918dba9`**（2026-10-18）：翻转 + §1.4 补齐 + §1.4.1/§1.4.2/§1.4.3 修复
> + P4/P5 fixture 与钉住门禁 + §1.4.4 union 克隆修复 + 本文档更新。
> 提交前处理：① 清理 `src/` 内调查期临时文件引用（`/tmp/leak4.no`）；
> ② 对本工作**新引入** gofmt 违规的 6 个文件做格式化（**不动** HEAD 既有的 12 个文件格式债，
> 避免与本次改动混杂）；③ 确认残留 `[debug-it]` 打印**全部受 `NOLANG_DEBUG_IT` 守卫**（默认静默）。
