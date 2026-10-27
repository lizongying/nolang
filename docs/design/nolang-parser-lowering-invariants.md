# 方案：Parser 忠实性与 Lowering 唯一性

> 状态（2026-10-24）：**S0–S6 全部落地**；S7（`compiler/` 同步）match 表示已裁定①、缺口 A→H→I 修完、**match-表示残差 0 档**（gap I 已关闭，实测 L2 533/120，见 §13.2）。**Stage 6（Checker 迁 THIR）✅ 完成（6A–6E）**；**6C-optional ✅ 全链完成（① nodeType 去反射 → ② 显式 type map → ③ DivByZero 精化/减报 → ④ StringConcat 补报/增报 → ⑤ §3.3 终态收口）**；**S3-b ✅ 完成（四层等价门禁全绿）**。
>
> 剩余（见 §末 backlog）：**S3-c（`__cap_N` 结构捕获切换，V4 违规收口）**、**S7 正交轨 120 档收敛**。
> **✅ 收官补记（2026-10-27）：上述两项剩余均已关闭，本文 backlog 清零。** ① S3-c 已于 2026-10-25 切换为生产默认（`HIRCaptureEnabled=true`，`src/parser/capturefacts.go`，详见 §13.1 三项决策）；② S7 正交轨——33 档 `cap` 残差随 S3-c 自动消失（§819 预言兑现）、前置 I/O 回归（缺口 4）已修（§854），且后续 s8q→s8y 增量把其余族（`synth`/`idxout`/`inferred`/`generic`/`warn`）全部由自举 parser 侧复现消解：语料 `tests src/std`（655 档）L2 `no ast` 实测 **DIFF=0**（commit `261ab91c`）。逐轮记录见 `docs/design/nolang-selfhost.md` §5.3.4。
> ⚠️ **两者实为同一杠杆**：120 档残差中 33 档（27.5%）直接源于 `__cap_N` 的 AST 注入，S3-c 落地即自动消失（§13.2）。
> 涉及层：parser / hir / checker / mir / fmt / lsp / build / 自举 `compiler/`
> 相关文档：`docs/design/nolang-selfhost.md`、`docs/design/option-unified.md`、`docs/design/nolang-ast-grammar.md`、`docs/design/nolang-mm-ast.md`、**`docs/design/nolang-s1-match-unification.md`（S1 全记录）**、`NOLANG-OWNERSHIP-MODEL.md`
> 行号基准：HEAD `3df86d8f`（2026-10-23）。本仓并行 session 常态在改 `src/parser/**` 与 `src/checker/**`，行号会腐坏，引用前请重新定位。

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
| **THIR（L1，带类型的 HIR）**   | **不新增独立树**：`hir.Package`（节点 arena + Flag 位）＋类型化副表（`Inferred`/`Anns`/`Embeds`/`Owners`/`FuncOwners`，迁移完成后另加 `SrcFile`/`FuncUnions`）整体即 THIR。base HIR 是 H0 到 THIR 之间的转换中间态；THIR 是 checker 与 MIR 之间唯一合法的程序表示。节点类型只经 `Node.Type`（宣告）与 `Package.Inferred`（推断）两槽，不得由形状反推。 |

### 1.4 范围声明

**严格范围（本方案采纳）**：`ParseProgram` 返回之后，**任何阶段不得写入任何 AST 节点字段**。

允许被写的只有与节点解耦的副表：`Sem`（及其 node-keyed 条目）、`Warnings`、`Errors`、`TrailingComments`、以及新的 type map。解析期由 parser 自己记录的形式无关 trivia（如 `OpeningBraceComment`）属于**合法的 parser 输出**，不受此限。

（S5 stage 6 口徑）：此處的「AST 节点」指 **ParseProgram 的输出**。裝配期以 `parser.CloneProgram` 取工作副本後，改名 / 模組前綴 / 呼叫改寫等結構改寫全落在**裝配自建的副本節點**上——與 `transpiler_inject` 合成節點同屬「非 ParseProgram 輸出」，不受此限。ParseProgram 輸出自此永久不可變。

**明确排除在两条不变量之外**：程序级**链接与命名**（模块合并、重载改名、跨函数 `-async` 着色、协程组展开）。它们是「装配」，不是「降糖」，见 §3.4。

---

## 2. 违反点退役总账

§2.2/§2.3 的设计时快照（V1–V9、W1–W5）已全部处置。截至 2026-10-23 的退役状态：

| 违反 | 结局 | 记录 |
| --- | --- | --- |
| V1（解析期降糖） | ✅ 退役（match 一支）：默认路径保留 `SurfaceMatch`；`UnwrapAssign` 仍在 parser 期展开；`GroupedExpression` 由 H0 抹除 | §22、§23 |
| V2（反射遍历 + 就地替换） | ✅ 退役（match 一支） | §22 |
| V3（降糖依赖类型） | ✅ 退役（match 一支） | §22 |
| V4（注入合成节点） | 🔵 部分：`__cap_N`/`?T` 合成仍在（S3 backlog） | §17 |
| V5（复合赋值解析期降糖） | 🔵 维持现状（`flushCompoundAssigns`） | — |
| V6（`->` 管道解析期筑成 if） | 🔵 维持现状 | — |
| V7（痕迹字段） | ✅ **字段已删**（Task #60） | §22 |
| V8（整程序装配写 AST） | ✅ **已收官**：語義事實遷 side-table + 裝配工作副本隔離，pin 30→**0**。ParseProgram 輸出永久不可變 | §13.5（摘要） |
| V9（反射读推断类型字段） | ✅ 退役：InBounds / 推断 `?T` 迁入 side-table | §19、§20 |
| W1（walk 后注入合成 HIR） | 🔵 仍在 H1（§3.4 封闭清单） | — |
| W2（再收拢回语义节点） | ✅ 退役：H0 直出 | §22 |
| W3（MIR 靠形状反推语义） | 🔵 仍在（`hir2mir.go` `blockStartsWithIt`）：降级为已知 wart | §22.6 |
| W4（糖 kind 存活在 HIR） | 🔵 `KUnwrapAssign` 保留（`?=` 产出） | §22.6 |
| W5（纯语法节点存活在 HIR） | ✅ 退役：`KGrouped` H0 抹除 + G3 真门禁 | §23 |

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
- 现在就地写 `.Type` 的做法改为**返回 type map**；`IdxLocalTypes`（`parser.go`）已是这一模式的先例。
- `PopulateInferredTypes`（`tohir.go`）的反射读被删除，改为消费 checker 的 type map。

**S3 Lowering H0**

- 形态：沿 AST 前进，每遇糖节点立即产出其基础 HIR 展开，**不回头改已产出的节点**。
- 共享节点别名改用 **AST 指针 → HIR id 记忆化**，避免重复发射。
- `hir.Builder` arena 天然支持 append-only，**不需要新的 HIR 表示**。

### 3.4 H1 的封闭清单（明确排除在两条不变量之外）

H1 是**链接与命名**，不是降糖：

| 项                                  | 今日位置                                         | 为什么不能进 H0                                         |
| ---------------------------------- | -------------------------------------------- | ------------------------------------------------- |
| 模块合并 / `use` 展开                    | `build/transpiler.go`                        | 需要先解析被导入模块，属跨文件装配                                 |
| 重载改名 / 冲突前缀                        | 同上                                           | 需要全程序名字集合                                         |
| `co` 的 `-async` 单态化与**跨函数**着色          | `hir/async_group.go` `MonomorphizeGo`    | 需要调用图**不动点**，与「流式」在定义上冲突                          |
| 协程组展开                              | `hir/async_group.go` `DesugarAsyncGroups` | 只看**父 block**，流式可见 ⇒ **应下沉到 H0**，H1 只留必须全程序不动点的部分 |

**H1 的允许与禁止**：允许新增函数、改写被调用者引用、填 `Package.Owners`/`FuncOwners`/`GoSpawns`；**禁止引入任何糖 kind**（门禁 G3）。

### 3.5 L1 THIR 分层定义与边界不变量

THIR = `hir.Package` + 类型化副表（`Inferred`/`Anns`/`Embeds`/`Owners`/`FuncOwners`/`GoSpawns`/`Syms`），不新增独立树。三表并立：`FmtTrivia` / `TypeFacts` / `NodeFacts`，宿主 `SemanticContext`，经 `(*Program).TypeFacts()`/`NodeFacts()`/`FmtTrivia` 暴露。（Stage 3 的第四张表 `AnnTable` 已于 2026-10-26 整条删除：`#{...}` 是语法，条目住在 AST 节点上 —— `parser.AnnotatedNode`，见 `parser/annotations.go`。）

**目标管线**：

```
L0  Surface AST（含糖，冻结）
     │  ResolveProgram ── 产出显式输出表（Anns / FmtTrivia / TypeFacts / NodeFacts）
     ▼
Assembly-A  装配（CloneProgram / slice-structs / devirtualize / monomorphize）
     │  产物：merged Program + asmfacts
     ▼
H0  ASTToHIRWithMap ── 降糖诊断 → h0Diags
     ▼
L1  THIR = hir.Package + 类型化副表
     ▼
Checker-on-THIR ──► L2 MIR ──► L3 LLVM
```

**层边界不变量（规范性）**：

| 边界 | 允许携带 | 禁止 |
| --- | --- | --- |
| L0 → Assembly | 语法树 + 解析期 RawAnnotations | 类型结论、改写事实 |
| Assembly → H0 | merged AST + asmfacts | 直接改写节点语义字段（§1.4 禁令维持） |
| H0 → THIR | 带类型节点（`Node.Type`）+ 副表 | AST 指针、formatter trivia |
| Checker → THIR | `Inferred` 回填 + 诊断 | 改 AST、改 HIR 结构 |
| fmt/LSP | L0 + FmtTrivia | 触碰 THIR 副表 |

**迁移分期**：~~FmtTrivia~~ ✅Stage2 → ~~注解类~~ ✅Stage3 → ~~类型类~~ ✅Stage4 → ~~溯源/union/改写事实/embed~~ ✅Stage5 → ~~checker 影子双跑/切换~~ ✅Stage6 → ~~删 NodeSemantics 空壳~~ ✅Stage7

**Stage 2–5/7 落地摘要**：
- **Stage 2**：`RTFlags`/`OpeningBraceComment` → `parser.FmtTrivia`。不进 blob（自带空 GobEncode/Decode）。`ASTVersion` → `2026-10-23.1`。
- **Stage 3**：`RawAnnotations`/`Annotations`/`PlatformKeys`/`GenericParams` → `parser.AnnTable`。**必须进 blob**（`CachedProgram.Anns`）。`ASTVersion` → `2026-10-23.2`。setter 不对称：`SetInferred("")` 不建条目。
  - **已撤销（2026-10-26）**：`AnnTable` 整条删除，条目回到 AST 节点（`AnnotatedNode`），`CachedProgram` 只剩 `{Prog, TypeFacts, NodeFacts}`，`ASTVersion` → `2026-10-26.1`。撤销理由：`attachAnnotations` 明确「不再挂载到 AST 节点上」⇒ **尾随注解**（`stmt #{...}`）只存在于表里、AST 完全不承载 ⇒ 任何「从 Prog 重建副表」的路径都会静默丢掉它们（全库 59 处尾随注解，18 处 `#{index-out}`）。
- **Stage 4**：`InferredType`/`InferredByOptionArith`/`TypeOverride` → `parser.TypeFacts`。三字段全镜像进 blob。`ASTVersion` → `2026-10-23.3`。setter 不对称：`SetByOptionArith(n,false)` 必建条目。
- **Stage 5**：剩餘 9 欄 → `parser.NodeFacts`。9 欄全鏡像（`EmbedFiles` 排序影子）。setter 不对称：`SetModuleConst(n,false)`/`SetIndexInBounds(n,false)` 只清不建。`ASTVersion` → `2026-10-23.4`。pin 30→0（装配工作副本隔离 `parser.CloneProgram`）。
- **Stage 7**：`NodeSemantics` 空殼 + 整條 nodeSem 通道全刪。`ASTVersion` → `2026-10-23.5`。

**Stage 6（Checker 迁 THIR）落地摘要**：
- **6A**：`lint.go` 按 lintSteps 表拆分（39 步，surface=true 27 步 / false 12 步）。切分等价门禁：`RunAllLints ≡ RunSurfaceLints ⊎ runTHIRStageChecks`。
- **6B**：THIRView + declTypeOf 基础设施（TypeOf / FuncSigs / FuncResultTypes）。
- **6C**：14 对影子分组迁移（walker 结构保留 AST + 糖 case，类型结论改读 THIR 副表 declTypeOf）。全语料多重集全等。
- **6D**：切换——lint 12 调用点 + transpiler 硬错误块 5 validator 改调 THIR 变体。`BuildTHIRView` 含 H0 副作用全隔离（h0Diags 回滚 + Sem 名称级映射回滚）。零行为变化。
- **6E**：复归收口——删临时指纹脚手架（lint_fp_temp_test.go），门禁收敛为 build/vet + checker 全量 + G7 + 切分等价/THIR 影子。
- **6C-optional**：
  - 第一步 `nodeType` 去反射（`parser/tohir.go`）：`reflect` 读 `.Type` 改显式 type switch（10 个带 Type 字段的节点类型），零行为变化。
  - 第二步「显式 type map」（`checker/thir_exprtypes.go`）：`inferExprType` 结果持久化成节点键型别表，`THIRView.TypeOf` 增第三档查询 ⇒ 表达式节点（Identifier / Infix / Call / Dot / Index …）首次可答（此前恒 `""`）。
    - **不写 `pkg.Inferred`**：MIR 有 4 处消费点（hir2mir.go KCast 1540 / inferredTypeOf 1641 / letTypeRaw 2038 / lowerMapLit 7050），塞表达式型别会把 codegen 的 void 兜底翻成型别 —— 超出本批范围。
    - **惰性构建**（`sync.Once`）：只在真有调用方查表达式时付一次全树代价；表内只含表达式键，宣告节点在 `TypeOf` 第三档前被 `parser.Expression` 断言挡掉 ⇒ 生产路径（declTypeOf 只查宣告节点）零次构建、零行为变化。
      - **§3.3 终态已收口**：`PopulateInferredTypes` 不再逐节点解析 AST —— 型别结论全部来自 `effectiveTypeMap`（显式 type map，解析序 `TypeOverride > 宣告 .Type`），函数体是纯搬运。表达式级**推断**型别刻意不入该表（活在 `checker.THIRView.ExprTypes`，经 `TypeOf` 第三档查询），否则 MIR 的 `inferredTypeOf` 兜底会被翻转（codegen 级行为变更）。

---

## 4. 糖节点清单（最小集）

### 4.1 清单

| # | 糖节点                                           | 现拼写                                              | 今日展开点                    | 违规性质                  |
| - | --------------------------------------------- | ------------------------------------------------ | ------------------------ | ------------------------ |
| 1 | `MatchExpr`（沿用 `SurfaceMatch`） | 裸 `{ c -> b }`、`x: { … }`、类型匹配 | H0 三路直出（S-a/S-b/S-c） | ~~V1+V3+V7~~ ✅ |
| 2 | `UnwrapAssign`（已有 `UnwrapAssignStatement`）    | `x ?= e`                                         | parser 期展开 | V1 |
| 3 | `CompoundAssign`                              | `a += b` …                              | `flushCompoundAssigns` | V1+V5 |
| 4 | `OptionArith`                                 | `a + b` 等「可能上抛 `?T`」                             | `lowering.go` | V3+V4 |
| 5 | `IndexOutAssign`                              | `a[i] #{index-out=0} = v`                        | 共享 resolve 层 | V1+V4 |
| 6 | `MultiAssign`（已有 `MultiAssignStatement`）      | `a, b = f()`                                     | H0 1:1 | — |
| 7 | `Pipeline`                                    | `cond -> A -> B`                                 | parser 期筑 if 链 | V6 |

**为什么是最小集**：这 7 项正是**当前在 parser 内被就地展开**的那批。字符串插值、`#{}` 注解、方法调用摊平、`co`/协程组等「下游才翻译」的构造，本波**不**升级为糖节点。

### 4.2 糖 → 基础 HIR 映射

| 糖节点               | 基础 HIR 展开                                                                   |
| ----------------- | --------------------------------------------------------------------------- |
| `MatchExpr`（值匹配）  | `KIf` 链 + 臂体 slot；`it` / `ok(v)` 绑定 → `KLet` + `KSlot`                      |
| `MatchExpr`（类型匹配） | `KTypeMatch` + 每臂 `KTypeArm`（H0 直出）                                           |
| `UnwrapAssign`    | tag 判定 → `KIf`；ok 臂 `KLet`/`KAssign`；nil/err 臂 `KReturn` 或默认零值               |
| `CompoundAssign`  | 与 `UnwrapAssign` 同形                                                        |
| `OptionArith`     | `KInfix` + option 包装节点；类型由副表回答                                              |
| `IndexOutAssign`  | 安全索引的 ok 臂 `KIndexStore` + 越界取值语义                                           |
| `MultiAssign`     | 1:1 → `KMultiAssign`                                                        |
| `Pipeline`        | 短路 `KIf` 链（陈述形态）／`KCond`（取值形态）                                              |

### 4.3 删除记录（已全部落地）

- **12 痕迹字段** ✅ 已删（Task #60 step 6）
- **两个 skip 旗标** ✅ 已删（`583791e1`，2026-10-17）
- **formatter 降糖代码** ✅ 已删（Task #60 step 4）
- **`hir.KUnwrapAssign`** 🔵 保留（`?=` 产出，非死 kind）
- **`RTFlag` 位** 🔵 保留（通用 infra + js/wasm 降糖路径）

---

## 5. base / sugar HIR 分类裁定（规范性）

| HIR kind                  | 裁定        | 理由 |
| ------------------------- | --------- | --- |
| `KTypeMatch` / `KTypeArm` | **基础**    | H0 直出（S-a） |
| `KCond`                   | 基础        | 独立源构造，MIR 单规则 |
| `KMultiAssign`            | 基础        | 1:1 产出 |
| `KIter`                   | 基础        | 1:1 产出 |
| `KSliceLit`               | 基础        | 1:1 产出 |
| `KRun` / `KAwait`         | 基础        | 1:1 对应 `run` / `awy` |
| `KAnnotation`             | 基础（**暂**） | 第二波目标：H0 消费成属性位后从 HIR 删除（§10） |
| `KGrouped`                | **糖**     | 纯括号无语义。H0 抹除（✅ 已落地，§23） |
| `KUnwrapAssign`           | **糖**     | 保留（`?=` 产出） |

> 判据 ③ 的措辞：「类型依赖**只经副表**，不得靠重新推导节点形状获得」——被禁止的是 W3，不是「读类型」。

---

## 6. 关键约束

- **C1** `ParseProgram` 返回后，无任何阶段写 AST 节点字段。（P1 严格范围）
- **C2** 每个 AST 类型必须被显式分类为 base 或 sugar，不得有未分类项。
- **C3** 每个 sugar AST 类型必须被 H0 消费；每个 H0 `case` 必须对应已登记的 sugar 类型。
- **C4** H0 之后、H1 之前，HIR 中不得存在糖 kind。
- **C5** H0 之后新增的 HIR 节点只能来自 H1 的封闭清单，且不得是糖 kind。
- **C6** H0 不修改 AST：AST dump 在 H0 前后逐字节相等。
- **C7** 糖节点必须携带完整表面信息，使 formatter 与 `no fix` 无需任何痕迹字段。
- **C8** `no ast` 必须能 dump 全部糖节点。
- **C9** `ASTVersion` 语义：缓存键只覆盖表面语法 + 解析期副表；降糖形状变化不 bump。降糖侧由 `hir.HIRVersion` 承担。
- **C10** `compiler/` 自举树与 `docs/design/nolang-selfhost.md` 必须同步。

---

## 7. 门禁

每条约束配一个**可失败**的门禁，且每条门禁都有**反空洞措施**。

### G1 ← C1 / C6 — AST 冻结

`TestASTIsFrozenAfterParse`：ParseProgram 返回点 + post-checker / post-H0 / post-H1 各取 dump，逐字节比较。反空洞：人为改字段必变 + dump 行数下界。

### G2 ← C2 / C3 — 糖节点两向分类

`sugarASTTypes` 登记表 + 双向完备性。反空洞：下界 + 登记项活性。

### G3 ← C4 — HIR 无糖

✅ **已启用**。`sugarHIRKinds` 计数为 0（G3.1）+ 抹除探针（G3.2）。破坏实验实证判别力。

### G4 ← C5 — H1 增量可解释

H0 快照与最终 `pkg.Nodes` 差值可被 H1 封闭清单逐条解释。

### G5 — 行为等价

`scripts/mir_golden.sh`：逐文件比 `(rc, sha256(stdout))`，绝不比 stderr。

### G6 — 自举

`scripts/selfhost_diff.sh` 五级门禁，重点 L2（逐字节 AST oracle）。

### G7 ← C9 — 缓存

✅ **已落地**。481 档 `tests/*.no` HIR 等价 oracle（`parser/hir_cache_equivalence_test.go`）+ 反空洞三件。破坏实验实证。

---

## 8. 迁移顺序

| 步      | 内容 | 状态 |
| ------ | --- | --- |
| **S0** | instrumentation | ✅ |
| **S1** | match 表示统一到 `SurfaceMatch` | ✅（§22） |
| **S2** | H0 流式降糖（match + KGrouped） | ✅（§22.5、§23） |
| **S3** | 类型驱动降糖只读副表 | 🔵 部分（`__cap_N` backlog） |
| **S4** | 消费方翻糖形态 | ✅（§21） |
| **S5** | 消灭 AST 写点 | ✅（pin 0） |
| **S6** | `ASTVersion` 政策 + G7 | ✅ |
| **S7** | `compiler/` 同步 | 🔵 match 已裁定①，残差 6 档 |

---

## 9. 风险实测结局

| # | 风险 | 实测结局 |
| --- | --- | --- |
| R1 | base/sugar 判据是定义问题 | ✅ 再收拢已删，H0 直出 |
| R2 | checker 前移破坏形状键控规则 | ✅ 已量测收敛（§14/§15），最终默认路径保留糖树 |
| R3 | `ASTVersion` 拆键引入陈旧缓存 | ✅ G7 在 |
| R4 | `PopulateInferredTypes` 反射读非法 | ✅ side-table 化 |
| R5 | 共享节点别名导致重复发射 | ✅ 两阶段发射 |
| R6 | checker 就地写 `.Type` | ❌ **被实测证伪**：checker 零 `.Type` 写入；真正写点在 `build/`（S5） |

---

## 10. 未决决策

1. **`co` 单态化是否算「程序级装配」** —— 判**是**（§3.4），在 H1 封闭清单。
2. **`KGrouped` 是否在 H0 抹除** —— ✅ **定案并落地**（§23）。
3. **`#{...}` 注解是否要在 HIR 中消失** —— 现 `KAnnotation` 存活。第二波目标。

---

## 11. 与自举 `compiler/` 的同步要求（C10）

### 11.1 S7 现状

- **match 表示已裁定①**（2026-10-21）：`ps.no` 改产 `SurfaceMatch`（`PS-SURFACE-MATCH=1`）。
- **缺口 A→H→I 全鏈修完**，L2 385/268 → 510/143 → **533/120**（2026-10-24 實測），match-表示殘差 **193 → 6 → 0 檔**（gap I：複雜主體 match 陳述 + str/txt 反向多臂，**已關閉**；殘餘 120 檔全為正交軌，見 §13.2）。
- **`compiler/` 构建破损**（`762ea045`）已于 2026-10-20 修复（8 处语言层修法 + CI 补 `no build compiler` 闸）。
- **挂起归零**（s58/s59 两个独立根因已修）。
- **陣列／切片接收者**（s64/s65）已修。
- **剩餘分歧成分**：264 档 SurfaceMatch 差异占 98%（已随裁定①收敛），非 match 残量主要是 safe-index 解糖缺口。

细节见 `nolang-selfhost.md` §5.3。

### 11.2 其余同步要求

- `nolang-selfhost.md` §5.3/§5.5 流水线图需同步。
- `compiler/` 侧 `hir.no` / `hir2mir.no` 等应按「parser 保留 SurfaceMatch + H0 三路直出」终态实现。

---

## 12. 已落地记录（摘要）

> 详细实施记录已归档。以下仅保留每期的核心结论与门禁状态。

### 12.1 S0（instrumentation）
- 三个测试文件：`sugar_classification_test.go`、`ast_freeze_test.go`、`sugar_kinds_test.go`。
- 实测：C2=66 marker；C6=532 档 AST dump 逐字节相同；C4 普查 `grouped=11021`。

### 12.2 S1a（lowering 接缝）
- `Parser.SkipLowering` + `(*Parser).LowerSurface`。门禁：532 档两路径逐字节相同。
- 糖两级分类：parser 展开 703 处、H0 消费 260 处。

### 12.3 S1 风险实测
- 三形态诊断普查：12 trace / 124 程序漂移。收敛后 10 条登记（`sugarDriftAllowlist`）。
- 两条真根因：`idxLocalTypes` 只在降糖期填（空表致静默失效）；`catchAllTailBody` nil/空切片歧义（降糖假阴性）。

### 12.4 S1 落地（三刀）
- 共享糖臂下降器 `walkSurfaceMatchChildren`（9 walker）。
- `idxLocalTypes` 去耦合（`precomputeIdxLocalTypes`）。
- `catchAllTailBody` 返回值改 `([]Statement, bool)`。
- 效果：`lowered→sugar` 相异 122→**19**，`surface→sugar` 124→**6**。

### 12.5 S2（h0Diags ERROR 通道）
- `Program.Errors` 新增 + `ASTToHIRWithMap` 按 Severity 分流。
- `merged.Errors` 非空则拒出货。门禁 4 条全绿。

### 12.6 C8（SurfaceMatch dump）
- `dump.go` 补 `case *SurfaceMatch` + `matchArm` helper。
- 糖形态门禁：532 程序 0 unknown + AST 冻结。
- L2 自举：650 档 A/B 逐字节相同（`no ast` 不产 SurfaceMatch）。

### 12.7 §19 InBounds 迁入 side-table
- `IndexExpression.InBounds` 删除 → `NodeFacts.IndexInBounds`。
- 快取镜像 + `ASTVersion` bump。反空洞门禁 3 条。

### 12.8 §20 推断 ?T 迁入 side-table
- `LetStatement.Type` 就地写消除 → `NodeFacts.InferredType` + `InferredByOptionArith`（1-bit 区分）。
- `publishInferredType` 统一入口。`ASTVersion` bump。
- **2026-10-26 追记：`InferredByOptionArith` 已删除**。它是 `inferOptionArith`
  在同一次 `LowerSurface` walk 内写、`maybeCaptureOptionAssign` 同一次 walk 稍后读的
  **局部判定结果**，不是可重建的程式事实；改由 walker 把 `inferOptionArith` 的回传值
  以参数 `byOptionArith` 直接传给消费点。`TypeFact` 现只剩
  `{InferredType, TypeOverride}`。`ASTVersion` bump 至 `2026-10-26.2`。
  - 语义等价证明：`ls.Type != nil` 时 `inferOptionArith` 必然提前返回 false ⇒
    旧的「两条短路」互斥，合并为 `(ls.Type != nil || byOptionArith)`。
  - 实测：482 档 `tests/*.no` 的 `no ast` 输出**逐字节相同**（旧语义二进制用
    `go build -overlay` 构造，未动工作树）；全仓 `go test ./...` 绿。
  - ⚠️ 不能改成「消费点重算 `signedIntOperandType`」：`inferOptionArith` 自己会
    `setVarType` 改动可见型别集合（`x = x + 1` 自引用），重算会得到不同答案。

### 12.8.1 「只存 Prog + 重走重建」的可行性（2026-10-26 实测，结论：不做）
动机是让 `CachedProgram` 只剩 `Prog`，两张事实表在解码后由 `BuildSemViews(prog)` 重建。

- **重建是可行的，但有三个前置，每一个都是静默失效源**：
  1. `ParseProgram` 设 `program.Sem = p.sem`；重建必须把 parser 的 `sem` 指回
     **解码后 program 的 Sem**，否则事实全写进另一张表（`no ast` 看不出来）。
  2. 解码后 `Sem.typeFacts` / `Sem.nodeFacts` 是 **nil**（unexported，gob 不搬），
     而所有 setter 对 nil 表**静默 no-op** ⇒ 必须先 `NewTypeFacts()` / `NewNodeFacts()`。
  3. 捕获 pass 依赖 **parser 实例状态** `funcSignatures` / `methodSignatures`
     （`returnTypeFromCallExpr`）。重建必须按 `decl.go:1818-1837` 的规则重填：
     方法（`IsMethodDef && Name[0] != '['`）进 `methodSignatures`，其余进
     `funcSignatures`，且 **`#{buildin}` 桩（`BuiltinStub`）一律跳过**——
     桩的回传型别来自 extern 表，本地不登记。
- **实测结果**（600 档 = `tests/*.no` 482 + `src/std/*.no` 118，比较
  `ASTToHIR().Dump()` 逐字节）：
  | 步骤 | 分歧 |
  |---|---|
  | 冷 parser，不补 (1)(2) | 78 / 482 |
  | 只补 (1)（sem 重指） | 75 / 482 |
  | 补 (1)(2)（安装事实表） | **0 / 482** |
  | 扩到 600 档 | 2 / 600（`dom.no` / `txt.no`） |
  | 补 (3) 只填 `funcSignatures` | 2 / 600（`fs.no` / `txt.no`） |
  | 补 (3) 按 `decl.go` 分流方法 | 1 / 600（`fs.no`） |
  | 再跳过 `#{buildin}` 桩 | **1 / 600**（`txt.no`） |
  残差 `txt.no`：`c = .byte(i + k) - target.byte(k)` 的参考捕获由
  `maybeIndexOutAssign` 一侧产生，重建只跑推断分析、不复现该侧效应。
- **但收益是 0.09%**：把两张表从 blob 里彻底去掉，41 个 std 顶层文件合计
  gzip 后 818,198 → 817,434 字节（**−764 B，−0.09%**）。真正的体积收益早已由
  「注解回归 AST」拿到（3,000,375 → 2,637,719，−12%）。
- **结论：不做。** 用 0.09% 换一条必须与 `decl.go` 永久同步的第二推导路径
  （两条规则已经踩中，第三条迟早出现）不划算。两张表保留序列化；
  `ByOptionArith` 的删除独立成立（少一栏、去掉顺序依赖、语料逐字节不变），已落地。

### 12.8.2 `CachedProgram` 收敛为 `{Prog}`（2026-10-26 定案并落地，推翻 12.8.1 的「不做」）

12.8.1 以体积论证被推翻：**这个 blob 是「结构化的原始码」**——未来可能换序列化
格式、且要作为**分发格式**，因此不得夹带任何编译器推导结果。推导规则会随编译器
版本变化，而缓存键（内容哈希 + `ASTVersion` + salt）看不到编译器建置；同一件事
有樹與副表兩個真相來源本身就會漂移。`ASTVersion = 2026-10-26.3`，作廢全部舊
blob（舊 blob 的 `TypeFacts`/`NodeFacts` 欄位被新 decoder 靜默丟棄——「能解碼」
不等於「語義正確」）。

**落地形態**（`parser/semviews.go`）：
- `CachedProgram` 只剩 `Prog`；`detCached` 同步收斂，兩表的 detU32 鏡像全刪。
- 解碼後由 `BuildSemViews(prog, cfg)` 重建；`cfg` 是 `SemRebuildConfig`
  （Filename / AllowAnonymousFnType / 跨模組簽名表 / 枚舉變體）——**新鮮解析與
  重建共用同一份 `cfg.Apply(p)`**，新增解析輸入不可能只加在一側。
- `Get(key, cfg)` 把 cfg 設為必傳參：解碼不重建會得到空事實表（靜默錯誤），
  簽名上無法遺忘。調用點：`build.parseFile` / `parseEntryProgram`（共用
  `astParseConfig`）、`checker.funcargs`（空配置，對應裸解析）、Tier-0 的
  `StdASTFor`（`stdSemConfig` + 已烤指紋驗證）、mmast。
- 門禁 `TestBuildSemViewsMatchesFreshParse`（`semviews_test.go`）：**523 檔**
  「encode `{Prog}` → decode → BuildSemViews ≡ fresh parse」逐位元組等價
  （事實指紋 + HIR dump 雙 oracle + blob 不夾帶事實的反空洞斷言）。

**12.8.1 的殘差（`txt.no`/`fs.no`）已全部解決，根因是「簽名登記規則」抄錯**，
三條實測規則（`rebuildLocalSignatures`）：

1. **`#{buildin}` 樁照樣登記**（decl.go:1820-1838 不查 `BuiltinStub`）。
   fs.no 的 `read` 樁最終不在表裡，不是因為它是樁，而是因為同檔後面的
   `file.read` 方法在註冊當下的裸名也是 `read`——**先覆寫、再被 ：56-58 的
   改名刪除**，淨效果是樁條目被沖掉。跳過樁會讓樁方法 `txt.byte`（實測有登記）
   漏掉，`.byte(...)` 的 `?byte` 回傳型別消失。
2. **`=`-形式方法落在 `funcSignatures` 的全名鍵**：註冊點執行時
   `IsMethodDef` 尚未置 true（decl.go:52 才設）⇒ 進了 `funcSignatures[裸名]`，
   隨後 ：56-58 刪除裸名、改名為全名。只查最終 `IsMethodDef` 會把它錯路由進
   `methodSignatures`（12.8.1 的 txt.no 殘差正是這個，不是 `maybeIndexOutAssign`）。
3. **運算子方法（`Name[0] == '['`，如 `[]txt.join`）不登記**（解析期不走
   `parseFunctionDefinition` 的登記點）。

⇒ 沖刷語意依賴**文件順序**（`file.read` 必須在其後刪掉 `read` 樁條目）；
`walkNodes` 按語句順序走訪頂層宣告，與解析順序一致。

**12.8.1 的殘差（`fs.no`）根因之二**：`prepareInferredTypes` 的 analysisOnly
走查會在重建時對 `n = read(...)` 憑空合成 `?i64`（call-root 捕獲）——上述規則 1
修好後，重建的 `funcSignatures` 與新鮮解析逐鍵一致，該分歧消失。

blob 体积：2,636,391 → 2,595,880 B（两表移出）。

### 12.9 §21 S4（formatter 翻糖形态）
- 532 档 A/B 门禁。收敛：169→1。
- 消费方全翻：`no fmt` / LSP 三消费方 / `no fix` 五条 / `tools/*` 三个。
- 诊断形态无关化：`warnUnreachableMatchArms` 移到 ParseProgram 收尾。

### 12.10 §22 S1 收官（match 表示统一）
- 默认路径保留 `SurfaceMatch`（翻转 `b918dba9`）。
- H0 三路直出（S-a/S-b/S-c）。12 痕迹字段已删。
- 双路径终态：js/wasm 走 AST 降糖（独立后端选择）。
- 全量验收：535 档 `no run` A/B 535/535 SAME；`no vet` 110/115 std 只增 HINT（真阳性）。

### 12.11 §23 KGrouped H0 抹除
- `tohir.go` 透传内层表达式。MIR 五处死分支已删。
- G3 真门禁启用。破坏实验实证。

### 12.12 §24 S6（ASTVersion 政策 + G7）
- `ASTVersion` 两向清单（规范性注释）。
- `hir.HIRVersion = "2026-10-21.1"`（无运行时读者，防键混用）。
- G7 HIR 等价 oracle：481/481 档。破坏实验（删镜像行 → 2 档 HIR 分歧）。
- CI 闸修复：注入 StdSigKey + 扩闸面到 `./...`。

### 12.13 V8 AST 写点清零（S5 全線）
- AST 寫入門禁（`astaudit` + pin）+ 六階段語義事實遷移 + 裝配工作副本隔離。
- pin **30→0**。ParseProgram 輸出永久不可變。
- 30 寫入點實測清單（供考古）：`SourceFile` ×9、`ModuleOwner` ×2、`IsModuleConst`、`VariadicUnion`/`GenericUnion`、`Type`（Override）、改名/前綴/呼叫改寫 15 鍵。

### 12.14 Stage 6 + 6C-optional（Checker 迁 THIR 全链，2026-10-23/24）
- **6A–6E**：lintSteps 切分（39 步 surface=27/THIR=12）→ 糖形态三锚点 → 14 对影子分组迁移 → 6D 切换（`BuildTHIRView` 含 H0 副作用全隔离）→ 6E 收口（删临时指纹脚手架，门禁收敛）。
- **6C-optional 五批**：① `nodeType` 去反射（type switch）→ ② 显式 type map（`THIRView.ExprTypes` + `TypeOf` 第三档，惰性构建，不写 `pkg.Inferred` 以免翻转 MIR 的 4 处 `inferredTypeOf` 兜底）→ ③ DivByZero 型别确认精化（浮点被除数跳过，**只减不增**）→ ④ StringConcat 补报（两侧确定为字串才报，**只增不减**）→ ⑤ §3.3 终态收口（`PopulateInferredTypes` 改消费 `effectiveTypeMap` 显式 type map，函数体为纯搬运）。
- 门禁：build/vet 清、checker 全量 ✅、G7 HIR 逐字节一致 ✅、切分等价 + THIR 影子 14 对全等 ✅、全仓 `go test ./...` 0 FAIL。

---

## 13. 剩余 backlog 分期评估（2026-10-24 摸底）

> 两项均为**跨包大改造**，非单批可完成；此处记录摸底结论与分期建议，供决策。

### 13.1 S3（`__cap_N` 结构捕获）—— V4 违规残差

**性质**：H0 结构捕获在** AST 层注入合成节点**——为 `a = b + (c / d)` 这类「可能上抛 `?T` 的算术」分配临时变量 `__cap_N`，再展开为 `match __cap_N { nil/err/ok -> … }`（`lowering.go`：`addCaptureNode` 分配、`collectCaptureNodes` 递归收集、`maybeCaptureOptionAssign` 构造 match；计数点 17 处）。

**为什么难**：
- 消除它需要把「捕获值」改为 **HIR 层结构化承载**（如 `KSlot`／直接嵌套表达），而非 AST 合成变量；
- 下游 **MIR / codegen 当前依赖 `__cap_N` + match 的形态**推导 option 传播（见 `lowering.go:1273` 注释：MIR 从 nil 臂的 `a = __cap_N` 推导），改造面横跨 parser → mir → codegen；
- option 语义是核心正确性面，任何分叉都会放大到 143 档失败或运行期语义漂移。

**建议分期**（每期独立门禁）：
1. **S3-a 可行性验证**：确认 HIR 层能否承载捕获（不落 AST 合成）—— ✅ **已完成（2026-10-24，结论见下）**；
2. **S3-b MIR 侧准备**：让 MIR 支持新的捕获表达（双轨并存、旧形态仍可 lower）—— ✅ **已完成（2026-10-24）**：双轨开关 + AST 冻结 + H0 直产 + 四层等价门禁（HIR / checker / MIR / LLVM IR），剩余三项决策见 §13.1 末；
3. **S3-c 切换 + 删合成路径**：G7 + 全仓门禁护航，删除 `__cap_N` 生成。

#### S3-a 可行性验证结论（2026-10-24）✅ 可行

**两项关键实测**：

| 探查 | 结论 |
|---|---|
| HIR 是否有 `KMatch` kind | ❌ **没有**。`KTypeMatch`/`KTypeArm` 仅服务类型解构；value match 在 H0 已展开为 if 链（`KIf` / `KCond`）⇒ 捕获形态实为 `KLet(__cap_N)` + if 链 |
| `src/mir` 是否硬编码 `__cap_` | ❌ **没有**。`__cap_` 仅出现在 `parser/lowering.go`(17)、`checker`(3)、`hir/async_group*(2)` 等；**MIR 靠结构推导** option 传播（nil 臂 `a = __cap_N` 的赋值形态），不依赖绑定名 |

**因此**：捕获的本质是「把可能出错的子表达式结果绑定到一个名字，供 if 链各分支引用」。消除 AST 合成的可行路径是——**把合成位置从 AST 层移到 HIR 层**：H0 在 AST→HIR 转换时直接产出 `KLet`（绑定）+ `KIf` 链，AST 保持冻结（ParseProgram 输出不可变，§1.4 / V4 合规）。HIR 层的合成是 H0 的**产物**而非「改写的输入」，不属 V4 违规。

**风险点（S3-b 前必须先解决）**：
1. **管线分层**：当前是「AST 降糖（`lowerer`）→ AST→HIR（`tohir`）」两段式，捕获降糖在**第一段**改写 AST。要移到 HIR 层，需把捕获降糖下移到 `tohir`（或让 `tohir` 直接产出展开形态），且 `match → if 链` 的展开要同步可用。
2. **语义分析可用性**：捕获依赖 `optionInnerOfVar` / `captureTargetAllowsOption` 等 **AST 侧**作用域与型别判定；下移后须保证这些判定在 `tohir` 阶段仍可得（否则会改变哪些语句被捕获 ⇒ 行为变更）。
   - **S3-b 前置探查结论（2026-10-24）**：**不能直接下移，但可经副表桥接解决**。实测 `captureTargetAllowsOption`（`lowering.go:5280`）依赖三样全是 lowerer 走查期累积的**实时作用域状态**——`l.declaredLocals`（已宣告局部集合）、`l.localDeclaredType(name)`（作用域型别）、`l.localDefinitelyNonOption(name)`——`tohir` 是独立 walker，无此状态。
   - **解法（与 6C-optional「显式 type map」同构）**：lowerer 照旧走查并**算出**判定结果（是否捕获 + 捕获节点列表 + 各节点 inner 型别），但**不再改写 AST**，改为把结果写进 **node-keyed 副表**；`tohir` 读副表直接产出 `KLet` + `KIf` 链。AST 保持冻结、判定口径不变 ⇒ 无行为变更。
   - **次生待办**：① 副表条目是否需进 AST cache blob（`TypeOverride` 是装配期产物但仍镜像，需同口径评估）；② 糖形态路径（`SkipLowering=true`，供 `no fmt` / LSP）本就不跑捕获，下移后仍不跑 ⇒ 该路径不受影响。
3. **MIR 识别面虽不靠名字，但靠形态**：新形态的 `KLet` 必须仍呈现「`?T` 型别 + if 链首臂传播」的结构，否则 `inferredTypeOf` 等兜底路径会翻车。

#### S3-b 第一步（2026-10-24）捕获判定副表基础设施 ✅ 已落地

新增 `src/parser/capturefacts.go`：`CaptureFacts`（node-keyed，`entries` 刻意未导出以避开反射走查，同 `TypeFacts`）+ `CaptureFact{Inner, Binds}` + `CaptureBind{Expr, Inner, Name}`；`SemanticContext` 增 `captureFacts` 字段与 `SetCaptureFact` / `CaptureFactOf` / `CaptureFactsLen` 三个访问器。

- **只写不读**：`maybeCaptureOptionAssign`（`lowering.go:1271-1277`）在「inner 最终确定、`nodes` 收集完」后写表，下方结构改写（预宣告 / match 三臂 / 取代节点）照旧 ⇒ 纯旁路，零行为变更。
- **刻意不进 blob**：本表是编译期降糖产物，AST cache 反序列化后由 lowerer 重算即可，与 `TypeOverride` 的镜像口径不同 ⇒ 不加 `Serialize*` / `Restore*`，不影响 `ASTVersion`。
- 门禁：`go build` / `go vet` / checker 全量 / G7 HIR 逐字节一致 / 切分等价 + THIR 影子 / 全仓 `go test` 全绿。

#### S3-b 第二步前置：捕获写入点触发条件排查结论（2026-10-24）✅ 已厘清

首版测试用 `x = 1 + 4 / 2` 断言「记 1 条」却得 0，一度怀疑埋点失效。实测排查后确认**不是 bug，是设计上的短路**。`maybeCaptureOptionAssign` 在写入点之前有四道闸门：

| 语料 | 是否写表 | 挡下的闸门 |
|---|---|---|
| `x = 1 + 4 / 2`（纯字面量算术） | ❌ 0 | `signedIntOperandType` 对整数字面量直接回 `i64` ⇒ 两侧可判定 ⇒ `inferOptionArith` 成功推出 `?i64` 并拉起 `InferredByOptionArith` ⇒ `lowering.go:1197` 短路（整条算术根由 codegen 逐层包裹，不做结构 desugar） |
| `o ?i64 = 1` / `x = o + 1`（算术根含 option 运算元） | ✅ 1 | —（`signedIntOperandType(o)` 对 `?i64` 失败 ⇒ 推断不出 ⇒ 走结构捕获） |
| `x = p[i] + 1`（巢状安全索引） | ✅ 1 | —（根是算术，索引在运算元位置被 `collectCaptureNodes` 就地捕获） |
| `x = p[i]`（安全索引**根**） | ❌ 0 | `lowering.go:1208-1213` 索引分支只合成 `?elem` 交 codegen，`return nil` 提前 |

**结论**：能走到写入点的是「算术根但 `inferOptionArith` 推不出」这一族——正是需要结构 desugar 的那些。

**对偶不变量（S3-b 切换唯一依赖的前提）**：

```
登记 1 条  ⟺  AST 真的被注入 `__cap_N` 合成节点（V4 违规面）
```

`capturefacts_test.go` 的 ③④ 两组断言同时钉住两侧，任一侧破都会红——只断言单侧时，副表可能记录到「其实没注入合成节点」的绑定（tohir 会凭空建 `KLet`+`KIf` 链），或漏掉真正注入的那一族。

> 安全索引**根**（`x = p[i]`）不写表、不注入合成 AST 节点，不属 V4 违规；其 `?elem` 已由 `sem.InferredType` 承载（见 `s3_inferred_side_table_test.go`）。本表只需覆盖「结构 desugar 族」，索引根无缺口。

#### S3-b MIR 侧识别面探查结论（2026-10-24）✅ MIR 无名字依赖，可切换

> 对应上文「风险点 3：MIR 识别面虽不靠名字，但靠形态」。结论：**靠结构，且有明确的形态清单**。

**① 名字依赖裁定（阻塞项排查）**

| 位置 | 是否硬编码 `__cap_` |
|---|---|
| `src/mir` | ❌ 零命中。唯一标识符字符串比较是语言关键字 `it`（`hir2mir.go:2700/3066/3468/11051`） |
| `src/hir` | ❌ 仅 2 处**注释**（`hir/async_group.go:127`、`async_group_test.go:178`） |
| `src/checker` | ⚠️ **有**——`checker/checker.go:1430`、`checker/funcargs.go:1942` 用 `strings.HasPrefix(name, "__")` |

checker 读的是 **AST**（`build/transpiler.go:1910` 才调 `ASTToHIRWithMap`），AST 冻结后 H0 不再产出 `__cap_` 命名的 AST 节点 ⇒ 这两条会失效。属**迁移动作的下游连带**，不是 MIR 阻塞项，但 S3-c 必须一并处理。

**② AST 标志在 HIR 的保留情况**（`tohir.go:504-515`）

- `IsSynthetic` → **保留**（`FlagSynthetic`），但 `src/mir` 内**零消费者**，H0 不必费心。
- `IsPropagation` → **丢弃**。HIR flag 列表无 `FlagPropagation` ⇒ **H0 无需复现**。
- `IsInferred` → **丢弃**。`typeFromInferredString`（`tohir.go:377-390`）重建的 `NullableType` 不设 `IsInferred`。

**③ 型别解析序**（`tohir.go:486-503`）：`TypeOverride` > `s.Type` > `sem.InferredType`。
⇒ H0 给 `KLet` 直接写 `Type = "?inner"` 即可让 MIR 拿到型别，**不必依赖副表**。

**④ MIR 的 nil 臂目标型别推导**（`hir2mir.go:3238-3287`）：依赖「赋值右侧是 `KIdent` 且 `valueTypeOf(val)` 为 `?inner`」——**纯结构**，与分支顺序、首臂优先、Block 嵌套深度均无关。（易混淆的「首臂优先」路径 `captureArmValue`（`:3844-3864`）要求 `KLet` 直接子节点是 `KIf` 且 `stmtVal` 已设，而 `KLet` 分支从不设 `stmtVal` ⇒ 捕获形态走不到。）

**⑤ H0 必须保留的最小结构特征清单**

1. `x` 的绑定型别可达：预宣告 `KLet` 带 `Type = "?inner"`，或首次绑定的 `KLet` 右值为 `KIdent` 且该 ident 已是 `?inner`（命中 `:3244-3258`）。
2. `__cap_N` 的 `KLet` 必须带 `Type = "?inner"`（或 `pkg.SetInferred` 同值），否则 `inferredTypeOf` 退化为 `void`。
3. 臂内 `x = __cap_N` 必须是 `KLet` + `KIdent` 右值（非 `KAssign`）——`:3197` 的 `cn.Kind == hir.KIdent` 门控。
4. 条件链必须是 `KIf`，slot 顺序 `cond/then/else`（`tohir.go:1041-1045`）。
5. 所有 `KLet` 的 `pkg.InferredType(id)` 由 `PopulateInferredTypes`（`tohir.go:296`）填好。
6. `KLet` 与 `KIf` 的包法须与今日一致（`KExprStmt(KIf)`，`tohir.go:518`），否则误入 `hir2mir.go:2618` 的 expr-capture 路径，语义改变。

**⑥ 最可能翻车处（按风险排序）**

1. `inferredTypeOf` 的 void 兜底（`hir2mir.go:1645-1647`）：漏写 `Type` ⇒ 静默变 `void`，症状（报 `add void`）离现场极远。
2. `letDeclaredType`（`:8905-8922`）+ `valueTypeOf(val)`（`:3253`）：nil 臂目标型别的**唯一**来源；二级兜底 `typeOfNode(cn)`（`:3254-3256`）在 `__cap_N` 自身型别缺失时会再退 void。
3. owned option 分支（`:3240-3252`）：`?str` / `?[]T` 走 `ownedOpt`，门控不成立 ⇒ 落到 `:3325` 别名路径 ⇒ 共享 drop（**双释放**）。
4. `captureArmValue` 的 `i64` 兜底（`:3851`）：若 H0 误把 `KLet(x)` 直接子节点做成 `KIf`，`x` 被定成 `i64` —— 正是 `lowering.go:1288-1292` 记录的 `xs.len-bytes()` 事故。
5. KIf 的 arm 合并/落块（`:3640-3648`、`:3710-3734`）：block 结构变化 ⇒ 「未初始化栈」类随机值错误。

#### S3-b 实作第一步（2026-10-24）：双轨开关 + AST 冻结 ✅

**落地**：

- 新增 `HIRCaptureEnabled`（`capturefacts.go`）双轨开关，**默认 false ⇒ 生产路径零行为变化**。
  - `false`：lowerer 照旧在 AST 层注入 `__cap_N`（现状）。
  - `true`：lowerer 只写副表、**不再改写 AST**；结构由 H0 产出（尚未实现）。
- `CaptureFact` 增 `WasDeclared bool`：目标变数在本陈述前是否已宣告（`declaredLocals`）。决定 H0 是否产出预宣告 `x ?T = nil`；该状态活在 lowerer 走查期作用域，`tohir` 拿不到，必须随表带出。
- `maybeCaptureOptionAssign` 在开关打开时：`synthesizeNullableType` 照旧（H0 的 `?T` 唯一来源）→ 直接 `return nil`，跳过预宣告 / match 三臂 / 取代节点。

**实测踩到的坑（已修，值得记）**：`collectCaptureNodes` 在 `case *Identifier`（`lowering.go:1100-1106`）**没有** `analysisOnly` 闸——改写由调用方完成。而 `maybeCaptureOptionAssign` 会被 walker 多次走到：第一次就地把 `o` 换成 `__cap_1`，而 `addCaptureNode` 已用 `setVarType` 给它登记了 `?i64` ⇒ **后续 pass 把合成标识当成新的可错源再捕获一次**，`Binds` 里存的是 `__cap_1` 本身而非原始表达式，副表直接失去意义。

修法：开关打开时临时置 `l.analysisOnly = true` 再调 `collectCaptureNodes`（复用既有的「不改写」分支），调完还原。

**⚠️ 关键裁定 —— 不需要克隆/改写表达式**：H0 不必拿到「替换后的表达式」。让 `tohir` 在 lowering 时按「原始表达式指针 → `__cap_N` 名」**即席替换**即可（Binding 保持指向原始节点）。理由：语义副表（`InferredType` / `TypeOverride`）以**原始指针**为键，克隆节点会查不到型别，产出的 HIR 与今日不一致。这取消了原先设想的 clone + substitute 方案。

**新增测试** `TestCaptureFactsHIRTrackFreezesAST`：断言 ① AST 无合成注入（S3-b 的核心收益）② `x.Type` 仍为 nil、`?i64` 只进 `sem.InferredType` ③ 副表 `Inner` / `Binds` / `WasDeclared` 齐备，且 `Binds[0].Expr` 指向**原始** `o`。

**H0 目标蓝本**（`x = o + 1`，当前 AST 降糖路径的 `hir.Package.Dump()`，下一批的逐字节比对目标）：

```
block
  let x t=?i64 flags=0x4            ← 预宣告（WasDeclared=false 时才产）
    nil
  block
    let __cap_1 t=?i64 flags=0x4    ← 绑定
      ident o
    expr-stmt
      if  cond: __cap_1 == nil → { let it t=nil flags=0x40000004; let x = __cap_1 }
          else: if cond: __cap_1 == err → { let it t=err;  let x = __cap_1 }
                else: if cond: __cap_1 == ok  → { let it t=i64;  let x = __cap_1 + 1 }
```

要点：三臂均有 `it` 绑定（`FlagArmBinding|FlagSynthetic`，型别分别为 `nil` / `err` / `i64`）；臂内 `let x` **不带型别**（靠 `valueTypeOf` 推导）；最内层 ok 臂无 else。

#### S3-b 实作第二步（2026-10-24）：H0 直产 + 双轨等价 ✅

**落地**：

- **抽取 `buildCaptureChain`**（`lowering.go`）：把「预宣告 + 绑定 + 三臂」的建构从 `maybeCaptureOptionAssign` 抽出，**两轨共用同一份实现**——这是「HIR 逐字节一致」的根本保证（与既有 `lowerUnwrapAssignToHIR` 同款手法）。
- **`lowerCaptureToHIR`**（`tohir.go`）：H0 侧的入口。用既有的 `c.scratchParser()` + 一个临时 `lowerer`（同 `lowerUnwrapAssignToHIR` 的范式）调 `buildCaptureChain`，产物经 `c.stmt` 折进 HIR。
- **`substCaptureExprs`**（`tohir.go`）：非破坏性替换。AST 轨是**就地改写** `ls.Value`；H0 轨不能碰用户的树，故另建替换版——**只浅拷贝通往被捕获节点的路径上的容器**，其余子树回传**原指针**（语义副表以原始指针为键，保留原身份才能与 AST 轨的型别解析一致）。
- 接线：`case *LetStatement` 开头，开关打开且副表有记录时走 H0 轨；失败回落原路径，不留 tombstone。

**验收门禁** `TestHIRCaptureMatchesASTDesugar`：同一语料两轨 `hir.Package.Dump()` **逐字节相等**。含自校验（两轨都必须真的产出 `__cap_1` 链），避免「两侧都空」的假绿。

**手造语料 7 条**（每条对准一种捕获形态，是等价性的主证——见下方 corpus 覆盖说明）：

| 语料 | 覆盖形态 |
|---|---|
| `x = o + 1` | 单绑定，算术根含 option 运算元 |
| `x = p[i] + 1` | 巢状安全索引（索引在运算元位置被捕获） |
| `x = o1 + o2 / o3` | **多绑定**（断言出现 `__cap_2`，验证巢状顺序：bind_0 在最外层） |
| `x = f1 * f2`（`?f64`） | 浮点 inner 补正（`commonCaptureInner` ⇒ `f64`，非默认 `i64`） |
| `x = s1 + s2`（`?str`） | str 拼接 inner 补正 |
| `x ?i64 = nil` 后 `x = o + 1` | `WasDeclared=true` ⇒ H0 不得产出预宣告 |
| `x = g(o + 1)` | 呼叫引数内捕获（根是呼叫，回传 plain） |

> Nolang 语法坑（实测）：函式回传值用**具名结果参数**再赋值——`g = (v i64) (r i64) { r = v }`，不是 `(i64) { v }`。

**`WasDeclared` 的覆盖方式（值得记）**：不能靠「产物差异」验证——目标已宣告为 `?T` 时，省掉的合成预宣告 `x ?T = nil` 与用户自己写的**等价**，两轨 MIR 与 `opt-operand` 同为 1155 bytes 是**正确**的。故由 `TestCaptureFactWasDeclared` 直接读副表断言双向（首次宣告 false / 已宣告 true）。

**corpus 门禁** `TestHIRCaptureTrackMatchesASTTrack`（`src/parser/dump/hir_equivalence_test.go`）：复用该文件既有的 corpus 走查设施（`equivalenceCorpusRoots` / `lowerToHIR`），对每份可独立解析的语料跑两轨并逐字节比对。实测 **623 份比较、0 未知发散、1 已知发散**。

> ⚠️ **反真空结论很重要**：623 份语料里**只有 2 份**真的走到结构捕获（dump 里出现 `__cap_N`）。corpus 对这条门禁的覆盖**极薄**——绝大多数源码用 `?=`／显式处理，或 `inferOptionArith` 已成功推出 `?T` 而走不到结构 desugar。所以真正的等价证据来自 fixture 测试（`TestHIRCaptureMatchesASTDesugar`），corpus 门禁只是防回归的烟雾警报。**不要拿「623 份 0 发散」当作等价已充分验证。**

**已知未覆盖 / 下一步（S3-c 前必须补齐）**：

1. ~~**语料面**：corpus 只有 2 份有效覆盖。~~ → **已补齐（2026-10-24）**：手造 7 条覆盖多绑定／浮点／str／`WasDeclared`／呼叫引数（见上表）。剩余缺口：corpus 门禁本身仍只有 2 份有效覆盖，是防回归用，不是等价主证。
2. ~~**多绑定**尚未验证嵌套顺序。~~ → **已验证**（`x = o1 + o2 / o3`，断言 `__cap_2` 存在）。
3. ~~**端到端**：尚未验证 H0 轨产物的 MIR/codegen 行为。~~ → **已验证到 MIR 层（2026-10-24）**：新增 `src/mir/hir_capture_mir_test.go`，7 条语料两轨 `mir.Module.String()` 逐字节相同（1155–2648 bytes，非空）。这道门禁是「S3-c 敢不敢切换」的实质依据——HIR 相同不代表 MIR 相同（`inferredTypeOf` 的 void 退化、`letDeclaredType`+`valueTypeOf` 的 nil 臂推导、owned option 的 drop 归属都是 HIR 之后的判定）。**尚未验证**：LLVM IR / 运行期行为。→ **已验证（2026-10-24）**：`TestCaptureTracksProduceIdenticalLLVMIR`，7 条语料两轨 `(*Transpiler).Compile` 产物逐字节相同。
4. **checker 的 `__` 前缀依赖**：`checker/checker.go:1430`、`checker/funcargs.go:1942` 靠 `HasPrefix(name,"__")` 识别合成绑定。H0 轨不再产出 `__cap_` **AST** 节点 ⇒ 这两条会失效，S3-c 切换时须一并处理。
5. **AST 轨的注解丢失（既有缺陷，已列入 allowlist）**：`tests/arr-test-tmp.no` 的 `#{index-out=0.0}` 在 AST 轨出现 **0 次**、H0 轨 **1 次**。

   - **成因（已探针证实，2026-10-24）**：parser 走查把被捕获的 `LetStatement` 换成 `BlockStatement` 时，原节点的 `#{...}` 注解被**从语义表整体丢弃**。实测两组语料：

     | 语料 | AST 轨（替换后各副本） | H0 轨（原节点） |
     |---|---|---|
     | `arr-test-tmp` (`let c`) | `AnnotationsOf=0` ×6 | `AnnotationsOf=1` |
     | `byte-option-index` (`let j`) | `AnnotationsOf=0` ×9 | `AnnotationsOf=1` ×2 |

     `annotate`（`tohir.go:324`）只读 `sem.AnnotationsOf(n)`，按**节点指针**；替换产生的新节点没有条目 ⇒ 注解进不了 HIR。`CommentedNode` 只是注释（`Doc`/`Comment`，`ast.go:428`），与 `#{}` 注解无关，已排除。
   - **为何只有 arr-test-tmp 暴露**：`byte-option-index` 的 `#{overflow=wrap}` 另有存活节点承载，两轨都看得到注解 ⇒ 不发散；`arr-test-tmp` 的 `#{index-out=0.0}` 没有其他载体 ⇒ 暴露。
   - ⚠️ 曾尝试「一律抑制 H0 侧的 annotate/remember」来对齐，实测**行不通**：`remember`（astOf）必须抑制（否则 `PopulateInferredTypes` 会把 `ls` 的推断型别写到 block id 上），`annotate` 必须保留（否则 byte-option-index 的 overflow 也会丢）。两者方向相反，没有通用抑制规则。
   - 当前处置：`hirCaptureKnownDivergence` allowlist 钉住（只减不增，且断言观察数 == 名单数，语料若修好会立刻报错要求清名單）。H0 侧是「修好了」的那一侧，但 S3-b 准则是零行为变化 ⇒ **切换前须先独立修掉 AST 轨的注解丢失**，不能靠抹平 H0 侧过关。
   - **修复方向（未实施）**：在「走查替换陈述」这一步把原节点的注解搬（remap）到替换出来的 `BlockStatement` 上。属**生产路径的行为变更**（被捕获陈述的注解会开始生效），必须独立成批做、并用 G7 + 全仓 + 端到端护航，不可夹带在 S3-b 里。

#### S3-b 检查器侧门禁（2026-10-24）：诊断确实不同，2/7

新增 `TestCheckerDiagnosticsMatchAcrossCaptureTracks`（`src/parser/dump/capture_checker_parity_test.go`）：7 条手造语料，两轨各跑 `checker.RunAllLints`（`no vet`／LSP 的聚合入口）比对诊断集合。

**为什么必须单列这道门禁**：HIR/MIR 相同**推不出**诊断相同——checker 读的是 **AST**，而两轨的 AST 根本不同（AST 轨看到降糖产物 `__cap_N ?T = …` + if 链；H0 轨看到冻结的用户源码 `x = o + 1` + 语义副表的 `?T`）。

**实测：2/7 语料诊断不同，且都是 H0 轨多报**：

| 语料 | 仅 H0 轨报告 | 级别 |
|---|---|---|
| `nested-safe-index` | `ovfhndld`（绑定 `?T` 却从未解包） | hint |
| `was-declared` | `optunuse`（option 绑定后从未被消费） | **error** |

**这不是 AST 轨的缺陷，恰恰相反**：AST 轨降糖后 `x` 在 nil/err/ok 各臂都被重新赋值，制造了「已消费」的假象；H0 轨看到用户源码，一眼看出 `x` 绑到 `?T` 后从未解包。**H0 侧是修好了漏报。**

> ⚠️ **这是 S3-c 最大的风险**：切换会在**今天能通过检查的程式上新增诊断**（`optunuse` 是 error 级）。必须在切换前独立决定——接受并更新语料／用户程式，或让检查器对冻结形态维持旧判决。不可夹带进 S3-b。
>
> **（2026-10-25 已切换并实测）**：`HIRCaptureEnabled` 默认翻为 `true`（H0 成为生产默认）。实测影响面：① `src/std` + `tests` 全量 `no vet` **零** `optunuse`/`ovfhndld`，标准库与测试不受影响；② 仅特定用户形态（option 源作呼叫引数、结果被绑定，如 `r0 = g(3, j - 1)`）在 H0 轨因看到原始 `?i64` 而新增 `optunuse` error——该诊断在语义上正确（绑定了 option 却未解包），属 S3-c 收紧检查器的预期收益，已接受。③ 编译器自身 2 个编码了 AST 轨不变量的测试（`TestCaptureFactsRecorded`、`TestBareOptionArgLoweringTempExempt`）已改为显式锁定各自适用的轨道。切换经 `make test` 全量门禁（含 S3-b 双轨四层等价）验证通过。

处置同注解一项：`captureCheckerKnownDiff` allowlist 钉住（只减不增 + 断言观察数 == 名单数）。

⚠️ 该测试在 `StdSigKey` 为空时 `t.Skip`，**裸 `go test ./...` 等于没跑** ⇒ 已接入 `tmp/run_gates.sh`（带 `-ldflags`）才会真正执行。

#### S3-b 端到端冒烟（2026-10-24）：默认路径完好 ✅

前 18 批的门禁都是单元级的，故重建 `no` 二进制（带 `StdSigKey`）实跑一次：

```
go build -ldflags="-X ...checker.StdSigKey=$KEY" -o ../bin/no-smoke ./cmd/no
PATH="/opt/homebrew/opt/llvm/bin:$PATH" ./bin/no-smoke run tests/<f>.no
```

| 语料 | 输出 | 结果 |
|---|---|---|
| `func-simple.no` | 5 / 104 / 101 / foobar / 4 / -1 | ✅ |
| `arr-test-tmp.no`（**走捕获**） | a=1.5 b=2.5 a*b=3.75 sum=3.75 | ✅ |
| `byte-option-index.no`（**走捕获**） | byte=195 idx=195 | ✅ |

⇒ 生产路径（默认 = H0 轨，即 `HIRCaptureEnabled=true`，S3-c 已于 2026-10-25 切换）在全部改动后完好，含 LLVM codegen 与运行期行为。

**仍未验证**：两轨的 **LLVM IR 等价**（原先认为需把 `HIRCaptureEnabled` 暴露成环境变量之类的外部开关，属生产代码改动）。→ **已验证，且不必改生产代码（见下）**。

#### S3-b LLVM IR 等价（2026-10-24）：两轨逐字节相同 ✅

新增 `src/build/capture_llvm_parity_test.go` 的 `TestCaptureTracksProduceIdenticalLLVMIR`：7 条语料两轨各跑 `(*Transpiler).Compile`，产出的 LLVM IR **逐字节相同**。

**关键手法（推翻了上一批的判断）**：不必把开关暴露成环境变量。`build` 包内的测试与 `parser` 同进程可直接设 `parser.HIRCaptureEnabled` 这个 package var，且 `(*Transpiler).Compile` 就是 `no build` 走的那条管线（parser → checker → HIR → MIR → LLVM），**不需要外部 LLVM 工具链**（只产 IR，不调 `llc`／`clang`）。⇒ 端到端等价可在纯 Go 测试内完成，生产代码零改动。

**语料形态（实测踩坑）**：不能用 `func main()`——`main` 在 Nolang 里是**保留名**（实测报 `'main' is a reserved name`），故 7 条全写成**顶层语句**。`call-arg` 的 `g` 必须收 `?i64`（收 `i64` 会被 checker 以 `fxxoptarg` 拒绝，编译根本走不到 codegen）。

**至此 S3-b 四层等价全部验证完毕**：

| 层 | 门禁 | 语料 | 结果 |
|---|---|---|---|
| HIR（fixture） | `TestHIRCaptureMatchesASTDesugar`（`parser`） | 7 条手造 | ✅ 逐字节相同（含自校验） |
| HIR（corpus） | `TestHIRCaptureTrackMatchesASTTrack`（`parser/dump`） | 623 份 | ✅ 0 未知发散 / 1 已知 |
| checker 诊断 | `TestCheckerDiagnosticsMatchAcrossCaptureTracks`（`parser/dump`） | 7 条 | ⚠️ 2/7 不同（H0 侧是修好漏报，见上） |
| MIR | `TestHIRCaptureTrackProducesIdenticalMIR`（`mir`） | 7 条 | ✅ 逐字节相同（1155–2648 bytes） |
| LLVM IR | `TestCaptureTracksProduceIdenticalLLVMIR`（`build`） | 7 条 | ✅ 逐字节相同 |

四层均已接入 `tmp/run_gates.sh`（带 `-ldflags`，否则 checker 那道会 `t.Skip`）。

**S3-c 决策（三项均已量化，于 2026-10-25 切换落地）**：

| # | 决策点 | 现状 | 影响 |
|---|---|---|---|
| ① | AST 轨注解丢失（`#{index-out=0.0}`） | **已修**（lowering.go:1364 将 `ls` 注解 remap 到替换出的 `BlockStatement`；`hirCaptureKnownDivergence` 已清空） | H0 侧是「修好了」的一侧；切换零行为变化，remap 随 S3-c 一并生效 |
| ② | checker 诊断差异（2/7，含 error 级 `optunuse`） | **已切换并实测** | `src/std`+`tests` 全量 `no vet` 零新增 `optunuse`/`ovfhndld`；仅 `r0 = g(3, j - 1)` 类「option 源作呼叫引数、结果被绑定」的特定用户形态在 H0 轨新增 `optunuse`（语义正确，已接受为收紧收益） |
| ③ | LLVM IR / 运行期 | 已验证逐字节相同 | 非阻塞项，已结案 |

### 13.2 S7 残差（gap I）—— 实测更正：match 表示已收官，残差全为正交轨

**⚠️ 本节此前记的「gap I 6 档 / L2 510/143」是 2026-10-21 中段的过期快照，已实测更正（2026-10-24）。**

**实测（2026-10-24）**：CTL=`bin/no`、SUT=`/tmp/noc-se`（2026-10-22 构建的自举编译器）、语料 `tests`+`src/std` 653 档、`no ast` 差分：

```
files=653  ctl_ok=653  ctl_empty=0  sut_ok=653  sut_empty=0
SAME=533  DIFF=120      失败集合 ctl=0 sut=0
oracle 覆盖：0 行 <unknown>（71/71 节点型别全覆盖）
```

**match-表示残差 = 0 档**：对 120 档 DIFF 逐档取 `MatchArm` / `SurfaceMatch` 头字段多重集归因 —— **count_gap 0 / flag_gap 0 / clean 120**。即 120 档的 match 表示全部恒等，分歧全在他处。这与 `nolang-selfhost.md` §5.3.4.y 的「s7m 收官」一致，且比文档记录的 515/138 又前进了 18 档。

> 结论：**gap I（复杂主体 match 陈述 + str/txt 反向多臂）已关闭**，S7 match-表示收敛线收官。「补齐 6 档清单」这项待办因此作废——那 6 档在 s7j→s7m 之间已被消除。

**当前残差 = 120 档正交轨**，按 CTL 侧独有的 dump marker 签名分类（12 类，合计 120）：

| # | marker 签名 | 档数 | 代表档 |
|---|---|---|---|
| 1 | `nomarker`（无特征 marker） | 35 | `collection/tree-map.no`、`crypto/blake2.no` |
| 2 | `synth+armBind+nullableT+idxout` | 32 | `archive/bzip2.no`、`archive/zip.no` |
| 3 | `synth+armBind+nullableT+cap+idxout+prop` | 12 | `archive/gzip.no`、`archive/tar.no` |
| 4 | `synth+armBind+inferred+nullableT+cap+idxout+prop` | 9 | `crypto/aes-gcm.no`、`crypto/ecdsa.no` |
| 5 | `inferred` | 8 | `embed.no`、`json.no`、`net/cookie.no` |
| 6 | `synth+armBind+nullableT+cap+prop` | 7 | `args.no`、`fmt.no`、`crypto/sha1.no` |
| 7 | `synth+armBind+inferred+nullableT+idxout` | 5 | `fs.no`、`crypto/tdes.no` |
| 8 | `synth+armBind+inferred+nullableT+cap+prop` | 5 | `crypto/argon2.no`、`dom.no` |
| 9 | **仅 SUT 侧多出行**（CTL 无独有行） | 4 | `collection/map.no`、`static-hashmap.no`（SUT 多吐 `GenericParams: v`）、`magic.no` |
| 10 | `warn`（warnings 计数） | 1 | `tests/match-test.no` |
| 11 | `synth+armBind+inferred+nullableT+idxout+generic` | 1 | `vec.no` |
| 12 | `inferred+warn` | 1 | `byte.no` |

**⚠️ 战略关联：S3 是自举收敛的最大单点杠杆**

按 marker 聚合：

| marker | 档数 | 占 120 档 | 含义 |
|---|---|---|---|
| `synth`（合成节点） | **71** | **59%** | 残差源于 **AST 层降糖注入的合成节点** |
| `idxout`（`__idx_out_N`） | 59 | 49% | 安全索引降糖 |
| `cap`（`__cap_N`） | **33** | **27.5%** | **S3 的结构捕获降糖** |

⇒ **S3-c 一旦落地（`__cap_N` 不再注入 AST），这 33 档残差会自动消失**——自举编译器不必再复现一个 AST 里已不存在的降糖产物。若 S3 的 H0 直产方案进一步扩展到安全索引降糖（`__idx_out_N`），杠杆可达 ~59 档。

这是「同一件事的两个收益面」：S3 在 §13.1 是 **V4 合规**问题，在这里是 **自举收敛**问题。此前两件事被分开跟踪，是漏看的协同。

**✅ mass-noop 已解决（2026-10-25）**：真凶是下方**缺口 4**（裸名呼叫在签名表未命中）。修复后自举编译器恢复完整 AST 输出（不再 `Program{}`），`fs.read-bytes` 正常。旧归因「Go 侧 `src/` 106 个未提交 `.go`」已失效——那些代码随 `a74c5b49` 收口，现已是 HEAD 的一部分。下钻到主编译器 `?=` 的 H0 直出（`tohir.go:495-506`，`lowerUnwrapAssignToHIR` 调 `lowerUnwrapAssign` 降成 `KBlock+KLet+KIf` 链，与 §13.1 的 S3-b 同类迁移；插桩证直出成功）产物，定位到三层缺口（逐层如下）：

**缺口 1（已修，`lowerUnwrapAssign` `*CallExpression` 分支漏设 `tmpType`）**：
`__unwrap_N = <rhs>` 的 `LetStatement.Type` 只来自 `tmpType`（`lowering.go:4925`），而 `*CallExpression` 分支只调 `setVarType(tmpName, inferred)` 漏设 `tmpType`（相邻 `*InfixExpression` / `*IndexExpression` 两分支都设了）。HIR 实测：`let s=__unwrap_5` 缺 `t=?i64`。H0 轨只从 `s.Type` 取型别 ID（不读 `FuncVarTypes`），漏设 ⇒ `?T` 变 nil ⇒ ok 臂 `it` 取值为 void ⇒ `size` 恒 undef。
**修复**：`lowering.go:4852` 的 `*CallExpression` 分支补 `if strings.HasPrefix(inferred,"?") { tmpType = &NullableType{Token:tok, Type:&NamedType{Token:tok, Value:inner}} }`，与 Infix/Index 同形。

**缺口 2（已修，H0 scratch parser 缺内置签名）**：
`lowerUnwrapAssignToHIR` 用 `scratchParser()` 重建 `funcSignatures`，但只从 `c.prog.Statements` 取（用户函数），**不含 transpiler 经 `SetExternSignatures` 注入的内置/跨文件签名**（`checker.CollectStdModuleSignatures`）。`?=` 对内置呼叫（如 `fs.fstat-size`）时 `inferTypeFromCallExpr` 返回 "" ⇒ 型别无法推断 ⇒ 同缺口 1 的后果。
**修复**：`SemanticContext` 新增 `ExternFuncSigs/ExternMethodSigs` 字段，`SetExternSignatures` 同步快照，scratchParser 合并进 `funcSignatures/methodSignatures`（与 `IdxLocalTypes/UnionMembers` 同一模式）。

**缺口 3（未修，本批新定位 —— 真正的 mass-noop 残根）**：
补完缺口 1+2 后 OK 臂已正确，但 **err/nil 传播臂** 仍崩：`func get` 的 MIR dump 显示
```
block 14 (err 传播臂):
    const  dst=47:void
    move   dst=0 args=[47 35]      ← 应为 move r = __unwrap_N（?i64），却变成 move r = void
```
即 `propagate()` 的 `let result = __unwrap_N`（`lowering.go:4980` 的 `Value: tmpIdent`）在 HIR→MIR 时 `__unwrap_N` 被解析成 **void 常量**，而非外层 `let __unwrap_N = <call>`（`?i64`）。nil 臂（block 12）同样用 `__unwrap_N` 却正确（`move dst=0 args=[37 35]`），唯独 err 臂解析为 void —— 说明 H0 解糖的传播臂对 `__unwrap_N` 的引用解析与 AST 轨旧解糖不一致（疑似 match 臂 HIR 转换对 `IsPropagation` let 的 `tmpIdent` 引用走错分支）。HEAD（AST 轨旧解糖）不触发此问题。
**修复方向（待下一轮，属用户主线 S3 迁移）**：核对 `lowerUnwrapAssign` 产出的传播臂 `let result = tmpIdent` 在 H0 HIR 转换下为何解析成 void（对比 AST 轨等价产物），重点看 `stmtNode` case `*LetStatement` 对 `IsPropagation` / `tmpIdent`（与 `tmpAssign` 同指针）的解析路径。验证门禁：探针 `probe_b0.no`（`use std/fs; get = (p)(r ?i64){ fd=fs.open-read(p); size ?= fs.fstat-size(fd); r=size }`）预期 `B0-OK x=5`。

**缺口 4（已修，2026-10-25 新定位 —— mass-noop 的真正残根）**：
`returnTypeFromCallExpr`（`src/parser/type.go`）的 `*Identifier` 分支**只用裸名**查 `p.funcSignatures`。std 模組內部以**非限定**方式呼叫同模組內建——`src/std/fs.no` 的 `file.read-bytes` 寫 `size ?= fstat-size(.fd)`——而签名表（`ExternFuncSigs` / `CollectStdModuleSignatures`）統一以 `"module.fn"`（`"fs.fstat-size"`）注册；S1 翻转後 std 以 surface 形态从 blob 载入、本地宣告不再写进 `funcSignatures`，裸名 `"fstat-size"` 因而查不到 ⇒ 回传型别为空 ⇒ `?=` 的 H0 直出（`lowerUnwrapAssign`）设不出 `tmpType` ⇒ tmp 变 void ⇒ `size` 为 0 ⇒ `file.read-bytes` 对非空档也回 `nil` ⇒ 自举编译器读不到源码 ⇒ 全档只吐 `Program{}`。

判别性对照（同一 `.fd`、同一 `?=`、同一 `?[]byte` 返回型别，只差限定与否）：

| 调用形态 | 查表键 | 结果 |
| --- | --- | --- |
| `fs.fstat-size(.fd)`（限定） | `fs.fstat-size` | 命中，`size=5` ✅ |
| `fstat-size(.fd)`（非限定，std 内部） | `fstat-size` | 未命中 ⇒ void ⇒ `size=0` ⇒ nil ❌ |

**修复**：`type.go` 新增 `currentModuleName()`（由 `p.Filename` 推模組名，`fs.no → fs`）；`*Identifier` 分支在裸名未命中时回退查 `module + "." + fnName`。对使用者档案该键不存在、自然落空，不会误配到别的模組。
**验证**：`file.read-bytes()` `RB-NIL → RB-OK`；`fs.read-bytes()` `NIL-OR-ERR → OK`；缺口 3 基线 `B0-OK x=5` 无回退；`make test` 全包 `ok`；`bin/no build compiler` 成功（此前 `EmitLLVM: setfield field` blocker 随之消失）；自举编译器 `ast` 输出与 `bin/no ast` 逐字一致（mass-noop 消除）。

> 验证现状（2026-10-24 本批）：缺口 1+2 已落地，`src/parser` 单测 `TestTmpUnwrapHIR` 证实 `let __unwrap_5 t=?i64`；`?=` 对普通呼叫（探针 `probe_min.no`：`x ?= f(5); r=x`）OK 臂执行通过（`OK x=6`）；S3-b 双轨门禁（parser / parser-dump / mir / build LLVM parity）全绿无回退。缺口 3 阻塞 `src/std/fs.no read-bytes`（`size ?= fs.fstat-size(.fd)`）等内置 `?=` 场景，是 L2 自举侧 mass-noop 的残根。

**✅ 自举侧「次生缺陷」经复核不成立（2026-10-25），无需改码**：`compiler/src/drv.no` 的 `nil -> got = true` 是否算缺陷，取决于 `fs.read-bytes` 的 `nil` 语义。当时的判断建立在**坏的** `read-bytes` 上（读取失败也回 `nil`），故看起来「把失败吞成空档」。缺口 4 修复后 `nil` 只表示「空档读取成功」、`err` 才表示失败，实测两侧语义一致：

| 场景 | 自举 `noc ast` | Go `bin/no ast` | 一致 |
| --- | --- | --- | --- |
| 空档 | `Program{}` rc=0 | `Program{}` rc=0 | ✅ |
| 缺档 | `cannot read …` rc=1 | `no such file` rc=1 | ✅（措辞异、退出码同） |

⇒ `nil -> got = true` 是**正确**语义，改成 `EXIT-FAIL` 反而会破坏「空档 ⇒ `Program{}`」这个与 oracle 一致的行为。**真正需要修的只有缺口 4**；本条保留以免日后重复排查。

⇒ 两件事：
1. **L2 是手动门禁，不在 CI 内** ⇒ 这个 mass-noop 从 2026-10-22 起一直没被发现。**已补 G8（2026-10-24）**：`scripts/selfhost_diff.sh` 的 `guard_level` 新增「SUT 在整批语料上只产生 1 种 stdout ⇒ `MASS-NOOP`」断言；SUMMARY 视为 FAIL，并额外打印「不是 N 个独立差异，是本层整体退化」。G8 与既有 G2（SUT 全挂 rc≠0）分工：G8 抓的是「每档都跑得动、却吐同一份东西」——更危险，因为它看起来像有讯号。
   - 验证：mass-noop SUT ⇒ `[G8] … MASS-NOOP` / `RESULT: FAIL` ✅；正常 SUT（`noc-se`）⇒ 不触发，仍报 `SAME=10 DIFF=10` ✅；`--self-test` ⇒ 不触发，`SAME=20 PASS` ✅（self-test 契约未被破坏）。
2. **S7 后续工作必须先让主编译器的文件 I/O 恢复**（不是修 `compiler/`），否则任何收敛数字都不可信。

**建议**：S3-c 与 S7 正交轨**合并排期**——先做 S3-c（同时收 V4 合规 + 33 档自举残差），再做安全索引降糖的同类迁移（再收 ~26 档）；**前置**是先修掉主编译器 WIP 的文件 I/O 回归（`file.read-bytes` 回 `nil`）——它当前让自举侧任何收敛数字都不可信。

---

## 14. 关键文件索引

| 类别         | 文件 |
| ---------- | --- |
| 主改点        | `src/parser/parser.go`、`lowering.go`、`tohir.go`、`match.go`、`match_warn.go`、`ast.go`、`resolver.go`、`astdiskcache.go`、`clone.go` |
| 副表         | `src/parser/fmttrivia.go`、`anntable.go`、`typefacts.go`、`nodefacts.go` |
| checker    | `src/checker/checker.go`、`sugar_walk.go`、`match_exhaustive.go`、`match_arm_reach.go`、`lint.go`（Stage 6A 拆分）、`lint_split_test.go` |
| 装配与消费方     | `src/build/transpiler*.go`、`src/cmd/no/main.go`、`src/tools/*`、`src/lsp/*.go`、`src/fmt/*.go` |
| MIR        | `src/mir/hir2mir.go` |
| 门禁与 oracle | `src/parser/dump/*_test.go`、`src/parser/hir_cache_equivalence_test.go`、`src/hir/sugar_kinds_test.go`、`src/build/ast_write_gate_test.go`、`src/mir/hir_capture_mir_test.go`、`src/build/capture_llvm_parity_test.go`、`scripts/mir_golden.sh`、`scripts/selfhost_diff.sh` |
| 自举         | `compiler/src/ps.no`、`compiler/src/ast.no`、`compiler/src/asp.no`、`docs/design/nolang-selfhost.md` |
