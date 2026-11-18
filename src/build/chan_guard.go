package build

// chan_guard.go — JS / Direct-WASM 後端對 `chan[T]` 的明確拒絕。
//
// 為什麼需要這道閘：`chan` 的實作是 MIR 環形緩衝 + LLVM 執行期函式
// （src/mir/codegen_chan.go）。JS 與 Direct-WASM 兩個後端是**AST 直譯式**
// generator（build/js、build/wasm），不經過 MIR，遇到通道節點時只會落進各自
// 的 `default:` 分支：
//
//   - build/js/stmt.go：`default: return "/* unhandled stmt */"`
//   - build/wasm/codegen.go：`default:` 直接跳過
//
// 兩者都是**靜默**的：編譯成功、輸出存在，但通道語句整段消失或變成註解，
// 程式行為與源碼無關。這比報錯糟得多，所以在這裡一次性攔死。
//
// 判據取自 AST：通道只有三種表徵——ChanType（宣告/參數/返回型別）、
// ChanSendStatement（`ch <- v`）、IterationExpr.IsChan（兩種接收迴圈，含
// `<- ch:` 脫糖後的 ForStatement）。走查用反射（與 build/astaudit 同手法），
// 這樣函式字面量、巢狀型別裡出現的通道也會被發現，不必逐節點補 case。

import (
	"reflect"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// rejectChanForNonLLVM 掃描 program，遇到任何通道節點即回傳「後端不支援」的
// 明確錯誤（含後端名與行列）。無通道時回傳 nil，行為與加入此閘之前逐位元組相同。
func rejectChanForNonLLVM(program *parser.Program, backend string) error {
	if program == nil {
		return nil
	}
	if pos, what, found := findChanNode(reflect.ValueOf(program), 0); found {
		return i18n.Errorf("chbkn001",
			"line %d, column %d: %s is not supported by the %s backend; channels need the LLVM backend (drop --js / --wasm, or use `no build`)",
			pos.Line, pos.Column, what, backend)
	}
	return nil
}

// findChanNode 遞歸走查 AST，回傳第一個通道節點的位置與說明。
// depth 上限只是防禦性的環路保護（AST 是樹，正常遠小於此值）。
func findChanNode(v reflect.Value, depth int) (lexer.Position, string, bool) {
	if depth > 64 || !v.IsValid() {
		return lexer.Position{}, "", false
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return lexer.Position{}, "", false
		}
		if n, ok := v.Interface().(parser.Node); ok && describeChanNode(n) != "" {
			return n.Pos(), describeChanNode(n), true
		}
		return findChanNode(v.Elem(), depth+1)
	case reflect.Struct:
		// ChanType 是值型別節點（`Elem` 為 Type 介面），指標分支看不到它。
		if v.CanAddr() {
			if n, ok := v.Addr().Interface().(parser.Node); ok && describeChanNode(n) != "" {
				return n.Pos(), describeChanNode(n), true
			}
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanInterface() {
				continue
			}
			if pos, what, ok := findChanNode(f, depth+1); ok {
				return pos, what, true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if pos, what, ok := findChanNode(v.Index(i), depth+1); ok {
				return pos, what, true
			}
		}
	}
	return lexer.Position{}, "", false
}

// describeChanNode 把通道節點描述成人話；非通道節點回傳空字串。
func describeChanNode(n parser.Node) string {
	switch t := n.(type) {
	case *parser.ChanType:
		return "channel type `chan[" + t.Elem.String() + "]`"
	case *parser.ChanSendStatement:
		return "channel send `ch <- v`"
	case *parser.IterationExpr:
		if t.IsChan {
			return "channel receive loop `v <- ch: { ... }`"
		}
	}
	return ""
}
