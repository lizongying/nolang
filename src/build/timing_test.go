package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lizongying/nolang/checker"
)

// TestStagedTiming is a temporary diagnostic that reports wall-clock time for
// each stage of the `no build` pipeline. Run with:
//
//	go test ./build -run TestStagedTiming -v -count=1 -timeout 300s \
//	    -args (input via NOPROF_INPUT env)
func TestStagedTiming(t *testing.T) {
	input := os.Getenv("NOPROF_INPUT")
	if input == "" {
		t.Skip("set NOPROF_INPUT to a .no file")
	}
	total := time.Now()
	mark := total
	lap := func(label string) {
		now := time.Now()
		fmt.Printf("  %-36s %8.1f ms\n", label, float64(now.Sub(mark).Microseconds())/1000)
		mark = now
	}

	ClearCaches()
	_ = CheckToolchain("clang")
	lap("CheckToolchain (llvm-config exec)")

	source, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	lap("read source")

	checker.CollectStdModuleSignatures()
	lap("CollectStdModuleSignatures (1st pass)")

	compiler := NewTranspiler(nil)
	compiler.sourcePath = input
	goos, goarch := parseTargetPlatform(DetectTarget())
	compiler.SetTargetPlatform(goos, goarch)
	code, err := compiler.Compile(string(source))
	lap("Compile (parse+sema+merge+codegen)")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	fmt.Printf("  %-36s %8d bytes\n", "raw IR size", len(code))

	tmp := t.TempDir()
	base := strings.TrimSuffix(filepath.Base(input), ".no")
	ll := filepath.Join(tmp, base+".ll")
	os.WriteFile(ll, []byte(code), 0644)
	lap("write .ll")

	if os.Getenv("NOPROF_FAST") != "" {
		// single-process backend: clang consumes LLVM IR directly
		runTool("clang", "-O3", "-ffp-contract=fast", "-x", "ir", ll, "-o", filepath.Join(tmp, base))
		lap("clang -O3 -x ir (single process)")
		fmt.Printf("  %-36s %8.1f ms\n", "TOTAL (fast backend)", float64(time.Since(total).Microseconds())/1000)
		return
	}

	optLL := filepath.Join(tmp, base+"_opt.ll")
	runTool("opt", "-O3", ll, "-S", "-o", optLL)
	lap("opt -O3")
	if st, err := os.Stat(optLL); err == nil {
		fmt.Printf("  %-36s %8d bytes (%.1f%% of raw)\n", "optimized IR size", st.Size(),
			100*float64(st.Size())/float64(len(code)))
	}

	s := filepath.Join(tmp, base+".s")
	runTool("llc", "--fp-contract=fast", optLL, "-o", s)
	lap("llc")

	runTool("clang", s, "-o", filepath.Join(tmp, base))
	lap("clang link")

	fmt.Printf("  %-36s %8.1f ms\n", "TOTAL", float64(time.Since(total).Microseconds())/1000)
}

// TestStdParseOnly isolates the cost of parsing the whole standard library.
func TestStdParseOnly(t *testing.T) {
	if os.Getenv("NOPROF_STD") == "" {
		t.Skip("set NOPROF_STD=1")
	}
	ClearCaches()
	start := time.Now()
	sigs, fields := checker.CollectStdModuleSignatures()
	fmt.Printf("  CollectStdModuleSignatures: %v  (%d fn sigs, %d structs)\n",
		time.Since(start), len(sigs), len(fields))
	start = time.Now()
	checker.CollectStdModuleSignatures()
	fmt.Printf("  second call (sync.Once cached): %v\n", time.Since(start))
	mods := checker.KnownStdModules()
	fmt.Printf("  std modules: %d\n", len(mods))
}

func runTool(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
