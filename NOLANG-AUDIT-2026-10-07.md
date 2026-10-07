# Nolang Builtin 链接库依赖审计

**基线**：`HEAD = 0fade5ce`（2026-10-07）
**审计问题**：builtin 注册表里，是否还有「能用 Nolang 自身实现、却仍走链接库」的条目？
**结论**：**数学类已清干净，无残留库依赖。** 审计过程中发现并修掉 3 个 bug（其中 1 个是 libm 移除重构引入的**静默错误答案**）。

---

## 0. 执行摘要

| 维度 | 状态 | 关键数字 |
|---|---|---|
| libm 依赖 | ✅ 已清除 | `src/build` 里 `-lm` 出现 **0** 次；唯一 `LLVMIntrinsic` 是 `llvm.sqrt.f64`（硬件 `fsqrt`） |
| builtin → C 库 | ✅ 均为真 OS API | 46 条 `CLibCall` / 44 个不同符号，全是 POSIX/Win32 系统调用，**无法**用 `.no` 自实现 |
| std 自身声明库 | ✅ 零 | `src/std` 的 `link-libs` **0** 处；`#{c}` FFI **0** 处（唯一命中在注释里） |
| 纯 .no 覆盖 | ✅ | 超越函数 / `pow` / `hypot` / `atan2` / `fmod` / `cbrt` 全部 `.no` 实现 |
| 过程中修的 bug | ✅ 3 个 | 见 §3；其中 1 个为**静默错误答案**（`rc=0`、无诊断） |

---

## 1. 方法

- 所有验证二进制重编：`cd src && go build -o /tmp/no_x ./cmd/no`（需 `export PATH="/opt/homebrew/bin:/opt/homebrew/opt/llvm/bin:/Library/Developer/CommandLineTools/usr/bin:/usr/bin:$PATH"`）。
- 改 `src/std/**/*.no` 后**必须**重生签名表：`touch src/std_embed.go && cd src && go run ./checker/genstdsig`。
  ⚠️ **不要**为这件事跑 `make no` —— 它会先跑 `stamp-traceid`，用 perl 改写 `src/checker/checker.go` 等 5 个文件（`TRACE_ID_FILES`），在**并行会话**在场时会污染别人的改动。
- A/B 只比 `(rc, stdout_hash)`，**永不比 stderr**（理由见 §4.2）。

---

## 2. Builtin 注册表普查

`src/builtin/*.go` 共 **179** 条 `BuiltinMethodList = append`：

| 文件 | 条数 | | 文件 | 条数 |
|---|---|---|---|---|
| `os.go` | 91 | | `process.go` | 20 |
| `net.go` | 15 | | `str.go` | 13 |
| `vec.go` | 13 | | `bits.go` | 9 |
| `fmt.go` | 6 | | `strconv.go` | 6 |
| `async.go` | 3 | | `array.go` / `math.go` / `math_f64.go` | 各 1 |

**降级方式分布**：`ForwardFunc` 134、`CLibCall` 46、`LLVMConv` 4、`Intercepted` 2、`LLVMIntrinsic` 1。

### 2.1 `CLibCall` 的 44 个符号全是系统 API

```
chdir chmod chown chroot close dup2 exit flock getcwd getegid getenv geteuid getgid
gethostid gethostname getpid getppid getuid kill link mkdir mkfifo mknod open read
rename rmdir setenv setpriority setsid signal symlink sysconf system truncate unlink write
+ nolang.now_{s,ms,us,ns} / nolang.sleep_{s,ms,us,ns}   ← 编译器自带的运行期辅助
```

**与 libm 的交集 = 0**（比对集合含 sin/cos/tan/asin/acos/atan/atan2/sinh/cosh/tanh/exp/exp2/exp10/log/log2/log10/pow/sqrt/cbrt/hypot/floor/ceil/round/trunc/fabs/fmod/remainder/copysign/frexp/ldexp/modf/lgamma/tgamma/erf/erfc/rint/nearbyint/scalbn/ilogb/log1p/expm1/asinh/acosh/atanh）。
`src/builtin/` 下对 libm 符号名的字面量搜索同样**零命中**。

### 2.2 曾经是 libm、现已纯 `.no`

`src/builtin/strconv.go` 的注释即为证据链：`atoi`/`strtoull`（`str.to-i64`/`to-u64`）、`strtod`（`str.to-f64`）、`sprintf`（整数 `.to-str` 与 `f64.to-str`）**全部移除**，改由 `src/std/str.no` / `src/std/number.no` 实现。
`src/builtin/math_f64.go` 现在只剩一条注册：`f64.sqrt` → `llvm.sqrt.f64`。

---

## 3. 本次修掉的 3 个 bug

### 3.1 🔴 `# std/xxx` 显式导入路径遮蔽 `#{buildin}` 桩 ⇒ **静默错误答案**

**根因**：`#{buildin}` 空体不是「没用的函数」，而是「Go 内建表实现了它」的**承诺**。三条模块加载路径中，前两条有跳过逻辑，第三条漏了：

| 路径 | 位置 | 守卫 |
|---|---|---|
| std 自动加载 | `checker.loadStdModuleBodyForce` | ✅ 跳过 |
| `stripBuiltinStubs` | `build/transpiler.go` | ✅ 剥离 |
| **显式 `# std/<mod>` 导入** | `processUseAndMerge`（`build/transpiler.go`） | ❌ **原先缺失** |

**后果**：空体被 codegen 成读未初始化 alloca 的真实符号，调用点改走该符号而非 Go 内建表。实测（`tests/explicit-std-import-builtin.no`）：

| | 修复前 | 修复后 |
|---|---|---|
| 通过断言 | 4 / 10 | **10 / 10** |

失败项：`f64.sqrt`（回堆栈垃圾，连带 `asin`/`acos`/`cbrt`）、`math.clamp`（回 0）、`os.get-wd()`（回 `''`）。
**没有显式导入时走自动加载路径，所以这些程序「看起来」是好的** —— 这也是金标 sweep 照不到的原因。

**修复**：`if fd.BuiltinStub { merged.BuiltinFuncNames[fd.Name] = true; continue }`。

**影响面（静态可判，且决定性）**：`processUseAndMerge` 只在**显式 `# ...` 导入**时执行。语料 97 个含 use 的 `.no` 中只有 **9** 个写 `# std/...`（8 个既有 + 1 个新回归测试）；那 8 个只导入 `sort`/`database/sql`/`heap`/`net/http`/`set`/`crypto/sha3`，**皆无桩**（传递闭包亦然）。⇒ **唯一受影响的档就是新测试本身。**

### 3.2 🔴 已死的 libm lowering 是「拆掉重装」地雷

`src/mir/codegen.go` 的 `removedMathIntrinsic` 表仍在，`emitBuiltin` 曾用它把 LLVM 21 已移除的 17 个超越 intrinsic 降回同名 libm 函数（`declare double @sin(...)`）——等于把整个 libm 移除重构**还原**，并让交叉编译重新需要 `-lm`。
**修复**：改为硬失败 `c.fail`，并加 `src/mir/removed_math_intrinsic_test.go`（两个测试，其中一个已用「把 `llvm.sqrt.f64` 换成 `llvm.sin.f64`」证伪过，确认会坏）。

### 3.3 🔴 `math.pow` 负底数符号（libm 移除重构引入的回归）

`1756511f` 把 `math.pow` 从 `#{buildin}` → libm `pow`（C 语义）改成纯 `.no` `exp(y*ln|x|)`。**`exp` 恒为正，符号无从还原**：

| 表达式 | C pow | 修复前 | 修复后 |
|---|---|---|---|
| `pow(-2.0, 3.0)` | -8.0 | **8.0** ❌ | -8.0 ✅ |
| `pow(-2.0, -3.0)` | -0.125 | **0.125** ❌ | -0.125 ✅ |
| `pow(-2.0, 0.5)` | NaN | **1.414…** ❌ | NaN ✅ |
| `pow(-2.0, 2.0)` | 4.0 | 4.0 ✅ | 4.0 ✅ |

**确认是回归**：`git show 2ec8f2ce:src/std/math.no` 显示修复前是 `#{buildin}` 桩（libm `pow`）。
**修复**（`src/std/math.no`）：`x < 0.0` 分支先 `y.trunc()`，非整数 ⇒ NaN；整数则 `f64-to-i64(yi) & 1` 判奇偶决定符号。
**已知限制**（已写入代码注释）：`|y| > 2^63` 时 `f64-to-i64` 溢出致奇偶失真（仅影响 inf 的符号）；`x==0.0 且 y<0` 回 0.0 而非 C 的 +inf（沿旧行为）。
**影响面**：`math.pow` **全语料唯一调用点就是 `test/std/math.no`**（其余 `pow` 全是 `bigint.pow` / `number.pow`(整数) / `pow-int`；`src/std/number.no:154` 明言 cbrt 不调 pow）。

---

## 4. 三个量测陷阱（都先给出过**假通过**）

### 4.1 🔴 扫描脚本内必须自己 `export PATH`

第一版 A/B 脚本没设 PATH ⇒ `no run` 找不到 LLVM 工具链 ⇒ **97 个档全部 `rc=1`、stdout 为空**（`e3b0c44298fc…` 就是空字符串的 sha256）⇒ 两边「完全一致」= **假通过**。

**判准**：扫描脚本结尾**必印「控制组 `rc=0` 的档数」**；≈0 ⇒ 全体 FAIL，此时「0 differ」毫无意义。

### 4.2 🔴 stderr 不可用于 A/B：里面有 build-time 版本戳

`Makefile:7` 用 `-ldflags="-X main.version=$(GIT_COMMIT)"`，而裸 `go build` 停在默认 `"dev"`。于是两支 binary 对**每个**档都吐不同的行：

```
warning: compiler version mismatch: package.jsonc requires "0.1.0", current compiler is "dev"      ← 裸 go build
warning: compiler version mismatch: package.jsonc requires "0.1.0", current compiler is "d546c4c3" ← make no
```

实测造成 **47/47 假 DIFF，而 stdout 逐字节相同**。**常数型 stderr 差异 = 假差异。**
⇒ **A/B 只比 `(rc, stdout_hash)`**；要比 stderr 就得两支 binary 走**同一条**建置路径，或先 `grep -v 'compiler version mismatch'`。

### 4.3 🔴 `rc` 不足以判「行为有无改变」

Nolang 测试 runner **断言失败仍 `rc=0`**。§3.1 的 bug 就是同一档 4/10 vs 10/10 而两边 `rc` 都是 0。⇒ **必须 hash stdout**。

### 4.4 ⚠️ 统计 `#{buildin}` 前必须先剥 `;` 行注释

`src/std/database/sql.no:19` 的注释里含 `#{buildin}` 字样。不剥注释会把 `database/sql` 误判为带桩模块，模块数从正确的 **15** 虚增到 16。剥注释后：**153 个桩 / 15 个模块**（os 49、fs 34、process 19、net/net 12、math 9、vec 8、global 6、time 5、fmt 3、async 2、net/unix 2、str/option/bool/number 各 1）。

---

## 5. 验证矩阵

| 检查 | 结果 |
|---|---|
| `go test ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/` | ✅ 全绿 |
| `go test ./...` 失败集合 | 仅 `build.TestVetImportedModuleDiagnosticsNotAttributedToMainFile`（**既有**，见下） |
| `test/` + `test/std/`（88 档，pow 修正 A/B） | BEFORE `passed=1862 failed=3` → AFTER `passed=1865 failed=0`；**stdout 只有 1 个档变**（`test/std/math.no`，75/3 → 78/0） |
| 显式 std 导入档 A/B（97 档） | `(rc, stdout)` **96/97 相同**，唯一差异即 §3.1 的回归测试（预期） |
| `no vet src/std` 全量诊断 | 控制组 vs 修复后 **17 676 行逐字节相同** |
| 超越函数密集探针（20 项） | ✅ 20/20 |
| **产出 IR 的 declare 清单** | **零 libm**：`clock_gettime/gettimeofday/nanosleep/usleep` + `memcmp/strlen/write` + `malloc/free` + `llvm.memcpy/llvm.memmove` |
| 金标 sweep（527 档，冻结 `tests/golden/mir-baseline.tsv`） | SAME=468；11 DIVERGE / 2 REGRESS **全部 A/B 证实为既有行为** |

**既有失败的确认为何可信**：用 `git worktree add /tmp/headtree HEAD` 在**纯 HEAD** 上跑 `go test ./build/`，**两个测试都失败**；当前工作区只剩 1 个失败（`TestMonomorphPreservesOverflowAnnotation` 已被并行会话的 checker 改动修好）⇒ 剩余那个与本审计无关。

---

## 6. 未决事项

- **`checker/stdsig_gen.go` 的 key 会因任何 `src/std/**/*.no` 改动（含注释）失效**：`computeStdSigKeyFromFS` 对每模块写入 `cache.ContentKey(source)`（内容哈希）。`checker.go` 执行期比对，不符则 fallback 到 full collection —— **正确但慢**，不会编错。本次已用 `go run ./checker/genstdsig` 重生并验证幂等。
- **`make no` 的 `install` 步骤在本沙箱被安全策略挡住**（`make: *** [install] Error 71`），但二进制此时已建好，可直接使用。
- §3.3 列出的两个 `math.pow` 极端边界（`|y| > 2^63`、`x==0 && y<0`）未修，已在代码注释中标注。

---

## 7. 并行会话提示

审计期间工作区被**多会话活跃修改**。以下改动**不属于**本次审计：

- `src/checker/checker.go`（+31 行，`GroupedExpression` 的 `operandIntKind`/`isIntExpr` 处理）
- `src/std/number.no`（-12 行，移除无符号除法上冗余的 `#{overflow=wrap}`，是其 checker 改动的连带清理）

⇒ 工作区 `git status` 混杂两方改动，**未经确认不得提交**。
