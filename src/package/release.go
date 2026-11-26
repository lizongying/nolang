package pkg

import (
	"fmt"
	"os"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// ToolSpec 是 `no install <tool>[:<version>]@<host>/<owner>/<repo>` 的解析結果：
// 從 GitHub Releases 抓取預編二進位資產（如 nogit-linux-amd64-v0.1.33）並安裝到 ~/no/bin/。
//
// 之所以是「工具名 + 倉庫」兩級而不是「包 = 倉庫」：一個倉庫可以產出多個可執行檔
// （notools 就有 nogit / noimg / nonpm / nouv / noagent …），資產名前綴決定抓哪一個，
// 倉庫只決定去哪個 release 抓。
type ToolSpec struct {
	Name    string // 工具／二進位名稱，同時是安裝後的命令名，如 "nogit"
	Version string // release tag；空字串（或 "latest"）表示取最新 release
	Host    string // 如 "github.com"
	Owner   string // 如 "lizongying"
	Repo    string // 如 "notools"
}

// defaultToolHost 是 spec 省略 host 時使用的倉庫託管方。
const defaultToolHost = "github.com"

// ParseToolSpec 解析安裝目標字串，支援以下寫法：
//
//	nogit:v0.1.33@github.com/lizongying/notools   -> 指定版本
//	nogit@github.com/lizongying/notools           -> 最新版本
//	nogit@lizongying/notools                      -> host 省略，預設 github.com
//	nogit:latest@github.com/lizongying/notools    -> 同「最新版本」
//
// 刻意不做的事：不猜工具名（沒有倉庫就無法定位 release），不支援非 GitHub 託管
// （Release 資產的 URL 形狀是 GitHub 專有的，與 DownloadPackage 的約定一致）。
func ParseToolSpec(s string) (ToolSpec, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return ToolSpec{}, i18n.Errorf("j3u5n35e", "empty install target")
	}
	// 用最後一個 '@' 切分：左側是 <tool>[:<version>]，右側是倉庫座標。
	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return ToolSpec{}, i18n.Errorf("dt39zm0i", "invalid install target %q: missing repository; want <tool>[:<version>]@<host>/<owner>/<repo>", raw)
	}
	left := strings.TrimSpace(raw[:at])
	right := strings.TrimSpace(raw[at+1:])

	var spec ToolSpec
	if idx := strings.Index(left, ":"); idx >= 0 {
		spec.Name = strings.TrimSpace(left[:idx])
		spec.Version = strings.TrimSpace(left[idx+1:])
	} else {
		spec.Name = left
	}
	if spec.Name == "" {
		return ToolSpec{}, i18n.Errorf("3gcfur0y", "invalid install target %q: missing tool name", raw)
	}
	if strings.ContainsAny(spec.Name, `/\`) {
		return ToolSpec{}, i18n.Errorf("ocsanpe6", "invalid install target %q: tool name must not contain a path separator", raw)
	}
	if spec.Version == "latest" || spec.Version == "*" {
		spec.Version = ""
	}

	host, owner, repo, ok := parseToolRepo(right)
	if !ok {
		return ToolSpec{}, i18n.Errorf("flfh2ueu", "invalid install target %q: repository must be <host>/<owner>/<repo> or <owner>/<repo>", raw)
	}
	spec.Host, spec.Owner, spec.Repo = host, owner, repo
	return spec, nil
}

// parseToolRepo 把倉庫座標拆成 (host, owner, repo)。含網址 scheme 時先剝除；
// 第一段不含 "." 時視為省略 host（補 github.com）。
func parseToolRepo(s string) (host, owner, repo string, ok bool) {
	t := strings.TrimSuffix(strings.TrimSpace(s), "/")
	t = strings.TrimPrefix(t, "https://")
	t = strings.TrimPrefix(t, "http://")
	parts := strings.Split(t, "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// host 一定含 "."（github.com），否則是 "owner/repo" 簡寫。
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		return parts[0], parts[1], parts[2], parts[1] != "" && parts[2] != ""
	}
	if len(parts) >= 2 {
		return defaultToolHost, parts[0], parts[1], parts[0] != "" && parts[1] != ""
	}
	return "", "", "", false
}

// String 回傳 spec 的正規書寫形式：<tool>[:<version>]@<host>/<owner>/<repo>。
func (t ToolSpec) String() string {
	if t.Version != "" {
		return fmt.Sprintf("%s:%s@%s/%s/%s", t.Name, t.Version, t.Host, t.Owner, t.Repo)
	}
	return fmt.Sprintf("%s@%s/%s/%s", t.Name, t.Host, t.Owner, t.Repo)
}

// RepoURL 回傳倉庫的 https 位址。
func (t ToolSpec) RepoURL() string {
	return fmt.Sprintf("https://%s/%s/%s", t.Host, t.Owner, t.Repo)
}

// DepKey 回傳 package.noml 的依賴鍵：<tool>@<host>/<owner>/<repo>。
// 刻意不含版本——版本是這個鍵的值。
func (t ToolSpec) DepKey() string {
	return fmt.Sprintf("%s@%s/%s/%s", t.Name, t.Host, t.Owner, t.Repo)
}

// ToolBinaryName 回傳安裝到磁碟上的檔名：Windows 需要 .exe 後綴才可執行。
// 這是統一命令名（~/no/bin 下的檔名與 /usr/local/bin 下的軟鏈接名一致），
// 刻意不帶版本／平台——版本只體現在 release 資產名（見 ReleaseAssetName）。
// goos 由呼叫端傳入（runtime.GOOS），以便單測覆蓋非宿主平台。
func ToolBinaryName(name, goos string) string {
	// 使用者可能直接寫 `no uninstall nogit.exe`，避免二次加後綴。
	if goos == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

// ReleaseAssetName 回傳 release 資產檔名：<tool>-<goos>-<goarch>-<version>（Windows 加 .exe）。
// version 是完整的 release tag（含前導 v），例如：
//
//	nogit-linux-amd64-v0.1.33, no-darwin-amd64-v0.4.35, no-windows-amd64-v0.4.35.exe
func ReleaseAssetName(name, version, goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("%s-%s-%s-%s.exe", name, goos, goarch, version)
	}
	return fmt.Sprintf("%s-%s-%s-%s", name, goos, goarch, version)
}

// ReleaseAssetURL 回傳給定版本的資產下載網址：
// https://<host>/<owner>/<repo>/releases/download/<version>/<tool>-<goos>-<goarch>-<version>
func ReleaseAssetURL(t ToolSpec, version, goos, goarch string) string {
	return fmt.Sprintf("%s/releases/download/%s/%s", t.RepoURL(), version, ReleaseAssetName(t.Name, version, goos, goarch))
}

// ReleaseASTAssetName 回傳預編 AST 包的資產檔名：<tool>-ast.zip。
// 與二進位資產（<tool>-<os>-<arch>-<version>）不同，AST 包與平台無關，所以檔名不帶 os/arch。
func ReleaseASTAssetName(name string) string {
	return name + "-ast.zip"
}

// ReleaseASTZipURL 回傳預編 AST 包的下載網址：
// https://<host>/<owner>/<repo>/releases/download/<version>/<tool>-ast.zip
func ReleaseASTZipURL(t ToolSpec, version string) string {
	// 本機回環（測試／本地 registry）沒有 TLS，改用 http——只對明確的 localhost /
	// 127.0.0.1 生效，真實倉庫一律 https。
	scheme := "https"
	if strings.HasPrefix(t.Host, "127.0.0.1") || strings.HasPrefix(t.Host, "localhost") {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s/%s/%s/releases/download/%s/%s",
		scheme, t.Host, t.Owner, t.Repo, version, ReleaseASTAssetName(t.Name))
}

// githubToken 讀取可用的 GitHub API token（避免匿名限流）。
func githubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	return os.Getenv("GH_TOKEN")
}
