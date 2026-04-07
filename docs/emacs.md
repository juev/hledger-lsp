# Emacs Setup

## Prerequisites

- Emacs 29+ (with built-in eglot) or Emacs 26+ with eglot package
- hledger-mode (optional, for syntax highlighting)

## Installation

1. Install hledger-lsp binary (see [main README](../README.md#-installation))

2. Configure eglot:

### Using use-package

```elisp
(use-package eglot
  :ensure t
  :hook ((hledger-mode . eglot-ensure))
  :config
  (add-to-list 'eglot-server-programs
               '(hledger-mode . ("hledger-lsp"))))

;; Optional: hledger-mode for syntax highlighting
(use-package hledger-mode
  :ensure t
  :mode ("\\.journal\\'" "\\.hledger\\'"))
```

### Without use-package

```elisp
(require 'eglot)

;; Associate hledger-lsp with journal files
(add-to-list 'eglot-server-programs
             '(hledger-mode . ("hledger-lsp")))

;; Auto-start eglot for hledger files
(add-hook 'hledger-mode-hook 'eglot-ensure)

;; File associations
(add-to-list 'auto-mode-alist '("\\.journal\\'" . hledger-mode))
(add-to-list 'auto-mode-alist '("\\.hledger\\'" . hledger-mode))
```

### Using lsp-mode (alternative)

```elisp
(use-package lsp-mode
  :ensure t
  :hook ((hledger-mode . lsp-deferred))
  :config
  (lsp-register-client
   (make-lsp-client
    :new-connection (lsp-stdio-connection '("hledger-lsp"))
    :major-modes '(hledger-mode)
    :server-id 'hledger-lsp)))
```

## Semantic Token Highlighting

hledger-lsp uses custom semantic token types. Configure faces for highlighting:

### Using eglot (Emacs 29+)

```elisp
(defface hledger-account-face '((t :inherit font-lock-variable-name-face)) "Face for accounts.")
(defface hledger-commodity-face '((t :inherit font-lock-type-face)) "Face for commodities.")
(defface hledger-payee-face '((t :inherit font-lock-function-name-face)) "Face for payees.")
(defface hledger-date-face '((t :inherit font-lock-constant-face)) "Face for dates.")
(defface hledger-amount-face '((t :inherit font-lock-constant-face)) "Face for amounts.")
(defface hledger-directive-face '((t :inherit font-lock-preprocessor-face)) "Face for directives.")
(defface hledger-code-face '((t :inherit font-lock-string-face)) "Face for codes.")
(defface hledger-status-face '((t :inherit font-lock-builtin-face)) "Face for status.")

(setq eglot-semantic-token-faces
      '((account . hledger-account-face)
        (commodity . hledger-commodity-face)
        (payee . hledger-payee-face)
        (date . hledger-date-face)
        (amount . hledger-amount-face)
        (directive . hledger-directive-face)
        (code . hledger-code-face)
        (status . hledger-status-face)))
```

### Using lsp-mode

```elisp
(setq lsp-semantic-tokens-apply-modifiers nil)

(defface lsp-face-semhl-account '((t :inherit font-lock-variable-name-face)) "Face for accounts.")
(defface lsp-face-semhl-commodity '((t :inherit font-lock-type-face)) "Face for commodities.")
(defface lsp-face-semhl-payee '((t :inherit font-lock-function-name-face)) "Face for payees.")
(defface lsp-face-semhl-date '((t :inherit font-lock-constant-face)) "Face for dates.")
(defface lsp-face-semhl-amount '((t :inherit font-lock-constant-face)) "Face for amounts.")
(defface lsp-face-semhl-directive '((t :inherit font-lock-preprocessor-face)) "Face for directives.")
(defface lsp-face-semhl-code '((t :inherit font-lock-string-face)) "Face for codes.")
(defface lsp-face-semhl-status '((t :inherit font-lock-builtin-face)) "Face for status.")
```

## Keybindings

With eglot, standard keybindings work:

| Key | Action |
|-----|--------|
| `C-c C-d` | Show documentation (hover) |
| `M-.` | Go to definition |
| `C-c C-r` | Rename symbol |
| `C-c C-f` | Format buffer |

## Format on Type

hledger-lsp registers Enter and Tab as trigger characters for `textDocument/onTypeFormatting`:

- **Enter**: auto-indents new posting lines after transaction headers or existing postings
- **Tab**: aligns cursor to the amount column after an account name

### Using eglot

**Enter** works automatically — eglot sends `onTypeFormatting` via `post-self-insert-hook`, which fires for regular character insertion including newlines.

**Tab requires a custom keybinding.** Eglot triggers `onTypeFormatting` through `post-self-insert-hook`, but Tab in Emacs is handled by `indent-for-tab-command`, not `self-insert-command`. The hook never fires for Tab, so without a workaround Tab inserts a literal `\t` instead of triggering LSP alignment.

Add this to your configuration:

```elisp
(defun hledger-align-amount ()
  "Send Tab to LSP for amount alignment."
  (interactive)
  (eglot-format nil nil "\t"))

(define-key hledger-mode-map (kbd "TAB") #'hledger-align-amount)
```

### Using lsp-mode

lsp-mode supports `onTypeFormatting` via `lsp-enable-on-type-formatting`. Ensure it is enabled:

```elisp
(setq lsp-enable-on-type-formatting t)
```

Tab may conflict with `company-mode` or `electric-indent-mode`. A custom keybinding may be needed if Tab does not trigger alignment.

## Configuration

Pass settings to hledger-lsp via `eglot-workspace-configuration`. Without this, the server uses defaults and `workspace/configuration` requests return `null`.

```elisp
(setq-default eglot-workspace-configuration
  '(:hledger
    (:formatting (:indentSize 4 :alignAmounts t :minAlignmentColumn 0
                  :amountAlignmentMode "right")
     :completion (:maxResults 100 :fuzzyMatching t :includeNotes t)
     :diagnostics (:undeclaredAccounts t :unbalancedTransactions t))))
```

By default amounts are aligned to the file's natural column. To pin amounts to a fixed minimum column (e.g. 50):

```elisp
(setq-default eglot-workspace-configuration
  '(:hledger (:formatting (:minAlignmentColumn 50))))
```

See [configuration.md](configuration.md) for the full list of settings.

## Verify

1. Open a `.journal` file
2. Run `M-x eglot` if not auto-started
3. Check `*eglot events*` buffer for connection status
4. Start typing — completions should appear

## Troubleshooting

**Eglot not connecting:**
- Check `*eglot events*` buffer for errors
- Verify hledger-lsp is in PATH: `M-! which hledger-lsp`
- Try `M-x eglot-reconnect`

**No completions:**
- Ensure company-mode or corfu is enabled
- Check if eglot is active: `M-x eglot-ensure`

**Tab inserts `\t` instead of aligning amounts:**
- Eglot does not send `onTypeFormatting` for Tab (see [Format on Type](#format-on-type))
- Add the `hledger-align-amount` keybinding from the section above

**Settings not applied (server uses defaults):**
- Set `eglot-workspace-configuration` (see [Configuration](#configuration))
- Verify with `*eglot events*` — look for `workspace/configuration` response

**Wrong major mode:**
- Verify with `M-x describe-mode`
- Add file association if needed
