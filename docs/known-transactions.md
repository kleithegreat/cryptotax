# Known Transactions

Use this file as a human-reviewed comparison log for representative normalized
transactions. Keep verified cases at the top, then duplicate the template block
below for new evidence.

## Verified Cases

### 1. Local Golden Fixture: Buy, Sell, and Own-Wallet Transfer

- Evidence id: `basic-buy-sell-transfer`
- Scope: one `buy`, one `sell`, and one non-taxable own-wallet transfer pair
- Normalized fixture:
  [basic-buy-sell-transfer.json](../haskell/testdata/basic-buy-sell-transfer.json)
- Expected 8949 CSV:
  [basic-buy-sell-transfer-8949.csv](../haskell/testdata/basic-buy-sell-transfer-8949.csv)
- Validation path: `nix flake check` runs the fixture through the Haskell core and asserts the exact CSV output
- Status: `pass`
- Support tier: **human-verified exemplar**

| Check | Expected result | Evidence |
| --- | --- | --- |
| Buy basis math | 2 ETH lot with total basis `4020.00` USD | Buy row has received USD `4000.00` and fee USD `20.00` |
| Transfer handling | No 8949 row from the own-wallet transfer pair | The core ignores `transfer_out` and `transfer_in` rows |
| Sell proceeds math | Net proceeds `2970.00` USD | Sell row has sent USD `3000.00` and fee USD `30.00` |
| Final 8949 row | One short-term disposal row for `1 ETH` with gain `960.00` USD | CSV fixture contains exactly `1 ETH,01/15/2024,08/20/2024,2970.00,2010.00,960.00,Short` |

## Pending Real-Wallet Validation Set

- Snapshot: `audit/normalized.json` generated locally from the recorded command and not committed by default because it contains the full real-wallet snapshot
- Summary: [summary.json](../audit/summary.json)
- Regression fixtures for documented current normalization only: [go/audit/testdata/real-wallet](../go/audit/testdata/real-wallet)
- Reviewer: `<fill in>`
- Default status for all cases below: `pending`
- `Verified Cases` above are the only cases in this file with explicit evidence-backed validation.
- The regression fixtures under `go/audit/testdata/real-wallet` freeze current observed normalized output from the local snapshot only.
- The `Human verification required` lines below remain the source of truth for what still needs human confirmation before any tax or semantic claim is treated as verified.
- **Support tier mapping**: Cases with regression fixtures in `go/audit/testdata/real-wallet/` are **current-behavior-only**. All other pending cases are **unsupported but surfaced**. Neither tier implies tax correctness; see `docs/repo/SPEC.md` for tier definitions.

### Ethereum / Arbitrum

#### 2. Ethereum own-wallet ETH transfer out

- Label: `eth-own-transfer-out`
- Source system: `Etherscan / Ethereum`
- Source reference: `TODO: confirm tx 0xb5fdbd8ffa96b080d509c2061ae41d02c52dff9731981e571b9536d2334b2524 on explorer and confirm the destination wallet is also owned`
- Filtered normalized case: [eth-own-transfer-out.json](../audit/cases/eth-own-transfer-out.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0xb5fdbd8ffa96b080d509c2061ae41d02c52dff9731981e571b9536d2334b2524 --wallet 0x29c5ee30daA4c6ceFA193c4ed9d821db36D9452e`
- Current normalized JSON: `1 row; transfer_out ETH 0.029315279140745676 @ 121.41918591 USD; fee ETH 0.000011446954155000 @ 0.04741145 USD`
- Human verification required: `TODO verify the counterparty wallet is owned and the row should stay non-taxable transfer activity instead of third-party disposition`

#### 3. Ethereum zero-USD ERC-20 receipt candidate

- Label: `eth-airdrop-epin`
- Source system: `Etherscan / Ethereum`
- Source reference: `TODO: confirm tx 0x3a3b6f3150f6fad511ab8e531e1953bbf06cd14fbc3bda2a4f906ada6d488d1d on explorer and determine whether EPIN is a real asset, spam token, or taxable airdrop`
- Filtered normalized case: [eth-airdrop-epin.json](../audit/cases/eth-airdrop-epin.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x3a3b6f3150f6fad511ab8e531e1953bbf06cd14fbc3bda2a4f906ada6d488d1d --wallet 0x29c5ee30daA4c6ceFA193c4ed9d821db36D9452e`
- Current normalized JSON: `1 row; transfer_in EPIN 1.000000000000000000 @ 0 USD`
- Human verification required: `TODO confirm whether this should remain a conservative zero-USD transfer_in, be excluded as spam, or be modeled as income with a verifiable fair market value`

#### 4. Ethereum ETH→USDC swap candidate

- Label: `eth-swap-usdc`
- Source system: `Etherscan / Ethereum`
- Source reference: `TODO: confirm tx 0x59a94dab0b560d8ec34f0d5a884812db5485e0fd71fdccab56c1c7976f0728bc on explorer and whether it is one swap rather than isolated transfer legs`
- Filtered normalized case: [eth-swap-usdc.json](../audit/cases/eth-swap-usdc.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x59a94dab0b560d8ec34f0d5a884812db5485e0fd71fdccab56c1c7976f0728bc --wallet 0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA`
- Current normalized JSON: `2 rows; transfer_in USDC 119.762878 @ 119.76287800 USD and sell ETH 0.028566527209111203 @ 118.31797546 USD with ETH fee 0.000302544319189428 @ 1.25309006 USD`
- Human verification required: `TODO confirm this is a true spot swap, confirm whether the split-leg representation is acceptable for downstream tax logic, and confirm fee attribution against the explorer trace`

#### 5. Ethereum bridge out candidate

- Label: `eth-bridge-out-usdc`
- Source system: `Etherscan / Ethereum`
- Source reference: `TODO: confirm tx 0xabe5b9a79e83186d6dc419b32ca316bcca0b6aee123e2f50243db115a94f7ecf on explorer and identify the bridge/protocol`
- Filtered normalized case: [eth-bridge-out-usdc.json](../audit/cases/eth-bridge-out-usdc.json)
- Regression fixture input: [eth-bridge-out-usdc.input.json](../go/audit/testdata/real-wallet/eth-bridge-out-usdc.input.json)
- Current-normalization fixture: [evm-bridge-usdc-fillrelay.expected.json](../go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json)
- Regression coverage: `documented current normalization only; semantic/tax verification still pending`
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0xabe5b9a79e83186d6dc419b32ca316bcca0b6aee123e2f50243db115a94f7ecf --wallet 0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA`
- Current normalized JSON: `2 rows under one tx id; sell USDC 499.000000 @ 499.00000000 USD plus a zero-amount ETH sell row, both with ETH fee 0.000276150331013650 @ 1.07408644 USD`
- Human verification required: `TODO confirm this was an own-wallet bridge rather than a taxable sale, and verify whether the zero-amount ETH leg is purely an artifact of token transfer parsing`

#### 6. Arbitrum bridge completion candidate

- Label: `arb-bridge-in-fillrelay`
- Source system: `Etherscan / Arbitrum`
- Source reference: `TODO: confirm tx 0x9e2b76f517772cfcbbf29c5426b3513fc1b872898263c2a46dfadebea4c15c7a on explorer and pair it with the correct outbound bridge transaction`
- Filtered normalized case: [arb-bridge-in-fillrelay.json](../audit/cases/arb-bridge-in-fillrelay.json)
- Regression fixture input: [arb-bridge-in-fillrelay.input.json](../go/audit/testdata/real-wallet/arb-bridge-in-fillrelay.input.json)
- Current-normalization fixture: [evm-bridge-usdc-fillrelay.expected.json](../go/audit/testdata/real-wallet/evm-bridge-usdc-fillrelay.expected.json)
- Regression coverage: `documented current normalization only; bridge matching still requires human confirmation`
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x9e2b76f517772cfcbbf29c5426b3513fc1b872898263c2a46dfadebea4c15c7a --wallet 0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA`
- Current normalized JSON: `1 row; transfer_in USDC 494.577367 @ 494.57736700 USD with raw_type fillRelay(...)`
- Human verification required: `TODO confirm this is the bridge completion for an own-wallet transfer and not third-party proceeds or another taxable inflow`

#### Additional Arbitrum suspicious-asset follow-ups

- Label: `arb-claimarb-label`
- Source system: `Etherscan / Arbitrum`
- Source reference: `TODO: confirm tx 0x7921b8fdeddb70a5e4f395757a1f0022e831b33ad00383d15a223e54b751b501 on explorer and determine whether the received label is a spoofed reward token`
- Filtered normalized case: [arb-claimarb-label.json](../audit/cases/arb-claimarb-label.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x7921b8fdeddb70a5e4f395757a1f0022e831b33ad00383d15a223e54b751b501 --wallet 0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA`
- Current normalized JSON: `1 row; transfer_in "ARB | T.ME/S/CLAIMARB | GET REWARD" 4420 @ 0 USD`
- Human verification required: `TODO confirm whether this is spam, a misleading symbol string, or a real claimable asset that needs explicit symbol normalization and valuation rules`

- Label: `arb-claim-pool-label`
- Source system: `Etherscan / Arbitrum`
- Source reference: `TODO: confirm tx 0x1e2ef81d438f1827a356e31007766eab9bbc756a2949681296d3300f7417bce0 on explorer and determine whether the received label is a spoofed reward token`
- Filtered normalized case: [arb-claim-pool-label.json](../audit/cases/arb-claim-pool-label.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x1e2ef81d438f1827a356e31007766eab9bbc756a2949681296d3300f7417bce0 --wallet 0x8D5A67da96cf80E013979C5C4CD0663d7090E3cA`
- Current normalized JSON: `1 row; transfer_in "ARB | T.ME/S/ARB_POOL | CLAIM REWARD" 3350 @ 0 USD`
- Human verification required: `TODO confirm whether this is spam, a misleading symbol string, or a real claimable asset that needs explicit symbol normalization and valuation rules`

### Robinhood

- Captured Robinhood rows in this snapshot are all `sell` rows; no synthetic `buy` rows were present in the imported 1099-DA data.

#### 7. Robinhood SOL sale with proceeds

- Label: `robinhood-sol-sell-short`
- Source system: `Robinhood 1099-DA CSV`
- Source reference: `TODO: confirm the exact 1099-DA line item for rh-sell-20250915-SOL-1.84841424`
- Filtered normalized case: [robinhood-sol-sell-short.json](../audit/cases/robinhood-sol-sell-short.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id rh-sell-20250915-SOL-1.84841424 --wallet robinhood`
- Current normalized JSON: `1 row; sell SOL 1.84841424 @ 441.48 USD with raw_type 1099-DA-SELL-SHORT`
- Human verification required: `TODO confirm the CSV line, acquisition lot linkage, and whether the term field matches Robinhood's exported evidence`

#### 8. Robinhood zero-USD micro-lot sale

- Label: `robinhood-sol-sell-micro`
- Source system: `Robinhood 1099-DA CSV`
- Source reference: `TODO: confirm the exact 1099-DA line item for rh-sell-20250921-SOL-1e-05`
- Filtered normalized case: [robinhood-sol-sell-micro.json](../audit/cases/robinhood-sol-sell-micro.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id rh-sell-20250921-SOL-1e-05 --wallet robinhood`
- Current normalized JSON: `1 row; sell SOL 0.00001 @ 0 USD with raw_type 1099-DA-SELL`
- Human verification required: `TODO confirm whether the CSV really reports zero proceeds for this dust sale or whether the source row was rounded below display precision`

### Solana

#### 9. Solana PUMP_FUN swap with zero-USD received mint

- Label: `sol-pumpfun-zero-usd`
- Source system: `Helius enhanced transactions / Solana explorer`
- Source reference: `TODO: confirm tx PbxPFcX7JQF6PuTMjRs2xKuC2azpAmnc1uALwYxCKuwnWFT3vb156p17CZRYjcU1ySB1ANHSWgGfPEfHtE2QXKm and identify the received mint`
- Filtered normalized case: [sol-pumpfun-zero-usd.json](../audit/cases/sol-pumpfun-zero-usd.json)
- Regression fixture input: [sol-pumpfun-zero-usd.input.json](../go/audit/testdata/real-wallet/sol-pumpfun-zero-usd.input.json)
- Current-normalization fixture: [solana-pumpfun-zero-usd.expected.json](../go/audit/testdata/real-wallet/solana-pumpfun-zero-usd.expected.json)
- Regression coverage: `documented current normalization plus audit-summary regression for the unresolved received-leg identity/valuation; asset identity and valuation still pending`
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id PbxPFcX7JQF6PuTMjRs2xKuC2azpAmnc1uALwYxCKuwnWFT3vb156p17CZRYjcU1ySB1ANHSWgGfPEfHtE2QXKm --wallet BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp`
- Current normalized JSON: `1 row; swap sent SOL 0.000803279 @ 0.12734483 USD, received CMMNJETQSDR79XALKTTGQJAQWUWQZULIFLJT8F7MPUMP 540724.686218000 @ 0 USD, fee SOL 0.001005000 @ 0.15932391 USD`
- Human verification required: `TODO confirm the received mint symbol/name, confirm whether the swap path is correct, and decide whether zero USD is acceptable or whether a valuation source is required`

#### 10. Solana DFLOW swap with same-asset SOL legs

- Label: `sol-dflow-swap`
- Source system: `Helius enhanced transactions / Solana explorer`
- Source reference: `TODO: confirm tx gcjrtf2z4ZKat5oq14ZyJG7Sk4D4M6ndTfo5arcLzPr5ChgTZ4HcERmYBbHwBrqbu9wBk6V6y711n1uu5SuT2TJ and inspect the instruction trace`
- Filtered normalized case: [sol-dflow-swap.json](../audit/cases/sol-dflow-swap.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id gcjrtf2z4ZKat5oq14ZyJG7Sk4D4M6ndTfo5arcLzPr5ChgTZ4HcERmYBbHwBrqbu9wBk6V6y711n1uu5SuT2TJ --wallet BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp`
- Current normalized JSON: `1 row; swap sent SOL 2.086215720 @ 487.83195732 USD, received SOL 0.002039280 @ 0.47685670 USD, fee SOL 0.000080001 @ 0.01870710 USD`
- Human verification required: `TODO confirm whether this is truly a taxable swap, an aggregator routing artifact, or a transfer/fee pattern that should not be modeled as SOL-for-SOL disposal`

#### 11. Solana address-like mint and multi-row transfer case

- Label: `sol-addresslike-mint`
- Source system: `Helius enhanced transactions / Solana explorer`
- Source reference: `TODO: confirm tx 2dh7RefWvkHAKL9YQ1wZx5DJnhYMGWNkz2xSTh86792TgzhDPZiYw3VivrM6GdwMfJbWYNihZByo5ujEtXHBmQUn and identify the address-like mint EPJFWDD5AUFQSSQEM2QN1XZYBAPC8G4WEGGKZWYTDT1V`
- Filtered normalized case: [sol-addresslike-mint.json](../audit/cases/sol-addresslike-mint.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 2dh7RefWvkHAKL9YQ1wZx5DJnhYMGWNkz2xSTh86792TgzhDPZiYw3VivrM6GdwMfJbWYNihZByo5ujEtXHBmQUn --wallet BeLzE7RD9XVg3y4CbLEfB29gMqvGHTxK5EwtvDJpLDWp`
- Current normalized JSON: `10 rows with mixed sell/transfer_in legs across SOL, wrapped SOL mint SO111..., and address-like mint EPJFW...; several legs have zero USD values`
- Human verification required: `TODO confirm whether this transaction is one economic swap, several internal account moves, or a combination of both; confirm which mints are real assets and whether the current row explosion is acceptable`

### Hyperliquid

#### 12. Hyperliquid negative funding row

- Label: `hl-funding-negative-2025-10-07`
- Source system: `Hyperliquid funding history`
- Source reference: `TODO: confirm the funding event for 2025-10-07T00:00:00Z from Hyperliquid source data`
- Filtered normalized case: [hl-funding-negative-2025-10-07.json](../audit/cases/hl-funding-negative-2025-10-07.json)
- Regression fixture input: [hl-funding-negative-2025-10-07.input.json](../go/audit/testdata/real-wallet/hl-funding-negative-2025-10-07.input.json)
- Current-normalization fixture: [hyperliquid-funding.expected.json](../go/audit/testdata/real-wallet/hyperliquid-funding.expected.json)
- Regression coverage: `documented normalization plus focused Go/Haskell funding tests; negative-funding final tax-output semantics still pending`
- Filter command used: `nix run .#audit -- filter audit/normalized.json --wallet 0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca --raw-type funding --from-timestamp 2025-10-07T00:00:00Z --to-timestamp 2025-10-07T23:59:59Z`
- Current normalized JSON: `1 row; funding_payment sent USDC 0.168095 @ 0.168095 USD`
- Human verification required: `TODO retain and review the Hyperliquid source row showing delta.usdc -0.168095 for 2025-10-07T00:00:00Z, then decide how this ordinary expense should appear in final tax output`

#### 13. Hyperliquid positive funding row

- Label: `hl-funding-positive-2025-12-02`
- Source system: `Hyperliquid funding history`
- Source reference: `TODO: confirm the funding event for 2025-12-02T00:00:00Z from Hyperliquid source data`
- Filtered normalized case: [hl-funding-positive-2025-12-02.json](../audit/cases/hl-funding-positive-2025-12-02.json)
- Regression fixture input: [hl-funding-positive-2025-12-02.input.json](../go/audit/testdata/real-wallet/hl-funding-positive-2025-12-02.input.json)
- Current-normalization fixture: [hyperliquid-funding.expected.json](../go/audit/testdata/real-wallet/hyperliquid-funding.expected.json)
- Regression coverage: `documented normalization plus focused Go/Haskell funding tests; funding evidence linkage still pending`
- Filter command used: `nix run .#audit -- filter audit/normalized.json --wallet 0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca --raw-type funding --from-timestamp 2025-12-02T00:00:00Z --to-timestamp 2025-12-02T23:59:59Z`
- Current normalized JSON: `1 row; funding_payment received USDC 1.879512 @ 1.879512 USD`
- Human verification required: `TODO retain and review the Hyperliquid source row showing delta.usdc 1.879512 for 2025-12-02T00:00:00Z, confirm whether the all-zero tx id is acceptable evidence linkage, and decide whether funding rows need a richer identifier`

#### 14. Hyperliquid perpetual open-long approximation

- Label: `hl-open-long-btc`
- Source system: `Hyperliquid fill history`
- Source reference: `TODO: confirm fill 0x296c458688c54a1e2ae6042cfc24eb02026d006c23c868f0cd34f0d947c92408 from Hyperliquid and whether it was an opening perp trade`
- Filtered normalized case: [hl-open-long-btc.json](../audit/cases/hl-open-long-btc.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0x296c458688c54a1e2ae6042cfc24eb02026d006c23c868f0cd34f0d947c92408 --wallet 0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca`
- Current normalized JSON: `the checked-in filtered case predates the perp-decision wave and is stale; current implementation should emit 1 row; perp_open BTC 0.0004 @ 49.92600000 USD with USDC fee 0.022466`
- Human verification required: `TODO confirm this is a perp position increase rather than a spot acquisition, then decide whether the current no-lot perp_open boundary is acceptable until a fuller position model exists`

#### 15. Hyperliquid perpetual close-short approximation

- Label: `hl-close-short-sol`
- Source system: `Hyperliquid fill history`
- Source reference: `TODO: confirm fill 0xd83f79b33a4eb3a3d9b9042d4d89e20204320098d541d2757c082505f9428d8e from Hyperliquid and whether all four rows are one short close sequence`
- Filtered normalized case: [hl-close-short-sol.json](../audit/cases/hl-close-short-sol.json)
- Filter command used: `nix run .#audit -- filter audit/normalized.json --tx-id 0xd83f79b33a4eb3a3d9b9042d4d89e20204320098d541d2757c082505f9428d8e --wallet 0x8d5a67da96cf80e013979c5c4cd0663d7090e3ca`
- Current normalized JSON: `the checked-in filtered case predates the perp-decision wave and is stale; current implementation should emit 4 rows; perp_close SOL totaling 31.34 units across four partial fills with per-row USDC fees plus exchange closed_pnl on each row`
- Human verification required: `TODO confirm these partial rows belong to one perp close event, confirm fee conservation, and decide whether per-fill closed_pnl should remain separate or be consolidated for later reporting`

## Transaction Review Template

- Label: `<fill in>`
- Source system: `<fill in>`
- Source reference: `<fill in>`
- Normalized snapshot file: `<fill in>`
- Filter command used: `<fill in>`
- Reviewer: `<fill in>`
- Status: `<pending | pass | fail | ambiguous>`

| Field | Expected from source | Actual normalized JSON | Notes |
| --- | --- | --- | --- |
| timestamp | `<fill in>` | `<fill in>` | |
| wallet | `<fill in>` | `<fill in>` | |
| tx_type | `<fill in>` | `<fill in>` | |
| sent | `<asset=<fill in>, amount=<fill in>>` | `<fill in>` | |
| received | `<asset=<fill in>, amount=<fill in>>` | `<fill in>` | |
| fee | `<asset=<fill in>, amount=<fill in>>` | `<fill in>` | |
| usd_value | `<sent=<fill in>, received=<fill in>, fee=<fill in>>` | `<fill in>` | |
