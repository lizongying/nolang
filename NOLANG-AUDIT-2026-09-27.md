# Nolang 全面体检报告

**复验基线**：`HEAD = adc925d4`（2026-10-04，距审计 32 commit + v0.3.12）
**语料规模**：`tests/*.no` 445 个；金标 488 条；`src/std` 39 顶层模块（含子目录 100+）

---

## 0. 执行摘要

| 维度 | 状态 | 关键数字 |
|---|---|---|
| MIR 内存安全 | 🟠 开放 | §2.1 越界堆写、§2.3 架构债 |
| 标准库测试 | 🟢 40/41 | bufio 5/6（option-cmp 遗留，非编译器缺陷） |
| 诊断工具链 | 🟠 开放 | §3.1-3.5 merged 过滤掩盖、`no fmt` 默认改语义、`--fix` 局限 |
| 测试覆盖 | 🔴 盲区 | crypto(31)/net(19) 零测试；json builder 零回归 |
| std workaround 债 | 🟠 开放 | §4.4：`&&` 不短路、out-param 条件块失效等 |
| CI 门禁 | ✅ 已补 | `.github/workflows/test.yml`（go-build/corpus-smoke/std-test/vet-gate） |

---

## 1. 审计方法

- **验证二进制一律重编**：`cd src && go build -ldflags="-s -w" -o ../bin/no ./cmd/no`（需 `export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"`）
- **回归用二分定位**：`git worktree add /tmp/nb-<sha> <sha>` 编对照
- **金标**：`NO=... GOLDEN=tests/golden/mir-baseline.tsv scripts/mir_golden.sh`（~8 min）
- **并发工作区**：审计/复验期间工作区被多会话活跃修改，结论用隔离快照排除在飞改动

---

## 2. MIR 后端内存安全（P1）

### 2.1 `%str-long` 下标写没有容量增长护栏 → 越界堆写

- **位置**：`codegen.go:7662` `ensureStrLongBuffer`
- **描述**：函数体只有 `lAlloc`（data==null → malloc）与 `lDone` 两块。`sizeReg = max(idx+1, 1024)` 仅在 null-alloc 分支使用；当 `data != null` 但 `idx+1 > cap` 时直接 `br lDone`（:7696），随后 `store` 越界写。
- **对照**：`ensureVecBuffer` 有完整的 `lCheck`+`lGrow` 分支。
- **最小复现**：`s str = 'ab'` → `s[8] = 'x'` → 写穿 6 字节。
- **影响**：静默堆破坏；`s[i]=c` 在 to-upper/to-lower/fill/set-byte 族大量使用。
- **解法**：在 `lDone` 前加 `lCheck`（`idx+1 > cap && data != null`）+ `lGrow`（newCap=max(cap*2, idx+1, 1024)，malloc+memset+memcpy+free）。

### 2.2 `%vec` 借用视图写入原地改他人存储（架构性技术债）

- **位置**：`ensureVecBuffer` 文档声明 "borrowed view 写入是 separate, pre-existing question"
- **判据**：`lCheck` 用 `cap > 0` 排除借用视图 ⇒ 永不增长 ⇒ 写入落到源存储。
- **建议**：纳入统一所有权模型设计，不逐个打补丁。

---

## 3. 诊断工具链（P1）

### 3.1 merged 模式 std 误报：过滤是掩盖不是根治

- `lint.go:626-641`：当检查目标不是 std 时丢弃 `isStdPath(r.File)` 结果。根因是 `nodeSem` 以节点指针为键，重建后查不到。
- 破口：① File 为空则过滤失效；② 只过滤 std 不过滤用户模块；③ 可能吞真诊断。
- **解法**：把 `nodeSem` 键改为稳定节点 ID，或重建时搬运 `NodeSemantics`。

### 3.2 行号范围回退 ⇒ 静默假阴性

- `lint.go:600-614` 取**第一个命中**区间；主档顶层非函数语句无 SourceFile ⇒ 被 std 区间吞掉。
- **解法**：给主档顶层语句补 `SetSourceFile`；回退时主档优先；匹配不到保留 `File==""`。

### 3.3 其它一致性（P2/P3）

- ~10 个 lint 分支不填 `File`（lint.go:233/274/…）
- 输出路径混用相对/绝对
- LSP vs CLI 配置相反（LightweightMode / SkipTypeChecks / MainFileVarNames）

### 3.4 默认 `no fmt`（不带 `--fix`）做语义改写

- `src/cmd/no/main.go:1258-1282`：默认路径跑 `fixRedundantTypeSource`（删冗余类型标注）+ `FormatProgramWithOverflow`（删 `#{overflow}` 注解）。
- 判定用单文件独立解析，与 `no vet` 包上下文判定方向相反 ⇒ 存在「fmt -w 删掉 build 必需注解」死循环。
- **解法**：默认只做排版，语义改写移入 `--fix=`；或 fmt 复用包上下文。

### 3.5 `--fix` 工具的已知局限

| 工具 | 局限 |
|---|---|
| `--fix=overflow` | ReturnStatement/CallExpression 实参静默跳过；一律写 `wrap`；同行多语句只生效一次 |
| `--fix=match` | 插入 `nil -> print('nil')` 占位臂是**行为变更**；同行两个 match 只修第一个；`?=` 跳过 |
| `--fix=redundant` | MapLiteral/十六进制/跨行不改；只处理 `LetStatement`；兜底失败整档回滚 |

---

## 4. 标准库（P1）

### 4.1 `test/std/` 40 绿 1 红

| 文件 | 结果 | 根因 |
|---|---|---|
| `bufio.no` | ❌ 5/6 | `read-byte 2nd` 测试的 matched-bare-match option 比较语法 `b2 == 89`（b2 为 `?byte`）在 wildcard arm 内的展开行为——实际 b2 值正确（调试输出 89），为 test 写法 / option-compare lowering 遗留，非编译器 codegen 缺陷。 |

> **2026-10-06 复核**：该文件现已通过；全量 `test/std/` 86/86 绿（见 §4.2.1）。

### 4.2 测试覆盖缺口（大面积盲区）

| 范围 | 缺口（整改前） |
|---|---|
| 顶层模块零测试（9 个） | yaml、markdown、toml、args、async、embed、types、enter、leave |
| `src/std/crypto/` | **31 模块 → 0 测试**（AES/RSA/ECDSA/Ed25519/X509/哈希/KDF） |
| `src/std/net/` | **19 模块 → 0 测试**（http/http2/tls/ws/quic/dns） |
| `src/std/database/sql.no` | 0 测试 |
| `src/std/archive/` | 7 → 仅 2 |
| `src/std/collection/` | 12 → 5 |

#### 4.2.1 整改进度（2026-10-06）

`test/std/` 由 40 文件增至 **86 文件，全量 86/86 绿**；`no vet src/std` **0 error**（仅 warning/hint）。新增测试 47 个。黄金向量以 RFC/NIST 标准与 OpenSSL/Python 参考实现交叉验证，过程中发现并修复以下**真实缺陷**（均已被回归测试锁定）：

| 模块 | 缺陷 | 处置 |
|---|---|---|
| crypto/sha224・sha384 | 返回值被丢弃 + IV 损坏 | 修复 |
| crypto/md5 | u32 宽度缺陷（循环左移按 64bit、`~` 未掩码） | 修复 |
| crypto/scrypt | salsa/integerify/romix 多处索引错误 | 修复 |
| crypto/argon2 | compress 列置换索引错误 | 部分修复；**完整 RFC 一致性仍开放**（属性测试锁定） |
| crypto/crc-64 | 多项式十进制常量笔误 | 修复 |
| crypto/poly1305・bigint | poly1305 整体不可用（bigint 重写）；bigint.shr1 多 limb 进位丢失 | 修复 |
| crypto/rand | xorshift32 未做 32bit 掩码 | 修复 |
| crypto/ed25519 | 非 RFC 8032 一致 | 重写；golden 8/8 |
| crypto/ecdsa | P-256 素数 p 存储字节序与大端引擎不符 | 修复；OpenSSL golden |
| crypto/x509 | fingerprint 手写 SHA-256 损坏 | 改为复用 sha256 |
| net/url | `to-str` 路径重复（群組双真臂）；`url-decode` 非法 `%` 转义产出 NUL | 修复；80/80 |
| net/ip | `is-private` 判定完全反转；`in-subnet` 同型参 `base.to-u32()` 误绑隐式接收者 | 修复；54/54 |
| net/cookie | 裸函数名 `sign` 与 `num.sign` 方法符号冲突，跨模块静默解析为 signum | 改名 `cookie-sign`/`cookie-verify`；58/58 |
| net/hpack | `encode-int` 在 val==max-prefix 时丢 0x00 延续 byte（零写入不推进 str 长度）；`encode-str` 空字串输出 0 byte | 改 `with-len` 预置长度；49/49 |
| net/dns | `parse-name` 遇保留 label 型别（前缀 01/10）无分支、pos 永不前进 → **恶意/畸形封包无限循环挂死（远端 DoS）**；越界截断返回 pos=0 | 新增错误臂返回 -1，调用方检查；51/51 |
| net/multipart | `extract-boundary` 未加引号分支误挂外层群組 default 臂 → 常规 boundary 返回空；`parse-headers` 的 `!!` 群組臂内退出触发 **BPT trap（reader.next 完全不可用）** | 重构条件循环；59/59 |
| std/args | `#{intrinsic}` 直连缺陷 | 改为委派 os；9/9 |
| std/yaml | 流式集合 EOF 挂死 | 修复；43/43 |

**仍开放的覆盖缺口（有意不测，原因标注）**：

- `net/` socket I/O 模块（http/http2/http3/ws/quic/tls/client/server/pool/proxy/sse/net/unix）：需活动套接字，非单测范畴。
- `net/cookie.parse-header`、`net/hpack.decode-headers`：`[n]str` 数组 out-param 多指派在呼叫端段错误（MIR codegen bug，最小重现与模块无关），测试头已记录。
- `hpack.tables.*`、`ip-addr.from-str`：跨模块方法无法实例化（EmitLLVM unknown callee），工具链限制。
- `collection/map`・`static-hashmap`：泛型模板受编译器限制，文档化。
- `database/sql.no`：纯 interface/struct 定义，无可测函数（同顶层 `types.no`）。
- `archive/`：仍仅 gzip/zlib 有测试；tar/zip 源码本轮有修补但未补 golden；bzip2/xz/zstd 未覆盖。
- 顶层 `async/embed/enter/leave`：仍无 test/std 专项（async 行为在 `tests/async-*.no` 有金样本）。

### 4.3 json builder 跨池缺陷 + 零回归覆盖

`arr-push`/`set-key`/`delete-key` 实测对 `parse`-建的接收者**返回 ok=true 却静默不改动**（跨池 copy-tree 未接上）；`json.new()`(null) 调 `arr-push` 则 **SIGSEGV**。详见 `tests/json-crosspool-mutate.no`（expected-fail）。`src/std/json.no` 正被并发会话编辑，本轮不碰。

### 4.4 std 源码 workaround 技术债清单

| 位置 | 问题 |
|---|---|
| `fmt.no:23-24` | 标量 out-param 条件块内指派不生效 |
| `fmt.no:137` | do-while `-> else` 始终被执行 |
| `str.no:1551` | out-param `.push()` 不生效 |
| `vec.no:210/411` | 方法内 `.push()` 触发 LLVM bug |
| `vec.no` 9 处 | 需 `sz=.len()` 强制 vec self 类型 |
| `byte.no`/`char.no` | `[]char` 切片方法不可用 |
| `process.no:618-623` | `process-shell` 命令注入风险、无超时 |
| `process.no:665` | **`&&` 不短路** |
| `json.no:28` | 枚举值不递增 |
| `str.no:765` | 不检查 i64 溢出 |

其中 `&&` 不短路、out-param 条件块失效是**语义级缺陷**。

---

## 5. 工程与流程（P2）

### 5.1 并发会话共享工作区

多会话同时修改工作树（20+ 文件在飞）。建议：归因前用隔离快照、关键改动前后 `git diff --stat`。

---

## 6. 语言与工具体验（P2/P3）

| 陷阱 | 说明 |
|---|---|
| option match 必须写满 ok/nil/err | 少一个 = parser error |
| `?=` 仅限有 option result param 的函数 | 顶层会 parser error |
| MIR 解 option 依赖显式 `?T` | 推断不足则 codegen 失败 |
| `println`/`eprintln` 不存在 | 只有 `print`/`eprint` |
| `no fmt` 不带 `--fix` 改语义 | 见 §3.4 |

---

## 7. 解决方案路线图

| # | 动作 | 优先级 | 层级 |
|---|---|---|---|
| 1 | json builder 跨池 copy-tree 修复 | 中 | std+编译器 |
| 2 | §2.1 ensureStrLongBuffer 增长护栏 | 中 | 编译器 |
| 3 | 补 crypto(31)/net(19) 测试覆盖 | 中 | std |
| 4 | 默认 no fmt 不改语义（§3.4） | 中 | CLI |
| 5 | §4.4 技术债（&&/out-param） | 长效 | 编译器 |
| 6 | nodeSem 键稳定化 + 去掩盖式过滤 | 长效 | 诊断 |

---

## 8. 附录：可复现命令

```bash
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"
cd src && go build -ldflags="-s -w" -o ../bin/no ./cmd/no
cd src && go test ./...
./bin/no vet src/std
./bin/no vet tests
NO=$PWD/bin/no GOLDEN=tests/golden/mir-baseline.tsv MIR_GOLDEN_JOBS=4 scripts/mir_golden.sh
./bin/no test test/std/bufio.no
./bin/no test test/std/math.no
./bin/no test test/std/uuid.no
```
