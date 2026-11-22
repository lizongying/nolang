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

#endif
