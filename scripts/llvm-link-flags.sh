#!/usr/bin/env bash
# Usage: llvm-link-flags.sh <llvm-config-path> <component> [component ...]
#
# Runs `llvm-config --ldflags --libs --link-static --system-libs <components>` and
# prints the result, EXCEPT it drops any `-lPolly*` library whose static/shared
# object is not actually present in `$(llvm-config --libdir)`.
#
# Why: some distro LLVM packages (e.g. Linux arm64) emit `-lPolly` / `-lPollyISL`
# from the `passes` component but do NOT ship the corresponding static libs, so
# the static `no-llvm` link fails with "cannot find -lPolly". Polly is an optional
# polyhedral optimizer; nolang only uses PassBuilder / LLVMRunPasses standard
# passes, so dropping it is safe. Platforms where libPolly.a/.so/.dylib DO exist
# keep the flag unchanged (no behaviour change for macOS / x86_64 Linux).

set -u
LLVM_CONFIG="$1"
shift

LIBDIR="$("$LLVM_CONFIG" --libdir 2>/dev/null || true)"

"$LLVM_CONFIG" --ldflags --libs --link-static --system-libs "$@" 2>/dev/null \
  | tr ' ' '\n' \
  | while IFS= read -r t; do
      case "$t" in
        -lPolly*)
          lib="${t#-l}"
          if [ -z "$LIBDIR" ] \
             || [ -e "$LIBDIR/lib$lib.a" ] \
             || [ -e "$LIBDIR/lib$lib.so" ] \
             || [ -e "$LIBDIR/lib$lib.dylib" ]; then
            printf '%s ' "$t"
          fi
          ;;
        *)
          printf '%s ' "$t"
          ;;
      esac
    done
