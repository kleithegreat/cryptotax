# Solana Architecture

## Purpose

The current Solana pipeline fetches Helius enhanced transactions, converts them into raw Solana rows, normalizes those rows into the shared IR, and relies on audit tooling to surface unresolved identity and valuation cases.

## Current ownership boundary

- `go/fetcher/helius.go` owns Helius RPC and enhanced-transaction ingestion plus raw-row construction.
- `go/normalize/normalize.go` owns Solana normalization in `normalizeHelius`.
- `go/price/coingecko.go` owns the current USD lookup path used by Solana normalization.
- `go/audit/audit.go` owns post-normalization surfacing for zero-USD and suspicious-asset Solana rows.
- The Haskell core does not know anything Solana-specific beyond the normalized rows it receives.

## Main files and named constructs

- `go/fetcher/helius.go`
- `go/normalize/normalize.go`
- `go/price/coingecko.go`
- `go/audit/audit.go`
- `go/fetcher/helius_test.go`
- `go/normalize/normalize_test.go`

Important named constructs:

- `Helius`
- `Fetch`
- `getSignatures`
- `parseEnhanced`
- `convertHeliusTx`
- `convertSwap`
- `convertTransfer`
- `convertGeneric`
- `attachFeeToPrimaryHeliusRow`
- `normalizeHelius`
- `heliusDisplayAsset`
- `heliusPriceLookupAsset`

## Data flow

- `buildPayload` in `go/cmd/main.go` creates `fetcher.NewHelius` for each `--sol-wallet`.
- `Helius.Fetch` first calls `getSignatures`, which paginates `getSignaturesForAddress` and drops signatures whose RPC result contains a non-nil `Err`.
- `parseEnhanced` then posts batched signature lists to the Helius enhanced-transactions endpoint. `shouldFallbackHeliusEnhanced` triggers a retry against `LegacyEnhancedURL` for the current 530 and 1016 failure shapes.
- `convertHeliusTx` dispatches on `heliusEnhancedTx.Type`.
- `convertSwap` produces one `fetcher.RawTransaction` with `Asset` and `Asset2` for the wallet-touching sent and received legs. It preserves the raw mint in `Asset` or `Asset2` and carries any source-backed symbol separately in `AssetSymbol` or `Asset2Symbol`.
- `convertTransfer` emits one raw row per wallet-touching token transfer or native transfer. `convertGeneric` does the same for non-`TRANSFER` non-`SWAP` Helius transaction types.
- `attachFeeToPrimaryHeliusRow` attaches the full signature fee to the first outbound raw row only so fee totals stay conserved across multi-row parses.
- `normalizeHelius` turns any raw row with `Asset2` into a normalized `swap`. For non-swap rows it uses `classifyAddressMovement` to choose between `transfer_out`, `sell`, and `transfer_in`.
- `normalizeHelius` uses `heliusPriceLookupAsset` for USD lookup and `heliusDisplayAsset` for the normalized `asset` string. If Helius supplies a separate symbol, the normalized row uses that symbol; otherwise it uses the mint string as the displayed asset.

## Outputs / side effects

- Solana rows enter the IR with `source="helius"` and `chain="solana"`.
- `raw_type` currently comes from `SWAP/<source>`, `TRANSFER`, or the original Helius transaction `Type`.
- Solana fees are currently attached as `fee.asset = "SOL"` only on the primary outbound row.
- Audit summaries flag Solana rows through `buildZeroUSDValueRow` and `buildSuspiciousAssetRow` when a mint-like asset string also has unresolved valuation.

## Current support boundary

### Implemented behavior

- Helius RPC plus enhanced parsing is the only Solana fetch path in the codebase.
- Simple wallet-touching swap-like rows and transfer-like rows are normalized end to end.
- Raw Helius rows preserve mint identity and source-backed symbol separately before normalization.
- Audit surfacing already highlights address-like Solana assets and zero-USD legs.

### Current-behavior-only checkpoints

- `go/fetcher/helius_test.go` freezes the current enhanced endpoint envelope, fallback behavior, and raw mint-versus-symbol handling.
- `go/normalize/normalize_test.go` freezes the current normalized behavior for mint-only rows, symbol-backed rows, and the PUMP_FUN zero-USD case.
- `go/audit/testdata/real-wallet/solana-pumpfun-zero-usd.expected.json` freezes one documented real-wallet Solana case.
- `docs/known-transactions.md` records additional current-behavior review targets such as `sol-dflow-swap` and `sol-addresslike-mint`.

### Unsupported but surfaced behavior

- Mint-only assets with no supported pricing path stay visible with `usd_value: "0"`.
- Same-asset SOL-for-SOL swap-like rows remain visible when Helius presents one outbound and one inbound native leg.
- Multi-leg transactions that expand into many transfer rows remain visible as multiple normalized rows instead of being collapsed into a guessed economic event.

## Current known approximations or conservative behavior

- `convertSwap` chooses one wallet-touching outbound leg and one wallet-touching inbound leg. It does not model richer routing paths or internal account churn.
- `normalizeHelius` defaults inbound non-swap rows to `transfer_in`, not `income`.
- `formatTokenAmount` renders Helius `TokenAmount` through a `float64` path and emits 9 decimal places.
- `price.Provider` only knows the small symbol map in `coingeckoIDs`, so mint-only Solana assets usually remain at `usd_value: "0"`.

## Notable current divergence from spec

- The raw Solana path preserves canonical mint identity separately from symbol, but `normalizeHelius` collapses that pair into one normalized `asset` string. When Helius supplies a symbol, the normalized row keeps the symbol and no longer carries the mint. This is a concrete mismatch with the spec's canonical-identity preference and likely belongs in a future `docs/solana/REVIEW.md`.
