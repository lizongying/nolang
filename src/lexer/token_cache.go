package lexer

import "sync"

// tokenCache 是包級別的 token 緩存，按文件路徑索引。
// 多個 Lexer 實例可以共享同一份 allTokens 切片，因為每個 Lexer
// 有自己的 pos / prevTokenType，allTokens 本身在構造後不可變。
//
// 設計目標：
//   - no build / run / test / vet 等命令在處理多個文件時，
//     共享 std 模組等公共依賴的 token，避免重複詞法分析。
//   - 並行安全：讀取用 RWMutex 保護，寫入用互斥鎖。
//   - 不可變性：allTokens 在 New() 構造後不再修改，
//     多個 Lexer 共享同一底層切片是安全的。
var (
	tokenCacheMu sync.RWMutex
	tokenCache   = make(map[string][]Token)
)

// NewCached 返回一個 Lexer，其 allTokens 優先從全局緩存讀取。
// key 為文件的唯一標識（通常是文件路徑或 embed 路徑）。
// 若 key 為空字串，則不使用緩存，直接創建新的 Lexer。
// 若緩存未命中，則執行完整的詞法分析並寫入緩存。
//
// 緩存命中時，直接共享底層 []Token 切片（不複製），
// 每個 Lexer 實例維護獨立的 pos / prevTokenType，互不干擾。
func NewCached(key string, source string) *Lexer {
	// 空 key 不緩存，避免不同源碼碰撞到同一快取條目
	if key == "" {
		return New(source)
	}

	tokenCacheMu.RLock()
	tokens, ok := tokenCache[key]
	tokenCacheMu.RUnlock()

	if ok {
		return &Lexer{
			allTokens: tokens,
			pos:       0,
		}
	}

	l := New(source)

	tokenCacheMu.Lock()
	tokenCache[key] = l.allTokens
	tokenCacheMu.Unlock()

	return l
}

// ClearTokenCache 清空全局 token 緩存。
// 在命令級別（而非每文件級別）調用：一個 no build / no test 命令
// 開始時清空一次，之後所有文件共享同一份緩存。
func ClearTokenCache() {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()
	tokenCache = make(map[string][]Token)
}
