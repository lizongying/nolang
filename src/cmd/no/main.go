package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

type ProjectConfig struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Keywords     []string          `json:"keywords"`
	Author       string            `json:"author"`
	Email        string            `json:"email"`
	Organization string            `json:"organization"`
	Repository   string            `json:"repository"`
	Homepage     string            `json:"homepage"`
	License      string            `json:"license"`
	Mirrors      []string          `json:"mirrors"`
	Dependencies map[string]string `json:"dependencies"`
	Main         string            `json:"main"`
	Compiler     CompilerConfig    `json:"compiler"`
	Output       string            `json:"output"`
	Ignore       []string          `json:"ignore"`
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
			fmt.Println("Usage: no new <package-name>")
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
	case "ast":
		astCommand(os.Args[2:])
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
	fmt.Println("      -ld-KEY=VALUE   Inject a compile-time global constant (see -ld section below)")
	fmt.Println("    Use -v to emit LLVM IR (.ll) files for analysis alongside the build.")
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
	fmt.Println("      no build -ld-VERSION=0.1.2 main.no         inject VERSION as a global constant")
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
	fmt.Println("      -ld-KEY=VALUE  Inject a compile-time global constant (see -ld section below)")
	fmt.Println("    Examples:")
	fmt.Println("      no test")
	fmt.Println("      no test tests/my-test.no")
	fmt.Println("      no test -cc zig")
	fmt.Println("      no test -target x86_64-linux-gnu")
	fmt.Println("      no test -target wasm32-wasi")
	fmt.Println("")
	fmt.Println("  no vet [--strict] [<file|dir>]  Validate source files + lints")
	fmt.Println("    --strict               treat warnings/hints as errors")
	fmt.Println("    Examples:")
	fmt.Println("      no vet                     validate main.no in current dir")
	fmt.Println("      no vet main.no             validate main.no")
	fmt.Println("      no vet --strict src/std/   fail on any lint")
	fmt.Println("")
	fmt.Println("  no ast <file>          Print the parsed AST for debugging")
	fmt.Println("    Examples:")
	fmt.Println("      no ast main.no             print AST for main.no")
	fmt.Println("      no ast src/std/vec.no      print AST for vec.no")
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
	fmt.Println("  -ld-KEY=VALUE  Inject compile-time global constants (build, run, test)")
	fmt.Println("    Multiple -ld flags can be used. The -ld prefix is stripped, and")
	fmt.Println("    KEY=VALUE is injected as a top-level global constant in the Nolang")
	fmt.Println("    source, as if you had written `KEY = VALUE` at the top of main.no.")
	fmt.Println("")
	fmt.Println("    Value type inference:")
	fmt.Println("      - Integers (e.g. 42)         -> i64")
	fmt.Println("      - Floats   (e.g. 3.14)       -> f64")
	fmt.Println("      - Booleans (true/false)      -> bool")
	fmt.Println("      - Other    (e.g. 0.1.2)      -> str (single-quoted string literal)")
	fmt.Println("")
	fmt.Println("    Boolean shorthand: -ld-DEBUG (no =VALUE) is equivalent to -ld-DEBUG=true")
	fmt.Println("")
	fmt.Println("    Examples:")
	fmt.Println("      no build -ld-VERSION=0.1.2 -ld-NAME='nolang' main.no")
	fmt.Println("      no run -ld-DEBUG=true main.no")
	fmt.Println("      no run -ld-RELEASE main.no")
	fmt.Println("")
	fmt.Println("")
}

// verbose 為全局 -v 旗標
var verbose = false

// version is injected at build time via -ldflags
var version = "dev"

// buildDate is injected at build time via -ldflags
var buildDate = ""

// parseLDFlags extracts all -ld-KEY=VALUE arguments from the given args slice.
// It returns a map of KEY→VALUE pairs and a filtered slice with -ld arguments removed.
// The -ld prefix is stripped: "-ld-VERSION=0.1.2" → {"VERSION": "0.1.2"}.
// Multiple -ld flags are allowed: -ld-VERSION=0.1.2 -ld-DEBUG=true
func parseLDFlags(args []string) (map[string]string, []string) {
	ldFlags := map[string]string{}
	var filtered []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-ld-") {
			// Strip the "-ld-" prefix, parse KEY=VALUE
			kv := arg[4:] // remove "-ld-"
			if kv == "" {
				// Just "-ld-" with no key, skip
				continue
			}
			idx := strings.IndexByte(kv, '=')
			if idx < 0 {
				// No '=' sign: treat as boolean true flag: -ld-DEBUG → {"DEBUG": "true"}
				ldFlags[kv] = "true"
			} else {
				key := kv[:idx]
				val := kv[idx+1:]
				if key != "" {
					ldFlags[key] = val
				}
			}
		} else {
			filtered = append(filtered, arg)
		}
	}
	return ldFlags, filtered
}

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
		fmt.Printf(" ($%s)", pkg.NOLANG_STD_SRC)
	}
	fmt.Println()

	// Source directory (third-party / local development)
	srcDir, srcSrc := nbuild.GetSourceDir()
	fmt.Printf("source:      %s\n", srcDir)
	fmt.Printf("  resolved:  via %s", srcSrc)
	if srcSrc == "env" {
		fmt.Printf(" ($%s)", pkg.NOLANG_SRC)
	}
	fmt.Println()

	// Environment variables
	stdEnvVal := os.Getenv(pkg.NOLANG_STD_SRC)
	if stdEnvVal != "" {
		fmt.Printf("$%s: %s\n", pkg.NOLANG_STD_SRC, stdEnvVal)
	} else {
		fmt.Printf("$%s: (not set)\n", pkg.NOLANG_STD_SRC)
	}
	srcEnvVal := os.Getenv(pkg.NOLANG_SRC)
	if srcEnvVal != "" {
		fmt.Printf("$%s:  %s\n", pkg.NOLANG_SRC, srcEnvVal)
	} else {
		fmt.Printf("$%s:  (not set)\n", pkg.NOLANG_SRC)
	}

	// Working directory
	if cwd, err := os.Getwd(); err == nil {
		fmt.Printf("workdir:     %s\n", cwd)
	}

	// Std module count
	modules := checker.GetStdModules()
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

	// `no init` only defines the workspace: it creates workspace.jsonc (if missing)
	// and does NOT generate a package.jsonc. Packages are added later via `no new`.
	createWorkspaceFile()

	fmt.Printf("Workspace initialized in %s\n", dir)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - workspace.jsonc (workspace definition)")
	fmt.Println("")
	fmt.Println("Next: create a package with `no new <name>`")
}

func newProject(name string) {
	// Register this package in the workspace (defined by `no init`) before changing
	// into the new package directory. The package directory itself is created next.
	registerPackageInWorkspace(name)

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

	// 從 git 和環境變數中探測作者、郵箱、倉庫等信息
	gitInfo := detectGitInfo()

	config := ProjectConfig{
		Name:         name,
		Version:      "0.1.0",
		Author:       gitInfo.Author,
		Email:        gitInfo.Email,
		Repository:   gitInfo.Repository,
		Homepage:     gitInfo.Homepage,
		Dependencies: map[string]string{},
		Main:         "main.no",
		Compiler: CompilerConfig{
			Version: "0.1.0",
		},
		Output: "/dist",
	}

	createConfigFile(config)
	createMainFile()
	createSrcDirectory()
	createLibFile(name)
	createTestDirectory()

	fmt.Printf("Package created: %s\n", name)
	fmt.Println("")
	fmt.Println("Files created:")
	fmt.Println("  - package.jsonc (package configuration)")
	fmt.Println("  - main.no (main entry file)")
	fmt.Println("  - lib.no (library export file)")
	fmt.Println("  - src/ (source directory)")
	fmt.Println("  - tests/ (test directory)")
	fmt.Println("")
	if gitInfo.Author != "" || gitInfo.Email != "" || gitInfo.Repository != "" {
		fmt.Println("Pre-filled from git/environment:")
		if gitInfo.Author != "" {
			fmt.Printf("  author:     %s\n", gitInfo.Author)
		}
		if gitInfo.Email != "" {
			fmt.Printf("  email:      %s\n", gitInfo.Email)
		}
		if gitInfo.Repository != "" {
			fmt.Printf("  repository: %s\n", gitInfo.Repository)
		}
		if gitInfo.Homepage != "" {
			fmt.Printf("  homepage:   %s\n", gitInfo.Homepage)
		}
		fmt.Println("")
	}
	fmt.Printf("Registered '%s' in workspace.jsonc\n", name)
}

func createConfigFile(config ProjectConfig) {

	content := fmt.Sprintf(`{
  "name": "%s",
  "description": "%s",
  "keywords": [],
  "author": "%s",
  "email": "%s",
  "organization": "%s",
  "repository": "%s",
  "homepage": "%s",
  "license": "%s",
  "mirrors": [],
  "dependencies": %s,
  "compiler": {
    "version": "%s",
	"link-libs": [],
  },
  "output": "%s",
  "ignore": [],
}`,
		config.Name,
		config.Description,
		config.Author,
		config.Email,
		config.Organization,
		config.Repository,
		config.Homepage,
		config.License,
		formatDependencies(config.Dependencies),
		config.Compiler.Version,
		config.Output,
	)

	err := os.WriteFile("package.jsonc", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing config file: %v\n", err)
	}
}

// GitInfo holds author/repository information detected from git config and environment.
type GitInfo struct {
	Author     string
	Email      string
	Repository string
	Homepage   string
}

// detectGitInfo tries to obtain author name, email, and repository URL from:
//  1. `git config user.name` / `git config user.email` / `git config remote.origin.url`
//  2. Environment variables: $GIT_AUTHOR_NAME, $GIT_AUTHOR_EMAIL,
//     $USER (or $USERNAME on Windows), $EMAIL
//
// For remote URLs in SSH format (git@host:org/repo.git) or HTTPS format,
// the homepage is derived as an https URL.
func detectGitInfo() GitInfo {
	var info GitInfo

	// --- Author ---
	// 優先使用 git config user.name
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		info.Author = strings.TrimSpace(string(out))
	}
	// 回退到環境變數
	if info.Author == "" {
		info.Author = os.Getenv("GIT_AUTHOR_NAME")
	}
	if info.Author == "" {
		info.Author = os.Getenv("USER")
	}
	if info.Author == "" {
		info.Author = os.Getenv("USERNAME")
	}

	// --- Email ---
	if out, err := exec.Command("git", "config", "user.email").Output(); err == nil {
		info.Email = strings.TrimSpace(string(out))
	}
	if info.Email == "" {
		info.Email = os.Getenv("GIT_AUTHOR_EMAIL")
	}
	if info.Email == "" {
		info.Email = os.Getenv("EMAIL")
	}

	// --- Repository & Homepage ---
	if out, err := exec.Command("git", "config", "remote.origin.url").Output(); err == nil {
		repoURL := strings.TrimSpace(string(out))
		if repoURL != "" {
			info.Repository = repoURL
			info.Homepage = gitURLToHTTPS(repoURL)
		}
	}

	return info
}

// gitURLToHTTPS converts a git remote URL (SSH or HTTPS) to an https:// URL suitable
// for the "homepage" field. Returns "" if the URL cannot be normalized.
//
//	git@github.com:org/repo.git  -> https://github.com/org/repo
//	https://github.com/org/repo.git -> https://github.com/org/repo
//	git@gitlab.com:org/repo.git  -> https://gitlab.com/org/repo
func gitURLToHTTPS(url string) string {
	url = strings.TrimSpace(url)

	// SSH format: git@host:org/repo.git
	if strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://") {
		// git@host:path -> host/path
		s := strings.TrimPrefix(url, "ssh://")
		s = strings.TrimPrefix(s, "git@")
		s = strings.Replace(s, ":", "/", 1)
		// 移除 .git 後綴
		s = strings.TrimSuffix(s, ".git")
		return "https://" + s
	}

	// HTTPS format: https://github.com/org/repo.git
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return strings.TrimSuffix(url, ".git")
	}

	return ""
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

// createWorkspaceFile generates a workspace.jsonc in the current directory if one
// does not already exist. A freshly initialized workspace starts empty (no packages
// yet); packages are added via `no new`, which also registers them here. An existing
// workspace.jsonc is never overwritten.
func createWorkspaceFile() {
	wsFile := "workspace.jsonc"
	if _, err := os.Stat(wsFile); err == nil {
		// workspace.jsonc already exists; keep the user's configuration intact.
		return
	}
	content := `{
}
`
	if err := os.WriteFile(wsFile, []byte(content), 0644); err != nil {
		fmt.Printf("Error writing workspace file: %v\n", err)
	}
}

// registerPackageInWorkspace records the new package in the nearest workspace.jsonc
// (searched upward from the current directory). If no workspace.jsonc is found, one
// is created in the current directory so the package is always registered.
func registerPackageInWorkspace(name string) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	wsRoot, ok := findWorkspaceRoot(cwd)
	if !ok {
		// No workspace.jsonc found anywhere upward; create one in cwd.
		wsRoot = cwd
	}

	wsFile := filepath.Join(wsRoot, "workspace.jsonc")
	ws := map[string]string{}
	if raw, err := os.ReadFile(wsFile); err == nil && len(raw) > 0 {
		// Use the JSONC parser so that comments and trailing commas — both
		// legal in JSONC — are handled natively without json.Unmarshal.
		parsed, perr := jsoncParseMap(raw)
		if perr != nil {
			fmt.Printf("Warning: parsing workspace.jsonc: %v\n", perr)
		} else {
			ws = parsed
		}
	}
	if ws == nil {
		ws = make(map[string]string)
	}

	rel, err := filepath.Rel(wsRoot, filepath.Join(cwd, name))
	if err != nil {
		rel = name
	}
	ws[name] = "./" + rel

	if err := os.WriteFile(wsFile, []byte(jsoncMarshalMap(ws)), 0644); err != nil {
		fmt.Printf("Error writing workspace file: %v\n", err)
	}
}

// findWorkspaceRoot walks up from start looking for a workspace.jsonc file.
func findWorkspaceRoot(start string) (string, bool) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "workspace.jsonc")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// resolveRunTarget interprets the `no run` positional argument as one of:
//  1. an existing .no file                 -> run that file
//  2. an existing directory                -> run its main.no
//  3. a package name in the nearest workspace.jsonc -> run that package's main.no
//
// An empty argument (or ".") defaults to the current directory (folder semantics).
// It returns the path to the entry .no file to build & run, or an error if the
// argument does not match any of the three forms.
func resolveRunTarget(arg string) (string, error) {
	// 無參數或 "."：目前目錄（資料夾語意）。
	if arg == "" || arg == "." {
		if _, werr := os.Stat("workspace.jsonc"); werr == nil {
			if _, merr := os.Stat("main.no"); merr != nil {
				return "", fmt.Errorf("current directory is a workspace root with no main.no; run a package with: no run <package>")
			}
		}
		return ".", nil
	}

	// 1. 已存在的檔案
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		return arg, nil
	}

	// 2. 已存在的資料夾 -> 其 main.no
	if info, err := os.Stat(arg); err == nil && info.IsDir() {
		mainPath := filepath.Join(arg, "main.no")
		if _, err := os.Stat(mainPath); err != nil {
			return "", fmt.Errorf("main.no not found in %s", arg)
		}
		return mainPath, nil
	}

	// 3. 最近 workspace.jsonc 中註冊的套件名稱
	if cwd, err := os.Getwd(); err == nil {
		if wsRoot, ok := findWorkspaceRoot(cwd); ok {
			wsFile := filepath.Join(wsRoot, "workspace.jsonc")
			if raw, rerr := os.ReadFile(wsFile); rerr == nil && len(raw) > 0 {
				ws, perr := jsoncParseMap(raw)
				if perr == nil && len(ws) > 0 {
					if rel, found := ws[arg]; found {
						dir := rel
						if !filepath.IsAbs(dir) {
							dir = filepath.Join(wsRoot, rel)
						}
						mainPath := filepath.Join(dir, "main.no")
						if _, serr := os.Stat(mainPath); serr != nil {
							return "", fmt.Errorf("main.no not found in package %q (%s)", arg, dir)
						}
						return mainPath, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no such file, directory, or package: %s", arg)
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

	content := `; Example module
greet = (name str) {
    print('Hello, ' - name)
}
`
	err = os.WriteFile("src/utils.no", []byte(content), 0644)
	if err != nil {
		fmt.Printf("Error writing utils.no: %v\n", err)
	}
}

func createLibFile(name string) {
	content := fmt.Sprintf(`; Export declarations
@ /%s/src/utils.greet greet
`, name)
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

	content := `; Test example
; test-greet = () {
;     print('test passed')
; }
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
	data, err := os.ReadFile("package.jsonc")
	if err != nil {
		return nil, fmt.Errorf("package.jsonc not found. Run 'no new <name>' to create a package, or cd into a package directory")
	}
	return jsoncParseProjectConfig(data)
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

func syncDependencies() {
	fmt.Println("Syncing dependencies...")

	pkg, err := nbuild.LoadPackage(".")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if pkg == nil {
		fmt.Println("Error: package.jsonc not found. Run 'no new <name>' to create a package, or cd into a package directory")
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
			fmt.Fprintf(os.Stderr, "Error: package.jsonc not found in current directory\n")
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
		CC:              "clang",
		Output:          outPath,
		Verbose:         verbose,
		CompilerVersion: version,
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
	// 載入 package 配置以套用 ignore 列表
	fmtPkg, _ := nbuild.LoadPackage(dirname)
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
		// 跳過 ignore 列表中匹配的檔案
		if fmtPkg != nil && fmtPkg.IsIgnored(path) {
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
	nbuild.ClearCaches()
	// Extract -ld-KEY=VALUE pairs before flag parsing
	ldFlags, args := parseLDFlags(args)
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
	fmt.Println("Use -v to emit LLVM IR (.ll) files for analysis alongside the build.")
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

	// 如果未通過命令列指定後端，檢查 package.jsonc 的 emit 配置自動選擇 JS 後端
	useJS := *jsBackend
	if !useJS && !*wasmDirect {
		checkDir := inputPath
		if info, err := os.Stat(inputPath); err == nil && !info.IsDir() {
			checkDir = filepath.Dir(inputPath)
		}
		if pkg, _ := nbuild.LoadPackage(checkDir); pkg != nil && pkg.Compiler.Emit == "js" {
			useJS = true
		}
	}

	opts := nbuild.BuildOptions{
		CC:              *cc,
		Target:          targetStr,
		Verbose:         verbose,
		Output:          *outputFile,
		NoBoundsCheck:   *unsafe,
		UseDirectWasm:   *wasmDirect,
		UseJS:           useJS,
		BrowserMode:     *browserMode,
		CompilerVersion: version,
		LDFlags:         ldFlags,
	}

	// JS 後端路徑：繞過 LLVM 工具鏈，直接發射 JavaScript 原始碼（型別擦除）。
	if useJS {
		// 如果 inputPath 是目錄，嘗試解析為 main.no
		if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
			mainPath := filepath.Join(inputPath, "main.no")
			if _, err := os.Stat(mainPath); err == nil {
				inputPath = mainPath
			} else {
				fmt.Fprintln(os.Stderr, "Error: JS backend requires an explicit input file or main.no (workspace mode not supported)")
				os.Exit(1)
			}
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
				// 嘗試從同目錄的 package.jsonc 取得套件名稱
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
	nbuild.ClearCaches()
	// Extract -ld-KEY=VALUE pairs before flag parsing
	ldFlags, args := parseLDFlags(args)
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cc := fs.String("cc", "clang", "C compiler: clang (default), zig")
	target := fs.String("target", "", "Target triple (e.g. x86_64-linux-gnu, aarch64-macos-gnu, x86_64-windows-gnu, wasm32-wasi)")
	unsafe := fs.Bool("unsafe", false, "Skip bounds checks for maximum performance (unsafe)")
	wasmDirect := fs.Bool("wasm-direct", false, "Use Direct WASM backend (no LLVM toolchain required, browser-compatible)")
	jsBackend := fs.Bool("js", false, "Use JS backend (emit JavaScript, run with node)")
	browserMode := fs.Bool("browser", false, "Open in browser (requires --js)")
	fs.Usage = func() {
		fmt.Println("Usage: no run [<package|dir|file>]")
		fmt.Println("")
		fmt.Println("Build and run a Nolang project.")
		fmt.Println("The argument is resolved in this order:")
		fmt.Println("  1. an existing .no file        -> run that file")
		fmt.Println("  2. an existing directory       -> run its main.no")
		fmt.Println("  3. a package name in workspace  -> run that package's main.no")
		fmt.Println("With no argument, runs main.no in the current directory.")
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
		fmt.Println("  no run                       build and run main.no in current dir")
		fmt.Println("  no run foo                   run package 'foo' (from workspace.jsonc)")
		fmt.Println("  no run ./foo                 run the ./foo directory's main.no")
		fmt.Println("  no run main.no               build and run main.no")
		fmt.Println("  no run -cc zig main.no       build and run with Zig compiler")
		fmt.Println("  no run -target wasm32-wasi main.no")
		fmt.Println("  no run --wasm-direct main.no  build via Direct WASM backend then run with wasmtime (if available)")
		fmt.Println("  no run --js main.no            build via JS backend then run with node")
		fmt.Println("  no run --js --browser main.no  build browser JS + HTML and open in default browser")
	}
	_ = fs.Parse(args)

	// 解析執行目標：包名（來自最近 workspace.jsonc）/ 資料夾 / 檔案。
	// 無參數時預設為目前目錄（資料夾語意，執行其中的 main.no）。
	var runArg string
	if len(fs.Args()) > 0 {
		runArg = fs.Args()[0]
	}
	inputPath, rerr := resolveRunTarget(runArg)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", rerr)
		os.Exit(1)
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

	// 如果未通過命令列指定後端，檢查 package.jsonc 的 emit 配置自動選擇 JS 後端
	useJS := *jsBackend
	if !useJS && !*wasmDirect {
		checkDir := filepath.Dir(inputPath)
		if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
			checkDir = inputPath
		}
		if pkg, _ := nbuild.LoadPackage(checkDir); pkg != nil && pkg.Compiler.Emit == "js" {
			useJS = true
		}
	}

	// JS 後端路徑：編譯為 .js 後以 node 執行。
	if useJS {
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
		CC:              *cc,
		Target:          targetStr,
		Output:          outPath,
		Verbose:         verbose,
		NoBoundsCheck:   *unsafe,
		UseDirectWasm:   *wasmDirect,
		CompilerVersion: version,
		LDFlags:         ldFlags,
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
	nbuild.ClearCaches()
	// Extract -ld-KEY=VALUE pairs before flag parsing
	ldFlags, args := parseLDFlags(args)
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

	// 載入 package 配置以套用 ignore 列表
	testPkg, _ := nbuild.LoadPackage(filepath.Dir(inputPath))

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
			// 跳過 ignore 列表中匹配的檔案
			if testPkg != nil && testPkg.IsIgnored(path) {
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

	// 兩遍遍歷方式：
	// Pass 1（並行構建）：所有測試文件並行編譯，共用全局 token/AST 緩存。
	//   編譯是最耗時的階段（lex+parse+check+LLVM codegen），並行化可大幅加速。
	// Pass 2（順序執行）：依次執行編譯產物，避免 stdout/stderr 交錯。
	type testBuildResult struct {
		testFile string
		binPath  string
		tmpDir   string
		err      error
	}

	// Pass 1: 並行構建所有測試文件
	var wg sync.WaitGroup
	concurrency := runtime.NumCPU()
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	results := make([]testBuildResult, len(testFiles))

	for i, tf := range testFiles {
		if verbose {
			fmt.Printf("Testing: %s\n", tf)
		}
		wg.Add(1)
		go func(idx int, f string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			tmpDir, err := os.MkdirTemp("", "nolang-test")
			if err != nil {
				results[idx] = testBuildResult{testFile: f, err: err}
				return
			}

			if useDirectWasm {
				outPath := filepath.Join(tmpDir, "out.wasm")
				wasmBytes, berr := nbuild.BuildDirectWasm(f, nbuild.BuildOptions{
					Target:        targetStr,
					UseDirectWasm: true,
					Verbose:       false,
					LDFlags:       ldFlags,
				})
				if berr != nil {
					results[idx] = testBuildResult{testFile: f, tmpDir: tmpDir, err: berr}
					return
				}
				if werr := os.WriteFile(outPath, wasmBytes, 0755); werr != nil {
					results[idx] = testBuildResult{testFile: f, tmpDir: tmpDir, err: werr}
					return
				}
				results[idx] = testBuildResult{testFile: f, binPath: outPath, tmpDir: tmpDir}
				return
			}

			outPath := filepath.Join(tmpDir, "out")
			opts := nbuild.BuildOptions{
				CC:            *cc,
				Target:        targetStr,
				Output:        outPath,
				Verbose:       false,
				UseDirectWasm: *wasmDirect,
				LDFlags:       ldFlags,
			}
			if err := nbuild.BuildFile(f, opts); err != nil {
				results[idx] = testBuildResult{testFile: f, tmpDir: tmpDir, err: err}
				return
			}
			results[idx] = testBuildResult{testFile: f, binPath: outPath, tmpDir: tmpDir}
		}(i, tf)
	}
	wg.Wait()

	// Pass 2: 順序執行編譯產物（避免輸出交錯）
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s\n  %v\n", r.testFile, r.err)
			hadFailure = true
			if r.tmpDir != "" {
				os.RemoveAll(r.tmpDir)
			}
			continue
		}

		// WASM 下無法執行編譯產物（瀏覽器沙箱不支援 spawn 子行程）。
		if runtime.GOOS == "wasip1" {
			fmt.Fprintln(os.Stderr, "Error: running compiled binary not supported in browser playground")
			os.Exit(1)
		}

		var cmd *exec.Cmd
		if useDirectWasm {
			cmd = exec.Command("wasmtime", "run", r.binPath)
		} else {
			cmd = exec.Command(r.binPath)
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s (exit code %v)\n", r.testFile, err)
			hadFailure = true
		}
		if r.tmpDir != "" {
			os.RemoveAll(r.tmpDir)
		}
	}

	if hadFailure {
		os.Exit(1)
	}
}

func vetCommand(args []string) {
	nbuild.ClearCaches()
	fs := flag.NewFlagSet("vet", flag.ExitOnError)
	reuseStdAST := fs.Bool("reuse-std-ast", false, "reuse already-parsed std Program AST for 'no vet src/std' (experimental, skips re-parsing each std module)")
	strict := fs.Bool("strict", false, "upgrade lint warnings/hints to errors")
	fs.Usage = func() {
		fmt.Println("Usage: no vet [--strict] [<file|dir>]")
		fmt.Println("")
		fmt.Println("Validate Nolang source files without producing output.")
		fmt.Println("If directory, validates all .no files in that directory.")
		fmt.Println("Output format: file:line:col: [SEVERITY] source: message")
		fmt.Println("")
		fmt.Println("Flags:")
		fmt.Println("  --strict         upgrade all lint warnings/hints to errors")
		fmt.Println("  --reuse-std-ast  reuse already-parsed std Program AST (experimental)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no vet                     validate main.no in current dir")
		fmt.Println("  no vet main.no             validate main.no")
		fmt.Println("  no vet src/std/            validate all .no files in src/std/")
		fmt.Println("  no vet --strict src/std/   fail if any lint warning/hint is found")
	}
	_ = fs.Parse(args)

	// --reuse-std-ast 開關：轉為環境變量，由 CompileTarget 直接讀取，
	// 避免透傳到編譯鏈多層簽名（VetFile → compiler.Compile → CompileTarget）。
	if *reuseStdAST {
		os.Setenv("NOLANG_REUSE_STD_AST", "1")
	}

	inputPath := "."
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	}

	opts := nbuild.BuildOptions{
		Verbose: verbose,
		Strict:  *strict,
	}

	// 檢查是文件還是目錄
	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 載入 package 配置以套用 ignore 列表 + 解析依賴
	vetPkg, _ := nbuild.LoadPackage(inputPath)
	if vetPkg != nil && info.IsDir() {
		if _, err := vetPkg.EnsureDependencies(10); err != nil {
			fmt.Fprintf(os.Stderr, "Error: dependency resolution failed: %v\n", err)
			os.Exit(1)
		}
	}

	// lintResults 收集所有文件的 lint 結果，最後統一輸出
	var allLints []nbuild.LintResult

	if info.IsDir() {
		// 目錄模式：遞迴驗證所有 .no 文件（與 testCommand/fmtProcessDirectory/BuildWorkspace 一致）
		var files []string
		err = filepath.WalkDir(inputPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".no") {
				// 跳過 ignore 列表中匹配的檔案
				if vetPkg != nil && vetPkg.IsIgnored(path) {
					return nil
				}
				// 檢查檔案名是否符合變量名規則：小寫字母、數字、中劃線
				base := strings.TrimSuffix(filepath.Base(path), ".no")
				if !isValidFileName(base) {
					allLints = append(allLints, nbuild.LintResult{
						File: path,
						Lints: []checker.LintResult{{
							Severity: checker.LintError,
							Source:   "nolang-lint",
							Message:  fmt.Sprintf("file name '%s.no' does not match naming convention: lowercase letters, digits, and hyphens only", base),
							TraceID:  "fn-rule",
						}},
					})
				}
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no .no files found in %s\n", inputPath)
			os.Exit(1)
		}
		// 並行驗證：每個文件獨立編譯，使用 worker pool 並行處理。
		// vet 模式下跳過 ClearModuleCache，全域 parseProgramFileCache 為執行緒安全
		// （mutex 保護），可跨文件復用。
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU())
		var errMu sync.Mutex
		var firstErr error
		var lintMu sync.Mutex
		for _, file := range files {
			wg.Add(1)
			sem <- struct{}{}
			go func(f string) {
				defer wg.Done()
				defer func() { <-sem }()
				lints, e := nbuild.VetFileWithLints(f, opts)
				if e != nil {
					// I/O error（讀文件失敗等），作為 fatal error
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("Error in %s: %w", f, e)
					}
					errMu.Unlock()
				}
				if len(lints) > 0 {
					lintMu.Lock()
					allLints = append(allLints, nbuild.LintResult{
						File:  f,
						Lints: lints,
					})
					lintMu.Unlock()
				}
			}(file)
		}
		wg.Wait()
		if firstErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", firstErr)
			os.Exit(1)
		}
	} else {
		// 文件模式：驗證單個文件
		// 檢查檔案名是否符合變量名規則
		base := strings.TrimSuffix(filepath.Base(inputPath), ".no")
		if !isValidFileName(base) {
			allLints = append(allLints, nbuild.LintResult{
				File: inputPath,
				Lints: []checker.LintResult{{
					Severity: checker.LintError,
					Source:   "nolang-lint",
					Message:  fmt.Sprintf("file name '%s.no' does not match naming convention: lowercase letters, digits, and hyphens only", base),
					TraceID:  "fn-rule",
				}},
			})
		}
		lints, err := nbuild.VetFileWithLints(inputPath, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(lints) > 0 {
			allLints = append(allLints, nbuild.LintResult{
				File:  inputPath,
				Lints: lints,
			})
		}
	}

	// 輸出結構化診斷到 stdout（與 LSP vet 格式一致），統計到 stderr
	errorCount := printLintResults(allLints)

	if errorCount > 0 {
		os.Exit(1)
	}

	if verbose {
		fmt.Println("Validation successful")
	}
}

// printLintResults 輸出結構化診斷到 stdout（格式與 LSP vet 一致），
// 統計摘要到 stderr。返回 error 數量。
func printLintResults(results []nbuild.LintResult) int {
	// 按文件、行、列排序
	sort.Slice(results, func(i, j int) bool {
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		if results[i].Lints == nil || len(results[i].Lints) == 0 {
			return true
		}
		if results[j].Lints == nil || len(results[j].Lints) == 0 {
			return false
		}
		return true
	})

	errorCount := 0
	warnCount := 0
	hintCount := 0
	for _, fileResult := range results {
		for _, l := range fileResult.Lints {
			sev := strings.ToUpper(string(l.Severity))
			// 優先使用 lint 結果攜帶的 File（來自 merged 程式中語句的原始檔案路徑），
			// 為空時回退到 fileResult.File（vet 命令遍歷的輸入檔案路徑）。
			filePath := l.File
			if filePath == "" {
				filePath = fileResult.File
			}
			if l.Line > 0 {
				fmt.Printf("%s:%d:%d: [%s] %s: %s [%s]\n",
					filePath, l.Line, l.Column, sev, l.Source, l.Message, l.TraceID)
			} else {
				fmt.Printf("%s: [%s] %s: %s [%s]\n",
					filePath, sev, l.Source, l.Message, l.TraceID)
			}
			switch l.Severity {
			case nbuild.LintError:
				errorCount++
			case nbuild.LintWarning:
				warnCount++
			case nbuild.LintHint:
				hintCount++
			}
		}
	}
	if errorCount > 0 || warnCount > 0 || hintCount > 0 {
		fmt.Fprintf(os.Stderr, "\n%d error(s), %d warning(s), %d hint(s)\n",
			errorCount, warnCount, hintCount)
	} else {
		fmt.Println("No issues found.")
	}
	return errorCount
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

func astCommand(args []string) {
	fs := flag.NewFlagSet("ast", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: no ast <file>")
		fmt.Println("")
		fmt.Println("Parse a Nolang source file and print its AST to stdout.")
		fmt.Println("This is useful for debugging parser output.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no ast main.no           print AST for main.no")
		fmt.Println("  no ast src/std/vec.no    print AST for vec.no")
	}
	_ = fs.Parse(args)

	if len(fs.Args()) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	inputPath := fs.Args()[0]
	source, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	l := lexer.New(string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()

	if errs := p.Errors(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "Parse error: %s\n", e)
		}
		os.Exit(1)
	}

	fmt.Println(dump.Dump(program))
}

// isValidFileName 檢查檔案名（不含 .no 副檔名）是否符合變量名規則：
// 只允許小寫字母 (a-z)、數字 (0-9) 和中劃線 (-)，且不以中劃線開頭或結尾。
func isValidFileName(name string) bool {
	if name == "" {
		return false
	}
	// 不以中劃線開頭或結尾
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	// 不允許連續中劃線
	if strings.Contains(name, "--") {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
