---
name: nolang-vet
description: 强制验证规则。每次修改完代码后，必须运行 `no vet src/std` 检查标准库，不允许出现 ERROR。适用于所有对 src/std/ 下 .no 文件或编译器的修改。其他项目的标准库目录可能不同，需根据实际项目结构调整 vet 路径。
---

# Nolang Vet — 修改后强制验证

## 核心原则

**每次修改完代码后，必须运行 `no vet` 检查标准库，不允许出现 ERROR。**

这条规则是强制性的，不可跳过。无论修改的是标准库 `.no` 文件、编译器 Go 源码、还是 LSP 代码，只要修改可能影响标准库的编译或诊断结果，就必须执行验证。

## 验证步骤

### 1. 确保编译器是最新版本

如果修改了编译器源码（`src/` 下的 Go 文件），先重新构建：

```bash
make no          # 重新构建 bin/no
```

### 2. 运行 `no vet` 检查标准库

```bash
./bin/no vet src/std
```

**不允许出现 ERROR。** 如果有 ERROR，必须修复后才能继续 —— **唯一例外**是下文
〈已知的既有 ERROR〉列出的那 1 个基线错误，它在 pristine HEAD 上同样存在。

### 3. （可选）运行 LSP vet 检查

`no vet` 只检查编译时错误（parse + type）。LSP vet 还会检查诊断级错误（未定义变量、命名规范、未使用变量等）：

```bash
make lsp                                         # 重新构建 LSP
./vscode-nolang/server/lsp vet src/std           # 完整 LSP 诊断检查
```

检查输出中的 `[ERROR]` 行：

```bash
./vscode-nolang/server/lsp vet src/std 2>&1 | grep '\[ERROR\]'
```

> **关键区别**：`no vet` 检查编译时错误（parse + type），`nolang-lsp vet` 检查诊断级错误（未定义变量、命名、未使用、重复等）。两者都应运行 — 先 `no vet`，再 `nolang-lsp vet`。

## 不同项目的路径差异

> **注意**：其他项目的标准库目录可能不同。

本项目的标准库位于 `src/std/`，但其他 Nolang 项目的标准库可能在不同的路径下。使用前应先确认标准库的实际位置：

```bash
# 查看项目结构，确定标准库目录
ls -d */std/              # 常见位置：src/std/、std/、lib/std/

# 根据实际路径调整 vet 命令
./bin/no vet <实际标准库路径>
```

如果项目使用了 `package.jsonc` 自定义包结构，标准库可能在 `pkg/std/` 或其他自定义路径下。始终根据项目实际结构调整 vet 路径。

## 何时执行验证

以下场景**必须**执行验证：

| 场景 | 原因 |
|------|------|
| 修改了 `src/std/` 下的 `.no` 文件 | 直接影响标准库内容 |
| 修改了编译器 Go 源码（parser/fmt/build/lexer 等） | 可能影响标准库的编译结果 |
| 修改了 LSP 源码 | 可能影响标准库的诊断结果 |
| 修改了标准库的类型定义或函数签名 | 可能引入类型错误 |
| 新增了标准库函数或模块 | 需要验证新代码的正确性 |
| 修改了 `stdsig_gen.go` 相关逻辑 | 影响标准库签名生成 |

## ERROR 处理流程

如果 `no vet src/std` 报告 ERROR：

1. **不要忽略** — ERROR 是阻断性的，不允许跳过
2. **定位错误** — 根据错误信息找到对应的文件和行号
3. **修复错误** — 修正语法、类型或语义问题
4. **重新验证** — 再次运行 `no vet src/std` 确认 ERROR 已消除
5. **如有必要，运行 LSP vet** — 确认诊断级错误也已消除

## 已知的既有 ERROR（基线，不要当作本次改动的回归）

本项目当前**存在 1 个既有 ERROR**，在 pristine `git archive HEAD` 构建上一模一样地出现，
不是任何人的改动引入的：

```
src/std/crypto/sha3.no: [ERROR] nolang-compile: parser errors:
  [sha3.no:line 24, column 23: [E_GENERAL] expected comma or right parenthesis, got ASSIGN(=) instead
   sha3.no:line 24, column 18: [E_GENERAL] expected expression after operator '*']
```

判定标准：**这个 ERROR 是基线，不是回归。** 只要 ERROR 集合没有比你改动前**增加**，就算通过。
正确的做法是先看内容，而不只是数行数：

```bash
./bin/no vet src/std 2>&1 | grep '\[ERROR\]'
```

> ⚠️ **不要只数行数。** `grep -c '\[ERROR\]'` 会把**标识符名里含 ERROR 的 HINT** 一起数进来 ——
> 例如 `number.no` 的 `LEVEL-ERROR`、`byte.no` 的 `DNS-RCODE-FORMAT-ERROR` /
> `DNS-RCODE-NAME-ERROR`。本例总共 4 行匹配，其中只有 `sha3.no` 那 **1** 行是真正的错误，
> 另外 3 行是 `[HINT]`。

**不要为了让数字变成 0 而去改 `sha3.no`** —— 那是既有问题，改它属于越界，而且会掩盖真正的
回归信号（下次真出回归时，你会以为"反正本来就有 ERROR"）。要判断新出现的 ERROR 是不是自己引入的，
用 pristine HEAD 二进制跑同一条命令对比：

```bash
mkdir -p /tmp/pristine && git archive HEAD | tar -x -C /tmp/pristine
cd /tmp/pristine && make no
/tmp/pristine/bin/no vet src/std 2>&1 | grep '\[ERROR\]'
```

## 与其他 Skill 的关系

- [nolang-build](file://../nolang-build/SKILL.md) — 构建流程中的 Post-Build Verification 已包含 `no vet`，本 skill 将其提升为强制规则
- [nolang-debug](file://../nolang-debug/SKILL.md) — 调试流程中的第 5 步也包含 `no vet` 验证
- [nolang-std](file://../nolang-std/SKILL.md) — 修改标准库 API 时参考，修改后必须验证
- [nolang-syntax](file://../nolang-syntax/SKILL.md) — 语法参考，修改 `.no` 文件后必须验证
- [nolang-memory](file://../nolang-memory/SKILL.md) — 内存相关修改后必须验证

## See Also — Nolang References

- [nolang-syntax](file://../nolang-syntax/SKILL.md) — Nolang syntax, grammar, types, operators, and language features
- [nolang-std](file://../nolang-std/SKILL.md) — Standard library API reference (60+ modules)
- [nolang-build](file://../nolang-build/SKILL.md) — Building the Nolang project with `make`
- [nolang-debug](file://../nolang-debug/SKILL.md) — Debugging guide for compiler and LSP issues
- [nolang-memory](file://../nolang-memory/SKILL.md) — Memory design and ownership model
