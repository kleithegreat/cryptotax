# EVM Quirks

This document captures non-obvious Etherscan and EVM source-specific gotchas that are easy to miss from code alone.

## 1. Etherscan ERC-20 token symbols can contain spam and social engineering text

Etherscan's `TokenSymbol` field for ERC-20 transfers is not sanitized. In practice, spam and scam tokens use symbol strings that contain URLs, pipe-separated instructions, and social engineering text.

Examples from real-wallet audit cases in `docs/known-transactions.md`:

- `"ARB | T.ME/S/CLAIMARB | GET REWARD"` (the `arb-claimarb-label` entry)
- `"ARB | T.ME/S/ARB_POOL | CLAIM REWARD"` (the `arb-claim-pool-label` entry)

The pipeline uses `TokenSymbol` (uppercased for display grouping) as the normalized `asset` string and preserves the token's contract address as `asset_canonical` (lowercased). Pricing trusts the canonical identity, not the symbol: a scam token claiming `USDC` does not get the $1 stablecoin shortcut unless its contract address is in `price.canonicalSymbols`. The audit summary flags suspicious symbols via `suspicious_asset_rows`, but normalization does not filter or reclassify them. NOTE: the Haskell core still keys FIFO lots by display symbol — a scam token named `USDC` can still commingle with real USDC lots (open decision, see `docs/repo/REVIEW.md`).

Do not treat these rows as real asset income or dispositions without human verification.

## 2. Zero-amount rows are emitted as artifacts of token transfer parsing

Some Etherscan transaction results include a native-asset row with `Value = 0` alongside a real ERC-20 token transfer. The current pipeline normalizes these as `sell` rows with zero amounts because `classifyAddressMovement` only reasons over addresses and direction, not amounts.

Example from the `eth-bridge-out-usdc` entry in `docs/known-transactions.md`: a USDC bridge-out transaction produces one real USDC sell row plus a zero-amount ETH sell row.

These zero-amount rows are preserved intentionally as visible artifacts rather than silently dropped. Their economic meaning is unresolved.

## 3. Gas is attributed once per transaction, to the initiator, on one surviving row

Etherscan stamps gas fields on every row of a hash (txlist and tokentx). The fetcher's `assembleRows` keeps fee data only for transactions the wallet initiated (a txlist row with `from == wallet`) and attaches it to exactly one row, preferring a nonzero outbound leg. Token rows from transactions initiated by others (e.g. an approved spender pulling tokens) carry no fee — the spender paid the gas. Zero-value txlist artifact rows are dropped when token rows exist for the same hash (the fee migrates to a token leg); a bare contract call with no token movement keeps its zero-value row so the gas evidence stays visible (though the core currently no-ops zero-amount disposals, so that gas does not reach tax output — an open gap).

## 4. Failed transactions are skipped, including their gas

Rows with `isError == "1"` are dropped. Gas on reverted transactions is a real cost but modeling it is an open tax-semantics question; skipping is conservative.

## 5. Etherscan's pagination window aborts loudly past 10,000 records

Etherscan caps `page * offset` at 10,000 per query window. A wallet with more records per endpoint fails the whole fetch with an explicit API error rather than truncating silently. Block-windowed pagination is the known fix if a wallet ever outgrows this.

## 6. Internal ETH transfers (`txlistinternal`) are not fetched

ETH received through internal transactions — DEX swap outputs, contract withdrawals, refunds — does not appear in `txlist` and is currently invisible to the pipeline. Missing inbound ETH surfaces later as insufficient-lot errors on disposals. Known unresolved gap.
