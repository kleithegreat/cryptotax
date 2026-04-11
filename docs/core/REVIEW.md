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

### The core has no stable output channel for modeled income or unsupported rows

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `support-boundary gap`
- Why this is a spec/implementation gap: `docs/core/SPEC.md` says the core should treat supported income-like events and make final output reflect the semantics actually modeled. The core already models more than 8949 disposals, but its stable file output is still disposal-only CSV.
- Current implementation evidence: `recordIncomeReceipt` in `haskell/src/GainLoss.hs` stores income-like results in `prIncome`, and `handleFunding` stores unsupported expense cases in `prErrors`. `Main.main` in `haskell/app/Main.hs` writes only `render8949CSV (prGainLosses result)` to disk and prints `prErrors` plus total income to stderr.
- Desired direction implied by the spec: final output should have a stable way to carry gain/loss rows, modeled income-like receipts, and unsupported-but-surfaced cases without pretending that everything is an 8949 disposal.
- What blocks resolution: adding durable non-8949 output changes the core's long-term output contract and needs a human decision on format and support claims.
- Smallest good next checkpoint: decide whether the core grows a second structured report or a richer multi-section output before broader support claims are made.

### Negative `funding_payment` is preserved but has no decided accounting or output semantics

- Status: `needs_human_decision`
- Issue type: `human-decision blocker`
- Why this is a spec/implementation gap: `docs/core/SPEC.md` names negative Hyperliquid funding as an immediate review priority. The core receives the event explicitly, but it still has no decided ordinary-expense treatment or final output behavior.
- Current implementation evidence: `handleFunding` in `haskell/src/GainLoss.hs` converts negative `funding_payment` rows into `prErrors` text only. `prop_negativeFundingIsExplicitlyUnsupported` in `haskell/test/Spec.hs` freezes that behavior. `docs/known-transactions.md` keeps both positive and negative funding cases pending human review.
- Desired direction implied by the spec: keep negative funding visible without reinterpreting it as spot activity, and decide how ordinary expense treatment should appear in final output, if at all.
- What blocks resolution: tax and output semantics for negative funding need a human decision, and the current real-wallet funding exemplars still require human verification.
- Smallest good next checkpoint: choose one explicit output posture for negative funding expense so follow-on implementation can stay within a documented support boundary.

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the core to reverse-engineer source-specific meaning from `raw_type`, counterparty data, or wallet traces.
- Exact arithmetic and FIFO behavior are not review items here; they are already implemented and covered by the current tests.
