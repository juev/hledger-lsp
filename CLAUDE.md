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
  server/server.go          LSP server: document sync, diagnostics
```

### Data Flow

1. **Lexer** tokenizes hledger journal text (dates, accounts, amounts, directives)
2. **Parser** builds AST with error recovery (continues parsing after errors)
3. **Server** manages documents, runs analysis, publishes diagnostics via LSP

### Key Design Decisions

- **Hand-written parser** (not generated) for better error recovery in LSP context
- **Pure Go validation** (no hledger CLI dependency) for fast response times
- **Include file handling**: set-based cycle detection, no depth limit

## hledger Journal Format

Transactions start with date at column 0, postings are indented:

```plain
2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
```

Account names contain colons, amounts separated by 2+ spaces from account.

**Documentation references** (use when questions arise about hledger format):

- Local: `docs/hledger.md` — comprehensive format reference
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
