---
description: Nolang 编译器仓库的硬红线与验收流程：git 安全、构建口径（make gen / make no）、src/std 改动纪律、索引写与空串禁令、parser ASTVersion 版本闸、自举与回归门禁。改动 src/、src/std/、compiler/、tests/ 之前必须按本规则执行。
alwaysApply: true
enabled: true
---

# Nolang 硬红线（Red Lines）

**权威顺序**：实测 > `AGENTS.md`（语法与编码权威）> 本规则（红线与流程摘要）> 记忆笔记。
本规则只收录「违反了就会静默出错或不可逆丢东西」的条目；语法细节查 `AGENTS.md`，
std 未修缺陷查 `test/README.md §5.2`，名词查 `./glossary/dist/glossary`。

## 1. Git：只读回看，绝不丢工作区

- 🔴 **禁止** `git checkout` / `reset` / `restore` / `stash` / `clean` / `push --force`。
- 本仓库常有**多个并发 session** 同时改同一棵树，上述命令会静默覆盖别人的在飞改动。
- 回看旧版：`git show <rev>:<path>`、`git diff <rev> -- <path>`。要比对/实验用隔离 worktree，
  目录放项目内 `tmp/` 下并按 `{type}/{short-id}` 命名（见下方 worktree 约定），不写系统 `/tmp`。
- 动工前后各跑一次 `git status`；**不属于本任务**的已修改文件一律不碰、不「顺手修复」。

### Worktree 约定（防孤悬）

- 目录名**短、稳定、可机器识别**：`{type}/{short-id}` 或 `{ticket}/{short-tag}`，例如
  `feat/option-3state`、`fix/verify-rule`、`refactor/hir-rebuild`；统一挂在 `tmp/` 下，如
  `tmp/feat/option-3state`。
- 🔴 每个 worktree **必须带元信息**，否则就是「谁建的、目的、能不能清理」都不明的孤悬树。
  worktree 内放 `worktree-meta.noml`（集中索引示例路径 `.worktree-meta/feat-option-3state.noml`，
  把 `/` 换成 `-`），至少写清**一句话 desc、创建人、时间、原因**，可选工单/PR 链接：

  ```noml
  {
    name: 'feat/option-3state'
    creator: 'lizongying'
    created-at: '2026-11-21 00:00:00'
    desc: '重构 Option 三态 ok/nil/err 校验逻辑，验证 ?= 上抛分支规则，临时验证，不合并到主分支'
    link: 'https://xxx/issue/123'
  }
  ```

- 🔴 清理 worktree 前**先读 `worktree-meta.noml`**：只删自己建的；他人建的、或归属/目的不明的
  一律留着（与 `tmp/` 清理红线一致）。用完 `git worktree remove` 及时收掉自己的树，每棵 100M+。

## 2. 构建：只走 make，别绕签名表

- 构建 `bin/no`：`make no`；跑测试：`make test`；只重生成签名表：`make gen no`。
- 🔴 **禁止** `cd src && go build`：`make no` 会先 `make gen` 重生成 `src/checker/stdsig_gen.go`，
  绕过它就是「签名表与源码不一致 ⇒ 构建被拒或静默退化」。
- 🔴 改 `src/std/**`（**注释也算**）⇒ 必须走 `make no`。`no build` / `no test` 的 std 来自
  **内嵌 blob**，不是磁盘上的 `src/std`；`touch src/std_embed.go && make no` 保证 embed 重算。
- 🔴 `bin/no` 陈旧 ⇒ 整片假象（绿色/红色都不可信）：行为诡异先比 mtime；exit 137 或零输出先
  `rm -f bin/no && make gen no`。
- 裸 `no`（`/usr/local/bin/no`）与 `./bin/no` **不是同一个二进制**；排查「你绿我红」先比 `version`。
- 🔴 **临时构建也写项目根 `tmp/`**：ad-hoc `no build -o …`、一次性编译探针、临时可执行一律输出
  到 `tmp/`，不要写系统 `/tmp`。`bin/no` / `bin/lsp` 是 `make` 产出的正式二进制，是唯一例外。

## 3. std：绝不对 `src/std` 跑 `no fmt -w`

单档解析会丢注解与手写排版。需要格式化时：`no fmt <f>` 输出到 /tmp → `diff -u` → 只 `patch` 目标 hunk，
改完必须 `./bin/no vet src/std` 仍是 **0 error**（`no fmt` 会删它判定「无效」的 `#{overflow}`，
`warning` / `hint` 会漂移，不作门槛）。

## 4. 语言禁令（改或写 `.no` 前必读）

- 🔴 **索引写全域禁用**：`s[i] = v` / `t[i] = c`（str / txt）。变长用 `push` 家族
  （`str.push(c)` / `str.push-str(s)`；`txt.push` / `push-str` / `push-txt`），原位覆写用 `fill` 或
  place-match，逐字节构造用 `[]byte` + `to-bytes-str`。
- 🔴 **空 `str` 的 `data == NULL`**：`eq(x, n)` 在 `n > 0` 直接崩（含 `str.slice` 提前 return 的空视图）
  ⇒ 空串断言一律 `n = 0`。
- 🔴 `t[i]` / `s[i]` **越界读是 `err` 不是 `-1`**：断言 tag，不要断言兜底值。
- 🔴 不写 `free`（deferred move + 作用域退出自动销毁）；协程只能写成 `awy { r = co f() }`，`co` 组外非法（`E_CO_OUTSIDE_GROUP`）；`go` **不是**关键字，`run` 已移除，`awy <handle>` 已废除。
- 🔴 match 各臂绑定变量**不得同名**（不同类型同名会静默串型，checker 不报）；`?t` 解包优先用 `it`。
- 🔴 `#{overflow=...}` **只作用于下一行陈述**（L3），挂在函数头 / 块头上对 codegen 无效。
- 🔴 **頂層數據綁定（全局/常量）按首字符四檔**（`build/global_case.go` 的 `checkLowercaseGlobals` 管同模組、`build/module_check.go` 的 `checkUnresolvedModuleCalls` 管跨模組，訊息含原因 + 改名建議）：
  - `BUF` 大寫 = **全局**：跨函式 / 跨模組都合法。
  - `buf` 小寫 = **局部**：被函式引用 → 硬錯誤（改 `Buf`）；跨模組 `mod.buf` 也硬錯誤。純 main 局部（如 `passed = 0`）合法。
  - `_BUF` = **私有全局**：**同模組跨函式讀寫合法**（`_` 是「模組私有」標記，不是「不可被函式引用」）；只有**跨模組** `mod._BUF` 訪問 → 硬錯誤（去底線改 `BUF`）。
  - `_buf` = **私有局部**：Nolang **不開放**，無條件硬錯誤（改 `Buf` 或去前綴）。全局由大寫前綴唯一標識；單獨 `_` 是通配綁定，放行。
  - **函數名、方法名、接口名、結構體名不受限**，可小寫 / `_` 開頭（如 `foo`、`_helper`、`_rsa-bn-add`）。別把合法的小寫函數名 / main 局部 / `_` 通配誤判成錯誤。
- 🔴 `with-len(0)` 这类 call-init 容器**只有大写名才注册为 module global**；小写名即使在同模块函数内 `push` 也会失败（`vec.push: needs receiver and element`）。跨函数共享的数据一律大写。
- 🔴 `(cond) { }` 是**条件循环（while）**，`(cond) -> { }` 才是一次性守卫（guard）。把 guard 写成 `(cond) { }` 且 body 不改条件会**死循环**（noui 内核 `el-add` 曾因此卡死）；两种写法见 `docs/docs/lang/syntax.md` 循环章节。
- 🔴 模块内避免全局可变状态。
- 每条语句独占一行（`;` 是注释标记，不是语句分隔符）；4 空格缩进；文件以单个换行结尾。

## 5. 版本闸：`parser.ASTVersion` 双向

- **必须 bump**：`ParseProgram` 的返回变了，或树的序列化变了——Surface AST 节点/字段、由源码或
  解析期推断产生的字段值、`CachedProgram` 的 gob 布局（含新增的按节点键控事实表漏了 `detU32*` 镜像）、
  HIR 消费的解析期侧表（annotations / platform / embed / 泛型参数 / IndexInBounds / InferredType /
  ModuleConst / SourceFile / ModuleOwner …）。
- **不得 bump**：严格在缓存下游的改动——lowering（H0 糖消除、match 表示）、单态化、模块改名/前缀、
  call 重写、克隆隔离。这些只改 HIR，归 `hir.HIRVersion`。
- 任何**跨版本 A/B 或 golden 对比**：`NOLANG_AST_CACHE_OFF=1`（缓存键是内容寻址，否则会读到旧版本产物）。

## 6. 自举闸只认 Go 后端

- `./bin/no run compiler/tests/<X>.no`；**不要** `noc run`（测试档带 `#` 导入，noc 不支持 ⇒ 假红）。
- 重建 noc：`./bin/no build -o OUT compiler`（耗时数分钟，属正常）。
- 套件模式下只有入口档过 checker，`compiler/` 的模组从未被型别检查；改编译器不要假设它们被验证过。

## 7. 黄金法则：不改合法代码来「修」疑似 bug

即使怀疑 parser / checker / codegen 有缺陷，也**只报告 + 最小复现**，不改写语法正确的代码。
确属编译器缺陷时，写 Go 测试钉住，报告时附「最小复现 + 期望/实际」。

## 8. 改完就验收（顺序固定）

```bash
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"

make no                              # 改过 src/std 或 Go 代码必做
./bin/no vet src/std                 # 必须 0 error
./bin/no test test/std               # 目录级 gate：ALL TESTS PASSED / rc=0
cd src && go test -count=1 ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/
```

- 改过 `src/std`：额外确认 `no fmt` 是 fixed point（fmt 前后 `shasum` 全等）。
- 改过 `src/`（Go）：`gofmt` 相关文件，并跑全量 `make test`（rc=0）。
- 判据只认 `ALL TESTS PASSED` / `timed out` / `SOME TESTS FAILED`；其余一律按构建失败排查，
  不要因为「输出看起来像通过」就宣布绿灯。
- 未修缺陷与已知噪声（非确定性用例、既有 DIVERGE）见 `test/README.md §5.2`：**别把预存红当自己造成的，
  也别把自己造成的红当预存**——拿干净 worktree 的 `bin/no` 跑同一份测试档即可分诊。
