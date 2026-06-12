# Audit Architecture

## Purpose

The audit tooling operates on normalized payloads. It captures a review snapshot, filters that snapshot without mutating rows, and builds machine-readable summaries that surface unresolved cases early.

## Current ownership boundary

- `go/cmd/audit.go` owns the `audit` CLI and its `capture`, `filter`, and `summary` subcommands.
- `go/audit/audit.go` owns payload I/O, filtering, summary generation, and summary flag construction.
- `go/audit/real_wallet_regression_test.go` owns the current-behavior regression layer for documented real-wallet fixtures.
- `docs/audit-workflow.md` and `docs/known-transactions.md` describe the human workflow layered on top of those commands.
- Audit does not change transaction semantics. It works on the normalized payload produced elsewhere.

## Main files and named constructs

- `go/cmd/audit.go`
- `go/audit/audit.go`
- `go/audit/audit_test.go`
- `go/audit/real_wallet_regression_test.go`
- `docs/audit-workflow.md`
- `docs/known-transactions.md`

Important named constructs:

- `newAuditCaptureCmd`
- `newAuditFilterCmd`
- `newAuditSummaryCmd`
- `buildAuditFilters`
- `WriteCaptureArtifacts`
- `FilterPayload`
- `BuildSummary`
- `buildZeroUSDValueRow`
- `buildSuspiciousAssetRow`
- `suspiciousAssetReasons`

## Data flow

- `audit capture OUTPUT_JSON` reuses `buildPayload` from `go/cmd/main.go`, then writes the normalized payload through `WriteCaptureArtifacts`.
- `WriteCaptureArtifacts` calls `WritePayload` for the JSON, writes `OUTPUT_JSON.command` containing the shell-safe rerun command from `captureRerunCommand` and `ShellJoin`, writes `OUTPUT_JSON.skipped.json` when `[]SkippedRow` is non-empty, and removes any stale skipped sidecar when the current capture has no skipped rows.
- `audit filter INPUT_JSON` parses CLI flags through `buildAuditFilters`, loads the payload with `LoadPayload`, filters rows with `FilterPayload`, and prints the resulting JSON.
- `FilterPayload` preserves `version` and `wallets` and only drops transactions that fail `matchesFilters`.
- `audit summary INPUT_JSON` optionally filters first, then calls `BuildSummary` and `MarshalSummary`.
- `BuildSummary` counts rows by source, chain, tx type, wallet, and raw type; calculates timestamp range; counts unique ids; and aggregates per-asset sent, received, and fee totals through `accumulateAsset` and `amountAccumulator`.

## Outputs / side effects

- `capture` writes a normalized JSON snapshot, a sibling `.command` file, and a `.skipped.json` sidecar when any rows were skipped during normalization. Reusing the same output path for a clean capture removes a stale skipped sidecar.
- `filter` writes filtered JSON to stdout.
- `summary` writes summary JSON to stdout.
- The summary currently includes `transaction_count`, `unique_id_count`, `missing_raw_type_count`, `timestamp_range`, `by_source`, `by_chain`, `by_tx_type`, `by_wallet`, `by_raw_type`, `by_asset`, `zero_usd_value_rows`, and `suspicious_asset_rows`.

## Current support boundary

### Implemented behavior

- Capture is reproducible because the exact rerun command is stored beside the snapshot.
- Filtering is exact on `id`, `wallet`, and `raw_type`, timestamp-aware for RFC3339 values, and case-insensitive for asset matches across `sent`, `received`, `fee`, and their optional `asset_canonical` fields.
- Summary output is stable and machine-readable enough for regression tests.

### Current-behavior-only checkpoints

- `go/audit/audit_test.go` freezes filter behavior, asset aggregation, summary flags, and capture artifact writing.
- `go/audit/real_wallet_regression_test.go` checks internal consistency between captured input payloads and their documented expectations in `go/audit/testdata/real-wallet/*.expected.json`. It does NOT run the normalizer (the inputs are normalized output, not raw source data); normalizer behavior regressions are covered by `go/normalize/normalize_test.go`.
- `docs/known-transactions.md` is the current evidence log that connects filtered payloads to manual review work.

### Unsupported but surfaced behavior

- Summary flags are heuristics for review priority, not tax-proof or semantics-proof outputs.
- Real-wallet fixtures freeze observed normalization only. They do not prove explorer truth or tax correctness.
- Capture preserves normalized payloads, not raw API responses.

## Current known approximations or conservative behavior

- `zero_usd_value_rows` is built by `buildZeroUSDValueRow`, which flags any row where `sent`, `received`, or `fee` has a USD value of exactly zero.
- `suspicious_asset_rows` is built by `buildSuspiciousAssetRow`, which combines the results of `suspiciousAssetReasons`, `looksLikeHexAddress`, `looksLikeBase58Mint`, and `looksLikeMintLikeAsset`.
- `buildSuspiciousAssetRow` adds `unresolved_asset_valuation` only when the suspicious asset reason implies manual identity review and that same leg also has `usd_value: "0"`.
- `accumulateAsset` groups assets case-insensitively by upper-cased displayed asset string, so any canonical-versus-display identity collapse that already happened in the IR stays collapsed in the summary.

## How current-behavior fixtures fit the workflow

- `docs/audit-workflow.md` describes the manual sequence: `capture`, then `filter`, then `summary`, then a comparison entry in `docs/known-transactions.md`.
- `go/audit/testdata/real-wallet/*.input.json` holds the normalized payload under review.
- `go/audit/testdata/real-wallet/*.expected.json` stores the documented current normalized rows plus TODO placeholders for human confirmation.
- `TestDocumentedRealWalletFixturesAreInternallyConsistent` asserts that each documented expectation matches its captured input snapshot and that unresolved human-verification placeholders stay explicit. The snapshots are frozen at capture time (`status: documented_snapshot_normalization`); after intentional normalizer changes they describe the OLD behavior until a fresh `audit capture` refresh.

## Notable current divergences from spec

None beyond the deliberately documented heuristic boundary above.
