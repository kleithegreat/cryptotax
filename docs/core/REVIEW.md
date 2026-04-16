# Financial Core Review

## Purpose

This document tracks grounded gaps between `docs/core/SPEC.md` and the current Haskell accounting and reporting implementation.

## Open review items

### `TransferIn` and `TransferOut` rows disappear inside `processTx`

- Status: `open`
- Issue type: `implementation gap`
- Why this is a spec/implementation gap: `docs/core/SPEC.md` says unsupported or ambiguous events must not silently disappear. Conservative upstream transfer rows survive normalization, but the core currently erases them from its own result.
- Current implementation evidence: `processTx` in `haskell/src/GainLoss.hs` returns the accumulator unchanged for `TransferIn` and `TransferOut`. `render8949CSV` in `haskell/src/Report.hs` only renders `prGainLosses`. `docs/known-transactions.md` also notes that the local golden fixture works because the core ignores transfer rows.
- Desired direction implied by the spec: the core should keep transfer activity visible as explicit no-op or unsupported output rather than silently dropping it after parsing.
- What blocks resolution: `ProcessResult` and `Report` currently have no transfer-oriented output channel.
- Smallest good next checkpoint: add a stable result path that records transfer rows as intentionally non-taxable or unsupported downstream activity, even before broader accounting semantics change.

### The core now writes a structured income report

- Status: `done`
- Issue type: resolved
- Resolution: `recordIncomeReceipt` now records structured `IncomeEntry` rows in `prIncome`, and `haskell/app/Main.hs` writes them to `income.csv` through `renderIncomeCSV` in `haskell/src/Report.hs`. Income is no longer stderr-only.

### Negative `funding_payment` is now a structured expense but does not affect inventory

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C adopted — negative funding creates structured `FundingExpense` entries written to `funding_expenses.csv`. The expense does not consume USDC lots. `prop_negativeFundingCreatesStructuredExpense` in `haskell/test/Spec.hs` freezes the new behavior. The report is informational, not a tax-semantic claim. Lot consumption (Option D) was explicitly not chosen.

### Perp realized PnL intentionally stays in `perp_pnl.csv`, not 8949

- Status: `done`
- Issue type: resolved
- Resolution: Human decision recorded — canonical output keeps perp realized PnL in the separate `perp_pnl.csv` supplemental report rather than forcing derivative PnL into 8949 cost-basis/proceeds columns. The Haskell core continues to emit `PerpPnlEntry` rows only in the supplemental report.

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the core to reverse-engineer source-specific meaning from `raw_type`, counterparty data, or wallet traces.
- Exact arithmetic and FIFO behavior are not review items here; they are already implemented and covered by the current tests.
