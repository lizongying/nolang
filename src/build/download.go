package build

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cacheDir returns the local package cache directory.
func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".nolang", "cache", "packages")
	}
	return filepath.Join(home, ".nolang", "cache", "packages")
}

// parseGitHubKey extracts owner and repo from a dependency key.
// "github.com/lizongying/nolang/test2" → owner="lizongying", repo="nolang"
func parseGitHubKey(key string) (owner, repo string, ok bool) {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) < 3 || parts[0] != "github.com" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// downloadPackage downloads a package from GitHub Releases and caches it locally.
// It returns the package directory path within the extracted archive.
func downloadPackage(key, version string) (string, error) {
	owner, repo, ok := parseGitHubKey(key)
	if !ok {
		return "", fmt.Errorf("unsupported dependency key format: %s (only github.com is supported)", key)
	}

	// Determine cache path
	cachePath := filepath.Join(cacheDir(), owner, repo, version)
	shortName := packageShortName(key)
	pkgDir := filepath.Join(cachePath, shortName)

	// Check if already cached
	if info, err := os.Stat(pkgDir); err == nil && info.IsDir() {
		return pkgDir, nil
	}

	fmt.Printf("Downloading %s@%s...\n", key, version)

	// Use GitHub API to get release info
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, version)
	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nolang")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching release %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch release %s (status %d)", apiURL, resp.StatusCode)
	}

	// Parse the release JSON to find the download URL.
	// We need to find an asset that matches the archive format.
	// Parse the JSON response.
	releaseInfo, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading release info: %w", err)
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

	// Download the archive
	dlResp, err := client.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("downloading archive: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download archive (status %d)", dlResp.StatusCode)
	}

	// Create cache directory
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Extract archive
	if strings.HasSuffix(assetURL, ".zip") {
		if err := extractZip(dlResp.Body, cachePath); err != nil {
			return "", fmt.Errorf("extracting zip archive: %w", err)
		}
	} else {
		if err := extractTarGz(dlResp.Body, cachePath); err != nil {
			return "", fmt.Errorf("extracting tar.gz archive: %w", err)
		}
	}

	// Find the actual package directory within the extracted content
	// by looking for workspace.jsonc or the shortName directory.
	actualDir := findPackageDir(cachePath, shortName)
	if actualDir == "" {
		return "", fmt.Errorf("package %s not found in extracted archive", shortName)
	}

	return actualDir, nil
}

// extractDownloadURL does a simple search for browser_download_url in the JSON response.
func extractDownloadURL(jsonStr string) string {
	// Simple approach: find "browser_download_url" values
	marker := `"browser_download_url":"`
	idx := strings.Index(jsonStr, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := strings.Index(jsonStr[start:], `"`)
	if end < 0 {
		return ""
	}
	return jsonStr[start : start+end]
}

// extractTarGz extracts a tar.gz archive from r into destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Sanitize path
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// extractZip extracts a zip archive from r into destDir.
func extractZip(r io.Reader, destDir string) error {
	// Read entire zip data (needed for zip.Reader)
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading zip data: %w", err)
	}

	// Use bytes.NewReader which implements io.ReaderAt
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// Fall back to manual extraction using OS unzip command
		return extractZipFallback(data, destDir)
	}

	for _, f := range zipReader.File {
		fpath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}

		dir := filepath.Dir(fpath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.Create(fpath)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// extractZipFallback uses the system unzip command as a fallback.
func extractZipFallback(data []byte, destDir string) error {
	tmpFile := filepath.Join(destDir, "_tmp_archive.zip")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	return execCommand("unzip", "-o", tmpFile, "-d", destDir)
}

// execCommand runs a system command.
func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// findPackageDir looks for the package directory within the extracted cache path.
// It first checks for workspace.jsonc, then falls back to looking for the shortName directory.
func findPackageDir(cachePath, shortName string) string {
	// Check if shortName directory exists directly
	pkgDir := filepath.Join(cachePath, shortName)
	if info, err := os.Stat(pkgDir); err == nil && info.IsDir() {
		return pkgDir
	}

	// Look for workspace.jsonc to find the actual package mapping
	wsFile := filepath.Join(cachePath, "workspace.jsonc")
	if _, err := os.Stat(wsFile); err == nil {
		raw, err := os.ReadFile(wsFile)
		if err == nil {
			cleaned := stripJSONC(raw)
			var ws WorkspaceMap
			if err := json.Unmarshal(cleaned, &ws); err == nil {
				if localPath, exists := ws[shortName]; exists {
					dir := filepath.Join(cachePath, localPath)
					if info, err := os.Stat(dir); err == nil && info.IsDir() {
						return filepath.Clean(dir)
					}
				}
			}
		}
	}

	// Fallback: return the first subdirectory found
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(cachePath, entry.Name())
		}
	}

	return ""
}
