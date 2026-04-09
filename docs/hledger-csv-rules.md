# hledger CSV Rules Format Reference

Reference for the `*.rules` file format used by hledger to convert CSV bank
exports into journal transactions. This document is the local source of truth
for the `internal/rules/` package — lexer, parser, completion, diagnostics.

For the journal format, see [`hledger.md`](./hledger.md).

## Table of Contents

1. [File Extension and Purpose](#file-extension-and-purpose)
2. [Top-Level Directives](#top-level-directives)
3. [Field Assignments](#field-assignments)
4. [Field Reference Interpolation](#field-reference-interpolation)
5. [If Block Grammar](#if-block-grammar)
6. [Edge Cases and Examples](#edge-cases-and-examples)
7. [References](#references)

---

## File Extension and Purpose

CSV rules live in files with the `.rules` extension. They are processed by
hledger's CSV reader (and by `internal/rules/` in this LSP) — not by the
journal parser. A `.rules` file describes how to map CSV columns onto
transaction fields.

`.rules` files are typically referenced from a journal via `include`, but the
LSP filters them out of the journal include tree (`internal/include/loader.go`)
and parses them through the rules-specific lexer/parser.

A `.rules` file can itself `include` other `.rules` files. The LSP resolves
this transitive closure via a dedicated rules-include loader
(`internal/rules/loader.go`) so that completion in the parent file can see
`fields` (and other directives) declared in any of the included files. See
[The `include` directive](#the-include-directive) below.

## Top-Level Directives

The authoritative list lives in `internal/rules/lexer.go` (`KnownDirectives`).
The lexer and the completion provider both derive their data from this slice.

| Directive | Purpose |
|-----------|---------|
| `skip N` | Skip the first N lines of the CSV input (typically the header row) |
| `fields name1, name2, …` | Declare CSV column names. Position-bound — order matters |
| `separator CHAR` | Field separator character (`,`, `;`, `\|`, `TAB`, `SPACE`) |
| `source path` | Source CSV file (relative to the rules file) |
| `date-format FORMAT` | Date parsing format (e.g. `%Y-%m-%d`, `%d/%m/%Y`) |
| `decimal-mark CHAR` | Decimal mark character (`.` or `,`) |
| `timezone TZ` | Timezone for date parsing |
| `encoding NAME` | Character encoding of the CSV file |
| `balance-type =\|==\|=*\|==*` | Balance assertion type to emit |
| `include path` | Include another rules file (transitive composition) |
| `newest-first` | Process rows newest-first |
| `intra-day-reversed` | Same-day rows are stored in reverse order |
| `archive` | Archive processed files |

Plus two structural keywords:

- `if` — open a conditional block (see [If Block Grammar](#if-block-grammar))
- `end` — terminate the current scope

## The `include` directive

A `.rules` file can pull in another `.rules` file with `include`:

```text
# common.rules — shared across several bank statements
fields date, payee, amount, description
date-format %Y-%m-%d
```

```text
# bank-a.rules
include common.rules
skip 1

if %payee Amazon
  account2 expenses:shopping
```

The LSP resolves the transitive closure of `include` directives (via
`internal/rules/loader.go`) so that field references typed inside an `if`
block in `bank-a.rules` — e.g. `%payee`, `%description` — are completed
using the `fields` list declared in `common.rules`. This works regardless of
whether the included file is also open in the editor: open-buffer content is
preferred, with a disk fallback.

Cycles (`a.rules` → `b.rules` → `a.rules`) are detected and broken; the
cursor's file still gets completion for its directly-reachable `fields`.
Non-`.rules` includes are reported as an error and their content is not
merged.

Path resolution follows hledger's own rules: relative paths are anchored to
the including file's directory, and `~` expands to the user's home. Glob
patterns are not yet supported in rules-include.

## Field Assignments

A field assignment maps a built-in field name to a value. Values may reference
CSV columns or regex match groups (see [Field Reference Interpolation](#field-reference-interpolation)).

The authoritative list of built-in field names lives in
`internal/rules/lexer.go` (`BuiltinFieldNames`). The lexer also accepts
dynamic patterns via `isBuiltinField()`.

### Built-in field names

- Date: `date`, `date2`
- Status / classification: `status`, `code`, `description`, `payee`, `note`, `comment`
- Account legs: `account1`, `account2`, `account3`, …
- Amounts: `amount`, `amount1`, `amount2`, `amount-in`, `amount-out`,
  `amountN-in`, `amountN-out`
- Currencies: `currency`, `currency1`, `currency2`, …
- Balances: `balance`, `balance1`, `balance2`, …

### Two assignment forms

**Top-level (unindented):** applies to every CSV row.

```text
account1 assets:bank:checking
currency $
```

**Inside an `if` block (indented):** applies only when the patterns match.

```text
if %payee Amazon
  account2 expenses:shopping
```

The indent terminates the if-block's pattern list (see grammar below).

## Field Reference Interpolation

A `%fieldname` token in an assignment value is interpolated to the CSV column
declared by `fields`. Match groups from a preceding pattern can also be
interpolated as `\1`, `\2`, …

Examples:

```text
fields date, payee, description, amount

description %payee | %description
account2    expenses:%payee
comment     imported on %date
```

Interpolation works in **any** assignment value — both top-level and inside
`if` blocks. The LSP completes `%fieldname` references in value position when
the user types `%` followed by an identifier prefix.

## If Block Grammar

An `if` block is a conditional that applies field assignments only when one
or more patterns match the current CSV row. The grammar supports several
forms; the LSP and the parser must understand all of them.

### Inline form

The `if` keyword may be followed inline by a pattern:

```text
if %payee Amazon
  account2 expenses:shopping
```

### Multi-line form

Or by patterns on subsequent lines:

```text
if
%payee Amazon
%payee Audible
  account2 expenses:shopping
```

Multiple patterns on separate lines are joined by **OR** (any pattern matches).

### Pattern types

A pattern is one of:

- **Named-field match** — `%fieldname REGEX` matches the value of one CSV
  field against a regex.

  ```text
  %payee Amazon
  %date  2026-01-.*
  ```

- **Raw regex** — a bare regex without `%` matches the **whole CSV record**
  (the entire line as it appears in the CSV).

  ```text
  if Amazon|Audible
    account2 expenses:shopping
  ```

### Continuation operators

Patterns can be combined with logical operators:

- **AND** — prefix `&` (or `&&`) joins the pattern with the previous one with
  logical AND. Both must match.

  ```text
  if %payee Amazon
  & %amount \\b3\\.99
    account2 expenses:subscriptions
  ```

  ```text
  if %payee JohnDoe && %amount 25
  && %date 2025-12-13
    comment "Hush money"
  ```

- **NOT** — prefix `!` negates the pattern.

  ```text
  if
  ! %date 2024-01-.*
    account2 expenses:other
  ```

- **AND NOT** — `&& !` (or `& !`) combines AND with negation.

  ```text
  if %payee Amazon
  && ! %amount 0\\.00
    account2 expenses:shopping
  ```

### Block termination

A pattern list ends and the assignment list begins at the **first indented
line** (any leading whitespace). The if block as a whole ends at the **next
unindented top-level directive or field assignment** — i.e. the next line
whose first word is in `KnownDirectives`, `{if, end}`, or `BuiltinFieldNames`.

Blank lines and comment lines (`#`, `;`, `*`) inside the pattern list do not
terminate the block.

## Edge Cases and Examples

The following examples are derived from
[issue #23](https://github.com/juev/hledger-lsp/issues/23) and serve both as
spec clarifications and as black-box test cases for completion.

### Example 1 — same-line and multi-line continuation

```text
if %payee JohnDoe && %amount 25
&& %date 2025-12-13
  comment "Hush money"
```

- The `if` line carries two patterns joined by `&&`.
- The next line continues with another `&&` pattern.
- The indented `comment` is the assignment that fires when all three match.

### Example 2 — raw-regex first pattern combined with field-specific match

```text
if something && %date 2026-01-02
  account2 foo:bar
```

- `something` is a raw regex that matches the whole CSV record.
- `&& %date 2026-01-02` adds an AND constraint on the date field.

### Example 3 — field reference interpolation in assignment value

```text
if
%payee Amazon
  description %payee | %description
```

- Inside the assignment value, `%payee` and `%description` are interpolated
  from the CSV columns declared by `fields`.

### Example 4 — negation

```text
if
! %date 2024-12-31
  account2 expenses:other
```

- The `!` prefix negates the pattern. The block matches every row whose date
  is not 2024-12-31.

### Example 5 — multi-line conditions, no inline pattern

```text
if
%payee Amazon
%payee Audible
%payee Netflix
  account2 expenses:subscriptions
```

- The `if` line stands alone; patterns follow on the next lines, joined by
  the default OR.

## References

- Official hledger CSV rules section:
  <https://hledger.org/hledger.html#csv>
- Official `if block` reference:
  <https://hledger.org/hledger.html#if-block>
- hledger CSV examples:
  <https://github.com/simonmichael/hledger/tree/main/examples/csv>
- Parser source of truth (Haskell):
  `hledger-lib/Hledger/Read/RulesReader.hs` (`conditionalblockp`,
  `matcherprefixp`, `fieldmatcherp`, `renderTemplate`)
- Local lexer / directive list: `internal/rules/lexer.go`
- Local parser: `internal/rules/parser.go`
- Local completion provider: `internal/rules/completion.go`
