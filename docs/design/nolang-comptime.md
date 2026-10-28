# Nolang 隐式编译期求值（comptime）

> 状态：已实现并默认启用。本文说明判定规则、实现位置、边界情况与已知缺口。

---

## 1. 目标与形态

**不需要任何用户标注。** 编译器在 lowering 阶段对表达式做静态推断：当一个表达式的值
在编译期可确定（由字面量与纯运算构成），就直接求值并把结果替换为常量，不再发出对应的
算术/比较/调用指令。

- 入口：`src/mir/hir2mir.go`
  - `comptimeFold`（入口判断）→ `evalConst`（递归求值）→ `evalInfix` / `evalConstCall` / `evalConstBody`
  - 结果落地：`emitConstVal`
  - 调用钩子：`lowerExpr` 内两处 `if cv, typ, ok := l.comptimeFold(id); ok { return l.emitConstVal(cv, typ) }`
  - 常量局部表：`constLocals`；折叠结果缓存：`constCache`；函数纯度表：`comptimePure`

## 2. 判定规则

### 2.1 能被折叠的表达式形态

只有以下 HIR 节点会进入求值，其余（下标、结构体字面量、字段访问等）直接走正常 lowering：

- `KInfix`（二元运算：算术、比较、位运算、移位、逻辑）
- `KPrefix`（一元）
- `KCond`（三元）
- `KCall`（纯函数调用 / 纯方法调用）

另有**聚合**形态（`KStructLit` / `KArrayLit` / `KSliceLit`）能被求值成 `cvAgg`，
但它们**不作为折叠结果发出**（见 §6.3）：聚合没有标量常量形态，字面量仍照常 lower
成真的 struct / 数组值。聚合只用来当**接收者**与读字段 / 读下标。

### 2.2 常量来源

- **字面量**：整数、浮点、布尔、字符串、字符。
- **常量局部**：`constLocals` 记录的局部绑定。
- **纯函数调用**：见 2.3。

### 2.3 纯函数（`comptimePure`）

`computeComptimePurity` 先把所有**顶层非方法**函数标为纯，再用 `funcImpure` 排除：

- 含副作用调用（如 `print`）的函数 → 非纯；
- 调用了不在 `funcNames` 里的名字（内建/未知）→ 非纯；
- 递归 → 非纯（同时由求值栈兜底）；
- **含 `co` / `run` 派生的函数 → 非纯**（`isSpawnCall`）；
- **写模块级全局的函数 → 非纯**（见 §2.3.4）。

**方法（`FlagMethod`）按需判定**：不在上面的批量表里，而是在调用点真正用到某个方法时
才用 `funcImpure` 算一次并缓存（`isComptimePure` 的懒判定分支）。理由是 std 有几百个
方法，批量遍历每个方法体是白付的编译期成本；而只有被调用到的方法才需要结论。
（`f64.to-str` 这类 std 方法仍不会被误折叠——它们是内建特殊分派，没有可求值的
`KFuncDef` 体。）

### 2.3.1 结果参数以 `KLet` 绑定返回（关键语义）

Nolang 通过**绑定结果参数**返回值，而「标识符左手赋值」`r = <expr>` 在 HIR 里是
`KLet` **而非** `KAssign`——`parser/stmt.go` 遇到 `IDENT =` 走 `parseLetStatement`，
`AssignExpression` 只在**点/下标**左手时构造（`parser/expr.go`）。

因此 `evalConstBody` 必须把「绑定到结果参数名的 `KLet`」当作返回语句，否则值算出来了
却没被标记为返回值，会在函数体遍历结束时被丢弃。（这正是跨函数调用折叠一度完全不生效的
根因，详见 §6 的历史记录。）

### 2.3.4 写全局的函数不折叠（已修的真实错误编译）

`counter = 0` + `bump = () { counter = 5 }`，`bump()` 会被折叠成常量 7，而那次
`counter = 5` 的写入**跟着折叠一起消失了**：实测关掉 comptime 打印 `7 5`，打开
comptime 打印 `7 0`——程序行为被改变了。

根因：写全局在 HIR 里就是一个**标识符左手的 `KLet`**，和「函数内的普通局部赋值」
长得一模一样，纯性分析只看了右值，于是判成纯。

修法：`blockImpure` 的 `KLet` 分支先看这个名字是不是**模块级绑定**
（`l.globals`），是就直接判非纯。判定偏保守：局部遮蔽了同名全局时也会被判非纯，
那只是少一次折叠机会，不影响健全性。

⚠️ 这条同时是方法折叠的前置条件：方法写全局（`self` 之外的包级状态）与函数同性质，
没有这道闸门，方法折叠会把写入一起吞掉。

### 2.3.5 `char` 结果现在可折叠（已修复类型丢失）

早期 v1 让 char 算术**当成 int** 算（`evalInfix` 里 `cvChar` 与 `cvInt` 混为 `li`，结果恒返 `cvInt`），
于是 `char` 结果到达 `evalConstCall` 时只是一个 `int`，`comptimeFold` 又把它当成 `i64` 常量发出。
char 在 MIR/codegen 里是 **i32**（码点整数），而所有下游使用都**静态绑定到 char 类型**
（`c.to-str()` 一定解析成 `char.to-str`），于是那个 i64 常量让它们在「运行时是字符」的地方读到了
「数字」：`"A" + 1` 关掉 comptime 打印 `B`，打开 comptime 却打印 `66`（65+1 被当数值而非字符）。

当时用双保险守卫（`resultTypeRaw == "char"` 与 `cvInt && char` 类型）直接**拒绝折叠** char，
代价是少一次折叠机会。

**现改为修复根因而非回避**：
- `evalInfix` 的 `li` 分支：当任一操作数是 `cvChar` 时 `resKind = cvChar`，所有算术 / 位运算返回
  `comptimeVal{kind: cvChar, i: ...}`（比较运算仍是 `cvBool`）。char 算术的结果保持 char。
- `emitConstVal` 的 `cvInt` 分支：**不再把 char 静态类型 widen 成 i64**。char 在 codegen 本就是
  i32，把 char 值发成 i64 会写成 `store i64 …, i32* %slot` 的类型错配、并让下游误读成数字；
  现在直接按静态类型（char → i32）发出。
- 移除 `evalConstCall` 的 `resultTypeRaw == "char"` 守卫与 `comptimeFold` 的 `cvInt && char` 守卫；
  多结果折叠（`foldMultiResultCall`）的 `KindChar` 拒绝也一并移除（char 现可折叠）。

修复后 `"A" + 1` 与 `to-next("A")`（返回 char）在 comptime 开 / 关两态都打印 `B`（经 `to-str`），
且折叠确实发生（调用消失、`OpConst` 带 `char` 类型）——见单测 `comptime_char_test.go`
（`TestComptimeFoldsCharResult` / `TestComptimeFoldsCharExpr` / `TestComptimeFoldsMultiResultChar`
/ `TestComptimeLeavesCharRuntimeArgAlone`）。

> 取值：与其「不折叠」而保留一次优化机会的丧失，不如从求值器与发出两条链路同时修掉类型改写，
> 让 char 表达式 / 结果像运行期一样端到端保持 char 类型。

### 2.3.3 Option 结果不折叠

Option 是 `{tag, payload}` 对，`comptimeVal` 既没有 tag 也没有 nil/err 态。折叠一个
返回 `?i64` 的调用会发出**裸标量**，调用方的 match 随即读到垃圾 tag —— 实测
`mk-ok()`（结果 `?i64`）被折叠成 11 后，`p: { ok(v) -> … }` 落到了 `nil` 臂
（`test/std/option.no` 捕获）。故结果类型为 Option 时一律放弃折叠。

任务（task）不能被常量替换：

- `run <call>` / `awy <call>` 会把该调用节点强制降级为 `OpRun`（`l.forceRunCall`
  精确标记了该节点 id），`comptimeFold` 对它直接放弃；
- 裸调用 `-async` 后缀的函数同样产出 `OpRun`，`evalConstCall` 按名拒绝。

否则折叠会**静默删掉一个任务**，连带破坏 spawn-graph / tier 分析——即使被调函数本身是纯的。
参数子表达式仍可正常折叠（`forceRunCall` 只作用于那一个节点 id）。

### 2.4 局部常量必须「只写一次」（关键正确性规则）

`constLocalWrites` 统计每个局部名的写入次数；**写入超过一次即不再视为编译期常量**
（`delete(constLocals, name)`）。

这是本次修复的核心。原来的实现在「任何一次绑定」时就记录常量且从不失效，于是
**未被执行分支里的字面量会泄漏到后续折叠**。最小复现：

```
x = 1023
e2 i64 = 0
x == 0 -> { e2 = -1074 }   ; 该分支不执行
-> { e2 = x - 1075 }       ; e2 = -52
print(0 - e2)              ; 曾折叠成 0-(-1074)=1074（应为 52）
```

该 bug 的实际破坏见 §5。

### 2.4.1 `constLocalWrites` 不够：预扫描 `reassignedNames`（`txt.fields` 回归）

`constLocalWrites` 的删除发生在「**看到重复 KLet**」时——即在 lowering 过程中，第二次
对同一名字的 `KLet` 绑定出现后才 `delete(constLocals, name)`。对绝大多数「先写后读再写」
的形态这够用（§2.4 的 `e2` 分支泄漏就是这种情况）。但 `txt.fields` 暴露了它的时序盲区：

```
start = -1          ; 第一次 KLet，初始值 -1 可折叠 → 登记进 constLocals
(keep) {
    start < 0 -> { start = i ; ... }   ; 循环体首条语句就读 start
    ...
}
```

lowering 是**顺序**的：进入循环体后，先 lower `start < 0` 这个**读**，而 `start = i`
（第二次 KLet）还没被 lower，于是 `constLocalWrites` 的「删除」尚未发生。
`start < 0` 在 `evalConst(KIdent)` 里查到 `start = -1` 这个常量，被折叠成**恒真** `i1 1`
（对照 OFF 模式是 `icmp slt` 运行时比较）。后果：`start` 永远被覆写成最后一个非空白位置，
`txt.fields()` 输出每个词的末字符（`e o e`）而非词本身（`one two three`）。checker 完全
不报错，编译通过——典型的静默错答案。

修法：函数体在 `lowerBlock` **之前**先 `scanReassignedNames` 做一遍预扫描（不 descend 进
嵌套 `KFuncDef`/`KFuncLit`）：
- 统计所有名字的**写入次数**，写入来源包括 `KLet` 目标**与** `KAssign` 的 `KIdent` 目标
  （Nolang 标识符左手赋值 lower 成 `KLet`，见 §2.3.1，必须一并计入）；
- 写入次数 **≥ 2** 的名字直接记进 `reassignedNames`；
- 之后 KLet 绑定登记 `constLocals` 时，**若名字已在 `reassignedNames` 则永不登记**
  （`constLocalWrites[name] > 1 || reassignedNames[name]` 二者任一即排除）。

这是纯加法：**单赋值**常量（如 `x = 5; out = x + 3` 折出 8）完全不受影响——
`x` 只写一次，不在 `reassignedNames` 里。预扫描把「会被重复写入」这件事在**任何读取之前**
就确定下来，从根本上消除 `constLocalWrites` 的时序盲区。

单测（`src/mir/comptime_reassigned_local_test.go`）：
- `TestComptimeDoesNotFoldReassignedLocalComparison`：钉住「被重复赋值的局部的比较必须留作
  运行时 `OpLt`」——ON 模式下 `mirCountOp(on, OpLt) >= 1`。临时禁用 `scanReassignedNames`
  时本测试确 fail（0 个 `OpLt`），证明钉到位了。
- `TestComptimeFoldsSingleAssignmentLocal`：对照——单赋值常量仍应折出 8，防止误伤正常折叠。

端到端门禁：`test/std/txt.no` 的 `txt.fields` 用例，comptime 开/关两态输出必须完全一致
（`f0=[one] f1=[two] f2=[three]`）。

### 2.5 其它守卫

- **递归守卫**：`stack` 记录当前求值链上的函数名，重复即放弃折叠。
- **记忆化**：`constCache` 按节点缓存折叠结果（仅当 `env == nil`），避免重复求值。
- **溢出**：`#{overflow=wrap}` 等注解在 v1 不参与常量判定；涉及可能溢出语义的表达式
  保守地不折叠（与 `TestOptLeavesOptionArithmeticAlone` 的取向一致）。
- **Option（`?T`）**：带 option 的算术不折叠。

## 3. 开关

| 环境变量 | 含义 |
|---|---|
| `NOLANG_COMPTIME` 未设置 / `1` | **启用**（默认） |
| `NOLANG_COMPTIME=0` / `off` / `false` / `no` / `none` | 禁用 |

`comptimeEnabled()` 每次求值都动态读取，便于测试切换。

> 设计取舍：comptime 是隐式特性，必须默认开启；这个开关主要为测试服务。
> `opt_test.go` 的 `lowerWithOpt` 会把它关掉——那里的「对照组」要求程序**未经任何折叠**，
> 否则「优化 pass 到底改变了什么」就无法断言。
>
> 同样的理由，一批**断言 IR 形状**的既有单测（`@sub_zero` 里有 `sub i128`、`@plain` 是
> 无 `.byte` 的负对照、out-param 调用会发 `%cres` 临时量……）也在测试内显式关掉 comptime：
> 那里的探针函数恰好都是「纯函数 + 字面量实参」，折叠后函数根本不会被发出，
> 断言就会因为「找不到函数」而非真正的不变量而失败。测试文件内都有一行注释说明。

## 4. 测试

- 端到端：`tests/comptime.no`（`./bin/no run tests/comptime.no`）。
- 单测：`src/mir/comptime_fold_test.go`。
  每个正向用例同时断言**两个方向**：comptime 开时常量存在、关时不存在——
  只断言「常量存在」会被无关的 lowering 产物蒙混过关。
  跨函数用例：`TestComptimeFoldsPureCall`（`add(2,3)`→5）、
  `TestComptimeFoldsConstLetFn`（`propagate()`→21）、
  `TestComptimeFoldsNestedCalls`（`compose(2,3)`→25，跨两层调用链）。
  分支折叠：`TestComptimeFoldsIfBranch`（`clamp(5)`→51，
  常量条件 `if` 只取 else 臂）、`TestComptimeFoldsMatchConstant`（`classify(1)`→72，
  常量 subject 的 `match`）。
  方法折叠：`TestComptimeFoldsMethodCall`（`p.dist()`→25，常量接收者 + 纯方法）。
  反向守卫：`TestComptimeLeavesImpureCallAlone`（含 `print`）、
  `TestComptimeLeavesRuntimeCallAlone`（循环变量）、
  `TestComptimeLeavesAsyncSpawnAlone`（`run` 必须保留 `OpRun`）、
  `TestComptimeLeavesOptionResultAlone`（`?i64` 返回值必须保留调用）、
  `TestComptimeLeavesRuntimeCondIfAlone`（循环变量的 `if` 条件运行时 ⇒ 调用保留）、
  `TestComptimeLeavesLoopBodyAlone`（函数体含 `KRange` 循环 ⇒ 调用保留）、
  `TestComptimeLeavesRuntimeReceiverMethodAlone`（接收者不是常量 ⇒ 调用保留）、
  `TestComptimeLeavesMutatingMethodAlone`（方法改写接收者 ⇒ 调用保留）、
  `TestComptimeLeavesGlobalWriteAlone`（写全局 ⇒ 调用保留，见 §2.3.4）。

## 5. 案例：一次由 comptime 不健全引起的 std 破坏

`f64.to-str` 曾对所有输入输出垃圾（`1.5` → `3.3376e-308`、`4.0` → `2.225e-308`）。
排查顺序与结论（均已实测，避免重复踩坑）：

1. 证伪 f64 ABI / 接收者：`(1.5).to-bits() = 0x3ff8000000000000`、`(4.0) = 0x4010000000000000`，
   Nolang `x f64 = .` 读取也正确；`f=(n f64)(r f64){r=n}` 的位模式不变。
2. 证伪 comptime 参与：`evalConstCall` 对方法直接跳过，故 `to-str` 本身没被折叠。
3. 证伪 bigint 语义：`_add/_mul/_sub` 不原地修改操作数；`cmp` 返回 -1/0/1 正常。
4. **真因**：§2.4 的常量泄漏。`f64.to-str` 里 `e2 = -1074`（次正规分支，未执行）被记成常量，
   于是 `ne = 0 - e2` 折叠成 1074 → `den = bigint.shl(one, 1074) = 2^1074`（应为 `2^52`）
   → `base/den = 1.5·2^-1022` → 打印出次正规量级的垃圾。

顺带修掉一处真实 latent bug：`src/std/bigint.no` 的 `bigint.shl1` 曾用
`.limbs[.len] = carry` 在**恰好 `.len` 个元素**的切片上越界追加（`copy`/`one` 都用
`with-len` 分配），没有扩容；`bigint.shl` 依赖它，故 `shl` 一并修好
（`test/std/bigint.no` 的 `t-shl` 用例此前因「預存 codegen bug」被注释停用，从未验证过）。

另：`f64.to-str` 原先全程使用坏的缓冲区写入惯用法 `out[pos]: { ok -> it = X ; -> _ }`
（实测不写入缓冲区），已统一改为 `out[pos] = X` + `Nstr.byte(i)`，与其他 `to-str-*`
保持一致。

## 6. 已知缺口（v1 非目标 / 待办）

- **方法调用的折叠已支持**（§6.3），但**内建方法**（`i64.to-str`、`str.len` 等走
  `builtin_call.go` 特殊分派的）仍不折叠——它们没有可求值的 `KFuncDef` 体，
  要折叠得为每个内建单独写特例。
- 被调用函数体内含**循环**时不可折叠（`evalConstBody` 遇到 `KFor/KRange` 直接放弃——
  折叠循环需要展开或证明不变式，v1 保守放弃）。
  **常量条件分支现在可以折叠**：函数体 / 守卫里的 `if`（`cond -> body`，含 `{}` 守卫群组的
  空守卫 `->` else 臂）或 `match`，只要条件在编译期可判定，`evalConstBody` 只求值**实际会执行
  的那一条臂**（`evalConstIfBranch`），绝不碰不会执行的分支——折叠一个不可能执行的分支是不健全的。
  判定纯性时 `blockImpure`/`subtreeImpure` 对 `KIf` 改为递归两个臂去找**真实**副作用（如 `print` /
  非纯调用），而不是一刀切判非纯。`match` 降成同样的 if 链，故常量 match 一并可折叠。
  见 §6.2，单测见 `TestComptimeFoldsIfBranch` / `TestComptimeFoldsMatchConstant`。
- 多结果参数（out-param）的纯函数现在可折叠（见 §6.5），前提是：函数纯、参数可常量化、结果类型为标量（非 Option / 聚合）、函数体直序（无循环 / 分支）、且调用为 arg-form（`f(x, a, b)` 末尾传结果目标）。方法 / 变长 / 含分支或循环 / 结果为 Option·聚合 的函数仍不折叠。Nolang 的结果参数**都是具名的**，没有「匿名结果」这一支。
- **聚合常量**（struct / 数组 / 切片）目前只用于「接收者」与「读字段 / 读下标」，
  **不能作为折叠结果发出**（`evalConstCall` 见到 `cvAgg` 结果就放弃折叠）。
  要折叠一个返回 struct 的函数，还得能发出常量聚合。
- 方法里**再调另一个方法**（`self.helper()` 或 `p.other()`）暂不折叠：
  隐式接收者（`.method()`，KDot 无子节点）拿不到接收者类型，无法定位方法；
  纯性分析也把「KDot 形式的调用」一律判非纯。
- 字符串拼接的常量折叠另有既有实现，与 comptime 独立。

### 6.1 已修复：跨函数折叠一度完全不生效

`evalConstCall` 早已实现却从不触发——值其实**算对了并写进了 `env["r"]`**，但
`evalConstBody` 的 `KLet` 分支不置 `found`，只有 `KAssign`/`KExprStmt` 会置，
于是函数体遍历结束时 `if !found` 把结果丢掉。根因是误以为「Nolang 通过 `KAssign`
给结果参数赋值来返回」，而实际是 `KLet`（§2.3.1）。修复即 `KLet` 绑到结果名时
`last, found = v, true`。

顺带加固：`KExprStmt`/`KAssign` 原本写 `last, _ = l.evalConst(...)` 丢弃 `ok`，
而 `comptimeVal{}` 的零值恰好是 `{cvInt, i:0}`——一旦放宽纯性判定就会把函数**静默折叠成 0**。
现在两处都要求 `ok2`。

启用跨函数折叠后暴露出的两个真实语义问题（均已修，见 §2.3.2）：

- **`run`/`awy` 的调用被折叠 → 任务被静默删除**，spawn-graph / tier 分析随之失效。
- **裸调用 `-async` 后缀函数**同样产出 `OpRun`，按名拒绝。

- **Option 结果被折叠 ⇒ 丢失 tag。** 折叠 `?i64` 返回值的调用会发出裸标量，match
  读到垃圾 tag 走错臂（`test/std/option.no` 实测捕获，见 §2.3.3）。

另有一批既有单测的探针函数「恰好是纯函数 + 字面量实参」，折叠后不再发出，
导致 IR 形状断言因「找不到函数」而失败（非不变量被破坏）；已在这些测试内显式关闭
comptime（§3）。

### 6.2 已实现：常量条件分支折叠（`if` / `match`）

v1 早期把「含控制流的函数」整段判为非纯（`funcImpure` / `subtreeImpure` 对 `KIf` 一刀切
返回 true），于是 `if` / `match` 哪怕条件在编译期已知也从不折叠。现改为：

- **纯性判定**：`blockImpure` / `subtreeImpure` 的 `KIf` 分支递归进 `then` / `else` 两个臂，
  只在这些臂里出现真实副作用（`print`、非纯调用、循环、非 `funcNames` 的名字）时才判非纯。
  普通算术 / 比较条件不构成副作用，故常量条件的纯函数仍判纯。
- **求值**：`evalConstBody` 新增 `KIf` 直接子节点分支，以及 `KExprStmt`→`KIf`（语句位 `if` 被
  包进 `KExprStmt`）分支，二者都走 `evalConstIfBranch`：先求 `cond` 槽，若不是编译期 bool 则
  返回 false 让调用方继续扫描；否则只 `evalConstBody` 实际会走的那条臂（`then` 或 `else`），
  绝不求值另一条臂——这是健全性的核心（折叠不会执行的分支会算出错误结果）。
- **`match` 免费搭车**：`match` 降成同一个 if 链，臂体即 then 块，故常量 subject 的
  `match` 一并可折叠（臂条件比较的是 subject 变量，不是降出来的 `it` 绑定）。

单测（`src/mir/comptime_fold_test.go`）：

- `TestComptimeFoldsIfBranch`：`clamp(5)` 用 `a > 10 -> {r=7*13} -> {r=3*17}`
  （空守卫 `->` 即 else 臂），`5 > 10` 假 ⇒ 取 else ⇒ 折叠为 51，调用消失。
- `TestComptimeFoldsMatchConstant`：`classify(1)` 用 `match` 常量 subject，
  取 `1 -> r=8*9` 臂 ⇒ 折叠为 72，调用消失。
- 反向守卫：`TestComptimeLeavesRuntimeCondIfAlone`（循环变量的 `if` 条件运行时 ⇒ 调用保留）、
  `TestComptimeLeavesLoopBodyAlone`（函数体含 `KRange` 循环 ⇒ 调用保留）。

> ⚠️ 语法坑：`if cond -> a -> b` 是**坏语法**——`->` 是短路管線守卫不是 else，第二个 `->`
> 会把前一条守卫包成嵌套 `KIf`（外层 `cond` 槽挂着一个内层 `KIf`），条件永远取不到。
> 正确的 Nolang if/else 是 `{}` 守卫群組：`cond -> {…}` 接一个空守卫 `-> {…}` 作 else 臂。
> 这些单测已改用正确语法；写新用例时务必照此。

### 6.3 已实现：方法调用折叠

方法的接收者是方法的**第一个实参**，所以要折叠 `p.dist()`，`p` 本身也必须是编译期
常量。这带来两个新东西：**聚合常量** 与 **方法名定位**。

#### 聚合常量（`cvAgg`）

`comptimeVal` 原本只有标量 / 字符串。新增 `cvAgg`（有序的 (字段名, 值) 列表）：

- `evalConst` 新增 `KStructLit` / `KArrayLit` / `KSliceLit` 分支 → 求值成聚合；
  struct 字段按名字存，数组 / 切片元素按位置存（名字为空）。
- `evalConst` 新增 `KDot` 分支 → 在聚合上读字段；方法体内 `.field`（隐式接收者）
  降成**没有子节点**的 `KDot`，此时底就是当前帧的 `self` 绑定。
- `evalConst` 新增 `KIndex` 分支 → 在聚合上按下标读元素。
- 局部绑定到聚合字面量时，`comptimeAggValue` 把它记进 `constLocals`
  （`comptimeFold` 只认可发出标量常量的表达式形态，聚合没有标量形态，所以走单独入口；
  字面量本身仍然照常 lower 成真的 struct / 数组值）。

#### 方法名定位

方法在 HIR 里的名字是 **`<接收者类型>.<方法名>`**（`parser/decl.go` 用
`structToken + "." + methodName` 拼的）。所以：

- `evalConstCall` 的 `fn` 槽是 `KDot` 时，取属性名当方法名，取 `KDot` 的子节点当
  接收者表达式；接收者先求值成常量，再用它的类型名（聚合常量自带 `typeName`，
  否则回退 `typeOfNode`）拼出 `funcNames` 的键。
- 接收者是**隐式**的（方法体内的 `.method()`，`KDot` 无子节点）时拿不到接收者类型，
  一律放弃折叠。

#### 接收者绑定

方法的隐式接收者是它的**第一个结果参数**（`parser/tohir.go` 把 `self` 作为
`Results[0]` 先于 `KParam` 发出）。`evalConstCall` 因此把方法的第一个 `KResult`
（名为 `self`）当接收者绑定，其余 `KResult` 才是真正的返回值（仍要求恰好 1 个）。

#### 健全性

- **改写接收者的方法不折叠**：`.x = .x + 1` 是点左手的 `KAssign`，`blockImpure`
  判非纯；求值侧也拒（`KAssign` 的左手不是结果参数名就放弃）。
- **写全局不折叠**：见 §2.3.4。
- **聚合结果不折叠**：返回 struct 的方法暂不折叠（还不能发常量聚合）。

单测（`src/mir/comptime_fold_test.go`）：

- `TestComptimeFoldsMethodCall`：`point { x: 3, y: 4 }` 绑定给 `p`，
  `p.dist()`（`r = .x*.x + .y*.y`）⇒ 折叠为 25，调用消失。
- 反向守卫：`TestComptimeLeavesRuntimeReceiverMethodAlone`（接收者的字段绑的是循环
  下标 ⇒ 接收者不是常量 ⇒ 调用保留）、
  `TestComptimeLeavesMutatingMethodAlone`（方法改写接收者 ⇒ 调用保留）、
  `TestComptimeLeavesGlobalWriteAlone`（函数写全局 ⇒ 调用保留）。

端到端：`tests/comptime.no` 里的 `point.dist()` 用例（开关两态输出同为 25）。

⚠️ 顺带修掉一处**既有**测试的失效：`mir/rebind_retracts_projection_test.go` 的探针
`rows[0].pset()` 的接收者是常量切片元素（`['x','y']` 的第 0 个），于是方法被折叠，
`@str_pset` 调用从 IR 里消失，断言改为在 `rebindIR` 里关掉 comptime
（沿用既有做法，见 §3）。

### 6.4 已修：花括号条件链（`{ c -> s }`）里的提前 `return` 不可被折叠跨过

带花括号的条件链 `{ c -> s }` 会**降糖成一个合成的 `if <常量>`**——它的 `cond` 槽挂的
不是 bool（真正的臂-`if` 在它里面）。早期折叠器用**单个 bool** 表示「取到的分支有没有
`return`」，于是这个合成 `if` 被读成「没 return」，扫描**越过**链里的提前 `return`
继续走后面的语句：

```
leap = false
{ y % 4 != 0 -> return }   ; 2023 命中
r = true                   ; 照跑 -> 折成 true
```

后果是 `is-leap(1900)` / `is-leap(2023)` 都返回 true，`test/std/time.no` 报
「1900 not leap」/「2023 not leap」（同一二进制 `NOLANG_COMPTIME=0` 时 41/41 全过）。
这是**静默错答案**，比「不折叠」危险得多——`time` 模块是日历/闰年计算，错一个闰年
就是生产级 bug。

修法：`evalConstIfBranch` 改成**三态**（`val, returned, ok`）——解释不了的 `if` 必须让
整个折叠**放弃**（`ok == false`），而不是继续扫描。因此 `leap(2023)` 这种「函数体是
解释不了的 cond-chain」一律不折叠，调用保留下来。

单测（`src/mir/comptime_cond_chain_test.go`）：

- `TestComptimeDoesNotFoldAcrossCondChainReturn`：钉住「折叠器不能跨过条件链里的提前
  `return`」——该调用必须留下来。若将来折叠器能正确解释这个合成 `if`（折成 `false`），
  本断言需改成「折出来的值必须是 false」；取值的端到端门禁是 `test/std/time.no`。
- `TestComptimeCondChainWithNoReturnStillFolds`：对照——同样的花括号条件链，但链里
  **没有** `return`（纯赋值），必须仍可折叠。否则上一条会因「任何条件链都不折叠」而
  空洞通过。

> 取含两个方向是这道门禁有意义的前提：只断言「调用在」会被「任何 cond-chain 都不折叠」
> 蒙混过关，所以配对一个「无 return 的 cond-chain 仍要折出来 72」的对照用例。

### 6.5 已实现：多结果（out-param）折叠

Nolang 的结果参数都是具名的 out-param（如 `str.copy = () (dst str)`，调用 `src.copy(dst)`
把 `dst` 作为末尾实参传进结果参数位；`countResultParams` / `bindOutParams` 据此把末尾 N 个
实参绑回 N 个结果）。此前 comptime 只折叠恰好 1 个结果（`evalConstCall` 的
`len(resultNames) != 1` 守卫），多结果函数一律不折叠。现已增加语句位的多结果折叠（2026-10-28）：

- **求值侧**：`foldMultiResultCall` 在 `lowerCall` 入口（`resolveCallee` 解出 `callee` 后、
  任何早返回之前）对 `countResultParams(callee) > 1` 的纯 HIR 函数尝试折叠；它复用
  `isComptimePure`，收集参数与结果名，对每个结果类型做 Option / 聚合守卫（任一命中
  则整体放弃，否则 tag / 类型被静默改写），再用 `evalConstBodyMulti` 把函数体求值为
  `map[结果名]comptimeVal`（直序 KLet/KAssign 绑结果、裸 `return` 或落到函数尾；遇到
  KIf / KFor / KRange 直接放弃——v1 保守）。char 结果现在可折叠（见 §2.3.5 的修复）。
- **落地侧**：拿到 N 个常量后依次 `emitConstVal` 发成 `OpConst`，再复用**与运行时完全相同**的
  `bindOutParams(n, callee, dsts)` 把常量 `EmitMoveInto` 进末尾 N 个 identifier 实参对应的
  caller 变量，随后 `lowerCall` 直接 `return NoVal`（不发调用）。单一结果 out-param 函数仍走原路径。

健全性边界：方法（需同时绑定 receiver 常量）、变长、含分支 / 循环的函数、或任一结果为
Option / 聚合的函数**整体放弃折叠**，调用照常发出——与单结果路径的同名守卫一致。

单测（`src/mir/comptime_multi_result_test.go`）：
- `TestComptimeFoldsMultiResult`：`g = (n i64) (a i64, b i64) { a = n+1; b = n*2 }` 经 `g(5, a, b)`
  折叠为 `a=6, b=10`，`g` 的调用消失；OFF 两态对照（6 / 10 在 ON 出现、OFF 不出现）。
- `TestComptimeLeavesMultiResultRuntimeCallAlone`：反向守卫——`g(i, a, b)`（i 为循环变量）
  不得折叠，调用保留。
