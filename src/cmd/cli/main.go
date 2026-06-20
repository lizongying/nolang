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
			fmt.Println("Usage: nolang new <project-name>")
			return
		}
		newProject(os.Args[2])
	case "add":
		if len(os.Args) < 3 {
			fmt.Println("Usage: nolang add <package-name>")
			return
		}
		addDependency(os.Args[2])
	case "remove":
		if len(os.Args) < 3 {
			fmt.Println("Usage: nolang remove <package-name>")
			return
		}
		removeDependency(os.Args[2])
	case "update":
		if len(os.Args) < 3 {
			fmt.Println("Usage: nolang update <pkg>")
			return
		}
		updateDependency(os.Args[2])
	case "update-all":
		updateAllDependencies()
	case "list":
		listDependencies()
	case "install":
		installCommand()
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
	fmt.Println("  nolang version           Print version information")
	fmt.Println("")
	fmt.Println("  nolang fmt [flags] <file|dir>  Format source files")
	fmt.Println("    Flags:")
	fmt.Println("      -w    write result to source file (in-place)")
	fmt.Println("      -d    process directory mode (recursive)")
	fmt.Println("    Examples:")
	fmt.Println("      nolang fmt main.no              format and print to stdout")
	fmt.Println("      nolang fmt -w main.no           format file in-place")
	fmt.Println("      nolang fmt -w -d src/           format all .no files in src/ recursively")
	fmt.Println("      echo 'x=1' | nolang fmt         format from stdin")
	fmt.Println("")
	fmt.Println("  nolang build [flags] [<file|dir>]  Build a Nolang project (default: current dir)")
	fmt.Println("    Flags:")
	fmt.Println("      -o <file>     Output file path")
	fmt.Println("      -cc <s>       C compiler: clang (default), zig")
	fmt.Println("    Examples:")
	fmt.Println("      nolang build main.no")
	fmt.Println("      nolang build -o output main.no      specify output path")
	fmt.Println("      nolang build -cc zig main.no        use zig as C compiler")
	fmt.Println("")
	fmt.Println("  nolang run [<file|dir>]          Build and run")
	fmt.Println("    If directory, requires main.no (entry point).")
	fmt.Println("    Examples:")
	fmt.Println("      nolang run                     build and run main.no in current dir")
	fmt.Println("      nolang run main.no             build and run main.no")
	fmt.Println("      nolang run -cc zig main.no     build and run with Zig compiler")
	fmt.Println("")
	fmt.Println("  nolang test [<file|dir>]         Run tests")
	fmt.Println("    If directory, runs main() from all .no files except main.no and lib.no.")
	fmt.Println("")
	fmt.Println("  nolang add <pkg>        Add a dependency")
	fmt.Println("  nolang remove <pkg>     Remove a dependency")
	fmt.Println("  nolang update <pkg>     Update a specific dependency")
	fmt.Println("  nolang update-all        Update all dependencies")
	fmt.Println("  nolang list              List dependencies")
	fmt.Println("  nolang install           Install nolang binary to system")
	fmt.Println("  nolang sync              Sync/download dependencies")
	fmt.Println("  nolang pub               Publish package")
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
		return nil, fmt.Errorf("mod.jsonc not found. Run 'nolang init' first")
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
		fmt.Println("Error: mod.jsonc not found. Run 'nolang init' first")
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

func installCommand() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	dst := "/usr/local/bin/nolang"
	data, err := os.ReadFile(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading binary: %v\n", err)
		return
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Printf("Try: sudo %s install\n", os.Args[0])
		return
	}
	fmt.Printf("Installed to %s\n", dst)
}

func fmtCommand(args []string) {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	writeInPlace := fs.Bool("w", false, "write result to source file")
	dirMode := fs.Bool("d", false, "process directory mode")
	fs.Usage = func() {
		fmt.Println("Usage: nolang fmt [flags] <file|directory>")
		fmt.Println("")
		fmt.Println("Format Nolang source files.")
		fmt.Println("When no file is given, reads from stdin.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  nolang fmt main.no")
		fmt.Println("  nolang fmt -w main.no")
		fmt.Println("  nolang fmt -w -d src/")
		fmt.Println("  echo 'x=1' | nolang fmt")
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
	fs.Usage = func() {
		fmt.Println("Usage: nolang build [flags] <file|directory>")
		fmt.Println("")
		fmt.Println("Build Nolang source files to an executable.")
		fmt.Println("")
		fmt.Println("Flags:")
		fs.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  nolang build                  build current directory")
		fmt.Println("  nolang build main.no")
		fmt.Println("  nolang build -o output main.no")
		fmt.Println("  nolang build -cc zig main.no")
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
	_ = fs.Parse(args)

	inputPath := "."
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var testFiles []string
	if info.IsDir() {
		// 文件夾：掃描所有 .no 文件，排除 main.no 和 lib.no
		entries, err := os.ReadDir(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".no") {
				continue
			}
			if name == "main.no" || name == "lib.no" {
				continue
			}
			testFiles = append(testFiles, filepath.Join(inputPath, name))
		}
		if len(testFiles) == 0 {
			fmt.Println("No test files found (no .no files other than main.no/lib.no).")
			return
		}
	} else {
		// 單一文件：直接執行
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

	fmt.Println("nolang pub: publishing is not yet implemented.")
	fmt.Printf("  token: %s\n", *token)
	fmt.Printf("  registry: %s\n", *registry)
}
