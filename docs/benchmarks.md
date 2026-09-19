# hledger-lsp Benchmark Results

All numbers below were measured on Apple M4 Pro, macOS, Go 1.27, at commit
`fb19bb6`. Every table carries its own date, commit, toolchain and command, so a
stale number is visible as a stale line rather than as an undated claim.

## NFR Targets

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test -tags perf ./internal/benchmark/ -v` (or `make perf`)._

| NFR | Target | Measured | Status |
| --- | --- | --- | --- |
| NFR-1.1 | Completion < 100ms | ~3.1ms | Pass |
| NFR-1.2 | Parsing 10k lines < 500ms | ~10.7ms | Pass |
| NFR-1.3 | Incremental updates < 50ms | ~4.9ms | Pass |
| NFR-1.4 | Memory < 200MB | ~43MB | Pass |

The NFR tests are additionally covered by ratio assertions that do not depend on
machine speed: `TestNFR_DidChangeSubLinear` (10k/1k <= 2x),
`TestNFR_PerKeystrokeCycle` (10k/1k <= 4x) and `TestNFR_KeystrokeBurst`.

## Parser Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/parser/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Lexer_Small | 10 | 4,350 | 68 | 17 |
| Lexer_Medium | 100 | 42,238 | 644 | 161 |
| Lexer_Large | 1,000 | 426,346 | 6,404 | 1,601 |
| Lexer_XLarge | 10,000 | 4,319,460 | 64,004 | 16,001 |
| Parser_Small | 10 | 11,888 | 30,464 | 163 |
| Parser_Medium | 100 | 107,652 | 306,996 | 1,516 |
| Parser_Large | 1,000 | 1,043,067 | 3,033,717 | 15,020 |
| Parser_XLarge | 10,000 | 10,118,200 | 30,481,928 | 150,027 |
| Parser_Parallel | 100 | 56,226 | 306,998 | 1,516 |

## Analyzer Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/analyzer/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Analyze_Small | 10 | 23,189 | 43,614 | 419 |
| Analyze_Medium | 100 | 196,063 | 390,480 | 2,968 |
| Analyze_Large | 1,000 | 2,087,587 | 4,546,691 | 27,787 |
| CalculateAccountBalances | 1,000 | 758,067 | 2,018,834 | 24,413 |
| CheckBalance | 1 tx | 664.6 | 2,208 | 25 |
| CheckBalance_MultiCommodity | 1 tx | 593.0 | 1,992 | 23 |
| CollectAccounts | 1,000 | 31,138 | 3,568 | 67 |
| CollectPayees | 1,000 | 59,710 | 143,944 | 31 |
| CollectCommodities | 1,000 | 22,681 | 112 | 3 |
| CollectTags | 1,000 | 9,028 | 16 | 1 |

## Workspace Index Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/workspace/ -bench=. -benchmem`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| BuildFileIndex_Small | 10 | 25,126 | 53,136 | 512 |
| BuildFileIndex_Medium | 100 | 226,846 | 482,503 | 4,000 |
| BuildFileIndex_Large | 1,000 | 2,372,683 | 5,027,255 | 38,468 |
| BuildFileIndex_XLarge | 10,000 | 24,575,601 | 49,688,916 | 382,614 |
| Workspace_UpdateFile_Small | 10 | 105,822 | 152,502 | 1,499 |
| Workspace_UpdateFile_Medium | 100 | 626,417 | 1,093,593 | 8,690 |
| Workspace_UpdateFile_Large | 1,000 | 5,892,558 | 11,257,718 | 79,481 |
| Workspace_IndexSnapshot_Small | 10 | 3,236 | 10,456 | 67 |
| Workspace_IndexSnapshot_Medium | 100 | 17,521 | 65,984 | 249 |
| Workspace_IndexSnapshot_Large | 1,000 | 154,301 | 734,584 | 2,055 |

`Workspace_UpdateFile_*` covers an edit that alters the include graph: re-parse,
index rebuild and include re-resolution. It does not defer indexing, so it is not
a sub-microsecond operation — see the Incremental Update section for the per-edit
cost as the server actually pays it.

## Include Loader Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/include/ -bench=. -benchmem`._

| Benchmark | Files/Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Load_Small | 1 file / 10 tx | 38,773 | 43,936 | 277 |
| Load_Medium | 1 file / 100 tx | 132,811 | 350,563 | 1,633 |
| Load_Large | 1 file / 1000 tx | 1,273,242 | 3,375,677 | 15,141 |
| LoadFromContent_Small | 1 file / 10 tx | 26,434 | 41,568 | 264 |
| LoadFromContent_Large | 1 file / 1000 tx | 1,148,324 | 3,146,211 | 15,128 |
| Load_IncludeTree_5Files | 5 files / 100 tx | 266,195 | 415,353 | 2,467 |
| Load_IncludeTree_10Files | 10 files / 200 tx | 515,847 | 824,341 | 4,805 |
| Load_IncludeTree_20Files | 20 files / 400 tx | 1,016,979 | 1,633,972 | 9,451 |

`Load_IncludeTree_*` currently grows linearly with file count: every load
re-parses every file in the tree, and the loader caches only normalized text.

## Incremental Update Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/server/ -run '^$' -bench='DidChange|PublishDiagnostics' -benchmem -benchtime=100x`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| DidChange_Incremental_Small | 10 | 183,799 | 252,883 | 2,140 |
| DidChange_Incremental_Medium | 100 | 690,898 | 1,232,753 | 9,342 |
| DidChange_Incremental_Large | 1,000 | 6,219,433 | 11,769,101 | 79,480 |
| PublishDiagnostics_Small | 10 | 75,174 | 129,089 | 1,067 |
| PublishDiagnostics_Medium | 100 | 482,296 | 1,105,521 | 7,336 |
| PublishDiagnostics_Large | 1,000 | 4,343,073 | 11,384,560 | 68,898 |

**Components of incremental update:**

1. `DidChange` (sync): apply the text change, update the workspace index, invalidate caches, schedule debounced diagnostics
2. `PublishDiagnostics` (debounced, async): parse the include tree, analyze, publish

## Server Benchmarks

_Measured 2026-09-19 at `fb19bb6`, Go 1.27. Command: `go test ./internal/server/ -bench=. -benchmem -benchtime=100x`._

| Benchmark | Transactions | ns/op | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Completion_Account_Small | 10 | 43,179 | 97,458 | 820 |
| Completion_Account_Medium | 100 | 306,938 | 700,909 | 5,618 |
| Completion_Account_Large | 1,000 | 3,329,747 | 7,428,106 | 52,430 |
| Completion_Payee | 1,000 | 2,947,168 | 7,472,784 | 37,676 |
| Completion_Commodity | 1,000 | 3,292,207 | 7,404,715 | 52,336 |
| DetermineContext_Posting | 1,000 | 41,903 | 73,728 | 1 |
| ApplyChange_Small | 10 | 855.4 | 2,336 | 4 |
| ApplyChange_Large | 1,000 | 50,150 | 229,440 | 4 |

`DetermineContext_*`, `ExtractAccountPrefix` and `ApplyChange_*` each pay 73,728
B/op because they build a full `PositionMapper` over the document — a
`strings.Split` of every line — to answer a question about one line.

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

1. **Parser scaling**: linear with transaction count (~1.0us per transaction)
2. **Memory efficiency**: ~5.0KB per transaction for the full file index
3. **Include tree**: linear in file count, ~50us per file — every load re-parses every file
4. **Incremental updates**: ~4.9ms for 1000 transactions (full cycle including diagnostics)
5. **Completion latency**: ~3.3ms for 1000 transactions
6. **Analyzer**: ~2.1ms for 1000 transactions; balance check sub-microsecond per transaction
7. **CollectTags**: near-zero allocations
