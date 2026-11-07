package checker

import (
	"github.com/lizongying/nolang/builtin"
)

// IntConvMethodName 返回「源整數型別 → 目標整數型別」的約定轉換方法名
// `<srcRaw>.to-<dstRaw>`，當且僅當它存在且契約精確成立（接收者 == srcRaw、
// 唯一返回 == dstRaw、非 option）。
//
// 用途：賦值/宣告的目標型別已經寫明時，命名轉換方法的存在就是「使用者明確
// 接受這次轉換」的證據，因此可以放行本會被 `15w45dqk` 攔下的窄化
// （`v i32 = <i64 值>` 命中 `i64.to-i32` 就按低 32 位截斷）。
//
// 兩檔查找，順序與 mir 的 intConvCallee 保持一致：
//  1. funcNames —— 本 program 內使用者可見的真方法；
//  2. builtin.FindBuiltinMethod —— `#{buildin}` 樁在合併期被剝離，不在
//     funcNames 裡，其真實發射在 Go 側註冊表。
//
// 只給「宣告/賦值」用：函式實參、欄位/索引賦值、多返回值賦值一律不查，
// 因為 MIR 不會在那些位置插入轉換，放行等於誤編譯。
func IntConvMethodName(srcRaw, dstRaw string, funcNames map[string]bool) string {
	if srcRaw == "" || dstRaw == "" || srcRaw == dstRaw {
		return ""
	}
	if _, _, ok := intTypeRange(srcRaw); !ok {
		return ""
	}
	if _, _, ok := intTypeRange(dstRaw); !ok {
		return ""
	}
	callee := srcRaw + ".to-" + dstRaw
	if funcNames[callee] {
		return callee
	}
	if bm := builtin.FindBuiltinMethod(callee); bm != nil && bm.LLVMConv != nil &&
		len(bm.Params) == 0 && len(bm.Return) == 1 &&
		bm.ReceiverType.String() == srcRaw && bm.Return[0].String() == dstRaw {
		return callee
	}
	return ""
}
