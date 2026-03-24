# Known Transactions

Use this file as a human-reviewed comparison log for representative normalized
transactions. Keep verified cases at the top, then duplicate the template block
below for new evidence.

## Verified Cases

### 1. Local Golden Fixture: Buy, Sell, and Own-Wallet Transfer

- Evidence id: `basic-buy-sell-transfer`
- Scope: one `buy`, one `sell`, and one non-taxable own-wallet transfer pair
- Normalized fixture:
  [basic-buy-sell-transfer.json](/home/kevin/repos/cryptotax/haskell/testdata/basic-buy-sell-transfer.json)
- Expected 8949 CSV:
  [basic-buy-sell-transfer-8949.csv](/home/kevin/repos/cryptotax/haskell/testdata/basic-buy-sell-transfer-8949.csv)
- Validation path: `nix flake check` runs the fixture through the Haskell core and asserts the exact CSV output
- Status: `pass`

| Check | Expected result | Evidence |
| --- | --- | --- |
| Buy basis math | 2 ETH lot with total basis `4020.00` USD | Buy row has received USD `4000.00` and fee USD `20.00` |
| Transfer handling | No 8949 row from the own-wallet transfer pair | The core ignores `transfer_out` and `transfer_in` rows |
| Sell proceeds math | Net proceeds `2970.00` USD | Sell row has sent USD `3000.00` and fee USD `30.00` |
| Final 8949 row | One short-term disposal row for `1 ETH` with gain `960.00` USD | CSV fixture contains exactly `1 ETH,01/15/2024,08/20/2024,2970.00,2010.00,960.00,Short` |

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
