# Red lines — 违反了就静默出错或丢东西

**权威顺序**：实测 > `AGENTS.md`（语法与编码权威） > `.agents/rules/nolang-red-lines.md`（红线全文） > 记忆笔记。

本档只放**每次动手前必须扫一眼**的条目；完整论证、命令与环境见
`.agents/rules/nolang-red-lines.md`（`alwaysApply`，宿主支持时无需点名加载）。
两份如有出入，以该文件与实测为准。

## Git

- 🔴 禁 `git checkout` / `reset` / `restore` / `stash` / `clean` / `push --force`。
- 🔴 工作区随时被**并发 session** 改写：动工前后各 `git status` 一次；不属于本任务的已改文件不碰、不"顺手修"。
- 只读回看用 `git show <rev>:<path>` / `git diff <rev> -- <path>`；要比对用隔离 worktree。

## 构建

- 🔴 构建只走 `make no`；**禁** `cd src && go build`（绕过 `make gen` 的签名表重生成）。
- 🔴 改 `src/std/**`（注释也算）⇒ 必须 `make no`。`no build` / `no test` 的 std 来自**内嵌 blob**，不是磁盘 `src/std`。
- 🔴 `bin/no` 陈旧 ⇒ 整片假象：行为诡异先比 mtime；exit 137 或零输出先 `rm -f bin/no && make gen no`。
- 裸 `no`（`/usr/local/bin/no`）与 `./bin/no` 不是同一个二进制；排查"你绿我红"先比 `version`。

## 语言（写 `.no` 前必读）

- 🔴 **索引写全域禁用**：`s[i] = v` / `t[i] = c`。变长走 `push` 家族（`str.push` / `push-str`；`txt.push` / `push-str` / `push-txt`），原位覆写走 `fill` 或 place-match，逐字节构造走 `[]byte` + `to-bytes-str`。
- 🔴 空 `str` 的 `data == NULL`：`eq(x, n)` 在 `n > 0` 直接崩 ⇒ 空串断言一律 `n = 0`。
- 🔴 `t[i]` / `s[i]` 越界读是 `err` 不是 `-1`：断言 tag，不要断言兜底值。
- 🔴 不写 `free`（deferred move + 作用域退出自动销毁）；协程用 `co` 或协程组 `{}`，`go` **不是**关键字。
- 🔴 match 各臂绑定变量不得同名（不同类型同名会静默串型，checker 不报）；`?t` 解包优先用 `it`。
- 🔴 `#{overflow=...}` 只作用于**下一行陈述**；挂函数头 / 块头对 codegen 无效。
- 🔴 全局变量必须大写开头；模块内避免全局可变状态。
- 每条语句独占一行（`;` 是注释标记，不是语句分隔符）；4 空格缩进；文件以单个换行结尾。

## 验收（顺序固定）

```bash
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"

make no                              # 改过 src/std 或 Go 代码必做
./bin/no vet src/std                 # 必须 0 error（见 m40-verify.md）
./bin/no test test/std               # 目录级 gate：ALL TESTS PASSED / rc=0
cd src && go test -count=1 ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/
```

- 判据只认 `ALL TESTS PASSED` / `timed out` / `SOME TESTS FAILED`；其余一律按构建失败排查。
- 改过 `src/std`：额外确认 `no fmt` 是 fixed point（fmt 前后 `shasum` 全等）；**绝不**对 `src/std` 跑 `no fmt -w`。
- 未修缺陷与已知噪声见 `test/README.md §5.2`：别把预存红当自己造成的，也别把自己造成的红当预存。

## 黄金法则

即使怀疑 parser / checker / codegen 有缺陷，也**只报告 + 最小复现**，不改写语法正确的代码。
确属编译器缺陷时，写 Go 测试钉住，报告时附「最小复现 + 期望/实际」。
