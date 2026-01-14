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

## Keybindings

With eglot, standard keybindings work:

| Key | Action |
|-----|--------|
| `C-c C-d` | Show documentation (hover) |
| `M-.` | Go to definition |
| `C-c C-r` | Rename symbol |
| `C-c C-f` | Format buffer |

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

**Wrong major mode:**
- Verify with `M-x describe-mode`
- Add file association if needed
