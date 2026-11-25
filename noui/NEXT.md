# noui 下一步

> 最近更新：阶段 81（事件面与绘制原语补齐）。进度明细见 `README.md` 的
> 「验收记录」与 `ARCHITECTURE.md` §5.73 / §5.74；验证入口是
> `scripts/run-noui-tests.sh`（无头全量，dummy 驱动 + 帧闸，CI 同路）。

## 现在能做什么

noui 是一个可运行的声明式 UI 装配层：组件树（`src/components*.no`）→ Scene
指令池（`src/cmp-scene.no`）→ SDL3 绘制（软件渲染器或 SDL_GPU），窗口 / 输入 /
文本 / 图片 / 多窗口齐备。截至阶段 80 已接入 CI；阶段 81 补齐了审查 P2 的
平台能力缺口（详见 `ARCHITECTURE.md` §5.74）。

## 尚未做（按价值排序）

1. **GPU 后端的纹理管线**：目前 GPU 只画矩形，文本 / 图片 / 圆角在 GPU 模式
   下静默降级或跳过。需要给 `gpu_shim.c` 加纹理创建 + 采样绘制（SDL_GPU
   的 texture pass），并让 `app-draw-scene` 分流新指令。
2. **压感笔 / 触摸的事件侧语义**：事件码与压力已通到 `app-poll`，但还没有
   组件消费它们（如手写板、双指手势）。可在 `dv-app.no` 主循环加分支，把
   finger / pen 命中派发到视图动作。
3. **拖放落点反馈**：`DROP_BEGIN/POS/COMPLETE/FILE/TEXT` 已能收到，组件侧
   还没有「拖入高亮 / 放置区」语义，需要视图层定义。
4. **字体回退列表产品化**：现在是 `app-init` 里的 5 个硬编码路径，缺一个
   可声明的候选清单（主题里配字体），以及运行期 DPI 变化时重开字体。
5. **工程化收尾**：`gen-test.sh` 已删（早期内核拼接脚本，产出保留为历史
   回归）；README / ARCHITECTURE 仍偏长，值得做一次目录索引（按组件 /
   阶段双向跳转）。

## 约定

- 新增平台能力：先加 shim C 函数 + `src/app.no` 的 FFI 包装 + 解码函数，
  再补 `tests/*.no`（纯逻辑优先，避免依赖真窗口），最后过全量套件。
- 全量基线：`cd noui && ./scripts/run-noui-tests.sh`，必须 100% 通过；
  CI job 同名同路（`.github/workflows/test.yml` 的 `noui-test`）。
