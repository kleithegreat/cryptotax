# Decision: Hyperliquid Perp Model Boundary

Status: `needs_human_decision`
Task: `decision.hyperliquid_perp_model_boundary`
Date prepared: 2026-04-11

## How perp fills are represented today

Hyperliquid perp fills currently flow through the pipeline as spot-like transactions:

1. **Fetch** (`go/fetcher/hyperliquid.go`): `fetchFills` receives `hlFill` records from the API. Each fill struct includes `Dir` (direction string like `Open Long`), `ClosedPnl`, `StartPosition`, and `Side`. Only `Dir` is propagated to the raw transaction; `ClosedPnl`, `StartPosition`, and `Side` are parsed but discarded.

2. **Normalize** (`go/normalize/normalize.go`): `normalizeHyperliquid` maps directions to spot-like transaction types:
   - `OPEN LONG` / `OPEN SHORT` → `buy` (received asset at fill price)
   - `CLOSE LONG` / `CLOSE SHORT` → `sell` (sent asset at fill price)
   - All other directions → `swap` (USDC-against-asset)

3. **Core** (`haskell/src/GainLoss.hs`): `handleBuy` creates a FIFO lot; `handleSell` disposes FIFO lots and emits a `GainLoss` row; `handleSwap` disposes then acquires. No handler distinguishes perp from spot.

4. **Granularity**: each API fill becomes one IR row. Partial fills sharing a hash remain separate. There is no grouping step.

Documented exemplars in `docs/known-transactions.md`:
- `hl-open-long-btc`: 1 row, `buy BTC 0.0004 @ 49.93 USD`
- `hl-close-short-sol`: 4 rows, `sell SOL` totaling 31.34 units across partial fills

## Why current spot-like output is only approximate

The spot-like mapping is structurally wrong for perpetual derivatives:

1. **Phantom lots from opens**: `OPEN LONG` creates a FIFO lot as if BTC were acquired. No BTC was actually purchased — the trader took a leveraged derivative position. The lot has a cost basis that will later be consumed by an unrelated spot sale or by the perp close, producing incorrect gain/loss math.

2. **Phantom disposals from closes**: `CLOSE LONG` disposes lots as if BTC were sold. The proceeds come from the fill price times size, not from actual realized PnL. The FIFO queue may match the close against an unrelated spot lot.

3. **Short positions are inverted**: `OPEN SHORT` becomes a `buy` (acquiring a phantom lot), and `CLOSE SHORT` becomes a `sell` (disposing it). In reality, a short open is economically a synthetic sale and a short close is a synthetic purchase. The PnL direction is reversed from what FIFO computes.

4. **ClosedPnl is discarded**: The Hyperliquid API provides `closedPnl` on every fill, which is the exchange's own realized PnL for that fill. This value is parsed into `hlFill.ClosedPnl` but never reaches the raw transaction, the IR, or the core.

5. **Partial-fill splitting**: One economic position change may arrive as multiple API fills (see `hl-close-short-sol` with 4 rows). Each row independently enters FIFO, potentially consuming lots at different basis prices than the aggregate position change warrants.

6. **Cross-contamination**: Perp lots and spot lots for the same asset share one FIFO queue. A spot BTC sale might consume a phantom lot from an `OPEN LONG`, and vice versa.

## Proposed support-boundary options

### Option A: Freeze and label — keep current behavior explicitly current-behavior-only

Accept that perp fills produce approximate output. Document the approximation explicitly and do not broaden the support claim.

**What changes:**
- Documentation only. No code changes.
- Existing `current-behavior-only` fixtures (`hl-open-long-btc`, `hl-close-short-sol`) remain the regression anchors.
- All perp-related 8949 rows carry an implicit "current-behavior-only, not a tax-correctness claim" caveat.

**What does not change:**
- Phantom lots continue to enter and leave the FIFO queue.
- Cross-contamination between perp and spot lots remains.
- `ClosedPnl` remains discarded.

**Files touched:**
- `docs/hyperliquid/SPEC.md` — strengthen the current-behavior-only label
- `docs/hyperliquid/ARCHITECTURE.md` — note the frozen approximation
- No code files

**Pros:**
- Zero code risk.
- Preserves all existing regression fixtures.
- Honest about the limitation.

**Cons:**
- Perp output is demonstrably wrong, and users who rely on the 8949 CSV for perp activity will get incorrect gain/loss numbers.
- Short-position PnL is inverted.
- Phantom lots can corrupt spot accounting for the same asset.

### Option B: Quarantine and propagate — limited perp-aware modeling

Stop perp fills from entering spot FIFO. Propagate `ClosedPnl` for evidence retention. Surface perp fills as explicitly unsupported rather than producing incorrect spot output.

**What changes:**
- `go/fetcher/hyperliquid.go`: propagate `ClosedPnl` and `StartPosition` into new fields on `RawTransaction`.
- `go/normalize/normalize.go`: map `OPEN LONG` / `OPEN SHORT` / `CLOSE LONG` / `CLOSE SHORT` to new IR type(s) (e.g., `perp_open`, `perp_close`, or a single `perp_fill`) instead of `buy` / `sell`. Carry `ClosedPnl` in the IR, possibly through an existing field like `raw_type` metadata or a new IR field.
- `haskell/src/Types.hs`: add new `TxType` variant(s) for perp events.
- `haskell/src/GainLoss.hs`: add handler(s) that surface perp fills as explicit unsupported rows in `prErrors` instead of routing them through `handleBuy` / `handleSell`.
- Regression fixtures for `hl-open-long-btc` and `hl-close-short-sol` would change from `buy`/`sell` to new type(s).

**What does not change:**
- No realized PnL computation from `ClosedPnl` yet.
- No position tracking.
- Spot FIFO accounting is unaffected (actually improved, since phantom perp lots no longer contaminate it).

**Files touched:**
- `go/fetcher/hyperliquid.go` — propagate `ClosedPnl`, `StartPosition`
- `go/types/types.go` (or equivalent) — new `RawTransaction` fields
- `go/normalize/normalize.go` — new tx_type mapping for perp directions
- `haskell/src/Types.hs` — new `TxType` variant(s), possibly new IR fields
- `haskell/src/GainLoss.hs` — new handler(s) for perp type(s)
- `haskell/test/Spec.hs` — new properties for perp quarantine behavior
- Existing regression fixtures — update expected output
- `docs/hyperliquid/SPEC.md`, `docs/hyperliquid/ARCHITECTURE.md` — update support boundary

**Pros:**
- Stops producing demonstrably wrong 8949 rows for perps.
- Stops phantom perp lots from corrupting spot FIFO.
- Preserves `ClosedPnl` for future use without committing to a PnL model yet.
- Honest: unsupported behavior is surfaced, not hidden behind incorrect spot semantics.

**Cons:**
- Perp activity produces no final output (only warnings), which may surprise users who currently get approximate numbers.
- Requires IR schema change (new tx_type or new fields).
- Regression fixture churn for existing perp test cases.

### Option C: API-PnL model — use ClosedPnl for realized perp output

Everything in Option B, plus use the API-provided `ClosedPnl` to emit realized gain/loss rows for perp closes without building a full position tracker.

**What changes (beyond Option B):**
- `haskell/src/GainLoss.hs`: perp close handler uses `ClosedPnl` directly as realized gain/loss instead of FIFO. Perp opens remain non-lot-creating. Emit a `GainLoss` row where proceeds minus basis equals the exchange-reported `ClosedPnl`.
- Need to decide how to represent the "basis" side of a perp close (notional entry price? zero? the exchange doesn't give us the open fill's price in the close record directly).
- Partial fill grouping question: emit one `GainLoss` per API fill, or consolidate fills sharing a hash?

**Files touched:**
- Everything from Option B
- `haskell/src/GainLoss.hs` — PnL computation from `ClosedPnl`
- `haskell/src/Report.hs` — decide how perp PnL rows appear in CSV output
- Possibly `haskell/src/Types.hs` — if `GainLoss` needs a perp-vs-spot flag

**Pros:**
- Produces realized PnL output for perp closes using exchange-provided data.
- No position tracker needed — the exchange already computed the PnL.
- More useful output than Option B for users with perp activity.

**Cons:**
- Relies on Hyperliquid's `ClosedPnl` as a source of truth for tax purposes. This is a new external source of truth adoption, which is a human-decision-boundary item per `AGENTS.md`.
- Unclear how to represent the "cost basis" and "proceeds" columns on 8949 for a perp PnL event (is it notional value? net settlement?). This is a tax-interpretation question.
- Partial-fill `ClosedPnl` values may need consolidation logic to avoid double-counting or row explosion.
- Larger scope than Option B with more human decisions required before implementation can start.

## Recommendation

**Option B (quarantine and propagate) is the smallest honest next boundary.**

Rationale:
- Option A is cheap but dishonest in practice: users who see 8949 rows for perp fills may reasonably believe those numbers are correct, when they are structurally wrong (especially for short positions). Labeling the approximation in docs does not prevent the incorrect output from being produced.
- Option B eliminates the incorrect output, protects spot FIFO from cross-contamination, and preserves `ClosedPnl` for a future Option C decision — all without requiring a tax-interpretation decision about how perp PnL should appear on 8949.
- Option C requires human decisions about source-of-truth adoption (`ClosedPnl`) and tax representation (how to fill 8949 columns for derivative PnL) that are not yet grounded in evidence or exemplars.

The recommended sequence is:
1. Human decides between Option A, B, or C (this memo).
2. If Option B: implementation can proceed as an agent task, since the quarantine boundary is clear and the IR change is narrow.
3. Option C becomes a separate future decision after perp exemplars are human-verified and the tax-representation question is resolved.

## What blocks this decision

- The exemplars `hl-open-long-btc` and `hl-close-short-sol` in `docs/known-transactions.md` still need human verification against Hyperliquid source data (task `verification.expand_human_verified_exemplars`).
- If Option C is chosen, `ClosedPnl` adoption as a tax source of truth requires human approval per `AGENTS.md` decision-boundary rules.
