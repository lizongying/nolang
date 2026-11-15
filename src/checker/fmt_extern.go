package checker

// fmt_extern.go — 單檔 validator 的跨模組簽名注入。
//
// `no fmt` 過去在全新單檔 parse 上直接呼叫 ValidateRedundantTypeAnnotation /
// StatementsWithIntOverflow；兩者只知道本檔宣告，於是 `x = mod.fn()` 的右值
// 推不出型別，形成 checker.go 多處註解記錄的「vet 有、fmt 沒有」。
//
// 現在 fmt 會注入 std / 本地模組簽名（見 build/fmt_signatures.go）。這裡提供
// 一組顯式入口讓 validator 把那些表併入本地推導表。設計上刻意採 opt-in：
//
//   - 舊函式一律委託零表，合併 vet 管道（validationFuncTypes /
//     validationStructFields 已由 ValidateTypes 填好）的行為完全不變；
//   - 合併策略是「只填空缺」：本檔宣告、vet 已算好的結果永遠優先；
//   - ext 為零值時兩個 merge helper 都是 no-op。
//
// 這些表是只讀視圖，呼叫端不應修改傳入的 map。

import "github.com/lizongying/nolang/parser"

// ExternSigTables 是一次單檔解析看到的跨模組簽名，供 validator 補齊
// inferExprType 查表用。FuncTypes 是「函式 / 方法全名 → 回傳型別字串」，
// StructFields 是「struct 名 → 欄位名 → 型別字串」。
type ExternSigTables struct {
	FuncTypes    map[string]string
	StructFields map[string]map[string]string
}

// ExternSigTablesFromConfig 把一次解析的 SemRebuildConfig（fmt.ParseConfig 是
// 它的 type alias）轉成單檔 validator 吃的「單一回傳型別」表。FuncSigs 先放、
// MethodSigs 只補空缺，與 build 的合併順序一致。
//
// CLI `no fmt` 原本在 cmd/no 自己寫這份轉換；LSP 的主動改寫也要用，故移到
// checker 讓兩邊共用一份，避免「哪一邊先放 method 表」这类細微漂移。
func ExternSigTablesFromConfig(cfg parser.SemRebuildConfig) ExternSigTables {
	ext := ExternSigTables{StructFields: cfg.StructFields}
	if len(cfg.FuncSigs) == 0 && len(cfg.MethodSigs) == 0 {
		return ext
	}
	ext.FuncTypes = make(map[string]string, len(cfg.FuncSigs)+len(cfg.MethodSigs))
	for _, table := range []map[string][]string{cfg.FuncSigs, cfg.MethodSigs} {
		for name, rets := range table {
			if len(rets) == 0 || rets[0] == "" {
				continue
			}
			if _, exists := ext.FuncTypes[name]; !exists {
				ext.FuncTypes[name] = rets[0]
			}
		}
	}
	return ext
}

// mergeExternFuncTypes 只補 dst 裡缺的鍵。funcTypes 的查表鍵可能是裸名
// （本檔函式）、`mod.fn`（模組限定呼叫）或 `type.method`（方法），三者共用
// 同一張表，故這裡不做任何鍵名正規化。
func mergeExternFuncTypes(dst, src map[string]string) {
	if dst == nil || len(src) == 0 {
		return
	}
	for k, v := range src {
		if v == "" {
			continue
		}
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

// mergeExternStructFields 只補 dst 裡缺的 struct（不逐欄位合併）：本檔若已
// 定義同名 struct，其欄位集合就是權威，不能被跨模組的同名結構覆蓋。
func mergeExternStructFields(dst, src map[string]map[string]string) {
	if dst == nil || len(src) == 0 {
		return
	}
	for name, fields := range src {
		if fields == nil {
			continue
		}
		if _, exists := dst[name]; !exists {
			dst[name] = fields
		}
	}
}
