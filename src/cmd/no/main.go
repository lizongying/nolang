package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	nbuild "github.com/lizongying/nolang/build"
	nfmt "github.com/lizongying/nolang/fmt"
)

type ProjectConfig struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Dependencies map[string]string `json:"dependencies"`
	Main         string            `json:"main"`
	Compiler     CompilerConfig    `json:"compiler"`
}

type CompilerConfig struct {
	Version string `json:"version"`
}

func main() {
	// 全局 flags
	for i, arg := range os.Args[1:] {
		if arg == "-v" {
			verbose = true
			// 從 os.Args 移除 -v
			os.Args = append(os.Args[:i+1], os.Args[i+2:]...)
			break
		}
	}

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]

	switch command {
	case "version":
		versionCommand()
		return
	case "init":
		initProject()
	case "new":
		if len(os.Args) < 3 {
			fmt.Println("Usage: no new <project-name>")
			return
		}
		newProject(os.Args[2])
	case "add":
		if len(os.Args) < 3 {
			fmt.Println("Usage: no add <package-name>")
			return
		}
		addDependency(os.Args[2])
	case "remove":
		if len(os.Args) < 3 {
			fmt.Println("Usage: no remove <package-name>")
			return
		}
		removeDependency(os.Args[2])
	case "update":
		if len(os.Args) < 3 {
			fmt.Println("Usage: no update <pkg>")
			return
		}
		updateDependency(os.Args[2])
	case "update-all":
		updateAllDependencies()
	case "list":
		listDependencies()
	case "install":
		installCommand(os.Args[2:])
	case "uninstall":
		uninstallCommand(os.Args[2:])
	case "pub":
		pubCommand(os.Args[2:])
	case "sync":
		syncDependencies()
	case "fmt":
		fmtCommand(os.Args[2:])
	case "build":
		buildCommand(os.Args[2:])
	case "run":
		runCommand(os.Args[2:])
	case "test":
		testCommand(os.Args[2:])
	case "vet":
		vetCommand(os.Args[2:])
	case "info":
		infoCommand()
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Printf("Nolang - A Programming Language (version %s)\n", version)
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("Global flags:")
	fmt.Println("  -v    Verbose mode (apply to all commands)")
	fmt.Println("")
	fmt.Println("  no version           Print version information")
	fmt.Println("")
	fmt.Println("  no fmt [flags] <file|dir>  Format source files")
	fmt.Println("    With no args in a terminal:")
	fmt.Println("      - if workspace.jsonc exists, format all its project dirs;")
	fmt.Println("      - otherwise, default to the current directory.")
	fmt.Println("    With piped stdin, reads from stdin (e.g. echo 'x=1' | no fmt).")
	fmt.Println("    Directories are processed recursively automatically.")
	fmt.Println("    Flags:")
	fmt.Println("      -w    write result to source file (in-place)")
	fmt.Println("      -d    output colored diff instead of formatted result")
	fmt.Println("    Examples:")
	fmt.Println("      no fmt                      list files in current dir needing formatting")
	fmt.Println("      no fmt -w                   format all .no files in current dir in-place")
	fmt.Println("      no fmt main.no              format and print to stdout")
	fmt.Println("      no fmt -w main.no           format file in-place")
	fmt.Println("      no fmt -d main.no           show colored diff")
	fmt.Println("      no fmt src/                 list .no files in src/ needing formatting")
	fmt.Println("      no fmt -w src/              format all .no files in src/ in-place")
	fmt.Println("      no fmt -d src/              show diff for all .no files in src/")
	fmt.Println("      echo 'x=1' | no fmt         format from stdin")
	fmt.Println("")
	fmt.Println("  no build [flags] [<file|dir>]  Build a Nolang project")
	fmt.Println("    If no file/dir is given and workspace.jsonc exists,")
	fmt.Println("    all projects in workspace.jsonc are built in parallel.")
	fmt.Println("    Flags:")
	fmt.Println("      -o <file>     Output file path")
	fmt.Println("      -cc <s>       C compiler: clang (default), zig")
	fmt.Println("      -target <s>   Target triple for cross-compilation")
	fmt.Println("                      e.g. x86_64-linux-gnu, aarch64-macos-gnu,")
	fmt.Println("                      x86_64-windows-gnu, wasm32-wasi")
	fmt.Println("      -js            Use JS backend (emit JavaScript, no LLVM toolchain)")
	fmt.Println("      -browser       Generate browser-targeted output (HTML + JS, use with -js)")
	fmt.Println("    For wasm32-wasi target, set $WASI_SYSROOT to your wasi-sysroot path.")
	fmt.Println("    Default: build current directory or workspace.jsonc projects")
	fmt.Println("    Examples:")
	fmt.Println("      no build")
	fmt.Println("      no build main.no")
	fmt.Println("      no build -o output main.no")
	fmt.Println("      no build -cc zig main.no")
	fmt.Println("      no build -target x86_64-linux-gnu main.no")
	fmt.Println("      no build -target wasm32-wasi main.no")
	fmt.Println("      no build --js main.no                     emit JavaScript (type erasure)")
	fmt.Println("      no build --js --browser main.no           emit browser JS + HTML wrapper")
	fmt.Println("")
	fmt.Println("  no run [<file|dir>]          Build and run")
	fmt.Println("    If directory, requires main.no (entry point).")
	fmt.Println("    Flags:")
	fmt.Println("      -js            Use JS backend (run with node)")
	fmt.Println("      -browser       Open in browser (use with -js)")
	fmt.Println("    Examples:")
	fmt.Println("      no run                     build and run main.no in current dir")
	fmt.Println("      no run main.no             build and run main.no")
	fmt.Println("      no run -cc zig main.no     build and run with Zig compiler")
	fmt.Println("      no run --js main.no            build via JS backend then run with node")
	fmt.Println("      no run --js --browser main.no  build browser JS + HTML and open in browser")
	fmt.Println("")
	fmt.Println("  no test [<file>]            Run tests")
	fmt.Println("    Defaults to tests/ directory.")
	fmt.Println("    Flags:")
	fmt.Println("      -cc <s>       C compiler: clang (default), zig")
	fmt.Println("      -target <s>   Target triple for cross-compilation")
	fmt.Println("                      e.g. x86_64-linux-gnu, aarch64-macos-gnu,")
	fmt.Println("                      x86_64-windows-gnu, wasm32-wasi")
	fmt.Println("    Examples:")
	fmt.Println("      no test")
	fmt.Println("      no test tests/my-test.no")
	fmt.Println("      no test -cc zig")
	fmt.Println("      no test -target x86_64-linux-gnu")
	fmt.Println("      no test -target wasm32-wasi")
	fmt.Println("")
	fmt.Println("  no vet [<file|dir>]            Validate source files")
	fmt.Println("    Examples:")
	fmt.Println("      no vet                     validate main.no in current dir")
	fmt.Println("      no vet main.no             validate main.no")
	fmt.Println("")
	fmt.Println("  no info               Show environment and source directory info")
	fmt.Println("")
	fmt.Println("  no add <pkg>        Add a dependency")
	fmt.Println("  no remove <pkg>     Remove a dependency")
	fmt.Println("  no update <pkg>     Update a specific dependency")
	fmt.Println("  no update-all        Update all dependencies")
	fmt.Println("  no list              List dependencies")
	fmt.Println("  no install [-u] [<pkg>@<version>]")
	fmt.Println("                    Install a package binary (store in ~/no/bin/, symlink in /usr/local/bin/)")
	fmt.Println("                    -u    force re-download and re-build")
	fmt.Println("  no uninstall <name>")
	fmt.Println("                    Remove installed package binary and symlink")
	fmt.Println("  no sync              Sync/download dependencies")
	fmt.Println("  no pub               Publish package")
	fmt.Println("")
	fmt.Println("")
}

// verbose 為全局 -v 旗標
var verbose = false

// version is injected at build time via -ldflags
var version = "dev"

// buildDate is injected at build time via -ldflags
var buildDate = ""

func versionCommand() {
	if buildDate != "" {
		if sec, err := strconv.ParseInt(buildDate, 10, 64); err == nil {
			t := time.Unix(sec, 0).UTC()
			fmt.Printf("version: %s(%s)\n", version, t.Format("2006-01-02 15:04:05"))
			return
		}
	}
	fmt.Printf("version: %s\n", version)
}

func infoCommand() {
	// Version
	if buildDate != "" {
		if sec, err := strconv.ParseInt(buildDate, 10, 64); err == nil {
			t := time.Unix(sec, 0).UTC()
			fmt.Printf("version:     %s (%s)\n", version, t.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("version:     %s\n", version)
		}
	} else {
		fmt.Printf("version:     %s\n", version)
	}

	// Binary path
	if exe, err := os.Executable(); err == nil {
		fmt.Printf("binary:      %s\n", exe)
	}

	// Std library source directory
	stdDir, stdSrc := nbuild.GetStdSourceDir()
	fmt.Printf("std source:  %s\n", stdDir)
	fmt.Printf("  resolved:  via %s", stdSrc)
	if stdSrc == "env" {
		fmt.Printf(" ($%s)", nbuild.NOLANG_STD_SRC)
	}
	fmt.Println()

	// Source directory (third-party / local development)
	srcDir, srcSrc := nbuild.GetSourceDir()
	fmt.Printf("source:      %s\n", srcDir)
	fmt.Printf("  resolved:  via %s", srcSrc)
	if srcSrc == "env" {
		fmt.Printf(" ($%s)", nbuild.NOLANG_SRC)
	}
	fmt.Println()

	// Environment variables
	stdEnvVal := os.Getenv(nbuild.NOLANG_STD_SRC)
	if stdEnvVal != "" {
		fmt.Printf("$%s: %s\n", nbuild.NOLANG_STD_SRC, stdEnvVal)
	} else {
		fmt.Printf("$%s: (not set)\n", nbuild.NOLANG_STD_SRC)
	}
	srcEnvVal := os.Getenv(nbuild.NOLANG_SRC)
	if srcEnvVal != "" {
		fmt.Printf("$%s:  %s\n", nbuild.NOLANG_SRC, srcEnvVal)
	} else {
		fmt.Printf("$%s:  (not set)\n", nbuild.NOLANG_SRC)
	}

	// Working directory
	if cwd, err := os.Getwd(); err == nil {
		fmt.Printf("workdir:     %s\n", cwd)
	}

	// Std module count
	modules := nbuild.GetStdModules()
	fmt.Printf("std modules: %d\n", len(modules))

	// Package info (if in a project)
	cwd, _ := os.Getwd()
	if pkg, _ := nbuild.LoadPackage(cwd); pkg != nil {
		fmt.Println()
		fmt.Println("project:")
		fmt.Printf("  root:      %s\n", pkg.RootDir)
		if len(pkg.Dependencies) > 0 {
			fmt.Printf("  deps:      %d\n", len(pkg.Dependencies))
			for name, ver := range pkg.Dependencies {
				fmt.Printf("    %s@%s\n", name, ver)
			}
		}
	}
}

func initProject() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	projectName := filepath.Base(dir)

	config := ProjectConfig{
		Name:        projectName,
		Version:     "0.1.0",
		Description: "A new Nolang project",
		Dependencies: map[string]string{
			"fmt": "*",
		},
		Main: "main.no",
		Compiler: CompilerConfig{
			Version: "0.1.0",
		},
	}

	createConfigFile(config)
	createMainFile()

	fmt.Printf("Project initialized in %s\n", dir)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - mod.jsonc (project configuration)")
	fmt.Println("  - main.no (main entry file)")
}

func newProject(name string) {
	err := os.MkdirAll(name, 0755)
	if err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	err = os.Chdir(name)
	if err != nil {
		fmt.Printf("Error changing directory: %v\n", err)
		return
	}

	config := ProjectConfig{
		Name:         name,
		Version:      "0.1.0",
		Description:  "A new Nolang project",
		Dependencies: map[string]string{},
		Main:         "main.no",
		Compiler: CompilerConfig{
			Version: "0.1.0",
		},
	}

	createConfigFile(config)
	createMainFile()
	createSrcDirectory()
	createLibFile()
	createTestDirectory()

	fmt.Printf("Project created: %s\n", name)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - mod.jsonc (project configuration)")
	fmt.Println("  - main.no (main entry file)")
	fmt.Println("  - lib.no (library export file)")
	fmt.Println("  - src/ (source directory)")
	fmt.Println("  - tests/ (test directory)")
}

func createConfigFile(config ProjectConfig) {

	content := fmt.Sprintf(`{
  "name": "%s",
  "version": "%s",
  "description": "%s",
  "keywords": [],
  "author": "",
  "email": "",
  "organization": "",
  "repository": "",
  "homepage": "",
  "license": "MIT",
  "mirrors": [],
  "dependencies": %s,
  "compiler": {
    "version": "%s",
  },
  "output": "./dist",
  "ignore": [],
}`,
		config.Name,
		config.Version,
		config.Description,
		formatDependencies(config.Dependencies),
		config.Compiler.Version,
	)

	err := os.WriteFile("mod.jsonc", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing config file: %v\n", err)
	}
}

func formatDependencies(deps map[string]string) string {
	if len(deps) == 0 {
		return "{}"
	}

	var sb strings.Builder
	sb.WriteString("{\n")
	for name, version := range deps {
		sb.WriteString(fmt.Sprintf("    \"%s\": \"%s\"\n", name, version))
	}
	sb.WriteString("  }")
	return sb.String()
}

func createMainFile() {
	content := `// Main entry point
print('Hello, Nolang!')
`
	err := os.WriteFile("main.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing main file: %v\n", err)
	}
}

func createGitIgnore() {
	content := `# Nolang project
dist/

# IDE
.vscode/
.idea/

# vim swap
*.sw[ponm]
*~

# OS
.DS_Store
Thumbs.db
`
	err := os.WriteFile(".gitignore", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing .gitignore: %v\n", err)
	}
}

func createSrcDirectory() {
	err := os.MkdirAll("src", 0755)
	if err != nil {
		fmt.Printf("Error creating src directory: %v\n", err)
		return
	}

	content := `// Example module
greet = (name str) {
    print('Hello, ' + name)
}
`
	err = os.WriteFile("src/utils.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing utils.no: %v\n", err)
	}
}

func createLibFile() {
	content := `// Export declarations
// @ /src/utils.greet greet
`
	err := os.WriteFile("lib.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing lib.no: %v\n", err)
	}
}

func createTestDirectory() {
	err := os.MkdirAll("tests", 0755)
	if err != nil {
		fmt.Printf("Error creating tests directory: %v\n", err)
		return
	}

	content := `// Test example
// # std/fmt.print

// test-greet = () {
//     print('test passed')
// }
`
	err = os.WriteFile("tests/test.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing test file: %v\n", err)
	}
}

func addDependency(name string) {
	config, err := loadProjectConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if config.Dependencies == nil {
		config.Dependencies = make(map[string]string)
	}

	config.Dependencies[name] = "*"
	createConfigFile(*config)
	fmt.Printf("Added dependency: %s\n", name)
}

func loadProjectConfig() (*ProjectConfig, error) {
	data, err := os.ReadFile("mod.jsonc")
	if err != nil {
		return nil, fmt.Errorf("mod.jsonc not found. Run 'no init' first")
	}
	cleaned := removeComments(string(data))
	var config ProjectConfig
	err = json.Unmarshal([]byte(cleaned), &config)
	return &config, err
}

func removeDependency(name string) {
	config, err := loadProjectConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if config.Dependencies == nil {
		fmt.Printf("No dependencies found.\n")
		return
	}
	if _, ok := config.Dependencies[name]; !ok {
		fmt.Printf("Dependency %q not found.\n", name)
		return
	}
	delete(config.Dependencies, name)
	createConfigFile(*config)
	fmt.Printf("Removed dependency: %s\n", name)
}

func updateDependency(pkg string) {
	config, err := loadProjectConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if _, ok := config.Dependencies[pkg]; !ok {
		fmt.Printf("Dependency %s not found.\n", pkg)
		return
	}
	config.Dependencies[pkg] = "*"
	createConfigFile(*config)
	fmt.Printf("Updated %s\n", pkg)
}

func updateAllDependencies() {
	config, err := loadProjectConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(config.Dependencies) == 0 {
		fmt.Println("No dependencies to update.")
		return
	}
	fmt.Println("Updating dependencies...")
	for name := range config.Dependencies {
		config.Dependencies[name] = "*"
		fmt.Printf("  Updated %s\n", name)
	}
	createConfigFile(*config)
	fmt.Println("All dependencies updated to latest.")
}

func listDependencies() {
	config, err := loadProjectConfig()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(config.Dependencies) == 0 {
		fmt.Println("No dependencies.")
		return
	}
	fmt.Println("Dependencies:")
	for name, version := range config.Dependencies {
		fmt.Printf("  %s: %s\n", name, version)
	}
}

func removeComments(jsonc string) string {
	var result strings.Builder
	inComment := false
	inString := false
	escape := false

	for i := 0; i < len(jsonc); i++ {
		c := jsonc[i]

		if escape {
			result.WriteByte(c)
			escape = false
			continue
		}

		if c == '\\' && inString {
			result.WriteByte(c)
			escape = true
			continue
		}

		if c == '"' && !inComment {
			inString = !inString
			result.WriteByte(c)
			continue
		}

		if inString {
			result.WriteByte(c)
			continue
		}

		if i+1 < len(jsonc) && jsonc[i] == '/' && jsonc[i+1] == '/' {
			inComment = true
			i++
			continue
		}

		if inComment && c == '\n' {
			inComment = false
			result.WriteByte(c)
			continue
		}

		if !inComment {
			result.WriteByte(c)
		}
	}

	return result.String()
}

func syncDependencies() {
	fmt.Println("Syncing dependencies...")

	pkg, err := nbuild.LoadPackage(".")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if pkg == nil {
		fmt.Println("Error: mod.jsonc not found. Run 'no init' first")
		return
	}
	if len(pkg.Dependencies) == 0 {
		fmt.Println("No dependencies to sync.")
		return
	}

	graph, err := pkg.EnsureDependencies(10)
	if err != nil {
		fmt.Printf("Error syncing dependencies: %v\n", err)
		return
	}

	count := len(graph.Resolved())
	fmt.Printf("Synced %d dependencies.\n", count)
	fmt.Println("Lock file and integrity sums saved.")
}

// nolangBinDir 返回 ~/no/bin 目錄
func nolangBinDir() string {
	return filepath.Join(nbuild.NoHomeDir(), "bin")
}

func installCommand(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	update := fs.Bool("u", false, "Force re-download and re-build")
	fs.Usage = func() {
		fmt.Println("Usage: no install [-u] [<pkg>@<version>]")
		fmt.Println("")
		fmt.Println("Install a package binary to system.")
		fmt.Println("  no install            build and install current package")
		fmt.Println("  no install -u         force rebuild current package")
		fmt.Println("  no install pkg@1.0    download and install remote package")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	remaining := fs.Args()
	var pkgSpec string
	if len(remaining) > 0 {
		pkgSpec = remaining[0]
	}

	var buildDir string
	var binName string

	if pkgSpec == "" {
		// 無參數：安裝當前目錄的包
		pkg, err := nbuild.LoadPackage(".")
		if err != nil || pkg == nil {
			fmt.Fprintf(os.Stderr, "Error: mod.jsonc not found in current directory\n")
			return
		}
		buildDir = "."
		binName = pkg.Name
		if !*update {
			fmt.Printf("Installing current package: %s\n", binName)
		} else {
			fmt.Printf("Updating current package: %s\n", binName)
		}
	} else {
		// 解析 <pkg>@<version>
		parts := strings.SplitN(pkgSpec, "@", 2)
		pkgKey := parts[0]
		version := "*"
		if len(parts) == 2 {
			version = parts[1]
		}

		if !*update {
			// 檢查是否已安裝
			binDir := nolangBinDir()
			installed := filepath.Join(binDir, nbuild.PackageShortName(pkgKey))
			if _, err := os.Stat(installed); err == nil {
				// 檢查軟鏈接
				symlink := filepath.Join("/usr/local/bin", nbuild.PackageShortName(pkgKey))
				if _, err := os.Stat(symlink); err == nil {
					fmt.Printf("%s already installed. Use -u to update.\n", pkgSpec)
					return
				}
			}
		}

		fmt.Printf("Downloading %s@%s...\n", pkgKey, version)

		// 從遠端下載包源碼
		pkgDir, _, err := nbuild.DownloadPackage(pkgKey, version, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading %s: %v\n", pkgSpec, err)
			return
		}

		buildDir = pkgDir
		binName = nbuild.PackageShortName(pkgKey)
	}

	// 建立臨時目錄用於構建
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

	outPath := filepath.Join(tmpDir, binName)
	opts := nbuild.BuildOptions{
		CC:      "clang",
		Output:  outPath,
		Verbose: verbose,
	}

	fmt.Printf("Building %s...\n", binName)
	if err := nbuild.BuildFile(buildDir, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error building: %v\n", err)
		return
	}

	// 確保 ~/.nolang/bin 目錄存在
	binDir := nolangBinDir()
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", binDir, err)
		return
	}

	// 複製 binary 到 ~/.nolang/bin/<name>
	dst := filepath.Join(binDir, binName)
	data, err := os.ReadFile(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading built binary: %v\n", err)
		return
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", dst, err)
		return
	}

	// 在 /usr/local/bin/ 建立軟鏈接
	symlink := filepath.Join("/usr/local/bin", binName)
	// 移除舊的軟鏈接/文件
	if _, err := os.Lstat(symlink); err == nil {
		os.Remove(symlink)
	}
	if err := os.Symlink(dst, symlink); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating symlink %s: %v\n", symlink, err)
		fmt.Printf("Try: sudo ln -sf %s %s\n", dst, symlink)
		return
	}

	fmt.Printf("Installed %s\n", binName)
	fmt.Printf("  binary: %s\n", dst)
	fmt.Printf("  symlink: %s\n", symlink)
}

func uninstallCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: no uninstall <name>")
		fmt.Println("")
		fmt.Println("Uninstall a package binary.")
		fmt.Println("  no uninstall pkg    remove pkg binary and symlink")
		return
	}

	name := args[0]
	binDir := nolangBinDir()
	binary := filepath.Join(binDir, name)
	symlink := filepath.Join("/usr/local/bin", name)

	removed := false

	// 移除軟鏈接
	if _, err := os.Lstat(symlink); err == nil {
		if err := os.Remove(symlink); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing symlink %s: %v\n", symlink, err)
			fmt.Printf("Try: sudo no uninstall %s\n", name)
			return
		}
		fmt.Printf("Removed symlink: %s\n", symlink)
		removed = true
	} else {
		fmt.Printf("Symlink not found: %s\n", symlink)
	}

	// 移除 binary
	if _, err := os.Stat(binary); err == nil {
		if err := os.Remove(binary); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing %s: %v\n", binary, err)
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

// stdinIsPiped 報告 stdin 是否為管道或重定向（非交互式終端）。
// 用於區分 `echo 'x=1' | no fmt`（管道）和直接運行 `no fmt`（終端）。
func stdinIsPiped() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// char device = 交互式終端；管道/重定向不是 char device
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func fmtCommand(args []string) {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	writeInPlace := fs.Bool("w", false, "write result to source file")
	diffMode := fs.Bool("d", false, "output colored diff instead of formatted result")
	fs.Usage = func() {
		fmt.Println("Usage: no fmt [flags] <file|dir>")
		fmt.Println("")
		fmt.Println("Format Nolang source files.")
		fmt.Println("When no file/dir is given and stdin is a terminal:")
		fmt.Println("  - if workspace.jsonc exists, format all its project dirs;")
		fmt.Println("  - otherwise, default to the current directory.")
		fmt.Println("When stdin is piped (e.g. echo 'x=1' | no fmt), reads from stdin.")
		fmt.Println("Directories are processed recursively automatically.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no fmt                      format current dir (list files needing formatting)")
		fmt.Println("  no fmt -w                   format all .no files in current dir in-place")
		fmt.Println("  no fmt -d                   show diff for all .no files in current dir")
		fmt.Println("  no fmt main.no              format and print to stdout")
		fmt.Println("  no fmt -w main.no           format file in-place")
		fmt.Println("  no fmt -d main.no           show colored diff")
		fmt.Println("  no fmt src/                 list .no files in src/ needing formatting")
		fmt.Println("  no fmt -w src/              format all .no files in src/ in-place")
		fmt.Println("  no fmt -d src/              show diff for all .no files in src/")
		fmt.Println("  echo 'x=1' | no fmt         format from stdin")
	}
	_ = fs.Parse(args)

	remaining := fs.Args()

	if len(remaining) == 0 {
		if stdinIsPiped() {
			// stdin 是管道/重定向（如 echo 'x=1' | no fmt），從 stdin 讀取
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
				os.Exit(1)
			}
			original := string(data)
			result, perrs := nfmt.FormatFileWithErrors(original)
			if len(perrs) > 0 {
				for _, e := range perrs {
					fmt.Fprintf(os.Stderr, "format error: %s\n", e)
				}
				os.Exit(1)
			}
			if *diffMode {
				fmt.Print(generateDiff("stdin", original, result))
			} else {
				fmt.Print(result)
			}
			return
		}
		// 交互式終端：先檢查 workspace.jsonc，有則按其中目錄找，否則用 ./
		// （與 build 命令的 workspace 模式行為一致）
		if ws, _ := nbuild.LoadWorkspaceFile("."); ws != nil && len(ws) > 0 {
			fmt.Fprintf(os.Stderr, "fmt: workspace.jsonc found, formatting %d project(s)\n", len(ws))
			for _, relPath := range ws {
				remaining = append(remaining, relPath)
			}
		} else {
			remaining = []string{"."}
		}
	}

	hadError := false
	for _, arg := range remaining {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", arg, err)
			hadError = true
			continue
		}

		if info.IsDir() {
			if err := fmtProcessDirectory(arg, *writeInPlace, *diffMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing directory %s: %v\n", arg, err)
				hadError = true
			}
		} else {
			if err := fmtProcessFile(arg, *writeInPlace, *diffMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing file %s: %v\n", arg, err)
				hadError = true
			}
		}
	}
	if hadError {
		os.Exit(1)
	}
}

func fmtProcessFile(filename string, writeInPlace bool, diffMode bool) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	original := string(data)
	result, perrs := nfmt.FormatFileWithErrors(original)
	if len(perrs) > 0 {
		for _, e := range perrs {
			fmt.Fprintf(os.Stderr, "%s: format error: %s\n", filename, e)
		}
		return fmt.Errorf("format failed: %d parse error(s)", len(perrs))
	}

	if diffMode {
		diff := generateDiff(filename, original, result)
		if diff != "" {
			fmt.Print(diff)
		}
		return nil
	}

	if writeInPlace {
		return os.WriteFile(filename, []byte(result), 0644)
	}
	fmt.Print(result)
	return nil
}

func fmtProcessDirectory(dirname string, writeInPlace bool, diffMode bool) error {
	var firstErr error
	checked := 0
	needFormat := 0
	// 無 -w 且無 -d 時為「檢查模式」：只列出需要格式化的文件名，
	// 不把所有文件內容拼接到 stdout（多文件拼接沒有意義）。
	checkMode := !writeInPlace && !diffMode
	_ = filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".no") {
			return nil
		}
		checked++
		if checkMode {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				if firstErr == nil {
					firstErr = rerr
				}
				return nil
			}
			result, perrs := nfmt.FormatFileWithErrors(string(data))
			if len(perrs) > 0 {
				for _, e := range perrs {
					fmt.Fprintf(os.Stderr, "%s: format error: %s\n", path, e)
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("format failed: %d parse error(s) in %s", len(perrs), path)
				}
				return nil
			}
			if result != string(data) {
				fmt.Println(path)
				needFormat++
			}
			return nil
		}
		if ferr := fmtProcessFile(path, writeInPlace, diffMode); ferr != nil && firstErr == nil {
			firstErr = ferr
		}
		return nil
	})
	if checkMode && checked > 0 {
		if needFormat == 0 {
			fmt.Fprintf(os.Stderr, "all %d file(s) already formatted\n", checked)
		} else {
			fmt.Fprintf(os.Stderr, "%d/%d file(s) need formatting\n", needFormat, checked)
		}
	}
	return firstErr
}

func buildCommand(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outputFile := fs.String("o", "", "Output file path")
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu, wasm32-wasi)")
	unsafe := fs.Bool("unsafe", false, "Skip bounds checks for maximum performance (unsafe)")
	wasmDirect := fs.Bool("wasm-direct", false, "Use Direct WASM backend (no LLVM toolchain required, browser-compatible)")
	jsBackend := fs.Bool("js", false, "Use JS backend (emit JavaScript source, no LLVM toolchain required)")
	browserMode := fs.Bool("browser", false, "Generate browser-targeted output (HTML + JS, requires --js)")
	fs.Usage = func() {
		fmt.Println("Usage: no build [flags] <file|directory>")
		fmt.Println("")
		fmt.Println("Build Nolang source files to an executable.")
		fmt.Println("If no file/directory is specified and workspace.jsonc exists,")
		fmt.Println("all projects in workspace.jsonc are built in parallel.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("For wasm32-wasi target, set $WASI_SYSROOT to your wasi-sysroot path.")
		fmt.Println("Use --wasm-direct to bypass LLVM toolchain and emit WASM directly (browser-compatible).")
		fmt.Println("Use --js to bypass LLVM toolchain and emit JavaScript source (Node.js/browser-compatible).")
		fmt.Println("Use --browser with --js to generate browser-targeted JS and an HTML wrapper.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no build                  build current directory or workspace.jsonc projects")
		fmt.Println("  no build main.no")
		fmt.Println("  no build -o output main.no")
		fmt.Println("  no build -cc zig main.no")
		fmt.Println("  no build -target x86_64-linux-gnu main.no")
		fmt.Println("  no build -target wasm32-wasi main.no")
		fmt.Println("  no build -unsafe main.no  build without bounds checks (max performance)")
		fmt.Println("  no build --wasm-direct -target wasm32-wasi main.no  emit WASM without LLVM toolchain")
		fmt.Println("  no build --wasm-direct -o out.wasm main.no        Direct WASM backend with explicit output")
		fmt.Println("  no build --js main.no                              emit JavaScript (type erasure)")
		fmt.Println("  no build --js -o app.js main.no                    JS backend with explicit output")
		fmt.Println("  no build --js --browser main.no                    emit browser JS + HTML wrapper")
	}
	_ = fs.Parse(args)

	var inputPath string
	if len(fs.Args()) == 0 {
		inputPath = "."
	} else {
		inputPath = fs.Args()[0]
	}
	// 未指定 target 時自動檢測當前平台
	targetStr := *target
	if targetStr == "" {
		targetStr = nbuild.DetectTarget()
	}

	opts := nbuild.BuildOptions{
		CC:            *cc,
		Target:        targetStr,
		Verbose:       verbose,
		Output:        *outputFile,
		NoBoundsCheck: *unsafe,
		UseDirectWasm: *wasmDirect,
		UseJS:         *jsBackend,
		BrowserMode:   *browserMode,
	}

	// JS 後端路徑：繞過 LLVM 工具鏈，直接發射 JavaScript 原始碼（型別擦除）。
	if *jsBackend {
		// workspace 模式不適用 JS 後端（單一輸出檔案）；要求明確輸入檔案。
		if inputPath == "." {
			fmt.Fprintln(os.Stderr, "Error: --js requires an explicit input file (workspace mode not supported)")
			os.Exit(1)
		}

		if *browserMode {
			// Browser mode: output both .html and .js
			baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
			if baseName == "main" {
				if pkg, _ := nbuild.LoadPackage(filepath.Dir(inputPath)); pkg != nil && pkg.Name != "" {
					baseName = pkg.Name
				}
			}

			outDir := "dist"
			outJs := filepath.Join(outDir, baseName+".js")
			outHtml := filepath.Join(outDir, baseName+".html")
			os.MkdirAll(outDir, 0755)

			jsCode, htmlCode, err := nbuild.BuildJSBrowser(inputPath, nbuild.BuildOptions{
				Verbose:     verbose,
				UseJS:       true,
				BrowserMode: true,
			}, baseName+".js")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if err := os.WriteFile(outJs, []byte(jsCode), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing JS: %v\n", err)
				os.Exit(1)
			}
			if err := os.WriteFile(outHtml, []byte(htmlCode), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing HTML: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Built: %s\n", outJs)
			fmt.Printf("Built: %s\n", outHtml)
			fmt.Printf("Open in browser: %s\n", outHtml)
			return
		}

		jsCode, err := nbuild.BuildJS(inputPath, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		out := *outputFile
		if out == "" {
			// 預設輸出路徑：dist/<base-name>.js
			baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
			if baseName == "main" {
				// 嘗試從同目錄的 mod.jsonc 取得套件名稱
				if pkg, _ := nbuild.LoadPackage(filepath.Dir(inputPath)); pkg != nil && pkg.Name != "" {
					baseName = pkg.Name
				}
			}
			out = filepath.Join("dist", baseName+".js")
			os.MkdirAll("dist", 0755)
		}
		if err := os.WriteFile(out, []byte(jsCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Built: %s\n", out)
		return
	}

	// Direct WASM 後端路徑：繞過 LLVM 工具鏈，直接發射 WASM 字節碼。
	// 觸發條件：
	//   (a) 用戶明確指定 --wasm-direct
	//   (b) 於 wasip1 平台下且目標含 wasm32（無 LLVM 可用，自動啟用並警告）
	if *wasmDirect || (runtime.GOOS == "wasip1" && strings.Contains(targetStr, "wasm32")) {
		if !*wasmDirect && runtime.GOOS == "wasip1" {
			fmt.Fprintln(os.Stderr, "[warn] wasip1 runtime: auto-switching to Direct WASM backend (LLVM toolchain unavailable)")
		}
		out := *outputFile
		if out == "" {
			out = "a.wasm"
		}
		// workspace 模式不適用 Direct WASM（單一輸出檔案）；要求明確輸入檔案。
		if inputPath == "." {
			fmt.Fprintln(os.Stderr, "Error: --wasm-direct requires an explicit input file (workspace mode not supported)")
			os.Exit(1)
		}
		wasmBytes, err := nbuild.BuildDirectWasm(inputPath, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(out, wasmBytes, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Built: %s\n", out)
		return
	}

	// 無參數時，優先檢查 workspace.jsonc 並行編譯
	if len(fs.Args()) == 0 {
		if _, err := os.Stat("workspace.jsonc"); err == nil {
			if err := nbuild.BuildWorkspace(".", opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	if err := nbuild.BuildFile(inputPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCommand(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu, wasm32-wasi)")
	unsafe := fs.Bool("unsafe", false, "Skip bounds checks for maximum performance (unsafe)")
	wasmDirect := fs.Bool("wasm-direct", false, "Use Direct WASM backend (no LLVM toolchain required, browser-compatible)")
	jsBackend := fs.Bool("js", false, "Use JS backend (emit JavaScript, run with node)")
	browserMode := fs.Bool("browser", false, "Open in browser (requires --js)")
	fs.Usage = func() {
		fmt.Println("Usage: no run [<file|dir>]")
		fmt.Println("")
		fmt.Println("Build and run a Nolang project.")
		fmt.Println("If directory, requires main.no (entry point).")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("For wasm32-wasi target, set $WASI_SYSROOT to your wasi-sysroot path.")
		fmt.Println("Use --wasm-direct to bypass LLVM toolchain and emit WASM directly (browser-compatible).")
		fmt.Println("Use --js to bypass LLVM toolchain and emit JavaScript (run with node).")
		fmt.Println("Use --js --browser to emit browser JS + HTML and open in the default browser.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no run                     build and run main.no in current dir")
		fmt.Println("  no run main.no             build and run main.no")
		fmt.Println("  no run -cc zig main.no     build and run with Zig compiler")
		fmt.Println("  no run -target wasm32-wasi main.no")
		fmt.Println("  no run --wasm-direct main.no  build via Direct WASM backend then run with wasmtime (if available)")
		fmt.Println("  no run --js main.no            build via JS backend then run with node")
		fmt.Println("  no run --js --browser main.no  build browser JS + HTML and open in default browser")
	}
	_ = fs.Parse(args)

	inputPath := "."
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	}

	// 如果是文件夾，驗證 main.no 存在
	info, err := os.Stat(inputPath)
	if err == nil && info.IsDir() {
		mainPath := filepath.Join(inputPath, "main.no")
		if _, err := os.Stat(mainPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: main.no not found in %s\n", inputPath)
			os.Exit(1)
		}
	}

	tmpDir, err := os.MkdirTemp("", "nolang-run")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if os.Getenv("NOLANG_KEEP_IR") == "" {
		defer os.RemoveAll(tmpDir)
	} else {
		fmt.Fprintf(os.Stderr, "[debug] keep run tmp dir: %s\n", tmpDir)
	}

	// 未指定 target 時自動檢測當前平台
	targetStr := *target
	if targetStr == "" {
		targetStr = nbuild.DetectTarget()
	}

	// JS 後端路徑：編譯為 .js 後以 node 執行。
	if *jsBackend {
		// 若 inputPath 是目錄，解析為 main.no
		if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
			inputPath = filepath.Join(inputPath, "main.no")
		}

		if *browserMode {
			// Browser mode: build HTML + JS, then open in default browser
			baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
			if baseName == "main" {
				if pkg, _ := nbuild.LoadPackage(filepath.Dir(inputPath)); pkg != nil && pkg.Name != "" {
					baseName = pkg.Name
				}
			}

			outJs := filepath.Join(tmpDir, baseName+".js")
			outHtml := filepath.Join(tmpDir, baseName+".html")

			jsCode, htmlCode, berr := nbuild.BuildJSBrowser(inputPath, nbuild.BuildOptions{
				Verbose:     verbose,
				UseJS:       true,
				BrowserMode: true,
			}, baseName+".js")
			if berr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", berr)
				os.Exit(1)
			}

			if werr := os.WriteFile(outJs, []byte(jsCode), 0644); werr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
				os.Exit(1)
			}
			if werr := os.WriteFile(outHtml, []byte(htmlCode), 0644); werr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
				os.Exit(1)
			}

			// Open in default browser
			var openCmd *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				openCmd = exec.Command("open", outHtml)
			case "linux":
				openCmd = exec.Command("xdg-open", outHtml)
			case "windows":
				openCmd = exec.Command("cmd", "/c", "start", outHtml)
			default:
				fmt.Printf("Open in browser: %s\n", outHtml)
				return
			}
			if err := openCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to open browser: %v\n", err)
				fmt.Printf("Open manually: %s\n", outHtml)
			}
			return
		}

		outPath := filepath.Join(tmpDir, "out.js")
		jsCode, berr := nbuild.BuildJS(inputPath, nbuild.BuildOptions{
			Target:  targetStr,
			UseJS:   true,
			Verbose: verbose,
		})
		if berr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", berr)
			os.Exit(1)
		}
		if werr := os.WriteFile(outPath, []byte(jsCode), 0644); werr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
			os.Exit(1)
		}
		// 以 node 執行編譯產物（若不可用則提示使用者安裝）。
		if _, lerr := exec.LookPath("node"); lerr != nil {
			fmt.Fprintf(os.Stderr, "Built: %s\n", outPath)
			fmt.Fprintln(os.Stderr, "Error: node not found in PATH; install Node.js to run JS backend output")
			os.Exit(1)
		}
		cmd := exec.Command("node", append([]string{outPath}, fs.Args()[1:]...)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Direct WASM 後端路徑：編譯為 .wasm 後嘗試以 wasmtime 執行。
	// 觸發條件與 buildCommand 一致（明確指定 --wasm-direct 或 wasip1 下自動啟用）。
	if *wasmDirect || (runtime.GOOS == "wasip1" && strings.Contains(targetStr, "wasm32")) {
		if !*wasmDirect && runtime.GOOS == "wasip1" {
			fmt.Fprintln(os.Stderr, "[warn] wasip1 runtime: auto-switching to Direct WASM backend (LLVM toolchain unavailable)")
		}
		// wasip1 瀏覽器沙箱不支援 spawn 子行程，無法執行編譯產物。
		if runtime.GOOS == "wasip1" {
			fmt.Fprintln(os.Stderr, "Error: running compiled WASM not supported in browser playground; use 'no build --wasm-direct' instead")
			os.Exit(1)
		}
		outPath := filepath.Join(tmpDir, "out.wasm")
		wasmBytes, berr := nbuild.BuildDirectWasm(inputPath, nbuild.BuildOptions{
			Target:        targetStr,
			UseDirectWasm: true,
			Verbose:       verbose,
			NoBoundsCheck: *unsafe,
		})
		if berr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", berr)
			os.Exit(1)
		}
		if werr := os.WriteFile(outPath, wasmBytes, 0755); werr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
			os.Exit(1)
		}
		// 以 wasmtime 執行編譯產物（若不可用則提示使用者手動執行）。
		if _, lerr := exec.LookPath("wasmtime"); lerr != nil {
			fmt.Fprintf(os.Stderr, "Built: %s\n", outPath)
			fmt.Fprintln(os.Stderr, "Error: wasmtime not in PATH; run manually: wasmtime run "+outPath)
			os.Exit(1)
		}
		cmd := exec.Command("wasmtime", append([]string{"run", outPath}, fs.Args()[1:]...)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	outPath := filepath.Join(tmpDir, "out")
	opts := nbuild.BuildOptions{
		CC:            *cc,
		Target:        targetStr,
		Output:        outPath,
		Verbose:       verbose,
		NoBoundsCheck: *unsafe,
		UseDirectWasm: *wasmDirect,
	}
	if err := nbuild.BuildFile(inputPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// WASM 下無法執行編譯產物（瀏覽器沙箱不支援 spawn 子行程）。
	if runtime.GOOS == "wasip1" {
		fmt.Fprintln(os.Stderr, "Error: running compiled binary not supported in browser playground")
		os.Exit(1)
	}
	cmd := exec.Command(outPath, fs.Args()[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func testCommand(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu, wasm32-wasi)")
	wasmDirect := fs.Bool("wasm-direct", false, "Use Direct WASM backend (no LLVM toolchain required, browser-compatible)")
	fs.Usage = func() {
		fmt.Println("Usage: no test [<file>]")
		fmt.Println("")
		fmt.Println("Run tests.")
		fmt.Println("  no test                     run all .no files in tests/ directory")
		fmt.Println("  no test test/my-test.no     run a single test file")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("For wasm32-wasi target, set $WASI_SYSROOT to your wasi-sysroot path.")
		fmt.Println("Use --wasm-direct to bypass LLVM toolchain and emit WASM directly (browser-compatible).")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no test")
		fmt.Println("  no test test/my-test.no")
		fmt.Println("  no test -cc zig")
		fmt.Println("  no test -target x86_64-linux-gnu")
		fmt.Println("  no test -target wasm32-wasi")
		fmt.Println("  no test --wasm-direct       run tests via Direct WASM backend with wasmtime")
	}
	_ = fs.Parse(args)

	var inputPath string
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	} else {
		inputPath = filepath.Join(".", "tests")
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if len(fs.Args()) == 0 {
			fmt.Fprintf(os.Stderr, "Error: tests/ directory not found\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}

	var testFiles []string
	if info.IsDir() {
		// 目錄：遞迴掃描所有 .no 文件，排除 main.no 和 lib.no
		err = filepath.WalkDir(inputPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".no") {
				return nil
			}
			if name == "main.no" || name == "lib.no" {
				return nil
			}
			testFiles = append(testFiles, path)
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(testFiles) == 0 {
			fmt.Println("No test files found in " + inputPath)
			return
		}
	} else {
		// 單一文件
		testFiles = append(testFiles, inputPath)
	}

	// 未指定 target 時自動檢測當前平台
	targetStr := *target
	if targetStr == "" {
		targetStr = nbuild.DetectTarget()
	}

	// Direct WASM 後端路徑：編譯為 .wasm 後以 wasmtime 執行每個測試。
	// 觸發條件與 buildCommand 一致。
	useDirectWasm := *wasmDirect || (runtime.GOOS == "wasip1" && strings.Contains(targetStr, "wasm32"))
	if useDirectWasm && !*wasmDirect && runtime.GOOS == "wasip1" {
		fmt.Fprintln(os.Stderr, "[warn] wasip1 runtime: auto-switching to Direct WASM backend (LLVM toolchain unavailable)")
	}
	// wasip1 瀏覽器沙箱不支援 spawn 子行程，無法執行測試產物。
	if useDirectWasm && runtime.GOOS == "wasip1" {
		fmt.Fprintln(os.Stderr, "Error: running test binaries not supported in browser playground")
		os.Exit(1)
	}
	if useDirectWasm {
		if _, lerr := exec.LookPath("wasmtime"); lerr != nil {
			fmt.Fprintln(os.Stderr, "Error: --wasm-direct test mode requires wasmtime in PATH")
			os.Exit(1)
		}
	}

	hadFailure := false
	for _, tf := range testFiles {
		if verbose {
			fmt.Printf("Testing: %s\n", tf)
		}
		tmpDir, err := os.MkdirTemp("", "nolang-test")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if useDirectWasm {
			// Direct WASM 路徑：產生 .wasm 後以 wasmtime 執行。
			outPath := filepath.Join(tmpDir, "out.wasm")
			wasmBytes, berr := nbuild.BuildDirectWasm(tf, nbuild.BuildOptions{
				Target:        targetStr,
				UseDirectWasm: true,
				Verbose:       false,
			})
			if berr != nil {
				fmt.Fprintf(os.Stderr, "FAIL: %s\n  %v\n", tf, berr)
				hadFailure = true
				os.RemoveAll(tmpDir)
				continue
			}
			if werr := os.WriteFile(outPath, wasmBytes, 0755); werr != nil {
				fmt.Fprintf(os.Stderr, "FAIL: %s\n  %v\n", tf, werr)
				hadFailure = true
				os.RemoveAll(tmpDir)
				continue
			}
			cmd := exec.Command("wasmtime", "run", outPath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: %s (exit code %v)\n", tf, err)
				hadFailure = true
				os.RemoveAll(tmpDir)
				continue
			}
			os.RemoveAll(tmpDir)
			continue
		}

		outPath := filepath.Join(tmpDir, "out")
		opts := nbuild.BuildOptions{
			CC:            *cc,
			Target:        targetStr,
			Output:        outPath,
			Verbose:       false,
			UseDirectWasm: *wasmDirect,
		}

		if err := nbuild.BuildFile(tf, opts); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s\n  %v\n", tf, err)
			hadFailure = true
			os.RemoveAll(tmpDir)
			continue
		}

		// WASM 下無法執行編譯產物（瀏覽器沙箱不支援 spawn 子行程）。
		if runtime.GOOS == "wasip1" {
			fmt.Fprintln(os.Stderr, "Error: running compiled binary not supported in browser playground")
			os.Exit(1)
		}
		cmd := exec.Command(outPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s (exit code %v)\n", tf, err)
			hadFailure = true
			os.RemoveAll(tmpDir)
			continue
		}
		os.RemoveAll(tmpDir)
	}

	if hadFailure {
		os.Exit(1)
	}
}

func vetCommand(args []string) {
	fs := flag.NewFlagSet("vet", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: no vet [<file|dir>]")
		fmt.Println("")
		fmt.Println("Validate Nolang source files without producing output.")
		fmt.Println("If directory, validates all .no files in that directory.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no vet                     validate main.no in current dir")
		fmt.Println("  no vet main.no             validate main.no")
		fmt.Println("  no vet src/std/            validate all .no files in src/std/")
	}
	_ = fs.Parse(args)

	inputPath := "."
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	}

	opts := nbuild.BuildOptions{
		Verbose: verbose,
	}

	// 檢查是文件還是目錄
	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if info.IsDir() {
		// 目錄模式：驗證所有 .no 文件
		files, err := filepath.Glob(filepath.Join(inputPath, "*.no"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no .no files found in %s\n", inputPath)
			os.Exit(1)
		}
		for _, file := range files {
			if err := nbuild.VetFile(file, opts); err != nil {
				fmt.Fprintf(os.Stderr, "Error in %s: %v\n", file, err)
				os.Exit(1)
			}
		}
	} else {
		// 文件模式：驗證單個文件
		if err := nbuild.VetFile(inputPath, opts); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if verbose {
		fmt.Println("Validation successful")
	}
}

func pubCommand(args []string) {
	fs := flag.NewFlagSet("pub", flag.ExitOnError)
	token := fs.String("token", "", "Registry authentication token")
	registry := fs.String("registry", "", "Package registry URL")
	_ = fs.Parse(args)

	if *token == "" {
		fmt.Println("Error: --token is required for publishing")
		fs.Usage()
		os.Exit(1)
	}

	fmt.Println("no pub: publishing is not yet implemented.")
	fmt.Printf("  token: %s\n", *token)
	fmt.Printf("  registry: %s\n", *registry)
}
