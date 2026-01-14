# VS Code Setup

## Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))

2. Install a generic LSP client extension, for example [vscode-lsp-client](https://marketplace.visualstudio.com/items?itemName=APerezSilva.vscode-lsp-client) or configure manually.

## Configuration

Add to your `settings.json`:

```json
{
  "lsp-client.serverPath": "hledger-lsp",
  "lsp-client.languageId": "hledger",
  "lsp-client.fileExtensions": [".journal", ".hledger"]
}
```

Or if using a different LSP client, configure it to:
- Run `hledger-lsp` as the language server
- Associate with `.journal` and `.hledger` file extensions

## Alternative: hledger-vscode Extension

For a more integrated experience, consider using [hledger-vscode](https://github.com/juev/hledger-vscode) extension which provides additional features specific to VS Code.

## Verify

1. Open a `.journal` file
2. Start typing an account name
3. You should see completion suggestions

## Troubleshooting

**LSP not starting:**
- Check that `hledger-lsp` is in your PATH: `which hledger-lsp`
- Check VS Code Output panel for LSP errors

**No completions:**
- Ensure the file has `.journal` or `.hledger` extension
- Check that the LSP client is configured for these extensions
