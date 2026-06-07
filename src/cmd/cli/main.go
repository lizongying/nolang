package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	Target string `json:"target"`
	Output string `json:"output"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	command := os.Args[1]

	switch command {
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
		updateDependencies()
	case "list":
		listDependencies()
	case "install":
		installDependencies()
	case "fmt":
		fmtCommand(os.Args[2:])
	case "build":
		buildCommand(os.Args[2:])
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Println("Nolang - Nolang Programming Language")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  nolang init             Initialize a new Nolang project in current directory")
	fmt.Println("  nolang new <name>       Create a new Nolang project")
	fmt.Println("  nolang fmt [flags] <file|dir>  Format source files")
	fmt.Println("  nolang build [flags] <file|dir>  Build a Nolang project")
	fmt.Println("  nolang add <pkg>        Add a dependency")
	fmt.Println("  nolang remove <pkg>     Remove a dependency")
	fmt.Println("  nolang update           Update dependencies")
	fmt.Println("  nolang list             List dependencies")
	fmt.Println("  nolang install          Install dependencies")
	fmt.Println("")
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
			Target: "llvm",
			Output: "./dist",
		},
	}

	createConfigFile(config)
	createMainFile()
	createGitIgnore()

	fmt.Printf("Project initialized in %s\n", dir)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - nolang.jsonc (project configuration)")
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
		Name:        name,
		Version:     "1.0.0",
		Description: "A new Nolang project",
		Dependencies: map[string]string{
			"fmt": "*",
		},
		Main: "main.no",
		Compiler: CompilerConfig{
			Target: "llvm",
			Output: "./dist",
		},
	}

	createConfigFile(config)
	createMainFile()
	createGitIgnore()
	createSrcDirectory()

	fmt.Printf("Project created: %s\n", name)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - nolang.jsonc (project configuration)")
	fmt.Println("  - main.no (main entry file)")
	fmt.Println("  - src/ (source directory)")
	fmt.Println("  - .gitignore")
}

func createConfigFile(config ProjectConfig) {
	content := fmt.Sprintf(`{
  "name": "%s",
  "version": "%s",
  "description": "%s",
  "author": "",
  "license": "MIT",
  "dependencies": %s,
  "main": "%s",
  "compiler": {
    "target": "%s",
    "output": "%s"
  }
}`,
		config.Name,
		config.Version,
		config.Description,
		formatDependencies(config.Dependencies),
		config.Main,
		config.Compiler.Target,
		config.Compiler.Output,
	)

	err := os.WriteFile("nolang.jsonc", []byte(content), 0644)
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
println('Hello, Nolang!')
`
	err := os.WriteFile("main.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing main file: %v\n", err)
	}
}

func createGitIgnore() {
	content := `# Nolang project
dist/
*.go
*.out

# IDE
.vscode/
.idea/
*.swp
*.swo
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
use std/fmt.println

greet(name str) {
    println('Hello, ' + name)
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
	data, err := os.ReadFile("nolang.jsonc")
	if err != nil {
		return nil, fmt.Errorf("nolang.jsonc not found. Run 'nolang init' first")
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

func updateDependencies() {
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

func installDependencies() {
	fmt.Println("Installing dependencies...")
	fmt.Println("Note: Dependency installation is not yet implemented.")
	fmt.Println("Standard library is included by default.")
}

func fmtCommand(args []string) {
	fs := flag.NewFlagSet("fmt", flag.ExitOnError)
	writeInPlace := fs.Bool("w", false, "write result to source file")
	dirMode := fs.Bool("d", false, "process directory mode")
	fs.Usage = func() {
		fmt.Println("Usage: nolang fmt [flags] <file|directory>")
		fmt.Println("\nFlags:")
		fs.PrintDefaults()
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
	targetStr := fs.String("target", "llvm", "Target: llvm, no")
	cc := fs.String("cc", "clang", "C compiler for llvm target: clang, zig")
	testMode := fs.Bool("test", false, "Build test module (test.no)")
	exportMode := fs.Bool("export", false, "Build library module (lib.no)")
	verbose := fs.Bool("v", false, "Verbose mode")
	fs.Usage = func() {
		fmt.Println("Usage: nolang build [flags] <file|directory>")
		fmt.Println("\nFlags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	if len(fs.Args()) != 1 {
		fmt.Println("Error: Missing input file")
		fs.Usage()
		os.Exit(1)
	}

	inputPath := fs.Args()[0]
	opts := nbuild.BuildOptions{
		Target:     *targetStr,
		CC:         *cc,
		TestMode:   *testMode,
		ExportMode: *exportMode,
		Verbose:    *verbose,
		Output:     *outputFile,
	}

	if err := nbuild.BuildFile(inputPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
