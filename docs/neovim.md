# Neovim Setup

## Prerequisites

- Neovim 0.8+
- [nvim-lspconfig](https://github.com/neovim/nvim-lspconfig)

## Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))

2. Add LSP configuration to your Neovim config:

### Using lazy.nvim

```lua
{
  "neovim/nvim-lspconfig",
  config = function()
    local lspconfig = require("lspconfig")
    local configs = require("lspconfig.configs")

    if not configs.hledger_lsp then
      configs.hledger_lsp = {
        default_config = {
          cmd = { "hledger-lsp" },
          filetypes = { "hledger", "journal" },
          root_dir = lspconfig.util.root_pattern(".git", "*.journal"),
          single_file_support = true,
        },
      }
    end

    lspconfig.hledger_lsp.setup({})
  end,
}
```

### Using init.lua directly

```lua
local lspconfig = require("lspconfig")
local configs = require("lspconfig.configs")

if not configs.hledger_lsp then
  configs.hledger_lsp = {
    default_config = {
      cmd = { "hledger-lsp" },
      filetypes = { "hledger", "journal" },
      root_dir = lspconfig.util.root_pattern(".git", "*.journal"),
      single_file_support = true,
    },
  }
end

lspconfig.hledger_lsp.setup({})
```

## Filetype Detection

Add to your config:

```lua
vim.filetype.add({
  extension = {
    journal = "hledger",
    hledger = "hledger",
  },
})
```

## Semantic Token Highlighting

hledger-lsp uses custom semantic token types. Add highlight links to your config:

```lua
vim.api.nvim_set_hl(0, "@lsp.type.account.hledger", { link = "Identifier" })
vim.api.nvim_set_hl(0, "@lsp.type.commodity.hledger", { link = "Type" })
vim.api.nvim_set_hl(0, "@lsp.type.payee.hledger", { link = "Function" })
vim.api.nvim_set_hl(0, "@lsp.type.date.hledger", { link = "Number" })
vim.api.nvim_set_hl(0, "@lsp.type.amount.hledger", { link = "Number" })
vim.api.nvim_set_hl(0, "@lsp.type.directive.hledger", { link = "PreProc" })
vim.api.nvim_set_hl(0, "@lsp.type.code.hledger", { link = "Special" })
vim.api.nvim_set_hl(0, "@lsp.type.status.hledger", { link = "Operator" })
```

Or with custom colors:

```lua
vim.api.nvim_set_hl(0, "@lsp.type.account.hledger", { fg = "#4EC9B0" })
vim.api.nvim_set_hl(0, "@lsp.type.commodity.hledger", { fg = "#569CD6" })
vim.api.nvim_set_hl(0, "@lsp.type.payee.hledger", { fg = "#DCDCAA" })
vim.api.nvim_set_hl(0, "@lsp.type.date.hledger", { fg = "#B5CEA8" })
vim.api.nvim_set_hl(0, "@lsp.type.amount.hledger", { fg = "#B5CEA8" })
vim.api.nvim_set_hl(0, "@lsp.type.directive.hledger", { fg = "#C586C0" })
vim.api.nvim_set_hl(0, "@lsp.type.code.hledger", { fg = "#9CDCFE" })
vim.api.nvim_set_hl(0, "@lsp.type.status.hledger", { fg = "#D4D4D4" })
```

## Keybindings

Recommended keybindings for LSP features:

```lua
vim.api.nvim_create_autocmd("LspAttach", {
  callback = function(args)
    local opts = { buffer = args.buf }
    vim.keymap.set("n", "K", vim.lsp.buf.hover, opts)
    vim.keymap.set("n", "gd", vim.lsp.buf.definition, opts)
    vim.keymap.set("n", "<leader>f", vim.lsp.buf.format, opts)
    vim.keymap.set("n", "<leader>ca", vim.lsp.buf.code_action, opts)
  end,
})
```

## Verify

1. Open a `.journal` file
2. Run `:LspInfo` — should show hledger_lsp attached
3. Type an account name and trigger completion (`<C-x><C-o>` or your completion plugin)

## Troubleshooting

**LSP not attaching:**
- Check `:LspLog` for errors
- Verify filetype with `:set ft?`
- Ensure `hledger-lsp` is in PATH

**No completions:**
- Check if completion plugin is configured (nvim-cmp, etc.)
- Try manual completion with `<C-x><C-o>`
