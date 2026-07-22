# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

hledger-lsp is a Language Server Protocol (LSP) server for hledger journal files, written in Go. It provides editing features (completions, diagnostics, formatting) for any LSP-compatible editor.

## Commands

```bash
# Build
go build -o ./bin/hledger-lsp ./cmd/hledger-lsp

# Test
go test ./...
go test -v ./internal/parser/...              # specific package
go test -v ./internal/parser -run TestLexer   # specific test
go test -cover ./...                          # with coverage

# Lint
golangci-lint run --fix ./...
```

## Architecture

```plain
cmd/hledger-lsp/main.go     LSP server entry point, protocol dispatcher
internal/
  ast/types.go              AST types: Journal, Transaction, Posting, Amount, etc.
  parser/
    token.go                Token types and Position
    lexer.go                Hand-written lexer for hledger format
    parser.go               Parser with error recovery
  formatter/
    formatter.go            Document formatting: alignment (right/decimal), indentation
  server/
    server.go               LSP server: document sync, diagnostics
    completion.go           Completion: context detection, item generation, fuzzy scoring
```

### Data Flow

1. **Lexer** tokenizes hledger journal text (dates, accounts, amounts, directives)
2. **Parser** builds AST with error recovery (continues parsing after errors)
3. **Server** manages documents, runs analysis, publishes diagnostics via LSP

### Key Design Decisions

- **Hand-written parser** (not generated) for better error recovery in LSP context
- **Pure Go validation** (no hledger CLI dependency) for fast response times
- **Include file handling**: occurrence-aware resolution following hledger 1.52.1
  (repeated/diamond includes counted per occurrence, textual-inline order, active
  canonical ancestor cycle detection, edge-based depth limit, multi-owner reload)

## hledger Journal Format

Transactions start with date at column 0, postings are indented:

```plain
2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
```

Account names contain colons, amounts separated by 2+ spaces from account.

**Documentation references** (use when questions arise about hledger format):

- Local: `docs/hledger.md` — journal format reference
- Local: `docs/hledger-csv-rules.md` — CSV rules format reference (for `.rules` files)
- Official: <https://hledger.org/hledger.html>

## Task Tracking

**IMPORTANT**: Always use `tasks.md` for tracking project progress.

Before starting work:

1. Check `tasks.md` to understand current status and priorities
2. Mark the task as in progress
3. After completion, mark task with `[x]` and update related items

This ensures continuity between sessions and clear visibility of what's done and what remains.

## Development Notes

- Use TDD methodology
- Target 80%+ test coverage for parser
- Decimal arithmetic via `shopspring/decimal`
- LSP protocol via `go.lsp.dev/protocol` and `go.lsp.dev/jsonrpc2`
- All user-visible error messages (parser errors, analyzer diagnostics, loader errors) must be human-readable. Do not expose internal token type names, AST node types, or other implementation details in error messages.

### Unicode

Account names, descriptions and comments may contain CJK, Cyrillic, emoji and other multi-byte characters. Three different metrics apply — do not conflate them:

- **Display width** (terminal cells, for visual column alignment): use the runewidth-based `displayWidth` helper in `internal/formatter` (East Asian Width via `go-runewidth`). A full-width CJK character occupies 2 cells. `utf8.RuneCountInString` is wrong here — it counts a CJK character as 1 and drifts amounts out of visual alignment.
- **Rune count / positional arithmetic**: use `utf8.RuneCountInString` (not `len`, which counts bytes).
- **LSP column offsets**: use `lsputil.UTF16Len`.

Every new feature that touches text must include tests with non-ASCII (CJK, Cyrillic) input.

### Line Endings (LF / CRLF)

Windows editors send documents with `\r\n`. The server normalizes all line endings to `\n` at ingestion (`DidOpen`, `DidChange`) via `normalizeLineEndings`. All downstream code (lexer, parser, formatter, mapper) assumes `\n`-only input. When adding new document ingestion paths, always apply `normalizeLineEndings`. Tests for any text-processing feature must include a CRLF variant to prevent regressions.

### Whitespace Normalization in Formatter

The formatter owns canonical whitespace: `"; "` prefix for comments, indent size for postings, 2+ spaces between account and amount. Input may contain arbitrary spacing (e.g. `;date:`, `;  date:`). The formatter must trim/normalize before writing, so that repeated formatting is idempotent (format(format(x)) == format(x)). Any new formatting rule must have an idempotency test.
