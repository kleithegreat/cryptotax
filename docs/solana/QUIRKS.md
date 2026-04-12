# Solana Quirks

This document captures non-obvious Helius and Solana source-specific gotchas that are easy to miss from code alone.

## 1. Raw mint strings appear as normalized asset names when Helius provides no symbol

When Helius enhanced transactions do not supply a source-backed symbol for a token transfer, the normalized `asset` field contains the raw mint string (e.g., `CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP`). This is by design: the pipeline uses `heliusDisplayAsset` which falls back to the mint when no symbol exists, rather than guessing a display name.

These mint-string assets will usually have `usd_value: "0"` because the pricing provider does not know their CoinGecko ID. The audit summary flags them through `buildSuspiciousAssetRow` heuristics.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `sol-pumpfun-zero-usd` entry shows `CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP` as the received asset at zero USD.

## 2. Helius `TokenAmount` is rendered through a float64 path with 9 decimal places

`formatTokenAmount` in `go/fetcher/helius.go` converts Helius `TokenAmount` values through a `float64` intermediate and emits 9 decimal places. This means very large or very precise token amounts may lose precision at the edges of float64 representability. For typical Solana token amounts this is adequate, but it is worth noting for any future high-precision or large-supply token cases.

## 3. Fee is attached only to the first outbound row in multi-row parses

`attachFeeToPrimaryHeliusRow` in `go/fetcher/helius.go` assigns the full Solana signature fee to the first outbound raw row produced from a transaction. In multi-row parses (e.g., `convertTransfer` or `convertGeneric` emitting several rows), only one row carries the fee and the rest have zero fee. This keeps fee totals conserved but means downstream consumers cannot assume every row with `fee: 0` was genuinely fee-free.

Example: the `sol-addresslike-mint` entry in `docs/known-transactions.md` shows 10 rows from one transaction, where only the first outbound row carries the SOL fee.

## 4. Helius enhanced endpoint has a fallback for specific failure codes

`shouldFallbackHeliusEnhanced` in `go/fetcher/helius.go` detects HTTP 530 and error code 1016 responses from the primary enhanced-transactions endpoint and retries against `LegacyEnhancedURL`. This fallback exists because the primary endpoint occasionally returns these codes for valid signatures. If the fallback is removed or the legacy URL becomes unavailable, some valid signatures may fail to parse.
