# Nolang 全面体检报告

**审计时间**：2026-09-27 23:15 – 23:50 (GMT+8)
**审计基线**：`HEAD = ae940358`（`fix(build): ensure diagnostic locations use correct source file info`）
**语料规模**：`tests/*.no` 445 个 + `tests/mem-safety/` 等子目录；金标 488 条；`src/std` 39 个顶层模块（含子目录共 100+）
**编译器**：`bin/no`（重编自 ae940358）+ 隔离快照 `/tmp/no-audit` + 历史对照二进制（v0.3.8 / v0.3.9 / 各候选 commit）

---

## 0. 执行摘要

**结论：功能面在收敛，但"防回归"这条腿是断的。当前 HEAD 有 13 个语料文件编译直接失败，其中 8 个是刚刚引入的回归；而 CI 完全没有测试门禁，所以它们不可能被自动发现。**

| 维度 | 状态 | 关键数字 |
|---|---|---|
| 编译正确性 | 🔴 **回归中** | 13 档 `no run` rc=1（金标 REGRESS=13） |
| 单元测试 | 🟡 | `go test ./...` 6 项失败（4 build + 1 build/js，含 1 项新暴露的真实缺陷） |
| 静态检查 | 🟢 | `no vet tests` = 0 error / 38 warning / 100 hint；`no vet src/std` = 0 error / 47 warning / 212 hint |
| 标准库测试 | 🟡 | `test/std/` 41 档 → 36 绿 **5 红** |
| 编译性能 | 🟢 | 小文件 2.2s，`http2.no` 2.5s（110s 级问题已解决） |
| CI 门禁 | 🔴 **缺失** | 无任何 `go test` / `no test` / 金标 / vet 门禁 |
| 金标时效 | 🔴 **过期** | 冻结于 2026-09-22（`63895e59`），此后 **70 个 commit** 未刷新 |

**三件最该先做的事**（详见 §8 路线图）：

1. **回滚或修掉 `ae940358` 的 MIR 部分** —— 它让 8 档程序编译失败（§2.1）。
2. **补 CI 测试门禁** —— 否则下一秒还会有新的回归被发布出去（§6.1）。已发布的 v0.3.9 就带着 5 档编译失败（§2.2），就是这个洞的直接后果。
3. **刷新金标** —— 否则 REGRESS 这个数字失去意义（§6.2）。

---

## 1. 审计基线与判定方法

所有结论均可复现。关键手法与坑：

- **验证二进制一律重编**：`cd src && go build -ldflags="-s -w" -o ../bin/no ./cmd/no`（本机需 `export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"`）。
- **⚠️ 工作区被并发会话活跃修改**：本次审计期间 HEAD 从 `2b9946ed` 被推进到 `ae940358`，工作树另有 20+ 文件在飞改动（含 `src/std/*`、`tests/*`）。因此：
  - 结论全部用 **隔离快照** 复核：`tar cf - src | (cd /tmp/audit-src && tar xf -)` → 编 `/tmp/no-audit`；
  - 再用 **纯 HEAD worktree** 编 `/tmp/nh-bin` 二次确认，排除"并发在飞改动"的干扰。**两者结论一致**，故报告中的失败均属 HEAD 本身，不是他人未提交代码造成的。
- **回归引入点用二分定位**：`git worktree add /tmp/nb-<sha> <sha>` 编对照二进制，跑同一语料，逐 commit 收敛。
- 金标扫描：`NO=<abs path> GOLDEN=tests/golden/mir-baseline.tsv MIR_GOLDEN_JOBS=4 scripts/mir_golden.sh`（约 8 分钟）。

---

## 2. 🔴 P0：当前 HEAD 的编译级回归（13 档）

金标扫描结果（对比 2026-09-22 冻结基线）：

```
SAME=463   DIVERGE=6   UNSTABLE=0
REGRESS=13 (golden ok -> now fails)   IMPROVED=0   BOTH_FAIL=0   NEW=16
```

13 档 REGRESS 实测分为两组，根因不同、引入时点不同。

### 2.1 【8 档】MIR 生成 i32/i64 类型不匹配 —— 由最新提交 `ae940358` 引入

**现象**（当前 HEAD 实测，`no run` 全部 rc=1）：

```
Error: compilation error: MIR codegen could not handle tests/regexp.no:
opt-verify: MIR IR failed LLVM verification:
  opt: .../m.ll:8819:13: error: '%lv2668' defined with type 'i32' but expected 'i64'
  store i64 %lv2668, ptr %gp2669
```

**受影响文件（8 个）**：
`tests/regexp.no`、`tests/regexp1.no`、`tests/re-debug.no`、`tests/re-dump.no`、`tests/regex-literal.no`、`tests/quant-all.no`、`tests/quant-all1.no`、`tests/dump2.no`

**二分结果（决定性证据）**：

| commit | `tests/regexp.no` |
|---|---|
| `41494277` (v0.3.9) | ✅ rc=0 |
| `41c1f153` | ✅ rc=0 |
| `bd600ab8` | ✅ rc=0 |
| `2b9946ed` | ✅ rc=0 |
| **`ae940358` (HEAD)** | ❌ **rc=1** |

⇒ **唯一引入者是 `ae940358`**。它的主体是"诊断位置修复"，但同时带了 MIR 后端改动（`src/mir/codegen.go` +35、`src/mir/hir2mir.go` +28），正是这两处造成回归。

**根因方向（高置信）**：`ae940358` 在 `emitSetField` 新增了 `declaredFieldLT`（`codegen.go` ~7983 行），把字段写入的 store 类型从「RHS 推导类型」改为「字段**声明**类型」。动机正确（修 option 值写入标量字段时把 16 字节 `{tag,payload}` 塞进 8 字节字段的 bug），但**没有补位宽对齐**：当声明类型推导出的 LLVM 类型（i64）与值的实际定义类型（i32，例如 `char`/`bool`/枚举/窄整型字段）不一致时，直接产出 `store i64 %lv` 而 `%lv` 是 i32 → LLVM verifier 硬失败。

同提交另一处 `hir2mir.go` 改动（option 变体上下文 `variantCtx`，让 `ok`/`err`/`nil` 在 option-match 测试中不被同名局部变量遮蔽）也可能参与类型选择，需一并回归。

**解决方案**：
1. **止血（推荐先做）**：`git revert` 或只回退 `ae940358` 的 `src/mir/*` 部分，保留其 checker/build 诊断修复（那部分是有价值的）。
2. **根治**：在 `emitSetField` 里，取到 `fieldLT` 后与值的实际类型做位宽比较，不一致时插入 `sext`/`zext`/`trunc`（整型）或 `bitcast`（指针），再 store。option→标量字段的解包分支要保留。
3. **回归钉**：把这 8 档加入必须通过的清单（当前它们不是单测，只是语料，CI 又不跑 → 所以才会漏）。建议新增 `src/mir/setfield_width_test.go`，用最小复现（`holder { c char }` + `h.c = x`）钉住，比跑 8 个大文件快得多。

**验证**：8 档 `no run` 全部 rc=0 且输出与 `2b9946ed` 一致。

### 2.2 【5 档】`unknown callee str.init` —— 由 `64e4615d` 引入，**已随 v0.3.9 发布**

**现象**：

```
Error: compilation error: MIR codegen could not handle tests/mem-safety/minimal-str-map.no:
EmitLLVM: unknown callee str.init in func test-minimal
```

**受影响文件（5 个）**：`tests/mem-safety/{minimal-str-map, minimal-str-map2, minimal-option-str, option-str-match, map-tombstone}.no`

**二分结果**：

| commit | `minimal-str-map.no` |
|---|---|
| `9c459765` (v0.3.8) | ✅ rc=0 |
| `da756f8d` | ✅ rc=0 |
| **`64e4615d`** (`fix(mir): fix three option/match LLVM backend bugs and improve global constants`) | ❌ **rc=1** |
| `554d051a` / `41494277` (v0.3.9) | ❌ rc=1 |
| `ae940358` (HEAD) | ❌ rc=1 |

⇒ **引入者是 `64e4615d`，且它是 v0.3.9 的一部分** —— 也就是说 **v0.3.9 发布时就带着这 5 档编译失败**，一路带到今天。这是"无 CI 测试门禁"的直接代价。

**根因方向**：这 5 档都用 `m = {}` + `m.init()`（`[str]str` 的 `str-map` 模板）。`unknown callee str.init` 说明 `str-map.init` 这类模板方法的 mangled 名在解析时被截成 `str.init` —— 名字解析/单态化环节把「类型名 + 方法名」的边界切错了。`64e4615d` 改动了 option/match 后端与全局常量处理，很可能是全局常量/模块作用域那条路径影响了具象化后的符号名解析。

**解决方案**：
1. 在 `resolveCallee` / mangled 名解析处打印候选符号表，定位 `str-map.init` 被切成 `str.init` 的切分点（`src/mir/hir2mir.go` 的 `resolveCallee`、`canonSliceRecv`、`mangledSuffixMatches` 是历史高发区）。
2. 修好后**必须**补一条 CI 能跑的断言（见 §6.1），否则同类问题还会再发布出去。

---

## 3. 🟠 P1：MIR 后端内存安全与正确性（当前仍存在）

以下 4 项经代码核实**仍然开放**（均以 `src/mir/codegen.go` 注释自认为证）。

### 3.1 `%str-long` 下标写没有容量增长护栏 → 越界堆写

- **位置**：`codegen.go:6768-6773`（注释自认 "deliberately NOT addressed here"）、`ensureStrLongBuffer` `:6774-6829`（只有 alloc/done 两块）
- **对照**：`ensureVecBuffer`（`:6602-6753`）有完整的 `lCheck`（`need > cap`）+ `lGrow`（malloc+memcpy+free）分支；`%str-long` 只有 null-data 分支。
- **可达性（非理论）**：字面量 `s = 'ab'` 走 `@str_from_const` → `malloc(len)`、`cap = len`（`:1954-1962`）；`with-cap(n)` → `malloc(n)`（`:12270-12312`）。而 `emitIndexStore` 结尾无条件 `emitExtendLen` 把 `len` 抬到 `idx+1`（`:7368-7382`），等于**承认越界写合法**。
- **最小复现**：`s str = 'ab'` → `s[8] = 'x'` → 写穿 6 字节，随后越界读。
- **影响**：静默堆破坏（不崩、值错），最难发现的一类；`str` 是主力类型，`s[i]=c` 在 `to-upper`/`to-lower`/`fill`/`set-byte` 族大量使用（它们靠 `with-cap(.len)` 恰好不越界，所以现状没炸）。
- **解法**：把 `ensureVecBuffer` 的 case 2 平移到 `ensureStrLongBuffer`——在 `lDone` 前加 `lCheck`（`idx+1 > cap` 且 `data != null`）与 `lGrow`（`newCap = max(cap*2, idx+1, 1024)`，malloc+memset+memcpy(min(len,cap))+free）。注意 `%str-long` 的 data 是 `i8*`，无需 ptrtoint 往返。

### 3.2 `[N]T → []T` 字段赋值对非平凡元素产生栈借用视图 → 悬垂指针

- **位置**：`vecFromArraySink` `codegen.go:10602-10613`：只有 `i8/i1/i64/double` 元素走堆拷贝 `vecOwnedFromArray`，**其余一律 `vecViewValue`**（`:10519-10540`，产出 `{len=N, cap=0, data=&arr[0]}`，指向栈上数组）。调用点 `emitSetField` `:8120-8127`。
- **关键**：函数注释说「缺 deep clone 所以退回借用视图」，但 **`vecDeepClone` 已经存在**（`codegen.go:1297`，且已在 4 处使用）→ 这是**过期判断**，修复路径已具备。
- **影响**：`[N]str`、`[N]struct`、`[][]i64` 全部命中；字段逃离当前帧即悬垂。且 `cap=0` 会让 `ensureVecBuffer` 的 `cap > 0` 护栏**永久跳过**增长，后续写入继续往栈地址写。
- **解法**：default 分支改为 `vecDeepClone` 产出堆持有副本；类型解析不出时 `c.fail` 报清错，不要静默产出别名。

### 3.3 `%vec` 元素赋值共享底层缓冲，缺 deep clone → double free / UAF

- **位置**：`emitIndexStore` `codegen.go:7350-7362`：只有 `%str-long` 元素插入 `@str_clone`，注释自认 "%vec / heap-option elements still share: there is no vec clone helper"。`cloneStructElemLeaves` `:5675-5677` 对容器元素直接 `return`（跳过克隆）。
- **影响**：`[][]i64`、`[]struct-with-slice`；`nested-container-clone.no` 只覆盖了 `push` 路径，**元素赋值 `m[0] = one` 路径仍缺克隆**。
- **解法**：`elemT == "%vec" && valT == "%vec"` 时插入 `vecDeepClone`（已在 `emitMove` `:5235` 验证可用）。

### 3.4 `%vec` 借用视图写入会原地改他人存储（架构性技术债）

- **位置**：`ensureVecBuffer` 文档 `:6597-6601` + `ensureStrLongBuffer` `:6770-6773`，两处均声明"borrowed view 写入是 separate, pre-existing question"。
- 判据：`lCheck` 用 `cap > 0` 排除借用视图 ⇒ 借用视图永不增长 ⇒ 写入原地落到源串存储上。
- **建议**：列为技术债条目，纳入统一的所有权模型设计，而不是逐个打补丁。

**已确认关闭的历史缺陷**（避免重复劳动）：嵌套成员链写入丢失（`h.k[0]=x`，已由 `lvalueAddrOf` 修复）、`%str-long` null-data 护栏（已加）、大数组物化致 110s+（`shouldUseMemcpy`，已修）、`json.parse('')` SIGSEGV（已修）、交叉编译走宿主（`SetTargetPlatform`，已修）、`internType` 悬垂指针（已修，有回归测试）。

---

## 4. 🟠 P1：诊断工具链（`no vet` / `no fmt` / LSP 一致性）

### 4.1 失败单测暴露的真实缺陷：导入模块深层节点无 `SourceFile`

```
--- FAIL: TestVetImportedModuleDiagnosticsNotAttributedToMainFile
  诊断归到主檔 .../main.no 的第 567 行，但主檔只有 9 行：nolang-overflow-ineffective
```

**根因链（逐环可证）**：

| 环节 | 位置 | 事实 |
|---|---|---|
| ① std 磁盘路径解析失败 | `src/build/transpiler.go:611-647` `resolveModuleFile` | std 分支要求 `os.Stat` 成功，否则 **return ""** |
| ② std 源目录定位依赖二进制位置 | `src/package/stdpath.go:48-87` | `$NOLANG_STD_SRC` → `<exeDir>/../src/std` → `~/no/src/std`；`go test` / 非标准布局部署时全部落空 |
| ③ 空串写进整棵 std 子树 | `transpiler.go:1236/1242/1264` | `SetSourceFileDeep(fd, "")` |
| ④ "来源未知"被当成"主档" | `src/checker/lint.go:106-119` `isImportedCopy` | `if file == "" { return false }` → 不跳过；`:134` `file = sourcePath` → std 行号贴到主档 |

**解法**（按优先级）：① `resolveModuleFile` 空路径回退成逻辑模块路径（`"std/number"`），至少让 `isStdPath` 能识别；② `isImportedCopy("")` 语义反转——merged 程序里"来源未知"应**跳过**而非当作主档；③ `SetSourceFileDeep` 补齐 `ReturnStatement` / `FunctionLiteral` / `CallExpression` 实参（`src/parser/ast.go:360-383/419-453/458-477` 目前漏这几类）；④ 单态化克隆改用 Deep（`transpiler.go:4685`、`generic_structs.go:356` 现在只做浅层，克隆出的新节点 SourceFile 为空）。

### 4.2 merged 模式 std 误报：现有"过滤"是**掩盖**，不是根治

- 现状：`lint.go:626-641` 在 `RunAllLints` 结尾，当检查目标不是 std 时丢弃 `isStdPath(r.File)` 的结果。注释自己写明根因是「lowering 重建 AST 节点、注解侧表 `nodeSem`（**以节点指针为键**，`resolver.go:341/367`）查不到」——但代码没修 `nodeSem`，只是把结果扔掉。
- **三个破口**：① 依赖 File 正确，而 §4.1 证明 File 在多条路径上为空 ⇒ 过滤失效（实测那条第 567 行就是这么漏出来的）；② 只过滤 std，不过滤用户模块 ⇒ `no vet A.no` 会报出 B.no 的诊断；③ **可能吞真诊断**（见 4.3）。
- **漏网入口**：`transpiler.go:2523/2537/2545` 的编译硬错误（`ValidateLHSInferredBuiltins` / `ValidateFuncArgCount` / `ValidateDivByZero`）**完全绕过 `RunAllLints`**，std 内部问题会被当成被编译文件的错误报出，且错误串只有 `line, column`、**没有文件名**。
- **解法**（根治）：把 `nodeSem` 的键从节点指针改为稳定节点 ID，或在 lowering / `substituteBody` / `prefixMethodNames` 重建节点时显式搬运 `NodeSemantics`。过滤逻辑降级为兜底，不作为修复。

### 4.3 行号范围回退 ⇒ 静默假阴性

- `lint.go:600-614`：`for _, r := range ranges { if line >= r.startLine && line <= r.endLine { …; break } }` —— **取第一个命中**。
- `merged.Statements` 顺序是 [用户模块 → 主档函数 → std → 主档其余顶层语句]，而主档**顶层非 FunctionDefinition 语句**没有 SourceFile（`transpiler.go:2349` 的 append 未调 `SetSourceFile`）⇒ 没有自己的区间 ⇒ 其诊断会被排在前面的 **std 区间吞掉** ⇒ 被 std 过滤直接丢弃。
- std 里 `yaml.no` 3186 行、`str.no` 2892 行，几乎覆盖任何小文件的行号，命中概率极高。
- **解法**：给主档所有顶层语句补 `SetSourceFile`；回退时主档区间优先于 std；匹配不到时保留 `File==""` 交由调用方决定，不要猜。

### 4.4 其它一致性问题

- **约 10 个 lint 源完全不填 `File`**（`lint.go:233/274/301/368/418/431/452/461/473/570`），与相邻有 `File:` 的分支口径不统一 —— 结构性缺陷，建议统一走一个构造函数。
- **输出路径写法不一致**：同一次 `no vet src/std` 里混着 `src/std/process.no`（相对）与 `/Users/.../src/std/toml.no`（绝对）。
- **LSP 与 CLI 口径不一致**：LSP 走 `LightweightMode:true` + `SkipTypeChecks:false` + `MainFileVarNames:nil`（`src/lsp/vet.go:92`、`server.go:148`），`no vet` 走相反配置 —— 同一份代码两处诊断结果不同，是独立的漂移风险。

### 4.5 ⚠️ 默认 `no fmt`（不带 `--fix`）仍会做**语义改写**

`src/cmd/no/main.go:1258-1282`：默认路径上会执行两件改语义的事——① `fixRedundantTypeSource` 删除冗余类型标注；② `FormatProgramWithOverflow` 删除被判为"无效"的 `#{overflow}` 注解。

风险：两者的判定都用**单文件独立解析**（`main.go:1909-1913`、`fmt/api.go:194-197`），不带包上下文，与 `no vet` 的包上下文判定可能**方向相反**——`checker.go:4330-4334` 的注释本身就记录了这个冲突：「拼接行的 `#{overflow}` 被 ovfhndld **硬错要求**，而 `no fmt` 的 relevant 集却判其**无效**」⇒ 存在「`no fmt -w` 删掉一个 `no build` 必需注解」的死循环。

**解法**：默认 `no fmt` 只做排版，把删注解/去标注全部移到显式 `--fix=`；或让 fmt 的删除判定复用包上下文（`VetFileWithLints` 而非 standalone 解析）。

> 已确认修复：`no fmt` 丢 `#{index-out}` 的缺陷**已修**（`src/fmt/index_out_preserve_test.go` 3 个子用例 PASS，9 个大文件计数零丢失）。`src/cmd/` 下 4 个近重复的溢出迁移工具**已全部删除**，无代码/注册/文档残留。

### 4.6 三个 `--fix` 工具的已知局限

| 工具 | 局限 |
|---|---|
| `--fix=overflow` | 下钻不全（`ReturnStatement` 的 IfExpression 值、`CallExpression` 实参闭包**静默跳过**）；一律写 `wrap`，不区分该用 `clamp0/min/max/saturate`；同一行多个语句共享注解时只生效一次 |
| `--fix=match` | 插入的是 `nil -> print('nil')` 占位臂 —— 程序从"漏处理"变成"静默吞掉"，是**行为变更**；同一行结束的两个 match 只修第一个，需重跑；`?=` 合成 match 无锚点，跳过 |
| `--fix=redundant` | MapLiteral / 十六进制字面量 / 跨行标注一律不改；只处理 `LetStatement`；`sourceParses` 兜底失败会**整档放弃**（部分成功也回滚） |

**建议**：`--fix=match` 的占位臂策略应改为「默认不插入行为，只标注待办」或要求显式 `--arm-body` 参数，避免静默改变程序语义。

---

## 5. 🟠 P1：标准库

### 5.1 `test/std/` 41 档实测：**36 绿 5 红**

| 文件 | rc | 结果 | 根因方向 |
|---|---|---|---|
| `test/std/regexp.no` | 1 | **编译失败** | 同 §2.1 的 MIR i32/i64（导致 regexp 整块不可测） |
| `test/std/bufio.no` | 1 | 3/6 | **`fs.read` 入参名与返回名撞名**：`src/std/fs.no:205` `read = (fd fd, buf []byte, n i64) (n i64)` ⇒ `n > 0` 恒不成立 ⇒ `fill` 返回 false、`read-byte` 全返回 nil。对照：紧邻的 `write` 写 `(written i64)` 就没撞名，**测试是绿的** |
| `test/std/math.no` | 1 | 27/29 | `math.abs` 是纯 builtin（`:7` `#{buildin} abs = (x f64) (res f64) { }`），同文件 `sqrt/pow/ceil/floor/round/trunc` 全绿 ⇒ **缺陷在 builtin lowering（fabs 映射），不在 std** |
| `test/std/heap.no` | 1 | 11/15 | `heap.pop`（`:60-107`）第二次 pop 起顺序错；`:71` 先 `.n = .n - 1`、`:74` 立刻 `.data[0] = .data[.n]`，疑似接收者字段写回在该路径未接上 |
| `test/std/uuid.no` | 1 | 16/17 | `new-v4 → to-str → from-str → eq` 往返不等。对照 `tests/bigint-uuid-byte-indexing.no` 8/8 PASS（证明 `to-str`/`from-str` 对字面量正确）⇒ 故障在 `new-v4`。`uuid.no:34-73` 用裸块 + 相邻 `->` 链填字节，而 `txt.no:1086` 注释实证「只有第一個會執行」⇒ **每 4 字节只填 2 个，16 字节里 8 个从未随机化**（这是独立的 UUID v4 熵缺陷） |

**P0 级的两条一行修复**：`fs.no:205` 返回名改掉（`(got i64)`），`process.no:79` 同款撞名同步改 —— 全 `src/std` 仅此 2 处。改完立即重跑 `test/std/bufio.no` 验证。

### 5.2 测试覆盖缺口（大面积盲区）

| 范围 | 缺口 |
|---|---|
| 顶层模块零测试（9 个） | **yaml**（78.9 KB）、**markdown**（24.1 KB）、**toml**（25.6 KB）、args、async、embed、types、enter、leave |
| `src/std/crypto/` | **31 个模块 → 0 测试**（AES/DES/RSA/ECDSA/Ed25519/X509/各哈希/KDF 全裸奔） |
| `src/std/net/` | **19 个模块 → 0 测试**（http/http2/http3/tls/ws/quic/dns…） |
| `src/std/database/sql.no` | 0 测试 |
| `src/std/archive/` | 7 个 → 仅 gzip、zlib 2 个 |
| `src/std/collection/` | 12 个 → 5 个 |

**这是当前最大的质量风险**：crypto 与 net 是安全敏感区，却完全没有自动化验证。

### 5.3 `json.no` 的 +96 行修复**零回归覆盖**

`ae940358` 同时烘进了 std 改动（`src/std/json.no` +96、`src/std/path.no` +3），内容正确且有价值：
- `json.get-i64` 修 f64→i64 位重解释（`:1223`，原来 `42.0` 会变成 `4631107791820423168`）；
- 新增 `json-pool.copy-tree`（`:728-801`）+ 重写 `json.set-key`（`:1705`），修跨节点池悬空索引；
- `path.exists` 目录也算子存在（`path.no:30-36`）。

**但**：`test/std/json.no` 的 28 个用例**没有**覆盖 `set-key` / `copy-tree` / `arr-push` / `delete-key` / `obj-keys`。即这次核心修复**没有任何回归测试**。

**另两条风险**：
- `JSON-MAX-NODES = 64`（`json.no:37`）硬上限，`copy-tree` 的 BFS 队列是定长 `[64]i64`（`:733-734`），超 64 节点子树 ⇒ `set-key` **静默返回 ok=false**。对真实 JSON 是很低的门槛，应提高上限或显式报错。
- `copy-tree` 未处理「根已存在但非对象」的情况。

### 5.4 文档与实现不符（3 例实证）

| 文档 | 实现 | 后果 |
|---|---|---|
| `docs/docs/std/module-overview.md:67` 列 `encoding/hex` 子模块 | `src/std/encoding/` 只有 base64/csv/pem，全仓无 hex | 照抄文档即编译失败 |
| `docs/docs/std/data-exchange.md:21-30` 写 `json.parse(s,n)` 两参、`json.parse-str`/`parse-num` 存在、`json.stringify(v,out)` | 实际 `json.parse = (s str) (?json)` **单参**；无 parse-str/parse-num；`stringify` 是**零参方法** | 4 个示例有 3 个照抄即错 |
| `module-overview.md:38` 收录 `unicode` 模块 | 不存在；且 `others.md:9-13` 自相矛盾说"已分散至 char 和 str" | 模块表误导 |

**解法**：以 `src/checker/stdsig_gen.go`（自动生成的权威签名表）为准，写个脚本校对文档，纳入 CI。

### 5.5 std 源码里"承认编译器 bug"的 workaround（技术债清单）

这些注释是**最有价值的缺陷索引**，每一条都对应一个编译器侧的真实缺陷：

| 位置 | 承认的问题 |
|---|---|
| `fmt.no:23-24` | 标量 i64 out-param 在条件块内指派**不生效** |
| `fmt.no:137` | do-while 的 `-> else` 分支**始终被执行** |
| `str.no:1551` | out-param 上 `.push()` 不生效 |
| `vec.no:210-211/411` | 方法内调 `.push()` 触发 LLVM bug，只能内联规避 |
| `vec.no:146/286/301/312/328/341/350/369/388` | 需 `sz = .len()` 强制 vec self 类型，否则错误特化为 arr 方法（**9 处重复**） |
| `regexp.no:54/675` | LLVM 对 `== 42` 有优化 bug，改用 1/2/3 魔数规避 |
| `byte.no:101-103/123`、`char.no:219` | `[]char` 切片支持不完整，**方法不可用**；"不使用变量下标，规避长度 bug" |
| `process.no:618-623` | `process-shell` 返回 C `system()` 原始 wait status（退出码 1 → 256）、无法捕获输出、**命令注入风险**、无超时 |
| `process.no:665` | **`&&` 不短路** |
| `process.no:872` | 函数中段裸块 + `->` 链后会**提前返回** |
| `txt.no:1086` | 多个相邻 `pos < cap -> {}` 短路式语句**只有第一个执行** |
| `txt.no:111/1363` | `[]t.set-byte` builtin 不支持；`[]txt` 切片方法不支持，只能改全局函数 |
| `json.no:28` | 枚举值不递增，只能用常量替代枚举 |
| `str.no:765` | 不检查 i64 范围溢出（静默截断） |
| `yaml.no:345` | 数值字面量不支持指数（`1e308` 被切成 `1` + `e308`） |
| `fs.no:73` | 返回顺序为 (name, fd) 反直觉 |
| `vec.no:54` | Nolang 尚无数组索引语法，实现在 codegen 里 |

其中 `&&` 不短路、`->` 链只执行第一个、out-param 在条件块失效这三条是**语义级缺陷**，直接影响用户代码正确性，优先级应高于表面的 warning 清理。

---

## 6. 🔴 P2：工程与流程

### 6.1 CI 没有任何测试门禁（**最严重的流程缺陷**）

`.github/workflows/build.yml` 只有两个 job：

- `build-release`（仅 tag 触发）：5 平台交叉编译 + 发布。
- `wasm-smoke`（仅 PR 触发）：编译 `bin/no` → `no build -cc zig -target wasm32-wasi` 跑 `wasi-hello` / `wasi-fib` 两个文件 → 起 Docusaurus 验 playground。

**没有** `go test ./...`、**没有** `no test test/std/`、**没有** 金标扫描、**没有** `no vet` 门禁。

这就是 §2.2 那 5 档编译失败能随 v0.3.9 发布的直接原因，也是 §2.1 那 8 档刚引入就无人知晓的原因。**补这个门禁的收益大于修任何一个具体 bug。**

建议的最小门禁（详见 §9）。

### 6.2 金标已过期 70 个 commit

`tests/golden/mir-baseline.tsv` 冻结于 2026-09-22（`63895e59`），此后 **70 个 commit**。后果：

- `REGRESS=13` 这个数字**混杂了"真回归"与"有意行为变更"**，不能直接当结论（本次靠逐档二分才分离出来两组真回归）。
- `NEW=16`（语料新增但金标无记录）意味着这 16 档**完全没有回归保护**。
- `DIVERGE=6`（`ffi-sqlite`、`markdown`、`net-client`、`path-char`、`slice-heavy`、`test-div-mod-option`）输出变了但 rc 未变，需人工判定谁对——按既有口径，`markdown.no` 输出本身不确定，其余应逐个用测试自带的 `expect:` 判读。

**解法**：修完 §2 的两组回归后，**刷新金标**（`scripts/mir_golden.sh -update`，注意必须用 `GOLDEN=` 指定路径，脚本无位置参数），并把刷新动作固化进发布流程。

### 6.3 并发会话共享工作区（风险正在发生）

本次审计期间：HEAD 从 `2b9946ed` 被推进到 `ae940358`，工作树同时有 20+ 文件在飞改动。历史上已多次因此产生混淆：

- 曾出现"同一变量两次结论相反"；
- 曾出现工具跑飞把编译器内部 lowering（`__idx_out_220_21`、`combined[i] = =`）倒灌进源文件；
- 曾出现"报错的函数名在文件里根本不存在"（他人正在改测试文件）。

**建议**：① 长跑/归因前把 `bin/no` 复制到私有路径当快照，别用会被重编的 `./bin/no`；② 归因"我的改动 vs 他人 churn"用隔离二元组（`tar` 当前 `src` + 只还原自己的文件）；③ 关键改动前后 `git diff --stat` 存快照。

### 6.4 其它工程卫生

| 项 | 现状 | 建议 |
|---|---|---|
| 未推送 commit | **4 个**（`41c1f153`、`bd600ab8`、`2b9946ed`、`ae940358`），其中 `ae940358` 引入 8 档回归 | 修完回归再推；当前状态不宜发版 |
| `tmp/` | 645 个文件 / 16M（含 482 个 `.no` 试验档） | 清理或加 `.gitignore`；它是"临时实验"的堆积，也是"工具跑飞"的来源地 |
| `dist/` | 74M 构建产物 | 已 gitignore，但建议定期清 |
| `.git/` | 123M | 可考虑 `git gc` / 清理历史大文件 |
| `--fix` 帮助文本 | `main.go:1145-1147` 列出三个取值，但 Usage 示例块与 `printUsage()` **均未提及** | 补齐，否则用户只能靠 `--help` 发现 |

---

## 7. 🟡 P2/P3：语言与工具体验

来自既有经验与本次核实的用户可见陷阱（多为"能编译但语义不符预期"）：

| 陷阱 | 说明 |
|---|---|
| option match 必须写满 `ok`/`nil`/`err` 三分支 | 少一个是 parser error，新手高频踩坑 |
| `?=` 只能在有 option 型别 result param 的函数内用 | 顶层 script 与 `() {}` 无回传函数里用会 parser error |
| MIR 解 option 依赖显式 `?T` 标注 | `mx = a.max()` 再 match 会 codegen 失败，须写 `mx ?i64 = a.max()` |
| 行注解作用域 | 标在 match arm 之前**盖不住它自己的条件运算**；标在外层区块之上才覆盖整棵子树 |
| `println` / `eprintln` **不存在** | 只在已删除的 legacy 后端里有；现在只有 `print` / `eprint` |
| 结构体语法 | 必须 `Name { fields }` 定义、`Name { field: val }` 字面量；函数式写法会被错误降 Kind 为 int |
| `no fmt` 不带 `--fix` 会改语义 | 见 §4.5 |
| 方法调用不做元数检查 | 缺参补 0（自由函数才会报 `expects at least N argument(s)`） |

**建议**：把这张表整理成 `docs/docs/lang/` 下的「已知陷阱」页，比让用户从 issue 里重新发现便宜得多。

---

## 8. 解决方案路线图

### 阶段一：止血（建议 1–2 天内，目标是让 HEAD 重新可发布）

| # | 动作 | 验收 |
|---|---|---|
| 1 | 回退或修复 `ae940358` 的 `src/mir/*` 部分（保留其 checker/build 诊断修复） | 8 档 regexp/dump/quant 家族 `no run` rc=0 且输出与 `2b9946ed` 一致 |
| 2 | 定位并修 `str.init` 符号解析（引入于 `64e4615d`） | 5 档 mem-safety `no run` rc=0 |
| 3 | `fs.no:205` + `process.no:79` 返回名撞名 | `no test test/std/bufio.no` 6/6 |
| 4 | 修 `math.abs` builtin lowering（或先用 Nolang 实现绕过） | `no test test/std/math.no` 29/29 |
| 5 | `git push origin main`（4 个未推送 commit） | 远端与本地一致 |

### 阶段二：加固（1–2 周，目标是让回归不可能再溜出去）

| # | 动作 | 验收 |
|---|---|---|
| 6 | **补 CI 测试门禁**（§9） | PR 上能跑出红/绿 |
| 7 | **刷新金标** + 把刷新动作固化进发布流程 | `REGRESS=0` 有真实意义 |
| 8 | 修 §4.1 的 SourceFile 归属（4 步） | `TestVetImportedModuleDiagnosticsNotAttributedToMainFile` 转绿 |
| 9 | 根治 `nodeSem` 键（节点 ID 或重建时搬运） | 去掉 `lint.go:626-641` 的掩盖式过滤后 `no vet tests` 仍 0 error |
| 10 | 默认 `no fmt` 只做排版，语义改写全部移入 `--fix=` | 不存在"fmt 删掉 build 必需注解"的循环 |
| 11 | 补 `test/std/json.no` 的 `set-key`/`copy-tree`/`arr-push`/`delete-key`/`obj-keys` 用例 | 那 +96 行修复有回归保护 |
| 12 | `heap.pop` 用局部量固化 `n-1`；`uuid.new-v4` 展开 4 次赋值 | 两档转绿 |
| 13 | 提高 `JSON-MAX-NODES` 或超限时显式报错 | 不再静默 ok=false |

### 阶段三：长效（1–2 月，目标是让质量可持续）

| # | 动作 |
|---|---|
| 14 | 修 §3 的内存安全三项（`%str-long` 增长护栏、`vecFromArraySink` deep clone、`%vec` 元素 clone），并统一所有权模型（含 §3.4 借用视图） |
| 15 | 补测试覆盖：crypto(31) / net(19) / database(1) / yaml / markdown / toml / args —— 先冒烟后深入 |
| 16 | 用 `stdsig_gen.go` 校对 `docs/docs/std/`，修 3 处不符（hex / json / unicode），并纳入 CI |
| 17 | 清理 §5.5 的 workaround 债：优先 `&&` 不短路、`->` 链只执行第一个、out-param 在条件块失效这三条语义级缺陷 |
| 18 | 把 §7 陷阱表沉淀为官方「已知陷阱」文档 |
| 19 | 清理 `tmp/`（645 文件）、整理 `.workbuddy/skills/` 下的近重复技能 |

---

## 9. 建议的最小 CI 门禁

```yaml
name: test
on: [push, pull_request]
jobs:
  go-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with: { go-version: "1.26" }
      - run: cd src && go build ./... && go test ./... # 先接受既有 6 项失败白名单，逐步收敛

  std-test:                    # 标准库回归（最便宜、命中率最高）
    runs-on: ubuntu-latest     # 需要 LLVM 21 + clang
    steps:
      - run: make no
      - run: |
          fail=0
          for f in test/std/*.no; do
            ./bin/no test "$f" || { echo "FAILED: $f"; fail=1; }
          done
          exit $fail

  corpus-smoke:                # 语料编译门禁（防 §2 那类"编译直接失败"）
    runs-on: ubuntu-latest
    steps:
      - run: |
          fail=0
          for f in tests/*.no tests/mem-safety/*.no; do
            ./bin/no build -o /tmp/out "$f" || { echo "BUILD FAIL: $f"; fail=1; }
          done
          exit $fail

  vet-gate:
    runs-on: ubuntu-latest
    steps:
      - run: make no
      - run: ./bin/no vet src/std   # 必须 0 error
      - run: ./bin/no vet tests     # 必须 0 error
```

**取舍说明**：`corpus-smoke` 只编译不运行，成本低得多，却能挡住 §2 那类"编译直接失败"的回归（本次 13 档全部属于此类）。**只编译不运行比不跑强一个数量级** —— 优先级：`go-test` > `std-test` > `corpus-smoke` > `vet-gate`。全量金标扫描（7–17 分钟）建议只在发版前手动跑或做 nightly。

---

## 10. 附录：本次审计的可复现命令

```bash
# 0) 环境
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"

# 1) 重编验证二进制（铁律：不要用 PATH 上的陈旧 no）
cd src && go build -ldflags="-s -w" -o ../bin/no ./cmd/no

# 2) 单元测试
cd src && go test ./...            # 实测 6 项失败

# 3) 静态检查
./bin/no vet tests                 # 0 error / 38 warning / 100 hint
./bin/no vet src/std               # 0 error / 47 warning / 212 hint

# 4) 全量金标扫描（约 8 分钟）
NO=$PWD/bin/no GOLDEN=tests/golden/mir-baseline.tsv MIR_GOLDEN_JOBS=4 scripts/mir_golden.sh

# 5) 回归二分（定位引入 commit）
git worktree add /tmp/nb-<sha> <sha>
cd /tmp/nb-<sha>/src && go build -ldflags="-s -w" -o /tmp/nb-<sha>-bin ./cmd/no
cd /tmp/nb-<sha> && /tmp/nb-<sha>-bin run tests/regexp.no; echo $?
git worktree remove --force /tmp/nb-<sha>     # 用完立刻清理

# 6) 单档复核（避开并发会话污染：用隔离快照）
tar cf - src | (cd /tmp/audit-src && tar xf -)
cd /tmp/audit-src/src && go build -ldflags="-s -w" -o /tmp/no-audit ./cmd/no
```

---

## 11. 一句话总结

**编译器本体在持续变好（性能、内存安全、诊断都在收敛），但质量闸门是开着的：CI 不跑测试、金标过期 70 个 commit、工作区多人并发改。结果是 HEAD 上躺着 13 档编译失败（8 档昨天刚引入、5 档已随 v0.3.9 发布出去）。先补门禁，再修 bug —— 顺序反了，修多少都会被下一批回归抵消。**
