# hledger-lsp Benchmark Results

Benchmarks run on Apple M4 Pro, macOS, Go 1.23.

## NFR Targets

| NFR | Target | Measured | Status |
|-----|--------|----------|--------|
| NFR-1.1 | Completion < 100ms | ~2.5ms | ✅ Pass |
| NFR-1.2 | Parsing 10k lines < 500ms | ~17ms | ✅ Pass |
| NFR-1.3 | Incremental updates < 50ms | ~4ms | ✅ Pass |
| NFR-1.4 | Memory < 200MB | ~27MB | ✅ Pass |

All NFR targets are validated by automated tests in `internal/benchmark/nfr_test.go`.

## Parser Benchmarks

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| Lexer_Small | 10 | 8,141 | 112 | 17 |
| Lexer_Medium | 100 | 81,521 | 640 | 149 |
| Lexer_Large | 1,000 | 825,026 | 5,920 | 1,469 |
| Lexer_XLarge | 10,000 | 8,278,067 | 58,720 | 14,669 |
| Parser_Small | 10 | 16,146 | 28,480 | 166 |
| Parser_Medium | 100 | 158,496 | 257,477 | 1,480 |
| Parser_Large | 1,000 | 1,620,972 | 2,726,442 | 14,576 |
| Parser_XLarge | 10,000 | 17,001,103 | 33,490,410 | 145,436 |

## Workspace Index Benchmarks

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| BuildFileIndex_Small | 10 | 26,853 | 43,900 | 408 |
| BuildFileIndex_Medium | 100 | 235,981 | 364,160 | 2,854 |
| BuildFileIndex_Large | 1,000 | 2,459,610 | 3,948,264 | 26,986 |
| BuildFileIndex_XLarge | 10,000 | 26,236,158 | 45,827,206 | 267,909 |
| UpdateFile | any | ~17 | 0 | 0 |
| IndexSnapshot | any | ~425,000 | 1,561,316 | 5,826 |

## Include Loader Benchmarks

| Benchmark | Files/Transactions | ns/op | B/op | allocs/op |
|-----------|-------------------|-------|------|-----------|
| Load_Small | 1 file / 10 tx | 60,131 | 37,632 | 182 |
| Load_Medium | 1 file / 100 tx | 167,311 | 331,827 | 1,499 |
| Load_Large | 1 file / 1000 tx | 1,662,050 | 3,669,862 | 14,600 |
| LoadFromContent_Small | 1 file / 10 tx | 16,079 | 34,582 | 174 |
| LoadFromContent_Large | 1 file / 1000 tx | 1,589,704 | 3,439,438 | 14,592 |
| IncludeTree_5Files | 5 files / 100 tx | 15,483 | 4,189 | 41 |
| IncludeTree_10Files | 10 files / 200 tx | 18,946 | 7,294 | 66 |
| IncludeTree_20Files | 20 files / 400 tx | 27,372 | 13,561 | 110 |

## Incremental Update Benchmarks

These benchmarks measure the full incremental update cycle when a document changes:

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| DidChange_Incremental_Small | 10 | 59,134 | 730,000 | 9 |
| DidChange_Incremental_Medium | 100 | 30,222 | 306,318 | 9 |
| DidChange_Incremental_Large | 1,000 | 65,252 | 271,241 | 9 |
| PublishDiagnostics_Small | 10 | 48,956 | 92,143 | 586 |
| PublishDiagnostics_Medium | 100 | 425,453 | 806,043 | 4,337 |
| PublishDiagnostics_Large | 1,000 | 4,064,322 | 8,517,535 | 41,440 |

**Components of incremental update:**
1. `DidChange` (sync): Apply text change, update workspace index, invalidate cache (~30-65µs)
2. `PublishDiagnostics` (async): Parse, analyze, publish diagnostics (~50µs - 4ms)

Full cycle for 1000 transactions: ~4ms (well under NFR-1.3 target of 50ms)

## Server Benchmarks

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| Completion_Account_Small | 10 | 29,157 | 64,714 | 453 |
| Completion_Account_Medium | 100 | 233,935 | 432,388 | 2,764 |
| Completion_Account_Large | 1,000 | 2,542,381 | 4,497,016 | 25,575 |
| Completion_Payee | 1,000 | 2,994,421 | 5,897,222 | 32,758 |
| Completion_Commodity | 1,000 | 2,547,245 | 4,409,372 | 25,524 |
| ApplyChange_Small | 10 | 593 | 2,336 | 4 |
| ApplyChange_Large | 1,000 | 54,961 | 229,440 | 4 |

## Running Benchmarks

```bash
# All benchmarks
go test ./... -bench=. -benchmem

# Specific package
go test ./internal/workspace/... -bench=. -benchmem

# With count for statistical significance
go test ./internal/parser/... -bench=. -benchmem -count=5

# NFR validation tests
go test ./internal/benchmark/... -v -run TestNFR

# With profiling
go test ./internal/parser/... -bench=BenchmarkParser_XLarge -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof -http=:8080 cpu.prof
```

## Key Observations

1. **Parser scaling**: Linear with transaction count (~1.6µs per transaction)
2. **Memory efficiency**: ~3.3KB per transaction for full index
3. **Include tree**: Minimal overhead for multi-file journals (~15-27µs for 5-20 files)
4. **Incremental updates**: ~4ms for 1000 transactions (full cycle including diagnostics)
5. **Completion latency**: ~2.5ms for 1000 transactions
6. **Workspace UpdateFile**: Sub-microsecond with zero allocations (deferred indexing)
