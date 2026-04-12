# EVM Quirks

This document captures non-obvious Etherscan and EVM source-specific gotchas that are easy to miss from code alone.

## 1. Etherscan ERC-20 token symbols can contain spam and social engineering text

Etherscan's `TokenSymbol` field for ERC-20 transfers is not sanitized. In practice, spam and scam tokens use symbol strings that contain URLs, pipe-separated instructions, and social engineering text.

Examples from real-wallet audit cases in `docs/known-transactions.md`:

- `"ARB | T.ME/S/CLAIMARB | GET REWARD"` (the `arb-claimarb-label` entry)
- `"ARB | T.ME/S/ARB_POOL | CLAIM REWARD"` (the `arb-claim-pool-label` entry)

Since the current pipeline uses `TokenSymbol` directly as the normalized `asset` string (see `docs/evm/ARCHITECTURE.md`, current known approximations), these symbols flow through to the IR and downstream output unchanged. The audit summary flags them via `suspicious_asset_rows` heuristics, but the normalization layer does not filter or reclassify them.

Do not treat these rows as real asset income or dispositions without human verification.

## 2. Zero-amount rows are emitted as artifacts of token transfer parsing

Some Etherscan transaction results include a native-asset row with `Value = 0` alongside a real ERC-20 token transfer. The current pipeline normalizes these as `sell` rows with zero amounts because `classifyAddressMovement` only reasons over addresses and direction, not amounts.

Example from the `eth-bridge-out-usdc` entry in `docs/known-transactions.md`: a USDC bridge-out transaction produces one real USDC sell row plus a zero-amount ETH sell row.

These zero-amount rows are preserved intentionally as visible artifacts rather than silently dropped. Their economic meaning is unresolved.
