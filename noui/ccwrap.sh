#!/bin/sh
# -cc 链接包装器：注入 sdl3/sdl3_ttf 与 noui shim 的 -I/-L 路径
# 路径基于脚本自身位置计算，移动工程目录后无需修改
W=$(cd "$(dirname "$0")" && pwd)
exec /opt/homebrew/opt/llvm/bin/clang \
  -I"$W/src/shim" \
  -I/opt/homebrew/opt/sdl3/include \
  -I/opt/homebrew/opt/sdl3_ttf/include \
  -L"$W/dist" \
  -L/opt/homebrew/opt/sdl3/lib \
  -L/opt/homebrew/opt/sdl3_ttf/lib \
  "$@"
