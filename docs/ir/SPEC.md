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

## Known current limitation

The normalized schema currently has one `asset` string per leg, so it does not fully encode canonical asset identity separately from display symbol/label. Where upstream code preserves that distinction more faithfully than the normalized schema can express, that limitation should be documented explicitly.

## Near-term design priority

The repository should continue clarifying the IR around:

- canonical vs display asset identity
- support boundaries for multi-leg events
- when a split-row representation is intentional vs merely current behavior
