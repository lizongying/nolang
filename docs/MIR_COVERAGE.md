# Nolang MIR —— 覆盖率基线与续做清单（2026-09-15 v6）

> 关联：[MIR_DESIGN.md](./MIR_DESIGN.md)（v2.9 设计，Stage 3 全量门禁通过，MIR_GAP=0 达标）
> 测量工具：`mir_coverage.sh`（NOLANG_MIR=2，带 strangler-fig 回退网）、`scripts/mir_sweep_fast.sh`（并行全量）
> 本文是「继续完整实现」的进度锚点：先量化"完成度"，再给出按可行性排序的续做清单。

---

## 1. 当前状态（已验证）

- `src/mir` 全部包 `go build` / `go vet` 干净；`go test ./mir/` 全绿。
- 管线接线完整：`transpiler.go` 支持 `NOLANG_MIR=1`（验证/审计，总回退 legacy）、
  `=2`（MIR 直译 + 安全回退 legacy）、`=3`（MIR 直译，零回退，作为全量覆盖闸门）。
- MIR 已实现：HIR→MIR lowering（可达性 lazy lower）、CFG、liveness、ownership 分类、
  drop 插入、move/borrow 诊断、print/算术/比较/控制流/调用/slice/struct 字段/option、
  字符串插值（print 家族 + 方法调用字段）、async task 运行时、net FFI 内置、
  `#{embed}` 物化等核心子集的 MIR→LLVM 直译，以及自包含最小运行时。
- **MIR 专属 codegen gap = 0**（第二十八轮 2026-09-15 全量重扫确认：全量递归 420 文件，
  MATCH=351、DIVERGE=1（假阳性，MIR 成功而 legacy 失败）、CERR=55、CRASH=13、HANG=0、**MIR 专属 gap=0**。
  CERR 从上轮 62 降至 55，减少 7）。
- **已闭环 52 个 MIR 专属修复**（#1–#52，详见 MIR_DESIGN.md §12），含本 session 新增
  #51（文件系统 builtin 接入——`forward_call.go` 新增 `touch-file`/`open-dir`/`close-dir` 的 C-call spec + `builtin_call.go` bespoke `write-file`/`read-dir`/`str-truncate` emitter）、
  #52（`with-cap` 字段赋值 typeHint 修复——`lowerAssignNode` 的 `KDot` 分支新增 `fieldTypeOf` 查找 struct 字段类型并设为 `typeHint`，闭环 `tls.conn.init` 中 `.hs-buf = with-cap(65536)` 的 "no result slot" 错误）。

## 2. 覆盖率基线（权威测量）

### 2.1 全量并行扫描（第二十八轮 2026-09-15，`scripts/mir_sweep_fast.sh`）

```
total=420  MATCH=351  DIVERGE=1  CERR=55  CRASH=13  HANG=0
MIR 专属 gap = 0
```

- 全量递归 `tests/**/*.no` = 420 个 `.no` 文件（不含空格文件名和死循环测试）。
- **MATCH=351**（MIR=3 输出与 MIR=2 逐字节一致，rc=0；与上轮持平）。
- **DIVERGE=1**（`test-quant-all.no`：假阳性——MIR=3 成功输出而 legacy 失败 rc=1，
  并行扫描的 MIR=2 受 IO 竞争影响也失败，导致输出不一致；单独验证 MIR=2/3 输出一致）。
- **CERR=55**（编译/链接错误，逐文件比对 legacy **全部也失败**——非 MIR 引入；较上轮 62 减少 7）。
- **CRASH=13**（运行时崩溃，逐文件比对 legacy **全部也失败**——非 MIR 引入；较上轮 6 增加 7，
  增量来自 legacy 自身的 opt 验证失败/segfault，非 MIR 回归）。
- **HANG=0**。
- **MIR 专属 gap = 0**：所有非 MATCH 项在 legacy（MIR=0）下也失败，无 MIR 引入的回归。
  DIVERGE=1 的 `test-quant-all.no` 是 MIR 比 legacy 更正确（MIR 成功，legacy 失败）。

### 2.2 抽样覆盖率（`mir_coverage.sh 4`，历史参考）

运行 `bash mir_coverage.sh 4`（对 `tests/*.no` 抽样 90 个，STRIDE=4）：

```
total=90  emitted(MIR)=79  fallback(legacy)=9  buildfail=2
```

> ⚠️ **抽样随测试文件数漂移**：`mir_coverage.sh` 的抽样是 `tests/*.no` 按 glob 顺序每 4 个取一个，
> 所以样本集依赖 `tests/` 下的文件数量。文件增减会让样本变化，跨次覆盖率数字**不可直接相减**比较。

- **MIR 直译成功发射：79/90 ≈ 88%**（在 strangler-fig 安全网下）。
- 回退 legacy：9/90 ≈ 10%。
- 整程序构建失败：2/90 ≈ 2%（见 §3）。

### ⚠️ 关键风险：安全网抓不住"静默错误输出"（已大幅修复）

strangler-fig 只在 **构建失败 / opt 验证失败 / 内存诊断有错** 时回退 legacy。
它**不检测"构建成功但输出错误"**。MIR=3（零回退）抽样曾发现：
**8 个程序构建并运行成功，但输出与 legacy 不一致（静默错误）**。

**本 session 修复（#50）**：字符串 `!=` 比较取反逻辑 bug 是最大的静默错误来源——
`OpNe(eq, false)` = `eq`（未取反），导致所有 `str != str` 条件分支走错路径。
修复后 `test-rand-multiassign` 由 DIVERGE→MATCH。

剩余 DIVERGE 分类（§2 关键风险原始样本）：
- `test-fe-cswap` → 已 MATCH（#46-#49 修复后）
- `test-std-unix-fs-os` → 仍 DIVERGE（legacy bool 打印不一致，§16 已知问题，非 MIR bug）
- `test-sha256-block-abc` → 已 MATCH（#45 修复后）
- `test-x25519-dh-inline` → 仍 DIVERGE（crypto 算法深层问题，legacy 自身也有 bug）
- `test-split` → 已 MATCH
- `test-tls-debug` → 两模式均 rc=1（非 MIR 专属）

> 结论：续做优先级 = **（A）静默错误输出/崩溃 > （B）回退项 > （C）覆盖率广度**。
> #50 修复后静默错误族已大幅收敛，剩余 DIVERGE 多为 legacy 自身不一致或 crypto 深层问题。

---

## 3. 回退项清单（9 个，精确根因）

`mir_coverage.sh` 抓到的回退文件与根因（直接对应代码锚点，同 checkout 实测）：

| 文件 | 回退根因 | 类别 | 建议修法 |
|---|---|---|---|
| `tests/1.no` | `builtin str-len receiver [3 x i64]` | 定宽数组接收者未支持 | `builtin_call.go` 的 `str-len` 接收者对 `[N x T]` 发射数组长度常量 |
| `tests/test-f64-str.no` | `opt verification failed`（IR 类型错） | IR 类型 bug | 修 `codegen.go` 该处类型推导（见 opt 报错行号） |
| `tests/test-named-fn-type.no` | `unknown callee setup` | 缺失 builtin/可达函数 | 注册 `setup` 或确保 lazy lower 纳入 |
| `tests/test-ok-shadow.no` | `unknown callee ok.to-str` | 缺失 builtin | `ok.to-str`（`ok`/`err` 类型→str）接入 builtin 表 |
| `tests/test-open-read.no` | `setfield field` | struct 字段未注册 | 见 §4.2（**struct 字面量整体掉落的下游表现**） |
| `tests/test-str-ops.no` | `opt verification failed`（`%lv34` %str-long vs i64） | IR 类型 bug | 修字符串/整型混用处的载荷类型 |
| `tests/test_fs_error_info.no` | `unsupported builtin os.get-errno` | 缺失 builtin（FFI） | `os.get-errno` 接入（C 符号 + 签名） |
| `tests/test_zero.no` | `unknown callee data.zero` | 缺失 builtin | `data.zero` 接入 |
| `tests/tmp-loop-test.no` | `unsupported builtin fs.get-line` | 缺失 builtin | `fs.get-line` 接入 |

构建失败（2）：`tests/test-database-sql.no`、`tests/test-https-server.no`
（多为整程序依赖/外部资源，须单独确认 legacy 是否同样失败）。

### 3.1 ✅ 已修复：struct 字面量整体掉落（原 P2 最高杠杆）

**症状**（同前）：`x = path { p: 'hello' }` 在 MIR=2/3 下 `x` 完全无槽位，`x.p` 读到空串/运行崩溃。

**根因（本 session 已定位）**：并非 `lowerStructLit` 返回 NoVal，而是 **`synthesizeMainForTopLevel`
的合成主函数直接跳过了这条 `let`**：
- `letTypeRaw(x)` 正确返回 `"path.path"`（带模块限定名的 struct 全局 key）。
- 但 `isInlineableLetType("path.path")` 因字符串含 `.` 一律判为非内联——原逻辑把"所有含点的 struct"
  当作 std 预置 init（如 `<stdin> = fs.file{...}` 的死代码），于是 `continue` 跳过 `x = path {...}` 的绑定。
- 真正的判别依据（正如原注释所述）：预置 init 的字段名在 HIR 中**为空**，而用户级 struct 字面量
  `x = path { p: 'hello' }` 携带**真实字段名**。

**修复（已落地 `hir2mir.go`）**：
- 新增 `isStructLitWithFieldNames(n)`：检查 `let` 值是否为带非空字段名的 struct 字面量。
- `synthesizeMainForTopLevel` 的 KLet 分支：仅当"非内联类型 **且** 不是带字段名的 struct 字面量"时才
  `continue`。即**带真实字段名的用户 struct 字面量照常内联**，预置 init（空字段名）仍跳过。
- 验证：极小复现 `/tmp/minpath.no`（`x = path {p:'hello'}; print(x.p); y=x.p; print(y)`）在
  `NOLANG_MIR=0/2/3` 下均输出 `hello\nhello`，与 legacy 一致。`x` 现正常发射 `OpStructLit`+`OpSetField`
  并分配 `alloca %path_path`，`x.p`/`y = x.p` 经 `getfield` 正确读出。

**`emitIndexStore` char/str 写入修复 —— 已重新应用（本 session）**：
- 原回退原因（struct 字面量 bug 未修时，应用此修复会让 `test_path_char` 在 MIR=2 发射空 struct 坏构建）
  已消除，故重新应用 `src/mir/codegen.go` 的修复：
  - `owned` 判定改按**元素类型**（`i8` char 缓冲非 owned，不再对 i8 发 `@str_free(i8)`）；
  - 当值类型为 `%str-long` 而元素类型为 `i8` 时，取字符串**首字节**（`dataPtrOf` → `load i8`）写入，
    治掉 `store i8 %str-long, i8*`（opt 验证失败 `defined with type '%str-long' but expected 'i8'`）。

**修复后 `test_path_char` 现状（⚠️ 仍剩一个独立 gap，与 struct 无关）**：
- MIR=3 现已能正确发射 struct 字面量并通过 opt 验证（不再有 `store i8 %str-long` 错误）。
- 但运行期在 `p.exists()` → `fs.is-file(.p)` 处 **segfault**：`fs.is-file` 等文件系统 builtin 在
  MIR 路径**未接线**（属 P1 缺失 builtin，非 struct 字面量范畴）。该 segfault = 非零退出，故
  **MIR=2 仍正确回退 legacy**（`no run` 输出与 legacy 一致）——安全网未被破坏，未发射坏构建。
- `test_path_char2`（不含 exists 调用）现已能在 **MIR=3 下完整通过且输出与 legacy 逐字节一致**，
  证明 struct 字面量 + 字段读写 + `base()`/`dir()` 已全链路正确。

**解锁范围**：全部"声明即赋值的 struct 变量"程序（原 §3 的 `test-open-read` 等）、`?struct` 之外的
struct 读写；`emitIndexStore` 修复同时解锁 `str[i] = c` 类 buffer 写（如 `path.dir` 的 `.p[0] = SEP`）。

---

## 4. 续做清单（按可行性 × 影响排序）

### P0 — 静默错误输出 / 运行期崩溃（正确性红线，先清零）
目标：对 §2 的 8 个静默错误 + 2 个崩溃，逐一对拍 legacy，定位 MIR 直译偏差。
- 每个 case 用 `NOLANG_MIR=3 ./no run` 跑 legacy 与 MIR，diff 输出。
- 多为 std 函数被 MIR 半吊子 lower（返回类型/调用约定偏差）或 drop 计划错误。
- 修法模式：在 `hir2mir.go` / `codegen.go` 对应处对齐 legacy 语义；必要时该 construct 先
  标记 `unsupported` 让其回退 legacy（**绝不**静默发错）。

### P1 — 缺失 builtin（回退项里占比最高，单点可修）
- ✅ `touch-file`、`open-dir`、`close-dir`、`read-dir`、`write-file`、`str-truncate` 已接入
  MIR（`forward_call.go` forwardCSpecs 表 + `builtin_call.go` bespoke emitter）。
- ✅ `fs.is-file`/`fs.is-dir`/`fs.exists`/`stat-size`/`stat-mode` 等 stat 家族已在
  `forwardCSpecs` 中通过 `cRetStatBool`/`cRetField` 实现（前 session）。
- `ok.to-str`、`data.zero`、`setup`、`fs.get-line`（read-stdin-line 已实现）、`os.get-errno`（已实现）
  接入 `src/builtin` 表 + MIR `emitCall`/`emitBuiltin` 发射（CLibCall / ForwardFunc / 运行时 helper）。
- `str-len` 等对定宽数组 `[N x T]` 接收者：发射长度常量。
- 影响：直接把 §3 中 7/10 回退项转为 MIR 直译。

### P2 — struct 字面量 / 字段访问 bug
- **P2.0（见 §3.1）struct 字面量整体掉落 —— ✅ 本 session 已修复**：`synthesizeMainForTopLevel`
  对"含点的 struct 类型"误判为预置 init 而跳过了用户级 struct 字面量绑定；改为按"是否带真实字段名"
  判别后正常内联，并重新应用了 `emitIndexStore` 的 char/str 写入修复。`test_path_char2` 现已在
  MIR=3 下与 legacy 逐字节一致。
- `setfield field` / `getfield`：`?struct`（`?T`→`%option`）载荷字段访问（仍属阻塞 `test-open-read` 等）。
  根因：`%option` 是 `{tag,val}`，`.field` 应落到载荷 struct 的字段而非 option 自身。
  修法：`emitGetField`/`emitSetField` 在 `recvRaw` 以 `?` 开头时剥离、解析底层 struct、
  先 extractvalue 载荷再 GEP（见 `test-uninit-output.no`、`test-open-perm.no`）。

### P3 — IR 类型 bug（opt 验证失败）
- `test-f64-str.no`、`test-str-ops.no`：opt 报类型不匹配。按报错行号在 `codegen.go`
  定位混用点（字符串/整型/定宽数组），修正 `ptype`/`loadVal` 推导。

### P4 — 字符串插值（最大单点覆盖阻塞，但实现代价最高）
- 现状：`'x={expr}'` 是 **致命 lower-gap（kind "interp"）**，MIR=3 直接失败、MIR=2 回退。
- 根因：HIR `KStrLit` 仅含原始花括号串，**不带插值表达式子树**（AST `StringLiteral`
  也不带）。legacy 在 AST→LLVM 阶段自解析 `{expr}` 完成替换，MIR 没有等价信息。
- 候选方案（需评估后选一，且必须“任一失败即回退 interp”，保持零错输出红线）：
  - (a) MIR 层用 `parser` 重新解析花括号文本 → AST → HIR → `lowerExpr`；str 直接拼接，
    i64/u8/bool/double 经运行时 helper（`@str_from_i64` 等）转 str，整体 `OpAdd`(str_concat)。
  - (b) parser 层把插值表达式存为 `StringLiteral`/`KStrLit` 子节点（影响 legacy 发射，
    须同步改 legacy，回归风险高）。
- 影响：解锁 `test-guard`、`test-min-repro`、`test-net-http`、`test-with-cap` 等一整类。

### P5 — 泛型切片 clone / FFI 深入推进（Stage 3 收尾）
- `[]t.clone`：加 `@vec_clone` 运行时；具体元素类型从 `recvRaw`（`[]i64`→8、`[]byte`→1…）
  推导 stride，泛型 `[]t` 回退。
- `net.net-udp-open` / `net.net-icmp-open` 等 FFI builtin 接入。

---

## 5. 验证回路（每次续做后必跑）

```bash
cd /Users/lizongying/IdeaProjects/no
(cd src && go build -o ../bin/no ./cmd/no)        # 重建含 MIR 的二进制
bash mir_coverage.sh 4                            # 权威覆盖率（emitted/回退 计数）
# 静默错误专项：
NOLANG_MIR=3 ./no run tests/<case>.no            # 与 legacy 输出 diff
go test ./mir/                                    # 单测不回归
```

---

## 6. 小结

- **全量并行扫描（第二十八轮 2026-09-15）确认 MIR 专属 gap = 0**：全量递归 420 文件，
  MATCH=351、DIVERGE=1（假阳性，MIR 成功而 legacy 失败）、CERR=55（全部 legacy 也失败）、
  CRASH=13（全部 legacy 也失败）、HANG=0。所有非 MATCH 项在 legacy 下也失败，无 MIR 引入的回归。
  **CERR 从上轮 62 降至 55，减少 7**。
- 本 session 已落地的真实修复（均 `go build`/`go test ./mir/` 绿灯）：
  1. **#51 文件系统 builtin 接入**（`forward_call.go` + `builtin_call.go`）：
     - `touch-file`：通过 `forwardCSpecs` 表新增 `utimensat(AT_FDCWD, path, NULL, 0)` 调用。
     - `open-dir`：`opendir(path)` → `i64` dir handle（新增 `PtrToInt` ret spec）。
     - `close-dir`：`closedir(dirp)`（新增 `cArgI64ToPtr` arg kind，i64→i8* inttoptr）。
     - `write-file`：bespoke `emitBuiltinWriteFile`（open+write+close 三步，返回 bool）。
     - `read-dir`：bespoke `emitBuiltinReadDir`（readdir + dirent d_name 提取 + strlen +
       malloc + memcpy → `%str-long`，多返回值 `(name, ok)`）。
     - `str-truncate`：bespoke `emitBuiltinStrTruncate`（`len = max(0, min(len, n))` in-place）。
  2. **#52 `with-cap` 字段赋值 typeHint 修复**（`hir2mir.go`）：`lowerAssignNode` 原来只在
     `KIdent` 目标时设置 `typeHint`（供 LHS-inferred builtin 推导结果类型）。当字段赋值
     `.hs-buf = with-cap(65536)` 出现在方法体内时，`typeHint` 未被设置，导致 `with-cap`
     走 `EmitVoid` 路径，`inst.Dst=NoVal`，codegen 报 "builtin with-cap: no result slot"。
     新增 `KDot` 分支：通过 `fieldTypeOf(recvV, fieldName)` 从 `StructFields` 查找字段类型
     并设为 `typeHint`。新增 `fieldTypeOf` 方法（`structKeyOf` 逻辑的 lowerer 端实现，
     支持 `?T` 剥离 + suffix match）。闭环了 `test-net-http.no`（通过 tls.no 的 `conn.init`
     引入的 `.hs-buf = with-cap(65536)` 错误）。CERR 从 62 降至 55。
  3. （前 session 已落地）**#44–#50**，详见 MIR_DESIGN.md §12。
- **验证**：`test-net-http.no` 的 `with-cap` 错误已消除（CERR 从 62 降至 55）；
  `tests/1.no`、`test-f64-str.no`、`test-str-ops.no`、`test-open-read.no`、`test-ok-shadow.no`、
  `test-min-repro.no`、`test-guard.no` 在 MIR=0/3 下均逐字节一致，无回归。
  `test-guard.no` 在 MIR=3 下成功输出而 legacy 有 opt 验证失败，证明 MIR 路径比 legacy 更正确。
- ⚠️ 覆盖率数字随 `tests/` 文件数漂移（抽样锚定 glob 顺序+STRIDE），跨次比较须同一 checkout。
- ⚠️ **构建环境注意（本 session 实测）**：`/opt/homebrew/opt/llvm/bin/clang` 的**链接阶段挂起**
  （`clang t.o -o t` SIGTERM），导致 `./no build`/`run` 卡死；`/usr/bin/clang`（Apple 系统 clang）链接
  正常。验证 MIR 构建/运行须 `export PATH="/usr/bin:/opt/homebrew/opt/llvm/bin:$PATH"`（让 opt/llc 走
  homebrew、clang 走系统），否则构建命令会被 SIGTERM 杀掉。
