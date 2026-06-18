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
	// 定義所有可能的參數
	showVersion := flag.Bool("version", false, "Show version")
	help := flag.Bool("help", false, "Show help")
	stdio := flag.Bool("stdio", false, "Use stdio for communication")

	flag.Parse()

	if *help {
		fmt.Println("Nolang Language Server (version " + version + ")")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
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
