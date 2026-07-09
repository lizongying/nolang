package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/lizongying/nolang/lsp"
)

// version is injected at build time via -ldflags
var version = "dev"

// buildDate is injected at build time via -ldflags
var buildDate = ""

func main() {
	// Check for "vet" subcommand before flag parsing
	if len(os.Args) > 1 && os.Args[1] == "vet" {
		vetCommand(os.Args[2:])
		return
	}

	// 定義所有可能的參數
	showVersion := flag.Bool("version", false, "Show version")
	help := flag.Bool("help", false, "Show help")
	stdio := flag.Bool("stdio", false, "Use stdio for communication")

	flag.Parse()

	if *help {
		printUsage()
		return
	}

	if *showVersion {
		if buildDate != "" {
			if sec, err := strconv.ParseInt(buildDate, 10, 64); err == nil {
				t := time.Unix(sec, 0).UTC()
				fmt.Printf("version: %s(%s)\n", version, t.Format("2006-01-02 15:04:05"))
				return
			}
		}
		fmt.Printf("version: %s\n", version)
		return
	}

	// 如果有 --stdio 參數，忽略它（LSP 總是使用 stdio）
	if *stdio {
		log.Println("stdio mode enabled (ignored, LSP always uses stdio)")
	}

	// 設置日誌
	log.SetOutput(os.Stderr)
	log.Println("Nolang LSP Server starting...")
	log.Printf("Arguments: %v", os.Args)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 處理中斷信號
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		log.Println("Received interrupt signal")
		cancel()
	}()

	server := lsp.NewServer()
	if err := lsp.RunServer(ctx, server); err != nil {
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}

	log.Println("Server stopped")
}

func printUsage() {
	fmt.Println("Nolang Language Server (version " + version + ")")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  nolang-lsp                Start LSP server (stdio mode)")
	fmt.Println("  nolang-lsp vet <file|dir> Validate source files using LSP diagnostics")
	fmt.Println("  nolang-lsp -version       Show version")
	fmt.Println("  nolang-lsp -help          Show this help")
	fmt.Println()
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  nolang-lsp                          Start LSP server")
	fmt.Println("  nolang-lsp vet main.no              Validate a single file")
	fmt.Println("  nolang-lsp vet src/std/             Validate all .no files in directory")
}

// vetCommand runs the full LSP validation pipeline on the given file or directory.
func vetCommand(args []string) {
	fs := flag.NewFlagSet("vet", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Usage: nolang-lsp vet <file|dir>")
		fmt.Println()
		fmt.Println("Validate Nolang source files using the full LSP diagnostic pipeline.")
		fmt.Println("Includes parse errors, type checking, naming, undefined vars,")
		fmt.Println("function argument types, and more.")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  nolang-lsp vet main.no              Validate a single file")
		fmt.Println("  nolang-lsp vet src/std/             Validate all .no files recursively")
	}
	_ = fs.Parse(args)

	inputPath := "."
	if len(fs.Args()) > 0 {
		inputPath = fs.Args()[0]
	}

	results := lsp.VetPath(inputPath)
	errorCount := lsp.FormatVetResults(results)

	if errorCount > 0 {
		fmt.Fprintf(os.Stderr, "\n%d error(s), %d warning(s), %d hint(s)\n",
			countBySeverity(results, "error"),
			countBySeverity(results, "warning"),
			countBySeverity(results, "hint"))
		os.Exit(1)
	} else if len(results) > 0 {
		fmt.Printf("\nNo errors. %d warning(s), %d hint(s)\n",
			countBySeverity(results, "warning"),
			countBySeverity(results, "hint"))
	} else {
		fmt.Println("No issues found.")
	}
}

func countBySeverity(results []lsp.VetResult, severity string) int {
	count := 0
	for _, r := range results {
		if r.Severity == severity {
			count++
		}
	}
	return count
}
