# Financial Core Review

## Purpose

This document tracks grounded gaps between `docs/core/SPEC.md` and the current Haskell accounting and reporting implementation.

## Open review items



## Resolved (kept as one-line history; details in git log)

- **`TransferIn` and `TransferOut` rows are now visible in `transfers.csv`** — `handleTransfer` records every transfer row as a `TransferEntry` in `prTransfers`, rendered to the supplemental `transfers.csv` (Date, Tx ID, Direction, Asset, Amount, USD Value, Wallet, Counterparty). Transfers remai...
- **The core now writes a structured income report** — `recordIncomeReceipt` now records structured `IncomeEntry` rows in `prIncome`, and `haskell/app/Main.hs` writes them to `income.csv` through `renderIncomeCSV` in `haskell/src/Report.hs`. Income is no longer stderr-only.
- **Negative `funding_payment` is now a structured expense but does not affect inventory** — Decision Option C adopted — negative funding creates structured `FundingExpense` entries written to `funding_expenses.csv`. The expense does not consume USDC lots. `prop_negativeFundingCreatesStructuredExpense` in `ha...
- **Perp realized PnL intentionally stays in `perp_pnl.csv`, not 8949** — Human decision recorded — canonical output keeps perp realized PnL in the separate `perp_pnl.csv` supplemental report rather than forcing derivative PnL into 8949 cost-basis/proceeds columns. The Haskell core continue...
