//go:build !wasm

package wasm

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

func TestRawVFSImportsAreLazy(t *testing.T) {
	mod := parseAndGenerate(t, "print('plain')")
	imports := parseImportSection(t, mod)
	if len(imports) != 2 {
		t.Fatalf("non-raw import count = %d, want 2", len(imports))
	}
	if imports[0].module != "wasi_snapshot_preview1" || imports[0].name != "fd_write" ||
		imports[1].module != "wasi_snapshot_preview1" || imports[1].name != "proc_exit" {
		t.Fatalf("non-raw imports = %#v", imports)
	}
}

func TestRawVFSImportSignatures(t *testing.T) {
	const src = `fd fs.fd = 7
buf []byte = [1]
opened = raw.raw-open('x', raw.RAW-READ, 0)
closed = raw.raw-close(fd)
position = raw.raw-seek(fd, 0, raw.RAW-SEEK-SET)
read = raw.raw-pread(fd, buf, 1, 0)
written = raw.raw-pwrite(fd, buf, 1, 0)
truncated = raw.raw-ftruncate(fd, 0)
size = raw.raw-fstat-size(fd)
synced = raw.raw-fsync(fd)
data-synced = raw.raw-fdatasync(fd)
dir-synced = raw.raw-sync-dir('.')
locked = raw.raw-lock-range(fd, 0, 1, raw.RAW-LOCK-EXCLUSIVE, true)
unlocked = raw.raw-unlock-range(fd, 0, 1)
caps = raw.raw-capabilities()`
	mod := parseAndGenerate(t, src)
	imports := parseImportSection(t, mod)
	types := parseTypeSection(t, mod)

	want := []struct {
		name   string
		params []byte
		result []byte
	}{
		{"raw_open", []byte{byte(I32), byte(I32), byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_close", []byte{byte(I64)}, []byte{byte(I64)}},
		{"raw_seek", []byte{byte(I64), byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_pread", []byte{byte(I64), byte(I32), byte(I32), byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_pwrite", []byte{byte(I64), byte(I32), byte(I32), byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_ftruncate", []byte{byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_fstat_size", []byte{byte(I64)}, []byte{byte(I64)}},
		{"raw_fsync", []byte{byte(I64)}, []byte{byte(I64)}},
		{"raw_fdatasync", []byte{byte(I64)}, []byte{byte(I64)}},
		{"raw_sync_dir", []byte{byte(I32), byte(I32)}, []byte{byte(I64)}},
		{"raw_lock_range", []byte{byte(I64), byte(I64), byte(I64), byte(I64), byte(I32)}, []byte{byte(I64)}},
		{"raw_unlock_range", []byte{byte(I64), byte(I64), byte(I64)}, []byte{byte(I64)}},
		{"raw_capabilities", nil, []byte{byte(I64)}},
	}
	if len(imports) != 2+len(want) {
		t.Fatalf("import count = %d, want %d", len(imports), 2+len(want))
	}
	for i, expected := range want {
		imp := imports[i+2]
		if imp.module != "nolang_raw" || imp.name != expected.name || imp.kind != byte(FuncImport) {
			t.Fatalf("import[%d] = %#v, want nolang_raw.%s", i+2, imp, expected.name)
		}
		ft := types[imp.typeIndex]
		if !bytes.Equal(ft.params, expected.params) || !bytes.Equal(ft.results, expected.result) {
			t.Errorf("%s type = (%x)->(%x), want (%x)->(%x)", expected.name,
				ft.params, ft.results, expected.params, expected.result)
		}
	}
	start, ok := findExport(parseExportSection(t, mod), "_start")
	if !ok || start.index != uint32(2+len(want)) {
		t.Fatalf("_start = %#v, want index %d", start, 2+len(want))
	}
}

func TestRawVFSFdUsesI64(t *testing.T) {
	if got := ValTypeFromName(&parser.NamedType{Value: "fs.fd"}); got != I64 {
		t.Fatalf("ValTypeFromName(fs.fd) = %v, want I64", got)
	}
}

func TestRawVFSOptionProvenance(t *testing.T) {
	const src = `opened ?fs.fd = raw.raw-open('data.db', raw.RAW-READ, 0)
opened: {
    ok -> print(it)

    nil -> print(-1)

    err -> print(-2)
}`
	g := generateForSignatureTest(t, src)
	if got := g.localKindMap["_start"]["opened"]; got != KindRawOption {
		t.Fatalf("opened kind = %v, want KindRawOption", got)
	}
}

func TestRawVFSNodeHostRoundTrip(t *testing.T) {
	const src = `print(raw.raw-capabilities())
print(raw.RAW-READ | raw.RAW-WRITE | raw.RAW-CREATE | raw.RAW-EXCLUSIVE | raw.RAW-TRUNCATE)
print(raw.RAW-SEEK-SET)
print(raw.RAW-SEEK-CUR)
print(raw.RAW-SEEK-END)
print(raw.RAW-LOCK-SHARED)
print(raw.RAW-LOCK-EXCLUSIVE)
print(raw.RAW-CAP-POSITIONAL-IO | raw.RAW-CAP-TRUNCATE | raw.RAW-CAP-FILE-SYNC | raw.RAW-CAP-DATA-SYNC | raw.RAW-CAP-DIR-SYNC | raw.RAW-CAP-RANGE-LOCK | raw.RAW-CAP-SHARED-LOCK | raw.RAW-CAP-NONBLOCKING-LOCK)
opened ?fs.fd = raw.raw-open('data.db', raw.RAW-READ | raw.RAW-WRITE | raw.RAW-CREATE | raw.RAW-TRUNCATE, 420)
opened: {
    ok -> {
        fd fs.fd = it
        print(fd)
        position ?i64 = raw.raw-seek(fd, 0, raw.RAW-SEEK-SET)
        position: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        data []byte = [65, 66, 67]
        written ?i64 = raw.raw-pwrite(fd, data, 3, 0)
        written: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        buf []byte = with-len(3)
        read ?i64 = raw.raw-pread(fd, buf, 3, 0)
        read: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        print(buf[0])
        print(buf[1])
        print(buf[2])
        size ?i64 = raw.raw-fstat-size(fd)
        size: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        truncated ?bool = raw.raw-ftruncate(fd, 5)
        truncated: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        size2 ?i64 = raw.raw-fstat-size(fd)
        size2: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        synced ?bool = raw.raw-fsync(fd)
        synced: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        data-synced ?bool = raw.raw-fdatasync(fd)
        data-synced: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        dir-synced ?bool = raw.raw-sync-dir('.')
        dir-synced: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        locked ?bool = raw.raw-lock-range(fd, 0, 4, raw.RAW-LOCK-EXCLUSIVE, true)
        locked: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        unlocked ?bool = raw.raw-unlock-range(fd, 0, 4)
        unlocked: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
        closed ?bool = raw.raw-close(fd)
        closed: {
            ok -> print(it)

            nil -> print(-1)

            err -> print(-2)
        }
    }

    nil -> print(-1)

    err -> print(-2)
}
missing ?fs.fd = raw.raw-open('missing.db', raw.RAW-READ, 0)
missing: {
    ok -> print(0)

    nil -> print(1)

    err -> print(2)
}`
	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	out := runRawVFSNode(t, mod, "supported")
	want := strings.Join([]string{
		"255", "31", "0", "1", "2", "1", "2", "255",
		"10", "0", "3", "3", "65", "66", "67", "3", "true", "5",
		"true", "true", "true", "true", "true", "true", "1", "",
	}, "\n")
	if out != want {
		t.Fatalf("stdout =\n%s\nwant:\n%s", out, want)
	}
}

func TestRawVFSUnsupportedHostReportsFailure(t *testing.T) {
	const src = `print(raw.raw-capabilities())
opened ?fs.fd = raw.raw-open('data.db', raw.RAW-READ, 0)
opened: {
    ok -> print(0)

    nil -> print(1)

    err -> print(2)
}`
	mod := parseAndGenerate(t, src)
	out := runRawVFSNode(t, mod, "unsupported")
	if out != "0\n1\n" {
		t.Fatalf("stdout = %q, want %q", out, "0\n1\n")
	}
}

func runRawVFSNode(t *testing.T, wasm []byte, mode string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(testFile), "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	tmpRoot := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatalf("mkdir tmp root: %v", err)
	}
	dir, err := os.MkdirTemp(tmpRoot, "wasm-raw-")
	if err != nil {
		t.Fatalf("mkdir test scratch: %v", err)
	}
	rel, err := filepath.Rel(tmpRoot, dir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		t.Fatalf("scratch path %q is outside tmp root %q", dir, tmpRoot)
	}
	wasmPath := filepath.Join(dir, "raw.wasm")
	jsPath := filepath.Join(dir, "host.js")
	defer func() {
		if t.Failed() && os.Getenv("NOLANG_KEEP_WASM_RAW_SCRATCH") != "" {
			t.Logf("kept raw VFS scratch: %s", dir)
			return
		}
		_ = os.Remove(wasmPath)
		_ = os.Remove(jsPath)
		_ = os.Remove(dir)
	}()
	if err := os.WriteFile(wasmPath, wasm, 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	if err := os.WriteFile(jsPath, []byte(rawVFSNodeHost), 0o644); err != nil {
		t.Fatalf("write node host: %v", err)
	}
	cmd := exec.Command(node, jsPath, wasmPath, mode)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node host failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

const rawVFSNodeHost = `'use strict';
const fs = require('fs');
const wasm = fs.readFileSync(process.argv[2]);
const mode = process.argv[3];
const unsupported = mode === 'unsupported';
let nextFd = 10;
const files = new Map();
let instance;
let memory;
const view = () => new DataView(memory.buffer);
const text = (ptr, len) => Buffer.from(memory.buffer, ptr, len).toString('utf8');
const fail = -58n;
const raw = {
  raw_capabilities: () => unsupported ? 0n : 255n,
  raw_open: (ptr, len, flags, create) => {
    if (unsupported) return fail;
    const path = text(ptr, len);
    if (path === 'missing.db') return -2n;
    const fd = BigInt(nextFd++);
    files.set(fd, {data: Buffer.alloc(0), pos: 0});
    return fd;
  },
  raw_close: (fd) => {
    if (unsupported || !files.has(fd)) return fail;
    files.delete(fd);
    return 1n;
  },
  raw_seek: (fd, offset, whence) => {
    if (unsupported || !files.has(fd)) return fail;
    const file = files.get(fd);
    let pos;
    if (whence === 0n) pos = offset;
    else if (whence === 1n) pos = BigInt(file.pos) + offset;
    else if (whence === 2n) pos = BigInt(file.data.length) + offset;
    else return -22n;
    if (pos < 0n) return -22n;
    file.pos = Number(pos);
    return pos;
  },
  raw_pread: (fd, ptr, bufLen, count, offset) => {
    if (unsupported || !files.has(fd)) return fail;
    if (count < 0n || offset < 0n || count > BigInt(bufLen)) return -22n;
    const file = files.get(fd);
    const start = Number(offset);
    const n = Math.min(Number(count), Math.max(0, file.data.length - start));
    for (let i = 0; i < n; i++) {
      view().setBigUint64(ptr + i * 8, BigInt(file.data[start + i]), true);
    }
    return BigInt(n);
  },
  raw_pwrite: (fd, ptr, dataLen, count, offset) => {
    if (unsupported || !files.has(fd)) return fail;
    if (count < 0n || offset < 0n || count > BigInt(dataLen)) return -22n;
    const file = files.get(fd);
    const start = Number(offset);
    const n = Number(count);
    if (file.data.length < start + n) {
      const grown = Buffer.alloc(start + n);
      file.data.copy(grown);
      file.data = grown;
    }
    for (let i = 0; i < n; i++) {
      file.data[start + i] = Number(view().getBigUint64(ptr + i * 8, true) & 0xffn);
    }
    return BigInt(n);
  },
  raw_ftruncate: (fd, length) => {
    if (unsupported || !files.has(fd) || length < 0n) return fail;
    const file = files.get(fd);
    const resized = Buffer.alloc(Number(length));
    file.data.copy(resized, 0, 0, Math.min(file.data.length, resized.length));
    file.data = resized;
    return 1n;
  },
  raw_fstat_size: (fd) => unsupported || !files.has(fd) ? fail : BigInt(files.get(fd).data.length),
  raw_fsync: (fd) => !unsupported && files.has(fd) ? 1n : fail,
  raw_fdatasync: (fd) => !unsupported && files.has(fd) ? 1n : fail,
  raw_sync_dir: () => unsupported ? fail : 1n,
  raw_lock_range: (fd) => !unsupported && files.has(fd) ? 1n : fail,
  raw_unlock_range: (fd) => !unsupported && files.has(fd) ? 1n : fail,
};
const wasi = {
  fd_write: (fd, iovs, iovsLen, nwritten) => {
    const dv = view();
    let total = 0;
    for (let i = 0; i < iovsLen; i++) {
      const ptr = dv.getUint32(iovs + i * 8, true);
      const len = dv.getUint32(iovs + i * 8 + 4, true);
      fs.writeSync(fd, Buffer.from(memory.buffer, ptr, len));
      total += len;
    }
    dv.setUint32(nwritten, total, true);
    return 0;
  },
  proc_exit: (code) => {
    if (code !== 0) throw new Error('guest exited ' + code);
    throw {guestExit: true};
  },
};
try {
  const module = new WebAssembly.Module(wasm);
  instance = new WebAssembly.Instance(module, {wasi_snapshot_preview1: wasi, nolang_raw: raw});
  memory = instance.exports.memory;
  instance.exports._start();
} catch (e) {
  if (!e || !e.guestExit) throw e;
}
`
