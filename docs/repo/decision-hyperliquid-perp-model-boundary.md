# Decision: Hyperliquid Perp Model Boundary

Status: `done`
Task DAG node: `decision.hyperliquid_perp_model_boundary`

## Outcome

Option C was chosen and implemented.

Hyperliquid perpetual fills are now quarantined from spot FIFO and carried through the IR explicitly:

- `OPEN LONG` / `OPEN SHORT` normalize to `perp_open`
- `CLOSE LONG` / `CLOSE SHORT` normalize to `perp_close`
- `closed_pnl` is propagated from the Hyperliquid API onto `perp_close`
- `handlePerpOpen` is a no-op in the Haskell core
- `handlePerpClose` emits `PerpPnlEntry` rows written to `perp_pnl.csv`
- perp rows no longer create phantom spot lots or consume spot FIFO lots

## Current behavior

### 1. Fetch

`fetchFills` in `go/fetcher/hyperliquid.go` now propagates `ClosedPnl` onto the raw row. `StartPosition` remains on the raw fetcher row for possible future use.

### 2. Normalize

`normalizeHyperliquid` in `go/normalize/normalize.go` now maps perp directions into dedicated IR types:

- `OPEN LONG` / `OPEN SHORT` -> `perp_open`
- `CLOSE LONG` / `CLOSE SHORT` -> `perp_close`

`perp_close` rows carry `closed_pnl` when the API provides it.

### 3. Core accounting

`haskell/src/GainLoss.hs` now treats perp rows separately from spot inventory:

- `handlePerpOpen` creates no lots
- `handlePerpClose` uses exchange-reported `closed_pnl` to emit `PerpPnlEntry`
- spot FIFO queues are unaffected by perp activity

### 4. Output

`haskell/app/Main.hs` writes `perp_pnl.csv` alongside the main 8949 output when any realized perp PnL entries exist. Human decision now fixes that boundary intentionally: canonical output keeps realized perp PnL in the separate report instead of forcing it into 8949.

### 5. Test coverage

`haskell/test/Spec.hs` freezes the quarantine and PnL behavior with:

- `prop_perpOpenDoesNotCreateLots`
- `prop_perpCloseUsesClosedPnl`
- `prop_perpCloseWithoutPnlIsError`
- `prop_perpCloseWithoutSentIsError`
- `prop_perpDoesNotContaminateSpotFIFO`

## Why Option C was chosen

- Option A would have kept producing structurally wrong spot-like output for perps.
- Option B would have prevented FIFO contamination but still withheld realized PnL that the exchange already reports.
- Option C preserves the quarantine boundary from spot accounting while still surfacing realized perp PnL in a dedicated non-8949 report.

## Options not chosen

### Option A: keep the old spot-like approximation

Not chosen because it created phantom lots, inverted short-position semantics, and contaminated spot FIFO.

### Option B: quarantine only, no realized PnL output

Not chosen because the repo already had exchange-reported `ClosedPnl` available and could surface it honestly in a separate current-behavior-only report.

## Remaining work after the decision

The boundary decision is resolved. Remaining work is now support-boundary and reporting cleanup:

- `verification.expand_human_verified_exemplars` still blocks promotion of the documented real-wallet perp cases
- partial-fill consolidation remains open; one economic event can still emit multiple `PerpPnlEntry` rows
- canonical output intentionally keeps derivative PnL outside 8949 in the separate report; remaining work is about consolidation and support verification, not 8949 formatting
- future grouping or richer event metadata may still require a separate IR contract change, but that is not this already-resolved decision
