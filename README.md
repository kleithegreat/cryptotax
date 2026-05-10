# cryptotax

A crypto tax report generator that fetches transaction history from multiple
chains, normalizes it in Go, and runs it through a formally typed Haskell
financial core to produce IRS Form 8949 reports.

The Haskell core uses exact `Rational` arithmetic for lot accounting. The Go
side is deliberately conservative and best-effort: plain transfers are modeled
well, while richer semantic reconstruction such as multi-leg EVM swaps/bridges
and Hyperliquid perps still needs explicit review.

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
│  → FIFO Lot Tracker                              │
│  → Gain/Loss Engine                              │
│  → Form 8949 CSV Report                          │
│                                                  │
│  QuickCheck property tests on all invariants     │
└──────────────────────────────────────────────────┘
```

The two binaries communicate via a JSON intermediate representation defined in
`schema/transactions.json`. All financial amounts are strings. The Go side
normalizes source-specific data into that IR, and the Haskell side parses those
strings into exact `Rational` values.

## Prerequisites

- [Nix](https://nixos.org/download/) with flakes enabled
- [direnv](https://direnv.net/) (optional)

## Getting started

```bash
# Enter the development shell
cd cryptotax
nix develop
# or: direnv allow

# Build the binaries
nix build .#cli
nix build .#core

# Inspect normalized JSON without invoking the Haskell core
nix run .#dry-run -- \
  --eth-wallet 0x... \
  --sol-wallet ... \
  --hl-wallet ... \
  --robinhood-csv path/to/robinhood.csv

# Capture normalized JSON for audit review and record the re-run command
nix run .#audit -- capture audit/normalized.json \
  --eth-wallet 0x... \
  --sol-wallet ... \
  --hl-wallet ... \
  --robinhood-csv path/to/robinhood.csv

# Filter or summarize a captured audit snapshot
nix run .#audit -- filter audit/normalized.json --wallet 0x... --asset ETH
nix run .#audit -- summary audit/normalized.json --wallet 0x...

# Run the full pipeline with the flake-wrapped core binary
nix run .#run -- \
  --eth-wallet 0x... \
  --sol-wallet ... \
  --hl-wallet ... \
  --robinhood-csv path/to/robinhood.csv \
  --output 8949_report.csv

# Validate the flake outputs and Haskell test suite
nix flake check
```

API keys can be passed explicitly with `--etherscan-key` / `--helius-key` or
via `ETHERSCAN_API_KEY` / `HELIUS_API_KEY`. The CLI intentionally does not bake
env-derived secrets into flag defaults, so `--help` output does not echo them.

The audit summary reports row counts by `source` and `tx_type`, exact
sent/received/fee asset totals, and diagnostic row lists such as
`zero_usd_value_rows` and `suspicious_asset_rows` for manual review.

For reproducible manual review of normalized transactions, see
`docs/audit-workflow.md` and the first verified fixture in
`docs/known-transactions.md`.

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

3. **Transfers**: Clear own-wallet transfers are preserved as non-taxable
   transfer rows. The core ignores them, which keeps aggregate basis intact.

4. **Holding period**: > 365 days = long-term capital gains rate. Otherwise
   short-term (taxed as ordinary income).

5. **Output**: Form 8949 CSV with one row per disposal, ready for TurboTax /
   TaxAct / H&R Block import.

## Nix outputs

- `packages.cli`: Go CLI binary
- `packages.core`: Haskell financial core
- `apps.run`: wrapper that runs the Go CLI with `--core` pointed at the flake-built Haskell binary
- `apps.dry-run`: wrapper that runs the Go CLI with `--dry-run`
- `apps.audit`: wrapper that runs `cryptotax audit ...`
- `checks`: Go build, Haskell build, Haskell test suite
- `devShell`: Go + Haskell development environment

## Project structure

```
cryptotax/
├── flake.nix                 # Flake packages, apps, checks, and dev shell
├── docs/
│   ├── audit-workflow.md     # Reproducible audit steps for normalized JSON
│   └── known-transactions.md # Human-reviewed transaction comparison template
├── schema/
│   └── transactions.json     # JSON Schema (Go→Haskell contract)
├── go/
│   ├── cmd/main.go           # CLI entrypoint (cobra)
│   ├── audit/                # Audit payload capture, filtering, and summaries
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

## Known limitations

- EVM swaps and bridges are not fully reconstructed from per-address Etherscan rows yet; inbound legs are treated conservatively instead of guessed into taxable income.
- Hyperliquid perp activity is still approximated onto the current IR; positive funding receipts are modeled as ordinary income plus USDC acquisitions, while negative funding expenses are emitted to `funding_expenses.csv` as informational supplemental output without consuming USDC lots or making a broader tax-semantics claim.
- Source APIs can omit metadata or use token symbols that do not yet map cleanly to historical price lookups. Those rows fall back to `"0"` USD values instead of inventing prices.

## Disclaimer

This tool is for informational purposes only. It is not tax advice. Consult a
qualified tax professional for your specific situation. The author is not
responsible for any errors in tax calculations.
