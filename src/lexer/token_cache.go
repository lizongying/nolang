package lexer

import "github.com/lizongying/nolang/cache"

// tokenCache 是包級別的 token 緩存。
// 多個 Lexer 實例可以共享同一份 allTokens 切片，因為每個 Lexer
// 有自己的 pos / prevTokenType，allTokens 本身在構造後不可變。
//
// 設計目標：
//   - no build / run / test / vet 等命令在處理多個文件時，
//     共享 std 模組等公共依賴的 token，避免重複詞法分析。
//   - 並行安全：底層 LRU 自帶互斥鎖。
//   - 不可變性：allTokens 在 New() 構造後不再修改，
//     多個 Lexer 共享同一底層切片是安全的。
//   - 內容尋址：緩存鍵為「僅內容哈希」（見 cache.ContentKey），與路徑無關——
//     同一份源碼無論以 embed 路徑或磁盤路徑出現都命中同一份詞法結果；
//     且 LRU 有容量上限，常駐進程不會無限增長（見 cache 包）。
var tokenCache = cache.NewLRU[[]Token](4096)

// NewCached 返回一個 Lexer，其 allTokens 優先從全局緩存讀取。
// key 為文件的標識（通常是文件路徑或 embed 路徑），僅用於「空 key 不緩存」
// 的守衛；緩存鍵本身已改為「僅內容哈希」（見 cache.ContentKey），與路徑無關。
// 若 key 為空字串，則不使用緩存，直接創建新的 Lexer。
// 若緩存未命中，則執行完整的詞法分析並寫入緩存。
//
// 緩存命中時，直接共享底層 []Token 切片（不複製），
// 每個 Lexer 實例維護獨立的 pos / prevTokenType，互不干擾。
func NewCached(key string, source string) *Lexer {
	// 空 key 不緩存，保留原守衛語義（無法標識來源時不進快取）。
	if key == "" {
		return New(source)
	}
	// Token 是源碼的純函數：同一份內容無論來自 embed 路徑、磁盤路徑或重複
	// 文件，詞法結果都相同。緩存鍵改為「僅內容哈希」（去路徑分量），使
	// CollectStdModuleSignatures 從 embed 解析的 std 模塊，與 CompileTarget
	// 從磁盤解析的同一模塊命中同一份詞法結果，跨遍/跨文件復用詞法分析。
	// 原 (路徑,哈希) 複合鍵會令 std/foo.no 與 src/std/foo.no 鍵不同，
	// 導致 no vet src/std 等場景每個 std 文件被獨立 lex 兩遍。
	ck := cache.ContentKey(source)
	if tokens, ok := tokenCache.Get(ck); ok {
		return &Lexer{
			allTokens: tokens,
			pos:       0,
		}
	}

	l := New(source)
	tokenCache.Put(ck, l.allTokens)
	return l
}

// ClearTokenCache 清空全局 token 緩存。
// 在命令級別（而非每文件級別）調用：一個 no build / no test 命令
// 開始時清空一次，之後所有文件共享同一份緩存。
func ClearTokenCache() {
	tokenCache.Clear()
}
