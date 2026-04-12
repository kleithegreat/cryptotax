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

### The core has supplemental output channels but income still lacks a structured report

- Status: `open`
- Issue type: `schema/contract gap`
- Why this is still a gap: The core now writes `funding_expenses.csv` and `perp_pnl.csv` as supplemental reports. But `prIncome` entries are still only summarized to stderr, not written to a structured file. A complete non-8949 output story would include structured income output.
- What blocks resolution: income report format and support claims need a human decision.

### Negative `funding_payment` is now a structured expense but does not affect inventory

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C adopted — negative funding creates structured `FundingExpense` entries written to `funding_expenses.csv`. The expense does not consume USDC lots. `prop_negativeFundingCreatesStructuredExpense` in `haskell/test/Spec.hs` freezes the new behavior. The report is informational, not a tax-semantic claim. Lot consumption (Option D) was explicitly not chosen.

### Perp PnL uses exchange-reported ClosedPnl; 8949 representation is unresolved

- Status: `open`
- Issue type: `support-boundary gap`
- Why this is a gap: Perp close events now emit `PerpPnlEntry` with the exchange-reported `ClosedPnl` in `perp_pnl.csv`. However, the representation of derivative PnL on Form 8949 (cost-basis and proceeds columns) is not yet decided. The perp PnL report is intentionally separate from 8949 output.
- What blocks resolution: the 8949 representation for derivative PnL is a tax-interpretation question that needs human decision or professional guidance.
- Smallest good next checkpoint: decide how perp PnL should appear on 8949 (notional entry/exit, net settlement, or a different form entirely).

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the core to reverse-engineer source-specific meaning from `raw_type`, counterparty data, or wallet traces.
- Exact arithmetic and FIFO behavior are not review items here; they are already implemented and covered by the current tests.
