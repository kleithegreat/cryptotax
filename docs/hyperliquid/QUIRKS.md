# Hyperliquid Quirks

This document captures non-obvious Hyperliquid API and normalization gotchas that are easy to miss from code alone.

## 1. Funding row identifiers are still often the all-zero hash from the API

Hyperliquid's `userFunding` endpoint returns a `hash` field that is often the all-zero string (`0x0000000000000000000000000000000000000000000000000000000000000000`) for funding entries. The pipeline still propagates this as-is into the normalized `id` field.

To keep funding rows auditable despite that weak source id, normalization now also preserves `market` and a stable `event_group_id` on funding rows. `id` alone is still not enough evidence linkage.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `hl-funding-positive-2025-12-02` entry shows the all-zero hash as its transaction id.

This is tracked in `docs/hyperliquid/REVIEW.md` under "Funding rows now preserve market context, but the source `id` can still be weak."

## 2. Older funding audit fixtures predate the `market` and `event_group_id` upgrade

Current normalization preserves Hyperliquid funding market context in the IR `market` field and adds a stable `event_group_id`. Some checked-in audit case files and documented current-behavior packets predate this upgrade and therefore do not yet show those fields.

## 3. Fill asset names depend on live spot metadata resolution for `@N` identifiers

Hyperliquid fill records use `@N` notation for spot token identifiers (e.g., `@1` rather than a human-readable coin name). `Fetch` in `go/fetcher/hyperliquid.go` calls `fetchSpotMeta` at fetch time and resolves these through the current spot metadata mapping. If spot metadata is unavailable or the mapping changes between fetches, the same `@N` identifier could resolve to different asset names. Perp fills use human-readable `Coin` strings and are not affected.

## 4. Partial fills share one API hash without consolidation

When Hyperliquid returns multiple partial fills for one economic event, each fill gets its own API row with the same `hash`. The pipeline emits one normalized row per API row without any later consolidation step. Those rows now share `event_group_id` and carry `split_reason = "api_fill_granularity"`, but one perp close or one large trade may still appear as several separate rows in normalized output.

Example from real-wallet audit cases in `docs/known-transactions.md`: the `hl-close-short-sol` entry is still the multi-row exemplar. Current implementation emits four separate `perp_close` rows for that case, but the checked-in filtered case snapshot still shows the old spot-like `sell` rows and needs refresh.
