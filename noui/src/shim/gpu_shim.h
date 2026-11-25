#ifndef NOUI_GPU_SHIM_H
#define NOUI_GPU_SHIM_H

int  noui_gpu_init(void);
int  noui_gpu_active(void);
void noui_gpu_set_clear(int r, int g, int b, int a);
long long noui_gpu_window_create(const char *title, int w, int h);
long long noui_gpu_window_get_size(void);
/* 坐标一律用 double（与 app.no 的 f64 FFI 声明一致，避免 float/double ABI 错位） */
void noui_gpu_draw_rect(double x, double y, double w, double h, int r, int g, int b, int a);
void noui_gpu_frame(void);
int  noui_gpu_should_quit(void);
void noui_gpu_delay(int ms);
void noui_gpu_quit(void);

#endif
