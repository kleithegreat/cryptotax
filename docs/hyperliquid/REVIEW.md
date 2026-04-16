# Hyperliquid Review

## Purpose

This document tracks grounded gaps between `docs/hyperliquid/SPEC.md` and the current Hyperliquid fetch, normalization, and downstream accounting behavior.

## Open review items

### Funding rows now preserve market context, but the source `id` can still be weak

- Status: `open`
- Issue type: `evidence gap`
- Why this is still a gap: Hyperliquid funding rows now preserve market context and a stable `event_group_id`, but the source `id` from the API is still often the all-zero hash. That means source linkage is improved but not fully source-backed.
- Current implementation evidence: `fetchFunding` in `go/fetcher/hyperliquid.go` now keeps `Delta.Coin` in `RawTransaction.Market` and synthesizes `EventGroupID` when the source hash is weak. `normalizeOne` copies both onto the IR as `market` and `event_group_id`. The normalized `id` still remains the Hyperliquid-provided hash.
- Desired direction implied by the spec: a reviewer should be able to connect one normalized funding row back to the Hyperliquid market and source event that produced it.
- What blocks resolution: whether the synthetic `event_group_id` plus market context is sufficient evidence for support-claim upgrades still depends on human verification of the documented exemplar cases.

### Negative funding is now a structured expense; inventory effect is not applied

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C adopted — negative funding creates structured `FundingExpense` entries in `prFundingExpenses`, written to `funding_expenses.csv`. The expense does not consume USDC lots (Option D was not chosen). `prop_negativeFundingCreatesStructuredExpense` in `haskell/test/Spec.hs` freezes the new behavior.

### Perp fills are quarantined from spot FIFO; ClosedPnl used for PnL output

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option C (API-PnL model) adopted — perp fills now use `perp_open` / `perp_close` tx types instead of spot `buy` / `sell`. `ClosedPnl` is propagated from the fetcher through normalization; `StartPosition` is retained only on the raw fetcher row for possible future use. The Haskell core emits `PerpPnlEntry` records using exchange-reported `ClosedPnl` in `perp_pnl.csv`. Perp opens are no-ops (no phantom lots). Perp closes do not consume spot FIFO lots. Tests `prop_perpOpenDoesNotCreateLots`, `prop_perpCloseUsesClosedPnl`, `prop_perpCloseWithoutPnlIsError`, `prop_perpCloseWithoutSentIsError`, and `prop_perpDoesNotContaminateSpotFIFO` freeze the quarantine and PnL behavior.

### Perp realized PnL intentionally stays in `perp_pnl.csv`, not 8949

- Status: `done`
- Issue type: resolved
- Resolution: Human decision recorded — canonical output keeps perp realized PnL in the separate `perp_pnl.csv` report rather than forcing a derivative-specific 8949 representation. The remaining open perp work is about consolidation and support verification, not about moving perps onto 8949.

### Partial-fill ClosedPnl is not consolidated

- Status: `open`
- Issue type: `support-boundary gap`
- Why this is a gap: each API fill gets its own `PerpPnlEntry`. Multiple fills sharing one economic position change (e.g., `hl-close-short-sol` with 4 partial fills) produce 4 separate PnL rows instead of one consolidated entry.
- Current implementation evidence: `fetchFills` preserves one raw row per API fill, `split_reason = "api_fill_granularity"`, and shared `event_group_id` ties related fills together. `handlePerpClose` still emits one `PerpPnlEntry` per normalized `perp_close` row.
- Smallest good next checkpoint: decide whether fills sharing one `event_group_id` should be consolidated into one later report row.

## Non-goals / intentionally narrow boundaries

- Spot `@N` asset resolution through `fetchSpotMeta` is not itself under review here.
