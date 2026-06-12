# Normalized IR Review

## Purpose

This document tracks grounded gaps between `docs/ir/SPEC.md` and the current implementation in the Go normalization pipeline, the shared JSON contract, and the Haskell consumer types.

## Open review items



## Resolved (kept as one-line history; details in git log)

- **Canonical asset identity and display identity collapse into one IR field** — Decision Option B adopted — `AssetAmount` gained an optional `asset_canonical` field in both Go (`go/types/types.go`) and Haskell (`haskell/src/Types.hs`). The Helius normalizer populates `asset_canonical` with the So...
- **The IR now distinguishes intentional multi-row preservation from flat row output** — `types.Transaction` gained optional `event_group_id` and `split_reason` fields in Go and Haskell. The normalization pipeline now uses them to preserve row-group context across sources: Hyperliquid fills use `api_fill_...
- **Normalization skip diagnostics are structured and persisted by audit capture** — `buildPayload` in `go/cmd/main.go` now calls `NormalizeWithDiagnostics` and returns `[]SkippedRow` alongside the payload. `WriteCaptureArtifacts` in `go/audit/audit.go` persists skipped rows as a `.skipped.json` sidec...
