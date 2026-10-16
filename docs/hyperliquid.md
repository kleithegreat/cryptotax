# Hyperliquid

## Intent

- The account is its own tracked account holding USDC (cash) and perpetual
  positions. Fills become perp events; funding becomes funding events; the
  non-funding ledger (deposits, withdrawals, sends) becomes movements.
- `closedPnl` is gross: fees are separate, and `fee` already includes builder
  fees. This was verified by replaying deposits, withdrawals (plus their $1
  fee), sends, closed PnL, fees and funding: the replay landed within $0.29
  of the live balance. Under the alternative readings it was hundreds off.
- A flip fill ("Long > Short") closes the whole old position and opens the
  remainder; the exchange's PnL belongs to the closing part.
- Spot fills, vaults, staking and non-USDC transfers are not modeled; they
  surface as blocking review items rather than being guessed.

## Quirks

- A deposit's or withdrawal's ledger hash is the Arbitrum bridge
  transaction's hash, so both sides share one movement id and pair exactly.
- Under unified margin, the spot USDC "total" already includes the
  unrealized PnL of open positions. Reconciliation subtracts it to compare
  with realized history.
- `send` to `0x2000000000000000000000000000000000000000` moves USDC to
  HyperEVM, not to a person.
- A `withdraw`'s or `send`'s fee is charged on top of the amount, but an
  `internalTransfer`'s fee comes out of what the recipient gets. Reading them
  the same way left a trading-bot account $2 off its live balance.
- The API serves only the 10,000 most recent fills; past that, history is
  lost for good. Export before then.
- Funding hashes are all zeros; funding is keyed by market and time.
