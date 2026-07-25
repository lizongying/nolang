package wasm

import (
	"bytes"
	"fmt"
)

// Writer 是低階的 WebAssembly 二進制寫入器，封裝一個 bytes.Buffer 並提供
// WASM binary format 所需的原語（LEB128、name、vec 等）。
//
// 參考規格：https://webassembly.github.io/spec/core/binary/
type Writer struct {
	buf bytes.Buffer
}

// NewWriter 建立一個空的 Writer。
func NewWriter() *Writer {
	return &Writer{}
}

// WriteByte 寫入單一位元組。
func (w *Writer) WriteByte(b byte) {
	w.buf.WriteByte(b)
}

// WriteBytes 寫入一段原始位元組。
func (w *Writer) WriteBytes(bs []byte) {
	w.buf.Write(bs)
}

// WriteU32 以小端序寫入 4 位元組。
func (w *Writer) WriteU32(v uint32) {
	w.buf.Write([]byte{
		byte(v),
		byte(v >> 8),
		byte(v >> 16),
		byte(v >> 24),
	})
}

// WriteU64 以小端序寫入 8 位元組。
func (w *Writer) WriteU64(v uint64) {
	w.buf.Write([]byte{
		byte(v),
		byte(v >> 8),
		byte(v >> 16),
		byte(v >> 24),
		byte(v >> 32),
		byte(v >> 40),
		byte(v >> 48),
		byte(v >> 56),
	})
}

// WriteLEB128 寫入無號 LEB128 編碼的 uint32。
// 規格：https://webassembly.github.io/spec/core/binary/values.html#binary-int
func (w *Writer) WriteLEB128(v uint32) error {
	more := true
	for more {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		} else {
			more = false
		}
		w.buf.WriteByte(b)
	}
	return nil
}

// WriteSLEB128 寫入有號 LEB128 編碼的 int64。
func (w *Writer) WriteSLEB128(v int64) error {
	more := true
	for more {
		b := byte(v & 0x7F)
		v >>= 7
		// 若符號位元已正確延展則結束。
		if (b&0x40 != 0 && v == -1) || (b&0x40 == 0 && v == 0) {
			more = false
		} else {
			b |= 0x80
		}
		w.buf.WriteByte(b)
	}
	return nil
}

// WriteName 寫入 WASM name：以 LEB128 表示的位元組長度後接 UTF-8 內容。
func (w *Writer) WriteName(s string) error {
	if err := w.WriteLEB128(uint32(len(s))); err != nil {
		return err
	}
	w.buf.WriteString(s)
	return nil
}

// WriteVec 寫入一個 vec：先寫入元素計數（LEB128），再依序由 writeElem
// 寫入每個元素。對應規格中的 vec(T)。
func (w *Writer) WriteVec(count uint32, writeElem func()) error {
	if err := w.WriteLEB128(count); err != nil {
		return err
	}
	for i := uint32(0); i < count; i++ {
		writeElem()
	}
	return nil
}

// Bytes 回傳目前累積的全部位元組（不影響內部緩衝）。
func (w *Writer) Bytes() []byte {
	return w.buf.Bytes()
}

// Len 回傳目前累積的位元組數。
func (w *Writer) Len() int {
	return w.buf.Len()
}

// Reset 清空內部緩衝。
func (w *Writer) Reset() {
	w.buf.Reset()
}

// WriteString 是 WriteName 的別名，保留給需要純位元組序列的场景。
// 注意：WASM 中的 name 必須以長度前綴編碼，請優先使用 WriteName。
func (w *Writer) WriteString(s string) {
	if err := w.WriteName(s); err != nil {
		// WriteName 內部只可能因 LEB128 失敗（實際不會），這裡以防萬一。
		panic(fmt.Sprintf("wasm: WriteName failed: %v", err))
	}
}
