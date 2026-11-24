# nohub 收录要求

对象是 **Nolang 仓库的使用者**：你要把自己的仓库公开到 nohub，需要满足以下要求。
（格式规范原文见 nohub 项目 `docs/noml-format.md` §5 / §6.1 / §8。）

## 一、四项要求

| 要求 | 判定规则 | 不满足时 |
| --- | --- | --- |
| 作者匹配 | 你的 GitHub login（大小写不敏感）等于被改动文件路径 `repos/<host>/<owner>.noml` 里的 `<owner>` | PR 检查失败，不能被收录 |
| 仓库存在 | 数组每个元素（字符串名或对象 `name`）对应的 `https://<host>/<owner>/<name>` 真实存在且公开 | 检查失败 |
| 工作区存在 | 该仓库根的 `workspace.noml`（对象条目可用 `workspace` 覆盖路径）可读，且是合法 noml 顶层对象 | 检查失败 |
| 至少一个 package | `workspace.noml` 至少有一个键；每个键对应的包目录里有 `package.noml`，顶层对象是纯字符串映射 | 检查失败 |

补充约束：

- 作者身份只看 GitHub login，**不看** `package.noml` 里可随意填写的 `author` 字段。
- 数组里的仓库短名不得重复；对象条目 `name` 必填且与 `<owner>/<name>` 一致。
- `status` 仅允许 `active` / `paused`（缺省 `active`）；`sync.schedule` / `sync.timezone` 为可选字符串。
- 仓库短名只含字母、数字、点、下划线、连字符。

## 二、workspace.noml 与 package.noml 要点

`workspace.noml` 是**纯字符串映射**，不能放对象值（否则 `no build` 直接报错）：

```noml
{
  glossary: '/glossary'
  sync: '/sync'
}
```

`package.noml` 放在每个包目录，键名与 `name` 一致；`version` 写了就要是语义化版本（也可不写），`license` 写了要是有效 SPDX 标识：

```noml
{
  name: 'glossary'
  version: 'v0.1.0'
  description: 'Nolang 词条化知识库'
  license: 'Apache-2.0'
}
```

## 三、失败对照表

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 检查提示作者不匹配 | 用 A 账号改了 `repos/github.com/B.noml` | 换 B 账号重开 PR，或把条目放进你自己（A）的作者文件 |
| 检查提示仓库不存在 | 短名拼错、仓库私有或已删 | 修正短名，或把仓库设为公开 |
| 检查提示缺少 workspace.noml | 清单不在仓库根，或对象条目 `workspace` 路径写错 | 把 `workspace.noml` 移到根，或更正 `workspace` 字段 |
| 检查提示没有 package | `workspace.noml` 为空对象，或键对应的 `package.noml` 缺失 | 补 `package.noml`，键名与 `name` 对齐 |
| 合并后 `no search` 查不到 | 收录尚未生效，或包因清单问题被跳过 | 核对 `package.noml` 字段，修正后重新触发收录 |

## 四、开 PR 前自查

跑 skill 自带的只读自查脚本（把路径换成你的作者记录）：

```bash
<skill目录>/scripts/precheck.sh repos/github.com/<owner>.noml /Users/lizongying/IdeaProjects/nohub
```

脚本只做只读检查：文件是否存在、noml 顶层是否为数组、短名是否合法且不重复，并打印需要网络确认的仓库与 workspace 地址。
它不修改任何文件、不执行 git 写操作。
