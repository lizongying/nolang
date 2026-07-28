//go:build !wasm

package parser

// tokenBufferSize 是定长 ring buffer 的容量（native 构建）。
// 解析器任意 save/restore 窗口内的 (前瞻距离 + 回溯距离) + 1 不得超过此值。
const tokenBufferSize = 1024
