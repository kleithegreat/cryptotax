# Hyperliquid Review

## Purpose

This document tracks grounded gaps between `docs/hyperliquid/SPEC.md` and the current Hyperliquid fetch, normalization, and downstream accounting behavior.

## Open review items

### Funding rows lose market context and still have weak evidence linkage

- Status: `needs_human_decision`
- Issue type: `schema/contract gap`, `evidence gap`
- Why this is a spec/implementation gap: `docs/hyperliquid/SPEC.md` says the pipeline should preserve enough information for later accounting improvements and human review, and it names evidence retention and linkage for funding rows as an immediate priority. The current normalized funding row keeps only the USDC flow and a weak source identifier.
- Current implementation evidence: `fetchFunding` in `go/fetcher/hyperliquid.go` receives `hlFunding.Delta.Coin`, stores it in `RawTransaction.Asset`, and copies `hlFunding.Hash` to `ID`. `normalizeHyperliquid` in `go/normalize/normalize.go` emits only USDC `Sent` or `Received` on `funding_payment`, so the market context is dropped. `docs/known-transactions.md` records real-wallet funding rows whose current `id` is the all-zero hash and still needs source-row retention for review.
- Desired direction implied by the spec: a reviewer should be able to connect one normalized funding row back to the Hyperliquid market and source event that produced it.
- What blocks resolution: `types.Transaction` has no dedicated market-context or secondary-source-identifier field, so preserving that context would change the IR contract.
- Smallest good next checkpoint: decide what minimum market and evidence context a funding row must retain before implementation work starts.

### Negative funding is preserved, but final output semantics are still undecided

- Status: `needs_human_decision`
- Issue type: `human-decision blocker`
- Why this is a spec/implementation gap: `docs/hyperliquid/SPEC.md` says negative funding must stay visible and explicitly calls out final tax-output semantics for negative funding as unresolved. The current pipeline preserves the event but still cannot say how it should appear in final output.
- Current implementation evidence: `normalizeHyperliquid` in `go/normalize/normalize.go` turns negative funding into outbound USDC on `funding_payment`. `handleFunding` in `haskell/src/GainLoss.hs` then turns that row into an unsupported expense warning in `prErrors`. `Main.main` in `haskell/app/Main.hs` writes only the 8949-style CSV file, so the negative funding row does not become durable final output.
- Desired direction implied by the spec: keep negative funding explicit without reinterpreting it as spot inventory activity, and decide whether and how ordinary expense handling should appear downstream.
- What blocks resolution: the ordinary-expense posture is a tax and output decision that needs human approval, and the current funding exemplars still require human verification.
- Smallest good next checkpoint: decide one explicit supported or unsupported final-output posture for negative funding so implementation can stay inside a documented boundary.

### Perp fills cross the IR as spot-like rows at API-fill granularity

- Status: `needs_human_decision`
- Issue type: `support-boundary gap`, `human-decision blocker`
- Why this is a spec/implementation gap: `docs/hyperliquid/SPEC.md` says agents must not present perp activity as fully solved spot semantics when that is not actually true. The current implementation still routes perp fills through spot-like transaction types and spot lot accounting.
- Current implementation evidence: `fetchFills` in `go/fetcher/hyperliquid.go` emits one `RawTransaction` per `hlFill`. `normalizeHyperliquid` in `go/normalize/normalize.go` maps `OPEN LONG` and `OPEN SHORT` to `TxBuy`, `CLOSE LONG` and `CLOSE SHORT` to `TxSell`, and all other directions to `TxSwap`. `handleBuy`, `handleSell`, and `handleSwap` in `haskell/src/GainLoss.hs` then apply spot inventory logic. `docs/known-transactions.md` records `hl-open-long-btc` and `hl-close-short-sol` as pending review targets.
- Desired direction implied by the spec: either keep perp fills explicitly current-behavior-only with durable surfacing, or adopt a distinct perp position and PnL model instead of routing them through spot inventory semantics.
- What blocks resolution: the repo still needs a human decision on the future perp model boundary, and the documented real-wallet fill exemplars still need human verification.
- Smallest good next checkpoint: decide whether future Hyperliquid support will keep an explicit current-behavior-only spot-like representation or move toward a dedicated perp model.

## Non-goals / intentionally narrow boundaries

- This review doc does not ask the repository to invent realized PnL from partial Hyperliquid data.
- Spot `@N` asset resolution through `fetchSpotMeta` is not itself under review here.
