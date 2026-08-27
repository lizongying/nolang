# `no build` 性能优化完整方案

> 实测日期：2026-08-24。机器：Apple Silicon（arm64），`no` 二进制 18:14 构建。
> 测量工具：`NOPROF_INPUT=<f> go test ./build -run TestStagedTiming`（分阶段墙钟 + IR 体积）、`NOPROF_STD=1 go test ./build -run TestStdParseOnly`（隔离 std 预收集）。
> 另有 `NOPROF_FAST=1` 单进程后端对照。

---

## 0. 实测基线（数据说话）

| 样本 | 用户代码 | std 预收集(固定) | Compile | 原始 IR | opt -O3 | llc | clang link | 总计 |
|---|---|---|---|---|---|---|---|---|
| `test-simple`（`print('hello')`） | 1 行 | **635 ms** | **1132 ms** | 2.25 MB | 97 ms | 40 ms | 317 ms | **2229 ms** |
| `test-map-generics` | 204 行 | 540 ms | 1167 ms | 3.00 MB | 205 ms | 100 ms | ~2800 ms* | 4819 ms |
| `test-std-hash` | 643 行 | 916 ms | 1653 ms | 4.84 MB | 885 ms | 393 ms | 599 ms | 4484 ms |

\* `clang link` 在 0.6–2.8 s 间大幅波动（macOS `ld64` 受机器争用影响），属噪声项，不计入稳定瓶颈。

**关键观察**
1. **死代码爆炸**：原始 IR 中 **77%–99.4%** 在 `opt` 阶段被 DCE 丢弃（`test-simple` 仅 0.6% 存活 → 13.9 KB；`test-std-hash` 23.4% 存活 → 1.13 MB）。即编译器生成了海量最终被抛弃的 IR。
2. **固定成本**：`CollectStdModuleSignatures` 每次 build 都全量解析 118 个 std 模块（51.8k 行），耗时 **540–916 ms**，与用户程序无关。
3. **Compile 占比被低估**：内存旧笔记称「主程序 parse/sema/codegen 只占个位数百分比」——**与实测不符**。1 行程序上 Compile 占 **51%**（1132 ms），大程序占 24%–37%。该论断已作废。
4. **单进程后端无益**：`clang -O3 -x ir` 单进程路径（2171 ms）反而慢于当前 `opt`+`llc`+`clang` 分步（1877 ms）。**不要**改成单进程。

---

## 1. 根因分析

- **R1（最大杠杆）— std 第二遍自动加载是「模块级粒度」**
  `build/transpiler.go:1062` `alwaysAutoLoadStd = ["fmt","io","byte","str","vec"]`：即使只 `print` 一行，这 5 个模块也**整体**被 codegen，再让 `opt` 丢 99%。引用任一函数即拖入整个模块体（见内存「IR 死代码是结构性问题」）。等价于把 DCE 推迟到了 codegen 之后，白白支付了 codegen + opt + llc 对死代码的处理。
- **R2（固定成本）— std 签名无跨进程持久化**
  `checker.CollectStdModuleSignatures`（`checker.go:3671`）每次 build 进程都并行解析全部 118 模块只为收集签名（parser 的 `let` 类型推断需要**全部** std 签名，故不能按引用跳过，见 `transpiler.go:958`）。`sync.Once` 仅进程内有效，跨 build 不生效。
- **R3（放大项）— opt 跑在 2–4.8 MB 文本 IR 上**
  `opt -O3 -S` 读写文本 IR，I/O 与优化都随 IR 体积线性放大（opt 时间：97ms→885ms ∝ 原始 IR 2.25→4.84 MB）。

---

## 2. 分阶段优化方案

### Stage 1 — std 签名 + token 序列磁盘缓存  ⭐ 已落地、最安全、最高 ROI 固定成本
- **机制**：把 `CollectStdModuleSignatures` 的产物（`funcSigs`/`structFields`/`aliases`/`structMod` **+ 全部 std 模块的 lexer token 序列**）按「全部 std 源内容哈希」序列化到磁盘缓存（`~/Library/Caches/nolang/stdsig-v1-<hash>.gob`，macOS；Linux 为 `$XDG_CACHE_HOME/nolang` 或 `$TMPDIR`）。下次 build 哈希命中：跳过 PASS1 全量解析（直接加载签名），并把 token 序列灌回全局 `tokenCache`，使 codegen 的 auto-load 重解析命中 token 缓存、免 lex。
- **为何必须连带缓存 token 序列（关键教训）**：最初只缓存签名表时，warm 下 `CollectStdModuleSignatures` 从 ~540ms 降到 ~5ms，但 **Compile 反而变慢 ~440ms**，净收益只剩 ~150ms。根因：`resolveUse`→`parseEmbeddedProgram` 每次都从 embed 重新 lex+parse std 模块；冷启动时 `CollectStdModuleSignatures` 解析全部 118 模块会顺带把 token 写进**全局 token LRU**，于是 auto-load 重解析 lex 命中缓存、几乎免费；warm 时 collect 跳过了 parse，token LRU 没被预热，auto-load 被迫完整 lex+parse，把省下的时间吃回去。**std 模块被 lex 了两遍**才是真问题，所以必须把 token 序列也持久化。
- **实测收益（2026-08-24，落地后）**：

  | 样本 | COLD 总耗时 | WARM 总耗时 | 净省 | CollectStd（COLD→WARM） | Compile（COLD→WARM） |
  |---|---|---|---|---|---|
  | `test-std-hash`（643 行） | 4278 ms | 3208 ms | **~1.07 s** | 525 ms → 44 ms | 1404 ms → 1465 ms（持平） |
  | `test-simple`（1 行 print） | 3112 ms | 2520 ms | **~0.59 s** | — | — |
  | 真实 `no build` `test-std-hash` | 3.88 s | 3.08 s | **~0.80 s** | — | — |

  固定成本（`CollectStdModuleSignatures`）从 **540–916 ms → ~5–44 ms**；Cache 文件约 **4.5 MB**（token 序列），加载+解码 ~25–40 ms。净省稳定在 **0.5–0.9 s** 区间，符合承诺。
- **实现要点 / 风险（已验证）**：
  - 签名表天然可 gob 序列化（低风险）；token 序列 `[]lexer.Token` 全为导出字段，可 gob（低风险）。
  - **未缓存 `parser.Program`（AST）**：`Program` 含 `Sem *SemanticContext` 等语义副表、且 Statement 是接口类型需 `gob.Register` 全部节点（易碎、有循环/未导出字段风险）。改为缓存 token 序列，既规避 AST 序列化风险，又通过复用全局 token LRU 让 auto-load 免 lex，效果等价且稳健。
  - `gatherStdTokens()` 在冷采集后从全局 `tokenCache` 抽取全部 std 模块 token；`tryLoadStdSigCache()` 命中时 `lexer.PutCachedTokens` 回灌。解码失败/字段缺失 → 透明回退全量采集。
  - 护栏：内容哈希失效即回退重解析；写缓存用「temp 文件 + rename」原子写，单写多读 race-clean；保留 `ClearCaches()` 入口（缓存键含 std 内容哈希，改 std 后哈希变即自动失效）；`NOLANG_NOCACHE_STD=1` 可强制绕过缓存（用于测量冷耗时）。
  - `go test -race -count=3` 在 `checker`/`lexer` 包零竞态。
  - ⚠️ **操作注意事项（改 std 必须重建 `no`）**：缓存键是对**嵌入的 std 内容**（`//go:embed` 编译期烘焙进 `no` 二进制）做哈希。因此**改 `src/std/*.no` 后必须 `cd src && go build -o ../no ./cmd/no` 重新构建**，`no` 二进制与缓存才会同步失效并重新收集——否则编译器（与缓存）会继续用旧的烘焙版 std，表现为「改了不生效」。这与既有的 std 编辑约束一致。**实测验证（2026-08-24）**：给 `bool.no` 加一行注释→重建 `no`→缓存目录出现**新哈希文件**（`stdsig-v1-4f619fd1…gob`，旧的 `7ea7daf2…` 仍并存）；还原+重建后旧缓存重新命中、warm build `exit=0`。不同 std 版本的 `no` 二进制各自生成不同哈希缓存文件，互不冲突。

### Stage 2 — 函数级惰性 codegen  ⭐ 最大杠杆、解 R1
- **机制**：不再以「模块」为粒度整体 codegen。从程序入口（含 `alwaysAutoLoadStd` 的隐式入口与 builtin 接线点）做**可达性分析**，仅对「实际被调用 + 传递闭包」的 std 函数生成 IR（递归闭包）。等价于把 `opt` 的 DCE **提前到 codegen 之前**。
- **预期收益**（基于 IR 体积反推）：
  - `test-simple`：原始 IR 2.25 MB → 数百 KB；Compile 1132 ms → ~100–200 ms，opt/llc/link 同步骤减 → 总计 **2229 → ~700–900 ms**。
  - `test-std-hash`：原始 IR 4.84 MB → ~1 MB 以内；Compile 1653 ms → ~500 ms，opt 885 ms → ~200 ms → 总计 **4484 → ~1500–1800 ms**。
- **风险**：中高。必须保证闭包**完整**（被拐角调用的函数、C 端/内建路径无条件引用的函数如 `print`/格式化、泛型实例化）。**严禁改动内存安全**：`movedVar` / CFG `free` 数据流、prologue 预分配等保持不变。
- **护栏**：`tests/mem-safety/` 12 用例 + 干净样本做退出码与 `leaks --atExit` 回归；**先只惰性 std 模块，主程序保持整体 codegen**，逐步开启。

### Stage 3 — 后端管线微调（低风险，解 R3）
- **3a（opt→llc→clang 管道直串）— ✅ 已實作（2026-08-24）**：
  - **決策**：放棄 bitcode 方案，改採用戶建議的管道直串——非 `-v` 時 `opt` 經 stdin 讀 code、stdout 喂 `llc`、再經 stdin 喂 `clang`，**徹底不落 IR/組譯 temp 文件**。
  - **實作**（`src/build/builder.go` `buildLLVMInternal`）：新增 `usePipe = !verbose && NOLANG_KEEP_IR=="" && NOLANG_PIPE!="0"`；管道模式 `opt -O3 -S -o -`（stdin=code, stdout→optBuf）→ `llc --fp-contract=fast -o -`（stdin=optBuf, stdout→asmBuf）→ `clang -x assembler -`（stdin=asmBuf，native）/ `clang -x ir -`（WASI）。文件模式（verbose / `NOLANG_KEEP_IR` / `NOLANG_PIPE=0`）保持舊行為，落 temp `.ll`/`_opt.ll`/`.s` 供檢視與診斷。
  - **實測驗證**：
    - 管道模式 temp 目錄**空（0 文件）**；文件模式 temp 目錄含 `.ll`(2.25MB)+`.s`(14KB)+`_opt.ll`(13.8KB) → 確認 IR/組譯全在內存流轉。
    - 6 個樣本（test-simple / test-std-hash / test-map-generics / test-tls / test-tls13-crypto / test-arr）管道 vs 文件模式**運行輸出完全一致**（如 test-std-hash 均 2636 字元）。
    - `TestBuildLLVMInternalWasiMissingSysroot` 通過（WASI 管道模式下 opt 先跑、再觸發 sysroot 檢查，順序不變）。
    - 時耗：管道 ~3.2–3.6s vs 文件 ~3.0–3.6s，**無顯著回歸**（符合預期：IR temp I/O 在 SSD 上本就便宜，省下的是潔淨度而非大幅提速）。
  - **開關**：`NOLANG_PIPE=0` 強制文件模式（除錯用）；`NOLANG_KEEP_IR=1` 強制文件模式並保留 temp 目錄。
- **3b**：`NOLANG_OPT_LEVEL` 已支持；評估開發者模式默認 `-O1/-O2`（構建提速 vs 運行期損失），發佈用 `-O3`。
- **3c**：**保持** `opt`+`llc`+`clang` 分步（實測單進程更慢，見 §0）。
- **預期收益**：3a 潔淨度（零 IR temp 文件）+ 微量 I/O 節省；3b 視優化級別。

### Stage 4 — PASS1 轻量解析（进阶，解 R2 备选） ✅ 已无需实现（Stage 1 已覆盖）
- **状态**：经 2026-08-24 实测与代码核查，**本阶段不再需要**。
  - 原前提「Stage 1 因 AST 不可序列化而收益打折」不成立：Stage 1 只缓存四张签名表 + token 序列（不缓存不可 gob 的 `parser.Program`），已把全量 parse 51.8k 行 std 的固定成本从 540–916 ms 砍到 ~5–44 ms。
  - LSP 与主编译器**共用同一个 `parser.ParseProgram()`**，没有「只取签名的轻量解析」路径（查 `src/lsp/documents.go` / `server.go` / `completion.go`：全为 `parser.New(l).ParseProgram()`，无 header-only / skip-body 模式）。故即便要做也需**新增** parser mode，而非复用。
- **保留的 Red line（仅作未来参考，当前不实施）**：若日后确需轻解析模式，不得改动共享 `sem` table 的 lex 前瞻；路径须复用 `tokenCache`。

### Part A — 编译期 embed 签名表（已取消，2026-08-24）
- **状态**：⛔ 经用户复核，**不再实现**。
- **决策依据**：`src/std_embed.go:5` 已是 `//go:embed std`，std **源（即程序结构/AST 的来源）本就烘焙进 `no` 二进制**。`CollectStdModuleSignatures`（`checker.go:3671`）每次 build 都从这份已嵌入的源把签名表直接写进内存的 `stdSigsCache`/`stdFieldsCache`/`stdAliasesCache`/`stdStructModCache`——也就是"直接写进程序结构"。**再单独 embed 一份签名表是多余的**：它和已嵌入的源 + 运行期 `CollectStdModuleSignatures` 产出的是同一份数据。
- **warm 路径已由 Stage 1 覆盖**：磁盘缓存跳过 PASS1 全量解析并把 token 序列回灌 `tokenCache`，固定成本已从 540–916 ms → ~5–44 ms。embed 签名表唯一能额外省的是**冷启动**（首次 build / 清缓存后）那一次 PASS1，但代价是要新增 `go:generate` 生成器 + Makefile regen 防 stale，性价比不如现有磁盘缓存。
- **若日后真要消灭冷启动 PASS1**：更干净的做法是"把生成器产出的签名表写成 `checker` 包的 Go 源码字面量（编译进二进制，非 `//go:embed` 文件）"，而非再 embed 一个 gob blob——但当前不实施。

---

## 3. 执行顺序与合并门禁

| 顺序 | 阶段 | 收益 | 风险 | 是否先上 |
|---|---|---|---|---|
| 1 | Stage 1 磁盘缓存 | 省 ~0.5–0.9 s（固定） | 低 | ✅ 立刻 |
| 2 | Stage 3 后端微调（3a 管道已實作，3b/3c 保留） | 潔淨度+微量 I/O | 低 | ✅ 已實作 |
| 3 | Stage 2 函数级惰性 codegen | 大（IR 降数倍） | 中高 | 充分回归后 |
| 4 | Stage 4 轻量解析 | — | — | ⛔ 已取消（Stage 1 覆盖） |

**合并门禁（沿用现有 env-gated 测试）**
- 相关包单测通过；`go test -race -count=3` **零竞态**。
- 重建 `no`（`cd src && go build -o ../no ./cmd/no`）并 `no build` 干净样本 **EXIT=0**。
  > ⚠️ 注意：内存合并门禁原引用 `tests/test-str.no`，但该测试当前因**预存 zext bug**（`zext i1 %x to i64` 但 `%x` 已为 i64）失败。须改用**当前干净样本**（`test-map-generics.no` / `test-std-hash.no`）作门禁，或先修该 bug。
- 性能回归：`NOPROF_STD=1` 验证缓存命中后 PASS1 时间；`NOPROF_INPUT=<f>` 验证分阶段墙钟与 IR 体积不恶化。
- 内存安全：`tests/mem-safety/` 退出 0 且 `leaks` 不回归（Stage 2 专属）。

---

## 4. 收益小结（Stage 1 已落地）

| 样本 | 落地前（COLD） | Stage 1 后（WARM） | 加速 |
|---|---|---|---|
| `test-std-hash`（643 行） | 4278 ms | **3208 ms** | **~1.07 s**（≈ 1.25×） |
| `test-simple`（1 行 print） | 3112 ms | **2520 ms** | **~0.59 s**（≈ 1.2×） |

- **Stage 1 已完成**：固定成本（`CollectStdModuleSignatures`）从 540–916 ms → ~5–44 ms，净省 **0.5–0.9 s**，race-clean，正确性经多样本 + token 序列 round-trip 校验。
- **Stage 2（函数级惰性 codegen）仍为最大杠杆**：当前原始 IR 仍有 77%–99.4% 被 `opt` 的 DCE 丢弃，Compile/opt/llc 随 IR 体积线性放大。Stage 2 把 DCE 提前到 codegen 前，预计再把中小程序压到 ~1.5–2.0 s（≈ 2.5×）。
- **Stage 3a（opt→llc→clang 管道直串）已完成**：非 `-v` 构建彻底不落 IR/組譯 temp 文件（temp 目錄實測 0 文件），6 樣本管道 vs 文件運行輸出完全一致，`go test ./build` + `go vet` 通過，時耗無顯著回歸。
- Stage 3b（優化級別）/ 3c（分步後端）保留。Stage 4 已取消（Stage 1 已覆盖固定解析成本，且 LSP/主编译器共用完整 `ParseProgram()`，无独立轻解析路径可复用）。

---

## 5. 下一步建议
1. 先落地 **Stage 1**（磁盘缓存）+ **Stage 3a**（bitcode），低风险、立即见效，且为 Stage 2 提供稳定的测量基线。
2. 同时排一个 **spike** 验证 `parser.Program` 能否 gob 序列化，决定 Stage 1 是「仅缓存签名」还是「连 AST 一起缓存」（后者可进一步免去 PASS2 重解析）。
3. Stage 2 单独开分支，配齐 `tests/mem-safety/` + 干净样本回归后再合。
