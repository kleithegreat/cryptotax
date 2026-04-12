# EVM Human-Verification Packet

This packet groups the pending EVM exemplar cases from `docs/known-transactions.md` for structured human verification. Each case is grounded in existing repo evidence only; no new semantic claims are introduced.

## How to use this packet

For each case below, the human reviewer should:

1. Check the source reference on the block explorer.
2. Answer the exact unresolved question listed.
3. Record the verdict back in `docs/known-transactions.md` by updating the relevant `Human verification required` line.
4. If the verdict confirms current behavior, the case can be promoted from `current-behavior-only` to `human-verified exemplar`.

## Blocker categories

- **bridge pairing** — needs confirmation that outbound and inbound legs are the same own-wallet bridge movement
- **own-wallet confirmation** — needs confirmation that both sides of a transfer belong to owned wallets
- **semantic interpretation** — needs confirmation of the economic meaning of a multi-leg transaction
- **suspicious-asset identity** — needs confirmation whether a received token is real, spam, or mislabeled

## Priority Group 1: Bridge Pairing

These two cases are the outbound and inbound legs of an apparent USDC bridge between Ethereum and Arbitrum. They should be verified together.

### eth-bridge-out-usdc

- **Label:** `eth-bridge-out-usdc`
- **Source system:** Etherscan / Ethereum
- **Current normalized description:** 2 rows under one tx id; sell USDC 499.000000 @ 499.00000000 USD plus a zero-amount ETH sell row, both with ETH fee 0.000276150331013650 @ 1.07408644 USD
- **Exact unresolved question:** Confirm this was an own-wallet bridge rather than a taxable sale, and verify whether the zero-amount ETH leg is purely an artifact of token transfer parsing.
- **Blocker type:** bridge pairing
- **Support-boundary upgrade unlocked:** `evm.bridge_swap_support_upgrade` — confirming this outbound leg validates the bridge matcher against real evidence and could upgrade the bridge pattern from current-behavior-only to human-verified exemplar
- **Source reference:** tx `0xabe5b9a79e83186d6dc419b32ca316bcca0b6aee123e2f50243db115a94f7ecf`
- **Regression fixture:** `go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json`

### arb-bridge-in-fillrelay

- **Label:** `arb-bridge-in-fillrelay`
- **Source system:** Etherscan / Arbitrum
- **Current normalized description:** 1 row; transfer_in USDC 494.577367 @ 494.57736700 USD with raw_type fillRelay(...)
- **Exact unresolved question:** Confirm this is the bridge completion for an own-wallet transfer and not third-party proceeds or another taxable inflow. Pair it with the outbound bridge transaction above.
- **Blocker type:** bridge pairing
- **Support-boundary upgrade unlocked:** `evm.bridge_swap_support_upgrade` — confirming this inbound leg completes the bridge pair and allows the confirmed bridge matcher to be promoted to human-verified
- **Source reference:** tx `0x9e2b76f517772cfcbbf29c5426b3513fc1b872898263c2a46dfadebea4c15c7a`
- **Regression fixture:** `go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json`

### Verification notes for this pair

- The outbound shows USDC 499.00 sent; the inbound shows USDC 494.58 received. The ~4.42 USDC difference is consistent with a bridge relay fee.
- The outbound has a zero-amount ETH leg that may be a parsing artifact from the native-transfer row accompanying the ERC-20 approval/transfer.
- Both rows reference wallet `0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA` on different chains (Ethereum and Arbitrum).

## Priority Group 2: Swap Reconstruction

### eth-swap-usdc

- **Label:** `eth-swap-usdc`
- **Source system:** Etherscan / Ethereum
- **Current normalized description:** 2 rows; transfer_in USDC 119.762878 @ 119.76287800 USD and sell ETH 0.028566527209111203 @ 118.31797546 USD with ETH fee 0.000302544319189428 @ 1.25309006 USD
- **Exact unresolved question:** Confirm this is a true spot swap (ETH sold for USDC received in one atomic operation). Confirm whether the split-leg representation (one `sell` + one `transfer_in`) is acceptable for downstream tax logic or whether it should be collapsed into a single `swap` row. Confirm fee attribution against the explorer trace.
- **Blocker type:** semantic interpretation
- **Support-boundary upgrade unlocked:** `evm.bridge_swap_support_upgrade` — confirming a real swap case provides the first human-verified EVM swap exemplar, which is prerequisite for any swap reconstruction work
- **Source reference:** tx `0x59a94dab0b560d8ec34f0d5a884812db5485e0fd71fdccab56c1c7976f0728bc`

## Priority Group 3: Own-Wallet Confirmation

### eth-own-transfer-out

- **Label:** `eth-own-transfer-out`
- **Source system:** Etherscan / Ethereum
- **Current normalized description:** 1 row; transfer_out ETH 0.029315279140745676 @ 121.41918591 USD; fee ETH 0.000011446954155000 @ 0.04741145 USD
- **Exact unresolved question:** Confirm the destination wallet is also owned. If owned, the row should stay as non-taxable transfer activity. If not owned, this is a third-party disposition that needs reclassification.
- **Blocker type:** own-wallet confirmation
- **Support-boundary upgrade unlocked:** Confirming own-wallet status validates the transfer matching logic for this address pair and strengthens confidence in `MatchTransfers` / `isOwnWalletTransferPair`
- **Source reference:** tx `0xb5fdbd8ffa96b080d509c2061ae41d02c52dff9731981e571b9536d2334b2524`

## Priority Group 4: Suspicious-Asset Identity

These three cases involve received tokens with zero USD valuation and suspicious or non-standard symbols. They share the same blocker pattern: the human must determine whether each token is real, spam, or mislabeled.

### eth-airdrop-epin

- **Label:** `eth-airdrop-epin`
- **Source system:** Etherscan / Ethereum
- **Current normalized description:** 1 row; transfer_in EPIN 1.000000000000000000 @ 0 USD
- **Exact unresolved question:** Confirm whether EPIN is a real asset, spam token, or taxable airdrop. If real, determine whether a valuation source is required. If spam, determine whether it should be excluded from normalized output entirely.
- **Blocker type:** suspicious-asset identity
- **Support-boundary upgrade unlocked:** Establishes the first precedent for how the pipeline should handle zero-USD ERC-20 receipts — whether to exclude spam, preserve as unsupported-but-surfaced, or model as income
- **Source reference:** tx `0x3a3b6f3150f6fad511ab8e531e1953bbf06cd14fbc3bda2a4f906ada6d488d1d`

### arb-claimarb-label

- **Label:** `arb-claimarb-label`
- **Source system:** Etherscan / Arbitrum
- **Current normalized description:** 1 row; transfer_in "ARB | T.ME/S/CLAIMARB | GET REWARD" 4420 @ 0 USD
- **Exact unresolved question:** Confirm whether this is spam, a misleading symbol string, or a real claimable asset. If spam, determine whether it should be excluded. If the symbol string is misleading, determine what the canonical identity should be.
- **Blocker type:** suspicious-asset identity
- **Support-boundary upgrade unlocked:** Establishes handling rules for tokens with embedded promotional text in Etherscan symbol fields — prerequisite for symbol normalization and suspicious-asset exclusion logic
- **Source reference:** tx `0x7921b8fdeddb70a5e4f395757a1f0022e831b33ad00383d15a223e54b751b501`

### arb-claim-pool-label

- **Label:** `arb-claim-pool-label`
- **Source system:** Etherscan / Arbitrum
- **Current normalized description:** 1 row; transfer_in "ARB | T.ME/S/ARB_POOL | CLAIM REWARD" 3350 @ 0 USD
- **Exact unresolved question:** Confirm whether this is spam, a misleading symbol string, or a real claimable asset. If spam, determine whether it should be excluded. If the symbol string is misleading, determine what the canonical identity should be.
- **Blocker type:** suspicious-asset identity
- **Support-boundary upgrade unlocked:** Same as `arb-claimarb-label` — these two cases share the same pattern and the verdict on one likely applies to the other
- **Source reference:** tx `0x1e2ef81d438f1827a356e31007766eab9bbc756a2949681296d3300f7417bce0`

## Summary

| Label | Blocker Type | Unlocks |
| --- | --- | --- |
| `eth-bridge-out-usdc` | bridge pairing | `evm.bridge_swap_support_upgrade` |
| `arb-bridge-in-fillrelay` | bridge pairing | `evm.bridge_swap_support_upgrade` |
| `eth-swap-usdc` | semantic interpretation | `evm.bridge_swap_support_upgrade` |
| `eth-own-transfer-out` | own-wallet confirmation | transfer matching validation |
| `eth-airdrop-epin` | suspicious-asset identity | zero-USD ERC-20 handling precedent |
| `arb-claimarb-label` | suspicious-asset identity | symbol normalization rules |
| `arb-claim-pool-label` | suspicious-asset identity | symbol normalization rules |

## Recommended verification order

1. **Bridge pair first** (`eth-bridge-out-usdc` + `arb-bridge-in-fillrelay`): highest impact because it can promote the existing confirmed bridge matcher to human-verified status.
2. **Swap case** (`eth-swap-usdc`): second-highest impact because it provides the first swap exemplar.
3. **Own-wallet** (`eth-own-transfer-out`): straightforward single-question verification.
4. **Suspicious assets** (`eth-airdrop-epin`, `arb-claimarb-label`, `arb-claim-pool-label`): lower priority but the Arbitrum pair likely shares one verdict.
