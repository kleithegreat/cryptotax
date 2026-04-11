# Solana Review

## Purpose

This document tracks grounded gaps between `docs/solana/SPEC.md` and the current Helius-based Solana implementation.

## Open review items

### Canonical mint identity is lost when a source-backed symbol exists

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `human-decision blocker`
- Why this is a spec/implementation gap: `docs/solana/SPEC.md` says canonical Solana identity should prefer the mint and that any display symbol should remain separate metadata. The current normalized row keeps only one `asset` string, so the mint disappears as soon as Helius provides a symbol.
- Current implementation evidence: `fetcher.RawTransaction` in `go/fetcher/fetcher.go` can carry both `Asset` and `AssetSymbol`. `convertSwap`, `convertTransfer`, and `convertGeneric` in `go/fetcher/helius.go` preserve that split. `normalizeHelius` in `go/normalize/normalize.go` then writes only `heliusDisplayAsset(...)` into `types.AssetAmount.Asset`, and `TestNormalizeHeliusUsesSourceBackedSymbolForDisplayAndPricing` in `go/normalize/normalize_test.go` freezes the current symbol-first behavior.
- Desired direction implied by the spec: canonical mint identity should remain explicit across normalization, while any display symbol stays separate and optional.
- What blocks resolution: the shared IR in `go/types/types.go` and `haskell/src/Types.hs` has only one `asset` field per leg, so keeping both identities requires an IR contract decision.
- Smallest good next checkpoint: decide the IR contract for canonical-versus-display identity, then update Solana normalization to carry both.

### Unresolved mint-only valuation is encoded as `usd_value: "0"` plus audit heuristics

- Status: `open`
- Issue type: `implementation gap`, `evidence gap`
- Why this is a spec/implementation gap: `docs/solana/SPEC.md` allows unresolved Solana valuations to remain at `0` USD, but it also says audit output should make the unresolved state easy to review. The current pipeline uses the same literal `0` shape for lookup misses and for any leg whose USD value is genuinely zero.
- Current implementation evidence: `normalizeHelius` in `go/normalize/normalize.go` calls `resolveUSDPrice`, and `resolveUSDPrice` returns `"0"` whenever `price.Provider.Lookup` cannot price the asset. `Lookup` in `go/price/coingecko.go` only knows the small `coingeckoIDs` map. After normalization, `buildZeroUSDValueRow` and `buildSuspiciousAssetRow` in `go/audit/audit.go` infer review need heuristically from `usd_value` and the asset string.
- Desired direction implied by the spec: reviewers should be able to tell when a Solana leg is unpriced because identity or pricing support is unresolved, not merely see a literal zero.
- What blocks resolution: neither `types.AssetAmount` nor the audit summary schema carries valuation provenance or an explicit unresolved-valuation reason.
- Smallest good next checkpoint: add explicit unresolved-valuation surfacing for Solana rows before expanding valuation support claims.

### Current multi-row Solana output cannot clearly separate conservative preservation from routing artifacts

- Status: `needs_human_verification`
- Issue type: `support-boundary gap`, `human-verification blocker`
- Why this is a spec/implementation gap: `docs/solana/SPEC.md` says economically meaningful legs should be preserved conservatively, even when one transaction contains many internal moves. Current output is frozen for representative cases, but the code cannot yet show which rows are intended conservative legs and which are parser artifacts.
- Current implementation evidence: `convertSwap` in `go/fetcher/helius.go` chooses one wallet-touching outbound leg and one inbound leg. `convertTransfer` and `convertGeneric` emit one row per wallet-touching transfer. `attachFeeToPrimaryHeliusRow` assigns the full fee to the first outbound row only. `docs/known-transactions.md` documents `sol-dflow-swap` and `sol-addresslike-mint` as pending human review because the resulting rows may be same-asset routing artifacts or one transaction exploded into many ambiguous rows.
- Desired direction implied by the spec: preserve conservative visibility without making same-asset SOL rows or large row explosions look like settled economic semantics.
- What blocks resolution: the representative real-wallet Solana cases still need human verification, and any durable fix likely depends on the IR decision about grouping or representation metadata.
- Smallest good next checkpoint: finish human review of the documented `sol-dflow-swap` and `sol-addresslike-mint` exemplars so the repo can separate acceptable conservative output from parser artifacts.

## Non-goals / intentionally narrow boundaries

- No guessed token symbols beyond source-backed Helius data.
- No automatic promotion of inbound Solana rows from `transfer_in` to `income` without richer source evidence.
