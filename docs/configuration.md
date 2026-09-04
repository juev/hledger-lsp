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
| `hledger.features.inlineCompletion` | `true` | Ghost text completions for transaction templates |
| `hledger.features.codeLens` | `false` | Balance check indicators on transactions |
| `hledger.features.inlayHints` | `true` | Inlay hints for inferred amounts and optional balance facts |

Inline completion chooses the posting pattern used most often in the last 50
transactions for the payee. Suggestions refresh after document edits and
watched journal-file changes; saving is not required.

## Inlay hints

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.inlayHints.inferredAmounts` | `true` | Show the inferred amount for a single elided posting |
| `hledger.inlayHints.runningBalances` | `false` | Show the account balance after each posting |
| `hledger.inlayHints.costExpansion` | `false` | Show the signed balancing contribution of `@` and `@@` costs |

Running balances are omitted when include ownership or occurrence is ambiguous,
or a transaction contains a posting-level `date:` or `date2:` tag.

## Completion

| Setting | Default | Description |
|---------|---------|-------------|
| `hledger.completion.maxResults` | `50` | Maximum number of completion items |
| `hledger.completion.fuzzyMatching` | `true` | Enable fuzzy matching |
| `hledger.completion.showCounts` | `true` | Show usage counts in completion details |
| `hledger.completion.includeNotes` | `true` | Include notes in payee completions |

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
| `hledger.formatting.minAlignmentColumn` | `0` | Minimum column floor for amount alignment. `0` = auto from file content (`indent + longest account + 2 spaces`). Set to a positive integer to enforce a minimum column for visual consistency across files. |
| `hledger.formatting.amountAlignmentColumn` | `0` | Fixed mode-specific alignment target. In `"left"` mode this is the amount start column; in `"right"` mode this is the amount end column; in `"decimal"` mode this is the decimal-point column. `0` disables the fixed target. |
| `hledger.formatting.amountAlignmentMode` | `"right"` | Amount alignment mode: `"left"` (align amount starts), `"right"` (right-align amounts), or `"decimal"` (align on decimal point) |
| `hledger.formatting.amountAlignmentTarget` | `"cost"` | For postings with cost notation (`@`/`@@`), which amount anchors alignment: `"cost"` (the cost/second amount — hledger 1.x behavior) or `"posting"` (the posting/first amount — hledger 2.x `print` behavior; lot price and cost annotation then trail freely). |

### Pin amounts to a fixed column (opt-in)

By default the server uses the file's natural alignment column (computed from the longest account name). If you prefer all amounts to be aligned to a fixed column for visual consistency across files, set `minAlignmentColumn` to your desired value:

```json
{
  "hledger.formatting.minAlignmentColumn": 40
}
```

```lua
formatting = {
  minAlignmentColumn = 40,
}
```

```elisp
:formatting (:minAlignmentColumn 40)
```

The setting acts as a **floor**: if a file has accounts longer than the specified column would accommodate, the alignment column shifts further right to fit them.

Use `amountAlignmentColumn` when you want a fixed target column instead of a floor:

```json
{
  "hledger.formatting.amountAlignmentMode": "right",
  "hledger.formatting.amountAlignmentColumn": 80
}
```

With `"right"`, amounts end at the configured column. With `"decimal"`, decimal points align at the configured column:

```json
{
  "hledger.formatting.amountAlignmentMode": "decimal",
  "hledger.formatting.amountAlignmentColumn": 60
}
```

With `"left"`, amount starts align at the configured column:

```json
{
  "hledger.formatting.amountAlignmentMode": "left",
  "hledger.formatting.amountAlignmentColumn": 40
}
```

### Cost-notation alignment target

A posting with cost notation has two amounts (`2.36 EUR @@ 3.12 USD`). `amountAlignmentTarget` selects which one anchors alignment. With the default `"cost"`, the cost (second) amount is aligned:

```text
2000-01-01
    assets:investments  2.36 EUR @@ 3.12 USD
    assets:cash                    -3.12 USD
```

With `"posting"`, the posting (first) amount is aligned and the cost annotation trails freely (matches hledger 2.x `print`):

```json
{
  "hledger.formatting.amountAlignmentMode": "decimal",
  "hledger.formatting.amountAlignmentTarget": "posting"
}
```

```text
2000-01-01
    assets:investments   2.36 EUR @@ 3.12 USD
    assets:cash         -3.12 USD
```

`"posting"` keeps the anchor short, so amounts still align even when a long account name would otherwise push the cost-anchored amount against the 2-space minimum.

The same alignment settings are used by document/range formatting, format-on-type for Enter after a posting line, and inline completion ghost text when amount data is available.

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
  "hledger.features.inlineCompletion": true,
  "hledger.features.codeLens": false,
  "hledger.features.inlayHints": true,
  "hledger.inlayHints.inferredAmounts": true,
  "hledger.inlayHints.runningBalances": false,
  "hledger.inlayHints.costExpansion": false,
  "hledger.completion.maxResults": 100,
  "hledger.completion.fuzzyMatching": true,
  "hledger.completion.includeNotes": true,
  "hledger.diagnostics.undeclaredAccounts": true,
  "hledger.diagnostics.unbalancedTransactions": true,
  "hledger.formatting.indentSize": 4,
  "hledger.formatting.alignAmounts": true,
  "hledger.formatting.minAlignmentColumn": 0,
  "hledger.formatting.amountAlignmentColumn": 0,
  "hledger.formatting.amountAlignmentMode": "right",
  "hledger.formatting.amountAlignmentTarget": "cost",
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
        inlineCompletion = true,
        codeLens = false,
        inlayHints = true,
      },
      inlayHints = {
        inferredAmounts = true,
        runningBalances = false,
        costExpansion = false,
      },
      completion = {
        maxResults = 100,
        fuzzyMatching = true,
        showCounts = true,
        includeNotes = true,
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
        amountAlignmentColumn = 0,
        amountAlignmentMode = "right",
        amountAlignmentTarget = "cost",
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
                :documentLinks t :workspaceSymbol t :inlineCompletion t
                :codeLens nil :inlayHints t)
     :inlayHints (:inferredAmounts t :runningBalances nil :costExpansion nil)
     :completion (:maxResults 100 :fuzzyMatching t :showCounts t :includeNotes t)
     :diagnostics (:undeclaredAccounts t :undeclaredCommodities t
                   :unbalancedTransactions t)
     :formatting (:indentSize 4 :alignAmounts t :minAlignmentColumn 0
                  :amountAlignmentColumn 0
                  :amountAlignmentMode "right"
                  :amountAlignmentTarget "cost")
     :cli (:enabled t :path "hledger" :timeout 30000)
     :limits (:maxFileSizeBytes 20971520 :maxIncludeDepth 100))))
```
