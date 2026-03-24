# Audit Workflow

This workflow is for manual validation of normalized transactions against source
data from Etherscan, Solana explorers, Robinhood CSV rows, or Hyperliquid API
results. The goal is to make reviews reproducible: capture one normalized JSON
snapshot, filter it consistently, then record findings in a shared template.

## 1. Capture a normalized snapshot

Run the audit capture flow and save the normalized JSON to a file:

```bash
nix run .#audit -- capture audit/normalized.json \
  --eth-wallet <eth-wallet> \
  --sol-wallet <sol-wallet> \
  --hl-wallet <hl-wallet> \
  --robinhood-csv <path-to-robinhood.csv>
```

What this does:

- writes normalized JSON to `audit/normalized.json`
- writes the exact re-run command to `audit/normalized.json.command`

Use the same wallet flags and input files that you want the audit to represent.
If you only need one source, omit the other flags.

## 2. Filter down to representative transactions

Start from the captured file and narrow the output before comparing it to the
source system.

Examples:

```bash
# Exact transaction id
nix run .#audit -- filter audit/normalized.json \
  --tx-id <tx-id>

# One wallet and one asset
nix run .#audit -- filter audit/normalized.json \
  --wallet <wallet> \
  --asset <asset>

# A timestamp window and raw type
nix run .#audit -- filter audit/normalized.json \
  --from-timestamp <2024-01-01T00:00:00Z> \
  --to-timestamp <2024-01-31T23:59:59Z> \
  --raw-type <raw-type>
```

Available filters:

- `--tx-id`: exact match on normalized transaction `id`
- `--from-timestamp`: lower bound on normalized RFC3339 UTC `timestamp`
- `--to-timestamp`: upper bound on normalized RFC3339 UTC `timestamp`
- `--wallet`: exact match on normalized `wallet`
- `--asset`: matches `sent.asset`, `received.asset`, or `fee.asset`
- `--raw-type`: exact match on normalized `raw_type`

Notes:

- Timestamp filters parse RFC3339 values and compare actual timestamps. Use the
  same UTC format that appears in the normalized JSON for consistency.
- If the same on-chain id appears once per owned wallet, combine `--tx-id` with
  `--wallet`.
- Redirect filtered output to a file if you want to attach a smaller audit
  artifact to a review:

```bash
nix run .#audit -- filter audit/normalized.json \
  --tx-id <tx-id> \
  --wallet <wallet> \
  > audit/tx-<label>.json
```

## 3. Optionally emit a machine-readable summary

The summary command runs over the full snapshot or any filtered subset and
emits stable JSON with counts by source, chain, transaction type, wallet, and
raw type, plus exact sent/received/fee totals grouped by asset.

```bash
nix run .#audit -- summary audit/normalized.json \
  --wallet <wallet> \
  --from-timestamp <2024-01-01T00:00:00Z>
```

Notes:

- Asset totals are grouped case-insensitively and emitted under upper-case
  asset keys.
- The summary is meant for reconciliation and review support; it does not add
  tax semantics beyond the normalized payload.

## 4. Choose representative cases

Do not sample only the easy rows. Pick transactions that exercise the current
normalization rules:

- a simple inbound transfer
- a simple outbound transfer
- an own-wallet transfer pair
- an EVM token movement that should stay conservative instead of being guessed
- a Solana multi-row transaction with a fee
- a Robinhood synthetic buy/sell pair
- a Hyperliquid fill or funding row

## 5. Record the comparison

Use [known-transactions.md](/home/kevin/repos/cryptotax/docs/known-transactions.md)
as the checklist template. Leave expected values blank until a human verifies
them from the source system.

For each reviewed transaction, capture:

- where the source truth came from
- which command produced the normalized row
- what the normalized JSON currently says
- whether the normalized row is clearly correct, clearly wrong, or ambiguous

## 6. When to stop and file follow-up work

Do not guess expected values to make the checklist look complete. If the source
data is ambiguous or the current model is too weak, record that explicitly and
file follow-up work instead of forcing a pass.
