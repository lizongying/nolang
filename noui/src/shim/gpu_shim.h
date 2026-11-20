#ifndef NOUI_GPU_SHIM_H
#define NOUI_GPU_SHIM_H

int  noui_gpu_init(void);
long long noui_gpu_window_create(const char *title, int w, int h);
void noui_gpu_draw_rect(float x, float y, float w, float h, int r, int g, int b, int a);
void noui_gpu_frame(void);
int  noui_gpu_should_quit(void);
void noui_gpu_delay(int ms);
void noui_gpu_quit(void);

#endif
