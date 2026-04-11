# Solana Spec

This document defines the intended support boundary for Solana ingestion and normalization.

## Source of truth

The current Solana pipeline is based on Helius enhanced transactions plus any human-reviewed explorer evidence recorded in repository docs.

Agents must not infer stronger semantics from free-form descriptions or labels when structured source data does not support them.

## Asset identity contract

For Solana token activity, canonical identity should prefer source-backed mint identity.

Where Helius or another adopted source also provides a source-backed display symbol or label, that may be preserved separately. It must not replace mint identity by guesswork.

### Rules

- mint-like identifiers are valid canonical Solana asset identity
- display symbols are optional metadata, not a substitute for canonical identity
- lack of a symbol is not license to guess one
- unresolved mint-only assets may remain unresolved in normalized output when necessary

## Transaction modeling contract

The Solana pipeline should preserve economically meaningful legs conservatively, even when one transaction contains many internal account moves.

### Supported intent

- simple transfer-like and swap-like cases where the source payload is clear enough to represent conservatively

### Current-behavior-only or unsupported but surfaced

- multi-leg Solana transactions that explode into awkward rows
- same-asset SOL-for-SOL swap-like rows caused by aggregator routing or internal wrapping behavior
- mint-only assets with unresolved valuation

## Valuation contract

Do not invent Solana token valuations.

A Solana leg may legitimately remain at `0` USD when:

- the repository lacks a supported valuation path for that asset identity, or
- the source-backed identity is still unresolved enough that valuation would be guesswork

In those cases, audit output should make the unresolved status easy to review.

## Immediate priority cases

The current highest-priority Solana work is:

- preserving canonical identity vs display identity cleanly
- improving audit surfacing for unresolved mint-only assets
- distinguishing between genuine swaps and multi-leg routing artifacts without guessing
