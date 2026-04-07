# Neovim Setup

## Prerequisites

- Neovim **0.12+** (recommended — full feature support including on-type formatting)
- Neovim 0.11 (supported — native LSP works, no on-type formatting)
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

A complete setup needs three pieces — and the Tab piece requires a custom keymap because of an LSP protocol limitation explained at the end of this section.

### 1. Auto-indent after `o` and `<CR>` (`indentexpr`)

`vim.lsp.on_type_formatting` handles `<CR>` (Enter) typed inside insert mode, but it does **not** fire on the `o` normal-mode command (which inserts a newline at the C level, never reaching `vim.on_key`). Without a Vim-side indent rule, `o` after a transaction header drops you on an unindented line. Set `indentexpr` so new posting lines start indented:

```lua
_G.hledger_indentexpr = function()
  local lnum = vim.v.lnum
  if lnum <= 1 then return 0 end
  local prev = vim.fn.getline(lnum - 1)
  -- Transaction header (date), periodic (~), auto-rule (=)
  if prev:match("^[%d~=]") then return vim.bo.shiftwidth end
  -- Existing posting (whitespace + content)
  if prev:match("^%s+%S") then return vim.bo.shiftwidth end
  return 0
end

vim.api.nvim_create_autocmd("FileType", {
  pattern = "hledger",
  callback = function(args)
    vim.bo[args.buf].autoindent = true
    vim.bo[args.buf].indentexpr = "v:lua.hledger_indentexpr()"
  end,
})
```

### 2. Enable native `onTypeFormatting` for Enter

Neovim 0.12 added native `textDocument/onTypeFormatting` support ([PR #34637](https://github.com/neovim/neovim/pull/34637)). Enable it explicitly per-client — it is **not active by default**:

```lua
vim.api.nvim_create_autocmd("LspAttach", {
  callback = function(args)
    local client = vim.lsp.get_client_by_id(args.data.client_id)
    if client and client.name == "hledger_lsp" then
      vim.lsp.on_type_formatting.enable(true, { client_id = args.data.client_id })
    end
  end,
})
```

See `:h lsp-on_type_formatting` for the API. With this, pressing `<CR>` at the end of a posting line inserts a new properly-indented line.

### 3. Custom Tab keymap for amount alignment

Tab alignment cannot use the native `vim.lsp.on_type_formatting` pipeline. The LSP `textDocument/onTypeFormatting` response is a `TextEdit[]` with no field for cursor position ([LSP spec](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_onTypeFormatting), [microsoft/language-server-protocol#724](https://github.com/microsoft/language-server-protocol/issues/724)). The server can insert the alignment spaces, but the client has no standardized way to learn that it should jump the cursor to the end of the inserted text. The native module applies the edit and leaves the cursor where it was — visually it looks like Tab "did nothing".

Use an insert-mode keymap that calls the LSP request synchronously and explicitly moves the cursor afterwards:

```lua
local function hledger_tab_fallback()
  local key = vim.api.nvim_replace_termcodes("<Tab>", true, false, true)
  vim.api.nvim_feedkeys(key, "n", false)
end

local function hledger_tab()
  local bufnr = vim.api.nvim_get_current_buf()
  local clients = vim.lsp.get_clients({ bufnr = bufnr, name = "hledger_lsp" })
  if #clients == 0 then return hledger_tab_fallback() end
  local client = clients[1]

  local pos = vim.api.nvim_win_get_cursor(0)
  local params = {
    textDocument = vim.lsp.util.make_text_document_params(),
    position = { line = pos[1] - 1, character = pos[2] },
    ch = "\t",
    options = { tabSize = vim.bo.tabstop, insertSpaces = vim.bo.expandtab },
  }
  local resp = client:request_sync("textDocument/onTypeFormatting", params, 500, bufnr)
  if not resp or not resp.result or #resp.result == 0 then
    return hledger_tab_fallback()
  end

  vim.lsp.util.apply_text_edits(resp.result, bufnr, client.offset_encoding)

  local edit = resp.result[#resp.result]
  local new_col = edit.range.start.character + #edit.newText
  vim.api.nvim_win_set_cursor(0, { pos[1], new_col })
end

vim.api.nvim_create_autocmd("FileType", {
  pattern = "hledger",
  callback = function(args)
    vim.keymap.set("i", "<Tab>", hledger_tab, { buffer = args.buf })
  end,
})
```

The keymap captures the cursor position **before** Tab is processed (matching the position the server expects), sends a synchronous `onTypeFormatting` request, applies the returned edit, and moves the cursor to `start + len(newText)`. If the LSP is unavailable or returns no edit, it falls back to a literal `<Tab>` (which `expandtab` will convert to spaces).

> **Note (completion plugins):** This keymap intercepts every `<Tab>` in insert mode for `hledger` filetype. If you use a completion plugin (nvim-cmp, blink.cmp, etc.) that maps `<Tab>` to navigate the completion menu, integrate accordingly — for example, only call `hledger_tab()` when the completion popup is not visible, otherwise delegate to the completion plugin.

### Older Neovim

`textDocument/onTypeFormatting` requires Neovim 0.12+. On 0.11 the LSP server still works (completions, diagnostics, hover, formatting on demand), but Enter auto-indent and Tab alignment are not available.

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
