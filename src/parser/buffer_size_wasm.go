//go:build wasm

package parser

// tokenBufferSize 是定长 ring buffer 的容量（wasm 构建）。
// wasm 内存受限，容量较 native（1024）更小；解析器任意 save/restore 窗口内的
// (前瞻距离 + 回溯距离) + 1 不得超过此值，否则触发 E_BUFFER_UNDERFLOW 结构化错误。
const tokenBufferSize = 256
