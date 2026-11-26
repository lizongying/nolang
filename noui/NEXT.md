# noui 下一步

> 最近更新：字体路径改为逐条 malloc（无长度上限）+ ARCHITECTURE 篇幅重构
> （§5.3–§5.78 外移到 `ARCHITECTURE-NOTES.md`）。进度明细见 `README.md` 的
> 「验收记录」、`ARCHITECTURE-NOTES.md`；验证入口是
> `scripts/run-noui-tests.sh`（无头全量，dummy 驱动 + 帧闸，CI 同路）。

## 现在能做什么

noui 是一个可运行的声明式 UI 装配层：组件树（`src/components*.no`）→ Scene
指令池（`src/cmp-scene.no`）→ SDL3 绘制（软件渲染器或 SDL_GPU），窗口 / 输入 /
文本 / 图片 / 多窗口齐备。截至阶段 80 已接入 CI；阶段 81 补齐了审查 P2 的
平台能力缺口；阶段 82 打通 GPU 纹理管线；阶段 83 统一鼠标 / 手指 / 压感笔
的主指针语义；阶段 84 补上拖放会话与放置区（`F-DROP` + `dva-drop-*`）；
阶段 85 把字体候选清单产品化（C 侧持有清单 + 内置默认 + DPI 重开，
`app-font-add/try-all/reopen`；后续把定长上限改成动态增长，主题可声明任意多条
回退）；工程化收尾补上「阶段 × 组件双向索引」
（`README.md` 开头「目录索引」、`ARCHITECTURE.md` §0），两处长文现在可以
按阶段 / 组件直接跳转。架构决策详见 `ARCHITECTURE.md` §5.1–§5.2 与
`ARCHITECTURE-NOTES.md`（原 §5.3–§5.78，编号保留）。

## 尚未做

无必做项。文档篇幅与字体清单两项收尾已全部完成，当前只留历史记录里那些
「早期决策、已不适用」的叙述（如 §1 探针结论），不再有新的执行计划。

## 约定

- 新增平台能力：先加 shim C 函数 + `src/app.no` 的 FFI 包装 + 解码函数，
  再补 `tests/*.no`（纯逻辑优先，避免依赖真窗口），最后过全量套件。
- 全量基线：`cd noui && ./scripts/run-noui-tests.sh`，必须 100% 通过；
  CI job 同名同路（`.github/workflows/test.yml` 的 `noui-test`）。
