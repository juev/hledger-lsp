# PRD: hledger-lsp - Language Server Protocol Server for hledger

## Overview

hledger-lsp is a standalone Language Server Protocol (LSP) server for hledger journal files, implemented in Go. It provides rich editing features including intelligent completions, real-time diagnostics, semantic highlighting, and document formatting for any LSP-compatible editor (VS Code, Neovim, Emacs, Helix, etc.).

This server aims to replace the built-in functionality of the hledger-vscode extension, making these features available to all editors while maintaining feature parity with the existing extension.

---

## Problem Statement

### Current Situation

- hledger users working in editors other than VS Code lack access to intelligent editing features
- The hledger-vscode extension bundles all functionality within VS Code, creating vendor lock-in
- Each editor community must independently implement hledger support, leading to fragmented and inconsistent experiences
- No standardized hledger language server exists despite LSP being the industry standard for editor-agnostic language support

### Evidence of Problem

- Plain text accounting tools like Beancount already have mature LSP servers (beancount-language-server)
- Neovim and Emacs users frequently request hledger language support in community forums
- The hledger-vscode extension has proven demand for these features with active usage

### Impact of Not Solving

- Continued fragmentation of hledger tooling ecosystem
- Reduced adoption of hledger by users of non-VS Code editors
- Duplicated effort across editor communities implementing similar features

---

## Goals & Success Metrics

### Primary Goal

Deliver a production-ready hledger LSP server that achieves feature parity with hledger-vscode within 6 months of initial development.

### Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Feature parity | 100% of hledger-vscode features | Feature checklist completion |
| Editor compatibility | VS Code, Neovim, Emacs | Verified integration guides |
| Response latency | < 100ms for completions | Benchmark tests |
| Parsing accuracy | 100% valid hledger files parsed | Parser test suite |
| Test coverage | > 80% | Go test coverage reports |
| Adoption | 100+ GitHub stars in first year | GitHub metrics |

### Secondary Goals

- Establish foundation for future hledger tooling (code actions, refactoring)
- Create reusable hledger parsing library in Go
- Enable CI/CD integration for hledger validation

---

## User Personas

### 1. Professional Accountant - "Sarah"

**Background:** CPA using hledger for personal and small business accounting
**Editor:** VS Code
**Needs:**

- Fast, accurate completions for accounts and payees
- Real-time balance validation
- Consistent formatting across team members
**Pain Points:**
- Manual account name entry is error-prone
- Typos in amounts cause hours of debugging

### 2. Developer with Personal Finance Tracking - "Alex"

**Background:** Software engineer tracking personal finances
**Editor:** Neovim with LSP support
**Needs:**

- Vim-native experience with intelligent suggestions
- Integration with existing Neovim LSP ecosystem
- Semantic highlighting matching colorscheme
**Pain Points:**
- Currently uses basic syntax highlighting only
- No validation until running hledger manually

### 3. Power User with Large Ledgers - "Jordan"

**Background:** Uses hledger with 10+ years of transaction history
**Editor:** Emacs
**Needs:**

- Fast performance on large files (10,000+ transactions)
- Include file support across multiple journals
- Workspace-wide operations (find references)
**Pain Points:**
- Slow parsing times
- Difficult to track account usage across included files

---

## Feature Requirements

### Functional Requirements

#### FR-1: Document Synchronization

- **FR-1.1:** Track open/close of hledger documents
- **FR-1.2:** Support incremental document changes
- **FR-1.3:** Handle save notifications for triggering validation
- **FR-1.4:** Support file extensions: `.journal`, `.hledger`, `.ledger`

#### FR-2: Completion Provider

- **FR-2.1:** Context-aware account name completion
- **FR-2.2:** Payee/description completion from journal history
- **FR-2.3:** Commodity symbol completion
- **FR-2.4:** Tag name and value completion
- **FR-2.5:** Directive completion (account, commodity, include, etc.)
- **FR-2.6:** Date completion with smart suggestions (today, relative dates)
- **FR-2.7:** Transaction templates from historical transactions
- **FR-2.8:** Frequency-based prioritization of suggestions
- **FR-2.9:** Configurable maximum results limit

#### FR-3: Diagnostics Provider

- **FR-3.1:** Real-time balance validation per transaction
- **FR-3.2:** Multi-commodity balance checking
- **FR-3.3:** Balance assertion validation (=, ==, =*, ==*)
- **FR-3.4:** Cost notation validation (@ unit, @@ total)
- **FR-3.5:** Undefined account warnings (when account directives used)
- **FR-3.6:** Undefined commodity warnings
- **FR-3.7:** Syntax error reporting with line/column positions
- **FR-3.8:** Include file resolution validation
- **FR-3.9:** Configurable enable/disable of diagnostics

#### FR-4: Document Formatting

- **FR-4.1:** Smart column alignment for amounts
- **FR-4.2:** Comment alignment
- **FR-4.3:** Configurable amount alignment column (20-120)
- **FR-4.4:** Commodity-aware formatting per commodity directives
- **FR-4.5:** Preserve balance assertions, virtual postings, metadata
- **FR-4.6:** Support format-on-save via standard LSP flow

#### FR-5: Semantic Tokens Provider

- **FR-5.1:** Token types: account, amount, comment, date, commodity, payee, tag, directive, operator, code, link
- **FR-5.2:** Full document semantic tokens
- **FR-5.3:** Delta updates for changed regions
- **FR-5.4:** Theme-adaptive token modifiers

#### FR-6: Hover Provider

- **FR-6.1:** Account balance on hover (current file)
- **FR-6.2:** Commodity information on hover
- **FR-6.3:** Transaction totals on hover
- **FR-6.4:** Tag value information

#### FR-7: Document Symbols

- **FR-7.1:** List all transactions as symbols
- **FR-7.2:** List account declarations
- **FR-7.3:** List commodity declarations
- **FR-7.4:** Hierarchical symbol structure

#### FR-8: Workspace Features

- **FR-8.1:** Include file resolution with glob support
- **FR-8.2:** Cross-file account tracking
- **FR-8.3:** Workspace-wide completions

#### FR-9: CLI Integration (Code Actions)

- **FR-9.1:** Insert balance report as comments
- **FR-9.2:** Insert income statement as comments
- **FR-9.3:** Insert statistics report
- **FR-9.4:** Configurable hledger CLI path
- **FR-9.5:** Support LEDGER_FILE environment variable

### Non-Functional Requirements

#### NFR-1: Performance

- **NFR-1.1:** Completion response < 100ms
- **NFR-1.2:** Document parsing < 500ms for 10,000 lines
- **NFR-1.3:** Incremental updates < 50ms
- **NFR-1.4:** Memory usage < 200MB for large workspaces

#### NFR-2: Reliability

- **NFR-2.1:** Graceful handling of malformed input
- **NFR-2.2:** No crashes on any valid or invalid input
- **NFR-2.3:** Proper cleanup on shutdown

#### NFR-3: Compatibility

- **NFR-3.1:** LSP specification 3.17 compliance
- **NFR-3.2:** hledger 1.30+ journal format support
- **NFR-3.3:** Cross-platform: macOS, Linux, Windows
- **NFR-3.4:** Go 1.21+ compatibility

#### NFR-4: Security

- **NFR-4.1:** Validate paths to prevent command injection
- **NFR-4.2:** Reject shell metacharacters in CLI arguments
- **NFR-4.3:** Sandbox file access to workspace

#### NFR-5: Maintainability

- **NFR-5.1:** Modular architecture with clear separation
- **NFR-5.2:** Comprehensive test coverage (TDD approach)
- **NFR-5.3:** Minimal code comments (self-documenting code)

---

## Technical Architecture

### High-Level Architecture

```
+------------------+     JSON-RPC 2.0     +------------------+
|                  | <------------------> |                  |
|  Editor Client   |      (stdio)         |  hledger-lsp     |
|  (VS Code, etc.) |                      |  Server          |
|                  |                      |                  |
+------------------+                      +--------+---------+
                                                   |
                                                   v
                                          +-------+--------+
                                          |                |
                                          |  Core Engine   |
                                          |                |
                                          +---+---+---+----+
                                              |   |   |
                              +---------------+   |   +---------------+
                              |                   |                   |
                              v                   v                   v
                      +-------+------+    +------+-------+    +------+-------+
                      |              |    |              |    |              |
                      |   Parser     |    |  Analyzer    |    |  Formatter   |
                      |   (Journal)  |    |  (Semantic)  |    |              |
                      |              |    |              |    |              |
                      +--------------+    +------+-------+    +--------------+
                                                 |
                                                 v
                                         +-------+--------+
                                         |                |
                                         |  hledger CLI   |
                                         |  (optional)    |
                                         |                |
                                         +----------------+
```

### Component Design

#### 1. Protocol Layer (`internal/protocol/`)

- JSON-RPC 2.0 message handling
- LSP message type definitions
- Request/response routing
- Uses `go.lsp.dev/protocol` and `go.lsp.dev/jsonrpc2`

#### 2. Server Layer (`internal/server/`)

- LSP server implementation
- Capability negotiation
- Document lifecycle management
- Request handlers for each LSP method

#### 3. Parser Layer (`internal/parser/`)

- hledger journal format parser
- Incremental parsing support
- AST representation
- Error recovery for partial parsing

#### 4. Analyzer Layer (`internal/analyzer/`)

- Semantic analysis
- Balance validation
- Account/commodity resolution
- Cross-file analysis for workspaces

#### 5. Provider Layer (`internal/providers/`)

- `completion.go` - Completion provider
- `diagnostics.go` - Diagnostics provider
- `formatting.go` - Document formatting
- `hover.go` - Hover information
- `semantic.go` - Semantic tokens
- `symbols.go` - Document symbols

#### 6. CLI Integration (`internal/cli/`)

- hledger CLI wrapper
- Report generation
- Path validation and security

### Data Models

#### Journal AST

```go
type Journal struct {
    Directives   []Directive
    Transactions []Transaction
    Comments     []Comment
    Includes     []Include
}

type Transaction struct {
    Date        Date
    Date2       *Date
    Status      Status
    Code        string
    Description string
    Postings    []Posting
    Tags        []Tag
    Comments    []Comment
    Range       Range
}

type Posting struct {
    Status      Status
    Account     Account
    Amount      *Amount
    BalanceAssertion *BalanceAssertion
    Comment     string
    Tags        []Tag
    Virtual     VirtualType
    Range       Range
}

type Amount struct {
    Quantity  Decimal
    Commodity string
    Cost      *Cost
}
```

### Technology Stack

| Component | Technology | Rationale |
|-----------|------------|-----------|
| Language | Go 1.21+ | Performance, cross-platform, single binary |
| LSP Protocol | go.lsp.dev/protocol | Standard Go LSP types |
| JSON-RPC | go.lsp.dev/jsonrpc2 | LSP-compatible transport |
| Decimal Math | shopspring/decimal | Precise financial calculations |
| Testing | testing + testify | Standard Go testing with assertions |
| CLI Parser | cobra | Standard Go CLI framework |

---

## API/Protocol Specifications

### Supported LSP Methods

#### Lifecycle

| Method | Support | Notes |
|--------|---------|-------|
| initialize | Yes | Returns server capabilities |
| initialized | Yes | Server ready notification |
| shutdown | Yes | Graceful shutdown |
| exit | Yes | Process termination |

#### Document Sync

| Method | Support | Notes |
|--------|---------|-------|
| textDocument/didOpen | Yes | Parse and analyze |
| textDocument/didChange | Yes | Incremental updates |
| textDocument/didSave | Yes | Trigger validation |
| textDocument/didClose | Yes | Cleanup resources |

#### Language Features

| Method | Support | Notes |
|--------|---------|-------|
| textDocument/completion | Yes | Context-aware |
| textDocument/hover | Yes | Account/amount info |
| textDocument/publishDiagnostics | Yes | Real-time errors |
| textDocument/formatting | Yes | Column alignment |
| textDocument/semanticTokens/full | Yes | All tokens |
| textDocument/semanticTokens/full/delta | Yes | Incremental |
| textDocument/documentSymbol | Yes | Transactions, directives |

#### Workspace

| Method | Support | Notes |
|--------|---------|-------|
| workspace/executeCommand | Yes | CLI integration |
| workspace/configuration | Yes | Settings |

### Server Capabilities Response

```json
{
  "capabilities": {
    "textDocumentSync": {
      "openClose": true,
      "change": 2,
      "save": { "includeText": false }
    },
    "completionProvider": {
      "triggerCharacters": [":", " ", "@", "="],
      "resolveProvider": true
    },
    "hoverProvider": true,
    "documentFormattingProvider": true,
    "semanticTokensProvider": {
      "legend": {
        "tokenTypes": ["account", "amount", "comment", "date", "commodity", "payee", "tag", "directive", "operator", "code", "link"],
        "tokenModifiers": ["declaration", "definition"]
      },
      "full": { "delta": true }
    },
    "documentSymbolProvider": true,
    "executeCommandProvider": {
      "commands": ["hledger.insertBalanceReport", "hledger.insertIncomeStatement", "hledger.insertStatistics"]
    }
  }
}
```

### Configuration Schema

```json
{
  "hledger.completion.enabled": {
    "type": "boolean",
    "default": true
  },
  "hledger.completion.maxResults": {
    "type": "integer",
    "default": 50
  },
  "hledger.diagnostics.enabled": {
    "type": "boolean",
    "default": true
  },
  "hledger.formatting.amountAlignmentColumn": {
    "type": "integer",
    "default": 52,
    "minimum": 20,
    "maximum": 120
  },
  "hledger.semanticHighlighting.enabled": {
    "type": "boolean",
    "default": true
  },
  "hledger.cli.path": {
    "type": "string",
    "default": "hledger"
  },
  "hledger.cli.journalFile": {
    "type": "string",
    "default": ""
  }
}
```

---

## Implementation Phases

### Phase 1: Foundation (Weeks 1-4)

**Goal:** Basic LSP server with document sync and parsing

**Deliverables:**

- Project structure and build system
- LSP server skeleton with lifecycle methods
- Basic hledger journal parser
- Document sync (open/close/change)
- Unit tests for parser

**Success Criteria:**

- Server starts and responds to initialize
- Parser handles basic transactions
- 80%+ test coverage for parser

### Phase 2: Core Features (Weeks 5-8)

**Goal:** Essential editing features

**Deliverables:**

- Completion provider (accounts, payees, commodities)
- Diagnostics provider (balance validation)
- Document formatting
- Semantic tokens

**Success Criteria:**

- Completions work in VS Code
- Balance errors detected
- Formatting aligns columns correctly

### Phase 3: Advanced Features (Weeks 9-12)

**Goal:** Feature parity with hledger-vscode

**Deliverables:**

- Hover provider
- Document symbols
- Transaction templates
- Include file support
- CLI integration

**Success Criteria:**

- All hledger-vscode features replicated
- Performance benchmarks met
- Integration tests pass

### Phase 4: Polish & Release (Weeks 13-16)

**Goal:** Production-ready release

**Deliverables:**

- Editor integration guides (VS Code, Neovim, Emacs)
- Performance optimization
- Documentation
- CI/CD pipeline
- Release binaries

**Success Criteria:**

- All editors working
- < 100ms completion latency
- Complete documentation

---

## Testing Strategy

### TDD Approach (Per User Requirements)

All features developed using Test-Driven Development:

1. Write failing test first
2. Implement minimal code to pass
3. Refactor while keeping tests green

### Test Categories

#### Unit Tests (`*_test.go`)

- Parser: Every syntax element
- Analyzer: Balance calculations, validations
- Providers: Completion logic, formatting rules
- Target: 80%+ coverage

#### Integration Tests (`internal/integration/`)

- Full LSP request/response cycles
- Multi-file workspace scenarios
- CLI integration

#### Benchmark Tests (`*_bench_test.go`)

- Parser performance on large files
- Completion response time
- Memory usage under load

### Test Data

- `testdata/valid/` - Valid journal files
- `testdata/invalid/` - Files with errors
- `testdata/large/` - Performance test files

### Example Test Structure

```go
func TestParser_Transaction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Transaction
        wantErr bool
    }{
        {
            name: "simple transaction",
            input: `2024-01-15 Grocery Store
    expenses:food    $50.00
    assets:checking`,
            want: &Transaction{
                Date: Date{Year: 2024, Month: 1, Day: 15},
                Description: "Grocery Store",
                Postings: []Posting{...},
            },
        },
        // More test cases...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

---

## Risks & Mitigations

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| hledger format complexity exceeds estimates | Medium | High | Start with core syntax, add edge cases iteratively; use hledger source as reference |
| Performance issues with large journals | Medium | Medium | Implement incremental parsing early; benchmark continuously |
| LSP specification edge cases | Low | Medium | Test with multiple editors from Phase 1 |
| hledger CLI version incompatibilities | Low | Low | Document supported versions; test against multiple versions |
| Go LSP libraries lack features | Low | Medium | Fall back to lower-level jsonrpc2 if needed |
| Decimal precision issues | Low | High | Use shopspring/decimal; extensive balance validation tests |

---

## Open Questions

1. **Parser Implementation Strategy**
   - Should we use a parser generator (ANTLR, PEG) or hand-written parser?
   - Recommendation: Hand-written for control over error recovery

2. **Include File Handling**
   - How to handle circular includes?
   - Maximum include depth?

3. **Validation Modes**
   - Should validation use hledger CLI or pure Go implementation?
   - Trade-off: Accuracy vs. performance and dependency

4. **Configuration Scope**
   - Support user-level vs. workspace-level settings?
   - Follow LSP standard configuration patterns

5. **Future Features**
   - Code actions for common refactorings (rename account)?
   - Go-to-definition for accounts?

---

## Out of Scope

The following are explicitly NOT included in this initial release:

- CSV/TSV import functionality (keep in VS Code extension)
- GUI components
- Web-based interface
- Direct database integrations
- Custom report generation beyond CLI wrappers
- Multi-currency conversion calculations
- Budget forecasting features
- Account reconciliation workflows

These may be considered for future versions.

---

## References

### Primary Sources

- [hledger-vscode Extension](https://github.com/juev/hledger-vscode) - Feature reference
- [hledger Journal Format](https://hledger.org/journal.html) - Official format specification
- [hledger Manual](https://hledger.org/1.51/hledger.html) - Complete documentation

### LSP Resources

- [LSP 3.17 Specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification)
- [gopls Implementation](https://go.dev/gopls) - Reference Go LSP server

### Go Libraries

- [go.lsp.dev/protocol](https://pkg.go.dev/go.lsp.dev/protocol) - LSP types
- [go.lsp.dev/jsonrpc2](https://pkg.go.dev/go.lsp.dev/jsonrpc2) - JSON-RPC transport

### Similar Projects

- [beancount-language-server](https://github.com/polarmutex/beancount-language-server) - Architecture reference

---

## Appendix A: Semantic Token Types

| Token Type | Description | Example |
|------------|-------------|---------|
| account | Account name | `expenses:food` |
| amount | Numeric value | `50.00` |
| commodity | Currency/commodity symbol | `$`, `EUR` |
| date | Transaction date | `2024-01-15` |
| payee | Payee/description | `Grocery Store` |
| comment | Comment text | `; note` |
| tag | Tag name/value | `trip:japan` |
| directive | Directive keyword | `account`, `include` |
| operator | Operators | `@`, `=` |
| code | Transaction code | `(12345)` |
| link | URL/link | `https://...` |

## Appendix B: Completion Trigger Contexts

| Context | Trigger | Completions |
|---------|---------|-------------|
| Line start | Date pattern | Date suggestions |
| After date | Space | Status markers, payees |
| Posting start | Indent | Account names |
| After account | Two spaces | Amount, assertions |
| After amount | `@` | Commodity for cost |
| After amount | `=` | Balance assertions |
| In comment | `:` | Tag names |
| After tag | `:` | Tag values |
| Directive | Keyword | Directive completions |

## Appendix C: Directory Structure

```
hledger-lsp/
├── cmd/
│   └── hledger-lsp/
│       └── main.go
├── internal/
│   ├── analyzer/
│   │   ├── analyzer.go
│   │   ├── balance.go
│   │   └── analyzer_test.go
│   ├── cli/
│   │   ├── hledger.go
│   │   └── hledger_test.go
│   ├── parser/
│   │   ├── parser.go
│   │   ├── lexer.go
│   │   ├── ast.go
│   │   └── parser_test.go
│   ├── providers/
│   │   ├── completion.go
│   │   ├── diagnostics.go
│   │   ├── formatting.go
│   │   ├── hover.go
│   │   ├── semantic.go
│   │   ├── symbols.go
│   │   └── *_test.go
│   └── server/
│       ├── server.go
│       ├── handlers.go
│       ├── config.go
│       └── server_test.go
├── testdata/
│   ├── valid/
│   ├── invalid/
│   └── large/
├── docs/
│   ├── vscode.md
│   ├── neovim.md
│   └── emacs.md
├── go.mod
├── go.sum
├── Makefile
└── README.md
```
