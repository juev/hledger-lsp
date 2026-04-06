# Neovim Setup

## Prerequisites

- Neovim 0.11+ (recommended) or Neovim 0.8+
- `hledger-lsp` binary in PATH (see [main README](../README.md#-installation))

## Neovim 0.11+ (recommended)

Neovim 0.11 introduced native LSP configuration via `vim.lsp.config()` and `vim.lsp.enable()`. No plugins required.

### Option A: lsp/ directory (cleanest)

Create `~/.config/nvim/lsp/hledger_lsp.lua`:

```lua
return {
  cmd = { "hledger-lsp" },
  filetypes = { "hledger", "journal" },
  root_markers = { ".git", "*.journal" },
  single_file_support = true,
}
```

Then enable it in your `init.lua`:

```lua
vim.lsp.enable("hledger_lsp")
```

### Option B: inline in init.lua

```lua
vim.lsp.config("hledger_lsp", {
  cmd = { "hledger-lsp" },
  filetypes = { "hledger", "journal" },
  root_markers = { ".git", "*.journal" },
  single_file_support = true,
})

vim.lsp.enable("hledger_lsp")
```

## Legacy (Neovim < 0.11)

For older Neovim versions, use [nvim-lspconfig](https://github.com/neovim/nvim-lspconfig):

<details>
<summary>Using lazy.nvim</summary>

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

</details>

<details>
<summary>Using init.lua directly</summary>

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

</details>

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

Neovim 0.11+ includes default LSP keymaps (`grn` rename, `gra` code action, `grr` references). For additional bindings:

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

## Format on Type

hledger-lsp registers Enter and Tab as trigger characters for `textDocument/onTypeFormatting`:

- **Enter**: auto-indents new posting lines after transaction headers or existing postings
- **Tab**: aligns cursor to the amount column after an account name

### Neovim 0.12+

Neovim 0.12 added native `textDocument/onTypeFormatting` support ([PR #34637](https://github.com/neovim/neovim/pull/34637)). Enable it explicitly — it is **not active by default**:

```lua
vim.lsp.formatting.enable()
```

See `:h lsp-on_type_formatting` for options.

### Neovim 0.11

Native onTypeFormatting is not available, but you can set up manual keymaps that call `vim.lsp.buf.format()` with a trigger character:

```lua
vim.api.nvim_create_autocmd("LspAttach", {
  callback = function(args)
    local client = vim.lsp.get_client_by_id(args.data.client_id)
    if not client or not client.supports_method("textDocument/onTypeFormatting") then
      return
    end

    -- Auto-indent after Enter
    vim.keymap.set("i", "<CR>", function()
      -- Insert newline first, then request formatting
      local key = vim.api.nvim_replace_termcodes("<CR>", true, false, true)
      vim.api.nvim_feedkeys(key, "n", false)
      vim.schedule(function()
        vim.lsp.buf.format({ trigger_character = "\n" })
      end)
    end, { buffer = args.buf })

    -- Align amount after Tab
    vim.keymap.set("i", "<Tab>", function()
      vim.lsp.buf.format({ trigger_character = "\t" })
    end, { buffer = args.buf })
  end,
})
```

> **Note:** These keymaps may conflict with completion plugins (nvim-cmp, etc.).
> Adjust or conditionally enable them based on your setup.

### Neovim < 0.11

`onTypeFormatting` is not supported. Enter auto-indent and Tab alignment are not available.

## Verify

1. Open a `.journal` file
2. Run `:checkhealth vim.lsp` — should show hledger_lsp attached
3. Type an account name and trigger completion (`<C-x><C-o>` or your completion plugin)

## Troubleshooting

**LSP not attaching:**
- Run `:checkhealth vim.lsp` and check for errors
- Verify filetype with `:set ft?`
- Ensure `hledger-lsp` is in PATH

**No completions:**
- Check if completion plugin is configured (nvim-cmp, etc.)
- Try manual completion with `<C-x><C-o>`
