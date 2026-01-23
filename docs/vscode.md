# VS Code Setup

hledger-lsp provides LSP features for VS Code through two setup options:

| Feature | hledger-vscode | vscode-lspconfig |
|---------|----------------|------------------|
| Completions, diagnostics, formatting | ✓ | ✓ |
| Per-language semantic token colors | ✓ | ✗ (global only) |
| File association (.journal, .hledger) | ✓ | ✓ |
| Setup complexity | Lower | Higher |

## Option 1: hledger-vscode Extension (Recommended)

The [hledger-vscode](https://github.com/juev/hledger-vscode) extension provides the best integration with proper language ID registration.

### Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))
2. Install [hledger-vscode](https://marketplace.visualstudio.com/items?itemName=juev.hledger-vscode) from VS Code Marketplace

### Configuration

The extension works out of the box. Optional settings in `settings.json`:

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

## Option 2: vscode-lspconfig (Minimal Setup)

Use [vscode-lspconfig](https://marketplace.visualstudio.com/items?itemName=whtsht.vscode-lspconfig) for generic LSP support without installing a dedicated extension.

### Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))
2. Install [vscode-lspconfig](https://marketplace.visualstudio.com/items?itemName=whtsht.vscode-lspconfig)

### Configuration

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

**Limitation**: vscode-lspconfig does not register a language ID, so semantic token color customizations will apply globally to all languages (see [Semantic Token Colors](#semantic-token-colors)).

## Semantic Token Colors

hledger-lsp provides semantic highlighting for journal elements. You can customize colors in VS Code settings.

### Token Type Reference

| hledger Element | Token Type | Example |
|-----------------|------------|---------|
| Account | `namespace` | `expenses:food` |
| Date | `number` | `2024-01-15` |
| Amount | `number` | `50.00` |
| Commodity | `type` | `USD`, `$` |
| Payee | `function` | `grocery store` |
| Directive | `macro` | `account`, `include` |
| Code | `variable` | `(123)` |
| Tag | `property` | `tag:value` |
| Comment | `comment` | `; note` |
| Status | `operator` | `*`, `!` |

### Customizing Colors with hledger-vscode

With hledger-vscode, use the `:hledger` suffix to apply colors only to hledger files:

```json
{
  "editor.semanticTokenColorCustomizations": {
    "rules": {
      "namespace:hledger": "#4EC9B0",
      "number:hledger": "#B5CEA8",
      "type:hledger": "#569CD6",
      "comment:hledger": "#6A9955",
      "operator:hledger": "#D4D4D4",
      "string:hledger": "#CE9178",
      "function:hledger": "#DCDCAA",
      "property:hledger": "#9CDCFE",
      "macro:hledger": "#C586C0",
      "variable:hledger": "#9CDCFE"
    }
  }
}
```

### Customizing Colors with vscode-lspconfig

Without a registered language ID, colors apply globally:

```json
{
  "editor.semanticTokenColorCustomizations": {
    "rules": {
      "namespace": "#4EC9B0",
      "number": "#B5CEA8",
      "type": "#569CD6",
      "comment": "#6A9955",
      "operator": "#D4D4D4",
      "string": "#CE9178",
      "function": "#DCDCAA",
      "property": "#9CDCFE",
      "macro": "#C586C0",
      "variable": "#9CDCFE"
    }
  }
}
```

**Warning**: These rules affect all languages. To avoid conflicts, you may want to customize only distinctive tokens like `macro` and `namespace`.

## Enable Format on Type

To enable auto-indentation after Enter and Tab alignment for amounts:

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

## Debugging Semantic Tokens

To verify semantic tokens are working:

1. Open a `.journal` file
2. Run command: **Developer: Inspect Editor Tokens and Scopes** (Ctrl+Shift+P / Cmd+Shift+P)
3. Click on any token in the editor
4. Check the **semantic token type** field in the popup

If no semantic token type appears, the LSP server may not be running or semantic tokens are not enabled.

## Verify Setup

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

**Semantic highlighting not working:**

- Use **Developer: Inspect Editor Tokens and Scopes** to debug
- Check Output panel for LSP errors
- Restart VS Code after configuration changes
