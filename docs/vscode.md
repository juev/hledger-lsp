# VS Code Setup

## Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))

2. Install [vscode-lspconfig](https://marketplace.visualstudio.com/items?itemName=whtsht.vscode-lspconfig) extension for generic LSP support.

## Configuration

Add to your `settings.json`:

```json
{
  "vscode-lspconfig.serverConfigurations": [
    {
      "name": "hledger-lsp",
      "document_selector": [
        {"pattern": "**/*.journal"},
        {"pattern": "**/*.hledger"}
      ],
      "command": ["hledger-lsp"]
    }
  ]
}
```

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
