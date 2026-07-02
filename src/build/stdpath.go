package build

import (
	"os"
	"path/filepath"
	"sync"
)

// NOLANG_STD_SRC is the environment variable for overriding the std library source directory.
// Should point to the std/ directory directly, e.g. /path/to/src/std
const NOLANG_STD_SRC = "NOLANG_STD_SRC"

// NOLANG_SRC is the environment variable for overriding the source directory
// (third-party modules, local development). Should point to the src/ directory, e.g. /path/to/src
const NOLANG_SRC = "NOLANG_SRC"

var (
	stdSourceDirOnce sync.Once
	stdSourceDir     string
	stdSourceSource  string // "env", "binary", "default"

	srcDirOnce sync.Once
	srcDir     string
	srcSource  string // "env", "binary", "default"
)

// NoHomeDir returns ~/no, the base directory for all nolang data
// (source, cache, bin, etc.)
func NoHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "no")
	}
	return filepath.Join(home, "no")
}

// defaultNoSrcDir returns ~/no/src
func defaultNoSrcDir() string {
	return filepath.Join(NoHomeDir(), "src")
}

// GetStdSourceDir returns the std source directory (the folder containing .no std module files).
//
// Resolution order:
//  1. $NOLANG_STD_SRC environment variable
//  2. Relative to the binary: <exedir>/../src/std/ or <exedir>/../../src/std/
//  3. Default: ~/no/src/std
func GetStdSourceDir() (dir string, source string) {
	stdSourceDirOnce.Do(func() {
		// 1. Environment variable
		if env := os.Getenv(NOLANG_STD_SRC); env != "" {
			abs, err := filepath.Abs(env)
			if err == nil {
				if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
					stdSourceDir = abs
					stdSourceSource = "env"
					return
				}
			}
		}

		// 2. Relative to binary
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			candidates := []string{
				filepath.Join(exeDir, "..", "src", "std"),       // bin/ → src/std/
				filepath.Join(exeDir, "..", "..", "src", "std"), // vscode-nolang/server/ → src/std/
			}
			for _, c := range candidates {
				abs, err := filepath.Abs(c)
				if err != nil {
					continue
				}
				if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
					stdSourceDir = abs
					stdSourceSource = "binary"
					return
				}
			}
		}

		// 3. Default: ~/no/src/std
		stdSourceDir = filepath.Join(defaultNoSrcDir(), "std")
		stdSourceSource = "default"
	})
	return stdSourceDir, stdSourceSource
}

// GetStdSourceFile returns the full path to a std library source file.
// e.g. GetStdSourceFile("hash/rand") → "/path/to/src/std/hash/rand.no"
func GetStdSourceFile(modulePath string) string {
	dir, _ := GetStdSourceDir()
	return filepath.Join(dir, modulePath) + ".no"
}

// GetSourceDir returns the source directory (used for third-party modules).
//
// Resolution order:
//  1. $NOLANG_SRC environment variable
//  2. Relative to the binary: <exedir>/../src/ or <exedir>/../../src/
//  3. Default: ~/no/src
func GetSourceDir() (dir string, source string) {
	srcDirOnce.Do(func() {
		// 1. Environment variable
		if env := os.Getenv(NOLANG_SRC); env != "" {
			abs, err := filepath.Abs(env)
			if err == nil {
				if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
					srcDir = abs
					srcSource = "env"
					return
				}
			}
		}

		// 2. Relative to binary
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			candidates := []string{
				filepath.Join(exeDir, "..", "src"),       // bin/ → src/
				filepath.Join(exeDir, "..", "..", "src"), // vscode-nolang/server/ → src/
			}
			for _, c := range candidates {
				abs, err := filepath.Abs(c)
				if err != nil {
					continue
				}
				if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
					srcDir = abs
					srcSource = "binary"
					return
				}
			}
		}

		// 3. Default: ~/no/src
		srcDir = defaultNoSrcDir()
		srcSource = "default"
	})
	return srcDir, srcSource
}

// ModuleShortName extracts the short name from a std path.
// e.g. "std/hash/rand" → "hash/rand", "std" → ""
func ModuleShortName(path string) string {
	return moduleShortName(path)
}
