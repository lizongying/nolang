# 方案：Parser 忠实性与 Lowering 唯一性

> 状态：**S0 + S1a 已落地**（S0 = instrumentation；S1a = lowering 接缝，默认路径逐字节不变）。S1 的剩余部分（checker 前移）与 S2 起尚未动手，但 S1 的**改写面已实测界定为 12 个 trace**（§14，并附硬门禁）。两条不变量的规范性定义、目标架构、阶段划分、关键约束（C1–C10）、门禁（G1–G7）与迁移顺序（S0–S7）已定。实施记录见 §12，S1a 落地与**设计稿修正**（R6 已被实测证伪）见 §13，S1 风险实测（三形态诊断普查）见 §14。  
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
3. **按需跳过降糖的两个开关**（`parser.go:90-104`）：`SkipUnwrapLowering`、`SkipSafeIndexLowering`；消费方 `no fmt`（`cmd/no/main.go:1527`）、`no fix`（`:1687`、`cmd/no/unwrap_fix.go:184`）、`src/tools/astpatch/main.go:125`。
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
- `SkipUnwrapLowering` / `SkipSafeIndexLowering` 两个开关（`parser.go:90-104`）与其全部消费方（`no fmt`、`no fix`、`astpatch`、LSP）；
- `hir.KUnwrapAssign`（死 kind，`hir/golden_test.go:322` 的豁免条目随之消失）。

`no fmt` / `no fix` / LSP / `astpatch` 改为**直接读糖节点**，一个真值源。

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
