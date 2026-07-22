# Workspace Mode

## How hledger CLI Finds Journals

hledger itself uses a simple priority chain:

1. `-f/--file` flag (explicit)
2. `LEDGER_FILE` environment variable
3. `$HOME/.hledger.journal` (default fallback)

There is no folder scanning or project discovery — hledger always works
with one explicitly configured file.

## How hledger-lsp Extends This

The LSP server operates in two modes depending on how the editor opens files.

### Single-file mode

Activated when the editor opens a file directly (no workspace folders).

- `workspace = nil`
- The opened file is parsed, its `include` directives are resolved
  recursively via `loader.LoadFromContent()`
- Hover, completion, diagnostics use transactions from the file and all
  its includes
- Each `publishDiagnostics` call re-resolves includes from disk

### Workspace mode

Activated when the editor opens a **folder** (`workspaceFolders` in LSP
`initialize` request).

- A `Workspace` struct is created with `rootURI` = opened folder path
- On `initialized`, the workspace discovers the root journal and loads
  the full include tree once

## Root Journal Discovery (workspace mode)

Search order within the opened folder:

1. `main.journal` at the workspace root
2. `.hledger.journal` at the workspace root
3. Scan all `*.journal`, `*.j`, `*.hledger`, `*.ledger`, `*.prices` files →
   build include graph → pick the file with no incoming edges (not
   included by anything else)

**Environment variables are intentionally ignored.** `LEDGER_FILE` and
`HLEDGER_JOURNAL` are for the hledger CLI, not for the LSP server.
The LSP server works only with what the editor provides: a file or a folder.

If no root journal is found, `resolved` stays nil and each opened file
is handled independently (same as single-file mode).

## Loading and Resolution

```text
workspace.Initialize()
  └─ findRootJournal()          → discovers root path
  └─ loader.Load(rootPath)      → reads root, recursively loads includes
  └─ buildIndexFromResolved()   → builds aggregate workspace index

ResolvedJournal:
  ├── Occurrences[]     per-include-site ASTs (repeated/diamond OK)
  ├── Items[]           textual-inline stream (parent→child→parent)
  ├── ByPath            exact source path → occurrence IDs
  ├── ByCanonical       canonical (symlink-resolved) path → IDs
  └── Errors[]          load errors with source provenance
```

`AllTransactions()` walks `Items` in textual-inline order, returning
transactions from every occurrence. Repeated includes are counted per
occurrence (hledger 1.52.1 parity). Navigation (references, definition)
deduplicates by unique `(URI, range)` source location.

## Document Updates

### DidOpen

Stores normalized content (`\r\n` → `\n`), publishes diagnostics
asynchronously. Does **not** update workspace — initial load already
has the data.

### DidChange

Applies incremental edits, normalizes line endings, then:

- `workspace.UpdateFile(path, content)` — re-parses file, updates
  resolved journal, rebuilds index
- If include list changed → `refreshIncludeTreeLocked()`:
  BFS from root → remove unreachable files → load newly reachable
  files from disk

### DidSave

Same as DidChange but reads from disk as fallback if document not in
editor buffer. Invalidates loader cache.

### DidChangeWatchedFiles

For files **not** open in the editor: reads from disk with CRLF
normalization, calls `workspace.UpdateFile()`. Triggers diagnostic
republish for parent files that include the changed file.

## Query Fallback Chain

```text
getWorkspaceResolved(docURI):
  if workspace != nil && workspace.GetResolved() != nil
    → return workspace resolved (all files aggregated)
  else
    → return per-document resolved (LoadFromContent, single file + includes)
```

Hover, completion, references, rename — all use this chain. Workspace
mode gives multi-file context; single-file mode works as fallback.

## CRLF Normalization

All paths that read files from disk normalize `\r\n` → `\n`:

- `loader.Load()` and `loadSingleInclude()` — include loading
- `workspace.buildIncludeGraph()` — root detection scan
- `workspace.addMissingReachableLocked()` — dynamic include discovery
- `DidOpen` / `DidChange` — editor content
- `DidSave` / `DidChangeWatchedFiles` — disk reads

The parser assumes `\n`-only input. Missing normalization on any disk
read path causes broken parsing on Windows.

## Error Handling

| Situation | Behavior |
|---|---|
| Self-include | Silently ignored (hledger 1.52.1 parity) |
| Ancestor cycle | Active canonical stack detects back-edge, reported as diagnostic |
| Diamond include | Two occurrences produced (no dedup, hledger 1.52.1 parity) |
| Missing include file | Reported as error diagnostic, other files still loaded |
| Include depth > 50 | Edge-based depth counting, reports `ErrorDepthExceeded` |
| File > 10 MB | Skipped, reported as error |
| Parse errors in include | File still added to resolved, errors shown as diagnostics |
| No journal in folder | `resolved = nil`, falls back to per-document mode |
| Multi-owner reload | Edit of shared source reloads all owning roots atomically |
