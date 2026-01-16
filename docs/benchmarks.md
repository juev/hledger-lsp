# hledger-lsp Benchmark Results

Benchmarks run on Apple M4 Pro, macOS, Go 1.23.

## NFR Targets

| NFR | Target | Measured | Status |
|-----|--------|----------|--------|
| NFR-1.1 | Completion < 100ms | ~3ms | ✅ Pass |
| NFR-1.2 | Parsing 10k lines < 500ms | ~25ms | ✅ Pass |
| NFR-1.3 | Incremental updates < 50ms | ~2.5ms | ✅ Pass |
| NFR-1.4 | Memory < 200MB | ~27MB | ✅ Pass |

All NFR targets are validated by automated tests in `internal/benchmark/nfr_test.go`.

## Parser Benchmarks

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| Lexer_Small | 10 | 7,791 | 112 | 17 |
| Lexer_Medium | 100 | 76,874 | 640 | 149 |
| Lexer_Large | 1,000 | 772,657 | 5,920 | 1,469 |
| Lexer_XLarge | 10,000 | 8,095,857 | 58,720 | 14,669 |
| Parser_Small | 10 | 15,305 | 28,479 | 166 |
| Parser_Medium | 100 | 141,629 | 257,470 | 1,480 |
| Parser_Large | 1,000 | 1,441,566 | 2,726,407 | 14,575 |
| Parser_XLarge | 10,000 | 15,985,550 | 33,489,853 | 145,436 |

## Workspace Index Benchmarks

| Benchmark | Transactions | ns/op | B/op | allocs/op |
|-----------|-------------|-------|------|-----------|
| BuildFileIndex_Small | 10 | 26,008 | 43,900 | 408 |
| BuildFileIndex_Medium | 100 | 229,906 | 364,170 | 2,854 |
| BuildFileIndex_Large | 1,000 | 2,394,453 | 3,948,479 | 26,988 |
| BuildFileIndex_XLarge | 10,000 | 23,823,716 | 45,825,862 | 267,902 |
| IndexSnapshot | any | ~390,000 | 1,562,406 | 5,828 |

## Include Loader Benchmarks

| Benchmark | Files/Transactions | ns/op | B/op | allocs/op |
|-----------|-------------------|-------|------|-----------|
| Load_Small | 1 file / 10 tx | 27,109 | 37,616 | 182 |
| Load_Medium | 1 file / 100 tx | 153,337 | 331,802 | 1,499 |
| Load_Large | 1 file / 1000 tx | 1,584,835 | 3,669,918 | 14,601 |
| LoadFromContent_Large | 1 file / 1000 tx | 1,597,170 | 3,439,435 | 14,592 |
| IncludeTree_5Files | 5 files / 100 tx | 16,040 | 3,940 | 37 |
| IncludeTree_10Files | 10 files / 200 tx | 19,505 | 6,797 | 61 |
| IncludeTree_20Files | 20 files / 400 tx | 24,177 | 12,569 | 104 |

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

1. **Parser scaling**: Linear with transaction count (~1.5µs per transaction)
2. **Memory efficiency**: ~3.3KB per transaction for full index
3. **Include tree**: Minimal overhead for multi-file journals
4. **Incremental updates**: Sub-millisecond for typical edit operations
