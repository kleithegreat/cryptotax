# cryptotax

US crypto tax reports (Form 8949, Schedule D totals, ordinary income) for
tax years 2023 onward, from:

| Source      | What is read                                              | Key                 |
|-------------|-----------------------------------------------------------|---------------------|
| Ethereum, Arbitrum | every transaction, internal transfer, token and NFT transfer (Etherscan v2) | `ETHERSCAN_API_KEY` |
| Solana      | raw transactions of the wallet and all its token accounts | `HELIUS_API_KEY`    |
| Hyperliquid | perp fills, funding, deposits/withdrawals/transfers       | none                |
| Robinhood   | Form 1099-DA section of the consolidated 1099 CSV         | none                |
| DefiLlama   | historical USD prices by contract/mint                    | none                |

The tax treatments, and the authority for each, are in `docs/core.md`.

## Use

```bash
cp cryptotax.example.json cryptotax.json   # wallets, timezone, labels
echo 'ETHERSCAN_API_KEY=…' >> .env; echo 'HELIUS_API_KEY=…' >> .env
nix run . -- report                        # fetch, classify, write reports/
nix run . -- report --offline              # rerun from the cached evidence
nix run . -- events                        # the classified events, as JSON
```

`reports/` gets, for each year: `form8949.csv`, `income.csv`,
`holdings-end.csv` (lots at year end) and `summary.txt` (Schedule D lines
and totals). Across all years it gets `review.csv`, `exported-lots.csv` (lots
sent to your untracked exchange accounts, with their basis) and
`events.json`. Every source response is kept in `.cache/` as evidence.

## Review loop

A blockchain cannot say whether an address is your exchange account, a
friend, or a merchant. Anything the data cannot decide is a **blocking**
line in `review.csv`, and the run prints PROVISIONAL (exit code 3) until
none remain. Resolve each one in `cryptotax.json`:

- `counterparties`: label an address once. Use `account` for a deposit
  address of a tracked account such as Robinhood, `custody` for your own
  untracked exchange account, `third_party` for someone else, and `spam`.
- `overrides`: classify one movement by id. The treatments are `income`,
  `spend`, `gift_out`, `lost`, `custody_out`, `custody_in`, `gift_in`, `spam`,
  `ignore`, `trade` (confirm a lopsided exchange), `pair` (join two halves of
  a bridge/swap), and `basis` (supply a broker sale's missing basis).

Then rerun with `--offline`. The run also replays every account's history
against its live balance and blocks on any difference, because a gap means
missing history and therefore wrong basis.

Not tax advice. Check the decisions in `docs/core.md` with a tax
professional, especially perpetual-futures treatment.
