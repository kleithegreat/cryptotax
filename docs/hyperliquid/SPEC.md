# Hyperliquid Spec

This document defines the intended support boundary for Hyperliquid ingestion, normalization, and downstream accounting.

## Source of truth

The current Hyperliquid pipeline is based on Hyperliquid API data plus any human-reviewed evidence retained in repository docs.

Agents must preserve source-backed details and avoid presenting perp activity as fully solved spot semantics when that is not actually true.

## Current support boundary

The bullets below describe current implemented behavior. Per `docs/repo/TASK_DAG.md`, real-wallet Hyperliquid support-claim upgrades remain blocked by `verification.expand_human_verified_exemplars`; the separate funding and perp reports are implementation checkpoints, not human-verified tax-correctness claims.

### Supported

- funding rows represented explicitly as `funding_payment`
- funding rows preserve market context in `market`
- positive funding preserved as received USDC and available to downstream income handling

### Current-behavior-only

- negative funding preserved as sent USDC and emitted as structured `FundingExpense` in `funding_expenses.csv`
- perp fills classified as `perp_open` / `perp_close`, quarantined from spot FIFO
- `ClosedPnl` propagated from the Hyperliquid API through normalization to the core for `perp_close`
- perp close realized PnL emitted in `perp_pnl.csv` using exchange-reported `ClosedPnl`
- grouped Hyperliquid rows carry `event_group_id`; fill rows use `split_reason = "api_fill_granularity"`

### Unsupported but surfaced

- multi-row fill groupings that may represent one economic perp event
- partial-fill `ClosedPnl` consolidation (each API fill is one PnL entry)
- negative funding does not consume USDC inventory (informational expense only)
- source `id` for funding rows may still be the all-zero hash even though market and `event_group_id` are preserved

## Rules

- do not silently erase funding outflows
- do not merge funding cash flows into perp PnL — keep them in separate output channels
- do not route perp fills through spot FIFO (no phantom lots)
- preserve enough information for later accounting improvements and human review

## Immediate priority cases

The current highest-priority Hyperliquid work is:

- evidence retention and linkage for funding rows (source `id` can still be weak even though `market` is preserved)
- human verification of real-wallet exemplars before upgrading support claims
- partial-fill consolidation for perp closes
