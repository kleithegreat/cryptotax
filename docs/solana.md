# Solana

## Intent

- Movements come from raw `getTransaction` balances (pre/post lamports and
  token balances by owner), not from any indexer's interpretation. Helius'
  enhanced-transaction parser was dropped: it reported pump.fun buys at about
  1% of their SOL cost, missed sells, and labeled swaps as transfers.
- Lamports held by the wallet's own token accounts are the wallet's SOL. Rent
  for its own accounts and wrapping SOL therefore net to zero, and wSOL token
  amounts are excluded so nothing is counted twice.
- Failed transactions still cost their fee payer the fee and are included.
- The fee counts only when the wallet is the fee payer; sponsored fees belong
  to the sponsor.

## Quirks

- `getTokenAccountsByOwner` does not list closed token accounts, and an
  inflow to a token account never names its owner. History is complete only
  because every wallet-owned token account seen in any fetched transaction is
  crawled too, repeated until nothing new appears. Without that, a CASH
  inflow went missing and the replay could not reach the live balance.
- Solana gives no order inside a second; see docs/ledger.md for the
  same-instant rule.
- Address poisoners send 1-lamport transfers from lookalike addresses right
  after real transfers; they are classified as spam.
