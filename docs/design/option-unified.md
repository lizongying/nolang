# 方案：算术与索引统一返回 option

> 状态：**待实施**（设计决策已与用户确认，见 §1）
> 涉及层：parser / checker / mir / lsp / fmt / build / 语料迁移
> 相关既有文档：`docs/docs/lang/syntax.md`〈整数运算溢出〉〈可空类型(option)〉、`NOLANG-OWNERSHIP-MODEL.md`

---

## 1. 已确认的设计决策

| # | 决策 | 结论 |
| --- | --- | --- |
| D1 | `#{overflow=…}` / `#{index-out=…}` 注解 | **保留，但只影响行为、不解包**。仍可写、仍生效（overflow 决定 wrap/clamp0/min/max/saturate；index-out 决定越界取值），但**结果类型恒为 `?T`**。注解**不是**消费路径 —— 仍须 `?=` 或 match 解包。 |
| D2 | 诊断分级 | 产生 option 处（`ovfhndld` / `idxhndld` / `ovf-int-default`）由 **ERROR → HINT**；"既没上抛、也没解包"另立 **ERROR**（新 trace `optunuse`）。 |
| D3 | `no fmt --fix=<新类>` 修复形态 | **能上抛就上抛，否则 match 解包**：所在函数已有 `?T` 结果参数 → `x ?= <expr>`；否则 → `__o = <expr>` + `x = __o: { ok -> it, -> }`（空臂 = 默认零值，D4）。 |
| D4 | 空 `->` 臂语义 | **默认零值**（0 / 0.0 / false / '' / 空容器 / 零值结构体）。它同时充当**兜底臂**，满足 match 穷尽性。 |
| D5 | `-> return` | **新增**：值-match 的臂体为 `return` 时直接从当前函数返回，该臂不参与"各臂值类型一致"检查。 |
| D6 | 统一规则作用范围 | 整数 `+ - * / %`、容器索引 `arr[i]`、`str`/`txt` 索引 `s[i]`。**不含**浮点算术、`char` 算术（保持现状）。 |
| D7 | in-bounds 豁免 | **取消**。`bounds.go` 的"可证明在界内"分析整套下线；`for i <- [0..N) { x = arr[i] }` 也照常产生 option，不处理即报错。惯用替代写法是**直接迭代容器**：`for i <- arr { x = i }`。 |
| D8 | match 穷尽性 | **规则不变**：必须能覆盖 `ok`/`nil`/`err`。但 `->` **兜底臂即可满足**，所以 **`nil`/`err` 臂可以不写**——`{ ok -> it, -> }` 合法，`{ ok -> it }`（无兜底）仍报 `must handle all branches`。注解不改变这一点。 |

### D1 的直接后果（必须写进预期）

注解不再是消费路径，意味着：

- `#{overflow=wrap}` ⏎ `x = a + b` ⇒ `x : ?i64`（**不再**是 plain `int`）。要拿到值必须 `x: { ok -> it, … }` 或 `x ?=`。
- `#{index-out=zero}` ⏎ `x = arr[i]` ⇒ `x : ?elem`（**不再**是 plain `elem`）。
- 因此 **`src/std` 的 3557 个 `#{overflow}` 与 1007 个 `#{index-out}` 站点本次全部需要迁移**（这正是原需求里"可能需要插入很多前面的解包操作"所指）。
- §2.2 里"std 已 100% 标注 ⇒ 本次无需改动"的推论**作废**，见修订后的 §2.2。

### 尚待定案（见 §8.1）

- **D1-b**：`s[i]` 变 `?char` 的影响面最大（`src/std` 大量文本解析），先按 §7.1 测量再决定是否分期。

---

## 2. 现状（实测，2026-10-12，`f0c71559`）

### 2.1 今天谁产生 option、谁报错

| 写法 | 现状 | 说明 |
| --- | --- | --- |
| `x = a + b`（未标注，非 `?T` 函数） | **ERROR** `ovfhndld` | 纯算术不触发就地捕获 |
| `x = a / b`（有符号，未标注） | `x : ?T`（自动） | `inferOptionDivMod` 合成 `?T` 注解 |
| `x = arr[i]`（非 `?T` 函数） | **ERROR** `idxhndld` | 索引捕获被 `enclosingFuncHasOptResult()` 挡住 |
| `x = arr[i]`（`?T` 函数内） | `x : ?elem`（自动） | 就地捕获 |
| `x = s[i]`（str） | `x : char`（plain） | `isSafeIndexBase` 明确排除 str/txt |
| `#{overflow=wrap}` ⏎ `x = a + b` | plain `int` | 注解解包 |
| `#{index-out=zero}` ⏎ `x = arr[i]` | plain `elem` | 注解给默认值 |
| `x ?= a + b` | 上抛 | 要求函数有 `?T` 结果参数 |

结论：**规则不统一**——同样是"算术/索引绑定到新变量"，`/ %` 自动成 option、`+ - *` 报错、索引视所在函数而定。

### 2.2 语料规模

| 位置 | 文件 | 行数 | `#{overflow}` | `#{index-out}` | `?=` | 索引表达式（≈） | `.byte(` |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `src/std` | 118 | 75 156 | 3 557 | 1 007 | 2 | ~651 | 544 |
| `tests` | 528 | 29 241 | 212 | 93 | 27 | — | — |
| `compiler` | 62 | — | — | — | — | — | — |

`./bin/no vet src/std` 当前 **0 error / 0 warning / 145 hint**——即 std 已 100% 标注完毕。
**但这不是"本次无需改动"**：因为 D1 规定注解只影响行为、不解包，std 的 **3557 + 1007 = 4564 个
已标注站点本次全部会从 plain 变成 `?T`**，全部需要补消费（`?=` / match / `_ =`）。
迁移面因此有四块：

| 来源 | 站点数（std） | 说明 |
| --- | --- | --- |
| 已标注 `#{overflow}` | 3 557 | D1：不再解包 ⇒ 变 `?T` |
| 已标注 `#{index-out}` | 1 007 | D1：不再解包 ⇒ 变 `?T` |
| 未标注但当前不报错的索引读取 | 待测 | D7：in-bounds 豁免取消后新增 |
| `str`/`txt` 索引 `s[i]` | 待测 | D6：`char` → `?char` |

后两块的数量必须先用 §7.1 的探针测出来，再决定 §8 的分期。前两块的数量是确定的，
是本次迁移的主体工作量（std 一处平均约 4 行，合计约 1.8 万行）。

### 2.3 相关实现位置（关键锚点）

| 关注点 | 位置 |
| --- | --- |
| 算术/索引绑定推断 | `src/parser/lowering.go`：`inferOptionDivMod`(506)、`maybeCaptureOptionAssign`(712)、`collectCaptureNodes`(642)、`captureInfixInner`(578)、`synthesizeNullableType`(553) |
| 安全索引判定（三方共用） | `src/parser/type.go`：`isSafeIndexBase`(311)；`src/parser/bounds.go`：`AnalyzInBoundsForFunc`、`p.inBoundsIdx` |
| 零值合成（可复用） | `src/parser/lowering.go`：`zeroValueLiteralFor`(1333) |
| 值-match 臂校验 | `src/parser/desugar.go`：`validateMatchArmReturns`(135)，错误在 151 行 |
| match 展开 | `src/parser/lowering.go`：`lowerSurfaceMatch`(1657) |
| 未处理溢出检查 | `src/checker/checker.go`：`ValidateUnhandledOverflow`(4523)、`ValidateUnhandledIndex`(4855)、`isDirectOverflowValue`(4393) |
| 诊断接线 / 严重度 | `src/checker/lint.go`：20c(507)、20d(538)、20e(558) |
| 批量修复 | `src/cmd/no/main.go`：`fmtOverflowFixes`(1556)、`fmtProcessFixFile`(1459) |

---

## 3. 目标语义规格

### 3.1 产生 option 的表达式

1. **有符号整数算术** `+ - * / %`：`i8..i128`。溢出 → `err`；`/ %` 除零 → `err`。
2. **无符号整数算术** `+ - *`：与现状一致，同样产生 option（`/ %` 不产生）。
3. **容器读取** `arr[i]`：定长数组 / 切片 / vec；越界 → `nil`。**含当前可证明在界内的读取**（D6）。
4. **`str` / `txt` 索引** `s[i]`：越界 → `nil`，成功 → `ok(char)`（D6）。

不产生 option（保持现状）：浮点算术、`char` 算术、位运算、比较、`&&`/`||`、
`str` 的 `-`/`+` 拼接、`.byte(i)` 方法调用。

> **注解不改变上述分类（D1）**：`#{overflow=…}` / `#{index-out=…}` **不改变结果类型**，
> 只改变"失败时怎么取值"。`#{overflow=wrap}` ⏎ `x = a + b` 的 `x` **仍是 `?i64`**，
> 只是溢出时 `ok` 的 payload 是回绕值而非 `err`。

### 3.2 绑定与类型推断（统一规则）

```
x = <产生 option 的表达式>      ; x 为新变量、无显式类型注解
=> x : ?T                       ; T 为内部类型
```

- 与今天的差异：`x = a + b` 不再报错，而是 `x : ?i64`；`x = arr[i]` 在任何函数里都是 `x : ?elem`；
  `x = s[i]` 是 `x : ?char`。
- **`#{overflow=…}` / `#{index-out=…}` 不影响本推断**：带注解的陈述同样得到 `?T`（D1）。
  这是与今天最大的行为差异 —— 今天注解会把结果变成 plain，现在不会。
- 显式**类型**注解优先：`x i64 = a + b` 仍报 `cannot assign ?i64 value to i64 variable`。
- 已存在的 plain 变量仍是错误（`x i64 = 0` 之后的 `x = a + b`）。

### 3.3 消费 option 的三条合法路径

1. `x ?= expr` —— 上抛（要求所在函数有 `?T` 结果参数）。
2. `x: { ok -> … , -> }` —— match 解包。`nil`/`err` 臂**可以不写**（D8），`->` 兜底臂即可
   满足穷尽性（由既有 `match_exhaustive.go` 保证）。
3. `_ = expr` —— 显式丢弃。

> **注解不是消费路径（D1）**。`#{overflow=…}` / `#{index-out=…}` 只影响行为，
> 不会让"未消费"变成合法。写了注解仍然要 match 或 `?=`。

### 3.4 诊断分级（D2）

| 场景 | 现状 | 目标 | trace |
| --- | --- | --- | --- |
| 未标注整数算术（"建议加注解"） | ERROR | **HINT** | `ovf-int-default` |
| 未标注算术绑定到新变量 | ERROR | **HINT**（"该变量是 ?T，记得 match 或 ?="） | `ovfhndld` |
| 未标注索引读取绑定到新变量 | ERROR | **HINT** | `idxhndld` |
| **option 既没上抛也没解包**（含"写了注解但没 match"） | — | **ERROR** | `optunuse`（新） |
| 区间索引惯用法建议（`for i <- [0..N) { x = arr[i] }`） | — | **HINT**（建议 `for i <- arr { x = i }`） | `rngidx`（新） |
| option 赋给 plain 变量 / 传给 plain 形参 | ERROR | ERROR（不变） | 既有 `ValidateTypes` |

`optunuse` 的判定（单趟、函数作用域内）：
收集本函数内由 §3.1 规则产生、且被绑定到局部变量的 option 值；对每个这样的变量检查它是否
出现在 `?=` 左侧、`x: {…}` 的匹配主体、或 `_ =` 右侧。三者皆无 → 在**产生处**报 ERROR。
**注解不参与判定**（D1）。

> 之所以要新立 `optunuse` 而不是把 `ovfhndld` 留在 ERROR：D2 要求产生处是 HINT（因为后面
> 通常确有处理），而"完全没处理"必须继续硬拦——这是两条不同的规则，不该共用一条 lint。

---

## 4. 语法扩展

### 4.1 `-> return`（早退臂，D5）

```no
ao = 1 + 2
a = ao: {
    ok -> it
    -> return
}
```

- 解析：`parseMatchExprFrom` 的 inline 分支当前走 `parseExpression(LOWEST)`，`RETURN` 关键字
  在此不被识别（实测 `vet` 报 `match arm in expression context must end with an expression`）。
  需在 inline 分支加 `case lexer.RETURN: stmt = p.parseReturnStatement()`。
- 语义：该臂直接 `return`（void 返回；若函数有结果参数，由既有 `ValidateUnassignedReturns` 管）。
- 校验：`validateMatchArmReturns`（`desugar.go:151`）需把"末条为 `ReturnStatement`（或含 `return`
  的 `BlockStatement`）"的臂视为 **不产出值**，跳过它、不参与"各臂类型一致"比较；
  其余臂仍需一致。

### 4.2 空 `->` 臂 = 默认零值 + 兜底（D4 / D8）

```no
ao = 1 + 2
a = ao: {
    ok -> it
    ->
}
```

- **`else` 可以不写**：`nil` / `err` 臂无需显式列出，`->` 兜底臂已经覆盖它们。
  实测：`{ ok -> it, -> 0 }` 通过并输出 `3`；`{ ok -> it }`（无兜底）报
  `option match must handle all branches: err, nil, ok (missing: err, nil)`。
- 空臂在 `lowerSurfaceMatch` 展开时，按**被匹配 option 的内部类型**合成零值表达式
  （直接复用 `zeroValueLiteralFor(tok, elem)`，与 `#{index-out=zero}` 同一张表）。
- 内部类型由 `inferIndexElemType` / `inferExprType` 得到；推断不出时**报错**（不猜）。
- 注意 `validateMatchArmReturns`（`desugar.go:143`）现在会把"空臂"判成
  `match arm has no body` —— 零值合成必须发生在该校验**之前**，或让校验先放行空臂。
- `fmt` 需原样打印空臂（不能补成 `-> 0`，否则重跑不幂等）。

> 这一条把解包写法的**标准形态**压到 4 行 —— 也是 §6 批量修复产出的形态。

### 4.3 fmt 打印

- `-> return`：单行臂。
- 空臂：打印 `->` 后换行。
- 两者都要进 `src/fmt` 的 roundtrip / idempotent 测试。

---

## 5. 编译器改造清单

### 5.1 `src/parser`

| 文件 | 改动 |
| --- | --- |
| `lowering.go` `inferOptionDivMod` | 更名 `inferOptionArith`；运算符集合 `/ %` → `+ - * / %`；`signedIntOperandType` 判定不变（排除 `str` 拼接、浮点、char、无符号 `/ %`）。**删除**开头两个注解短路（`s.OverflowMode != ""` 与 `RawAnnotationsOf` 的 `overflow` 检查）——D1：注解不再阻止推断。 |
| `lowering.go` `captureInfixInner` | `+ - *` 也视为"自身即 option 的可错源"（操作数皆为有符号整数即可，**不再要求"无注解"**）。 |
| `lowering.go` `collectCaptureNodes` | `*InfixExpression` 分支按上式放宽；`*IndexExpression` 分支**删除** `l.enclosingFuncHasOptResult()` 门（索引捕获全函数生效）。 |
| `lowering.go` `maybeCaptureOptionAssign` | ①根为纯算术 Infix 且 `len(nodes)==0` 时改为**整体捕获**（合成 `?T` 注解），而不是返回 nil；②**删除**所有注解短路（`ls.OverflowMode != ""`、`l.enclosingOvf`、`RawAnnotationsOf` 的 `overflow` / `index-out` 分支）——D1：注解不再豁免。 |
| `lowering.go` `maybeIndexOutAssign`(959) | `#{index-out=DEF}` 的语义由"改成 plain + 默认值"改为"**结果仍是 `?elem`**，越界时 `ok(DEF)` 而非 `nil`"。不再合成 plain 类型。 |
| `lowering.go` `synthesizeNullableType` | 内部类型来源补上 str/txt 索引（`char`）。 |
| `type.go` `isSafeIndexBase` | ①接受 str/txt 基底（`inferIndexElemType` 返回 `char`）；②**删除** `p.inBoundsIdx[idx]` 早退（D7）。 |
| `bounds.go` | **整套下线**（D7）：`AnalyzInBoundsForFunc`、`p.inBoundsIdx`、`p.recordIndexLocalType` 中的 in-bounds 记录、`checker.ValidateUnhandledIndex` 的 in-bounds 分支、`fmt.AnalyzeInBoundsIndex` 与"删除冗余 `#{index-out}`"逻辑。净删代码，但要连带清理测试。 |
| `desugar.go` `validateMatchArmReturns` | 允许 `return` 结尾臂（§4.1）。 |
| `lowering.go` `lowerSurfaceMatch` | 空臂合成零值（§4.2）。 |
| `expr.go` `parseMatchExprFrom` | inline 臂体支持 `return`（§4.1）。 |

### 5.2 `src/checker`

- **合并** `ValidateUnhandledOverflow`(4523) 与 `ValidateUnhandledIndex`(4855) 的遍历为一条
  "option 生命周期"趟：
  - 产生处 → 记入本函数"待消费集合"，产出 **HINT**（`ovfhndld` / `idxhndld`）。
  - 消费处（`?=` / `x: {…}` / `_ =`）→ 从集合移除。**注解不参与**（D1）。
  - 趟末仍未消费 → **ERROR** `optunuse`（D2）。
- **删除** `stmtOverflowAnnotated`(4346) 与 `effOverflow` 的"注解即豁免"传递逻辑：注解不再让
  子树的算术免于检查（D1）。`isDirectOverflowValue`(4393) 保留（仍用于判定"是不是整数运算"）。
- `lint.go`：20c(507)、20d(538)、20e(558) 的 `Severity` 由 `LintError` → `LintHint`；新增
  `optunuse`（ERROR）与 `rngidx`（HINT，建议 `for i <- arr { x = i }`）的接线。
- **不动**：`ValidateTypes` 的 `cannot assign ?T value to T variable`（这是"用在 plain 语境"的硬错误，保留）。
- **不动**：`match_exhaustive.go` 的穷尽性规则本身（D8：`->` 兜底臂即可满足穷尽性，
  规则不变，只是**不必**写 `nil`/`err`）。
- `ValidateUninitOutputParams` / `ValidateUnassignedReturns` 需复核：新增 `-> return` 后
  "某臂早退" 不应被判成"未赋值返回"。
- 新增 `rngidx` 检查：识别 `for <v> <- [a..b) { … <arr>[<v>] … }`（`arr` 与 `<v>` 同源、且
  `arr` 的静态长度已知）并报 HINT，建议改写为 `for <v> <- arr { … <v> … }`。

### 5.3 `src/mir` / codegen

- `+ - *` 的 option 路径**已存在**（实测 `ao ?i64 = a + b` 正常输出 `15`）；`/ %` 的除零守卫、
  溢出守卫均已在位。本次主要是**放开触发条件**，不是新造机制。
- **新增**：`str`/`txt` 索引的 option 化。当前 `emitIndex` 对 str 走"码点读取、返回 char"；
  需改为产出 `%option_char`（越界 → nil），并保持 `s.byte(i)` 不受影响。
- 复核 `shouldUseMemcpy` / `computeTypeSize`：新增 `?char` 会改变尺寸分类，凡改动
  `shouldUseMemcpy` 结果者必须跑全语料（见 skill 中的告诫）。

### 5.4 `src/lsp`

- 严重度映射跟随 lint（HINT 显示为灰色波浪线，不再阻断）。
- quickfix：现有"Add `#{overflow = wrap}`"保留；**新增**"Unwrap with match"，插入
  `x = __o: { ok -> it, -> 0 }` 骨架。

### 5.5 `src/fmt`

- §4.3 的打印改动。
- 新增 `--fix=unwrap` 类（见 §6）。

---

## 6. 批量迁移：`no fmt --fix=unwrap`

在 `src/cmd/no/main.go` 的 `fmtOverflowFixes`(1556) 增加分支 `case "unwrap"`，实现新文件
`src/cmd/no/unwrap_fix.go`。要求与既有 fix 类同风格：**手术式文字插入、不改排版、不改其它注解、重跑幂等**。

### 6.1 修复算法

对每个被报告的站点（来源＝同 package 的合并 vet，与 `fmtOverflowFixes` 一致）：

```
; 情况 A：所在函数已有 `?T` 结果参数（resultName 非空）
x = <expr>                      =>   x ?= <expr>

; 情况 B：否则（标准形态，4 行）
x = <expr>                      =>   __o<N> = <expr>
                                     x = __o<N>: {
                                         ok -> it
                                         ->
                                     }
```

- 用**空兜底臂**而非显式 `nil -> 0` / `err -> 0`（D8：`else` 可以不写），产物最短且幂等。
- 默认臂零值由 `zeroValueLiteralFor` 的同一张表决定（`i64→0`、`f64→0.0`、`bool→false`、
  `str→''`、`char→0`、容器 → 空容器、结构体 → 零值结构体）。
- **已带 `#{overflow=…}` / `#{index-out=…}` 的陈述照常处理**（D1：注解不解包，所以它们
  同样是"未消费"，是本次迁移的**主体**）。注解**原样保留**在原陈述上方 —— 它仍承担
  "失败时怎么取值"的行为职责，只是不再解包。
- `x` 已存在且非 `?T`：跳过并在 stderr 记一行"需人工处理"（不猜语义）。
- 变量名 `__o<N>`：`__` 前缀在 Nolang 源码中不可书写（见 skill），不会与用户符号冲突。
- 写索引 `k[i] = v` 与读索引分别处理：写路径本就无 option（除非下标表达式本身产生），
  不要误改。

### 6.2 迁移顺序

1. `src/std`（118 文件）
2. `tests`（528 文件）
3. `compiler`（62 文件）、`example`、`test`
4. `tmp/`（1084 文件，可最后、可批量）

每批之后跑 `./bin/no vet <dir>` 确认 ERROR 归零，再进下一批。

### 6.3 不做的事

- 不跑 `no fmt -w` 全量重排（diff 不可审）。注：D7 下线 in-bounds 分析后，`no fmt` 也不再
  会"删除冗余 `#{index-out}`"，此前的这条顾虑消失，但仍不批量重排。
- 不自动删除既有 `#{overflow=…}` / `#{index-out=…}`（D1 保留其行为职责）。
- 不自动改写函数签名为 `?T`（保守；`?=` 只在**已有** `?T` 结果参数时使用）。
- 不自动把 `for i <- [0..N) { x = arr[i] }` 改写成 `for i <- arr { x = i }`——只报 `rngidx` HINT，
  改写交人工（两者语义不完全等价：前者需要下标本身）。

---

## 7. 验证与门禁

### 7.1 M0 探针（先测量，再动刀）

新规则的判定**无法用现有二进制测**（现有二进制把注解当豁免）。做法：把 §5.1/§5.2 的判定
先落在 `NOLANG_OPT_UNIFIED=1` 后面，用同一个二进制跑两遍语料做差集：

```bash
for d in src/std tests compiler example; do
  NOLANG_OPT_UNIFIED=1 ./bin/no vet "$d" 2>&1 \
    | grep -oE '\[(ovfhndld|idxhndld|optunuse|rngidx)\]' | sort | uniq -c
done
```

要拿到四张表：

| # | 统计对象 | 用途 |
| --- | --- | --- |
| ① | 已标注 `#{overflow}` / `#{index-out}` 站点（std = 4564，已确定） | 迁移主体工作量 |
| ② | in-bounds 豁免取消后新增的索引读取（D7） | 决定是否要为 `[N]T` + 字面量区间单独留后门 |
| ③ | `str`/`txt` 索引 `s[i]`（D6 / D1-b） | 决定是否分期 |
| ④ | 纯算术绑定（`x = a + b`） | 主体工作量 |

**①④是确定的，②③决定 §8 的分期。**

### 7.2 单元测试

```bash
cd src && go test ./...          # 全量；注意 build/build-js/build-wasm 有已知既有失败
```

新增/改写的测试：

- `src/parser/option_arith_infer_test.go`：`+ - * / %` 全部推断 `?T`；`str` 拼接、浮点、char、
  无符号 `/ %` **不**推断（负对照，防止"全都变 option"）。
- `src/parser/option_index_capture_test.go`：非 `?T` 函数内 `x = arr[i]` 推断 `?elem`；
  str/txt → `?char`；`#{index-out}` 仍为 plain（负对照）。
- `src/parser/match_early_return_test.go`：`-> return` 解析；空臂零值合成；fmt 幂等。
- `src/checker/option_lifecycle_test.go`：产生处 = HINT；未消费 = ERROR `optunuse`；
  `?=` / match / `_ =` 三条路径各一个豁免用例，外加一个"只写注解、没 match ⇒ 仍报 `optunuse`"
  的反例（D1 的关键回归钉）。
- `src/checker/match_exhaustive_test.go`：`{ ok -> it, -> }`（空兜底臂）**合法**；
  `{ ok -> it }`（无兜底）**报错** `must handle all branches`；写不写注解都不改变这两条
  （D8 回归钉）。
- `src/checker/unhandled_*_test.go`：既有用例按新严重度更新（**不是**删掉断言）。

### 7.3 全语料门禁

```bash
# 1) 行为回归（rc + stdout 指纹）
GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default bash scripts/mir_golden.sh

# 2) 编译回归（只编译，无超时）
# 3) vet 回归：ERROR 集合与对照二进制逐行 diff（必须为空）
diff <(grep -E '\[ERROR\]' /tmp/cur.vet.out) <(grep -E '\[ERROR\]' /tmp/ctl.vet.out)
```

⚠️ 按 skill 的纪律：
- A/B 必须**同一快照**各建 CTL/SUT（并行 session 常驻，工作树含他人未提交改动）。
- 不比 stderr（裸 `go build` 会吐 compiler version mismatch）；断言失败仍 rc=0，**必须 hash stdout**。
- 假通过检测：控制组 rc=0 文件数、空 stdout 的 sha256 `e3b44298fc…` 计数。
- 一次只跑一个 sweep（`$WORKDIR=/tmp/mir_golden` 硬编码）。

### 7.4 黄金语料刷新

`tests/*.no` 被批量改写后，`tests/golden/mir-baseline.tsv` 必须**手工 merge**（不跑 `-update`），
并断言 `set(new) == set(cand)` 且逐条相等。

---

## 8. 风险与取舍

### 8.1 已定案的取舍

**(a) in-bounds 豁免：取消（D7）。** 理由：定长数组 + 字面量区间的"在界内"判定**范围不好
界定**（要证明下标恒在 `[0,N)` 需要绕开重绑定、嵌套同名 for-range、切片/vec 的未知长度…），
与其留一个半可信的豁免，不如统一按"可能越界"处理并照常报错。
- `bounds.go` 整套下线（净删代码），`no fmt` 的"删除冗余 `#{index-out}`"一并下线。
- 惯用替代写法是**直接迭代容器**：`for i <- arr { x = i }`（`i` 即元素，无 option）。
  需要下标本身时保留 `i <- [0..N)` 并对 `arr[i]` 显式解包。
- 新增 `rngidx` HINT 引导这个改写（§5.2）。

**(b) `s[i]`：先测量再分期（D1-b）。** 影响面最大（`src/std` 大量文本解析，且历史上刚把
109 处 `s[i]` 改成 `s.byte(i)`）。按 §7.1 的 ③ 测出数量：
- < ~50 处 → 与主体同期做；
- 上百处 → 拆成独立里程碑：先落主体（算术 + 容器索引），再落 str/txt 索引。

**(c) 穷尽性：规则不变，但 `else` 可以不写（D8，已定案）。** match 仍必须能覆盖
`ok`/`nil`/`err`，而 `->` 兜底臂即可满足；所以标准解包形态压到 4 行：

```no
a = ao: {
    ok -> it
    ->
}
```

不必列 `nil` / `err`。注解不改变这一点。空兜底臂的零值语义见 D4，批量修复产出的正是这个形态。

### 8.2 其它风险

| 风险 | 表现 | 缓解 |
| --- | --- | --- |
| **已标注站点迁移量巨大** | std 4564 处 `#{overflow}`/`#{index-out}` 全变 `?T`，每处要补 match | `--fix=unwrap` 幂等批量；每批 vet 归零再进下一批 |
| `?T` / plain 混用错误激增 | 大量 `cannot assign ?i64 value to i64 variable` | `--fix=unwrap` + 先测量 |
| `?=` 适用范围受限 | `?=` 要求函数有 `?T` 结果参数，多数函数没有 | fix 走情况 B（match），不改签名 |
| 空兜底臂的零值猜错 | `->` 合成零值，但原语义未必是零（如越界本该走另一分支） | fix 只对**新报告**的站点生效；已标注站点走 D1 路径（失败臂本就不触发） |
| `?char` 改变 `shouldUseMemcpy` | 编译期/运行期行为漂移 | 改尺寸相关代码后必须跑全语料 |
| 删 `bounds.go` 连带回归 | `no fmt` 少了一条删注解能力；既有 bounds 测试失效 | 同期删/改测试；golden 覆盖 |
| 诊断严重度下调漏掉真错 | 真正的"沉默泄漏"被 HINT 掩盖 | 新 ERROR `optunuse` 兜底 |
| fmt 幂等被空臂破坏 | 重跑 `no fmt` 反复改写 | roundtrip 测试 + 空臂原样打印 |
| 迁移把 std 改坏 | 运行时行为变化 | 每批 vet + 全语料 golden |

### 8.3 回滚

改动跨 parser/checker/mir/fmt，且语料被批量改写，**不是单点回滚**。策略：
- 语义改动（§5.1–5.3）与语料迁移（§6）**分成不同 commit**，语义 commit 单独可 revert。
- 语义改动尽量集中在少数几个谓词（`inferOptionArith` / `isSafeIndexBase` / 诊断严重度），
  便于用"谓词返回旧值"的方式做对照二进制（skill 的标准手法）。

---

## 9. 里程碑与实施顺序

| 阶段 | 内容 | 出口判据 |
| --- | --- | --- |
| **M0** 测量 | §7.1 探针（`NOLANG_OPT_UNIFIED=1`）；确定 D1-b 分期 | 四张计数表 + 分期决定 |
| **M1** parser 语义 | `inferOptionArith`、删注解短路、索引捕获去门、`isSafeIndexBase` 改、`maybeIndexOutAssign` 语义改 | `go test ./parser/...` 绿；新单测（含负对照） |
| **M1b** 删 bounds | `bounds.go` 整套 + `inBoundsIdx` + fmt 的 `AnalyzeInBoundsIndex` 下线 | 相关测试清理完毕；`go test ./...` 绿 |
| **M2** checker 诊断 | 合并生命周期检查、删 `effOverflow` 豁免、严重度下调、`optunuse` + `rngidx` | `go test ./checker/...` 绿；vet ERROR 集合可解释 |
| **M3** 语法 | `-> return`、空臂零值、fmt 打印 | parser/fmt 测试绿；roundtrip 幂等 |
| **M4** MIR | str/txt 索引 option 化（按 M0 决定是否同期） | 全语料编译通过；尺寸相关测试绿 |
| **M5** fix | `--fix=unwrap` | 幂等；对语料 dry-run（`-d`）人工抽检 |
| **M6** 迁移 std | 按 §6.2 顺序（主体：4564 处已标注站点） | `no vet src/std` ERROR=0；golden 无 REGRESS |
| **M7** 迁移 tests | 同上 | 全语料 golden 只出现**预期内**的 diff |
| **M8** 收尾 | 文档（`docs/docs/lang/syntax.md` CN + `docs/i18n/en/...` EN）、golden 手工 merge | 全部门禁绿 |

---

## 10. 文档同步

- `docs/docs/lang/syntax.md`〈整数运算溢出〉〈安全索引〉〈可空类型(option)〉需重写：
  - 删除"纯算术不触发捕获"的说明；
  - 统一规则 + **三条**消费路径（`?=` / match / `_ =`）+ 诊断分级表；
  - **明确写出注解只影响行为、不解包**（`#{overflow=wrap}` ⏎ `x = a + b` 的 `x` 仍是 `?i64`）；
  - `-> return` / 空臂零值；
  - in-bounds 豁免取消，并给出 `for i <- arr { x = i }` 这一惯用替代写法。
- EN 镜像 `docs/i18n/en/.../lang/*.md` 同步（历史上 EN 常落后，需显式跟进）。
- `docs/docs/lang/memory.md`（若有 option/ownership 相关段落）复核。
