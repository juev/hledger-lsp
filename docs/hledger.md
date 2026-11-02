# hledger Journal Format Reference

Comprehensive reference for hledger journal file format, syntax, and features.
This document serves as an internal reference for the hledger VSCode extension development.

## Table of Contents

1. [File Format Overview](#file-format-overview)
2. [Transactions](#transactions)
3. [Postings](#postings)
4. [Amounts and Commodities](#amounts-and-commodities)
5. [Account Names](#account-names)
6. [Comments](#comments)
7. [Tags](#tags)
8. [Balance Assertions](#balance-assertions)
9. [Virtual Postings](#virtual-postings)
10. [Directives](#directives)
11. [Periodic Transactions](#periodic-transactions)
12. [Auto Posting Rules](#auto-posting-rules)
13. [Period Expressions](#period-expressions)
14. [Query Syntax](#query-syntax)
15. [Strict Mode](#strict-mode)

---

## File Format Overview

### File Extensions

hledger recognizes these file extensions:

- `.journal` - Primary hledger format
- `.j` - Short form
- `.hledger` - Alternative
- `.ledger` - Ledger compatibility
- `.prices` - Price directives data for journal imports

### Basic Structure

A journal file contains three types of entries:

1. **Comments** - Lines starting with `#` or `;`
2. **Transactions** - Financial entries starting with a date
3. **Directives** - Configuration and declarations

### Character Encoding

- UTF-8 encoding supported
- Full Unicode support including Cyrillic, CJK, etc.
- Account names can contain any Unicode letters

---

## Transactions

### Basic Transaction Format

```
DATE [STATUS] [(CODE)] DESCRIPTION
    ACCOUNT1    AMOUNT
    ACCOUNT2    [AMOUNT]
```

### Date Formats

All date formats use `YYYY-MM-DD`, `YYYY/MM/DD`, or `YYYY.MM.DD`:

| Format | Example | Notes |
|--------|---------|-------|
| Full | `2024-01-15` | Recommended |
| Slashes | `2024/01/15` | Alternative |
| Dots | `2024.01.15` | Alternative |
| No leading zeros | `2024-1-5` | Valid |
| Year omitted | `01-15` | Inferred from context |

**Year Inference Order:**

1. Current transaction context
2. `Y` directive (default year)
3. Current system date

### Secondary Dates

```
2024-01-15=2024-01-20 Transaction description
```

The secondary date (after `=`) is used with `--date2` flag.

### Status Marks

| Mark | Name | Meaning |
|------|------|---------|
| (empty) | Unmarked | Recorded, not reconciled |
| `!` | Pending | Tentatively reconciled |
| `*` | Cleared | Verified and complete |

```
2024-01-15 ! Pending transaction
2024-01-15 * Cleared transaction
```

Status can appear on transaction line or individual postings.

### Transaction Code

Optional reference in parentheses after status:

```
2024-01-15 * (CHK12345) Check payment
2024-01-15 (INV-001) Invoice payment
```

### Description

Text after date/status/code until end of line or semicolon.

**Payee | Note Separator:**

```
2024-01-15 Whole Foods | Groceries for party
2024-01-15 PAYEE | MY DESCRIPTION | BANK'S CSV DESCRIPTION
```

The first `|` divides description into queryable `payee` (left) and `note` (right) fields.
Only the first `|` is significant — any subsequent `|` characters are part of the note text.
For example, `PAYEE | A | B` produces payee=`PAYEE` and note=`A | B`.

---

## Postings

### Basic Format

```
    [STATUS] ACCOUNT    AMOUNT [@ PRICE] [= ASSERTION]    [; COMMENT]
```

### Indentation Rules

- **Minimum**: 1 space or tab (but 2-4 spaces is conventional)
- **Account-Amount separator**: two or more whitespace characters in a row, where a tab counts as one character

```
    expenses:food  $10.00            ; correct - 2 spaces
    expenses:food<TAB><TAB>$10.00    ; correct - 2 tabs
    expenses:food<TAB> $10.00        ; correct - tab and space (any pair works)
    expenses:food<TAB>$10.00         ; WRONG - a single tab, amount becomes part of the account name
    expenses:food $10.00             ; WRONG - only 1 space, amount becomes part of the account name
```

**Critical:** The separator must be at least two whitespace characters. A single space or a single tab is part of the account name, so `expenses:food<TAB>$10.00` is an account called `expenses:food $10.00` with no amount (hledger shows the tab as a space). The same rule applies before a `;` comment and before a `=` balance assertion on an amountless posting. After an amount, one space or tab is enough between amount, price, assertion and comment.

### Inferred Amounts

One posting per transaction can have its amount omitted:

```
2024-01-15 Grocery shopping
    expenses:food        $50.00
    assets:checking              ; Amount inferred as -$50.00
```

### Posting Status

Individual postings can have their own status:

```
2024-01-15 Transaction
    * assets:checking    $-100    ; Cleared
    ! expenses:pending    $100    ; Pending
```

### Transaction Balancing

All posting amounts in a transaction must sum to zero (within balancing precision).

**Balancing precision** (since hledger 1.50, verified against hledger 1.52.4):

- Inferred from the highest decimal precision of the amounts denominated in each commodity within the transaction
- Cost amounts (`@`/`@@`) and the native amount behind a cost do not affect the cost commodity's precision:
  `1.005 AAPL @ $2` with `$-2.0` balances because `$` keeps precision 1 (tolerance 0.05), while `$-2.00` does not
- `commodity` and `D` directive precision is **not** a floor: `commodity $1,000.000` alone does not tighten the
  tolerance for `$` amounts written with fewer decimals
- Lot prices (`{}`/`{{}}`) are not costs: they do not participate in balancing at all
- Example: if one posting uses `$1.00` and another `$1.5`, precision is 2 decimals

**Currency conversion inference:**

A transaction with no explicit cost that is left with exactly two residual commodities of opposite
signs is accepted — hledger infers the conversion price:

```
2024-01-15 convert
    assets:bank:eur     €100
    assets:bank:usd     $-110        ; accepted: two commodities, opposite signs, no @
```

An explicit `@`/`@@` cost anywhere in the transaction disables the inference: the converted sum must
be zero within tolerance. One residual commodity, three or more, or two with the same sign are all
unbalanced.

**One amount can be omitted:**

- hledger infers the missing amount to balance the transaction
- Only ONE posting can have an omitted amount

```
2024-01-15 Example
    expenses:food    $10.00
    assets:cash              ; Inferred as $-10.00
```

---

## Amounts and Commodities

### Amount Structure

`[SIGN] [COMMODITY] [SIGN] QUANTITY [COMMODITY]`

**Sign placement:** The minus sign can appear before the commodity, after the commodity, or both positions are valid:

```
-$100       ; Sign before commodity
$-100       ; Sign after commodity (before quantity)
-100 USD    ; Sign before quantity
```

All three forms are equivalent and represent negative $100.

Both signs may be present, with spaces around the commodity and the second sign. The two signs multiply, so `-EUR - 5` is `EUR 5` and `+EUR - 5` is `EUR -5`. A third sign is an error (`-EUR --5`, `-EUR - - 5`). The same rule holds in costs (`@`, `@@`), balance assertions and the price of a `P` directive.

### Quantity Formats

| Format | Example | Notes |
|--------|---------|-------|
| Integer | `100` | Simple number |
| Decimal | `100.50` | Period decimal |
| European | `100,50` | Comma decimal |
| With grouping | `1,000.00` | US style |
| European grouping | `1.000,00` | European style |
| Space grouping | `1 000.00` | International |
| Indian grouping | `1,00,00,000.00` | Lakh/crore style |
| Scientific | `1E-6` | Scientific notation (also `EUR 1E3`) |
| Negative | `-100` | Explicit sign |
| Positive | `+100` | Explicit plus sign (optional) |
| Trailing decimal | `10.` | Disambiguates integer from decimal |

**Empty commodity:** Bare numbers without commodity symbol default to empty commodity (`""`).

### Commodity Placement

```
$100        ; Symbol before, no space
100 USD     ; Symbol after, with space
€100        ; Symbol before, no space
100€        ; Symbol after, no space
```

### Commodity Names

| Type | Example | Notes |
|------|---------|-------|
| Symbol | `$`, `€`, `£` | Currency symbols |
| Code | `USD`, `EUR`, `BTC` | Uppercase codes |
| Lowercase | `hours`, `units` | Lowercase names |
| Quoted | `"chocolate bars"` | Multi-word or special chars |
| Quoted special | `"ACME Inc."` | Contains period or other special chars |

**Quoted commodities:** Use double quotes when commodity name contains spaces, periods, or other special characters:

```
3 "green apples"
10 "ACME Inc." @ $50
```

**Unquoted symbols:** hledger 1.52.4 reads every character as part of an unquoted symbol except ASCII digits, space, tab and ``-+.@*;"{}=``. Punctuation, brackets and any other Unicode character belong to the symbol, and the symbol runs as far as such characters go:

```
,5          ; symbol ",", quantity 5
$,5         ; symbol "$,", quantity 5
5 EUR!      ; symbol "EUR!"
5 (x)       ; symbol "(x)"
5 😀        ; symbol "😀"
```

The same rule holds in postings, costs, balance assertions, the `D` directive and the `commodity` directive, and for the commodity a `P` directive prices. That commodity must be followed by a space and may not hold a digit or a character from the list above unless it is quoted. A comma glued to a symbol is part of it, so a number that starts with the decimal comma needs a prefix symbol and a space: `$ ,5` is `$ 0.5`. After the price, hledger ignores the rest of a `P` line. A `(` or `[` after a complete amount opens a lot annotation instead of a symbol.

### Decimal Mark Disambiguation

When amounts contain only one period or comma (e.g., `1,000`), hledger assumes it's a decimal mark. Use `decimal-mark` directive to clarify:

```
decimal-mark .    ; Period is decimal
decimal-mark ,    ; Comma is decimal
```

**Rules verified against hledger 1.52** (`internal/testutil/hledger.go` holds the case matrix; run it with `nix shell nixpkgs#hledger -c env HLEDGER_ORACLE=1 go test ./... -run Oracle`):

- Only a number with one separator and no space group is ambiguous. `1.234,50`, `1,234,567` and `1 500,50` state their own reading, whatever is declared.
- An ambiguous number takes the first declared mark from: the `decimal-mark` directive, the commodity's own style, the `D` directive's style. `D` is the fallback for every commodity, not only the one it names. With nothing declared, the lone separator is the decimal mark.
- Under a declared mark, a lone separator of the other kind is a digit group: with `commodity 1000.00 X`, `15,000 X` is 15000.
- Directives apply from their position onward, and commodity styles cross `include` in both directions.
- `commodity` and `D` numbers are read by the same rules and must end up with a decimal mark: `commodity 1000 X` is an error, `commodity 1000. X` is not, and `commodity 1000,00 X` is an error when `.` is already in force. `commodity 1,000 X` declares a comma decimal mark.
- `commodity X` with no format resets the style.

### Digit Group Marks

The character NOT used as decimal mark can separate digit groups:

- Period decimal (`1,000.00`) → comma grouping
- Comma decimal (`1.000,00`) → period grouping
- Space grouping (`1 000.00`) → works with either

### Cost Notation

Costs record the exchange rate at the time of a transaction. This differs from market prices (`P` directive) which track value over time.

**Per-unit cost:**

```
assets:stocks    10 AAPL @ $150     ; 10 shares at $150 each = $1500 total
```

**Total cost:**

```
assets:stocks    10 AAPL @@ $1500   ; 10 shares for $1500 total
```

**Inferred cost:**

When exactly two commodities are used without explicit cost notation, hledger infers the cost:

```
2024-01-15
    assets:euros     €100
    assets:dollars  $-135           ; Cost inferred as €100 @@ $135
```

**Cost reporting:**

- Use `-B/--cost` flag to convert amounts to their cost basis
- Costs are normally positive amounts (though not required)
- Cost amounts display with full decimal precision

**Cost vs Market Price:**

| Feature | Cost (`@`/`@@`) | Market Price (`P`) |
|---------|-----------------|-------------------|
| Where | In transaction posting | Separate directive |
| When | Records historical exchange rate | Declares price at a point in time |
| Use | Track purchase price | Report current value with `-V` |

---

## Account Names

### Format

`TOPACCOUNT[:SUBACCOUNT[:SUBACCOUNT...]]`

### Rules

| Rule | Valid | Invalid |
|------|-------|---------|
| Start with letter | `assets` | `123assets` |
| Single spaces allowed | `credit card` | `credit  card` |
| Colon separator | `assets:bank` | - |
| Unicode letters | `активы:банк` | - |
| Numbers after letters | `card1` | - |
| Symbols in name | `a]b` | - |

### Examples

```
assets:bank:checking
liabilities:credit card:visa
expenses:food:groceries
revenues:salary:bonus
equity:opening balances
активы:наличные                ; Russian
支出:食品                       ; Chinese
```

### Account Hierarchy

From `assets:bank:checking`, hledger infers:

- `assets`
- `assets:bank`
- `assets:bank:checking`

Parent accounts inherit child properties in reports.

---

## Comments

### Line Comments

```
# Hash comment (entire line)
; Semicolon comment (entire line)
* Asterisk comment (org-mode outline)
```

### Inline Comments

After an amount, or after two or more whitespace characters when an account has no amount (a lone space or tab there belongs to the account name):

```
2024-01-15 Description  ; Transaction comment
    account    $100     ; Posting comment
```

### Multi-line Comment Block

```
comment
This is a multi-line comment.
It can span multiple lines.
end comment
```

### Indented Comments

Comments can continue on indented lines:

```
2024-01-15 Description  ; First line
    ; Continuation of transaction comment
    account    $100
```

---

## Tags

### Format

`TAGNAME:` or `TAGNAME:VALUE`

Tags appear in comments (transaction or posting level).

### Syntax Rules

| Rule | Example |
|------|---------|
| Tag name is the last word before the colon | `paid on date:2024-01-19` is the tag `date` |
| Any non-space characters form the name, in any script | `проект:`, `a.b:`, `[x]y:` |
| A comma glued to the word belongs to the name | `x,date:2024-01-19` is the tag `x,date`, not a date |
| A colon with no word before it names nothing | `; :x, y:1` has only the tag `y` |
| Value is optional | `reviewed:` |
| Value ends at comma or line end, spaces around it are trimmed | `tag: two words , next:1` |
| A colon inside a value starts no tag | `foo:bar baz:qux` is `foo` = `bar baz:qux` |
| Multiple tags | `tag1:val1, tag2:val2` |
| Each comment line is read on its own | a name or value never spans lines |

These rules were checked against hledger 1.52.4. They apply to every
comment: the transaction line, the posting line, comment lines below them and
directive comments such as `account`. A `date:` or `date2:` tag written after
prose dates the posting like any other, and hledger refuses its invalid value.

A posting comment (on the posting line or a `;` line below a posting, in a
transaction, a periodic transaction or an auto posting rule) differs in one
detail: after a colon that names nothing, hledger skips the spaces and
one comma before the next name. So `; :,date:2024-01-19` dates a posting, while
on a transaction the same text is the tag `,date`.

### Examples

```
2024-01-15 Business dinner  ; client:acme, project:alpha
    expenses:meals    $50   ; billable:
    assets:cash             ; date:2024-01-16
```

### Special Tags

| Tag | Purpose | Notes |
|-----|---------|-------|
| `date:` | Posting date override | Must have valid date value |
| `date2:` | Secondary posting date | Must have valid date value |
| `type:` | Account type declaration | Used in account directives |

### date: and date2: Tags

These tags MUST have valid date values:

```
; Valid
expenses:food    $50    ; date:2024-01-20

; Invalid - will cause error
expenses:food    $50    ; date:
expenses:food    $50    ; date:invalid
```

### Bracketed Date Syntax (Legacy)

```
expenses:food    $50    ; [2024-01-20]
expenses:food    $50    ; [2024-01-15=2024-01-20]
```

### Tag Scope

- **Transaction comment tags**: Apply to transaction AND all its postings
- **Posting comment tags**: Apply only to that posting
- Transactions acquire tags from their postings for queries

---

## Balance Assertions

### Types

| Syntax | Name | Behavior |
|--------|------|----------|
| `= AMT` | Single commodity | Check this commodity only |
| `== AMT` | Exact/Total | Check all commodities (fails if others exist) |
| `=* AMT` | Inclusive | Include subaccount balances |
| `==* AMT` | Exact inclusive | All commodities, include subaccounts |

### Examples

```
; Single commodity assertion
assets:checking    $100    = $5000

; Total assertion (fails if other commodities present)
assets:checking    $100    == $5000

; Including subaccounts
assets            $0       =* $10000

; Total with subaccounts
assets            $0       ==* $10000
```

### Balance Assertion Without Amount

Postings can have a balance assertion without an explicit amount. hledger infers the amount needed to reach the asserted balance; that amount may be nonzero. Write an explicit `0` amount to check a balance without changing it:

```
; Balance assertion only (no amount)
assets:checking           = $5000
assets:checking           == $5000
assets                    =* $10000
assets                    ==* $10000

; Compact format (no space after =)
assets:checking           =$5000
assets:checking           =$-1775.30

; Check without changing the balance
assets:checking           $0 = $5000
```

**Use cases:**

- Setting an account to a known balance with an inferred posting amount
- Reconciliation checkpoints
- Opening balance verification

### Balance Assignment

`:=` is **not** supported by hledger 1.52.4: it fails to load with `unexpected ':'`. Do not use it in
journals. Use a balance assertion (`= AMOUNT`) and the inferred amount instead, or set the posting
amount explicitly.

### Assertion Behavior

- Checked by primary posting date (`date:` or `[DATE]`), then parse order on the same day; `date2:` does not change this order
- **Enabled by default**; disable with `-I/--ignore-assertions`
- **Include virtual postings** (not affected by `-R` flag or `real:` query)
- **Work across include files** when using `include` directive
- **Cost amounts in assertions are ignored** - don't affect whether assertion passes
- **Balancing precision**: Since hledger 1.50, inferred from highest decimal precision per commodity in each transaction

### Balance Assertions and Auto Postings

**Warning:** Balance assertions ARE affected by `--auto` flag, which generates auto postings that can alter account balances.

To avoid fragile assertions:

- Either consistently use `--auto` with files containing balance assertions
- Or avoid balance assertions on accounts affected by auto posting rules

---

## Virtual Postings

Virtual postings are indicated by enclosing the **full** account name in parentheses or brackets. Parentheses or brackets *internal* to the account name have no special meaning.

### Unbalanced Virtual Postings (Parentheses)

```
2024-01-15 Set opening balance
    (assets:checking)    $1000
```

**Properties:**

- **NOT required to balance** - exempt from the rule that postings must sum to zero
- Excluded by `--real/-R` flag
- Excluded by `real:1` query
- With no amount, always infers zero amount

**Use cases:**

- Setting opening balances without equity account
- Tracking notes or metadata that don't affect balances
- One-off adjustments

### Balanced Virtual Postings (Brackets)

```
2024-01-15 Purchase with budget tracking
    expenses:food             $50
    assets:cash              $-50
    [budget:food]            $-50
    [budget:available]        $50
```

**Properties:**

- **Must balance to zero** (separately from real postings)
- Bracketed postings balance among themselves
- Excluded by `--real/-R` flag
- Excluded by `real:1` query

**Use cases:**

- Budget envelope tracking with balance requirement
- Double-entry tracking alongside real transactions

### Complete Example

```
2024-01-15 Buy food with cash, update budget envelopes
    assets:cash                      $-10   ; <- real postings balance
    expenses:food                     $10   ; <-
    [assets:checking:budget:food]   $-10    ; <- balanced virtual postings balance
    [assets:checking:available]      $10    ; <-
    (tracking:purchase)               $1    ; <- unbalanced, not required to balance
```

### Balance Assertions and Virtual Postings

**Important:** Balance assertions always consider BOTH real and virtual postings. They are NOT affected by `--real/-R` flag or `real:` query.

---

## Directives

### account

Declares account names and properties:

```
account assets:checking
account assets:checking    ; type:A
account expenses:food      ; type:X, category:discretionary
```

**Subdirectives** (indented on following lines):

```
account assets:checking
    alias checking
    note Main checking account
    type A
```

### commodity

Declares commodity display format:

```
commodity $0.00              ; 2 decimals, symbol left
commodity 1,000.00 USD       ; 2 decimals, symbol right, comma grouping
commodity €1.000,00          ; European format
commodity 1.00000000 BTC     ; 8 decimals
```

**Multi-line format:**

```
commodity INR
    format INR 1,00,00,000.00    ; Indian grouping
```

**Rules:**

- Must include decimal mark (period or comma)
- For zero decimals, put mark at end: `commodity $1.`
- Quantity value doesn't matter, only format

### decimal-mark

Sets decimal separator for file:

```
decimal-mark .    ; Period is decimal
decimal-mark ,    ; Comma is decimal
```

### payee

Declares valid payee names:

```
payee Whole Foods Market
payee Amazon
payee "John's Hardware"
```

Used with `hledger check payees`.

### tag

Declares valid tag names:

```
tag project
tag client
tag billable
```

Used with `hledger check tags`.

### Y (Year)

Sets default year for dates without year:

```
Y 2024

01-15 Transaction in 2024
    expenses:food    $50
    assets:cash
```

### P (Price)

Declares market price:

```
P 2024-01-15 EUR $1.08
P 2024-01-15 AAPL $185.50
P 2024-01-15 BTC $42000.00
```

### alias

Creates account name alias:

```
; Simple alias
alias checking = assets:bank:checking

; Regex alias
alias /^expenses:food/=expenses:dining
```

**Scope**: Affects all subsequent entries in file and included files.

**Command line**: `--alias 'OLD=NEW'`

### include

Includes another journal file:

```
include accounts.journal
include ~/finances/2024.journal
include ../shared/prices.journal
```

**Relative paths**: Relative to including file's directory.

### apply account

Prepends parent account to all accounts:

```
apply account business

2024-01-15 Sale
    revenue    $100
    checking               ; becomes business:checking

end apply account
```

### comment / end comment

Multi-line comment block:

```
comment
This entire block
is ignored by hledger
end comment
```

---

## Periodic Transactions

### Format

```
~ PERIOD_EXPRESSION [DESCRIPTION]
    ACCOUNT    AMOUNT
    ACCOUNT    [AMOUNT]
```

### Examples

```
~ monthly
    expenses:rent          $2000
    assets:checking

~ weekly from 2024-01
    expenses:groceries     $150
    assets:checking

~ every 15th of month
    (budget:savings)       $500

~ yearly from 2024-01-01
    expenses:insurance     $1200
    assets:checking
```

### Usage

**Forecast mode** (`--forecast`):

- Generates transactions after latest recorded transaction
- Default: until 6 months from today
- Can specify period: `--forecast=2024`

**Budget mode** (`--budget`):

- Compares actual vs budgeted amounts
- Shows percentage of budget used
- Requires reporting interval (`-M`, `-Q`, `-Y`)

---

## Auto Posting Rules

### Format

```
= QUERY
    ACCOUNT    AMOUNT_EXPRESSION
    ACCOUNT    AMOUNT_EXPRESSION
```

### Amount Expressions

| Expression | Meaning |
|------------|---------|
| `$100` | Fixed amount |
| `*0.10` | 10% of matched posting |
| `*-0.10` | Negative 10% of matched |
| `*$2` | Twice the matched amount, in `$` |

The `*` must come directly before the amount: hledger 1.52 rejects `* -1`. A
bare `*` is accepted and generates a zero amount. The multiplier is valid only
in auto posting rules; in regular and periodic transactions hledger rejects it.

### Examples

```
; Add tax to all income
= revenues
    (liabilities:tax)    *0.25
    (expenses:tax)      *-0.25

; Track business expenses
= expenses:business
    (tracking:business)    *1
```

### Enabling

- Use `--auto` flag to apply auto posting rules
- Applied before reporting
- Rules affect whole file and included files (not sibling files)

---

## Period Expressions

### Standard Intervals

| Flag | Expression | Meaning |
|------|------------|---------|
| `-D` | `daily` | Every day |
| `-W` | `weekly` | Every week (Monday start) |
| `-M` | `monthly` | Every month |
| `-Q` | `quarterly` | Every quarter |
| `-Y` | `yearly` | Every year |

### Custom Intervals

```
every 2 weeks
every 3 months
every day
every 2nd day of month
every 15th of month
every monday
every mon,wed,fri
every weekday           ; mon-fri
every weekendday        ; sat-sun
```

### Date Ranges

```
from 2024-01
to 2024-12
from 2024-01 to 2024-06
2024                    ; Entire year
2024-Q1                 ; First quarter
2024-01                 ; January 2024
```

### Combined Expressions

```
monthly from 2024-01
weekly from 2024-01 to 2024-03
every 2 weeks from 2024-01-15
yearly from 2020
```

### Smart Dates (UI/queries only)

```
today
yesterday
tomorrow
this week
last month
next quarter
this year
```

---

## Query Syntax

### Account Queries

| Query | Matches |
|-------|---------|
| `bank` | Accounts containing "bank" |
| `^assets` | Accounts starting with "assets" |
| `checking$` | Accounts ending with "checking" |
| `acct:bank` | Explicit account query |

### Description Queries

```
desc:amazon
desc:'whole foods'
desc:/grocery|food/
```

### Amount Queries

```
amt:>100        ; Greater than 100
amt:<50         ; Less than 50
amt:=0          ; Exactly zero
```

### Date Queries

```
date:2024
date:2024-01
date:2024-01-15
date:thismonth
date:lastweek
```

### Status Queries

```
status:*        ; Cleared only
status:!        ; Pending only
status:         ; Unmarked only
```

### Tag Queries

```
tag:project              ; Has tag named "project"
tag:project=alpha        ; Tag "project" with value "alpha"
tag:date=2024            ; Tag "date" containing "2024"
tag:.=202[1-3]           ; Any tag with value matching regex 202[1-3]
tag:project=             ; Tag "project" with empty value
```

Tag queries support regex patterns for flexible matching.

### Payee and Note Queries

```
payee:amazon
payee:'whole foods'
note:groceries
```

### Commodity Queries

```
cur:USD
cur:$
cur:EUR
```

### Boolean Operations

```
assets checking         ; AND (implicit)
assets AND checking     ; AND (explicit)
assets OR liabilities   ; OR
NOT expenses            ; Negation
not:expenses            ; Negation (alternative)
```

### Depth Limiting

```
depth:2                 ; Show only 2 levels deep
```

### Real vs Virtual

```
real:1                  ; Real postings only
real:0                  ; All postings (default)
```

---

## Strict Mode

Enable with `-s` or `--strict` flag.

### Requirements in Strict Mode

| Check | Requirement |
|-------|-------------|
| Accounts | Must be declared with `account` directive |
| Commodities | Must be declared with `commodity` directive |
| Balance | Always checked (default) |
| Assertions | Always checked (default) |

### Specific Checks

```bash
hledger check accounts       # Verify all accounts declared
hledger check commodities    # Verify all commodities declared
hledger check payees         # Verify all payees declared (needs payee directives)
hledger check tags           # Verify all tags declared (needs tag directives)
hledger check ordereddates   # Verify transactions in date order
hledger check uniqueleafnames # Verify unique leaf account names
```

### Example Setup

```
; Strict mode requires these declarations:

account assets:checking         ; type:A
account assets:savings          ; type:A
account liabilities:credit      ; type:L
account expenses:food           ; type:X
account revenues:salary         ; type:R

commodity $0.00
commodity €0.00

payee Whole Foods
payee Employer Inc

tag project
tag billable
```

---

## Account Types

### Type Codes

| Code | Full Name | Report Usage |
|------|-----------|--------------|
| `A` | Asset | Balance sheet (left) |
| `L` | Liability | Balance sheet (right) |
| `E` | Equity | Balance sheet (right) |
| `R` | Revenue | Income statement (top) |
| `X` | Expense | Income statement (bottom) |
| `C` | Cash | Cash flow statement |
| `V` | Conversion | Currency conversion |

### Declaration

```
account assets           ; type:A
account liabilities      ; type:L
account equity           ; type:E
account revenues         ; type:R
account expenses         ; type:X
```

### Auto-detection

If using English names, types are inferred:

- `assets*` → Asset
- `liabilities*` → Liability
- `equity*` → Equity
- `revenues*`, `income*` → Revenue
- `expenses*` → Expense

Subaccounts inherit parent's type.

---

## File Organization

### Common Patterns

**Single file:**

```
; main.journal
account assets:checking  ; type:A
account expenses:food    ; type:X

2024-01-01 Opening
    assets:checking    $1000
    equity:opening
```

**Multi-file:**

```
; main.journal
include accounts.journal
include 2024/january.journal
include 2024/february.journal
include prices.journal

; accounts.journal
account assets:checking  ; type:A
account expenses:food    ; type:X
...

; 2024/january.journal
2024-01-15 Grocery
    expenses:food    $50
    assets:checking
```

### Directive Scope

| Directive | Scope |
|-----------|-------|
| `account` | Global (all files) |
| `commodity` | Global (all files) |
| `alias` | Current file + includes |
| `apply account` | Until `end apply account` or EOF |
| `Y` (year) | Current file + includes |
| `decimal-mark` | Current file only |
| Auto posting rules | Current file + parent/child (not siblings) |

---

## Special Cases

### Empty Postings

Virtual posting with no amount infers zero:

```
(tracking:category)        ; Infers $0
```

**Note:** Unbalanced virtual postings with no amount always infer zero amount.

### Scientific Notation

```
assets:micro    0.000001 BTC
assets:micro    1E-6 BTC      ; Equivalent
```

### Multi-commodity Accounts

Accounts can hold multiple commodities:

```
2024-01-01
    assets:wallet    $100
    assets:wallet    €50
    assets:wallet    0.01 BTC
    equity:opening
```

**In reports:** hledger combines single-commodity amounts into multi-commodity display:

```
1 USD, 2 EUR, 3.456 TSLA
```

**Note:** You cannot write multi-commodity amounts directly in journal files - only single commodities per posting.

### Lot Prices / Cost Basis Annotations

hledger supports lot syntax for tracking acquisition cost basis. These annotations are preserved
but not used for balance calculations — only `@`/`@@` determines the transaction price.
Verified against hledger 1.52.4: `10 AAPL {$100}` with `$-900` is accepted (the residual is a
two-commodity conversion), while the same posting written with `@ $100` is rejected.

**Ledger-style** (separate annotations, any order):

```text
assets:stocks    10 AAPL {$150}              ; unit lot cost
assets:stocks    10 AAPL {{$1500}}           ; total lot cost
assets:stocks    10 AAPL [2024-01-15]        ; lot date
assets:stocks    10 AAPL (lot1)              ; lot label
assets:stocks    10 AAPL {$150} [2024-01-15] (lot1)  ; all combined
```

**hledger consolidated style** (all fields inside `{}`, comma-separated, order: date, label, cost):

```text
assets:stocks    10 AAPL {2026-01-15, $50}
assets:stocks    10 AAPL {2026-01-15, "lot1", $50}
assets:stocks    10 AAPL {$50}
assets:stocks    10 AAPL {}
```

Lot annotations can appear before or after `@`/`@@` cost:

```text
assets:stocks    10 AAPL {$150} @ $180
assets:stocks    10 AAPL @ $180 {$150}
```

---

## References

- [hledger Manual](https://hledger.org/hledger.html)
- [Journal Format](https://hledger.org/journal.html)
- [Quick Reference](https://plaintextaccounting.org/quickref/hledger)
- [hledger and Ledger Differences](https://hledger.org/ledger.html)
