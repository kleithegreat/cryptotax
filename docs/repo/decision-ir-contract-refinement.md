# Decision: IR Asset Identity Contract Refinement

Status: `needs_human_decision`
Task DAG node: `decision.ir_contract_refinement`

## Current behavior

The normalized IR uses one `asset` string per leg (`types.AssetAmount.Asset` in Go, `aaAsset` in Haskell). Every downstream consumer — lot tracking, transfer matching, audit accumulation — keys on that single string.

The Helius fetcher *does* preserve canonical mint identity and source-backed display symbol separately in `fetcher.RawTransaction` (`Asset` + `AssetSymbol`). But normalization collapses them:

```
heliusDisplayAsset(mint, symbol):
  if symbol != "" → return UPPER(symbol)     ← mint is lost
  else            → return mint as-is
```

### Concrete example of the collapse

A Solana USDC transfer:
- Raw: `Asset = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"`, `AssetSymbol = "USDC"`
- Normalized: `asset = "USDC"` — the mint is gone

A mint-only token (no Helius symbol):
- Raw: `Asset = "7GCihgDB8fe6KNjn2MYtkzZcRjQy3t9GHdC8uHYmW2hr"`, `AssetSymbol = ""`
- Normalized: `asset = "7GCihgDB8fe6KNjn2MYtkzZcRjQy3t9GHdC8uHYmW2hr"`

### Where the single string is consumed

| Consumer | File / construct | What it does with `asset` |
|---|---|---|
| Lot tracking | `haskell/src/GainLoss.hs` via `handleBuy`, `handleSell`, `handleSwap`, and `recordIncomeReceipt` | `AssetSymbol (aaAsset ...)` is the FIFO queue key |
| Transfer matching | `go/transfer/match.go` via `sameAsset` | case-insensitive string equality decides whether two legs are the same asset |
| Audit accumulation | `go/audit/audit.go` via `accumulateAsset` | upper-cased displayed asset strings become per-asset summary keys |
| Price lookup | `go/normalize/normalize.go` via `heliusPriceLookupAsset` | symbol-or-mint selection drives CoinGecko lookup |
| Report rendering | `haskell/src/Report.hs` via `render8949CSV` | `unAsset (glAsset gl)` appears in the output description |

## Why the current contract is insufficient

1. **Identity collision.** Two distinct Solana tokens with the same Helius-provided symbol would merge into one lot queue and one audit bucket. The system cannot tell them apart after normalization.

2. **Irreversible information loss.** Once the mint is discarded at normalization, no downstream code can recover it. Audit review cannot link a normalized row back to its on-chain identity without re-fetching.

3. **Mixed semantics in one field.** The `asset` field is sometimes a canonical identifier (mint string, `"SOL"`, `"ETH"`), sometimes a display symbol (`"USDC"`, `"BONK"`), and sometimes an uppercased source token name from Etherscan. Consumers cannot tell which they are getting.

4. **Spec divergence.** `docs/ir/SPEC.md` says "if canonical identity and display identity differ, the repository should preserve that distinction." `docs/solana/SPEC.md` says "canonical Solana identity should prefer the mint." The implementation contradicts both.

These are not hypothetical: the current Solana pipeline already processes tokens where mint and symbol coexist, and the mint is already being dropped.

## Options

### Option A: Prefer canonical identity in the existing field

**Change:** Flip `heliusDisplayAsset` to return the mint when present, symbol only as fallback. No schema change.

**Touched files:**
- `go/normalize/normalize.go` — `heliusDisplayAsset`, `heliusPriceLookupAsset`
- `go/normalize/normalize_test.go` — update frozen expectations
- Existing real-wallet fixture `.expected.json` files

**Downstream effects:**
- Lot tracking keys become mint strings for Solana tokens → lots that were previously grouped by symbol (e.g. `"USDC"`) would now group by mint. Cross-chain lots for the same economic asset (Solana USDC vs Robinhood USDC) would no longer match.
- Transfer matching via `sameAsset` would compare mints, which is correct for same-chain matching but breaks cross-chain transfer pairs.
- Audit summaries become mint-keyed for Solana, symbol-keyed for everything else. Human readability degrades.
- CoinGecko price lookup currently depends on symbol strings → mint-keyed lookups would fail unless `coingeckoIDs` is expanded.
- Haskell output descriptions would show mint strings instead of readable symbols.

**What this solves:** Eliminates identity collision for Solana tokens. No schema change needed.

**What this does not solve:** The `asset` field still has mixed semantics (mint for Solana, symbol for others). Cross-chain asset grouping is broken. Human-readable output requires a separate display path.

### Option B: Add an optional `asset_canonical` field to `AssetAmount`

**Change:** Add `asset_canonical *string` (Go) / `aaAssetCanonical :: Maybe Text` (Haskell) alongside the existing `asset` field. The existing `asset` field continues to carry the display string. `asset_canonical` carries the mint or contract address when it differs from display.

**Touched files:**
- `go/types/types.go` — add field to `AssetAmount`
- `haskell/src/Types.hs` — add field to `AssetAmount`, update JSON instances
- `go/normalize/normalize.go` — populate `asset_canonical` in `normalizeHelius` (and optionally `normalizeEVM` for Etherscan contract addresses in the future)
- `go/transfer/match.go` — `sameAsset` gains a canonical-preferred comparison
- `go/audit/audit.go` — `accumulateAsset` can accumulate by canonical when present
- `haskell/src/GainLoss.hs` — lot key prefers canonical when present
- `haskell/src/Report.hs` — display rendering continues to use `aaAsset`
- Test files and fixtures that serialize `AssetAmount`

**Downstream effects:**
- JSON schema gains one new optional field per leg. Old payloads without it still parse (backward compatible).
- Lot tracking can key on canonical identity → Solana USDC (by mint) and Robinhood USDC (no canonical, falls back to `"USDC"`) remain separate or can be explicitly mapped.
- Transfer matching becomes canonical-aware → same-chain matching is more precise.
- Audit accumulation can offer both views (by canonical for dedup, by display for readability).
- No change to price lookup path — it can continue to use the display string for CoinGecko.
- The Haskell core needs a small accessor change (`lotKey` function that prefers canonical).

**What this solves:** Preserves both identities. Backward compatible. Display path unchanged. Each consumer can opt into canonical identity at its own pace.

**What this does not solve:** `asset` field semantics remain mixed (symbol for some chains, uppercased name for others). Does not address the broader question of cross-chain asset equivalence.

### Option C: Replace `asset` string with structured `AssetIdentity`

**Change:** Replace the single `asset` string in `AssetAmount` with a structured object:

```json
{
  "asset": {
    "canonical": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
    "display": "USDC",
    "chain": "solana"
  }
}
```

**Touched files:**
- `go/types/types.go` — new `AssetIdentity` struct, change `AssetAmount.Asset` from `string` to `AssetIdentity`
- `haskell/src/Types.hs` — mirror the struct, update all JSON instances
- `go/normalize/normalize.go` — all normalizers must construct `AssetIdentity`
- `go/transfer/match.go` — matching logic changes to use structured fields
- `go/audit/audit.go` — accumulation and summary keys change
- `haskell/src/GainLoss.hs`, `haskell/src/Lot.hs` — lot key type changes
- `haskell/src/Report.hs` — display rendering changes
- All test files, all fixtures, all golden files

**Downstream effects:**
- Breaking JSON schema change. Old payloads will not parse.
- Every consumer must be updated simultaneously.
- Solves the mixed-semantics problem completely for all chains.
- Enables future cross-chain equivalence mapping at the IR level.
- Largest blast radius of any option.

**What this solves:** Full separation of identity concerns. Clean semantics for all chains. Future-proof for cross-chain asset mapping.

**What this does not solve:** Still does not define cross-chain equivalence policy (is Solana USDC == Ethereum USDC for lot purposes?). That remains a separate human decision.

## Tradeoffs

| | Option A | Option B | Option C |
|---|---|---|---|
| Schema change | None | Additive (backward compatible) | Breaking |
| Blast radius | Normalization + fixtures | IR types + all consumers | Everything |
| Solana identity collision fixed | Yes | Yes | Yes |
| Display output preserved | No (mints in output) | Yes | Yes |
| Cross-chain grouping | Broken for Solana | Opt-in per consumer | Clean but needs equivalence policy |
| Reversibility | Easy to revert | Moderate | Hard to revert |
| Incremental adoptability | Immediate | Consumer by consumer | All at once |

## Recommended smallest good decision

**Option B: add optional `asset_canonical` to `AssetAmount`.**

Reasoning:
- It solves the concrete Solana identity collision without breaking existing output or cross-chain behavior.
- It is backward compatible — old captured payloads still parse.
- Each consumer (lot tracking, transfer matching, audit) can adopt canonical identity incrementally, with its own test coverage.
- It does not require solving cross-chain asset equivalence now.
- It aligns with the spec's language: "preserve that distinction as far upstream as the schema allows."
- If it later proves insufficient, migrating from B to C is straightforward because the data is already present.

Option A is tempting for its simplicity but it trades one set of broken semantics for another (mints in display output, broken price lookups, broken cross-chain lot grouping). The fix would need to be undone when display identity is inevitably re-added.

Option C is the cleanest end state but has unjustified blast radius for the current problem scope. It should remain a future possibility, not a prerequisite.

## What is a human design decision versus an implementation detail

### Human decisions required

1. **Whether to add `asset_canonical` to the IR at all** — this is a long-term contract change per `AGENTS.md` and `docs/repo/SPEC.md`.
2. **Which option (A, B, or C)** — determines the schema contract going forward.
3. **Whether Solana mint identity should be the canonical key for lot tracking** — this is a tax-semantic decision (grouping by mint vs by symbol changes what lots match what disposals).
4. **Whether cross-chain equivalence (Solana USDC mint == `"USDC"` symbol from Robinhood) needs a mapping table or remains separate** — this is out of scope for this decision but must be acknowledged.

### Implementation details (agent-executable once decided)

- Populating `asset_canonical` in each normalizer
- Updating `sameAsset` to prefer canonical comparison
- Updating `accumulateAsset` to accumulate by canonical
- Updating Haskell `AssetAmount` and JSON instances
- Updating lot key selection in `GainLoss.hs`
- Updating test fixtures and golden files

## What remains blocked until human decision

Per `TASK_DAG.md`:
- `solana.identity_valuation_support_boundary_upgrade` — cannot upgrade Solana identity handling without knowing the IR contract
- `core.accounting_support_upgrade` — cannot strengthen lot-tracking semantics without canonical identity
- `hyperliquid.perp_semantics_upgrade` — partially blocked (any future asset identity work depends on this)
