# Nolang MIR — 工业级中端中间表示设计（实施记录 v2.26）

> **当前状态（截至 2026-09-21）**：legacy 后端已删除，MIR 是唯一后端（`NOLANG_MIR` 未设置即 MIR-only；`=0`/`=2` 明确报错，`=1` 保留为 lowering 转储）。实测全量 428 条中 **rc≠0 仅剩 4 条**（3 条 FFI/接口分派特性缺失 + 1 条有意死循环）；红线目标仍是 MIR 专属运行时崩溃 = 0、MIR 专属 gap = 0。
>
> **当前已闭合**：#83（`afa8d92`，sroa 大聚合爆炸）、#84（json 早返回 SIGSEGV）、#85（调用结果 NoVal / 静默错误编译）、5 处同形状空守卫体 bug（`regexp`/`x509`/`multipart`×2/`sse`）、`test-x25519-fe-diag` 测试源漂移（fe-* 参数顺序）。
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/parser/tohir.go`（AST→HIR）、`src/mir`（本层）。~~`src/build/llvm`（legacy LLVM 后端）~~ 已删除（见 §13.3）

---

## 0. 本文档的定位与 v1→v2 变更

v1（本文档前身）是一份**前向设计**：描述了 `NOLANG_MIR=1` 审计模式、`MIR→HIR` 桥（Stage 1）、以及"MIR 作为内存安全审计器运行"的路线。但实际实现**超越了 v1 的路线**：MIR 直接跳到了 `MIR→LLVM` 直发（v1 的 Stage 3），并且 env 开关语义从 v1 的 `0/1` 演进为 `0/2/3`。本 v2 把文档**对齐到真实已落地的架构**，并补充：

- 真实的 `NOLANG_MIR=0/2/3`（及 `=1` 调试）语义；
- 全量 corpus 覆盖实测（最近快照见 §10.1；#84/#85 修复后的全量统计尚未重扫）；
- 已闭环的 **63 个** MIR 专属运行时崩溃/代码生成修复（#1–#63），分类目录见 §12 摘要。red-line 里程碑：**#41** 闭环 C 家族 3 个运行时崩溃；**#42–#50** 覆盖 newtype 展开 / 跨模块调用 / 切片 / 窄整型 / 字符串方法 / 类型别名等常规语义缺口。
- 当前已知的 gap 家族与待修项：见 §13.3（BOTH_FAIL 余集仍需重扫校准）与 §16（开放风险）。
- 覆盖扫描方法论（`scripts/mir_cov.py` / `mir_sweep.py`；⚠️ 两者用**陈旧的仓库根 `no`**，权威扫描应改用 `./bin/no` 并覆盖 `tests/**/*.no`）。

> ⚠️ 历史实施约束（**已随 legacy 删除失效，仅作追溯**）：MIR 成熟期曾要求任何 MIR 失败在 `NOLANG_MIR=2` 下**必须安全回退**到 legacy HIR 路径（strangler-fig），`NOLANG_MIR=3` 关闭回退以暴露覆盖缺口。删除 legacy 后端后，`=0`/`=2` 均为明确报错，回退机制不存在；回归保护改由冻结基线承担（§13.3⑥）。

---

## 1. 动机：为什么需要 MIR（不变）

当前 Nolang 的内存安全（无 GC、deferred move + scope-exit destruction）由 `src/build/llvm/` 下约 450KB 的 emitter 在生成 LLVM IR 时**手工散布**地实现，导致三大 bug 族：

1. **双重释放 / 使用已释放（double-free / UAF）**：move 后原变量仍被结尾 `emitHeapFree` 释放。
2. **值/指针混淆导致的越界（OOB）**：返回类型传播错误使 alloca 类型与 store 类型不一致（`%vec` 被当作 `%str-long`、定宽数组被当作 `[0 x i16]`）。
3. **生命周期/借用关系无法静态验证**：新增语言特性（coroutine、FFI、泛型）时极易引入回归。

**结论**：内存安全逻辑必须从一个"散布在 450KB emitter 中的隐含约定"，提升为一个**显式、可分析、可验证、可机械翻译**的中间表示——MIR。LLVM 后端退化为 MIR 的**机械翻译器**，不再自行决定释放点。

---

## 2. 在编译 pipeline 中的位置（真实）

```
        parser         checker           HIR                MIR                LLVM (+opt/llc/clang)
  .no ──────► AST ─────────► 标注类型 ───► hir.Package ───────► mir.Module ───────► LLVM IR ──► 可执行
                         (PopulateInferredTypes)   (ASTToHIR)      (LowerHIR)        (EmitLLVM)
```

- HIR（`src/hir`）：去指针化的 AST 扁平竞技场，携带声明/推断类型字符串，但**没有 CFG、没有内存语义、没有 SSA**。
- **MIR（`src/mir`）**：从 HIR 生成；显式建模 basic block、owned/borrowed 值、move/clone/drop。
- 开关策略（strangler-fig，`src/build/transpiler.go:2780`）：

| `NOLANG_MIR` | 行为 | 回退 |
|---|---|---|
| 未设置 / `0` | 现有 HIR 路径，`GenerateHIR` 直发（legacy，默认） | — |
| `1` | `LowerHIR`+`Analyze` 打印 MIR+内存诊断到临时文件，然后**总是回退** legacy（无行为变化，调试用） | 总是 |
| `2` | 从 MIR **直接发射** LLVM IR；任何 gap/panic/opt-verify 失败 → **回退** legacy | 是（strangler-fig） |
| `3` | 从 MIR **直接发射**，关闭回退（Stage 3 全量 corpus 门禁） | **否**——任何 gap 直接让构建失败，以暴露覆盖缺口 |

`scripts/mir_cov.py` 即以 `MIR=2`（对照，应全绿/回退）与 `MIR=3`（门禁，应 rc=0）对照 `tests/*.no` 全量扫掠。

### 2.1 可达性函数级 lazy lower

从 `main` 出发建函数 worklist；`KCall`/`KFuncDef`（方法）引用到的函数从 HIR `pkg.Top`（已 merge user+std）取出一并 lower；未被引用的 std 函数不进入 MIR（死代码消除）。天然实现"user 的 hir 生成 mir，用到的 std hir 才生成 mir"。

---

## 3. 设计原则（不变）

1. **显式内存所有权**：每个值携带 `Owned`/`Borrowed`，释放点由 MIR 分析唯一决定。
2. **CFG 一等公民**：basic block + 前驱/后继 + 支配树（`cfg.go` 迭代支配算法）。
3. **准 SSA**：局部值用 `ValueID` 编号、单次定义；参数/Alloc 是定义点；块汇合处 `OpPhi`。
4. **可验证**：`Validate` 结构校验 + `Analyze` 内存诊断（use-after-move / duplicate-drop / missing-drop / borrow-escape）。
5. **机械翻译**：`EmitLLVM` 逐 op 翻译 + 调用约定映射，不含语义判断。
6. **单向依赖**：`mir` 依赖 `hir`（`src/parser`→`hir`→`mir`），**不** import `build/llvm`。

---

## 4. 数据模型（已实现，`src/mir/mir.go`）

```go
type Type struct {
    ID    TypeID
    Raw   string   // nolang 类型串: "str","i64","[]byte","?db","[3]i64","vec", struct 名...
    Kind  TypeKind // Int/Float/Bool/Char/Str/Slice/Array/Map/Option/Ptr/Struct/Func/Void/Unknown
    Owned bool     // true ⇒ 堆拥有, 需要且只需一次 Drop
    Elem  TypeID   // Slice/Array/Option/Ptr 的元素类型
    Sizes []int64  // 定宽数组维度
    Func  *FuncType // KindFunc 的 by-value 参数/结果签名
}
```

**Owned 分类**（`ClassifyOwnership`）：`str`/`vec`/`[]T`/`map`/`?T(其中 T owned)` ⇒ `Owned=true`；struct 任一字段 Owned ⇒ 整体 Owned；`&T` ⇒ false；标量及纯标量 struct ⇒ false。

**值/指令/块/函数/模块**：见 v1 §4.2，已实现（`mir.go`）。每个 MIR 值由 codegen 阶段**统一背后一个 LLVM alloca slot**（`codegen.go:17-18`），ownership/drop 因此平凡：`Drop` → `call void @str_free(...)`。

### 4.1 运行期 LLVM 结构（codegen 约定）

- `%vec` = `{ i64 len, i64 cap, i64 data_ptr }`（堆 slice）
- `%str-long` = `{ i64 len, i64 cap, i8* data }`
- `%option` = `{ i64 tag, [N x i64] slot }`（tag：0=some/ok，1=nil/none，2=err）
  - **每个 `?T` 都是这同一个类型**，payload 类型不再体现在 LLVM 类型名里，需要时由 MIR 类型表还原（`optPayloadLTOf`）。
  - **`N` 由 `option-inline-threshold` 决定**（见下），默认 24 → 32 字节的 option。
  - `sizeof(payload) <= threshold` → **内联**，按位存入 slot。
  - `sizeof(payload) > threshold`（或大小无法证明）→ **装箱**：`malloc` 一块堆内存，slot[0] 存其指针。读装箱载荷一律走 `optPayloadAddr` / `optPayloadAddrAs` / `optPayloadTypedAddr`。
  - ⚠️ 非装箱那一侧**不能**取 slot 地址：装箱载荷比 slot 大，从 slot 读它就是越界 `getelementptr inbounds` = UB，LLVM 会判定该分支不可达并把 load 提到分支之上，于是优化后解引用 err 消息当指针 → SIGSEGV（`-O0` 下完全正常，只在 `opt` 之后复现）。故那一侧指向一个 payload 大小的零常量 `@__nolang_opt_zero_<T>`。
  - ⚠️ "slot[0] 里是箱子吗" 要看 tag，而且**没有一个统一规则**：箱子是按「实际存进去的那个类型」的尺寸 malloc 的，所以只有 tag 与读取类型匹配时才能当作该类型解引用。详见 §4.2。
  - **载荷所有权与释放见 §4.1.2**：`?T` 在「载荷本身 owned」或「载荷放不进 slot、必须堆装箱」时拥有堆，`insertDrops` 据此插 drop，`@__nolang_opt_drop_<payload>` 按 tag 释放载荷内容与箱子。（旧的「装箱载荷永不释放」泄漏已消除：实测 20 万次 `v ?big = b` + match 的常驻内存从 104.5 MB 降到 1.67 MB。）
- `char` = **`i32`**：代表单个 Unicode 纯量码点（合法范围 `0 ..= 0x10FFFF`，21 bit 足够，无需 i64）。底层存储就是 i32（`codegen.llvmTypeOf`：`KindChar → "i32"`），**不是 i64**。
  - **唯一出现 i64 的地方是调用边界。** `@str_from_cp` 的 IR 签名取 `i64 %cp`（这是外部 runtime 函数的 ABI 约定），所以每个调用点把 char 的 i32 参数**零扩展**（`zext i32 → i64`）后再传入。这只是参数准备阶段的临时转换，**不改变 char 自身的类型与存储宽度**——形如 C 里 `char c='a'; f((long)c);`：变量仍是 1 字节，只是调用时临时提升。
  - 源码层签名写作 `str_from_cp(cp char) -> ?str`，参数类型是 `char`（i32）；i32→i64 的 zext 由 codegen 完成（`codegen.zextCharToI64`），用于两处：`strFromScalar`（char → str 隐式转换）与 `emitCall` 的 str 形参提升。
  - 配套的口径修正：`isScalarLLVM` 把 i32 计入「按值传递」的标量（char 形参必须按值传），`supportedLLVM` 也包含 i32；`coerce` 补上 `i32→i8`（trunc，写进 str 的 i8 元素）、`i8→i32`（zext）等转换。
  - 因为 char 现在是独立的 i32 lane，凡是「按 LLVM 宽度猜类型」的地方都必须改看**源码类型**（`rawTypeOfValue`）：i64 与 i32 不再同宽，`@str_from_cp`（UTF-8 编码）与 `@str_from_i64`（十进制文本）才不会走错。

#### 4.1.1 `option-inline-threshold`：payload 内联阈值

`?T` 的容器要不要内联载荷，是**编译期全局策略**，和结构体字段的 `#{inline=...}` 注解是**两套独立的东西**：

| | 作用域 | 控制什么 |
|---|---|---|
| 结构体字段 `#{inline=false}` / `#{layout}` | 单个结构体字段 | 该结构体**自身**的内存布局（字段是按值还是堆指针） |
| `--option-inline-threshold=N` | 整个编译单元所有 `?T` | Option 容器**装载荷**的方式（内联 vs 堆装箱） |

阈值**只改变底层布局，不改变语言语义**：无论设成多少，源码依旧统一写 `?T`，程序执行结果不变，变化的只是性能与内存分配行为。

```
no build --option-inline-threshold=128 main.no   # 多一点类型走内联，少堆分配，方便排查泄漏
no build --option-inline-threshold=24  main.no   # 默认：平衡栈开销与堆分配
no build --option-inline-threshold=8   main.no   # 极小栈环境：option 只有 16 字节
```

- **配置来源（优先级从高到低）**：命令行 `--option-inline-threshold=N` → 环境变量 `NOLANG_OPTION_INLINE_THRESHOLD=N` → `package.jsonc` 的 `compiler.option-inline-threshold`。
- **默认 24**，因为必须永远放得下的是 err 消息（一个 `%str-long` 正好 24 字节）。旧的 `{ i64, i64 }` 只有 8 字节，`err('msg')` 进 `?i64/?bool/?f64` 时只能存下堆指针（len/cap 丢失，err 臂打印出裸地址，且那次 `str_clone` 泄漏）。
- **可调范围**：最小值 **8**（一个 payload 至少得放得下一个 i64）；`< 8` 是**编译错误**。
- **8..23 会打编译警告**：`err payloads (a 24-byte str) no longer fit the slot and are heap-boxed`。此时 err 消息也走指针，`?str` 的 ok 载荷同样装箱 —— 这是这个模式的设计后果，不是退化。
- 阈值向上取整到 i64 的整数倍，于是 `%option` = `8 + 8*ceil(N/8)` 字节。

> 阈值调小**不是免费的**：`?str`（24 字节）在阈值 < 24 时会变成堆装箱，每次 option 拷贝 `optBoxClone` 都会再 malloc 一块。调小前先确认目标场景真的缺栈空间。

#### 4.1.2 option 载荷的所有权与释放（2026-09-21）

**问题**：统一 32 字节布局下，放不进 slot 的载荷由 codegen `malloc` 装箱。旧实现里 `?T` 从不被认作 owned（`ClassifyOwnership` 不把结构体算作 owned），于是 `insertDrops` 不给 option 插 drop —— 装箱载荷**永不释放**，而且 `optBoxClone` 每次 option 拷贝还会再 malloc 一块。20 万次 `v ?big = b` + match 的实测常驻内存 104.5 MB（基线 1.67 MB）。

**判定（`Module.OptionOwnsHeap`）**：`?T` 拥有堆，当且仅当

1. 载荷本身 owned —— `ClassifyOwnership(elem)` 为真（`?str` / `?vec` / `?[]T` / `?map`）；**或**
2. 载荷放不进 slot、必须堆装箱 —— `Module.OptionPayloadBoxed(elem)` 为真。这条让 `?big`（载荷是 POD 结构体、自身不 owned）也拥有那个 malloc 出来的箱子。

`typeOwnsHeap` 在 `KindOption` 上分派到它，`insertDrops` 才会为 option 插 drop。`?i64` 两条都不满足，保留原来（正确）的 no-op drop。

> ⚠️ `OptionPayloadBoxed` **走 emitter 自己的代码**（临时 `codegen` + `optionPayloadLLVMType` + `llvmTypeSizeUpper`），不重新实现一遍尺寸规则；而 slot 宽度的唯一来源是 `optionSlotBytesFor`，由 emitter 的 `initOptionSlot` 与分析共用。两侧若对「是否装箱」有分歧，就会出现一边 `free` 而另一边仍在共用同一个箱子。

**释放（`@__nolang_opt_drop_<payload>`）**：helper 必须定义在函数体外（函数体内插新 basic block 会破坏 LLVM 验证），按 tag 分支：

| tag | 动作 |
|---|---|
| 0（ok/some） | 先释放载荷内容：`%str-long` → `@str_free`；`%vec` → `free(data)`；带 owned 叶子的结构体 → `@__nolang_drop_<T>`。**装箱时**再 `free(box)`。 |
| 2（err） | err 消息是 `%str-long`；**它自己也装箱时**（阈值 < 24）同样 `@str_free` + `free(box)`。 |
| 1（nil） | 什么都不做。 |

⚠️ **tag 守卫是必须的**：默认 slot 下 err 消息内联，它的前 8 字节是字符串**长度**，看起来与箱子指针一模一样。

**为什么不会 double free**：箱子从不共享 —— wrap 每次新 `malloc`，option→option 拷贝在 `OpClone` 下由 `optBoxClone` 重新装箱并深拷。至于**载荷内容**是否共享，交给 `insertDrops` 既有的 liveness 规则（与普通结构体赋值同一套机制）：`moveStructSharesHeap` 通过 `optionCopySharesHeap` 认出两种「把共享堆的载荷存进 option」的形态 ——

- **wrap**（`o ?T = x`，`OpOptionWrap`）：载荷是按位存入 box / inline slot 的，所以 option 的叶子别名 x 的；
- **装箱的 option→option 拷贝**：`emitClone` 会重装箱（`optBoxClone`），所以这次拷贝并不消耗源。

两种形态都按同一个规则处理：

- 源**还活着** → 改写成 `OpClone`，`emitClone` 把载荷**深拷**进 option（`emitLeafFieldsClone` / `emitPtrFieldsClone`，或 option→option 走 `optBoxClone`），两侧各自拥有、各自 drop；
- 源**已死** → 保持按位 move（真正的所有权转移）：`insertDrops` 的 `OpOptionWrap` 规则豁免源，option 的 drop 是唯一释放点。

> **第三条路径是「剥壳」**（`x = opt`）：它不消耗 option，所以 option 仍然要 drop，剥出来的值就必须深拷（§4.3）。`err` 臂的 `it` 是这条路径上最容易漏的一种形态。

> 反例（修前实测）：`o ?conn = c` 后 `c.path = 'second value'` 会 `str_free` 掉箱子仍指向的缓冲区，`print(o.path)` 读到 NUL；`o = c` 的重新赋值形态（`EmitMoveInto`）更直接印出空白 —— 都因为「分析说是 move、emitter 其实是浅拷贝」这一处不一致。

> **末注 · 一个尚未修的既有坑（与本次改动无关）**：同一个 option 上再叠一次
> option→option 拷贝，会让**更靠前**的那个 match 把载荷读成空串。最小重现：
>
> ```
> o ?big = b               ; b.name = 'hello world'
> o:  { -> print(it.name) }   ; 打印空行
> print(b.name)               ; hello world
> q ?big = o
> q:  { -> print(it.name) }   ; hello world（后一个反而正常）
> ```
>
> 去掉末尾的 `q ?big = o` 则前两行都正常。这一段 **HEAD 与修后输出逐字节一致**，
> 所以是既有的 lowering 缺陷，不是本次改动引入的；`tests/test-opt-box-drop.no`
> 刻意让每个小节用自己的一套变量，就是为了不把它写进回归测试的期望里。
> （顺带一提：去掉那段拷贝后 HEAD 反而会印出空白的 `b.name` —— 那是 §4.1.2
> 正文里「wrap 浅拷贝」那类旧 bug，本次一并修好了。）

### 4.2 装箱载荷的读取规则（`optPayloadAddrAs`）

箱子是按「存进去的那个类型」的尺寸分配的，因此**"slot[0] 是不是我想要的那个箱子"必须同时看 tag 和读取类型**：

| 读取类型 `readLT` | 与声明载荷 `declaredLT` 的关系 | 哪些 tag 下 slot[0] 是 `readLT` 的箱子 |
|---|---|---|
| 非 `%str-long`（即声明载荷本身） | `readLT == declaredLT` | 只有 **tag 0**（ok 载荷） |
| `%str-long` | `declaredLT == %str-long`（`?str`） | **tag 0**（ok 字符串）和 **tag 2**（err 消息）都是，只有 tag 1（nil）不是 |
| `%str-long` | `declaredLT != %str-long`（err 消息类型双关，如 `?i64` 里的 `err('msg')`） | 只有 **tag 2** |

三者之外（nil、无消息的 `err()`）slot 都是零，靠 `slot[0] != 0` 排除。

为什么不能用一个统一规则：默认 slot（≥24）下 err 消息是内联的，它的前 8 字节 —— 字符串**长度** —— 就躺在 slot[0]，和一个箱子指针长得一模一样，所以 tag 必须参与判断；而 slot < 24 时 err 消息自己也装箱，此时把 tag 2 误判成「箱子是 824 字节的 `%sse_client`」就是一次越界读外加 `free()` 一个野指针（`malloc` 报 `pointer being freed was not allocated`），`tests/test-sse.no` 在阈值 8 下大约 12% 的复现率就是它。

`optBoxClone` 的重装箱（`emitOptionBoxHelpers`）沿用同一套 tag 规则，并且**拷贝长度要按实际装箱的东西算**：`select(tag==0, sizeof(payloadLT), sizeof(%str-long))` —— 阈值 < 24 时一个 `?sse_client` 的 err 分支只装了 24 字节，按 824 字节 memcpy 就是堆越界读。

### 4.3 剥壳 owned 载荷必须深拷（`?str` → `@str_clone`，`?[]T` → `vecDeepClone`）

`x = opt`（Option 剥壳，`emitMove` 的 `!isOptionType(dstT) && isOptionType(srcT)` 分支）**不转移所有权**——同一个 `?str` 之后可能再被剥一次（`str.replace-n` 就这么做）。所以剥出来的值必须**自己拥有一份独立的堆内存**，否则「剥出来的副本」和「option 里的载荷」会各 drop 一次 → double free。

- `payloadLT == "%str-long"` → `@str_clone`，副本拿到独立 buffer。
- `payloadLT == "%vec"`（`?[]T`）→ `vecDeepClone`（见下）。
- **err 消息的类型双关**：`dstT == "%str-long"` 而 `payloadLT != "%str-long"`（`?json` / `?i64` 这类 option 的 `err` 臂里 `it` 就是那条消息，而声明载荷是 ok 载荷的类型）→ 同样 `@str_clone`。`emitMove` 的剥壳分支按 `payloadLT` 选深拷方式，这条第三种形态曾经**漏掉**：读出来的 `%str-long` 直接按位存进目标，于是剥出来的 `it` 与 option 共用同一条消息 buffer，`drop it` 释放一次、option 自己的 drop 再释放一次 → `trace/BPT trap`。

> 触发面比看起来大：任何返回 `err(...)` 的 std 调用（`json.parse('')` 就够）只要调用方 match 到 `err` 臂就会踩到，而修前 option 从不被 drop，所以这个缺陷一直只是潜伏（剥出来的副本释放了 buffer，option 留着悬垂指针）。`isOptionPeelMove` 让 option 保留所有权，就必须让剥壳目标真正拥有一份自己的内存 —— 三者缺一不可。

**为什么 `%vec` 不能照抄 `@str_clone`**：`%vec = {i64 len, i64 cap, i64 data}` 是类型擦除的，`data` 只是个 i64。`len * sizeof(elem)` 和「元素自己是否还持有堆内存」两件事都只能从 MIR 的元素类型里问出来。所以 `vecDeepClone(elemType, depth)` 按**元素类型**特化出一份 helper（`@__nolang_vec_clone_<TypeID>_<depth>`，写进 `extraFuncsBody`，按需生成、去重）：

```
malloc(len * sizeof(elem)) + memcpy(整个 backing store)      ; 副本不再 alias 源 buffer
per element（仅当元素自己持有堆内存时）:
    elem == str      -> @str_clone，逐元素换掉 memcpy 进来的共享指针
    elem == []T      -> 递归调用内层 helper（深度上限 vecCloneMaxDepth = 4）
    elem == 标量/POD -> memcpy 就是全部，无需循环
cap 设为 len（副本只装它拿到的元素，下次 push 再增长）
```

不做逐元素修好的话，副本的元素仍指向源的 buffer，同样的 double free 只是下沉了一层。

**这个位置踩过的坑**（`tests/mem-safety/nested-container-clone.no`）：

```
m1 [str][]str ; m1.put('items', list1)
v = m1.get('items')        ; ?[]str
v: { ok -> { v.push('z') } }   ; 甚至只是 `v.len()`
```

`?[]T` 之前没有深拷，剥出来的副本直接 alias map 内部的 buffer；副本是 owned，区块结束 drop 时 `@vec_free` 把 map 的 buffer 释放掉，main 结尾 `m1`（hashmap 带 owned leaf）再 drop 一次 → `trace/BPT trap`。HEAD 上同样崩，与 `option-inline-threshold` 无关（8/16/24/64/512 每档都崩）。

⚠️ 已知限制：`vecDeepClone` 只修 `%str-long` 元素和嵌套 `%vec` 元素。**元素是有 owned `str` 叶子的 struct 时仍然只做 memcpy**，共享会下沉一层 —— 真要收紧得把 `emitLeafFieldsClone` 也做成可复用的 helper。

- `%txt` = `[255 x i8]`

---

### 4.4 短路管道与 `?T` 闸控（`OpCondBr` 的 option 条件）

语法层的 `->` 是**左结合的短路管道**，语义不随上下文变化（裸语句 / match 臂单行 body / 赋值右侧共用同一套 lowering）。HIR→MIR 把每个管道节点切成一个 block，节点后的 `cond-br` 决定是否进入下一个 block：

- **无返回的副作用节点**（`print(...)`）：不产生新 state，节点后直接 `br`，没有 `cond-br`。
- **返回 `?T` 的节点**：该值本身就是 `cond-br` 的条件。MIR 形如

```
block 2:  call dst=4:?i64 ...
          TERM cond-br args=[4] targets=[5 6]
block 5:  ; 下一个节点（state 仍 ok）
block 6:  ; 短路目标
```

codegen 端（`emitTerm` 的 `OpCondBr`）必须把该条件**翻译成 tag 测试**：`%option` 是结构体，`icmp ne %option %v, 0` 根本不是合法 IR（opt-verify 直接拒），过去任何带 `?T` 节点的管道都编译失败。正确做法是先 `extractvalue %option %v, 0` 取 tag，再 `icmp eq i64 tag, 0`（只有 ok 才继续，nil/err 都短路）。

> ⚠️ `loadVal` 在这里返回的是**已 load 的结构体值**，不是 slot 指针，所以取 tag 用 `extractvalue` 而不是 `optLoadTag`（后者要 `%option*`）。

**管道的产值（`x = A -> B -> V`）**

赋值右侧的管道整体就是赋值的值，结果是末尾的值节点。parser 把它建成 `IfExpression{Condition=A, Consequence=if B { V }}`（`parseLetStatement`，与裸语句的 standalone if-then 同一套嵌套），HIR→MIR 走既有的 `exprCapture`：把 `if` 当语句 lower，每个臂的末尾值存进共享 slot `l.exprSink`，再把 `x` 绑定到那个 slot。

> ⚠️ **共享 slot 必须在支配两个分支的 block 里播种**（`lowerIf` 末尾的 seed）。`captureArmValue` 是「第一个产出值的臂」才惰性建 slot，于是它的零初始化落在**那个臂里面** —— 任何没走到产出臂的路径（条件为假、被 `?T` 节点短路、match 没有任何臂命中）读到的都是**未初始化的栈**。这个洞很隐蔽，因为残留的栈垃圾常常正好等于臂里的值：`x = cond -> 42` 在假分支上打印 42，`x = print('F') -> might-fail(bad) -> 99` 则完全无视短路。
>
> 播种值取 `l.exprSinkInit`（外层绑定在 lower 之前记下的 `x` 当前值，owned 类型走 `OpClone`），没有则取该类型的零常量 —— 也就是「失败时赋值不生效」，与叙述形式 `{ cond -> x = 42 }` 一致。往已终结的 block 追加指令是安全的：MIR 把 `Term` 与 `Insts` 分开存，codegen 先发 `Insts` 再发 `Term`。

**类型推断**：`checker.inferExprType` 原先对 `IfExpression` 走 `default` 分支返回 `i64`，所以 `s str = 'old'; s = cond -> 'new'` 会报 `cannot assign i64 value to str variable`（既有的 `x = subject: { arms }` match 取值形式同样中招）。现在 `IfExpression` 取**臂末尾表达式**的类型（`blockTrailingExprType`；else 链本身还是 `IfExpression`，递归即可走到最后一臂），`str`/`?T` 都能正确推断。

## 5. 指令集（内存感知，已实现 `mir.go` Op 枚举）

| 类别 | Op | 语义 |
|---|---|---|
| 终结器 | `OpReturn`/`OpBr`/`OpCondBr`/`OpSwitch` | 控制流 |
| 内存 | `OpAlloc`/`OpLoad`/`OpStore`/`OpMove`/`OpClone`/`OpDrop`/`OpBorrow` | 所有权原语 |
| 数据 | `OpConst`/`OpGetField`/`OpSetField`/`OpStructLit`/`OpOptionWrap`/`OpIndex`/`OpIndexStore`/`OpSliceOp`/`OpLen`/`OpCap` | |
| 算术/逻辑 | `OpAdd`/`OpSub`/`OpMul`/`OpDiv`/`OpMod`/`OpNeg`/`OpNot`/`OpAnd`/`OpOr`/`OpBitAnd`/`OpBitOr`/`OpXor`/`OpShl`/`OpShr` | |
| 比较 | `OpEq`/`OpNe`/`OpLt`/`OpLe`/`OpGt`/`OpGe`/`OpStrEq` | `OpStrEq` 走 `@str_eq`（str 是 struct，不能 `icmp`） |
| 控制值 | `OpPhi` | 块汇合 SSA |
| 调用 | `OpCall`/`OpCallExtern`/`OpCallFFI` | 用户/外部/FFI |
| 转换 | `OpCast`/`OpTxtFromStr` | 类型转换（zext/sext 集中于此） |

**调用约定**：owned 返回值走 sret（调用方 `OpAlloc` 隐式首参），callee 写入后调用方持有；借用参数按指针传；owned 参数默认 move 或 clone。

---

## 6. 所有权与生命周期模型（不变，见 v1 §6）

- Owned 值一生：`OpAlloc(Owned)` → 使用 → 恰好一次 `OpDrop`（最后使用之后/块出口）。
- `OpMove` 转移释放责任，源不再 drop ⇒ 原生解决 double-free。
- `OpClone` 深拷贝，二者各自需一次 Drop。
- `OpBorrow` 取引用，生命周期限定在 owner 支配域内。

---

## 7. CFG 与（准）SSA（已实现 `src/mir/cfg.go`）

- `BuildCFG` 由 `Term.Targets` 推导每块的 `Preds/Succs`。
- `Dominators` 迭代支配算法（用于 borrow 生命周期界定）。
- SSA：局部值单次定义；条件分支汇合处 `OpPhi` 合并。

---

## 8. 分析 Pass（MIR 的核心价值，`src/mir/analysis.go`）

### 8.1 Liveness（反向数据流）
`liveOut(b)=∪liveIn(s)`；`liveIn(b)=use(b)∪(liveOut(b)−def(b))`。owned 值的"使用"含其释放前最后一次读；`OpDrop` 是 owned 值最后一次使用。

### 8.2 所有权分类
`ClassifyOwnership` 给每个值打 `Owned`/`Borrowed`。

### 8.3 Drop 插入算法（修复 double-free / 缺失释放）
对每函数每个 owned local，在其最后使用之后插入恰好一次 `OpDrop`；move 已转交释放责任 ⇒ 源不再 drop。

### 8.4 Move / Borrow 检查器（诊断，不直接改 IR）
- use-after-move：nolang `OpMove` 是按位拷贝（emitMove 做 load+store），源字节保持有效，故 moved 值其后 **read 安全、不诊断**（曾因此阻塞 test-std-hash.no：md5 把 `data []byte` move 进函数后多次读）。
- duplicate-drop / missing-drop / borrow-escape：分析后某 owned 值 0 或 >1 次 drop ⇒ 诊断。

### 8.5 OOB / 类型一致性（修复"值/指针混淆"）
- `Validate`（`validate.go`）结构校验：每块恰好一个 `Term`、操作数引用有效值、值单次定义。
- 定宽数组 `[N]T`（`Kind=Array`,带 `Sizes`）与 slice `[]T`（`Kind=Slice`）在 MIR **区分类型** ⇒ 消除 `[0 x i16]` 误判。
- `OpCast` 显式 zext/sext ⇒ 消除 `zext i1→i64`。

---

## 9. HIR → MIR Lowering（核心子集，按可达性，`src/mir/hir2mir.go`）

映射表见 v1 §9（已实现：`KFuncDef`→`Function`、`KLet`→`Alloc`+`Store`、`KIf`→`CondBr`+`Phi`、`KFor`→前置/条件/体/更新/出口块、`KCall`→`OpCall`（sret）、`KInfix/KPrefix`→`Op*`、`KIdent`→读 `ValueID`、`KStructLit/KArrayLit/KSliceLit`→构造指令、`KIndex/KDot`→`OpIndex/OpGetField`）。

unsupported kind ⇒ 记录 diagnostic 并安全终止该函数 lower（验证模式），不影响其他函数或现有构建。

---

## 10. MIR → LLVM 后端策略（真实进度）

- **v1 描述**：验证模式（Stage 0）→ `MIR→HIR` 桥（Stage 1）→ `MIR→LLVM` 直发（Stage 2）→ 全量切换（Stage 3）。
- **真实实现**：**跳过了 Stage 1（MIR→HIR 桥）**，直接实现 Stage 2/3 的 `MIR→LLVM` 直发（`EmitLLVM`，`codegen.go:182`）。理由：直发路径更短、oracle 对照更直接；`MIR→HIR` 桥作为历史方案保留在 v1 中但未实现。
- `EmitLLVM` 已是 `MIR → LLVM` 的直接机械翻译器：每个 `Op*` → 对应 LLVM；`OpDrop` → `emitHeapFree` 块；调用约定按 §5 映射。此时 `emitHeapFree` 的散布逻辑在 MIR 路径中已被删除，内存安全由 MIR 保证（legacy 路径仍保留散布逻辑以供回退）。

### 10.1 覆盖实测（当前口径）

> 口径：全量递归 `tests/**/*.no`（421 个）；`NOLANG_MIR=3`；逐测试 `no run` 超时 8s。门禁以 `MIR=3` 为准（`MIR=2` 回落口径会虚高“构建通过”数，不代表缺口已消）。

| 类别 | 数量 | 说明 |
|---|---|---|
| **MATCH** | **367 / 421**（≈87.2%） | `MIR=3` rc=0 且输出与 legacy 逐字节一致。 |
| **MIR 专属 gap** | **0**（抖动校正后稳定为 1：`test-diff-debug.no`） | 真值 gap 仍是 `test-diff-debug.no`（DP 表全 0，`[]str` 元素 `compare` 在两模式都错，属更底层的预存缺陷）。 |
| 预存失败（两模式都挂） | **53**（CERR 39 + CRASH 14） | legacy 也失败，**非 MIR 引入**。 |
| HANG | 1（`test-for2.no`） | 有意死循环（`while(true){}`），设计使然，不用修。 |

**结论**：MIR 专属 codegen gap = 1（`test-diff-debug`，预存非 MIR 引入）；MIR 专属运行时崩溃 = 0（red-line 持续达标）。剩余失败全部是预存/通用 bug。下一步见 §15。



---

## 11. 不变式与验证 Oracle

- **Oracle**：`NOLANG_MIR=3` 产生的可执行行为必须与 legacy（`MIR=0/2`）逐输出一致；MIR 只"把内存决策说清楚"，不改语义。
- **结构不变式**：每块恰好一个 `Term`；每 `ValueID` 单次定义；`OpDrop` 恰好一次/owned 值；`OpMove` 源之后无 read。
- **Sanitizer 回路**：对 MIR 产出二进制跑 `ASan`/`LSan` 作为回归门（长期）。

---

## 12. 已闭环目录（摘要）

MIR 专属运行时崩溃与 gap 的修复目录共 **#1–#86**，截至 2026-09-16 **全部闭环**。要点：

- **红线已达标**：MIR 专属运行时崩溃 = 0（自 #41 起）、MIR 专属 gap = 0（持续达标）。
- **默认后端已切 MIR-only**（#73），legacy 后端已删除（#79）。
- **#83 编译期 sroa 爆炸**根因由 `afa8d92` 结清（`shouldUseMemcpy` 对 >4KB 聚合改用 `llvm.memcpy` 搬运）。
- **#82** 预检去重、`#86` golden 骨架加固（超时/并发/`-update` 守卫/`UNSTABLE` 分流）已落地。

逐条闭环的过程档案（现象/根因/修法/验证）不在本文保留；完整分类卡见 git history。

---

## 13. 当前已知 gap 家族（待推进目标）

### 13.1 MIR 专属回归（已闭环）
- **`test-arr.no` 的 `option/i64` opt-verify 失配**：已于 2026-09-11 闭环，`emitIndexStore` 加 `%option`→标量解包。
- **`emitSetField` 同类解包**：`emitSetField`（`codegen.go`）的常规 store 路径与 `?T.field` 分支均加同法 option→标量解包（当 RHS 为 `%option` 且目标字段为标量时用 `optionPayloadOf` 取 ok payload 再 store）。标量赋值 `x = v` 走 `OpMove`/`emitMove`，无独立 `emitStore`，故 sink 站点已覆盖 `emitIndexStore`+`emitSetField`。
  ⚠️ 统一 slot 之后**不能**再写 `extractvalue %option %v, 1` / `getelementptr %option ..., 0, 1`：field 1 是 24 字节 slot，装箱载荷还多一层指针。统一走 `optionPayloadOf` / `optPayloadAddr` / `optPayloadTypedAddr`。

### 13.2 预存 crash/hang（非 MIR 专属，legacy 同崩/同挂）
实测根因（两模式各跑，超时 8s）：
- `vec.no` —— `SIGSEGV`（两模式 rc=1）。
- `test-std-net-ext.no` —— `SIGSEGV`（两模式）。
- `test-tls-part2.no` —— `SIGSEGV`（两模式）。
- `test-for2.no` —— `HANG`（两模式 rc=124，死循环/阻塞）。**（该文件全文两行 `{` `} (true)` —— 块调用语法写成的 `while (true) { }`，无限循环是**设计**而非缺陷；`no build` 1 秒完成、rc=0。与它同批记为 HANG 的三个 json/parse 文件**都不是**死循环，而是编译期爆炸。）**
- `tmp-getline.no` —— MIR=0 `HANG`（等 stdin）；**MIR=3 rc=0**（无输入正常退出 "no input"）——MIR 反而更好。
- `tmp-loop-test.no` —— 两模式 **rc=0**（现已通过，非真实挂死）。

**关键结论**：MIR=3 在失败前沿与 legacy **完全持平——零新增崩溃、零新增挂起**。3 个 SIGSEGV + 1 个 HANG 均为 nolang 通用预存 bug（legacy 代码生成缺陷），应作为通用 bug 在双路径闭环，不 blocking MIR 门禁。它们是独立的 legacy 修复轨道，不在 MIR 覆盖推进范围内。

### 13.3 当前状态与待修

**当前口径（2026-09-21 实测，`./bin/no run` + `tests/golden/mir-baseline.tsv`）**：全量 428 条中 **rc≠0 仅剩 4 条**（原快照为 10 条）。

**红线目标**：MIR 专属运行时崩溃 = 0、MIR 专属 gap = 0。

#### BOTH_FAIL 余集（4 条，按性质分类）

| 类别 | 文件 | 性质 |
|---|---|---|
| FFI / 外部库 + 接口分派 | `test-database-sql`、`test-ffi-mysql`、`test-ffi-sqlite` | 均报 `unknown callee sql.db.exec`。`sql.db`/`rows`/`stmt` 是**接口**（只声明方法）；具体实现在外部驱动（`example/sqlite-driver` 的 `db-sqlite sql.db`）。需实现接口分派 + C FFI 库链接（libsqlite3 / libmysqlclient），属**特性缺失**，非 MIR 缺口 |
| 有意死循环 | `test-for2` | rc=124；源码全文为 `{ } (true)`（`while(true){}`），设计使然，不用修 |

> ⚠️ **BOTH_FAIL 桶不比哈希**——“编译失败”与“编译成功但程序自己失败”被压成同一桶。判读必须直接 `diff` 指纹行，否则“编译失败”完全不可见。

#### 本轮（2026-09-21）由 rc≠0 转 rc=0 的条目（已更新 golden）

| 文件 | 原 rc | 说明 |
|---|---|---|
| `test-x25519-fe-diag` | 1（abort trap） | **测试源漂移**：`fe-*` 的 std 签名是输出在前（`fe-add = (h []i64, f []i64, g []i64)`、`fe-frombytes = (h []i64, s [32]byte)`、`fe-tobytes = (s [32]byte, h []i64)`），而测试把输出写在**最后**，`fe-frombytes(BYTES, OUT)` 把 10 个 i64 limb 写进 32 字节数组 ⇒ 越界 ⇒ abort trap。已按 `scratch-fe.no` 的正确惯例把 8 处调用改为输出在前 |
| `test-json-parse-option` | 1 | #84 闭合 |
| `test-json`、`test-https-server`、`test-sse`、`i.no` | 1 | 随本轮编译器修复（module-namespace 方法 receiver 合成等）自然转 rc=0 |

> ⚠️ 改测试前先确认 std 的**真实签名**，不要改编译器去迁就漂移的测试源。`i.no` 现输出 `3`（`'abc'.len()`），是正确行为，不是「接受了非法输入」。

#### 已修 / 近期闭环

- **#83**（`afa8d92`）：sroa 大聚合爆炸，根因已结清。
- **#84**（json 运行期 SIGSEGV，原待修）：`test-json-parse-option.no` / `test-json-nested-match.no` 在本快照之后的编译器改动中已复测 rc=0（golden 已记 rc=0），`json.parse('')` 早返回路径不再崩溃。
- **#85**（静默错误编译根因，原待修）：底层 lowering 已随 `lowerCall` 的三档 `resTyp` 回退 + `EmitCallMulti`（所有非 void 调用都填充 `inst.Results`）闭合；复测 `i.to-str()` 正确输出、`test-std-hash` 哈希全部正确。`Inst.Sym` 字段本就存在（mir.go），文档「需补 Sym」的子注已过时。
- **5 处同形状空守卫体 bug**（`src/std/{regexp,x509,multipart×2,sse}.no`，2026-09-21）：内联 guard arm 体为空、本应在其内的语句落在 arm 外，已修复并 `make no` 重建，std `no vet` 零 ERROR。
- **match 臂单行 body 里的 `->` 现在就是管道**（2026-09-21，parser `stmt.go`）：原先 `CTX_MATCH_ARM` 会掐断 standalone if-then，使得 `ok -> print('A') -> print('B')` 的第二个 `->` 被当成**新臂**（静默丢代码，`ok -> v.len() == 2 -> ok = true` 里的赋值永远不执行）。现在 `->` 语义唯一：臂由**换行**分隔，臂内的 `->` 是管道延续。配套修好 `OpCondBr` 对 `%option` 条件的处理（见 §4.4），带 `?T` 节点的管道不再编译失败。
- **`test-x25519-fe-diag` 测试源漂移**（2026-09-21）：`fe-*` std 签名为输出在前，测试却把输出写在最后；`fe-frombytes(BYTES, OUT)` 把 10 个 i64 limb 写进 32 字节数组 ⇒ 越界 ⇒ abort trap。已把 8 处调用改为输出在前（对齐 `scratch-fe.no` 惯例），rc 1→0，`FEADD=21118`/`FESUB=1104` 与手算一致。

---


## 14. 溢出默认（overflow-default）与 MIR 的集成（进行中）

`#{overflow}` 默认使整数 `+ - * /` 返回 `option<int>`（不 panic）。

- **checker 侧**：`ValidateUnhandledOverflow` 对未注解/未 `?=` 的算术报错（硬错阻断构建）；std 豁免已撤除，131 处已用 `#{overflow=wrap}` 修。
- **MIR 侧**：HIR 算术节点已被打成 `?i64`，`OpOptionWrap` 构造 `{tag,payload}`；sink 站点（`emitIndexStore`/`emitSetField`）解包、算术/负号结果包回、`emitBitwise`、`print` payload 路由、`err/ok/some` 构造器 CALL 均已闭环。

**剩余**（非算术的表达式路径，需在对应 emit 处加 `unwrapOptionOperand` + 包回）：字符串算术运算符重载推断 / async void 操作数 / void 数组 alloca。
（struct-field-on-option GEP 已闭环：`emitGetField`/`emitSetField`/`lvalueAddrOf` 的 `?T.field` 一律走 `optPayloadTypedAddr`，装箱载荷自动解引用；`tests/test-opt-struct-field.no` 与 HEAD 输出逐字节一致。）

### NOLANG_MIR_DUMP_* 调试开关

- `NOLANG_MIR_DUMP_MIR=1`：打印 lowered MIR 到 stderr。
- `NOLANG_MIR_DUMP_LL=1`：写出 emitted LLVM IR 到临时 `.ll`（路径在 stderr）。
- `NOLANG_MIR_DUMP_HIR=1` / `NOLANG_MIR_DEBUG=1` / `NOLANG_MIR_DUMP_BAD=1`：辅助诊断。
- `NOLANG_MIR_DEBUG_UNDEF=1`：打印 `loadVal` 里“类型非 void 却无槽位”的每一个点；判断命中时**看 `value=` 字段**——`value=0`（`NoVal`）才是真洞，其余（真实值 id）是合法的 void 分支。

---

## 15. 路线图（真实进度）

- **已完成**：`src/mir` 核心（模型/CFG/分析/Liveness/所有权/Drop/Move·Borrow/validator/HIR→MIR lowering）；`MIR→LLVM` 直接发射；legacy 后端删除、默认 MIR-only；跨 corpus 门禁（red-line 达标：MIR 专属崩溃=0、gap=0，默认通过率 92.2%）。
- **进行中**：溢出默认与 MIR 的集成（§14）；逐站消除 BOTH_FAIL 长尾（§13.3）。
- **待做（Stage 4 剩余）**：
  1. 收敛最后一个 MIR_GAP `test-diff-debug`（DP 表全 0 / bus error；根因落在 `[]str` 元素的 `compare` 调用降级路径，两模式都 segfault）。
  2. `with-len`/`index dst slot` void 型别族（#54 同族，5）。
  3. `str-len receiver i64`（3）。
  4. 语料迁移 8 个真·未标注溢出运算。
  5. `net-dial` 非 IP 字面量 host 的 `getaddrinfo` 回落（3）。
  6. 逐站消除长尾；FFI/async/crypto/net/map/字符串方法内置补齐。
  7. MIR 单测补齐：`datalayout`/`triple`（无可测对象）、溢出注解读取（MIR 完全无此概念）。
  8. `DIVERGE=1`（地址差异假阳性）正式标记。

---

## 16. 开放问题与风险

- **平台过滤未完全**：HIR `#{platform}` 注解已在 `src/mir/platform.go` 实现并应用于 `KFuncDef`/`KExtern`/`KLet` 注册点与脚本模式顶层内联循环；`src/mir/platform_test.go` 钉住判据。**未做**：① `KStructDef`/`KConst`/`KTypeAlias` 等其余顶层节点类型尚未过滤；② **函数体/块体内**语句两后端都不过滤（共有既有限制，已由 `TestFunctionBodyPlatformVariantIsNotFiltered` 钉住）。

- **溢出模式策略未生效（两后端共有）**：`#{overflow = clamp0 | min | max | saturate}` 实际编译中与 `wrap` 行为完全一致，注解目前只起“关掉 `option<int>` 包装”的作用。MIR 侧完全没有溢出模式概念（`src/mir/*.go` 无 `nsw`/`OverflowMode`）。要让其真正生效，需先改语言实现，再在 MIR 侧引入 `curOverflowMode` 上下文与 `Inst` 级模式分派。

- **静默错误编译（最高优先风险）**：rc=0 但输出错误，比崩溃更危险——rc 类判据（`MIR_GAP`/`REGRESS`/`CRASH`）对它完全失效，只有逐字节哈希比对能看见。#85 的 `loadVal` 护栏已把“静默”属性消除（现以 rc=1 + 精确报文响铃），但底层 lowering 缺陷未修。处理原则：① `DIVERGE`/哈希不一致先当 bug，命名“假阳性”前必须看实际输出；② 已知会不确定的条目放进 `MIR_GOLDEN_UNSTABLE` 并写明原因与 bug 号；③ **`IMPROVED` 桶要人工过一遍**——MIR 从 rc=1 变 rc=0 可能正是“接受了 legacy 正确拒绝的非法输入”，不能当成绩；④ 同类风险下次可能以别的形式出现，判据是“是否存在没有产生者的值”，`NoVal` 检查点值得作为一类断言保留。

- **跨模块 owner 判定**：与 `globalVarOwner`/`funcOwner` 对齐（`transpiler.go`）。

- **bool 打印**：legacy 自身不一致（命名变量 `true/false` vs 比较表达式 `1/0`），MIR 全站统一 `1/0`；维持现状不强行对齐（会引入新分歧）。

- **工程纪律（可复用判读口径）**：
  1. 未提交的改动必须重扫才能计入结论；文档结论只覆盖“上次扫描时的二进制”。
  2. 判定“是否本次引入”用 `git worktree` + 干净 HEAD 基线 + 逐 hunk 二分，而非只看当前失败集。
  3. 注释里“因为某机制所以安全”的推理必须回代码核对。
  4. 一个持续的哈希不一致先当 bug，不要先当“假阳性”；命名“假阳性”前必须看实际输出。
  5. 口径矛盾（同文件 rc=0 却在基线记 124）是测量条件线索，不是数据噪声。
  6. 跨改动的逐条等值 > 事后跑一遍（用改动前冻结基线做预言机）。
  7. “输出稳定”不是“没有 undef”的证据。
  8. 给出“影响半径”必须写明扫描范围。
  9. 手工探针 `no build` 与预言机 `no run` 的 rc 不是一回事（编译器成败 vs 程序退出码）。
  10. 扫描期间不要重编译 `bin/no` / 编辑扫描脚本（会混用两个版本、污染计时）。
  11. BOTH_FAIL 桶不比哈希，需直接 `diff` 指纹行。
  12. 计数有歧义时不可用作判据，必须找语义标记（如 `FlagVariadic`）。
  13. 小改判据也必须全量重扫。
  14. “行为随代码布局漂移”是读未初始化内存的签名。
