# Normalized IR Architecture

## Purpose

The normalized IR is the current JSON boundary between the Go ingestion pipeline and the Haskell accounting core. The Go side builds `types.TxPayload` in `go/types/types.go`; the Haskell side consumes the mirrored `TxPayload`, `Transaction`, `AssetAmount`, and `TxType` types in `haskell/src/Types.hs`.

## Current ownership boundary

- `go/fetcher` owns source-specific `fetcher.RawTransaction` construction.
- `go/normalize` owns conversion from `fetcher.RawTransaction` into `types.Transaction`.
- `go/transfer` owns the post-normalization relabeling pass in `MatchTransfers`.
- `go/cmd/main.go` owns payload assembly in `buildPayload` and JSON serialization in `marshalPayload`.
- The IR does not own lot accounting, basis math, or final report generation. Those start after `haskell/app/Main.hs` decodes the payload.

## Main files and named constructs

- `go/types/types.go`
- `go/fetcher/fetcher.go`
- `go/normalize/normalize.go`
- `go/transfer/match.go`
- `go/cmd/main.go`
- `haskell/src/Types.hs`

Important named constructs:

- `types.TxPayload`
- `types.Transaction`
- `types.AssetAmount`
- `types.TxType`
- `fetcher.RawTransaction`
- `buildPayload`
- `Normalize`
- `NormalizeWithDiagnostics`
- `NormalizeResult`
- `SkippedRow`
- `normalizeOne`
- `MatchTransfers`
- `parseDecimal`

## Data flow

- `buildPayload` collects raw rows from the configured fetchers, keeps the explicit wallet list with `appendUniqueWallet`, and creates a shared `price.Provider`.
- Each fetcher emits `fetcher.RawTransaction`. That shape still carries source-specific fields such as `FromAddr`, `ToAddr`, `Asset`, `AssetSymbol`, `Asset2`, `Asset2Symbol`, `Fee`, `USDPrice`, and `RawType`.
- `Normalize` loops over raw rows and calls `normalizeOne`. `normalizeOne` always sets `ID`, `Timestamp`, `Source`, `Chain`, `Wallet`, and optional `RawType`, `Market`, `EventGroupID`, and `SplitReason` before dispatching to `normalizeRobinhood`, `normalizeHyperliquid`, `normalizeHelius`, or `normalizeEVM`.
- Normalization populates the IR as one `types.Transaction` per raw row. The main exception is upstream on the Helius side, where `convertSwap` already turns one enhanced transaction into one raw swap row with both legs.
- `MatchTransfers` runs after normalization and can relabel already-normalized rows when they match an own-wallet transfer pair or the narrow confirmed bridge pair.
- `buildPayload` sorts the final `[]types.Transaction` by `Timestamp` and returns `types.TxPayload{Version, Wallets, Transactions}`.
- `marshalPayload` writes JSON for `cryptotax run --dry-run`, `audit capture`, or stdin into `cryptotax-core`.

## Outputs / side effects

- The top-level JSON object has `version`, `wallets`, and `transactions`.
- Each normalized `Transaction` currently carries `id`, `timestamp`, `source`, `chain`, `tx_type`, `wallet`, `counterparty`, `sent`, `received`, `fee`, `raw_type`, and optionally `market`, `event_group_id`, `split_reason`, and `closed_pnl` (for perp closes).
- Each `AssetAmount` carries `asset` (display string), optional `asset_canonical` (canonical identity when it differs from display), `amount`, and `usd_value`.
- `NormalizeWithDiagnostics` returns a `NormalizeResult` containing both the normalized `Transactions` and structured `[]SkippedRow` diagnostics (tx ID, source, chain, raw type, reason). `Normalize` wraps it and prints skipped-row warnings to stderr for backward compatibility.
- `buildPayload` calls `NormalizeWithDiagnostics` and returns `[]SkippedRow` alongside the payload. It also logs skipped rows to its stderr writer for backward-compatible console output.
- `WriteCaptureArtifacts` persists skipped rows as a `.skipped.json` sidecar file alongside the normalized payload when any rows were skipped during normalization. The sidecar is a JSON array of `SkippedRow` objects.

## Current support boundary

### Implemented behavior

- Every normalized row carries an explicit `wallet` copied from the raw row.
- Decimal quantities and USD values stay as exact decimal strings in Go end to end (the `go/decimal` package centralizes parsing/rendering; CoinGecko prices are captured as `json.Number`, never `float64`) and are parsed to exact `Rational` values by `parseDecimal` in the Haskell core.
- `finalizeTransaction` in `go/normalize/normalize.go` is the single IR exit point: every amount, usd_value, and closed_pnl is canonicalized (no scientific notation, no negatives outside closed_pnl) or the row is skipped with a diagnostic. The Haskell `parseDecimal` is strict and crashes loudly on anything non-canonical.
- The payload shape is stable enough for both direct CLI use and test fixtures.

### Current-behavior-only checkpoints

- `go/normalize/normalize_test.go` freezes several normalization decisions, including Hyperliquid funding market propagation, Hyperliquid perp grouping metadata, and Solana mint-versus-symbol handling.
- `go/transfer/match_test.go` freezes the narrow confirmed bridge relabeling behavior.
- `go/audit/testdata/real-wallet/*.expected.json` documents selected real-wallet normalized payloads as of their capture snapshot, without claiming semantic or tax correctness.

### Unsupported but surfaced behavior

- Unknown valuations still reach the IR as `usd_value: "0"`.
- Address-like and mint-like asset strings remain in the payload instead of being rewritten.
- Hyperliquid perp fills now enter the IR as `perp_open` and `perp_close` rows, quarantined from spot `buy`/`sell`. Perp closes carry `closed_pnl` from the exchange API.
- Grouped or conservatively split rows now carry `event_group_id` and, when known, `split_reason`.

## Current known approximations or conservative behavior

- The `asset` field still has mixed semantics (symbol for some chains, mint for others). `asset_canonical` provides canonical identity when it differs from display, but downstream consumers have not yet adopted it.
- `resolveUSDPrice` returns `"0"` when pricing is unavailable or the asset is outside `price.coingeckoIDs` in `go/price/coingecko.go`.
- EVM rows currently use the source token symbol that `go/fetcher/etherscan.go` receives from Etherscan. Contract-address identity is not present in the normalized row.
- Solana rows currently use a source-backed symbol when Helius provides one, and otherwise fall back to the mint string.
- `event_group_id` and `split_reason` are preserved in the IR, but most downstream consumers do not yet use them for consolidation or reporting.

## Notable current divergence from spec

- No additional schema divergence remains around canonical-versus-display identity or multi-row representation metadata: normalized rows now preserve `asset_canonical`, `event_group_id`, and `split_reason`. The remaining gaps are downstream adoption and evidence-backed support claims, not missing IR fields.
