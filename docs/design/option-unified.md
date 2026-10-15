# 方案：算术与索引统一返回 option

> 状态：**已实施（见 §11）**——主体 M1–M5 与 M4(str/txt→?char) 已落地；D7 定案为「用户侧恒 option + 内部消除」（§8.1(a) 最终修订，2026-10-15）。**D9「下标写入必须显式解包」于 2026-10-15 定案，见 §12（编译器实现与全仓迁移进行中）**。本文件曾长期停留「待实施」草稿，§11 为 2026-10-13 实测复核结论。  
> 涉及层：parser / checker / mir / lsp / fmt / build / 语料迁移  
> 相关既有文档：`docs/docs/lang/syntax.md`〈整数运算溢出〉〈可空类型(option)〉、`NOLANG-OWNERSHIP-MODEL.md`

---

## 1. 已确认的设计决策

| #  | 决策                                    | 结论                                                                                                                                        |
| -- | ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| D1 | `#{overflow=…}` / `#{index-out=…}` 注解 | **保留，但只影响行为、不解包**。仍可写、仍生效（overflow 决定 wrap/clamp0/min/max/saturate；index-out 决定越界取值），但**结果类型恒为 `?T`**。注解**不是**消费路径 —— 仍须 `?=` 或 match 解包。 |
| D2 | 诊断分级                                  | 产生 option 处（`ovfhndld` / `idxhndld` / `ovf-int-default`）由 **ERROR → HINT**；"既没上抛、也没解包"另立 **ERROR**（新 trace `optunuse`）。                   |
| D3 | `no fmt --fix=<新类>` 修复形态              | **能上抛就上抛，否则 match 解包**：所在函数已有 `?T` 结果参数 → `x ?= <expr>`；否则 → `__o = <expr>` + `x = __o: { ok -> it, -> <默认零值> }`。                         |
| D4 | 空 `->` 臂语义                            | **默认零值**（0 / 0.0 / false / '' / 空容器 / 零值结构体）。                                                                                             |
| D5 | `-> return`                           | **新增**：值-match 的臂体为 `return` 时直接从当前函数返回，该臂不参与"各臂值类型一致"检查。                                                                                 |
| D6 | 统一规则作用范围                              | 整数 `+ - * / %`、容器索引 `arr[i]`、`str`/`txt` 索引 `s[i]`。**不含**浮点算术、`char` 算术（保持现状）。                                                            |
| D7 | in-bounds 豁免 | **最终定案（2026-10-15）：两层分离。** ①**用户侧**：即使编译器能静态证明不会溢出/除零/越界，`arr[i]` / 算术**一律产生 `?T`**，后续使用必须解包（match / `?=` / `_ =`）——**无任何用户侧豁免**。②**语言内部**：编译器在能证明安全时**内部消除** option 包装与解包（codegen/MIR 优化，如省略 nil 检查、直接走 ok 分支），用户不可见。`bounds.go` 静态分析保留，服务内部消除与诊断，**不影响用户侧类型**。 |
| D8 | match 穷尽性                             | **不放宽**。注解使失败情形变成"有值"，`nil`/`err` 臂虽永不触发仍**必须写**——值终究要靠 match 解包出来，match 是唯一的解包语法。不引入"注解后只需 `ok` 臂"的特例。                                   |
| D9 | 下标写入（index store）                   | **必须显式解包**（2026-10-15 定案）：`a[i] = v` 为 ERROR（新 trace `idxwrite`），正确写法为 place-match `a[i]: { ok -> it = v, -> _ }` 或错误上抛 `a[i] ?= v`；map（`[K]V`）不受影响；D7 两层分离延伸到写入侧（可证明界内 → 内部消除、零开销；用户侧无豁免）。详见 §12。 |

### D1 的直接后果（必须写进预期）

注解不再是消费路径，意味着：

- `#{overflow=wrap}` ⏎ `x = a + b` ⇒ `x : ?i64`（**不再**是 plain `int`）。要拿到值必须 `x: { ok -> it, … }` 或 `x ?=`。
- `#{index-out=zero}` ⏎ `x = arr[i]` ⇒ `x : ?elem`（**不再**是 plain `elem`）。
- 因此 **`src/std` 的 3557 个 `#{overflow}` 与 1007 个 `#{index-out}` 站点本次全部需要迁移**（这正是原需求里"可能需要插入很多前面的解包操作"所指）。
- §2.2 里"std 已 100% 标注 ⇒ 本次无需改动"的推论**作废**，见修订后的 §2.2。

### 尚待定案（见 §8.1）

- **D1-b**：`s[i]` 变 `?char` 的影响面最大（`src/std` 大量文本解析），先按 §7.1 测量再决定是否分期。
  **状态（2026-10-13）：已与主体同期落地。** §7.1③ 测得 src/std 11 处 + tests 34 处需迁移（几乎全是同一 x25519 十六进制解析块在 16 文件的副本），< 50 阈值，故直接同期做；`parser/type.go` 的 `containerElemType` 对 str/txt 返回 `char`，`s[i]` → `?char` 已生效（详见 §11.3）。

---

## 2. 现状（实测，2026-10-12，`f0c71559`）

### 2.1 今天谁产生 option、谁报错

| 写法                                 | 现状                   | 说明                                     |
| ---------------------------------- | -------------------- | -------------------------------------- |
| `x = a + b`（未标注，非 `?T` 函数）         | **ERROR** `ovfhndld` | 纯算术不触发就地捕获                             |
| `x = a / b`（有符号，未标注）               | `x : ?T`（自动）         | `inferOptionDivMod` 合成 `?T` 注解         |
| `x = arr[i]`（非 `?T` 函数）            | **ERROR** `idxhndld` | 索引捕获被 `enclosingFuncHasOptResult()` 挡住 |
| `x = arr[i]`（`?T` 函数内）             | `x : ?elem`（自动）      | 就地捕获                                   |
| `x = s[i]`（str）                    | `x : char`（plain）    | `isSafeIndexBase` 明确排除 str/txt         |
| `#{overflow=wrap}` ⏎ `x = a + b`   | plain `int`          | 注解解包                                   |
| `#{index-out=zero}` ⏎ `x = arr[i]` | plain `elem`         | 注解给默认值                                 |
| `x ?= a + b`                       | 上抛                   | 要求函数有 `?T` 结果参数                        |

结论：**规则不统一**——同样是"算术/索引绑定到新变量"，`/ %` 自动成 option、`+ - *` 报错、索引视所在函数而定。

### 2.2 语料规模

| 位置         | 文件  | 行数     | `#{overflow}` | `#{index-out}` | `?=` | 索引表达式（≈） | `.byte(` |
| ---------- | --- | ------ | ------------- | -------------- | ---- | -------- | -------- |
| `src/std`  | 118 | 75 156 | 3 557         | 1 007          | 2    | ~651     | 544      |
| `tests`    | 528 | 29 241 | 212           | 93             | 27   | —        | —        |
| `compiler` | 62  | —      | —             | —              | —    | —        | —        |

`./bin/no vet src/std` 当前 **0 error / 0 warning / 145 hint**——即 std 已 100% 标注完毕。  
**但这不是"本次无需改动"**：因为 D1 规定注解只影响行为、不解包，std 的 **3557 + 1007 = 4564 个  
已标注站点本次全部会从 plain 变成 `?T`**，全部需要补消费（`?=` / match / `_ =`）。  
迁移面因此有四块：

| 来源                    | 站点数（std） | 说明                   |
| --------------------- | -------- | -------------------- |
| 已标注 `#{overflow}`     | 3 557    | D1：不再解包 ⇒ 变 `?T`     |
| 已标注 `#{index-out}`    | 1 007    | D1：不再解包 ⇒ 变 `?T`     |
| 未标注但当前不报错的索引读取        | 待测       | D7：in-bounds 豁免取消后新增 |
| `str`/`txt` 索引 `s[i]` | 待测       | D6：`char` → `?char`  |

后两块的数量必须先用 §7.1 的探针测出来，再决定 §8 的分期。前两块的数量是确定的，  
是本次迁移的主体工作量（std 一处平均约 4 行，合计约 1.8 万行）。

### 2.3 相关实现位置（关键锚点）

| 关注点          | 位置                                                                                                                                                                  |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 算术/索引绑定推断    | `src/parser/lowering.go`：`inferOptionDivMod`(506)、`maybeCaptureOptionAssign`(712)、`collectCaptureNodes`(642)、`captureInfixInner`(578)、`synthesizeNullableType`(553) |
| 安全索引判定（三方共用） | `src/parser/type.go`：`isSafeIndexBase`(311)；`src/parser/bounds.go`：`AnalyzInBoundsForFunc`、`p.inBoundsIdx`                                                          |
| 零值合成（可复用）    | `src/parser/lowering.go`：`zeroValueLiteralFor`(1333)                                                                                                                |
| 值-match 臂校验  | `src/parser/desugar.go`：`validateMatchArmReturns`(135)，错误在 151 行                                                                                                    |
| match 展开     | `src/parser/lowering.go`：`lowerSurfaceMatch`(1657)                                                                                                                  |
| 未处理溢出检查      | `src/checker/checker.go`：`ValidateUnhandledOverflow`(4523)、`ValidateUnhandledIndex`(4855)、`isDirectOverflowValue`(4393)                                             |
| 诊断接线 / 严重度   | `src/checker/lint.go`：20c(507)、20d(538)、20e(558)                                                                                                                    |
| 批量修复         | `src/cmd/no/main.go`：`fmtOverflowFixes`(1556)、`fmtProcessFixFile`(1459)                                                                                             |

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
2. `x: { ok -> … , nil -> … , err -> … }` —— match 解包（穷尽性由既有 `match_exhaustive.go` 保证）。
3. `_ = expr` —— 显式丢弃。

> **注解不是消费路径（D1）**。`#{overflow=…}` / `#{index-out=…}` 只影响行为，  
> 不会让"未消费"变成合法。写了注解仍然要 match 或 `?=`。

### 3.4 诊断分级（D2）

| 场景                                          | 现状    | 目标                                    | trace              |
| ------------------------------------------- | ----- | ------------------------------------- | ------------------ |
| 未标注整数算术（"建议加注解"）                            | ERROR | **HINT**                              | `ovf-int-default`  |
| 未标注算术绑定到新变量                                 | ERROR | **HINT**（"该变量是 ?T，记得 match 或 ?="）     | `ovfhndld`         |
| 未标注索引读取绑定到新变量                               | ERROR | **HINT**                              | `idxhndld`         |
| **option 既没上抛也没解包**（含"写了注解但没 match"）        | —     | **ERROR**                             | `optunuse`（新）      |
| 区间索引惯用法建议（`for i <- [0..N) { x = arr[i] }`） | —     | **HINT**（建议 `for i <- arr { x = i }`） | `rngidx`（新）        |
| option 赋给 plain 变量 / 传给 plain 形参            | ERROR | ERROR（不变）                             | 既有 `ValidateTypes` |

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

### 4.2 空 `->` 臂 = 默认零值（D4）

```no
ao = 1 + 2
a = ao: {
    ok -> it
    ->
}
```

- 空臂在 `lowerSurfaceMatch` 展开时，按**被匹配 option 的内部类型**合成零值表达式  
  （直接复用 `zeroValueLiteralFor(tok, elem)`，与 `#{index-out=zero}` 同一张表）。
- 内部类型由 `inferIndexElemType` / `inferExprType` 得到；推断不出时**报错**（不猜）。
- `fmt` 需原样打印空臂（不能补成 `-> 0`，否则重跑不幂等）。

### 4.3 fmt 打印

- `-> return`：单行臂。
- 空臂：打印 `->` 后换行。
- 两者都要进 `src/fmt` 的 roundtrip / idempotent 测试。


## 8. 风险与取舍

### 8.1 已定案的取舍

**(a) in-bounds 豁免：两层分离（D7 最终定案，2026-10-15）。** 原稿写「取消」（用户侧一律 option），实施中一度做成「保留用户侧豁免」（D7-R1：可证明在界内的读取不产生 `?T`）。用户复核后定案为**两层分离**，纠正了「内部消除 = 用户侧豁免」的误解：

- **用户侧（语言表面语义）**：**一律 option，必须解包**。即使编译器能静态证明 `arr[i]` 不会越界、`a+b` 不会溢出、`a/b` 不会除零，`x = arr[i]` / `x = a + b` 的 `x` 仍是 `?T`，后续使用必须 match / `?=` / `_ =` 解包。**不存在**「可证明安全 → plain 类型」的用户侧捷径。诊断同理：`optunuse` / `idxhndld` 等对 in-bounds 读取照常生效。
- **语言内部（编译器优化）**：编译器在能证明安全时**内部消除** option 包装与解包——codegen/MIR 据 `bounds.go` 分析省略 nil 检查、直接走 ok 分支、不生成冗余 match 分支。这是**不可见的代码生成优化**，不改变用户可见的类型与消费义务。
- `bounds.go` 整套**保留**，服务两个消费方：①内部消除（codegen/MIR 优化依据）；②诊断口径（`rngidx` 等 HINT 在可证明安全处降噪，仅影响提示频率、不影响 ERROR/HINT 的正确性判定）。**三方共用同一份判定**（codegen 优化 / vet 提示 / fmt 删注解），但判定结果**只影响优化与提示**，不影响用户侧类型。
- 惯用替代写法仍是**直接迭代容器**：`for i <- arr { x = i }`（`i` 即元素，无下标即无 option），`rngidx` HINT 引导这个改写（§5.2）。

**(b) `s[i]`：先测量再分期（D1-b）。** 影响面最大（`src/std` 大量文本解析，且历史上刚把  
109 处 `s[i]` 改成 `s.byte(i)`）。按 §7.1 的 ③ 测出数量：

- < ~50 处 → 与主体同期做；
- 上百处 → 拆成独立里程碑：先落主体（算术 + 容器索引），再落 str/txt 索引。

> **状态（2026-10-13）：已与主体同期落地。** 实测 src/std 11 处 + tests 34 处（< 50 阈值），故未拆独立里程碑，直接同期做。`s[i]` → `?char` 已生效。

**(c) 注解失败臂的冗余：不放宽穷尽性（D8，已定案）。** 注解把失败情形变成"有值"  
（wrap ⇒ ok(回绕值)；index-out ⇒ ok(DEF)），`nil`/`err` 臂永不触发，但**仍必须写**——  
值终究要靠 match 解包出来，而 match 是唯一的解包语法。不引入"注解后只需 `ok` 臂"的特例，  
match 穷尽性规则保持单一。§7.2 需有回归钉钉住这一点（写了注解但缺 `nil`/`err` 臂 ⇒ 照报）。

### 8.2 其它风险

| 风险                           | 表现                                                         | 缓解                                 |
| ---------------------------- | ---------------------------------------------------------- | ---------------------------------- |
| **已标注站点迁移量巨大**               | std 4564 处 `#{overflow}`/`#{index-out}` 全变 `?T`，每处要补 match | `--fix=unwrap` 幂等批量；每批 vet 归零再进下一批 |
| `?T` / plain 混用错误激增          | 大量 `cannot assign ?i64 value to i64 variable`              | `--fix=unwrap` + 先测量               |
| `?=` 适用范围受限                  | `?=` 要求函数有 `?T` 结果参数，多数函数没有                                | fix 走情况 B（match），不改签名              |
| 注解失败臂冗余（D1-a）                | `#{overflow=wrap}` 后 `err` 臂永不触发却必须写                       | M1 定案；默认不放宽穷尽性                     |
| `?char` 改变 `shouldUseMemcpy` | 编译期/运行期行为漂移                                                | 改尺寸相关代码后必须跑全语料                     |
| 删 `bounds.go` 连带回归           | `no fmt` 少了一条删注解能力；既有 bounds 测试失效                          | 同期删/改测试；golden 覆盖                  |
| 诊断严重度下调漏掉真错                  | 真正的"沉默泄漏"被 HINT 掩盖                                         | 新 ERROR `optunuse` 兜底              |
| fmt 幂等被空臂破坏                  | 重跑 `no fmt` 反复改写                                           | roundtrip 测试 + 空臂原样打印              |
| 迁移把 std 改坏                   | 运行时行为变化                                                    | 每批 vet + 全语料 golden                |

### 8.3 回滚

改动跨 parser/checker/mir/fmt，且语料被批量改写，**不是单点回滚**。策略：

- 语义改动（§5.1–5.3）与语料迁移（§6）**分成不同 commit**，语义 commit 单独可 revert。
- 语义改动尽量集中在少数几个谓词（`inferOptionArith` / `isSafeIndexBase` / 诊断严重度），  
  便于用"谓词返回旧值"的方式做对照二进制（skill 的标准手法）。

---

## 9. 里程碑与实施顺序

| 阶段                | 内容                                                                            | 出口判据                                      |
| ----------------- | ----------------------------------------------------------------------------- | ----------------------------------------- |
| **M0** 测量         | §7.1 探针（`NOLANG_OPT_UNIFIED=1`）；确定 D1-b 分期                                    | 四张计数表 + 分期决定                              |
| **M1** parser 语义  | `inferOptionArith`、删注解短路、索引捕获去门、`isSafeIndexBase` 改、`maybeIndexOutAssign` 语义改 | `go test ./parser/...` 绿；新单测（含负对照）        |
| **M1b** 删 bounds  | `bounds.go` 整套 + `inBoundsIdx` + fmt 的 `AnalyzeInBoundsIndex` 下线              | 相关测试清理完毕；`go test ./...` 绿                |
| **M2** checker 诊断 | 合并生命周期检查、删 `effOverflow` 豁免、严重度下调、`optunuse` + `rngidx`                       | `go test ./checker/...` 绿；vet ERROR 集合可解释 |
| **M3** 语法         | `-> return`、空臂零值、fmt 打印                                                       | parser/fmt 测试绿；roundtrip 幂等               |
| **M4** MIR        | str/txt 索引 option 化（按 M0 决定是否同期）                                              | 全语料编译通过；尺寸相关测试绿                           |
| **M5** fix        | `--fix=unwrap`                                                                | 幂等；对语料 dry-run（`-d`）人工抽检                  |
| **M6** 迁移 std     | 按 §6.2 顺序（主体：4564 处已标注站点）                                                     | `no vet src/std` ERROR=0；golden 无 REGRESS |
| **M7** 迁移 tests   | 同上                                                                            | 全语料 golden 只出现**预期内**的 diff               |
| **M8** 收尾         | 文档（`docs/docs/lang/syntax.md` CN + `docs/i18n/en/...` EN）、golden 手工 merge     | 全部门禁绿                                     |

---

## 10. 文档同步

- `docs/docs/lang/syntax.md`〈整数运算溢出〉〈安全索引〉〈可空类型(option)〉需重写：
  - 删除"纯算术不触发捕获"的说明；
  - 统一规则 + **三条**消费路径（`?=` / match / `_ =`）+ 诊断分级表；
  - **明确写出注解只影响行为、不解包**（`#{overflow=wrap}` ⏎ `x = a + b` 的 `x` 仍是 `?i64`）；
  - `-> return` / 空臂零值；
  - in-bounds 豁免取消，并给出 `for i <- arr { x = i }` 这一惯用替代写法。
  - **下标写入必须显式解包（D9，§12）**：`a[i] = v` 不允许，必须 place-match `a[i]: { ok -> it = v, -> _ }` 或 `a[i] ?= v`；写入侧同样「用户侧恒 option、内部消除」。
- EN 镜像 `docs/i18n/en/.../lang/*.md` 同步（历史上 EN 常落后，需显式跟进）。
- `docs/docs/lang/memory.md`（若有 option/ownership 相关段落）复核。

---

## 11. 实施状态复核（2026-10-13）

> 本节为代码实测结论，覆盖 M0–M8 与 D1–D8。原 §1–§10 为设计稿，部分已偏离实际实现（本节纠正）。

### 11.1 里程碑落地

| 阶段 | 状态 | 证据 |
| --- | --- | --- |
| M0 测量 | ✅ | §7.1 探针 + §11.2 实测计数 |
| M1 parser 语义 | ✅ | `parser/lowering.go:493 inferOptionArith`；`maybeIndexOutAssign`/`maybeCaptureOptionAssign` 保留 `?T` |
| M1b in-bounds 口径 | ✅ 修订 | D7 最终定案两层分离（§8.1(a)）：用户侧恒 option、内部消除保留 bounds.go |
| M2 checker 诊断 | ✅ | `optunuse`(新 ERROR) + `rngidx`(HINT) + 严重度下调，均在 `checker.go`/`lint.go` |
| M3 语法 | ✅ | `-> return` 早退臂、空 `->` 臂零值、fmt 打印（提交 c721fe6c / 853ef9ca） |
| M4 str/txt→?char | ✅ | `parser/type.go:230-247` containerElemType 对 str/txt 返回 char；迁移 src/std 11 + tests 34 |
| M5 `--fix=unwrap` | ✅ | `cmd/no/unwrap_fix.go` |
| M6 std 迁移 | ✅ | §11.2：0 error / 59 warn / 328 hint |
| M7 tests 迁移 | ✅ | §11.2：0 error / 165 warn / 202 hint |
| M8 收尾 / §10 文档 | ⚠️ 部分 | 用户文档 `docs/docs/lang/syntax.md` 已同步统一规则；本设计文档长期停留「待实施」草稿，§11 为补录 |

### 11.2 全目录迁移验证（2026-10-13，`bin/no` v0.4.7-dirty 重建后实测）
- 设计预期：所有含 nolang 源码的目录 `no vet <dir>` ERROR=0（允许 HINT/WARN）。
- 历史计数（§2.2）：std 已标注 `#{overflow}` 3557 + `#{index-out}` 1007 = 4564 站点需补消费；D1-b 额外 11 处 str 索引。
- **实测结果（均 exit 0）：**

  | 目录 | error | warning | hint | 说明 |
  | --- | --- | --- | --- | --- |
  | `src/std` | 0 | 59 | 328 | 59 warning 全为 `arm-should`（D4 空 `->` 兜底臂「可能被执行」）；328 hint = `6kryrbsq`×143 + `rngidx`×103 + `idxhndld`×78 + `tcpoxtfd`×4 |
  | `tests` | 0 | 165 | 202 | warning 含 `STR_INDEX_COMPLEXITY`（M4 str/txt→?char 后新 lint：循环内 `s[i]` 为 O(n²)）与 `arm-should` |
  | `test` | 0 | 5 | 785 | 5 warning = 2 `nolang-overflow-ineffective` + 3 `arm-should`；785 hint 中 450 为 `tcpoxtfd`（类型注解可省略），余为 `idxhndld`/`rngidx` 等 |
  | `bench` | 0 | 0 | 1 | 仅 1 处 `sink` 未使用 hint |
  | `example` | 0 | 6 | 6 | 6 warning = 4 `STR_INDEX_COMPLEXITY`（M4 str→?char 后新 lint：循环内 `s[i]` O(n²)）+ 2 `nolang-overflow-ineffective`（注解对应陈述无整数运算，建议删）；6 hint 为 `idxhndld`/`nolang-deprecated-call` |

- 结论：**全部 5 个目录迁移完成，无阻塞错误（0 ERROR）**。剩余 warning/hint 均为新诊断（D4 空臂、`arm-should`、M4 str 索引复杂度、类型注解省略建议、溢出注解无效提示）的风格/健壮性建议，非正确性阻断；与 §2.2「0 error / 0 warning / 145 hint」基线的差异来自本次新增诊断类别，属预期。

### 11.3 与文档设计的背离（务必知悉）
1. **D7 最终定案为两层分离（2026-10-15 修订）**：原稿「取消」与中途实现「保留用户侧豁免」（D7-R1）均被纠正。用户侧**一律 option、必须解包**（in-bounds 读取照常产生 `?T`，`optunuse`/`idxhndld` 照常生效）；编译器**内部**在可证明安全时消除 option 包装/解包（codegen/MIR 优化，用户不可见）。`bounds.go` 保留但只服务内部消除与诊断降噪，不再影响用户侧类型。详见 §8.1(a)。
2. **D1-b 已落地但原稿标「待定」**：§8.1(b) 仍写「先测量再分期」，代码已与主体同期做（阈值 <50）。
3. **文档状态头长期错误**：文件头 `状态：待实施` 与实际「已基本实施」不符，本次补录 §11 纠正。

### 11.4 后续
- `no vet src/std` / `no vet tests` / `no vet test` / `no vet bench` / `no vet example` 已于 2026-10-13 实测回填 §11.2（5 个目录均 0 error）。
- 本设计文档状态头与 §8.1(a)(b) 已加「状态」注记，避免误导后续读者。

### 11.5 2026-10-14 复核：D1 收紧后的重新迁移（`fbd38fb4` / tag `v0.4.9`）

**背景**：提交 `3837fcdd`（2026-10-14 17:28）「fix(type-inference): enforce strict option arithmetic type rules」把
「**显式 plain 型别 + 算术**」由「压平」改成**硬错** `15w45dqk`（`cannot assign ?i64 value to i64 variable`）。用户同日定案：

> `k = i + 1 #{overflow=wrap}` 必须是 option；`k i64 = i + 1 #{overflow=wrap}` 是**错的**。

这使 §11.2 的「0 error」失效：**HEAD（`fbd38fb4`）的 `no vet src/std` 回退到 44 error**
（`uuid.no` / `number.no` / `yaml.no` / `net/tls.no` / … 里用显式 plain 型别压平算术的站点全部报错）。

**编译器侧修复（已在 HEAD）**：`parser/stmt.go parseLetStatement` 增 `*Identifier` 型别传播分支
（`x = y` 时把 y 的已知有号整型传给 x，仅当 x 尚无型别），使 `x-opt = pos + N` 这类链式推断不再退化成 plain；
`parser/astdiskcache.go ASTVersion` bump 到 `2026-10-14.1`（磁盘 AST 缓存键 = 内容 + ASTVersion + sigSalt，
**不含编译器二进制** ⇒ 改推断逻辑必须 bump 版本号，否则读到过期缓存）。

**语料侧重新迁移（本次）** —— 修法统一为**陈述式 match（两段式解包）**，保留 `#{overflow=wrap}`（回绕语义不变）：

```no
#{overflow=wrap}
z-opt = a + b
z = 0
z-opt: {
    ok -> z = it
    -> z = 0
}
```

三条**实测**边界（迁移时必须遵守）：
1. **窄型别 i8/i16/i32**：算术恒把结果拓宽成 `?i64`，`ok -> it` 拿到的是 i64，赋回窄型别报
   narrowing（`cannot assign i64 value to i8 variable`）⇒ 改用「先声明窄型别 plain + 注解式重绑」：
   `z i8 = 0` ⏎ `#{overflow=wrap}` ⏎ `z = a + b`。
2. **同行内联 `z = z-opt: { ok -> it` ⏎ `-> 0 }` 会坏**：整段被推成 `err | nil` 而非 `T`，下游当引数用时报
   `6fgg3htw` / `stdargtyp`（`expected 'i64', got 'err | nil'`）。**一律用多行陈述式 match。**
3. **全域**重绑加 `#{overflow=wrap}` **不会**折回 plain（局部会）⇒ `net/ws.no` 的 `RAND-COUNTER` 需
   `+ 0` 落回一般 `?i64` 算术再解包。

**实测（2026-10-14，`bin/no` @ `fbd38fb4`，`NOLANG_AST_CACHE_OFF=1`）**：

| 目录 | error | warning | hint |
| --- | --- | --- | --- |
| `src/std` | **0** | 59（全 `arm-should`） | 389 |

- **无 warning 回退**：59 与 §11.2 的 59 持平（此前一度出现的 1247 warning 是上述第 2 条内联写法造出的假象，已消除）。
- `std_ast.bin` 已随本次 std 改动重烤（`go run ./checker/genstdsig`）；`stdsig_gen.go` 未变（无签名改动）。
- `go test ./checker/ ./parser/ ./mir/ ./fmt/ ./lexer/` 全绿；`hir TestKindCoverageAcrossBothCorpora`
  为**既有**失败（`[type-arm type-match]`），与本次改动无关。
- `notools` 侧（`~/IdeaProjects/notools`）沿用同一修法，见其 `docs/design/option-unified.md`。

---

## 12. 下标写入必须显式解包（D9，2026-10-15 定案）

> 决策来源（用户定案原话，两轮综合）：「`a[1] = 2` 这样的应该报错，错误提示正确写法
> `a[1]: { ok -> it = 2; -> _ }`；另一种写法 `a[1] ?= 2`（也就是错误上抛，在返回值是 option
> 情况下）也要在报错信息中加上。对于明确推断没有越界的，内部消除，会直接赋值，没有开销，
> 但对用户来说必须使用解包语法，所有 `.no` 都要处理。」

### 12.1 决策

| # | 决策 | 结论 |
|---|---|---|
| D9 | 下标写入（index store） | **必须显式解包**。`a[i] = v`（赋值目标为 IndexExpression）为 **ERROR**（新 trace `idxwrite`），报错信息给出两种正确写法；用户层无豁免（D7 两层分离延伸到写入侧）；map 不受影响；全仓 `.no` 迁移完成后才启用 ERROR。 |

### 12.2 rationale：为什么「option 赋值不合理」（现状实测，2026-10-15）

读侧与写侧今天**不对称**，而写侧是裸的：

| 侧 | 写法 | 今天的行为 | 实测锚点 |
|---|---|---|---|
| 读 | `x = a[i]` | 恒为 `?elem`（越界 → nil），必须 `?=` / match / `_ =` 解包（D6/D7） | `parser/type.go:233 containerElemType` |
| 写 | `a[i] = v` | `emitIndexStore` = GEP + store，**无边界检查**；常量越界仅编译期拦（`checkConstIndexBounds`），运行期越界 = raw OOB write | `mir/codegen.go:8930 emitIndexStore`；`build/transpiler_validate.go:1051` |

「索引读取返回 option（越界为 nil），直接写入等于把越界变成未检查的越界写」——这与语言「无 GC、
内存安全」的目标直接冲突，且逼迫用户读侧解包、写侧裸奔，规则不统一。D9 把写侧拉到与读侧同一
纪律：**要么显式处理越界臂，要么上抛**。

另两项实测事实（直接影响实现）：

1. `a[i] ?= v` 的 parser 支持已存在（`parser/expr.go:632-638` 构造
   `UnwrapAssignStatement{Target: IndexExpression}`，结构见 `parser/ast.go:673-679`），但
   `lowerUnwrapAssign`（`parser/lowering.go:3097`）把 `Value` 当 option 降糖
   （`__unwrap_N = Value`）——对读取目标 `v ?= arr[i]` 正确，对**写入目标** `a[i] ?= 2`
   是错的，需修（ok 臂 `target = it` 的既有结构 `lowering.go:3360` 可复用）。
2. place-match 的**读侧**形态已存在（安全索引 match 主体 `parts[i]: { ok -> ... }`，
   `checker/stdast_test.go:112-117`；臂文法 `ok -> body` / `-> body` 通配臂，
   `parser/match.go:96-145`）。D9 把 statement 语境的 `a[i]: { ok -> it = v, -> _ }`
   定为**写入**形态。

### 12.3 语义规格：两种正确写法

**(1) place-match（自行处理越界臂）**

```no
a[1]: {
    ok -> it = 2      ; it = 元素位置（可读可写）：it = 2 是写入，it = it + 1 亦可
    -> _              ; 越界臂：行为由用户自定
}
```

- 求值 `a[i]` 为 option-place；ok 臂 `it` 绑定**元素位置**（不是值副本）——读 = load，
  `it = v` = store through place。实现复用 MIR 既有 lvalue/view 机制（`lvalueAddrOf`
  投影链地址计算，`mir/codegen.go:8982` 一带）。
- 通配臂（nil / 越界）行为由用户自定：`-> _` 显式丢弃 / 空臂 `->`（D4 默认零值语义）/
  `-> { log('oob'); return }` 等。
- 与 §11.5 迁移惯用法同构：**多行陈述式 match**。同行内联（`z = z-opt: { ok -> it` ⏎
  `-> 0 }`）实测会坏（§11.5 边界 2：整段被推成 `err | nil`），codemod 与文档范例一律多行。

**(2) 错误上抛 `?=`（要求所在函数有 option 结果参数）**

```no
f = (i i64) (res ?i64) {
    a = [1, 2, 3]
    a[i] ?= 2          ; ok → 写入；越界 → res = <option>; return
    res = nil
}
```

- 降糖（修正 `lowerUnwrapAssign`）：`__o_N = a[i]`（option 读）→ match；ok 臂 `a[i] = v`；
  nil/err 臂 `result = __o_N; return`。
- 所在函数**没有** option 结果参数 → 报错并引导 place-match（与
  `parser/compound_assign_test.go:121` 既定口径一致：那是 ERROR 而非 WARNING——`?=` 在
  那种函数里不合法，编译器也不能猜用户想要什么）。

### 12.4 范围界定

| 形态 | 处置 |
|---|---|
| `a[i] = v`（单赋值，基底 = 直接变数 arr/slice/vec/str/txt） | **ERROR `idxwrite`** |
| `x, a[i] = f()`（多赋值目标，`parser/stmt.go:776-835`） | **ERROR `idxwrite`** |
| `a[i] += v`（复合赋值） | 维持 parse error（`parser/stmt.go:858-869`，复合赋值只支持 IDENT LHS）；文案补两种正确写法 |
| map `[K]V`（`MapType`） | 不受影响（无下标赋值语法，走 put/get）。型别判定**必须先判 `MapType`**——`containerElemType("[str]i64")` 会把 map 型别字串误判为数组（返回 `"i64"`） |
| `self.keys[i] = v`（struct-field 基底，str-map.no 等大量存在） | **wave 1 豁免**：其读侧走 legacy 非 option 路径（`isSafeIndexBase` 明确排除 DotExpression 基底，`parser/type.go:313-321`；`lowerUnwrapAssign:3197` 对 `receiver.field[i]` 直接报错）。待 field-base 索引统一后二期 |

### 12.5 零开销：D7 两层分离延伸到写入

- **用户侧**：无豁免。即使 `a[1]` 常量界内，也必须写解包语法（与 D7 读侧口径完全一致）。
- **语言内部**：`bounds.go` 判据（定长数组 + 字面量下标；for-range 循环变量 + 字面量区间；
  `with-len(K)` 固定长切片）扩展到**写入位**索引（`collectWriteTargets`，`bounds.go:476`
  已收集写入目标，补 in-bounds 判定）；MIR 在可证明界内时消除边界检查与死臂 → 直接
  GEP+store。不可证明时成本 = 一次边界检查 + 分支（与读侧解包同构）。
- 验证：IR 级断言——可证明界内的写入站点无 bounds check、无 nil 分支（与读侧内部消除同一
  机制，D7 已落地）。

### 12.6 错误文案定稿（`idxwrite`，ERROR 级，`no vet` 同步）

```
不能通过下标直接赋值 'a[1] = 2'：索引读取返回 option（越界为 nil），直接写入等于把越界变成未检查的越界写。正确写法：
    a[1]: { ok -> it = 2, -> _ }    ; place-match：自行处理越界臂
    a[1] ?= 2                        ; 错误上抛（要求所在函数有 option 结果参数）
```

实现要点：checker 新增 `ValidateIndexWrite`（仿 `ValidateUnhandledIndex` 架构：
walkStmt/walkExpr + report + seen 去重 + canonPath 归属，`checker/checker.go:4868-4924`）；
**必须跳过合成节点**（`IsSynthetic`）——`?=` / match 降糖内部就产生 `a[i] = it`，不能误伤
（`parser/verify_test.go:154` 有先例）。

### 12.7 迁移与启用顺序

1. 编译器侧落地：两种写法可编译可运行；`idxwrite` 可报；写侧 BCE。
2. codemod：`BASE[IDX] = VALUE` → 多行陈述式 place-match（python 精确替换、先断言命中计数、
   保留缩进与行尾注释、不碰 `#{...}` 注解行与注释行、struct-field 基底站点跳过）。
   review 清单：① 依赖「常量越界 = 编译错误」的测试（tests/oob-slice-assign.no 等）改为显式
   越界臂行为；② 所在函数有 option 结果参数的热点站人工改 `?=`；③ str/txt 字节写
   （str.no 大量 `val[i] = .byte(i)`）确认 growth guard 行为不变；④ 多赋值目标形态专门设计。
3. 验证门：`make gen no` 重编 → `no vet src/std` 0 error → `no test test/std/` 47/47 →
   `tests/mem-safety/` 全绿 → 三后端冒烟 → `make test` 基线（build 4 + checker 2 + fmt 1
   为既有失败，新增即回归）。
4. 打开 `idxwrite` ERROR，全仓 vet/build 清零后收尾。

规模实测（2026-10-15）：约 **330 个 `.no` 文件**命中 `X[...] = v` 模式（含注释误报），主体
`src/std`（str.no 87、tls.no 294、x25519 282、hpack 163、argon2 96…）、`tests/`、
`test/std/`、`compiler/`、`example/`、`bench/`。

### 12.8 开放问题

- `-> _` 与空臂 `->` 的规范写法以探针定稿（全仓 `.no` 现有 0 处 `-> _` 用法，属新形式；
  bare `_` 作臂体若不过 checker，规范写法定空臂 `->` 并在文档注明）。
- 多赋值目标 codemod 形态（`x, a[i] = f()` 整体包 match 还是逐目标）。
- LSP「插入解包」quickfix（仿现有 `#{index-out = 0}` quickfix，`lsp/server.go:903`）不在
  本决策范围，后续另开。
