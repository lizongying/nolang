package checker

// scalar_promote.go — 「整數 → 具名結構體」的隱式提升表。
//
// 政策（用戶定調）：**賦值**時，若目標型別存在唯一、無損、無歧義的轉換途径，
// 允許隱式提升 —— `k i64 = 2` 之後 `x bigint = k` 是安全且明確的，不该要求
// 使用者每次都手寫 `bigint.from-i64(k)`。運算時則相反：有明確處理方式（如
// bigint 運算符重載的 i64 快速臂）優先走它，那屬最佳化；兩側都無路可走的
// 型別組合（如 `?byte` 進 `big`）一律由 unionarg / structasg 拒收報斷。
//
// 落點分工：
//   - 本表是 checker 與 build 的**唯一事實來源**：checker 據此決定
//     structasg 要不要報（可提升 ⇒ 不報），build 據此在裝配工作副本上把
//     `x bigint = <整數>` 改寫成 `x bigint = bigint.from-i64(<整數>)`
//     （見 build/transpiler_promote.go）。兩邊讀同一張表，才不會出現
//     「checker 放行、後端沒提升」的靜默誤編譯 —— 那正是本表要消滅的東西。
//   - 刻意只收「 widen 無損」的整數源型別：u64/u128/i128 可能超出 i64 值域，
//     f* 有小數／精度問題，str/bool/char 語義不明，一律不提升。
//   - 大整數（u64/i128）的轉換必須顯式寫 `bigint.from-u64` 等專用構造子，
//     因為同一個源型別可能對應多個構造子，此時提升不再「明確」。

// scalarPromotion 是一個結構體的隱式提升規則：可接受的源型別集合 +
// 唯一轉換构造函数（源碼書寫形態，模組限定名）。
//
// mod 是構造子所在的模組短名。它同時用來卡住「同名不同家」：用戶在自己檔案裡
// 宣告一個自己的 `bigint` 結構體時，末段裸名會撞上一樣的規則，若不看來源就
// 合成 `bigint.from-i64(...)`，那會指向另一家的構造子，又是一次靜默誤編譯。
type scalarPromotion struct {
	mod  string
	ctor string
	from map[string]bool
}

// intPromotableSources 是能被無損塞進 i64 的整數源型別。
var intPromotableSources = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true,
	"byte": true, "u8": true, "u16": true, "u32": true,
}

// scalarPromotions 以結構體裸名為鍵。新增條目前請確認：該源型別到該結構體
// 的轉換存在**唯一**且**無損**的构造函数（例如 bigint 同時有 from-i64 與
// from-u64 兩個吃 i64 的構造子，所以 u64 絕不能放進 from 集合 —— 那有歧義）。
var scalarPromotions = map[string]scalarPromotion{
	"bigint": {mod: "bigint", ctor: "bigint.from-i64", from: intPromotableSources},
}

// ScalarPromotionCtor 回報把 `scalarType` 的值隱式提升進 `structName` 所需的
// 构造函数源碼名；不存在安全且明確的提升途径時回 ""。
//
// structName 允許帶模組限定前綴（`bigint.bigint`），但前綴必須等於規則的 mod
// 才算數——末段同名的外地結構體不吃這條規則。
//
// 兩個名字都只認**裸拼寫**：帶 `?` / `&` 修飾的一律回 ""。目標是 option 時
// 提升還要多打一次 tag，後端沒有那一步；來源是 option 時值可能是 err/nil，
// 提升會丟掉失敗分支 —— 兩處都不「安全且明確」。
func ScalarPromotionCtor(structName, scalarType string) string {
	if structName == "" || scalarType == "" {
		return ""
	}
	if structName[0] == '?' || structName[0] == '&' ||
		scalarType[0] == '?' || scalarType[0] == '&' {
		return ""
	}
	tail := structName
	if i := lastDot(tail); i >= 0 {
		mod := tail[:i]
		tail = tail[i+1:]
		if scalarPromotions[tail].mod != mod {
			return ""
		}
	}
	rule, ok := scalarPromotions[tail]
	if !ok || !rule.from[scalarType] {
		return ""
	}
	return rule.ctor
}

// IsPromotableTarget 回報 t 是否為「有提升規則的結構體型別」（不看源型別，只認
// 目標）。build 用它篩出需要入型別表的宣告 —— 只有這種目標才可能被改寫。
// 名字規則與 ScalarPromotionCtor 一致：裸名或 `mod.Name` 且 mod 等於規則的 mod。
func IsPromotableTarget(t string) bool {
	if t == "" || t[0] == '?' || t[0] == '&' {
		return false
	}
	name := t
	if i := lastDot(name); i >= 0 {
		mod := name[:i]
		name = name[i+1:]
		if scalarPromotions[name].mod != mod {
			return false
		}
	}
	_, ok := scalarPromotions[name]
	return ok
}

// IsPromotableSource 回報 t 是否為某條規則接受的整數源型別。build 的提升 pass
// 用它決定「識別字的型別算不算整數」，只有這裡認得的型別才允許被包進構造子。
func IsPromotableSource(t string) bool {
	if t == "" || t[0] == '?' || t[0] == '&' {
		return false
	}
	for _, rule := range scalarPromotions {
		if rule.from[t] {
			return true
		}
	}
	return false
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
