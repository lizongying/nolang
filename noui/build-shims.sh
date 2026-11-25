#!/bin/sh
# noui C shim 统一构建：SDL_Render(+TTF 字体) 回退后端 + SDL_GPU 后端
set -e
W=$(cd "$(dirname "$0")" && pwd)
# 默认 macOS homebrew（Intel 与 arm64 的 prefix 都是 /opt/homebrew）；
# Linux / 自定义安装用环境变量覆盖，CI 与本机共用一份脚本。
CLANG=${CLANG:-/opt/homebrew/opt/llvm/bin/clang}
AR=${AR:-/opt/homebrew/opt/llvm/bin/llvm-ar}
SDL_I=${SDL_I:-/opt/homebrew/opt/sdl3/include}
TTF_I=${TTF_I:-/opt/homebrew/opt/sdl3_ttf/include}

mkdir -p "$W/dist"

echo "[shim] 编译 SDL_Render + SDL_ttf 后端 (shim.c)"
"$CLANG" -c -O1 -I"$SDL_I" -I"$TTF_I" \
  "$W/src/shim/shim.c" -o "$W/dist/shim.o"

# 阶段78：PNG 解码器（依赖 zlib；CI 侧 brew install zlib 或系统 SDK 均提供头文件，
# 链接时由 package.noml 的 link-libs 追加 -lz）。
echo "[shim] 编译 PNG 解码 (img_png.c, zlib)"
"$CLANG" -c -O1 -I"$W/src/shim" \
  "$W/src/shim/img_png.c" -o "$W/dist/img_png.o"

"$AR" rcs "$W/dist/libnoui-shim.a" "$W/dist/shim.o" "$W/dist/img_png.o"

echo "[shim] 编译 SDL_GPU 后端 (gpu_shim.c)"
"$CLANG" -c -O1 -I"$SDL_I" \
  "$W/src/shim/gpu_shim.c" -o "$W/dist/gpu_shim.o"
"$AR" rcs "$W/dist/libnoui-gpu-shim.a" "$W/dist/gpu_shim.o"

echo "[shim] 完成：libnoui-shim.a / libnoui-gpu-shim.a"
