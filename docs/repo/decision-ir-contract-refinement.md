# Decision: IR Asset Identity Contract Refinement

Status: `done`
Task DAG node: `decision.ir_contract_refinement`

## Outcome

Option B was chosen and implemented.

`AssetAmount` now carries an optional `asset_canonical` field alongside the existing `asset` display field:

- Go: `go/types/types.go`
- Haskell: `haskell/src/Types.hs`
- Solana normalization populates it in `go/normalize/normalize.go`

Old payloads without `asset_canonical` still parse, so the change is backward compatible.

## Current behavior

The normalized IR now preserves display and canonical identity separately when the source provides both.

Example:

- Raw Solana row: mint `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`, symbol `USDC`
- Normalized leg: `asset = "USDC"`, `asset_canonical = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"`

If the source provides only one identity, the normalized row keeps that value in `asset` and omits `asset_canonical`.

This resolves the original schema gap where a source-backed symbol could erase the canonical mint during normalization.

## Why Option B was chosen

- It preserves both identities without breaking existing output.
- It avoids a breaking JSON contract change.
- It lets downstream consumers adopt canonical identity incrementally instead of forcing an all-at-once rewrite.
- It keeps human-readable display output in `asset` while preserving source-backed canonical identity separately.

## Options not chosen

### Option A: Prefer canonical identity in the existing `asset` field

Not chosen because it would have replaced readable display output with mint strings, broken the current price-lookup path, and changed existing cross-source grouping behavior without preserving both identities.

### Option C: Replace `asset` with a structured identity object

Not chosen because the blast radius was too large for the immediate problem. It would have been a breaking IR change across Go types, Haskell types, tests, fixtures, and downstream rendering.

## Remaining work after the decision

The decision is resolved. The remaining follow-on work is implementation adoption and support-boundary cleanup, not another pending decision on this node:

- downstream consumers still adopt `asset_canonical` only incrementally for lot tracking, transfer matching, and audit accumulation
- `solana.identity_valuation_support_boundary_upgrade` in `docs/repo/TASK_DAG.md` remains blocked by `verification.expand_human_verified_exemplars`, not by this decision
- cross-chain equivalence policy remains out of scope for this decision; preserving canonical identity does not, by itself, decide whether assets from different sources should share lots
