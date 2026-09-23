# 发布流水线细节

本 skill 不绑定任何具体仓库：仓库根、remote、分支、HISTORY.md 位置全部在运行时探测。
下面描述的是**这类仓库的通用约定**（Nolang 是其典型场景）。

## 运行时探测的项

| 项 | 怎么拿到 |
| --- | --- |
| 仓库根 | `git rev-parse --show-toplevel`，从当前工作目录往上找；`--repo` 可覆盖 |
| 当前分支 | `git rev-parse --abbrev-ref HEAD` |
| remote / 发布页 URL | `git remote get-url origin`（ssh 形态会转成 https） |
| 工作树是否干净 | `git status --porcelain` |
| 上一个版本 | 仓库里 `vMAJOR.MINOR.PATCH` tag 中最大的那个 |

## 仓库侧的约定（不是探测出来的，使用前先确认）

| 项 | 约定 |
| --- | --- |
| 变更日志 | 仓库根 `HISTORY.md`，H1 是标题（如 `# 更新日誌`） |
| 版本提取脚本 | 仓库根 `history.sh` |
| tag 形态 | 轻量 tag（lightweight，非 annotated），命名 `vMAJOR.MINOR.PATCH` |
| 递增惯例 | 默认只递增末位：`vX.Y.Z` → `vX.Y.(Z+1)`（此举可改，见脚本 `resolve_version`） |
| 版本号补全 | 残缺输入自动补全：`0.3` → `v0.3.0`，`0.2.33` → `v0.2.33`（见 `normalize_version`） |

若目标仓库的变更日志文件名或提取脚本不同，改 `scripts/release.py` 里的
`HISTORY_FILE` 常量与 `cmd_verify` 即可。

## GitHub Actions 如何消费 HISTORY.md

`.github/workflows/build.yml` 的 `build-release` job（`if: startsWith(github.ref, 'refs/tags/')`）里有：

```yaml
- name: Get History
  run: |
    chmod +x history.sh
    output=$(./history.sh)      # 注意：没有传 $1
    echo "$output" > history.md
- uses: actions/create-release@v1
  with:
    tag_name: ${{ github.ref }}
    body_path: history.md
```

**关键推论**：`history.sh` 不带参数时，`VERSION` 为空、`TARGET_HEADER="## "`，于是它命中
**文件中第一个 `## ` 段落**并输出其内容。因此：

- 新版本条目**必须插到 HISTORY.md 最上方**（H1 之后的第一个 `## `），否则 release body 会取到错误的旧版本。
- 版本号标题必须**与 tag 完全一致**（`## v0.2.33` ↔ tag `v0.2.33`），否则匹配不上。
- 正文只需 bullet list（`- xxx`），不要用子标题，release body 会原样渲染。

`release.py verify` 就是跑 `history.sh`（同样不传参）预览 release body，等于 CI 的干跑。

## HISTORY.md 正文的语言与格式规范

Release body 会直接挂在 GitHub Release 页面上，面向公开读者，所以**只写英文**：

```
- feat(mir): implement tagged enums and fix option unpacking
- fix(checker): resolve self in flattened method call receivers
- docs(syntax): document enum and struct field annotations
```

- `- <type>(<scope>): <description>`，type ∈ `feat|fix|refactor|perf|docs|test|build|ci|chore`
- scope 用仓库自己的模块名（读取 `git log` 里已有提交的 scope 来对齐，别凭空编）
- description 小写祈使动词开头，句尾无句号
- 只用 bullet，不要子标题、不要中文

`release.py apply` 会做机器校验（CJK 字符检测 + conventional 正则），不合规直接 exit 3。
老版本段落里可能残留中文条目（比如初始 release），那是历史遗留，新条目不要照着写。

## 发布后会发生什么

push tag `v*` → `build.yml` 触发 → 交叉编译 5 个目标
（linux amd64/arm64、darwin amd64/arm64、windows amd64）→
用 `main.version` ldflag 注入版本号 → 创建 GitHub Release 并上传 5 个资产。

受影响的其他 workflow：
- `static.yml`：push `main`（且改动 `docs/**`、`src/**`、`Makefile`）会重建并部署文档站。
  所以 push main 会顺带触发文档部署，属正常现象。
- `build-nolang-app.yml`：仅 `workflow_dispatch`，是给下游项目抄的模板，与本仓库发布无关。

## 为什么必须连 main 一起 push

tag 指向本地 commit。只 `git push origin vX.Y.Z` 而该 commit 不在远端时，
CI checkout 拿不到包含 HISTORY.md 更新的代码，`history.sh` 读到的就是旧文件，release body 为空或错误。
所以顺序固定为：提交 → `git push origin main` → `git push origin vX.Y.Z`。

## 删除/回滚 tag（出错时）

```bash
git tag -d vX.Y.Z              # 删本地
git push origin :refs/tags/vX.Y.Z   # 删远端
```

已创建的 GitHub Release 需到网页手动删除；已上传的资产不会自动清理。
