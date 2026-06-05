package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/lizongying/nolang/lsp"
)

func main() {
	// 定義所有可能的參數
	showVersion := flag.Bool("version", false, "Show version")
	help := flag.Bool("help", false, "Show help")
	stdio := flag.Bool("stdio", false, "Use stdio for communication")

	flag.Parse()

	if *help {
		fmt.Println("Nolang Language Server")
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		return
	}

	if *showVersion {
		fmt.Println("nolang-lsp v0.1.0")
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
