package checker

import (
	"fmt"
	"sync"

	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// struct_assign.go — 拒絕「純量靜默塞進結構體變數」。
//
// 背景：LetStatement 的相容性檢查（checker.go 的 15w45dqk）以
// isConcreteType(declared) 作門檻，而具名結構體不在那張具體型別表裡，於是
// `n node = 5` 整條賦值檢查被跳過：checker 放行，codegen 把 i64 位元塞進
// struct 槽位。之後任何欄位讀取都是越界解讀（`x bigint = 5` 之後
// `x.to-i64()` 讀回 4310293264 這類垃圾）。
//
// 與 D2b（unionarg）同一條政策：後端無法轉換的型別必須在源碼層拒收並報
// 診斷，絕不靜默誤編譯。此處只認「可靜態證明是純量」的右值（字面量、其
// 組合運算），因為推斷面對 CallExpression 會回落成泛用 i64，直接拿它判斷
// 會把 `b node = mk()` 這類合法寫法誤殺。
//
// 邊界：
//   - 宣告型別必須是本程式（含合併後的 std）真實宣告過的結構體；union
//     別名、interface、型別參數一律不碰（它們有自己的派發路徑）。
//   - 右值只認字面量 / 分組 / 前綴負號 / 雙運算數字面量；識別字與呼叫一律
//     放行——推斷出來的型別不足以定罪。
//   - `?T` / `&T` 宣告剝掉修飾後查結構體，`?node = 5` 同樣拒收。

// stdStructNameSet 是 std 結構體裸名表（如 "bigint"），由烤進二進位的簽名表
// 提供。單檔校驗／未合併 std 的 program.Statements 看不到其他模組的 struct
// 宣告，所以 `x bigint = 5` 的宣告面只能從這裡認出來。
//
// StdSigKey 為空（裸 go build / go test）時表不存在，此處直接回 nil：寧可漏報
// 也不要在沒有簽名表的環境裡誤報。
var (
	stdStructNameSetOnce sync.Once
	stdStructNameSet     map[string]string
)

func stdStructNames() map[string]string {
	if StdSigKey == "" {
		return nil
	}
	stdStructNameSetOnce.Do(func() {
		stdStructNameSet = CollectStdStructModules()
	})
	return stdStructNameSet
}

// structAssignShape 把宣告型別剝成裸結構體名（去 `?` / `&` / `ptr` 修飾），
// 若該名是已知結構體（本程式宣告的或 std 的）則回傳其裸名，否則回傳 ""。
// wrapped 表示宣告帶修飾（`?bigint`、`&bigint`）——隱式提升只認「裸結構體
// 目標」，option 目標還要多一步打 tag，後端沒有那一步，照提升會變回靜默
// 誤編譯，所以 wrapped 的宣告不參與提升判定。
//
// 修飾剝離走 ty 的結構（Kind/Elem），不做字串前綴掃描——與 make test 的
// verify-ty 契約一致。
func structAssignShape(declared string, structFields map[string]map[string]string) (string, bool) {
	if declared == "" {
		return "", false
	}
	t := ty.Parse(declared)
	wrapped := false
	for i := 0; t != nil && i < 8; i++ {
		switch t.Kind {
		case ty.Option, ty.View, ty.Ptr:
			t = t.Elem
			wrapped = true
			continue
		}
		break
	}
	if t == nil || t.Kind != ty.Named {
		return "", false
	}
	base := t.Name
	if base == "" {
		base = t.Raw()
	}
	candidates := []string{base}
	if bare := stripModulePrefix(base); bare != base {
		candidates = append(candidates, bare)
	}
	for _, cand := range candidates {
		if _, ok := structFields[cand]; ok {
			return cand, wrapped
		}
		if _, ok := stdStructNames()[cand]; ok {
			return cand, wrapped
		}
	}
	return "", false
}

// provableScalarType 回傳 expr 可靜態證明為純量的型別拼寫；證明不了回傳 ""。
//
// 只承認不依賴推斷表的形狀：字面量本身、分組、前綴負號、以及兩個都已被
// 證明為純量的孩子所組成的中綴運算。任何識別字 / 呼叫 / 欄位存取都可能是
// 結構體值，一律回 ""（放行）。
func provableScalarType(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.CharLiteral:
		return "char"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.NilLiteral:
		return "nil"
	case *parser.GroupedExpression:
		return provableScalarType(e.Expression)
	case *parser.PrefixExpression:
		if e.Operator == "-" || e.Operator == "~" || e.Operator == "!" {
			return provableScalarType(e.Right)
		}
	case *parser.InfixExpression:
		if l, r := provableScalarType(e.Left), provableScalarType(e.Right); l != "" && r != "" {
			// 比較運算的結果是 bool，其餘算術／位運算沿用左孩子（純量）。
			switch e.Operator {
			case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
				return "bool"
			}
			return l
		}
	}
	return ""
}

// checkStructAssignScalar 是上面政策在 let 陳述式上的落點：宣告型別是結構體、
// 右值可證明為純量時報 ERROR。
//
// 例外（用戶定調）：**賦值**位置上「整數 → 有唯一無損構造子的結構體」允許隱式
// 提升。此處必須與 build 的提升 pass 讀同一張表（scalar_promote.go）——checker
// 放行而後端沒提升，就等於把本檔要消滅的靜默誤編譯放回來。
func checkStructAssignScalar(name, declared string, value parser.Expression, structFields map[string]map[string]string) []ValidateResult {
	if name == "" || name == "_" || value == nil {
		return nil
	}
	base, wrapped := structAssignShape(declared, structFields)
	if base == "" {
		return nil
	}
	scalar := provableScalarType(value)
	if scalar == "" || scalar == "nil" {
		return nil
	}
	if !wrapped && ScalarPromotionCtor(base, scalar) != "" {
		return nil
	}
	pos := value.Pos()
	return []ValidateResult{{
		TraceID: "structasg",
		Line:    pos.Line,
		Column:  pos.Column,
		Message: fmt.Sprintf("cannot assign %s value to %s variable '%s': a struct value must be constructed explicitly (e.g. `%s { … }` or its constructor); an integer is promoted implicitly only into a struct that has one lossless constructor (such as bigint)",
			scalar, declared, name, base),
	}}
}
