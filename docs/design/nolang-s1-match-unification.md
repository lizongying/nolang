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

### 1.4 唯一的实质缺口
- **`build/transpiler` 不认识 `SurfaceMatch`**：`transpiler_stdload.go:159,675` 只有 `case *parser.IfExpression` 且读 `ex.MatchedExpr`/`ex.RawCond`/`ex.EqualityPattern`；`transpiler_monomorph.go` 同理（10× 读 12 字段）。`build/transpiler.go` 在管线中位于 **checker 之后、H0(tohir) 之前**（见 invariants §2.1），作用在 AST 上，因此默认路径若保留 SurfaceMatch，transpiler 必须加 `SurfaceMatch` case，否则崩溃。

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

- **G-equivalence**：`parser/dump/h0_sugar_gap_test.go` —— H0 的 `SurfaceMatch` 路径与旧 in-lowering 路径产出**逐字节相同 HIR**。每次 P1–P6 改动后必跑。
- **G-corpus**：532 语料 `no fmt` round-trip 冪等 + LSP lint/index 冒煙（沿用 §4.3 steps 1-4 留下的糖形态单形态冒煙测试）。
- **G-compile**：`no build tests/str.no` / `basic.no` / `str-test.no` EXIT=0（重建 `bin/no` 须注入 `StdSigKey`）。
- **G-unit**：`go test` parser/checker/fmt/lsp/tools/cmd/no（注入 `StdSigKey`）全绿。
- **G-ast-freeze**（C1/C6）：`SurfaceMatch` 在 `ParseProgram` 返回后不被任何阶段写入字段（§5 删除即消除 12 字段写点）。

---

## 8. 立项状态

- [x] §4.3 steps 1-4 已落地（commit `583791e1`）
- [x] 调查证伪「步骤 5 局部清理」，定位为 S1
- [x] 现状事实厘清（parser 只产 SurfaceMatch；H0 desugar 已实现证等价；checker 已双形态；缺口仅在 build/transpiler）
- [ ] 待评审批准 → 进入 P1 实施（首阶段打通端到端 + 补 transpiler SurfaceMatch case）
- [ ] P2–P7 按阶段推进，每阶段独立提交 + 门禁
