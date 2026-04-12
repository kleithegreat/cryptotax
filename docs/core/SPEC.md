# Financial Core Spec

This document defines the intended scope of the Haskell financial core.

## Purpose

The Haskell core consumes normalized events and produces accounting and tax-oriented output for supported cases.

It is responsible for applying accounting semantics, not for guessing what upstream source data meant.

## Responsibilities

The core should own:

- FIFO lot accounting
- basis and proceeds calculations for supported dispositions
- treatment of supported income-like events
- generation of final tax-oriented output for supported cases

The core should not own:

- source-specific transaction reconstruction
- wallet-ownership inference
- symbol guessing
- source-truth validation against explorers or CSV exports

## Core invariants

- arithmetic must remain exact; do not introduce floating-point tax math
- supported disposals must consume inventory consistently
- unsupported or ambiguous events must not silently disappear
- final output must reflect the semantics actually modeled, not stronger claims

## Current support intent

### Supported

- the small local end-to-end golden fixture covering buy, sell, and own-wallet transfer behavior
- FIFO lot accounting for supported normalized events that map cleanly into current core semantics

### Current-behavior-only

- many real-wallet cases that reach the core through current normalization snapshots
- any behavior that is guarded only by current-behavior fixtures rather than evidence-backed semantic proof

### Unsupported but surfaced

- normalized cases that represent economically meaningful activity but do not yet have a final supported tax interpretation in the core

## Immediate review priorities

The highest-priority open semantics questions for the core are:

- how negative Hyperliquid funding should appear in final output
- how broader income/expense handling should be represented when upstream normalization is conservative but incomplete
- how unsupported multi-leg or perp-like cases should be preserved without implying false precision
