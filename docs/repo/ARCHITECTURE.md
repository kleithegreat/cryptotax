# Repository Architecture

This document describes the current high-level implementation shape of `cryptotax`.

## Top-level layout

- `./go` — source fetchers, normalization pipeline, transfer matching, pricing helpers, and audit tooling
- `./haskell` — financial core for lot accounting and final tax-output generation
- `flake.nix` — repository entry point for supported builds, checks, and runnable commands
- `docs/` — design intent, implementation maps, review backlog, and operational notes

## Runtime pipeline

The repository currently works in two broad stages:

1. **Go stage**
   - fetches source data from Etherscan, Helius, Hyperliquid, and Robinhood CSV
   - converts source payloads into raw transaction rows
   - normalizes raw rows into a JSON transaction payload
   - performs transfer matching and conservative classification where supported
   - exposes audit tooling for capture, filtering, and summary generation

2. **Haskell stage**
   - consumes the normalized JSON payload
   - performs FIFO lot accounting and disposal calculations
   - emits final tax-oriented output, including 8949-oriented CSV for supported cases

## Supported repository commands

The repository is intentionally flake-native. Supported top-level entry points currently include:

- `nix build .#cli`
- `nix build .#core`
- `nix run .#run -- ...`
- `nix run .#dry-run -- ...`
- `nix run .#audit -- ...`
- `nix flake check`

Go tests run from the nested module via:

- `nix develop -c bash -lc 'cd go && go test ./...'`

## Current implementation characteristics

### Go side

The Go implementation currently owns:

- source-specific fetchers (which also own fee attribution: a fee on a raw row means the wallet actually paid it)
- raw transaction row construction
- normalization into the JSON IR, ending in the `finalizeTransaction` canonicalization boundary (canonical decimals, no negatives outside `closed_pnl`, classified tx_type — violations become structured skip diagnostics)
- exact decimal-string arithmetic for all money paths in `go/decimal` (no float64 anywhere on a money path)
- deterministic payload ordering in `types.SortTransactions` (timestamp, then acquisitions before disposals, then id/wallet)
- wallet identity canonicalization in `types.CanonicalWallet` (EVM addresses fold to lowercase; Solana base58 stays exact)
- transfer matching heuristics
- price lookup integration, identity-gated: a canonical on-chain identity (contract address / mint) decides the pricing symbol; unverified identities never price
- audit capture/filter/summary tooling
- snapshot-documentation fixtures for selected real-wallet cases

### Haskell side

The Haskell implementation currently owns:

- FIFO lot accounting
- basis and proceeds calculations for supported normalized events
- translation from normalized events into final tax-output rows
- exact golden coverage for a small verified local end-to-end fixture

## Current evidence / fixture layers

The repository currently has multiple evidence layers:

- small local end-to-end golden fixtures in `haskell/testdata`
- focused Go tests in `./go`
- audit workflow docs in `docs/audit-workflow.md`
- human-reviewed transaction tracking in `docs/known-transactions.md`
- current-behavior real-wallet fixtures in `go/audit/testdata/real-wallet`

These layers do not all mean the same thing:

- the local golden fixture is the strongest current proof of exact end-to-end behavior
- the real-wallet audit fixtures mainly freeze observed normalization behavior
- `known-transactions.md` is the current evidence log for what still requires human verification

## Current known architectural tension

The repository is currently stronger on implementation and regression coverage than on durable agent-readable design docs. The new `docs/*` hierarchy is intended to become the stable contract layer that sits above code and fixtures.
