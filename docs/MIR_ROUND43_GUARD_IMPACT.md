# 第四十三轮：`loadVal` 静默-`undef` 护栏 —— 影响面报告

> 日期：2026-09-16 · 变更：`src/mir/codegen.go`（`loadVal`）· 基线：`tests/golden/mir-baseline.tsv`（已重冻结）
> 详设见 `docs/MIR_DESIGN.md` §12 #85、§13.3.17；本文只记录**影响面与证据**。
> 变更未提交。

## 1. 改了什么

`loadVal` 原来一个条件管两件事：

```go
if lt == "void" || slot == "" { return "void", "undef" }   // 旧
```

拆成两支，只让第二支响铃：

```go
if lt == "void" { return "void", "undef" }            // 合法：void/unit 无存储
if slot == ""   { c.fail(...); return lt, "undef" }   // 静默错误编译 -> 响铃
```

**拆分的依据是测量，不是猜测**（见 §2）：合在一起改会让 51 处合法命中全部误报。
`c.fail` 只累积、在 `EmitLLVM` 出口汇总，故**一次构建把函数内每处越界点全部列出**。

## 2. 为什么是"51 合法 / 4 真洞"，而不是"19 文件"

上一轮的诊断（`NOLANG_MIR_DEBUG_UNDEF`）统计出 19 文件 / 55 处命中。**那是一个混合计数**：

| 类别 | 条数 | `value=` | 判定 |
| --- | --- | --- | --- |
| `llvm="void"` | **51** | 真实值 id（173/708/604/…） | ✅ 合法：void 值本无存储，调用方靠 `"void"` 跳过 |
| `value=0 llvm="i64" slot=""` | **4** | **`0`** = `NoVal` | ❌ 真洞：消费方在读没有任何指令产生过的操作数 |

## 3. 影响面：**恰好 3 个文件，且全都本已失败**

`tests/` 语料用 golden 预言机；`test/` + `example/` 用**护栏前后的两个二进制**逐文件比 `rc`
（`git worktree add /tmp/no-preguard HEAD` → 建旧二进制 → 用完 `remove --force`）。

| 文件 | 护栏前 | 护栏后 | 性质 |
| --- | --- | --- | --- |
| `tests/mem-safety/str-concat-leak.no` | `run rc=0`，输出 **0–775 MB 不确定垃圾** | `run rc=1`，stdout 空 | 静默错误编译 |
| `tests/test-std-hash.no` | `run rc=1`（**编译成功**，程序自己失败，有输出） | `run rc=1`，stdout 空 | 静默错误编译 |
| `test/std/process.no` | `build rc=0` → `no test` **在 `t-cmd` 上 SIGSEGV** | `build rc=1` → `no test` **编译期点名 `t_cmd`** | ⭐ 段错误 → 可读诊断 |

- 前二者加护栏后的指纹与 `legacy-baseline`（冻结语义 oracle）**逐字节相同**（`1 e3b0c442…` 空 stdout）
  ⇒ **对上 oracle，不是回归**。
- 第三者的**最终判定没变**（`no test` 两版都 `FAIL`）⇒ **没有任何测试由通过变为失败**；
  变的是"跑着跑着段错误（零线索）"→"编译期点名 `t_cmd` 与缺失的值"。

## 4. 未改动的证明

`tests/` 全量（422，300 s / `-P 4`）比对 + 重冻结后与改动前基线 `diff`：

```
43c43
< 0 eb28fc09… tests/mem-safety/str-concat-leak.no
> 1 e3b0c442… tests/mem-safety/str-concat-leak.no
262c262
< 1 399229a5… tests/test-std-hash.no
> 1 e3b0c442… tests/test-std-hash.no
```

**恰好 2 行，无其它任何变动**（无 flaky、无附带）。`rc=0` 390 → 389、`rc!=0` 32 → 33。

| 桶（对 `mir-baseline`） | 值 |
| --- | --- |
| `SAME` / `DIVERGE` / `UNSTABLE` / `REGRESS` / `IMPROVED` / `BOTH_FAIL` | 389 / 0 / 0 / 0 / 0 / 32 |

`UNSTABLE` 1 → 0 不是漏报：`str-concat-leak.no` 现在编译失败、stdout 恒空，指纹**重新可冻结**，
故按脚本注释的约定从 `MIR_GOLDEN_UNSTABLE` 移除。语义桶（对 `legacy-baseline`，由两份冻结 TSV
join 推导）：`SAME=302 / DIVERGE=38 / REGRESS=0 / IMPROVED=49 / BOTH_FAIL=33` ——
**只有 `str-concat-leak.no` 一个文件**从 `IMPROVED` 移入 `BOTH_FAIL`。

## 5. 顺带发现（下轮工作项）

1. **根因更正**：不是"接收者绑错"（`args=[3]` 就是循环计数器 `i`）。真因是 `hir2mir.go:4959–4963`
   的 **void 分支**：`resTyp = resultTypeOfCallee(callee)` 三档回退（HIR 定义 → 内建表 → LHS 类型提示）
   全落空 ⇒ 调用按语句型发射、返回 `NoVal`。同族的 `s str = i.to-str()` / `g(i.to-str())` 正常，
   只因**有外部类型提示**。
2. **未定位**：该 `callee` 的确切拼写（既有调试钩子无输出、stderr 亦无 "unknown callee" ⇒ 名字可解析）。
   需给 MIR 转储补 `Sym` 字段。
3. **第二条独立失效路径**：`codegen.go` 的 `i64-to-str` 快路径在 `inst.Results` 为空时**静默 `return nil`**。
4. **桶方案盲点**：`BOTH_FAIL` 不比哈希，故 `test-std-hash.no` 的修复在桶计数上**完全不可见**。
   若修订桶方案，优先把 `BOTH_FAIL` 按"编译期失败 / 运行期失败"再切一层。

## 6. 判读口径（本轮新增）

- **`no build` 的 rc（编译器）≠ `no run` 的 rc（程序退出码）**，而预言机记的是后者。混用会虚构出
  并不存在的 `test-std-hash.no` `0→1` 转变（它早就是 rc=1）。
- **"输出稳定"不是"没有 undef"的证据**：`test-std-hash.no` 稳定输出 `399229a5…` 却含 2 处 `NoVal` 消费。
- **给出的影响半径必须写明扫描范围**：本轮"2 个文件"一度被当成完整答案，扩到 `test/`+`example/`
  就多出 1 个（全扫仅 2 分钟）。
- **不用猜数**：语义桶是两份冻结 TSV 的纯函数，`join` + 四个分支即可精确推导（本轮猜 48/34，实测 49/33）。
