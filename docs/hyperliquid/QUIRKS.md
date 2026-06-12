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

## 5. Per-endpoint response caps differ — funding is 500, fills are 2000

`userFillsByTime` returns at most 2000 fills per response while `userFunding` returns at most 500 entries. Pagination termination must check the per-endpoint cap: a single `< 2000` check silently truncated funding history to the first 500 records. The fetcher now paginates each endpoint against its own cap, advances the time window to the LAST record's timestamp (not +1, which skips same-millisecond records at page boundaries), and deduplicates the overlap by record identity.

## 6. Only the 10,000 most recent fills are served at all

Hyperliquid's API serves at most the 10,000 most recent fills per account regardless of pagination. Accounts with deeper fill history silently lose the oldest fills; there is no client-side workaround. If an account approaches that volume, export history before it rotates out.

## 7. `@N` spot identifiers resolve via the universe `index` field, not array position

`spotMeta` universe entries carry an explicit `index` field, and spot fills reference pairs as `@<index>`. The returned array order is not guaranteed to match those indices; keying by slice position can mislabel every spot asset.

## 8. Direction flips ("Long > Short") are normalized as perp closes

A flip fill closes the existing position and opens the opposite side in one fill; the exchange reports the realized PnL of the closed side on that fill. Normalization maps `Long > Short` / `Short > Long` to `perp_close` (capturing `closed_pnl` in the supplemental report). Any other unrecognized direction (e.g. liquidation strings) is skipped with a diagnostic instead of guessed — the old behavior force-classified every unknown direction as a spot BUY, inverting real spot sells.

## 9. Deposits and withdrawals are not fetched

The fetcher reads fills and funding only. USDC bridged into Hyperliquid is seen by the Etherscan side as an outbound transfer (classified `sell` to an unknown counterparty) and never appears as a Hyperliquid acquisition, so HL spot trades and funding expenses consume USDC the core never saw arrive. This double-exposure is a known unresolved gap — see `docs/hyperliquid/REVIEW.md`.
