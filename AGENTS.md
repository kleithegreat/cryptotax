# Agent Guidelines

`cryptotax` produces US tax reports from on-chain and exchange history: Go
(`go/`) fetches sources and classifies what happened, the Haskell core
(`haskell/`) applies tax rules in exact arithmetic. Accuracy is the product:
a wrong number that looks plausible is the worst possible outcome, so
anything the data cannot decide must block the report, never be guessed.

## Documentation

Each domain has exactly one doc: `docs/<domain>.md` (`core`, `ledger`,
`assets`, `solana`, `evm`, `hyperliquid`, `robinhood`). Read the relevant ones,
plus `TODO.md`, before working.

Each doc has two sections and nothing else:

- **Intent**: the tax and design decisions the code cannot express, and why.
  This is the review anchor. A tax-treatment change is an Intent change:
  update the doc in the same change and cite the authority.
- **Quirks**: non-obvious operational knowledge, each naming the trap a
  competent reader would fall into from the code alone.

Docs never describe the implementation. Delete any statement your change made
false; resolved `TODO.md` entries are deleted.

## Comments

One line at the code it protects, stating a local invariant. Anything longer
belongs in the domain doc. No narration, no history.

## Validation

```bash
nix flake check                       # Go tests + Haskell tests
nix run . -- report --offline         # real data from .cache/, reports/ out
```

Treat the real-data run as the integration test. Balance reconciliation
(history replayed against live balances) must stay clean, and review items
that appear or disappear must be explained. Never commit personal data:
`cryptotax.json`, `.env`, `data/`, `.cache/` and `reports/` are gitignored, and
tests use synthetic fixtures.
