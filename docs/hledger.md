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
```

The pipe `|` divides description into queryable `payee` and `note` fields.

---

## Postings

### Basic Format

```
    [STATUS] ACCOUNT    AMOUNT [@ PRICE] [= ASSERTION]    [; COMMENT]
```

### Indentation Rules

- **Minimum**: 1 space or tab (but 2-4 spaces is conventional)
- **Account-Amount separator**: At least 2 spaces OR 1 tab required

```
    expenses:food    $10.00     ; correct - 2+ spaces before amount
    expenses:food $10.00      ; correct - 1 tab before amount
    expenses:food $10.00        ; WRONG - only 1 space, amount parsed as part of account name!
```

**Critical:** The two-space (or tab) delimiter between account and amount is essential. With only one space, hledger will misparse the amount as part of the account name.

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

**Balancing precision** (since hledger 1.50):

- Inferred from the highest decimal precision used in each commodity within the transaction
- Cost amounts don't affect balancing precision
- Example: if one posting uses `$1.00` and another `$1.5`, precision is 2 decimals

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

### Decimal Mark Disambiguation

When amounts contain only one period or comma (e.g., `1,000`), hledger assumes it's a decimal mark. Use `decimal-mark` directive to clarify:

```
decimal-mark .    ; Period is decimal
decimal-mark ,    ; Comma is decimal
```

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

After two spaces or tab:

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
| Tag name | Letters, numbers, hyphens |
| No spaces in name | `project-x:` not `project x:` |
| Value is optional | `reviewed:` |
| Value ends at comma or line end | `tag:value,` |
| Multiple tags | `tag1:val1, tag2:val2` |

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

Postings can have a balance assertion without an explicit amount. The posting contributes zero to the transaction but still validates the account balance:

```
; Balance assertion only (no amount)
assets:checking           = $5000
assets:checking           == $5000
assets                    =* $10000
assets                    ==* $10000

; Compact format (no space after =)
assets:checking           =$5000
assets:checking           =$-1775.30
```

**Use cases:**

- Verifying balance at a point in time without affecting transaction
- Reconciliation checkpoints
- Opening balance verification

### Balance Assignment

Use `:=` to set balance (compute posting amount):

```
assets:checking    $0    := $5000    ; Sets balance to $5000
```

### Assertion Behavior

- Checked in **date order** (not parse order)
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

### Lot Prices (Ledger compatibility)

```
assets:stocks    10 AAPL {$150}    ; Lot price (ignored by hledger)
assets:stocks    10 AAPL {@$150}   ; hledger uses @ notation
```

---

## References

- [hledger Manual](https://hledger.org/hledger.html)
- [Journal Format](https://hledger.org/journal.html)
- [Quick Reference](https://plaintextaccounting.org/quickref/hledger)
- [hledger and Ledger Differences](https://hledger.org/ledger.html)
