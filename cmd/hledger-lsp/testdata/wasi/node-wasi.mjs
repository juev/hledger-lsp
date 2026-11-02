// Runs a WASI preview 1 module under Node's built-in node:wasi with the
// process stdio as the module's stdio, so an editor or test can talk LSP to it.
//
//   node --no-warnings node-wasi.mjs hledger-lsp.wasm DIR...
//
// Each DIR is preopened at the same path inside the module, so file:// URIs
// from the editor resolve unchanged. The module's exit code becomes ours.
import { readFile } from 'node:fs/promises';
import { WASI } from 'node:wasi';

const [wasmPath, ...dirs] = process.argv.slice(2);
if (!wasmPath) {
  console.error('usage: node node-wasi.mjs MODULE.wasm [DIR...]');
  process.exit(2);
}

const wasi = new WASI({
  version: 'preview1',
  args: ['hledger-lsp'],
  env: {},
  preopens: Object.fromEntries(dirs.map((dir) => [dir, dir])),
  returnOnExit: true,
});
const module = await WebAssembly.compile(await readFile(wasmPath));
const instance = await WebAssembly.instantiate(module, wasi.getImportObject());
process.exitCode = wasi.start(instance);
