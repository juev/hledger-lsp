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

Settings example:

```json
{
  "hledger": {
    "completion": {
      "maxResults": 50
    },
    "limits": {
      "maxFileSizeBytes": 10485760,
      "maxIncludeDepth": 50
    }
  }
}
```

### Enable Format on Type

To enable auto-indentation after Enter and Tab alignment for amounts, add:

```json
{
  "editor.formatOnType": true
}
```

Features enabled:
- **Enter after transaction header**: auto-indents new posting line
- **Enter after posting**: auto-indents next posting line
- **Tab after account name**: aligns cursor to amount column

Note: Tab alignment works only outside snippet mode. When editing a template snippet, Tab navigates between tabstops.

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

**Enter/Tab formatting not working:**
- Enable `"editor.formatOnType": true` in VS Code settings
- Check that the LSP server is running (Output panel → select hledger-lsp)
