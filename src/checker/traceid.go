package checker

// TraceID placeholder and stamping mechanism.
//
// Every ValidateResult literal in the checker source code carries
//   TraceID: "PLACEHOLDER"
// The Makefile target `stamp-traceid` replaces each occurrence of
// "PLACEHOLDER" with a unique 8-char random base36 string (e.g.
// "k2f8b1x9").  Repeated `make` does NOT re-stamp — once a real
// ID is in place it is left untouched.
//
// This means every error message from a specific ValidateResult
// site carries a stable, unique identifier baked into the binary.
// Even if line numbers are wrong (due to macro expansion, module
// merging, or formatter reflow), the trace ID pinpoints exactly
// which ValidateResult produced the error.
