# WebAssembly (WASI) Build

hledger-lsp also ships as a WebAssembly module for WASI preview 1
(`GOOS=wasip1 GOARCH=wasm`). The module is the same language server: it speaks
LSP over stdin and stdout and reads journals through the files its host lets it
open. The native binaries remain the recommended choice for desktop editors;
the WASI module is slower, larger in memory and depends on the host runtime.

The module is not a browser extension. A browser has no WASI runtime, no
stdio and no file system; see [Browser editors](#browser-editors-vs-code-for-the-web)
for what an integration would need.

## Tested runtimes

| Runtime | Version tested | Result |
| --- | --- | --- |
| [wasmtime](https://wasmtime.dev) | 50.0.1, Linux x86_64 | Works |
| Node.js `node:wasi` | 26.12.0 (uvwasi 0.0.23), Linux x86_64 | Works; Node marks `node:wasi` experimental |

Both runtimes run the same automated session in CI and before every release
(`TestWASISmoke` in `cmd/hledger-lsp/wasi_smoke_test.go`). The session opens a
four-file journal with an explicit `include` and a glob `include`, Cyrillic and
CJK account names, and CRLF line endings. It checks initialization,
diagnostics, completion, formatting, go to definition, references, document
links, workspace symbols, an unknown method, `shutdown`/`exit` and the exit
code when stdin closes without `shutdown`. Every answer must match the native
build byte for byte, except the operating system's wording in file errors.
The test also requires stdout to carry nothing but well-formed LSP frames and
stderr to stay empty.

Other WASI runtimes were not tested.

## Running

Download `hledger-lsp_wasip1_wasm.wasm` from
[GitHub Releases](https://github.com/juev/hledger-lsp/releases). It is listed in
`checksums.txt` and has a `.minisig` signature like the native binaries.

The server can only open files in directories the host preopens. Preopen the
journal directory at the same path inside the module, so the `file://` URIs the
editor sends resolve unchanged:

```bash
wasmtime run --dir /home/me/finance::/home/me/finance hledger-lsp_wasip1_wasm.wasm
```

Use this command line wherever an editor expects the server command, for
example in Neovim:

```lua
vim.lsp.config("hledger_lsp", {
  cmd = { "wasmtime", "run", "--dir", "/home/me/finance::/home/me/finance",
          "/path/to/hledger-lsp_wasip1_wasm.wasm" },
  filetypes = { "hledger", "journal" },
})
```

Under Node, `cmd/hledger-lsp/testdata/wasi/node-wasi.mjs` is the runner the
tests use; it preopens each directory argument at the same path:

```bash
node --no-warnings node-wasi.mjs hledger-lsp_wasip1_wasm.wasm /home/me/finance
```

wasmtime compiles the module on first start and caches the result (by default
under `$XDG_CACHE_HOME/wasmtime`); later starts load the cache.

## Limitations

- **Only preopened directories are readable.** An `include` that leaves them
  is reported as `cannot read included file: stat …: Bad file number` (wasmtime
  and Node give the same answer). Preopen every directory your includes reach.
- **No hledger CLI.** WASI has no child processes, so the `hledger.run`
  command fails with "hledger-lsp: hledger is not available at "hledger"". All
  other features work without the CLI.
- **No environment.** The runners above pass no environment variables, so an
  `include ~/…` path has no home directory to expand. This case was not tested.
- **File error wording** comes from WASI: `No such file or directory` instead of
  Linux's `no such file or directory`.
- **Performance.** See the measurements below: analysis runs several times
  slower than native and uses about three times the memory.
- **Idle CPU.** See [How stdin is read](#how-stdin-is-read).

## How stdin is read

A Go program under WASI runs on one thread. A read from stdin with no data
blocks the whole program, so no timer fires until the editor sends the next
message, and diagnostics, which the server publishes after a 100 ms debounce
timer, would wait for the next keystroke. Neither runtime lets Go switch stdin
to non-blocking mode (wasmtime answers `EBADF`), and under Node a plain read
answers `EAGAIN` and stops the server after `initialize`.

The WASI build therefore asks the host with `poll_oneoff` whether stdin has
data, waiting at most 1 ms, and sleeps between checks so timers and other
goroutines run (`internal/stdin`). The sleep doubles from 1 ms to 25 ms while
idle and resets when a message arrives. This adds up to about 25 ms before the
first message after a pause is read and costs CPU while idle:

| Idle ceiling | wasmtime | Node |
| --- | --- | --- |
| 10 ms | 1.6% | 4.4% |
| 25 ms (used) | 0.8% | 2.1% |
| 50 ms | 0.4% | 1.1% |

_Percent of one core, idle server after `initialize`, 10 s sample, measured
2026-11-02 on an Intel Xeon E3-1245 v6 (Linux)._

A host must report stdin readiness through `poll_oneoff` (an `fd_read` event
only when stdin is readable or closed), or answer `EAGAIN` from a read with no
data. A host that does neither still works for requests, but diagnostics are
published only when the next message arrives.

Node's `poll_oneoff` does not report stdin when the clock timeout is zero; it
does from 1 ms up, which is why the check waits 1 ms.

## Measurements

_Measured 2026-11-02 at `d0afa1f` plus this change, Go 1.27, wasmtime 50.0.1,
Node 26.12.0, on an Intel Xeon E3-1245 v6 (8 threads, Linux) that was shared
with other jobs (load average 11–18), so latencies are noisy; compare builds
within one table rather than across tables._

Module size (`-ldflags "-s -w"`, as released): 17,806,809 bytes, 3,556,306
bytes gzip -9. The native linux/amd64 binary built the same way is 8,704,160
bytes.

Command:

```bash
GOOS=wasip1 GOARCH=wasm go build -ldflags '-s -w' -o hledger-lsp.wasm ./cmd/hledger-lsp
HLEDGER_WASI_WASM=$PWD/hledger-lsp.wasm HLEDGER_WASI_MEASURE=native,wasmtime,node \
  go test ./cmd/hledger-lsp -run WASIMeasure -v -count=1
```

The journals are the smoke-test fixture (`cmd/hledger-lsp/testdata/wasi/workspace`)
and `testutil.GenerateIncludeTree(dir, 10, 1000)`: a `main.journal` including
10 files of 1000 transactions each. "Startup" is process start to the
`initialize` answer; "first diagnostics" and "edit to diagnostics" include the
100 ms debounce; peak RSS is the runtime process's `VmHWM`; idle CPU is
sampled over 3 s.

First run, empty wasmtime cache (wasmtime startup includes compiling the module):

| journal | build | startup | first diagnostics | completion p50 / max | formatting p50 | edit to diagnostics p50 | peak RSS | idle CPU |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| fixture (4 files) | native | 25.3 ms | 107.1 ms | 0.8 ms / 40.9 ms | 0.4 ms | 104.6 ms | 11 MiB | 0.0% |
| fixture (4 files) | wasmtime | 23054.3 ms | 173.0 ms | 5.1 ms / 59.1 ms | 3.3 ms | 140.7 ms | 396 MiB | 1.0% |
| fixture (4 files) | node | 1874.1 ms | 331.0 ms | 10.8 ms / 101.2 ms | 4.7 ms | 119.4 ms | 122 MiB | 2.3% |
| 10 files x 1000 tx | native | 81.1 ms | 1299.5 ms | 777.2 ms / 1792.6 ms | 1.2 ms | 275.2 ms | 105 MiB | 0.3% |
| 10 files x 1000 tx | wasmtime | 1367.0 ms | 4421.3 ms | 2314.9 ms / 4355.3 ms | 1.1 ms | 1011.4 ms | 242 MiB | 1.0% |
| 10 files x 1000 tx | node | 733.1 ms | 4221.2 ms | 2811.8 ms / 5817.3 ms | 4.5 ms | 1601.4 ms | 284 MiB | 3.0% |

Second run, wasmtime cache filled:

| journal | build | startup | first diagnostics | completion p50 / max | formatting p50 | edit to diagnostics p50 | peak RSS | idle CPU |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| fixture (4 files) | native | 6.3 ms | 103.1 ms | 0.4 ms / 50.2 ms | 0.3 ms | 107.1 ms | 11 MiB | 0.0% |
| fixture (4 files) | wasmtime | 645.8 ms | 105.9 ms | 4.1 ms / 50.0 ms | 1.0 ms | 129.3 ms | 99 MiB | 0.7% |
| fixture (4 files) | node | 2131.7 ms | 800.2 ms | 5.9 ms / 75.5 ms | 1.2 ms | 221.6 ms | 123 MiB | 3.7% |
| 10 files x 1000 tx | native | 78.6 ms | 1672.5 ms | 549.9 ms / 1231.9 ms | 0.2 ms | 262.9 ms | 102 MiB | 0.0% |
| 10 files x 1000 tx | wasmtime | 840.8 ms | 5586.7 ms | 4346.7 ms / 6289.2 ms | 1.2 ms | 1015.8 ms | 317 MiB | 1.0% |
| 10 files x 1000 tx | node | 520.0 ms | 4558.9 ms | 2309.0 ms / 4256.1 ms | 1.2 ms | 907.6 ms | 282 MiB | 2.0% |

On the large journal the WASI build needs roughly 3–5 times the native time for
analysis-heavy requests and about three times the memory. Formatting stays
around a millisecond.

## Browser editors (VS Code for the Web)

The WASI module is a building block, not browser support. vscode.dev and
github.dev run extensions in a web worker without processes or a local file
system, so the hledger-vscode extension would need:

1. **A web extension entry point** (`browser` in `package.json`) that bundles or
   downloads the `.wasm` module.
2. **A WASI host in the worker.** Microsoft's `@vscode/wasm-wasi` and
   `@vscode/wasm-wasi-lsp` (pre-release, 0.1.0-pre.9 at the time of writing)
   provide one: `createStdioOptions` connects stdin/stdout as pipes and
   `startServer` turns them into an LSP transport for `vscode-languageclient`.
3. **File system mapping.** The server resolves includes only for `file://`
   URIs. Browser workspaces use other schemes (for example `vscode-vfs://`).
   `createUriConverters` in `@vscode/wasm-wasi-lsp` maps a workspace folder to
   `file:///workspace` (or `file:///workspace/<name>` for several folders); the
   extension must mount the same folder at that path in the WASI host.
4. **stdin readiness.** Reading the `@vscode/wasm-wasi-core` source suggests its
   `poll_oneoff` returns an `fd_read` event even when no bytes are available
   (`nbytes` 0). If so, the server falls back to blocking reads there and
   publishes diagnostics late. This was not run; verifying it, and either
   fixing the host or teaching `internal/stdin` to treat a zero-byte event as
   not ready, is the first follow-up.
5. **Feature gaps** listed under [Limitations](#limitations): no hledger CLI
   commands, only mounted folders are readable.

None of this was built or run in a browser. Track the extension work in the
hledger-vscode repository.
