//go:build !wasip1

package pkg

// 這個檔案刻意帶 `!wasip1` 約束：函式體裡的 net/http 會把 Go 的 socket 系
// wasmimport（sock_accept / sock_shutdown）鏈進 wasip1 產物，而瀏覽器裡的
// WASI 實作（@wasmer/wasi）沒有這兩個導入，`WebAssembly.Instance` 直接
// LinkError ⇒ playground 整個執行鏈路失敗。wasip1 側只保留報錯樁
// （download_net_wasip1.go），下載在瀏覽器沙箱裡本來就無意義。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// DownloadPackage downloads a package from GitHub Releases and caches it locally.
// It returns the package directory path and the SHA256 hash of the downloaded archive.
func DownloadPackage(key, version string, mirrors []string) (pkgDir string, archiveHash string, err error) {
	owner, repo, ok := parseGitHubKey(key)
	if !ok {
		return "", "", i18n.Errorf("7mitslag", "unsupported dependency key format: %s (only github.com is supported)", key)
	}

	// Determine cache path
	cachePath := filepath.Join(cacheDir(), owner, repo, version)
	shortName := PackageShortName(key)
	pkgDir = filepath.Join(cachePath, shortName)

	// Check if already cached
	if info, err := os.Stat(pkgDir); err == nil && info.IsDir() {
		// Try to compute hash from saved archive
		// Try both .tar.gz and .zip
		for _, name := range []string{"_archive.tar.gz", "_archive.zip"} {
			archivePath := filepath.Join(cachePath, name)
			if hash, err := archiveHashFromFile(archivePath); err == nil {
				return pkgDir, hash, nil
			}
		}
		// Archive not found, return empty hash (no sum entry for cached packages without archive)
		return pkgDir, "", nil
	}

	fmt.Printf("Downloading %s@%s...\n", key, version)

	// Create HTTP client
	client := &http.Client{}

	// Use GitHub API to get release info
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, version)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", "", i18n.Errorf("g391pqjl", "creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nolang")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", i18n.Errorf("o4wq2axo", "fetching release %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", i18n.Errorf("5jtjxzmz", "failed to fetch release %s (status %d)", apiURL, resp.StatusCode)
	}

	// Parse the release JSON to find the download URL.
	// We need to find an asset that matches the archive format.
	// Parse the JSON response.
	releaseInfo, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", i18n.Errorf("zs1q169p", "reading release info: %w", err)
	}

	// Simple JSON parsing to find the first asset with .tar.gz or .zip.
	// Look for "browser_download_url" in the response.
	// For simplicity, construct the URL following GitHub's convention:
	// https://github.com/{owner}/{repo}/releases/download/{tag}/{repo}-{version}.tar.gz
	fallbackURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s-%s.tar.gz", owner, repo, version, repo, version)

	// Try to find a proper asset URL from the API response
	assetURL := extractDownloadURL(string(releaseInfo))
	if assetURL == "" {
		assetURL = fallbackURL
	}

	// Collect download URLs: mirrors first, then original
	downloadURLs := []string{assetURL}
	for _, mirror := range mirrors {
		mirror = strings.TrimRight(mirror, "/")
		mirrorURL := strings.Replace(assetURL, "https://github.com", mirror, 1)
		if mirrorURL != assetURL {
			downloadURLs = append([]string{mirrorURL}, downloadURLs...)
		}
	}

	// Try each URL in order
	var data []byte
	var downloadErr error
	for _, url := range downloadURLs {
		if url == assetURL {
			fmt.Printf("  Downloading from %s\n", url)
		} else {
			fmt.Printf("  Trying mirror: %s\n", url)
		}

		dlResp, err := client.Get(url)
		if err != nil {
			downloadErr = i18n.Errorf("vmy4qtcr", "downloading from %s: %w", url, err)
			continue
		}

		if dlResp.StatusCode != http.StatusOK {
			downloadErr = i18n.Errorf("r6p4a75i", "downloading from %s (status %d)", url, dlResp.StatusCode)
			dlResp.Body.Close()
			continue
		}

		// Read all bytes for hashing
		data, err = io.ReadAll(dlResp.Body)
		dlResp.Body.Close()
		if err != nil {
			downloadErr = i18n.Errorf("lqcz5x45", "reading archive data from %s: %w", url, err)
			continue
		}

		downloadErr = nil
		break
	}

	if downloadErr != nil {
		return "", "", i18n.Errorf("duufpprw", "all download attempts failed: %w", downloadErr)
	}

	// Compute SHA256 of the raw archive
	h := sha256.Sum256(data)
	archiveHash = hex.EncodeToString(h[:])

	// Create cache directory
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return "", "", i18n.Errorf("2rkb38mw", "creating cache directory: %w", err)
	}

	// Save archive to cache for later re-hashing
	archivePath := archiveFilePath(cachePath, assetURL)
	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		fmt.Printf("Warning: failed to cache archive: %v\n", err)
	}

	// Extract archive from buffered data
	r := bytes.NewReader(data)
	if strings.HasSuffix(assetURL, ".zip") {
		if err := extractZip(r, cachePath); err != nil {
			return "", "", i18n.Errorf("fs4qotyl", "extracting zip archive: %w", err)
		}
	} else {
		if err := extractTarGz(r, cachePath); err != nil {
			return "", "", i18n.Errorf("eit5cjgr", "extracting tar.gz archive: %w", err)
		}
	}

	// Find the actual package directory within the extracted content
	// by looking for workspace.noml or the shortName directory.
	actualDir := findPackageDir(cachePath, shortName)
	if actualDir == "" {
		return "", "", i18n.Errorf("9edsvha2", "package %s not found in extracted archive", shortName)
	}

	return actualDir, archiveHash, nil
}
