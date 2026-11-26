#ifndef NOUI_GPU_SHIM_H
#define NOUI_GPU_SHIM_H

int  noui_gpu_init(void);
int  noui_gpu_active(void);
void noui_gpu_set_clear(int r, int g, int b, int a);
long long noui_gpu_window_create(const char *title, int w, int h);
long long noui_gpu_window_get_size(void);
/* 坐标一律用 double（与 app.no 的 f64 FFI 声明一致，避免 float/double ABI 错位） */
void noui_gpu_draw_rect(double x, double y, double w, double h, int r, int g, int b, int a);

/* 阶段82：纹理管线。
 * - 字体：GPU 后端自己开 TTF 字体，文字渲染成白底 alpha 蒙版纹理（着色时再乘颜色）。
 * - 图片 / 文字统一进纹理注册表（id 从 1 起，0=无效），tex_size 打包 w<<32|h。
 * - draw_tex 把纹理拉伸到像素矩形；draw_tex_rot 绕盒中心旋转 angle 度并翻转
 *   （flip 位 1=水平、2=垂直）。
 * - round / shadow 在 GPU 上用同一套四边形管线：圆角走片元 SDF，投影拆成多层。
 */
int  noui_gpu_font_init(const char *path, double ptsize);
int  noui_gpu_font_active(void);
void noui_gpu_font_close(void);
long long noui_gpu_img_load(const char *path);
long long noui_gpu_text_load(const char *text);
long long noui_gpu_tex_size(long long id);
void noui_gpu_draw_tex(double x, double y, double w, double h, long long id,
                       int r, int g, int b, int a);
void noui_gpu_draw_tex_rot(double x, double y, double w, double h, long long id,
                           double angle, int flip);
void noui_gpu_draw_round(double x, double y, double w, double h, double rad,
                         int r, int g, int b, int a);
void noui_gpu_draw_shadow(double x, double y, double w, double h, double rad,
                          double dy, int r, int g, int b, int a);
long long noui_gpu_display_scale1000(void);

void noui_gpu_frame(void);
int  noui_gpu_should_quit(void);
void noui_gpu_delay(int ms);
void noui_gpu_quit(void);

#endif
