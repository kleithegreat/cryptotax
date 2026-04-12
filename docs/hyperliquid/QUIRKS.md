# Hyperliquid Quirks

This document captures non-obvious Hyperliquid API and normalization gotchas that are easy to miss from code alone.

## 1. Funding row identifiers are the all-zero hash from the API

Hyperliquid's `userFunding` endpoint returns a `hash` field that is the all-zero string (`0x0000000000000000000000000000000000000000000000000000000000000000`) for funding entries. The pipeline propagates this as-is into the normalized `id` field. This means funding rows cannot be uniquely traced back to their source event by `id` alone.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `hl-funding-positive-2025-12-02` entry shows the all-zero hash as its transaction id.

This is tracked as a review item in `docs/hyperliquid/REVIEW.md` under "Funding rows lose market context and still have weak evidence linkage."

## 2. Market context is dropped during funding normalization

`normalizeHyperliquid` in `go/normalize/normalize.go` emits funding rows with only the USDC flow (sent or received). The raw row carries `Delta.Coin` (the Hyperliquid market that generated the funding payment), but normalization drops it. A reviewer cannot tell from the normalized row which market produced a given funding event.

This is documented as a divergence in `docs/hyperliquid/ARCHITECTURE.md` and tracked in `docs/hyperliquid/REVIEW.md`.

## 3. Fill asset names depend on live spot metadata resolution for `@N` identifiers

Hyperliquid fill records use `@N` notation for spot token identifiers (e.g., `@1` rather than a human-readable coin name). `Fetch` in `go/fetcher/hyperliquid.go` calls `fetchSpotMeta` at fetch time and resolves these through the current spot metadata mapping. If spot metadata is unavailable or the mapping changes between fetches, the same `@N` identifier could resolve to different asset names. Perp fills use human-readable `Coin` strings and are not affected.

## 4. Partial fills share one API hash without consolidation

When Hyperliquid returns multiple partial fills for one economic event, each fill gets its own API row with the same `hash`. The pipeline emits one normalized row per API row without any later consolidation step. This means one perp close or one large trade may appear as several separate rows in normalized output.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `hl-close-short-sol` entry is still the multi-row exemplar. Current implementation emits four separate `perp_close` rows for that case; the checked-in filtered case snapshot predates the perp-decision wave and still needs refresh.
