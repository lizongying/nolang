# 方案：算术与索引统一返回 option

> 状态：**已实施（见 §11）**——主体 M1–M5 与 M4(str/txt→?char) 已落地；D7 被主动 revert（in-bounds 豁免保留、bounds.go 未删）。本文件曾长期停留「待实施」草稿，§11 为 2026-10-13 实测复核结论。  
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
| D7 | in-bounds 豁免                          | **取消**。`bounds.go` 的"可证明在界内"分析整套下线；`for i <- [0..N) { x = arr[i] }` 也照常产生 option，不处理即报错。惯用替代写法是**直接迭代容器**：`for i <- arr { x = i }`。       |
| D8 | match 穷尽性                             | **不放宽**。注解使失败情形变成"有值"，`nil`/`err` 臂虽永不触发仍**必须写**——值终究要靠 match 解包出来，match 是唯一的解包语法。不引入"注解后只需 `ok` 臂"的特例。                                   |

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

**(a) in-bounds 豁免：取消（D7）。** 理由：定长数组 + 字面量区间的"在界内"判定**范围不好  
界定**（要证明下标恒在 `[0,N)` 需要绕开重绑定、嵌套同名 for-range、切片/vec 的未知长度…），  
与其留一个半可信的豁免，不如统一按"可能越界"处理并照常报错。

- `bounds.go` 整套下线（净删代码），`no fmt` 的"删除冗余 `#{index-out}`"一并下线。
- 惯用替代写法是**直接迭代容器**：`for i <- arr { x = i }`（`i` 即元素，无 option）。  
  需要下标本身时保留 `i <- [0..N)` 并对 `arr[i]` 显式解包。
- 新增 `rngidx` HINT 引导这个改写（§5.2）。

> **⚠️ 状态（2026-10-13）：本条被主动 revert，未落地。** 提交 `deac8377 "revert in-bounds index exemption and restore bounds.go checks"` 撤銷了豁免移除——`parser/type.go:339` 仍使用 `p.inBoundsIdx`、`bounds.go` 仍活跃，in-bounds 读取**仍不降级为 `?T`**。即 D7 按原文字面未实现，实际**保留**豁免。详见 §11.3。

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
| M1b 删 bounds | ❌ 反向 | 见 §11.3（D7 被 revert） |
| M2 checker 诊断 | ✅ | `optunuse`(新 ERROR) + `rngidx`(HINT) + 严重度下调，均在 `checker.go`/`lint.go` |
| M3 语法 | ✅ | `-> return` 早退臂、空 `->` 臂零值、fmt 打印（提交 c721fe6c / 853ef9ca） |
| M4 str/txt→?char | ✅ | `parser/type.go:230-247` containerElemType 对 str/txt 返回 char；迁移 src/std 11 + tests 34 |
| M5 `--fix=unwrap` | ✅ | `cmd/no/unwrap_fix.go` |
| M6 std 迁移 | ⚠️ 待复核 | 见 §11.2 |
| M7 tests 迁移 | ⚠️ 待复核 | 见 §11.2 |
| M8 收尾 / §10 文档 | ⚠️ 部分 | 用户文档 `docs/docs/lang/syntax.md` 已同步统一规则；本设计文档长期停留「待实施」草稿，§11 为补录 |

### 11.2 M6/M7 迁移验证（跑 `no vet src/std` / `no vet tests`）
- 设计预期：`no vet src/std` ERROR=0（允许 HINT）。
- 历史计数（§2.2）：std 已标注 `#{overflow}` 3557 + `#{index-out}` 1007 = 4564 站点需补消费；D1-b 额外 11 处 str 索引。
- 实际结果（2026-10-13，见运行日志）：_待回填_。

### 11.3 与文档设计的背离（务必知悉）
1. **D7 被主动 revert**：原设计「取消 in-bounds 豁免 + 删 bounds.go」。实际提交 `deac8377 "revert in-bounds index exemption and restore bounds.go checks"` 撤銷了豁免移除——`parser/type.go:339` 仍使用 `p.inBoundsIdx`、`bounds.go` 仍活跃，in-bounds 读取**仍不降级为 `?T`**。即 D7 按字面未落地，实际**保留**豁免。
2. **D1-b 已落地但原稿标「待定」**：§8.1(b) 仍写「先测量再分期」，代码已与主体同期做（阈值 <50）。
3. **文档状态头长期错误**：文件头 `状态：待实施` 与实际「已基本实施」不符，本次补录 §11 纠正。

### 11.4 后续
- 跑 `no vet src/std` / `no vet tests` 确认 ERROR=0，把结果回填 §11.2。
- 本设计文档状态头与 §8.1(a)(b) 已加「状态」注记，避免误导后续读者。
