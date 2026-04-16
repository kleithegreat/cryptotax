# Decision: Hyperliquid Negative Funding Output

Status: `done`
Task DAG node: `decision.hyperliquid_negative_funding_output`

## Outcome

Option C was chosen and implemented.

Negative Hyperliquid funding is now emitted as structured `FundingExpense` output in `funding_expenses.csv`.

- normalization preserves negative funding as outbound USDC on `funding_payment`
- the Haskell core records negative rows as `FundingExpense` entries in `prFundingExpenses`
- `haskell/app/Main.hs` writes `funding_expenses.csv` and prints the funding-expense total to stderr
- negative funding does not consume USDC lots

Positive funding still follows the existing income path.

## Current behavior

### 1. Fetch

`fetchFunding` in `go/fetcher/hyperliquid.go` reads Hyperliquid `userFunding` rows and preserves:

- `ID = hlFunding.Hash` (often the all-zero hash)
- `Asset = hlFunding.Delta.Coin` (the market that produced the funding)
- `Amount = hlFunding.Delta.USDC`
- `RawType = "funding"`

### 2. Normalize

`normalizeHyperliquid` in `go/normalize/normalize.go` emits `tx_type = funding_payment`:

- negative amounts become outbound USDC in `sent`
- positive amounts become inbound USDC in `received`
- `raw_type = "funding"` is preserved

The normalized row now also preserves market context in the optional `market` field while keeping the USDC cash-flow legs in `sent` or `received`.

### 3. Core accounting

`handleFunding` in `haskell/src/GainLoss.hs` now:

- routes positive funding into the existing income receipt path
- records negative funding as structured `FundingExpense` entries
- does not emit an error for normal negative funding handling
- does not consume USDC inventory

### 4. Output

`haskell/app/Main.hs` writes `funding_expenses.csv` alongside the main 8949 output whenever any `FundingExpense` rows exist. The report is informational and intentionally separate from 8949 output.

### 5. Test coverage

`haskell/test/Spec.hs` freezes the behavior with `prop_negativeFundingCreatesStructuredExpense`.

## Why Option C was chosen

- it makes negative funding visible in structured output instead of leaving it as a warning only
- it avoids pretending negative funding is an 8949 disposal
- it avoids forcing a USDC lot-consumption policy without explicit human sign-off on that tax interpretation

## Options not chosen

### Option A: warning only

Not chosen because it kept a real economic outflow out of structured output entirely.

### Option B: stderr summary only

Not chosen because the repo already needed a durable artifact, not just a runtime summary.

### Option D: consume USDC inventory

Not chosen because that is a tax/accounting interpretation change, not just a presentation choice.

## Remaining work after the decision

The output decision is resolved. The remaining open work is elsewhere:

- `verification.expand_human_verified_exemplars` still blocks any support-boundary upgrade for the documented real-wallet funding cases
- funding rows still need human verification because Hyperliquid can use the all-zero hash even though normalization now preserves `market` and `event_group_id`
- the broader structured income-report question remains open in `docs/core/REVIEW.md`, but it is separate from the negative-funding output decision itself
