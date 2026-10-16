# 方案：Parser 忠实性与 Lowering 唯一性

> 状态：**设计稿（未实施）**。两条不变量的规范性定义、目标架构、阶段划分、关键约束（C1–C10）、门禁（G1–G7）与迁移顺序（S0–S7）已定；迁移尚未动手。  
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

| 术语 | 判据 |
| --- | --- |
| **忠实（faithful）** | 源码中每个语法构造在 AST 中有唯一对应节点；节点字段只能来自 token、位置与子节点，不得包含任何由语义或类型决定的补写。 |
| **糖节点（sugar node）** | AST 节点，其语义不是「它自己」，而是「某个更大基础构造的展开」。命中任一条即为糖：① 在源码中有独立拼写，且含义必须被展开才能执行；② 当前在 parser 内被就地展开；③ 语义需要类型信息才能定案。 |
| **基础 HIR 节点（base node）** | 三条**全中**：① 由 H0 **流式**为**唯一一个** AST 构造 1:1 产出（非回填、非再收拢、非注入）；② MIR 对它**只有一条**降低规则；③ 该规则的类型依赖**只经副表**（declared/inferred type map），不得靠重新推导节点形状获得类型。 |
| **糖 HIR 节点** | 不满足上述任一条者。P2 要求它在 HIR 中**计数为 0**。 |

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

| # | 违反 | 锚点 | 性质 |
| --- | --- | --- | --- |
| V1 | `ParseProgram` 在返回前执行降糖 | `parser.go:1600` → `lowering.go:59` | 语义展开 |
| V2 | 降糖是**反射遍历 + 就地替换** | `lowering.go:73` `l.walk(reflect.ValueOf(prog))`；`:119` `v.Set(...)` | 原地修改 |
| V3 | 降糖**依赖类型** | `lowering.go:493` `inferOptionArith`、`:860` `signedIntOperandType`、`:683` `maybeCaptureOptionAssign`，读 `sem.FuncVarType` / `VarTypes` | 类型相关变换 |
| V4 | **注入合成节点** | `lowering.go:609/683` 合成 `__cap_N` 与 `?T` 注解；`:960` `maybeIndexOutAssign` 合成 match；`lowering.go:81-90` 的 `__opt_N` | 隐式注入 |
| V5 | 复合赋值在**解析期**即降成 `?=` | `stmt.go:850`、`:870-901`；`flushCompoundAssigns:912` 类型驱动 | 语义展开 + 类型驱动 |
| V6 | `->` 管道在**解析期**筑成嵌套 `IfExpression` | `stmt.go:1848`（陈述）、`:1230-1247`（赋值 RHS）、`match.go:253-286`（臂体） | 语义展开 |
| V7 | 类型匹配降成 if 链，原始臂信息只以痕迹字段留存 | `lowering.go:1732` `buildTypeMatchDesugar`；痕迹字段见 `ast.go:1611-1650` | 语义展开 + 信息损失 |
| V8 | 整程序装配写 AST 节点字段 | `parser.SetModuleOwner`/`SetSourceFile`/`SetSourceFileDeep`（`ast.go`）写 `s.ModuleOwner`/`s.SourceFile`；`transpiler.go:1208-1303` 写 `fd.Name = use.Alias`、`ls.Name.Value`、`ls.IsModuleConst` | 违反严格范围 |
| V9 | 反射读 AST 的推断类型字段 | `tohir.go:170` `PopulateInferredTypes` → `nodeType(astNode)` | 违反严格范围（读的是 parse 后被写入的字段） |

### 2.3 P2 的违反点

| # | 违反 | 锚点 | 性质 |
| --- | --- | --- | --- |
| W1 | walk 之后注入合成 HIR | `tohir.go:77` `MonomorphizeGo`、`:81` `DesugarGoInlineAwait`、`:108` `DesugarAsyncGroups` | 非流式变换 |
| W2 | 把降糖产物**再收拢**回语义节点 | `tohir.go:828-874`：类型匹配的 if 链收成 `KTypeMatch` + `KTypeArm` | 非流式、且是「反向降糖」 |
| W3 | MIR 靠**形状**反推语义 | `hir2mir.go:3455` 靠 if 链形状判断「这是不是一个 match」 | 基础 HIR 不成立（判据 ③） |
| W4 | 糖 kind 存活在 HIR | `KUnwrapAssign`（`hir/golden_test.go:322` 的豁免表自述它「永远到不了 HIR」，是**死 kind**） | 残留 |
| W5 | 纯语法节点存活在 HIR | `KGrouped`（括号），MIR 三个 case 只是透明穿透：`hir2mir.go:1519/5743/8162` | 残留 |

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

| 阶段 | 输入 | 输出 | 职责 | **禁止** |
| --- | --- | --- | --- | --- |
| **S0** Lexer | 源字节 | token | 词法 | — |
| **S1** Parser | token | AST（含糖节点） | 只产语法；糖以独立节点存在 | 展开、注入合成节点、读写类型 |
| **S2** Checker | AST（含糖） | 诊断 + **类型副表** | 直接对糖节点做类型/穷尽性/合法性检查 | 写 AST 节点字段（含 `.Type`） |
| **S3** Lowering **H0** | 冻结的含糖 AST + 类型副表 | 基础 HIR | **唯一降糖点**；流式；不原地改 AST | 写 AST；walk 之后注入/再收拢/回填 |
| **S4** 装配 **H1** | 基础 HIR | HIR' | 链接与命名（封闭清单，见 §3.4） | 引入糖 kind |
| **S5** MIR | HIR' | MIR | 基础节点 → MIR | 靠节点形状反推语义 |
| **S6** Codegen | MIR | LLVM IR | — | — |

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

| 项 | 今日位置 | 为什么不能进 H0 |
| --- | --- | --- |
| 模块合并 / `use` 展开 | `build/transpiler.go:1208-1303` | 需要先解析被导入模块，属跨文件装配 |
| 重载改名 / 冲突前缀 | 同上 | 需要全程序名字集合 |
| `co`/`go` 的 `-async` 单态化与**跨函数**着色 | `hir/async_group.go:488` `MonomorphizeGo` | 需要调用图**不动点**，与「流式」在定义上冲突 |
| 协程组展开 | `hir/async_group.go:69` `DesugarAsyncGroups` | 只看**父 block**，流式可见 ⇒ **应下沉到 H0**，H1 只留必须全程序不动点的部分 |

**H1 的允许与禁止**：允许新增函数、改写被调用者引用、填 `Package.Owners`/`FuncOwners`/`GoSpawns`；**禁止引入任何糖 kind**（门禁 G3）。

> 定性依据：H1 不翻译「语法糖」，它做的是「同一个语法构造在另一个模块里的实体是谁」。把它算进 H0 会让 H0 需要全程序不动点，直接摧毁 P2 的流式性质。

---

## 4. 糖节点清单（最小集）

### 4.1 清单

| # | 糖节点 | 现拼写 | 今日展开点 | 今日的违规性质 |
| --- | --- | --- | --- | --- |
| 1 | `MatchExpr`（沿用 `SurfaceMatch`，扩展为**保留类型臂原样**） | 裸 `{ c -> b }`、`x: { … }`、类型匹配 `v: { i64 -> … }` | `lowering.go:1658/1999/1849/1732` | V1+V3+V7 |
| 2 | `UnwrapAssign`（已有 `UnwrapAssignStatement`） | `x ?= e` | `lowering.go:3293` | V1（展开为 `BlockStatement`） |
| 3 | `CompoundAssign` | `a += b`、`a -= b` … | `stmt.go:850/870-901`；`flushCompoundAssigns:912` | V1+V5 |
| 4 | `OptionArith` | `a + b` 等「可能上抛 `?T`」 | `lowering.go:493` + `:683` + `:609` | V3+V4 |
| 5 | `IndexOutAssign` | `a[i] #{index-out=0} = v`、place-match 写入 | `lowering.go:960` | V1+V4 |
| 6 | `MultiAssign`（已有 `MultiAssignStatement`） | `a, b = f()` | 未在 parser 展开 | 只需分类确认 + 保持 1:1 产出 |
| 7 | `Pipeline` | `cond -> A -> B` | `stmt.go:1848`、`:1230-1247`、`match.go:253-286` | V6 |

**为什么是最小集**：这 7 项正是**当前在 parser 内被就地展开**的那批，也就是 V1–V7 的全部现症。字符串插值、`#{}` 注解、方法调用摊平、`co`/协程组等「下游才翻译」的构造，本波**不**升级为糖节点（它们不在 parser 内展开，不违反 P1）。

### 4.2 糖 → 基础 HIR 映射

| 糖节点 | 基础 HIR 展开 |
| --- | --- |
| `MatchExpr`（值匹配） | `KIf` 链 + 臂体 slot；`it` / `ok(v)` 绑定 → `KLet` + `KSlot` |
| `MatchExpr`（类型匹配） | `KTypeMatch` + 每臂 `KTypeArm`（**直接产出**，不再经 if 链往返） |
| `UnwrapAssign` | tag 判定 → `KIf`；ok 臂 `KLet`/`KAssign`；nil/err 臂 `KReturn`（有 `?T` 结果参数时）或默认零值 |
| `CompoundAssign` | 与 `UnwrapAssign` 同形（今日 `+=` 就是先降成 `?=`） |
| `OptionArith` | `KInfix` + option 包装节点；**不合成 `?T` 注解、不建 `__cap_N`**——类型由副表回答 |
| `IndexOutAssign` | 安全索引的 ok 臂 `KIndexStore` + 越界取值语义 |
| `MultiAssign` | 1:1 → `KMultiAssign` |
| `Pipeline` | 短路 `KIf` 链（陈述形态）／`KCond`（取值形态） |

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

| HIR kind | 裁定 | 理由 / 需要的动作 |
| --- | --- | --- |
| `KTypeMatch` / `KTypeArm` | **基础** | 前提：**删除 `tohir.go:828-874` 的再收拢**，改由 H0 从糖节点直接产出。否则它是「非流式变换的产物」，按判据 ① 即为糖 |
| `KCond` | 基础 | `c ? a : b` 是独立源构造，MIR 单规则 `lowerCond`（`hir2mir.go:5758`） |
| `KMultiAssign` | 基础 | `a, b = f()` 1:1 产出，MIR 单规则 `hir2mir.go:3384` |
| `KIter` | 基础 | `i <- [0..n)` 1:1 产出；MIR `hir2mir.go:4204` |
| `KSliceLit` | 基础 | 1:1 产出；MIR `hir2mir.go:5609/5836` |
| `KRun` / `KAwait` | 基础 | 1:1 对应 `run` / `awy`；MIR `hir2mir.go:6566/6568` |
| `KAnnotation` | 基础（**暂**） | 承载平台 / overflow 属性。**第二波**目标：H0 把注解消费成基础 HIR 上的属性位，`KAnnotation` 从 HIR 删除（见 §10） |
| `KGrouped` | **糖（括号）** | 纯括号无语义。H0 抹除。安全性：formatter 读 **AST** 不读 HIR，故抹除对格式化无影响 |
| `KUnwrapAssign` | **糖** | 对应糖节点 `x ?= e`，由 H0 消费。**从 HIR kind 中删除** |

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

| 步 | 内容 | 证伪门禁 | 会静默坏掉的东西 |
| --- | --- | --- | --- |
| **S0** | 只加 instrumentation：`sugarASTTypes`、双向反射测试、dump-equality harness、`sugarHIRKinds`；两条不变量成立前用 `t.Skip` | G1/G2/G3 的 **self-test** 通过；套件 skip | 无（零行为变更） |
| **S1** | 把 `lowerProgram` 从 `ParseProgram`（`parser.go:1600`）摘出；checker 改在**含糖 AST** 上跑；给 checker 补糖节点规则 | G5 + G6/L2 | **最大风险点**：按降糖后形状识别的规则全部失效 —— `hir2mir.go:3455` 靠 if 链形状反推 match、`ValidateUnhandledIndex`、match 穷尽性（`checker/match_exhaustive.go:19`、`:387` 的 Fallback）。必须逐条补 surface case |
| **S2** | 把降糖折进流式 `hirConv`：删 `lowerer.walk` 的 `v.Set`，从糖节点直接产出基础 HIR；删 `tohir.go:828-874` 的再收拢 | G1（AST 仍含糖且冻结）+ G3 + G5 | `lowerer.visited` 的指针去重与 `declaredLocals` 的顺序语义（共享 `MatchedExpr`）必须由「AST 指针 → HIR id 记忆化」等价复现 |
| **S3** | 类型驱动的降糖只读副表（`inferOptionArith` / `signedIntOperandType` / `maybeCaptureOptionAssign`） | G5 + G1（证明没有回写） | inferred 与 declared 的区分（现 `nodeType` 只返回 declared） |
| **S4** | 消费方改读糖节点：`no fmt`（`cmd/no/main.go:1527`）、`no fix`（`:1687`、`unwrap_fix.go:184`）、`astpatch`（`src/tools/astpatch/main.go:125`）、LSP；删两个 skip 开关与全部痕迹字段 | `dump_completeness` / `spec_gen` 528 文件逐字节回放 + `no fmt` 幂等 / reparse | formatter 里反推降糖形状的启发式（如从合成 let 的 `Value` 捞回原主体，`fmt/control.go:198-200`） |
| **S5** | 消灭 parser 之外的 AST 写点：`SetModuleOwner` / `SetSourceFile` / `SetSourceFileDeep` 与 `transpiler.go:1208-1303` 的 `fd.Name` / `ls.Name.Value` / `ls.IsModuleConst` 全部改为副表 / HIR 侧记录；删 `PopulateInferredTypes` 的反射读 | **G1 转为非 skip** | `tohir.go:62-102` 的 `Owners` / `FuncOwners` 键（现在依赖 `GetModuleOwner(stmt)` 读节点字段） |
| **S6** | 重定义 `ASTVersion` 政策（C9），文档化 `HIRVersion` | G7 | 陈旧缓存静默复活旧语义 |
| **S7** | `compiler/` + `nolang-selfhost.md` 同步；跑满五级 | G6 全五级 | L2 oracle 逐字节偏离 |

---

## 9. 风险

| # | 风险 | 缓解 |
| --- | --- | --- |
| R1 | **base/sugar 的判据是定义问题，不是发现问题** | 已给三条判据（§1.3）。**硬前提**：必须先删 `tohir.go:828-874` 的再收拢，否则 `KTypeMatch` 按定义就是糖，P2 必然不成立 |
| R2 | **checker 前移会静默破坏形状键控的规则**（风险最高的单项） | 逐条镜像 surface case；G6/L2 兜底；优先处理 match 穷尽性与 `ValidateUnhandledIndex` |
| R3 | `ASTVersion` 拆键引入陈旧缓存 | 拆键 + G7 |
| R4 | `PopulateInferredTypes` 反射读 AST `.Type` 在严格范围内非法 | checker 返回 type map（`IdxLocalTypes` 已是先例） |
| R5 | 共享节点别名导致流式重复发射 | AST 指针 → HIR id 记忆化 |
| R6 | 严格范围的真实代价：checker 就地写 `.Type` 必须改成副表 | 有界但非平凡；除此之外 S1/S2 落地后其余基本是机械工作 |

---

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

## 12. 关键文件索引

| 类别 | 文件 |
| --- | --- |
| 主改点 | `src/parser/parser.go`（`:1589-1611`）、`src/parser/lowering.go`（4034 行，终将**删除并折入** `tohir.go`）、`src/parser/tohir.go`（1102 行，H0 的宿主）、`src/parser/ast.go`（糖节点定义 + `SetModuleOwner`/`SetSourceFile*`）、`src/parser/resolver.go`（`RTFlag`）、`src/parser/astdiskcache.go`（`ASTVersion`） |
| checker | `src/checker/checker.go`、`src/checker/match_exhaustive.go`、`src/checker/funcargs.go`、`src/checker/lint.go` |
| 装配与消费方 | `src/build/transpiler.go`（`:1208-1303`、`:1772`）、`src/cmd/no/main.go`（`:1527`、`:1687`）、`src/cmd/no/unwrap_fix.go`、`src/tools/astpatch/main.go`、`src/lsp/*.go`、`src/fmt/*.go` |
| MIR | `src/mir/hir2mir.go`（`:1519` `KGrouped`、`:3384` `KMultiAssign`、`:3455` 形状反推、`:5758` `KCond`、`:6566` `KRun`、`:6610` `KTypeMatch`） |
| 门禁与 oracle | `src/parser/tohir_exhaustive_test.go`、`src/parser/dump/dump_completeness_test.go`、`src/parser/dump/spec_gen_test.go`、`src/hir/golden_test.go`（`:300`、`:322`）、`src/hir/hir.go`（`KindCounts`）、`scripts/mir_golden.sh`、`scripts/selfhost_diff.sh`、`tests/golden/mir-baseline.tsv` |
| 自举 | `compiler/src/ps.no`、`compiler/src/ast.no`、`compiler/src/asp.no`、`docs/design/nolang-selfhost.md` |
