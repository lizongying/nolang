# 方案：算术与索引统一返回 option

> 状态：**实施中**（M1a/M2 已落地；D7-R1 已落地：隐式验证 —— 可证明安全即消除 option 包装/解包/分支）；D9 空兜底臂
> 雙態診斷已落地。M4（str/txt 索引 option 化）已落地 —— §11.14 記錄了它的實測遷移量；M5（`--fix=unwrap`）已落地，
> §11.9 的 `rngidx` 已補上。**D10（複合賦算運算符廢棄）已落地**：結果是 option 的函式裡
> WARNING + `no fmt` 自動重寫為 `a ?= a + 1`；結果不是 option 的**報 ERROR 且不降糖**
> （非 ok 分支只能用戶自己決定），見 §11.18。
> M6 首批迁移完成 —— `src/std` 与 `tests/` 均 0 error。
> 实测勘误见 §11，其中 §11.3/§11.8/§11.10 推翻了正文三处前提，§11.9 已大部分閉合，§11.15 是未閉合项）
> 涉及层：parser / checker / mir / lsp / fmt / build / 语料迁移
> 相关既有文档：`docs/docs/lang/syntax.md`〈整数运算溢出〉〈可空类型(option)〉、`NOLANG-OWNERSHIP-MODEL.md`

---

## 1. 已确认的设计决策

| # | 决策 | 结论 |
| --- | --- | --- |
| D1 | `#{overflow=…}` / `#{index-out=…}` 注解 | **保留，但只影响行为、不解包**。仍可写、仍生效（overflow 决定 wrap/clamp0/min/max/saturate；index-out 决定越界取值），但**结果类型恒为 `?T`**。注解**不是**消费路径 —— 仍须 `?=` 或 match 解包。 |
| D2 | 诊断分级 | 产生 option 处（`ovfhndld` / `idxhndld` / `ovf-int-default`）由 **ERROR → HINT**；"既没上抛、也没解包"另立 **ERROR**（新 trace `optunuse`）。 |
| D3 | `no fmt --fix=<新类>` 修复形态 | **能上抛就上抛，否则 match 解包**：所在函数已有 `?T` 结果参数 → `x ?= <expr>`；否则 → `<x>-opt = <expr>` + `x = <x>-opt: { ok -> it, -> }`（空臂 = 默认零值，D4）。🔴 勘误：暫存名**不能**用 `__o`（`__` 前綴識別字在源碼中不可書寫，見 §11.16），改用 `<x>-opt`。 |
| D4 | 空 `->` 臂语义 | **默认零值**（0 / 0.0 / false / '' / 空容器 / 零值结构体）。它同时充当**兜底臂**，满足 match 穷尽性。 |
| D5 | `-> return` | **新增**：值-match 的臂体为 `return` 时直接从当前函数返回，该臂不参与"各臂值类型一致"检查。 |
| D6 | 统一规则作用范围 | 整数 `+ - * / %`、容器索引 `arr[i]`、`str`/`txt` 索引 `s[i]`。**不含**浮点算术、`char` 算术（保持现状）。 |
| D7 | 可证明安全的表达式 | **不是取消、是隐式验证 + 优化**（D7-R1）。`bounds.go` 一类静默分析照旧上線：編譯器**能證明**不會溢出 / 不會除零 / 不會越界的運算式，**直接消除 option 包裝、消除解包、也消除對應的 match 分支**；LSP 僅在"可能出錯"處提示 warning。用戶定案理由：寫下看似冗餘的程式碼是「有備無患」——不寫就得自己判斷會不會出錯，反而提高思考成本。 |
| D7-R1 | D7 的适用边界 | 只有**靜態可證明安全**的讀取才享受此优化：定長陣列 `[N]T` + 界內字面量/區間下標（`bounds.go`）。切片、`str`/`txt`、動態長度容器、跨函式不可見的下標**一律照常產生 option**。判定由 codegen / `no vet` / `no fmt` 三方共用同一份（`p.inBoundsIdx`），口徑不一致會造成「`no fmt` 刪了註解 → `no vet` 報錯」的假回歸。 |
| D8 | match 穷尽性 | **规则不变**：必须能覆盖 `ok`/`nil`/`err`。但 `->` **兜底臂即可满足**，所以 **`nil`/`err` 臂可以不写**——`{ ok -> it, -> }` 合法，`{ ok -> it }`（无兜底）仍报 `must handle all branches`。注解不改变这一点。 |
| D9 | 空 `->` 兜底臂的双态诊断 | 兜底臂**不能移除**（D8 穷尽性）、**也不改语法**（保留还有一层意义：未来可能出错、也可能正确，语法不用动）。编译器改为把状态告诉用户：可证明主体恒为 ok → **HINT** `arm-unreach`「这个分支不会走到」；无法证明 → **WARNING** `arm-should`「这个分支可能会走到，请决定它该做什么（如 `log('...')` / `print('...')`）」。两者都只提示、不阻断。 |
| D10 | 复合赋算运算符 `+=` `-=` `*=` `/=` `%=` | **提示废弃 +（僅在能上拋處）自動重寫**，按所在函式的結果型別分成兩條路（2026-10-13 用戶定案）：① **結果是 option** ⇒ 每個出現處發 **WARNING**（`nolang-deprecated-compound-assign` / `W_COMPOUND_ASSIGN`），**保存時 `no fmt` 自動改寫**成 `a ?= a + 1`（等價：統一 option 規則下右式是 `?T`，走 `?=` 的解包 + 失敗上拋路徑）；② **結果不是 option（或頂層）** ⇒ **ERROR，不降糖** —— 編譯器沒辦法替用戶決定非 ok 分支怎麼處理，只提示「可以這麼寫」：`a + 1: { ok -> a = it, -> }`，或把結果參數改成 `?T` 後寫 `a ?= a + 1`。實現上 parser 只在 option 結果函式裡把複合賦算降糖成 `UnwrapAssignStatement`（`DeprecatedOp` 記下原運算符），formatter 渲染 surface AST 時輸出的就是重寫後的寫法；非 option 分支只出診斷、AST 不動，所以 `no fmt` 對它原樣退回。位元複合賦算（`&=` / `|=` / `<<=` …）不產生 option，**不在廢棄名單**；`a[i] += 1` / `d.f += 1` 也會重複求值下標，同樣不降糖（維持 parse error）。 |

### D1 的直接后果（必须写进预期）

注解不再是消费路径，意味着：

- `#{overflow=wrap}` ⏎ `x = a + b` ⇒ `x : ?i64`（**不再**是 plain `int`）。要拿到值必须 `x: { ok -> it, … }` 或 `x ?=`。
- `#{index-out=zero}` ⏎ `x = arr[i]` ⇒ `x : ?elem`（**不再**是 plain `elem`）。
- 因此 ~~**`src/std` 的 3557 个 `#{overflow}` 与 1007 个 `#{index-out}` 站点本次全部需要迁移**~~
  （这正是原需求里"可能需要插入很多前面的解包操作"所指）。
  🔴 **勘误（§11.3 实测）**：这些站点**不会**因 D1 而编译失败 —— 「option 赋给 plain 变量」
  目前是静默接受（隐式取 payload），所以它们照旧编译、照旧运行。实测 `src/std` 全量
  `no vet` 在 SUT 上只新增 **8** 条诊断（全是 `fxxoptarg`，即 option 流入 plain **形参**），
  `optunuse` 与 `ovfhndld` 均为 **0**。真正的迁移量取决于 §11.4 那条**尚未定案**的规则。
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
| 未标注但当前不报错的索引读取 | 待测 | D7-R1：编译器的隐式验证（可证明在界内 ⇒ 消除 option 包装）覆盖不到的部分 |
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
| 复合赋算运算符 `a += 1` 一族（D10） | 原為 parse 後結構損壞（編譯期報錯） | 結果是 option 的函式裡：**WARNING**（廢棄；`no fmt` 保存時自動重寫為 `a ?= a + 1`）；結果不是 option（或頂層）：**ERROR**（提示 `a + 1: { ok -> a = it, -> }` 或改結果參數為 `?T` 再上拋，不降糖） | `nolang-deprecated-compound-assign`（LSP）/ `W_COMPOUND_ASSIGN`（parser 診斷） |
| option 赋给 plain 变量 / 传给 plain 形参 | ~~ERROR~~ **实为静默接受** | **暂不变**（缺口未闭合，见 §11.3） | 既有 `ValidateTypes`（**不生效**） |

> 🔴 末行是**勘误**：实测「option 赋给 plain 变量」既不报 `cannot assign` 也不报
> 任何其他诊断，运行期静默取 payload。方案原稿把它当成现成的硬错误来兜底，
> 因此高估了安全网、低估了缺口。详见 §11.3 / §11.4。
>
> 🔴 `ovfhndld` 一行也要补一句：它只对**未标注**站点发出（**已标注**站点不发，
> 见 §11.8 的实测决策），而 `optunuse`（ERROR）对两者一视同仁。

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
  实测：`{ ok -> it }`（无兜底）报
  `non-exhaustive match on option 'a': missing arm(s): nil, err — add a `-> ` (else) arm, or handle every variant`。
- 🔴 **空臂必须独立成行**（实测，2026-10-12）：逗号分隔的 inline 形态
  `{ ok -> it, -> }` / `{ ok -> it, -> 0 }` / `{ ok -> it, err -> 0 }` **一律解析失败**——
  `,` 在语句层被 `parseExpressionStatement`（`stmt.go:1745`）当作**多重赋值**分隔符，
  报 `expected variable name after ','`。本方案里所有 inline 逗号写法都是**错误转述**，
  实际合法形态是逐行：

  ```no
  d = a: {
      ok -> it
      ->
  }
  ```

  §6 的 `--fix=unwrap` 因此只能产出逐行形态（本来也就是 §8.1(c) 的 4 行标准形）。
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
| `type.go` `isSafeIndexBase` | ①接受 str/txt 基底（`inferIndexElemType` 返回 `char`）；②**保留** `p.inBoundsIdx[idx]` 早退（D7-R1）——可證明在界內的讀取不降級成 option。 |
| `bounds.go` | **整套保留**（D7-R1 反轉）：`AnalyzInBoundsForFunc` / `p.inBoundsIdx` / `RecordIndexLocalType` 的 in-bounds 記錄、`checker.ValidateUnhandledIndex` 的 in-bounds 分支、`fmt.AnalyzeInBoundsIndex` 與"刪除冗餘 `#{index-out}`"邏輯全部恢復上線。 |
| `desugar.go` `validateMatchArmReturns` | 允许 `return` 结尾臂（§4.1）。 |
| `lowering.go` `lowerSurfaceMatch` | 空臂合成零值（§4.2）。 |
| `expr.go` `parseMatchExprFrom` | inline 臂体支持 `return`（§4.1）。 |

### 5.2 `src/checker`

- **合并** `ValidateUnhandledOverflow`(4523) 与 `ValidateUnhandledIndex`(4855) 的遍历为一条
  "option 生命周期"趟：
  - 产生处 → 记入本函数"待消费集合"，产出 **HINT**（`ovfhndld` / `idxhndld`）。
  - 消费处（`?=` / `x: {…}` / `_ =`）→ 从集合移除。**注解不参与**（D1）。
  - 趟末仍未消费 → **ERROR** `optunuse`（D2）。
- ~~**删除** `stmtOverflowAnnotated`(4346) 与 `effOverflow` 的"注解即豁免"传递逻辑：注解不再让
  子树的算术免于检查（D1）。~~ **实测推翻，改为保留（仅豁免 HINT）**：直接删除会在
  `src/std` 上爆出 **2029 条** `ovfhndld` HINT（143 `6kryrbsq` / 8 `fxxoptarg` / 2 `tcpoxtfd`
  不变），不可用；且该删除是**冗余**的 —— D1 要拦的「标注了却没消费」已由 `optunuse`
  （ERROR，**不受** `effOverflow` 门控）覆盖。见 §11.8。
  `isDirectOverflowValue`(4393) 保留（仍用于判定"是不是整数运算"）。
- `lint.go`：20c(507)、20d(538)、20e(558) 的 `Severity` 由 `LintError` → `LintHint`；新增
  `optunuse`（ERROR）与 `rngidx`（HINT，建议 `for i <- arr { x = i }`）的接线。
- **不动**：~~`ValidateTypes` 的 `cannot assign ?T value to T variable`（这是"用在 plain 语境"的硬错误，保留）。~~
  🔴 **勘误（§11.3）**：这条诊断**根本不存在** —— `y i64 = g()`（g 返回 `?i64`）与
  `y i64 = x`（x 是 `?i64`）都是 `No issues found`，运行期静默取 payload。
  「option 用在 plain 语境」目前**无人把关**，是本方案最大的未闭合缺口。
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
x = <expr>                      =>   x-opt = <expr>
                                     x = x-opt: {
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
- 变量名 `<x>-opt`：由目标名派生、可读；`__` 前缀在 Nolang 源码中**根本不可书写**（`__o1 = 5` 之后 `out = __o1` 直接 `expected expression, got nil instead`），所以原计划的 `__o<N>` 不可用 —— 见 §11.16。碰撞（檔案中已出現同名識別字）時追加序號 `<x>-opt2`、`<x>-opt3`……
- 写索引 `k[i] = v` 与读索引分别处理：写路径本就无 option（除非下标表达式本身产生），
  不要误改。

### 6.2 迁移顺序

1. `src/std`（118 文件）
2. `tests`（528 文件）
3. `compiler`（62 文件）、`example`、`test`
4. `tmp/`（1084 文件，可最后、可批量）

每批之后跑 `./bin/no vet <dir>` 确认 ERROR 归零，再进下一批。

### 6.3 不做的事

- 不跑 `no fmt -w` 全量重排（diff 不可审）。注：D7-R1 恢复 in-bounds 分析后，`no fmt`
  仍会"删除冗余 `#{index-out}`"（`fmt.AnalyzeInBoundsIndex` 与 codegen / vet 共用同一份
  判定，口径一致才安全），所以这条顾虑依在，仍不批量重排。
- 不自动删除既有 `#{overflow=…}` / `#{index-out=…}`（D1 保留其行为职责）。
- 不自动改写函数签名为 `?T`（保守；`?=` 只在**已有** `?T` 结果参数时使用）。
- **不覆盖 `15w45dqk`（option 赋给 plain 变量）与 `fxxoptarg`（option 流入 plain 形参）**：
  两者由 `validateStmtTypes`（未导出）产出，且前者常常**没有**可绑定的新变量名可派生
  暫存名（`out = x` 的 `out` 早已存在）。本次只处理 `optunuse`（未消費的綁定），
  其餘三类 ERROR 留人工 —— 实测三者在 `compiler/` 合计仅 16 处，且与对照二进位
  的 ERROR 集合逐行相同（非本次新增）。
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
| ② | 未被隐式验证覆盖的索引读取（定长数组 + 界内下标之外，即 `bounds.go` 证明不了安全的部分） | 决定这些读取是否需要显式解包，以及 `rngidx` HINT 的提示范围 |
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

**(a) 可证明安全 ⇒ 隐式验证 + 优化（D7-R1；原文把这条写成"取消"是错的）。** 原判決是取消（D7：區域不好界定，`for i <-
[0..N) { x = arr[i] }` 也照常產生 option），用戶復核後定案**保留，并把它的性质说清楚** ——
这不是"给库作者开后门"的豁免，而是**编译器的静默证明与代价消除**：
- 編譯器**能證明安全**就**消除 option 包裝、解包與 match 分支**。這是特性而非漏洞 ——
  用戶不必為「看得出來不會越界」的下標寫防禦碼；用戶側照常写的 match 也不會被删掉，
  只由 LSP 以 HINT 说明该臂不会走到（D9 `arm-unreach`）。
- LSP 在「可能出錯」處提示 **warning**（不是 forcing 顯式處理），把「會不會出錯」的
  判斷成本從用戶轉移給編譯器。
- `bounds.go` 整套保留（`AnalyzInBoundsForFunc` / `p.inBoundsIdx` /
  `AnalyzeInBoundsIndex`），`no fmt` 的"刪除冗餘 `#{index-out}`"一併恢復。**三方共用
  同一份判定**：codegen（`isSafeIndexBase` 早退）、`no vet`（`ValidateUnhandledIndex`
  放過）、`no fmt`（移除死註解）。任一方漏接就會出現「`no fmt` 刪了註解 → `no vet`
  報錯」的假回歸。
- `rngidx` HINT 的定位隨之改變：從"引導改用容器迭代"改為"在优化範圍之外提示"。

**(a-2) D9：空 `->` 兜底臂的双态诊断（2026-10-12 用户定案）。** 用户的出發點是這段看起來
很多餘的程式碼：

```
ao = 1 + 2
a  = ao: {
    ok -> it
    ->          ; ← 主要是影响这里
}
```

定案：**這個分支不能移除**（match 必须所有分支），**也不改语法**（保留还有作用：未来可能
出错、可能正确，语法不用改）。編譯器该做的是把狀態說清楚：

| 判定 | 诊断 | 嚴重度 | 语义 |
| --- | --- | --- | --- |
| 可证明主体恒为 ok | `arm-unreach` | HINT | 这个分支不会走到 |
| 无法证明 | `arm-should` | WARNING | 这个分支可能会走到，请决定它该做什么（`log` / `print`） |

**实现位置**：`src/checker/match_arm_reach.go`（新檔），由 `lint.go` 的 20f 步依 trace
逐條決定嚴重度（同 20d 的 ovfhndld/optunuse 分工）。

**"可证明恒为 ok"的判定**（与 D7-R1 的 bounds.go 同一套静態分析，口径一致）：
- 索引讀取 `a[i]`：可证明在界内（`parser.ProvablyInBoundsIndexReads`）；
- 整數算術：
  - `+ - *`：整棵可常量折疊且不溢位；
  - `/` `%`：**除數為非零字面量**（且不是 -1，`MIN / -1` 會溢位）——被除數是普通變數也
    無妨（`x / 2` 可證明不除零），但被除數若是算術就必須同樣可證明（`(a+b)/2` 的
    `a+b` 自己會溢位）；
  - 標識符：追蹤到其唯一初始器後遞迴（綁定多次一律保守剔除）。

**安全方向**：宁可多報 WARNING，不可把"其實会失败"的臂說成死臂。因此 `-> 0` 與 `->`
在 AST 上無法區分（D4：空臂語義就是默認零值），兩者一視同仁——語義本來就相同。
寫了實際動作的臂（`-> log('bad')`）完全不提示。

**已知限制**：以索引為主體的 match（`x = a[0]: { ... }`）降級後的 IfExpression 沒有
`MatchedExpr`，`collectMatchRoots` 看不見它，故暫不提示。由
`TestCatchAllArmIndexSubjectLimitation` 釘住現況。

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

⚠️ 这 4 行**不能压成一行** `a = ao: { ok -> it, -> }`——逗号 inline 形态解析失败，见 §4.2。

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
| **M1b** ~~删 bounds~~ → **D7-R1：保留 bounds 作为隐式验证** | `bounds.go` 整套 + `inBoundsIdx` + fmt 的 `AnalyzeInBoundsIndex` **保留上線**，作為「可证明安全 ⇒ 消除 option 包装/解包/分支」的优化 | 相关测试随优化恢复；`go test ./...` 绿；`idxhndld` 与 CTL 一致 |
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
  - **隐式验证**：编译器能证明不会溢出 / 不会除零 / 在界内时，静默消除 option 包装、
    解包与 match 分支（用户侧无需写防御码）；证明不了的地方照常产生 option，
    LSP 提示 warning（`arm-should` / `rngidx`）。
- EN 镜像 `docs/i18n/en/.../lang/*.md` 同步（历史上 EN 常落后，需显式跟进）。
- `docs/docs/lang/memory.md`（若有 option/ownership 相关段落）复核。

---

## 11. 实施勘误（M1a / M2 实测，2026-10-12）

> 这一节记录**实施过程中被实测推翻**的前提。方案正文（§1–§10）保留原样以便对照，
> 以本节为准。

### 11.1 🔴 AST 磁盘缓存会静默服务过期的 lowering 结果

缓存位于 `~/.cache/nolang/ast`，键 = `(文件内容, ASTVersion, sigSalt)`。**它看不见
编译器二进制**：改了 lowering 却忘了 bump `ASTVersion`，同一份源码会继续拿到**改动前**
的降级结果。

实测症状：`x i64 = 0` ⏎ `x = a + b` 在有缓存时**静默通过**（缓存里是旧的「已合成 ?T」
版本），`NOLANG_AST_CACHE_OFF=1` 才报 `ovfhndld`。这会把任何 A/B 结论引向错误方向。

**纪律（已并入 skill）**：
- 所有涉及 lowering 的 A/B 一律带 `NOLANG_AST_CACHE_OFF=1`；
- 每次改 lowering 都要 bump `ASTVersion`（本次 `2026-10-11.2` → `2026-10-12.1`，
  见 `src/parser/astdiskcache.go` 的说明块）；
- bump 后必须重跑 `cd src && go run ./checker/genstdsig`（重烤 `stdsig_gen.go` 与
  `stdastembed/std_ast.bin`），否则 `TestStdASTBlobMatchesFreshParse` 失败。

### 11.2 `inferOptionArith` 必须复用 `captureTargetAllowsOption`（已修）

M1a 第一版只删了注解短路，**没有**加目标闸门，于是它会替**已存在的 plain 变量**
合成 `?T`——`x i64 = 0` 之后的 `x = a + b`、以及纯量出参 `r i64` 都中招。这不是
「推断」而是「就地改掉一个已存在变量的型别」。

实测后果（最刺眼的一例）：`wrapped = (a i64, b i64) (r i64) { #{overflow=wrap} \n r = a / b }`
的 `r` 被合成 `?i64`，整个除法从 **plain 路径**被拖到 **option 路径**，
`@__nolang_div_zero_report` 的 stderr 报告消失 →
`TestDivZeroGuardSurvivesOverflowAnnotation` 失败。

补上 `if !l.captureTargetAllowsOption(s) { return }` 后该测试**一行不改**即通过。
（`captureTargetAllowsOption` 与 `maybeCaptureOptionAssign` 用的是同一条规则：
「已存在的变量不改变其型别」。）

顺带闭合了一个**既有**漏洞：`x i64 = 0` ⏎ `x = a / b` 在改动前是**静默通过**的
（`inferOptionDivMod` 同样没有闸门），现在按 §3.2 报 `ovfhndld`。

### 11.3 🔴 §3.4 的前提有误：`option 赋给 plain 变量` 目前**不是**错误

§3.4 写着「option 赋给 plain 变量 / 传给 plain 形参 → ERROR（不变，既有 `ValidateTypes`）」。
实测**不成立**：

```no
g = () (q ?i64) { q = 1 }
f = () (r i64) {
    y i64 = g()      ; 无诊断
    r = y
}
```
```no
f = () (r i64) {
    x ?i64 = ok(1)
    y i64 = x        ; 无诊断
    r = y
}
```
两种写法在 CTL 与 SUT 上都是 `No issues found`，运行期静默取 payload。

两个直接后果：

1. **好消息**：D1 让 std 的 4564 个已标注站点「全变 `?T`」，**不会**因此爆出大批
   `cannot assign ?i64 value to i64 variable` —— 它们照旧编译、照旧运行（隐式取 payload）。
   §2.2 的「1.8 万行迁移」在**编译**意义上被高估了；真正的迁移压力来自下面的 2。
2. **坏消息**：`optunuse` 只看「该变量**有没有被读**」，不看「**怎么被读**」。
   `x = a + b` ⏎ `r = x`（r 是 plain）是**合法且静默**的 —— option 的 err 被丢掉却无人报。
   D1 真正想拦的「option 被当作普通值用掉」目前**无人把关**。见 §11.4。

### 11.4 新增风险：`optunuse` 的口径偏松

`optunuse` 用「本函数内是否出现过该标识符的读取」判定，覆盖不到：

- 被**当普通值**用掉（`r = x`、`io.outln(x)`、`f(x)`）——隐式取 payload，err 丢失；
- 只被**写**过（`x = a + b` 之后再 `x = c + d`）——旧值被覆盖，第一次的 err 丢失。

若要把 D1 落完整，需要第二条规则：「option 型变量出现在**非 option 语境**（赋给 plain
变量 / 传给 plain 形参 / 参与非 option 运算）」→ ERROR。这会与 11.3 的「隐式取 payload」
现状正面冲突，**必须单独定案**（放开隐式取 payload，还是全语料补 match）。在定案之前，
§6 的迁移量无法准确估计。

### 11.5 逗号 inline match 形态不受支持（§4.2 已修）

`{ ok -> it, -> }` / `{ ok -> it, -> 0 }` / `{ ok -> it, err -> 0 }` **一律解析失败**
（`,` 在语句层被 `parseExpressionStatement`（`stmt.go:1745`）当成**多重赋值**分隔符）。
合法形态只有逐行。§4.2 与 §8.1(c) 里的逗号写法均为错误转述，已就地更正；
§6 的 `--fix=unwrap` 只能产出逐行形态。

### 11.6 诊断分级落地点（已实现）

- `ValidateUnhandledOverflow` **一趟**产出两种 trace：`ovfhndld`（HINT）与
  `optunuse`（ERROR）；严重度在 `src/checker/lint.go` 20d **逐条依 TraceID** 决定，
  不能整批套用。
- 20c（`ovf-int-default`）、20e（`idxhndld`）整批降 `LintHint`。
- `ovfhndld` 的豁免由「`declaredOption` 一刀切」改为「只豁免用户**显式**写下的 `?T`」
  （`NullableType.IsInferred`）。改动前 `x = a + b` 被推断成 `?T` 后**完全无声**——
  既没有 HINT（被 `declaredOption` 吃掉）也没有 ERROR。
- 两条消息文本按 D1 重写：删掉「加 `#{overflow = wrap}` 注解回普通 int」这一**已不成立**
  的建议，改为「注解只改变失败时怎么取值，结果仍是 `?T`」。

### 11.7 测试面同步（已完成）

| 文件 | 改动 |
| --- | --- |
| `src/mir/div_zero_guard_test.go` | **未改**（11.2 修好后自动通过） |
| `src/mir/opt_test.go` | `foldSrc` 的 `a = 2 + 3` → `a i64 = 2 + 3`：D1 之后只有显式 plain 型别才落得到 folder 的 plain 路径 |
| `src/checker/unhandled_overflow_test.go` | 新增 `TestUnhandledOverflowOptionBindingMustBeConsumed`（`x ?i64 = a + b` 不再算「已处理」）；`explicit_option_declaration` / `div_mod_binding_inferred_to_option` 从「允许」移到「必须报 optunuse」 |
| `src/checker/option_capture_lint_test.go` | `TestCaptureAssignmentNotReported` → `TestCaptureAssignmentStillNeedsConsumption`（捕獲 ≠ 消费）；新增 `TestCaptureAssignmentConsumedIsClean`（三条消费路径的正面对照）；`plain_arith_assignment` 的 `ovf-int-default` 期望 1 → 0（目标已是 `?T`，lint 被吸收） |
| `src/checker/discard_exempt_test.go` | 2026-10-12 D7-R1/D6 勘误：`TestBareIndexStillReportedWithoutDiscard` 原断言「报 `idxhndld`」，改为断言 `optunuse`（D6 捕獲加寬後產生處 HINT 被捕獲吸收），並改走 `RunAllLints`（`optunuse` 由 `ValidateUnhandledOverflow` 產出） |
| `src/parser/option_capture_test.go` | `TestCaptureRootSafeIndexGatedOutOfOptionFunc` → `TestCaptureRootSafeIndexAppliesOutsideOptionFunc`（閘門 `enclosingFuncHasOptResult` 已删，改斷言 `a` 為 `?i64`；另新增 D7-R1 對照 `TestCaptureRootProvablyInBoundsIndexNotCaptured`）；`enclosing_overflow_annotation_no_longer_covers_arm_bodies` 的 `wantCap` 2 → 3 |
| `src/build/lib_export_sem_test.go` | 末段断言不再看 `Severity`（D2 把 `ovf-int-default`/`ovfhndld` 都降 HINT），只看「第 4 行有没有报」；`checker` import 随之删除 |
| `src/parser/compound_assign_test.go` | **D10 新增**：五個運算符各自降糖成 `UnwrapAssignStatement` + `DeprecatedOp`；解析期 `W_COMPOUND_ASSIGN` 警告逐條落位（僅 option 結果函式）；兩組對照（`a[i] += 1` 與位元複合賦算**不**降糖、不警告，手寫 `?=` 不警告）；**普通函式／頂層 ⇒ ERROR** 且不得同時發警告、建議文案還原真實右式（`g(y) * 2`）、兩函式接連定義時各自按自己的結果型別定案 |
| `src/checker/compound_assign_lint_test.go` | **D10 新增**：WARNING 嚴重度經 `RunAllLints` 路由後仍是 WARNING；五個站點各一條、逐行定位；**普通函式裡 0 條 lint**（那一路由 parser 報 ERROR，避免同一站點被報兩次）；對照組守住「棄用函數 HINT 不被波及」與「手寫 `?=` 不報廢棄」 |
| `src/fmt/compound_assign_test.go` | **D10 新增**：`no fmt` 把五個運算符重寫成 `a ?= a <op> <expr>`、冪等、右式可為任意表達式、`a[i] += 1` 不得被誤改寫；**普通函式裡原樣退回**（輸出逐位元組等於輸入）並把 ERROR 透出來 |

### 11.8 🔴 §5.2「删除 `effOverflow`」被实测推翻：保留，但**只豁免 HINT**

§5.2 要求删掉 `stmtOverflowAnnotated` 与 `effOverflow`，理由是「注解不再让子树的算术免于
检查（D1）」。实测把 `stmtOverflowAnnotated` 强制返回 `false`（等价于删除）后：

| | `src/std` 的 `no vet` 输出 |
| --- | --- |
| SUT（保留豁免） | `6kryrbsq` 143、`fxxoptarg` 8、`tcpoxtfd` 2 |
| 探针（删除豁免） | **`ovfhndld` 2029**、`6kryrbsq` 143、`fxxoptarg` 8、`tcpoxtfd` 2 |

**2029 条 HINT** 全来自 std 里既有的 `#{overflow=wrap}` / `#{index-out=…}` 标注站点 ——
每一处 `#{overflow=wrap} p = pos + 1` 都会新增一条 HINT。这既淹没真正的诊断，也与 HINT
的语义不符：

- **语义上不成立**：标注站点的 option **恒为 `ok`**（`wrap`/`clamp0`/`min`/`max`/`saturate`
  都保证有值，只是值不同），**不存在失败分支**。而 `ovfhndld` 的消息讲的是
  「默认在溢出时返回 option<T>」——那是**未标注**站点的情形。对标注站点发这条 HINT
  是**指错方向**（用户会去找一个永远不会出现的 `nil`/`err`）。
- **功能上冗余**：D1 真正要拦的「标注了却没上抛也没解包」由 `optunuse`（ERROR）覆盖，
  而 `optunuse` 的判定**刻意不看** `effOverflow`（`checker.go` 中 `reportOptUnuse`
  调用点无该门控）。实测确认：`#{overflow=wrap}` ⏎ `x = a + b` ⏎ `io.outln('done')`
  在保留豁免与删除豁免两种构建上**都**报 `optunuse` ERROR。

**结论**：`stmtOverflowAnnotated` / `effOverflow` **保留**，语义收窄为「只豁免 `ovfhndld`
HINT，不豁免 `optunuse`」。§5.2 的删除项作废（已就地划掉）。

> 一般化教训：HINT 的「豁免口径」与 ERROR 的「豁免口径」必须**分开设计**。方案原稿把
> 两者当成同一套豁免（删掉就一起删），结果一个合法的小改动把 HINT 变成洪水。
> 判定「某个豁免该不该留」不能只看语义推演，必须**实测规模**（2029 vs 0）。

### 11.9 待办（`rngidx` 已實現，其餘仍未閉合）

- ~~`rngidx` HINT（§5.2 末条）未实现~~ → **已實現**（2026-10-13）：
  `src/checker/range_index_lint.go` 的 `ValidateRangeIndexLoops`，在
  `src/checker/lint.go` 以 20g 接入 `RunAllLints`（`LintHint` /
  `Source: "nolang-range-index"`）。判據：`for i <- [a..b)` 且體內有以該迴圈變數
  為下標、基底是另一識別字的 `IndexExpression`；用
  `parser.ProvablyInBoundsIndexReads(program)` 剔除可證明在界內的讀取，故與
  D7-R1 的豁免邊界一致（不會在 `a [3]i64` + `[0..3)` 上誤報）。
  **不自動改寫**（§6.3）：兩種寫法語意不等價（前者需要下標本身）。
  回歸：`src/checker/range_index_lint_test.go` 三例（切片 + 手動下標 → 1 條；
  容器迭代 → 0 條；可證明在界內 → 0 條）。
- 「option 用在 plain 语境」的**完整**规则仍未定案：§11.10 只闭合了「**裸标识符/调用
  结果**赋给 plain 变量」这一半，§11.15 记录了另一半（内联算式的 option 性未被
  `inferExprType` 建模）的现状与影响。
- **D9 的索引主體支援**（仍未閉合）：以索引為主體的 match 降級後沒有 `MatchedExpr`，
  `collectMatchRoots` 看不見它（見 §8.1(a-2)「已知限制」），故 `arm-unreach` /
  `arm-should` 目前只覆蓋「標識字 / 呼叫 / 屬性」主體。要把
  `x = a[0]: { ok -> it, -> }` 也納入，需要擴充 match 走訪（會動到
  `CollectNonExhaustiveMatches` 共用的 `collectMatchRoots`，需一併評估風險）。
  ⚠️ 注意：`--fix=unwrap` 的情況 B **刻意**先 `x-opt = <expr>` 再 `x = x-opt: { … }`，
  正是為了讓 match 主體落成「標識字」—— 這樣一來修復產物自動享有 D9 的雙態診斷，
  不必等這一項做完。

### 11.10 🔴 §11.4 的缺口已定位并闭合：`isArgTypeCompatible` 的 `?T → T` 豁免

§11.4 说「option 被当普通值用掉无人把关」。**根因找到了**，在
`src/checker/funcargs.go:1054-1058`：

```go
// ?T 到 T 的隱式解包：在 ok 分支中，?T 變數已確認為有值，
// 傳給期望 T 的參數是安全的（代碼生成器會自動取值）。
if strings.HasPrefix(argType, "?") && argType[1:] == expectedType {
    return true
}
```

这条豁免自述的前提是「**在 ok 分支中**」，但 `isArgTypeCompatible` 是通用助手，被
**赋值路径**一并调用（`checker.go` 的 LetStatement / AssignExpression / MultiAssign 三处），
前提不成立的地方也被放行 —— `y i64 = x`（x 是 `?i64`）于是完全静默。

**而语言对同一情形在「调用引数」上早已明令禁止**：`funcargs.go:1753` 的 Phase-2 规则
（trace `fxxoptarg`）写着 *"Implicit unwrap of option arguments is not allowed"*，且同一段
注释指出 match 臂内的 `it` 绑定会解析成 plain T、**根本不会**走到该分支 —— 也就是说
1054 那条豁免宣称要服务的「ok 分支」情境实际由 `it` 承担，**豁免本身没有存在必要**。

**修法**（最小侵入）：不动通用助手（`?T = ?T` 是拷贝，仍须放行），只在三处赋值判据上
用 `(!isArgTypeCompatible(...) || optionImplicitUnwrapRejected(target, value))` 把
`?T → T` 强制翻回「不相容」。新增 `optionImplicitUnwrapRejected` /
`optionUnwrapAssignHint`（`checker.go`，紧邻 `optionTypesCompatible`），命中时附上
可行修法而不是只报「cannot assign」。

实测（`NOLANG_AST_CACHE_OFF=1`，`/tmp/no_guard` 对 `/tmp/no_sut`）：

| | `src/std` ERROR | 明细 |
| --- | --- | --- |
| 闭合前（SUT） | 8 | 全是 `fxxoptarg` |
| 闭合后 | 19 | `15w45dqk` **11** + `fxxoptarg` 8 |

**11 个新 error 就是 D1 在 std 的真实迁移量**（不是 §2.2 估的 4564）。11 处全是
`?i64` 裸拷贝：`argon2:615`、`rsa:222`、`x509:73`、`json:440`、`net/net:886`、
`quic:601`、`str:2185/2292`、`txt:219`、`yaml:229/1404`。

### 11.11 🔴 `optunuse` 的两个**误报**（已修）

M2 的 `optunuse` 上线后，`tests/` 语料爆出 110 条 ERROR，其中**绝大多数是伪阳**。
两类根因：

**(a) 具名格式字串的 `{field}` 没被算作「读取」。** `print('result: {result}')` 里的
`result` 是真正的读取，但插值只在 MIR 才降级（`hir2mir.go:7902`：
*"substituted ONLY at a print-family call site"*），checker 看到的仍是一个含 `{...}`
的 `StringLiteral`，而 `collectReadNamesInExpr` 的注释写着「Literals … don't read
variables」。修法：在 `CallExpression` 分支按 `isPrintFormatCall` 判定，用
`parser.ParseFormatString` 抽出字段名（取基底标识符）登记为读取
（新增 `collectFormatFieldReads`）。

**(b) 顶层（指令稿）作用域从不填 `curReadNames`。** 它只在 `FunctionDefinition` /
`FunctionLiteral` 分支被赋值，于是**所有顶层 option 绑定**都被判成「从未被消费」——
`tests/fs-error-*.no`、`tests/heap.no` 等指令稿全中。修法：进入顶层循环前
`curReadNames, curResultTypes = functionScopeFacts(program.Statements, nil)`
（`collectReadNames` 不递归函数体，所以只收顶层）。

修后 `tests/` 的 `optunuse` 从 **110 → 16**，且这 16 条经人工核对**全部为真**
（`fs-error-final.no` 的 `f1..f4`、`uninit-output.no` 的 `r1..r5`、
`option-copy.no`、`safe-index.no` 的 `auto-prop` 等：绑定了 option 却从不消费）。

### 11.12 `_` 被写进 `varTypes`（既有缺陷，迁移中被暴露）

`ValidateTypes` 的 LetStatement 分支把 `_ = expr` 当普通绑定处理，于是
`varTypes["_"]` 记下了**第一条** `_ = …` 的型别，之后每条 `_ = …` 都拿它比对 →
`uninit-output.no` 出现 `cannot assign ?[]i64 value to ?uninit-struct variable '_'`。

`_` 是显式丢弃、不是变量，三條 option 消费路径之一就是 `_ = expr`，它必须永远可用。
修法：在 LetStatement 分支（`checker.go:7865` 起）于合成语句判断之后加一道
`if s.Name != nil && s.Name.Value == "_" { break }`，既不登记也不检查。

### 11.13 `tcpoxtfd` 与 D1 互相打架（已修）

`#{overflow=wrap} p i64 = pos + 1` 里的 `i64` **不是**冗余标注 —— 它正是抑制 option
化的那个开关（`inferOptionArith` 见 `s.Type != nil` 即返回；`captureTargetAllowsOption`
「已存在的变量不改其型别」）。但 `checkRedundantTypeInAnnotation` 只比
`inferExprType(s.Value) == annotatedType`（两者都是 `i64`），于是要求把它删掉 ——
删掉立刻变 `?i64`，触发 `15w45dqk`。两条提示自相矛盾，迁移无法收敛。

修法：`checkRedundantTypeInStmt` 增加 `optionProducing` 判据（`!HasPrefix(annotated,"?")
&& isDirectOverflowValue(...)`，复用同一套运算元型别推断），命中则不报冗余。

### 11.14 迁移结果（M6 首批）

| 语料 | 迁移前 ERROR | 迁移后 ERROR | HINT |
| --- | --- | --- | --- |
| `src/std` | 19（11 `15w45dqk` + 8 `fxxoptarg`） | **0** | 145（与基线**逐条相同**） |
| `tests/` | 16（全 `optunuse`） | **0** | — |

修法形态只有两种，都**保持既有运行时语义**：

1. **显式 plain 型别**（11 处 `15w45dqk` + 8 处 `fxxoptarg` 中的大多数）：`p = pos + 1`
   → `p i64 = pos + 1`。D1 之前这些站点因注解短路本就是 plain 算术，补上型别即**精确
   复原**旧语义（回绕 + 除零守卫强制 0）。
2. **拆成两条 + `#{index-out}`**（`toml.no:586/688`）：`#{index-out=zero}` 只在「根为
   索引」的绑定上生效（`maybeIndexOutAssign`），`toml-strip-comment(lines[i])` 的根是
   调用，注解空转、option 直接流入 `str` 形参。改为先 `line-raw = lines[i]`（注解生效，
   越界折成 `''`）再以纯 `str` 调用。
3. **`_ = x` 显式丢弃**（`tests/` 的 16 处）：这些测试的意图本就是「不取值、只看行为」。

另：改动 `src/std/**` 后必须重跑 `cd src && go run ./checker/genstdsig` 重烤
`stdastembed/std_ast.bin`，否则 `TestStdASTBlobMatchesFreshParse` /
`TestStdASTBlobHasSafeIndexNormalisation` 失败（本次已重烤，2538795 字节）。

### 11.15 ⚠️ 仍未闭合：内联算式的 option 性未被 `inferExprType` 建模

§3.2 写着「已存在的 plain 变量仍是错误（`x i64 = 0` 之后的 `x = a + b`）」。实测
**不成立**：

```no
f = (a i64, b i64) () { x i64 = 0 \n x = a + b \n print(x) }   ; 0 error
```

原因是 `inferExprType` 对 **InfixExpression** 只在**运算元本身就是 option 变量**时才
把结果标成 `?T`（`x = y` 其中 `y ?i64` 会报；`x = a + b` 其中 a/b 是 plain 不会报）。
所以 11.10 的守卫实际只抓「**裸标识符 / 调用结果**赋给 plain 变量」这一半。

**影响是「漏报」而不是「误报」**：现有代码不会被误伤，只是「plain 变量 = 算式」这类
泄漏仍未被拦。要不要补齐取决于 §11.4 那条完整规则怎么定案 —— 补齐会让
`x = a + b` 这类写法在全语料（含 std 的 3944 个 overflow 标注站点）都需要显式型别，
**规模远超 11 处**，故本次**刻意不做**，留给下一次单独定案。

### 11.16 🔴 `__` 前綴識別字在 Nolang 源碼中**不可書寫**（`--fix=unwrap` 的暫存名因此改設計）

§6.1 原本指定情況 B 的暫存變數叫 `__o<N>`，理由是「`__` 前綴在 Nolang 源碼中不可書寫，
不會與用戶符號衝突」。實測這個理由**用反了** —— 它不是「用戶不能寫、但編譯器可以寫」，
而是**源碼裡根本不能出現**：

```no
f = () (out i64) {
    __o1 = 5        ; 這行parse過了
    out = __o1      ; ERROR: expected expression, got nil instead
}
```

（`out = o1` 完全正常；`out = __o1` 在同一行同一欄位報錯。）

**結論**：`--fix=unwrap` 的暫存名改用 **`<x>-opt`**（`-` 是合法識別字字元，且由目標名
派生、可讀）。碰撞時追加序號：`<x>-opt2`、`<x>-opt3`……。碰撞集合刻意取保守的
「整份檔案出現過的識別字狀 token」（切成 token 而非 substring 比對，避免 `abc` 誤命中
`xabc`）。§6.1 正文與 D3 決策表已就地更正。

### 11.17 M4 / M5 實測（2026-10-13）

#### M4：str/txt 索引 option 化的真實遷移量

回答 §7.1 表 ③（「決定是否分期」）：**規模可接受，與主體同期落地，不分期**。

| 對象 | 站點數 | 說明 |
| --- | --- | --- |
| `src/std` | 11 | `crypto/base32.no` 1、`encoding/base64.no` 4、`fmt.no` 4；另有 `regexp.no` 7 處是因 `?char` 流入 `i64` 形參而 **SIGSEGV**，補顯式 `char` 後消失 |
| `tests/` | 34 | 幾乎全是同一個 x25519 十六進制解析塊（`hi/lo char = hex-str[i * 2( + 1)]`）在 16 個檔案裡的副本 |
| `compiler/` | 0 | ERROR 集合與對照二進位**逐行相同**（76 → 76，無新增） |

**修法**：在綁定上補顯式 plain 型別（`c char = s[i]`），把結果拉回 plain。這與
§11.13 的 `tcpoxtfd` 豁免（`producesOptionIndex`）是同一件事的兩面 ——
一邊不催你刪註解，另一邊就不能再催你處理 option。

配套的實作細節（均已落）：

- `src/parser/type.go` 的 `containerElemType` 把 `str` / `txt` 從「回傳 `''`
  （char，非 option 索引）」改為回傳 `"char"`，`isSafeIndexBase` /
  `inferIndexElemType` 因此自動收編（一處改、三處口徑一致）。
- `src/mir/hir2mir.go` 新增 `isUTF8IndexReceiver` / `lowerSafeUTF8Index`：
  **先讀、再與 `-1` 哨兵比較**，而不是先比長度。根因是 `OpLen` 是**位元組數**、
  而下標是**碼點數**，`'héllo'[5]`（位元組長度 6）會被誤判在界內、把哨兵 `-1`
  當 ok 交出（實測 `it=4294967295`）。修好後 `s[4] ok it=111`、`s[5]` / `s[99]`
  都落到 fallback 分支。
- `src/mir/codegen.go` 的 `emitCast` 補窄化分支（此前只發 sext/zext，LLVM 對
  `i64 → i32` 直接報 `invalid cast opcode`）。
- `src/mir/borrow_release_test.go`：D6 後 borrow 被 option-wrap，不再有獨立
  `OpRelease`；改為 IR 層判據（必須有 `@vec_retain`，且必須走 `@vec_free` /
  `@__nolang_vec_free_`，**不能**有裸 `@nolang_free(i8* …)`）。
- `src/mir/spawn_arg_move_test.go` / `spawn_arg_retain_test.go`：D6 讓
  `m = a[1]` 變成界比較 + 兩分支，drop 被複製到兩條分支；全函式計數失真但
  **每條路徑仍只釋放一次**。新增 `dropsPerPath`（沿 CFG 走到每個終結塊、
  環用 `seen` map 防無限展開）改按路徑斷言。

**全語料門禁**（`scripts/mir_golden.sh`，`MIR_GOLDEN_JOBS=6`；CTL = 對照二進位）：

| | SAME | DIVERGE | REGRESS | NEW |
| --- | --- | --- | --- | --- |
| CTL | 446 | 13 | 22 | 48 |
| SUT（修完 `zz-strindex-lint.no` 後） | **446** | **13** | **22** | **48** |

**完全一致**。過程中唯一出現過的差異是 `tests/zz-strindex-lint.no`（SUT 一度新增
rc=1）：該檔是 `STR_INDEX_COMPLEXITY` lint 的語料，D6 後 `ch = b[i]` 變成未被消費的
option → `optunuse` ERROR。修法：5 處 str 索引讀取補顯式 `char`（把語義拉回 D6 之前，
golden 輸出不變），並把第二個迴圈的 `ch` 改名 `ch2`（同一函式作用域內兩個 `ch`
在 plain 型別下會報 `duplicate variable`；option 型別因捕獲合成規避了它）。

`no vet` 的 ERROR 集合同樣**逐行相同**：`tests/` 27 → 27（零差異，hint 因 `rngidx`
從 133 增至 165）、`compiler/` 76 → 76、`src/std` 0。

#### M5：`no fmt --fix=unwrap`

- 新檔案 `src/cmd/no/unwrap_fix.go` + `src/cmd/no/unwrap_fix_test.go`；
  `fmtOverflowFixes` 增分支 `case "unwrap"`，`-fix` 的 help 文案同步。
- 站點來源：**完整解析**（含 lowering）跑 `checker.ValidateUnhandledOverflow` 過濾
  `optunuse` 行號；再用 **surface 解析**（`SkipUnwrapLowering` /
  `SkipSafeIndexLowering`）取使用者寫下的那條陳述（名稱 token 的確切行列、型別
  標註、語句跨度、所屬函式）。lowering 會把 `x = arr[i]` 換成合成區塊，靠它拿不到
  「使用者寫的那個名字」，故必須兩次解析、以行號 join。
- 多行陳述曾有一個 bug：`EndPos().Line` 有時指到陳述**之後**那一行，續行被重複
  輸出（`t8-multiline-infix.no` 末尾的 `2` 印出兩次）。已收斂到最後一個有內容的行，
  並把 `[start+1, end]` 標記為「已被吃掉」不再輸出；回歸測試
  `multiline_statement_continuation_consumed` 釘住。
- 冪等實測：對 `t1.no` 連跑兩趟 `-w`，第二趟 `ok=false`（無改動）。
- 語料 dry-run（`no fmt --fix=unwrap compiler`）：命中 5 個檔案，全是
  `compiler/tests/fixtures/` 下的極小語料（`t25-nullable` / `t4-infix-prec` /
  `t45-composite-type` / `t7-group-nil-float` / `t8-multiline-infix`）。
  **刻意不套用**：這些是自舉編譯器 parser 覆蓋率的語料，改寫會動到它自己的測試
  基準；且其 optunuse 屬於「頂層指令稿的死綁定」，語意上本就該人工決定。

---

### 11.18 D10 實測：複合賦算運算符廢棄 —— option 結果自動重寫，其餘報錯（2026-10-13）

§1 的 D10 落地。語料盤點先行：**`src/std` / `tests/` / `example/` / `compiler/`
沒有任何一處真正使用 `+=` 一族**（grep 命中的全在註解與字面量裡），所以這是一個
「零遷移成本」的廢棄 —— 舊語法本來就編不過（`a += 1` 會被解析成無值綁定 + 裸字面量，
編程期報錯或結構損壞），本次只是把這條死路換成「能用 + 提示 + 自動重寫」。

**最終語義按「所在函式的結果型別」分裂**（用戶 2026-10-13 定案：`error吧`）：

| 情境 | 診斷 | 降糖／重寫 |
| --- | --- | --- |
| 函式結果是 `?T` | **WARNING**（`W_COMPOUND_ASSIGN` / `nolang-deprecated-compound-assign`） | parser 降糖成 `UnwrapAssignStatement`（`DeprecatedOp` 記下原運算符），`no fmt` 保存時輸出 `a ?= a + 1` |
| 函式結果不是 `?T` | **ERROR**，文案點名兩種可寫法 | **不降糖**，AST 原樣，`no fmt` 原樣退回（rc=1） |

ERROR 文案（`a += 1` 在普通函式裡）：

```text
line 3, column 5: [E_GENERAL] `a += 1` 已废弃，且所在函数的结果不是 option，无法上抛：
请改写成 `a + 1: { ok -> a = it, -> }`（非 ok 分支如何处理由你决定），
或把结果参数改成 `?T` 后写 `a ?= a + 1`
```

為什麼非 option 一路**不降糖**：`a + 1` 是 `?i64`，普通函式裡沒有 option 結果可以
上拋，唯一的出路是 `a + 1: { ok -> a = it, -> }`，而**非 ok 分支做什麼（丟棄？
記日誌？中斷？）只能由用戶決定**。編譯器代寫 match 就是把這個決定權偷走了，
所以只提示、不動 AST —— 也正因為 AST 不動，`no fmt` 走既有的
「parse 錯 ⇒ 原樣退回」路徑，不會把壞東西寫回檔案。

**三層面各自的位置**：

| 消費路徑 | 落點 | 形態 |
| --- | --- | --- |
| LSP 實時診斷 | `checker.ValidateDeprecatedCalls`（surface AST 看得見 `DeprecatedOp`；只在 option 結果函式裡發） | WARNING |
| `no vet` / `no build` | parser 期 `W_COMPOUND_ASSIGN`（option 結果）／`saveError`（其餘，lowering 後 surface AST 已消失） | WARNING / **ERROR** |
| 保存時 `no fmt` | parser 降糖 ⇒ formatter 渲染即輸出新寫法；非 option 一路 parse 報錯 ⇒ 原樣退回 | 自動重寫 / 不改 |

為什麼必須有 parser 期那一條：CLI 管線在 `ParseProgram` 裡就跑完 lowering，
`UnwrapAssignStatement` 被展開成 `__unwrap_N` 區塊，`RunAllLints` 看到的 merged
program 裡已經沒有 `DeprecatedOp`；而 LSP 主解析刻意設 `SkipUnwrapLowering=true`
保住 surface AST（格式-on-save 需要），所以兩邊分工、不會重複
（`p.Errors()` 只回 ERROR 級，LSP 的 diagnostics 不消費 parser warning；
複合賦算的歸屬靠 `pendingCompoundAssigns` 佇列在每個函式定義收尾時
save/flush/restore 定案，見 `parser.stmt.flushCompoundAssigns`）。

**A/B 實測**（同一凍結樹，`NOLANG_AST_CACHE_OFF=1`）：

| 語料 | 改前 rc / ERROR | 改後 rc / ERROR | 新增 WARNING |
| --- | --- | --- | --- |
| `src/std` | 0 / 0 | 0 / 0 | **0** |
| `tests/` | 1 / 27 | 1 / 27 | **0**（ERROR 行逐條相同） |

**語義等價實測**：`a += 1` → `a ?= a + 1` 之後，五個運算符串接的函式
（`41 +=1 -=1 *=2 /=2 %= 3`）在新舊寫法下都印 `2`；`a += 1; b = a*2; b /= 3`
（`f(10)`）重寫前後都印 `7`。`no fmt` 冪等（第二遍零 diff）；對普通函式裡的
`a += 1` 跑 `no fmt -w` 回 rc=1 且檔案**逐位元組不變**。

**歸屬與文案細節**（`parser/compound_assign_test.go` 採樣釘住）：兩個函式接連定義時，
各自的複合賦算按**自己**的結果型別定案（option 函式出 WARNING、普通函式出 ERROR，
互不污染）；建議文案還原使用者原本的右式（`a += g(y) * 2` ⇒ 建議
`a + g(y) * 2: { ok -> a = it, -> }`，靠 `UnwrapAssignStatement.CompoundParts()`
+ `parser.exprToString` 印出來 —— 後者因此補齊了 bare `%v` 結構傾瀉的問題）。

**刻意不做**（留下一條清楚的界線）：

- `a[i] += 1` / `d.f += 1`：不降糖。重複求值下標，且下標自己現在就是 option
  （`a[i]` 是 `?T`，`a[i] ?= a[i] + 1` 語義是錯的）。維持原來的 parse error，
  由 `TestCompoundAssignIndexTargetNotDesugared` 釘住。
- 位元複合賦算（`&=` / `|=` / `^=` / `<<=` / `>>=` / `&^=``）：不產生 option，
  與本次統一規則無關，不進廢棄名單，同樣維持 parse error。

### 11.19 ⚠️ 新發現的自相矛盾：`p1 i64 = f(n - 1)` 同時被「要求寫」與「要求刪」

遷移 `tests/wasi-fib.no`（既有 3 個 option ERROR，對照二進位上一樣有）時撞到一對
與 §11.13 同型的矛盾，但**發生在「巢狀 option 運算元」而非「索引讀取」上：

```no
#{overflow=wrap}
p1 = fib-rec(n - 1)     ; ERROR fxxoptarg：`n - 1` 是 ?i64，不能傳給 plain 形參
```

加顯式 plain 型別即可消除：

```no
#{overflow=wrap}
p1 i64 = fib-rec(n - 1) ; 0 error —— 但接著被 tcpoxtfd 要求刪掉 `i64`
```

（`i64` 確實等於推斷型別 —— `fib-rec` 回傳 `i64`，所以 `tcpoxtfd` 沒錯；
但刪掉它 `fxxoptarg` 就回來了。）根因是 `#{overflow=wrap}` **只讓「根運算元位置」的
option 被自動解包**（`a + b + c`、`f(b + c)`，見 §11.15 末段），而**不涵蓋函式引數**；
顯式 plain 型別走的才是另一條（`captureTargetAllowsOption`）路徑。

**本次不修**：要閉合需要讓 `checkRedundantTypeInStmt` 的 `optionProducing` 判據認出
「RHS 任意深處含 option 產生式子運算式」，而那會把一大批合法站點的 `tcpoxtfd`
提示一起吞掉（口徑比 §11.13 的 `producesOptionIndex` 寬得多，風險不對稱）。
已記入待辦；`tests/wasi-fib.no` 因此**保持原樣**（其 3 個 ERROR 與對照二進位一致，
非本次新增，且它是 wasm32-wasi 專用語料）。
