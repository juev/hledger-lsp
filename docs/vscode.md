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

hledger-lsp uses custom semantic token types for domain-specific highlighting:

| hledger Element | Token Type | Example |
|-----------------|------------|---------|
| Account | `account` | `expenses:food` |
| Date | `date` | `2024-01-15` |
| Amount | `amount` | `50.00` |
| Commodity | `commodity` | `USD`, `$` |
| Payee | `payee` | `grocery store` |
| Directive | `directive` | `account`, `include` |
| Code | `code` | `(123)` |
| Status | `status` | `*`, `!` |
| Tag | `tag` | `client:` |
| Tag Value | `tagValue` | `acme` (in `; client:acme`) |
| Comment | `comment` | `; note` |

### Customizing Colors with hledger-vscode

With hledger-vscode, use the `:hledger` suffix to apply colors only to hledger files:

```json
{
  "editor.semanticTokenColorCustomizations": {
    "rules": {
      "account:hledger": "#4EC9B0",
      "date:hledger": "#B5CEA8",
      "amount:hledger": "#B5CEA8",
      "commodity:hledger": "#569CD6",
      "payee:hledger": "#DCDCAA",
      "directive:hledger": "#C586C0",
      "code:hledger": "#9CDCFE",
      "status:hledger": "#D4D4D4",
      "tag:hledger": "#FF8C00",
      "tagValue:hledger": "#98FB98",
      "comment:hledger": "#6A9955"
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
      "account": "#4EC9B0",
      "date": "#B5CEA8",
      "amount": "#B5CEA8",
      "commodity": "#569CD6",
      "payee": "#DCDCAA",
      "directive": "#C586C0",
      "code": "#9CDCFE",
      "status": "#D4D4D4",
      "tag": "#FF8C00",
      "tagValue": "#98FB98",
      "comment": "#6A9955"
    }
  }
}
```

**Warning**: Since these are custom token types, they won't affect other languages. However, if other LSP servers use the same custom type names, conflicts may occur.

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
