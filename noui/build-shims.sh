#!/bin/sh
# noui C shim 统一构建：SDL_Render(+TTF 字体) 回退后端 + SDL_GPU 后端
set -e
W=$(cd "$(dirname "$0")" && pwd)
CLANG=/opt/homebrew/opt/llvm/bin/clang
AR=/opt/homebrew/opt/llvm/bin/llvm-ar
SDL_I=/opt/homebrew/opt/sdl3/include
TTF_I=/opt/homebrew/opt/sdl3_ttf/include

mkdir -p "$W/dist"

echo "[shim] 编译 SDL_Render + SDL_ttf 后端 (shim.c)"
"$CLANG" -c -O1 -I"$SDL_I" -I"$TTF_I" \
  "$W/src/shim/shim.c" -o "$W/dist/shim.o"
"$AR" rcs "$W/dist/libnoui-shim.a" "$W/dist/shim.o"

echo "[shim] 编译 SDL_GPU 后端 (gpu_shim.c)"
"$CLANG" -c -O1 -I"$SDL_I" \
  "$W/src/shim/gpu_shim.c" -o "$W/dist/gpu_shim.o"
"$AR" rcs "$W/dist/libnoui-gpu-shim.a" "$W/dist/gpu_shim.o"

echo "[shim] 完成：libnoui-shim.a / libnoui-gpu-shim.a"
