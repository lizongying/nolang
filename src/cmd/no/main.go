package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	case "version", "-V", "--version":
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
	fmt.Println("  -V    Print version")
	fmt.Println("")
	fmt.Println("  no version           Print version information")
	fmt.Println("")
	fmt.Println("  no fmt [flags] <file|dir>  Format source files")
	fmt.Println("    Flags:")
	fmt.Println("      -w    write result to source file (in-place)")
	fmt.Println("      -d    process directory mode (recursive)")
	fmt.Println("    Examples:")
	fmt.Println("      no fmt main.no              format and print to stdout")
	fmt.Println("      no fmt -w main.no           format file in-place")
	fmt.Println("      no fmt -w -d src/           format all .no files in src/ recursively")
	fmt.Println("      echo 'x=1' | no fmt         format from stdin")
	fmt.Println("")
	fmt.Println("  no build [flags] [<file|dir>]  Build a Nolang project (default: current dir)")
	fmt.Println("    Flags:")
	fmt.Println("      -o <file>     Output file path")
	fmt.Println("      -cc <s>       C compiler: clang (default), zig")
	fmt.Println("      -target <s>   Target triple for cross-compilation")
	fmt.Println("                      e.g. x86_64-linux-gnu, aarch64-macos-gnu,")
	fmt.Println("                      x86_64-windows-gnu")
	fmt.Println("    Examples:")
	fmt.Println("      no build main.no")
	fmt.Println("      no build -o output main.no      specify output path")
	fmt.Println("      no build -cc zig main.no        use zig as C compiler")
	fmt.Println("      no build -target x86_64-linux-gnu main.no  cross-compile")
	fmt.Println("")
	fmt.Println("  no run [<file|dir>]          Build and run")
	fmt.Println("    If directory, requires main.no (entry point).")
	fmt.Println("    Examples:")
	fmt.Println("      no run                     build and run main.no in current dir")
	fmt.Println("      no run main.no             build and run main.no")
	fmt.Println("      no run -cc zig main.no     build and run with Zig compiler")
	fmt.Println("")
	fmt.Println("  no test [<file>]            Run tests")
	fmt.Println("    Defaults to test/ directory.")
	fmt.Println("    Flags:")
	fmt.Println("      -cc <s>       C compiler: clang (default), zig")
	fmt.Println("      -target <s>   Target triple for cross-compilation")
	fmt.Println("                      e.g. x86_64-linux-gnu, aarch64-macos-gnu,")
	fmt.Println("                      x86_64-windows-gnu")
	fmt.Println("    Examples:")
	fmt.Println("      no test")
	fmt.Println("      no test test/my-test.no")
	fmt.Println("      no test -cc zig")
	fmt.Println("      no test -target x86_64-linux-gnu")
	fmt.Println("")
	fmt.Println("  no add <pkg>        Add a dependency")
	fmt.Println("  no remove <pkg>     Remove a dependency")
	fmt.Println("  no update <pkg>     Update a specific dependency")
	fmt.Println("  no update-all        Update all dependencies")
	fmt.Println("  no list              List dependencies")
	fmt.Println("  no install [-u] [<pkg>@<version>]")
	fmt.Println("                    Install a package binary (store in ~/.nolang/bin/, symlink in /usr/local/bin/)")
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
	createGitIgnore()

	fmt.Printf("Project initialized in %s\n", dir)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - mod.jsonc (project configuration)")
	fmt.Println("  - main.no (main entry file)")
	fmt.Println("  - .gitignore")
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
	createGitIgnore()
	createSrcDirectory()

	fmt.Printf("Project created: %s\n", name)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - mod.jsonc (project configuration)")
	fmt.Println("  - main.no (main entry file)")
	fmt.Println("  - src/ (source directory)")
	fmt.Println("  - .gitignore")
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
  "workspace": "",
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
# std/fmt.print

greet = (name str) {
    print('Hello, ' + name)
}
`
	err = os.WriteFile("src/utils.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing utils.no: %v\n", err)
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

// nolangBinDir 返回 ~/.nolang/bin 目錄
func nolangBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".nolang", "bin")
	}
	return filepath.Join(home, ".nolang", "bin")
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
	defer os.RemoveAll(tmpDir)

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

func fmtCommand(args []string) {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	writeInPlace := fs.Bool("w", false, "write result to source file")
	dirMode := fs.Bool("d", false, "process directory mode")
	fs.Usage = func() {
		fmt.Println("Usage: no fmt [flags] <file|directory>")
		fmt.Println("")
		fmt.Println("Format Nolang source files.")
		fmt.Println("When no file is given, reads from stdin.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no fmt main.no")
		fmt.Println("  no fmt -w main.no")
		fmt.Println("  no fmt -w -d src/")
		fmt.Println("  echo 'x=1' | no fmt")
	}
	_ = fs.Parse(args)

	remaining := fs.Args()

	if len(remaining) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		result := nfmt.Format(string(data))
		fmt.Print(result)
		return
	}

	for _, arg := range remaining {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", arg, err)
			continue
		}

		if info.IsDir() {
			if *dirMode {
				if err := fmtProcessDirectory(arg, *writeInPlace); err != nil {
					fmt.Fprintf(os.Stderr, "Error processing directory %s: %v\n", arg, err)
				}
			} else {
				fmt.Fprintf(os.Stderr, "Skipping directory %s (use -d flag)\n", arg)
			}
		} else {
			if err := fmtProcessFile(arg, *writeInPlace); err != nil {
				fmt.Fprintf(os.Stderr, "Error processing file %s: %v\n", arg, err)
			}
		}
	}
}

func fmtProcessFile(filename string, writeInPlace bool) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	result := nfmt.Format(string(data))

	if writeInPlace {
		return os.WriteFile(filename, []byte(result), 0644)
	}
	fmt.Print(result)
	return nil
}

func fmtProcessDirectory(dirname string, writeInPlace bool) error {
	return filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".no") {
			return fmtProcessFile(path, writeInPlace)
		}
		return nil
	})
}

func buildCommand(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outputFile := fs.String("o", "", "Output file path")
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu)")
	fs.Usage = func() {
		fmt.Println("Usage: no build [flags] <file|directory>")
		fmt.Println("")
		fmt.Println("Build Nolang source files to an executable.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no build                  build current directory")
		fmt.Println("  no build main.no")
		fmt.Println("  no build -o output main.no")
		fmt.Println("  no build -cc zig main.no")
		fmt.Println("  no build -target x86_64-linux-gnu main.no")
	}
	_ = fs.Parse(args)

	var inputPath string
	if len(fs.Args()) == 0 {
		inputPath = "."
	} else {
		inputPath = fs.Args()[0]
	}
	opts := nbuild.BuildOptions{
		CC:      *cc,
		Target:  *target,
		Verbose: verbose,
		Output:  *outputFile,
	}

	if err := nbuild.BuildFile(inputPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCommand(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu)")
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
	defer os.RemoveAll(tmpDir)
	outPath := filepath.Join(tmpDir, "out")
	opts := nbuild.BuildOptions{
		CC:      *cc,
		Target:  *target,
		Output:  outPath,
		Verbose: verbose,
	}
	if err := nbuild.BuildFile(inputPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command(outPath)
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
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu)")
	fs.Usage = func() {
		fmt.Println("Usage: no test [<file>]")
		fmt.Println("")
		fmt.Println("Run tests.")
		fmt.Println("  no test                     run all .no files in test/ directory")
		fmt.Println("  no test test/my-test.no     run a single test file")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	var inputPath string
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	} else {
		inputPath = filepath.Join(".", "test")
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if len(fs.Args()) == 0 {
			fmt.Fprintf(os.Stderr, "Error: test/ directory not found\n")
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
		outPath := filepath.Join(tmpDir, "out")
		opts := nbuild.BuildOptions{
			CC:      *cc,
			Target:  *target,
			Output:  outPath,
			Verbose: false,
		}

		if err := nbuild.BuildFile(tf, opts); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s\n  %v\n", tf, err)
			hadFailure = true
			os.RemoveAll(tmpDir)
			continue
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
