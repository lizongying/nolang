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
  而非像修复前那样静默 PASS。修复后实测：**532 比对 / 0 分歧 / 168 真正走到 H0 降糖**。
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
- [x] **翻转落地**（工作区未提交）：`ParseProgram` 默认路径**保留** `SurfaceMatch`
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
- [ ] P2–P6 按子形态推进（逐项验证 H0 desugar 覆盖）
- [ ] P7 收尾删除（§5 清单，全部「先验证等价再删」）
- [ ] S2：H0 擦除 `SurfaceMatch`（`removedByH0` tier 的终点）

> ⚠️ 提交纪律：翻转 + §1.4 补齐 + §1.4.1 修复 + §1.4.2 收尾修复（含 (e) 每臂独立拷贝）
> + P4/P5 fixture 与钉住门禁 + §1.4.3 归因与修法（9 处 std 运行期回归已修，Lowering 唯一性单点方案）
> 目前**仍在工作区未提交**。§1.4.3 的修法落地后独立 commit（不擅自动 git）。
