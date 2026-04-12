# Financial Core Spec

This document defines the intended scope of the Haskell financial core.

## Purpose

The Haskell core consumes normalized events and produces accounting and tax-oriented output for supported cases.

It is responsible for applying accounting semantics, not for guessing what upstream source data meant.

## Responsibilities

The core should own:

- FIFO lot accounting
- basis and proceeds calculations for supported dispositions
- treatment of supported income-like events
- generation of final tax-oriented output for supported cases

The core should not own:

- source-specific transaction reconstruction
- wallet-ownership inference
- symbol guessing
- source-truth validation against explorers or CSV exports

## Core invariants

- arithmetic must remain exact; do not introduce floating-point tax math
- supported disposals must consume inventory consistently
- unsupported or ambiguous events must not silently disappear
- final output must reflect the semantics actually modeled, not stronger claims

## Current support intent

### Supported

- the small local end-to-end golden fixture covering buy, sell, and own-wallet transfer behavior
- FIFO lot accounting for supported normalized events that map cleanly into current core semantics

### Current-behavior-only

- many real-wallet cases that reach the core through current normalization snapshots
- any behavior that is guarded only by current-behavior fixtures rather than evidence-backed semantic proof

### Unsupported but surfaced

- normalized cases that represent economically meaningful activity but do not yet have a final supported tax interpretation in the core

## Supplemental output channels

The core now supports two structured output channels beyond the 8949 CSV:

1. **Funding expense report** (`funding_expenses.csv`): Negative Hyperliquid funding is emitted as structured `FundingExpense` entries. These are informational expense records, not tax-semantic claims. The funding expense does not affect USDC inventory (no lot consumption). Written only when funding expenses exist.

2. **Perp realized PnL report** (`perp_pnl.csv`): Perp close events with exchange-reported `ClosedPnl` are emitted as `PerpPnlEntry` records. This is intentionally separate from 8949 because the cost-basis/proceeds representation for derivative PnL on Form 8949 is unresolved. Written only when perp PnL entries exist.

Canonical output keeps perp realized PnL and funding cash flows separate. A derived net-performance summary may be added later.

## Immediate review priorities

The highest-priority open semantics questions for the core are:

- how broader income/expense handling should be represented when upstream normalization is conservative but incomplete
- incremental adoption of `asset_canonical` for lot tracking keys
- the 8949 cost-basis/proceeds representation for perp PnL (currently surfaced in a separate report)
