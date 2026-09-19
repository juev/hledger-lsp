# Account completion scopes

Clients can offer a short account list first, then include zero-balance and
unused declared accounts when the user asks. Standard `textDocument/completion`
continues to show only accounts with a nonzero balance in at least one commodity.

When completion is enabled, the initialize response advertises:

```json
{
  "experimental": {
    "hledgerCompletion": { "accountScope": true }
  }
}
```

Clients supporting this capability can send `hledger/completion` with the
standard document URI, zero-based UTF-16 position, optional completion context,
and a required `accountScope` of `nonzero` or `all`:

```json
{
  "textDocument": { "uri": "file:///home/user/main.journal" },
  "position": { "line": 12, "character": 11 },
  "context": { "triggerKind": 1 },
  "accountScope": "all"
}
```

The response wraps a standard CompletionList and, in account context, the
replacement range of the current account name. For example, given an unused
declaration `account assets:unused` and the prefix `    assets:` on line 12:

```json
{
  "completionList": {
    "isIncomplete": true,
    "items": [
      {
        "label": "assets:unused",
        "textEdit": {
          "range": {
            "start": { "line": 12, "character": 4 },
            "end": { "line": 12, "character": 11 }
          },
          "newText": "assets:unused"
        }
      }
    ]
  },
  "accountRange": {
    "start": { "line": 12, "character": 4 },
    "end": { "line": 12, "character": 11 }
  }
}
```

The example omits item ranking and resolve fields. Real items retain those
fields and use the standard `completionItem/resolve` method. `accountRange` is
present even if no account matches; it is absent in other completion contexts
and for documents the server has not opened.

- `nonzero` uses the same balance filter, ranking, and `completion.maxResults`
  limit as standard completion.
- `all` includes every matching known account from the loaded journal and its
  includes, without the account balance filter or result limit. Nonzero
  accounts precede zero-balance and unused accounts; each group retains its
  usual ranking. Balances still exclude the transaction being edited.
- Payee, date, commodity, tag, directive, and rules completions retain their
  standard behavior and limits regardless of scope.

Scope is local to each request; the server stores no expanded mode and does
not change settings. Clients decide how long to keep sending `all`. A missing
scope, an unsupported scope, a missing document URI, or malformed parameters
produces JSON-RPC `InvalidParams` (`-32602`). An unopened document returns an
empty list. Clients without this capability should use standard completion.

## Zero balances without a custom request

The experimental `hledger/completion` request with `accountScope: "all"` exists for extensions that
stage their own suggestions. Standard clients reach the same list through the
`hledger.completion.accountScope` setting:

```json
{
  "hledger.completion.accountScope": "all"
}
```

`"nonzero"` (the default) hides accounts whose balance is zero in every commodity, including
declared-but-unused accounts. `"all"` keeps them, which matters for closed accounts that are still
worth completing.
