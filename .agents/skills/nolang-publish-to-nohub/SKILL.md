---
name: nolang-publish-to-nohub
description: 把一个 Nolang 仓库公开或收录到 nohub（元数据注册中心）的标准流程：在 lizongying/nohub 的 repos 目录下增加作者收录条目并发起 PR，预先自查仓库存在、作者身份、仓库根 workspace.noml、至少一个 package 四项要求。当用户要求把某 Nolang 仓库公开到 nohub、收录到 nohub、加到 repos 清单、向 nohub 提 PR，或询问 nohub 收录要求时使用。
agent_created: true
---

# Nolang 公开到 nohub

## 概述

nohub 是「只索引、不存码」的社区包注册中心。把一个 Nolang 仓库公开到 nohub **不是**往你的仓库里推代码，而是在
`lizongying/nohub` 的 `repos/<host>/<owner>.noml`（作者收录记录）里加一条你的仓库短名，然后由作者本人开 PR。
合并后，你的仓库就会被 nohub 收录索引，别人可以用 `no search/info/docs` 找到它。

本 skill 只讲**使用者要做什么、要满足什么要求**。它不替用户执行不可逆的 git 操作（add / commit / push / 开 PR）——
这些必须由用户逐步确认后自行完成。

## 关键事实（动手前必读）

- 作者记录路径即身份：`repos/github.com/<owner>.noml` 只能由 GitHub login 为 `<owner>` 的人创建/修改/删除。别人代提的 PR 不会被接受。
- 元素是仓库**短名**（如 `nolang`），对应 `https://github.com/<owner>/<short-name>`；也可写成对象覆盖 `branch` / `workspace` / `status` / `sync`。
- 先看目标文件当前内容，已有条目不要重复添加：`nolang`、`notools` 已在 `lizongying` 名下。
- nohub 自身不提供检索命令；检索在 `no` 里完成。收录是否生效，以合并后的索引为准。

## 工作流

### 第 1 步：确认仓库满足四项要求

提交前逐项自查（详见 `references/admission-checks.md`）：

1. **仓库存在且公开**：`https://github.com/<owner>/<repo>` 可访问；你的账号对其有 push 权限。
2. **作者匹配**：目标文件路径里的 `<owner>` 等于你的 GitHub login。
3. **仓库根有 workspace.noml**：位于仓库根，是纯字符串映射（包名 → 相对路径），至少一个键。
4. **至少一个 package**：`workspace.noml` 每个键对应的包目录里有 `package.noml`，且 `name` 与键一致。

单包仓库可省略 `workspace.noml`、改在根目录放 `package.noml`——但本流程按「根有 workspace.noml 且至少一个 package」为准。

### 第 2 步：编辑作者记录

在 nohub 仓库（默认 `/Users/lizongying/IdeaProjects/nohub`）的 `repos/<host>/<owner>.noml` 追加一条：

```noml
[
  'nolang',
  'notools',
  'my-new-repo',        ; 字符串元素 = 全部缺省配置
]
```

需要覆盖配置时用对象元素：

```noml
[
  {
    name: 'my-new-repo'
    branch: 'main'
    workspace: '/workspace.noml'
    status: 'active'
    sync: {
      schedule: 'daily'
      timezone: 'Asia/Shanghai'
    }
  },
]
```

命名与缩进遵循 `no fmt`：数组元素带尾随逗号，成员无逗号，2 空格缩进。改完跑 `no fmt -d <file>` 确认无差异。

### 第 3 步：开 PR（由用户执行）

- 基于最新 `main` 新建分支，只提交作者记录文件的改动。
- PR 标题建议 `feat(repos): add <owner>/<repo>`，正文说明仓库用途与包清单。
- 必须用户本人确认后再 `git add <具体路径>` / `commit` / `push` / 开 PR。禁止 `git add -A` 与任何 checkout/reset/stash/restore/clean。
- 若 `gh` token 失效，改用 git credential（keychain）push，再由用户在网页端开 PR。

### 第 4 步：PR 检查与收录生效

PR 提交后会有一个自动检查，核对你的仓库是否满足上面四项要求；检查不通过会标红，按 `references/admission-checks.md`
的失败对照表修正后再推送。检查通过并合并后，仓库即被收录；稍后用 `no search <包名>` 验证能否检索到。

## 资源

- `references/admission-checks.md` —— 四项要求的判定规则、失败对照、noml 字段约束与开 PR 前自查方法。
- `scripts/precheck.sh` —— 开 PR 前的本地自查脚本（只读检查作者记录格式与重复元素；网络项给出手工验证命令）。
