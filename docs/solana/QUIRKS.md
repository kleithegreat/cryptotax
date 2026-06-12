# Solana Quirks

This document captures non-obvious Helius and Solana source-specific gotchas that are easy to miss from code alone.

## 1. Raw mint strings appear as normalized asset names when Helius provides no symbol

When Helius enhanced transactions do not supply a source-backed symbol for a token transfer, the normalized `asset` field contains the raw mint string (e.g., `CMMNJETQSDR79XaLkttgQjaQwuWqzuLifLJT8F7mpump`). This is by design: the pipeline uses `heliusDisplayAsset` which falls back to the mint when no symbol exists, rather than guessing a display name.

Caution when reading older evidence: captures and docs from before commit c09615b carry UPPERCASED mint strings (a former normalization bug). Base58 is case-sensitive, so an uppercased mint is unresolvable on explorers — e.g. the old packets' `EPJFWDD5AUFQSSQEM2QN1XZYBAPC8G4WEGGKZWYTDT1V` is actually the USDC mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`. Fresh captures preserve exact casing.

These mint-string assets will usually have `usd_value: "0"` because the pricing provider does not know their CoinGecko ID. The audit summary flags them through `buildSuspiciousAssetRow` heuristics.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `sol-pumpfun-zero-usd` entry shows the pump.fun mint (uppercased in that older capture) as the received asset at zero USD.

## 2. Helius `TokenAmount` is decoded as `json.Number` (float64 path removed)

`heliusTokenTransfer.TokenAmount` is decoded as `json.Number` and flows through exact `big.Rat` arithmetic; the former `float64` intermediate (which lost precision past ~2^53) is gone. Amounts pass to the IR verbatim and are canonicalized at the normalize boundary (`decimal.Canon`), so scientific-notation JSON numbers also arrive as plain decimals.

## 3. Fee is attached only when the wallet is the fee payer, and only to one row

`attachFeeToPrimaryHeliusRow` in `go/fetcher/helius.go` assigns the signature fee to the first outbound row of a transaction, and only when the enhanced payload's `feePayer` equals the wallet — fees on sponsored/gasless transactions belong to the sponsor and are not charged to the wallet. In multi-row parses only one row carries the fee. Downstream consumers cannot assume every row without fee data was genuinely fee-free.

Example: the `sol-addresslike-mint` entry in `docs/known-transactions.md` shows 10 rows from one transaction, where only the first outbound row carries the SOL fee.

## 4. Helius enhanced endpoint has a fallback for specific failure codes

`shouldFallbackHeliusEnhanced` in `go/fetcher/helius.go` detects HTTP 530 and error code 1016 responses from the primary enhanced-transactions endpoint and retries against `LegacyEnhancedURL`. This fallback exists because the primary endpoint occasionally returns these codes for valid signatures. If the fallback is removed or the legacy URL becomes unavailable, some valid signatures may fail to parse.

## 5. Inbound SPL transfers require token-account signature enumeration

`getSignaturesForAddress` on the owner wallet does NOT return transactions that only touch the owner's associated token accounts (e.g. someone sending USDC to an existing ATA). `Fetch` therefore enumerates the wallet's token accounts via `getTokenAccountsByOwner` (classic + Token-2022 programs) and unions signatures across the owner and every ATA, deduplicating before the enhanced parse. Removing that enumeration silently hides inbound SPL history.

## 6. SWAP events are reconstructed from net per-asset balance change

`convertSwap` nets every wallet-touching transfer per asset (native SOL and the wSOL mint count as one asset, so wrap/unwrap legs cancel). A swap row is emitted only when exactly one asset is net-sent and one net-received; a rent-sized stray SOL flow (< 0.01 SOL) riding along with token legs is excluded as account rent. Anything else falls back to per-leg preservation so no leg is dropped. The previous "last matching transfer wins" logic could record a rent micro-leg as the entire sent side of a swap.

## 7. The enhanced parse is reconciled against requested signatures

The Enhanced Transactions API silently omits signatures it cannot parse. `parseEnhanced` warns on stderr with the count and a sample of missing signatures — those transactions are absent from the report and must be investigated manually.

## 8. Failed transactions are skipped, including their fees

Signatures with a non-null `err` are filtered out during signature collection. The fee paid on a failed transaction is a real cost, but modeling it is an open tax-semantics question; skipping is conservative (never overstates deductions).
