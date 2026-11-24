#ifndef NOUI_SHIM_H
#define NOUI_SHIM_H

int noui_init(void);
long long noui_window_create(const char *title, int w, int h);
void noui_quit(void);

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
