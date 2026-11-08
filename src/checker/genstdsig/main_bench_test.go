package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/checker"
)

func findRepoRoot() string {
	_, thisFile, _, _ := caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// caller is a thin wrapper so the benchmark file doesn't need runtime import
// at every call site; it matches main.go's usage.
func caller(skip int) (uintptr, string, int, bool) {
	return callerImpl(skip)
}
