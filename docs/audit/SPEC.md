# Audit Spec

This document defines what the repository's audit tooling is for and what it must not claim.

## Purpose

Audit tooling exists to:

- capture a normalized snapshot reproducibly
- filter representative cases for inspection
- summarize high-risk anomalies and reconciliation buckets
- preserve evidence for human review and future implementation work

Audit tooling does **not** make unsupported tax semantics correct by itself.

## Intended properties

- capture should preserve the exact normalized payload under review
- filtering should make representative cases easy to isolate without mutating them
- summaries should surface unresolved or suspicious patterns early
- outputs should be stable and machine-readable enough for regression tests and review workflows

## Current high-priority audit signals

The current highest-priority review buckets are:

- `zero_usd_value_rows`
- `suspicious_asset_rows`
- unusual `tx_type` / `raw_type` / `source` groupings

## Support boundary

### Supported

- using audit output to find cases that deserve review first
- using audit fixtures to freeze current observed normalization behavior intentionally

### Not sufficient by itself

- proving tax correctness of a real-wallet case
- proving that a current normalized representation is the right semantic interpretation

## Human-verification boundary

Human verification is reserved for cases where repository support claims may change, such as:

- confirming a real bridge pattern as own-wallet movement
- confirming whether a suspicious asset is real, spam, or mislabeled
- confirming whether a current split-row or swap-like representation matches the source truth closely enough to upgrade support status
