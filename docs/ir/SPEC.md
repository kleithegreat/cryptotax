# Normalized IR Spec

This document defines the intended contract for the normalized JSON payload produced by the Go pipeline and consumed by the Haskell core.

## Purpose

The normalized IR is the boundary between:

- source-specific ingestion and normalization logic
- downstream accounting and tax-output generation

The IR should preserve enough information for the core to behave correctly without forcing the core to reverse-engineer source-specific fetcher details.

## Design goals

1. Preserve economic meaning as honestly as possible.
2. Preserve unsupported or ambiguous cases explicitly.
3. Avoid inventing semantics that are not supported by evidence.
4. Keep the IR stable enough for regression testing and downstream accounting.

## Row-level support tiers

Each normalized behavior that emits a row should be understood as one of:

- supported
- current-behavior-only
- unsupported but surfaced

The row format itself does not currently encode that tier directly, so the relevant domain docs and review docs must state it.

## Intended invariants

### Identity

- Each normalized row must carry a stable transaction identifier and wallet identity.
- Wallet identity should remain explicit rather than being inferred later from source ownership assumptions.

### Assets

- Asset fields should represent source-backed identity or explicitly documented normalized display identity.
- The system must not rewrite an address-like or mint-like identifier into a guessed symbol.
- If canonical identity and display identity differ, the repository should preserve that distinction as far upstream as the schema allows.

### USD values

- USD values should only be populated from supported valuation logic.
- `0` USD is allowed when valuation is genuinely unresolved or zero, but unresolved valuation should be auditable and reviewable.

### Unsupported behavior

- If a transaction cannot yet be modeled fully, the repository should still emit a conservative representation when possible.
- Unsupported handling must not silently drop economically meaningful legs.

## Asset identity contract

`AssetAmount` now carries an optional `asset_canonical` field alongside the existing `asset` display field. When canonical identity differs from display identity (e.g. a Solana mint vs a source-backed symbol like "USDC"), the normalizer populates `asset_canonical` with the canonical identifier. Old payloads without this field parse as `Nothing` (backward compatible).

Each downstream consumer can opt into canonical identity at its own pace. The `asset` field retains its current mixed-semantics role (symbol for some chains, mint for others) to avoid breaking existing output.

## Perp transaction types

The IR now supports `perp_open` and `perp_close` transaction types for Hyperliquid perpetual fills. These are distinct from spot `buy`/`sell` to prevent perp positions from contaminating spot FIFO accounting.

`perp_close` rows carry an optional `closed_pnl` field containing the exchange-reported realized PnL. This field is populated from the Hyperliquid API's `ClosedPnl` value during normalization.

## Event grouping and market context

Normalized `Transaction` rows may now carry three additional optional fields:

- `market`: source-backed market context when the row needs it, currently used for Hyperliquid funding rows
- `event_group_id`: a stable key tying related normalized rows back to one source event or representation group
- `split_reason`: a short explanation for why one source event is represented as many normalized rows

These fields are additive and backward compatible. They exist so audit and downstream accounting can tell the difference between one flat source row and a conservative multi-row representation.

## Near-term design priority

The repository should continue clarifying the IR around:

- incremental consumer adoption of `asset_canonical` for lot tracking, transfer matching, and audit accumulation
- downstream use of `event_group_id` and `split_reason` in audit and support-boundary work
- support boundaries for multi-leg events now that representation metadata is available
