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
- the IRS more-than-one-year calendar rule for the long-term holding period (anniversary-day sales are short-term; Feb 29 acquisitions roll to Mar 1; dates are UTC trade dates)
- strict IR parsing: malformed decimal strings and unknown schema versions fail loudly instead of being silently misread

### Current-behavior-only

- many real-wallet cases that reach the core through current normalization snapshots
- any behavior that is guarded only by current-behavior fixtures rather than evidence-backed semantic proof

### Unsupported but surfaced

- normalized cases that represent economically meaningful activity but do not yet have a final supported tax interpretation in the core

## Supplemental output channels

The core now supports three structured output channels beyond the 8949 CSV:

1. **Income report** (`income.csv`): Ordinary income rows are emitted as structured `IncomeEntry` records with timestamp, tx id, asset, amount, and USD value. Written only when income entries exist.

2. **Funding expense report** (`funding_expenses.csv`): Negative Hyperliquid funding is emitted as structured `FundingExpense` entries. These are informational expense records, not tax-semantic claims. The funding expense does not affect USDC inventory (no lot consumption). Written only when funding expenses exist.

3. **Perp realized PnL report** (`perp_pnl.csv`): Perp close events with exchange-reported `ClosedPnl` are emitted as `PerpPnlEntry` records. Canonical output intentionally keeps these rows separate from 8949 rather than forcing a derivative-specific cost-basis/proceeds representation into the 8949 CSV. Written only when perp PnL entries exist.

4. **Transfers report** (`transfers.csv`): Own-wallet transfer rows are recorded as `TransferEntry` rows (non-taxable, no FIFO effect) so they stay visible downstream instead of disappearing in the core. Written only when transfer rows exist.

Supplemental files are removed when their channel is empty on a re-run, so stale outputs from earlier runs cannot linger. An optional `--tax-year YYYY` flag restricts which rows are RENDERED in all reports (8949 by disposal date); lot accounting always processes full history. When any transaction cannot be processed, the reports are still written but the core prints an explicit incompleteness error and exits nonzero.

Canonical output keeps income, perp realized PnL, and funding cash flows in separate supplemental channels from 8949.

## Immediate review priorities

The highest-priority open semantics questions for the core are:

- wallet-by-wallet lot tracking (Rev. Proc. 2024-28, required for 2025+ tax years) — see `docs/repo/REVIEW.md`
- incremental adoption of `asset_canonical` for lot tracking keys
- in-kind fee (gas) consumption from inventory
