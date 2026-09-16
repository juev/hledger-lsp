# Tasks

- [x] Filter account completion by nonzero balances, excluding the edited transaction. Verified with tests, race detection, lint, and build.
- [x] Add request-local nonzero/all account completion for staged suggestions in hledger-vscode #301. Verified with JSON-RPC, include/Unicode/CRLF and large-list tests, race detection, CI lint, build, and VS Code integration.
- [x] Cut account-completion request time on a 25k-line journal from ~130 ms to ~20 ms: the balance-only walk no longer reuses the inlay-hint effects walk, which snapshotted the full balance map after every posting. Completion payloads are byte-identical; verified with the full Go test suite, race detection, real-workspace benchmarks, and a JSON-RPC typing burst.
