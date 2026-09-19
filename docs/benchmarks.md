# hledger-lsp Benchmark Results

All numbers below were measured on Apple M4 Pro, macOS, Go 1.27, at commit
`d0ba678`. Every table carries its own date, commit, toolchain and command, so a
stale number is visible as a stale line rather than as an undated claim.

## NFR Targets

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test -tags perf ./internal/benchmark/ -v` (or `make perf`)._

| NFR | Target | Measured | Status |
| --- | --- | --- | --- |
| NFR-1.1 | Completion < 100ms | ~3.3ms | Pass |
| NFR-1.2 | Parsing 10k lines < 500ms | ~11ms | Pass |
| NFR-1.3 | Incremental updates < 50ms | ~4.0ms | Pass |
| NFR-1.4 | Memory < 200MB | ~38MB | Pass |

The NFR tests also assert ratios that do not depend on machine speed:
`TestNFR_DidChangeSubLinear` (10k/1k <= 2x), `TestNFR_PerKeystrokeCycle`
(10k/1k <= 4x) and `TestNFR_KeystrokeBurst`.

## Parser Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/parser/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Lexer_Small | 10 | 4,568 | 68 | 17 |
| Lexer_Medium | 100 | 44,390 | 644 | 161 |
| Lexer_Large | 1,000 | 439,410 | 6,404 | 1,601 |
| Lexer_XLarge | 10,000 | 4,472,446 | 64,004 | 16,001 |
| Parser_Small | 10 | 10,848 | 30,464 | 163 |
| Parser_Medium | 100 | 108,106 | 306,996 | 1,516 |
| Parser_Large | 1,000 | 1,050,651 | 3,033,720 | 15,020 |
| Parser_XLarge | 10,000 | 11,253,811 | 30,481,939 | 150,027 |
| Parser_Parallel | 100 | 57,254 | 306,998 | 1,516 |

The parser still takes a flat `string`. Migrating it to a rope-backed source
was planned and dropped: after the document store began holding the rope, the
CPU profile of the per-keystroke cycle contains no frames from
`internal/document` at all, so there is no materialization left for the
migration to remove while it would add an interface call per byte access.

## Analyzer Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/analyzer/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Analyze_Small | 10 | 21,372 | 43,614 | 419 |
| Analyze_Medium | 100 | 195,144 | 390,470 | 2,967 |
| Analyze_Large | 1,000 | 2,142,632 | 4,546,650 | 27,787 |
| CalculateAccountBalances | 1,000 | 811,284 | 2,018,833 | 24,413 |
| CheckBalance | 1 tx | 684.6 | 2,208 | 25 |
| CheckBalance_MultiCommodity | 1 tx | 623.6 | 1,992 | 23 |
| CollectAccounts | 1,000 | 32,890 | 3,568 | 67 |
| CollectPayees | 1,000 | 61,461 | 143,944 | 31 |
| CollectCommodities | 1,000 | 23,032 | 112 | 3 |
| CollectTags | 1,000 | 8,901 | 16 | 1 |

## Workspace Index Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/workspace/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| BuildFileIndex_Small | 10 | 26,090 | 53,136 | 512 |
| BuildFileIndex_Medium | 100 | 237,696 | 482,502 | 4,000 |
| BuildFileIndex_Large | 1,000 | 2,565,466 | 5,027,334 | 38,468 |
| BuildFileIndex_XLarge | 10,000 | 25,655,620 | 49,688,562 | 382,613 |
| Workspace_MarkFileDirty_Small | 10 | 44.92 | 16 | 1 |
| Workspace_MarkFileDirty_Large | 1,000 | 45.25 | 16 | 1 |
| Workspace_ApplyEdit_Small | 10 | 43,131 | 78,818 | 730 |
| Workspace_ApplyEdit_Medium | 100 | 321,010 | 592,395 | 4,497 |
| Workspace_ApplyEdit_Large | 1,000 | 3,060,306 | 6,115,407 | 41,715 |
| Workspace_IndexSnapshot_Small | 10 | 3,465 | 10,456 | 67 |
| Workspace_IndexSnapshot_Medium | 100 | 17,614 | 65,984 | 249 |
| Workspace_IndexSnapshot_Large | 1,000 | 156,495 | 734,584 | 2,055 |

An edit is now two separate costs. `MarkFileDirty` records the edit and is flat
in journal size — 45ns whether the journal holds 10 transactions or 1,000.
`ApplyEdit` is that edit plus the read that triggers the deferred recompute:
re-parse, index rebuild and include re-resolution. The O(n) work still costs
what it did, and is now paid once per burst of keystrokes rather than once per
keystroke.

## Include Loader Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/include/ -bench=. -benchmem`._

| Benchmark | Files/Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Load_Small | 1 file / 10 tx | 23,725 | 36,296 | 193 |
| Load_Medium | 1 file / 100 tx | 118,831 | 342,923 | 1,549 |
| Load_Large | 1 file / 1000 tx | 1,262,795 | 3,368,031 | 15,057 |
| LoadFromContent_Small | 1 file / 10 tx | 13,743 | 34,440 | 192 |
| LoadFromContent_Large | 1 file / 1000 tx | 1,156,174 | 3,139,083 | 15,056 |
| Load_IncludeTree_5Files | 5 files / 100 tx | 145,321 | 348,760 | 1,753 |
| Load_IncludeTree_10Files | 10 files / 200 tx | 264,338 | 698,987 | 3,461 |
| Load_IncludeTree_20Files | 20 files / 400 tx | 515,942 | 1,391,087 | 6,847 |

`Load_IncludeTree_*` still grows linearly with file count: every load re-parses
every file in the tree. A cache of parsed journals was planned and not taken —
it would re-resolve only the edited file of a multi-file tree, saving 0.6ms at
5 files and 2.6ms at 20, once per debounced burst, in exchange for replaying and
validating recorded resolver answers across cycles, depth limits and the running
context of a glob. What the loader does cache is the context-free parse of a
document version, shared with the server.

## Incremental Update Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/server/ -run '^$' -bench='DidChange|PublishDiagnostics' -benchmem -benchtime=100x`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| DidChange_Incremental_Small | 10 | 1,019 | 861 | 11 |
| DidChange_Incremental_Medium | 100 | 1,060 | 865 | 11 |
| DidChange_Incremental_Large | 1,000 | 1,306 | 869 | 11 |
| PublishDiagnostics_Small | 10 | 117,756 | 234,394 | 1,516 |
| PublishDiagnostics_Medium | 100 | 768,505 | 1,677,796 | 11,371 |
| PublishDiagnostics_Large | 1,000 | 7,288,082 | 16,883,094 | 108,157 |

**Components of incremental update:**

1. `DidChange` (sync): apply the text change to the rope, record the edit, invalidate caches, schedule debounced diagnostics. Constant in document size.
2. `PublishDiagnostics` (debounced, async): materialize the document, re-resolve the include tree, re-index, analyze, publish.

The publish benchmark edits the document between rounds, because a publish is
always scheduled by an edit and so always sees a version nothing has analysed.
Publishing one version in a loop would measure the caches instead — the journal,
the workspace tree and the analysis are all kept per version — and report a
fraction of the real cost.

## Server Benchmarks

_Measured 2026-09-19 at `d0ba678`, Go 1.27. Command: `go test ./internal/server/ -bench=. -benchmem -benchtime=100x`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Completion_Account_Small | 10 | 48,344 | 97,531 | 821 |
| Completion_Account_Medium | 100 | 306,958 | 703,689 | 5,619 |
| Completion_Account_Large | 1,000 | 3,329,388 | 7,428,510 | 52,429 |
| Completion_Payee | 1,000 | 3,007,480 | 7,473,320 | 37,675 |
| Completion_Commodity | 1,000 | 3,308,861 | 7,405,310 | 52,335 |
| DetermineContext_Posting | 1,000 | 44,310 | 73,729 | 1 |
| DetermineContext_Transaction | 1,000 | 43,401 | 73,728 | 1 |
| ExtractAccountPrefix | 1,000 | 43,585 | 73,728 | 1 |
| ApplyChange_Small | 10 | 1,798 | 2,336 | 4 |
| ApplyChange_Large | 1,000 | 53,286 | 229,440 | 4 |

`DetermineContext_*` and `ExtractAccountPrefix` each pay 73,728 B/op because
they build a full `PositionMapper` over the document — a `strings.Split` of
every line — to answer a question about one line. The rope can answer that in
O(log n) and `lsputil.PositionSource` is the seam for it, but migrating the
callers is worth about 4% of a completion (one split is 41us against 3,345us)
and is not done.

## Running Benchmarks

```bash
# All benchmarks
go test ./... -bench=. -benchmem

# Specific package
go test ./internal/workspace/... -bench=. -benchmem

# With count for statistical significance
go test ./internal/parser/... -bench=. -benchmem -count=5

# NFR validation tests (tagged, and not built under -race)
make perf
go test -tags perf ./internal/benchmark/ -v

# With profiling
go test ./internal/parser/... -bench=BenchmarkParser_XLarge -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof -http=:8080 cpu.prof
```

## Key Observations

1. **Parser scaling**: linear with transaction count (~1.1us per transaction)
2. **Memory efficiency**: ~5.0KB per transaction for the full file index
3. **Include tree**: linear in file count, ~26us per file — every load re-parses every file
4. **Incremental updates**: `DidChange` is constant in document size; the debounced publish
   behind it is proportional to the journal, at ~7.3ms for 1000 transactions
5. **Completion latency**: ~3.3ms for 1000 transactions
6. **Analyzer**: ~2.1ms for 1000 transactions; balance check sub-microsecond per transaction
7. **CollectTags**: near-zero allocations
