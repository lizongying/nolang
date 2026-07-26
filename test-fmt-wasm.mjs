// Test no.wasm with rand example
import {init, WASI, MemFS} from './docs/node_modules/@wasmer/wasi/dist/Library.esm.min.js';
import {readFile} from 'fs/promises';

await init();

const NO_WASM = './docs/static/wasm/no.wasm';
const INPUT_PATH = '/tmp/input.no';
const OUTPUT_PATH = '/tmp/out.wasm';
const TMP_DIR = '/tmp';
const SOURCE = `score = 85
grade = score: {
  [0..60) -> 'F'
  [80..90) -> 'B'
  -> 'invalid'
}
print('grade=' - grade)
`;

const wasmBytes = await readFile(NO_WASM);
const noModule = await WebAssembly.compile(wasmBytes);

console.log('=== Stage 1: Compile ===');
const fs = new MemFS();
try { fs.createDir(TMP_DIR); } catch {}
const f = fs.open(INPUT_PATH, {read: true, write: true, create: true});
f.writeString(SOURCE);
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
  console.log('output size:', userWasmBytes.length, 'bytes');
} catch (e) {
  console.log('compile error:', e.message || String(e));
  process.exit(1);
}

console.log('\n=== Stage 2: Execute user wasm ===');
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
  if (stdout) console.log('stdout:', stdout);
  if (stderr) console.log('stderr:', stderr.trim());
} catch (e) {
  console.log('execute error:', e.message || String(e));
}

console.log('\n=== Done ===');
