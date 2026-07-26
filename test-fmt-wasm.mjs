// Test no.wasm with rand example
import {init, WASI, MemFS} from './docs/node_modules/@wasmer/wasi/dist/Library.esm.min.js';
import {readFile} from 'fs/promises';

await init();

const NO_WASM = './docs/static/wasm/no.wasm';
const INPUT_PATH = '/tmp/input.no';
const OUTPUT_PATH = '/tmp/out.wasm';
const TMP_DIR = '/tmp';

async function runSource(label, source) {
  console.log(`\n=== ${label} ===`);
  const wasmBytes = await readFile(NO_WASM);
  const noModule = await WebAssembly.compile(wasmBytes);
  const fs = new MemFS();
  try { fs.createDir(TMP_DIR); } catch {}
  const f = fs.open(INPUT_PATH, {read: true, write: true, create: true});
  f.writeString(source);
  f.flush();
  const wasi = new WASI({
    args: ['no', 'build', '--wasm-direct', '-target', 'wasm32-wasi', '-o', OUTPUT_PATH, INPUT_PATH],
    env: {},
    preopens: {'/tmp': '/tmp'},
    fs,
  });
  let userWasmBytes;
  try {
    wasi.instantiate(noModule, {});
    wasi.start();
    const stderr = wasi.getStderrString();
    if (stderr) console.log('compile stderr:', stderr.trim());
    const outFile = wasi.fs.open(OUTPUT_PATH, {read: true});
    outFile.seek(0);
    userWasmBytes = outFile.read();
  } catch (e) {
    console.log('compile error:', e.message || String(e));
    return;
  }
  const execFs = new MemFS();
  try { execFs.createDir(TMP_DIR); } catch {}
  const execWasi = new WASI({
    args: ['out.wasm'],
    env: {},
    preopens: {'/tmp': '/tmp'},
    fs: execFs,
  });
  try {
    const moduleBytes = new Uint8Array(userWasmBytes.length);
    moduleBytes.set(userWasmBytes);
    const userModule = new WebAssembly.Module(moduleBytes);
    execWasi.instantiate(userModule, {});
    execWasi.start();
    const stdout = execWasi.getStdoutString();
    const stderr = execWasi.getStderrString();
    if (stdout) console.log('stdout:\n' + stdout);
    if (stderr) console.log('stderr:', stderr.trim());
  } catch (e) {
    console.log('execute error:', e.message || String(e));
  }
}

await runSource('Variables & Functions', `; Variables & Functions — declaration, types, calls

; Type inference (i64 is the default integer type)
i = 42
f = 3.14
s = 'hello'
flag = true

; Explicit type annotation
n u64 = 100
arr [3]i64 = [1, 2, 3]
v []u8 = [10, 20, 30]

; Function definition with named result parameter
add = (a i64, b i64) (result i64) {
  result = a + b
}

; Function call — result binds to LHS
sum = add(3, 4)
print('3 + 4 = ' - sum.to-str())

; String concatenation with '-'
greeting = 'Hello, ' - s
print(greeting)

; Array element access
print('arr[0] = ' - arr[0].to-str())
print('v[1]   = ' - v[1].to-str())

; Multi-return function
swap = (a i64, b i64) (x i64, y i64) {
  x = b
  y = a
}
a, b = swap(5, 9)
print('swap(5, 9) = ' - a.to-str() - ', ' - b.to-str())
`);
