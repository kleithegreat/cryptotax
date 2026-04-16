# Hyperliquid Architecture

## Purpose

The current Hyperliquid pipeline fetches public fill and funding history, converts those records into raw rows, normalizes them into the shared IR, and lets the Haskell core consume the resulting `funding_payment`, `perp_open`, `perp_close`, or spot `swap` rows.

## Current ownership boundary

- `go/fetcher/hyperliquid.go` owns Hyperliquid API access and raw-row construction.
- `go/normalize/normalize.go` owns Hyperliquid normalization in `normalizeHyperliquid`.
- `haskell/src/GainLoss.hs` owns the downstream handling of normalized `funding_payment` rows.
- The codebase does not currently have a separate perp-position tracker. Hyperliquid perp fills now cross the boundary through dedicated `perp_open` / `perp_close` IR types rather than spot `buy` / `sell` rows.

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
- Each fill becomes one `fetcher.RawTransaction` with `ID = Hash`, `Asset = Coin`, `Amount = Sz`, `USDPrice = Px`, `Fee`, `FeeAsset`, `RawType = Dir`, `ClosedPnl`, and `StartPosition`. If `Coin` starts with `@`, `Fetch` resolves it through the spot metadata map before writing the raw row.
- `fetchFunding` paginates `userFunding` the same way. Each funding entry becomes one raw row with `Asset = Delta.Coin`, `Market = Delta.Coin`, `Amount = Delta.USDC`, `RawType = "funding"`, and a stable `EventGroupID` synthesized when the source hash is weak.
- `normalizeHyperliquid` turns `RawType == "funding"` into `tx_type = funding_payment`. Negative amounts become outbound USDC in `Sent`; positive amounts become inbound USDC in `Received`. Funding rows preserve Hyperliquid market context in the normalized `market` field.
- Non-funding rows are classified by the upper-cased `RawType`. `OPEN LONG` and `OPEN SHORT` become `perp_open`; `CLOSE LONG` and `CLOSE SHORT` become `perp_close` with `closed_pnl` propagated from the API; all other fill directions currently become `swap` rows against USDC.
- Fill rows carry `event_group_id` and `split_reason = "api_fill_granularity"` so partial fills sharing one economic event can be grouped downstream.
- `normalizeHyperliquid` prices fees directly in USD when `FeeAsset == "USDC"` and otherwise falls back to `resolveUSDPrice`.
- In the Haskell core, `handleFunding` records positive funding as income and negative funding as structured `FundingExpense` entries. `handlePerpOpen` is a no-op (no phantom lots). `handlePerpClose` uses `ClosedPnl` to emit `PerpPnlEntry` records.

## Outputs / side effects

- Hyperliquid rows enter the IR with `source="hyperliquid"` and `chain="hyperliquid"`.
- Funding rows preserve `raw_type = "funding"`.
- Funding rows preserve `market` and `event_group_id` in the IR.
- Fill rows preserve Hyperliquid's direction string in `raw_type`.
- Fill rows preserve `event_group_id` and `split_reason` in the IR.
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

- Negative funding does not consume USDC inventory (informational expense report only).
- Partial fills that share one hash remain separate normalized rows when the API returns them separately; each gets its own `ClosedPnl` entry.
- Canonical output keeps perp PnL in the separate `perp_pnl.csv` report rather than emitting derivative rows on 8949.

## Current known approximations or conservative behavior

- Perp fills are quarantined with `perp_open` / `perp_close` types. `ClosedPnl` from the exchange is used as-is for the PnL value; the core does not independently compute PnL from position tracking.
- Non-open and non-close fills currently become USDC-against-asset `swap` rows.
- Fill grouping stays at one row per API fill. Partial-fill `ClosedPnl` values are not consolidated.
- Current real-wallet fixtures show funding rows whose `id` is the all-zero source hash. The code still propagates the source hash as-is, but it now also preserves `market` and a stable `event_group_id` for downstream grouping.

## Notable current divergence from spec

- No additional schema divergence remains around funding market context: normalized funding rows now preserve `market`. The remaining funding gap is evidence strength when the Hyperliquid-provided `id` is the all-zero hash.
