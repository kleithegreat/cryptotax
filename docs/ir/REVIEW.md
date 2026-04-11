# Normalized IR Review

## Purpose

This document tracks grounded gaps between `docs/ir/SPEC.md` and the current implementation in the Go normalization pipeline, the shared JSON contract, and the Haskell consumer types.

## Open review items

### Canonical asset identity and display identity collapse into one IR field

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `human-decision blocker`
- Why this is a spec/implementation gap: `docs/ir/SPEC.md` says asset fields should preserve source-backed identity and keep canonical identity separate from display identity as far upstream as the schema allows. The current IR contract only exposes one `asset` string per leg.
- Current implementation evidence: `fetcher.RawTransaction` in `go/fetcher/fetcher.go` can carry `Asset` plus `AssetSymbol`. `convertSwap`, `convertTransfer`, and `convertGeneric` in `go/fetcher/helius.go` preserve that pair, but `normalizeHelius` in `go/normalize/normalize.go` collapses it through `heliusDisplayAsset` into `types.AssetAmount.Asset` in `go/types/types.go`. Downstream code then keys off that single string through `transfer.sameAsset` in `go/transfer/match.go`, `accumulateAsset` in `go/audit/audit.go`, and `AssetSymbol`, `Lot.acquire`, and `Lot.dispose` in `haskell/src/Types.hs`, `haskell/src/Lot.hs`, and `haskell/src/GainLoss.hs`.
- Desired direction implied by the spec: canonical identity should stay explicit across the Go-to-Haskell boundary, while display labels remain secondary metadata instead of the accounting key.
- What blocks resolution: separating canonical and display identity would change the normalized IR contract in `go/types/types.go` and `haskell/src/Types.hs`, which needs a human decision.
- Smallest good next checkpoint: decide whether `AssetAmount` grows separate canonical and display fields or whether the IR needs a parallel asset-metadata container before implementation work starts.

### The IR cannot distinguish intentional multi-row preservation from current-behavior splitting

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `support-boundary gap`
- Why this is a spec/implementation gap: `docs/ir/SPEC.md` calls out multi-leg support boundaries and says the repository should clarify when a split-row representation is intentional versus merely current behavior. The current row contract has no field for grouping semantics or split reason.
- Current implementation evidence: `normalizeEVM` in `go/normalize/normalize.go` emits one row per raw Etherscan movement. `convertTransfer` and `convertGeneric` in `go/fetcher/helius.go` can emit many rows for one Helius transaction. `fetchFills` in `go/fetcher/hyperliquid.go` emits one raw row per fill, while `docs/hyperliquid/ARCHITECTURE.md` documents that partial fills sharing one hash stay separate. `MatchTransfers` in `go/transfer/match.go` can relabel rows after normalization, but it does not add grouping metadata to `types.Transaction`.
- Desired direction implied by the spec: the IR should let audit and downstream accounting tell the difference between deliberate conservative multi-row output and a representation that is only a temporary approximation.
- What blocks resolution: `types.Transaction` currently has only `ID`, `RawType`, and the three asset legs, so adding grouping or representation metadata would be a cross-domain IR contract change.
- Smallest good next checkpoint: decide whether the IR needs explicit event-grouping or representation-tier metadata before multi-leg support expands further.

### Normalization failures are printed and skipped instead of becoming durable audit evidence

- Status: `open`
- Issue type: `implementation gap`, `evidence gap`
- Why this is a spec/implementation gap: `docs/ir/SPEC.md` requires unsupported behavior to remain explicit and `docs/repo/SPEC.md` requires evidence retention. Today a raw row that fails normalization can disappear from the captured payload entirely.
- Current implementation evidence: `Normalize` in `go/normalize/normalize.go` collects per-row errors, prints them to stderr, and continues. `WriteCaptureArtifacts` in `go/audit/audit.go` persists the normalized payload plus the re-run command, but it does not persist normalization warnings or skipped-row details.
- Desired direction implied by the spec: audit capture should leave a durable, machine-readable record of skipped normalization cases so a reviewer can tell whether the payload is complete.
- What blocks resolution: the current capture contract persists only the payload JSON and the command sidecar, so there is no agreed place for structured normalization failures.
- Smallest good next checkpoint: persist normalization warnings beside `audit capture` output in a stable machine-readable artifact.

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the IR to invent swap, bridge, or perp semantics that upstream sources do not support.
- `usd_value: "0"` remains valid when valuation is unresolved; the open issue is how to make that state auditable without guessing.
