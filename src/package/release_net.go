//go:build !wasip1

package pkg

// 這個檔案刻意帶 `!wasip1` 約束：GitHub Releases API / 資產下載都要 net/http，
// 它會把 sock_accept / sock_shutdown 這兩個 wasmimport 鏈進 wasip1 產物，
// 而瀏覽器裡的 WASI 實作（@wasmer/wasi）沒有實作，`WebAssembly.Instance`
// 直接 LinkError。wasip1 側只保留報錯樁（release_net_wasip1.go）：
// 瀏覽器裡既沒有 socket 也沒有落盤目錄，安裝工具本就無意義。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lizongying/nolang/i18n"
)

// LatestReleaseTag 查詢倉庫最新的 release tag（GitHub Releases API）。
// 未設定 $GITHUB_TOKEN / $GH_TOKEN 時走匿名請求，受 GitHub 每小時 60 次限流約束。
func LatestReleaseTag(host, owner, repo string) (string, error) {
	if host != defaultToolHost {
		return "", i18n.Errorf("1m1ocf8t", "unsupported host %q (only github.com is supported)", host)
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	body, err := githubGET(apiURL)
	if err != nil {
		return "", err
	}
	var info struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", i18n.Errorf("3owkeirf", "parsing release info for %s/%s: %w", owner, repo, err)
	}
	if info.TagName == "" {
		return "", i18n.Errorf("ectmoin3", "no release found for %s/%s", owner, repo)
	}
	return info.TagName, nil
}

// DownloadReleaseAsset 下載 url 指向的 release 資產，回傳其內容。
func DownloadReleaseAsset(url string) ([]byte, error) {
	return githubGET(url)
}

// githubGET 發一個帶 User-Agent（與 DownloadPackage 一致）與可選 Authorization
// 的 GET。傳輸層失敗（連線中斷 / EOF）最多重試 3 次——release 下載走 GitHub 的
// 重定向 + CDN，偶發 EOF 很常見；HTTP 狀態碼錯誤（404 資產不存在、403 限流）不重試。
func githubGET(url string) ([]byte, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			fmt.Printf("  retrying (%d/%d)...\n", attempt-1, maxAttempts-1)
			time.Sleep(time.Duration(attempt-1) * 500 * time.Millisecond)
		}
		data, httpErr, err := githubGETOnce(url)
		if err == nil {
			return data, nil
		}
		if httpErr {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// maxAssetBytes 是單個 release 資產允許下載的最大體積（256 MiB）。
// 防止被劫持或惡意的來源返回超大響應體把進程讀到 OOM。
const maxAssetBytes = 256 << 20

// githubHTTPClient 帶整體超時，避免慢速／掛死的伺服器讓 no install 永久阻塞
// （http.DefaultClient 沒有 Timeout，會無限期等待）。
var githubHTTPClient = &http.Client{Timeout: time.Hour}

// githubGETOnce 執行一次 GET。httpErr 為 true 表示伺服器回了非 200 的狀態碼
// （資產不存在／限流），這類錯誤重試不會改變結果；false 表示傳輸層失敗，值得重試。
func githubGETOnce(url string) (data []byte, httpErr bool, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, false, i18n.Errorf("g391pqjl", "creating request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "nolang")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, false, i18n.Errorf("rsz5rmmt", "fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	// 限制響應體大小：超出 maxAssetBytes 直接失敗（重試也不會改變結果）。
	data, err = io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxAssetBytes))
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			return nil, true, i18n.Errorf("6p2o4902", "asset %s exceeds size limit (%d bytes)", url, maxAssetBytes)
		}
		return nil, false, i18n.Errorf("k1h5kums", "reading %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, true, i18n.Errorf("g9elmost", "fetching %s (status %d)", url, resp.StatusCode)
	}
	return data, false, nil
}
