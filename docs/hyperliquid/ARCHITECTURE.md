# Hyperliquid Architecture

## Purpose

The current Hyperliquid pipeline fetches public fill and funding history, converts those records into raw rows, normalizes them into the shared IR, and lets the Haskell core consume the resulting funding, buy, sell, or swap rows.

## Current ownership boundary

- `go/fetcher/hyperliquid.go` owns Hyperliquid API access and raw-row construction.
- `go/normalize/normalize.go` owns Hyperliquid normalization in `normalizeHyperliquid`.
- `haskell/src/GainLoss.hs` owns the downstream handling of normalized `funding_payment` rows.
- The codebase does not currently have a separate perp-position model. Hyperliquid perp fills therefore cross the boundary through the same IR types used for spot-like rows.

## Main files and named constructs

- `go/fetcher/hyperliquid.go`
- `go/normalize/normalize.go`
- `haskell/src/GainLoss.hs`
- `haskell/test/Spec.hs`

Important named constructs:

- `Hyperliquid`
- `Fetch`
- `fetchFills`
- `fetchFunding`
- `fetchSpotMeta`
- `hlFill`
- `hlFunding`
- `normalizeHyperliquid`
- `handleFunding`

## Data flow

- `buildPayload` in `go/cmd/main.go` creates `fetcher.NewHyperliquid` for each `--hl-wallet`.
- `Hyperliquid.Fetch` first calls `fetchSpotMeta` and builds the current `@N` to token-name mapping for spot identifiers.
- `fetchFills` paginates `userFillsByTime` from `startTime = 0`, advancing with the last fill time plus one millisecond.
- Each fill becomes one `fetcher.RawTransaction` with `ID = Hash`, `Asset = Coin`, `Amount = Sz`, `USDPrice = Px`, `Fee`, `FeeAsset`, and `RawType = Dir`. If `Coin` starts with `@`, `Fetch` resolves it through the spot metadata map before writing the raw row.
- `fetchFunding` paginates `userFunding` the same way. Each funding entry becomes one raw row with `Asset = Delta.Coin`, `Amount = Delta.USDC`, and `RawType = "funding"`.
- `normalizeHyperliquid` turns `RawType == "funding"` into `tx_type = funding_payment`. Negative amounts become outbound USDC in `Sent`; positive amounts become inbound USDC in `Received`.
- Non-funding rows are classified by the upper-cased `RawType`. `OPEN LONG` and `OPEN SHORT` become `buy`; `CLOSE LONG` and `CLOSE SHORT` become `sell`; all other fill directions currently become `swap` rows against USDC.
- `normalizeHyperliquid` prices fees directly in USD when `FeeAsset == "USDC"` and otherwise falls back to `resolveUSDPrice`.
- In the Haskell core, `handleFunding` records positive funding as income and surfaces negative funding as an unsupported expense error.

## Outputs / side effects

- Hyperliquid rows enter the IR with `source="hyperliquid"` and `chain="hyperliquid"`.
- Funding rows preserve `raw_type = "funding"`.
- Fill rows preserve Hyperliquid's direction string in `raw_type`.
- The code does not write a Hyperliquid-specific artifact by itself; audit capture is the current persistence layer for normalized review snapshots.

## Current support boundary

### Implemented behavior

- Funding rows are explicitly preserved as `funding_payment`.
- Negative funding is preserved as an outbound USDC leg instead of being dropped.
- Positive funding is preserved as an inbound USDC leg and reaches the Haskell income path.
- Spot identifier resolution through `fetchSpotMeta` is implemented for `@N` fill assets.

### Current-behavior-only checkpoints

- `go/audit/testdata/real-wallet/hyperliquid-funding.expected.json` freezes the current positive and negative funding normalization.
- `haskell/test/Spec.hs` freezes the current downstream behavior for positive and negative funding rows.
- `docs/known-transactions.md` records current-behavior review targets for perp open and close cases such as `hl-open-long-btc` and `hl-close-short-sol`.

### Unsupported but surfaced behavior

- Negative funding reaches the core as an explicit unsupported expense warning instead of a final supported expense output.
- Perp open and close fills are still visible in the IR, but they are not modeled as perp positions or realized PnL.
- Partial fills that share one hash remain separate normalized rows when the API returns them separately.

## Current known approximations or conservative behavior

- The main perp approximation enters in `normalizeHyperliquid`, where `OPEN LONG`, `OPEN SHORT`, `CLOSE LONG`, and `CLOSE SHORT` are mapped to spot-like `buy` and `sell` rows.
- Non-open and non-close fills currently become USDC-against-asset `swap` rows.
- Fill grouping stays at one row per API fill. There is no later consolidation step for one economic perp event.
- Current real-wallet fixtures show funding rows whose `id` is the all-zero source hash. The code propagates the source hash as-is and does not synthesize a richer identifier.

## Notable current divergence from spec

- `fetchFunding` preserves `hlFunding.Delta.Coin` in the raw row, but `normalizeHyperliquid` drops that market context and emits only the USDC flow. The normalized row therefore loses which Hyperliquid market produced the funding payment. That is a concrete evidence-retention gap and likely belongs in a future `docs/hyperliquid/REVIEW.md`.
