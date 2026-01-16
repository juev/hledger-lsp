# Configuration

The server reads settings from the `hledger` section of your LSP client configuration.

## Limits

- `hledger.limits.maxFileSizeBytes` (default: `10485760`)  
  Maximum journal file size in bytes used by the include loader.
- `hledger.limits.maxIncludeDepth` (default: `50`)  
  Maximum include depth for recursive loading.

## Completion

- `hledger.completion.maxResults` (default: `50`)
  Maximum number of completion items returned.

## Editor Examples

### VS Code (settings.json)

```json
{
  "hledger.completion.maxResults": 100,
  "hledger.limits.maxFileSizeBytes": 20971520,
  "hledger.limits.maxIncludeDepth": 100
}
```

### Neovim (nvim-lspconfig)

```lua
lspconfig.hledger_lsp.setup({
  settings = {
    hledger = {
      completion = { maxResults = 100 },
      limits = {
        maxFileSizeBytes = 20971520,
        maxIncludeDepth = 100
      }
    }
  }
})
```

### Emacs (eglot)

```elisp
(setq-default eglot-workspace-configuration
  '(:hledger (:completion (:maxResults 100)
              :limits (:maxFileSizeBytes 20971520
                       :maxIncludeDepth 100))))
```
