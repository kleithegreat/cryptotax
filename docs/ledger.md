# Ledger — classifying movements into events

## Intent

Sources report only facts: for each transaction and tracked account, the
exact net change per asset, the fee that account paid, whether its holder
signed, and the external addresses on the other side. Classification
decides what each change was, in this order. The first rule that applies
wins.

1. **Overrides** (`cryptotax.json` `overrides`, keyed by movement id) beat
   everything. `pair` joins two movements into one exchange or move.
2. **One transaction touching several own accounts** is a move (one asset)
   or an exchange whose output landed in another own account.
3. **Fee-only** transactions (approvals, failures) spend the fee.
4. **Two-sided** movements are exchanges.
5. **Counterparty labels** (`counterparties`): `account` mirrors the transfer
   into a tracked account (e.g. `robinhood`); `custody` is the taxpayer's own
   untracked account; `third_party` makes outflows payments and inflows
   income; `spam` gives a $0 basis.
6. **Spam and spoofs.** An inflow the holder did not sign is spam when it is
   worth under $0.01 or has no trusted price. An outflow the holder did not
   sign is an address-poisoning spoof (dropped) when its asset has no value.
   If it does have value, it may be theft and stays unresolved.
7. **Pairing.** A remaining outflow pairs with a remaining single-asset
   inflow in another own account landing 2 minutes before to 30 minutes after
   it, worth 90–102% of the outflow's net value. This covers bridges and
   cross-chain swaps done by solvers. Same asset is a move; different assets
   are an exchange. Every pairing is listed in the review as info.
8. **Everything else is unresolved**: provisionally removed or added at fair
   market value, and blocking.

Nothing is guessed silently: every heuristic outcome is visible in
`review.csv`, and anything that needs knowledge the chain lacks blocks the
report until the config supplies it.

Within one timestamp, events that only add are applied before events that
also remove, because balances never go negative and sources give no order
inside a second.

## Quirks

- Override keys are **movement** ids (`chain:tx`), not the combined event ids
  shown for paired events (`a+b`).
- Solver bridges often refund dust on the source chain, so the source side
  looks two-sided. Pairing uses the movement's net outflow value, otherwise
  such swaps would be booked as absurd in-chain exchanges.
- Address poisoners send dust from lookalike addresses (same first and last
  characters as a real counterparty) right after a real transfer. Never
  label a counterparty by its prefix.
