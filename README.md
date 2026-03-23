# cryptotax

A crypto tax report generator that fetches transaction history from multiple
chains, normalizes it, and runs it through a formally-typed Haskell financial
core to produce IRS Form 8949 reports.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  Go binary (plumbing)                            │
│                                                  │
│  CLI → Fetchers → Normalizer → Transfer Matcher  │
│         │  │  │  │                               │
│         │  │  │  └─ Robinhood (CSV)              │
│         │  │  └──── Hyperliquid (REST, no key)   │
│         │  └─────── Helius/Solana (REST)         │
│         └────────── Etherscan V2 (ETH + ARB)     │
│                                                  │
│  CoinGecko price resolution for USD values       │
└──────────────────┬───────────────────────────────┘
                   │ JSON via stdin
┌──────────────────▼───────────────────────────────┐
│  Haskell binary (financial core)                 │
│                                                  │
│  Types (Rational arithmetic, no floats)          │
│  → FIFO Lot Tracker                             │
│  → Gain/Loss Engine                             │
│  → Form 8949 CSV Report                         │
│                                                  │
│  QuickCheck property tests on all invariants     │
└──────────────────────────────────────────────────┘
```

The two binaries communicate via a JSON intermediate representation defined
in `schema/transactions.json`. All financial amounts are strings — the Go
side never does arithmetic on them, and the Haskell side parses them into
exact `Rational` values.

## Prerequisites

- [Nix](https://nixos.org/download/) with flakes enabled
- [direnv](https://direnv.net/) (optional, for automatic shell activation)

## Getting started

```bash
# Clone and enter the dev shell
cd cryptotax
direnv allow    # or: nix develop

# Copy and fill in your API keys
cp .env.example .env
$EDITOR .env

# Build both binaries
make

# Dry run — fetch and normalize, print JSON without running Haskell core
make dry-run ETH_WALLET=0x... SOL_WALLET=...

# Full run — fetch, normalize, compute gains, write 8949 CSV
make run ETH_WALLET=0x... SOL_WALLET=... ROBINHOOD_CSV=data/robinhood.csv

# Run Haskell property tests
make test
```

## Supported sources

| Source       | Method               | API key required | Chain(s)         |
|-------------|----------------------|------------------|------------------|
| Etherscan V2 | REST API            | Yes (free)       | Ethereum, Arbitrum |
| Helius       | REST API            | Yes (free)       | Solana           |
| Hyperliquid  | REST API            | No               | Hyperliquid      |
| Robinhood    | CSV file export     | No               | N/A (CEX)        |
| CoinGecko    | REST API (prices)   | No               | N/A              |

## How tax calculations work

1. **Acquisition**: When you buy, receive income, or receive the "got" side of
   a swap, a new tax lot is created with your cost basis (USD value at that time).

2. **Disposal**: When you sell, send to someone, or send the "gave" side of a
   swap, the FIFO engine pops lots from the oldest acquisition and computes
   gain = proceeds - cost basis.

3. **Transfers**: Movements between your own wallets are matched by the Go layer
   and marked as non-taxable. Cost basis carries over.

4. **Holding period**: > 365 days = long-term capital gains rate. Otherwise
   short-term (taxed as ordinary income).

5. **Output**: Form 8949 CSV with one row per disposal, ready for TurboTax /
   TaxAct / H&R Block import.

## Project structure

```
cryptotax/
├── flake.nix                 # Nix dev shell + package builds
├── Makefile                  # Build orchestration
├── schema/
│   └── transactions.json     # JSON Schema (Go→Haskell contract)
├── go/
│   ├── cmd/main.go           # CLI entrypoint (cobra)
│   ├── fetcher/              # Chain-specific data fetchers
│   ├── price/                # CoinGecko USD price lookups
│   ├── normalize/            # Raw → unified transaction schema
│   ├── transfer/             # Own-wallet transfer matching
│   └── types/                # Shared Go structs
└── haskell/
    ├── app/Main.hs           # Reads JSON stdin, runs engine
    ├── src/
    │   ├── Types.hs          # ADTs with Rational arithmetic
    │   ├── Lot.hs            # FIFO cost basis lot tracker
    │   ├── GainLoss.hs       # Per-disposal gain/loss engine
    │   └── Report.hs         # Form 8949 CSV output
    └── test/Spec.hs          # QuickCheck properties
```

## Disclaimer

This tool is for informational purposes only. It is not tax advice. Consult a
qualified tax professional for your specific situation. The author is not
responsible for any errors in tax calculations.
