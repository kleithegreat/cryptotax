# Hyperliquid Review

## Purpose

This document tracks grounded gaps between `docs/hyperliquid/SPEC.md` and the current Hyperliquid fetch, normalization, and downstream accounting behavior.

## Open review items

### Funding rows lose market context and still have weak evidence linkage

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `evidence gap`
- Why this is a spec/implementation gap: `docs/hyperliquid/SPEC.md` says the pipeline should preserve enough information for later accounting improvements and human review, and it names evidence retention and linkage for funding rows as an immediate priority. The current normalized funding row keeps only the USDC flow and a weak source identifier.
- Current implementation evidence: `fetchFunding` in `go/fetcher/hyperliquid.go` receives `hlFunding.Delta.Coin`, stores it in `RawTransaction.Asset`, and copies `hlFunding.Hash` to `ID`. `normalizeHyperliquid` in `go/normalize/normalize.go` emits only USDC `Sent` or `Received` on `funding_payment`, so the market context is dropped. `docs/known-transactions.md` records real-wallet funding rows whose current `id` is the all-zero hash and still needs source-row retention for review.
- Desired direction implied by the spec: a reviewer should be able to connect one normalized funding row back to the Hyperliquid market and source event that produced it.
- What blocks resolution: `types.Transaction` has no dedicated market-context or secondary-source-identifier field, so preserving that context would change the IR contract.
- Smallest good next checkpoint: decide what minimum market and evidence context a funding row must retain before implementation work starts.

### Negative funding is now a structured expense; inventory effect is not applied

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C adopted — negative funding creates structured `FundingExpense` entries in `prFundingExpenses`, written to `funding_expenses.csv`. The expense does not consume USDC lots (Option D was not chosen). `prop_negativeFundingCreatesStructuredExpense` in `haskell/test/Spec.hs` freezes the new behavior.

### Perp fills are quarantined from spot FIFO; ClosedPnl used for PnL output

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C (API-PnL model) adopted — perp fills now use `perp_open` / `perp_close` tx types instead of spot `buy` / `sell`. `ClosedPnl` is propagated from the fetcher through normalization; `StartPosition` is retained only on the raw fetcher row for possible future use. The Haskell core emits `PerpPnlEntry` records using exchange-reported `ClosedPnl` in `perp_pnl.csv`. Perp opens are no-ops (no phantom lots). Perp closes do not consume spot FIFO lots. Tests `prop_perpOpenDoesNotCreateLots`, `prop_perpCloseUsesClosedPnl`, `prop_perpCloseWithoutPnlIsError`, `prop_perpCloseWithoutSentIsError`, and `prop_perpDoesNotContaminateSpotFIFO` freeze the quarantine and PnL behavior.

### Perp PnL 8949 representation is unresolved

- Status: `open`
- Issue type: `support-boundary gap`
- Why this is a gap: The perp PnL report uses exchange-reported `ClosedPnl` as realized PnL. The cost-basis and proceeds representation for derivative PnL on Form 8949 is unresolved (notional entry/exit? net settlement?). The separate `perp_pnl.csv` is intentionally not 8949-formatted.
- What blocks resolution: this is a tax-interpretation question requiring human decision or professional guidance.

### Partial-fill ClosedPnl is not consolidated

- Status: `open`
- Issue type: `support-boundary gap`
- Why this is a gap: each API fill gets its own `PerpPnlEntry`. Multiple fills sharing one economic position change (e.g., `hl-close-short-sol` with 4 partial fills) produce 4 separate PnL rows instead of one consolidated entry.
- Smallest good next checkpoint: decide whether fills sharing a hash or timestamp should be consolidated into one PnL entry.

## Non-goals / intentionally narrow boundaries

- Spot `@N` asset resolution through `fetchSpotMeta` is not itself under review here.
