#ifndef NOUI_SHIM_H
#define NOUI_SHIM_H

int noui_init(void);
long long noui_window_create(const char *title, int w, int h);
void noui_quit(void);

/* 阶段80：多窗口。id 一律 1 基槽位号（0=失败/无）。attach 决定既有
 * setter/getter/绘制函数作用在哪个窗；event 返回最近一次事件所属窗口。 */
int  noui_window_attach(long long id);
int  noui_window_count(void);
int  noui_window_close(long long id);
int  noui_window_event(void);

void noui_begin_frame(int r, int g, int b, int a);
void noui_draw_rect(float x, float y, float w, float h, int r, int g, int b, int a);
void noui_draw_text(float x, float y, const char *text);
/* 阶段6：SDL_ttf (HarfBuzz shaping) 字体渲染 */
int  noui_font_init(const char *path, double ptsize);
void noui_font_close(void);
void noui_font_draw(double x, double y, int r, int g, int b, int a, const char *text);
void noui_set_color(int r, int g, int b, int a);
void noui_end_frame(void);
/* 阶段23：滚动容器的裁剪（SDL_RenderSetClipRect；clear = 传 NULL） */
void noui_clip(int x, int y, int w, int h);
void noui_clip_clear(void);

long long noui_poll(void);
int noui_should_quit(void);
void noui_delay(int ms);
const char *noui_version(void);
/* 阶段76：文本输入与系统剪贴板。text_take 取走最近一次 TEXT_INPUT（读完即空）；
 * clipboard_copy / clipboard_paste 直接对接系统剪贴板（paste 有 4096 字节上限）。 */
const char *noui_text_take(void);
void noui_clipboard_copy(const char *text);
const char *noui_clipboard_paste(void);

/* 阶段78：图片 / 精灵。img_load 从 PNG 解码成 surface（纹理由各窗口惰性创建），
 * 返回 id（>0；0=失败）；img_size 返回 w<<32|h；draw_image 拉伸到 (x,y,w,h) 像素矩形。 */
long long noui_img_load(const char *path);
long long noui_img_size(long long id);
void noui_draw_image(double x, double y, double w, double h, long long id);

/* 阶段81：事件面扩展。类型码 1..24 见 shim.c 顶部常量表；鼠标/键盘 aux 字节带
 * 修饰键位（bit0 Shift / bit1 Ctrl / bit2 Alt / bit3 Meta），触摸 aux 带压力
 * （0..255）。drop_take 取走最近一次 DROP_FILE / DROP_TEXT 的路径或文本
 * （读完即空，配合类型 19 / 24）；display_scale1000 返回窗口 DPI 缩放 x1000。 */
const char *noui_drop_take(void);
long long noui_window_display_scale1000(void);

/* 阶段81：绘制原语。round_rect 用逐行收窄近似圆角（SDL_Renderer 无圆角 API）；
 * shadow 为多层半透明外扩圆角矩形（spread 固定 10px，dy 为竖直偏移）；
 * draw_image_rot 按目标盒中心旋转 angle 度并可选水平 / 垂直翻转。 */
void noui_draw_round_rect(float x, float y, float w, float h, float rad,
                          int r, int g, int b, int a);
void noui_draw_shadow(float x, float y, float w, float h, float rad, float dy,
                      int r, int g, int b, int a);
void noui_draw_image_rot(double x, double y, double w, double h, long long id,
                         double angle, int flip);

/* 阶段52：窗口选项（C-WINDOW-EXT 直接驱动真窗口）。
 * 全部标量进出；查询类把宽 / 高打包进一个 i64（w << 32 | h），flags 原样返回。 */
void noui_window_set_title(const char *title);
void noui_window_set_resizable(int yes);
void noui_window_set_min_size(int w, int h);
void noui_window_set_max_size(int w, int h);
void noui_window_set_fullscreen(int yes);
void noui_window_set_always_on_top(int yes);
void noui_window_set_bordered(int yes);
void noui_window_set_opacity(double o);
void noui_window_set_size(int w, int h);
long long noui_window_get_size(void);
long long noui_window_get_flags(void);
const char *noui_window_get_title(void);

#endif
