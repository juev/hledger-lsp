# hledger-lsp

[![Go Version](https://img.shields.io/github/go-mod/go-version/juev/hledger-lsp)](https://go.dev/)
[![License](https://img.shields.io/github/license/juev/hledger-lsp)](LICENSE)
[![Release](https://img.shields.io/github/v/release/juev/hledger-lsp)](https://github.com/juev/hledger-lsp/releases)
[![codecov](https://codecov.io/gh/juev/hledger-lsp/branch/main/graph/badge.svg)](https://codecov.io/gh/juev/hledger-lsp)

A Language Server Protocol (LSP) implementation for [hledger](https://hledger.org/) journal files. Provides IDE features like completions, diagnostics, formatting, and more for any LSP-compatible editor.

## 🎯 Features

- **Completions** — Account names, payees, commodities with context-aware suggestions
- **Diagnostics** — Real-time error detection for unbalanced transactions, syntax errors
- **Formatting** — Automatic alignment of amounts and consistent indentation
- **Hover** — Account balances and transaction details on hover
- **Semantic Tokens** — Syntax highlighting for dates, accounts, amounts, comments
- **Document Symbols** — Navigate transactions and directives with outline view
- **Include Support** — Full support for `include` directives with cycle detection

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
| Go to Definition | 🔜 |
| Find References | 🔜 |

## 📚 Resources

- [hledger Documentation](https://hledger.org/hledger.html)
- [LSP Specification](https://microsoft.github.io/language-server-protocol/)

## 📄 License

[MIT](LICENSE) © Denis Evsyukov
