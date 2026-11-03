# CLI CSV import — continuation notes for #169

## Status

Work in progress, **not ready to merge**. This replaces the earlier pure-Go-importer approach with the user-approved configured `hledger` executable. The older experimental branches are retained; they are not prerequisites or approved implementations.

Implementation before this handoff: `7034e53c0c3ec0c10b617b541a9b6f4076edd92f`, based on main `e612554ae52fd85e27497101496e8fe82e39848e`. Independent review of this implementation has **not** been completed. Do not infer completion of #169 or #85 from passing tests.

## Implemented

- `hledger.targetVersion`: `"1"` by default, `"2"`, or `"auto"`. CSV inclusion is disabled under target 1. The backend checks actual CLI availability/version and currently requires a version at least 1.99.5 for CSV inclusion.
- Structured-argv bounded CLI execution; no shell, `--no-conf`, no state-changing import/add/new mode, timeout/output limits and at most two concurrent conversion processes. WASI has an explicit no-subprocess path.
- Import conversion combines `print -O json` exact mantissa/decimal-place values with ordinary `print` structure to distinguish written versus inferred amounts, costs and assignments. Input/output agreement is checked; unsupported representations fail explicitly.
- CSV/rules dependency planning, input-identity cache and gated regression for sharing results only after input revalidation.
- Include/workspace integration and imported-source edit protection, including explicit reader prefixes, failed/empty imports and physical-file aliases.
- Real stable/preview binary integration fixtures, assertion/assignment tests and fault-injection tests. Source inputs must remain unchanged and must not acquire generated rules/latest files.

## Required follow-up before merge

1. **Dependency watching:** dynamic watchers do not yet cover arbitrary-extension inputs such as `csv:data.txt`; they refresh only when the importing journal is reloaded.
2. **Request cancellation and locking:** the production loader is not wired to the LSP request's cancellation. Workspace rebuild currently runs conversion synchronously while holding its workspace lock, potentially until `cli.timeout`. Fix without publishing stale computations or blocking unrelated requests.
3. **Encoded source coordinates:** ranges currently use raw newline-byte positions; non-UTF-8/UTF-16 input can produce incorrect diagnostic coordinates. Decode and map correctly, or explicitly refuse representations whose coordinates cannot be supported rather than claim accurate provenance.
4. **WASI runtime verification:** build/vet passed, but the actual unsupported-import diagnostic has not been exercised under a WASI runtime.
5. **Independent review:** financial semantics, subprocess/input safety, dependency invalidation, cache publication, role protection and the final combined tree still require review. The deliberately restricted subset below is not approval to omit requested features.
6. **Integration:** main may advance after `e612554`; merge it normally, preserve other settings fixes and rerun affected tests. Do not force-push or discard the retained experimental branches.

## Current explicit restrictions and caveats

- Source commands are refused. Bare/tilde source paths and glob/tilde rules includes are refused; path-search semantics are not reproduced.
- Rules expansion limits are 1 MiB, 256 include edges and depth 16. Raw numeric guards refuse digit runs longer than 255 and exponents over 99, potentially rejecting such text even inside descriptions.
- Imported lot cost bases and explicit multi-commodity posting amounts are refused. `rules:` is not treated as a supported 1.99.5 reader prefix.
- Unsaved CSV/rules buffers produce `INCLUDE_IMPORT_UNSAVED` warnings; conversion still uses the saved file. Decide whether this policy meets the required editor contract before merging.
- Regex aliases at the include site are not applied. Some read-only journal-style features still answer for source buffers, though edit-producing features are guarded.
- Version threshold is conservative: capabilities of 1.99.0–1.99.4 were not established.

## Verification to resume locally

Use real `hledger` binaries, not fake commands, for semantic acceptance. Reference runs used stable 1.52.4 and preview 1.99.5; choose the matching binary on `PATH` and record its version. Fake binaries cover only failure/control-flow injection.

```sh
hledger --version
HLEDGER_ORACLE=1 go test -p 2 -parallel 2 -count=1 ./internal/csvimport ./internal/include ./internal/server
HLEDGER_ORACLE=1 go test -p 2 -parallel 2 -count=1 -run PreviewOracle ./...
go test -p 2 -parallel 2 -race -timeout 20m ./...
go vet -p 2 ./...
golangci-lint run --allow-serial-runners --concurrency 2 ./...
GOOS=wasip1 GOARCH=wasm go build -p 2 ./...
GOOS=wasip1 GOARCH=wasm go vet -p 2 ./...
```

These are test selections, not a statement that every selected test executes under one binary version. Inspect skips. Full oracle suites can take substantially longer than ordinary tests. On a shared runner use its build queue and keep worker limits; never exercise giant regex/numeric/resource-exhaustion inputs against the reference binary. Verify quantities, commodity identities, counts, ordering, assertions and source coordinates, not just matching exit codes.

No release or tag is part of this work.
