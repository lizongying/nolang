package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	nbuild "github.com/lizongying/nolang/build"
	pkg "github.com/lizongying/nolang/package"
)

// installCommand 處理 `no install`：
//
//	no install                                        以當前目錄的套件建構並安裝
//	no install [-u] <tool>[:<version>]@<host>/<owner>/<repo>
//	                                                  從 GitHub Releases 下載預編二進位
//
// 遠端形式抓的是**二進位**而不是源碼：資產名由工具名 + `no` 自身所在平台決定
// （<tool>-<goos>-<goarch>，Windows 帶 .exe），因此裝進來的就是可直接執行的檔案，
// 不需要本地 LLVM 工具鏈、也不需要該工具是 Nolang 寫的。
func installCommand(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	update := fs.Bool("u", false, "force re-download / re-install")
	fs.Usage = func() {
		fmt.Println("Usage: no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]")
		fmt.Println("")
		fmt.Println("Install a binary to ~/no/bin/ and symlink it into /usr/local/bin/.")
		fmt.Println("")
		fmt.Println("With no argument: build the package in the current directory and")
		fmt.Println("install the resulting binary (needs a local LLVM toolchain).")
		fmt.Println("")
		fmt.Println("With a tool spec: download a prebuilt release asset for the platform")
		fmt.Println("`no` itself runs on — no toolchain, no sources, the tool need not be")
		fmt.Println("written in Nolang.")
		fmt.Println("")
		fmt.Println("Spec format:")
		fmt.Println("  <tool>[:<version>]@<host>/<owner>/<repo>")
		fmt.Println("    tool             asset name prefix; also the installed command name")
		fmt.Println("    version          release tag, e.g. v0.1.33 (default: latest release)")
		fmt.Println("    host/owner/repo  GitHub repository; 'github.com' may be omitted")
		fmt.Println("")
		fmt.Println("Asset lookup: <tool>-<os>-<arch> (plus .exe on Windows), where")
		fmt.Println("<os>-<arch> is the platform of the running `no`, e.g.:")
		fmt.Println("  linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no install                                       build & install current package")
		fmt.Println("  no install -u                                    force rebuild current package")
		fmt.Println("  no install nogit@github.com/lizongying/notools   install latest nogit")
		fmt.Println("  no install nogit:v0.1.33@github.com/lizongying/notools")
		fmt.Println("  no install nogit@lizongying/notools              host defaults to github.com")
		fmt.Println("  no install -u nogit@github.com/lizongying/notools  force re-download")
		fmt.Println("")
		fmt.Println("Set $GITHUB_TOKEN (or $GH_TOKEN) to avoid GitHub API rate limits.")
	}
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		installCurrentPackage(*update)
		return
	}

	code := 0
	for _, spec := range remaining {
		if err := installReleaseBinary(spec, *update); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			code = 1
		}
	}
	if code != 0 {
		os.Exit(code)
	}
}

// installCurrentPackage 建構當前目錄的套件並把產物裝進 ~/no/bin/。
func installCurrentPackage(force bool) {
	p, err := nbuild.LoadPackage(".")
	if err != nil || p == nil {
		fmt.Fprintf(os.Stderr, "Error: package.noml not found in current directory\n")
		return
	}
	name := p.Name
	if force {
		fmt.Printf("Updating current package: %s\n", name)
	} else {
		fmt.Printf("Installing current package: %s\n", name)
	}

	tmpDir, err := os.MkdirTemp("", "nolang-install")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	if os.Getenv("NOLANG_KEEP_IR") == "" {
		defer os.RemoveAll(tmpDir)
	} else {
		fmt.Fprintf(os.Stderr, "[debug] keep build tmp dir: %s\n", tmpDir)
	}

	outPath := filepath.Join(tmpDir, name)
	opts := nbuild.BuildOptions{
		CC:              "clang",
		Output:          outPath,
		Verbose:         verbose,
		CompilerVersion: version,
	}

	fmt.Printf("Building %s...\n", name)
	if _, err := nbuild.BuildFile(".", opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error building: %v\n", err)
		return
	}
	if err := installBuiltBinary(outPath, name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

// installReleaseBinary 解析 spec、定位 release 資產、下載並安裝。
func installReleaseBinary(specStr string, force bool) error {
	spec, err := pkg.ParseToolSpec(specStr)
	if err != nil {
		return err
	}
	binName := pkg.ToolBinaryName(spec.Name, runtime.GOOS)
	dst := filepath.Join(nolangBinDir(), binName)

	if !force {
		if _, serr := os.Stat(dst); serr == nil {
			return fmt.Errorf("%s already installed (%s); use -u to reinstall or update", spec.Name, dst)
		}
	}

	ver := spec.Version
	if ver == "" {
		ver, err = pkg.LatestReleaseTag(spec.Host, spec.Owner, spec.Repo)
		if err != nil {
			return fmt.Errorf("resolving latest release: %w", err)
		}
		fmt.Printf("Latest release of %s/%s: %s\n", spec.Owner, spec.Repo, ver)
	}

	url := pkg.ReleaseAssetURL(spec, ver, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Downloading %s %s (%s-%s)...\n", spec.Name, ver, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  %s\n", url)
	data, err := pkg.DownloadReleaseAsset(url)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("downloaded asset is empty: %s", url)
	}

	if err := installBytes(data, binName); err != nil {
		return err
	}
	fmt.Printf("Installed %s %s\n", spec.Name, ver)
	fmt.Printf("  binary:  %s\n", dst)
	fmt.Printf("  symlink: %s\n", filepath.Join(pathBinDir(), binName))
	return nil
}

// installBuiltBinary 把已建構在 srcPath 的二進位搬到 ~/no/bin/ 並建立軟鏈接。
func installBuiltBinary(srcPath, name string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading built binary: %w", err)
	}
	return installBytes(data, pkg.ToolBinaryName(name, runtime.GOOS))
}

// installBytes 把二進位內容寫進 ~/no/bin/<name>（0755）並在 /usr/local/bin/ 建立軟鏈接。
func installBytes(data []byte, name string) error {
	binDir := nolangBinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", binDir, err)
	}
	dst := filepath.Join(binDir, name)
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return symlinkIntoPathBin(dst, name)
}

// symlinkIntoPathBin 在 /usr/local/bin/<name> 建立指向 dst 的軟鏈接。
// 權限不足時不視為致命錯誤：二進位已經裝好，印出可手動執行的命令即可。
func symlinkIntoPathBin(dst, name string) error {
	dir := pathBinDir()
	symlink := filepath.Join(dir, name)
	if lerr := os.MkdirAll(dir, 0755); lerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot create %s: %v\n", dir, lerr)
		fmt.Printf("Try: sudo ln -sf %s %s\n", dst, symlink)
		return nil
	}
	// 同名條目已存在時必須先移除，否則 os.Symlink 會報 EEXIST（即便是軟鏈接）。
	if _, err := os.Lstat(symlink); err == nil {
		if rerr := removePathEntry(symlink); rerr != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot replace %s: %v\n", symlink, rerr)
			fmt.Printf("Try: sudo ln -sf %s %s\n", dst, symlink)
			return nil
		}
	}
	if err := os.Symlink(dst, symlink); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot create symlink %s: %v\n", symlink, err)
		fmt.Printf("Try: sudo ln -sf %s %s\n", dst, symlink)
		return nil
	}
	return nil
}

// removePathEntry 移除 path 下的條目（軟鏈接或檔案）。
//
// macOS 26 起 /usr/local/bin 下新建的條目會被系統打上 com.apple.provenance
// 擴展屬性，unlink 只允許持有對應 entitlement 的進程執行；我們的 Go 二進位
// 拿 EPERM（rename 覆蓋同樣被拒），但 Apple 簽名的 /bin/rm 可以。故先試
// os.Remove，失敗則退回 `rm -f`，兩者都失敗才回報錯誤。
func removePathEntry(p string) error {
	err := os.Remove(p)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "wasip1" {
		if rerr := exec.Command("rm", "-f", p).Run(); rerr == nil {
			return nil
		}
	}
	return err
}

// pathBinDir 返回放置軟鏈接的系統目錄 /usr/local/bin。
func pathBinDir() string { return "/usr/local/bin" }

func uninstallCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: no uninstall <name>")
		fmt.Println("")
		fmt.Println("Remove an installed binary and its symlink.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no uninstall nogit    remove ~/no/bin/nogit and /usr/local/bin/nogit")
		return
	}

	name := args[0]
	binDir := nolangBinDir()
	symlink := filepath.Join(pathBinDir(), pkg.ToolBinaryName(name, runtime.GOOS))

	removed := false

	// 移除軟鏈接。失敗不中斷——binary 仍應被清掉（見 removePathEntry 關於
	// macOS provenance 的說明）。
	if _, err := os.Lstat(symlink); err == nil {
		if err := removePathEntry(symlink); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing symlink %s: %v\n", symlink, err)
			fmt.Printf("Try: sudo rm -f %s\n", symlink)
		} else {
			fmt.Printf("Removed symlink: %s\n", symlink)
			removed = true
		}
	} else {
		fmt.Printf("Symlink not found: %s\n", symlink)
	}

	// 移除 binary（Windows 上帶 .exe，Unix 上不帶）
	binary := filepath.Join(binDir, pkg.ToolBinaryName(name, runtime.GOOS))
	if _, err := os.Stat(binary); err == nil {
		if err := removePathEntry(binary); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing %s: %v\n", binary, err)
			fmt.Printf("Try: rm -f %s\n", binary)
			return
		}
		fmt.Printf("Removed binary: %s\n", binary)
		removed = true
	} else {
		fmt.Printf("Binary not found: %s\n", binary)
	}

	if !removed {
		fmt.Printf("%s is not installed.\n", name)
	}
}
