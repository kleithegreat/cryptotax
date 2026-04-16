# Normalized IR Review

## Purpose

This document tracks grounded gaps between `docs/ir/SPEC.md` and the current implementation in the Go normalization pipeline, the shared JSON contract, and the Haskell consumer types.

## Open review items

### Canonical asset identity and display identity collapse into one IR field

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option B adopted — `AssetAmount` gained an optional `asset_canonical` field in both Go (`go/types/types.go`) and Haskell (`haskell/src/Types.hs`). The Helius normalizer populates `asset_canonical` with the Solana mint when a source-backed display symbol is present. Old payloads without the field parse as `Nothing` (backward compatible). Downstream consumers (lot tracking, transfer matching, audit accumulation) have not yet adopted canonical identity; that adoption is incremental and separate from this schema change.

### The IR now distinguishes intentional multi-row preservation from flat row output

- Status: `done`
- Issue type: resolved
- Resolution: `types.Transaction` gained optional `event_group_id` and `split_reason` fields in Go and Haskell. The normalization pipeline now uses them to preserve row-group context across sources: Hyperliquid fills use `api_fill_granularity`, Helius transfer/generic rows use `wallet_touching_leg_preservation`, Robinhood synthetic buy/sell pairs use `synthetic_1099da_row`, and Etherscan rows use `source_transfer_granularity`. Hyperliquid funding rows also preserve `market` plus stable `event_group_id` context.

### Normalization skip diagnostics are structured and persisted by audit capture

- Status: `done`
- Issue type: resolved
- Resolution: `buildPayload` in `go/cmd/main.go` now calls `NormalizeWithDiagnostics` and returns `[]SkippedRow` alongside the payload. `WriteCaptureArtifacts` in `go/audit/audit.go` persists skipped rows as a `.skipped.json` sidecar alongside the normalized payload and re-run command. The `audit capture` command threads skipped rows through and reports the sidecar path to stderr. The `run` command logs skipped rows to stderr but does not persist the sidecar (appropriate since `run` pipes to the Haskell core, not to audit output).

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the IR to invent swap, bridge, or perp semantics that upstream sources do not support.
- `usd_value: "0"` remains valid when valuation is unresolved; the open issue is how to make that state auditable without guessing.
