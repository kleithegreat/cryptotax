# Normalized IR Review

## Purpose

This document tracks grounded gaps between `docs/ir/SPEC.md` and the current implementation in the Go normalization pipeline, the shared JSON contract, and the Haskell consumer types.

## Open review items

### Canonical asset identity and display identity collapse into one IR field

- Status: `done`
- Issue type: resolved
- Resolution: Decision Option B adopted — `AssetAmount` gained an optional `asset_canonical` field in both Go (`go/types/types.go`) and Haskell (`haskell/src/Types.hs`). The Helius normalizer populates `asset_canonical` with the Solana mint when a source-backed display symbol is present. Old payloads without the field parse as `Nothing` (backward compatible). Downstream consumers (lot tracking, transfer matching, audit accumulation) have not yet adopted canonical identity; that adoption is incremental and separate from this schema change.

### The IR cannot distinguish intentional multi-row preservation from current-behavior splitting

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `support-boundary gap`
- Why this is a spec/implementation gap: `docs/ir/SPEC.md` calls out multi-leg support boundaries and says the repository should clarify when a split-row representation is intentional versus merely current behavior. The current row contract has no field for grouping semantics or split reason.
- Current implementation evidence: `normalizeEVM` in `go/normalize/normalize.go` emits one row per raw Etherscan movement. `convertTransfer` and `convertGeneric` in `go/fetcher/helius.go` can emit many rows for one Helius transaction. `fetchFills` in `go/fetcher/hyperliquid.go` emits one raw row per fill, while `docs/hyperliquid/ARCHITECTURE.md` documents that partial fills sharing one hash stay separate. `MatchTransfers` in `go/transfer/match.go` can relabel rows after normalization, but it does not add grouping metadata to `types.Transaction`.
- Desired direction implied by the spec: the IR should let audit and downstream accounting tell the difference between deliberate conservative multi-row output and a representation that is only a temporary approximation.
- What blocks resolution: `types.Transaction` currently has only `ID`, `RawType`, and the three asset legs, so adding grouping or representation metadata would be a cross-domain IR contract change.
- Smallest good next checkpoint: decide whether the IR needs explicit event-grouping or representation-tier metadata before multi-leg support expands further.

### Normalization skip diagnostics are structured and persisted by audit capture

- Status: `done`
- Issue type: resolved
- Resolution: `buildPayload` in `go/cmd/main.go` now calls `NormalizeWithDiagnostics` and returns `[]SkippedRow` alongside the payload. `WriteCaptureArtifacts` in `go/audit/audit.go` persists skipped rows as a `.skipped.json` sidecar alongside the normalized payload and re-run command. The `audit capture` command threads skipped rows through and reports the sidecar path to stderr. The `run` command logs skipped rows to stderr but does not persist the sidecar (appropriate since `run` pipes to the Haskell core, not to audit output).

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the IR to invent swap, bridge, or perp semantics that upstream sources do not support.
- `usd_value: "0"` remains valid when valuation is unresolved; the open issue is how to make that state auditable without guessing.
