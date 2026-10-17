# 方案：Parser 忠实性与 Lowering 唯一性

> 状态：**S0 + S1a + S1 + S2 + C8 + §19/§20 已落地**；**S4 进行中**（§21：formatter、`no fmt` 与 **LSP** 三个消费方已翻到糖形态，532 语料 A/B 逐字节相同；唯一相异都是糖形态修好了旧形态的 bug——一个 `no fmt -w` 毁档、一处诊断漏报）。S6/S7 未动。S1 的**改写面已实测界定为 12 个 trace**（§14，并附硬门禁）。两条不变量的规范性定义、目标架构、阶段划分、关键约束（C1–C10）、门禁（G1–G7）与迁移顺序（S0–S7）已定。实施记录见 §12，S1a 落地与**设计稿修正**（R6 已被实测证伪）见 §13，S1 风险实测（三形态诊断普查）见 §14。  
> 涉及层：parser / hir / checker / mir / fmt / lsp / build / 自举 `compiler/`  
> 相关既有文档：`docs/design/nolang-selfhost.md`（§5.3 parser、§5.5 HIR/MIR 阶段）、`docs/design/option-unified.md`（§2.3 逐条锚定了今日 `lowering.go` 的降糖点）、`docs/design/nolang-ast-grammar.md`（AST 节点目录）、`docs/design/nolang-mm-ast.md`（AST 序列化与 `ASTVersion`）、`NOLANG-OWNERSHIP-MODEL.md`  
> 行号基准：HEAD `86131e9c`（2026-10-16）。本仓并行 session 常态在改 `src/parser/**` 与 `src/checker/**`，行号会腐坏，引用前请重新定位（本文件给出的是函数名 + 行号双锚）。

---

## 1. 两条不变量

### 1.1 P1 — Parser 不变量

> **Parser 输出的 AST 必须忠实反映源码语法结构；语法糖在 AST 中表现为独立的糖节点；parser 不执行任何语义展开、隐式注入或类型相关变换。**

### 1.2 P2 — Lowering 不变量

> **唯一的 AST→HIR pass，其唯一职责是把 AST 中的糖节点翻译为基础 HIR 节点，并保证 HIR 输出中不残留任何语法糖；采用边 lowering 边生成 HIR 的方式；不原地修改 AST。**

**「不存在独立的 AST→HIR 阶段」的准确含义**：本方案中「AST→HIR」与「lowering」是**同一个 pass**（下称 **H0**）。不设第二个转换步骤，也不存在「带糖的 HIR」这种中间表示 —— HIR 从它被创建的第一个字节起就只含基础节点。

### 1.3 术语判据（规范性）

| 术语                       | 判据                                                                                                                                               |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| **忠实（faithful）**         | 源码中每个语法构造在 AST 中有唯一对应节点；节点字段只能来自 token、位置与子节点，不得包含任何由语义或类型决定的补写。                                                                                 |
| **糖节点（sugar node）**      | AST 节点，其语义不是「它自己」，而是「某个更大基础构造的展开」。命中任一条即为糖：① 在源码中有独立拼写，且含义必须被展开才能执行；② 当前在 parser 内被就地展开；③ 语义需要类型信息才能定案。                                          |
| **基础 HIR 节点（base node）** | 三条**全中**：① 由 H0 **流式**为**唯一一个** AST 构造 1:1 产出（非回填、非再收拢、非注入）；② MIR 对它**只有一条**降低规则；③ 该规则的类型依赖**只经副表**（declared/inferred type map），不得靠重新推导节点形状获得类型。 |
| **糖 HIR 节点**             | 不满足上述任一条者。P2 要求它在 HIR 中**计数为 0**。                                                                                                                |

### 1.4 范围声明

**严格范围（本方案采纳）**：`ParseProgram` 返回之后，**任何阶段不得写入任何 AST 节点字段**。

允许被写的只有与节点解耦的副表：`Sem`（及其 node-keyed 条目）、`Warnings`、`TrailingComments`、以及新的 type map。解析期由 parser 自己记录的形式无关 trivia（如 `OpeningBraceComment`）属于**合法的 parser 输出**，不受此限。

**明确排除在两条不变量之外**：程序级**链接与命名**（模块合并、重载改名、跨函数 `-async` 着色、协程组展开）。它们是「装配」，不是「降糖」，见 §3.4。

---

## 2. 现状与违反点

### 2.1 今日的阶段边界

```
token ──[lexer]──►
        ──[parse loop]──► AST(已含糖节点)
        ──[ResolveProgram]──► 副表(注解/平台键/泛型/embed)      src/parser/resolver.go:628
        ──[lowerProgram]──► AST(糖已被就地展开)  ◄── 违反 P1     src/parser/parser.go:1600
        ──[flushCompoundAssigns]──►                            src/parser/parser.go:1611
        ═══════ ParseProgram 返回 ═══════
        ──[checker.Validate*]──► 在「已降糖的 AST」上跑
        ──[整程序装配]──► 合并/改名/摊平，就地改 AST  ◄── 违反严格范围   src/build/transpiler.go:1208-1303
        ──[ASTToHIRWithMap]──► 只读流式 walk（合规）              src/parser/tohir.go:52
        ──[三个注入 pass]──► HIR 合成节点  ◄── 违反 P2            src/parser/tohir.go:77/81/108
        ──[LowerHIR]──► MIR ──[EmitLLVM]──► LLVM
```

### 2.2 P1 的违反点

| #  | 违反                                 | 锚点                                                                                                                                                                                         | 性质                        |
| -- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------- |
| V1 | `ParseProgram` 在返回前执行降糖            | `parser.go:1600` → `lowering.go:59`                                                                                                                                                        | 语义展开                      |
| V2 | 降糖是**反射遍历 + 就地替换**                 | `lowering.go:73` `l.walk(reflect.ValueOf(prog))`；`:119` `v.Set(...)`                                                                                                                       | 原地修改                      |
| V3 | 降糖**依赖类型**                         | `lowering.go:493` `inferOptionArith`、`:860` `signedIntOperandType`、`:683` `maybeCaptureOptionAssign`，读 `sem.FuncVarType` / `VarTypes`                                                      | 类型相关变换                    |
| V4 | **注入合成节点**                         | `lowering.go:609/683` 合成 `__cap_N` 与 `?T` 注解；`:960` `maybeIndexOutAssign` 合成 match；`lowering.go:81-90` 的 `__opt_N`                                                                         | 隐式注入                      |
| V5 | 复合赋值在**解析期**即降成 `?=`               | `stmt.go:850`、`:870-901`；`flushCompoundAssigns:912` 类型驱动                                                                                                                                   | 语义展开 + 类型驱动               |
| V6 | `->` 管道在**解析期**筑成嵌套 `IfExpression` | `stmt.go:1848`（陈述）、`:1230-1247`（赋值 RHS）、`match.go:253-286`（臂体）                                                                                                                             | 语义展开                      |
| V7 | 类型匹配降成 if 链，原始臂信息只以痕迹字段留存          | `lowering.go:1732` `buildTypeMatchDesugar`；痕迹字段见 `ast.go:1611-1650`                                                                                                                        | 语义展开 + 信息损失               |
| V8 | 整程序装配写 AST 节点字段                    | `parser.SetModuleOwner`/`SetSourceFile`/`SetSourceFileDeep`（`ast.go`）写 `s.ModuleOwner`/`s.SourceFile`；`transpiler.go:1208-1303` 写 `fd.Name = use.Alias`、`ls.Name.Value`、`ls.IsModuleConst` | 违反严格范围                    |
| V9 | 反射读 AST 的推断类型字段                    | `tohir.go:170` `PopulateInferredTypes` → `nodeType(astNode)`                                                                                                                               | 违反严格范围（读的是 parse 后被写入的字段） |

### 2.3 P2 的违反点

| #  | 违反                | 锚点                                                                                      | 性质               |
| -- | ----------------- | --------------------------------------------------------------------------------------- | ---------------- |
| W1 | walk 之后注入合成 HIR   | `tohir.go:77` `MonomorphizeGo`、`:81` `DesugarGoInlineAwait`、`:108` `DesugarAsyncGroups` | 非流式变换            |
| W2 | 把降糖产物**再收拢**回语义节点 | `tohir.go:828-874`：类型匹配的 if 链收成 `KTypeMatch` + `KTypeArm`                               | 非流式、且是「反向降糖」     |
| W3 | MIR 靠**形状**反推语义   | `hir2mir.go:3455` 靠 if 链形状判断「这是不是一个 match」                                              | 基础 HIR 不成立（判据 ③） |
| W4 | 糖 kind 存活在 HIR    | `KUnwrapAssign`（`hir/golden_test.go:322` 的豁免表自述它「永远到不了 HIR」，是**死 kind**）                | 残留               |
| W5 | 纯语法节点存活在 HIR      | `KGrouped`（括号），MIR 三个 case 只是透明穿透：`hir2mir.go:1519/5743/8162`                           | 残留               |

### 2.4 违反点的补偿机制（本方案将全部删除）

这些机制的存在本身就是「降糖有损」的证据：

1. **痕迹字段**（`src/parser/ast.go:1611-1650`）：`MatchedExpr`、`DotValBody`、`RangePattern`、`EqualityPattern`、`OptionPatterns`、`ValuePatterns`、`RawCond`、`MatchEndPos`、`IsTypeMatch`、`TypePattern`、`TypeBindingName`、`IsTypeDefault`。
2. **`RTFlag` 位**（`src/parser/resolver.go:85-121`）：`RTBareMatch`、`RTMatchWildcard`、`RTStandalone`、`RTMatchWrapper`、`RTElif`、`RTElseNewline`、`RTPipelineValue`、`RTEmptyArmDefault`。注释自述「僅 formatter 讀取，編譯管線不讀」。
3. **按需跳过降糖的两个开关**（`parser.go:90-104`）：`SkipUnwrapLowering`、`SkipSafeIndexLowering`；曾经的消费方 `no fmt` / `no fix` / LSP / `tools/astpatch` / `tools/std_qualify` / `tools/std_methodify` 已全部翻到糖形态（§21.4 / §21.9 / §21.11 / §21.12），只剩形状门禁的 `sugar=false` 分支还在设它们。
4. **缓存键按降糖输出 bump**：`src/parser/astdiskcache.go:89` `ASTVersion`，规则自述「bump this for any change to the lowering pass」。

**它们全部在 S4 一并删除** —— 糖节点保留原始表面信息后，不再需要任何痕迹。

---

## 3. 目标架构

### 3.1 数据流

```
token ──[S0 Lexer]──►
        ──[S1 Parser]──►  AST(含糖，冻结) ─────────────────────────┐
                                                                  │ 只读
        ──[S2 Checker(糖感知)]──► AST(只读) + 类型副表 ────────────┤
                                                                  ▼
        ──[S3 Lowering H0]──► 基础 HIR(无糖) ──[S4 装配 H1]──► HIR'
                                                 ──[S5 MIR]──► ──[S6 Codegen]──► LLVM
```

### 3.2 阶段职责

| 阶段                     | 输入               | 输出            | 职责                    | **禁止**                 |
| ---------------------- | ---------------- | ------------- | --------------------- | ---------------------- |
| **S0** Lexer           | 源字节              | token         | 词法                    | —                      |
| **S1** Parser          | token            | AST（含糖节点）     | 只产语法；糖以独立节点存在         | 展开、注入合成节点、读写类型         |
| **S2** Checker         | AST（含糖）          | 诊断 + **类型副表** | 直接对糖节点做类型/穷尽性/合法性检查   | 写 AST 节点字段（含 `.Type`）  |
| **S3** Lowering **H0** | 冻结的含糖 AST + 类型副表 | 基础 HIR        | **唯一降糖点**；流式；不原地改 AST | 写 AST；walk 之后注入/再收拢/回填 |
| **S4** 装配 **H1**       | 基础 HIR           | HIR'          | 链接与命名（封闭清单，见 §3.4）    | 引入糖 kind               |
| **S5** MIR             | HIR'             | MIR           | 基础节点 → MIR            | 靠节点形状反推语义              |
| **S6** Codegen         | MIR              | LLVM IR       | —                     | —                      |

### 3.3 S1 / S2 / S3 的细化约束

**S1 Parser**

- 解析期若保留类型推断，**只能写副表**，不得写 `stmt.Type`（`IsInferred`）等节点字段。
- 糖节点必须携带完整表面信息（含 `Pos`/`EndPos`），使 formatter 与 `no fix` 无需痕迹字段（约束 C7）。
- 关键推论：**parser 无法判定 `a + b` 是否上抛 `?T`**（那是类型问题）。因此 `OptionArith` 糖节点对**所有候选算术位置**一律标记，判定权下放给 S2/S3，通过副表类型回答。

**S2 Checker（前移）**

- 位置从「降糖之后」前移到「lowering 之前」，输入是**含糖 AST**。
- 需新增糖节点类型规则：`MatchExpr`（含类型臂）、`UnwrapAssign`、`CompoundAssign`、`OptionArith`、`IndexOutAssign`、`Pipeline`。
- 现在就地写 `.Type` 的做法改为**返回 type map**；`IdxLocalTypes`（`parser.go:1608`）已是这一模式的先例。
- `PopulateInferredTypes`（`tohir.go:170`）的反射读被删除，改为消费 checker 的 type map。

**S3 Lowering H0**

- 形态：沿 AST 前进，每遇糖节点立即产出其基础 HIR 展开，**不回头改已产出的节点**。
- 共享节点别名（现 `lowerer.visited` 的指针去重，`lowering.go:107-110`；典型是 `MatchedExpr`）改用 **AST 指针 → HIR id 记忆化**，避免重复发射。
- `hir.Builder` arena（`hir.Builder.Add` 返回 node id）天然支持 append-only，**不需要新的 HIR 表示**。


### 3.4 H1 的封闭清单（明确排除在两条不变量之外）

H1 是**链接与命名**，不是降糖：

| 项                                  | 今日位置                                         | 为什么不能进 H0                                         |
| ---------------------------------- | -------------------------------------------- | ------------------------------------------------- |
| 模块合并 / `use` 展开                    | `build/transpiler.go:1208-1303`              | 需要先解析被导入模块，属跨文件装配                                 |
| 重载改名 / 冲突前缀                        | 同上                                           | 需要全程序名字集合                                         |
| `co`/`go` 的 `-async` 单态化与**跨函数**着色 | `hir/async_group.go:488` `MonomorphizeGo`    | 需要调用图**不动点**，与「流式」在定义上冲突                          |
| 协程组展开                              | `hir/async_group.go:69` `DesugarAsyncGroups` | 只看**父 block**，流式可见 ⇒ **应下沉到 H0**，H1 只留必须全程序不动点的部分 |

**H1 的允许与禁止**：允许新增函数、改写被调用者引用、填 `Package.Owners`/`FuncOwners`/`GoSpawns`；**禁止引入任何糖 kind**（门禁 G3）。

> 定性依据：H1 不翻译「语法糖」，它做的是「同一个语法构造在另一个模块里的实体是谁」。把它算进 H0 会让 H0 需要全程序不动点，直接摧毁 P2 的流式性质。

---

## 4. 糖节点清单（最小集）

### 4.1 清单

| # | 糖节点                                           | 现拼写                                              | 今日展开点                                            | 今日的违规性质                  |
| - | --------------------------------------------- | ------------------------------------------------ | ------------------------------------------------ | ------------------------ |
| 1 | `MatchExpr`（沿用 `SurfaceMatch`，扩展为**保留类型臂原样**） | 裸 `{ c -> b }`、`x: { … }`、类型匹配 `v: { i64 -> … }` | `lowering.go:1658/1999/1849/1732`                | V1+V3+V7                 |
| 2 | `UnwrapAssign`（已有 `UnwrapAssignStatement`）    | `x ?= e`                                         | `lowering.go:3293`                               | V1（展开为 `BlockStatement`） |
| 3 | `CompoundAssign`                              | `a += b`、`a -= b` …                              | `stmt.go:850/870-901`；`flushCompoundAssigns:912` | V1+V5                    |
| 4 | `OptionArith`                                 | `a + b` 等「可能上抛 `?T`」                             | `lowering.go:493` + `:683` + `:609`              | V3+V4                    |
| 5 | `IndexOutAssign`                              | `a[i] #{index-out=0} = v`、place-match 写入         | `lowering.go:960`                                | V1+V4                    |
| 6 | `MultiAssign`（已有 `MultiAssignStatement`）      | `a, b = f()`                                     | 未在 parser 展开                                     | 只需分类确认 + 保持 1:1 产出       |
| 7 | `Pipeline`                                    | `cond -> A -> B`                                 | `stmt.go:1848`、`:1230-1247`、`match.go:253-286`   | V6                       |

**为什么是最小集**：这 7 项正是**当前在 parser 内被就地展开**的那批，也就是 V1–V7 的全部现症。字符串插值、`#{}` 注解、方法调用摊平、`co`/协程组等「下游才翻译」的构造，本波**不**升级为糖节点（它们不在 parser 内展开，不违反 P1）。

### 4.2 糖 → 基础 HIR 映射

| 糖节点               | 基础 HIR 展开                                                                   |
| ----------------- | --------------------------------------------------------------------------- |
| `MatchExpr`（值匹配）  | `KIf` 链 + 臂体 slot；`it` / `ok(v)` 绑定 → `KLet` + `KSlot`                      |
| `MatchExpr`（类型匹配） | `KTypeMatch` + 每臂 `KTypeArm`（**直接产出**，不再经 if 链往返）                           |
| `UnwrapAssign`    | tag 判定 → `KIf`；ok 臂 `KLet`/`KAssign`；nil/err 臂 `KReturn`（有 `?T` 结果参数时）或默认零值 |
| `CompoundAssign`  | 与 `UnwrapAssign` 同形（今日 `+=` 就是先降成 `?=`）                                     |
| `OptionArith`     | `KInfix` + option 包装节点；**不合成 `?T` 注解、不建 `__cap_N`**——类型由副表回答                |
| `IndexOutAssign`  | 安全索引的 ok 臂 `KIndexStore` + 越界取值语义                                           |
| `MultiAssign`     | 1:1 → `KMultiAssign`                                                        |
| `Pipeline`        | 短路 `KIf` 链（陈述形态）／`KCond`（取值形态）                                              |

### 4.3 随之删除的东西

§2.4 的 1/2/3 项全部删除。具体：

- 12 个痕迹字段（`ast.go:1611-1650`）；
- 因降糖而存在的 `RTFlag` 位（`resolver.go:85-121`）；
- `SkipUnwrapLowering` / `SkipSafeIndexLowering` 两个开关（`parser.go:90-104`）与其全部消费方。截至 §21.12，真实消费方已全部翻到糖形态（`no fmt` / `no fix` / LSP / `tools/astpatch` / `tools/std_qualify` / `tools/std_methodify`），剩下的 `sugar=false` 分支**只为形状门禁存在**（量测 surface 侧行为用），随门禁一起删；
- `hir.KUnwrapAssign`（死 kind，`hir/golden_test.go:322` 的豁免条目随之消失）。

`no fmt` / `no fix` / LSP / `astpatch` / `std_qualify` / `std_methodify` 改为**直接读糖节点**，一个真值源。

### 4.3.1 实现计划（2026-10-17 起，实测校正后的真实 scope）

> ⚠️ 上面「消费方已全部翻糖」的判断**已过时**。`grep` 实测：3 个工具的 **surface 分支**仍在 set 这两个旗标，约 **22 个单测**也在 set。且这些单测构造的「半降糖中间态」（只保留 `?=`/`#{index-out}` 为糖、其余照常降糖）删旗标后**不可达**。删除必须逐文件迁移，不是机械 grep 删除。

**行为不变性（关键安全论据）**：默认编译路径 `SkipLowering=false` 时两旗标为零值，`lowering.go` 的 gate `if !l.p.SkipUnwrapLowering` 等恒真 → 完全展开。删旗标 = 让展开**无条件发生**，默认路径**逐位元不变** ⇒ `no build`/`no vet`/`no test` 不受影响。`no fmt`/`lsp`/正式工具路径早已只 set `SkipLowering=true`（糖），根本不碰这两个旗标 ⇒ 亦不受影响。唯一受影响的是「surface 消费方」（3 工具 surface 分支 + 22 单测）。

**执行顺序（每步后用 `make test` / `go test ./parser/... ./checker/... ./fmt/... ./tools/...` 验证）**：

1. **生产工具 surface 分支**（自身注释已标「随旗标一起删」）：`tools/astpatch/main.go` 的 `parseSourceShape`/`parseCodeExprShape`/`parseCodeStmtsShape` 删 `sugar=false` 分支，统一走 `SkipLowering=true`；`tools/std_qualify/main.go:293-296`、`tools/std_methodify/main.go:160-163` 同改。其形状门禁（`*_test.go` 的 surface 形态）改为只留 sugar 形态或删除。
2. **`lowering.go` 的 gate 删除**：238/319/334/345/970-974/2841-2845/3179-3182/3413/3508 处的 `if l.p.SkipUnwrapLowering` / `if !l.p.SkipSafeIndexLowering` 全部去掉，展开无条件化。
3. **删 `parser.go:90-104` 两旗标定义**。
4. **迁移 22 个单测**（最重的一步，需逐文件判断）：
   - 仅要「保留原始节点供分析」的（`bounds_test.go` 的 `AnalyzeInBoundsIndex`、`s3_inferred_side_table_test.go` 的副表推断）→ 换成 `p.SkipLowering = true`（糖形态同样保留节点）。
   - 形状 A/B 门禁（`checker_on_sugar_test.go` 的 surface/surface-lw 形态、`hir_equivalence_test.go`、`astpatch`/`std_qualify`/`std_methodify` 的 `shape_gate_test.go`）→ 删 surface 形态，只留 lowered/sugar。
   - 断言「`+=`→`UnwrapAssignStatement` 中间态」或「`?=`/`#{index-out}` 不降糖」的（`compound_assign_test.go`、`d9_index_write_test.go`、`option_capture_test.go` 等）→ **改写断言为最终降糖形态**（default 路径下 `UnwrapAssignStatement`→`BlockStatement`、`#{index-out}`→match 块），因为该中间态已不可达。
   - 显式 set `= false` 的（`zz_tmp_dump_test.go`、`zz_hir_dump_test.go`、`d9_index_write_test.go` 的 default 分支）→ 直接删这两行（默认即 false）。
5. **删 12 痕迹字段**（`ast.go:1611-1650`）+ **RTFlag 位**（`resolver.go:85-121`）+ **formatter 降糖代码**（`formatBareMatchExpressionSubj` 的 `RTMatchWrapper`/索引主体归一化反推、`writeBareMatchArm` 合成 let 打捞）。此步独立、风险更高，单独成项，不在第一步内。

> `hir.KUnwrapAssign` 死 kind 一并删（golden_test.go:322 豁免随之消失）。

### 4.3.2 ✅ §4.3 旗标删除已落地（2026-10-17）

步骤 1–4 全部完成，验证通过：

- **步骤 1（生产工具 surface 分支）**：`tools/astpatch/main.go`（`parseSourceShape`/`parseCodeExprShape`/`parseCodeStmtsShape`）、`tools/std_qualify/main.go`、`tools/std_methodify/main.go` 删 `sugar=false` 分支，统一 `p.SkipLowering = true`；其 `shape_gate_test.go` 的 surface 形态改为只留 sugar。
- **步骤 2（lowering.go gate 删除）**：238/317/332-343/972/2843/3178 处的 `if l.p.SkipUnwrapLowering` / `if !l.p.SkipSafeIndexLowering` 全部去掉，展开无条件化（默认路径行为不变）。
- **步骤 3（删 parser.go 定义）**：`parser.go:90-104` 的 `SkipUnwrapLowering` / `SkipSafeIndexLowering` 两字段及注释已删。
- **步骤 4（单测迁移）**：22+ 个单测的旗标 set 已迁到 `p.SkipLowering = true`；其中「surface vs sugar」A/B 形状门禁（`no_fix_shape_gate_test.go`、`fmt/sugar_fmt_ab_test.go`、`lsp/sugar_lint_ab_test.go`、`lsp/sugar_index_ab_test.go`）改写为**糖形态单形态语料冒煙測試**（parse→format/lint/index→重解析→冪等，532 份語料不崩潰）；随 surface 退役的 obsolete allowlist（`sugarFmtKnownDivergence` / `sugarLintKnownDivergence` / `overflowShapeKnownDivergence`）一并删除。LSP 的 `fixOverflowSourceShape` / `fixMatchArmsSource` / `fixUnwrapSourceShape` 删 `sugar bool` 参数与 surface 分支。

**验证**：`go build ./...` 干净；`go test` 全绿（parser / checker / fmt / lsp / tools / mir / cmd/no / mmast / package，均注入 `StdSigKey`）。默认编译路径（SkipLowering=false）与糖形态（SkipLowering=true）行为均不变 ⇒ `no build` / `no vet` / `no test` 不受影响。

**步骤 5 尚未做（独立高风险项，单独排期）**：12 痕迹字段、RTFlag 位、formatter 降糖代码仍保留，待单独迁移。

---

## 5. base / sugar HIR 分类裁定（规范性）

按 §1.3 的三条判据逐 kind 裁定：

| HIR kind                  | 裁定        | 理由 / 需要的动作                                                                       |
| ------------------------- | --------- | -------------------------------------------------------------------------------- |
| `KTypeMatch` / `KTypeArm` | **基础**    | 前提：**删除 `tohir.go:828-874` 的再收拢**，改由 H0 从糖节点直接产出。否则它是「非流式变换的产物」，按判据 ① 即为糖        |
| `KCond`                   | 基础        | `c ? a : b` 是独立源构造，MIR 单规则 `lowerCond`（`hir2mir.go:5758`）                        |
| `KMultiAssign`            | 基础        | `a, b = f()` 1:1 产出，MIR 单规则 `hir2mir.go:3384`                                    |
| `KIter`                   | 基础        | `i <- [0..n)` 1:1 产出；MIR `hir2mir.go:4204`                                       |
| `KSliceLit`               | 基础        | 1:1 产出；MIR `hir2mir.go:5609/5836`                                                |
| `KRun` / `KAwait`         | 基础        | 1:1 对应 `run` / `awy`；MIR `hir2mir.go:6566/6568`                                  |
| `KAnnotation`             | 基础（**暂**） | 承载平台 / overflow 属性。**第二波**目标：H0 把注解消费成基础 HIR 上的属性位，`KAnnotation` 从 HIR 删除（见 §10） |
| `KGrouped`                | **糖（括号）** | 纯括号无语义。H0 抹除。安全性：formatter 读 **AST** 不读 HIR，故抹除对格式化无影响                           |
| `KUnwrapAssign`           | **糖**     | 对应糖节点 `x ?= e`，由 H0 消费。**从 HIR kind 中删除**                                        |

> 判据 ③ 的措辞经过一次修正：初稿写作「MIR 规则**不读类型**」，但 `KTypeMatch`/`KIter` 等显然需要类型。正确的表述是「类型依赖**只经副表**，不得靠重新推导节点形状获得」——被禁止的是 W3（`hir2mir.go:3455` 靠 if 链形状反推 match），不是「读类型」。

---

## 6. 关键约束

- **C1** `ParseProgram` 返回后，无任何阶段写 AST 节点字段。（P1 严格范围）
- **C2** 每个实现 `statementNode` / `expressionNode` / `annotationValueNode` marker 的 AST 类型，必须被显式分类为 base 或 sugar，不得有未分类项。
- **C3** 每个 sugar AST 类型必须被 H0 消费（在 H0 的 dispatch 中有 `case`）；每个 H0 `case` 必须对应已登记的 sugar 类型（无孤儿 handler）。
- **C4** H0 之后、H1 之前，HIR 中不得存在糖 kind。
- **C5** H0 之后新增的 HIR 节点只能来自 H1 的封闭清单，且不得是糖 kind。
- **C6** H0 不修改 AST：AST dump 在 H0 前后逐字节相等。
- **C7** 糖节点必须携带完整表面信息（含 `Pos`/`EndPos`），使 formatter 与 `no fix` 无需任何痕迹字段。
- **C8** `no ast` 必须能 dump 全部糖节点（`src/parser/dump/dump.go` 需补 `case`；`dump/dump_completeness_test.go` 会强制）。
- **C9** `ASTVersion` 语义重定义：降糖离开 `ParseProgram` 后，缓存键只覆盖表面语法 + 解析期副表；**降糖形状变化不再需要 bump**。降糖/装配的形状版本改由文档化的 `HIRVersion` 概念承担（当前无 HIR 磁盘缓存，仅文档化）。
- **C10** `compiler/` 自举树与 `docs/design/nolang-selfhost.md` §5.3/§5.5 必须同步，否则 `scripts/selfhost_diff.sh` 的 L2 逐字节 AST oracle 会偏离。

---

## 7. 门禁

每条约束配一个**可失败**的门禁，且每条门禁都有**反空洞措施**（本仓惯例：`0 违规 / ALL PASSED` 在被证伪前不算数；先写违例语料 → 确认门禁 FAIL → 再跑干净语料）。

### G1 ← C1 / C6 — AST 冻结

`TestASTIsFrozenAfterParse`：在 `ParseProgram` 返回点用 `dump.DumpWithOptions` 取规范化 dump（确定性已由 `dump/dump_completeness_test.go:TestDumpIsDeterministic` 保证），在 post-checker / post-H0 / post-H1 各再取一份，逐字节比较。`Sem` / `Warnings` / `TrailingComments` **单独**比较（它们不是节点）。

**反空洞**：① 配套 self-test 人为改一个节点字段（如 `LetStatement.Name`），断言 dump **必变**；② 断言 dump 行数 ≥ 阈值，防「dump 报错 → 空串相等」。

### G2 ← C2 / C3 — 糖节点两向分类

把 `src/parser/tohir_exhaustive_test.go` 的反射手法（用 Go `go/ast` 读源码，比对 marker 类型集与 `switch` case 集）**双向化**并扩展到 H0 的 dispatch：新增 `sugarASTTypes` 登记表。断言：① 无未分类项；② 每个 sugar 有 handler；③ 无孤儿 handler。

**反空洞**：沿用 `TestMarkerCountsAreSane` 的下界，加 `len(sugarASTTypes) > 0`；要求每个 sugar 类型**被语料中某个文件实际产出**（与 `hir/golden_test.go:300` 的两向纪律同款）。

### G3 ← C4 — HIR 无糖

声明 `sugarHIRKinds` 集（§5 裁定为糖的 kind），语料上 `KindCounts` 求和为 **0**。

**反空洞**：在 **H0 之后、H1 之前**取一次快照，要求声明集里每个 kind 在该快照上**非 0** —— 若某 kind 从未被产出，说明该豁免是死的，必须删掉条目。`hir/golden_test.go:322` 的 `KUnwrapAssign` 条目已经是这个模式，直接泛化。

### G4 ← C5 — H1 增量可解释

比较 H0 快照与最终 `pkg.Nodes` 的长度 / kind 直方图，差值必须能被 H1 封闭清单**逐条解释**。

### G5 — 行为等价

`scripts/mir_golden.sh` 对 `tests/golden/mir-baseline.tsv`：逐文件比 `(rc, sha256(stdout))`，**绝不比 stderr**（裸 `go build` 会吐 compiler version mismatch 造成假 DIFF）。

### G6 — 自举

`scripts/selfhost_diff.sh` 五级门禁，重点 **L2**（逐字节 AST oracle）。注意 L1（token 级）当前因无 `no tokens` 子命令而 UNAVAILABLE，不是 PASS。

### G7 ← C9 — 缓存

增量构建的 cache-hit 测试，验证拆键后不会静默复活陈旧 blob —— `astdiskcache.go:70-79` 记录的正是这个失败模式的实测事故（`ovf-int-default` 假阳性，只在 cache HIT 时复现，`NOLANG_AST_CACHE_OFF=1` 即消失）。

---

## 8. 迁移顺序

每步独立可落地、可验证、保持绿（本仓为共享树，并行 session 常态在改 `src/parser/**` 与 `src/checker/**`）。

| 步      | 内容                                                                                                                                                                                                           | 证伪门禁                                                                 | 会静默坏掉的东西                                                                                                                                                                  |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **S0** | 只加 instrumentation：`sugarASTTypes`、双向反射测试、dump-equality harness、`sugarHIRKinds`；两条不变量成立前用 `t.Skip`                                                                                                           | G1/G2/G3 的 **self-test** 通过；套件 skip                                  | 无（零行为变更）                                                                                                                                                                  |
| **S1** | 把 `lowerProgram` 从 `ParseProgram`（`parser.go:1600`）摘出；checker 改在**含糖 AST** 上跑；给 checker 补糖节点规则                                                                                                               | G5 + G6/L2                                                           | **最大风险点**：按降糖后形状识别的规则全部失效 —— `hir2mir.go:3455` 靠 if 链形状反推 match、`ValidateUnhandledIndex`、match 穷尽性（`checker/match_exhaustive.go:19`、`:387` 的 Fallback）。必须逐条补 surface case |
| **S2** | 把降糖折进流式 `hirConv`：删 `lowerer.walk` 的 `v.Set`，从糖节点直接产出基础 HIR；删 `tohir.go:828-874` 的再收拢                                                                                                                        | G1（AST 仍含糖且冻结）+ G3 + G5                                              | `lowerer.visited` 的指针去重与 `declaredLocals` 的顺序语义（共享 `MatchedExpr`）必须由「AST 指针 → HIR id 记忆化」等价复现                                                                             |
| **S3** | 类型驱动的降糖只读副表（`inferOptionArith` / `signedIntOperandType` / `maybeCaptureOptionAssign`）                                                                                                                        | G5 + G1（证明没有回写）                                                      | inferred 与 declared 的区分（现 `nodeType` 只返回 declared）                                                                                                                        |
| **S4** | 消费方改读糖节点：`no fmt`（`cmd/no/main.go:1527`）、`no fix`（`:1687`、`unwrap_fix.go:184`）、`astpatch`（`src/tools/astpatch/main.go:125`）、LSP；删两个 skip 开关与全部痕迹字段                                                           | `dump_completeness` / `spec_gen` 528 文件逐字节回放 + `no fmt` 幂等 / reparse | formatter 里反推降糖形状的启发式（如从合成 let 的 `Value` 捞回原主体，`fmt/control.go:198-200`）                                                                                                  |
| **S5** | 消灭 parser 之外的 AST 写点：`SetModuleOwner` / `SetSourceFile` / `SetSourceFileDeep` 与 `transpiler.go:1208-1303` 的 `fd.Name` / `ls.Name.Value` / `ls.IsModuleConst` 全部改为副表 / HIR 侧记录；删 `PopulateInferredTypes` 的反射读 | **G1 转为非 skip**                                                      | `tohir.go:62-102` 的 `Owners` / `FuncOwners` 键（现在依赖 `GetModuleOwner(stmt)` 读节点字段）                                                                                          |
| **S6** | 重定义 `ASTVersion` 政策（C9），文档化 `HIRVersion`                                                                                                                                                                     | G7                                                                   | 陈旧缓存静默复活旧语义                                                                                                                                                               |
| **S7** | `compiler/` + `nolang-selfhost.md` 同步；跑满五级                                                                                                                                                                   | G6 全五级                                                               | L2 oracle 逐字节偏离                                                                                                                                                           |


## 10. 未决决策

以下三项**不在第一波**，但需要在实施前定案：

1. **`co`/`go` 单态化是否算「程序级装配」** —— 本方案判**是**（§3.4），须列入 H1 封闭清单。若判否，则 H0 需要全程序不动点，与「流式」在定义上冲突。
2. **`KGrouped` 是否在 H0 抹除** —— 本方案判**是**（纯括号；formatter 读 AST）。这是一个**理想的第一个非空洞实例**：改动极小、行为中性，可用来证明 G3 真的会 FAIL。
3. **`#{...}` 注解是否要在 HIR 中消失** —— 现 `KAnnotation` 存活。建议第二波：H0 把注解消费成基础 HIR 上的属性位，`KAnnotation` 从 HIR 删除。

---

## 11. 与自举 `compiler/` 的同步要求（C10）

`compiler/` 是自举中的编译器（`src/ps.no` ↔ `src/parser/*.go`，`src/ast.no` ↔ `parser/ast.go` + `dump.go`，`src/asp.no` 是自动生成的 dump spec）。它必须**复现构造路径**，不只是语义：

- `compiler/src/ps.no:5682` 明确写着「`bin/no` 在解析期把 match 展开成 if/else（非 `SurfaceMatch`）」—— 本方案改变这条事实，该注释与相应代码必须同步。
- `nolang-selfhost.md` §5.3（parser）与 §5.5（HIR/MIR 阶段）的流水线图与阶段描述需同步。
- 否则 `scripts/selfhost_diff.sh` 的 **L2 逐字节 AST oracle** 必然偏离。

`compiler/` 侧尚未实现的 `hir.no` / `hir2mir.no` / `mir.no` / `ck.no` / `cg.no`（见 `nolang-selfhost.md:436-500`）应在 H0 的形态**定案之后**再写，避免按今日的「parser 内降糖」形态实现一遍。

---

## 12. S0 实施记录（instrumentation，零行为变更）

三个新测试文件，不改生产代码（`git diff HEAD -- src/` 为空）：

| 文件                                        | 内容                                                                    |
| ----------------------------------------- | --------------------------------------------------------------------- |
| `src/parser/sugar_classification_test.go` | `sugarASTTypes`(4) / `baseASTTypes`(62) 显式登记表 + 双向完备性（C2/G2）+ 登记项活性检查 |
| `src/parser/dump/ast_freeze_test.go`      | G1 harness：C6 半已启用；C1 半改为 checker 变异普查（见 §13.3）                       |
| `src/hir/sugar_kinds_test.go`             | `sugarHIRKinds` 声明 + 迁移前普查；C4 门禁以 `t.Skip` 占位                         |

实测：C2 = 66 个 marker（62 base + 4 sugar）；C6 = **532 个语料文件，AST dump 过 `ASTToHIR` 逐字节相同**；C4 普查 = 118 个 std 模块 / 326 205 个 HIR 节点，**`grouped=11021`、`unwrap-assign=0`**（把 §2.3 的 W4/W5 从读码推论变成实测）。

反空洞（注入违例 → 确认 FAIL → 还原 + md5 核验）：删 `baseASTTypes` 里的 `LetStatement` → FAIL；两次 dump 之间改 `Name.Value` → FAIL；临时启用 C4 真实断言 → FAIL（`grouped: 11021`）。**C4 门禁今天必然失败，这正是它非空洞的证明。**

---

## 13. S1a 落地与设计稿修正

### 13.1 S1a — lowering 接缝（默认路径逐字节不变）

- `Parser.SkipLowering`（`parser.go`）：为 true 时 `ParseProgram` 跳过整个 lowering pass，回传**含糖 AST**。
- `(*Parser).LowerSurface(prog)`（`lowering.go`）：**唯一**的 lowering 实现。`ParseProgram` 的默认路径也走它，所以「延后降糖」与「默认降糖」两条路径不可能漂移。
- 门禁 `src/parser/dump/lowering_seam_test.go`：**532 个语料文件**，`SkipLowering + LowerSurface` 与 `ParseProgram` 逐字节相同（含 warnings）。  
  ⚠️ 该门禁**必须**同时断言「共享结果确已降糖」：首版只做比较，结果在反空洞时被抓到——把 `p.lowerProgram` 注释掉后两条路径**都**不降糖，比较自然相等，门禁照样通过。这是「对称比较」的经典空洞。

### 13.2 糖的两级分类（对 §4.1 的实测修正）

`src/parser/sugar_seam_test.go` 用反射在 532 个语料文件上实测（`matchArm` 字段未导出，只有本包内能遍历）：

| 糖类型                     | lowering 前 | parser 降糖后 | 由谁消除             |
| ----------------------- | ---------- | ---------- | ---------------- |
| `SurfaceMatch`          | 697        | 0          | parser（P1 违反 V1） |
| `UnwrapAssignStatement` | 6          | 0          | parser（P1 违反 V1） |
| `GroupedExpression`     | 106        | 106        | **H0**（P2 义务，S2） |
| `MultiAssignStatement`  | 154        | 154        | **H0**（P2 义务，S2） |

即 parser 展开 **703** 处、留给 H0 **260** 处。§4.1 只用散文区分了这两级；现在它是可测的（`sugarTiers` + 门禁 `TestSugarTierClassificationIsComplete`，新增糖类型必须声明属于哪一级）。

### 13.3 设计稿修正：R6 被实测证伪

| 原论断                                    | 实测                                                                                                                      |
| -------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| R6 / V9：checker 就地写 `.Type`，是严格范围的主要代价 | **证伪**。`checker/`、`hir/`、`mir/` 的非测试文件**没有任何** AST 字段写入；`checker.ValidateTypes` 在 **532/532** 个语料程序上 **0 次**改动 AST dump |

推论：前端（S0–S3）今天**已经满足**「`ParseProgram` 返回后不写 AST 节点」。真正的 AST 写点全在 `build/`（H1，§3.4 已排除在两条不变量之外），清单见 §13.5。**R6 应从风险表删除；S5 的规模由 V8 决定，与 checker 无关。**

### 13.4 S0/S1a 期间的新发现

1. **`tohir.go` 没有 `case *SurfaceMatch`** → 该节点一旦抵达 H0 会静默降成 `hir.KUnknown`（`ASTToHIR` 的类型 switch 无 default 诊断）。今天无害只因 `lowering.go` 总是先展开它——而这正是 S1/S2 要改变的。
2. **既有 `astMarkerTypes`（`tohir_exhaustive_test.go`）只扫 `ast.go`**，而 `SurfaceMatch` 声明在 `lowering.go` → 它不在 marker 集内，任何基于该集合的完备性检查都看不到它。新的 `sugarParserSources` 改扫全部非测试 `.go`。
3. **C8 现状**：`dump.go` 有 `MultiAssignStatement` / `UnwrapAssignStatement` / `GroupedExpression` 的 case，**没有 `SurfaceMatch`** → 落到 `default` 打印 `<unknown *parser.SurfaceMatch>`，整棵子树对 dump 不可见。门禁 `TestSugarASTIsDumpable` 记录了这个缺口。  
   ⚠️ **代价比预想大**：`spec_gen_test.go` 是 `scripts/gen_ast_spec.py`（1664 行手写 Go 解析器）**生成**的，补一个 dump case 必须让生成器同时理解该 case 并重新生成，否则 L2 oracle 会静默偏离。**建议单独立项，不要在 S1 里顺带做。**
4. **`src/fmt`、`src/lsp`、`src/cmd/no`、`src/tools` 完全不提 `SurfaceMatch`** → S4（formatter 改读糖节点）是**新增**代码，不是改接线；formatter 现在靠降糖后的形状 + 痕迹字段（§2.4）。
5. **`GroupedExpression` 的归类是定义问题**：§1.3 三条判据都不命中它，但 §5 判 `KGrouped` 为糖。S0 依 §5 归为糖，张力记在登记表注释里。

### 13.5 V8 的真实清单（H1 的 AST 写点）

§2.2 的 V8 只引了 `transpiler.go:1208-1303`，实际是 **9 个文件**：

| 文件                                                                                     | 写什么                                                       |
| -------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| `build/transpiler.go`（`:1024`、`:1207`、`:1230`、`:1253`、`:1299`、`:1304`）                 | `fd.Name = use.Alias`、`ls.IsModuleConst`、`SetSourceFile*` |
| `build/transpiler_stdload.go`（`:369-417`）                                              | `SetModuleOwner` / `SetSourceFileDeep` / `IsModuleConst`  |
| `build/transpiler_mangle.go`（`:76` 注释直写「直接修改 AST」、`:211`、`:215`）                       | 重载改名 `fd.Name` / `ident.Value`                            |
| `build/module_prefix.go`（`:65-73`、`:130`、`:140`、`:234`、`:394`、`:501`、`:816`、`:921`）    | 模块前缀 `Name` / `Value` / `Type`                            |
| `build/transpiler_monomorph.go`（`:37`、`:289`、`:441`、`:463`、`:665-756`、`:1065`、`:1812`） | union / 泛型单态化克隆                                           |
| `build/generic_structs.go`（`:356`、`:440`）                                              | 泛型结构体展开                                                   |
| `build/slice_structs.go`（`:196`、`:432`、`:486`）                                         | 切片结构体展开                                                   |
| `build/transpiler_inject.go`（`:85`、`:99`、`:126`、`:219`）                                | 合成节点注入                                                    |
| `parser/ast.go`（`SetModuleOwner:444`、`SetSourceFile:469`、`SetSourceFileDeep:528`）      | 上述所有 `Set*` 的实现                                           |

### 13.6 S1 剩余工作

S1a 只做了「接缝」。剩下的 S1 = **把 checker 移到 lowering 之前并让它能处理糖节点**。

⚠️ 本小节的初稿把 S1 描述成「全流程风险最高的一步」，并把风险面写成「`hir2mir.go:3455` 靠 if 链形状反推 match、`ValidateUnhandledIndex`、match 穷尽性（`checker/match_exhaustive.go`）都会静默失效」。**§14 的实测部分推翻了这一描述**：

- panic 为 0 ⇒ 失败模式确实是静默的（这部分成立）；
- 但**实际失效的是 8 条规则**（`idxwrite` / `arm-should` / `arm-unreach` / `rngidx` / `nolang-deprecated-call` / `ovf-int-default` / `tcpoxtfd` / `LintIneffectiveOverflow`），**新增误报 7 条**（`6kryrbsq` / `optunuse` / `i3k422u3` / `4bek3xc6` / 及 3 条位移），合计 **12 个 distinct trace**；
- `checker/match_exhaustive.go` **不在**失效清单里；
- `checker/` 早已有 59 处糖节点 handler（两个既有 skip 旗标保留的节点），**缺的只有 `SurfaceMatch`**（出现 0 次）。

⇒ S1 的规模应改判为「补 12 个 trace 的糖分支 + 翻默认形态」，不是「重写 checker」。

**C8（§13.4.3）仍应在 S1 落地之前解决**，否则降糖搬离 `ParseProgram` 的那一刻，`no ast` 与 L2 oracle 就会失效。

---

## 14. S1 风险实测（三形态诊断普查）

§13.6 把 S1 的风险写成散文（「形状键控的规则会静默失效」）。本节把它变成数字与清单。

门禁：`src/parser/dump/checker_on_sugar_test.go` → `TestCheckerShapeAgreementCensus`。入口用 `checker.RunAllLints`（`no vet` 与 LSP **共用**的聚合入口，覆盖 `ValidateTypes`/`Naming`/`UnusedVars`/`UndefinedVars`/`CatchAllArms`/`IndexWrite`… 共 20+ 条规则），语料为 `tests/` 中在三种形态下**都解析干净**的 532 个程序。

三种形态：

| 形态        | 配置                                             | 今日谁在用                                                          |
| --------- | ---------------------------------------------- | -------------------------------------------------------------- |
| `lowered` | 默认                                             | `no build` / `no vet`                                          |
| `surface` | `SkipUnwrapLowering` + `SkipSafeIndexLowering` | **LSP**（`lsp/documents.go:270,276`）、`no fmt`（`fmt/api.go:204`） |
| `sugar`   | `Parser.SkipLowering`（S1a 新增）                  | 无人（S1 的目标形态）                                                   |

| 形态        | 诊断总数    | 非空程序    | panic |
| --------- | ------- | ------- | ----- |
| `lowered` | 455     | 143     | 0     |
| `surface` | 472     | 145     | 0     |
| `sugar`   | **666** | **195** | 0     |

| 配对                | 相异程序    | lost | gained |
| ----------------- | ------- | ---- | ------ |
| `lowered→surface` | **16**  | 9    | 30     |
| `lowered→sugar`   | **122** | 136  | 315    |
| `surface→sugar`   | 124     | 148  | 306    |

### 14.1 checker 在糖上**不崩**，它**静默错**

三种形态 532 个程序 **panic 全为 0**。S1 的失败模式因此是**诊断静默漂移**，正是 R2 的预言——而且「跑得起来」完全不能作为它的门禁。这也说明为什么必须用诊断集合差分（本门禁），而不是「有没有报错」。

### 14.2 S1 的改写清单（`lowered→sugar` 中**停止触发**的诊断）

**12 个 distinct trace**（8 条停止触发、7 条新增触发、其中 3 条两向都有 —— 见 §14.3 的说明）。

停止触发的 8 条：

| trace                    | 次数  | 规则                                | 位置                               |
| ------------------------ | --- | --------------------------------- | -------------------------------- |
| `idxwrite`               | 121 | `ValidateIndexWrite`              | `checker/checker.go:5563`        |
| `arm-should`             | 8   | `ValidateCatchAllArms`            | `checker/match_arm_reach.go:37`  |
| `rngidx`                 | 6   | `ValidateRangeIndexLoops`         | `checker/range_index_lint.go:10` |
| `nolang-deprecated-call` | 4   | `ValidateDeprecatedCalls`         | `checker/deprecated.go:32`       |
| `ovf-int-default`        | 3   | `ValidateIntOverflow`             | `checker/checker.go:3884`        |
| `<no-trace>`             | 2   | `LintIneffectiveOverflow`         | `checker/lint.go:95`             |
| `tcpoxtfd`               | 2   | `ValidateRedundantTypeAnnotation` | `checker/checker.go:3007`        |
| `arm-unreach`            | 1   | match 臂可达性                        | `checker/match_arm_reach.go:36`  |

§13.6 点名担心的 `checker/match_exhaustive.go`（`ValidateNonExhaustiveMatch`）**不在**此列——它按臂条件匹配，不依赖降糖形状。


⚠️ **本节初稿只列了 4 条，是错的。** 成因：普查的逐条报告（`report`）按次数排序并**截断到 12 条**，而 `idxwrite`/`arm-should` 等高次数条目与 `optunuse` 的多行长文案占满了展示位，把 `tcpoxtfd`、`arm-unreach` 这类 1–4 次的尾巴挤掉了。**必须读无截断的 trace 级汇总**（普查新增 `reportTraces`，门禁新增 `logTraceTally`），不能读逐条 top-12。

### 14.3 S1 会**新增**的误报（`lowered→sugar` 中**只**在糖上触发）

| trace             | 次数  | 规则                               | 位置                               |
| ----------------- | --- | -------------------------------- | -------------------------------- |
| `6kryrbsq`        | 244 | `ValidateUnusedVars`             | `checker/checker.go:1546`        |
| `optunuse`        | 75  | option 绑定未被消费                    | `checker/checker.go:4782`        |
| `i3k422u3`        | 9   | `ValidateUnassignedReturns`      | `checker/checker.go:2223`        |
| `rngidx`          | 3   | （与 §14.2 同规则，不同程序）               | `checker/range_index_lint.go:10` |
| `<no-trace>`      | 2   | （与 §14.2 同规则，不同程序）               | `checker/lint.go:95`             |
| `4bek3xc6`        | 1   | `ValidateUndefinedVars`（跨模块限定提示） | `checker/checker.go:7631`        |
| `ovf-int-default` | 1   | （与 §14.2 同规则，不同程序）               | `checker/checker.go:3884`        |

两个大头（`6kryrbsq` 244、`optunuse` 75）**同一根因**：**降糖会合成 `__cap_N` / `__unwrap_N` 脚手架，那些合成节点「消费」了臂绑定**；糖形态下绑定留在 `matchArm`（未导出字段）里，walker 看不见它们被使用，于是判成「未使用 / 未消费」。S1 必须让这些规则认识糖臂绑定（`matchArm` 的 `it` / `ok(v)` / 类型臂绑定），否则用户会看到成片假报。

`rngidx` / `ovf-int-default` / `<no-trace>` 三者在两个方向都出现 ⇒ 它们不是单纯「多报/少报」，而是**诊断位移**（同一规则在糖形态下锚到了不同程序/位置）。S1 的门禁必须比对**位置**，不能只比 trace 集合。

### 14.4 一条**今天就在**的不一致

`lowered→surface` 有 **16 个程序相异**（9 lost / 30 gained）。也就是说 `no vet`/`no build` 与 **LSP** 今天对同一份源码给出的诊断就不一致，**与 S1 无关**。其中一部分是**设计如此**（`checker/compound_assign_lint_test.go:9-13` 明确写了 `+=` 弃用提示只在 surface 可见），另一部分未分类。

⚠️ 这与 §13.3 不矛盾但**是另一个轴**：§13.3 证明的是「checker 不写 AST 字段」（严格范围），本节量的是「checker 对不同形态给出不同诊断」（形状键控）。前者已满足，后者没有。

### 14.5 结论：S1 不是「重写 checker」，但也**不是 6 条规则**

`checker/` **已经**有 **59 处** handler 处理 `GroupedExpression` / `UnwrapAssignStatement`——正是两个既有 skip 旗标保留的节点；且 `checker/index_write_test.go:14` 立有明文规则「surface AST 与降级 AST 两端口径一致」。**从未被喂过的是 `SurfaceMatch`**：它在 `checker/` 里出现 **0 次**。

所以 S1 = **给 checker 补 `SurfaceMatch`**（并确认 `MultiAssignStatement`，它是 H0 级、两形态都在，实测无差异）**+ 把默认形态翻过去**，而不是重写那 40 多个 `Validate*` 入口。

改写面由 §14.2/§14.3 界定：**12 个 trace**，其中：

- **8 条要补糖分支**（否则静默少报）；
- **7 条要补糖臂绑定识别**（否则成片假报，`6kryrbsq` + `optunuse` 合计 319 次）；
- **3 条要处理「位移」而非「有无」**。

### 14.6 门禁

| 门禁                                      | 作用                                                                                          |
| --------------------------------------- | ------------------------------------------------------------------------------------------- |
| `TestCheckerShapeAgreementCensus`       | 普查（census），打印三形态全量对照 + **无截断** trace 汇总                                                     |
| `TestCheckerSugarDriftIsWithinKnownSet` | **硬门禁**：糖形态引起的漂移必须 ⊆ `sugarDriftAllowlist`（本节成书时 12 条，**落地后 10 条**——见 §15）。出现新 trace ⇒ FAIL |

> ⚠️ §14 各表的数字是**落地前**的基线，已被 §15 取代（`sugar` 666 → 483、`lowered→sugar` 相异 122 → 19、`surface→sugar` 124 → 6）。§14 保留作「为什么必须做诊断集合差分」的证据，**不要**拿它当现状。

反空洞：该门禁在初版 6 条 allowlist 下**确实 FAIL 了**（报出 `tcpoxtfd` / `arm-unreach` / `nolang-deprecated-call` / `i3k422u3` / `4bek3xc6` / `ovf-int-default` / `rngidx` 共 7 条未登记漂移），这正是它非空洞的证明，也正是 §14.2 初稿被推翻的原因。另有「低形态零诊断 ⇒ `t.Fatalf` 判空」与「checker panic ⇒ 硬失败」两道保险。

---

## 15. S1 落地（三刀）：共享遍历器 + 两条真根因

§14 把 S1 的改写面量成 **12 个 trace / 124 个程序**。本节记录三刀落地后的实测收敛，以及**两条被 §14 猜错的根因**。

### 15.1 三处改动

| # | 改动                                                                                                                                       | 文件                                                                                            |
| - | ---------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| 1 | **共享糖臂下降器** `walkSurfaceMatchChildren` / `surfaceMatchArmBindings`，接进 **9 个 walker**                                                     | `src/checker/sugar_walk.go`（新）、`checker.go`（×7）、`deprecated.go`（×1）、`range_index_lint.go`（×2） |
| 2 | **`idxLocalTypes` 去耦合**：`SkipLowering` 分支预计算该侧表（`precomputeIdxLocalTypes` + `gatherFuncDefs`）；默认路径仍就地填，**逐字节不变**                         | `src/parser/lowering.go`、`src/parser/parser.go`                                               |
| 3 | **`arm-should` / `arm-unreach` 的糖原生路径**：`collectSugarCatchAllArms` + `CatchAllArm.Sugar/Pos/EndColumn`；并修 `catchAllTailBody` 的 nil/空切片歧义 | `src/checker/match_arm_reach.go`                                                              |

第 1 刀为什么值得抽成共享函数：`checker/` 的 walker 是**静默跳过未知节点**的（`switch` 无 default），所以每漏一个节点类型就少一条诊断、且不报错。把「SurfaceMatch 的直接子节点有哪些」收敛到**一处**，避免 ×9 份各自漂移。`walkSurfaceMatchChildren` 只枚举**直接**子节点（`Matched` + 每臂的 `Condition`/`MultiValuePatterns`/`Body.Statements`），递归交给各 walker 自己；刻意**不**枚举 `MultiOptionPatterns`（`[]string` 变体名不携带引用）。

### 15.2 两条真根因（§14 都猜错了）

**(1) `idxwrite` 的 121 条不是臂体不可见，是侧表只在降糖期被填。**

§14.2 把 12 条 trace 全部归因于「臂体对 walker 不可见」。实测证伪：最小复现取 `tests/mem-safety/element-assign-clone.no`（`s[0] = a`，**不在任何臂里**），三形态 AST dump 逐字节相同，但

| 形态        | `IdxLocalTypes`      | `idxwrite` |
| --------- | -------------------- | ---------- |
| `lowered` | `funcs=6 vars=16`    | 5          |
| `surface` | `funcs=6 vars=16`    | 5          |
| `sugar`   | **`funcs=0 vars=0`** | **0**      |

根因：`IdxLocalTypes` 只在 `lowerProgram` 的反射 walk 里、遇到 `FunctionDefinition` 时由 `collectLocalTypes` 填写。跳过度糖 ⇒ 表为空 ⇒ 所有以它为键的规则（`isContainerWriteBase` → `baseTypeOf`）**静默失效**。而 `collectLocalTypes` **只读** AST——它描述的是一个**源码事实**，不是降糖产物。

> **给 S3 的一般化教训**：只在降糖 pass 内被填的侧表**不是侧表，是降糖产物**。S3 把「类型驱动的降糖」改成「读侧表」之前，必须先证明该侧表在 `SkipLowering` 下存在。

**(2) `arm-should` 的残差是**降糖路径的漏&#x62A5;**，不是糖路径的多报。**

糖原生路径接上后，`arm-unreach` 归零，但 `arm-should` 仍比降糖多 4 条。逐条定位到 `tests/wasi-fib.no:22` 的 `b-opt`（`fib-loop` 内）：

```no
b-opt = t          ; t 是 ?i64，但 resolver 没登记 b-opt 的类型
b-opt: {
    ok -> b = it
    ->
}
```

降糖不报、糖报。对照 `tests/sha256-simplified.no:22` 的 `mod-val-opt`（结构完全相同，`mod-val-opt = (dlen + 9) % 64`）**两边都报**。二分源码后定位到**唯一判别式是主体初始器的形状**：

| 主体初始器               | 降糖后 else 体                      | 降糖报？  |
| ------------------- | ------------------------------- | ----- |
| `b-opt = t`（标识符）    | `Statements == nil`（**nil 切片**） | **否** |
| `b-opt = n - 1`（算术） | `[it = b-opt]`（1 条合成绑定）         | 是     |

机制：通配臂的 `it` 绑定由 `buildItBindingForArm` 合成，而它在 `matchedVarType` 未知时**回 nil**（`lowering.go:3274`）⇒ else 块保持零语句；而 parser 建块时用的是 **nil 切片**而非空切片 ⇒ 旧签名 `catchAllTailBody` 返回 `nil`，把「兜底臂存在但为空」与「根本没有兜底臂」当成同一件事，于是 `if body == nil { continue }` 静默跳过。

⇒ 这是**降糖路径的假阴性**，糖形态才是对的那个。修法是把返回值改成 `([]parser.Statement, bool)`，`found` 显式表示「兜底臂存在」。修完**降糖形态也修好了**，`arm-should` / `arm-unreach` 因此从 `sugarDriftAllowlist` 里**整条消失**（不是加白名单）。

### 15.3 实测效果

| 阶段                             | `lowered` | `surface` | `sugar` | `lowered→sugar` 相异 | `surface→sugar` 相异 |
| ------------------------------ | --------- | --------- | ------- | ------------------ | ------------------ |
| §14 基线                         | 455       | 472       | 666     | **122**            | **124**            |
| + 共享臂下降器（9 个 walker）           | 455       | 472       | 326     | 45                 | 46                 |
| + `idxLocalTypes` 去耦合          | 455       | 472       | 467     | 39                 | 23                 |
| + 糖原生 catch-all 路径             | 455       | 472       | 473     | 27                 | 11                 |
| + `catchAllTailBody` nil/空切片修复 | 455       | 472       | 483     | 20                 | 9                  |
| **本次（含 9 walker 全量接完 + 上述全部）** | **456**   | **477**   | **483** | **19**             | **6**              |

两个大头：`6kryrbsq` 244 → **2**；`optunuse` 75 → **4**。

⚠️ 表中 `lowered` 455 → **456**、`surface` 472 → **477** 是 `catchAllTailBody` 修复**刻意**带来的（新增真阳性），不是漂移。除此之外 `lowered` / `surface` 全程恒定——这是行为中立的对照组：`case *parser.SurfaceMatch` 在降糖形态上**永远不可能命中**。

`lowered→surface` 今天相异 **13** 个程序（§14.4 记的是 16；该数随 `lowered` 一并被上述修复改动）。

### 15.4 剩余的 S1 改写面（实测，`lowered→sugar`）

**LOST 7 条 / 4 个 trace**：`<no-trace>` 2、`ovf-int-default` 2、`tcpoxtfd` 2、`idxwrite` 1（残留于 `tests/range-inline.no`）。  
**GAINED 37 条 / 9 个 trace**：`idxhndld` 20、`optunuse` 4、`ovf-int-default` 3、`rngidx` 3、`6kryrbsq` 2、`<no-trace>` 2、`ovfhndld` 2、`4bek3xc6` 1、`idxwrite` 1。

去重后 **10 个 trace**，全部登记在 `sugarDriftAllowlist`（`checker_on_sugar_test.go`）。其中 5 条（`idxhndld` / `optunuse` / `4bek3xc6` / `ovfhndld` / `6kryrbsq` 的一部分）的**糖计数等于 surface 计数** ⇒ 它们是**既有**的 `lowered`/`surface` 分歧，S1 只是把它暴露出来，不是新造的。

**怎么读「参考形态」**：门禁拿糖去 diff `lowered`（因为 `no vet`/`no build` 出货的是它），但 `lowered` 与 `surface` **今天本来就不一致**（13 个程序）。当某规则的糖计数 = surface 计数 ≠ lowered 计数时，漂移继承自那条既有分歧，不是 S1 新造的。`NOLANG_CENSUS_TRACES=1` 会并排打印三列，是区分两种情形的唯一可靠办法。

### 15.5 门禁现状

| 门禁                                                                   | 状态                                                                                            |
| -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `TestCheckerShapeAgreementCensus`                                    | ✅ 532 程序 / 3 形态 / **0 panic**；`compared < 200` 即 `t.Fatalf`（防语料塌陷）                            |
| `TestCheckerSugarDriftIsWithinKnownSet`                              | ✅ 漂移 ⊆ 10 条登记项；出现新 trace ⇒ FAIL                                                               |
| `TestMatchArmPayloadIsReadableAcrossPackages`                        | ✅ `MatchArm` 跨包可读（S1 的前置）                                                                     |
| `TestSugarArmBodiesAreInvisibleToChecker` / `...AlsoHideIndexWrites` | ✅ 两条最小复现**已自废**（`t.Skip`）——根因修掉后它们不再能失败，证据链保留在测试注释里                                           |
| `TestFormatIdempotent`                                               | ⚠️ **非本改动**：报出的 10 个文件恰是并行 session 正在改的 `src/std/**`；**刻意不重烤 `std_ast.bin`**（否则会把在途编辑烤进 blob） |

回归全绿（`go test`）：`./mir/ ./parser/ ./parser/dump/ ./lexer/ ./hir/ ./checker/ ./build/ ./cmd/... ./lsp/`。

### 15.6 未做（S1 收尾 / 后续）

1. **翻默认形态**：`no vet` / LSP 仍走 `lowered` / `surface`。S1 的目标是让 checker 跑在 `sugar` 上——现在**够格了**（漂移 10 条且已全部归因），但翻旗标会改变 `no vet` 对 **19 个程序**的输出，属用户可见的行为变更，**建议与 S2 同批做**并单独确认。
2. **`arm-should` 的 `sugarDriftAllowlist` 旧注释已删**，连同 `nolang-deprecated-call` / `i3k422u3` 两条同批修好的项，改记在「Retired allowlist entries」块里。
3. **C8 仍独立**（§13.4.3）：`dump.go` 补 `SurfaceMatch` case 必须同时改 `scripts/gen_ast_spec.py` 并重生成 L2 oracle，代价比预想大。
4. S2（降糖折入流式 `hirConv`、删 `lowerer.walk` 的 `v.Set`、删 `tohir.go:828-874` 反塌缩、**给 `tohir.go` 补 `case *SurfaceMatch`**）、S3（类型驱动降糖只读侧表，前置已由 §15.2(1) 钉住）见 §8。S2 的门禁前置已由 §15a 补上。


## 16. 关键文件索引

| 类别         | 文件                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 主改点        | `src/parser/parser.go`（`:1589-1611`）、`src/parser/lowering.go`（4034 行，终将**删除并折入** `tohir.go`）、`src/parser/tohir.go`（1102 行，H0 的宿主）、`src/parser/ast.go`（糖节点定义 + `SetModuleOwner`/`SetSourceFile*`）、`src/parser/resolver.go`（`RTFlag`）、`src/parser/astdiskcache.go`（`ASTVersion`）                                                                                                                                                                                                                                                      |
| checker    | `src/checker/checker.go`、`src/checker/match_exhaustive.go`、`src/checker/match_arm_reach.go`（`catchAllTailBody` 的 nil/空切片歧义，§15.2）、**`src/checker/sugar_walk.go`（新，共享糖臂下降器，§15.1）**、`src/checker/funcargs.go`、`src/checker/lint.go`                                                                                                                                                                                                                                                                                                  |
| 装配与消费方     | `src/build/transpiler.go`（`:1208-1303`、`:1772`）、`src/cmd/no/main.go`（`:1527`、`:1687`）、`src/cmd/no/unwrap_fix.go`、`src/tools/astpatch/main.go`、`src/lsp/*.go`、`src/fmt/*.go`                                                                                                                                                                                                                                                                                                                                                         |
| MIR        | `src/mir/hir2mir.go`（`:1519` `KGrouped`、`:3384` `KMultiAssign`、`:3455` 形状反推、`:5758` `KCond`、`:6566` `KRun`、`:6610` `KTypeMatch`）                                                                                                                                                                                                                                                                                                                                                                                                    |
| 门禁与 oracle | `src/parser/dump/checker_on_sugar_test.go`（S1 普查 + 硬门禁，§14/§15）、`src/parser/dump/ast_freeze_test.go`、`src/parser/dump/lowering_seam_test.go`、`src/parser/sugar_classification_test.go`、`src/parser/sugar_seam_test.go`、`src/hir/sugar_kinds_test.go`、`src/parser/tohir_exhaustive_test.go`、`src/parser/dump/dump_completeness_test.go`、`src/parser/dump/spec_gen_test.go`、`src/hir/golden_test.go`（`:300`、`:322`）、`src/hir/hir.go`（`KindCounts`）、`scripts/mir_golden.sh`、`scripts/selfhost_diff.sh`、`tests/golden/mir-baseline.tsv` |
| 自举         | `compiler/src/ps.no`、`compiler/src/ast.no`、`compiler/src/asp.no`、`docs/design/nolang-selfhost.md`                                                                                                                                                                                                                                                                                                                                                                                                                                   |

---

## 17. S2 已落地（追加，2026-10-16）：H0 降糖的「ERROR 半」通道

> 本节是 §15 之外、本轮实际并入工作树的 S2 收尾。**背景**：§15 的 S2 核心（H0 自己降 `SurfaceMatch`，532/532 HIR 等价）上线后，`hirConv.h0Diags` 只折叠 **WARNINGS** 进 `Program.Warnings`，ERRORS 无人读——一个真实的「静默丢错」洞。本节补上。

### 17.1 漏洞本身（实测，非假设）

`buildMatchDesugar` 在 H0 路径下会产出 ERROR：`empty match arm cannot determine a value (matched expression type is unknown)`（lowering.go:2412）。复现输入（已写入 `src/parser/dump/h0_diag_test.go` 的 `h0DiagErrorSrc`）：

```no
s = 'abc'
main = () () {
    k str = s: {        ; s 是 str，不在 ?T 也不在 union ⇒ 空臂无法推导零值
        ok ->
        err -> 'e'
        nil -> 'f'
    }
    bb = k
}
```

两条流水线对同一份代码、同一段 `buildMatchDesugar`（同样的 `matchedVarType="str"`）的对照：

| 流水线 | 报错位置 | 结果 |
| --- | --- | --- |
| 出货 `ParseProgram()` → `LowerSurface` | `p.saveError` → `p.Errors()` | ✅ `p.Errors()` 非空，`no build` rc=1（实测确认） |
| 目标 `ParseProgram(SkipLowering)` → `ASTToHIR` | `p.saveError` → `hirConv.h0Diags`（**无读者**） | ❌ `p.Errors()` 空、`prog.Warnings` 空、HIR 照产 |

根因：`ParseProgram` 返回即丢弃 `*Parser`，而 `p.diags` 跟着 **Parser 走**。出货路径的 lowering 还在同一个 parser 实例内跑，error 自然落到 `p.Errors()`；目标路径的 lowering 搬到了 H0（`ASTToHIR` 里），error 只能落在 H0 的 `hirConv.h0Diags` 切片上，而 `ASTToHIRWithMap` 当时只读了 **WARNINGS** 半。

### 17.2 修复

1. **`src/parser/ast.go`**：`Program` 增加 `Errors []string`——与 `Warnings` 对称的「解析后错误通道」。理由同上：`Parser.diags` 在 `ParseProgram` 返回后已失效，任何**更晚阶段**的诊断无处可去（文档注释里挂了 17.1 的实测复现）。
2. **`src/parser/tohir.go`**：`ASTToHIRWithMap` 的折叠逻辑由「只收 warnings」改为按 **Severity 分流**——`SeverityWarning` → `prog.Warnings`、`SeverityError` → `prog.Errors`。**严保 severity**：error 绝不能降格进 Warnings（那样会让本该编译失败的程序编译通过）。
3. **`src/parser/astcache_det.go`**：`detProgram` 镜像加 `Errors []string`，保持 gob 确定性（尽管该字段只在 `ASTToHIRWithMap` 写、缓存发生在更早，blob 里恒为空——镜像是为了不让 `TestCachedProgramEncodingIsDeterministic` 有理由特判它）。
4. **`src/build/transpiler.go:1772` 之后**：合并程序降完 HIR 后立刻查 `merged.Errors`，非空则 `no build` 返回 error。这是 **S1 翻转形态后的真守门**（S1 前出货路径走默认 ParseProgram，`merged.Errors` 恒空，本检查不会误伤），但提前装上，避免 S1 上线后发现「目标形态静默编译坏程序」。

### 17.3 门禁（带非空洞校验）

`src/parser/dump/h0_diag_test.go`：

- `TestH0DesugarErrorsReachProgramErrors`：目标形态下 `prog.Errors` 必须非空、且**不能**出现在 `prog.Warnings`。**非空洞**：删掉 `case SeverityError` 分支 ⇒ FAIL；把 error 改成写进 `Warnings` ⇒ FAIL（两道 sabot 实测都拦截）。
- `TestH0DesugarWarningsStillReachProgramWarnings`：warning-only 输入仍只走 `Warnings`、`Errors` 空——防止 severity 被反向混淆。
- `TestH0DesugarCleanInputHasNoDiagnostics`：控制组，良构 match 不产生任何诊断，证明「Errors 空」不是「降糖没跑」。
- `TestShippedCorpusProducesNoH0Errors`：532 文件巡扫，H0 不得拒绝任何出货 parser 接受的输入。

### 17.4 回归

`parser` / `parser/dump` / `hir` / `mir` / `checker` / `build` / `lsp` / `cmd/no` / `lexer` 全绿（后三者需 `-ldflags -X …StdSigKey`）。`fmt` 的 62 个 `src/std/**` 失败是并行 session 正在改写 63 个 std 文件 + 56 个 `.p4bak` 临时文件导致的「读两次不一致」（非 std 文件零失败，且失败集合随 session 写入节奏波动），与本次改动无关。

### 17.5 仍未做（S2 收尾 / S3）

- **S3 的 51/532 分桶**（`typeSynth` 24 + `structural` 24 + `other` 3，见 `s3_breakdown_test.go`）：两个桶都被 `checker/checker.go:5147` 消费 `NullableType.IsInferred` 挡在「`OptionArith`/`IndexOutAssign` 必须先成为解析期糖节点」之后——那是会改变 `no vet` 输出的行为变更，需用户单独确认。
- **`h0Diags` 在 LSP 路径**（`no fmt` 等纯 surface 场景）是否也要翻面读 Errors，留待 S1 翻转形态时一并决定。

## 18. C8 已落地（追加，2026-10-16）：`SurfaceMatch` 的 L2 可见性 + 非变性首次有覆盖

### 18.1 两个盲点，同一个根因

普查用的 `parseCorpus`（`dump_completeness_test.go`）走的是**默认** `ParseProgram`——降完糖的树。糖形态下 `SurfaceMatch` 一个都不剩，于是：

1. **`dump.go` 没有 `case *parser.SurfaceMatch`** ⇒ 糖形态下 168 个语料文件产出 **626 行 `<unknown *parser.SurfaceMatch>`**，整棵臂子树对 `no ast`（L2 自举 oracle）不可见。dump 规则 1 的 `default:` 分支本来就会印 *"Its entire subtree is invisible from here on"*，`TestCorpusHasNoUnknownNodes` 也只在降糖形态上跑，所以**没有任何门禁报过警**。
2. **`desugarSurfaceMatch` 的「不改写 AST」声明零覆盖**。G1 的 C6 半边在 `ast_freeze_test.go` 的 `TestASTIsFrozenAcrossHIRLowering`，但它走降糖语料——match 早就没了，唯一跑在 H0 里的降糖从未被它碰到。**更关键的是它在原理上不可能有覆盖**：修好 1 之前，被改写的臂体两侧都 dump 成 `<unknown *parser.SurfaceMatch>`，`before == after` 恒成立，门禁是个橡皮图章。

### 18.2 修复

1. **`src/parser/dump/dump.go`**：新增 `case *parser.SurfaceMatch` + `matchArm` helper，渲染 `buildMatchDesugar` 读/写的每一个字段（`ValueCtx`、`Body` 都在内）。
   - **位置放在 `default:` 之前（switch 尾端），但这是可读性选择、不是 id 稳定性选择**。⚠️ 本节初版写的是「kind id 由 **switch 源码顺序**推导，插在中间会把后面每个 id 全部重排」——**那是错的，已被实验证伪**（见 §18.8）。两个 emitter 都是 `kinds = sorted(spec.keys())`（`gen_ast_spec.py:636`／`:1403`），id 按 Go 类型名**字母序**分配，与 case 位置完全无关。尾端的真实理由：switch 按语义类别分组（陈述→表达式→字面量→类型→注解），`SurfaceMatch` 是糖节点、没有归属组；且紧邻 `default:`（`<unknown %T>` 分支，正是 `TestCorpusHasNoUnknownNodes` 的触发点）能让最新种类贴着它要防的失败模式。
   - **helper 参数必须是裸标识符**（`for _, a := range n.Arms { d.matchArm(&a, …) }`，不能写 `&n.Arms[i]`）：`gen_ast_spec.py` 的 HELPER 分类器只接受裸标识符作实参，索引形式会被报成 `unclassified for body over Arms`，`SurfaceMatch` 会**静默掉出** L2 spec 表。dump 是只读的，dump 副本等价。
2. **`src/parser/dump/sugar_dump_gate_test.go`（新）**：`parseCorpusSugar`（`SkipLowering` 语料解析）+ `dumpNodeTree`，两道门禁——`TestSugarShapeCorpusHasNoUnknownNodes`（532 个程序、168 个含 `SurfaceMatch`、0 个 unknown）与 `TestASTIsFrozenAcrossHIRLoweringSugar`（532 个程序跨 `ASTToHIR` AST 逐字节不变，168 个确实携带 `SurfaceMatch`）。
   - `dumpNodeTree` 剥掉 `Program{...}` 头行：头行只渲染 `warnings: N`，而 `ASTToHIRWithMap` 把 H0 降糖 WARNINGS 折进 `prog.Warnings` 是 §1.4 明文允许的副表写入。**顺带暴露一个事实**：降糖形态的 C6 孪生门禁 `TestASTIsFrozenAcrossHIRLowering` 比原始 dump 也能过，纯粹是**运气**——降糖语料上 `SurfaceMatch` 在 `ASTToHIR` 之前就没了，没人能触发 H0 诊断。糖形态才是诊断出现的地方。
3. **`src/parser/dump/lowering_seam_test.go`**：C8 豁免清单清空（旧豁免把 `*parser.SurfaceMatch` 列为「dump.go 还渲染不了」；双向门禁主动报出豁免已过期并指示删除）。
4. **`src/parser/lowering.go`**：`desugarSurfaceMatch` 上那段过时的「非变性由某某测试覆盖」注释改为实测结论，并写清为什么它此前**不可能**被覆盖。

### 18.3 spec 重烤，以及**漏掉的孪生钉**

`spec_gen_test.go`（71 → 72 kinds）与 `compiler/src/asp.no` 重生成（`K_SURFACE_MATCH = 65`，id 65–72 位移）。生成器复跑 **0 problems、幂等**（`asp.no` 与 `spec_gen_test.go` 的 sha256 不变，72 kinds == 72 个 dump case）。

**这里漏了一个钉，是被 nolang 侧测试抓到的**：Go 侧 `TestSpecKindCount` 我更新了，但 `compiler/tests/ast-test.no` 里的孪生钉 `runner.eq(asp.KIND-COUNT, 71, …)` 还在。实跑 nolang 测试套件 ⇒ 43 passed / **1 failed**。这说明「两端各有一份 pin、只更新一端」是这次改动真实的风险形状，不是假想。

修法不止是把 71 改成 72。**只改数字仍然留一个死条目的洞**：`kind-name` / `kind-id` 两张表里 `SurfaceMatch` 的条目此前**没有任何人调用**——id 位移错了也没人报。所以补了三条断言（照 `Program` 的既有形状，且遵守该文件注释里记录的 bug #85 家族约束：跨模块常量必须先落局部变量）：

```
ksrf = asp.K_SURFACE_MATCH
runner.eq(ksrf, 65, 'SurfaceMatch is kind 65')
runner.eq(asp.kind-id('SurfaceMatch'), ksrf, 'kind-id(SurfaceMatch) round-trips')
runner.is-true(asp.kind-name(ksrf).eq-full('SurfaceMatch'), 'kind-name(65) = SurfaceMatch')
```

### 18.4 门禁与 sabotage 表

| 门禁 | 覆盖 | 非空洞校验 |
| --- | --- | --- |
| `TestSugarShapeCorpusHasNoUnknownNodes` | 糖形态 532 程序 0 个 unknown 节点类型 | 反空洞：反射 walk 必须含 `reflect.Struct` 分支（首版只处理 Ptr/Interface/Slice ⇒ 恒 0） |
| `TestASTIsFrozenAcrossHIRLoweringSugar` | 糖形态 532 程序跨 `ASTToHIR` AST 不变 | **实测**：往原 arm slice 写回（`sm.Arms[0].ValueCtx = true`，正是上面 `copy` 防住的那件事）⇒ FAIL，指名 `tests/arch.no` 并印 `MatchArm{}` → `MatchArm{valueCtx: true}` |
| `ast-test.no` 的 `KIND-COUNT` / `SurfaceMatch` 钉 | nolang 侧 spec 表 | **实测**：把 `ksrf` 的期望值改成 64 ⇒ FAIL；真实漏钉已自己触发过一次（71 vs 72） |
| `TestSpecKindCount` / `TestSpecMatchesDump`（Go 侧） | spec 与 `dump.go` switch 同步 | 由生成器单次运行保证一致，非手抄 |

### 18.5 L2 自举门禁：实测证明不受影响（不是推断）

先确认前提：`grep -rnE 'SkipLowering' cmd/ lsp/ tools/ fmt/` ⇒ **0 命中**。没有任何 CLI 路径设 `SkipLowering`，所以 `no ast` 永远是「先降糖再 dump」，**永远不产出 `SurfaceMatch`**，`dump.go` 的新 case 在出货路径上是死代码。

再用同树 A/B 把它变成实测而非推断：同一份工作树只换 `dump.go`（工作树版 vs `git show HEAD:src/parser/dump/dump.go`），各建一个 `no`，对全部 **650** 个语料文件（`tests` + `src/std`）跑 `no ast` 比 `(rc, sha256(stdout))`：

```
=== files=650 differ=0 ctl_empty=0 ===
```

`ctl_empty=0` 是非空洞判据（语料路径写错 ⇒ 两边都 0 檔 ⇒ 「0 differ」完美）。**650/650 逐字节相同。**

`asp.no` 位移 8 个 id 的安全性同样是实测而非推断：

- `asp.K_*` 常量在 `asp.no` 之外只以**符号形式**被引用——`ps.no` 206 处、`ast.no` 21 处、`main.no` 1 处、`ast-test.no` 45 处；全树唯一的裸数字 `kind` 字面量都与 AST 种类无关（range 字面量的 ellipsis `kind = 0/1/2`、结构体字段默认值 `kind: 0`）。所以常量换值即自动传播。
- `ps.no:5682` 写明「bin/no 於 parse 期就將 match 展開為 if/else 鏈（非 SurfaceMatch）」⇒ noc **从不产出** `SurfaceMatch`，新条目是防御性完备（`kind-name(65)` 不至于回 `'UNKNOWN-KIND'`）。
- nolang 侧四套测试全绿：`ast-test` 47 / `lx-test` 45 / `tok-test` 62 / `util-test` 27（共 181）。

### 18.6 回归

Go 侧 `parser` / `parser/dump` / `mir` / `hir` / `checker` / `build` / `lsp` / `cmd/no` 全绿（`hir`/`checker`/`build`/`lsp`/`cmd/no` 需 `-ldflags -X …StdSigKey`）。`fmt` 的 62 个 `src/std/**` 失败仍是并行 session 改写 std 文件导致的读两次不一致（零非 std 失败），与本次无关。

### 18.7 仍未做

- `docs/design/nolang-ast-spec.md`：`gen_ast_spec.py` 的 docstring 声称会写这个文件，但它**从来不存在**（实际只写 `asp.no`、`spec_gen_test.go`、`/tmp/ast_spec.json`）。已顺手把 docstring 里的 71 改成 72，但「写一份人类可读 spec」这件事本身没做。
- §17.5 列的两项（S3 分桶的行为变更确认、`h0Diags` 在 LSP 路径）原样保留。

### 18.8 §18.2 的一处**自我证伪**（追加，同日）

§18.2 关于「`case *SurfaceMatch` 必须放在 switch 尾端」的理由是错的，本节记录证伪过程——**结论比原来的说法更有用**。

**原断言（错）**：`gen_ast_spec.py` 的 kind id 由 **switch 源码顺序**推导（`K_<SNAKE> = <1-based index>`），所以插在中间会把后面每个 id 全部重排；放尾端使 diff 纯增量，把自举门禁的爆炸半径压到一个新种类。

**证伪实验**：把 `case *parser.SurfaceMatch` 从尾端（`default:` 之前）整块搬到 switch 中段（`AnnotationEntry` 之前），重跑生成器，比对两个产物：

```
asp.no           : IDENTICAL
spec_gen_test.go : IDENTICAL
```

**根因**：两个 emitter 的第一行都是

```python
kinds = sorted(spec.keys())          # gen_ast_spec.py:636 与 :1403
kidx = {k: i + 1 for i, k in enumerate(kinds)}
```

kind id 按 **Go 类型名的字母序**分配，与 case 在 switch 里的位置**完全无关**。

**真正的后果**：加 `SurfaceMatch` 造成的 id 位移（65–72，各 +1）是**不可避免**的——`"SurfaceMatch"` 在字母序上落在 `"StructLiteral"`(64) 与 `"TaggedEnumDefinition"`(65→66) 之间，**无论 case 写在哪里**。原断言里「放尾端可避免重排」不仅错，还错在把一次不可避免的重排描述成了可避免的，从而掩盖了「加一个种类必然位移其后续种类」这个真实成本。

**为什么仍然保留尾端位置**（理由换成真的）：switch 本身按语义类别分组（陈述 → 表达式 → 字面量 → 类型 → 注解），`SurfaceMatch` 是糖节点、没有归属组；且紧邻 `default:`（`<unknown %T>` 分支，即 `TestCorpusHasNoUnknownNodes` 的触发点）能让最新种类贴着它要防的失败模式。这是**可读性**理由，不是 id 稳定性理由。

**方法論收穫**：这条断言当初是**从生成器的产物形状反推机制**（看到 id 连续、又看到我恰好把 case 放在尾端，就编出了因果），而不是读生成器源码。产物形状与机制在这里恰好一致（tail 放置 + 字母序插入同一位置区间），所以**没有任何测试会抓到它**——`asp.no` 逐字节相同，两端的 pin 也都绿。**唯一能证伪它的方式是搬位置重跑**，也就是「主动构造反例」，而不是「跑干净语料」。这与 §17.3 的 sabotage 纪律是同一条：绿灯不证明机制正确，只证明当前产物与断言不矛盾。

## §19 `IndexExpression.InBounds` 迁入语义 side-table（P1 非变性修复）

`boundsAnalyzer.mark` 曾同时写两处：返回值 `InBoundsIndex.Reads[e] = true`，以及 AST 字段 `e.InBounds = true`。后者使一个宣称返回分析侧表的 pass 在原地改写语法树；HIR 和 D9 place-write lowering 又读这个字段，形成隐藏的数据通道。

本次收敛：

1. **AST 不再承载该事实**：移除 `IndexExpression.InBounds`。`AnalyzeInBoundsIndex` 只产出 `Reads` / removable sets，不写 AST，也不暗中更新 Program.Sem。
2. **证明属于节点语义**：`SemanticContext.NodeSemantics.IndexInBounds` 作为 AST 节点 side-table 字段；`ParseProgram` 在 lowering 前先 `precomputeIdxLocalTypes`，再统一运行 bounds 分析并显式发布证明。默认路径与 `SkipLowering` 路径都准备同一份 side-table，避免 H0 侧缺 `hir.FlagInBounds`。
3. **消费者读同一事实**：`ASTToHIR` 从 `Program.Sem` 读取并写入 `hir.FlagInBounds`；D9 place-write 合成的索引写回节点也在 side-table 中继承原匹配索引的证明。MIR 不再依赖一个可变 AST bool。
4. **快取随语义一起保存**：`detNodeSem` 镜像新增 bool，disk cache 的 `ASTVersion` bump 至 `2026-10-16.3`，旧 blob 不会 HIT 后丢失证明。`InBoundsIndex` 仍保留 pointer-keyed 的分析结果，供 fmt/checker 使用；它和持久化的 HIR 消费 side-table 是两个明确的读取面。
5. **Tier-0 标准库 blob 也要同步重烤**：`go run ./checker/genstdsig` 将 `std_ast.bin` 更新到新 ASTVersion；签名表若内容未变会保留原输出。这个步骤不可只 bump Tier-1 key，否则 checker 的 parse-configuration fingerprint 门禁会将旧 Tier-0 blob 拒绝。

**反空洞门禁**：`TestInBoundsProofLivesInSemanticSideTable` 断言 AST 类型不再有 `InBounds` 字段、解析后 side-table 有正例、HIR 有 flag、参数下标负例无 flag；随后主动清空 side-table，再调用 `AnalyzeInBoundsIndex`，要求返回集合仍命中而 SemanticContext 仍为空——证明分析器既可用又不偷写。`TestUserASTCacheRoundTripPreservesInBoundsProof` 钉住 Tier-1 gob 與 AST-MM 两种 round-trip；`TestD9IndexInBoundsFlag` 检查 surface 节点与 side-table 的匹配。

## §20 `LetStatement` 推断 `?T` 迁入语义 side-table（S3 收尾，P1 非变性修复）

`inferOptionArith`（D6 根算术）与 `synthesizeNullableType`（结构捕获期间合成）两个推断源，过去就地写 `s.Type = &NullableType{IsInferred: true}`，再另写一行 `setVarType(...)`。前者已被 P1 禁止（lowering 不得在 `ParseProgram` 返回后改写 AST）；后者一直在 side-table。S3 把两步合并成单一副表操作，并引入 1-bit 区分写入来源，使下游能在不破坏语法树的前提下读到同样的 `?T` 事实——这是 S0/S1 之后第三次「从 lowering 移除隐式 AST 写」。

本次收敛：

1. **AST 不再承载推断型别**：两个推断源跑完后 `LetStatement.Type` 保持 `nil`。移除就地写 `s.Type = ...`；`*NullableType.IsInferred` 字段仍保留以向后相容（用户写 `?T` 时仍照旧）。
2. **`NodeSemantics.InferredType` 与新字段 `InferredByOptionArith`**：前者存推断出的型别字符串（如 `"?i64"`），后者标志「是否由算术根推断写入」。`maybeCaptureOptionAssign` 的短路检查依后者决定是否要把 `x = a + b` 进一步改写成 `__cap_N`：根算术（true）已收手，结构捕获（false）仍需走结构 desugar。一个 bit 区分两个写入路径，丢失它会让 option-capture pass 重新 desugar 一个本已被算术根推断收手过的绑定、静默剥掉 `?i64` 注解。
3. **`publishInferredType(s, typ, byArith)` 统一入口**：原 `s.Type = ...; setVarType(...)` 两步合并为单一调用，两步不会遗漏。`ParseProgram` 在 lowering 之前先 `prepareInferredTypes`（`lowerer.analysisOnly` 模式）预填 side-table；SkipLowering 路径同样补齐，避免 `ASTToHIR` 在表面形态下丢失 HIR `t=?int` 型别（见 `parser/dump/s3_breakdown_test.go` 的 `typeSynth` 桶 24/532）。
4. **三个消费者读同一事实**：`ASTToHIR` 的 `KLet` 产生从 `Program.Sem` 补读后才走 `typeID`（否则 HIR 给 NoID，`?i64` 凭空消失）；checker 的 `inferredOption` 判定从 `NullableType.IsInferred` 改为 `HasInferredType`；formatter 在 `s.Type == nil` 且 side-table 有推断时跳过 `?` 渲染，与旧版逐字节一致。
5. **`detNodeSem` 镜像新增 bool**：gob Tier-1 cache 随语义一起 round-trip。
   `ASTVersion` bump 至 `2026-10-16.4`，旧 blob HIT 会丢失推断事实——这一步与 §19 的 InBounds bump 是同一条机制、同一类教训。
6. **Tier-0 标准库 blob 同步重烤**：`go run ./checker/genstdsig` 将 `std_ast.bin` 重新生成。**这一步最易遗漏**：ASTVersion 进入 `parser.ExternSignatureFingerprint` hash 输入（`fpWriteString(h, ASTVersion)`），bump 后 `TestStdASTBlobCarriesParseConfig` 会用旧 `Config` 比新 hash、立刻失败。必须 bump ASTVersion + 同步重烤 blob，缺一即被门禁抓。**形状与 §19.5 同**，两条同类教训不必分开教——任何让 `parser.ExternSignatureFingerprint` 变得更敏感的改动都会触发同一道门禁、必须同步重烤。

**反空洞门禁**：`TestInferredTypeLivesInSemanticSideTable`（`src/parser/s3_inferred_side_table_test.go`）5 个 case，每条都实测而非推断：

- **字段存在性反射**：遍历 `LetStatement` 字段名，若发现 `InferredType` 或 `IsInferred` 直接 FAIL。防止「下次有人为了让 checker 简单点又把字段加回去」。
- **`x = a + b` 路径**：parse 后 `x.Type == nil`、`Program.Sem.InferredType(x) == "?i64"`、`InferredByOptionArith == true`。
- **`x = p[i]` 路径**（结构捕获源）：同样 `?i64`，但 `InferredByOptionArith == false`，证明 1-bit 区分真正生效。
- **surface 路径**：`SkipUnwrapLowering + SkipSafeIndexLowering` 下 `x.Type == nil` 仍成立，`prepareInferredTypes` 在 surface 模式下跳过算术根推断（formatter 没有消费者），side-table 留空不写——这条是「AST untouched」门禁而非「side-table populated」门禁，因为后者的成立条件因 surface 模式不同而异。
- **gob Tier-1 round-trip**：`Encode` → `Decode` 后 `InferredType` 与 `InferredByOptionArith` 都保留。

`s3_breakdown_test.go` 的 `typeSynth` 桶 24/532 在补齐 `prepareInferredTypes` 后归零（两形态 `t=?int` 完全对齐）；其他 S3 桶（`structural` 24 / `other` 3）是「option-capture 的额外 `__cap_N` 改写」留下的，是预期内的 lowering 行为，不归本节管。

**回归**：Go 侧 `parser` / `parser/dump` / `mir` / `hir` / `checker` / `build` / `lsp` / `cmd/no` 全绿（后四者需 `-ldflags -X …StdSigKey`）。`fmt` 的 62 个 `src/std/**` 失败仍是并行 session 改写 std 文件导致的读两次不一致，与本节无关。nolang 侧四套测试全绿（`ast-test` 47 / `lx-test` 45 / `tok-test` 62 / `util-test` 27，共 181）。

## §21 S4（进行中）：formatter 改读糖节点 + `no fmt` 翻形态

> 本节记录 S4 的第一刀：**让 formatter 能直接渲染糖 AST**，并把 `no fmt` 的解析形态从
> `surface`（`SkipUnwrapLowering + SkipSafeIndexLowering`）翻到 `sugar`（`SkipLowering`）。
> 后续各刀：LSP（§21.9）、`no fix` 五条规则（§21.11）、`tools/*` 三个工具（§21.12）均已翻完。
> 尚未做的：两个 skip 旗标与 12 个痕迹字段的删除（§4.3）。见 §21.7。

### 21.1 先做尺子，再做改动

S4 的成败判据本来是散文（「formatter 改读糖节点」）。§14 的教训（「必须先有无截断的
trace 汇总，不能读 top-12」）在这里同样适用，故第一件事是把判据变成可测的差分：

`src/fmt/sugar_fmt_ab_test.go` 在 **532 个语料文件**上把同一份源碼跑两种解析形态，
逐字节比 formatter 输出。三条门禁，各有分工：

| 门禁                                            | 作用                                                     |
| --------------------------------------------- | ------------------------------------------------------ |
| `TestSugarFmtVsSurfaceFmtCensus`              | 普查（永不失败）：`identical / differ / 只被某一形态拒绝` 三档计数      |
| `TestSugarFmtMatchesSurfaceFmt`               | 硬门禁：相异必须 ⊆ `sugarFmtKnownDivergence`；且登记项**必须仍在相异**（防死豁免） |
| `TestSugarFmtDivergenceIsAlwaysSurfaceBeingWrong` | 硬门禁：对每条登记项证明**哪一边是错的**（surface 输出无法重解析；sugar 输出可重解析且幂等） |
| `TestNoFmtSugarVsSurfaceCensus` / `TestNoFmtSugarMatchesSurface` | 同一件事，但复现 `no fmt` 的完整管线（含 `checker.OverflowAnnotationRelevance`）——见 §21.4 |

反空洞（实测，不是推断）：

- 拆掉 `fmt/expr.go` 的 `case *parser.SurfaceMatch` ⇒ 两条硬门禁同时 FAIL，**167/532 相异**。
- 把 `StatementsWithIntOverflow` 里的糖臂下钻短路成 `if false && …` ⇒ `no fmt` 门禁 FAIL，**6/532 相异**（basic / database-sql / https-server / match / option / sse）。
- `TestSugarFmtMatchesSurfaceFmt` 的「登记项仍在相异」半边，在 allowlist 键名与语料路径不同前缀时**自己先 FAIL 过一次**（`tests/type-match.no` vs `../../tests/type-match.no`），已改为后缀匹配。

### 21.2 收敛过程（每一次下降都有归因）

| 阶段                                     | 相异      |
| -------------------------------------- | ------- |
| 基线：formatter 完全不认识 `SurfaceMatch`（静默输出空串） | **169** |
| + `case *parser.SurfaceMatch` + `writeMatchArm`  | 43      |
| + `valBranch` 判据修正（见 §21.3(1)）          | 6       |
| + `isBareMatchBody` 认糖臂体 + `hasInferred` 加 `s.Type == nil` 守门（§21.3(2)(3)） | **1**   |

最后剩下的 1 个是 **§21.5 的真实 bug**，不是待修的差异。

### 21.3 实现：一个臂排版器，两个生产者

`fmt/control.go` 把 `writeBareMatchArm` 拆成：

- `bareMatchArm`：臂的「已备妥」描述（主体、已滤掉合成脚手架的语句、wildcard / valBranch、绑定后缀、`writeCond`）；
- `armFromIfExpr`：今日的降糖形态，臂模式从 `MatchedExpr` / `OptionPatterns` / `ValuePatterns` / `RawCond` / `EqualityPattern` 等**痕迹字段反推**；
- `armFromMatchArm`：糖形态，臂模式照 `MatchArm` 语法字段直读；
- `writeArm`：唯一的臂排版实现（内联 / 区块 / 注释位置），两边共用 —— 那是排版决定，与糖无关。

差别因此只剩「模式从哪裡读」。S4 收尾时 `armFromIfExpr` 随降糖形态一起退役。

三个修正点，全部由 A/B 逼出来：

1. **`ok ->` 不是 catch-all**。`wildcard` 初版写成 `IsWildcard && !IsDotVal`，使 val 分支走进 `writeCond` 的 default、`Condition` 为 nil ⇒ 印成 ` ->`。正确判据与降糖形态同构：`wildcard = IsWildcard`、`valBranch = IsDotVal`（降糖那边是 `RTMatchWildcard` + `DotValBody == Consequence`）。
2. **`isBareMatchBody` 必须认 `*SurfaceMatch`**。糖形态下臂体里的裸 match 是 `SurfaceMatch` 而非 `IfExpression`，少了这一条会替巢状 match 多包一层大括号（json.no）。
3. **`hasInferred` 必须同时要求 `s.Type == nil`**（§20 的既有 bug，本节顺手修）。side-table 的推断事实不问源码有没有写型别；`res ?i64 = a[off + 1]` 这种「用户写了、推断也得到」的陈述在糖形态下照样被写推断，于是显式 `?i64` 被当成推断而**整段消失**（option-index.no：`no fmt` 把 `res ?i64 = …` 印成 `res = …`）。

### 21.4 翻形态的第二个前提：checker 也得看得见糖臂体

formatter 的 A/B 绿了**不等于**可以翻形态：`no fmt` 还会用 `checker.OverflowAnnotationRelevance`
算出「哪些 `#{overflow=…}` 是无效的」再删掉它们。实测 532 文件：**6 个相异**，全是
「糖形态把有效的 overflow 注解删了」（basic.no 的 `r = 0 - a`）。

根因正是 §15.2(1) 那类「只在降糖形态下成立」的盲点，换了两个新 walker：

- `StatementsWithIntOverflow`（`checker/checker.go`）：`switch` 没有 `case *parser.SurfaceMatch` ⇒ 臂体对 `relevant` 完全不可见 ⇒ 漏登。
- `OverflowAnnotationRelevance` 的 `collect`：只下钻 `IfExpression`，不下钻糖臂体 ⇒ `governed` 缺项 ⇒ formatter 查到 `gov == nil` ⇒ 误删。这里还有个额外陷阱：**臂体必须以整份语句列表扫描**（新增 `walkSurfaceMatchArmBodies`），逐条回调会把「注解后面第一条陈述是谁」这个列表上下文切碎 —— 这也是为什么不能直接用既有的 `walkSurfaceMatchChildren(…, onStmt)`。

补完后 `no fmt` A/B：**531/532 逐字节相同**，1 个相异即 §21.5。

### 21.5 唯一的相异：糖形态修好了一个「`no fmt -w` 会毁档」的既有 bug

`tests/type-match.no` 的 `r = x: { i32 -> lo  i64 -> hi  -> x }`，今日 `no fmt` 输出：

```
r = : {          ; 主語 x 不見了
    lo           ; 臂模式 i32 -> 不見了
} else: {
    : {
        hi
    } else: { … }
```

**这份输出无法重新解析**（实测：`no fmt` 二次执行报 3 个 `expected expression, got nil`）。
即 `no fmt -w` 会静默毁掉用户的类型 match。糖形态照语法原样输出，逐字还原源码。

这一条因此进 `sugarFmtKnownDivergence`，并由
`TestSugarFmtDivergenceIsAlwaysSurfaceBeingWrong` 把「surface 不可重解析 / sugar 可重解析且幂等」
两半都钉住。**若哪天有人修好了降糖形态的那条路，这道门禁会 FAIL 并要求删除登记项**——
而不是让豁免无限期留着。

### 21.6 已翻的消费方

| 位置                                                    | 改动                              |
| ----------------------------------------------------- | ------------------------------- |
| `src/fmt/api.go` `formatProgramWithLoopStyle`           | `SkipUnwrapLowering + SkipSafeIndexLowering` → `SkipLowering` |
| `src/cmd/no/main.go` `parseProgramForFmt`（`no fmt` 主路径） | 同上（注释写明必须与 formatter 内部解析一致）     |

`no fix` / `fixRedundantTypeSource` / `sourceParses` / hex 字面量扫描 / `unwrap_fix.go`
仍是 surface 形态，**刻意未翻** —— 它们各自跑不同的 checker 规则，需要各自的 A/B（§21.7）。

### 21.9 LSP：三个消费方全部过闸，已翻形态

LSP 只解析一次，那棵树同时喂三个消费方，所以翻形态必须三个都过。各做一把尺子：

| 消费方                 | 门禁                                                                       | 结果                          |
| ------------------- | ------------------------------------------------------------------------ | --------------------------- |
| format-on-save       | 复用 §21.1（`fmt.FormatProgram`）                                              | ✅ 531/532（1 个是修好的 bug）      |
| 符号索引（hover / 跳转 / 找引用） | `src/lsp/sugar_index_ab_test.go` `TestLspIndexSugarMatchesSurface`            | ✅ 532/532                   |
| 实时诊断（`RunAllLints`）   | `src/lsp/sugar_lint_ab_test.go` `TestLspLintsSugarMatchesSurface`            | ✅ 531/532（1 个是糖形态更正确）    |

**索引那一半的修复**：`ASTWalker.walkExpression` 是手写 `switch`、无 default（§15.2(1) 的老
问题），没有 `case *parser.SurfaceMatch` ⇒ 糖形态下 **158/532 文件的符号整体消失**。补上之后
还需登记降糖形态由合成 let 提供、糖形态只活在 `MatchArm` 元资料里的名字：析构绑定
（`ok(v)` / `err(e)` / `rect(w, h)` / 类型臂 `v i32`）、隐含 `it`（`SkipItBinding` 的哨兵臂除外）、
以及 val / raw-cond 臂的 `ok` 关键字。收敛 158 → 0（基线：`case` 拆掉则 FAIL，104/532）。

门禁只比**名字集合**且只查「糖形态不许丢名字」，理由写在测试里：完整条目带 `Type`/`Range`，
而这两者在两形态下**本来就该不同**（降糖那边的绑定是 lowering 的合成 let，带推断型别但区间
是合成的、甚至退化成 `{{3 3} {3 0}}`；糖这边是真实源码位置 + 无型别）。要求逐字节相同等于逼
糖形态去复刻降糖的产物。`__match_subj_L_C` 这类合成临时名则反向允许消失——那正是 S4 的目的。

**诊断那一半的收敛：6/532 → 4/532 → 1/532**，三个根因全部修掉（详见 §21.10）：

1. ~~丢 4 条 hint~~（match-test）——`'->' arm is unreachable` 是**降糖期诊断**，
   已搬到解析期（§21.10）。
2. ~~丢 2 条 error~~（sse / yaml）——`checker/unresolved.go` 两个手写 walker 没有
   `case *parser.SurfaceMatch`，臂里的 `client.is-connected()` / `yaml.parse()` 整条不可见。
3. ~~多 2 条误报~~（net-client / path-char）——`'ok' is defined but never used`。
   裸 match `{ ok -> … }` 里 `ok` 其实是**变量**，parser 在 `parser/match.go:263` 把它当成
   val 分支关键字消费掉、丢弃了源码识别字。这是**糖树的 P1 忠实性 bug**：已在原分支补
   `ma.Condition = &Identifier{...}`（附迁移说明），并用 532 份语料的降糖 dump 快照证明
   降糖树逐字节不变。

剩下 1 个 allowlist 条目（`tests/type-match.no`）方向相反：**糖形态多报 2 条** `ovf-int-default`，
而它是正确的一方——`w i64 -> w + 1` 的 `w` 由臂自己绑成 i64，`w + 1` 确实返回 `?T`；降糖形态
把臂改写后丢了绑定型别，反而看不见。与 §21.5 同型：旧形态是错的一边。门禁因此仍然两向
（丢诊断 = 编辑器里的洞，多诊断 = 用户眼前的误报），并要求每个 allowlist 条目**仍在相异**，
否则视为死豁免而失败；另有两条测试把「哪边错」钉住（`TestSugarValueCtxArmIsNotADiscardedOverflow`
钉住顺手修掉的糖臂下钻误报，`TestSugarArmOverflowDivergenceIsSurfaceUnderReporting` 证明
allowlist 的方向）。

翻形态本身：`src/lsp/documents.go` 由两个局部 skip 旗标改为 `SkipLowering`，与
`fmt/api.go`、`cmd/no/main.go` 三处同形态。另跑一次的纯诊断解析（`collectUnwrapLoweringErrors`，
补 `?=` 的 lowering 期错误）与主解析形态无关，继续保留。

### 21.10 诊断的形态无关化：把 `'->' arm is unreachable` 搬出降糖循环

§21.9 剩下的最后一条「丢诊断」暴露了 §17.1 那一族的真实代价。这个诊断由
`p.saveWarning` 在 `buildMatchDesugar` 展开 match 的循环**内部**发出，于是它天然是
「降糖的副产物」：`SkipLowering` 下整段不执行，消费方翻形态后静默丢失。

**做法**：判定输入从「降糖过程中的 arm 状态」改成「`SurfaceMatch` 自己携带的语法字段」，
新开 `src/parser/match_warn.go`，在 `ParseProgram` 里、降糖**之前**执行——两种形态跑同一段
代码，降糖与否不再影响诊断。走访复用 `lowerer.walk`（`analysisOnly + warnMatchArms`），
因此 `curFuncName` 口径与走訪顺序都和 `lowerSurfaceMatch` 一致（嵌套 match 的诊断先后不乱）。
原 pass **不写回 AST**：列舉載荷析構綁定（`some(v) ->`）的正規化原本是就地改寫
`arm.Condition`，這裡只在區域變數裡記「該臂等同於列出哪個變體」——糖形態的 AST 不容許
被這種診斷污染。降糖路徑那兩處 `saveWarning` 隨之移除（`skipItBinding = true` 是結構性
邏輯，保留）。

复刻的判定链与 `buildMatchDesugar` 逐条对应：无主語（裸 match）与 union 主語（type match）
不走这条路 ⇒ 不报；`itStmt != nil` 等价于 `buildItBinding` 的型别开关（Identifier /
IndexExpression）；raw-cond 臂整段跳过；主體型別取自 `sem.FuncVarType`（函式作用域）、
列舉變體取自 `sem.EnumVariantsOf`；索引主語的 `__match_subj` 歸一化只算型別、不寫 sem。

**验证**（三层，缺一层都不算数）：

| 层 | 尺子 | 结果 |
| -- | ---- | ---- |
| 降糖路径零行为改变 | 532 份语料的 `Program.Warnings` 快照 before/after | 逐字节相同 |
| 两形态不再漂移 | `parser/dump/warnings_shape_test.go` `TestParserWarningsAreShapeIndependent`（532 份两形态逐文件比 warnings） | ✅ 532/532 |
| 诊断没有整体消失 | 同文件 `TestUnreachableArmHintSurvivesBothShapes`（`match-test.no` 必须仍产出 2 条，且两形态一致） | ✅ |

🔴 **反空洞抓到的尺子缺陷**（值得记住）：第一轮反空洞把 `p.warnUnreachableMatchArms` 关掉后，
**两把 sugar-vs-surface 门禁都仍然绿**——因为两种形态的这条 hint 现在同出一个 pass，关掉后
两边一起归零，「一致」依然成立。也就是说**任何只比较两种形态的尺子，都抓不到「两形态共同
回退」**，而这恰好是「诊断原本由 lowering 产生」这种迁移的默认失败模式。补的第三层
（fixture 存在性断言）才能抓到：关掉 pass ⇒ `lowered: expected 2, got 0` ⇒ FAIL。

### 21.7 仍未做

1. **删两个 skip 旗标与 12 个痕迹字段**（§4.3 + §2.4）：`SkipUnwrapLowering` / `SkipSafeIndexLowering` 全部消费方归零之后才能删；`RTFlag` 位与 `ast.go` 的痕迹字段需先确认 `h0Diags`/`tohir.go` 不再读。（2026-10-17 量测更正：这两个旗标在编译器默认路径 `SkipLowering=false` 仍活着——`lowering.go` 的 `?=` / `#{index-out}` 展开依赖它——且被大量「部分形态单测」（`d9_index_write_test` / `hir_equivalence_test` surface 配置 / `compound_assign_test` / `bounds_test` 等）set 来单测局部形态；生产侧仅在 `SkipLowering=true` 时三旗标同设（此时整段 lowering 跳过、二者冗余/死）。⇒ 删它们 = 重写那些单测，**非小改动**，降级为需谨慎、单独立项，不再视为「顺手做」。）
2. **formatter 里剩下的降糖形态代码**（`formatBareMatchExpressionSubj` 的 `RTMatchWrapper` / 索引主体归一化反推、`writeBareMatchArm` 的合成 let 打捞）在糖形态下是死代码，随 1 一起退役。
3. **checker 里其余 5 个手写 walker 的 `SurfaceMatch` case —— ✅ 已完成（2026-10-17）**：`listdir.go`、`option_compare.go`、`range_ellipsis_lint.go`、`funcargs.go`（3 个 expr walker：`checkArgCountInExpr` / `checkCallArgsInExpr` / `filterByExports` 的 inline `walkExpr`）、`deprecated_printf.go` 的 `walkExprForRemovedPrintf` 全部补上 `case *parser.SurfaceMatch:` 走 `walkSurfaceMatchChildren`（option_compare 额外对齐 `itStack` 的 option 內層型別推斷，与 IfExpression case 同语义）。实证：非空洞门禁 `src/checker/sugar_match_lint_test.go`（`TestSugarMatchLint*` 5 个，臂内触发器，断言 sugar==lowered 且非零）全绿；`make test` 全绿；`TestLspLintsSugarMatchesSurface` / `TestCheckerShapeAgreementCensus` 两道 parity gate 仍绿。**注意**：`*parser.SurfaceMatch` 是 Expression 节点（缺 `statementNode`），必须加在 expr walker（经 `ExpressionStatement` 抵达），不能加在 stmt switch——初版误加在 stmt 导致 `impossible type switch case` 编译失败。
   - **RunAllLints 三形态普查纳入 LightweightMode —— ✅ 已完成（2026-10-17）**：`shape` 结构加 `lightweight bool` 字段，新增 `loweredShapeLW()`/`surfaceShapeLW()`/`sugarShapeLW()` 三轻量克隆，`allShapes()` 现扩为 **6 形态**（lowered/surface/sugar + 各自 `-lw`），`shapeCensus` 的 `RunAllLints` 调用加 `LightweightMode: sh.lightweight`。由此 `ckaj3355`（`CheckUnresolvedModuleCalls`，仅在 `LightweightMode` 产出）现被普查覆盖。实测每形态痕量直方图：`ckaj3355` = `lowered 0 / surface 0 / sugar 0 / lowered-lw 82 / surface-lw 82 / sugar-lw 82`——只在三轻量形态出现且计数完全稳定（82/82/82），故**不 drift、无需登记进 `sugarDriftAllowlist`**（`TestCheckerSugarDriftIsWithinKnownSet` 仅 diff `lowered↔sugar`，`ckaj3355` 两侧皆为 0，天然不可见）。随此次普查顺带清理：3 条 stale allowlist 条目（`<no-trace>`/`6kryrbsq`/`ovfhndld`，在 `lowered↔sugar` 已不再 drift）从 `sugarDriftAllowlist` 删除并移入「Retired」记录段；`TestCheckerSugarDriftIsWithinKnownSet` 现 0 stale 警告、仍 PASS。
4. **`match-nonex` 诊断到不了 `no vet` 输出 —— ✅ 已修复（2026-10-17）**（§21.11 末尾曾记此缺口，且 §21.7 旧结论误判为「`RunAllLints(merged)` 对 call-subject 形状返回 0 条，疑与 transpiler 注入 extern/std 签名、`?=` lowering 改写有关」——实测**非**语法/lowering 问题）。
   - **根因（实测定位）**：`match-nonex` 的 `LintResult` 在 `lint.go` 的 19b 段**未填 `File` 字段**；merged / 多文件模式下 `RunAllLints` 的 `singleFile == false`，于 `lint.go` 过滤段 `if !singleFile && r.File == "" { continue }` 将该诊断**静默丢弃**。此 bug 影响**所有正常 `no vet`**（凡引用 std 即 `singleFile=false`），不只 call-subject。设计稿原例 `get-v(): { ok -> … }`（`get-v` 未定义）报的是 compile error 短路，是量测假象，并非规则漏报。
   - **修复**：给 `NonExhaustiveMatch` 加 `File` 字段；`collectMatchRoots` / `collectSugarMatchRoots` 在 walk 进入顶层 `Statement` 时以 `parser.GetSourceFile(st)` 取 SourceFile 并下传为该函数/块内 match 根的 `File`（与 `range_index_lint.go` 的 `curFile` 下传口径一致，复用 transpiler 合并前为顶层 `FunctionDefinition` 设的 `SourceFile`）；两形态收集器产出 `NonExhaustiveMatch{ File: rootFile[…] }`；`ValidateNonExhaustiveMatch` 的 `ValidateResult` 与 `lint.go` 19b 段 `LintResult` 均填 `File: …`。这样 `RunAllLints` 的 `!singleFile && r.File == ""` 过滤不再误杀。
   - **非空洞门禁**：`src/checker/match_exhaustive_test.go` 新增 `TestNonExhaustiveMatchFileAttribution`（断言 `ValidateNonExhaustiveMatch` 回传 `File` 已归因）与 `TestNonExhaustiveMatchVetMergedSurvivesFilter`（构造 merged 双文件程式、跑 `RunAllLints`、断言 `match-nonex` 仍产出且 `File` 非空前）。**反空洞已验证**：短路归因（`rootFile[x] = ""` / `File: u.File` → `File: ""`）两测试均 FAIL。
   - **E2E 复现（修复后）**：`use std/env` + `start = () { env.lookup('host'): { ok -> 1 } }` 跑 `no vet` 现正确报 `/tmp/case3b.no:4:25: [nolang-type-checker] non-exhaustive match on option 'lookup()': missing arm(s): nil, err …[match-nonex]`；修复前为 `No issues found`。
   - 注：糖形态路径（`SkipLowering`）走 `collectSugarMatchRoots` 同样下传 `File`，与降糖路径口径对齐。
5. **surface 形态对「型别解构臂」的迴印 / 解析是坏的 —— ✅ 已诊断（2026-10-17，仅诊断，不动大重构）**：`tests/type-match.no` 与 `src/std/crypto/rsa.no` 在 surface 形态（SkipUnwrapLowering + SkipSafeIndexLowering，非 SkipLowering）下确实坏，糖形态（SkipLowering，即 `no fmt` 现走路径）完好。两处根因已实测定位，**不修，随 §4.3 旗标退役自然消失**。

   - **量测方法**：临时探针以 surface / sugar 两形态解析同份源码，walk AST 直读 `IfExpression.IsTypeMatch/TypePattern/TypeBindingName/MatchedExpr` 与 `SurfaceMatch/MatchArm.IsTypeArm/TypePattern/TypeBindingName`，并收 `p.Errors()`。核心结论（下表）：

     | 源 | 形态 | 结果 | 关键 AST 事实 |
     | --- | --- | --- | --- |
     | `type-match.no`（`x: { v i64 -> …; -> 0 }`） | surface | 0 errors，但 match 已降糖 | 3 个 `IfExpression.IsTypeMatch=true`，`TypePattern="i64"/"i32"/""`、`TypeBindingName="v"`、`MatchedExpr` 非空——**型别信息其实被 lowering 保留在 IfExpression 上** |
     | `type-match.no` | sugar | 0 errors | 2 个 `SurfaceMatch` 型别臂，`MatchArm.TypePattern/TypeBindingName` 完整 |
     | `rsa.no` | surface | **1 error**：line 477 `empty match arm cannot determine a value (matched expression type is unknown)` | `SurfaceMatch` 已不存在（全降糖） |
     | `rsa.no` | sugar | 0 errors | `SurfaceMatch` 存在，空臂合法 |

   - **根因 A（type-match 迴印坏）**：降糖后的型别 match 由 `buildTypeMatchDesugar`（lowering.go:2139-2231）构造成 `IfExpression` 链，并将 `IsTypeMatch` / `TypePattern` / `TypeBindingName` / `MatchedExpr` 写进节点（上表实测存在）。但 surface formatter 的唯一「降糖臂读取器」`armFromIfExpr`（fmt/control.go:406-482）的 `writeCond` switch（445-480）**只认 `RangePattern` / `OptionPatterns` / `ValuePatterns` / `RawCond` / `EqualityPattern`，没有 `IsTypeMatch` / `TypePattern` / `TypeBindingName` 分支**，全部失配就回退到 `formatExpression(e.Condition)`——而型别 match 的 `IfExpression.Condition` 为 `nil`（构造时 `Condition: nil`，lowering.go:2214）。⇒ 型别模式与綁定被静默丢弃，臂只剩 body，输出退化为 `r = : { lo } else: { … }` 这类不可重解析的形状。
     - **补强事实**：`buildTypeMatchDesugar` 构造函数**不设 `RTBareMatch`**（lowering.go:2212-2221 无 `SetRTFlag(..., RTBareMatch)`）。所以当前 `formatIfExpression`（control.go:129）也不会把它路由到 `formatBareMatchExpression`，而是当普通 `cond: { } else: { }` if 链输出——match 语法整体被毁。无论走哪条路径，surface 都还原不出 `v i64 ->`。
     - **对照（糖为何对）**：糖形态 `formatSurfaceMatch` → `armFromMatchArm`（control.go:490-547）的 `case ma.IsTypeArm:`（531-535）直接读 `MatchArm.TypeBindingName` + `TypePattern`，不依赖任何降糖痕迹字段。故 `no fmt`（现强制 `SkipLowering=true`，api.go:209）对 type-match 迴印完好。

   - **根因 B（rsa.no 空臂解析失败）**：surface 解析 rsa.no 在 line 477 报硬错：
     ```
     local-qn = qn-step: {
         ok -> it
         ->
     }
     ```
     `qn-step` 是 `(bits / 32) + 1`（`#{overflow=wrap}`）的结果，按 D1 规则其静态型别为 `?i64`。但 surface 形态跳过 `?=` / 算术 `?T` 推断（`prepareInferredTypes` 在 `SkipUnwrapLowering` 下被跳过，lowering.go:238），`qn-step` 的型别从不写入 `FuncVarTypes` ⇒ `buildMatchDesugar`（lowering.go:2562-2596）为「值语境空臂（`->` 后无语句）」合成零值时，`elemType == ""` ⇒ 直接 `saveError("empty match arm cannot determine a value (matched expression type is unknown)")`。该 match 位于赋值 RHS（值语境），故 `ValueCtx` 为真、命中此分支。sugar 形态（`SkipLowering=true`）整段 lowering 跳过，`SurfaceMatch` 原样保留、空臂合法 ⇒ 0 errors。

   - **结论**：两处坏都是 surface 形态的**固有代价**——它把 match 降糖成 if 链，并跳过糖形态保留的型别推断 / 型别模式管线。`armFromIfExpr` 即便补上 `IsTypeMatch` 分支也只能救 formatter 一侧，rsa.no 的解析失败发生在 lowering 阶段、与 formatter 无关。故正确处置是**不修 surface**，等 §4.3 删两 skip 旗标后整体消失；糖形态已是 `no fmt` / CLI 的正确路径，用户侧不可见。

### 21.11 `no fix` 翻形态：五条全翻（末条 `--fix=match` 也已翻）

按 §21.1 纪律逐个来。**先记一个量测陷阱**：这三条规则（`ValidateIntOverflow` /
`CollectNonExhaustiveMatches` / `ValidateRedundantTypeAnnotation`）在 532 份语料上
**一条都不报**（算术都带注解、match 都穷尽、标注都不冗余）⇒ 拿语料当尺子会得到
`0 == 0` 的空洞「一致」。改用针对性 fixture，且每个 fixture 都必须**真的触发**它的
fixer（`TestNoFixFixturesTriggerTheirFix` 就是防这个的空洞闸门——把 match fixture
的宣告顺序调回「`get-v` 在 `main` 之前」立刻 FAIL，因为那时 resolver 推不出 `v` 的
型别、match 读起来就不是 option match）。

| 位置 | 处理 | 依据 |
| --- | --- | --- |
| `fixRedundantTypeSource`（默认 `no fmt` 的「先去冗余」步） | ✅ 已翻 `SkipLowering` | 规则 532/532 两形态逐条相同；fixture 两形态结果数相同；`cmd/no` 既有回归全绿。这同时消掉 §21.7 第 4 条的不一致（同一份源码在一次 `no fmt` 里被解析两次、两种形态） |
| `sourceParses`（移除后「仍可解析」防御） | ✅ 已翻 `SkipLowering` | 它的契約就是「与 `parseProgramForFmt` 同形態」，后者已是糖形态 |
| `fixMatchArmsInFile`（`--fix=match`） | ✅ 已翻 `SkipLowering` | 规则侧先长糖路径再翻：14 个 match 形状两形态 Pos/End/Subject/Missing 逐字段相同；语料尺子仍空洞（0/532），门禁改立 fixture，详见下文 |
| `fixOverflowInFile`（`--fix=overflow`） | ✅ 已翻 `SkipLowering` | 端到端量测 532/532：报告差异 1 份、输出差异 2 份，全部方向正确且已过安全四验证，详见下文 |
| `fixUnwrapInFile`（`--fix=unwrap`） | ✅ 已翻 `SkipLowering` | 语料尺子空洞（0/532 命中），改立 fixture 门禁；关键是「报告点在 match 臂内」的敏感 fixture，详见下文 |
| `fixRedundantTypeInFileWithVet`（`--fix=redundant` 的 Path 2） | ✅ 已翻 `SkipLowering` | 语料上 Path 2 零到达（9 个会被改的语料档全部由 Path 1 处理）；hex 防御经破坏实验证明仍然有效，详见下文 |
| `unwrap_fix.go` 的 hex 字面量扫描 | ✅ 随 Path 2 一並验证 | `walkHasHexLiteral` 没有 SurfaceMatch 分支，但 `containsHexLiteral` 的**文本扫描兜底**覆蓋整條陳述 span（含臂體）；且實測「值為 match 的冗餘標註」在兩種形態下都不被規則報告，該盲區目前不可達 |

**`--fix=overflow` 的翻转量测**（`fixOverflowSourceShape` 两形态逐檔比對，
门禁 `TestFixOverflowRuleIsShapeParityEndToEnd`）：

- **报告差异 1/532**：`tests/type-match.no`，surface 0 条 / sugar 2 条。方向是
  sugar 补上 surface 的漏报——`w i64 -> w + 1` 的 `w` 型别只存在于糖节点
  `MatchArm.TypeBindingName/TypePattern`，降糖后臂绑定失去型别、合成 let 又带
  `?T` 被豁免 ⇒ surface 看不见这两处。
- **输出差异 2/532**：`type-match.no`（surface 不修、sugar 修，修的正是上述真
  漏报）与 `enum-annotation.no`（两形态都修同一站點、粒度不同——surface 把注解
  插到 match 區塊内部臂模式行之前，sugar 插到整个 match 语句上方）。
- **插入位置的安全性四验证**（`type-match.no`）：① `no build` 修复后文件 EXIT=0；
  ② 运行输出与原版逐位元相同；③ `no fmt` 保留该注解（`StatementsWithIntOverflow`
  的 SurfaceMatch 分支让臂體运算对 relevant 集合可见）⇒ 不会「fmt 删、fix 插」
  churn；④ 重跑幂等。
- **单向不变量**：sugar 绝不能比 surface 少修。语料上逐檔断言（surface 修 4 份：
  `edge-i64` / `enum-annotation` / `hpack` / `test-div-zero`；sugar 修 5 份 =
  前述 4 份 + `type-match`）。
- **`findFixTargetNode` 刻意不下鑽 SurfaceMatch 臂體**：臂内报告的最小可標註节点
  会是臂體语句，而行內臂（`w i64 -> w + 1`）的语句与臂模式同一行——注解插上去
  会落在 match 區塊内、臂模式行之前，该位置能否被 parser/lowering 认可取決于註解
  附著细节，不安全。停在整個 match 语句上方反而正確（語句級註解對整棵子樹生效）。
  语料实测：糖形態下 0 条报告因此找不到目標。
- **反空洞两轮**：① 让糖分支恒不修 ⇒ 三层 FAIL（fixture 不再触发 / 死豁免 / 单向
  不变量点名 4 个文件）；② 删掉一条白名單 ⇒ FAIL 点名 `enum-annotation.no`。

**`--fix=unwrap` 的翻转量测**（`fixUnwrapSourceShape` 两形态逐 fixture 比對，
门禁 `TestFixUnwrapRuleIsShapeParity`）：

- **语料尺子空洞**：`--fix=unwrap` 在 532 份语料上 0 命中（实測 `no fmt
  --fix=unwrap -d tests/` 得 0 檔）⇒ 「两形態一致」会是 0 == 0。门禁改立 fixture，
  且每个 fixture 必须**两种形態下都真的触发**。
- **敏感点是「报告点在 match 臂内」**：optunuse 判「option 綁定後有沒有被消費」，
  而臂體正是最常見的消費場所。surface 形態下 match 被降糖成 if 鏈，
  `collectUnwrapSites` 的 IfExpression 分支看得見臂體；糖形態下 match 是
  SurfaceMatch，walker 必須自己下鑽。三个 fixture：`plain`（臂外，形態不敏感）、
  `arm-block-index` 与 `arm-nested-match`（臂內，敏感）。
- **实测过的无效探针**（都**不**产生 optunuse 报告，記下來省得後人重踩）：
  臂內算術（`ok -> w = it + 1`）、臂內第二條陳述、臂內 for 迴圈——只有臂內的
  **索引讀取**才報。
- **反空洞**：删掉 `collectUnwrapSites` 的 SurfaceMatch 下鑽 ⇒ 两个臂內 fixture
  立刻 FAIL（「sugar shape silently drops the fix」），且 `TestFixUnwrapFixedCompiles`
  的新增两例（`arm_block_index` / `arm_nested_match`，把關改寫結果真能編過）同時 FAIL。

**`--fix=redundant` Path 2（vet 路徑）的翻转量测**：

- **语料上零到达**：`--fix=redundant` 会改的 9 个语料档全部由 Path 1
  （standalone，上一轮已翻）处理，Path 2 在 532 份语料上一次都没跑 ⇒ 翻转的
  语料行为变化为 0（实測确认，非推断）。
- **节点集差异不可达**：两种形態下 `findRedundantTypeNode` 走访找到的「带型別
  註解的 let」節點集有 2/532 差異——surface 多两个 lowering 合成的帶型別 let
  （`match-index-option.no` 70:15 / 85:15，翻轉後不再可見，**更安全**），sugar 多一个
  推斷型別節點（`stmt-boundary.no` 73:13，`y = 1` 這類無手寫標註的 let 在糖形態下
  被 prepareInferredTypes 蓋上推斷型別）。後者要造成損壞需要規則報告恰好落在那個
  推斷位置上，而報告只攜帶**手寫標註**的位置 ⇒ 不可達；且 `computeRedundantTypeRemoval`
  另有兩道護欄（型別必須嚴格在變數名之後、掃到分隔符才刪、跨行即拒）加上 Path 2
  末尾的 re-vet 回歸驗證。
- **hex 防禦仍然有效**（破壞實驗）：把 `computeRedundantTypeRemoval` 的 hex 檢查
  改成 `false &&` ⇒ 既有回歸 `hex literal ... preserved` 立刻 FAIL，證明該防禦在
  翻轉後的糖形態路徑上確實承重；`walkHasHexLiteral` 沒有 SurfaceMatch 分支這一盲區
  由文本扫描兜底覆蓋，且实测不可達（值為 match 的標註兩種形態都不被報告）。

**`--fix=match` 的翻转量测**（规则侧先长糖路径，再翻 `fixMatchArmsInFile`；
门禁 `TestFixMatchRuleIsShapeParity` + `TestFixMatchRuleCorpusShapeParity`）：

- **规则侧的糖路径**（`checker/match_exhaustive.go`）：新增
  `collectSugarNonExhaustiveMatches`，与降糖路径**逐条对应**——
  `sm.Matched != nil` ↔ `MatchedExpr != nil`、`sugarMatchVariants` ↔ `matchVariants`、
  `sm.RBracePos.Line == 0` ↔ `MatchEndPos.Line == 0`、`subjectIsDeclaredEnumExpr` ↔
  `subjectIsDeclaredEnum`、`sugarMatchHasCatchAll` ↔ `matchHasCatchAll`、
  `sm.Pos()`/`sm.RBracePos` ↔ `root.Pos()`/`root.MatchEndPos`。语义助手全部复用
  `match_arm_reach.go` 已有的糖形态版（`collectSugarMatchRoots` /
  `sugarMatchVariants` / `sugarMatchSubjectName`），判定口径不会两套漂移。
  模式照 `collectSugarCatchAllArms`：**并行收集器 + 共用助手**，不让
  `collectMatchRoots` 改回传型别。
- **位置等价性实测**：糖形态 `sm.Pos()` / `sm.EndPos()` / `sm.RBracePos` 与降糖形态
  `root.Pos()` / `root.EndPos()` / `root.MatchEndPos` **逐字段相同**（`RBracePos` 正是
  lowering 转移到 `MatchEndPos` 的那一个位置）。据此把 `NonExhaustiveMatch` 加上
  两形态共同填写的 `Pos` / `End` / `Subject` 字段（`Root` 保留），fixer 只读这三个
  字段 ⇒ 形态无关。
- **两形态不会重复上报**：降糖后 AST 里 `SurfaceMatch` 节点数为 0（实测：default 0、
  surface 0、sugar 1）⇒ 两条收集路径按构造不相交，`CollectNonExhaustiveMatches`
  两条都跑是安全的。
- **14 个 match 形状两形态一致**（`ok-only` / `ok-nil` / `nil-only` / `err-only` /
  `ok-nil-err` / `ok-catchall` / `nil-err-combined` / `call-subject` / `nested-arm` /
  `bare-guard-ok` / `bare-guard-cond` / `plain-value` / `multiline-brace` /
  `module-level`），且**降糖形态（默认全降糖）也给出同一答案** ⇒ 三形态一致。
- **语料尺子仍空洞**：532 份语料两形态各 0 命中（实測）⇒ 端到端语料 sweep 只是
  **前向回归网**（将来谁往 `tests/` 提交一条非穷尽 match 就会被真比到），非空洞性
  由 `matchShapeFixtures` 的 15 个 fixture 承担（8 个必须修、7 个必须不修）。
- **反空洞三轮**：
  ① 把 `sm.Matched == nil` 護欄改成 `false &&` ⇒ `bare-guard-ok` 立刻 FAIL
     （糖形态獨有誤報：裸 guard 區塊 `{ ok -> print(1) }` 的條件恰好叫 `ok`，
     `Matched` 為 nil 而 `sugarMatchVariants` 仍數出 `ok`）；
  ② 把 `sugarMatchHasCatchAll` 的 `!last.IsDotVal` 去掉 ⇒ 5 个 fixture FAIL
     （`ok ->` 與 `->` 兩者都帶 `IsWildcard=true`，只差 `IsDotVal`；去掉區分會把
     `v: { ok -> a }` 誤判成已有兜底臂 ⇒ 靜默漏報 nil/err）；
  ③ 把糖路径的 `End` 從 `sm.RBracePos` 換成 `sm.Pos()` ⇒ 全部 must-fix fixture
     輸出不同（插入點落到 match 起點，臂插到 `{` 之前）。
- **安全验证**（`no fix` 端到端 + CLI）：修复后文件 `no build` EXIT=0、运行输出
  与原版一致（`1`）、二次运行幂等、`no fmt` 不重排插入的 arm（唯一 diff 是两条顶层
  定义间的空行，该偏好在修复前的原文件上同样存在）。
- **既有回归全绿**：`match_fix_test.go` 的 4 个形状回归（缺 nil+err / 只缺 err /
  嵌套 match 缩排 / tab 缩排）、幂等、`LeavesExhaustiveAlone` 三例（含 tagged enum）
  在糖形态下全部通过——这翻的是 `fixMatchArmsInFile` 唯一的形态决策点，行为不变。

**顺带发现的既有缺口 —— ✅ 已修复（2026-10-17，见 §21.7 第 4 条）**：
`match-nonex` 这条诊断**原先到不了 `no vet` 的输出**，根因是 `lint.go` 19b 段
`LintResult` 未填 `File`，在 merged 多文件模式（`singleFile == false`）下被
`!singleFile && r.File == ""` 过滤静默丢弃（影响所有引用 std 的 `no vet`，
非仅 call-subject）。现已给 `NonExhaustiveMatch` 加 `File` 字段并自 `parser.GetSourceFile`
下传归因，修复后 `no vet` 能正确产出 `match-nonex`（E2E 复现见 §21.7 第 4 条）。
旧记的「`f = () { get-v(): { ok -> print(1) } }` 报 `No issues found`」属量测假象：
`get-v` 未定义会先触发 compile error 短路，并非规则漏报；改用自包含 call-subject
源码即可复现并验证修复。`no fix --fix=match` 与 checker 单测不受影响。

### 21.12 `tools/*` 翻糖形态：astpatch / std_qualify / std_methodify

三个工具都是「解析 .no → 走訪 AST 改寫 → formatter 重新打印」，翻到糖形态的共同前提是
**走訪器必须能看见 SurfaceMatch 的臂子树**。但三者的盲区性质不同：

| 工具 | 走訪器 | 糖形态下的盲区 | 修法 |
| --- | --- | --- | --- |
| `astpatch` | 反射 walker（`op.go`） | 不是「缺 case」，而是切片下鑽判据只看「元素是否實作 Node」。`SurfaceMatch.Arms` 是 `[]MatchArm`——**值结构体、刻意不实作 Node** ⇒ 整条被跳过，臂條件/臂體/巢狀 match 对所有 op 不可见，且不报错 | `carriesNodes`（结构体是否承載 Node）+ `walkSlice` 的值结构體分支（只下鑽，就地改寫臂內表達式槽位） |
| `std_qualify` | 手写 `walkExpr` | `switch node.(type)` 没有 `case *parser.SurfaceMatch` ⇒ 臂內的 std 呼叫/常量引用不被加模組前綴，靜默 | `case *parser.SurfaceMatch` + `parser.WalkSurfaceMatchChildrenRW`（RW 版：onExpr 回傳值寫回槽位） |
| `std_methodify` | 手写 `walkExpr` | 同上 ⇒ 臂內的 `x.f(...)` 呼叫不被改寫成方法形態，靜默 | 同 std_qualify |

三个 parse site（`parseSourceShape` / `parseSurfaceShape`）都翻 `SkipLowering`；`surface`
分支只为门禁存在，S4 收尾随旗标一起删。`parseCodeExprShape` / `parseCodeStmtsShape`
（astpatch 的 replace/insert snippet）必須与主樹同形态——这是独立的坑，门禁只用 rename
（不涉 snippet 拼接）绕开它。

**量测与门禁**（每个工具一份 `shape_gate_test.go`；`make test` 已加入 `./tools/...`，
因为 std_qualify 的门禁经 `buildOwnership` 依赖 `CollectStdModuleSignatures`，
**必须注入 StdSigKey**，空 key 会 `os.Exit(1)`）：

| 门禁 | astpatch | std_qualify | std_methodify |
| --- | --- | --- | --- |
| 臂 fixture（兩形態都觸發、輸出逐位元相同） | rename `inner`→`innerX` 伸进 `ok ->` 臂 | `sha1-hex(inner())` → `sha1.sha1-hex(inner())`（owner 唯一） | `math.f64-to-i64(1.5)` → `1.5.to-i64()` |
| 語料網（tests/ 532 份） | **非空洞**：259/532 觸發 rename，計數與輸出全部一致 | **非空洞**：4/532 改寫（fd-newtype 1 / rand-multiassign 10 / std-unix-fs-os-2 1 / x25519-keypair-diff 2），0 差異 | 空洞：0/532 |
| 語料網（src/std 118 份） | — | 0 命中（已全部加過前綴） | 0 命中（已全部方法化過） |
| 附加結構門禁 | 爆炸半徑釘死 = 1（`SurfaceMatch.Arms []parser.MatchArm`）+ `MatchArm` 不實作 Node | — | — |

- **astpatch 語料網为什么非空洞**：对每份语料档取第一个函式定義名跑同一条 rename，比
  op 計數與輸出。259/532 真的發生了改名 ⇒ 这條网**当场就能抓到臂內失明**：破坏实验里
  `yaml.no` 计数 surface=55 / sugar=6、`struct-field-leaf-clone.no` 9 vs 5。
- **astpatch 語料網的前置條件**：两形態輸出都必须能重新解析。糖形態輸出坏 = 正式形態
  的產物坏，直接 FAIL；surface 輸出坏只記帳跳過——那是 surface 形態「降糖 + formatter
  迴印」的**既有缺口**（`tests/type-match.no` 型別解構臂，§21.7 第 5 条），與走訪完整性
  無關。同类既有缺口：`src/std/crypto/rsa.no` 空 match 臂在 surface 形態直接解析失敗，
  两个手写 walker 工具的语料网同样採「糖形態失敗 = FAIL / surface-only 失敗 = 記帳跳過」。
- **反空洞（每个工具至少一轮，全部實測觸發）**：
  ① astpatch：`carriesNodes` 恒 `false` ⇒ 三条门禁全 FAIL（含語料网点名 `yaml.no`）；
     再删掉 `walkSlice` 值結構體分支的 `r.walkStruct(ei)` ⇒ 臂 fixture + 語料 FAIL
     （爆炸半徑门禁按預期仍绿——它量的是判據、不是走訪本身）；
  ② std_qualify：禁用 `case *parser.SurfaceMatch` ⇒ 糖形态計數 1→0、notes 變空、五條斷言全 FAIL；
  ③ std_methodify：同上 ⇒ 計數 1→0、輸出留殘餘模組前綴呼叫。
- **顺带重构**（都是为了可测接缝，無行为变化）：astpatch `applyPatch` 回傳值加 op 計數；
  std_qualify `qualifyFile` 抽出 `qualifyProgram(prog, path, content)`；std_methodify
  `methodifyFile` 抽出 `methodifyProgram(prog, content)`。
- **`WalkSurfaceMatchChildrenRW`**（`parser/sugar_walk.go`，RW 版：onExpr 回傳值寫回槽位）
  现有 3 个工具侧消费方 + checker 侧薄委派；只讀版 `WalkSurfaceMatchChildren` 保留原签名
  （checker 有 12 个呼叫点，改签名会摊大 diff）。

### 21.8 回归

`make test` 全绿（`./tools/...` 于 §21.12 纳入）：`build` / `build/js` / `build/llvmapi` /
`build/wasm` / `mir` / `checker` / `lsp` / `fmt` / `hir` / `mmast` / `package` / `parser` /
`parser/dump` / `tools/astpatch` / `tools/std_methodify` / `tools/std_qualify`。
`go build ./...` 干净；`cmd/no`（注入 key）全绿；`no build tests/basic.no` 冒烟 EXIT=0。
checker 的两处新增 `case` 在降糖形态上**按构造不可能命中**（那里没有 `SurfaceMatch`），
故 `no vet` / `no build` 行为不变。
