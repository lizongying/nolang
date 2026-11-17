package builtin

import "github.com/lizongying/nolang/parser"

// chan.go — `chan[T]` 的內建方法。
//
// 三個方法都掛在規範化接收者 `chan` 上（不是 `chan[i64]`）：通道控制塊自己
// 記著元素寬度，所以 `.len()` / `.cap()` / `.close()` 完全與 T 無關，一份
// 註冊服務所有元素型別。MIR 側把 `chan[T].m` 歸一成 `chan.m`
// （mir.canonSliceRecv），因此這裡的 MethodName 必須是帶前綴的全名。
//
// 發送 `ch <- v` 與接收 `v <- ch:` 是語句/迴圈語法，不經過內建表。
//
// 三者的實際 IR 由 MIR codegen 手寫（emitBuiltinForward 的 chan-* 分派），
// 呼叫運行期函數 @nolang_chan_len / @nolang_chan_cap / @nolang_chan_close。
func init() {
	// chan.len: 當前排隊元素數（不是容量）。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverChan,
		MethodName:   "chan.len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Number of elements currently queued in the channel",
		ForwardFunc:  "chan-len",
	})

	// chan.cap: 已分配的環形緩衝容量。初值來自 with-cap(n)，之後 send 滿了
	// 就倍增（首次 4）。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverChan,
		MethodName:   "chan.cap",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Capacity of the channel's ring buffer (grows on demand)",
		ForwardFunc:  "chan-cap",
	})

	// chan.close: 冪等關閉。關閉後 send 中止進程，recv 在排空後給 nil。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverChan,
		MethodName:   "chan.close",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Close the channel (idempotent; send after close aborts)",
		ForwardFunc:  "chan-close",
	})
}
