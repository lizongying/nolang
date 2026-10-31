// type.go — 类型解析与类型名判定：类型别名、类型表达式、buildType、类型推断辅助。
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/ty"
)

// tyPool is the parser's structural pool. The parser classifies a type from its
// SPELLING only (a struct name and a scalar alias look identical at that point —
// the registry is not built yet), so a resolver-free pool is exactly right:
// unresolved names stay Named and keep their spelling.
var tyPool = ty.NewPool(nil)

// TyOf converts a surface type node into the structural type, recursively and
// WITHOUT a string round-trip (the plan's "Parser 語法節點 → ty" boundary).
// It is the bridge the checker and MIR use when they hold an AST type node
// rather than raw text. Returns nil for a nil type.
func TyOf(t Type) *ty.Type {
	if t == nil {
		return nil
	}
	switch typ := t.(type) {
	case *NamedType:
		return tyPool.Parse(typ.Value)
	case *SliceType:
		return tyPool.MakeOn("[]"+rawOf(TyOf(typ.Elem)), ty.Type{Kind: ty.Slice, Elem: TyOf(typ.Elem)})
	case *ArrayType:
		lenText, n := arraySizeOf(typ.Size)
		return tyPool.MakeOn("["+lenText+"]"+rawOf(TyOf(typ.Elem)), ty.Type{
			Kind: ty.Array, Elem: TyOf(typ.Elem), Len: n, LenText: lenText,
		})
	case *NullableType:
		return tyPool.MakeOn("?"+rawOf(TyOf(typ.Type)), ty.Type{Kind: ty.Option, Elem: TyOf(typ.Type)})
	case *ViewType:
		return tyPool.MakeOn("&"+rawOf(TyOf(typ.Type)), ty.Type{Kind: ty.View, Elem: TyOf(typ.Type)})
	case *PointerType:
		return tyPool.MakeOn("ptr "+rawOf(TyOf(typ.Type)), ty.Type{Kind: ty.Ptr, Elem: TyOf(typ.Type)})
	case *MapType:
		return tyPool.MakeOn("["+rawOf(TyOf(typ.Key))+"]"+rawOf(TyOf(typ.Value)), ty.Type{
			Kind: ty.Map, Key: TyOf(typ.Key), Elem: TyOf(typ.Value),
		})
	case *UnionType:
		members := make([]*ty.Type, 0, len(typ.Types))
		parts := make([]string, 0, len(typ.Types))
		for _, m := range typ.Types {
			mt := TyOf(m)
			members = append(members, mt)
			parts = append(parts, rawOf(mt))
		}
		return tyPool.MakeOn(strings.Join(parts, "|"), ty.Type{Kind: ty.Union, Members: members})
	}
	return nil
}

// rawOf is the spelling of a structural type, "" for nil.
func rawOf(t *ty.Type) string {
	if t == nil {
		return ""
	}
	return t.Raw()
}

// arraySizeOf renders an array size expression the way the AST printer does and
// reports the literal length (-1 when the size is symbolic or inferred).
func arraySizeOf(e Expression) (string, int64) {
	switch sz := e.(type) {
	case *IntegerLiteral:
		return strconv.FormatInt(sz.Value, 10), sz.Value
	case *Identifier:
		return sz.Value, -1
	}
	return "?", -1
}

// resolveReceiverType 解析方法調用接收者的型別，用於推斷方法返回型別。
// 支援：局部變數 (Identifier)、self 欄位 (DotExpression{self, field})、
// 以及 self.field.subfield (嵌套 DotExpression{DotExpression{self, field}, subfield})
func (p *Parser) resolveReceiverType(receiver Expression) string {
	if ident, ok := receiver.(*Identifier); ok {
		// 必須優先查函數作用域表（FuncVarType，內部自帶全域 VarTypes 回退）：
		// setVarType 已不再把函數參數/區域變數寫入全域 VarTypes，若這裡只查
		// 全域快照，body 解析期將找不到接收者型別（如 `path.split('.')` 的
		// `path`），導致方法返回型別推斷靜默丟失。
		if t, ok := p.sem.FuncVarType(p.curFuncName, ident.Value); ok && t != "" {
			return strings.TrimPrefix(t, "?")
		}
		// self 欄位：在方法體中，`.field` 語法會解析為裸識別符 `field`，
		// 其型別應從所屬 struct 的欄位表中查詢（而非區域變數表）。
		// 例如 sse-client.reconnect 中的 `.tls-c.recv`：receiver 為裸識別符
		// `tls-c`，其型別是 sse-client 結構的 tls-c 欄位（tls.conn）。
		if len(p.methodStructStack) > 0 {
			structName := p.methodStructStack[len(p.methodStructStack)-1]
			if fields, ok := p.structFields[structName]; ok {
				if fieldType, ok := fields[ident.Value]; ok {
					return strings.TrimPrefix(fieldType, "?")
				}
			}
		}
		// self in method body: resolve via methodStructStack.
		// self is added to def.Parameters after parseFunctionBody,
		// so varDeclTypes does not have it during body parsing.
		// This allows inferTypeFromCallExpr to resolve self.method() calls
		// (e.g., val = .get(key) in str-map.contains -> val inferred as ?str).
		if ident.Value == "self" && len(p.methodStructStack) > 0 {
			return p.methodStructStack[len(p.methodStructStack)-1]
		}
	}
	if dot, ok := receiver.(*DotExpression); ok {
		// self.field → 查詢 struct 欄位型別
		if selfIdent, ok := dot.Receiver.(*Identifier); ok && selfIdent.Value == "self" {
			if len(p.methodStructStack) > 0 {
				structName := p.methodStructStack[len(p.methodStructStack)-1]
				if fields, ok := p.structFields[structName]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			} else {
				// 自由函數中沒有 self：`.field` 視為對同名區域變數的存取
				// （例如 sse-connect 中的 `tls-c tls.conn` 區域變數，`.tls-c.recv`
				// 應解析為 tls.conn.recv）。此回退讓 inferTypeFromCallExpr 能推斷
				// 方法呼叫的回傳型別，避免 match 的 it 綁定型別缺失而誤報。
				// 優先查函數作用域表（含全域回退），與 Identifier 分支同理。
				if t, ok := p.sem.FuncVarType(p.curFuncName, dot.Property); ok && t != "" {
					return strings.TrimPrefix(t, "?")
				}
			}
		}
		// self.field.subfield → 遞迴解析 receiver 型別，再查其 struct 欄位
		if innerDot, ok := dot.Receiver.(*DotExpression); ok {
			innerType := p.resolveReceiverType(innerDot)
			if innerType != "" {
				if fields, ok := p.structFields[innerType]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			}
		}
	}
	// CallExpression receiver (e.g., fs.read-file(path).to-str()):
	// recursively infer the return type of the inner call, then use it
	// as the receiver type for the outer method call.
	// 剝 `?`：其餘分支（Identifier / Dot / Grouped）查方法簽名時一律以
	// payload 型別作鍵，option 接收者查 `?T.method` 必然落空。鏈式運算子
	// `a + b + c` 的外層探測正是以 `a._add(b)` 呼叫作接收者，若保留
	// `?bigint`（filterInferableType 對 option 原樣放行），鍵 `?bigint._add`
	// 查不到簽名 → 放棄改寫 → 退回原生 codegen 對 struct emit add → 崩潰。
	if innerCall, ok := receiver.(*CallExpression); ok {
		return strings.TrimPrefix(p.inferTypeFromCallExpr(innerCall), "?")
	}
	// GroupedExpression receiver（如 `(a + b).method()`）：剥括號後遞迴解析。
	if grouped, ok := receiver.(*GroupedExpression); ok {
		return p.resolveReceiverType(grouped.Expression)
	}
	// InfixExpression receiver（鏈式運算子 `a + b + c`、`(a + b) * c`）：
	// 左運算元本身是可能派發為運算符重載的中綴式。若它是運算符重載（左型別
	// 定義了 `_op`），其生效型別即該運算符重載方法的 payload 回傳型別；否則
	// 回傳空字串（純內建數值中綴，不作方法接收者）。不依賴 post-order 是否已
	// 把內層重寫為 CallExpression——兩條路徑在此均能解析出一致的结果型別。
	if innerInf, ok := receiver.(*InfixExpression); ok {
		// 這裡就是「巢狀運算子的接收者位置」——接收者必須
		// 以 payload 型別作鍵查簽名，故只在這一處剝 `?`（與 CallExpression
		// 分支 155 行同形）。運算符重載方法本身回傳書寫的 `?T`。
		return strings.TrimPrefix(p.OperatorOverloadReturnType(innerInf), "?")
	}
	return ""
}

// inferTypeFromCallExpr 嘗試從函數/方法調用推斷返回型別。
// 推斷 option 型別（?type）和結構體型別（如 bufio.reader），避免泛型/聯合型別的特化問題。
// 返回空字串表示無法推斷。
//
// 查找規則：
//   - 同檔案函數（裸名 fn）查 p.funcSignatures
//   - 模組函數（mod.fn）查 p.funcSignatures
//   - 結構體方法（receiver.method）查 p.methodSignatures
//     本模組方法以 "struct.method" 為鍵，跨模組 std 方法以 "module.struct.method" 為鍵。
//
// currentModuleName 由目前原始檔名推出所屬模組名（".../std/fs.no" → "fs"）。
//
// 只用於裸名呼叫在 funcSignatures 未命中時的「module.fn」回退查表；對使用者
// 檔案（如 /tmp/x.no → "x"）該鍵不存在，自然落空，不會誤配到別的模組。
func (p *Parser) currentModuleName() string {
	if p == nil || p.Filename == "" {
		return ""
	}
	base := p.Filename
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '\\'); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".no")
}

func (p *Parser) inferTypeFromCallExpr(call *CallExpression) string {
	return p.filterInferableType(p.returnTypeFromCallExpr(call))
}

// returnTypeFromCallExpr 回報呼叫的回傳型別原文（不做 filterInferableType 過濾）。
//
// inferTypeFromCallExpr 會把 i64/i32/f64 等基本型別濾成 ""（那是為「可安全推斷的
// 接收者型別」而設），但捕獲 pass 需要真實回傳型別來決定 option 內部型別 T
// （`a = g(b / c)` 的 T 就是 g 的回傳型別），故另設此不過濾的入口。
//
// 查找規則同 inferTypeFromCallExpr。
func (p *Parser) returnTypeFromCallExpr(call *CallExpression) string {
	if call.Function == nil {
		return ""
	}
	if ident, ok := call.Function.(*Identifier); ok {
		// 同檔案函數調用（如 foo()）：以裸名查找
		fnName := ident.Value
		if p.funcSignatures != nil {
			if rets, ok := p.funcSignatures[fnName]; ok && len(rets) == 1 {
				return rets[0]
			}
		}
		// 裸名未命中時，退回「目前模組」限定鍵（module.fn）。
		//
		// 情境：std 模組內部以**非限定**方式呼叫同模組的內建/函數——例如
		// src/std/fs.no 的 `file.read-bytes` 寫 `size ?= fstat-size(.fd)`。
		// 簽名表（ExternFuncSigs / CollectStdModuleSignatures）統一以
		// "module.fn"（如 "fs.fstat-size"）註冊；S1 翻轉後 std 以 surface
		// 形態從 blob 載入、本地宣告不再寫進 funcSignatures，裸名因而查不到。
		//
		// 後果不只是「推斷為空」：`?=` 的 H0 直出（lowerUnwrapAssign）拿不到
		// 回傳型別就設不出 tmpType，tmp 變 void → MIR 取值為 0／解析失敗，
		// 表現為 file.read-bytes 對非空檔也回 nil（§13.2 的 mass-noop 殘根）。
		if mod := p.currentModuleName(); mod != "" && p.funcSignatures != nil {
			if rets, ok := p.funcSignatures[mod+"."+fnName]; ok && len(rets) == 1 {
				return rets[0]
			}
		}
		return ""
	}
	if dot, ok := call.Function.(*DotExpression); ok {
		receiverType := p.resolveReceiverType(dot.Receiver)
		if receiverType != "" {
			// Map types are stored as [K]V in varDeclTypes but function
			// signatures use hashmap-K-V. Convert for lookup.
			if strings.HasPrefix(receiverType, "[") {
				if idx := strings.Index(receiverType, "]"); idx > 0 {
					keyPart := receiverType[1:idx]
					valPart := receiverType[idx+1:]
					receiverType = "hashmap-" + keyPart + "-" + SanitizeLLVMTypeName(valPart)
				}
			}
			// 結構體方法：查 methodSignatures
			if p.methodSignatures != nil {
				// receiverType 可能是裸名（如 conn，本模組方法）或
				// "module.struct"（如 tls.conn，跨模組 std 方法），
				// 兩種鍵均以 receiverType + "." + dot.Property 查找即可。
				key := receiverType + "." + dot.Property
				if rets, ok := p.methodSignatures[key]; ok && len(rets) == 1 {
					return rets[0]
				}
			}
			// array/slice type methods (如 []t.len()) 仍存於 funcSignatures
			if p.funcSignatures != nil {
				fnName := receiverType + "." + dot.Property
				if rets, ok := p.funcSignatures[fnName]; ok && len(rets) == 1 {
					return rets[0]
				}
			}
			return ""
		}
		// Module-path receiver（如 http.do-req(...)）：receiver 是模組
		// 識別符而非變數。std 模組函數統一以 "module.fn" 為鍵註冊。
		modName := ""
		fullPath := "" // 完整點分路徑（如 bufio.reader.init）
		if ident, ok := dot.Receiver.(*Identifier); ok {
			modName = ident.Value
			fullPath = modName + "." + dot.Property
		} else if innerDot, ok := dot.Receiver.(*DotExpression); ok {
			// Multi-level module path (e.g. encoding.base64.decode):
			// use the last path segment as the module short name.
			modName = innerDot.Property
			// 同時構建完整路徑（如 bufio.reader.init），
			// 用於查找 stdsig 中的 "module.struct.method" 鍵。
			if innerIdent, ok := innerDot.Receiver.(*Identifier); ok {
				fullPath = innerIdent.Value + "." + innerDot.Property + "." + dot.Property
			}
		}
		// 先用完整路徑查找（如 bufio.reader.init），
		// 再回退到 "module.fn" 格式（如 reader.init）。
		// 方法簽名（methodSigs）以 "module.struct.method" 為鍵存儲，
		// 函數簽名（funcSigs）以 "module.fn" 為鍵存儲。
		if fullPath != "" {
			// 先查方法簽名（如 bufio.reader.init 在 methodSigs 中）
			if p.methodSignatures != nil {
				if rets, ok := p.methodSignatures[fullPath]; ok && len(rets) == 1 {
					return rets[0]
				}
			}
			if p.funcSignatures != nil {
				if rets, ok := p.funcSignatures[fullPath]; ok && len(rets) == 1 {
					return rets[0]
				}
			}
		}
		if modName != "" && p.funcSignatures != nil {
			if rets, ok := p.funcSignatures[modName+"."+dot.Property]; ok && len(rets) == 1 {
				return rets[0]
			}
		}
		return ""
	}
	return ""
}

// multiReturnTypesFromCallExpr 回報「多返回值呼叫」宣告的全部回傳型別（按宣告
// 順序）；單返回值、零返回值或查不到時回 nil。
//
// 為什麼不複用 returnTypeFromCallExpr：那條路在每個查表點都要求 `len(rets) == 1`
// （單值才是「這個呼叫的回傳型別」），多返回值呼叫一律落空回 ""。`q, r =
// a.div-mod(b)` 的兩個目標需要的恰恰是那份**有序列表**，且多值時不存在「再退一層
// 查表」的語義，所以這裡只看 len >= 2，不做 strip、不做 filterInferableType。
//
// 鍵順序與 returnTypeFromCallExpr 逐字對稱（receiverType 裸名／module.struct、
// 完整點分路徑、module.fn 三級回退），避免「單值查得到、多值查不到」的不對稱。
func (p *Parser) multiReturnTypesFromCallExpr(call *CallExpression) []string {
	if call == nil || call.Function == nil || p == nil {
		return nil
	}
	multi := func(rets []string) []string {
		if len(rets) < 2 {
			return nil
		}
		return rets
	}
	if ident, ok := call.Function.(*Identifier); ok {
		if p.funcSignatures != nil {
			if rets := multi(p.funcSignatures[ident.Value]); rets != nil {
				return rets
			}
			if mod := p.currentModuleName(); mod != "" {
				if rets := multi(p.funcSignatures[mod+"."+ident.Value]); rets != nil {
					return rets
				}
			}
		}
		return nil
	}
	dot, ok := call.Function.(*DotExpression)
	if !ok {
		return nil
	}
	if receiverType := p.resolveReceiverType(dot.Receiver); receiverType != "" {
		if strings.HasPrefix(receiverType, "[") {
			if idx := strings.Index(receiverType, "]"); idx > 0 {
				receiverType = "hashmap-" + receiverType[1:idx] + "-" +
					SanitizeLLVMTypeName(receiverType[idx+1:])
			}
		}
		key := receiverType + "." + dot.Property
		if p.methodSignatures != nil {
			if rets := multi(p.methodSignatures[key]); rets != nil {
				return rets
			}
		}
		if p.funcSignatures != nil {
			if rets := multi(p.funcSignatures[key]); rets != nil {
				return rets
			}
		}
		return nil
	}
	// 模組限定形態（`bufio.reader.init(...)` / `mod.fn(...)`）
	modName, fullPath := "", ""
	if ident, ok := dot.Receiver.(*Identifier); ok {
		modName = ident.Value
		fullPath = modName + "." + dot.Property
	} else if innerDot, ok := dot.Receiver.(*DotExpression); ok {
		modName = innerDot.Property
		if innerIdent, ok := innerDot.Receiver.(*Identifier); ok {
			fullPath = innerIdent.Value + "." + innerDot.Property + "." + dot.Property
		}
	}
	if fullPath != "" {
		if p.methodSignatures != nil {
			if rets := multi(p.methodSignatures[fullPath]); rets != nil {
				return rets
			}
		}
		if p.funcSignatures != nil {
			if rets := multi(p.funcSignatures[fullPath]); rets != nil {
				return rets
			}
		}
	}
	if modName != "" && p.funcSignatures != nil {
		if rets := multi(p.funcSignatures[modName+"."+dot.Property]); rets != nil {
			return rets
		}
	}
	return nil
}

// filterInferableType 判斷返回型別是否可安全地從函數/方法調用推斷。
// 可推斷的型別：
//   - option 型別（?type）：match desugar 依賴它生成正確的 it 型別窄化
//   - 結構體型別（如 bufio.reader）：使 codegen 能正確分配結構體記憶體
//   - str 型別：方法調用（如 deps.split）依賴正確的 receiver 型別
//   - 切片型別（[]byte, []str 等）：同上
//
// 不可推斷的型別：整數和浮點數型別，因為整數字面量已有專用推斷路徑。
func (p *Parser) filterInferableType(retType string) string {
	if retType == "" {
		return ""
	}
	// option 型別：始終推斷
	if strings.HasPrefix(retType, "?") {
		return retType
	}
	// 排除整數和浮點數基本型別
	switch retType {
	case "i64", "i32", "i16", "i8", "u64", "u32", "u16", "u8",
		"f64", "f32", "bool", "byte", "char", "fd":
		return ""
	}
	// 排除聯合型別（含 | ）
	if strings.Contains(retType, "|") {
		return ""
	}
	// str 和切片型別（[]byte, []str 等）可安全推斷，
	// 因為方法調用依賴正確的 receiver 型別（如 raw.split, line.trim）。
	// parseLetStatement 僅在變數未宣告時才推斷，不會覆蓋已有的顯式型別。
	return retType
}

// containerElemType returns the element type of a container type string used
// for arr/vec/slice indexing, or "" if lt is not an indexable container.
// str/txt ARE included since D6 (their s[i] yields Option<char>); see below.
// The result doubles as the "this base is option-indexed" predicate, so adding
// a kind here changes isSafeIndexBase / maybeCaptureOptionAssign as one.
func containerElemType(lt string) string {
	if lt == "" {
		return ""
	}
	// D6（2026-10-13）：str/txt 索引一样产生 option —— `s[i]` 越界 → `nil`，
	// 成功 → `ok(char)`。此前这里返回 ""，注释写着 "char, not option-indexed"，
	// 是 D1-b「先测量再分期」留下的豁免。§7.1 ③ 的测量结果否掉了分期：
	// src/std 11 处、tests 34 处需要迁移（几乎全是同一个 x25519 十六进制解析块
	// 在 16 个文件里的副本），规模可接受，故与主体同期落地。
	//
	// 注意两者的元素型别口径必须与 hir2mir 的 elemTypeOfType 一致：那里对 str 与
	// txt 都回 `char`（尽管 txt 在历史上曾写成 i8），不统一会让
	// `x ?char = s[i]` 的舱体尺寸与实际读取错位。
	if lt == "str" || lt == "txt" {
		return "char"
	}
	if strings.HasPrefix(lt, "[]") {
		return lt[2:]
	}
	if strings.HasPrefix(lt, "[") {
		if i := strings.Index(lt, "]"); i >= 0 && i+1 < len(lt) {
			return lt[i+1:]
		}
	}
	if strings.HasPrefix(lt, "vec[") && strings.HasSuffix(lt, "]") {
		return lt[len("vec[") : len(lt)-1]
	}
	return ""
}

// ContainerElemType 是 containerElemType 的導出版，供 checker 套件判定容器元素型別
// （區分 arr/vec/slice 與 str/txt、struct field 等不會產生 option 索引的基底）。
func ContainerElemType(lt string) string { return containerElemType(lt) }

// inferIndexElemType returns the element type of an IndexExpression's base
// container, using the parser-resolved variable types (available at lowering
// time). Returns "" if the element type cannot be determined.
func (p *Parser) inferIndexElemType(idx *IndexExpression) string {
	if idx == nil || idx.Left == nil {
		return ""
	}
	lt := ""
	// 必須用函數作用域查詢（FuncVarType）以避免同名區域變數的跨函數型別污染：
	// p.sem.VarTypes 是全域快照，其他函數的 `arr []byte` 會覆寫本函數的
	// `arr []i64`，導致元素型別被誤判為 byte（it 綁定成 i8 而崩潰）。
	lookupVarType := func(name string) string {
		if t, ok := p.sem.FuncVarType(p.curFuncName, name); ok && t != "" {
			return t
		}
		if t, ok := p.sem.VarTypes[name]; ok {
			return t.Raw()
		}
		return ""
	}
	switch b := idx.Left.(type) {
	case *Identifier:
		// 優先查詢「安全索引專用」本地型別表（lower 預掃描所得），
		// 避免跨函數型別污染（見 isSafeIndexBase 註解）。
		if t, ok := p.idxLocalTypes[p.curFuncName][b.Value]; ok && t.Raw() != "" {
			lt = ty.StripOptionRaw(t.Raw())
		} else if t := lookupVarType(b.Value); t != "" {
			lt = ty.StripOptionRaw(t)
		}
	case *DotExpression:
		// receiver.field[i] — resolve the struct field's container type.
		rt := p.resolveReceiverType(b.Receiver)
		if rt != "" {
			if fields, ok := p.structFields[rt]; ok {
				if ft, ok := fields[b.Property]; ok {
					lt = strings.TrimPrefix(ft, "?")
				}
			}
		}
	}
	if lt == "" {
		return ""
	}
	return containerElemType(lt)
}

// isSafeIndexBase reports whether an IndexExpression indexes a direct
// arr/vec/slice variable (which should become a safe Option-returning access),
// as opposed to str/txt (which keeps returning char) or a struct field
// (receiver.field[i], which keeps the existing bounds_check path).
//
// NOTE: only direct-variable bases are supported by the safe-index codegen
// (generateOptionAssign's IndexExpression case). Struct-field indexing is
// intentionally excluded here so it is never desugared into the safe form
// (which the codegen does not yet handle for DotExpression bases).
//
// D7 最终定案（2026-10-15 两层分离）：可证明越界安全的读取（定长数组 + 界内下标，
// 由 bounds.go 静态分析得出）在**用户侧仍一律产生 option**——`x = arr[i]` 的 x 仍是
// `?elem`，必须 match / `?=` / `_ =` 解包。「内部消除」（codegen/MIR 在可证明安全时
// 省略 nil 检查、直接走 ok 分支）只是不可见的代码生成优化，不改变用户可见类型。
// 故此处**不再**依据 p.inBoundsIdx 早退：in-bounds 读取照常返回 true、照常降级为
// option。bounds.go 分析改由 codegen/MIR 消费（内部消除）与 rngidx 等诊断降噪使用。
func (p *Parser) isSafeIndexBase(idx *IndexExpression) bool {
	if idx == nil || idx.Left == nil {
		return false
	}
	ident, ok := idx.Left.(*Identifier)
	if !ok {
		return false
	}
	lt := ""
	// 優先查詢「安全索引專用」本地型別表（lower 預掃描區域變數容器型別所得，
	// 如 `av = a.to-vec()` → []i64），其優先級高於全域快照，避免跨函數污染。
	if t, ok := p.idxLocalTypes[p.curFuncName][ident.Value]; ok && t.Raw() != "" {
		lt = ty.StripOptionRaw(t.Raw())
	} else if t, ok := p.sem.FuncVarType(p.curFuncName, ident.Value); ok && t != "" {
		lt = ty.StripOptionRaw(t)
	} else if t, ok := p.sem.VarTypes[ident.Value]; ok {
		lt = ty.StripOptionRaw(t.Raw())
	}
	return containerElemType(lt) != ""
}

// mapIndexBaseType returns the map type text of an IndexExpression's base when
// that base is a map-typed variable (`m [str]i64` -> "[str]i64"), else "".
//
// It exists because isSafeIndexBase CANNOT answer this: containerElemType treats
// a map type as an array (`[str]i64` -> elem "i64"), so a map base looks exactly
// like a positional container there. That conflation is harmless when typing a
// read (both yield an option) but fatal for `?=`, whose write-back assumes the
// element lives at a POSITION in the container - see lowerUnwrapAssign.
//
// Type lookup order mirrors isSafeIndexBase (per-function table first, to avoid
// cross-function pollution from the global snapshot).
func (p *Parser) mapIndexBaseType(idx *IndexExpression) string {
	if idx == nil || idx.Left == nil {
		return ""
	}
	ident, ok := idx.Left.(*Identifier)
	if !ok {
		return ""
	}
	lt := ""
	if t, ok := p.idxLocalTypes[p.curFuncName][ident.Value]; ok && t.Raw() != "" {
		lt = ty.StripOptionRaw(t.Raw())
	} else if t, ok := p.sem.FuncVarType(p.curFuncName, ident.Value); ok && t != "" {
		lt = ty.StripOptionRaw(t)
	} else if t, ok := p.sem.VarTypes[ident.Value]; ok {
		lt = ty.StripOptionRaw(t.Raw())
	}
	if IsMapTypeString(lt) {
		return lt
	}
	return ""
}

// isTypeName checks if the given literal is a known type name.
// Used to support concise declarations like `i64` on its own line.
func isTypeName(literal string) bool {
	switch literal {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
		"f32", "f64",
		"byte", "bool", "str":
		return true
	}
	return false
}

// IsBuiltinTypeName reports whether name is a builtin type name usable as a
// map key or value type (e.g. [str]i64). Exported for stages that implement
// ty.Resolver (the checker's IsTypeName needs the same vocabulary).
func IsBuiltinTypeName(name string) bool { return isBuiltinTypeName(name) }

// isBuiltinTypeName reports whether name is a builtin type name usable as a
// map key or value type (e.g. [str]i64).
func isBuiltinTypeName(name string) bool {
	switch name {
	case "str", "txt", "i64", "i32", "i16", "i8", "i128",
		"u64", "u32", "u16", "u8", "u128",
		"bool", "byte", "char",
		"f64", "f32":
		return true
	}
	return false
}

// isRegisteredTypeName reports whether name is a registered struct or enum
// type name (usable as a map key or value type).
func (p *Parser) isRegisteredTypeName(name string) bool {
	if p.structFields != nil {
		if _, ok := p.structFields[name]; ok {
			return true
		}
	}
	if p.sem.EnumVariants != nil {
		if _, ok := p.sem.EnumVariants[name]; ok {
			return true
		}
	}
	return false
}

// isFunctionTypeAlias 判斷當前位置是否為具名函式型別定義：name = (params)(results)?
// 判斷依據：
//  1. currentToken 為 IDENT，peekToken 為 ASSIGN，且其後緊接 LPAREN
//  2. 第一組 `(...)` 後緊接 NEWLINE/EOF/SEMICOLON（無結果列表），或緊接另一組 `(...)`
//     （結果列表），結果列表後再緊接 NEWLINE/EOF/SEMICOLON
//  3. `(...)` 內容確實為合法的函式型別參數列表（透過 parseFunctionType 驗證）
//
// 此方法不消耗任何 token（呼叫前後 parser 狀態相同）。
// 注意：look(0) 回傳 peekToken 之後的下一個 token（即 currentToken 之後第二個）。
func (p *Parser) isFunctionTypeAlias() bool {
	// Step 1: 結構檢查（快速篩選，避免對非匹配結構呼叫 parseFunctionType）
	if !p.isFunctionTypeAliasShape() {
		return false
	}
	// Step 2: 內容驗證（透過 parseFunctionType 確認 `(...)` 內容為合法函式型別參數）
	// 使用 saveState/restoreState 確保不影響 parser 狀態。
	// 注意：不依賴 parseFunctionType 的游標位置（其會跳過 NEWLINE 尋找結果列表），
	// 僅以錯誤計數判斷內容是否合法。
	state := p.saveState()
	errCount := len(p.diags)
	p.nextToken() // IDENT → ASSIGN
	p.nextToken() // ASSIGN → LPAREN
	p.parseFunctionType()
	parseFailed := len(p.diags) > errCount
	if parseFailed {
		// 回滾試解析期間產生的錯誤
		p.diags = p.diags[:errCount]
	}
	p.restoreState(state)
	return !parseFailed
}

// isFunctionTypeAliasShape 以 token 掃描方式判斷當前位置是否符合
// `name = (params)(results)?` 的括號結構與陳述句邊界。
// 不檢查 `(...)` 內容是否為合法函式型別參數（由 isFunctionTypeAlias 的 Step 2 負責）。
// 不消耗任何 token。
func (p *Parser) isFunctionTypeAliasShape() bool {
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN {
		return false
	}
	// `=` 之後必須是 `(`（look(0) = peekToken 之後的 token）
	if p.look(0).Type != lexer.LPAREN {
		return false
	}

	// 透過 look 掃描：位置 0=`(`, 1=第一個 ( 內的 token 或 `)`, ...
	// 我們需要找到第一組 `(...)` 的結尾 `)`，然後檢查其後。
	// 掃描時追蹤括號深度，遇到深度歸零的 `)` 後檢查下一個 token。
	depth := 0
	i := 0
	// 先找到 `(`（位置 0）
	for {
		t := p.look(i)
		switch t.Type {
		case lexer.LPAREN:
			depth++
		case lexer.RPAREN:
			depth--
			if depth == 0 {
				// 找到第一組 `(...)` 的結尾 `)`
				// 檢查下一個 token
				next := p.look(i + 1)
				switch next.Type {
				case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
					return true
				case lexer.LPAREN:
					// 可能是結果列表 `(results)`
					// 掃描第二組 `(...)`
					depth2 := 0
					j := i + 1
					for {
						t2 := p.look(j)
						switch t2.Type {
						case lexer.LPAREN:
							depth2++
						case lexer.RPAREN:
							depth2--
							if depth2 == 0 {
								// 第二組 `)` 後必須是陳述句結束
								next2 := p.look(j + 1)
								switch next2.Type {
								case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
									return true
								default:
									return false
								}
							}
						case lexer.EOF:
							return false
						}
						j++
						if j > 256 { // 安全上限
							return false
						}
					}
				default:
					return false
				}
			}
		case lexer.EOF:
			return false
		}
		i++
		if i > 256 { // 安全上限
			return false
		}
	}
}

// parseFunctionTypeAlias 解析具名函式型別定義：name = (params)(results)?
// 前置條件: currentToken 為 IDENT（名稱），peekToken 為 ASSIGN
// 後置條件: currentToken 為函式型別後的第一個 token（通常為 NEWLINE/EOF）
// 回傳 *TypeAlias{ Name, Type: *FunctionType{...} }
func (p *Parser) parseFunctionTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip IDENT → ASSIGN
	if p.currentToken.Type != lexer.ASSIGN {
		p.saveErrorf("cjih2jtt", "line %d, column %d: expected '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil
	}
	p.nextToken() // skip ASSIGN → LPAREN

	if p.currentToken.Type != lexer.LPAREN {
		p.saveErrorf("sd13rw1l", "line %d, column %d: expected '(' after '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil
	}

	ft := p.parseFunctionType()
	ta.Type = ft
	return ta
}

// builtInTypeNames are the built-in type names that can be used in type aliases.
// When `name = IDENT` is seen at the top level and IDENT is one of these,
// it is treated as a type alias rather than a let statement.
var builtInTypeNames = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true,
	"f32": true, "f64": true,
	"byte": true, "bool": true, "str": true, "any": true,
}

// looksLikeEqualsTypeAlias reports whether the current statement looks like an
// equals-syntax type alias: name = type | type | ..., name = []type,
// name = [N]type, name = ?type, or name = known-type-name
// The current token is the alias name (IDENT), peekToken is ASSIGN.
// Note: look(0) returns the token AFTER peekToken (=), i.e. the first RHS token.
func (p *Parser) looksLikeEqualsTypeAlias() bool {
	if p.ctx.contains(CTX_FUNC_BODY) {
		return false
	}
	t1 := p.look(0) // first RHS token (after '=')
	switch t1.Type {
	case lexer.IDENT:
		// name = IDENT | IDENT | ... → union type alias
		// name = IDENT              → single type alias (if IDENT is a known type)
		t2 := p.look(1)
		if t2.Type == lexer.OR {
			// Distinguish union type alias from bitwise-or expression.
			// A union type alias has the form: type (| type)* where each
			// type is IDENT, []T, [N]T, ?T, or (params)(results).
			// A bitwise-or expression like `bv | (1 << bt)` contains
			// arithmetic operators (other than |) at bracket depth 0.
			// Scan the RHS: at depth 0, only `|` and statement-end
			// tokens are allowed; any other operator means it's an
			// expression, not a type alias.
			return p.scanIsUnionTypeAlias()
		}
		if t2.Type == lexer.NEWLINE || t2.Type == lexer.EOF || t2.Type == lexer.SEMICOLON {
			return builtInTypeNames[t1.Literal] || (p.typeAliasNames != nil && p.typeAliasNames[t1.Literal])
		}
		return false
	case lexer.LBRACKET:
		// name = []type or name = [N]type
		t2 := p.look(1)
		if t2.Type == lexer.RBRACKET {
			// []type — check that next is IDENT (type name)
			t3 := p.look(2)
			if t3.Type == lexer.IDENT {
				return true
			}
			return false
		}
		if t2.Type == lexer.INT {
			t3 := p.look(2)
			if t3.Type == lexer.RBRACKET {
				t4 := p.look(3)
				if t4.Type == lexer.IDENT {
					return true
				}
			}
		}
		return false
	case lexer.QUESTION:
		// name = ?type — ? at expression start is not valid
		t2 := p.look(1)
		if t2.Type == lexer.IDENT {
			return true
		}
		return false
	default:
		return false
	}
}

// scanIsUnionTypeAlias scans the RHS of `name = ... | ...` to determine
// whether it is a union type alias (type (| type)*) or a bitwise-or
// expression. The scan starts from look(0) (the first RHS token)
// and examines tokens at bracket depth 0:
//   - `|` separates union members (allowed)
//   - NEWLINE/EOF/SEMICOLON end the statement (→ union type alias)
//   - `[`/`(`/`{` increase depth; `]`/`)`/`}` decrease depth
//   - Any other BINARY operator at depth 0 (ADD, SUB, MUL, SHL, AND, etc.)
//     means it's a bitwise-or expression, not a type alias
//   - IDENT, INT, FLOAT, STRING, etc. are part of a type/operand
//
// Special handling for `(` at depth 0: a function type's params start
// with IDENT/IN/`)`, so if the token inside `(` is something else (INT,
// FLOAT, etc.), it's a grouped expression, not a type → return false.
func (p *Parser) scanIsUnionTypeAlias() bool {
	depth := 0
	i := 0
	for {
		t := p.look(i)
		switch t.Type {
		case lexer.LPAREN:
			if depth == 0 {
				// Distinguish function type (params) from grouped expression.
				// Function type params start with IDENT, IN, or `)` (empty).
				inside := p.look(i + 1)
				switch inside.Type {
				case lexer.IDENT, lexer.IN, lexer.RPAREN:
					// Likely function type — treat as type, enter parens
				default:
					// Grouped expression like (1 << bt) — not a type alias
					return false
				}
			}
			depth++
		case lexer.LBRACKET, lexer.LBRACE:
			depth++
		case lexer.RBRACKET, lexer.RPAREN, lexer.RBRACE:
			if depth == 0 {
				// Unmatched closing bracket — end of construct
				return true
			}
			depth--
		case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
			if depth == 0 {
				return true
			}
		case lexer.ADD, lexer.SUB, lexer.MUL, lexer.QUO, lexer.MOD,
			lexer.SHL, lexer.SHR, lexer.AND, lexer.XOR,
			lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.LESS_EQUALS,
			lexer.GREATER, lexer.GREATER_EQUALS, lexer.LOR, lexer.LAND,
			lexer.ASSIGN, lexer.COLON,
			lexer.QUESTION, lexer.ARROW, lexer.RARROW:
			if depth == 0 {
				// An arithmetic/comparison operator at depth 0 means
				// this is a bitwise-or expression, not a type alias.
				return false
			}
		}
		i++
		if i > 1024 {
			return false
		}
	}
}

// parseTypeAlias parses a type alias or union type alias statement.
//
//	int = i8 | i16 | ... | u64   →  TypeAlias{Name:"int", Union:[i8,i16,...,u64]}
//	float = f32 | f64            →  TypeAlias{Name:"float", Union:[f32,f64]}
//	num = int | float            →  TypeAlias{Name:"num", Union:[int,float]}
//	my-int = i64                 →  TypeAlias{Name:"my-int", Type:NamedType{"i64"}}
//	bytes = []byte               →  TypeAlias{Name:"bytes", Type:SliceType{byte}}
//	buf = [16]u8                 →  TypeAlias{Name:"buf", Type:ArrayType{16,u8}}
//
// The first type must be a concrete type. If it is followed by `|`, we
// collect the rest of the union.
func (p *Parser) parseTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip alias name → ASSIGN
	// Skip ASSIGN
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}
	typ, ok := p.parseTypeExpression()
	if !ok {
		p.saveErrorf("46jwfg5y", "line %d, column %d: expected type after %q in type alias",
			p.currentToken.Line, p.currentToken.Column, name)
		return nil
	}
	// After parseTypeExpression, currentToken is the first non-type token
	// (e.g. `|`, NEWLINE, EOF).
	if p.currentToken.Type == lexer.OR {
		ut := &UnionType{Token: nameToken, Types: []Type{typ}}
		for p.currentToken.Type == lexer.OR {
			p.nextToken() // skip |
			// Allow newlines between | and the next type
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			next, ok := p.parseTypeExpression()
			if !ok {
				p.saveErrorf("cvl8rf8s", "line %d, column %d: expected type after '|'",
					p.currentToken.Line, p.currentToken.Column)
				return nil
			}
			ut.Types = append(ut.Types, next)
		}
		ta.Union = ut
	} else {
		ta.Type = typ
	}
	// Record the type alias name for future detection
	if p.typeAliasNames == nil {
		p.typeAliasNames = make(map[string]bool)
	}
	p.typeAliasNames[name] = true
	// Record union members for type-destructuring match detection
	// (lowering needs to know if a matched value's static type is a union).
	if ta.Union != nil {
		if p.unionMembers == nil {
			p.unionMembers = make(map[string][]string)
		}
		members := make([]string, 0, len(ta.Union.Types))
		for _, t := range ta.Union.Types {
			if nt, ok := t.(*NamedType); ok {
				members = append(members, nt.Value)
			}
		}
		p.unionMembers[name] = members
	}
	return ta
}

// parseTypeExpression parses a single type (used by type aliases). It
// returns the parsed Type and whether parsing succeeded. The current
// token is left at the first token that is NOT part of the type
// (typically NEWLINE, EOF, OR, or a statement-end token).
func (p *Parser) parseTypeExpression() (Type, bool) {
	startTok := p.currentToken
	switch p.currentToken.Type {
	case lexer.LPAREN:
		// Function type: (params) (results)?
		return p.parseFunctionType(), true
	case lexer.IDENT:
		// Could be:
		//   - NamedType (e.g., "i64")
		//   - PointerType (e.g., "ptr i64") — though typical syntax is
		//     "ptr" followed by an IDENT.
		if p.peekToken.Type == lexer.IDENT {
			// Check for "ptr T" form
			if p.currentToken.Literal == "ptr" {
				inner := p.peekToken
				p.nextToken() // skip "ptr"
				p.nextToken() // skip inner type
				return &PointerType{Token: startTok, Type: &NamedType{Token: inner, Value: inner.Literal}}, true
			}
		}
		name := p.currentToken.Literal
		t := p.currentToken
		p.nextToken()
		// Support dotted/qualified type names: net.conn, tls.conn, sql.result, etc.
		for p.currentToken.Type == lexer.DOT {
			p.nextToken() // skip DOT
			if p.currentToken.Type == lexer.IDENT {
				name += "." + p.currentToken.Literal
				p.nextToken() // skip IDENT part
			}
		}
		return &NamedType{Token: t, Value: name}, true
	case lexer.LBRACKET:
		p.nextToken() // skip [
		if p.currentToken.Type == lexer.RBRACKET {
			// []T — element type can itself be a complex type (e.g. [][]str)
			p.nextToken() // skip ]
			elemType, ok := p.parseTypeExpression()
			if !ok {
				return nil, false
			}
			return &SliceType{Token: startTok, Elem: elemType}, true
		}
		if p.currentToken.Type == lexer.QUESTION {
			// [?]T
			p.nextToken() // skip ?
			p.nextToken() // skip ]
			elemName := p.currentToken.Literal
			elemTok := p.currentToken
			p.nextToken()
			return &SliceType{Token: startTok, Elem: &NullableType{Token: elemTok, Type: &NamedType{Token: elemTok, Value: elemName}}}, true
		}
		// [K]V — MapType when K is a builtin or registered type name;
		// otherwise [N]T — ArrayType with size.
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K
			p.nextToken() // skip ]
			// Value type can itself be a complex type (e.g. [str][]str)
			valType, ok := p.parseTypeExpression()
			if !ok {
				return nil, false
			}
			return &MapType{Token: startTok, Key: &NamedType{Token: keyTok, Value: keyName}, Value: valType}, true
		}
		// [N]T
		sizeTok := p.currentToken
		p.nextToken() // skip size
		p.nextToken() // skip ]
		// Build size expression: IntegerLiteral for numeric sizes (so constFoldInt
		// can fold them in arrayTypeToLLVM), Identifier for symbolic sizes (e.g. [n]T).
		var sizeExpr Expression
		if sizeTok.Type == lexer.INT {
			if val, err := strconv.ParseInt(sizeTok.Literal, 10, 64); err == nil {
				sizeExpr = &IntegerLiteral{Token: sizeTok, Value: val, Raw: sizeTok.Literal}
			} else {
				sizeExpr = &Identifier{Token: sizeTok, Value: sizeTok.Literal}
			}
		} else {
			sizeExpr = &Identifier{Token: sizeTok, Value: sizeTok.Literal}
		}
		// [N][M]T — nested array type
		if p.currentToken.Type == lexer.LBRACKET {
			elemType, ok := p.parseTypeExpression()
			if ok {
				return &ArrayType{Token: startTok, Size: sizeExpr, Elem: elemType}, true
			}
			return nil, false
		}
		// Element type after [N]: could be "net.conn" (dotted) or "i64" (simple).
		elemName := p.currentToken.Literal
		elemTok := p.currentToken
		p.nextToken()
		// Support dotted/qualified type names: net.conn, tls.conn, sql.result, etc.
		for p.currentToken.Type == lexer.DOT {
			p.nextToken() // skip DOT
			if p.currentToken.Type == lexer.IDENT {
				elemName += "." + p.currentToken.Literal
				p.nextToken() // skip IDENT part
			}
		}
		return &ArrayType{Token: startTok, Size: sizeExpr, Elem: &NamedType{Token: elemTok, Value: elemName}}, true
	case lexer.QUESTION:
		// ?T
		p.nextToken() // skip ?
		innerName := p.currentToken.Literal
		innerTok := p.currentToken
		p.nextToken()
		return &NullableType{Token: startTok, Type: &NamedType{Token: innerTok, Value: innerName}}, true
	}
	return nil, false
}

// parseFunctionType parses a function type: (params) (results)?
// Pre-condition: p.currentToken is the opening '(' token.
// Post-condition: p.currentToken is the first token after the function type
// (typically NEWLINE, COMMA, RPAREN, or LBRACE).
// Each entry in params/results can be either "name type" or just "type"
// (name-less form, stored with empty Name).
func (p *Parser) parseFunctionType() *FunctionType {
	ft := &FunctionType{Token: p.currentToken}

	if p.currentToken.Type != lexer.LPAREN {
		p.saveErrorf("ljfz84ro", "line %d, column %d: expected '(' to start function type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return ft
	}
	p.nextToken() // skip (

	// parse params
	if p.currentToken.Type != lexer.RPAREN {
		for {
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			param, ok := p.parseFunctionTypeEntry()
			if !ok {
				return ft
			}
			ft.Params = append(ft.Params, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				p.saveErrorf("wg5j97zm", "line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				return ft
			}
			p.nextToken() // skip COMMA
		}
	}
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
	}

	// Optional results list: (results)
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				for p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				result, ok := p.parseFunctionTypeEntry()
				if !ok {
					return ft
				}
				ft.Results = append(ft.Results, result)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					p.saveErrorf("qayttv4q", "line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					return ft
				}
				p.nextToken() // skip COMMA
			}
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}

	return ft
}

// parseFunctionTypeEntry parses one entry inside a function type's param or
// result list. Supports two forms:
//   - "name type"  (e.g. `n i64`)
//   - "type"       (name-less, e.g. `i64`)
//
// Pre-condition: p.currentToken is the first token of the entry (IDENT or IN).
// Post-condition: p.currentToken is the first token after the entry
// (typically COMMA or RPAREN).
func (p *Parser) parseFunctionTypeEntry() (*Parameter, bool) {
	if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
		p.saveErrorf("y9lj5a9g", "line %d, column %d: expected parameter name or type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil, false
	}
	firstTok := p.currentToken
	firstLit := p.currentToken.Literal
	p.nextToken()

	// If next token starts a type, firstLit was the param name.
	if p.currentToken.Type == lexer.IDENT ||
		p.currentToken.Type == lexer.LBRACKET ||
		p.currentToken.Type == lexer.QUESTION {
		paramType, ok := p.parseParamTypeAfterName()
		if !ok {
			return nil, false
		}
		return &Parameter{Token: firstTok, Name: firstLit, Type: paramType}, true
	}

	// If next token is DOT, firstLit was the start of a dotted type name
	// (e.g. sql.result as a name-less type).
	if p.currentToken.Type == lexer.DOT {
		paramType := firstLit
		for p.currentToken.Type == lexer.DOT {
			paramType += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				paramType += p.currentToken.Literal
				p.nextToken()
			}
		}
		return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: paramType}}, true
	}

	// Otherwise firstLit is a name-less simple type (NamedType).
	return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: firstLit}}, true
}

// parseParamTypeAfterName parses a single parameter type after the parameter
// name has been consumed. Supports: ?type, []type, [N]type, [?]type, named
// type, and (params)(results) function type. Returns the parsed Type and whether
// parsing succeeded. On success, p.currentToken is the first token after
// the type (typically COMMA, RPAREN, or NEWLINE).
func (p *Parser) parseParamTypeAfterName() (Type, bool) {
	// Function type: (params) (results)?
	// 受 anonymous-fn-type 開關控制：開關關閉時，參數列表中的匿名函式型別
	// 語法（如 `cb ()()`）被拒絕，應改用具名函式型別定義。
	if p.currentToken.Type == lexer.LPAREN {
		if !p.AllowAnonymousFnType {
			p.saveErrorf("zc1zfsat", "line %d, column %d: anonymous function type syntax '()' is disabled; use a named function type alias or enable 'anonymous-fn-type' in package.noml",
				p.currentToken.Line, p.currentToken.Column)
			return nil, false
		}
		return p.parseFunctionType(), true
	}

	paramType := ""
	isOption := false
	// view（借用）型別前綴：`name &T`，可與 option 組合為 `name ?&T`。
	// `?` 與 `&` 的書寫順序不限（`?&T` 與 `&?T` 等價），正規形式為 `?&T`。
	// 注意：`&` 在表達式裡是按位與，但在此處（參數/返回名之後的型別位置）
	// 只可能是 view 型別，不會歧義。
	viewPrefix := ""
	for p.currentToken.Type == lexer.QUESTION || p.currentToken.Type == lexer.AND {
		if p.currentToken.Type == lexer.QUESTION {
			isOption = true
		} else {
			viewPrefix += "&"
		}
		p.nextToken()
	}
	// 指標型別前綴：`name *T` / `name **T`。fmt 以 typeString 的規範前綴形式
	// （`*i8`）輸出，後綴形式（`i8*`）是源碼慣用法——兩者必須等價可解析，
	// 否則 fmt 輸出不可再解析（TestFormatReparseClean/async.no）。
	ptrPrefix := ""
	for p.currentToken.Type == lexer.MUL {
		ptrPrefix += "*"
		p.nextToken()
	}
	if p.currentToken.Type == lexer.STAR_STAR {
		ptrPrefix += "**"
		p.nextToken()
	}
	if p.currentToken.Type == lexer.LBRACKET {
		// Parse first bracket group: [], [N], [?], or [K]
		p.nextToken() // skip [
		if p.currentToken.Type == lexer.INT {
			paramType = "[" + p.currentToken.Literal + "]"
			p.nextToken()
		} else if p.currentToken.Type == lexer.IDENT {
			paramType = "[" + p.currentToken.Literal + "]"
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION {
			paramType = "[?]"
			p.nextToken()
		} else {
			paramType = "[]"
		}
		if p.currentToken.Type == lexer.RBRACKET {
			p.nextToken()
		}
		// Parse element type: IDENT (possibly dotted), or nested []/[N] (e.g. [][]str)
		if p.currentToken.Type == lexer.IDENT {
			paramType = paramType + p.currentToken.Literal
			p.nextToken()
			// Support dotted element type: []sql.result
			for p.currentToken.Type == lexer.DOT {
				paramType += "."
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					paramType += p.currentToken.Literal
					p.nextToken()
				}
			}
			// Support nested brackets after element type: []str -> already handled
		} else if p.currentToken.Type == lexer.LBRACKET {
			// Nested slice/array type: [][]str, [N][]str, etc.
			innerType, ok := p.parseTypeExpression()
			if ok {
				paramType = paramType + typeToString(innerType)
			}
		}
	} else if p.currentToken.Type == lexer.IDENT {
		paramType = p.currentToken.Literal
		p.nextToken()
		// Support dotted/qualified type names: sql.result, http.request, etc.
		for p.currentToken.Type == lexer.DOT {
			paramType += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				paramType += p.currentToken.Literal
				p.nextToken()
			}
		}
	} else if !isOption && viewPrefix == "" {
		p.saveErrorf("ujfsckuo", "line %d, column %d: expected parameter type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil, false
	}

	// 指標型別後綴：`name T*`（如 `task i8*`）。
	// 與 struct 欄位的前綴形式 `*T`（decl.go）及 typeString 的 `*T` 表示一致，
	// 統一交給 buildType 轉成 PointerType。`*` 綁定在基礎型別上（先於 ? 包裹），
	// 故 `?T*` 解讀為 ?(*T)。支援多層：`T**` → `*(*T)`。
	for p.currentToken.Type == lexer.MUL {
		p.nextToken()
		ptrPrefix += "*"
	}
	if p.currentToken.Type == lexer.STAR_STAR {
		p.nextToken()
		ptrPrefix += "**"
	}
	paramType = ptrPrefix + paramType

	// view 包在指標外層：`&T` / `&*T` → ViewType{T} / ViewType{PointerType{T}}
	paramType = viewPrefix + paramType

	if isOption {
		paramType = "?" + paramType
	}

	return buildType(paramType, p.currentToken), true
}

// exprToType 將 Expression 轉換為 Type，用於 NullableType/PointerType 的內部型別
func exprToType(expr Expression) Type {
	if t, ok := expr.(Type); ok {
		return t
	}
	if ident, ok := expr.(*Identifier); ok {
		return &NamedType{Token: ident.Token, Value: ident.Value}
	}
	return &NamedType{
		Token: lexer.Token{Line: expr.Pos().Line, Column: expr.Pos().Column},
		Value: fmt.Sprintf("%v", expr),
	}
}

func (p *Parser) parsePointerType() Expression {
	pt := &PointerType{Token: p.currentToken}
	p.nextToken() // skip ptr
	// ptr(type)
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken() // skip (
		typeExpr := p.parseExpression(LOWEST)
		pt.Type = exprToType(typeExpr)
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}
	return pt
}

func (p *Parser) parseNullableType(expression Expression) Expression {
	nullable := &NullableType{
		Token: p.peekToken,
		Type:  exprToType(expression),
	}

	p.nextToken() // 跳过 QUESTION 令牌

	return nullable
}

// markInferred 遞迴標記型別節點為 parser 推斷（非源碼顯式標注）。
// 用於從表達式推斷變數型別的場景（如 CallExpression 返回型別），
// 讓 formatter 直接讀取 IsInferred 欄位而非依賴 token 位置相等啟發式。
func markInferred(t Type) Type {
	switch typ := t.(type) {
	case *NamedType:
		typ.IsInferred = true
	case *ArrayType:
		typ.IsInferred = true
		markInferred(typ.Elem)
	case *SliceType:
		typ.IsInferred = true
		markInferred(typ.Elem)
	case *NullableType:
		typ.IsInferred = true
		markInferred(typ.Type)
	case *MapType:
		typ.IsInferred = true
		markInferred(typ.Key)
		markInferred(typ.Value)
	case *PointerType:
		markInferred(typ.Type)
	case *ViewType:
		typ.IsInferred = true
	}
	return t
}

// typeIsInferred 回報型別節點是否由 parser 推斷而來（非源碼顯式標注）。
// 與 markInferred 對稱；捕獲 pass 用它區分「用戶寫下的 plain 型別」與
// 「parse 期替 `v = arr[i]` 推斷出的元素型別」——只有後者允許被改寫成 ?elem。
func typeIsInferred(t Type) bool {
	switch typ := t.(type) {
	case *NamedType:
		return typ.IsInferred
	case *ArrayType:
		return typ.IsInferred
	case *SliceType:
		return typ.IsInferred
	case *NullableType:
		return typ.IsInferred
	case *MapType:
		return typ.IsInferred
	case *ViewType:
		return typ.IsInferred
	}
	return false
}

// buildType 將型別字串轉換為 Type 節點。
//
// 分類（option / 指標 / view / 切片 / 陣列 / map）不再由本函式自行判斷前綴，
// 一律委派 ty.Parse：那裡的決定是「唯一規則」（`[N]T` 的 N 為數字/`?`/單字母
// 時是陣列，其餘是 map；`*T` 與 `ptr T` 都是 Ptr）。本函式只負責把結構結果
// 還原成語法節點，因此 AST 形狀、位置資訊與 String() 輸出都不變。
func buildType(typeStr string, tok lexer.Token) Type {
	if typeStr == "" {
		return nil
	}
	st := tyPool.Parse(typeStr)
	switch st.Kind {
	case ty.Option:
		if inner := buildType(rawOf(st.Elem), tok); inner != nil {
			return &NullableType{Token: tok, Type: inner}
		}
	case ty.Ptr:
		if inner := buildType(rawOf(st.Elem), tok); inner != nil {
			return &PointerType{Token: tok, Type: inner}
		}
	case ty.View:
		if inner := buildType(rawOf(st.Elem), tok); inner != nil {
			return &ViewType{Token: tok, Type: inner}
		}
	case ty.Slice:
		elem := buildType(rawOf(st.Elem), tok)
		if elem == nil {
			elem = &NamedType{Token: tok, Value: "i64"}
		}
		return &SliceType{Token: tok, Elem: elem}
	case ty.Array:
		var sizeExpr Expression
		if st.Len >= 0 {
			sizeExpr = &IntegerLiteral{Token: tok, Value: st.Len, Raw: st.LenText}
		} else if st.LenText != "" && st.LenText != "?" {
			sizeExpr = &Identifier{Token: tok, Value: st.LenText}
		}
		elem := buildType(rawOf(st.Elem), tok)
		if elem == nil {
			elem = &NamedType{Token: tok, Value: rawOf(st.Elem)}
		}
		return &ArrayType{Token: tok, Size: sizeExpr, Elem: elem}
	case ty.Map:
		valType := buildType(rawOf(st.Elem), tok)
		if valType == nil {
			valType = &NamedType{Token: tok, Value: rawOf(st.Elem)}
		}
		return &MapType{
			Token: tok,
			Key:   &NamedType{Token: tok, Value: rawOf(st.Key)},
			Value: valType,
		}
	}
	// 簡單名稱型別（含純量、別名、模組限定名與單態化殘留名）
	return &NamedType{Token: tok, Value: typeStr}
}

// parseMapTypeString 嘗試將 "[K]V" 格式的型別字串解析為 MapType。
// 區分規則：[ 後至 ] 的內容若為空（[]）或純數字（[3]）或問號（[?]，
// 視為陣列/切片型別，返回 nil；否則視為 map 型別 [K]V，返回 MapType。
// 例如 "[str]i64" → MapType{Key:str, Value:i64}，"[3]i64" → nil。
// IsMapTypeString 報告型別字串 s 是否為 map 型別 `[K]V`（導出供 checker 使用）。
//
// 必須在「容器分類」之前先判 map：`containerElemType("[str]i64")` 會把 map 的
// 型別字串誤判為陣列（回傳 "i64"），若不先排除，map 的下標寫入 `m[k] = v`
// 就會被 idxwrite 之類的檢查誤傷。判定走 parseMapTypeString（[] / [?] / [數字]
// 都視為陣列或切片而非 map），因此涵蓋**所有**鍵型別，不限於內建鍵名——
// 比 checker 側只認 str/bool/整數鍵的 checkerKeyCategory 更嚴謹，且與 parser
// 的型別解析同口徑。
func IsMapTypeString(s string) bool {
	return parseMapTypeString(s, lexer.Token{}) != nil
}

func parseMapTypeString(s string, tok lexer.Token) *MapType {
	// 分類委派 ty：`[K]V` 是 map 還是陣列由 ty 的單一規則回答（`[]`/`[?]`/
	// `[數字]`/`[單字母符號長度]` 是陣列，其餘是 map），因此 parser 不再維護
	// 自己的一套「非數字即 map」判定。`[fd]i64` 從此三處同結論（map）。
	st := tyPool.Parse(s)
	if st.Kind != ty.Map || st.Key == nil || st.Elem == nil {
		return nil
	}
	return &MapType{
		Token: tok,
		Key:   &NamedType{Token: tok, Value: rawOf(st.Key)},
		Value: &NamedType{Token: tok, Value: rawOf(st.Elem)},
	}
}

// typeToString converts a Type node back to its string representation.
// Used by parseParamTypeAfterName to build type strings for nested types.
func typeToString(t Type) string {
	switch typ := t.(type) {
	case *NamedType:
		return typ.Value
	case *SliceType:
		return "[]" + typeToString(typ.Elem)
	case *ArrayType:
		if lit, ok := typ.Size.(*IntegerLiteral); ok {
			return fmt.Sprintf("[%d]%s", lit.Value, typeToString(typ.Elem))
		}
		if ident, ok := typ.Size.(*Identifier); ok {
			return fmt.Sprintf("[%s]%s", ident.Value, typeToString(typ.Elem))
		}
		return "[]" + typeToString(typ.Elem)
	case *NullableType:
		return "?" + typeToString(typ.Type)
	case *ViewType:
		return "&" + typeToString(typ.Type)
	case *PointerType:
		// 雙前綴不重複：內層本身已是指標時沿用其拼寫（結構判斷，不掃字串）。
		if inner := TyOf(typ.Type); inner != nil && inner.Kind == ty.Ptr {
			return rawOf(inner)
		}
		return "ptr " + typeToString(typ.Type)
	case *MapType:
		return fmt.Sprintf("[%s]%s", typeToString(typ.Key), typeToString(typ.Value))
	default:
		return ""
	}
}
