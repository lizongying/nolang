package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/mir"
	"github.com/lizongying/nolang/parser"

	"github.com/lizongying/nolang/i18n"
)

// verifyMIRIRViaOpt pre-flights MIR-emitted IR through the LLVM toolchain so a
// broken IR is reported with the MIR-specific message ("MIR IR failed LLVM
// verification/assembly") instead of surfacing several seconds later as a
// generic optimizer error from the builder.
//
// MIR is still maturing; some constructs produce IR that only the optimizer's
// verifier rejects — e.g. register type mismatches, undefined callees, or a void
// value used where a result is expected.
//
// DEFAULT = CHEAP GATE. Only `opt -passes=verify` runs (~30 ms). This is exactly
// the check this stage exists for: the IR verifier is the first thing any `opt`
// pipeline runs. The previous default — a full `opt -O3` *plus* `llc` — did not
// just check the IR, it duplicated the builder's entire backend on every single
// build; combined with an IR shape LLVM's SROA cannot handle cheaply, that is
// what turned a 5-line json program into a 62 s compile (§13.3.15). Note the
// duplicated stages cannot even be kept for free: `llc` on *unoptimized* MIR IR
// is far slower than on the optimized IR (>100 s vs ~14 s), so a "verify-only"
// pre-flight that still assembled would be a pessimization.
//
// NOLANG_MIR_PREFLIGHT=full restores the old behaviour (full pipeline + llc) for
// hunting an IR shape that survives verification but breaks a transformation
// pass or the assembler. It changes only WHERE a build fails, never WHETHER it
// fails: the builder runs the identical command over the identical bytes, so an
// IR the full pre-flight rejects is rejected by the builder too.
//
// If `opt` is unavailable we cannot verify and assume the IR is sound (mirroring
// buildLLVMInternal's own opt-unavailable handling, which skips optimization).
func verifyMIRIRViaOpt(ll string) error {
	dir, err := os.MkdirTemp("", "nolang-mir-opt")
	if err != nil {
		return nil // cannot verify; do not break the build
	}
	if os.Getenv("NOLANG_MIR_KEEP_IR") == "" {
		defer os.RemoveAll(dir)
	}
	inPath := filepath.Join(dir, "m.ll")
	// LLVM 17+ 解析器自動升級 typed pointer → opaque `ptr`，無需 toOpaquePointers 重寫層（死代碼，2026-10-09 刪除）。
	if err := os.WriteFile(inPath, []byte(ll), 0644); err != nil {
		return nil
	}
	// Stage 1: IR verification. See the function comment: the default runs only
	// the verifier pass, because the full pipeline here duplicated the builder's
	// opt on every build.
	fullPreflight := os.Getenv("NOLANG_MIR_PREFLIGHT") == "full"
	if _, err := exec.LookPath("opt"); err == nil {
		outPath := filepath.Join(dir, "m_opt.ll")
		args := []string{"-passes=verify", inPath, "-S", "-o", outPath}
		if fullPreflight {
			optLevel := os.Getenv("NOLANG_OPT_LEVEL")
			if optLevel == "" {
				optLevel = "-O2"
			}
			args = []string{optLevel, inPath, "-S", "-o", outPath}
		}
		cmd := exec.Command("opt", args...)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			if os.Getenv("NOLANG_MIR_DUMP_BAD") != "" {
				_ = os.WriteFile("/tmp/mirb/failed.ll", []byte(ll), 0644)
			}
			return i18n.Errorf("d8en6a9j", "MIR IR failed LLVM verification: %v: %s", err, buf.String())
		}
		inPath = outPath
	}
	// Stage 2: assembly. opt can pass while llc still rejects the IR (e.g. an
	// instruction the codegen emitted that the assembler lowers incorrectly).
	// Skipped by default: assembling costs ~14 s on optimized json IR (>100 s on
	// unoptimized IR, see the function comment) and the builder assembles the
	// identical IR immediately after this returns.
	if _, err := exec.LookPath("llc"); err == nil && fullPreflight {
		sPath := filepath.Join(dir, "m.s")
		cmd := exec.Command("llc", "--fp-contract=fast", inPath, "-o", sPath)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			return i18n.Errorf("46qem63e", "MIR IR failed LLVM assembly: %v: %s", err, buf.String())
		}
	}
	return nil
}

// emitMIR is the codegen path. It lowers HIR -> MIR, analyzes memory, emits LLVM
// IR, and pre-flights it past the LLVM verifier (see verifyMIRIRViaOpt). It
// returns MIR-emitted IR only when every stage succeeds; on any failure it
// returns empty IR plus a reason and the caller rejects the build.
//
// There is deliberately NO fallback to a second backend. Until §13.3.13 this
// function took an `allowFallback` flag that, under NOLANG_MIR=2, degraded to the
// legacy HIR generator on ANY failure (unsupported construct, optimizer
// verification error, or panic). That made MIR unbreakable but also invisible: a
// gap silently produced legacy output, so nothing failed and nothing was
// reported. Failing loudly is the point — see §13.3.13 for the deletion.
//
// The deferred recover() is retained (panics during lowering are still bugs, not
// user errors) but now always propagates: it catches panics that neither
// EmitLLVM's own recovery nor opt verification would.
func (t *Transpiler) emitMIR(hirPkg *hir.Package) (out string, reason string) {
	debug := os.Getenv("NOLANG_MIR_DEBUG") != ""
	defer func() {
		if r := recover(); r != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "[MIR] recovered panic: %v\n", r)
			}
			out = "" // propagate empty; caller rejects the build
			reason = fmt.Sprintf("panic: %v", r)
		}
	}()
	mod, rep, diags := mir.LowerHIR(hirPkg, checker.CollectStdEnumVariants())
	t.applyOptionInlineThreshold(mod)
	if os.Getenv("NOLANG_MIR_DUMP_HIR") != "" {
		fmt.Fprintf(os.Stderr, "[HIR-DUMP]\n%s\n", hirPkg.Dump())
	}
	if mod == nil {
		return "", "lowering produced nil module"
	}
	// Lowering gaps that would produce WRONG-but-exit-0 output (a shape the v1 MIR
	// backend cannot lower and would emit unsubstituted) are hard errors.
	// Shipping silently-wrong output is the one outcome worse than refusing to
	// compile, so these fatal lower diagnostics (kind "interp") stop the build.
	//
	// kind "deprecated" joins them: it is the backstop for a call to a REMOVED
	// builtin (printf / eprintf) that reached lowering. The checker rejects
	// those first (ValidateDeprecatedPrintf) with a better, source-located
	// message, so this only fires if a call site somehow bypassed it — and
	// exactly then it must refuse the build rather than emit a call whose
	// target does not exist.
	if d, ok := firstFatalLowerDiag(diags); ok {
		// Surface the concrete lowering diagnostic (field name / type / shape),
		// not just the generic bucket. A bare "unsupported construct" forces a
		// re-run with debugger every single time — the diag already carries the
		// answer, so print it.
		return "", "unsupported construct in MIR backend: " + d.Kind + ": " + d.Msg
	}
	if os.Getenv("NOLANG_MIR_DUMP_MIR") != "" {
		fmt.Fprintf(os.Stderr, "[MIR-DUMP]\n%s\n", mod.DumpAnnotated())
	}
	// Memory-safety gate: only emit when the analysis is CLEAN. If the analyzer
	// found any unsafe construct (use-after-move, missing/duplicate drop,
	// borrow-escape, unsupported) the codegen would produce IR with a
	// double-free / UAF / leak — exactly the "lots of memory problems" class.
	// Those always surface as rep.Diagnostics, so a non-empty report means "MIR
	// cannot guarantee safety here" and the build is refused rather than emitting
	// a binary that aborts at runtime.
	if rep != nil && rep.HasErrors() {
		if debug {
			fmt.Fprintf(os.Stderr, "[MIR] memory analysis found unsafe constructs: %v\n", rep.Error())
		}
		return "", rep.Error()
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "[MIR] EmitLLVM failed: %v\n", err)
		}
		return "", fmt.Sprintf("EmitLLVM: %v", err)
	}
	if os.Getenv("NOLANG_MIR_DUMP_LL") != "" {
		if f, e := os.CreateTemp("", "nolang-mir-emit-*.ll"); e == nil {
			_ = os.WriteFile(f.Name(), []byte(ll), 0644)
			fmt.Fprintf(os.Stderr, "[MIR] emitted LLVM -> %s\n", f.Name())
		}
	}
	// verify 門禁（opt -passes=verify）僅 verbose（或 NOLANG_MIR_PREFLIGHT=full）
	// 時保留：非 verbose 走單進程 clang -x ir，嚴格 1 spawn，不再 spawn opt。
	// IR 若有錯，clang 自身會在 parse 期執行同一套 LLVM verifier 並報錯
	//（僅訊息不如 MIR 專屬錯誤具體）。
	runVerify := t.verbose || os.Getenv("NOLANG_MIR_PREFLIGHT") == "full"
	if runVerify {
		if verr := verifyMIRIRViaOpt(ll); verr != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "[MIR] opt verification failed: %v\n", verr)
			}
			return "", fmt.Sprintf("opt-verify: %v", verr)
		}
	}
	return ll, ""
}

// applyOptionInlineThreshold 把配置的 `?T` 載荷內聯閾值交給 MIR。0（未配置）
// 交給 MIR 自己取預設值 24，讓 MIR 單獨使用時也有正確行為。
func (t *Transpiler) applyOptionInlineThreshold(mod *mir.Module) {
	if mod == nil || t.optionInlineThreshold == 0 {
		return
	}
	mod.OptionInlineThreshold = t.optionInlineThreshold
}

// firstFatalLowerDiag returns the first diagnostic that represents a construct
// the MIR backend cannot emit correctly, together with true. Benign lower gaps
// (kind "ident"/"dot") are intentionally NOT fatal — they emit a best-effort
// form that works for the v1 subset.
func firstFatalLowerDiag(diags []mir.LowerDiag) (mir.LowerDiag, bool) {
	for _, d := range diags {
		// "interp": a named-format shape the backend would emit unsubstituted.
		// "deprecated": a call to a removed builtin (printf / eprintf) that got
		//   past the checker — the call target does not exist, so the build must
		//   stop instead of emitting it.
		// "map-index": a map subscript read/write the lowerer could not turn into
		//   a get/put (no specialised hashmap method, unresolvable ?V, or a `?V`
		//   value fed to put's `v`). Every one of those DROPS the operation: the
		//   read yields nothing, the write silently does not happen. A silent
		//   no-op map write is exactly the kind of wrong-but-exit-0 outcome this
		//   list exists to prevent, so it is fatal.
		if d.Kind == "interp" || d.Kind == "deprecated" || d.Kind == "map-index" {
			return d, true
		}
	}
	return mir.LowerDiag{}, false
}

// lowerToIR 將已檢查的 AST（merged）降級為 LLVM IR。The checked AST is lowered
// to HIR; the MIR backend consumes the HIR package (restoring inferred types
// from the side-table). This is the ONLY codegen path. The legacy surface-AST
// generator (selected with NOLANG_HIR=0) and the legacy HIR-based generator
// both lived in src/build/llvm/ and were removed once the full-corpus gate was
// clean (docs/MIR_DESIGN.md §13.3.13).
//
// targetGoos/targetGoarch are honoured in two places: the merge pass
// (checker.MatchesTargetPlatform at the std-module import sites) and the MIR
// backend (mir.SetTargetPlatform, set from SetTargetPlatform), which filters
// MAIN-file #{platform} variants and picks target-specific C symbols. The
// backend still falls back to the host when the triple did not resolve to a
// complete (goos, goarch) pair — see src/mir/platform.go.
func (t *Transpiler) lowerToIR(merged *parser.Program) (string, error) {
	var ir string
	{
		if os.Getenv("NOLANG_DEBUG_SELF") != "" {
			f, _ := os.Create("/tmp/merged_dump.txt")
			for i, st := range merged.Statements {
				nm := ""
				blen := -1
				var bfirst string
				switch v := st.(type) {
				case *parser.FunctionDefinition:
					nm = v.Name
					if v.Body != nil {
						blen = len(v.Body.Statements)
						if blen > 0 {
							bfirst = fmt.Sprintf("%T", v.Body.Statements[0])
						}
					}
				case *parser.LetStatement:
					if v.Name != nil {
						nm = v.Name.Value
					}
				}
				fmt.Fprintf(f, "MERGED[%d] %T name=%q bodyLen=%d firstBody=%q\n", i, st, nm, blen, bfirst)
			}
			f.Close()
		}
		hirPkg, idMap := parser.ASTToHIRWithMap(merged)
		// H0-side desugaring can emit ERRORS (buildMatchDesugar's "empty match arm
		// cannot determine a value"). They have nowhere else to go — the *Parser
		// that produced the AST is long gone by this point — so they are folded
		// into Program.Errors by ASTToHIRWithMap, and checked here. Skipping this
		// check is what made the target pipeline (ParseProgram(SkipLowering) →
		// ASTToHIR) silently compile a program whose match arms have no value,
		// while the shipped path failed the same input: see the Program.Errors
		// block comment in src/parser/ast.go.
		if len(merged.Errors) > 0 {
			return "", fmt.Errorf("%s: %s", t.sourcePath, strings.Join(merged.Errors, "\n"))
		}
		parser.PopulateInferredTypes(hirPkg, idMap, merged.Sem)
		// Stage 6C 影子双跑挂载点（docs/design/nolang-parser-lowering-invariants.md
		// §3.5）：对 14 对已迁移 THIR 轨检查做旧版 vs THIR 版诊断多重集对拍，
		// 只记 stderr、不改行为。开关 NOLANG_THIR_SHADOW=1。6D 切换后正式
		// THIR 轨入口已切至 RunAllLints / 硬错误块（BuildTHIRView）；本挂载
		// 保留为回归对拍——在同一 merged program 上再建一次 view 做全量比对，
		// 验证 THIR 变体与旧版持续全等。
		if os.Getenv("NOLANG_THIR_SHADOW") != "" {
			view := checker.NewTHIRView(hirPkg, idMap)
			for _, m := range checker.ShadowCompareTHIR(merged, view) {
				fmt.Fprintf(os.Stderr, "[thir-shadow] %s\n", m)
			}
		}
		if os.Getenv("NOLANG_DEBUG_SELF") != "" {
			for _, id := range hirPkg.Top {
				n := hirPkg.Node(id)
				if n == nil {
					continue
				}
				if n.Kind == hir.KLet || n.Kind == hir.KFuncDef {
					fmt.Fprintf(os.Stderr, "[debug-self] pkg.Top[%d] kind=%v name=%q\n", id, n.Kind, hirPkg.Str(n.S))
				}
			}
		}
		// MIR is the only codegen path. The strangler-fig machinery that used to
		// live here is gone (docs/MIR_DESIGN.md §13.3.13): there is no NOLANG_MIR=2
		// fallback to a legacy HIR generator, no NOLANG_MIR=0 legacy backend, and
		// no NOLANG_HIR=0 surface-AST generator left to degrade to. A construct the
		// MIR backend cannot handle now fails the build loudly instead of silently
		// producing output from a second implementation.
		//
		// NOLANG_MIR=1 is retained as a pure DIAGNOSTIC: it dumps the lowered
		// module + memory report to a temp file and then emits MIR exactly as
		// normal. (It used to dump and then hand back to legacy, which no longer
		// exists.) 0 and 2 are rejected rather than silently ignored so an old
		// invocation cannot be mistaken for a working legacy run.
		mirMode := os.Getenv("NOLANG_MIR")
		if mirMode == "0" || mirMode == "2" {
			return "", i18n.Errorf("cb4biu8s", "NOLANG_MIR=%s: the legacy codegen backend was removed and MIR is the only backend; unset NOLANG_MIR (or use NOLANG_MIR=1 for the lowering dump)", mirMode)
		}
		if mirMode == "1" {
			func() {
				defer func() { recover() }()
				mod, rep, ldiags := mir.LowerHIR(hirPkg, checker.CollectStdEnumVariants())
				t.applyOptionInlineThreshold(mod)
				if mod != nil {
					if f, err := os.CreateTemp("", "nolang-mir-*.txt"); err == nil {
						fmt.Fprintf(f, "=== NOLANG_MIR verification for %s ===\n", t.sourcePath)
						fmt.Fprintf(f, "--- lowered MIR ---\n%s\n", mod.String())
						fmt.Fprintf(f, "--- memory analysis ---\n")
						fmt.Fprintf(f, "drops inserted: %d\n", rep.DropsInserted)
						for _, d := range rep.Diagnostics {
							fmt.Fprintf(f, "[mem-diag] %s: %s (func=%s)\n", d.Kind, d.Msg, d.Func)
						}
						for _, d := range ldiags {
							fmt.Fprintf(f, "[lower-gap] %s/%s: %s\n", d.Func, d.Kind, d.Msg)
						}
						f.Close()
					}
				}
			}()
		}
		mout, mreason := t.emitMIR(hirPkg)
		if mout == "" {
			return "", i18n.Errorf("s28lrleh", "MIR codegen could not handle %s: %s", t.sourcePath, mreason)
		}
		ir = mout
	}
	return ir, nil
}
