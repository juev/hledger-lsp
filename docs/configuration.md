# Configuration

The server reads settings from the `hledger` section of your LSP client configuration.

## Features

Enable or disable specific LSP features.

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.features.hover` | `true` | Hover information |
| `hledger.features.completion` | `true` | Completions |
| `hledger.features.formatting` | `true` | Document formatting |
| `hledger.features.diagnostics` | `true` | Diagnostics |
| `hledger.features.semanticTokens` | `true` | Semantic tokens |
| `hledger.features.codeActions` | `true` | Code actions |
| `hledger.features.foldingRanges` | `true` | Folding ranges for transactions and directives |
| `hledger.features.documentLinks` | `true` | Clickable links for include directives |
| `hledger.features.workspaceSymbol` | `true` | Workspace symbol search |
| `hledger.features.inlineCompletion` | `false` | Ghost text completions for transaction templates |

## Completion

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.completion.maxResults` | `50` | Maximum number of completion items |
| `hledger.completion.fuzzyMatching` | `true` | Enable fuzzy matching |
| `hledger.completion.showCounts` | `true` | Show usage counts in completion details |

## Diagnostics

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.diagnostics.undeclaredAccounts` | `true` | Report undeclared accounts |
| `hledger.diagnostics.undeclaredCommodities` | `true` | Report undeclared commodities |
| `hledger.diagnostics.unbalancedTransactions` | `true` | Report unbalanced transactions |

## Formatting

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.formatting.indentSize` | `4` | Number of spaces for posting indent |
| `hledger.formatting.alignAmounts` | `true` | Align amounts across postings |
| `hledger.formatting.minAlignmentColumn` | `0` | Minimum column for amount alignment (0 = no minimum) |

## CLI

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.cli.enabled` | `true` | Enable hledger CLI integration |
| `hledger.cli.path` | `"hledger"` | Path to hledger executable |
| `hledger.cli.timeout` | `30000` | CLI command timeout in milliseconds |

## Limits

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.limits.maxFileSizeBytes` | `10485760` | Maximum journal file size (bytes) |
| `hledger.limits.maxIncludeDepth` | `50` | Maximum include depth for recursive loading |

## Editor Examples

### VS Code (settings.json)

```json
{
  "hledger.features.hover": true,
  "hledger.features.completion": true,
  "hledger.features.formatting": true,
  "hledger.features.foldingRanges": true,
  "hledger.features.documentLinks": true,
  "hledger.features.workspaceSymbol": true,
  "hledger.features.inlineCompletion": false,
  "hledger.completion.maxResults": 100,
  "hledger.completion.fuzzyMatching": true,
  "hledger.diagnostics.undeclaredAccounts": true,
  "hledger.diagnostics.unbalancedTransactions": true,
  "hledger.formatting.indentSize": 4,
  "hledger.formatting.alignAmounts": true,
  "hledger.formatting.minAlignmentColumn": 0,
  "hledger.cli.path": "hledger",
  "hledger.cli.timeout": 30000,
  "hledger.limits.maxFileSizeBytes": 20971520,
  "hledger.limits.maxIncludeDepth": 100
}
```

### Neovim (nvim-lspconfig)

```lua
lspconfig.hledger_lsp.setup({
  settings = {
    hledger = {
      features = {
        hover = true,
        completion = true,
        formatting = true,
        diagnostics = true,
        semanticTokens = true,
        codeActions = true,
        foldingRanges = true,
        documentLinks = true,
        workspaceSymbol = true,
        inlineCompletion = false,
      },
      completion = {
        maxResults = 100,
        fuzzyMatching = true,
        showCounts = true,
      },
      diagnostics = {
        undeclaredAccounts = true,
        undeclaredCommodities = true,
        unbalancedTransactions = true,
      },
      formatting = {
        indentSize = 4,
        alignAmounts = true,
        minAlignmentColumn = 0,
      },
      cli = {
        enabled = true,
        path = "hledger",
        timeout = 30000,
      },
      limits = {
        maxFileSizeBytes = 20971520,
        maxIncludeDepth = 100,
      },
    },
  },
})
```

### Emacs (eglot)

```elisp
(setq-default eglot-workspace-configuration
  '(:hledger
    (:features (:hover t :completion t :formatting t :diagnostics t
                :semanticTokens t :codeActions t :foldingRanges t
                :documentLinks t :workspaceSymbol t :inlineCompletion nil)
     :completion (:maxResults 100 :fuzzyMatching t :showCounts t)
     :diagnostics (:undeclaredAccounts t :undeclaredCommodities t
                   :unbalancedTransactions t)
     :formatting (:indentSize 4 :alignAmounts t :minAlignmentColumn 0)
     :cli (:enabled t :path "hledger" :timeout 30000)
     :limits (:maxFileSizeBytes 20971520 :maxIncludeDepth 100))))
```
