# hledger-lsp

[![Go Version](https://img.shields.io/github/go-mod/go-version/juev/hledger-lsp)](https://go.dev/)
[![License](https://img.shields.io/github/license/juev/hledger-lsp)](LICENSE)
[![Release](https://img.shields.io/github/v/release/juev/hledger-lsp)](https://github.com/juev/hledger-lsp/releases)
[![coverage](https://raw.githubusercontent.com/juev/hledger-lsp/badges/.badges/main/coverage.svg)](https://github.com/juev/hledger-lsp/actions)

A Language Server Protocol (LSP) implementation for [hledger](https://hledger.org/) journal files. Provides IDE features like completions, diagnostics, formatting, and more for any LSP-compatible editor.

## 🎯 Features

### Completions
- **Accounts** — Fuzzy matching with frequency-based ranking
- **Payees** — From transaction history with usage counts
- **Commodities** — From directives and usage
- **Tags** — Name and value completion from existing tags
- **Dates** — today/yesterday/tomorrow + historical dates from file

### Navigation
- **Go to Definition** — Jump to account/commodity/payee declaration
- **Find References** — Find all usages across workspace
- **Rename** — Refactor accounts, commodities, and payees across files
- **Workspace Symbol** — Quick search for accounts, commodities, payees

### Diagnostics
- Real-time validation of transactions
- Balance checks and syntax errors

### Other
- **Formatting** — Automatic alignment of amounts (right-aligned or decimal-point aligned)
- **Hover** — Account balances on hover
- **Semantic Tokens** — Syntax highlighting with delta support
- **Document Symbols** — Outline navigation
- **Folding Ranges** — Collapse transactions and directives
- **Document Links** — Clickable include file paths
- **Include Support** — Multi-file journals with cycle detection

## 📦 Installation

### From Releases

Download the latest binary for your platform from [GitHub Releases](https://github.com/juev/hledger-lsp/releases).

```bash
# macOS (Apple Silicon)
curl -L https://github.com/juev/hledger-lsp/releases/latest/download/hledger-lsp_darwin_arm64 -o hledger-lsp
chmod +x hledger-lsp
sudo mv hledger-lsp /usr/local/bin/

# macOS (Intel)
curl -L https://github.com/juev/hledger-lsp/releases/latest/download/hledger-lsp_darwin_amd64 -o hledger-lsp
chmod +x hledger-lsp
sudo mv hledger-lsp /usr/local/bin/

# Linux (x86_64)
curl -L https://github.com/juev/hledger-lsp/releases/latest/download/hledger-lsp_linux_amd64 -o hledger-lsp
chmod +x hledger-lsp
sudo mv hledger-lsp /usr/local/bin/
```

### From Source

```bash
go install github.com/juev/hledger-lsp/cmd/hledger-lsp@latest
```

### Verify Installation

```bash
hledger-lsp --version
```

## 🚀 Quick Start

1. Install hledger-lsp (see above)
2. Configure your editor (see below)
3. Open a `.journal` or `.hledger` file
4. Start typing and enjoy completions!

```hledger
2024-01-15 * grocery store
    expenses:food  $50.00
    assets:cash
```

## 🔧 Editor Setup

- [VS Code](docs/vscode.md)
- [Neovim](docs/neovim.md)
- [Emacs](docs/emacs.md)

## ⚙️ Configuration

See `docs/configuration.md` for supported settings and defaults.

## ⚙️ Supported Features

| Feature | Status |
|---------|--------|
| Completions | ✅ |
| Diagnostics | ✅ |
| Formatting | ✅ |
| Hover | ✅ |
| Semantic Tokens | ✅ |
| Document Symbols | ✅ |
| Go to Definition | ✅ |
| Find References | ✅ |
| Rename | ✅ |
| Folding Ranges | ✅ |
| Document Links | ✅ |
| Workspace Symbol | ✅ |
| Inline Completion | ✅ |

## ⚡ Performance

- **Incremental updates**: ~2.7ms for 1000 transactions (NFR < 50ms)
- **Completion**: ~4.2ms response time (NFR < 100ms)
- **Parsing**: ~11ms for 10k transactions (NFR < 500ms)
- **Memory**: ~31MB for large journals (NFR < 200MB)

See [docs/benchmarks.md](docs/benchmarks.md) for detailed benchmarks.

## 📚 Resources

- [hledger Documentation](https://hledger.org/hledger.html)
- [LSP Specification](https://microsoft.github.io/language-server-protocol/)

## 📄 License

[MIT](LICENSE) © Denis Evsyukov
