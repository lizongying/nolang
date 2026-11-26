# noui 下一步

> 最近更新：阶段 82（GPU 后端的纹理管线）。进度明细见 `README.md` 的
> 「验收记录」与 `ARCHITECTURE.md` §5.73 / §5.74 / §5.75；验证入口是
> `scripts/run-noui-tests.sh`（无头全量，dummy 驱动 + 帧闸，CI 同路）。

## 现在能做什么

noui 是一个可运行的声明式 UI 装配层：组件树（`src/components*.no`）→ Scene
指令池（`src/cmp-scene.no`）→ SDL3 绘制（软件渲染器或 SDL_GPU），窗口 / 输入 /
文本 / 图片 / 多窗口齐备。截至阶段 80 已接入 CI；阶段 81 补齐了审查 P2 的
平台能力缺口；阶段 82 打通 GPU 纹理管线（PNG / 文字 / 旋转翻转 / 圆角 / 阴影，
dummy 驱动优雅降级，详见 `ARCHITECTURE.md` §5.75）。

## 尚未做（按价值排序）

1. **压感笔 / 触摸的事件侧语义**：事件码与压力已通到 `app-poll`，但还没有
   组件消费它们（如手写板、双指手势）。可在 `dv-app.no` 主循环加分支，把
   finger / pen 命中派发到视图动作。
2. **拖放落点反馈**：`DROP_BEGIN/POS/COMPLETE/FILE/TEXT` 已能收到，组件侧
   还没有「拖入高亮 / 放置区」语义，需要视图层定义。
3. **字体回退列表产品化**：现在是 `app-init` 里的 5 个硬编码路径，缺一个
   可声明的候选清单（主题里配字体），以及运行期 DPI 变化时重开字体。
4. **工程化收尾**：`gen-test.sh` 已删（早期内核拼接脚本，产出保留为历史
   回归）；README / ARCHITECTURE 仍偏长，值得做一次目录索引（按组件 /
   阶段双向跳转）。

## 约定

- 新增平台能力：先加 shim C 函数 + `src/app.no` 的 FFI 包装 + 解码函数，
  再补 `tests/*.no`（纯逻辑优先，避免依赖真窗口），最后过全量套件。
- 全量基线：`cd noui && ./scripts/run-noui-tests.sh`，必须 100% 通过；
  CI job 同名同路（`.github/workflows/test.yml` 的 `noui-test`）。
