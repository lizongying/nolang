# Nolang MIR — 工业级中端中间表示设计（实施记录 v2.26）

> 状态：**legacy 后端已删除，MIR 是唯一后端**（`NOLANG_MIR=0`/`=2` 现为明确报错；未设置 == `=3` == MIR-only；`=1` 保留为 lowering 转储）。**第四十轮（2026-09-16）：`src/mir` 单测 6 → 13（新增 `platform_test.go`，含判别力实测），并更正两处依据、记录两个"两后端共有"的既有限制 —— 详见 §13.3.14。** ⚠️ 特别注意 §13.3.13 ① 的更正：**`#77` 不是"MIR 偏离 legacy"，而是 legacy 在脚本模式下同样不过滤（实测 `600`）、MIR 单方面按注解的文档语义修好了它。**
> **第四十一轮（2026-09-16）：`HANG=4` 被证伪（→1，且剩下那个是有意死循环）+ 预检去重（编译耗时 −50%：11 行 json 程序 62s → 32s，普通程序不变）。** 三个 json 用例不是死循环而是**编译期爆炸** —— 根因是 LLVM `sroa` 无法廉价处理 MIR 产出的 22KB 内联聚合（`%json_json_pool = { [64 x %json_json_value], i64 }`），单跑该 pass 就 458× 膨胀（229KB → 105MB）。详见 §13.3.15 与 §12 #82/#83/#84。⚠️ 由此**推翻两条旧记载**：§13.2 的"`test-for2.no` 两模式 HANG"（它全文两行 `{ } (true)`，即块调用语法写成的 `while(true){}`）与"三个 json/parse 测试两模式都挂死、属 std 缺陷"（legacy 对它们 rc=1，**从未跑起来过**）。
> **第三十九·续轮（2026-09-16）落地：移除 strangler-fig 回退 + 删除 `src/build/llvm/`（48 文件 / 50,006 行 / 2.0MB）+ 冻结双基线 oracle，详见 §13.3.13 ⑥。**
> **第四十二轮（2026-09-16）：`rc=124` 语义清理（4 → 1，只剩那个有意死循环）+ golden 骨架加固（300s 超时 / `-P 4` / `-update` 破坏守卫 / `UNSTABLE` 分流，`DIVERGE` 从恒 1 变 0），详见 §13.3.16 与 §12 #86。** ⚠️ 本轮**推翻一条持续三轮的旧结论**：`mem-safety/str-concat-leak.no` 的哈希不一致**不是**"二进制地址差异假阳性"——该文件输出 1000 行纯文本、不含地址；真因是 **#85**：`print('item' + i.to-str())` 在 count-for 里把拼接的右操作数降级成 `undef`，于是**以 rc=0 输出 0–775MB 不确定垃圾**（2 秒可复现）。同一轮另把 **#84** 定性为**运行期 SIGSEGV**（stderr `Error: signal: segmentation fault`，崩在第一条语句 `json.parse('')` 的最简早返回路径上），并用同形状的 `/tmp` 探针排除了"22KB 值语义载荷 + option + match"这一假设。
> 第三十九轮权威全量扫描（删除前口径，`MATCH=388/422`（≈92.2%）、`DIVERGE=1`（`mem-safety/str-concat-leak.no`，二进制地址差异假阳性 —— **第四十二轮更正：并非地址差异，该文件输出纯文本、不含任何地址；真因是 #85 静默错误编译，见 §13.3.16 ⑤**）、`MIR 专属 gap=0`、`两模式都失败=29`（CERR 18 + CRASH 11）、`HANG=4`）——本轮相对第三十八轮 **MATCH 387→388**（新增 `tests/test-platform-const.no`），其余分类逐项不变、零回归。同一轮另对 `test/`（40 文件）与 `example/`（11 文件）做交叉扫描：`GAP=0`/`DIVERGE=0`/`HANG=0`。本轮交付：**#77** 平台变体过滤在脚本模式下失效（`synthesizeMainForTopLevel` 漏 `nodeMatchesPlatform`，导致语料区分不出的**真** gap）；**#9/#10 范围勘定与落地**（`src/build/llvm/` 实为 5 万行不是 60KB）。第三十八轮交付 **#71** `hir.KRegexLit` 未处理、**#72** `lowerFormatField` 的 `default:` 把结构体当整数、**#73** 默认后端切 MIR-only、**#74** `transitive_import_test.go` 接受 MIR 符号净化拼写（配套：扫描脚本补打 CERR/CRASH 名单、致命诊断带具体字段）、**#77** 平台过滤脚本模式缺口、**#78** 扫描器名单输出（净 MATCH +1、两模式都失败 30→29）。
> **第四十三轮（2026-09-16）：#85 护栏落地 —— `loadVal` 的"静默 `undef`"改为响铃诊断，并修正上一轮高估的影响半径（`tests/` 内 19 文件 → **2 文件**），详见 §13.3.17 与 §12 #85。** ⚠️ 上一轮用 `NOLANG_MIR_DEBUG_UNDEF` 量到的"19 文件 / 55 处命中"**把两类命中混在一起了**：其中 **51 处 `llvm="void"` 是合法的**（void 值本无存储，调用方正是靠 `"void"` 跳过它），**只有 4 处 `value=0` 是真洞**（`value=0` 即 `NoVal`：消费方在读取没有任何指令产生过的操作数），落在 **2 个文件**（`mem-safety/str-concat-leak.no`、`test-std-hash.no`）。这两个文件的 `legacy-baseline` 判定**本来就是 rc=1**，所以护栏是把 MIR 拉回冻结语义基线的判定 —— **对上 oracle，不是回归**；重冻结后两份基线与改动前 **恰好 diff 2 行**，且两行的新值都与 `legacy-baseline` **逐字节相同**。**把扫描范围扩到 `test/`(40)+`example/`(11) 又恰好多出 1 个**：`test/std/process.no` 护栏前 `no test` 在 **`t-cmd` 上 SIGSEGV**，护栏后变成编译期点名同一函数的诊断 ⇒ **没有任何测试由通过变失败**。另记两条判读教训：`no build` 的 rc（编译器）与 `no run` 的 rc（程序退出码）**不是一回事**；`BOTH_FAIL` 桶**不比哈希**，故 `test-std-hash.no` 的修复在桶计数上完全不可见。
> **第四十五轮（2026-09-17）：两个「语言级缺口」落地（tagged enum + safe index），并修掉一个更底层的 `option` 槽位默认值缺陷 —— 金标 `DIVERGE` 归零（`SAME=409 / DIVERGE=0 / REGRESS=0 / IMPROVED=0 / BOTH_FAIL=13`），详见 §13.3.18。** ⚠️ 本轮最有价值的不是两个大特性，而是 ③：**`%option` 的零值是 `ok(0)` 而不是 `nil`**（tag 0=ok / 1=nil / 2=err），于是**从未被赋值的 `?T` 具名出参**会把「同一槽位上一次调用的返回值」当成自己的结果 —— `hashmap.get` 未命中时只是从探针循环掉出去、`result` 从未写过，删掉的 key 因此仍报 `found`。仓库文档 `docs/docs/lang/code-style.md` 早已定义「具名出参延迟零值（option → `nil`）」，legacy 用 `%__ret_init_bitmap` 在 return 处补零，**MIR 从未实现**；本实现在 prologue 写 nil 常量，观察等价。另记一条判读方法：**「行为随代码布局漂移」是读未初始化内存的签名** —— 四次消融（禁 safe index / 强制 ok / 换 `OpCap` / 加诊断打印）每次都改变现象、而诊断从未触发，这只能是「槽位没被写过」，不是「边界算错」。顺带修 `std/collection/map.no`+`static-hashmap.no` 的 int 模板**空守卫体**（`.keys[idx] == key -> { }` 让键比较形同虚设，6 处；同形状全仓另有 5 处在 net/regexp/x509，无 oracle 故未动）。

> **关键量化：** 对同一份语料，默认口径（MIR-only）通过 **388/421**，与旧默认（`MIR=2`：`MATCH 387 + DIVERGE 1`）的 `rc=0` 集合**完全一致** —— 即切换默认不改变任何测试的成败，只是停止用 legacy 掩盖 MIR 的缺口。详见 §13.3.12。
> 作者：编译器工作流
> 关联：`src/hir`（HIR）、`src/parser/tohir.go`（AST→HIR）、`src/mir`（本层）。~~`src/build/llvm`（legacy LLVM 后端）~~ 已于第三十九·续轮删除（§13.3.13 ⑥）

---

## 0. 本文档的定位与 v1→v2 变更

v1（本文档前身）是一份**前向设计**：描述了 `NOLANG_MIR=1` 审计模式、`MIR→HIR` 桥（Stage 1）、以及"MIR 作为内存安全审计器运行"的路线。但实际实现**超越了 v1 的路线**：MIR 直接跳到了 `MIR→LLVM` 直发（v1 的 Stage 3），并且 env 开关语义从 v1 的 `0/1` 演进为 `0/2/3`。本 v2 把文档**对齐到真实已落地的架构**，并补充：

- 真实的 `NOLANG_MIR=0/2/3`（及 `=1` 调试）语义；
- 全量 corpus 覆盖实测（**权威口径（第二十轮 2026-09-13 续）：全量递归 `tests/**/*.no`=421 文件 → `MATCH=279`、`DIVERGE=43`、`MIR 专属 gap=1`、`LEGACY_FAIL=97`、`HANG=1`**；**第二十三轮（2026-09-14）两次全量重扫：round-24（争用高）MATCH=283/DIVERGE=33/MIR_GAP=0/LEGACY_FAIL=96/HANG=9；round-25（复跑）MATCH=285/DIVERGE=37/MIR_GAP=1/LEGACY_FAIL=97/HANG=1。稳定口径 MIR_GAP=1（test-diff-debug，interp 族）、HANG≈1（round-24 的 9 系 8-worker 争用下 crypto/str 两模式均超时假象，非 MIR 回归）；MATCH≈283-285、DIRVERGE≈33-37**；第十九轮同口径为 271/51/1/97/1；第十八轮 271/51/1/97/1；第十七轮 269/52/1/98/1；第十六轮 267/55/1/97/1；第十五轮 261/55/3/98/4（两次一致）；第十四轮为 248/58/17/97/1（但其"17"逐文件清单已证不准确，真实稳定 gap=3）；第十三轮 240/57/21/98/4；早期 "281/418"、"323/418"、gap=0 均为**定向子集/旧口径**，见 §10.1 与 §13.3.7 纠错）；
- 已闭环的 **63 个** MIR 专属运行时崩溃/代码生成修复目录（#1–#63，早期含 §13.3.4 顶层 main 合成结构修复 #17、`emitBitwise` option 解包包回 #18、`print` 内联 option payload 路由 #19；近期含 #19 print payload、#24–#27 mem-safety 崩溃族、#28 字符串插值、#29–#35 切片/窄整型/类型推导/内建补齐、#36 顶层未初始化全局、#37/#38 net FFI 内置、#39 `await`/`run` task 运行时、#40 async 取消/让出内置、**#41 C 家族 red-line 3 崩溃全闭环（语法化 `run` + 异步实参强制 + 任务结果类型跟踪 + `#{embed}` 物化）**；**#42 值类型别名展开（`fd=i64` 等 newtype receiver 经 `Module.ValueTypeAliases`+`expandTypeAlias` 在 `resolveCallee` 展开为底层型别，闭环 `test_errno_basic` 的 `fd.to-str`→`i64.to-str`）**；**#43 跨模块自由函数调用名解析（`# /path` 模块自由函数注册 bare 名、`resolveModuleCallName` 优先 qualified 否则回退 bare，闭环 `mem-safety/bug13-bool-coercion` 的 `helper.compute-str`→`compute-str`）**；**#44 字符串插值方法调用字段（`lookupFormatValue` 扩展 `ident.method()` 模式，闭环 `test-diff-debug` 的 `eprint('{content.len-bytes()}')` interp gap）**；**#45 数组字面量元素类型推导（`lowerArrayElems` 优先使用 `typeHint` 的元素类型，闭环 `data []byte = [0x61,0x62,0x63]` 生成 `[3]i64` 而非 `[3]byte` 导致的 SHA1/HMAC 哈希错误）**；**#46 反向切片 `@mir_slice_copy` 运行时 helper + `rightInc` codegen 处理（`lowerSlice` 移除 `hi+1`，`emitSliceOp` 统一 `abs(hi-lo)+rightInc` 长度计算 + `@mir_slice_copy` 运行时 helper 调用，避免 MIR codegen 基本块分支）**；**#47 具名函数类型别名 `TypeAliases` 注册 + `resolveCallee` 间接调用路由（`collectValueTypeAliases` 新增 `FlagFuncType` 的 `TypeAliases` 注册，`resolveCallee` `KIdent` 分支检测 `KindFunc` 类型参数 → `emitIndirectCall`，闭环 `test-named-fn-type.no` 的 `unknown callee setup`）**；**#48 `for-in` 对 slice/vec 的迭代支持（`lowerRangeFor` collection form 新增 `OpLen` 运行时长度获取，闭环 `test-quant-all1.no` 的 DIVERGE——regexp 库中的 `for-in` 切片迭代静默错误）**；**#49 函数引用作为参数传递（新增 `OpFuncRef` 操作 + `funcSigRaw` 从 HIR 函数定义构建 `fn(params)(results)` 类型字符串，`lowerExpr` KIdent 分支检查 `funcNames` 并发射 `KindFunc` 类型的值，`loadVal` 解析为 `@funcname`，闭环 `test-named-fn-type.no` 的 CRASH——函数指针参数为 `undef`）**；**#50 字符串 `!=` 比较修复（`hir2mir.go`：`str != str` 原来通过 `OpNe(eq, false)` 实现，但 `OpNe(eq, false)` 等价于 `eq != false` 即 `eq` 本身——不是取反。改为 `OpNot(eq)`（xor 1），正确反转 `OpStrEq` 的结果。修复了 `test-rand-multiassign.no` 等依赖 `str != str` 条件分支的静默错误）**；**#53–#62 见 §12 目录正文（第二十九·三十轮）：`net-listen`/`net-accept`/`net-udp-open` 内置（#53）、KLet 回退改判 `void`（#54）、MIR 平台过滤（#55）、重载 mangling 后的 callee 名解析（#56）、具体切片方法名回落（#57）、`process-pipe`/`process-waitpid-nohang` 内置 + `process-kill` `CmpRet`（#58）、`emitCmp` option 语义二修（#59）、`break` 蹦床重复 start-drop（#60）、`with-len` 元素步长与清零（#61）、回退未提交的 `emitFunc` owned 参数不别名改动（#62）、**`emitCallBody` 变参启发式把"显式写出的具名出参实参"误当变参展开（#63：`lcs = (a []str, b []str) (ops []diff-op)` 以 `lcs(a, b, ops)` 调用时 `len(Args)=3 > len(inParams)=2`，而末入参 `b []str` 恰好是切片 → 把 `b`/`ops` 打进 `%cav` 假 vec；因新降级的切片字面量此时仍是定长数组 `[3 x %str-long]`，IR 出现 `%vec` 与 `[3 x %str-long]` 混用 → LLVM 校验整模块失败（`'%lv39' defined with type '%vec' but expected '[3 x %str-long]'`），元素型别一旦偶然吻合则 IR 通过但切片被静默污染。修法：新增 `mir.Function.Variadic`（`hir2mir.go` 由 `hir.FlagVariadic` 置位）作为消歧依据——仅当 `!cf.Variadic` 且 `len(Args)==len(inParams)+len(outParams)` 时才把尾部实参视作出参实参并从计数中扣除；若不加此判据直接扣减，会把 `number.max(10, 20)`（1 个展开入参 + 1 个出参，恰为 2 实参）误判成"无展开"，使 `test-number-generic.no`/`test-number.no` 回归 trace/BPT（本轮实测踩到并已双向验证））**——本节头部列表里 #51/#52 的旧编号与 §12 正文不同轨（#51 起两处各记各的），**以 §12 正文为权威**）**；
- 当前已知的 gap 家族：**第三十一轮（2026-09-15）权威全量扫描：`MATCH=367`、`DIVERGE=0`、`MIR 专属 gap=0`、`两模式都失败=53`、`HANG=1`（`test-for2.no`，两模式都挂死）**（全量递归 `tests/**/*.no`=421，本轮 421 全部纳入统计；口径见下）。**第三十轮同口径为 MATCH=367/DIVERGE=0/gap=1/失败=52/HANG=1**，第二十九轮 364/1/2/53/1，第二十八轮 352/0/0/68/1。**第三十→三十一轮的配方变化（gap 1→0、CRASH 13→14）全部来自抖动，不是修复**：`test-diff-debug.no` 的 legacy 基线 6 次里失败 1 次（健康 5/6 时被标 MIR_GAP，抖动时落 CRASH）；`test-quant-all1.no` 两模式都打印未初始化栈指针（第三十轮标 DIVERGE、第三十一轮标 MATCH，其 legacy 自身输出不自洽）。**抖动校正后的真实态：MIR 专属 gap = 1（`test-diff-debug.no`）、DIVERGE = 0**。① 唯一真实 gap `test-diff-debug.no`：MIR=3 恒错（DP 表恒为全 0，6/6），legacy DP 表恒正确（6/6，`1 1 1 0`）——`a[ai].compare(b[aj])` 对相等元素（`'c'`vs`'c'`、`'a'`vs`'a'`）在 MIR=3 返回非 0；但其 legacy 基线自身也有 1/6 崩溃率（同一处 `ops` 构建循环），故按 §13.3.7 口径时通时不通，属"两模式共有的深层缺陷 + MIR 侧确定性放大的表征"；② 第三十轮的 2 个 gap（`mem-safety/bug15-read-dowhile-copyfile.no`、`test-process-run.no`）仍**闭环**（#55–#59）；③ 第三十轮回退的 `emitFunc` 改动（#62）未再回归；④ **本轮 #63 修的是"文档 gap 数以外"的一类**：它只在"显式写出具名出参实参 + 末入参是切片"的组合下触发，语料中原本没有该形状的测试，故不出现在任何轮次的 gap 数里（新发现路径见 §13.3.10）。详见 **§13.3.7 / §13.3.9 / §13.3.10**；
- 覆盖扫描方法论（`scripts/mir_cov.py` / `mir_sweep.py`；⚠️ 两者用**陈旧的仓库根 `no`**，权威扫描应改用 `./bin/no` 并覆盖 `tests/**/*.no`）。

> ⚠️ 历史实施约束（**已随 legacy 删除失效，仅作追溯**）：MIR 成熟期曾要求任何 MIR 失败在 `NOLANG_MIR=2` 下**必须安全回退**到 legacy HIR 路径（strangler-fig），`NOLANG_MIR=3` 关闭回退以暴露覆盖缺口。第三十九·续轮删除 legacy 后，`=0`/`=2` 均为明确报错，回退机制不存在；回归保护改由冻结基线承担（§13.3.13 ④⑥）。

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
- `%option` = `{ i64 tag, i64 payload }`（tag：0=some/ok，1=nil/none，2=err）
- `%txt` = `[255 x i8]`

---

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

### 10.1 覆盖实测（2026-09-11 全量递归扫掠 `tests/**/*.no`，MIR=3，8s/测试超时）

> 口径：全量递归 `tests/**/*.no` = **418** 个 `.no`；`NOLANG_MIR=3`（关闭 legacy 回退）；逐测试 `no run` 超时 8s。此口径下：committed 基线（§13.3.4 前）= **244/418**；§13.3.4 + `emitBitwise` 修复后 = **280/418**；本轮 `print` option payload 路由修复后 = **281/418**。文档早期 "≈287/418" 是**非递归** `tests/*.no`（≈368 文件）测量，总数不可直接比较。

| 类别 | 数量（**第三十一轮 2026-09-15 权威全量并行扫描（`./bin/no`，全量递归 `tests/**/*.no` = 421，421 全部纳入统计，`scripts/mir_sweep_fast.sh`（超时工具已修，#63 同轮）8-worker/120s 超时）**：**MATCH=367 DIVERGE=0 CERR=39 CRASH=14 HANG=1（`test-for2.no`）MIR 专属 gap=0**。CERR/CRASH 全部 legacy 也失败。**与第三十轮（367/0/1/52/1）的全部差异都是抖动**：`test-diff-debug.no` MIR_GAP→CRASH（其 legacy 基线 1/6 失败）、`test-quant-all1.no` DIVERGE→MATCH（两模式都打印未初始化栈指针）。**抖动校正后：gap=1、DIVERGE=0**） | 说明 |
|---|---|---|
| **MATCH** | **367 / 421** (≈87.2%)（**第三十一轮 2026-09-15**） | `MIR=3` rc=0 且输出与 `MIR=2` 逐字节一致（全量并行扫描）。第三十轮的 367 由 `mem-safety/bug15-read-dowhile-copyfile.no`（#55/#57/#58/#59）、`test-process-run.no`（#55/#56/#58/#59）、`test-tls-part1.no`/`test_char_step4.no`/`tmp-nbody-debug.no`/`test-realpath-debug.no`/`tmp-arr-f1.no`（#62 回退引入它们的 `emitFunc` 改动）构成；第三十一轮该数不变（#63 修的是语料未覆盖的形状，见 §13.3.10），DIVERGE 位由 `test-quant-all1.no` 的抖动填充/让出 |
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **0**（**第三十一轮全量并行扫描实测**，2026-09-15；全量 421 文件全部纳入）**但抖动校正后为 1** | 扫描标签为 0 仅因 `tests/test-diff-debug.no` 的 legacy 基线本轮抖动失败（6 次里 1 次）而落进 CRASH 桶。**真实 gap 仍是它**：MIR=3 恒错（DP 表 6/6 全 0，`a[ai].compare(b[aj])` 对相等 str 返回非 0），legacy 健康时 DP 表 6/6 正确（`1 1 1 0`）。同一处 `ops` 构建循环在 legacy 侧也有 1/6 崩溃率（`segfault`）——即"两模式共有的深层缺陷，MIR 侧确定性放大"。历史弧线：69 → 41 → … → 21（13 轮）→ 17 → 3 → 1（16 轮）→ 0（flake 掩）→ 1（稳定）→ 2（#51 浮出）→ 1（第三十轮）→ **0/1（第三十一轮，标签 0、真值 1）** |
| 预存失败（两模式都挂） | **53**（第三十一轮：CERR 39 + CRASH 14）／52（第三十轮：39+13）／53（第二十九轮）／68（第二十八轮） | legacy 也失败，**非 MIR 引入**。CRASH 桶 14 个已逐个复跑 5×legacy+3×MIR：只有 `test-diff-debug.no` 的基线不稳定（3/5 成功），其余 13 个两模式均 0/5、0/3 全败（确定性双失败）。家族分布见 §13.3.9 / §13.3.10 |
| HANG | 1（`test-for2.no`） | 脚本带 120s 超时（`run_to` 已改为 `setpgid` + `kill -KILL -$pgid` 杀整个进程组，见 §13.3.10）；`test-for2.no` **两模式都挂死**，本轮由工具自身在 120s 内干净杀掉（无残留进程），无需人工 `pkill` |

> **构建计数口径提示（避免 331↔299 混淆）**：本表 MATCH/失败/HANG 均为 **`MIR=3`（关闭回退）运行口径**。另有两种"构建通过"计数常被误用：`MIR=2`（fallback 开启）下 MIR codegen 失败会回退 legacy，故"构建通过"数虚高（前期轮次报的 **331/418** 即此口径，含大量靠回退掩盖的 gap）；`MIR=3` 真实"构建通过"数为 **299/418**（第十一·续轮 #37/#38 把 `test-net-client`/`tmp-icmp-test` 等 net 测试从构建失败转为构建通过，由 ≈297 升至 299）。门禁以 `MIR=3` 为准；`MIR=2` 计数仅供对照、不代表缺口已消。

> **第七轮（2026-09-12）**：全量重扫 **289 → 298**（净 +9 = 9 个 mem-safety MIR 专属崩溃测试全转 PASS，**零 MIR 专属回归**）。MIR 专属运行时崩溃（red-line）**10 → 0**（§12 #24–#27）。FAIL 15 → 5、HANG 3 → 4。
>
> **第八轮（2026-09-12，字符串插值族）**：全量重扫 **298 → 315**（净 **+17**，**零回归**：原 298 个 PASS 无一退化）。本轮闭环 `interp` 家族 15 个中的 **15 个转 rc=0**（其中 6 个 `test-guard`/`test-quant-all`/`test-quant-all1`/`test-re-debug`/`test-regexp`/`test-regexp1` 是 **legacy 反而失败、MIR 正确**：legacy 对含 `{...}` 的普通字面量（JSON/正则）会误当插值而崩，MIR 现在只在 print 家族调用点做替换）。另 3 个（`test-embed`/`test-sha1-minimal`/`test-hmac2`）虽 rc=0 但仍与 legacy 输出不同，属**已定位的 MIR 错值缺陷**（见 §13.3.6）。

> **第九轮（2026-09-12，类型推导 / 字符串语义 / 内建补齐）**：全量重扫 **315 → 316 → 318 → 320**（累计净 **+5**，**零回归**：原 315 个 PASS 无一退化）。本轮闭环 5 个：`move-eligibility-improved.no`（`[a.len()]` 元素型 `[1]void`，§12 #34）、`test-str-ops.no`（str/char 算术与拼接语义对齐 legacy，#33）、`test-std-unix-fs-os.no`（裸结构体名解析为限定名，`uts = os.uname()`，#32）、`bug12-builtin-slice-to-str.no`（`fs.read-file` 内建 + `[]byte`→`str` 重新解释，#35）、`element-assign-clone.no`（`vec[i] = s` 深克隆，#35）。另修正一处**测量口径**：crypto 系列单测编译+运行需 10–14s，扫掠超时由 8s 提到 60s 后，此前被误判为 HANG 的 6 个 sha256/hmac 测试恢复 PASS（见 §16）。
>
> **关键修正（2026-09-11 第五轮）**：早期 §13.3.1 按**错误症状**直接分类，把"两模式都挂"的预存失败也计进了 MIR 专属 gap（曾报约 38）。本轮对 418 全量做了**legacy-parity 二分**（每个失败测试额外跑 `MOLANG_MIR=0`）：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败）。故 MIR 专属 gap 真实数是 **41**，不是 38；那 92 个属 nolang 通用预存 bug / std·test 缺陷，与 MIR 路径无关，应作为通用 bug 在双路径闭环、不 blocking MIR 门禁。这也印证了"std、tests 都可能不合理"的现场警示（§13.3.5）。

**结论（第三十轮 2026-09-15 更新）**：MIR 专属 codegen gap = **1**（第二十九轮 2 → 本轮 −1；`test-diff-debug` 为**预存**，非本轮引入）；**MIR 专属运行时崩溃 = 0**（**red-line 持续达标**——13 轮曾出现的 3 个 MIR 专属崩溃自 #41 后未再复发）。剩余失败 52 全部是预存/通用 bug（legacy 自身也失败，与 MIR 路径无关）。**下一步优先级**：① 收敛最后一个 MIR_GAP `test-diff-debug`（DP 表全 0 的 `[]str` 元素方法 `compare` 降级路径，探针显示 `with-len`+字面量赋值的 `[]str` 元素方法调用在**两模式**都 segfault，属更底层的预存缺陷，需先修通用缺陷再判定 MIR 责任）→ ② `with-len`/`index dst slot` 的 void 型别族（#54 同族余孽，4+1）→ ③ `str-len receiver i64`（3）→ ④ 语料迁移 8 个真·未标注溢出运算 → ⑤ `net-dial` 非 IP 字面量 host 的 `getaddrinfo` 回落（3）。

---

## 11. 不变式与验证 Oracle

- **Oracle**：`NOLANG_MIR=3` 产生的可执行行为必须与 legacy（`MIR=0/2`）逐输出一致；MIR 只"把内存决策说清楚"，不改语义。
- **结构不变式**：每块恰好一个 `Term`；每 `ValueID` 单次定义；`OpDrop` 恰好一次/owned 值；`OpMove` 源之后无 read。
- **Sanitizer 回路**：对 MIR 产出二进制跑 `ASan`/`LSan` 作为回归门（长期）。

---

## 12. 已闭环的 MIR 专属运行时崩溃修复目录

以下 11 个家族曾在 `MIR=3` 下崩/分歧，现已闭环（`src/mir/hir2mir.go` / `codegen.go` / `mir.go`）：

1. **顶层 let 丢绑定（undef→trap）**：`synthesizeMainForTopLevel` 四类误 `continue` 丢弃（常量折叠 `KPrefix/KIdent`、内联判定改只读 `StructFields/OwnedStructs` 表、调用值 let 无条件内联）。→ `test-fd-newtype`/`slice1`/`test_path_char`/`path.path` 字面量。
2. **`lowerIf` 空 merge 拼接（无限循环）**：记 `contTargets[merge]=enclosingCont`，`ensureReturn` 查表补 `br`。→ `test-number-generic`（pow 二进制幂）。
3. **count-for 循环缺失（无限循环）**：`lowerFor` 忽略 HIR `for` 的 `count` 槽 → 新增 `lowerCountFor`。→ `test-for3`。
4. **option let 不包装**：`let n ?i64 = 42` 存裸 `i64` → `KLet` 处 `EmitOptionWrap`。→ `test-self-write-str`。
5. **`print(<option>)` 取 tag**：改调 `print_option` helper 打内值。→ `test-it-probe`（现输出 `42`）。
6. **bool 打印格式**：`print_bool` 由 `true/false` 改 `1/0` 对齐 legacy。
7. **变参 vec 打包（trap）**：`emitCallBody` 检测"实参多于形参且末形参是 slice" → `buildVecViewFromValues`。→ `test-number-generic`。
8. **包限定方法调用自递归（SIGSEGV）**：删 `resolveCallee` KDot 误写的 `path.path.method` 重写块，`fs.is_file(self.p)` 保持 `recvName.method`。→ `test_path_char`。
9. **`nil` 变体未识别（undef→trap）**：`KIdent "nil"` 走 `EmitOptionWrap(tag=1)`，`KInfix.isVar` 加 `nil` 兄弟 seed `typeHint`。→ `test-arr-at-minimal`/`test-arr`(`.at`)、`test-vec-fl-minimal`/`test-vec-last-minimal`(`.fl`/`.last`)。
10. **MIR 专属崩溃归零**：上述 11 个测试 `MIR=3` 现 rc=0 且输出逐字节一致。
11. **跨模式一致性**：40–47 样本 `MIR=3` vs `MIR=0` 广域对照 0 红线崩溃、0 输出分歧。
12. **`test-arr.no` 溢出默认回归闭环（2026-09-11）**：溢出默认使整数算术返回 `?i64`；在 `a[i] = a[j]+a[k]`（`a:[N]i64`）这类把 `%option` RHS 赋给标量数组元素的站点，`emitIndexStore`（`codegen.go`）此前未解包 option payload，opt-verify 报 `store i64 %lv475, i64* %ep477` 中 `%lv475` 实为 `%option` → 构建失败（MIR=3 回归）。修复：sink 站点当 `valT` 为 `%option` 且目标元素为标量时 `extractvalue %option %valV, 1` 提取 ok payload 再 store（对齐 legacy 解包）。`test-arr.no` 现 `MIR=3` rc=0 且输出逐字节一致；10 个既有通过测试无回归。
13. **`str-clear` 内置闭环（2026-09-11 第二轮）**：`emitBuiltinForward`（`builtin_call.go`）漏 `"str-clear"` case，`str.clear()` 报 `unsupported builtin str.clear`（`test_str_clear.no` 等 cerr）。修复：新增 `case "str-clear"` → `emitBuiltinStrClear`（置 `%str-long` len=0，保留 data/cap，对齐 builtin 注释 "no storage switch"）。`test_str_clear.no` 现 `MIR=3` rc=0 且输出与 legacy 一致。
14. **`emitSetField` option→标量解包（2026-09-11 第二轮）**：`emitSetField` 常规 store 路径与 `?T.field` 分支加同 §12 #12 的解包，覆盖字段写入类 sink 站点（item 1 收口）。无回归。
15. **溢出默认算术/负号 option 包回（2026-09-11 第三轮）**：`emitArith`/`emitNeg`（`codegen.go`）此前只在 sink 站点解包，算术**结果**本身是 `?i64`（`%option`）却直接 `mul %option ...`（非法）→ opt 报 `invalid operand type`/`defined with type %option but expected i64`。修复：若 `lt`（结果类型）为 `%option`，先 `unwrapOptionOperand` 解操作数到标量 payload、按标量元素型别算、再 `insertvalue` 把结果包回 `%option`（tag 0=ok）后 store；并新增 `optionZeroLit` helper。同步在 `emitArith`/`emitNeg` 操作数读取处对 `%option` 操作数做 `extractvalue` 解包（表达式路径）。**本轮解锁 13 个 MIR=3 测试**（x25519 系列、fe-mul 等）；含 `test-fe-mul-simple.no` rc=0 且输出与 legacy 一致；14 个既有通过测试零回归。
16. **`resolveCallee` 顶层绑定值 receiver 路由（2026-09-11 第三轮）**：`resolveCallee` KDot 分支的"模块命名空间"判定只查 `l.locals`，而顶层变量登记在 `l.globals`（带 NoVal 标记），致 `data.zero()`（`data [4]i64` 数组）等**数组 receiver** 方法调用被误判为模块、丢掉 receiver（`arr.zero: needs receiver`）。修复：模块判定同时排除 `l.globals` 成员（local/global 皆视为绑定值，走方法调用路径）。`test_zero.no` 现 rc=0 且输出与 legacy 一致。注意：切片无初始化顶层变量（`v []str`）仍卡在"顶层 main 合成结构性 bug"（§13.3.4），非本修复范围。
17. **顶层容器字面量/变量物化（§13.3.4 闭环，2026-09-11 第四轮）**：`synthesizeMainForTopLevel`（`hir2mir.go`）此前把顶层 `v []i64 = [10,20,30]`/`data []i64`（无初始化）等容器字面量/变量注册为**破模块全局**（`@name = global %vec <固定数组常量>`，类型与初值不符 → LLVM verifier 拒），致其后 `v[0]`/`data.push(10)`/`a[i]=x` 操作数 void 化 → `has_no_slot`+`index_slot`+`receiver-切片` 共 **15** 个失败。修复：(a) 注册循环加 `isUnsafeInlineType` 守卫，切片/数组/option/vec 顶层 `let` 在脚本模式**不注册为全局**；(b) `KLet` 分支对 `foldConstText` 有值且 `isUnsafeInlineType` 的 raw 落到**内联为合成 main 局部**（对齐函数内同名 `let` 已验证可正确 lower 为真实 `%vec`/`%option`）。闭环 `has_no_slot`(7)/`index_slot`(6)/`receiver-切片`(2) 三家族（共 15 个）；committed 基线 244 → 280 PASS（净 +36，含 #18）。
18. **`emitBitwise` option 解包/包回（2026-09-11 第四轮，§13.3.4 伴生修复）**：溢出默认使整数算术返回 `?i64`（%option），但 `emitBitwise`（`codegen.go`）此前对 `lshr/shl/and/or/xor` 直接做裸标量运算、未解包 option 操作数，致 `?i64` 移位量（来自溢出默认）触发 opt-verify `lshr %option %lv, %lv`。修复：镜像 `emitArith` 模式——当 `lt`（Dst）为 `%option` 时先 `unwrapOptionOperand` 解两操作数到标量 payload、按标量算、再 `insertvalue` 包回 `%option`（tag 0）后 store。闭环 `test-vec-assign.no`（输出 `1779033703` 与 legacy 逐字节一致）。
19. **`print` 内联 option payload 路由（2026-09-11 第四轮）**：`emitPrint`（`codegen.go`）对**非扁平**内联 option（`%option_<elem> = { i64 tag, <payload> }`，如 `?str` → `%option_str` 携带 `%str-long` payload）此前 `extractvalue` 取 field 1 后**一律** `call @print_i64`，致 opt-verify `%optplN` 定义 `%str-long` 但期望 `i64`（`print(?str)` 崩）。修复：新增 `codegen.optPayload` 映射（类型声明时填 `%option_<elem>`→payload LLVM 型），内联 option 打印时按 payload 型别路由（`%str-long`→`@print_str`、`double`→`@print_double`、`i64`→`@print_i64`、`i8`→zext+print_i64、`i1`→`@print_bool`；`%vec`/用户结构 payload 暂 `fail` 跳过）。闭环 `option-heap-leak.no`/`test-option-match-basic.no`/`test-option-match-direct.no` 3 个 opt_verify 失败（输出与 legacy 逐字节一致）；280 → **281** PASS，零 MIR 专属回归（crypto 假 HANG 见 §10.1）。
20. **`%txt` receiver 的 `str-len-bytes`（2026-09-11 第五轮）**：`emitBuiltinLen`（`builtin_call.go`）此前仅接受 `%str-long`/`%vec` receiver，对 `%txt`（`%txt = type { [255 x i8], i8 }`，field 1 为 i8 长度字节）报 `unsupported receiver type %txt` → `test-slot-rebind.no` 在 `MOLANG_MIR=3` 硬失败（`MIR=0` 通过）。修复：新增 `%txt` 分支——`getelementptr` 取 field 1（`load i8`）→ `zext i8 to i64` 作为字节长度 store 到结果 slot，对齐 `emitLenCap` 的 `txt.len` 路径与 legacy。`test-slot-rebind.no` 现 `MIR=3` rc=0 可运行（注意该 test 打印指针类值、输出非确定，非逐字节对照候选；属 §13.3.5 的 test 自身缺陷）。

21. **slice 结果元素类型推导（2026-09-12 第六轮）**：`lowerSlice`（`hir2mir.go`）此前用 `l.typeOfNode(n)` 推导切片结果类型，但 `typeOfNode(KSlice)` 对标识符 receiver（声明类型挂在 KLet 而非 KIdent）返回 void → 结果塌缩为裸元素型 `i64` → `b[0]` 的索引结果 void 化、无 alloca slot → `index dst slot`（`tests/arr-slice.no` 在 `MIR=3` 硬失败、`MIR=0` 通过）。修复：当 `typeOfNode(n)` 为 void 时，从已 lower 的 receiver 值（`arrV`）类型回推——固定数组 `[N]Elem` 切片为 `[]Elem`、切片保持自身型别（镜像 `lowerSlice` 既有 `KindArray→[]Elem` 转换）。`arr-slice.no` 现 `MIR=3` rc=0，且输出与 legacy 逐字节一致（9 种切片形态 `a[..]`/`a[2..]`/`a[..3]`/`a[1..4]`/`a[1..4)`/`a(1..4]`/`a(1..4)`/`a[..1)`/`a[1..2)` 全过）。

22. **slice 边界语义（exclusive/inclusive 括号，2026-09-12 第六轮）**：`lowerSlice`（`hir2mir.go`）此前把 `lo`/`hi` 原样传给 `emitSliceOp`（其约定 `[lo,hi)` 即 hi 独占），但忽略了 `(` 独占起点与 `]` 独占终点的语义调整，致 `a(1..4]`/`a(1..4)` 等输出错位（与 legacy 不一致）。修复：依据 range 节点 `FlagLeftInc`/`FlagRightInc` 调整——起点 `(` 独占 → `lo+1`；终点 `]` 独占 → `hi+1`（因 op 取hi独占）；`[`/`)` 保持不变；开边界默认 `0`/`len`。9 种切片形态输出与 legacy 逐字节一致。与 #21 同站闭环 `arr-slice.no`。

23. **`err`/`ok`/`some` 构造器 CALL 路由（2026-09-12 第六轮）**：`err('msg')`/`ok(x)`/`some(x)` 带参是 **KCall**（非裸 KIdent），原走通用 call 路径把 `err` 误解析成 std io.err stderr-writer（`define void @err(%str-long*, i64*)` 返回 `write()` 字节数 i64），caller 再把该 i64 塞进 `%option_str` 的 `%str-long` payload slot → opt-verify 拒（`tests/test-option.no`/`test-option-match.no` 在 `MIR=3` 硬失败、`MIR=0` 通过）。`Builder.EmitOptionWrap` 注释已明示此坑。修复：在 `lowerCall`（`hir2mir.go`）`resolveCallee` 后、`enqueueCallee` 前，对 `callee∈{err,ok,some}` 走 `EmitOptionWrap(optType, tag, payload)`（tag：ok/some=0、err=2；payload 为已 lower 的实参）；新增 helper `variantCtorOptType` 推导 `?T` 结果型——优先用 `l.typeHint`（赋值 LHS 经 `lowerAssignNode` 发布的 `?T`），否则 `?`+payload 型别。`tests/test-option.no`/`test-option-match.no` 现 `MIR=3` rc=0（legacy 的 `err` 不打印 stderr、仅构造 `?str{tag=err,payload}`，与 `EmitOptionWrap` 行为一致）。opt_verify 家族 6 → 闭环 2（本 #23），剩 4 为独立深坑（见 §13.3.1）。

24. **定宽数组 → slice 字段/变量的 `%vec` 归约（2026-09-12 第七轮，mem-safety 崩溃族）**：`c.data = [1,2,3]`（`data []i64`）与 `out = T{ items: [1,2,3] }`（结构体字面量字段）此前把定宽数组**原始字节**直接 store 进 `%vec` slot（`store [N x T] %v, [N x T]* %gp`）——`%vec = {len,cap,data}` 的 len 于是读到数组第 0 个元素，data 读到第 2 个元素（当指针用）→ `.len()` 返回 1、`.[i]` 解引用垃圾 → **SIGSEGV**（`struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak` 在 `MIR=3` 崩、`MIR=0` 通过）。修复：`emitSetField`（常规路径 + `?T.field` 分支）在「RHS 为 `[` 定宽数组 且 目标字段 `TypeRaw` 前缀 `[]`」时归约为 `%vec`。归约实现分两种（`vecViewValue` / `vecOwnedFromArray`）：**借用视图**（len=N, cap=0, data=&arr[0]；cap=0 非拥有，`vec_free` 跳过）仅在不逃逸帧时安全；**堆拥有拷贝**（malloc + memcpy + len=cap=N）用于可能逃逸帧的 sink（结构体字段被返回/move 出）。对可平凡拷贝元素（i8/i1/i64/double）用堆拷贝（对齐 legacy 的拥有语义），其余元素型别暂回退借用视图（深克隆缺失，避免 memcpy 别名嵌套堆 → 双释放）。
25. **定宽数组 → `%vec` 变量的 move 归约（2026-09-12 第七轮）**：`emitMove` 常规路径 `a = x`（`x` 为本地数组字面量 `[N x T]`、`a` 为 `[]T`）此前按目标型 `%vec` 去 `load %vec, [N x T]* src`——把数组前三元素当 `{len,cap,data}`（`x=[1,2,3]; a=x` 读成 len=1）→ 输出错、`get-pair` 双返回等多解引用崩/abort。修复：`dstT=="%vec" && src 型以 "[" 开头` 时走 `vecFromArraySink` 建真实 slice 视图（堆拥有拷贝）。闭环 `double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`（输出与 legacy 逐字节一致）。
26. **`@main` 入口 out-param 接线（2026-09-12 第七轮）**：用户 `main = () (out i64)` 的 out 参数是进程退出码，`_nolang_main` 被 emit 为 `define void @_nolang_main(i64* %p0)`；但 `emitEntry` 无条件发 `call void @_nolang_main()`（**漏传 out 指针**）→ callee 的 `store ..., i64* %p0` 打到 null/垃圾指针 → `-O3` 折成 `unreachable`（SIGTRAP）并删除收尾（全局 free + ret）——`cross-fn-str-return-dfree.no` 的崩溃根因。修复：`emitEntry(main *Function)` 读 `main.ResultParams`，有 out 参数则 `alloca` 槽 → 传址调用 → 返回值截断为 i32 作为 `@main` 退出码（`call void @_nolang_main(i64* %rc)` + `ret i32`）。
27. **结构体字面量字段初始化器的所有权转移（2026-09-12 第七轮）**：`lowerStructLit` 把字段初始化表达式 lower 成临时值再 `OpSetField(res, vv)`；drop pass（`insertDrops`）把该临时值当作 owned local 在其末次使用后插入 `str_free`/`vec_free`——但该值已被 move 进结构体字段（结构体可能逃逸帧）。于是结构体仍指向的堆被提前释放 → `h1.name` 读到 NUL/垃圾（`struct-move-is-moved`/`struct-field-leak`）。修复：`Inst` 加 `MovesArg bool`，`lowerStructLit` 置位；`insertDrops` 在「`OpSetField` 且 `MovesArg` 且 value 在 store 后不再活跃（`!liveOut`）时把 `Args[1]` 计入 `moveSrc`」（豁免其自身 drop，所有权由结构体承担）；`Validate` 里对应的 `moveSrc` 计算同步豁免，否则 leak 检查误报 `[missing-drop]`。

28. **print 家族具名格式串（`print('x={n}')`）降级（2026-09-12 第八轮，interp 家族）**：legacy 只在 **print 家族调用点**做 `{name[:spec]}` 替换（`llvm.callFmt -> shouldInterceptNamedFormat -> callNamedFormat`），其他上下文（`s = 'x={n}'`、结构体字段、用户函数实参）**原样输出花括号**（已用 legacy 实测确认）。MIR 此前用的是粗粒度判据 `Contains("{") && Contains("}")`——把任何含花括号的普通字面量（JSON、正则、模板）都打成 `interp` 致命诊断 → 整模块回退。修复分两步：

    - **判据精确化**（`lowerExpr` 的 `KStrLit`）：只有 `parser.ParseFormatString` 解析成功**且含字段**、**且**当前正在 lower print 家族调用的实参（`lowerer.inPrintArgs`）时才记 `interp` 诊断；其余一律按字面量发射（与 legacy 一致）。
    - **替换实现**（新增 `lowerNamedFormat` / `lowerNamedFormatResult` / `lowerFormatField` / `lookupFormatValue`）：在 `lowerCall` 里 `resolveCallee` 之后、`enqueueCallee` 之前拦截 `print/eprint/printf/eprintf/format/sprintf`（`println` 依 legacy 表**故意不在内**），按段发射——字面段直接写，字段段按值类型调 std `fmt-int`/`fmt-uint`/`fmt-f64`/`fmt-str`（`bool` 走 `fmt-int` 输出 1/0，与 MIR 自身 `print_bool` 及 legacy 函数体内行为一致），`print/eprint` 末尾补一个换行；`format/sprintf` 用新增的合成 `$str_concat`（prelude 的 `_mir_str_concat`）折叠成 str 结果。表达式字段只支持 `ident[index]`（`{hash[i]:02x}`）这一种常见形态（用 `OpIndex` 复用普通下标路径），其余（如 `content.len-bytes()`）拒收并回退——MIR 阶段已无 parser 可用。
    - codegen 侧新增两个只在 MIR 内部使用的合成 callee：`$print_str`/`$eprint_str`/`$print_nl`/`$eprint_nl`（无分隔符、无换行的裸写）与 `$str_concat`；名字以 `$` 开头（非合法 nolang 标识符），永不与真实函数或 builtin 冲突。prelude 补 `define void @eprint_str(...)` 与 `define %str-long @_mir_str_concat(...)`。

29. **切片越界写入不增长 `len`（2026-09-12 第八轮，silently-wrong 修复）**：`padded []byte` 声明后直接 `padded[i] = 0`（std/crypto 各大 hash 构造 padding 缓冲区的标准写法）在 legacy 下会把 `len` 增长到 `max(len, idx+1)`，MIR 只 malloc 了缓冲（`ensureVecBuffer`）并写了字节却**保持 len=0**，于是后续 `.len()`/遍历看到的是空容器 → **SHA-1/HMAC 摘要错但 rc=0**。修复：`emitIndexStore` 对 `%vec` 也执行 `len = max(len, idx+1)`（原先只对 `%str-long` 做），抽出 `emitExtendLen` 共用。

30. **`i8` 窄整型按无符号零扩展（2026-09-12 第八轮，silently-wrong 修复）**：`coerceInt` 对窄→宽一律 `sext`，而 nolang 的 `byte`/`u8` 是**无符号**且是 LLVM `i8` 的绝对主要使用者（MIR 把 byte/u8/i8 全映射成 `i8`），legacy 也是 `zext`。于是 `padded[k] = 0x80` 后参与任何更宽的表达式都会被符号扩展成 `0xFFFF...FF80` 并污染 OR/加法。修复：`coerceInt` 对 `i8` 源改用 `zext`；`emitCast` 同步（`srcRaw == "i8"` 不再算 signed）。

31. **窄结果类型截断已被提升的操作数（2026-09-12 第八轮，silently-wrong 修复）**：`KIndex` 读取 `[]byte`/`[N]byte` 元素时结果提升为 `i64`（与 legacy 一致：按字节寻址缓冲区里 load 出来的 byte 落进 i64 寄存器），但 `KInfix` 的结果类型仍取 HIR 声明的 `byte`，于是 `padded[i] << 24` 又被截断回 1 字节（0），`(a<<24)|(b<<16)|c` 塌缩成只剩 `c`。修复：`lowerExpr` 的 `KInfix` 在「声明结果类型是 byte/u8/i8 但任一侧已 lower 成 `i64`」时把结果类型提升为 `i64`（只在真正的算术/位运算上生效，见 `isArithOrBitwiseOp`）；byte **变量**（`b byte = 255; b << 4` → 240）因两侧都是 `i8` 而保持 8 位语义，与 legacy 一致。

32. **裸结构体类型名解析为限定名（2026-09-12 第九轮）**：`internType`（`mir.go`）对「裸标识符且不在 `StructFields` 里」的类型一律降级为 `KindInt`（原意是 `fd`/`code` 这类标量 newtype）。但 std 结构体是**按限定名**注册的（`os.utsname`），而 builtin 签名/局部声明只知道裸名（`utsname`），于是 `uts = os.uname()` 的槽位被建成 `i64`，后续 `uts.sysname` 发出 `getelementptr inbounds i64, i64* %v66.s, i32 0, i32 0` → opt-verify 拒（`tests/test-std-unix-fs-os.no`）。修复：新增 `uniqueQualifiedStruct`，在降级前把裸名解析成**唯一**的 `X.utsname` 限定键并复用其类型；**有歧义（两个模块同名）时返回空、保持原有标量解释**，不做猜测。`emitBuiltinUname` 写入的 `%os_utsname` 与槽位类型从此一致。

33. **字符串算术/拼接的 legacy 语义对齐（2026-09-12 第九轮，silently-wrong 修复）**：nolang 里 `'...'` 是 StringLiteral、`"..."` 是 CharLiteral，且 **`*` 与 `+/-` 对字面量的判定不同**（legacy `Generator.isStringExpr`）：
    - `*` 只看左侧，StringLiteral 恒为字符串 → `'x' * 5` 是 **repeat**（`"xxxxx"`），不是 `120*5`；
    - `+`/`-` 中，**单字符** StringLiteral 在与**非字符串**操作数配对时才是字节 → `'A' + 1` = `66`；但 `'a' + 'b'` 两侧都是字符串 → 拼接成 `"ab"`；
    - `str <op> char` 要把 char 渲染成**单字符**字符串，不是十进制码（`hi - "B"` → `helloB`，不是 `hello66`）。
    MIR 此前三者全错：结果类型取 HIR 推断值，导致 `sub i64 %str-long, ...`（opt-verify 拒）或把字节按十进制拼接。修复：`lowerExpr` 的 `KInfix` 先按 `srcOp` 判定（`+`/`-` 才允许单字符字面量折叠成字节，且仅当对侧非字符串），再对 `str <op> char` 把 CharLiteral 具体化成单字符 str 常量，最后把「任一侧是 str 且运算符属于 `+ - *`」的结果类型定为 `str`（其余运算符保持旧行为，避免把类型错误静默变成拼接）。新增 `singleCharStrByte` / `charLitCode` 两个 helper。修复后 7 项探针（`helloB`/`Bhello`/`66`/`66`/`xxxxx`/`ababab`/`ab`）与 legacy **逐字节一致**。

34. **数组字面量元素类型（2026-09-12 第九轮）**：`lowerArrayElems` 用 `typeOfNode(首元素)` 定元素型；首元素是**方法调用**时该推断落到 void → 元素型变 `void` → `alloca [1 x void]`（opt-verify 拒，`tests/mem-safety/move-eligibility-improved.no` 的 `a = [a.len()]`）。修复：推断为 void 时**先 lower 首元素**并用其实际值类型（`valueTypeOf`），且该值不重复 lower（避免副作用重放）；仍推断不出才回落 `i64`。

35. **`fs.read-file` + `[]byte`→`str` 重新解释 + 元素赋值深克隆 + option 索引解包（2026-09-12 第九轮）**：
    - **`fs.read-file` builtin**：新增 `emitBuiltinReadFile`（`builtin_call.go`）：`open` → `lseek(fd,0,SEEK_END)` 测长 → `malloc` → `read` → `close`，构造 `%vec {len,cap,data}`（data 是 `ptrtoint` 后的 i64）。失败（open/lseek/read 任一）一律给**空切片**而非负长度（负长度会被当无符号巨数 → 越界读）。注意：`open` 必须复用 fs.open 家族已有的 `declare i32 @open(i8*, i32, i32)` 签名，变参形式会触发 `invalid redefinition of function 'open'`。
    - **`data str = fs.read-file(...)`**：新增 `OpStrFromVec`（slice→str 重新解释，仅 `inttoptr` data 字段，与 legacy 同样**不拷贝**）。因为结果**别名**源缓冲，`insertDrops` 与 `Validate` 两处的 `moveSrc` 都要把源计入（同 `OpOptionWrap`），否则源 vec 与 str 各 free 一次 → `trace/BPT trap`。
    - **`s[0] = a` 深克隆**：`emitIndexStore` 对拥有型元素（`%str-long`）在 store 前 `@str_clone`——此前只 free 旧元素、新值仍是**浅拷贝共享缓冲**，`a` 重新赋值后 `s[0]` 悬垂且最终双释放。`tests/mem-safety/element-assign-clone.no` 现 rc=0 且与 legacy 一致。（`%vec` 元素仍共享：缺 vec 克隆 helper。）
    - **option 索引解包**：`coerceIndex` 对 `%option`/`%option_*` 索引先 `extractvalue ..., 1` 取 payload——`a[res]`（`res ?i64`）否则 opt 拒 `defined with type '%option' but expected 'i64'`。

36. **顶层未初始化模块全局沦为未定义 `external constant`（链接失败，2026-09-12 第十轮）**：`emitGlobals`（`codegen.go`）此前对 `g.ConstText == ""` 的顶层全局一律发 `@name = external constant <T>`——一个**无定义的外部声明**，在 `NOLANG_MIR=2` 下靠回退掩盖，但 `NOLANG_MIR=3` 关闭回退后链接期报 `Undefined symbols: _ga-priv / _gb-priv`（`tests/test-x25519-keypair-diff.no`：顶层 `ga-priv [32]byte` 等 5 个未初始化定宽数组在显式 `fn main` 外声明、在 `main` 内被写入）。根因：显式 `fn main` 程序里顶层 `let` 是合法模块全局（注册进 `l.globals`），其引用经 `lowerGlobalRef` 物化为 `GlobalDecl`，但无初始值 → `ConstText` 空 → 落到 `external constant` 分支。修复：把该分支改为发射**已定义的、可变的** `@name = private global <T> zeroinitializer`（nolang 顶层变量零初始化且可变，与同函数内同名 `let` 已验证的 `%vec`/`%option` 物化一致），删掉"verifier 拒→回退"的旧兜底。`test-x25519-keypair-diff.no` 现 `MIR=3` rc=0；**注意该 test 的 MIR 输出比 legacy 正确**——legacy 对顶层定宽数组全局经 `x25519` 函数读写存在既有 bug（打印 `FAIL: ECDH shared secrets mismatch`，而 ECDH 共享密钥相等是数学不变量），MIR 现给出匹配的正确共享密钥（与第八轮 6 个 interp 测试"MIR 对、legacy 错"同一类：顶层全局物化修好即正确）。改动仅影响"无 ConstText 的顶层全局"发射形态（extern→defined），对 320 个既有通过测试零回归（此前该分支要么产生未定义符号令链接失败、要么对应全局本就未被引用为死代码，二态皆不受损）。

37. **`net.net-icmp-open` builtin（2026-09-12 第十一·续轮）**：`emitBuiltinForward`（`builtin_call.go`）漏 `"net-icmp-open"` case，`tests/tmp-icmp-test.no` 报 `unsupported builtin net.net-icmp-open`（`MIR=3` 构建失败、`MIR=0` 通过）。`net-icmp-open` 是创建 ICMP socket 的 FFI，legacy 直接内联发射 `call i32 @socket(AF_INET, sockType, IPPROTO_ICMP)`（无 C shim）。修复：新增 `emitBuiltinNetIcmpOpen`——`declare i32 @socket(i32,i32,i32)` + 按 `runtime.GOOS` 选 sockType（macOS `SOCK_DGRAM=2` 非特权 / Linux `SOCK_RAW=3` 需 `CAP_NET_RAW`），`sext i32→i64` 存入结果槽。`tmp-icmp-test.no` 现 `MIR=3` **构建 + 运行均通过**，打印真实 fd（macOS 上 `SOCK_DGRAM` ICMP 非特权，故非 root 即得有效 fd），与 legacy 输出逐字节一致（同值 `5`）。

38. **`net.conn` 内置族 `net-dial` / `net-send` / `net-recv`（2026-09-12 第十一·续轮）**：`emitBuiltinForward` 漏 `net-dial`/`net-send`/`net-recv` case，`tests/test-net-client.no` 报 `unsupported builtin net-dial`（`MIR=3` 构建失败、`MIR=0` 通过；`close` 已是 `CLibCall` 通用路径，无需补）。修复：新增三个 emitter，均镜像 legacy（`build/llvm/call_stdlib.go`）：
    - `emitBuiltinNetDial`：`socket(AF_INET, SOCK_STREAM, 0)` + `inet_pton(host)` + `connect`，按 `runtime.GOOS` 布局 `sockaddr_in`（macOS `sin_len@0=16, sin_family@1` / Linux `sin_family@0`），`sin_port = htons(port)`，返回 `(socket ok && connect ok) ? socket : -1`。hostname 的 `getaddrinfo` DNS 回退暂未移植（IP 字面量主路径已正确；非 IP 主机暂返回 -1）。
    - `emitBuiltinNetSend`：`send(i32 fd, i8* data, i64 n, i32 0)`，data 取 str 的 data 指针（显式长度，无需 NUL 终止）。
    - `emitBuiltinNetRecv`：`recv(i32 fd, i8* buf, i64 n, i32 0)`，buf 取 str 的 data 指针（原地写入；不更新 Nolang 串长，与 legacy 对定长缓冲的 recv 行为一致）。
    - 验证：新建 `tests/mini_net.no`（dial→send 'hello'→recv→close），`MIR=3` 输出 `dial fd=5, written=5, read-n=5, done`，与 legacy **逐字节一致**（证明三个内置 IR 正确）。`test-net-client.no` 现 `MIR=3` **构建通过**（该 test 自身标 `vet-only`、不执行，故运行需 live `nc -l 8080` 属环境外）。注意 `test-net-client` 顶层程序里 socket 偶发占用 `fd=1`（stdio 关闭后最低可用 fd）导致 `print` 输出混入 socket，是顶层程序 stdio-fd 处理的既有现象（legacy 同获 `fd=0`），非本修复引入的回归——`mini_net` 在 stdio 完好时 socket 取 `fd=5`、收发均正确可证。

39. **`await`/`run` coroutine 完整 task 运行时（2026-09-12 第十二轮，闭环最后一个 MIR 专属 gap `test-async.no`）**：MIR 此前只有 `OpRun`/`OpAwait` opcode 与 `KRun`/`KAwait` 脚手架（lowering 把 `-async` 后缀 call 转 `OpRun`、把 `awy <expr>` 转 `OpAwait`），但 codegen 未发射运行时 → `MIR=3` 对 `test-async` 崩。修复：在 `src/mir/codegen.go` **逐字移植** legacy `build/llvm` 协作式调度器契约（用户明确选择"完整 task 运行时"而非 MVP 同步 thunk，以忠实对齐字节）：
    - **`%task` 类型**：`{ void (i8*)* resume_fn, i64 data, i1 done, i1 cancelled }`（24 字节）；`data` 指向 args struct = `{ i8* result_ptr, i8* arg0_ptr, ... }`。前导声明写在 `%option` 之后、`declare i8* @malloc` 之前。
    - **`emitAsyncScheduler`**：把 legacy `decl.go:889-968` 的 5 个 `nolang_async_*` 函数 + 全局（`nolang_async_enqueue`/`_yield`/`_wait`/`_done`/`_run`）与 `@malloc`/`@free` 声明逐字写入 `extraGlobals`/`extDeclOrder`（尾部追加机制，不破坏既有顺序）。
    - **`OpRun`（`emitAsyncRun`）**：堆建 `%task`（resume_fn = 生成的 `async_wrapper.N`，data = args struct，done/cancelled = false）→ `nolang_async_enqueue` 入队 → 返回不透明 i8* 句柄（`ptrtoint`→i64 存 Dst）。`inst.Sym` 为空表示 `run <handle-var>`，仅转发既有句柄。
    - **`OpAwait`（`emitAsyncAwait`）**：检查 done；未完成则同步 `call resume_fn(task)` 驱动到完成（顶层平坦 await 树无嵌套 yield，同步驱动即与 legacy 事件循环字节一致）；再从 args struct field 0 读 `result_ptr` 取结果；最后 free 三个容器（result/args/task，内部拥有数据已归属结果槽）。
    - **每调用点 wrapper `async_wrapper.N`**（`asyncWrapperFor`）：按 MIR 调用 ABI 调目标——**标量参数（`i64`/`i1`/`double`/`i8`）按值传递**（先 `load` 再按值传），聚合/拥有类型按指针传递（关键 ABI 修复：初版把标量按指针传，致 `awy f` 读出垃圾如 `8763099328` 而非 `50`）；wrapper 内带 done/cancelled 守卫。
    - **验证**：`NOLANG_MIR=3 ./bin/no run tests/test-async.no` rc=0 输出 **8 行** `42/42/1/-1/0/60/60/50`，覆盖 `compute-async`/`add-async`/`classify-async` + serial/multi-params/conditional/concurrent/lazy-future/await-future 七子用例。**零回归证明**：`git stash` 暂存改动 → 构建 baseline（无 async 代码）→ 对 curated 集合跑 `MIR=3` vs legacy，`BAD=6` 画像与改动版**完全一致**（test-arr OK、test-match OK、test-option/number/vec DIFF、test-basic/str/map MIR3_FAIL），均为既有 MIR gap，与 async 无关 ⇒ 改动零回归。注意 legacy（=0）对 `test-await-future`（`awy f` 其中 `f` 是 future 变量）**漏最后一行 `50`**（既有 bug，见 §16），故 MIR 输出比 legacy 正确、不计入 MATCH。

40. **async 取消/让出内置 `async-cancel` / `async-cancelled` / `async-yield`（2026-09-12 第十二·续轮，收尾完整运行时契约）**：`src/builtin/async.go` 与 `src/std/async.no` 已声明这三个 `#{buildin}`（D10 协程取消原语），但 MIR 的 `emitBuiltinForward` 没有任何 case → `MIR=3` 报 `unsupported builtin async-cancel`。修复（`src/mir/builtin_call.go` 新增三个 emitter + dispatch case，逐条镜像 legacy `build/llvm/call_stdlib.go:2660-2722`）：
    - **`emitBuiltinAsyncCancel(inst)`**：`h` 是 MIR 的不透明 i64 句柄（`OpRun` 把堆 `%task` 的 i8* `ptrtoint` 成 i64），故 `inttoptr i64 → %task*` → `getelementptr %task, field 3` → `store i1 true`。（legacy 里句柄是 i8*，直接 bitcast；此处多一步 inttoptr。）置位后，生成的 `async_wrapper.N` 入口守衛读到 cancelled 即 `store done=true` 跳过目标函数，于是随后的 `awy h` 返回 **zero value**——正是 `std/async.no` 文档的契约。
    - **`emitBuiltinAsyncCancelled(inst)`**：镜像 legacy——`load @nolang_current_task` → null 判定分支 → 非 null 则读 `%task` field 3 → `phi i1`（**绝不 deref null**）→ `coerce("i1")` 存入结果槽（i1 或 i64 均可）。当前无任务时返回 false，与 legacy 注释"若当前不在异步任务中...安全返回 false"一致。
    - **`emitBuiltinAsyncYield(inst)`**：`call void @nolang_async_yield()`（退化路径）。legacy 在 `-async` 函数内作为顶层语句时由 `coro.go` 改写为真协程挂起点；MIR 无协程状态变换，恒走退化路径（重入队后返回）。
    - **`emitAsyncAwait` 补 `@nolang_current_task` 发布**（`codegen.go`）：同步驱动 wrapper 期间 `store i8* %task → @nolang_current_task`，返回后置回 null，使目标函数内的 `async-cancelled()` 读到正确的 `%task.cancelled`（对齐 legacy 事件循环调度前写 current_task 的行为）。单任务同步驱动，单对 store/restore 足够。
    - **验证**：新建三个测试并 MIR=3 逐字节对照 legacy——`tests/tmp-async-cancel.no`（cancel→`awy` 得 `0`；未 cancel→`42`；非任务上下文 `async-cancelled()`→`0`；退化 `async-yield()`→无副作用）输出 `0/42/0/7` 与 legacy **逐字节一致**；`tests/tmp-async-coop.no`（`-async` 目标内 `async-cancelled()` 自检）输出 `7/0` 与 legacy **逐字节一致**。`tests/tmp-async-yield.no`（`-async` 目标内 `async-yield()` 后继续）MIR=3 输出 `6/9` **正确**，而 **legacy（=0）SIGSEGV**（`coro.go` 协程挂起路径对 `run`+`awy` 驱动的 `-async` 函数崩溃）→ 又一个 "MIR 正确 / legacy 错误" 案例（见 §16）。**零回归**：curated 集合 `BAD=6` 画像与改动前**完全一致**，`test-async.no` 仍 8 行。

41. **C 家族 3 个 red-line 运行时崩溃全闭环 + E 家族 2 个连带闭环（2026-09-12 第十四轮）**：第十三轮全量重扫暴露 3 个 `MIR` 专属运行时崩溃（legacy 过、MIR=3 崩）与 2 个 `str-len receiver i64` 编译错误。逐条根因与修法：
    - **① `tests/test-slot-rebind-unsafe.no` SIGSEGV——`run` 的异步性是"语法级"而非"命名级"**。`lowerCall`（`hir2mir.go`）原先仅凭 `strings.HasSuffix(callee, "-async")` 判定异步：callee 名不含 `-async` 后缀的 `run double(x)` 被降级成**普通 eager `OpCall`**，于是 `awy h` 拿到的 `h` 是 `double` 的**返回值 14**（i64）而非任务句柄，`emitAsyncAwait` 把 `14` `inttoptr` 成 `%task*` → SIGSEGV。而 legacy 的 `generateRunExpression`（`build/llvm/expr.go:8226`）对 `run <CallExpression>` **无条件**走 `prepareAsyncCall`——`run` 本身就是异步标记，`-async` 后缀只决定**裸调用**是否产生 future。修复：新增 `lowerer.forceRunCall`（记录 `run`/`awy` 直接子调用节点的 HIR id），`lowerCall` 在 `l.forceRunCall != hir.NoID && n.Id == l.forceRunCall` 时同样发射 `OpRun`。**按节点 id 精确匹配**，故 `run f(g())` 里的 `g()` 仍是普通 eager 调用。`lowerAsyncAwait` 对直接调用操作数同样处理（镜像 legacy `generateAwaitForCoro` Case 1）。
    - **② `tests/mem-safety/async-shared-race.no` SIGSEGV——异步实参缓冲区按"实参值类型"而非"形参类型"分配**。`emitAsyncRun` 用 `c.loadVal(av)` 得到的**值类型**建堆缓冲：`shared []i64 = [1,2,3]` 的实参值类型是定宽数组 `[3 x i64]`，而形参是 `[]i64`（`%vec`）；`async_wrapper.N` 按**形参类型** `%vec*` 重解释该缓冲，于是 callee 把元素字节 `{1,2,3}` 当成 `%vec{len,cap,data}` 头，`v[0]=999` 写向 `data=3` 这个假指针 → SIGSEGV。修复：新增 `coerceAsyncArg(av, plt)`（`codegen.go`），镜像 `emitCallBody` 的实参强制——`[N x T] → %vec` 走 `vecFromArraySink`（可平凡拷贝元素用堆拥有副本，否则 `cap=0` 借用视图）、`%str-long → %vec` 走字节视图；缓冲一律按**形参类型** `argTypes[i]` 建。**副产（同为 #41）**：`awy` 侧的任务**结果类型**此前无从得知（`lowerAsyncAwait` 只对"直接调用"操作数能从 callee 签名推），`awy <句柄变量>` 一律回落 `i64` → `print(<str 结果>)` 打出**字符串长度**（`5` 而非 `hello`）。修复：新增 `lowerer.asyncResTypes map[ValueID]TypeID`，在 OpRun 处按 callee 签名登记，并沿标量 let 拷贝（`OpMove`）传播，`lowerAsyncAwait` 优先查它。
    - **③ `tests/test-embed.no` trace-BPT——MIR 完全没有 `#{embed}` 支持**。`#{embed='file'}` 的字节由 `hir.Package.Embeds` 承载（`pkg.EmbedDataOf(id)`），legacy 在 `build/llvm/generator.go:2322` 把顶层 let 物化成"私有常量字节数组 + 指向它的 `%vec` 全局"。MIR 侧**零引用**（`src/mir/` 无任何 `Embed` 字样），故 `DATA []byte` 退化为空切片（`embed len: 0`），`DATA[0]` 越界 → trap。修复：`lowerEmbedBinding`（`hir2mir.go`）在 KLet 上发现 `EmbedDataOf(id)` 非空时，物化 `%vec { N, N, ptrtoint([N x i8]* @.embed.<name> to i64) }` 全局，字节经新增的 `GlobalDecl.EmbedBytes` 传给 codegen；`emitGlobals` 在 `ConstText` 分支前先发 `@.embed.<name> = private constant [N x i8] c"..."`。（**目录型 `#{embed=dir}` 仍未实现**——仅 `src/std/embed.no` 的文档提及，无测试覆盖。）
    - **④ 连带闭环 E 家族 2 个**：`mem-safety/async-str-result.no` 与 `async-str-stress.no` 报的 `builtin str-len receiver i64: unsupported receiver` 根因即 ②的副产——`s1 = awy task1` 取回 `str` 结果时默认成 `i64`，`print(s1)` 遂以 `i64` 接收者调 `str-len`。同一修复即闭环。
    - **⑤ B 家族推进（未闭环）**：`tests/test-opt-struct-field.no` 的 `unknown callee port.to-str` 根因是 `?T.field` **字段路径**用 `?conn` 去查 `StructFields`（表里只登记了 `conn`）→ `p = opt.path` 未绑定 → 之后 `port` 被当作**未绑定名**，`resolveCallee` 的 KDot 模块命名空间分支把 `port.to-str()` 写成 `port.to-str`。修复：字段路径与 `resolveCallee` 一致地 `strings.TrimPrefix(recvRaw, "?")`（`emitGetField` 本就实现了 option peel，故只需修查表键）。该测试遂由 `unknown callee` 推进到 `opt-verify`：`%conn` vs `%tls_server_conn` 结构体**命名歧义**（`structKeyOf` 的 `HasSuffix(k, "."+raw)` 后缀匹配在多模块合并时会把用户 `conn` 与 std 的 `tls.server-conn` 混淆），仍为 gap。
    - **验证**：**第十四轮全量重扫**（`tests/**/*.no`=421，`./bin/no`）`MATCH 240 → 248`、`MIR_GAP 21 → 17`、`HANG 4 → 1`、**MIR 专属运行时崩溃 3 → 0（red-line 达标）**。三个原崩溃测试现均 rc=0 且与 legacy **逐字节一致**（`7 14` / `999 42 hello hello done` / `embed len: 5 len OK first byte OK`）；`test-embed`、`async-str-result`、`async-str-stress`、`async-shared-race`、`test-slot-rebind-unsafe` 共 5 个转入 MATCH。**`test-diff-debug` 由 LEGACY_FAIL 转为 MIR_GAP，非本轮引入**：第十三轮前的 `MIR_GAP=22` 全量扫描已收录它，其 legacy `MIR=0` rc 存在 flaky（在 1/0 间抖动）；本轮的 `unsupported construct (string interpolation)` 诊断位于 KStrLit 内插路径，与本轮改动无关。**零回归**：opt-field `?` 剥离改动前后两次全量扫描画像完全一致（248/58/17/97/1）。

42. **【目录 #63】`emitCallBody` 变参启发式把"显式写出的具名出参实参"误判为变参展开（2026-09-15 第三十一轮，opt-verify 整模块失败 / 静默污染切片）**：
    - **触发形状**（最小复现 `/tmp/rep7.no`、`/tmp/rep8.no`）：被调函数**非变参**且有具名出参，且其**最后一个入参是切片**，且调用点**显式把出参实参写在尾部**——例如 `lcs = (a []str, b []str) (ops []diff-op)` 以 `lcs(a, b, ops)` 调用。
    - **根因**：`emitCallBody`（`codegen.go`）的变参判定是数数——`len(inst.Args) > len(inParams)` 且末入参是 `KindSlice` 就认为有展开。但 `inParams` 已剔除出参，而调用点可以显式写出出参实参，于是 `3 > 2` 成立、末入参 `b []str` 又是切片 → 误判。误判后 `buildVecViewFromValues` 把 `b`（和 `ops`）打进 `%cav` 假 vec；此刻**刚 lower 出来的切片字面量 `['a','b','c']` 仍是定长数组**（`lowerSlice` 的 `KindArray → []elem` 转换发生在其被 store 进 `%vec` 槽位时），于是 IR 里出现 `store [3 x %str-long] %lv39, ptr %cav42` 而 `%lv39` 定义为 `%vec` → LLVM 校验器整模块拒绝（`opt: error: '%lv39' defined with type '%vec' but expected '[3 x %str-long]'`）。**"整模块失败"其实是幸运情形**：元素型别一旦偶然吻合，IR 会被接受，切片被静默污染而不报错（silently-wrong 类）。
    - **修法**：新增 `mir.Function.Variadic`（`hir2mir.go` 在 `NewFunc` 之后由 `n.Has(hir.FlagVariadic)` 置位），让 codegen 不必靠数数猜。`emitCallBody` 改为：**仅当 `!cf.Variadic` 且 `len(inst.Args) == len(inParams)+len(outParams)`** 时，才把尾部 `len(outParams)` 个实参认定为具名出参实参并从变参计数中扣除（`effArgs`），且变参收集循环的上界也改用 `effArgs`，以免把出参实参卷进 `%vec`。
    - **⚠️ 第一版修法踩的坑（务必记住）**：先写的是"无条件扣减"（不看 `cf.Variadic`）。这会让 `number.max(10, 20)` 也中招——它是「1 个展开入参 + 1 个出参，恰好 2 个实参」，扣减后 `effArgs = 1`，`1 > 1` 不成立 → 变参展开不再发生 → **`tests/test-number-generic.no` 与 `tests/test-number.no` 双双由 MATCH 回归 trace/BPT**（紧随其后的全量扫描 gap 0→3 立刻抓到）。**计数在此时本质歧义，必须用 `FlagVariadic` 这类语义标记消歧，不能靠算术**。加 `cf.Variadic` 判据后两个方向都成立：`rep7`/`rep8` 输出与 legacy 逐字节一致（`2 1 1 0 1 1 1 0 1 1 1 0 0 0 0 0`），`test-number-generic`/`test-number` 回到 `m2=0 m3=0`；再跑全量扫描 gap 3→0、MATCH 365→367。
    - **为什么它不在任何轮次的 gap 数里**：语料中恰好没有"显式写出具名出参实参 + 末入参是切片"的形状，所以它既不表现为 gap 也不表现为 DIVERGE——只有从 `test-diff-debug.no` 的深挖中做最小复现时才被逼出来。**方法论教训：沿着一个失败测试往下挖到最小复现，常会挖出一个与该测试无关、但比它更普遍的真 bug（本轮 #63 即如此），而只盯 gap 计数会永远看不到它；反之，任何"看起来只是改判据"的小改动都必须立刻全量重扫兜底。**

67. **【目录 #67】`collectVarTypesFromBody` 把「已声明局部的再赋值」误当「遮蔽全局」，删掉了它的型别（2026-09-15 第三十七轮，两模式同失败）**：
    - **现象**：`b [3]i64 = [10, 20, 30]` 之后写 `b = b.clone()`，`tests/tmp-arr-half1.no` 在 **两模式**都失败——legacy 报 `opt: use of undefined value '@b.clone'`（调用点停在下线前的 `@b.clone`，即 dot 表达式根本没被重写），MIR=3 报 `unknown callee []t.clone`。而 `c = b.clone()`（绑定到**新名**）两模式都正常，这是唯一线索。
    - **根因**：`build/transpiler.go` 的 `collectVarTypesFromBody` 对「`Type == nil` 且 `inferTypeFromExpr(Value)` 推断不出」的 `LetStatement` 执行 `delete(varTypes, name)`，注释称是为了清掉「从外层/全局继承的同名陈旧条目」。但 `b = b.clone()` 是**同一函数体内已声明局部的再赋值**，不是陈旧全局：删除后 `resolveMethodCall` 里 `varTypes[recvIdent.Value]` 查不到 `b` 的型别 → 直接 `return false` → 泛型模板 `[n]t.clone` 从未被 `cloneAndSubstitute` 单态化 → 调用点留下未解析的 `b.clone`。**这是前端（transpiler）缺陷，不是 MIR 缺陷**——正因如此修它对两个后端同时生效。
    - **修法**：新增 `collectVarTypesFromBodyIn(body, varTypes, declared)`，用 `declared map[string]bool` 记录**在本体内**绑定过型别的名字（显式 `Type` 与可推断初值都记）；只有当名字**不在** `declared` 里时才执行原来的 delete。递归分支（`BlockStatement` / `ForStatement` / `IfExpression` 块）共享同一个 `declared`，故内层块声明的名字也受保护。
    - **为何不会破坏原语义**：名字只被**外层的全局/外层作用域**污染（原 delete 的靶子）时，`declared` 里没有它，行为与修复前完全一致；同名局部在本体内**先声明后赋值**才有 `declared` 命中，此时保留自己的型别才是正确的。
    - **验证**：最小复现（`/tmp/mt/c5.no`）两模式都输出 `10/20/30`；`tests/tmp-arr-half1.no` 由「两模式都失败」→ **MATCH**（9 个子测试全过）。

68. **【目录 #68】print 家族的容器实参未被渲染成字符串（2026-09-15 第三十七轮，MIR 侧 `printableValue`）**：
    - **现象**：`print(v)`（vec/array）与 `print(a[0..2])` 在 MIR=3 报 `print unsupported arg type %vec`；legacy 报 `opt: use of undefined value '@_LB__RB_t.to-str'`——legacy 想调 `[]t.to-str`，但该泛型模板没有被任何真实调用点触发单态化。`tests/test-slice-heavy.no` 两模式都失败。
    - **修法（MIR 侧）**：`hir2mir.go` 新增 `printableValue(v)`：当实参的 MIR 型别 Kind 为 `KindSlice`/`KindArray` 时，用 `toStrCalleeFor(ty)` 按 **`_<N>x<E>.to-str` → `_x<E>.to-str` → `[]<E>.to-str` → `[]t.to-str` → `[n]t.to-str`** 的顺序找已注册符号，然后 `enqueueCallee` + `EmitCallMulti` 包一层 to-str。**顺序不可颠倒**：必须**先试 mangled 单态名**（`_xi64.to-str`）**再试泛型模板名**（`[]t.to-str`）——模板在 HIR 里没有体，调到它就是 undefined callee。（`str` 的 Kind 是 `KindStr`，天然被排除。）
    - **⚠️ 第一版踩的坑（本轮最大教训）**：包裹开关一开始直接复用 `l.inPrintArgs > 0`。**该标志在整棵实参子树里都 > 0**，于是 `print(b.to-str())` 里**被嵌套的 `b.to-str()` 把自己的 receiver `b` 也包了一层 to-str**，源码语义被降级成 `print(b.to-str().to-str())`：`[3]i64` 先渲染成 str、再对 str 走一遍数组版 to-str，输出 `[9, 9, <堆地址>]`（垃圾，且末位每次运行都变），而基线是 `[1, 2, 3]`。**一次引入 13 个 MATCH→DIVERGE**（`tmp-arr-g1/f1/f2/f3/g1/g2/g3/h1/q1/q2/ts1/half2` 等 12 个 + `test-arr-max-print`/`test-vec-str-to-str`）。修法：新增**独立的** `wrapPrintArgs bool`——`lowerCall` 只在**最外层** print 家族调用前置 true，`lowerCallArgs` 进入时先存后清零、`defer` 恢复，因此嵌套调用自己的实参不会被包裹。**`inPrintArgs` 必须保持整棵子树可见**（`KStrLit` 的字符串插值诊断要它），绝不能复用。
    - **发现方式**：全量扫描 MATCH 381→368、DIVERGE 1→15；用 `git worktree add /tmp/no-base af2e494` 编出 HEAD 基线二进制，再用 `/tmp/cmp2.sh`（同一文件跑 base/new × MIR=2/3，同时比 rc 与输出）逐个确认：5 个 DIVERGE 文件在 base 下输出恒为 `[1, 2, 3]`、在 new 下输出每次不同的地址 → 确证是本轮引入而非抖动。
    - **验证**：`tests/test-slice-heavy.no` 由「两模式都失败」→ **MATCH**（`[10, 20, 30]` / `[30, 40, 50, 0]` / `[2, 3, 4]` 逐字节一致）。有趣的是 legacy 也随之转好：`collectVarTypesFromBody` 的修复（#67）让 `v`/`a` 的型别在 legacy 的 varTypes 里也保住了。

69. **【目录 #69】`net-dial`/`net-send`/`net-recv` 的实参序列化 + 补齐 5 个 net 内建（2026-09-15/16 第三十七轮）**：
    - **`cstrOf` 只认 `%str-long`/`%vec`**：`net.net-dial(host, port)` 的 `host` 在 MIR 里是 `%option_str`（`?str`，payload 内联），于是 `net-dial: cannot marshal host as C string`。修法：`cstrOf` 先判 `strings.HasPrefix(lt, "%option")` → `optionPayloadOf` 剥出 field 1，payload 是 `%str-long`/`%vec` 时再 `@str_cstr`。nil option 的 payload 为零值，`inet_pton` 自然失败、builtin 返回 -1，与"host 串非法"同结局。
    - **`dataPtrOf` 只认 `%str-long`/`%vec`**：`net-send(fd, data, n)` 的 `data` 在测试里是 `[5 x i8]`（定宽字节数组）。修法：同样支持 option 载荷（`extractvalue .../2` 取 data 指针），并对 `[N x T]` 走 `getelementptr inbounds [N x T], ... , i64 0, i64 0` + `bitcast T* → i8*`。
    - **补齐 5 个 ForwardFunc**：`net-set-recv-timeout`（`setsockopt(SO_RCVTIMEO)`，`SOL_SOCKET`/`SO_RCVTIMEO` 常量按 `runtime.GOOS` 取值：darwin 65535/4102、linux 1/20）、`net-accept-nb`（`fcntl(F_SETFL, O_NONBLOCK)` + `accept`，`EAGAIN` 时返回 -2）、`net-recv-nb`（`recv(..., MSG_DONTWAIT=64)`）、`net-udp-sendto`（按 `emitBuiltinNetDial` 同款 sockaddr_in 布局 + `sendto`）、`net-udp-recvfrom`（`recvfrom` 带 scratch sockaddr）。
    - **验证**：`tests/test-http-rest.no`、`tests/test-net-http.no` 由「两模式都失败」→ **MATCH**（`test-tls.no`/`test-tls-part3.no`/`test-tls-prf-only.no`/`test-https-server.no` 推进到下一层错误：定宽数组 GEP / `%addopt.final` option 未解包）。

70. **【目录 #70】`std/crypto/aes.no` 的源码笔误 `ek[ek] = aes-key-expand(key)`（2026-09-16 第三十七轮，两模式同失败）**：
    - **现象**：`tests/test-aes-enc.no` 两模式都失败。MIR=3 的 opt-verify 报 `%ep25641 = getelementptr [176 x i8], ptr %v1.s, i64 0, i64 %lv25640` 且 `'%lv25640' defined with type '[176 x i8]' but expected 'i64'`。**初看是 MIR 的定宽数组 GEP 类型推导错（§15 清单第 1 项，据信阻塞 6 个测试），实为 std 源码笔误。**
    - **根因**：`std/crypto/aes.no:439` 写成 `ek[ek] = aes-key-expand(key)` —— 用数组 `ek` 自身当**下标**、把整个 176 字节展开密钥赋给**一个字节元素**。同文件 `aes-128-dec`（第 475 行）写的是 `ek = aes-key-expand(key)`，可确证笔误。MIR 忠实 lower 出 `indexstore [ek, ek, <176B 结果>]`，于是 GEP 的下标是 `[176 x i8]`。
    - **修法**：改为 `ek = aes-key-expand(key)`（与 dec 一致），`make gen no` 重烘焙 std。
    - **验证**：`tests/test-aes-enc.no` 由「两模式都失败」→ **MATCH**（两模式均输出 `6b c1 be e2 2e 40 9f 96 e9 3d 7e 11 73`）。连带 `test-http-rest`/`test-net-http`（走 TLS→AES-GCM）也一并转 MATCH。
    - **教训**：**"定宽数组 GEP 类型有偏差"这类判读必须回到源码核对**——MIR 报的 GEP 错常常只是忠实反映了一个上游（std/前端）的形状错误；`elemAddr` 自身在这条路径上是对的。同理，`coerceIndex` 不该把数组强转 i64 来"修"这个症状，那会把真 bug 藏起来。

71. **【目录 #71】MIR 从未处理 `hir.KRegexLit`：正则字面量在 lowering 期没有对应物（2026-09-16 第三十八轮，两模式同失败）**：
    - **现象**：`tests/test-regex-literal.no` 两模式都失败。MIR=3 报 `unsupported construct (string interpolation)`，**看起来像 §15 #6「字符串插值」的工作项**。
    - **根因**：`/pattern/flags` 在 legacy 里是 **codegen 期**由 AST 改写完成脱糖（`build/llvm/expr.go` 把 `*parser.RegexLiteral` 换成 `regexp-compile('pattern')` 调用）。**MIR 没有 codegen 期 AST**，而 `grep KRegexLit src/mir/*.go` 为空 —— lowering 里一处都没处理，字面量落进默认分支、**不产生任何值**，`re2 = /hello/gi` 因此从不绑定局部 `re2`，后续 `print('re2.pattern = {re2-pattern}')` 报的是 `unresolved format field re2-pattern`。**表象在插值，真因在字面量。**
    - **修法**：`hir2mir.go` 新增 `lowerRegexLit`，把字面量下沉为 `regexp-compile('pattern')` 调用（`enqueueCallee` + `EmitCallMulti(dsts=[regexp], args=[str 常量])`）。callee 名字先试裸名 `regexp-compile`，再回退 `regexp.regexp-compile`。flags（`n.S2`）不参与语义——legacy 同样只是把 flags 挂在节点上不消费，保持逐字节兼容。
    - **验证**：`tests/test-regex-literal.no` 两模式 `rc=0` 且输出逐字节一致（`matched = 0` / `re2.pattern = hello`）。最小复现 `/tmp/mt/i4.no`（`re2 = /hello/gi; p = re2.pattern; print(p)`）修复前**两模式都输出空行**，修复后都输出 `hello` —— 说明 MIR 此前对 `re2.pattern` 是"静默错"（rc=0 但值为空），不只是报错。

72. **【目录 #72】`lowerFormatField` 的 `default:` 把结构体当整数（2026-09-16 第三十八轮，由 #71 暴露）**：
    - **现象**：`print('x = {re2}')`（`re2` 是 `regexp` 结构体）MIR=3 opt-verify 报 `'%lv17' defined with type '%regexp_regexp = type { %str-long, i1, ... }' but expected 'i64'`，位置在 `call void @fmt_int(i64 %lv17, ...)`。
    - **根因**：该函数的 `switch` 只特判了 `str`/`f64`/容器-option-未知，`default:` 分支的隐含前提是"其余 ⇒ 整数"，于是 `%regexp_regexp` 被直接塞进 `fmt-int`。
    - **修法**：`default:` 改成显式的「不可渲染类型」拒绝分支（`l.unsupported(... "format field type "+raw+" not lowered")`），只有 `isIntegerMIRType(raw)`（已含 `bool`）才走 `fmt-int`/`fmt-uint`。
    - **教训**：`switch` 的 `default:` 一旦被当成"剩下的都是 X"，每引入一种新类型都会静默落进来。改这类分支前先把 X 写成显式判据。

73. **【目录 #73】默认后端切换为 MIR-only（2026-09-16 第三十八轮，第二优先级 #8）**：
    - **改动**：`src/build/transpiler.go` 中 `mirMode := os.Getenv("NOLANG_MIR")` 之后加 `if mirMode == "" { mirMode = "3" }` —— 未设置即 MIR-only（无回退）；`NOLANG_MIR=0` 仍显式选 legacy，`1`/`2` 语义不变。
    - **依据**：全量语料默认口径通过 **388/421**，与旧默认口径（`MIR=2`：`MATCH 387 + DIVERGE 1 = 388`）**集合完全一致**；`test/`（40）+`example/`（11）交叉扫描 `GAP=0`/`DIVERGE=0`/`HANG=0`。`MIR_GAP=0` 的直接推论是"没有 legacy 能过而 MIR 不能过的用例"，故切换不改变任何测试成败。
    - **反向证据**：`NOLANG_MIR=0` 下**最简单的 `print(1+2)` 程序编译失败**（`'%addopt.final.N' defined with type '%option' but expected 'i64'`，`store i64 %addopt.final, ptr %fmtval`）。legacy 对"默认溢出模式的整数算术作 print 实参"这条主干路径是坏的，MIR 正确。
    - **配套**：`scripts/mir_sweep_fast.sh` 的汇总补打 CERR/CRASH 名单；致命 lowering 诊断带上具体字段名与类型（`unsupported construct in MIR backend: interp: unresolved format field X`，替换掉原来只报桶名的 `unsupported construct (string interpolation)`）；新增 `firstFatalLowerDiag` 返回诊断本身供上报。

74. **【目录 #74】`transitive_import_test.go` 断言接受 MIR 的符号净化拼写（2026-09-16 第三十八轮）**：
    - **现象**：默认切到 MIR 后 `go test ./build/` 多出 4 个失败（`TestTransitiveImportLLVM`/`...ThreeLevels`/`...Diamond`/`...EntryUsesDeepFn`）：`LLVM IR does not contain @deep-fn`。
    - **复核**：**不是功能回归**。手工搭同一模块图，默认口径与 `NOLANG_MIR=0` 都正确输出 `142`；转储 IR 可见 MIR 产出的是 `@middle_fn`/`@deep_fn`（净化过的拼写），legacy 是 `@middle-fn`。LLVM 无引号标识符只保证 `[-a-zA-Z$._0-9]`，而 MIR 要从任意 Nolang 标识符生成名字，故做净化。两者都是合法 IR，链接与运行一致。
    - **修法**：新增 `irHasFunc(ir, fn)` 辅助函数，同时接受原拼写与净化拼写；`@shared-fn` 的计数同理加 `@shared_fn`。**这些测试的意图是"符号没被模块合并丢掉"（D17），不该被拿来钉死命名风格。**
    - **核对**：与基线 worktree（`/tmp/no-base`，HEAD `af2e494`）对比，`build` 包失败集在 `NOLANG_MIR=0`/`2`/`3` 下完全一致（`TestSliceMethodLenCall{,OnI64,OnStr}`、`TestProgramUsesPrintDetectsLoopAndBlockBodies`、`TestGenerateHIRMatchesGenerate`、`TestUserReadOverridesBuiltin`）——均为既有失败。

77. **【目录 #77】平台变体过滤在脚本模式下失效（2026-09-16 第三十九轮；**口径经第四十轮更正**）**：
    - **现象**（arm64 macOS，脚本无显式 `main`）：`#{mac-amd64} V = 8` 单独 → MIR **打印 8**、legacy 也 **打印 8**；`#{linux-amd64} V = 9` 单独 → 两者都 **`9`**；`#{mac-arm64} V=1` + `#{mac-amd64} V=2` → 两者都 **`2`**；六平台变体各持不同值 → 两者都 **`600`**。修复后 MIR 依次为 **无输出 / 无输出 / `1` / `100`**。带显式 `fn main` 的顶层 let 走注册循环，两个后端**本来就都正确过滤**（实测 `111`/`111`），故缺口只出现在**脚本模式的顶层内联路径**。
    - ⚠️ **第四十轮更正（重要）**：本条目初稿记作"legacy 正确（报 opt 错 / `1` / `100`）、MIR 错"——**实测 legacy 在脚本模式下同样不过滤**（用 `git worktree add <tmp> 47b6cad` 现场编译的 pre-deletion 二进制重测）。所以 `#77` 的真身是**两后端共有的缺口、MIR 单方面修好了它**；判据改用**注解的文档语义**（`docs/docs/lang/syntax.md`），而非"与 legacy 逐字节一致"。直接后果：`tests/test-platform-const.no` 现在 MIR `100` vs legacy `600` **故意分叉**（计入 `legacy-baseline` 的 `DIVERGE`）。详见 §13.3.14 ②。
    - **根因**：`nodeMatchesPlatform`（`src/mir/platform.go`）只在顶层**注册循环**（`hir2mir.go` 的 `KFuncDef`/`KLet` 各一处）被调用，而脚本路径 `synthesizeMainForTopLevel` 没有这个检查 → 被注册循环过滤掉的变体**仍被内联**进合成的 `main`，其错误平台的值覆盖了匹配变体注册的全局。
    - **修法**：在 `synthesizeMainForTopLevel` 的 `for _, id := range pkg.Top` 循环入口（判空后、`switch` 前）补 `if !nodeMatchesPlatform(pkg, id) { continue }`，让所有顶层节点种类共用同一判据（无注解节点返回 `true`，std 预置节点不受影响）。
    - **验证**：新增 `tests/test-platform-const.no`（六平台变体各持不同值）作常驻回归——它对"**过滤是否发生**"有判别力（修复前 MIR `600` → 修复后 `100`），对"两后端是否一致"则没有（legacy 本来就不过滤）。第四十轮补 `src/mir/platform_test.go`（`src/mir` 单测 6→14），覆盖穷尽全表、`fn main` 路径、无注解/带值注解不被误判，并**实测判别力**（把检查短路成 `false &&` 后恰好 2 个脚本模式测试失败）。
    - **为何全量扫描漏掉**：`tests/` 无任何平台注解用例，且 `std/fs.no` 的 `mac-amd64`/`mac-arm64` 常量值相同（`O-CREAT` 512/512、`O-TRUNC` 1024/1024），错选不可观测；`std/process.no` 的注解在**函数**上（走注册循环，本来就对）。⇒ **`MIR_GAP=0` 只说明"语料区分不出"。**

78. **【目录 #78】扫描器新增 CERR/CRASH 名单输出（2026-09-16 第三十九轮）**：`scripts/mir_sweep_fast.sh` 汇总段补打 `CERR`/`CRASH` 逐文件名单，避免排查时重复 9 分钟全量重扫。

80. **【目录 #80】MIR 单测补齐（平台变体）+ 两处依据更正 + 两个两后端共有既有限制（2026-09-16 第四十轮，第三优先级第一项启动）**：
    - **交付**：`src/mir/platform_test.go`，`src/mir` 单测 **6 → 13**（7 个新测试 / 9 个用例，全部以宿主推导期望值，非覆盖平台自动 `t.Skip`）。
    - **依据更正 ×2**：§13.3.13 ①（现象表 legacy 列）与 ⑥（"`mir-baseline`/`legacy-baseline` 在该文件上同值"）—— 两处都错把 legacy 当正确参照。实测 legacy 在脚本模式**不过滤**（`600`）。⇒ **方法论：把"与 X 一致"当判据前先把 X 也测一遍。**
    - **既有限制 A（两后端一致）**：**函数体内**的平台注解不被过滤（后出现者胜出，与宿主无关）。已由 `TestFunctionBodyPlatformVariantIsNotFiltered` 显式钉住；修则测试失败并提示改期望值。
    - **既有限制 B（两后端一致）**：`#{overflow = clamp0 | min | max | saturate}` **策略未生效**，5 种模式输出完全相同（都回绕），注解只起"关掉 `option<int>` 包装"的作用。MIR 侧完全无溢出模式概念（`grep nsw\|OverflowMode src/mir/*.go` 为空）。范围限定：仅验证了**贴在 `let` 上方**的语句级形式；`ExpressionStatement`（if/match 臂体）路径未验证。
    - **oracle 通道**：需要"逐字节对照具体用例"时用 `git worktree add <tmp> 47b6cad` 现场编译 legacy；需要"批量回归"时用冻结的 TSV。这是 §13.3.13 ④ 之外的第二条通道。

82. **【目录 #82】预检不再重复整条后端流水线（2026-09-16 第四十一轮，编译耗时 −50%）**：
    - **现象**：11 行 json 脚本（`p = json-pool {}` + `root, next, ok = p.parse(body, 0)`）`no build` 需 **61.4s**，而 `print('hi')` 只要 1.4s；耗时与 MIR 块数无关（15 块的 `alloc` 14.0s > 71 块的 `parse-num` 4.7s），产出 IR 仅 173KB。
    - **根因（成本面）**：`verifyMIRIRViaOpt` 每次构建跑完整 `opt -O3`（15.1s）**加** `llc`（14s），产物 `m.s` 丢弃；builder 随后对同一份字节再跑一遍。62s ≈ 2 + (15+14) × 2。
    - **改动**：默认只跑 `opt -passes=verify`（~30ms，仍是该阶段存在的理由），跳过 Stage 2 的 `llc`；`NOLANG_MIR_PREFLIGHT=full` 恢复旧行为。**不改变任何构建的成败**（builder 对同字节跑同命令），只改变失败发生在哪一步、报哪条消息。
    - **验证**：`print('hi')` 1.6–2.0s（不变）；11 行 json **62s → 31.8/32.3s**；`NOLANG_MIR_PREFLIGHT=full` 63.5s（忠实复现旧默认）；假 `opt`（`exit 1`）下两种模式都在预检处报 `MIR IR failed LLVM verification`。
    - **反例护栏**：`llc` 在**未优化** IR 上 >100s（684 个 `alloca` ≈ 1.8MB 栈帧）、在 `-O3` 后只要 14s。所以"降级为 verify-only 但继续汇编"会把构建改**慢**——这条是在实施前顺手测出来的。

83. **【目录 #83】MIR 产出 IR 的形态对 LLVM 病态：`sroa` 撕裂 22KB 聚合（2026-09-16 第四十一轮，**未修**）**：
    - **量化**：`opt -O0` 30ms / `-O1` 15.2s / `-O3` 15.1s，输出 173KB → **5.7MB**；逐 pass 隔离后唯一爆点是 **`sroa`**（229KB → **105,192,073 B**，×458，1.26s）；`-unroll-threshold=0`/`-inline-threshold=0` 均无效（后者恶化到 12.4MB）。
    - **形态**：`%json_json_value = { i64, %str-long, double, i1, [16 x i64], [16 x %str-long], i64 }`（≈350B）、`%json_json_pool = { [64 x %json_json_value], i64 }`（≈22KB），而 `alloca [64 x %json_json_value]` 出现 **81 次**（全函数 684 个 `alloca`）。SROA 按常量下标把内联大数组拆成标量 ⇒ 十万量级标量。
    - **修法方向**（属 codegen 语义改动，需单独评估）：大数组字段改堆分配/按引用访问（避免"取一次 `.nodes` 复制 22KB"）；或用属性抑制对这类聚合的 SROA（代价是丢优化）。
    - **回归信号**：11 行 json 程序的 `no build` 耗时（现 32s，修好后应回秒级）。

84. **【目录 #84】两个 json 用例在编译成功后被"解锁"出来，运行期 SIGSEGV（2026-09-16 第四十一轮发现 / 第四十二轮定性，**待修**）**：
    - `mem-safety/test-json-parse-option.no`（117.1s）与 `mem-safety/test-json-nested-match.no`（113.4s）在 #82 之前因编译超时被记为 HANG，从未真正执行过；现在都能编译，但**运行期 rc=1**，stdout 只有 `start`。legacy 侧这两份文件 rc=1（连编译都过不去），所以历史扫描不可能发现。
    - **第四十二轮定性：rc=1 不是编译器或超时造成的，而是段错误。** stderr 为 `Error: signal: segmentation fault`（运行期把信号翻成该诊断并以 1 退出）——这解释了"编译成功却 rc=1"的怪相。
    - **已定位到失败语句**：把 `json.parse('')` 之后的每一步用 `print` 切开（串行、单独编译），输出止于第一个 `print`，即崩溃发生在**第一条语句 `e1 = json.parse('')` 上**，而那是 `json.parse` 里最简单的早返回路径（`result = nil` → `s.len-bytes() == 0` → `result = err('empty input')` → `return`）。所以不是递归下降解析器的问题。
    - **已排除**：用同样的类型（22KB 的 `json-pool`/`json`/`?json`）在自己的模块里复刻"option 包装 + match + `err(<str>)` 早返回"全部**通过**（`/tmp` 探针 p5/p6/p9，2–10s 编译），见 §13.3.16 ④。故触发因素在 `src/std/json.no` 自身，而非该类型形状或 option 机制。
    - **代价**：json 族每个探针的编译仍要 110s+（#83），这是它至今未闭环的原因；`?json` 的载荷是 22KB 值语义结构，任何 json 程序都会拖入这个成本。

85. **【目录 #85】`count-for` 迭代变量上未绑定的方法调用被降级成 `undef`：静默输出 0–775MB 垃圾（2026-09-16 第四十二轮发现 / 第四十三轮加护栏；**底层 lowering 缺陷待修**）**：
    - **症状**：`tests/mem-safety/str-concat-leak.no`（rc=**0**）输出 **0–775MB 二进制垃圾**，且**字节数与行数每次运行都不同**（5 次实测：775904872 / 784287641 / 775904872 / 766050306 / 775904872 字节；1000 / 1118 / 1128 行）。首行字节为 `item` + 12 个 NUL，随后是 `06 00…`（i64=6）两次——即打印的是**结构体原始内存**。
    - **⚠️ 旧结论被推翻**：此前（第三十九轮起）把这条记成"DIVERGE 的**地址差异假阳性**"。**不成立** —— 该文件的输出是 `'item' + i.to-str()` 的**纯文本**，不含任何地址；哈希不稳定是因为**输出本身不确定**。这是一条真实的**静默错误编译**，被"假阳性"这个标签写掉了整整三轮。
    - **2 秒复现**（无需 json，故不必付 #83 的 110s 代价）：
      ```nolang
      i <- [0..3): {
          print('item' + i.to-str())
      }
      ```
      输出 1169 字节二进制垃圾，rc=0。
    - **IR 证据**（`NOLANG_MIR_DUMP_LL=1`，`@_nolang_main.bb5`）：拼接的右操作数是 `undef`，且 `i.to-str()` 的 `call` 指令**根本没有生成**（只有两次未被使用的 `load i64, i64* %v2.s`）：
      ```llvm
      %lv13 = load %str-long, %str-long* %v8.s
      %c12 = call %str-long @str_concat(%str-long %lv13, %str-long undef)
      call void @print_str(%str-long %lv14)
      ```
    - **MIR 证据**（`NOLANG_MIR_DUMP_MIR=1`）：`call dst=0 args=[3]` —— `dst=0` 是"无目标槽位"哨兵，`args=[3]` 指向 `const dst=3:i64`（即 count-for 的**初值常量**，而不是循环变量 `v2`）。消费方随后 `add dst=12:str args=[11 0]` 引用了值 `0` ⇒ `undef`。
    - **判别矩阵**（全部 2s 级探针，`/tmp/fz/`）：
      | 接收者 | 上下文 | 结果 |
      | --- | --- | --- |
      | `x i64 = 42`（let） | `print(x.to-str())` | ✅ `42` |
      | `i`（count-for） | `print(i.to-str())` | ❌ 空 |
      | `i`（count-for） | `s str = i.to-str()` 后 `print(s)` | ✅ `0/1/2` |
      | `i`（count-for） | `g(i.to-str())`（**用户函数**实参） | ✅ `0/1/2` |
      | `x i64 = 42` | `print('item' + x.to-str())` | ✅ `item42` |
      | 普通函数 `f(1)` | `print('item' + f(1))` | ✅ `itemz` |
      | `i`（count-for） | `print('item' + i.to-str())` | ❌ 1169B 垃圾 |
      | `i`（count-for） | 先 `s str = i.to-str()` 再 `print('item' + s)` | ✅ `item0/1/2` |
      ⇒ **触发条件 = count-for 迭代变量作接收者 + 内建调用的实参位置 + 方法调用未先绑定**。三者缺一即正常。
    - **为什么危险**：rc=0 且 stdout "看起来有输出"，比崩溃更难被发现；`str-concat-leak.no` 正是语料里唯一的哨兵，却因被标为"假阳性"而失声。
    - **修法方向**（两条独立缺陷，建议分开修）：
      1. **lowering**：`hir2mir.go` 里"零入参方法调用（接收者类型不同、签名相同的 `X.to-str` 族）在实参位置 + 迭代变量接收者"**根本没有算出结果类型**，于是走了 void 那条路（见下方第四十三轮更正）。
    - **⚠️ 第四十三轮更正（上一条的旧说法有两处错）**：原记"结果参数未绑定到值 id，且**接收者被绑成循环初值常量**"。逐条核对 `NOLANG_MIR_DUMP_MIR` 后：
      - **接收者是绑对的**。`call dst=0 args=[3]` 里的值 `3` 就是循环计数器本身——block 1 分配 `const 1..4`，block 11/12 做 `add dst=15 args=[3 14]` 再 `move dst=0 args=[15 3]`，即 `3` 每次迭代被重新写回 ⇒ **它就是 `i`，不是"循环初值常量"**。原文那句话是误读（把 `args=[3]` 里的值 id 当成了常量池里的初值）。
      - **真正的机制在 `lowerCall` 的 void 分支**（`hir2mir.go:4959–4963`）：
        ```go
        if resTyp == l.voidType {
            // void call: emit without a destination value (statement, not expression)
            l.b.EmitVoid(OpCall, argv, callee)
            return NoVal          // <-- 表达式位置却拿到 NoVal
        }
        ```
        `resTyp` 由 `resultTypeOfCallee(callee)` 得到（4907 行），三档回退（HIR 定义 → 内建表 → LHS 类型提示）都没命中时**才是 void**。本形状里三档全落空 ⇒ 调用被当成"语句型 void 调用"发射，**不分配结果值**，`lowerCall` 返回 `NoVal` 给上层的 `+`。⇒ 上游 `add dst=12 args=[11 0]` 的第二操作数就是 `NoVal`。
      - **为什么同族的其它形状正常**：`s str = i.to-str()` 与 `g(i.to-str())` 都有**外部类型提示**（LHS 声明 / 形参类型），`resTyp` 因此不是 void，走 `EmitCallMulti`（4973 行）→ 结果值进 `inst.Results` → 正常。⇒ **触发条件可以精确表述为"该方法调用的结果类型三档回退全落空"**，而"count-for 迭代变量 + 实参位置"只是能构造出这种落空的一种场景。
      - **已确证但尚未定位的一环**：`NOLANG_MIR_DEBUG=1` 的既有钩子（只对 `i64-to-str`/`number.i64-to-str` 打印）**没有输出**，且 stderr 里**只有 `NoVal` 一条报文、没有"unknown callee"**，说明该 `callee` 名**是能被解析的**、但既不是 `i64-to-str` 也不为 `resultTypeOfCallee` 所知。要把 `callee` 的确切拼写钉死，需要给 MIR 转储补上 `Sym` 字段——**本轮故意没做**：扫描正在使用 `bin/no`，中途重编译会让同一次比对混用两个二进制（见 §13.3.17 ⑧）。
      - **附带发现的第二处隐患**：即使把 `resTyp` 修对，`codegen.go` 的 `i64-to-str` 快路径（4611–4672 行）在 `inst.Results` 为空时**静默 `return nil`**——整个调用凭空消失。也就是说同类"结果值没被登记"的失效有**两条**独立路径，护栏只拦下游消费者那一条。
      2. **checker**：`s str = 'item' + x.to-str()`（接收者**显式** i64）报 `cannot assign non-string value to string variable 's'` —— 即 `str + <调用>` 没有被判成 `str`。两个阶段对同一表达式各有各的错，这正是"未绑定形式"能穿过 checker 直达错误降级的原因。
    - **回归信号**：`no run tests/mem-safety/str-concat-leak.no` 的输出应稳定为 1000 行 `item0`…`item999`（当前是 0–775MB 垃圾）。
    - **⭐ 与 legacy 的对照（现场重建 `47b6cad`，`NOLANG_MIR=0`）—— 根因共有，但失败模式是 MIR 独有的**：

      | 探针 | legacy | MIR |
      | --- | --- | --- |
      | `str-concat-leak.no` | **rc=1** `opt: error: use of undefined value '@i.to.str'` | **rc=0，0–775MB 垃圾** |
      | c1 `print('item' + i.to-str())` | rc=1，同一错误 | rc=0，1169B 垃圾 |
      | c4 `print(i.to-str())` | **rc=1** `codegen error: expression produced empty value in emitArgAsStrLong (expr: *parser.CallExpression)` | **rc=0，空输出** |
      | d1 `x i64 = 42; print(x.to-str())` | rc=0 ✅ `42` | rc=0 ✅ `42` |
      | d5 `s str = 'item' + x.to-str()` | rc=1 `cannot assign non-string value to string variable 's'` | rc=1，同一错误 |
      | e1 count-for 绑定后再 print | rc=0 ✅ `0/1/2` | rc=0 ✅ |
      | f1 `print('item' + x.to-str())` | rc=0 ✅ `item42` | rc=0 ✅ |
      | g2 `g(i.to-str())`（用户函数实参） | rc=0 ✅ `0/1/2` | rc=0 ✅ |

      ⇒ ① **共性根因是前端的**：两个后端都把 `i.to-str()` 解析成"名为 `i` 的类型上的方法"（legacy 直接发出 `@i.to.str` 这个不存在的符号）。这在 legacy 时代就存在，**不是 MIR 引入的**。② **MIR 独有且更危险的是失败模式**：legacy 在 `emitArgAsStrLong` 处**精确检测到"表达式没产生值"并硬报错**，MIR 则在 `loadVal`（`codegen.go:1927–1931`）**静默返回 `("void","undef")`**，消费者照单全收 ⇒ rc=0 + 错输出。**"表达式没产生值"这个条件两个后端都知道，只有一个选择说出来。**
    - **修法（两步，第二步收益最大）**：① 前端：修"未指定类型的接收者（count-for 迭代变量）在实参位置的方法名解析"——属**共有**缺陷，按本项目的经验修它会让 `gap`/失败数先升后降（掩盖的真缺口浮出）；② **MIR：把 `loadVal` 的 `lt=="void"||slot==""` 从"返回 `undef`"改成 `c.fail(...)`**（该文件已有 54 处 `c.fail` 的同款模式），即复刻 legacy 的 `emitArgAsStrLong` 护栏。这一步把一整类"静默错误编译"变成响铃诊断，**是风险最高的缺陷类别里性价比最高的一处改动**；预期副作用是若干当前 rc=0 的用例会变 rc=1（含 `str-concat-leak.no`），需按"先响铃、再逐条修"的顺序接受并重冻结基线。
    - **⭐ 第四十三轮：护栏已落地（第 ② 步），并修正上一轮高估的影响半径。** 上一轮由 `NOLANG_MIR_DEBUG_UNDEF` 得到"**19 个文件 / 55 处命中**，其中 18 个当前 rc=0"，据此推断"可能有一批静默错编译"。**这个推断把两类命中混在一起了** —— 把 55 条诊断按 `llvm=` / `value=` 字段切开：

      | 类别 | 条数 | `value=` | 落点 | 判定 |
      | --- | --- | --- | --- | --- |
      | `llvm="void"` | **51** | `173 / 708 / 604 / …`（真实值 id） | 集中在 `hashmap_str_str_hash`(18) / `hashmap_str_i64_hash`(12) / `hashmap_str_bool_hash`(3) 等 std 函数 | ✅ **合法**：void 值本就没有存储，调用方正是靠 `"void"` 这个返回值跳过它（`emitCall` 的 print 循环 `argT == "void" -> continue`） |
      | `value=0 llvm="i64" slot=""` | **4** | **`0`**（即 `NoVal`，`mir.go:41`） | `_nolang_main`(2) + `des_block`(2) | ❌ **真洞**：消费方在读取**没有任何指令产生过**的操作数 |

      ⇒ 影响半径从"19 个文件"收敛到 **2 个文件**（**仅就 `tests/` 语料而言**，见下方"第三个文件"），而且这两个文件的 `legacy-baseline` 判定**本来就是 rc=1**。**⚠️ 但要看清"哪个 rc"**：两处 rc 不是一回事——`no build` 的 rc 是**编译器**的成败，而 golden 基线记的是 `no run` 的 rc（**先编译再执行，程序自己的退出码**）。手工探针用的是前者，预言机用的是后者，下面是按**预言机口径**（`no run` + stdout 哈希）逐字节对齐后的真表：

      | 文件 | legacy（语义 oracle） | MIR 加护栏前 | MIR 加护栏后 |
      | --- | --- | --- | --- |
      | `tests/mem-safety/str-concat-leak.no` | `1 e3b0c442…`（空 stdout） | `0 eb28fc09…` ← **编译成功**，程序跑了，输出 0–775MB 不确定垃圾，**退出码 0** | `1 e3b0c442…` ✅ **与 oracle 逐字节相同** |
      | `tests/test-std-hash.no` | `1 e3b0c442…`（空 stdout） | `1 399229a5…` ← **编译成功**，程序跑了并**自己有输出**，但程序自身返回 1 | `1 e3b0c442…` ✅ **与 oracle 逐字节相同** |

      ⇒ 两点结论：① **两个文件加护栏后的指纹都与冻结语义 oracle 逐字节相同**（不只是 rc 相等）——护栏把 MIR 拉回 oracle 的判定，**这是"对上 oracle"，不是回归**；② **`test-std-hash.no` 的 rc 在预言机里没变（1 → 1）**，变的只是原因（"编译成功但程序失败"→"编译失败"）与 stdout。而比对脚本对"两边都非零"只记 `BOTH_FAIL`、**不比哈希**，所以这次修复在桶计数上**完全不可见**——它是本轮桶方案的一个盲点，见 §13.3.17 ④。
    - **⭐ 第三个文件在语料之外：`test/std/process.no`（本轮最有说服力的一条证据）**。上一轮的诊断扫描只覆盖 `tests/**/*.no`，所以"2 个文件"**只是 `tests/` 内的答案**。本轮用**护栏前后的两个二进制**对 `test/`（40）+ `example/`（11）做逐文件 `rc` 对照，**恰好多出一个**：

      | 文件 | 护栏前 | 护栏后 | 说明 |
      | --- | --- | --- | --- |
      | `test/std/process.no` | `build rc=0` | `build rc=1` | 唯一一个 rc 变化的文件（`test/`+`example/` 全扫） |

      而它的**真实性质**要用该文件自己文档化的命令 `no test test/std/process.no` 才看得见：

      | | 护栏前 | 护栏后 |
      | --- | --- | --- |
      | `no test test/std/process.no` | 依次跑 `process.new` / `process.shell` / `process.spawn-wait` / `process.parent-pid`，**第 5 个测试崩掉**：`FAIL: test/std/process.no (exit code signal: segmentation fault)` | `FAIL: test/std/process.no` + **`compilation error: … func t_cmd: consumer reads value 0 (NoVal) …`** |

      第 5 个测试正是 `t-cmd`（源文件 46 行 `t-cmd = () {`），**与护栏点名的函数 `t_cmd` 完全一致**。⇒ 这条链完整闭合：**同一个未绑定的值，护栏前表现为"跑着跑着段错误"（没有任何线索指向原因），护栏后变成"编译期点名 `t_cmd` 和缺失的值"**。这是护栏价值最强的例证 —— 它把一次 SIGSEGV 变成了一条可读的诊断。（`no test` 的最终判定两版都是 `FAIL`，即**没有任何测试从通过变成失败**。）
    - **顺带澄清两个易踩的命令语义**：① `no build <file> -o <out>` **不产出二进制**（rc=0 表示"编译器流水线成功"，`-o` 被忽略）——所以 `no build` 的 rc 只能当"编译成败"用，要执行必须用 `no run`（预言机正是 `no run`）；② `test/std/*.no` 是给 `no test` 用的**测试库文件**（无 `main`），不是独立程序。**背景数字（避免把既有失败误算进本次改动）**：`test/` 40 个里 23 个 `no build` 失败，其中 22 个**两版一致**（7 个 `ovfhndld` 陈旧测试源 + 15 个其它既有 MIR gap：`opt-verify`、`cannot marshal arg 0 as C string` 等），只有 `process.no` 是护栏引入；`example/` 11 个里 1 个失败（`mysql-driver/src/mysql.no`），两版一致。
    - **落地实现**（`src/mir/codegen.go` `loadVal`）：把原来合在一起的条件 `lt == "void" || slot == ""` **拆成两支**，只对第二支响铃：
      ```go
      if lt == "void" { return "void", "undef" }            // 合法：void/unit 无存储
      if slot == "" { c.fail(...); return lt, "undef" }     // 静默错误编译 -> 响铃
      ```
      **拆开的理由就是上表的测量数据**：合在一起改会让 51 条合法命中全部误报。报文区分两种成因：`NoVal` 者直指"喂这个操作数的指令没有 Dst"，其余报出 `value` 与 LLVM 类型。
    - **为什么用 `c.fail` 而不是立刻 `return err`**：`c.fail` 只累积、统一在 `EmitLLVM` 出口（`codegen.go:369`）汇总，故**一次构建就把函数内每一处越界点全部列出**（`test-std-hash.no` 的两处一次性报全）——这也是它取代上一轮"先加 env 诊断手工测一遍"流程的原因。`NOLANG_MIR_DEBUG_UNDEF` 保留（供日志 grep 与将来的扫描脚本用）。
    - **验证**：`go build ./...` 通过；`/tmp/fz/c1.no` 由"1169B 垃圾 rc=0"变为 **rc=1 + 精确报文**；5 个正常探针（`d1` let 接收者 / `f1` 显式 i64 接收者 / `g1` 用户函数实参 / `e1` 绑定后 print / `lit` 纯字面量拼接）rc 与输出**逐字节不变**；未受影响的高频命中文件（`test-map-generics.no` 9 处、`test-map.no` 3 处 —— 全是 `llvm="void"` 那类）仍 rc=0。
    - **仍未修（本护栏不解决）**：底层 lowering 缺陷（`i.to-str()` 的 `call` 没有 Dst）依旧在，只是现在会响铃而非静默。修好它之后这两个文件的 rc 应回到 0 且输出正确（`str-concat-leak.no` 应为 1000 行 `item0…item999`）——那一步才是真正的修复，届时需再次重冻结基线。

86. **【目录 #86】golden 测试骨架加固：`rc=124` 语义清理、`-update` 破坏守卫、`UNSTABLE` 分流（2026-09-16 第四十二轮）**：
    - **`rc=124` 的语义问题**：`MIR_GOLDEN_TIMEOUT` 原为 90s，而 124 混装了三种完全不同的东西——真死循环、**慢但有限**的编译、以及 **`-P 8` 自身并发争抢**。第三条是实测的：`tests/test-parse-min.no` 串行 **32.9s / rc=0**，却在冻结基线里记成 **124**（指纹哈希是空输出哈希，说明它根本没跑到运行）。后果不是数字不准，而是**超时的文件无法报告任何行为回归**。
    - **改动**：`MIR_GOLDEN_TIMEOUT` 默认 **300s**、新增 `MIR_GOLDEN_JOBS` 默认 **4**（降并发以减少争用）；注释里写明"oracle 的存在意义是准确，它是手工跑的，不在循环里"。
    - **`-update` 守卫**：`src/build/llvm/` 删除后 `NOLANG_MIR=0` 是硬错，而 `-update` 的默认 `GOLDEN_MIR=0` 会把 `legacy-baseline.tsv`（唯一的**语义**参照）覆盖成"422 个文件全部非零"的垃圾。现直接拒绝（rc=2，不写文件），且**故意不留覆盖开关**：若将来恢复 legacy，应连同这道守卫一起删。`legacy-baseline.tsv` 自此为**只读历史产物**。
    - **`UNSTABLE` 分流**：`MIR_GOLDEN_UNSTABLE` 列出"输出本身不确定"的文件（当前只有 `str-concat-leak.no`，原因指向 #85），哈希不符时记 `UNSTABLE` 而非 `DIVERGE`。理由写在脚本里：一个**永远不可能匹配**的条目会在每次比对时都报警，最终把读者训练成忽略 DIVERGE。`DIVERGE` 必须保持是干净信号。
    - **验证**：见 §13.3.16 ①③。


79. **【目录 #79】删除 legacy 后端 `src/build/llvm/` + 移除 strangler-fig 回退（2026-09-16 第三十九·续轮，第二优先级 #9/#10 收官）**：
    - **规模实测**：48 文件 / 50,006 行 / 2.0MB = 15 生产文件（41,806 行）+ 33 测试文件（8,200 行 / 188 个 `func Test`）。**旧文档记的"约 60KB"低估约 30 倍。**（`src/build/wasm/`、`src/build/js/` 是另外两个后端，不在本项内。）
    - **改动**：`transpiler.go` 删 `build/llvm` import / `llvmGenerator` 字段 / 构造，`emitMIR(hirPkg, allowFallback bool)` → `emitMIR(hirPkg)`，删 8 处 `GenerateHIR` 回退分支 + `LastMIREmitted` + `hasFatalLowerDiag`，`NOLANG_MIR=0`/`=2` 改为明确报错（保留 `=1` 转储）；`cmd/no/main.go` 删 `no run` 的重编重跑重试点；`hir/golden_test.go` 解除对 `llvm.FilterByPlatform` 的依赖；`git rm -r src/build/llvm/`；删 scratch 目录 `src/cmd/tmp_test_and_i8/`。
    - **构建系统零改动**：`Makefile` 的 `GO_SOURCES`/`NO_SOURCES` 都是 `find` 通配，删文件自动适配——§15 里"更新构建系统"这条偏保守。
    - **Oracle 替代（删除的前置条件）**：新增 `scripts/mir_golden.sh` + `tests/golden/{legacy,mir}-baseline.tsv`（各 422 条，每文件 `<rc> <sha256(stdout)> <path>`）。`legacy-baseline` 是**语义**参照（注意其 82/422 即 19.4% 的 `rc=1` 属"无参照"，因 legacy 连 `print(1+2)` 都编不过），`mir-baseline` 是**回归**参照（删除后唯一可用）。
    - **验证**：`go build ./...`/`go vet ./...` 通过；`go test ./...` 失败集与干净 HEAD worktree（`47b6cad`）**逐条一致**（`fmt` 2 + `build` 4 + `checker` 2，全部既有失败；`TestGenerateHIRMatchesGenerate` 随包消失）；golden 比对 `SAME=388` / `REGRESS=0` / `DIVERGE=1`（已知假阳性）。
    - **踩坑**：`mir_golden.sh` 首版在**比对模式**也强制 `NOLANG_MIR=0`（与自身注释"比对模式从不强制"相矛盾），删 legacy 后表现为 `REGRESS=389` 的假象；macOS bash 3.2 不支持 `${b^^}`。详见 §13.3.13 ⑥。

---

## 13. 当前已知 gap 家族（下一轮推进目标）

### 13.1 MIR 专属回归（已闭环）
- **`test-arr.no` 的 `option/i64` opt-verify 失配**：已于 2026-09-11 闭环（见 §12 #12），`emitIndexStore` 加 `%option`→标量解包。
- **`emitSetField` 同类解包（2026-09-11 第二轮）**：`emitSetField`（`codegen.go`）的常规 store 路径与 `?T.field` 分支均加同法 option→标量解包（当 RHS 为 `%option` 且目标字段为标量时 `extractvalue %option %valV, 1` 取 ok payload 再 store）。标量赋值 `x = v` 走 `OpMove`/`emitMove`，无独立 `emitStore`，故 sink 站点已覆盖 `emitIndexStore`+`emitSetField`。10 个既有通过测试 + `test-arr.no` 无回归。

### 13.2 预存 crash/hang（非 MIR 专属，legacy 同崩/同挂）
实测根因（两模式各跑，超时 8s）：
- `vec.no` —— `SIGSEGV`（两模式 rc=1）。
- `test-std-net-ext.no` —— `SIGSEGV`（两模式）。
- `test-tls-part2.no` —— `SIGSEGV`（两模式）。
- `test-for2.no` —— `HANG`（两模式 rc=124，死循环/阻塞）。**（第四十一轮更正：该文件全文两行 `{` `} (true)` —— 块调用语法写成的 `while (true) { }`，无限循环是**设计**而非缺陷；`no build` 1 秒完成、rc=0，见 §13.3.15 ②。第四十一轮另证伪：与它同批记为 HANG 的三个 json/parse 文件**都不是**死循环。）**
- `tmp-getline.no` —— MIR=0 `HANG`（等 stdin）；**MIR=3 rc=0**（无输入正常退出 "no input"）——MIR 反而更好。
- `tmp-loop-test.no` —— 两模式 **rc=0**（现已通过，非真实挂死）。

**关键结论**：MIR=3 在失败前沿与 legacy **完全持平——零新增崩溃、零新增挂起**。3 个 SIGSEGV + 1 个 HANG 均为 nolang 通用预存 bug（legacy 代码生成缺陷），应作为通用 bug 在双路径闭环，不 blocking MIR 门禁。它们是独立的 legacy 修复轨道，不在 MIR 覆盖推进范围内。

### 13.3 COMPILE_ERR 长尾的精确分类（2026-09-11 第二轮）
对全量扫掠的 143 个失败测试逐个查 `MIR=0`（legacy）是否通过，得到关键二分：

| 二分 | 数量 | 含义 |
|---|---|---|
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **69** | 真正的 MIR codegen 缺口——本门禁要消除的目标 |
| 预存失败（两模式都挂） | 70 | legacy 也失败，非 MIR 引入（溢出 checker、`str-len` receiver、未 lower 特性等） |

> 注：143 = 140 COMPILE_ERR + 3 预存 HANG（其中 `tmp-getline`/`tmp-loop-test` 现 MIR=3 已通过，见 §13.2）。经三轮修复（§13.1、`str-clear`、`emitSetField`、§12 #15/#16），MIR 专属 gap 已由首轮 69 降至 **53**（§13.3.1）。

#### 13.3.1 MIR 专属 gap 的家族分布（**历史弧线：首轮 69 → 第五轮 41 → 第六轮精炼 39 → … → 第十二轮记 0（**定向子集口径**）→ **第十三轮权威全量重扫 = 21 → 第十四轮 = 17**）；下表为**历史根因归档**，不再代表当前未闭环集合，当前清单见 §13.3.7）

> **现状（第十轮 2026-09-12）**：全量重扫后 MIR 专属 gap 已降至 **3**（见 §10.1 覆盖表）：`test-async.no`（coroutine `await/run` 未实现）、`test-net-client.no`（`net-dial` FFI 内置未实现）、`tmp-icmp-test.no`（`net.net-icmp-open` raw socket FFI 未实现）。原 §13.3.1 表中 "opt_verify 剩 4 / crypto 深坑 / 字符串插值" 等 MIR 专属家族经第七~十轮已逐站闭环（插值 #28、crypto 系列 #15/#18/#23/#35、`test-x25519-keypair-diff` #36），下表仅保留作历史根因归档，不再代表当前未闭环集合。**第十一·续轮 #37/#38 闭环两个 net FFI 缺口 → 剩 1（`test-async`）；第十二轮（2026-09-12）`test-async.no` 经 §12 #39 完整 task 运行时闭环 → MIR 专属 gap 3 → 0（见 §10.1 覆盖表）。**
> **口径修正**：早期按错误症状直接分类曾报约 38，但漏做 legacy-parity 二分——混入了"两模式都挂"的预存失败。第五轮对每个失败测试额外跑 `NOLANG_MIR=0`：418 全量失败 137 个中，仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。第六轮（2026-09-12）对 133 个失败重做 fresh-parity，精炼为 **39 MIR 专属 / 93 预存**（更准，因部分 test 在第五轮后已被 #20 改变归属）。历史累计已闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、str-clear、#15 算术/负号 option 包回(13)、#16 数组 receiver(1)、#17 顶层容器字面量物化(15)、#18 `emitBitwise`(1)、#19 `print` option payload(3)、#20 `%txt` str-len-bytes receiver(1)、**#21/#22 arr-slice 切片(1)、#23 `err/ok/some` 构造器路由(2)**。

| 家族 | 数量 | 根因 | 本轮进展 |
|---|---|---|---|
| 字符串插值（`unsupported construct (string interpolation)`） | 16 → **1**（`test-diff-debug`，其 legacy 也失败） | `'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失 | **已闭环（2026-09-12 #28）**：改为仅在 print 家族调用点按段替换（字段走 std `fmt-*`），其余上下文按字面量发射——与 legacy 语义一致且顺便修好了 legacy 误伤 JSON/正则字面量的问题。仅剩表达式字段 `content.len-bytes()`（MIR 阶段已无 parser）拒收回退 |
| `opt_verify`（LLVM 类型校验失配） | 6（第五轮 parity 口径；其中 `test-option-index.no` 为两模式都挂的预存失败）→ **MIR 专属 4 实测** | 算术/负号/#18/#19 已闭环；`err/ok/some` 构造器 CALL 路由已由 #23 闭环。**实测 4 个为 MIR 专属独立深坑**（legacy 过/MIR=3 挂）：①`test-async.no`=`add void undef`（async 结果 void 操作数）；②`test-str-ops.no`=`sub i64,%str-long`（legacy 把 `str - char` 当**字符串拼接**结果 str，MIR 把 `t1=hi-"B"` 推成 i64 发 `sub`——字符串算术运算符重载的类型推断分歧，feature 级）；③`test-std-unix-fs-os.no`=invalid GEP on i64（struct field 落在标量 option）；④`move-eligibility-improved.no`=`alloca [1 x void]`（void 数组元素型别）。注：全量重扫时另现 `test-iout-autoconvert`/`test-safe-index-utl`/`test-tls-prf-only`/`test-tls13-crypto` 等也报 opt-verify 文本，但其中部分属预存失败或其他症状族混入，需各自 legacy-parity 细分（勿整体计为 MIR 专属）。均非快速 sink 站修复，建议 corpus 豁免或延后（逐站 lower 的"字符串算术/async/struct"特性族） | **已全部闭环**：#15/#18/#19/#23 + 第九轮 #32/#33/#34 + 第十二轮 #39；第十三轮复核**剩 0**（详见 §13.3.2） |
| `builtin` option/receiver 处理 | **2（现存）** | `str-len`/`str-len-bytes` receiver 为 `%option___byte`（option-of-byte）或 `i64`，未解包 → `builtin str-len receiver ...` | **第十三轮全量重扫确认仍存**：`tests/mem-safety/async-str-result.no`（`str-len receiver i64`）、`async-str-stress.no`（`str-len-bytes receiver i64`）、`tests/test-fs-struct.no`（`str-len receiver %option___byte`）= 家族 E |
| `builtin` 未实现（FFI/net 族） | 0 | `net-dial` / `net.net-icmp-open` / `net-send` / `net-recv` 已由 §12 #37/#38 闭环（内联发射 libc `socket`/`connect`/`inet_pton`/`send`/`recv`） | 已闭环（#37 #38） |
| `index dst slot` | 0 | `arr-slice.no`：`x = slice[i]` 结果类型推导为 void → 无 slot（与 with-len 同类型推导缺口） | **已闭环（2026-09-12 #21/#22）**：recv 值类型回推 + 切片边界语义，输出与 legacy 逐字节一致 |
| **MIR 专属运行时崩溃**（segfault/abort/trace-BPT） | **3 → 0** | 早期 7 个已由第七轮 #24–#27 闭环；**第十三轮全量重扫发现 3 个新暴露的 MIR 专属运行时崩溃**（legacy 过、MIR=3 崩）：`tests/mem-safety/async-shared-race.no`（SIGSEGV）、`tests/test-slot-rebind-unsafe.no`（SIGSEGV）、`tests/test-embed.no`（trace/BPT trap） | **已闭环（第十四轮 §12 #41）**：`run` 语法化异步 + 异步实参按形参类型强制 + 任务结果类型跟踪 + `#{embed}` 物化 → 三者 `MIR=3` rc=0 且与 legacy 逐字节一致，**red-line 恢复达标** |
| `test-str.no`（1 diagnostic） | 1 | 单点诊断 | 第十三轮全量重扫为 MIR 专属 gap（legacy rc=0、MIR=3 rc=1，报 `1 diagnostic(s)`）；**已于第十六轮（2026-09-13）闭环**：match 臂合成 `let it = <matched>` 由 transferring `OpMove` 改为 ALIAS（`hir2mir.go` `lowerStmt` `name=="it"` 分支），消除共享 subject 的 use-after-move/double-free，现逐字节 MATCH（见 §13.3.7） |
| `Undefined symbols`（链接缺符号） | 1 → **0** | `test-x25519-keypair-diff`：某内置未 emit 致链接缺符号 | 已闭环（#36 顶层未初始化全局物化）；第十三轮复核为 DIVERGE（两模式均 rc=0、仅输出不同） |

#### 13.3.7 全量重扫（2026-09-12）：真实 gap 清单（第十三轮 **21** → 第十四轮 **17**，纠正"0"的误判）

> **最新权威扫描见 §13.3.9（第三十轮）与 §13.3.10（第三十一轮）**；本节保留第十三~二十九轮的清单与口径演进，用于追溯"为什么曾经报 0"。

> **背景（重要纠错）**：此前多轮把 MIR 专属 gap 记为 **0**，但那是**定向/抽样子集**（curated 集合，`BAD=6` 画像）的结论，**不是全量 corpus 的结论**。第十三轮以**修正后的二进制路径**（`./bin/no`，而非陈旧的仓库根 `no`）对**全量递归 `tests/**/*.no` = 421 个**逐测跑 `MIR=0` vs `MIR=3`（25s/测超时），得到权威口径；第十四轮以同口径复核 C 家族闭环效果：
>
> | 类别 | 第十三轮 | 第十四轮 | 第十五轮 | 第十六轮 | 第十七轮 | **第十八轮（2026-09-13 续，双向 range 修复）** |
> |---|---|---|---|---|---|---|
> | MATCH（两模式 rc=0 且 stdout 逐字节一致） | 240 | 248 | 261 | 267 | 269 | 271 | **279** |
> | DIVERGE（两模式 rc=0、输出不同） | 57 | 58 | 55 | 55 | 52 | 51 | **43** |
> | **MIR_GAP（legacy rc=0、MIR=3 rc≠0）** | 21 | 17 | 3 | 1 | 1 | 1 | **1** |
> | LEGACY_FAIL（legacy 自身 rc≠0，非 MIR 引入） | 98 | 97 | 98 | 97 | 98 | 97 | **97** |
> | HANG（超时） | 4 | 1 | 4 | 1 | 1 | 1 | **1** |
>
> `MIR_GAP=17 ≠ 0`（第十四轮口径）。**第十三轮的 3 个 C 家族 red-line 运行时崩溃已在第十四轮全部闭环**（§12 #41），连带闭环 E 家族 2 个（`async-str-result`/`async-str-stress`）。下表为第十三轮的家族画像（**历史归档**），当前清单见表后"第十四轮更新"/"第十五轮更新"。
>
> ⚠️ **第十四轮"17"逐文件清单已证不准确**：权威全量扫描（`./bin/no`，第十五轮两次运行一致，2026-09-12）显示 `test_fs_error_complete`(DIVERGE)、`test-opt-struct-field`(MATCH)、`ffi-str-return`(MATCH)、`tmp-nbody-debug`(MATCH)、`test-uninit-output`(MATCH)、`test-open-perm`(MATCH)、`test-open-write`(MATCH)、`test_fs_error_final/info/simple`(MATCH×3)、`test-diff-debug`(LEGACY_FAIL) **均非 MIR_GAP**；真正的稳定 MIR_GAP 实为 **3 个**：`test-fs-struct.no`、`test-open-read.no`、`test-str.no`（连续两轮扫描均命中，见第十五轮更新）。第十五轮经 FIX A/B 闭环 `test_errno_basic`/`bug13-bool-coercion` 两个 `unknown callee` 缺口。
>
> | 家族 | 第十三轮数量 | 测试 | MIR=3 症状 | 第十四轮状态 |
> |---|---|---|---|---|
> | **A** `setfield field` | 4 | `test-open-perm/read/write`, `test_fs_error_complete`, `test-uninit-output` | 匿名选项结构体字面量无类型 → `FieldIndex` 失败 | 仍 4（下游另有缺陷） |
> | **B** `unknown callee <recv>.<method>` | 4 | `test_fs_error_complete`(`self.write-str`), `test_errno_basic`(`fd.to-str`), `test-opt-struct-field`(`port.to-str`), `mem-safety/bug13-bool-coercion`(`helper.compute-str`) | 接收者/模块限定名未解析 | 3（`test-opt-struct-field` 推进到 `opt-verify`） |
> | **C** **运行时崩溃（red-line）** | 3 | `mem-safety/async-shared-race`(SIGSEGV), `test-slot-rebind-unsafe`(SIGSEGV), `test-embed`(trace/BPT) | 两模式结论相反：legacy 过、MIR 崩 | **0 — 全部闭环（#41）** |
> | **D** `opt-verify`（LLVM 类型失配） | 2 | `mem-safety/ffi-str-return`（`i64` vs `%str-long`）, `tmp-nbody-debug` | 值类型与期望类型不符 | 3（+`test-opt-struct-field`） |
> | **E** `str-len`/`str-len-bytes` receiver 未解包 | 3 | `mem-safety/async-str-result`, `async-str-stress`, `test-fs-struct`(`%option___byte`) | 接收者为 `i64`/`%option___byte` | **1**（前两个为同一"任务结果类型"根因，随 #41 闭环） |
> | **F** `1 diagnostic(s)` | 1 | `test-str` | 待细分 | 1 |
> | — | — | `test-diff-debug` | `unsupported construct (string interpolation)` | 1（第十三轮被 legacy flaky 掩盖为 LEGACY_FAIL，见下） |
>
> **第十四轮更新（2026-09-12）**：`MIR_GAP` **21 → 17**，`MATCH` **240 → 248**（+8：3 个崩溃测试转 MATCH、2 个 E 家族转 MATCH、3 个原 HANG 转 MATCH）。**净减 4 个 gap**：`async-shared-race`、`async-str-result`、`async-str-stress`、`test-embed`、`test-slot-rebind-unsafe` 共 5 个闭环，其中 `test-diff-debug` 由 LEGACY_FAIL 转为 MIR_GAP（**非本轮引入**——第十三轮前的 `MIR_GAP=22` 扫描已收录它，其 legacy `MIR=0` rc 在 flaky 波动，见 §12 #41 注）。当前 17 个：A(4) `test-open-perm/read/write`、`test_fs_error_complete`、`test-uninit-output`；B/D(7) `test_fs_error_*`(4)+`test_fs_open_err`、`test_errno_basic`、`bug13-bool-coercion`、`test-opt-struct-field`、`ffi-str-return`、`tmp-nbody-debug`；E(1) `test-fs-struct`；F(1) `test-str`；interp(1) `test-diff-debug`。
>
> **第十五轮更新（2026-09-12 续）**：`MIR_GAP` **17 → 3**（权威全量重扫**两次一致**：`./bin/no`，421 文件，25s/测，**MATCH=261 DIVERGE=55 MIR_GAP=3 LEGACY_FAIL=98 HANG=4**；两次扫描逐文件分类完全相同，含 3 个稳定 gap）。**可归因的闭环（2 个，见 §12 #42/#43）**：`test_errno_basic.no`（`fd.to-str` → `i64.to-str`，值类型别名展开 FIX A #42）、`mem-safety/bug13-bool-coercion.no`（`helper.compute-str` → `compute-str`，跨模块自由函数调用名解析 FIX B #43）——均逐字节 MATCH（已 `diff` 验证）。**剩余 3 个稳定 MIR_GAP（连续两轮扫描均命中，与 FIX A/B 无关，pre-existing）**：
> - `tests/test-open-read.no`：`f-r: { ... it.close() ... }`（`it` 为 `?fd` match 臂绑定）。MIR `resolveCallee` 的 `recvTypeName` 因 match 臂 `it` 类型未传播，回退默认 `"str"` → 误解析为 `str.close` → `builtin str.close: cannot coerce arg 0 from %str-long to i32`（family A 下游；`### func fs.file.close` 已注册）。
> - `tests/test-fs-struct.no`：同类 `it.close()`/`[]t.close` 强制错误（`builtin []t.close: cannot coerce arg 0 from %vec to i32`）；legacy `MIR=0` 偶发 HANG（flaky），故该测试在两轮扫描中均判 MIR_GAP，但严格 parity 下 legacy 亦不稳定。
> - `tests/test-str.no`：`[use-after-move] value ... dropped after move at inst ... (double-free risk): (func=str.replace-n block=... inst=...)` 内存安全诊断，F 家族 `1 diagnostic`。
>
> **第十六轮更新（2026-09-13 续）**：`MIR_GAP` **3 → 1**（权威全量重扫 `./bin/no`，421 文件，8-worker，**MATCH=267 DIVERGE=55 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**）。本轮闭环 **第十五轮剩余的全部 3 个稳定 MIR_GAP**（均逐字节 MATCH，已 `diff` 验证）：
> - `tests/test-open-read.no` 与 `tests/test-fs-struct.no`：根因为 **match 臂 `it` 类型未传播**——嵌套/并列 match 的内层 arm 合成 `let it = <内层 subject>` 覆盖了外层 `it` 的 LLVM/值类型，使 `it.close()` 在 `resolveCallee` 处 `recvTypeName` 回退 `"str"`/`"[]t"` → 实参 `i32` 强制错误。**修复（`hir2mir.go`）**：新增 `lowerer.matchDepth` 计数 + `blockStartsWithIt` helper；`lowerIf` 在 arm body 进入/退出时增减 depth；`lowerStmt` 的 `name=="it"` 分支对 `matchDepth > 1` 的内层 `let it` **不覆盖外层 `it`**（保留外层绑定、内层 payload 若 owned 则 drop），外层 `let it` 仍给每 arm 独立值（per-arm 类型正确，避免 test_fs_error_complete 的 LocalType 污染）。两测试转 MATCH（第十五轮口径）。
> - `tests/test-str.no`：根因为 **match 臂合成 `let it = <matched>` 做了 transferring `OpMove`**——`str.replace-n` 中 `parts[i]`（`?str`）被 materialize 一次为同一 value id，但 `it` 绑定（`move it = parts[i]`）transfer 走该共享 subject，而 arm body 仍引用 `parts[i]` 原值（`s = parts[i]` 之后的 `s.len-bytes()`/`s.byte(j)`）→ `[use-after-move] value 1419 dropped after move`（真 double-free：option 与 `it` 共享堆 buffer、两处都 drop）。**修复（`hir2mir.go` `lowerStmt` `name=="it"` 分支）**：把 `it` 绑定改为 **ALIAS**（直接 `locals["it"] = val`，不 emit move、不 clone）——`it` 与 subject 同 value id，内存分析器仅 drop 一次；nil 安全（alias 不触碰 buffer，避免对可能 nil 的 `?str` 做 `str_clone` 崩溃）；每 arm 的 `val` 是 desugar 产出的 per-arm 独立 value id，per-arm 类型仍正确。该改动同时消除 test_fs_error_complete 旧基线（`unknown callee self.write-str` 编译错误）的 receiver 解析问题，使其干净编译（仅余 `fs.open` 后端语义 DIVERGE，见下）。`test-str` 转 MATCH（与 legacy 逐字节一致，输出 `a_b-c`/`a-b-c`/`a_b_c` 等）。
>
> ⚠️ **`test_fs_error_complete.no` 属 DIVERGE（非回归）澄清**：该测试 MIR=3 与 legacy 输出不同（legacy `fs.open('/tmp', read)` 返回 `ok` 并打开目录，MIR=3 返回 `err` "Is a directory"；写模式 MIR=3 带 `O_EXCL`、legacy 用 `O_CREAT`）。arm 选取由 `fs.open` 返回值决定，`it` 绑定改动**不可能**影响后端 syscall 语义——已用最小探针 `/tmp/fs_probe.no` 核对（`fs.open('/tmp')` 在 MIR=3 报 `MIR_ERR`、legacy 报 `MIR_OK`），证为 **`fs.open` 后端语义差**，计入 DIVERGE（55 个之一），非本改动引入的 MIR_GAP。
>
> **当前唯一稳定 MIR_GAP（1）**：`tests/test-diff-debug.no`（`unsupported construct (string interpolation)` 族；legacy `MIR=0` rc 在 1/0 间 flaky 抖动，故严格 parity 下偶判 MIR_GAP，属 interp/legacy 自身不稳定，非 MIR codegen 缺口）。

> **修复方向（收官）**：A 家族下游（`it.close()`/`[]t.close` 强制错误）、F 家族（`test-str` use-after-move）均已随第十六轮闭环；仅剩 `test-diff-debug`（interp 族 + legacy flaky），建议后续聚焦 interp 后端或将其从 MIR 门禁的稳定 gap 清单中剔除（按 legacy flaky 性质单独标注）。

> **第十七轮更新（2026-09-13 续，闭环 DIVERGE `?bool` 打印族 → DIVERGE 55→52）**：权威全量重扫（`./bin/no`，421 文件，8-worker）复测结果 **MATCH=269 DIVERGE=52 MIR_GAP=1 LEGACY_FAIL=98 HANG=1**（DIVERGE 55→52，净 −3，零 MIR_GAP 回归；LEGACY_FAIL 97→98 为 legacy 侧 flaky 失败——rc0≠0 不受 MIR 改动影响）。本轮闭环 DIVERGE=55 中的 `?bool` 打印族三测试：`tests/test-bool-debug.no` / `tests/test-bool-direct.no` / `tests/test-min-u8-bool.minimal.no`（均逐字节 MATCH，已 `diff` 验证）。**两处根因**：
> 1. **`str.to-bool` 内置 tag 误写**（`src/mir/builtin_call.go` `emitBuiltinStrToBool`）：`str.to-bool` 是 ForwardFunc 内置（generic body 被整段替代），旧实现 `insertvalue %option {i64 0,i64 0}, i64 1, 0` 把 **tag=1（=nil）** 写进 `ok(true)`，且只比较 `"true"` 一个串（`"false"`/空/`err` 全错判）——option 约定 tag 0=some/ok、1=nil、2=err（§runtime）。**修复**：重写 `emitBuiltinStrToBool`——比较 `"true"` 与 `"false"` 两串 + 取 receiver len 判空，`select` 出正确 tag/payload：`""`→nil(tag1)、`"true"`→ok(true)(tag0,payload1)、`"false"`→ok(false)(tag0,payload0)、其余→err(tag2)。**范围确认**：`str.to-u8`/`to-u32`/`to-i64` 不是内置（nolang 实现），其 `ok(value)` 走普通 `emitMove` option-wrap 已正确，故 `?bool` 唯一内置坑即 `str.to-bool`。
> 2. **legacy 的 bool 打印是「双通道」**（关键纠正，初判曾误改 `@print_bool` 致回退）：legacy `print(true)`（裸 bool）输出 **`1`/`0`**，而 `print(?bool)` of `ok(true)` 输出 **`true`/`false`**——两套行为不同。MIR 原先 `@print_bool` 写 `1`/`0`、`@print_option`（flat `%option`）读 i64 payload 也写 `1`/`0`，故 `?bool` ok 被印成 `1`/`0`（legacy 要 `true`/`false`）→ DIVERGE。**修复（双通道对齐）**：(a) `@print_bool` 维持 `1`/`0`（裸 bool，对齐 legacy）；(b) 新增 `@print_option_bool` 专印 `?bool` 为 `true`/`false`（在 `emitCall` 的 print 特殊分支，对 `argT=="%option"` 经新 helper `optionElemKind` 读 nolang 元素 `Kind==KindBool` 路由至此，flat `%option` 的 LLVM 类型不含元素信息，须查 nolang 类型表）；(c) `hir2mir.go` `lowerAssignNode` 的 KIdent/KIndex 赋值路径加 `wrapOptionIfNeeded`（非 option 值赋 `?T` 目标时隐式 `ok(v)` 包装，mirror `let` 声明已做的包装），修普通 `?bool` 局部重绑定同类坑（不触发 `str.to-bool` 内置路径）。
> ⚠️ **回退教训**：曾把 `@print_bool` 全局改为印 `true`/`false`，结果 `test-bool-print.no`（裸 `print(true)`）等 11 个测试由 MATCH 翻 DIVERGE（legacy 裸 bool 印 `1`/`0`）——重扫立刻暴露，确认 legacy 双通道后回退 `@print_bool`、保留 `@print_option_bool`，重扫 DIVERGE 63→52 全数恢复，无净回归。

> **第十八轮更新（2026-09-13 续，DIVERGE 52→51：闭环双向 range-for）**：本轮先对第十七轮遗留的 **52 个 DIVERGE** 做**确定性分流**（每个测试在 `MIR=0` 与 `NOLANG_MIR=3` 各跑 K=3 次，脚本 `/tmp/mir_triage.py`），结果：**REAL_GAP=32**（两模式稳定 rc=0 且输出不同）、**NONDET=2**（`tests/mem-safety/reverse-slice-clone.no`、`tests/test_path_char2.no`——legacy 侧非确定输出，非 MIR bug）、**RC_MISMATCH=18**（主体为 crypto/char 在 `NOLANG_MIR=3` 超时 + 少量「MIR 正确 / legacy 错误」族如 `tests/test-i8-debug2.no`（MIR 印 `-128`/`i8.MIN access ok`，legacy 印 `0`/`bad`））。关键认知：**并非所有 DIVERGE 都是 MIR bug**——`test-i8-debug2` 等 MIR 已正确、legacy 有误；crypto(sha1/sha256/x25519) MIR 产出**错误**哈希/密钥（真缺口但实现体量大，暂缓）；float打印需复刻 legacy 的 Go `strconv.FormatFloat(v,'g',-1,64)` 最短表示（`@print_double` 现硬编码 6 位小数→`3.140000`，暂缓）。REAL_GAP 最大单一族是 **match 作表达式**（`r = n: { ... }` 把 match 结果赋给变量）：`lowerExpr` 的 `hir.KIf`/`hir.KFor` 分支显式 `return NoVal`（`hir2mir.go` 2896「control flow used as expression value」），故 `r` 永不赋值、打印空行——横跨 `test-match-basic`/`test-minimal-ok`/`test-opt-match-all`/`test-option`(首行 nil) 等 **11 测试**，是下个高杠杆修复目标（需 phi/结果槽捕获 arm 值，慎防 260 个 MATCH 回归，靠全量重扫守护）。
> **本轮闭环 DIVERGE=52 中的 `tests/test-range.no`**：根因 `lowerRangeFor` 的整数区间形式**硬编码升序**（`iSlot + 1` 步进 + `iSlot < end` 比较），`[5..0)` 类**降序 range** 条件首轮即假→循环体零次执行→`sum` 恒 0（legacy 支持降序：5,4,3,2,1）。**修复**：以循环不变量 `start <= end` 经分支解析方向——init/步进/比较算子均按方向选择（升序 `<`/`<=` + `+1`；降序 `>`/`>=` + `-1`），IR 无 select/mux 故用 loop-invariant 分支（opt 可提升）。升序路径行为完全不变。**全量重扫（`./bin/no`，421 文件）实测 MATCH=271 DIVERGE=51 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**：`test-range` 由 DIVERGE→MATCH（净 −1），**零 MIR_GAP 回归、零新增 DIVERGE**（LEGACY_FAIL 98→97 为 legacy 侧 flaky 失败）。

> **第十九轮更新（2026-09-13 续，match 作表达式尝试回退，回到 18 轮基线）**：对第十八轮标记的「match 作表达式」族做了实现尝试，但**判定为死路、已全部回退**，净效果回到 18 轮干净基线。`tests/test-match-basic.no`/`tests/test-minimal-ok.no`/`tests/test-opt-match-all.no`/`tests/test-minimal-it.no` 仍 DIVERGE（MIR 打印空行），与 18 轮一致。两次具体改动及其回退原因：
> 1. **`match 作表达式` 捕获（`exprSink`/`exprCapture`/`stmtVal` + `captureArmValue`，改 `lowerStmt`/`lowerBlock`/`lowerIf`/`KLet`）**：**对多 arm match 根本错误**——把共享槽建成常量、且只在 `lowerIf` 外侧对首个 arm 调 `captureArmValue`，运行期真正执行的 arm 值不进槽→静默错值（潜在正确性 bug，非仅空输出）。属 feature 级改造（需 phi/多 arm 结果收敛 + 类型推导决定槽型），**不该在 parity 收官期冒险**，故整体回退。
> 2. **`undef` 守卫的 concrete-zero 发射**（在 KIdent 变体块 `name=="ok"/"err"/"nil"/"some"` 末尾无条件 `return` 零常量）：**引入 3 个真实回归**——`tests/mem-safety/bug13-bool-coercion.no`/`tests/test-bare-match.no`/`tests/test-ok-shadow.no`（均 18 轮 MATCH）被翻成 MIR 输出全 0，因为它们都声明了 `ok bool` **变量**，守卫把变量读取短路成变体零常量。该守卫本意是防「非 option 主体匹配 `err` 变体」产生 `undef`→poison→opt 折成运行期 trace/BPT（red-line 崩溃，曾现于 `tests/test-simple-match.no`）；但 pre-existing 变体块已对**真变体**正确处理，只对「变量名恰好是 ok/err/nil/some」误伤。回退后恢复 18 轮「变体块失败即落入变量查找」行为，3 个回归立刻回 MATCH；`test-simple-match` 的非 option err-match 崩溃属 **legacy 自身 quirk**（整数 42 去比 `err` 判别位无意义），MIR 复刻其 `'err'` 输出才是不合理的，按用户警示**不强行对齐**。
> **保留的安全修复（red-line 加固）**：`emitMove` 标量 option（`?i64`/`?u8`/`?bool`）的 `err`/`nil` peel——旧实现对 `dstT=="%str-long"` 走 `alloca i64`+`bitcast`+`load` 读 **24 字节**，但标量 option 的 payload 仅 8 字节（data 指针），高 16 字节为**未初始化栈**→`str_clone` 越界拷贝→SIGTRAP。`option` 约定 `err(e str)` 的 payload 在标量 option 的 8 字节槽内**无法承载 str 的 len/cap**，故改为**标量 option 跳过 str-peel**（不产生 OOB 读），行为对当前执行路径**无变化**；该路径为 latent（corpus 暂无测试运行期触发），故未改变任何测试分类。

> **第二十轮更新（2026-09-13 续，match/if 作表达式 正确实现 + 闭环 8 测试 + 消除 3 个 red-line trap）**：继承第十八/十九轮线索，**重做 match/if 作表达式捕获并成功闭环**。核心修正吸收了第十九轮两次失败教训：
> 1. **可变 slot + 每 arm `EmitMoveInto`（取代第十九轮的常量槽+首 arm 守卫）**：`r = subject: { arms }` 经 `lowerStmt` KLet 检测 `child.Kind==KIf && name!=""` → 置 `exprCapture=true`、`exprSink=NoVal`、调 `lowerIf`；每个 arm 经 `lowerBlock`→`lowerStmt` KExprStmt（裸值语句 `l.stmtVal = l.lowerExpr(child)`）→`lowerIf` 的 then/else 后 `captureArmValue(armVal)` 把值 `EmitMoveInto(exprSink)`（str 值先 `OpClone` 防双释放）；`r` 绑定 `exprSink`（可变 slot）。`if` 作表达式（`r = if cond {a} else {b}`）同路。嵌套 match 守卫（`ok(it>127) ->`）自身是 if 链，`exprCapture` 仍活跃→其 arm 收敛进同一 slot。
> 2. **去第十九轮 `undef` 守卫（修回退回归）**：不再在 KIdent 变体块短路发零常量，故 `ok`/`nil`/`some`/`err` 同名**变量**正常查 locals→`bug13`/`test-bare-match`/`test-ok-shadow` 不回归（三者本就 MATCH，本轮仍 MATCH）。
> 3. **去 `KExprStmt` 双 lower（修重复输出 bug）**：旧 `KExprStmt` 在「非 KIf/KFor 分支」落空穿透到尾部 `for` 循环把同一 child **再 `lowerExpr` 一次**→每条语句执行两遍（test-bare-match 输出 `1\ndone`×2）。改为每分支 `return`，裸值语句 `l.stmtVal = l.lowerExpr(child)` 后直接 `return`。
> 4. **KIdent 变体块 red-line 修复（闭环 3 个新 trap）**：`match 42: { err -> }` 类**非 option subject** 比 `err`/`nil` 变体臂，旧路径变体块对非 option 上下文「不短路也不发零」→落入 unresolved identifier 发 `undef`→`subject == undef` 是 LLVM poison→opt 折成 `trace/BPT trap`（red-line 崩溃，第十九轮已现于 test-simple-match）。**正确修法**：变体块**入口先查 `l.locals[name]`/`l.globals[name]`**（命中即 `return` 变量，不当变体——正是第十九轮回归的根因），仅当非变量时，option 上下文 `EmitOptionWrap`、非 option 上下文发 `typeHint` 类型 concrete-zero（比较恒 false→fall through，无 poison、无 trap）。`test-match-2`/`test-simple-match`/`test-simple-match2` 由 trap→**DIVERGE**（rc=0 安全；legacy 打 `'err'` 是 quirk，MIR 不复刻，输出空行）。
> **全量重扫（`./bin/no`，421 文件，8-worker）权威实测：MATCH=279 DIVERGE=43 MIR_GAP=1 LEGACY_FAIL=97 HANG=1**（对比第十九轮 271/51/1/97/1：**DIRVERGE 51→43（净 −8，零回归）、MIR_GAP 稳 1**）。闭环的 8 个 match/if 作表达式测试：`tests/test-if-str.no`、`tests/test-match-basic.no`、`tests/test-minimal-it.no`、`tests/test-minimal-it2.no`、`tests/test-minimal-ok.no`、`tests/test-opt-match-all.no`、`tests/test-opt-match2.no`、`tests/test-opt-match3.no`（均逐字节 MATCH）；另 3 个非 option-subject quirk 测试由 trap→DIVERGE（不再崩溃）。**本轮零新增 MIR_GAP、零回退**（`bug13`/`test-bare-match`/`test-ok-shadow` 均仍 MATCH）。剩余 DIVERGE=43 主体为 crypto(sha/x25519)/char/float 打印/option 等 REAL_GAP 与 NONDET（legacy flaky），下轮续分。
> **全量重扫（`./bin/no`，421 文件）**：DIRVERGE 集合与 18 轮**逐字节相同**（comm 双向为空）→ 精确回到 18 轮基线（稳定口径 **MATCH=271 DIVERGE=51 MIR_GAP=1 LEGACY_FAIL=98 HANG=1**）。本 19 轮 sweep 显示 `MIR_GAP=0` 仅为 `tests/test-diff-debug.no` 的 legacy 本次 flake 至 rc≠0、被重分类进 LEGACY_FAIL 的**假象**（其 `MIR=3` 始终 rc=1：字符串插值不支持；3 次手动复测均为 `legacy_rc=0 mir_rc=1`，稳定 MIR_GAP 仍是 1）——非真闭环。**结论**：match 作表达式的正确修复需独立的 phi/结果槽捕获 feature（且须区分多 arm 收敛），建议作为后续独立任务，不在 parity 收官期与 260 个 MATCH 同改同担风险。

> **第二十三轮更新（2026-09-14，全局重赋值修复 + `?f64` 闭环 + 基线零回归裁决）**：全量重扫（`./bin/no`，421 文件，8-worker，25s/测）权威实测 **MATCH=283 DIVERGE=33 MIR_GAP=0 LEGACY_FAIL=96 HANG=9**（对比第二十轮 279/43/1/97/1：MATCH +4、DIVERGE −10、MIR_GAP −1、LEGACY_FAIL −1、HANG +8）。本轮两处修复：
> 1. **全局重赋值 bug 闭环 `test-ifelse2`**（`hir2mir.go`）：顶层 `let` 双重角色注册污染。注册循环仅首次同名 `let` 注册模块级 `@global`（后续重赋值 `x=10` 不覆盖初始化器）；`synthesizeMainForTopLevel` 仅当本 `let` 恰是已注册模块常量声明才 `continue` 跳过内联，同名后续重赋值落到内联分支发运行时 store。`test-ifelse2`（`x i64=5` + `x=10` + block 判 `x==5`）由 DIVERGE→MATCH（`five`/`not five`）。
> 2. **`?f64` 闭环 `test-f64-option`**（`codegen.go` 两处）：(a) `optionPayloadLLVMType` 原 `case "double":` 无 `f64` → `?f64` 降为 flat `%option`（i64 载荷）截断 `3.14`→`3`；改 `case "double","f64": return "double"` → `?f64`→`%option_f64={i64,double}`（自动按值存 double，`@print_option_f64` 经 `emitOptionPrintHelper` 已支持 double 载荷→调 `@print_double`）。(b) `@print_double` 旧实现硬编码 6 位小数 + 把带符号 `ipart`/`fi` 喂 `@digits`（期望绝对值）→ 负数 garbage；重写为手写 `%g` 风格（绝对值喂 `@digits`、符号单独写 `-`、小数部 `fpart*1e6` fptosi 后 6 位 unrolled 打印并裁 trailing zeros，复刻 legacy `%g`：`3.14`/`-2.5`/`0`/`1500`/`1500.0`→`1500`）。
> **关键陷阱（snprintf mis-lowering）**：曾试 `declare i32 @snprintf(i8*,i64,i8*,...)` + `snprintf("%g",v)`，但 variadic 调用被 opaque-pointer 重写/`opt -O3` **mis-lower**——double 实参到达 snprintf 时已被破坏，输出 `%g` 格式化垃圾（`5.3e-315`/`4.08e+179` 等）。改**手写 `@print_double`**（仅用已验证 `@digits`+浮点 op）。手写 IR 块里 `%d5 = %q5` 是非法指令（值别名值），直接用 `%q5` 即可（首个版本因此 opt-verify 失败报 `expected instruction opcode`）。
> **MIR_GAP 1→0 的真相**：`test-diff-debug` 在第二十三轮 sweep 中因 legacy 本次 flake（rc0≠0）被重分类进 LEGACY_FAIL，致 MIR_GAP 显示 0——但 `MIR=3` 始终 rc=1（字符串插值不支持），**稳定 MIR 专属缺口仍为 1（interp 族），非真闭环**；`test_fs_error_complete` 由 MIR_GAP(崩溃 rc3=1)→**DIVERGE（rc3=0，MIR 输出比 legacy 更正确）**：legacy `fs.open('/tmp')` 误报 `ok` 打开目录、写模式不报 `File exists`，MIR=3 正确返回 `Is a directory`/`File exists`。按用户铁律「原实现不一定正确」——此处 **MIR 正确、legacy 错误，计入 DIVERGE 而非对齐 MIR 到 legacy**。
> **⚠️ HANG 1→9 已复跑确认（round-25）非 MIR 回归**：round-24 的 9 个 HANG（`test-for2`/`test-hmac`/`test-hmac2`/`test-http-rest`/`test-net-http`/`test-sha256-min`/`test-shortname-import`/`test-str`/`test-vec-assign`）经逐测 + round-25 复跑确认系 **8-worker 争用下 CPU 饥饿超时假象**——`test-hmac`/`test-sha256-min`/`test-str`/`test-vec-assign`/`test-shortname-import` 等均在 `MIR=0` 与 `MIR=3` **两模式同时超时**（nolang SHA256/HMAC 源码在争用下 >25s），与 MIR 后端无关；round-25（争用较低）HANG 回落至 **1**（仅 `test-for2`，与 第二十轮一致）。**结论：HANG 跳变零 MIR 回归，稳定 HANG≈1**。MIR_GAP 在 round-25 回落显示 **1**（`test-diff-debug` 字符串插值不支持，legacy 本次未 flake 故未被掩），证 round-24 的 `MIR_GAP=0` 系 legacy flake 掩的假象——**稳定 MIR 专属缺口仍为 1（interp 族）**。DIRVERGE/MATCH 在两轮间 4 个边界测试浮动（33↔37 / 283↔285），属 legacy 侧 flaky/非确定输出，已记入 NONDET。
> **基线零回归裁决（实证）**：曾误判 5 个「回归」（test-hmac/test-hmac2/test-sha256/test-shortname-import/test_fs_error_complete），因脏 baseline 比对。用 `git stash` 取 committed HEAD 重建干净 baseline（`/tmp/no_stash` 与 `/tmp/no_pre` 字节同尺寸 16412498，证实 `/tmp/no_pre` 即干净 round-22），直跑：4 个 crypto 测试**仍 DIVERGE**（与 current 一致）→ 系 MIR 既有 sha256/hmac 真 bug，非本改动回归。结论：**当前未提交改动零真实回归**，净效果 MATCH 240→283、DIRVERGE ~43→33、MIR_GAP 由稳定 1（test-diff-debug）经 round-24 分类 0（flake 掩）。

> **A 家族根因与第十三轮修复（部分）**：`fs.open(p, { mode: 0 })` 的选项字面量在 parser 中 `StructLiteral.Type` 为**空串**（`parseStructLit` 注释："由 codegen 推斷"，即由上下文推断），legacy 在 codegen 期从**调用点**解析该上下文，而 MIR 在 codegen 期没有调用点信息 → `lowerStructLit` 发射**无类型** `structlit` → `emitSetField` 的 `FieldIndex` 失败（`setfield field`）。**修复（`hir2mir.go`）**：`lowerCallArgs` 现从被调函数的**声明形参类型**为匿名 `{...}` 实参播种类型（`structLitTypes` + `paramRawTypesOfCallee` + `noteAnonymousStructLit`，并经 `canonStructRaw` 规范化为 `StructFields` 键，保证与 codegen 的 LLVM 结构体类型命名一致）。**效果**：`test-open-perm/read/write`、`test_fs_error_complete` 越过 `setfield field`（4 个测试由**编译错误**推进到**更深层**错误），`test_fs_error_final/info/simple`、`test_fs_open_err` 进入运行时后触发 trace/BPT trap。**未收官**：A 家族下游仍有独立缺陷（见 C/D/E/F 家族），故第十三轮**未净减 gap 数**（`MIR_GAP` 仍 21；`test-diff-debug` 的"新增"系 **legacy 自身 flaky**：其 `MIR=0` rc 在 1/0 间抖动，与本修复无关）。

> **§13.3.1 的历史计数说明**：该表所列 41/39/36/23 等均为**历史快照**；第十三轮复核后，其中 §13.3.2 曾记的"`opt_verify` 剩 3"（`test-str-ops`/`test-std-unix-fs-os`/`move-eligibility-improved`）确已随第九轮 #32/#33/#34 闭环（`test-str-ops` 第十三轮实测逐字节一致）。但**这不等于"MIR 专属 gap = 0"**——另有上表家族共 **21 个真实缺口**（第十三轮实测；第十四轮闭环 C 家族 3 个 + E 家族 2 个后为 **17**），多为早期按症状分类时被并入"预存失败"、未做 fresh legacy-parity 的项。

#### 13.3.8 第二十九轮（2026-09-15）权威全量扫描：当前未闭环全集

> 口径：`./bin/no`（**绝不能用仓库根陈旧的 `no`**），全量递归 `tests/**/*.no` = **421**，实测 **420**（`test-for2.no` 两模式都挂死，本轮起脚本带 90s `perl alarm` 超时仍被排除）。基线用 `NOLANG_MIR=2`（fallback 开启，等同 legacy 行为），被测为 `NOLANG_MIR=3`（关闭回退）。**分类先看基线 rc**：基线过、MIR=3 挂 → `MIR_GAP`（真缺口）；两模式都挂 → 预存失败（非 MIR 引入）。这一分界是 `scripts/mir_sweep_fast.sh` 第二十九轮才补上的——此前只看 MIR=3 的 rc，导致"预存失败"被当成"MIR 缺口"或反过来互相掩盖。

| 类别 | 第二十八轮（2026-09-15 初） | **第二十九轮（2026-09-15 终）** | 说明 |
|---|---|---|---|
| MATCH | 352 | **364**（净 **+12**） | rc 均 0 且 stdout 逐字节一致 |
| DIVERGE | 0 | **1** | `test-quant-all.no` |
| **MIR 专属 gap** | 0（在 4 个测试基线被 std 硬伤拖挂的前提下） | **2** | `mem-safety/bug15-read-dowhile-copyfile.no`（`unknown callee []t.slice`）、`test-process-run.no`（`unknown callee i64.trim`） |
| 预存失败（两模式都挂） | 68 | **53**（净 **−15**） | 见下表家族分布 |
| HANG | 1 | 1 | `test-for2.no` |

**预存失败 53 的家族分布（第二十九轮实测）**：

| 家族 | 数量 | 根因 / 下一步 |
|---|---|---|
| 运行时崩溃（`trace/BPT` 5、`SIGSEGV` 5、`abort` 2） | 12 | red-line 观察项；两模式同崩属预存 bug，但 MIR 侧应逐个确认是否同因 |
| MIR codegen 报错（未展开） | 11 | 需逐条取 stderr 细分 |
| 溢出演算未标注（真·整数运算，非误报） | 8 | `test-all`/`test-sse`/`test-database-sql`/`test-ffi-mysql`/`test-ffi-sqlite`/`tmp-words-test`/`test-sha256-simplified`/`test-tagged-enum`——语料迁移：加 `#{overflow = wrap}` 或改 `?=` |
| `builtin with-len: no result slot` | 4 | `test-json`/`test-parse-min`/`test-json-nested-match`/`test-json-parse-option`；Dst 无槽位，与 #54 同族（值被判成 void） |
| `builtin str-len receiver i64` | 3 | `i.no`/`map-tombstone`/`test-map-generics`；receiver 型别解析失败回落 i64 |
| `net-dial: cannot marshal host as C string` | 3 | `test-http-rest`/`test-net-http`/`test-tls`；DNS 名（非 IP 字面量）host 未走 `getaddrinfo` |
| `unknown callee []t.get` / `[]t.clone` / `i64.to-str` | 3 | 泛型切片方法 `[]t.*` 与标量别名方法解析 |
| 其余单点 | ~9 | `net-send` data ptr、`print` 不支持 `%vec`、`number.f64-to-f32` 强转、`index dst slot type=void`、`# {index-out}` 必须单独成行（parser，2）、`x25519` unknown function、`test-diff-debug`（自身 DP 算法 bug） |

**下一轮优先级（建议，第三十轮更新）**：① 收敛最后一个 MIR_GAP `test-diff-debug`（DP 表全 0 / `bus error`；根因落在 `[]str` 元素的 `compare` 调用降级路径——注意探针显示 `with-len`+字面量赋值的 `[]str` 元素方法调用在**两模式**都 segfault，属更底层的预存缺陷，须先修通用缺陷再判定 MIR 责任）→ ② `with-len`/`index dst slot` 的 void 型别族（#54 同族余孽，4+1）→ ③ `str-len receiver i64`（3）→ ④ 语料迁移 8 个真的未标注溢出 → ⑤ `net-dial` host 编组（3）。

#### 13.3.9 第三十轮（2026-09-15）权威全量扫描：当前未闭环全集

**口径**：`./bin/no`（绝不用仓库根 `no`），全量递归 `tests/**/*.no` = **421**，实测 **420**（`test-for2.no` 两模式都挂死，未计入）；基线 `NOLANG_MIR=2`（fallback 开启，等同 legacy 行为），被测 `NOLANG_MIR=3`（关闭回退）；4-worker、`perl -e 'alarm shift; exec @ARGV'` 150s 超时。分类口径与脚本同 §13.3.7：先看基线 rc——基线过而 MIR=3 挂 = `MIR_GAP`（真缺口）；两模式都挂 = 预存失败。

**实测**：`MATCH=367`、`DIVERGE=0`、`MIR_GAP=1`、`两模式都失败=52`（CERR 39 + CRASH 13）、`HANG=1`。

| 对比 | 第二十九轮 | 第三十轮 | Δ |
|---|---|---|---|
| MATCH | 364 | **367** | +3 |
| DIVERGE | 1 | **0** | −1 |
| MIR 专属 gap | 2 | **1** | −1 |
| 两模式都失败 | 53 | **52** | −1 |
| HANG | 1 | 1 | 0 |

**唯一剩余 MIR_GAP = `tests/test-diff-debug.no`（预存）**：`MIR=0/2` rc=0（3/3 稳定，输出含正确的 DP 表 `2 1 1 0 / 1 1 1 0 / 1 1 1 0` 与 5 条 diff 操作），`MIR=3` rc=1（3/3 稳定）——输出前缀与 legacy 逐字节相同，但 `DEBUG dp table` 的每一行都是 `0 0 0 0`，随后 `Error: signal: bus error`。**用 `git worktree add /tmp/no-r29 d2803fe` 在干净 HEAD 上复测**：同样 rc=1、同样全 0 的 DP 表（只是信号为 `trace/BPT trap`）→ **证明是预存缺口，与本轮改动无关**。DP 循环里唯一的"可疑新面"是 `a[ai].compare(b[aj]) == 0`（`[]str` 元素的 `str.compare` 方法调用）；已构造等价探针 `/tmp/r3.no` 复刻整个 DP 循环，结果**两模式都 segfault**，说明该路径上还有一个更底层的通用缺陷，需单独立项。

**预存失败 52 的家族分布（第三十轮实测，CERR 39 + CRASH 13）**：主体仍是第二十九轮 §13.3.7 表列出的那几族（溢出演算未标注、`builtin with-len: no result slot`、`str-len receiver i64`、`net-dial` host 编组、泛型切片方法 `[]t.*` 等），CRASH 侧新增可见的 `[]str`/map 族：`vec.no`、`test-map.no`、`test-basic.no`、`test-chain-copy.no`、`mem-safety/test-minimal-str-map{,2}.no`、`mem-safety/test-minimal-option-str.no`、`mem-safety/test-option-str-match.no`、`mem-safety/map-key-leak.no`、`test-tls-debug.no`/`test-tls-part2.no`/`test-http3.no`/`test-std-net-ext.no`——**这些均 legacy 同挂**，属通用 bug，不 blocking MIR 门禁。

**本轮的方法论要点（两轮验证后固化）**：
1. **未提交的改动必须重扫才能计入结论**。第二十九轮末尾有一个**未提交、未重扫**的 `emitFunc` 参数别名收窄改动，它引入 5 个新 MIR_GAP（`test-tls-part1`/`test_char_step4`/`tmp-nbody-debug`/`test-realpath-debug`/`tmp-arr-f1`）。若本轮直接信任文档的 "MIR_GAP=2"，这 5 个会被整体漏掉——**文档的结论只覆盖"上次扫描时的二进制"，不等于"当前工作树"**。
2. **判定"是否本轮引入"必须用 `git worktree` + 逐 hunk 二分**。`git worktree add /tmp/no-r29 <rev>` 建干净基线；把工作树改动按 hunk 拆成子补丁（`git diff | awk` 截取 hunk 段）再逐个 `git apply -R` 回退，可精确定位到某个 hunk。本次即用此法把 5 个 gap 钉到 `emitFunc` 的两个 hunk 上（单回退它们即复绿）。
3. **注释里"因为某机制所以安全"的推理必须回到代码核对**。`emitFunc` 改动的注释声称"别名会让 callee 的 drop 释放调用方 buffer"，但 `insertDrops` 的 `droppable` 计算里一行 `!isParam[inst.Dst]` 就把全部入参排除在 drop 之外——**callee 从不 drop 入参**，整段推理不成立；而它引入的"按引用改写入参容器不回传"才是真问题（`buf []byte` 无初值时 `tls.put-u16(buf, …)` 的 `ensureVecBuffer` 只给 callee 的副本分配缓冲）。
4. **反过来说，"两模式都挂"的测试也可以提示 MIR 侧的缺口**。`test-diff-debug` 自身在早期轮次两模式都 rc=1（测试自己的 DP bug），#44 让它能构建后 legacy 侧稳定 rc=0，缺口才显形——**预存失败集是会漂移的**，每轮都应重跑两侧 rc 而不是沿用旧分类。

**扫掠工具缺陷（第三十一轮已修复，见 §13.3.10 ①）**：`run_to`（`perl -e 'alarm shift; exec @ARGV'`）对本轮的 `test-for2.no` 失效——`MIR=2` 那一侧的子进程连 150s alarm 都未被杀死，`sweep_one` 永久阻塞，`xargs` 因 `-P 4` 仍有空位而不退出，最终只能人工 `pkill -9 -f 'test-for[2]'` 收尾（此时 `xargs` 报 `terminated with signal 9; aborting`，汇总仍打印但 `HANG` 计为 0）。**修法（已落地）**：`run_to` 改为 `fork` + `POSIX::setpgid(0,0)`（macOS 无 `setsid`）+ 超时 `kill -KILL -$pgid`（杀整个进程组/子树），并保留退出码/信号语义（信号→`128+sig`），另补 `HANG_LEGACY` 桶与 `grep -c "^HANG "` 修正——**只杀 `no` 本身会留下编译产物孤儿持续吃 CPU**（memory 2026-09-15 已记录同款级联假象）。

#### 13.3.2 各家族根因与修复方向
> 注意：本节原按错误症状分类（首轮）。第五轮 legacy-parity 二分（§13.3.5）显示 `setfield field`/`unknown callee`/`lencap kind` 等"症状家族"绝大多数是**两模式都挂**的预存失败（已计入 §13.3.5 的 92），并非 MIR 专属缺口；下文仅保留经 parity 确认的 MIR 专属修复方向（共 41，见 §13.3.1）。

- **字符串插值（15，MIR 专属）—— 第八轮已闭环**：`'x={expr}'` 在 HIR `KStrLit` 仅保留原始文本，嵌入表达式在 parser 阶段已丢失；MIR 无法 re-parse。关键事实：parser 已有 `parser.ParseFormatString`（`src/parser/format_spec.go`），legacy 在 **codegen 期**对 `sl.Value` 原文调用它，经 `callNamedFormat`→`dispatchFmtCall` 路由到 `@fmt-int`/`@fmt-str`/`@fmt-bool`/`@fmt-f64`/`@fmt-uint`。**实测确认的关键语义**：legacy 的拦截点 `shouldInterceptNamedFormat` 只由 `callFmt`（print/eprint/printf/eprintf/sprintf/format）调用，所以**只有 print 家族做替换**；`s = 'x={n}'` 之类的其他上下文原样输出花括号（实测：legacy 输出 `x={n}`）。第八轮据此实现（§12 #28）：判据精确化 + 在 `lowerCall` 拦截按段发射；无 spec、带 spec、表达式下标 `{h[i]:02x}`、`format()` 返回 str 全部支持，输出与 legacy 逐字节一致（`test-named-format` 含 `{{`/`}}` 转义与 20 余种 spec 全部一致）。副作用：6 个原"两模式都挂"的测试（`test-guard`/`test-quant-all`/`test-quant-all1`/`test-re-debug`/`test-regexp`/`test-regexp1`）现在 **MIR 通过、legacy 失败**——legacy 把 JSON/正则里的 `{...}` 误当插值而崩。

- **`opt_verify`（第十轮实测 4 个 MIR 专属独立深坑 → 第十三轮复核**已全部闭环，现剩 **0**）**：§12 #15/#18/#19 闭环算术/负号路径；#23 闭环 `err/ok/some` 构造器 CALL；**第十二轮 #39 闭环 `test-async.no`**（`add void undef`——async 结果 void 操作数，经完整 task 运行时 + `OpRun`/`OpAwait` 直发消除）；**第九轮 #32/#33/#34 实际已闭环余下 3 个**——本行"剩 3"系文档滞后（原第 (b)(c)(d) 三项均已随第九轮落地）：(b)`test-str-ops.no`（#33，`str - char` 拼接语义对齐 legacy）**第十三轮实测 `MIR=3` 与 legacy 逐字节一致**；(c)`test-std-unix-fs-os.no`（#32，裸结构体名解析为限定名）**第十三轮实测两模式均 rc=0，输出仅剩 §16 所述 bool 打印形态差异（DIVERGE，非 gap）**；(d)`move-eligibility-improved.no`（#34，`[a.len()]` 元素型 `[1]void`；实际路径 `tests/mem-safety/move-eligibility-improved.no`，文档早前写作 `tests/move-eligibility-improved.no` 系路径笔误）。**结论**：opt_verify 家族 MIR 专属缺口已清零，无遗留 sink 站待修。
- **`builtin` option/receiver（2 剩余，MIR 专属）**：`str-len: missing receiver`（async）、`number.f64-to-f32: cannot coerce %option`；`str-len-bytes receiver %txt` 已由 #20 修。修复方向：对齐 legacy 的 receiver 路由 + option 实参解包。
- **`builtin` 未实现（0，已由 §12 #37/#38 闭环，FFI/net 族）**：`net-dial`/`net.net-icmp-open`/`net-send`/`net-recv` 现已通过内联发射 libc `socket`/`connect`/`inet_pton`/`send`/`recv` 实现（`fs.read-file` 已由 #35 闭环）。此前 `test-net-client.no`/`tmp-icmp-test.no` 因缺这些内置在 `MIR=3` 构建失败，现均构建通过（`tmp-icmp-test` 在 macOS 非特权 `SOCK_DGRAM` ICMP 下还能实际运行、输出与 legacy 一致）。
- **`index dst slot`（0，已闭环）**：`arr-slice.no` 索引结果类型推导为 void→无 slot，与 `with-len` 同类类型推导缺口。已由 §12 #21（recv 值类型回推切片结果型） + #22（切片 `(`/`)` 边界语义）闭环，输出与 legacy 逐字节一致。
- **MIR 专属运行时崩溃（red-line，第七轮大幅闭环）**：clone/move/struct-field 系列内存所有权 bug（legacy 通过、MIR 崩）。第七轮按根因拆为三族并闭环：(a) **定宽数组→slice sink 未归约 `%vec`**（`emitSetField`/`emitMove`，§12 #24/#25）——`store [N x T]` 直塞 `%vec` slot 致 len/data 读到数组元素；闭环 `struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak`/`double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`（均输出与 legacy 逐字节一致）。(b) **`@main` out-param 漏传**（`emitEntry`，§12 #26）——闭环 `cross-fn-str-return-dfree`。(c) **结构体字面量字段初始化器所有权**（`MovesArg`，§12 #27）——闭环 `struct-field-leak`/`struct-move-is-moved` 的 NUL-name。**崩溃计数 10 → 0**（red-line 达标）。剩 `struct-field-shallow-copy-bug` 1 个为**语义分歧非崩溃**（legacy 对 `out = c.data` 做 vec 深克隆使 `d` 独立；MIR 浅拷贝共享 data → `d[0]=999` 影响 `c.data[0]`；需 `%vec` 深克隆 helper，暂缺）。
- **`setfield field`/`unknown callee`/`lencap kind`（症状家族，多为预存失败）**：第五轮 parity 二分显示这些多属两模式都挂（§13.3.5 的 92）；原"std 结构体字段未进 StructFields"等根因分析仍成立，但其对应 test 大多 legacy 也失败，需先修 std/test 再判定是否 MIR 责任。

#### 13.3.3 本轮（第三/四轮）增量
- 第三轮：修复 **§12 #15**（算术/负号 option 包回）+ **§12 #16**（数组 receiver 路由）。MIR 专属通过数（非递归口径）：273 → ≈287（净增 ≈14；其中 #15 解锁 13 个 x25519/fe-mul 系列 + #16 解锁 `test_zero.no`）。
- 第四轮（2026-09-11）：闭环 **§13.3.4**（#17 顶层容器字面量物化）+ **#18**（`emitBitwise` option 解包包回）+ **#19**（`print` 内联 option payload 路由）。全量递归口径：committed 基线 **244** → **280**（#17+#18，净 +36）→ **281**（#19，净 +1；crypto 假 HANG 不计，见 §10.1）。**零 MIR 专属回归**（14 个既有通过测试 + 全量重扫守护）。
- 口径提示：第三轮 "≈287" 为**非递归** `tests/*.no`（≈368）测量；第四轮 "281" 为**全量递归** `tests/**/*.no`（418）测量，总数不可直接相减。
- 第五轮（2026-09-11）：**legacy-parity 二分**重塑失败分类 + **#20**（`%txt` receiver 的 `str-len-bytes`）。(a) 对 418 全量每个失败测试额外跑 `NOLANG_MIR=0`：失败 137 个里仅 **41 个 MIR 专属**（legacy 过、MIR=3 挂），**92 个两模式都挂**（legacy 也失败，属通用预存 bug / std·test 缺陷，见 §13.3.5）。据此修正 §10.1/§13.3.1 的真实 MIR 专属 gap = **41**（非早期按症状估的 38）。(b) 闭环 #20 → `test-slot-rebind.no` 从失败转 PASS（全量递归口径 **281 → 282**）；该 test 打印指针类值输出非确定，属 §13.3.5 的 test 自身缺陷，但 codegen 门禁已解除。(c) 印证用户"std/tests 都可能不合理"警示：**绝大多数长尾失败是预存/通用 bug，不 blocking MIR 门禁**，应作为通用 bug 双路径闭环（§13.3.5）。
- 第七轮（2026-09-12）：**mem-safety 崩溃族（red-line）大幅闭环** —— §12 #24（定宽数组→slice 字段 `%vec` 归约：借用/堆拥有两种）、#25（定宽数组→`%vec` 变量 move 归约）、#26（`@main` out-param 接线）、#27（结构体字面量字段初始化器所有权转移 `MovesArg`）。**MIR 专属运行时崩溃 10 → 0**（red-line 达标）：闭环 `struct-field-move-test`/`struct-field-uaf-bug`/`struct-move-is-moved`/`struct-field-leak`/`double-move-same-source`/`clone-reset-is-moved`/`move-clone-liveness`/`prologue-buf-leak`/`cross-fn-str-return-dfree`（**全部输出与 legacy 逐字节一致**）。仅剩 `struct-field-shallow-copy-bug` 为语义分歧（需 `%vec` 深克隆），已非崩溃。改动触及 `emitSetField`/`emitMove`/`emitEntry`/`insertDrops`/`Validate`/`lowerStructLit`，以全量递归重扫守护零回归。

#### 13.3.4 主导深层 blocker：顶层 main 合成结构性 bug（**已闭环 #17，2026-09-11 第四轮**）
**现象**：顶层 `block []u32 = [...]`、`v []str`（无初始化）等顶层容器字面量/变量，在 `synthesizeMainForTopLevel` 合成的 `main` 中未被物化为真实 slice/array，而是打成 `const dst=:void`（或 `fs.file` 垃圾 structlit），致其后的索引/方法调用操作数 void 化 → `has_no_slot`/`index_slot`/`receiver-切片` 共 **15 个**测试失败。**这是单点修复即可解锁最多测试的深层 bug**：正确物化顶层容器字面量/变量进合成 main（对齐 legacy 的顶层语句 lowering），15 个测试应一举通过。
**状态（已闭环）**：`#17` 修复后 15 个家族全归零；committed 基线 244 → 280 PASS（净 +36，含 #18 `emitBitwise` 伴生修复）；全量递归重扫 +14 个既有通过测试零回归。风险：改动 `synthesizeMainForTopLevel` 影响所有顶层语句程序，已用全量递归重扫守护。

#### 13.3.5 预存失败与 std/test 缺陷警示（2026-09-11 第五轮）
> 现场警示：**当前项目开发中，`std`、tests 都可能不合理/不正确**，识别失败时要先怀疑 std/test 而非 MIR 后端。

第五轮对 418 全量做了 **legacy-parity 二分**（`NOLANG_MIR=0` vs `=3`），得出关键二分：

| 二分 | 数量 | 含义 |
|---|---|---|
| **MIR 专属 gap**（legacy 过、MIR=3 挂） | **41** | 真正的 MIR codegen 缺口（§13.3.1） |
| 预存失败（两模式都挂） | **92** | legacy 也失败，**非 MIR 引入** |

**预存失败（92）的典型根因**（均不 blocking MIR 门禁，应作为通用 bug 在双路径闭环）：
- **std 源码缺陷**：`bug15-read-dowhile-copyfile.no` → `std/process.no:line 637` 含非法 token `®`（std 源损坏）。
- **溢出默认 checker 硬错**（无 `#{overflow=wrap}`/`?=`）：`match.no`/`test-all.no`/`test-database-sql.no`/`test-ffi-mysql.no`/`test-ffi-sqlite.no`/`test-sha256-simplified.no`/`test-sse.no`/`test-str-concat.no`/`test-tagged-enum.no`/`test_char_*.no`/`tmp-arr-test.no`/`tmp-words-test.no` 等约 17 个——两模式都因 `ValidateUnhandledOverflow` 拒，与 MIR 无关；部分属 test 漏注解（test 自身缺陷）。
- **std 内建经 MIR 路径触发的"no result slot"**：`test-basic.no`（`bigint.gcd` 内部 `with-len`）、`test-map.no`/`test-parse-min.no`/`test-minimal-option-str.no` 等——这些 test 在 `MIR=0` 同样 rc=1，属 std 函数/`with-len` 类型推导缺口经 MIR 路径更早暴露，**非 MIR 引入的回归**。
- **非确定/指针输出 test**：`test-slot-rebind.no` 等打印指针类值，无法逐字节对照（test 自身缺陷，§12 #20 已解除其 codegen 门禁）。
- **std 缺陷引发的 `link`/crash**：`test-x25519-keypair-diff.no`（链接缺符号）、`option.no`/`test-option-index.no` 等（两模式都挂）。

**结论**：推进 MIR 覆盖率时，先把 92 个预存失败从"MIR gap"清单剔除，聚焦 41 个真 MIR 专属缺口；对其中疑似 std/test 缺陷的，先核对 `MIR=0` 是否通过再判定是否 MIR 责任。

---

#### 13.3.10 第三十一轮（2026-09-15）全量扫描 + 扫描器工具缺陷修复 + 抖动甄别

**口径**：`./bin/no`（绝不用仓库根 `no`），全量递归 `tests/**/*.no` = **421**（本轮 421 全部纳入统计，`test-for2.no` 归入 `HANG` 而非剔除）；基线 `NOLANG_MIR=2`，被测 `NOLANG_MIR=3`；`scripts/mir_sweep_fast.sh`，8-worker、`MIR_SWEEP_TIMEOUT=120`。

**结果**：`MATCH=367`、`DIVERGE=0`、`MIR_GAP=0`、`CERR=39`、`CRASH=14`、`HANG=1`（合计 421）。

##### ① 扫描器超时工具缺陷（第三十轮踩到，本轮修好）

- **缺陷**：`run_to` 原实现是 `perl -e 'alarm shift; exec @ARGV'`。`alarm` 到期只终止 **perl 自身**，而被 `exec` 掉的进程（及其再 spawn 的子进程：`clang`、被编译出来的程序、`no run` 的孙进程）不会被回收。第三十轮遇到 `test-for2.no`（两模式都真挂死）时，`MIR=2` 那一侧的子进程逃逸出 alarm，`sweep_one` 永久阻塞；`xargs -P 4` 因仍有空位而不退出 → 整轮扫描卡死，只能人工 `pkill -9 -f 'test-for[2]'` 收尾，且汇总里 `HANG` 被计成 0（假阴性）。
- **修法**：改用 `setpgid`（macOS 无 `setsid`）+ **杀整个进程组**：
  ```perl
  my $pid = fork(); exit 127 unless defined $pid;
  if ($pid == 0) { POSIX::setpgid(0, 0); exec @ARGV; exit 127; }
  $SIG{ALRM} = sub { $timedout = 1; kill("-KILL", $pid); kill("KILL", $pid); };
  alarm $t; waitpid($pid, 0); alarm 0;
  exit 142 if $timedout;
  exit(($st & 127) ? 128 + ($st & 127) : ($st >> 8));   # 保留信号语义
  ```
  子进程被放进自己的进程组（pgid = 子 pid），`kill("-KILL", $pid)` 即 `kill -KILL -<pgid>` 清掉整棵子树。同时**保留退出码语义**（信号退化为 `128+sig`），否则 `CRASH` 桶会把 SIGSEGV/SIGBUS 误判成编译错误。
- **验证**：单元验证（孙进程挂死 → 2s 返回 142、`pgrep` 无残留；`exit 7`→7、`exit 0`→0、`SIGSEGV`→139、`/bin/echo`→0）+ 真实用例（`tests/test-for2.no` 两模式均 25s 干净返回 142，**无残留进程**）。
- **顺带修的两处分类缺陷**：`grep -c "^HANG"` 会把 `HANG_LEGACY` 一起数进去 → 改为 `"^HANG "`；新增 `HANG_LEGACY` 桶——**基线自身超时**时不再因 `rc2≠0 && rc3=0` 落进 `MIRBETTER`（把"挂死"报成"MIR 更好"是最危险的假阳性）。

##### ② 抖动甄别：第三十轮与第三十一轮的配方差**全部**是抖动，不是修复

两轮 `MATCH` 都是 367，但分类位互换。逐文件复跑（每文件 5×`MIR=2` + 3×`MIR=3`）后确认只有两个文件不稳定：

| 文件 | 现象 | 证据 | 结论 |
|---|---|---|---|
| `tests/test-diff-debug.no` | 第三十轮标 `MIR_GAP`，第三十一轮标 `CRASH` | `MIR=3`：DP 表恒全 0（**6/6**）、rc=1（**0/6** 成功）；`MIR=2`：DP 表恒正确（**6/6**，`1 1 1 0`）、rc=0 仅 **5/6**（1 次 `segfault`） | **真实 MIR gap**，但 legacy 基线自身有 ~1/6 崩溃率 → 扫描标签随机落 `MIR_GAP` 或 `CRASH` |
| `tests/test-quant-all1.no` | 第三十轮标 `DIVERGE`，第三十一轮标 `MATCH` | 两模式输出第 13 行 `2 : CHAR 8412289056` vs `…072`（`0x1f5695820`/`0x1f5695830`，**堆地址**，相差 0x10）；`MIR=2` 自身 6 次里两种值都出现，`MIR=3` 6 次恒为 `…072` | **test 自身缺陷**（`regexp` 结构体的 `arg1[128]i64` 未初始化槽被打印成地址），DIVERGE 标签是抖动，**不是 MIR 语义分歧** |

**抖动校正后的真实态：`MIR 专属 gap = 1`（`test-diff-debug.no`）、`DIVERGE = 0`。** 另对 `CRASH` 桶 14 个逐个复跑（5×legacy + 3×MIR），除 `test-diff-debug.no` 外 **13 个全是 0/5、0/3 的确定性双失败**，即 `CRASH` 桶名实相符。
**方法论结论：`MIR_GAP` 与 `DIVERGE` 这类"差一位"的指标必须在**标签不稳定**时用逐文件重复采样定真值；单次扫描的配方变化不足以判断修复效果——尤其当 legacy 基线自己就是 flaky 的时候（"基线过、MIR 挂"这句话只有在基线稳定通过时才成立）。**

##### ③ `test-diff-debug.no` 的症候定位（未闭环，属两模式共有深层缺陷）

- **形状**：`diff-split-lines(c, lines)` 经具名出参填 `[]str`；`diff-engine-lcs(a, b) (ops []diff-op)` 跑 LCS DP（`dp []i64 = with-len(total)`，内层是三条 `cond -> dp[...] = ...` 守卫式赋值）；随后按 DP 回溯构建 `ops` 并打印。
- **MIR=3 的病征**：DP 表恒全 0 → `a[ai].compare(b[aj])` 对**相等**元素（`'c'`vs`'c'`、`'a'`vs`'a'`）返回非 0，而 legacy 健康时返回 0；表全 0 又使回溯走另一条分支、最终在 `ops[1]` 上 `bus error`。
- **内层插桩结论**（`/tmp/bisectE.no`，往内层循环尾追加 `eprint('IT ai={ai} aj={aj} c={cc} d={dd}')`）：MIR=3 迭代 **9** 次（3×3，正确），legacy 迭代 **18** 次且第一轮 `(2,2)`/`(0,0)` 的 `c` 也是错的、第二轮才对——**legacy 侧同一程序也有异常**（同一 (ai,aj) 上 compare 结果在两次访问间从 `-1` 变 `0`，说明被比较的 str 内容在中途变化）。两模式的 `dp` 写入本身可执行（把守卫换成无条件 `dp[...] = 7` 后表正确显示 7）。
- **状态**：**未闭环**。已排除"DP 循环没跑""索引步长错""守卫赋值不执行"三种解释（分别用无条件写入、独立复现 `rep5`/`rep6` 验证）。剩余嫌疑集中在 `[]str` 经具名出参传播后的**字符串生命周期/别名**（legacy 也不稳定），属 §13.3.5 已记录的"两模式共有深层缺陷"家族。下一轮从这里入手；注意它是**唯一**的真实 gap。

##### ④ 本轮副产品：从该测试的最小复现中逼出的真 bug #63

为定位上面的病征做了 `rep5`→`rep9` 一系列最小复现，其中 `rep7`/`rep8`（`lcs(a, b, ops)` 三个实参形态）暴露了**与 `test-diff-debug` 无关**的 `emitCallBody` 变参误判（见 §12 目录 #63）——该形状语料里没有，任何轮次的 gap 计数都看不到它。`rep9`（struct 出参 + DP + `push`）则给出 `MIR=3` `bus error` 而 legacy 打印出垃圾 `kind`（`122/48/10`）的复现，佐证 ③ 的"两模式共有缺陷"判断。

##### ⑤ 与 `#63` 相关的回归兜底（本轮实操）

第一版 #63 修法（无条件扣减出参实参）在紧随其后的全量扫描里立刻暴露 `MIR_GAP 0 → 3`（`test-number-generic.no`、`test-number.no` 回归），改用 `cf.Variadic` 判据后回落到 `MATCH 367`、`MIR_GAP 0`。**这是"小改判据也必须全量重扫"的实例**：受影响的是变参展开路径，而这两个测试与本轮正在看的 `test-diff-debug` 毫无关系。

---

#### 13.3.11 第三十七轮（2026-09-16）权威全量扫描：按 §15 剩余清单顺序推进

**口径**：`./bin/no`（绝不用仓库根 `no`），全量递归 `tests/**/*.no` = **421**；基线 `NOLANG_MIR=2`，被测 `NOLANG_MIR=3`；`scripts/mir_sweep_fast.sh`，8-worker、`MIR_SWEEP_TIMEOUT=100`。

**结果**：`MATCH=386`（≈91.7%）、`DIVERGE=1`、`MIR_GAP=0`、`MIRBETTER=0`、`CERR=19`、`CRASH=11`、`HANG=4`（合计 421）。

**与上一轮（第三十六轮）对比**：MATCH 381→**386**（+5）、DIVERGE 1→1、MIR_GAP 0→0、两模式都失败 35→**30**（−5）、HANG 4→4。**本轮净增的 5 个 MATCH 全部来自"两模式都失败 → 两模式都成功"**：`tests/tmp-arr-half1.no`（#67）、`tests/test-slice-heavy.no`（#68）、`tests/test-aes-enc.no`（#70）、`tests/test-http-rest.no`、`tests/test-net-http.no`（#69 + #70）。

##### ① 扫描器分类缺陷（本轮修好，影响此前所有轮次的口径）

`scripts/mir_sweep_fast.sh` 里 CERR/CRASH 的判別写的是

```sh
grep -q "compilation error\|Undefined symbols\|ld: \|cannot find\|error: linker\|undefined reference" "$err3"
```

**BSD grep 的 BRE 不把 `\|` 当"或"**（它匹配字面的 `|`），所以这个 `grep` **永不命中**，`CERR` 恒为 0，所有"两模式都失败"的测试一律落进 `CRASH`。实测：本轮同一次扫描，用 `grep -E "...|..."` 重新分类即得 `CERR 26 + CRASH 7`（与第三十六轮的 28/7 形状吻合）。**修法**：改用 `grep -E` + 裸 `|`。**影响**：第三十二~三十六轮文档里的 CERR/CRASH 拆分不可信（两者之和才可比）。

##### ② 本轮确认的"两模式共有"根因（非 MIR 专属）

- **`%addopt.final` 未解包（legacy 报错，阻塞 6+ 测试）**：`test-tls.no`、`test-json.no`、`test-tls13-crypto.no`、`test-http-rest.no`、`test-net-http.no`、`test-https-server.no` 在 **legacy** 报 `opt: error: '%addopt.final.N' defined with type '%option = type { i64, i64 }' but expected 'i64'`——一条 `#{overflow}` 算术的结果仍是 `option`，却被存进 i64 槽位/参与 `icmp`。这是**前端/legacy 共有**的问题（MIR 侧已由 §12 #15 的 sink 解包覆盖），修它可同时解锁两后端，是下一轮性价比最高的一项。
- **`test-tls-prf-only.no` / `test-tls-part3.no`**：MIR=3 报 `call void @prf(..., i64 %lv215, ...)` 里 `'%lv215' defined with type '%vec'` —— 传给 `prf` 的第 4 个（i64）形参的是个 `[]byte`。属 MIR 的实参编组问题。
- **`test-json.no`**：MIR=3 报 `call void @str_slice(%str-long*, i64, i64 %lv186, ...)` 里 `'%lv186' defined with type '%str-long'` —— `s.slice(i, j)` 的第二个实参（`i64`）传成了 str。与上一条同族。
- **`tests/i.no` 是负测试**（源码注释即写 `; compilation error: i16 'a' has no method 'len'`），其 CERR 是期望行为——**§15 清单里的 #7「`str-len` 接收者 i64」不是真工作项**，勿再投入。

##### ③ 本轮被证伪的判读

§15 清单第 1 项「定宽数组 GEP 类型不匹配，阻塞 6 个 crypto/JSON 测试」经本轮复核**至少在 `test-aes-enc.no` 上是误判**：真因是 `std/crypto/aes.no` 的 `ek[ek] = aes-key-expand(key)` 源码笔误（§12 #70）。`elemAddr` 自身的定宽数组分支（`getelementptr [N x T], [N x T]* slot, i64 0, i64 idx`）是对的。**其余 5 个是否同源待逐个复核**——判这类错时必须先回到 std/前端源码确认形状，再动 codegen。

##### ④ 方法论要点（本轮新增）

- **改动标志位作用域前先问"它是否对整棵子树可见"**：`inPrintArgs` 故意对整棵实参子树可见（字符串插值诊断需要），拿它当"只作用于最外层实参"的包裹开关就会把嵌套调用也改造掉，一次引入 13 个 DIVERGE。需要不同可见范围就**另开一个标志**（`wrapPrintArgs`），不要复用。
- **未触发单态化的"合成调用"救不了 legacy**：为了让 legacy 也能 `print(vec)`，试过在 transpiler 里合成一个 dummy `x.to-str()` 调用来触发 `[]t.to-str` 单态化；名字确实被解析成 `_xi64.to-str`，但**没有被真实调用点引用的单态化产物会在 codegen 之前被丢掉**，legacy 仍报 undefined。已回退。
- **`git worktree` + 基线二进制 + 逐文件 A/B（`/tmp/cmp2.sh`）是判定"是否本轮引入"最快的手艺**：编一份 HEAD 二进制，对可疑文件同时跑 base/new × MIR=2/3，比 rc 与输出。它比"重跑一次全量扫描看数字变了没"快一个数量级，且能区分"抖动"与"真回归"（本轮 5 个 DIVERGE 在 base 下输出恒定、new 下每次都不同 → 真回归）。

#### 13.3.12 第三十八轮（2026-09-16）权威全量扫描：语料分类 + 默认路径切换

**口径**：`./bin/no`，全量递归 `tests/**/*.no` = **421**；基线 `NOLANG_MIR=2`，被测 `NOLANG_MIR=3`；`scripts/mir_sweep_fast.sh`，8-worker、`MIR_SWEEP_TIMEOUT=90`。另对 `test/`（40）与 `example/`（11）跑同一对口径做交叉验证。

**结果**：`MATCH=387`（≈91.9%）、`DIVERGE=1`、`MIR_GAP=0`、`MIRBETTER=0`、`CERR=18`、`CRASH=11`、`HANG=4`。**与上一轮（第三十七轮）对比**：MATCH 386→**387**（+1，`test-regex-literal.no` 由 CERR 转 MATCH）、两模式都失败 30→**29**。`test/` 与 `example/`：`GAP=0`、`DIVERGE=0`、`HANG=0`。

**默认口径（不设 `NOLANG_MIR`）实测：`388/421`（92.2%）通过，`FAIL=29`、`HANG=4`。**

##### ① 正则字面量：MIR 的 codegen 期脱糖缺口（#71）

`tests/test-regex-literal.no` 在 **两个后端都失败**，但 MIR 报的是 `unsupported construct (string interpolation)`，看起来像 §15 #6「字符串插值」的工作项。实际根因完全不同：

- `/pattern/flags` 在 **legacy 是 codegen 期**由 AST 改写完成脱糖（`build/llvm/expr.go`：把 `*parser.RegexLiteral` 换成 `regexp-compile('pattern')` 调用）。
- **MIR 没有 codegen 期 AST** —— lowering 是最后一个还能看到这个形状的地方。`hir.KRegexLit` 在 `src/mir/` 下**一处都没处理**（`grep KRegexLit src/mir/*.go` 为空），于是字面量落进 `lowerExpr` 的默认分支，**根本不产生值**。
- 后果的**表象具有强误导性**：`re2 = /hello/gi` 这条 `let` 从未绑定局部 `re2`，后面 `print('re2.pattern = {re2-pattern}')` 就报 `unresolved format field re2-pattern`。**看到"未解析格式字段"先别急着查插值管线，要先确认被插值的那个绑定本身有没有建立。**

**修法**：新增 `lowerRegexLit`（`hir2mir.go`），把 `/pattern/flags` 下沉为 `regexp-compile('pattern')` 调用（`enqueueCallee` + `EmitCallMulti`，结果型别 `regexp`）。flags 仍不参与语义——与 legacy 一致（legacy 也只是把 flags 挂在节点上不消费），保持逐字节兼容。**结果**：`test-regex-literal.no` 两模式 `rc=0` 且输出逐字节一致（`matched = 0` / `re2.pattern = hello`）。

##### ② `lowerFormatField` 的 `default:` 把结构体当整数（#72）

修完 #71 暴露出来：`print('x = {re2}')`（`re2` 是 `regexp` 结构体）走 `lowerFormatField` 的 `default:` 分支，而那个分支的隐含前提是"不是特判类型 ⇒ 整数"，于是把 `%regexp_regexp` 直接传给 `fmt-int`，产出非法 IR：

```
error: '%lv17' defined with type '%regexp_regexp = type {...}' but expected 'i64'
  call void @fmt_int(i64 %lv17, %str-long* %carg19, %str-long* %cres20)
```

**修法**：`default:` 改成显式的「不可渲染类型」拒绝分支，只有 `isIntegerMIRType(raw)`（含 `bool`，见该处注释）才走 `fmt-int`/`fmt-uint`。**原则**：`switch` 的 `default:` 若被当成"剩下的都是 X"，就在改动前先把 X 写成判据——否则每加一种新类型都会静默落进来。

##### ③ 「两模式都失败」的精确分类（29 个）

本轮逐文件复核（`/tmp/d30_all.txt`），结论是**绝大多数不是 MIR 的工作项**：

| 类别 | 数量 | 文件 | 归属 |
| --- | --- | --- | --- |
| **测试源陈旧（API/语法漂移）** | **14** | 8 个缺 `#{overflow=wrap}` 而触发 `ValidateUnhandledOverflow` 硬错（`test-all`/`test-database-sql`/`test-ffi-mysql`/`test-ffi-sqlite`/`test-sha256-simplified`/`test-sse`/`test-tagged-enum`/`tmp-words-test`）；2 个 `#{index-out=...}` 与语句同行（`test-safe-index`/`test-safe-index-containers`）；`test-json.no` 调 `p.stringify(root, out-buf, 0)` 而现行签名是 `json-pool.stringify(node-idx) -> str`；`test-tls-prf-only.no`/`test-tls-part3.no` 用 6 参 `prf(secret,4,label,seed,3,32)` 而现行 4 参 `prf(secret,label,seed,out-len)`；`test-x25519-fe-diag.no` 引用已不存在的 std 函数 | **非 MIR**：测试要与语言/std 对齐（**修测试**） |
| **两模式同崩/同挂的 std 缺陷** | **8** | `test-basic`/`test-std-hash`/`test-std-net-ext`/`test-tls-debug`/`test-tls-part2`/`test-tls`/`test-txt`/`vec` | **非 MIR**：std/legacy 共有 |
| **legacy 专属编译缺陷** | **3** | `test-http3`/`test-https-server`/`test-tls13-crypto`：legacy 报 `%addopt.final` 未解包。**MIR=3 已能编译过去**，只是随后运行时 segfault | **非 MIR**：legacy 停用后消失 |
| **MIR 侧真实缺口** | **3** | `test-strconv.no`（`%option_f64` 未能 coerce 到 `double`）、`test-std-new.no`（`%str-long` 送进 `inttoptr i64`）、`mem-safety/nested-container-clone.no`（`%vec` 参与 `icmp`） | **MIR 工作项** |
| **负测试（期望失败）** | **1** | `tests/i.no`（源码注释即 `; compilation error: i16 'a' has no method 'len'`） | **非 MIR**：应为期望行为 |

> 合计 14 + 8 + 3 + 3 + 1 = **29**，与 `CERR 18 + CRASH 11` 一致。**注意 `test-json.no` 的 MIR 错误（`str_slice` 第二个实参拿到 `%str-long`）不是独立的 MIR 缺口**——它正是"测试调了旧签名的 `stringify`、于是 `out-n` 其实是 `str`"的直接后果；测试修好后 MIR 侧无需任何改动。**同理 `test-std-new.no`/`test-strconv.no` 同时具备 legacy `%addopt.final` 与 MIR 真缺口两种身份**，此处按"MIR 侧是否还有活要干"归入 MIR。
>
> 另有 **4 个 HANG**（`test-for2.no`、`test-parse-min.no`、`mem-safety/test-json-parse-option.no`、`mem-safety/test-json-nested-match.no`）**不计入上面 29**：3 个是 json parse 的两模式死循环，属 std 缺陷；`print('start')` 的输出被 SIGKILL 吞掉，**不能靠 stdout 判断挂在哪里**。

> **HANG 的一处口径陷阱（必记）**：`mir_sweep_fast.sh` 先判 `rc3` 再判 `rc2`，所以 `HANG_LEGACY=0` **并不**意味着"基线没挂"——只要两者都挂，它就记成 `HANG`。本轮 3 个 json/parse 测试即属此类：**两模式都挂死**，不是 MIR 专属。要区分必须单独跑基线。

##### ④ 交叉扫描：语料外无缺口

对 `test/`（40 文件，多为 `test/std/*.no` 这类无 `main` 的模块文件，单独 `run` 必然失败）与 `example/`（11）跑同口径：**`GAP=0`、`DIVERGE=0`、`HANG=0`**，全部失败都是「两模式都失败」的既有失败。这是切换默认路径的前置条件。

##### ⑤ 默认路径切换（#73）与它的量化依据

`src/build/transpiler.go` 中 `NOLANG_MIR` 未设置时默认值由 legacy（不进入 MIR 分支）改为 **`"3"`**（MIR-only，无回退）；`NOLANG_MIR=0` 仍可显式选 legacy。

**依据**：全量语料上默认口径通过 **388/421**，与旧默认口径（`MIR=2`，`MATCH 387 + DIVERGE 1 = 388`）**完全一致** —— `MIR_GAP=0` 的直接推论是"没有任何 legacy 能过而 MIR 不能过的用例"，因此切换默认**不改变任何测试的成败**，只是把"用 legacy 掩盖 MIR 缺口"这条路径关掉，让新暴露的缺口立刻可见。加上 `test/`+`example/` 的 `GAP=0` 交叉验证，语料内外的风险都已被测量过。

**一个顺带得到的强证据**：`NOLANG_MIR=0 ./bin/no run <最简单的 print(1+2) 程序>` **编译失败**：

```
opt: error: '%addopt.final.5254' defined with type '%option = type { i64, i64 }' but expected 'i64'
  store i64 %addopt.final.5254, ptr %fmtval.5262
```

即 legacy 对「默认溢出模式下的整数算术作为 print 实参」这一**极常见形状**是坏的。§13.3.11 ② 记的「`%addopt.final` 阻塞 6+ 测试」不止是那 6 个测试的问题，而是 legacy 的一条主干缺陷。**MIR 在这条路径上是正确的**，这从另一个方向支持了默认切换。

##### ⑥ 单测回归处置（#76）

切换默认后 `go test ./build/` 多出 4 个失败（`TestTransitiveImportLLVM` / `...ThreeLevels` / `...Diamond` / `...EntryUsesDeepFn`）。**不是功能回归**：手工搭同一模块图，默认口径与 `NOLANG_MIR=0` 都正确输出 `142`，程序可跑。差异只在**符号拼写**——legacy 原样输出 `@middle-fn`，MIR 净化为 `@middle_fn`（LLVM 无引号标识符只能保证 `[-a-zA-Z$._0-9]`，而 MIR 要从任意 Nolang 标识符生成名字）。这些测试的**意图是"符号没被模块合并丢掉"（D17）**，不该被拿来钉死命名风格，故加 `irHasFunc` 同时接受两种拼写。

**核对方式**：与基线 worktree（`/tmp/no-base`，HEAD `af2e494`）逐项对比，`build` 包失败集在 `NOLANG_MIR=0`/`2`/`3` 下**完全一致**（`TestSliceMethodLenCall{,OnI64,OnStr}`、`TestProgramUsesPrintDetectsLoopAndBlockBodies`、`TestGenerateHIRMatchesGenerate`、`TestUserReadOverridesBuiltin`）——这些是既有失败，与本轮无关。

##### ⑦ 方法论要点（本轮新增）

- **"两模式都失败"必须先分类再动手**：本轮 29 个里只有约 7 个是 MIR 的事，14 个是测试源陈旧。若不分类就直接去改 codegen，会追着 legacy 的锅修 MIR。
- **看到"未解析 X"先查绑定、再查使用**：正则字面量的表象是插值报错（#71），`%addopt.final` 的表象是 bounds-check 报错，两者真因都在上游。
- **错误信息要把诊断细节带出来**：`unsupported construct (string interpolation)` 这个桶名把已经拿到手的 `unresolved format field re2-pattern` 丢掉了（#75），导致每查一次都要重新插桩。**桶名越短，调试成本越高。**
- **切换默认前先量"新旧默认的 rc=0 集合是否相同"**，而不是只量新默认的通过数——前者才是"不改变行为"的直接证据。

---

#### 13.3.13 第三十九轮（2026-09-16）：平台变体过滤缺口 + `#9/#10` 范围勘定

**口径**：同 §13.3.12（`./bin/no`，全量递归 `tests/**/*.no`，基线 `NOLANG_MIR=2`，被测 `=3`）。
**结果**：`MATCH=388`（≈92.2%）、`DIVERGE=1`、`MIR_GAP=0`、`MIRBETTER=0`、`CERR=18`、`CRASH=11`、`HANG=4`。相对第三十八轮 **MATCH 387→388（+1，即本轮新增的 `tests/test-platform-const.no`）**，其余分类**逐项不变**，零回归。

##### ① 平台变体过滤在脚本模式下失效（#77）

查 `#9/#10` 的耦合面时偶然实测到的**真 gap**（legacy 正确、MIR 错误），且此前的全量扫描完全看不到它。

**现象（宿主 arm64 macOS，脚本无显式 `main`）** — 三栏对照，第三栏是第四十轮用 pre-deletion 二进制（`git worktree add <tmp> 47b6cad`）重测的权威值：

| 用例 | legacy（`47b6cad`） | MIR=3（修复前） | MIR=3（修复后） |
| --- | --- | --- | --- |
| `#{mac-amd64}` `V = 8` 单独 | 打印 **8** | 打印 8 | **无输出**（变体被过滤 → `V` 未定义，落进 MIR 的「未解析标识符静默」通病） |
| `#{linux-amd64}` `V = 9` 单独 | 打印 **9** | 打印 9 | **无输出** |
| `#{mac-arm64} V=1` + `#{mac-amd64} V=2` | **`2`** | **`2`** | **`1`** |
| 六平台变体各持不同值 | **`600`** | **`600`**（win-amd64 的值） | **`100`** |

规律是**后出现的变体总胜出**（与宿主无关），宿主的 `mac-arm64` 反被丢弃。已排除"跑在 Rosetta 下导致 `runtime.GOARCH` 是 amd64"这一可能：`file bin/no` = arm64、`uname -m` = arm64、`go env GOARCH` = arm64。**带显式 `fn main` 时顶层 let 走注册循环，两个后端都正确过滤**（实测 `111/111`），所以缺口限定在**脚本模式的顶层内联路径**。

> ⚠️ **依据更正（第四十轮实测，重要）**：本节初稿记作"legacy 正确、MIR 错误"，**这是错的**。用 `47b6cad` worktree 编译的 legacy 二进制重测证明：**legacy 在脚本模式下同样不过滤**平台变体 —— 单独 `#{mac-amd64}` 照常打印 8（并非"正确报错"），双变体打印后出现者 2（并非 1），六变体打印 600（并非 100）。
>
> 所以 `#77` 不是"MIR 偏离了 legacy"，而是**两个后端共同的缺口，MIR 单方面把它修好了**。判据必须改用**注解的文档语义**（`docs/docs/lang/syntax.md`：平台注解决定该语句是否参与本次编译），而不是"与 legacy 逐字节一致"。这也是只有**顶层 let**（有 `fn main` 时）才天然一致的原因：那条路径两个后端都过滤。
>
> 直接后果：修复后 `tests/test-platform-const.no` 在 MIR 与 legacy 之间**故意分叉**（`100` vs `600`）。该分叉在 `tests/golden/legacy-baseline.tsv` 的比对里表现为一个 `DIVERGE`（两后端都 rc=0 但输出不同）—— 是**有意修复的签名**，不是回归。**教训：把"与 legacy 一致"当作正确性判据之前，先确认 legacy 真的做对了这件事。**

**根因**：`nodeMatchesPlatform`（`src/mir/platform.go`）只在顶层**注册循环**里被调用——`hir2mir.go` 的 `KFuncDef`（`l.funcNames[name]=id` 之前）与 `KLet`（`l.globals` 之前）各一处。脚本路径的 `synthesizeMainForTopLevel`（`hir2mir.go` 起于 `func (l *lowerer) synthesizeMainForTopLevel`）**没有这个检查**，于是被注册循环过滤掉的变体**仍然被内联**进合成的 `main`，其错误平台的值覆盖了匹配变体注册的全局。这解释了全部四种现象：单个不匹配变体被内联成脚本局部（所以"照编不误"）；多变体时匹配者成为全局、不匹配者成为内联局部，后者胜出；两个都不匹配时按源序后者胜出。

**修法**：在 `synthesizeMainForTopLevel` 的 `for _, id := range pkg.Top` 循环入口（`n == nil` 判空之后、`switch n.Kind` 之前）补同一个检查：

```go
if !nodeMatchesPlatform(pkg, id) {
    continue
}
```

放在循环入口而非 `case hir.KLet` 内，是为了让所有顶层节点种类（`KLet`/`KStructLit`/表达式语句…）共用同一判据。无注解的节点 `nodeMatchesPlatform` 返回 `true`，std 预置节点不受影响。

**验证**：新增 `tests/test-platform-const.no`（六平台变体各持不同值）作为常驻回归。用修复前二进制（`47b6cad` worktree）实测该用例得 MIR **`600`** —— 同一份 legacy 也是 **`600`**（见上方更正），所以该用例对"**过滤是否发生**"有判别力，对"两后端是否一致"则没有（legacy 本来就不过滤）。`src/mir/platform_test.go`（第四十轮新增）把这条规律固化为 7 个 Go 单测（含穷尽 `package.PlatformKeys` 全表、`fn main` 顶层路径、以及钉住"函数体内不过滤"这一两后端共有的既有限制），并**实测了判别力**：把 `synthesizeMainForTopLevel` 的检查改成 `if false && !nodeMatchesPlatform(...)` 后，恰好两个脚本模式测试失败，其余不受影响。

**为什么全量扫描抓不到（本轮最重要的一条方法论）**：`tests/` 里**没有任何**用例使用平台注解（`grep -rl 'mac-arm64\|linux-amd64' tests/` 为空），而 `std/fs.no` 的 `mac-amd64` 与 `mac-arm64` 常量**值恰好相同**（`O-CREAT` 512/512、`O-TRUNC` 1024/1024），所以即使错选也不产生可观测差异；`std/process.no` 的注解挂在**函数**上（走注册循环，本来就对）。⇒ **`MIR_GAP=0` 只意味着"语料区分不出"，不等于"两后端等价"。判定一个 gap 家族是否真清零，要问"语料里有没有能区分它的用例"。**

##### ② `#9/#10` 的范围勘定：`src/build/llvm/` 是 5 万行不是 60KB

§15 旧记的"约 60KB"**低估约 30 倍**。实测：**48 个 `.go` 文件 / 50,006 行 / 2.0MB**
= **15 个生产文件（41,806 行）** + **33 个测试文件（8,200 行 / 188 个 `func Test`）**。
生产文件：`stmt.go` 11,463 / `expr.go` 8,701 / `call.go` 6,022 / `call_stdlib.go` 4,925 / `generator.go` 3,873 / `decl.go` 1,279 / `coro.go` 1,242 / `gen_hir.go` 1,036 / `reachability.go` 780 / `slice_view.go` 730 / `dataflow.go` 617 / `types.go` 599 / `strchar_at.go` 416 / `clone_slice.go` 86 / `dfstat_tmp.go` 37。
（`src/build/wasm/`、`src/build/js/` 是另外两个后端，**不在本项内**。）

**「移除 strangler-fig 回退」的精确含义**：目前实际并存**四条**代码生成路径，不止两条。
`NOLANG_MIR=3`（默认）→MIR；`=2`→MIR+失败即回退；`=0`→legacy **HIR** 后端（`transpiler.go` `GenerateHIR` 的 else 分支）；`NOLANG_HIR=0`→legacy **AST** 后端（更旧的 surface-AST 路径 `Generate(merged)`）。另有 `NOLANG_MIR=1`：转储后**总是**回退。
`GenerateHIR` 调用点共 **8 处**，全部由 `emitMIR(hirPkg, allowFallback bool)` 的布尔控制——该参数本身就是 strangler-fig；`hasFatalLowerDiag`/`firstFatalLowerDiag` 也只为"要不要回退"而存在。

**跨包耦合面极小（本项可行的关键）**：`go list -deps ./mir/` **不含** `nolang/build/llvm`，MIR 包完全独立。全仓 import 该包仅 4 处：`transpiler.go`（`llvm.NewGenerator`/`llvm.Generator`）、`src/hir/golden_test.go`（`llvm.FilterByPlatform`，仅测试）、`src/cmd/tmp_test_and_i8/main.go`、`tmp/bug13-dump.go`。
需先"接管"的非 codegen 物只有两项：4 个 setter（`SetTargetPlatform`/`SetNoBoundsCheck`/`SetMainFileNames`/`SetGlobalVarOwners`）与 `CodegenErrors()` 错误通道（`transpiler.go` 读、`build/llvm/generator.go` 写，**MIR 路径也读它**）。
**构建系统实际零改动**：`Makefile` 的 `GO_SOURCES`/`NO_SOURCES` 都是 `find` 通配，删文件自动适配（§15 里"更新构建系统"这条偏保守）。

##### ③ 188 个 legacy 测试的覆盖盘点（删除前必读）

| 类别 | 数量 | 内容 | 处置 |
| --- | --- | --- | --- |
| **A · legacy 内部机制** | ~100 | `dataflow_test.go` 67（CFG 数据流框架自身：bitset/effect/move fact/out-bind/init fact）、`gen_hir_test.go` 14（legacy HIR→IR 文本断言，含**既有失败** `TestGenerateHIRMatchesGenerate`）、`reachability_test.go` 2、`expr_test.go` 3、`bug11/bug13/bug15/bug16/bug_dom` 8、`chain`/`callvec_store` 等 | **随包删除**。它们断言 legacy 的 IR 文本或框架内部结构，不描述语言语义。MIR 的对应物是 `src/mir/analysis.go` + `mir_test.go` 的 4 个分析测试 |
| **B · 语言/std 语义知识** | ~20 | `generator_test.go` 9（**平台变体解析 / datalayout / triple / 各平台声明**）、`decl_test.go` 4（`TestStatLayoutForAllPlatforms`、`TestOpenWriteFlagsForAllPlatforms`）、`overflow_test.go` 4（语句级 `#{overflow=...}` 读取）、`chacha`/`poly1305` 2 | **迁移重点**。MIR 有独立镜像表（`src/mir/platform.go`、`builtin_call.go`）但**零测试**——① 的缺口正出自这里：`TestPlatformVariantResolution`/`TestPlatformVariantMultiKey` 只测 legacy |
| **C · 历史 bug 回归（IR 文本断言）** | ~68 | `struct_*` 33（字段访问/数组字段/深拷贝/push/clear-truncate/retinit）、`str_slice_regression` 5、`str_index_regression` 4、`cross_module_str_stride` 7、`callvec_store` 6、`recursive_match_it` 3、`for_loop_match` 4、`match_str_clone` 2、`arr_byte_offset_slice` 2、`d23_option_struct_field` 1、`user_func_overrides` 3 等 | **无法机械迁移**（全部断言 legacy IR 字符串）。其**语义意图**由 `tests/*.no`（371 个运行比对用例）承载；建议增量策略：删后若出现回归再补 |

**当前不对称**：legacy 188 个单测 vs MIR **6 个**（`src/mir/mir_test.go` 209 行）→ **第四十轮起为 13 个**（新增 `src/mir/platform_test.go` 的 7 个测试函数，覆盖分类 B 的**平台变体解析**；含 3 个子用例共 15 个用例，详见 §13.3.14 ①）。这是第三优先级"MIR 单测扩展"的量化依据。

##### ④ 删 legacy 会失去 Oracle —— 必须先冻结基线快照

`MATCH`/`DIVERGE`/`MIR_GAP` 这套判据的**全部**信息都来自 `NOLANG_MIR=0` 这个对照后端。删掉它之后：
- `DIVERGE`（两模式 rc=0 但输出不同）**永久无法再检测**——而这正是本文件反复依赖的核心信号；
- `NOLANG_MIR=0` 这条排障退路消失。

**对策（删除前必做）**：把当前语料在 `NOLANG_MIR=0` 下的 stdout/rc 冻结成 golden 快照并入库，后续用它与新 MIR 输出做同口径 diff。否则第 ① 类"两后端分叉"的 bug 将不再有任何自动发现手段。

##### ⑤ 方法论要点（本轮新增）

- **过滤类逻辑要同时问"注册侧"和"内联侧"**：平台过滤只在注册循环里做了，脚本内联路径漏了。**任何"跳过某节点"的检查，都要确认所有会物化该节点的路径都过了同一个检查。**
- **`MIR_GAP=0` ≠ 两后端等价**，只在"语料能区分"的范围内成立。要给结论加"语料是否具备区分能力"这一前提（① 就是反例）。
- **排除环境假设要比推理快**：怀疑"宿主架构被 Rosetta 改写"只需 `file bin/no` + `uname -m` 两条命令，省掉一整轮错误归因。
- **回归用例要用修复前的二进制实测它确实失败**：`git worktree add <tmp> HEAD` + 旧二进制跑新用例，才能证明用例有判别力而不是恒真。
- **"未定义标识符静默"是既有的、更普遍的 MIR 宽松策略**，不是平台过滤特有：`print(ZZZ)`（真正未定义）在 MIR 下同样 rc=0 且输出空行，legacy 则报 opt 错。修复后平台场景与该通病行为一致；这条通病单独成项，不在本轮范围。

##### ⑥ 删除落地（第三十九·续轮）：strangler-fig 移除 + `src/build/llvm/` 删除 + 基线对账

按 ② 的范围勘定与 ④ 的 oracle 对策执行，全部改动**未提交**（工作区状态见文末）。

**改了什么（5 个文件 + 1 个新脚本 + 2 个新基线）：**

| 项 | 内容 |
| --- | --- |
| `src/build/transpiler.go` | 删 `build/llvm` import、`Transpiler.llvmGenerator` 字段与其构造；`emitMIR` 签名由 `(hirPkg, allowFallback bool)` 收为 `(hirPkg)`；删全部 `if allowFallback { return t.llvmGenerator.GenerateHIR(...) }` 分支、`LastMIREmitted`、`hasFatalLowerDiag`；`NOLANG_MIR=0`/`=2` 改为**明确报错**，`=1` 仅 dump。保留 `targetGoos/targetGoarch`（前端合并阶段 `checker.MatchesTargetPlatform` 仍需要） |
| `src/cmd/no/main.go` | 删 `no run` 的 strangler-fig 重试段（原"legacy 编译失败 → 重编重跑"），改为直接失败退出 |
| `src/hir/golden_test.go` | 解除对 `llvm.FilterByPlatform` 的依赖，新增 `astMatches(prog, i, goos, goarch)` 用 `prog.Sem.PlatformKeysOf` 自算（两函数均显式从 `prog` 取数，避免闭包捕获循环变量） |
| `src/mir/hir2mir.go` | ① 的 `nodeMatchesPlatform` 缺口修复（一并随本批落地） |
| `tests/test-platform-const.no` | ① 的常驻回归用例（六平台变体各持不同值） |
| `scripts/mir_golden.sh`（新） | 冻结/比对基线指纹：每文件一行 `<rc> <sha256(stdout)> <path>`。`-update` 冻结，无参则与当前默认后端比对；复用 `mir_sweep_fast.sh` 的进程组级 kill 超时 |
| `tests/golden/{legacy,mir}-baseline.tsv`（新） | 各 422 条。legacy-baseline 的 `rc=0` 有 340（失败 82，19.4%，即 ④ 说的"无参照"条目）；mir-baseline 的 `rc=0` 有 389 |
| 删除 | `git rm -r src/build/llvm/`（48 文件）+ 删 `src/cmd/tmp_test_and_i8/`（scratch，直接调 legacy 生成器，留在模块内会让 `go build ./...` 失败） |

**验证矩阵（全部通过）：**

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | rc=0 |
| `go vet ./...` | 仅 `build/wasm` 一条既有警告（`WriteByte` 签名），与本次无关 |
| `./bin/no run tests/test-platform-const.no` | `100`（默认 == `NOLANG_MIR=3`）；`=0`/`=2` 均报 `the legacy codegen backend was removed` |
| `go test ./...` | 失败集与**干净 HEAD worktree（`47b6cad`）逐条一致**：`fmt`(2) + `build`(4) + `checker`(2)。**全部为既有失败**；`TestGenerateHIRMatchesGenerate` 已随 legacy 包消失 |
| golden 比对（`mir-baseline.tsv`） | `SAME=388`、**`REGRESS=0`**、`DIVERGE=1`（`tests/mem-safety/str-concat-leak.no`，已知的二进制地址差异假阳性，非新问题）、`BOTH_FAIL=33`、`NEW=0`（388+1+33=422） |

**基线时序核对（防"冻结了错的参照"）**：`mir-baseline.tsv` 冻于 11:36，晚于 ① 的修复（11:12），且含 `tests/test-platform-const.no`，其哈希 `eea8254c…` **正是 `sha256("100\n")`** —— 即冻结的是**修复后**行为，而修复前 MIR 的 `sha256("600\n")`（`ab8e9a58…`）不在 `mir-baseline` 里。确认基线冻对了时机。

> ⚠️ **同一处的第二处依据更正（第四十轮实测）**：本节初稿接着写"`mir-baseline` 与 `legacy-baseline` 在该文件上同值（legacy 本就输出宿主平台值 `100`），也就是 ① 的修复把 MIR 拉回了 legacy 语义"——**这句是错的**。`legacy-baseline.tsv` 在该文件上记的是 **`600`**（用 `47b6cad` worktree 重测证实 legacy 脚本模式不过滤），与 `mir-baseline` 的 `100` **不同值**。所以 ① 的修复**没有**"拉回 legacy 语义"，而是让 MIR 按注解的**文档语义**过滤、**有意偏离** legacy；这条分叉就计入下面 `legacy-baseline` 的 `DIVERGE=38` 之中。详见 ① 的更正框。

**新旧口径对照（本轮的"零回归"证据）**：第三十九轮扫描（删除前）`MATCH=388/422`、`MIR_GAP=0`；删除后同一语料经 `mir-baseline` 比对为 `SAME=388` / **`REGRESS=0`** / `DIVERGE=1`（沿用同一条已知假阳性）/ `BOTH_FAIL=33`。两者**逐项一致**，说明删除本身**不改变任何文件的行为**（预期如此：legacy 在默认口径下本就未被调用）。两个口径用不同的机制得到同一个 388，互为交叉验证。

**两个基线的比对结果（删除后首次实跑）：**

| 基线 | SAME | DIVERGE | REGRESS | IMPROVED | BOTH_FAIL | 读法 |
| --- | --- | --- | --- | --- | --- | --- |
| `mir-baseline.tsv`（回归参照） | 388 | 1 | **0** | 0 | 33 | 与删除前 `MATCH=388` 逐项吻合；唯一 DIVERGE 是已知地址假阳性 |
| `legacy-baseline.tsv`（语义参照） | 302 | 38 | **0** | 49 | 33 | `REGRESS=0` ⇒ 没有任何"legacy 能编而 MIR 不能"的文件；`IMPROVED=49` ⇒ legacy 坏掉的 82 个里 49 个 MIR 能过；`DIVERGE=38` 与历史上 legacy 口径的分叉规模（第三十七/三十八轮 ~37–38）**吻合**，说明冻结的 oracle 忠实复现了被删掉的那个后端 |

**这张表的用法（删除后的日常口径）**：`mir-baseline` 的 `REGRESS` 桶就是新的 `MIR_GAP`（"以前能过，现在不能过"）；`legacy-baseline` 的 `DIVERGE` 桶就是新的 `DIVERGE`（语义分歧候选，逐个判"谁对"——已知多数是 legacy 错，如 bool 打印不一致、`async-yield` legacy SIGSEGV、hmac/sha256 legacy 算错）。



**本批踩到的两个 harness 自伤（都已修，值得记住）：**

- **`mir_golden.sh` 在比对模式误强制 `NOLANG_MIR=0`**：脚本注释明确写着"比对模式从不强制 NOLANG_MIR"，但代码里 `GOLDEN_MIR=${GOLDEN_MIR:-0}` 是无条件的，`fingerprint_one` 按它选命令行。删掉 legacy 后，比对**每个文件都走 `NOLANG_MIR=0` 的报错路径** → 首次实跑报 `REGRESS=389`（= golden 全部 rc=0 条目），差点被读成"全量回归"。修法：引入 `FORCE_MIR`，**仅 `-update` 模式**按 `GOLDEN_MIR` 取值，比对模式恒空（恒测当前默认）。**教训：harness 的"默认参数"本身就是被测对象的一部分——注释描述的行为必须与代码逐行核对，尤其是"从不做 X"这种断言。**
- **`${b^^}` 不可用**：macOS 自带 bash 3.2（`/bin/bash`）无 bash 4 的大写展开，脚本在打印非空桶时以 `bad substitution` 中断（前一次空桶侥幸没触发）。改为 `tr 'a-z' 'A-Z'`。**教训：脚本要么声明 `#!/usr/bin/env bash` 并确认版本，要么只用 POSIX 子集。**

---

#### 13.3.14 第四十轮（2026-09-16）：MIR 单测补齐（平台变体）+ 两处依据更正 + 两个「两后端共有」的既有限制

**触发**：第三优先级第一项 —— "MIR 单测 6→对齐 legacy 188，**重点补平台变体/datalayout/溢出注解**，那正是 §13.3.13 ① 缺口的零测试区"。从平台变体入手（① 刚证明它是零测试区），过程中反查出 ① 的**依据记载有误**，并顺带钉住两个两后端共有的既有限制。

**口径与前置**：legacy 已于第三十九·续轮删除，所以本轮的"legacy 对照值"一律来自 **`git worktree add /tmp/no-legacy 47b6cad` 现场编译的 pre-deletion 二进制**（`go build -ldflags="-s -w" -o bin/no-legacy ./cmd/no`）。这正是 §13.3.13 ④ 冻结基线之外的第二条 oracle 通道：**需要"逐字节对照某个具体用例"时用 worktree 现场重建；需要"批量回归"时用冻结的 TSV。** 用完 `git worktree remove --force`。

##### ① 交付：`src/mir/platform_test.go`（`src/mir` 测试数 6 → 13）

7 个新单测（其中一个含 3 个子用例），全部以**宿主**推导期望值（`package.PlatformKeyFor(runtime.GOOS, runtime.GOARCH)`）而非硬编码 `darwin/arm64`，因此在任何 `PlatformKeys` 覆盖的机器上都有意义（无覆盖的机器自动 `t.Skip`）。

| 测试 | 钉住的行为 |
| --- | --- |
| `TestPlatformKeyForRoundTrip` | `PlatformKeys` ↔ `PlatformKeyFor` 双射；`freebsd/arm64` → `""`（表外平台不得解析成别的 key） |
| `TestNodeMatchesPlatformForEveryKey` | **穷尽全表**：`nodeMatchesPlatform` 对每个 key 恰好只有宿主那个返回 `true` |
| `TestNodeMatchesPlatformKeepsUnannotatedNodes`（3 子用例） | 无注解 / `#{overflow = wrap}` / `#{overflow = clamp0}` **都保留** —— 带值注解与裸平台旗标共用 `#{}` 语法，误判会把半个程序过滤掉 |
| `TestScriptModePlatformVariantKeepsHostValue` | **① 的回归**：脚本模式（无 `fn main`）只保留宿主变体，且**只有**它 materialise 成全局 |
| `TestScriptModeLoneWrongPlatformVariantIsDropped` | 单独一个非宿主变体不得留下任何值 |
| `TestTopLevelVariantWithExplicitMainKeepsHostValue` | 有显式 `fn main` 时顶层 let 走注册循环，宿主变体胜出 |
| `TestFunctionBodyPlatformVariantIsNotFiltered` | **钉住既有限制 A**（见 ③）：函数体内的变体两个后端都不过滤 |

**判别力实测（不是"跑绿就算"）**：把 `synthesizeMainForTopLevel` 的检查临时改成 `if false && !nodeMatchesPlatform(pkg, id)`，恰好 `TestScriptModePlatformVariantKeepsHostValue` 与 `TestScriptModeLoneWrongPlatformVariantIsDropped` 两个失败、其余 5 个不受影响；恢复后全绿且 `git diff src/mir/hir2mir.go` 无残留。⇒ 用例确实绑定到被修的那条路径。

**测试如何驱动 lowering**：`parseHIR` 必须复刻 `build/transpiler.go` 的前端序列 —— `parser.ASTToHIRWithMap(prog)` **加上** `parser.PopulateInferredTypes(pkg, idMap)`。只做前者时脚本模式产出 `globals=0`、`main` 为空的空壳（顶层语句读不到推断类型就不进合成 `main`），"过滤有没有发生"这个问题根本无法被观测。**这是写这类 lowering 测试最容易踩的坑。**

##### ② 依据更正（重要）：§13.3.13 ① 的 legacy 列**全是错的**

① 原文把 `#77` 描述成"legacy 正确、MIR 错误"，并给出 `legacy=100`、`legacy=1`、"legacy 正确报错"等值。第四十轮用 `47b6cad` worktree 现场重测，**实测值如下**（详见 ① 的更正框）：

| 用例（脚本模式） | legacy 实测 | MIR（修复前） | MIR（修复后） |
| --- | --- | --- | --- |
| `#{mac-amd64} V = 8` 单独 | **`8`** | `8` | 无输出 |
| `#{linux-amd64} V = 9` 单独 | **`9`** | `9` | 无输出 |
| `#{mac-arm64} V=1` + `#{mac-amd64} V=2` | **`2`** | `2` | `1` |
| 六平台变体各持不同值 | **`600`** | `600` | `100` |

**legacy 在脚本模式下从来不过滤平台变体**（一律"后出现者胜出"）。所以 `#77` 的真身是：**两个后端共有的缺口，MIR 单方面修好了它** —— 不是"MIR 偏离了正确的 legacy"。判据从"与 legacy 逐字节一致"改为**注解的文档语义**（`docs/docs/lang/syntax.md`）。直接后果：`tests/test-platform-const.no` 现在在 MIR（`100`）与 legacy（`600`）之间**故意分叉**，在 `legacy-baseline` 比对里表现为一个 `DIVERGE`。

**为什么当初会记错**：① 写于删 legacy **之前**，当时想当然地认为"legacy 是参照物、必然正确"，只实测了 MIR 侧就填了 legacy 列。⇒ **方法论：把"与 X 一致"当正确性判据之前，必须先把 X 也测一遍。参照物本身可能正是错的那一方。**

##### ③ 既有限制 A：函数体内的平台注解不被过滤（两后端一致）

```nolang
main = () () {
  #{mac-arm64}
  PV = 111
  #{linux-amd64}
  PV = 222
  print(PV)
  return
}
main()
```

宿主 arm64 macOS：legacy **`222`**、MIR **`222`**（一致）。**后出现者胜出，与宿主无关。** 语句级平台过滤只接在**顶层**（注册循环 + `synthesizeMainForTopLevel`），函数体/块体内的语句两条路径都没接。这不是回归（删除前两者就一致），本轮以 `TestFunctionBodyPlatformVariantIsNotFiltered` 把它**显式钉在测试里**：将来若真要修，该测试会失败并提示改期望值。

##### ④ 既有限制 B：4 种溢出模式策略在实际编译中未生效（两后端一致）

规范（`docs/docs/lang/syntax.md` 溢出节）定义 `#{overflow = wrap | clamp0 | min | max | saturate}`，legacy 也确实有 `emitOverflowArith` → `emitClampArith` 的完整实现（`expr.go`，约 100 行，含 `@llvm.*.with.overflow` + 溢出方向判定 + `select` 箝位）。但**端到端实测**（宿主 arm64 macOS）：

```nolang
f = (x i64) () {
  #{overflow = clamp0}      ; 依次换成 wrap / min / max / saturate
  b = x + 1
  print(b)
  return
}
f(9223372036854775807)
```

| 模式 | legacy | MIR |
| --- | --- | --- |
| `wrap` / `clamp0` / `min` / `max` / `saturate` | 全部 `-9223372036854775808` | **完全相同** |

**5 种模式输出完全一致（都退化为回绕）**，即：注解目前只起到"关掉 `option<int>` 包装、让未处理溢出的算术通过 `ValidateUnhandledOverflow`"的作用，**具体溢出策略没有生效**（`clamp0` 应为 `0`、`max`/`saturate` 应为 `9223372036854775807`）。因为**两后端一致**，这**不是 MIR 缺口**，而是语言特性的既有限制。

**范围限定**：本条只验证了**语句级注解贴在 `let` 上方**这一形式（恰好是规范示例使用的唯一形式）。legacy 的 `overflowModeFromNode` 对 `ExpressionStatement`（if/match 臂体）有独立的 `OverflowMode` 字段读取路径，**该路径本轮未验证**，可能生效 —— 若将来要修，先从那里查起。根因线索：`overflowModeFromNode` 对 `FunctionDefinition` 显式返回 `""`（函数级注解已被移除），而 `LetStatement` 没有 `OverflowMode` 字段，只能靠 `annotationsFor`（AST 节点 → HIR id → 注解）兜底，实测该兜底在 let 上取不到值。

**MIR 侧现状**：`grep -n 'nsw\|nuw\|OverflowMode\|overflow-mode' src/mir/*.go` **为空** —— MIR 完全没有溢出模式的概念，整数算术一律裸 `add`/`sub`/`mul`（即 wrap 语义）。这解释了为什么"退化"在 MIR 上是**预期行为**；要真正实现，需在 lowering 侧引入 `curOverflowMode` 上下文并把模式带到 `Inst` 上供 codegen 分派。

##### ⑤ 方法论要点（本轮新增）

- **"与 X 一致"不是正确性判据，除非你先测过 X。** ② 是这条的实例：参照物（legacy）本身在脚本模式下就是错的，把它当基准会把"修对了"误记成"跑偏了"。
- **零测试区既可能藏缺口，也可能藏"两后端共有的既有限制"。** 本轮在同一个区域（平台变体 / 溢出注解）同时挖到两种：平台变体是**真缺口**（MIR 单方面可修），溢出模式是**共同限制**（需先改语言实现）。**发现差异后第一件事是"另一侧也测一遍"，据差异方向分流。**
- **写 lowering 测试必须复刻真实前端序列**（`ASTToHIRWithMap` + `PopulateInferredTypes`），否则会得到"看起来跑通了、其实什么都没 lower"的空壳（见 ① 末段）。
- **回归用例要实测判别力**：临时把被修的检查短路成 `false && …`，确认"恰好该失败的失败"。顺带能确认用例绑定的是哪条路径（本轮 5/7 不受影响，正说明只有 2 个绑定到该路径）。

---

#### 13.3.15 第四十一轮（2026-09-16）：`HANG=4` 的证伪 —— 三个是编译期爆炸（SROA 病态），一个是有意死循环

##### ① 结论先行：冻结基线直接推翻了旧分类

`tests/golden/*.tsv` 的 rc 分布是权威依据（**冻结值**，不是扫描脚本的推定值）：

| 文件 | legacy rc | MIR rc | 真身 |
| --- | --- | --- | --- |
| `tests/test-for2.no` | **124** | 124 | 真·无限循环，但**是程序语义**（见 ②） |
| `tests/test-parse-min.no` | **1** | 124 | MIR 编译期爆炸；legacy 根本没跑到运行 |
| `mem-safety/test-json-parse-option.no` | **1** | 124 | 同上 |
| `mem-safety/test-json-nested-match.no` | **1** | 124 | 同上 |

（mir-baseline 全量 rc 分布：`0`×389 / `1`×29 / `124`×4；legacy-baseline：`0`×340 / `1`×81 / `124`×**1**。）

第三十八轮记的"3 个 json/parse 测试**两模式都死循环**、属 std 缺陷"**不成立**。§3b 的两条口径陷阱在这里同时生效：扫描脚本的基线臂跑的是 `NOLANG_MIR=2`，而 `MIR=2` 就是 MIR 自己（第三十九轮已勘），所以 `rc2` 复现的是 MIR 的挂死、不是 legacy 的行为；legacy 对这三份文件的 rc 是 **1**（编译期拒绝），从来没挂过。

##### ② `test-for2.no` 的"挂死"是设计，不是缺陷

文件全文两行：`{` `} (true)`。`no build` **1 秒完成、rc=0**，`NOLANG_MIR_DUMP_MIR=1` 显示合成的 `main` 是

```
block1 → block2 { const true; cond-br → {3,5} } → block3 → block4 → block2 ; block5: return
```

即 `while (true) { }`。**"块 + 条件"就是循环语法**，`(true)` 是恒真条件；两个后端都转成无限循环，这是正确行为。它没有 `expect:`、没有任何输出断言，扫描把它记 `HANG` 是**口径**问题而不是 bug —— 语料里应当有一条"有意死循环"的标注。

##### ③ 三个 json 文件的真身：62 秒的编译期爆炸

最小复现（11 行，脚本模式）：

```nolang
p = json-pool {}
body str = '"hi"'
root, next, ok = p.parse(body, 0)
```

**逐构造耗时**（`no build`，串行无争用）：

| 程序 | MIR 块数 | `no build` |
| --- | --- | --- |
| `print('hi')` | 1 | **1.4s** |
| `p.init()` | 15 | 3.3s |
| `p.skip-ws()` | 15 | 3.6s |
| `p.alloc()` | 15 | **14.0s** |
| `p.parse-str()` | 29 | 10.2s |
| `p.parse-num()` | 71 | 4.7s |
| `p.parse()` | 129 | **61.4s** |

**耗时与 MIR 块数无关**（15 块的 `alloc` 比 71 块的 `parse-num` 贵 3 倍），且产出的 IR 只有 **173KB / 4694 行** —— 所以既不是 IR 体积问题，也不是前端问题（`no fmt` 0s、`no vet` 2s，`vet` 已经解析并检查了整个内嵌 std）。

用 `no build -v` 逐行打时间戳（零代码改动）切出阶段：**第一条 `-v` 输出出现在 +31.5 秒**（= 前端 + MIR + `emitMIR` 内嵌的 verify 预检 `opt -O3`），此后 builder 再跑一次 `opt -O3`。单命令实测：

| 命令 | 耗时 | 输出体积 |
| --- | --- | --- |
| `opt -O0 dist/c_parse.ll` | **30ms** | 211KB |
| `opt -O1 …` | **15.2s** | **5.7MB** |
| `opt -O3 …` | 15.1s | 5.7MB |
| `llc` on 预优化 IR | **>100s（未跑完）** | — |
| `llc` on `-O3` 后的 IR | ~14s | — |

⇒ 62s ≈ 2s（前端+MIR）+ (15s opt + 14s llc) **× 2**。**整条后端流水线每次构建跑了两遍。**

##### ④ 根因：`sroa` 撕裂超大聚合（IR 形态问题，尚未修）

`opt -passes=inline` 之后逐个 pass 单独施加，只有一个爆：

| pass | 耗时 | 输出体积 |
| --- | --- | --- |
| **`sroa`** | 1.26s | **105,192,073 B（458×）** |
| jump-threading / simplifycfg / early-cse / mem2reg / gvn / licm / loop-rotate / correlated-propagation / tailcallelim / inline / loop-unroll / loop-vectorize | 26–33ms | 157–330KB |

`-unroll-threshold=0`、`-unroll-count=0` 均无效（仍 5.7MB），`-inline-threshold=0` 反而恶化到 12.4MB ⇒ **既不是展开也不是内联，是 SROA**。

为什么 SROA 会炸：

```
%json_json_value = type { i64, %str-long, double, i1, [16 x i64], [16 x %str-long], i64 }   ; ≈350B
%json_json_pool  = type { [64 x %json_json_value], i64 }                                     ; ≈22KB
```

且 `alloca [64 x %json_json_value]` 在 366 个块里出现 **81 次**（`alloca [16 x i64]` / `[16 x %str-long]` 各 6 次，全函数共 684 个 `alloca`）。SROA 按常量下标把内联大数组拆成标量：单个 alloca 可拆出 `64 × 38 ≈ 2400` 个标量，81 个就是十万量级 —— 正是 105MB 的来源。优化后的 `json_json_pool_parse` 由 1896 行涨到 **55,822 行**，`alloc` 由 340 行涨到 13,988 行。

**一个必须记住的反直觉点**：`llc` 在**未优化** IR 上超过 100 秒（684 个 alloca ≈ 1.8MB 栈帧），在 `opt -O3` 后的 IR 上只要 14 秒。任何"跳过 opt 直接汇编"的直觉都会把构建变慢 —— 这也解释了为什么 `NOLANG_OPT_LEVEL=-O0` 的构建比默认 `-O3` **更慢**。

##### ⑤ 落地 #82：预检不再重复整条后端流水线

`verifyMIRIRViaOpt` 原本在**每次构建**里跑完整 `opt $NOLANG_OPT_LEVEL`（默认 `-O3`）**加** `llc`，而 builder 随后用**同样的命令、同样的字节**再跑一遍；预检的 `m.s` 产物直接丢弃。改动：

- **默认**只跑 `opt -passes=verify`（~30ms）。这正是该阶段存在的理由 —— IR verifier 是任何 `opt` 管线的第一件事，注册类型不匹配 / 未定义被调 / void 误用都会在这里被拒；而优化产物没人用。
- **跳过 Stage 2（`llc`）**：见 ④ 的实测，喂未优化 IR 会更慢；builder 紧接着就用同一条 IR 汇编。
- `NOLANG_MIR_PREFLIGHT=full` 恢复旧行为（完整管线 + `llc`），留给"只被某个变换 pass 拒绝"的 IR 形态排查。

**这次改动不改变任何构建的成败，只改变失败发生在哪一步、报哪条消息**：builder 随后对同一份字节跑同一条命令，所以 full 预检拒绝的 IR 在 builder 侧同样被拒。实测：

| 场景 | 改前 | 改后 |
| --- | --- | --- |
| `print('hi')` | 1.4s | **1.6–2.0s**（不变） |
| 11 行 json 复现 | 61.4s | **31.8 / 32.3s** |
| 同上，`NOLANG_MIR_PREFLIGHT=full` | — | 63.5s（忠实复现旧默认） |
| `tests/test-parse-min.no` | rc=124（>90s 超时） | **rc=0 / 33.4s**，输出 `start / ok: parse str / next= / 7` |
| `mem-safety/test-json-parse-option.no` | rc=124 | **rc=1 / 117.1s**，输出仅 `start` |
| `mem-safety/test-json-nested-match.no` | rc=124 | **rc=1 / 113.4s**，输出仅 `start` |

预检门本身也验过仍然有效：用会把 `opt` 替换成"总是失败"的假二进制（`PATH=/tmp/fz/failbin:$PATH`），两种模式下都得到 `MIR IR failed LLVM verification: … fake-opt: deliberate failure`。

⇒ **三个"挂死"文件没有一个是死循环**：一个转为通过，另两个编译成功后**在运行期以 rc=1 失败**（都只打印出 `start`，失败发生在 parse/match 之内）。这是把编译期爆炸拆掉之后**新浮出**的既有缺陷 —— 记为 **#84 待查**（legacy 侧它们连编译都过不去，此前从未被执行过）。

`HANG` 至此只剩 `test-for2.no` 一个，且是有意的。但注意 `test-parse-min`（33s）与两个 json 用例（113–117s）的差距说明**重复成本只占其中一半**：去掉重复后它们仍紧贴 90s 扫描预算，所以在重新冻结之前，这两个文件在基线里**仍会被记 124** —— 真正解决要靠 ⑥ 的 IR 形态改动。这也顺手解释了扫描为何慢：凡是碰 `json` 的用例都在白付一倍后端成本。

##### ⑥ 待办 #83（未做）：IR 形态本身

③④ 只是把重复成本去掉；**根因仍在** —— 值语义结构体（`json-pool` 内联 64 元素数组，元素又各带两个 16 元素数组）被整体物化成 `alloca`，单次 `opt` 仍要 15s、`llc` 14s。可能的修法（都属 codegen 语义改动，需单独评估）：

- 大数组字段走堆分配 / 按引用访问，避免"取一次 `.nodes` 就复制 22KB"；
- 或用属性/元数据（`optnone`/`noinline`/`llvm.sroa` 相关）抑制对这类聚合的 SROA —— 代价是丢掉优化。

**判别这个 bug 是否回归的最快信号**：`no build` 一个 11 行 json 程序的耗时（现在是 32s，修好 IR 形态后应降到秒级）。

##### ⑦ 方法论要点（本轮新增）

- **超时不是死循环的证据**。`MIR_SWEEP_TIMEOUT=90` 的产物里，慢编译与挂死无法区分；四个 `HANG` 里三个是慢编译。判"是否循环"用"两个不同预算下的 `NOLANG_MIR_DUMP_MIR` 是否**逐字节相同且完整**"，再配合 `no fmt`/`no vet` 排除前端。
- **`ps` 在 sandbox 内 `operation not permitted`**，返回空列表看起来就像"没有残留进程"——所有基于 `ps` 的"环境是干净的"结论都不可信，改用 `pgrep -f`（且模式要窄：`pgrep -f 'bin/no'` 会匹配到工具自身的 shell）。残留编译进程会让相邻两次测量差一倍。
- **"两遍流水线"这类成本要靠读阶段顺序发现，不能靠直觉**：`verifyMIRIRViaOpt` 的注释写着"预检"，但它跑的是完整管线 + 汇编，产物全部丢弃。
- **反例优先**：`llc` 在未优化 IR 上 >100s 这一条，是在实施"降级为 verify-only"**之前**顺手测出来的；没测就会把构建从 62s 改成 >130s 并以为是优化。
- **确认测试真的在测这件事**：把 SROA 说成"优化器很慢"是不够的，逐 pass 隔离（`-passes=<one>`）才能把 15s 归到一个 pass 上。

##### 13.3.16 第四十二轮（2026-09-16 续）：`rc=124` 的语义清理；证伪"地址差异假阳性"，挖出静默错误编译 #85

##### ① #82 的行为保持：拿改动**之前**冻结的基线当预言机

`tests/golden/mir-baseline.tsv` 的 mtime 是 **11:36:34**，而 `src/build/transpiler.go` / `bin/no` 是 **12:49** —— 即基线早于 #82 一小时。用改动后的后端跑全量比对，422 条**逐条完全一致**（`SAME=388` / `REGRESS=0` / `IMPROVED=0` / `BOTH_FAIL=33` / `DIVERGE=1`），且非零 rc 的 33 条路径与数值**逐条相同**（`0`×389 / `1`×29 / `124`×4）。

这是比"跑一遍没挂"强得多的证据：**一次跨改动的逐条等值**。原计划的"#82 改了 rc，所以要重新冻结"因此**不必要**——但它的前提（"改了 rc"）本身是错的，见 ②。

##### ② `rc=124` 里的第三个来源：并发争抢（而不是文件慢）

`tests/test-parse-min.no` 在冻结基线里是 **124**，串行单跑却是 **32.9s / rc=0**（输出 `start / ok: parse str / next= / 7`）。同一条目两种口径矛盾，只可能来自测量条件：golden 用 `-P 8` 并发跑 422 个文件，而 LLVM `opt` 是 CPU 密集的，8 路争抢把 33s 顶过 90s 上限。指纹哈希是**空输出哈希**（`e3b0c44…`）正好印证——一个打印瞬时发生的程序根本没轮到运行。

⇒ **超时不是文件的属性，是测量环境的属性。** 124 桶里同时住着：真死循环（`test-for2.no`）、慢但有限的编译（两个 json 用例 111–120s）、以及纯争用伪影。危害不止是数字错，而是**超时的文件无法报告任何行为回归**——两个 json 用例编译成功后**运行期 sigsegv（#84）**，只要它们卡在 90s 之上就永远看不见。

##### ③ 重冻结：`rc=124` 从此只表示"死循环"

`MIR_GOLDEN_TIMEOUT` 90s → **300s**、`MIR_GOLDEN_JOBS` 固定 8 → **4**（§12 #86）。重冻结结果：

| | 旧基线（90s / -P 8） | 新基线（300s / -P 4） |
| --- | --- | --- |
| `rc=0` | 389 | **390** |
| `rc=1` | 29 | **31** |
| `rc=124` | 4 | **1** |
| 124 名单 | for2 + 2 json + **parse-min（伪影）** | **只有 `tests/test-for2.no`** |

逐条 diff 正好三处，全部是"拿到了本该早就拿到的判定"：

```
rc: 124 -> 1   tests/mem-safety/test-json-nested-match.no
rc: 124 -> 1   tests/mem-safety/test-json-parse-option.no
rc: 124 -> 0   tests/test-parse-min.no
```

即：**#84 的运行期失败第一次被基线记录在案**——修好后会显示 `IMPROVED`，将来再回归会被抓住。`rc=124` 现在有单一含义（死循环），`for2` 是语料里唯一的、且是设计使然的那个。

随后用新骨架对**刚冻结的**基线跑一次比对，验证三个新机制同时生效：

```
SAME=389   DIVERGE=0   UNSTABLE=1   REGRESS=0   IMPROVED=0   BOTH_FAIL=32   NEW=0
UNSTABLE: tests/mem-safety/str-concat-leak.no
```

`DIVERGE=0` 是本轮最有价值的单个数：在此之前它恒为 1，且那 1 是**假信号**（见 ⑤），即"每次比对都报警、每次都不必看"。389 + 0 + 1 + 0 + 0 + 32 = 422 闭合。`BOTH_FAIL=32` 与冻结分布（31×rc=1 + 1×rc=124）一致。

##### ④ #84 定性为段错误，并排除了"类型形状"这一假设

stderr 给出确凿证据：`Error: signal: segmentation fault`（运行期把信号翻成该诊断，以 rc=1 退出）。用 `print` 把 `json.parse('')` 之后每一步切开，输出止于第一个 `print` ⇒ 崩溃在**第一条语句**，走的是最简单的早返回（`result = nil` → 空串 → `result = err('empty input')` → `return`），与递归下降解析器无关。

随后把嫌疑类型**原样搬进一个 `/tmp` 模块**（`json-value` 350B × 64 ⇒ `json-pool` 22KB、`json`、`?json`、struct 字面量初始化、option 包装、`err(<str>)` 早返回、`ok ->` 匹配）——**p5/p6/p9 全部通过**（2–10s 编译）。所以触发因素在 `src/std/json.no` 自身，而非 22KB 值语义载荷的 option/match 机制。

（这也顺手证实了 #83 的成本面：p5 用**同样的类型**只要 10s，而**任何** json 程序都要 110s+ —— 差距来自 json.no 的代码体量与 `sroa` 交互，不是类型本身。）

##### ⑤ 反转：`str-concat-leak.no` 不是"地址差异假阳性"，是静默错误编译

`tests/mem-safety/str-concat-leak.no` 的哈希在**同一个后端**两次运行之间就不一样。此前三轮把它记作"DIVERGE 的地址差异假阳性"——**查下去发现完全不是**：该文件打印的是 `'item' + i.to-str()`（1000 行纯文本，**不含任何地址**）。5 次实测：

```
rc=0 lines=1000 bytes=775904872
rc=0 lines=1000 bytes=784287641
rc=0 lines=1000 bytes=775904872
rc=0 lines=1118 bytes=766050306
rc=0 lines=1128 bytes=775904872
```

一个打印 `item0`…`item999` 的程序产出 **775MB**，首字节是 `item` + 12 个 NUL + `06 00…` ×2——打印的是**结构体原始内存**。IR 直接指明了原因：

```llvm
%c12 = call %str-long @str_concat(%str-long %lv13, %str-long undef)
```

拼接的右操作数是 **`undef`**：`i.to-str()` 的 `call` 指令压根没生成，消费方读到 `undef`，而 `str_concat` 会把 `undef` 的 len/data 字段当真 ⇒ 长度任取寄存器残留值。**rc=0**，输出"看着有东西"。8 组 2 秒级探针把触发条件收窄为：**count-for 迭代变量作接收者 + 内建调用的实参位置 + 方法调用未先绑定**（矩阵与修法方向见 §12 #85）。

两个可疑点都指向**未绑定**这个字眼，而该文件自己的注释写的就是"拼接结果**未绑定变量**"——即这条测试从一开始就在测这件事，只是三轮里没人打开它。

**现场重建 legacy（`47b6cad`）把"谁错"划清了**：同一批探针下 legacy 在 **c1/c4/`str-concat-leak.no` 上都 rc=1 并给出精确诊断**（`opt: use of undefined value '@i.to.str'`；`codegen error: expression produced empty value in emitArgAsStrLong`），而 MIR 在同一位置 **rc=0 输出垃圾**。所以：**根因（前端把 `i.to-str()` 解析成"类型 `i` 上的方法"）是共有的**，**MIR 独有的是失败模式**——legacy 说"表达式没产生值"，MIR 静默把 `undef` 交给消费者。这条对照把 #85 从"一个诡异现象"变成了"一个明确的护栏缺失"，修法见 §12 #85。

**顺带影响判读口径：`IMPROVED` 不能无条件当成好事。** `legacy-baseline` 里 `rc!=0` 的 82 条是"legacy 编不过 = 无参照"，而本轮对它的语义比对里 `IMPROVED` 从 49 涨到 **50**，多出来的那一条正是 `str-concat-leak.no`：legacy rc=1 → MIR rc=0，harness 记为"改进"，**实际是 MIR 接受了 legacy 拒绝的输入并错误编译**。⇒ 判读 `IMPROVED` 时分两类：legacy 因**自身缺陷**编不过（如 `%addopt.final`）而 MIR 正确 → 真改进；legacy **正确地拒绝**了非法输入而 MIR 接受 → **可疑，必须人工看输出**。当前 50 条里至少 1 条属后者。

##### ⑥ 方法论要点（本轮新增）

- **一个持续的哈希不一致，先当作 bug，不要先当作噪声。** "地址差异假阳性"这个结论从未被验证过（该文件根本没有地址），却静默地把一条真实的静默错误编译（rc=0 + 775MB 垃圾）封存了三轮。命名一个现象为"假阳性"之前，去看一眼它的输出。
- **口径矛盾（同一文件 33s/rc=0 vs 基线 124）是测量条件的线索，不是数据噪声。** 顺着它挖出了 `-P` 争用这一类系统性偏差，并让一个 bucket 从"三种含义"变成"一种含义"。
- **跨改动的逐条等值 > 事后跑一遍。** 用改动**之前**冻结的基线做预言机，得到的"422 条逐条一致"才是"行为保持"的证据；顺序反过来（先改后冻）就等于把结论假设掉了。
- **拿"同形状搬到干净模块里"来二分归属。** p5/p6/p9 把"22KB 值语义载荷 + option + match"整条链排除掉，把 #84 的范围从"可能的机制"缩到"`json.no` 自身"；代价 2–10s，而直接在 json 上试是 110s/次。
- **`grep` 在 `-n "pattern\|pattern"` 形式下会静默返回空**（本轮多次踩到），改用工具的 Grep 或简单单元模式。
- **`pgrep -f` 的模式会匹配到工具自身的 shell 命令行**（模式文本就在那条 `zsh -f -c …` 里），所以"窄模式"并不足够；判残留进程要连 `命令名` 一起排除。

##### 13.3.17 第四十三轮（2026-09-16 续）：#85 护栏落地 —— 静默错误编译变响铃诊断，影响半径由 19 文件修正为 2 文件

第四十二轮把 #85 挖了出来，但**止步于"加了一条 env 门控诊断"**，并把测量结论写成"19 个文件 / 55 处命中，其中 18 个当前 rc=0 ⇒ 可能有一批静默错编译"。本轮做两件事：**把护栏真正落地**，以及**修正上面那个被高估了 10 倍的影响半径**。

**① 影响半径 19 → 2：诊断的两次命中被混成了一类**

`NOLANG_MIR_DEBUG_UNDEF` 打印的 55 条记录里同时住着两种完全不同的东西，按字段切开即分：

| 类别 | 条数 | `value=` | 落点 | 判定 |
| --- | --- | --- | --- | --- |
| `llvm="void"` | **51** | `173 / 708 / 604 / 183 / 1367 / …`（真实值 id） | `hashmap_str_str_hash`(18) / `hashmap_str_i64_hash`(12) / `hashmap_str_bool_hash`(3) / `str_to_i8`(4) 等 **std 函数** | ✅ **合法** —— void 值本就没有存储，调用方正是靠这个 `"void"` 返回值跳过它（`emitCall` 的 print 循环里那句 `argT == "void" -> continue`） |
| `value=0 llvm="i64" slot=""` | **4** | **`0`** = `NoVal`（`mir.go:41`） | `_nolang_main`(2) + `des_block`(2) | ❌ **真洞** —— 消费方在读取**没有任何指令产生过**的操作数 |

⇒ 真实影响半径是 **2 个文件**：`tests/mem-safety/str-concat-leak.no`、`tests/test-std-hash.no`。上一轮"18 个 rc=0 文件可能都被静默错误编译"是**把 51 条合法命中当成可疑**推出来的。

**教训（本轮方法论第一条）**：一条诊断如果**为两种不同原因**同时触发，**必须先按字段分类再计数**，否则计数本身就是错的。上一轮的做法（`awk '$1>0' | wc -l` 数"有命中的文件"）恰恰丢掉了区分所需的那一列 —— 而那一列**当时就打印在日志里**，只是没有被读。

**② 落地：把合在一起的条件拆成两支，只对第二支响铃**

`src/mir/codegen.go` `loadVal` 原来是 `if lt == "void" || slot == ""` 共用一个 `return "void", "undef"`。**拆开的依据就是上表**——合在一起改会让 51 条合法命中全部误报。现在：

```go
if lt == "void" { return "void", "undef" }          // 合法：void/unit 无存储
if slot == ""   { c.fail(...); return lt, "undef" } // 静默错误编译 -> 响铃
```

报文区分两种成因：`NoVal` 者直指"喂这个操作数的指令没有 Dst"，其余报出 `value` 与 LLVM 类型。**注意第二种报文目前是未被执行过的路径**——语料里 55 条命中中，除 4 条 `NoVal` 外全是合法的 void 分支，**没有任何一条**是"有类型（非 void）却无槽位"。它保留在那里是为了让将来出现的该类失败不至于又变成静默 `undef`，但**它至今只在代码里存在，没有被任何真实输入触发过**（写判据时不要把"两处报文都见过"当真）。用 `c.fail` 而非立刻 `return err` 的理由：`c.fail` 只累积、统一在 `EmitLLVM` 出口汇总，**一次构建就把函数内每一处越界点全部列出**（`test-std-hash.no` 的 2 处一次性报全）；这也让上一轮那条 `NOLANG_MIR_DEBUG_UNDEF` 测量流程变成可选（该开关保留，供日志 grep）。

**③ 这是"对上 oracle"，不是回归 —— 而且要先分清"哪个 rc"**

`no build` 的 rc 是**编译器**的成败；golden 基线记的是 `no run` 的 rc，即**先编译再执行、程序自己的退出码**。手工探针（`no build`）与预言机（`no run`）口径不同，混用会得出错表。按预言机口径逐字节对齐：

| 文件 | legacy（语义 oracle） | MIR 加护栏前 | MIR 加护栏后 |
| --- | --- | --- | --- |
| `tests/mem-safety/str-concat-leak.no` | `1 e3b0c442…`（空 stdout） | `0 eb28fc09…`（编译成功，程序跑了，输出垃圾，**退出 0**） | `1 e3b0c442…` ✅ 与 oracle **逐字节相同** |
| `tests/test-std-hash.no` | `1 e3b0c442…`（空 stdout） | `1 399229a5…`（编译成功，程序跑了**有输出**，但程序自身返回 1） | `1 e3b0c442…` ✅ 与 oracle **逐字节相同** |

⇒ 这次 `rc=0 → rc=1` **不是 REGRESS，是把 MIR 的判定拉回语义 oracle 的判定**（且连 stdout 都逐字节相同）。这是上一轮 §13.3.16 里"`IMPROVED` 不能无条件当成绩"的**反向应用**：`REGRESS` 也不能无条件当退步，要看 oracle 在那边怎么说——**并且要看 oracle 记的是哪个 rc**。

**④ 本轮新发现的桶方案盲点：两个"都失败"不等于同一件事**

`test-std-hash.no` 的**预言机 rc 根本没变**（`1 → 1`），变的是**原因**（"编译成功、程序自己失败并有输出" → "编译失败、stdout 变空"）。而比对脚本对"两边都非零"只记 `BOTH_FAIL`、**不比较哈希** —— 所以这次修复在桶计数上**完全不可见**：`BOTH_FAIL=32` 前后一模一样，`SAME/REGRESS` 也没动。`test-std-hash.no` 正好同时命中两件事：它在 `legacy-baseline` 里也是 `rc=1`（legacy 编不过），于是"legacy 编不过 / MIR 编不过"与"legacy 编不过 / MIR 编得过但跑挂"被**压成同一个桶**。

⇒ `BOTH_FAIL` 是个**只会掩盖信息的桶**：它把"两侧都失败"当成同一件事，而失败的原因（编译期 vs 运行期、有没有输出）恰恰是最需要区分的。**将来若要给桶方案做一次升级，优先项是把 `BOTH_FAIL` 按"两侧失败发生在编译期还是运行期"再切一层**，而不是再加新的顶层桶。本轮不改桶方案（会牵动全部历史数字），只在此记下。

**⑤ 反直觉点：加护栏前 `test-std-hash.no` 的哈希是*稳定*的**

它稳定输出 `399229a5…`，却在 `des_block` 里有 2 处 `NoVal` 消费。即 `undef` 恰好落在了一个不影响输出的位置（LLVM 把 `undef` 折成了所需的值）。⇒ **"输出稳定"不是"没有 undef"的证据**，用哈希稳定性当无罪证明会漏掉整类缺陷；判据必须是"是否存在没有产生者的值"，那正是护栏在问的问题。（对照：`str-concat-leak.no` 的 `undef` 落在 len/data 字段上，于是长度任取寄存器残留 ⇒ 不确定。）

**⑥ 验证**

- `go build ./...` 通过；`bin/no` 重出。`gofmt` 只报告既存差异（改动前 `codegen.go` 就在 `gofmt -l` 名单里，全仓 67 个文件如此），我的新增行未被 gofmt 触碰。
- `go test ./mir/` 全绿；`go test ./...` 的失败包与既有集**逐条一致**（`fmt`/`build`/`checker`，无 `mir`）——即护栏不改 Go 层测试。
- `/tmp/fz/c1.no`：`rc=0` + 1169B 垃圾 → **rc=1** + `func _nolang_main: consumer reads value 0 (NoVal), which no instruction produced …`（同一构建把函数内每一处都列全，`test-std-hash.no` 的 2 处一次性报全）。
- 5 个正常探针（`d1` let 接收者 / `f1` 显式 i64 接收者 / `g1` 用户函数实参 / `e1` 先绑定再 print / `lit` 纯字面量拼接）rc 与输出**逐字节不变**。
- 未受影响的高频命中文件仍 rc=0：`test-map-generics.no`（9 处命中）、`test-map.no`（3 处）—— 全部属 `llvm="void"` 那类。
- **全量回归比对（对改动前的 `mir-baseline.tsv`，300s / `-P 4`）**：`SAME=389` / `DIVERGE=0` / `UNSTABLE=0` / **`REGRESS=1`**（唯一一条：`tests/mem-safety/str-concat-leak.no` (now rc=1)）/ `IMPROVED=0` / `BOTH_FAIL=32` / `NEW=0`（合计 422）。**唯一变动就是那一个文件，且已按 §12 #85 的花名册判定为"对上 oracle"**。注意 `UNSTABLE` 由 1 变 **0** 不是漏报：`str-concat-leak.no` 现在编译失败、stdout 恒空，指纹**重新变得可冻结**，`MIR_GOLDEN_UNSTABLE` 里那条已按脚本注释的约定移除（见 §12 #86）。
- **重冻结结果（`GOLDEN=tests/golden/mir-baseline.tsv GOLDEN_MIR=default scripts/mir_golden.sh -update`）**：422 条，`rc=0` **390 → 389**、`rc!=0` **32 → 33**。与改动前基线 diff **恰好 2 行**：

  | 文件 | 改动前 | 重冻结后 |
  | --- | --- | --- |
  | `tests/mem-safety/str-concat-leak.no` | `0 eb28fc09…` | `1 e3b0c442…` |
  | `tests/test-std-hash.no` | `1 399229a5…` | `1 e3b0c442…`（**rc 未变，只变 stdout**） |

  **没有任何其它行变动** ⇒ 本轮改动的影响面被**穷举到 2 行**（无 flaky、无附带改动）。两行的新值都与 `legacy-baseline` **逐字节相同**。
- **重冻结后按两份冻结基线 join 推导语义桶**（桶是这两列的纯函数，无需重跑 8 分钟）：`SAME=302 / DIVERGE=38 / REGRESS=0 / IMPROVED=49 / BOTH_FAIL=33`（=422）。对比第四十二轮的 302/38/0/**50**/**32**：**只有 `str-concat-leak.no` 一个文件从 `IMPROVED` 移到 `BOTH_FAIL`**（`test-std-hash.no` **本来就在 `BOTH_FAIL`** —— 它的 oracle rc 前后都是 1，所以"rc 桶"看不见它；这正是 ④ 的盲点）。**顺带：本轮先按直觉写成"两个文件都从 IMPROVED 移出（50→48/32→34）"，实测是 49/33 —— 猜数的方向对、个数错，验证一次就纠正了。**
- **护栏影响的完整穷举（`tests/` 之外）**：用护栏前后的两个二进制对 `test/`（40）+ `example/`（11）逐文件比 `build` rc，**恰好 1 个文件变化**：`test/std/process.no`（`pre=0 post=1`）。护栏前它 `no test` 时在 `t-cmd` 上 **SIGSEGV**，护栏后变成编译期点名诊断，**无测试由通过变失败**（见 §12 #85 的"第三个文件"）。`test/` 里另有 22 个 `no build` 失败**两版一致**、与护栏无关，作背景记录：7 个 `ovfhndld`（测试源陈旧，缺 `#{overflow=wrap}`）+ 15 个其它既有 MIR gap（`opt-verify: MIR IR failed LLVM verification`、`builtin fs.is-file: cannot marshal arg 0 as C string` 等）。`example/` 11 个中 1 个失败（`mysql-driver/src/mysql.no`），同样两版一致。

**⑦ 未闭环的部分（护栏不解决底层缺陷）**
`i.to-str()` 的 `call` 依旧**没有结果值**（MIR 证据 `call dst=0 args=[3]`），只是现在会响铃而不是静默。本轮把成因从"接收者绑错"更正为 **`lowerCall` 的 void 分支**：`resTyp` 的三档回退（HIR 定义 → 内建表 → LHS 类型提示）全落空时，调用被当成语句型 void 调用发射、返回 `NoVal`（`hir2mir.go:4959–4963`），于是上游 `+` 拿到 `NoVal`；同族能正常工作只是因为它们有外部类型提示。详见 §12 #85 的第四十三轮更正——**那里还记了本轮确证但未定位的一环（`callee` 的确切拼写，需要给 MIR 转储补 `Sym` 字段）**，以及 `codegen.go` 里第二条独立失效路径（`i64-to-str` 快路径在 `Results` 为空时静默 `return nil`）。修好之后这两个文件的 rc 应回到 0 且输出正确（`str-concat-leak.no` 应为 1000 行 `item0…item999`），届时需再次重冻结基线。**当前状态是"响铃"而非"修好"**，不要把它读成已修复。

**⑧ 一条操作纪律（本轮踩到）**

**不要在扫描运行期间重编译 `bin/no`。** 扫描脚本不复制二进制，`xargs` 起的每个子进程都在调用同一个 `./bin/no`；中途覆盖它，同一次比对就会**混用两个版本的编译器**，产出的桶计数无法解释。同理，**也不要在扫描期间编辑 `mir_golden.sh` 本身**（bash 增量读取脚本，改到未读部分会让后续行从错误的偏移解析）。本轮这两件事都发生了，处理方式是**杀掉重跑**而不是"反正只差一点"。衍生纪律：**杀掉扫描前先记下它可能留下的子进程**——被杀的扫描不会回收它已经 fork 出去的 `bin/no`（`test-for2.no` 是死循环，会一直占满一个核），而残留的高 CPU 进程恰好会污染下一次扫描的计时。

**⑨ 手工探针与预言机的 rc 必须分清（本轮差点写错结论）**

`no build` 的 rc = **编译器**成败；预言机记的是 `no run` 的 rc = **先编译再执行、程序自己的退出码**。本轮的探针全用 `no build`，于是得到"两个文件都是 rc=0 → 加护栏后都是 rc=1，都改了"，而按预言机口径实际上是**只有一个变了**（`test-std-hash.no` 早就是 rc=1）。若照前一种说法去写文档，就会虚构出一次并不存在的 `0 → 1` 转变，并把"桶没动"误读成"护栏没生效"。

⇒ 纪律：**凡是要写进"某文件从 X 变成 Y"的结论，必须用与预言机同一条命令（`no run`）在同一路径上取得**；`no build` 只用于判断"编译期是否失败"。两者的差异在"程序自身会失败"的测试上尤其致命——而语料里这类（自断言测试）恰恰不少。

**⑩ 语料 ≠ 仓库：`tests/` 之外的 51 个文件里还有一个**

本轮差点把结论停在"2 个文件"。上一轮的诊断扫描是 `find tests -name '*.no'`，所以那个数字**只在 `tests/` 内成立**。用护栏前后的两个二进制（`git worktree add /tmp/no-preguard HEAD` → 建旧二进制 → **用完立刻 `worktree remove --force`**）对 `test/`（40）+ `example/`（11）逐文件比对 `build` rc，**恰好多出一个**：`test/std/process.no`（`pre=0 post=1`）。而它的性质比语料里那两个更有说服力 —— 护栏前 `no test` 在**第 5 个测试**（正是 `t-cmd`，与护栏点名的 `t_cmd` 同函数）上 `signal: segmentation fault`；护栏后变成编译期点名诊断，**没有任何测试由通过变失败**。

⇒ 纪律：**给出"影响半径"时必须写明扫描范围**（这里是"`tests/**/*.no`，422 个"），并且**在一处修复的收尾阶段把范围扩到相邻目录**（`test/`、`example/` 各 11–40 个文件，全扫 2 分钟）。**代价极低、回报是一次完整的、可写进结论的穷举**；不扩，就会把一个不完整的数字当成完整的。

---

#### 13.3.16 第四十四轮（2026-09-17）：三个真缺口 —— option 解包的两个新 sink + `KCond`（三元）从未 lowering

**指标（金标 `mir-baseline`，422 语料）**

| 指标 | 上轮 | 本轮 |
|---|---|---|
| SAME | 390 | 389 |
| DIVERGE | 1 | 2（新增的 1 个是**修复**，见下） |
| REGRESS | 0 | **0** |
| IMPROVED | 14 | **16** |
| BOTH_FAIL | 17 | **15** |

新修 2 个：`tests/test-strconv.no`、`tests/test-all.no`。

**① `emitBuiltinConv`：option 流入标量转换时没有解包**

`std/str.no` 的 `str.to-f32` 写作 `f ?f64 = .to-f64()` → nil/err 两臂已 `return` → `val = number.f64-to-f32(f)`。存活下来的 `f` 是 `ok(payload)`，传给形参 `a f64` 前必须剥壳。`emitBuiltinConv` 直接把整个 `%option_f64` 交给 `coerce`：

```
builtin number.f64-to-f32: cannot coerce %option_f64 to double
```

修法：新增 `codegen.peelOptionValue(lt, val)`（`extractvalue <optLT> %v, 1`，payload 类型从 `c.optPayload` 查，`%option` 恒为 `i64`），在 `emitBuiltinConv` 里无条件试剥。**这是 §13.1「sink 解包」契约的又一站**：此前只覆盖 `emitIndexStore`/`emitSetField`/`print`，标量转换这一站漏了。

**② option 包裹的容器被索引：元素类型取错 + GEP 打在 option 结构体上**

`b = '6162'.from-hex()` 得 `?[]byte`，match 默认臂把 `it` 绑到 **option 本身**，于是：

- lowering 侧 `elementTypeOf` 见 `ty.Kind==KindOption` 就返回 `ty.Elem`（= `[]byte`），`it[0]` 的结果类型成了 `[]byte` 而非 `byte`；
- codegen 侧 `elemAddr` 拿 `arrT = "%option___byte"` 走 default 分支，发出 `getelementptr %option___byte, ptr %slot, i64 0, i64 %i` —— 结构体成员下标必须是常量，opt 直接拒：`invalid getelementptr indices`。

修法两处配套，缺一不可：
- `hir2mir.elementTypeOfType(ty)`：新增类型层递归，`KindOption` 时穿透到 payload 再取元素（`?[]byte` → `[]byte` → `byte`）；`elementTypeOf` 退化为「取值 → 调它」。
- `codegen.elemAddr`：option 先 `getelementptr <optLT>, <optLT>* slot, i32 0, i32 1` 拿到 payload 指针，再以 payload 为容器类型递归（原函数体改名 `elemAddrRaw`）。

⇒ 纪律：**「解包」类修复必须 lowering 与 codegen 两侧一起改**。只改一边不会报错更少，只会把 `opt-verify` 的类型错配从一处挪到另一处。

**③ `KCond`（三元 `c ? a : b`）在 MIR 里根本没有 lowering 分支**

`hir2mir.lowerExpr` 的 switch 没有 `case hir.KCond`（全仓 `KCond` 只在 `parser/tohir.go` 产生、在 `hir/hir.go` 声明，**没有任何消费者**），整棵表达式 lower 成 `NoVal`：

- `max = sum > 10 ? sum : 10`（未声明类型）→ `max` 永不被绑定 → 后续 `print('max: {max}')` 报 `interp: unresolved format field max`（`tests/test-all.no` 死的正是这一句）；
- `max i64 = sum > 10 ? sum : 10`（**已声明**类型）→ 走「声明但无初值」的零初始化兜底，**编译通过但恒为 0**。

后者比前者危险得多：它是**静默的错误值**，扫 rc 永远看不见。

修法：新增 `lowerCond`，把两臂当真分支降到共享结果槽，复用 `r = if c { a } else { b }` 已有的 `exprCapture`/`exprSink`/`captureArmValue` 机制。**唯一的结构差异**：结果槽在 `cond-br` **之前**的支配块里创建（`lowerIf` 的路径是在 then 块创建），否则 merge 块读它不满足支配关系。已验证：i64 / str / 函数内 / 真假两臂全部正确。

**④ 一个 DIVERGE 是修复不是回归（务必这样判）**

`tests/test-slot-rebind.no` 本轮新进 DIVERGE。判据不是「哈希变了」，而是**文件自带期望输出**：

```
// 期望输出（每行一个）: 42 7 1 1 17 5 5 10 100 99 84
```

用 `git worktree add /tmp/no-h HEAD` 建旧二进制对跑：

| | 输出 |
|---|---|
| 基线哈希对应 | `42 7 (空) (空) 17 5 5 10 100 99 84` |
| 本轮 | `42 7 1 1 17 5 5 10 100 99 84` |

两个空行正是三元结果——**修复前 `x1 = qi == ri ? 1 : 0` 连 0 都没打印出来**（`print` 收到 void）。⇒ 纪律：**DIVERGE 条目先找「谁对」的独立判据**（测试文件自带的期望、另一份 TSV、旧二进制对跑），哈希本身只会告诉你「变了」。

`tests/tmp-icmp-test.no` 那条 DIVERGE **不是本轮引入**：HEAD 二进制与本轮二进制 md5 完全相同（`1dcca233…`，输出就是 `5`），说明它与冻结基线之间的差异先于本轮存在，属环境相关（raw socket）。

**影响半径（按第十三轮⑩纪律扩到相邻目录）**：`test/` + `example/` 共 **51** 个文件，用前后两个二进制逐文件比 `build` rc，**差异 0**。`go test ./mir/` 全绿。

**剩余 15 个 BOTH_FAIL 的重新分类（本轮勘定）**

| 类别 | 文件 | 性质 |
|---|---|---|
| 负测试（**失败即正确**） | `i.no` | `print(a.len())` 本就该编译失败；MIR 报 `builtin str-len receiver i64`，rc=1 ✓ |
| FFI / 外部库 | `test-database-sql`、`test-ffi-mysql`、`test-ffi-sqlite`（`unknown callee sql.db.exec`）、`test-sse`（`sse.connect`） | 需 FFI 实现，非本轮 |
| **语言特性缺口**（两后端都无） | `test-tagged-enum`（`unknown callee full`；`KVariant` 在 `src/mir` 里**零引用**，JS 后端同样 skip）、`test-safe-index`（`#{index-out=DEF}` 要求 OOB→`?elem`，MIR 的 `emitIndex` 无边界检查） | 不是 MIR 的锅；改 `OpIndex` 全局语义风险高，单独立项 |
| 测试源与 std 漂移 | `test-json`（`p.stringify(root, out-buf, 0)` 3 参，现行签名 `(node-idx i64) (out str)`）、`test-basic`（`bigint.gcd(12, 18, r)` 传 i64 给 `bigint` 形参 → bus error） | **修测试**，别改编译器 |
| 深层运行时崩溃 | `nested-container-clone`、`test-json-parse-option`、`test-https-server`、`test-basic` | SIGSEGV，逐个挖 |
| #85 家族 | `test-std-hash` | `des_block` 里 `NoVal`；触发点是 `std/crypto/des.no` 把 `#{index-out}` 写在**续行的中缀表达式中间** |
| 有意死循环 | `test-for2` | rc=124，设计如此 |
| 待定 | `test-x25519-fe-diag`（abort trap） | 未挖 |

⇒ 本轮最大的认知修正：**上一轮把 `i.no`、`test-json`、`test-basic` 都归进「Codegen 类型问题 / 深层运行时」，实际三个里有两个是测试源自身的问题**（`i.no` 是负测试、`test-json`/`test-basic` 是 API 漂移）。**给 BOTH_FAIL 分类时必须先看测试源在说什么**，否则会把「修测试」的活当成「修编译器」的活排进路线图。

#### 13.3.18 第四十五轮（2026-09-17）：两个语言级缺口（tagged enum / safe index）+ option 槽位默认值 + std int 哈希模板空守卫体

**指标（金标 `mir-baseline`，422 语料，重冻结前口径）**

| 指标 | 第四十四轮末 | 本轮 |
|---|---|---|
| SAME | 389 | 387 |
| DIVERGE | 2 | **4**（4 个**全部**判为「基线记的是错行为」，见 ⑤） |
| REGRESS | 0 | **0** |
| IMPROVED | 16 | **18** |
| BOTH_FAIL | 15 | **13** |

新修 2 个（rc 1→0）：`tests/test-tagged-enum.no`、`tests/test-safe-index.no`（并带动 `test-safe-index-containers.no`、`test-safe-index-utl.no`、`test-map-generics.no`、`mem-safety/map-tombstone.no` 的输出变为正确）。
**回归 oracle 重冻结后权威口径：`SAME=409 / DIVERGE=0 / REGRESS=0 / IMPROVED=0 / BOTH_FAIL=13`。**

**① tagged enum（`hir.KVariant`）落地 —— 此前是全后端缺口**

`KVariant` 在 `src/mir` 里**零引用**，JS 后端同样 skip，所以这不是 MIR 缺口而是语言级缺口（第四十四轮已如此判定）。本轮补齐：

- `mir.go`：新增 `KindEnum` 与 `%tenum_<name> = { i64 tag, [N x i64] }` 布局（payload 是**各变体共享的槽数组**，不是 per-variant struct，字段访问按槽位 bitcast）；新增 `Module.TaggedEnums`（含 `VariantInfo{Name, Tag, Fields, FieldNames}`）与三条指令 `OpEnumNew` / `OpEnumTag` / `OpEnumField`。
- `hir2mir.go`：`collectTaggedEnums` 采集定义；`enumVariantOf(prefer, variant)` 解析变体名 —— `prefer` 优先取绑定点的声明类型，无提示时回退**本包**枚举的**唯一**变体名（`localEnums` 界定回退范围，见 ④）；构造器 `lowerEnumCtor` / `lowerEnumUnit`，且 `resolveCallee` 与 `KIdent` 两个入口都要接（变体名有 `full(7)` 与裸 `empty` 两种形态）。
- **arm 析构绑定**（`ok(v) -> print(v)` 的 `v` 投影到 payload）踩了三个坑，每个都是「顺序/身份」问题而非类型问题：
  - 投影判断必须在**局部绑定之前**做。`a-res` 与 `b-res` 两个 arm 都把载荷叫 `v`，locals-first 会让第二个 arm 读到第一个 arm 的旧绑定（`tests/test-tagged-enum.no` 印出空串）。
  - arm 的 `it` **槽位在 arm 之间复用**（同一 match、甚至同一函数内的不同 match），其声明类型可能是**上一个 arm 的** → 不能拿它做枚举类型校验；以 arm 自己的 `armVariant` 变体表为准，投影源用 arm 条件处捕获的 subject 表达式在 arm 体开始时**重新求值**（`armSubjectID` + `itSrc`）。
  - 实参位置的变体（`area(circle(2))`）需要 `lowerCallArgs` 按形参类型设 `typeHint` 才能消歧。
- 比较只比 tag：`emitCmp` 新增 enum↔enum 走 `%tenum` 的 field 0。
- 所有权：`OpEnumNew` 的载荷**转移进** enum（`analysis.go` 的 `moveSrc` + 泄漏检查两处），否则 `q b-res = ok('hi')` 的字符串会在块尾被 `str_free` 掉、arm 里印出空。

**② safe index（`#{index-out=N}` / `x ?= v[i]`）落地**

`lowerIndex` 此前**无视绑定点的 `?elem` 声明**，一律发射裸 `OpIndex`：越界读到垃圾内存后**包成 `ok(垃圾)`**，`ok` 臂随即把垃圾赋给目标（`test-safe-index-utl.no` 的越界调用在整个 HEAD 里印出 `untyped-local x = 8415131232`，而该文件头注释明确要求越界时 some 分支**不得执行**）。

- 新增 `lowerSafeIndex`：`OpLen` + `0 <= i < len` → `ok(elem)` / `nil`；结果槽在分叉**之前**以 nil 常量创建（MIR 无 phi，每个分支汇聚都经过这样一块共享槽）。
- 元素为 owned（`str`）时，`OpIndex` 读出的只是**借用**；包进 option 会让 option 成为 owner，随后 drop 会释放容器仍持有的缓冲 → 必须先 `OpClone`。

**③ option 槽位的默认值不是它的零值（`codegen.go` 的 `allocaFor`）—— 本轮最有价值的一处**

`%option` 是 `{ i64 tag, payload }`，**tag 0=ok / 1=nil / 2=err**，所以 `zeroinitializer` 读回来是 **`ok(0)`**，不是 nil。旧行为只对 owned 槽写 `zeroinitializer`，于是一个**从未被赋值的 `?T` 具名出参**要么是 `ok(0)`、要么是未初始化垃圾、要么是**同一槽位上一次调用留下的 `ok`**。

后果最典型的是 `hashmap.get`：未命中时它只是从探针循环里掉出去（`result` 从未被写过），调用方于是看到**上一次调用写进同一槽位的返回值**。`tests/mem-safety/map-tombstone.no` 里已删除的 key 仍报 `c-found`，`test-map-generics.no` 的 `remove` 之后 `contains` 仍为 1。

修法：option 槽一律以 **nil 常量**初始化（`store %option_i64 { i64 1, i64 zeroinitializer }, ...`）。

- **与既有设计的关系**：`docs/docs/lang/code-style.md` 早已定义「具名出参**延迟零值**」——prologue 不做初始化，legacy 用 `%__ret_init_bitmap` 追踪是否显式赋值、在 **return 处**按型别补零，且**明确写了「option → `nil`」**。MIR 从未实现这套 bitmap。本实现把同一语义放在 **prologue**：观察等价（唯一差别是「未赋值即读」从「未定义」变为「nil」），代价是每个 option 出参多一条会被 LLVM 折叠的 store。
- **顺带修掉两个既有崩溃**：`print(<nil option>)` 从 `trace/BPT trap` 变为正常印 `nil`；option 出参早返回后调用方的后续语句不再**静默截断**（`f = (k i64) (r ?i64) { k == 0 -> return ... }` 连调两次，第二次之后整个程序停止输出）。

**④ std 的 `[i64]K` 哈希模板有空守卫体（`src/std/collection/map.no`、`static-hashmap.no`，int 模板 put/get/remove 共 6 处）**

```nolang
.occ[idx] == 1 -> {
    #{index-out = 0}
    .keys[idx] == key -> {     ; ← 守卫体为空
}
    #{index-out = 0}
    result = .vals[idx]        ; ← 与 return 一起落在守卫之外
    return
}
```

键比较形同虚设：只要 `occ[idx]==1` 就无条件返回该槽的值。实测 `[i64]i64` map `remove(2)` 后 `contains(2)` 仍为 1，`test-int-tombstone` 期望 `2-removed-ok` 却得到 `2-found`。str 键模板写法正确（它必须先 `eq = .str-eq(k, key)` 再比较），所以只有 int 模板中招。

- **已排除「fmt 事故」**：`no fmt -d src/std/collection/map.no` 只规范化 `#{index-out=0}` 的空白与几行缩进，**不会**把语句挪进守卫体 ⇒ 是源码 bug。
- **同形状全仓还有 5 处**（`net/multipart`×2、`net/sse`、`regexp`、`crypto/x509`）**本轮未动**：没有 oracle 判定其意图（可能是「有意的空操作」），也没有任何已知现象指向它们。

**⑤ 四个 DIVERGE 逐个判「谁对」——全部是基线错**

| 文件 | 基线（旧 oracle） | 本轮 | 判据 |
|---|---|---|---|
| `tests/test-slot-rebind.no` | `42 7 (空)(空) 17 …` | `42 7 1 1 17 5 5 10 100 99 84` | 文件第 5–6 行自带期望输出，逐字相同 |
| `tests/test-safe-index-utl.no` | 越界时印出 `8415131232` | 越界时不执行 some 分支 | 文件头注释明确要求 |
| `mem-safety/map-tombstone.no` | `c-found` / `name-found` | `c-removed-ok` / `name-removed-ok` | 第 34 行注释「验证已删除的 key 查不到」+ nil 臂的命名 |
| `test-map-generics.no` | `1` | `0` | 第 21、131 行 `print(found); expect: 0`（紧随 `remove`） |

⚠️ **`legacy-baseline` 对这 4 个文件全部记的是 `rc=1` + 空 stdout 哈希**（legacy 根本编译不过），所以**没有语义 oracle 可用**，只能以测试自带的 `expect:`/注释为准 —— 判读时不要拿「legacy 一致」当论据（§13.3.14 ① 的同一教训）。

**⑥ 回归 oracle 重冻结：25 行变化，三类，逐类可解释**

`GOLDEN_MIR=default scripts/mir_golden.sh -update` 后与旧文件 diff **恰好 25 条**：

1. **4 条 DIVERGE** —— 上表，全部是「基线记的是错行为」；
2. **18 条 IMPROVED** —— rc 1→0，仓上唯一 `-update` 允许的方向；
3. **3 条 BOTH_FAIL 的陈旧哈希** —— `nested-container-clone`、`test-https-server`、`test-x25519-fe-diag`：rc 都是 1，但指纹与冻结时不同（两个从「空 stdout」变成「有 stdout 后崩」，即失败点从编译期移到了运行期）。**用 HEAD 二进制复核三者输出与本轮逐字节相同** ⇒ 漂移早于本轮（甚至早于本 session 的 HEAD），`-update` 只是顺带纠正。**若不是沿 diff 逐条核对，这 3 条会完全不可见**（§13.3.17 ④ 的第二次印证：`BOTH_FAIL` 桶比 rc 不比哈希）。

**⑦ 判读方法（本轮新增，可复用）**

- **「行为随代码布局漂移」是读未初始化内存的签名**。定位 `map-tombstone` 差异时逐次消融：禁用 safe index → 恢复基线行为；**只保留块结构、强制 `ok=true`** → 又回到基线行为；把界从 `OpLen` 换成 `OpCap` → 仍与基线不同；在 `none` 臂加诊断打印 → **诊断一次都没触发**，行为却再次改变。四步合起来的结论不可能是「边界判定错」（那样强制 ok 应当无影响），只能是**该槽位本身没被写过** ⇒ 直接指向 `allocaFor`。教训：当消融操作本身会改变被观测现象时，别继续假设「我的新代码算错了」，要假设「我在读没被写过的内存」。
- **`allocaFor` 的零初始化只在 `owned` 时发生**，而 option 的「零值」与「默认值」不同 —— 任何新增类型 Kind 时都要问一遍「它的 `zeroinitializer` 语义正确吗」。

**影响半径**：`tests/**/*.no`（422，全量金标扫描）+ `test/`（40）与 `example/`（11）共 51 个文件逐个与 HEAD 对拍：**`build` rc 差异 0；`run` 输出差异 1，且是改进** —— `test/std/enum_cross.no` 从 `passed: 0 / total: 0` 变为 `passed: 2 / total: 2`（跨模块枚举 `file-mode.write` / `file-perm.perm-644` 的 match 此前完全没跑起来）。
单测红线：`go test -count=1 ./mir/` 全绿；`go test ./...` 的失败集与 HEAD worktree 逐项相同（`build` 4 + `checker` 2 + `fmt` 2）。

---

## 14. 溢出默认（overflow-default）与 MIR 的集成（进行中）

`#{overflow}` 默认使整数 `+ - * /` 返回 `option<int>`（不 panic）。当前状态：
- checker 侧：`ValidateUnhandledOverflow` 对未注解/未 `?=` 的算术报错（硬错阻断构建）；std 豁免已撤除，131 处已用 `#{overflow=wrap}` 修。
- **MIR 侧（中段→表达式边收尾）**：HIR 算术节点已被打成 `?i64`（option），`OpOptionWrap` 构造 `{tag,payload}``。sink 站点 `emitIndexStore`+`emitSetField` 的解包（§13.1）+ **算术/负号结果包回**（§12 #15）已落地，`test-arr.no`、x25519 系列、fe-mul 等 opt-verify 失配已闭环。

**下一步（闭环 §13.1 与溢出集成）**：`opt_verify` 家族算术/负号路径（#15）、`emitBitwise`（#18）、`print` payload 路由（#19）已闭环；`err/ok/some` 构造器 CALL（#23）已闭环。**剩余 opt_verify（§13.3.1，4 个，实测为独立深坑）**在**非算术的表达式路径**（字符串算术运算符重载推断 / async void 操作数 / struct-field-on-option GEP / void 数组 alloca），需在对应表达式 emit 处加同法 `extractvalue`/`insertvalue`，修复模式已统一（`unwrapOptionOperand` + §12 #15 的包回）。§13.3.4 顶层 main 合成（#17）已闭环。溢出默认 integration 的 MIR 侧算术环节现已基本闭环，剩余是表达式边的零散补全。

### NOLANG_MIR_DUMP_* 调试开关
- `NOLANG_MIR_DUMP_MIR=1`：打印 lowered MIR 到 stderr。
- `NOLANG_MIR_DUMP_LL=1`：写出 emitted LLVM IR 到临时 `.ll`（路径在 stderr）。
- `NOLANG_MIR_DUMP_HIR=1` / `NOLANG_MIR_DEBUG=1` / `NOLANG_MIR_DUMP_BAD=1`：辅助诊断。
- `NOLANG_MIR_DEBUG_UNDEF=1`（**第四十二轮新增 / 第四十三轮起为附加日志**）：打印 `loadVal` 里"类型非 void 却无槽位"的每一个点（`[mir-undef] func=… value=… llvm=… slot=…`）。第四十二轮它是 **#85 的测量入口**（env 门控、零行为变更，用于量化"改成 `c.fail` 会影响多少用例"）；**第四十三轮护栏落地后**该分支已无条件 `c.fail`（`lt == "void"` 那支仍是合法的 `return "void","undef"`，不打印也不报错），所以此开关现在只多打一份 stderr 日志，供扫描脚本 grep。⚠️ 判断命中时要**看 `value=` 字段**：`value=0`（`NoVal`）才是真洞，其余（真实值 id）是合法的 void 分支 —— 第四十二轮把两者混为一谈，把影响半径高估了 10 倍（§13.3.17 ①）。

---

## 15. 分阶段路线图（修正为真实进度）

- **Stage 0（已完成）**：`src/mir` 核心（模型/Builder/CFG/分析/Liveness/所有权/Drop/Move·Borrow/printer/validator/HIR→MIR lowering）+ `NOLANG_MIR=1` 验证模式 + 单元测试。MIR 作为审计器运行，现有构建零回归。
- **Stage 1（已跳过）**：`MIR→HIR` 桥未实现（直发路径更优）。
- **Stage 2（已完成）**：`MIR→LLVM` 直接发射（`EmitLLVM`），strangler-fig 回退（`NOLANG_MIR=2`）。
- **Stage 3（进行中 · 当前前沿）**：`NOLANG_MIR=3` 全量 corpus 门禁。**第十四轮权威全量重扫（2026-09-12，`./bin/no`，421 文件）**：`MATCH=248`、`DIVERGE=58`、**`MIR 专属 gap=17`**、`LEGACY_FAIL=97`、`HANG=1`——**red-line 已达标（MIR 专属运行时崩溃 = 0）**，gap 由第十三轮 21 降至 17。第十三轮实测的 3 个 MIR 专属运行时崩溃（`async-shared-race`/`test-slot-rebind-unsafe` SIGSEGV、`test-embed` trace-BPT）已由 §12 #41 全部闭环（同时连带闭环 E 家族 `async-str-result`/`async-str-stress`）。累计闭环：§13.1 `emitIndexStore`/`emitSetField` option 解包、`str-clear` 内置、§12 #15 算术/负号 option 包回（解锁 13 个）、#16 数组 receiver 路由（解锁 `test_zero.no`）、**#17 顶层容器字面量物化（§13.3.4，解锁 15 个 has_no_slot/index_slot/receiver）**、#18 `emitBitwise` option 解包包回、#19 `print` 内联 option payload 路由（解锁 3 个 opt_verify）、**#20 `%txt` str-len-bytes receiver**、**#21/#22 arr-slice 切片（元素型回推+边界语义）**、**#23 `err/ok/some` 构造器 CALL 路由**、**#24–#27 mem-safety 崩溃族（定宽数组→slice 归约 / `@main` out-param / 结构体字面量字段所有权）（解锁 9 个，red-line 达标）**、**#28 字符串插值（解锁 15→1）**、**#29–#35 切片越界/窄整型/类型推导/内建补齐（解锁 20+）**、**#36 顶层未初始化全局物化（闭环 `test-x25519-keypair-diff`，+1）**、**#37 `net.net-icmp-open` raw socket FFI（闭环 `tmp-icmp-test`）**、**#38 `net-dial`/`net-send`/`net-recv` FFI 内置（闭环 `test-net-client` 构建）**、**#39 `await`/`run` coroutine 完整 task 运行时（闭环最后一个 MIR 专属 gap `test-async.no`，MIR 输出比 legacy 正确）**、**#40 async 取消/让出内置 `async-cancel`/`async-cancelled`/`async-yield`（收尾完整运行时契约；3 个新测试 MIR=3 全过，其中 `tmp-async-yield` legacy SIGSEGV 而 MIR 正确）**、**#41 第十四轮 C 家族 red-line 3 崩溃全闭环**（`run` 语法化异步 / 异步实参按形参类型强制 / 任务结果类型跟踪 / `#{embed}` 物化；连带闭环 E 家族 2 个 → gap 21→17、MATCH 240→248、**MIR 专属运行时崩溃 = 0**）。MIR 专属 gap 由首轮 69 经多轮降至 **17**（第十四轮实测）。下一步优先级：A 家族（4：fs-open 下游）→ B/D（7：`unknown callee`/`opt-verify`）→ E/F（2）→ `test-diff-debug`（interp）。
- **Stage 3 续（第二十九轮 2026-09-15，`./bin/no`，421 文件 / 实测 420）**：**`MATCH=364`（≈87%）、`DIVERGE=1`、`MIR 专属 gap=2`、`两模式都失败=53`、`HANG=1`**（第二十八轮同口径 352/0/0/68/1，净 **MATCH +12、失败 −15**）。本轮四项修复：**#51** `std/process.no` 词法硬伤（解锁 4 个测试的 legacy 基线）、**#52** 溢出检查器三类误报（顶层语句共享 `varTypes` + 未标注绑定类型推断 + match 表达式臂末值不再当丢弃，解锁 9）、**#53** MIR 补齐 `net-listen`/`net-accept`/`net-udp-open`（`test-tcp-fork` 首次跑通）、**#54** KLet 回退分支把模块全局改型为 `void` → `undef` → 顶层变量参与 match 的 SIGTRAP（#52 解开误报后才暴露，闭环 `tests/match.no`/`option.no`）。**方法论要点**：修"两模式都失败"的公共依赖（std/checker）会让 MIR_GAP 先升后降——gap 从 0 变 2 不是回归，而是 3 个此前被掩盖的真缺口浮出；因此每轮必须**先跑基线 rc 做二分**，只看 MIR=3 的 rc 会系统性误判。
- **Stage 3 续（第三十一轮 2026-09-15，`./bin/no`，421 文件全量纳入）**：**`MATCH=367`（≈87.2%）、`DIVERGE=0`、`MIR 专属 gap=0`（标签值；**抖动校正后为 1**）、`两模式都失败=53`（CERR 39 + CRASH 14）、`HANG=1`**。三项产出：① **#63** 修掉 `emitCallBody` 把"显式写出的具名出参实参"误判为变参展开（新增 `mir.Function.Variadic` 作消歧依据；该 bug 在语料里无对应形状，只有做最小复现才逼得出来——见 §12 #63 与 §13.3.10 ④）；② **修好扫描器超时工具缺陷**（`run_to` 由 `perl -e 'alarm; exec'` 改为 `setpgid` + `kill -KILL -$pgid` 杀整个进程组，并补 `HANG_LEGACY` 桶与修正 `grep -c "^HANG"` 误统计 —— `test-for2.no` 不再卡死整轮，见 §13.3.10 ①）；③ **抖动甄别**：查实第三十→三十一轮的 `gap 1→0`、`CRASH 13→14`、`DIVERGE 0→1→0` 全是两个 flaky 文件（`test-diff-debug.no` 的 legacy 基线 1/6 失败、`test-quant-all1.no` 打印未初始化栈指针）造成的**标签互换，不是任何修复的效果**；对 `CRASH` 桶 14 个逐文件重复采样确认 13 个为确定性双失败（见 §13.3.10 ②）。**方法论要点**：① 小改判据也必须全量重扫——#63 第一版"无条件扣减"当场让 `test-number-generic.no`/`test-number.no` 回归（gap 0→3），加 `cf.Variadic` 判据后才回落（§13.3.10 ⑤）；② **计数在有歧义时不可用作判据**，必须找语义标记（`FlagVariadic`）；③ 沿一个失败测试挖最小复现，常能挖出比它更普遍的真 bug（本轮 #63），而只盯 gap 计数永远看不到它；④ **当基线自己 flaky 时，"基线过、MIR 挂"这个判据本身不可靠**，`MIR_GAP`/`DIVERGE` 这类差一位指标必须重复采样定真值。
- **Stage 3 续（第三十轮 2026-09-15，`./bin/no`，421 文件 / 实测 420）**：**`MATCH=367`（≈87.4%）、`DIVERGE=0`、`MIR 专属 gap=1`、`两模式都失败=52`、`HANG=1`**（第二十九轮同口径 364/1/2/53/1，净 **MATCH +3、DIVERGE −1、失败 −1、gap −1**）。本轮八项修复：**#55** MIR 缺失的平台过滤（新增 `src/mir/platform.go`，`process.cmd` 的 POSIX/Win32 同参变体不再塌缩到同一 mangled 符号）、**#56** 重载 mangling 后的 callee 名解析（`resolveOverloadedFuncName` + `mangledSuffixMatches` 反向校验，闭环 `unknown callee i64.trim` 这一"症状与根因完全脱节"的误导性诊断）、**#57** 泛型切片方法名回落具体元素类型（`[]byte.slice` 优先于 `[]t.slice`）、**#58** 补齐 `process-pipe`/`process-waitpid-nohang` 内置 + `process-kill` 改 `CmpRet`、**#59** `emitCmp` 的 option 语义二修（option↔option 比 tag、option↔普通值比 payload；`%str-long` 的 `!=` 从"等同 `==`"改为 `xor @str_eq`）、**#60** `break` 蹦床块的重复 start-drop → 双释放（`redundantStartDrop` 沿单后继链消冗余，确定性顺序保证纯 dropper 环不会消掉全部 drop）、**#61** `with-cap/with-len/with-cap-len` 的元素步长硬编码 8 + 新缓冲未清零（`[]str` 需 24 字节槽位；新增 `mirStaticTypeSize`/`typeSizeOperand`（`ptrtoint(gep(T, null, 1))` 让 LLVM 折叠出精确 `sizeof`）/`allocBytesOperand`，并在两条 malloc 路径补 `llvm.memset` 清零以镜像 legacy）、**#62** 回退一个**未提交**的 `emitFunc` 参数别名收窄改动（它引入 5 个 MIR_GAP）。**方法论要点（本轮最重要的产出）**：① **文档里的 gap 数只代表"上次扫描时的二进制"**，工作树里未提交的改动必须重扫才能计入结论——若信任文档的 "gap=2" 会漏掉 `emitFunc` 引入的 5 个；② 判定"是否本轮引入"必须 `git worktree add` 建**干净 HEAD 基线**，并把工作树改动**逐 hunk** `git apply -R` 二分（本次据此把 5 个 gap 精确钉到 `emitFunc` 的两个 hunk）；③ 代码注释里"因为某机制所以安全"的推理要回到源码核对（`droppable` 里一行 `!isParam[inst.Dst]` 就证伪了整段"别名会被 callee drop"的说法）；④ 修"两模式都失败"的公共依赖会让 MIR_GAP 先升后降（第二十九轮的 gap 0→2），**"两模式都挂"不是稳态**。

- **Stage 3 续（第三十七·三十八轮 2026-09-15/16，`./bin/no`，421 文件全量）**：**`MATCH=387`（≈91.9%）、`DIVERGE=1`、`MIR 专属 gap=0`、`两模式都失败=29`（CERR 18 + CRASH 11）、`HANG=4`；默认口径（不设 `NOLANG_MIR`）通过 `388/421`（92.2%）**。第三十七轮四修：**#67** `collectVarTypesFromBody` 把"已声明局部的再赋值"误当"遮蔽全局"而删掉其型别 → `[n]t.clone` 从未单态化；**#68** MIR 的 print 家族容器实参未走 `to-str`（`printableValue`/`toStrCalleeFor` + 独立 `wrapPrintArgs` 标志）；**#69** `net-dial`/`net-send`/`net-recv` 的 option 载荷与定宽数组实参序列化 + 补齐 5 个 net 内建；**#70** `std/crypto/aes.no` 源码笔误 `ek[ek] = ...`（净 MATCH +5）。第三十八轮四修：**#71** MIR 从未处理 `hir.KRegexLit`（正则字面量在 legacy 是 codegen 期脱糖，MIR 无 codegen 期 AST → 字面量不产生值 → 表象为"未解析格式字段"）；**#72** `lowerFormatField` 的 `default:` 把结构体当整数；**#73** 默认后端切换为 MIR-only（第二优先级 #8 落地）；**#74** `transitive_import_test.go` 接受 MIR 的符号净化拼写。**方法论要点**：① "两模式都失败"必须先分类——本轮 29 个里只有 **3 个**是 MIR 的事（`test-strconv`/`test-std-new`/`nested-container-clone`），**14 个是测试源陈旧**（API/语法漂移）、8 个两模式同崩同挂、3 个 legacy 专属编译缺陷（`%addopt.final`）、1 个负测试；② 扫掠脚本先判 `rc3` 再判 `rc2`，故 `HANG_LEGACY=0` **不等于**基线没挂；③ 调试期不要把诊断细节丢在桶名里。详见 §13.3.11/§13.3.12。

- **Stage 4（下一步）**：① 收敛最后一个 MIR_GAP `test-diff-debug`（DP 表全 0 / `bus error`；根因落在 `[]str` 元素的 `compare` 调用降级路径，探针显示 `with-len`+字面量赋值的 `[]str` 元素方法调用在**两模式**都 segfault → 需先修更底层的通用缺陷）；② `with-len`/`index dst slot` 的 void 型别族（#54 同族，5）；③ `str-len receiver i64`（3）；④ 语料迁移 8 个真·未标注溢出运算；⑤ `net-dial` 非 IP 字面量 host 的 `getaddrinfo` 回落（3）；⑥ ~~修 `run_to` 超时只杀 `no` 不杀子进程组的问题~~（**已完成**，`setpgid` + `kill -KILL -$pgid`，见 §13.3.10 ①）；**⑥'（第四十一轮已结清）`HANG=4`**：~~`test-parse-min.no`、`mem-safety/test-json-parse-option.no`、`mem-safety/test-json-nested-match.no` 的 json parse 与 `test-for2.no`）——两后端同挂，属 std 缺陷~~ —— **该判断已被证伪**：三个 json 文件是**编译期爆炸**（62s > 90s 预算；legacy 侧 rc=1 从未跑起来过），`test-for2.no` 是**有意的 `while(true){}`**。见 §13.3.15 与 §12 #82/#83/#84。⑦ 逐站消除 §13.3 长尾；FFI/async/crypto/net/map/字符串方法内置补齐；**（`no build` 默认走 MIR=3 已于第三十八轮 #73 完成）**；⑧ ~~**#9/#10：移除 strangler-fig 回退、清理 legacy 后端（`src/build/llvm/`）并更新构建系统**~~ —— **已完成（第三十九·续轮）**：legacy 后端已删除（48 文件 / 50,006 行 / 2.0MB，非旧记的"约 60KB"），`NOLANG_MIR=0/2` 改为明确报错；构建系统**零改动**（`Makefile` 的 `GO_SOURCES`/`NO_SOURCES` 都是 `find` 通配，删文件自动适配）。回归保护改由 `tests/golden/*.tsv` 冻结基线承担。详见 §13.3.13 ⑥。

- **Stage 4 续（第三十九·续轮 2026-09-16，已落地）**：**删除 legacy 后端**（`src/build/llvm/`）+ 移除 strangler-fig 回退 + 冻结双基线 oracle。验证：`go build ./...` / `go vet ./...` 通过；`go test ./...` 失败集与干净 HEAD worktree **逐条一致**（`fmt` 2 + `build` 4 + `checker` 2，全部既有）；golden 比对 `SAME=388` / `REGRESS=0` / `DIVERGE=1`（已知假阳性）/ `BOTH_FAIL=33`。**遗留：改动未提交**（见 §13.3.13 ⑥ 与末节）。**下一步（第三优先级）**：① ~~把 MIR 的单测从 6 个补到与 legacy 188 个相当的覆盖~~（**已启动：第四十轮 6 → 13**，新增 `src/mir/platform_test.go` 覆盖**平台变体解析**这一零测试区，并顺带更正 §13.3.13 ① 的依据、记录两个"两后端共有"的既有限制 —— 详见 §13.3.14）；**剩余重点**：`datalayout`/`triple`（MIR 已无硬编码 triple，宿主即目标，无可测对象）、**溢出注解读取**（MIR 侧完全无此概念，见 §13.3.14 ④）、§13.3.13 ③ 分类 B 的其余主题；② ~~`HANG=4`（两模式死循环，std 缺陷）~~ **第四十一轮已结清**（三个 json 用例是编译期爆炸、`test-for2.no` 是有意死循环，见 §13.3.15），`DIVERGE=1`（地址差异假阳性）的正式标记待做；③ §13.3.13 ③ 分类 C 的 68 个 IR 文本断言测试，按"删后出现回归再补"的增量策略迁移；④ 若要让溢出模式**真正生效**，需先改语言实现（§13.3.14 ④），再在 MIR 侧引入 `curOverflowMode` 上下文与 `Inst` 级模式分派；⑤ 若要修**函数体内**平台变体不过滤（§13.3.14 ③），`TestFunctionBodyPlatformVariantIsNotFiltered` 会失败并提示改期望值。

- **Stage 4 续（第四十一轮 2026-09-16）：`HANG=4` 的证伪 + 预检去重（编译耗时 −50%）**：**`HANG 4 → 1`，且剩下那一个（`test-for2.no`）是设计上有意的死循环**。三步：① 用**冻结基线的 rc** 推翻"三个 json 用例两模式都死循环"的旧结论 —— legacy 对它们 rc=1（**从未跑起来过**），MIR 的 rc=124 是**编译期爆炸**（62s > 90s 扫描预算）；② 根因定位：`sroa` 无法廉价处理 MIR 产出的 22KB 内联聚合（`%json_json_pool = { [64 x %json_json_value], i64 }`，`alloca [64 x %json_json_value]` 出现 **81 次**），单跑这一个 pass 就 458× 膨胀（229KB → 105MB）；③ 落地 **#82**：预检不再重复整条后端流水线（`opt`+`llc` 各跑两遍 → 只跑 verifier、不汇编），11 行 json 程序 **62s → 32s**、`print('hi')` 不变、`NOLANG_MIR_PREFLIGHT=full` 保留旧行为。**副产品 #84**：两个 json 用例被"解锁"后暴露运行期 rc=1（此前从未被执行过）。**明确未修 #83**：IR 形态本身（值语义大数组字段被整体物化）。详见 §13.3.15。

- **Stage 4 续（第四十二轮 2026-09-16）：`rc=124` 语义清理 + golden 骨架加固；证伪"地址差异假阳性"，挖出静默错误编译 #85**：**`rc=124` 4 → 1**（只剩 `test-for2.no`，即那个有意死循环），`DIVERGE` 从"恒 1"变为 **0**。四步：① 用 #82 改动**之前**冻结的基线与改动后后端做全量比对，422 条**逐条一致**（`SAME=388`/`REGRESS=0`）⇒ #82 行为保持，且**不需要重新冻结**（原计划的理由本身是错的）；② 顺着"`test-parse-min.no` 串行 32.9s/rc=0 却在基线里记 124"这个口径矛盾，查出 `rc=124` 混装了第三种来源——**`-P 8` 并发争抢**（指纹是空输出哈希，即根本没跑到运行）；超时的文件**无法报告任何行为回归**，正是它掩盖了 #84 两轮；③ 落地 **#86**：超时 90s→300s、并发 8→4、`-update` 非默认 oracle 守卫（防止 `legacy-baseline.tsv` 被全失败垃圾覆盖，且故意不留开关）、`UNSTABLE` 分流（`str-concat-leak.no`）；④ **反转一条件持续三轮的结论**——`str-concat-leak.no` 的哈希不一致**不是**"二进制地址差异假阳性"（该文件输出 1000 行纯文本、**不含地址**），真因是 **#85**：`print('item' + i.to-str())` 在 count-for 里把拼接右操作数降级成 **`undef`**（IR 直证 `str_concat(%str-long %lv13, %str-long undef)`，且 `i.to-str()` 的 `call` 指令根本没生成），于是**以 rc=0 输出 0–775MB 不确定垃圾**——典型的**静默错误编译**，比崩溃更危险，被"假阳性"标签封存了三轮。**副产品**：把 #84 定性为**运行期 SIGSEGV**（stderr `Error: signal: segmentation fault`，崩在第一条语句 `json.parse('')` 的最简早返回路径），并用同形状 `/tmp` 探针（p5/p6/p9）排除了"22KB 值语义载荷 + option + match"这一整条假设。**本轮的写法教训**：命名一个现象为"假阳性"之前先去看它的输出。**另外现场重建 legacy（`47b6cad`）划清了 #85 的归属**：根因（前端把 `i.to-str()` 解析成"类型 `i` 上的方法"）**两个后端共有**（legacy 发出不存在的符号 `@i.to.str`），而 **MIR 独有的是失败模式**——legacy 在 `emitArgAsStrLong` **硬报错**"expression produced empty value"，MIR 在 `loadVal` 静默返回 `undef`。⇒ 性价比最高的一处改动是给 `loadVal` 补护栏（复刻 legacy 那个诊断），把一整类静默错误编译变成响铃失败。**并由此补一条判读口径**：`IMPROVED` 不能无条件算成绩，50 条里至少 1 条（`str-concat-leak.no`）是"MIR 接受了 legacy 正确拒绝的非法输入"。详见 §13.3.16 与 §12 #84/#85/#86。

- **Stage 4 续（第四十三轮 2026-09-16）：#85 护栏落地 —— `loadVal` 的静默 `undef` 变响铃；影响半径由 19 文件修正为 2 文件（`tests/` 内）+ 1 个（`test/`）**：把上一轮"先加 env 诊断测一遍"的测量结果**按字段切开**，发现 55 条命中里 **51 条是 `llvm="void"` 的合法命中**（void 值无存储，调用方靠 `"void"` 跳过它），**只有 4 条 `value=0`（=`NoVal`）是真洞**，落在 **2 个文件**。据此把 `loadVal` 里合在一起的 `lt == "void" || slot == ""` **拆成两支**，只对 `slot == ""` 那支 `c.fail`（报文区分 `NoVal` 与"有类型无槽位"两种成因；后者至今未被任何真实输入触发）——**拆分支的依据正是这份测量**，合在一起改会让 51 处合法命中全部误报。**两个受影响文件加护栏后的指纹与冻结语义 oracle（`legacy-baseline`）逐字节相同**（`1 e3b0c442…` 空 stdout）⇒ **对上 oracle，不是回归**；重冻结与改动前**恰好 diff 2 行**、无其它变动。**把范围扩到 `test/`(40)+`example/`(11) 又多出 1 个**：`test/std/process.no` 护栏前 `no test` 的**第 5 个测试 `t-cmd` 直接 SIGSEGV**，护栏后变为编译期点名 `t_cmd` 的诊断 ⇒ **无测试由通过变失败**（护栏把一次无解释的段错误变成可读诊断，是本轮价值最强的例证）。**三条判读教训**：① 手工探针用 `no build` 而预言机用 `no run`，**两个 rc 不是一回事**（前者=编译器成败，后者=程序退出码），混用会虚构出并不存在的 `test-std-hash.no` 的 `0→1` 转变（它早就是 rc=1）；② `BOTH_FAIL` **不比哈希**，所以"编译失败"与"编译成功但程序自己失败"被压成一个桶，这次修复在桶计数上**完全不可见**（要直接 diff 指纹行）；③ 加护栏前 `test-std-hash.no` 哈希是**稳定**的（`399229a5…`）却仍含 2 处 `NoVal` 消费 —— **"输出稳定"不是"没有 undef"的证据**。**根因也一并更正**：不是"接收者绑错"（`args=[3]` 就是循环计数器 `i`），而是 `hir2mir.go:4959–4963` 的 **void 分支**（`resTyp` 三档回退全落空 ⇒ 按语句型 void 调用发射、返回 `NoVal`）。**未闭环**：`callee` 的确切拼写待定（需给 MIR 转储补 `Sym`），`codegen.go` 的 `i64-to-str` 快路径在 `Results` 为空时还会**静默 `return nil`**（第二条独立失效路径）。详见 §13.3.17 与 §12 #85。

---

## 16. 开放问题与风险（更新）

- **生成顺序/平台过滤（第三十轮落地 #55，第三十九轮补 `#77`，第四十轮补测）**：HIR `#{platform}` 注解需在 lower 前过滤——已实现为 `src/mir/platform.go`（`nodeMatchesPlatform`，镜像已删除的 `build/llvm.matchesPlatform`；`package.PlatformKeys` 为唯一键表），现应用于**三处**：`hir2mir.go` 的 `KFuncDef`/`KExtern` 与 `KLet` 两个**注册点**，以及**脚本模式**的 `synthesizeMainForTopLevel` 顶层内联循环（#77，漏了它会让被过滤变体的值覆盖匹配变体注册的全局）。`src/mir/platform_test.go`（第四十轮）穷尽全表钉住这套判据。**未做的两部分**：① `KStructDef`、`KConst`、`KTypeAlias` 等其余**顶层**节点类型尚未过滤，若将来出现"同名同参的平台异构 struct/常量"仍会塌缩；② **函数体/块体内**的语句**两个后端都不过滤**（后出现者胜出，与宿主无关）——这是**共有的既有限制**而非 MIR 缺口，见 §13.3.14 ③，已由 `TestFunctionBodyPlatformVariantIsNotFiltered` 显式钉住。
- **溢出模式策略未生效（第四十轮新记，两后端共有）**：`#{overflow = clamp0 | min | max | saturate}` 在**实际编译**中与 `wrap` 行为完全一致（5 种模式输出相同），注解目前只起"关掉 `option<int>` 包装"的作用。legacy 虽实现了 `emitClampArith`，但在 HIR 模式下读不到 `let` 上的注解；MIR 侧则**完全没有**溢出模式概念（`src/mir/*.go` 无 `nsw`/`OverflowMode`）。因为**两后端一致**，不是 MIR 缺口。详见 §13.3.14 ④。
- **编译期成本：MIR 产出 IR 的形态对 LLVM 病态（第四十一轮新记，**未修 #83**）**：`json-pool` 这类"值语义结构体内联大数组"（`[64 x %json_json_value]` ≈22KB，其元素又各带两个 16 元素数组）被整体物化成 `alloca`，LLVM 的 `sroa` 会按常量下标把它们拆成十万量级标量 —— 单跑这一个 pass 实测 **229KB → 105MB（×458）**。后果：`opt -O3` 15s、`llc` 14s，且在 #82 之前这两个阶段**每次构建各跑两遍**（62s/次构建）。**扫掠预算的含义因此变了**：触碰 json 的用例耗时 113–117s，即使 90s 预算也仍会被记 HANG —— 不是挂死，是慢。判别方法（`no fmt`/`no vet` 先排除前端、两个预算下比对 `NOLANG_MIR_DUMP_MIR` 是否逐字节相同）与修法方向见 §13.3.15 ③④⑥。
- **【最高优先风险】静默错误编译：rc=0 但输出错误（第四十二轮新记 #85 / 第四十三轮已加护栏，**底层未修**）**：`print('item' + i.to-str())`（count-for 迭代变量、未先绑定）把拼接右操作数降级成 IR 里的 `undef`，于是**以 rc=0 输出 0–775MB 不确定垃圾**。这类缺陷比崩溃危险得多：它不触发任何判据——rc=0、无 stderr、输出"看着有东西"；而 rc 类判据（`MIR_GAP`/`REGRESS`/`CRASH`）对它完全失效，**只有逐字节哈希比对能看见它**。更值得记取的是**它是怎么被藏住三轮的**：语料里唯一的哨兵 `mem-safety/str-concat-leak.no` 确实报了哈希不一致，但被标注为"二进制地址差异假阳性"——**该结论从未被验证过，而那个文件根本不打印地址**。⇒ 处理原则：① `DIVERGE`/哈希不一致先当 bug，命名"假阳性"之前必须看一眼实际输出；② 已知会不确定的条目放进 `MIR_GOLDEN_UNSTABLE` 并**写明原因与 bug 号**，而不是靠"反正是噪声"忽略；③ 修 #85 时三条要分开（前端方法名解析（**共有**）+ MIR 的 `loadVal` 静默 `undef` 护栏 + checker 对 `str + <调用>` 的类型判定，见 §12 #85）；④ **`IMPROVED` 桶要人工过一遍**——`legacy-baseline` 的 `rc!=0` 是"无参照"，MIR 从 rc=1 变 rc=0 可能正是"接受了 legacy 正确拒绝的非法输入"（`str-concat-leak.no` 就是这一条），不能当成绩。
  - **第四十三轮更新**：**护栏已落地**（`loadVal` 拆两支，`slot == ""` 那支 `c.fail`），该类的**静默**属性已消除 —— 现在会以 rc=1 + 精确报文响铃。**影响半径修正为 2 个文件（`tests/` 语料内）+ 1 个（`test/`）**（`str-concat-leak.no`、`test-std-hash.no`、`test/std/process.no`，三者本即失败/broken：前二者 `legacy-baseline` 即 rc=1，第三者护栏前 `no test` 在 `t-cmd` 上 SIGSEGV）：上一轮"19 文件"是把 51 条 `llvm="void"` 的合法命中与 4 条 `value=0` 的真洞混在一起数出来的（§13.3.17 ①）。**剩余的开放风险从"静默"变为两个**：① 底层 lowering 缺陷未修（`i.to-str()` 的 `call` 没有结果值；成因是 `hir2mir.go:4959–4963` 的 void 分支，非"接收者绑错"），响铃只是不掩盖；② **同类风险下一次可能以别的形式出现**——判据是"是否存在没有产生者的值"，而不是"rc 是否为 0"，所以 §12 #85 的 `NoVal` 检查点值得作为一类断言保留。另记两条口径：**"输出稳定"不等于"没有 undef"**（`test-std-hash.no` 稳定却含 2 处），哈希稳定性不能当无罪证据；**给出的影响半径必须写明扫描范围**（本轮"2 个文件"一度被当成完整答案，扩到 `test/`+`example/` 就多出一个）。

- **跨模块 owner 判定**：与 `globalVarOwner`/`funcOwner` 对齐（`transpiler.go`）。

- **推断类型来源**：`pkg.Inferred[id]` 是分类 ownership 的权威输入；缺失时回退声明类型 `Node.Type` 并标记诊断。
- **回归红线（第四十轮补充；第四十二轮改为"分桶语义"口径）**：~~任何 MIR 失败在 `NOLANG_MIR=2` 必须回退现有路径~~ —— **legacy 已删除，回退机制不存在**。现在的红线是：`scripts/mir_golden.sh` 对 `tests/golden/mir-baseline.tsv` 的比对必须 **`REGRESS=0`**（对 `legacy-baseline.tsv` 的比对同样必须 `REGRESS=0`，那是"legacy 能编而 MIR 不能"的禁止集）。**判读这套桶时必须知道三件事（第四十二轮 #86）**：① `rc=124` 是"没拿到判定"，不是文件的属性——超时 90s→**300s**、并发 `-P` 8→**4** 之后，124 只剩 `test-for2.no`（有意死循环）；② `UNSTABLE` 是"输出本身不确定"的已知清单（当前 1 个：`str-concat-leak.no`，原因 #85），它与 `DIVERGE` 分开列出，为的是让 `DIVERGE` 保持干净信号——**一个永远不可能匹配的条目会让读者学会忽略整个桶**；③ `legacy-baseline.tsv` 已**不可再生**（`-update` 配非 `default` 的 oracle 会被守卫直接拒绝，rc=2），它是只读历史产物。**`legacy-baseline` 的 `DIVERGE` 桶必须逐个判"谁对"**：它现在是"语义分歧候选"而非"错误清单"，已知多数是 legacy 错（bool 打印不一致、`async-yield` legacy SIGSEGV、hmac/sha256 legacy 算错），且**新增了一个方向相反、有意为之的成员** —— `tests/test-platform-const.no`（MIR `100` vs legacy `600`，§13.3.14 ②）。全量 `no build` 扫掠在 sandbox 受限（约 360 测试会被 SIGKILL），用定向子集 + `opt -passes=verify` 快速回路验证（`scripts/mir_cov.py` / `mir_sweep.py`）。单测层面：`go test ./mir/` 必须全绿（第四十轮起 13 个测试函数 / 15 个用例，含平台变体判据）。
- **bool 打印：legacy 自身不一致，暂不强行对齐（2026-09-12 实测）**。legacy 的 bool 输出**依赖表达式形态**而非值：命名变量 `print(b)` / 结构体字段 `print(p.vis)` → `true`/`false`；比较表达式 `print(n > 3)` / vec 元素 `print(v[0])` → `1`/`0`；`(n>3).to-str()` → `1` 而命名变量 `.to-str()` → `true`。MIR 全站统一输出 `1`/`0`（`print_bool`）。因此在顶层命名 bool 场景 MIR 与 legacy 分歧（如 `tests/test-std-unix-fs-os.no` 的 `utime ok = true` vs `1`），但这类测试 rc 仍为 0。**判定**：属 legacy 历史不一致（且 legacy 在函数体内 `print(局部 bool)` 会直接 opt 失败：`'%b.val' defined with type 'i64' but expected 'i1'`），不是干净的 MIR 缺陷；强行翻转会在另一半场景引入新的分歧，故维持现状并记录。**扫掠超时口径**：crypto 系列（sha256/hmac/tls）单测编译+运行需 10–14s，扫掠脚本超时必须 ≥60s，否则（尤其在并发 `go build` 抢 CPU 时）会被误判为 HANG；**json 系列在 90s 下必超**（串行 33–120s，属性 §13.3.15 的**编译期成本**而非挂死）。**第四十二轮更正**：这个"90s 会超"的现象里**混着两种成因**——`test-parse-min.no` 串行只要 32.9s，它记 124 纯属 `-P 8` 并发争抢；故 `scripts/mir_golden.sh` 的默认超时已提到 **300s**、并发降到 **4**，让 124 只表示死循环（§12 #86）。**并发争抢是系统性的**：同一份语料在 8 路并发与串行下会得到不同的 rc 分类。
- **溢出默认集成中段风险（已消解）**：§13.1/§14 的解包缺失曾令 `test-arr.no` 退化（已闭环，`emitIndexStore`）；续闭环 `emitSetField`（§12 #14），sink 站点解包已覆盖 `emitIndexStore`+`emitSetField`。**原"剩余 `opt-verify` 家族（§13.3.1，23 个）在非 sink 表达式路径未解包"的判断已过时**：该家族经 #15/#18/#19/#23/#32/#33/#34/#39 逐站闭环，第十三轮复核 **MIR 专属剩 0**。`test-arr.no` 已于第十二轮实测 `MIR=3` 与 legacy 逐字节一致。**精确 MIR=3 通过数**：第十三轮以**修正二进制路径**（`./bin/no`，非陈旧的仓库根 `no`）对全量 `tests/**/*.no` 重跑，结果见 §10.1。
- **legacy `awy f` future 变量漏行 bug（2026-09-12 第十二轮实测，MIR 正确、legacy 错）**：`tests/test-async.no` 的 `test-await-future` 子用例写 `f = compute-async(25); r = awy f; print(r)`。MIR=3 正确输出 8 行（`42/42/1/-1/0/60/60/50`），但 legacy（`MIR=0`）只输出 7 行、**漏最后一行 `50`**——legacy 对 `awy <future 变量>`（future 已先 `run` 进变量、再 await 该变量）的调度路径存在既有 bug，未把该 future 的结果打印出来。故 `test-async` 在 §10.1 覆盖表中**不计入 MATCH**（要求逐字节一致），而归入 "MIR 正确 / legacy 错误" 族（同 #28 插值、#36 x25519）。这是 legacy 自身缺陷，非 MIR 回归；MIR 因忠实移植 legacy `build/llvm` 协作式调度器契约（§12 #39）而绕过了该 bug。
- **legacy `async-yield()` 在 `-async` 函数内 SIGSEGV（2026-09-12 第十二·续轮实测，MIR 正确、legacy 崩）**：`tests/tmp-async-yield.no` 的 `-async` 目标 `yielder-async` 体内首条语句是 `async-yield()`，随后 `r = n + 1`，由 `run`/`awy` 驱动。MIR=3 输出 `6/9` **正确**；legacy（`MIR=0`）**SIGSEGV**（最小复现 `/tmp/min-yield.no` 亦崩）——legacy 的 `coro.go` 把顶层 `async-yield()` 改写为协程挂起点后，在 `run`+`awy` 驱动的 `-async` 函数上挂起/恢复路径崩。MIR 因无协程状态变换、恒走退化路径（`call @nolang_async_yield`）而正确。又一 "MIR 正确 / legacy 错误" 案例，非 MIR 回归。注意：**函数名以 `-async` 结尾会被 MIR/legacy 都当作异步调用**（`strings.HasSuffix(callee, "-async")` → `OpRun`），故测试函数名应避免该后缀（本测试初版误名 `test-yield-in-async` 被当作 `run` 而未被 await，已改名 `test-yield-inside`）。

44. **字符串插值方法调用字段（2026-09-14 第二十四轮，闭环唯一稳定 MIR_GAP `test-diff-debug`）**：`lookupFormatValue`（`hir2mir.go`）此前仅支持 `ident` 和 `ident[index]` 两种格式字段模式，对 `ident.method()`（如 `eprint('SPLIT: entered, content.len={content.len-bytes()}')` 中的 `{content.len-bytes()}`）无支持 → 报 `unsupported construct (string interpolation)` 致命诊断 → 整模块回退。`test-diff-debug.no` 是**唯一稳定的 MIR 专属 gap**（MIR=3 rc=1 构建/运行失败，legacy flaky rc 在 0/1 间抖动）。修复：新增 `fmtMethodFieldRe` 正则匹配 `ident.method()` 模式，在 `lookupFormatValue` 中优先查 builtin 表（`sliceMethodBuiltin` + `builtin.FindBuiltinMethod`），按 receiver 类型+方法名路由到 ForwardFunc 内置（如 `str-len-bytes`），非内置时走用户方法调用路径（`canonSliceRecv` + `enqueueCallee`）。验证：`test-diff-debug.no` 在 `MIR=3` 下不再报 `unsupported construct`，输出与 legacy 逐字节一致（两模式均 rc=1 属测试自身 DP 算法 bug，非 codegen 问题）。

45. **数组字面量元素类型推导修复（2026-09-14 第二十四轮，闭环 SHA1/HMAC 静默错误族）**：`lowerArrayElems`（`hir2mir.go`）此前用 `typeOfNode(首元素)` 推导数组字面量元素类型；整数常量 `0x61` 的 HIR 类型为 `i64` → `data []byte = [0x61, 0x62, 0x63]` 生成 `[3]i64`（元素 stride 8 字节）而非 `[3]byte`（stride 1 字节）。当 SHA1 从 `data[i]` 读取字节时，`elemAddr` 按 `i64` 步长计算偏移 → `data[1]` 读到 offset 8（越界）而非 offset 1 → **哈希完全错误但 rc=0**（静默错误）。`tests/test-sha1-minimal.no` 的 MIR 输出 `d1 e2 40...` 而 legacy 输出正确的 `a9 99 3e...`。修复：`lowerArrayElems` 新增优先级——先检查 `l.typeHint` 是否为 slice/array 类型，若是则取其元素类型作为数组字面量的元素类型（使 `data []byte = [0x61,0x62,0x63]` 生成 `[3]byte`）。仅在 `typeHint` 不可用时才回退到首元素类型推导。验证：`test-sha1-minimal.no` 在 `MIR=3` 下输出与 legacy 逐字节一致（`a9 99 3e...`）；`test-hmac.no`/`test-hmac2.no` 的 MIR 输出 `86` **正确**（legacy 输出 `182` 错误——legacy 的 slice 写入 `inner-data[64+i]` 未正确写入 msg 字节，属 "MIR 正确 / legacy 错误" 族）。

46. **字符串 `!=` 比较取反逻辑修复（2026-09-14 第二十八轮，闭环静默错误族 P0）**：`lowerExpr` 的 `KInfix` 分支中，字符串 `!=` 比较的降级（`hir2mir.go` 3196-3202）先用 `OpStrEq` 计算相等结果 `eq`，然后用 `OpNe(eq, false)` 取反。但 `OpNe(eq, false)` = `eq != false` = `eq`——**并没有取反**！因此 `'hello' != 'world'` 返回 `false`（而非 `true`），导致所有使用 `str != str` 的条件分支静默走错分支——程序构建成功、运行不崩溃，但输出**错误**（rc=0 静默错误）。这是最危险的 bug 类别：`test-rand-multiassign.no` 最后一行 `PASS: different seeds produce different priv key bytes` 在 MIR 下不输出（条件分支 `ka-priv != kb-priv` 判为 false → 落入 `FAIL` 分支但 `FAIL` 分支的条件 `ka-priv == kb-priv` 也判为 false → 两个分支都不执行 → 空输出）。修复：把 `OpNe(eq, false)` 改为 `OpNot(eq)`（`xor i1 eq, 1`），正确取反 `@str_eq` 的结果。验证：四种组合（`==`/`!=` × first-arm/second-arm）与 legacy 逐字节一致；`test-rand-multiassign.no` 由 DIVERGE→MATCH（净 +1）；21 个 curated 测试零回归（`test-async`/`test-named-fn-type`/`test-quant-all1` 的 DIVERGE 为已知的 "MIR 正确 / legacy 错误" 族，非本修复引入）。

51. **`std/process.no` 词法硬伤修复（2026-09-15 第二十九轮，解锁 4 个测试）**：`src/std/process.no` 第 637 行是一行**漏了 `;` 注释前缀的裸中文**（`守衞區塊內。`），疑似此前 `no fmt -w` 对含 `#{...}` 注解的块重排时把注释行切碎（同一块内还留下 `; 注意：... -> {` 与错位缩进）。lexer 报 `[E_GENERAL] illegal token "®"` 等一串伪乱码（实为 UTF-8 续字节被逐字节报错），使 `std/process` 无法解析 → 任何 `import process` / 自动加载该模块的程序在**两模式**下都编译失败（`test-process-run`、`test-https-server`、`test-tcp-fork`、`mem-safety/bug15-read-dowhile-copyfile`）。修复：把该行并入上方注释块并恢复 `#{index-out = 0}` 单独成行置于被守卫语句之上的正确形态。验证：4 个测试全部越过该错误；其中 3 个（`test-tcp-fork` 经 #53 补 net 内置、`bug15`/`test-process-run` 仍在 MIR_GAP）的 legacy 基线随之由失败转为成功。**副作用（重要）**：修掉 std 硬伤后，原先被"两模式都失败"掩盖的 3 个真 MIR 缺口浮出水面（`net-listen`/`net-accept`、`[]t.slice`、`i64.trim`），这正是下一轮要清零的对象——**"两模式都挂"不是稳态，修好公共依赖后 MIR_GAP 会先升后降**。

52. **溢出检查器三类误报修复（2026-09-15 第二十九轮，解锁 9 个测试）**：`ValidateUnhandledOverflow`（`src/checker/checker.go`）把非整数运算也当成"未处理的整数溢出"，是当时最大的失败家族（19 个测试）。三类根因一次性修掉：
    - **(a) 顶层顺序语句不共享 `varTypes`**：驱动循环对每条顶层语句都新建空 map，前一条注册的标注（`a f64 = arr[0]`）在下一条（`c f64 = a * b`）中不可见，运算元退化成"型别未知"→ 保守视为整数 → 对**浮点运算**误报。改为顶层共享一份 `topTypes`（函数体仍由 `seedVarTypes` 建独立作用域）。
    - **(b) 未标注类型的绑定不登记**：只有 `s.Type != nil` 才写入 `varTypes`，于是 `a = 'foo'` 之后的 `c = a - b` 里 `a`/`b` 型别未知 → 把 **str 的 `-`（字符串拼接）** 误报成整数溢出（与 `isIntType` 注释所声明的"非整数不提示"意图相悖）。新增：无标注入口时以 `inferExprType(s.Value, ...)` 推断并登记，推断不出则保持未知走保守路径。
    - **(c) match 表达式的臂末值被当成"丢弃的表达式陈述"**：`result = x: { 2 -> 2 + 1 }` 中臂末表达式是**该臂的值**，却被当作 `ExpressionStatement` 命中"作为表达式语句被丢弃（未处理）"。给 `walkExpr` 增加 `valueCtx` 参数（绑定右值/回传值/条件为 true，陈述位置为 false），值上下文下跳过每条臂的最后一个表达式陈述。

    验证：`test-str-concat`、`tmp-arr-test`、7 个 `test_char_*` 全部由编译硬错转为 rc=0；`checker` 包 `TestUnhandledOverflow*` 四个用例全过（真正的泄漏 `x = a + b` / `out = a + b` / 循环内 `i = i + 1` 仍照常报错）。剩余 8 个仍报"未处理溢出"的（`test-all`/`test-sse`/`test-database-sql`/`test-ffi-mysql`/`test-ffi-sqlite`/`tmp-words-test`/`test-sha256-simplified`/`test-tagged-enum`）是**真的未标注整数运算**，属语料迁移（加 `#{overflow = wrap}` 或改 `?=`），不是误报。

53. **MIR 补齐 `net-listen` / `net-accept` / `net-udp-open` 内置（2026-09-15 第二十九轮）**：`emitBuiltinForward` 的 ForwardFunc 分发表此前只有 `net-dial`/`net-send`/`net-recv`/`net-icmp-open`，监听侧与 UDP 侧未实现 → `unsupported builtin net.net-listen` / `net.net-accept` / `net.net-udp-open`。新增三个 emitter（镜像 `build/llvm/call_stdlib.go` 同名实现）：`emitBuiltinNetListen`（socket + `setsockopt(SO_REUSEADDR)` + bind + listen，`sockaddr_in` 布局按 `runtime.GOOS` 选 `sin_len/sin_family` 偏移与 `SOL_SOCKET` 常量，任一步失败返回 -1）、`emitBuiltinNetAccept`（16 字节 sockaddr + in/out addrlen 的 `accept(2)`，符号扩展回 i64）、`emitBuiltinNetUdpOpen`（`socket(AF_INET, SOCK_DGRAM, 0)`）。验证：`test-tcp-fork.no` 首次在 `MIR=3` 下跑通（rc=0，输出 "listening / child accepting / parent dialing / parent connected"）；`test-https-server` 推进到下一层错误（`net-send: cannot take data pointer of arg 1`）；`test-http3` 越过编译错误（legacy 同测亦是 LLVM opt 失败，两模式仍都挂）。

54. **KLet 回退分支把已有类型的值改判为 `void` → codegen 对模块全局发射 `undef`（2026-09-15 第二十九轮，闭环顶层变量参与 match 的 SIGTRAP）**：`lowerStmt` 的 KLet 收尾处有`f.LocalTypes[val] = l.typeOfNode(l.pkg.Node(childID))`（仅当 LocalTypes 缺失时填充）。当被绑定值**已经有正确类型**——最典型是 `lowerGlobalRef` 惰性创建的模块全局（顶层 `x = 3` → `@x`），`Builder.Global` 不写 `LocalTypes`——而 KLet 的子节点恰是**裸 `ident`（自身不带型别）**时，`typeOfNode` 返回 `void`，于是把一个活生生的值**改型为 void**。后果二连：`allocaFor` 对 void 不分配槽位，`loadVal` 返回 `undefined` → `x: { 1 -> ... }`（顶层全局变量作 match 主题）生成 `icmp eq i64 undef, 1`，`opt` 后 `br i1 undef` → **trace/BPT trap**；同时第二条的 `let it = x` 因 `valueTypeOf` 已是 void 而退回 `EmitMoveInto` 自赋值。修复：节点型别为 `NoType`/void 时改用 `l.valueTypeOf(val)`（值自带的类型），已知则保留，未知才落 void。验证：`/tmp` 最小复现（`x = 3` + `x: {...}`）与 `tests/match.no`、`tests/option.no` 在 `MIR=3` 下与 legacy 逐字节一致（此前这两个用例被 #52 之前的溢出误报挡住，修好后才暴露本崩溃——**修误报会揭开下一层真 bug，属正常推进顺序**）。

55. **MIR 缺失的平台过滤 → 平台异构重载塌缩到同一 mangled 符号（2026-09-15 第三十轮，新增 `src/mir/platform.go`）**：`build.mangleOverloads` 用「函数名 + 经 `sanitizeTypeForName` 规范化的形参类型串」生成符号。`process.cmd` 有 POSIX（`#{mac-*, linux-*, wasi-wasm32}`）与 Win32（`#{win-*}`）两个**形参列表完全相同**的变体 → 二者 mangled 成**同一个**符号。MIR 的 `funcNames` 注册循环此前**不做平台过滤**（HIR 里平台变体全都还在），后访问者覆盖先访问者，于是 Win32 函数体胜出 → `MIR=3` 报 `unsupported builtin win-create-pipe`（修掉后立刻暴露下一层 `unsupported builtin process-pipe`）。修复：新增 `src/mir/platform.go`，`mirTargetPlatform()` 取 `runtime.GOOS/GOARCH`，`nodeMatchesPlatform(pkg, id)` 镜像 `build/llvm.matchesPlatform`（读 `pkg.AnnotationKeys(id)` 的裸布尔项，逐项比对 `nopkg.PlatformKeys`；无平台注释则 `!hasPlatform` 放行），在 `KFuncDef`/`KExtern` 与 `KLet` 两处注册点 `continue` 掉不匹配变体。**教训**：MIR 管线此前完全没有平台投影（legacy 有），任何"同名同参的平台变体"都会塌缩；新增跨平台 std 函数时必须确认 MIR 侧同款过滤。验证：`process.cmd` 解析到 POSIX 变体，`test-process-run.no` 越过 `win-create-pipe`。

56. **重载 mangling 后的 callee 名解析缺口（2026-09-15 第三十轮，闭环 `test-process-run.no` 的 `unknown callee i64.trim`）**：`resolveCallee` 返回的点号调用名 `process.cmd` **不在** `funcNames` 里（表里只有 `build.mangleOverloads` 生成的 `process.cmd_str_slice.str_str_str_slice.str_i64_bool`），因为 `mangleOverloads` 只重写**函数定义与未限定引用**，不重写 `a.b` 形式的限定调用。后果不是"找不到函数"这么简单：`resultTypesOfCallee` 返回空 → 多返回值解构 `out, se, code, err = process.cmd(...)` 的 4 个目标全部退化成 **i64 零占位** → 后续 `out.trim()` 把 `out` 当 i64 → 报 **`unknown callee i64.trim`**（症状与根因完全脱节，是典型的误导性诊断）。修复：`resolveOverloadedFuncName(callee)` —— 先查精确名；未命中则以 `callee + "_"` 为前缀在 `funcNames` 里找候选，并用 `mangledSuffixMatches(id, suffix)` **反向校验**（把该定义的 `KParam` 类型逐个过 `mangleTypeReplacer` 再拼接，要求与符号后缀**逐字符相等**），唯一命中才采用，多命中则放弃（避免误配）。`resolveModuleCallName` 的 qualified 分支回退调用它。验证：`test-process-run.no` 的 10 个用例全部 PASS，与 legacy 逐字节一致。

57. **泛型切片方法名 `[]t.slice` 未回落具体元素类型（2026-09-15 第三十轮，闭环 `bug15` 的 `unknown callee []t.slice`）**：`canonSliceRecv` 把 `buf.slice(0, n)`（receiver 声明为 `[]byte`）规范化成**泛型占位** `[]t.slice`，而 `funcNames` 里真实存在的是具体化后的 `[]byte.slice`。修复：在 `KDot` 方法解析里 `sliceMethodBuiltin` 之后**优先查具体名** `recvTypeName + "." + method`，命中则直接用，未命中才退回 `canonSliceRecv` 的泛型名。验证：`mem-safety/bug15-read-dowhile-copyfile.no` 三个子测试全 PASS。

58. **MIR 补齐 `process-pipe` / `process-waitpid-nohang` + `process-kill` 改 `CmpRet`（2026-09-15 第三十轮）**：`emitBuiltinForward` 分发表缺 `process-pipe`、`process-waitpid-nohang`（第 55/56 项把 `process.cmd` 指向 POSIX 变体后暴露）。新增 `emitBuiltinPipe`（`pipe(2)` 后把 `(read_fd<<32)|write_fd` 打包成 i64，镜像 `call_stdlib.go` 的 `process-pipe`）与 `emitBuiltinWaitpidNohang`（`waitpid(pid, &st, WNOHANG=1)`，仍在运行返回 -1，否则返回退出码）。另修 `src/builtin/process.go`：`process-kill` 原声明 `RetExt: &i64Type`，但 `kill(2)` 是 POSIX「0 即成功」约定，MIR 把裸 i32 塞进 i1 结果槽时报 `cannot coerce result i32 to i1` → 改 `CmpRet: true`。验证：`test-process-run.no` 与 `bug15` 的 `test-cmd-capture` 均 rc=0 且与 legacy 一致。

59. **`emitCmp` 的 option 语义二修（2026-09-15 第三十轮：tag vs payload、`!=` 取反）**：两处独立缺陷。(a) 原实现**只要任一侧是 option 就取 discriminANT（field 0）**，于是 `content == 'Hello World'`（`content` 是 `?str`）拿 tag 的十进制文本去比字面量，**永远为假** —— 正确语义是 option↔option 比 tag、option↔普通值比 **payload（field 1）**；新增 `optionPayloadOf(v, optLT)` 与 `optionElemRawOf(optLT)`（从 `%option_str` 这类逐 payload 内联名反查元素 raw 类型）。注意 `%option`（标量 payload，`{i64,i64}`）与 `%option_<elem>`（逐 payload 内联）是两种布局，二者都要处理。(b) `%str-long` 比较分支对 `==` 与 `!=` **都**发 `@str_eq` → `!=` 行为等同 `==`；改为 `!=` 时发 `xor i1 %eq, true`。验证：`bug15` 的 `content == 'Hello World'` / `content == 'Hello Nolang'` 两个断言恢复正确判定。

60. **`break` 蹦床块的重复 start-drop → 双释放（2026-09-15 第三十轮，`analysis.go`）**：`i > 2 -> break` 被降级成一个**唯一指令就是 `br <loop-exit>`** 的蹦床块，而循环退出块本身**合法地**需要一条 `v` 的 start-drop（`v` 在 header 的退出边上死亡）。`insertDrops` 于是给蹦床块和退出块各插了一条 start-drop —— break 路径上同一个循环不变量的 buffer/字符串被释放**两次**，`{ ...; cond -> break } (true)` 且循环体读循环外声明的 owned 值（最小复现：`s str = 'xy'` + 循环内 `print(s)` 后 break）必然 SIGTRAP/SIGABRT。自测确认此为**既有缺陷**（非本轮引入）。修复：新增 `redundantStartDrop(b, v, startDrop, skipped, seen)`，只沿**单后继链**行进（单后继链上不存在能绕过下游 drop 的分支），链上遇到一个"尚未被跳过且持有该值 start-drop"的块即可判 `b` 冗余。块按 **block id 升序**确定性遍历，且**被跳过的块不能再用作他人的理由**，故一个纯 dropper 环不可能把所有 drop 都消掉（至少留一条）。残余风险只朝**泄漏**方向（多删），不会朝双释放方向。验证：最小复现 `c17.no` 与 `tests/mem-safety/bug15-*` 的 `test-infinite-break`/`test-pretest-loop` 在 `MIR=3` 下与 legacy 逐字节一致。

61. **`with-cap/with-len/with-cap-len` 元素步长硬编码 8 + 新缓冲未清零（2026-09-15 第三十轮，本轮最隐蔽的一处）**：`emitBuiltinAlloc` 用 `stride := int64(8)`（仅 `%str-long` 目标特殊化为 1）计算后端存储字节数，**完全没有看元素类型**。但 `[]str` 的元素是 **24 字节**的 `%str-long`、`[]?i64` 是 16 字节的 `%option` —— 8 字节的块**严重欠配**，随后 `a[0] = 'x'` 时 `emitIndexStore` 会**按 24 字节读一个完整元素**，越过 malloc 边界读到相邻堆内容，把这个**垃圾当 `data` 指针**交给 `@str_free` → `free(非本进程分配指针)`：`-O0` 下 SIGABRT（崩溃报告 `___BUG_IN_CLIENT_OF_LIBMALLOC_POINTER_BEING_FREED_WAS_NOT_ALLOCATED`），`-O3` 下同一 UB 被优化成 SIGTRAP。叠加第二个缺陷：malloc 返回的是**未初始化**内存，而 `emitIndexStore` 对 owned 元素类型会**先 drop 旧元素再写新值**（`load %str-long; call @str_free`），未初始化的旧元素同样是垃圾指针。修复三件套：(a) 新增 `mirStaticTypeSize`（标量 + `%str-long`/`%vec`=24、`%option`=16）+ `typeSizeOperand`（未知类型发 `ptrtoint ptr getelementptr (T, ptr null, i64 1)`，LLVM 直接常量折叠出 ABI 精确的 `sizeof(T)`，**MIR 端无需复刻 LLVM 的结构体布局规则**）+ `allocBytesOperand(cap, elemLT)`（步长取 `elemTypeOfReceiver(inst.Dst)` —— 对 `str` 目标返回 `i8`，天然覆盖"每槽 1 字节"的字符串情形）；(b) `emitBuiltinAlloc` 与新缓冲路径 `ensureVecBuffer` 都在 malloc 后补 `llvm.memset(...,0,...)`，**镜像 legacy 的同款注释**（"load undef -> icmp -> free(undef) is UB that SCCP deletes the whole fn"）；(c) 顺带修正 `[]byte` 的步长由 8 改回 1（与 legacy `llvmTypeSize(byte)` 一致，此前 8 倍超额分配）。验证：最小复现 `a []str = with-len(1); a[0] = "x".to-str()` 由 SIGABRT/SIGTRAP 转为与 legacy 逐字节一致；`tests/mem-safety/bug15-read-dowhile-copyfile.no`（含 `[]str` 级别的高层用例）三个子测试全 PASS。

**本轮（第三十轮）净效果**：闭环第二十九轮浮出的**全部 2 个** MIR 专属 gap（`mem-safety/bug15-read-dowhile-copyfile.no`、`test-process-run.no`），二者经 `NOLANG_MIR=0` 与 `NOLANG_MIR=3` 双跑已**逐字节 MATCH**。修复链是层层递进的：平台过滤（#55）→ 重载名解析（#56）→ 具体切片方法（#57）→ process 内置补齐（#58）→ 运行时内存正确性（#60/#61），每一步都只在**上一层修好之后**才暴露下一层——与 #51→#52→#53/#54 的推进顺序完全同构。

62. **回退第二十九轮续的「owned 参数不别名」改动（2026-09-15 第三十轮，闭环 5 个由它引入的 MIR_GAP）**：第二十九轮末尾（**未提交、未重扫**）把 `emitFunc` 的 by-pointer 参数别名规则收窄为「只别名方法 receiver 与非 owned 值语义聚合；owned 参数（`str`/`vec`/`option`）改为 `load`+`store` 拷进独立槽」，理由是"别名会让 callee 的 drop 释放调用方共享的堆 buffer（`move2.no` 挂死）"。**该理由不成立**：`insertDrops` 的 `droppable` 集合显式排除全部入参（`!isParam[inst.Dst]`，因为 nolang 的 owned 实参按引用传递、所有权留在调用方，callee 从不 drop 入参），`move2.no` 的挂死经复测属**预存的 legacy flaky**（memory 2026-09-15 已记录 "committed 基线也挂，属 legacy UAF 不是 MIR 回归"，且 HEAD 上 `move2.no` 在 `MIR=3` 下实测通过）。而"owned 参数不别名"带来一个**真实且严重**的语义破坏：**按引用改写入参容器不再回传调用方**。最小后果：`buf []byte`（无初值，data==null）传给 `tls.put-u16(buf, 0, v)` 时，callee 内的 `ensureVecBuffer` 为**自己的副本**分配缓冲，调用方 `buf` 仍是 `{0,0,0}` → 后续 `buf[0]` 读到 0（`test-tls-part1.no` 由 PASS→失败）；进一步叠加读到 null/越界即 SIGSEGV/SIGTRAP。**发现方式**：本轮全量扫描报出 **6 个 MIR_GAP**（`test-tls-part1`/`test_char_step4`/`tmp-nbody-debug`/`test-realpath-debug`/`test-diff-debug`/`tmp-arr-f1`）——**用 `git worktree add /tmp/no-r29 d2803fe` 建干净基线二分**：HEAD 上 6 个里 5 个 MATCH、`test-diff-debug` 同样 rc=1（说明后者是**预存** gap），再把工作树改动拆成 hunk 逐个 `git apply -R` 定位到 `emitFunc` 两个 hunk（`git diff | awk` 抽出 hunk 生成子补丁）→ 单独回退该 hunk 即复绿。**修复**：`git apply -R` 回退这两个 hunk，恢复"所有 by-pointer 参数（含 owned）都别名到调用方指针"的 committed 行为。验证：5 个 gap 全部与 legacy 逐字节 MATCH（`test-tls-part1` → `=== Done ===`、`test_char_step4` → `done`、`tmp-nbody-debug` → 相同的浮点值、`test-realpath-debug`/`tmp-arr-f1` 输出一致）；`move2.no`、`test-arr-param` 在 `MIR=3` 下仍正常。**教训**：① 未提交的改动必须重跑全量扫描才能计入结论——本轮若信任文档的 "MIR_GAP=2" 就会漏掉这 5 个；② 判断"某改动是否引入回退"必须用 `git worktree` + 干净 HEAD 基线做**逐 hunk 二分**，而不是只看当前工作树的失败集；③ 注释里写的"用了某机制所以安全"必须回到代码里核对（这里 `droppable` 的 `!isParam` 一行就证伪了整段推理）。

63. **`emitCallBody` 变参启发式把"显式写出的具名出参实参"误当变参展开（2026-09-15 第三十一轮）**：`emitCallBody` 的变参检测用 `len(inst.Args) > len(inParams)` 判断尾部是否为展开的变参元素。但一个非变参函数 `lcs = (a []str, b []str) (ops []diff-op)` 以 `lcs(a, b, ops)` 调用时 `len(Args)=3 > len(inParams)=2`，而末入参 `b []str` 恰好是切片 → 把 `b`/`ops` 打进 `%cav` 假 vec；因新降级的切片字面量此时仍是定长数组 `[3 x %str-long]`，IR 出现 `%vec` 与 `[3 x %str-long]` 混用 → LLVM 校验整模块失败。元素型别一旦偶然吻合则 IR 通过但切片被静默污染。修法：新增 `mir.Function.Variadic`（`hir2mir.go` 由 `hir.FlagVariadic` 置位）作消歧依据——仅当 `!cf.Variadic` 且 `len(Args)==len(inParams)+len(outParams)` 时才把尾部实参视作出参实参并从计数中扣除；若不加此判据直接扣减，会把 `number.max(10, 20)`（1 个展开入参 + 1 个出参，恰为 2 实参）误判成"无展开"，使 `test-number-generic.no`/`test-number.no` 回归 trace/BPT。

64. **`emitIndex` 读取 `%str-long` 元素时未做 deep clone → 共享堆缓冲区 → drop 后悬空指针（2026-09-15 第三十二轮，闭环唯一稳定 MIR gap `test-diff-debug.no`）**：`emitIndex`（`codegen.go`）从 `%vec` 中读取 `%str-long` 元素时直接 `load %str-long, %str-long* %ep`，这是**浅拷贝**——复制 `{len, cap, data}` 三元组但共享 `data` 指针。当读取的目标变量（如 `s = lines2[i]`）在循环体结束被 drop（`@str_free` 释放 data 指向的堆缓冲区），数组元素 `lines2[i]` 变成**悬空指针**。后续 `a[ai].compare(b[aj])` 对相等字符串比较时读取已释放的内存，`@str_cmp` 的返回值不确定（非 0），导致 `compare == 0` 恒为 false → DP 表恒全 0 → 回溯阶段 segfault。**根因定位**：通过逐步简化原始测试（`/tmp/test-lcs5.no` ~ `/tmp/test-lcs7.no`）发现——单个循环打印 `L1` 无碍，加第二个循环打印 `L2` 后 DP 表变全 0；精确定位到 `s = lines2[i]`（`OpIndex`）的浅拷贝 + `s` 的 drop 破坏了 `lines2` 元素。**修复**：在 `emitIndex` 中，当 `elemT == "%str-long" && dstT == "%str-long"` 时，对加载的值调用 `@str_clone`（与 `emitIndexStore` 写入侧已有的 clone 对称）。验证：`test-diff-debug.no` 在 `MIR=3` 下 DP 表正确（`2 1 1 0 / 1 1 1 0 / 1 1 1 0 / 0 0 0 0`）、rc=0、不再 segfault；全量扫描 MATCH 367→368、CRASH 14→12、MIR_GAP 0（此前唯一稳定 gap 闭环）。**教训**：读侧（`emitIndex`）和写侧（`emitIndexStore`）的 ownership 语义必须对称——写侧 clone 而读侧不 clone，等于只做了半边所有权正确性；浅拷贝 + drop = use-after-free。

65. **Map 类型方法路由误入 slice builtin 路径（2026-09-15 第三十三轮）**：nolang 的 map 语法是 `[key]val`（如 `[str]i64`），与数组/切片共享 `[` 前缀。`sliceMethodBuiltin`（`hir2mir.go`）和 `canonSliceRecv` 用 `strings.HasPrefix(recvTypeName, "[")` 检测 slice/array，但**没有排除 map 类型** → `m.len()` 被路由到 `str-len` 内建（`FindBuiltinMethod("len")` 返回的第一个条目），`emitBuiltinLen` 随即拒绝 `i64` 接收者（map 在 MIR 中是不透明 `i64` 句柄）。**修复**：① 在 `sliceMethodBuiltin` 和 `canonSliceRecv` 中添加 `isMapRaw(recvTypeName)` 守卫，排除 map 类型；② 在 `resolveCallee` 中新增 map 类型到 hashmap struct 名的解析：`[str]i64` → `hashmap-str-i64`，与 legacy `generic_structs.go` 的 `"hashmap-" + key + "-" + sanitize(val)` 命名规则一致（val 中的 `[]` → `slice_`）。验证：`test-map-generics.no` 从 CERR（`str-len receiver i64`）转为 CRASH（MIR 编译通过但运行时 abort，legacy 也失败），`nested-container-clone.no` 从 `[]t.get` 变为 `[str]i64.get`（map 名正确但 hashmap 实例未生成）。全量扫描 MATCH 368→369。

66. **`with-len`/`with-cap` 在索引赋值路径下 `typeHint` 未设置（2026-09-15 第三十三轮）**：`lowerAssignNode`（`hir2mir.go`）在 `KIdent`（`x = with-len(n)`）和 `KDot`（`.field = with-cap(n)`）分支中设置了 `typeHint`，但缺少 `KIndex` 分支——`.keys[cnt] = with-len(key-len)` 这种索引赋值在 `json.no` 中大量出现，RHS 的 `with-len` 调用因无 typeHint 而 lower 为 void，codegen 报 `builtin with-len: no result slot`。**修复**：新增 `KIndex` 分支，从索引容器（第一个子节点）的 `elementTypeOf` 推导元素类型并设为 `typeHint`。验证：`test-json-nested-match.no` 和 `test-json-parse-option.no` 的 `with-len` 错误消除（暴露更深层的 `i64 vs double` 类型问题，但 legacy 也失败）。

67. **Builtin vec/str 方法缺失 GEP 优化 → 结构体字段 push/clear/truncate 等不回写（2026-09-17 第四十六轮）**：`emitCallBody`（`codegen.go`）已有 GEP 优化——当方法 receiver 是 `OpGetField` 的结果（如 `m.rows.push(x)`）时，直接计算结构体字段的 GEP 地址传递，使变异回写到结构体。但 builtin vec/str 方法（`emitBuiltinVecPush`、`emitBuiltinVecClear`、`emitBuiltinVecPop`、`emitBuiltinVecReverse`、`emitBuiltinVecInsert`、`emitBuiltinVecRemove`、`emitBuiltinVecSort`、`emitBuiltinVecTruncate`、`emitBuiltinStrClear`、`emitBuiltinStrTruncate`、`emitBuiltinArrZero`）此前直接使用 `c.valSlot[recv]`——该 slot 存的是字段值的**拷贝**，builtin 的变异写入拷贝而非原始字段，导致 `m.rows.push(x)` 后 `m.rows.len()` 仍为 0。**修复**：新增 `resolveReceiverSlot(recv)` 辅助函数（`builtin_call.go`），镜像 `emitCallBody` 的 `OpGetField` GEP 逻辑——检测 receiver 是否由 `OpGetField` 产生，若是则发射 GEP 指令获取字段地址并返回，否则返回原始 slot。所有上述 builtin 函数改用 `resolveReceiverSlot` 替代直接 `c.valSlot` 访问。验证：`tests/mem-safety/nested-container-clone.no` 从 BOTH_FAIL 转为 IMPROVED（rc=0，12 个子测试全通过）；`/tmp/test-gep-push.no`（最小复现：`m.rows.push(1/2/3)` 后 `m.rows.len()==3`）正确。

68. **Nolang 参数语义澄清与文档更新（2026-09-17 第四十六轮）**：用户澄清 Nolang 的顶层参数规则：**输入参数只读**（标量传值，复合类型只读引用，禁止写入输入及其子字段）；**输出参数可写**（调用方可绑定已有变量到输出槽，函数直接修改该内存；也可不绑定由函数生成新值）；**方法语法糖** `type.method = (inputs) (rest-outputs...) {}` 脱糖为 `method = (inputs) (self type, rest-outputs...) {}`，instance 绑定到输出区 self。更新 `.agents/skills/nolang-syntax/SKILL.md`、`docs/docs/lang/syntax.md`、`docs/i18n/en/.../lang/syntax.md`、`docs/i18n/en/.../lang/memory.md` 中的参数语义描述——此前误写"所有参数都是引用类型"，修正为"输入只读/输出可写"模型。

69. **`test-basic.no` 源码修正（2026-09-17 第四十六轮）**：`test-basic.no` 的函数签名 `i64-max = (a i64, b i64, r i64)` 把输出变量 `r` 放在了输入参数括号中，违反 Nolang 的"输入只读"规则——函数内对 `r` 赋值只是修改局部副本，不回写调用方。修正为 `i64-max = (a i64, b i64) (r i64)`，`r` 移到输出参数括号；调用从 `i64-max(10, 20, r)` 改为 `r = i64-max(10, 20)`。四个函数（`i64-max`/`i64-min`/`abs-i64`/`gcd`）全部修正。`bigint.gcd` 因 bigint 堆类型在 MIR codegen 中有段错误暂跳过，改用本地 `gcd(12, 18)` 验证。验证：输出 `20 / 10 / 42 / 6 / ok: all tests done`，rc=0。

70. **HIR 层面 self 从 KParam 转为 KResult，对齐 method-receiver-as-out-param 重构（2026-09-18 第四十七轮）**：#68 定义了方法语义为 `type.method = (inputs) (self type, rest-outputs...)`，HEAD 提交（`17e8362`）已在 codegen/hir2mir 侧把 self 当作第一个 KResult（out-param）处理，但 parser（`decl.go`）仍把 self 作为第一个 KParam（input param）插入。这导致三重断裂：① `lowerFunction` 的 `l.curRecv = resultVals[0]` 取到的是真正的 result param 而非 self；② `resultTypeOfCallee` 跳过第一个 KResult（以为是 self）导致方法返回类型退化为 void；③ `l.locals` 的 `paramNames[i]` 与 `params[i]` 索引不对齐（KResult self 在 KParam 之前但 paramNames 只含 KParam 名）→ 方法体内参数名绑定到错误的 value ID。**修法**：在 `tohir.go` 的 `funcLike` 中，对方法定义把 self 从 KParam 转为 KResult 并**置于 HIR children 首位**（`[self_KResult, param1_KParam, ..., result1_KResult, ...]`），确保 LLVM 参数顺序与 `emitCallBody` 的 `callArgs` 顺序一致（`[self_ptr, arg1, ...]`）；同时在 `hir2mir.go` 的 `lowerFunction` 中重建 `l.locals` 的 KParam→value 映射（按 HIR children 顺序遍历，用 `pIdx+rIdx` 索引到正确的 `params` 条目），并在 `bindOutParams` 中排除 self 不计入 `kResult`。验证：`test-arr.no` 全 17 个子测试 PASS（此前 CERR）；`test-basic.no`/`match.no`/`option.no`/`test-diff-debug.no`/`test-vec.no`/`test-str.no` 全部无回归；MIR 单测 13/13 全绿；`no vet src/std` 无新增 ERROR。
